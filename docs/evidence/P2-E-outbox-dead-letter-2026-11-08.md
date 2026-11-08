# P2-E Outbox dead-letter 只读归因（2026-11-08）

## 1. 范围

本报告只读检查生产扩展库 `ext_projection_outbox` 的 dead-letter 现状，分析失败类型与回放前置条件。

- 只读 `sqlite3` URI：`mode=ro`
- 未修改、重试、删除或重放任何 Outbox 记录
- 不输出 tenant、KB、文档或请求正文；仅保留聚合数量
- 本报告不改变 P2-A A4-05“dead-letter 暂缓处理”的既有决定

## 2. 当前快照

| 指标 | 结果 |
|---|---:|
| 记录总数 | 174 |
| status | 全部 `dead` |
| pending / processing | 0 / 0 |
| projection_type | 全部 `tag_projection` |
| aggregate_type | 全部 `document` |
| retry_count | 全部 `5` |
| 涉及知识库数 | 1 |
| created_at 范围（UTC） | 2026-10-31T14:16:08.921 — 2026-11-08T05:07:37.695 |

由于不存在 pending/processing，`OldestPendingAge` 为 `null`；不能据此判断 Outbox 健康。

## 3. 失败分类

### 3.1 `canonical tag not configured in native KB`：102 条

- created_at：2026-10-31T14:16:08.921 — 2026-11-08T05:07:37.695 UTC
- next_retry_at：2026-10-31T14:16:38.925 — 2026-11-08T05:08:32.472 UTC
- 失败性质：配置/前置条件不满足，重试已耗尽；在 canonical tag 配置前重放大概率再次失败
- 处理方向：先确认该库的 canonical tag 与原生 tag mapping；配置完成后再生成受控回放清单

### 3.2 `upstream transport failed`：72 条

- created_at：2026-11-05T14:35:24.630 — 2026-11-05T15:54:38.926 UTC
- next_retry_at：2026-11-05T14:36:35.468 — 2026-11-05T15:59:00.601 UTC
- 失败性质：上游传输失败，时间集中在 2026-11-05 一个窗口；可能是 Core/Host 或网络短暂不可用
- 处理方向：先确认当前上游健康，再对这 72 条做受控回放；不要与 102 条配置类失败混在一起

## 4. 建议处理顺序

1. **先修配置类失败**：确认 canonical tag 已配置、原生 KB 可读，并核对 tag mapping；未完成前不要批量重放 102 条。
2. **再处理传输类失败**：确认 Core/Host/网络当前正常，按原 tenant/KB scope 生成 72 条回放清单。
3. **受控回放**：现有 `extensions/cmd/outbox-retry` 一次只接受一个 dead 记录 ID；应先生成清单并逐条确认，避免直接改数据库。
4. **补监控**：将 dead-letter 数量、按错误分类的数量和 oldest age 纳入 P2-E-03 趋势告警；不能只看 pending 数量。

## 5. 当前状态

- 本报告为只读分析，不产生生产写入
- Outbox 仍保留 174 条 dead 记录
- 建议将该事项作为独立可靠性待办继续跟踪，不纳入 P2-E-02 最小埋点提交
