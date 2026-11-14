package main

// Read-only inventory inspired by shangjie1998/airsim-f50-openwrt
// @ 4df1116997425ae4b91d44cd6121bb25c836162c. Never loads modules, invokes the
// updater, repairs metadata or contacts hardware. See docs/KERNEL_SELECTION.md.
import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const verifiedAudioKernel = "5.4.254-gb50db5b6224c"
const verifiedAudioConfig = "1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3"
const audioModulesDir = "/mnt/mu300-disk/extra/airsim-audio/modules"

type kernelChoice struct {
	Series string `json:"series"`
	Audio  string `json:"audio"`
}
type kernelBundle struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type audioInventory struct {
	Directory string `json:"directory"`
	Kernel    string `json:"kernel"`
	Modules   int    `json:"modules"`
	Status    string `json:"status"`
}
type kernelInventory struct {
	OK                 bool              `json:"ok"`
	Error              string            `json:"error,omitempty"`
	Kernel             string            `json:"kernel"`
	BootSelection      string            `json:"boot_selection"`
	InstalledKernel    string            `json:"installed_kernel"`
	InstalledTag       string            `json:"installed_tag"`
	ConfigSHA256       string            `json:"config_sha256"`
	ProfileStatus      string            `json:"profile_status"`
	BundledAudio       bool              `json:"bundled_audio"`
	ReservationMissing string            `json:"reservation_missing"`
	Choices            []kernelChoice    `json:"choices"`
	Bundles            []kernelBundle    `json:"bundles"`
	Audio              []audioInventory  `json:"audio"`
	Updater73          kernelUpdaterInfo `json:"updater_73"`
}

func limitedFile(path string, max int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		return nil, fmt.Errorf("file changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, fmt.Errorf("unreadable or oversized file")
	}
	return b, nil
}

func kernelConfigDigest(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return ""
	}
	defer z.Close()
	b, err := io.ReadAll(io.LimitReader(z, 4*1024*1024+1))
	if err != nil || len(b) > 4*1024*1024 {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "CONFIG_") || strings.HasPrefix(line, "# CONFIG_") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Strings(lines)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "\n")+"\n")))
}

func audioProfileStatus(kernel, config string) string {
	if kernel != verifiedAudioKernel {
		return "unsupported_kernel"
	}
	if config == "" {
		return "config_unknown"
	}
	if config != verifiedAudioConfig {
		return "config_mismatch"
	}
	return "profile_match"
}

func moduleMagic(path string) string {
	b, err := limitedFile(path, 32*1024*1024)
	if err != nil {
		return ""
	}
	f, err := elf.NewFile(bytes.NewReader(b))
	if err != nil {
		return ""
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB || f.Machine != elf.EM_AARCH64 || f.Type != elf.ET_REL {
		return ""
	}
	s := f.Section(".modinfo")
	if s == nil || s.Size > 1024*1024 {
		return ""
	}
	info, err := s.Data()
	if err != nil {
		return ""
	}
	magic := ""
	for _, part := range strings.Split(string(info), "\x00") {
		if strings.HasPrefix(part, "vermagic=") {
			if magic != "" {
				return ""
			}
			magic = strings.TrimPrefix(part, "vermagic=")
		}
	}
	return magic
}

func inspectAudioDirectory(path, name, kernel, config string) audioInventory {
	a := audioInventory{Directory: name, Status: "metadata_unknown"}
	b, err := limitedFile(filepath.Join(path, "KERNEL"), 8192)
	metadata := map[string]string{}
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok {
				if _, exists := metadata[key]; exists {
					return a
				}
				metadata[key] = value
			}
		}
	}
	a.Kernel = metadata["KREL"]
	modules, _ := filepath.Glob(filepath.Join(path, "*.ko"))
	a.Modules = len(modules)
	if a.Kernel == "" || metadata["CONFIG"] == "" {
		return a
	}
	if a.Kernel != kernel || (name != kernel && !strings.HasPrefix(name, kernel+"+")) {
		a.Status = "other_kernel"
		return a
	}
	if config == "" {
		a.Status = "config_unknown"
		return a
	}
	if metadata["CONFIG"] != config {
		a.Status = "config_mismatch"
		return a
	}
	if len(modules) == 0 || len(modules) > 128 {
		a.Status = "modules_invalid"
		return a
	}
	magic := ""
	reserve := false
	for _, path := range modules {
		m := moduleMagic(path)
		fields := strings.Fields(m)
		if len(fields) == 0 || fields[0] != kernel || (magic != "" && m != magic) {
			a.Status = "vermagic_mismatch"
			return a
		}
		magic = m
		reserve = reserve || filepath.Base(path) == "audio_reserve.ko"
	}
	if !reserve {
		a.Status = "modules_invalid"
		return a
	}
	// Metadata is not provenance or a symbol CRC audit, even for the known kernel.
	a.Status = "metadata_match_unverified"
	return a
}

func inspectKernel(root string) kernelInventory {
	path := func(p string) string { return filepath.Join(root, p) }
	read := func(p string) string { b, _ := limitedFile(path(p), 4096); return strings.TrimSpace(string(b)) }
	r := kernelInventory{
		Kernel: read("/proc/sys/kernel/osrelease"), BootSelection: read("/mnt/mu300-disk/boot/kernel"),
		InstalledKernel: read("/mnt/mu300-disk/boot/installed.kernel"), InstalledTag: read("/mnt/mu300-disk/boot/installed.tag"),
		ReservationMissing: read("/sys/module/audio_reserve/parameters/missing"),
		Choices: []kernelChoice{
			{"5.4", "本项目仅验证 5.4.254-gb50db5b6224c 与指定配置；不能按 5.4 大版本判断。"},
			{"6.18", "参考项目提供 6.18.60 音频扩展；本项目尚未提供或验收。"},
			{"7.3", "参考项目提供 7.3.1 音频扩展；本项目尚未提供或验收。"},
		}, Bundles: []kernelBundle{}, Audio: []audioInventory{},
	}
	if r.Kernel == "" {
		r.Error = "无法读取当前运行内核"
		return r
	}
	r.OK = true
	if b, err := limitedFile(path(nativeUpdater), 1024*1024); err == nil {
		r.Updater73 = updater73Info(b)
	} else {
		r.Updater73.Status = "missing"
	}
	r.ConfigSHA256 = kernelConfigDigest(path("/proc/config.gz"))
	r.ProfileStatus = audioProfileStatus(r.Kernel, r.ConfigSHA256)
	if st, err := os.Stat(path("/usr/lib/airsim/deps/airsim-audio-0.1.0-r16.tar.gz")); err == nil {
		r.BundledAudio = st.Mode().IsRegular() && st.Size() > 0
	}
	for _, dir := range []string{"/mnt/mu300-disk/pkg", "/tmp"} {
		files, _ := filepath.Glob(path(dir + "/*kernel*.tar.gz"))
		for _, file := range files {
			st, err := os.Lstat(file)
			if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
				continue
			}
			r.Bundles = append(r.Bundles, kernelBundle{filepath.Join(dir, filepath.Base(file)), st.Size()})
			if len(r.Bundles) >= 64 {
				break
			}
		}
		if len(r.Bundles) >= 64 {
			break
		}
	}
	entries, _ := os.ReadDir(path(audioModulesDir))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		r.Audio = append(r.Audio, inspectAudioDirectory(path(audioModulesDir+"/"+entry.Name()), entry.Name(), r.Kernel, r.ConfigSHA256))
		if len(r.Audio) >= 16 {
			break
		}
	}
	return r
}
