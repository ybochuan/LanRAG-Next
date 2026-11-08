# P2-E 性能监控只读审计（2026-11-07）

## 1. 审计范围与结论

本轮只读审计基于 HEAD `5dd40b9`，未修改 Core、Host、SQLite、前端或其他源码。审计对象是当前仓库已有的性能可观测能力，以及 P2-A/P2-B 已记录的生产症状：

- Core API 请求量、P50/P95、错误率；
- SQLite 慢查询、Busy/Locked、事务耗时与连接池等待；
- RAG 检索、Rerank、上下文组装耗时；
- Agent 工具调用次数、失败率、单轮耗时；
- Host 对账失败/恢复次数与 Outbox 积压。

结论：现有能力是“请求日志 + 慢 SQL 日志 + 可选 Langfuse trace + 零散耗时字段 + 部分管理快照”，足以做离线抽样和故障排查，但还不能直接形成实时 P50/P95、错误率、SQLite 锁等待、RAG 阶段分位、Agent 工具失败率和 Outbox 趋势。P2-E 不应先引入一套完整 Prometheus/Grafana 体系，而应先冻结低基数指标契约，优先复用现有日志、`outbox.Store.Stats` 和管理快照，再决定最小埋点范围。

## 2. 现有可观测能力清单

### 2.1 Core HTTP API

- `internal/router/router.go:142` 注册 `middleware.RequestID()`，`:144` 注册 `middleware.Logger()`；`:149-151` 只有 `GET /health` 返回 `{"status":"ok"}`，没有专用 `/metrics` 路由。
- `internal/middleware/logger.go:147-175`：`RequestID()` 读取或生成 `X-Request-ID`，写入响应头、请求上下文和日志上下文。
- `internal/middleware/logger.go:179-289`：`Logger()` 记录 `request_id`、`method`、`path`、`status_code`、`size`、`latency`（`time.Since(start).String()`）和 `client_ip`；`status_code>=500` 用 Error 级别，`>=400` 用 Warn 级别。`/assets/` 和 wiki stats 路径跳过日志（`:185-189`）。
- 日志会读取并记录最多 10KB 的 JSON/表单/文本请求体（`maxBodySize = 1024 * 10`，`:20`；`readRequestBody` `:99-144`），也会捕获最多 10KB 的响应体（`loggerResponseBodyWriter` `:32-50`；响应体处理 `:236-275`）。敏感字段通过 `sanitizeBody` 和 `sanitizeQuery` 脱敏，但响应体仍可能包含业务数据；这属于现有日志机制的重要隐私和容量边界，不应作为指标正文来源。
- 现有日志格式为文本 key=value；`internal/logger/logger.go:68-208` 的 `CustomFormatter` 支持 `WEKNORA_LOG_FORMAT` 模板，但没有结构化 JSON 或指标导出器。
- 已有的管理快照：`internal/handler/system.go:1906-1915` 的 `GetRuntimeQueues`，路由在 `internal/router/routes_auth_tenant.go:336-338`；响应类型见 `internal/handler/system.go:1841-1854`，包含队列深度、worker pool 并发/活跃数、模型限流状态和 `Timestamp`。它是系统管理员权限的实时快照，不是历史指标，也不能替代 Core API 延迟统计。

### 2.2 SQLite 与 Core 连接池

- Core DSN 在 `internal/container/container.go:1053`：`dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"`。
- `internal/container/container.go:1141-1146`：注释说明 SQLite 即使 WAL 也只支持一个并发写者，随后 `sqlDB.SetMaxOpenConns(1)`，`SetMaxIdleConns(10)`。P2-B 已确认当前业务并发下 P95 仍可接受，但数据库等待和查询耗时没有独立指标可区分。
- `internal/container/gorm_logger.go:11-26`：GORM `SlowThreshold=200ms`，Warn 级别，`ParameterizedQueries: true`（不记录绑定值）。只能得到超过 200ms 的慢 SQL 文本和耗时，没有 Busy/Locked 计数、事务持续时间、`sql.DBStats` 连接池状态或按操作分类的直方图。
- P2-B 证据 `docs/evidence/P2-B-sqlite-pool-wal-2026-11-07.md` 记录：16 并发 `knowledge_list` P95 75.51ms、全部 200；ext4 写压测期间隔离 Core 读 P95 13.5–14.2ms、无 Busy/Locked。该结果说明当前未出现锁错误，不代表以后不会出现；这正是 P2-E 需要持续观测的原因。

### 2.3 RAG 检索与重排

- `internal/application/service/session_knowledge_qa.go:764-829` 在 chat pipeline 的每个阶段记录 `duration_ms`，并在 `all_stages_complete` 记录 `total_duration_ms`；日志辅助函数在 `internal/common/tools.go:253-266`，输出仍为文本 `stage=... action=... duration_ms=...`。
- 这些阶段日志只覆盖 chat pipeline；`POST /api/v1/knowledge-search` 的 handler（`internal/handler/session/qa.go:903-1003`）只记录“开始/完成/结果数”，没有耗时字段；`internal/types/rerank.go:107-116` 的 `RetrievalMeta` 只包含 rerank 诊断，不是延迟指标。
- 模型层有零散的 debug 日志，例如 `internal/models/embedding/llm_debug.go`、`internal/models/chat/llm_debug_wrapper.go` 的 `time.Since` 调用，但通常受 debug 配置控制，不能作为稳定业务指标。
- Langfuse 的 RAG/重排 span 可以在启用 `LANGFUSE_*` 时查看，但它是 trace 数据，不是聚合指标；`internal/tracing/langfuse/middleware.go` 只追踪 LLM 相关路径，默认不覆盖普通列表/管理请求。

### 2.4 Agent 工具调用

- `internal/handler/session/agent_stream_handler.go:269-347`：`handleToolCall`/`handleToolResult` 用 `eventStartTimes` 计算工具执行耗时；SSE `tool_result` metadata 带 `tool_name`、`success`、`error`、`duration_ms`、`tool_call_id`。
- `internal/handler/session/agent_stream_handler.go:1027-1028` 记录 fallback answer 的 `total_steps`/`total_duration_ms`；`:1062-1065` 的完成事件带有 `total_steps`、`total_duration_ms`、`final_content`。
- `internal/types/agent.go:421-430` 的 `ToolCall` 有 `Duration` 字段（毫秒）；`internal/types/message.go:521-522` 的 `AgentDurationMs`、`internal/types/message_suggestion.go:88` 的 `LatencyMs` 是消息/建议字段。
- 这些数据能支持单次排障，但没有全局工具调用次数、失败率、按工具名的耗时分布或单轮耗时分布。

### 2.5 Host 对账与 Outbox

- Host 的启动/停止和后台服务生命周期在 `extensions/host/runtime.go`；`Report` 在 `extensions/host/runtime.go:140` 只接收并输出 `err.Error()`，`extensions/cmd/extension-host/main.go:18` 最终只执行 `log.Print(code)`。
- P2-A 已形成安全事件：`reconciliation_waiting_for_core`、`reconciliation_recovered`、兼容事件 `reconciliation_failed`，并记录 `code=...`、`consecutive=...`、`next_retry=...`，见 `data/launcher-run-lY5BJT/host.log` 的启动和恢复记录。它们适合做事件计数，但不是 Prometheus 指标。
- `extensions/outbox/store.go:272-295` 已有 `Store.Stats`：按 scope 返回 `pending`、`processing`、`dead` 数量和 `OldestPendingAge`；但当前没有 HTTP 端点或周期性日志输出。P2-A 审计记录的 140 条全部 `dead`、`retry_count=5` 的 Outbox 积压只能通过直接查询该表或复用 `Stats` 才能看到。
- Host 健康端点只有 `/healthz`、`/readyz`、`/bridge/healthz`（`extensions/bridge/server.go:53-66`），不包含业务指标。
- `extensions/host/runtime.go:248-256` 启动 reconciler、outbox worker、依赖扫描和每日 review 扫描；这些后台任务的失败通知是事件字符串，不是计数器。

### 2.6 Tracing 与其它

- Langfuse exporter 在 `internal/tracing/langfuse/exporter.go:13-38`，使用 OTLP/HTTP；`internal/router/router.go:223-225` 注册 `langfuse.GinMiddleware()`。只有配置 `LANGFUSE_*` 才启用；它的强项是单次 RAG/LLM 调用链追踪，不提供业务级 P50/P95 聚合。
- `go.mod` 中已有 `go.opentelemetry.io/otel` 和间接的 `github.com/prometheus/client_golang` 依赖，但没有直接的 `/metrics` endpoint、Prometheus registry 或 exporter 代码；不能因为间接依赖存在就认为已经有指标能力。
- 现有日志中的 `latency` 是 `time.Duration.String()`（如 `14.910776ms`），不是数值字段；`path` 带原始 URL 路径和查询串，直接做聚合会产生高基数，需要先按 Gin 的 `c.FullPath()` 归一化 route，并丢弃 query。

### 2.7 一次性只读日志基线（2026-11-07）

为验证日志字段是否足以支撑离线基线，对 `data/launcher-run-lY5BJT/core.log` 做了一次只读抽样（不是 P2-E-01 的正式产物，也不是压测结果）：

- 文件 87,939,048 bytes；可解析请求行 10,479 条；时间范围 2026-11-07 16:55:47.735–20:58:51.100。
- 方法分布 GET 10,459、POST 20；status 全部 200；整体延迟 P50 7.234454ms、P95 30.578833ms、P99 77.541521ms、max 19,770.898576ms；>200ms 42 条、>1s 26 条。
- 平均日志行 5,725.5 bytes、最大 10,484 bytes；主要路由：`/api/v1/knowledge-bases/:id/knowledge` 7,177、`/health` 2,620、`/api/v1/knowledge-bases` 142、`/api/v1/me/browser` 76、`/api/v1/knowledge-search` 20。
- 日志请求行包含 `client_ip`、`latency`、`method`、`path`、`response_body`、`size`、`status_code`；`latency` 是 `time.Duration.String()`，不能直接作为数值指标；`response_body` 也说明不能把正文写入指标或长期日志聚合。

该抽样只用于确认日志字段、数据量和解析难点，P2-E-01 应使用固定时间窗口、统一 route 归一化和脱敏后的输出重新生成可复现基线。

## 3. 建议的最小指标契约

以下契约已在 P2-E-02 实现 P0 部分（Core API、SQLite、Host）；RAG/Agent 仍属 P1，优先固定字段和基数，不先引入完整监控平台。

| 维度 | 建议指标 | 类型/标签 | 建议采集点 |
|---|---|---|---|
| Core API | `core_http_requests_total` | counter `{method,route,status_class}` | `internal/middleware` 响应结束后；route 用 `c.FullPath()` |
| Core API | `core_http_request_duration_seconds` | histogram `{method,route}`，固定桶（50ms/100ms/250ms/500ms/1s/2.5s/5s/10s/30s/60s） | 同上 |
| SQLite | `sqlite_slow_queries_total` | counter `{operation}` | GORM slow-query callback；operation 为固定分类（select/insert/update/delete/other） |
| SQLite | `sqlite_busy_total` / `sqlite_locked_total` | counter `{operation}` | GORM/驱动错误分类；不要把 SQL 文本作为标签 |
| SQLite | `sqlite_transaction_duration_seconds` | histogram `{operation}` | 事务包装层；当前没有现成事务计时 |
| SQLite | `sqlite_conn_open_total`、`sqlite_conn_in_use`、`sqlite_conn_wait_total`、`sqlite_conn_wait_duration_seconds` | gauge/counter/histogram | `sql.DB.Stats()`；当前未暴露 |
| RAG | `rag_stage_duration_seconds` | histogram `{stage}` | chat pipeline 的 `stage_complete`；`knowledge-search` 需要单独补 stage 计时 |
| RAG | `rag_stage_failures_total` | counter `{stage,error_type}` | `stage_failed`/fallback 分支；error_type 固定枚举 |
| Agent | `agent_tool_calls_total` | counter `{tool,success}` | `handleToolCall`/`handleToolResult`；tool 名白名单，其他归 `other` |
| Agent | `agent_tool_duration_seconds` | histogram `{tool}` | 工具结果事件 |
| Agent | `agent_turn_duration_seconds` | histogram | 完成事件 `total_duration_ms` |
| Host | `host_reconciliation_events_total` | counter `{event}` | `Report` 回调；event 只保留 waiting/recovered/failed 等固定值 |
| Host | `host_outbox_items` | gauge `{status}` | 复用 `extensions/outbox/store.go:272-295` 的 `Stats`；status 只用 pending/processing/dead |
| Host | `host_outbox_oldest_pending_seconds` | gauge | 同一 `Stats.OldestPendingAge` |

统一约束：

- 标签只使用固定枚举、HTTP method、Gin route template、工具名白名单；禁止把 tenant_id、knowledge_base_id、session_id、document_id、用户输入、URL query、SQL 文本或响应正文作为标签。
- 指标值必须是数值类型；现有日志的 `latency=...ms` 只能作为离线解析输入，不应作为长期指标契约。
- 采样和保留由部署方式决定：优先在进程内聚合后由本地管理端点或周期性结构化日志输出；不要默认公网暴露。
- 所有新指标应同时有失败/空数据语义（例如 `available=false`、`*_error`），避免把“无数据”解释成“健康”。

## 4. 缺口、风险与优先级

### 4.1 可直接复用

1. 请求日志已有 `request_id`、method、path、status、size、latency，可作为离线 API 基线和 `request_id` 关联排障；但不能直接得到实时直方图。
2. GORM 200ms slow SQL 日志可作为临时慢查询清单；P2-B 已证明当前无 Busy/Locked，但没有持续计数。
3. `session_knowledge_qa.go` 的 stage 日志和 Agent 工具 metadata 已覆盖单次排障所需的阶段/工具耗时。
4. `extensions/outbox/store.go` 的 `Stats` 可直接生成 pending/processing/dead/oldest-age 快照，不需要新表或 Schema。
5. `SystemHandler.GetRuntimeQueues` 已能提供队列深度、worker pool 活跃度和模型限流快照，适合管理页/人工巡检，不适合历史趋势。

### 4.2 主要缺口

- 没有时间序列存储和导出端点，无法从现有文本日志直接计算稳定 P50/P95、错误率或趋势。
- 请求日志的 `path` 含真实路径和 query；`latency` 是字符串；日志还会捕获 10KB 级别的请求/响应正文，直接扫描日志做指标既高基数又带来隐私/容量风险。
- SQLite 只有超过 200ms 的慢 SQL；没有 `database/sql` 连接池等待、事务时间和 Busy/Locked 分类。Core `SetMaxOpenConns(1)` 的排队时间会混入请求总延迟，无法从当前日志拆分“等连接”和“实际查询”。
- `knowledge-search` 没有 `duration_ms`；RAG 阶段日志只覆盖 chat pipeline，Rerank/向量召回/上下文组装的独立耗时不可直接聚合。
- Agent 只有工具调用事件和消息字段，没有按工具名聚合的调用量/失败率/耗时直方图。
- Host `Report` 只输出固定事件文本；Outbox `Stats` 没有 HTTP/periodic 输出；P2-A 的 140 条 dead-letter 只能通过数据库查询发现。
- Langfuse 是可选 trace，不是指标后端；无 `LANGFUSE_*` 时没有 trace。

### 4.3 优先级建议

- **P0（先做，低风险）**：Core API RED 指标（请求量、延迟、状态类）、Host 对账事件计数、Outbox `Stats` 快照、SQLite slow/busy/locked 与连接池等待。先完成离线基线，再决定是否加最小的进程内 metrics registry。
- **P1**：RAG stage 直方图和 `knowledge-search` 专用计时、Agent 工具调用计数/失败率/耗时。
- **P2（后置）**：Prometheus/Grafana、告警、长期存储、跨进程聚合。没有稳定契约前不做。

## 5. 建议的下一步

1. **P2-E-01：离线基线（2026-11-08 已完成）**。已新增只读脚本解析 `data/launcher-run-*/core.log` 的请求行、`host.log` 的事件行，并只读查询 `ext_projection_outbox`；输出一次性 JSON 基线，详见 [P2-E-01 只读离线基线](P2-E-01-offline-baseline-2026-11-08.md)。该步骤未改源码，不引入监控服务。
2. **P2-E-02：最小埋点（2026-11-08 已实施并提交）**。已实现第 3 节 P0 的 Core API RED、SQLite 慢查询/Busy/Locked/DBStats、Host reconciliation 事件与 Outbox 快照；Core 与 Host 均只在 loopback/管理面暴露 `/metrics`，不记录 body、query、tenant/KB 标识，不改变业务逻辑。实现与验证见 [P2-E-02 最小埋点与本地指标端点](P2-E-02-minimal-metrics-2026-11-08.md)。
3. **P2-E-03：趋势和告警**。只有在 P2-E-02 证明指标稳定、基数可控后，才讨论 Prometheus/Grafana 或长期存储。

本轮审计的建议是：P2-E 先完成 P2-E-01 的离线基线，不直接改代码；RAG 混合负载 P50 5.3s→11.8s 单独作为 RAG 性能专题，Outbox 140 条 dead-letter 作为独立可靠性待办，不混入本轮指标工作。

## 6. 审计验证边界

- 本轮只读审计，未修改任何源码或数据库；未启动/停止生产服务。
- 现有日志样本：`data/launcher-run-lY5BJT/core.log` 在 2026-11-07 20:53:14 仍有请求日志，包含 `latency=9.152008ms method=GET status_code=200` 和 `response_body` 字段；这证明日志可用，也证明当前日志会携带业务响应体，指标解析必须避免依赖正文。
- P2-A 的 Host 事件样本在 `data/launcher-run-lY5BJT/host.log`：`reconciliation_waiting_for_core code=upstream_unreachable consecutive=1 ...`、`reconciliation_recovered`。
- 本报告不把 P2-B 的压测结论当作长期监控结果；P2-B 的测量窗口有限，P2-E 需要持续采集。

## 7. P2-E-01 执行结果（2026-11-08）

P2-E-01 已完成，脚本为 `extensions/scripts/p2e_offline_baseline.py`，测试为 `extensions/scripts/p2e_offline_baseline_test.py`。对 `data/launcher-run-MZLn17` 的只读运行得到 7,423 条请求、解析错误 0、P50 7.109756ms、P95 13.269646ms、P99 35.049696ms；Outbox 当前有 174 条 dead、无 pending/processing。完整输入、命令、Host 事件对照和限制见 [P2-E-01 只读离线基线](P2-E-01-offline-baseline-2026-11-08.md)。

## 8. P2-E-02 执行结果（2026-11-08）

P2-E-02 已实现 P0 最小埋点：`internal/metrics` 提供进程内 HTTP RED、SQLite slow/busy/locked、DBStats 与 Prometheus 文本输出；`internal/middleware/metrics.go` 负责 Core 路由模板和 loopback 访问限制；`internal/container/gorm_logger.go`/`container.go` 接入 SQLite 与连接池；`extensions/host/metrics.go` 与 `extensions/bridge/server.go` 提供 Host reconciliation 事件和 Outbox 快照。独立测试结果：`internal/metrics` 通过，`internal/middleware` 文件级测试通过，`extensions/host`/`extensions/bridge` 及 `cd extensions && go test -mod=readonly ./...` 全部通过；Core 全包测试受本机 `CGO_ENABLED=0`、缺少 gcc、sqlite-vec/duckdb Windows build constraints 阻断，已记录为基线环境问题。实现细节、指标/标签安全边界和完整测试命令见 [P2-E-02 最小埋点与本地指标端点](P2-E-02-minimal-metrics-2026-11-08.md)。
