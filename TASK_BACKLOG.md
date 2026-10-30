# AgentRAG Task Backlog

本文件只管理任务生命周期。强制规则见 `AGENTS.md`；稳定事实与基线源码映射见 `PROJECT_CONTEXT.md`。2026-10-29 用户已授权按方案开展项目；遇到扩大核心补丁或改变实现边界的冲突，先报告和决策，不擅自硬改原生代码。

架构依据统一指向 `docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`。已有明确的冻结里程碑，因此以下不是空占位 backlog，也不重新估算架构文档中的周期。

## 当前里程碑

**M0／已完成（2026-10-30，按用户确认的验收范围）。** 默认Pass-Through校验协议/格式及显式非空文档范围，原样保留原生授权候选；Host不可达时原生HTTP门卫fail-closed，Host运行时真实RAG完成。真实WSL E2E退出0：offline=True、online=True、M0=PASS；通网SSE含answer/references/complete且答案生成1次。完整日志：`docs/evidence/m0-e2e-2026-10-30-pass-through.log`。上一轮503失败日志原样保留。

用户明确接受既有Linux race断言失败，作为已知待办保留，不阻塞单机场景的本次M0验收。**这是用户批准的阶段验收，不声称Frozen原版所有治理/Hook/MCP/负载门禁均已实现。** 下表原M0编号中尚未交付或验证的项目保留真实状态，转后续增强；不得把它们伪记为已完成。

**下一里程碑：M1／待开始。** 独立解析代理按既有范围另行开展，不重构原生解析器。

## 待办

状态可使用“待开始／进行中／阻塞／已完成”。每个条目保留期望结果、依据、何时重新审视；开始实施时再补充负责人、验收证据和完成日期。

| ID／状态 | 期望结果 | 依据 | 何时重新审视 |
|---|---|---|---|
| M0-01／进行中 | Change Map和冲突报告已形成；用户已确认C-01至C-04的M0取舍。核心物理补丁清单在实际接入前再核实，不能按协议类别数代替调用点数。 | 架构3.3/3.8/16/17.3；Change Map的2.1决定及C-09排除字段差异。 | 首次核心接入前；上游变更后。 |
| M0-02／已完成（本次HTTP门卫+默认透传范围） | 原生500ms门卫与Host默认Pass-Through接通，断网阻断、通网完整RAG E2E均通过。保留原生检索/过滤；复杂策略、响应收窄及其他Hook转后续，不声明已实施。 | 用户2026-10-30明确采用M0默认放行并要求E2E通过后完成M0；C-09作为后续项保留。 | 开始真实治理过滤、其他Hook、Agent接入及策略变化时。 |
| M0-03／进行中 | 对账调度、SQLite事实/Outbox、HTTP Source和持久Observer已接入显式worker Host；临时DB+本地原生格式HTTP验证周期恢复与范围隔离。真实服务及业务接收端仍待验。M0接受来源内存队列并以状态对账兜底。 | 不变量A/E、架构4/16、用户C-01决定。 | 真实服务/业务接收端接线和锁竞争出现时。 |
| M0-04／进行中，C-10接受为Baseline Limitation | 标签队列/API Store/KB映射/Outbox Handler已接入Host，多KB共用唯一Writer已测；null标签安全拒绝获用户接受，无标签首写留后续只读API，真实服务仍待验。只声明扩展内部串行，不保证原生UI并发安全。 | 架构6、用户C-02及2026-10-30 C-10决定；本批不改原生API。 | 首次真实投影、标签只读API接入及并发来源变化后。 |
| M0-05／待开始 | 使用原生 MCP 按需发现注册本机 Extension MCP Server，采用 Streamable HTTP `/mcp` 与本机认证；不改 ToolRegistry。 | 架构 3.5、16；`query_related_docs`／`discover_peers` 目标能力。 | 首次开发 MCP 工具前；上游注册／发现实现变化时。 |
| M0-06／进行中 | 独立门禁、专用CI定义、SQLite/FK/事务/Outbox及Host本地联合测试已通过；真实QA全链路、真实来源对账和上游混合并发待验。并发2有上游竞态则记录后降至1重跑。 | 架构4.4/15–16；M0范围按用户四项决定限定；远端Actions尚未运行。 | 首个实现PR前；module/workflow/DB变化后。 |
| M1／待开始 | PDF/Image 通过 Cheap Preflight 选择 MinerU 档位且只正式 parse 一次；Office 走 native Flash/builtin；保存带版本的 parser artifacts；按库完成阈值标定。 | 架构 7、15；路由准确率 ≥ 85%、高估率 ≤ 10%、低估率 ≤ 15%。 | M0 通过并开始解析扩展前；MinerU 或源文档分布变化时。 |
| M1.5／待开始 | 完成库级检索基线、RRF 参数实验、重排降级与查询扩展门控；量化只通过 feature flag 验证，保留原生 RRF 实现。 | 架构 14–15；Recall@5 ≥ 0.85、MRR ≥ 0.80（或基线 95%）；量化 Recall@10 退化 ≤ 2%。 | 开始检索优化前；模型、语料或 sqlite-vec 基线变化时。 |
| M2／待开始 | 实现含 scheduled 的生命周期状态机、库级规则与 stale 区分，event/current/outbox 同事务；通过崩溃注入与事实重建验证。 | 架构不变量 A、4、15。 | M0 持久化门禁通过且开始生命周期业务前；状态迁移规则变化时。 |
| M3／待开始 | 建立版本族、版本节点和分级 lineage DAG；provenance 与 version-ancestry 分离；Critical Edge 同事务，递归具备 cycle guard。 | 架构不变量 D、5、8、15。 | 开始血缘实现前；新增边类型或修改版本事实模型时。 |
| M4／待开始 | 实现规范性关联和受治理的语义关联，按库校准权重；通过原生 MCP 暴露 query_related_docs；完成 Agentic 评估及 GraphRAG 预算门禁。 | 架构 9–10、14–15；关联精确率 ≥ 0.70、召回率 ≥ 0.60；GraphRAG 默认关闭。 | M3 事实模型稳定并开始关联实现前；用户申请跨库关联或开启 GraphRAG 时。 |
| M5／待开始 | 完成反馈 → guidance → revision proposal → 幂等发布 → supersedes 闭环；治理与 audit 原子提交；重试前先做远端 reconciliation；落实审批、冷静期、指导预算。 | 架构不变量 B/C/D、10、12、15。 | 开始反馈治理或任何 ingest 发布副作用前；远端身份能力、审批策略变化时。 |
| M6／待开始 | 实现 opaque mDNS、认证能力查询、静态节点回退与架构列出的治理／运维／评估 UI；广播不含知识库元数据。 | 架构 11、13、15。 | 开始局域网发现或扩展 UI 前；暴露的网络范围和权限模型变化时。 |

## 细分实施任务：文件、验证与依赖

验收编号和操作步骤见 [M0 Change Map](docs/design/M0-Change-Map-2026-10-29.md)。新增文件均为计划落位；标记“受控候选”的现有核心文件尚未修改。父任务按用户明确确认的验收范围判断；本次范围之外的原冻结条目保留真实状态作为后续任务，不得冒充已完成。

| ID／状态 | 期望结果／涉及文件（仓库相对路径） | 验证方法 | 依据／冲突／何时重新审视 |
|---|---|---|---|
| M0-01A／已完成（审计交付） | 源码审计与冲突报告：`docs/design/M0-Change-Map-2026-10-29.md`；完整路线见本表。 | 逐条对照真实函数和行号；原生diff为空；不是M0验收通过。 | 架构16/17.3；核心接入前、基线改变及用户决策后重审。 |
| M0-02A／已完成（协议基础） | 协议版本、存活／就绪、无服务拒绝：`extensions/bridge/protocol.go`、`server.go`、`server_test.go`。 | FC-0；37个HTTP子用例通过；不是原生全链路证明。 | 架构3.2/3.6/3.7；业务handler接入时重审。 |
| M0-02B／进行中（扩展检索前契约完成） | `extensions/contract/retrieval.go`及测试、`contracts/bridge/v1/`样例、`extensions/bridge/retrieve.go`及测试和可配置server已实现；原生候选`extensionbridge/contract.go`未创建，其他Hook契约待做。 | FC-2/CT-1：严格JSON/scope、显式空deny_all、子集/关联、坏策略/取消503、停滞请求体拒绝；仅扩展消费样例，两module接线待验。 | 用户C-03决定、Frozen3.4与C-09；原生消费者及持久策略接线前重审。 |
| M0-02C／已完成（门卫+Pass-Through E2E） | `extensions/bridge/retrieve.go`新增默认Pass-Through构造器，`extensions/host/runtime.go`接线；更新bridge/host测试与脚本验收范围说明。原生核心保持原补丁，不改检索/过滤。 | WSL真实核心/Host同凭据同请求：断网关联日志+error+答案生成0；通网Host receipt+answer/references/complete+答案生成1；脚本退出0，M0=PASS。格式错误/版本错误仍拒绝，原固定tenant/KB handler继续保留。 | 用户批准M0默认放行；2026-10-30完成。真实治理策略/范围收窄另行实施，不能将透传称为active过滤。 |
| M0-02D／待开始，M0仅搜索排除 | 受控候选 `internal/agent/tools/search_knowledge.go` 和公共薄适配；直接读/图谱/完整治理移至POST-M0-GOV，不在M0硬改。 | FC-1/FC-2：搜索排除和Host不可用拒绝；FC-3明确后移。 | 用户C-03务实范围；搜索接入前复核C-09。 |
| M0-02E／待开始，依赖02B | 检索后元数据增强：`extensions/retrieval/contextualize.go`；受控候选 `internal/application/service/chat_pipeline/filter_top_k.go`、`internal/agent/tools/search_knowledge.go`。 | FO-1：Host失败保留原结果；增强在ToolResult格式化前生效。 | 架构3.2/3.3/10.3；契约和源调用点稳定后。 |
| M0-02F／待开始，依赖02B | 生成前指导与预算：`extensions/guidance/inject.go`、`budget.go`、`budget_test.go`；受控候选 `internal/application/service/chat_pipeline/chat_completion_stream.go`、`internal/agent/think.go`，fallback另核实。 | FO-1/GD-1：重试去重、每轮3条、同ID每会话2次、总800token、失败保留原消息。 | 架构10.2；首次注入前和模型重试机制变化后。 |
| M0-03A／进行中（Host已接线） | `extensions/ingest/reconcile.go`、`http_source.go`、`observer.go`及测试，`extensions/weknora/client.go`、schema002；Host接线见03E。 | IN-1本地：原生格式分页/scope校验、幂等/陈旧/冲突、SQL故障回滚与持久重开；Host来源失败后周期收敛已测，lifecycle保持独立。实际来源崩溃后收敛待验。 | 用户C-01；2026-10-30本地Host交付，真实服务接线前重审；不承诺历史完整性。 |
| M0-03B／已完成（持久化组件） | SQLite连接与单写者：`extensions/store/sqlite.go`、`write_executor.go`、`migrate.go`、`sqlite_test.go`、`write_executor_test.go`；独立`go.mod/go.sum`锁定纯Go驱动。 | DB-1通过：3个同时物理reader配置及写拒绝、BEGIN IMMEDIATE实际写锁、WAL并行读；1000并发真实INSERT精确计数；错误/panic/执行中取消/排队取消及关闭已测。feedback业务及上游混合并发属于06B。 | 架构4.1–4.4；完成2026-10-30。Host接线、平台/驱动变化及锁竞争时重审。 |
| M0-03C／已完成（事实存储组件） | event/current/outbox原子提交及存在性/scope组合FK：`extensions/store/migrations/001_lifecycle.sql`、`extensions/lifecycle/store.go`、`store_test.go`。 | TX-1组件通过：event/current/outbox三处真实SQL触发器故障全回滚、事件内存重建current、ID/来源键冲突、stale from_state、缺失/跨文档FK拒绝；提交前进程退出无部分数据。 | 不变量A/E；完成2026-10-30。M2正式状态政策及接收端ACK接线前重审。 |
| M0-03D／已完成（队列组件） | 因果Outbox/Worker：`extensions/outbox/store.go`、`worker.go`及测试；`extensions/integration/outbox_recovery_test.go`。 | OB-1组件通过：提交/领取后进程直接退出可恢复、100并发认领只有1活租约、过期回收/旧token拒绝、重试/dead/分scope人工retry/统计；健康小样本3秒预算内排空。真实标签副作用与负载SLO待04B/06B。 | 架构4.6–4.9；完成2026-10-30。首个实际投影接入、投影幂等/顺序设计变化时重审。 |
| M0-04A／已完成（队列组件） | `extensions/taggov/writer.go`、`writer_test.go`：扩展发起的read/merge/replace/verify单队列，原生ID写入，只改目标命名空间。 | TG-1：1000并发扩展更新、最大API并发1、保留业务/其他gov、错误上报、关闭后拒绝、命名空间越界拒绝。 | 用户C-02决定；真实API接入前重审，原生UI竞态仍存在。 |
| M0-03E／已完成（Host本地接线） | `extensions/host/config.go`、`runtime.go`及测试，`cmd/extension-host/main.go`及测试；Outbox Store/Worker scoped claim及测试；`.env.example`、`README.md`、`.gitignore`。 | HO-1：真实回环探针、双KB唯一Writer、周期失败后恢复；外部tenant/KB的pending/过期processing不变；坏配置/DB拒绝/端口占用无后台副作用，取消及Serve错误释放DB/listener。实际8090命令探针已验。 | 架构3/4/6、用户C-01/C-02；2026-10-30完成本地接线。真实服务/业务handler、组件及配置变更时重审。 |
| M0-04B／进行中（Host已接线，接受C-10限制） | `extensions/taggov/mapping.go`、`http_store.go`、`projector.go`及测试；`writer.go`队列内ApplyResolved；`extensions/integration/public_api_test.go`及Host测试串起持久Outbox与HTTP适配。 | TG-1本地：跨KB映射、merge保留标签、旧payload读最新truth、失败持久记录及重试成功、Host唯一Writer已测。真实API待验；null拒绝作为预期结果，无标签首写不冒充成功。 | 用户C-02、架构6及C-10 Baseline Limitation决定；后续只读API另列POST-M0-TAGS。首次真实投影前重审。 |
| M0-05／待开始 | 本机MCP与认证：`extensions/mcp/server.go`、`auth.go`、`server_test.go`；使用原生MCP设置注册。 | MC-1：按需schema、token、stdio拒绝、回环allowlist及容器拓扑。 | 架构3.5；C-08配置核实后。 |
| M0-06A／已完成（本地门禁与CI定义） | `extensions/scripts/check_module_boundary.py`、`check_module_boundary_test.py`、`.github/workflows/extensions.yml`。 | CI-1：7个真实临时module案例和实际module通过；workflow本地解析通过；远端Actions/race未运行。 | 架构15；module/import/workflow变化时重审。 |
| M0-06B／待开始，依赖上述M0接线 | `extensions/integration/bridge_test.go`、`ingest_recovery_test.go`、`concurrency_test.go`。 | 2文档真实ingest+1000feedback+lifecycle+投影，终态后核对；并发失败按流程降至1重跑。 | 架构4.4/15；可用Lite环境/持久层/适配就绪后；验收范围按用户M0决定限定。 |
| M0-06C／待处理（用户接受，M0非阻塞） | 既有Linux race命令中的Outbox报告次数断言失败保留：`extensions/outbox/worker_test.go:102`、候选`worker.go`；未修复，不将它描述为已通过。 | 已有证据：完整race及单独该测试均退出1，calls=3 reports=2；没有DATA RACE报告。后续记录错误种类并区分正常取消/投影故障，不放宽断言。 | 用户2026-10-30明确接受现状，不阻塞本次单机M0验收。进入后续修复、扩大部署或更改Worker语义时重审。 |
| M1-A／后续阶段，M0不实施 | 独立解析代理/预检：`extensions/parser/preflight.go`、`router.go`、`mineru_proxy.go`、`router_test.go`。 | PA-1：预检不parse、正式job=1、Office不进四档、按库标定。 | 用户C-04决定；M0完成后独立POC。 |
| M1-B／待开始，依赖M1-A | 版本化原始产物：`extensions/parser/artifacts.go`、计划`extensions/store/migrations/003_parser_artifacts.sql`（002已用于M0，实施时核实下一版本）。 | PA-1：真实服务产物、schema/MinerU版本、无二次解析，失败与重试可追踪。 | 架构7.4；真实MinerU服务及产物契约验证后。 |
| M1.5／待开始 | 评估/调参/量化实验：`extensions/eval/retrieval.go`、`rrf_grid.go`、`quantization.go`、`testdata/retrieval/`；原生租户retrieval-config API。 | RE-1：每库30–50查询，Recall/MRR基线、flag隔离、Recall@10退化≤2%。 | 架构14/15；M0通过，模型/语料/sqlite-vec变化时重审。 |
| M2／待开始 | 生命周期业务、规则和stale：`extensions/lifecycle/machine.go`、`rules.go`、`stale.go`、`machine_test.go`；按库配置。 | LC-1/TX-1/OB-1；正式状态矩阵含scheduled，时钟、非法迁移、重放、两种stale分测。 | 架构M2；M0持久化门禁通过及状态设计明确后。 |
| M3／待开始 | 血缘/版本族/查询：`extensions/lineage/store.go`、`edges.go`、`provenance.go`、`ancestry.go`、`query_test.go`，相应ext迁移。 | LN-1/TX-1：混合边白名单、反向溯源、环、tenant/KB和Critical Edge原子性。 | 架构5/8；C-07按API语义澄清，新增边类型时重审。 |
| M4／待开始 | 关联与Agentic评估：`extensions/relations/service.go`、`calibrate.go`、`graph_budget.go`、`extensions/mcp/related_docs.go`、`extensions/eval/agentic.go`。 | AS-1：precision/recall、轮次、跨库确认、GraphRAG批准/预算/回退。 | 架构9/10/14；M3稳定后，跨库或GraphRAG申请时重审。 |
| M5-A／待开始 | 反馈指导治理：`extensions/feedback/events.go`、`governance.go`、`cooldown.go`、`governance_test.go`；扩展预算组件。 | GD-1/RV-1/TX-1：冷静期/二次确认/抽检/熔断/audit同事务/质量回归。 | 架构10/12；M0–M4基础稳定、审批策略明确后。 |
| M5-B／待开始（C-06 POC） | 发布Saga与对账：`extensions/revision/saga.go`、`remote_identity.go`、`reconcile.go`、`publish_recovery_test.go`。 | RV-1：提交后响应丢失先对账，create保持1；未知状态不重放；binary源需用户正式上传。 | 不变量B/C/D、架构12；远端身份POC通过后。 |
| M6-A／待开始 | mDNS/认证能力：`extensions/discovery/mdns.go`、`capabilities.go`、`static.go`、`discovery_test.go`。 | DS-1：广播字段抓包、无KB元数据、认证拒绝、静态回退。 | 架构11；公开网络范围与认证模型确定后。 |
| M6-B／待开始 | 独立UI与最薄bootstrap：计划 `extensions/ui/src/routes.ts`、`views/`、`api/`；受控候选 `frontend/src/router/index.ts`。 | DS-1；前端测试/type-check/build；库切换权限，溯源/版本历史分离，运维真实状态。 | 架构13；独立构建/发布路由方案核实后，不提前重构前端。 |
| POST-M0-GOV／后续机制 | Agent直接读/旧handle/图谱/中途停用的完整治理；候选 `internal/agent/tools/read_document.go`、`query_knowledge_graph.go` 或共用执行门禁。 | FC-3；M0未验证，不得宣称所有工具已受控。 | 用户C-03决定；M0后结合真实绕过案例和补丁预算重审。 |
| POST-M0-TAGS／后续，未实施 | 独立标签只读API，候选`internal/handler/knowledge.go`、`internal/router/routes_knowledge.go`；扩展`taggov/http_store.go`适配。 | 无标签显式[]、查询故障非2xx、跨tenant/KB拒绝；保留业务标签，重试后收敛；核心补丁范围须单独核实。 | 用户2026-10-30接受C-10安全拒绝并选择后续只读API；启动该任务并明确核心薄补丁授权时重审。 |

## 执行记录：Bridge基础（2026-10-29）

- 实施计划：`docs/superpowers/plans/2026-10-29-m0-bridge-foundation.md`。
- 决定：继续在现有工作目录保留上一轮未跟踪骨架；不移动、提交或覆盖用户原件。受控核心仍只读。
- 用户决定：C-01周期对账、不改原生数据库；C-02扩展标签单队列；C-03先排除搜索、完整工具治理后移；C-04解析代理后移。详细边界见Change Map 2.1。
- RED→GREEN：HTTP新增端点/版本校验先失败后通过；模块CLI缺失时7个fixture先失败，补实现后全部通过；对账/投影/排除组件先因类型尚未实现失败，补实现后Go suite通过。
- 本地证据：`go test -mod=readonly ./... -count=1`、`go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、`python scripts/check_module_boundary.py`、`python -m unittest discover -s scripts -p '*_test.py' -v`均退出0；`gofmt -l .`无输出。bridge/ingest/retrieval/taggov有通过的测试，cmd/extension-host与lifecycle仍无测试文件。Python7例通过；Go真实HTTP37子用例通过；标签1000更新是接口组件测试。
- 不声明：端到端Fail-Closed、真实上游API接线、SQLite混合并发、Outbox崩溃恢复、远端Actions/race。1000标签排队测试不是1000条真实数据库feedback写入。
- 最终只读复核：无Critical/Important代码问题，独立重跑Go suite退出0；发现一处文档仍把直接读/图谱列入M0，已按用户决定收窄至搜索排除并转POST-M0-GOV，未修改代码。
- 复核范围裁定：实际API/持久层/Outbox/SQLite混合并发保留后续任务，代价是本批不具备部署就绪或完整M0证明；原生端到端尚待接线，直接读/图谱明确后移，代价是残余绕过风险；原生UI标签并发按用户接受的M0限制保留，代价是跨系统覆盖风险；Go1.26实际运行/远端CI/Linux race未验证，代价是对应平台证据尚缺。上述均不伪装成已通过项目。

## 执行记录：持久化基础（2026-10-30）

- 实施计划：`docs/superpowers/plans/2026-10-29-m0-persistence.md`；本次交付03B/03C/03D组件，M0父任务仍进行中。未改`internal/`、根依赖、原始冻结方案或BASELINE。
- RED→GREEN：store/lifecycle/outbox测试分别先因接口缺失失败；补实现后真实数据库测试通过。额外的执行中取消测试复现自动回滚先完成时的ErrTxDone，已正确返回取消并恢复被释放的reserved connection，绝不自动重放业务回调。
- 数据库环境：Windows amd64、Go1.27.0、CGO_ENABLED=0；`modernc.org/sqlite v1.61.0`仅加入扩展模块。独立临时SQLite验证1000条并发feedback探针INSERT；没有实现或冒充M5的feedback业务模型，也没有访问真实用户数据。
- 故障证据：真实SQL触发器在event/current/outbox三处中止，三表全回滚；临时子进程分别在提交前、提交后、claim后直接`os.Exit(23)`，父进程重开同一文件验证回滚/恢复及token fencing。提交前测试直接在事务中构造三表数据；提交/领取后测试通过实际lifecycle.Append/Outbox.Claim。
- 独立只读复核：发现1项Important——Handler自身deadline使lease过期后Fail无法累计失败，导致无限重领。新增`TestHandlerDeadlineCountsFailureAndReachesDead`先失败，再修复为processing+token匹配即可记录尚未被回收的失败；Complete仍拒绝过期lease，旧token的Complete/Fail仍拒绝新lease。完整suite再次退出0；无其他实质发现。
- 本地验证：`go test -mod=readonly ./... -count=1`、`go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、实际Python模块门禁和7例回归均退出0，`gofmt -l .`无输出。cmd/extension-host仍无测试文件；lifecycle现在已有真实存储测试。原件及根go.mod/go.sum的SHA256与开工前一致。
- Final: Ruling: Host/Observer/实际标签API/原生Hook尚未接线 — 作为03A/04B/02后续任务保留，本次只交付持久组件 — 代价是readyz仍503，不能声明完整M0或原生Fail-Closed通过。
- Final: Ruling: 真实上游2文档混合ingest、生产负载oldest_pending_age、远端CI、Go1.26运行及Linux race未在本批验证 — 本地纯Go临时数据库证据不能覆盖这些环境/负载 — 代价是对应验收门禁仍未通过，不解除06B依赖。
- 投影接线约束：至少一次投递可能重复或乱序；Handler必须幂等并读取当前truth收敛，不能盲目重放旧payload。租约只约束本地ACK，不解决远端副作用重叠，也不解决用户已接受的原生UI标签覆盖风险；04B接线时验证。
- 下一依赖：03A公共API Source与持久Observer → 04B真实标签Store/KB映射/Outbox Handler → Host启动与业务DTO/handler；首次核心接入前仍需逐项核实C-05/C-09和物理薄补丁边界。

## 执行记录：公共API与持久适配（2026-10-30）

- 计划：`docs/superpowers/plans/2026-10-30-m0-public-api.md`；完成03A/04B的API/持久组件，Host/真实服务/C-10仍未解除，父任务继续进行。
- 本批新增`extensions/weknora/client.go`及测试；`ingest/http_source.go`、`observer.go`及测试；`store/migrations/002_ingest_taggov.sql`和迁移测试；`taggov/http_store.go`、`mapping.go`、`projector.go`及测试；`integration/public_api_test.go`。修改仅扩展迁移/Writer及项目文档，没有引入新依赖。
- RED→GREEN：client/Source、Observer、Mapping/HTTPStore/Projector分别先因接口缺失失败；v1升级测试先发现缺少schema2后通过。源更新时间保留纳秒；重复只更新last_seen，陈旧/同时间异状态拒绝，事务两处SQL故障均回滚。
- 联合证据：真实临时SQLite+原生格式HTTP fixture；对账后重开DB仍可读origin=reconcile，lifecycle无active；显式测试审批事件后，通过HTTP失败/回读不一致/null拒绝三次失败再成功，失败记录count3、业务/其他gov标签保留、队列排空。没有访问运行中的真实WeKnora或用户数据。
- C-10重要冲突已报告：原生标签加载失败仍200且tags:null，与无标签歧义。当前拒绝全量覆盖并记录重试；无标签首写未解决。最薄只读标签API或反向枚举POC仅是建议，未获核心修改授权，未实施。详见Change Map 2.2。
- 独立复核：Important为上游WeKnora忽略规则在Windows隐藏了client包；补`extensions/.gitignore`精确例外，实际`git ls-files --others --exclude-standard -- extensions/weknora/`确认两个文件可交付。裸? origin违反本计划固定origin输入约束，已补构造器回归RED→GREEN；超限fixture改为有效大JSON并精确断言size-limit，避免无效JSON假阳性。
- 本地验证：完整`go test -mod=readonly ./... -count=1`、build/vet、模块边界和7例Python回归退出0；gofmt无输出；cmd/extension-host仍无测试文件。原始Frozen/BASELINE及根go.mod/go.sum hash与开工前一致，tracked源码diff为空，未提交或推送。
- Final: Ruling: C-10 null标签先拒绝而不改原生 — 未知读结果不能安全merge，核心保持只读 — 代价是无标签首写受限，04B不能宣称完整验收。
- Final: Ruling: 规范gov标签不自动创建，需原生UI/API预配置 — 现有任务只要求解析原生ID与映射，不擅加创建副作用 — 代价是缺失规范标签会重试/dead，配置完成后需人工retry。
- Final: Ruling: Host/真实WeKnora/业务DTO/原生Hook/MCP与混合ingest、生产SLO、Go1.26/远端CI/Linux race保留后续门禁 — 本批只证明组件与fixture联合行为 — 代价是尚未部署就绪、端到端Fail-Closed未验证，C-02外部UI/远端未知结果竞态仍存在。
- 下一依赖：Host配置与组件生命周期接线、业务DTO/handler；C-10后续核心接口须另行明确授权，C-05/C-09接入预算仍需逐项核实。

## 执行记录：Host配置与生命周期接线（2026-10-30）

- 计划：`docs/superpowers/plans/2026-10-30-m0-host-runtime.md`；完成03E本地接线，03A/04B及M0父任务继续进行。仅修改扩展和项目文档，没有新增依赖、迁移或原生补丁。
- 配置/启动：默认probe无持久/上游副作用；显式worker完整校验独立DB、origin/key、tenant/KB与重试/周期配置。配置格式化及错误不泄露secret；生产入口先占用127.0.0.1:8090再打开DB，端口冲突不能启动第二个Writer。
- 隔离/恢复：事务内scoped claim同时限定pending及过期processing；Host始终提供显式非空allowlist。双KB共享唯一Writer，初始来源503后周期恢复并持久保存观察/完成投影；外部tenant/KB的三项pending/过期processing保持原状。测试使用临时SQLite和本地原生格式HTTP，没有访问真实WeKnora或用户数据。
- RED→GREEN：配置、scoped claim、Host/command测试先因接口缺失失败后通过；显式空Worker.Scopes必须在取消前拒绝的测试先失败，再补前置验证通过。坏配置/外部DB/预取消/端口占用/非回环地址/Serve退出及取消清理均有回归。
- 本地验证：完整`go test -mod=readonly ./... -count=1`（11个包，host与cmd现在均有测试）、`go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、Python实际模块门禁及7例回归退出0；gofmt无输出。原件及根依赖hash一致，原生tracked源码diff为空。
- 命令实测：`go run -mod=readonly ./cmd/extension-host`以probe启动8090；独立HTTP检查脚本退出0，healthz=200、readyz=503、版本1 before-retrieve=503。Ctrl+C后打印host_stopped并结束；go run终端包装进程因中断返回1，不把它记为成功退出0。
- 一次独立只读复核无Critical/Important/Minor发现，并重跑11包测试退出0；确认配置/监听前置、scope隔离、唯一Writer、关闭顺序和日志脱敏。最终文档与命令探针由实施者收尾。
- Final: Ruling: ShutdownTimeout只限制HTTP排空，后台任务取消后等待自身清理完成再关闭Writer/DB — 避免强制放弃持久事务 — 代价是总体关闭时间可能超过HTTP期限；Reporter调用方须快速返回。
- Final: Ruling: readyz及四类业务Hook继续503，观察不生成治理active — 业务DTO/handler、核心Hook与MCP尚未接通 — 代价是仍不能声明部署就绪或原生端到端Fail-Closed通过。
- Final: Ruling: C-10、原生UI/远端未知结果竞态、真实WeKnora/混合ingest/生产SLO、Go1.26运行及远端CI/Linux race保留后续门禁 — 本地fixture证据不替代真实环境验收 — 代价是03A/04B/06B与M0父任务仍未完成。
- 下一依赖：公开业务DTO/handler与契约fixture；首次核心接入仍需明确授权和C-05/C-09逐点补丁核实，C-10接口建议未实施。

## 执行记录：C-10决定与检索前公开契约（2026-10-30）

- 用户决定：接受C-10安全拒绝，明确归为Baseline Limitation；后续独立标签只读API解决。M0不为强求同步而猜测null为空数组、误删业务标签或更改原生接口；无标签首写仍是功能限制，记录POST-M0-TAGS。
- 计划：`docs/superpowers/plans/2026-10-30-m0-retrieval-contract.md`；完成02B扩展端检索前契约及typed handler，不宣称02B全部或M0完成。新增`extensions/contract/retrieval.go`及测试、`extensions/bridge/retrieve.go`及测试、`contracts/bridge/v1/`三份JSON样例和说明；修改bridge构造器/读取期限与项目文档。没有修改原生源码、依赖或schema。
- RED→GREEN：contract先报no non-test Go files；typed handler先报NewRetrieveHandler/NewServerWithServices/Services缺失，补实现后通过。JSON往返测试按语义比较对象键序且独立验证大整数，不把map编码顺序当契约。停滞body真实TCP测试先在7秒客户端deadline失败，加5秒请求读取期限后400且策略未调用，随后正常请求成功。
- CT-1证据：原生授权范围由未来调用方提供，本端校验tenant/KB allowlist及显式文档集合；缺失/null文档400、显式空数组deny_all且不调用策略。共享样例保留agent/多KB/tag scopes/metadata；排除结果只收窄、保留关联/输入顺序，策略错误/候选外ID/重复ID/取消503；策略修改输入切片不能扩大响应。JSON重复/未知/大小写别名/拼接/类型错误拒绝；超1MiB为413；未配置及其他Hook仍503。
- 本地命令：`go test -mod=readonly ./... -count=1`十二包、`go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、`python scripts/check_module_boundary.py`（244 imports）与`python -m unittest discover -s scripts -p '*_test.py' -v`（7例）均退出0；`gofmt -l .`无输出。`go env GOMOD GOWORK`归属extensions且workspace空；原件/根依赖SHA256与开工前一致。
- Task 2: Ruling: 5秒完整请求读取期限用于终止停滞body — 只有header期限不能覆盖新增业务body读取 — 代价是极慢本机请求需重试，不改变业务fail-closed策略。
- 独立只读复核：无Critical/Important/Minor实质问题；独立重跑contract/bridge测试、模块归属与Git范围核对退出0，不需要代码修复。完整扩展十二包/build/vet/边界及Python回归由主任务本轮执行，复核者没有重复全部命令。未提交、推送或操作真实用户数据。
- Final: Ruling: 原生消费者、客户端超时/响应校验及chunk/entity/fallback零调用证明后续实施 — 当前没有核心接入 — 代价是端到端Fail-Closed仍未验收，02B及M0父任务不能完成。
- Final: Ruling: 文档所属KB、原生授权/tag交集、多KB分组和大库完整枚举在C-09薄适配POC验证 — 本端只消费调用方已授权候选，不重新授权 — 代价是调用方未经验证前不能接通；1MiB/10000候选限额不得截断或回退整库。
- Final: Ruling: 持久排除策略、实际执行期限及生产启用后续接线 — 本批只提供策略接口，5秒读取期限不等于策略执行超时 — 代价是生产Host仍503，不以演示策略开放许可。
- Final: Ruling: after-retrieve/before-generate/ingest持久ACK、整体ready、MCP及完整active/直接读/图谱治理保留后续任务或用户明确后移阶段 — 不把typed检索前handler当作全链路实现 — 代价是这些功能仍未就绪；ingest不提前ACK。
- Final: Ruling: C-10独立只读标签API后续实施，原生UI/远端未知结果竞态及既有持久组件本轮不重审 — 用户接受安全拒绝并限制本批范围，已有证据保留为各批记录 — 代价是无标签首写和跨系统覆盖风险仍存在；安全拒绝不是同步成功。
- Final: Ruling: 真实上游/LLM/数据库服务、混合ingest/生产负载、Linux race/远端CI、前端/上游完整构建测试未运行 — 本批只改扩展和文档，按AGENTS执行本轮完整扩展检查即可 — 代价是这些环境、负载与整体验收证据仍缺；独立复核只重跑contract/bridge，不能冒充独立完整suite。
- 下一依赖：持久排除策略与原生契约消费者/薄适配POC，以及其他Hook业务DTO/handler。生产Host不配置演示策略，readyz/业务Hook仍503；核心接入前复核C-05/C-09并明确对应授权。

## 执行记录：用户限定的原生HTTP门卫（2026-10-30）

- 范围：停止extensions新契约/组件开发；用户明确授权新增`internal/extensionbridge/client.go`、在`search_parallel.go`增加HTTP调用/固定日志/硬错误。生产补丁仅这两个文件，不改原有检索、过滤或调度代码；没有扩展区写入、依赖变更、提交或推送。
- 接入点：原生NeedsRetrieval跳过判断之后，pipeline start/Clone/RunParallel之前。客户端固定127.0.0.1:8090/bridge/before-retrieve、POST JSON、版本1、500ms；禁代理和重定向，只接受200；不读取/应用响应中的过滤结果。原始request/session/tenant/查询与现有原始ID字段只做传输。
- 失败路径：返回非ErrSearchNothing的PluginError，ErrorType=bridge_before_retrieve_failed，Err保留客户端错误；日志严格使用用户指定文本。现有session_knowledge_qa.go的硬错误分支返回err.Err，不进入无结果fallback；未改动该上层文件。
- 测试：`internal/extensionbridge/client_test.go`先因客户端缺失失败，再通过真实本机HTTP fixture验证POST/协议头/200、各非200、拒绝重定向、不泄露响应body、Host停止、500ms超时、取消及编码失败。测试把固定URL连接路由到测试自有临时端口，不接触用户8090进程。
- 原生回归：新增`search_parallel_bridge_test.go`，只在可独占8090时运行fixture，覆盖停止/503/超时阻断、200恢复原空检索行为及原skip保持；8090被用户占用则明确skip，不杀进程。该测试在本机尚未执行到业务断言：修改前完整包与本次定向命令均因CGO_ENABLED=0无法构建DuckDB windows-amd64依赖而退出1；没有伪称原生运行时验收通过。
- 当前限制：这是用户要求的HTTP连通门卫，非完整策略响应消费。C-09排除过滤、全库候选枚举、直接读/Agent等均不在本批实施；Host当前业务503意味着实际问答仍阻断，不能用healthz=200证明放行。
- 最终验证：`go test -mod=readonly ./internal/extensionbridge -count=1 -v`、`go build -mod=readonly ./internal/extensionbridge`、`go vet -mod=readonly ./internal/extensionbridge`均退出0；四个Go文件gofmt无输出。原生`go test -mod=readonly ./internal/application/service/chat_pipeline -run '^TestSearchParallelBridge' -count=1 -v`退出1，停在上述DuckDB构建限制，未运行业务断言。根go.mod/go.sum、Frozen和BASELINE的SHA256与开工前一致。
- 一次独立只读复核：用户限定的HTTP门卫无Critical/Important/Minor实质问题，独立重跑client测试退出0；确认上层硬错误传播，原有检索/过滤未变。主任务同步并核对文档，文档未另行独立复核。
- Final: Ruling: 原生运行时测试/E2E受既有CGO环境阻断，保留未验而不改依赖 — 代价是当前只有客户端网络行为实测与源代码错误路径证据，不能宣称完整M0-02C运行时验收。
- Final: Ruling: 按用户限定只做HTTP状态门卫，未解析策略响应/排除过滤/全工具治理 — 代价是200只代表本层继续，不代表治理策略已应用；生产Host仍503继续阻断。扩展及持久化旧组件本轮不重审、不改动，文档同步由主任务核对。

## 执行记录：WSL真实核心认证与RAG E2E（2026-10-30）

- 本轮授权：用户要求直接编写并运行E2E脚本，解决凭证取得、验证Host关闭时拦截及Host启动时正常放行；不允许以手动尝试、健康探针或Bridge 200桩替代。
- 环境核实：发现WSL Ubuntu上运行用户编译的`/mnt/d/LanRAG-Next/WeKnora-lite`，后续该原进程已停止，脚本没有杀原进程或未知端口进程。二进制确有指定门卫字符串，但build info没有Edition=lite注入。原DB只有1用户/1租户，没有模型/KB/文档/会话。因此在只读backup副本中启动同一真实二进制，创建独立测试账号/租户/模型/KB/文档/会话，使用本地OpenAI兼容夹具，未使用生产LLM或改写原库。
- 认证机制：RBAC=false不关闭Auth；原生middleware仍要求Bearer JWT/API key。命令行标准edition的auto-setup返回403；即使Lite edition也必须持有桌面专用token。脚本显式ENV_FILE加载已有配置，并以WEKNORA_WEB_DIR启用已有web静态资源，根页面由401变200；API匿名/me仍401。通过原生register201/login200取得JWT，认证/me200，凭据不输出，测试结束销毁副本。
- 新增`extensions/scripts/e2e-test.sh`、`e2e_test.py`、`e2e_test_test.py`；只在`extensions/bridge/server.go`增加固定请求接收日志及`receipt_test.go`，不输出body/header/凭据，不改变业务503。原生核心源码、依赖、Frozen/BASELINE和.env本轮未改。
- 最终实际命令：`wsl.exe -d Ubuntu -- bash /mnt/d/LanRAG-Next/extensions/scripts/e2e-test.sh --report /mnt/d/LanRAG-Next/docs/evidence/m0-e2e-2026-10-30.log`退出1。完整终端日志见`docs/evidence/m0-e2e-2026-10-30.log`；实际时间21:11（Asia/Shanghai）。
- 断网PASS：8090无监听；同一凭据和真实RAG请求返回HTTP200 SSE，但包含error/complete；关联核心日志出现指定GATE、bridge_before_retrieve_failed及connection refused，答案生成0。不会把HTTP200误判为成功。
- 通网FAIL：实际go run Host健康200、终端出现bridge_before_retrieve_received；同一JWT/session/query/KB/document请求仍SSE error，核心报before_retrieve returned HTTP503，答案生成0。根因`extensions/host/runtime.go:109`仍用无业务策略NewServer；未注入真实BeforeRetrieve服务。M0未验收，不能因为Host已启动而宣称放行。
- RED→GREEN：Host接收日志测试先失败再通过；脚本策略测试先因实现缺失失败后通过。首轮密码生成超过原生8..32限制，新增回归复现47字符后修正；JWT误带Host健康请求以回归复现并限缩到核心API。
- 独立只读复核发现日志字节/字符偏移混用，按证据正确性定为Important；新增中文前缀回归先失败，改binary seek/read后通过。SIGTERM默认退出会留下独立进程组和副本，复核初评Minor，主任务按资源所有权上调Important；对实际main注入SIGTERM，先复现无cleanup，再加信号处理后真实子进程/临时副本均清理。WSL九项脚本回归全部退出0，未派第二次复核。
- 本轮常规验证：扩展十二包Go完整test/build/vet、模块门禁244 imports均退出0；Windows Python十六例回归退出0（SIGTERM用例因平台skip），该SIGTERM用例已在WSL实际通过；gofmt/bash语法检查通过。Linux完整race退出1：十一包通过，Outbox的TestRunRetriesReportsAndDrainsHealthyBacklog断言calls=3 reports=2失败；单独重跑同样退出1，未出现DATA RACE报告。单列06C，不修改旧组件或弱化测试冒充通过。
- 最终补证：WSL Linux Go1.26环境执行Python全部十六例回归退出0、无skip。源DB复查仍为users=1、tenants=1、models/knowledge_bases/knowledges/sessions=0，与测试前一致；8080/8090可重新绑定，/tmp/agentrag-e2e-*剩余0。最终真实E2E退出1为预期的验收失败结果，不是脚本成功状态。
- Final: Ruling: 原生Hook/算法及既有扩展持久化治理不全面重审 — 本轮只增加验证与固定接收日志 — 代价是E2E不能覆盖这些组件全部门禁。
- Final: Ruling: 不修改原生认证/SSRF业务，不添加Bridge200桩或新策略 — 按原生JWT认证，只在隔离进程允许本地模型夹具并保留真实503 — 代价是正常放行仍失败，需要真实策略接线及C-09响应消费。
- Final: Ruling: 不做GUI/生产LLM/生产负载或全部M0验收 — 使用真实二进制/API/SQLite副本与本地模型夹具 — 代价是生产模型和浏览器交互证据仍缺，M0父任务保持进行中。
- Final: Ruling: 完整本地验证和修复后E2E由主任务执行，文档由主任务核对，不再派二次复核 — 以失败回归和本轮完整命令验证修复 — 代价是没有第二份独立修复后验收；远端CI未运行。
- Final: Ruling: Linux race既有Outbox报告次数断言失败单列后续，不在认证/E2E范围内顺手修复 — 本轮未改Outbox，真实通网也已被503阻塞 — 代价是Linux race门禁仍未通过，不能只凭Windows绿色suite宣布M0成功。

## 执行记录：默认Pass-Through与M0验收完成（2026-10-30）

- 用户决定：M0采用默认放行，不继续开发复杂治理过滤；真实断网/通网E2E通过后完成M0。既有Linux race失败接受为单机M0非阻塞项，记录M0-06C；未修复、未弱化旧测试，也不据此断言生产并发无风险。M1待开始，未混入独立解析代理开发。
- 实现：`extensions/host/runtime.go`为probe/worker默认注入`NewPassThroughRetrieveHandler`；`bridge/retrieve.go`严格校验后原样保留非空原生授权候选，不新增排除。无Services的NewServer仍503，其余三个Hook和readyz仍503；核心源码和检索/过滤算法本轮未改。
- RED→GREEN：先以缺失默认构造器/Host仍503复现失败，再完成接线。独立只读复核发现Important：空范围200+deny_all被原生状态门卫忽略；新断言先实际失败200，再仅在默认透传分支改为403并通过。固定scope handler旧契约保持；省略/null范围400，whole-KB枚举仍在C-09后续。
- 真实E2E：`wsl.exe -d Ubuntu -- bash /mnt/d/LanRAG-Next/extensions/scripts/e2e-test.sh --report /mnt/d/LanRAG-Next/docs/evidence/m0-e2e-2026-10-30-pass-through.log`退出0；修复后运行标识20261030-213758。认证通过，断网关联指定门卫日志/错误事件且答案生成0，通网Host收到同一请求并返回answer/references/complete、答案生成1。该文件保存完整终端输出，旧503失败报告保留。
- 测试边界：用户已编译的真实WeKnora-lite、实际go run Host、隔离SQLite backup及本地模型夹具，无Bridge200桩。脚本只更新范围说明，未削弱成功判据；原DB不写入，脚本拥有的进程/凭据/副本均清理。生产模型、GUI、上游全量构建/测试和远端CI本轮未验证。
- 修复后验证：完整十二包`go test -mod=readonly ./... -count=1`、build、vet均退出0；Linux Go1.26的`go test -mod=readonly -race ./bridge ./host -count=1`退出0，仅覆盖涉及包，不替代既有完整race失败。Python十六例本轮Windows退出0（SIGTERM一例因平台skip，上轮Linux同一套十六例无skip通过）；模块边界、格式和Git范围检查以本轮最终收尾结果为准。
- 复核边界：独立复核重跑bridge/host/contract退出0，发现上述空范围问题及待同步旧文案。实施者完成一轮修复、文档同步和完整Go/E2E验证，没有第二次独立修复后复核。持久化/Outbox/标签旧组件不全面重审，生产并发/负载、全部治理、Agent直接读/图谱、其他Hook/MCP未验收。
- Final: Ruling: M0按用户确认的HTTP门卫+默认Pass-Through范围已完成 — 真实断网与通网均有日志/SSE证明，脚本退出0 — 代价是复杂治理/范围消费及原冻结剩余任务继续保留后续，不以阶段完成抹去限制。
- Final: Ruling: 空文档范围用非200拒绝，不把200中的deny_all当作原生已执行 — 维持空范围不得回退whole-KB的安全边界 — 代价是无显式文档范围的原生整库请求暂不能使用本层默认透传，需C-09另行接线。
- Final: Ruling: 完整Linux race的Outbox报告次数断言失败按用户决定后移 — 当前两包race通过不表示旧失败消失 — 代价是仍需后续真实并发和事务复核，M0-06C不能标为已修复。

## 新任务占位（待填充）

- ID／状态：待填充
- 期望结果：待填充（描述可观察的交付及验收条件）
- 依据：待填充（用户请求、架构章节或具体源码／问题证据）
- 何时重新审视：待填充（时间、前置里程碑或可观察触发条件）
- 验收证据／完成日期：实施后填写
