# T4 可信运行封存与模型副本门卫：原生接线申请

状态：**用户2026-11-01明确批准T4整批薄接线及未知来源安全拒绝。** T1/T2、T3已交付；T4仍在实施，不能把批准当作已完成。源码HEAD `fa6353e8c821daa5eeb667dc46140e4d0d32f179` / `v0.9.0-2-gfa6353e`，行号按当前工作区读取。

目标沿用用户选择的“同步来源账本 + M3可信来源基础”，由可信服务端累计实际模型输入，封存后才能使模型读取派生文件。原生只传身份、指纹、输入组成与调用结果；状态机、许可、重试、审计和持久事实继续在extensions。不重构合并、RRF、Agent循环、工具注册、shell或存储算法。

## 已确认冲突及取舍

| 冲突 | 本次方案 |
|---|---|
| C-34：merge替换父正文、拼邻块/FAQ/图像描述，CitationSources与SubChunkID不是完整输入证明 | 保留原生merge；只认可实际使用字段与当前原生快照的精确匹配。变换/组合缺失可信组成时，该运行保持unattributed，派生副本不能ready，不拿引用列表补成complete。恢复这些形式要补来源适配；仍不能通过薄点证明时报告，不改合并算法。 |
| v1快照只有chunk.content_hash与整行fields_hash；不能独立核验ContextHeader等字段 | 在既有只读快照路由增加**显式可选组件视图**，默认v1 JSON与root完全不变。指定最多64个chunk，返回其全部字段hash；按既有排序折叠后必须等于对应fields_hash，从而绑定既有完整root。Header复用header_fields。仍无正文返回、同一只读事务、原权限与500ms/容量限制。 |
| C-28/C-31：任意shell、旧workspace/skill文件及历史memory没有完整来源，旁路远多于read_file | 本批建议对未知模型副本安全拒绝；无可信执行代理的shell不启动，旧job仅保留已授权的ID/状态控制信息，正文/标题/描述不进模型。自动Recall/search_memory及后台文本无证据则省略或拒绝，原件/任务仍保留。此项会降低现有文件/技能/命令能力，需此次明确确认。 |
| C-32：文件/原生DB/extDB跨事务，ACK可能未知 | 模型可读态以扩展提交为准；文件已存在不等于已封存。幂等键绑定payload，ACK未知先查回执/事实，不重放上传或命令。源文件和原生记录不删除。 |
| Producer归属缺口：ArtifactCollector扫描整个session目录，旧文件可能被新run拾取 | 必须有对应实际写入的producer凭据，绑定run/step/owner/session/规范化path/最终完整bytes hash及逻辑copy版本；collector只能匹配凭据，不能签来源。未知旧文件保持不可读，不以本轮目录扫描、mtime或文件hash单独洗白。 |
| 治理依赖与模型实际读过的来源不同 | 两组证据分开：清单只存实际输入来源；每次读取再展开最新T3依赖做许可。本轮扩展修复已覆盖此点，不把父规范文件伪称模型输入。 |
| C-30/C-36：人工下载、旧grant、外部副本与原生图像hash不足 | 本次不接管人工下载、既发grant或外部镜像。模型侧web_fetch/缓存读取不能用旧下载URL绕过，未知内容拒绝。没有实际字节/组成证明的视觉运行不能签发完整副本证明；原生直接用户附件仍是独立授权边界，不冒充KB来源。 |

## 复用、适配与拒绝

- KEEP：schema6同步清单/幂等/audit，schema8真实观察版本，schema9依赖，原生资源/会话/KB权限，既有编码、分页、缓存、模型客户端和工具调度。
- ADAPT：新增扩展运行/步骤事实及`SealedCopyReader`生产实现；原生在真实对象消费/模型发送和产物绑定处传DTO。真实模型启动/探测优先使用内置model-supervisor的`/extensions/models/start`、`/{id}/probe`；确定性故障夹具继续用于捕获所有模型输入与精确时序，不替代真实模型联调。
- REJECT：模型/浏览器自报complete、用request_id当run授权、把空来源当纯聊天证明、按path/mtime或hash单独认领owner、用异步derived_from作即时许可、为签出complete改原生业务算法。

## 本次申请的原生薄点

下表每行都是明确的接线范围；方法内只增DTO/指纹/HTTP和相应拒绝/省略分支。测试文件可随对应包添加。未列出的原生生产文件不默认获授权，出现漏点先报告。

| 文件 | 当前点／拟增加内容 |
|---|---|
| `internal/extensionbridge/source_run.go`（新）、`source_copy.go`（新） | 固定回环、版本1、单请求500ms、无代理/重定向；沿用服务端强凭据。DTO绑定tenant/KB/user/session/run/attempt/step/精确hash，严格校验响应。只做传输、hash和请求内身份传递，不存许可缓存或运行事实。 |
| `internal/handler/extension_source_snapshot.go`、`extension_source_components.go`（新） | 既有ExtensionSourceSnapshot显式`view=components-v1&chunk_ids=...`分支；默认v1不变。组件视图校验非空/重复/64上限与精确归属，全root不截断，缺任一指定chunk整体拒绝。复用既有GET路由，无原生表改动。 |
| `internal/application/service/session_knowledge_qa.go`、`session_agent_qa.go` | 原生身份/授权scope确定后传BeginRun DTO，正常终态请求封存；失败/取消标中止。Agent自动Recall无证据省略。后台未完成时不发送“所有输入已结束”证明，不复用浏览器request_id。 |
| `internal/application/service/chat_pipeline/into_chat_message.go`、`governance.go` | 对实际用于标题/正文/描述/上下文的字段传来源证据；无法核验的合并或图像组成标unattributed，不改原有拼接算法，不偷用CitationSources。 |
| `internal/application/service/chat_pipeline/chat_completion.go`、`chat_completion_stream.go` | 编码后、实际Chat/ChatStream之前登记真实message hash/已证明组成；调用结束登记结果hash与终态。此处不改guidance fail-open，不在T4引入流中取消。 |
| `internal/agent/think.go`、`act.go` | 实际模型调用与executeSingleToolCall的入口/出口登记步骤；并发每个step独立ID，所有步骤ACK且无unknown才能封存。**未知副本内容/文件名/图像的读取拒绝，必须早于act的describeToolImages和任何后续LLM/VLM调用或结果交付**，不能只污染最终seal。保留原errgroup、重试、截断和调度；新来源失败不得自动重放有副作用的工具。 |
| `internal/agent/engine.go`、`internal/application/service/agent_service.go` | 在实际Execute结束而非HTTP异步请求返回时结束run；原有systemPromptOptions/selected/pinned metadata生成后，核验或省略未知skill等可变描述。仅元数据DTO/拒绝分支，不改循环、注册、技能安装或调度算法。 |
| `internal/agent/tools/governance.go`、`search_knowledge.go`、`read_document.go`、`list_documents.go` | 原授权后，为实际返回的字段/片段取得来源证明；只把服务端证明带回受信运行上下文，不接受工具参数注入来源或complete。schema/analysis既有未知内容拒绝保持；不能核验的新增工具结果默认污染运行，不能静默当空源。 |
| `internal/application/service/artifact_collector.go`、`agent_artifact_files.go` | persistBytes已知真实字节/hash后请求登记逻辑copy绑定；读取保留原native owner/session/协作权限，缺封存证明不得用于模型。登记ACK未知仅查询事实，不重新SaveBytes。 |
| `internal/agent/tools/sandbox_write.go`、`sandbox_edit.go` | 已有lockSandboxFile保护范围内，对实际参与append/edit的旧bytes先取许可并继承旧清单来源，再执行原写算法；成功后以期望新bytes与实际落盘hash核对，登记producer步骤凭据。旧来源未知则副作用前拒绝。overwrite亦由Host解析已有逻辑copy/version，禁止靠新ID或路径换壳缩小已知源集。不调整既有文件变更队列或替换算法。 |
| `internal/agent/tools/read_file.go`、`read_artifact.go`、`workspace_reader.go` | 区分真实用户附件与派生文件；派生读取前许可、返回前核验实际字节hash并重查许可；缓存命中每次同样校验。未知workspace/skill副本拒绝，不改原分页/缓存算法，不给目录列表/路径文字开内容旁路。 |
| `internal/agent/tools/sandbox_ls.go` | 目录元数据同样是模型可见文本；未知派生文件名/路径不作为内容旁路输出。保留原生会话/目录范围，返回受许可的元数据或安全省略，不修改底层目录访问算法。 |
| `internal/agent/tools/shell_exec.go`、`shell_exec_async.go`、`work.go` | 无执行来源证明时，在新命令副作用前拒绝。已有work仍按原生会话权限读取/取消状态；unknown任务不向模型返回Title/Payload/输出文件内容，不笼统禁止所有协作状态控制。 |
| `internal/application/service/chat_pipeline/memory_recall.go`、`internal/agent/tools/search_memory.go` | 未绑定完整源集的记忆不进入模型；不删除memory，不把manual/origin字段当来源豁免。query_understand.memoryBackground已有安全拒绝保持。 |
| `internal/agent/background.go` | 在RoundStart/BeforeFinal取正文前拒绝未知背景注入；不确认未展示文本、不改通知存储/调度。状态控制与未知正文分开，避免新一轮因未知通知无限继续。 |
| `internal/agent/tools/web_fetch.go`、`read_web_page.go` | 模型读取旧资源URL/重定向/缓存或快照时同样要求可核验的来源；未知读取拒绝，不能仅拦resource://字符串。M2已拒绝的Wiki/页面快照保持。原生WebSearch结果的既有保留规则不改，不声称外部公开镜像可撤回。 |

T4仍不能替代T5生成中撤销；已经发给用户/提供方的数据无法收回。人工下载handler、resource grant解析、原生DB迁移、ToolRegistry、shell执行器/任务调度及merge实现不在本次补丁清单。

## 扩展运行事实与协议约束（拟实施）

文件：`extensions/provenance/{run,step,seal,copy_http,components}.go`、`extensions/host/runtime.go`及下一序号迁移（当前最高009，实施前再核对）。不改已发布006–009，不动根Go依赖。

拟定服务端证明接口如下，均为固定回环POST，`X-AgentRAG-Bridge-Version: 1`，复用Core/Host已有`AGENTRAG_MANAGEMENT_KEY`认证；不开放浏览器直连。这些是来源管理/证明API，不改变Frozen四类Hook及其降级语义。

| 端点 | 职责／禁止事项 |
|---|---|
| `/provenance/runs/begin` | 原生身份/scope/幂等operation绑定，Host签发run/attempt capability；拒绝自定complete或任意run认领。 |
| `/provenance/runs/steps` | 服务端步骤开始/结果与对应input/output digest；terminal不等于完整，unknown不可擦除。 |
| `/provenance/runs/inputs` | 实际模型输入组成与组件证明登记，完整性由Host验证；未知组合显式记录，不把消息hash当来源证据。 |
| `/provenance/runs/finish` | 请求收束，由Host核对所有步骤及来源后决定sealed或unattributed/aborted；不接受complete布尔开关。 |
| `/provenance/producers` | 精确写入步骤/旧源继承/实际最终bytes及逻辑copy绑定；不是文件扫描上传接口。 |
| `/provenance/copies/resolve` | 原生目标文件owner/session授权后，按精确binding向受信Core返回待核验的源ID/清单revision等元数据，无正文；错误binding不查源也不透露存在性。 |
| `/provenance/copies/check` | Core按现有原生逻辑重新授权**全部源文档**，传显式允许范围与精确copy/binding；Host核对current/active/依赖及集合，不按Host配置范围替代原生ACL。500ms/超时/503/错scope/解码错均拒绝。 |

来源ACL解析留在原生现有授权服务/工具辅助器：`agent_service.go`传递已有SearchTargets，`tools/governance.go`复用现有authorizeLoadedKnowledge/searchTargets检查，`agent_artifact_files.go`调用该薄适配。当前原生授权范围未覆盖来源KB/文档时拒绝，不因为caller拥有文件就扩大到整库；双阶段resolve/check的源ID只供受信Core授权，不能透传给模型。

1. Begin只接受经鉴权Core；Host生成opaque run/capability，绑定原生tenant/单KB/user/session/attempt。浏览器/模型只传request_id或猜run_id无效，capability不进ToolResult/Prompt/一般日志。
2. 步骤状态open→finished或unknown/aborted；幂等操作键同时绑定内容摘要。unknown不可被删源或complete=true覆盖。Core取消/重启而未封存时保持不可读，不自动重放副作用。
3. Source proof校验实际字段hash、native root与active/T3依赖。模型输入组成必须覆盖实际发送内容；原生只做DTO标注，无法证明的变换/编码/工具路径明确unknown。原生“消息hash相同”单独不足以证明来源，不允许据此签出complete。
4. Seal仅在所有真实输入、模型轮次与并发工具终态齐全时，由受信协调层决定。空受治理源集也须有完整的“全部输入均是明确独立输入”证据；第一批无法证明的空集不授予可读清单。
5. 清单来源集为完整保守并集，派生副本继承父副本源集；权限依赖按本轮preparePermission/verifyPermissionTx另行展开。多KB运行可继续既有QA，但第一批跨KB副本封存拒绝，不能缩成某一KB签证据。
6. 生产`SealedCopyReader`只能读取已持久封存、身份/hash全绑定的事实。Host业务路由必须用真实Registry；probe/缺provider返回非200，无测试放行夹具。
7. pending文件/不可封存运行不能变成模型可读副本；这不会回滚已发生的文件保存，不接管原生人工下载，也不把T4说成全系统内容删除。
8. 文件生产与收集分离：只有实际写工具/受信producer能登记生成凭据；run seal和producer commit是两个必要条件。追加/编辑继承被许可旧bytes的来源并保留已有No-shrink；collector匹配当前内容hash、producer step和逻辑copy版本，旧run凭据不得被新run认领。未纳入来源协议的shell/skill写入产物不能靠collector升级为ready。
9. 读取拒绝与来源完整性是两道门：未知copy/memory/skill/目录元数据/后台正文在首次模型消费前拒绝或省略，包含自动VLM支路。对已经过M2文档active许可但缺少完整变换证据的原生KB结果，只能保持运行不可封存，不能宣称C-34已完整解决；其完整证据适配另逐分支验收。不以最终seal拒绝替代当前模型读取门卫。

## 薄补丁骨架（设计，未应用）

```diff
@@ internal/handler/extension_source_snapshot.go: ExtensionSourceSnapshot
+ // Optional explicit view only; default snapshot/root are byte-compatible.
+ // In the same authorized read transaction, collect requested field hashes.
@@ internal/agent/act.go: executeSingleToolCall
+ // Begin this server-derived step over the authenticated bridge.
  result, err = e.toolRegistry.ExecuteTool(...)
+ // Register exact output/binding evidence, or mark this run unknown.
@@ internal/application/service/agent_artifact_files.go: Read
  // Existing native session/owner/resource authorization remains first.
+ // Check exact logical copy binding; unknown/Host failure denies model read.
  data, err := io.ReadAll(...)
+ // Hash these actual bytes and recheck permission before returning them.
```

以上不是让业务规则进入core的许可。若实际实施需要修改变换/合并、重排、执行器或持久表才能覆盖某分支，该分支暂不恢复，并报告Implementation Difference。

## 按顺序交付与验证

| 阶段 | 期望结果 | 验证 |
|---|---|---|
| T4-00／本轮 | 副本许可沿用T3父依赖，语义来源不被污染 | active/review正向；父stale/过期/编辑/离线/损坏负向；核验后新增依赖拒绝；副本再派生不绕过；外部scope不触发来源I/O；完整扩展门禁与review。 |
| T4-01 | 完整root绑定的组件视图 | 默认v1 golden不变；字段hash折叠与leaf一致；替换/缺块/越界/同事务并发/超限拒绝；无原文、零原生写。 |
| T4-02 | 运行/步骤/封存/audit原子事实 | 16并发seal单赢家、重复ACK幂等、不同payload同键冲突；open step/unknown/假空源/错owner/旧cap拒绝；每个SQL故障及提交前/后进程退出回滚/恢复。 |
| T4-03 | 实际模型输入来源 | RAG/Agent文本、标题、描述分开canary；父/邻/FAQ/编码/并发工具分支未证明不能complete；partial capture不能ready，结果receipt不是读许可。 |
| T4-04 | 逻辑副本与模型读取 | 真实上传active→已证明问答→write_sandbox_file/产物→同resource/路径/分页缓存正向；源或父失效/版本变更后拒绝；同hash多owner、读后替换、同mtime不同hash、重启后读取、Host断/超时/503分别测，原件保留。 |
| T4-04P | Producer归属与修改继承 | 预放上一run未收集文件，新run即使完整也不得认领；同path/hash跨run/step或owner不得重用producer。已许可append/edit必须保留旧源，旧源stale时修改前拒绝；覆盖不能缩源，新内容/实际落盘hash不符不得ready；ACK丢失不重复写文件。 |
| T4-05 | 旁路闭合 | 同步/异步shell、既有job、work状态/详细结果、自动/显式memory、skill描述、目录/路径元数据、未调用work_wait的通知、旧URL/缓存分别负向；工具图片在自动describe/VLM前拦截，图片描述调用次数为0；只检查最终答案不算通过。 |
| T4-06 | 联合验收 | 内置管理器启动/探测真实模型做正向冒烟；可控模型夹具捕获全部请求做撤销/故障；全扩展/race、受影响原生包、前端必要范围、Lite构建和隔离E2E。未通过则T4/M2不标整体完成。 |

当前提案保持T4分批实施但一次确认全部**已列出**薄点，避免每完成一个文件重新询问。T5流式取消仍需其专门清单与实测SLA，不混入此确认。人工下载/旧grant如需纳管另列范围。

一次只读builder check提出producer归属、元数据/VLM消费顺序两项Important，已补实际写工具、目录薄点、producer与旧源继承规则、自动视觉调用前拒绝以及相应负向验收；没有因此修改原生源码。
