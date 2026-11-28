# AgentRAG 当前状态

本文件由 extensions/scripts/status_doc.py 生成，请勿手工编辑。
任务与验收事实仍以 TASK_BACKLOG.md 和 docs/evidence/ 为准；本文件只汇总当前条目状态。
生成来源：TASK_BACKLOG.md（共 120 个去重条目）。校验命令：python extensions/scripts/status_doc.py --check。

## 文档族语义整改（2026-11-09）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| DOC-WORKBENCH-RETENTION | 已实施（2026-11-18） | schema 35 与 extensions/retention/ 实现 provenance/快照证据 30 天滚动保留；清理铃铛扩展到共享空间成员。 | 2026-11-18 | [document-list-cleanup-2026-11-18.md](docs/evidence/document-list-cleanup-2026-11-18.md) |
| DOC-WORKBENCH | 已实施，隔离联合验收通过 | /platform/documents 收敛六个旧导航；单文档详情接入日期、版本族、关系、图、审计、建议及修订；schema 28 区分提交摘录、意见和审核定稿。 |  |  |
| DOC-WORKBENCH-FOLLOWUP | 已实施并完成本机正式切换（2026-11-11） | extensions/business/{feedback,family_workspace}.go 和单文档抽屉补齐双向补充关系、建议处理审计及拆分发布批次。REL-04 升级检查按仓库迁移序列核对。 | 2026-11-11 |  |
| DOC-WORKBENCH-AUDIT | 已实施，隔离与生产 E2E 通过（2026-11-14） | 按 docs/evidence/document-workbench-audit-2026-11-13.md 完成：overview/list 统一 verified current 与降级标记；family workspace 跨库关系/补充 incoming/outgoing；review inbox 增加 li… | 2026-11-14 | [document-workbench-audit-2026-11-13.md](docs/evidence/document-workbench-audit-2026-11-13.md)<br>[workbench-ui-final-2026-11-17.md](docs/evidence/workbench-ui-final-2026-11-17.md) |
| DOC-WORKBENCH-AUTHORITY | 已实施（2026-11-15） | 工作台内嵌版本族/业务视图直接复用 LifecycleView 已读取的 can_write，scope 内嵌模式不再为权限额外请求 overview；未提供权限时保留原 overview 回退。文件：frontend/src/views/lifecycle/LifecycleView.vue、frontend/sr… | 2026-11-15 |  |
| DOC-WORKBENCH-HEAD-RECOVERY | 已实施（2026-11-15） | 文档族工作台在发布头缺失时显示告警，并在当前成员页提供“选择可用成员 + 恢复原因 + 二次确认”的恢复表单；调用既有 family_head_recover，保留历史记录和审计。文件：frontend/src/views/families/FamilyWorkspaceView.vue、frontend/src/a… | 2026-11-15 |  |
| ATTACH-ACCESS | 已实施并完成本机正式切换（2026-11-11） | 单文件下载、预览、批量下载和 /files 复用同一附件许可。 | 2026-11-11 |  |
| FAM-P1 | 已实施，隔离验证通过 | extensions/retrieval/ 在许可时读取有效期和原生状态；extensions/business/ 普通修订先绑定禁用草稿，索引完成后以同一治理事务释放目标、使旧件失效；extensions/mcp/ 按原生范围返回补充通知与核验过的附件。 |  |  |
| FAM-01 | 已实施，隔离验收通过 | 扩展 schema 23 和 extensions/business/family.go 提供同库人工建族、成员加入／移出及查询，独立返回业务族和观察链 ID。见 架构决策。 |  |  |
| FAM-02 | 已实施，隔离验收通过 | 扩展 schema 24 和 extensions/business/family_replacement.go 实现同族替代的明确确认、预约、版本锁定和原子切换。 |  |  |
| FAM-02-HISTORY | 已上线并完成正式补录（2026-11-11） | 在版本族工作台按生效日查看更替，阻止过期草案直接批准，连续到期草案可从当前发布件整链预览和确认。 | 2026-11-11 |  |
| FAM-03 | 正式库只读盘点完成（2026-11-11） | extensions/business/family_backfill.go 提供只读盘点、显式确认、历史发布头和映射。分库切换仅处理当前成员及曾明确纳入投影的文档。 | 2026-11-11 |  |
| FAM-04 | 已实施，隔离联合验收通过 | 工作台与验收记录。工作台聚合成员、关系、生命周期和审计。扩展 schema 27 支持同库扫描、证据、分库评分和建议确认／忽略。原生 JWT 开放五项只读动作。 |  |  |
| FAM-REVIEW | 部分已实施 | family.go、family_replacement.go、taggov/family.go 与 lineage/graph.go 修复成员历史、附件端点、版本证据、标签保护、发布头绕过与补充撤销。 | 2026-11-15 | [fam-review-production-recon-2026-11-15.md](docs/evidence/fam-review-production-recon-2026-11-15.md) |

## 一键启动（2026-11-05）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| ONEKEY-01 | 已接线并实测（主链，2026-11-07 复验） | Windows start.cmd＋scripts/onekey.ps1，WSL start-all.sh（status/doctor/logs/token/stop 子命令），launcher_config.py 的 plan/token/mcp-key 与 STATE_ONLY 状态键；WSL 网络自愈（nat→… | 2026-11-07 |  |
| ONEKEY-02 | 部分完成（docreader 已验证） | docreader 自动拉起与配置已实测（host override 发布 50051、AGENTRAG_DOCREADER_IMAGE 固定镜像、DOCREADER_ADDR 按可达性写入）；launcher_config.py mcp-key 交互签发 + register_related_mcp.py 刷新目录… |  |  |

## SQLite 存储性能与 Management Overview 可用性整改（2026-11-07）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| SQLITE-P0 | 已完成（2026-11-07 实机复验） | 管理页 overview 在 native ATTACH 遇到 SQLITE_BUSY/SQLITE_LOCKED/SQLITE_CANTOPEN 时降级为扩展库查询 + titles() 批量标题，响应标记 degraded=true、degraded_reason=native_database_unavaila… | 2026-11-07 |  |
| SQLITE-P1 | 已完成（2026-11-07 WSL ext4 迁移） | 备份 data/weknora.db 与 data/agentrag_ext.db，停止 Core/Host 写入后迁移到 WSL ext4 /home/ybochuan/.local/share/agentrag/，修改 data/agentrag-launcher.json 的 DB_PATH/AGENTRAG_… | 2026-11-07 |  |
| SQLITE-P2 | P2-A A4 完成、P2-B 评估完成、P2-C 已提交（0ab3bbd）、P2-D 轮询优化已提交（5dd40b9）、P2-E 性能监控审计完成（2026-11-07）、P2-E-01 离线基线完成（2026-11-08）、P2-E-02 最小埋点完成（2026-11-08） | 评估 Core SetMaxOpenConns(1)/WAL/读写连接池；前端 documentTitle 批量查询、合并重复 api.list、减少 5 秒轮询并将 Overview/业务数据独立加载；P2-A 已完成 A4-01～A4-04：Core readiness 启动门控、错误分类脱敏、5→10→20→4… | 2026-11-07 |  |
| RAG-PERF-01 | 只读拆解完成（2026-11-08）；P2-E P1 RAG 阶段指标完成（2026-11-08）；RAG-ISOLATION-01／已完成（2026-11-08） | 解释 P2-B 混合负载 knowledge_search P50 11.8s：按日志将 knowledge_search 拆成 chunk_search/chunk_rerank/chunk_merge/filter_top_k，区分冷启动/并发排队与远程 rerank。证据：docs/evidence/RAG-m… | 2026-11-08 |  |
| SQLITE-ACCEPT | 已完成（2026-11-07 先于 P2 的联合回归） | 完成 P0/P1 后做一次完整联合回归；验收基线：知识库列表 <500ms、文档列表 <1s、Overview 200、Rules/Reviews/Business 列表正常、后台对账稳定。证据：docs/evidence/SQLITE-ACCEPT-2026-11-07.md。 | 2026-11-07 |  |

## 附件阶段二／三（2026-11-03）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| ATTACH-04 | 已接线并验证 | 主命中关联附件、直接命中自身链接；weknora/attachment_read.go、retrieval/attachments.go、Bridge after-retrieve、原生RAG/Agent薄DTO |  |  |
| ATTACH-05 | 已接线并验证 | 父active前置条件、附件点击时许可；retrieval/policy.go及原生下载薄门卫 |  |  |
| ATTACH-06 | 已交付（聊天配套） | TDesign卡片、RAG/Agent去重、登录下载和Markdown链接点击；RelatedAttachments.vue及相关工具/薄渲染 |  |  |
| ATTACH-07A | 已交付并生产验证（2026-11-08） | DOCX/Office附件真实标题识别、正文附件引用映射、ext_attachment_semantics v14、Bridge DTO可选语义字段、RelatedAttachments.vue语义卡片与排序；extensions/parser/attachment/semantic.go、importer.go、e… | 2026-11-08 |  |
| ATTACH-08A | 自动归属已接线并隔离验收 | schema15、上传意图及回执恢复、附件→主件的系统关系、文档关联列表、同库图投影及读取核对。复用 parser/attachment、store/attachment_links.go、business/feedback.go、lineage/graph.go、retrieval 与现有前端入口。另提供 atta… |  |  |
| ATTACH-08B | 删除联动已接线并隔离验收 | executeKnowledgeDelete 薄接线、schema16、上传围栏、原生软删除证明、子件清理续跑与文档关联状态。实现位于 extensions/attachmentcleanup/、store/migrations/016_attachment_cleanup.sql、bridge/attachment… |  |  |

## 文档附件阶段一（2026-11-03）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| ATTACH-01 | 已实现并验证 | Registry、PDF/三类Office内嵌文件、正文DOCX附录；extensions/parser/attachment/ |  |  |
| ATTACH-02 | 已实现并验证 | 官方multipart上传与强制父标记；extensions/weknora/attachment{,_test}.go和Importer |  |  |
| ATTACH-03 | 已接线并隔离验收 | PDF预检前提取、Host认证端点、Office原生薄包装及定向测试 |  |  |
| ATTACH-LATER | 阶段外 | 复杂意图路由、OLE容器及跨父附件复用；父状态读取联动已转ATTACH-05 |  |  |

## 本机启动闭环（2026-10-31）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| M2-OPS | 本地闭环已验证 | 独立扩展库默认路径、严格手工DDL认领、可诊断初始化错误；一键Host本地Ready→Core、官方机器凭证初始化、Ctrl+C清理；完整启动指引。 |  |  |

## 生产环境整改项目（2026-11-16 起）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| REM-BASE | 生产验证基线已建立（2026-11-16） | 真实生产栈在线并可验证：Core 18880、Host 8090、docreader 50051、MinerU 8000、模型 supervisor 8092、embedding 8081、rerank 8082；Core JWT 可由原生登录取得；四个业务接口（feedback/guidance/revision/… | 2026-11-16 |  |
| REM-WORKBENCH | 文档工作台顶层入口已补齐（2026-11-16） | 工作台作为统一治理入口，5 个工作区视图均可从顶层 tab 到达，URL 契约不变。 | 2026-11-16 |  |
| REM-AGENTIC | 评估工具与问题集就绪，执行待云端账户恢复 | 每库 15–20 道多跳问题的 Agentic 评估可执行：工具 extensions/cmd/agentic-eval、15 题清单、轮次与工具序列口径正确。 | 2026-11-16 | [m4-agentic-eval-2026-11-16.md](docs/evidence/m4-agentic-eval-2026-11-16.md) |
| REM-MCP-REG | 原生 MCP 按需发现注册已完成（2026-11-16 生产环境） | 用原生 MCP 按需发现注册本机 Extension MCP Server，Streamable HTTP /mcp，不改 ToolRegistry。 | 2026-11-16 | [m0-mcp-registration-2026-11-16.md](docs/evidence/m0-mcp-registration-2026-11-16.md) |
| REM-ROUNDLIMIT | 已完成（2026-11-16，上限改为 4） | 冻结架构要求 Agent 轮次上限 3–4；生产 builtin-smart-reasoning 原为 50。 | 2026-11-16 | [rem-roundlimit-message-centre-2026-11-16.md](docs/evidence/rem-roundlimit-message-centre-2026-11-16.md) |
| REM-T4-PRODUCER | 写入侧 producer 接线已完成，完整闭环待真实输入证据（路线第 1 项） | 真实写→归档→跨轮读闭环；覆盖不缩源；ACK 未知不重放；重启撤销。 | 2026-11-16 | [t4-04-producer-wiring-2026-11-16.md](docs/evidence/t4-04-producer-wiring-2026-11-16.md) |
| REM-M2-NOTIFY | 已改为原生铃铛（2026-11-17） | M2 外部通知通道与生产配置入口接线。 | 2026-11-17 | [rem-roundlimit-message-centre-2026-11-16.md](docs/evidence/rem-roundlimit-message-centre-2026-11-16.md) |
| REM-RETRIEVAL-BASELINE | 库级检索基线已实测达标，调参与量化受约束待决策（路线第 4 项） | 每库 Recall@5/10/20 与 MRR 实测；RRF 参数实验；重排降级与查询扩展门控。 | 2026-11-16 | [m1-retrieval-baseline-2026-11-16.md](docs/evidence/m1-retrieval-baseline-2026-11-16.md) |

## M4/M5 业务冲刺（2026-11-02）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| BIZ-01 | 后台主链已接线 | 原生JWT管理→同库关联；反馈→指导→提案→真实manual ingest/index→supersedes和旧文档停用。路径extensions/business/*、management/business.go、host/runtime.go、schema11；原生仅handler/management DTO/… |  |  |
| BIZ-UI | 已交付（2026-11-03） | 原生导航的文档关联、反馈与指导、修订发布三页；持久候选列表/确认/撤销，反馈与种子录入、审批/冷静期/二次确认，提案创建/审批/发布恢复；KB切换、只读与20项分页。路径frontend/src/{api/business.ts,views/business/*,router/index.ts,stores/menu… | 2026-11-03 |  |
| M4-MCP | 生产真实模型 discover→call 与停用已验证（2026-11-16） | 经原生按需发现调用可信Host的query_related_docs，返回active补充通知、同库/跨库确认关联及附件，保留scope/active/版本门禁；不放开不透明外部MCP。实现位于extensions/mcp/server.go及既有原生MCP薄点。 | 2026-11-16 | [m4-mcp-live-2026-11-14.md](docs/evidence/m4-mcp-live-2026-11-14.md)<br>[m4-mcp-production-2026-11-16.md](docs/evidence/m4-mcp-production-2026-11-16.md) |
| M4-CAL | 生产元数据校准已通过，独立人工复核与Agentic待补，GraphRAG初步门禁保持关闭 | 20–50组每库关联对标定权重，真实precision≥0.70/recall≥0.60；Agentic问题集与GraphRAG预算门禁。 | 2026-11-16 | [m4-agentic-eval-2026-11-16.md](docs/evidence/m4-agentic-eval-2026-11-16.md)<br>[m4-calibration-production-2026-11-16.md](docs/evidence/m4-calibration-production-2026-11-16.md)<br>[m4-graphrag-budget-gate-2026-11-16.md](docs/evidence/m4-graphrag-budget-gate-2026-11-16.md) |
| M5-QA | 快速问答入口、Agent 文本重排反馈及单库指导注入已接线（2026-11-05） | feedback_capture 将本人已封存答复的实际检索分块绑定到反馈。before_generate 从认证run选择同库 active 指导，临时追加到 RAG/Agent 请求，有model step时登记指导指纹。重试、会话预算和指纹持久化到 schema13。 | 2026-11-05 |  |
| M5-PROJECTION | 已接线并隔离验收（2026-11-05） | extensions/taggov/family.go 由既有单写者消费 family_tag，按已发布的修订事实向新旧文档投影同一个库内 gov:family: 标签；extensions/cmd/outbox-retry/ 可按租户、库与任务 ID 重排死信。 | 2026-11-05 |  |
| M5-BINARY | 后端、工作台与生产环境链路已验证，故障覆盖已补（2026-11-16） | revision_create_binary 建立 binary_source 建议，revision_bind_binary 绑定用户已上传的同库 native 文档并记录观察版本，revision_approve 后 Publish 直接校验目标当前版本并原子 finalize；不自动把 PDF/DOCX 换成… | 2026-11-16 | [m5-binary-production-2026-11-16.md](docs/evidence/m5-binary-production-2026-11-16.md) |
| M5-ACCEPT | 真实时钟激活与质量回归已完成（2026-11-17） | 自动激活扫描、快速通道10–20%抽检/主题恢复、完整SQL/进程崩溃/负载矩阵与每库生成质量回归。 | 2026-11-17 | [m3-m5-acceptance-closeout-2026-11-17.md](docs/evidence/m3-m5-acceptance-closeout-2026-11-17.md)<br>[m5-acceptance-2026-11-16.md](docs/evidence/m5-acceptance-2026-11-16.md) |

## T4 实施与验收门禁（2026-11-02）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| T4-00 | 依赖许可修复已验证 | 副本读/登记展开最新T3父依赖，最终事务重查；权限依赖不混入实际模型来源。文件：extensions/provenance/{permission,policy,manifest}.go、lineage/dependency_ledger_test.go。 |  |  |
| T4-01/02 | 底座已实现，故障矩阵已补全（2026-11-16） | 完整root绑定的组件视图、schema10运行/步骤/核验意向/producer与回执审计；未知/未完成/空源不能封存。路径：internal/handler/extension_source_*、extensions/{lineage/components,provenance/*}.go、迁移010。 | 2026-11-16 | [t4-01-02-failure-matrix-2026-11-16.md](docs/evidence/t4-01-02-failure-matrix-2026-11-16.md) |
| T4-03 | 原生文本主链接入，rerank 已适配，其余七项需派生输入链（2026-11-16） | QA以原授权scope/owner/native消息身份建run；completion/stream、think/act登记实际指纹及独立步骤，read_document、Agent search正文与受支持的重排文本可核验；终态收束，取消或证明失败不能封存。 | 2026-11-16 | [t4-03-input-composition-2026-11-16.md](docs/evidence/t4-03-input-composition-2026-11-16.md) |
| T4-04 | **Blocked by Security/Infrastructure**（2026-11-16 用户决定） | 实际写入、原run/step/path/hash、旧源继承、收集与资源别名闭环，已封存active副本可读，撤销后拒绝。 | 2026-11-16 | [p1b-producer-status-2026-11-16.md](docs/evidence/p1b-producer-status-2026-11-16.md) |
| T4-05 | 拒绝代码已接入，联合捕获待验 | shell启动前拒绝；未知memory/skill/目录/网页/后台正文不进模型；未知外部MCP服务摘要/schema/pin在模型读取前省略；保留任务控制、用户附件，图片先于VLM拒绝。 |  |  |
| T4-06/M2 | 待整体验收 | 真上传→Observer→文本来源→模型→producer→文件→撤销/重启，内置模型正向冒烟、故障捕获、全检查与最终整批复核。 |  |  |
| TEST-MODEL-PORT | 已知测试隔离问题 | 并行端口观测不误报临时端口，不弱化断言。文件：extensions/modelsupervisor/probe_test.go及观测实现。 |  |  |
| TEST-NATIVE-RACE | 既有工作区失败待核实 | 消除认证、沙箱和技能测试共享fake数据的竞态。文件：internal/application/service/tenant_{api_key,sandbox_config,skill_install,skill_served}_test.go及对应实现。 |  |  |

## 待办

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| M0-01 | 进行中 | Change Map和冲突报告已形成；用户已确认C-01至C-04的M0取舍。核心物理补丁清单在实际接入前再核实，不能按协议类别数代替调用点数。 |  |  |
| M0-02 | 已完成（本次HTTP门卫+默认透传范围） | 原生500ms门卫与Host默认Pass-Through接通，断网阻断、通网完整RAG E2E均通过。保留原生检索/过滤；复杂策略、响应收窄及其他Hook转后续，不声明已实施。 |  |  |
| M0-03 | 进行中 | 对账调度、SQLite事实/Outbox、HTTP Source和持久Observer已接入显式worker Host；临时DB+本地原生格式HTTP验证周期恢复与范围隔离。真实服务及业务接收端仍待验。M0接受来源内存队列并以状态对账兜底。 |  |  |
| M0-04 | 进行中，C-10接受为Baseline Limitation | 标签队列/API Store/KB映射/Outbox Handler已接入Host，多KB共用唯一Writer已测；null标签安全拒绝获用户接受，无标签首写留后续只读API，真实服务仍待验。只声明扩展内部串行，不保证原生UI并发安全。 |  |  |
| M0-05 | 已完成（2026-11-16 生产环境） | 使用原生 MCP 按需发现注册本机 Extension MCP Server，采用 Streamable HTTP /mcp 与本机认证；不改 ToolRegistry。 | 2026-11-16 | [m0-mcp-registration-2026-11-16.md](docs/evidence/m0-mcp-registration-2026-11-16.md) |
| M0-06 | 进行中 | 独立门禁、专用CI定义、SQLite/FK/事务/Outbox及Host本地联合测试已通过；真实QA全链路、真实来源对账和上游混合并发待验。并发2有上游竞态则记录后降至1重跑。 |  |  |
| M1 | 进行中 | PDF预检、原生身份薄接线/Host、真实4.0.1单job与文件归档→原生分块闭环已验证；SQL产物/恢复、真实标定和完整M1验收仍待做。 |  |  |
| M1.5 | 基线已实测达标；调参与量化按用户决定暂缓（2026-11-16） | 完成库级检索基线、RRF 参数实验、重排降级与查询扩展门控；量化只通过 feature flag 验证，保留原生 RRF 实现。 | 2026-11-16 | [m1-retrieval-baseline-2026-11-16.md](docs/evidence/m1-retrieval-baseline-2026-11-16.md) |
| M2 | 进行中（内核/Observer/扫描/普通RAG策略/站内消息中心已接线） | 已实现默认矩阵、DB规则/标注、原子决策回执和两类stale；Observer/每日扫描、普通RAG策略及本地复核告警已接线；站内消息中心（schema 33 已读状态）已交付，外部邮件通道按用户决定不做。 |  |  |
| M3 | Critical Edge 账本一致、run 来源封存生产验证通过（2026-11-17） | 已完成原生只读快照、schema8观察版本/family、真实源权威、分边查询及环防护；Critical Edge 关键边与业务事实同事务（生产 interprets 2、supersedes 10、54 族/55 版本、0 FK 违规）；生产存在真实 sealed run（d25ddecb…，2 来源、3 mode… | 2026-11-17 | [m3-m5-acceptance-closeout-2026-11-17.md](docs/evidence/m3-m5-acceptance-closeout-2026-11-17.md)<br>[t4-01-02-failure-matrix-2026-11-16.md](docs/evidence/t4-01-02-failure-matrix-2026-11-16.md) |
| M4 | 进行中：后台关联已接线，跨库关系与补充首刀完成 | 同库amends/interprets声明、related_to候选/确认/撤销、双向active/当前版本过滤并联合T3 supplements；schema 30 新增跨库规范性关系（amends/interprets、经二次确认的related_to），跨库关键边独立表，管理API和前端支持目标知识库。关系声明… |  |  |
| M5 | 进行中：后台反馈与manual发布已接线 | 管理反馈→审批/冷静期→指导→修订提案→原生Markdown创建/索引→supersedes/旧文档停用；Saga先对账、治理+audit同事务。真实问答事件、guidance预算注入、抽样复核、binary正式文件、family投影和UI待做。 |  |  |
| M6 | 可视化提前进行中 | 单库血缘图首版已接通，opaque mDNS、认证能力查询、静态节点回退及其余运维／评估 UI 继续按规划推进。 |  |  |

## 细分实施任务：文件、验证与依赖

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| M0-01A | 已完成（审计交付） | 源码审计与冲突报告：docs/design/M0-Change-Map-2026-10-29.md；完整路线见本表。 |  |  |
| M0-02A | 已完成（协议基础） | 协议版本、存活／就绪、无服务拒绝：extensions/bridge/protocol.go、server.go、server_test.go。 |  |  |
| M0-02B | 已接线（本批普通RAG消费范围） | extensions/retrieval/policy.go查生命周期DB；原生extensionbridge/{client,response}.go校验策略并由search_parallel.go过滤结果。 |  |  |
| M0-02C | 已完成（门卫+Pass-Through E2E） | extensions/bridge/retrieve.go新增默认Pass-Through构造器，extensions/host/runtime.go接线；更新bridge/host测试与脚本验收范围说明。原生核心保持原补丁，不改检索/过滤。 |  |  |
| M0-02D | 搜索排除已接线并验收（2026-11-17） | filterBridgeResults 对块/实体合并结果应用 before_retrieve 允许集与排除集，显式排除优先。 | 2026-11-17 | [post-m0-gov-closeout-2026-11-17.md](docs/evidence/post-m0-gov-closeout-2026-11-17.md) |
| M0-02E | 用户决定不做（2026-11-17） | 检索后元数据增强无实现；用户决定不引入原生 RAG 行为变更。 | 2026-11-17 |  |
| M0-02F | M5单库接线已验证 | extensions/guidance/inject.go、provenance/guidance.go 及迁移013实现持久预算与实际模型步骤绑定。受控区为 internal/extensionbridge/generate.go、chat_pipeline/chat_completion{,_stream}.go… |  |  |
| M0-03A | 已完成（Host 接线与本地验证在册；真实来源崩溃收敛属边界未验） | extensions/ingest/reconcile.go、http_source.go、observer.go及测试，extensions/weknora/client.go、schema002；Host接线见03E。 |  |  |
| M0-03B | 已完成（持久化组件） | SQLite连接与单写者：extensions/store/sqlite.go、write_executor.go、migrate.go、sqlite_test.go、write_executor_test.go；独立go.mod/go.sum锁定纯Go驱动。 |  |  |
| M0-03C | 已完成（事实存储组件） | event/current/outbox原子提交及存在性/scope组合FK：extensions/store/migrations/001_lifecycle.sql、extensions/lifecycle/store.go、store_test.go。 |  |  |
| M0-03D | 已完成（队列组件） | 因果Outbox/Worker：extensions/outbox/store.go、worker.go及测试；extensions/integration/outbox_recovery_test.go。 |  |  |
| M0-04A | 已完成（队列组件） | extensions/taggov/writer.go、writer_test.go：扩展发起的read/merge/replace/verify单队列，原生ID写入，只改目标命名空间。 |  |  |
| M0-03E | 已完成（Host本地接线） | extensions/host/config.go、runtime.go及测试，cmd/extension-host/main.go及测试；Outbox Store/Worker scoped claim及测试；.env.example、README.md、.gitignore。 |  |  |
| M0-04B | 已完成（已接线；C-10 为已接受的 Baseline Limitation） | extensions/taggov/mapping.go、http_store.go、projector.go及测试；writer.go队列内ApplyResolved；extensions/integration/public_api_test.go及Host测试串起持久Outbox与HTTP适配。 |  |  |
| M0-06A | 已完成（本地门禁与CI定义） | extensions/scripts/check_module_boundary.py、check_module_boundary_test.py、.github/workflows/extensions.yml。 |  |  |
| M0-06B | 用户决定不做（2026-11-17，容量压测非功能缺口） | extensions/integration/bridge_test.go、ingest_recovery_test.go、concurrency_test.go。 | 2026-11-17 |  |
| M0-06C | 已修复（M2内核批次，2026-10-31） | outbox/worker.go仅抑制纯父上下文取消的额外报告，保留业务错误join取消；worker_test.go新增两例，原严格drain断言不改。 | 2026-10-31 |  |
| M1-A | 已完成（REL-08 真实 MinerU E2E 与权限回归通过 11-15） | PDF Cheap Preflight、原生薄客户端/Host及真实上传归档→分块已交付；持久恢复/SQL产物、全来源/权限重查及每库标定未完成。 |  |  |
| M1-B | 已由版本化产物实现覆盖（parser/artifacts.go 已在册） | 版本化原始产物：extensions/parser/artifacts.go、计划extensions/store/migrations/003_parser_artifacts.sql（002已用于M0，实施时核实下一版本）。 |  |  |
| M5-A | 抽检复核 UI 已交付（2026-11-17）；其余由 M5-ACCEPT 覆盖 | 反馈指导治理：extensions/feedback/events.go、governance.go、cooldown.go、governance_test.go；扩展预算组件。 | 2026-11-17 |  |
| M5-B | 已由 M5-BINARY 验证覆盖（发布 Saga 与对账已接线） | 发布Saga与对账：extensions/revision/saga.go、remote_identity.go、reconcile.go、publish_recovery_test.go。 |  |  |
| M6-A | 用户决定不做（2026-11-17，原始目标排除血缘图） | mDNS/认证能力：extensions/discovery/mdns.go、capabilities.go、static.go、discovery_test.go。 | 2026-11-17 |  |
| M6-B | 单库血缘图首版已交付（2026-11-05） | 原生导航 /platform/lineage 使用AntV X6，分开呈现版本历史、内容溯源、文档关联；支持库切换、图中搜索、缩放平移、节点详情和指定节点展开。扩展只读查询实际节点/边，复用JWT管理转发。 | 2026-11-05 |  |
| POST-M0-GOV | 治理面已接线并验收（2026-11-17） | 直接读 read_document.go:163、图谱 query_knowledge_graph.go:228、搜索排除 search_parallel.go:193、名单/数据/分析/schema/MCP 均已接 requireGovernedDocument/rejectUnattributed；中途停用已转… | 2026-11-17 | [post-m0-gov-closeout-2026-11-17.md](docs/evidence/post-m0-gov-closeout-2026-11-17.md) |
| POST-M0-TAGS | 已完成并生产验证（2026-11-17 用户改判实施） | 独立标签只读API，候选internal/handler/knowledge.go、internal/router/routes_knowledge.go；扩展taggov/http_store.go适配。 | 2026-11-17 |  |

## M1细分任务（2026-10-30启动）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| M1-00 | 已完成 | 源码映射与首批计划落盘，M0提交及本机4.0.1/可用PDF库核实。 |  |  |
| M1-01 | 已完成（协议/显式scope POC） | scope及V1构件通过冷/热源字节证明、跨scope拒绝、同进程回执/未知结果门卫；真实4.0.1 Flash/txt两job/ZIP/版本/缓存计数退出0。一次复核及一轮修复完成；不启用Host，不代替原生上传API、KB权限或Doc归属。 |  |  |
| M1-02 | 已完成（纯决策函数） | parser/router.go及router_test.go覆盖四档/跨页/native/空预检/稀疏比例；定向与最后完整十三包测试退出0，Linux parser race退出0，一次实际独立复核无实质问题。不代表文件预检或M1已验收。 |  |  |
| M1-03 | 已交付（PDF支持子集） | preflight.go使用pdfcpu v0.16.4；真实PDF统计总页/结构文本层/纯图页，冷/热源均预检；强制有效flash/txt且去重前覆盖auto/ocr/advanced；ActualText、Form/压缩/隐藏层/低字符码、坏文件/取消/解码超限回归。真实4.0.1合成文本文件无OCR下载证据；不… |  |  |
| M1-04 | 已交付原生PDF接线（恢复待续） | 三处原生生产文件仅注入转发；parser/native.go、host/config.go/runtime.go、bridge/server.go挂载可选鉴权路由；身份/版本拒绝、配置关闭、失败不回退、Linux构建与真实上传已验证。非PDF/URL保持原生；持久恢复另列。 |  |  |
| M1-04D | 已交付（可信核心身份） | Knowledge的tenant/KB/doc通过Header逐请求传递；NativeHandler每doc独立scope自动绑定，同字节两个doc独立job/归档、跨doc拒绝；原生并发身份与后台cancel测试通过。涉及parser_proxy.go/native.go/scope.go/mineru_uploa… |  |  |
| M1-05 | 已交付SQL与进程恢复，断电待验 | 003_parser.sql及recovery.go完成ext_parser_artifacts原始JSON、格式/schema/MinerU版本、正式任务及身份FK；FULL同步DB，落盘后登记再返回ZIP；登记失败503，重启可核验文件并补登记。v1/v2升级保留原数据、进程直接退出/并发/跨身份及真实SQL原字… |  |  |
| M1-06 | 工具已交付，真实标定待语料 | calibrate.go、cmd/parser-calibrate、空manifest.json；真实文件哈希核对、按tenant/KB计算验证集85%/10%/15%、拒绝重复内容/文档族及空集、不使用验证集调参。用户确认暂缺真实PDF/扫描件与人工标签，故未报告真实指标或完成阈值拟合。 |  |  |
| M1-07 | 已交付联合回归，整体验收待语料/档位 | 既有真实模型证据保留；新增--joint-m0实测上传→预检→唯一job→FS/SQL原始产物→原生分块，重启生产Host复用同job；同一凭证/问答断网关联拒绝且答案生成0、通网收到门卫且答案生成1。此联合模式用本地确定性模型，不宣称再次调用DeepSeek。生产语料/其他档位及03/06完整验收仍待续。 |  |  |
| M1-04R | 已交付（已知恢复与受控容量） | 以已验证内容hash+tenant/KB/doc+有效策略+后端去重，SQLite先提交intent；已知job/权限可跨重启恢复，未知结果不重发。空闲且持久状态成功的scope可驱逐恢复；130完整文档、12并发、活跃job不驱逐、子进程直接退出与真实Host重启复用单job验证通过。128仍是同时驻留上限，满活跃… |  |  |
| M1-05P | 目录屏障及崩溃窗口已交付，断电待验 | artifacts.go、artifact_sync.go/windows.go及同步/崩溃测试：非Windows尝试文件Sync→暂存目录Sync→Rename→根目录Sync，再允许SQL/ZIP；失败503且不登记，已发布文件重试补屏障。四个真实退出窗口及9个行为场景验证单job/原件/SQL/FK/跨身份/遗… |  |  |

## M2 内核任务（2026-10-31）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| M2-SOURCE-GATE | 已实现并验收指定链路 | RAG摘要、最终上下文、Agent search/read/list/schema/analysis及MCP文档读取服从active；internal/extensionbridge/documents.go和对应薄Hook。 |  |  |
| M2-UNKNOWN-SOURCE | M2安全拒绝已实施 | 历史/压缩/恢复、Wiki/图谱、页面快照、旧答案亲和标题、SQL/会话检索、通用操作和不透明外部MCP结果暂拒绝；保留存储。 |  |  |
| M2-COPY-LINEAGE | **Blocked by Security/Infrastructure**（2026-11-16 用户决定） | 用户批准的同步来源账本及离线事务/许可决策位于extensions/provenance/、schema6；Host契约已挂载（/provenance/copies/* 实测 fail-closed），原生读取原语 ResolveSourceCopy/CheckSourceCopy 已实现但无调用点。 | 2026-11-16 |  |
| M2-STREAM-REVOKE | 用户决定不做（2026-11-17） | 保持入口快照 + 最终拼接许可，不承诺生成中撤销。 | 2026-11-17 |  |
| M2-BASELINE-CHECKS | 本轮相关8包已通过 | 原49项环境/夹具失败已清除；追加4项换行/DNS/MCP治理期望兼容，保留业务/协议/RBAC断言。 |  |  |
| M2-FRONTEND-GATES | 已完成（2026-11-01） | 清除8项前端测试环境基线失败；六个原测试与 frontend/src/test-utils/posixShell{,.test}.ts，业务断言保留。 | 2026-11-01 |  |
| M2-LOCAL-DEPLOY | 本机验收完成 | 已备份并安装候选Core，启动Core/worker Host；Windows访问端口18880，Bridge仍为WSL回环8090。 |  |  |
| M2-CORE | 已交付（组件范围） | extensions/lifecycle/{machine,rules,stale}.go及测试、store.go同事务guard、store/migrations/004_lifecycle_policy.sql；标注日期驱动、每库规则、并发fence、不可变决策回执。 |  |  |
| M2-INGEST | 已接线（公共API对账观察） | extensions/ingest/lifecycle.go适配持久Observer，Host worker启用；completed后pending→active/scheduled/stale，重复观察恢复未完成决策。 |  |  |
| M2-TIMER | 已接线（24h扫描） | extensions/lifecycle/automation.go及Host启动补扫/24h循环；pending/scheduled/active过期直接stale，scheduled到期可active。 |  |  |
| M2-REVIEW | 已完成（本地告警范围，2026-10-31） | extensions/review/、schema5、Host显式分流/24h扫描、cmd/review-alerts只读CLI；review_due只告警不改状态。 | 2026-10-31 |  |
| M2-CONFIG | 已交付（2026-11-01） | extensions/management/、Host管理认证、原生JWT薄转发及配置UI；日期快照保留标签，规则正文/本库版本同事务读取。 | 2026-11-01 |  |
| M2-UI-DATE | Baseline Limitation | 保留冻结矩阵：拒绝active未来effective_from，拒绝scheduled清空effective_from；不改变revision、状态，不复活失效态。 |  |  |
| M2-SUPPLEMENT | T3已交付并验收（2026-11-01） | 同库显式父关系、schema9、版本绑定、Critical Edge/声明/audit原子提交、父失效/复核联动、原生JWT管理薄接口和详情UI已接入；见docs/evidence/m2-supplements-2026-11-01.md。 | 2026-11-01 |  |

## 检索缺陷：knowledge-chat 检索为空（2026-11-16 修复）

| ID | 状态 | 能力／期望结果 | 验证日期 | 证据 |
|---|---|---|---|---|
| BUG-KB-CHAT-EMPTY | 已修复并生产验证 | 整库聊天在 before_retrieve 前枚举原生授权文档；guardPromptResults 把空 KnowledgeIDs 视为无文档级限制。文件：internal/application/service/chat_pipeline/{search_parallel,governance}.go。 | 2026-11-16 | [knowledge-chat-search-nothing-2026-11-16.md](docs/evidence/knowledge-chat-search-nothing-2026-11-16.md) |
