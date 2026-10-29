# Xray-WinOptimize

面向 Windows 的非官方 Xray-core 网络性能优化项目。参考 Linux 的接收缓冲区增长思想，针对高延迟、拥堵网络自适应优化 Xray 的 TCP 出站性能。

现有默认实现保留 Windows 原生 TCP，**不需要额外驱动或管理员权限**。它会在满足条件时调整连接接收缓冲区；这不等同于移植 Linux 内核 TCP。项目仍处于实验阶段，不能保证对所有网络提速。

### 下载与使用

1. 进入仓库 **Actions → Xray-WinOptimize (latest upstream)**，点击 **Run workflow**。版本留空时跟随 Xray 上游最新 Release（包含预发布）。
2. 构建成功后在 **Artifacts** 下载构建包，解压得到 `xray-winopt.exe`。
3. 保留现有 Xray 配置，但请移除此前手动设置的 `SO_RCVBUF`，避免覆盖自动优化策略。
4. 和原版 Xray 一样启动：

   ```powershell
   .\xray-winopt.exe run -config .\config.json
   ```

默认只影响 Xray 发起的 TCP 出站连接，不改变其他 Windows 应用的流量或系统全局 TCP 设置。关闭优化：

```powershell
$env:XRAY_WINOPT_MODE='off'
.\xray-winopt.exe run -config .\config.json
```

**可选的 TCP 时间戳实验：** 在启动前设 `$env:XRAY_WINOPT_TCP_TIMESTAMPS='on'` 可以仅对 Xray 出站连接尝试启用 TCP 时间戳；默认保持 Windows 原始设置。该选项的性能收益尚未验证，可用 `XRAY_WINOPT_LOG=1` 检查是否设置成功。详见 [技术文档](docs/TECHNICAL.md)。

### 项目文档

- [技术设计、详细改动与变更记录](docs/TECHNICAL.md)
- [Windows 安装和回退](docs/INSTALL-WINDOWS.md)
- [GitHub 自动更新和发布](docs/PUBLISH-GITHUB.md)
- [English](README.en.md)

另有 WinDivert 实验性独立 TCP 栈研究代码，**不包含在默认发布的 Xray 可执行文件中**。本项目与 [XTLS/Xray-core](https://github.com/XTLS/Xray-core) 官方无隶属关系。
