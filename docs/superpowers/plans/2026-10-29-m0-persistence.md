# M0 Persistence Implementation Plan

> 用户已授权按TASK_BACKLOG继续实施。按executing-plans直接执行，先写测试再实现；进度及证据写回TASK_BACKLOG，不提交或推送，不改原生源码/数据库。

**Goal:** 完成M0-03B/03C/03D的独立SQLite、单写者、生命周期原子事务和可恢复Outbox。

**Spec:** Frozen Architecture v1.0第4章、不变量A/E；用户C-01明确只接受来源队列弱点，不降低扩展侧已接收事件的事务要求。C-02/C-03/C-04决定保持。

**Architecture:** 独立文件agentrag_ext.db，现代纯Go SQLite驱动modernc.org/sqlite v1.61.0（要求Go1.26）。一个dedicated writer connection，经可取消的串行入口执行BEGIN IMMEDIATE；reader连接mode=ro，连接级PRAGMA在每次物理连接创建时设置。生命周期event/current/outbox在一个事务中提交，FK约束到已提交事件。Outbox用带token的租约避免旧worker确认被重新认领的任务，失败重试可进入dead并人工重试。

## Constraints / review focus

- 只更新extensions/go.mod/go.sum和扩展新包，根依赖与internal保持原样。
- 只测试临时目录数据库；打开已有含非ext表的数据库时必须在修改PRAGMA/建表前拒绝，不触碰上游数据库。
- 不把parse_status映射成治理active，不提前实现M2迁移策略，不更改原生SQLite并发。
- 生命周期重试幂等；同ID或来源键不同payload可观察地拒绝；from_state必须匹配当前事实。
- reader物理连接均需配置FK/busy timeout等，且实际不能写；不只在启动时对pool执行一次PRAGMA。
- 错误/取消/回调panic必须回滚并释放writer入口；只有COMMIT成功才返回成功。
- Outbox重启恢复pending/expired processing，旧token不能complete/fail新lease；外部处理按at-least-once，依赖幂等投影。
- 子进程崩溃只针对临时DB；真实上游混合ingest和远端CI仍单列未验。

## Task 1: M0-03B SQLite / WriteExecutor

**Files:** extensions/store/sqlite.go、write_executor.go、migrate.go、sqlite_test.go、write_executor_test.go；extensions/go.mod/go.sum。

**Interfaces:** Open(ctx,path) (*DB,error)，DB.Write(ctx,func(*sql.Tx) error)，DB.View(ctx,func(*sql.Tx) error)，DB.Close() error。

- [x] 写PRAGMA/reader只读/原数据库拒绝测试、1000并发真实INSERT、callback错误/取消/panic回滚测试，运行RED。
- [x] 固定驱动版本；实现连接与串行BEGIN IMMEDIATE、回滚、关闭和迁移入口。
- [x] 完整Go suite GREEN；精确计数1000、无未处理SQLITE_BUSY、所有物理reader连接设置正确。

## Task 2: M0-03C Event/current/outbox transaction

**Files:** extensions/store/migrations/001_lifecycle.sql；extensions/lifecycle/store.go、store_test.go。

**Interfaces:** lifecycle.NewStore(db)，Append(ctx,Event) (inserted bool,error)，Current/Events读取方法。Current含State/LastEventID；事件按提交序列有序读取，可重建current。

- [x] 写重复键/冲突/from_state/FK/重放测试；用真实SQLite触发器在event/current/outbox各步制造失败，运行RED。
- [x] 实现event/current/outbox原子提交；M2业务状态枚举与审批策略不在本任务。
- [x] 全有或全无、current与event重建一致；跨scope引用和不存在事件引用被拒绝，未提交事务进程退出后无部分数据。

## Task 3: M0-03D Outbox store / worker / recovery

**Files:** extensions/outbox/store.go、worker.go、store_test.go、worker_test.go；extensions/integration/outbox_recovery_test.go。

**Interfaces:** Store.Claim/Complete/Fail/RetryDead/ListDead/Stats；Worker.ProcessOne/Run，通过Handler接口执行投影，配置lease/retry/max attempts/poll/error reporting。

- [x] 写租约/旧token/重试/dead/人工retry/健康积压测试及临时子进程崩溃恢复测试，运行RED。
- [x] 实现事务内claim、fenced completion和失败退避、dead可观察、worker取消退出。
- [x] 子进程COMMIT后直接退出仍能读到pending；claim后退出过期lease可重新认领；旧token不能确认；完整suite GREEN。

## Completion

- [x] Go build/test/vet/gofmt、Python边界门禁/回归、原件hash和工作树范围检查通过。
- [x] 一次独立只读复核；修正实质问题，更新TASK_BACKLOG/PROJECT_CONTEXT及Change Map实际验收证据。
- [x] 明确尚未完成：公共API/对账Observer/Host与原生Hook接线、真实上游2文档混合并发、MCP、远端Actions与本机race。

## 执行证据

- 完成日期2026-10-30；详细任务状态、命令及范围裁定见TASK_BACKLOG“持久化基础”记录。
- 独立复核1项Important已修正：Handler自身deadline后，Fail允许匹配token且尚未被回收的过期持有者记录失败；新lease的旧token仍拒绝，Complete仍要求未过期。新增`TestHandlerDeadlineCountsFailureAndReachesDead`先失败再通过。
- 1000并发写入是真实临时SQLite探针表；健康排空是小样本组件验证；未将它们当成M0上游混合并发或生产SLO。
