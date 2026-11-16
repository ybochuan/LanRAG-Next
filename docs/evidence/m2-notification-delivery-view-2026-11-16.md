# M2 复核提醒：通知投递状态在工作台的呈现（2026-11-16）

## 范围

本文件记录整改项目路线第 2 项（REM-M2-NOTIFY）中**工作台侧**的交付：让待复核面板显示通知队列的真实投递状态。验证使用当前真实生产环境（生产 Core 18880、生产 Host 8090、生产扩展库），未建立隔离环境或虚拟环境。

## 现状核对

M2 的复核告警链路早已接线：worker 启动与每 24h 扫描过期 review_due，创建持久收件箱记录与同事务 Outbox，notify 是事务内本地信号，输出 `document_review_overdue` 日志。README 明确约束「快速本地回调不能执行网络 I/O 或递归写 DB；外部消息渠道与 UI 后续接入」。

管理接口 `/lifecycle/reviews` 实际已返回 `delivery_state` 与 `delivered_at`；前端 `ReviewAlert` 类型也声明了 `delivery_state`。但工作台的待复核面板**完全不展示该字段**，每条提醒固定显示「待复核 →」——操作员无法区分已投递、投递中、待投递、投递失败与未入队。这就是 P2a 要修的缺口。

## 改动

- `frontend/src/api/lifecycle.ts`：`ReviewAlert` 补 `delivered_at?: string`。
- `frontend/src/views/lifecycle/model.ts`：新增 `reviewDeliveryLabel` 与 `reviewDeliveryClass`，把队列状态映射为中文标签与徽标色调。
- `frontend/src/views/lifecycle/LifecycleView.vue`：待复核项按 `delivery_state` 显示真实状态徽标；有 `delivered_at` 时显示本地投递接受时间；面板增加说明「已投递表示本地回调已接受该提醒，不代表人工已读」。

标签映射：done→已投递、processing→投递中、pending→待投递、dead→投递失败、missing→未入队，空值→未知且保留原值。已投递用品牌色、失败用错误色、其余用次要色。

## 生产环境验证

为取得真实告警，对生产知识库一篇真实文档（`7f9a23fb-2cd6-4940-a3a6-d5ffa5b7ed30`，annex-06.docx，当前 active）经原生授权管理接口设置一个已过期的 review_due，重启 Core/Host 触发启动扫描：

| 项 | 结果 |
|---|---|
| 生产扩展库 `ext_review_alerts` | 从 0 行变为 1 行，alert 状态 open，`delivered_at` 已填 |
| `ext_projection_outbox` | `review:...:1` 状态 `done`，retry_count 0 |
| `/lifecycle/reviews` 返回 | `delivery_state: "done"`、`delivered_at`、`title: "annex-06.docx"` 全部返回 |
| 部署的生产 chunk | `LifecycleView-Dc7wawmh.js` 含「已投递」「投递失败」「未入队」「不代表人工已读」「delivered_at」全部标记 |

验证后已通过原生接口还原该文档的原始业务值：`review_due` 清空、`effective_from` 与 labels 保持原值。revision 因真实写入递增到 3，属正常审计痕迹，未改变业务语义。

## 验证命令与结果

```text
npm run type-check                                                → 退出 0
npx tsx --test src/views/lifecycle/model.test.ts component.test.ts → 14/14 通过
npm run build                                                     → 退出 0
web/ 增量同步（旧目录备份 tmp/web-backup-20261116-175835，344 文件）
```

新增测试：2 个投递标签/色调单元测试 + 1 个工作台渲染契约测试（断言面板必须展示 delivery_state、不得保留固定「待复核 →」、必须声明不代表人工已读）。

## 边界

- 本项只完成**工作台呈现**，不是外部投递通道本身。外部消息渠道（邮件/站内信/待办系统）形态未定，仍待用户决策，不能声称 M2 外部通知已完成。
- `delivered_at` 只表示本地回调接受，不表示人工已读；该边界已在界面文案与证据中明确。
- 未改扩展库 schema、未改 Outbox 语义、未改 notify 的事务内约束。
- 生产数据的临时改动已还原；只保留系统自动产生的告警记录与审计痕迹。
