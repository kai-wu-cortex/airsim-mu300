package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed audio_catalog.json
var audioCatalogJSON []byte

type audioCatalogProfile struct {
	ID        string `json:"id"`
	Kernel    string `json:"kernel"`
	Config    string `json:"config_sha256,omitempty"`
	Available bool   `json:"available"`
	Bundle    string `json:"bundle,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Evidence  string `json:"evidence"`
}

func catalogProfiles() []audioCatalogProfile {
	var c struct {
		Profiles []audioCatalogProfile `json:"profiles"`
	}
	_ = json.Unmarshal(audioCatalogJSON, &c)
	return c.Profiles
}
func (a *deviceAdmin) audioCatalog() (map[string]any, error) {
	kernel := inspectKernel(a.root)
	profiles := []map[string]any{}
	for _, p := range catalogProfiles() {
		eligible := p.Available && kernel.Kernel == p.Kernel && kernel.ConfigSHA256 == p.Config
		bundleOK := false
		if eligible {
			b, e := limitedFile(a.path("/usr/lib/airsim/deps/"+p.Bundle), 64<<20)
			bundleOK = e == nil && fmt.Sprintf("%x", sha256.Sum256(b)) == p.SHA256
		}
		profiles = append(profiles, map[string]any{"id": p.ID, "kernel": p.Kernel, "available": p.Available, "profile_match": eligible, "bundle_verified": bundleOK, "installable": eligible && bundleOK, "evidence": p.Evidence})
	}
	return map[string]any{"kernel": kernel.Kernel, "config_sha256": kernel.ConfigSHA256, "profiles": profiles, "note": "不切换系统内核。安装还须通过现有音频预检和模块校验；未知配置不强制加载。"}, nil
}
func (a *deviceAdmin) audioInstall(id string) (map[string]any, error) {
	var selected *audioCatalogProfile
	for _, p := range catalogProfiles() {
		if p.ID == id {
			copy := p
			selected = &copy
			break
		}
	}
	if selected == nil || !selected.Available {
		return nil, errors.New("该音频组合未完成 ABI 验证，禁止安装")
	}
	k := inspectKernel(a.root)
	if k.Kernel != selected.Kernel || k.ConfigSHA256 != selected.Config {
		return nil, errors.New("运行内核或配置与音频构建不匹配")
	}
	b, e := limitedFile(a.path("/usr/lib/airsim/deps/"+selected.Bundle), 64<<20)
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != selected.SHA256 {
		return nil, errors.New("缺少通过固定哈希验证的本地音频包，请使用本项目签名的一体包")
	}
	if _, e = a.call(15, "/usr/lib/airsim/audio-preflight"); e != nil {
		return nil, errors.New("音频预检失败，未开始安装")
	}
	if _, e = a.call(100, "/usr/lib/airsim/audio-setup", "--install"); e != nil {
		return nil, errors.New("音频准备未完成，请检查维护状态；不会自动重试或重启")
	}
	state, e := a.call(5, "/usr/lib/airsim/audio-setup", "--status")
	if e != nil {
		return nil, e
	}
	s := strings.TrimSpace(string(state))
	if s != "ready" && s != "reboot_required" {
		return nil, errors.New("音频最终状态未确认")
	}
	return map[string]any{"state": s, "message": "音频准备流程已返回。需要重启时请另行安排；服务未自动启动。"}, nil
}
func (a *deviceAdmin) selfcheck() map[string]any {
	checks := []map[string]any{}
	add := func(name string, ok bool, detail string, repair bool) {
		checks = append(checks, map[string]any{"name": name, "passed": ok, "detail": detail, "repairable": repair})
	}
	k := inspectKernel(a.root)
	add("内核与构建配置", k.ProfileStatus == "profile_match", k.Kernel+" / "+k.ProfileStatus, false)
	for _, p := range []string{"/usr/lib/airsim/airsim-agent", "/usr/lib/airsim/airsim-mu300d", "/usr/libexec/rpcd/airsim", "/etc/init.d/airsim", "/usr/lib/airsim/prepare"} {
		s, e := os.Stat(a.path(p))
		add(p, e == nil && s.Mode().IsRegular() && s.Mode()&0111 != 0, "须由签名软件包提供；不从其他固件复制覆盖", false)
	}
	_, e := a.fingerprint()
	add("硬件绑定备份", e == nil, "需要本机 eMMC CID 与设备树序列号", false)
	_, e = a.smsPool()
	add("原生短信目录", e == nil, "支持 sms-pool/msg 和 sms/msg；不创建跨系统符号链接", false)
	fw, e := limitedFile(a.path("/lib/firmware/agdsp.bin"), 32<<20)
	add("DSP 固件", e == nil && len(fw) > 0, "仅检查已有文件；保留设备固件，不自动下载或重置 DSP", false)
	add("音频内存预留", k.ReservationMissing == "0", "必须在匹配驱动加载后为 0；修复需要单独安装及重启验收", false)
	cards := a.text("/proc/asound/cards")
	add("ALSA 声卡", cards != "" && !strings.Contains(cards, "no soundcards"), "枚举成功不代表双向通话通过", false)
	for name := range backupNames {
		p := a.path(filepath.Join(a.data, name))
		s, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		regular := err == nil && s.Mode().IsRegular()
		safe := regular && s.Mode().Perm() == 0600
		add("私有配置权限："+name, safe, "仅修复本项目已存在的普通配置文件为 0600", regular && !safe)
	}
	return map[string]any{"checks": checks, "note": "自检不拨号、不录音、不加载驱动；未通过项不能用版本号或伪造 KERNEL 元数据消除。"}
}
func (a *deviceAdmin) selfcheckFix() (map[string]any, error) {
	fixed := []string{}
	for name := range backupNames {
		p := a.path(filepath.Join(a.data, name))
		s, e := os.Lstat(p)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil || !s.Mode().IsRegular() {
			return nil, errors.New("配置文件类型异常，暂停权限修复")
		}
		if s.Mode().Perm() != 0600 {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			actual, err := f.Stat()
			if err != nil || !os.SameFile(s, actual) {
				f.Close()
				return nil, errors.New("配置文件在修复前发生变化")
			}
			err = f.Chmod(0600)
			f.Close()
			if err != nil {
				return nil, err
			}
			fixed = append(fixed, name)
		}
	}
	return map[string]any{"fixed": fixed, "message": "已修复已有私有配置权限。缺失程序、固件、内存预留和 ABI 问题须按对应安装流程处理。"}, nil
}
