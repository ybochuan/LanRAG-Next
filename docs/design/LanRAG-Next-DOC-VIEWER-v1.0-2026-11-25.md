# LanRAG-Next 多格式文档阅读引擎升级专项

**DOC-VIEWER · Zrimo 优先选型与渐进式迁移 · v1.0**

| 项目 | 内容 |
|---|---|
| 日期 | 2026-11-25（UTC+8） |
| 状态 | 专项设计建议稿，待实施批准；未执行 PoC、部署或源码修改 |
| 项目 | ybochuan/LanRAG-Next / Windows LanRAG Lite |
| 代码基线 | `codex/m1-parser-routing`，远程 HEAD `87642ade0f9787e51faa34dd746acf52612c88fc` |
| 阅读中心实现基线 | `5311e50385196b9a281a70e7a7d1ea87627a36f5` |
| 上游冻结基线 | Tencent/WeKnora v0.9.0，`f17ee8a` |
| 上位方案 | 《文档阅读中心与关联研读系统 v1.1》、Frozen Architecture、AGENTS.md |
| 首选引擎 | Zrimo `v0.1.2`（拟冻结，须核验 npm 产物及源码对应关系） |
| 建议归档 | `docs/design/DOC-VIEWER-多格式阅读引擎升级-v1.0.md` |

> 本方案基于已读取的最新远程分支、空间文件库中 2026-11-25 00:30 部署记录、阅读中心 v1.1、原生薄补丁与 Zrimo v0.1.2 公开 API/安全/兼容/字体文档。远程代码和历史验收记录不等同于重新验证本机生产环境。本方案不自动授权核心受控区改动、创建 Fork、安装依赖或部署。

## 0. 执行摘要

当前阅读中心已经形成独立路由、文档选择、多标签、单文档与双文档阅读、工作台往返、附件、沿革和溯源等整体框架。问题集中在原文渲染体验与操作一致性，而不是缺少另一套知识库或治理系统。

推荐先以 Zrimo 统一多格式原件阅读，使用其已有受管理视口、虚拟滚动、分页、搜索、缩放及文本选择能力；原有 WeKnora 提供文件、鉴权、解析与定位契约，Extension Host 继续提供治理事实。现有 `ReaderPane.vue` 是首选替换边界，`readerApi.ts` 保留为文件读取与身份核验边界。Zrimo 接入不需要新增 Office Server、业务数据库、模型或 Go 服务。

统一的是工作区及阅读操作，不承诺一个内核覆盖所有格式。原生预览继续承担解析内容、勘误 Markdown、精确证据定位及未通过资格测试格式的兼容路径。后续确有复杂排版缺陷时再评估独立维护 Zrimo Fork 或按需 PDF 衍生预览，不同时展开多个重型项目。

**排期原则：先完成整体可用框架，随后补体验细节；身份、权限、取消、资源隔离和安全回退是首批硬门禁，不延后。**

## 1. 当前事实及专项边界

### 1.1 最新实施与验收基线

根据远程 `TASK_BACKLOG.md` 与 `docs/evidence/document-studio-source-mapping-2026-11-24.md`：

- DOC-STUDIO P1 阅读闭环通过隔离浏览器夹具；Windows Lite 安装版已完成真实授权 DOCX 单窗、附件面板验收。P2/P3 部分代码已落地。
- 2026-11-25 00:30 前端切换，入口 SHA-256 为 `5f86d3c11d00e6151b672e2d55cd187e2008f3e648baf9f2bd1d709d38a143b5`，记录的回滚目录为 `resources/web-backup-20261125-0030`。
- 定向用例 115/115、六语审计 15/15、类型检查与构建通过；最近一次完整前端测试 2529 项（2527 pass、2 skip、0 fail），最终两处 UI/对照修复后未重跑全量。隔离夹具主要使用 Markdown，不证明复杂 Office 双窗通过。
- 最新提交 `5311e503` 实现阅读中心，`87642ade` 更新实现记录。空间中较早的 `TASK_BACKLOG(1).md`、v1.1 更新副本仍记录工作区未提交时的 `d6057e41`；以后者作为历史材料，不覆盖远程代码事实。
- 尚待真实 PDF/XLSX/图片、双窗工具栏、权限负向、工作台实机往返、条款锚点、位置恢复和旧图替代验收。

### 1.2 源码复用矩阵

| 现有文件（相对于仓库根） | 已有能力 | DOC-VIEWER 策略 |
|---|---|---|
| `frontend/src/views/document-reader/DocumentReaderView.vue` | 工作区、路由、标签、双窗、拖拽、关系导航、下载 | 保留；只按需接线阅读控制、引擎状态 |
| `.../document-reader/ReaderPane.vue` | 文件读取、请求取消、版本不可读状态、预览组件和独立工具栏 | 主要接入点；保留读取与治理判定，增加 Zrimo 渲染分支 |
| `.../document-reader/readerApi.ts` | 校验 KB/ID；GET `/api/v1/knowledge/{id}/preview`；读取前后元数据比对；勘误读取 | 保持授权链路，不另建文件服务 |
| `.../document-reader/model.ts` | `ReaderDocument {kb,id,version?,kind?}`、路由、readerKey | 保留身份，不为引擎发明第二套文档 ID |
| `.../document-reader/ReaderHistory.vue` | 业务族与观测来源 | 保留；不能按当前 ID 冒充历史字节 |
| `.../document-reader/ReaderAttachments.vue` | 主件、附件及模板 | 保留现有关系验真和简洁附件列表 |
| `.../document-reader/readerRelations.ts` | 同库关系过滤 | 保留；不得因新引擎扩大跨库范围 |
| `frontend/src/stores/documentReader.ts` | 作用域切换清理、12 个标签上限、工作台返回快照 | 复用；标签不是常驻引擎实例 |
| `frontend/src/components/document-preview.vue` | 多格式预览、Markdown、原文定位、可选全屏契约 | 不删除、不复制；作为原生兼容路径 |
| `frontend/src/components/source-preview/PdfSourceViewer.vue` | PDF.js、文本层、定位与资源控制 | 保留，避免丢失精确证据定位 |
| `frontend/src/utils/sourceLocator.ts` 等 | 原文定位语义与类型 | 复用，不将偏移、页码或哈希直接强转 |
| `frontend/src/components/ChatReferenceSourceView.vue` | 原生问答引用预览 | 保持原有行为；条款入口按 DOC-STUDIO 衔接 |

当前前端已有 `pdfjs-dist`、`docx-preview`、`@vue-office/pptx`、SheetJS。其阅读能力主要在前端，不应把 docreader 解析链路当作当前所有预览问题的原因。最新分支没有发现 Zrimo 依赖。

### 1.3 设计裁决（拟修订，不覆盖原文其他约束）

| ID | 裁决 |
|---|---|
| DV-D01 | 仅升级阅读中心窗格内的渲染实现，不新建阅读页面或治理体系。 |
| DV-D02 | 将 DS-D03 改为「复用原生授权和文件契约；允许通过独立组件替换阅读中心渲染实现；原生预览保留」。 |
| DV-D03 | Zrimo 为优先候选，按格式通过资格测试后才成为默认；不凭格式宣传或 Stars 上线。 |
| DV-D04 | PoC 使用内置 UI 和脱敏夹具；正式使用容器 + `ui:false`、TDesign 工具栏，不重写视口。 |
| DV-D05 | 原件/解析为同一窗格的内容视图，不新增工作模式；仍只有单窗/双窗。解析内容复用现有有序产物，非原件明确标识。 |
| DV-D06 | 原生证据路径先保留，精准定位逐格式通过验证后才迁移。 |
| DV-D07 | 文件读取和下载权限分离；任何回退不得绕过授权、身份、生命周期或撤销。 |
| DV-D08 | 无新增 Core 补丁、Host 服务、业务表和数据库为首期目标；不得直接读取 SQLite 或本地存储路径。 |
| DV-D09 | 自主 Fork 为条件任务；先基于固定上游包验证，再决定是否改内核。 |
| DV-D10 | 按需 PDF 衍生预览为条件项，独立审批；不在入库时强制转换、不重复索引。 |
| DV-D11 | 旧图退役仍由 DOC-STUDIO 验收决定，与阅读器选型相互独立。 |
| DV-D12 | 以 Windows Lite 桌面真实阅读体验为首要目标，浏览器夹具不替代实机验收。 |

## 2. 选型及风险判断

### 2.1 优先候选

Zrimo（<https://github.com/bnku/zrimo>）采用 MIT OR Apache-2.0 双许可，TypeScript + Rust/WASM，现代 Office 底层使用 `@silurus/ooxml`，PDF 使用 PDF.js，旧版 Office 走受限解析与转换适配。v0.1.2 文档公开了 API、测试、资源上限、字体与安全约束；维护规模小且发布历史短，需要独立构建能力和实测资格作为准入条件。

| 能力 | 已核验公开 API/说明 | 本项目约束 |
|---|---|---|
| 文档加载 | `viewer.load(Blob/File/ArrayBuffer/...)` | 只接收已经授权取得的 Blob，不传任意外部 URL |
| 多实例 | `ViewerClient.create()`、`createViewer()` | A/B 两个独立实例；共享 Client 是否影响隔离须验真 |
| 导航 | `goToPage`、`next`、`previous`、`setSheet` | 页/幻灯片/图片/工作表类型按能力显示，UI 页码 1 基 |
| 缩放 | `setZoom`、`fitWidth`、`fitPage` | 独立控制，不能假设不同格式的缩放等价 |
| 文内搜索 | `search`、`searchNext`、`searchPrevious`、`clearSearch` | 能力探测，搜索结果不等同证据定位 |
| 文本定位 | `getPageText`、`selectText` | UTF-16 偏移；须核对内容与原件身份 |
| 表格选择 | `selectCells`、`selectCellRanges` | sheet 0 基，行列 1 基；不编辑 |
| 缩略图 | `renderThumbnail` | 能力支持时按需生成；不把缩略图当章节大纲 |
| 状态/资源 | `viewer.state`、`viewer.on`、`close`、`destroy` | 请求代次、事件退订、释放 Worker/Canvas/WASM 必验 |
| 主题/国际化 | CSS tokens、内置 en/ru、translations | 正式 UI 使用原生 TDesign 六语；文档字体与界面字体分离 |
| 资源限制 | `limits`、本地 assets、fontPolicy | 固定版本、自托管、无外网依赖；配置需实机确认 |

### 2.2 候选优先级与不纳入首期项

| 方案 | 定位 | 选择条件 |
|---|---|---|
| Zrimo | 主选 | 离线、源码/许可证、复杂中文业务文件、双窗、资源门禁通过 |
| 现有多格式预览 | 必选对照及兼容 | 已有能力保留，按同一测试集比较 |
| JitViewer | 可选对照 | 仅脱敏样本；完整核心源码、可重建和运行时许可未核实，不进入生产默认 |
| ONLYOFFICE / Collabora | 备选 | 只有 Zrimo 对核心 Office 格式不合格且按需转换无法满足，另行审议部署成本及许可 |
| LibreOffice PDF 副本 | 条件兜底 | 有实际排版失败、确认安全隔离及缓存治理后另行批准 |

Zrimo 已声明旧版 DOC/XLS/PPT 为受限支持，复杂形状/OLE、宏、部分高级表格等可能缺失；Excel 公式仅显示已有缓存值，不计算；加密 PDF/Office 不支持；OFD 未列为支持。不能把兼容列表等同于质量承诺。

## 3. 目标架构与改动范围

```text
WeKnora 原生平台（Vue 3 / TDesign / 六语）
└─ DocumentReaderView.vue（保留）
   ├─ 标签、关系、附件、沿革、工作台返回（保留）
   ├─ ReaderPane A（授权读取、请求代次、历史门禁）
   │  ├─ ZrimoReader（新增，受管理 viewport）
   │  └─ DocumentPreview（已有，兼容/证据/解析）
   └─ ReaderPane B（独立实例，同一授权契约）
           ↑ 已核验 Blob + ReaderDocument + 当前代次
       readerApi.ts（保留）
           ↑ 原生 JWT、KB 权限、附件许可、读取前后核验
       WeKnora Core + Extension Host（业务职责不变）
```

### 3.1 计划文件（具体命名以实审为准）

| 文件 | 操作 | 职责 |
|---|---|---|
| `frontend/src/views/document-reader/ZrimoReader.vue` | 新增 | 挂载/销毁 Viewer、渲染状态、能力与事件桥接 |
| `.../document-reader/zrimoRuntime.ts` | 新增（必要时） | 延迟加载、资源根路径、固定配置及 Client 生命周期 |
| `.../document-reader/readerEngine.ts` | 新增 | 小型静态路由及格式资格判定；不建插件框架 |
| `.../document-reader/ReaderToolbar.vue` | P2 新增 | 统一 TDesign 控件，按窗格能力启用 |
| `.../document-reader/ReaderPane.vue` | 修改 | 在原生文件读取成功后选择渲染器，回退提示与代次保护 |
| `.../document-reader/DocumentReaderView.vue` | 最小修改 | 活动侧、工具栏/视图切换及状态记忆接线 |
| `frontend/package.json` 与 lockfile | 修改 | 固定依赖及资源复制构建脚本，审核 transitive 变更 |
| `frontend/src/i18n/locales/*.ts` | 修改 | 六语完整翻译、aria/tooltip/error |
| `frontend/scripts/verify-document-reader.mjs` 或独立测试脚本 | 扩展 | 浏览器/桌面回归与资源探测 |
| `TASK_BACKLOG.md`、ADR、evidence | 经授权更新 | 专项进度、决策、可复现实测记录 |

原则上不修改 `internal/`、`extensions/` 业务逻辑、根/扩展 Go 依赖、原生 `document-preview.vue`、`PdfSourceViewer.vue`、`KnowledgeBase.vue`。如发现源文件身份绑定缺口，只记录并提出独立审批，不为引擎绕过约束。

### 3.2 文件读取与渲染契约

现有 `readCurrentFile()` 先读元数据、再按 ID 取 Blob、再读元数据并比较 ID/KB/type/file_hash/file_path/name 等字段。其 `file_hash` 不能假定为 SHA-256 原件指纹；前后元数据一致也不是严格固定版本字节证明。

建议最小组件输入为已读取的 `blob`、文件名/类型、`readerKey`、当前授权作用域/请求代次、活动状态和可选原生 locator；输出 ready、warning、error、能力和阅读状态。实际 TS 类型须与固定版本 SDK 对照，禁止依据伪代码猜 API。引擎不得自行获取 KB 或 Host 凭据，不得发起文档读取。父组件只在授权读取成功且代次仍有效时挂载 Viewer。

`ReaderPane` 继续负责固定观察版本拒绝、历史记录提示和勘误数据读取；业务族中独立文档 ID 的历史原件仅按已有授权读取。相同 ID 的旧观察版本不能读当前文件冒充旧版本。认证/权限异常、身份变化、未知版本、撤销不能触发降级放行。

### 3.3 引擎路由（拟定）

| 类型/场景 | 初始 PoC | 正式候选策略 |
|---|---|---|
| DOCX / XLSX / PPTX | Zrimo + 现有引擎对照 | 按格式通过准入后默认 Zrimo |
| PDF | Zrimo + 原生 PDF.js | 普通阅读可切换；精确引用仍原生优先 |
| DOC / XLS / PPT | 独立复杂样本 | 未通过资格则沿用现有支持或明确不支持 |
| 图片 / CSV / TSV | 视需求抽样 | 按能力启用，不强制全部迁移 |
| Markdown / TXT / 勘误内容 | 原生 | 继续原生安全渲染 |
| 原生 SourceLocator 请求 | 原生 | 只有格式级证据验收通过才允许新引擎接管 |
| 不支持、排版警告、资源超限 | 明确错误/警告 | 同授权上下文下回退；资源超限不得自动以更宽松限制反复尝试 |
| 401/403、身份冲突、历史不可核验 | 不渲染 | fail-closed，不提供自动回退 |

禁止静默回退后宣称 Zrimo 渲染成功。UI 标明实际引擎、原件/解析/衍生视图及不可用原因。首期可用一个实验开关 + 静态格式 allowlist，不建动态注册中心。

## 4. UI 与交互设计

保留原有 `/platform/reader`、顶部文档选择、多标签、关联面板、双窗与返回机制。整体采用 AGENTS.md §8 原生灰白底色、绿色品牌色、深浅主题语义 token、TDesign 组件、六语和键盘规范。正文保持原件页面颜色，深色主题只改变阅读框架，不擅自反色公章、图片或表格。

```text
原生侧栏 │ 阅读中心                选择文档   返回来源
         │ [主制度 ×] [附件 ×] [旧版 ×]  +
         │ 关联/沿革/溯源/附件  │ 单窗/对照
         │ ┌──────────────────────────────┐
         │ │ A · 当前文件                  │
         │ │ 页码 / 缩放 / 适宽 / 搜索 / 全屏 │
         │ │                              │  现有关联面板
         │ │       文档渲染视口             │  继续复用
         │ │                              │
         │ └──────────────────────────────┘
```

此图仅为布局原则，不指定附件面板必须左右迁移。沿用现有布局为默认；优化顶部拥挤与标题截断，不重做导航体系。双窗使用 A/B 独立工具栏和缩放；全屏仍由活动窗格统一仲裁，避免 Viewer 与宿主同时拦截 Esc/F11。窄屏沿用现有 A/B 切换。目录若无可靠结构数据，只提供页缩略图；不从 Chunk 标题猜测正式章节。

### 4.1 两阶段 UI 接入

- PoC：独立测试页 `ui:true`，验证内置工具栏及完整能力。只允许脱敏夹具或明确拥有下载许可的测试身份，禁止将带原件下载的内置 UI 暴露给普通 Viewer。
- 正式：`container` + `ui:false` 保留 Zrimo 管理的视口、虚拟滚动及交互，不是完全无 DOM 的 Headless。TDesign 工具栏由宿主控制。禁止仅隐藏内置下载按钮的 CSS hack。
- Headless `renderPage` / `renderSheetViewport` 仅在已有视口 API 确实无法满足已批准需求时考虑，不首期重写 Canvas 与分页逻辑。

### 4.2 内容视图与阅读状态

原件是默认阅读视图；解析内容为可选辅助视图，仅在现有原生合并内容/有序解析产物已核实且权限成立时启用，清晰标注「解析内容（非原件）」。禁止直接拼接重叠或乱序的检索 Chunks 作为完整文档。解析视图不影响原件身份、RAG 索引和治理事实。现有已审核勘误展示不被普通解析内容替代。

只保留单窗和双窗两种工作状态。解析视图、专注阅读、搜索和证据均属窗格内或面板配置。

位置记忆优先在会话内按用户/租户/KB/readerKey/有效版本或可核验资源标识区分；仅保存页、缩放、fit、工作表、可安全恢复的滚动锚点。不要持久化 Blob、正文、搜索片段或授权令牌。认证作用域变化、身份漂移、关闭标签或撤销时按现有机制清理。跨重启恢复不是首批必须项，另经权限和数据保留审查。

双窗默认独立滚动；比例同步仅在用户显式开启且公开事件/导航契约可用时实现，防止反馈循环。主制度与附件不默认同步；条款锚点同步须有可信条款映射，不因标题相似而推断。

## 5. 技术实施与独立 Fork 策略

### 5.1 最小接入示例（非已验证代码）

```ts
import { ViewerClient } from '@zrimo/viewer'

// 真实路径须基于 Vite BASE_URL 和 Windows Lite WebView 资源协议验真。
const client = ViewerClient.create({
  assetBaseUrl: verifiedLocalAssetRoot,
  fontPolicy: { mode: 'auto' }, // 仅允许本地 package assets / 已授权字体
})
const viewer = client.createViewer({
  container: paneElement,
  ui: false,
  layout: 'continuous',
  fit: 'width',
  overscan: 1,
})
// blob 必须由 ReaderPane/readerApi 的原生授权读取链路取得。
await viewer.load(blob, { fileName })
// 实际封装还须处理事件、代次、取消、资源上限、能力探测及错误。
// 在切换、关闭和卸载时，退订监听并执行 await viewer.destroy()；
// Client 仅在其拥有的所有实例都退出后销毁。
```

不能将 Zrimo SDK 的 `getOriginalBytes()`、`downloadOriginal()` 接入正式 UI、快捷键或上下文菜单；原件下载继续由现有 native download API 实时验证。已授权预览 Blob 在客户端内存中可被用户侧技术手段取得，这是现有浏览器授权模型的固有限制；不能声称隐藏下载按钮构成 DRM 或持续撤销。

### 5.2 依赖、资源与 Windows Lite 打包

P0 核对 npm registry 可用性、v0.1.2 tag 对应源码/包版本、integrity、license、第三方 notices、SBOM、当前 PDF.js 重复版本和 bundle 体积。固定版本与 lockfile，不能改用浮动 git main。若 tag 与 npm 产物无法对应则阻断生产接入。

依照 Zrimo 文档使用 `zrimo-copy-assets` 复制完整运行时资源目录；将复制和检查纳入可重复构建脚本，不依赖手工下载或 CDN。实际 Vite base、Windows Lite 本地 HTTP/WebView 打包路径及 `application/wasm`、ESM worker MIME、font MIME 必须实测。使用同源资产和明确 CSP/网络策略，不为 WASM 任意打开外部网络或不安全脚本。自动字体模式的本地 fallback 会通过 assetBaseUrl 获取本地字体；`offline` 模式反而不取打包 fallback，需有系统字体或合法注册字节。字体替代警告须显示/记录；不得未经许可分发商业字体。

将 build manifest 及静态文件哈希纳入部署记录，不能只核对 index.html 后声称全部 Worker/WASM 资源一致。测试断外网后 DOCX/PDF/XLSX/PPTX 正常打开，并记录所有请求域名和资源失败；无远程字体、转换、遥测、嵌入文档外链隐式读取。PoC 不更改正式安装资源。

### 5.3 自主维护决策

建议第一阶段直接固定上游包；可以经授权建立独立 Fork，但不因 Fork 提前修改 Rust/WASM。仅当以下条件成立，才启用自维护发布：

1. 真实业务文件暴露无法通过宿主配置、公共 API 或轻薄适配解决的关键缺陷，并已有最小脱敏回归样本。
2. 可从固定源码及锁文件完成源码构建、打包、许可证/依赖审计和跨浏览器关键回归；记录产物哈希与 tag/commit。若不能证明位级可重复，至少证明可重建和行为回归，明示差异。
3. 评估缺陷实际归属 Zrimo TypeScript、Rust 适配器、Silurus OOXML、office_oxide 或 PDF.js；优先提交上游修复，避免长期复制多套解析内核。

建议独立仓库名 `LanRAG-Viewer`（候选，未创建），保留 upstream `bnku/zrimo`，打基线 tag，改动以独立分支与小提交管理。若只需阅读器 UI 定制，仍在 LanRAG-Next 内使用宿主工具栏，**不为换配色 Fork 内核**。需要发行 fork 时可使用本地构建 tarball 或经批准的独立包名/私有 registry，固定版本与 integrity；不得假设 npm 名称已可用或已发布。

上游 `v0.1.2` workspace 提供 `npm run build`、`npm run check`、`npm run test:qualification`、`npm run test:e2e:matrix`、`npm run test:pack`、`npm run audit:vulnerabilities`、`npm run report:sbom` 等脚本。Node/npm/Rust 工具链以该 tag 声明及锁文件为准，在独立仓库或隔离 CI 构建，不更改 LanRAG-Next Go/Node 工具链来迁就它。注意构建脚本及语料下载仅使用审核过的公开测试材料；真实银行业务文件绝不进入上游仓库或公开 CI。

MIT/Apache 双许可允许修改与再分发，但必须满足所选许可及第三方版权、NOTICE、字体 OFL、其他依赖条款。保留第三方署名；开源许可与商标权是两回事。许可审计为发布门禁。

## 6. 原文定位、权限与安全

### 6.1 Source Locator 分级迁移

现有 `SourceLocateRequest` 包含定位结构、引用文本及原文身份信息。Zrimo `search()` 返回页 0 基及 UTF-16 start/end，`selectText()` 使用逻辑文本范围；现有 PDF bbox、DOCX 段落、Excel 行/单元格等不可直接套用。当前问答入口传文档身份，条款锚点尚待贯通，此问题仍由 DOC-STUDIO 与本专项协调，不能宣称新引擎自动解决。

定位流程：先验证租户/KB/文档、有效版本和实际原件证据；读取本页或限定区域文本；核对原有 locator、引用、上下文和候选唯一性；在匹配成立且支持目标类型时跳转/选择。PDF bbox 若无稳定公开绘制接口，继续用原生 PdfSourceViewer；禁止操纵私有 DOM/canvas 伪造精确定位。

状态至少保留「精准定位」「仅页/段落」「多个候选」「未找到」「版本不一致」「不支持」等现有语义；不得用搜索到相同关键词表示已精准定位。歧义、过期、不可核验时显示原因并使用安全原生路径或仅显示证据记录。扫描 PDF 没有文本层时只能在可信 OCR/页面坐标存在时定位，不假定新引擎会 OCR。严格证据迁移必须通过对照测试，错误精准标记为阻断缺陷。

### 6.2 资源、缓存与撤销

Zrimo v0.1.2 安全文档描述默认 input 100 MiB、展开 Office 512 MiB、单 ZIP entry 64 MiB、文本缓存 64 MiB、并发渲染 2、worker 操作 30s 等限制；这不是整个应用的 RAM 上限，也不等于本机已经测量。P0 确定产品级文件预算：可先建议 PoC 每文件 50 MiB、最多 2 个活动实例、overscan=1；如真实合法文件超限，测量后再调整，不无条件提高限制。原生已取完整 Blob 的读取阶段仍有内存风险，不能只依赖 Viewer 解析后的限额；读取前可用可信大小字段时先检查，读取后按 blob.size 校验，不修改 Core 即可先设浏览器保护。

禁止预加载 12 个标签的文件字节；仅挂载活动窗格 A/B。切标签、窄屏换侧、路由离开、认证切换、刷新、失败都测试取消和释放；`AbortSignal` 仅为取消信号，必须同时使用代次检查防止迟到事件。卸载必须销毁实例、移除订阅、释放宿主 Blob 引用。共享 Client 是否真正限制全局并发和释放 Worker 需测量；不能先销毁共享 Client 造成另一窗崩溃。运行内存、页面渲染和文本缓存预算须包含 A/B 合计及现有应用开销。

现有授权后续读取/重开时复验。应用能控制的缓存和有效状态按当前机制清除，已交付客户端的字节不承诺可持续撤回。外部链接默认不自动打开；必须由可信宿主显式处理允许的 URL scheme，宏、OLE、脚本不执行。异常文档和 Worker 崩溃不能导致页面白屏。

### 6.3 条件项：PDF 衍生预览

仅在复杂 DOCX/旧 Office 关键排版不能通过且有实际收益时申请独立 PoC。优先按需转换，不在入库时批量生成。转换器与字体版本、配置、原件字节 SHA-256、所属作用域构成缓存身份；权限继承原件，过期/撤销/删除均不得访问旧缓存；标注「预览副本」，不替代正式原件和 SourceLocator，不重索引、不写业务事实。转换进程需隔离网络、禁止宏及外部读取、限制 CPU/RAM/超时/临时目录与清理；不绕过 Core/Host。当前无此功能实施授权，也不因 Zrimo 失败自动启动转换服务。

## 7. 分阶段实施（整体框架先行）

| 阶段 | 建设工作 | 可使用成果 / 放行条件 |
|---|---|---|
| DV-P0（建议 0.5–1 工作日） | 核对当前分支和本机版本；冻结样本、许可/依赖/资产；原生基线；确定质量与资源预算 | PoC 准入记录；不要求全面引擎研究才开工 |
| DV-P1a（1–2 日） | 隔离页面，内置 UI，真实脱敏 DOCX/PDF/XLSX/PPTX，单/双实例 | 能亲自体验并对比原生；实际格式结果而非截图模拟 |
| DV-P1b（1–2 日） | 最小接入 ReaderPane；宿主基本操作、单/双窗、附件打开、返回工作台；正式路径关闭内置下载，静态 allowlist 和回退 | **第一个整体框架里程碑**：选文件→阅读→对照→返回，安全门禁与资源释放成立；仅测试环境/实验开关 |
| DV-P2（2–3 日） | TDesign 完整工具栏、搜索、缩放、页码/工作表、缩略图、布局与位置恢复、六语、主题、解析视图能力核实 | 日常可用的统一阅读体验；不支持功能按能力隐藏或说明 |
| DV-P3（2–3 日） | 与 DOC-STUDIO 协同定位与业务衔接，真实权限负向、格式级证据准入、实机性能、全量回归、发布/回滚 | 按格式正式启用；未通过格式继续原生；形成 ADR 和验收记录 |
| DV-P4（条件项，重新估算） | 对真实缺陷做独立 Fork 补丁/必要时 PDF 转换 PoC | 仅经批准后实施；与主体完成度分别统计 |

上述为单开发者规划估算，不是承诺交付时间；实际以样本准备、构建、桌面环境和缺陷数量为准。P1 不等待所有细节完成；但未通过安全门禁的格式不得启用。六语、基本 TDesign、取消和真实错误态从接入开始执行；不是 P2 才开始。P2/P3 的定位增强不应阻断已合格普通阅读，但原生证据路径必须完整保留。

### 7.1 与现有 DOC-STUDIO 的依赖关系

| DOC-STUDIO 待办 | 本专项衔接 |
|---|---|
| 原生真实格式/权限矩阵 | 共享样本、基线及负向测试，保留现有验收结论 |
| 工作台往返及附件、沿革 | 保持既有逻辑；P1b/P3 共同验收，不重做 |
| 条款证据与问答锚点 | DOC-STUDIO 负责可信输入贯通；DOC-VIEWER 负责渲染端验证/回退 |
| 标签/位置/缩放/主题 | 用新引擎能力完善，避免两条任务重复实现 |
| 旧 M6 血缘图退役 | 保持原任务及验收条件，不以新阅读器替代为退役依据 |

建议每阶段独立提交；原有阅读中心代码和回退路径始终能正常构建。DV-P4 不纳入首期完成定义，不阻塞已通过格式上线。

## 8. PoC 样本、评价与正式准入

### 8.1 基线样本

使用合法本地脱敏副本或公开同结构夹具；先记录 SHA-256、大小、格式、参考软件/版本、字体环境、页数及关键检查点。截图只展示脱敏内容，测试记录不保存原件正文或内部业务编号。PoC 不上传外部在线 Demo。不得把真实业务文档提交到 Git。

| 样本 ID | 建议数量/结构 | 关键验收 |
|---|---|---|
| DOCX-POLICY | 3–5 份中文长制度 | 页边距、分页、标题编号、页眉页脚、表格、印章图片 |
| DOCX-FORM | 5–8 份申请书、审批单、记录清单、跨页表 | 合并单元格、行高、换行、空白填写区、单元格内容和顺序无遗漏 |
| DOC-LEGACY | 2–3 份 Word 97–2003 | 受限格式、旧字体、复杂表格、警告及回退 |
| PDF-TEXT | 3 份电子 PDF（含混合页面大小） | 搜索、复制、页码、缩略图、定位、图文完整 |
| PDF-SCAN | 2 份扫描及混合 PDF | 图片、旋转、无文本层提示、可信 OCR/坐标路径 |
| XLSX | 3 份多工作表、大表格、冻结行列、公式缓存 | sheet 导航、合并/隐藏单元格、选择及复制，公式不误称已重算 |
| PPTX | 2–3 份图文混排与表格图表 | 页序、图片、文本、裁切与字体 |
| OTHER | 2–3 份 CSV/图片及现有 Markdown | 能力按格式展示、原生解析和勘误兼容 |
| ADVERSARIAL | 受控合成损坏/加密/超限/外链文件 | 明确错误、网络隔离、取消、资源保护；不使用真实恶意附件 |

每类使用同一批文件、同一桌面环境、同一参考渲染或经人工核对的检查点；对照现有引擎和 Zrimo。Office 页数可能受字体与渲染器影响，不以页数相等代替内容正确；正式公文排版变化需由用户确认可接受性。

### 8.2 评分方法（建议权重，P0 冻结）

| 指标 | 权重 | 证据 |
|---|---:|---|
| 中文公文及复杂表格还原 | 30 | 人工逐检查点、对照截图 |
| 多格式与统一操作 | 20 | 按格式能力矩阵、可体验演示 |
| 证据定位兼容性 | 15 | 明确精确/降级/拒绝结果、错误定位计数 |
| 双窗、切换与资源释放 | 15 | 重复操作脚本、内存与 Worker 数 |
| 首屏、搜索、缩放响应 | 10 | 同样本冷/热 P50/P95、失败率 |
| 长期维护及宿主集成 | 10 | 版本固定、构建、依赖体积、补丁与升级成本 |
| 合计 | 100 | 每项 0–5 分，按权重换算；未测试记 N/T，不计为通过 |

建议核心格式候选分数不低于 80/100，且不低于现有引擎；以格式级准入为准，不将 PDF 优势抵消 DOCX 数据缺失。评分只是辅助，不替代以下硬门禁：许可/源码可维护性、真实授权、关键文本/表格无实质遗漏或错误、无错误精确证据、双窗不串文档、无外发和不可接受资源问题。未通过的格式回退原生或明确不支持；不人为调整分数制造上线结论。

### 8.3 性能实测口径

记录 Windows Lite 实机 Windows/架构、WebView 版本、CPU/RAM、其他运行服务及模型占用、文件大小和引擎版本。计时分开：授权读取（含前后元数据核验）、资源加载、解析至首次可见、搜索结果、交互至完成渲染。每个代表样本建议至少 5 次冷/热打开（冷/热定义一致），P95 小样本仅作观察并记录样本量；完整基准可扩至 20 次。并记录 renderer、WebView 整体 RSS/working set、CPU、Worker 数量和长任务。用同一测试顺序及稳定环境与原生比较。

初始目标：无明显恶化的读取/首屏体验；简单文件不得因新引擎引入持续卡顿；复杂 Office 重点看准确性和可用性改善。具体首屏 P95、双窗峰值内存、30 次切换后的允许增长在 P0 基于设备可用 RAM 与实测基线书面冻结，不在缺少硬件和数据时捏造固定秒数或 MB。禁止通过降低画质、跳过页面或不渲染表格作弊。预览服务不分配生成模型 GPU，不承诺 WebView 不使用 GPU 加速。

## 9. 验收清单（实施前冻结，未测均为未完成）

### 9.1 产品与体验

| 编号 | 验收用例 | 通过标准 |
|---|---|---|
| DV-UX-01 | 从文档选择打开 DOCX/PDF/XLSX/PPTX | 实际内容可读、页/表/图无关键遗漏；未合格格式明确原生/不支持 |
| DV-UX-02 | 主制度 + 附件、新旧制度并排 | 两窗身份准确，独立加载、滚动、缩放，关闭互不影响 |
| DV-UX-03 | 多标签、快速切换、A/B 交换 | 不串页、不残留旧搜索或高亮；可安全恢复已支持状态 |
| DV-UX-04 | 搜索、上一处/下一处、文本选择 | 中文、数字、条款号匹配可核对；不支持时禁用 |
| DV-UX-05 | 页码、缩放、适宽/适页、工作表切换 | 按渲染能力显示；页码 0/1 基转换无偏差 |
| DV-UX-06 | 拖拽、全屏/Esc、窄屏 A/B | 活动侧独立，键盘及焦点恢复正常，不发生双全屏 |
| DV-UX-07 | 原件/解析内容切换（若能力成立） | 明确来源类型，原件不变、解析内容有序可追溯 |
| DV-UX-08 | 工作台、附件/沿革、原生问答入口 | 现有安全跳转、业务状态及返回恢复不退化 |
| DV-UX-09 | 六语、深浅主题、长标题 | TDesign 语义样式，完整翻译，无关键截断、正文无非预期反色 |
| DV-UX-10 | 不支持、加密、渲染警告 | 明确真实状态，可控重试/兼容回退，无虚假成功 |

### 9.2 安全、证据与资源硬门禁

| 编号 | 验收用例 | 通过标准 |
|---|---|---|
| DV-SEC-01 | 未授权 KB/文档、篡改 ID 或版本 | 不提供原件；不泄漏标题、历史或关系；不因回退放行 |
| DV-SEC-02 | Viewer/Editor 下载区别 | 正式 UI 无 Zrimo 原件下载捷径，现有下载 API 继续实时授权 |
| DV-SEC-03 | 历史观测版本、读取中换件 | 固定历史无字节证据只展示记录；身份变化中止，不显示新原件冒充旧版 |
| DV-SEC-04 | 权限撤回、附件失效、Host 不可用 | 后续读取/重新打开/下载保持现有拒绝；应用缓存及时按契约清理 |
| DV-SEC-05 | 快速切标签、认证/租户切换、迟到事件 | 取消旧请求、拒绝迟到结果，作用域清理，不能串数据 |
| DV-SEC-06 | 文档嵌入外链/宏/OLE、外部字体 | 本地资产按白名单加载，外链不隐式读取，宏脚本不执行 |
| DV-SEC-07 | 损坏、超限、加密、Worker 崩溃 | 有界失败；无白屏/无限重试；不能自动放宽安全限制 |
| DV-SEC-08 | 正常/过期/歧义/无文本定位 | 精确结果有原件及匹配依据；无法证明明确降级；错误精准标记=0 |
| DV-SEC-09 | 勘误撤回、关系变化、历史阅读 | 保留已有事实与权限分离；不将历史阅读变为 RAG 许可 |
| DV-SEC-10 | 静态资源、依赖及许可证 | 固定 tag、包、完整 notices、依赖审计及构建记录；无未审计外部资源 |
| DV-PERF-01 | 同文件冷/热基线、双窗峰值 | 与 P0 冻结预算一致，原文与关系按现有机制解耦 |
| DV-PERF-02 | 30 次打开/关闭/切换，含窄屏与退出全屏 | 无持续内存/Worker/事件监听增长超预算；另一窗不被销毁 |
| DV-PERF-03 | 大文件与资源压力 | 有界内存与响应，不能预加载非活动标签，不阻塞现有问答主界面 |
| DV-PERF-04 | Windows Lite 断外网并启动 | 本地 Worker/WASM/字体正常，所有资源来源符合配置，实机无未授权联网 |
| DV-REG-01 | 原生预览与问答引用回归 | 旧入口、PDF bbox/文本定位、原生格式、全屏和下载正常 |
| DV-REG-02 | 原生管理、扩展和旧图 | 无越界修改，现有测试和使用行为不退化，旧图按 DOC-STUDIO 退役 |
| DV-REG-03 | 实际发布及回滚 | 构建清单/哈希可核对；启停开关和包级回滚经授权演练成功 |

测试至少分「原生基线」「Zrimo PoC」「正式宿主集成」「Windows Lite 安装版」四类报告。对每个用例记录 PASS/FAIL/N/T、环境、样本 ID、命令、证据路径、缺陷编号和回退措施。隔离夹具不代替真实授权、实机安装版及复杂文件测试。

## 10. 发布、升级与回滚

- 功能开关默认关闭，PoC 不接管全局原生预览；正式按已通过资格的格式逐步启用。初期不在同一次变更删除任何原生依赖。
- 构建入口、完整静态资源清单、npm lockfile、SDK 版本和 SHA-256 必须归档。Windows Lite 的资源拷贝/安装路径与健康检查沿用现有部署规则；部署需另经授权。以当前已知 `resources/web-backup-20261125-0030` 为历史回滚记录，而不是默认假设目录仍存在或可用，实际部署前核验新备份。
- 发生渲染回归、离线资产缺失、错误精准定位、内存失控或权限异常时关闭实验开关；恢复经核验的原生路径。如配置开关不可用，按授权回滚前端资源整包，不恢复旧业务数据库，不改文档索引。
- 升级 Zrimo 时重复格式、身份、恶意输入、双窗、离线和全量 UI 回归；原生上游升级时复查 `readerApi` 读取契约和原生定位入口。不要以 SDK 的语义版本代替实际兼容验证。

## 11. 工程验收及交付文件

建议每阶段按 AGENTS.md 的范围审计执行。以下为待执行命令，未在本次设计中运行：

```bash
# 仓库根目录，只读核对
 git status --short
 git diff --stat
 git rev-parse HEAD
 git describe --tags --always
 git diff -- internal/
# frontend/，实际新增依赖与构建脚本经授权后执行
 npm run check-i18n
 npm run type-check
 npm run build
 npm test
 node scripts/verify-document-reader.mjs
# 仓库根目录，变更范围复核
 git diff --check
 git status --short --untracked-files=all
 git diff --name-only
```

增加专项目标测试：引擎路由、资源路径、代次与释放、权限回退阻断、身份/格式判定、双窗状态、定位粒度及无错误精确标记；命令以实际实现为准。若确实没有后端修改，不声称运行过全量 Core/Host 测试；后端缺口另行申请，依 AGENTS 运行独立模块验证。

| 交付物（建议路径） | 内容 |
|---|---|
| `docs/evidence/doc-viewer-baseline-YYYY-MM-DD.md` | 源码基线、依赖、样本及性能预算 |
| `docs/evidence/doc-viewer-poc-YYYY-MM-DD.md` | 原生/Zrimo 格式矩阵、截图、实测数据、缺陷 |
| `docs/design/ADR-DOC-VIEWER.md` | 最终按格式决策、许可、回退和 Fork 判定 |
| `docs/evidence/doc-viewer-acceptance-YYYY-MM-DD.md` | 实机安全、全量回归、部署与回滚证据 |
| `TASK_BACKLOG.md` | DV-P0..P4 的状态、依赖、剩余项、验证命令与真实部署标识 |
| `PATCHES.md` | 仅记录实际新增的原生接线；无原生修改则明确说明 |
| `PROJECT_CONTEXT.md` | 正式验收后写入稳定事实；不提前把建议写成已实现 |

## 12. 首轮 Codex 实施提示（待用户授权后交付）

> 目标：完成 DOC-VIEWER 专项，先构建整体可用的多格式阅读框架，再逐步完善细节。本轮先执行 P0/P1a，P1b 在门禁通过后推进；不自行发布生产、不修改核心、不开启条件项。
>
> 1. 读取仓库 AGENTS.md、PROJECT_CONTEXT.md、TASK_BACKLOG.md、PATCHES.md、BASELINE.txt、阅读中心 v1.1、最新源码映射与本专项。先执行 git 状态/版本审计，保留用户已有修改。远程核对基线 `87642ade` 不是本机当前 HEAD 的保证；以工作区实际事实报告差异。
> 2. 审核 `ReaderPane.vue`、`readerApi.ts`、`DocumentReaderView.vue`、`documentReader.ts`、SourceLocator 及现有原生预览。明确读取前后身份、固定历史拒绝、权限和返回契约；不另建文件接口。
> 3. 固定 Zrimo v0.1.2，审查 npm 产物、源码 tag、依赖许可与本地静态资源。不得直接访问外部在线 Demo 上传业务文档。PoC 使用脱敏样本，测试原生与 Zrimo DOCX/PDF/XLSX/PPTX、两个实例、搜索/复制、字体、复杂表格、关闭与离线；实际输出可复现实验和截图。
> 4. PoC 可用内置 UI（脱敏且有下载许可的身份），正式集成关闭内置 UI 并由宿主控制下载与 TDesign 操作。新增组件、修改文件和依赖前先说明范围。最小变更完成选文件→Zrimo 阅读→附件/双窗→返回；保留原生回退与证据路径；没有可信定位就不假装定位成功。
> 5. 不重写阅读中心、工作台、关系、版本、RAG、Core/Host，不新增数据库/服务，不废弃原生预览或旧图，不擅自创建 Fork。DS-D03 的引擎替换例外须由本专项审批，其他 v1.1 裁决保持。
> 6. P1 完成后按测试结果决定格式 allowlist；通过方可推进 P2/P3。逐阶段更新 TASK_BACKLOG，明确已实现/隔离验证/真实桌面验收/已部署四类状态。发现关键排版缺陷先归因，再提出是否需 Fork 或 PDF 转换；不得直接扩大改造范围。
> 7. 按 §9 验收，完整记录测试退出码、未验证项及基线差异。全量前端测试需在最终修复后重跑；实机验收需用户授权和环境可用。任何失败、权限疑问、版本漂移或不可维护依赖都必须记录，不能用演示替代正式准入。

## 13. 参考与证据索引

### LanRAG-Next 当前分支（本方案静态核对依据）

- [远程 HEAD 87642ade](https://github.com/ybochuan/LanRAG-Next/commit/87642ade0f9787e51faa34dd746acf52612c88fc)
- [阅读中心实现 5311e503](https://github.com/ybochuan/LanRAG-Next/commit/5311e50385196b9a281a70e7a7d1ea87627a36f5)
- [TASK_BACKLOG.md](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/TASK_BACKLOG.md)
- [AGENTS.md](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/AGENTS.md)
- [PATCHES.md](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/PATCHES.md)
- [PROJECT_CONTEXT.md](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/PROJECT_CONTEXT.md)
- [源码映射与验收记录](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/docs/evidence/document-studio-source-mapping-2026-11-24.md)
- [ReaderPane.vue](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/frontend/src/views/document-reader/ReaderPane.vue)
- [readerApi.ts](https://github.com/ybochuan/LanRAG-Next/blob/87642ade0f9787e51faa34dd746acf52612c88fc/frontend/src/views/document-reader/readerApi.ts)
- [阅读中心实现目录](https://github.com/ybochuan/LanRAG-Next/tree/87642ade0f9787e51faa34dd746acf52612c88fc/frontend/src/views/document-reader)
- 文件库：`TASK_BACKLOG(1).md`、`PATCHES(1).md`、`LanRAG-Next-文档阅读中心与关联研读系统-v1.1-最终方案-2026-11-24(1).md`。这些副本提供设计及此前实机记录；远程 HEAD 更新了提交事实。

### Zrimo 固定版本（公开文档事实，不代表本项目实测）

- [GitHub v0.1.2](https://github.com/bnku/zrimo/tree/v0.1.2)
- [API reference](https://github.com/bnku/zrimo/blob/v0.1.2/docs/api/reference.md)
- [Headless API](https://github.com/bnku/zrimo/blob/v0.1.2/docs/api/headless.md)
- [Basic UI](https://github.com/bnku/zrimo/blob/v0.1.2/docs/ui.md)
- [Compatibility](https://github.com/bnku/zrimo/blob/v0.1.2/docs/compatibility.md)
- [Security](https://github.com/bnku/zrimo/blob/v0.1.2/docs/security.md)
- [Performance](https://github.com/bnku/zrimo/blob/v0.1.2/docs/performance.md)
- [Fonts](https://github.com/bnku/zrimo/blob/v0.1.2/docs/fonts.md)
- [Workspace scripts](https://github.com/bnku/zrimo/blob/v0.1.2/package.json)
- [上游官网](https://bnku.github.io/zrimo/)、[Office OOXML Viewer](https://github.com/yukiyokotani/office-open-xml-viewer)

### 已知局限与待批准事项

本次仅完成静态设计；没有安装 npm 包、构建 Zrimo、实测当前设备或真实文件，也没有修改仓库/任务文件、创建 Fork、部署服务。当前完整源码与发布产物对应性、Windows Lite WebView 兼容、实际剩余 RAM、中文字体/复杂排版、精确 locator 与资源释放仍须在 P0/P1 核验。空间独立页面此前未返回可访问条目，使用的是已读文件库副本与最新远程分支。建议任务编号、文件落位、试验阈值、评分权重和排期为本方案提出的可审查选择，未视作用户已批准的实施事实。

**专项完成定义：** 在现有阅读中心内形成真实可用的多格式阅读体验，已通过资格的格式采用 Zrimo，未通过的格式安全回退；现有身份、权限、证据及工作台联动不退化；完成桌面验收和可回滚发布记录。深度 Fork、OFD、新服务、自动条款差异不作为首期完成的前置条件。
