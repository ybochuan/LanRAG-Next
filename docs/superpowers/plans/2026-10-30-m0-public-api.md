# M0 Public API Adapters Implementation Plan

> 用户已授权继续既有M0清单；按executing-plans本人顺序实施、TDD验证，最后一次独立只读复核。不提交、不推送、不改原生源码或数据库。

**Goal:** 完成03A公共API Source/持久Observer、04B标签映射/API Store/Outbox Handler组件及其联合测试。

**Spec:** Frozen Architecture第4/6章、用户C-01/C-02决定、TASK_BACKLOG的03A/04B。

**Architecture:** 共享有超时、响应上限、重定向拒绝的HTTP客户端，通过配置的API key和tenant访问原生公开路由。对账保存观察历史和current，不自动创建治理active。标签Handler在唯一Writer队列内读取当前lifecycle truth、按KB目录解析原生tag ID、merge/replace/verify；异常持久记录并由既有Outbox重试。

## Constraints / Review Focus

- 原生源码只读、root manifests不改；只对临时DB/httptest服务执行测试。
- C-10：原生knowledge.go:604–613标签加载失败仍成功，tags:null也可能是真正空列表。M0对null/缺失数组拒绝覆盖并重试，不假装已解决无标签文档首写；明确数组（含[]）按契约读取。
- 本批不自动创建原生tag；规范标签先由原生UI/API配置，缺失/重名拒绝，不用名称代替ID。
- 凭据只在固定源HTTP header；禁止重定向、URL userinfo/query/fragment；http仅限loopback，https允许配置的主机；错误不包含响应body/凭据。
- 每份文档、标签及分页结果检查tenant/KB/id；分页有上限，跨scope/重复/不完整结果拒绝。offset分页不是原子快照，不推断缺失/删除。
- 对账重复快照不重复追加；旧时间拒绝覆盖、同时间异状态冲突；observer更新同事务且重启可读，不冒充原始事件或生命周期事实。
- current truth的读取/映射/标签API全在同一个Writer任务内；旧Outbox payload不可降级当前状态。前后truth变化返回可重试错误。
- 标签读回不一致/HTTP错误写ext_tag_sync_failures；Outbox保留失败重试/dead，失败不回滚lifecycle truth。
- C-02原生UI及超时请求远端结果未知仍是限制；本地队列/lease不提供跨系统CAS。
- Host启动/业务DTO/原生Hook与MCP另批接线；readyz仍503；本地HTTP契约测试不等于真实WeKnora集成验收。

## Task 1: Scoped public HTTP client and ingest Source

Files: `extensions/weknora/client.go`、`client_test.go`；`extensions/ingest/http_source.go`、`http_source_test.go`。

Interfaces: `weknora.NewClient(baseURL,apiKey string,tenantID uint64) (*Client,error)`、`Client.TenantID() uint64`、`Client.JSON(ctx,method,path string,query url.Values,body,result any) error`；`ingest.NewHTTPSource(client,pageSize) (*HTTPSource,error)`实现既有Source。

- [x] RED：API key/tenant header、超时/重定向不泄露、错误/坏JSON/超限；native knowledge列表顶层分页、scope/元数据/重复校验。
- [x] 实现固定源客户端与完整分页适配；GREEN。

## Task 2: Durable observations and migration upgrade

Files: `extensions/store/migrations/002_ingest_taggov.sql`、`migrate.go`、`migration_test.go`；`extensions/ingest/observer.go`、`observer_test.go`。

Interfaces: `ingest.NewObserver(db) *PersistentObserver`、`Observe(ctx,Observation) error`、`Current(ctx,Scope,docID) (Observation,error)`；错误`ErrStaleObservation`、`ErrObservationConflict`。

- [x] RED：v1升级保留数据、快照幂等/陈旧/冲突、两处SQL故障回滚、重开DB读取，lifecycle表保持空。
- [x] 同一WriteExecutor提交observations/current；source时间UTC、重复时更新last_seen；GREEN。

## Task 3: Native tag Store, mappings and current-truth projector

Files: `extensions/taggov/http_store.go`、`mapping.go`、`projector.go`及各自测试；修改`writer.go`与测试增加`ApplyResolved(ctx,Scope,namespace,func(context.Context)([]Tag,error)) error`，解析器在worker内执行。

Interfaces: `NewHTTPStore(client) *HTTPStore`实现Store并有`Catalog(ctx,Scope) ([]Tag,error)`；`NewMapping(db,catalog) *Mapping`、`Resolve(ctx,Scope,canonicalName) (Tag,error)`；`NewProjector(db,lifecycleStore,mapping,writer) *Projector`、`Handle(ctx,outbox.Item) error`。

- [x] RED：两库同名不同ID、catalog分页/重名/scope、tags:null与缺失拒绝、显式数组/全量ID body、错误/回读不一致持久记录、旧Outbox投影最新truth、队列内解析顺序。
- [x] 实现HTTP Store/映射/失败记录和Handler；GREEN。
- [x] `extensions/integration/public_api_test.go`通过真实临时SQLite+原生格式HTTP fixture串起对账重启与Outbox错误后重试成功；保留普通/其他gov标签，truth不回滚。

## Completion

- [x] 新鲜Go build/test/vet/gofmt、Python门禁/7例回归、原件hash/核心diff与范围检查。
- [x] 一次独立只读复核；实质问题RED→GREEN；更新TASK_BACKLOG/PROJECT_CONTEXT/Change Map，C-10与未验范围明确保留。

## 执行记录

详见TASK_BACKLOG对应2026-10-30公共API批次。一次独立复核已修正Windows忽略规则导致client包不可追踪的问题；裸?配置校验回归RED→GREEN，4MiB限制用有效大JSON精确验证。C-10/真实服务/Host及其他后续门禁未解除。
