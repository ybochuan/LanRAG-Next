# T4 原生模型输入接线（2026-11-02）

状态：有限原生文本主链已接线；T4/M2继续进行，文件生产者/收集/已封存副本正向读取未完成。基点fa6353e / v0.9.0-2-gfa6353e，保留已有改动，未提交、推送、替换生产服务或迁移用户库。

## 行为与边界

- `session_knowledge_qa.go`、`session_agent_qa.go`在原授权targets、owner和服务端assistant message身份确定后建run；不接受浏览器或工具参数提供run能力。不支持的跨KB/缺身份运行不签来源。
- `extensionbridge/source_run.go`只累计请求内传输指纹，不持有许可事实。模型实际编码后的消息/选项hash，以及解码前响应/分片hash经既有回环500ms接口登记；Host保留所有状态、核验、事务和审计。未知组成必须先取得Host的不可封存ACK，不能把连接错误当成该ACK。
- `chat_completion{,_stream}.go`、`agent/think.go`、`agent/act.go`登记各轮实际调用、独立工具步骤和终态。流启动函数返回不封存；取消/异常不能登记success。结束时来源不足的运行中止，不遗留可继续补证的开放运行。
- 纯文本content、title/description/file_name等，以及`read_document`实际行/元数据通过完整原生root核验。检索/合并/分块、模型编码和工具调度算法保持原样。
- 未适配查询改写、重排、FAQ/视觉/合并或formatted custom metadata、listing updated_at仍保持unattributed。原active QA在此情况下可继续，但不能据此生成可读副本证明。
- 外部MCP服务摘要/工具schema/pin描述在模型加载前省略，内置知识工具保留；不改ToolRegistry或删除MCP配置。自动memory/skill、未知文件/缓存、shell等此前拒绝保持。
- write/edit尚未绑定实际producer，write步骤明确不可封存；文件原件保留。人工下载、旧grant和T5持续撤销不在本批范围。

## 本批验证

`extensions/scripts/governance_e2e.py`使用临时强管理凭据、临时账号/KB、独立SQLite、真实候选Core/worker Host及确定性模型；所有LLM请求与重排输入保留独立标记。没有使用用户账号、云端密钥或真实文档。

完整输出：`docs/evidence/m2-governance-t4-regression-2026-11-02.log`。

- 旧候选二进制：active问答通过，但新断言检测不到run/step事实，退出1；重编译后同断言通过。
- 新客户端/字段/流测试：实际请求与响应hash、零原文传给Host、无可信handle不能发明来源、未登记step拒绝、未知不能洗成success、流未结束不封存、取消中止、证明503时模型启动次数0。
- MCP元数据spy先RED：未知服务标题/描述仍被加载；薄拒绝后spy调用次数0，测试退出0。
- 真实E2E先发现第8个已结束Agent运行仍open，增加终态中止后，所有已结束请求open计数0。
- 最终真实联合E2E退出0：原生运行和实际模型步骤及字段核验记录落库，至少一个实际Agent执行sealed；不支持组成保持unattributed；FK干净。真实annotations→启动扫描stale后，同会话/已知ID均无正文/描述/旧答案进入任何模型请求；断Host答案生成次数0。

构建和包级检查的完整命令、退出状态保存在忽略目录`tmp/t4-20261102-*`的`.log`及`.exit`。没有以E2E夹具冒充真实云模型/文件生成验收。

### 最终构建与回归

Go在WSL Ubuntu运行；需要监听的检查在独立用户/网络空间启用lo，避免占用生产8090。WSL提示mirrored网络失败并回落None，本轮没有重启WSL或调整系统网络；这不构成真实Windows模型网络验收。

| 命令／目录 | 退出状态与覆盖 |
|---|---|
| `CGO_ENABLED=0 go build -mod=readonly ./...`，extensions | 0 |
| `go vet -mod=readonly ./...`，extensions | 0 |
| `go test -mod=readonly -p 1 ./... -count=1`，extensions | 0，完整结束；无测试文件的命令包不称已有覆盖 |
| `go test -mod=readonly -race -p 1 ./... -count=1`，extensions | 0，完整结束 |
| `python3 scripts/check_module_boundary.py`，extensions | 0；292 imports，无原生internal耦合／workspace |
| `python3 -m unittest discover -s scripts -p '*_test.py' -v`，extensions | 0；33项，31通过、2个opt-in跳过 |
| `CGO_ENABLED=1 go test -mod=readonly -tags=sqlite_fts5 -p 1 ./internal/extensionbridge ./internal/application/service/chat_pipeline ./internal/agent/tools ./internal/agent ./internal/application/service -count=1`，根目录 | 0；五包完整通过，最后service 87.926s |
| `CGO_ENABLED=1 go test -mod=readonly -tags=sqlite_fts5 ./internal/handler ./internal/extensionbridge -count=1`，根目录 | 0 |
| `CGO_ENABLED=1 go build -mod=readonly -tags=lite,sqlite_fts5 -o tmp/WeKnora-lite-t4-check ./cmd/server`，根目录 | 0；仅候选二进制，不替换运行服务／前端 |
| `CORE_BINARY=tmp/WeKnora-lite-t4-check python3 extensions/scripts/governance_e2e.py`，根目录 | 0；实际执行使用该候选的绝对路径及隔离网络，完整输出见联合E2E日志 |
| `CGO_ENABLED=1 go test -mod=readonly -tags=sqlite_fts5 -race -p 1 ./internal/agent/tools ./internal/agent ./internal/application/service ./internal/application/service/chat_pipeline ./internal/handler ./internal/extensionbridge -count=1`，根目录 | **1**；service的5个既有工作区测试数据竞态，其余5包通过。该命令启动早于本批最后的终态/MCP小补丁，不能当作最终源码完整race通过证据 |
| `CGO_ENABLED=1 go test -mod=readonly -tags=sqlite_fts5 -race -p 1 ./internal/extensionbridge ./internal/application/service/chat_pipeline ./internal/agent/tools ./internal/agent ./internal/application/service -run 'Test(Source\|StreamingSource\|DocumentSource\|SearchSource\|AgentSource)' -count=1`，根目录 | 0；最终源码中本批来源/流/字段/MCP元数据测试，五包均实际执行。表格中的转义竖线展示正则分支，不是Shell转义；完整原始命令见`tmp/t4-20261102-native-source-race.log` |
| `gofmt -l .`，extensions；本批19个原生Go文件的`gofmt -l` | 0且无输出 |
| 本批范围`git diff --check` | 0；全局检查仍为2，既有frontend菜单／i18n／router／store行尾空白，本批未改 |

普通扩展并行全量仍保留退出1记录：`modelsupervisor/TestObservePortsReportsOnlyListening`观测到额外临时端口；串行隔离全量与race退出0，不抹去原失败。

完整原生race失败测试为：`TestTenantAPIKeyServiceAuthenticateThrottlesLastUsedUpdates`、`TestQueryTemplatesEnsureAndReplaceDoNotShareSingleflight`、`TestRegisterCatalogDoesNotMoveTheDefinitionWhenPinFails`、`TestUpgradeKeepsThePreviousVersionServingWhileItRuns`、`TestFailedUpgradeLeavesThePreviousVersionServing`。栈指向`fakeTenantAPIKeyRepo`、`fakeConfigRepo`、`installSkillRepo`、`installConfigRepo`的并发读写。本批未修改这些路径，也未在干净基线上独立复现，列为既有工作区失败，不擅自宣称上游基线缺陷。

根go.mod/go.sum与开始时SHA256一致；007、008迁移内容一致。扩展`go env GOMOD GOWORK`返回独立module路径和空workspace。没有运行根仓库全量Go测试、前端测试／类型检查／构建、真实云模型、实际文件副本E2E或T4最终整批审查。

## 后续

按批准表继续T4-04：真实写入绑定原run/step/path/最终bytes和旧来源；collector只查询原消息对应生产者，不能扫描认领旧文件。之后验证已封存active文件跨轮/缓存读取、停用与版本变化拒绝、重启和未知ACK不重放，再做T4整批复核。T4/M2不提前标完成。
