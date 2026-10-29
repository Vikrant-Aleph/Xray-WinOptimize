# Xray-WinOptimize — GitHub 公开发布与持续更新

## 1. 公开发布源码

在 https://github.com/new 创建名为 `Xray-WinOptimize` 的 **Public** 仓库。不要勾选生成 README、.gitignore 或许可证模板（先审查下述许可证问题）。

**仓库简介：** Windows 平台上修改 TCP 协议栈的 Xray-core，支持 Linux 风格接收窗口自动调谐，旨在改善高延迟、拥堵网络下 Windows 平台的 Xray 代理性能。

将完整项目解压为 `Xray-WinOptimize` 目录，在该目录运行 PowerShell（替换 GitHub 用户名）：

```powershell
git init -b main
git add .
git commit -m "Initial experimental Windows TCP backend"
git remote add origin https://github.com/你的用户名/Xray-WinOptimize.git
git push -u origin main
```

如果 Git 提示作者身份未设置，使用自己的身份配置 `git config --global user.name "..."` 和 `git config --global user.email "..."`。首次推送可能需要浏览器或 Git 凭据管理器完成 GitHub 授权。**不要将密码、令牌、私钥或含服务器访问密钥的配置文件上传。**

## 2. 触发首次自动编译

在仓库 **Actions** 页面找到 **Xray-WinOptimize experimental build**，选择 **Run workflow**，`upstream_tag` **默认留空**，直接点击运行便会查询并构建 **上游最新已发布的 Release**（包含预发布版本，排除草稿）；仅当你主动输入具体标签（例如 `v26.10.21`）时才固定构建该版本。定时运行也会自动查询最新发布。自动选版改为用 `scripts/select_latest_release.py` 处理 GitHub API 的原始 JSON，并有 `tests/test_release_selector.py` 回归测试；异常响应会停止构建。成功后到该次运行页面的 **Artifacts** 下载实验构建包；**尚未完成全链路验证，构建通过不等于可用**。

工作流会每 6 小时检查 Xray 上游 Release，发现新标签时尝试拉取源码、严格打补丁、运行测试并编译 Windows x64；不兼容则应失败而不发布。定时检查并非上游发布即刻触发；GitHub 可能延迟调度。

## 3. 实验预发布

在完成 Windows 实机联网、VLESS/REALITY 和测速验证，核对项目与 WinDivert 许可证及对应声明之后，仓库所有者可在 **Settings → Secrets and variables → Actions → Variables** 增加 `ENABLE_EXPERIMENTAL_PRERELEASE=true`，启用符合条件的自动 Pre-release。此功能**默认关闭**。如遇 GitHub Token 写入权限限制，检查仓库 Actions 的 Workflow permissions 设置；工作流本身声明 `contents: write`。

**不要在验证前开启自动发布**；也不要把生成的实验二进制标为稳定版。WinDivert DLL/驱动不包含在构建包中，需从官方来源单独获取。

## 4. 维护与交接

每次改动实现、补丁、测试、CI 或安装过程时，更新 `docs/TECHNICAL.md` 的相关章节和第 13 节变更记录。推送前运行 `go test ./...`、`python -m unittest discover -s tests -v`，并让 GitHub Actions 检查通过。

## 5. 许可证与公开声明

公开仓库不等于自动赋予他人复制／再分发代码的许可。本项目目前**尚未选择单独的顶层开源许可证**。仓库所有者应审查 Xray-core 的 MPL-2.0、WinDivert 的许可选项与本项目代码组成后添加 `LICENSE` 和适用的第三方声明；如将修改版 Xray 二进制向公众分发，还须履行上游许可证要求。不要直接重新打包发布未经审查的 WinDivert 驱动。
