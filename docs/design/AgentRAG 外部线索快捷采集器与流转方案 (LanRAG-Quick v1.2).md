# AgentRAG 外部线索快捷采集器与流转方案 (LanRAG-Quick v1.2)

> 状态：设计草案。本次修订以复用 WeKnora 原生能力为主，实施需另行授权。
> 基线：WeKnora `v0.9.0`（`f17ee8a`，见 [BASELINE.txt](../../BASELINE.txt)）。本次新增的 LanRAG-Next 接口核对至 `f16263be`。
> 上位约束：[AGENTS.md](../../AGENTS.md) 与 [Frozen Architecture](<AgentRAG 技术方案 v1.0 — Frozen Architecture.md>)。v1.1 仅作历史记录。

## 1. 目标与范围

LanRAG-Quick 是 LanRAG 的独立浏览器知识助手。首版在同一个 Chrome 侧栏提供 **网页剪藏、Markdown 速记、知识库问答**。剪藏和速记共用 Quick 的暂存与审批，审批后复用 WeKnora 的 Markdown 文档、标签、解析和索引。问答直接复用 LanRAG 已有的会话、检索治理和答案引用。

| 首版入口 | 可用功能 | 操作结果 |
|---|---|---|
| **剪藏** | 右键划选送审，或在侧栏预览并提交当前页正文、选区或链接。记录标题和来源 URL。 | 进入 Quick 待审队列。只收藏链接时按链接笔记显示。 |
| **速记** | 在侧栏编辑、预览 Markdown，保存本机草稿，按需带上当前页出处。 | 草稿留在侧栏。点击「送审」后进入同一待审队列。 |
| **问答** | 在侧栏选一个授权知识库，发起快速问答、查看流式答案与引用、在同一会话追问。 | 显示答案及原始引用，可跳转 LanRAG。切换知识库开启新会话。 |

首版提供打开侧栏的 Chrome 快捷键。AI 智能提取、复杂网页与图片归档、服务端 URL 抓取、文件导入、系统级热键和托盘留待后续评估。普通直接收藏仍可使用 WeKnora 官方插件。

四项验收约束：

1. **审批前零入库。** 目标知识库无对应原生文档，也无解析或索引任务。原生草稿只能在审批后创建。
2. **审批绑定正文。** 预览对应一份 UTF-8 Markdown 快照。创建原生草稿后回读正文核对字节数和 SHA-256，通过后才提交发布。原生入库使用审批正文，来源 URL 作为出处保存。
3. **审批绑定身份和作用域。** 服务端核对当前登录用户、租户、目标库和写权限。审批与入库意图同一扩展事务提交，原生写操作使用当前审批人的 JWT。
4. **结果可核对。** 创建或发布结果不明时，优先核对同一条线索及其原生目标。确认存在的草稿继续使用同一个 `document_id`。正文、标签或身份校验失败时停在待核对状态。

## 2. 原生能力复用

| 能力 | 判断 | 使用方式与边界 |
|---|---|---|
| 官方 Chrome「知识管理助手」 | **KEEP** 用于普通收藏，**参考**产品能力 | 以剪藏、速记和侧栏问答为产品参考。商店版连接实例后将剪藏写入所选知识库。当前基线未找到插件本体的可复用源码或自定义暂存契约。Quick 独立实现侧栏。 [官方剪藏说明](../../website-docs/05-clients/06-chrome-extension.md) |
| BrowserSkill 扩展 | **参考**扩展工程 | 以本仓库锁定的 BrowserSkill v0.3.1 为参考，查看扩展构建、UI 与后台入口的划分、消息传递和连接反馈。Quick 按用户主动操作配置所需权限，首版交付独立扩展，不依赖 BrowserSkill daemon、配对或 Agent 浏览器控制。 [WeKnora 锁定版本](../../scripts/browserskill-release.json) · [BrowserSkill 扩展源码](https://github.com/Tencent/BrowserSkill/tree/da6bf4eed2dd7256567e152df8c903c87f6598c3/apps/extension) · [许可](https://github.com/Tencent/BrowserSkill/blob/da6bf4eed2dd7256567e152df8c903c87f6598c3/LICENSE) |
| 原生会话、快速问答与引用 | **KEEP** 用于侧栏问答 | 复用 `POST /api/v1/sessions` 和 `POST /api/v1/knowledge-chat/{session_id}`。按现有 SSE 事件渲染答案和来源，沿用 LanRAG 的权限与生命周期门禁。 [会话与问答 API](../../website-docs/04-api/02-api-chat.md#L24) · [原生问答路由](../../internal/router/routes_chat.go#L150) |
| 原生 URL 导入 | **REJECT** 用于审批前暂存 | 原生先建文档再入解析队列。普通直存仍使用已有能力。受控流程保存来源 URL 和已采集正文。 [URL 创建](../../internal/application/service/knowledge_create.go#L333) |
| 原生 Markdown 草稿与发布 | **KEEP** 用于审批后 | `POST /api/v1/knowledge-bases/{kb}/knowledge/manual` 创建 `draft`。核对通过后用 `PUT /api/v1/knowledge/manual/{id}` 把相同正文提交为 `publish`，沿用原生索引任务。保存草稿本身已经创建文档。 [原生路由](../../internal/router/routes_knowledge.go#L73) · [DTO](../../internal/types/knowledge.go#L327) |
| 原生会话、库权限和标签 | **KEEP** | 调用 `/api/v1/auth/me` 和目标库 `/lifecycle/authority` 核验当前用户与权限。用原生标签 ID 创建草稿，再回读绑定结果。 [认证路由](../../internal/router/routes_auth_tenant.go#L235) · [库权限路由](../../internal/router/routes_knowledge.go#L235) · [标签读取](../../internal/handler/knowledge.go#L683) |
| 修订发布与对账 | **ADAPT** | 沿用先持久化意图、写入可核对标识、回读正文、绑定文档 ID 后继续发布的做法。Quick 单独保存线索意图，现有修订提案的来源、替代和审批规则继续按原业务执行。 [现有发布适配器](../../extensions/business/native.go#L70) |
| 解析状态、生命周期、补充关系 | **KEEP** | Quick 只读现有导入观察，文档详情链接到生命周期工作台。补充关系经现有 `supplements` 管理入口维护。 [导入观察](../../extensions/ingest/observer.go#L93) · [生命周期 API](../../frontend/src/api/lifecycle.ts#L31) |

## 3. 侧栏、暂存与审批

### 3.1 侧栏与浏览器权限

- 使用 Chrome Manifest V3 的 `sidePanel`。工具栏、右键菜单和 Chrome 扩展快捷键打开侧栏。当前页选区及正文只在用户明确发起剪藏后读取。浏览器侧只申请当前标签页和已配置本机服务所需权限，不监听全站页面内容。
- 侧栏使用独立的 LanRAG-Quick 品牌。网络请求由扩展上下文发往配置的本机 Core 或 Quick 服务。网页脚本与采集内容只携带待处理的数据，接触不到这两类凭据。出站凭据只发往各自的固定地址。 [Chrome 侧栏](https://developer.chrome.com/docs/extensions/reference/api/sidePanel) · [Chrome 快捷键](https://developer.chrome.com/docs/extensions/reference/api/commands) · [Chrome 扩展网络权限](https://developer.chrome.com/docs/extensions/develop/concepts/network-requests)

### 3.2 剪藏、速记与快照

- **网页剪藏**。选区直接整理为 Markdown。当前页正文从当前已加载的页面提取可见文章文本，预览后提交。正文提取失败时，提示改用划选或由用户明确选择「只存链接」。只存链接生成含标题和 URL 的 Markdown 条目，显示为链接笔记。三种方式均不触发 Core 重新抓取。
- **Markdown 速记**。侧栏提供编辑和预览，在当前浏览器配置中保存未送审草稿。可显式附上当前页出处。送审时生成不可变的待审快照；浏览器本地草稿和待审快照均不会直接进入问答上下文。
- 平台粘贴入口继续用于手动投递。网页剪藏、速记和粘贴都走同一份快照、审批与发布流程。
- 固定记录的租户与目标库。粘贴入口用当前登录用户的权限选库。浏览器扩展的投递凭据绑定服务端配置的目标库，客户端传入的租户与库 ID 不授予访问权。
- 将正文整理为供审批的 UTF-8 Markdown，落在 Host 数据目录的 `clips/{clip_id}.md`。服务端生成不可预测的 `clip_id`，文件名与标题无关。临时写入、同步并重命名后，记录哈希、字节数和落盘时间。快照与记录均持久化成功后才返回采集成功。
- 快照目录仅本机服务用户可读写，纳入忽略规则，并核实实际路径位于 Agent 工作区根以外。审批预览按固定快照读取，以安全的文本或 Markdown 渲染呈现。来源 URL 仅作出处，不因预览或入库发起远端抓取。 [副本治理设计](M2-Copy-Lineage-2026-11-01.md)
- 清理复用扩展的任务执行机制。保留期默认 180 天，可按使用场景配置。自动清理只处理已放弃的线索，以及达到保留期且原生文档已确认删除的线索。待审、待核对及仍需用于恢复的快照保留。记录清理结果并在失败后保留可重试状态。

### 3.3 审批台与权限

- 审批台落在 `/platform/clipper`，使用现有平台外壳、TDesign、六语 i18n、知识库选择与原生权限查询。展示正文、来源、字节数、采集时间、拟写标签和实际入库进度。只有具备目标库审批权限的登录用户可查看待审快照和执行审批。
- 使用 WeKnora 登录 JWT。Quick 服务端以本次 JWT 和当前工作区上下文调用 `GET /api/v1/auth/me` 取得实际用户，并调用 `GET /api/v1/knowledge-bases/{kb}/lifecycle/authority` 核对 `tenant_id`、`knowledge_base_id` 与 `can_write`。第一版只接受当前工作区下 Host 已管理的目标库。原生写请求仍经过 Core 自身的写权限校验。读取身份与权限在持久化事务之外执行。
- Quick 使用固定本机 Core 地址转发 JWT，禁止重定向。JWT 只用于本次请求，不入库、不进入日志。签名过期、权限撤销或 Core 不可用时，等待用户重新登录或权限恢复后，续办同一条记录。后台可用既有只读能力做观察，不能用服务 Key 替代审批人的写权限。
- Quick 接口独立监听回环地址。审批入口只接受配置内的平台 Origin 和登录 JWT。浏览器扩展只用投递凭据调用 Quick 暂存接口，凭据绑定固定扩展 Origin 和目标库，不能列出或读取快照、审批或触发原生写入。审批和投递请求分别校验 Origin、方法、Content-Type 和请求大小。问答凭据及服务地址按 §3.5 单独配置。Origin、回环地址和源 URL 均不作为用户身份。
- 确认操作绑定当前快照 SHA-256、标题、来源、目标库、实际审批人和操作 ID。预览后若快照或关键字段变化，重新展示并确认。放弃操作只关闭本地待审记录。已经创建原生草稿的异常记录进入对账和人工处置流程。

### 3.4 正文与标签预检

- 采用原生 `manual` 的正文限制，按清洗后 **200,000 个 Unicode 码点**校验，并设定与该入口匹配的请求字节上限。审批页显示手工 Markdown 的限制。文件上传上限用于后续文件入口。
- WeKnora 创建和更新手工文档时均会执行 `CleanMarkdown`。Quick 保存的是用户看到并批准的规范化正文。对可能被原生改写的内容，以草稿回读为准。发生差异时保留禁用草稿和差异状态，交给用户处理。任何新正文都需要重新预览和审批。
- 复用目标库的原生标签列表及 `taggov.Mapping` 的按库匹配规则，外部线索审批时重新解析 `gov:doctype:external` 的原生 ID。网页选区、当前页正文、网页链接笔记以及有明确网页出处的外部粘贴内容另需 `gov:source:web`。原创 Markdown 笔记保存独立的速记类型，作者附上的链接作为参考出处展示，不自动归类为外部网页。所需标签需在目标库预先创建。缺失或匹配不唯一时阻断审批。创建时带 `tag_ids`，创建后依赖原生精确标签接口回读。 [Markdown 清洗和校验](../../internal/application/service/knowledge_create.go#L780) · [标签分库匹配](../../extensions/taggov/mapping.go#L30)

### 3.5 侧栏问答

1. **选库和会话。** 在可信扩展设置中配置独立的原生受限 API Key，启用 `chat` 与列库和引用读取所需的 `retrieve`，仅授权明确的知识库列表。侧栏用原生接口显示可用库，每轮发送用户当前显式选择的非空 `knowledge_base_ids`。无可用库时呈现错误或空态。换库、新建会话或更换凭据时重新绑定。 [原生 API Key 约定](../../website-docs/04-api/01-api-overview.md#L43) · [原生列库入口](../../internal/router/routes_knowledge.go#L247)
2. **提问和显示。** 复用 `POST /api/v1/sessions` 与 `POST /api/v1/knowledge-chat/{session_id}` 的快速问答路径，显示流式答案、引用和失败反馈，支持在同一范围内追问和跳转原文。问题只来自用户此轮输入；插件不自动把当前网页、待审快照、笔记草稿或网页选区拼进问答请求。 [快速问答请求](../../website-docs/04-api/02-api-chat.md#L384) · [原生流式事件](../../website-docs/04-api/01-api-overview.md#L168)
3. **治理与凭据。** 由 Core 校验真实知识库与会话权限，沿用 LanRAG 的检索和来源门禁。问答 Key 不含 `ingest` 或管理能力，也不代替审批 JWT；投递凭据不能调用问答。Quick 首版界面和请求只走快速问答。原生 `chat` 能力也可使用 Core 的 `agent-chat`，仍由该接口执行自身权限校验。Quick 的智能推理、网页上下文提问与更多模式另行评估。

## 4. 审批后的入库和恢复

执行顺序：

| 步骤 | 行为 | 继续条件与失败处理 |
|---|---|---|
| 1. 提交意图 | 本地核对快照哈希后，使用记录版本和操作 ID 领取该次提交。原子保存 `approving`、审批审计、快照哈希和稳定关联标识（建议 `channel=agentrag-clip-<clip_id>`）。 | 意图未提交前无原生写操作。并发点击只有一次可发出创建请求。该 `channel` 用于对账，具体采集方式保存在 Quick 记录中。 |
| 2. 创建原生草稿 | 向目标库的原生 `manual` 入口提交批准的标题、完整正文、必需标签 ID、关联标识，显式指定 `status=draft`。 | 原生草稿应处于 `draft/disabled`。收到格式和作用域有效的创建响应后，将文档 ID 持久绑定到同一条线索。超时、断连、非预期响应或写后校验失败按结果未知处理。 |
| 3. 回读并核验 | 调 `GET /api/v1/knowledge/{id}` 核验租户、库、类型、关联标识、标题、草稿状态及原生 `metadata.content` 的哈希与字节数，再调 `GET /api/v1/knowledge/{id}/tags/ids` 核验必需标签。 | 所有校验通过才允许发布。标签写入可能在文档创建后失败，即使原生返回错误也要先核对已有草稿。缺标签时由有权限的用户在原生界面修复同一草稿，Quick 再回读核验。 |
| 4. 发布同一文档 | 重新读取审批快照，核对当前审批权限、目标绑定、标题、正文与标签后，持久化 `publishing` 后向已绑定的原生文档 ID 提交同一份 Markdown，显式指定 `status=publish`。 | 成功后回读确认身份、标题、正文、标签及已保存的发布状态，记录为 `created`。原生 API 接收发布与解析完成分别展示。 |

原生 `manual` 创建未提供幂等键，`channel` 是对账标识。恢复时先读取已绑定的文档 ID。缺 ID 时，按原生分页契约枚举目标库并精确匹配关联标识，对唯一候选再按步骤 3 回读。若查到已进入发布阶段的同一目标，则按下述发布规则判定。分页不完整、超出扫描上限、多个匹配、身份或正文冲突均待人工核对。超时后的零匹配不足以授权自动重发创建请求。只有能够确认原请求未发出或在写入前被拒绝时，才在当前授权下继续原操作。 [原生先写文档后写标签](../../internal/application/service/knowledge_create.go#L853) · [现有分页对账](../../extensions/business/native.go#L111)

发布响应未知时，只读回已绑定目标及已出现的真实解析观察。原生会先保存 `pending`，再尝试入队，单凭该状态无法确认任务已经入队。若同一目标已实际进入 `processing` 或 `completed` 且回读核验通过，可补记 `created`。只有 `pending` 且缺成功回执时保留 `publishing`。解析失败时展示真实状态，由有权限的用户通过既有重试入口处理同一文档。状态不明时禁止自动重发发布或创建第二份文档。 [原生发布顺序](../../internal/application/service/knowledge_create.go#L1120)

Quick 控制自身的发布顺序。原生普通编辑入口仍由 Core 按权限开放。现有 API 没有覆盖正文、标签和发布状态的联合 CAS。Quick 在发布前后核对，发生变更时报告冲突。若要防止其他原生编辑入口并行更改或发布该草稿，需要另行设计并授权相应门禁。

## 5. 来源、观察与生效

- **来源记录。** Quick 保存来源 URL、采集方式、采集时间、快照哈希和已核验的原生文档 ID，在审批台提供来源详情和原生文档跳转。原生 `custom_metadata` 仅支持顶层标量、按整体替换处理。需要在原生详情显示出处时，再评估使用平铺字段和并发写入契约。首版发布只依赖 Quick 的持久化来源记录。 [原生元数据契约](../../internal/application/service/knowledge.go#L886)
- **补充关系。** 对已完成必要版本观察的文档，通过生命周期详情的现有 `supplements` 接口登记。Quick 提供入口链接，由既有页面展示真实结果。必须先建立父件依赖才能生效的场景超出本版的单次审批流程，不能用事后补链替代准入条件。外部 URL 保存在来源记录，不创建 URL 血缘节点。 [补充关系契约](../../PROJECT_CONTEXT.md#L139)
- **解析观察。** Quick 只读 `ext_ingest_current`，通过现有 `ingest.Reconciler` 获取真实解析状态。无记录时显示「等待对账」。一分钟是默认对账间隔，缺记录、分页和退避可能延长等待时间。 [对账实现](../../extensions/ingest/reconcile.go#L194) · [默认配置](../../extensions/host/config.go#L59)
- **生命周期。** 第一版采用一次准入审批。原生草稿阶段不可检索。发布并解析完成后，现有观察者可将文档初始化为 `pending`，随后按日期和依赖推进为 `active`、`scheduled` 或 `stale`。没有有效的 `active` 许可时，现有检索策略会排除文档。UI 显示「解析完成后按库规则生效」，不承诺入库后的第二次人工放行。 [完成观察](../../extensions/ingest/lifecycle.go#L24) · [状态机](../../extensions/lifecycle/automation.go#L32) · [检索许可](../../extensions/retrieval/policy.go#L266)

## 6. 工程落位

| 范围 | 实施时的落点 |
|---|---|
| 扩展后端 | `extensions/clipper/` 保存暂存、审批事务、原生 API 请求和对账。复用扩展存储、审计、分页和对账机制。 |
| 数据 | 在实施时按实际最新版本分配下一迁移号。本轮最高为 `041`，`010` 已用于来源运行记录。保存 `clip_id`、租户、库、来源、标题、快照路径与指纹、审批人和时间、操作 ID、关联标识、原生文档 ID、阶段和错误码。记录状态建议 `captured → approving → draft_bound → publishing → created`，未发起远端写入可置 `abandoned`。结果未知保留对应阶段和原因。文档 ID 一旦绑定，不被另一条线索重复认领。正文存放于快照。 |
| 审批 UI | `frontend/src/views/clipper/` 与 `frontend/src/api/clipper.ts`，复用既有平台路由和管理页面规范。 |
| 浏览器扩展 | `extensions/ui/browser-extension/`，独立实现剪藏、速记和问答侧栏。剪藏和速记投递到 Quick，问答使用原生会话和 SSE。参考 BrowserSkill 的扩展工程，构建与发布独立。 |

核心业务源码、Frozen Architecture、原生表、根 Go 依赖和解析调度保持冻结。Quick 与 WeKnora 通过公开 HTTP 接口协作。扩展来源记录不写入问答 `ext_feedback_events`，不创建替代关系，也不接管生命周期裁决。本方案是 M2 生命周期和 M3 来源能力之上的浏览器入口。待审数据仍由 Quick 管理，问答仍由原生会话承载。 [冻结架构](<AgentRAG 技术方案 v1.0 — Frozen Architecture.md>) · [扩展模块规则](../../AGENTS.md)

## 7. 实施验收

| 验收点 | 可核对的结果 |
|---|---|
| 首版侧栏 | 同一扩展可右键划选、提取当前页正文、只存链接、编辑和恢复 Markdown 草稿、选择授权知识库及发起快速问答。切换标签页不自动采集，快捷键能打开侧栏。 |
| 审批前隔离 | 划选、整页正文、链接笔记、Markdown 速记和手动粘贴均能生成待审快照。直接查询原生目标库及队列，确认零新文档、零新解析任务。放弃后仍为零。 |
| 侧栏问答 | 验证流式答案、真实引用、继续追问与换库隔离。投递凭据不能问答，无可用库时不发起问答，未授权库请求被拒绝。确认问答请求不附带当前页和待审内容，待审或失去有效许可的文档不能成为受控知识来源。 |
| 审批授权 | 只有通过当前用户、租户和库写权限验证的 JWT 能执行审批与发布。投递凭据、问答 Key、过期 JWT、只读用户、跨库和错误 Origin 均不能触发审批或原生知识库文档写入。 |
| 正文和标签 | 草稿回读哈希与已审批快照一致，必需标签 ID 属于目标库。快照变更、原生正文改写、标签缺失或失败均不触发 Quick 发布。 |
| 创建恢复 | 覆盖意图提交前失败、草稿创建后断连、重复点击和标签写入部分失败。最多认领同一目标一次。零匹配但结果不明、扫描不完整或多个候选时保持待核对。 |
| 发布恢复 | 在保存发布状态后、提交队列前后分别注入故障。始终核对原 ID。仅有 `pending` 且缺回执时展示待核对，解析失败通过既有入口恢复。 |
| 生命周期 | 核对草稿不可检索、已发布而未获有效 `active` 许可时被排除、正常库规则下可自动生效、排期或依赖失效时按既有策略处理。 |
| 数据与界面 | 快照不出现在未跟踪清单、日志或 Agent 工作区。检查六语词条、zh-CN 与 en-US 界面、实际构建产物和失败状态。 |
| 仓库边界 | 实施时运行扩展格式、构建、测试、静态检查与独立模块门禁。前端运行 `npm run check-i18n`、`npm run type-check`、`npm test`、`npm run build` 并按仓库规则验证部署。用 `git diff -- internal/` 核查核心边界。 |

本稿只记录静态源码核对和实施验收标准。真实剪藏、笔记草稿、侧栏问答、审批入库、解析、崩溃恢复及检索行为需在实现后验证。
