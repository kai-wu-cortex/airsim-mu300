package main

// Management runs as a short-lived root CLI behind rpcd's authenticated ACL,
// including while the telephone service is stopped. No new network listener.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type managementSettings struct {
	Mode            string `json:"mode"`
	RelayURL        string `json:"relay_url"`
	Interface       string `json:"interface"`
	TunnelInterface string `json:"tunnel_interface,omitempty"`
	PairPort        int    `json:"pair_port"`
	ControlPort     int    `json:"control_port"`
	PCMPort         int    `json:"pcm_port"`
}

func defaultSettings() managementSettings {
	return managementSettings{Mode: "cloud", Interface: "br-lan", PairPort: 8580, ControlPort: 8581, PCMPort: 8582}
}

var interfacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)

func (s managementSettings) validate() error {
	if s.Mode != "local" && s.Mode != "cloud" {
		return errors.New("请选择局域网或云端模式")
	}
	if !interfacePattern.MatchString(s.Interface) {
		return errors.New("LAN 接口名称无效")
	}
	if s.TunnelInterface != "" && (!interfacePattern.MatchString(s.TunnelInterface) || s.TunnelInterface == s.Interface || s.TunnelInterface == "lo") {
		return errors.New("Tailscale 接口名称无效或与 LAN 重复")
	}
	seen := map[int]bool{7580: true, 7581: true, 8575: true, 8576: true}
	for _, port := range []int{s.PairPort, s.ControlPort, s.PCMPort} {
		if port < 1024 || port > 65535 || seen[port] {
			return errors.New("端口须为 1024–65535，互不重复且不占用内部端口 7580/7581/8575/8576")
		}
		seen[port] = true
	}
	if s.RelayURL != "" {
		u, e := url.Parse(s.RelayURL)
		if e != nil || len(s.RelayURL) > 512 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(s.RelayURL, "\r\n\t ") {
			return errors.New("Relay 须为不含账号、查询参数或片段的 HTTPS 地址；留空跟随配对配置")
		}
	}
	return nil
}

func decodeSettings(r io.Reader) (managementSettings, error) {
	s := defaultSettings()
	b, err := io.ReadAll(io.LimitReader(r, 8193))
	if err != nil || len(b) > 8192 || !bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		return s, errors.New("设置格式无效或超过大小限制")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, errors.New("设置格式无效")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return s, errors.New("设置含多余内容")
	}
	s.RelayURL = strings.TrimRight(strings.TrimSpace(s.RelayURL), "/")
	return s, s.validate()
}

func loadSettings(data string) (managementSettings, error) {
	b, e := os.ReadFile(filepath.Join(data, "management.json"))
	if errors.Is(e, os.ErrNotExist) {
		return defaultSettings(), nil
	}
	if e != nil {
		return managementSettings{}, errors.New("无法读取管理设置")
	}
	return decodeSettings(bytes.NewReader(b))
}

// Both runtime processes hold shared locks for their entire lives. A save
// holds an exclusive lock, so startup cannot race an atomic settings replace.
func managementLock(data string, exclusive bool) (*os.File, error) {
	if e := os.MkdirAll(data, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(data, "management.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if e = syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("服务仍在运行或设置正在保存，请先关闭服务后重试")
	}
	return f, nil
}

func legacyServiceListening() bool {
	for _, address := range []string{"127.0.0.1:8575", "127.0.0.1:7581"} {
		c, e := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if e == nil {
			c.Close()
			return true
		}
	}
	return false
}

func saveSettings(data string, s managementSettings) error {
	if e := s.validate(); e != nil {
		return e
	}
	lock, e := managementLock(data, true)
	if e != nil {
		return e
	}
	defer lock.Close()
	if legacyServiceListening() {
		return errors.New("检测到运行中的 Agent，请先关闭服务")
	}
	hosts, e := directAddresses(s)
	if e != nil {
		return e
	}
	listeners, e := bindDirectListeners(hosts, s)
	if e != nil {
		return errors.New("所选端口已占用或不可用")
	}
	defer listeners.close()
	return writeSettingsFile(data, s)
}

func writeSettingsFile(data string, s managementSettings) error {
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(data, ".management-*")
	if e != nil {
		return errors.New("无法创建设置文件")
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return errors.New("保存设置失败")
	}
	if e = f.Sync(); e != nil {
		return errors.New("保存设置失败")
	}
	if e = f.Close(); e != nil {
		return errors.New("保存设置失败")
	}
	if e = os.Rename(name, filepath.Join(data, "management.json")); e != nil {
		return errors.New("保存设置失败")
	}
	dir, e := os.Open(data)
	if e != nil {
		return errors.New("设置已写入但持久化状态未知，请重新读取核对")
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return errors.New("设置已写入但持久化状态未知，请重新读取核对")
	}
	return nil
}

func runManagedAgent(ctx context.Context, s managementSettings) error {
	cmd := exec.Command("/usr/lib/airsim/airsim-agent")
	cmd.Env = append(os.Environ(), "AIRSIM_MU300_MODE="+s.Mode, "AIRSIM_MU300_RELAY="+s.RelayURL)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if e := cmd.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case e := <-done:
		return e
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case e := <-done:
			return e
		case <-time.After(25 * time.Second):
			_ = cmd.Process.Kill()
			return <-done
		}
	}
}

func managementSnapshot() map[string]any {
	client := &http.Client{Timeout: 5 * time.Second}
	r, e := client.Get("http://127.0.0.1:7581/status")
	if e != nil {
		return nil
	}
	defer r.Body.Close()
	var s map[string]any
	if r.StatusCode != 200 || json.NewDecoder(io.LimitReader(r.Body, 131072)).Decode(&s) != nil {
		return nil
	}
	return s
}

func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func flagValue(v any) bool        { b, _ := v.(bool); return b }

// A typed allowlist, not regex redaction of raw logs. Unknown fields, messages,
// addresses, identifiers, URLs and free-text error strings are never exported.
func diagnosticReport(s map[string]any, settings managementSettings) map[string]any {
	a, relay, sim, calls, audio := object(s["agent"]), object(s["relay"]), object(s["sim"]), object(s["calls"]), object(s["audio"])
	return map[string]any{
		"schema": 1, "product": "AirSIM for MU300", "version": version, "generated_at": time.Now().UTC().Format(time.RFC3339),
		"mode": settings.Mode, "status_available": s != nil, "agent_reachable": flagValue(a["reachable"]),
		"sim_inserted": flagValue(sim["sim_inserted"]), "audio_available": flagValue(s["audio_available"]), "audio_active": flagValue(audio["active"]),
		"call_status_available": flagValue(calls["available"]), "call_active_or_unknown": s == nil || flagValue(s["call_active"]),
		"relay_configured": flagValue(relay["configured"]), "cloud_enabled": flagValue(relay["cloud_enabled"]),
		"relay_command_connected": flagValue(relay["command_connected"]), "relay_error_present": relay["last_error"] != nil && relay["last_error"] != "",
		"audio_error_present": audio["last_error"] != nil && audio["last_error"] != "",
		"privacy":             "仅导出状态白名单；不含原始日志、号码、短信、音频、地址、设备标识或凭据",
	}
}

const releasePage = "https://github.com/kai-wu-cortex/airsim-mu300/releases"
const releasesAPI = "https://api.github.com/repos/kai-wu-cortex/airsim-mu300/releases?per_page=30"

var releasePattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)-r(\d+)$`)

func versionParts(v string) ([]int, bool) {
	m := releasePattern.FindStringSubmatch(v)
	if m == nil {
		return nil, false
	}
	p := make([]int, 4)
	for i := range p {
		n, e := strconv.Atoi(m[i+1])
		if e != nil {
			return nil, false
		}
		p[i] = n
	}
	return p, true
}
func newer(a, b string) bool {
	x, ok := versionParts(a)
	if !ok {
		return false
	}
	y, ok := versionParts(b)
	if !ok {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

type githubRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

func selectRelease(releases []githubRelease, current string, previews bool) map[string]any {
	var selected *githubRelease
	for i := range releases {
		r := &releases[i]
		_, valid := versionParts(r.Tag)
		if r.Draft || !valid || r.Prerelease && !previews {
			continue
		}
		installable := false
		for _, a := range r.Assets {
			if a.Name == "luci-app-airsim-"+strings.TrimPrefix(r.Tag, "v")+".apk" {
				installable = true
			}
		}
		if installable && (selected == nil || newer(r.Tag, selected.Tag)) {
			selected = r
		}
	}
	if selected == nil {
		return map[string]any{"ok": true, "status": "no_release", "message": "此频道最近 30 个 Release 中没有匹配的安装包", "url": releasePage}
	}
	_, comparable := versionParts(current)
	status := "current"
	if !comparable {
		status = "unknown_version"
	} else if newer(selected.Tag, current) {
		status = "available"
	} else if newer(current, selected.Tag) {
		status = "ahead"
	}
	return map[string]any{"ok": true, "status": status, "current": current, "latest": selected.Tag, "prerelease": selected.Prerelease, "url": releasePage + "/tag/" + selected.Tag, "checked_at": time.Now().UTC().Format(time.RFC3339)}
}

func checkUpdates(ctx context.Context, previews bool) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AirSIM-MU300-update-check")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("无法连接 GitHub，请检查设备联网后重试；未下载或安装任何文件")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("GitHub 检查失败或请求受限，请稍后重试")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1048577))
	if e != nil || len(b) > 1048576 {
		return nil, errors.New("版本信息过大或不完整")
	}
	var releases []githubRelease
	if json.Unmarshal(b, &releases) != nil {
		return nil, errors.New("版本信息格式无效")
	}
	return selectRelease(releases, version, previews), nil
}

func managementCommand(action, data string) any {
	if action == "updates-status" || action == "updates-policy" || action == "update-install" {
		return updateManagement(action, data)
	}
	s, e := loadSettings(data)
	if e != nil {
		return map[string]any{"error": e.Error()}
	}
	switch action {
	case "get":
		return map[string]any{"ok": true, "settings": s, "version": version, "release_url": releasePage}
	case "save":
		s, e = decodeSettings(os.Stdin)
		if e == nil {
			e = saveSettings(data, s)
		}
		if e == nil {
			return map[string]any{"ok": true, "settings": s, "message": "设置已保存；请启动服务使其生效。更换 Relay 需在客户端配置相同地址并重新配对。"}
		}
	case "diagnostics":
		return map[string]any{"ok": true, "report": diagnosticReport(managementSnapshot(), s)}
	case "updates", "updates-stable":
		var result map[string]any
		result, e = checkUpdatesCached(context.Background(), data, action == "updates", true)
		if e == nil {
			return result
		}
	default:
		e = errors.New("未知管理操作")
	}
	return map[string]any{"error": e.Error()}
}

func startManagedAgent(data string) error {
	lock, e := managementLock(data, false)
	if e != nil {
		return e
	}
	defer lock.Close()
	s, e := loadSettings(data)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return runManagedAgent(ctx, s)
}

func portText(port int) string { return fmt.Sprint(port) }
