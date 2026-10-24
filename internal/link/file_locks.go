package link

import (
	"context"
	"sync"
)

// A slow network transfer must not serialize unrelated local files. Keep only
// active file IDs in memory, and allow a canceled waiter to leave immediately.
type fileLocks struct {
	mu      sync.Mutex
	entries map[string]*fileLock
}

type fileLock struct {
	token chan struct{}
	users int
}

func (l *fileLocks) acquire(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[string]*fileLock)
	}
	entry := l.entries[id]
	if entry == nil {
		entry = &fileLock{token: make(chan struct{}, 1)}
		l.entries[id] = entry
	}
	entry.users++
	l.mu.Unlock()
	drop := func() {
		l.mu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(l.entries, id)
		}
		l.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case entry.token <- struct{}{}:
		return func() { <-entry.token; drop() }, nil
	}
}
