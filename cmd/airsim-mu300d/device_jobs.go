package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

var maintenanceJobID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type maintenanceJob struct {
	ID      string         `json:"id"`
	Action  string         `json:"action"`
	Started int64          `json:"started"`
	Done    bool           `json:"done"`
	Result  map[string]any `json:"result,omitempty"`
}

func (a *deviceAdmin) launchJob(r deviceRequest, lock *os.File) (map[string]any, error) {
	random := make([]byte, 16)
	if _, e := rand.Read(random); e != nil {
		return nil, e
	}
	id := hex.EncodeToString(random)
	job := maintenanceJob{ID: id, Action: r.Action, Started: time.Now().Unix()}
	raw, _ := json.Marshal(job)
	if e := a.write(filepath.Join(a.data, "maintenance-job.json"), raw); e != nil {
		return nil, e
	}
	started := false
	defer func() {
		if !started {
			job.Done = true
			job.Result = map[string]any{"ok": false, "error": "维护任务未启动，尚未执行设备操作"}
			raw, _ := json.Marshal(job)
			_ = a.write(filepath.Join(a.data, "maintenance-job.json"), raw)
		}
	}()
	executable, e := os.Executable()
	if e != nil {
		return nil, e
	}
	request, e := os.CreateTemp(a.path(a.data), ".maintenance-input-*")
	if e != nil {
		return nil, e
	}
	defer request.Close()
	defer os.Remove(request.Name())
	if e = request.Chmod(0600); e != nil {
		return nil, e
	}
	if e = json.NewEncoder(request).Encode(r); e != nil {
		return nil, e
	}
	if _, e = request.Seek(0, 0); e != nil {
		return nil, e
	}
	cmd := exec.Command(executable, "--device-worker", id, "--data", a.data)
	cmd.Stdin = request
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = cmd.Start(); e != nil {
		job.Done = true
		job.Result = map[string]any{"ok": false, "error": "无法启动维护任务；尚未执行设备操作"}
		raw, _ = json.Marshal(job)
		_ = a.write(filepath.Join(a.data, "maintenance-job.json"), raw)
		return nil, errors.New("无法启动维护任务")
	}
	started = true
	_ = cmd.Process.Release()
	return map[string]any{"pending": true, "id": id, "message": "维护任务已接收。请等待结果，断线或超时后先刷新状态，不要重复提交。"}, nil
}
func (a *deviceAdmin) jobStatus() (map[string]any, error) {
	b, e := limitedFile(a.path(filepath.Join(a.data, "maintenance-job.json")), 1<<20)
	if e != nil {
		return map[string]any{"none": true}, nil
	}
	var job maintenanceJob
	if json.Unmarshal(b, &job) != nil {
		return nil, errors.New("维护记录无效")
	}
	return map[string]any{"job": job, "uncertain": !job.Done && time.Now().Unix()-job.Started > 300}, nil
}
func deviceWorker(id, data string) {
	if !maintenanceJobID.MatchString(id) {
		return
	}
	// FD 3 inherits the parent's exclusive runtime lock without an admission gap.
	lock := os.NewFile(3, "management-lock")
	if lock == nil {
		return
	}
	defer lock.Close()
	// Ensure a valid regular descriptor before accessing devices.
	if s, e := lock.Stat(); e != nil || !s.Mode().IsRegular() {
		return
	}
	a := &deviceAdmin{root: "/", data: data, run: commandDevice, sleep: time.Sleep}
	var r deviceRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil || decoder.Decode(new(any)) != io.EOF || !r.Confirm || !deviceWriteActions[r.Action] || r.Action == "backup_export" {
		return
	}
	raw, e := limitedFile(filepath.Join(data, "maintenance-job.json"), 65536)
	if e != nil {
		return
	}
	var job maintenanceJob
	if json.Unmarshal(raw, &job) != nil || job.ID != id || job.Done {
		return
	}
	var result map[string]any
	if e = a.idle(); e == nil {
		result, e = a.dispatch(r)
	}
	if e != nil {
		result = map[string]any{"ok": false, "error": e.Error()}
	} else {
		if result == nil {
			result = map[string]any{}
		}
		result["ok"] = true
	}
	job.Done = true
	job.Result = result
	raw, _ = json.Marshal(job)
	_ = a.write(filepath.Join(data, "maintenance-job.json"), raw)
}
