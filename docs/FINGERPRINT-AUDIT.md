# Xray-WinOptimize 被动网络指纹安全审计

**审计日期：2026-10-30**  
**审计范围：** 最后提供的 `Xray-WinOptimize-Linux-Design-Wording-Full.zip` 中 Fast Ramp v3.3 的 **主构建（Winsock 接收缓冲区控制器）**。不把历史 WinDivert 实验后端与主构建混同。  
**证据限制：** 已检查项目静态源码和 Microsoft/TCP/GFW 公开文档；**没有**从用户 Windows 机器获取原始 SYN、ACK 或 PCAP，也没有 GFW 内部规则。因此不能断言线上流量与原版完全相同，更不能保证无法被识别。GitHub 线上仓库未能直接读取，本审计针对上述本地源码包；如用户有未提供的新提交，须另行核对。

## 2026-10-30 更新：v3.4 默认路径指标缓存（与本报告原始快照区分）

本报告其他章节记录的是 **v3.3 旧默认行为**，其中“热连接在握手前复用 SO_RCVBUF”已在 v3.4 中默认关闭。新版本的默认行为为：

- **连接建立前：** 不根据历史缓存设置 `SO_RCVBUF`；仍由 Windows 原生 Winsock 生成 SYN，未显式设置 `XRAY_WINOPT_TCP_TIMESTAMPS` 时也不主动更改时间戳选项。
- **连接建立后：** 可读到目标最近一次连接的平滑 RTT 历史，但它仅用于当前 `SIO_TCP_INFO.RttUs` 暂时未知时组织观测周期；当前连接没有真实 RTT 样本就**不得扩容**。
- **接收缓冲区：** 每条连接按自身观测的流量独立计算是否扩容；仍会在连接中途通过 Windows `SO_RCVBUF` 产生可能可观察的窗口变化。
- **旧热连接性能模式：** 仅在用户设置 `XRAY_WINOPT_PRECONNECT_CACHE=on` 时重新启用旧的预握手缓冲区容量继承，此时本报告原有热连接指纹风险仍成立。

这项代码改变**缩小了默认配置中由历史容量继承产生的特定 SYN 指纹面**，但不等同于已实测证明“完全无指纹”；仍需 Windows 本机 SYN/窗口抓包及多网络条件测试。Linux `tcp_metrics` 是启发，不是本项目实现了 Linux TCP 栈。

## 0. 核心结论

- **默认主构建不自造 TCP/IP 报文。** 仍由 Windows Winsock/IP 栈产生 SYN、ACK、SACK、FIN/RST、IP TTL、TCP Options 等；程序通过 `SO_RCVBUF` 影响接收缓冲区及后续通告窗口，并有可选 `TCP_TIMESTAMPS` 设置。
- **握手层面：冷启动无缓存且未启用时间戳实验时，增长算法不直接修改初始 SYN。** 但目标缓存命中时，会在 `connect` 前设置 `SO_RCVBUF`，可能影响 SYN 初始 Window 与 Window Scale；具体字段是否变化需实机抓包，不应凭源码猜值。
- **连接中后期：存在可观察差异。** 通告窗口大小、扩容阶段、窗口更新及 ACK/数据节奏可能与原版不同。TCP 这些字段不加密，但 `setsockopt` 数值、程序日志、200ms 采样时刻和缓存表本身不在网线上明文传输。
- **高优先级风险是跨连接缓存与时间戳实验，而非“改过 Xray 源码”本身。** 开启时间戳时 SYN 选项可能明显变化；默认设置为空，不应把它当作当前真实指纹。
- **公开研究不支持“GFW 会因该算法而直接判定 Xray”的确定性结论。** GFW 已证实进行被动流量分类，但公开实证聚焦首包特征、协议指纹等；针对这种接收窗口演化进行特定识别的现实使用没有证据可确认。

## 1. 实际代码证据及暴露路径

| 代码 | 触发与行为 | 是否直接出现在线路上 | 结论 |
|---|---|---|---|
| `patch/apply_portable.py` | 只修改 Xray 默认 TCP `net.Dialer.Control`、拨号后监控；不改 REALITY/Vision/TLS 编码 | 否 | 业务层字节序列未被此补丁主动重写 |
| `portable/socket_windows.go:winOptApplyTCPOpts` | `XRAY_WINOPT_TCP_TIMESTAMPS=on/off` 时在握手前调用 `setsockopt(TCP_TIMESTAMPS)` | **是，可能变更 SYN 选项及后续时间戳** | 默认空值时不调用 |
| `winOptBeforeConnect` | 命中按目标缓存时在握手前 `setsockopt(SO_RCVBUF)` | **可能改变 SYN 初始窗口或 WS** | 这是握手指纹首要核查点 |
| `winOptAfterConnect` | 握手完成后每 200ms 用 `SIO_TCP_INFO` 采样；增长时 `SetReadBuffer` | **间接改变 Window/ACK 行为** | 冷连接最早在收到一定 TCP 数据后改变 |
| `portable/controller.go:Observe` | 一个活跃 RTT 周期、阈值、趋势估计、4 倍单次上限、400ms 冷却、64KiB 对齐 | 算法本身不会明文出现；**输出可能反映在窗口序列上** | 攻击者只能推断输出，无法直接观察内部决策 |
| `winOptRemember` / `winOptKnown` | 每目标缓存成功设置的大小 10min，后续预连接复用 | **间接影响多次握手** | 可能造成冷/热连接差异或跨流相关性 |
| `.github/workflows/upstream-release.yml` | 主版本仅运行便携的 Winsock 补丁 | 否 | 不依赖 WinDivert 原始报文栈 |
| `adapter/packet_windows.go` | 旧 WinDivert 后端源码，单独实验构建 | **如果启用另一构建，TCP 指纹可能全面改变** | 不在此次默认构建风险结论内 |

所有代码行号与字段应以对应 GitHub Commit 为准；此处列的是实际函数和调用链。

## 2. 分层指纹面

### 2.1 IP / TCP 初始握手

- SYN 的 TTL、MSS、TCP 选项排列、SACK permitted、ECN、Timestamp、Window、Window Scale 都可能用于被动 OS/堆栈识别。
- 冷连接（无已学习目标缓存，默认时间戳未设置）：该补丁调用 Windows 自带拨号器，不自己构造 SYN，亦未显式改 MSS、TTL、SACK、ECN；所以**没有直接证据表明初始握手与相同 Windows/Go 版本的原版不同**。但这一判断仍须通过抓包验证。
- 热连接（10min 缓存命中）：在 SYN 之前设置 `SO_RCVBUF`；Windows 可以在此时决定初始通告窗口、Window Scale；可能导致热连接 SYN 与冷连接不同。是否发生、差异多大、正常其他 Windows 应用是否同样出现，由 Windows 版本与 Socket 行为决定。
- 时间戳实验开关为 `on` 时，会在 SYN 前设置 `TCP_TIMESTAMPS`；这是相对明确、容易观察到的握手差异；**默认不开启**。
- 不要假设 `SO_RCVBUF=2MiB` 就一定意味着 TCP 协议头 Window 等于 2MiB。TCP 16bit Window 字段与 SYN 协商的 WS 必须结合计算，Windows 仍可裁定实际通告值。

### 2.2 连接过程中的 TCP 行为

- `SO_RCVBUF` 影响接收窗口管理，ACK 的 Window 字段可见，可能出现扩容阶跃、较大窗口平台、较少窗口受限时段、不同 ACK 时间序列。
- Windows 内核仍掌管 ACK/SACK/RTO/重传。应用侧每 200ms 采样 **不意味着**每 200ms 发出独特的网络包；同理 64KiB 对齐的 Socket 请求值不会按原数字写进 TCP Option。
- 缓冲区越大可改变服务器端 BBR 的发送速率与 burst/reordering，从而改变两方向包长、间隔、持续时长；这是**间接流量指纹**，也受链路、服务器、负载的巨大扰动。
- 攻击者若有足够多同网络环境下的真实样本，理论上可训练“优化 Windows vs 未优化 Windows”分类器；其是否能高度区分、是否能判断为代理、GFW 是否使用该特征，均未证实。

### 2.3 TLS / REALITY / XTLS Vision

- 补丁仅接触 `net.Dialer` 和 TCP Socket，不改 `ClientHello` 内容、SNI、TLS 扩展排列、ALPN、REALITY 握手内容或 VLESS/Vision 编码；因此 **TLS/协议层直接指纹未由补丁改变**。
- TCP 分段边界、首包时机可能间接受调度与网络条件影响；TLS 字节流的相同性并不保证 packetization 相同。
- TLS/REALITY 自身的已有识别风险以及目标 IP 连接模式仍然存在，不可将其混同为本补丁新引入的风险。

### 2.4 跨连接及目标关联

- 程序按目标地址缓存大小 10min；冷热连接可能出现 SYN 窗口差异，即使应用层 TLS 近似一致。
- 不同目标可能学习到不同大小；同一目标后续会趋向一致的需求值。该规律可能被观察，但 TCP 接收窗口因应用/网络条件而变化是常见现象；不能直接定义为唯一 Xray 指纹。
- 现有实现不附加网络标记、不发送额外探测数据包、不改变远端目标地址，也不向外发送调谐日志。

## 3. 定性风险分级（非 GFW 实证结论）

| 层面 | 被动可见性 | 相对原版的差异 | 对“识别为 Xray”的特异性 | 核查优先级 |
|---|---|---|---|---|
| 默认冷连接 SYN | 高 | 预计小或无直接变化 | 低/未知 | 高：确认零变化 |
| 缓存热连接 SYN Window/WS | 高 | **可能变化** | 未知；可能与普通 Socket 调优重叠 | **最高** |
| 显式开启 TCP_TIMESTAMPS | 高 | **可能明显变化** | 中等以下/未知，非 Xray 独有 | **最高**（默认保持关闭） |
| 中途通告窗口/Window Update | 高 | **确定存在变化可能** | 低至中等/未知 | **高** |
| ACK/SACK/速率/包长时序 | 高 | 间接变化 | 低至中等/高度依赖数据 | 中高 |
| TLS ClientHello 内容 | 高（握手未被加密部分） | 代码未主动更改 | 项目引入增量预计无 | 中（抓包核对） |
| 自定义 TCP 栈实验后端 | 高 | 可能全面变化 | 未知，风险明显高于 Winsock | 单独项目审计 |

**重要解释：** “被动可见”与“可被唯一识别为代理”不是一回事；同一个指标可被观察并不意味着具有高分类准确率。

## 4. 可复现的 Windows 本地验证（无需 VPS SSH）

> 这些命令需要管理员 PowerShell 运行 `pktmon`，但 Xray 本身仍可以普通权限启动。仅需一个熟悉的 VPS 测试目标、相同 Xray 版本和相同配置。捕获的 PCAP 含连接 IP、时序及 TLS 握手元数据，不应公开上传，尤其不要与 REALITY 配置密钥一起上传。

### 4.1 采集三个状态

**A 原版/优化关闭：**

```powershell
$env:XRAY_WINOPT_MODE = 'off'
Remove-Item Env:XRAY_WINOPT_TCP_TIMESTAMPS -ErrorAction SilentlyContinue
# 启动与 B、C 相同版本的 xray-winopt.exe，连接同一个 VPS
```

**B 优化冷启动：** 完全退出 Xray 后在新进程中运行；无需设置特殊模式，确认不带用户自定义 `SO_RCVBUF`。同样采集一次新 TCP 连接。

**C 优化热连接：** 保持 B 的 Xray 进程不重启，在 10 分钟内发起到同一目标的第二次连接；尽量保证之前的下载使 `WINOPT expanded` 实际发生，日志确认 `WINOPT preconnect`，否则 C 不是有效的热连接样本。

**每轮抓包：** 保持同一上游版本、同一配置、同一 TLS 指纹设置和相近负载。在独立的管理员 PowerShell 中（下例目标 IP 仅作为示范，请改成当前节点地址）运行：

```powershell
mkdir C:\XrayFingerprint -Force
cd C:\XrayFingerprint
pktmon stop 2>$null
pktmon filter remove
pktmon filter add -i 167.234.222.106 -p 443 -t tcp
pktmon start --capture --comp nics -p 128
# 此时在另一个窗口新建连接并做相同的小规模下载，确保抓到 SYN
pktmon stop
pktmon etl2pcap PktMon.etl --out A.pcapng  # B/C 轮请替换文件名
pktmon filter remove
```

微软官方支持 `pktmon` 及 `etl2pcap` 在 Windows 11 上使用。不同版本的参数、输出文件位置可用 `pktmon help start`、`pktmon help etl2pcap` 校验。`-p 128` 主要保留包头并减少敏感应用负载；若网络虚拟化/卸载导致抓包缺少 SYN，则调整抓包位置或使用 Wireshark + Npcap。`--comp nics` 可减少同一包在不同栈层被重复记录，但抓到的校验和仍可能受网卡卸载影响。

### 4.2 在 Wireshark 中对比

筛选该目标后，提取/观察字段：

| 位置 | 关键字段 | 比较目的 |
|---|---|---|
| Client SYN | `tcp.window_size_value`、`tcp.options.wscale.shift`、`tcp.options.mss_val`、`tcp.options.sack_perm` | A/B/C 的初始握手是否相同 |
| Client SYN | TCP Options 原始列表顺序、是否有 `tcp.options.timestamp.tsval`、`ip.ttl` | 是否出现新的被动 OS 指纹 |
| 后续客户端 ACK | `tcp.window_size`、`tcp.analysis.window_update`、`tcp.analysis.zero_window` | 窗口增长轨迹是否异常或频繁阶跃 |
| 两方向数据流 | `tcp.len`、`tcp.time_delta`、重传、SACK | 包长与时序差异；需同一负载、多次采样 |
| 初始 TLS 数据 | ClientHello 的扩展顺序、SNI、ALPN（如果可见） | 初步检查 Socket 补丁是否影响上层握手；**128 字节截断通常不足以完整对比 ClientHello** |

**判定规则：**

- A 与 B 首个 SYN 完全相同：支持冷连接握手无额外指纹的判断，但不等于整个会话不可分辨。
- A/B 相同、C 的 SYN 不同：优先调查 `winOptBeforeConnect` 的预握手缓存设置。
- 仅开启时间戳实验后 SYN 选项不同：属于明确的配置影响，不应归因于默认调谐算法。
- SYN 相同、后续窗口不同：这是补丁本身的预期影响，应评估该差异是否自然、是否在多种网络负载下稳定及独特。
- 抓包中出现 TCP checksum 异常：先考虑 Windows NIC checksum offload，不要自动推断网络包损坏或代理指纹。

### 4.3 为什么这项验证还不等于测试 GFW 识别能力

本地抓包只能证实**上网链路中可能观察到的包差异**。运营商 NAT、隧道、网络中间盒、非对称路由等可能改变最终被观察的报文；GFW 的实际部署规则及训练样本不公开。没有跨多次、不同负载和路径的测量，不能给出所谓识别概率。

## 5. 修复与发布建议（未在本次审计中修改程序代码）

1. 默认坚持 Windows 原生 TCP；**不要**默认改 `TCP_TIMESTAMPS`、TTL、MSS、SACK、ECN 或伪装操作系统选项顺序。
2. 对预连接缓存做一项可选的关闭开关（**当前源码尚未实现**），用于判断“热连接 SYN 指纹风险”是否值得以性能换取；不建议未测量就直接删除缓存。
3. 添加 CI 的**抓包回归测试**：至少对同一 Windows 版本比较 baseline/cold/warm 的 SYN Option 顺序、Window Scale、Timestamp 状态、初始 Window 等，并记录受 Win11 版本和网络条件影响的字段。
4. 保留 `XRAY_WINOPT_MODE=off` 与用户显式 `SO_RCVBUF` 优先逻辑；安全敏感用户可回退到不施加 Socket 选项的模式。
5. 若未来将独立 WinDivert/netstack 后端设为默认，必须重新做完全不同级别的 TCP/IP 指纹审计：ISN、TTL、IPID、WS、SACK/DSACK、RTO、重传、FIN/RST、TCP Option 顺序等。
6. 项目公开 README 不应宣称“无法被 GFW 检测”“无指纹”；可以准确写“优化器不修改 REALITY/TLS 内容，默认仍使用 Windows TCP，但 TCP 窗口时序可能变化”。

## 6. 资料

- [微软：SIO_SET_COMPATIBILITY_MODE 与 SO_RCVBUF / autotuning](https://learn.microsoft.com/en-us/windows/win32/winsock/sio-set-compatibility-mode)
- [微软：Windows TCP 特性、Window Scale 与 Timestamp](https://learn.microsoft.com/en-us/troubleshoot/windows-server/networking/description-tcp-features)
- [微软：IPPROTO_TCP Socket 选项（含 TCP_TIMESTAMPS）](https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-tcp-socket-options)
- [Wireshark：TCP 解析字段](https://www.wireshark.org/docs/dfref/t/tcp.html)
- [微软：pktmon 使用与过滤](https://learn.microsoft.com/en-us/windows-server/networking/technologies/pktmon/pktmon-syntax)
- [微软：pktmon ETL 转 PCAPNG](https://learn.microsoft.com/en-us/windows-server/networking/technologies/pktmon/pktmon-pcapng-support)
- [USENIX Security 2023：How the GFW Detects and Blocks Fully Encrypted Traffic](https://www.usenix.org/conference/usenixsecurity23/presentation/wu-mingshi)
- [IMC 2020：How China Detects and Blocks Shadowsocks](https://gfw.report/publications/imc20/en/)

**最后界限：** 此审计完成了**静态代码核查**与公开机制风险分析，**没有完成实机线上包对照**。PCAP 结果才可以把“可能改变的指纹”进一步判定成“确实改变的字段”；即便 PCAP 与原版高度相似，也不能证明 GFW 无法识别 REALITY 或其他代理行为。
