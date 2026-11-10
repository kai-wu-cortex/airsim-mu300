package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type WatchIdentity struct {
	AgentID    string `json:"agent_id"`
	CallID     string `json:"call_id"`
	CallUUID   string `json:"call_uuid"`
	Generation uint64 `json:"generation"`
}
type WatchState struct {
	State    string `json:"state"`
	Instance string `json:"instance"`
	Owned    bool   `json:"owned"`
}
type WatchBackend interface {
	Action(context.Context, string, string, WatchIdentity) (WatchState, error)
}
type watchGrant struct {
	WatchIdentity
	Owner        string `json:"owner"`
	Instance     string `json:"instance"`
	Session      string `json:"session"`
	ExpiresAt    int64  `json:"expires_at"`
	SessionUntil int64  `json:"session_until"`
}
type watchLease struct {
	grant   watchGrant
	encoded string
}

var watchUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (v WatchIdentity) valid() bool {
	return earlyIdentity(v.AgentID) && earlyIdentity(v.CallID) && watchUUID.MatchString(v.CallUUID) && v.Generation > 0 && v.Generation <= 1<<63-1
}
func deriveWatch(key []byte, domain string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(domain))
	return h.Sum(nil)
}
func watchOwner(key []byte) string                { return fmt.Sprintf("%x", sha256.Sum256(key)) }
func (g *Gateway) SetWatchBackend(b WatchBackend) { g.watchBackend = b }
func (g *Gateway) watchAuth(r *http.Request, body []byte) ([]byte, string) {
	if r.Header.Get("X-AirSIM-Watch-Control") != "2" || r.Header.Get("X-AirSIM-VoWLAN-Version") != "1" {
		return nil, ""
	}
	ts, nonce, sig := r.Header.Get("X-AirSIM-VoWLAN-Timestamp"), r.Header.Get("X-AirSIM-VoWLAN-Nonce"), r.Header.Get("X-AirSIM-VoWLAN-Signature")
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.freshWatchNonceLocked(ts, nonce) {
		return nil, ""
	}
	for _, encoded := range g.watchParentsLocked() {
		parent, _ := decode(encoded)
		key := deriveWatch(parent, "AirSIM/watch-call-only/v2")
		owner := watchOwner(parent)
		clear(parent)
		if hmac.Equal([]byte(Sign(key, canonical(r.Method, r.URL.RequestURI(), body, ts, nonce))), []byte(sig)) {
			g.seen[nonce] = time.Now().Unix() + 61
			return key, owner
		}
		clear(key)
	}
	return nil, ""
}
func (g *Gateway) freshWatchNonceLocked(ts, nonce string) bool {
	n, e := strconv.ParseInt(ts, 10, 64)
	now := time.Now().Unix()
	if e != nil || strconv.FormatInt(n, 10) != ts || n < now-30 || n > now+30 || len(nonce) < 7 || len(nonce) > 128 {
		return false
	}
	for k, v := range g.seen {
		if v < now {
			delete(g.seen, k)
		}
	}
	_, seen := g.seen[nonce]
	return !seen && len(g.seen) < 8192
}

// Check durable state as well: a local pairing reset must revoke live Watch
// sessions even before the service restarts. Never adopt arbitrary new keys here.
func (g *Gateway) watchParentsLocked() []string {
	raw, e := os.ReadFile(g.path)
	if e != nil {
		return nil
	}
	var disk []string
	if json.Unmarshal(raw, &disk) != nil || len(disk) > 16 {
		return nil
	}
	var matched []string
	for _, k := range g.keys {
		for _, v := range disk {
			if k == v {
				matched = append(matched, k)
				break
			}
		}
	}
	return matched
}
func (g *Gateway) watchKeyLocked(owner string) []byte {
	for _, encoded := range g.watchParentsLocked() {
		key, _ := decode(encoded)
		match := watchOwner(key) == owner
		derived := deriveWatch(key, "AirSIM/watch-call-only/v2")
		clear(key)
		if match {
			return derived
		}
		clear(derived)
	}
	return nil
}
func (g *Gateway) watchResponse(w http.ResponseWriter, nonce string, key []byte, code int, id WatchIdentity, state, grant string) {
	payload := struct {
		WatchIdentity
		Version int    `json:"version"`
		State   string `json:"state"`
		Grant   string `json:"grant,omitempty"`
	}{id, 2, state, grant}
	b, _ := json.Marshal(payload)
	encoded := base64.RawURLEncoding.EncodeToString(b)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"payload": encoded, "proof": Sign(key, fmt.Sprintf("watch-response-v2\n%s\n%d\n%s", nonce, code, encoded))})
}
func (g *Gateway) serveWatch(w http.ResponseWriter, r *http.Request) {
	if g.watchBackend == nil || r.Method != "POST" || (r.URL.Path != "/api/watch/status" && r.URL.Path != "/api/watch/answer" && r.URL.Path != "/api/watch/hangup") {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "body", 413)
		return
	}
	key, owner := g.watchAuth(r, body)
	if key == nil {
		http.Error(w, "authentication required", 401)
		return
	}
	defer clear(key)
	var id WatchIdentity
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&id)
	var extra any
	code, state, grant := 400, "unknown", ""
	if err == nil && decoder.Decode(&extra) == io.EOF && id.valid() && id.AgentID == g.id && r.URL.RawQuery == "" {
		action := strings.TrimPrefix(r.URL.Path, "/api/watch/")
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		s, e := g.watchBackend.Action(ctx, action, owner, id)
		code = 409
		if e == nil {
			state = s.State
			code = 200
			if s.State == "active" && s.Owned && s.Instance != "" {
				grant, e = g.issueWatch(owner, id, s.Instance)
				if e != nil {
					code = 409
					state = "unknown"
				}
			}
		}
	}
	g.watchResponse(w, r.Header.Get("X-AirSIM-VoWLAN-Nonce"), key, code, id, state, grant)
}
func (g *Gateway) issueWatch(owner string, id WatchIdentity, instance string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := g.watchKeyLocked(owner)
	if key == nil || g.closing {
		return "", errors.New("unpaired")
	}
	clear(key)
	now := time.Now().Unix()
	if g.watchLeases == nil {
		g.watchLeases = map[string]*watchLease{}
	}
	for k, l := range g.watchLeases {
		if l.grant.SessionUntil <= now {
			delete(g.watchLeases, k)
		}
	}
	if l := g.watchLeases[instance]; l != nil {
		if l.grant.Owner != owner || l.grant.WatchIdentity != id {
			return "", errors.New("owned")
		}
		return l.encoded, nil
	}
	if len(g.watchLeases) >= 64 {
		return "", errors.New("capacity")
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	session := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	grant := watchGrant{id, owner, instance, session, now + 60, now + 14400}
	raw, _ := json.Marshal(grant)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	g.watchLeases[instance] = &watchLease{grant, encoded}
	return encoded, nil
}

// Remove revokes a parent and all its Watch grants. Media observes removal on its
// bounded revalidation loop. Failed durable writes leave the old state intact.
func (g *Gateway) Remove(encoded string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	parent, e := decode(encoded)
	if e != nil {
		return e
	}
	owner := watchOwner(parent)
	clear(parent)
	keys := make([]string, 0, len(g.keys))
	for _, k := range g.keys {
		if k != encoded {
			keys = append(keys, k)
		}
	}
	if err := g.persistKeys(keys); err != nil {
		return err
	}
	g.keys = keys
	for k, l := range g.watchLeases {
		if l.grant.Owner == owner {
			delete(g.watchLeases, k)
		}
	}
	return nil
}

type mu300WatchBackend struct{ client *http.Client }

func NewMU300WatchBackend() WatchBackend { return &mu300WatchBackend{newEarlyHTTPClient()} }
func (b *mu300WatchBackend) Action(ctx context.Context, action, owner string, id WatchIdentity) (WatchState, error) {
	raw, _ := json.Marshal(id)
	req, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8575/internal/watch/"+action, bytes.NewReader(raw))
	if err != nil {
		return WatchState{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AirSIM-Internal-Call-Owner", "wlan:"+owner)
	resp, err := b.client.Do(req)
	if err != nil {
		return WatchState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return WatchState{}, errors.New("watch call unavailable")
	}
	var s WatchState
	err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&s)
	return s, err
}

// ServeRevokeParent is installed on the loopback admin listener only. The body
// is a non-secret fingerprint, never a pairing key. No modem command is sent.
func (g *Gateway) ServeRevokeParent(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		http.Error(w, "loopback only", 403)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var body struct {
		Owner string `json:"owner"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&body) != nil || d.Decode(&extra) != io.EOF || len(body.Owner) != 64 || strings.Trim(body.Owner, "0123456789abcdef") != "" {
		http.Error(w, "owner", 400)
		return
	}
	g.mu.Lock()
	encoded := ""
	for _, candidate := range g.keys {
		key, _ := decode(candidate)
		if watchOwner(key) == body.Owner {
			encoded = candidate
		}
		clear(key)
	}
	g.mu.Unlock()
	if encoded == "" {
		w.WriteHeader(204)
		return
	}
	if g.Remove(encoded) != nil {
		http.Error(w, "persist failed", 500)
		return
	}
	w.WriteHeader(204)
}
