# Xray-WinOptimize

简体中文 | [English](README.en.md)

在我自己的 Windows 电脑、网络线路和 VPS 环境中，我发现：**网络拥堵、延迟较高时，Windows TCP 接收窗口自动调谐有时无法及时扩大接收窗口，Xray 的下载速度会明显下降。** 手动设置较大的 `SO_RCVBUF` 可以缓解这个问题，但长期使用固定缓冲区并不是理想的通用方案。**这就是 Xray-WinOptimize 诞生的原因。**

Xray-WinOptimize 是一个**非官方、实验性的 Xray-core Windows TCP 出站性能优化项目**。它借鉴 Linux TCP 接收缓冲区自适应调节的思路，在**保留 Windows 原生 TCP 协议栈**的前提下，根据连接状态调整 Socket 接收缓冲区，尝试改善特定拥堵、高延迟网络下的下行吞吐。

> **适用范围声明：** 上述现象仅在我个人的设备、线路和特定 VPS 测试环境中得到观察。它不意味着所有 Windows 用户都会遇到相同问题，也不能据此认定 Windows TCP 自动调谐普遍存在缺陷。

## 项目缘起与实测

排查过程中，我观察到 Windows 的接收缓冲区**并非完全不自动增长**：它会逐步扩大，但在某些拥堵条件下，增长速度和窗口大小不足以在有限的下载时间内充分利用链路带宽。显式增大 `SO_RCVBUF` 后，窗口和下行速度都曾明显改善。这促使我尝试在 Xray 中实现一种面向连接状态的自适应策略，而不是要求用户长期写死一个缓冲区大小。

在**同一套个人网络与 VPS 测试环境**的一次对照中，使用本项目修改后的 Xray-core，下行速度约从 **40 Mbps 提升至 70 Mbps**。

这只是特定环境和时段的测试记录，**不是普遍加速比例或速度保证**。实际效果取决于线路、RTT、丢包、服务器、并发连接和 Windows 版本；在其他环境中可能没有改善，甚至可能变慢。

## 优化方式与边界

- **优化对象：** 由 Xray 默认 TCP 拨号器建立的 Windows 出站 TCP 连接，而不是电脑上的所有网络流量。
- **实现思路：** 利用 Windows 原生 Socket 接口（包括 `SO_RCVBUF`）结合连接状态调整接收缓冲区；不会替换 Windows TCP 协议栈。
- **运行要求：** 默认不需要额外网络驱动或管理员权限，也不需要更改 Windows 全局 TCP 参数。
- **设计边界：** 借鉴 Linux 的自适应调节理念，**不是**移植 Linux TCP 协议栈，也不等同于 Linux 内核中的自动调谐算法。
- **跨连接信息：** 默认只保留用于参考的路径 RTT 等历史提示，**不会**在新连接 SYN 之前直接沿用上一条连接的接收缓冲区大小。

有关具体算法、配置选项、源码改动和实验限制，请阅读 [技术文档](docs/TECHNICAL.md)。

## 下载与自动发布

从 [GitHub Releases](https://github.com/Vikrant-Aleph/Xray-WinOptimize/releases) 下载对应架构的构建包：

| 发布文件 | 目标系统 |
| --- | --- |
| `Xray-winopt-win7-32.zip` | Windows 7 x86（特殊兼容工具链） |
| `Xray-winopt-win7-64.zip` | Windows 7 x64（特殊兼容工具链） |
| `Xray-winopt-win-32.zip` | 现代 Windows x86 |
| `Xray-winopt-win-64.zip` | 现代 Windows x64 |
| `Xray-winopt-win-arm-64.zip` | Windows ARM64 |

**每个 ZIP 只包含 `xray.exe`、`LICENSE` 和 `BUILD-INFO.txt`。** README 和其他文档保存在本仓库，不会随二进制包重复打包；可执行文件统一命名为 `xray.exe`，无需额外重命名。

GitHub Actions **每隔一天**检查 Xray-core 最新已发布的 Release（包含预发布版本，不包含草稿）。如果该上游版本尚无完整的 WinOptimize Release，工作流会尝试构建全部五个平台；**只有五个平台全部成功并通过架构检查，才发布新的 Release**，不会用旧版本构建包充当新版本。开发者也可在 [Actions](https://github.com/Vikrant-Aleph/Xray-WinOptimize/actions) 手动运行 `Xray-WinOptimize (latest upstream)`；版本输入留空即选择上游最新 Release。

> **Windows 7 注意：** Windows 7 已不受标准 Go 工具链支持。对应构建使用 [XTLS/go-win7](https://github.com/XTLS/go-win7) 兼容工具链并依赖必要的系统更新。由于自适应机制使用的 `SIO_TCP_INFO` 接口在 Windows 7 上不保证可用，**能编译或启动并不等于优化已生效**。Windows 7 的实际联网与运行兼容性尚未经过实机验证，其他平台的实际兼容性也仍需测试。

## 安装与使用

使用前建议备份现有 Xray 内核和配置，以便出现连接问题时快速回退。如果已在出站配置中手动指定 `SO_RCVBUF`，建议先移除该选项再比较本项目的自适应效果，避免测试条件相互干扰。

### 命令行运行

将 ZIP 中的 `xray.exe` 放入现有 Xray 运行目录，沿用原来的配置文件：

```powershell
.\xray.exe run -config .\config.json
```

### 在图形客户端中替换内核（例如 v2rayN）

1. **完全退出**图形客户端并确保 Xray 进程已经停止，备份现有 `xray.exe`。
2. 找到客户端实际使用的 Xray 内核目录。Windows 版 v2rayN 常见位置为 `bin\xray\`，具体以你的安装方式为准。
3. 将发布包中的 `xray.exe` 复制到该目录并覆盖原内核。**不要覆盖图形客户端本身的 EXE，也不要删除其他必要文件。**
4. 重新启动客户端，确认正在使用 Xray 核心，并检查日志确认内核正常启动。遇到问题时恢复原版 `xray.exe`。

图形客户端的“更新内核”功能可能再次覆盖这个修改版，届时需要重新替换。

### 临时关闭优化

使用命令行时，可为当前 PowerShell 会话设置以下环境变量，然后重新启动 Xray：

```powershell
$env:XRAY_WINOPT_MODE = 'off'
.\xray.exe run -config .\config.json
```

## 限制与流量行为风险

本项目仍处于实验阶段。调整 Socket 接收缓冲区可能改变网络上可观察到的 **TCP 接收窗口通告、ACK 时序及数据包时序**，理论上可能增加流量行为分析可利用的特征。但目前**没有证据证明这些变化是 Xray 独有的指纹，或已形成可靠的识别方法**；项目也不承诺无法被识别。详见 [TCP 流量指纹风险审计](docs/FINGERPRINT-AUDIT.md)。

该优化不保证解决所有慢速原因，也不能替代对线路质量、丢包、服务器负载和代理配置的排查。不建议仅凭单次测速判断效果。

## 文档与声明

- [技术设计、测试记录与变更历史](docs/TECHNICAL.md)
- [Windows 安装与回退](docs/INSTALL-WINDOWS.md)
- [TCP 流量指纹风险审计](docs/FINGERPRINT-AUDIT.md)
- [GitHub 构建与发布说明](docs/PUBLISH-GITHUB.md)
- [English README](README.en.md)

本项目为独立的非官方实验项目，与 [XTLS/Xray-core](https://github.com/XTLS/Xray-core) 官方无隶属关系。
