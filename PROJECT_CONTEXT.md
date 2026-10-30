# AgentRAG 项目上下文

本文件保存稳定项目事实与已核实的源码位置。强制规则见 `AGENTS.md`，任务及其生命周期见 `TASK_BACKLOG.md`。

## 项目目标与架构依据

AgentRAG 基于 WeKnora v0.9.0，目标运行形态是 WeKnora Lite：单空间、多知识库、单二进制核心、本地 SQLite（FTS5 + sqlite-vec）和内存任务执行器。当前仓库保留完整上游源码，包含标准版、Lite、桌面及其他客户端；“目标为 Lite”不表示仓库只有 Lite。

架构来源：`docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`，状态为 FROZEN。该文档描述目标架构，不等同于已经实现的功能。

目标能力包括文档生命周期、版本族及血缘 DAG、规范性／语义关联、MinerU 分路解析、反馈指导与修订发布、库级治理、mDNS 发现和可视化。

WeKnora 核心通过四类本机 HTTP Hook 与独立 Extension Host 交换公开 DTO：`before_retrieve`、`after_retrieve`、`before_generate`、`ingest_lifecycle_event`。两侧分别构建、分别运行；根 module 不依赖扩展 module，扩展不引用 WeKnora 内部类型。

## 上游基线

| 项 | 已核实值 |
|---|---|
| 基线记录 | `BASELINE.txt` |
| 上游仓库 | `https://github.com/Tencent/WeKnora` |
| 上游 tag / VERSION | `v0.9.0` / `0.9.0` |
| 基线提交 | `f17ee8a8da901bd658a4cb25f40b5fd41adbfdd7` |
| fork | `https://github.com/ybochuan/LanRAG-Next` |
| 基线记录分支 | `baseline/v0.9.0` |
| 基线冻结日期 | 2026-10-29 |

基线定位信息来自现有 `BASELINE.txt` 与 Git tag/commit 的交叉核对。

## 技术栈与模块

| 部分 | 技术／入口 |
|---|---|
| 上游后端 | Go，module `github.com/Tencent/WeKnora`，`go.mod` 声明 `go 1.26.0`；Gin、GORM、dig；入口 `cmd/server/` |
| Lite 存储／检索 | SQLite、FTS5、sqlite-vec；原生部分需要 CGO 和 C 编译器；Lite 构建 tag 为 `sqlite_fts5` |
| 任务执行 | 标准部署有 Asynq/Redis；Lite 使用 `internal/router/sync_task.go` 的内存执行器，包含重试和最终失败回调 |
| 前端 | Vue 3、TypeScript、Vite、Pinia、TDesign；`frontend/package.json`；上游前端 CI 使用 Node.js 24 和 npm |
| 文档处理 | `internal/infrastructure/docparser/`、Go chunker、可选 `docreader/`；包含 builtin、MinerU 等适配实现 |
| Agent／LLM | `internal/agent/`、`internal/models/`、`internal/modelcontext/`；原生 MCP 服务在 `internal/mcp/` |
| 独立扩展 | Go，module `github.com/ybochuan/LanRAG-Next/extensions`，声明 `go 1.26.0`；标准库HTTP、纯Go SQLite驱动 `modernc.org/sqlite v1.61.0`；独立进程入口 `extensions/cmd/extension-host/` |

## 目录职责

| 目录／文件 | 职责 |
|---|---|
| `cmd/server/`、`cmd/desktop/` | 上游服务和桌面入口 |
| `internal/router/`、`internal/handler/` | 路由、HTTP 处理、任务注册及执行入口 |
| `internal/application/service/` | 上游业务服务、问答编排、知识入库与后处理 |
| `internal/application/service/chat_pipeline/` | 普通 RAG 的检索、重排、合并、Top-K、提示拼接和生成插件；包名 `chatpipeline` |
| `internal/extensionbridge/client.go` | 用户授权的原生HTTP门卫：固定POST到127.0.0.1:8090/bridge/before-retrieve，500ms超时；非200/传输失败返回错误，不解析策略响应或修改检索范围 |
| `internal/application/repository/` | 上游数据库仓储和检索仓储 |
| `internal/agent/`、`internal/agent/tools/` | Agent 推理循环与工具实现 |
| `internal/infrastructure/`、`internal/models/`、`internal/types/` | 解析等基础设施、模型适配、内部类型与接口 |
| `frontend/`、`web/` | 前端源码及后端嵌入用 Web 资源 |
| `config/`、`migrations/`、`scripts/` | 上游配置、数据库迁移与构建／运维脚本 |
| `docreader/`、`client/`、`cli/`、`mcp-server/` | 文档读取组件、SDK、CLI 和 MCP 相关组件；部分有独立依赖清单 |
| `docs/design/` | AgentRAG 冻结架构来源 |
| `extensions/lifecycle/events.go` | 独立生命周期事件 DTO，包含身份、作用域、状态转换、原因、时间和来源阶段／尝试信息 |
| `extensions/lifecycle/store.go` | event/current/outbox原子追加、身份冲突拒绝、当前状态及有序事件读取；正式状态迁移政策属于M2 |
| `extensions/store/` | 独立SQLite、连接级配置、BEGIN IMMEDIATE单写者、只读连接池和嵌入式ext迁移 |
| `extensions/outbox/` | 因果投影队列、带token租约、失败退避/dead/人工重试、队列统计与Worker |
| `extensions/integration/` | 临时子进程退出后的真实SQLite恢复测试；不是上游端到端测试 |
| `extensions/bridge/server.go` | 标准库 HTTP 服务构造；健康探针 |
| `extensions/bridge/protocol.go` | Bridge major version 校验与固定错误响应 |
| `extensions/contract/retrieval.go`、`contracts/bridge/v1/` | 检索前公开DTO、严格JSON解码与共享样例；原生仅发送请求及检查HTTP状态，未消费策略收窄结果 |
| `extensions/bridge/retrieve.go` | M0默认Pass-Through原样保留非空原生授权候选，空范围403；可选固定tenant/KB策略handler保留集合收窄契约 |
| `extensions/weknora/client.go` | 绑定单一origin/tenant的公共HTTP客户端，API key在header、拒绝重定向、5秒请求超时和4MiB响应上限 |
| `extensions/ingest/reconcile.go`、`http_source.go`、`observer.go` | 周期完整分页校验、原生公共API列表适配和独立持久观察；不推断删除/中间事件，不写治理active |
| `extensions/taggov/writer.go`、`http_store.go` | 扩展共享标签单写者，原生公开GET/PUT适配，完整read/merge/replace/verify；null标签读取拒绝覆盖 |
| `extensions/taggov/mapping.go`、`projector.go` | 按tenant/KB持久映射原生tag ID；队列内读取当前治理truth，Outbox驱动投影，失败单独持久记录 |
| `extensions/retrieval/exclusions.go` | 从已枚举的授权文档中减去 excluded_document_ids，空结果显式 DenyAll |
| `extensions/host/config.go`、`runtime.go` | probe/worker配置验证；固定回环监听、独立DB、周期对账、限定tenant/KB的Outbox与唯一标签Writer的生命周期管理 |
| `extensions/README.md`、`.env.example` | Host运行说明和环境变量示例；程序只读取进程环境，不自动加载.env文件 |
| `extensions/scripts/`、`.github/workflows/extensions.yml` | 独立模块边界检查及其回归测试、扩展专用CI；开发检查需要Python 3.11+，Python不参与Host运行 |
| `extensions/scripts/e2e-test.sh`、`e2e_test.py` | WSL/Linux真实核心与真实Host E2E；只读备份源DB，在隔离副本经原生API认证/建测试资源，使用本地模型夹具，核对SSE和关联日志 |
| `extensions/cmd/extension-host/main.go` | Extension Host 启动入口 |
| `AGENTS.md` | 可执行工作规则 |
| `TASK_BACKLOG.md` | 已知待办的期望结果、依据与重审条件 |

## 运行与构建方式

- Extension Host：在 `extensions/` 执行 `go run -mod=readonly ./cmd/extension-host`，仅监听 `127.0.0.1:8090`。默认probe不打开DB或调用上游；显式worker模式要求独立DB路径、API origin/key、tenant及非空KB列表，启动周期对账和限定范围的Outbox。完整变量及默认值见 `extensions/README.md`。
- `GET /bridge/healthz`、`GET /healthz`返回200，只表示存活。Host默认检索前Pass-Through对合法非空文档范围返回200，格式错误400、空范围403；`GET /readyz`及其余三个业务Hook仍503，不代表全部业务就绪。
- Host先校验配置并占用固定端口，再初始化持久组件；端口占用不启动DB或投影。Interrupt/SIGTERM取消后台工作，HTTP排空后等待任务和Writer退出，再关闭DB；ShutdownTimeout只限定HTTP排空，不强制放弃事务。
- 扩展独立检查：在 `extensions/` 执行 `go build -mod=readonly ./...`、`go test -mod=readonly ./... -count=1`、`go vet -mod=readonly ./...`。根 module 的 `./...` 不遍历这个嵌套 module。
- 上游标准后端：`Makefile` 的 `make build` 构建 `./cmd/server`；`make dev-start`、`make dev-app`、`make dev-frontend` 是开发脚本入口，需要 Bash/make 和对应基础设施。
- 上游 Lite：`.env.lite.example` 描述 `DB_DRIVER=sqlite`、`RETRIEVE_DRIVER=sqlite`、本地文件和内存流管理。`make build-lite` 会先构建前端、更新 `web/`，再以 CGO 和 `sqlite_fts5` 构建；`make run-lite` 加载本地 `.env.lite` 后运行。该环境文件与真实凭据由本地运行环境提供。
- 上游 Docker：根目录 `docker-compose.yml`，README 中的 `docker compose up -d` 是标准部署入口；上游默认后端端口为 8080，与 Extension Host 的 8090 分开。
- 前端：在 `frontend/` 使用 `npm ci`、`npm run dev`；验证脚本为 `npm test`、`npm run type-check`、`npm run build`。

## 四类 Hook 的基线源码映射

以下是对 `f17ee8a` 的只读扫描结果，描述可注入的现有调用点，未修改上游。行号仅适用于该基线；方法名前的接收者区分同名函数。

| Hook 类型 | 文件相对路径 | 函数名／方法名 | 大致行号 | 所在包名 |
|---|---|---|---|---|
| before_retrieve：普通 RAG | `internal/application/service/chat_pipeline/search_parallel.go` | `(*PluginSearchParallel).OnEvent` | 90–120；分发检索任务前 | `chatpipeline` |
| before_retrieve：Agent | `internal/agent/tools/search_knowledge.go` | `(*SearchKnowledgeTool).Execute` | 189–256；`concurrentSearchByTargets` 前 | `tools` |
| after_retrieve：Rerank | `internal/application/service/chat_pipeline/rerank.go` | `(*PluginRerank).OnEvent` | 122–180 | `chatpipeline` |
| after_retrieve：普通 RAG 最终 Top-K | `internal/application/service/chat_pipeline/filter_top_k.go` | `(*PluginFilterTopK).OnEvent` | 60–77；截断后 `next()` 前 | `chatpipeline` |
| after_retrieve：Agent | `internal/agent/tools/search_knowledge.go` | `(*SearchKnowledgeTool).Execute` | 308–347；结果集完成至返回前 | `tools` |
| before_generate：普通 RAG 上下文完成 | `internal/application/service/chat_pipeline/into_chat_message.go` | `(*PluginIntoChatMessage).OnEvent` | 185–220 | `chatpipeline` |
| before_generate：普通 RAG 流式调用 | `internal/application/service/chat_pipeline/chat_completion_stream.go` | `(*PluginChatCompletionStream).OnEvent` | 73–101；`ChatStream` 前 | `chatpipeline` |
| before_generate：Agent 每轮入口 | `internal/agent/engine.go` | `(*AgentEngine).runReActIteration` | 942–959 | `agent` |
| before_generate：Agent 实际流式调用 | `internal/agent/think.go` | `(*AgentEngine).streamLLMToEventBus` | 60–70；编码／`ChatStream` 边界 | `agent` |
| ingest_lifecycle_event：任务入口 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).ProcessDocument` | 3754–3892 | `service` |
| ingest_lifecycle_event：解析完成 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).convert` | 4418–4457 | `service` |
| ingest_lifecycle_event：分块完成 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).ProcessDocument` | 4243–4287 | `service` |
| ingest_lifecycle_event：分块持久化 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).processChunks` | 690–709 | `service` |
| ingest_lifecycle_event：索引完成／失败 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).processChunks` | 786–832 | `service` |
| ingest_lifecycle_event：异步最终完成 | `internal/application/repository/knowledge.go` | `(*knowledgeRepository).FinalizeSubtask` / `finalizeSubtask`；`(*knowledgeRepository).CompleteProcessingWithoutSubtasks` | 679–750；786–798 | `repository` |
| ingest_lifecycle_event：解析失败／重试 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).convert`；`(*knowledgeService).failKnowledge` | 4423–4443；4534–4554 | `service` |
| ingest_lifecycle_event：解析超时 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).callDocReaderWithTimeout` | 4468–4493 | `service` |
| ingest_lifecycle_event：Lite 异常／重试耗尽 | `internal/router/sync_task.go` | `(*SyncTaskExecutor).Enqueue` | 172–212 | `router` |
| ingest_lifecycle_event：终态失败回调 | `internal/router/task.go` | `newDeadLetterKnowledgeFailer` | 387–450 | `router` |
| ingest_lifecycle_event：停滞恢复 | `internal/application/service/knowledge_housekeeping.go` | `(*HousekeepingService).recoverStalled` | 471–507 | `service` |

调用链上的重要事实：

- 普通 RAG 的编排见 `internal/application/service/session_knowledge_qa.go:221–233`：`CHUNK_SEARCH_PARALLEL → CHUNK_RERANK → CHUNK_MERGE → FILTER_TOP_K → INTO_CHAT_MESSAGE → CHAT_COMPLETION_STREAM`，另有按配置插入的节点和直接聊天分支。
- RAG 的 before_retrieve 位置在 `search_parallel.go` 意图跳过判断之后、克隆检索上下文及启动并发任务之前，才能同时约束 chunk/entity 两路。实际拒绝返回路径需在后续接入时核实，不能假设错误会自动阻止其他降级路径。
- Agent 检索先验证库作用域，再并发查询、Rerank、MMR、去重和 limit；`search_knowledge.go:332` 将最终结果格式化为 ToolResult，347 行返回给上层观察流程。after_retrieve 元数据增强适合在 332 行前处理，使输出与结构化数据一致。
- Agent 每轮经 `callLLMWithRetry`（`think.go:569`）清理消息、重试与流式调用；`streamLLMToEventBus` 是实际请求边界。业务指导注入需按会话／轮次去重，不能每次传输重试重复追加。
- 索引成功不等于整个摄取完成。`processChunks` 的 `finalizeIndexedKnowledgeState`（263–288、884–895）对没有文本／多模态后续工作的分支可直接完成；其余由 `KnowledgePostProcessService.Handle`（`knowledge_post_process.go:303–336`）进入无子任务完成或 `finalizing`，再由仓储原子计数晋升到 `completed`。
- `knowledge.go:313–358` 的阶段辅助函数及 `knowledge_span_tracker.go` 是 best-effort 进度追踪，不能替代不可丢失的 Bridge 事件投递。Lite 异常重试、最终失败回调和 housekeeping 的数据库更新也是单独的摄取信号来源。

## 当前骨架的能力边界

扩展模块包含事件DTO、协议端点、周期对账、标签单写者、文档排除计算、SQLite生命周期/Outbox，以及原生公共API适配和持久Observer/标签Handler组件。Host在worker模式初始化数据库并运行对账与投影，各KB共用唯一标签Writer，Outbox仅认领配置tenant/KB内的任务。RAG原生HTTP门卫与Host默认Pass-Through已接通并通过真实核心WSL E2E；对账/标签适配主要依据独立数据库和原生格式HTTP fixture，不能以本次E2E代替全部业务部署证明。复杂治理/策略响应消费、其他Hook及MCP仍属后续。

`PluginSearchParallel.OnEvent`保留原有无需检索的skip，在并发chunk/entity启动前调用本机Bridge。失败打印固定`[Bridge] before_retrieve failed, blocking search to prevent policy bypass`，返回`bridge_before_retrieve_failed`硬错误而非ErrSearchNothing，不走无结果fallback。Host默认启用M0 Pass-Through，合法非空候选200原样通过；Host不可达或错误状态仍阻断。HTTP200只代表本层准入，不代表治理过滤已完成。

原生认证与RBAC分开：关闭tenant RBAC不会关闭Auth；API需要Bearer JWT或有效X-API-Key。`/api/v1/auth/auto-setup`要求Lite edition及桌面专用token，普通CLI无匿名管理员入口。前端静态资源只在Lite edition或显式WEKNORA_WEB_DIR时挂载；二进制文件名不决定edition。显式ENV_FILE可加载已有配置，程序不把任意.env自动当作Lite配置。

Host对版本合法的检索前请求仅记录固定`bridge_before_retrieve_received`，不记录请求内容或凭据。runtime通过NewServerWithServices接入NewPassThroughRetrieveHandler，probe与worker均启用；原生权限仍由核心执行。单独调用未配置Services的NewServer仍拒绝业务，避免把库构造器当作默认放行策略。

四个POST端点均要求协议版本1：缺失／不支持／多值版本返回400；未配置的业务仍503。Host仅默认配置before-retrieve，不提前确认摄取成功或宣称生成前/检索后增强已就绪。

库调用方仍可通过NewRetrieveHandler配置固定scope策略。该模式保留allowlist、集合收窄及200+deny_all语义，未来消费者须解释结果。M0 Host默认透传保留非空原请求候选，不新增排除；因原生门卫只检查状态码，空候选直接403。两者共用严格字段/版本/1MiB/10000文档/5秒读取校验；省略/null文档范围不接受，whole-KB候选枚举和复杂范围转换仍按C-09后续实施。

持久层使用独立 `agentrag_ext.db`；`store.Open`拒绝含非ext用户表的已有数据库。writer连接使用WAL、synchronous=NORMAL、busy_timeout=5000、foreign_keys=ON、cache_size=-64000、wal_autocheckpoint=1000；每个reader物理连接单独配置并以mode=ro/query_only运行。事务取消后需要时重新预留writer连接，业务回调不会自动重放。

扩展schema v2增加`ext_ingest_observations/current`、`ext_kb_tag_mapping`和`ext_tag_sync_failures`；v1升级保持既有表数据。重复对账快照只刷新last_seen，源时间较旧拒绝覆盖、同时间不同parse_status报告冲突；source更新时间保留纳秒UTC。观察与生命周期事实分开存储。

生命周期存储只校验身份、scope及from_state，不把解析状态转换为治理active。event/current/outbox在一次提交中落库；current的last_event_id同时受存在性及tenant/KB/document组合FK约束。Outbox按因果事实去重并至少投递一次，租约token约束本地确认；实际投影Handler仍需幂等并以当前truth收敛，数据库租约不能约束远端HTTP副作用的先后顺序。

原生知识列表的分页位于顶层；标签列表的分页和数组位于`data`内。API key通过`X-API-Key`，执行租户通过`X-Tenant-ID`；公共API仍执行原生权限。规范gov标签需要预先在各KB通过原生UI/API创建，扩展不自动创建。跨页数据不是原子快照；回读校验不能替代跨系统CAS。

**C-10（Baseline Limitation，用户已接受）：** `internal/application/service/knowledge.go:604–613`加载标签失败仅记录日志；真正无标签也可能返回`tags:null`，公共响应不能区分两者。扩展对null/缺失数组安全拒绝并记录重试，显式数组才允许merge。无标签首写仍受限；后续通过独立标签只读API解决，本批没有修改原生API。安全拒绝不是同步成功。

## 已确认的 M0 实施取舍

用户在2026-10-29确认：

- 接受Lite内存队列的弱点，M0不修改原生数据库；通过周期对账补当前状态，不宣称完整恢复所有历史阶段。
- 扩展发起的标签投影经单一队列串行；该队列不约束原生UI/自动标签写入，跨系统并发覆盖仍是已知限制。
- Agent先做检索排除过滤，直接读、旧handle、图谱等完整治理留待后续机制；不能宣称覆盖所有工具。
- 独立解析代理放到后续阶段，不混入M0。

2026-10-30后续决定覆盖实施顺序：M0先采用原生HTTP门卫与默认Pass-Through，暂不启用复杂治理排除；前述过滤目标保留后续，不声称active、Agent直接读或图谱门禁已实现。当前默认透传要求显式非空文档范围，不能把未知/空范围当作whole-KB许可。

基线没有公开的 `excluded_document_ids` 搜索参数。底层 `internal/types/retriever.go:58` 虽有 ExcludeKnowledgeIDs，但 `internal/types/search.go` 的 SearchParams 未贯通，SQLite `repository.go:877–908` 也未应用该列表。扩展计算必须由薄适配变成显式文档范围/结果排除；空范围不能变成整库。具体差异、验证和残余风险见 `docs/design/M0-Change-Map-2026-10-29.md`。
