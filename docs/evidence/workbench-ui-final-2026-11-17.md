# 文档工作台三项 UI 收尾（2026-11-17）

## 用户决策

1. 跨库 CreateForm：**不改后端**，目标侧加明确提示并禁用跨库创建入口。
2. family_suggestions：**不扩到跨库**，保持单 KB 查询。
3. #4：待复核列表增加按 review_due 时间的正序/倒序筛选。

## 实现

### 1. 目标侧跨库关系入口禁用

`frontend/src/views/business/CreateForm.vue`：新增 `targetSideCrossBlocked`（`kind==='relation'` 且 `fixedDocument.end==='target'`），提交按钮 `:disabled` 纳入该条件，`submit()` 首行也直接 return（双保险）。提示文案升级为 notice：「当前从目标文档创建关系，仅支持同库关联。跨库关系请前往源文档所在知识库发起；此入口已禁用。」

理由：`relation_declare` 没有 `source_knowledge_base_id`，从目标侧无法表达跨库源；与其 silent 生成错误作用域关系，不如明确禁用并指路。

### 2. family_suggestions 保持单 KB

确认 `FamilyWorkspaceView` 调用 `api.suggestions(scope,...)` 为单 KB 查询，符合决策，未改动。

### 3. 待复核时间排序

`frontend/src/views/lifecycle/LifecycleView.vue`：新增 `reviewSort`（asc/desc，默认 asc）与 `sortedReviewItems` computed，按 `review_due` 排序（同值用 alert_id 稳定排序）；待复核面板加"正序/倒序"切换按钮，列表改渲染 `sortedReviewItems`。

## 验证

- 极简契约测试 2 条：CreateForm 目标侧必须存在 guard、submit 拒绝、按钮禁用、提示指路；待复核列表必须跟踪方向、排序后渲染、双向可选。
- `npm run type-check` 退出 0；相关前端测试 **29/29 PASS**；`npm run build` 退出 0。
- 新前端产物已部署生产 `web/`（备份 `web-backup-20261117-204456`），生产 HTTP 提供新 chunk。

## 边界

未改后端契约、未改生产数据；排序仅在前端对已返回列表重排，不改变 1000 条上限与 truncated 提示。
