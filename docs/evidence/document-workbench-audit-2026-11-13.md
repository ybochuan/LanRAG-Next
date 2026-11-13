# 文档工作台全面审计与优化建议

- 日期：2026-11-13
- 审计对象：`frontend/src/views/lifecycle/LifecycleView.vue` 及其版本族、关系、血缘、业务治理子视图；`extensions/management` 的 overview/list 读面；`extensions/business` 的 family workspace、relation/feedback 读面；`extensions/review` 的待复核 inbox。
- 审计基线：`git HEAD 8d28e71`，工作区包含未提交的跨库关系、跨库 Graph、关系标签 Outbox、跨库 supplements 改动。本报告只做审计与建议，不修改业务代码。
- 结论：工作台主流程可用，但存在 **2 个 P0 数据一致性/跨库可见性问题**、**4 个 P1 回归风险/性能问题**，以及若干 P2 交互和测试缺口。建议先修 P0 读面，再补跨库 family workspace 与内嵌视图的知识库传递，最后做性能与 UX 收口。

## 1. 执行摘要

| 级别 | 问题 | 主要影响 | 建议 |
|---|---|---|---|
| P0 | 生命周期 counts/list 未使用 verified current | 损坏或未核验的 `ext_lifecycle_current` 会进入统计和列表；单篇 snapshot 失败会让整个 overview 503 | counts/list 统一加 `LifecycleEventJoinSQL + LifecycleCurrentVerifiedSQL`；不一致时明确 `degraded` 或 503 |
| P0 | 跨库关系在版本族/工作台读面不可见或显示为历史 | 跨库 `amends/interprets/related_to/supplements` 无法在“当前关系”、活动时间线、`relation_list` 正确展示 | 建立带 `source/target_knowledge_base_id` 的统一关系读模型；union cross 表；补跨库 family workspace 测试 |
| P1 | 内嵌视图的 `knowledgeBases` 为空 | 版本族内 `SupplementEditor` 父知识库下拉无选项；抽屉内 `CreateForm` 目标知识库下拉无选项 | 由 `LifecycleView` 统一传知识库列表；`loadCatalog/loadKBs` 在 scope 模式仍加载目录 |
| P1 | 跨库目标文档的打开/查询/标题解析丢失 KB | 跨库关系记录、图节点、目标文档详情打开到错误 KB；标题显示 ID | `open`/`inspectRelated`/`documentTitles` 全部携带 KB，使用 `kb+id` 复合键 |
| P1 | overview 的 N+1 snapshot 与标题 fallback 全表扫描 | 大库首页延迟高；按标题搜索时内存/响应体风险 | 批量 annotations；标题搜索下推 SQL 或限制候选并返回 degraded |
| P1 | 错误一律进入“必须重新载入”状态 | 409 与 403/网络错误不区分，用户无法重试 | 按 HTTP 状态处理；只有冲突才锁表单 |
| P2 | 表单无 dirty 保护、待复核行无标题、truncated 未重置 | 未保存内容丢失、可读性差、提示残留 | 统一 dirty guard + 标题批量加载 + 状态重置 |
| P2 | review inbox 未校验 lifecycle/availability | 归档/删除文档的旧提醒可能出现在待复核 | 加 verified current + availability 过滤，补回归测试 |

## 2. P0：生命周期读面门禁不一致

### 证据

- `extensions/management/read.go:91`：

```sql
SELECT state,count(*) FROM ext_lifecycle_current
WHERE tenant_id=? AND knowledge_base_id=? GROUP BY state
```

- `extensions/management/list.go:36-44` 的 native 分支直接：

```sql
FROM ext_lifecycle_current c
LEFT JOIN weknora.knowledges k ...
```

- `extensions/management/list.go:56-66` 的 fallback `extOnly` 同样直接读 `ext_lifecycle_current`。
- `extensions/management/read.go:120-127` 对当前页每篇文档调用 `m.Snapshot(...)`；任意一篇失败直接返回 `errDatabase`，整个 overview 变成 503。

`extensions/store/lifecycle_current.go` 已定义：

- `LifecycleEventJoinSQL`：把 `ext_lifecycle_current c` 与 `ext_lifecycle_events e` 按 `last_event_id` 对齐。
- `LifecycleCurrentVerifiedSQL`：要求 `c.state = e.to_state` 且不存在同文档更高 `sequence` 的新事件。
- `ReadLifecycleCurrentTx` 在 current 与 event 不一致时返回 `ErrLifecycleInconsistent`。

### 影响

1. 统计数字与列表可能包含未核验或已损坏的 current 投影；详情页却因 `Snapshot` 核验失败而不可读。
2. 单篇损坏会让整个 overview 503，而不是只降级该行。
3. counts 与 `overviewPage` 是两个独立读事务，生命周期快速变化时计数与当前页可能不一致（次要但真实）。

### 建议

- 在 `read.go` 的 counts 查询加入 `store.LifecycleEventJoinSQL` 与 `store.LifecycleCurrentVerifiedSQL`；对未知/不一致状态单独计数或返回 `degraded_reason`。
- 在 `list.go` 的 native 分支和 fallback 分支都加入同一 verified 条件；fallback 不应绕过核验。
- 对 `m.Snapshot` 失败做逐行容错：返回该行 `state` 为不可用/`degraded`，不要让整页失败；或者先批量读取 annotations，只有详情页才做完整 Snapshot。
- 补 `extensions/management/read_test.go` 测试：插入 `ext_lifecycle_current` 与最新 event 不一致的 fixture，断言 overview/list 不泄漏该状态、返回可解释的 degraded 或 503；断言一页中单篇损坏不拖垮其余文档。

## 3. P0：跨库关系在版本族/工作台不可见

### 证据

- `extensions/business/family_workspace.go:24-37` 的 `WorkspaceRelation` 只有 `DocumentID`/`TargetDocumentID`，没有 `source_knowledge_base_id`/`target_knowledge_base_id`。
- `extensions/business/family_workspace.go:239-278` 的 `workspaceScope`：
  - `records` 的 `relationRecords JOIN scope USING(tenant_id,knowledge_base_id)` 只取源 KB。
  - supplement 分支只 `JOIN ext_supplement_parents p`，没有 `ext_supplement_cross_parents`。
  - `incident` 的 `ext_document_version_family f/g` 只按 `scope.knowledge_base_id` 关联，跨库目标版本无法验证 `current`。
- `extensions/business/family_workspace.go:82-122` 的 `documentActivitySQL` 只按源 KB 的 relations/revisions/suggestions/replacements/supplements 查询。
- `extensions/business/feedback.go:349-360` 的 `documentRelationRecords` supplement 分支只 `ext_supplement_parents`，未 union `ext_supplement_cross_parents`；`relation_list` 在文档工作台漏跨库补充关系。
- `extensions/business/related.go:363-368` 的 `relatedSQL` 已支持跨库 supplements，但补充分支 join 只按 `declaration_id`/`document_id`，缺 tenant/KB 冗余约束。

### 影响

1. 从目标 KB 打开文档的版本族时，看不到其他 KB 指向它的 incoming `amends/interprets/related_to`。
2. 从源 KB 打开文档时，跨库目标文档不会进入成员候选，`current` 可能恒为 `false`，显示为“历史记录”。
3. 跨库 supplement 在版本族“当前关系”和活动时间线缺失；`relation_list` 可能只显示同库 supplement。
4. 跨库目标文档的标题取不到，前端回退显示短 ID。

### 建议

- 为 `WorkspaceRelation` 增加 `SourceKnowledgeBaseID`、`TargetKnowledgeBaseID`（至少 `TargetKnowledgeBaseID`），API/前端类型同步。
- 建立统一关系读模型：`workspaceScope` 的 `records` 同时 union：
  - 同库 `relationRecords`；
  - 跨库 `ext_document_relations`（按 `knowledge_base_id = scope.kb OR target_knowledge_base_id = scope.kb`）；
  - 同库 `ext_supplement_parents` 与跨库 `ext_supplement_cross_parents`；
  - 每条记录携带 source/target KB。
- `incident` 的版本校验分别使用 source/target 的 `ext_document_version_family` join（`f` 用 source KB，`g` 用 target KB），成员判断区分 `internal`/`external`：目标不在当前家庭成员时仍可显示为外部关系，而不是直接丢弃或恒 `current=false`。
- `documentActivitySQL` 的 relations/supplements 分支 union 跨库表，并按 source/target KB 过滤当前文档。
- `feedback.documentRelationRecords` 的 supplement 分支 union `ext_supplement_cross_parents`；`relation_list` 继续使用已验证的 `target_knowledge_base_id` 过滤。
- 补 `extensions/business/family_workspace_test.go`：源 KB 家庭可见 outgoing 跨库 `amends`/`supplements`，目标 KB 家庭可见 incoming，目标标题按目标 KB 加载，activity 可见对应跨库记录。
- 修 `workspaceScope` 的 `LEFT JOIN ext_supplement_declarations latest ON latest.declaration_id=c.declaration_id`，补 `tenant_id`/`knowledge_base_id` 条件。

## 4. P1：内嵌视图知识库列表为空

### 证据

- `frontend/src/views/families/FamilyWorkspaceView.vue:254-273`：

```ts
if (props.scope) { kb.value=props.scope; catalogBusy.value=false; return }
```

`bases.value` 保持空数组。

- `FamilyWorkspaceView.vue:120-121` 把 `:knowledge-bases="bases"` 传给 `FamilyDocumentPanel`；`FamilyDocumentPanel.vue:41` 的 `knowledgeBases` 为必填 prop，内部 `SupplementEditor` 用于父知识库选择。
- `FamilyWorkspaceView.vue:172-173` 的 `CreateForm` 只传 `:kb="kb"`，没有 `:knowledge-bases`，即使 `bases` 有值也无法选择目标知识库。
- `frontend/src/views/business/BusinessView.vue:329-334` 在 `props.scope` 时同样提前返回，`knowledgeBases.value=[]`；`CreateForm`（:151-153）需要该列表来显示目标知识库下拉。
- `frontend/src/views/lineage/LineageView.vue:110-116` 在 `props.scope` 时同样清空 `knowledgeBases`，跨库节点只能显示 ID。

### 影响

- 版本族内嵌的 `SupplementEditor` 父知识库 `<select>` 没有 option，跨库 supplements 无法在当前文档抽屉中创建。
- 抽屉/内嵌 `BusinessView` 的 `CreateForm` 目标知识库下拉为空，新跨库关系无法从这些入口创建。
- 跨库图节点详情显示知识库 ID，而不是名称。

### 建议

- `LifecycleView.vue` 的 `<FamilyWorkspace>`、`<BusinessView>`、`<LineageView>` 统一传 `:knowledge-bases="knowledgeBases"`。
- `FamilyWorkspaceView` 增加可选 `knowledgeBases` prop；`loadCatalog` 在 scope 模式仍调用 `listKnowledgeBases` 填充 `bases`，但保留 `kb=props.scope`，不要覆盖外部 scope。
- `BusinessView` 增加可选 `knowledgeBases` prop；scope 模式同样加载目录或直接复用父组件传入列表。
- `FamilyWorkspaceView` 的 `CreateForm` 补 `:knowledge-bases="bases"`。
- 补前端测试：`FamilyWorkspaceView` scope 模式下 `knowledgeBases` 非空、`SupplementEditor` 有父 KB 选项、`CreateForm` 目标 KB 选项可用；`BusinessView` 内嵌模式同样。

## 5. P1：跨库目标文档的打开/查询/标题解析

### 证据

- `frontend/src/views/lifecycle/LifecycleView.vue:194-195` 的 `openGraphDocument`/`openRelatedDocument` 只接收 `id,title`，没有 KB；`openDocument` 使用当前 `kb` 调用 `api.documentDetail(scope,id)`（:198-203）。
- `frontend/src/views/lineage/LineageView.vue:56` 的 `open` 事件只发 `document_id,title`；`defineEmits<{open:[id,title]}>`（:74）无 KB。
- `frontend/src/views/business/BusinessView.vue:136-139` 的“查询源文档关联/查询目标文档关联”分别只传 `detail.document_id`、`detail.target_document_id`，没有传 `detail.knowledge_base_id`/`detail.target_knowledge_base_id`。
- `frontend/src/views/families/FamilyWorkspaceView.vue:345-350` 的 `documentTitles(scope,docs)` 只用当前 source scope；关系行含跨库目标时标题取不到。
- `frontend/src/views/business/BusinessView.vue:202` 的 `docName` 默认 `scope=kb.value`；部分记录标题调用未传跨库 KB。

### 影响

- 跨库图节点点击“打开文档详情”会在源 KB 查询目标文档，可能返回 404 或错误文档。
- 跨库关系的目标标题显示短 ID，记录详情“查询目标文档关联”落到源 KB，用户看到空结果。
- 如果两个 KB 有相同 document_id（历史导入/测试数据），标题和详情可能串库。

### 建议

- 统一 `OpenDocumentTarget { id, title, knowledgeBaseId }`（或事件第三参数）；`LifecycleView.openDocument` 接受可选 `scope`，详情读取使用 `detailScope`，主工作台 `kb` 保持当前上下文。
- `LineageView`/`BusinessView`/`FamilyWorkspaceView` 的 `open` emit 增加 `knowledge_base_id`；`BusinessView` 的 `inspectRelated` 显式传 `detail.knowledge_base_id` 或 `detail.target_knowledge_base_id`。
- 前端标题缓存统一使用 `kb + '\u0000' + id` 复合键；`FamilyWorkspaceView` 从 `documentTitles` 改为按 `row.target_knowledge_base_id` 分 KB 批量加载。
- 补测试：跨库图节点 open 事件携带目标 KB；跨库关系详情查询目标文档时请求目标 KB；同 ID 不同 KB 的标题不串。

## 6. P1：overview 性能与标题 fallback

### 证据

- `extensions/management/read.go:120-127` 对最多 20 篇文档逐篇 `m.Snapshot`；`Snapshot` 还会读取事件/策略等，首页成本随页大小线性增长。
- `extensions/management/list.go:57-70` 在 native 标题库不可用且 `r.Title != ""` 时：

```go
query := `SELECT ... FROM ext_lifecycle_current c` + extOnly + ` ORDER BY c.document_id`
params := args
if r.Title == "" { query += ` LIMIT ? OFFSET ?` }
```

标题非空时没有 LIMIT，读取整个 scoped 列表后再在内存过滤（:88-101）。

- `FamilyWorkspaceView.vue:275-297` 用 `overview(scope,1,'')` 前 20 篇只为取 `can_write`；`BusinessView.vue:248-252` 同样。
- `FamilyWorkspaceView.vue:315-330` 的 replacements/suggestions 分支额外调用 `api.workspace(...,'members',1)` 取 header。

### 影响

- 大 KB 首页有 N+1 查询；标题搜索在 native 标题不可用时可能把整库文档读入内存，接口延迟和内存占用不可控。
- 进入版本族/业务治理页时重复读取 20 篇 overview，只为权限标志。

### 建议

- overview 增加批量 `Snapshot`/annotations 查询：按当前页 IDs 一次读取 annotations、current、availability，只在详情页做完整 Snapshot。
- title fallback 改为 SQL 可下推的标题索引；若 native 标题无法访问，明确返回 `degraded_reason=title_index_unavailable`，限制候选集并返回 `truncated`，禁止无上限全表读。
- 增加轻量 authority/can_write 端点，或在 `listFamilies`/`workspace` 响应里直接返回 `can_write`；`FamilyWorkspaceView`/`BusinessView` 不要为权限额外拉 overview。
- `FamilyWorkspaceView` replacements/suggestions 分支复用已加载的 `family` header，减少重复 `workspace(...,'members')`。

## 7. P1/P2：错误处理、dirty 状态与降级提示

### 证据

- `LifecycleView.vue:214`：`submitDocument` 的 catch 无条件 `documentConflict.value=true`；403/网络 503 也会禁用保存按钮直到重新载入。
- `FamilyDocumentPanel.vue:89-93`：任意错误 `reloadRequired.value=true`。
- `SupplementEditor.vue:73`：任意错误 `conflict.value=true`。
- `LifecycleView.vue:180` `closeDocument` 只检查 `detailLocked`，没有检查日期表单 dirty；`FamilyDocumentPanel` 关闭/切换也没有 dirty 确认。
- `LifecycleView.vue:218` 切 KB 只清 `reviewsLoaded`，不清 `reviewsTruncated`；`reviewItems`（:49）只显示 `document_id`，没有标题。
- `frontend/src/api/lifecycle.ts:10` 的 `Overview` 未声明后端已返回的 `degraded`/`degraded_reason`；页面没有降级提示。

### 建议

- 按 `httpStatus(e)` 分类：409/`revision conflict` 才设置 `documentConflict`/`reloadRequired`；403 展示权限提示并保留只读；网络/503 允许重试。
- 给日期表单增加初始值快照与 `dirty` 计算；`closeDocument`/关闭面板/切换文档前确认；保存成功后重置基线。
- `watch(kb)` 同步清空 `reviewsTruncated`；`reviewItems` 批量调用 `documentTitles`，显示标题和文档 ID 辅助。
- `Overview` 类型增加 `degraded?: boolean`、`degraded_reason?: string`；顶部展示“原生标题库不可用，标题仅显示编号”等提示。
- 补组件测试：非 409 错误可重试；dirty 关闭弹出确认；reviews truncated 切库清零；degraded 横幅出现。

## 8. P1：待复核 inbox 没有 lifecycle/availability 门禁

### 证据

- `extensions/review/inbox.go:27` 直接查询 `ext_review_alerts`：

```sql
SELECT ... FROM ext_review_alerts a
LEFT JOIN ext_projection_outbox o ...
WHERE a.tenant_id=? AND a.knowledge_base_id=? AND a.status='open'
ORDER BY a.detected_at,a.alert_id
```

- `extensions/review/service.go:65-96` 的 `deadlineTx` 只读 `ext_document_annotations` 与 supplement 依赖，未检查 `ext_lifecycle_current` 或 `ext_document_availability`。
- `extensions/management/server.go:254-258` 的 `reviews` 直接调用 `review.NewService(...).List`，返回 1000 条。

### 影响

文档已归档、已删除或已不可用时，旧 `review_alert` 仍可能出现在待复核列表；用户点进去看到已失效文档，或者无法处理。

### 建议

- `ListTx` 增加 `ext_lifecycle_current` verified/readable 条件（至少排除 archived/缺失 availability 的文档）；或由 `review` 服务在 sweep/alert 生成时同步 resolve 非 active 文档的 open alerts。
- `management/reviews` 返回 `degraded`/`truncated` 与标题信息。
- 补 `extensions/review` 测试：archived/deleted 文档的 open alert 不出现在 inbox；重新激活后可按新 generation 生成。

## 9. P2：其他范围/体验问题

1. **scope join 完整性**：`family_workspace.go:258` 的 `latest` join 缺 `tenant_id`/`knowledge_base_id`；`related.go:363-368` supplement 分支缺 tenant/KB 约束。建议所有辅助表 join 都带完整复合键。
2. **家庭列表重复 overview**：`FamilyWorkspaceView.vue:280` 的 `Promise.all([listFamilies, overview(scope,1,'')])`；`BusinessView.vue:250` 同样。建议 authority 轻量化。
3. **记录标题**：`FamilyWorkspaceView.vue:247` 的 `title(id)` 只按 document_id；跨库目标一旦进入关系列表，应使用 `kb+id` 复合键。
4. **列表可读性**：`LifecycleView.vue:37-41` 文档列表已有标题列，但 `reviewItems`（:49）只有 ID；建议补标题、时间、类型筛选。
5. **URL 状态**：`showList`/`showFeedback` 只维护 `view`/`kb`，tab、分页、筛选没有可分享 URL；建议用 query 参数统一 `tab/page/state/title`。
6. **CreateForm 目标侧**：`CreateForm.vue:14` 固定 `fixedDocument.end==='target'` 时源文档 picker 仍用当前 `kb`，没有“源知识库”选择；后端 `relation_declare` 也只有 `TargetKnowledgeBaseID`，所以从目标文档一侧创建跨库关系不可达。建议增加 `source_knowledge_base_id` 或明确限制只能从源 KB 创建。
7. **测试缺口**：已有 `frontend/src/views/lifecycle/component.test.ts`（只覆盖 overview/reviews 晚响应隔离）、`frontend/src/views/families/component.test.ts`（批次重试/替代审批）、`extensions/management/read_test.go`（native fallback/标题过滤）、`extensions/business/family_workspace_test.go`。缺 corrupt current、degraded 前端消费、跨库 family workspace、空 knowledgeBases、跨库 open/query、dirty 表单、review archived 等测试。

## 10. 建议实施顺序与验收

### 阶段 A：数据正确性（先做）

1. 修 `management/read.go` counts + `management/list.go` native/fallback 的 verified current。
2. 修 `workspaceScope`/`documentActivitySQL`/`documentRelationRecords` 的跨库 union 与 KB 字段。
3. 修 review inbox 的 lifecycle/availability 过滤。
4. 验收：`go test -mod=readonly ./management ./business ./review -count=1`；新增 corrupt-current、跨库 family workspace、archived alert 用例。

### 阶段 B：跨库工作台可用性

1. `LifecycleView` 向 FamilyWorkspace/BusinessView/LineageView 传 `knowledge-bases`。
2. `FamilyWorkspaceView`/`BusinessView` scope 模式加载目录；`CreateForm` 补 `knowledge-bases`。
3. open/query/title 全链路携带 `knowledge_base_id`，标题用复合键。
4. 验收：`npm run type-check`、`npm run build`；组件测试覆盖空列表、跨库目标、图节点 open。

### 阶段 C：性能与错误处理

1. overview 批量 annotations；title fallback 限制候选并 degraded。
2. 轻量 authority/can_write 或复用 workspace header。
3. stale/超时/409 分类；dirty guard；degraded 横幅。
4. 验收：大库 fixture 下 overview 查询次数与响应上限；网络错误重试用例。

### 阶段 D：体验与回归

1. 待复核标题、排序、URL 状态、空状态。
2. 补齐 `frontend`/`management`/`business` 回归测试。
3. 最终 `gofmt -l .`、`go vet -mod=readonly ./...`、`go test -mod=readonly ./... -count=1`、`npm run type-check`、`npm run build`。

## 11. 开放问题（需要产品/架构确认）

1. 跨库关系在目标 KB 的版本族中是否应显示为“外部关系”，以及目标文档不在当前家庭成员时是否仍允许出现在“当前关系”？
2. 跨库 `supplements` 的目标文档是否必须属于当前 family？如果不属于，工作台如何展示 external 关系？
3. `family_suggestions` 目前只按单 KB 查询；跨库建议是否属于本阶段范围？
4. `review` 的待复核列表是否只面向 active 文档，还是归档文档也需要保留可追溯提醒？
5. `CreateForm` 是否支持从目标文档一侧选择“源知识库”；若不做，需要在 UI 明确限制只允许从源 KB 创建跨库关系。

## 12. 审计涉及的主要文件

- 前端：`frontend/src/views/lifecycle/LifecycleView.vue`、`LifecycleView.vue`、`SupplementEditor.vue`、`frontend/src/views/families/FamilyWorkspaceView.vue`、`FamilyDocumentPanel.vue`、`frontend/src/views/business/BusinessView.vue`、`CreateForm.vue`、`frontend/src/views/lineage/LineageView.vue`、`frontend/src/api/lifecycle.ts`、`frontend/src/api/business.ts`。
- 后端：`extensions/management/read.go`、`list.go`、`server.go`、`business.go`；`extensions/business/family_workspace.go`、`feedback.go`、`related.go`、`family_suggestions.go`、`family_suggestion_scan.go`；`extensions/review/inbox.go`、`service.go`；`extensions/store/lifecycle_current.go`。
- 关联基线：`PROJECT_CONTEXT.md` schema 30/31 说明、`TASK_BACKLOG.md` M4 行、`docs/design/文档关联总体改造.md` REL-01/REL-09。
