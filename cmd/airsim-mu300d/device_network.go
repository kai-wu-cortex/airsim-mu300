package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func (a *deviceAdmin) uci(config string) (map[string]map[string]any, error) {
	v, err := a.ubus("uci", "get", map[string]string{"config": config})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(v["values"])
	var values map[string]map[string]any
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, errors.New("无法读取网络配置")
	}
	return values, nil
}
func option(v map[string]any, key string) string { s, _ := v[key].(string); return s }
func disabled(v map[string]any) bool             { return option(v, "disabled") == "1" }
func networkMode(n, w map[string]map[string]any) string {
	switch {
	case !disabled(n["wan"]) && disabled(w["ap0"]) && disabled(w["wwan0"]):
		return "cell"
	case disabled(n["wan"]) && disabled(w["ap0"]) && !disabled(w["wwan0"]):
		return "wifi"
	case !disabled(n["wan"]) && !disabled(w["ap0"]) && disabled(w["wwan0"]):
		return "ap"
	}
	return "unknown"
}
func networkProfile(n, w map[string]map[string]any) error {
	if option(n["lan"], "device") != "br-lan" || option(n["lan"], "proto") != "static" || option(n["lan"], "ipaddr") == "" {
		return errors.New("USB 管理口结构未知，已禁止切换")
	}
	bridge := false
	for _, v := range n {
		if option(v, "type") != "bridge" || option(v, "name") != "br-lan" {
			continue
		}
		raw, _ := json.Marshal(v["ports"])
		if string(raw) == `["usb0"]` || option(v, "ports") == "usb0" {
			bridge = true
		}
	}
	if !bridge {
		return errors.New("br-lan 并非仅包含 usb0，需先适配网络配置")
	}
	if option(w["wwan0"], "network") != "wwan" || option(w["ap0"], "network") == "lan" || option(w["ap0"], "network") == "" {
		return errors.New("无线网络关联未知或与 USB 口共用，暂停切换")
	}
	if option(n["wan"], "proto") != "mu300cell" || option(n["wwan"], "proto") != "dhcp" || option(w["ap0"], "mode") != "ap" || option(w["wwan0"], "mode") != "sta" || option(w["ap0"], "device") == "" || option(w["ap0"], "device") != option(w["wwan0"], "device") {
		return errors.New("无线或蜂窝接口布局未适配，不能猜测切换")
	}
	return nil
}
func (a *deviceAdmin) networkStatus() (map[string]any, error) {
	n, err := a.uci("network")
	if err != nil {
		return nil, err
	}
	w, err := a.uci("wireless")
	if err != nil {
		return nil, err
	}
	reason := ""
	if e := networkProfile(n, w); e != nil {
		reason = e.Error()
	}
	result := map[string]any{"supported": reason == "", "reason": reason, "mode": networkMode(n, w), "ssid": option(w["wwan0"], "ssid"), "encryption": option(w["wwan0"], "encryption"), "has_password": option(w["wwan0"], "key") != "", "usb_management_preserved": reason == "", "admin_known": false}
	// Report configured and applied state separately; an unreadable firewall
	// never becomes a successful/off switch in the management UI.
	if firewall, e := a.uci("firewall"); e == nil {
		rule, exists := firewall["airsim_wifi_admin"]
		if !exists || option(rule, "name") == "AirSIM-WiFi-Admin" {
			if rules, e := a.call(4, "nft", "list", "chain", "inet", "fw4", "input"); e == nil {
				result["admin_known"] = true
				result["admin_enabled"] = exists && option(rule, "enabled") != "0"
				applied := false
				for _, line := range strings.Split(string(rules), "\n") {
					applied = applied || (strings.Contains(line, "AirSIM-WiFi-Admin") && strings.Contains(line, "wlan0"))
				}
				result["admin_applied"] = applied
			}
		}
	}
	return result, nil
}
func (a *deviceAdmin) wifiScan() (map[string]any, error) {
	// Scan only an existing interface. Do not create temporary interfaces or alter
	// radio/USB configuration just to populate a list.
	v, err := a.ubus("iwinfo", "scan", map[string]string{"device": "wlan0"})
	if err != nil {
		return nil, errors.New("无线扫描不可用，请确认 wlan0 和 iwinfo 服务已就绪")
	}
	return map[string]any{"results": v["results"]}, nil
}
func uciQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (a *deviceAdmin) configTransaction(changes map[string]string, deletes []string, apply, verify func() error) (map[string]any, error) {
	packages := map[string]bool{}
	for key := range changes {
		packages[strings.SplitN(key, ".", 2)[0]] = true
	}
	for _, key := range deletes {
		packages[strings.SplitN(key, ".", 2)[0]] = true
	}
	base := a.path(filepath.Join(a.data, "maintenance"))
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(base, "config-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err = os.Mkdir(filepath.Join(stage, "delta"), 0700); err != nil {
		return nil, err
	}
	before := map[string][]byte{}
	pkgs := []string{}
	for pkg := range packages {
		if pkg != "network" && pkg != "wireless" && pkg != "firewall" {
			return nil, errors.New("配置范围不允许")
		}
		data, e := limitedFile(a.path("/etc/config/"+pkg), 262144)
		if e != nil {
			return nil, errors.New("无法保存原网络配置")
		}
		before[pkg] = data
		pkgs = append(pkgs, pkg)
		if e = os.WriteFile(filepath.Join(stage, pkg), data, 0600); e != nil {
			return nil, e
		}
	}
	sort.Strings(pkgs)
	keys := []string{}
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var batch strings.Builder
	for _, key := range deletes {
		fmt.Fprintf(&batch, "delete %s\n", key)
	}
	for _, key := range keys {
		fmt.Fprintf(&batch, "set %s=%s\n", key, uciQuote(changes[key]))
	}
	for _, pkg := range pkgs {
		fmt.Fprintf(&batch, "commit %s\n", pkg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = a.run(ctx, "uci", []string{"-c", stage, "-P", filepath.Join(stage, "delta"), "batch"}, []byte(batch.String())); err != nil {
		return nil, errors.New("配置校验失败，尚未改动设备网络")
	}
	for _, pkg := range pkgs {
		current, e := limitedFile(a.path("/etc/config/"+pkg), 262144)
		if e != nil || !bytes.Equal(current, before[pkg]) {
			return nil, errors.New("网络配置已被其他管理程序改变，请刷新后重试")
		}
	}
	saved, _ := json.Marshal(before)
	if err = a.write(filepath.Join(a.data, "maintenance", "network-before.json"), saved); err != nil {
		return nil, errors.New("无法保存回退副本")
	}
	rollback := func() bool {
		ok := true
		for _, pkg := range pkgs {
			if a.write("/etc/config/"+pkg, before[pkg]) != nil {
				ok = false
			}
		}
		if apply() != nil {
			ok = false
		}
		return ok
	}
	for _, pkg := range pkgs {
		b, e := limitedFile(filepath.Join(stage, pkg), 262144)
		if e != nil || a.write("/etc/config/"+pkg, b) != nil {
			rollback()
			return nil, errors.New("写入失败，已尝试恢复原配置，请核对网络状态")
		}
	}
	if err = apply(); err == nil {
		err = verify()
	}
	if err != nil {
		if !rollback() {
			return nil, errors.New("操作失败且回退未确认，请通过 USB 管理口检查；原配置保存在维护目录")
		}
		return nil, errors.New("操作未通过检查，已恢复原配置；请刷新核对连接")
	}
	return map[string]any{"message": "配置已应用并通过接口检查；USB 管理配置保持原样。请刷新并确认实际联网。"}, nil
}
func (a *deviceAdmin) networkChange(r deviceRequest) (map[string]any, error) {
	n, err := a.uci("network")
	if err != nil {
		return nil, err
	}
	w, err := a.uci("wireless")
	if err != nil {
		return nil, err
	}
	if err = networkProfile(n, w); err != nil {
		return nil, err
	}
	if r.Action == "wifi_admin" {
		return a.wifiAdmin(r.Enabled)
	}
	mode := r.Mode
	changes := map[string]string{}
	deletes := []string{}
	if r.Action == "wifi_connect" {
		mode = "wifi"
		if !utf8.ValidString(r.SSID) || len([]byte(r.SSID)) < 1 || len([]byte(r.SSID)) > 32 || strings.ContainsAny(r.SSID+r.Password, "\x00\r\n") {
			return nil, errors.New("Wi-Fi 名称或密码格式无效")
		}
		if r.Encryption != "psk2" && r.Encryption != "sae" && r.Encryption != "none" {
			return nil, errors.New("仅支持 WPA2、WPA3 或开放网络")
		}
		if r.Encryption != "none" && (len(r.Password) < 8 || len(r.Password) > 63) {
			return nil, errors.New("Wi-Fi 密码须为 8–63 字节")
		}
		changes["wireless.wwan0.ssid"] = r.SSID
		changes["wireless.wwan0.encryption"] = r.Encryption
		if r.Encryption != "none" {
			changes["wireless.wwan0.key"] = r.Password
		} else if option(w["wwan0"], "key") != "" {
			deletes = append(deletes, "wireless.wwan0.key")
		}
	}
	if r.Action == "wifi_forget" {
		mode = "cell"
		for _, key := range []string{"ssid", "key"} {
			if option(w["wwan0"], key) != "" {
				deletes = append(deletes, "wireless.wwan0."+key)
			}
		}
	}
	if mode != "cell" && mode != "wifi" && mode != "ap" {
		return nil, errors.New("未知联网模式")
	}
	if mode == "wifi" && r.Action != "wifi_connect" && option(w["wwan0"], "ssid") == "" {
		return nil, errors.New("请先配置 Wi-Fi")
	}
	changes["network.wwan.disabled"] = "1"
	changes["network.wan.disabled"] = "0"
	changes["wireless.ap0.disabled"] = "1"
	changes["wireless.wwan0.disabled"] = "1"
	if mode == "wifi" {
		changes["network.wwan.disabled"] = "0"
		changes["network.wan.disabled"] = "1"
		changes["wireless.wwan0.disabled"] = "0"
	}
	if mode == "ap" {
		changes["wireless.ap0.disabled"] = "0"
	}
	apply := func() error {
		if _, e := a.call(10, "wifi", "reload"); e != nil {
			return e
		}
		current, e := a.uci("network")
		if e != nil {
			return e
		}
		wireless, e := a.uci("wireless")
		if e != nil {
			return e
		}
		if disabled(wireless["wwan0"]) {
			if _, e = a.call(8, "ifdown", "wwan"); e != nil {
				return e
			}
		} else if _, e = a.call(8, "ifup", "wwan"); e != nil {
			return e
		}
		// Bring up the desired bearer; avoid manually resetting or manipulating IMS.
		if !disabled(current["wan"]) {
			_, e = a.call(8, "ifup", "wan")
		} else {
			_, e = a.call(8, "ifdown", "wan")
		}
		return e
	}
	verify := func() error {
		iface := "wan"
		if mode == "wifi" {
			iface = "wwan"
		}
		for i := 0; i < 10; i++ {
			v, e := a.ubus("network.interface."+iface, "status", map[string]any{})
			if e == nil && truth(v["up"]) {
				ips, ok := v["ipv4-address"].([]any)
				if ok && len(ips) > 0 {
					return nil
				}
			}
			a.sleep(2 * time.Second)
		}
		return errors.New("目标网络尚未获得地址")
	}
	return a.configTransaction(changes, deletes, apply, verify)
}
func (a *deviceAdmin) wifiAdmin(enabled bool) (map[string]any, error) {
	existing, e := a.uci("firewall")
	if e != nil {
		return nil, e
	}
	deletes := []string{}
	if section, ok := existing["airsim_wifi_admin"]; ok {
		if option(section, "name") != "AirSIM-WiFi-Admin" {
			return nil, errors.New("同名防火墙规则不属于本功能，已保留")
		}
		deletes = append(deletes, "firewall.airsim_wifi_admin")
	}
	changes := map[string]string{"firewall.airsim_wifi_admin": "rule", "firewall.airsim_wifi_admin.name": "AirSIM-WiFi-Admin", "firewall.airsim_wifi_admin.src": "*", "firewall.airsim_wifi_admin.proto": "tcp", "firewall.airsim_wifi_admin.dest_port": "80 443", "firewall.airsim_wifi_admin.target": "ACCEPT", "firewall.airsim_wifi_admin.device": "wlan0", "firewall.airsim_wifi_admin.direction": "in", "firewall.airsim_wifi_admin.enabled": "0"}
	if enabled {
		changes["firewall.airsim_wifi_admin.enabled"] = "1"
	}
	return a.configTransaction(changes, deletes, func() error { _, e := a.call(8, "/etc/init.d/firewall", "reload"); return e }, func() error {
		b, e := a.call(4, "nft", "list", "chain", "inet", "fw4", "input")
		if e != nil {
			return e
		}
		found := strings.Contains(string(b), "AirSIM-WiFi-Admin") && strings.Contains(string(b), "wlan0")
		if found != enabled {
			return errors.New("实际防火墙规则未匹配")
		}
		return nil
	})
}
func (a *deviceAdmin) flow(action string) (map[string]any, error) {
	kernel := a.text("/proc/sys/kernel/osrelease")
	f, e := a.uci("firewall")
	if e != nil {
		return nil, e
	}
	section := ""
	off := ""
	for name, v := range f {
		if option(v, ".type") == "defaults" {
			if section != "" {
				return nil, errors.New("防火墙默认段不唯一")
			}
			section = name
			off = option(v, "flow_offloading")
		}
	}
	if section == "" || !interfacePattern.MatchString(section) {
		return nil, errors.New("防火墙默认段未知")
	}
	table, te := a.call(4, "nft", "list", "table", "inet", "fw4")
	supported := strings.HasPrefix(kernel, "5.4.")
	risk := strings.Contains(string(table), "sipa_eth0") && strings.Contains(string(table), "flowtable") && off != "0"
	if action == "flow_status" {
		return map[string]any{"kernel": kernel, "supported": supported, "offloading": off, "rules_readable": te == nil, "risk": risk}, nil
	}
	if !supported {
		return nil, errors.New("此修复仅面向 5.4；其他内核保持现有设置")
	}
	if te != nil {
		return nil, errors.New("无法读取实际规则，暂停修复")
	}
	path := filepath.Join(a.data, "maintenance", "flow-before.json")
	value := "0"
	deletes := []string{}
	changes := map[string]string{}
	if action == "flow_fix" {
		if !risk {
			return nil, errors.New("未识别到目标冲突，不自动修改卸载设置")
		}
		if _, err := os.Stat(a.path(path)); errors.Is(err, os.ErrNotExist) {
			raw, _ := json.Marshal(map[string]string{"section": section, "value": off})
			if e = a.write(path, raw); e != nil {
				return nil, e
			}
		}
	} else {
		var saved map[string]string
		b, err := limitedFile(a.path(path), 4096)
		if err != nil || json.Unmarshal(b, &saved) != nil || saved["section"] != section {
			return nil, errors.New("没有匹配的原始设置")
		}
		value = saved["value"]
		if value != "" && value != "0" && value != "1" {
			return nil, errors.New("原设置无效")
		}
	}
	key := "firewall." + section + ".flow_offloading"
	if value == "" {
		deletes = append(deletes, key)
	} else {
		changes[key] = value
	}
	return a.configTransaction(changes, deletes, func() error { _, e := a.call(8, "/etc/init.d/firewall", "reload"); return e }, func() error {
		b, e := a.call(4, "nft", "list", "table", "inet", "fw4")
		if e != nil {
			return e
		}
		if action == "flow_fix" && strings.Contains(string(b), "flowtable") {
			return errors.New("流卸载规则仍在")
		}
		return nil
	})
}
