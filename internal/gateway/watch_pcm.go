package gateway

import (
	"bufio"
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"time"
)

func (g *Gateway) watchLeaseValid(l watchLease, attach bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	stored := g.watchLeases[l.grant.Instance]
	now := time.Now().Unix()
	if stored == nil || stored.encoded != l.encoded || g.closing || now >= stored.grant.SessionUntil || (attach && now >= stored.grant.ExpiresAt) {
		return false
	}
	key := g.watchKeyLocked(l.grant.Owner)
	ok := key != nil
	clear(key)
	return ok
}
func (g *Gateway) watchLive(ctx context.Context, l watchLease) bool {
	if !g.watchLeaseValid(l, false) {
		return false
	}
	bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	s, e := g.watchBackend.Action(bounded, "check", l.grant.Owner, l.grant.WatchIdentity)
	return e == nil && s.Owned && s.State == "active" && s.Instance == l.grant.Instance && g.watchLeaseValid(l, false)
}
func (g *Gateway) serveWatchPCM(parent context.Context, c net.Conn, reader *bufio.Reader, parts []string) {
	if len(parts) != 5 || g.watchBackend == nil || len(parts[1]) > 2048 {
		return
	}
	raw, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if e != nil {
		return
	}
	var grant watchGrant
	if json.Unmarshal(raw, &grant) != nil {
		return
	}
	l := watchLease{grant, parts[1]}
	g.mu.Lock()
	stored := g.watchLeases[grant.Instance]
	if stored == nil || stored.encoded != l.encoded || !g.freshWatchNonceLocked(parts[2], parts[3]) {
		g.mu.Unlock()
		return
	}
	key := g.watchKeyLocked(grant.Owner)
	if key == nil {
		g.mu.Unlock()
		return
	}
	media := deriveWatch(key, "watch-media-v2\n"+l.encoded)
	clear(key)
	ok := hmac.Equal([]byte(Sign(media, "watch-pcm-v2\n"+l.encoded+"\n"+parts[2]+"\n"+parts[3])), []byte(parts[4]))
	clear(media)
	if !ok {
		g.mu.Unlock()
		return
	}
	g.seen[parts[3]] = time.Now().Unix() + 61
	g.mu.Unlock()
	if !g.watchLeaseValid(l, true) || !g.watchLive(parent, l) {
		return
	}
	g.mu.Lock()
	if g.pcm || g.closing {
		g.mu.Unlock()
		return
	}
	g.pcm = true
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.pcm = false; g.mu.Unlock() }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Start cancellation monitoring before dialing/initializing the hardware.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				c.Close()
				return
			case <-ticker.C:
				if !g.watchLive(ctx, l) {
					cancel()
				}
			}
		}
	}()
	defer func() { cancel(); c.Close(); <-done }()
	dialer := net.Dialer{Timeout: 3 * time.Second}
	up, e := dialer.DialContext(ctx, "tcp", "127.0.0.1:7580")
	if e != nil {
		return
	}
	defer up.Close()
	upDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			up.Close()
		case <-upDone:
		}
	}()
	defer close(upDone)
	up.SetDeadline(time.Now().Add(18 * time.Second))
	if _, e = io.WriteString(up, "AIRSIMPCM1\n"); e != nil {
		return
	}
	ack := make([]byte, len("AIRSIMREADY"))
	if _, e = io.ReadFull(up, ack); e != nil || string(ack) != "AIRSIMREADY" {
		return
	}
	if ctx.Err() != nil || !g.watchLive(ctx, l) || !g.watchLeaseValid(l, true) {
		return
	}
	c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if e = writeEarlyFull(c, ack); e != nil {
		return
	}
	c.SetDeadline(time.Time{})
	up.SetDeadline(time.Time{})
	copied := make(chan struct{})
	go func() { io.Copy(up, reader); up.Close(); c.Close(); close(copied) }()
	io.Copy(c, up)
	up.Close()
	c.Close()
	<-copied
}
