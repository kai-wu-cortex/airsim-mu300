package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var maintenanceSMSID = regexp.MustCompile(`^[0-9]{1,12}$`)

func (a *deviceAdmin) smsPool() (string, error) {
	for _, p := range []string{"/etc/mu300/sms-pool/msg", "/etc/mu300/sms/msg"} {
		st, e := os.Lstat(a.path(p))
		if e == nil && st.IsDir() && st.Mode()&os.ModeSymlink == 0 {
			return p, nil
		}
	}
	return "", errors.New("未找到支持的原生短信目录")
}
func (a *deviceAdmin) smsSnapshot() (string, map[string]string, error) {
	pool, e := a.smsPool()
	if e != nil {
		return "", nil, e
	}
	f, e := os.Open(a.path(pool))
	if e != nil {
		return "", nil, e
	}
	defer f.Close()
	entries, e := f.ReadDir(10001)
	if (e != nil && !errors.Is(e, io.EOF)) || len(entries) > 10000 {
		return "", nil, errors.New("短信目录无法完整读取")
	}
	hashes := map[string]string{}
	total := 0
	for _, entry := range entries {
		if !maintenanceSMSID.MatchString(entry.Name()) {
			continue
		}
		p := a.path(filepath.Join(pool, entry.Name()))
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			return "", nil, errors.New("短信文件状态不稳定，请刷新")
		}
		b, err := limitedFile(p, 69632)
		if err != nil {
			return "", nil, errors.New("短信读取失败")
		}
		total += len(b)
		if total > 16<<20 {
			return "", nil, errors.New("短信目录超过维护上限")
		}
		hashes[entry.Name()] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	raw, _ := json.Marshal(hashes)
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(pool+"\n"), raw...))), hashes, nil
}
func (a *deviceAdmin) smsMaintenance(r deviceRequest) (map[string]any, error) {
	token, hashes, e := a.smsSnapshot()
	if e != nil {
		return nil, e
	}
	if r.Action == "sms_preview" {
		return map[string]any{"count": len(hashes), "token": token}, nil
	}
	if token != r.Token || r.Token == "" {
		return nil, errors.New("短信列表已改变，请重新预览并确认")
	}
	if r.Action == "sms_mark_all" {
		raw, _ := json.Marshal(hashes)
		if e = a.write(filepath.Join(a.data, "sms-read.json"), raw); e != nil {
			return nil, e
		}
		return map[string]any{"message": "当前短信已在本地标记已读，未修改 SIM 或短信正文。", "count": len(hashes)}, nil
	}
	ids := []string{}
	for id := range hashes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	removed := 0
	for _, id := range ids {
		if removed >= 20 {
			break
		} // Bounded batches; the user must confirm the next snapshot.
		_, current, err := a.smsSnapshot()
		if err != nil || len(current) != len(hashes) {
			return nil, errors.New("短信发生变化，已停止删除；请刷新核对部分结果，不会自动重试")
		}
		for key, value := range hashes {
			if current[key] != value {
				return nil, errors.New("短信发生变化，已停止删除；请刷新核对部分结果")
			}
		}
		v, err := a.ubus("mu300dash", "sms_delete", map[string]string{"id": id})
		if err != nil || !truth(v["ok"]) {
			return nil, errors.New("删除结果未确认，已停止；请刷新列表，不会自动重试")
		}
		delete(hashes, id)
		removed++
	}
	return map[string]any{"message": "原生服务已确认删除当前批次（每批最多 20 条）。如有剩余，请重新预览确认。", "count": removed, "remaining": len(hashes)}, nil
}
func (a *deviceAdmin) smsList(page string) (map[string]any, error) {
	n, e := strconv.Atoi(page)
	if e != nil || n < 1 || n > 10000 {
		return nil, errors.New("页码无效")
	}
	v, e := a.ubus("mu300dash", "sms_list", map[string]string{"page": page})
	if e != nil {
		return nil, e
	}
	b, e := limitedFile(a.path(filepath.Join(a.data, "sms-read.json")), 2<<20)
	if e != nil {
		return v, nil
	}
	marks := map[string]string{}
	if json.Unmarshal(b, &marks) != nil || len(marks) > 10000 {
		return v, nil
	}
	pool, e := a.smsPool()
	if e != nil {
		return v, nil
	}
	adjusted := 0
	msgs, _ := v["msgs"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := fmt.Sprint(m["id"])
		if !maintenanceSMSID.MatchString(id) || marks[id] == "" || m["status"] != "unread" {
			continue
		}
		data, err := limitedFile(a.path(filepath.Join(pool, id)), 69632)
		if err == nil && marks[id] == fmt.Sprintf("%x", sha256.Sum256(data)) {
			m["status"] = "read"
			adjusted++
		}
	}
	// Native counts may cover all pages. Compute marked unread messages only when
	// the pool format is recognized, otherwise retain the native count.
	marked := 0
	for id, digest := range marks {
		if !maintenanceSMSID.MatchString(id) {
			continue
		}
		data, err := limitedFile(a.path(filepath.Join(pool, id)), 69632)
		if err == nil && digest == fmt.Sprintf("%x", sha256.Sum256(data)) && strings.Contains("\n"+strings.SplitN(string(data), "\n\n", 2)[0]+"\n", "\nstatus: unread\n") {
			marked++
		}
	}
	if marked < adjusted {
		marked = adjusted
	}
	if unread, ok := v["unread"].(float64); ok {
		v["unread"] = max(0, unread-float64(marked))
	}
	return v, nil
}
