package main

import (
	"airsim.local/mu300/internal/pairing"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDirectEncryptedPairingProofAndLocalIsolation(t *testing.T) {
	d := pairingFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("local pairing touched Agent/Relay configuration")
		return nil, nil
	})
	d.settings = defaultSettings()
	d.directHosts = map[string]bool{"100.64.0.2": true}
	s, _ := pairing.New(time.Now())
	d.session = s
	reg := map[string]any{"local_mode": true, "vowlan_secret": pairingRegistrationFixture(1)["vowlan_secret"]}
	raw := encryptedPairingClaim(t, s, s.Code, reg)
	var claim pairing.Claim
	json.Unmarshal(raw, &claim)
	claim.WantDirect = true
	raw, _ = json.Marshal(claim)
	req := httptest.NewRequest("POST", "/pair/v1/claim", bytes.NewReader(raw))
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("100.64.0.2"), Port: d.settings.PairPort}))
	req.RemoteAddr = "100.64.0.3:12345"
	req.Host = "evil.example:9999"
	w := httptest.NewRecorder()
	privatePairing(http.HandlerFunc(d.claim)).ServeHTTP(w, req)
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got["configured"] != true || got["control_port"] != float64(8581) || got["pcm_port"] != float64(8582) {
		t.Fatalf("unexpected result: %d %s", w.Code, w.Body)
	}
	key, _ := base64.RawURLEncoding.DecodeString(reg["vowlan_secret"].(string))
	canonical := fmt.Sprintf("AIRSIM-DIRECT1\n%s\n%s\nmu300-test\n8581\n8582", s.ID, claim.ClientPublic)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(canonical))
	if got["route_proof"] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("cross-client proof mismatch")
	}
	if d.gateway.Diagnostics()["paired_clients"] != 1 || d.activeSession() != nil {
		t.Fatal("pairing not persisted/consumed")
	}
	w = httptest.NewRecorder()
	d.claim(w, req)
	if w.Code != 410 {
		t.Fatal("replay accepted")
	}
}

func TestPairNetworkBoundaries(t *testing.T) {
	for _, tc := range []struct {
		ip string
		ok bool
	}{{"100.64.0.1", true}, {"100.127.255.254", true}, {"100.63.255.255", false}, {"100.128.0.1", false}, {"192.168.1.3", true}, {"10.0.0.2", true}, {"172.31.1.1", true}, {"169.254.1.1", true}, {"8.8.8.8", false}, {"127.0.0.1", false}, {"::1", false}} {
		req := httptest.NewRequest("GET", "/pair/v1/info", nil)
		req.RemoteAddr = net.JoinHostPort(tc.ip, "1234")
		req.Header.Set("X-Forwarded-For", "100.64.0.1")
		w := httptest.NewRecorder()
		privatePairing(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, req)
		if (w.Code == 204) != tc.ok {
			t.Fatalf("%s: %d", tc.ip, w.Code)
		}
	}
}

func TestDirectListenersRollbackOnConflict(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	free, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	s := defaultSettings()
	s.PairPort = port
	s.ControlPort = occupied.Addr().(*net.TCPAddr).Port
	if l, err := bindDirectListeners([]string{"127.0.0.1"}, s); err == nil {
		l.close()
		t.Fatal("conflict accepted")
	}
	again, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
	if err != nil {
		t.Fatal("partial listener leaked", err)
	}
	again.Close()
}

func TestTunnelSettingsValidation(t *testing.T) {
	for _, name := range []string{"", "tailscale0", "lo", "br-lan", "bad;command"} {
		s := defaultSettings()
		s.TunnelInterface = name
		if (s.validate() == nil) != (name == "" || name == "tailscale0") {
			t.Fatal(name)
		}
	}
}

func TestDisabledTunnelRemainsCompatibleWithOldSettings(t *testing.T) {
	b, err := json.Marshal(defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("tunnel_interface")) {
		t.Fatal("disabled setting breaks legacy config decoder")
	}
	s := defaultSettings()
	s.TunnelInterface = "tailscale0"
	if err := writeSettingsFile(t.TempDir(), s); err != nil {
		t.Fatal(err)
	}
}
