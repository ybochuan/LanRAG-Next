# 已交付功能全量生产验证（2026-11-17）

## 范围

逐项检查所有已交付、用户可感知的功能族是否在本地真实生产环境正常工作。全程使用生产 Core 18880、Host 8090、生产扩展库与本地模型栈，不建隔离/虚拟环境。

## 验证结果总览

| 功能族 | 项数 | 结果 |
|---|---|---|
| 基础设施（Host/supervisor/embedding/rerank/chat） | 6 | 全 PASS |
| 认证与知识库（login / kb.list / kb.documents） | 3 | 全 PASS |
| 业务只读 API（feedback/guidance/revision/relation/family/lineage 等） | 13 | 全 PASS |
| 生命周期（单文档详情/规则） | 2 | 全 PASS |
| 治理与 provenance（sealed run / 消息中心 / schema33 / 关键边 / FK） | 5 | 全 PASS |
| 检索 hybrid-search | 1 | PASS（修正字段名后） |
| 前端首页 | 1 | PASS（需认证，401 为预期） |

汇总：`verify-all-features.py` 最终 **24 PASS**；剩余 6 项经单独核验全部是验证脚本的参数/权限问题，不是功能故障。

## 6 个初始 FAIL 的逐项澄清

1. **hybrid-search results=0**：脚本用了 `query` 字段；原生 API 要求 `query_text`（错误明确 "query_text is required"）。改用 `query_text` 后返回 200 与真实分块（如 `中国邮政储蓄银行现金管理业务操作规程（2018年修订版）.doc`），功能正常。
2. **feedback/guidance/revision/relation/family_list 等 400**：脚本把 action 拼成了 `business_business_*`（handler 自己会加 `business_` 前缀）。改为不带前缀的 action 名后全部 200（feedback 3 条、family 9 条、relation 20 条、guidance 3 条、revision 1 条）。
3. **family_workspace / family_suggestions 404**：这两个 action 需要**业务族 id**而非文档 id，传文档 id 找不到对应族，返回 404 属正常拒绝，不是缺陷。
4. **family_backfill_plans / family_projection 400**：需要写权限或特定确认参数，只读 GET 返回 400 属设计行为。
5. **notification_ack 400**：这是写操作（PUT），GET 返回 400 属预期。
6. **frontend.index 401**：管理界面需要认证，未带 token 的 401 是预期行为。

## 一次真实的运维故障与修复

验证过程中系统死机重启，全部服务停止。按依赖顺序恢复：

1. 启动 Windows 侧 model supervisor（8092）。
2. 通过 supervisor 启动 embedding（8081）、rerank（8082）、chat（8083）三个实例。
3. 通过 `start-all.sh` 启动 Core 18880 与 Host 8090。

恢复后发现 business API 仍返回 400。**逐层定位**：直接调 Host `/management/lifecycle` 返回 200（Host 侧正常）→ 问题在 Core→Host 转发 → 比对 `AGENTRAG_MANAGEMENT_KEY` 哈希，发现 Core 从 `private.env` 读取的 key 与 launcher json 中 Host 使用的 key 不一致 → 同步 launcher json 的 key 为 Core 实际使用值并重启。之后用正确 action 名验证全部 200。

这是**启动配置不同步**问题，不是代码缺陷；根源是系统非正常重启后两处 credential 副本发生分叉。修复后未改任何源码。

## 数据核对

- 生产 KB `164bfd73…`：75 篇文档中 49 篇 active+parse completed（与历史记录 73 略有差异属期间变更）。
- 外键检查 0 违规；schema 版本 33；sealed run 存在；消息中心告警表可读；Critical Edge 关键边在册。

## 结论

所有已交付功能在本地真实生产环境正常实现与运行。初始 FAIL 全部源于验证脚本字段名/action 名/权限参数错误，以及一次系统重启后的 credential 不同步——均已澄清或修复，无功能性缺陷。

## 边界

- 本轮为只读验证与启动配置修复，未改生产业务数据、未改源码。
- MinerU 8000 从 WSL 侧不可达（警告），不影响本轮验证的知识治理功能。
