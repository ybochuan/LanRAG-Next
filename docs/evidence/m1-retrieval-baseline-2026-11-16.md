# M1.5 库级检索基线（生产环境，2026-11-16）

## 范围

本文件记录冻结架构 M1.5「每库 30–50 查询 Recall@5/10/20、MRR」在**真实生产环境**下的首次实测。使用生产 Core 18880、生产知识库 `164bfd73-d231-4d5c-994e-057ac0cdf48a`（75 篇 active 且解析完成的文档），未建立隔离环境或虚拟环境，未修改原生 RRF 实现、未修改生产检索配置、未修改生产数据。

## 工具

新增 `extensions/cmd/retrieval-eval`：一个只读评估客户端。读查询清单，调用原生 `POST /api/v1/knowledge-bases/{id}/hybrid-search`，按返回的 `knowledge_id` 做**文档级** Recall@k 与 MRR 计算。报告含每题的排名列表、命中排名、各档 Recall 与平均倒数排名。它不改检索状态、不改知识库、不改原生 RRF。

## 查询集

两组各 35 道查询，均来自生产库真实文档：

- **标题组**：从有语义的中文文档标题构造（去除版本号与扩展名、去重后 35 道），期望文档为标题对应的真实文档 ID。
- **描述组**：从真实文档的 description 首句构造（非标题回显，35 道），期望文档为描述对应的真实文档 ID。

两组都满足冻结架构 30–50 查询的要求。查询与期望文档清单位于忽略目录 `tmp/`。

## 结果

| 查询集 | 查询数 | 失败 | Recall@5 | Recall@10 | Recall@20 | MRR |
|---|---:|---:|---:|---:|---:|---:|
| 标题组 | 35 | 0 | 0.9714 | 0.9714 | 0.9714 | 0.8795 |
| 描述组 | 35 | 0 | 1.0000 | 1.0000 | 1.0000 | 0.8976 |

冻结判据为 Recall@5 ≥ 0.85 且 MRR ≥ 0.80（或基线 95%）。**两组均达标**：标题组 Recall@5 0.9714 ≥ 0.85、MRR 0.8795 ≥ 0.80；描述组两项更高。标题组多数查询首位命中，少量因版本族兄弟文档（同一办法的不同修订版）共享标题词片而使期望文档排在第 2 位，这是 Recall@5 未达 1.0 的原因。

## 受约束未做的部分

M1.5 还包含 RRF 参数网格搜索、重排降级、查询扩展门控与 int8 量化 feature flag。本轮**未**实施，理由如下，均不是实现缺陷：

- **RRF 网格搜索**：生产 RRF 参数来自租户 `RetrievalConfig`（`rrf_k`、`rrf_vector_weight`、`rrf_keyword_weight`，见 `internal/types/retrieval_config.go`），经 `fuseWithRRF` 生效。没有原生只读 API 可枚举这些参数；`internal/router` 未命中 retrieval-config 路由。在不知道生产当前取值的前提下做网格搜索会误改生产检索行为，冻结架构同时要求「CI 无原生 RRF 修改」，因此本轮只做只读基线。
- **重排降级与查询扩展门控**：需要改变生产问答链路的运行时参数或新增原生薄点，属于 M1.5 调参实施，不在本轮只读基线授权范围内。
- **int8 量化 feature flag**：冻结要求以 `vec_quantize_i8` 为准、只经 flag 验证且 Recall@10 退化 ≤ 2%。生产 sqlite-vec 仍处 pre-v1，缺 flag 与对照组，无法做合规对比。

这些项登记为受约束后续项，需要用户决定是新增原生只读/配置 API，还是授权在生产检索配置上做受控实验。

## 验证命令与结果

```text
gofmt -l extensions/cmd/retrieval-eval/                     → 无输出
CGO_ENABLED=0 GOWORK=off go build -mod=readonly ./cmd/retrieval-eval → 退出 0
CGO_ENABLED=0 GOWORK=off go test -mod=readonly ./cmd/retrieval-eval/... -count=1 → ok 0.090s（4 例）
./tmp/retrieval-eval --manifest <标题组> --core <生产> --token <JWT> --top-k 20 → 35 题零失败
./tmp/retrieval-eval --manifest <描述组> --core <生产> --token <JWT> --top-k 20 → 35 题零失败
```

新增测试 4 例：文档级 Recall/MRR 计算（含多命中与首位排名）、HTTP 失败上报、跨查询聚合、空清单拒绝。

## 边界

- 本文件证明**基线可测且当前达标**，不证明 RRF 已优化、量化已验证或重排降级/查询扩展门控已实施。
- 查询集由生产文档标题/描述构造，是内容检索基线；不是业务问答质量评估（那属 M4 Agentic）。
- 未改原生 RRF 实现、未改生产检索配置、未改生产数据；报告与清单位于忽略目录 `tmp/`。
- 原生包构建需 CGO（pg_query/DuckDB 既有基线），本工具在扩展模块以 CGO_ENABLED=0 构建通过。
