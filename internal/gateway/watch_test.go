package gateway

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type watchFixture struct {
	mu    sync.Mutex
	state WatchState
	calls int
}

func (b *watchFixture) Action(_ context.Context, action, owner string, id WatchIdentity) (WatchState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	return b.state, nil
}
func (b *watchFixture) set(state string) { b.mu.Lock(); defer b.mu.Unlock(); b.state.State = state }
func watchSetup(t *testing.T) (*Gateway, *watchFixture, []byte, []byte, WatchIdentity) {
	t.Helper()
	g, e := New(filepath.Join(t.TempDir(), "keys.json"), "fixture-agent")
	if e != nil {
		t.Fatal(e)
	}
	parent := []byte(strings.Repeat("p", 32))
	g.Add(base64.RawURLEncoding.EncodeToString(parent))
	key := deriveWatch(parent, "AirSIM/watch-call-only/v2")
	b := &watchFixture{state: WatchState{"active", "native-instance", true}}
	g.SetWatchBackend(b)
	return g, b, parent, key, WatchIdentity{g.id, "fixture-call", "891eae43-89f7-4baa-a21e-e15beecbe584", 7}
}
func watchRequest(g *Gateway, key []byte, id WatchIdentity, path, nonce string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(id)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	ts := fmt.Sprint(time.Now().Unix())
	r.Header.Set("X-AirSIM-Watch-Control", "2")
	r.Header.Set("X-AirSIM-VoWLAN-Version", "1")
	r.Header.Set("X-AirSIM-VoWLAN-Timestamp", ts)
	r.Header.Set("X-AirSIM-VoWLAN-Nonce", nonce)
	r.Header.Set("X-AirSIM-VoWLAN-Signature", Sign(key, canonical("POST", path, raw, ts, nonce)))
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func TestWatchSignedControlAndCredentialIsolation(t *testing.T) {
	g, b, parent, key, id := watchSetup(t)
	w := watchRequest(g, key, id, "/api/watch/answer", "watch-one")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var env map[string]string
	json.Unmarshal(w.Body.Bytes(), &env)
	if env["proof"] != Sign(key, "watch-response-v2\nwatch-one\n200\n"+env["payload"]) {
		t.Fatal("response proof")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(env["payload"])
	var result struct {
		WatchIdentity
		Version int
		Grant   string
	}
	json.Unmarshal(raw, &result)
	if result.Version != 2 || result.WatchIdentity != id || result.Grant == "" {
		t.Fatal("identity/grant")
	}
	w = watchRequest(g, key, id, "/api/watch/answer", "watch-two")
	json.Unmarshal(w.Body.Bytes(), &env)
	raw, _ = base64.RawURLEncoding.DecodeString(env["payload"])
	var again struct{ Grant string }
	json.Unmarshal(raw, &again)
	if again.Grant != result.Grant {
		t.Fatal("retry minted TTL")
	}
	before := b.calls
	if watchRequest(g, key, id, "/api/watch/status", "watch-two").Code != 401 {
		t.Fatal("replay")
	}
	if watchRequest(g, parent, id, "/api/watch/status", "watch-parent").Code != 401 {
		t.Fatal("parent accepted as watch")
	}
	if watchRequest(g, key, id, "/api/calls/answer", "watch-general").Code != 401 {
		t.Fatal("scope escaped")
	}
	wrong := id
	wrong.AgentID = "other"
	if watchRequest(g, key, wrong, "/api/watch/status", "watch-wrong-agent").Code != 400 {
		t.Fatal("cross agent")
	}
	if b.calls != before {
		t.Fatal("rejected request reached backend")
	}
	for _, k := range [][]byte{key, deriveWatch(key, "watch-media-v2\n"+result.Grant)} {
		ts := fmt.Sprint(time.Now().Unix())
		nonce := "old-pcm-nonce"
		if g.authenticate("PCM", "/v1/pcm", nil, ts, nonce, Sign(k, canonical("PCM", "/v1/pcm", nil, ts, nonce))) != nil {
			t.Fatal("watch credential accepted by legacy PCM")
		}
	}
	if e := g.Remove(base64.RawURLEncoding.EncodeToString(parent)); e != nil {
		t.Fatal(e)
	}
	if watchRequest(g, key, id, "/api/watch/status", "watch-revoked").Code != 401 {
		t.Fatal("revoked control")
	}
	reloaded, e := New(g.path, g.id)
	if e != nil || len(reloaded.keys) != 0 {
		t.Fatal("revocation not persistent")
	}
}
func watchPreface(key []byte, grant, nonce string) string {
	ts := fmt.Sprint(time.Now().Unix())
	media := deriveWatch(key, "watch-media-v2\n"+grant)
	return fmt.Sprintf("AIRSIMWATCH2 %s %s %s %s\n", grant, ts, nonce, Sign(media, "watch-pcm-v2\n"+grant+"\n"+ts+"\n"+nonce))
}
func TestWatchPCMGrantDuplexAndRevocation(t *testing.T) {
	for _, end := range []string{"ended", "unpair", "reset-file", "identity", "hard-limit"} {
		t.Run(end, func(t *testing.T) {
			g, b, parent, key, id := watchSetup(t)
			grant, e := g.issueWatch(watchOwner(parent), id, b.state.Instance)
			if e != nil {
				t.Fatal(e)
			}
			listener, e := net.Listen("tcp", "127.0.0.1:7580")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			backendDone := make(chan struct{})
			go func() {
				defer close(backendDone)
				c, e := listener.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				r := bufio.NewReader(c)
				line, _ := r.ReadString('\n')
				if line != "AIRSIMPCM1\n" {
					return
				}
				io.WriteString(c, "AIRSIMREADY")
				io.Copy(c, r)
			}()
			server, client := net.Pipe()
			defer client.Close()
			done := make(chan struct{})
			go func() { g.ServePCM(context.Background(), server); close(done) }()
			client.SetDeadline(time.Now().Add(3 * time.Second))
			io.WriteString(client, watchPreface(key, grant, "watch-pcm-one"))
			ack := make([]byte, 11)
			if _, e = io.ReadFull(client, ack); e != nil || string(ack) != "AIRSIMREADY" {
				t.Fatal("ready", e, string(ack))
			}
			io.WriteString(client, "fixture-frame")
			frame := make([]byte, 13)
			if _, e = io.ReadFull(client, frame); e != nil || string(frame) != "fixture-frame" {
				t.Fatal("duplex", e)
			}
			// A second authenticated socket may not overlap, nor replay this preface.
			for _, nonce := range []string{"watch-pcm-one", "watch-pcm-second"} {
				a, c := net.Pipe()
				closed := make(chan struct{})
				go func() { g.ServePCM(context.Background(), a); close(closed) }()
				c.SetDeadline(time.Now().Add(time.Second))
				io.WriteString(c, watchPreface(key, grant, nonce))
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("duplicate PCM admitted")
				}
				c.Close()
			}
			// Expired attach TTL must not kill an already attached session.
			g.mu.Lock()
			l := g.watchLeases[b.state.Instance]
			l.grant.ExpiresAt = time.Now().Unix() - 1
			g.mu.Unlock()
			time.Sleep(120 * time.Millisecond)
			io.WriteString(client, "ok")
			if _, e = io.ReadFull(client, make([]byte, 2)); e != nil {
				t.Fatal("attach expiry killed stream", e)
			}
			switch end {
			case "ended":
				b.set("ended")
			case "reset-file":
				os.Remove(g.path)
			case "unpair":
				g.Remove(base64.RawURLEncoding.EncodeToString(parent))
			case "identity":
				b.mu.Lock()
				b.state.Instance = "replacement"
				b.mu.Unlock()
			case "hard-limit":
				g.mu.Lock()
				l.grant.SessionUntil = time.Now().Unix() - 1
				g.mu.Unlock()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("revocation did not close stream")
			}
			<-backendDone
			g.mu.Lock()
			busy := g.pcm
			g.mu.Unlock()
			if busy {
				t.Fatal("PCM reservation leaked")
			}
		})
	}
}
func TestWatchPCMRejectsUnissuedTamperedExpiredAndReplay(t *testing.T) {
	g, b, parent, key, id := watchSetup(t)
	grant, _ := g.issueWatch(watchOwner(parent), id, b.state.Instance)
	lines := []string{watchPreface(key, grant+"x", "watch-bad-grant"), watchPreface([]byte(strings.Repeat("x", 32)), grant, "watch-bad-key")}
	g.mu.Lock()
	g.seen["watch-replayed"] = time.Now().Unix() + 61
	g.mu.Unlock()
	lines = append(lines, watchPreface(key, grant, "watch-replayed"))
	for _, line := range lines {
		a, c := net.Pipe()
		done := make(chan struct{})
		go func() { g.ServePCM(context.Background(), a); close(done) }()
		c.SetDeadline(time.Now().Add(time.Second))
		io.WriteString(c, line)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("invalid handshake accepted")
		}
		c.Close()
	}
	g.mu.Lock()
	g.watchLeases[b.state.Instance].grant.ExpiresAt = time.Now().Unix() - 1
	g.mu.Unlock()
	// The issued record is authoritative, not only the JSON client supplied.
	raw, _ := base64.RawURLEncoding.DecodeString(grant)
	var v watchGrant
	json.Unmarshal(raw, &v)
	if g.watchLeaseValid(watchLease{v, grant}, true) {
		t.Fatal("expired issued grant accepted")
	}
}
func TestWatchRevokeEndpointIsLocalOnly(t *testing.T) {
	g, _, parent, _, _ := watchSetup(t)
	body := fmt.Sprintf(`{"owner":%q}`, watchOwner(parent))
	for _, addr := range []string{"192.168.1.3:5000", "127.0.0.1:5000"} {
		r := httptest.NewRequest(http.MethodPost, "/internal/watch/revoke-parent", strings.NewReader(body))
		r.RemoteAddr = addr
		w := httptest.NewRecorder()
		g.ServeRevokeParent(w, r)
		if strings.HasPrefix(addr, "127.") {
			if w.Code != 204 {
				t.Fatal(w.Code)
			}
		} else if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
}

func TestWatchCrossLanguageVectors(t *testing.T) {
	raw, e := os.ReadFile("testdata/watch-direct-v2-vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]string
	if json.Unmarshal(raw, &v) != nil {
		t.Fatal("vector JSON")
	}
	parent, e := hex.DecodeString(v["root_hex"])
	if e != nil {
		t.Fatal(e)
	}
	key := deriveWatch(parent, "AirSIM/watch-call-only/v2")
	if hex.EncodeToString(key) != v["control_hex"] {
		t.Fatal("Kwatch differs")
	}
	media := deriveWatch(key, "watch-media-v2\n"+v["grant"])
	if hex.EncodeToString(media) != v["media_hex"] {
		t.Fatal("Kmedia differs")
	}
	if Sign(media, v["pcm_canonical"]) != v["pcm_signature"] {
		t.Fatal("PCM signature differs")
	}
	if Sign(key, "watch-response-v2\nfixture-nonce\n200\n"+v["response_payload"]) != v["response_signature"] {
		t.Fatal("response signature differs")
	}
	grantBytes, e := base64.RawURLEncoding.DecodeString(v["grant"])
	if e != nil {
		t.Fatal(e)
	}
	var grant watchGrant
	if json.Unmarshal(grantBytes, &grant) != nil || grant.Owner != watchOwner(parent) || !grant.WatchIdentity.valid() {
		t.Fatal("grant schema differs")
	}
}

func TestWatchRevokedDuringPCMStartupNeverSendsReady(t *testing.T) {
	g, b, parent, key, id := watchSetup(t)
	grant, _ := g.issueWatch(watchOwner(parent), id, b.state.Instance)
	listener, e := net.Listen("tcp", "127.0.0.1:7580")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	started := make(chan struct{})
	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		bufio.NewReader(c).ReadString('\n')
		close(started)
		io.Copy(io.Discard, c)
	}()
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { g.ServePCM(context.Background(), server); close(done) }()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	io.WriteString(client, watchPreface(key, grant, "watch-startup-revoke"))
	<-started
	if e = g.Remove(base64.RawURLEncoding.EncodeToString(parent)); e != nil {
		t.Fatal(e)
	}
	buf := make([]byte, 11)
	n, _ := client.Read(buf)
	if n != 0 {
		t.Fatal("revoked startup produced READY")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("startup was not canceled")
	}
	<-backendDone
}
