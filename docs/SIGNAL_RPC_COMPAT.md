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
