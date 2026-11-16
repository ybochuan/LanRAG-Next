# M5-ACCEPT 自动激活、抽样复核与主题恢复证据（2026-11-16）

## 范围

本批补充 M5 快速通道的自动化与可审计能力：

- `SweepFeedbackActivations`：Host ticker 扫描已批准、冷静期到期的低/中风险反馈并激活；高风险仍要求显式 `second_confirmation`。
- 低风险激活时按 feedback ID 的确定性哈希执行 20% 抽样，写入 `ext_feedback_sample_reviews`。
- `feedback_sample_review`：复核人提交 `passed` 或 `failed`；失败时反馈与指导同时隔离，且抽样只能处理 pending 记录。
- `feedback_topic_recover`：同主题两次负反馈熔断后，经二次确认记录恢复事件；恢复只允许新的指导进入 active，旧指导不自动复活。
- schema 032 新增抽样表和主题恢复表，Host ticker 增加自动激活扫描。

## 变更文件

- `extensions/store/migrations/032_feedback_acceptance.sql`
- `extensions/store/migrate.go`
- `extensions/store/manual_schema.go`
- `extensions/store/manual_schema_test.go`
- `extensions/business/feedback_accept.go`
- `extensions/business/feedback_accept_test.go`
- `extensions/business/feedback.go`
- `extensions/business/service.go`
- `extensions/host/runtime.go`

## 验证命令与结果

```text
go test -mod=readonly ./business -run 'TestFeedback|TestBinary|TestLowRisk' -count=1
ok github.com/ybochuan/LanRAG-Next/extensions/business 8.497s

go test -mod=readonly ./store -run 'TestManualSchemaAdoption' -count=1
ok github.com/ybochuan/LanRAG-Next/extensions/store 143.453s

go test -mod=readonly ./... -count=1
退出 0（全部扩展包 ok；本轮 business 82.284s、store 167.641s，其余包见终端记录）

go build -mod=readonly ./...
退出 0

go vet -mod=readonly ./...
退出 0

python scripts/check_module_boundary.py
module boundary passed: 325 imports, no WeKnora dependency or active workspace
```

`-race` 未执行：本机 Go 默认 `CGO_ENABLED=0`，启用 CGO 后缺少 `gcc`，命令在构建阶段失败；不把该失败写成 race 通过。

## 生产实时探测（2026-11-16）

在 `start-all.sh` 启动的 Core `127.0.0.1:18880`、Host `127.0.0.1:8090` 上，对生产扩展库执行了一次可清理的合成低风险反馈探测：

- 通过 `business_feedback_create` 创建 `low` 风险反馈（topic `m5-accept-live-4d06f2c69e9d`，id `a5ec229d589890ba0623a0770bd4f764`），初始 `approved`、revision 1。
- 为绕开真实 24 小时等待，只把该合成行的 `ready_at` 推进到过去；Host ticker 在 15 秒内将其自动激活为 `active`、revision 2。
- 该 feedback ID 命中 20% 确定性抽样，`ext_feedback_sample_reviews` 自动产生 pending 记录；`business_feedback_sample_review` 以 `passed`、reviewer `m5-live-reviewer` 完成复核，feedback 保持 active、revision 3，列表返回 `sample_decision=passed`。
- 随后 `business_feedback_withdraw` 将其置为 `withdrawn`、revision 4；对应 `ext_feedback_guidances` 同步为 `withdrawn`，生产库无残留 active 合成反馈或指导。
- 生产扩展库 `ext_schema_migrations` 已到 32，`ext_feedback_sample_reviews` 与 `ext_feedback_topic_recoveries` 均存在；这证明启动迁移和 Host ticker 接线在生产进程上生效。

边界：本次只推进了一条合成行的 `ready_at`，不代表真实 24/48 小时冷静期已经等待完成；管理调用需要显式 `can_write: true`（缺少时管理路由返回 `read_only`）；专用管理页面仍未联调，真实用户反馈与每库质量回归未执行。

## 覆盖的场景

- `TestFeedbackAutoActivationSweepAndSampleReview`：low 风险到期自动激活、抽样记录 pending、列表可见 sample decision、failed 复核同时隔离 staging/guidance、重放返回冲突。
- `TestFeedbackSampleReviewPassKeepsActive`：passed 复核保留 staging/guidance 为 active，抽样记录更新为 passed。
- `TestFeedbackSweepSkipsHighRiskAndNegativeTopic`：high 不自动激活；medium 到期自动激活；同主题两次负反馈阻止低风险自动激活。
- `TestFeedbackTopicRecoveryResetsCircuit`：无 `second_confirmation` 拒绝恢复；恢复后新种子可 active；旧 guidance 保持 quarantined；恢复事件写入 `ext_feedback_topic_recoveries`。
- `TestFeedbackAutoActivationAuditFailureRollsBack`：指导插入触发器失败时，sweep 返回错误，staging 仍 approved、无 guidance、无 sample；移除故障后重试只产生一条指导。
- `TestFeedbackSweepLoadIsIdempotent`：24 条低风险反馈、6 个并发 sweep，最终 guidance 与 sample 各 24 条；再次 sweep 不增加。
- `TestFeedbackSweepSurvivesReopen`：低风险反馈在关闭并重新打开同一 SQLite 后仍可由 sweep 激活，重复 sweep 不重复产生指导或抽样。
- M5-BINARY 的 `TestBinaryPublishUnknownObservationRetainsSourceAndRetries`、`TestBinaryPublishFinalAuditFailureRollsBack` 及扩展后的版本漂移测试同时通过；详见 `m5-binary-backend-2026-11-15.md`。

## 边界与后续

- 自动激活仅在 Host ticker 的进程生命周期内运行；本批未在生产环境等待真实 24/48 小时，时间推进由测试时钟完成。
- 20% 抽样只覆盖低风险快速通道；medium 仍需人工批准后激活，未对其抽样；抽样复核 API 已接入业务 action，但专用管理页面尚未接入。
- 主题恢复只记录恢复事实并解除后续负反馈计数，不自动复活旧指导；这是有意的安全边界。
- schema 032 兼容性由 `TestManualSchemaAdoption` 覆盖，生产扩展库 `ext_schema_migrations` 已达到 32；其他环境仍应按运维流程在维护窗口执行迁移。
- M5-ACCEPT 完整验收仍需真实 24/48 小时时间推进、管理端页面联调和每库生成质量回归；本文件证明本轮代码、SQLite 事务/崩溃恢复、并发负载、模块门禁以及一次生产合成实时探测。
