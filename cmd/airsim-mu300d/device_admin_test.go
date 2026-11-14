package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAdmin(t *testing.T) *deviceAdmin {
	t.Helper()
	return &deviceAdmin{root: t.TempDir(), data: "/data", sleep: func(time.Duration) {}, run: func(context.Context, string, []string, []byte) ([]byte, error) {
		t.Fatal("unexpected device operation")
		return nil, nil
	}}
}
func putAdmin(t *testing.T, a *deviceAdmin, path, value string) {
	t.Helper()
	writeKernelFixture(t, a.root, path, []byte(value))
}
func TestMaintenanceIdleFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		clcc, job string
		audio     bool
		ok        bool
	}{{"OK", `{"none":true}`, false, true}, {"OK", `{"done":true}`, false, true}, {"+CLCC: 1\nOK", `{"none":true}`, false, false}, {"OK", `{}`, false, false}, {"ERROR", `{"none":true}`, false, false}, {"", `{"none":true}`, false, false}, {"OK", `{"running":true}`, false, false}, {"OK", `{"none":true}`, true, false}} {
		a := testAdmin(t)
		a.run = func(_ context.Context, name string, args []string, _ []byte) ([]byte, error) {
			switch name {
			case "mu300-at":
				return []byte(tc.clcc), nil
			case "pidof":
				if tc.audio {
					return []byte("123"), nil
				}
				return nil, errors.New("not running")
			case "ubus":
				return []byte(tc.job), nil
			}
			t.Fatal(name)
			return nil, nil
		}
		if got := a.idle(); (got == nil) != tc.ok {
			t.Errorf("idle mismatch %v: %v", tc, got)
		}
	}
}
func TestMaintenanceBoundaryAndSafeFiles(t *testing.T) {
	for action := range deviceWriteActions {
		if deviceReadActions[action] {
			t.Fatal("write leaked into read ACL", action)
		}
	}
	a := testAdmin(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, a.path("/data")); err != nil {
		t.Fatal(err)
	}
	if a.write("/data/notifications.json", []byte("secret")) == nil {
		t.Fatal("linked directory accepted")
	}
	p := filepath.Join(outside, "value")
	os.WriteFile(p, []byte("secret"), 0600)
	os.Symlink(p, a.path("/linked"))
	if _, e := limitedFile(a.path("/linked"), 100); e == nil {
		t.Fatal("linked file accepted")
	}
}
func TestBackupHardwareBindingAndCredentialValidation(t *testing.T) {
	a := testAdmin(t)
	putAdmin(t, a, "/sys/block/mmcblk0/device/cid", strings.Repeat("a", 32))
	putAdmin(t, a, "/proc/device-tree/serial-number", "synthetic-device")
	putAdmin(t, a, "/data/agent-id", "mu300-synthetic")
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	keys, _ := json.Marshal([]string{key})
	putAdmin(t, a, "/data/vowlan-clients.json", string(keys))
	exported, e := a.backupExport()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := base64.StdEncoding.DecodeString(exported["payload"].(string))
	var b deviceBackup
	json.Unmarshal(raw, &b)
	if len(b.Files) != 2 {
		t.Fatal("backup scope", b.Files)
	}
	putAdmin(t, a, "/data/agent-id", "mu300-changed")
	if _, e = a.backupRestore(exported["payload"].(string)); e == nil {
		t.Fatal("mixed identity accepted")
	}
	putAdmin(t, a, "/data/agent-id", "mu300-synthetic")
	putAdmin(t, a, "/sys/block/mmcblk0/device/cid", strings.Repeat("b", 32))
	if _, e = a.backupRestore(exported["payload"].(string)); e == nil {
		t.Fatal("other hardware accepted")
	}
	putAdmin(t, a, "/sys/block/mmcblk0/device/cid", strings.Repeat("a", 32))
	if _, e = a.backupRestore(exported["payload"].(string)); e != nil {
		t.Fatal(e)
	}
	b.Files["../../escape"] = []byte("no")
	if validateDeviceBackup(b) == nil {
		t.Fatal("path escaped")
	}
	delete(b.Files, "../../escape")
	delete(b.Files, "agent-id")
	if validateDeviceBackup(b) == nil {
		t.Fatal("keys without identity accepted")
	}
}
func TestSMSBatchSnapshotAndNoRetry(t *testing.T) {
	a := testAdmin(t)
	pool := "/etc/mu300/sms/msg"
	putAdmin(t, a, pool+"/1", "status: unread\n\nsynthetic message\n")
	putAdmin(t, a, pool+"/2", "status: unread\n\nsynthetic two\n")
	token, _, e := a.smsSnapshot()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.smsMaintenance(deviceRequest{Action: "sms_mark_all", Token: token}); e != nil {
		t.Fatal(e)
	}
	calls := 0
	a.run = func(_ context.Context, name string, args []string, _ []byte) ([]byte, error) {
		calls++
		if name != "ubus" || args[2] != "sms_delete" {
			t.Fatal(args)
		}
		os.Remove(a.path(pool + "/1"))
		putAdmin(t, a, pool+"/3", "new arrival")
		return []byte(`{"ok":1}`), nil
	}
	if _, e = a.smsMaintenance(deviceRequest{Action: "sms_clear", Token: token}); e == nil {
		t.Fatal("arrival did not halt batch")
	}
	if calls != 1 {
		t.Fatal("delete retried", calls)
	}
	if _, e = a.smsMaintenance(deviceRequest{Action: "sms_clear", Token: token}); e == nil || calls != 1 {
		t.Fatal("stale preview was reused")
	}
}
func TestSMSReadMarksDoNotFollowReusedID(t *testing.T) {
	a := testAdmin(t)
	pool := "/etc/mu300/sms-pool/msg"
	putAdmin(t, a, pool+"/1", "status: unread\n\nfirst\n")
	token, _, _ := a.smsSnapshot()
	a.smsMaintenance(deviceRequest{Action: "sms_mark_all", Token: token})
	a.run = func(context.Context, string, []string, []byte) ([]byte, error) {
		return []byte(`{"unread":1,"msgs":[{"id":"1","status":"unread"}]}`), nil
	}
	v, e := a.smsList("1")
	if e != nil || v["msgs"].([]any)[0].(map[string]any)["status"] != "read" {
		t.Fatal(v, e)
	}
	putAdmin(t, a, pool+"/1", "status: unread\n\nsecond\n")
	v, e = a.smsList("1")
	if e != nil || v["msgs"].([]any)[0].(map[string]any)["status"] != "unread" {
		t.Fatal("reused ID was marked read")
	}
}
func TestUnknownKernelCannotInstall(t *testing.T) {
	a := testAdmin(t)
	putAdmin(t, a, "/proc/sys/kernel/osrelease", "7.3.1")
	for _, id := range []string{"reference-7.3", "reference-6.18", "u30-5.4", "../../module"} {
		if _, e := a.audioInstall(id); e == nil {
			t.Fatal("unproven kernel accepted", id)
		}
	}
	r, _ := a.audioCatalog()
	for _, raw := range r["profiles"].([]map[string]any) {
		if raw["installable"] == true {
			t.Fatal("unproven catalog enabled")
		}
	}
}
func TestConfigTransactionRollsBackOnFailedVerification(t *testing.T) {
	a := testAdmin(t)
	putAdmin(t, a, "/etc/config/network", "original")
	commands := 0
	a.run = func(_ context.Context, name string, args []string, input []byte) ([]byte, error) {
		if name != "uci" {
			t.Fatal(name)
		}
		commands++
		if !strings.Contains(string(input), "set network.wan.disabled='1'") {
			t.Fatal(string(input))
		}
		if e := os.WriteFile(filepath.Join(args[1], "network"), []byte("changed"), 0600); e != nil {
			t.Fatal(e)
		}
		return nil, nil
	}
	applies := 0
	_, e := a.configTransaction(map[string]string{"network.wan.disabled": "1"}, nil, func() error { applies++; return nil }, func() error { return errors.New("unreachable") })
	b, _ := os.ReadFile(a.path("/etc/config/network"))
	if e == nil || string(b) != "original" || commands != 1 || applies != 2 {
		t.Fatal("rollback failed", e, string(b), commands, applies)
	}
	if _, e = a.configTransaction(map[string]string{"system.hostname": "bad"}, nil, func() error { t.Fatal("apply"); return nil }, func() error { return nil }); e == nil {
		t.Fatal("escaped config scope")
	}
}
func TestNetworkProfileRejectsSharedUSB(t *testing.T) {
	n := map[string]map[string]any{"lan": {"device": "br-lan", "proto": "static", "ipaddr": "192.0.2.1"}, "bridge": {"type": "bridge", "name": "br-lan", "ports": []any{"usb0"}}, "wan": {"proto": "mu300cell"}, "wwan": {"proto": "dhcp"}}
	w := map[string]map[string]any{"ap0": {"mode": "ap", "device": "radio0", "network": "wlan"}, "wwan0": {"mode": "sta", "device": "radio0", "network": "wwan"}}
	if e := networkProfile(n, w); e != nil {
		t.Fatal(e)
	}
	n["bridge"]["ports"] = []any{"usb0", "wlan0"}
	if networkProfile(n, w) == nil {
		t.Fatal("shared bridge accepted")
	}
	n["bridge"]["ports"] = []any{"usb0"}
	w["wwan0"]["network"] = "lan"
	if networkProfile(n, w) == nil {
		t.Fatal("sta on management network accepted")
	}
}
func TestNotificationTransitionsAndUnknownSnapshots(t *testing.T) {
	tr := notificationTracker{}
	tr.observe(uiCalls{Available: true})
	c := uiCall{ID: "mu300-synthetic", Direction: "incoming", State: "incoming"}
	events := tr.observe(uiCalls{Available: true, Calls: []uiCall{c}})
	if len(events) != 1 || events[0].Event != "ringing" {
		t.Fatal(events)
	}
	if len(tr.observe(uiCalls{})) != 0 {
		t.Fatal("unknown ended call")
	}
	tr.observe(uiCalls{Available: true})
	c.State = "active"
	events = tr.observe(uiCalls{Available: true, Calls: []uiCall{c}})
	if len(events) != 1 || events[0].Event != "answered" {
		t.Fatal(events)
	}
	if len(tr.observe(uiCalls{Available: true})) != 0 {
		t.Fatal("one missing poll ended call")
	}
	events = tr.observe(uiCalls{Available: true})
	if len(events) != 1 || events[0].Event != "ended" {
		t.Fatal(events)
	}
	if len(tr.observe(uiCalls{Available: true})) != 0 {
		t.Fatal("duplicate ended event")
	}
	baseline := notificationTracker{}
	if len(baseline.observe(uiCalls{Available: true, Calls: []uiCall{c}})) != 0 {
		t.Fatal("startup flooded")
	}
	tr.observe(uiCalls{Available: true, Calls: []uiCall{{ID: "mu300-second", State: "incoming", Direction: "incoming"}}})
	tr.observe(uiCalls{Available: true})
	events = tr.observe(uiCalls{Available: true})
	if len(events) != 1 || events[0].Event != "missed" {
		t.Fatal(events)
	}
}

type notificationRoundTrip func(*http.Request) (*http.Response, error)

func (f notificationRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestNotificationPrivacyAndFailureNoRetry(t *testing.T) {
	c := notificationConfig{Enabled: true, Provider: "telegram", URL: "https://example.com/secret", ChatID: "test"}
	event := callNotification{Event: "ringing", Number: "15550001111"}
	body, _ := notificationBody(c, event)
	if bytes.Contains(body, []byte(event.Number)) {
		t.Fatal("number leaked without opt in")
	}
	c.IncludeNumber = true
	body, _ = notificationBody(c, event)
	if !bytes.Contains(body, []byte(event.Number)) {
		t.Fatal("explicit number missing")
	}
	count := 0
	client := &http.Client{Transport: notificationRoundTrip(func(*http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":false}`))}, nil
	})}
	if sendNotification(context.Background(), client, c, event) || count != 1 {
		t.Fatal("provider failure or retry")
	}
	for _, addr := range []string{"127.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.1.1", "::1", "fc00::1"} {
		if publicNotificationIP(net.ParseIP(addr)) {
			t.Fatal("private endpoint allowed", addr)
		}
	}
	for _, url := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com/#secret"} {
		b, _ := json.Marshal(notificationConfig{URL: url})
		if _, e := decodeNotification(b); e == nil {
			t.Fatal("invalid URL accepted")
		}
	}
}
func TestNotificationStatusNeverReturnsSecrets(t *testing.T) {
	a := testAdmin(t)
	raw := `{"enabled":true,"provider":"pushplus","url":"https://example.com/private","secret":"private-token","include_number":false}`
	if _, e := a.notificationSave(raw); e != nil {
		t.Fatal(e)
	}
	v, e := a.notificationStatus()
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "private") {
		t.Fatal("credentials leaked")
	}
	if _, e := a.notificationSave(`{"enabled":false,"provider":"pushplus","url":"","secret":""}`); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(a.path("/data/notifications.json"))
	if !bytes.Contains(b, []byte("private-token")) {
		t.Fatal("blank edit lost secret")
	}
}
func TestBluetoothRequiresVerifiedFinalState(t *testing.T) {
	a := testAdmin(t)
	a.run = func(_ context.Context, _ string, args []string, _ []byte) ([]byte, error) {
		if args[0] == "show" {
			return []byte("Controller AA:BB:CC:DD:EE:FF test\n Powered: no\n"), nil
		}
		return []byte("Failed to set power"), nil
	}
	if _, e := a.bluetooth(deviceRequest{Action: "bt_power", Enabled: true}); e == nil {
		t.Fatal("exit zero misreported success")
	}
	if _, e := os.Stat(a.path("/data/bluetooth-power.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("failed state persisted")
	}
}

func TestMaintenanceWorkerInheritedLock(t *testing.T) {
	if os.Getenv("AIRSIM_TEST_INHERITED_LOCK") == "1" {
		lock := os.NewFile(3, "inherited")
		if lock == nil {
			os.Exit(9)
		}
		defer lock.Close()
		path := os.Getenv("AIRSIM_TEST_LOCK_ROOT")
		if e := os.WriteFile(filepath.Join(path, "ready"), []byte("ready"), 0600); e != nil {
			os.Exit(8)
		}
		for i := 0; i < 200; i++ {
			if _, e := os.Stat(filepath.Join(path, "release")); e == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(7)
	}
	data := t.TempDir()
	lock, e := managementLock(data, true)
	if e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	child := exec.Command(exe, "-test.run=^TestMaintenanceWorkerInheritedLock$")
	child.Env = append(os.Environ(), "AIRSIM_TEST_INHERITED_LOCK=1", "AIRSIM_TEST_LOCK_ROOT="+data)
	child.ExtraFiles = []*os.File{lock}
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	lock.Close()
	defer child.Process.Kill()
	ready := false
	for i := 0; i < 100; i++ {
		if _, e = os.Stat(filepath.Join(data, "ready")); e == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("child did not inherit descriptor")
	}
	if leaked, e := managementLock(data, true); e == nil {
		leaked.Close()
		t.Fatal("parent close released worker ownership")
	}
	if e = os.WriteFile(filepath.Join(data, "release"), []byte("release"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = child.Wait(); e != nil {
		t.Fatal(e)
	}
	again, e := managementLock(data, true)
	if e != nil {
		t.Fatal("worker leaked lock", e)
	}
	again.Close()
}

func TestMaintenanceBlocksNativeSMSAdmission(t *testing.T) {
	data := t.TempDir()
	lock, e := managementLock(data, true)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	f, e := os.CreateTemp(t.TempDir(), "request")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.WriteString(`{"num":"15550001111","text":"synthetic test"}`); e != nil {
		t.Fatal(e)
	}
	f.Seek(0, 0)
	original := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = original }()
	result := deviceSMSSend(data).(map[string]any)
	if !strings.Contains(result["error"].(string), "维护进行中") {
		t.Fatal("SMS escaped maintenance guard", result)
	}
	// No native tools exist in this test and no SMS submission occurs.
}

func TestNetworkManagementReadSeparatesConfiguredAppliedAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, ruleName, rules string
		nftError              bool
		known, applied        bool
	}{
		{"configured-not-applied", "AirSIM-WiFi-Admin", "", false, true, false},
		{"disabled", "AirSIM-WiFi-Admin", "", false, true, false},
		{"applied", "AirSIM-WiFi-Admin", "iifname wlan0 accept comment AirSIM-WiFi-Admin", false, true, true},
		{"unrelated-lines", "AirSIM-WiFi-Admin", "iifname wlan0 accept\ncomment AirSIM-WiFi-Admin", false, true, false},
		{"unreadable", "AirSIM-WiFi-Admin", "", true, false, false},
		{"foreign-rule", "Someone-Else", "", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testAdmin(t)
			a.run = func(_ context.Context, name string, args []string, _ []byte) ([]byte, error) {
				if name == "ubus" {
					if len(args) != 4 || args[0] != "call" || args[1] != "uci" || args[2] != "get" {
						t.Fatal("unexpected command", args)
					}
					if strings.Contains(args[3], "firewall") {
						enabled := "1"
						if tc.name == "disabled" {
							enabled = "0"
						}
						return json.Marshal(map[string]any{"values": map[string]any{"airsim_wifi_admin": map[string]any{"name": tc.ruleName, "enabled": enabled}}})
					}
					return []byte(`{"values":{"lan":{},"wan":{},"wwan":{},"ap0":{},"wwan0":{}}}`), nil
				}
				if name == "nft" && strings.Join(args, " ") == "list chain inet fw4 input" {
					if tc.nftError {
						return nil, errors.New("unavailable")
					}
					return []byte(tc.rules), nil
				}
				t.Fatal("write or unexpected command", name, args)
				return nil, nil
			}
			r, e := a.networkStatus()
			if e != nil || r["admin_known"] != tc.known {
				t.Fatal(r, e)
			}
			if tc.known && (r["admin_enabled"] != (tc.name != "disabled") || r["admin_applied"] != tc.applied) {
				t.Fatal(r)
			}
			if r["supported"] != false {
				t.Fatal("unsafe layout became supported")
			}
		})
	}
}
