package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
)

func pairAdminLocal(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		http.Error(w, "loopback only", 403)
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}
func (d *daemon) pairClients(w http.ResponseWriter, r *http.Request) {
	if !pairAdminLocal(w, r) || !method(w, r, "GET") {
		return
	}
	write(w, 200, map[string]any{"ok": true, "clients": d.gateway.Clients(), "activity_scope": "since_service_start"})
}
func (d *daemon) removePairClient(w http.ResponseWriter, r *http.Request) {
	if !pairAdminLocal(w, r) || !method(w, r, "POST") {
		return
	}
	var input struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.ID) != 64 || strings.Trim(input.ID, "0123456789abcdef") != "" {
		write(w, 200, map[string]string{"error": "配对标识无效，请刷新列表"})
		return
	}
	if !d.callMu.TryLock() {
		write(w, 200, map[string]string{"error": "通话操作进行中，请稍后再试"})
		return
	}
	defer d.callMu.Unlock()
	calls := d.callsSnapshot(r.Context())
	if !calls.Available || len(calls.Calls) > 0 || d.bridge.Status()["active"] == true {
		write(w, 200, map[string]string{"error": "请先结束通话；通话状态未知时不能移除配对"})
		return
	}
	if err := d.gateway.RemoveClientIdle(input.ID); err != nil {
		write(w, 200, map[string]string{"error": "未能移除配对，请确认音频会话已结束且设备存储可写"})
		return
	}
	write(w, 200, map[string]any{"ok": true, "id": input.ID})
}
