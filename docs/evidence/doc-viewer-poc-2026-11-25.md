# DOC-VIEWER · 首轮 DOCX 对照记录

**状态**　DV-P0 部分完成。DV-P1a 隔离原型已构建并完成 Windows Edge 冒烟。待补获准的脱敏业务文件、Windows Lite WebView2、真实授权负向和正式性能预算。

## 比较环境

- 仓库基线 `87642ade`，原生引擎通过同一份 `frontend/src/components/document-preview.vue` 组件与 `sourceMode` 渲染。候选使用 `@zrimo/viewer@0.1.2`，两个引擎使用相同 DOCX 字节。
- Windows Edge `158.0.4370.26`，1720 × 1100 CSS px，简体中文环境。本机有宋体。参考软件为 LibreOffice `26.8.0.3`，用隔离配置转换为 PDF 并生成参考页 PNG。
- 原型在 `examples/doc-viewer-poc/`，构建产物、样本及截图在 `tmp/doc-viewer-poc/`。生成步骤见[原型说明](../../examples/doc-viewer-poc/README.md)。

| 样本 | 源文件 SHA-256 | 现有预览 | Zrimo | LibreOffice 参考 | 观察 |
|---|---|---|---|---|---|
| 合成资料归档制度 | `3dfc912cb5bb5292a227996c2275f3f6eb911b1e03fb47647ac52cb0a1c207dc` | 1 个连续文档段 | 3 页，末尾标记第 3 页 | 4 页 | 两边均显示中文标题和编号。候选可按页跳转并高亮检索命中，页分布与参考件不同。 |
| 合成跨页检查表 | `006e7a662cd25872f05949d806507aaf1cc1ebb4b89ec7497097a52f0c727ace` | 1 个连续文档段 | 2 页，末尾标记第 2 页 | 3 页 | 候选截图可见前三项纵向合并和第二页重复表头，搜索命中 42 个检查点，末行可见。参考件首行至第 16 项在第一页，候选第二页从第 24 项开始。 |

现有预览的 DOM 段数不可当作排版页数。企业模板、固定页脚及页码字段、印章图像、文字基线、同页表格行序还需要用业务样本逐点验收。

## 已验证

| 验证 | 结果 |
|---|---|
| 原型 `npm run build` | 退出 0，按 `/doc-viewer-poc/` 基址构建并复制本地运行资产。Vite 仍有体积提示，本次产物只供隔离预览。 |
| 原型 `npm test` | 退出 0。验证原生正文可见、候选 Canvas 有画面、两种样本唯一末尾标记、42 个表格检查点、两个实例并存、关闭 B 后 A 可用、快速切换、`ui:true` 和 `ui:false`、本地文件读取。外部 HTTP 请求、失败响应与页面异常均为 0。该用例是 Edge 隔离测试。 |
| WASM 与 Worker | 运行期间 DOCX WASM 由 Vite hashed asset 返回 `application/wasm`，观察到 Inline Blob Worker。另以本地 HTTP 验证随包的 PDF Worker 返回 JavaScript MIME。PDF 引擎尚未在此轮运行。 |
| 候选依赖 | 隔离锁文件固定 `@zrimo/viewer@0.1.2`。`npm audit signatures` 检查实际安装的 6 个包，各自的 registry 签名与 attestations 均通过。`npm audit --omit=dev --registry https://registry.npmjs.org` 本轮返回 0 漏洞。正式许可清单及第三方 notices 待确认。 |
| 原生前端 | 将附件文件图标尺寸移至 TDesign `size`。样式测试 10/10，正式前端 `npm test` 2,528 通过、2 跳过、0 失败，`npm run type-check` 和独立输出的 `npm run build` 均退出 0。 |

## 准入判断

Zrimo 仍为 DOCX 候选。浏览体验目前有按页、搜索和续页表头的收益，分页与参考软件仍有差异。固定版本的 modern Office adapter 为 Silurus 指定 `mode: "main"`，本地观测到 Blob Worker 不能证明 Canvas 绘制已移出主线程。下一轮测真实复杂业务件的排版、长任务、双窗峰值及退出后的资源回收（该轮已于 2026-11-25 完成复杂合成夹具，见文末）。单次 `smoke.json` 计时用于定位耗时，无冷暖分布。

正式阅读窗格接入还需要 Windows Lite WebView2 和真实 Core/Host 权限结果。安装版入口复核仍为 `5f86d3c11d00e6151b672e2d55cd187e2008f3e648baf9f2bd1d709d38a143b5`，相关生产路径保持在原生阅读中心。

## 2026-11-25 正式前端接入

> 以下为首次接入记录，构建与入口哈希为当次值；当前部署哈希见文末“2026-11-25 阅读中心多格式与紧凑 UI”。

用户确认优先使用 Zrimo。阅读中心对普通 DOCX 默认使用 `@zrimo/viewer@0.1.2`。其他格式、引用精确定位和固定历史沿现有路径。可以在运行配置设置 `READER_DOCX_ENGINE: 'native'` 恢复原生预览。

| 验证 | 结果 |
|---|---|
| `npm test` | 标准配置退出 0，2,530 通过、2 个选配浏览器用例跳过。 |
| `npm run type-check` / `npm run check-i18n` | 均退出 0，六语审计 15/15。 |
| `npm run build` | 退出 0，`frontend/dist/index.html` SHA-256 为 `f687c9c789a7da95de9f17fc1fa5a29fd281fd4e64eba0989734d457a6512caf`。Zrimo 同源目录 233 个文件，含许可和 notices。 |
| `node scripts/verify-document-reader.mjs` | Windows Edge 加载生产构建与隔离接口夹具，退出 0。覆盖普通 DOCX 默认渲染、双窗、全屏、中文搜索、403 拒绝、超限前停止读取、WASM 请求失败后的兼容回退及配置切回原生。 |

本次测试使用了隔离接口。Windows Lite 安装包、实际 Core/Host 和双窗资源预算保留在发布验收。

### 工作区 Web 切换

已将构建部署至 `D:/LanRAG-Next/web`（WSL 路径 `/mnt/d/LanRAG-Next/web`）。暂存目录与正式目录分别重新打开页面执行 `verify-document-reader.mjs`，均退出 0。部署入口哈希与上表一致，原 `web/config.js` 哈希保持一致。原目录备份于 `tmp/doc-viewer-zrimo-web-backup-20261125-211001`。Windows Lite 安装目录维持其独立发布进度。

### Windows Lite 安装版切换

2026-11-25 将现有安装目录的 `resources/web` 切换为已验证的 Zrimo 前端构建。入口 SHA-256 为 `f687c9c789a7da95de9f17fc1fa5a29fd281fd4e64eba0989734d457a6512caf`。504 个构建文件逐文件匹配，原 `config.js` 与切换前逐字节一致。完整旧目录为 `resources/web-backup-zrimo-20261125-211410`，其入口仍匹配切换前的 `5f86d3c11d00e6151b672e2d55cd187e2008f3e648baf9f2bd1d709d38a143b5`。

| 验收项 | 结果 |
|---|---|
| 安装版启动 | 已启动 `LanRAG Lite` 主窗口，观察到该进程的 WebView2 Runtime `158.0.4370.33`。 |
| 安装版实际 HTTP 服务 | 入口返回 200 并引用新构建，DOCX WASM 返回 200 和 `application/wasm`，随包 Worker 返回 200 和 JavaScript MIME。 |
| 实际 Core 和 Host | `scripts/test-windows-lite.ps1` 退出 0。Core `ok`，Host `ready`。限域凭据范围一致，未知文档及匿名 Host MCP 请求均拒绝。 |
| 实际 DOCX 原件 | 使用既有测试文档和限域凭据。匿名读取返回 401，授权读取返回 200 和 DOCX 原件，读取前后文档、知识库、哈希及大小匹配。 |
| 安装资源的界面回归 | 以安装目录为资源根运行 `node frontend/scripts/verify-document-reader.mjs`，独立 Edge 和隔离接口夹具通过默认渲染、双栏、中文搜索、全屏、超限、403、回退、切回原生以及既有阅读中心操作。 |
| 安装窗口内的交互 | 待测。Computer Use 返回 `not approved to use lanrag desktop`。安装窗口内的实际排版、双窗资源峰值及交互单列；跨库角色、附件失效和 Host 停止仍属后续现场用例。 |

### 阅读中心布局与附件列表调整

依据用户提供的双栏和附件截图，缩小读区外围间距，以分隔行呈现附件并保留完整标题提示。双栏标题等高，桌面点击或键盘聚焦即可切换活动侧。查找默认收起，阅读窗格支持 Ctrl+F、Enter、Shift+Enter 和 Esc。适宽、整页随窗格尺寸更新，手动缩放沿用引擎状态。

- 前端全量测试退出 0，2,530 通过、2 跳过。类型检查、六语审计 15/15 和构建均退出 0。
- 独立 Edge 在工作区与安装目录分别从新入口运行夹具，覆盖宽窗双栏对齐、窄窗切换、活动侧、附件打开、标题提示、工具栏、面板开合后的适宽和查找快捷键。
- 运行中的 Windows Lite 已返回新版入口和 DOCX WASM。两个目标的 504 个当前构建文件逐项核对，原配置保留，旧静态块继续为尚未重新加载的窗口服务。
- 新入口 SHA-256 为 `6627614af61113919bbb15fbf8efe49e1cac69f0096b40f7cf3a036cb35be356`。回退备份为工作区 `tmp/reader-ux-web-backup-20261125-214241` 和安装目录 `resources/web-backup-reader-ux-20261125-214241`。对照截图使用合成文件，位于 `tmp/doc-reader-layout-acceptance.png`。

## 2026-11-25 阅读中心多格式与紧凑 UI

- 阅读中心普通文档改用 Zrimo 统一适配：`readerApi.ts` 的 `ZRIMO_FORMATS` 覆盖 DOCX/DOCM/XLSX/XLSM/PPTX/PPTM/PPSX/DOC/XLS/PPT/PDF/CSV/TSV/PNG/JPEG/GIF/WebP/SVG/BMP/TIFF；`ZrimoReader.vue` 显式使用 `layout: 'continuous'`，保留连续滚动与单页模式，并新增可选双页视图：同一已加载 `viewer` 通过 `renderPage` 渲染两块 canvas，按页对翻页，单页/双页图标切换；XLSX 多工作表时提供 `t-select` 工作表切换；固定版本、勘误和证据定位仍走原生 `DocumentPreview`。
- 阅读体验按原生 WeKnora 骨架收敛：搜索区改为 `t-card` 三列布局，打开/对照按钮与文档选择同一行，窄窗自动堆叠；侧栏 18–24rem，标签页标题不再被 14em 截断；版本沿革、关系与附件列表减少重复元信息，版本沿革日期只显示日期并移除重复的“当前观测版本”标签，阅读标签栏无多余纵向滚动条；附件行仅保留图标、单行省略标题和纯图标（眼睛/交换）打开／对照操作（图标按钮带 aria-label 与 title，不再显示可见文字；截图 `tmp/doc-studio-reader-icononly.png`）。
- 验证：`npm test` 退出 0（2539 tests，2537 pass、2 skip、0 fail，使用真实 Git for Windows shell 的 cmd.exe）；`npm run type-check && npm run build` 退出 0；`npm run check-i18n` 退出 0（15/15）；`frontend/dist` 505 个文件、入口 `index.html` SHA-256 `882a3a9eb7209f178aa57c46b78e865ee9f1de347d7840803196c9b0035158ea`（4736 字节）。
- `node frontend/scripts/verify-document-reader.mjs`（带 `DOC_READER_DOCX_FIXTURES`、`DOC_READER_FORMAT_FIXTURES` 与 `DOC_READER_COMPLEX_FIXTURES`）退出 0，输出 `Reader DOCX fixture passed: ...`、`Reader multi-format fixture passed: XLSX, PDF, PNG, PPTX, CSV, SVG, JPG, WEBP, BMP, TIFF`、`Reader dual-pane multi-format fixture passed: PDF + PPTX`、`Reader complex docx passed: 61 pages...`、`Reader complex pptx passed: 20 pages...`、`Reader complex pdf passed: 30 pages...`、`Reader complex dual-pane passed: PDF + PPTX...`，并新增 DOCX/PDF/PPTX 双页视图断言（两页 canvas、DOCX Pages 1–2／3–4 翻页、PDF/PPTX Pages 1–2 of 2、单页恢复；截图 `tmp/doc-studio-reader-spread-20261126.png`），以及既有 Markdown 工作台/双栏/窄屏/中英切换夹具，含 488px 内容区无横向溢出断言（截图 `tmp/doc-studio-reader-content-488.png`）。
- Windows Lite 安装版与 `tmp/doc-studio-windows-stage` 已同步为 505 个文件、同入口哈希 `882a3a9eb7209f178aa57c46b78e865ee9f1de347d7840803196c9b0035158ea`；安装目录备份 `resources/web-backup-20261126-0020`，stage 备份 `tmp/doc-studio-windows-stage-backup-20261126-0020`，部署脚本 `tmp/deploy-doc-studio-web-20261126-0020.ps1`。安装窗口内的实际界面由用户自行复核。
- 边界：XLSX/PDF/PNG/PPTX/CSV/SVG/JPG/WEBP/BMP/TIFF 已用合成夹具验证真实加载与画布渲染，DOCX 有独立浏览器夹具，PDF/PPTX 已验证连续多页滚动，PDF+PPTX 已验证双窗同时渲染；61 页 DOCX、20 页 PPTX、30 页 PDF 复杂业务件连续滚动与 PDF+PPTX 双窗采样峰值已补测。全部 20 个格式逐一实渲与真实权限负向仍待补测，不能据此宣称所有格式真实排版已通过。
