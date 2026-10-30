# Xray-WinOptimize

**Xray-core 的非官方 Windows TCP 出站性能优化项目（实验性）。** 项目借鉴 Linux TCP 接收缓冲区自动调谐的设计思路，在保留 Windows 原生 TCP 协议栈的前提下，让 Xray 根据连接状态适时调整接收缓冲区，尝试改善高延迟、拥堵网络下的下载表现。它**不是** Linux TCP 协议栈移植，也不等同于 Linux 内核的自动调谐算法。

## 项目背景与测试结果

**关于 Windows 自动调谐在拥堵网络中表现不佳的现象，目前仅来自项目作者本人的一台 Windows 电脑及其特定网络、VPS 测试环境。** 这不代表所有 Windows 设备、所有网络环境都存在相同问题，也不能据此断言 Windows TCP 自动调谐普遍存在缺陷。

在**作者本人使用的拥堵网络、指定 VPS 及对应测速条件下**，观察到采用本项目优化后的下行速度约从 **40 Mbps 提升至 70 Mbps**。这是**单一环境的实测个例**，不是普遍性能增幅、稳定速度保证或其他用户的测试结论；不同网络、设备、服务器和测试时段可能没有改善，甚至可能下降。

## 使用前须知

- 本项目主要作用于 **Xray 默认 TCP 拨号器建立的出站连接**，不改变整台 Windows 系统的 TCP 栈。默认无需额外网络驱动或管理员权限。
- 自适应机制会调用 Windows Socket 的 `SO_RCVBUF`，因此可能改变网络上可观察到的 **TCP 接收窗口通告、ACK/数据包时序等细微流量行为特征**。这些变化理论上可能成为被动流量分析的辅助特征；**尚无证据表明它们构成独有的 Xray 指纹，也不能保证不可被识别**。参见[流量指纹风险审计](docs/FINGERPRINT-AUDIT.md)。
- 当前为实验版本。使用前备份原内核和配置；若已在出站手动指定 `SO_RCVBUF`，建议先移除该选项再比较自适应效果。

## 下载

进入仓库 **Actions → Xray-WinOptimize (latest upstream)**，点击 **Run workflow**。版本输入留空时使用 Xray 上游最新 Release（包括预发布）。构建成功后，在 **Artifacts** 下载构建包并解压，得到 `xray-winopt.exe`。

## 安装与启动

### 方式一：命令行

将 `xray-winopt.exe` 和原来的 Xray 配置文件放在同一目录，按原版方式启动：

```powershell
.\xray-winopt.exe run -config .\config.json
```

### 方式二：替换图形化客户端中的 Xray 内核（例如 v2rayN）

1. **彻底退出** v2rayN 等图形化客户端，确保 Xray 进程已停止，并备份原来的 `xray.exe`。
2. 找到客户端实际使用的 **Xray 内核目录**。常见 v2rayN Windows 安装目录为 `bin\xray\`，其中的 `xray.exe` 是要替换的内核；不同版本或安装方式可能有所区别，请以实际路径为准。
3. 将构建产物 `xray-winopt.exe` **重命名为 `xray.exe`**，复制到该目录，覆盖原内核文件。**不要替换图形化客户端自身的 EXE**，也不要删除该目录中的其他必要文件。
4. 重新打开客户端，确认选择的是 **Xray 核心**，并从运行日志或内核版本信息检查是否正常启动。节点和现有系统代理设置通常无需改变。

客户端自身的 **更新内核** 功能可能重新覆盖该文件；更新后如需继续使用本项目，须再次替换。遇到兼容性或连接问题，停止客户端并恢复备份的原版 `xray.exe`。

## 关闭优化及更多说明

关闭自适应优化（重启进程生效）：

```powershell
$env:XRAY_WINOPT_MODE = 'off'
.\xray-winopt.exe run -config .\config.json
```

默认只缓存路径 RTT 等历史指标，**不会**自动把上一条连接的缓冲区大小用到下一条连接握手前；其他实验选项、源码改动与限制见技术文档。

- [技术设计、变更记录与交接](docs/TECHNICAL.md)
- [Windows 安装及回退](docs/INSTALL-WINDOWS.md)
- [TCP 指纹风险审计](docs/FINGERPRINT-AUDIT.md)
- [GitHub 构建和发布](docs/PUBLISH-GITHUB.md)
- [English README](README.en.md)

本项目与 [XTLS/Xray-core](https://github.com/XTLS/Xray-core) 官方无隶属关系。
