package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type deviceBackup struct {
	Format      string            `json:"format"`
	Version     int               `json:"version"`
	Fingerprint string            `json:"fingerprint"`
	Created     string            `json:"created"`
	Files       map[string][]byte `json:"files"`
}

var backupNames = map[string]bool{"management.json": true, "agent-id": true, "vowlan-clients.json": true, "notifications.json": true, "bluetooth-power.json": true}

func (a *deviceAdmin) fingerprint() (string, error) {
	cid := a.text("/sys/block/mmcblk0/device/cid")
	serial := a.text("/proc/device-tree/serial-number")
	if !regexp.MustCompile(`^[a-fA-F0-9]{32}$`).MatchString(cid) || len(serial) < 2 || len(serial) > 128 {
		return "", errors.New("硬件身份读取不完整，不能导出或恢复本机备份")
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cid+"\x00"+serial))), nil
}
func (a *deviceAdmin) backupExport() (map[string]any, error) {
	fingerprint, err := a.fingerprint()
	if err != nil {
		return nil, err
	}
	b := deviceBackup{Format: "airsim-device-settings", Version: 1, Fingerprint: fingerprint, Created: time.Now().UTC().Format(time.RFC3339), Files: map[string][]byte{}}
	for name := range backupNames {
		data, err := limitedFile(a.path(filepath.Join(a.data, name)), 65536)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if _, e := os.Stat(a.path(filepath.Join(a.data, name))); errors.Is(e, os.ErrNotExist) {
				continue
			}
			return nil, errors.New("配置无法完整读取，备份已取消")
		}
		b.Files[name] = data
	}
	if err = validateDeviceBackup(b); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(b)
	return map[string]any{"filename": "airsim-settings-" + time.Now().Format("20060102-150405") + ".json", "payload": base64.StdEncoding.EncodeToString(raw), "sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "warning": "包含配对凭据和通知配置，请私密保存；不含短信、通话记录、音频、系统网络配置或固件。"}, nil
}
func validateDeviceBackup(b deviceBackup) error {
	if b.Format != "airsim-device-settings" || b.Version != 1 || len(b.Files) == 0 || len(b.Files) > len(backupNames) {
		return errors.New("备份格式不支持")
	}
	if _, ok := b.Files["vowlan-clients.json"]; ok && len(b.Files["agent-id"]) == 0 {
		return errors.New("配对备份必须包含设备身份")
	}
	for name, data := range b.Files {
		if !backupNames[name] || len(data) > 65536 {
			return errors.New("备份包含不允许的文件")
		}
		switch name {
		case "management.json":
			if _, err := decodeSettings(bytes.NewReader(data)); err != nil {
				return err
			}
		case "agent-id":
			if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`).Match(bytes.TrimSpace(data)) {
				return errors.New("设备身份格式无效")
			}
		case "vowlan-clients.json":
			var keys []string
			if json.Unmarshal(data, &keys) != nil || len(keys) > 16 {
				return errors.New("配对数据无效")
			}
			seen := map[string]bool{}
			for _, key := range keys {
				raw, e := base64.RawURLEncoding.DecodeString(key)
				if e != nil || len(raw) != 32 || seen[key] {
					return errors.New("配对数据无效")
				}
				seen[key] = true
			}
		case "notifications.json":
			if _, err := decodeNotification(data); err != nil {
				return err
			}
		case "bluetooth-power.json":
			var v struct {
				Enabled bool `json:"enabled"`
			}
			if json.Unmarshal(data, &v) != nil {
				return errors.New("蓝牙设置无效")
			}
		}
	}
	return nil
}
func (a *deviceAdmin) backupRestore(payload string) (map[string]any, error) {
	if len(payload) > 1024*1024 {
		return nil, errors.New("备份文件过大")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		return nil, errors.New("备份编码无效")
	}
	var b deviceBackup
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&b) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("备份内容无效")
	}
	if err = validateDeviceBackup(b); err != nil {
		return nil, err
	}
	fingerprint, err := a.fingerprint()
	if err != nil {
		return nil, err
	}
	if fingerprint != b.Fingerprint {
		return nil, errors.New("备份不属于本机，已拒绝恢复")
	}
	current := a.text(filepath.Join(a.data, "agent-id"))
	if id, ok := b.Files["agent-id"]; ok && current != "" && strings.TrimSpace(string(id)) != current {
		return nil, errors.New("设备身份已改变，不能混用旧配对数据")
	}
	before := map[string][]byte{}
	names := []string{}
	for name := range b.Files {
		names = append(names, name)
		p := filepath.Join(a.data, name)
		old, e := limitedFile(a.path(p), 65536)
		if e != nil {
			if _, se := os.Lstat(a.path(p)); !errors.Is(se, os.ErrNotExist) {
				return nil, errors.New("原配置不可备份")
			}
		}
		before[name] = old
	}
	snapshot, _ := json.Marshal(before)
	backupPath := filepath.Join(a.data, "maintenance", "before-restore-"+time.Now().Format("20060102-150405.000000000")+".json")
	if err = a.write(backupPath, snapshot); err != nil {
		return nil, errors.New("无法保存恢复前副本")
	}
	sort.Strings(names)
	applied := []string{}
	for _, name := range names {
		if err = a.write(filepath.Join(a.data, name), b.Files[name]); err != nil {
			rollbackOK := true
			for _, n := range applied {
				if before[n] == nil {
					if e := os.Remove(a.path(filepath.Join(a.data, n))); e != nil {
						rollbackOK = false
					}
				} else if a.write(filepath.Join(a.data, n), before[n]) != nil {
					rollbackOK = false
				}
			}
			if !rollbackOK {
				return nil, errors.New("恢复失败且回退未完成，请从设备维护副本人工恢复")
			}
			return nil, errors.New("恢复失败，已回退到操作前配置")
		}
		applied = append(applied, name)
	}
	return map[string]any{"message": "已恢复配置，服务保持关闭。请核对配对与通知设置后手动启动。", "backup": backupPath}, nil
}
