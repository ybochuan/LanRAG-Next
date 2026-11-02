# AgentRAG 外部线索快捷采集器与流转方案 (LanRAG-Quick v1.2)

> 状态：**Draft，草案，未获实施授权**。本文不构成任何改动 `internal/`、Frozen、原生表或根 `go.mod` 的许可。
> 取代：`AgentRAG 外部线索快捷采集器与流转方案 (LanRAG-Quick v1.1).md`（已废弃，仅作历史记录）。
> 上位文档：`AgentRAG 技术方案 v1.0 — Frozen Architecture.md`（本方案不改其任何条款）。
> 基线：上游 `WeKnora v0.9.0` / `upstream_commit f17ee8a`，`frozen_at 2026-10-29`（见 `BASELINE.txt`）。
> 边界自查：本文件所有结论来自源码直读；本轮 shell 不可用（`ops_bash` 报 workspace 前置条件失败），`git status/diff` 与 HEAD 未核实，标为未验证项。

---

## 0. 修订摘要（v1.1 → v1.2）

| # | v1.1 写法 | 问题 | v1.2 处理 |
|---|---|---|---|
| 1 | 采集优先调原生 `POST /knowledge/url` | `createKnowledgeFromURL` 在审批**之前**落库并 `Enqueue(documentProcess)`（`internal/application/service/knowledge_create.go:412-427,485`），内容已入库可检索 | 删除该路径。**审批前目标 KB 内不得出现任何文档**，改用 `manual` 创建在此次审批通过时一次完成 |
| 2 | 抓取内容生成 `ext_feedback_events` + 反思 Agent 出 Diff | Frozen 的反馈事件绑定 `retrieved_chunk_ids`（`Frozen:493`），语义是问答质量反馈；仓库无“反思 Agent” | 删除。改用自建 `ext_clip_record`（迁移 010），Diff 不做 |
| 3 | `ext_feedback_staging` 记 `ingest_status` | 该表不存在；迁移只有 001–009 | 删除。复用 `ext_ingest_observations` |
| 4 | 后台 Worker 每 10 秒轮询 `parse_status` | `ext_ingest_observations.origin` 有 `CHECK(origin='reconcile')`（`extensions/store/migrations/002_ingest_taggov.sql:9`），另起写者写不进；且已有 `ingest.Reconciler` 周期运行（默认 1 分钟） | 删除。**只读**现有观察结果 |
| 5 | 调 `TagProjectionWriter` 打 `gov:source:*` | 投影读标签遇 null/未标签直接 `ErrAmbiguousTags` 拒绝（`extensions/taggov/http_store.go:14`），新建文档必失败 | 创建时用 `tag_ids` 直接带原生 tag ID |
| 6 | 写 `derived_from` 溯源原 URL | `RecordDerived` 两端必须是 lineage 节点（`extensions/lineage/edges.go` `requireNode`），URL 不是节点 | 来源写 `custom_metadata`（命名空间 `agentrag`）；不做“URL→文档”血缘边 |
| 7 | 写 `supplements` / `amends` 边 | `amends` 是 governance_critical，明确无通用写入器（`extensions/lineage/edges.go:37-38`） | `supplements` 只走既有管理接口；`amends` 不做 |
| 8 | “原子性流转” | 跨原生 HTTP + 本地 DB + 文件系统，无原子可言；且状态回写放在原生创建之后，崩溃即孤儿 | 改为 **intent-first Saga**，含 adopt-or-escalate 恢复 |
| 9 | `cmd/desktop-quick/` 落在根 module，新增 `golang.design/x/hotkey` | 改根 `go.mod`/`go.sum`，违反 `AGENTS.md` 2.4 | 全部落在 `extensions/`；热键与托盘用 `x/sys/windows`，不引 GUI 框架 |
| 10 | “仅 127.0.0.1” 即安全边界 | 8090 的 4 个 hook 路径无鉴权（`extensions/bridge/server.go:62-67`），本地 HTTP 无鉴权等于对本机任意进程开放 | 独立监听 + Bearer + Origin 白名单，复用 modelsupervisor 形态 |
| 11 | `data/clips/{timestamp}_{title}.md` | `data/.gitignore` 无 `clips/` → 抓取内容进版本控制；标题未净化可路径注入 | `data/clips/{clip_id}.md` + 补 `.gitignore` |
| 12 | “Web 端审批台与治理工作台融合” | 治理工作台不存在（前端只有 `/platform/lifecycle`、`/platform/model-supervisor`） | 新建 `/platform/clipper`，直连 Host |

---

## 1. 定位与不变量

**定位**：外部内容（网页 / 划词 / 未来本地文件）的**本地采集前端 + 准入审批台**。它本身不做治理裁决，只把“采到的东西”按治理流程交给人放行，然后调用**既有原生接口**完成入库。

四条不变量，实现必须可验证：

1. **I1 审批前不可见**：在任何人工放行动作完成之前，目标知识库内不得出现该内容对应的文档，也不得产生解析任务。验证方式是查原生库，不是查本地表。
2. **I2 快照唯一事实源**：入库正文逐字节等于审批时预览的本地快照；原生侧不得因 `source_url` 重新抓取任何东西。
3. **I3 单一写入路径**：入库只用既有原生 REST 接口（`manual` 创建 + `custom_metadata` 更新）。不新增原生表、不新增原生解析入口、不改 `internal/`。
4. **I4 失败不改写结论**：采集失败、标签缺失、血缘接口不可用，一律显式报错并停在当前状态，不得静默降级（例如无标签入库、假装已关联）。

---

## 2. 总流程

```
[触发] 热键 / 浏览器扩展 / 托盘
   │
   ▼
[采集] 本地抓取 → 提取正文 → 写快照文件 + 计算 sha256
   │                                  状态: captured
   ▼
[审批] /platform/clipper 预览（正文、来源、目标 KB、标签、关联）
   │                                  状态: approved（人已放行）
   ▼
[入库] ① 先落 approving(意图)
      ② POST /knowledge-bases/:id/knowledge/manual  status=publish
      ③ PUT  /knowledge/:id  custom_metadata{agentrag{...}}
      ④ 提交 created + document_id
   │                                  状态: created
   ▼
[观察] 只读 ext_ingest_current / ext_ingest_observations 展示解析结果
   │
   ▼
[生命周期] 原生无 lifecycle 记录的新文档默认被检索排除；
           由操作员在既有生命周期界面推进，本方案不自动置 active
```

---

## 3. 采集端

### 3.1 通道

| 通道 | 第一版 | 说明 |
|---|---|---|
| 全局热键 | ✅ | 默认 `Ctrl+Shift+S`，可改；注册失败（被占用）必须**显式提示并按原值回滚**，不静默换键 |
| 浏览器扩展 | ✅ | 选中文字 / 当前页 / 链接右键；走 Host 本地接口 |
| 托盘 | ✅ | 设置、手动粘贴、退出 |
| Windows 文件右键 | ❌ 二期 | 见 §15：扩展 HTTP client 目前只有 `JSON()`（`extensions/weknora/client.go:45,54`），无 multipart 上传能力 |

热键与托盘用 `golang.org/x/sys/windows`（`RegisterHotKey` / `Shell_NotifyIcon`），**不引入 Wails、systray、`golang.design/x/hotkey`**，避免动根 `go.mod` 与新增第三方门面依赖。

### 3.2 本地抓取

采集由**本机进程**发起，不经过原生 `/knowledge/url`。因此原生 SSRF 校验（`secutils.ValidateURLForSSRF`）不会替我们兜底，必须自带一份等价防护：

- 仅 `http` / `https`；解析后对**全部** A/AAAA 记录校验，拒绝 loopback、私有段、link-local（含 `169.254.169.254`）、组播、未指定地址。
- 重定向最多 3 跳，**每跳重新校验**最终 IP；禁止跳转到 `file:`/`data:`。
- 连接与读取超时、响应体上限（默认 8MiB）必须硬编码可配，超限即失败，不截断后当成功。
- 抓不到正文 → 失败，不落半截快照。

HTML → Markdown 的清洗：移除 script/style/iframe，输出仅保留 Markdown 白名单语法；转换结果为空或长度 < 200 字符视为失败。

---

## 4. 快照与暂存

### 4.1 落盘

- 目录：`data/clips/`（Host 数据目录下，与 `agentrag_ext*.db` 同级）。
- 文件名：`{clip_id}.md`，`clip_id` 为服务端生成的有序 ID（ULID 或 `时间戳-随机串`），**不使用标题**。标题含 `../`、`:`、`*`、`?` 或超长时不得影响落盘路径。
- 写入方式：临时文件 → `fsync` → 原子 `rename`；随后计算 `sha256` 与字节数，写入 `ext_clip_record`。
- 权限：目录与文件仅当前用户可读写。

### 4.2 必须补的忽略规则

`data/.gitignore` 现有 13 条，**不含 `clips/`**。本方案实施的第一步就是在该文件加入：

```
clips/
```

验收判据：抓取若干条后 `git status --short --untracked-files=all` 不出现任何 `data/clips/` 路径。

### 4.3 COPY-04 边界（重要）

快照是**被采集内容的副本**。`M2-Copy-Lineage-2026-11-01.md:35` 已把 Web 缓存/页面快照列为必须继承来源的副本：只要 Agent 的 `read_file` / workspace reader 能读到该目录，它就成了一条绕过治理的读取通道。

因此：

- 快照目录**不得**位于任何 Agent 工作区根之下；实施时以 `internal/agent/tools/read_file.go` 的真实 workspace root 做一次显式否证（列出 workspace root，断言 `data/clips` 不在其中）。
- 若未来确实需要 Agent 读取快照，必须按 COPY-04 继承来源并重新核验当前许可，不能因为“是本机文件”直接放行。

### 4.4 保留与清理

- 保留期默认 180 天（可配）；只清理 `abandoned` / `failed`，以及 `created` 且文档已被原生删除的记录。
- `approved` / `approving` 状态的快照**永不自动删除**（否则重试无正文可用）。
- 清理任务删除文件前先置状态并记审计，删除失败保留文件与记录，不掩盖。

---

## 5. 审批台

前端新增路由 `/platform/clipper`（`frontend/src/router/index.ts`，紧邻 :101-102 两个既有扩展页面），API 走裸 `fetch` + Host CORS 白名单，**不复用 `@/utils/request`**（那条链路会带 Core JWT 并加 `/api/v1` 前缀，参考 `frontend/src/api/modelSupervisor.ts` 的先例）。

弹窗/页面展示：

```text
┌──────────────────────────────────────────────────────────────┐
│ 📥 外部线索 → 准入审批                                        │
├──────────────────────────────────────────────────────────────┤
│ 来源     https://example.com/…                               │
│ 采集于   2026-11-01 23:40:12 (+08:00)                        │
│ 快照     sha256:3f9a…  ·  118.4 KiB / 12,904 字符             │
│          ⚠ 正文上限 200,000 字符；上传上限 50 MiB（全局，可配）│
│                                                              │
│ 目标知识库  [ 财务制度库 ▼ ]                                  │
│ 文档标题    [ 2026年最新差旅报销补充规定                      ] │
│ 来源标签    [x] gov:source:web   [x] gov:doctype:external    │
│             （标签须已在目标库内通过原生界面创建）            │
│                                                              │
│ 关联内部知识（可选，走 supplements 管理接口）                  │
│   [ ] 《2024差旅报销管理办法》 —— 关联类型 固定为 supplements │
│                                                              │
│ 入库后                                             [待确认 ▼] │
│   待确认：本方案默认，文档入库后仍被检索排除，需在生命周期界面 │
│           推进                                              │
│                                                              │
│ 内容预览（只读，即入库正文）                                  │
│   # 2026年最新差旅报销…                                       │
│                                                              │
│                          [ 取消 ]   [ 确认入库 ]              │
└──────────────────────────────────────────────────────────────┘
```

规则：

- **大小提示必须来自真实约束**：`MAX_FILE_SIZE_MB` 默认 50（`internal/utils/filesize.go:9`）是**全局**上传上限且运行期可配，不是“目标知识库的限制”；正文上限是 `manualContentMaxLength = 200000` 字符（`internal/application/service/knowledge_create.go:784`）。两者都要显示，且服务端二次校验，前端提示不作数。
- 预览内容**直接读快照文件**，不经任何重新抓取。
- 目标 KB 下拉只列出当前凭据可写（Editor 及以上）的知识库。
- 血缘区块在 supplements 接口不可用时**整块禁用并说明原因**，不得渲染成“已关联”。
- 取消 = 置 `abandoned`，不调用任何原生写接口。

---

## 6. 入库（intent-first Saga）

### 6.1 顺序（不可调换）

| 步 | 动作 | 落库/外部效果 | 失败处理 |
|---|---|---|---|
| 0 | 校验快照仍存在且 `sha256` 与记录一致 | 无 | 失败即拒绝，状态回 `captured` |
| 1 | 本地提交意图 | `state=approving`，`attempt_at=now` | 事务失败则不发起任何外部调用 |
| 2 | `POST /api/v1/knowledge-bases/{kb}/knowledge/manual`，`status="publish"` | 原生新建文档（唯一副作用） | 明确 4xx → `state=failed`，保留错误正文；超时/网络错误 → **保持 `approving`**（结果未知） |
| 3 | `PUT /api/v1/knowledge/{id}`，`custom_metadata` | 原生文档挂来源 | 失败记 `last_error`，状态仍可 `created`（来源可后补），但 UI 必须标黄 |
| 4 | 提交 `state=created` + `document_id` | — | SQL 失败 → 重启后走恢复 |

第 2 步的请求体（`internal/types/knowledge.go:328-339`）：

```json
{
  "title": "2026年最新差旅报销补充规定",
  "content": "<快照全文，逐字节>",
  "status": "publish",
  "tag_ids": ["<原生标签ID>", "<原生标签ID>"],
  "channel": "browser_extension"
}
```

`channel` 取 `browser_extension`：`internal/types/knowledge.go:187-188` 明确 `Channel` 用于标识采集通道并给出了该取值。

第 3 步（`internal/handler/knowledge.go:2353-2359`，部分更新、需 Editor 权限）：

```json
{
  "custom_metadata": {
    "agentrag": {
      "clip_id": "01JB…",
      "source_url": "https://…",
      "source_kind": "url",
      "captured_at": "2026-11-01T15:40:12Z",
      "snapshot_sha256": "3f9a…"
    }
  }
}
```

注意两点：`custom_metadata` 是**整体替换**语义，因此创建后只写一次，之后不得再改写（避免覆盖用户后续在原生界面维护的字段）；用 `agentrag` 子键命名空间，不与用户自定义字段抢占。

### 6.2 为什么不是“原子流转”

链路跨原生 HTTP、本地 SQLite 与文件系统，没有任何事务能覆盖。所以设计目标不是原子性，而是**任何一步崩溃后状态可判定、且不产生重复文档**。`AGENTS.md` 5.4 的要求在此体现为：第 1 步本地意图未提交时，不得发起第 2 步。

### 6.3 崩溃恢复（adopt-or-escalate）

启动时扫描 `state='approving'` 的记录：

1. 读取 `attempt_at` 与标题，在目标 KB 内枚举 `channel=browser_extension` 且创建时间落在 `[attempt_at-5min, now]` 的文档（原生列表分页在顶层，见 `PROJECT_CONTEXT.md:195`）。**枚举必须有上限**（如最近 200 条 / 10 分钟窗口）。
2. **标题精确匹配且唯一** → 认领：写 `document_id`，补第 3 步，置 `created`。
3. **零匹配** → 说明第 2 步未生效，可安全重试一次（重试前重新校验快照哈希）。
4. **多匹配或超出枚举上限** → **升级为人工处置**：置 `failed` 并写明确错误「存在 N 个候选文档，请人工确认后关联」，**禁止猜测**。

`manual` 创建没有幂等键参数，重复调用必然产生重复文档，所以恢复逻辑必须先把“有没有创建成功”判定清楚，再决定是否重试。

### 6.4 失败判据

- `401/403` → 凭据或权限问题，`failed`，提示重新登录/换库。
- `400`（标题非法、内容超长） → `failed`，附原生错误文本。
- 5 秒超时（扩展 client 固定上限，`PROJECT_CONTEXT.md:107`） → **结果未知**，保持 `approving`，由 §6.3 判定。
- 原生不可用（连不上 Core） → 保持 `approving`，不重试超过 3 次，之后升级人工。

---

## 7. 标签

- 来源：`ext_kb_tag_mapping` 中该 KB 的 `gov:source:web` / `gov:doctype:external` 映射，取**原生 tag ID**。
- `PROJECT_CONTEXT.md:195`：规范 gov 标签必须在各 KB 通过原生 UI/API **预先创建**，扩展不自动创建。因此首次在某库使用时做一次预检：
  - 缺失 → 阻断审批，给出可操作提示（"请先在目标库创建标签 X 后重试"），并记录预检结果。
  - **不做**“缺标签就无标签入库”的降级。
- 绝不调用 `TagProjectionWriter` 给**已存在**文档做全量替换（`extensions/taggov/http_store.go:14` 的 `ErrAmbiguousTags` 与 `PROJECT_CONTEXT.md:197` 的“无标签首写仍受限”即此约束的现状）。

---

## 8. 来源与血缘

| 需求 | v1.2 做法 | 理由 |
|---|---|---|
| 详情页能看到来源 URL | `custom_metadata.agentrag` | 原生详情页展示该字段，无需改核心 |
| 证明入库正文未被篡改 | 本地 `sha256` + `ext_clip_record` | 与 M3 快照接口"只返回身份和指纹"思路一致 |
| “这份网页源自某内部制度”的可追溯边 | **不做** | URL 不是 lineage 节点；`derived_from` 需要真节点（`extensions/lineage/edges.go` 的 `requireNode`） |
| 补充通知（supplements） | 走既有管理接口：`extensions/management/` + `extensions/lineage/supplements.go` + 原生 `internal/handler/extension_lifecycle.go`（迁移 `009_supplements.sql`） | 同库父文档、≤8 父、深度 ≤10、唯一节点 ≤64、禁自环/重复/环等校验都在既有实现里，不能绕开直接写边 |
| 修订替代（amends / supersedes） | **不做** | governance_critical 无通用写入器（`extensions/lineage/edges.go:37-38`），必须与治理事实同事务（`AGENTS.md` 5.6），本方案不承载 |

若将来确需“URL→文档”的溯源图，应作为独立提案：新增节点类型或证据表，并明确它与 M3 版本族、`ext_source_snapshot_evidence` 的关系，不能借 `derived_from` 蒙混。

---

## 9. 归档状态观察

**不自建轮询器**。解析状态只从既有观察管道读取：

- 数据源：`ext_ingest_observations` / `ext_ingest_current`（`extensions/store/migrations/002_ingest_taggov.sql:1-23`），由现有 `ingest.Reconciler` 按 `AGENTRAG_RECONCILE_INTERVAL`（默认 `1m`，`extensions/host/config.go:47`）写入，`origin` 恒为 `reconcile`。
- 审批台展示：`ext_ingest_current` 里该 `document_id` 的最新 `parse_status`。
- 无记录时显示「等待下一轮对账（≤1 分钟）」，**不得**自行推断 `processing` 或 `success`。
- 若将来需要更快反馈，正确做法是评估调小该 KB 的对账间隔或另批一个带新 origin 值的写入者（需要迁移 + 设计决策），而不是在 clipper 里偷偷写同一张表。

---

## 10. 生命周期与默认不可检索

- `AGENTS.md` 6.1：未有生命周期记录、或任意非 active 文档均被检索排除。新建的 clip 文档天然没有 lifecycle 记录，因此**入库后默认不可被检索**，这是安全默认值，必须在 UI 里讲清楚，不能让人以为“入库=生效”。
- 推进到 active 由操作员在既有 `/platform/lifecycle` 界面完成，本方案不调用任何直接置 active 的接口。
- 是否提供“审批时直接 active”的快捷开关，列为未决问题 Q1（需要 lifecycle 决策事务的正式入口，不能本地伪造）。
- 新增文档的 lifecycle 登记沿用既有 observer，不在本方案里另写一套。

---

## 11. 安全

### 11.1 本地接口

不复用 `127.0.0.1:8090` 的 bridge mux：那 4 个 hook 路径无鉴权（`extensions/bridge/server.go:62-67`），"仅回环"在本地 HTTP 下不构成鉴权（任意本机进程可直接投递、浏览器可发简单跨域 POST、DNS rebinding 可解析到 127.0.0.1）。

按 modelsupervisor 的既有形态做独立监听：

- 独立端口，只绑 `127.0.0.1`（形态见 `extensions/modelsupervisor/manager.go:116`）。
- 凭据：首次运行生成 32 字节随机 token，写 `data/clipper-token`（权限仅本人，且加入 `.gitignore`），请求必须带 `Authorization: Bearer`；比较用常量时间（同 `extensions/management/server.go:76`）。
- Origin 白名单：浏览器扩展的 `chrome-extension://<固定 ID>` + 审批台来源；**拒绝通配符、拒绝带路径、非 http(s)**（校验规则照 `extensions/modelsupervisor/config.go:100-165`）。POST 请求缺 Origin 一律拒绝。
- 方法限定 `POST` / `GET`，`Content-Type` 必须是 `application/json`，请求体上限 4MiB，`Accept` 之外的跨域响应不回显通配 `*`。

### 11.2 数据

- 不在日志、错误消息、`ext_clip_record` 里存正文；日志只记 `clip_id`、字节数、哈希前缀。
- 快照目录加入 `.gitignore`（§4.2），并在验收里实际检查。
- 浏览器扩展不硬编码 token；由用户在扩展设置里粘贴一次，保存在扩展本地存储。

### 11.3 边界

桌面端只与本机 Core / Host 通信，不监听公网地址，不做局域网发现与多设备同步。

---

## 12. 数据模型（迁移 010）

当前最高迁移是 `009_supplements.sql`，本方案新增 `extensions/store/migrations/010_clipper.sql`。

```sql
CREATE TABLE ext_clip_record (
    clip_id           TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    source_kind       TEXT NOT NULL CHECK(source_kind IN ('url','selection','file')),
    source_url        TEXT NOT NULL DEFAULT '',
    title             TEXT NOT NULL,
    snapshot_path     TEXT NOT NULL,
    snapshot_sha256   TEXT NOT NULL,
    snapshot_bytes    INTEGER NOT NULL,
    captured_at       INTEGER NOT NULL,
    state             TEXT NOT NULL CHECK(state IN
                        ('captured','approved','approving','created','failed','abandoned')),
    document_id       TEXT NOT NULL DEFAULT '',
    last_error        TEXT NOT NULL DEFAULT '',
    attempt_at        INTEGER NOT NULL DEFAULT 0,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);

CREATE INDEX idx_clip_record_state ON ext_clip_record(state, updated_at);
CREATE UNIQUE INDEX idx_clip_record_doc
    ON ext_clip_record(tenant_id, document_id) WHERE document_id <> '';
```

约束说明：

- **不存正文**。正文只在快照文件里，DB 只存路径与指纹，避免出现第二份事实源。
- 不建到原生表的 FK（跨库无法保证）。
- 状态机单向：`captured → approved → approving → created`，异常分支 `failed` / `abandoned`；`failed` 可由人工重置为 `approved` 重试，重置必须记审计。
- `document_id` 唯一（部分索引）防止同一原生文档被两条记录认领。

---

## 13. 工程落位

| 组件 | 位置 | 依赖约束 |
|---|---|---|
| 领域逻辑（抓取、快照、状态机、恢复） | `extensions/clipper/` | 只用 `extensions/go.mod` 内依赖；不得 import `internal/`；构建 `go build -mod=readonly ./...` |
| 本地 HTTP 服务 | `extensions/clipper/http.go`（独立监听） | Bearer + Origin 白名单，见 §11.1 |
| 桌面壳（热键/托盘/剪贴板） | `extensions/cmd/clipper-desktop/` | `x/sys/windows`，`//go:build windows`；非 Windows 返回 `ErrUnsupported`（形态同 `extensions/modelsupervisor/winjob/`） |
| 浏览器扩展 | `extensions/ui/browser-extension/` | 纯 JS，无 Go 依赖，不进 Go module |
| 审批台 | `frontend/src/views/clipper/` + `frontend/src/api/clipper.ts` | 裸 fetch；路由 `/platform/clipper` |

硬性约束：

- **根 `go.mod` / `go.sum` 零改动**（`AGENTS.md` 2.4）；不新增 `go.work`。
- 不修改 `internal/`；`git diff -- internal/` 必须为空。
- 模块门禁必须通过：`cd extensions && python scripts/check_module_boundary.py`；`go list -deps -f '{{.ImportPath}}' ./...` 中不得出现 `github.com/Tencent/WeKnora/internal/`。

---

## 14. 必须验证

| # | 场景 | 判据 |
|---|---|---|
| V1 | 采集指向 `127.0.0.1` / `169.254.169.254` / 私有段 / 重定向到私有段 | 全部拒绝，不落快照 |
| V2 | 采集后**未审批**，直接查原生 KB | 目标库无新文档、无新解析任务（I1） |
| V3 | 审批入库 | 文档存在；`custom_metadata.agentrag.snapshot_sha256` 与本地文件一致；正文与快照逐字节一致（I2） |
| V4 | 第 2 步后强杀进程再重启 | §6.3 只认领 1 条，不产生重复文档；歧义时升级人工而非猜测 |
| V5 | 目标 KB 缺 `gov:source:web` 标签 | 阻断并给出可操作提示；**不得**无标签入库 |
| V6 | 入库后解析状态展示 | 必须来自 `ext_ingest_current`；无记录时显示等待，不伪造 |
| V7 | 无权限 / 跨库 / 错 token | 401/403；状态停在 `approved`，无副作用 |
| V8 | 无 Origin、错 Origin、通配 Origin | 全部拒绝 |
| V9 | 抓取 3 条后 `git status --short --untracked-files=all` | 不出现 `data/clips/` |
| V10 | 文档入库后立即检索 | 因无 lifecycle 记录被排除（§10） |
| V11 | 标题含 `../`、`:`、超长 | 落盘路径不变，标题按原生校验结果报错 |
| V12 | 快照文件被外部修改后审批 | 哈希不一致直接拒绝（步 0） |
| V13 | `go build -mod=readonly ./...`、`go vet`、`gofmt -l .`、模块门禁 | 分别退出 0 / 无输出 |
| V14 | `git diff -- internal/` | 空 |

未执行项必须如实注明；不得用“设计通过”替代上述任何一条。

---

## 15. 明确不做

1. 调原生 `/knowledge/url` 采集（会在审批前入库）。
2. `ext_feedback_events` / 反思 Agent / 自动 Diff（属 M5 问答反馈语义，非外部内容准入）。
3. `amends` / `supersedes` 等 governance_critical 边的自动写入。
4. “URL→文档”的 lineage 边（URL 不是节点）。
5. 本地文件（PDF/DOCX）右键入库：需要给 `extensions/weknora` 增加 multipart 能力，列为二期。
6. 局域网 / 多设备同步 / 公网监听。
7. 自动把新文档置为 active。
8. 修改 Frozen、原生表、检索/解析/调度算法。

---

## 16. 未决问题

| # | 问题 | 影响 |
|---|---|---|
| Q1 | 是否允许“审批时直接 active” | 需要 lifecycle 决策事务的正式入口；在此之前只能是“入库后人工推进” |
| Q2 | 浏览器扩展固定 ID 与打包方式 | 决定 Origin 白名单的具体取值，须在打包后确认 |
| Q3 | 快照保留期与合规要求 | 影响 §4.4 的默认值 |
| Q4 | 二期文件采集是否需要 multipart | 决定是否扩展 `extensions/weknora/client.go` |
| Q5 | clip 记录是否接入 `extensions/review/` 复核告警 | 若接入需另设计告警策略，本方案不夹带 |
| Q6 | 是否在 `TASK_BACKLOG.md` 登记为独立条目 | 未登记前本文件不构成实施授权（`AGENTS.md` 1.4） |

---

## 17. 与里程碑的关系

- **不属于 M5**。Frozen 的 `ext_feedback_events` 绑定 `retrieved_chunk_ids`（`Frozen:493`），其“采集→裁决→准入→审批→指导层”（`Frozen:623`）指问答反馈驱动的指导层修订；外部网页采集没有 `retrieved_chunk_ids`，把它塞进 M5 是分类错误。
- 本方案是**建立在 M2 生命周期 / M3 可信来源之上的外部准入前端**：复用 lifecycle 排除规则、复用 ingest 观察、复用 supplements 管理接口、复用 M3 的“只存身份与指纹”取舍。
- 它不声称任何 M2/M3 的验收结论，也不改变它们的验收状态。
