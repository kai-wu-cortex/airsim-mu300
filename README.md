# AirSIM for MU300 — OpenWrt LuCI 通话网关插件

> **R23 · Tailscale 配对与云端缓冲（开发预览）**。新增与 standalone 一致的手动 IP 加密配对，以及云端稳定优先／手动接收缓冲；已通过自动化测试，尚未完成新功能实机验收。见 [R23 发布说明](docs/RELEASE_v0.1.0-r23.md)。

- **Tailscale 配对：** 在 LuCI 管理页填写可选 `tailscale0` 接口，两端先连接 Tailscale，再用设备 IP、配对端口和六位码配对。本地配对只保存网关密钥，保留原 Relay 配置；不内嵌 Tailscale、不自动修改防火墙。见 [使用说明](docs/TAILSCALE_PAIRING.md)。
- **云端缓冲：** 支持 iPhone 新版的自动、稳定优先及手动 240/480/720/960 ms。U30 自动策略保持原来的 200–600 ms；稳定优先为 240–720 ms，手动 720 ms 为 36 帧预缓冲。只影响接收播放，重连保留选择并清空旧音频。见 [协议与测试](docs/CLOUD_AUDIO_BUFFER.md)。
- **保留既有能力：** R22 早期媒体、严格通话身份、R20 配对修复，以及短信、电量和 DSP 离线回退。8 kHz 音频、系统、内核和 AirSIM 客户端保持原样。PR33 通知字段仅完成核对，尚未接入。

- 电量：通过现有认证健康接口和云端心跳提供百分比、充电状态及采样时间。未知或过期样本不显示为有效电量；此前 R21 修复版已核对设备与云端读数一致，用户确认云端手机显示正常；充放电变化待验收，见 [电量说明](docs/AIRSIM_BATTERY.md)。
- 通话：取消同号两分钟等待，按已认证身份与操作 ID 持久去重；保留原通话结束事件并通知客户端。立即重拨、接通后对端挂断仍待完整实机回归，见 [通话修复记录](docs/R18_REDIAL.md)。
- 短信：接入客户端现有收件箱、确认与发送接口；真实收发和后台推送尚未验收，见 [短信说明](docs/AIRSIM_SMS.md)。
- 云端号码：此前 R21 修复版发送当前 SIM 号码，号码不可用时清除旧值；已核对 Relay 号码与设备一致，用户确认手机显示正常，未改客户端，详见 [接入记录](docs/AIRSIM_BATTERY.md)。
- 固件：缺失 DSP 固件时尝试固定上游，失败或哈希不符则用保存的固定副本回退，两条路径核对同一 SHA-256。已有固件保留，来源与许可边界见 [记录](docs/DSP_FIRMWARE_PROVENANCE.md)。

R22 云端接通前运营商语音要求客户端发送 `early_media_version: 1` 和完整通话身份；首帧前返回 `early_media_ready_v1`，接通前仅下行、不提前计时。未协商的旧客户端仍可正常接通后通话，但不会收到云端早期语音。请使用支持此协议的新版 AirSIM（171 已补充 R21 旧状态名兼容）。VoWLAN 协议不变。此前 R21 两种模式的提示音、计时与双向通话已通过用户验收，见 [记录](docs/EARLY_MEDIA.md)。

此前 R20 的配对修复候选已安装 U30 Air，用户确认 iPhone 保持云模式、连续两次使用新验证码配对成功；设备身份和原配对保留，云连接正常。R23 完整 APK 未单独完成实机升级回归，其他功能不因配对通过而视为已验收，见 [R23 发布说明](docs/RELEASE_v0.1.0-r23.md)。

> [!WARNING]
> **R23 沿用 R17 的 U30 Air 一体安装方案，当前仅提供匹配 `5.4.254-gb50db5b6224c` 及指定内核配置的音频扩展。F50 / 7.2.8 尚未适配，不能仅凭“1031 之后”判断兼容。**
> 安装器会检查音频扩展匹配条件。R17 实机升级、重启自动续接及重启后两通双向通话已通过验收；R19 新增功能尚未完成整体验收；本次早期媒体已有 VoWLAN 与云端提示音、计时和接通后双向声音的用户确认。

F50 部署独立维护在 [F50 分支](https://github.com/kai-wu-cortex/airsim-mu300/tree/F50)，不包含在本 U30 Air Release 中，不能用此安装包替代 F50 部署步骤。

将运行 mu300-linux OpenWrt 的 U30 Air 接入 AirSIM。它是 LuCI 插件、Linux Agent 和音频桥，不是 Android APK 或完整系统镜像。

[下载 Release](https://github.com/kai-wu-cortex/airsim-mu300/releases) · [一体部署手册](docs/DEPLOYMENT.md) · [R23 说明](docs/RELEASE_v0.1.0-r23.md) · [硬件适配指南](HARDWARE_ADAPTATION.md)

**R23 把插件、已重建音频模块、工具和准备流程合并到一个签名 APK。** 首次缺少 DSP 固件时，从上游固定地址下载并校验，失败时使用包内固定备用副本；已有文件保留。首次需要重启时，明确提示重启进入 OpenWrt 一次，随后自动继续准备。无需用户单独下载音频扩展或运行恢复命令。系统镜像、内核、客户端与配对配置保持原样。

已有 R16 恢复扩展会在内容匹配后复用；不覆盖未知扩展或自定义准备脚本。R15 已撤回。**历史版本线：R16 面向 1031 之后的系统，R12 面向 1031 之前的系统。** 日期标签不能替代硬件、内核配置与音频 ABI 检查，也不代表所有新版系统均可通话。

## 功能

- 对齐 AirSIM PR #29：严格校验推送 call_id / call_uuid / generation，开放认证历史确认，传递短信槽标签并验证显式选卡。新增能力尚未实机验收，详见 [接口兼容记录](docs/PR29_AGENT_COMPATIBILITY.md)。
- 联合升级顺序：兼容设备端 Agent → 支持短信字段的 Worker → 新版 iOS。旧 Worker 可能忽略选卡，不能仅凭本插件升级宣称云端选卡可用。

- 修复新版 AirSIM 云模式配对兼容性；配对成功与云推送就绪分开判断，失败注册不新增局域网密钥，详见 [修复记录](docs/PAIRING_COMPATIBILITY_FIX.md)。

- LuCI 展示服务、SIM 本机号码、网络、通话和音频状态。
- 启动/关闭服务并显示确认结果；获取六位配对码时开启 120 秒 Bonjour 发现。
- 复用 AirSIM 配对与 VoWLAN 协议，提供经过身份验证的局域网通话网关。
- 通过 MU300 SIM 拨号、选择具体来电接听、选择具体通话挂断。
- 将硬件通话音频转换为 AirSIM PCM，兼容现有云端 Relay 媒体链路。
- 在状态卡下方查看短信、未读数量、全文及发送短信，发送前二次确认。
- 一体安装先校验随包音频的内核与配置，保留架构、签名及通话/媒体/短信空闲检查。
- 固定上游 Agent 提交，在隔离副本应用 MU300 补丁，**不修改 AirSIM iOS / Android 项目**。
- 局域网／云端策略、Relay 地址限制、LAN 接口与配对/控制/PCM 端口设置。
- 脱敏状态报告下载、离线版本说明、手动检查项目 Release（不自动安装）。
- 后台自动检查、检查开关与预览频道、用户确认后升级、下载/签名校验/安装进度及失败提示；不自动安装或降级。

### 当前边界

| 功能 | 状态与限制 |
| --- | --- |
| LuCI 通话控制 | 网页不提供麦克风/扬声器，不自动建立客户端媒体会话 |
| SIM 本机号码 | 从 SIM / 基带读取；未存储时显示未提供，不推测号码 |
| 通话身份 | UUID 与操作回执；基带快照短暂缺失下的 ID 连续性仍需实机回归，多路验收未完成 |
| 呼叫等待 | 定向接听/挂断与保持策略；不是会议通话或完整多路切换 UI |
| PCM | 单媒体会话；当前网络音频为 8 kHz、单声道、S16LE |
| 短信 | R17 仅 LuCI；R19 包含 AirSIM 同步与发送，待实机验收。每条最多 70 字，不支持 emoji 或长短信拆分 |
| 远程访问 | 需要兼容 AirSIM 客户端/Relay；不应把 LuCI、SSH 或裸 PCM 暴露到公网 |

## 支持环境

| 项目 | 当前已验证基础环境 |
| --- | --- |
| 设备 | U30 Air，F50 尚未验收 |
| 系统 | mu300-linux v2026.10.30.1 / OpenWrt 25.12.6 |
| 内核 | `5.4.254-gb50db5b6224c` |
| 内核配置 SHA-256 | `1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3` |
| 软件包 | aarch64_generic，APK v3，非 Android APK |
| 音频 | 随包附带 24 个重建模块；设备已有固件、固定上游下载或包内固定备用副本 |

R16 的音频实现已由用户确认短时局域网双向声音清晰、卡顿消失或明显改善。R17 复用该实现，增加一体安装流程；重启后两通测试均由用户确认双向清晰、无卡顿，挂断后资源正常释放。实际驱动兼容性不能只看系统日期或内核大版本；7.2.8、其他内核配置与其他硬件不在当前一体包验收范围。

## 快速安装

以下命令安装 R23 开发预览版；LuCI 更新器需开启预览频道才能发现该版本。安装前阅读适用范围和未验收项，并保存原版包以便回退。

先结束全部电话与短信任务，并确认设备符合上表。在设备 SSH 终端执行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r23/install.sh
cat airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

也可按 [部署手册](docs/DEPLOYMENT.md) 在 LuCI 上传同版签名 APK。不使用忽略签名或强制依赖选项。

- 显示“准备完成”：刷新 LuCI → 服务 → AirSIM，启动服务、获取配对码并连接客户端。
- 显示“需重启完成安装”：安排一次 **进入 OpenWrt 的重启**，安装会自动继续，无需补装音频包。部分设备默认进入 Android，须使用设备已有的 Linux 启动入口。
- 显示“准备未完成”：不能将软件包存在视为可通话；查看安装错误，不能绕过内核/配置检查。

开发签名公钥 SHA-256：`0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de`。

没有自动拨号、接听、短信发送或录音。配对后由用户手动确认双向声音。长通话、多路与公网 Relay 异常恢复尚未完整验收。

### 回退与卸载

回退到 R22 或更早版本前，先关闭服务、清空 Tailscale 接口并保存，移除旧版不认识的配置字段。

先结束通话并保存可信旧包与配置。插件卸载停止服务，保留配对数据和音频恢复备份；已驻留音频模块需重启才会退出。音频扩展回退另按 [音频指南](packaging/openwrt-audio/README.md)，不以跨版本强装方式回退。

## 工作原理

```text
AirSIM 客户端 ── 加密配对 / 签名 VoWLAN ── MU300 网关
                                               ├─ Agent → mu300-atd → 基带 / SIM
                                               └─ PCM 桥 → mu300-audio → ALSA / DSP
LuCI ── 登录会话 + rpcd ACL ── 服务控制 / 配对 / 通话 / 原生短信服务
```

`mu300-atd` 独占 AT 串口；插件不抢占串口、不套用其他设备的 USB 恢复/刷写逻辑。信令、媒体和发现服务分层，“AT 能拨号”不代表“有双向声音”。

| 接口 | 默认地址 | 用途 |
| --- | --- | --- |
| Agent | `127.0.0.1:8575` | 内部状态和控制 |
| PCM 桥 | `127.0.0.1:7580` | 内部音频，不直接向 LAN 开放 |
| 管理后端 | `127.0.0.1:7581` | LuCI / rpcd 操作 |
| 配对 | LAN / 可选 Tailscale `8580` | 两分钟加密配对窗口，支持手动 IP |
| VoWLAN | LAN / 可选 Tailscale `8581` / `8582` | 签名控制 / 认证 PCM |

配对加密与请求认证不意味着全部媒体使用 TLS 加密；当前局域网 PCM 会话认证后传输原始音频。仅在可信 LAN 使用，跨不可信网络需安全隧道或经审查的传输设计。

## 源码与构建

需要 Git、Go 1.24+、Node.js（测试）、Docker、curl、OpenSSL。

```sh
git clone https://github.com/kai-wu-cortex/airsim-mu300.git
git clone https://github.com/kai-wu-cortex/AirSIM.git
cd airsim-mu300
go test -race ./...
go vet ./...
node --test scripts/overview.test.mjs scripts/sms.test.mjs scripts/install.test.mjs scripts/manage.test.mjs
AGENT_TREE=$(sh scripts/prepare-agent.sh)
(cd "$AGENT_TREE/module/module-agent" && go test -race ./... && go vet ./...)
docker pull node:22-alpine
sh scripts/package-apk.sh
```

Agent 固定到 [`cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`](https://github.com/kai-wu-cortex/AirSIM/commit/cc2ade3124ac34bec7d8c441f4b66a57a6dfec62)，见 `patches/AIRSIM_BASE`。构建器只读提交，将补丁应用到 `out/` 隔离副本，不修改相邻主项目。

`AIRSIM_UPSTREAM_REPO` 指定只读上游 Git 仓库；`AIRSIM_PACKAGE_KEY` 指定签名私钥。默认密钥仅存在本机 `out/signing/`。**自行构建的公钥与本 Release 不同，不要沿用发布安装脚本的固定公钥指纹。**

```text
HARDWARE_ADAPTATION.md   给 AI Agent / 开发者的跨硬件通话适配指南
cmd/airsim-mu300d/       管理服务、配对入口、设备集成
internal/audio/         PCM 协议、重采样、ALSA 会话生命周期
internal/gateway/       VoWLAN 签名、路由和媒体网关
internal/pairing/       加密配对会话
patches/                固定上游版本及 MU300 专用 Agent 补丁
packaging/openwrt/      LuCI、rpcd ACL、procd、APK 钩子
scripts/                构建、安装、测试与发布辅助
docs/                   实施证据、验收边界与历史设计
```

## 排障与安全

- 启动失败：核对 LuCI 音频准备状态、必需工具、DSP 固件，以及是否有其他进程占用语音场景。R23 随包音频模块要求精确匹配内核与配置；不绕过驱动 ABI 检查。
- 能拨号但无声音：分别检查基带状态、客户端路由、PCM 握手、采集/播放帧数，不能只看注册状态或声卡。
- 号码未提供：SIM 可能未存储本机号码，不能用来电号码、IMSI 或测试号码替代。
- 短信接口缺失：需要固件 `mu300dash` 短信服务，不回退到冲突的 AT 编码流程。
- 请自行设置 LuCI 管理密码；脚本不修改凭据。勿开放 WAN 管理。
- 提交日志时去除号码、IMSI/IMEI、短信正文、配对码、密钥和音频。

详见 [实施记录](docs/MU300_PLUGIN_IMPLEMENTATION.md) 和 [呼叫等待说明](docs/MU300_R9_CALL_WAITING.md)。U30 Air 整体设计与 UFI Tools 文档属于历史探索，不代表当前已发布功能。

## 上游、Fork / 镜像关系与致谢

本仓库是**独立集成与适配工程**，不是 mu300-linux 或 mu300-audio 的官方发布，也不是它们的 GitHub Fork。

| 项目 | 如何使用 | 作者与来源 |
| --- | --- | --- |
| [AirSIM](https://github.com/kai-wu-cortex/AirSIM) | 固定 Agent 基线、配对/VoWLAN/Relay 协议；设备专用补丁 | AirSIM contributors |
| [mu300-linux](https://github.com/dikeckaan/mu300-linux) | Linux/OpenWrt、AT 服务、原生短信与设备基础设施 | **dikeckaan** 及贡献者 |
| [mu300-audio 原项目](https://github.com/dikeckaan/mu300-audio) | 音频驱动、DSP 场景与工具，固定 `v1.0.5` | **dikeckaan** 及贡献者 |
| [mu300-audio 源码镜像](https://github.com/kai-wu-cortex/mu300-audio) | 原仓库不可访问时保存源码与标签 | 不改变原作者署名；当前 GitHub 元数据不是 Fork 关系 |
| [OpenWrt / LuCI](https://openwrt.org/) 与 [ALSA](https://www.alsa-project.org/) | 管理界面、包管理与音频工具 | 各上游贡献者 |

感谢 **dikeckaan** 在 MU300 Linux 与通话音频栈上的工作，也感谢音频上游注明的 **kano / kanoqwq、Enceka、e5-linux** 及 Unisoc 驱动贡献者。上游引用不代表作者对本插件的背书或支持承诺。

2026-10-31 发布准备时，原 mu300-audio URL 返回 404；镜像仍可访问。Release 保留固定版本音频依赖、源码与校验信息，不把镜像描述为官方新版本。

## 许可证

本项目自身代码沿用非商业定位，见 [LICENSE](LICENSE)、[NOTICE](NOTICE)。AirSIM Agent 使用 **PolyForm Noncommercial 1.0.0**；mu300-audio 按文件采用 **GPL-2.0 / MIT**；ALSA 按组件采用 **LGPL-2.1-or-later / GPL-2.0-or-later**。各组件保留各自权利，不以单一许可证覆盖整包。

这是 **source-available（源码可用）非商业项目**，不能把整包称为无商业限制的开源软件。商业使用需分别取得相关许可。独立 DSP 固件的许可未明确，项目许可证不覆盖它，见 [固件来源记录](docs/DSP_FIRMWARE_PROVENANCE.md)。依赖与对应源码说明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) 和 [SOURCES.md](SOURCES.md)。
