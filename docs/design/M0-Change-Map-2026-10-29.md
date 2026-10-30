# M0 Change Map / WeKnora Lite Source Mapping

审计日期：2026-10-29。基线：WeKnora `v0.9.0`，`f17ee8a8da901bd658a4cb25f40b5fd41adbfdd7`，与现有 `BASELINE.txt` 一致。

依据：[Frozen Architecture v1.0](<AgentRAG 技术方案 v1.0 — Frozen Architecture.md>)。本报告记录 Source Mapping、Implementation Difference 和 Baseline Limitation，不修改冻结方案。任务状态见根目录 `TASK_BACKLOG.md`。

## 1. 审计结论与本批范围

四类 Hook 均有可定位入口，但“四类 Hook”不等于“四个调用点”。普通 RAG 与 Agent 的检索前、检索后、生成前至少有六处主要调用边界；摄取解析、分块、索引、异步终态、失败和停滞恢复还分散在多个文件。完整首轮映射保存在 `PROJECT_CONTEXT.md`。

累计交付扩展端协议/独立模块门禁、对账/排除/标签队列、SQLite事实事务/Outbox、公共API适配/持久Observer/标签Handler，以及Host配置与运行接线。不修改 `internal/`、上游依赖、原有基线或冻结方案。本地Host验证不代表核心接线或真实混合并发验收通过。

## 2. 冲突点（重要）

| ID／级别 | 冻结要求及源码证据 | 冲突／影响 | 建议与当前处理 |
|---|---|---|---|
| C-01／P0，摄取可靠性阻塞 | 3.2 要求 ingest 不静默丢失。`internal/router/sync_task.go:138–170` 直接启动 goroutine；`internal/application/service/knowledge_span_tracker.go:22–24,478–499` 明确 best-effort。 | 核心提交后、HTTP/spool 写入前仍有崩溃窗口；ext Outbox 只能保证已进入 ext 事务的工作。轮询当前状态不能保证恢复所有中间事件。 | 用户决策来源侧可靠性：同事务 source outbox 最强但可能扩大补丁；durable spool + reconciliation 必须说明仍未原子覆盖的窗口和历史边界。内存重试／span 不能充当 durable 方案。本批不接入。 |
| C-02／P0，标签并发安全阻塞 | 6.2 要求保留业务标签。`internal/handler/knowledge.go:2662–2670,2731–2795` 没有版本条件；`internal/application/repository/knowledge_tag.go:13–50` 全量删除后重建。 | 扩展 read/merge/replace 可覆盖读后发生的用户写入；回读可能无法发现丢失。ext 单写者不约束 UI／上游自动标签写入。 | 先验证最小原子 merge/CAS 接口是否可接受；若操作层串行化，必须覆盖全部上游写入者并明确限制。仅 ext 文档锁无效。未决前不启动真实投影。 |
| C-03／P0，完整治理门禁阻塞 | 3.3 只列 search_knowledge。`internal/agent/tools/read_document.go:198–225` 直接读文档/chunk；`scope_authorization.go:44–105` 只校验原生作用域；`query_knowledge_graph.go:238–347` 有独立图谱查询。 | 搜索 Hook 不能自动约束旧 handle、其他工具和运行中状态变化。原生权限不等于 active 治理状态。 | 复用并收窄 SearchTargets，保留原权限交集；直接读／图谱边界薄检查，明确每工具调用刷新还是整轮快照。调用矩阵／补丁预算需用户确认。本批不改核心。 |
| C-04／P1，M1 接入需 POC | 7 要求预检选档、只正式 parse 一次、保存原始产物。`internal/types/knowledge_process.go:34–37` 支持 upload overrides；`internal/infrastructure/docparser/mineru_converter.go:144–162` 已透传 tier，但 ReadResult 只交付 Markdown/ImageRefs/SourceBlocks；`mineru_v1_client.go:266–280` 下载 zip 后提取。 | 四类 Hook 没有解析前文件和原始 job/artifact 句柄；解析后选档太晚，再次解析违反方案。 | 优先验证可配置 MinerU endpoint 的扩展解析代理，复用 mineru_tier/process_config 并保存产物。无法覆盖的来源再评估额外薄注入；不重写原生适配器。 |
| C-05／P1，补丁预算未证实 | 3.8 估计 2～3 QA hook + 1 ingest bridge；现有映射已有六个 QA 主要边界，C-01/C-03 可能增加调用位置。 | 若个数指物理调用点／文件数，该估计不成立；若指协议类型／公共 helper，可以复用但调用仍分散。 | 分别统计协议类型、适配包、物理调用点、受控文件。每个受控文件附测试，不以 helper 掩盖补丁面。确认前不接入核心。 |
| C-06／P1，M5 远端身份需 POC | 12.7 要求未知结果先 reconcile。`internal/types/knowledge.go:328–339` 的 manual 创建没有 publication_key／客户端ID；`knowledge_create.go:771–870` 先创建再入队；文件上传支持 metadata（`internal/handler/knowledge.go:374–436`）。 | 不能假设所有接口均可原子写发布身份；随后 PUT custom_metadata 不能填补响应丢失窗口。 | manual 验证 deterministic title/source marker + 远端已存正文 hash；binary 验证上传 metadata + file hash。限定 tenant/KB、完整分页、处理多候选及未知状态。先 POC，不直接重复创建，也不预判必须改核心。 |
| C-07／P1，可由实现澄清 | 8.3 SQL 首层混合 derived_from/merged_into/supersedes，递归层没有边白名单；8.4 要求两个 API 分离。 | 直接复制 SQL 会混入版本关系，还可能递归进入其他边。 | 以8.4语义分别实现两套查询，首层和递归层均限定边类型、tenant/KB 和 cycle guard；混合边夹具验收，不改冻结原文。 |
| C-08／配置差异 | `internal/mcp/manager.go:105–106` 禁用 stdio；`mcp_exposure.go:95–104` 默认按需发现；`internal/mcp/security.go:15–32`、`internal/utils/security.go:1332–1374` 校验出站。 | localhost MCP 仍需显式 allowlist；容器内127.0.0.1指向容器自身。 | Lite 同机进程部署，原生设置仅放行所需回环主机并配置认证；保留 SSRF，不改 ToolRegistry。容器拓扑另验收。 |

P0/P1 表示首次审计发现的完整目标风险，不表示上游通用漏洞。上表保留首次审计证据及建议；**M0实际采用以下用户已确认决定，覆盖表中的待决策状态，不再要求为M0增加source outbox、CAS或全工具治理。**

### 2.1 用户确认的 M0 决定

| 决定 | M0做法 | 仍保留的边界／验证 |
|---|---|---|
| C-01 已决定 | 接受Lite内存队列弱点，不改原生数据库；周期对账补当前处理状态，失败可观察并在下周期重试。 | 不保证原始中间事件完整；origin=reconcile观察与lifecycle分开。HTTP Source/持久Observer/临时DB重开已验证，Host/真实服务周期恢复仍待验。 |
| C-02 已决定 | 共享一个Extension标签单写者队列，current truth/ID解析/read/merge/replace/verify串行。 | 不约束原生UI/自动标签/超时请求远端未知结果；接受残余风险，不宣称CAS。HTTP适配/Outbox及Host双KB单写者已在本地fixture验证；真实服务另验。C-10安全拒绝已接受为Baseline Limitation，首写功能仍受限。 |
| C-03 已决定 | 先按excluded_document_ids过滤搜索，完整Agent工具防护后续补。 | 直接读/旧handle/图谱/历史上下文为已知限制；治理Host不可用仍fail-closed。原生未贯通排除参数，见C-09。 |
| C-04 已决定 | 解析代理后移到独立阶段。 | 不纳入M0，不改原生解析器；后续按PA-1验收。 |

**C-09／实现差异：排除字段未贯通。** `internal/types/search.go:255–291` 的SearchParams没有公开excluded_document_ids；`internal/types/retriever.go:58` 的ExcludeKnowledgeIDs仅是底层字段，SQLite `internal/application/repository/retriever/sqlite/repository.go:877–908` 的过滤构造没有应用它。仅在HTTP中发送字段会被忽略。扩展在已枚举授权文档范围上做集合减法，薄适配再转为显式document目标及结果排除；不能扩大原授权、不能把空结果降级为whole-KB。若只在Top-K之后排除，会减少可用候选且可能不足K，不能宣称与检索前过滤等价。本批仅交付集合计算，未修改上述原生文件。

C-05的物理补丁预算在核心接入前逐项复核；C-06仍是M5的身份POC；C-07按冻结API语义实现；C-08通过原生配置处理。

### 2.2 C-10／Baseline Limitation：标签空值歧义（2026-10-30）

源码证据：`internal/application/service/knowledge.go:604–613` 的`GetKnowledgeByID`对`GetKnowledgeTags`失败只记录日志，仍返回knowledge成功；`internal/application/repository/knowledge_tag.go:120–154`无关系时不为文档初始化空数组。原生`GET /knowledge/{id}`因此可能在“确实没有标签”和“加载失败但实际有标签”两种情况下都返回`tags:null`。

影响：把null当空数组执行整体replace可能静默删除业务标签；仅靠Extension单写者和回读不能识别原始读取失败。反过来，安全拒绝null会使原生无标签文档的第一次gov投影无法完成。**用户已接受安全拒绝，将该差异明确归为Baseline Limitation；不再为M0强求同步成功，无标签首写的功能限制仍客观存在。**

当前扩展保护：`HTTPStore.ReadTags`对null/缺失数组返回`ErrAmbiguousTags`，不发送PUT；Projector记录`ext_tag_sync_failures`，Outbox按已配置退避重试/dead。显式数组包括[]允许merge；规范gov标签预先由原生UI/API配置，扩展不擅自创建。真实业务标签绝不从未知读取中重建。

后续方向：用户选择独立标签只读API。后续单独评估只读tag端点，复用已有`KnowledgeService.GetKnowledgeTags`、传播查询错误、正常无关系显式返回[]；候选仅为`internal/handler/knowledge.go`和`internal/router/routes_knowledge.go`，配权限/scope/故障测试，不重构业务服务。该决定不授权本批修改核心；接入时须明确薄补丁范围并复核C-05，当前未实施。不采用把null猜成[]或为强求同步而覆盖用户标签的做法。

## 3. 可复用能力与接入注意点

### E2E接线差异：默认Pass-Through已解除503阻塞（2026-10-30）

历史阻塞：runtime当时调用无Services的`bridge.NewServer()`，Host健康但检索前始终503。用户随后明确批准M0默认Pass-Through；runtime现用`NewServerWithServices`接入`NewPassThroughRetrieveHandler`，保留严格校验、原生授权的非空候选及HTTP门卫，不新增治理排除。空范围直接403，避免原生仅检查200时忽略deny_all；没有改动核心算法。未配置的库构造器及其他业务Hook仍拒绝。复杂过滤、范围枚举和响应消费（C-09）后移，不属于本次完成声明。

认证差异：RBAC=false只关闭角色检查，Auth始终独立。`auth.go:899–911`限制auto-setup为Lite+桌面token；`router.go:174`只在Lite edition或显式WEKNORA_WEB_DIR时挂载SPA。用户二进制名含Lite但未注入Edition=lite，导致根路径落入Auth。脚本以显式前端目录将根页面恢复200，并使用原生register/login JWT使API认证通过；没有修改Auth实现。

历史报告`docs/evidence/m0-e2e-2026-10-30.log`保留认证/断网PASS、通网503失败（退出1）。新报告`docs/evidence/m0-e2e-2026-10-30-pass-through.log`退出0，认证、断网阻断和通网答案/引用/完成全部通过，Host收到同一请求。使用真实核心和Host、隔离SQLite副本及本地模型夹具，未使用Bridge200桩。M0按用户确认的HTTP门卫+默认透传范围验收；既有Outbox Linux race断言失败用户接受为后续项，不能称为已修复或Frozen全部目标已实现。

- `internal/types/search.go:24–46` 已有 KB/document 目标、tenant、tag 和 ScopeTagIDs。`chat_pipeline/search.go:582–610` 对 document 类型空列表不查询；whole-KB 类型仅填写 KnowledgeIDs 不会自动收窄。
- entity 分支也检查 SearchTargets（`chat_pipeline/search_entity.go:185–203`）。治理收窄放在 `search_parallel.go` 克隆前，两路共用；原生权限只能取交集，不能被扩展扩大。
- `session_knowledge_qa.go:777–795` 区分 ErrSearchNothing 与硬错误；治理不可用走硬错误。ErrSearchNothing 可能进入模型 fallback（1036–1081），接入测试必须覆盖。
- RRF 已有租户参数（`internal/types/retrieval_config.go:8–12,28–37`），融合函数使用它们（`knowledgebase_search_fusion.go:170–175`）。网格搜索走既有配置，不重写算法。
- MinerU 已有 tier／per-upload override；MCP 已有按需发现、HTTP Streamable、认证和 SSRF 校验。

## 4. Requirement → Source → Hook → Patch → Test

“受控候选”仍未修改，需要冲突决策；“扩展新增”是计划文件，不代表已经实现。所有路径均相对仓库根。

| 任务／需求 | 来源／入口 | 文件落位或受控候选 | 验证 |
|---|---|---|---|
| M0-02A 协议、健康、拒绝 | 冻结3.6–3.7；现有HTTP骨架 | `extensions/bridge/protocol.go`、`server.go`、`server_test.go` | FC-0，本批 |
| M0-02B DTO/scope 契约 | `internal/types/search.go`、C-09 | 已有 `extensions/retrieval/exclusions.go`；`extensions/contract/retrieval.go`、`contracts/bridge/v1/`和`extensions/bridge/retrieve.go`及测试；受控候选`extensionbridge/contract.go`未创建 | FC-2/CT-1扩展契约与typed handler已测；持久策略/两模块消费/核心接线未验 |
| M0-02C RAG检索前 | `internal/application/service/chat_pipeline/search_parallel.go:90–120`，克隆前 | 受控候选该文件、新增 `extensionbridge/client.go`；扩展 `extensions/retrieval/policy.go` | FC-1/FC-2；C-05 |
| M0-02D Agent搜索排除 | `internal/agent/tools/search_knowledge.go:189–256` | 受控候选 `search_knowledge.go` 与公共薄适配；直接读/图谱明确转POST-M0-GOV，不列入本批或M0补丁 | FC-1/FC-2；C-09字段转换；FC-3后移 |
| M0-02E 检索后 | `internal/application/service/chat_pipeline/filter_top_k.go:60–77`、`internal/agent/tools/search_knowledge.go:308–347` | 受控候选上述两文件；扩展新增 `extensions/retrieval/contextualize.go` | FO-1，格式化前增强 |
| M0-02F 生成前 | `internal/application/service/chat_pipeline/chat_completion_stream.go:73–101`、`internal/agent/think.go:60–70,569–679`；fallback另有调用 | 受控候选上述文件及需覆盖的fallback；扩展新增 `extensions/guidance/inject.go`、`budget.go` | FO-1/GD-1；C-05 |
| M0-03A 摄取对账 | `internal/router/routes_knowledge.go:70–76`、`internal/handler/knowledge.go:1179–1185` 的分页公共API；源Hook位置保留供后续 | 已实现 `extensions/ingest/reconcile.go`、`http_source.go`、`observer.go`及测试，`extensions/weknora/client.go`；Host待接线，不加来源侧数据库表 | IN-1组件：分页/快照幂等/陈旧/冲突、SQL回滚、临时DB重开；真实来源周期恢复待验 |
| M0-03B 单写者 | 冻结4.1–4.4 | 已实现 `extensions/store/sqlite.go`、`write_executor.go`、`migrate.go`及测试；扩展独立依赖锁定 | DB-1组件通过，1000真实并发INSERT；CC-1混合场景待06B |
| M0-03C 事实事务 | 不变量A、4.5/4.10 | 已实现 `extensions/store/migrations/001_lifecycle.sql`、`extensions/lifecycle/store.go`、`store_test.go` | TX-1组件通过：回滚/重建/FK/重复键；M2正式政策另做 |
| M0-03D Outbox | 4.6–4.9 | 已实现 `extensions/outbox/store.go`、`worker.go`及测试、`extensions/integration/outbox_recovery_test.go` | OB-1存储/worker/进程恢复通过；实际远端投影待04B |
| M0-04 标签 | `internal/handler/knowledge.go:2662–2795`、`internal/application/repository/knowledge_tag.go:13–50`、C-10 | 已实现 `extensions/taggov/writer.go`、`http_store.go`、`mapping.go`、`projector.go`及测试；M0不加上游CAS | TG-1组件：两库映射/分页/最新truth/merge/错误重试；C-10无标签首写、原生并发覆盖及真实服务待验 |
| M0-05 MCP | 原生 `internal/mcp/`、`internal/agent/tools/mcp_exposure.go` | 新增 `extensions/mcp/server.go`、`auth.go`、`server_test.go`；原生配置注册 | MC-1；C-08 |
| M0-06A 独立门禁 | 根 lint matrix 未覆盖 nested module | 新增 `extensions/scripts/check_module_boundary.py`、对应测试；`.github/workflows/extensions.yml` | CI-1，本批 |
| M0-06B 集成验收 | 上述真实源调用点及受控补丁清单 | 新增 `extensions/integration/bridge_test.go`、`ingest_recovery_test.go`、`concurrency_test.go`；受控包补接入测试 | FC-1/IN-1/CC-1/TX-1/OB-1；按2.1限定M0范围，组件测试不替代完整M0 |

上表摄取文件的完整路径、方法及行号见 `PROJECT_CONTEXT.md` 的只读映射。实际实施前重验调用链；候选表不构成擅自扩大核心改动的授权。

## 5. 可执行验证方法

### FC-0：本批 Host 协议边界

在 `extensions/` 运行 `go test -mod=readonly ./bridge -run 'TestBridge' -count=1 -v`：两个存活探针200、骨架readyz503；四个POST缺版本/未知版本/多值版本400，版本1但服务未配置503。ingest未持久提交不能返回2xx，before_retrieve没有治理服务不能许可。错误只返回固定code。**这不证明原生编排已停止检索。**

### FC-1：端到端 Fail-Closed（核心接入后）

使用隔离上游测试库，在实际 RAG/Agent 编排注入可计数检索和 LLM 适配器。通过真实 Bridge transport 制造：Host退出、超时、500、2xx非法JSON、错误版本、缺少决策字段、scope扩大。断言请求明确失败、HybridSearch/entity调用计数为零、未进入模型fallback、无知识内容或新引用进入上下文。恢复Host后合法请求才允许检索。RAG/Agent均测，不能用Host单测代替。

### FC-2：空集合、交集、权限

准备两个KB、两个租户、active/superseded/quarantined文档和tag/mention限制。eligible与原生授权取交集；空集合显式拒绝或转为document类型空目标，不能回退whole-KB。检查entity/FAQ/wiki及旧字段降级路径；扩展返回额外ID不能扩大权限。契约fixture在独立模块及未来核心适配中分别解码。

### FC-3：Agent直接读与中途停用

第一轮获取active文档handle，第二轮前停用；分别直接读、图谱查询、再搜索。完整治理目标下应按批准刷新语义阻止非active内容，并验证旧handle、跨租户同ID及多个scope union不能绕过。用户已将直接读/图谱等完整治理后移，此项作为后续验收，M0只验排除搜索；不得以M0通过宣称FC-3通过。

### FO-1 / GD-1：增强失败与指导预算

Host超时/错误时 after_retrieve 保留原结果，before_generate 保留原消息；ingest不能套用fail-open。成功注入每轮≤3条、每会话同guidance≤2次、总token≤800。传输/溢出重试不重复追加或扣预算；不回写当轮事实与索引。

### DB-1 / CC-1：并发写入（必须真实数据库）

1. 临时独立 `agentrag_ext.db`，不访问真实data。先验证SQLite驱动与平台；当前工作机CGO_ENABLED=0且未发现gcc/clang，上游Lite运行需另备CGO环境。
2. 在WriteExecutor dedicated connection实际读取连接级PRAGMA：WAL、synchronous=NORMAL、busy_timeout=5000、foreign_keys=ON等；其他连接也验证各自设置。
3. 并发调用方提交1000个不同feedback writes，交错生命周期迁移和投影。只有WriteExecutor写ext_*；等待每个结果，校验精确计数、唯一键、event/current重建一致，无吞错及未处理SQLITE_BUSY/constraint failure。
4. 集成场景同时通过真实上游API ingest两份固定文档（concurrency=2），等待真正终态再验ext数据/Outbox。若上游竞态，保留日志/计数/commit，按冻结流程降至1重跑；不改原生并发算法。
5. race detector仅在有CGO/C编译器的Linux执行。分别记录ext单库并发、上游ingest+ext混合并发证据，前者不代表后者通过。

### TX-1：事务与事实重建

在每条SQL前后注入失败，event/current/outbox必须全有或全无；插入不存在event的last_event_id必须触发FK拒绝。提交后只用events重建current并逐项比对。同来源幂等键重复不再迁移，同键不同payload可观察地拒绝。M3/M5将governance/audit/Critical Edge纳入同一故障矩阵。

### IN-1 / OB-1：来源与投影恢复

- M0来源：核心提交后、Hook发送前退出，重启后经周期公共API对账恢复当前状态；验收在配置的interval+扫描时间窗口内收敛。用多阶段变化证明中间历史可能不可恢复并如实标记；不要求M0改原生数据库。
- 接收：仅ext事务持久提交后ACK；响应丢失后按稳定 `source_event_id + document_id + stage + attempt` 去重。
- 投影：Outbox提交后、外部投影成功ACK前分别终止worker；重启继续pending，过期processing lease可回收，无永久orphan；健康时oldest_pending_age<30s，dead可查错/人工retry。
- 覆盖解析/分块/索引失败、重试成功、取消/替换旧attempt、异步最终完成、耗尽及housekeeping；BatchIndex成功不等于全部摄取完成。

### TG-1：标签并发覆盖

M0使用一个Writer实例，1000个并发调用方执行完整read/merge/replace/verify，断言API最大并发=1、成功写入计数=1000、业务标签和其他gov命名空间不丢，失败传回调用者供Outbox重试；写入原生tag ID，不把标签名称当ID。

另保留已知限制案例：扩展读 `{business:A}` → 原生UI加B → 扩展写 `{business:A,gov:state:active}`，仍可能丢B；用户已接受M0用扩展队列防范而不加CAS。此限制不能被“1000并发扩展测试通过”掩盖。跨库同名异ID、回读不一致、外部API和持久Outbox接线继续验收。

### MC-1：原生MCP注册/认证

临时租户配置HTTP Streamable `/mcp`；首轮仅发现入口，按需发现后才暴露schema。无/错误token拒绝，stdio创建拒绝；先验证未allowlist回环地址被阻止，再仅放行所需主机并重测。不关闭SSRF，不以自建工具列表代替原生发现链路。

### CT-1：检索前公开契约与HTTP边界

共享JSON位于`contracts/bridge/v1/`；扩展contract测试验证冻结字段名、agent/多KB上下文和metadata大整数保持，以及省略/null与显式空文档范围的区别。typed handler真实HTTP验证排除后的显式子集、关联/作用域、全排除deny_all、跨tenant/KB拒绝、错误/坏策略/取消503、超限413且不调用策略、默认及未配置Hook仍503。策略修改传入切片不能扩大响应；停滞body通过真实TCP连接验证5秒读取期限与拒绝后正常请求恢复。

Implementation Difference：当前检索前请求最大1MiB、1000 KB/10000显式候选、JSON嵌套64层；未满足约束必须拒绝，不能截断候选或whole-KB降级。tag_scopes按KB保存原生tag ID，metadata使用原始JSON。该批不实现检索后results[]契约/生成前guidance/摄取持久ACK；不引入新的治理状态规则。原生消费者未创建，因此不是两module契约验收，更不是原生端到端Fail-Closed证明。C-09候选完整枚举、原生目标分组及大库请求限额须在后续薄适配POC中核实。

### CI-1：独立模块检查

在 `extensions/` 执行 `python scripts/check_module_boundary.py`、`python -m unittest discover -s scripts -p '*_test.py' -v`、Go build/test/vet及只读gofmt。门禁实际调用Go module/import查询；临时fixture验证合法通过、错误module名、WeKnora require/replace及workspace耦合拒绝。远端Actions未运行时必须单列未验证。

## 6. 后续阶段验收索引

| ID | 验收方法 |
|---|---|
| PA-1 | M1：人工标注PDF/Image扫描/乱码/多栏/表格夹具；预检不创建parse job，正式job计数=1；Office不进四档；按库准确≥85%、高估≤10%、低估≤15%；保存artifact格式/schema/MinerU版本。真实服务另验。 |
| RE-1 | M1.5：每库30–50查询，Recall@5/10/20、MRR对比；RRF仅调既有租户参数；量化flag关闭不改基线，开启时真实vec_quantize_i8和Recall@10退化≤2%均实测。 |
| LC-1 | M2：正式迁移矩阵含scheduled；非法迁移不写事实；时间推进、重放、Document/Guidance stale分测；继承TX-1/OB-1。 |
| LN-1 | M3：混合边夹具分别验证两个API白名单、方向、tenant/KB、环/深度；版本族单事实源；Critical Edge崩溃后原子一致。 |
| AS-1 | M4：每库20–50标注对校准，precision≥0.70/recall≥0.60；15–20多跳问题/库，3–4轮；GraphRAG默认关闭，预算获批再开，超150%预算或提升<3个百分点回退。 |
| RV-1 | M5：远端创建提交但响应丢失，恢复先reconcile，已存在时create计数保持1；reconcile未知失败不能再次创建。审计/关键边同事务，冷静期/二次确认/抽检/熔断/生成质量回归；binary_source不自动改正式源文件。 |
| DS-1 | M6：TXT只含允许字段，无KB元数据；能力查询认证；静态回退；UI库切换/权限/两种溯源入口分离；运维显示真实outbox/dead/预算。 |

## 7. 本批验收记录

第一批（2026-10-29）：用户确认2.1的M0取舍，交付协议、对账调度、1000次串行标签更新和排除集合组件，以及模块边界门禁和专用CI定义。组件尚未接通实际公共API与原生Hook。

第二批（2026-10-30）：M0-03B/C/D持久组件完成。纯Go SQLite在本机CGO_ENABLED=0可构建/运行；三条同时占用的reader实际验证连接级配置及写拒绝，writer在无DML时已持有IMMEDIATE写锁且reader可读。1000并发真实SQLite INSERT精确计数1000，逐条收集错误；它们使用feedback探针表，不代表M5反馈业务或CC-1上游混合场景通过。

TX-1组件：event/current/outbox各SQL处触发器中止后全无；事件重建current一致；重复身份幂等、变更payload/stale from_state拒绝；缺失与跨scope event引用被FK拒绝。Outbox组件：100并发认领仅1活租约，失败重试/dead/人工分scope retry及统计通过；临时子进程在提交前/后及领取后直接退出，重开真实文件验证回滚、pending恢复、过期lease回收和旧token拒绝。健康小样本Worker.Run在3秒测试预算内排空，不声明生产负载SLO。投影Handler须按当前truth幂等收敛，不能依赖lease约束远端副作用。

独立只读复核发现并修复Handler自身超时无法累计失败的Important问题；新增真实deadline测试完成RED→GREEN，旧token回归和完整suite通过。Go test/build/vet/gofmt及Python实际门禁/7例回归通过；根依赖/原件hash保持一致，原生源码diff为空。

以上为第二批时点记录；后续组件进展见下文，真实服务/Host/核心门禁未因组件测试而解除。

第三批（2026-10-30）：03A/04B公共API与持久组件交付。客户端固定origin/tenant、拒绝重定向、超时/4MiB响应上限/错误脱敏；知识与标签两种原生分页格式、scope及目录完整性已测。schema v1→v2保留数据；对账幂等/陈旧/冲突/两处SQL故障及重开数据库通过，未生成治理active。标签解析当前truth发生在同一个Writer任务内，跨库原生ID持久映射，旧Outbox payload不会降级事实。

`integration/public_api_test.go`使用真实临时SQLite和原生格式httptest接口，串起对账/重开DB、HTTP失败→回读不一致→null拒绝→重试成功；保留普通/其他gov标签，三次失败均持久记录，最终队列排空。它不是运行中的真实WeKnora服务验收。C-10按2.2保留。

独立复核发现客户端包受Windows上游binary忽略规则影响，新增`extensions/.gitignore`精确例外，实际Git枚举已能看到两个client文件；裸? origin校验补RED→GREEN，有效超限JSON测试精确验证size-limit错误。构建/test/vet、模块边界及7例回归均通过。

以上为第三批时点；Host本地接线进展见下文，真实服务及核心接入仍未验收。

第四批（2026-10-30）：Host配置与生命周期接线完成。默认probe不访问DB/上游；显式worker先验证配置、绑定固定127.0.0.1:8090，再打开独立DB并启动即时/周期对账及scoped Outbox。多KB共用唯一Tag Writer；认领在事务内限制tenant/KB，未配置范围的pending及过期processing保持原状。配置/错误/日志不输出凭据或KB元数据。

### HO-1：Host本地验证

`extensions/host/config_test.go`验证默认/完整配置、无效origin/tenant/重复KB/时长/混用参数拒绝和secret脱敏。`outbox`新增scoped claim/Worker测试，空范围拒绝，外部范围包含过期lease也不认领。`host/runtime_test.go`实际监听回环地址，使用临时SQLite+双KB原生格式HTTP：来源首次503后周期收敛、两项投影完成、标签API最大并发1、三个外部范围任务不变；取消后数据库可重开、端口可复用。坏配置/外部DB/预取消/端口占用/非回环地址/Serve退出均不遗留Host资源。

命令入口实际以probe运行8090，独立HTTP检查退出0：healthz=200、readyz=503、版本1 before-retrieve=503；Ctrl+C打印host_stopped后终止，go run终端包装进程因中断返回1。ShutdownTimeout仅限制HTTP排空；后台取消后仍等待Writer/DB事务清理，不强杀事务。一次独立只读复核无实质问题；完整11包Go测试、build/vet、gofmt、Python实际边界门禁及7例回归通过。

尚未验收：业务DTO/原生Hook/MCP、IN-1真实来源重启周期对账、C-10无标签首写、CC-1真实上游混合并发、实际远端副作用恢复及生产负载积压、Go1.26运行、远端Actions/Linux race。readyz和业务Hook保持503，M0父任务仍进行中。完整命令与退出状态见TASK_BACKLOG本批执行记录。

第五批（2026-10-30）：用户接受C-10安全拒绝为Baseline Limitation，后续独立标签只读API解决，本批不改原生API。完成M0-02B扩展端检索前公开DTO、共享JSON和typed handler；CT-1测试通过。严格解码/handler先因实现缺失失败后通过；停滞body测试先在客户端7秒deadline失败，配置5秒请求读取期限后通过。完整12包Go测试、build/vet、gofmt、244 imports模块门禁及7例Python回归通过。生产Host继续不配置策略，其他Hook/readyz继续503；持久策略及原生消费/接线留后续，父任务未完成。

第五批独立只读复核无实质问题，contract/bridge独立重跑退出0；范围裁定逐项保存在TASK_BACKLOG。核心消费者、原生授权/目标分组、大库枚举、真实策略期限及其他Hook/真实服务/平台验收均未因本批通过而解除。没有未修复的minor项。

### 最新进展：用户确认的M0范围已验收（2026-10-30）

后续已接通原生HTTP门卫；早先真实E2E的通网503由用户批准的默认Pass-Through解除。runtime为probe/worker配置真实handler，合法非空原生授权候选原样通过，未知/空范围拒绝，不新增治理排除或改写核心算法。复核发现空范围200+deny_all会被状态门卫忽略，已通过失败回归后改为默认分支403。

修复后真实WSL E2E退出0：断网指定门卫日志、SSE错误与零答案生成；通网实际Host收到同一请求并返回答案/引用/完成。完整报告见`docs/evidence/m0-e2e-2026-10-30-pass-through.log`。使用隔离SQLite副本和本地模型夹具，不代表生产模型验收。完整十二包测试/build/vet及本次bridge/host Linux race退出0；既有完整race的Outbox计数断言失败用户接受后移，未修复。

TASK_BACKLOG已将M0按本次HTTP门卫+默认透传范围标记完成，M1待开始；原Frozen剩余治理/Hook/MCP及C-09/C-10等限制保留后续，以上历史记录不作当前状态读取。完整范围裁定和未验证项见最新执行记录。
