# mu300-at 兼容层（opt-in）

这个目录里的东西**默认不会被安装**（`packaging/openwrt/Makefile` 只拷贝 `root/`），
因为它是可选的运行期兼容层，不是 LuCI 插件的一部分：它接管的是镜像自带的
`/opt/mu300/bin/mu300-at`，而那个脚本属于设备固件，不属于本仓库。要不要接管理应由
设备主人显式决定。

## 它解决什么

镜像自带的 `mu300-at` 只支持 `mu300-at [-t SECONDS] "AT+命令"`。在 5G SA + 双卡设备上，
这会在两处卡住 AirSIM：

1. **stdin 模式缺失**：r24 起的 agent 用 `mu300-at -` 把指令写进 stdin。自带脚本把 `-`
   当成一条 AT 指令发给基带，得到 `+CME ERROR: 0`，于是 CLCC 轮询全失败、agent 上报
   `calls.available=false`，手机端提示"没有可用的拨号链路"。
2. **`AT+CSQ` 在 5G SA 下恒为 `99,99`**：信号其实是好的，真值只在 `AT+CESQ` 尾部的
   SS-RSRP / RSRP 里。于是面板上的信号一直是空。
3. （可选）**语音卡槽被静默改路由**：`/run/mu300-radio-freeze` 存在且
   `/run/mu300/sim-slot=1` 时，自带脚本会把"没指定 `MU300_AT_DIR`"的调用改到卡2的
   通道。若卡2 是没有 VoLTE 的纯数据卡，`ATD` 就永远接不通。本层允许把一个卡槽钉给
   语音命令，**默认关闭**。

## 安装

```sh
# 1) 备份并安装
sh /path/to/at-compat/enable.sh          # 自带脚本保存为 mu300-at.vendor
sh /path/to/at-compat/enable.sh --check  # 只打印现状

# 2) 验证
mu300-at 'AT+CLCC'          # 和以前一样
printf 'AT+CLCC\nAT+CPIN?\n' | mu300-at -   # 新增：stdin 模式
mu300-at 'AT+CSQ'           # 5G SA 下现在返回 0..31 的可用值

# 3) 回退
sh /path/to/at-compat/enable.sh --disable
```

`enable.sh` 只改 `/opt/mu300/bin/mu300-at` 这一个文件（`/usr/bin/mu300-at` 通常是指向它
的软链，不用动）。它用脚本首行注释判断当前装的是自带脚本还是本包装层，重复执行是幂等的。

## 语音卡槽（可选）

```sh
echo 0 > /etc/airsim/voice-card     # 0 = 卡1（/run/mu300-at），1 = 卡2（/run/mu300-at4）
rm -f /etc/airsim/voice-card        # 删除即恢复默认：不钉任何卡槽
```

只有 `ATD*`、`ATA`、`ATH`、`AT+CHUP`、`AT+VTS`、`AT+CLCC`、`AT+CLIP`、`AT+CMUT`、
`AT+CHLD`、`AT+CTFR` 会被钉到指定卡槽；其它命令（数据、短信、SIM 操作）原样透传，
调用方自己的 `MU300_AT_DIR` 不受影响。

## 边界

- 不打开 tty、不直接写串口：所有指令仍然经过 `mu300-atd` 的 FIFO，串行化与互斥完全交给它。
- 不修改 `/etc/config/airsim`，不改固件里的任何文件；卸载就是一次 `cp` 回来。
- `-t SECONDS` 原样透传；退出码沿用自带脚本的语义（`0` 成功、非 `0` 失败）。
- `AT+CESQ` 也拿不到有效值时（2G/3G 或基带不支持），回退到原来的 `AT+CSQ` 输出。

## 测试

`packaging/openwrt/at-compat/mu300-at` 用纯 POSIX sh 写，除 `awk`、`tr`、`readlink`
外不依赖额外命令，可直接用一个桩脚本在 PC 上跑通两种模式：

```sh
cat > /tmp/at.vendor <<'EOF'
#!/bin/sh
echo "[$*]" >&2
echo '+CESQ: 30,99,255,255,255,25,255,255,40'
EOF
chmod +x /tmp/at.vendor
MU300_AT_REAL=/tmp/at.vendor sh packaging/openwrt/at-compat/mu300-at 'AT+CSQ'
```
