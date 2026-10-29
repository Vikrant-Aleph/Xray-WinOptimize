# Xray-WinOptimize（实验项目）

这是一个独立的 **Xray-core Windows TCP 出站实验分支**。它尝试使用 Xray 自带的轻量级 TCP/IP 栈，通过 WinDivert 收发网络报文，并加入参考 Linux 思路设计的接收窗口增长策略，研究 Windows 原生 Xray 在高 RTT、拥堵链路上的吞吐问题。

**当前尚未完成 Xray 全工程编译与 Windows 实机联网验证，不能视为稳定版或已验证的提速方案。**

功能开启后，目标是替换 **Xray 默认拨号器的 TCP 出站连接**；SOCKS5/HTTP 系统代理入站保持不变，UDP 及其他应用的 TCP 不会被整体接管。部分特殊出站或 Socket 选项尚不支持。

## 下载与安装（Windows x64）

1. 打开本仓库的 **Actions**，运行 `Track upstream Xray release and build experimental TCP backend`，或下载已有的成功构建 Artifact。只有仓库所有者主动开启实验预发布后，才会在 **Releases** 中出现可下载版本。
2. 解压构建 ZIP，保留原版 `xray.exe` 作为回退。构建包默认只包含 `xray-winrecv-experimental.exe`、构建信息和说明，**不包含 WinDivert 驱动**。
3. 从 [WinDivert 官方发布页](https://github.com/basil00/WinDivert/releases/tag/v2.2.2)获取匹配的 Windows x64 `WinDivert.dll` 和签名的 `WinDivert64.sys`，放在实验版 EXE 所在目录。不要从不明来源下载驱动。
4. 使用原来的 Xray 配置文件，移除此前测试添加的 `SO_RCVBUF` 自定义选项。在**管理员 PowerShell** 中执行（替换实际配置路径）：

   ```powershell
   $env:XRAY_EXPERIMENTAL_TCP_STACK = '1'
   .\xray-winrecv-experimental.exe run -config .\config.json
   ```

5. 先验证单条 TCP 连接及 VLESS/REALITY 握手，再尝试测速。实验模式可能与 VPN、防火墙或网络过滤驱动冲突，请勿直接用于重要网络连接。

**关闭／回退：** 停止实验版 Xray，清除变量并启动保存好的官方版：

```powershell
Remove-Item Env:\XRAY_EXPERIMENTAL_TCP_STACK -ErrorAction SilentlyContinue
.\xray.exe run -config .\config.json
```

自行构建或排错请看 [Windows 详细安装说明](docs/INSTALL-WINDOWS.md)；仓库所有者首次上传请看 [GitHub 公开发布指南](docs/PUBLISH-GITHUB.md)。

## 项目维护

- [技术文档与变更记录](docs/TECHNICAL.md)：架构、各文件修改、TCP 窗口算法、WinDivert 设计、限制、验证进度与交接说明。
- [上游 Xray-core](https://github.com/XTLS/Xray-core)；本仓库为非官方实验项目。
- **代码、补丁、构建或安装流程每次变更，必须同步更新 `docs/TECHNICAL.md`**；仓库的文档检查工作流会阻止缺少技术文档更新的改动。

English: [README.en.md](README.en.md)
