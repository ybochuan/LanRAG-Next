# 文档阅读中心 Zrimo 多格式阅读设计

- 日期：2026-11-25
- 状态：已实现并通过隔离浏览器夹具；Windows Lite 前端已部署（含双页视图），界面由用户复核
- 依据：用户 m02516/m02517/m02527；[阅读中心 v1.1](LanRAG-Next-文档阅读中心与关联研读系统-v1.1-最终方案-2026-11-24.md)
- 实现：`frontend/src/views/document-reader/`（`readerApi.ts`、`ReaderPane.vue`、`ZrimoReader.vue`、`DocumentReaderView.vue`）

## 1. 目标与边界

1. 普通、当前版本的文档优先由 Zrimo 渲染，覆盖用户实际会遇到的 Office、PDF、图片和文本表格格式。
2. 固定版本、勘误记录和证据定位继续走原生 `DocumentPreview`；原生 `/preview` 只按文档 ID 返回当前文件，不能把当前字节当作固定版本原件。
3. Zrimo 不可用、资源超限或解析失败时保留原生兼容渲染；不因统一阅读器牺牲授权、证据定位和可回退性。
4. 文档读取保持原有 metadata → preview → metadata 前后核对；预览流上限 50 MiB，超限在读取前或流式读取中停止并取消。

## 2. 格式矩阵

允许列表：DOCX、DOCM、XLSX、XLSM、PPTX、PPTM、PPSX、DOC、XLS、PPT、PDF、CSV、TSV、PNG、JPEG、GIF、WebP、SVG、BMP、TIFF。

- `readerApi.ts` 的 `ZRIMO_FORMATS` 统一格式名，`zrimoFormat()` 优先使用 `file_type`，再回退文件名后缀；`jpg` 映射为 `jpeg`、`tif` 映射为 `tiff`。
- 未在允许列表内的普通文档仍走 `DocumentPreview`；固定版本和 `kind: 'correction'` 不进入 Zrimo。
- 所有 Zrimo 格式统一通过流式读取进入内存 Blob，避免大文件一次性分配；50 MiB 上限为 `READER_ZRIMO_MAX_BYTES`。

## 3. 视图与交互

- 阅读窗格使用 `layout: 'continuous'`：在单一滚动容器中连续显示多页、多幻灯片或多工作表，解决“单页居中、左右大片空白”的问题；Zrimo 同时支持 `layout: 'single'`，当前默认连续模式。
- 可选双页视图：同一已加载 viewer 通过 `renderPage(pageIndex, canvas, { zoom, devicePixelRatio, priority })` 直接渲染两块 canvas，按页对翻页，工具栏用图标按钮在单页/双页间切换；双页时隐藏缩放、适宽/整页和搜索，退出后回到对应单页并恢复适宽/整页。该模式不新建第二个 viewer，避免重复解析与内存翻倍；Zrimo 原生无横向 spread 布局，双页是应用层渲染扩展。
- 工具栏保留页码/上一页/下一页、缩放、适宽/整页、查找、全屏和 Zrimo 标记；XLSX 多工作表时显示 `t-select` 工作表切换。
- 搜索结果通过 `viewer.search()` / `searchNext()` / `searchPrevious()` 驱动，支持 Ctrl+F、Enter、Shift+Enter、Esc。
- 全屏只允许当前活动窗格进入；退出后恢复触发按钮焦点，原生全屏失败时使用阅读窗格覆盖层并限制焦点逃逸。
- 阅读中心骨架遵循 WeKnora 原生 `section.section` + `section-header`/`section-description`，搜索区使用 `t-card` 三列网格，窄窗自动堆叠；侧栏 18–24rem。

## 4. 验证与已知限制

已执行：

- `readerEvidence.test.ts`：格式矩阵、XLSX 流式读取、固定版本/勘误拒绝、metadata 前后核对、超限取消、双页渲染静态断言。
- `frontend/scripts/verify-document-reader.mjs`：DOCX 双窗；额外合成夹具验证 XLSX、PDF、PNG、PPTX、CSV、SVG、JPG、WebP、BMP、TIFF 的实际加载和画布渲染，验证 DOCX 双页 canvas、Pages 1–2／3–4 页对翻页与单页恢复，PDF/PPTX 双页切换与 Pages 1–2 of 2，PDF/PPTX 连续多页滚动、PDF+PPTX 双窗同时渲染及 488px 内容区无横向溢出；复杂业务件夹具验证 61 页 DOCX、20 页 PPTX、30 页 PDF 的连续滚动，并以 PDF+PPTX 双窗采样记录浏览器堆峰值约 92–107 MiB（两次隔离运行）。输出 `Reader multi-format fixture passed: XLSX, PDF, PNG, PPTX, CSV, SVG, JPG, WEBP, BMP, TIFF`、`Reader dual-pane multi-format fixture passed: PDF + PPTX`、`Reader complex docx passed: 61 pages...`、`Reader complex pptx passed: 20 pages...`、`Reader complex pdf passed: 30 pages...` 与 `Reader complex dual-pane passed: PDF + PPTX...`。
- `npm test` 2,539 tests / 2,537 pass / 2 skip / 0 fail；`npm run type-check && npm run build` exit 0；`npm run check-i18n` 15/15。

尚未宣称：

- 全部 20 个格式逐一做真实业务件排版验收；
- 复杂 Excel/PPT 的公式、图表、嵌入对象与打印分页；双窗资源峰值目前为浏览器堆采样，不等于桌面进程峰值；
- 安装版窗口内的实际界面由用户复核，不能以隔离浏览器夹具替代。

## 5. 验收命令

```powershell
cd frontend
npm run check-i18n
npm run type-check
npm run build
npm test
$env:BROWSERSKILL_TEST_CHROMIUM='C:/Program Files/Google/Chrome/Application/chrome.exe'
$env:BROWSERSKILL_TEST_PLAYWRIGHT='C:/Users/ybochuan/.dsh/profiles/desktop/node_modules/playwright-core/index.mjs'
$env:DOC_READER_DOCX_FIXTURES='D:/LanRAG-Next/tmp/doc-reader-fixtures'
$env:DOC_READER_FORMAT_FIXTURES='D:/LanRAG-Next/tmp/doc-reader-fixtures'
$env:DOC_READER_COMPLEX_FIXTURES='D:/LanRAG-Next/tmp/doc-reader-fixtures'
$env:DOC_READER_SPREAD_SCREENSHOT_PATH='D:/LanRAG-Next/tmp/doc-studio-reader-spread-20261126.png'
node scripts/verify-document-reader.mjs
```
