# M3 可信来源基础与 M2 联合验收

状态：用户已选择“连同M3可信来源基础一起实施，再验收M2”，并明确批准本文T1/T2。原生快照与扩展观察版本已实现，真实隔离Core/Host E2E通过；T3～T5仍按后续具体接线审批推进，不能据此标记M2整体验收。

基点：`fa6353e8c821daa5eeb667dc46140e4d0d32f179`，`v0.9.0-2-gfa6353e`。不改Frozen、原生表或检索/解析/任务算法，不升级根依赖，不重启或迁移真实实例，不提交推送。

## 选择与目标

用户选择同时补齐M3可信来源基础；此前“只完成M2并延期来源”及“单纯关闭能力”不作为本次交付路径。

目标分三道门禁：

1. 原生当前一致快照 → 扩展不可变版本/版本族事实 → 每次使用重新核对当前证据。
2. 显式补充通知依赖 → 权威同库血缘与生命周期联动；缺来源、来源变化、失效均拒绝，不能把异步derived_from当授权事实。
3. 可信运行协调层累计实际模型输入 → 完整封存源集 → 副本读门卫与生成期间撤销联合验收。前两项不能冒充第三项已完成。

范围不含M4语义关联/图谱召回、M5修订发布Saga或外部设备/云供应商已收到数据的撤回。人工下载与旧grant是否接管仍需独立确认，不暗中改变现有授权。

## 新冲突（重要）

| 编号 | 源码事实 | 解决方案与边界 |
|---|---|---|
| C-33 | Knowledge没有整文档单调generation。Chunk.ContentRevision只对单chunk单调；修改chunk不保证同步更新Knowledge.FileHash/UpdatedAt，摘要/重建索引亦非同一整文档事务。 | 原生新增只读事务快照指纹，不伪造原生版本号；ext_document_version存不可变已观察版本。来源账本NativeSequence改为明确的ObservationSequence，表示扩展观察顺序；串行采集登记，不宣称恢复未观察历史。若要求原生不可漏的完整写历史，必须另批所有写点的generation维护，超出本方案。 |
| C-34 | 快照接口返回root后，再独立读取/合并内容，可能取得另一状态。RAG Merge会换父正文、补邻居；只按DocID许可不能证明实际模型字节。 | 在实际对象读取后、外送模型/重排前，对原始字段指纹逐项核对当前快照leaf，携带已验证root和完整组成来源经过变换。无法证明组成的结果拒绝；不能把CitationSources直接称完整来源。 |
| C-35 | 定期同步版本后只查扩展head，无法及时发现两次同步之间的原生编辑。 | 恢复副本/发放来源许可时重新查询原生快照并核对root；不可用/超限/错误/错scope拒绝，无allow缓存。只读快照检查后的新变化属于后续持续撤销机制边界。 |
| C-36 | DB文本指纹不能证明对象存储图片、文件或外部URL的当前字节；资源URL/hash也不能证明完整语义来源。 | 视觉/二进制恢复需要实际字节hash和不可变制品绑定；缺证据拒绝相应恢复，不按路径/mtime放行。不把用户独立提供内容伪装成受治理知识来源。 |
| C-37 | Harness的007只守卫后续current写入，不追溯修复旧异常投影。旧active与所指事件不符、头落后于新事件，曾继续放行；有历史但无current还可能被当新文档初始化。 | 007保持不动。扩展共享读取验证状态/最新事件；有历史缺projection返回显式异常，不自动初始化/修复。检索和副本许可拒绝，需操作员从事件日志审核重建；三类损坏均有RED→GREEN临时库回归。 |

## 第一部分：最薄原生查询接口

仅修改两个既有原生文件，新增一个实现文件及相应测试：

| 文件 | 修改 |
|---|---|
| `internal/handler/knowledge.go` | 给KnowledgeHandler与NewKnowledgeHandler增加现有DI的`*gorm.DB`；不改现有方法 |
| `internal/handler/extension_source_snapshot.go`（新） | 授权后同一只读事务查询tenant+KB+document及全部live chunk，固定结构规范化SHA256；只返回身份和指纹，不返回正文/标题/描述/元数据原文 |
| `internal/router/routes_knowledge.go` | 在现有kbRead组新增Viewer+KB read查询；handler内再次执行既有KB/API-key范围授权 |
| `internal/handler/extension_source_snapshot_test.go`（新） | 原生鉴权/错scope/软删除/并发写/无写操作/完整指纹/超限拒绝 |

路由：`GET /api/v1/knowledge-bases/:id/knowledge/:document_id/source-snapshot`。

骨架diff（仅设计，尚未应用；省略既有构造参数与错误响应细节）：

```diff
--- a/internal/handler/knowledge.go
+++ b/internal/handler/knowledge.go
@@ KnowledgeHandler
+ sourceDB *gorm.DB
@@ NewKnowledgeHandler
+ // Receive the existing *gorm.DB from the current container.
+ sourceDB: db,
--- a/internal/router/routes_knowledge.go
+++ b/internal/router/routes_knowledge.go
@@ kbRead
+ kbRead.GET("/:document_id/source-snapshot", g.Viewer(),
+     g.KBAccessRead("id"), handler.ExtensionSourceSnapshot)
--- /dev/null
+++ b/internal/handler/extension_source_snapshot.go
@@
+ func (h *KnowledgeHandler) ExtensionSourceSnapshot(c *gin.Context) {
+     // Existing native authorization determines effective tenant and KB.
+     // One read transaction queries exact scope and hashes explicit fields.
+     // Return fingerprints only after successful transaction completion.
+ }
```

Hash scheme固定为`agentrag-native-snapshot-v1`。文档模型可见字段使用显式结构编码；所有live chunk类型都纳入，不只text；按chunk_index/id稳定排序。leaf覆盖正文、类型、revision、enabled/index状态、ContextHeader、Metadata、ImageInfo及关联/定位字段；root覆盖文档header及完整leaf列表。无chunk或不满足可用parse状态不能冒充完整成功；不截断列表返回成功。输入/输出限制与超时拒绝必须有定向测试。

v1公开DTO固定为scheme、tenant_id、knowledge_base_id、document_id、header_fields（字段名→SHA256）、header_hash、chunks完整数组、root_hash。每个chunk leaf为id、content_revision、chunk_index、content_hash、fields_hash。content_hash使用正文原始UTF-8字节SHA256；其余字段摘要使用显式字段值的Go JSON编码，序号为有界整数、定位BBox为有限浮点的Go JSON表示、nil为null、时间统一UTC/RFC3339Nano，JSON型原生字段转换为原始byte slice再编码（保留原字节，非语义JSON解析）。header_hash使用按字段名排序的name/hash列表；root_hash使用固定结构的scheme/scope/doc/header_hash及完整有序leaf列表编码。两模块用共享JSON样例分别重算，不能import核心类型。

Header字段白名单：type、title、description、source、channel、parse_status、summary_status、profile、enable_status、file_name、folder_path、file_type、file_size、file_hash、metadata、custom_metadata、created_at。chunk fields_hash覆盖content、source_content、content_revision、index_status、chunk_index、is_enabled、flags、status、start_at、end_at、pre_chunk_id、next_chunk_id、chunk_type、parent_chunk_id、relation_chunks、indirect_relation_chunks、metadata、content_hash、image_info、context_header、source_locators、created_at；不对投影标签或仅更新时间签版本。这个白名单变动须新hash scheme，不能默默变更v1含义。

实施默认限额：最多4096个live chunk，参与hash的源数据合计最多64MiB，返回JSON最多2MiB，原生快照查询最多500ms；超限整体拒绝，无截断成功或只签部分chunk。completed之外状态拒绝；索引processing/failed不当作稳定可读证据。这些是实施者选定的保守限额，超限后先评估容量，不能放宽完整性。

SQLite同一事务连接；PostgreSQL显式repeatable-read。`ReadOnly:true`不等于所有驱动都会强制禁写，测试验证本方法只执行查询，不能借此执行迁移或初始化原生表。

## 第二部分：扩展版本权威与补充依赖

新增`extensions/lineage/`与下一顺序迁移（当前最高7，实施采用008）。事实沿用Frozen命名：`ext_lineage_node`、`ext_document_version_family`、`ext_document_version`、`ext_lineage_edge`，另设快照证据/观察头/幂等回执和审计。全部作用域复合FK；版本、family head、审计同事务，CAS及冲突幂等键拒绝。

- 新版本只表示可信原生接口实际观察到的一致快照；相同当前root幂等，变更生成新版本与递增观察序号，不把最大chunk revision/时间戳当顺序。
- 默认每个原生文档先有独立family，普通编辑只推进观察head，不自动创建规范性supersedes/merged_into边、不把文档自身转superseded、不伪造Revision Published。规范性替代继续要求独立治理事务。
- `SnapshotReader`只调用配置中固定的Core origin与既有KB范围机器凭据；拒绝重定向，校验scheme/scope/doc/完整性与大小。没有真实provider时禁止生产降级到测试夹具。
- 采集开始至登记提交必须跨进程串行，不能只用内存锁。最小实现由扩展BEGIN IMMEDIATE取得写许可后执行有界只读快照请求，再同事务登记；请求失败回滚，不用旧响应重试。该受限HTTP查询可能占用写者至500ms，需测并发容量及超时失败关闭；后续若改为租约fencing须单独证明旧响应不能推进head。原生只读快照与扩展提交不在同一事务，不宣称跨库原子历史。
- 008保留schema6所有source/manifest/引用/回执，不改写已发布006 DDL或重命名其物理列。Go的NativeSequence改为ObservationSequence，旧native_sequence列仅作历史存储名；新权威记录必须关联真实快照证据。旧行没有新证据时保持未核验，生产Authority不得自动提升为可信，不按DocID/已有receipt认领。schema7升级和schema6→8升级分别测试保留事实与旧证据拒绝。
- 来源账本通过真实版本权威适配获得SourceProof；读取时新鲜核对当前root，并在最终扩展一致快照中再次比对当前版本。缺权限、版本变化、Host/Core不可用均拒绝。
- `derived_from`仍为可重建异步投影；`/provenance`仅derived_from，`/version-ancestry`仅supersedes/merged_into，查询分别限制边类型、scope、深度与节点数，恶意环不能无限遍历。
- 补充通知是独立文档，不并入父版本族；同库显式`supplements`经原生双文档权限核验后持久化。边类型决定同步级别，调用者不能降低；声明/审计/相应治理事实同事务。
- 父失效、过期或已引用版本不再当前，子文档许可拒绝；周期任务将符合条件的子文档转stale。决策事务重读父/边/子版本，不能依赖先前查询后无条件迁移。环与跨tenant/KB默认拒绝；不自动恢复失效子文档。

## 第三部分：封存与持续撤销的后续门禁

完整运行源集不是第一部分接口能证明的事实。必须在原生已批准文档门卫、最终RAG上下文、Agent实际输入及产物登记处累计已核对片段；并发任务未结束不封存。未知的shell输出、自动Recall、后台通知、Web快照和历史副本不能自行声明完整。新patch清单必须逐点列出，保持原生权限和业务算法，不重构执行器或事件总线。

流中撤销与Frozen before_generate guidance fail-open分离，采用独立来源许可。拟定检查周期1秒、单次HTTP最多500ms；此值是实施建议，不是尚未测得的验收承诺。撤销取消context，丢弃受影响解码尾部、禁止自动重试和后续模型调用；已发给用户/供应商的数据无法收回。最终可承诺的停止上界须以隔离E2E中生命周期提交T0与最后受控输出实测为准，不能写“立即”。

## 实施与验证顺序

| 顺序 | 交付及主要路径 | 门禁 |
|---|---|---|
| T1 | 上述原生快照接口/测试 | 授权、错scope、chunk编辑改变root、元数据改变root、并发一致性、零原生写入、无原文泄漏 |
| T2 | `extensions/lineage/{versions,snapshot,authority,edges,query}.go`及008迁移/测试 | 16并发单赢家、真实SQL失败/进程退出回滚、复合FK、观察重放、当前root变化拒绝；schema6/7升级保留事实，旧无证据版本不升格 |
| T3 | Host认证/版本适配、原生管理薄转发、supplement lifecycle/策略/UI | 缺凭据/跨scope/伪造证据/错版本/父失效/环/并发改边均拒绝，边/治理/audit同事务 |
| T4 | 运行输入封存/模型副本读取薄点清单及定向测试 | 真实输入指纹、完整继承源集、错hash/身份/分页缓存/旧副本/Host断开拒绝；原生补丁逐点核对 |
| T5 | 流式取消门卫与隔离E2E | 不flush敏感尾部、不重试、RAG/Agent/连接等待/双流/Host503/多会话逐项测量 |
| T6 | 全扩展/原生定向与race、前端全测/type/build、Lite构建、联合E2E | 全部命令与退出状态记录；未过门禁保持M2未验收，不以局部通过代替整体 |

T1/T2已批准并实施；候选二进制未安装，真实数据库未由本任务迁移。T3～T5在前置证据成立后再把精确补丁写入实施计划；发现无法通过薄点覆盖的新入口及时报告，不以大改原生硬凑。验收与复核证据见`../evidence/m3-trusted-sources-2026-11-01.md`。

独立builder复核已完成；提出的四项Important（指纹契约、观察与替代语义、跨进程采集顺序、旧证据迁移）均补入本文。不是产品实现验收。
