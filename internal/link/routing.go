package link

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"
)

func (d *Daemon) acceptIncoming(ctx context.Context, m Message, cursor string, receipt *Message, route string) error {
	if m.Activity != nil {
		if m.Kind != "progress" || m.Activity.TaskID != m.ReplyTo || m.Activity.Worker != m.Session {
			return errors.New("invalid activity")
		}
		if e := d.Store.SaveActivity(ctx, m.From, *m.Activity); e != nil {
			return e
		}
	}
	if e := d.Store.accept(ctx, &m, cursor, receipt, route); e != nil {
		return e
	}
	d.notify()
	return nil
}

func (d *Daemon) fileKey(ctx context.Context, peer string) (string, error) {
	if peer == d.Config.DeviceID {
		return d.Transport.identity.PairKey(d.Config.Workspace+"/local", d.Transport.LocalPeer())
	}
	return d.Transport.PairKey(ctx, peer)
}

func (d *Daemon) deliverOutgoing(ctx context.Context, m Message) error {
	if m.To == d.Config.DeviceID {
		if m.File != nil {
			src, e := os.Open(d.Files.stagedPath(m.ID))
			if e != nil {
				return e
			}
			key, e := d.fileKey(ctx, m.To)
			if e == nil {
				d.fileMu.Lock()
				var path string
				path, e = d.Files.receive(m, key, src)
				d.fileMu.Unlock()
				if e == nil {
					e = d.Store.SetFile(ctx, m.ID, path)
				}
			}
			src.Close()
			if e != nil {
				return e
			}
		}
		if e := d.Store.deliverLocal(ctx, m.ID); e != nil {
			return e
		}
		d.finishStaged(ctx, m)
		d.notify()
		return nil
	}
	if d.Direct != nil && !strings.HasPrefix(m.Kind, "pair_") {
		if e := d.Direct.send(ctx, m); e == nil {
			if e = d.Store.setRoute(ctx, m.ID, "lan"); e != nil {
				return e
			}
			d.finishStaged(ctx, m)
			return nil
		}
	}
	// Old queued files already live in OSS. New ones upload only if direct
	// delivery failed; a restart never rereads the original user path.
	if m.File != nil && d.Store.metadata(ctx, "staged-file:"+m.ID) == "pending" {
		if e := d.Files.uploadStaged(ctx, m); e != nil {
			return e
		}
		if e := d.Store.setMetadata(ctx, "staged-file:"+m.ID, "uploaded"); e != nil {
			return e
		}
	}
	if e := d.Transport.Publish(ctx, m); e != nil {
		return e
	}
	if e := d.Store.setRoute(ctx, m.ID, "cloud"); e != nil {
		return e
	}
	if e := d.Store.MarkSent(ctx, m.ID); e != nil {
		return e
	}
	d.finishStaged(ctx, m)
	return nil
}

func (d *Daemon) finishStaged(ctx context.Context, m Message) {
	if m.File != nil && d.Store.metadata(ctx, "staged-file:"+m.ID) != "" {
		os.Remove(d.Files.stagedPath(m.ID))
		d.Store.db.ExecContext(ctx, "DELETE FROM metadata WHERE key=?", "staged-file:"+m.ID)
	}
}

func (d *Daemon) flushPending(ctx context.Context, files bool) {
	rs, e := d.Store.Pending(ctx)
	if e != nil {
		return
	}
	for _, r := range rs {
		if (r.Message.File != nil) != files {
			continue
		}
		timeout := 4 * time.Second
		if files {
			timeout = 30 * time.Minute
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		e = d.deliverOutgoing(attempt, r.Message)
		cancel()
		if e != nil && !errors.Is(e, ErrNotPaired) && ctx.Err() == nil {
			log.Print("delivery unavailable; queued for retry")
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (d *Daemon) send(ctx context.Context)      { d.sendLoop(ctx, false) }
func (d *Daemon) sendFiles(ctx context.Context) { d.sendLoop(ctx, true) }

func (d *Daemon) sendLoop(ctx context.Context, files bool) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.changedChan():
		}
		d.flushPending(ctx, files)
	}
}

func (d *Daemon) heartbeat(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		e := d.Transport.Heartbeat(attempt)
		cancel()
		d.setConnected(e == nil)
		d.checkActivity(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
