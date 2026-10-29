# Windows 安装与运行（Portable 默认）

## 从 Actions 下载

1. 进入 **Actions → Xray-WinOptimize Portable (latest upstream)**，手动运行。留空 `upstream_tag` 即选择 Xray 最新已发布版本，包括预发布；也可填 `v26.10.21` 诊断上游兼容性。
2. 等工作流成功后，从运行详情的 **Artifacts** 下载 `xray-winopt-portable-v...`，解压获得构建 ZIP，再解压可执行文件 `xray-winopt-portable.exe`。
3. 复制你的 `config.json`；建议先移除曾经用于测速的 `SO_RCVBUF` 自定义 Socket 选项，其他 VLESS/REALITY/Vision/SOCKS 配置不变。务必保留原版 Xray 作为回退。
4. 无需管理员权限，无需 WinDivert DLL/SYS，无需安装服务：

```powershell
.\xray-winopt-portable.exe version
.\xray-winopt-portable.exe run -config .\config.json
```

## 工作模式

- 默认 Portable 自适应：未设置 `XRAY_WINOPT_MODE`，或设置为 `portable`。针对 Xray 默认拨号器发起的 TCP 出站，连接初始不强制缓冲大小。按接收 RTT 周期分析持续下载，短流不介入；先做 1 MiB 探测，然后参考 Linux 风格 RTT 数据增长趋势动态扩容；上限与内存预算由系统物理内存决定。
- 禁用：`$env:XRAY_WINOPT_MODE='off'`，随后**重启 Xray**，新连接恢复 Windows 原生 TCP 行为。
- 日志：`$env:XRAY_WINOPT_LOG='1'`，打印 `WINOPT ...` 调整事件；请勿在公开日志中泄露 VPS IP 或配置密钥。
- 单连接最大缓冲区：**默认自动根据 RAM 容量确定**（4～32 MiB）；可设置 `$env:XRAY_WINOPT_MAX_MIB='16'` 固定上限（支持 1～64 MiB）。
- 进程请求预算：默认随 RAM 容量变化（64～512 MiB），可设置 `$env:XRAY_WINOPT_BUDGET_MIB='128'` （支持 32～2048 MiB）；物理内存不足时跳过新的缓冲区扩容。
- 已显式指定 `customSockopt` 的 Windows `SO_RCVBUF` 的连接被跳过。

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
go build -trimpath -buildvcs=false -o xray-winopt-portable.exe ./main
```

`apply_portable.py` 会严格检查 Xray 拨号源码锚点，不匹配时直接失败。实验后端的 `patch/apply.py` 与此为**两种互斥补丁**，不要套在同一源码目录上。

## 尚未验证／风险

- 独立 Go 模块、补丁测试和 Windows amd64 交叉编译检查通过；完整 Xray 编译和 Windows 11 实际连接尚未完成验证。
- 中途增大 SO_RCVBUF 不一定能立即改变窗口缩放；新建连接的预握手缓存可能更有效。
- 不是 Linux TCP 栈，不直接修改 Windows ACK/SACK/丢包恢复；不保证超过已实测的固定 1 MiB 速度。
- 回退始终是停止进程，用原版 `xray.exe` 和原配置启动；不修改 Windows 全局设置。
