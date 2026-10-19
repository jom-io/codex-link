package link

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"time"
)

// Discovery reuses Redis's existing device directory. No multicast service,
// network scan or new rendezvous server is needed. Cached, pinned endpoints
// remain usable during a Redis outage while their address stays reachable.
type directAdvertisement struct {
	Device    string    `json:"device"`
	Public    string    `json:"public"`
	Scope     string    `json:"scope"`
	Endpoints []string  `json:"endpoints"`
	SeenAt    time.Time `json:"seen_at"`
	Version   int       `json:"version"`
	Signature string    `json:"signature"`
}

func (a directAdvertisement) signedBytes() []byte {
	a.Signature = ""
	b, _ := json.Marshal(a)
	return append([]byte("agent-link/direct/v1/"), b...)
}

func (t *Transport) directAdvertisement() directAdvertisement {
	a := directAdvertisement{Device: t.config.DeviceID, Public: t.identity.SignPublic(), Scope: t.prefix, Endpoints: t.endpoints, SeenAt: time.Now().UTC(), Version: 1}
	a.Signature = t.identity.Sign(a.signedBytes())
	return a
}

func (t *Transport) verifyDirect(ctx context.Context, id string, b []byte) (directAdvertisement, bool) {
	var a directAdvertisement
	if len(b) > 8192 || json.Unmarshal(b, &a) != nil || a.Device != id || a.Version != 1 || a.Scope != t.prefix || len(a.Endpoints) > 8 || len(a.Endpoints) == 0 || a.SeenAt.After(time.Now().Add(time.Minute)) || time.Since(a.SeenAt) > 7*24*time.Hour {
		return a, false
	}
	pair, e := t.store.Pair(ctx, id)
	if e != nil || !pair.Ready() || a.Public != pair.Peer.SignPublic || !verifySignature(a.Public, a.signedBytes(), a.Signature) {
		return a, false
	}
	return a, true
}

func (t *Transport) directPeer(ctx context.Context, id string) (directAdvertisement, bool) {
	if t.store == nil {
		return directAdvertisement{}, false
	}
	cached := t.store.metadata(ctx, "direct-cache:"+id)
	a, valid := t.verifyDirect(ctx, id, []byte(cached))
	if valid && time.Since(a.SeenAt) < time.Minute {
		return a, true
	}
	probe, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	b, e := t.client.HGet(probe, t.prefix+"direct", id).Bytes()
	if e == nil {
		if fresh, ok := t.verifyDirect(ctx, id, b); ok {
			t.store.setMetadata(ctx, "direct-cache:"+id, string(b))
			return fresh, true
		}
	}
	return a, valid
}

func privateNetworks() ([]string, []*net.IPNet) {
	var addresses []string
	var networks []*net.IPNet
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok || network.IP.To4() == nil || !network.IP.IsPrivate() {
				continue
			}
			addresses = append(addresses, network.IP.String())
			networks = append(networks, network)
			if len(addresses) == 8 {
				return addresses, networks
			}
		}
	}
	return addresses, networks
}

func (d *directRouter) allowedIP(ip net.IP) bool {
	if ip == nil || (!ip.IsPrivate() && !(d.allowLoopback && ip.IsLoopback())) {
		return false
	}
	for _, network := range d.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (d *directRouter) allowedEndpoint(endpoint string) bool {
	host, port, e := net.SplitHostPort(endpoint)
	p, err := strconv.Atoi(port)
	return e == nil && err == nil && p > 0 && p <= 65535 && d.allowedIP(net.ParseIP(host))
}
