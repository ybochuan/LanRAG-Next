# 复核提醒接入原生消息铃铛（2026-11-17）

## 决策与范围

按用户决策：采用窄端点方案，不做双写；工作台"消息中心"tab 撤掉；原生铃铛通知的 Link 指向工作台待复核 tab；已读不同步（原生铃铛已读只管 UI，业务 ack 仍发生在工作台）。

## Core 侧（受控区 PATCH，已登记）

- 新增 `POST /api/v1/notifications/internal/review-alert`：handler `NotifyReviewAlert`（`internal/handler/automation.go`），读 `AGENTRAG_MANAGEMENT_KEY`，用 `crypto/subtle` 常量时间比对专用头 `X-AgentRAG-Management-Key`。入参 `{title, content, link, source_id, knowledge_base_id, tenant_id}`（后两项为解析接收人所必需，是用户给定字段的必要增量）。
- 接收人**由服务端解析**：用注入的 `interfaces.KnowledgeBaseService.GetKnowledgeBaseByIDOnly` 查 KB owner（`CreatorID`），Host 不能指定 user。校验 link 必须以 `/platform/lifecycle` 开头、scope 匹配。
- 调 `NotificationService.Notify(ctx, kind="lifecycle.review_due", source_type="review_alert", source_id=alertID)` 写原生 notifications 表。
- 路由注册在 `internal/router/routes_automation.go`，**不在 Viewer 组**（机器主体走管理 key）；`internal/middleware/auth.go` 的 noAuthAPI 加该路径，避免全局 JWT middleware 误拦——这是这个端点唯一放开的口子，鉴权由 handler 自己完成。

## Host 侧（定制区）

- 新增 `extensions/host/review_notifier.go`：review 投递钩子在事务内只做 `Signal(alert)`（非阻塞入 channel，256 缓冲），真正的 HTTP 调用由独立后台 worker 在事务外完成——遵守 review delivery 的"notify 不得做网络 I/O"约束。
- `extensions/host/runtime.go`：worker 模式下构造 notifier（需 `AGENTRAG_WEKNORA_URL` + `AGENTRAG_MANAGEMENT_KEY`），alert 钩子中 New alert 时 Signal，原 `report(review.SignalCode)` 保留。HTTP 失败只 `report("review_notification_failed")`，不阻断 review 主流程。
- 考虑启动时序（Host 先于 Core 就绪）：deliver 做 3 次重试、每次 2s backoff，最终失败仅记日志。

## 前端

- 移除工作台"消息中心"tab：`LifecycleView.vue` 删 tab 项、渲染分支、异步组件、状态与 loadMessages/acknowledgeMessage 等整块；删除孤儿组件 `MessageCenter.vue`。
- 组件契约测试改为断言消息中心已移除；删除组件自身测试。前端 type-check 0、29/29 测试通过。

## 测试与生产端到端

- Host 单测 2 例（`review_notifier_test.go`）：Signal 非阻塞 + 正确 POST payload（source_id/KBase/tenant/link）；无效配置 noop + 投递失败只报告不 panic。全过。
- 生产端到端：先把某 active 文档 `review_due` 设到过去，重启触发 Host startup sweep → 新 alert `b38750c5…` 产生并 delivered → 原生铃铛 API 出现 `kind=lifecycle.review_due`、`source_id=b38750c5…`、`link=/platform/lifecycle?tab=reviews` 的通知。
- 期间发现过一次 `review_notification_failed`：Host 投递发生在 Core 就绪前；加重试后复测成功——这是真实的启动时序边界，已修。
- 生产数据已还原：review_due 回 NULL、测试 alert resolve、测试通知删除。

## 边界

- 不做已读双写：铃铛已读只改 notifications 表，不改 `ext_review_alerts.acknowledged_at`；业务 ack 仍在工作台复核动作里完成。
- 入参新增 `tenant_id`/`knowledge_base_id` 是必要的：铃铛按 tenant+user 过滤，接收人必须由 KB owner 服务端解析，不能由调用方伪造。
- 未改原生通知 UI 与 API 语义；只加了一个受管理 key 保护的写入端点。

## 测试基线说明

- 本轮新增/修改的相关测试全过：Host notifier 2 例、integration public API、前端 29 例、module boundary。
- 全量 `go test ./...` 中 `TestRuntimeStartsScopedWorkersRetriesAndClosesDatabase` 失败（observations=2, done=0，projection outbox 未收敛）。已隔离验证：临时禁用 notifier 构造后该测试仍失败，证明与本轮改动无关（禁用后仍 8s/20s 均不收敛）。属既有环境/时序问题，保留原有 8s deadline，不在本轮范围内修改。
