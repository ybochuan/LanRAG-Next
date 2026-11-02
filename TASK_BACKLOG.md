# AgentRAG Task Backlog

## 本机启动闭环（2026-10-31）

| 任务 / 状态 | 期望结果 | 依据 | 验证及何时重新审视 |
|---|---|---|---|
| M2-OPS／本地闭环已验证 | 独立扩展库默认路径、严格手工DDL认领、可诊断初始化错误；一键Host本地Ready→Core、官方机器凭证初始化、Ctrl+C清理；完整启动指引。 | 用户本轮启动崩溃修复、一键脚本、真实M2上传E2E和运维文档请求。文件：`extensions/store/manual_schema*`、`host/{config,runtime,database_error}.go`、`bridge/server.go`、`extensions/scripts/{launcher_config,startup_e2e}.py`、`start-all.sh`、`data/.gitignore`、`docs/operations/AgentRAG-启动与运维指南.md`。 | 真实WSL原生TXT上传、2个chunk、Observer active、正常RAG、有效期输入后生产扫描stale、实际LLM上下文和引用无过期文档；Ctrl+C端口释放、源DB校验不变。完整日志`docs/evidence/m2-startup-e2e-2026-10-31.log`。更换启动配置/迁移/权限/网络拓扑时重审；本轮未重跑真实MinerU与云端模型。 |
| M2-UI提前／已交付（2026-11-01，生命周期范围） | 原生 `/platform/lifecycle`：库切换、状态分布与分页、日期、事件时间线、待复核、库级规则；沿用登录及KB权限。 | 明确授权原生导航/薄管理接口；真实Core/Host/SQLite上传、过期排除、8并发单赢家、401/403/404及浏览器只读验收，见 `docs/evidence/m2-lifecycle-ui-validation-2026-11-01.md`。 | 新增归档审批、谱系/修订UI或权限变化时；不随此项宣称其他M2/M6完成。 |

本文件只管理任务生命周期。强制规则见 `AGENTS.md`；稳定事实与基线源码映射见 `PROJECT_CONTEXT.md`。2026-10-29 用户已授权按方案开展项目；遇到扩大核心补丁或改变实现边界的冲突，先报告和决策，不擅自硬改原生代码。

架构依据统一指向 `docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`。已有明确的冻结里程碑，因此以下不是空占位 backlog，也不重新估算架构文档中的周期。

## 当前里程碑

**M0／已完成（2026-10-30，按用户确认的验收范围）。** 默认Pass-Through校验协议/格式及显式非空文档范围，原样保留原生授权候选；Host不可达时原生HTTP门卫fail-closed，Host运行时真实RAG完成。真实WSL E2E退出0：offline=True、online=True、M0=PASS；通网SSE含answer/references/complete且答案生成1次。完整日志：`docs/evidence/m0-e2e-2026-10-30-pass-through.log`。上一轮503失败日志原样保留。

用户明确接受既有Linux race断言失败，作为已知待办保留，不阻塞单机场景的本次M0验收。**这是用户批准的阶段验收，不声称Frozen原版所有治理/Hook/MCP/负载门禁均已实现。** 下表原M0编号中尚未交付或验证的项目保留真实状态，转后续增强；不得把它们伪记为已完成。

**当前推进：M2／进行中（2026-11-02）。** 生命周期、Observer、24h扫描、RAG策略、本地复核、登录管理界面与T3补充依赖已交付。用户已批准T4整批薄接线；来源底座、组件视图、有限原生文本输入/模型步骤与终态已接入，真实Core/Host已验证运行封存、停用/离线拦截及无开放运行遗留。实际写入producer与已封存文件正向链路继续实施，T5另列。M2尚未整体验收；M1真实语料标定等缺口保留。

## M4/M5 业务冲刺（2026-11-02）

| 任务／状态 | 期望结果 | 依据 | 验证及何时重新审视 |
|---|---|---|---|
| BIZ-01／后台主链已接线 | 原生JWT管理→同库关联；反馈→指导→提案→真实manual ingest/index→supersedes和旧文档停用。路径`extensions/business/*`、`management/business.go`、`host/runtime.go`、schema11；原生仅handler/management DTO/router薄转发。 | 用户本轮业务优先授权；`docs/operations/M4-M5-业务验收.md`及`docs/evidence/m4-m5-business-2026-11-02.md`。 | 并发8单赢家、审计失败回滚、远端丢ACK不重复创建、失效/撤销复查、PDF模式拒绝与真实Core/Host联合验证。对业务状态、权限或发布协议改动时重审；不代表完整M4/M5完成。 |
| BIZ-UI／待接线 | 关联候选确认、反馈待审/风险/冷静期、指导、提案内容与发布状态页面。现有后台列表已提供完整可审阅字段、ID读取和分页。 | 用户希望便于验收；冻结方案第13节，复用原生登录管理API。 | 前端实施时只调用已鉴权API；完成类型/构建/权限与真实浏览器验收后交付。 |
| M4-MCP／待接线，存在既有拒绝冲突 | 经原生按需发现调用可信Host的query_related_docs，保留scope/active/版本门禁；不放开不透明外部MCP。候选`extensions/mcp/related_docs.go`及既有原生MCP薄点，实施前核实具体调用位置。 | 冻结9.4；现有M2/T4未知MCP摘要/schema/结果拒绝会阻挡此路径。当前管理查询成功不能算Agent已可用。 | 可信描述/参数/结果来源方案确认并做真实discover→call与停用/Host离线测试时；不重构ToolRegistry。 |
| M4-CAL／待真实标注 | 20–50组每库关联对标定权重，真实precision≥0.70/recall≥0.60；Agentic问题集与GraphRAG预算门禁。 | 冻结9/14；本批默认0.60/0.25/0.15及合成用例不构成指标。 | 人工关联标签到位时；跨库/Graph申请需显式确认，默认Graph关闭。 |
| M5-QA／待接线 | 原生本轮retrieved_chunk_ids与身份绑定的反馈采集；指导fail-open末尾注入、每轮≤3、每guidance每session≤2、session≤800 tokens；来源组成随真实输入登记。 | 冻结10/12；当前管理录入不冒充问答采集，未把指导注入模型。 | 核实原生薄点及实际输入组成后；测试本轮不反哺、预算/去重/取消/审计与停用、所有模型请求捕获。 |
| M5-PROJECTION／待接线 | 消费已持久family_tag意向，使用既有单写者/KB原生tag ID投影gov:family，不影响truth；提供失败修复/重试。 | 当前worker仅有state/alert处理，family_tag可能重试/dead；C-10未知tags拒绝继续有效。 | 家族标签读/写适配完成时；未知tags不误删、并发投影、源事实不变及不重复ingest验收。 |
| M5-BINARY／待接线 | binary_source建议→用户上传正式修订文件→scope/版本绑定提案→原子finalize。路径复用M1原生上传与`extensions/business/revision.go`，不自动把PDF/DOCX换成Markdown并声称正式发布。 | 冻结12.8；当前自动发布仅manual_content，其余记录binary_source_upload_required。 | 正式文件上传/批准语义落地后；同库身份、未知ACK、哈希/版本不符、回滚与源原件保留测试。 |
| M5-ACCEPT／待完整验收 | 自动激活扫描、快速通道10–20%抽检/主题恢复、完整SQL/进程崩溃/负载矩阵与每库生成质量回归。 | 冻结B/C/D及12/14；当前显式激活、临时库时间推进测试不能冒充真实等待或全阶段验收。 | 前述业务/模型/UI接线完成及真实样本可用时；保留M2/T4与M1既有缺口。 |

## T4 实施与验收门禁（2026-11-02）

用户2026-11-02要求业务冲刺，当前优先推进M4/M5后台接线；T4未完成项保留，不回退已生效治理。

| 任务／状态 | 期望结果 | 依据 | 验证及何时重新审视 |
|---|---|---|---|
| T4-00／依赖许可修复已验证 | 副本读/登记展开最新T3父依赖，最终事务重查；权限依赖不混入实际模型来源。文件：`extensions/provenance/{permission,policy,manifest}.go`、`lineage/dependency_ledger_test.go`。 | 已批准T3语义及T4方案，5个错误放行先RED再修复；只读复核无Critical/Important。 | active/review、父stale/过期/编辑/离线/损坏、并发新增依赖、再派生已测；扩闭包或改依赖语义时重审。 |
| T4-01/02／底座已实现，矩阵待补全 | 完整root绑定的组件视图、schema10运行/步骤/核验意向/producer与回执审计；未知/未完成/空源不能封存。路径：`internal/handler/extension_source_*`、`extensions/{lineage/components,provenance/*}.go`、迁移010。 | v1 golden、字段/root错配拒绝、16并发单赢家、幂等/错误绑定、核验/结束竞争、审计回滚与提交前后进程退出恢复。 | 见本轮证据；seal/producer完整SQL故障、多进程/容量矩阵及真实原生E2E尚未全部覆盖，不标整项完成。 |
| T4-03／原生文本主链接入，完整组成待补 | QA以原授权scope/owner/native消息身份建run；completion/stream、think/act登记实际指纹及独立步骤，read_document与无重排search文本核验；终态收束，取消/证明失败不封存。路径为批准表中的QA/pipeline/engine/think/act及search/read/list。 | `docs/evidence/m3-t4-native-inputs-2026-11-02.md`：真实Core/worker Host合成模型运行已封存；旧二进制缺事实、MCP元数据提前加载、终态遗留open先RED再修复。 | 查询改写/重排、FAQ/图像/合并、自定义元数据及listing updated_at仍unattributed；全部组成/并发及真实文件联合验收前不标整项完成。 |
| T4-04／未知读取已拒绝，正向producer待接线 | 实际写入、原run/step/path/hash、旧源继承、收集与资源别名闭环，已封存active副本可读，撤销后拒绝。 | 收集由`internal/handler/session/agent_stream_handler.go`在QA调用外触发；用已批准collector/读取薄点与原生消息身份适配，不改handler/调度，不让新run认领旧文件。 | 当前未知文件/缓存、追加与编辑拒绝；补真实写→归档→跨轮读、覆盖不缩源、读后替换/同mtime、ACK未知不重放及重启撤销。 |
| T4-05／拒绝代码已接入，联合捕获待验 | shell启动前拒绝；未知memory/skill/目录/网页/后台正文不进模型；未知外部MCP服务摘要/schema/pin在模型读取前省略；保留任务控制、用户附件，图片先于VLM拒绝。 | 定向RED→GREEN与原生包回归；MCP描述spy验证加载次数0。不改ToolRegistry、存储或调度算法。 | 全部LLM/VLM/重排输入捕获及通知ACK/取消真实联合E2E前不标完成。 |
| T4-06/M2／待整体验收 | 真上传→Observer→文本来源→模型→producer→文件→撤销/重启，内置模型正向冒烟、故障捕获、全检查与最终整批复核。 | 用户M2/M3目标；本批无生产替换、真实库迁移、提交/推送。 | 组件验证与真实E2E分开报告；T4全部门禁完成后再评估，T5不在本批授权。 |
| TEST-MODEL-PORT／已知测试隔离问题 | 并行端口观测不误报临时端口，不弱化断言。文件：`extensions/modelsupervisor/probe_test.go`及观测实现。 | 普通全量退出1，仅`TestObservePortsReportsOnlyListening`失败；隔离串行全量/race退出0。本批未改该模块。 | 独立处理模型管理器/网络观测时重审；保留两种运行结果，不抹去原失败。 |
| TEST-NATIVE-RACE／既有工作区失败待核实 | 消除认证、沙箱和技能测试共享fake数据的竞态。文件：`internal/application/service/tenant_{api_key,sandbox_config,skill_install,skill_served}_test.go`及对应实现。 | 2026-11-02六包完整race退出1，service中5个测试报告数据竞态，其余5包通过；普通五包全量退出0。本批未修改这些失败路径，未在干净基线独立复现，不能直接称上游已知缺陷。 | 独立处理这些测试/服务并完成基线对照时；不以来源定向race替代失败的完整race。证据见`docs/evidence/m3-t4-native-inputs-2026-11-02.md`。 |

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
| M1／进行中 | PDF预检、原生身份薄接线/Host、真实4.0.1单job与文件归档→原生分块闭环已验证；SQL产物/恢复、真实标定和完整M1验收仍待做。 | 架构7/15及用户M1-03/原生接线授权；每库指标不以合成文件替代；C-11–C-17。 | 真实语料、容量/恢复、规则及后端版本变化时。 |
| M1.5／待开始 | 完成库级检索基线、RRF 参数实验、重排降级与查询扩展门控；量化只通过 feature flag 验证，保留原生 RRF 实现。 | 架构 14–15；Recall@5 ≥ 0.85、MRR ≥ 0.80（或基线 95%）；量化 Recall@10 退化 ≤ 2%。 | 开始检索优化前；模型、语料或 sqlite-vec 基线变化时。 |
| M2／进行中（内核/Observer/扫描/普通RAG策略已接线） | 已实现默认矩阵、DB规则/标注、原子决策回执和两类stale；Observer/每日扫描、普通RAG策略及本地复核告警已接线；外部通知及生产配置入口待做。 | 架构不变量A/4/15及用户2026-10-31业务规则；五处故障回滚、真实崩溃恢复和24并发CAS验证。 | 自动接线前、业务矩阵/有效期规则变化时；不以组件测试代替全阶段验收。 |
| M3／进行中：T1/T2已交付 | 已完成原生只读快照、schema8观察版本/family、真实源权威、分边查询及环防护；Critical Edge业务/完整来源封存继续实施。 | 用户批准与`docs/evidence/m3-trusted-sources-2026-11-01.md`；不将观察顺序称原生generation。 | 新边类型、真实来源适配、关键事务和原生接线变化时。 |
| M4／进行中：后台关联已接线 | 同库amends/interprets声明、related_to候选/确认/撤销、双向active/当前版本过滤并联合T3 supplements；原生JWT管理API可调用。MCP消费、按库标定、跨库、UI与Agentic/Graph预算待做。 | 用户2026-11-02业务冲刺授权；`extensions/business/related.go`、管理薄转发；见业务验收记录。精确率/召回率不以合成用例冒充，GraphRAG保持关闭。 | 接原生MCP、标注数据可用、跨库或GraphRAG申请时重审。 |
| M5／进行中：后台反馈与manual发布已接线 | 管理反馈→审批/冷静期→指导→修订提案→原生Markdown创建/索引→supersedes/旧文档停用；Saga先对账、治理+audit同事务。真实问答事件、guidance预算注入、抽样复核、binary正式文件、family投影和UI待做。 | 架构B/C/D；真实Core/Host业务E2E与SQLite并发/审计故障/未知ACK测试；`extensions/business/{feedback,revision,native}.go`，schema11。 | 详见新增细分条目；不据本批声明M5或M2整体验收完成。 |
| M6／待开始 | 实现 opaque mDNS、认证能力查询、静态节点回退与架构列出的治理／运维／评估 UI；广播不含知识库元数据。 | 架构 11、13、15。 | 开始局域网发现或扩展 UI 前；暴露的网络范围和权限模型变化时。 |

## 细分实施任务：文件、验证与依赖

验收编号和操作步骤见 [M0 Change Map](docs/design/M0-Change-Map-2026-10-29.md)。新增文件均为计划落位；标记“受控候选”的现有核心文件尚未修改。父任务按用户明确确认的验收范围判断；本次范围之外的原冻结条目保留真实状态作为后续任务，不得冒充已完成。

| ID／状态 | 期望结果／涉及文件（仓库相对路径） | 验证方法 | 依据／冲突／何时重新审视 |
|---|---|---|---|
| M0-01A／已完成（审计交付） | 源码审计与冲突报告：`docs/design/M0-Change-Map-2026-10-29.md`；完整路线见本表。 | 逐条对照真实函数和行号；原生diff为空；不是M0验收通过。 | 架构16/17.3；核心接入前、基线改变及用户决策后重审。 |
| M0-02A／已完成（协议基础） | 协议版本、存活／就绪、无服务拒绝：`extensions/bridge/protocol.go`、`server.go`、`server_test.go`。 | FC-0；37个HTTP子用例通过；不是原生全链路证明。 | 架构3.2/3.6/3.7；业务handler接入时重审。 |
| M0-02B／已接线（本批普通RAG消费范围） | `extensions/retrieval/policy.go`查生命周期DB；原生`extensionbridge/{client,response}.go`校验策略并由`search_parallel.go`过滤结果。 | DB真实Host＋原生PluginSearchParallel跨进程通过；允许/排除分割、deny_all、scope、坏响应fail-closed；全库枚举及其他入口仍待做。 | 用户2026-10-31批准原生消费；部署新版核心/Host、候选枚举和Agent接入变化时。 |
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
| M0-06C／已修复（M2内核批次，2026-10-31） | `outbox/worker.go`仅抑制纯父上下文取消的额外报告，保留业务错误join取消；`worker_test.go`新增两例，原严格drain断言不改。 | RED复现report1而期望0；修复后扩展全suite/build/vet及Linux全race退出0。历史M0豁免记录保留。 | Worker取消/错误分类、租约或部署来源变化时。 |
| M1-A／进行中 | PDF Cheap Preflight、原生薄客户端/Host及真实上传归档→分块已交付；持久恢复/SQL产物、全来源/权限重查及每库标定未完成。 | PA-1；合成PDF上传不等于生产语料验收；C-12–C-17。 | 生产启用前、原生多来源/身份、容量/恢复、真实语料及OCR资源就绪时。 |
| M1-B／待开始，依赖M1-A | 版本化原始产物：`extensions/parser/artifacts.go`、计划`extensions/store/migrations/003_parser_artifacts.sql`（002已用于M0，实施时核实下一版本）。 | PA-1：真实服务产物、schema/MinerU版本、无二次解析，失败与重试可追踪。 | 架构7.4；真实MinerU服务及产物契约验证后。 |
| M1.5／待开始 | 评估/调参/量化实验：`extensions/eval/retrieval.go`、`rrf_grid.go`、`quantization.go`、`testdata/retrieval/`；原生租户retrieval-config API。 | RE-1：每库30–50查询，Recall/MRR基线、flag隔离、Recall@10退化≤2%。 | 架构14/15；M0通过，模型/语料/sqlite-vec变化时重审。 |
| M2／进行中 | 已完成内核/schema4、Observer/每日扫描/普通RAG DB策略；剩余接线见下方M2细分任务。 | LC-1/TX-1/OB-1；用户正式默认矩阵、注入时钟、事务/并发/崩溃回归通过。 | 自动运行前，规则及标注接口变化时。 |
| M3／进行中 | 已交付`extensions/lineage/`观察版本/family/来源权威/分边查询，schema8；完整Critical Edge业务与答案输入来源封存后续。 | T1/T2真实原生上传/编辑/旧证据拒绝E2E与SQL/进程恢复、完整8原生包及扩展/race通过；见本轮M3证据。 | 架构5/8；来源权限/原生薄接线/新边类型变化时。 |
| M4／后台关联进行中，评估待做 | 已实现`extensions/business/related.go`并复用lineage/dependencies；MCP、每库calibrate、graph_budget与Agentic评估继续待做，不预建空组件。 | AS-1真实指标/跨库/预算门禁未验收；本批关联声明/当前版本/撤销已通过组件和原生API验证。 | 原生MCP恢复、真实标注或Graph申请时重审。 |
| M5-A／待开始 | 反馈指导治理：`extensions/feedback/events.go`、`governance.go`、`cooldown.go`、`governance_test.go`；扩展预算组件。 | GD-1/RV-1/TX-1：冷静期/二次确认/抽检/熔断/audit同事务/质量回归。 | 架构10/12；M0–M4基础稳定、审批策略明确后。 |
| M5-B／待开始（C-06 POC） | 发布Saga与对账：`extensions/revision/saga.go`、`remote_identity.go`、`reconcile.go`、`publish_recovery_test.go`。 | RV-1：提交后响应丢失先对账，create保持1；未知状态不重放；binary源需用户正式上传。 | 不变量B/C/D、架构12；远端身份POC通过后。 |
| M6-A／待开始 | mDNS/认证能力：`extensions/discovery/mdns.go`、`capabilities.go`、`static.go`、`discovery_test.go`。 | DS-1：广播字段抓包、无KB元数据、认证拒绝、静态回退。 | 架构11；公开网络范围与认证模型确定后。 |
| M6-B／待开始 | 独立UI与最薄bootstrap：计划 `extensions/ui/src/routes.ts`、`views/`、`api/`；受控候选 `frontend/src/router/index.ts`。 | DS-1；前端测试/type-check/build；库切换权限，溯源/版本历史分离，运维真实状态。 | 架构13；独立构建/发布路由方案核实后，不提前重构前端。 |
| POST-M0-GOV／后续机制 | Agent直接读/旧handle/图谱/中途停用的完整治理；候选 `internal/agent/tools/read_document.go`、`query_knowledge_graph.go` 或共用执行门禁。 | FC-3；M0未验证，不得宣称所有工具已受控。 | 用户C-03决定；M0后结合真实绕过案例和补丁预算重审。 |
| POST-M0-TAGS／后续，未实施 | 独立标签只读API，候选`internal/handler/knowledge.go`、`internal/router/routes_knowledge.go`；扩展`taggov/http_store.go`适配。 | 无标签显式[]、查询故障非2xx、跨tenant/KB拒绝；保留业务标签，重试后收敛；核心补丁范围须单独核实。 | 用户2026-10-30接受C-10安全拒绝并选择后续只读API；启动该任务并明确核心薄补丁授权时重审。 |

## M1细分任务（2026-10-30启动）

完整源码映射、文件路径及测试步骤见M1 Source Map；下表的预期不代表已验收。

| ID／状态 | 期望结果 | 依据 | 何时重新审视 |
|---|---|---|---|
| M1-00／已完成 | 源码映射与首批计划落盘，M0提交及本机4.0.1/可用PDF库核实。 | 本轮Git/包元数据/固定源码读取；Source Map。 | HEAD、MinerU或运行OS变化。 |
| M1-01／已完成（协议/显式scope POC） | scope及V1构件通过冷/热源字节证明、跨scope拒绝、同进程回执/未知结果门卫；真实4.0.1 Flash/txt两job/ZIP/版本/缓存计数退出0。一次复核及一轮修复完成；不启用Host，不代替原生上传API、KB权限或Doc归属。 | 2026-10-31计划、scope POC报告与本批执行记录；C-12自动绑定/C-13持久恢复/C-14 OCR资源仍保留。 | 预检与生产接线、原生多来源/身份、后端版本及M1最终验收时。 |
| M1-02／已完成（纯决策函数） | parser/router.go及router_test.go覆盖四档/跨页/native/空预检/稀疏比例；定向与最后完整十三包测试退出0，Linux parser race退出0，一次实际独立复核无实质问题。不代表文件预检或M1已验收。 | Frozen7.1/7.2；本批RED→GREEN及最新执行记录。 | 规则变化、真实预检接线及每库标定时。 |
| M1-03／已交付（PDF支持子集） | preflight.go使用pdfcpu v0.16.4；真实PDF统计总页/结构文本层/纯图页，冷/热源均预检；强制有效flash/txt且去重前覆盖auto/ocr/advanced；ActualText、Form/压缩/隐藏层/低字符码、坏文件/取消/解码超限回归。真实4.0.1合成文本文件无OCR下载证据；不代表全语料/Unicode/乱码或图片文件检测完毕。 | 用户2026-10-31明确纯Go依赖/路由要求，覆盖Frozen7.2 PDF升档；计划/证据见本批执行记录。 | Tagged结构归属、inline/加密/复杂真实文件、预算及每库标定变化时。 |
| M1-04／已交付原生PDF接线（恢复待续） | 三处原生生产文件仅注入转发；parser/native.go、host/config.go/runtime.go、bridge/server.go挂载可选鉴权路由；身份/版本拒绝、配置关闭、失败不回退、Linux构建与真实上传已验证。非PDF/URL保持原生；持久恢复另列。 | 用户明确批准薄补丁和Host闭环；C-12 PDF缺身份已解决，C-13不等于跨重启exactly-once。 | 正式启用前，原生升级、多来源、重试/身份/端点变化时。 |
| M1-04D／已交付（可信核心身份） | Knowledge的tenant/KB/doc通过Header逐请求传递；NativeHandler每doc独立scope自动绑定，同字节两个doc独立job/归档、跨doc拒绝；原生并发身份与后台cancel测试通过。涉及parser_proxy.go/native.go/scope.go/mineru_upload.go及测试。 | 用户本批授权；不从hash/file/job猜doc_id；Host信任核心，不独立回查原生权限。 | 多核心共享key、所有权变化、文档修订/重解析或身份契约变化时。 |
| M1-05／已交付SQL与进程恢复，断电待验 | 003_parser.sql及recovery.go完成ext_parser_artifacts原始JSON、格式/schema/MinerU版本、正式任务及身份FK；FULL同步DB，落盘后登记再返回ZIP；登记失败503，重启可核验文件并补登记。v1/v2升级保留原数据、进程直接退出/并发/跨身份及真实SQL原字节检查通过；不宣称文件系统与SQLite跨资源原子或物理断电已验收。 | 用户M1剩余任务授权、Frozen7.4；本轮证据m1-recovery-calibration-2026-10-31.md。 | 生产启用、存储设备/挂载方式、schema、版本或身份变化时；断电专测另列05P。 |
| M1-06／工具已交付，真实标定待语料 | calibrate.go、cmd/parser-calibrate、空manifest.json；真实文件哈希核对、按tenant/KB计算验证集85%/10%/15%、拒绝重复内容/文档族及空集、不使用验证集调参。用户确认暂缺真实PDF/扫描件与人工标签，故未报告真实指标或完成阈值拟合。 | Frozen7.3；用户本轮回复“暂时没有”；合成测试只验证工具。 | 取得每库独立人工标签、train与validation语料后；分布/规则/模型变化时。 |
| M1-07／已交付联合回归，整体验收待语料/档位 | 既有真实模型证据保留；新增--joint-m0实测上传→预检→唯一job→FS/SQL原始产物→原生分块，重启生产Host复用同job；同一凭证/问答断网关联拒绝且答案生成0、通网收到门卫且答案生成1。此联合模式用本地确定性模型，不宣称再次调用DeepSeek。生产语料/其他档位及03/06完整验收仍待续。 | 两批证据文档，当前真实核心/Host/MinerU4.0.1联合脚本退出0。 | M1最终验收前，真实语料、OCR资源与其他档位就绪时。 |
| M1-04R／已交付（已知恢复与受控容量） | 以已验证内容hash+tenant/KB/doc+有效策略+后端去重，SQLite先提交intent；已知job/权限可跨重启恢复，未知结果不重发。空闲且持久状态成功的scope可驱逐恢复；130完整文档、12并发、活跃job不驱逐、子进程直接退出与真实Host重启复用单job验证通过。128仍是同时驻留上限，满活跃或写失败时503；同doc自身128次上传上限未移除。 | C-13/C-16；recovery.go/native.go、003迁移及恢复测试。本地唯一Host串行文档入口，原生调度未改。 | 多Host、多核心并发或扩展配置/后端更换时；未知远端结果需人工/唯一关联对账，不能宣称exactly-once。 |
| M1-05P／目录屏障及崩溃窗口已交付，断电待验 | artifacts.go、artifact_sync.go/windows.go及同步/崩溃测试：非Windows尝试文件Sync→暂存目录Sync→Rename→根目录Sync，再允许SQL/ZIP；失败503且不登记，已发布文件重试补屏障。四个真实退出窗口及9个行为场景验证单job/原件/SQL/FK/跨身份/遗留pending；Windows与WSL通过，WSL另在仓库v9fs磁盘路径验证。Windows保持原先文件Sync/rename，无目录刷新承诺。 | Frozen7.4及本轮M1继续授权；docs/evidence/m1-artifact-publication-2026-10-31.md。物理断电、祖先目录创建、目标设备/挂载语义仍未验，不自动清理pending。 | 确认生产OS/卷/备份方式后，进行受控断电或可丢弃VM测试；生产验收前。 |

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

## 执行记录：M1启动与首批纯路由（2026-10-30）

- 基点：用户确认M0已验收/提交，本地HEAD为d1c62cd，开工clean；在当前D:/LanRAG-Next建立codex/m1-parser-routing分支，未创建额外worktree、提交或推送。
- 环境：用户指定D:/mineru-venv，元数据实测MinerU4.0.1、pypdf6.19.0、pypdfium2 5.13.0。包源码保留Uploads/Jobs及版本化产物；尚未实连API Server/推理模型。未安装、升级或修改venv，未启动GPU服务或下载模型。WSL默认Python不具备这些库，Docker守护进程不可连接，均只记录实际可见证据。
- 交付：新增M1 Source Map、首批实施计划和extensions/parser/router.go、router_test.go；同步PROJECT_CONTEXT与本backlog。core、Frozen、BASELINE及两module依赖与HEAD相同；M0门卫和Host未更改。
- RED→GREEN：先写路由测试，go test -mod=readonly ./parser -count=1因no non-test Go files退出1，再最小实现退出0。覆盖四类OCR、所有布局信号、同页/跨页最大档位、各图像四档、native不入档、空预检/未知类型拒绝、稀疏1/2与2/3，以及Advanced页之后继续统计。
- 验证：开工十二包完整测试退出0；新增后普通完整suite一次Outbox旧断言calls=3 reports=2退出1；补齐图像四档覆盖后的最后go test -mod=readonly ./... -count=1十三包退出0，旧问题未修复。build/vet、模块门禁245 imports均退出0；Python十六例退出0（Windows SIGTERM一例skip）；最新Linux Go1.26 parser包race退出0。gofmt无输出；最终Git范围/依赖检查通过。
- 独立复核：第一次Astra调用因额度限制未产生评审结论，随后可用模型完成一次实际只读复核，无Critical/Important/Minor实质问题；独立parser测试和diff --check退出0，确认core无改动。未派第二次实际复核或修改既有Outbox。
- 边界：未验证真实文件提取、信号完整性、HTTP/scope、MinerU实连/单job、产物与恢复、每库标定和M1 E2E；未重跑上游全量/前端/生产模型/远端CI，也没有对M0持久组件全面重审。独立复核没有替代主任务的完整suite。
- Ruling: 继续沿用Frozen和已有4.0.1，首批只交付无I/O纯路由 — 避免新协议/环境升级及核心改动 — 代价是实际预检、代理和业务闭环仍未交付，M1不能标完成。
- Ruling: 同页OCR+DenseGraphics选Advanced，其他布局Standard，文档取页级最大需求 — Frozen允许的高密度布局选择具体化，避免跨页错误升级 — 代价是密度信号与真实档位仍需每库标定，不能宣称85%准确率。
- Ruling: 既有Outbox偶发断言只补证、不顺手修或减弱断言 — 本轮路由无共享状态且Outbox源码未改，M0接受决定保留 — 代价是完整suite仍有已知不稳定性，最终复跑0不能代表已解决。

## 执行记录：M1-01 显式scope/V1兼容POC（2026-10-31）

- 基点fa6353e，用户已提交首批M1，开工clean。仅新增独立parser协议/作用域构件、HTTP测试和POC脚本，更新说明；不改core、Host、schema或依赖，不提交/推送。
- RED→GREEN：先写HTTP测试，因ScopeBinding/NewScopeProxy未定义退出1后实现通过；身份解析测试先缺ResolveScope/ScopeIdentity再通过；结果file不能当input的回归以临时恢复旧逻辑实测失败，再还原修复通过。并发回执快照在锁内读取，网络写不持scope锁。
- 能力：固定回环origin、不跟随重定向/环境代理、上下游凭据分离；每scope静态tenant/KB与key，跨scope句柄拒绝；后端缓存源也要求实际字节/SHA256；结果只授予读取能力。只支持单file_id+ZIP原生子集；同进程已知回执复用、未知结果拒绝盲目重发，状态/容量均有限，不提供重启恢复。
- 真实实测：m1_scope_poc.py复用D:/mineru-venv的4.0.1，私有home/uploads与随机回环端口。最终使用Flash/txt和local/offline，两个scope两个正式job，直接确认后端热缓存命中、跨scope拒绝、版本4.0.1及ZIP；退出0。成功报告docs/evidence/m1-scope-poc-2026-10-31.md；后端配置和用户模型目录未修改。
- C-14实际发现：第一次auto实测45.15秒超时，私有home已获取1780590字节ONNX OCR资源，不能称为零下载。切txt仅针对已有文本层fixture验证协议；未测试OCR或其他三档，不以静默改档满足M1。首次失败摘要单列，普通模型资源位置/就绪在后续正式解析前核实。
- 清理：新脚本路径校验与限时重试由Windows真实文件锁回归覆盖；本轮成功试跑自己的新临时目录清理完成。首次失败目录agentrag-m1-scope-5uoknq6h清理请求被自动审批拒绝（仅给出blocked by policy），未换工具绕过，残留C:/Users/ybochuan/AppData/Local/Temp/agentrag-m1-scope-5uoknq6h。后续未查到Python进程；先前孤儿进程判断证据不足，已更正，不猜测具体占用方。
- 原生SDK验证：Windows定向命令因internal/utils/inject.go引用pg_query.Parse/Deparse且CGO不可用而退出1，未执行断言；WSL CGO=1运行同一docparser目标测试退出0。不改原生代码/依赖修环境，也不将这些原生fixture测试冒充原生API→代理→真实服务联合E2E。
- 完整检查：本轮完整十三包先重现旧Outbox TestRunRetriesReportsAndDrainsHealthyBacklog(calls=3 reports=2)退出1；修复后的最后同一完整命令退出0，旧问题未修复。build/vet、245 imports模块门禁及最新Linux parser race均退出0；Python二十例退出0（Windows SIGTERM一例skip）。最终真实4.0.1 POC于01:13:03启动，退出0，明确热缓存命中、两scope两job与ZIP；资源快照无ONNX权重。原生SDK定向WSL退出0，Windows构建失败已单列。
- 一次独立复核：Critical0、Important1、Minor2。确认async失败HTTP200也会带后端str(exc)，新增create/poll/cancel/cache回归先失败再掩码，保留失败状态与固定parse_failed，不回传异常参数；其代价是减少公开错误细节。endpoint文档误带/v1会实际阻断原生调用，主任务按功能影响上调Important并修正为不带/v1；prose不写文本镜像测试。清理失败仍声称stopped涉及证据准确性，同样上调Important，OS拒绝模拟回归先失败再增加确认逻辑后通过。只做这一轮修复，没有第二次独立复核；修复后完整Go/脚本/真实后端检查由主任务执行。
- 验证边界：未重跑上游全量/前端/生产LLM、原生API→本代理→真实服务联合E2E、真实权限/Doc关联、预检、持久产物/重启恢复、OCR/其他档位或每库标定。独立复核未重跑真实服务、Linux race、完整suite或原生SDK，也未重审M0或清理被拒绝目录。
- Ruling: 本批完成协议及显式scope POC，不启用生产Host — 原生身份仍只来自配置断言，预检/产物/持久恢复未接线 — 代价是C-12自动KB/Doc证明与C-13重启恢复仍需后续，M1不能标完成。
- Ruling: 后端缓存也要求本地上传字节与hash证明，不用hash猜测KB权限 — 保住未来预检与每scope源能力边界 — 代价是热缓存仍有回环源字节传输，POC还不提供性能/持久缓存保证。
- Ruling: 真实PoC固定文本层PDF+Flash/txt+local/offline，auto失败及私有ONNX获取另记 — 不把资源未齐的OCR流程伪装成功 — 代价是其他三档/auto/OCR能力尚未验证；首次失败目录清理被审批拒绝后保留，不绕过。
- Ruling: 对异步失败也掩码，纠正endpoint和退出证据；旧Outbox不顺手修 — 本轮限定解析协议边界且旧失败与新代码无依赖 — 代价是后端公开诊断更少、完整suite仍有已知不稳定性，不以最终绿色抹去。

## 执行记录：M1-03 真实PDF预检与显式文件归档（2026-10-31）

- 依据：用户明确引入pdfcpu纯Go库，文本PDF禁auto/OCR、只纯扫描可OCR，接代理并保存middle.json。基点仍fa6353e；保留M1-01未提交工作；只改扩展及说明，未提交/推送，不挂载生产Host或改核心。
- 计划：docs/superpowers/plans/2026-10-31-m1-pdf-preflight.md；依赖固定pdfcpu v0.16.4，Go1.26兼容；只有extensions/go.mod/go.sum变化，根依赖不变。pdf_probe.py计划被本轮用户明确的纯Go方案取代。
- RED→GREEN：新预检API缺失先编译失败；真实PDF字符/TJ/hex/隐藏层、混合/纯图/空白/字体资源/坏文件/取消/压缩Form/解码超限验证；代理unsafe偏好先造成3job而非1、损坏PDF先204，接线后均通过。低编号编码真实字符与未确认文本字节的扫描误判、提前ZIP绕过归档门卫也实测失败后修复。
- 产物：启用WithArtifactRoot后须可信BindDocument；缺ID为409且0job。完成ZIP的正式middle_json.json保留字节落为{root}/{doc_id}/middle.json，manifest记录真实格式/schema/MinerU版本及scope/source/hash；8并发临时真实文件系统写入幂等、内容/归属冲突拒绝，路径与多份middle拒绝。不是SQL/断电/崩溃恢复验收。
- 首次真实验证01:49:14退出0，报告docs/evidence/m1-preflight-2026-10-31.md。日志当时“zero external DNS/TCP”仅表示Python socket审计，不代表系统级网络隔离；最终日志已明确限定。
- 独立复核一次：Critical0、Important1、Minor0。ActualText直接/命名属性被当纯图的Important确认；新增实际PDF/代理回归先失败，再支持可解码ActualText，结构树MCID未解析时不授予纯图许可；未知操作符误授OCR回归亦先失败后拒绝。修复后parser全包退出0；只有此一轮修复，没有第二次复核。
- 最终真实验证02:05:03：`python scripts/m1_scope_poc.py --mineru-python D:/mineru-venv/Scripts/python.exe --report ../docs/evidence/m1-preflight-2026-10-31-final.md`（extensions目录）退出0。1页/1文本页；上游收到flash/txt而非调用方auto/ocr+advanced；2scope共2正式job、真实热缓存命中；明确测试doc_id下middle.json、schema2.0/MinerU4.0.1；退出后Python受审计外部DNS/TCP尝试0、私有目录ONNX0，清理确认成功。
- Go完整命令`go test -mod=readonly ./... -count=1`：首轮退出1，仅既存Outbox TestRunRetriesReportsAndDrainsHealthyBacklog（calls=3 reports=2）；修复预检后最终相同命令十三包全部通过、退出0。旧不稳定性未修，不以绿色抹去首轮失败或扩展M0豁免为M1全量验收。
- 最终`go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、`gofmt -l .`均退出0（格式无输出）。`python scripts/check_module_boundary.py`退出0，306 imports；`python -m unittest discover -s scripts -p '*_test.py' -v`退出0，21例、Windows SIGTERM一例skip。WSL/Linux CGO=1 `go test -mod=readonly -race ./parser -count=1`退出0；未声称全模块race通过。
- 范围：git diff --check退出0；internal/及根go.mod/go.sum无diff，BASELINE/Frozen与根依赖SHA256保持原值。未跑上游全量/前端/真实LLM、实际OCR/模型下载、原生上传→代理→索引联合E2E、权限/Doc自动绑定、产物SQL/崩溃/断电恢复或每库语料标定；M1总体不标完成。
- Ruling: 用户最新PDF规则优先于Frozen7.2升档 — 任何已确认文本页固定flash/txt，混合页复核，只有全纯图basic/ocr — 代价是复杂文本布局不升Standard/Advanced、混合扫描页不自动恢复。
- Ruling: 文本层按结构证据检查，不冒充完整Unicode/乱码或每库准确率；unsupported/不确定拒绝 — 代价是inline/部分Tagged等真实PDF需后续支持/复核，不能称全PDF兼容。
- Ruling: 原生未传doc_id，仅可信进程内绑定启用归档，缺ID拒绝 — 代价是生产原生身份及SQL恢复仍未接线，POC不证明原生权限。
- Final: Ruling: 复核暂不判定的同scope同file_id多doc_id，本阶段保留安全拒绝 — 防止错误复用或覆盖，不能未经用户允许扩展原生契约 — 代价是该重复内容多文档场景暂不支持，交M1-04D解决。可选归档/未挂载Host、完整Unicode/每库标定及M1验收也未由复核认定完成，按上述边界保留。

## 执行记录：M1 原生PDF薄接线（2026-10-31）

- 范围：用户明确批准三处原生生产文件及Host接收端。修改knowledge_process.go分派、mineru_converter.go可选请求级V1 client，新增parser_proxy.go与测试；原生解析结果消费、Markdown转换、分块/索引和调度算法未改。原有M1未提交改动保留；未提交/推送。BASELINE、Frozen及根go.mod/go.sum哈希保持原值。
- 传输：环境origin可指定宿主IP，客户端固定origin/path、禁redirect/环境proxy，所有上传/complete/job/poll/ZIP与后台取消都携带不可变tenant/KB/doc/version及Bearer；配置后任何错误不回退。Host单独配置后端凭据及显式scope，重复/缺失/伪造Header、错major/key/scope拒绝。每doc子scope自动绑定；同字节不同doc独立归档，原件冲突安全拒绝。
- RED→GREEN：原生薄客户端、Host接收端和配置先缺API编译失败，后通过；非PDF绕预检、源下载误报归档、尾随scope JSON先实测失败后修复；Markdown下划线转义核验先失败再增加完整句子规范化验证，未改原生文本。补401/503/不可达/跨origin、并发身份和脱离context取消测试。
- 一次独立复核：Critical0、Important1、Minor0。挂载parser继承Bridge5秒body与上游30秒bulk导致慢上传误拒绝，主任务确认。唯一修复轮次：真实分段5.2秒上传先HTTP400再204；独立metadata/bulk超时与取消回归先缺实现失败再通过；默认bulk/body1000秒，可用AGENTRAG_PARSER_BULK_TIMEOUT与核心WEKNORA_MINERU_TIMEOUT配套，M0仍5秒。修复后扩展十三包全绿；未派第二次复核。用户随后追加的真实模型脚本由主任务验证，未再独立评审。
- 原生Linux验证：Ubuntu Go1.26/GCC，CGO_ENABLED=1、GOWORK=off、sqlite_fts5、lite edition及protobuf兼容ldflags构建新二进制成功退出0，位置tmp/m1-native-build-83iqcjjt/WeKnora-lite（不覆盖用户原件）；最新完整docparser包测试退出0。Windows无CGO的旧原生问题未冒称解决；扩展Windows及Ubuntu均CGO_ENABLED=0 build退出0。
- 原生service完整包：两次退出1，当前DNS将example.com与api.e2b.app解析至受限198.18地址，引发URL/MCP/sandbox测试SSRF拒绝；错误摘要与命令见证据文档。未修改SSRF或上游测试，未将定向测试/构建的成功说成上游完整测试通过；未跑根全量/前端构建、生产负载或远端CI。
- 扩展：go test -mod=readonly ./... -count=1十三包退出0；build/vet退出0；gofmt -l无输出。Linux CGO=1 go test -mod=readonly -race ./parser ./host ./bridge -count=1退出0，未声称完整race。边界脚本306 imports退出0，GOMOD指向extensions、GOWORK空；Python25例Windows退出0/skip1，Ubuntu退出0/skip2（原M0生命周期因端口占用及Windows文件锁案例）。既存Outbox不稳定性未修，本批最后完整suite未复现。
- 原生上传实测：最终默认夹具模式退出0，实际核心/Host/Windows MinerU4.0.1；2原生分块中1条包含完整PDF正文，页数1、flash/txt、正式job1、三重身份一致、middle schema2.0，脚本私有DB清理确认。采用自有回环互操作中继，不修改WSL/防火墙/全局转发。最初端口占用、临时二进制缺失及Markdown核验失败均保留，不能反推前几次已通过。
- 真实模型：用户明确授权使用DeepSeek生成式问答和两个本地模型，并批准仅为8081/8082补--embeddings/--reranking重启。原服务能力接口先501；重启后实际向量2560维，重排返回正确排序。key仅进程环境/私有stdin和隔离DB、日志脱敏，不写仓库。--live-models通过原生API注册到新空间；最终上传单job，显式knowledge-search diagnostics.applied=true/outcome=ok；successful embedding4、rerank1、DeepSeek generation3，返回“两名审阅者”并引用该PDF。问答本身重排0次，独立检索API补验，不声称默认问答已自动重排。模型服务测试后保留运行。
- 失败证据：真实模式初次base_url缺/v1，测试中继拒绝/embeddings，导致整个原生任务重试及4次parse POST，随后归档冲突503；修正测试地址后正常流程单job。后两轮因默认问答未调用reranker被脚本判FAIL，最后用原生显式检索API分开验证后PASS。C-13记录该真实重试边界，不擅改原生调度/覆盖middle或宣称所有失败只解析一次。
- 完整脱敏终端输出、全部主要命令及退出状态汇总：docs/evidence/m1-native-integration-2026-10-31.md。Python受审计的MinerU外网尝试0、私有ONNX0，仅表示该文本夹具/进程审计；真实DeepSeek云端调用由测试授权并单独中继，不将此日志解读为全系统零外网。
- Ruling: WSL/Windows分离使用拥有的测试回环中继，核心生产只读配置origin、Host仍127.0.0.1 — 避免改全局网络或放宽监听 — 代价是本轮不证明用户的生产隧道可达。
- Final: Ruling: SQL/崩溃重启/128 scope释放属于04R/05后续 — 当前安全容量拒绝及未知结果拒绝保持 — 代价是有限试用批次及原生重试仍可能重复解析，不宣称可靠恢复。
- Final: Ruling: OCR/其他档位、Unicode质量和每库标定仍未交付 — 本批只验证真实文本PDF — 代价是不能宣布Frozen M1整体完成。
- Final: Ruling: Host信任已鉴权核心的身份Header，限定配置tenant/KB，不独立回查所有权 — 与授权薄补丁一致 — 代价是共享key持有者及所有权后续变更需单独治理。
- Final: Ruling: 已归档doc不同内容仍拒绝覆盖 — 不擅加修订状态机 — 代价是原doc重解析/修订需后续受控版本发布。
- Final: Ruling: 真实检索和生成通过只证明该合成语料的可用性 — 未以此宣称向量/FTS质量标定或全语料验收 — 代价是仍需独立生产语料与负载验证。
- Final: Ruling: 本轮不重审全部M0/预检器，也不重跑M0问答断网通网 — 保留既有验收与本轮有限回归区分 — 代价是完整M1联合门禁仍待完成。Deferred minors: none。

## 执行记录：M1持久恢复／SQL产物／标定工具（2026-10-31后续）

- 用户要求处理M1未完成项目，另确认“暂时没有”真实PDF/扫描件及人工期望标签。新增计划docs/superpowers/plans/2026-10-31-m1-recovery-calibration.md；所有原有工作保留，本轮未新增原生补丁、未提交/推送。
- 04R：物理验证hash+可信文档身份/有效策略/后端的正式任务去重；先FULL同步intent再POST，已知回执/权限持久恢复，未知结果不重发。130完整文档、12并发原生创建、两个独立SQLite写连接的12次claim唯一性、活跃job不驱逐，以及真实Host重启单job均通过。
- 05：003_parser.sql、ext_parser_artifacts原始BLOB及格式/schema/MinerU版本/正式任务与身份FK；v1/v2升级保留数据。文件发布后SQL登记，失败503，恢复核验原文件再补登记。子进程os.Exit绕过defer分别在intent、receipt和archive后退出，恢复测试通过；闭库拒绝发送、取消轮询后独立取消、旧归档无日志拒绝通过。
- 06：纯Go标定测量库/CLI及空manifest；实际读取PDF核hash，按库验证85/10/15与拒绝数，训练/验证文档族与重复内容泄漏拒绝。不是自动阈值拟合，不报告真实指标；空清单正确失败。
- RED→GREEN：恢复API/迁移v3与标定API先缺失失败后通过。一次独立复核Critical0/Important3/Minor0，单轮修复分别覆盖NORMAL路径改写、取消轮询毒化已知job/拒绝取消、旧归档缺快照被当新文档；各先复现失败后通过，随后完整suite。未二次复核。
- 最终命令：扩展build/test/vet均0，gofmt无输出，边界308 imports退出0；Python26例退出0/skip1；Ubuntu CGO=1完整go test -race ./... -count=1最后退出0。此前race两次失败均保留：新fixture初始化竞争已修；既有Outbox calls=3/reports=2未改，最后通过不等于解决其不稳定性。
- 07：真实WSL核心/生产Host/Windows MinerU4.0.1、全新隔离DB，--joint-m0最后退出0。原生上传→单job flash/txt→FS/SQL原字节/schema2.0/MinerU4.0.1/FK→分块通过；停Host核心关联拒绝/答案生成0，重启Host恢复同job且远端POST仍1；同凭证会话问答通网生成1且收到门卫。此轮模型是本地夹具，未冒称再次DeepSeek实测。
- 证据：docs/evidence/m1-recovery-calibration-2026-10-31.md，含完整最终E2E输出、命令及历次故障。根全量测试/前端/物理断电/真实标定未执行。只复用此前真实CGO核心二进制；本轮扩展Windows及Linux构建不需CGO。
- Ruling: 未知远端结果及旧归档无日志时拒绝，不推断job归属或自动补发 — 防止重复正式解析/覆盖原件 — 代价是需人工恢复，暂不承诺exactly-once。
- Ruling: 真实标定、图片/复杂预检及物理断电继续保持未验收，不以合成夹具替代 — 缺语料/资源及部署存储依据 — 代价是M1不能整体结项。Deferred minors: none。

## 执行记录：M1归档发布屏障与故障窗口（2026-10-31继续）

- 范围：用户要求继续M1；先推进05P无需真实语料的可实施部分。新增计划2026-10-31-m1-artifact-publication.md；仅修改extensions/parser/artifacts.go、新增两平台同步helper与两测试文件，更新文档。没有新增原生补丁、依赖或SQL迁移，既有改动保留，未提交/推送。
- 顺序：原有两个文件Sync后，非Windows额外Sync暂存目录，Rename整个目录，再Sync产物根。任一步错误均先于SQL登记/ZIP ACK返回；已有相同归档先核验字节，再Sync文档目录及根，补上上次失败屏障。Windows保留原行为，不能冒称等同Linux目录同步。
- RED→GREEN：新增屏障顺序/失败/重试测试先因缺少syncDirectory字段编译失败，补最小实现后通过。原生接口的stage/root注入失败返回503且SQL为0，修复后返回200且正式job始终1；测试未改变业务路由。
- 崩溃：直接退出子进程分别落在stage已同步、Rename后根未同步、根同步后SQL前、SQL提交后ACK前；检查前三窗口SQL0、最后窗口SQL1，恢复后同job/SQL1/文件原字节/FK，跨doc403，旧.pending数据保留。既有未知结果/旧归档无日志拒绝规则保持。
- 验证：Windows扩展build/test/vet退出0，gofmt无输出，边界308imports与独立go list检查0；Python26例0/skip1。Linux完整race命令本次退出1，仅既有Outbox calls=3 reports=2断言；parser等其他包通过，无新data-race报告，未改Outbox或重跑到绿掩盖失败。
- 存储实测：WSL /tmp实测为tmpfs，不能作为磁盘证据；额外将TMPDIR定在仓库tmp下自有隔离目录（/mnt/d实测v9fs），同样同步与退出窗口测试退出0，清理仅限该自有目录。v9fs调用成功不是底层设备断电证明。
- 真实联合E2E退出0：复用此前未变的WSL CGO核心，新构建Host及Windows MinerU4.0.1，单job flash/txt、SQL原字节/版本/FK、分块、Host重启恢复、M0断网生成0/通网生成1均通过。使用本地模型夹具，本轮没有再调用DeepSeek。
- 一次独立审查Critical0/Important0/Minor0，复核者定向测试及diff-check退出0；无修复轮次、无二次审查。完整命令和E2E终端输出见docs/evidence/m1-artifact-publication-2026-10-31.md。根全量/前端/物理断电未跑，M1整体未结项。
- Ruling: Windows保持原有文件Sync/rename，物理断电和上层目录创建继续待验 — 不以不可移植目录句柄刷新破坏现有行为或伪造保证 — 代价是Windows目录元数据/设备掉电持久性未承诺，M1-05P不能标成全部完成。Deferred minors: none。

## 新任务占位（待填充）

- ID／状态：待填充
- 期望结果：待填充（描述可观察的交付及验收条件）
- 依据：待填充（用户请求、架构章节或具体源码／问题证据）
- 何时重新审视：待填充（时间、前置里程碑或可观察触发条件）
- 验收证据／完成日期：实施后填写

## M2 内核任务（2026-10-31）

2026-11-01当前增量（不覆盖下方历史批次结论）：

| 任务／状态 | 期望结果与涉及路径 | 依据与验证 | 何时重新审视 |
|---|---|---|---|
| M2-SOURCE-GATE／已实现并验收指定链路 | RAG摘要、最终上下文、Agent search/read/list/schema/analysis及MCP文档读取服从active；`internal/extensionbridge/documents.go`和对应薄Hook。 | 用户漏洞修复授权；六包定向/race、真实上传→Observer active→日期扫描stale→同会话/已知ID→断Host E2E退出0；模型所有输入和重排输入均检查。 | 新工具、DTO、模型上下文或上游版本变化时。 |
| M2-UNKNOWN-SOURCE／M2安全拒绝已实施 | 历史/压缩/恢复、Wiki/图谱、页面快照、旧答案亲和标题、SQL/会话检索、通用操作和不透明外部MCP结果暂拒绝；保留存储。 | 用户明确批准；最早读取入口的负向测试、RAG/Agent同会话旧答案标记及独立复审。 | 有完整逐文档来源链、许可重新验证和撤销测试后；禁止仅移除拒绝恢复功能。 |
| M2-COPY-LINEAGE／扩展账本已完成，原生未接线 | 用户批准的同步来源账本及离线事务/许可决策位于`extensions/provenance/`、schema6；Host契约、可信来源适配及文件/记忆/下载门卫另行确认。 | C-27同步事实适配已批准，COPY-01/02A验证通过；并发、故障、进程退出及完整扩展/race见`docs/evidence/copy-provenance-ledger-2026-11-01.md`。尚不能声明真实副本撤销，C-28～32仍限制运行边界。 | 原生接线审批、M3版本权威性、来源封存或下载范围变化时。 |
| M2-STREAM-REVOKE／设计已形成，未实现 | 生成期间停用的取消/租约或许可版本策略；见`docs/design/M2-Stream-Revoke-2026-11-01.md`，待明确SLA/策略及完整来源集。 | 当前仅入口/最终RAG拼接许可；原生取消可能flush缓冲，不能直接声称持续撤销；Frozen guidance fail-open保持独立。 | 定义即时撤销SLA或生产发布前。 |
| M2-BASELINE-CHECKS／本轮相关8包已通过 | 原49项环境/夹具失败已清除；追加4项换行/DNS/MCP治理期望兼容，保留业务/协议/RBAC断言。 | 最终完整8包命令退出0；WSL补齐venv，精确测试fixture不关闭生产保护；见`docs/evidence/m3-trusted-sources-2026-11-01.md`。 | OS/Python/DNS/原生协议变化或上游全仓发布验收前；不等于全仓Go测试通过。 |
| M2-FRONTEND-GATES／已完成（2026-11-01） | 清除8项前端测试环境基线失败；六个原测试与 `frontend/src/test-utils/posixShell{,.test}.ts`，业务断言保留。 | 完整2369项中2368通过/0失败/1既有skip；type-check/build退出0；三种损坏行为仍被原测试拒绝；独立34项复核通过，见 `docs/evidence/m2-frontend-gates-2026-11-01.md`。 | 操作系统、Git安装、换行策略、构建工具或对应业务行为变化时；原生后端全量门禁仍独立待排查。 |
| M2-LOCAL-DEPLOY／本机验收完成 | 已备份并安装候选Core，启动Core/worker Host；Windows访问端口18880，Bridge仍为WSL回环8090。 | C-26：Windows保留7991–8090，获准重启WSL后仍保留；调整Core端口与Host Core URL后，Windows UI/健康/原账号登录/知识库API均200。 | Windows保留范围、WSL网络、部署配置或重新编译时；不要硬删系统端口保留。 |

证据、冲突点及准确命令：`docs/design/M2-Governance-Leak-Closure-2026-11-01.md`。本增量不将M2整体、UI、生产云模型或MinerU联合验收标记完成。

| ID／状态 | 期望结果＋文件 | 依据／验证方法 | 何时重新审视 |
|---|---|---|---|
| M2-CORE／已交付（组件范围） | `extensions/lifecycle/{machine,rules,stale}.go`及测试、`store.go`同事务guard、`store/migrations/004_lifecycle_policy.sql`；标注日期驱动、每库规则、并发fence、不可变决策回执。 | 用户默认矩阵；五处SQL故障全回滚、真实子进程中途/提交后退出、24迁移及24标注并发各单赢家、ABA/跨scope/FK/幂等；详见M2设计及证据。 | 自动接线、有效期或分类体系变化时。 |
| M2-INGEST／已接线（公共API对账观察） | `extensions/ingest/lifecycle.go`适配持久Observer，Host worker启用；completed后pending→active/scheduled/stale，重复观察恢复未完成决策。 | 事务内重查最新完成观察；旧completed不能盖过failed，重复不增事件，业务决策故障后同观察可恢复；真实Host/SQLite跨进程通过，未伪造原生中间历史。 | 原生公开列表字段、事件身份或解析重试行为变化时；直接原生事件Hook仍后续。 |
| M2-TIMER／已接线（24h扫描） | `extensions/lifecycle/automation.go`及Host启动补扫/24h循环；pending/scheduled/active过期直接stale，scheduled到期可active。 | CAS/幂等事实保证重扫，scope限制；scheduled激活同事务要求最新completed，failed/processing/缺失拒绝；到期边界、启动catch-up和正常取消已测。 | 需要精确到秒/指定时区日历cron、扫描容量或解析来源变化时；默认最多24h迁移延迟。 |
| M2-REVIEW／已完成（本地告警范围，2026-10-31） | `extensions/review/`、schema5、Host显式分流/24h扫描、`cmd/review-alerts`只读CLI；review_due只告警不改状态。 | 重复/257页扫描、24并发检查与投递、日期往返、故障回滚、dead/人工重试、三类真实退出恢复、Host无标签调用；全扩展checks/Linux race通过，独立复核无问题。 | 外部渠道、UI/鉴权、扫描频率或通知负载变化时；本地日志至少一次，不承诺外部exactly-once。 |
| M2-CONFIG／已交付（2026-11-01） | `extensions/management/`、Host管理认证、原生JWT薄转发及配置UI；日期快照保留标签，规则正文/本库版本同事务读取。 | 真实8并发写单赢家；scope/凭证/只读/跨库拒绝；不支持的生效日期改动同CAS事务拒绝；默认规则不追溯。 | 状态矩阵、来源权限、批量编辑或管理API面向机器调用时。 |
| M2-UI-DATE／Baseline Limitation | 保留冻结矩阵：拒绝active未来effective_from，拒绝scheduled清空effective_from；不改变revision、状态，不复活失效态。 | 审查发现Timer缺少active→scheduled；新增扩展同写事务守卫及回归，防止保存成功却仍放行。 | 需要重新排期或重新生效时，先明确迁移矩阵和来源完整性规则。 |
| M2-SUPPLEMENT／T3已交付并验收（2026-11-01） | 同库显式父关系、schema9、版本绑定、Critical Edge/声明/audit原子提交、父失效/复核联动、原生JWT管理薄接口和详情UI已接入；见`docs/evidence/m2-supplements-2026-11-01.md`。 | 16并发1赢家、6 SQL故障点与进程崩溃/幂等、scope/CAS/环/共享祖先深度/并发改边拒绝；真实上传/RAG/Agent正反E2E、全扩展/race/8原生包/前端验证通过。真实组件的合成接口浏览器点击通过，完整原生App/真实用户库浏览器验收及生产替换未执行；M2仍待T4/T5。 | 完整run封存/副本门卫/持续撤销接入，或跨库、来源和业务矩阵变化时。 |

M2边界：本批没有新增原生补丁，没有接入active检索过滤，也没有完成Observer/定时/告警全链路。M1未验收项目保留；物理断电、生产负载、上游/前端全量检查和远端CI本批未验证。

## M2原生消费与策略接管（2026-10-31）

本轮用户明确授权原生薄补丁，补全过期规则并要求DB真实策略。M0默认透传记录保留为历史；当前Host probe对业务返回503，worker以FULL持久DB和显式tenant/KB配置接管。无生命周期事实也排除，部署需先对账；不回退透传或whole-KB。

新增extensions/lifecycle/automation.go、ingest/lifecycle.go、retrieval/policy.go及Host接线；原生新增internal/extensionbridge/response.go，client返回Decision，search_parallel在原合并结果处执行允许集/排除集过滤。RRF、检索算法、Agent、分块及原生DB未重构。

验收细分：Observer/每日扫描/DB策略/普通RAG消费已通过组件及真实Host/SQLite→原生PluginSearchParallel跨进程验证，底层源列表和检索结果为夹具。Lite新二进制CGO编译成功。未运行生产数据库迁移、真实上传/LLM问答、前端或全部Agent入口验收；M1缺口及M2配置UI/复核告警/补充通知权威血缘仍保留。

独立复核发现Important1：scheduled在failed/processing/缺失最新观察时会被Timer激活。三例先RED再修复，事务内要求completed；修复后全suite/race补证，不派二次复核。普通文档命中还必须位于返回允许集，避免原生目标范围较宽时混入未判定文档；WebSearch类型保留，排除优先。

后续待办：适配历史M0/M1完整上传/问答E2E的worker凭证与DB配置；当前新增extensions/scripts/m2-policy-e2e.sh专门验证本批跨模块策略链路。Agent直接读/旧handle/独立图谱/生成中停用、C-10标签null拒绝限制、whole-KB授权文档枚举不因本批接线消失。
