package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

// Webhooks are opt-in and receive call events only. They never control calls or
// relay audio. A configured URL may contain a secret, so it is never logged.
type notificationConfig struct {
	Enabled       bool   `json:"enabled"`
	Provider      string `json:"provider"`
	URL           string `json:"url"`
	Secret        string `json:"secret,omitempty"`
	ChatID        string `json:"chat_id,omitempty"`
	IncludeNumber bool   `json:"include_number"`
}

func decodeNotification(b []byte) (notificationConfig, error) {
	var c notificationConfig
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if len(b) > 8192 || d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, errors.New("通知设置格式无效")
	}
	switch c.Provider {
	case "", "webhook", "bark", "telegram", "pushplus", "serverchan", "wecom", "dingtalk":
	default:
		return c, errors.New("通知渠道不支持")
	}
	if len(c.Secret) > 512 || len(c.ChatID) > 128 {
		return c, errors.New("通知设置过长")
	}
	if c.URL != "" {
		u, e := url.Parse(c.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(c.URL) > 2048 {
			return c, errors.New("通知地址须为不含用户名、密码或片段的 HTTPS 地址")
		}
	}
	if c.Enabled && (c.URL == "" || c.Provider == "" || (c.Provider == "telegram" && c.ChatID == "") || (c.Provider == "pushplus" && c.Secret == "")) {
		return c, errors.New("启用前请填写完整通知渠道")
	}
	return c, nil
}
func (a *deviceAdmin) notificationStatus() (map[string]any, error) {
	c := notificationConfig{Provider: "webhook"}
	if b, e := limitedFile(a.path(filepath.Join(a.data, "notifications.json")), 8192); e == nil {
		var err error
		c, err = decodeNotification(b)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"enabled": c.Enabled, "provider": c.Provider, "has_url": c.URL != "", "has_secret": c.Secret != "", "chat_id": c.ChatID, "include_number": c.IncludeNumber}, nil
}
func (a *deviceAdmin) notificationSave(payload string) (map[string]any, error) {
	// Empty URL/secret retains the current secret only for the same provider.
	var incoming notificationConfig
	if len(payload) > 8192 || json.Unmarshal([]byte(payload), &incoming) != nil {
		return nil, errors.New("通知设置无效")
	}
	if b, e := limitedFile(a.path(filepath.Join(a.data, "notifications.json")), 8192); e == nil {
		old, err := decodeNotification(b)
		if err != nil {
			return nil, err
		}
		if incoming.Provider == old.Provider {
			if incoming.URL == "" {
				incoming.URL = old.URL
			}
			if incoming.Secret == "" {
				incoming.Secret = old.Secret
			}
		}
	}
	// Validate original shape as well, to reject unknown fields.
	var shape map[string]json.RawMessage
	_ = json.Unmarshal([]byte(payload), &shape)
	for k := range shape {
		if k != "enabled" && k != "provider" && k != "url" && k != "secret" && k != "chat_id" && k != "include_number" {
			return nil, errors.New("未知通知字段")
		}
	}
	raw, _ := json.Marshal(incoming)
	if _, e := decodeNotification(raw); e != nil {
		return nil, e
	}
	if e := a.write(filepath.Join(a.data, "notifications.json"), raw); e != nil {
		return nil, e
	}
	return map[string]any{"message": "已保存。手动启动 AirSIM 服务后生效；未发送测试通知。"}, nil
}

type callNotification struct {
	Event  string `json:"event"`
	ID     string `json:"call_id"`
	Number string `json:"number,omitempty"`
	Time   string `json:"time"`
}
type notificationTracker struct {
	initialized bool
	previous    map[string]uiCall
	misses      map[string]int
	connected   map[string]bool
}

func (t *notificationTracker) observe(s uiCalls) []callNotification {
	if !s.Available {
		return nil
	}
	now := map[string]uiCall{}
	for _, c := range s.Calls {
		now[c.ID] = c
	}
	if !t.initialized {
		t.initialized = true
		t.previous = now
		t.misses = map[string]int{}
		t.connected = map[string]bool{}
		for id, c := range now {
			t.connected[id] = c.State == "active" || c.State == "held"
		}
		return nil
	}
	events := []callNotification{}
	emit := func(event string, c uiCall) {
		events = append(events, callNotification{event, c.ID, c.Number, time.Now().UTC().Format(time.RFC3339)})
	}
	for id, c := range now {
		_, exists := t.previous[id]
		delete(t.misses, id)
		if !exists {
			if c.Direction == "incoming" {
				emit("ringing", c)
			} else {
				emit("dialing", c)
			}
		}
		if (c.State == "active" || c.State == "held") && !t.connected[id] {
			t.connected[id] = true
			emit("answered", c)
		}
		t.previous[id] = c
	}
	for id, c := range t.previous {
		if _, exists := now[id]; exists {
			continue
		}
		t.misses[id]++
		if t.misses[id] < 2 {
			continue
		}
		event := "ended"
		if c.Direction == "incoming" && !t.connected[id] {
			event = "missed"
		}
		emit(event, c)
		delete(t.previous, id)
		delete(t.misses, id)
		delete(t.connected, id)
	}
	return events
}
func notificationBody(c notificationConfig, e callNotification) ([]byte, string) {
	title := "AirSIM · " + e.Event
	content := e.Time
	if c.IncludeNumber && e.Number != "" {
		content += "\n" + e.Number
	} else {
		e.Number = ""
	}
	var body any
	switch c.Provider {
	case "bark":
		body = map[string]string{"title": title, "body": content}
	case "telegram":
		body = map[string]string{"chat_id": c.ChatID, "text": title + "\n" + content}
	case "pushplus":
		body = map[string]string{"token": c.Secret, "title": title, "content": content, "template": "txt"}
	case "serverchan":
		return []byte(url.Values{"title": {title}, "desp": {content}}.Encode()), "application/x-www-form-urlencoded"
	case "wecom", "dingtalk":
		body = map[string]any{"msgtype": "text", "text": map[string]string{"content": title + "\n" + content}}
	default:
		body = e
	}
	b, _ := json.Marshal(body)
	return b, "application/json"
}
func publicNotificationIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !inNotificationSpecialRange(ip)
}
func inNotificationSpecialRange(ip net.IP) bool {
	for _, s := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "0.0.0.0/8", "240.0.0.0/4"} {
		_, n, _ := net.ParseCIDR(s)
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
func notificationHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("notification DNS failed")
		}
		for _, ip := range ips {
			if !publicNotificationIP(ip.IP) {
				return nil, errors.New("notification destination is not public")
			}
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}}
}
func sendNotification(ctx context.Context, client *http.Client, c notificationConfig, event callNotification) bool {
	body, ct := notificationBody(c, event)
	req, e := http.NewRequestWithContext(ctx, "POST", c.URL, bytes.NewReader(body))
	if e != nil {
		return false
	}
	req.Header.Set("Content-Type", ct)
	resp, e := client.Do(req)
	if e != nil {
		return false
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if e != nil || len(raw) > 8192 || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	// Most providers use HTTP 200 for an application-level failure.
	var status map[string]any
	_ = json.Unmarshal(raw, &status)
	switch c.Provider {
	case "telegram":
		return truth(status["ok"])
	case "bark", "pushplus":
		return status["code"] == float64(200)
	case "wecom", "dingtalk":
		return status["errcode"] == float64(0)
	case "serverchan":
		return status["code"] == float64(0)
	}
	return true
}
func (d *daemon) notificationLoop(ctx context.Context, data string) {
	b, e := limitedFile(filepath.Join(data, "notifications.json"), 8192)
	if e != nil {
		return
	}
	c, e := decodeNotification(b)
	if e != nil || !c.Enabled {
		return
	}
	client := notificationHTTPClient()
	defer client.CloseIdleConnections()
	queue := make(chan callNotification, 20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-queue:
				if ctx.Err() != nil {
					return
				}
				ok := sendNotification(ctx, client, c, event)
				// Only an event kind and outcome are stored; no URL, number or response body.
				raw, _ := json.Marshal(map[string]any{"event": event.Event, "delivered": ok, "at": time.Now().UTC().Format(time.RFC3339)})
				a := deviceAdmin{root: "/", data: data}
				_ = a.write(filepath.Join(data, "notification-last.json"), raw)
			}
		}
	}()
	defer func() { <-done }()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	tracker := notificationTracker{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, event := range tracker.observe(d.callsSnapshot(ctx)) {
				select {
				case queue <- event:
				default: /* bounded best-effort, never retry a delivered/unknown event */
				}
			}
		}
	}
}
