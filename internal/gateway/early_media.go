package gateway

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// EarlyMediaBackend must distinguish a verified downlink source from a modem
// indication. Check is read-only and bound to the authenticated dial owner.
// Open may start capture only; it must not create an uplink or synthesize audio.
type EarlyMediaBackend interface {
	Enabled() bool
	Check(context.Context, string, string) (int, error)
	Open(context.Context) (io.ReadCloser, error)
}

type earlyLease struct {
	owner, callID, sessionID string
	expires                  int64
	issued                   map[int64]bool
}
type earlyGrant struct {
	Version   int    `json:"version"`
	AgentID   string `json:"agent_id"`
	CallID    string `json:"call_id"`
	SessionID string `json:"session_id"`
	ExpiresAt int64  `json:"expires_at"`
	State     string `json:"state"`
	Direction string `json:"direction"`
	Format    string `json:"format"`
	Proof     string `json:"proof"`
}

const earlyFormat = "s16le-8000-mono"
const earlyLeaseSeconds = 30

// Configure before serving. Every stream still requires fresh, owned early
// signaling evidence; providing a capture callback alone never starts audio.
func (g *Gateway) SetEarlyMediaBackend(b EarlyMediaBackend) { g.earlyBackend = b }
func earlyOwner(key []byte) string                          { return fmt.Sprintf("wlan:%x", sha256.Sum256(key)) }
func earlyIdentity(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func earlyMapKey(owner, call string) string { return owner + "\n" + call }
func (g *Gateway) checkEarly(ctx context.Context, owner, call string) int {
	if g.earlyBackend == nil || !g.earlyBackend.Enabled() {
		return 501
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	code, err := g.earlyBackend.Check(bounded, owner, call)
	if err != nil {
		return 410
	}
	switch code {
	case 200, 202, 403, 410, 501:
		return code
	}
	return 410
}
func (g *Gateway) earlyMediaControl(w http.ResponseWriter, r *http.Request, key []byte, nonce string) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) != 1 || len(values["call_id"]) != 1 || !earlyIdentity(values.Get("call_id")) {
		http.Error(w, "invalid early-media identity", 400)
		return
	}
	call, owner := values.Get("call_id"), earlyOwner(key)
	code := g.checkEarly(r.Context(), owner, call)
	if code != 200 {
		g.mu.Lock()
		delete(g.earlyLeases, earlyMapKey(owner, call))
		g.mu.Unlock()
		http.Error(w, http.StatusText(code), code)
		return
	}
	now := time.Now().Unix()
	g.mu.Lock()
	if !g.hasClientLocked(watchOwner(key)) {
		g.mu.Unlock()
		http.Error(w, "pairing revoked", 403)
		return
	}
	if g.earlyLeases == nil {
		g.earlyLeases = make(map[string]*earlyLease)
	}
	for k, l := range g.earlyLeases {
		if l.expires <= now || l.expires > now+earlyLeaseSeconds {
			delete(g.earlyLeases, k)
		}
	}
	mapKey := earlyMapKey(owner, call)
	lease := g.earlyLeases[mapKey]
	if lease == nil {
		if len(g.earlyLeases) >= 16 {
			g.mu.Unlock()
			http.Error(w, "early-media capacity", 429)
			return
		}
		var random [24]byte
		if _, err = rand.Read(random[:]); err != nil {
			g.mu.Unlock()
			http.Error(w, "session unavailable", 503)
			return
		}
		lease = &earlyLease{owner: owner, callID: call, sessionID: base64.RawURLEncoding.EncodeToString(random[:]), issued: make(map[int64]bool)}
		g.earlyLeases[mapKey] = lease
	}
	lease.expires = now + earlyLeaseSeconds
	for expiry := range lease.issued {
		if expiry <= now {
			delete(lease.issued, expiry)
		}
	}
	lease.issued[lease.expires] = true // integer seconds: at most 30 live issuance values
	grant := earlyGrant{1, g.id, call, lease.sessionID, lease.expires, "early_media", "recvonly", earlyFormat, ""}
	g.mu.Unlock()
	grant.Proof = Sign(key, strings.Join([]string{"AIRSIM-EARLY-MEDIA1", nonce, grant.AgentID, call, grant.SessionID, strconv.FormatInt(grant.ExpiresAt, 10), grant.State, grant.Direction, grant.Format}, "\n"))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(grant)
}

func (g *Gateway) earlyLeaseValid(owner, call, session string, issued int64, handshake bool) bool {
	now := time.Now().Unix()
	g.mu.Lock()
	defer g.mu.Unlock()
	l := g.earlyLeases[earlyMapKey(owner, call)]
	if !g.hasClientLocked(strings.TrimPrefix(owner, "wlan:")) || l == nil || l.sessionID != session || l.expires <= now || l.expires > now+earlyLeaseSeconds {
		return false
	}
	return !handshake || (issued > now && issued <= now+earlyLeaseSeconds && l.issued[issued])
}

func (g *Gateway) serveEarlyPCM(parent context.Context, c net.Conn, reader *bufio.Reader, parts []string) {
	if len(parts) != 5 || g.earlyBackend == nil || !g.earlyBackend.Enabled() {
		return
	}
	// Client binding is RFC4648 standard Base64, distinct from base64url HMAC.
	binding, err := base64.StdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(binding) > 512 {
		return
	}
	fields := strings.Split(string(binding), "\n")
	if len(fields) != 6 || fields[0] != g.id || !earlyIdentity(fields[1]) || !earlyIdentity(fields[2]) || fields[4] != "recvonly" || fields[5] != earlyFormat {
		return
	}
	expiry, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || strconv.FormatInt(expiry, 10) != fields[3] {
		return
	}
	key := g.authenticate("PCM", "/v1/pcm/early-media", binding, parts[1], parts[2], parts[3])
	if key == nil {
		return
	}
	owner := earlyOwner(key)
	clear(key)
	call, session := fields[1], fields[2]
	if !g.earlyLeaseValid(owner, call, session, expiry, true) || g.checkEarly(parent, owner, call) != 200 {
		return
	}
	g.mu.Lock()
	if !g.reserveClientPCMLocked(strings.TrimPrefix(owner, "wlan:")) {
		g.mu.Unlock()
		return
	}
	done := make(chan struct{})
	g.earlyStreamDone = done
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.pcm = false; g.earlyStreamDone = nil; close(done); g.mu.Unlock() }()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Monitor even during backend startup. Any inbound bytes are a protocol
	// violation, never an uplink. Disconnect, state loss and expiry cancel capture.
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		checkAt := time.Now()
		for {
			select {
			case <-ctx.Done():
				c.Close()
				return
			case <-ticker.C:
				if !g.earlyLeaseValid(owner, call, session, 0, false) {
					cancel()
					continue
				}
				if time.Since(checkAt) >= 250*time.Millisecond {
					checkAt = time.Now()
					if g.checkEarly(ctx, owner, call) != 200 {
						cancel()
					}
				}
			}
		}
	}()
	c.SetReadDeadline(time.Time{})
	incomingDone := make(chan struct{})
	go func() { defer close(incomingDone); var b [1]byte; _, _ = reader.Read(b[:]); cancel() }()
	defer func() { cancel(); c.Close(); <-watchDone; <-incomingDone }()
	setup := time.AfterFunc(8*time.Second, func() { cancel() })
	defer setup.Stop()
	source, err := g.earlyBackend.Open(ctx)
	if err != nil {
		return
	}
	// Close waits for ALSA teardown; the shared PCM reservation is released last.
	defer source.Close()
	sourceDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			source.Close()
		case <-sourceDone:
		}
	}()
	defer close(sourceDone)
	first := make([]byte, 320)
	_, err = io.ReadFull(source, first)
	setup.Stop()
	if err != nil || ctx.Err() != nil || g.checkEarly(ctx, owner, call) != 200 || !g.earlyLeaseValid(owner, call, session, 0, false) {
		return
	}
	c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err = io.WriteString(c, "AIRSIMREADY"); err != nil {
		return
	}
	// Keep read side deadline clear; leases are governed by the monitor, including
	// renewal. Every frame is from the capture source; there is no silence filler.
	c.SetReadDeadline(time.Time{})
	frameWatch := time.AfterFunc(3*time.Second, cancel)
	defer frameWatch.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err = writeEarlyFull(c, first); err != nil {
			return
		}
		if _, err = io.ReadFull(source, first); err != nil {
			return
		}
		frameWatch.Reset(3 * time.Second)
	}
}
func writeEarlyFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
