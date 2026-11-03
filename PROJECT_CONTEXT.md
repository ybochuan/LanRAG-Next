# AgentRAG 项目上下文

## M4/M5 后台业务接线（2026-11-02）

- `extensions/business/`复用DB单写者、M3 Registry与M2/T3许可，提供同库规范性关联/语义候选、双向active关联查询、管理反馈与指导、修订提案和手工Markdown发布Saga。GraphRAG默认关闭，默认关联权重未做真实标定。
- 原生GET/PUT `/api/v1/knowledge-bases/:id/lifecycle/business/:business_action`沿用JWT和KB权限，只传业务DTO；`extensions/management/business.go`执行业务。管理列表支持正文/风险/截止时间/完整修订内容、精确ID与20项分页，包括持久关联候选`relation_list`。
- `frontend/src/views/business/`通过原生导航提供`/platform/associations`、`/platform/feedback-governance`、`/platform/revisions`；复用原生JWT，不向浏览器下发Host凭据。页面按当前KB读写权限展示动作；审批核对完整正文、冷静期和高风险二次确认。结果未知时保留原operation_id/内容供重试，结束核对前阻止切页。
- 当前扩展schema11新增业务表/回执/审计；迁移001–010不因本批改变，review CLI识别5–11，12及以上拒绝。原生go.mod/go.sum及原生业务表不因本批改变。
- 反馈仅写指导层：低/中/高风险24/48/72h，后两者需人工审批，高风险二次确认；同主题2次负反馈隔离指导并拒绝再激活，新种子同样隔离。种子confidence=0.75。当前为管理录入和显式激活，未接真实问答事件采集、自动激活扫描或before_generate预算注入。
- 原生manual源可创建新Markdown修订；PDF/DOCX等仍需修订原件上传。发布先持久意向、租约/CAS，再完整远端对账；未知ACK不得重放创建。最后将supersedes边、旧文档superseded生命周期/Outbox、family投影意向、提案状态及审计同事务提交。`lifecycle.AppendTx`复用原生命周期实现。
- 修订链保留逻辑family_id，M3的观察family/current仍按原生文档ID维护。family_tag处理器/真实投影尚未接通；MCP消费、真实QA反馈/指导注入、标定和完整M4/M5验收继续列在backlog。启动/API/页面与自动验证见`docs/operations/M4-M5-业务验收.md`。

## T4 来源底座与未知来源拒绝（2026-11-02，接线进行中）

- 用户已批准 `docs/design/M3-T4-Native-Integration-2026-11-01.md` 整批薄点。`extensions/provenance/` 新增持久run/step/input intent/producer/receipt/audit及迁移010；当前M4/M5又新增schema11，版本识别见上节。007/008/009未改，没有迁移真实库。
- worker在现有回环监听上挂载 `/provenance/` 服务端API，要求版本1、强管理凭据、配置scope及运行能力凭据；probe/缺provider返回503。能力由服务端密钥派生，只保存摘要。文档授权与原生整库授权采用显式不同DTO；缺范围不能扩大为整库。
- 既有source-snapshot可选 `view=components-v1&chunk_ids=...`，1–64个精确chunk；完整v1快照/root不变，组件全部字段hash折叠到其leaf。`extensions/lineage/components.go`核验实际字段与当前root；不返回原文，不写原生表。
- 核验意向先持久化，未核验输入不能先结束步骤；unknown不可洗白，全部步骤/producer完成且源集非空才可封存。副本许可每次展开最新T3父依赖，权限依赖不伪称模型实际输入。
- 原生已增加薄HTTP客户端，以及未知shell、memory、skill、目录、网页、文件/缓存、追加/编辑、后台通知拒绝。work保留原授权ID/状态与取消，省略未知详情且不确认未展示通知；图片门卫早于自动VLM。用户附件保留独立原生授权，原件与任务不删除。
- 普通RAG/Agent已按原生授权targets、会话owner及服务端assistant message ID建立run。最终模型调用登记实际编码消息/选项和原始响应分片指纹；Agent工具有独立step，read_document原始字段、search_knowledge无重排文本可向当前原生快照核验。RAG流由实际消费终态收束，取消/异常不能封存；无法封存的已结束运行中止或保持unattributed。
- 查询改写、重排、FAQ/图像/合并变换、格式化custom metadata和listing的updated_at等未适配组成仍不可封存，不能靠引用或结果hash补齐。外部MCP未知服务摘要/工具schema和pin描述在加载进模型前省略；不改ToolRegistry算法或删除配置，内置知识工具保持原授权与active检查。
- **实际文件写入producer、收集和已封存文件正向读取仍未接通。** 当前文件读取、追加和编辑属于安全拒绝，覆盖写入也不会签发来源。产物收集位于问答调用之外，后续需关联原生消息身份对应的原生产者，不能用新run认领旧文件。真实Core/Host与合成模型联合验证已覆盖实际运行封存、stale及断Host；它不等于真实云模型/文件E2E，T4与M2仍未整体验收；见 `docs/evidence/m3-t4-native-inputs-2026-11-02.md`。

## 补充文档依赖（T3，2026-11-01）

- 生命周期详情的 `SupplementEditor.vue` 提供同库父文档选择；原生 GET/PUT `/api/v1/knowledge-bases/:id/lifecycle/documents/:document_id/supplements` 只做 JWT/读写权限、父子归属和 DTO 转发，actor 由登录上下文生成。Host 管理凭据不进入浏览器。
- `extensions/lineage/supplements.go` 将声明、current revision、父版本、critical supplements edge/linkage、audit/receipt 同事务提交。0 个父文档撤销，最多 8 个；同库、禁止自依赖/重复/环，深度最多10/唯一节点最多64。声明关联独立版本族，不自动变成supersedes。
- `extensions/dependencies/` 统一读取父级状态、日期和当前观察版本；检索每次使用真实Core快照重新核验，最后DB快照重查，未核验来源或并发扩大闭包拒绝。父失效/过期/版本变化拒绝子检索，后台将pending/scheduled/active子文档转stale，不自动复活。
- 父review_due仅形成继承提醒，不改子日期/标签/状态。schema9新增review subject并保留旧提醒，支持没有自身标注的子文档；T4进一步增加schema10，当前版本识别见上节。每日来源刷新任务与Host同生命周期join；初次local ready不依赖Core。
- 007/008保持不变。真实副本完整源集封存和生成中撤销未由T3完成；验收、重编译及隔离E2E证据见 `docs/evidence/m2-supplements-2026-11-01.md`。

## 生命周期管理界面（M2）

- 原生导航 `/platform/lifecycle` 提供库切换、状态与分页、日期、事件时间线、待复核和库级默认规则。页面在 `frontend/src/views/lifecycle/`，请求封装在 `frontend/src/api/lifecycle.ts`。
- 原生 `/api/v1/knowledge-bases/:id/lifecycle` 的JWT接口只授权、核对文档归属并转发；`extensions/management/` 承担查询与配置。统一启动器生成服务端 `AGENTRAG_MANAGEMENT_KEY` 并传给Core/Host；分开启动需两端相同、至少32字节值，浏览器不持有。
- 日期UTC传输，revision CAS保存；规则正文与本库编辑版本同事务读取。默认规则只影响后续初始化。未定义的active未来排期、scheduled清空生效时间安全拒绝；不提供直接状态写入或失效态复活入口。
- 前端shell行为测试在Windows使用Git for Windows的POSIX shell，由测试辅助器从Git安装位置解析；其他平台使用本机shell。辅助器仅用于测试，不进入应用运行路径。

## 本机启动入口（2026-10-31）

- 运维入口：`docs/operations/AgentRAG-启动与运维指南.md`；WSL仓库根执行`bash start-all.sh`。脚本管理Core+Host，Windows MinerU和模型服务单独启动。
- 扩展默认独立库为仓库根`data/agentrag_ext.db`，原生仍使用`data/weknora.db`。相对路径受支持并规范化；Native/混合库继续拒绝接管。无迁移历史的完整已发布DDL前缀，经严格结构比对与FK检查后事务补齐历史；不一致时保留原库并拒绝认领。
- Host worker的`/health`与`/readyz`表示本地DB/服务和首次生命周期扫描就绪，JSON`status=ready, scope=local`；它不依赖Core在线。`/bridge/healthz`仍只证明HTTP存活。一键脚本先等待本地就绪再启动Core，并随后校验原生机器凭证。
- 私有启动配置默认`data/agentrag-launcher.json`；官方登录仅用于首次签发指定KB机器凭据，不保存密码。环境变量优先于已保存配置和默认值。默认私有运行数据由`data/.gitignore`排除。
- WSL网络模式（2026-11-01实测）：已启用WSL2镜像网络（`.wslconfig`含`networkingMode=mirrored`与`autoProxy=false`），`127.0.0.1`在WSL与Windows两侧双向互通，Windows侧MinerU与模型服务保持只绑回环即可。`autoProxy=false`必需：本机残留`ProxyServer=127.0.0.1:7897`会导致镜像网络配置失败并回退`networkingMode=none`（WSL完全无网络）。启用前为NAT，WSL→Windows回环不通。检查与回退步骤见运维指南第5节。
- Windows侧本地模型运行时（2026-11-01）：`data/bin/`已放置llama-server `0.2.0-dev (build 10709)`及其13个依赖文件（含必需的`mtmd.dll`，缺失时进程静默`exit 127`）；`data/models/`已放置`Qwen3-Embedding-4B-Q6_K.gguf`与`qwen3-reranker-0.6b-q8_0.gguf`。两者均由`data/.gitignore`排除。实测embedding 7秒就绪、输出2560维向量，rerank 4秒就绪、`/v1/rerank`判别正确。
- 模型进程管理器 P1（2026-11-01）：`extensions/modelsupervisor/` + `extensions/cmd/model-supervisor/`，独立进程监听`127.0.0.1:8092`，强制鉴权与回环绑定；Windows Job Object 保证 supervisor 退出即清理子进程；`/extensions/models/probe` 是唯一能回答「该地址从 WSL 侧是否真的可达」的接口（`loopback`/`unverified` 两态，`embedding_dim` 来自真实向量请求）。`MODEL_SUPERVISOR_WSL_DISTRO` 必须显式指定，本机默认发行版是 `docker-desktop` 无 `curl`。尚未接入 `start-all.sh`。证据见 `docs/evidence/model-supervisor-p1-2026-11-01.md`。
- 模型进程管理器 P2 控制台（2026-11-01）：`frontend/src/views/model-supervisor/`，入口 `/platform/model-supervisor`；卡片每次刷新都调 `GET /extensions/models/{id}/probe`（`/list` 的 `reachability` 是启动快照，不能直接渲染）；前端用 `frontend/src/api/modelSupervisor.ts` 的裸 `fetch`，**不复用 `@/utils/request`**（那条链路会加 `/api/v1` 前缀、带 Core JWT、打到 Core）；supervisor 增加 CORS 允许列表 `MODEL_SUPERVISOR_ALLOWED_ORIGINS`（默认 `127.0.0.1:18880`/`localhost:18880`），预检在鉴权之前处理。证据见 `docs/evidence/model-supervisor-p2-2026-11-01.md`。
- 模型进程管理器任务类型与模型目录（2026-11-01）：支持 `embedding`/`rerank`/`chat` 三类，默认端口 8081/8082/8083，与 Core `models.parameters` 已存地址对齐；`chat` 不带模式开关（传未知 flag 会让 llama-server 启动即退出）并自动追加 `--jinja`，探测走 `/v1/chat/completions` 且超时 45s（其余 3s）。`GET /extensions/models/catalog` 扫描 `data/models/` 的 `.gguf` 供控制台下拉选择；模型目录缺失只出 `note` 不算错误。显存防呆只对开启 GPU 的实例生效，`use_gpu:false` 不会被 428 拦下。
- 模型进程管理器 GPU 层数（2026-11-01）：`Spec.GPULayers *int`（`gpu_layers`）对应 llama-server `-ngl`，由 `Spec.GPULayerCount()` 统一解析（显式值 > `use_gpu:false`=0 > 默认 99），`/list` 回传解析后的真实值；`use_gpu:false` 配 `gpu_layers>0` 直接拒绝，`-ngl` 属受保护参数不能经 `extra_args` 覆盖。控制台提供「使用 GPU」复选框与层数输入（留空=全部）。实测 `-ngl 30` 部分卸载仍返回 2560 维向量。
- 模型进程管理器运行期加固（2026-11-01）：受保护参数表补齐 `-c/--ctx-size`、`-t/--threads`（规则是「代管的全部参数」而非「记住的几个」）；启动前做端口绑定探测，冲突返回 `409 port_in_use` 且不留下半成品实例；启动失败的 `last_error` 附 stderr 末三行；`Address` 不再携带可达性（地址是 spec 的纯函数，缓存在实例上会在重启后变成空串），可达性移入带 `checked_at` 的 `Health`，由每 30s 的后台巡检写入（`MODEL_SUPERVISOR_SWEEP_INTERVAL`，0 关闭），巡检只汇报健康、绝不改状态；探测超时按任务区分（chat 45s / embedding 20s / rerank 3s），embedding 的 20s 来自实测冷启动 `12.27s → 4.98s → 0.10s`。证据见 `docs/evidence/model-supervisor-p2-2026-11-01.md` 第 8 节。

本文件保存稳定项目事实与已核实的源码位置。强制规则见 `AGENTS.md`，任务及其生命周期见 `TASK_BACKLOG.md`。

## 项目目标与架构依据

当前模型来源治理补充（2026-11-01）：普通RAG的最终上下文及空检索文档描述、Agent search/read/list/schema/analysis和原生MCP文档列表已使用现有Bridge进行active许可校验；Agent检索先过滤再重排。KB聚合description/profile/recent-doc摘要不自动进入模型上下文，原生存储和UI保留。历史答案/压缩/恢复、旧答案文档亲和标题、页面快照、Wiki/图谱混合读取、SQL/会话查询、通用操作与不透明外部MCP结果在M2暂时拒绝或省略，等待完整来源链。普通REST管理、已签资源及显式复制副本属于独立边界。详情见 `docs/design/M2-Governance-Leak-Closure-2026-11-01.md`；旧批次“Agent未治理”的描述保留为历史记录。

AgentRAG 基于 WeKnora v0.9.0，目标运行形态是 WeKnora Lite：单空间、多知识库、单二进制核心、本地 SQLite（FTS5 + sqlite-vec）和内存任务执行器。当前仓库保留完整上游源码，包含标准版、Lite、桌面及其他客户端；“目标为 Lite”不表示仓库只有 Lite。

架构来源：`docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`，状态为 FROZEN。该文档描述目标架构，不等同于已经实现的功能。

目标能力包括文档生命周期、版本族及血缘 DAG、规范性／语义关联、MinerU 分路解析、反馈指导与修订发布、库级治理、mDNS 发现和可视化。

WeKnora 核心通过四类本机 HTTP Hook 与独立 Extension Host 交换公开 DTO：`before_retrieve`、`after_retrieve`、`before_generate`、`ingest_lifecycle_event`。两侧分别构建、分别运行；根 module 不依赖扩展 module，扩展不引用 WeKnora 内部类型。

## 上游基线

| 项 | 已核实值 |
|---|---|
| 基线记录 | `BASELINE.txt` |
| 上游仓库 | `https://github.com/Tencent/WeKnora` |
| 上游 tag / VERSION | `v0.9.0` / `0.9.0` |
| 基线提交 | `f17ee8a8da901bd658a4cb25f40b5fd41adbfdd7` |
| M0已提交基点 | `d1c62cd900f76823b7a9fca0279a97426682e25a`；2026-10-30用户确认验收，M1从此继续 |
| fork | `https://github.com/ybochuan/LanRAG-Next` |
| 基线记录分支 | `baseline/v0.9.0` |
| 基线冻结日期 | 2026-10-29 |

基线定位信息来自现有 `BASELINE.txt` 与 Git tag/commit 的交叉核对。

## 技术栈与模块

| 部分 | 技术／入口 |
|---|---|
| 上游后端 | Go，module `github.com/Tencent/WeKnora`，`go.mod` 声明 `go 1.26.0`；Gin、GORM、dig；入口 `cmd/server/` |
| Lite 存储／检索 | SQLite、FTS5、sqlite-vec；原生部分需要 CGO 和 C 编译器；Lite 构建 tag 为 `sqlite_fts5` |
| 任务执行 | 标准部署有 Asynq/Redis；Lite 使用 `internal/router/sync_task.go` 的内存执行器，包含重试和最终失败回调 |
| 前端 | Vue 3、TypeScript、Vite、Pinia、TDesign；`frontend/package.json`；上游前端 CI 使用 Node.js 24 和 npm |
| 文档处理 | `internal/infrastructure/docparser/`、Go chunker、可选 `docreader/`；包含 builtin、MinerU 等适配实现 |
| Agent／LLM | `internal/agent/`、`internal/models/`、`internal/modelcontext/`；原生 MCP 服务在 `internal/mcp/` |
| 独立扩展 | Go，module `github.com/ybochuan/LanRAG-Next/extensions`，声明 `go 1.26.0`；标准库HTTP、纯Go SQLite驱动 `modernc.org/sqlite v1.61.0`；独立进程入口 `extensions/cmd/extension-host/` |

## 目录职责

| 目录／文件 | 职责 |
|---|---|
| `cmd/server/`、`cmd/desktop/` | 上游服务和桌面入口 |
| `internal/router/`、`internal/handler/` | 路由、HTTP 处理、任务注册及执行入口 |
| `internal/application/service/` | 上游业务服务、问答编排、知识入库与后处理 |
| `internal/application/service/chat_pipeline/` | 普通 RAG 的检索、重排、合并、Top-K、提示拼接和生成插件；包名 `chatpipeline` |
| `internal/extensionbridge/client.go`、`response.go` | 原生薄HTTP客户端：固定回环、500ms、1MiB响应上限、严格身份/集合分割验证；非200/传输/坏响应fail-closed，返回允许集/排除集/deny_all |
| `internal/application/repository/` | 上游数据库仓储和检索仓储 |
| `internal/agent/`、`internal/agent/tools/` | Agent 推理循环与工具实现 |
| `internal/infrastructure/`、`internal/models/`、`internal/types/` | 解析等基础设施、模型适配、内部类型与接口 |
| `frontend/`、`web/` | 前端源码及后端嵌入用 Web 资源 |
| `config/`、`migrations/`、`scripts/` | 上游配置、数据库迁移与构建／运维脚本 |
| `docreader/`、`client/`、`cli/`、`mcp-server/` | 文档读取组件、SDK、CLI 和 MCP 相关组件；部分有独立依赖清单 |
| `docs/design/` | AgentRAG 冻结架构来源 |
| `extensions/lifecycle/events.go` | 独立生命周期事件 DTO，包含身份、作用域、状态转换、原因、时间和来源阶段／尝试信息 |
| `extensions/lifecycle/store.go` | event/current/outbox原子追加、身份冲突拒绝、当前状态及有序事件读取；保留M0低层接口，M2使用同事务guard |
| `extensions/lifecycle/machine.go`、`rules.go`、`stale.go`、`automation.go` | M2内核及自动迁移：标注日期、KB规则/CAS/幂等回执、两类stale；completed Observer与启动补扫/24h定时已接；复核期限变更同事务关闭旧告警 |
| `extensions/store/migrations/004_lifecycle_policy.sql` | 扩展schema4：ext_lifecycle_rules、ext_document_annotations、ext_lifecycle_decisions；标注effective_from/effective_until/review_due为UTC，未配置截止时间默认永久有效 |
| `extensions/review/`、`extensions/store/migrations/005_review_alerts.sql` | schema5复核告警：scope/期限去重、generation重开、同事务Outbox、当前标注复查、本地固定日志；不改变生命周期 |
| `extensions/provenance/`、`extensions/store/migrations/006_copy_provenance.sql` | schema6同步来源账本：不可变源版本/副本清单、CAS当前指针、继承源集、幂等回执及同事务审计；库内读决策逐次检查版本、active、日期和祖先清单。独立于异步derived_from，不是完整M3版本族/DAG |
| `internal/handler/extension_source_snapshot.go` | 原生授权后的只读事务指纹接口，`GET /api/v1/knowledge-bases/:id/knowledge/:document_id/source-snapshot`；completed/ready、完整live chunks、scope/容量/500ms约束；只返回hash，不创建原生版本或修改表 |
| `extensions/lineage/`、`extensions/store/migrations/008_lineage_sources.sql` | 可信原生快照读取、不可变已观察版本/family、当前头、证据/审计；采集至登记在共享写事务内串行。provenance和version-ancestry分边查询，普通编辑不自动supersede；不等于完整M3发布/关键边治理 |
| `extensions/store/lifecycle_current.go`、`007_lifecycle_current_consistency.sql` | 007守卫后续写入；共享读取守卫另核对current与最新事件、区分真新文档与有历史缺投影。损坏旧行拒绝自动初始化，不静默修复事实 |
| `extensions/cmd/review-alerts/`、`extensions/store/read_only.go` | 显式tenant/KB的本地JSON收件箱查询，现有ext库mode=ro/query_only，无建库或迁移；属于本地操作员工具而非多用户鉴权API |
| `extensions/store/migrations/007_lifecycle_current_consistency.sql` | schema7生命周期一致性守卫：`ext_lifecycle_current.state` 必须等于 `last_event_id` 所指事件的 `to_state`，且 `last_event_id` 必须是该文档最新的已提交事件；INSERT/UPDATE各两条触发器，同时堵住「直接改投影而不写事件」和「回指旧事件让投影落后于日志」两条旁路；缺失事件仍由外键拒绝；不回溯修复既有不一致行。守卫按代码而非数据对待：每次可写打开都 `DROP TRIGGER IF EXISTS` 后重建，已停在 schema7 的库同样换上新触发器体，因此这次修订不新增版本号 |
| `extensions/store/` | 独立SQLite、连接级配置、BEGIN IMMEDIATE单写者、只读连接池和嵌入式ext迁移 |
| `extensions/outbox/` | 因果投影队列、带token租约、失败退避/dead/人工重试、队列统计与Worker |
| `extensions/integration/` | 临时子进程退出后的真实SQLite恢复测试；不是上游端到端测试 |
| `extensions/bridge/server.go` | 标准库 HTTP 服务构造；健康探针 |
| `extensions/bridge/protocol.go` | Bridge major version 校验与固定错误响应 |
| `extensions/contract/retrieval.go`、`contracts/bridge/v1/` | 检索前公开DTO、严格JSON解码与共享样例；原生仅发送请求及检查HTTP状态，未消费策略收窄结果 |
| `extensions/bridge/retrieve.go`、`extensions/retrieval/policy.go` | worker按固定tenant/KB查生命周期DB，仅active保留，非active/缺失排除；构造器仍保留历史M0透传组件，但生产Host不再挂载它 |
| `extensions/weknora/client.go` | 绑定单一origin/tenant的公共HTTP客户端，API key在header、拒绝重定向、5秒请求超时和4MiB响应上限 |
| `extensions/ingest/reconcile.go`、`http_source.go`、`observer.go` | 周期完整分页校验、原生公共API列表适配和独立持久观察；不推断删除/中间事件，不写治理active |
| `extensions/taggov/writer.go`、`http_store.go` | 扩展共享标签单写者，原生公开GET/PUT适配，完整read/merge/replace/verify；null标签读取拒绝覆盖 |
| `extensions/taggov/mapping.go`、`projector.go` | 按tenant/KB持久映射原生tag ID；队列内读取当前治理truth，Outbox驱动投影，失败单独持久记录 |
| `extensions/retrieval/exclusions.go` | 从已枚举的授权文档中减去 excluded_document_ids，空结果显式 DenyAll |
| `extensions/parser/router.go` | 纯决策；PDF按用户最新规则有文本即flash/txt、全纯图才basic/ocr、其余拒绝；图片保留原四档信号规则，不改原生引擎/M0门卫 |
| `extensions/parser/preflight.go` | pdfcpu v0.16.4纯Go读取真实PDF对象/内容流；统计页数、结构文本层/纯图页，检查实际文本显示与ActualText，未解析结构树/inline等不授予OCR许可；不是完整Unicode/乱码或质量检测 |
| `extensions/parser/scope.go`、`mineru_proxy.go`、`mineru_upload.go`、`mineru_jobs.go` | scope V1代理；冷/热源字节证明与PDF预检、有效策略先于同进程去重；NativeHandler按可信doc身份创建隔离实例，不提供持久重启恢复 |
| `internal/infrastructure/docparser/parser_proxy.go` | 已授权PDF转发薄客户端；环境配置origin、独立共享key、每一V1请求及取消携带tenant/KB/doc/version；复用原生readV1结果转换，不回退 |
| `extensions/parser/native.go` | 可选Host接收端`/parser/native/v1/`；Bearer/Header及显式tenant/KB校验、每doc独立scope、PDF预检与结果ZIP归档；原生核心身份是信任源 |
| `extensions/parser/artifacts.go` | 可选显式根目录文件归档；可信进程内BindDocument后保存原始middle.json及格式/schema/MinerU/归属/hash清单，原子发布目录、冲突拒绝；无产物SQL表/事务或崩溃恢复 |
| `extensions/host/config.go`、`runtime.go`、`projections.go` | probe/worker配置验证；固定回环、独立DB、对账/生命周期/复核扫描；Outbox显式分流标签/复核告警，未知类型拒绝；唯一标签Writer及任务join |
| `extensions/README.md`、`.env.example` | Host运行说明和环境变量示例；程序只读取进程环境，不自动加载.env文件 |
| `extensions/scripts/`、`.github/workflows/extensions.yml` | 独立模块边界检查及其回归测试、扩展专用CI；开发检查需要Python 3.11+，Python不参与Host运行 |
| `extensions/scripts/e2e-test.sh`、`e2e_test.py` | WSL/Linux真实核心与真实Host E2E；只读备份源DB，在隔离副本经原生API认证/建测试资源，使用本地模型夹具，核对SSE和关联日志 |
| `extensions/cmd/extension-host/main.go` | Extension Host 启动入口 |
| `AGENTS.md` | 可执行工作规则 |
| `TASK_BACKLOG.md` | 已知待办的期望结果、依据与重审条件 |

## 运行与构建方式

来源账本信任边界：`lineage.Authority`可通过固定Core origin/限定KB机器凭据实时核验原生当前快照，再在最终扩展view核对权威头；ObservationSequence仅为扩展已观察顺序，schema6旧native_sequence物理列保留。没有完整运行封存适配时不产生ready；模型自报complete无权签署。`CheckRead`不替代原生权限校验，先核对绑定再访问来源；幂等回执不是当前读取许可。当前没有Host副本路由、完整run来源封存或原生副本门卫，现有资源/文件/记忆的运行行为不会因新增库代码自动受控。

- Extension Host：在 `extensions/` 执行 `go run -mod=readonly ./cmd/extension-host`，仅监听 `127.0.0.1:8090`。默认probe不打开DB或调用上游；显式worker模式要求独立DB路径、API origin/key、tenant及非空KB列表，启动周期对账和限定范围的Outbox。完整变量及默认值见 `extensions/README.md`。
- `GET /bridge/healthz`、`GET /healthz`返回200，仅表示存活。probe检索业务503；worker在数据库可用时按scope策略返回允许/排除/deny_all，错误或DB故障拒绝；`GET /readyz`及其余三个业务Hook仍503。
- Host先校验配置并占用固定端口，再初始化持久组件；端口占用不启动DB或投影。Interrupt/SIGTERM取消后台工作，HTTP排空后等待任务和Writer退出，再关闭DB；ShutdownTimeout只限定HTTP排空，不强制放弃事务。
- 扩展独立检查：在 `extensions/` 执行 `go build -mod=readonly ./...`、`go test -mod=readonly ./... -count=1`、`go vet -mod=readonly ./...`。根 module 的 `./...` 不遍历这个嵌套 module。
- 上游标准后端：`Makefile` 的 `make build` 构建 `./cmd/server`；`make dev-start`、`make dev-app`、`make dev-frontend` 是开发脚本入口，需要 Bash/make 和对应基础设施。
- 上游 Lite：`.env.lite.example` 描述 `DB_DRIVER=sqlite`、`RETRIEVE_DRIVER=sqlite`、本地文件和内存流管理。`make build-lite` 会先构建前端、更新 `web/`，再以 CGO 和 `sqlite_fts5` 构建；`make run-lite` 加载本地 `.env.lite` 后运行。该环境文件与真实凭据由本地运行环境提供。
- 上游 Docker：根目录 `docker-compose.yml`，README 中的 `docker compose up -d` 是标准部署入口；上游默认后端端口为 8080，与 Extension Host 的 8090 分开。
- 前端：在 `frontend/` 使用 `npm ci`、`npm run dev`；验证脚本为 `npm test`、`npm run type-check`、`npm run build`。

## 四类 Hook 的基线源码映射

以下是对 `f17ee8a` 的只读扫描结果，描述可注入的现有调用点，未修改上游。行号仅适用于该基线；方法名前的接收者区分同名函数。

| Hook 类型 | 文件相对路径 | 函数名／方法名 | 大致行号 | 所在包名 |
|---|---|---|---|---|
| before_retrieve：普通 RAG | `internal/application/service/chat_pipeline/search_parallel.go` | `(*PluginSearchParallel).OnEvent` | 90–120；分发检索任务前 | `chatpipeline` |
| before_retrieve：Agent | `internal/agent/tools/search_knowledge.go` | `(*SearchKnowledgeTool).Execute` | 189–256；`concurrentSearchByTargets` 前 | `tools` |
| after_retrieve：Rerank | `internal/application/service/chat_pipeline/rerank.go` | `(*PluginRerank).OnEvent` | 122–180 | `chatpipeline` |
| after_retrieve：普通 RAG 最终 Top-K | `internal/application/service/chat_pipeline/filter_top_k.go` | `(*PluginFilterTopK).OnEvent` | 60–77；截断后 `next()` 前 | `chatpipeline` |
| after_retrieve：Agent | `internal/agent/tools/search_knowledge.go` | `(*SearchKnowledgeTool).Execute` | 308–347；结果集完成至返回前 | `tools` |
| before_generate：普通 RAG 上下文完成 | `internal/application/service/chat_pipeline/into_chat_message.go` | `(*PluginIntoChatMessage).OnEvent` | 185–220 | `chatpipeline` |
| before_generate：普通 RAG 流式调用 | `internal/application/service/chat_pipeline/chat_completion_stream.go` | `(*PluginChatCompletionStream).OnEvent` | 73–101；`ChatStream` 前 | `chatpipeline` |
| before_generate：Agent 每轮入口 | `internal/agent/engine.go` | `(*AgentEngine).runReActIteration` | 942–959 | `agent` |
| before_generate：Agent 实际流式调用 | `internal/agent/think.go` | `(*AgentEngine).streamLLMToEventBus` | 60–70；编码／`ChatStream` 边界 | `agent` |
| ingest_lifecycle_event：任务入口 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).ProcessDocument` | 3754–3892 | `service` |
| ingest_lifecycle_event：解析完成 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).convert` | 4418–4457 | `service` |
| ingest_lifecycle_event：分块完成 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).ProcessDocument` | 4243–4287 | `service` |
| ingest_lifecycle_event：分块持久化 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).processChunks` | 690–709 | `service` |
| ingest_lifecycle_event：索引完成／失败 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).processChunks` | 786–832 | `service` |
| ingest_lifecycle_event：异步最终完成 | `internal/application/repository/knowledge.go` | `(*knowledgeRepository).FinalizeSubtask` / `finalizeSubtask`；`(*knowledgeRepository).CompleteProcessingWithoutSubtasks` | 679–750；786–798 | `repository` |
| ingest_lifecycle_event：解析失败／重试 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).convert`；`(*knowledgeService).failKnowledge` | 4423–4443；4534–4554 | `service` |
| ingest_lifecycle_event：解析超时 | `internal/application/service/knowledge_process.go` | `(*knowledgeService).callDocReaderWithTimeout` | 4468–4493 | `service` |
| ingest_lifecycle_event：Lite 异常／重试耗尽 | `internal/router/sync_task.go` | `(*SyncTaskExecutor).Enqueue` | 172–212 | `router` |
| ingest_lifecycle_event：终态失败回调 | `internal/router/task.go` | `newDeadLetterKnowledgeFailer` | 387–450 | `router` |
| ingest_lifecycle_event：停滞恢复 | `internal/application/service/knowledge_housekeeping.go` | `(*HousekeepingService).recoverStalled` | 471–507 | `service` |

调用链上的重要事实：

- 普通 RAG 的编排见 `internal/application/service/session_knowledge_qa.go:221–233`：`CHUNK_SEARCH_PARALLEL → CHUNK_RERANK → CHUNK_MERGE → FILTER_TOP_K → INTO_CHAT_MESSAGE → CHAT_COMPLETION_STREAM`，另有按配置插入的节点和直接聊天分支。
- RAG 的 before_retrieve 位置在 `search_parallel.go` 意图跳过判断之后、克隆检索上下文及启动并发任务之前，才能同时约束 chunk/entity 两路。实际拒绝返回路径需在后续接入时核实，不能假设错误会自动阻止其他降级路径。
- Agent 检索先验证库作用域，再并发查询、Rerank、MMR、去重和 limit；`search_knowledge.go:332` 将最终结果格式化为 ToolResult，347 行返回给上层观察流程。after_retrieve 元数据增强适合在 332 行前处理，使输出与结构化数据一致。
- Agent 每轮经 `callLLMWithRetry`（`think.go:569`）清理消息、重试与流式调用；`streamLLMToEventBus` 是实际请求边界。业务指导注入需按会话／轮次去重，不能每次传输重试重复追加。
- 索引成功不等于整个摄取完成。`processChunks` 的 `finalizeIndexedKnowledgeState`（263–288、884–895）对没有文本／多模态后续工作的分支可直接完成；其余由 `KnowledgePostProcessService.Handle`（`knowledge_post_process.go:303–336`）进入无子任务完成或 `finalizing`，再由仓储原子计数晋升到 `completed`。
- `knowledge.go:313–358` 的阶段辅助函数及 `knowledge_span_tracker.go` 是 best-effort 进度追踪，不能替代不可丢失的 Bridge 事件投递。Lite 异常重试、最终失败回调和 housekeeping 的数据库更新也是单独的摄取信号来源。

## 当前骨架的能力边界

扩展模块包含事件DTO、协议端点、周期对账、标签单写者、文档排除计算、SQLite生命周期/Outbox，以及原生公共API适配和持久Observer/标签Handler组件。Host在worker模式初始化数据库并运行对账与投影，各KB共用唯一标签Writer，Outbox仅认领配置tenant/KB内的任务。RAG原生HTTP门卫与Host默认Pass-Through已接通并通过真实核心WSL E2E；对账/标签适配主要依据独立数据库和原生格式HTTP fixture，不能以本次E2E代替全部业务部署证明。复杂治理/策略响应消费、其他Hook及MCP仍属后续。

`PluginSearchParallel.OnEvent`保留无需检索skip，并发chunk/entity前调用Bridge；坏响应/断网打印固定失败日志并返回bridge_before_retrieve_failed。合法deny_all清空结果并返回ErrSearchNothing；否则合并结果后减排除集，并将普通文档限制在返回允许集内，WebSearch结果保留。本次普通RAG路径已消费策略；Agent等其他入口不据此宣称受控。

原生认证与RBAC分开：关闭tenant RBAC不会关闭Auth；API需要Bearer JWT或有效X-API-Key。`/api/v1/auth/auto-setup`要求Lite edition及桌面专用token，普通CLI无匿名管理员入口。前端静态资源只在Lite edition或显式WEKNORA_WEB_DIR时挂载；二进制文件名不决定edition。显式ENV_FILE可加载已有配置，程序不把任意.env自动当作Lite配置。

Host对版本合法的检索前请求仅记录固定`bridge_before_retrieve_received`，不记录请求内容或凭据。runtime通过NewServerWithServices接入NewPassThroughRetrieveHandler，probe与worker均启用；原生权限仍由核心执行。单独调用未配置Services的NewServer仍拒绝业务，避免把库构造器当作默认放行策略。

四个POST端点均要求协议版本1：缺失／不支持／多值版本返回400；未配置的业务仍503。Host仅默认配置before-retrieve，不提前确认摄取成功或宣称生成前/检索后增强已就绪。

库调用方仍可通过NewRetrieveHandler配置固定scope策略。该模式保留allowlist、集合收窄及200+deny_all语义，未来消费者须解释结果。M0 Host默认透传保留非空原请求候选，不新增排除；因原生门卫只检查状态码，空候选直接403。两者共用严格字段/版本/1MiB/10000文档/5秒读取校验；省略/null文档范围不接受，whole-KB候选枚举和复杂范围转换仍按C-09后续实施。

持久层使用独立 `agentrag_ext.db`；`store.Open`拒绝含非ext用户表的已有数据库。writer连接使用WAL、synchronous=NORMAL、busy_timeout=5000、foreign_keys=ON、cache_size=-64000、wal_autocheckpoint=1000；每个reader物理连接单独配置并以mode=ro/query_only运行。事务取消后需要时重新预留writer连接，业务回调不会自动重放。

扩展schema v2增加`ext_ingest_observations/current`、`ext_kb_tag_mapping`和`ext_tag_sync_failures`；v1升级保持既有表数据。重复对账快照只刷新last_seen，源时间较旧拒绝覆盖、同时间不同parse_status报告冲突；source更新时间保留纳秒UTC。观察与生命周期事实分开存储。

生命周期存储只校验身份、scope及from_state，不把解析状态转换为治理active。event/current/outbox在一次提交中落库；current的last_event_id同时受存在性及tenant/KB/document组合FK约束。Outbox按因果事实去重并至少投递一次，租约token约束本地确认；实际投影Handler仍需幂等并以当前truth收敛，数据库租约不能约束远端HTTP副作用的先后顺序。

原生知识列表的分页位于顶层；标签列表的分页和数组位于`data`内。API key通过`X-API-Key`，执行租户通过`X-Tenant-ID`；公共API仍执行原生权限。规范gov标签需要预先在各KB通过原生UI/API创建，扩展不自动创建。跨页数据不是原子快照；回读校验不能替代跨系统CAS。

**C-10（Baseline Limitation，用户已接受）：** `internal/application/service/knowledge.go:604–613`加载标签失败仅记录日志；真正无标签也可能返回`tags:null`，公共响应不能区分两者。扩展对null/缺失数组安全拒绝并记录重试，显式数组才允许merge。无标签首写仍受限；后续通过独立标签只读API解决，本批没有修改原生API。安全拒绝不是同步成功。

## 已确认的 M0 实施取舍

用户在2026-10-29确认：

- 接受Lite内存队列的弱点，M0不修改原生数据库；通过周期对账补当前状态，不宣称完整恢复所有历史阶段。
- 扩展发起的标签投影经单一队列串行；该队列不约束原生UI/自动标签写入，跨系统并发覆盖仍是已知限制。
- Agent先做检索排除过滤，直接读、旧handle、图谱等完整治理留待后续机制；不能宣称覆盖所有工具。
- 独立解析代理放到后续阶段，不混入M0。

2026-10-30后续决定覆盖实施顺序：M0先采用原生HTTP门卫与默认Pass-Through，暂不启用复杂治理排除；前述过滤目标保留后续，不声称active、Agent直接读或图谱门禁已实现。当前默认透传要求显式非空文档范围，不能把未知/空范围当作whole-KB许可。

基线没有公开的 `excluded_document_ids` 搜索参数。底层 `internal/types/retriever.go:58` 虽有 ExcludeKnowledgeIDs，但 `internal/types/search.go` 的 SearchParams 未贯通，SQLite `repository.go:877–908` 也未应用该列表。扩展计算必须由薄适配变成显式文档范围/结果排除；空范围不能变成整库。具体差异、验证和残余风险见 `docs/design/M0-Change-Map-2026-10-29.md`。

## M1解析接入依据

用户指定复用本机`D:/mineru-venv`，实测包版本MinerU4.0.1，含pypdf/pypdfium2；这是已核实的本机环境，不是仓库自动安装依赖，也不证明所有档位模型ready。原生自建MinerUReader使用Uploads/Jobs；经用户授权，PDF文件在设置`AGENTRAG_PARSER_PROXY_ADDR`时由convert入口的薄客户端转到Host，保留原生响应转换与分块/索引流程。原生ReadRequest结构不变，Header身份取自持久化Knowledge；无配置和非PDF保持原生路径。源码映射和版本风险见`docs/design/M1-Source-Mapping-2026-10-30.md`。

独立POC的`mineru_endpoint`为`http://127.0.0.1:端口/parser/scopes/{route}`，不能带末尾`/v1`。生产原生薄客户端使用不带路径的`AGENTRAG_PARSER_PROXY_ADDR`，内部追加`/parser/native/v1/...`，不要求上传时指定MinerU引擎。Host解析配置默认关闭，完整配置后挂载NativeHandler；冷/热源都验证实际字节，输出file不自动成为输入。共享key与后端key分开，原生可显式配置宿主IP，Host仍固定回环监听；地址可达性由运行拓扑提供。4.0.1的PDF auto路径可能需要OCR资源，Flash不等于零资源；文本层txt测试不能替代OCR模型就绪验证。构建、E2E及配置见extensions/README。

PDF预检使用无文件系统配置的pdfcpu，源上限50MiB、1000页、3秒取消期限，单内容解码4MiB、累计检查页面/Form内容32MiB。文本层是有实际文本显示指令/字节或可解码ActualText的结构证据，不证明Unicode质量。混合PDF固定flash/txt且需复核，扫描页不会自动补OCR；无文本且无法确认纯图的文件返回422。带结构树但尚未解析MCID归属的文本不确定页不视为纯图。此PDF规则来自用户M1-03指令，覆盖Frozen7.2的PDF布局升档，Frozen原文保留。

文件归档路径相对Host工作目录解析。NativeHandler从可信核心Header自动绑定原生doc_id，每tenant/KB/doc一个独立scope，同内容不同doc不会共用文档归档；裸ScopeProxy的单file单doc限制仍保留。Host限定配置tenant/KB，但不独立回查原生权限。生产Host使用FULL同步的独立扩展SQLite：probe+parser默认在产物根.parser-state.sqlite，worker+parser复用配置的扩展DB；纯probe未启用parser时不打开DB。schema v3包含ext_parser_documents/jobs/artifacts，原始middle JSON、版本和正式任务FK持久保存。最多128个同时驻留scope，空闲且已持久保存者可驱逐并恢复；全活跃/保存失败容量耗尽仍503。上传/权限/回执可恢复，正式解析以已验证字节hash及可信文档身份/有效策略/后端去重，先提交intent再POST；未知结果拒绝重发，非exactly-once承诺。4.0.1 ZIP正式成员为middle_json.json（docvortex.middle，schema_version=2.0），本地固定名middle.json保留原始字节，FS核验及SQL登记都成功才返回ZIP；两资源使用恢复协议，不保证物理断电原子性。

自动原生上传测试位于extensions/scripts/m1_native_e2e.py、m1_native_e2e_linux.py和m1-native-e2e.sh：真实WSL CGO核心、生产Host与已安装Windows MinerU4.0.1，使用独立DB及仅测试使用的回环互操作中继。默认用embedding夹具；显式--live-models可用8081向量/8082重排与DeepSeek生成（凭据取进程环境）。验证PDF上传、flash/txt单job、身份归档和原生分块，真实模式另以原生检索API显式重排及问答API检查引用/答案。两API分别记录，不证明默认问答自动使用重排。--joint-m0使用确定性本地模型，另验证同Host启用parser后的SQL原始字节/FK、生产Host重启恢复同job、M0关联拒绝及通网生成；与--live-models互斥。上述都不代替生产语料/OCR/复杂预检、真实每库标定或物理断电验收。

归档的非Windows目录同步顺序是：两个文件Sync→暂存目录Sync→Rename→产物根目录Sync→SQL登记；已有归档核对字节后重新同步文档目录和根，修复上次发布屏障。目录同步错误会阻断SQL登记和ZIP成功。Windows保留既有文件Sync/rename，不提供同等目录刷新保证。本操作不覆盖产物根及其祖先目录的创建持久性，也不保证设备实际断电语义；遗留.pending目录不自动清理。

### 2026-10-31当前策略接管补充

Host worker现运行LifecycleObserver、每日生命周期扫描和生命周期DB检索策略；M0真实RAG Pass-Through是历史证据，不能用于说明当前生产默认行为。当前probe业务503；worker仅允许已知active候选。完整上传/LLM问答E2E在本批没有重跑，跨进程M2策略脚本使用真实Host/SQLite/原生管线与底层来源/检索夹具。原生其他入口治理与复核告警尚未接线。
