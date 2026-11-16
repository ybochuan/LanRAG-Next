# M4 家族关系校准（生产元数据，2026-11-16）

## 范围

本批使用生产知识库 `164bfd73-d231-4d5c-994e-057ac0cdf48a` 的文档元数据、标题、描述和分块哈希，构造 33 组同库家族关系对，运行 `extensions/cmd/family-calibrate`。清单文件为 `tmp/m4-cal-production.json`，报告文件为 `tmp/m4-cal-report.json`。

清单不包含文档正文。文档标识和标题来自生产扩展库 `~/.local/share/agentrag/agentrag_ext.db` 的 `ext_source_snapshot_evidence`；人工标签依据现有生产家族成员、发布头、血缘边（`Supersedes`/`Interprets`）和业务语义构造。没有修改生产数据。

## 清单与分组

- 33 个唯一 pair，train 15、holdout 18。
- train：`train-cash` 5、`train-other` 5、`train-invoice` 3、`train-settlement` 2。
- holdout：`holdout-unit` 4、`holdout-door` 4、`holdout-confirmation` 4、`holdout-trade` 3、`holdout-ebank` 3。
- expected 分布：supersedes 14、none 7、related_to 5、membership 4、interprets 2、amends 1。
- 文档 ID 和业务主题按 train/holdout 分组隔离，未跨分区复用文档。

清单 SHA256：

```text
5e2709e499d03e8f406d2c6eeaf9ee7d0e37d77896a3e2501a3abd39fbc57a40
```

## 命令与结果

```bash
cd extensions
go run ./cmd/family-calibrate ../tmp/m4-cal-production.json
```

退出状态 0。报告 SHA256：

```text
bb3668226a03b9422354a333286d358a682d30c59deacbd37f14bfce9f98e2aa
```

选中策略：

```text
similarity_weight = 0.60
declaration_weight = 0.25
structure_weight = 0.15
threshold = 0.60
```

指标：

| 分区 | samples | TP | FP | FN | precision | recall |
|---|---:|---:|---:|---:|---:|---:|
| train | 15 | 10 | 0 | 0 | 1.000 | 1.000 |
| holdout | 18 | 10 | 0 | 6 | 1.000 | 0.625 |

`accepted: true`（要求 precision≥0.70 且 recall≥0.60）。

逐类原始预测（阈值过滤前）：

- amends：1/1 正确；
- interprets：2 个均预测为 related_to；
- membership：4/4 正确；
- none：7 个中 1 个正确、6 个预测为 related_to；
- related_to：5/5 正确；
- supersedes：14/14 正确。

## 解释与边界

- 本次运行的本地 embedding 环境未配置，校准器仅使用标题和结构信号；报告中不包含语义模型结果。
- holdout 的 6 个 false negative 为 4 个 related_to 与 2 个 interprets；`none → related_to` 的原始误报被 0.60 阈值过滤。
- 这些标签由生产元数据和现有血缘关系辅助构造，不是独立双人人工标注；不能把本报告表述为已完成外部业务标注验收。
- 本次只验证 M4 家族关系校准；Agentic 15–20 多跳问题集未在本报告中执行。Agentic 15–20 多跳评估未执行。阻塞是云端生成模型账户与网络，不是凭据或实现：Core JWT 可由原生登录 `POST /api/v1/auth/login` 取得（顶层 `token` 字段），用它请求 `/api/v1/models`、`/api/v1/agents`、`/api/v1/sessions` 均为 200；此前记录的 403 来自 API Key 作用域（API Key 不能调用管理接口），不是评估阻塞。真实阻塞是 `builtin-smart-reasoning` 绑定的 `deepseek-flash` 返回 402 Insufficient Balance（另有 `api.deepseek.com` DNS 超时）。评估工具与多跳问题集已就绪，见[`docs/evidence/m4-agentic-eval-2026-11-16.md`](docs/evidence/m4-agentic-eval-2026-11-16.md)；解除条件是云端账户恢复可用后重跑并补人工评分。在此之前不得把 M4 Agentic 评估标为完成。 GraphRAG 的初步开启门禁判断见[`m4-graphrag-budget-gate-2026-11-16.md`](m4-graphrag-budget-gate-2026-11-16.md)：当前 `related_to` 召回率不低于 60%，GraphRAG 保持关闭，未产生索引预算。
- 生产库、生产服务与用户数据未被修改；清单和报告位于忽略目录 `tmp/`，保留作复现证据。

## 复现材料

- `tmp/m4-cal-production.json`（33 对清单，SHA256 见上）
- `tmp/m4-cal-report.json`（校准器输出，SHA256 见上）
- `tmp/m4-cal-breakdown.log`（逐类预测调试输出）
