# 轮次上限对齐与站内消息中心（生产环境，2026-11-16）

## 范围

本文件记录两项经用户于 2026-11-16 明确决策的整改：REM-ROUNDLIMIT（Agent 轮次上限改为 4）与 REM-M2-NOTIFY（站内信／本地待办，消息中心提醒）。验证使用当前真实生产环境（生产 Core 18880、生产 Host 8090、生产扩展库），未建立隔离环境或虚拟环境。

## 一、REM-ROUNDLIMIT：轮次上限对齐为 4

冻结架构要求 Agent 轮次上限 3–4；生产 `builtin-smart-reasoning` 原为 50。用户决定取 **4**。

### 生效位置的两层结构

内置 Agent 的轮次上限有两处来源，二者优先级不同：

- `config/builtin_agents.yaml` 的 `config.max_iterations`（内置注册表默认值）。
- `custom_agents` 表中的同名行，`GetAgentByID` 与 `ListAgents` 对内置 Agent **优先使用数据库行**（注释写明「for customized config」）。

因此只改 YAML 不会生效：本轮先改 YAML 为 4，重启后接口仍返回 50，确认数据库行优先；随后经原生 Agent 更新接口把该行的 `max_iterations` 改为 4（其余配置原样回传），接口读回为 4。两处现均为 4，避免后续重启时再被旧值覆盖。

### 验证

```text
GET /api/v1/agents → builtin-smart-reasoning.config.max_iterations = 4
```

未改 `internal/` 源码，未改 Agent 调度算法；改动是配置值。

## 二、REM-M2-NOTIFY：站内消息中心

用户选择「站内信 / 本地待办系统（消息中心提醒即可）」，即站内投递，不接邮件或外部系统。

### 现状核对

复核告警链路早已接线：worker 启动与每 24h 扫描过期 `review_due`，写持久收件箱 `ext_review_alerts` 与同事务 Outbox，`notify` 为事务内本地信号。工作台「待复核」页签已显示投递状态。缺的是：跨库统一入口，以及「人是否读过」的持久状态——`delivered_at` 只代表本地回调接受了提醒，不代表有人看过。

### 改动

**扩展库 schema 33**（`033_review_acknowledgements.sql`）：`ext_review_alerts` 增加 `acknowledged_at`，并建未读部分索引。与投递时间是两个不同事实，刻意分开。

**review 服务**（`extensions/review/notification.go`）：新增 `Acknowledge`，幂等——重复确认不会改写首次阅读时间；未知告警返回 `ErrAlertNotFound`。`ListTx` 增加 `acknowledged_at` 与派生的 `unread`。为保持运维 CLI 对旧 schema（5–18）的只读能力，`acknowledged_at` 仅在列存在时查询，旧库退化为「读状态未知」而不是整表失败。

**管理接口**（`extensions/management/notification.go`）：新增 `business_notification_ack`（PUT）。命名复用原生 `business_` 前缀，因此**不需要改原生路由或处理器**；该动作不在只读白名单内，原生 facade 会按写操作校验 `can_write`。

**前端**：新增 `MessageCenter.vue` 与工作台「消息中心」顶层页签。消息中心对当前用户可读的每个知识库调用既有的 `/lifecycle/reviews`，在客户端合并并按到期时间排序；单个库读取失败只报该库，不清空整页。未读项带圆点与「标记已读」，已读项显示阅读时间。页面文案明确：「标记已读只影响本页提醒状态，不改变文档生命周期，也不代表已经完成复核」。

### 生产验证

1. 扩展库迁移：`ext_schema_migrations` = **33**，`ext_review_alerts` 存在 `acknowledged_at`。迁移前用 SQLite backup API 备份两库（扩展库当时 schema 32，完整性与外键检查通过），备份位于 `~/agentrag-remediation-backup-20261116-204023`。
2. 真实告警：对生产文档 `7f9a23fb-2cd6-4940-a3a6-d5ffa5b7ed30` 设置已过期 `review_due`，重启触发启动扫描，生成告警 `b366d67a…`，`delivery_state=done`、`delivered_at` 已填。
3. 读取：`GET /lifecycle/reviews` 返回 `unread: true`、无 `acknowledged_at`。
4. 标记已读：`PUT /lifecycle/business/notification_ack` 返回 `acknowledged: true`；再次读取为 `unread: false`，`acknowledged_at` 已写入。
5. 幂等：再次确认同一告警仍返回成功，且未改写首次阅读时间。
6. 还原：通过原生接口清空该文档的 `review_due`，`effective_from` 与 labels 保持原值（revision 因真实写入递增，属正常审计痕迹）。
7. 前端产物：`web/assets/MessageCenter-1lGcHKOw.js` 含「标记已读」「unread-dot」「不代表已经完成复核」；`LifecycleView-DG7GDKVu.js` 含 `notification_ack` 与「消息中心」。

### 验证命令与结果

```text
gofmt -l extensions/                        → 无输出
CGO_ENABLED=0 go build -mod=readonly ./...  → 退出 0
CGO_ENABLED=0 go vet -mod=readonly ./...    → 退出 0
CGO_ENABLED=0 go test -mod=readonly ./... -count=1 → 退出 0（含新增 review 确认 4 例）
python3 scripts/check_module_boundary.py    → module boundary passed: 302 imports
npm run type-check                          → 退出 0
npx tsx --test src/views/lifecycle/*.test.ts → 16/16 通过
npm run build                               → 退出 0
```

期间发现并修正一处兼容性问题：最初的查询直接选择 `acknowledged_at`，导致 `cmd/review-alerts` 对旧 schema（10–18）的测试全部失败；改为按列存在性自适应后全部通过。

## 三、Agentic 评估重跑的时间与用量（供决策）

上一轮 DeepSeek 侧 Agentic 评估（15 题、`max_iterations` 当时为 50）在生产日志中留下的真实用量：

| 项 | 实测值 |
|---|---|
| LLM 调用次数 | 146 次 `agent_round` |
| prompt tokens | 4,353,179 |
| completion tokens | 33,530 |
| 合计 tokens | 4,386,709 |
| 15 题累计耗时 | 约 473 秒（约 8 分钟） |

轮次上限改为 4 后，单题最多 4 轮（原轮次约 10 轮），因此重跑预计：**约 60–75 次 LLM 调用、150–220 万 tokens、4–6 分钟**。

费用取决于该账户的 DeepSeek 计价档（本机模型目录未配置单价，无法给出精确金额）。按 DeepSeek 公开列表价量级估算，输入约百万元级 tokens、输出仅数万 tokens，单次重跑大致在**数元人民币**量级；缓存命中会进一步降低输入成本。人工评分是另一项成本：15 题逐题判分，按每题 3–5 分钟计约 45–75 分钟人工时间。

## 边界

- 消息中心是站内提醒，不接邮件、短信或外部待办系统；用户已明确「消息中心提醒即可」。
- 「已读」仅代表界面确认，不代表完成复核，也不改变文档生命周期。
- 标记已读走写权限（原生 facade 的 `can_write`）：只读账号可查看消息但不能标记已读，这是一处已知限制。
- 本轮未改原生路由与处理器；`business_notification_ack` 复用既有 `business_` 转发路径。
- 未改原生 RRF、检索、解析或调度算法；未改根 `go.mod`/`go.sum`。
