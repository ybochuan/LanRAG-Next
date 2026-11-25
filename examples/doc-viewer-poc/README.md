# DOCX 阅读引擎对照原型

同一份合成 DOCX 同时交给 LanRAG 现有 `document-preview.vue` 和 Zrimo。可查看中文制度、跨页表单，切换 Zrimo 双窗，以及比较宿主工具栏与上游工具栏。构建和预览均使用独立入口，预览默认绑定回环地址。

## 运行

需要 Node.js 22.13+、已安装依赖的 `frontend/`、Python 的 `python-docx`（实测 1.2.0）和 Microsoft Edge。从本目录运行：

```powershell
npm ci --ignore-scripts --no-fund --no-audit --registry https://registry.npmjs.org
npm run build
npm test
npm run preview -- --port 4178 --strictPort
```

打开 [本地 DOCX 对照原型](http://127.0.0.1:4178/doc-viewer-poc/)。通过「打开本地 DOCX」选择获准的脱敏文件（50 MiB 以内），两个阅读器直接接收浏览器内的同一份文件。原型将网络和渲染资产限制在当前本地服务。`npm test` 自动启动临时预览，使用隔离的 Edge 会话，阻断非本地 HTTP 请求，并检查渲染、中文搜索、双窗切换、本地文件和 WASM 响应类型。

输出位于仓库根目录的 `tmp/doc-viewer-poc/`。其中 `public/samples/manifest.json` 是样本清单，`smoke.json` 是实测记录，`policy-comparison.png`、`form-comparison.png` 和 `dual-comparison.png` 是浏览器截图。

## 生成参考页

从仓库根目录执行。参考软件只读取生成的合成文件，使用独立 LibreOffice 用户目录。

```powershell
$poc = (Resolve-Path 'tmp/doc-viewer-poc').Path
$reference = Join-Path $poc 'reference'
New-Item -ItemType Directory -Force -Path $reference | Out-Null
$profile = ([Uri](Join-Path $poc 'office-profile')).AbsoluteUri
& 'C:/Program Files/LibreOffice/program/soffice.com' "-env:UserInstallation=$profile" --headless --convert-to pdf --outdir $reference (Join-Path $poc 'public/samples/policy.docx') (Join-Path $poc 'public/samples/form.docx')
```

上下文及后续门禁见 [DOC-VIEWER 实施计划](../../docs/superpowers/plans/2026-11-25-doc-viewer.md)。
