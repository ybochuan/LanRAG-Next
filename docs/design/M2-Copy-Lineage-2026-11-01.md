# M2-COPY-LINEAGE：副本来源与撤销设计提案

状态：**用户已批准扩展来源账本；扩展内实现及离线验收完成。原生接线、Host接口、能力关闭及下载扩围未获本阶段授权，当前运行行为不变。** 验证见 `../evidence/copy-provenance-ledger-2026-11-01.md`。

源码：HEAD `fa6353e8c821daa5eeb667dc46140e4d0d32f179`，describe `v0.9.0-2-gfa6353e`；Frozen上游v0.9.0。本文不改写Frozen；下表原生候选均未在本阶段接线。

## 目标及当前边界

按M2待办顺序，先完成C-24副本设计：当受治理文档失效后，模型不能通过其受控副本重新读取内容。原生租户/用户/会话/文件权限仍先执行；治理只能进一步收窄权限。文件、消息、记忆原件保留。

当前已经实现的active门禁针对已知文档来源；它不能自动证明任意文件、记忆或shell输出的完整来源。用户粘贴、独立提供的附件、模型自身知识、已经下载到外部设备的字节及生成中的持续撤销不由本设计伪称覆盖。已开始生成的撤销另属M2-STREAM-REVOKE。

下列分析只读取源代码，没有操作真实文件、资源令牌、记忆或数据库。确认了可用入口和缺少的来源字段，不等于在生产配置中复现了泄漏；记忆入口需相关功能开启才会执行。

## 精确源码位置

| 边界 | 文件／位置 | 方法／行为 | 当前证据与候选接线 |
|---|---|---|---|
| 统一模型文件路由 | `internal/agent/tools/read_file.go:142,158` | `ReadFileTool.Execute/read` | 根据resource/sandbox、web、attachment、skill及workspace分流；必须分别保留原权限，不能只校验工具名。 |
| 产物模型读取 | `internal/agent/tools/read_artifact.go:28` | `ReadFileTool.readArtifact` | `artifacts.Read`后直接分页；需要在读取前及内容返回前绑定当前副本许可。 |
| 产物授权／文件字节 | `internal/application/service/agent_artifact_files.go:30` | `agentArtifactFiles.Read` | 已核对tenant/user/session、协作组及持久artifact；`ContentHash`可复用，但没有完整DocID来源集。候选门卫须位于原授权之后、GetFile之前；返回字节还需校验绑定版本/摘要。 |
| 工作区分页及缓存 | `internal/agent/tools/workspace_reader.go:94,127` | `readWithCache/read` | 缓存依据session/path/size/mtime/变更epoch；这些字段不是来源许可。每次命中缓存也必须重验，不重写缓存算法。图片和文本共用同一前置门卫。 |
| 产物创建 | `internal/application/service/artifact_collector.go:511` | `ArtifactCollector.persistBytes` | 上传字节、绑定resource，再产生带hash/path/mtime的artifact；候选加入来源登记ACK，不能把“文件已落盘”当作“治理已发布”。 |
| 消息产物元数据 | `internal/types/message_artifact.go:12` | `MessageArtifactRecord` | 有ContentHash、SourcePath、ModTime及session/message，缺完整文档依赖；当前不提出修改此原生表。 |
| 同步shell内容 | `internal/agent/tools/shell_exec.go:450` | `ShellExecTool.Execute` | 标准输出、标准错误及OutputFiles均可包含副本内容，不经过read_file。没有完整来源时不能只给输出文件加校验便声称覆盖。 |
| 异步shell | `internal/agent/tools/shell_exec_async.go:149` | `ShellExecAsyncTool.Execute` | 提交与后续取结果分离；重启后已存在job亦需在读取结果时校验。 |
| 异步工作结果 | `internal/agent/tools/work.go:153` | `WorkWaitTool.Execute` | Detailed状态可携带结果；必须按job类型处理，不能不加区分地阻断所有协作任务。实现前继续定位公共状态读取服务，列出每个结果出口。 |
| RAG自动记忆 | `internal/application/service/chat_pipeline/memory_recall.go:52` | `PluginMemoryRecall.OnEvent` | Recall后注入Prompt；旧答案affinity背景的省略不等于此入口关闭。 |
| Agent自动记忆 | `internal/application/service/session_agent_qa.go:329` | `AgentQA`内Recall／SetMemoryPrompt | 依赖记忆服务及用户开关；并无逐文档来源重验证。 |
| 显式记忆工具 | `internal/agent/tools/search_memory.go:87` | `SearchMemoryTool.Execute` | 调用SearchMemory；`agent_service.go:1343`按开关注册，后续门卫不能只隐藏schema。 |
| 记忆事实 | `internal/types/memory.go:287` | `MemoryItem` | origin及source_session/message不等于完整文档来源。manual/explicit标记不能自动证明不含受治理内容。 |
| 原生下载 | `internal/handler/session/artifact_download.go:160,224` | `DownloadMessageArtifact` | 原生会话与产物授权后GetFile；这是用户下载面，是否扩围必须单独确认。 |
| 已签发令牌 | `internal/application/service/resource.go:451`；`internal/router/files.go:272` | `ResolveAccessGrant`；下载handler | 校验令牌/资源后取文件，未核对生命周期；授权对象只有ResourceID，缺副本owner及DocID依赖。 |
| URL抓取旁路 | `internal/agent/tools/web_fetch.go:248` | `WebFetchTool.Execute` | 获得旧下载URL可能绕过resource://读取工具；资源端重新核验比仅按URL字符串拦截可靠。外部CDN/已下载内容仍不在可撤回控制范围内。 |
| Web缓存与快照 | `internal/agent/tools/web_fetch.go:117`、`read_web_page.go:11`；`internal/application/service/agent_web_pages.go:42,95` | 抓取缓存、页面Save/Read、read_file的web分支 | 已保存的内容可绕过远端URL再次读取；来源必须继承到缓存/快照，跨轮、分页和重启后逐次重验，缺来源则拒绝。只拦下载端点不够。 |
| 后台自动通知 | `internal/agent/background.go:39`；`internal/application/service/shell_job_service.go:1510`、`local_shell_job_recovery.go:25` | injectBackground、任务通知与恢复 | 无需调用work_wait也能把任务描述、产物路径写入下一轮Prompt；标题/描述均可能含文档内容，须按来源复核或省略相关文本。 |

大致行号基于本次HEAD加既有工作区补丁；接线前需再次核实。表中的候选位置是调查结论，不是原生改动授权。

## 冲突点（重要）

| 编号 | 冲突 | 处置建议与代价 |
|---|---|---|
| C-27 | Frozen第5章规定derived_from为异步可重建边；以它作为即时授权依据，会在提交/队列延迟或崩溃窗口放行。 | 用户已批准独立同步来源账本；清单/源引用/审计同事务，只有可信源集完整才可成为ready。derived_from仍为异步投影，本阶段没有实现其投影或原生发布接线。 |
| C-28 | 现有产物hash不能证明语义来源；shell能读写任意许可范围内文件并直接回显。 | 完整源集只能由可信的运行协调层累计、封存，不接受模型自报完整。缺来源时拒绝相关读取；一般shell进程读写溯源不能靠极薄HTTP补丁实现。 |
| C-29 | MemoryItem只有会话/消息来源；启用自动Recall或search_memory时，可能重新注入过去保存的知识内容。 | 恢复前建立记忆到可信源集的绑定；没有绑定的模型记忆读取应按单独获批的安全拒绝处理，不删除记忆，不把origin当白名单。 |
| C-30 | 下载授权只绑定物理资源；同一内容可多owner绑定/去重。ResourceID或hash不能唯一还原逻辑副本来源。 | 以copy_id+owner binding区分逻辑副本；新受控链接可绑定copy_id。历史链接无法确定来源时仍是C-24限制，不能按“某个owner有效”放行另一个owner。 |
| C-31 | 全面封口涉及模型文件、shell/异步结果、记忆、资源下载及URL读取，明显超出只改一个read_file入口和Frozen恒定PATCH面的原假设。 | 逐入口申请明确薄接线；不重构权限、ToolRegistry、shell执行器、存储和调度算法。若要求任意进程输出精确溯源，需要隔离执行/独立代理阶段。 |
| C-32 | 文件存储、原生DB、扩展DB不是同一事务；登记超时可能“已提交但ACK未知”。 | 采用隔离待发布状态、幂等操作键和reconcile；读取前事实缺失即拒绝，不删除文件或盲目重放上传，也不宣称跨模块原子提交。 |

独立检查补充：Web内容缓存/持久快照和后台自动通知必须作为副本处理，不能因为未经过read_file或work_wait而省略。其恢复前要求与普通副本一致的来源继承及当前许可；未知来源保持不可信。

## 方案比较与批准范围

1. **临时封住指定模型入口。** 复用现有未知来源拒绝能力，暂时停止模型读取未绑定的workspace/resource副本、记忆及相关shell输出。接线较小，但会降低文件处理能力；旧URL、外部副本仍是限制，不能作为“全部副本撤销”验收。
2. **推荐：分阶段建立可信来源账本，再逐入口接管。** 先完成扩展内的清单事实与事务门禁，再按明确清单接原生、补录可证明的历史来源；未绑定内容保持拒绝。保留模型能力的恢复路径，代价是需要M3最小权威来源基础及多入口接线，不能一轮宣称完成。
3. **全进程/全文件精确溯源。** 需要受控执行代理或隔离边界；不能用原生大重构完成，留待独立阶段。

用户选择第2项的扩展基础及离线事务验收，原生接线另行确认。是否临时关闭第1项涉及的能力、是否同时接管人工下载仍未授权；本阶段不改变现有行为。

## 来源清单拟定约束

- `copy_id`标识逻辑副本，不等于物理ResourceID或hash。绑定tenant、原生owner/session/message或job、内容版本与完整字节摘要；相同字节的不同owner不能共享许可。
- `source_refs`为完整且可验证的文档版本集合，含owner tenant/KB/doc、生命周期event版本和可用原生文档版本证据。当前尚无可靠文档版本身份时保持pending，不用DocID伪装完整version。
- `complete`只能由可信协调层在所有输入来源封存后写入。新运行使用服务端生成的opaque run ID；浏览器request_id和模型参数不具备签署完整性的权限。
- `manifest + source refs + audit`在扩展同一WriteExecutor事务提交；未确认的副本不得进入可读发布态。清单幂等键同时绑定scope、payload digest和操作；不同payload复用同键冲突拒绝。
- 所有父源当前都存在、原生权限允许且active，才允许副本读取。任一源未知/失效、摘要或owner不匹配、Host不可用、响应scope不匹配均拒绝。每次读和缓存命中重验；每次许可只绑定本次已核对的内容版本，不跨请求缓存allow。
- source集合不得通过覆盖/删源缩小以“恢复可读”；更正来源生成新清单版本及审计。受治理副本再次复制继承源集；不是只保存一条父边或只检查直接父节点。
- cross-KB/cross-tenant默认拒绝，待显式授权、来源与目标权限均核验后再设计。不能把另一个租户的文档存在性暴露给调用者。
- derived_from作为异步可重建DAG投影；投影失败不改变清单、生命周期或许可事实。不会自动复活stale父源或修改原文档日期。

本清单不是Frozen版本族/完整DAG的替代品。上述C-27同步事实适配已获批准，schema6在独立扩展库定义来源版本、清单、当前指针、幂等回执及审计；本轮只迁移临时测试库，没有升级正在使用的数据库。

## 按顺序实施与验证清单

| 顺序／任务 | 涉及路径（新建路径为建议） | 可观察验收 |
|---|---|---|
| COPY-00／设计已完成 | 本文、TASK_BACKLOG | 来源扫描、冲突、批准范围已记录；不等于原生治理已接管。 |
| COPY-01／扩展账本已完成 | `extensions/provenance/{types,storage,source,manifest}.go`、`store/migrations/006_copy_provenance.sql`、迁移/并发/恢复测试 | 缺可信完整源证据只能pending；源版本/清单/回执/审计任一步SQL故障回滚；16写者同键去重/CAS单赢家，独立连接竞争、进程提交前/后退出恢复及FK已验证。 |
| COPY-02A／扩展只读决策已完成 | `extensions/provenance/policy.go`及测试 | 当前版本、active、有效日期、完整继承源集、祖先当前清单、scope/owner/session/hash逐次校验；未知或DB不可用拒绝。只做扩展库决策，不推断原生ACL。 |
| COPY-02B／Host契约待另批 | Host路由、可信Authority适配及契约测试（尚未新增） | 原生先授权；Host停/超时/503/错scope均拒绝，不能以本地库测试代替HTTP或真实原生验收。 |
| COPY-03／可信来源累计 | 原生现有文档门卫DTO适配、运行上下文、产物登记位置 | 所有受治理模型输入纳入保守源集；并发工具未完成不得封存；伪造request_id/empty sources/complete=true不能放行；不假定LLM自报可靠。具体补丁必须逐项确认。 |
| COPY-04／文件模型门卫 | 上表read_file/read_artifact/agent_artifact_files/workspace_reader、web_fetch缓存、read_web_page及agent_web_pages候选 | 真实active源→保存产物/Web快照→源stale→同一resource/path/分页缓存/图片/跨轮web读取被拒绝；重启后复测。读后替换、同路径/同mtime/不同hash、跨会话/同名文件均拒绝。 |
| COPY-05／旁路治理 | shell_exec/async/work结果、background自动通知及shell恢复、两类自动Recall/search_memory、资源grant/下载及web_fetch候选 | 提交前已有job、未调用work_wait的自动注入、重启恢复描述、旧memory/旧token/URL重定向/Host不可用分别测试；捕获所有模型文本/图片输入，标题/描述/正文独立标记。人工下载是否纳管取决于明确批准，不以工具负向代替资源端验证。 |
| COPY-06／联合验收与界面 | 隔离E2E脚本；后续来源可视化入口 | 原生上传→active→派生产物→重启→stale后重读拒绝；保留原件；单源/多源/副本再复制/多个owner去重/撤销后旧链接逐项验证。UI只展示经授权的来源与拒绝原因，不暴露其他租户资源。 |

COPY-01/02A的定向/完整build、test、vet、Linux race、模块边界及SQL故障/真实并发已执行，准确命令和状态见验收记录。COPY-02B～06的HTTP、原生集成与真实副本撤销仍是**未来验收**，未标通过。

## 审批与后续顺序

已确认：分阶段清单方案、C-27适配以及扩展内同步来源账本/离线验收。未确认：可信原生版本和完整来源封存适配、Host/原生接线、临时关闭哪些模型副本能力、人工下载和旧grant纳管。不能把COPY-02B～06标为已授权或已完成。

原生接线仍受AGENTS.md第2.2条约束。候选表只用于审批范围，不授予修改原生业务的权限。M2-STREAM-REVOKE接在COPY设计之后，独立定义撤销SLA和已发送内容边界；原生后端基线随后排查，M3提供来源版本权威性。

## 本轮设计复核

一次独立只读检查发现两项Important：Web快照/缓存、异步后台自动通知未写全；均已补入上表、来源继承约束及COPY-04/05验收。实施者再次核对对应源码。未派第二次独立检查，未把设计复核当作业务实现验收。

扩展实现另经一次独立代码检查，无Critical/Important；完整测试发现的schema6只读CLI兼容问题修复后作了定向复查。真实文档版本与完整来源封存仍是`Authority`可信端口，只有测试夹具实现，Host未挂载，不能声称已经约束真实副本。
