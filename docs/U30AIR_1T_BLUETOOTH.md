# 7.3.1 内核下的蓝牙：补齐 `sprdbt_tty` 并在实机验证

结论先说：**内核支持 BlueZ，缺的只是 Unisoc 的传输层驱动 `sprdbt_tty`**。7.3.1 镜像里没有
这个模块，也没有可替代的 H4 传输，所以 `btattach`、`bluetoothd` 装了也用不起来。

给 7.3.1 重编并修掉 6 个驱动缺陷之后，已经在一台 U30 Air 1T 上装载验证通过：控制器正常
上电，`bluetoothctl` 8 秒扫描发现 11 个周边设备（其中 3 个带名称），WiFi AP 与 AirSIM
音频无回归。

本文记录判断依据、重编方法、审读源码发现的缺陷，以及实机验证结果。文中不含任何设备
标识（BD_ADDR 等）。

## 1. 判断依据（装载前的只读检查）

内核配置（`/proc/config.gz`，7.3.1）：

```
CONFIG_BT=y              CONFIG_BT_BREDR=y         CONFIG_BT_LE=y
CONFIG_BT_RFCOMM=y       CONFIG_BT_RFCOMM_TTY=y    CONFIG_BT_BNEP=y
CONFIG_BT_HIDP=m         CONFIG_BT_HCIUART=y       CONFIG_BT_HCIUART_H4=y
CONFIG_RFKILL=y
```

`HCIUART_H4` 正好对上厂商脚本 `bluetooth-start` 里的
`btattach -B /dev/ttyBT0 -P h4`。

但传输层是空的：

| 检查项 | 结果 |
|---|---|
| `/dev/ttyBT0` | 不存在 |
| `/sys/class/bluetooth` | 空 |
| `lsmod` | 没有 `sprdbt_tty` |
| 设备树 | `sprd-marlin3/sprd-mtty`：`compatible="sprd,mtty"`、`sprd,name="ttyBT"` 存在 |
| 驱动绑定 | `/sys/bus/platform/devices/sprd-marlin3:sprd-mtty` 没有 driver 链接 |
| `rfkill` | 只有 `phy0`（wlan），没有 bluetooth 条目 |

也就是说：设备树节点在、内核框架在、**中间那层没加载**。

## 2. 为什么不能直接拿旧模块

6.18.54 与 5.4.254 的社区包都带了 `sprdbt_tty.ko`（`depends=wcn_bsp`），但那两个包能开
蓝牙的原因只是"随包带了与内核严格匹配的模块"，不是内核配置更强。`vermagic` 是
`6.18.54` / `5.4.254-…`，`insmod` 到 7.3.1 上会被直接拒绝 —— 和当初音频模块一样。

所以要么给 7.3.1 重编，要么退回 6.18.54（代价是丢掉 7.3.1 上的音频改造）。

## 3. 重编方法（不随本仓库分发源码）

驱动源码属于 Unisoc vendor 内核（`modules/sprdbt_tty`，`tty-pcie/` 下
`tty.c` / `rfkill.c` / `lpm.c` / `alignment/sitm.c`，Kbuild 里
`BSP_BOARD_UNISOC_WCN_SOCKET := pcie`）。**由于许可证原因，本仓库不包含这些文件**，
下面只记录步骤与结论，方便有 vendor 源码的人复现。

```sh
make -C <kernel-src> M=$PWD ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- \
     KBUILD_EXTRA_SYMBOLS=<wcn_bsp build dir>/Module.symvers modules
```

`KBUILD_EXTRA_SYMBOLS` 是必须的：不传时 modpost 报 6 个未定义符号 ——
`get_wcn_bus_ops` / `start_marlin` / `stop_marlin` / `marlin_set_wakeup` /
`marlin_set_sleep` / `wcn_reset_notifier_list`，它们都由 `wcn_bsp` 以
`EXPORT_SYMBOL_GPL` 导出。

值得注意的是：**总线操作不是通过导出符号拿的**，而是 `get_wcn_bus_ops()` 返回一张
操作表，所以 `kallsyms` 里看不到 `mtty_*` / `sprdwcn_bus_list_alloc` 并不代表
`wcn_bsp` 缺东西。只要这 6 个符号在，模块就能链接成。

产物 `sprdbt_tty.ko` 的 `vermagic` 必须与设备一致（`7.3.1 SMP preempt mod_unload
aarch64`），且 `depends=wcn_bsp`；装载顺序是 `wcn_bsp` → `sprdbt_tty`。

## 4. 审读源码发现的 6 个缺陷

这些是**驱动自身**的问题，与 7.3.1 无关，旧内核上同样存在。第 1、2 个会直接导致蓝牙
"起不来"或"用一会儿就卡死"，第 6 个在反复装卸时会污染内核通知链，其余是异常路径下的
泄漏/越界。

| # | 位置 | 问题 | 建议 |
|---|---|---|---|
| 1 | `rfkill.c` | `rfkill_init_sw_state(bt_rfk, true)` 让适配器**默认处于软阻塞**。OpenWrt 上 busybox 没有 `rfkill`，软件源里也没有单独的 `rfkill` 包，于是永远解不开，BlueZ 一直认为适配器被关 | 初始化为 `false`，或在首次 `btattach` 前显式 unblock |
| 2 | `tty.c` `mtty_write()` | `dma_alloc_coherent` 失败时直接 `return -ENOMEM`，**漏了 `up(&sem_id)`**，也没把 list 还回去。每失败一次少一个额度，累计 63 次后写路径永久阻塞 | 失败分支补 `sprdwcn_bus_list_free()` + `up()` |
| 3 | `tty.c` `mtty_close()` | 先 `mtty_dma_buf_free()` 再 `sprdwcn_bus_chn_deinit()`；中间若有 RX 回调进来，会往已释放的 DMA 缓冲写（use-after-free） | 改成 置状态 → 摘通道 → 再释放缓冲 |
| 4 | `tty.c` `mtty_tx_cb()` | 每个 mbuf 都用 `head->phy` 去 `dma_free_coherent`，`num > 1` 时释放的是错地址 | 改用 `pos->phy` |
| 5 | `alignment/sitm.c` `sitm_ini()` | `kmalloc` 后不判空就 `memset`；`kfifo_alloc` 失败只打印，留下半初始化的 `rd` | 补判空 + 失败回收并返回 `-ENOMEM` |
| 6 | `tty.c` `mtty_probe()` | 每次装载都往 `wcn_reset_notifier_list` 注册，卸载路径不注销。反复 `insmod`/`rmmod` 会累积重复项，内核报 `WARNING: notifier_chain_register`，且残留项指向已卸载模块 —— 之后任何一次 WCN 复位都可能跳到已释放内存 | 在 `mtty_remove()`/`mtty_exit()` 里 `unregister`；修好之前运维上不要反复 rmmod/insmod |

另外几处只在极端情况下触发，记录但不建议贸然改：`tty_register_driver()` 排在锁/链表
初始化之前；`mtty_open()` 不检查 `start_marlin()`/`chn_init()`/`dma_buf_alloc()` 的
返回值；`mtty_close()` 从不 `stop_marlin()`（厂商设计）；`bluesleep_init()` 的返回值
被忽略，而 `bt_wakeup_host()` 会无条件 `__pm_relax()`。

## 5. 与 WiFi 共存的冲突核对

蓝牙和 WiFi 共用一颗 Marlin3，走同一条 WCN 总线，所以这是最容易出事的地方。核对结果：

- **通道不撞**：WiFi 用 TX 4/5、RX 7/9/11；蓝牙用 TX0=1、RX=2。
  `BT_TX_CHANNEL1=4` 虽然和 WiFi 的 TX 命令通道同号，但代码里从未 `chn_init` 它；
- **GNSS 不抢**：`platform/gnss/*` 没有注册 mchn，且 gnss 模块未加载；
- **上电不复位**：`start_marlin(MARLIN_BLUETOOTH)` 在芯片已为 WiFi 上电时走
  "marlin have open" 分支，只置位并通知，不会复位芯片，不会打断 WiFi AP。真正上电
  发生在 `open /dev/ttyBT0` 时。

## 6. 装载顺序（这一条比驱动本身更容易踩）

驱动装好不等于蓝牙能用。实测必须按下面顺序，否则 `hci0` 只注册一半：

```sh
busybox insmod /lib/modules/$(uname -r)/sprdbt_tty.ko   # 1. 出 /dev/ttyBT0
mu300-bt-init                                            # 2. 写 PSKey / RF（176 B / 252 B）
btattach -B /dev/ttyBT0 -P h4 &                          # 3. 才 attach H4
bluetoothd -n &                                          # 4. BlueZ 起协议栈
```

- 跳过第 2 步直接 `btattach`：`/sys/class/bluetooth/hci0` 会出现，但 `address` / `type` /
  `bus` 三个属性全空，内核不打印任何 `Bluetooth:` 初始化日志，`bluetoothctl` 也拿不到
  控制器 —— 看起来"驱动在、就是没设备"；
- `/sbin/insmod` 在 OpenWrt 上是 kmodloader 的软链，**拒绝绝对路径**且没有
  `modules.dep` 索引，所以要显式用 `busybox insmod <绝对路径>`；
- `rfkill` 在 busybox 里没有对应 applet，源里也没有单独包。厂商 `bluetooth-start` 在
  `set -e` 下会因 `rfkill unblock` 返回 127 而中止，需要在 PATH 前置一个 no-op 桩。

## 7. 实机验证结果

在 U30 Air 1T（OpenWrt 25.12.5 用户态 + 自编 7.3.1）上装载后：

| 项 | 结果 |
|---|---|
| 模块 | `sprdbt_tty` 在载，`wcn_bsp` 引用计数 1 → 2 |
| 传输 | `/dev/ttyBT0` 出现，`btattach -B /dev/ttyBT0 -P h4` 常驻 |
| rfkill | 新增 `type=bluetooth` 条目，`soft=0`（第 1 个缺陷的补丁生效；否则 BlueZ 永远认为适配器被关） |
| 控制器 | `bluetoothctl show` → `Powered: yes`、`Manufacturer: 0x01ec`（Spreadtrum）、`Name: BlueZ 5.83` |
| 扫描 | `bluetoothctl --timeout 8 scan on` 发现 **11 个周边设备**，其中 3 个带名称；其余为 BLE 随机地址设备，广播里不带名字，BlueZ 只能用地址生成别名 |
| 回归 | WiFi AP（ch36 / 80 MHz，客户端在线）、AirSIM `audio_available: true`、蜂窝数据、负载均无变化 |

**启动与持久化**：`/etc/init.d/bluetooth-mu300`（`START=96`）负责 装载模块 → 等
`/dev/ttyBT0` → `mu300-bt-init` + `btattach` → `bluetoothd`，已 `enable`；BD_ADDR 写在
`/etc/mu300/bluetooth.conf`（`/var/lib/bluetooth` 是指向 `/etc/bluetooth/keys/` 的软链，
落在 rootfs 上，配对与设备缓存重启后保留）。

**第 6 个缺陷的运维规避**：`mtty_probe` 不注销 notifier，所以 `bluetooth-mu300 stop`
只停 `btattach`/`bluetoothd`、不 `rmmod`，避免反复装卸累积重复项（真要卸载用单独的
`unload` 子命令，并在之后不再 attach）。

## 8. 应用层注意（与驱动无关）

AirSIM 的 LuCI「蓝牙」页把开关、扫描、配对都实现为设备写操作，走
`airsim-mu300d --manage device-write`，需要管理独占锁。**AirSIM 服务运行时这些按钮会
直接返回「服务仍在运行或设置正在保存，请先关闭服务后重试」**，必须先到「设备概览」
关闭服务，扫完再启动 —— 这是应用侧的设计，不是驱动或 BlueZ 的问题。

另外，BLE 设备在广播里不带名称时，BlueZ 的 `Alias` 会退化成"地址的冒号换短横线"，于是
列表里会出现"地址 · 同一地址"的观感；这类设备只有在连接后通过 GATT Device Name 才能
拿到真名。

## 9. 对上游的建议

蓝牙驱动不属于本仓库的职责范围（README 明确系统/内核保持原样），所以这里只留结论。
真要推进，建议路径是：把第 4 节的 6 个缺陷作为 bug report 提给 Unisoc vendor 内核维护方，
或在一个独立仓库里存放"针对某个内核版本的 `sprdbt_tty` 重编脚本 + 补丁"。
