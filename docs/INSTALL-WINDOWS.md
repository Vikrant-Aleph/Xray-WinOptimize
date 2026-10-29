# Windows 安装与运行

## 从 Actions 下载

1. 进入 **Actions → Xray-WinOptimize (latest upstream)**，手动运行。留空 `upstream_tag` 即选择 Xray 最新已发布版本，包括预发布；也可填 `v26.10.21` 诊断上游兼容性。
2. 等工作流成功后，从运行详情的 **Artifacts** 下载 `xray-winopt-v...`，解压获得构建 ZIP，再解压可执行文件 `xray-winopt.exe`。
3. 复制你的 `config.json`；建议先移除曾经用于测速的 `SO_RCVBUF` 自定义 Socket 选项，其他 VLESS/REALITY/Vision/SOCKS 配置不变。务必保留原版 Xray 作为回退。
4. 无需管理员权限，无需 WinDivert DLL/SYS，无需安装服务：

```powershell
.\xray-winopt.exe version
.\xray-winopt.exe run -config .\config.json
```

## 工作模式

- 默认自适应优化：未设置 `XRAY_WINOPT_MODE`，或设置为 `portable`。针对 Xray 默认拨号器发起的 TCP 出站，连接初始不强制缓冲大小。按接收 RTT 周期分析持续下载，短流不介入；首次和后续均按**借鉴 Linux 设计**的 RTT 周期接收量及增长趋势策略决定缓冲区，没有固定 1 MiB/2 MiB 探测；上限与内存预算由系统物理内存决定。
- 禁用：`$env:XRAY_WINOPT_MODE='off'`，随后**重启 Xray**，新连接恢复 Windows 原生 TCP 行为。
- 日志：`$env:XRAY_WINOPT_LOG='1'`，打印 `WINOPT ...` 调整事件；请勿在公开日志中泄露 VPS IP 或配置密钥。
- 单连接最大缓冲区：**默认自动根据 RAM 容量确定**（4～32 MiB）；可设置 `$env:XRAY_WINOPT_MAX_MIB='16'` 固定上限（支持 1～64 MiB）。
- 进程请求预算：默认随 RAM 容量变化（64～512 MiB），可设置 `$env:XRAY_WINOPT_BUDGET_MIB='128'` （支持 32～2048 MiB）；物理内存不足时跳过新的缓冲区扩容。
- 已显式指定 `customSockopt` 的 Windows `SO_RCVBUF` 的连接被跳过。

## 通用自适应增长策略

当前版本只有**一套默认增长逻辑**。首次干预不强制设置 1 MiB，后续扩容也不会因为通告窗口大于接收缓冲区的 70% 就自动停下；两者都按每 RTT 收量和增长趋势计算需求，单次最多扩大至当前的四倍（只在公式估计需求足够时），调整间隔至少 `max(RTT,400ms)`，并受到内存预算约束。首次可在一个完整活跃 RTT 统计周期后介入。

旧版本的 `$env:XRAY_WINOPT_GROWTH='aggressive'` 在本版不再生效，无须设置或保留。如需对照完全没有补丁的 TCP 行为，设置 `$env:XRAY_WINOPT_MODE='off'` 并重启 Xray；如需对照上一版算法，请使用上一版二进制文件。

可设置 `$env:XRAY_WINOPT_LOG='1'` 查看实际调整日志（`WINOPT expanded ... set=...`），但该日志仅确认请求成功，不表示吞吐一定提高。这版是 v3.3 Fast Ramp，已取消首次额外等待第二个活跃 RTT 统计周期；保持 RTT≥40ms、累计 128KiB、单周期至少 32KiB 的初筛，并维持相同的内存预算与低内存保护。更快增长也可能增加内存压力，尚需 Windows 实机验证。详见 [TECHNICAL.md](TECHNICAL.md)。

## 可选的 TCP 时间戳对照（默认不改变）

微软 Windows Winsock 提供连接级 `TCP_TIMESTAMPS`（IPPROTO_TCP/10），用于协商 TCP 时间戳。该选项可能影响 RTT 测量与包头开销，但**不能保证提高吞吐率**。仅针对 Xray 默认 TCP 拨号器里的新连接，必须在启动进程前设置变量并重建连接：

```powershell
# A：默认，完全不覆盖系统的 TCP 时间戳选择
Remove-Item Env:XRAY_WINOPT_TCP_TIMESTAMPS -ErrorAction SilentlyContinue
.\xray-winopt.exe run -config .\config.json

# B：新进程，显式尝试开启该连接的 TCP 时间戳
$env:XRAY_WINOPT_TCP_TIMESTAMPS='on'
$env:XRAY_WINOPT_LOG='1'
.\xray-winopt.exe run -config .\config.json
```

也可设为 `off`，实验后请删除该变量。`TCP_TIMESTAMPS` 必须在握手前设置；若系统拒绝，错误会被记录但不会中断代理连接。请避免同时改动窗口/网络/测速服务器。此实验不能调整服务器 BBR、不能改变 Windows 内核的 ACK/SACK 算法。

## 本地构建

在当前项目目录：

```powershell
go test ./portable ./recvwindow
python -m unittest discover -s tests -v
```

准备一个**干净、与目标 Release 对应**的 Xray 源码目录后：

```powershell
python patch/apply_portable.py C:\path\to\Xray-core
cd C:\path\to\Xray-core
gofmt -w .\transport\internet\system_dialer.go
Get-ChildItem .\transport\internet\winopt_*.go | ForEach-Object { gofmt -w $_.FullName }
go build -trimpath -buildvcs=false -o xray-winopt.exe ./main
```

`apply_portable.py` 会严格检查 Xray 拨号源码锚点，不匹配时直接失败。实验后端的 `patch/apply.py` 与此为**两种互斥补丁**，不要套在同一源码目录上。

## 尚未验证／风险

- 独立 Go 模块、补丁测试和 Windows amd64 交叉编译检查通过；完整 Xray 编译和 Windows 11 实际连接尚未完成验证。
- 中途增大 SO_RCVBUF 不一定能立即改变窗口缩放；新建连接的预握手缓存可能更有效。
- 不是 Linux TCP 栈，不直接修改 Windows ACK/SACK/丢包恢复；不保证超过已实测的固定 1 MiB 速度。
- 回退始终是停止进程，用原版 `xray.exe` 和原配置启动；不修改 Windows 全局设置。
