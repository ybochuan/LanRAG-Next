# M2 补充通知联动：T3 接线清单（用户已批准）

前置：用户已批准并交付M3 T1/T2的快照/观察版本基础，并在本轮明确批准T3管理薄接口、扩展与界面联动。本文不修改Frozen，T4完整run封存与T5生成中撤销仍独立。

目标：用户在现有生命周期界面为独立补充文档显式选择同库父文档；父失效/过期或引用版本不再当前时，立即拒绝子文档检索，并由周期任务将pending/scheduled/active子文档转stale。复核到期仅提醒，不迁移；不自动复活失效文档，不把补充文档并入父family。

## 原生薄补丁（仅管理接口）

| 路径 | 修改 |
|---|---|
| `internal/handler/extension_lifecycle.go` | 新增supplements动作解析；沿用JWT和KB读写权限，写入前逐个核对child与所有parent的原生归属/权限；不信任浏览器tenant/CanWrite/actor |
| `internal/extensionbridge/management.go` | 公开DTO增加父文档ID与CAS版本，继续固定回环/服务端凭据/严格响应绑定；无本地治理算法 |
| `internal/router/routes_knowledge.go` | 增加该文档supplements的GET/PUT，沿用管理路由权限 |

已批准并应用的路由骨架：

```diff
@@ 生命周期管理路由
+ GET  /knowledge-bases/:id/lifecycle/documents/:document_id/supplements
+ PUT  /knowledge-bases/:id/lifecycle/documents/:document_id/supplements
@@ 原生handler
+ // Check child and each parent through existing native services/authorization.
+ // Derive actor and can_write server-side, then forward the bounded DTO.
```

不修改原生Agent、检索排序、解析、分块、DB表及任务调度。已有桥接超时/失败关闭和管理503/409语义保留。

## 扩展与界面

- 新`extensions/lineage/supplements.go`及下一顺序迁移：显式关系声明/current revision、治理事实、audit与critical edge同事务。临时库故障/进程退出验证，保留原始版本与边；撤销关系也留审计。`derived_from`仍异步，不混作授权。
- 默认同tenant/KB、最多8个显式父文档；禁止自身、重复、跨库和依赖环。声明时使用真实当前源版本，父须active且日期有效，子不得已失效；current事件、parent版本和CAS在同一决策事务重查。没有完整证据拒绝声明。
- Host安装真实版本Reader及管理动作，`extensions/management/`处理声明；`extensions/retrieval/`校验当前父状态/日期/版本；lifecycle每日任务和配置后重评估使用相同持久事实。读取异常拒绝，不回退M0透传。
- `frontend/src/api/lifecycle.ts`和`frontend/src/views/lifecycle/`添加同库父文档选择、有效依赖及失效原因。保留日期/标签，不提供任意状态修改或复活入口。浏览器不持有Host凭据。
- Review依赖提醒只读父日期并保留来源说明，不更改父/子原始标注；如需外部通知渠道仍单独配置，不向外部发消息。

## 必须验证

1. 原生JWT、只读角色、父/子任一越权、伪造actor/tenant及错scope拒绝。
2. 16并发声明/CAS只有一个成功；精确重试幂等、冲突payload拒绝；边/治理/audit任一步SQL失败与进程退出均无半提交。
3. 父active时子按自身日期检索；父stale/archived/superseded/failed、过期、root变化、缺记录或Core/Host不可用时子排除。补充再补充、多父任一失败、恶意环分别测。
4. 真日期扫描/依赖决策迁移子stale，事件/当前/Outbox同事务；不复活已有失效态。父review_due仅提醒，不能据此把子改stale。
5. 真实隔离上传两文档→界面/API声明→父失效→子RAG/Agent门禁；日期与标签保留，旧revision409。完整扩展/race、原生定向、前端test/type/build与候选编译。

本清单已获确认并实施，证据见`../evidence/m2-supplements-2026-11-01.md`；新发现需要扩大原生改动或修改业务矩阵时先报告。T3通过仍不能替代真实副本封存和持续流撤销验收。
