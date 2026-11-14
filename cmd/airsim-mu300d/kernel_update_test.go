package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type kernelMember struct {
	name, body string
	kind       byte
}

func goodKernelMembers() []kernelMember {
	return []kernelMember{{"./devices", "f50\nu30air\n", tar.TypeReg}, {"./kernel.release", "7.3.1\n", tar.TypeReg}, {"./Image", "kernel fixture", tar.TypeReg}, {"./ramdisk-generic.lz4", "ramdisk fixture", tar.TypeReg}, {"./modules/test.ko", "module fixture", tar.TypeReg}}
}
func kernelTar(t *testing.T, members []kernelMember) []byte {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	w := tar.NewWriter(z)
	for _, m := range members {
		h := &tar.Header{Name: m.name, Mode: 0600, Typeflag: m.kind, Size: int64(len(m.body))}
		if m.kind != tar.TypeReg {
			h.Size = 0
			h.Linkname = "/tmp/escape"
		}
		if e := w.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if h.Size > 0 {
			if _, e := w.Write([]byte(m.body)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestKernel73ArchiveProtection(t *testing.T) {
	if e := inspect73Archive(bytes.NewReader(kernelTar(t, goodKernelMembers()))); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func([]kernelMember) []kernelMember{
		"F50 only":        func(m []kernelMember) []kernelMember { m[0].body = "f50"; return m },
		"no devices":      func(m []kernelMember) []kernelMember { return m[1:] },
		"wrong series":    func(m []kernelMember) []kernelMember { m[1].body = "7.2.8"; return m },
		"not future 7.x":  func(m []kernelMember) []kernelMember { m[1].body = "7.30.1"; return m },
		"no modules":      func(m []kernelMember) []kernelMember { return m[:4] },
		"empty image":     func(m []kernelMember) []kernelMember { m[2].body = ""; return m },
		"image directory": func(m []kernelMember) []kernelMember { m[2].kind = tar.TypeDir; return m },
		"symlink":         func(m []kernelMember) []kernelMember { m[4].kind = tar.TypeSymlink; return m },
		"hardlink":        func(m []kernelMember) []kernelMember { m[4].kind = tar.TypeLink; return m },
		"duplicate":       func(m []kernelMember) []kernelMember { return append(m, m[0]) },
		"traversal":       func(m []kernelMember) []kernelMember { m[4].name = "./modules/../../outside"; return m },
		"absolute":        func(m []kernelMember) []kernelMember { m[4].name = "/outside"; return m },
		"native layout":   func(m []kernelMember) []kernelMember { m[0].name = "devices"; return m },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if inspect73Archive(bytes.NewReader(kernelTar(t, change(goodKernelMembers())))) == nil {
				t.Fatal("accepted unsafe bundle")
			}
		})
	}
	b := kernelTar(t, goodKernelMembers())
	b[len(b)-8] ^= 1
	if inspect73Archive(bytes.NewReader(b)) == nil {
		t.Fatal("accepted corrupt gzip CRC")
	}
}
func TestKernel73RequestAndHash(t *testing.T) {
	v := kernel73Request{Release: "v2026.11.02.1", Bundle: "/tmp/mu300-kernel-7.3.tar.gz", SHA256: strings.Repeat("a", 64)}
	encode := func(v kernel73Request) string { b, _ := json.Marshal(v); return string(b) }
	if _, e := decodeKernel73(strings.NewReader(encode(v))); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{encode(v) + "{}", strings.Replace(encode(v), "v2026.11.02.1", "v1;reboot", 1), strings.Replace(encode(v), "/tmp/", "/etc/", 1), strings.Replace(encode(v), strings.Repeat("a", 64), "bad", 1), `{"extra":true}`} {
		if _, e := decodeKernel73(strings.NewReader(s)); e == nil {
			t.Fatal("accepted invalid request", s)
		}
	}
	dir := t.TempDir()
	name := filepath.Join(dir, "kernel.tar.gz")
	b := kernelTar(t, goodKernelMembers())
	os.WriteFile(name, b, 0600)
	f, e := openKernelBundle(name)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = verify73Bundle(f, fmt.Sprintf("%x", sha256.Sum256(b))); e != nil {
		t.Fatal(e)
	}
	if verify73Bundle(f, strings.Repeat("0", 64)) == nil {
		t.Fatal("accepted wrong digest")
	}
	link := filepath.Join(dir, "link")
	os.Symlink(name, link)
	if f, e := openKernelBundle(link); e == nil {
		f.Close()
		t.Fatal("accepted symlink")
	}
	if _, e := updater73Copy([]byte(`KERNELS="5.4 6.18 7.2"`)); e == nil {
		t.Fatal("accepted unpinned updater")
	}
}

// Full source integration test: opt in using publicly reproducible pinned files;
// fixtures contain no device firmware. See docs/KERNEL_SELECTION.md.
func TestKernel73PinnedNativeIntegration(t *testing.T) {
	dir := os.Getenv("AIRSIM_KERNEL_UPDATER_FIXTURES")
	if dir == "" {
		t.Skip("set AIRSIM_KERNEL_UPDATER_FIXTURES to the pinned public sources")
	}
	for _, name := range []string{"mu300-update.legacy", "mu300-update.original"} {
		t.Run(name, func(t *testing.T) {
			b, e := os.ReadFile(filepath.Join(dir, name))
			if e != nil {
				t.Fatal(e)
			}
			patched, e := updater73Copy(b)
			if e != nil {
				t.Fatal(e)
			}
			if name == "mu300-update.original" && !bytes.Equal(b, patched) {
				t.Fatal("native version changed")
			}
			if name == "mu300-update.legacy" {
				restored := strings.Replace(string(patched), `KERNELS="5.4 6.18 7.2 7.3"`, `KERNELS="5.4 6.18 7.2"`, 1)
				restored = strings.Replace(restored, "        5.4|6.18|7.2|7.3) ;;", "        5.4|6.18|7.2) ;;", 1)
				if restored != string(b) {
					t.Fatal("patch changed protection code")
				}
			}
			modified := append([]byte(nil), b...)
			modified[len(modified)/2] ^= 1
			if _, e = updater73Copy(modified); e == nil {
				t.Fatal("accepted locally modified updater")
			}
			tmp := t.TempDir()
			installed := filepath.Join(tmp, "installed-updater")
			if e := os.WriteFile(installed, b, 0755); e != nil {
				t.Fatal(e)
			}
			backups := filepath.Join(tmp, "backups")
			if e := persist73Updater(installed, backups, b, patched); e != nil {
				t.Fatal(e)
			}
			actual, _ := os.ReadFile(installed)
			if !bytes.Equal(actual, patched) {
				t.Fatal("patch not persisted")
			}
			if e := persist73Updater(installed, backups, patched, patched); e != nil {
				t.Fatal("repeat changed patched updater", e)
			}
			if name == "mu300-update.legacy" {
				saved, e := os.ReadFile(filepath.Join(backups, "mu300-update."+legacyUpdaterSHA+".original"))
				if e != nil || !bytes.Equal(saved, b) {
					t.Fatal("original not preserved", e)
				}
				if e := persist73Updater(installed, backups, b, patched); e == nil {
					t.Fatal("stale source accepted")
				}
			} else if _, e := os.Stat(backups); !os.IsNotExist(e) {
				t.Fatal("native support should not create backup")
			}
			script := filepath.Join(tmp, "updater")
			os.WriteFile(script, patched, 0600)
			if out, e := exec.Command("/bin/sh", "-n", script).CombinedOutput(); e != nil {
				t.Fatal(e, string(out))
			}
			// Source functions only; stub every kernel_cmd side effect. The real
			// selection, asset naming, failure restoration and device guard run.
			harness := `set -eu
export MU300_LIB=1 MU300_DISK="$2"
. "$1"
mkdir -p "$BOOTDIR"
echo 5.4 > "$KERNEL_CHOICE"
mounted_disk() { :; }
ubuntu_release() { echo 24.04; }
boot_downgrade_ok() { return 0; }
boot_update() { test "$(kernel_choice)" = 7.3 && test "$(kernel_asset)" = mu300-kernel-7.3.tar.gz; }
sync() { :; }
reboot_goes_to_android() { return 0; }
MU300_RELEASE=v2099.1
STAGE="$2/stage"
kernel_cmd 7.3
test "$(cat "$KERNEL_CHOICE")" = 7.3
echo 5.4 > "$KERNEL_CHOICE"
boot_update() { return 1; }
if (kernel_cmd 7.3); then exit 9; fi
test "$(cat "$KERNEL_CHOICE")" = 5.4
this_device() { echo u30air; }
: > "$2/list"
if bundle_runs_here unused "$2/list" "$2"; then exit 10; fi
echo ./devices > "$2/list"
untar() { printf f50 > "$MU300_DISK/devices"; }
if bundle_runs_here unused "$2/list" "$2"; then exit 11; fi
untar() { printf 'f50\nu30air\n' > "$MU300_DISK/devices"; }
bundle_runs_here unused "$2/list" "$2"
`
			cmd := exec.Command("/bin/sh", "-c", harness, "fixture", script, tmp)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("native guard/rollback regression: %v\n%s", e, out)
			}
		})
	}
}
