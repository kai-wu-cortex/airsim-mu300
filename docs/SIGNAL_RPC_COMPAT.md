# MU300 信号为空：设备 RPC 兼容修复

2026-11-24 纳入 R27 开发预览。此前已按用户授权安装候选 `0.1.0_p20261123-r26` 到 USB U30 Air。
未修改 AirSIM 客户端、Relay、系统或内核。

## 已确认的原因

USB U30 Air 修复前桥版本为 `0.1.0-r25-pairing.1`。只读检查发现：

- Agent `/api/status` 返回 `signal_dbm: null`，同时 SIM 已注册。
- 原回退使用的 `ubus call mu300.modem signal '{"slot":"0"}'` 不存在。
- 固件实际提供 `ubus call mu300dash signal '{}'`，返回带 `ts` 的
  `sig.rsrp` / `nr.rsrp`，该次约 -76 dBm。
- R26 对应代码同样只有第一个 RPC，单纯从此前候选升级到已发布 R26 不会补上这个缺口。

连接在线与蜂窝信号是不同状态。信号字段为空能够解释客户端未知读数，不能据此认为
蜂窝未注册，或用假设的满格信号替代。

## develop 处理

继续优先 `AT+CSQ`；无有效 RSSI 时保留已有 `mu300.modem`，再尝试
固件 `mu300dash`。不直接打开串口，不新增后台高频轮询。

- 每个 RPC 有 2 秒期限、64 KiB 输出上限，不记录原生输出。
- 每次 RPC 前后检查通话状态；通话开始后丢弃样本。
- SIM 切换、SIM 状态不明或锁频变更期间暂停厂商信号读取。
- `mu300dash` 必须给出最近 60 秒内的采样时间，允许最多 5 秒时钟偏差。
- 支持 `sig`、`nr`、`lte` 的 RSRP，接受 -140 至 -30 dBm；拒绝错误响应、无效值和过期数据。
- 保持 `signal_source=vendor_rsrp`，不把 RSRP 描述为 RSSI。
- 保持 `/api/status.signal_dbm` 和现有云端心跳 `signal_dbm` 协议。

## 验证与边界

回归覆盖旧 RPC 成功不降级、新固件 RPC 回退、NR、超时预算、输出上限、通话保护、
过期数据，以及同一读数进入本地状态与模拟 HTTPS Relay 心跳。没有请求真实 Relay
发送测试事件。安装后 Agent 返回 `signal_dbm: -74`、`signal_source: vendor_rsrp` 和新鲜采样时间；
桥及 Agent 已恢复，音频 ready，配置和配对保留，未重启设备。134 个关键文件与签名候选包一致。
本地及云端的 iPhone 灵动岛显示仍待用户验收。

读取到的本地 AirSIM 客户端源码，Live Activity 的信号取自 `radio?.signalDBM`；
不能仅凭设备端修复保证所有客户端版本的纯云端灵动岛显示。如果新版 Agent 已上报
有效信号而纯云端仍为“－”，下一步核对 Relay 保存值与客户端 Live Activity 的云端
映射。此任务保持客户端代码不变。

## 2026-11-26：云端真实采样时间（R28 发布准备，未部署）

同步 AirSIM 客户端/Relay 联调协议：Agent 状态与云端心跳新增可选
`signal_observed_at_ms`（Unix 毫秒）。在成功的 `AT+CSQ` 返回后立即记录，
不采用后续 COPS/CNUM 完成时间、`signal_updated_at`、心跳发送时间或转发时间。
读取失败且没有替代读数时保留原值与原采样时间，由客户端按 300 秒自然过期；
成功返回 CSQ 99、越界值或无有效 CSQ 且没有厂商回退时清除读数与时间。
重复心跳不刷新时间。通话期间仍跳过元数据查询。

**厂商 RSRP 边界：** 既有 `vendor_rsrp` 的本地/旧心跳值保持支持，但不赋予
CSQ 的采样时间。新字段的本次同步契约要求真实 CSQ 观测；厂商 RPC 可能读缓存，
不能把查询时刻或旧 CSQ 时间附给 RSRP。此前实机正是 vendor_rsrp 路径，因而
不能据此宣布该固件的云端灵动岛问题已全部解决。若仍依赖 RSRP，需要与客户端/Relay
另行明确接受厂商真实 `ts`、度量类型及过期语义后再扩展；不伪装成 SIM 1/2。

已在本地隔离 Agent 完成正常/失败/未知采样、CSQ 恢复、厂商回退、旧样本超过
300 秒后重复 HTTPS 心跳、本地状态序列化与通话保护测试。完整 Agent race/vet、
Linux ARM64 编译及固定归档补丁重放通过。保留昨日冷启动接听改动；未修改 AirSIM
客户端/Relay 工作树，未访问设备或真实 Relay，未部署、重启、拨号或发短信。
