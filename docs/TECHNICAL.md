# Xray-WinOptimize 技术文档（交接主文档）

> 版本：Windows TCP Optimization v3.4（Path Metrics Cache），2026-10-30。**此前版本有用户报告的性能提升；本次加速增长尚未在真实 Windows 测速验证，完整工作流构建仍需验证。** 默认工作模式仍是免管理员的 Windows Winsock；旧 WinDivert 独立协议栈保留为手动实验构建，见附录。每次修改代码、测试、补丁或 Actions，必须在同一次变更中更新本文和变更记录，`docs-check` 仅检查是否修改文档，不能代替审核文档内容。

## 0. 目标及最重要的限制

目标：**借鉴** Linux 内核 `tcp_rcv_space_adjust()` 和 `tcp_rcvbuf_grow()` 的增长设计思路，开发独立的 Windows Socket 接收缓冲区增长策略，同时保留 Windows Xray 的单 EXE、普通用户权限、零驱动、现有 SOCKS5/HTTP 和 VLESS/REALITY/Vision 配置。作用范围是经过 Xray **默认 TCP 系统拨号器**建立的出站连接，**不包括整台 Windows 系统的 TCP，也不保证覆盖第三方自定义拨号器**。

**设计边界（不是 Linux 内核算法移植）：** Linux 用 `copied_seq` / 接收队列进度计算“应用每接收 RTT 已读多少”，以 `rcv_rtt_est`、实时乱序队列跨度、`sk_rmem_alloc` 和系统 TCP 内存压力进行决策。Windows `SIO_TCP_INFO` 只暴露 `BytesIn`（TCP 层接收字节）、`RttUs`（发送方向估计 RTT）、累计 `BytesReordered`、`RcvWnd` 和 `RcvBuf`；这些**不是等价内核变量**。用户态不能控制 TCP 内核 ACK/SACK、窗口通告实现和 skb 内存记账。显式 `SO_RCVBUF` 会改变/停止 Windows 该 Socket 的原生接收窗口自动调谐；之后本控制器接管的是**缓冲区配置请求**，不是 Windows 内核窗口算法。

本版不再把固定 2 MiB 或 8 MiB 说成 Linux 默认值。Linux `tcp_moderate_rcvbuf` 默认启用、上限由 `tcp_rmem[2]` 控制，随内存条件变化；Windows 模拟采取自己的安全参数。引用：

- Linux 当前 `tcp_rcv_space_adjust/tcp_rcvbuf_grow`：https://github.com/torvalds/linux/blob/master/net/ipv4/tcp_input.c
- Linux TCP sysctl： https://kernel.org/doc/html/latest/networking/ip-sysctl.html
- Windows `SIO_SET_COMPATIBILITY_MODE` 与 SO_RCVBUF： https://learn.microsoft.com/en-us/windows/win32/winsock/sio-set-compatibility-mode

**对外术语规范：** README、GitHub About、Release 和项目介绍统一使用“借鉴 Linux TCP 接收缓冲区自动调谐的设计思路”，不得使用“支持 Linux 风格自动调谐”“移植 Linux TCP 调谐算法”“使用 Linux TCP 栈”等可能暗示等价实现的描述。`Linux-inspired` 仅作为历史源码注释或历史版本称呼；技术说明必须明确 Windows `SO_RCVBUF` 与 Linux 内核窗口算法的差异。

## 1. 源码改动对应表

| 项目文件 | 具体职责 |
| --- | --- |
| `portable/controller.go` | 时间戳实验参数解析；RTT 周期测量；借鉴 Linux 思路的 2 倍基础量 + 趋势增长；有界乱序提示、通用一次性试探扩容和资源限制；v3.3 首次一个完整活跃 RTT 周期即可介入、调整间隔 `max(RTT,400ms)`、单次最多四倍；不使用 RcvWnd 百分比门槛及固定容量特例 |
| `portable/controller_test.go` | 首次按需求增长（含单 RTT 快速介入）、短流、低 RTT、RTT 周期、满通告窗口仍扩容、400ms 冷却、按需四倍上限、超过 2 MiB 通用增长、冷却/上限/溢出、旧环境变量不影响算法等回归测试 |
| `portable/quota.go`、`portable/quota_test.go` | 并发安全的进程级 Socket 请求预算，成功申请、增额、回退、关闭释放 |
| `portable/history.go`、`portable/history_test.go` | 有界、带 TTL 的按目标 RTT 历史缓存；只保存 RTT 指标，不保存缓冲区容量；首次 RTT 未就绪时作为观测周期提示。包含预连接容量缓存显式开关解析及行为测试 |
| `portable/socket_windows.go` | 可选连接级 TCP 时间戳（仅握手前，失败可回退）；Windows `SIO_TCP_INFO_v1`、`GlobalMemoryStatusEx`、SO_RCVBUF、生存期监控、低内存判断、域名/IP 的 RTT 路径缓存、可选的旧容量缓存及并发拨号候选管理 |
| `portable/socket_other.go` | 非 Windows 不启用任何优化，仅供其他平台编译；与 Windows 调用签名一致 |
| `patch/apply_portable.py` | 在 Dialer.Control 中注入时间戳设置；严格锚点打 Xray TCP 拨号器补丁；复制五个优化模块 Go 文件到 `internet` 包；保留用户显式 `SO_RCVBUF` 优先 |
| `tests/test_portable_patcher.py` | 补丁锚点匹配、重复应用、对 UDP 分支保持不变、拷贝 RTT 历史模块及预握手功能默认禁用的回归测试 |
| `.github/workflows/upstream-release.yml` | 选择最新 Release（含预发布）、打补丁、测试、Windows x64 编译及 Artifact；默认不公开 Release |
| `docs/INSTALL-WINDOWS.md` | 用户操作、开关、默认内存上限与预算、回退 |
| `docs/TECHNICAL.md` | 实现事实、设计取舍、验证状态、变更历史（必须随代码同步） |

## 2. 借鉴 Linux 设计的 RTT 周期测量

`winOptController.Observe` 接收的 `BytesIn` 是累计值。维护上一采样及上个**测量 RTT 周期**的基线，而不是按固定 350ms 直接算瞬时速度。观察调用按 200ms 定时；每次测量周期至少为 `max(RttUs, 100ms)`。只在周期累计字节足够时参与增长决策。

- Linux 的真实测量是“应用每接收 RTT 读取的字节量”；本版只能以 **TCP 收到的数据**做代理。这会受到应用消费速度、网络乱序、队列排空和接收窗口限制的影响。
- Windows RTT 不等于 Linux 接收 RTT；使用 100ms 最小周期减轻极低 RTT 下的采样噪声。
- 当单轮累计接收小于 32 KiB、采样间隔异常、字节计数重置时，跳过增长或重置趋势状态。

## 3. 借鉴 Linux 设计的增长公式（Windows 自定义近似）

借鉴 Linux 当前 `tcp_rcvbuf_grow` 的设计意图，但以下系数、门槛和扩容规则属于本项目自己的 Windows 实现：基础目标为 **2 倍当前 RTT 交付字节数**，并为同比上一 RTT 的增长留出额外空间。

```text
current = 当前测量周期收到的 TCP 字节数
previous = 上一个有效周期收到的 TCP 字节数
base = 2 × current
if previous > 0 and current > previous:
    trend = 4 × current × (current - previous) / previous
else:
    trend = 0
suggested = 1.25 × (base + trend + bounded_reorder_hint)
```

其中 `bounded_reorder_hint` 为最近周期 `BytesReordered` 累计量增量（最多 256 KiB）。它**不等于实时乱序队列跨度**，故只能起有界预留提示的作用。`1.25` 是 Windows `SO_RCVBUF` 请求与实际通告窗口/内存空间缺乏 Linux skb 换算时采用的工程保守系数，**不是 Linux 内核常数**。

v3.3 单次调整在估算需求充分时最多增长**四倍**（不是强制四倍），按 64 KiB 对齐；先前成功设置后的调整间隔改为 `max(RTT, 400ms)`。增长采用**估算需求与当前缓冲区大小的比较**，不再使用 `RcvWnd < 0.7 × applied` 之类的百分比门槛：`RcvWnd` 是通告窗口，既不是当前缓冲区占用率，也不能单凭它判断是否达到路径 BDP。所有整数乘法采用饱和运算，防止溢出。

**统一的首次和后续扩容：** 初次不立即覆盖 Windows 的自动调谐；仍保留首次干预所需的轻量筛选（RTT ≥40ms、累计接收 ≥128KiB、**至少一个完整活跃 RTT 测量周期**，该周期接收 ≥32KiB），但**不再指定首次必设 1 MiB**，也不再以 Windows 当前 `RcvBuf <768KiB` 作为单独门槛。通过筛选后，首次与后续均执行同一公式：

```text
baseline = max(当前 Windows RcvBuf, 补丁上一次成功设置的 SO_RCVBUF)
current  = 本 RTT 统计周期收到的字节数
previous = 上一统计周期收到的字节数
base     = 2 × current
trend    = current > previous 且 previous > 0 时的增长补偿，否则为 0

若流量连续且不增长、该连接累计至少接收过一个当前缓冲区的数据（避免大缓冲区上的微小传输），可在整个稳定期做至多一次通用探索：
    base = max(base + trend, 2 × baseline)
否则：
    base = base + trend

target = 1.25 × (base + min(周期乱序字节计数增量, 256KiB))
若 target <= baseline：不调整
否则：
    next = min(target, 4 × baseline, 单连接上限)
    next = 向上对齐 64KiB
    若 next > baseline 且通过进程级预算和低内存检查：设置 SO_RCVBUF(next)
```

这里**没有固定为 1 MiB 或 2 MiB 的路径**。探索步骤用当前缓冲区的倍数表示，适用于 128KiB、512KiB、4MiB 等任何规模；当观测到接收量明显增长时重新允许一次探索，而平稳低吞吐数据不会持续无条件翻倍。探索前至少已接收一个当前缓冲区的数据量，这是一条按当前容量归一化的短流保护条件，而不是新引入某个固定 MiB 门槛。首次介入也受到 4 倍上限和预算限制；**新版默认不在后续连接的握手前复用上次缓冲区容量**，仅在用户明确开启旧容量缓存实验后才如此操作。

这项改变旨在让“接收窗口保持很大但仍不足以支撑 BDP”的连接不再被 70% 门槛错误拦截。代价是部分原本被旧门槛压制的连接会更早要求缓冲区、并可能导致不必要的 `setsockopt`；因此仍保留短流筛选、活动量要求、试探次数限制、冷却和预算，**暂不声称对所有网络一定更快**。

**历史兼容性：** `XRAY_WINOPT_GROWTH=aggressive` 在 v3.2 不再影响算法；所有连接都使用统一策略。需要彻底关闭优化时使用 `XRAY_WINOPT_MODE=off`，或使用旧版本二进制回退。Windows 自带接收窗口自动调谐在未显式设置 SO_RCVBUF 的连接上继续工作，已被补丁接管的连接不再与其行为等价。

## 4. 内存上限、进程预算与低内存保护

Linux 使用 `tcp_rmem[2]` 和内核内存压力判定，Windows 没有对等应用接口。本版通过 `GlobalMemoryStatusEx` 获取可用/总物理内存，用受控近似替代：

- **单连接默认上限：** `clamp(物理内存字节数 / 256, 4 MiB, 32 MiB)`；例如 4 GiB RAM 约 16 MiB，16 GiB RAM 上限 32 MiB。内存 API 失败时默认 8 MiB。
- **进程级 SO_RCVBUF 请求预算：** `clamp(物理内存字节数 / 128, 64 MiB, 512 MiB)`；例如 16 GiB RAM 的默认预算约 128 MiB。这个预算统计**我们为活跃 Socket 请求的缓冲区尺寸总和**，不是进程实际 RSS，也不包括没有受我们干预的 Socket。
- **低内存：** 若当前可用物理内存低于 `max(总内存的 5%, 256 MiB)`，暂停新申请／扩容；**不缩小已设定连接的缓冲区**。
- **手动覆盖：** `XRAY_WINOPT_MAX_MIB=1..64`（超界或格式无效则回到自动上限）；`XRAY_WINOPT_BUDGET_MIB=32..2048`（无效则回到默认预算）。预算是可配置防护参数，不是 Linux `tcp_mem` 的逐行实现。
- 并发限额仍为最多 256 个观察协程（每 200ms 轮询、直至 Socket 关闭/读取失败）；不限制 TCP 本身的连接数量，超过监测限额的连接保持系统默认行为。
- **申请/回收：** 任何 `SetReadBuffer` 前先预留预算，调用失败时回退预算；Socket 关闭或观察失败时释放预算。为了让预连接设置也遵守预算，`net.Dialer.Control` 的多个候选 fd 各自预留，拨号返回后释放未成功的候选，成功的 Socket 继续持有额度直到关闭。
- 限制说明：WinSock 的真实内存分配和通告接收窗口不必等于 SO_RCVBUF 请求数，故预算是保守资源管理指标，无法替代 Windows TCP 内核的真实接收队列内存记账。

## 5. Linux 启发的路径指标缓存与并发拨号

**v3.4 默认：历史 RTT 与每连接接收缓冲区独立。** Linux `net/ipv4/tcp_metrics.c` 的 `tcp_metrics` 保存的是部分目标路径指标（例如 RTT、RTT 波动、重排序和发送侧拥塞指标），而**不直接复用上一条 TCP 连接最终自动增长的 `SO_RCVBUF`**。本项目将这一设计原则实现为**简化的 RTT 路径缓存**，不声称完全实现 Linux 的 `tcp_metrics`。

- `portable/history.go`：以拨号目标域名/IP:端口及连接实际对端 IP:端口为键，记录当前连接测得的 RTT（`SIO_TCP_INFO.RttUs`），使用整数 EWMA（新数据约 1/8 权重）平滑。10 分钟后过期，最多 256 条，过满时淘汰最早过期的条目；仅保留内存中，重启即清空；没有持久化 IP/域名历史文件。不同目标互不影响。
- 新 TCP 连接**正常完成 Windows 握手后**，控制器才读取目标历史 RTT；它只在**本连接的 RTT 暂时为 0** 时作为接收量统计周期长度的提示，使有效样本不必从头等待。**不允许历史 RTT 单独触发 `SetReadBuffer`**，必须先取得当前连接真实有效的 `RttUs`，再经相同的短流筛选、需求公式、冷却期、低内存保护及预算执行扩容。
- 默认的 `winOptBeforeConnect` **不再调用 `SO_RCVBUF`**。这消除了本补丁由于历史容量继承导致“缓存命中时 SYN 前设置不同缓冲区”的直接路径；**并不保证 SYN 包逐字节相同**，用户手动开启的 TCP 时间戳选项、系统状态和链路条件仍可能影响握手。
- 保留旧版容量缓存作为 **显式 A/B 性能实验**：只有启动进程时设置 `XRAY_WINOPT_PRECONNECT_CACHE=on`（亦支持 `1/true/enabled`），才把成功设置的 `SO_RCVBUF` 容量记入旧表，并在同目标后续连接的 `net.Dialer.Control` 阶段、TCP `connect` 前恢复。默认关闭，可能导致不同的 SYN Window/Window Scale，亦可能提高短连接性能。不要把这个实验模式与 Linux `tcp_metrics` 等同。
- 旧缓存仅在实验模式使用，仍有效期 10 分钟、最多 256 条、设置时受进程预算/低内存约束。关闭实验后重启，所有内存缓存立即清空。原有 `winOptConnectState` 保留，避免实验模式下并发 IPv4/IPv6 候选 Socket 遗留预算。默认没有预连接容量请求或相关预算预留。
- 原始 `customSockopt` 显式配置 Windows `SO_RCVBUF` 时，仍以用户指定值优先；原版 UDP/SOCKS 入站/Vision Direct Copy 不变。**本缓存是客户端本地行为，不操作 VPS 或 Windows 全局 TCP 设置。**

参考 Linux 实现：https://github.com/torvalds/linux/blob/master/net/ipv4/tcp_metrics.c

## 6. 行为与配置

| 环境变量 | 默认 | 作用 |
| --- | --- | --- |
| `XRAY_WINOPT_MODE` | `portable`（空值，仅内部兼容标识） | `off` 关闭全部 Windows TCP 自适应修改；关闭后重启进程，既有 Socket 的设定不会自动撤回 |
| `XRAY_WINOPT_LOG` | 空 | 设为 `1` 输出 `WINOPT expanded/rtt_hint/preconnect/failed/pressure/tcp_timestamps` 等诊断日志 |
| `XRAY_WINOPT_PRECONNECT_CACHE` | 空／关闭 | 默认只缓存历史 RTT、不在握手前继承上次缓冲区；仅设 `on` 时开启旧容量缓存与预握手 `SO_RCVBUF`，用于性能对照，可能改变 TCP SYN 的窗口特征 |
| `XRAY_WINOPT_TCP_TIMESTAMPS` | 空（不改变 Windows 默认） | 可选 `on`/`off`，仅对该 Xray 默认拨号器创建的 TCP 连接、握手前设置 `TCP_TIMESTAMPS`；性能收益待验证 |
| `XRAY_WINOPT_MAX_MIB` | **自动随物理内存** | 可指定 1..64 MiB 的单连接上限 |
| `XRAY_WINOPT_BUDGET_MIB` | **自动随物理内存** | 可指定 32..2048 MiB 的进程请求预算 |

使用方式是 `xray-winopt.exe run -config config.json`，不安装驱动，不修改 Windows 全局 TCP 设置，不要求管理员权限。**免管理员并不意味着可以在所有 Windows 沙箱或安全软件环境中正常运行 SIO_TCP_INFO**；运行验证仍然必需。

## 7. GitHub Actions 与发布流程

默认工作流 `upstream-release.yml` 每 6 小时检查 `XTLS/Xray-core` 最新已发布 Release（包括预发布）。留空 `upstream_tag` 的手动运行也会选择最新版本；特定标签可用于复现。流程：获取上游标签 → 拉取源码 → Go/Python 测试 → 严格 `patch/apply_portable.py` → Windows amd64 构建 → ZIP Artifact；发布预发布版本需仓库变量 `ENABLE_WINOPT_PRERELEASE=true`，**目前不要开启**。原 WinDivert 工作流是独立手动实验，不包含在默认 EXE 中。

上游源码改变导致锚点不匹配，必须明确失败并维护补丁。工作流通过不等于代理功能或性能已验证。

## 8. 当前验证状态

- 用户报告：Windows 自适应版下载约 50 Mbps、手动固定 SO_RCVBUF=2 MiB 约 80 Mbps、Android 约 100 Mbps。不同客户端或不同时间的速率不足以单独证明 TCP 接收窗口是唯一瓶颈；当前没有同一轮逐连接调节日志、也不能据此推断 TCP 时间戳实验开关的收益。
- v3.4 当前离线验证范围：通用控制器单测（含一个活跃 RTT 快速介入、400ms 冷却、需求驱动 4 倍单次上限、满通告窗口增长、容量扩展与内存上限、低流量过滤）、`go test ./portable ./recvwindow`、`go test -race ./portable ./recvwindow`、Windows amd64 `go build ./portable`、22 项 Python 补丁及仓库测试；真实 Windows 行为仍需验证。
- 尚未验证：v3.2 通用增长策略在真实 Xray 中是否提速、最新真实 Xray 完整源码编译、新版时间戳设置在 Windows 11 的有效性；`GlobalMemoryStatusEx` / `SIO_TCP_INFO` 的完整边界行为、IPv4/IPv6 Happy Eyeballs、短连接／断线后的预算释放、VLESS+REALITY+Vision 握手及实测速度。
- 风险：Linux 原生语义不可完全移植、`BytesIn` 作为应用读取量替身可能偏差较大、较长连接的持续轮询有额外开销、Windows 设置 SO_RCVBUF 后可能出现无法再缩小的限制、内存预算并非实际内存使用量、不同网络拥堵下策略可能误判。
- 验收建议：在相同网络环境比较原版默认、原版固定 1MiB、本版自适应优化，观察 CPU/内存/并发以及测速下载速度；不应宣称已超过 Linux 或已修复 Windows 内核。

## 9. 维护规则与逐次变更记录

### 2026-10-30 — README 明确个人实测范围、TCP 流量行为风险与图形化客户端替换方式

- **问题／目标：** 防止将作者个人机器和特定拥堵网络观察到的 Windows 接收窗口自动调谐差异泛化为 Windows 普遍缺陷；公开说明潜在被动流量行为特征；提供除命令行以外的 GUI 客户端使用途径。
- **涉及文件：** `README.md`、`README.en.md`、本 `docs/TECHNICAL.md` 的文档维护记录。
- **用户可见变化：** README 明确只有作者个人特定 VPS/网络测试中观察到下载速度约 **40 Mbps → 70 Mbps**，不作为普遍结果或性能承诺；提醒 `SO_RCVBUF` 调整可能影响 TCP 窗口通告、ACK/包时序等可观察特征，是否形成独有代理指纹尚未验证；新增可在 v2rayN 等图形化客户端中备份原 `xray.exe`、把 `xray-winopt.exe` 重命名为 `xray.exe` 并替换内核的步骤，说明 GUI 的核心更新可能覆盖修改版。
- **实现变化：** 无。**没有修改 TCP 增长算法、缓存、拨号器、构建产物名称、自动工作流或运行依赖**。
- **验证：** 检查文档所述可执行文件名、现有构建清单、相对链接和所述 v2rayN 典型内核目录；未增加 Windows 实机运行或可泛化的性能证据。
- **风险／回退：** GUI 客户端不同版本的内核路径、核心选择和兼容性可能变化；替换前务必退出、备份并确认当前确实使用 Xray；问题发生时恢复原始 `xray.exe`。撤销该文档修改不会改变二进制行为。

每次提交修改任何实现／测试／工作流／安装方式时，**同一提交范围内更新本文的相关章节和本节**。README 只描述用途、安装及重要风险。

### 2026-10-30 — v3.4：缓存目标 RTT，不再默认继承上一连接缓冲区

- **动机与设计依据：** Linux `tcp_metrics` 跨连接记忆路径 RTT、部分拥塞/乱序指标，但接收缓冲区的实际增长独立发生在每条连接内。旧 Xray-WinOptimize 在热连接 `connect` 前复用上次 `SO_RCVBUF` 可能带来新的初始 SYN Window/Window Scale 差异；本版默认取消这种容量继承。
- **变更文件：** 新增 `portable/history.go`、`portable/history_test.go`；修改 `portable/socket_windows.go`（历史 RTT 采样、握手后参考、旧缓存显式开关）、`portable/controller.go`/`controller_test.go`（RTT 缺失时的观察提示及安全约束）、`patch/apply_portable.py`（新模块随上游补丁复制）、`tests/test_portable_patcher.py`（自动补丁验证）；同步更新 `README.md`、`README.en.md`、`docs/INSTALL-WINDOWS.md`、`docs/FINGERPRINT-AUDIT.md` 与本文。
- **新默认逻辑：** 记录目标和实际对端的短期 RTT EWMA（10 分钟、最多 256 条），仅在新连接**完成握手之后**提供给控制器；当 `SIO_TCP_INFO.RttUs=0` 时允许历史 RTT 用于确定测量周期，但**不能据此调用 `SO_RCVBUF`**，直到当前连接测得有效 RTT 并通过自身流量判断；默认握手前无历史容量配置。
- **旧行为开关：** `XRAY_WINOPT_PRECONNECT_CACHE=on` 临时允许原有容量缓存及握手前设置，仅供性能 A/B；默认关闭。切换时重启 Xray。显式用户 `SO_RCVBUF` 仍优先。
- **不变：** v3.3 Fast Ramp 公式、单步最多按需四倍、`max(RTT,400ms)` 冷却、首次活动量/RTT 筛选、有限探测、低内存/进程预算与 TCP 时间戳默认不覆盖。仍使用 Windows TCP，没有 Linux TCP 栈移植，也不会使服务器端改变 BBR。
- **风险/性能：** 短时间多连接下载可能比旧版慢；RTT 历史可能失效，故它不能直接批准扩容。SYN 指纹差异的减少属于静态代码推导，不是 Windows PCAP 实测，**不能保证防止被动检测**。实验开关 `on` 可回退旧缓存行为，完全回退可使用原版 Xray 或 `XRAY_WINOPT_MODE=off`。
- **验证范围：** Go 历史缓存/首次 RTT 测量回归、race 检查、Windows amd64 交叉编译、Python 补丁检查；完整上游 Xray 构建和 Windows 实机抓包/速度需要 GitHub Actions 与本机验证。


### 2026-10-30 — 项目术语校正：明确为“借鉴 Linux 设计”

- **原因：** 当前 Windows 默认实现仍使用 Winsock 与 `SO_RCVBUF`；RTT 代理量、增长系数、阈值、资源预算及调用方式均是本项目独立实现，不具备 Linux 内核相同的调谐语义。将项目宣传为“支持 Linux 风格接收窗口自动调谐”容易造成对兼容性、移植程度和性能效果的误解。
- **修改文件：** `README.md`、`README.en.md`、`docs/PUBLISH-GITHUB.md`、`docs/INSTALL-WINDOWS.md`、`docs/TECHNICAL.md`。
- **行为变化：** 无。这是纯文档和对外描述修订，不改变连接、扩容、测试与构建工作流。
- **验证：** 检查当前公开介绍和安装文档没有“支持 Linux 风格”承诺；继续保留原实现边界及历史版本记录。
- **回退：** 不涉及运行时回退；如修改此术语规范，应同步审查所有公开文档。

### 2026-10-30 — v3.3：Fast Ramp——放宽首次介入、冷却及单步增长节奏

- **目的：** 反馈 v3.2 速度仍未达到用户此前手动指定接收缓冲区时的水平。在不引入固定 1/2 MiB 分支的前提下，减少短时大流量连接早期被 Windows 小窗口限制的时间。
- **涉及文件：** `portable/controller.go`（修改首次介入、调节冷却和单次增长上限）、`portable/controller_test.go`（更新既有语义并添加快速介入、400ms 冷却、按需求增长、内存上限等测试）、`docs/TECHNICAL.md`（此技术说明及记录）、`docs/INSTALL-WINDOWS.md`（用户行为说明）。拨号器、WinDivert 适配器、配置、工作流没有变化。
- **增长逻辑：** 首次介入只需完成**一个**满足活动量要求的 RTT 测量周期，仍需 RTT≥40ms、累计收到至少 128KiB 且本周期至少 32KiB；之后增长冷却从 `max(2×RTT,1秒)` 降至 `max(RTT,400ms)`；单步上限从 `min(target,2×baseline,cap)` 改为 `min(target,4×baseline,cap)`。这里的四倍仅允许**估计需求确实达到**时请求较大值，不是逢连接就四倍扩容。仍按 64KiB 对齐。
- **保持不变：** 200ms 采样、`max(RTT,100ms)` 计量周期、RTT 字节/趋势/乱序提示计算公式、通用有限的平稳期探索、无 70% 通告窗口门槛、无固定大小探测、每连接上限、进程预算、低内存保护、256 监测协程限额、10 分钟预握手目标缓存及用户显式 `SO_RCVBUF` 优先。
- **风险：** 大量高吞吐连接可能更早锁定较大的 `SO_RCVBUF`，使 Windows 原生自动调谐更早退出，增加请求预算和内存压力；Winsock 实际分配不等于请求数；更快增长不保证更高吞吐或更优并发公平性。默认全局应用于 Xray 默认 TCP 出站，应先以可回退的实验二进制验证。
- **验证：** 当前在独立仓库运行 Go 单测、race 检查、Python 单测及 Windows x64 交叉编译均通过；完整上游 Xray 最新版本构建与 Windows/REALITY/性能对照尚未经验证。
- **回退：** 可执行文件回退上一版 v3.2；`XRAY_WINOPT_MODE=off` 关闭**全部**优化（不是仅关闭 Fast Ramp）。本版没有另设专门的激进模式开关。

### 2026-10-30 — v3.2：取消 70% 窗口压力门槛，统一需求驱动增长

- **背景：** 不同网络条件下，`RcvWnd < SO_RCVBUF × 70%` 缺乏明确的容量/占用含义；固定 1 MiB 初次探测和 2 MiB Aggressive 探测又引入特定网络经验参数。用户要求保留现有增长公式等其他主要机制，只去除特殊容量判定，形成适用于不同 BDP 的通用增长。
- **修改文件：** `portable/controller.go` 将首次和后续干预合并为一个按 RTT 收量、增长趋势、一次平稳期探索计算需求的分支，不再根据 `RcvWnd` 百分比放弃增长；`portable/controller_test.go` 替换旧特例测试，覆盖 512KiB 初始增长、满通告窗口增长、跨 2MiB 增长、低流量与大缓冲区短流保护、冷却/上限及旧环境变量无效；`docs/INSTALL-WINDOWS.md` 移除 Aggressive 开关说明；本技术文档同步修改。
- **保留行为：** 200ms 采样、`max(RTT,100ms)` 统计周期、首次 40ms/128KiB/2 活跃 RTT 周期筛选、32KiB 活动量、原趋势公式及 1.25 系数、单次最大翻倍、通用探索前至少接收一个缓冲区的数据、`max(2×RTT,1s)` 冷却、单连接内存上限、进程预算和低内存保护、10分钟目标缓存、Windows Winsock 出站不变。
- **删除行为：** 初次固定 1MiB 探测、`RcvBuf <768KiB` 独立门槛、`RcvWnd < applied×70%` 放行条件、按 1MiB→2MiB 硬编码的 `aggressive` 模式。旧变量 `XRAY_WINOPT_GROWTH` 被忽略。
- **性能与风险：** 通用探索次数仍然有限，但在窗口较大时可能更积极地申请缓冲区；真实吞吐提升取决于链路、单连接有效 BDP、Windows TCP 行为和应用流量。没有 Windows 实机 A/B 测试之前不能宣称优于 v3.1。
- **验证：** 控制器 Go 单测、竞态检查、Python 补丁测试及 Windows 交叉编译；完整 Xray 最新发行版编译/实机联网仍需在 Actions 和 Windows 验证。
- **回退：** `XRAY_WINOPT_MODE=off` 关闭整个策略，或切换旧版可执行文件；本版不再提供旧版算法模式开关。

### 2026-10-30 — 历史版本 v3.1：可选积极容量探测（已在 v3.2 移除）

- **背景与原因：** 实机观察自适应版约 50 Mbps，固定 2 MiB 约 80 Mbps，Android 约 100 Mbps。审查 `winOptController.Observe` 发现 `RcvWnd >= 70% × applied` 会直接禁止后续增长；这对“接收窗口通告值保持很大，但 BDP 仍超过 1 MiB”的连接可能形成错误的增长抑制。
- **修改文件：** `portable/controller.go` 增加环境变量 `XRAY_WINOPT_GROWTH=aggressive` 的读入以及有界 1→2 MiB 试探路径；`portable/controller_test.go` 新增积极模式／默认模式／低流量／用户上限的回归测试；`docs/TECHNICAL.md`、`docs/INSTALL-WINDOWS.md` 同步说明与风险。
- **运行行为：** 缺省完全保持 v3 策略；只有选择积极模式、已经进入有流量的 1 MiB Socket、RTT ≥100ms、连续至少 3 个活跃 RTT 测量周期且本周期收到的数据达到原设置的 75% 时，才绕过单一 RcvWnd 压力门槛，试探到 2 MiB。其余按原 RTT 冷却、预算/低内存与上限限制。成功的请求继续通过已有缓存提供给后续同目标的连接，在 connect 之前设置。
- **验证：** 对策略源码做 Go 单元测试与 Windows amd64 交叉编译；仅为模型行为验证，不代表已在真实 Xray 完整源码或真实 Windows 网络中运行。
- **风险：** 某些连接在 1 MiB 已经足够时也可能多申请 1 MiB，增加内存使用；缓冲区增长不保证超过手动固定 2 MiB；Windows 的 ACK/SACK/发送端 BBR 行为未改变。
- **回退：** 删除 `XRAY_WINOPT_GROWTH` 或设为其他值、重启 Xray，新连接恢复旧增长行为；完全关闭仍可用 `XRAY_WINOPT_MODE=off`。
- **下一步：** 在同一节点、相近拥堵条件下对照保守模式与积极模式，并检查日志是否出现 2 MiB 设置以及预连接缓存；如果已经设置 2 MiB 仍维持 50 Mbps，重新检查发送端、连接并发与其他 Windows TCP 限制。

### 2026-10-29 — Linux 风格 RTT 调谐与内存预算 v2

- **动机：** 用户希望尽量贴近 Linux 接收缓冲区调谐，不再默认锁定 2 MiB，并保留 Portable 单 EXE、免管理员使用体验。
- **改动：** `controller.go` 从固定间隔 BDP 判断改为 RTT 测量周期和趋势增长；新增 `quota.go` 和测试；`socket_windows.go` 增加 GlobalMemoryStatusEx 的单流动态上限、请求预算、低内存门限、连接生命周期账本及并发双栈候选管理；`socket_other.go` 更新非 Windows 空函数；`apply_portable.py` 增加状态传递和新模块复制；更新安装与技术文档。
- **与 Linux 的差异：** 不能读取真实 `copied_seq/rcv_rtt_est/ooo_queue/sk_rmem_alloc`；加入 Windows 适配系数、有界乱序提示、窗口压力门限及反馈闭环探测；保留 Winsock。
- **测试：** 本地 Go/Python 测试、Windows Go 模块交叉编译；完整 Xray 编译和 Windows 性能仍待验证。
- **风险及回退：** 可能误增大缓冲区、影响内存/性能；`XRAY_WINOPT_MODE=off` 重启恢复新连接，或切回原版 Xray。进程退出清理所有内存缓存／账本。

### 2026-10-30 — Windows TCP Optimization v3（本次）

- **背景：** 用户在 Windows 实机报告 Linux 风格接收缓冲区调节版速度明显提升但仍有差距，要求避免在公开构建名称与说明强调“Portable”，并寻找接收窗口以外的可行性能实验项。
- **新增实验参数：** `XRAY_WINOPT_TCP_TIMESTAMPS`，缺省不覆盖 Windows 的 TCP 时间戳设置；`on` / `off` 在连接的 `Dialer.Control` 阶段，即 TCP 握手前使用 `setsockopt(IPPROTO_TCP, TCP_TIMESTAMPS=10, DWORD)` 设置。失败仅记录（需 `XRAY_WINOPT_LOG=1`），不会阻断拨号。该选项是微软公开支持的连接级 TCP 选项，可能影响 RTT 测量；**并无提速保证**。它对 ACK/SACK 调度、VPS 端的 BBR 控制器无控制权。使用 `XRAY_WINOPT_MODE=off` 时不设置时间戳。
- **代码路径：** `portable/controller.go` 新增无副作用的参数解析及测试；`portable/socket_windows.go` 新增 `winOptApplyTCPOpts`，在 Dialer.Control 前置选项；`portable/socket_other.go` 新增非 Windows 空函数；`patch/apply_portable.py` 在控制回调调用该函数；`tests/test_portable_patcher.py` 校验注入锚点。
- **用户可见名称：** 默认 GitHub Actions 工作流 `Xray-WinOptimize (latest upstream)`；构建文件 `xray-winopt.exe`；Artifact `xray-winopt-v<upstream>`；Release tag `v<upstream>-winopt`；自动预发布变量 `ENABLE_WINOPT_PRERELEASE`。源码目录 `portable/` 与 `apply_portable.py` 仍是内部实现标识，不代表单独发布模式。
- **验证：** Go 控制器单元测试、Python 补丁测试、Windows amd64 编译检查；尚未在 Windows 实机确认时间戳协商成功，也未验证其下载速度影响。某些中间盒可能与 TCP 时间戳兼容性不佳，因此默认禁用该实验开关。
- **回退：** 删除 `XRAY_WINOPT_TCP_TIMESTAMPS` 环境变量、重启 Xray；已有连接不受追溯影响；如需要完全关闭优化，则 `XRAY_WINOPT_MODE=off` 后重启。Release 命名改变不会自动删除旧版产物。

### 2026-10-29 — Portable v1（历史）

- 初始版本以高 RTT / 活跃数据量触发 1MiB 扩容，再根据短期峰值 BDP 与窗口压力增长到默认上限 2MiB；10 分钟缓存、15 秒监测、最多 256 条观察连接。此逻辑已由 v2 替代。

---

## 附录 A：旧版 WinDivert 独立 TCP 栈研究记录（历史实现，不属于默认 Portable）

以下为先前实验架构及实现分析，用于交接、保留历史；其中“当前默认”“启动方式”“自动构建”等描述已由上面的第 0–9 章覆盖。旧版原型没有完成 Windows 实机联网验证。

# Xray-WinOptimize：技术设计、变更记录与交接手册

> **文档属性：项目的唯一技术事实记录（living document）**  
> **基线：Xray-core v26.10.21 的实验补丁原型**  
> **最近文档修订：2026-10-29**  
> **验证状态：控制器单元测试与独立 Windows 适配层交叉编译已报告通过；完整 Xray 编译、WinDivert 实机收发及 REALITY 端到端运行尚未确认。**  
> **规则：每次改变实现、源码补丁、依赖／构建流程、运行方式、支持范围或安全边界，都必须在同一 PR／提交范围内更新本文对应章节和第 13 节变更记录。**

本文件记录**当前仓库实际已有的代码**与计划中的工作。凡未经过实际运行验证的功能均标为“设计／待验证”，不会因为具备代码或编译通过就视为可用。

## 目录

1. [背景、目标和范围](#1-背景目标和范围)
2. [架构和数据路径](#2-架构和数据路径)
3. [仓库与被修改的 Xray 文件](#3-仓库与被修改的-xray-文件)
4. [Windows WinDivert 报文适配层](#4-windows-windivert-报文适配层)
5. [Xray 出站拨号器集成](#5-xray-出站拨号器集成)
6. [轻量级 TCP 栈源码补丁](#6-轻量级-tcp-栈源码补丁)
7. [Linux 思路的接收窗口调谐](#7-linux-思路的接收窗口调谐)
8. [配置与运行边界](#8-配置与运行边界)
9. [构建、测试及自动上游版本发布](#9-构建测试及自动上游版本发布)
10. [验证现状、已知风险与未实现功能](#10-验证现状已知风险与未实现功能)
11. [调试、故障定位与回退](#11-调试故障定位与回退)
12. [后续开发路线和交接清单](#12-后续开发路线和交接清单)
13. [维护规范和逐次变更记录](#13-维护规范和逐次变更记录)

## 1. 背景、目标和范围

### 1.1 现象及已有研究依据

研究场景：Windows 原生 Xray-core v26.10.21，通过 SOCKS5 → VLESS + REALITY + XTLS Vision → 远端服务器的 TCP 路径；RTT 约 140–180 ms。在部分拥塞条件下，原生 Windows TCP 的下载速度显著低于 Linux / Android，显式设置 `SO_RCVBUF` 到 1–2 MiB 可以明显提高 Windows 端吞吐，但并不总能达到 Linux / Android 水平。

之前的 Windows `SIO_TCP_INFO`、EStats 和 Xray 读间隔实验提示：Windows 端实际通告接收窗口通常较小；扩大 `SO_RCVBUF` 有效；并未找到长时间由 Xray 用户态延迟调用接收操作造成的阻塞。以上属于**用户环境观察**，不是本仓库新 TCP 后端已取得的性能结果。

### 1.2 目标

- 不改变既有 SOCKS5/HTTP 入站、VLESS/REALITY/Vision 处理链或服务端配置。
- 在 Windows 平台对**走 Xray 默认系统拨号器的 TCP 出站**提供可选替代 TCP/IP 实现。
- 独立实现真实 IP 报文的收发／回包截获，不要求用户切换到 TUN 入站。
- 在 Xray 自带轻量级 TCP 栈内实验性实现参考 Linux 接收窗口调整思想的策略，允许比固定 256 KiB / 1 MiB 更灵活的增长。
- 保留明确开关与官方 Xray 二进制回退路径。
- 以独立 GitHub 仓库跟踪 Xray 发布：不兼容时停止构建，而不是悄悄发布未验证二进制。

### 1.3 “所有 TCP 连接”的严格定义

此处仅指**Xray 通过默认出站拨号器发起、且符合实验条件的 TCP 连接**，不是所有 Windows 进程的 TCP，也不是所有 Xray 流量协议。具体限制见 §5、§8。

**当前已知例外：** UDP 始终使用原来的路径；环回／部分非全局单播目的地回退原生拨号器；显式绑定源地址及部分 Socket 选项被拒绝；部分非默认拨号路径可能不经该替换接口；不代表任何 TUN/WireGuard/MASQUE/特殊网络均已完成兼容测试。

## 2. 架构和数据路径

```text
浏览器 / 应用
    │ Windows 系统代理（SOCKS5 / HTTP）
    ▼
Xray 现有入站 → VLESS / REALITY / Vision（无修改）
    │
    ▼
transport/internet 的默认 TCP 拨号路径
    │
    ├─ 实验关闭 / 非 TCP / 指定回退路径 ──→ 原生 Windows Winsock
    │
    └─ 实验开启、符合条件的 TCP
           ▼
       proxy/tun/dialer_experimental_windows.go
           │ Stack.DialTCP(ctx, remoteAddrPort) → net.Conn
           ▼
       proxy/tun 内置轻量级 TCP/IP Stack
           │ 窗口/ACK/SACK/重传/乱序数据结构
           │ Linux-inspired recvwindow 调节器
           ▼
       proxy/tun/winpacket_adapter_windows.go
           │ WinDivertSend 注入出站 IP 包
           │ WinDivertRecv 截获精确反向五元组
           ▼
       Windows 网络路径 → VPS TCP 服务端
```

**关键边界：** 用户态 TCP 不可以只调用 `DialTCP` 就通过物理网卡发送；必须有报文 I/O 接口与回包获取机制。在第一条 SYN 注入前，必须先为这条连接建立捕获路径，否则来自服务器的 SYN-ACK 可能落入原生 Windows TCP 处理路径。当前实现尝试按五元组完成此顺序，但尚未实机验证。

### 2.1 主要模块职责

| 模块 | 职责 | 不负责的事情 |
|---|---|---|
| Windows PacketAdapter | 为各 flow 保留源端口、创建 WinDivert 过滤器、IP 包出站注入和入站收取 | TCP 重传、乱序、ACK 语义 |
| Xray `Stack` / `tcpConn` | TCP 连接状态、SYN 握手、窗口通告、数据流 `net.Conn` | 物理网卡驱动、Windows 全局路由修改 |
| 自适应窗口 Controller | 根据读取量、RTT 和乱序范围建议增大接收窗口 | Linux 原生内存记账、精确复制 Linux TCP 行为 |
| 实验出站 Dialer | 选择目标/源地址，按源 IP 复用 Stack，返回连接 | 改写 SOCKS5/REALITY 业务逻辑 |
| GitHub Actions | 拉取上游标签，严格补丁、测试、构建、选择性发布 | 自动验证 Windows 真实网络稳定性 |

## 3. 仓库与被修改的 Xray 文件

### 3.1 当前仓库文件（真实源码位置）

| 文件 | 技术用途 |
|---|---|
| `adapter/packet_windows.go` | Windows 专用独立 WinDivert 适配代码；由补丁器嵌入 `proxy/tun` 包 |
| `adapter/packet_other.go` | 非 Windows 占位实现，明确返回 Windows-only 错误 |
| `recvwindow/window.go` | 与 Xray 无关的纯 Go 调节器，实现 `Controller.Observe` |
| `recvwindow/window_test.go` | 观察间隔、空闲、计数复位、乱序增长、最大窗口等单测 |
| `patch/apply.py` | 通过**严格字符串锚点**将改动应用到新鲜 Xray 源码树，并备份原文件 |
| `patch/dialer_windows.go` | 编译为 `proxy/tun/dialer_experimental_windows.go` 的 Windows 拨号器 |
| `.github/workflows/upstream-release.yml` | 定时／手动跟踪上游发布并 Windows amd64 构建 |
| `scripts/select_latest_release.py` | 解析 GitHub Releases 原始 JSON，按发布时间选择最新发布版本 |
| `tests/test_release_selector.py` | 对自动选版进行 API 响应模拟和命令行回归测试 |
| `.github/workflows/docs-check.yml` | PR / push 文档同步检查；不等同技术审查 |
| `tests/test_patcher.py` | 模拟上游源码，验证锚点、生成文件可 gofmt、重复打补丁拒绝 |
| `tests/check_technical_doc.py` | 检查改动的文件集是否同时包含 `docs/TECHNICAL.md` |
| `tests/test_doc_policy.py` | 文档同步检查的单元测试 |
| `docs/INSTALL-WINDOWS.md` | Windows 详细构建、测试和回退步骤 |
| `README.md` / `README.en.md` | 中文默认／英文访客项目简介与安装流程；不维护算法细节 |
| `docs/TECHNICAL.md` | 本文件；设计、限制、测试状态、交接和完整变更记录 |

### 3.2 补丁器实际改动上游 Xray 的位置

补丁器检查 Xray 源码目录中下列文件的存在，随后精确替换锚点；如果锚点不存在或出现多次，则抛错并停止，不做模糊匹配。

| 上游文件 | 具体改动 |
|---|---|
| `proxy/tun/stack.go` | `StackOptions` 增加 `AdaptiveReceive bool`；`stack` 增加 `adaptive` 字段；初始化时读取开关并对自适应连接设初始 256 KiB 窗口 |
| `proxy/tun/tcp_lightweight.go` | `tcpConn` 增加控制器与已交付字节累计；连接初始化加入调节器；自适应 TCP 连接的接收窗口缩放改为 8；应用读走数据后触发窗口观察和必要 ACK；发送失败向上报告；自适应模式扩大乱序片段数硬上限 |
| `main/distro/all/all.go` | 在未注册时加入 `_ "github.com/xtls/xray-core/proxy/tun"`，以启用 `init()` 中的 Windows 实验拨号器注册 |
| `transport/internet/system_dialer.go` | 只验证此文件存在；**当前补丁器并不改写它的内容**，复用原有 `UseAlternativeSystemDialer` 接口 |
| `proxy/tun/winpacket_adapter_windows.go` | 由 `adapter/packet_windows.go` 复制、替换包名为 `tun`、工厂名为 `NewWinDivertPacketAdapter` 后新增 |
| `proxy/tun/dialer_experimental_windows.go` | 直接复制 `patch/dialer_windows.go` 后新增 |
| `proxy/tun/recvwindow_adaptive.go` | 由 `recvwindow/window.go` 改名/替换包名，加上 `tcpConn.adjustAdaptiveReceive` 方法后新增 |

修改的既有文件旁产生 `.tcpbackend.orig` 备份。完整恢复最稳妥的方式是重新检出干净的上游标签并重新构建；**不要直接在已多次打补丁的源码上重复执行**。

**注意：** `apply.py` 对多个上游文件执行严格字面锚点替换；它不是稳定的通用 AST 补丁格式。后续 Xray 版本变动时必须重新检查每一处锚点、修改后的代码语义与构建结果。

## 4. Windows WinDivert 报文适配层

源文件：`adapter/packet_windows.go`；生成到 Xray：`proxy/tun/winpacket_adapter_windows.go`。

### 4.1 创建与端口预留

- Windows 实现通过 `syscall.NewLazyDLL("WinDivert.dll")` 加载 DLL 的 `WinDivertOpen` / `WinDivertRecv` / `WinDivertSend` / `WinDivertClose`；调用驱动需要管理员权限和匹配的签名驱动。
- `Start()` 加载 DLL 并启动周期清理 goroutine，但**还没有立即打开特定 flow 的 WinDivert 句柄**。
- TCP 栈第一次发送对应 flow 的报文时，`WritePacket` 调用 `decodeTCP` 提取本地与远端 IP:port；`flowFor` 先调用 `reservePort` 创建原生、已 `bind` 但不 `listen` 的 TCP Socket 以保留源地址/端口；再建立反向五元组 WinDivert 入站过滤句柄，随后才允许该 TCP 包注入。
- 这个原生 Socket **仅用于端口预留，不承载到 VPS 的 TCP 会话**。该方案在真实 Windows 上阻止端口冲突及意外 RST 的有效性尚待验证。

### 4.2 过滤器、收包与发包

过滤器约束为 `inbound`、IP 版本、`tcp`、远端源地址 / 本地目的地址、远端源端口 / 本地目的端口，与 Xray 出站方向恰好反向；**并不全局截获 Windows TCP**。

- 发包：拼接 Stack 提供的 IP header 与 payload；设置 `WINDIVERT_ADDRESS` 的出站方向字段，通过 `WinDivertSend` 注入网络层数据。
- 收包：每个 flow 独立 `WinDivertRecv` goroutine；捕获到的 IP 数据通过共享有界 `received` channel 交给 `Stack.ReadPacket`。
- `decodeTCP` 仅识别**无分片 IPv4**或**无扩展头 IPv6**的 TCP 包。IPv4 分片、IPv6 扩展头、多重封装不受支持，发送端会返回错误。

### 4.3 生命周期与并发参数

| 参数 | 当前代码值 | 说明 |
|---|---:|---|
| `maxFlows` | 128 | **每个 PacketAdapter 实例**最多跟踪 128 条 flow；不是整台机器的上限 |
| `packetCapacity` | 65,536 字节 | 单次 WinDivert 收包缓冲 |
| `received` 队列 | 2,048 个包 | 入站数据队列满时直接丢弃包，不阻塞捕获线程；依赖 TCP 重传恢复 |
| `reap` 扫描周期 | 15 秒 | 清理空闲 flow |
| 空闲关闭阈值 | 5 分钟 | 超过后关闭 WinDivert handle |
| 已出现 FIN/RST 的 flow | 45 秒 | 至少保留一段时间接受后续重传／收尾报文 |

`Close()` 结束适配器，并关闭所有 tracked handles。Flow 的删除与源端口预留 Socket 关闭在 `capture()` 的 defer 内完成；通道和连接状态的竞态边界尚未通过高并发/故障注入测试。

### 4.4 已知不确定性（严禁当作已解决）

- WinDivert 的网络层地址结构布局、出站方向位、发送网卡选择和 TCP/IP checksum 是否正确，在目标 Windows / WinDivert 版本上**未经过报文级实测**。
- 连接到不同源 IP 的多网卡环境、VPN、NAT、休眠恢复、MTU、IPv6 Scope ID、第三方 WFP 过滤器等未充分处理。
- Flow 是按首包建立，不是提前完整预注册；顺序上在 SYN 注入前完成，实际竞态行为需验证。
- 即使是精确反向 flow，驱动级截获仍可能影响其他过滤器；关闭实验版不保证能自动恢复第三方软件异常状态。
- 当前实现为每条 flow 建一个 WinDivert 句柄和 goroutine，高连接数下系统开销与资源清理可能成为瓶颈。

## 5. Xray 出站拨号器集成

源文件：`patch/dialer_windows.go`；生成到 Xray：`proxy/tun/dialer_experimental_windows.go`。仅 Windows 构建参与编译。

1. `init()` 只有在进程启动时 `XRAY_EXPERIMENTAL_TCP_STACK=1` 才调用 `internet.UseAlternativeSystemDialer`；否则不注册替代拨号器。
2. `Dial` 遇到非 TCP 请求，直接委派给 `internet.DefaultSystemDialer`。
3. 使用 `net.SplitHostPort`、`net.DefaultResolver.LookupNetIP` 解析目标；目前选中**第一个有效的全局单播地址**并 `Unmap`。如果没有符合条件的地址，则使用原生拨号器。这里没有完整 Happy Eyeballs / 多地址择优实现。
4. 若有显式源地址绑定，则返回“不支持”；`TcpMptcp`、指定 KeepAlive、非空 `CustomSockopt` 等选项也被拒绝。**这些不是完整 Socket 选项白名单，其他选项是否被忽略仍需审计。**
5. 用临时 `net.DialUDP` 查询 Windows 的本地出站源 IP（UDP socket 不发送应用数据，使用其选路结果）；基于这个本地 IP 复用或创建 `experimentalStack`。
6. 创建 `NewWinDivertPacketAdapter()`，以 `StackOptions{Tun, MTU, Addresses, Remote:true, AdaptiveReceive:true}` 调用 `NewStack`、`Start`；最后通过 `Stack.DialTCP` 获取 `net.Conn` 交还既有 VLESS/REALITY 链。

**MTU：** IPv4 设为 1400，IPv6 设为 1280，是当前实验选择，并非自动发现路径 MTU。真实链路 PMTU 需验证。

**失败策略：** 对明确回退类别使用 Windows 原生拨号器；若已决定实验路径但 WinDivert 打开失败、Socket 选项不支持或 TCP 握手失败，则通常向调用方返回错误，**不会自动静默回退**（避免掩盖实验故障）。

**重要限制：** 现有拨号器注册的是整个 Xray 默认 TCP 出站入口，不提供“只对某个服务器启用”参数；`XRAY_EXPERIMENTAL_TCP_STACK` 为进程级开关，不是动态逐连接开关。此设计符合当前“尽可能覆盖全部 Xray TCP 出站”的目标，但**风险面也更大**，首轮必须小流量测试。

## 6. 轻量级 TCP 栈源码补丁

补丁通过 `patch/apply.py` 进入 Xray 自带 TCP 协议栈，而不是自己新建一套独立的 TCP 状态机。

### 6.1 Stack 配置

- 增加 `StackOptions.AdaptiveReceive`，并保存在 `stack.adaptive`。
- 默认堆栈逻辑仍保留原 `tcpWindow` / `tcpLargeWindow`；仅自适应模式初始设为 `256 << 10`。
- 活跃 outbound connection 初始化 `tcpConn` 时，带入此开关和独立控制器实例。

### 6.2 窗口缩放

- 调整 `tcpConn` 的两处接收窗口缩放赋值；自适应连接使用 `rcvShift=8`，其他连接保持原 `tcpWindowShift`。
- 目的：支持实验控制器最高 **8 MiB** 的通告窗口，超出原较小固定窗口的规模。TCP Window Scale 需在 SYN 阶段协商，连接建立后不得临时任意更改；此处的实际 SYN/SYN-ACK 行为还需抓包验证。

### 6.3 读走数据后的窗口更新

当应用读取完成、`c.rcvLen -= size` 后，调用 `c.adjustAdaptiveReceive(size)`。这段新增代码按应用**已经消费的数据量**累计 `adaptiveDelivered`，读取 `c.srtt`，并计算 `c.rcvHeld` 内最新乱序片段尾部相对 `c.rcvNxt` 的序列号跨度，传给控制器。若窗口建议增长，就改变 `c.window`，并使当前 ACK/窗口更新逻辑考虑本次增长。

> 乱序跨度不是实际占用的全部 Socket 缓冲内存；它只是一个可用启发式输入。当前尚未与 Linux 的 `tcp_rmem`／`sk_rmem_alloc` 做一一对应的内存预算。

### 6.4 其他 TCP 修改

- `tun.WritePacket` 返回错误时，自适应连接调用 `c.abort(err)`；其并发、锁定状态与连接关闭路径必须通过完整源码编译和实测确认。
- 接收乱序片段的上限：原先 `tcpHeld` 约束在**非自适应连接**上继续保留，自适应连接允许最多 `4096` 个片段；这是按数量计的边界，**不是严格的字节内存上限**。
- 所有改动都是实验模式条件分支；不启用时意图保留 Xray 原有行为，但“无回归”尚未经过完整集成测试证明。

## 7. Linux 思路的接收窗口调谐

纯 Go 实现：`recvwindow/window.go` → 生成 `proxy/tun/recvwindow_adaptive.go`。

### 7.1 输入与状态

```go
Observe(now time.Time, rtt time.Duration,
        delivered uint64, reorderSpan int32) (window int32, grew bool)
```

- `delivered`：**累计被应用读取**的字节数，按连接单调增长。
- `rtt`：当前 TCP 连接的平滑 RTT `srtt`；如果调用处不可用，临时使用 `150ms`。
- `reorderSpan`：被缓存乱序数据覆盖的估计序列号跨度，不能视作物理内存占用量。
- 控制器保存 `Window`、上次观察时刻 `lastTime`、上次已交付计数 `lastTotal`、历史每窗口峰值 `priorPeak`。

### 7.2 逐 RTT 判定（以现有代码为准）

```text
初始窗口 Initial = 256 KiB
最大窗口 Maximum = 8 MiB

若 RTT <= 0：不增长
RTT 观测间隔限于 [10ms, 3s]
首次采样或累计计数回退：只重置基线，不增长
距离上次有效采样不足一个 RTT：等待
copied = delivered - lastTotal
copied == 0 或已达 8 MiB：不增长
reorderSpan 限制在 [0, 8 MiB]

如果 copied < 当前窗口 / 3 且 reorderSpan < 当前窗口 / 4：
    记录可能的新历史读取峰值，窗口不增长
否则：
    candidate = 2 * copied + reorderSpan
    若 copied > 历史峰值且历史峰值 > 0：
        candidate += 2 * (copied - 历史峰值)
    candidate <= 当前窗口：不增长
    candidate 至多为当前窗口的 2 倍，也至多为 8 MiB
    向上按 4096 字节对齐（再执行最大值裁剪）
    若 candidate > 当前窗口：更新当前窗口，报告 grew=true
```

**策略仅增长、不主动缩小窗口。** 这是保守设计：缺少完整 ACK 在途窗口和内存记账时收缩有机会违背之前已通告的接收空间。

### 7.3 与 Linux 内核的关系

参考思想来自 Linux 的接收窗口自调谐机制，特别是根据应用读取速率和链路情况选择可通告窗口，考虑乱序队列的影响。但这**不是逐行移植** `tcp_rcv_space_adjust` 或 `tcp_rcvbuf_grow`，并且缺少 Linux 的 socket 内存记账、精确 receive RTT 估算、缓冲预留与压力反馈。

不能把本算法描述为“Linux TCP 栈移植”“Linux 自动调谐完整实现”，更不能据此保证已达到 Linux / Android 的下载速度。相关 Linux 内核参考：https://github.com/torvalds/linux/blob/master/net/ipv4/tcp_input.c

### 7.4 当前已知算法问题与后续建议

- 读取数据少可能是网络发送端受限，也可能是应用无数据可读；当前算法不能可靠区分。
- 扩窗后真实网络是否改善还未实测；在有丢包时过度增长有可能扩大内存占用。
- 接收窗口最大值是逻辑窗口上限；实际队列内存管理仍由 Xray TCP 实现承担，**并未形成统一的完整内存预算**。
- 未来可添加延迟／丢包／接收窗口利用率指标、连接数上限，以及和固定 1 MiB 的公平性能对照；先保证连接正确性再优化速度。

## 8. 配置与运行边界

| 项目 | 当前行为 |
|---|---|
| 启用方式 | 启动进程前设置 `XRAY_EXPERIMENTAL_TCP_STACK=1` |
| 禁用 | 不设置变量或使用原版 Xray，且重启进程 |
| SOCKS5/HTTP 入站 | 不修改 |
| VLESS/REALITY/Vision | 不修改业务逻辑；使用实验拨号器返回的 `net.Conn` |
| Windows TCP 全局设置 | 不修改；不需要 `netsh` 或注册表调参 |
| WinDivert DLL/驱动 | 用户从官方获取并按 WinDivert 许可证使用；**不随本项目打包** |
| 必须管理员运行 | 是，实验驱动测试所需 |
| UDP | 原生路径 |
| 默认 TCP 出站 | 满足第 5 节条件时尝试实验栈 |
| 自定义 TCP Socket 选项 | 部分明令拒绝；其他支持情况仍需审计 |
| 目标 IP | 当前仅选择首个全局单播地址；部分本地目标走原生 |
| 对所有 Windows 应用 | **不支持，也非目标** |

## 9. 构建、测试及自动上游版本发布

### 9.1 本地已有检查

在仓库根目录：

```bash
go test ./recvwindow
go test ./...
GOOS=windows GOARCH=amd64 go build ./adapter
python3 -m unittest discover -s tests -v
```

`tests/test_patcher.py` 在模拟文件树验证补丁锚点及 gofmt，不代表真实上游源码具备同样结构，也不证明完整 Xray 能编译。

### 9.2 构建真正 Xray（需上游源码）

```powershell
git clone --branch v26.10.21 --depth 1 https://github.com/XTLS/Xray-core.git xray-src
python patch/apply.py .\xray-src
cd .\xray-src
gofmt -w .\main\distro\all\all.go .\proxy\tun\stack.go .\proxy\tun\tcp_lightweight.go .\proxy\tun\winpacket_adapter_windows.go .\proxy\tun\dialer_experimental_windows.go .\proxy\tun\recvwindow_adaptive.go
go build -trimpath -buildvcs=false -o xray-winrecv-experimental.exe ./main
```

以**对应上游 Xray `go.mod` 声明的 Go 版本**编译，不要把本仓库 `go.mod` 的 `go 1.23` 误认为上游编译要求。未成功完成真实完整构建前，不能将其标记为通过。

### 9.3 上游 Release 工作流

文件：`.github/workflows/upstream-release.yml`。

1. 每 6 小时定时检查一次（UTC cron `21 */6 * * *`），也支持 `workflow_dispatch` 指定 `upstream_tag`。手动运行 `upstream_tag` 默认**空字符串**，与定时运行一样自动跟踪上游最新发布；填写具体 tag 才固定版本。
2. 当 `upstream_tag` 为空（手动默认或定时运行）时，用 `Invoke-WebRequest` 获取 Xray Releases 原始 JSON（最多前 100 条），写入临时文件，再使用 `scripts/select_latest_release.py` 按 `published_at` 选择最近发布版本。排除 draft，包含 prerelease，只接受 `^v[0-9]+\.[0-9]+\.[0-9]+$` 格式；最新发布不符合此格式时**报错，不回退旧版**。非空 `upstream_tag` 使用指定标签。这样避免 `Invoke-RestMethod` 的 JSON 数组被作为单个管道对象传给 `Where-Object`，导致所有记录筛选失败。API 非数组、无有效发布记录、请求失败均停止。
3. 通过 GitHub REST API `GET /repos/{owner}/{repo}/releases/tags/{tag}` 检查 `$tag-winrecv-experimental` Release。HTTP 200 表示已存在、跳过；HTTP 404 表示尚不存在、继续构建；其他响应按错误处理并显示状态码。不可直接用 `gh release view` 的退出码作不存在判断，因为 Windows Actions 的 `pwsh` 可能会在非零退出码时提前终止。**若只上传 Actions artifact、没有正式 Release，后续定时运行仍可能重复构建该上游版本。**
4. `git clone --depth 1 --branch <tag>` 精确获取上游源码；记录 commit hash；按上游 `go.mod` 配置 Go 工具链。
5. 运行控制器/适配层检查、模拟补丁器测试、应用补丁、`gofmt`、编译 Windows amd64 Xray。
6. 打包 EXE、上游 commit、中英文 README、**完整技术文档 `TECHNICAL.md`**、详细安装文档及 SHA256；上传 GitHub Actions Artifact。
7. **默认不会创建 GitHub Release。** 只有仓库变量 `ENABLE_EXPERIMENTAL_PRERELEASE=true` 时才创建带 `--prerelease` 标记的 GitHub Release。
8. 编译成功只说明代码可编译，不是实机网络通过；默认不应把公开预发布视为稳定发布。

2026-10-29 两次真实 Actions 运行均在版本选择／Release 检查步骤失败：首次是 `gh release view` 非零退出；第二次（Run 110724657760）是 `No published upstream Xray releases found`，原因是 `Invoke-RestMethod` 数组枚举行为。现已切换原始 JSON + Python 选版并补充测试，**修复后的在线 Actions 仍待验证**；全工程构建与联网验证未完成。

### 9.4 文档同步检查

`.github/workflows/docs-check.yml` 在 push 和 pull request 时运行 `tests/check_technical_doc.py`，比较变更文件。

判定范围：`adapter/`、`recvwindow/`、`patch/`、`scripts/`、`tests/`、`go.mod`、`.github/workflows/`、`docs/INSTALL-WINDOWS.md` 等实现、测试和构建／安装相关路径。若其中任一文件改变，变更集必须同时包含 `docs/TECHNICAL.md`。纯 README 或纯技术文档调整不要求再次额外修改其他文件。

该检查只能**证明技术文档文件被同步修改**，不能证明描述与实现一致；代码审查时仍须核对变更记录是否具体准确。初次仓库创建和某些特殊 Git 历史范围需要额外判断，详见脚本注释。

## 10. 验证现状、已知风险与未实现功能

### 10.1 证据等级

| 验证事项 | 状态 | 证据／仍缺少的材料 |
|---|---|---|
| 窗口 Controller 单元测试 | 已有覆盖，需 CI 持续运行 | `recvwindow/window_test.go` |
| 独立 WinDivert 适配层 Windows x64 交叉编译 | 先前已报告通过，需 CI 复验 | `GOOS=windows GOARCH=amd64 go build ./adapter` |
| 模拟上游文件锚点与 gofmt | 已有测试，非真实 Xray 集成 | `tests/test_patcher.py` |
| 完整上游 Xray 编译 | **未验证** | 需要 GitHub Actions / 本地真实源码构建记录 |
| WinDivert 驱动加载／校验和／路由正确性 | **未验证** | 需要 Windows 11 实机包级验证 |
| TCP 三次握手与普通 TCP 下载 | **未验证** | 需要同步客户端／目标端测试 |
| VLESS / REALITY / Vision | **未验证** | 需要真实用户配置握手与数据校验 |
| 高 RTT 下载能否优于固定 SO_RCVBUF | **未验证** | 需要控制条件下 A/B/A 测试 |
| IPv6、VPN、多网卡、休眠恢复 | **未验证** | 覆盖测试与故障恢复测试 |
| GitHub 定时构建与发布 | **两次入口失败，现已更新逻辑，待复验** | `logs_110717158087.zip`：旧 Release 检查失败；`logs_110724657760.zip`：JSON 数组未正确筛选；仍缺在线构建成功日志 |
| 文档同步 CI | 新增，需要 GitHub 首次运行确认 | `tests/check_technical_doc.py` + `tests/test_doc_policy.py` |

### 10.2 高优先级风险

1. **Windows 实际收发正确性：** WinDivert 方向、网卡选路、TCP checksum 及原生 TCP 干扰未经实机核实。
2. **资源用量：** 每连接 driver handle、保留 Socket、goroutine；上限 128 flow/适配器，队列 2048 包会在满时丢包，乱序缓存片段数可达 4096。
3. **TCP 语义：** SYN/SYN-ACK Window Scale、ACK/SACK 时序、RST/FIN、重传与丢包场景尚未整体验证。
4. **网络兼容性：** IPv4 分片、IPv6 扩展头、路由变更、NAT/VPN/第三方过滤驱动、用户指定源地址与 Socket 选项。
5. **源码兼容性：** 严格字面锚点可能因上游字段重命名或布局变化失败；即使匹配，也需要确认语义没有改变。
6. **维护与发布：** 自动编译并不等于安全可用。未完成测试前必须保留 experimental 标记和默认不发布策略。

## 11. 调试、故障定位与回退

请优先遵循 `docs/INSTALL-WINDOWS.md`。此处提供定位原则，而不是让用户一直收集诊断数据。

| 现象 | 先检查 | 当前处理 |
|---|---|---|
| `WinDivert.dll` 加载失败 | DLL/驱动版本与体系架构、文件位置、管理员权限 | 停止实验，使用官方 Xray；不要随意换驱动来源 |
| `WinDivertOpen` 失败 | Filter 表达式、WFP 过滤驱动、管理员权限 | 保存本地错误信息、必要时复现；实验连接会返回错误 |
| `reserve source TCP port` 失败 | 同一源 IP:port 是否被原生 Socket 占用、端口耗尽 | 不应盲目放宽过滤器；先分析绑定与重试策略 |
| TCP 连接超时 | SYN 是否已发、SYN-ACK 是否截获、checksum/出站接口 | 从单流抓包定位，不能仅凭测速结果判断 |
| REALITY 握手失败 | 先确定普通 TCP 往返有效，再检查 net.Conn 语义 | 保留原始配置和官方 Xray 对照 |
| 高并发下降或连接泄漏 | flow 数、WinDivert handle、goroutine、端口预留、队列丢包 | 完成资源上限测试前不发布稳定版 |
| Go 编译失败 | 上游 tag、go.mod、锚点、生成文件、包导入循环 | 不要跳过检查强行发布；修复并更新本文件 |

**回退顺序：** 结束实验 Xray → 清除 `XRAY_EXPERIMENTAL_TCP_STACK` → 启动之前保存的**官方原版 Xray** → 如果网络仍异常，检查驱动／WFP 状态及系统网络配置；不要假定清除环境变量会影响已经在运行的进程。

## 12. 后续开发路线和交接清单

### 12.1 必须按顺序解决的工作

1. **构建可信基线：** 在 Xray-core v26.10.21 真正源代码上运行 `apply.py`、`gofmt` 和完整 Windows x64 `go build`，解决任何字段、接口、依赖、循环引用错误；记录上游完整 SHA。
2. **单流报文验证：** 在隔离的 Windows 设备上验证 WinDivert 驱动、回包捕获、checksum、源端口预留、SYN/SYN-ACK、关闭与 RST 行为。
3. **完整代理链：** 维持 SOCKS/HTTP 入站，验证 VLESS + REALITY + Vision 的握手、稳定传输、流量正确性。
4. **并发与兼容性：** 改善 flow 注册/释放、特殊 Socket 配置、多地址 DNS、IPv6、VPN、不同网卡、丢包与重排情形。
5. **性能回归：** 与官方 Windows 默认、官方 Windows + 1 MiB `SO_RCVBUF`、Linux/Android 对比相同时刻同一节点下载；不要把单条连接的峰值直接等同多连接总速率。
6. **自动发行质量门：** 实机回归通过后，才考虑开启自动实验 Pre-release；发布时包含可复核的构建来源和测试结果。

### 12.2 新维护者交接必读

- 从 `patch/apply.py` 入手：它定义真实被修改的上游文件，以及需要审查的字符串锚点。
- 对照 `patch/dialer_windows.go` 和 `adapter/packet_windows.go`：前者决定哪些流量由实验栈接管，后者决定是否能完成实际收发。
- 从 `recvwindow/window.go` 和 `recvwindow/window_test.go` 理解调谐策略，不要直接改生成文件而遗漏源文件。
- 从 `.github/workflows/upstream-release.yml` 理解每次如何选择上游 tag、用什么 Go 版本构建、何时发布。
- 修改代码前核对 §10 的验证状态，**不要把“计划实现”“交叉编译通过”和“已经跑通实机”混用**。
- 每次修改都按 §13 更新技术文档并运行文档同步 CI。

## 13. 维护规范和逐次变更记录

### 13.1 必须遵守的更新协议

每个包含实现、补丁锚点、调谐算法、依赖、测试、CI/发布或运行行为变更的 PR，**必须同时更新这份技术文档**：

1. 更新对应的“当前实现”章节，写明具体**文件、函数／字段、行为变化及生效范围**。
2. 更新 §10 的验证状态：哪些测试实际运行通过，哪些仍未验证；性能结论必须有可复现对照数据。
3. 更新 §8／§11 的配置、风险和回退步骤（如果受到影响）。
4. 在下面的时间倒序变更记录增加条目；包含背景、涉及文件、设计理由、测试结果、已知风险和待办。
5. 提交前运行 `go test ./...`、`python -m unittest discover -s tests -v` 以及文档同步检查；对涉及上游编译的变更还需跑完整上游构建。

如果只改 README 的措辞，可不增加技术变更条目；如果改变了安装行为或兼容性结论，则必须记录。

> 自动检查是最低门槛：目前只验证 `docs/TECHNICAL.md` 出现在代码变更文件集中，**无法判断文字是否准确**。维护者必须进行内容审查。

### 13.2 变更记录模板（后续复制填写）

```markdown
### YYYY-MM-DD — <改动简述>（PR # / commit / 上游 tag）
- **问题／目标：** 为什么要改。
- **涉及文件：** 路径、关键函数和生成到上游的目标文件。
- **行为变化：** 修改前 → 修改后；哪些出站受影响。
- **验证：** 具体命令、平台、结果；未执行的检查明确写“未验证”。
- **兼容性与风险：** API、WinDivert、路由、资源、许可证或安全边界的变化。
- **回退方法：** 如何恢复此前版本。
- **遗留问题：** 需要后续维护者继续做什么。
```

### 2026-10-29 — 修复最新上游 Release 选择器（Run 110724657760）

- **问题／目标：** 第二次 Actions 在 `Determine upstream tag and check idempotence` 报 `No published upstream Xray releases found`，实际未进入克隆／编译。根因是 `@(Invoke-RestMethod ...)` 输出的 JSON 数组不能按预期在 `Where-Object` 逐项枚举。
- **涉及文件：** `.github/workflows/upstream-release.yml`、新增 `scripts/select_latest_release.py`、`tests/test_release_selector.py`、更新 `tests/check_technical_doc.py`、`tests/test_doc_policy.py`、本文件 §3/§9/§10/§13、`docs/PUBLISH-GITHUB.md`。
- **行为变化：** 默认仍跟随上游最新 Release（含预发布、排除草稿）；工作流改用 `Invoke-WebRequest` 下载 JSON 文本，然后由 Python `select_latest_release()` 逐项比较 `published_at`。手动输入版本仍可固定。非法或意外响应明确停止，不回退旧版；仓库自身的 HTTP 200/404 Release 查询不变。
- **验证：** 根据本次日志确定失败点；新增针对数组、草稿、预发布、非数组 API 错误、未知新标签和 CLI 的单元测试；本地 Python 测试通过。真实 GitHub 在线请求、完整 Xray 构建和 WinDivert 收发仍待验证。
- **兼容性与风险：** 依赖 runner 内 Python（现有测试已有同一依赖）；检查 GitHub API 前 100 条 Release；若最新 tag 改变命名格式，会安全失败。
- **回退方法：** 回退本次工作流与脚本，但旧逻辑会恢复当前已知故障。
- **遗留问题：** 再运行 Actions，确认进入克隆、源码补丁和编译，再根据实际日志继续修复。

### 2026-10-29 — 默认始终构建上游最新发布（手动、定时一致）

- **问题／目标：** 第一次 Actions 修复将手动触发的 `upstream_tag` 固定预填为 `v26.10.21`，与项目自动跟随 Xray 新发布的目标不符。用户要求默认使用上游最新版本。
- **涉及文件：** `.github/workflows/upstream-release.yml` 的 `workflow_dispatch.inputs.upstream_tag.default` 与 `Determine upstream tag and check idempotence`；`docs/PUBLISH-GITHUB.md` 和本文件 §9.3、§13。
- **行为变化：** 手动构建的版本输入默认为空，定时构建也不提供版本输入；两者都查询 Xray 上游 Release 列表，排除草稿、包含预发布，按 `published_at` 选择最近发布的版本。手动填写 `v26.10.21` 等标签仍可明确指定版本。HTTP 200/404 Release 检查与后续编译流程保持不变。
- **验证：** 静态检查工作流版本输入为空及自动选版逻辑、YAML 解析、项目既有 Python/Go 单元测试；**GitHub Actions 实际在线运行和 Xray 全工程构建仍待验证**。
- **兼容性与风险：** 最新上游代码可能改变补丁锚点或 Go 依赖，导致严格打补丁／编译失败；失败应停止并修复，不自动退回旧版本。当前“最新”按发布时间定义，不等于 GitHub `/releases/latest`（后者不含预发布）。
- **回退方法：** 回退本工作流和文档修改；如需要只构建旧版，推荐在手动触发时填写明确标签，而不是修改默认值。
- **遗留问题：** 运行新工作流验证最新版本的真实补丁兼容性；实验预发布关闭时，每 6 小时可能重复尝试同一版本，后续需引入构建成功标记。

### 2026-10-29 — 修复首次 GitHub Actions 版本与 Release 检查（Run 110717158087）

- **问题／目标：** 新建仓库首次运行 Actions 时，在 `Determine upstream tag and check idempotence` 以退出码 1 失败；未进入 Xray 克隆和编译。原逻辑运行 `gh release view` 并将非零退出码视为不存在，但 PowerShell runner 可能直接终止步骤；其错误输出又被重定向到 `$null`，无法精确确认是哪条 native 命令终止。日志显示 `REQUESTED_TAG` 为空，因此自动选择最新上游发布而非指定基线版本。
- **涉及文件：** `.github/workflows/upstream-release.yml`、`README.md`、`docs/PUBLISH-GITHUB.md` 和本文 §9.3、§10 及变更记录。
- **行为变化：** 手动运行默认预填 `v26.10.21`；定时任务仍采用最新上游版本；Release 检查改用 PowerShell `Invoke-WebRequest -SkipHttpErrorCheck`，只把 HTTP 404 当作未发布，HTTP 200 当作已发布，其他状态报错；明确打印所选上游 tag 和 API 检查状态。未修改 TCP 协议栈、WinDivert、窗口控制器或补丁锚点。
- **验证：** 解压用户提供的 Actions 日志并确认 `checkout` 成功、版本检查步骤退出码 1、后续步骤均未运行；本地 YAML 解析和 Python/Go 项目测试通过。**修复后的 GitHub Actions 在线执行尚未验证**；PowerShell 逻辑尚未在真实 runner 上执行。
- **兼容性与风险：** `-SkipHttpErrorCheck` 需要 PowerShell 7，工作流声明 `shell: pwsh`；REST API 需可访问 GitHub 并拥有读取 release 权限。错误 HTTP 状态不会被悄悄当作“未发布”。
- **回退方法：** 回退该工作流和文档提交；但原 `gh release view` 逻辑会再次遇到本次入口失败风险。
- **遗留问题：** 上传修复、重新手动运行 `v26.10.21`，再根据后续首次真正运行的补丁/编译日志处理兼容性错误；防止实验阶段重复构建同一 tag 的持久状态机制另行设计。

### 2026-10-29 — 公开仓库命名、中文首页与发布指引

- **问题／目标：** 对外仓库使用 `Xray-WinOptimize`，突出 Windows 平台 Xray 优化，中文 README 为默认入口；保留独立英文说明和可复现上传流程。
- **涉及文件：** 仓库根目录命名；`README.md`（改为中文默认）、`README.en.md`（英文版，原 `README.md`）、移除 `README.zh-CN.md`；`.github/workflows/upstream-release.yml`（打包两种 README）；新增 `docs/PUBLISH-GITHUB.md`；本 `docs/TECHNICAL.md` 的文件映射和变更记录。
- **行为变化：** Xray TCP 运行代码、WinDivert 适配器和 Linux 风格窗口算法均**未改动**。初次发布仍只上传源码与 Actions 实验产物，自动 GitHub 预发布由仓库变量控制。
- **验证：** 校验文件清单、工作流 README 引用、原有 Python／Go 单元测试；GitHub 在线 Actions、完整 Xray 编译及 Windows 实机 TCP 联网仍未验证。
- **兼容性与风险：** 项目源码可公开，但驱动再分发、许可证选型和未经验证的实验二进制风险仍须谨慎处理；没有捆绑 WinDivert。
- **回退方法：** 使用前一版仓库包，或将 README 与工作流文件还原；不影响正在使用的官方 Xray。
- **遗留问题：** 仓库所有者选择适用许可证并检查第三方声明，首次运行 GitHub Actions；通过真实网络验证前不启用自动预发布。

### 2026-10-29 — 建立技术文档与强制同步检查（仓库文档维护）

- **问题／目标：** 将访客安装说明与开发技术交接资料分离，避免后续随 Xray 自动升级时丢失设计原因与验证状态。
- **涉及文件：** 新增 `docs/TECHNICAL.md`、`tests/check_technical_doc.py`、`tests/test_doc_policy.py`、`.github/workflows/docs-check.yml`、`.github/pull_request_template.md`；精简 `README.md`、`README.zh-CN.md`；调整 `.github/workflows/upstream-release.yml` 的构建包清单，附带中英文 README 和技术文档；在 `docs/INSTALL-WINDOWS.md` 增加交接文档入口。
- **行为变化：** **未改动 TCP、WinDivert 或窗口算法运行代码**；PR/push 若更改实现／测试／CI／安装程序却未更改本文，文档检查失败；每个成功构建的 Artifact 内附技术文档，方便离线交接。
- **验证：** 本地 Python 单元测试共 9 项通过，`go test ./...` 通过，Windows amd64 独立适配器交叉编译通过；GitHub 在线 CI 和完整上游 Xray 编译仍需首次运行验证。
- **兼容性与风险：** 文档策略要求在每次代码变更中维护本文；脚本只能做文件级检查，不能审查内容真实性。
- **回退方法：** 可以回退本次文档／CI 变更，但建议始终保留历史变更记录；运行程序不受影响。
- **遗留问题：** 首次在 GitHub Actions 验证检查工作流，并持续核对上游补丁接口。

### 2026-10-29 — 实验 TCP 出站原型基线（先前原型，未完成全链路验证）

- **问题／目标：** 研究 Windows 原生 Xray 高 RTT 下的 TCP 接收窗口性能差距，避免继续仅使用固定 `SO_RCVBUF` 作为绕过方案。
- **涉及文件：** `adapter/packet_windows.go`、`adapter/packet_other.go`、`recvwindow/window.go`、`recvwindow/window_test.go`、`patch/apply.py`、`patch/dialer_windows.go`、`tests/test_patcher.py`、`.github/workflows/upstream-release.yml`、`docs/INSTALL-WINDOWS.md`。
- **行为变化：** 通过独立 WinDivert 报文适配器复用 Xray 自带轻量级 TCP 栈，新增 Linux-inspired 接收窗口控制器；进程环境变量控制替代出站拨号器；自动构建拉取 Xray 上游标签并严格应用补丁。
- **验证：** 独立窗口算法单测、Windows 交叉编译和模拟源码打补丁检查有初步记录；完整 Xray 编译、真实 Windows 网络传输、REALITY 和测速均未验证。
- **兼容性与风险：** 驱动使用、SYN-ACK 捕获、校验和、高并发资源、IPv6/VPN/多网卡及上游字段变化；仅供实验。
- **回退方法：** 停止实验版 Xray、清除启用变量、恢复官方 Xray。
- **遗留问题：** 逐项完成 §12 的构建、单流、握手、兼容性和性能验证。

---

## 参考资料

- [Xray-core 仓库](https://github.com/XTLS/Xray-core)（以对应版本 tag 的源码为准）。
- [WinDivert 仓库](https://github.com/basil00/WinDivert) 与 [WinDivert API 文档](https://reqrypt.org/windivert-doc.html)。
- [Linux TCP 源码 `net/ipv4/tcp_input.c`](https://github.com/torvalds/linux/blob/master/net/ipv4/tcp_input.c)（只作为设计思路参考，**没有照搬 Linux 内核源代码**）。
- [Windows 实验安装手册](INSTALL-WINDOWS.md)。

**交接原则：** 上面的代码位置与参数以本仓库当前源码为准；外部上游源码、驱动版本和运行环境发生变化时，必须重新验证，不能沿用旧版本的“已通过”结论。

## 安全审计补充（2026-10-30，静态代码与被动指纹风险）

新增独立文档 [`docs/FINGERPRINT-AUDIT.md`](FINGERPRINT-AUDIT.md)，审核默认 Winsock 后端与早期 WinDivert 实验构建的差异，说明冷连接、目标缓存热连接和可选 TCP 时间戳三种情况下的被动指纹面，提供不依赖服务器 SSH 的 Windows `pktmon` 抓包对照流程，并明确证据边界。

**本次仅新增文档，不改变任何 Go 代码、缓冲区增长算法或 GitHub Actions。** 已确认在默认模式中 TIME_STAMPS 不主动设置；10min 目标缓存可能在握手前改变 Socket 选项，属于需要 PCAP 核查的潜在风险。尚未获得 SYN/ACK 抓包及 GFW 规则实证，不得对外宣称“无指纹”或“保证不可检测”。
