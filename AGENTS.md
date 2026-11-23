# AgentRAG 工作规则

本文件是仓库内 Codex 工作的强制约束。规则归本文件，稳定事实归 `PROJECT_CONTEXT.md`，任务及状态归 `TASK_BACKLOG.md`。

## 1. 开始工作与范围控制

1. 开始前读取本文件、`PROJECT_CONTEXT.md`、`TASK_BACKLOG.md`，以及本次任务涉及的更深层 `AGENTS.md`；执行 `git status --short`、`git diff --stat`，识别并保留已有改动和未跟踪文件。
2. 对照 `BASELINE.txt`、`git rev-parse HEAD`、`git describe --tags --always` 核实源码版本；架构依据是 `docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`。版本变化后重新核实 Hook，不沿用旧行号。
3. 每次写入前先说明具体文件和改动内容。用户要求只读扫描时，只读取和报告；不得顺手修复、格式化或生成源码。
4. 只执行用户本轮授权范围内的任务；backlog 中的条目不自动构成实施授权。想采用超出已授权方案的“更好写法”，先说明并取得用户同意。
5. 完成后执行 `git diff --check`、`git status --short --untracked-files=all`、`git diff --name-only`；逐项核对改动范围，报告验证命令、退出状态及未验证项。

## 2. 核心受控区与独立扩展区

1. `internal/` 等上游核心源码是受控区，绝对禁止重构现有核心业务逻辑，禁止修改原生 RRF、检索、Agent、解析和任务调度算法来承载定制功能。
2. 默认只读扫描核心。只有用户明确授权对应 Hook 接入时，才允许在确认过的调用点做极薄 DTO 转换与本机 HTTP 调用；治理、状态机、持久化、重试及策略必须实现在 `extensions/`。用 `git diff -- internal/` 检查实际补丁。
3. `extensions/` 是独立 Go module：`github.com/ybochuan/LanRAG-Next/extensions`。不得 import WeKnora 的 `internal/` 包，不得复制核心业务逻辑或通过 `replace`、`go:linkname` 等方式绕过边界。
4. 上游根 `go.mod` / `go.sum` 不得因扩展模块引入依赖而改变；不得添加把两模块耦合起来的 `go.work`。在 `extensions/` 单独执行 Go 命令，并用 `go env GOMOD GOWORK` 确认模块归属。
5. 在 `extensions/` 执行 `go list -deps -f '{{.ImportPath}}' ./...`，检查依赖不得出现 `github.com/Tencent/WeKnora/internal/`；检查 `go.mod` 中不得出现指向 WeKnora 的 `require` / `replace`。仅根目录的 `go test ./...` 不会覆盖嵌套扩展模块。
6. Source Mapping 中发现的问题按 Source Mapping / Implementation Difference / Baseline Limitation 记录。不得擅自改写 Frozen Architecture 或假设已经完成 Hook 接入。

## 3. 构建、测试和类型检查

以下命令按改动范围执行。Go 声明版本以各模块 `go.mod` 为准；构建本身承担 Go 类型检查。不得为修复环境问题而擅自升级依赖或修改上游。

| 场景 | 工作目录 | 命令／通过标准 |
|---|---|---|
| 扩展格式检查（只读） | `extensions/` | `gofmt -l .`；无输出 |
| 扩展格式修正 | `extensions/` | `gofmt -w` 后接本次修改的具体 Go 文件；不格式化上游 |
| 扩展构建／类型检查 | `extensions/` | `go build -mod=readonly ./...` |
| 扩展测试 | `extensions/` | `go test -mod=readonly ./... -count=1`；无测试文件时明确报告，不声称已有测试覆盖 |
| 扩展静态检查 | `extensions/` | `go vet -mod=readonly ./...` |
| 独立模块门禁 | `extensions/` | `python scripts/check_module_boundary.py`；拒绝 WeKnora require/replace/import、错误 module 身份及启用的 workspace |
| 模块门禁回归 | `extensions/` | `python -m unittest discover -s scripts -p '*_test.py' -v`；在临时真实 Go module 上运行正反案例 |
| 扩展 race 检查 | `extensions/`，具备 C 编译器的 Linux | `go test -mod=readonly -race ./... -count=1`；不能代替真实 SQLite 并发／事务验证 |
| 扩展启动 | `extensions/` | `go run ./cmd/extension-host` |
| 扩展健康探针（另一个终端） | 任意 | `curl.exe --fail http://127.0.0.1:8090/bridge/healthz`；HTTP 200、JSON `status=ok` |
| 已授权核心改动的定向测试 | 仓库根目录 | `go test -mod=readonly ./实际改动包路径 -count=1`；替换为真实包路径 |
| 上游后端构建（PowerShell） | 仓库根目录 | `go build -mod=readonly -o "$env:TEMP/WeKnora.exe" ./cmd/server`；需要可用的 CGO/C 编译工具链 |
| 上游 Lite 构建（Bash + make） | 仓库根目录 | `make build-lite`；需要 CGO、C 编译器、Node/npm，会构建前端并更新 `web/`，仅经授权执行 |
| 上游完整 Go 测试 | 仓库根目录 | `go test -mod=readonly ./... -count=1`；部分包依赖服务与配置，逐项报告失败 |
| 上游 make 测试（Bash + make） | 仓库根目录 | `make test`；还包含 `scripts/docker-entrypoint_test.sh`，不等同于仅运行 Go 测试 |
| 前端依赖安装 | `frontend/` | `npm ci --no-audit --no-fund`；仅在授权前端任务且缺少依赖时执行 |
| 前端测试 | `frontend/` | `npm test` |
| 前端类型检查 | `frontend/` | `npm run type-check` |
| 前端构建 | `frontend/` | `npm run build`；构建不能代替类型检查 |

当任务仅修改扩展或根文档时，执行扩展检查及范围核对；无需启动上游数据库、LLM 或全量前端构建。任何未执行的上游检查必须如实注明。

## 4. 代码风格与验证纪律

1. Go 代码使用 `gofmt`，遵循所在模块现有命名和注释风格；公开类型、字段和函数注明语义。不要为了复用少量代码引入跨模块耦合。
2. DTO 使用显式 JSON 字段名、独立公开类型；时间使用 `time.Time` 并约定传输时间为 UTC。不要把核心模型类型暴露到扩展接口。
3. 只添加当前任务所需功能；禁止顺手整理无关代码、批量改名、调整目录或格式化整个仓库。用 diff 验证每个改动都有对应任务依据。
4. 针对业务行为、边界和故障恢复采用有意义的测试；不得只测实现细节或为简单声明堆叠镜像测试。HTTP 骨架至少实际启动并检查探针和监听地址。
5. “通过”“完成”必须有本轮完整命令及退出状态支持；无测试文件、未运行、依赖缺失、基线失败分别说明，不能互相替代。

## 5. 安全与冻结边界

1. Bridge 默认只绑定 `127.0.0.1:8090`，不得改成 `:8090`、`0.0.0.0` 或公网监听；健康接口不得输出配置、凭据、知识库或文档内容。
2. 四类业务 Hook 后续接入时使用 `X-AgentRAG-Bridge-Version: 1`，不支持的 major version 必须快速拒绝；健康探针不代表业务 Hook 或持久层已就绪。
3. `before_retrieve` 不可用时必须 fail-closed；`after_retrieve` / `before_generate` 后续按冻结方案 fail-open。用户2026-10-30批准M0默认Pass-Through：合法且显式非空的原生授权文档范围返回200，不新增治理排除；复杂过滤、完整active/直接读/图谱门禁后移，不可宣称已生效。原生M0门卫只检查HTTP状态，默认透传对空文档范围必须直接返回非200，不能只返回200+deny_all；不得回退whole-KB。基线未贯通 `excluded_document_ids`，不得只发送字段就当作过滤生效。
4. 用户已确认M0不改原生数据库，以周期对账兜底Lite内存队列。必须上报失败、周期重试，区分reconcile观察与原始事件；不得声称恢复了无法观察的中间历史。扩展事务尚未持久提交时，ingest端点不得返回成功ACK。
5. 扩展标签投影共用单写者队列，当前truth读取、ID解析、读取/合并/替换/回读属于同一队列操作。只修改目标gov命名空间，以KB已解析的原生tag ID写入。原生`tags:null`或缺失数组表示未知，禁止当空数组全量替换；记录失败供Outbox重试（C-10）。用户已接受该安全拒绝并将C-10归为Baseline Limitation；后续独立标签只读API另行接入，不得为追求同步而删除用户标签。队列不保证原生UI/自动标签的并发安全，必须保留该限制，不要求M0擅加上游CAS。
6. 生命周期 event + current + outbox 同事务提交，`last_event_id` 必须有 FK；治理状态与 audit 同事务；critical lineage edge 与相应治理事实同事务。验收需能注入崩溃并检查数据库状态。
7. Revision Publish 采用幂等 Saga，远端结果未知时先 reconcile，再决定是否重试 ingest；不得直接重放副作用。反馈只写指导层，不修改原始知识或事实索引。
8. 投影不是事实源；投影失败不得改变 lifecycle / lineage / governance 的 truth。独立解析代理已由用户明确放到后续阶段，不能混入M0或重写原生解析器。
9. MCP 使用上游原生按需发现；禁止为了挂载扩展重构 `ToolRegistry` 或启用 stdio。mDNS 广播不包含知识库 ID、名称或领域；跨库语义关联需用户明确确认。
10. 不在输出、日志或提交中暴露 `.env`、token、密钥及用户数据。不擅自删除原件、迁移真实数据、执行破坏性命令或提交／推送；保留用户已有改动。

## 6. M2真实策略接管（2026-10-31用户新增授权）

1. M0默认透传决定保留为历史验收；当前Host worker必须使用持久DB策略，probe无业务DB时before_retrieve返回503，不得回退Pass-Through。未有生命周期记录或任意非active文档均排除。
2. 原生before_retrieve仍固定回环、500ms、非200/传输/响应解码失败一律fail-closed；必须验证响应trace/tenant/KB和允许/排除集合为原授权范围的完整分割。deny_all不得进入检索或回退whole-KB。
3. 用户批准的原生补丁范围是Bridge DTO解析和SearchParallel结果过滤；过滤chunk/entity合并结果，显式排除优先，普通文档还必须属于返回允许集；原生WebSearch类型结果保留。不得据此宣称Agent直接读、旧handle、图谱独立入口或生成过程中停用已受控。
4. pending/scheduled/active超过effective_until可直接stale；completed观察按日期初始化pending后active/scheduled/stale，不复活已有失效状态。scheduled到期激活必须在同一决策事务内确认最新持久观察为completed，缺失/failed/processing拒绝；不限制过期转stale。每日任务启动先补扫、默认间隔24h，取消时join后再关DB。

## 7. M2 模型来源治理（2026-11-01 用户授权补充）

1. 用户已扩围批准摘要、Agent search/read/list/schema/analysis、原生MCP文档读取的薄校验；第6.3条“Agent未接入”属于此前批次边界。新入口必须先保留原生权限约束，再通过现有before_retrieve协议获取许可，禁止把原生范围扩成whole-KB。
2. 文档正文、标题、描述进入工具结果、重排或模型上下文前应服从当前active许可；协议仍固定回环、500ms、严格响应校验、失败关闭。不可使用测试放行夹具启动生产服务。
3. 用户接受M2拒绝无完整来源的历史回答/压缩/恢复、Wiki/图谱混合内容。原生页面快照、旧答案来源记忆背景、任意SQL/会话检索、通用操作及不透明外部MCP结果按此拒绝或省略；存储不删除。恢复能力须先补齐来源链并验证撤销。
4. 不宣称普通REST管理、已签发资源权限、显式复制到工作区/文件/记忆的副本、用户附件或生成过程中的持续撤销已受控。测试须区分当前来源门禁与内容级删除。
5. 回归运行 `python3 extensions/scripts/governance_e2e.py`（8090需空闲，或按运维文档使用隔离网络空间）；检查active正向、真实stale迁移、同会话/已知ID负向、断Host答案生成零次。模型夹具必须捕获所有请求与重排输入，正文/描述/旧答案标记独立，不能只检查最终答案。

## 8. 前端 UI 规范：以 WeKnora 原生 TDesign 体系为唯一标准（2026-11-17 用户授权）

适用范围：`frontend/` 下全部自建页面（含文档工作台 `views/lifecycle/`、业务视图 `views/business/`、版本族 `views/families/`、关系图 `views/lineage/`）。

### 8.1 事实基准（从原生代码实测，不得凭印象改）

- 原生 UI 组件库是 `tdesign-vue-next`（`frontend/package.json`），并在 `main.ts` 通过 `app.use(TDesign)` 全局注册；页面可直接写 `<t-button>` / `<t-select>` / `<t-input>` / `<t-table>`，无需逐个 import。
- 原生页面实测组件密度：`views/knowledge/KnowledgeBaseList.vue` 有 97 个 `<t-*>`、8 个原生控件；`views/knowledge/KnowledgeBase.vue` 有 42 个 `<t-*>`。自建工作台整改前为 0 个 `<t-*>`、约 200 个原生控件。
- 字体：`--app-font-family` 经 theme.css 映射为 `--td-font-family`；`App.vue` 全局 `font-family: var(--app-font-family)`。
- 背景：`App.vue` 全局 `background: var(--td-bg-color-page)`；卡片/面板用 `--td-bg-color-container`，次级容器（表头、分组）用 `--td-bg-color-secondarycontainer`。

### 8.2 强制规则

1. **禁止裸控件**。不得在业务页面直接写 `<select>`、`<input>`、`<button>`、`<textarea>` 作为交互控件；一律改用 TDesign：下拉 `<t-select :options="…">`、文本 `<t-input>`、按钮 `<t-button>`、多行 `<t-textarea>`、开关 `<t-switch>`、弹层 `<t-dialog>`/`<t-drawer>`、标签 `<t-tag>`、加载 `<t-loading>`。
2. **禁止自造按钮样式**。不得新增 `.primary` / `.secondary` / `.text-button` 这类仿制按钮类；用 `<t-button theme="primary">`、`<t-button variant="outline">`、`<t-button variant="text">`。
3. **字号只用 TDesign 阶梯**：正文 `--td-font-size-body-medium`(14px)、次要 `--td-font-size-body-small`(12px)、区块标题 `--td-font-size-title-medium`(16px)、页面标题 `--td-font-size-title-large`(20px)。禁止再用 `--app-text-sm` 把正文压到 12px。
4. **圆角只用 TDesign 阶梯**：控件 `--td-radius-default`(3px)、卡片/面板 `--td-radius-medium`(6px)、抽屉/弹窗 `--td-radius-large`(9px)。禁止 `--app-radius-xl`(12px) 这类超规格圆角。
5. **颜色只用语义 token**：品牌 `--td-brand-color`；文字 `--td-text-color-primary` / `-secondary` / `-placeholder`；描边 `--td-component-stroke`；背景 `--td-bg-color-page` / `-container` / `-secondarycontainer`。禁止硬编码 hex。
6. **悬浮态统一**：行内含文档名等文本触发元素时，行**不得**加 hover 底色——单行文字下面铺一块圆角色块会被读成输入框；只保留 `cursor:pointer`，选中态才允许填色。多列信息行（原生 `DocumentListView` 的 `.doc-list-row`）可用 `background: var(--td-bg-color-secondarycontainer)` + `border-radius: var(--td-radius-medium)` + 内边距。禁止用 `--td-bg-color-container-hover` 铺在无圆角、无内边距的行上。

7. **表格**：优先 `<t-table>`；若因排序/可访问性保留语义 `<table>`，必须复刻 TDesign 表格视觉——表头 `--td-bg-color-secondarycontainer` + `--td-text-color-secondary`、行分隔 `--td-component-stroke`、行悬停同样遵循第 6 条（`secondarycontainer` + 圆角），单元格内边距与字号对齐 TDesign。
8. **控件尺寸统一**：默认 `size="medium"`（32px 高）；表格内操作按钮 `size="small"`。禁止用 padding 手工拼高度。
9. **页面骨架**：根容器不自行绘制页面底色（跟随平台外壳），内容卡片 `background: var(--td-bg-color-container)` + `border: 1px solid var(--td-component-stroke)` + `border-radius: var(--td-radius-medium)`；页面左右留白 24–40px。

### 8.4 原生页面骨架（强制，后续所有页面照此实现）

事实基准：`views/settings/ModelSettings.vue` 的 `.section-header` + `.section-header__top` + `.section-description`；`views/settings/Settings.vue` 的 `.content-wrapper > .section`；`views/knowledge/KnowledgeBaseList.vue` 的 `--td-bg-color-container` 卡片。不得自造另一套页面外壳。

1. **根容器**：平台级页面用 `<section class="section ...">`，不写 `<main class="page-...">`；不设 `max-width` 居中、不自定义页面底色，跟随平台外壳；只保留滚动所需的 `height/min-height/overflow`。嵌套设置页继续用 `.content-wrapper > .section`。
2. **页头**：结构固定为 `.section-header > .section-header__top + .section-description`。标题用 `<h2>` 且必须 `$t()`，描述用 `<p class="section-description">` 且必须 `$t()`。操作按钮放进 `.section-header__top`，统一 `<t-button>`；禁止 `.page-heading` / `.page-subtitle` / `.title-block` 等自造页头类。
3. **内容分区**：每个独立功能块用 `<t-card :bordered="true">` 或 `.section`；禁止 `<article class="card">` + 手写 border/background 冒充卡片。卡片内边距通过 `:deep(.t-card__body)` 设置，值取 TDesign 阶梯；状态边框只用 `--td-*` 语义 token。
4. **表单**：一律 `<t-form label-align="top" @submit.prevent="...">` + `<t-form-item label="..." name="...">`；禁止 `<form class="...">` + `<label>` 手写字段，禁止用 padding 拼控件高度。表单项之间的间距交给 `.form-grid` 或 TDesign 默认值。
5. **文案与 i18n**：新增或改动的可见文案必须 `$t()`，键同时加入 zh-CN / zh-TW / en-US / ru-RU / ko-KR / ja-JP 六个 locale 且键集完全一致（`localeKeyAudit.test.ts` 会拦截缺失/多余）；zh-CN、en-US 必须实译，其余语种提供对应语言译文，不得留空或复制中文。存量页面本次只迁移骨架时，已有硬编码文案可暂时保留，但不得新增硬编码文案。
6. **状态反馈**：错误/提示用 `<t-alert theme="error|info|warning">`，状态用 `<t-tag>`，空态用 `<t-empty>`，加载用 `<t-loading>`；禁止手绘 `.notice` / `.error` 色块或硬编码 hex。
7. **弹层归属**：抽屉/弹窗内再做操作时，`<t-dialog>` 必须 `attach="body"`；成员/更替/关联等表单优先内联展开或独立抽屉，禁止把页面级工作区塞进单文档详情抽屉形成“窗体套窗体”。

### 8.3 验收门禁

- 每个自建页面必须通过：`npm run type-check`、`npm test`、**`npm run build`**。
- **模板编译错误（如 `v-else-if` 失去前置 `v-if`）只有 `npm run build` 会报，`vue-tsc` 查不出来**；因此前端改动必须跑 build，不得只跑 type-check。
- 构建产物必须部署到 `WEKNORA_WEB_DIR`（生产为 `/mnt/d/LanRAG-Next/web`，gitignored），并在浏览器硬刷新后核对；只提交源码不等于上线。
- 新增页面验收时抽查：页面内 `<select>`/`<input>`/`<button>` 出现次数应为 0（由 `<t-*>` 替代）；CSS 中不应出现新增硬编码颜色。
- 骨架抽查：页面内 `.page-heading` / `.page-subtitle` / `<article class="card">` / 裸 `<form class="...">` 出现次数应为 0；页头必须是 `.section-header__top + .section-description`，卡片必须是 `<t-card :bordered="true">`，表单必须是 `<t-form>` + `<t-form-item>`。
- i18n 抽查：新增键必须在六个 locale 中键集一致；本地化审计测试（`localeKeyAudit.test.ts`）必须通过。

### 8.5 多语种与原生语言/字体适配（强制，2026-11-23 用户授权）

适用范围：`frontend/` 下全部自建页面，重点包括 `views/lifecycle/LifecycleView.vue`、`views/lifecycle/AttachmentWorkbench.vue`、`views/lifecycle/SupplementEditor.vue`、`views/model-supervisor/ModelSupervisorView.vue`，以及 `views/lifecycle/model.ts`、`api/modelSupervisor.ts` 等文案来源。

1. **所有可见文案必须国际化**：模板、脚本生成的按钮/选项/标签、`aria-label`、`placeholder`、`title`、空态、确认框、Toast、错误提示统一使用 `$t()` 或 `useI18n().t()`；不得在 `.vue` / `.ts` 中硬编码中文或英文界面文案。真实业务数据（如知识库文件夹名“附件”、文档标题、模型名）不属于 UI 文案，不得翻译。
2. **六语键集一致**：新增或修改文案必须同时写入 `frontend/src/i18n/locales/{zh-CN,zh-TW,en-US,ru-RU,ko-KR,ja-JP}.ts`，键集完全一致；zh-CN、en-US 必须实译，其余语种提供对应语言译文，不得留空或复制中文。`npm run check-i18n` 与 `localeKeyAudit.test.ts` 必须通过。
3. **脚本内文案同样走 t()**：`stateLabels`、`taskTypeOptions`、状态/任务/可达性/错误映射、复制成功/失败提示等，必须在组件内用 `computed` / `t()` 生成；不得在模块顶层导出写死的中文常量给页面直接渲染。
4. **时间与区域格式跟随界面语言**：不得硬编码 `toLocaleTimeString('zh-CN')` / `toLocaleDateString('zh-CN')`；使用当前 `locale.value` 或 i18n 的日期时间格式化。数字、单位、文件名、路径保持原值，不翻译。
5. **错误码与文案分离**：API/服务层抛错时携带稳定错误码（如 `SupervisorError` 的 `file_pick_unavailable` / `not_ready` / `unreachable` / `desktop_required`），由页面统一映射到六语词条；禁止把后端中文 `Error.message` 直接渲染给用户。
6. **语言/字体继承原生**：不覆盖 `font-family`，统一继承 `--app-font-family` / `--td-font-family`；字号、圆角、颜色使用 TDesign token（见 8.2）；错误/提示使用 `<t-alert>`，禁止手写 `.token-error` / `.notice` / `.error` 色块。
7. **长文本适配**：英文、俄文文本普遍长于中文；固定宽度侧栏、表格列、按钮和卡片必须允许换行、省略或提供 `title` / tooltip，不得以中文长度估算宽度。语言切换后必须复核主要页面无溢出、无截断、无中英混排残留。
8. **验收**：除 8.3 门禁外，多语改动必须用真实桌面端或与桌面端同源的构建产物，至少核对 zh-CN 与 en-US 两个语言下的文档工作台、模型进程管理页面；切换语言后页面标题、页头、标签、按钮、空态、错误提示和日志时间全部使用目标语言。仅替换 locale 而不部署构建产物不算完成。
