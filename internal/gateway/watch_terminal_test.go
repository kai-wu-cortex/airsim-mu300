package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// Fake only the loopback Agent boundary; run the real signed HTTP dispatcher.
type terminalWatchBackend struct {
	id    WatchIdentity
	state string
	err   error
}

func (b *terminalWatchBackend) Action(_ context.Context, _, _ string, id WatchIdentity) (WatchState, error) {
	if id != b.id {
		return WatchState{}, errors.New("unknown tuple")
	}
	return WatchState{State: b.state, Instance: "immutable-ended-instance", Owned: true}, b.err
}
func TestWatchTerminalProofRetainsExactTupleAndDoesNotIssueMedia(t *testing.T) {
	g, _, _, key, id := watchSetup(t)
	b := &terminalWatchBackend{id: id, state: "ended"}
	g.SetWatchBackend(b)
	for i, action := range []string{"status", "hangup", "status"} {
		nonce := fmt.Sprintf("terminal-%d", i)
		w := watchRequest(g, key, id, "/api/watch/"+action, nonce)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var envelope map[string]string
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope["proof"] != Sign(key, "watch-response-v2\n"+nonce+"\n200\n"+envelope["payload"]) {
			t.Fatal("invalid terminal proof")
		}
		raw, e := base64.RawURLEncoding.DecodeString(envelope["payload"])
		if e != nil {
			t.Fatal(e)
		}
		var payload struct {
			WatchIdentity
			Version      int
			State, Grant string
		}
		if json.Unmarshal(raw, &payload) != nil || payload.WatchIdentity != id || payload.Version != 2 || payload.State != "ended" || payload.Grant != "" {
			t.Fatal("terminal tuple/grant mismatch", string(raw))
		}
	}
	if len(g.watchLeases) != 0 {
		t.Fatal("terminal issued media")
	}
	wrong := id
	wrong.Generation++
	for i, state := range []string{"unknown", "ended"} {
		b.state = state
		if state == "ended" {
			b.err = errors.New("sample failed")
		}
		w := watchRequest(g, key, id, "/api/watch/status", fmt.Sprintf("unknown-%d", i))
		var envelope map[string]string
		json.Unmarshal(w.Body.Bytes(), &envelope)
		raw, _ := base64.RawURLEncoding.DecodeString(envelope["payload"])
		var payload struct{ State string }
		json.Unmarshal(raw, &payload)
		if payload.State != "unknown" {
			t.Fatal("unknown/error signed as ended", string(raw))
		}
	}
	b.err = nil
	w := watchRequest(g, key, wrong, "/api/watch/status", "wrong-generation")
	if w.Code != 409 {
		t.Fatal("another generation accepted", w.Code)
	}
}
