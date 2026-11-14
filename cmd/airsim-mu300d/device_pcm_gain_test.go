package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPCMGainRequestValidation(t *testing.T) {
	for _, p := range []string{`{}`, `null`, `{"downlink_gain_db":3}`, `{"downlink_gain_db":null,"uplink_gain_db":0}`, `{"downlink_gain_db":3.5,"uplink_gain_db":0}`, `{"downlink_gain_db":13,"uplink_gain_db":0}`, `{"downlink_gain_db":0,"uplink_gain_db":-1}`, `{"downlink_gain_db":3,"uplink_gain_db":0,"enabled":true}`, `{"downlink_gain_db":"3","uplink_gain_db":0}`, `{"downlink_gain_db":3,"uplink_gain_db":0} {}`} {
		if _, err := testAdmin(t).pcmGainSave(p); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	if !deviceReadActions["pcm_gain"] || deviceReadActions["pcm_gain_save"] || !deviceWriteActions["pcm_gain_save"] {
		t.Fatal("gain RPC scope incorrect")
	}
}

func TestPCMGainReadsLegacyAndRejectsUnknown(t *testing.T) {
	for _, tc := range []struct {
		config string
		ok     bool
	}{
		{`{"values":{"main":{".type":"service"}}}`, true},
		{`{"values":{"main":{".type":"service","pcm_downlink_gain_db":"6","pcm_uplink_gain_db":"0"}}}`, true},
		{`{"values":{}}`, false},
		{`{"values":{"main":{".type":"other"}}}`, false},
		{`{"values":{"main":{".type":"service","pcm_downlink_gain_db":"13"}}}`, false},
		{`{"values":{"main":{".type":"service","pcm_downlink_gain_db":3}}}`, false},
		{`{"values":{"main":{".type":"service","pcm_downlink_gain_db":""}}}`, false},
		{`invalid`, false},
	} {
		a := testAdmin(t)
		a.run = func(_ context.Context, name string, args []string, _ []byte) ([]byte, error) {
			if name != "ubus" || strings.Join(args[:3], " ") != "call uci get" {
				t.Fatal("unexpected read", name, args)
			}
			return []byte(tc.config), nil
		}
		r, err := a.pcmGainStatus()
		if (err == nil) != tc.ok {
			t.Fatalf("config %s: %v", tc.config, err)
		}
		if tc.ok && r["settings"].(map[string]any)["uplink_gain_db"] != 0 {
			t.Fatal("legacy default")
		}
	}
}

func TestPCMGainSaveAtomicAndPreservesConfiguration(t *testing.T) {
	for _, failure := range []string{"", "pending", "batch", "verify", "concurrent"} {
		t.Run(failure, func(t *testing.T) {
			a := testAdmin(t)
			before := "config service 'main'\n\toption enabled '0'\n\toption interface 'br-lan'\n\toption custom 'preserve-me'\n"
			putAdmin(t, a, "/etc/config/airsim", before)
			a.run = func(_ context.Context, name string, args []string, input []byte) ([]byte, error) {
				if name == "ubus" {
					return []byte(`{"values":{"main":{".type":"service"}}}`), nil
				}
				if name != "uci" {
					t.Fatal("unexpected device command", name)
				}
				if args[0] == "changes" {
					if failure == "pending" {
						return []byte("airsim.main.interface='draft'"), nil
					}
					return nil, nil
				}
				if len(args) < 5 || args[0] != "-c" || args[2] != "-P" || args[3] != filepath.Join(args[1], "delta") {
					t.Fatal("not isolated", args)
				}
				if args[4] == "batch" {
					if string(input) != "set airsim.main.pcm_downlink_gain_db='3'\nset airsim.main.pcm_uplink_gain_db='0'\ncommit airsim\n" {
						t.Fatal("unscoped UCI write", string(input))
					}
					if failure == "batch" {
						return nil, errors.New("failed")
					}
					if failure == "concurrent" {
						putAdmin(t, a, "/etc/config/airsim", before+"# other admin\n")
					}
					return nil, os.WriteFile(filepath.Join(args[1], "airsim"), []byte(before+"\toption pcm_downlink_gain_db '3'\n\toption pcm_uplink_gain_db '0'\n"), 0600)
				}
				if args[4] != "get" {
					t.Fatal(args)
				}
				if failure == "verify" {
					return []byte("12"), nil
				}
				if args[5] == "airsim.main.pcm_downlink_gain_db" {
					return []byte("3\n"), nil
				}
				return []byte("0\n"), nil
			}
			r, err := a.pcmGainSave(`{"downlink_gain_db":3,"uplink_gain_db":0}`)
			if (err == nil) != (failure == "") {
				t.Fatalf("save: %v", err)
			}
			got, _ := os.ReadFile(a.path("/etc/config/airsim"))
			if failure == "" {
				if !strings.HasPrefix(string(got), before) || !strings.Contains(string(got), "pcm_downlink_gain_db '3'") || r["settings"].(pcmGainSettings).Downlink == nil {
					t.Fatal("settings lost")
				}
			} else {
				want := before
				if failure == "concurrent" {
					want += "# other admin\n"
				}
				if string(got) != want {
					t.Fatal("failure changed original config")
				}
			}
		})
	}
}
