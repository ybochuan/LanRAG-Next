# M0 Host Runtime Implementation Plan

> 用户授权继续上一批Host配置/启动接线；按executing-plans本人实施、TDD，最后一次独立只读复核。保留现有工作目录，不提交/推送，不修改原生源码或根依赖。

**Goal:** Host进程接通既有SQLite、周期对账和唯一标签Writer/Outbox Worker，具备配置验证、scope隔离、启动失败回收与安全关闭。

**Spec:** Frozen Architecture 3/4/6；TASK_BACKLOG 03A/04B、用户C-01/C-02/C-10范围及既有API组件。

**Architecture:** 无配置probe模式只运行127.0.0.1:8090探针；worker模式显式提供独立DB路径、API origin/key、tenant与KB列表。先监听成功，再初始化DB/单一Writer，启动Reconciler和scoped Worker；终止时取消后台上下文、关闭HTTP、等待任务、关闭Writer后关闭DB。各业务Hook/readyz保持503，不能因后台任务运行而声称完整Bridge就绪。

## Review Focus / Constraints

- 仅extensions和项目文档；原生源码/根依赖/Frozen/BASELINE不变。
- worker配置必须完整且先验证，无效配置不创建DB/访问网络；API key不出现在日志、配置格式化或错误中。probe不能静默忽略提供的worker配置。
- 生产入口固定IPv4回环8090，不提供公网地址覆盖。占用端口时不打开DB、不启动后台任务；测试Serve只允许IPv4回环监听器。
- Worker只认领配置tenant/KB的pending及expired processing，未配置任务保持原状；空scope不得扩大为全库。
- 多KB共享唯一Tag Writer；C-10 null拒绝、目录预配置及远端/UI竞态边界不改变。worker可执行真实标签PUT，需显式启用和配置权限。
- 启动失败/取消/Serve错误均回收listener、HTTP、后台任务、Writer和DB；Reporter只收固定事件码且需快速返回；数据库在任务退出后关闭。
- 接受来源内存队列弱点，对账只保存观察，不生成治理active或缺失历史。readyz/业务Hook不会提前放行或ACK。
- 本批验证只使用临时DB/本地HTTP fixture；真实WeKnora、业务DTO/核心薄接入、MCP、C-10首写、远端CI/race另验。

## Task 1: Configuration

Files: `extensions/host/config.go`、`config_test.go`、`extensions/.env.example`、`extensions/README.md`。

Interfaces: `host.LoadConfig(getenv func(string)string) (Config,error)`、`Config.Validate() error`。Mode probe/worker；worker字段DBPath/UpstreamURL/APIKey/TenantID/KnowledgeBaseIDs；ReconcileInterval=1m、PollInterval=1s、Lease=1m、RetryDelay=5s、MaxAttempts=5、ShutdownTimeout=10s；所有时长1ms..24h，重试次数1..100、KB列表1..1000且去重拒绝。

- [x] RED：默认probe、完整worker、缺失/错误origin/tenant/重复KB/时长/混用配置拒绝，格式化及错误无secret。
- [x] 实现完整配置验证；不自动读取.env，不引入新依赖；GREEN。

## Task 2: Scoped durable claims

Files: `extensions/outbox/store.go`、`worker.go`及测试。

Interfaces: `Store.ClaimScoped(ctx,now,lease,[]Scope) (*Item,error)`，空列表拒绝；Worker新增Scopes（nil保留既有全库组件接口，Host必须显式非空）。

- [x] RED：未配置tenant/KB不被认领，含expired processing；空/非法scope拒绝，Worker只处理授权集合。
- [x] 事务内SELECT限制scope再claim；既有token/失败重试语义保持；GREEN。

## Task 3: Lifecycle owner and command

Files: `extensions/host/runtime.go`、`runtime_test.go`；修改`extensions/cmd/extension-host/main.go`、新增`main_test.go`；修改`extensions/.gitignore`忽略本地DB文件。

Interfaces: `host.Run(ctx,Config,report func(string)) error`固定监听；`host.Serve(ctx,Config,net.Listener,report func(string)) error`拥有测试监听器且只接受127.0.0.1。Reporter固定码；默认probe不打开DB。worker模式使用既有NewHTTPSource/NewObserver/NewWriter/NewMapping/NewProjector/NewStore/Worker/Reconciler，初始化后立即对账及处理积压。

- [x] RED：真实loopback探针/业务503、worker模式周期收敛与持久Outbox恢复、两个KB单写者、未配置任务保持pending、API故障可观察后恢复；取消后可重开DB/端口可复用。
- [x] 验证坏配置/数据库拒绝/已取消/端口占用/非loopback listener全部无启动副作用并回收资源。
- [x] 接线main的signal.NotifyContext（Interrupt/SIGTERM）和安全错误出口；GREEN。

## Completion

- [x] Go build/test/vet/gofmt、Python模块门禁和7例回归、原件hash与范围核对。
- [x] 一次独立只读复核；无实质问题，更新PROJECT_CONTEXT/TASK_BACKLOG/Change Map；保留C-10及真实服务/Bridge/MCP/混合并发/CI/race未验门禁。

完成本地接线：2026-10-30。实际8090 probe请求检查退出0；Ctrl+C后host_stopped，go run终端包装进程因中断退出1。ShutdownTimeout仅约束HTTP排空，后台取消后等待持久资源安全清理；未宣称总体退出硬期限。详细证据见TASK_BACKLOG第四批执行记录。
