# M2 原生后端失败重新复现记录

日期：2026-11-01。HEAD：`fa6353e8c821daa5eeb667dc46140e4d0d32f179`、`v0.9.0-2-gfa6353e`及既有工作区补丁。

本轮只读复现，没有修改原生实现、测试或依赖来消除失败。扩展来源账本的完成不代表原生完整门禁已通过。

仓库根目录实际命令：

```powershell
wsl -d Ubuntu --cd /mnt/d/LanRAG-Next --exec /usr/bin/env PATH=/usr/local/go/bin:/usr/bin:/bin CGO_ENABLED=1 go test -mod=readonly -tags sqlite_fts5 -json ./internal/agent/tools ./internal/application/service -count=1
```

退出状态：**1**。完整JSON日志记录5502个pass事件、3个skip事件；49个顶层失败，包含子测试共52个fail测试事件。pass事件包含Go测试父/子项及包结果，不等同于5502个独立用例。

| 分组 | 顶层失败数 | 代表测试与下一步 |
|---|---:|---|
| Python依赖恢复 | 1 | `TestSkillPythonPackageRecoveryInstallsWithoutPip`；检查隔离Python恢复环境/依赖来源，不能删除恢复断言 |
| DataSource替换 | 1 | `TestDataSourceReplacementCommitsBeforeEnqueueAndCleanup`；核对持久提交、排队及清理顺序，先确认来源再修复 |
| MCP更新 | 1 | `TestUpdateMCPService_AppliesNonScalarUpdateWithoutName`；检查fixture/验证契约与当前原生行为 |
| Sandbox配置/模板 | 46 | `TestQueryTemplatesEnsuresMissingWeKnoraTemplate`、`TestUpdateIdentityChangeCordonsThenWrites`、`TestDeleteConfigSealsCreatesOnTheResolvedManager`等；逐组识别共享前置校验与行为断言，不将所有失败笼统归环境 |

私有原始记录：忽略目录`tmp/m2-native-baseline-current.jsonl`，不将可能带配置/请求的原始终端日志公开到文档。

历史治理修复批次用overlay撤销当时修改，仅确认三个代表失败仍在；不能推广为这49项均是上游基线。本次没有对每个失败执行原始版本对照，也未修复上述问题，因此`M2-BASELINE-CHECKS`继续待归因修复。没有执行上游全仓`go test ./...`、新的Lite构建或生产E2E，不能称原生整体通过。
