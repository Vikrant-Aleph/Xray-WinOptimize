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
