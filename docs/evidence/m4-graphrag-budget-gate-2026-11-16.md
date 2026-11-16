# M4 GraphRAG 预算门禁初步评估（2026-11-16）

## 范围

本文件记录冻结架构 9.3 的 GraphRAG 开启门禁在本次 M4 生产元数据校准集上的初步判断。GraphRAG 保持默认关闭；本文件不声称 GraphRAG 索引、查询或预算看板已经在生产环境启用。

## 门禁条件

冻结架构要求以下条件同时满足后才可开启某知识库的 GraphRAG：

1. 该库 `related_to` 召回率低于 60%；
2. 评估集中至少 30% 的问题属于“答案跨多文档”；
3. 用户接受该知识库当前估算预算（`graph_index_llm_calls` / `input_tokens` / `output_tokens` / `estimated_cost` / `index_duration`）。

监控回退条件：Recall 提升低于 3 个百分点，或 `estimated_cost` 超过预算 150% 时自动关闭该库。

## 当前输入

- 知识库：生产 KB `164bfd73-d231-4d5c-994e-057ac0cdf48a`。
- 校准清单：`tmp/m4-cal-production.json`，holdout 18 对；其中 `related_to` 标签 5 对。
- 校准报告：`tmp/m4-cal-report.json`（SHA256 `bb3668226a03b9422354a333286d358a682d30c59deacbd37f14bfce9f98e2aa`）。
- 逐类原始预测：`related_to` 5/5 正确，raw recall = 1.00（见 `tmp/m4-cal-breakdown.log`）。

## 结论

- **第一项不满足**：本次 holdout 的 `related_to` 原始召回率为 1.00，不低于 60%，没有触发“关联召回不足”的开启理由。
- **第二项未测量**：本次校准集是家族关系对，不是 15–20 道多跳问题集；没有“答案跨多文档”比例数据。
- **第三项未申请**：未建立 GraphRAG 索引，也未请求用户接受任何索引预算。因此 `graph_index_llm_calls=0`、`input_tokens=0`、`output_tokens=0`、`estimated_cost=0`、`index_duration=0`；当前没有预算超限风险。
- 决策：**GraphRAG 保持关闭，无需开启预算门禁**。后续只有在上述三项同时满足并提供真实预算估算后，才进入开启审批；开启后按 Recall 提升 <3 个百分点或成本超过预算 150% 的规则回退。

## 边界

本文件只覆盖 GraphRAG 的开启门禁判断，不证明 GraphRAG 索引实现、预算看板或回退机制已在生产运行。Agentic 15–20 多跳问题集仍未执行，不能以本文件替代 Agentic 评估。

## 复现材料

- `tmp/m4-cal-production.json`（33 对清单，SHA256 `5e2709e499d03e8f406d2c6eeaf9ee7d0e37d77896a3e2501a3abd39fbc57a40`）
- `tmp/m4-cal-report.json`（校准器输出）
- `tmp/m4-cal-breakdown.log`（逐类原始预测）
