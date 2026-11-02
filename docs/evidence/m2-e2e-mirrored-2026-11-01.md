# 镜像网络下端到端启动验证 — 2026-11-01

本轮用户要求「跑一次，验证下，并做好记录」。目标是验证 WSL 镜像网络模式下 Core（WSL）、Extension Host（WSL）、MinerU 与本地模型（Windows）三处服务能否真实互通，并记录发现。

HEAD 为 `fa6353e8c821daa5eeb667dc46140e4d0d32f179`（`v0.9.0-2-gfa6353e`），无提交推送。本轮**只改了两个运行时数据**（`data/weknora.db` 的 `models.parameters`）与新增本文件；`internal/`、`extensions/` 源码未改动。

## 1. 拓扑与启动

| 位置 | 服务 | 端口 | 监听地址 |
|---|---|---|---|
| WSL (Ubuntu) | WeKnora Core | 18880 | `127.0.0.1` |
| WSL (Ubuntu) | Extension Host | 8090 | `127.0.0.1` |
| Windows | MinerU 4.0.1 | 8000 | `127.0.0.1` |
| Windows | Embedding `Qwen3-Embedding-4B-Q6_K` | 8081 | `127.0.0.1` |
| Windows | Rerank `qwen3-reranker-0.6b-q8_0` | 8082 | `127.0.0.1` |

启动命令：

```bash
wsl.exe -d Ubuntu -e bash -lc 'cd /mnt/d/LanRAG-Next && MINERU_ENDPOINT=http://127.0.0.1:8000 bash start-all.sh'
```

`start-all.sh` 输出（无需交互登录，已保存凭据有效）：

```
[BUILD] Independent Extension Host (CGO_ENABLED=0)
[START] Host first; waiting for local DB and services (Core-independent readiness)
[READY] Host
[START] Core
[READY] Core
[AUTH] Existing scoped machine credential verified
[READY] AgentRAG: http://localhost:18880
[LOGS] /mnt/d/LanRAG-Next/data/launcher-run-<id>
```

## 2. 跨网络可达性（全部通过）

| 方向 | 目标 | 结果 |
|---|---|---|
| Windows → WSL | Core `127.0.0.1:18880/health` | 200 |
| Windows → WSL | Host `127.0.0.1:8090/health` | 200，`{"status":"ready","scope":"local"}` |
| WSL → WSL | Core 18880、Host 8090 | 200 |
| **WSL → Windows** | MinerU `127.0.0.1:8000/v1/health` | 200，`{"status":"ok","version":"4.0.1"}` |
| **WSL → Windows** | Embedding `127.0.0.1:8081` | 200 |
| **WSL → Windows** | Rerank `127.0.0.1:8082` | 200 |

镜像网络下两侧共享 loopback，`127.0.0.1` 双向通用，模型与 MinerU 继续只绑回环，未暴露到局域网。

## 3. 发现一（已修复）：切换网络模式后 Core 保存的模型地址变成死地址

### 现象

首次 `POST /api/v1/knowledge-search`（`query=上门服务管理办法`，KB `164bfd73-d231-4d5c-994e-057ac0cdf48a`）返回 **HTTP 200 且结果为空**：

```json
{"data":[],"meta":{"rerank":{"applied":false,"outcome":"no_candidates","candidate_count":0}},"success":true}
```

### 根因

`models.parameters` 里保存的 `base_url` 仍是 NAT 模式时代的网关地址 `http://172.26.0.1:8081/v1` 与 `http://172.26.0.1:8082/v1`。镜像网络下该地址不存在，Core 日志：

```
[embedding] retrying (1/3) after 1s: send request: Post "http://172.26.0.1:8081/v1/embeddings": dial tcp 172.26.0.1:8081: i/o timeout
[embedding] retrying (3/3) after 4s: ... i/o timeout
WARNING [PIPELINE] stage=Search action=group_embed_degrade_keyword error="embedding: ... i/o timeout" failed_targets=0 fallback_targets=1 kb_id="164bfd73-..." model_key="Qwen3-Embedding-4B-Q6_K|http://172.26.0.1:8081/v1|2560"
INFO [PIPELINE] stage=Search action=group_plan combined_kb_count=1 individual_targets=0 vector_len=0
INFO session_knowledge_qa.go:941[SearchKnowledge] | Knowledge base search completed, found 0 results, rerank outcome: no_candidates
```

即向量检索**静默降级为纯关键词**，随后 0 候选。前端与 API 都只看到「没搜到」，没有任何错误提示。

### 修复

Core 停机后直接改库（`models.parameters` 为 BLOB）：

```python
raw.replace(b"172.26.0.1", b"127.0.0.1")
```

两行受影响：Embedding `ab7c1c1a-92d0-4fe1-a37e-d71ef83afe9c`、Rerank `bbe5b47a-ad60-405a-9d82-89e1feb27ebe`。DeepSeek 云端模型（`https://api.deepseek.com/v1`）不受影响。重启 `start-all.sh` 生效。

> 正规路径是原生 UI「设置 → 模型」修改 base URL。本次机器 API Key 只有 retrieve 权限（`POST /api/v1/sessions` → `Forbidden: API key scope does not allow this operation`），`AGENTRAG_MANAGEMENT_KEY` 对 `/api/v1/models` 也返回 401，因此直接改库。

### 修复后

```json
{"rerank":{"applied":true,"outcome":"ok","threshold":0.2,"effective_threshold":0.2,
          "top_score":0.9995514750480652,"candidate_count":49,"result_count":3}}
```

3 条结果，均为 `中国邮政储蓄银行公司业务上门服务管理办法` 2026/2024 修订版，score `0.9686` / `0.9608` / `0.9501`，响应 26344 字节。

这证明整条跨网络链路可用：Core(WSL) → embedding(Windows 8081) 出向量 → 向量召回 49 候选 → rerank(Windows 8082) 打分 0.9995。

### 结论（需写进运维指南）

**切换 WSL 网络模式后必须同步修正 Core 里已保存的模型 `base_url`**，否则检索会以 HTTP 200 + 空结果的形式静默失效。镜像模式用 `127.0.0.1`，NAT 模式用网关地址。

## 4. 发现二（已修复）：生命周期现状与事件不一致

直连 Host 治理接口验证：

```
POST http://127.0.0.1:8090/bridge/before-retrieve
X-AgentRAG-Bridge-Version: 1
{"request_id":"e2e-mirrored-001","tenant_id":1,"knowledge_base_ids":["164bfd73-..."],
 "knowledge_ids":[4 个 knowledge_id], ...}
```

响应（HTTP 200）：

```json
{"request_id":"e2e-mirrored-001","tenant_id":1,
 "knowledge_base_ids":["164bfd73-d231-4d5c-994e-057ac0cdf48a"],
 "knowledge_ids":[],
 "excluded_document_ids":["f9be1714-...","e12160e1-...","678a8d05-...","c38a328e-..."],
 "deny_all":true}
```

Host 日志新增 `bridge_before_retrieve_received`，说明治理层本身在镜像网络下工作正常，**返回 `deny_all` 是策略按设计执行的结果**。原因在数据：

`data/agentrag_ext.db` 现状：

| 表 | 现状 |
|---|---|
| `ext_lifecycle_current` | 4 行，`state` 全部为 **`stale`** |
| `ext_lifecycle_events` | 8 行 = 4 文档 ×（`""→pending` 初始化 + `pending→active` 解析完成），**没有任何 `→stale` 事件** |
| `ext_lifecycle_decisions` | 8 行，与事件一一对应，**没有任何 stale 决策** |
| `ext_document_annotations` | 4 行，`effective_from`/`effective_until`/`review_due` 全为 `NULL`，`labels_json={}` |
| `ext_lifecycle_rules` | 0 行 |

即：`current.state='stale'`，但 `current.last_event_id` 指向的是那条 `to_state='active'` 的事件；事件表与决策表都没有产生过 stale 迁移，标注里也没有过期时间，代码里唯一写 `state` 的位置是 `extensions/lifecycle/store.go:137-138` 的 `INSERT ... ON CONFLICT DO UPDATE`，它与事件写入在同一事务且使用同一个 `to_state`。

**这违反了 Frozen 不变量 A（`ext_lifecycle_current` 必须可由 `ext_lifecycle_events` 完全重建）。** 表结构上的 FK 只保证 `last_event_id` 指向一条已提交事件，`state` 列只有 `CHECK(length(state) > 0)`，因此「state 与 last_event 的 to_state 不一致」不会被数据库拦住。

后果：**这 4 篇文档全部被排除，任何走治理路径的问答都检索不到内容**（`knowledge-search` 不走 Bridge，所以第 3 节的检索仍然返回 3 条结果）。

### 处置与复验（用户确认后执行）

用户确认该 `stale` 是此前手工改动所致，授权恢复为 `active`。恢复方式是**按 Frozen 不变量 A 从事件表重建 `current`**，而不是直接写一个常量：

1. 对每篇文档取 `MAX(sequence)` 的最后一条已提交事件，得到其 `to_state` 与 `event_id`；
2. 与 `current` 比对。预演输出显示 4 行的 `last_event_id` 全部与事件匹配，**只有 `state` 一列与事件的 `to_state` 不一致**；
3. 把 `state` 改回该事件的 `to_state`（`active`），`last_event_id` 不变；
4. 写后重新校验：4 行全部 `state == 最后事件的 to_state` 且 `last_event_id` 匹配，不一致行数 0。

这一步没有新增任何事件、没有改写历史，只是把被破坏的投影修回事件日志本来就记录的状态。执行前先停栈，避免与 Host 的 WriteExecutor 争用同一 SQLite 写者（Frozen 4.3）。

等价的 SQL 形式（先查后改，改前务必先跑查询确认）：

```sql
-- 1. 每篇文档的最后一条已提交事件
SELECT e.tenant_id, e.knowledge_base_id, e.document_id, e.to_state, e.event_id
FROM ext_lifecycle_events e
JOIN (SELECT tenant_id, knowledge_base_id, document_id, MAX(sequence) AS seq
      FROM ext_lifecycle_events
      GROUP BY tenant_id, knowledge_base_id, document_id) m
  ON m.tenant_id = e.tenant_id AND m.knowledge_base_id = e.knowledge_base_id
 AND m.document_id = e.document_id AND m.seq = e.sequence;

-- 2. 与 current 比对，找出 state 与 to_state 不一致的行
SELECT c.document_id, c.state, c.last_event_id, e.to_state
FROM ext_lifecycle_current c
JOIN ext_lifecycle_events e ON e.event_id = c.last_event_id
WHERE c.state <> e.to_state;

-- 3. 只修 state，last_event_id 不动
UPDATE ext_lifecycle_current
SET state = (SELECT e.to_state FROM ext_lifecycle_events e
             WHERE e.event_id = ext_lifecycle_current.last_event_id)
WHERE state <> (SELECT e.to_state FROM ext_lifecycle_events e
                WHERE e.event_id = ext_lifecycle_current.last_event_id);
```

修复后复验：

| 检查 | 结果 |
|---|---|
| `ext_lifecycle_current` | 4 行全部 `active`，`last_event_id` 与事件一致 |
| Bridge 正向（4 个 active 文档） | `knowledge_ids` 返回全部 4 个，`excluded_document_ids=[]`，`deny_all=false` |
| Bridge 负向（1 个 active + 1 个无记录文档） | 允许 1 个、排除无记录的那个，`deny_all=false`；完整分割成立 |
| `knowledge-search` | 3 条结果，`candidate_count=49`，`top_score=0.9995` |

正向响应：

```json
{"request_id":"e2e-active-002","tenant_id":1,
 "knowledge_base_ids":["164bfd73-d231-4d5c-994e-057ac0cdf48a"],
 "knowledge_ids":["f9be1714-...","e12160e1-...","678a8d05-...","c38a328e-..."],
 "excluded_document_ids":[],"deny_all":false}
```

负向响应（`00000000-0000-0000-0000-0000000000ff` 为无生命周期记录的文档）：

```json
{"knowledge_ids":["f9be1714-..."],
 "excluded_document_ids":["00000000-0000-0000-0000-0000000000ff"],
 "deny_all":false}
```

**加固（已实施）**：表结构上的 FK 只保证 `last_event_id` 指向一条已提交事件，`state` 列只有 `CHECK(length(state) > 0)`，因此「`current.state` 与对应事件 `to_state` 不一致」不会被数据库拦住——这正是本次损坏能发生的原因。已在 schema7 增加一致性守卫，见第 10 节。

## 5. 发现三（未修复）：标签投影全部 dead

`ext_projection_outbox` 8 行全部为 `status='dead'`、`retry_count=5`、`last_error='canonical tag not configured in native KB'`、`processed_at=NULL`。

按 Frozen 不变量 E，投影失败不影响 lifecycle/lineage/governance truth，事实侧确实未被污染；但 `gov:*` 标签投影在本 KB 上从未成功过一次，需要先在该 KB 配置规范标签，或明确接受该投影在本环境不可用。

## 6. 本轮未验证

- **`knowledge-chat` 真实管线**：只有它经过 `chat_pipeline/search_parallel.go` 的门卫。机器 Key 只有 retrieve 权限，缺 chat 权限凭证，本轮未跑通端到端问答。
- **PDF 上传 → Host → MinerU 解析链路**：`/v1/health` 已通，但未实际提交文档走一遍。
- **Core 出站到 DeepSeek 生成**：未触发。

## 7. 命令与退出状态

| 命令 | 结果 |
|---|---|
| `wsl.exe -d Ubuntu -e bash -lc '... bash start-all.sh'` | 0，输出 `[READY] AgentRAG` |
| `curl .../health`（Core 18880、Host 8090） | 0，均 200 |
| `curl .../v1/health`（MinerU 8000） | 0，200，version 4.0.1 |
| `POST /api/v1/knowledge-search`（修复前） | 0，HTTP 200，0 条，`candidate_count=0` |
| `POST /api/v1/knowledge-search`（修复后） | 0，HTTP 200，3 条，`candidate_count=49`，`top_score=0.9995` |
| `POST http://127.0.0.1:8090/bridge/before-retrieve`（修复前） | 0，HTTP 200，`deny_all=true`，4 条全部排除 |
| `python3 tmp-rebuild-current.py`（预演） | 0，报告 4 行需修正，未写入 |
| `python3 tmp-rebuild-current.py --apply` | 0，写入后校验「不一致行数：0」 |
| `POST .../bridge/before-retrieve`（修复后，4 个 active） | 0，HTTP 200，`deny_all=false`，允许 4 个、排除 0 个 |
| `POST .../bridge/before-retrieve`（修复后，含无记录文档） | 0，HTTP 200，允许 1 个、排除 1 个，完整分割成立 |
| `POST /api/v1/knowledge-search`（修复后复验） | 0，HTTP 200，3 条，`candidate_count=49`，`top_score=0.9995` |

## 8. 测试方法注意事项

- 中文 query 必须写入 UTF-8 文件后用 `--data-binary @file` 提交；在 Git Bash 里用 `-d '中文'` 会变成乱码（日志出现 `���ŷ��������취`），检索结果无意义。
- 用英文 query（如 `shangmen service`）得到 `outcome: all_below_threshold`、`top_score≈0.05`，属于正常的语义不匹配，不是故障。
- MinerU 的健康探针是 `/v1/health`，`/health` 返回 404。

## 9. 已知限制（沿用，未改变）

- Agent 直接读、图谱独立入口、旧 handle 仍未纳入治理（POST-M0-GOV）。
- `knowledge-search` 不经过 Bridge，只有 `knowledge-chat` 经过。
- Host 启动日志出现 `reconciliation_failed`：Core 尚未就绪时的对账失败属预期行为，会由后续周期重试。

## 10. 加固：schema7 生命周期一致性守卫（2026-11-01）

第 4 节的损坏之所以能发生，是因为 `ext_lifecycle_current.state` 与它指向事件的 `to_state` 之间没有任何数据库级约束：外键只保证事件存在且在同一个 tenant / 知识库 / 文档范围内，`state` 列只有 `CHECK(length(state) > 0)`。任何直接 `UPDATE` 都能写下一个没有任何事件产生过的状态，而所有约束仍然成立。

### 改动

| 文件 | 内容 |
|---|---|
| `extensions/store/migrations/007_lifecycle_current_consistency.sql` | 四条 `BEFORE INSERT/UPDATE` 触发器：`*_matches_event` 用 `RAISE(ABORT)` 拒绝与所指向事件 `to_state` 不符的 `state`；`*_heads_latest_event` 拒绝把 `last_event_id` 指向该文档非最新的已提交事件。每条 `CREATE` 前先 `DROP TRIGGER IF EXISTS` |
| `extensions/store/migrate.go` | 嵌入 007，版本上界 6 → 7；`version < 7` 只负责盖版本号，守卫 DDL 改为**每次可写打开都重放** |
| `extensions/store/manual_schema.go` | 手工 schema 认领的迁移前缀列表加入 007 |
| `extensions/cmd/review-alerts/main.go` | 只读 CLI 接受 schema 5–7（原为 5–6），仍然拒绝未知的更高版本 |
| 测试 | `lifecycle/store_test.go` 新增守卫正反用例；`store` 与 `review-alerts` 的版本断言随之上移 |

设计取舍：

- **用触发器而不是复合外键**。让 `state` 参与外键需要给 `ext_lifecycle_events` 加 `(…, event_id, to_state)` 唯一索引并重建 `ext_lifecycle_current` 表；触发器不需要重建表，也不会改变既有写入路径。
- **缺失事件交给外键**。触发器带 `EXISTS` 前置条件，事件不存在时保持沉默，让外键报错——两条约束各自保留独立的失败语义，`TestCurrentForeignKeyRejectsMissingAndCrossScopeEvent` 仍然测的是外键。
- **不回溯修复**。迁移只加守卫，不改写已有行。已不一致的行维持原状（fail-closed，文档继续被排除），必须按第 4 节的流程从事件日志重建。迁移不静默改事实。
- **仍然没有覆盖「`last_event_id` 是否最新」——已在同一版本号内补齐**。只用「state 匹配」做判据时，把 `last_event_id` 回指到同一文档的旧事件仍然通过：状态与所指事件自洽，投影却落后于日志。副本实测确认了这个盲区（把 `f9be1714` 的指针从 seq 2 改回 seq 1，`UPDATE` 被接受）。补上的 `*_heads_latest_event` 要求所指事件的 `sequence` 是该文档最大者，两条规则分别报错，因为它们是不同的损坏。
- **守卫按代码而不是数据对待，因此不新增版本号**。修订一个已发布的迁移，如果只靠 `version < 7` 触发，已经停在 schema7 的库永远拿不到新的触发器体。改为每次可写打开都重放（`DROP IF EXISTS` + `CREATE`），新旧库在同一条路径上收敛；代价是每次打开多 8 条 DDL，换来「触发器定义永远等于代码里的定义」。只读 CLI 不经过 `migrate`，不受影响。

### 验证

| 检查 | 命令 | 结果 |
|---|---|---|
| 格式 | `gofmt -l .` | 无输出 |
| 构建 | `go build -mod=readonly ./...` | 退出 0 |
| 静态检查 | `go vet -mod=readonly ./...` | 退出 0 |
| 模块门禁 | `python scripts/check_module_boundary.py` | 退出 0，312 imports，无 WeKnora 依赖 |
| 全量测试 | `go test -mod=readonly ./... -count=1` | 退出 0，全部包通过 |
| 真实库迁移 | 重启 Host 后查 `ext_schema_migrations` 与 `sqlite_master` | `MAX(version)=7`（未新增版本号），四条触发器齐全 |
| 已停 schema7 库的升级 | 复制真实库副本，用 `store.Open` 打开后查触发器 | 副本原只有两条旧触发器，打开后自动补齐为四条 —— 重放路径生效 |
| 副本回指旧事件 | 副本上把 `current.last_event_id` 改回 seq 1 的 `e318a33e`（`to_state=pending`） | 被拒：`constraint failed: ext_lifecycle_current.last_event_id must be the latest committed event of the document (1811)`；`state` 仍为 `active` |
| 真实库守卫 | 对运行中的库执行 `UPDATE ext_lifecycle_current SET state='stale'` | 被拒：`IntegrityError: ext_lifecycle_current.state must equal the to_state of last_event_id`；写入后 `state` 仍为 `active` |
| 真实库一致性 | 联表核对 `state` 与 `to_state`、以及是否存在更新的已提交事件 | 两问均 0 行 |
| 迁移后功能 | Bridge `before-retrieve` 正向 4 个 active 文档 | 允许 4、排除 0、`deny_all=false` |
| 迁移后功能 | Bridge `before-retrieve` 负向（1 active + 1 无记录） | 允许 1、排除 1，完整分割成立 |
| 迁移后功能 | `knowledge-search` | 7 条结果，`candidate_count=49`，`rerank.applied=true`，`top_score=0.9996` |

真实库守卫测试前先整库备份、测试后删除备份，确认写入被拒且数据未改变。

### 注意

`docs/design/M3-Trusted-Sources-2026-11-01.md` 原计划把 `007` 留给 `extensions/lineage/` 的版本族迁移。M3 尚未实施（`extensions/lineage/` 不存在），`007` 已被本次守卫占用；该文档已同步改为「当前最高 7，实施采用 008」，T2 门禁里的迁移编号与升级基线一并从 007/schema6 改为 008/schema7。
