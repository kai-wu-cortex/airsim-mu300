package main

import (
	"encoding/json"
	"io"
	"os"
	"regexp"
	"unicode/utf8"
)

// Native SMS submission is asynchronous. Hold a shared runtime lock through
// job admission; afterwards native sms_job prevents maintenance until completion.
func deviceSMSSend(data string) any {
	var r struct {
		Num  string `json:"num"`
		Text string `json:"text"`
	}
	d := json.NewDecoder(io.LimitReader(os.Stdin, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || !regexp.MustCompile(`^\+?[0-9]{1,20}$`).MatchString(r.Num) || !utf8.ValidString(r.Text) || utf8.RuneCountInString(r.Text) < 1 || utf8.RuneCountInString(r.Text) > 70 {
		return map[string]any{"error": "短信参数无效"}
	}
	for _, c := range r.Text {
		if c > 0xffff || c == 0x7f || (c < 0x20 && c != '\n' && c != '\r' && c != '\t') {
			return map[string]any{"error": "短信含不支持字符"}
		}
	}
	lock, e := managementLock(data, false)
	if e != nil {
		return map[string]any{"error": "设备维护进行中，短信未提交"}
	}
	defer lock.Close()
	a := deviceAdmin{root: "/", data: data, run: commandDevice}
	v, e := a.ubus("mu300dash", "sms_send", map[string]string{"num": r.Num, "text": r.Text})
	if e != nil {
		return map[string]any{"error": "短信提交结果未知，请先查询发送任务，勿重复发送"}
	}
	return v
}
func prepareDeviceService(data string) error {
	lock, e := managementLock(data, false)
	if e != nil {
		return e
	}
	defer lock.Close()
	a := deviceAdmin{root: "/", data: data, run: commandDevice}
	_, e = a.call(90, "/usr/lib/airsim/prepare")
	return e
}
