# P2-A `reconciliation_failed` 启动对账失败专项审计（2026-11-07）

范围：针对 `docs/evidence/SQLITE-ACCEPT-2026-11-07.md` 验收结论中保留的“启动时单条 `reconciliation_failed`”开展只读审计。目标为依据用户 m03653 的 P2-A A1/A2/A3 要求，定位实际调用链、完整失败原因与重试/租约/事务行为；本文件不包含代码修改，A4 修复与恢复测试待确认后进行。

审计基线：HEAD `a6aadb8`；审计期间生产栈保持运行，未停止 Core/Host，未修改数据库。

## 结论

**A1/A2/A3 已完成：`reconciliation_failed` 是一次启动时序竞争导致的瞬时对账失败，不是数据损坏、租约冲突或事务失败。**

- Host 先启动并立即执行首次 `Reconciler.Sweep`，此时 Core 尚未监听 `127.0.0.1:18880`；第一次 `ListKnowledge` 请求得到 `upstream transport failed`（底层为连接失败）。
- Core 约 1.7 秒后启动，首次成功 `ListKnowledge` 出现在 `16:18:20.609`；Host 的 5 秒周期重试随后成功，当前运行没有再次出现该错误。
- `reconciliation_failed` 只有固定代码，没有对象、页码或底层错误：`extensions/host/runtime.go:135` 的 `Report: func(error) { report("reconciliation_failed") }` 丢弃了 `error`；`extensions/cmd/extension-host/main.go:18` 的 logger 只接收 code；`extensions/weknora/client.go:80-85` 在 `http.Do` 失败时也只返回 `upstream transport failed`，没有保留 `connection refused` 等根因。因此无法仅凭现有日志恢复完整错误文本，这是 P2-A 的第二项缺陷。
- 当前数据库没有对账造成的数据损坏：`ext_ingest_current=77`（69 completed / 8 failed，后者是文档解析状态，不是对账失败）、`ext_ingest_observations=537`；每文档 2–19 条、平均 6.97 条，未发现重复当前指针或无限增长。`last_seen_at` 最大为 `2026-11-07 16:32:00`（本地时间），说明 5 秒对账在此后仍持续工作。
- `ext_projection_outbox` 的 140 条 `dead` 记录是独立历史问题（2026-10-31 至 2026-11-05），不属于本次启动失败；不应在 P2-A 修复中顺手改动。

## A1：实际调用链

1. `start-all.sh:259-270` 的启动顺序是先 `start_host`，等待 Host `/health` 后再启动 Core；注释明确写着 “Host first; waiting for local DB and services (Core-independent readiness)”。
2. Host 的 `/health` 由 `extensions/bridge/server.go:55-63` 处理，`services.Ready` 在 `extensions/host/runtime.go:173-178` 只检查 `localReady` 和本地扩展数据库 `SELECT 1`，不检查 Core。
3. `extensions/host/runtime.go:135` 创建 `ingest.Reconciler`，`Report` 回调固定为 `reconciliation_failed`；`extensions/host/runtime.go:242-243` 启动 `reconcile.Run(ctx, scopes)` 后台协程。
4. `extensions/ingest/reconcile.go:106-128` 的 `Run` 明确“performs an immediate scan, then periodic scans”；第一个循环在等待 ticker 前就调用 `Sweep`，没有初始延迟、Core readiness 门槛或启动宽限期。
5. `extensions/ingest/reconcile.go:73-75` 调用 `Source.List` 并将错误包装为 `reconcile page 1: ...`；`extensions/ingest/http_source.go:41` 的 Source 最终调用 `weknora.Client.JSON`。
6. `extensions/weknora/client.go:80-85` 在 `c.http.Do` 失败且 context 未取消时返回固定字符串 `upstream transport failed`，不保留底层 `net.OpError`；`http_source.go` 和 `reconcile.go` 没有更细分类。
7. `extensions/host/runtime.go:135` 与 `extensions/cmd/extension-host/main.go:18` 两层都只保留 code，最终日志只有 `reconciliation_failed`。

## A2：实际错误与失败对象

当前生产运行目录 `data/launcher-run-8p8Mbr`：

```text
2026/11/07 16:18:15 host_workers_started
2026/11/07 16:18:15 reconciliation_failed
```

同一运行目录 `core.log` 第一行时间为 `2026-11-07 16:18:16.990`，首次 `ListKnowledge` 请求时间为 `16:18:20.609`。这证明 Host 首次扫描发生在 Core 尚未启动完成之前。

失败对象是 Host 对第一个知识库 scope 的第 1 页 `ListKnowledge`；由于 `weknora.Client.JSON` 丢弃底层错误，现有日志中没有可恢复的 URL、tenant、KB、HTTP 状态或具体网络错误。依据启动顺序与错误路径，最可能的底层错误是连接 `127.0.0.1:18880` 时的 `connection refused`/transport failure；不能把这条推断写成已从日志直接读取的完整错误。

当前数据库中的文档状态是 `ext_ingest_current` 69 completed / 8 failed，8 个 failed 对应已知的文档解析失败，并非本次 reconciler 失败。历史 `host.log` 中同一秒的 `reconciliation_failed` 与 `host_workers_started` 共同出现，当前运行从 16:18:15 到 16:18:20 后恢复，未见持续对象级错误。

## A3：重试、租约与事务

- **周期**：运行目录 `private.env` 设置 `AGENTRAG_RECONCILE_INTERVAL=5s`；`extensions/ingest/reconcile.go:112-127` 使用 `time.NewTicker` 固定周期，失败后不改变间隔，也没有指数退避、抖动或“仅在状态变化时报告”。
- **顺序**：`extensions/ingest/reconcile.go:67-103` 先完整分页收集并校验所有文档，再逐条 `Observe`；不会在分页半途写入一半就提交，避免不完整页被应用。
- **幂等与并发**：`extensions/ingest/observer.go:55-90` 的 `PersistentObserver.Observe` 以 `(tenant_id,knowledge_base_id,document_id)` 当前指针和 `source_updated_at` 做幂等/陈旧保护；`Reconciler.Sweep` 由 `r.mu` 串行化，单写者事务按顺序执行。
- **数据库证据**：`ext_ingest_current` 当前 77 条，`ext_ingest_observations` 537 条，观测覆盖 77 个文档；每文档 2–19 条、平均 6.97 条，无重复当前指针；`last_seen_at` 最大 `2026-11-07 16:32:00`（本地），证明 5 秒扫描持续运行。
- **历史模式**：扫描全部 `data/launcher-run-*/host.log`，40 个运行目录出现过 `reconciliation_failed`；绝大多数只有启动时 1 条，少数为：
  - `launcher-run-XSXYKV`：10:27:39、10:27:44 两条；
  - `launcher-run-blel2s`：11:07:23、11:07:28 两条；
  - `launcher-run-ObfOS5`：22:15–22:56 连续 233 条；
  - `launcher-run-1eY0Oc`：23:24–23:59 连续 296 条；
  - `launcher-run-4xSZ7s`：11:41–13:57 连续 240 条（对应 xlsx embedding/SQLite 长时间不可用期间）。
  该分布符合“启动瞬间 Core 不可用”或“Core 长时间不可用”两类可用性问题；没有证据表明存在周期性对象级数据错误。
- **Outbox**：`ext_projection_outbox` 140 条全部 `status=dead`、`retry_count=5`、`processed_at` 为空；创建时间 2026-10-31 22:16–2026-11-05 23:58（本地），错误为 72 条 `upstream transport failed`、68 条 `canonical tag not configured in native KB`。`extensions/outbox/store.go:198-217` 在达到 `maxAttempts` 后置 dead；`store.go:220-243` 提供 `RetryDead` 但没有自动恢复；canonical tag 缺失是 C-10 安全拒绝。该问题与启动时 `reconciliation_failed` 独立，不能把 dead 记录当作本次启动错误的对象。

## 根因判定

**根因（A1/A2）：Host 启动时先于 Core 建立监听，`Reconciler.Run` 首次 `Sweep` 没有启动宽限期或 Core readiness 检查，导致第一次 `ListKnowledge` 连接失败。** 这是启动时序竞争，不是数据库事务、租约或文档状态不一致。

**可观测性缺陷：错误在 `weknora.Client.JSON` 与 Host Report 两层被降级为固定 code，无法从日志判断“连接拒绝 / 超时 / HTTP 503 / 凭证错误”，也无法看到失败 page/scope。**

## 建议修复范围（待用户确认，尚未实施）

1. **启动门控（首选）**：在 `Reconciler.Run` 首次扫描前增加可配置启动宽限期（例如 5–10 秒），或增加不改变 Host 本地 `/health` 语义的 Core readiness 探测；Host `/health` 继续保持 Core-independent，避免破坏启动设计。
2. **错误可观测性**：把 `weknora.Client.JSON` 的 transport error 与 HTTP status 分类为不含 URL/token/响应体的稳定 code，例如 `reconciliation_failed: upstream_unreachable`、`reconciliation_failed: upstream_http_503`；Report 保留脱敏分类，不记录密钥或请求体。
3. **有上限退避与状态变化报告**：连续失败时按 5s→10s→20s→40s→60s 上限退避，成功时重置；同一状态只在首次/状态变化时报告，避免每 5 秒刷屏，但不得静默忽略。
4. **测试**：增加 Host 启动时 Core 暂时不可用、Core 随后恢复、context 取消、重复 sweep 幂等的测试；恢复测试应确认只产生一条启动失败记录（或明确状态变化记录），Core 可用后一次成功扫描恢复。
5. **Outbox 单列**：140 条 dead 记录、canonical tag C-10 与重新入队策略单独排期，不混入 P2-A 对账修复。

## A4 状态

**未执行**：未修改任何源码，未做重启/异常恢复测试。需用户确认上述修复范围后，再按范围修改并补充 `go test ./ingest ./host`、重启 Host/Core 和数据库状态验证。
