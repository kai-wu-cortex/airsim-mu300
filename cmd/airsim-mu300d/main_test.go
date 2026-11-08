package main

import (
	"airsim.local/mu300/internal/pairing"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCurrentIOSRegistrationAdapter(t *testing.T) {
	var reg map[string]any
	if e := json.Unmarshal([]byte(`{"device_id":"fixture-device","device_secret":"fixture-secret","voip_token":"fixture-token","relay_url":"https://example.test","vowlan_secret":"local-gateway-secret","notification_forwarding":{"enabled":true}}`), &reg); e != nil {
		t.Fatal(e)
	}
	prepareRegistration(reg, "mu300-fixture")
	for _, key := range []string{"vowlan_secret", "notification_forwarding"} {
		if _, ok := reg[key]; ok {
			t.Fatalf("client-only field forwarded: %s", key)
		}
	}
	for key, want := range map[string]string{"device_id": "fixture-device", "device_secret": "fixture-secret", "voip_token": "fixture-token", "relay_url": "https://example.test", "android_agent_id": "mu300-fixture", "android_agent_kind": "mu300"} {
		if reg[key] != want {
			t.Fatalf("registration field changed: %s", key)
		}
	}
}

func TestPairInfoDoesNotRevealCode(t *testing.T) {
	s, _ := pairing.New(time.Now())
	d := &daemon{session: s}
	w := httptest.NewRecorder()
	d.pairInfo(w, httptest.NewRequest("GET", "/pair/v1/info", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), `"code"`) {
		t.Fatal("pairing code exposed")
	}
}
func TestManagementMethods(t *testing.T) {
	d := &daemon{}
	w := httptest.NewRecorder()
	d.startPair(w, httptest.NewRequest(http.MethodGet, "/pair/start", nil))
	if w.Code != 405 {
		t.Fatal("GET can open pairing")
	}
}
func TestDirectClaimRefusedBeforeConsumingSession(t *testing.T) {
	s, _ := pairing.New(time.Now())
	d := &daemon{session: s}
	w := httptest.NewRecorder()
	d.claim(w, httptest.NewRequest("POST", "/pair/v1/claim", strings.NewReader(`{"want_direct_route":true}`)))
	if w.Code != 503 {
		t.Fatalf("%d", w.Code)
	}
	if d.session != s {
		t.Fatal("unavailable route destroyed pairing")
	}
}
