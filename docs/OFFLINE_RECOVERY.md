# 上游不可用时的恢复与构建

R27 开发预览，2026-11-24。目标是保住**已固定版本**的源码、构建输入和安装能力；
不是承诺上游未来的新系统、新内核自动兼容，也不把客户端/Relay 服务可用性包含在内。

## 两层保存

1. **随 Git 保存**：`third_party/` 包含真正的源码压缩包（非 LFS 指针），包括固定
   Agent 基线及原始 LICENSE/NOTICE、桥和 Agent 的 Go vendor。构建不再要求旁边
   存在 AirSIM 克隆，也不访问 Go 模块代理。锁文件同时校验归档和 go.mod/go.sum。
2. **独立依赖恢复库**：`third_party/recovery.lock.json` 列出固定的 ALSA 包/源码、
   旧音频工具及许可证、R16 音频模块和对应源码、DSP 备用文件、内核切换测试原文、
   内核源码/系统构建配方/参考模块。使用下述脚本保存到仓库和 `out/` 以外的目录。
   锁文件只有校验值，不能替代实际归档；发布时应把恢复库作为独立附件保存并备份。

内核归档只排除了原上游 `certs/signing_key.pem`，另有固定校验值；其余 tar 成员
保留，构建器同时认可这个明确的无私钥版本。DSP 来自此前固定公开副本，绝不从用户
设备提取；其许可未明确，见 [来源记录](DSP_FIRMWARE_PROVENANCE.md)。二进制和固件
不提交到源码 Git。既有签名私钥、配对、短信和设备备份也不纳入恢复库。

## 保存和恢复

维护者在固定输入都可取得时运行（替换示例路径）：

```sh
python3 scripts/dependency-vault.py capture /safe-storage/airsim-dependencies-20261123
python3 scripts/dependency-vault.py verify /safe-storage/airsim-dependencies-20261123
```

`capture` 拒绝覆盖已有恢复库；所有文件先验哈希，再保存。不会从网络下载。
把整个恢复库复制到第二块磁盘或项目自有存储，再运行 `verify`；一台电脑上的副本
不能抵御磁盘损坏。上游删库后，从**匹配本版本锁文件**的恢复库恢复：

```sh
python3 scripts/dependency-vault.py restore /safe-storage/airsim-dependencies-20261123
AIRSIM_OFFLINE=1 sh scripts/fetch-deps.sh
AIRSIM_OFFLINE=1 sh scripts/fetch-sources.sh
AIRSIM_OFFLINE=1 sh scripts/fetch-firmware.sh
AIRSIM_OFFLINE=1 sh scripts/build.sh
```

恢复前校验完整清单；缺文件、错哈希、锁不匹配或符号链接立即失败，不用下载掩盖
丢失。`AIRSIM_OFFLINE=1` 禁止上述下载脚本联网；源码构建本身始终使用 vendor，
固定 `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`，缺工具链时明确失败。

## 安装与音频重建

已经保存的完整签名 APK 内含固定音频恢复包和 DSP 备用副本。缺固件时即使上游
不可用，也会校验并使用包内副本；已有设备文件保持不变。离线安装需事先保存同版
APK、公钥及校验清单，并使用已满足基础依赖的支持系统。恢复库不绕过签名、内核
配置、ABI 或设备空闲检查。

完整打包仍需要预装 Docker 及可用的 `node:22-alpine` 镜像和自己的签名私钥。
Go、Python、Node、Git、OpenSSL 和宿主系统不包含在源码归档内；不能把这里描述成
整个 OpenWrt 系统或全工具链的封闭构建。保存构建机时应另行 `docker save` 保存镜像，
并记下镜像 ID，离线 `docker load` 后再打包。

如需重新编译匹配的音频模块：

```sh
sh scripts/prepare-audio-offline.sh /safe-storage/airsim-dependencies-20261123
```

脚本恢复既有固定原文并解包 `out/openwrt-audio/{linux-reference,audio-reference,release-kernel}`，
拒绝覆盖已有工作目录。音频源码已包含 R16 的两个补丁，不再重复应用。
再按 [音频构建说明](../packaging/openwrt-audio/README.md) 挂载 downloads 和 reference
目录执行构建及完整 ABI 检查。Linux 编译容器及其中的编译工具仍需提前准备；本轮
保存源码不等于重新完成内核编译，也没有操作设备或切换内核。

## 每次更新与发布

开发工作树的完整离线检查入口：

```sh
python3 scripts/check-offline.py --vault /safe-storage/airsim-dependencies-20261123
```

它在全新目录复制源码，使用空 Go 模块/构建缓存，拦截 git 网络操作、curl/wget，
恢复输入并执行 ARM64 构建、双方 Go race/vet、所有 Node 测试、恢复/安装 fixture
和音频源码解包。不会运行设备命令。结果写到 `out/offline-check.*/result.json`。
2026-11-23 已通过本地冷验证，网络下载尝试为 0；未执行完整 APK 打包或内核编译。

- 更新上游基线/依赖时，一并更新保存文件、锁与来源；不删除旧版本恢复库。
- 离线冷构建必须使用新的目录、空模块缓存及禁用的网络下载入口，不能只依赖开发机缓存。
- Release 除现有签名包、源码、许可证外，应保存与其锁匹配的依赖恢复附件及 SHA-256。
  上传后重新下载校验；未上传的本地归档不能称为用户可下载的镜像。
- `scripts/package-sources.sh` 现在先调用 `scripts/package-recovery.sh`；缺任一
  固定输入即失败。可用 `AIRSIM_DEPENDENCY_VAULT` 指定已保存的恢复库。生成的
  `airsim-mu300-<版本>-recovery.tar.gz` 是独立附件，不把固件混入源码包。
- 不备份用户数据或私钥，不以归档为由修改上游许可证及署名。
