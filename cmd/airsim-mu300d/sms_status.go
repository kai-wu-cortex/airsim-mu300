package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// A separate, read-only LuCI diagnostic for the phone's inbox path. Never
// expose the inbox, native output, sender, body, or arbitrary Agent errors.
func (d *daemon) smsStatus(w http.ResponseWriter, r *http.Request) {
	if !pairAdminLocal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8575/api/sms/status", nil)
	resp, err := d.client.Do(req)
	unavailable := func() { write(w, 200, map[string]any{"available": false}) }
	if err != nil {
		unavailable()
		return
	}
	defer resp.Body.Close()
	var status struct {
		Count   *int    `json:"count"`
		Error   *string `json:"last_poll_error"`
		Backend *struct {
			Source      string `json:"source"`
			LastSuccess int64  `json:"last_success_at"`
			Captured    int64  `json:"compat_captured_at"`
		} `json:"backend"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 65537))
	if resp.StatusCode != 200 || decoder.Decode(&status) != nil || decoder.Decode(new(any)) != io.EOF || status.Count == nil || status.Error == nil || *status.Count < 0 || *status.Count > 10000 {
		unavailable()
		return
	}
	source, state := "unknown", "ready"
	var lastSuccess, captured int64
	if b := status.Backend; b != nil {
		switch b.Source {
		case "native", "native_json", "compat", "unavailable":
			source = b.Source
		}
		if b.LastSuccess >= 0 && b.LastSuccess <= time.Now().Unix()+5 {
			lastSuccess = b.LastSuccess
		}
		if b.Captured >= 0 && b.Captured <= time.Now().Unix()+5 {
			captured = b.Captured
		}
		if lastSuccess == 0 {
			state = "waiting"
		}
	}
	if *status.Error != "" {
		state = "error"
	}
	write(w, 200, map[string]any{"available": true, "source": source, "state": state, "pending_count": *status.Count, "last_success_at": lastSuccess, "compat_captured_at": captured})
}
