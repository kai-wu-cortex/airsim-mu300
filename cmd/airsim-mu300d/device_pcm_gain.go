package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type pcmGainSettings struct {
	Downlink *int `json:"downlink_gain_db"`
	Uplink   *int `json:"uplink_gain_db"`
}

func decodePCMGain(payload string) (pcmGainSettings, error) {
	var s pcmGainSettings
	d := json.NewDecoder(strings.NewReader(payload))
	d.DisallowUnknownFields()
	if len(payload) > 1024 || d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || s.Downlink == nil || s.Uplink == nil || *s.Downlink < 0 || *s.Downlink > 12 || *s.Uplink < 0 || *s.Uplink > 12 {
		return s, errors.New("请分别选择 0～12 dB 的整数音量增益")
	}
	return s, nil
}

func (a *deviceAdmin) pcmGainStatus() (map[string]any, error) {
	config, err := a.uci("airsim")
	if err != nil {
		return nil, errors.New("无法读取音量设置，请刷新后重试")
	}
	main := config["main"]
	if main == nil || main[".type"] != "service" {
		return nil, errors.New("AirSIM 服务配置不可用")
	}
	result := map[string]any{}
	for _, direction := range []string{"downlink", "uplink"} {
		key := direction + "_gain_db"
		value := 0
		if raw, exists := main["pcm_"+key]; exists {
			s, ok := raw.(string)
			v, e := strconv.Atoi(s)
			if !ok || e != nil || v < 0 || v > 12 || strconv.Itoa(v) != s {
				return nil, errors.New("已有音量配置无效，请检查 /etc/config/airsim")
			}
			value = v
		}
		result[key] = value
	}
	return map[string]any{"settings": result}, nil
}

// Admission is synchronous under deviceManagement's exclusive runtime lock
// and native call/media/SMS idle check. Only the two known options can change.
func (a *deviceAdmin) pcmGainSave(payload string) (map[string]any, error) {
	s, err := decodePCMGain(payload)
	if err != nil {
		return nil, err
	}
	if _, err = a.pcmGainStatus(); err != nil {
		return nil, err
	}
	changes, err := a.call(3, "uci", "changes", "airsim")
	if err != nil || len(bytes.TrimSpace(changes)) != 0 {
		return nil, errors.New("AirSIM 有未提交的配置或无法读取，请先处理后再保存音量")
	}
	path := a.path("/etc/config/airsim")
	before, err := limitedFile(path, 262144)
	if err != nil {
		return nil, errors.New("无法读取原配置，音量未保存")
	}
	stage, err := os.MkdirTemp("", "airsim-pcm-gain-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, "airsim"), before, 0600); err != nil {
		return nil, err
	}
	if err = os.Mkdir(filepath.Join(stage, "delta"), 0700); err != nil {
		return nil, err
	}
	batch := fmt.Sprintf("set airsim.main.pcm_downlink_gain_db='%d'\nset airsim.main.pcm_uplink_gain_db='%d'\ncommit airsim\n", *s.Downlink, *s.Uplink)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []string{"-c", stage, "-P", filepath.Join(stage, "delta")}
	if _, err = a.run(ctx, "uci", append(args, "batch"), []byte(batch)); err != nil {
		return nil, errors.New("音量配置校验失败，原设置保留")
	}
	for key, value := range map[string]int{"pcm_downlink_gain_db": *s.Downlink, "pcm_uplink_gain_db": *s.Uplink} {
		out, e := a.run(ctx, "uci", append(args, "get", "airsim.main."+key), nil)
		if e != nil || strings.TrimSpace(string(out)) != strconv.Itoa(value) {
			return nil, errors.New("音量配置核对失败，原设置保留")
		}
	}
	after, err := limitedFile(filepath.Join(stage, "airsim"), 262144)
	if err != nil {
		return nil, err
	}
	current, err := limitedFile(path, 262144)
	changes, changeErr := a.call(3, "uci", "changes", "airsim")
	if err != nil || !bytes.Equal(before, current) || changeErr != nil || len(bytes.TrimSpace(changes)) != 0 {
		return nil, errors.New("配置已发生变化，请刷新后再保存音量")
	}
	if err = a.write("/etc/config/airsim", after); err != nil {
		return nil, errors.New("音量保存未确认，请刷新核对")
	}
	return map[string]any{"settings": s, "message": "音量已保存，下次启动 AirSIM 服务生效。"}, nil
}
