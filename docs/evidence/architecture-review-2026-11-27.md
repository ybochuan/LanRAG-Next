# AgentRAG 架构与改造状态评估（2026-11-27）

> 性质：只读评估。本轮不修改任何源码、配置或生产数据。
> 评估时间：2026-11-27 22:20–22:50（CST）。
> 基线：`BASELINE.txt` 记录上游 `Tencent/WeKnora v0.9.0 / f17ee8a`；当前 HEAD `dea9fb11d`（`batch6-124-gdea9fb11d`，分支 `sync/v0.9.1`）。
> 方法：`git status/diff/log` + 扩展模块构建/测试/vet/gofmt/模块边界 + 本机回环探针 + 扩展库与原生库只读 SQL 查询 + 既有设计/证据文档交叉核对。

## 0. 结论摘要

1. **工程成熟度高于“自研分支”的平均水平。** 扩展模块已有 3.2 万行源码、2.5 万行测试、41 个 schema 版本；原生侧只在 47 个非测试文件保留薄调用点，RRF、检索、分块、Agent 循环未被重写。
2. **系统的主要风险已从“功能不足”转为“版本漂移、状态漂移、复杂度漂移”。** 上游从 v0.9.0 走到 v0.9.1 及大量上游提交；文档仍写 schema 32/76 篇生产文档，实测 schema 41/1 库 1 文档；91 张扩展表中 53 张为空。
3. **治理主路径是 O(N) 全量枚举 + 双次校验 + 500ms 同步预算。** 当前 1 篇文档无感；上万篇时会同时压延迟、1MiB 请求上限和 fail-closed 可用性。
4. **最该先做的三件事**：迁移器改用 `embed.FS` + checksum、Hook 契约测试、标签前置条件预检。三者都是“地基型”改动，不触碰原生算法。

---

## 1. 本轮验证记录

| 检查 | 目录 | 结果 | 说明 |
|---|---|---|---|
| `git status --short --untracked-files=all` | 仓库根 | 17 个已修改、0 暂存、0 未跟踪 | 含 ASR/mmproj 后端与对应前端/i18n 改动，未提交 |
| `git diff --check` | 仓库根 | 退出 0 | 无空白错误；LF/CRLF 警告来自 Git 自动换行提示 |
| `go version` | `extensions/` | `go1.27.0 windows/amd64` | 根 `go.mod` 声明 1.26.0 |
| `go build -mod=readonly ./...` | `extensions/` | 退出 0 | 全部包编译通过 |
| `go test -mod=readonly ./... -count=1` | `extensions/` | 退出 0，49.7s | 33 个有测试的包全部 `ok`；4 个命令包无测试文件 |
| `go vet -mod=readonly ./...` | `extensions/` | 退出 0 | — |
| `gofmt -l .` | `extensions/` | 无输出 | 含未提交改动在内全部已格式化 |
| `python scripts/check_module_boundary.py` | `extensions/` | 退出 0 | 332 个 import，无 WeKnora 依赖、无启用 workspace |
| `GET 127.0.0.1:8090/readyz` | — | 200 `{"status":"ready","scope":"local"}` | Host 可用 |
| `GET 127.0.0.1:8090/bridge/healthz` | — | 200 `{"status":"ok"}` | 仅存活 |
| `GET 127.0.0.1:18880/health`、`:8092/healthz`、`:8081/health`、`:8082/health` | — | 均连接拒绝 | 探测时 Core、supervisor、embedding、rerank 未运行 |
| 扩展库只读查询 | WSL Ubuntu `python3 sqlite3` | schema 41、91 张表 | 见第 4、5 节 |
| 原生库只读查询 | 同上 | 1 个知识库、1 篇文档 | 见第 5.4 节 |

**未执行（不得视为通过）**：

- 原生 `go build ./...` / `go test ./...`（需 CGO 与 Linux 环境；本轮未跑）。
- `go test -race`（按 `AGENTS.md` 只在具备 C 编译器的 Linux 跑）。
- 前端 `npm test` / `npm run type-check` / `npm run build`（本轮未复跑，引用文档记录时已标注）。
- 真实上传、真实 LLM 问答、真实浏览器 E2E（本轮未跑）。

---

## 2. 版本与工作区状态

| 项 | 值 |
|---|---|
| 基线记录 | `BASELINE.txt`：`Tencent/WeKnora v0.9.0 / f17ee8a`，冻结 2026-10-29 |
| 当前 HEAD | `dea9fb11d`，`git describe` = `batch6-124-gdea9fb11d` |
| 当前分支 | `sync/v0.9.1`（同时指向多个 feature 分支） |
| 上游 tag | 本地已有 `v0.9.0`、`v0.9.1`；`v0.9.1` 不是当前 HEAD 的祖先（同步方式并非直接 merge） |
| 10-29 以来非合并提交 | 291 次，其中 159 次作者为仓库 owner |
| 未提交改动 | 17 个文件：`extensions/modelsupervisor/*`（ASR/mmproj）9 个，`frontend` 模型管理页与 6 个 locale 8 个 |

判断：**当前工作区是一次未收口的 ASR 功能增量**。它已经能编译并通过包测试，但 probe 仍走 `/v1/chat/completions`，只能证明进程可推理，不能证明音频转写可用。

---

## 3. 改造版图（能力矩阵）

| 能力域 | 主要代码 | 状态 | 已有验证 |
|---|---|---|---|
| M0 检索前门卫/Pass-Through | `internal/extensionbridge/client.go`、`chat_pipeline/search_parallel.go` | 历史验收，当前已被 DB 策略取代 | 2026-10-30 真实 WSL E2E（`docs/evidence/m0-e2e-2026-10-30-pass-through.log`） |
| M1 PDF 预检与 MinerU 代理 | `extensions/parser/*`、`internal/infrastructure/docparser/parser_proxy.go` | 已交付，真实 MinerU 4.0.1 验证 | `docs/design/M1-Source-Mapping-2026-10-30.md`、M1 系列证据 |
| M2 生命周期/复核/检索策略 | `extensions/lifecycle`、`review`、`retrieval`、`management` | 已交付，**整体未验收** | `docs/evidence/m2-*`，生产曾以 76 篇文档运行 |
| M3 补充依赖（T3） | `extensions/dependencies`、`lineage/supplements.go` | 已交付 | `docs/evidence/m2-supplements-2026-11-01.md` |
| T4 来源账本 | `extensions/provenance`、`internal/extensionbridge/source_*` | 部分闭环 | 写入侧 producer 已接（2026-11-16）；reserve→写→跨轮读仍受真实输入证据限制 |
| M4/M5 业务关联/反馈/修订 | `extensions/business`、`guidance`、`corrections` | 已交付 | 业务 E2E、真实 Core/Host 隔离运行；Agentic 未人工评分 |
| FAM 业务族/替代/建议 | `extensions/business/family_*.go` | 已交付 | 家族工作台 E2E |
| REL 拆分/废止/条款 | `extensions/business/{split,repeal,clause}.go` | 已交付 | 对应 schema 19–21 测试 |
| ATTACH 提取/归属/换版/删除 | `extensions/parser/attachment`、`attachmentcleanup`、`attachmentworkbench` | 已交付 | 候选 Core/Host 上传与清理 E2E |
| 清理与证据保留 | `extensions/cleanup`、`retention` | 已交付 | schema 34/35 与生产消息中心验证 |
| 阅读中心 | `frontend/src/views/document-reader`、`docs/design/document-reader-zrimo-multiformat-2026-11-25.md` | 已部署 | 前端 2537 通过/2 跳过（文档记录）、两处资源 SHA-256 一致；WebView2 待复核 |
| 模型 supervisor | `extensions/modelsupervisor` | 已交付，ASR 增量未收口 | P1/P2 证据；Windows Job Object、GPU 层数、探针 |
| MCP | `extensions/mcp`、`internal/agent/tools/mcp_tool.go` | 已交付 | 生产注册 `query_related_docs`、`search_archived_documents` |
| 前端治理工作台 | `views/lifecycle`、`business`、`families`、`lineage`、`document-reader` | 已交付 | 六语审计、类型检查、构建；本轮未复跑 |

---

## 4. 代码与数据规模

| 指标 | 数值 |
|---|---|
| 扩展 Go 文件 | 344（源码 184 / 测试 160） |
| 扩展源码行 | 32,145 |
| 扩展测试行 | 24,921（源码的 0.77） |
| 扩展 Go 包 | 37（含 9 个 `cmd`） |
| 扩展 schema 迁移 | 41（`001`–`041`） |
| 扩展库表 | 91（空表 53，非空 38） |
| 原生非测试 Go 文件 | 1,485，其中 47 个引用 `internal/extensionbridge`（3.2%） |
| 原生适配层 | `internal/extensionbridge` 19 个文件，约 95KB |
| 自建前端视图 | 约 14,700 行；最大单文件 `LifecycleView.vue` 1,841 行 |
| 文档 | 设计 30、证据 124、运维 8 |

按包看源码量：`business` 6,969、`parser` 3,499、`modelsupervisor` 2,475、`lineage` 2,083、`provenance` 1,919、`store` 1,733、`host` 1,432、`management` 1,277、`lifecycle` 1,105。

---

## 5. 效果评估

### 5.1 检索质量（文档实测，非本轮复跑）

`docs/evidence/m1-retrieval-baseline-2026-11-16.md`：每库 35 题，零失败。

| 组 | Recall@5 | Recall@10 | Recall@20 | MRR |
|---|---|---|---|---|
| 标题组 | 0.9714 | 0.9714 | 0.9714 | 0.8795 |
| 描述组 | 1.0000 | 1.0000 | 1.0000 | 0.8976 |

均高于冻结判据 Recall@5 ≥ 0.85、MRR ≥ 0.80。注意这是 76 篇规模的单库基线，不代表大规模语料。

### 5.2 存储与启动（文档实测）

| 路径 | `knowledge_bases` P50 | `knowledge_list` P50 | Host Overview P50 |
|---|---|---|---|
| WSL ext4 | 4.2ms | 9.2ms | 7.8ms |
| `/mnt/d` 9p | 200–700ms | 4.0s | — |

把扩展库与原生库迁到 ext4 是本轮性价比最高的性能优化。

### 5.3 Agentic 评估（文档记录）

15 题、376 次工具调用（`read_document` 162、`search_knowledge` 90、`list_documents` 50、`discover_mcp_tools` 42、`call_mcp_tool` 16、MCP 关联 16），14 题产出答案，13 题触达 MCP 发现→调用链；**人工正确性评分未做**。

### 5.4 当前运行观测（本轮实测）

- Host `/readyz` 200；扩展库 schema 41；原生库 1 个知识库、1 篇文档。
- 扩展库：`ext_ingest_current` 1 行、`ext_lifecycle_current` 1 行（active）、`ext_projection_outbox` 2 行（done）、`ext_tag_sync_failures` 10 行、`ext_provenance_runs` 12 行（11 sealed / 1 unattributed）。
- 10 条标签失败的 reason 全部为 `canonical tag not configured in native KB`，按 5s × 5 次烧成 dead letter。
- 与 11-16 记录（76 篇、schema 32、KB `164bfd73…`）不一致；当前配置 KB 为 `df5f5dfa…`。说明文档里的“生产”是历史快照，当前实例是轻量/重建环境。

**效果结论**：单机功能链路有效；规模、真实语料与当前环境的一致性未验证。

---

## 6. 架构上已经做对的四件事

1. **模块边界真实成立。** 扩展 `go.mod` 直接依赖仅 `pdfcpu`、`modernc.org/sqlite`；332 个 import 经脚本核验无 `Tencent/WeKnora/internal`；原生只做 DTO 转换与回环 HTTP。
2. **fail-closed 落到代码，不是文档口号。** `internal/extensionbridge/client.go` 固定 500ms、1MiB、禁代理与重定向、只认 200、严格解码；任何非 200/传输/解码失败都阻断检索。`extensions/retrieval/policy.go` 对“无生命周期事实”的文档默认排除。
3. **事务内核扎实。** `extensions/store/sqlite.go` 单写者 + `BEGIN IMMEDIATE` + gate channel；连接失效后重新预留且**不重放回调**；读连接 `mode=ro&query_only(ON)`；event/current/outbox 同事务；007 触发器堵住“改投影不写事件”和“回指旧事件”两条旁路。
4. **可测试性与证据文化。** 160 个测试文件、测试/源码行比 0.77；今天 33 个有测试的包全绿，耗时 50s；124 份证据文档可按功能追溯。

---

## 7. 风险台账

| ID | 级别 | 风险 | 证据 | 影响 | 建议动作 |
|---|---|---|---|---|---|
| P0-1 | 高 | 上游同步静默丢 Hook | 基线 v0.9.0，HEAD 已吸收 v0.9.1 及大量上游提交；47 个原生文件依赖 Hook；Bridge version 只校验协议 | 编译通过但治理整段失效，且难以发现 | 每个 Hook 一条契约测试；文档按符号锚定；建立 patch series |
| P0-2 | 高 | 文档/环境状态漂移 | `PROJECT_CONTEXT` 仍写“M2 进行中、schema 32”；实测 schema 41、1 篇文档；backlog 生产整改表重复两遍 | 决策基于过期事实，验收口径混乱 | 生成单一 `STATUS.md`；backlog 去重；过期事实加日期戳 |
| P1-1 | 中高 | 治理路径 O(N)+双查+500ms | `search_parallel.go` 每轮 `ListKnowledgeByKnowledgeBaseID` 全量枚举；JSON 上限 1 万 ID / 1MiB；`guardPromptResults` 再查一次 | 大库延迟上升、超限 fail-closed | 版本化快照 + `known_version` 增量；请求内决策复用 |
| P1-2 | 中高 | 迁移器与 schema 演化成本 | `migrate.go` 442 行、41 段重复 if 块；`ext_schema_migrations` 只存版本无 checksum | 新版本改三处、易错；历史脚本被改不可检测 | `embed.FS` 自动注册 + checksum + 基线快照 |
| P1-3 | 中高 | 标签前置条件是隐藏运维坑 | 实测 10 条 `canonical tag not configured` dead letter；无预检、无界面告警 | 治理标签“看似成功、实际未投影”；烧尽重试 | 启动/readyz 预检；前置错误标记 blocked，不消耗 attempts；工作台展示 |
| P2-1 | 中 | 单写者全局串行 | 所有域共用一个写 gate；大批量投影或长事务会阻塞全部写入 | 写入吞吐上限、尾延迟 | 写事务时长预算、分域队列、批量合并 |
| P2-2 | 中 | 测试与验证盲区 | `guidance` 测试/源码 0.27、`corrections` 0.31、`weknora` 0.31；race 仅 Linux；ASR probe 不验证音频 | 关键路径回归靠人工 E2E | 补单测；race CI；音频冒烟或 `audio_verified=false` |
| P2-3 | 中 | 运维安全面 | launcher 中 `SSRF_DISABLE=true` 全局关闭；`agentrag-launcher.json` 明文多组密钥；parser 产物与 `WEKNORA_WEB_DIR` 仍在 `/mnt/d` 9p | SSRF 暴露面变大；凭据泄露风险；I/O 性能回退 | `SSRF_WHITELIST` 精确放行；密钥入凭据管理；产物/前端移 ext4 |
| P2-4 | 低中 | 代码集中度 | `host/runtime.go` 28 个 import / 约 470 行装配；`LifecycleView.vue` 1,841 行；9 个 `cmd` 二进制 | 修改冲突、评审困难 | 按组件拆装配；视图按 tab 拆；CLI 合并为 `agentragctl` |

---

## 8. 优化方案（按 ROI 排序）

### 8.1 迁移器重构（0.5–1 天）

- **目标**：新增 schema 只改一个 `.sql` 文件，且能发现历史脚本被篡改。
- **改动**：`extensions/store/migrate.go` 改用 `//go:embed migrations/*.sql` + `embed.FS`，按文件名排序执行；`ext_schema_migrations` 增加 `checksum` 列（新迁移 042），启动时比对已应用版本；保留 `001`–`041` 的现有 SQL 原文不动。
- **验收**：全新库从 0 到 41 与旧库升级到 41 结果一致；篡改任一已应用脚本时启动拒绝；`go test ./store` 全绿。
- **风险**：旧库没有 checksum 列，需允许“首次回填”而不是拒绝；回填只记录当前文件 hash，不追溯历史。

### 8.2 Hook 契约测试（2 天）

- **目标**：上游 merge 后，任一 Hook 调用点被删除/改名时 CI 立刻红。
- **改动**：复用 `internal/agent/governance_fixture_test.go`、`chat_pipeline/governance_fixture_test.go` 的夹具，为四类 Hook 各写“真实插件执行 → 断言 bridge 被调用且决策生效”的测试；再补一个轻量的“符号存在性”测试（对函数/字段的引用，不写行号）。
- **验收**：临时注释掉任一 Hook 调用，测试失败；恢复后通过；`go test` 不依赖网络与真实 Host。
- **风险**：测试不应复制业务逻辑；只断言“决策被消费”的最小事实。

### 8.3 版本化治理快照（3–5 天）

- **目标**：整库问答不再每次传全量文档 ID。
- **改动**：`before_retrieve` 响应增加 `snapshot_version`；原生按 KB 缓存上一次 allowlist；后续请求带 `known_version`，Host 返回 `unchanged` / `diff` / `full`。版本取“该 KB 最近一次 lifecycle 事件 ID + 对账水位”的最大值。Host 不可达或版本落后超过阈值仍 fail-closed。
- **验收**：稳态请求 payload 从 O(N) 降到 O(1)；新增/失效文档在一个对账周期内可见；断 Host 时检索被阻断；缓存与全量结果做 1000 次随机对拍一致。
- **风险**：缓存语义必须与“完整分割”一致，不能把“未变更”误判为“允许”；版本必须单调。

### 8.4 标签预检与治理健康面板（1–2 天）

- **目标**：把“标签没建”从 dead letter 变成可操作提示。
- **改动**：Host 启动/`readyz` 对每个配置 KB 检查 `gov:*` 规范标签是否存在；新增前置错误类型 `tag_precondition`，Outbox 标记 `blocked` 而非累计 attempts；`management` 增加只读健康摘要，工作台展示 per-KB 状态、outbox lag/dead、最近 reconcile 错误。
- **验收**：空标签 KB 启动后不产生 5 次重试；创建标签后 blocked 项可一键恢复；面板数字与库内计数一致。
- **风险**：预检需用原生只读标签 API，不能自动创建标签（仍遵循 C-10 决定）。

### 8.5 装配层与前端拆分（2–3 天）

- **目标**：降低修改冲突与评审成本。
- **改动**：`host/runtime.go` 按组件拆为 `host/services/*.go`，统一 `New/Close` 与启动顺序；`LifecycleView.vue` 按 5 个 tab 拆子组件 + 共享 composable；9 个 `cmd` 合并为 `agentragctl`（保留 `extension-host`、`model-supervisor` 两个常驻入口）。
- **验收**：行为不变，现有测试全绿；`runtime.go` < 150 行；`LifecycleView.vue` < 600 行。
- **风险**：纯重构需单独提交，不与功能混在一起。

### 8.6 运维硬化（1–2 天）

- `SSRF_DISABLE=true` 改为 `SSRF_WHITELIST` 精确放行本机 MinerU/docreader/模型地址，保留其他出站检查。
- launcher 密钥改用 Windows 凭据管理或至少收紧 ACL；文档只记录键名，不记录值。
- parser artifacts、`WEKNORA_WEB_DIR` 迁到 ext4；保留 `/mnt/d` 仅作备份。
- ASR probe 增加 1 秒静音 WAV 的 `/v1/audio/transcriptions` 冒烟；不做则响应明确 `audio_verified=false`。

### 8.7 单一状态源（1 天 + 每月维护）

- **目标**：消除“文档说 A、实测 B”。
- **改动**：脚本从 `TASK_BACKLOG.md` 条目头 + `docs/evidence/*.md` Front Matter 生成 `STATUS.md`（能力 / 状态 / 验证日期 / 证据链接）；`PROJECT_CONTEXT.md` 只保留稳定事实，动态状态全部指向 `STATUS.md`；删除 backlog 中重复的生产整改表。
- **验收**：任一能力在三处文档中的状态一致；生成脚本可重复执行且 diff 稳定。

---

## 9. 建议排期

| 周期 | 内容 | 产出 |
|---|---|---|
| 本周 | 收口 17 个未提交文件并补 ASR 音频冒烟；标签预检；迁移器重构方案评审 | 干净工作区、可操作告警、迁移器 diff |
| 两周 | 落地迁移器 + checksum；Hook 契约测试；治理健康面板 | CI 门禁、单一健康视图 |
| 一个月 | 版本化快照；host/前端拆分；运维硬化 | 大库可用性、可维护性提升 |
| 持续 | 每月生成 `STATUS.md`；每次上游 merge 跑 Hook 契约测试 | 状态不再漂移 |

---

## 10. 边界与未验证

- 本评估不覆盖 M6 血缘图之外的新功能设计，不构成实施授权。
- 未运行原生 CGO 构建、原生全量测试、race、前端三件套；相关结论引用既有证据文档并标注日期。
- 未访问外部网络、未修改生产库、未签发凭据、未重启服务。
- 扩展库只读查询通过 `file:...?mode=ro`，未执行任何写语句。
- 当前实例的 1 库 1 文档可能是有意重建；本评估只指出“与文档记录不一致”，不推断原因。

## 附录：关键文件索引

- 边界与客户端：`internal/extensionbridge/client.go`、`documents.go`
- 治理消费：`internal/application/service/chat_pipeline/{search_parallel,governance}.go`
- 存储内核：`extensions/store/{sqlite.go,write_executor.go,migrate.go}`
- 策略：`extensions/retrieval/policy.go`
- 标签：`extensions/taggov/{mapping.go,writer.go}`
- 装配：`extensions/host/runtime.go`
- Outbox：`extensions/outbox/{store.go,worker.go}`
- 模型管理：`extensions/modelsupervisor/{spec.go,manager.go,probe.go}`
- 证据：`docs/evidence/m1-retrieval-baseline-2026-11-16.md`、`docs/evidence/m2-*`、`docs/evidence/t4-04-producer-wiring-2026-11-16.md`
