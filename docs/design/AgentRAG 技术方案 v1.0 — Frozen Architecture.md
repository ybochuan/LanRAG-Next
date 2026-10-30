# AgentRAG 技术方案 v1.0 — Frozen Architecture

**Architecture Status: FROZEN**
后续发现的问题默认归类为 Source Mapping / Implementation Difference / Baseline Limitation，不再重新讨论 Architecture。

## 一、项目定位与核心决策

### 1.1 目标

在 WeKnora Lite 之上构建 AgentRAG，实现：文档生命周期管理、版本族血缘 DAG、文档规范性/语义关联、局域网 mDNS 发现、反馈驱动的自我提升、MinerU 4.0 分路解析、单空间多知识库下的差异化治理、反馈回归正式知识的修订闭环。

### 1.2 核心决策（Frozen）

| 决策 | 内容 | 冻结理由 |
|---|---|---|
| **基座** | WeKnora Lite（单空间、多知识库，SQLite + sqlite-vec，单二进制） | Lite 是单机轻量形态，运维简洁；**单二进制内嵌 SQLite（FTS5 + sqlite-vec）与内存队列**，数据完全本地 |
| **扩展方式** | **Extension Bridge（4 类 Hook）+ Local Extension Host（IPC）** | 解决 Go module 边界，Bridge 成为稳定 ABI |
| **MCP 工具** | **WeKnora 原生 MCP Service 按需发现** | WeKnora 生产默认“持久目录 + 按需加载”，**不预加载全部工具 schema**；工具由 `tools.MCPTool` 包装以适配内部接口 |
| **摄取信号** | **ingest hook**（Bridge），不走 EventBus | EventBus 事件集中在 Chat/Agent 流式管道 |
| **生命周期写入** | event + current **同事务** + **Transactional Outbox（causation-based）** | 消除崩溃窗口 |
| **标签写入** | **TagProjectionWriter** merge 语义 | `PUT /knowledge/tags` 是整体替换，直接调用会清空业务标签 |
| **治理 Audit** | **与状态迁移同事务** | 不变量 B |
| **Lineage Edge 分级** | **Critical Edge 同步事务；Non-critical Edge Async** | 不变量 D |
| **版本族** | `ext_document_version_family` / `ext_document_version` 作为事实源 | 父关系由 lineage edge 表达，避免双事实源 |
| **Revision Publish** | **幂等 Saga + Remote Reconciliation** | 不变量 C |
| **SQLite 单写者** | 仅保证 `ext_*` 表单写者；上游遵循 WeKnora 原生并发模型 | 收窄表述 |
| **量化** | feature flag 实验，API 以 `vec_quantize_i8` 为准 | sqlite-vec 仍处 **pre-v1**，BASELINE 升级需重新校验 |
| **GraphRAG 成本** | 预算化指标（calls / tokens / estimated_cost） | 避免硬编码 |
| **Provenance API** | **`/provenance` 与 `/version-ancestry` 分离** | 两个语义问题不应一次递归混答 |

## 二、五条工程不变量（冻结条款）

**不变量 A：事实状态一致性**

`ext_lifecycle_current` 必须可由 `ext_lifecycle_events` 完全重建，且 `current.last_event_id` 必须引用一个**已提交**的 lifecycle event。**数据库层通过 FK 强制**：`ext_lifecycle_current.last_event_id REFERENCES ext_lifecycle_events(event_id)`。

**不变量 B：治理动作可审计**

任何治理状态迁移——`approve` / `reject` / `quarantine` / `publish` / `supersede` 等——必须与其对应的 Audit Event **在同一事务中提交**。Async audit 只能用于非治理的 trace / telemetry。

**不变量 C：跨系统发布幂等 + Remote Reconciliation**

任何涉及 WeKnora ingest 的 Revision Publish，必须通过**幂等 Saga**完成，且 **retry 前必须先执行 remote reconciliation**。未知远端结果不得直接重放副作用操作。

**不变量 D：治理关键关系原子化**

`publish` / `supersede` / normative relation（`supplements` / `amends` / `interprets`）**不能依赖最终一致的普通异步 lineage queue**。这类边必须与对应的 lifecycle 状态、governance 状态、audit 同事务提交。只有可重建/可推断的边（`derived_from` / `semantic_auto related_to` / parser enrichment）允许 Async。

**不变量 E：投影永远不是事实源**

`gov:*` tag / UI cache / sidecar index 均可重建；它们的失败**不得改变** lifecycle / lineage / governance 的 truth。

**数据责任分层**：

```
Truth
├─ Lifecycle Events / Current
├─ Governance State + Audit
├─ Version / Lineage Critical Edges
└─ Revision Saga State

Projection / Cache
├─ gov:* tags
├─ sidecar vectors
├─ UI cache
└─ derived relation metadata
```

## 三、Extension Bridge + Local Extension Host

### 3.1 调用/构建边界

WeKnora 是独立 Go module `github.com/Tencent/WeKnora`；`extensions/` 位于该 module 外部。

```
┌─────────────────────────────┐
│  WeKnora Lite 进程           │
│  （go.mod 不依赖 extensions）│
│  ┌───────────────────────┐   │
│  │  Extension Bridge     │   │  ← 4 类 Hook 的 localhost HTTP client
│  │  （极薄，仅 DTO 转换）│   │
│  └───────────────────────┘   │
└──────────┬──────────────────┘
           │ localhost HTTP（后续可替换为 UDS / named pipe）
           │ DTO 契约不变
┌──────────┴──────────────────┐
│  Local Extension Host        │
│  （独立进程 / Go module）    │
│  ├─ lifecycle                │
│  ├─ lineage                  │
│  ├─ feedback                 │
│  ├─ revision                 │
│  ├─ taggov                   │
│  ├─ discovery                │
│  └─ retrieval policy         │
└──────────┬──────────────────┘
           │
           ├─ SQLite（ext_* 表，WriteExecutor 单写者）
           ├─ Sidecar 向量索引
           └─ MCP Server（供 WeKnora 原生 MCP 按需发现）
```

**4 个 localhost HTTP 端点**：

| 调用 | 端点 |
|---|---|
| before_retrieve | `POST /bridge/before-retrieve` |
| after_retrieve | `POST /bridge/after-retrieve` |
| before_generate | `POST /bridge/before-generate` |
| ingest_lifecycle_event | `POST /bridge/ingest-event` |

**收益**：WeKnora `go.mod` 不依赖 extensions；extensions 完全不 import WeKnora 内部包；Bridge 成为真正的稳定 ABI/API 边界。

### 3.2 降级策略（冻结）

| Hook | Extension Host 不可用 |
|---|---|
| **before_retrieve** | **fail-closed**：治理库不可用时不得绕过 `active` 状态过滤 |
| **after_retrieve** | **fail-open**：仅缺少关联增强元数据 |
| **before_generate** | **fail-open**：仅不注入 guidance |
| **ingest_lifecycle_event** | **不允许静默丢失**：进入 durable retry / reconcile |

### 3.3 Bridge Invocation Matrix（冻结）

| Hook | 快速问答 | 智能推理（Agent） |
|---|---|---|
| `before_retrieve` | `CHUNK_SEARCH_PARALLEL` 前 | `search_knowledge` 执行前 |
| `after_retrieve` | `FILTER_TOP_K` 后 | `search_knowledge` 返回 Observe 前 |
| `before_generate` | `INTO_CHAT_MESSAGE` 后 | 每轮 LLM call 前 |
| `ingest_lifecycle_event` | ingest pipeline | 同左 |

### 3.4 Public DTO 冻结字段

```
request_id / session_id / mode / agent_round
tenant_id / knowledge_base_ids / knowledge_ids / tag_scopes
original_query / rewritten_query / results[] / token_budget / metadata
```

### 3.5 MCP 按需发现（不占用 Bridge Hook）

`query_related_docs` 与 `discover_peers` 以 **Local MCP Server** 形式运行在 Local Extension Host 内，通过 WeKnora 原生 MCP Service 配置注册。WeKnora 生产默认起始只向模型提供 `discover_mcp_tools`，取得完整定义后才暴露对应函数与 `call_mcp_tool`，**不会把所有上游 schema 一次性发送给模型**。工具在内部由 `tools.MCPTool` 包装以适配 `types.Tool` 接口，因此**不需要 PATCH `ToolRegistry`**。

**M0 记录**：Local MCP Server 使用 **Streamable HTTP**，端点 `http://127.0.0.1:<port>/mcp`，配置本机 token/auth。WeKnora 支持 SSE 与 HTTP Streamable 两种 transport；**stdio 被严格禁用**，因此不使用 stdio bridge。

### 3.6 Bridge 协议版本

HTTP 层使用 `X-AgentRAG-Bridge-Version: 1`，Extension Host 对不支持的 major version fail-fast。DTO 字段保持冻结，协议版本只承载传输层兼容性。

### 3.7 Bridge 超时与健康检查

`GET /healthz`、`GET /readyz`。四类 Hook 的超时行为严格按 3.2 降级策略。

### 3.8 核心 PATCH 面（恒定）

**1 个 ingest bridge + 2~3 个 QA pipeline hook + 1 个 UI route bootstrap + 1 个 public extension contract**。MCP 走原生能力，不计入 PATCH 面。

## 四、数据库与事务架构

### 4.1 SQLite 配置基线（连接级）

| PRAGMA | 值 | 说明 |
|---|---|---|
| `journal_mode` | `WAL` | 允许多个 reader 与 writer 并行存在，但任一时刻仍只能有一个 SQLite write transaction |
| `synchronous` | `NORMAL` | WAL 下安全 |
| `busy_timeout` | `5000` | 连接级 |
| `foreign_keys` | `ON` | 连接级 |
| `cache_size` | `-64000` | 单用户足够 |
| `wal_autocheckpoint` | `1000` | 自动 checkpoint |

`WriteExecutor` 持有 **dedicated SQLite connection**，并在该 connection 初始化时设置连接级 PRAGMA。**不能只在应用启动时执行一次然后默认整个 pool 永久生效**。

### 4.2 ext SQLite 物理文件拓扑（M0 实现选择）

当前倾向 **`weknora.db` + `agentrag_ext.db` 独立文件**。所有必须原子化的数据（lifecycle / revision / audit / critical lineage / outbox）都在 ext 侧；Tag 和 WeKnora ingest 通过 projection/Saga 跨系统协调。拆文件让“`ext_*` 单写者”变成物理意义上的单写者，减少与 WeKnora ingestion 抢同一个 SQLite write lock。**这不改变 Frozen Architecture**。

### 4.3 WriteExecutor 范围收窄

**只保证 `ext_*` 表只有一个 writer。** 上游 WeKnora 仍会写 `documents`、`chunks`、`embeddings`、`sessions`、`messages` 等表，遵循其原生 SQLite 并发模型。

### 4.4 M0 SQLite 并发验收（流程化判据）

```
先以 concurrency=2 进行：
  2 文档并发 ingest + 1000 ext feedback writes + lifecycle transition + tag projection

若 upstream baseline 有竞态：
  记录基线问题（写入 M0 报告） → 降至 ingest concurrency=1 → 重新验收

验收判据：
  在最终采用的并发策略下，不出现未处理的 SQLITE_BUSY、
  constraint failure 或数据不一致。
```

### 4.5 生命周期事务 + Transactional Outbox

```sql
BEGIN IMMEDIATE
  INSERT ext_lifecycle_events (...)
  UPSERT ext_lifecycle_current (last_event_id = :event_id)
  INSERT ext_projection_outbox (
    projection_type = 'tag_projection',
    aggregate_type  = 'document',
    aggregate_id    = :document_id,
    causation_type  = 'lifecycle_event',
    causation_id    = :event_id,
    status          = 'pending'
  )
COMMIT
```

### 4.6 Outbox 表（causation-based）

```sql
ext_projection_outbox (
  outbox_id         TEXT PRIMARY KEY,
  tenant_id         TEXT NOT NULL,
  knowledge_base_id TEXT,
  projection_type   TEXT NOT NULL,   -- tag_projection / relation_tag / family_tag
  aggregate_type    TEXT NOT NULL,
  aggregate_id      TEXT NOT NULL,
  causation_type    TEXT NOT NULL,   -- lifecycle_event / lineage_edge / document_version
  causation_id      TEXT NOT NULL,
  payload_json      TEXT NOT NULL,
  status            TEXT NOT NULL,   -- pending / processing / done / dead
  retry_count       INTEGER NOT NULL DEFAULT 0,
  next_retry_at     TIMESTAMP,
  last_error        TEXT,
  created_at        TIMESTAMP NOT NULL,
  processed_at      TIMESTAMP,
  UNIQUE(projection_type, aggregate_type, aggregate_id, causation_id)
);
```

**三类 causation**：`lifecycle_event → event_id`（状态标签）、`lineage_edge → edge_id`（关联标签）、`document_version → version_node_id`（版本族标签）。Outbox 天然幂等，无 version 歧义。

### 4.7 Outbox 验收判据

- restart 后 pending 可继续处理；
- 无永久 orphan；
- healthy 状态下 `oldest_pending_age < 30s`；
- `dead` 项全部可观察、可人工 retry。

### 4.8 治理状态迁移与 Audit 同事务

`ext_feedback_staging.status` / `ext_feedback_guidances` / `ext_revision_proposals` 的所有治理迁移，**状态更新与 Audit Event 在同一事务提交**。

### 4.9 L1 丢失指标

验收指标改为 **“正常 DB 健康状态下，accepted-but-not-committed 的 p99 age < 50ms”**，或引入 disk-backed spool。

### 4.10 Lifecycle FK 强化不变量 A

```sql
ext_lifecycle_current (
  ...
  last_event_id TEXT NOT NULL,
  FOREIGN KEY(last_event_id) REFERENCES ext_lifecycle_events(event_id)
);
```

数据库层直接禁止 `last_event_id` 引用不存在的 event。

## 五、Lineage Edge 分级

| 边类别 | 边类型 | 写入方式 | 理由 |
|---|---|---|---|
| **Governance-Critical** | `supersedes` / `merged_into` / `supplements` / `amends` / `interprets` | **Sync transaction** | 不变量 D：治理关键关系不能依赖最终一致 |
| **Reconstructable / Inferred** | `derived_from` / `semantic_auto related_to` / parser enrichment | **Async** | 可从 chunk/revision 关系推断，或可从 parser artifact 重建 |

**代码审查约束（枚举级）** ：

```
EdgeClass(edgeType):
  GovernanceCritical → 强制同步事务
  Reconstructable    → Async
  Inferred           → Async
```

`EdgeClass` 由边类型决定，**不由调用者选择**。

**Revision Publish Finalize（治理关键事务）** ：

```sql
BEGIN IMMEDIATE
  INSERT ext_lineage_node (new_version)
  INSERT ext_document_version (...)
  INSERT ext_lineage_edge (type = 'supersedes')
  INSERT ext_lifecycle_events (old active → superseded)
  UPDATE ext_lifecycle_current
  UPDATE ext_revision_proposals SET status = 'published'
  INSERT ext_revision_audit_log (...)
  INSERT ext_projection_outbox (
    projection_type = 'family_tag',
    causation_type  = 'document_version',
    causation_id    = :version_node_id
  )
COMMIT
```

**这一步成功提交后，系统才可以声称“Revision Published”。**

## 六、标签治理层

### 6.1 保留命名空间

| 前缀 | 用途 | 维护者 |
|---|---|---|
| `gov:state:` | 生命周期状态投影 | 生命周期服务 |
| `gov:domain:` | 知识域 | 用户 + 系统同步 |
| `gov:family:` | 版本族标识 | 血缘服务 |
| `gov:relation:` | 关联类型 | 血缘服务 |
| `gov:parse:` | 解析复杂度标记 | 解析服务 |

### 6.2 TagProjectionWriter（merge 语义）

WeKnora 的 `PUT /knowledge/tags` 是**整体替换**。所有 `gov:*` 写入必须经过 merge writer：

```
读取当前全部标签
  → 分离普通标签 / 现有 gov: 标签
  → 只修改目标 gov: 命名空间
  → 普通标签原样保留
  → 全量 replace
  → 重新读取校验
```

校验失败写 `ext_tag_sync_failures` 并重试，不回滚业务标签。投影由 **Outbox + ProjectionWorker** 驱动，不在生命周期事务中同步执行。

### 6.3 标签过滤的 AND 语义

`tag_ids` 是 OR 语义。`gov:state:active` + `gov:domain:finance` 不能简单传两个 tag_id。长期方案：Extension 先计算 eligible document IDs（取交集），再交给原生 retrieval。

### 6.4 跨库标签映射

`ext_kb_tag_mapping` 维护“规范名 → 各库 tag_id”映射，解决 `KnowledgeTag` 库内作用域导致的同名不同 ID 问题。

## 七、解析层：MinerU 4.0 分路

### 7.1 分路策略

MinerU 4.0 的 tier 语义按文件类型分组：**PDF 和 images 支持全部四档（flash / basic / standard / advanced）；Office、OpenDocument、EPUB、OFD、HTML/MHTML、CSV/TSV 使用本地 Flash 原生解析**。

| 文件类型 | 策略 |
|---|---|
| **PDF / Image** | Cheap Preflight → 选择 `flash` / `basic` / `standard` / `advanced` → **只正式 parse 一次** |
| **DOCX / PPTX / XLSX / Office / HTML / CSV** | **builtin 或 MinerU Flash Native**，**不进入四档分层** |
| **.txt / .md / .rst / .tex** | builtin 直接读取 |

**Cheap Preflight** 使用轻量 PDF 文本层检查 + 页级启发式，**不通过完整 MinerU 解析做预检**。

### 7.2 页级信号与决策规则

**硬信号（`needs_ocr`）** ：`scanned`、`no-text`、`garbled`、`vector-text` —— 任一命中至少 Basic。

**布局信号（`layout`）** ：`multi-column`、`table-likely`、`dense-graphics`、`embedded-images` —— 命中则考虑 Standard。

**决策规则**：零复杂信号 → Flash；命中 `needs_ocr` → 至少 Basic；无 OCR 但命中布局 → Standard；OCR + 高密度布局 → Standard/Advanced；`sparse-text` > 50% → 标记“解析价值低”，人工复核。

### 7.3 阈值标定（按知识库）

**验收标准**（每库独立）：路由准确率 ≥ 85%、高估率 ≤ 10%、低估率 ≤ 15%。

### 7.4 结构化输出契约

`content_list.json` **不作为固定契约**。`ext_parser_artifacts` 保存 `artifact_format`、`artifact_schema_version`、`mineru_version`，优先保存 `middle_json` / `structured_content` 等 V1 API 正式暴露的输出。

## 八、版本族与血缘 DAG

### 8.1 版本族表（事实源）

```sql
ext_document_version_family (
  family_id, tenant_id, knowledge_base_id, family_label,
  root_version_node_id, current_version_node_id,
  created_at, updated_at,
  FOREIGN KEY(root_version_node_id) REFERENCES ext_lineage_node(node_id),
  FOREIGN KEY(current_version_node_id) REFERENCES ext_lineage_node(node_id),
  UNIQUE(tenant_id, knowledge_base_id, family_id)
);

ext_document_version (
  version_node_id, tenant_id, knowledge_base_id,
  family_id, document_id, content_hash,
  source_uri, source_format, created_at,
  FOREIGN KEY(version_node_id) REFERENCES ext_lineage_node(node_id),
  FOREIGN KEY(family_id) REFERENCES ext_document_version_family(family_id)
);
```

**父关系统一由 `supersedes` / `merged_into` / `derived_from` 边表达**，不存 JSON `parent_version_ids`。

### 8.2 Node 注册表

`ext_lineage_node` 作为统一节点注册表（`document_version` / `chunk` / `parser_artifact` / `chunk_revision`），`ext_lineage_edge` 正常 FK 到 node。

### 8.3 Provenance SQL（方向修正 + cycle guard）

`derived_from` 语义：**派生节点 → 来源节点**。从答案 chunk 反向溯源，从 `from_node_id = answer_chunk_id` 出发，沿 `to_node_id` 向上走。

```sql
WITH RECURSIVE provenance(node_id, edge_type, depth, path) AS (
  SELECT e.to_node_id, e.edge_type, 1,
         '|' || e.from_node_id || '|' || e.to_node_id || '|'
  FROM ext_lineage_edge e
  JOIN ext_lineage_node src ON src.node_id = e.from_node_id
  JOIN ext_lineage_node dst ON dst.node_id = e.to_node_id
  WHERE e.from_node_id = :answer_chunk_id
    AND e.edge_type IN ('derived_from', 'merged_into', 'supersedes')
    AND (:include_cross_kb = TRUE
         OR (src.knowledge_base_id = :current_kb AND dst.knowledge_base_id = :current_kb))
  UNION ALL
  SELECT e.to_node_id, e.edge_type, p.depth + 1,
         p.path || e.to_node_id || '|'
  FROM provenance p
  JOIN ext_lineage_edge e ON e.from_node_id = p.node_id
  JOIN ext_lineage_node src ON src.node_id = e.from_node_id
  JOIN ext_lineage_node dst ON dst.node_id = e.to_node_id
  WHERE p.depth < 10
    AND instr(p.path, '|' || e.to_node_id || '|') = 0
    AND (:include_cross_kb = TRUE
         OR (src.knowledge_base_id = :current_kb AND dst.knowledge_base_id = :current_kb))
)
SELECT * FROM provenance;
```

`instr` 路径检查防止手工治理数据错误造成的环。

### 8.4 Provenance API 拆分

| API | 边类型 | 回答的问题 |
|---|---|---|
| **`/provenance`** | 仅 `derived_from` | “这段答案从哪里来？” |
| **`/version-ancestry`** | `supersedes` + `merged_into` | “这个版本是怎么演变来的？” |

UI 上分开呈现：引用面板的“溯源”按钮走 `/provenance`；版本族图上的“版本历史”走 `/version-ancestry`。

## 九、文档关联与 GraphRAG

### 9.1 两类关联

| 类型 | 隔离层级 | 机制 | 写入方式 |
|---|---|---|---|
| **规范性关联** | **可跨库** | 用户显式声明 → `supplements` / `amends` / `interprets` | **Sync** |
| **语义关联** | **默认库内**，跨库需显式确认 | 自动发现 + 治理层确认 → `related_to` | Async 候选 / Sync 转正 |

补充通知**不进同一版本族**。版本族是“同一逻辑文档的线性演进”，补充通知是独立文档，用 `supplements` 边表达依附关系。

### 9.2 关联强度校准

`confidence = w_sim · sim_score + w_decl · declaration_signal + w_struct · structural_signal`

**权重冷启动校准（M4，按库）** ：20–50 组人工标注的关联文档对跑网格搜索。**验收：精确率 ≥ 0.70、召回率 ≥ 0.60**。未达标时优先调整结构信号规则。

### 9.3 GraphRAG 开启条件（预算化）

GraphRAG **默认关闭**。开启条件需同时满足：该库 `related_to` 召回率 < 60%；评估集中 ≥ 30% 属“答案跨多文档”；**用户接受该 KB 当前估算预算**（看板展示 `graph_index_llm_calls` / `input_tokens` / `output_tokens` / `estimated_cost` / `index_duration`）。

**监控回退**：若 Recall 提升 < 3 个百分点或 `estimated_cost` 超预算 150%，自动回退关闭该库。

### 9.4 `query_related_docs`（MCP 按需发现）

| 项 | 内容 |
|---|---|
| **暴露方式** | **Local MCP Server** → WeKnora 原生 MCP Service → `discover_mcp_tools` / `call_mcp_tool` **按需发现** |
| **触发条件** | “仅当检索结果元数据 `related_edge_types` 包含 `supplements` / `amends`，且用户问题涉及具体条款、生效时间或适用范围时调用。” |
| **治理闸门** | 仅返回 `active` 文档的关联边；低置信自动边过滤；跨库默认不返回 |

## 十、问答环节

### 10.1 两条路径与 Bridge 注入

- **快速问答**：`before_retrieve` 附加 `gov:state` / `gov:domain` 过滤；`before_generate` 注入指导层。
- **智能推理**：`search_knowledge` 外层挂载过滤；MCP 工具 `query_related_docs` 按需调用；每轮 `before_generate` 注入指导。

### 10.2 指导注入规则

| 机制 | 规则 |
|---|---|
| 库内优先 | 当前库指导优先注入 |
| 跨库降权 | 跨库指导 `confidence × 0.8` |
| 会话级去重 | 同一 `guidance_id` 每会话最多 **2 次** |
| 单轮上限 | 最多 **3 条** |
| 会话总预算 | 总 token ≤ **800** |
| 缓存前缀分离 | 指导作为 **ephemeral addition** 追加消息末尾 |

### 10.3 Agentic 推理效率

轮次硬上限 **3–4 轮**。`after_retrieve` 做**结果摘要 + 相关性标注**（Contextualization 优先于 de-duplication）。

### 10.4 反馈时序

反馈绑定本轮 `retrieved_chunk_ids`，写入 `ext_feedback_events`（WriteExecutor Async，带 `knowledge_base_id`）。**同一轮问答内部，反馈不回写当前轮**。

## 十一、mDNS（opaque capability）

TXT 记录只包含：`service_id`、`role=rag`、`protocol_version`、`capability_hash`、`api_port`。**不广播** `kb_domain` / `kb_id` / 知识库名称。发现节点后通过**已认证 HTTP** `/extensions/discovery/capabilities` 获取允许展示的知识库信息。**一个 AgentRAG Node = 一个 mDNS service**。静态节点列表始终保留为回退路径。

## 十二、反馈驱动学习与 Revision Proposal

### 12.1 三层隔离

| 层 | 内容 | 是否被反馈修改 |
|---|---|---|
| **事实源** | WeKnora 原始知识库 | **只读，永不修改** |
| **索引层** | FTS5 + sqlite-vec | **不写入反馈** |
| **指导层** | `ext_feedback_guidances` + sidecar 语义索引 | 反馈的唯一归宿 |

### 12.2 冷启动

真实反馈积累前，手动种入 **3–5 条种子指导**（`source=seed`，置信度 0.75）。

### 12.3 治理状态与 Audit 同事务

`pending_review → approved / rejected / quarantined` 等所有治理迁移，**状态更新与 Audit Event 在同一事务提交**。

### 12.4 审批路径与防自欺四机制

| 路径 | 触发条件 | 流程 |
|---|---|---|
| 快速通道 | 低风险 | 自动批准 → 冷静期 24h → 激活，抽检池（10%–20%） |
| 标准审批 | 中风险 | 人工审批 → 冷静期 48h → 激活 |
| 严格审批 | 高风险 | 人工审批 + **二次确认** + 冷静期 72h → 激活 |

1. **冷静期**：强制等待，期间可撤回；
2. **二次确认**：高风险条目激活前再次确认；
3. **抽样复核**：快速通道条目按比例抽检；
4. **影响面熔断**：同主题被再次负面反馈 ≥ 2 次 → 自动降级 `quarantined`（按库生效）。

### 12.5 Revision Proposal 数据模型

```sql
ext_revision_proposals (
  proposal_id, tenant_id, knowledge_base_id,
  source_document_id, source_version_id,
  trigger_guidance_ids, proposed_patch, evidence_refs, impact_score,
  status,                     -- pending_review / approved / publishing / publish_failed / indexed / published / superseded / rejected
  publish_attempt_id,         -- AgentRAG 侧幂等键
  publication_key,            -- 跨系统幂等键
  target_content_hash,
  target_document_id,
  publish_error,
  reviewed_at, published_at, created_at
);
```

### 12.6 Revision Publish Saga 状态机

```
pending_review → approved → publishing
                              ├─ publish_failed（可重试）
                              └─ indexed
                                    ↓
                                 published
                                    ↓
                                 superseded
```

### 12.7 跨系统幂等 + Remote Reconciliation

```
publication_key = SHA256(proposal_id + source_version_id + approved_patch_hash)
target_content_hash = SHA256(approved_patch_normalized)
```

**恢复逻辑**：

```
status = publishing / unknown
  ↓
先 reconcile remote state
  ↓
是否已存在 publication_key / content_hash 对应的远端文档？
   ├─ YES → 绑定已有 target_document_id → 继续 Saga
   └─ NO  → 才允许重新 ingest
```

**审计标准提问**：不问“WeKnora 有没有 idempotency API”，而问 **“当前 BASELINE 能提供哪种 remote identity，使我们实现 Frozen Architecture 要求的 reconciliation？”** 如果 BASELINE 无 metadata，使用 deterministic filename / source marker / content hash fallback。

### 12.8 PDF/DOCX 的 Publish 边界

| 模式 | 行为 |
|---|---|
| **manual_content** | 系统基于 approved patch 创建新版本 |
| **binary_source** | 系统生成修订建议 / diff → 用户上传修订后的正式源文件 → ingest → 建立新版本 |

**系统可以提出 PDF/DOCX 的修订建议，但不自动声称自己已经修改正式源文件。**

## 十三、UI 页面

| 路径 | 页面 | 关键组件 |
|---|---|---|
| `/platform/lifecycle` | 生命周期 | 库切换 + 状态分布 + 事件时间线 + 待复核/待归档 |
| `/platform/lineage` | 血缘/关联图 | 库切换 + “仅本库/包含跨库” + AntV X6 DAG；**溯源与版本历史入口分离** |
| `/platform/associations` | 关联候选 | 按库分组 + 确认/忽略；跨库单独分组 |
| `/platform/tags` | 标签治理 | `gov:*` 分布、跨库映射、膨胀告警、`ext_tag_sync_failures` |
| `/platform/revisions` | 修订提案 | 提案列表、审核、**Saga 发布状态 + reconcile 结果 + `publish_failed` 重试** |
| `/platform/feedback-governance` | 反馈治理 | 待审队列、已激活指导、种子指导、隔离区、熔断告警 |
| `/platform/ops` | 运维 | 全局 + 按库下钻；容量哨兵；**Outbox 积压 / oldest_pending_age / dead 列表**；队列健康；量化 flag；GraphRAG 预算 |
| `/platform/eval` | 评估 | 按库检索基线、Agentic 指标、质量回归、关联权重校准 |
| `/platform/peers` | 局域网 | opaque capability + 认证后能力查询 |

## 十四、评估体系（按库独立）

| 层级 | 内容 | 门禁 |
|---|---|---|
| **检索基线**（M1.5） | 每库 30–50 查询 Recall@5/10/20、MRR | Recall@5 ≥ 0.85、MRR ≥ 0.80（或基线 95%） |
| **Agentic 评估**（M4） | 每库 15–20 多跳问题，轮数、工具序列、人工评分 | 轮次上限 3–4 |
| **生成质量回归**（M5） | 每库 30–50 查询人工评分 | 不低于上次通过阈值 |

量化验证为 **feature flag**，验收只保留 **Recall@10 退化 ≤ 2%**；存储/延迟改善作为测量结果。API 以 **`vec_quantize_i8`** 为准，sqlite-vec 仍处 pre-v1，BASELINE 升级时需重新校验。

## 十五、里程碑

| 阶段 | 周期 | 交付 | 关键验证 |
|---|---|---|---|
| **M0 基线与 Bridge** | 2.5 周 | Lite 基线冻结；**Bridge（4 类 Hook）+ Local Extension Host（IPC）+ DTO 冻结**；**Transactional Outbox（causation-based）+ ProjectionWorker**；WriteExecutor（`ext_*` 单写者）；**Lifecycle FK 强化**；TagProjectionWriter 原型；CI 门禁；SQLite 并发冒烟测试 | demo extension 走通三 Hook 全链路且不 import internal；**Outbox 崩溃恢复测试**（restart 可继续、无永久 orphan、`oldest_pending_age < 30s`）；**SQLite 并发按流程化判据通过**；`last_event_id` FK 违反时插入被拒绝 |
| **M1 解析层** | 2 周 | MinerU PDF/Image 四档路由 + Office native Flash/builtin；只 parse 一次；按库阈值标定；`ext_parser_artifacts`（含 schema_version） | 每库路由准确率 ≥ 85%、高估率 ≤ 10%；Office 不走 Standard |
| **M1.5 检索优化** | 1 周 | 全局 RRF 网格搜索；重排降级；查询扩展门控；每库检索基线；int8 量化 feature flag + Recall 对比 | 量化 Recall@10 退化 ≤ 2%（flag）；CI 无原生 RRF 修改 |
| **M2 生命周期** | 3 周 | 状态机含 `scheduled`；标注体系；判定层；**event + current + Outbox 同事务**；Document stale / Guidance stale 拆分；补充通知时效联动；库级规则覆盖 | 不变量 A 通过故障注入验证；Outbox 验收按 4.7 判据 |
| **M3 血缘 DAG** | 3 周 | `ext_lineage_node` + `ext_document_version_family` / `ext_document_version`；**修正 provenance SQL + cycle guard**；**`/provenance` 与 `/version-ancestry` 拆分**；**Lineage Edge 分级**；反向溯源 API | 从答案 chunk 正确反向走到 document_version；环数据不导致无限递归；Critical Edge 崩溃注入后无状态不一致 |
| **M4 文档关联与 Agentic 评估** | 3 周 | 规范性边声明（Sync 写入）；按库权重校准；**`query_related_docs` 走 WeKnora 原生 MCP 按需发现**；GraphRAG 预算化；每库 Agentic 评估 | 精确率 ≥ 0.70、召回率 ≥ 0.60；GraphRAG 默认关闭 |
| **M5 反馈治理 + Revision Proposal** | 5.5 周 | 冷启动种子；采集 → 裁决 → 准入 → 审批 → 指导层；**治理状态与 Audit 同事务**；多轮注入预算；**Revision Proposal + 幂等 Publish Saga + Remote Reconciliation** | **不变量 B / C / D 通过故障注入验证**；`feedback → guidance → revision proposal → publish → supersedes` 闭环可走通 |
| **M6 mDNS + 可视化** | 2 周 | opaque mDNS + 认证 HTTP；`DiscoverPeersTool`；血缘/关联图；生命周期；标签治理；修订提案看板；运维（**Outbox 积压 / dead 列表**）；评估 | 静态节点回退可用；mDNS TXT 不含 kb 元数据 |

## 十六、M0 Change Map 必填记录项

| 项 | 内容 |
|---|---|
| **WeKnora BASELINE** | tag + commit hash，固定在 `BASELINE` 文件 |
| **Bridge 4 类 Hook 插入位置** | 每个 Hook 对应的 WeKnora 源文件、函数、行号 |
| **ingest hook 调用点** | 解析 / 分块 / 索引 / 失败 各自对应的源码位置 |
| **Outbox + ProjectionWorker 落位** | Extension Host 内的 package 结构 |
| **TagProjectionWriter 调用链** | 从 Outbox Worker 到 WeKnora `PUT /knowledge/tags` 的完整路径 |
| **MCP Server 注册方式** | WeKnora MCP Service 配置项与实际生效路径；**transport = Streamable HTTP，端点 `/mcp`，本机 token** |
| **Bridge 协议版本** | `X-AgentRAG-Bridge-Version: 1`；Extension Host 对不支持的 major version fail-fast |
| **ext SQLite 物理文件拓扑** | `ext_db_path`、`same-file / separate-file`、选择理由、锁竞争基准、备份/迁移方式 |
| **ingest_lifecycle_event 恢复策略** | 方案 A / B / C 选择；稳定幂等键 `source_event_id + document_id + stage + attempt` |
| **Bridge 超时参数** | `healthz` / `readyz`；四类 Hook 的超时值与降级行为 |
| **SQLite 并发基线验证结果** | concurrency=2 的实际表现；是否降至 1 |
| **核心 PATCH 面清单** | 1 ingest bridge + 2~3 QA hook + 1 UI route + 1 public contract 的具体文件与行号 |
| **实现差异清单** | 审计中发现的与冻结架构不一致的地方，以及对应的 Change Map 决策 |

## 十七、冻结声明与后续行动

### 17.1 冻结范围

本版（v1.0 Final）冻结：五条工程不变量；Extension Bridge + Local Extension Host 的调用/构建边界与降级策略；4 类 Hook + Invocation Matrix + Public DTO 字段；MCP 走 WeKnora 原生按需发现；Transactional Outbox（causation-based）+ ProjectionWorker；Lineage Edge 分级；版本族与血缘事实源；`/provenance` 与 `/version-ancestry` 语义分离；Revision Proposal + 幂等 Saga + Remote Reconciliation；MinerU 4.0 分路策略；mDNS opaque capability；M0–M6 里程碑。

### 17.2 允许 vs 不允许的变化

**允许的变化（Implementation Difference）** ：WeKnora ingest 无法写入 idempotency metadata 时的 reconcile 替代方案；Bridge 传输层从 HTTP 切到 UDS；某 API 的实际字段名与 DTO 的对齐；`ext` SQLite 同文件/拆文件选择。这些走 Change Map 记录，**不修改冻结架构**。

**不允许的变化（Architecture 重构）** ：改变五条不变量、改变 Bridge 边界、改变 Lineage Edge 分级、改变 Revision Saga 幂等模型、改变版本族事实源。这些需要新的设计评审版本。

### 17.3 下一阶段工作契约

**下一份文档是《M0 Change Map / WeKnora Lite Source Mapping》，不是 RC2.2、RC3。**

```
固定 WeKnora BASELINE
  ↓
逐文件源码审计
  ↓
建立 Requirement → Source → Hook → Patch → Test 映射
  ↓
输出 M0 Change Map
  ↓
只允许发现“实现差异”
不允许擅自重构冻结架构
  ↓
开始 M0
```

**验收标准**：Source Mapping 完成后，能明确回答“核心 PATCH 是否真的只有 2~3 个 QA hook + 1 个 ingest bridge + 1 个 UI route + 1 个 public contract”，而不是继续靠架构猜测。

**架构状态：FROZEN。进入 Source Mapping。**

