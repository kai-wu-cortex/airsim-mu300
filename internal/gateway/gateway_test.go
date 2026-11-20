package gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestSignedHealthIncludesLiveSIMNumber(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "clients.json"), "mu300-test")
	key := []byte(strings.Repeat("s", 32))
	if err := g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://127.0.0.1:8575/api/health" {
			t.Fatal("unexpected backend")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"canonical_call_ids":true,"runtime_profile":"mu300-openwrt","phone_number":"15550001111"}`))}, nil
	})}
	w := signed(g, "/v1/health", "number-check", key)
	var health map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || health["phone_number"] != "15550001111" || health["canonical_call_ids"] != true || health["identity_proof"] == "" {
		t.Fatal("authenticated SIM metadata missing")
	}
}

// Sign independently of the implementation, using the existing AirSIM contract.
func signed(g *Gateway, path, nonce string, secret []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	ts := fmt.Sprint(time.Now().Unix())
	hash := sha256.Sum256(nil)
	m := hmac.New(sha256.New, secret)
	fmt.Fprintf(m, "GET\n%s\n%x\n%s\n%s", path, hash, ts, nonce)
	r.Header.Set("X-AirSIM-VoWLAN-Version", "1")
	r.Header.Set("X-AirSIM-VoWLAN-Timestamp", ts)
	r.Header.Set("X-AirSIM-VoWLAN-Nonce", nonce)
	r.Header.Set("X-AirSIM-VoWLAN-Signature", base64.RawURLEncoding.EncodeToString(m.Sum(nil)))
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}

func TestOwnedDialProofMatchesClientProtocol(t *testing.T) {
	key := []byte(strings.Repeat("s", 32))
	output, err := signedDialReceipt([]byte(`{"operation_id":"op-1"}`), []byte(`{"operation_id":"op-1","android_id":"mu300-call-1","backend_id":"mu300-call-1"}`), "nonce-proof", key)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]string
	if err = json.Unmarshal(output, &receipt); err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("AIRSIM-DIAL1\nnonce-proof\nop-1\nmu300-call-1\nmu300-call-1"))
	if receipt["operation_proof"] != base64.RawURLEncoding.EncodeToString(m.Sum(nil)) {
		t.Fatal("iPhone cannot verify receipt")
	}
	for _, response := range []string{`{"operation_id":"other","android_id":"a","backend_id":"b"}`, `{"operation_id":"op-1","android_id":"a"}`, `{"operation_id":"op-1","android_id":"a\nb","backend_id":"b"}`} {
		if _, err = signedDialReceipt([]byte(`{"operation_id":"op-1"}`), []byte(response), "nonce-proof", key); err == nil {
			t.Fatal("unproven identity signed")
		}
	}
}
func TestHealthIdentityPersistsAndRejectsReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	g, err := New(path, "mu300-test")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("s", 32))
	if err = g.Add(base64.RawURLEncoding.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	g, err = New(path, "mu300-test")
	if err != nil {
		t.Fatal(err)
	}
	w := signed(g, "/v1/health", "nonce-test-1", key)
	m := hmac.New(sha256.New, key)
	m.Write([]byte("AIRSIM-HEALTH1\nnonce-test-1\nmu300-test"))
	proof := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), proof) || !strings.Contains(w.Body.String(), `"canonical_call_ids":false`) {
		t.Fatalf("health %d %s", w.Code, w.Body.String())
	}
	if signed(g, "/v1/health", "nonce-test-1", key).Code != 401 {
		t.Fatal("replay allowed")
	}
	if signed(g, "/v1/health", "nonce-test-2", []byte(strings.Repeat("x", 32))).Code != 401 {
		t.Fatal("foreign key allowed")
	}
	if signed(g, "/api/at", "nonce-test-3", key).Code != 404 {
		t.Fatal("raw AT exposed")
	}
	mode, _ := os.Stat(path)
	if mode.Mode().Perm() != 0600 {
		t.Fatal("secret file permissions")
	}
}
func TestSecretValidationAndUnlimitedSavedClients(t *testing.T) {
	g, _ := New(filepath.Join(t.TempDir(), "clients.json"), "test")
	for _, key := range []string{"", "bad!", base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if g.Add(key) == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for i := 0; i < 64; i++ {
		key := sha256.Sum256([]byte(fmt.Sprint(i)))
		if err := g.Add(base64.RawURLEncoding.EncodeToString(key[:])); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := New(g.path, "test")
	if err != nil || len(reloaded.Clients()) != 64 {
		t.Fatal("saved pairings above the old limit did not reload", err)
	}
	for i := 0; i < 64; i++ {
		key := sha256.Sum256([]byte(fmt.Sprint(i)))
		if err := reloaded.Add(base64.RawURLEncoding.EncodeToString(key[:])); err != nil {
			t.Fatal(err)
		}
		ts, nonce := fmt.Sprint(time.Now().Unix()), fmt.Sprintf("saved-client-%d", i)
		if reloaded.authenticate("GET", "/v1/health", nil, ts, nonce, Sign(key[:], canonical("GET", "/v1/health", nil, ts, nonce))) == nil {
			t.Fatalf("saved client %d cannot authenticate", i)
		}
	}
	if len(reloaded.Clients()) != 64 {
		t.Fatal("duplicate keys appended")
	}
	original := reloaded.Clients()
	if err := reloaded.RemoveClientIdle(original[16].ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err = New(g.path, "test")
	if err != nil || len(reloaded.Clients()) != 63 {
		t.Fatal("single removal did not persist", err)
	}
	for _, c := range reloaded.Clients() {
		if c.ID == original[16].ID {
			t.Fatal("removed pairing survived")
		}
	}
	keys := append(append([]string(nil), reloaded.keys...), "invalid-key")
	raw, _ := json.Marshal(keys)
	if err := os.WriteFile(g.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(g.path, "test"); err == nil {
		t.Fatal("invalid durable key accepted")
	}
}
