# POST-M0-TAGS 与管理端抽检复核 UI 交付（2026-11-17）

## 范围

用户 2026-11-17 改判：此前标记"不做"的 POST-M0-TAGS 与管理端抽检审核页面两项改造现在实施并完成。全程本地真实生产环境（Core 18880 / Host 8090 / 生产扩展库），不建隔离环境。

## 改造一：POST-M0-TAGS（解除 C-10 的 null 歧义）

### 问题

C-10 Baseline Limitation：`GET /api/v1/knowledge/:id` 的 `tags` 字段在"标签查询失败"与"文档确实无标签"两种情况下都序列化为 null，扩展只能安全拒绝（`ErrAmbiguousTags`），无法可靠区分。用户已接受该限制，现决定通过独立只读 API 解除。

### 实现

- **原生新增端点**（受控区薄接线）：`GET /api/v1/knowledge/:id/tags/ids`，handler `GetKnowledgeTagIDs`（`internal/handler/knowledge.go`），路由 `kRead.GET("/:id/tags/ids", ...)`（`internal/router/routes_knowledge.go`）。复用 `resolveKnowledgeAndValidateKBAccess` 的 Viewer 级 KB 授权，不做任何业务规则判断。
- 返回 `data: {knowledge_id, tenant_id, knowledge_base_id, tags: [{id,name,tenant_id,knowledge_base_id}]}`；**无标签返回空数组 `[]`，不是 null**。
- **扩展适配**（`extensions/taggov/http_store.go::ReadTags`）：优先调新端点；**一次成功的响应即为权威**——scope 不匹配或标签格式非法直接失败关闭，绝不回退到有歧义的旧 DTO。仅当端点不可达（传输错误，如旧版 Core 返回非 2xx）才回退旧逻辑，并继续保留 null 安全拒绝。

### 测试与验证

- 新增 `TestHTTPStoreExplicitTagReadEndpoint`，覆盖 one / empty（显式 []）/ wrong_scope / malformed 四场景；连同既有 `TestHTTPStoreRefusesAmbiguousTagsAndSendsNativeIDArray` 全部 PASS。测试暴露了一个真实边界：最初把"200 但 scope 不符"静默回退到旧端点，会绕过 fail-closed；已修为成功的 200 响应一律权威、校验失败即拒绝。
- 生产验证：新 Core 部署后，对 3 个 active 文档调 `/knowledge/:id/tags/ids` 均返回 **200 + `tags: []`**（这些文档确实无标签），证明 null 歧义已解除。
- 编译：`go build ./internal/handler/ ./internal/router/` 与扩展 `go build ./...` 均退出 0；扩展 `go test ./taggov/` PASS。

## 改造二：管理端抽检复核 UI

### 问题

后端 `feedback_sample_review` action 早已完整（`extensions/business/feedback.go`）：20% 确定性抽检产生 pending 记录，复核 passed 保持 active、failed 隔离 staging 与 guidance。但前端没有操作入口，只能调 API。

### 实现

- `frontend/src/api/business.ts`：`BusinessAction` 增加 `feedback_sample_review`；`BusinessPayload` 增加 `sample_decision` 与 `review_note`；`BusinessRecord` 增加 `sample_decision` / `sample_selected_at` / `sample_reviewed_at`。
- `frontend/src/views/business/model.ts`：新增 `actionLabels.feedback_sample_review` 与 `sampleReviewPending(row)`（仅当 `sample_decision==='pending'` 且 `status==='active'` 时显示复核控件）。
- `frontend/src/views/business/BusinessView.vue`：反馈详情中新增"抽样复核 · 待处理"区，提供结论选择（通过/失败（隔离））、复核说明、提交按钮；提交走既有 `beginWrite('feedback_sample_review', ...)`，保持幂等/待核对/权限语义不变。已复核的记录显示结论标签。

### 测试与构建

- 新增组件契约测试（`component.test.ts`）与模型测试（`model.test.ts`）：验证 action 接线、payload 携带 sample_decision、仅 pending 样本显示控件、failed 选项说明隔离语义。
- `npm run type-check` 退出 0；业务+生命周期测试 **19/19 PASS**；`npm run build` 退出 0。
- 部署：先备份 `web/` 到 `web-backup-20261117-184946`，再把新构建产物部署到生产静态目录 `web/`。生产 HTTP 验证 `BusinessView-C4UZJCkr.js` 返回 200（37026 字节），含 `feedback_sample_review`（3 处）与"失败则隔离"文案。
- 生产 API 侧确认 `feedback_list` 已暴露 `sample_decision` 字段；当前 3 条 active 反馈均已复核完，无 pending 样本，因此未对真实 pending 记录执行写操作（后端 action 由单元/组件测试覆盖）。

## 回归

两项改造部署后，真实 `/knowledge-chat` 提问返回 2036 字中文回答并带 2 个 `<kb>` 引用，检索与问答链路未受影响。

## 边界

- POST-M0-TAGS 是只读标签 ID/名称 API，不改变原生标签写入逻辑，也不自动复活旧标签投影；扩展的 merge/ReplaceTags 仍走单写者队列。
- 抽检复核 UI 复用 BusinessView 既有写路径，不新增独立页面；权限不足时按钮禁用。
- 未改生产业务数据；前端旧静态资源已备份。
