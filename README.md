# Xray-WinOptimize

**面向 Windows 平台的非官方 Xray-core 网络性能优化实验项目。** 默认采用免管理员权限、无需驱动的 **Portable TCP 自适应接收缓冲区策略**，参考 Linux 接收 RTT 周期和增长趋势来调整缓冲区，希望改善高延迟、拥堵网络下 Windows 原生 Xray 的下载性能。

这不是 Linux TCP 内核移植，也不是已经验证稳定提速的版本：Portable 仍使用 Windows Winsock，并根据连接 RTT、数据接收量与接收窗口状态，选择性设置 `SO_RCVBUF`。设置该选项会改变 Windows 原生接收窗口自动调谐行为，性能可能不及或优于固定缓冲区，取决于环境。**完整 Xray 构建和 Windows 实机测速待验证。**

### 下载与运行

1. 打开仓库 **Actions → Xray-WinOptimize Portable (latest upstream) → Run workflow**；版本留空即选上游最新 Release（包含预发布）。构建成功后下载页面底部的 **Artifacts**。
2. 解压 `xray-winopt-portable.exe`，保留你原来的 Xray 配置。**删除之前在配置中手写的 `SO_RCVBUF`**，否则该 Socket 的自适应策略会让位于显式配置。
3. 像原版 Xray 一样，直接启动（普通权限即可）：

   ```powershell
   .\xray-winopt-portable.exe run -config .\config.json
   ```

默认开启 Portable 模式，作用于 **Xray 默认拨号器发起的 TCP 出站**（含 SOCKS5/HTTP 入站经代理连接到的远端；不接管 Windows 其他进程，也不改变 UDP）。低 RTT、小流量连接通常不会被修改。

关闭优化、恢复纯原版 Windows TCP 行为：

```powershell
$env:XRAY_WINOPT_MODE='off'
.\xray-winopt-portable.exe run -config .\config.json
```

要记录策略日志，在启动前设置 `$env:XRAY_WINOPT_LOG='1'`。单连接缓冲区上限和进程预算默认随物理内存变化；可选设置 `XRAY_WINOPT_MAX_MIB=1..64`、`XRAY_WINOPT_BUDGET_MIB=32..2048`，详见技术文档。

原来的 **WinDivert 独立 TCP 栈**保留在源码中作为另一条需要管理员权限的研究路线，不包含在默认 Portable 可执行文件中。它与 Portable 的性能、权限和验证状态不能混为一谈。

### 开发与维护

- [实现原理、逐文件改动、限制和变更记录](docs/TECHNICAL.md)
- [Windows 安装、验证和回退](docs/INSTALL-WINDOWS.md)
- [GitHub 自动更新与发布](docs/PUBLISH-GITHUB.md)
- [英文说明](README.en.md)
- 上游：[XTLS/Xray-core](https://github.com/XTLS/Xray-core)，本项目与官方无隶属关系。

**每次修改代码、补丁、测试或构建流程，必须同时更新 `docs/TECHNICAL.md`。** 仓库 CI 会检查。
