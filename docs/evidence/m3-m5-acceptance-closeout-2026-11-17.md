# M3 Critical Edge 与 M5-ACCEPT 收官验证（2026-11-17）

## 范围

本轮收官两项：M3 的 Critical Edge 业务账本与完整 run 来源封存；M5-ACCEPT 的真实时钟等待与每库生成质量回归。全程使用真实生产环境（Core 18880 / Host 8090 / 生产扩展库），不建隔离环境；唯一的合成数据是一条可撤销的低风险反馈，已清理。

## M3：Critical Edge 账本与完整来源封存

### 生产 Critical Edge 账本

| 项 | 实测值 |
|---|---|
| `governance_critical` 关键边（同库） | `interprets` 2 条、`supersedes` 10 条 |
| 跨库关键边 | 0 条 |
| `ext_document_version_family` | 54 行 |
| `ext_document_version` | 55 行 |
| 外键检查 | 0 违规 |

关键边由业务事实的同事务写入产生（`extensions/business/{family_replacement,related,revision}.go`、`extensions/lineage/supplements.go`）；账本一致，说明 Critical Edge 与治理事实同步提交、无孤儿边。

### 完整 run 来源封存

生产库存在一条真实原生 agent 运行产生的封存记录：

| 项 | 值 |
|---|---|
| run | `d25ddecb601934e2…` |
| 状态 / revision | `sealed` / 246 |
| 来源 | 2 个真实文档版本（`9e63c31f…`、`aecd03d7…`） |
| 步骤 | 3 个 model success、2 个 tool success |
| 已登记输入 | 117 条 |
| 末条审计 | `sealed` @ 2026-11-16T07:47:12Z |

这证明 run 级来源封存链路在生产真实可用。此前 T4-01/02 已补全封存的逐表 SQL 故障矩阵、容量与跨进程 CAS（见 `t4-01-02-failure-matrix-2026-11-16.md`）。

### 边界

**完整来源封存的"副本/文件"延伸仍不可用**：真实文件副本与产物读门卫依赖沙箱 producer，而用户已在单机安全评估后决定不接入沙箱（见 `T4-04`／`M2-COPY-LINEAGE` 的 Blocked by Security/Infrastructure）。本轮 M3 验收范围是 **run 来源封存 + Critical Edge 账本**，不包括文件副本封存与生成中撤销（T5）。

## M5-ACCEPT：真实时钟等待

与此前"把 `ready_at` 改到过去"的合成探测不同，本轮使用**真实时钟向前推进**：

1. 通过 `business_feedback_create` 创建一条低风险反馈（`m5-realwait-*`），初始 `approved`、`ready_at=None`。
2. 将其 `ready_at` 设为**当前时间的 20 秒之后**（真实未来时刻，不回溯历史时间）。
3. 不做任何时钟伪造，真实等待 Host ticker 到达该时刻。
4. ticker 在真实时间到达后将其自动激活：`approved` → `active`、revision 1→2。
5. 用 `business_feedback_withdraw` 撤销（注意 `id` 必须放在 `business` 包装内），结果 `withdrawn`、revision 3；对应 `ext_feedback_guidances` 同步为 `withdrawn`。

清理核验：无残留 active 合成反馈（`SELECT count(*) WHERE topic LIKE 'm5-%' AND status='active'` = 0），无残留 active 合成指导。

边界：真实等待验证了 20 秒级自动激活与撤销闭环，**不是**冻结配置要求的 24/48 小时完整冷静期。24/48 小时是业务策略常量，代码路径与 20 秒完全相同（同一 `datetime(f.ready_at)<=datetime(now)` 判定）；完整时长等待需要跨日运行，本轮不冒充已完成。

补充说明：这条真实等待反馈**未命中 20% 确定性抽样**（`ext_feedback_sample_reviews` 无记录），因此本轮不重复声称抽样复核通过。抽样复核由 2026-11-16 的合成探测（命中 20% 并完成 `passed` 复核）单独证明；本轮只证明真实时钟下的自动激活与撤销闭环。

用 `extensions/cmd/retrieval-eval` 对同一生产 KB 重跑两组查询（每组 35 题），全部来自真实文档：

| 查询集 | 题数 | Recall@5 | Recall@10 | Recall@20 | MRR |
|---|---|---|---|---|---|
| 标题组 | 35 | 0.9714 | 0.9714 | 0.9714 | 0.8795 |
| 描述组 | 35 | 1.0000 | 1.0000 | 1.0000 | 0.8976 |

与 2026-11-16 首次基线**逐项一致**（描述组 MRR 0.8976 完全相同），说明在经历了消息中心、检索修复、静默关闭沙箱、轮次上限调整等一系列变更后，检索质量未发生回归。

判据 Recall@5 ≥ 0.85 且 MRR ≥ 0.80：两组均达标。

## 现场还原

- 合成反馈已撤销并清理；无生产数据残留。
- 未改阈值与 `config.yaml`；未引入新依赖。
- 本轮只做验证，未改生产代码。

## 结论

- **M3**：Critical Edge 账本一致、run 来源封存生产可用 → 该范围内完成；文件副本封存仍 Blocked（沙箱决定），生成中撤销属 T5 未授权。
- **M5-ACCEPT**：真实时钟自动激活与撤销闭环通过、每库检索质量无回归 → 该范围内完成；抽样复核另由 2026-11-16 合成探测证明；24/48 小时完整冷静期跨日等待与专用管理页面仍未执行。
