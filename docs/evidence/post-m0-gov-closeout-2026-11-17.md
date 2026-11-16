# POST-M0-GOV / M0-02D / M0-02E / STREAM-REVOKE 收官（2026-11-17）

## 总览

用户要求「三项一起完成」：POST-M0-GOV、M0-02D/02E、M2-STREAM-REVOKE。核实后实际情况是前两项代码早已接线、只缺验收证据；后两项属于策略决策而非代码缺口，用户决定不做。全部在真实 Core 18880 / Host 8090 上核验。

## POST-M0-GOV：代码已接线，验收通过

核实四个治理面，均已在代码中接线，不是缺口：

| 治理面 | 接线点 | 状态 |
|---|---|---|
| Agent 直接读 | `internal/agent/tools/read_document.go:163` → `requireGovernedDocument` | 已接线 |
| 独立图谱入口 | `internal/agent/tools/query_knowledge_graph.go:228` → `rejectUnattributed` + scopeEnforced | 已接线 |
| 搜索排除 | `internal/application/service/chat_pipeline/search_parallel.go:193` → `filterBridgeResults` | 已接线 |
| 中途停用 | 属 M2-STREAM-REVOKE / T5，用户决定不做（见下） | 不实现 |

名单/数据分析/schema/MCP 另有 `requireGovernedDocument` / `rejectUnattributed` 接线（`data_analysis.go:756`、`data_schema.go:83`、`list_documents.go:120`、`mcp_tool.go:148` 等）。

**生产 E2E 验收**：在隔离网络命名空间启动真实 patched Core + 真实 worker Host + 本地模型夹具，运行 `extensions/scripts/governance_e2e.py`，exit 0，20 项 PASS：active 正文进入真实提示词、stale 正文/描述/旧答案标记在全部模型与重排请求中零出现、Host 离线答案生成调用为 0、图谱/MCP 只返回授权摘录、provenance 封存且 FK 干净、未支持的组成保持 unattributed。

注：该脚本运行时会覆盖 `docs/evidence/m2-governance-leak-e2e-2026-11-01.log`，提交前已还原原文件。

## M0-02D：搜索排除，验收通过

`filterBridgeResults` 把 before_retrieve 返回的允许集与排除集应用到块/实体合并结果，显式排除优先，普通文档必须属于允许集。E2E 中 `AGENT-SEARCH-STALE`、`AGENT-SEARCH-OFFLINE`、`RAG-STALE`、`RAG-OFFLINE` 均 PASS，证明排除与断 Host fail-closed 生效。

## M0-02E：用户决定不做

检索后元数据增强 `extensions/retrieval/contextualize.go` 当前无实现。它要在 ToolResult 格式化前注入额外元数据，属原生 RAG 行为变更，且会影响来源核验；当前检索质量已达基线（标题组 R@5 0.9714 / MRR 0.8795，描述组 1.0 / 0.8976），收益不明确。用户 2026-11-17 决定不做，标记阶段外。

## M2-STREAM-REVOKE：用户决定不做

设计文档 `docs/design/M2-Stream-Revoke-2026-11-01.md` 明确状态为「源码调查及设计提案；无运行行为变化，无已验收的即时撤销 SLA」，并列出两个硬约束：依赖完整来源集（其副本链已因沙箱决定 Blocked），以及原生 ctx 取消会 flush 已缓冲文本、无法撤回已输出内容。用户 2026-11-17 决定保持现状（入口快照 + 最终拼接许可，不承诺生成中撤销）。Frozen 的 before_generate guidance fail-open 保持独立不变。

## 验证与现场

- 治理 E2E：exit 0，20 项 PASS（隔离网络命名空间，真实 Core/Host）。
- 历史日志副作用已还原，工作树只留本次证据与 backlog 改动。
- 未改生产代码、未改生产数据、未改阈值。

## 结论

POST-M0-GOV 与 M0-02D 实质完成（代码在册、生产 E2E 验收通过）；M0-02E 与 M2-STREAM-REVOKE 按用户决定不做，不标完成但明确关闭后续追踪。
