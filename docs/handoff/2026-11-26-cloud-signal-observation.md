# MU300 云端信号时间同步

2026-11-26 完成 develop 同步，2026-11-27 纳入 R28 发布准备；信号改动未部署。请求来自 AirSIM 聊天的信号兼容同步。

- 固定 Agent 基线仍为 `cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`。
- 在 `patches/AirSIM-mu300.patch` 增加 `modemStatus.SignalObservedAtMS`、心跳 snapshot/event
  的对应字段及 `signal_observed_at_ms,omitempty`。定时心跳在同一锁内读取值和时间。
- 通用和 MU300 CSQ 路径都在命令完成时记录时间；未知清零，查询失败不续期。
- 本地 `signal_updated_at` 仍是旧刷新元数据，不能用作 Relay 信号采样时间。
- 没有移植 AirSIM 新版即时心跳入口：固定基线没有该入口；当前唯一生产心跳构造处已接入。
- 既有冷启动接听补丁和交接文件保留；未修改 AirSIM 工作树。

## 必须协调的 RSRP 限制

用户此前 U30 Air 的有效读数来自 `mu300dash` 的 RSRP，CSQ 可能为 99。
本次请求限定新时间字段来自成功 CSQ，因此厂商回退只保持既有 signal_dbm，
不造采样时间，也不借用上一次 CSQ 时间。新客户端应当把缺少采样时间的值视为未知。
要完整覆盖此固件，需要明确厂商 RSRP/原生 ts 能否使用同一协议及对应有效范围；
不能将本次同步当作该实机云端显示已经修复。

## 验证

- 隔离 Agent 全量 `go test -mod=vendor -race ./...`、`go vet -mod=vendor ./...` 通过。
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -mod=vendor` 通过。
- 新回归 `mu300_signal_observation_test.go` 覆盖有效、未知、失败、过期不续期、重复 HTTPS
  心跳、厂商回退无假时间、恢复、状态接口与通话中跳过查询。
- 固定归档重新应用补丁，与测试副本逐文件一致；其余 97 份原有 Agent/fixture 文件
  与修改前一致（含昨日冷启动接听修复）。
- 未安装、未发布、未接触设备/真实云端，无通话/SMS/录音测试。
