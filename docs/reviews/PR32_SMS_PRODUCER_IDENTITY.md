# PR32 第一批：U30 Air 短信推送设备身份

2026-11-11，基于本地 Watch v2 提交 `c7bdc00` 同步，保留该提交全部功能。
参考 AirSIM `82bd5f64554e53e7d1945e3b3f1bd0f36b005777` 中 Agent 身份修复；
固定上游基线仍为 `cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`，不升级整个客户端或 Agent。

## 发现与修复

U30 配对网关原本已通过 `prepareRegistration(reg, d.deviceID)` 强制写入
`android_agent_id`；配对响应及签名 `/v1/health` 也使用同一持久设备 ID。
客户端传来的默认标签不会覆盖它。纯本地配对继续不修改已有云端注册。

但固定 Agent 确实有上游报告的两处缺口：

- `incomingSMSPushEvent` 没有 `agent_id`。现加入该字段，由当前注册的
  `AndroidAgentID` 填入，与 U30 配对网关注入的身份一致。
- `pushManager.store` 整体替换注册配置，自动再注册省略身份会清空它。
  现先读取已有身份，仅在新注册省略时继承，在主配置与 `.last-good` 序列化/落盘前完成。
  显式新身份仍可替换；正常 U30 配对入口继续强制使用本机可信 ID。

不修改 delivery_id、SIM 标签、短信加密、ACK 或去重逻辑，不使用号码、SIM 名称、
型号或默认字符串猜测设备身份。若旧主配置和备份本来就没有可信身份，本修复不能凭空
恢复，应通过正常配对补全；不会给旧记录杜撰 agent_id。客户端旧记录迁移属于另一批。

## 回归

- 移植上游原始 JSON 推送测试：自动再注册后仍输出 agent_id，delivery_id/正文与去重不变。
- 移植上游恢复测试：检查主配置、`.last-good`、损坏主配置恢复，以及显式身份替换。
- 新增 MU300 测试：两个设备使用不同可信 fixture ID、相同 delivery ID，连续三次省略身份
  再注册并损坏主配置恢复；分别推送后原始 JSON 必须属于各自设备，SIM 1 标签、来源、
  正文、事件类型和 delivery_id 不变；同管理器重复推送仍去重，显式替换重启后仍生效。
- 增强网关测试：客户端自报的 `untrusted-client-default` 必须被本机持久身份覆盖。
- 无历史身份时，保存与重启不会生成默认身份。
- 全新固定源导出后重应用完整补丁，逐个比较 Agent Go 文件与实现副本一致；
  隔离 Agent 和插件完整 Go race/vet 通过（包含 Watch v2 与原有早期媒体/云端/SMS 回归）；
  全部 30 项 Node 测试通过，Linux ARM64 构建通过。

## 权限与未验收项

只修改本仓库测试、隔离 Agent 补丁与本文。未修改 AirSIM 工作树，未操作 USB、安装、
重启、发起真实电话/短信、部署 Worker 或发布 Release。新测试使用本机 TLS 测试服务器，
不是运营商短信发送。真实 U30 → Relay → iPhone 的短信通知归属、恢复与旧记录迁移
尚未实机验收；本次不更新 R23 发布物，也不代表 Watch v2 已实机验收。
