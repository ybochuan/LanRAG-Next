# 文档工作台统一 WeKnora UI 风格与全量列表（2026-11-17）

## 风格依据（读代码，不靠图片）

- frontend/package.json 依赖 tdesign-vue-next ^1.19.2 与 tdesign-icons-vue-next：WeKnora 原生 UI 即 TDesign 体系。
- 工作台 CSS 已全面使用 TDesign token：--td-brand-color、--td-text-color-primary、--td-text-color-secondary、--td-component-stroke、--td-bg-color-container、--app-radius-lg。本次沿用这些 token，不引入新配色。
- 图一（WeKnora 原生知识库页）文件名是主文本黑色，图二工作台列表文件名是品牌绿链接。本次把工作台拉回 TDesign 主文本色，仅 hover/focus 用品牌色。

## 改动

1. 文档名默认黑色：.document-link 显式设 color:var(--td-text-color-primary)，新增 :hover/:focus-visible 才用品牌色；不改共享规则以免影响其他按钮。
2. 列表不分页、全量显示 + 滚动条：loadOverview 先以 page_size=100 拉首页，再按 total 循环拉剩余页，累积到 allDocuments；表格渲染 sortedDocuments。移除分页 footer，改为说明文字。.table-wrap 加 max-height:60vh; overflow:auto，th 设 sticky 表头。
3. 清理死代码：删除 pageFromQuery/page/changePage 及相关 watch/URL 写入。

## 验证

- 极简契约测试钉住主文本色 + hover 品牌色、全量累积、表格渲染全部、无分页 footer、有界滚动区；路由测试改为不再断言 page。
- npm run type-check 退出 0；相关前端测试 30/30 PASS；npm run build 退出 0。
- 生产实测：GET /lifecycle?page=1&page_size=100 返回 total=87、documents=87，一次拉全；新 chunk LifecycleView-NSTkd3cz.js 部署后 HTTP 200。

## 边界

- 后端 page_size 上限 100，前端循环拉取；排序/筛选/URL 状态保留，只是文档列表不再分页。
