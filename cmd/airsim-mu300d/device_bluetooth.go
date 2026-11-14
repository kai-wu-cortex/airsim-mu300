package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var btAddress = regexp.MustCompile(`^[0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5}$`)
var btDeviceLine = regexp.MustCompile(`^Device ([0-9A-Fa-f:]{17}) (.*)$`)

func btProperties(raw []byte) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if ok && (k == "Powered" || k == "Paired" || k == "Connected" || k == "Name" || k == "Alias" || k == "Discoverable") {
			result[strings.ToLower(k)] = v
		}
	}
	return result
}
func (a *deviceAdmin) bluetooth(r deviceRequest) (map[string]any, error) {
	show, err := a.call(4, "bluetoothctl", "show")
	if err != nil || !strings.Contains(string(show), "Controller ") {
		if r.Action == "bluetooth" {
			return map[string]any{"supported": false, "reason": "未发现可用 BlueZ 控制器；请先安装与当前内核匹配的蓝牙组件。"}, nil
		}
		return nil, errors.New("BlueZ 控制器未就绪，不能执行蓝牙操作")
	}
	properties := btProperties(show)
	if r.Action == "bluetooth" {
		devices, e := a.call(4, "bluetoothctl", "devices")
		if e != nil {
			return nil, e
		}
		list := []map[string]any{}
		for _, line := range strings.Split(string(devices), "\n") {
			match := btDeviceLine.FindStringSubmatch(strings.TrimSpace(line))
			if match == nil || !btAddress.MatchString(match[1]) {
				continue
			}
			list = append(list, map[string]any{"address": match[1], "name": match[2]})
			if len(list) >= 64 {
				break
			}
		}
		return map[string]any{"supported": true, "controller": properties, "devices": list, "pairing_note": "使用已有 BlueZ 配对代理；需要验证码时请在其交互界面确认，不会自动同意陌生设备。"}, nil
	}
	if r.Action == "bt_power" {
		desired := "off"
		if r.Enabled {
			desired = "on"
		}
		if _, err = a.call(8, "bluetoothctl", "power", desired); err != nil {
			return nil, err
		}
		check, e := a.call(4, "bluetoothctl", "show")
		if e != nil || (btProperties(check)["powered"] == "yes") != r.Enabled {
			return nil, errors.New("蓝牙电源状态未确认")
		}
		raw, _ := json.Marshal(map[string]bool{"enabled": r.Enabled})
		if e = a.write(filepath.Join(a.data, "bluetooth-power.json"), raw); e != nil {
			return nil, errors.New("电源状态已改变，但开机设置保存失败，请重新核对")
		}
		return map[string]any{"message": "蓝牙电源已确认，开机偏好已保存。"}, nil
	}
	if r.Action == "bt_scan" {
		// bluetoothctl owns a bounded discovery session, then relinquishes it.
		if _, err = a.call(14, "bluetoothctl", "--timeout", "8", "scan", "on"); err != nil {
			return nil, errors.New("扫描结果未确认，请刷新已有设备列表")
		}
		return a.bluetooth(deviceRequest{Action: "bluetooth"})
	}
	if !btAddress.MatchString(r.Address) {
		return nil, errors.New("蓝牙地址无效")
	}
	action := map[string]string{"bt_pair": "pair", "bt_connect": "connect", "bt_disconnect": "disconnect", "bt_remove": "remove"}[r.Action]
	if action == "" {
		return nil, errors.New("不支持的蓝牙操作")
	}
	if _, err = a.call(20, "bluetoothctl", "--timeout", "15", action, r.Address); err != nil {
		return nil, errors.New("蓝牙操作未确认，请核对对端或配对代理，不会自动重试")
	}
	info, e := a.call(4, "bluetoothctl", "info", r.Address)
	p := btProperties(info)
	confirmed := e == nil && ((action == "pair" && p["paired"] == "yes") || (action == "connect" && p["connected"] == "yes") || (action == "disconnect" && p["connected"] == "no"))
	if action == "remove" {
		devices, de := a.call(4, "bluetoothctl", "devices")
		confirmed = de == nil && !strings.Contains(strings.ToUpper(string(devices)), strings.ToUpper(r.Address))
	}
	if !confirmed {
		return nil, errors.New("蓝牙最终状态未确认，请刷新核对")
	}
	return map[string]any{"message": "蓝牙操作已确认。配对或连接成功不代表蜂窝音频路由已经支持。"}, nil
}
func deviceBoot(data string) error {
	a := &deviceAdmin{root: "/", data: data, run: commandDevice}
	b, err := limitedFile(filepath.Join(data, "bluetooth-power.json"), 128)
	if err != nil {
		return nil
	}
	lock, err := managementLock(data, true)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = a.idle(); err != nil {
		return err
	}
	var v struct {
		Enabled bool `json:"enabled"`
	}
	if json.Unmarshal(b, &v) != nil {
		return errors.New("invalid bluetooth preference")
	}
	desired := "off"
	if v.Enabled {
		desired = "on"
	}
	_, err = a.call(8, "bluetoothctl", "power", desired)
	return err
}
