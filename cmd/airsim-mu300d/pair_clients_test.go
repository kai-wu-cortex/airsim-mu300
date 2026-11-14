package main

import (
	"airsim.local/mu300/internal/gateway"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func pairFixture(t *testing.T, status string) (*daemon, string) {
	t.Helper()
	g, err := gateway.New(filepath.Join(t.TempDir(), "keys.json"), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Add(base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))); err != nil {
		t.Fatal(err)
	}
	d := &daemon{gateway: g, client: &http.Client{Transport: callTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/api/calls/status" {
			t.Fatal("unexpected operation", r.Method, r.URL.Path)
		}
		return callResponse(200, status), nil
	})}}
	return d, g.Clients()[0].ID
}
func pairRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/pair/remove", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	return r
}
func TestPairRemovalFailsClosedAndDoesNotRestart(t *testing.T) {
	for _, status := range []string{`{}`, `broken`, `{"active":null,"last_poll_error":"timeout"}`, `{"active":{"id":"mu300-call"}}`} {
		d, id := pairFixture(t, status)
		w := httptest.NewRecorder()
		d.removePairClient(w, pairRequest("POST", `{"id":"`+id+`"}`))
		if !strings.Contains(w.Body.String(), `"error"`) || len(d.gateway.Clients()) != 1 {
			t.Fatal("unknown or active state allowed removal", w.Body.String())
		}
	}
	d, id := pairFixture(t, `{"active":null,"calls":[]}`)
	w := httptest.NewRecorder()
	d.removePairClient(w, pairRequest("POST", `{"id":"`+id+`"}`))
	if !strings.Contains(w.Body.String(), `"ok":true`) || len(d.gateway.Clients()) != 0 {
		t.Fatal(w.Body.String())
	}
}
func TestPairAdminBoundary(t *testing.T) {
	d, id := pairFixture(t, `{"active":null}`)
	for _, body := range []string{`{"id":"0"}`, `{"id":"` + id + `","key":"bad"}`, `{"id":"` + id + `"} {}`} {
		w := httptest.NewRecorder()
		d.removePairClient(w, pairRequest("POST", body))
		if !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatal(w.Body.String())
		}
	}
	r := pairRequest("POST", `{"id":"`+id+`"}`)
	r.RemoteAddr = "192.0.2.3:1234"
	w := httptest.NewRecorder()
	d.removePairClient(w, r)
	if w.Code != 403 {
		t.Fatal("public removal")
	}
	w = httptest.NewRecorder()
	d.pairClients(w, pairRequest("GET", ""))
	if !strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), `"key"`) {
		t.Fatal(w.Body.String())
	}
	if len(d.gateway.Clients()) != 1 {
		t.Fatal("invalid request mutated keys")
	}
}
