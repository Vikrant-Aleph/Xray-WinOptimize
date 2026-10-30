# GitHub 发布与更新（2026-10-30：Xray-WinOptimize 默认构建）

**建议的 GitHub About 描述：** Windows 平台的 Xray-core TCP 性能优化项目，借鉴 Linux 接收缓冲区自动调谐的设计思路，旨在改善高延迟、拥堵网络下的代理性能。

仓库：[Vikrant-Aleph/Xray-WinOptimize](https://github.com/Vikrant-Aleph/Xray-WinOptimize)。将完整包里的 `Xray-WinOptimize/` **除 `.git` 之外**覆盖原工作目录，保留 `.github`；检查 `git status`，执行 `git add -A`、`git commit -m "Improve Windows TCP receive performance"`、`git push origin main`。

**默认 Actions：** `Xray-WinOptimize (latest upstream)`，手动版本留空即跟随上游最新 Release（包含预发布），每 6 小时自动检查。成功后从 Artifacts 下载 `xray-winopt-v...`，不需要 WinDivert 或管理员权限。实验 WinDivert 后端转移到 `Xray-WinOptimize experimental WinDivert backend` 手动工作流；它不是普通用户下载入口。

自动公开预发布默认关闭。等完整编译+Windows 真实联网+性能验证通过后，再设置 GitHub Actions 仓库变量 `ENABLE_WINOPT_PRERELEASE=true`。此变量只对成功构建后的未来运行生效。源码与技术文档必须在同一次提交更新，`docs-check` CI 会检查。

---

## 2026-10-30 — 每隔一天自动发布五种 Windows 核心（当前流程）

`.github/workflows/upstream-release.yml` 是唯一的正式 Xray-WinOptimize 发布工作流：

1. **定时检查**：UTC `04:37`，cron `37 4 */2 * *`（按月份日期每隔一天，月初交界可能不严格间隔 48 小时；GitHub 也可能延迟任务）。可手动触发；`upstream_tag` 留空时选上游最新发布，**包含预发布但排除草稿**。
2. **防重复**：读取项目同名 GitHub Release 的资产。只有 `win7-32`、`win7-64`、`win-32`、`win-64`、`win-arm-64` 这 **5 个 ZIP 全部存在且大小非零** 才跳过。之前 Release 不存在或部分资产缺失时构建 5 个完整目标。手动触发时可勾选 `force_rebuild`，让已有完整 Release 重新构建并以新格式覆盖五个 ZIP。
3. **补丁校验**：针对该上游精确 tag 克隆源码；运行 Python、Go 单测，并严格匹配补丁锚点；不兼容时终止构建。
4. **矩阵编译**：所有目标使用 `GOOS=windows`、`CGO_ENABLED=0`；`GOARCH` 分别为 `386`、`amd64`、`arm64`。Windows 7 两个目标使用 [XTLS/go-win7](https://github.com/XTLS/go-win7) 已补丁工具链，沿用 Xray 官方 Windows 7 构建流程；**绝不能**以标准 Go 构建的二进制冒充 Windows 7 版本。若 patched Go 版本落后于 Xray 所需 Go 版本，该目标会报错并中止发布，而不是自动切换回标准 Go。
5. **资产校验**：每个 `Xray-winopt-<target>.zip` **仅含** `xray.exe`、`LICENSE` 和 `BUILD-INFO.txt` 三个文件，不附带中文/英文 README 或技术文档；说明文档保留在仓库。Python 打包器验证 PE Machine，发布阶段校验五个 ZIP 的**精确文件清单**和架构，生成 `SHA256SUMS.txt`。
6. **原子化公开**：所有矩阵任务成功后，先创建 Draft Release 并上传完整资产，最后再公开为 Pre-release。现有缺文件的 Release 只有在全部构建成功后才尝试修复。任何编译／检查失败都不创建新的公开 Release。

ZIP 内部文件固定为 `xray.exe`、`LICENSE`、`BUILD-INFO.txt`。若发现额外的 `README-project.zh-CN.md`、`README-project.en.md` 或 `TECHNICAL.md`，发布验证会失败。

正式文件名如下：

```text
Xray-winopt-win7-32.zip
Xray-winopt-win7-64.zip
Xray-winopt-win-32.zip
Xray-winopt-win-64.zip
Xray-winopt-win-arm-64.zip
SHA256SUMS.txt
```

GitHub 自动发布要求仓库 **Settings → Actions → General → Workflow permissions** 允许 `GITHUB_TOKEN` 写入仓库内容。workflow 已声明 `contents: write`。当前自动公开的是 **Pre-release**（实验性），并非稳定版。此流程替代历史的 `ENABLE_WINOPT_PRERELEASE` 变量，更新后不再需要该开关。

**重要兼容性警告：** 当前优化控制器依赖 `SIO_TCP_INFO`；Windows 7 不保证提供这个接口，虽然使用 patched Go 能生成 Win7 二进制，但 **Win7 自适应接收窗口可能不生效**，而且没有实机联网验证。与 Windows 10/11 的真实运行能力应分别验证。Win7 最新补丁工具链也可能落后上游 Xray 所需版本，此时五平台整套发布会暂停，不能发布不兼容版本。

**已经发布的旧包不会因修改工作流自动改变。** 如需替换同一 Xray 版本的 ZIP，请手动运行 `Xray-WinOptimize (latest upstream)`，填写对应 `upstream_tag`（或留空选择最新），并勾选 `force_rebuild`。只有 5 平台重新编译且通过校验后才更新资产。

**回退方式：** 回退 `.github/workflows/upstream-release.yml`、新增 `scripts/*release*.py`、`scripts/package_windows.py` 与文档改动。已经公开的历史 Release 不会自动删除。不要把二进制重命名为 `win7` 来绕过编译失败。

---

## 旧版独立 TCP 栈发布流程（历史备份）

# Xray-WinOptimize — GitHub 公开发布与持续更新

## 1. 公开发布源码

在 https://github.com/new 创建名为 `Xray-WinOptimize` 的 **Public** 仓库。不要勾选生成 README、.gitignore 或许可证模板（先审查下述许可证问题）。

**仓库简介（适用于当前默认实现）：** Windows 平台的 Xray-core TCP 性能优化项目，借鉴 Linux 接收缓冲区自动调谐的设计思路，旨在改善高延迟、拥堵网络下的代理性能。

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
