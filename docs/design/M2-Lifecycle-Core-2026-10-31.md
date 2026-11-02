# M2 生命周期内核 — 2026-10-31

依据：Frozen Architecture4.5–4.10/16，以及用户本轮补充的默认迁移矩阵。本文件记录实现决定，不修改 Frozen；M1 保持未验收。

## 数据驱动规则

日期来自扩展表 ext_document_annotations 的 effective_from、effective_until、review_due，存储 UTC；labels_json 保存显式分类，不臆造业务词表或自动投影分类。

| 触发 | 迁移 | 条件 |
|---|---|---|
| Initialize | 无状态 → pending | 缺失日期用默认规则补齐 |
| ParseCompleted | pending → active | 未提前、未过期 |
| ParseCompleted | pending → scheduled | effective_from 在未来且未过期，防止提前生效 |
| Timer | scheduled → active | now >= effective_from 且未过期 |
| Timer | active → stale | now > effective_until |
| review_due 超过 | 不迁移 | Snapshot.ReviewOverdue=true，告警投递待接线 |

严格按“超过”实现截止/复核时间：now > deadline；无 effective_until 永久有效。已过期的 pending/scheduled 不可激活；其直接 stale 的业务边尚未指定，当前拒绝并留后续运营规则。

## 数据库配置

选择用户允许的数据库方案，无 TOML 新依赖。Configuration.PutRules 通过 revision CAS 写 ext_lifecycle_rules；Resolve 返回来源、revision、token。KB 行完整覆盖 tenant 行，不逐字段合并，不跨租户共享。没有配置即全部0、永久有效。

- effective_delay_days：初始化缺 effective_from 时，以事务 UTC 时钟加日历天补齐，0不补。
- valid_days：缺 effective_until 时，以 effective_from（若有）或初始化时钟加日历天补齐，0不补截止。
- review_days：缺 review_due 时，以初始化时钟加日历天补齐，0不补复核日期。
- 默认天数限制0..36500，只在 Initialize 补缺，显式日期优先。后续修改规则不改写旧标注。PutAnnotations 是带 revision CAS 的完整替换；可清空日期，以后 Apply 不偷偷补回。

## 事务与重放

Machine.Snapshot 在只读事务读 current、annotations、resolved policy。Apply 在共享 BEGIN IMMEDIATE 内验证 expected_last_event_id、expected_annotation_revision、expected_policy_token 和触发/日期条件。

event + current + Outbox + 初始化 annotations + decision receipt 同事务。receipt 保存命令、有效标注、规则、决策时钟；复合身份事件 FK 延迟到提交校验，禁止跨 tenant/KB/doc 挂靠。current.last_event_id FK 保持。

精确重试即使时间/配置/状态变化仍 no-op；相同事件改变 trigger/head/revision/token 拒绝。Store.Append 保持 M0 低层 API，不能称为全局政策边界，旧事件不能冒充 M2 回执。

## 冲突与边界

- M2-01 已澄清：用户本轮提供默认矩阵及日期来源，取代前稿“无默认、完全配置图”的临时提议。未来生效的解析完成进入 scheduled；日期默认锚定见上文。
- M2-02：原 Outbox flake 根因是正常 parent 取消被多报一次。只抑制纯 parent cancellation；业务错误即使 join cancellation 仍报告。保留严格 drain 断言。
- M2-03：本批仅内核，不挂新 HTTP/Hook、不启后台任务。Observer→ParseCompleted、定时扫描、review 告警投递、配置鉴权接口、完整 active 检索治理待接线。M0 Pass-Through 和 C-10 拒绝保持，不能宣称上传后自动 active 或 stale 已被检索过滤。
- Document 与 Guidance stale 是独立类型/评估。补充通知仅接受显式同 tenant/KB、不同 doc 且有事件身份的父证据评估；父证据权威查询、M3 DAG 和自动传播未实现。

## 文件与验证

| 文件 | 验证 |
|---|---|
| extensions/lifecycle/rules.go、rules_test.go | KB全量覆盖、永久默认、UTC、CAS、24并发单赢家、非法窗口和跨租户隔离 |
| extensions/lifecycle/machine.go、machine_test.go；store.go薄guard | 提前激活、精确到期、复核不迁移、重放/变更拒绝、24并发、ABA、配置fence |
| extensions/lifecycle/stale.go、stale_test.go | 两类stale独立、补充通知同库证据及跨租户拒绝 |
| extensions/store/migrations/004_lifecycle_policy.sql、migrate.go、migration_test.go | v1/v2/v3升级保留解析产物/事件，不捏造标注 |
| extensions/lifecycle/machine_recovery_test.go | 实际Apply子进程在current前退出/提交后退出，重开核对五表和FK |
| extensions/lifecycle/machine_test.go | annotation/decision/event/current/Outbox五处SQL故障完整回滚 |
| extensions/outbox/worker.go、worker_test.go | 纯取消report0、业务错误+取消report1、原drain严格计数及Linux race |

进程退出不能代替物理断电测试。测试仅用临时独立扩展DB，本轮无新增原生补丁、依赖变动、提交或推送。

## 2026-10-31 接线增补（覆盖前述首批内核边界）

用户补全过期边：pending/scheduled/active超过effective_until直接stale。新增automation.go、ingest/lifecycle.go、retrieval/policy.go及Host接线；启动补扫后每24h执行，worker使用FULL持久DB，probe业务503。

默认无日期completed文档初始化pending后active；未来生效scheduled，过期直接stale。Observer原始观察先持久提交，业务决策独立原子提交；重复completed观察重试决策，不能把raw observation已存在当作业务完成ACK。业务记录stage仍是reconcile，不伪造原生中间历史。

ParseCompleted含ParseObservationAt时，写事务内确认最新观察completed且source timestamp匹配。scheduled→active的Timer迁移也必须同事务确认最新观察completed，缺失/failed/processing拒绝。该条件不阻止任意过期状态转stale。

DB检索策略对候选集合查询当前事实，只有active保留，其他状态和缺失记录排除。原生Bridge验证request/tenant/KB、完整且不重叠的allow/exclude分割及deny_all；结果过滤普通文档允许集并减排除集，WebSearch保留。仍不枚举whole-KB，不保护其他Agent入口或生成中停用。

新增跨进程脚本extensions/scripts/m2-policy-e2e.sh，验证真实Host DB策略→原生管线过滤，底层检索/源列表为夹具。完整LLM问答、生产数据、其他入口、review告警和配置UI不包含在该证据中。

## 2026-10-31 M2-REVIEW 本地渠道交付

复核告警现使用extensions/review、schema5及Host显式Outbox分流。启动补扫/24h扫描，标注期限变化同事务关闭旧告警；只读CLI可查询当前scope收件箱。日志固定code无文档身份，外部投递不在本批。delivered_at是本地回调接受时间，崩溃窗口仍可能重复信号，不改变生命周期。

C-18 Implementation Difference：新增review_alert因果记录，保持原三类标签来源及Frozen不变；原生本批无新增补丁。M2-REVIEW本地范围验收，M2-CONFIG/权威补充通知联动仍待实施。证据：docs/evidence/m2-review-alerts-2026-10-31.md。
