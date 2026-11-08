# RAG 隔离实验：带 P2-E 指标复跑（2026-11-08）

## 1. 目的

在重新构建包含 P2-E-02/P2-E-P1 埋点的 `WeKnora-lite` 后，重跑三组隔离实验，验证：

1. Core `/metrics` 是否真正暴露 RAG 阶段、SQLite 等待和 HTTP RED 指标；
2. 搜索延迟是否由 `chunk_search`、`chunk_rerank`、`chunk_merge`、`filter_top_k` 中某一阶段主导；
3. 列表与 Host Overview 并发是否会影响搜索；
4. SQLite 连接等待是否与搜索长尾同时出现。

本轮仍只做只读测量，不修改业务代码、数据库或配置。

## 2. 构建与运行

- 在 WSL Ubuntu 中使用 `/usr/local/go/bin/go`（go1.26.0 linux/amd64）与 gcc，执行：
  - `make build-lite SKIP_FRONTEND=1`
- 构建产物：`WeKnora-lite`，271MB；旧二进制在构建前临时备份，确认新二进制可用后已清理。
- 启动：`start.cmd -SkipDocker -SkipMinerU`，运行目录 `data/launcher-run-mkZb0T`。
- 模型：embedding GPU、rerank GPU（`ctx_size=512`，`force=true`）；GPU 显存余量约 236MB，属于资源受限环境。
- 实验脚本：`data/rag_isolation.py`，每组 15 秒；2 次预热搜索；每次搜索使用递增后缀的唯一 query。
- 原始结果：`data/rag-isolation-metrics-2026-11-08.json`；Core 指标快照：`data/core-metrics-after-isolation.txt`。

## 3. 客户端结果

`generated_at 2026-11-08T21:47:25+0800`。

| 组别 | 任务 | n | 状态 | P50 | P95 | max |
|---|---|---:|---|---:|---:|---:|
| search_only | knowledge_search | 9 | 200×9 | 3,513.67ms | 17,035.69ms | 17,035.69ms |
| search_list | knowledge_search | 10 | 200×10 | 4,962.21ms | 18,867.74ms | 18,867.74ms |
| search_list | knowledge_list | 2,994 | 200×2,994 | 15.48ms | 40.69ms | 439.31ms |
| search_host_overview | knowledge_search | 10 | 200×10 | 5,111.34ms | 18,517.34ms | 18,517.34ms |
| search_host_overview | host_overview | 3,344 | 200×3,344 | 17.74ms | 21.72ms | 32.55ms |

预热两条搜索耗时 3,835.61ms、2,470.59ms；所有请求无错误。

与第一轮无指标复跑（P50 6.21/4.09/5.74s，P95 18.3–20.0s）相比，趋势一致：搜索 P50 约 3.5–5.1s，P95 约 17–19s；列表与 Overview 的 P50 仍为 15–18ms，没有稳定被搜索拖慢的迹象。

## 4. Core 指标

Core `/metrics` 返回 `200 OK`，`Content-Type: text/plain; version=0.0.4`，包含 `core_http_*`、`rag_stage_*`、`sqlite_*` 指标。

### 4.1 请求总量

- `POST /api/v1/knowledge-search`：31 次，全部 2xx；平均请求耗时 `214.128085s / 31 = 6.907s`。
- `GET /api/v1/knowledge-bases/:id/knowledge`：3,050 次，全部 2xx；平均请求耗时 `46.426437s / 3050 = 15.22ms`。
- 搜索请求耗时分布：≤2.5s 1 次、≤5s 16 次、≤10s 27 次、≤30s 31 次；没有超过 30s。

### 4.2 RAG 阶段（31 次搜索）

| 阶段 | count | sum | 平均 | 关键分桶 |
|---|---:|---:|---:|---|
| `chunk_search` | 31 | 33.999s | 1.097s | ≤0.5s 7、≤1s 17、≤2.5s 31 |
| `chunk_rerank` | 31 | 177.270s | 5.718s | ≤2.5s 2、≤5s 22、≤10s 27、≤30s 31 |
| `chunk_merge` | 31 | 1.600s | 0.052s | ≤0.05s 20、≤0.1s 29、≤0.25s 31 |
| `filter_top_k` | 31 | 0.074s | 0.002s | ≤0.05s 31 |

按总请求耗时粗算，`chunk_rerank` 约占 **82.8%**，`chunk_search` 约占 **15.9%**，`chunk_merge`/`filter_top_k` 合计不足 1%。`rag_stage_errors_total` 与 `rag_stage_empty_total` 全部为 0。

这直接确认了此前日志分析的结论：当前 RAG 搜索的主要耗时在远程 rerank，不是 merge/filter，也不是 SQLite 锁错误。

### 4.3 SQLite

- `sqlite_busy_total=0`、`sqlite_locked_total=0`，没有出现 `SQLITE_BUSY`/`database is locked`。
- `sqlite_slow_queries_total{operation="select"}=16`。
- `sqlite_conn_wait_total=8648`，`sqlite_conn_wait_duration_seconds=14.636741447`。
- 运行后连接池：`sqlite_conn_open_total=1`、`sqlite_conn_in_use=0`、`sqlite_conn_idle=1`。

本轮没有单独记录每个阶段开始/结束时的 wait 计数，因此不能把 14.64s 的直接归因给某一组；但 3,050 次列表请求期间没有 Busy/Locked，且平均列表延迟仍为 15.22ms，说明连接等待存在但不是搜索长尾的主要解释。后续若要精确归因，应在每组切换时抓取一次 `sqlite_conn_wait_*` 快照。

## 5. 结论

1. 重新构建后的 Core 指标可用，P2-E-02/P2-E-P1 埋点已实际工作。
2. 搜索长尾主要来自 `chunk_rerank`：31 次搜索中 4 次 rerank 超过 10s，贡献了大部分总耗时；`chunk_search` 平均仅 1.1s，merge/filter 可忽略。
3. 列表与 Host Overview 并发没有稳定拖慢搜索；两者自身 P50 分别为 15.48ms、17.74ms。
4. SQLite 没有 Busy/Locked；连接等待总量存在，但当前不足以解释 17–19s 的搜索 P95。下一步应评估 rerank 模型并发/排队与 GPU 容量，而不是修改 SQLite 连接池。

## 6. 边界

- 资源受限环境（GPU 余量约 236MB、rerank `ctx_size=512`、强制 GPU 启动），绝对延迟不代表正常容量。
- 单 query、单 KB、每组 15 秒、每组 9–10 个搜索样本，仅用于定位。
- 未对 rerank 模型服务内部做 profiling；`chunk_rerank` 是请求内阶段耗时，不等于模型纯推理时间。
- 本轮未修改源码（除此前已有 P2-E P1 实现）、数据库或配置；实验服务已 `start.cmd -Stop` 停止。
