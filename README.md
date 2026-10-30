# Xray-WinOptimize

简体中文 | [English](README.en.md)

在我自己的 Windows 电脑、网络线路和 VPS 环境中，我发现：**当网络拥堵、延迟较高时，Windows TCP 接收窗口自动调谐虽然仍在工作，但接收缓冲区增长不够及时，Xray 的下载速度会明显下降。** 手动增大 `SO_RCVBUF` 后，下行速度得到显著改善。**这就是 Xray-WinOptimize 的起点。**

Xray-WinOptimize 是基于 Xray-core 的 Windows TCP 出站性能优化项目。它参考 Linux TCP 接收缓冲区自适应调节的设计思路，利用 **Windows 原生 TCP 协议栈和 Socket 接口**，根据连接状态调整接收缓冲区，改善拥堵、高延迟网络下的下行吞吐表现。

## 项目背景与实测

排查过程中，我通过 Windows TCP 连接的内部统计确认：默认接收缓冲区会逐步扩大，但在拥堵条件下，增长速度可能跟不上实际下载需求。相比之下，显式设置较大的 `SO_RCVBUF` 能更快提供足够的接收窗口。

在我使用的网络和 VPS 测试环境中，采用本项目修改后的 Xray-core，**下行速度曾从约 40 Mbps 提升至 70 Mbps**。项目的目标就是把这一思路发展为根据连接状态动态调整的方案，减少手工设置固定缓冲区的需要。

## 优化方式

- **针对 Xray 出站连接：** 优化通过 Xray 默认 TCP 拨号器建立的 Windows 出站 TCP 连接。
- **自适应接收缓冲区：** 结合连接状态，使用 Windows Socket 接口（包括 `SO_RCVBUF`）调整接收缓冲区。
- **原生运行：** 沿用 Windows TCP 协议栈，通常无需管理员权限、额外网络驱动或全局 TCP 参数修改。
- **可随时关闭：** 通过 `XRAY_WINOPT_MODE=off` 关闭优化，方便进行对照测试或回退。

具体算法、配置选项与源码实现见 [技术文档](docs/TECHNICAL.md)。

## 下载与自动发布

在 [GitHub Releases](https://github.com/Vikrant-Aleph/Xray-WinOptimize/releases) 下载对应平台的构建包：

| 发布文件 | 目标系统 |
| --- | --- |
| `Xray-winopt-win7-32.zip` | Windows 7 x86 |
| `Xray-winopt-win7-64.zip` | Windows 7 x64 |
| `Xray-winopt-win-32.zip` | 现代 Windows x86 |
| `Xray-winopt-win-64.zip` | 现代 Windows x64 |
| `Xray-winopt-win-arm-64.zip` | Windows ARM64 |

**每个 ZIP 仅包含 `xray.exe`、`LICENSE` 和 `BUILD-INFO.txt`。** 可执行文件统一命名为 `xray.exe`，可直接用于兼容的命令行环境和图形客户端。

GitHub Actions **每隔一天**检查 Xray-core 最新已发布的 Release（包含预发布版本）。如果该上游版本尚无完整的 WinOptimize Release，就启动五平台构建；**全部构建成功并通过架构检查后才发布新版本**。也可以在 [Actions](https://github.com/Vikrant-Aleph/Xray-WinOptimize/actions) 手动运行 `Xray-WinOptimize (latest upstream)`，版本输入留空表示选择上游最新 Release。

**Windows 7 兼容性：** 对应构建使用 [XTLS/go-win7](https://github.com/XTLS/go-win7) 工具链。自适应优化依赖的 `SIO_TCP_INFO` 接口在 Windows 7 上可能不可用，因此该平台的优化效果和运行兼容性需要实机确认。

## 安装与使用

建议先备份原来的 `xray.exe` 和配置文件。如果已手动设置出站 `SO_RCVBUF`，可以先移除该选项，再测试自适应效果。

### 命令行运行

将发布包中的 `xray.exe` 放入现有 Xray 运行目录，使用原来的配置文件启动：

```powershell
.\xray.exe run -config .\config.json
```

### 在图形客户端中替换内核（例如 v2rayN）

1. 完全退出图形客户端，确保 Xray 进程已经停止，并备份原来的 `xray.exe`。
2. 找到客户端正在使用的 Xray 内核目录。Windows 版 v2rayN 的常见路径为 `bin\xray\`，具体以实际安装目录为准。
3. 将发布包中的 `xray.exe` 复制到该目录，覆盖原内核。注意保留图形客户端程序和目录中的其他文件。
4. 重新启动客户端，确认选用 Xray 核心，并检查日志是否正常启动。

如果图形客户端更新了内核，可能需要再次替换。遇到兼容性问题时，可恢复备份的原版 `xray.exe`。

### 关闭优化

在 PowerShell 中设置环境变量后重新启动 Xray：

```powershell
$env:XRAY_WINOPT_MODE = 'off'
.\xray.exe run -config .\config.json
```

## 流量行为与兼容性

接收缓冲区调整可能影响 TCP 接收窗口通告、ACK 时序及数据包时序。这些变化与流量分析有关，具体讨论见 [TCP 流量指纹风险审计](docs/FINGERPRINT-AUDIT.md)。

本项目主要针对 Windows TCP 出站接收窗口调节。实际效果与网络拥堵程度、RTT、丢包和服务器状态等因素有关。

## 相关文档

- [技术设计、测试记录与变更历史](docs/TECHNICAL.md)
- [Windows 安装与回退](docs/INSTALL-WINDOWS.md)
- [TCP 流量指纹风险审计](docs/FINGERPRINT-AUDIT.md)
- [GitHub 构建与发布说明](docs/PUBLISH-GITHUB.md)
- [English README](README.en.md)

本项目为独立的非官方项目，与 [XTLS/Xray-core](https://github.com/XTLS/Xray-core) 官方无隶属关系。
