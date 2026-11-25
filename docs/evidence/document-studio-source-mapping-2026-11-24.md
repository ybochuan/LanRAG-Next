# 文档阅读中心源码映射与 P0 核验证据

- 日期：2026-11-24（2026-11-25 复核更新）
- 仓库：`D:\LanRAG-Next`
- 分支与提交：`codex/m1-parser-routing`；工作区基线 `87642ade0f9787e51faa34dd746acf52612c88fc`（`docs(doc-studio): record implementation commit`）；DOC-VIEWER 功能提交 `d8445458`（`feat(document-reader): ship Zrimo multi-format reader with two-page view`）；阅读中心实现提交 `5311e503`；核心构建基线 `d6057e41` / `batch6-1-gd6057e41`
- 上游基线：`Tencent/WeKnora v0.9.0` / `f17ee8a`（`BASELINE.txt`）
- 工作区状态：阅读中心实现提交为 `5311e503`；DOC-VIEWER 多格式与阅读体验改动已提交为 `d8445458`（Zrimo 阅读器、格式矩阵、双页视图、紧凑 UI 等），工作区干净，`internal/` 无 diff。本文记录实际源码映射、权限边界与已执行核验，不构成核心改动授权。

## 1. 边界与约束

- `internal/` 为受控区，本轮未修改；`git diff -- internal/` 为空。
- `extensions/` 为独立 Go module；未新增对 WeKnora `internal/` 包的 import、require 或 replace。
- 前端改动位于 `frontend/`；全部可见文案进入六语 locale，TDesign token 与原生页面骨架为 UI 基准。
- 阅读中心实现已提交为 `5311e503`。Windows Lite 安装版前端已于 2026-11-26 00:20 切换至双页视图构建（505 个文件），入口 SHA-256 为 `882a3a9eb7209f178aa57c46b78e865ee9f1de347d7840803196c9b0035158ea`；回滚目录为 `resources/web-backup-20261126-0020`。

## 2. 原生预览契约

| 事实 | 源码位置 | 影响 |
|---|---|---|
| 预览接口只接收文档 ID，不接收版本 ID 或 source hash | `internal/handler/knowledge.go:2158-2185`：`PreviewKnowledgeFile` 从 `c.Param("id")` 取 ID，`GetKnowledgeFile(effCtx, id)` 在 2177 行按 ID 读取 | 无法通过现有预览接口请求某个固定历史版本的原始字节 |
| 预览前必须通过原生 KB viewer 权限与附件生命周期门 | `internal/handler/knowledge.go:2167` `resolveKnowledgeAndValidateKBAccess(c,id,types.OrgRoleViewer)`；2173-2175 `extensionbridge.GuardAttachmentDownload` 失败返回 403 | 前端不能靠切换 URL 绕过权限；阅读中心复用同一授权入口 |
| 原生 `file_hash` 是上传文件的 MD5，并会在处理中被重算 | `internal/application/service/knowledge_util.go:130-149` 使用 `crypto/md5` 计算文件流；`knowledge_process.go:3653-3654` 以 `calculateStr(knowledgeID, knowledge.FileHash, imageInfo)` 覆盖 `knowledge.FileHash` | `file_hash` 不能作为不可变原件字节摘要，也不能证明固定版本对应的原始文件 |
| 预览响应禁止缓存 | `internal/handler/knowledge.go:2183-2185` `Cache-Control: private, no-store` | 浏览器刷新仍重新请求授权预览；阅读器不得依赖缓存证明版本 |

### 2.1 预览与权限测试矩阵

| 场景 | 执行方式 | 结果 | 证据 |
|---|---|---|---|
| Markdown 原文预览、同一 KB 关联、双栏对照、键盘/指针调宽、窄屏 A/B、488px 内容区无横向溢出 | 隔离浏览器夹具 `frontend/scripts/verify-document-reader.mjs`，使用本地 HTTP 夹具与 Playwright | 通过；夹具输出 `Reader browser fixture passed: workbench restoration, same-KB comparison, keyboard and pointer resizing, pinned denial, narrow A/B, narrow 488px no overflow, en-US and zh-CN, read-only requests`，退出 0 | 本文件第 5 节；脚本位于 `frontend/scripts/verify-document-reader.mjs` |
| DOCX 单窗预览 | Windows Lite 安装版 WebView2，真实测试库，文档 `70ec19b1-fae0-4950-925b-38d1fff6ff9f`（`annex-06.docx`） | 通过；预览渲染、文件名与工具栏显示 | 截图 `422f15dd8485778d94129a7cc3fc332d0283f012592aba1e7bdca048c1619aee`（菜单）、`2ada468c168570358f5637adaeabde1913e199ac79597e32cd42d3c6b782ccb7`（预览） |
| DOCX 双栏对照与唯一全屏窗格 | Windows Lite 安装版，真实测试库，对照“记录清单”与“上门影像特殊情况说明” | 通过；双栏出现，活动 A 栏有“全屏预览”，非活动 B 栏只有“下载原件”；退出全屏后焦点回到触发按钮 | 截图 `1fe94698f043ee38e0c5aa1e8f32dc06effb37e1129258455e8137ded690a6e4`、`9129f4ff59c01c25981e5320679f42cdb0f2789746b81cf58964f77c110b25d0`、`f116cf1c06fd0c933bbfc5a1026eda6cd17536cc1e35d19bccd240e530c4897b` |
| 固定历史版本不取当前原文 | Windows Lite 安装版，制度沿革点击“查看版本记录”打开“原文 · 3216193a” | 通过；提示“仅有历史记录，无法核验此版本的原件。”，未调用当前预览 | 截图 `a3459eec5d517cfb4c967a8211d444ccc958a9b6e6006a994ed1a2de3bbf8e40` |
| 同级附件文档的附件与模板 | Windows Lite 安装版；打开 `annex-06` 后查看“附件与模板” | 通过：显示父主件提示和同级附件；按用户要求将附件行精简为标题与“打开”（对照时保留“加入对照”），不再显示附件序号、原文件、正文引用或摘要。 | 截图 `tmp/doc-studio-attachment-simplified-20261125-0002.png`（SHA-256 `3b26b5ed524f068e3cc076205911600882aa4a2a74e2bae8a130364926cb1fda`）；实现 `frontend/src/views/document-reader/ReaderAttachments.vue`；回归见 `readerEvidence.test.ts` |
| PDF、XLSX、图片等格式矩阵 | 未执行 | 未验证 | 需在安装版用真实授权文档补测；不据此宣称全部格式通过 |
| 无权限、跨库目标、附件生命周期拒绝 | 代码路径已核对，真实负向请求未执行 | 部分验证 | 需在真实 Core/Host 补 403 与跨库过滤验收 |

## 3. 版本与证据能力

| 事实 | 源码位置 | 结论 |
|---|---|---|
| 原生版本数据不携带证明原始字节的 source hash/resource | `extensions/lineage/graph.go:15-29` `GraphNode` 只有 ID/Type/KB/Document/Family/ObservationChain/BusinessFamily/Sequence/Current/State/Availability/CreatedAt/Title；`GraphEdge` 32-38 只有 ID/From/To/Type/Status | 版本族图可显示沿革，不能作为固定版本原件的字节证明 |
| 快照只含哈希，且 header 字段包含原生 `file_hash` | `extensions/lineage/snapshot.go:35-56` `headerNames` 含 `file_hash`；`Snapshot` 为 `HeaderFields/HeaderHash/Chunks/RootHash`；`rootDigest` 87-97 只绑定 scheme/tenant/KB/doc/HeaderHash/Chunks | 快照能证明扩展观察到的字段与分块记录，不改变原生 `file_hash` 为 MD5 且可重算的事实 |
| 版本节点可打开但固定版本原件不可核验时只能展示记录 | 阅读器 `ReaderPane.vue` 与 `ReaderHistory.vue` 实现 | 固定版本/附件版本进入“仅有历史记录”状态；不把普通预览冒充严格证据 |

**结论**：现有原生接口不具备“按固定版本返回可信原始字节”的能力。阅读中心因此将普通浏览与严格证据分离：普通浏览只做 metadata→preview→metadata 同文档校验；固定版本/附件版本只显示历史记录和受限元数据，不开放精确原文比对。

## 4. 关系与授权映射

| 事实 | 源码位置 | 前端处理 |
|---|---|---|
| 原生 related 仅以 source 授权的 KB scope 查询目标；跨库目标只替换 KB，不携带逐目标用户授权 | `extensions/business/related.go:100-102` `relationTargetScope` 返回 `{TenantID: source.TenantID, KnowledgeBaseID: targetKB}`；`management/business.go:88-91` 只检查配置 scope；151-156 按 `s.scopes[tenant,targetKB]` 过滤返回 | `readerRelations.ts:15-25` 过滤 `knowledge_base_id !== source.kb`、非法 readerID、自身、非法方向、空 edge_type、重复 relation；同库目标才请求标题和显示 |
| Host 的 HTTP 负载可能包含跨库标识 | `extensions/business/related.go:358-419` 的查询与 `management/business.go:151-156` 的响应过滤 | 前端对显式 foreign/legacy 行不渲染、不计数、不请求；同库目标走 native 精确授权 |
| 无同库服务端选择器 | 原生 `knowledge` 列表接口与扩展缺少逐目标筛选能力 | 作为 Baseline Limitation 记录；阅读中心不伪造服务端关系选择器 |

## 5. 双实例与全屏 P0 结论

- `document-preview.vue` 的公开 props 增加可选 `fullscreenEnabled`；不传时保持原生调用方行为不变。
- `ReaderPane.vue` 向原生预览传独立 `toolbarTarget`，并按 `fullscreenOwner` 控制 `:fullscreen-enabled`。
- `DocumentReaderView.vue` 以 A/B 活动窗格决定唯一 `fullscreenOwner`；单窗固定为 true，双栏由 `activeSide` 决定。
- 全屏退出后通过 `fullscreenTrigger` 恢复触发按钮焦点；fallback 全屏将焦点限制在预览根；保存并恢复 `body.style.overflow`。
- 桌面实测：双栏中仅活动 A 栏显示“全屏预览”，B 栏仅“下载原件”；全屏 `annex-05.docx` 后按 Esc 退出，UI Tree 焦点回到“全屏预览”切换按钮。

## 6. 旧图调用方清单（退役前保留）

| 调用方 | 当前入口 | 退役条件 |
|---|---|---|
| `frontend/src/router/index.ts:106-108` | `/platform/families`、`/platform/lineage`、`/platform/associations`、`/platform/revisions` 重定向到 `/platform/documents?view=<view>` | 阅读中心覆盖对应关系/沿革/来源入口并完成旧链接兼容回归 |
| `frontend/src/views/lifecycle/LifecycleView.vue:25,292,359,836` | `workspaceView==='lineage'`、`detailTab==='graph'`、graph tab 与 `openGraphDocument` | 关系与沿革验收覆盖后移除内嵌图 |
| `frontend/src/views/families/FamilyWorkspaceView.vue:46,605-607` | “version history” 按钮跳转 `/platform/lineage?mode=version-ancestry` | 版本沿革在阅读中心完整可用并验收 |
| `frontend/src/views/business/BusinessView.vue:57,465-478` | “view graph” 跳转 `/platform/documents?view=lineage&mode=relations` | 关系图入口由阅读中心替代并验收 |
| `frontend/src/views/lineage/LineageView.vue`、`LineageCanvas.vue`、`browser.fixture.mjs:25` | 旧可视化本体与夹具 | 旧图退役单独审批，不在本轮删除 |

## 7. P1 文件清单

新增：
- `frontend/src/views/document-reader/DocumentReaderView.vue`
- `frontend/src/views/document-reader/ReaderPane.vue`
- `frontend/src/views/document-reader/ReaderHistory.vue`
- `frontend/src/views/document-reader/ReaderAttachments.vue`
- `frontend/src/views/document-reader/model.ts` / `model.test.ts`
- `frontend/src/views/document-reader/readerApi.ts`
- `frontend/src/views/document-reader/readerPicker.ts`
- `frontend/src/views/document-reader/readerRelations.ts`
- `frontend/src/views/document-reader/readerDiscovery.test.ts`
- `frontend/src/views/document-reader/readerEvidence.test.ts`
- `frontend/src/stores/documentReader.ts` / `documentReader.test.ts`
- `frontend/scripts/verify-document-reader.mjs`

修改（阅读中心相关）：
- `frontend/src/router/index.ts`：注册 `/platform/reader`
- `frontend/src/stores/menu.ts`、`frontend/src/components/menu.vue`：菜单项与显示集合
- `frontend/src/components/document-preview.vue`：可选 `fullscreenEnabled`、焦点恢复/圈定、body overflow 恢复
- `frontend/src/components/doc-content.vue`、`frontend/src/components/ChatReferenceSourceView.vue`：增加“阅读原文”内部入口
- `frontend/src/views/lifecycle/LifecycleView.vue`、`BusinessView.vue`、`FamilyWorkspaceView.vue`、`FamilySuggestionPanel.vue`、`CorrectionWorkspace.vue`、`AttachmentWorkbench.vue`：工作台往返、草稿保护、阅读状态保存/恢复
- 六个 `frontend/src/i18n/locales/*.ts`：reader 命名空间键集

未修改：
- `internal/`、根 `go.mod`/`go.sum`、`extensions/go.mod`/`go.sum`、`PdfSourceViewer.vue`、`KnowledgeBase.vue`。

## 8. 待批准的薄补丁与剩余缺口

- 原生预览只加可选 prop，不改变 Core/Host 接口；PdfSourceViewer 不修改。
- 文档内容/引用入口只增加“阅读原文”内部路由；不改变原生预览调用。
- 仍需真实 Core/Host 补测：PDF/XLSX/图片格式、真实权限 403、跨库关系负向、附件生命周期撤销、勘误撤回后的来源状态。
- 严格证据仍需上游提供固定版本字节绑定或专用只读接口；在此之前只显示历史记录，不开放精确原文比对。

## 9. 本轮验证记录

- `npm run check-i18n`：退出 0（15/15）。
- `npm run type-check && npm run build`：退出 0；`frontend/dist` 505 个文件，入口 `index.html` SHA-256 `882a3a9eb7209f178aa57c46b78e865ee9f1de347d7840803196c9b0035158ea`（4736 字节）。
- `npx tsx --test` 定向阅读用例：退出 0（含 Zrimo 格式矩阵、XLSX 流式读取、Zrimo 组件静态断言、固定证据拒绝、普通预览前后校验、附件文档父件族展示、附件动作变化失败关闭、历史当前观察解析、业务版本对照按文档 ID 打开）。
- 完整前端 `npm test`：退出 0（2539 tests，2537 pass、2 skip、0 fail）；使用 `C:\Program Files\Git\cmd` 置于 PATH 的 cmd.exe 运行，排除此前 POSIX shell 环境误报。
- `node frontend/scripts/verify-document-reader.mjs`（带 DOCX、十种合成格式与复杂业务件夹具）：退出 0，输出 `Reader DOCX fixture passed: ...`、`Reader multi-format fixture passed: XLSX, PDF, PNG, PPTX, CSV, SVG, JPG, WEBP, BMP, TIFF`、`Reader complex docx passed: 61 pages...`、`Reader complex pptx passed: 20 pages...`、`Reader complex pdf passed: 30 pages...`、`Reader complex dual-pane passed: PDF + PPTX...` 与 `Reader browser fixture passed: ... narrow 488px no overflow ...`。
- Windows Lite 安装版资源切换与桌面实测：见 TASK_BACKLOG.md 第 18-20 行及本文件第 2.1 节截图。
