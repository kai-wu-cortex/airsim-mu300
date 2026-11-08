# Tailscale 手动 IP 配对（R23 开发预览，尚未实机验收）

与 AirSIM standalone 的「私网 IP」配对使用相同线协议。Tailscale 提供网络，AirSIM 的控制、短信与 PCM 复用现有 VoWLAN 认证网关；无需为此开启云端模式。此功能不内嵌 Tailscale，也不修改其登录、ACL、路由或防火墙。

## 使用

1. 在 U30 Air 和 iPhone 上配置并连接可互访的 Tailscale 网络。本实现要求 Linux 原生 TUN 接口（通常 `tailscale0`）已启动并具有 `100.64.0.0/10` IPv4；暂不支持 userspace 代理、MagicDNS、IPv6 或公网域名配对。
2. 结束通话后在 LuCI → AirSIM 关闭服务，在「管理与维护」填写「Tailscale 接口（可选）」为 `tailscale0`，保存后重新启动。留空关闭隧道监听；原 LAN 监听仍保留。缺少接口、地址或端口冲突会明确失败，不回退到公网通配监听。
3. Tailscale 访问规则和设备防火墙需允许 iPhone 访问这台设备的 TCP 配对、控制与 PCM 端口，默认分别为 **8580 / 8581 / 8582**，以 LuCI 设置为准。不要开放内部 7580、7581、8575、8576，也不要把 LuCI、SSH 暴露到公网。插件不自动创建放行规则。
4. 在概览页点击「获取配对码」，查看设备 Tailscale IP、配对端口和两分钟有效的六位码。
5. iPhone AirSIM 配对页选择「私网 IP」，输入 **U30 Air 的 Tailscale IPv4** 和配对端口，选择「本地模式配对」，输入六位码。不要填写 iPhone 自己的 IP，也不要填写控制或音频端口。
6. 客户端验证端口签名后保存直连地址，再通过签名健康检查确认设备在线。配对成功不等同于通话音频可用，双向通话须由用户单独手动验收。

Tailscale 地址或接口变化后，空闲时重启插件以重新绑定；地址变更需重新手动配对。服务启动前须先建立隧道。此版本不自动监控接口变化、不扫描网段、不进行自动网络修复。Tailscale 本身可能使用中继，不能据此保证点对点直连或通话延迟。

回退到不支持隧道设置的旧插件前，先关闭服务、清空 Tailscale 接口并保存，再安装旧包。新设置只在启用时写入 `tunnel_interface`，旧版严格配置解析不认识该字段。

## 协议与隔离

- 复用一次性 X25519 / HKDF-SHA256 / AES-GCM 六位码配对、两分钟期限和最多五次解密尝试。`GET /pair/v1/info` 不提供验证码、私钥或云端凭据。Bonjour 失败时仍保留手动窗口。
- `want_direct_route=true` 返回实际成功绑定的 `control_port`、`pcm_port` 及 `route_proof`。证明沿用 standalone 的 HMAC-SHA256 / base64url 无填充：`AIRSIM-DIRECT1\nsession_id\nclient_public_key\nagent_id\ncontrol_port\npcm_port`，密钥为本次加密注册里的 VoWLAN secret。
- 按 TCP 本地地址选择当前监听，不信任 Host / X-Forwarded-For。配对来源仅允许 RFC1918、IPv4 链路本地及 100.64/10；不接受任意 100.x、公网或回环来源。
- 仅显式 LAN 和 Tailscale IPv4 上监听认证配对／控制／PCM；Agent、管理和裸 PCM 仍限 loopback。所有入口共享原网关密钥、nonce 重放缓存和单音频会话互斥。
- 本地配对只保存新的网关密钥，不读取或覆盖原有 Agent / Relay 注册参数；非本地配对继续执行原有云端注册验证。不能把 local_mode 与 cloud_enabled=true 同时提交。
- 未宣告 standalone 的端到端短信能力，没有改变 AirSIM iOS/Android、上游 Agent、音频驱动、内核或 F50 分支。

## 验证边界

自动化覆盖完整加密手动 claim、与客户端同格式的端口证明、一次性重放拒绝、本地配对不调用 Agent、CGNAT 边界与伪造代理头拒绝、端口冲突回滚及 LuCI 配置校验。保留旧云模式重复配对、注册失败、错误验证码和密钥容量检查。

真实 Tailscale 两端配对、重启重连、VPN 断开、LAN 切换、双向通话和短信尚未验收。代码接入不代表 F50 或其他硬件获得通话兼容性；R22 及更早 Release 不包含本功能，R23 首次纳入。没有安装、重启、拨号、发短信或录音。

参考：AirSIM standalone 的 `docs/manual-ip-pairing.md`、`PairingServer.java`、`DirectGatewayRoutes.java` 和 iOS `SamsungPairingClient.swift`，均只读核对。
