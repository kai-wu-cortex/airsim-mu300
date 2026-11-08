# R23 · Tailscale 配对与云端接收缓冲（开发预览）

2026-11-08，新增与 standalone 相同的手动 IP 加密配对，以及云端接收播放缓冲协商。本版保留 U30 音频一体安装、8 kHz 单声道 PCM、早期媒体、配对、号码、电量及既有短信功能。**新功能和完整安装包尚未实机验收，本版标记为 Pre-release。**

## 必须先确认兼容性

**历史版本选型：R16 适用于 1031 之后的 mu300-linux 系统版本线；R12 适用于 1031 之前的旧系统版本线，不得跨版本线混装。** 日期不能替代硬件、内核配置与音频 ABI 检查。

R23 一体包当前只提供以下 U30 Air 音频配置：

| 项目 | 范围 |
| --- | --- |
| 设备 / 架构 | U30 Air / aarch64 |
| 已验证基础系统 | mu300-linux v2026.10.30.1 / OpenWrt 25.12.6 |
| 内核 | `5.4.254-gb50db5b6224c` |
| 内核配置 SHA-256 | `1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3` |
| 软件包 | OpenWrt APK v3，非 Android APK |

**F50 / 7.2.8、其他内核与配置未适配。** 上游系统更新的源码比较不构成 ABI 或实机验收。安装器继续检查匹配条件，不强装音频驱动。

## 本版变化

- **Tailscale / 私网 IP 配对**：LuCI 新增可选隧道接口（通常 `tailscale0`），两端先建立 Tailscale 网络，再输入 U30 的隧道 IPv4、配对端口和六位码。使用 standalone 的 X25519/HKDF/AES-GCM 与签名端口证明；仅实际绑定的接口和端口可被认证。Bonjour 失败时手动窗口仍可用。
- 默认关闭额外隧道监听。仅支持 Linux 原生 TUN 的 `100.64.0.0/10` IPv4；不内嵌或登录 Tailscale，不自动修改防火墙、路由或 ACL，不支持 userspace 代理、MagicDNS 或 IPv6。本地配对仅保存网关密钥，保留原 Relay 配置。
- **云端缓冲**：支持 `audio_buffer_version=1` 协商。自动保持 U30 原来的 200–600 ms 目标及 1000 ms 队列上限；稳定优先为 240–720 ms 目标、760 ms 上限；手动可选 240/480/720/960 ms，上限为目标加 40 ms。
- 720 ms 是 36 个 20 ms 网络帧预缓冲，并非端到端延迟保证。只改变 iPhone → U30 的接收播放，不增加采集发送等待；接通前仍只有下行，不提前 active 或计时。
- 首个成功 answer 的策略绑定准确通话身份，重复控制不重置；重连保留策略但清空音频，新通话重新协商。未知或非法附加字段回退原自动策略。挂断清空队列，迟到连接不能恢复已关闭的音频桥。

详见 [Tailscale 步骤](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r23/docs/TAILSCALE_PAIRING.md) 和 [缓冲协议与测试](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r23/docs/CLOUD_AUDIO_BUFFER.md)。本仓库不修改或发布 AirSIM 客户端、Worker。

## 客户端与分支

云端缓冲需支持该协议的新版 iPhone AirSIM（174 所对应协议）；旧客户端保留 U30 原自动策略。云端运营商公告继续要求 R22 引入的 `early_media_version:1`，VoWLAN 认证与租约不变。

PR33 的未接/SIM 通知字段仅完成核对，未纳入本版；不根据 Relay 没收到 active 推断 missed，也没有增加全局麦克风静音或零音量策略。

F50 部署独立位于 [F50 分支](https://github.com/kai-wu-cortex/airsim-mu300/tree/F50)，本次不更新该分支。本 U30 一体包不构成 F50/其他内核的适配发布。

## 验证与限制

插件与全新隔离 Agent 的 Go race/vet、30 项 Node 测试及 40 项一体安装/音频保护测试通过；Linux ARM64 构建、APK 签名与空目录解包、对应源码及许可证核验纳入发布检查。

新增回归覆盖完整加密手动配对、签名端口证明、地址边界、一次性重放、端口冲突回滚、本地配对不改 Relay、720 ms 门槛和短暂空档、缓冲硬上限、稳定回落、错误身份、重复控制、重连策略、挂断清空及早期媒体转换。

**真实 Tailscale 两端配对、重连/网络切换、三档缓冲的双向听感、完整 APK 升级仍未验收。** 既有短时 U30 通话记录不能代替这些验收；长通话、多路、真实短信、其他硬件仍未完整验证。发布前执行非变更设备预检；不安装、重启、自动拨号、发短信或录音。

## 安装与回退

LuCI 自动更新需要开启预览频道，或使用下方固定版本脚本。结束通话和短信任务，保存配置与可信旧包。在符合上表的设备 SSH 终端执行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r23/install.sh
cat airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

安装器校验固定公钥、哈希、APK 签名及空闲状态，保留配对、配置与原短信。首次需要音频内存准备时提示安排一次进入已有 OpenWrt 的重启，不改变默认系统。详见 [部署手册](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r23/docs/DEPLOYMENT.md)。

附件：`luci-app-airsim-0.1.0-r23.apk`、`airsim-dev.pem`、`install.sh`、`airsim-mu300-0.1.0-r23-sources.tar.gz`、`SHA256SUMS`、`SHA256SUMS.sig`。

发布公钥 SHA-256：`0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de`。音频包与 DSP 固件哈希不变；缺失固件先从固定上游下载，失败时使用固定副本，两者均校验哈希，已有固件保留。备用固件来自公开上游，不来自用户设备；其独立许可未明确，项目许可证不覆盖它，见 [来源说明](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r23/docs/DSP_FIRMWARE_PROVENANCE.md)。

回退到 R22 或更早版本前，先关闭插件、清空 Tailscale 接口并保存，移除旧版严格配置解析不认识的字段。结束通话和短信任务，安装适配同一设备的可信旧包；不跨内核强装模块。本项目为独立集成，保留上游署名，**不是 MU300 Linux/Audio 官方发布**。发布物不含用户设备状态、真实号码、短信、通话录音、配对凭据或签名私钥。
