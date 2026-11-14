# PR36 Watch v2：U30 Air 设备端评估

日期：2026-11-10。基线：R23 / d4cc2d7；Agent 固定源仍为
`cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`，应用本仓库补丁。
工作分支：`codex/pr36-watch-support`。本文是实现前评估，不是已发布能力或最终线协议。

后续状态：精确协议已收到，源码候选实现与验收边界见 [WATCH_DIRECT_V2](../WATCH_DIRECT_V2.md)。
以下保留实现前评估；其中“待提供/尚未修改”描述的是评估时点。

## 范围与当前证据

用户在 AirSIM 会话授权 PR36 的 U30Air 同步支持。本阶段读取代码并设计设备侧适配，
不改 AirSIM iOS/Android、上游工作树，不操作 USB、不安装、不发布、不真实拨号或发短信。
已发布 R23 的既有云端、VoWLAN、早期媒体和缓冲策略须保持兼容。
目标客户端在独立 `codex/pr36-watch-integration` 分支整合；精确 v2 文档尚待提供。

已知硬件基础为 U30 Air / aarch64 / OpenWrt 25.12.6，内核
`5.4.254-gb50db5b6224c`。本阶段没有读取设备现状；当前连接、活动通话均未知。
硬件控制仍由 mu300-atd 承担，音频由现有桥与 DSP/ALSA 实现；不新增硬件所有者。

## 当前入口与缺口

| 代码 | 已有行为 | v2 不能直接沿用的部分 |
| --- | --- | --- |
| `internal/gateway/gateway.go` | HMAC、时间窗、nonce 缓存、父配对密钥存储和认证路由 | 全权限配对只有 Add，无 Watch scope 和撤销接口；不得将 Watch 密钥加入父密钥列表 |
| `Gateway.ServePCM` | `AIRSIMVWL1` 验证后代理到 loopback PCM；单媒体会话 | 签名没有通话身份；Watch 限权凭据不得通过此旧入口 |
| `internal/gateway/early_media.go` | 30 秒 owner/call/session 租约、签名 proof、入口复查、持续信令与失效取消 | 仅 call_id，没有 UUID/generation；仅 recvonly，非通用双向许可；现有票据不是一次性票据 |
| `early_media_mu300.go` 与 Agent `mu300_early_media.go` | 网关派生可信 owner，Agent 核对拨号日志和新鲜拨出信令 | 只适用拨出；来电 Watch 接听不能用拨号日志推断所有者 |
| `cloud_early_media.go` | loopback 限制、完整云端 tuple、250 ms 信令复查、取消后释放 | 内部信任边界，不能直接开放到 Watch/LAN |
| Agent `mu300_resolve.go` | 来电 call ID/UUID/代次核对及新鲜基带快照 | resolve 不校验配对 owner，也不签发媒体许可 |
| Agent `push.go` / `cloud_call.go` | Watch token 注册及云端 watch/iphone 所有权事件 | 不代表已支持 Watch 独立 VoWLAN；事件与媒体权限必须分开 |
| 网关短信路由 | 已签名请求转发到现有 Agent 短信接口 | Watch 限权凭据不可自动获得整套短信/设置权限；回复先走客户端现有协作路径，最终范围待协议 |

`sendCallOwner(callID, callUUID, ...)` 当前直接取 `lastGeneration`，没有与传入 ID/UUID
同锁核对。异步旧事件可能携带后续通话代次；后续实施需把事件身份快照与目标绑定，
不得用“最新代次”补齐旧通话。此项是代码发现，尚未修改或实机复现。

## 实施边界（待精确协议冻结）

1. 分离父配对凭据、Watch 限权控制凭据和每通话短期媒体授权；认证返回可信 owner/scope，
   不接受请求自报 owner。父凭据可走原路径；Watch 凭据只允许明确列出的动作。
2. 新媒体票据绑定 agent、call ID、UUID、generation、父配对 owner、session、方向、
   格式与有效期。新入口在打开硬件前、初始化结束发 READY 前再次确认全部身份及状态。
   接听命令 ACK 不能替代基带已接通证据；held、未知或过期快照不授权双向媒体。
3. 复用单 PCM 所有权与先关闭早期采集再启动双向的顺序。通话结束、身份变化、撤销配对、
   超时和服务关闭均终止已有流并撤销票据；迟到连接不能复活音频。并发撤销/打开需原子检查。
4. 新控制和媒体授权独立于现有 AIRSIMEM1、AIRSIMVWL1 及云端接口。
   不为旧接口增加接受 Watch 长期密钥的捷径，不改变 8 kHz PCM 或 R23 缓冲。
5. 仅在完整实现及后端就绪时声明准确能力；能力缺失时客户端保留 iPhone 协作路径。
   其他设备接听后的停止响铃依赖精确通话状态，不能以泛化 owner 事件清除另一通来电。
6. 只从固定上游导出隔离 Agent 再改补丁，保持客户端工作树和 F50 分支不变。

需协议明确：派生域与编码、鉴权头与动作白名单、媒体请求/响应/握手字节格式、
TTL 与续租/重复申请/单次消费语义、incoming owner 的建立、取消配对通知入口、
状态切换与暂时失联的处理、错误码、能力字段，以及客户端/设备共用的固定测试向量。

## 后续验证矩阵

- 非父 owner、跨 agent/call/UUID/generation、篡改/过期/重放、撤销后的控制与媒体全部拒绝。
- 未接通/held/基带查询失败不能开启 duplex；旧媒体票据不能用于下一通相同号码电话。
- 撤销或挂断发生在后端打开前、初始化中、READY 前、活跃流中均释放资源；迟到 ACK 不恢复流。
- 并发 iPhone/Watch 抢占、重复 answer/票据、连接断开重连保持明确所有权；不误挂另一通电话。
- 原 VoWLAN、云端、早期媒体转换、短信、配对、缓冲测试保持通过；补跨语言协议向量。
- 发布前仍须 Go race/vet、隔离 Agent、所有 Node、包/源码/许可及只读设备预检。
- 真机验收另记：Watch 锁屏接听、其他设备接听后停止响铃、双向听感、挂断释放、短信回复、
  取消配对与网络切换。目前以上新增能力全部 NOT TESTED；本次代码评估没有硬件验收结论。


## 2026-11-13 增量核对结果（develop，未发布）

收到 AirSIM `5904e0e` 的协议交接后，只核对并修复 U30 固定 Agent 补丁与网关测试：

1. 原 `sendIncoming` 为 MU300 原生 UUID 再生成独立 `call_uuid`。新增 profile 隔离分支复用有效 UUID，保留旧非 UUID ID、其他 profile 和已存在的重试 tuple。
2. 原 Watch status/hangup 总是先读基带，且历史终态也受最新采样时限影响。已确认挂断后若基带读取失败，就不能为客户端提供终态确认。改为只对已绑定且有精确历史证据的 instance 保留终态；空列表、另一通电话、失败/陈旧快照均不作为终态证据。
3. 网关没有新增 API 或字段，补充真实签名 HTTP dispatcher 测试，验证 terminal 响应绑定原 tuple、不签发 grant；unknown/error/错误代次不返回已结束。

测试过程中曾有一项“不同 UUID”夹具误用了原 UUID；已纠正为确实不同的 UUID，保留四个 tuple 维度的逐项隔离断言。没有通过放宽实现来消除该失败。

保留 `c7bdc00`、`9f5c5ee`、本工作树既有 UI/7.3 内核适配及其他未提交变更。不修改 AirSIM 工作树；不安装、重启、拨号、发送短信、部署或发布。

本轮验证通过：插件 Go race/vet、全新隔离 Agent Go race/vet（完整包测试 8.328 秒）、65 项 Node 测试、Linux ARM64 双二进制构建及 `git diff --check`。固定基线仍为 `cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`，补丁可重新应用到干净导出树。未提交、推送、安装或发布；上述结果不替代 Watch／U30 实机验收。
