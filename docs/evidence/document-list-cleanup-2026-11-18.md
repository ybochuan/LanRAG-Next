# 工作台文档列表与 30 天清理队列（2026-11-18）

## 需求

1. 文档工作台「全部文档」与原生文档列表一致，已删除文档不再以 UUID 出现在列表里。
2. 已删除文档进入 30 天保留期，清理有工作台提示和原生顶部铃铛通知，默认需要确认才清理扩展表记录。
3. 清理提醒接入原生通知中心，而不是自建消息通道。

## 实现

- **列表来源**：`extensions/management/list.go` 改为从 `weknora.knowledges` 出发，`deleted_at IS NULL` 过滤；`ext_lifecycle_current` 变成左连接属性。无生命周期行的原生存活文档显示 `pending`，不再消失。状态计数同步改为原生来源（`read.go` 的 `nativeOverviewCounts`）。原生库不可用时才退回旧扩展投影并标记 `degraded`。
- **清理队列**：新增 schema 34（`extensions/store/migrations/034_cleanup_tasks.sql`）和 `extensions/cleanup/`：每日扫描原生软删除，仅为有扩展投影的文档建任务；`first_observed_at + 30 天` 计算到期；D0 / T-7 / T-1 / 到期四阶段提醒；到期默认转 `awaiting_confirmation`，不自动删除；`AGENTRAG_CLEANUP_AUTO=true` 才自动清理。确认时再次核对原生 `deleted_at` 与业务族引用，按事务清理 lifecycle/annotation/availability/review/version 等扩展投影，清理任务本身保留为审计。
- **原生铃铛**：Core 新增 `POST /api/v1/notifications/internal/cleanup-alert`（管理密钥、服务端解析 KB owner、Kind=`lifecycle.cleanup_due`、SourceType=`cleanup_alert`）；Host `cleanupNotifier` 事务外异步投递；前端铃铛新增清理类标签，点击跳 `/platform/documents?tab=cleanup`。
- **工作台**：新增「待清理」标签、横幅汇总、真实标题、到期时间、保留 90 天和二次确认清理按钮。

## 验证

- `go test -mod=readonly ./... -count=1`（extensions）：除 `TestRuntimeStartsScopedWorkersRetriesAndClosesDatabase` 外全部通过；该用例在 HEAD 干净 worktree 同样失败，属于既有基线问题，未修改。
- 新增 `TestOverviewUsesNativeLiveDocumentSet`、`TestSweepCreatesTaskAndDetectsDeletion`、`TestHoldAndConfirmCleanExtensionProjections`、`TestCleanupManagementActions` 四个回归测试，均通过。
- Core 变更包 `internal/handler`、`internal/router`、`internal/middleware` 测试通过；CGO lite 构建退出 0。
- 前端 `npm run type-check`、`npm run build`、`npm test`（2438 项，2437 通过 / 1 skip）、`npm run check-i18n` 全部通过；构建产物已同步到 `/mnt/d/LanRAG-Next/web`。
- 实机（Core :18880 / Host :8090 均 200）：`overview` 返回 total=76（原生存活 76）、`cleanup.total=12`、degraded=false；`business_cleanup_list` 返回 12 条带真实标题的任务；扩展库 schema=34；原生 `notifications` 表出现 1 条 `lifecycle.cleanup_due` 铃铛消息。
- 备份：清理队列重置前扩展库 `data/backups/agentrag_ext-20261118-1205.db`，旧 Core `data/backups/WeKnora-lite-remediation-20261118-1205`。

## 后续两项待办（2026-11-18 完成）

- **证据保留**：schema 35 新增 `ext_evidence_retention_runs` 审计表和相关索引；`extensions/retention/` 每日按 30 天剪除终态 run 的子表证据（receipts/audit/inputs/intents/steps）、快照证据和 tag 同步失败记录。run 头本身不删，因为 `ext_feedback_qa_bindings` 外键指向它；当前版本族的 root/current 版本证据受保护。配置 `AGENTRAG_EVIDENCE_RETENTION_DAYS=30` 与 `AGENTRAG_EVIDENCE_RETENTION_INTERVAL=24h`。
- **共享空间成员通知**：Core `NotifyCleanupAlert` 在知识库创建者之外，还遍历 KB 分享和 org 成员，逐租户给代表用户写同一铃铛通知；`tenant:user` 去重。服务缺失或查询失败退化为仅创建者，不影响通知主路径。

## 最终形态（2026-11-18 深夜）

- **移除投影开关**：`family_projection_set`、投影开关表 `ext_business_family_projection_scopes` 及其读/写/UI 全面删除；业务族成员与发布头变化后自动向 Outbox 入队 `family_tag`，投影始终按当前业务族事实执行。
- **环境初始化**：原生库知识库/文档/分块/标签/资源/会话/通知/审计清空，保留模型/存储/系统设置；扩展库 83 张业务表清空，schema 35 与完整性保留。
- 凭证校验接受「知识库尚未创建」的空环境，干净安装可直接启动。
- 清理审计表里的运行记录由服务运行时产生，不属于知识库数据。

## 限制

- 清理只删除扩展表投影；原生文档、原件、FAQ 和向量索引不受影响。
- 当前不自动删除已删除文档的业务族成员记录；存在成员引用时清理拒绝并进入 `failed`，需人工处理。
- 铃铛接收人沿用知识库创建者；共享空间其他成员暂不单独通知。
- 证据保留以「run 最后活动时间」判定；未封存的 open run 永远不清理。已剪除的旧反馈 QA 绑定仍保留 run 头，但因为 inputs/steps 已删，不再满足指导来源匹配条件（符合既有「无完整来源的历史回答拒绝」策略）。
