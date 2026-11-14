package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientActivityAndFingerprintPrivacy(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "keys.json"), "fixture")
	key := []byte(strings.Repeat("a", 32))
	encoded := base64.RawURLEncoding.EncodeToString(key)
	if err := g.Add(encoded); err != nil {
		t.Fatal(err)
	}
	before := g.Clients()
	if len(before) != 1 || before[0].LastUsed != 0 || before[0].Recent {
		t.Fatal(before)
	}
	ts := fmt.Sprint(time.Now().Unix())
	nonce := "activity-fixture"
	sig := Sign(key, canonical("GET", "/v1/health", nil, ts, nonce))
	if g.authenticate("GET", "/v1/health", nil, ts, nonce, "bad") != nil || g.Clients()[0].LastUsed != 0 {
		t.Fatal("invalid request updated activity")
	}
	if got := g.authenticate("GET", "/v1/health", nil, ts, nonce, sig); got == nil {
		t.Fatal("valid auth rejected")
	}
	clients := g.Clients()
	if !clients[0].Recent || clients[0].LastUsed == 0 || clients[0].ID != watchOwner(key) {
		t.Fatal(clients)
	}
	raw, _ := json.Marshal(clients)
	if strings.Contains(string(raw), encoded) || strings.Contains(string(raw), encoded[len(encoded)-8:]) {
		t.Fatal("key leaked")
	}
	g.mu.Lock()
	g.lastUsed[clients[0].ID] = time.Now().Unix() - 121
	g.mu.Unlock()
	if g.Clients()[0].Recent {
		t.Fatal("old activity shown as recent")
	}
	reloaded, err := New(g.path, "fixture")
	if err != nil || reloaded.Clients()[0].LastUsed != 0 {
		t.Fatal("activity scope is not process lifetime")
	}
}
func TestRemoveClientIdlePersistsAndRevokesOnlySelected(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "keys.json"), "fixture")
	first := []byte(strings.Repeat("a", 32))
	second := []byte(strings.Repeat("b", 32))
	for _, k := range [][]byte{first, second} {
		if err := g.Add(base64.RawURLEncoding.EncodeToString(k)); err != nil {
			t.Fatal(err)
		}
	}
	owner := watchOwner(first)
	g.watchLeases = map[string]*watchLease{"fixture": {grant: watchGrant{Owner: owner}}}
	g.pcm = true
	if err := g.RemoveClientIdle(owner); err == nil || len(g.Clients()) != 2 {
		t.Fatal("active PCM removal")
	}
	g.pcm = false
	g.earlyStreamDone = make(chan struct{})
	if err := g.RemoveClientIdle(owner); err == nil {
		t.Fatal("early media removal")
	}
	g.earlyStreamDone = nil
	if err := g.RemoveClientIdle(owner); err != nil {
		t.Fatal(err)
	}
	if len(g.watchLeases) != 0 || len(g.Clients()) != 1 || g.Clients()[0].ID != watchOwner(second) {
		t.Fatal("wrong client removed")
	}
	if err := g.RemoveClientIdle(owner); err != nil {
		t.Fatal("repeat removal is not harmless")
	}
	restored, err := New(g.path, "fixture")
	if err != nil || len(restored.Clients()) != 1 {
		t.Fatal("removal not durable")
	}
	ts := fmt.Sprint(time.Now().Unix())
	nonce := "removed-key-fixture"
	if g.authenticate("GET", "/v1/health", nil, ts, nonce, Sign(first, canonical("GET", "/v1/health", nil, ts, nonce))) != nil {
		t.Fatal("removed parent authenticates")
	}
}
func TestClientRemovalStorageFailureAndConcurrentAccess(t *testing.T) {
	root := t.TempDir()
	g, _ := New(filepath.Join(root, "keys.json"), "fixture")
	key := []byte(strings.Repeat("a", 32))
	encoded := base64.RawURLEncoding.EncodeToString(key)
	g.Add(encoded)
	if err := os.Mkdir(g.path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if g.RemoveClientIdle(watchOwner(key)) == nil || len(g.Clients()) != 1 {
		t.Fatal("failed persistence revoked key")
	}
	os.Remove(g.path + ".tmp")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.Add(encoded)
			g.Clients()
			ts := fmt.Sprint(time.Now().Unix())
			n := fmt.Sprintf("parallel-%d", i)
			g.authenticate("GET", "/v1/health", nil, ts, n, Sign(key, canonical("GET", "/v1/health", nil, ts, n)))
		}(i)
	}
	wg.Wait()
	if len(g.Clients()) != 1 {
		t.Fatal("duplicate pair record")
	}
}

func TestRemovedClientCannotReserveAudioAfterAuthentication(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "keys.json"), "fixture")
	key := []byte(strings.Repeat("a", 32))
	g.Add(base64.RawURLEncoding.EncodeToString(key))
	owner := watchOwner(key)
	ts := fmt.Sprint(time.Now().Unix())
	nonce := "delayed-handshake"
	if g.authenticate("PCM", "/v1/pcm", nil, ts, nonce, Sign(key, canonical("PCM", "/v1/pcm", nil, ts, nonce))) == nil {
		t.Fatal("fixture auth")
	}
	g.earlyLeases = map[string]*earlyLease{"fixture": {owner: "wlan:" + owner}}
	if err := g.RemoveClientIdle(owner); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	admitted := g.reserveClientPCMLocked(owner)
	g.mu.Unlock()
	if admitted || len(g.earlyLeases) != 0 {
		t.Fatal("revoked handshake can start audio or retain grant")
	}
}
