package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKernelFixture(t *testing.T, root, path string, data []byte) {
	t.Helper()
	p := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func gzipFixture(t *testing.T, data string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if _, err := z.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestKernelConfigDigest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.gz")
	for _, text := range []string{"# heading\nCONFIG_Z=y\n# CONFIG_A is not set\nCONFIG_B=m\n", "CONFIG_B=m\nCONFIG_Z=y\n# CONFIG_A is not set\n"} {
		writeKernelFixture(t, root, "config.gz", gzipFixture(t, text))
		want := fmt.Sprintf("%x", sha256.Sum256([]byte("# CONFIG_A is not set\nCONFIG_B=m\nCONFIG_Z=y\n")))
		if got := kernelConfigDigest(path); got != want {
			t.Fatalf("digest=%s want=%s", got, want)
		}
	}
	for _, b := range [][]byte{[]byte("bad gzip"), gzipFixture(t, "# no config\n"), gzipFixture(t, strings.Repeat("CONFIG_X=y\n", 500000))} {
		writeKernelFixture(t, root, "config.gz", b)
		if got := kernelConfigDigest(path); got != "" {
			t.Fatalf("invalid config accepted: %s", got)
		}
	}
	b := gzipFixture(t, "CONFIG_X=y\n")
	writeKernelFixture(t, root, "config.gz", b[:len(b)-4])
	if kernelConfigDigest(path) != "" {
		t.Fatal("truncated gzip accepted")
	}
}
func TestAudioProfileDoesNotAcceptVersionAlone(t *testing.T) {
	for _, tc := range []struct{ k, c, w string }{
		{verifiedAudioKernel, verifiedAudioConfig, "profile_match"},
		{verifiedAudioKernel, "wrong", "config_mismatch"},
		{verifiedAudioKernel, "", "config_unknown"},
		{"5.4.254-other", verifiedAudioConfig, "unsupported_kernel"},
		{"6.18.60", verifiedAudioConfig, "unsupported_kernel"},
		{"7.3.1", verifiedAudioConfig, "unsupported_kernel"},
	} {
		if got := audioProfileStatus(tc.k, tc.c); got != tc.w {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
}

// Minimal AArch64 relocatable ELF fixture with real .modinfo section headers.
func moduleFixture(magic string) []byte {
	names := []byte("\x00.shstrtab\x00.modinfo\x00")
	info := []byte("vermagic=" + magic + "\x00")
	b := make([]byte, 64+3*64+len(names)+len(info))
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	le := binary.LittleEndian
	le.PutUint16(b[16:], 1)
	le.PutUint16(b[18:], 183)
	le.PutUint32(b[20:], 1)
	le.PutUint64(b[40:], 64)
	le.PutUint16(b[52:], 64)
	le.PutUint16(b[58:], 64)
	le.PutUint16(b[60:], 3)
	le.PutUint16(b[62:], 1)
	for i, s := range []struct {
		name, kind   uint32
		offset, size uint64
	}{{1, 3, 256, uint64(len(names))}, {11, 1, uint64(256 + len(names)), uint64(len(info))}} {
		h := b[64+(i+1)*64:]
		le.PutUint32(h, s.name)
		le.PutUint32(h[4:], s.kind)
		le.PutUint64(h[24:], s.offset)
		le.PutUint64(h[32:], s.size)
		le.PutUint64(h[48:], 1)
	}
	copy(b[256:], names)
	copy(b[256+len(names):], info)
	return b
}
func TestAudioInventoryRejectsFalseMatches(t *testing.T) {
	for _, tc := range []struct{ name, kernel, config, dir, magic, status string }{
		{"metadata match is still unverified", verifiedAudioKernel, "cfg", verifiedAudioKernel, verifiedAudioKernel + " SMP modversions aarch64", "metadata_match_unverified"},
		{"renamed directory", "6.18.60", "cfg", verifiedAudioKernel, "6.18.60 SMP", "other_kernel"},
		{"config mismatch", verifiedAudioKernel, "wrong", verifiedAudioKernel, verifiedAudioKernel + " SMP", "config_mismatch"},
		{"wrong module", verifiedAudioKernel, "cfg", verifiedAudioKernel, "6.18.60 SMP", "vermagic_mismatch"},
		{"unknown config", verifiedAudioKernel, "", verifiedAudioKernel, verifiedAudioKernel + " SMP", "config_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeKernelFixture(t, root, "KERNEL", []byte("KREL="+tc.kernel+"\nCONFIG=cfg\n"))
			writeKernelFixture(t, root, "audio_reserve.ko", moduleFixture(tc.magic))
			got := inspectAudioDirectory(root, tc.dir, verifiedAudioKernel, tc.config)
			if got.Status != tc.status {
				t.Fatalf("%+v", got)
			}
		})
	}
	root := t.TempDir()
	writeKernelFixture(t, root, "KERNEL", []byte("KREL="+verifiedAudioKernel+"\nCONFIG=cfg\n"))
	writeKernelFixture(t, root, "audio_reserve.ko", moduleFixture(verifiedAudioKernel+" SMP"))
	writeKernelFixture(t, root, "other.ko", moduleFixture(verifiedAudioKernel+" SMP modversions"))
	if got := inspectAudioDirectory(root, verifiedAudioKernel, verifiedAudioKernel, "cfg"); got.Status != "vermagic_mismatch" {
		t.Fatalf("mixed vermagic: %+v", got)
	}
	writeKernelFixture(t, root, "other.ko", []byte("vermagic="+verifiedAudioKernel))
	if got := inspectAudioDirectory(root, verifiedAudioKernel, verifiedAudioKernel, "cfg"); got.Status != "vermagic_mismatch" {
		t.Fatalf("non ELF: %+v", got)
	}
	writeKernelFixture(t, root, "KERNEL", []byte("KREL="+verifiedAudioKernel+"\nCONFIG=wrong\nCONFIG=cfg\n"))
	if got := inspectAudioDirectory(root, verifiedAudioKernel, verifiedAudioKernel, "cfg"); got.Status != "metadata_unknown" {
		t.Fatalf("ambiguous metadata: %+v", got)
	}
}
func TestKernelInventoryReadOnlyAndUnknown(t *testing.T) {
	root := t.TempDir()
	if r := inspectKernel(root); r.OK || r.Error == "" {
		t.Fatalf("missing kernel: %+v", r)
	}
	writeKernelFixture(t, root, "/proc/sys/kernel/osrelease", []byte("6.18.60\n"))
	writeKernelFixture(t, root, "/mnt/mu300-disk/boot/kernel", []byte("5.4\n"))
	writeKernelFixture(t, root, "/mnt/mu300-disk/boot/installed.kernel", []byte("7.3\n"))
	writeKernelFixture(t, root, "/tmp/mu300-kernel.tar.gz", []byte("archive fixture"))
	writeKernelFixture(t, root, "/tmp/empty-kernel.tar.gz", nil)
	writeKernelFixture(t, root, "/tmp/unrelated.tar.gz", []byte("ignored"))
	if err := os.Symlink(filepath.Join(root, "tmp/mu300-kernel.tar.gz"), filepath.Join(root, "tmp/link-kernel.tar.gz")); err != nil {
		t.Fatal(err)
	}
	r := inspectKernel(root)
	if !r.OK || r.Kernel != "6.18.60" || r.BootSelection != "5.4" || r.InstalledKernel != "7.3" || r.ProfileStatus != "unsupported_kernel" {
		t.Fatalf("%+v", r)
	}
	if len(r.Bundles) != 1 || r.Bundles[0].Path != "/tmp/mu300-kernel.tar.gz" {
		t.Fatalf("bundles: %+v", r.Bundles)
	}
	if len(r.Choices) != 3 || len(r.Audio) != 0 || r.BundledAudio {
		t.Fatalf("%+v", r)
	}
	b, _ := os.ReadFile(filepath.Join(root, "mnt/mu300-disk/boot/kernel"))
	if string(b) != "5.4\n" {
		t.Fatal("boot modified")
	}
}
