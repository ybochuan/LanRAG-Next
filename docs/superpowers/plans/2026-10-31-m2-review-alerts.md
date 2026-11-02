# M2-REVIEW 本地复核告警设计与实施计划

授权：用户要求继续下一项；按TASK_BACKLOG顺序推进M2-REVIEW。只修改独立扩展及文档，保留已有M1/M2/SSRF改动，不提交推送、不启动真实DB迁移或外部通知。

**目标**：review_due严格超过后进入本地持久告警收件箱，固定日志通知，绝不改变生命周期；重启、重复扫描、失败重试和日期变更可验证。

**技术选择**：复用SQLite单写者/Outbox/Worker。新增review package与schema5。Host显式分派tag_projection与review_alert，未知投影拒绝；启动补扫后24h扫描。CLI只读现有独立扩展DB，必须指定tenant/KB，不创建DB或运行迁移。

## 明确的行为

- 按scope+规范UTC review_due识别告警，同期限持续逾期只建一次。标签等无关标注修订不重复通知。
- 日期改为未来/清空后旧告警resolved，未投递的旧意图校验当前标注后无操作完成；收件箱查询立即屏蔽失效日期。
- 同一旧期限后来恢复为逾期，重开原告警并增加generation，产生新投递意图；已处理的旧generation不发送。
- 告警变更与Outbox入队同事务。通知回调在写事务内校验最新标注/代次并快速发本地固定信号，成功后记delivered_at；回调不得网络I/O或递归写DB。此回调适配现有Host report约束。
- 外部日志副作用与DB提交不能原子，崩溃窗口可能重复日志；持久收件箱去重且Outbox至少一次，不承诺外部exactly-once。
- UI/邮件/webhook不接入；信息由只读CLI列出，Host日志不含tenant/KB/doc/凭据。告警元数据写自身DB，不是生命周期事实。

## 冲突／实施差异

C-18 Implementation Difference：Frozen4.6列出的三类causation针对现有标签投影，复核告警使用独立review_alert因果记录（alert_id+generation），不冒充lifecycle_event/lineage/document_version。原三类及Frozen文件不改，无跨模块契约或原生补丁。

## 文件与步骤

- [x] review/service_test.go先RED；service.go事务扫描/去重/重开、delivery.go即时校验与投递；review/inbox.go只读Tx查询。
- [x] store/migrations/005_review_alerts.sql、migrate.go、migration_test.go升级，store/read_only.go及测试用于CLI安全只读。
- [x] cmd/review-alerts/main.go及测试：JSON列表、显式scope、原生/缺失DB拒绝、无写入/隐式迁移。
- [x] Host dispatcher及runtime接线、真实回环/SQLite集成回归，未知类型不得误投标签。
- [x] 真实SQLite并发/故障回滚、进程崩溃、失败到dead/人工重试/重启恢复；全部扩展checks/Linux race/模块门禁/Python。
- [x] 一次最终独立复核，无Critical/Important/Minor，无需修复轮次；同步证据、事实、任务状态。

Ruling: 缺少外部渠道定义时仅本地收件箱和固定日志 — 不擅发消息 — 外部通知与UI可见性留后续。
Ruling: 查询时用最新标注过滤，投递时同写事务复查 — 防止日期改动后旧告警继续发送 — 扫描状态收敛仍最多24h；不是精确时钟定时器。

Ruling: 在saveAnnotationsTx同事务关闭旧期限告警 — 日期未来→旧期限的往返可能发生在扫描之间，回归先RED后GREEN — 生命周期状态不变；旧schema查询保持只读，由新版Host负责升级。

Evidence: docs/evidence/m2-review-alerts-2026-10-31.md。开发者/独立复核均测试通过；不对真实DB迁移、不替换服务、不运行生产模型、不提交推送。
