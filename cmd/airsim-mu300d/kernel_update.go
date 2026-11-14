package main

// Local CLI only: no rpcd method, install hook, automatic download or reboot.
// The native updater owns boot/device/slot/module rollback checks. We only add
// 7.3 to two dispatch lists in a byte-pinned legacy copy. Explicit apply also
// persists that patch, with a backup, so later native updates recognize 7.3.
import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const nativeUpdater = "/opt/mu300/bin/mu300-update"
const legacyUpdaterSHA = "d2b4594130654047d9b91cedb22e8b18b888515192073ff8a94d0cc688b74a23"
const native73UpdaterSHA = "cda543e8c077e92fffbea947a4662f571a5718ae9b82fba6491801674a6df12e"
const patched73UpdaterSHA = "97a93e4fe679caf801c48dc2a5f76783692bc525883471b3892286b42dd6e3f9"
const kernelBundleLimit = int64(1024 * 1024 * 1024)

type kernelUpdaterInfo struct {
	Status string `json:"status"`
	SHA256 string `json:"sha256"`
}

func updater73Info(b []byte) kernelUpdaterInfo {
	r := kernelUpdaterInfo{Status: "unknown", SHA256: fmt.Sprintf("%x", sha256.Sum256(b))}
	switch r.SHA256 {
	case legacyUpdaterSHA:
		r.Status = "patch_available"
	case native73UpdaterSHA:
		r.Status = "native"
	case patched73UpdaterSHA:
		r.Status = "patched"
	}
	return r
}

func updater73Copy(b []byte) ([]byte, error) {
	switch updater73Info(b).Status {
	case "native", "patched":
		return b, nil
	case "patch_available":
		s := string(b)
		for _, pair := range [][2]string{
			{"KERNELS=\"5.4 6.18 7.2\"", "KERNELS=\"5.4 6.18 7.2 7.3\""},
			{"        5.4|6.18|7.2) ;;", "        5.4|6.18|7.2|7.3) ;;"},
		} {
			if strings.Count(s, pair[0]) != 1 {
				return nil, errors.New("更新器补丁上下文不匹配")
			}
			s = strings.Replace(s, pair[0], pair[1], 1)
		}
		if fmt.Sprintf("%x", sha256.Sum256([]byte(s))) != patched73UpdaterSHA {
			return nil, errors.New("更新器补丁结果校验失败")
		}
		return []byte(s), nil
	default:
		return nil, errors.New("原生更新器版本未审查；未修改任何内核，请先核对更新器 SHA-256")
	}
}

type kernel73Request struct {
	Release string `json:"release"`
	Bundle  string `json:"bundle"`
	SHA256  string `json:"sha256"`
	Confirm bool   `json:"confirm"`
}

func decodeKernel73(r io.Reader) (kernel73Request, error) {
	var v kernel73Request
	b, err := io.ReadAll(io.LimitReader(r, 8193))
	if err != nil || len(b) > 8192 {
		return v, errors.New("内核请求过大或读取失败")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return v, errors.New("内核请求格式无效")
	}
	if !regexp.MustCompile(`^v[0-9][A-Za-z0-9._-]{0,63}$`).MatchString(v.Release) ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(v.SHA256) ||
		!regexp.MustCompile(`^/(mnt/mu300-disk/pkg|tmp)/[^/]*kernel[^/]*\.tar\.gz$`).MatchString(v.Bundle) {
		return v, errors.New("需要有效 Release、本机内核包和可信来源的 SHA-256")
	}
	for _, c := range v.Bundle {
		if c < 32 || c == 127 {
			return v, errors.New("内核包路径无效")
		}
	}
	return v, nil
}

// Validate the entire archive without extracting it. No links, duplicate names,
// traversal or special files can reach the native tar extractor. A declared
// device list is mandatory even though native tooling permits legacy F50 bundles.
func inspect73Archive(r io.Reader) error {
	z, err := gzip.NewReader(r)
	if err != nil {
		return errors.New("内核包不是有效 gzip")
	}
	defer z.Close()
	bounded := &io.LimitedReader{R: z, N: 2*kernelBundleLimit + 1}
	t := tar.NewReader(bounded)
	seen := map[string]bool{}
	regular := map[string]bool{}
	meta := map[string]string{}
	modules := 0
	for count := 0; ; count++ {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil || count >= 20000 || bounded.N <= 0 {
			return errors.New("内核包损坏或超出限制")
		}
		n := strings.TrimPrefix(h.Name, "./")
		if n == "" && h.Typeflag == tar.TypeDir {
			continue
		}
		n = strings.TrimSuffix(n, "/")
		if n == "" || path.IsAbs(n) || path.Clean(n) != n || n == ".." || strings.HasPrefix(n, "../") || seen[n] {
			return errors.New("内核包含重复或不安全路径")
		}
		seen[n] = true
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > kernelBundleLimit {
			return errors.New("内核包含链接或不支持的文件")
		}
		regular[n] = true
		if (n == "devices" || n == "kernel.release" || n == "Image" || n == "ramdisk-generic.lz4") && h.Name != "./"+n {
			return errors.New("内核包路径布局与原生更新器不匹配")
		}
		if n == "devices" || n == "kernel.release" {
			if h.Size > 4096 {
				return errors.New("内核元数据过大")
			}
			b, e := io.ReadAll(t)
			if e != nil {
				return errors.New("内核元数据损坏")
			}
			meta[n] = strings.TrimSpace(string(b))
		}
		if (n == "Image" || n == "ramdisk-generic.lz4") && h.Size == 0 {
			return errors.New("内核或 ramdisk 为空")
		}
		if strings.HasPrefix(n, "modules/") && strings.HasSuffix(n, ".ko") {
			if h.Size == 0 {
				return errors.New("内核模块为空")
			}
			modules++
		}
	}
	// Consume the gzip footer too; tar EOF alone does not verify its CRC.
	if _, err := io.Copy(io.Discard, bounded); err != nil || bounded.N <= 0 {
		return errors.New("内核包校验失败或过大")
	}
	u30 := false
	for _, d := range strings.Fields(meta["devices"]) {
		if d == "u30air" {
			u30 = true
		}
	}
	if !u30 {
		return errors.New("内核包未声明支持 u30air，拒绝使用 F50 专用包")
	}
	if !regexp.MustCompile(`^7\.3\.[0-9]+[A-Za-z0-9._+-]*$`).MatchString(meta["kernel.release"]) {
		return errors.New("内核包 kernel.release 不是 7.3 系列")
	}
	if !regular["Image"] || !regular["ramdisk-generic.lz4"] || modules == 0 {
		return errors.New("内核包缺少 Image、ramdisk 或模块")
	}
	return nil
}

func openKernelBundle(name string) (*os.File, error) {
	st, err := os.Lstat(name)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > kernelBundleLimit {
		return nil, errors.New("内核包不是有界普通文件")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		f.Close()
		return nil, errors.New("内核包在打开时改变")
	}
	return f, nil
}

func verify73Bundle(f *os.File, expected string) error {
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, kernelBundleLimit+1))
	if err != nil || n > kernelBundleLimit || fmt.Sprintf("%x", h.Sum(nil)) != expected {
		return errors.New("内核包 SHA-256 不匹配")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	return inspect73Archive(f)
}

// Preserve the old updater, then atomically publish only the reviewed patch.
// Never roll it back automatically: a successfully written 7.3 boot selection
// must remain understood by subsequent mu300-update invocations.
func persist73Updater(filename, backupDir string, original, patched []byte) error {
	current, err := limitedFile(filename, 1024*1024)
	if err != nil || !bytes.Equal(current, original) {
		return errors.New("更新器已改变，暂停切换")
	}
	if updater73Info(original).Status != "patch_available" {
		return nil
	}
	if updater73Info(patched).Status != "patched" {
		return errors.New("补丁结果未通过校验")
	}
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(backupDir)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return errors.New("更新器备份目录必须为私有目录")
	}
	backup := filepath.Join(backupDir, "mu300-update."+legacyUpdaterSHA+".original")
	if saved, e := limitedFile(backup, 1024*1024); e == nil {
		if !bytes.Equal(saved, original) {
			return errors.New("已有更新器备份不匹配")
		}
	} else {
		f, e := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(original)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err = syncKernelDir(backupDir); err != nil {
		return err
	}
	st, err = os.Lstat(filename)
	if err != nil || !st.Mode().IsRegular() {
		return errors.New("更新器不是普通文件")
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".airsim-updater-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(st.Mode().Perm()); err != nil {
		return err
	}
	if _, err = f.Write(patched); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	current, err = limitedFile(filename, 1024*1024)
	if err != nil || !bytes.Equal(current, original) {
		return errors.New("更新器已改变，保留备份并暂停")
	}
	if err = os.Rename(f.Name(), filename); err != nil {
		return err
	}
	return syncKernelDir(filepath.Dir(filename))
}

func syncKernelDir(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func kernel73CLI(mode, data string, input io.Reader, output io.Writer) error {
	if mode != "check" && mode != "apply" {
		return errors.New("用法：--kernel-7-3 check|apply（JSON 请求从标准输入读取）")
	}
	if os.Geteuid() != 0 {
		return errors.New("请通过设备 SSH 以 root 执行")
	}
	v, err := decodeKernel73(input)
	if err != nil {
		return err
	}
	if mode == "apply" && !v.Confirm {
		return errors.New("切换需要 confirm=true；check 不会切换内核")
	}
	b, err := limitedFile(nativeUpdater, 1024*1024)
	if err != nil {
		return errors.New("无法读取原生更新器")
	}
	patched, err := updater73Copy(b)
	if err != nil {
		return err
	}
	a := deviceAdmin{root: "/", data: data, run: commandDevice}
	device, err := a.call(5, "/opt/mu300/bin/mu300-device")
	if err != nil || strings.TrimSpace(string(device)) != "u30air" {
		return errors.New("仅允许已识别的 U30 Air")
	}
	f, err := openKernelBundle(v.Bundle)
	if err != nil {
		return err
	}
	defer f.Close()
	if mode == "check" {
		if err = verify73Bundle(f, v.SHA256); err != nil {
			return err
		}
		fmt.Fprintln(output, "7.3 更新器与 U30 包检查通过；未写入内核，音频与启动仍待实机验证。")
		return nil
	}
	lock, err := managementLock(data, true)
	if err != nil {
		return err
	}
	defer lock.Close()
	if legacyServiceListening() {
		return errors.New("请先关闭 AirSIM 服务")
	}
	if err = a.idle(); err != nil {
		return err
	}
	// Use a private immutable snapshot of the verified payload during the native
	// operation. The operator's local package remains intact.
	dir, err := os.MkdirTemp("/mnt/mu300-disk/pkg", ".airsim-kernel73-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	bundle := filepath.Join(dir, "mu300-kernel-7.3.tar.gz")
	dest, err := os.OpenFile(bundle, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer dest.Close()
	n, err := io.Copy(dest, io.LimitReader(f, kernelBundleLimit+1))
	if err != nil || n > kernelBundleLimit {
		return errors.New("内核包快照失败，请检查磁盘空间")
	}
	if err = dest.Sync(); err != nil {
		return err
	}
	if err = verify73Bundle(dest, v.SHA256); err != nil {
		return err
	}
	updater := filepath.Join(dir, "mu300-update")
	if err = os.WriteFile(updater, patched, 0600); err != nil {
		return err
	}
	// Recheck after copying the archive; incoming calls must not cross admission.
	if err = a.idle(); err != nil {
		return err
	}
	if err = persist73Updater(nativeUpdater, "/mnt/mu300-disk/extra/airsim/kernel-updater", b, patched); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", updater, "kernel", "7.3")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin:/opt/mu300/bin:/opt/mu300/busybox-bin", "LANG=C", "MU300_RELEASE=" + v.Release, "MU300_KERNEL_BUNDLE=" + bundle}
	cmd.Stdin = nil
	cmd.Stdout = output
	cmd.Stderr = output
	// No timeout/retry: killing a boot write on a timer is unsafe. Native locking,
	// traps, device ramdisk preservation and rollback-boot are unchanged.
	if err = cmd.Run(); err != nil {
		return errors.New("原生内核更新未成功；请查看输出，勿自动重试或重启")
	}
	fmt.Fprintln(output, "7.3 内核写入流程已结束；没有重启。请先检查原生结果与回退信息，再单独安排启动和音频验收。")
	return nil
}
