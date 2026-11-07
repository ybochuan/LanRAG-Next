# P2-C 前端请求链路审计（2026-11-07）

## 范围与方法

本报告是 P2-C 的审计与实施记录，覆盖 `BusinessView.vue`、`LifecycleView.vue`、`frontend/src/api/business.ts`、`frontend/src/api/lifecycle.ts`、`frontend/src/api/knowledge-base/index.ts` 和 `frontend/src/utils/request.ts`。审计阶段未修改前端源码，未请求生产接口；实施结果见下文。

审计目标：统计页面加载和交互中的请求数量，画出调用关系，为最小改造方案提供基线。

## 现有请求路径

### BusinessView

- `BusinessView.vue:117-124`：`loadKBs()` 调 `listKnowledgeBases()`，成功后设置当前 `kb`。
- `BusinessView.vue:122`：`watch([kb, kind], resetScope)`；`resetScope()` 清空状态后调用 `refresh()`。
- `BusinessView.vue:83`：`refresh()` 调 `loadAuthority()` + `loadRecords()`；有选中记录时再 `openRecord`。
- `BusinessView.vue:78-82`：`loadAuthority()` 调 `overview(scope,1,'')`，只用于 `can_write` 和第一页标题，且每次切换业务 tab 都会重复执行。
- `BusinessView.vue:72-77`：`loadRecords()` 调 `api.list(scope,kind,page)`。
- `BusinessView.vue:85-90`：`openRecord()` 先 `api.list(scope,kind,1,row.id)`，随后对 `document_id/source_document_id/target_document_id` 逐个 `api.documentTitle()`；最多 3 个额外请求。
- `frontend/src/api/business.ts:49-52`：`documentTitle()` 每次 GET `/api/v1/knowledge/{id}`。
- 业务 tab 切换（`associations`/`feedback-governance`/`revisions`）会触发 `kind` 变化；在路由复用组件时执行 `resetScope()` → `refresh()`。

### LifecycleView

- `LifecycleView.vue:77`：`loadKnowledgeBases()` 调 `listKnowledgeBases()`。
- `LifecycleView.vue:76`：`watch(kb)` 重置状态并 `loadAll()`。
- `LifecycleView.vue:66`：`loadAll()` 同时 `loadOverview()` 和 `loadReviews()`，即使当前 tab 是“文档状态”也会请求 Reviews。
- `LifecycleView.vue:59-63`：`loadOverview()` GET `/api/v1/knowledge-bases/{kb}/lifecycle?page=&page_size=20&state&title`。
- `LifecycleView.vue:65`：`loadReviews()` GET `/reviews`。
- `LifecycleView.vue:75`：标题输入 250ms 防抖，状态筛选立即触发 `loadOverview`。
- `LifecycleView.vue:70-71`：打开文档抽屉时 `documentDetail()`；保存日期后 `loadOverview()` + `loadReviews()`。
- `LifecycleView.vue:18-27`：模板以 `v-else-if="data"` 包住全部 tabs 内容；`loadOverview` 失败会隐藏 Reviews 与规则，`busy` 还会阻塞整页。

### 公共 API 层

- `frontend/src/api/knowledge-base/index.ts:33-52`：`listKnowledgeBases()` 直接 `get('/api/v1/knowledge-bases')`，无缓存/去重。
- `frontend/src/utils/request.ts:233-235`：`get()` 直接走 Axios，无请求去重或 TTL 缓存。
- `frontend/src/api/knowledge-base/index.ts:667-673`：已有 `batchQueryKnowledge(idsQueryString, kbId?)`，请求 GET `/api/v1/knowledge/batch`；后端路由 `internal/router/routes_knowledge.go:131`，处理器 `internal/handler/knowledge.go:2158-2260`，响应 `data: []types.Knowledge`，含 `id`、`title`、`knowledge_base_id`（`internal/types/knowledge.go:167-179`）。可直接用于批量标题查询。

## 请求次数基线

以下为静态代码推导的最小请求数；“页面切换”按组件重新挂载计算。

| 场景 | 当前请求数 | 说明 |
|---|---:|---|
| Business 首次进入（有 KB） | 3 | KB 列表 1 + overview 1 + 业务列表 1 |
| Business 首次进入（无 KB） | 1 | 仅 KB 列表 |
| Business 切换业务 tab | 2 | 当前实现重复 overview + 业务列表 |
| Business 打开记录详情 | 1 + N | 记录详情 list 1 + 每个文档标题单请求，最多 3 |
| Business 分页 | 1 | 仅业务列表 |
| Business 刷新 | 2（+详情） | overview + 业务列表；有详情时再 1+N |
| Lifecycle 首次进入（有 KB） | 3 | KB 列表 1 + overview 1 + reviews 1 |
| Lifecycle 切换 tab | 0 | 数据已预加载 |
| Lifecycle 标题搜索/状态筛选 | 1 | 每次 overview |
| Lifecycle 打开文档 | 1 | documentDetail |
| Lifecycle 保存日期 | 3 | PUT + overview + reviews |
| Lifecycle 刷新 | 2 | overview + reviews |
| Lifecycle → Business 页面切换 | 各自重新挂载 | KB 列表、overview 等全部重新请求，无跨页缓存 |

## 问题定位

- **C-01（标题 N+1）**：`BusinessView.openRecord()` 逐个调用 `documentTitle()`；每页记录列表也没有批量标题，非第一页文档只有“文档 xxxx”回退。已有 `/api/v1/knowledge/batch` 可直接批量补齐。
- **C-02（重复 KB 列表）**：`BusinessView` 与 `LifecycleView` 各自 `onMounted` 调 `listKnowledgeBases()`，页面切换即重复请求；API 层没有 in-flight 去重或短 TTL 缓存。
- **C-03（页面加载）**：`BusinessView` 每次切换业务 tab 都通过 `resetScope()` 重拉 overview，但 `can_write` 在同一 KB 内不会变化；`LifecycleView.loadAll()` 无条件预加载 Reviews，且过滤/分页只应刷新文档数据。
- **C-04（请求缓存）**：无任何页面级/API 级缓存；KB 列表、同页 overview 在快速切换 KB 或页面间重复读取。
- **C-05（竞态）**：两页已有 `requestFence`，KB 切换会 invalidate 旧 token；但 `BusinessView` 的 `loadAuthority`/`loadRecords`/详情标题请求使用不同 fence，标题批量请求需要自己的 token，防止旧 KB 响应写回标题 Map。
- **C-06（错误隔离）**：`LifecycleView` 的 `v-else-if="data"` 与全局 `busy` 使 Overview 失败阻断 Reviews/规则；`BusinessView.loadAuthority()` 的错误写入共享 `error`，也会影响业务列表提示。

## 最小改造方案（已实施）

1. **公共请求缓存与去重**：新增 `frontend/src/api/requestCache.ts`（TTL + in-flight 去重 + 前缀失效），用于批量标题（60 秒）；`listKnowledgeBases` 使用文件内本地 10 秒缓存（避免测试夹具跨模块依赖），`DocumentPicker` 的 active 文档列表使用 5 秒缓存（key 含 tenant/KB）。缓存失效：KB 增删改/复制/置顶后失效 KB 列表；文档手动更新/删除/批量删除后失效标题与 DocumentPicker；生命周期保存后失效 DocumentPicker；标题查询对 ID 排序归一，key 含 tenant。
2. **C-01 批量标题**：`frontend/src/api/business.ts` 增加 `documentTitles(kb, ids)`，调用已有 `batchQueryKnowledge`；`BusinessView` 的列表加载与详情打开都只发 1 次批量标题请求，最多一次补齐所有缺失标题。
3. **C-02/C-04**：`listKnowledgeBases` 走 10 秒缓存（key 包含 tenant/参数）；Business 与 Lifecycle 页面切换复用结果；DocumentPicker 三个相同 active 文档请求降为 1 个。
4. **C-03**：`BusinessView` 拆分 `watch(kb)` 与 `watch(kind)`：KB 变化才重载 overview；业务 tab 变化只重载当前业务列表。`LifecycleView` 只在 Reviews tab 激活时首次加载 Reviews；文档筛选/分页只刷新 overview。
5. **C-05**：为标题 Map 增加独立 `titleFence`，在 KB 切换/组件卸载时 invalidate；批量标题返回后校验 KB 和 token。
6. **C-06**：`LifecycleView` 将 tabs 固定在 `kb` 存在时显示，documents/reviews/rules 分别按 `data` 条件显示；`BusinessView` 把 `authorityError` 与列表 `error` 分离，overview 失败只影响写权限提示，不阻断记录列表。

## 实施后验证结果

### 自动化测试

- `npm run type-check`：exit 0。
- `npm test`：2402 项，2398 pass、1 skipped；3 个失败均为环境性 `POSIX shell tests require the Git for Windows usr/bin shells`（`src/test-utils/posixShell.test.ts`、`src/views/chat/components/SandboxTerminal.test.mjs`、`src/views/integrations/cliIntegration.test.ts`），不是本次改动引入。
- `frontend/src/api/cacheConsistency.test.ts`：8/8 pass，覆盖 in-flight 去重与失败清理、in-flight 期间失效不残留旧缓存、TTL/前缀失效、批量标题排序归一、KB 列表 tenant 隔离与增删失效、文档更新/删除后标题与 DocumentPicker 失效、DocumentPicker tenant/KB 缓存键、生命周期保存后 DocumentPicker 失效。
- `frontend/src/views/business/component.test.ts`：7/7 pass，覆盖批量标题唯一 ID 请求、Overview 权限错误隔离、旧 KB/旧业务 Tab 响应不覆盖新状态。
- `frontend/src/views/lifecycle/component.test.ts`：2/2 pass，覆盖旧 KB 的 Overview/Reviews 响应不覆盖新 KB。
- 定向测试合计：17/17 pass。
- `npm run build`：exit 0（最终代码构建完成）。

### 基线确认

- 在临时 worktree `D:\LanRAG-Next-baseline`（detached HEAD `cd6514e`，与 P2-C 基线一致）运行 `npx tsx --test src/test-utils/posixShell.test.ts src/views/chat/components/SandboxTerminal.test.mjs src/views/integrations/cliIntegration.test.ts`：12 tests、9 pass、3 fail，三个失败均报 `POSIX shell tests require the Git for Windows usr/bin shells`，与当前工作区一致，确认是既有环境问题；临时 worktree 已删除。

### 真实浏览器验证（fixture，headless Chrome，`http://127.0.0.1:18991`）

BusinessView 单次 `/platform/associations` 加载请求：

- KB 列表 1 次
- lifecycle overview 1 次
- business relation_list 1 次
- DocumentPicker active 文档 overview 1 次（原组件三个实例会重复 3 次）
- 批量标题 `/api/v1/knowledge/batch` 1 次（无逐文档 GET）

切换“反馈与指导”只新增 `feedback_list` 1 次；切回“文档关联”只新增 `relation_list` 1 次，未重复 KB 列表或 overview。

LifecycleView `/platform/lifecycle` 初始请求：KB 列表 1 次 + overview 1 次，无 reviews；点击“待复核”后才新增 `/reviews` 1 次。

`?failOverview=1` 模拟 Overview 503：页面显示错误提示，但 tabs 仍可见，点击“待复核”可正常打开独立 Reviews 面板，证明 C-06 错误隔离。

## 验收指标

| 指标 | 目标 |
|---|---|
| Business 切换业务 tab | 2 → 1 请求（仅业务列表） |
| Business 打开记录 | 1 + N → 2 请求（详情 + 1 次批量标题） |
| Lifecycle 首次进入 | 3 → 2 请求（Reviews 懒加载） |
| 同一加载周期 KB 列表 | 1 次（缓存/去重命中时为 0） |
| Lifecycle Overview 失败 | Reviews 与规则仍可独立显示/重试 |
| 权限、分页、筛选、业务交互 | 语义不变 |
| 前端测试 | 单测、`vue-tsc --build`、`vite build` 全通过 |

## 边界

- 不修改 Core、Host、SQLite、WAL、Outbox 或后端接口契约。
- 不改变页面视觉、路由和用户操作流程；仅减少重复请求和改善局部加载状态。
- 真实浏览器验证使用测试知识库/测试记录，不对正式制度文件执行写操作。
