# P2-E-01 只读离线基线（2026-11-08）

## 1. 范围

本轮只实现 P2-E-01：从现有 Core/Host 日志和扩展 Outbox 数据库生成一次性、可复现的离线基线。

- 基准 HEAD：`5dd40b9`（P2-D 已提交）
- 新增脚本：`extensions/scripts/p2e_offline_baseline.py`
- 新增测试：`extensions/scripts/p2e_offline_baseline_test.py`
- 未修改 Core、Host、SQLite、前端源码，未启动/停止生产服务，未实施 P2-E-02 埋点。
- 输出只含聚合值；不输出请求/响应正文、tenant_id、knowledge_base_id、session_id、URL query 或数据库凭据。

## 2. 输入与命令

当前最新 run：`data/launcher-run-MZLn17`，`core.log` 55,075,855 bytes、22,562 行；`host.log` 210 bytes。

```bash
wsl -d Ubuntu -- python3 /mnt/d/LanRAG-Next/extensions/scripts/p2e_offline_baseline.py \
  --run-dir /mnt/d/LanRAG-Next/data/launcher-run-MZLn17 \
  --ext-db /home/ybochuan/.local/share/agentrag/agentrag_ext.db \
  --out /tmp/p2e-baseline-MZLn17.json
```

脚本也支持 `--latest N`、`--all-runs`、`--log-root`、`--out`；默认从 `data/launcher-run-*/core.log` 发现最新 run。扩展库通过 SQLite `mode=ro` 只读打开，`ext_projection_outbox` 不存在时输出 `table_missing`，不抛出异常。

## 3. Core 请求基线

| 指标 | 值 |
|---|---|
| 可解析请求行 | 7,423 |
| 解析错误 | 0 |
| 时间范围 | 2026-11-08 14:04:07.025–17:31:56.522 |
| 方法 | GET 7,421 / POST 2 |
| 状态码 | 200: 7,414 / 304: 7 / 401: 1 / 404: 1 |
| 状态类 | 2xx: 7,414 / 3xx: 7 / 4xx: 2 |
| P50 | 7.109756 ms |
| P95 | 13.269646 ms |
| P99 | 35.049696 ms |
| max | 1,165.548187 ms |
| mean | 6.773789 ms |
| >200 ms | 1 |
| >1 s | 1 |

主要路由（已归一化 UUID/数字 ID）：

| 路由 | 请求数 |
|---|---:|
| `/api/v1/knowledge-bases/:id/knowledge` | 4,760 |
| `/health` | 2,392 |
| `/api/v1/knowledge-bases/:id/lifecycle` | 15 |
| `/api/v1/notifications` | 14 |
| `/api/v1/knowledge-bases/:id/lifecycle/business/relation_list` | 12 |
| `/api/v1/me/browser` | 12 |
| `/api/v1/sessions` | 12 |
| `/api/v1/sessions/:id/messages/:id/suggestions` | 12 |

> 该 run 的 401/404 是日志中的真实请求结果，不代表当前系统整体错误率；本次只是单 run 样本。

## 4. Host 事件基线

### 最新 run `MZLn17`

`host.log` 只有启动/恢复事件：

- `host_workers_started`：1
- `reconciliation_waiting_for_core`：1，`code=upstream_unreachable`
- `reconciliation_recovered`：1
- 解析错误：0

### 历史 run `6Wjyh0`（同一 Host 日志解析器的手工对照）

`data/launcher-run-6Wjyh0/host.log` 共 126 行，解析错误 0：

- `projection_failed`：110
- `bridge_before_retrieve_received`：10
- `parser_attachments_imported`：2
- `host_workers_started`、`reconciliation_waiting_for_core`、`reconciliation_recovered`、`host_stopped` 各 1

该对照说明 Host 事件计数可以覆盖真实失败事件；`MZLn17` 本身没有新的 `projection_failed`，不能据此推断 Outbox 已恢复。

## 5. Outbox 基线

只读汇总 `ext_projection_outbox`（不输出 tenant/KB scope）：

| 指标 | 值 |
|---|---|
| 总行数 | 174 |
| 状态分布 | dead: 174 / pending: 0 / processing: 0 |
| max retry_count | 5 |
| created_at 范围 | 1793456168921–1794114457695（Unix ms） |
| oldest pending age | null（没有 pending/processing 行） |

结论：当前积压全部是 dead-letter，不是“等待重试”的 pending 行；`oldest_pending_age_seconds=null` 不能解释为 Outbox 健康。该 174 条 dead-letter 继续作为独立可靠性待办，不混入 P2-E 指标实施。

## 6. 解析与安全边界

- Core 日志行格式为 `LEVEL [timestamp] [request_id client_ip=... latency=... method=... path=... response_body=... size=... status_code=...]`；`response_body` 可能被截断，脚本只从尾部 `size=... status_code=...]` 提取数值字段，不读取正文。
- `latency` 是 Go `time.Duration.String()`（如 `940.186µs`），脚本统一换算为毫秒；Windows 终端直接打印 `µ` 可能触发 GBK 编码错误，脚本输出使用 JSON/UTF-8，不依赖终端编码。
- route 先去掉 query，再将 UUID 和纯数字 ID 归一为 `:id`；原始路径不会写入输出。
- Host 日志解析只保留事件名和 `code`，忽略其余字段。
- 这是一次性离线样本，不是持续采集，也不能替代 P2-B 的压测结果；不会把日志文本作为指标标签。

## 7. 测试与验证

```bash
python -m unittest discover -s extensions/scripts -p 'p2e_offline_baseline_test.py' -v
```

结果：6 tests，6 pass。覆盖 Core 请求解析/归一化/延迟聚合、Host 事件与 reconciliation code、Outbox 状态与 pending age、缺失数据库、run 发现与 JSON 序列化。

真实运行结果：`outbox.available=true`，`total=174`，`parse_errors=0`；本次输出写入本地临时文件 `/tmp/p2e-baseline-MZLn17.json`，可重复生成，不纳入提交。

## 8. 下一步

- P2-E-02 最小埋点尚未实施，需要用户单独批准；本轮不引入 Prometheus/Grafana、长期存储或告警。
- P2-E-01 只证明现有日志可以生成一次性基线；下一步若要判断趋势，需先确定固定时间窗口和可重复调度，而不是继续人工抽样。
