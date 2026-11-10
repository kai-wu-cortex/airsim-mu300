# U30 Air Watch 直连 v2（源码候选）

2026-11-10，分支 `codex/pr36-watch-support`，基于 R23；源码候选，未安装或发布。
AirSIM 客户端协议源为 PR36 集成分支的 `docs/protocols/watch-direct-v2.md`。
本仓库只改设备网关与固定 Agent 的隔离补丁；不改客户端、Worker 或 F50。

## 接入

- `/v1/health` 可返回 `watch_direct_version:2`。最终确认依据是已认证
  `POST /api/watch/status` 的签名响应和 `payload.version == 2`。
- 仅增加 `/api/watch/status`、`/api/watch/answer`、`/api/watch/hangup` 三个 POST。
  `X-AirSIM-Watch-Control: 2`、原 VoWLAN v1 请求签名与时间窗/nonce 检查均必需。
  `Kwatch = HMAC-SHA256(Kpair, "AirSIM/watch-call-only/v2")`，不会注册为普通配对密钥。
- 请求必须含 agent_id、call_id、小写 UUID 格式 call_uuid、JSON 正整数 generation。
  本版仅支持 incoming 接听；不增加 Watch 拨号、短信或任意控制权限。
- 响应 payload 为无填充 base64url JSON，proof 签名绑定请求 nonce、HTTP 状态和原始 payload。
  状态为 incoming / connecting / active / held / ended / unknown。
- Agent 首次验证精确推送 tuple 后绑定不可复用的 MU300 原生 UUID；原生槽号和号码不作身份。
  初次 ringing answer 在与原有控制共用的锁内认领父配对 owner，之后即使命令结果未知也不重发。
  已被其他设备接通的电话不能通过新 claim 接管；只有本 owner 能结束已认领目标。
  最多保存 64 个绑定，保留四小时；有明确原生终态记录才返回 ended。

### U30 的初始身份限制

本次复用现有 `/resolve` 的推送 tuple。当前纯本地模式没有生成该推送 UUID/代次，
因此没有已知 tuple 的电话返回 unknown/409，不执行 answer；客户端会在尝试直连接听前
保留 iPhone 协作路径（PR36 集成会话已确认）；一旦尝试 direct answer，不跨通道重复接听。已建立云推送 tuple 的来电可通过私网 HTTP 直连，媒体无需走云。
不能把这一版称为“完全无云的 Watch 独立接听”；纯本地 tuple 发现与传递需另行对齐协议。

## 音频

原生状态确认 active 后才签发 grant，绑定父 owner 的 SHA256 裸小写 hex、完整 tuple、
原生 instance、随机 UUID session、60 秒 attach 期限和最长四小时会话期限。
重复请求复用同一 grant，不刷新 TTL；held 不签发双向媒体。

`Kmedia = HMAC-SHA256(Kwatch, "watch-media-v2\n" + 原始grant字符串)`；媒体入口使用
`AIRSIMWATCH2 <grant> <timestamp> <nonce> <signature>\n`。入口只接纳本进程已签发的
精确 grant，并再次检查父配对、完整 tuple/instance、active 状态、时限和单 PCM 所有权。
硬件准备后发送 READY 前再次验证；与原有 VoWLAN、云端和早期媒体共享 PCM 互斥。

已附着会话不因 60 秒期限结束，但会在四小时上限、身份变化、结束、held、撤销、
连接失败或无法确认状态时关闭。只有 attach 期限内且先前 PCM 已释放才允许重连。
旧 AIRSIMVWL1 不接受 Kwatch/Kmedia，AIRSIMEM1、云端和 8 kHz PCM 格式保持原样。

媒体监视每 100 ms 发起一次 loopback 核验，单次请求最多 100 ms。Agent 使用最近的
基带快照（最多 2.5 秒），失败/缺行/过期快照视为 unknown，并与常规 AT 轮询锁隔离。
这不是“运营商挂断到设备收尾 250 ms”的保证：AT 采样、URC、原有消失确认宽限仍影响
真实端到端时延，必须单独实测。没有通过虚构 ended 来缩短界面计时。

## 配对撤销（仅供设备管理员明确操作）

新增 loopback-only 管理入口：`POST http://127.0.0.1:7581/internal/watch/revoke-parent`，
请求为 `{"owner":"<父Kpair的SHA256裸小写hex>"}`。此入口不是 Watch/general API，
不允许从 LAN 访问，也不暴露密钥。成功返回 204（重复删除幂等）。

调用会持久删除该父配对及其 Watch 授权；活动 Watch 媒体在监视周期内关闭，不发送
基带挂断命令。外部管理员清空/删除配对文件同样使 Watch 控制与媒体失效。
客户端取消配对 UI 尚未集成该入口；不能声称手机点“取消配对”已自动通知设备。
本次没有执行任何撤销操作。重启清空短期授权，父配对仍按原有方式保存。

## 状态通知与代次修复

Watch 认领通话确认 active/ended 后发布绑定 tuple 的所有权事件；原有原生通话状态
事件保留。`sendCallOwner` 现在从调用方的通话描述符显式接收 generation，不再读取
全局最新代次，因此晚到旧通话事件不会带下一通的 generation，也不必丢弃旧结束事件。

## 自动化与验收边界

- 固定跨语言向量：`internal/gateway/testdata/watch-direct-v2-vectors.json`，仅含公开
  固定测试密钥；验证 Kwatch、Kmedia、精确 grant、PCM 与响应签名。
- 网关回归：响应身份/签名、重放、错误 agent、父/派生凭据隔离、重复 grant，真实
  net.Pipe/TCP 双向数据流、重复 PCM、结束/换 instance/删除父配对/清空文件/硬时限关闭，
  attach 到期不误关已附着流、初始化中撤销不发送 READY、互斥释放。
- 隔离 Agent 回归：原子认领与重复 answer、其他父 owner 拒绝、iPhone 先接通、错误代次、
  未确认 ACK、held/outgoing 拒绝、终态后新电话不替换绑定、缺少/过期快照、旧事件代次保留。
- 已通过插件与全新隔离 Agent 的 Go race/vet、全部 30 项 Node 测试、干净固定上游重新应用补丁核验，以及 Linux ARM64 构建。
- 干净 race 回归发现旧 resolve 重启夹具在异步通知运行时替换 push 指针，已改为在锁内清空推送身份，保留原先的重启失去 tuple 测试语义；最终完整回归通过。

**全部新增硬件行为待验收**：锁屏 iPhone 与 Watch 接听、其他设备先接听后停止响铃、
双向音质、挂断/配对撤销时延、网络切换、长通话、真实短信回复；后台通知端到端也待验证。
源码测试不构成 R23 已发布包新增这些能力，不构成 F50/其他设备适配。没有 USB 操作、
安装、发布、真实电话、短信或录音。
