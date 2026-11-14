# 用户来电接听退出：设备端 SIP 号码修复与客户端待核对项

## 范围与结论

用户报告其他用户的 U30 R23：呼出正常，呼入经 CallKit 接听后返回拨号页，
对端短暂无声后掉线；本地及云端都有反馈。不是当前 USB 测试设备。
反馈 App 为 0.9.8 (175)，后续称 TestFlight 最新版；没有新构建号可核对。
没有修改客户端、上游工作树或设备，也没有自动通话、安装或发布。

截图可见通话记录把 `sip:…@[IPv6]` 当作来电号码。现有客户端日志有 12 组
answer_observed / stopped，其中 3 组包含 pcm_ready / first_downlink。
verbose_enabled=false、agent_events=0，因此不能判断是谁先结束通话。
JSON 的停止后 PCM 计数为零不证明整段通话从未建立媒体；没有崩溃诊断。

## 已复现并修复的设备端缺口（尚未用户实机验收）

MU300 的 CLCC 号码此前原样写入 callRecord。通话槽身份替换条件使用
normalizeDialNumber 比较号码：SIP URI 返回空字符串，普通号码返回数字。
若同一来电在 incoming → active 时由 SIP 地址改报普通号码（或反向），
旧代码结束原 UUID 并分配新 UUID。ATA 成功后，原接听请求反而返回
“接听期间目标来电已结束，未接听其他通话”。这一缺口同时影响本地和云端
复用的设备端状态机。截图没有证明该用户发生了这种格式切换，不能据此
认定用户故障已经定位或修复完成。

隔离 Agent 补丁增加 mu300_caller_identity.go：

- 仅针对 MU300 基带上报的 numeric SIP/SIPS subscriber，投影为号码。
- 接受有效主机／IPv6、端口以及限定的 user=phone、transport 参数。
- 不猜国家码，不把 phone-context、ext、密码、非数字用户名或未知参数
  静默丢弃；不支持的地址原样保留，不把 IPv6 数字拼进电话号码。
- 同槽、同方向、同号码表示变化保持 ID；不同号码、方向变化、已接通槽
  回到响铃仍分配新 ID。不同不透明地址不再因归一化为空而互相混淆。
- 不改变拨号输入、短信收件人、SIM 本机号码、鉴权及其他平台解析。

依据：[RFC 3261](https://www.rfc-editor.org/rfc/rfc3261.html) 的 SIP user/host
结构；这是蜂窝基带号码展示投影，不是通用 SIP URI 身份等价算法。
[RFC 3966](https://www.rfc-editor.org/rfc/rfc3966.html) 的 phone-context
不能当作可直接拼接的号码前缀，本补丁不做这种转换。

## 回归证据

测试仅用合成号码、2001:db8::/32、192.0.2.0/24 和 example 域名，无真实电话。

- 改动前 TestMU300SIPAnswerKeepsIncomingIdentity 两个方向均失败：
  ATA 发出一次，随后原 ID 被终止，接听返回错误。
- 改动后两个方向均保留 ID、AndroidID、active 状态，号码投影正确，无假结束历史。
- 覆盖不同来电、不透明用户名、同号码 IMS 主机变化、槽位复用、非法 URI、
  扩展／上下文、端口、国家码不猜测及 SIP 不成为拨号输入。
- 隔离 Agent 全量 go test -race ./...、go vet ./... 通过。
- 固定源码重新应用补丁成功，与已测隔离源码逐文件一致；干净副本全量 race/vet
  通过，Linux arm64 静态构建通过。仅生成 Agent 测试二进制，未构建／安装新 APK。
  候选标记 0.4.7-mu300.0.1.0-r23-sip.1；实机待验证。

## 客户端需要核对的两项（本仓库没有修改）

1. **来电显示号码、历史显示备注**：只读本地 AirSIM 源码发现
   CallKitController.synchronizeOwnedCall 传 callerName:nil；
   AppModel.presentOwnedCall 创建本地 IncomingVoIPCall 时也只传 number。
   ContactStore 的历史展示另行调用联系人匹配，所以两处展示不一致有独立
   可疑路径。原始 SIP 地址也会妨碍联系人匹配。应在 build 175 对应源码
   验证，并复用本机联系人缓存填充 CallKit 名称；不能上传通讯录到 U30。
2. **纯本地模式后台保活**：当前本地源码模式切换没有直接关闭 standby，
   但 standby 启动还取决于 enabled、appIsBackground、suspendedForCall、
   CallKit audioSessionIsActive。需要记录模式切换及这些条件，并核对
   CallKit 接听期间 suspend/resume、音频会话 deactivate 的顺序。
   “纯本地模式必然禁用保活”尚未代码／实机证实。

本地客户端工作树含用户修改，且不能证明与 TestFlight 175 一致，以上是
待验证代码线索，不是对该发行构建的已确认结论。

## 下一次用户验收

先取得目标用户系统／内核与设备日志，确认适用包后再安排候选安装。
让用户手动测试本地、云端各一通来电，接听后双方轮流说话并手动挂断。
对齐 CLCC 的脱敏号码形式（普通／SIP，不记录真实号码）、槽位、方向、
状态、逻辑 ID、接听回执和客户端 CallKit finish/deactivated。
启用详细日志后从响铃开始采样，故障后立即导出。重点确认来电至 active
不更换 ID；不能仅以联系人显示修复或 PCM 字节增长替代双向可听验收。

## 新 verbose 反馈与独立 R23 修复候选

新反馈：[FEEA8AED](https://app.notion.com/p/415f8285a7fd81398c10e4c21114b94b)，
2026-11-11 23:13（北京时间）导出，0.9.8 (175)，verbose_enabled=true。
与上一份逐行比较：原 83 条事件按原顺序保留，新增 22 条，均为 VoWLAN
探测超时／浏览器重建。新增接听、CallKit finish、hangup、stopped 事件为 0。
最后一次 answer_observed 仍为 22:16:58.081，详细探测事件集中在
22:33 与 23:12–23:13，agent_events 仍为 0。因此这份日志没有补全一次
启用 verbose 后的接听故障；不能由探测超时倒推出此前退出的原因。

为便于只测试 SIP 修复，另从本地已发布标签 v0.1.0-r23 导出隔离候选，
只回移 mu300_caller_identity.go、对应回归测试和 mu300_calls.go 两处变更。
没有把 develop 的其他设备扩展混入。候选包版本为 0.1.0-r24，
同步更新了隔离源码的 VERSION / 安装器版本常量，但**没有正式发布 R24**。
不要使用候选源码安装器的 GitHub 下载地址；该版本尚未上传。

- 产物：`out/incoming-r23-candidate/dist/luci-app-airsim-0.1.0-r24.apk`
- 源码：同目录 `airsim-mu300-r24-candidate-source.tar.gz`
- APK SHA-256：`03cffca128855eeb542e1a6e2f08730831b0acd21cd8ca93505187380fda6985`
- 使用原项目受信公钥对应的私钥签名；私钥没有进入产物。
- R23 基线插件 race/vet、隔离 Agent race/vet、30 项 Node 测试通过。
- APK 签名、二进制与资源内容、固定 DSP/模块哈希、许可证／源码说明检查通过。
- 保留 R23 完整安装包的严格音频 ABI 预检；没有操作当前 USB 设备，
  没有受影响用户设备的预检或双向通话验收，未提交、推送或发布。

下一步：受影响用户开启详细日志后重新接听并导出；核对安装适用范围后，
人工安装候选并按上文完成本地与云端来电验收。候选只修复已证实的代码缺口，
不保证解决尚未记录到的 CallKit／后台保活故障。
