# 第 3 类剩余项功能审计与 BACKLOG 状态订正（2026-11-17）

## 目的

用户要求订正 BACKLOG 里过期的「待开始／进行中」规划行，并重新审计上一轮列为第 3 类（可新授权推进）的三个项目的实际功能、作用与实现效果。Hindsight 搜索接口持续报错（服务端不可用），本文件完全基于仓库当前代码与生产状态。

## 三项第 3 类的实际实现审计

### 1. POST-M0-TAGS（独立标签只读 API）

- **真实状态**：未实施。扩展侧 `extensions/taggov/http_store.go` 已有 `ReadTags`、`ReplaceTags`、`Catalog`，但这些读方法是扩展内部用于标签投影对账的，不是原生独立只读 API。
- **功能**：计划新增原生只读标签端点，让「无标签」能被明确表达为 `[]`，而不是把 `tags:null` 当作未知并安全拒绝；同时区分查询故障（非 2xx）与真空列表。
- **作用**：解除 C-10 Baseline Limitation——用户 2026-10-30 接受了 `tags:null` 安全拒绝，代价是扩展无法可靠区分「文档确实没标签」和「标签查询失败」。
- **实现效果/代价**：要动 `internal/handler/knowledge.go` 与 `internal/router/routes_knowledge.go`（受控区），核心补丁范围须单独授权。当前 C-10 的安全拒绝仍在生效，投影失败会由 Outbox 重试收敛，功能上没有卡死。
- **结论**：可选增强，不是缺陷；不动也不影响已交付的知识治理 RAG 链路。

### 2. 管理端专用审核页面联调（M5-ACCEPT 遗留）

- **真实状态**：前端没有专用页面。`frontend/src/views/lifecycle/` 只有 LifecycleView / MessageCenter / SupplementEditor；`frontend/` 全仓搜索 `sample_review`／`sampleReview`／`抽样` 均无命中。
- **后端功能已完整**：`extensions/business/feedback.go:185` 的 `feedback_sample_review` action 已实现，`service.go:327` 已纳入管理业务 action；`ext_feedback_sample_reviews` 表、`List` 里的 `sample_decision` 字段都已接好。11-16 生产合成探测已通过 `sample_review passed`。
- **作用**：给人工审核员一个界面去处理 20% 抽检记录（passed/failed），失败时隔离 staging/guidance。当前只能通过 API 操作，管理端没有 UI。
- **实现效果/代价**：纯前端工时，不改后端语义、不碰受控区；可做但收益是操作便利性，不是功能缺失。
- **结论**：可选项，不影响治理闭环。

### 3. M0-06B（集成压测：2 文档 ingest + 1000 feedback 并发）

- **真实状态**：未实施。`extensions/integration/` 只有 `outbox_recovery_test.go` 与 `public_api_test.go`，没有规划中的 `bridge_test.go`、`ingest_recovery_test.go`、`concurrency_test.go`。
- **功能**：大规模并发集成压测——2 文档真实 ingest，叠加 1000 条 feedback + lifecycle + 标签投影，终态核对；并发失败按流程降至 1 重跑。
- **作用**：验证持久层与 Host 在高并发下的一致性上限，属于性能/容量验收，不是功能缺口。
- **实现效果/代价**：既有测试已覆盖 24 条 feedback / 6 并发 sweep 幂等、16/24 并发 CAS、进程崩溃恢复；1000 条量级尚未测。压测本身不产出新功能。
- **结论**：可选项，属于容量验收；当前无生产容量问题。

## 三项的共同判断

三项都是「可选增强/验收」，不是功能缺陷，也都不属于你定义的「纯粹知识治理 RAG」核心。POST-M0-TAGS 要动受控区且解除的是你已接受的 C-10；审核页面是操作便利性；压测是容量证明。建议保持不做。

## BACKLOG 状态订正

早期规划表（2026-10-29）里的「待开始／进行中」行，多数已被后续以不同路径实现或在本次收官中明确关闭。本次订正为反映真实状态，不改任何代码：

| 行 | 订正后状态 | 依据 |
|---|---|---|
| M0-03A | 已完成（Host 接线与本地验证在册；真实来源崩溃收敛待验属边界） | 03E 已完成；本轮多项 E2E 使用真实 Host |
| M0-04B | 已完成（已接线，C-10 限制为已接受的 Baseline Limitation） | M5-PROJECTION 已接线并验收 |
| M0-06B | 用户决定不做（容量压测，非功能缺口） | 本次审计 |
| M1-A / M1-B | M1-A 已完成（REL-08 真实 MinerU E2E 与权限回归通过 11-15）；M1-B 由版本化产物实现覆盖 | REL-08 证据、parser/artifacts.go |
| M2／（早期表） | 已被 M2 细分任务表取代，见 M2／进行中（内核…已接线） | 两张表并存导致重复 |
| M3／（早期表） | 已由 11-17 收官行取代（Critical Edge 账本一致、run 封存验证通过） | m3-m5-acceptance-closeout |
| M4／（早期表与后台表重复） | 后台关联行状态更新，跨库关系与补充首刀已接入；每库 calibrate / Agentic 待外部条件 | 本次核对 |
| M5-A / M5-B | M5-A 由 M5-ACCEPT 已实现覆盖；M5-B 由 M5-BINARY 已验证覆盖 | 两个细分表的实现路径不同 |
| M6-A | 用户原始目标排除（血缘图），不再追踪 | 原始目标声明 |
| M6-B | 已交付（单库血缘图首版），但属原始目标排除范围 | 11-05 证据 |
| POST-M0-TAGS | 用户决定不做（解除 C-10 需动受控区，已接受的 Baseline Limitation 保持） | 本次审计 |

## 边界

- 本轮只订正状态文字与写审计证据，不改代码、不改生产数据。
- Hindsight 接口不可用，未引用任何 memory；订正依据是仓库当前代码与已归档证据。
