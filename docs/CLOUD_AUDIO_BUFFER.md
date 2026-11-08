# U30 Air 云端接收缓冲（R23 开发预览）

2026-11-08 对齐 AirSIM standalone 134 / iPhone 174 的可选缓冲协商。只在固定上游 Agent 的隔离补丁中实现；客户端、Relay、音频驱动、内核和既有 Tailscale 配对改动保持原样。

## 行为

| 策略 | U30 接收播放目标 | 队列硬上限 |
| --- | --- | --- |
| automatic / 缺失或非法配置 | 保留原 U30 策略：200 ms 起，最高 600 ms | 1000 ms |
| stable | 240 ms 起，缺帧/抖动时增加，最高 720 ms；稳定后降回 240 ms | 760 ms |
| manual | 固定 240、480、720、960 ms | 所选目标 + 40 ms |

U30 原自动策略不同于 Android 的 80–200 ms 和 iPhone 的 80–240 ms。本次没有借协商改变 U30 默认听感。稳定策略在连续 100 帧以 15–25 ms 间隔到达时降低一个 20 ms 档位，并裁剪过多积压；耗尽后重新预缓冲，不重复旧语音。队列满时丢弃最旧帧，保留时间有界。

本端「接收」指 iPhone → 认证云端 WebSocket → Agent → 本机 PCM 桥 → 蜂窝上行播放。网络格式仍是 8 kHz / mono / S16LE，每 20 ms 为 320 字节，720 ms 换算为 36 帧；不是把 320 字节套到硬件的 48 kHz capture 或 16 kHz playback。硬件桥原有 40 ms 预留与设备/网络延迟仍存在，设置值不是端到端延迟保证。

只改变接收播放，正常与接通前的采集发送不加入此缓冲，原发送路径、写入超时和背压行为保留。没有把 Android 的发送队列参数复制成 U30 的新增行为，也没有在 Relay 加等待。

## 协商与生命周期

既有认证媒体 WebSocket 的首个成功 `answer` 支持：

```json
{"audio_buffer_version":1,"audio_buffer_mode":"manual","audio_buffer_ms":720}
```

- 保留 call_id / call_uuid / generation / command_id / trace_id 校验，不产生新拨号意图。非当前通话或已经结束的 U30 控制不接受策略。
- 自定义策略要求显式完整的 call_id / call_uuid / 正数 generation 与当前描述符匹配；不能用旧兼容默认值补齐授权。
- 可选策略使用独立 JSON 原始字段解析：未知版本、错误类型和非法手动档位回退自动，不因附加字段损坏而阻断合法旧通话控制。旧版不完整身份只沿用自动策略。
- 同一通话首个成功 answer 后冻结选择；重复、迟到 answer 不重置当前队列。WebSocket 和本地 PCM 重连沿用该通快照，但清空样本并重新预缓冲。新通话重新协商。
- 接通前仍只允许原有认证下行，收到的上行不会缓存到接通之后；正常 PCM 仍要求真实 active/held，READY 不代表接听。
- 挂断/断开释放队列和分帧残片；已关闭的桥拒绝迟到连接，旧连接的完成回调不能清理新连接。

## 验证

- 固定上游 `cc2ade3124ac34bec7d8c441f4b66a57a6dfec62` 重新导出并应用补丁成功；全新隔离 Agent 的 Go race/vet 通过。
- 插件 Go race/vet、全部 30 项 Node 测试通过，包含此前 Tailscale 设置回归；Linux ARM64 Agent 候选编译通过。
- 新增 7 项 Agent 测试覆盖：默认/非法类型与四档选择、720 ms 门槛和 400 ms 到达空档、队列上限、耗尽重新预缓冲、稳定回落、身份与每通快照、内存双向字节流的播放门槛、接通前丢弃上行、发送无额外等待、重连/关闭清空、迟到连接拒绝，以及真实 WebSocket 帧在两次连接中的重复/错误身份。
- 原自动队列、早期媒体及接通状态竞争测试继续通过；测试使用合成样本，没有通话录音。

R23 首次纳入本功能。源码阶段未安装、重启、拨号、短信或录音；本次发布授权不包含这些设备操作。真实两端 Tailscale/云端网络、弱网听感、长通话、音频路由切换和三档策略的双向实测仍待用户验收。R22 及更早版本不能因此视为具有双向新缓冲。

## PR33 通知字段核对（本轮没有修改通知）

固定 Agent 目前未完整接入 PR33：

- `/v2/events/call` 的 `incomingPushEvent` 有 agent_id 和号码，但没有 slot_index。
- `/v1/events/call-owner` 的 `callOwnerPushEvent` 仍只有旧身份/owner/phase，没有新增 agent_id、direction、number、slot_index、timestamp、missed。
- MU300 当前终态通过 `/v2/events/call-state` 发送 ended；这不等同于新增未接通知。`callRecord.Missed` 虽有本地字段，不能将其不经来源/通话身份核对直接拼到异步通知。
- `sendCallOwner` 当前读取最近来电代次；后续增加通知事实时应同时按具体 UUID/generation 绑定，避免异步旧通知取到下一通的代次。

因此本轮不宣称云端未接提醒或 SIM 通知已经对齐，也不根据 Relay 未收到 active 推断 missed=true。没有引入仅 VoWLAN 限制、全局麦克风静音、零音量策略或第二套早期媒体协议。
