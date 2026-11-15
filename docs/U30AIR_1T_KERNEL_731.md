# U30 Air 1T 版 + 内核 7.3.1：实机适配记录

本文记录一台 U30 Air **1TB 存储版**在 OpenWrt 25.12.5 用户态 + 自编 Linux 7.3.1 内核上的
实际改动与现象，供后续适配参考。文中不含设备序列号、SIM 标识、电话号码、蓝牙地址等
可识别信息。

与 `HARDWARE_ADAPTATION.md` 的关系：那边讲"怎么开始"，这里只写"这一台机器上到底
发生了什么、哪些是必须的、哪些还没验证"。

## 1. 这台机器的基本情况

| 项 | 实测值 |
|---|---|
| SoC | Spreadtrum UMS9620（`/proc/device-tree/model`） |
| 平台标识 | `/run/mu300/device` = `u30air` |
| 存储 | UFS，`/proc/partitions` 中 76 个 `sda*` 分区；rootfs 在 `/dev/sda76`（ext4，885.8 GiB），同一分区另挂在 `/mnt/mu300-disk` |
| 用户态 | OpenWrt 25.12.5（`r20261109.165714-g115621f5-s031415b8`） |
| 内核 | 自编 7.3.1（`vermagic 7.3.1 SMP preempt mod_unload aarch64`，`CONFIG_MODVERSIONS` 关闭） |
| 调制解调器 | `mu300-atd` 独占 AT 通道，客户端经 FIFO 串行化访问 |

1T 版的分区布局与常见 128G/256G 版不同（同一条 UFS 上分区更多、rootfs 更大），
刷机脚本按小容量版写死分区号时会失败；本次没有改分区，只换了内核镜像 + 用户态。

## 2. 官方 25.12.5 包为什么不能直接用

官方 `k6.18.54` 包在这台机器上缺三样东西：

1. **ALSA 整体缺失**：内核配置里 `CONFIG_SND` 未开，`/proc/asound` 不存在，AirSIM 的
   PCM 桥没有设备可开；
2. **WCN 侧的 WiFi/BT 模块不全**：`wcn_bsp.ko` / `sprd_wlan_combo.ko` 不在包内，
   `wlan0` 起不来；
3. **initramfs 里的模块是 6.18.54 的**：只替换 boot image 的 kernel 段会让 initramfs
   带着 111 个 vermagic 不匹配的模块启动，直接卡在挂载 rootfs 之前。

因此适配走的是"整镜像替换"：用旧固件里已经能跑音频/WiFi 的 7.3.1 启动镜像，配上
25.12.5 的 rootfs，再把缺的模块补进去。**不要**只换 kernel 段。

## 3. 必须补的内核模块

7.3.1 的启动镜像里没有这些（或没随包带），需要按同一内核重编：

| 模块 | 作用 | 缺了会怎样 |
|---|---|---|
| `snd-timer.ko`、`snd-pcm.ko` | ALSA 定时器与 PCM 核心 | `snd_soc_core` 加载失败，声卡 0 不注册 |
| `snd-pcm-dmaengine.ko`、`snd-compress.ko`、`snd-soc-core.ko` | SoC 音频框架 | 同上 |
| `wcn_bsp.ko` | WCN 总线/电源 | WiFi、BT 都没有传输层 |
| `sprd_wlan_combo.ko` | WiFi 驱动 | `wlan0` 不出现 |
| `sipa-dele.ko` | USB 网络卸载/数据通路 | USB 共享网络"连着但没网" |

**加载顺序有硬约束**：`snd_soc_core` 依赖 `snd_pcm`，`snd_pcm` 依赖 `snd_timer`。顺序错了
只会看到 `Unknown symbol`，不会告诉你缺的是哪一个：

```
snd-timer → snd-pcm → snd-pcm-dmaengine → snd-compress → snd-soc-core
```

加载方式也有坑：OpenWrt 的 `/sbin/insmod` 是 kmodloader 的软链，**拒绝绝对路径**，且要求
`/lib/modules/$(uname -r)` 下有模块索引。自编模块没进索引，用
`/opt/mu300/bin/busybox insmod /lib/modules/7.3.1/xxx.ko` 最稳。

## 4. 音频

补完模块后：

- 声卡 0 = `sprdphonesc2730`，19 个 PCM 设备；
- 通话前端 `FE_ST_VOICE` = `hw:0,5`；上行 `hw:0,53` 是 dump 通道；`hw:0,16` 用于抓包；
- DSP 起来后有 4 条路由，`VBC_VOLUME` 控件对应 `dsp_vbc_set_volume()`。

一个**已知不足**：驱动层没有独立的"上行增益"控件。`VBC_VOLUME` 影响的是整体 VBC 音量，
对端仍反馈"声音偏小"。推测与缺失的 DSP profile（`audio_structure` / `dsp_vbc` / `cvs`）
有关，本次没有继续挖；如果要做，方向是补齐 DSP profile 而不是继续调 mixer。

## 5. 信号读数：`AT+CSQ` 在 5G SA 下恒为 99

现象：信号明明是满的，面板上信号一直空。

原因：`AT+CSQ` 返回 `+CSQ: 99,99`（unknown）。真值只出现在 `AT+CESQ` 的尾部字段。

`AT+CESQ` 在 5G SA 下返回 9 段：

```
+CESQ: rxlev,ber,rscq,ecn0,rsrq,rsrp,ss_rsrq,ss_rsrp,ss_sinr
```

解析时要注意两点：

1. LTE 场景用第 6 段 `rsrp`（dBm = 值 − 140），NR/SA 场景用第 8 段 `ss_rsrp`
   （dBm = 值 − 156）；
2. `rxlev` 不能直接当信号值用 —— 它在 SA 下仍是 99。

设备侧的 `cellparse.uc` 原来只读前 6 段，SA 下永远是空。补上 `f[6..8]` 后：
`ss_rsrq = −43 + 0.5×值`、`ss_rsrp = −156 + 值`、`ss_sinr = −23 + 0.5×值`。

`packaging/openwrt/at-compat/mu300-at` 里做了兼容：`AT+CSQ` 内部改用 `AT+CESQ`，把
`ss_rsrp`/`rsrp` 折回 CSQ 的 0..31 刻度（0 = −113 dBm，步进 2 dB），所以旧的解析代码
不用改就能继续工作。

## 6. 双卡与语音

这台机器装了两张卡，实测：

| 卡槽 | 用途 | `+CIREP` |
|---|---|---|
| 卡 1（`/run/mu300-at` → `/dev/stty_nr1`） | 语音（VoLTE） | 已注册 |
| 卡 2（`/run/mu300-at4` → `/dev/stty_nr4`） | 仅数据 | 未注册 |

两个容易踩的点：

1. **AT 通道决定指令发给哪张卡**。`ATD` 发给没有语音能力的卡，表现是"拨号没反应"，
   而不是报错；
2. 自带 `mu300-at` 在 `/run/mu300-radio-freeze` 存在且 `/run/mu300/sim-slot=1` 时，
   会把**没有显式指定 `MU300_AT_DIR`** 的调用改路由到卡 2。切卡上网之后，agent 的
   `ATD` 正好落在这一支上。

如果你的设备只有一张卡有语音，用 `at-compat` 的 `voice-card` 把语音命令钉在卡 1：
`echo 0 > /etc/airsim/voice-card`（详见该目录 README）。默认不启用，行为与自带脚本一致。

另外，**通话建立瞬间数据会短暂让路**（单射频分时）。如果数据走的是卡 2、语音走卡 1，
通话那几秒隧道会抖一下，客户端可能把"事件流断了"误判成"拨号失败"，而基带其实已经接通。
把数据也留在卡 1（VoLTE 可并发）能避免这个误判。

## 7. IPv6

原配置 LAN 是 `ra=relay`，但这台机器没有可中继的上游 RA，于是客户端拿不到 IPv6 地址。
改成 server 模式即可：

```
lan: ra=server  dhcpv6=server  ndp=disabled  ra_default=1  ra_flags=other-config
wan: masq6=1
```

`ndp=disabled` 是因为前缀已经通过 RA 下发，不需要再代理邻居发现。

## 8. USB 共享网络

只补 `sipa-dele.ko` 不够：`sipa_eth0` / `sipa_eth8` 起来之后，主机侧还要有默认路由与
NAT，否则表现为"网卡连着、没有网"。Windows 主机侧正常 DHCP 即可；设备侧确认
`ip route` 有默认路由、`masq` 在 WAN 侧生效。

## 9. 蓝牙

内核配置齐全（`CONFIG_BT=y`、`BREDR/LE/RFCOMM/BNEP/HCIUART_H4`），但镜像里没有传输层驱动
`sprdbt_tty`：`/dev/ttyBT0` 不存在、`/sys/class/bluetooth` 为空、设备树的
`sprd-marlin3/sprd-mtty` 节点没有驱动绑定。

给 7.3.1 重编（修掉 6 个驱动缺陷）后已实机验证：控制器上电、`btattach` 走 H4、8 秒扫描
发现 11 个周边设备，WiFi 与 AirSIM 音频无回归。装载必须按 `insmod → mu300-bt-init →
btattach → bluetoothd` 的顺序，跳过 `mu300-bt-init` 会只注册半个 `hci0`。

详见 [U30AIR_1T_BLUETOOTH.md](U30AIR_1T_BLUETOOTH.md)。

## 10. 未验证 / 不在本记录范围内

按 `AGENTS.md` 的要求如实列出：

- **只有 7.3.1 一台**：以上结论都来自这一台 1T 版 + 7.3.1，换内核版本要重新核对
  `vermagic` 与模块；
- **短信**：没有系统验证，双卡下的 `AT+CMGS`（走 `mu300-atd` 的 CR/Ctrl-Z 协议）未测；
- **2G/3G 回落**：没有测试环境，`AT+CESQ` 在非 SA 场景的回退逻辑只用桩验证过；
- **蓝牙音频**：驱动与 BlueZ 已跑通（可扫描、可配对），但**蓝牙通话（HFP）与 A2DP 未适配**，
  通话仍走 VoLTE；
- **`/lib/modules/6.18.54` 下的旧模块**在 7.3.1 下不可用（`f2fs`、`xfs` 等需要时得重编）；
- **通话质量**：对端反馈上行偏小，未解决，见第 4 节。
