package main

// Device maintenance is a separate, authenticated rpcd CLI. Mutations share
// the runtime/update lock, require an idle native modem, and never start calls.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type deviceRequest struct {
	Action     string `json:"action"`
	Page       string `json:"page,omitempty"`
	Confirm    bool   `json:"confirm,omitempty"`
	Mode       string `json:"mode,omitempty"`
	SSID       string `json:"ssid,omitempty"`
	Password   string `json:"password,omitempty"`
	Encryption string `json:"encryption,omitempty"`
	Address    string `json:"address,omitempty"`
	Enabled    bool   `json:"enabled,omitempty"`
	Payload    string `json:"payload,omitempty"`
	Token      string `json:"token,omitempty"`
}
type deviceRunner func(context.Context, string, []string, []byte) ([]byte, error)
type deviceAdmin struct {
	root, data string
	run        deviceRunner
	sleep      func(time.Duration)
}

func commandDevice(ctx context.Context, name string, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	var output cappedDeviceOutput
	cmd.Stdout = &output
	// Native stderr can contain credentials or SMS. Never return or log it.
	if err := cmd.Run(); err != nil {
		return nil, errors.New("设备命令未成功，请刷新状态确认；不会自动重试")
	}
	return output.Bytes(), nil
}

type cappedDeviceOutput struct{ bytes.Buffer }

func (b *cappedDeviceOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1024*1024 {
		return 0, errors.New("response too large")
	}
	return b.Buffer.Write(p)
}
func (a *deviceAdmin) path(p string) string { return filepath.Join(a.root, p) }
func (a *deviceAdmin) call(seconds int, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	return a.run(ctx, name, args, nil)
}
func (a *deviceAdmin) ubus(object, method string, args any) (map[string]any, error) {
	b, _ := json.Marshal(args)
	out, err := a.call(8, "ubus", "call", object, method, string(b))
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if json.Unmarshal(out, &v) != nil || v == nil {
		return nil, errors.New("设备状态响应无效")
	}
	return v, nil
}
func (a *deviceAdmin) text(p string) string {
	b, _ := limitedFile(a.path(p), 65536)
	return strings.TrimSpace(strings.Trim(string(b), "\x00"))
}
func (a *deviceAdmin) write(p string, b []byte) error {
	dest := a.path(p)
	for parent := filepath.Dir(dest); parent != filepath.Clean(a.root) && parent != "." && parent != "/"; parent = filepath.Dir(parent) {
		st, e := os.Lstat(parent)
		if e == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
			return errors.New("拒绝链接配置目录")
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	if st, e := os.Lstat(dest); e == nil && !st.Mode().IsRegular() {
		return errors.New("目标不是普通文件")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".airsim-admin-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), dest)
}
func (a *deviceAdmin) idle() error {
	out, err := a.call(5, "mu300-at", "-t", "3", "AT+CLCC")
	if err != nil {
		return errors.New("无法确认基带空闲")
	}
	ok := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "+CLCC:") || strings.Contains(line, "ERROR") {
			return errors.New("请先结束通话；状态未知时暂停维护")
		}
		if line == "OK" {
			ok = true
		}
	}
	if !ok {
		return errors.New("基带空闲状态未经确认")
	}
	for _, process := range []string{"pcmhost", "aplay", "arecord", "mu300-hfp"} {
		if _, err := a.call(2, "pidof", process); err == nil {
			return errors.New("音频正在使用，请结束后操作")
		}
	}
	job, err := a.ubus("mu300dash", "sms_job", map[string]any{})
	if err != nil {
		return errors.New("短信任务状态未知，暂停维护")
	}
	if truth(job["running"]) || (!truth(job["none"]) && !truth(job["done"])) {
		return errors.New("短信任务进行中或状态未知")
	}
	return nil
}
func truth(v any) bool { return v == true || v == float64(1) || v == "1" || v == "true" }

var deviceReadActions = map[string]bool{"pcm_gain": true, "maintenance_job": true, "selfcheck": true, "network": true, "flow_status": true, "bluetooth": true, "notifications": true, "sms_preview": true, "audio_catalog": true, "sms_list": true}
var deviceWriteActions = map[string]bool{"pcm_gain_save": true, "backup_export": true, "wifi_scan": true, "network_mode": true, "wifi_connect": true, "wifi_forget": true, "wifi_admin": true, "flow_fix": true, "flow_restore": true, "bt_power": true, "bt_scan": true, "bt_pair": true, "bt_connect": true, "bt_disconnect": true, "bt_remove": true, "backup_restore": true, "selfcheck_fix": true, "notifications_save": true, "sms_mark_all": true, "sms_clear": true, "audio_install": true}

func deviceManagement(mode, data string) any {
	var input deviceRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4*1024*1024+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return map[string]any{"error": "维护请求无效"}
	}
	if (mode == "device-read" && !deviceReadActions[input.Action]) || (mode == "device-write" && (!deviceWriteActions[input.Action] || !input.Confirm)) {
		return map[string]any{"error": "操作未授权或未确认"}
	}
	a := &deviceAdmin{root: "/", data: data, run: commandDevice, sleep: time.Sleep}
	// All external-state writes, scans and exports are explicitly requested. A
	// stopped service avoids racing call admission, relay media and config reload.
	exclusive := mode == "device-write" || input.Action == "backup_export"
	if exclusive {
		lock, err := managementLock(data, true)
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		defer lock.Close()
		if legacyServiceListening() {
			return map[string]any{"error": "请先关闭 AirSIM 服务"}
		}
		if err = a.idle(); err != nil {
			return map[string]any{"error": err.Error()}
		}
		if input.Action != "backup_export" && input.Action != "pcm_gain_save" {
			result, err := a.launchJob(input, lock)
			if err != nil {
				return map[string]any{"error": err.Error()}
			}
			result["ok"] = true
			return result
		}
	}
	result, err := a.dispatch(input)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if result == nil {
		result = map[string]any{}
	}
	result["ok"] = true
	return result
}
func (a *deviceAdmin) dispatch(r deviceRequest) (map[string]any, error) {
	switch r.Action {
	case "pcm_gain":
		return a.pcmGainStatus()
	case "pcm_gain_save":
		return a.pcmGainSave(r.Payload)
	case "maintenance_job":
		return a.jobStatus()
	case "sms_list":
		return a.smsList(r.Page)
	case "selfcheck":
		return a.selfcheck(), nil
	case "selfcheck_fix":
		return a.selfcheckFix()
	case "network":
		return a.networkStatus()
	case "wifi_scan":
		return a.wifiScan()
	case "network_mode", "wifi_connect", "wifi_forget", "wifi_admin":
		return a.networkChange(r)
	case "flow_status", "flow_fix", "flow_restore":
		return a.flow(r.Action)
	case "bluetooth", "bt_power", "bt_scan", "bt_pair", "bt_connect", "bt_disconnect", "bt_remove":
		return a.bluetooth(r)
	case "backup_export":
		return a.backupExport()
	case "backup_restore":
		return a.backupRestore(r.Payload)
	case "notifications":
		return a.notificationStatus()
	case "notifications_save":
		return a.notificationSave(r.Payload)
	case "sms_preview", "sms_mark_all", "sms_clear":
		return a.smsMaintenance(r)
	case "audio_catalog":
		return a.audioCatalog()
	case "audio_install":
		return a.audioInstall(r.Mode)
	}
	return nil, fmt.Errorf("不支持的维护操作")
}
