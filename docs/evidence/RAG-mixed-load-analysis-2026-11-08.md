# RAG 混合负载延迟拆解（2026-11-08）

## 结论摘要

P2-B 混合负载中的 `knowledge_search` P50 11.8s 不是单个请求在稳定状态下的延迟，而是 15 秒基准内两波并发搜索的混合值：

- 第一波 4 个请求总耗时 13.243–13.635s，其中 `chunk_search` 6.214–6.482s、`chunk_rerank` 6.557–7.021s；
- 第二波 4 个请求总耗时 8.030–10.239s，其中 `chunk_search` 2.183–2.399s、`chunk_rerank` 5.581–7.772s；
- `chunk_merge` 与 `filter_top_k` 合计始终小于 0.25s，可以忽略。

进一步拆解 `chunk_search` 后，第一波 vector 与 keywords 两条检索链均约 5.8–6.2s，第二波均约 2.1–2.3s，二者完成时间几乎重合，说明瓶颈不是某一条检索链，而是两条链共同等待的共享资源/冷缓存。`chunk_rerank` 是非线性主项：本轮 4 路并发下为 5.6–7.8s，而后续单请求探测为 4.17–4.88s；17:39:35 还有一条单请求 rerank 17.714s 的离群值。

结论边界：本轮证据支持“搜索阶段共享排队 + 远程 rerank 模型并发延迟”两个方向，但不足以单独证明 SQLite 连接池是根因。P2-B 已确认日志中没有 `database is locked`、`SQLITE_BUSY`、`context deadline`；在没有连接等待指标和隔离实验前，不应仅凭混合负载的 11.8s 修改 Core 的 `SetMaxOpenConns(1)` 或 WAL 配置。

## 输入与方法

- 混合负载脚本：`data/p2b_mixed.py`；持续 15 秒，三类任务各 4 个 worker：
  - `POST /api/v1/knowledge-search`，固定 query，单 KB；
  - `POST /management/lifecycle`（Host overview）；
  - `GET /api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=20`。
- 基准结果：`data/p2b-mixed.json`（generated_at `2026-11-07T17:39:16+0800`）。
- 请求日志：`data/launcher-run-lY5BJT/core.log`；`knowledge-search` 共 20 条请求记录（8 条属于本次混合负载，其余为后续单请求探测）。
- 解析方式：只读取请求行前缀与搜索事件边界，不读取请求/响应正文；`latency` 使用 Go `time.Duration` 格式（`ns`/`µs`/`μs`/`ms`/`s`）解析为毫秒。阶段边界取自 `Starting to trigger search event: <stage>` 与 `Event <stage> triggered successfully`；请求完成取自 `Knowledge search completed`。
- 未修改 Core/Host/SQLite/前端源码，未重启或停止生产服务，未写入生产数据库。

## 基准总量

`data/p2b-mixed.json`：

| 任务 | 并发 | 请求数 | P50 | P95 | max | 状态 |
|---|---:|---:|---:|---:|---:|---|
| `knowledge_search` | 4 | 8 | 11,831.21ms | 13,659.53ms | 13,659.53ms | 8/8 200 |
| Host `overview` | 4 | 2,231 | 25.24ms | 39.54ms | 110.36ms | 2,231/2,231 200 |
| `knowledge_list` | 4 | 1,844 | 19.37ms | 92.55ms | 1,308.97ms | 1,844/1,844 200 |

`knowledge_search` 只有 8 条，是因为 4 个搜索 worker 的每个请求约 8–14s；15 秒窗口内先发出第一波 4 条，第一波结束后才发出第二波 4 条，第二波在基准停止后完成。

## 分阶段结果

### 第一波（4 路同时开始）

| 请求（短 ID） | 总耗时 | `chunk_search` | `chunk_rerank` | `chunk_merge` | `filter_top_k` |
|---|---:|---:|---:|---:|---:|
| `03fc8dd2` | 13.590s | 6.214s | 7.021s | 0.178s | 0.021s |
| `a8f6e0bc` | 13.425s | 6.313s | 6.828s | 0.112s | 0.012s |
| `2bb2675c` | 13.635s | 6.401s | 6.873s | 0.182s | 0.008s |
| `e5b75033` | 13.398s | 6.482s | 6.557s | 0.157s | 0.023s |

### 第二波（第一波结束后开始）

| 请求（短 ID） | 总耗时 | `chunk_search` | `chunk_rerank` | `chunk_merge` | `filter_top_k` |
|---|---:|---:|---:|---:|---:|
| `077ac84a` | 10.240s | 2.313s | 7.772s | 0.046s | 0.003s |
| `f76ac3b7` | 8.147s | 2.399s | 5.581s | 0.042s | 0.003s |
| `2a84537e` | 10.193s | 2.259s | 7.766s | 0.052s | 0.003s |
| `dbfa8124` | 9.930s | 2.183s | 7.484s | 0.043s | 0.003s |

两波的总耗时差异主要来自 `chunk_search`（约 6.2–6.5s 对 2.2–2.4s）；`chunk_rerank` 两波都维持在 5.6–7.8s，说明 rerank 是独立的远端模型瓶颈，不随第二波搜索变快而消失。

## `chunk_search` 内部

相对 `chunk_search` 开始时间：

| 波次 | vector 完成 | keywords 完成 | fusion 完成 | output 完成 | `chunk_search` 总耗时 |
|---|---:|---:|---:|---:|---:|
| 第一波 | 5.792–6.214s | 5.800–6.204s | 5.953–6.318s | 6.212–6.480s | 6.214–6.482s |
| 第二波 | 2.078–2.272s | 2.071–2.265s | 2.128–2.317s | 2.182–2.396s | 2.183–2.399s |

vector 与 keywords 几乎同时完成，且各自都占满了大部分阶段时间；这符合两条检索链并行、共同等待底层共享资源的形态。`fusion` 和 `output` 只在最后增加约 0.1–0.3s，不构成主瓶颈。

本窗口内 `/knowledge` 列表请求的粗粒度统计（按日志时间窗，非 p2b 全量统计）：

- 第一段约 14 秒：1,747 条，P50 12.88ms、P95 64.63ms、max 1,261.73ms；
- 第二段约 10 秒：107 条，P50 36.14ms、P95 89.32ms、max 104.45ms。

第一段与第一波搜索重叠，列表 worker 持续产生短请求；第二段已接近基准停止，列表流量退潮。该时间相关性提示：Core 单连接下大量短列表请求与长搜索查询同时存在时，搜索可能排队；但也同时包含缓存从冷到热的变化，本轮数据不能把两者完全分离。P2-B 的独立读并发测试显示列表本身在 16 并发下仍无错误（P95 75.51ms），因此不能把列表延迟误判成列表接口故障。

## `chunk_rerank` 观察

- 模型：远程 `qwen3-reranker-0.6b-q8_0`；每请求 50 candidates → 10 results，阈值 0.200，top score 0.9995。
- 第一波 4 个 rerank 在 `17:38:58.66–58.99` 开始，`17:39:05.506–05.714` 完成，阶段耗时 6.557–7.021s。
- 第二波 4 个 rerank 在 `17:39:08.15–08.37` 开始，`17:39:13.864–15.845` 完成，阶段耗时 5.581–7.772s。
- 17:39:41–50 的后续单请求探测：search 0.537–0.635s、rerank 4.172–4.880s、总 4.881–5.541s；说明单请求时 rerank 仍需约 4–5s。
- 17:39:35 的 `9873b7e9`：search 1.943s、rerank 17.714s、总 19.771s；同一批 50 candidates，属于远程 rerank 的明显离群值。

因此 rerank 是当前 `knowledge-search` 的主要固定成本，且并发时会被进一步放大；这不是 SQLite 锁错误可以解释的部分。

## 归因结论

1. **P50 11.8s 是两波混合值**：第一波 13.2–13.6s、第二波 8.0–10.2s；用单一“平均搜索延迟”描述会掩盖冷启动/并发差异。
2. **搜索阶段存在共享排队或冷缓存效应**：第一波 `chunk_search` 约 6.2–6.5s，第二波约 2.2–2.4s；vector 与 keywords 同步变慢，说明不是某一路检索单独故障。列表请求洪峰与第一波重叠，但需专门实验才能区分 SQLite 单连接排队、冷缓存和 CPU/IO 争用。
3. **rerank 是独立主项**：4 路并发 5.6–7.8s，单请求 4.2–4.9s，离群 17.7s；远程模型调用是主要成本，不能归因于 Core SQLite。
4. **不应据此修改连接池**：本轮没有 `SQLITE_BUSY`/`database is locked` 证据；P2-B 的 `SetMaxOpenConns(1)` 决策仍然有效。应先补 RAG 阶段指标与连接等待指标，再做隔离实验。

## 建议的最小后续动作

1. **补 RAG 阶段指标（P2-E P1）**：对 `knowledge-search` 记录 `chunk_search`、`chunk_rerank`、`chunk_merge`、`filter_top_k` 的阶段耗时直方图，标签只使用固定阶段名与 outcome，不带 query/tenant/KB/session。否则只能靠日志事件边界做离线分析，无法持续观察分位。
2. **利用已实现的 SQLite 等待指标**：`internal/metrics` 代码已提供 `sqlite_conn_wait_total` 与 `sqlite_conn_wait_duration_seconds`（P2-E-02）；当前生产进程是否已加载取决于部署，下一次重启后应抓取这两个 gauge/counter，验证第一波搜索是否伴随 Core 单连接等待上升。这比根据 `database is locked` 缺失推断更直接。
3. **做隔离实验再决定优化**：固定同一 query/KB，分别测 (a) 仅 4 路搜索、(b) 4 路搜索 + 4 路列表、(c) 4 路搜索 + Host overview；每组先 warm-up 5 次，再记录 20 次；同时输出 `chunk_search`/`chunk_rerank` 分位。只有确认列表洪峰使搜索阶段显著恶化，才考虑前端轮询/列表缓存或 Core 读连接池，不应直接改 SQLite 配置。
4. **rerank 容量单独评估**：在多路并发下比较模型服务 1/2/4 并发的阶段耗时与 GPU/CPU 队列；若模型服务已饱和，应优先调模型服务并发或排队策略，而不是把 rerank 延迟归入 SQLite。
5. **保留既有优化**：P2-C 已对 KB 列表/文档标题做缓存，会减少真实前端的列表请求量；本基准直接打 HTTP，绕过了前端缓存，结果代表“后端最坏输入”，不能直接等同于生产用户延迟。

## 边界与未做事项

- 单个 15 秒基准、单个 query、单个 KB；`knowledge_search` 只有 8 个样本，P50/P95 仅用于这次定位，不构成生产 SLO。
- `p2b_mixed.py` 的列表与搜索请求是直接 HTTP，不包含前端缓存与浏览器请求合并。
- 日志阶段边界是事件日志，不是生产 metrics；阶段耗时包含请求内的并发等待，不能拆成 CPU 时间。
- 本轮只读分析没有修改任何源码、配置或生产数据库；后续 P2-E P1 阶段指标已单独实现，见 [P2-E P1：RAG 阶段指标](P2-E-P1-rag-stage-metrics-2026-11-08.md)。
- 未新增 Prometheus/Grafana；本轮没有重新运行基准，也没有改动 P2-B 的结论；P2-B 报告仍为 `docs/evidence/P2-B-sqlite-pool-wal-2026-11-07.md`。
