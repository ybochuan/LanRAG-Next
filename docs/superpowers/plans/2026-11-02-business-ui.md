# M4/M5 业务工作台实施计划

> **For agentic workers:** Use superpowers:executing-plans。范围内直接实施，保留现有成果，不提交或推送。

**Goal:** 将已完成的M4/M5后台业务接到原生导航，用户可在界面声明/审核关联、审批反馈、创建/审核/发布修订。
**Architecture:** 三个路由复用一个Vue工作台和现有登录请求封装；后台只补缺失的relation_list，原生仅扩展现有管理转发的只读动作。业务规则仍由extensions执行，不改检索/生成算法。
**Tech Stack:** Vue3/TypeScript、已有CSS变量/Node测试、现有Gin转发与Go/SQLite；不加依赖。
**Spec:** Frozen Architecture第13节；TASK_BACKLOG的BIZ-UI；用户“继续快速推进任务”。源码5f77d7b / v0.9.0-3-g5f77d7b，开始时工作区干净。

## Tasks

- [x] 1. 补候选列表：`extensions/business/feedback.go`的管理列表、`management/business.go`及`internal/handler/extension_lifecycle.go`只读动作；测试scope/无写权限可读/候选持久列表和真实原生GET。
- [x] 2. 新增`frontend/src/api/business.ts`、`views/business/{model,model.test}.ts`。绑定显示revision和operation_id、权限/冷静期/状态动作，失败重试复用同一请求，测试scope过期响应。
- [x] 3. 新增`BusinessView.vue`、`CreateForm.vue`、`DocumentPicker.vue`和`business.css`；关联/反馈与指导/修订三视图、KB切换、可审阅详情、审批确认、分页、只读、错误与发布恢复。
- [x] 4. `frontend/src/{router/index.ts,stores/menu.ts,components/menu.vue,i18n/locales/*.ts}`挂导航和三路由；不改变其他功能。
- [x] 5. 前端全量测试/类型/构建、扩展受影响测试与必要门禁、原生定向/构建/联合API验证及浏览器真实渲染交互；更新文档/backlog，记录未验收项。

## Review focus

候选刷新后仍可审核；Viewer无写入口；409先刷新，网络未知复用原operation_id；切库/租户/详情后的旧响应不能覆盖新视图；高风险激活二次确认与发布影响可见；真实构建/测试结果不由页面夹具替代。

## Ledger

- Ruling: 现有API缺relation_list，补已授权管理facade的只读枚举，不用浏览器临时内存冒充持久候选。
- Ruling: 复用已授权业务规则与原生权限，不增加新后台契约、DB迁移或组件依赖；GraphRAG、MCP和guidance注入仍是独立后续任务。

## Results (2026-11-03)

- Tasks 1-5 complete; verification, real Core/Host API run and browser component interactions: `docs/evidence/m4-m5-business-ui-2026-11-03.md`. No production deployment or commit.
- Review: 2 Important findings; one fix pass, both RED then GREEN in actual setup regression tests; final frontend suite 2375 pass / 1 skip, type-check/build exit 0.
- Ruling: pending writes block route navigation until the operator resolves or explicitly ends retry; account/tenant changes still clear sensitive scope. The post-refresh continuation checks generation again.
- Existing frontend unchanged lines retain baseline EOL; only task lines change, avoiding a repository-wide formatting diff.
