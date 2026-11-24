package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPhoneSMSStatusIsReadOnlyAndRedacted(t *testing.T) {
	calls := 0
	d := &daemon{client: &http.Client{Transport: callTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != "http://127.0.0.1:8575/api/sms/status" {
			t.Fatal("unexpected Agent operation")
		}
		return callResponse(200, `{"count":1,"last_poll_error":"private fixture","body":"private fixture","backend":{"source":"compat","last_success_at":1}}`), nil
	})}}
	for _, tc := range []struct {
		method, addr string
		code         int
	}{{"GET", "192.0.2.1:1234", 403}, {"POST", "127.0.0.1:1234", 405}, {"GET", "127.0.0.1:1234", 200}} {
		r := httptest.NewRequest(tc.method, "/sms/status", nil)
		r.RemoteAddr = tc.addr
		w := httptest.NewRecorder()
		d.smsStatus(w, r)
		if w.Code != tc.code || strings.Contains(w.Body.String(), "private fixture") {
			t.Fatal("status access or privacy failure")
		}
		if tc.code == 200 && (!strings.Contains(w.Body.String(), `"state":"error"`) || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("missing failure status")
		}
	}
	if calls != 1 {
		t.Fatal("denied requests reached Agent")
	}
}

func TestPhoneSMSStatusNeverTreatsMalformedResponseAsEmptyInbox(t *testing.T) {
	for _, body := range []string{`{}`, `{"count":0}`, `{"count":0,"last_poll_error":null}`, `{"count":-1,"last_poll_error":""}`, `{"count":0,"last_poll_error":""} {}`} {
		d := &daemon{client: &http.Client{Transport: callTransport(func(*http.Request) (*http.Response, error) { return callResponse(200, body), nil })}}
		r := httptest.NewRequest("GET", "/sms/status", nil)
		r.RemoteAddr = "127.0.0.1:1"
		w := httptest.NewRecorder()
		d.smsStatus(w, r)
		if !strings.Contains(w.Body.String(), `"available":false`) {
			t.Fatal("malformed status appeared healthy")
		}
	}
}
