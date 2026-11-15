# 文档工作台正式生产登录态浏览器 E2E（2026-11-14）

## 环境

- 代码版本：`23e7472`（分支 `codex/m1-parser-routing`）
- 生产服务：Core `http://127.0.0.1:18880`、Extension Host `http://127.0.0.1:8090`
- 服务状态：MinerU `:8000`、supervisor `:8092`、embedding `:8081`、rerank `:8082`、docreader `:50051`、Host `:8090`、Core `:18880` 均为 UP
- 前端：`frontend/dist` 已同步到 `web/`；`web/index.html` SHA256 `3a86d48eaad8f0780dd86de3d7f09385286c8bb650b8f776e9b9c826b9a930b8`
- 浏览器：Chrome headless + Playwright Core；Windows Node `v24.13`
- 生产知识库：`164bfd73-d231-4d5c-994e-057ac0cdf48a`（银行知识库，85 项文档）
- 登录态：使用临时 owner JWT（2 小时，写入 `auth_tokens`，验证后删除）；另创建临时 viewer 用户验证只读角色，验证后删除用户、成员关系和 token。

## 结果

| 检查项 | 结果 | 证据 |
| --- | --- | --- |
| 旧入口跳转 | PASS | `/platform/lifecycle?kb=...` 自动重定向到 `/platform/documents?kb=...` |
| 侧栏菜单收敛 | PASS | 侧栏存在“文档工作台”；“文档管理”旧入口不存在 |
| 台账搜索 | PASS | 标题搜索“深圳市业主大会和业主委员会备案管理办法.txt”由 85 项过滤为 1 项 |
| 详情抽屉与日期表单 | PASS | 文档详情显示生命周期、版本族、关联关系、关系图、治理记录、关联建议、修订发布 7 个入口；三个日期输入可编辑，存在“保存日期” |
| 单文档关系图 | PASS | 打开有版本族的《中国邮政储蓄银行发票管理办法（2024年修订版）.doc》，关系图显示 1 个版本节点（V1）；该文档当前无已登记关系边 |
| 治理记录 | PASS | 详情治理记录显示 `lifecycle_initialize`、`lifecycle_parse_completed` 事件及时间 |
| 反馈审核页 | PASS（页面可达） | “反馈审核”页加载正常，当前生产库反馈记录为 0；实际反馈审批/引用链路由隔离 E2E 的 `[DOCUMENT-FEEDBACK]` 覆盖 |
| 只读角色限制 | PASS | 临时 viewer 登录显示“只读”；文档详情三个日期输入均为 `disabled`，无“保存日期”按钮 |
| 浏览器控制台 | PASS | owner/viewer 测试均无 pageerror；无业务 5xx |

## 范围与限制

- 本次验证使用生产 Core/Host 与生产前端构建，但使用临时测试登录态；不包含用户密码登录流程，也不修改生产文档。
- 关系图仅验证版本节点渲染；未在生产库登记新的关系边，关联边核验使用隔离 E2E 结果。
- 生产库没有反馈记录，因此“审核员反馈/核验引用”仅验证页面可达；完整反馈提交、审核和引用由隔离 `governance_e2e.py` 的 `[DOCUMENT-FEEDBACK]` 覆盖。
- 临时 owner/viewer token、viewer 用户和成员关系均已在验证后清理。

## 相关证据

- 截图：`docs/evidence/document-workbench-production-e2e-2026-11-14.png`
- 隔离联合 E2E：`docs/evidence/document-workbench-browser-e2e-2026-11-14.log`
- 隔离接口 E2E：`docs/evidence/document-workbench-e2e-2026-11-13.log`
