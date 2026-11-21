# 问答流程修复与隔离验收

工作区 `D:\LanRAG-Next`，变更基于 `de6f69a`。Core、Extension Host 和静态前端均使用隔离环境验收。SQLite 使用新建临时库，模型输入来自合成报销制度。生产服务、原有配置和原始验收证据未改动。

| 候选产物 | SHA-256 |
| --- | --- |
| `tmp/qa-hardening-core-release` | `2bdc4b1a58cd8fb4f0e229e7b94bc2d27368dc271fe4fc5851484c4c9b196d42` |
| `tmp/qa-hardening-host-ready` | `bc14d429116bd628bdf3a81a42c19d512d51722a692c9c0ce65a0fcdff8c360b` |
| `frontend/dist/index.html` | `ed3086a38d7101d1f1d2d4838b03365d498ce7fa4fecd5f0aa0ec3d395c7e4cd` |

## 实施内容

| 项 | 行为 |
| --- | --- |
| 01 期限证据 | 有适用勘误时先给唯一现行口径。无适用勘误的已核验单库问答也明确约束起算日、工作日历和顺延外推。验收分别检查现行口径、无依据日历推算、实际送模内容与引用。 |
| 02 勘误范围 | 审核员可指定完整项目名。反向候选和发给模型的勘误共用项目检查，使用原问题识别项目。项目名随勘误证据、详情及 Markdown 保存。已命中原文但未确认项目时提示补充。历史无项目字段的条目沿用原有适用条件与来源核验。 |
| 03 并存与替代 | 同分块不同口径须显式替代或确认已有条目的 ID 和修订号。激活事务重新核对已有生效条目，冷静期内出现未核对条目时阻止激活。 |
| 04 局部失败 | 保留 `before_generate` fail-open 和 `before_retrieve` fail-closed。答案事件及历史记录区分勘误暂未核验、已核验无匹配、已应用和库级关闭。 |
| 05 支持范围 | 候选按单库纯文本试跑。Quick 的多库原生检索沿用现有授权并提示范围限制。Agent 需要取证时提示选择一个知识库。 |
| 06 Agent 取证 | 显式知识范围及内置智能推理的有效范围直接触发前置授权检索。保留问候、已授权归档和已核验追问的路径。 |
| 07 多轮证据 | 夹具覆盖应用先检索、模型自主追加搜索、两份本轮工具结果进入最终模型请求，以及原文和勘误引用的一致性。 |
| 08 写队列 | 在 WSL 本地临时库使用真实 Markdown 投影测量。空载和持续投影各 200 次写入。写队列加事务开启的 p95 分别为 0.174 ms 和 10.058 ms，投影时最大 15.972 ms。本轮保留事务边界，数据仅代表该夹具。 |
| 09 隔离发布与回退 | 复用同一 Core 和 Host 候选，验证单库开启、关闭、恢复、撤回不回流、双账号 ACL 和两条正式发布路径。前端使用 `frontend/dist` 作为隔离 `WEKNORA_WEB_DIR`。 |

## 验收证据

- 前端 `npm run type-check`、`npm run build` 均退出 0。使用 Git 自带 Bash 执行 `npm test`，2462 项通过，2 项跳过。见[前端构建](../../tmp/qa-hardening-frontend-build-final.log)及[前端测试](../../tmp/qa-hardening-frontend-test-final.log)。
- 隔离 Chrome 核对授权范围、键盘与焦点、版本引用、Markdown 下载和项目状态刷新，退出 0。扩展的旧浏览器夹具在手工修订提案列表刷新处超时。真实手工与二进制发布路径单独验证。见[勘误工作台浏览器检查](../../tmp/qa-hardening-browser-scoped.log)及[旧完整浏览器夹具](../../tmp/qa-hardening-browser.log)。
- WSL 核心定向测试覆盖 `extensionbridge`、`agent`、`agent/tools`、`chat_pipeline`、`service` 和 `handler/session`，退出 0。核心候选使用 `go build -mod=readonly -tags lite,sqlite_fts5 -o tmp/qa-hardening-core-release ./cmd/server`，扩展 Host 使用扩展模块独立构建。
- 真实治理 E2E 分别记录[单库与回退](../../tmp/qa-hardening-ready-full.log)、[手工发布](../../tmp/qa-hardening-ready-manual.log)、[二进制发布](../../tmp/qa-hardening-ready-binary.log)。本地夹具负责稳定重现检索、工具调用和撤销边界，云端记录负责核对真实生成。
- 期限对照的旧错误样例保留在[第一轮 DeepSeek 结果](../../tmp/feedback-materials-deepseek-5ytcljof/results.json)和[第二轮 DeepSeek 结果](../../tmp/feedback-materials-deepseek-a4umcihn/results.json)。第二轮规则通过活跃勘误与真实自主工具调用检查，随后从未激活对照答案发现原文起算仍会外推，最终候选增加原文期限约束并复跑。
- 最终 DeepSeek 候选运行完成 15 个合成场景、20 次模型请求，原始进程因期限判定器误报退出 1。三条误报分别是逐字重述「活动结束后」和明确表示「没有写明具体哪天起算」。修正判定器并增加正反例后，通过 `python -B extensions/scripts/check_feedback_deadlines.py tmp/feedback-materials-deepseek-uyxqxrob/results.json --include-fixture-checks` 对同一请求与答案证据重判，156 项均通过，退出 0。真实供应商发出了额外工具调用，最终引用仍绑定本轮可用原文。两次搜索均返回可用结果的路径在本地治理 E2E 另有稳定夹具。见[原始结果](../../tmp/feedback-materials-deepseek-uyxqxrob/results.json)及[独立重判](../../tmp/qa-hardening-deepseek-regrade.json)。
- 扩展模块边界门禁检查 332 个 imports，退出 0。`go build -mod=readonly ./...`、`go vet -mod=readonly ./...` 和 `go test -mod=readonly ./... -count=1` 退出 0。Python 脚本测试执行 51 项，3 项按各自条件跳过，退出 0。见[扩展单测](../../tmp/qa-hardening-extension-tests-ready.log)和[Python 脚本测试](../../tmp/qa-hardening-python-ready.log)。
- Linux 最终 Go 源码快照附带同版本 Bridge 契约文件，执行 `go test -mod=readonly -race -p=2 ./... -count=1`，全部测试包退出 0。降低包级并发复验了高负载首跑中出现的计时失败，验收断言保持原样。见[最终全量 race 日志](../../tmp/qa-hardening-extension-race-all.log)。
