# P2-E P1：RAG 阶段指标（2026-11-08）

## 范围

本项是 P2-E 性能监控的 P1 最小扩展，只补 `knowledge-search` 检索管线的阶段耗时可观测性，不改变检索、rerank、合并、过滤或 LLM 生成逻辑，不引入 Prometheus/Grafana，不采集 query、tenant、knowledge-base、session 或文档正文。

## 实现

- `internal/metrics/metrics.go` 新增：
  - `rag_stage_duration_seconds` histogram，固定阶段标签：`chunk_search`、`chunk_rerank`、`chunk_merge`、`filter_top_k`；未知阶段归并为 `other`。
  - `rag_stage_errors_total{stage}`：阶段返回非 `ErrSearchNothing` 错误。
  - `rag_stage_empty_total{stage}`：阶段以 `ErrSearchNothing` 结束、结果为空的次数。
  - 复用已有固定直方图桶；阶段名和状态均为固定枚举，不接受自由文本标签。
- `internal/application/service/session_knowledge_qa.go` 的 `runKnowledgeSearchPipeline`：
  - 在每次 `eventManager.Trigger` 前后记录阶段耗时；
  - `ErrSearchNothing` 记为 `empty`，其他错误记为 `error`，成功记为 `success`；
  - 指标记录不改变原有错误处理、Langfuse span 或返回值语义。
- `/metrics` 同时输出 Core HTTP RED、SQLite slow/busy/locked/DBStats、Host reconciliation/Outbox 以及本次新增的 RAG 阶段指标；Core 端点仍只允许 loopback。

示例输出：

```text
rag_stage_duration_seconds_bucket{stage="chunk_rerank",le="2.5"} 1
rag_stage_duration_seconds_sum{stage="chunk_rerank"} 2.5
rag_stage_duration_seconds_count{stage="chunk_rerank"} 1
rag_stage_errors_total{stage="chunk_merge"} 1
rag_stage_empty_total{stage="chunk_search"} 1
```

## 验证

- `go test -mod=readonly ./internal/metrics -count=1`：通过（含 RAG 指标渲染断言）。
- `gofmt -l internal/metrics internal/application/service`：无输出。
- `make build-lite SKIP_FRONTEND=1`（WSL Ubuntu，go1.26.0 linux/amd64，gcc）：构建成功，产出 `WeKnora-lite` 271MB；实际运行后 Core `/metrics` 返回 200 且包含 `rag_stage_duration_seconds`/`rag_stage_errors_total`/`rag_stage_empty_total`。
- Windows 直接 `go build -mod=readonly ./internal/application/service ./internal/metrics` 仍受本机 CGO/`pg_query` 基线阻断；不是本次改动引入；该限制不适用于 WSL Lite 构建。
- `go test -mod=readonly ./internal/application/service`：受既有测试依赖 `miniredis` 下载失败（网络不可达）及上述 CGO 基线阻断，未作为本项通过依据。

## 边界与未做事项

- 不修改检索、rerank、merge、filter 的业务判断，不调整 `defaultBatchConcurrency=4`。
- 不新增 Prometheus/Grafana、长期存储或告警规则；这些仍属于 P2-E-03 的后续决策。
- 不采集请求正文、query、tenant/KB/session 标识；RAG 阶段指标只用于进程内汇总。
- 本次只解决“持续观察阶段分位”的缺口；隔离实验（仅搜索/搜索+列表/搜索+Host overview）已完成，见 `docs/evidence/RAG-isolation-experiment-2026-11-08.md` 与带指标复跑 `docs/evidence/RAG-isolation-metrics-2026-11-08.md`；rerank 模型容量评估仍需单独执行。

## 相关文件

- `internal/metrics/metrics.go`
- `internal/metrics/metrics_test.go`
- `internal/application/service/session_knowledge_qa.go`
- `docs/evidence/RAG-mixed-load-analysis-2026-11-08.md`
- `docs/evidence/P2-E-performance-monitoring-audit-2026-11-07.md`
