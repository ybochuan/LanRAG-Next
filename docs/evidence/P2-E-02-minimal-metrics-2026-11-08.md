# P2-E-02 最小埋点与本地指标端点（2026-11-08）

## 1. 范围

在 P2-E 审计和 P2-E-01 离线基线之后，实现 P0 最小指标：

- Core API RED：请求量、延迟直方图、状态类；
- SQLite：slow query、busy/locked、连接池 DBStats；
- Host：reconciliation 固定事件计数、Outbox pending/processing/dead/oldest-age；
- 不实现 RAG/Agent P1 指标，不引入 Prometheus/Grafana、长期存储、告警或跨进程聚合；
- 不改业务判断、检索、解析、Agent、视觉交互或 Outbox 语义。

## 2. 实现

### 2.1 Core 指标注册表

新增 `internal/metrics/metrics.go`：

- `Registry` 使用进程内互斥锁，`Default` 为进程级注册表；
- `ObserveHTTP(method, route, status, duration)`：`core_http_requests_total{method,route,status_class}`，以及固定桶（50ms/100ms/250ms/500ms/1s/2.5s/5s/10s/30s/60s）的 `core_http_request_duration_seconds`；
- `ObserveSQLite(operation, duration, err)`：`sqlite_slow_queries_total`、`sqlite_busy_total`、`sqlite_locked_total`，operation 只允许 select/insert/update/delete/other；
- `SetDBStatsProvider`：输出 `sqlite_conn_open_total`、`sqlite_conn_in_use`、`sqlite_conn_idle`、`sqlite_conn_wait_total`、`sqlite_conn_wait_duration_seconds` 及连接关闭计数；
- `ServeHTTP`：Prometheus 文本格式，标签顺序固定，空数据保留 `*_available` 语义。

### 2.2 Core HTTP 与路由

新增 `internal/middleware/metrics.go`：

- `Metrics()` 在响应结束后记录 `c.FullPath()`，不记录原始 URL 或 query；
- `MetricsEndpoint()` 只允许 `RemoteAddr` 为回环地址，并拒绝携带 `X-Forwarded-For`/`X-Real-IP` 的代理请求；
- `internal/router/router.go` 在认证前注册 `Metrics()` 和 `GET /metrics`。

### 2.3 SQLite 与连接池

修改 `internal/container/gorm_logger.go`：

- 包装现有 GORM logger，慢查询或错误 trace 时调用 `metrics.Default.ObserveSQLite`；
- SQL 文本仅用于固定 operation 分类，不进入指标标签；
- 慢查询阈值复用 `metrics.SQLiteSlowThreshold`（200ms）。

修改 `internal/container/container.go`：

- 在 `sql.DB` 连接池配置后注册 `sql.DB.Stats()` provider；
- Core 尚未初始化数据库时，端点输出 `sqlite_conn_stats_available 0`，不把无数据当作健康。

### 2.4 Host 事件与 Outbox

新增 `extensions/host/metrics.go`：

- `hostMetrics.record` 只保存固定事件码；`reconciliation_waiting_for_core ...`、`reconciliation_failed ...` 按前缀归一，其余未知文本归 `other`，不会把错误详情当成标签；
- `newHostOutboxSnapshot` 复用 `outbox.Store.Stats`，对配置的 scope 聚合 pending/processing/dead/oldest-age，不输出 tenant/KB 标识；
- 输出 `host_reconciliation_events_total`、`host_outbox_items`、`host_outbox_oldest_pending_seconds`、`host_outbox_oldest_pending_available`、`host_outbox_available`、`host_outbox_query_errors_total`。

修改 `extensions/bridge/server.go` 的 `Services` 增加可选 `Metrics http.Handler`，并在 `NewServerWithServices` 注册 `GET /metrics`；`extensions/host/runtime.go` 在 `Serve` 中包装 `report` 回调并把 Host Outbox 快照注入 Bridge。Host 监听仍固定 `127.0.0.1:8090`，不改变绑定地址。

## 3. 指标与安全边界

| 指标 | 标签/语义 |
|---|---|
| `core_http_requests_total` | method、Gin route template、status_class (`2xx`/`3xx`/`4xx`/`5xx`) |
| `core_http_request_duration_seconds` | method、route template，固定 histogram 桶 |
| `sqlite_slow_queries_total` | operation 固定枚举 |
| `sqlite_busy_total` / `sqlite_locked_total` | operation 固定枚举 |
| `sqlite_conn_*` | 无标签，来自 `sql.DB.Stats()` |
| `host_reconciliation_events_total` | 固定 event 枚举 |
| `host_outbox_items` | status 仅 pending/processing/dead |
| `host_outbox_oldest_pending_*` | 聚合值，无 scope 标签 |

约束：

- 禁止把 tenant_id、knowledge_base_id、session_id、document_id、用户输入、URL query、SQL 文本、请求/响应正文作为标签；
- Core `/metrics` 仅回环可访问；Host `/metrics` 只存在于已绑定 loopback 的 Bridge；
- 指标为进程内快照，不持久化、不改变业务行为；无数据时使用 `available=0`，避免误读为健康。

## 4. 验证

### 4.1 通过项

```bash
go test -mod=readonly ./internal/metrics -count=1
# ok github.com/Tencent/WeKnora/internal/metrics

go test -mod=readonly ./internal/middleware/metrics.go ./internal/middleware/metrics_test.go -count=1
# ok command-line-arguments

go test -mod=readonly ./internal/container/gorm_logger.go -count=1
# ? command-line-arguments [no test files]（仅编译验证）

cd extensions
go test -mod=readonly ./host ./bridge -count=1
# ok github.com/ybochuan/LanRAG-Next/extensions/host
# ok github.com/ybochuan/LanRAG-Next/extensions/bridge

go test -mod=readonly ./... -count=1
# 全部扩展包 ok（2026-11-08 本机运行）

go vet -mod=readonly ./...
go build -mod=readonly ./...
# 均无输出、exit 0
```

覆盖内容：

- `internal/metrics/metrics_test.go`：HTTP 状态类/路由标签、延迟 histogram、SQLite slow/busy/locked、DBStats、无 DBStats 时的 available 语义；
- `internal/middleware/metrics_test.go`：Gin route template 不泄露真实 ID/query、非回环和代理头请求返回 404；
- `extensions/host/metrics_test.go`：固定事件归一化、Outbox snapshot、错误与空数据语义；
- `extensions/bridge/server_test.go`：配置 `Services.Metrics` 时 Bridge 暴露 `/metrics`。

### 4.2 受本机基线阻断的项

- `go test -mod=readonly ./internal/middleware -count=1` 无法在当前 Windows 环境构建：`CGO_ENABLED=0` 且缺少 gcc 时，`internal/utils` 报 `undefined: pg_query.Parse/Deparse`；
- `CGO_ENABLED=1` 直接报 `cgo: C compiler "gcc" not found`；
- `go test -mod=readonly ./internal/container -count=1` 受 `sqlite-vec` 和 `duckdb` Windows build constraints 阻断。

上述为实现前的仓库/环境限制，不是 P2-E-02 引入的失败；本轮已用独立文件级测试覆盖 middleware 与 GORM logger 的可验证逻辑。

## 5. 与 P2-E-01 的关系

- P2-E-01 仍是离线基线（日志 + Outbox 只读快照）；P2-E-02 不替换它，而是提供实时、低基数的进程内指标；
- P2-E-02 不改变 `data/launcher-run-*`、不查看或输出请求/响应正文；
- RAG 混合负载 P50 5.3s→11.8s、174 条 Outbox dead-letter 继续作为独立事项，不因本端点存在而被视为已解决。

## 6. 未实施/后续

- RAG stage、Agent 工具调用/失败率/单轮耗时（P1）；
- Prometheus/Grafana、长期存储、告警（P2）；
- 跨进程聚合和真实生产压测；需在具备 CGO/Windows 或 Linux 构建条件的环境补跑 Core 包测试并验证真实 `/metrics` 请求。
