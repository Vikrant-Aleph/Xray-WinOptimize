## 改动目的 / Purpose

<!-- 问题、为什么需要修改，相关上游 Xray 版本。 -->

## 改动范围 / Changed files and behavior

<!-- 描述源文件与生成到 Xray 的目标文件、运行行为与影响范围。 -->

## 验证 / Verification

- [ ] `go test ./...`
- [ ] `python3 -m unittest discover -s tests -v`
- [ ] 如果更改上游补丁：在对应 Xray tag 上执行真实源码打补丁、gofmt、完整 Windows 构建
- [ ] 如果更改报文/网络行为：说明真实 Windows TCP/REALITY 测试结果（未做请明确写“未验证”）

## 技术文档 / Technical documentation

- [ ] 已同步更新 `docs/TECHNICAL.md` 对应章节
- [ ] 已在 `docs/TECHNICAL.md` §13 添加本次变更记录（涉及文件、行为、测试、风险、回退）
- [ ] 如果只调整 README 表述而不改变运行/安装行为，已在 PR 中说明无需技术记录

## 安全及回退 / Safety and rollback

<!-- 对 WinDivert、路由、端口、资源、兼容性和用户回退路径的影响。 -->
