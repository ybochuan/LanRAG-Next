# P2-D 轮询优化审计与实施（2026-11-07）

## 1. 审计范围与方法

本文先记录只读审计，随后记录用户批准后的实施与验收结果。审计对象为：

- `frontend/src/views/business/BusinessView.vue`
- `frontend/src/views/lifecycle/LifecycleView.vue`
- `frontend/src/views/knowledge/KnowledgeBaseList.vue`
- `frontend/src/views/knowledge/KnowledgeBase.vue`
- `frontend/src/App.vue`
- `frontend/src/components/GlobalInvitationBell.vue`
- `frontend/src/components/menu.vue`
- `frontend/src/views/knowledge/wiki/WikiBrowser.vue`
- `frontend/src/views/knowledge/settings/DataSourceSettings.vue`
- `frontend/src/views/knowledge/components/FAQEntryManager.vue`（仅作为同类失败重试参考，非 P2-D 核心页面）

方法：先对 `setInterval`、`setTimeout`、`visibilitychange`、`document.hidden`、`poll`、`refreshTimer` 做全目录检索，再逐文件读取定时器创建、条件、请求点、清理与失败处理。该检索覆盖 1,076 个文件，命中 96 条相关语句；本文只保留与页面生命周期和网络请求有关的项。

## 2. 结论摘要

1. `BusinessView.vue` 和 `LifecycleView.vue` 没有后台数据轮询：BusinessView 的 30 秒 timer 只更新页面上的当前时间，不发 API；LifecycleView 只有标题/筛选输入的 250ms 防抖。两者都不需要重复 P2-C 的缓存/请求去重改造。
2. `KnowledgeBaseList.vue` 没有周期刷新；上传完成和删除/移动后的刷新都是一次性 `setTimeout`，且都有卸载清理。知识库列表本身不是 P2-D 的请求源。
3. 真正需要处理的是 `KnowledgeBase.vue` 的三类条件轮询：文档解析状态 1.5 秒、Wiki 索引状态 5 秒、移动进度 2 秒。它们都只在对应任务进行时运行，但缺少页面隐藏暂停和失败退避，其中文档状态轮询在请求失败后仍会按 1.5 秒无限重试。
4. 全局轮询中，`GlobalInvitationBell.vue` 已有隐藏页面跳过请求；`App.vue` 的邀请角标每 2 分钟轮询无可见性门控，`menu.vue` 的 session activity 每 5 秒刷新无可见性门控，但后者只对 `detached` 会话发请求。
5. `WikiBrowser.vue` 的 Wiki 统计 5 秒轮询和 `FAQEntryManager.vue` 的 FAQ 导入 3 秒轮询存在同类失败重试问题；其中 FAQ 属于导入任务，不属于本次 Business/Lifecycle 核心页面，列为 P2-D 后续可选项。
6. `KnowledgeBase.vue` 的 Wiki 状态请求和 `WikiBrowser.vue` 的统计请求在卸载时清理了已有 timer，但没有在异步请求返回后检查组件是否已卸载；若请求恰好在卸载后返回，可能重新创建 timer。`KnowledgeBase.vue` 的文档状态轮询已有 `statusPollingDisposed` 防护，可作为实现参考。

## 3. 定时器清单

| 文件与位置 | 周期/触发 | 是否发请求 | 页面隐藏处理 | 卸载清理 | 失败处理 |
|---|---|---|---|---|---|
| `frontend/src/views/business/BusinessView.vue:67` | 30 秒 `setInterval` | 否，仅更新 `now` 显示 | 不适用 | `:142` `onBeforeUnmount` 清理 | 不适用 |
| `frontend/src/views/lifecycle/LifecycleView.vue:58,77` | 250ms 搜索防抖 | 是，标题/筛选一次请求 | 不需要周期轮询 | `:83` `onBeforeUnmount` 清理 | 由请求错误提示处理，无重试 |
| `frontend/src/views/knowledge/KnowledgeBaseList.vue:833,1567` | 上传完成后一次性 800ms | 是，`fetchList(true)` 一次 | 不适用 | `:1218` 清理 | 无周期重试 |
| `frontend/src/views/knowledge/KnowledgeBaseList.vue:1189,1194` | 删除/移动后一次性 5s | 是，`fetchList(true)` 一次 | 不适用 | `:1197` 清理 | 无周期重试 |
| `frontend/src/views/knowledge/KnowledgeBase.vue:455,1508-1599` | 1.5 秒（stalled 时 15 秒） | 是，批量 `batchQueryKnowledge` | 无 | `:1428-1436` 设置 `statusPollingDisposed` 并清理 | `catch { continue }` `:1542-1545`，失败后下一轮仍按原间隔 |
| `frontend/src/views/knowledge/KnowledgeBase.vue:148-179` | 5 秒，仅 `wikiIsIndexing` | 是，`getWikiStats` | 无 | `:202-205` 清理 | `catch` 静默忽略，可能继续 5 秒 |
| `frontend/src/views/knowledge/KnowledgeBase.vue:477,1772-1800` | 2 秒移动进度 | 是，`getKnowledgeMoveProgress` | 无 | `:1433` `stopMovePoll` | `catch` 忽略，继续 2 秒 |
| `frontend/src/App.vue:184-194` | 2 分钟邀请角标 | 是，`fetchPendingInvitationCount` | 无 | `:270-275` 清理 | 请求错误由 store 处理；timer 继续 |
| `frontend/src/components/GlobalInvitationBell.vue:122,303-313` | 60 秒通知列表 | 是，`listNotifications` | `refresh` 在 `document.visibilityState === 'hidden'` 时直接返回；`visibilitychange` 时补一次刷新 | `:308-313` 清理 | `loadError` 标记，timer 继续 |
| `frontend/src/components/menu.vue:325,1067-1070` | 5 秒 session activity | 仅对 `detached` 会话发 `fetchMessages` | 无 | `:1102-1107` 清理 | store 内连续失败 3 次删除 entry；timer 继续 |
| `frontend/src/views/knowledge/wiki/WikiBrowser.vue:2961-2997` | 5 秒，仅 `pending_tasks` 或 `is_active` | 是，`getWikiStats` | 无 | `:5123-5126` 清理已有 timer | `catch` 忽略；异步返回可能重新创建 timer |
| `frontend/src/views/knowledge/settings/DataSourceSettings.vue:38-52` | 3 秒，仅同步 running | 是，`listDataSources` | 无 | `:187` `onBeforeUnmount(stopPolling)` | `catch` 打印；无失败重试调度 |
| `frontend/src/views/knowledge/components/FAQEntryManager.vue:2009-2100` | 3 秒，FAQ 导入期间 | 是，`getFAQImportProgress` | 无 | `:2371-2373` `stopPolling` | 仅 404/not-found 停止；其他错误继续 3 秒（非核心页面，列为后续） |

## 4. P2-D 最小改造建议

### P2-D-01：有条件轮询的可见性暂停/恢复

适用于 `KnowledgeBase.vue` 的文档状态、Wiki 状态、移动进度，以及 `WikiBrowser.vue` 的统计轮询：

- `document.hidden` 时暂停当前 timer；不发起新请求。
- `visibilitychange` 回到可见时，如果任务仍在进行，恢复一次立即查询，再按原周期继续。
- 不改变 `needsStatusPolling`、`wikiIsIndexing`、`moveSubmitting` 的判定条件，不改变任务状态语义。
- 恢复时避免与新一次 `watch` 触发的更新重叠：沿用现有 `statusPollRequest`/timer 清理约定。

### P2-D-02：失败退避

适用于 `KnowledgeBase.vue` 文档状态轮询、移动进度轮询、Wiki 状态轮询；FAQ 导入轮询作为可选后续项：

- 文档状态：连续失败按 `1.5s → 3s → 6s → 12s → 30s → 60s` 递增，任一成功批次后重置为 1.5 秒；保留现有 stalled 的 15 秒下限逻辑，取两者较大值。
- 移动进度：连续失败按 `2s → 5s → 10s → 30s` 退避；达到上限后显示一次可重试提示，不重复弹窗。
- Wiki 状态：连续失败按 `5s → 10s → 20s → 60s` 退避，成功后重置。
- 失败计数必须随 KB 切换和组件卸载清零，不能在旧 KB 状态上继续。

### P2-D-03：全局低频轮询的可见性门控

- `App.vue` 的邀请角标：隐藏时跳过 tick；可见时若距离上次刷新超过一个周期则补一次，仍保持 2 分钟基础周期。
- `menu.vue` 的 session activity：隐藏时不调用 `refresh()`；可见时恢复。保留 `stores/sessionActivityState.ts` 只处理 `detached` 条目的判断。
- `GlobalInvitationBell.vue` 已有请求级隐藏门控，可保持现状；如要统一，可在 timer 层暂停，行为不变。

### P2-D-04：卸载/异步返回防护

- `KnowledgeBase.vue` 的 `fetchWikiStatusOnce` 增加已卸载标记或 `AbortController`，在 `await getWikiStats` 返回后先检查，避免卸载后重新创建 `wikiStatusTimer`。
- `WikiBrowser.vue` 的 `loadStats` 同样增加 disposed 检查；不能只依赖 `onUnmounted` 清理已有 timer。
- 所有新增 timer 在 `onBeforeUnmount`/`onUnmounted` 中清理，并清空 failure/backoff 状态。

### P2-D-05：不需要修改的页面

- `BusinessView.vue`：30 秒 timer 仅用于显示当前时间，不产生 API 请求；已有卸载清理，保持现状。
- `LifecycleView.vue`：没有周期轮询；搜索防抖与 Reviews 懒加载已是正确形态，保持现状。
- `KnowledgeBaseList.vue`：没有周期轮询；上传/删除/移动后的刷新是事件驱动的一次性动作，已有清理，不纳入 P2-D。

## 5. 验收计划

1. 增加前端单元测试：用 fake timers 验证隐藏时暂停、可见时恢复、失败退避序列、成功重置、卸载后无 timer/无请求。
2. 使用现有 `frontend/src/views/business/browser.fixture.mjs` 对 Business/Lifecycle 做一次空载验证：记录请求列表，确认等待 10 秒没有新增周期请求；切换页面/卸载后无残留请求。
3. 对 `KnowledgeBase.vue` 的状态轮询做可控失败注入：让批量状态接口返回错误，断言请求间隔按退避增长而不是固定 1.5 秒；恢复成功后回到基础间隔。
4. 验证隐藏页面：模拟 `document.visibilityState='hidden'`，确认不发起轮询；切回 visible 后只补一次并继续。
5. 执行 `npm run type-check`、`npm test`、`npm run build`；保留 P2-C 基线中 3 个既有 `POSIX shell tests require the Git for Windows usr/bin shells` 失败，不能为了全绿修改无关业务代码。

## 6. 边界

- 不修改 Core、Outbox、数据库 Schema、检索逻辑。
- 不改变 BusinessView/LifecycleView 的业务交互、分页、筛选、权限语义。
- 不把 P2-B 的 RAG 混合负载延迟（P50 5.3s→11.8s）塞入 P2-D；该项保留为独立 RAG 性能分析事项。
- 本轮审计后按用户 m06395 批准范围实施 P2-D-01～P2-D-05；未修改 Core、Host、数据库 Schema、检索逻辑或视觉交互。

## 7. 实施结果

### 7.1 公共轮询工具

新增 `frontend/src/utils/polling.ts`：

- `DOCUMENT_STATUS_POLL_BACKOFF_MS = [1500, 3000, 6000, 12000, 30000, 60000]`
- `WIKI_STATUS_POLL_BACKOFF_MS = [5000, 10000, 20000, 60000]`
- `MOVE_PROGRESS_POLL_BACKOFF_MS = [2000, 5000, 10000, 30000]`
- `pollingBackoffDelay(consecutiveFailures, schedule)`：按失败次数取序列值并 clamp 到末尾。
- `isPageHidden()` / `addPageVisibilityListener(listener)`：统一读取 `document.visibilityState` 并注册 `visibilitychange`，返回 disposer。

### 7.2 KnowledgeBase.vue

`frontend/src/views/knowledge/KnowledgeBase.vue` 的文档状态、Wiki 状态、移动进度三类轮询均改为 `setTimeout` 自调度：

- 文档状态：正常 1.5s、stalled 15s；失败按 `1.5→3→6→12→30→60s` 退避，任一成功批次重置；保留原有的批量查询、reparse 版本校验、待确认窗口和文档状态判断。
- Wiki 状态：正常 5s；失败按 `5→10→20→60s` 退避，成功后重置；`wikiStatusProbes` 保留原有 500ms/2s/5s/10s 探测节奏并增加隐藏/卸载保护。
- 移动进度：正常 2s；失败按 `2→5→10→30s` 退避，成功后重置；使用 generation token 防止旧任务的响应恢复新任务轮询。
- 三类轮询分别维护 `statusPollFailureCount`、`wikiStatusFailureCount`、`movePollFailureCount`；均增加 in-flight 门、`disposed` 门、隐藏页面门和恢复待处理标记。
- `handlePageVisibilityChange`：隐藏时暂停并清掉三类待执行 timer；恢复时每个仍有任务的任务只立即发起一次刷新，然后恢复自身周期。
- 修复 `updateStatus` 的 `finally`：先判断当前请求身份，再清空 `statusPollRequest`，避免当前请求完成后不能重排下一轮。

### 7.3 全局轮询

- `frontend/src/App.vue`：邀请角标保持 2 分钟周期；隐藏页面停止 timer，恢复时重新启动；登出/卸载清理 timer 与 visibility 监听。`updateCheckTimer` 的 4 小时更新检查未改。
- `frontend/src/components/menu.vue`：session activity 保持 5 秒周期；隐藏页面停止 timer，恢复时立即 `refresh()` 一次；保留只处理 `detached` 会话的 store 条件；卸载清理 timer 与监听。
- `frontend/src/views/knowledge/wiki/WikiBrowser.vue`：Wiki 统计保持 5 秒周期；隐藏停止、恢复立即刷新一次；增加 in-flight/disposed gate；用 `statsPollingActive` 区分真正轮询周期与静默初始加载，只有轮询结束（索引完成）时才重载 pages/selected/graph，避免初次加载重复刷新。
- `GlobalInvitationBell.vue` 已有请求级隐藏保护，保持现状。

### 7.4 未改动范围

按 P2-D-05 与用户要求，以下保持现状：`BusinessView.vue`、`LifecycleView.vue`、`KnowledgeBaseList.vue`、`DataSourceSettings.vue`、`FAQEntryManager.vue`、`GlobalInvitationBell.vue`。

## 8. 验证结果

### 8.1 自动化测试

- `frontend/src/utils/polling.test.ts`：2/2 pass，覆盖三套退避序列、clamp、可见性读取与 listener disposer。
- P2-D 定向 `npx tsx --test src/utils/polling.test.ts src/views/knowledge/KnowledgeBase.test.ts`：21/21 pass；包含文档状态轮询隐藏暂停、可见恢复一次请求的测试。
- 定向 `npx tsx --test src/views/knowledge/KnowledgeBaseBatchReparse.test.ts src/views/knowledge/assistantRefresh.test.ts src/views/knowledge/wiki/wikiBacklinkBrowser.test.ts`：48/48 pass。
- `npm test`：2405 tests、2401 pass、3 fail、1 skipped、duration 33,425ms。3 个失败均为既有环境错误 `POSIX shell tests require the Git for Windows usr/bin shells`：
  - `frontend/src/test-utils/posixShell.test.ts`
  - `frontend/src/views/chat/components/SandboxTerminal.test.mjs`
  - `frontend/src/views/integrations/cliIntegration.test.ts`
- `npm run type-check` exit 0；`npm run build` exit 0（`✓ built in 42.31s`，仅既有 chunk size warning）。

### 8.2 真实 headless Chrome（fixture）

fixture 文件：`frontend/src/utils/polling.browser.fixture.mjs`（仅本地验证，生产不引用）；启动命令：

```bash
POLLING_FIXTURE_PORT=18993 node src/utils/polling.browser.fixture.mjs
```

使用 Chrome CDP（`127.0.0.1:9333`）控制真实页面，fixture 使用真实 `polling.ts` 并对 `/__fixture/hit` 发起真实 HTTP 请求。结果：

- 初始：`doc/wiki/move` 三个任务均有 timer，命中数 `176/53/132`。
- 隐藏后立即：三类 timer 清空；继续等待 5 秒，命中数仍为 `176/53/132`，无隐藏期请求。
- 恢复：三个任务各恰好增加一次，命中数 `177/54/133`；随后 500ms 无额外请求，无并行轮询链。
- 卸载：`disposed=true`，timer 清空；卸载前已有一次正常 doc tick 使 `doc` 从 177 到 178，卸载后等待 2.5 秒命中数保持 `178/54/133`，无卸载后新建 timer 或请求。

## 9. P2-D 验收对照

- [x] 文档解析轮询隐藏后停止。
- [x] Wiki 状态轮询隐藏后停止。
- [x] 移动进度轮询隐藏后停止。
- [x] 页面恢复后每个任务只立即请求一次。
- [x] 三类失败退避分别符合冻结序列（公共工具单测 + 代码路径）。
- [x] 成功后各自重置退避。
- [x] 连续隐藏、恢复不产生重复 timer。
- [x] 请求未完成时不重复发起（in-flight gate）。
- [x] 组件卸载后异步返回不会重新创建 timer（disposed/generation gate）。
- [x] App 邀请角标隐藏时不请求。
- [x] menu 仅在 detached 会话存在时请求（store 条件保持）。
- [x] WikiBrowser 隐藏时停止统计请求。
- [x] 页面重新显示后数据正常恢复。

边界：上述验收覆盖前端轮询调度与真实浏览器 fixture 的请求计数；未做 Core/Host/SQLite 修改，也未改变解析、Wiki 同步、移动任务的状态判断逻辑。`mcpProxy` 在早先全量并发运行中曾 20s 超时，单独运行 2/2 pass，最终全量运行已通过；不作为 P2-D 失败。
