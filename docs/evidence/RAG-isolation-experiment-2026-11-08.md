# RAG 隔离实验（2026-11-08）

## 1. 目的与边界

本轮只做只读测量，回答以下问题：

1. RAG 搜索单独运行时，延迟是否仍接近 P2-B 混合负载的 `knowledge_search` P50 11.8s？
2. 搜索与 KB 文档列表并发时，搜索是否被列表请求拖慢？
3. 搜索与 Host Overview 并发时，搜索是否被 Overview 拖慢？
4. 当前运行环境能否观测 P2-E-02/P2-E-P1 新增的 SQLite 与 RAG 阶段指标？

不修改源码、数据库、配置或业务行为；不把本轮数据解释为长期容量结论。RAG 混合负载的原始日志拆解见 `docs/evidence/RAG-mixed-load-analysis-2026-11-08.md`。

## 2. 运行环境与方法

- 启动方式：`start.cmd -SkipDocker -SkipMinerU`，Core/Host 在 WSL，模型 supervisor 在 Windows；运行目录 `data/launcher-run-2X6xUV`。
- Core `http://127.0.0.1:18880`，Host `http://127.0.0.1:8090`；KB `164bfd73-d231-4d5c-994e-057ac0cdf48a`。
- 模型：embedding `Qwen3-Embedding-4B-Q6_K.gguf`（GPU，`ctx_size=4096`）、rerank `qwen3-reranker-0.6b-q8_0.gguf`（GPU，`ctx_size=512`）。由于本机 GPU 当时只剩约 1.9GB 空闲，embedding 先以 `force=true` 启动；rerank 使用小上下文并 `force=true` 启动。该资源条件低于正常空载基线，见第 5 节限制。
- 负载脚本：`data/rag_isolation.py`，使用 `requests`，每轮时长 15 秒：
  - `search_only`：4 个并发搜索 worker；
  - `search_list`：4 个搜索 worker + 4 个 KB 文档列表 worker；
  - `search_host_overview`：4 个搜索 worker + 4 个 Host lifecycle overview worker。
- 每组测量前先执行 2 个不同的预热搜索；组内每个搜索请求使用递增后缀的独立 query，避免同 query 缓存命中。本次预热两条耗时 1,295.54ms、1,234.40ms。列表与 Overview 使用与 P2-B 相同的参数（page=1、page_size=20）。
- 原始结果：`data/rag-isolation-2026-11-08.json`。

## 3. 结果

### 3.1 汇总

| 组别 | 任务 | n | 状态 | P50 | P95 | max |
|---|---|---:|---|---:|---:|---:|
| search_only | knowledge_search | 10 | 200×10 | 6,214.75ms | 19,977.70ms | 19,977.70ms |
| search_list | knowledge_search | 11 | 200×11 | 4,087.32ms | 18,297.09ms | 18,297.09ms |
| search_list | knowledge_list | 2,649 | 200×2,649 | 18.24ms | 43.99ms | 196.79ms |
| search_host_overview | knowledge_search | 10 | 200×10 | 5,736.43ms | 19,500.51ms | 19,500.51ms |
| search_host_overview | host_overview | 2,203 | 200×2,203 | 26.88ms | 32.76ms | 65.16ms |

所有请求均无错误；`knowledge_search` 三组均为 HTTP 200。

### 3.2 对照

- 单独搜索 P50 6.21s，低于 P2-B 混合负载的 11.83s；搜索 + 列表为 4.09s，搜索 + Host Overview 为 5.74s。
- 列表 P50 18.24ms、Host Overview P50 26.88ms，与 P2-B 基线（19.37ms、25.24ms）同量级，没有出现列表/Overview 被搜索并发拖慢的迹象。
- 三组搜索 P95 均约 18–20s，且每组均有一个明显长尾请求；这说明当前模型/GPU 资源下仍有偶发排队或重排长尾，不能把 P50 差异解释为列表或 Overview 的职责影响。
- 搜索 P50 在三组间并非单调上升（6.21s → 4.09s → 5.74s）；鉴于每组只有 10–11 个样本且每组首波都存在冷/排队影响，本轮不能得出“增加列表/Overview 会改善或恶化搜索”的因果结论，只能得出“没有稳定的额外拖慢证据”。

## 4. 指标可观测性限制

- 本文件记录第一轮使用旧 `WeKnora-lite` 二进制的结果；Core `/metrics` 当时返回前端 HTML，无法读取 `rag_stage_duration_seconds`、`rag_stage_errors_total`、`rag_stage_empty_total`，也无法读取 SQLite 连接等待/DBStats 指标。带 P2-E 指标的复跑见 [RAG 隔离实验：带 P2-E 指标复跑](RAG-isolation-metrics-2026-11-08.md)。
- Host `/metrics` 正常返回 `host_reconciliation_events_total`、`host_outbox_items` 等；本实验没有改变 Outbox 状态。
- 因此本轮不把“阶段指标为零/缺失”解释为业务健康，只将其记为“旧 Core 二进制未包含新指标”的构建限制。要验证 P2-E-P1 的 RAG 阶段指标，需在可构建的环境中重新构建 `WeKnora-lite` 后重跑同一脚本。

## 5. 结论与建议

1. 预热后，单搜索和混合组均未复现 P2-B 的 11.8s P50；本轮 P50 为 4.1–6.2s，说明原 11.8s 更可能由冷启动/并发首波和模型排队放大，而不是稳定的混合负载基线。
2. 列表与 Host Overview 的 P50 与 P2-B 基线同量级；没有足够证据把搜索长尾归因于这两个接口。
3. 搜索 P95 仍有 18–20s 长尾，优先怀疑 rerank 模型调用排队/GPU 资源紧张；当前 GPU 空闲仅约 236MB，且 embedding 与 rerank 均被强制启动，不能代表正常容量。
4. 下一步应在有足够 GPU 空闲、且 Core 二进制包含 P2-E-P1 指标的环境中重跑本脚本；同时记录 `rag_stage_duration_seconds` 与 SQLite 连接等待，才能区分搜索、rerank 与数据库等待。本轮不修改任何业务或配置。

## 6. 原始结果

```json
{
  "generated_at": "2026-11-08T21:32:48+0800",
  "run_dir": "/mnt/d/LanRAG-Next/data/launcher-run-2X6xUV",
  "groups": {
    "search_only": {
      "duration_s": 15.0,
      "tasks": {
        "knowledge_search": {"n": 10, "statuses": {"200": 10}, "errors": [], "p50_ms": 6214.75, "p95_ms": 19977.7, "max_ms": 19977.7}
      }
    },
    "search_list": {
      "duration_s": 15.0,
      "tasks": {
        "knowledge_search": {"n": 11, "statuses": {"200": 11}, "errors": [], "p50_ms": 4087.32, "p95_ms": 18297.09, "max_ms": 18297.09},
        "knowledge_list": {"n": 2649, "statuses": {"200": 2649}, "errors": [], "p50_ms": 18.24, "p95_ms": 43.99, "max_ms": 196.79}
      }
    },
    "search_host_overview": {
      "duration_s": 15.0,
      "tasks": {
        "knowledge_search": {"n": 10, "statuses": {"200": 10}, "errors": [], "p50_ms": 5736.43, "p95_ms": 19500.51, "max_ms": 19500.51},
        "host_overview": {"n": 2203, "statuses": {"200": 2203}, "errors": [], "p50_ms": 26.88, "p95_ms": 32.76, "max_ms": 65.16}
      }
    }
  }
}
```
