# P2-A `reconciliation_failed` 启动对账修复证据（2026-11-07）

范围：按用户 2026-11-07 的 A4 批准，修复 Host 启动时 `reconciliation_failed` 的启动时序竞争，并实现 Core readiness 门控、底层错误分类与脱敏、失败退避和恢复测试。A4-05（Outbox dead-letter 重新入队）明确不在本轮范围。

代码基线：`a6aadb8`；P2-A 只读审计报告见 `docs/evidence/P2-A-reconciliation-2026-11-07.md`。

## 结论

**A4-01～A4-04 已完成：正常启动不再产生 `reconciliation_failed`；Core 暂时不可用时 Host 保持运行并等待；Core 恢复后自动完成对账。A4-05 仍暂缓。**

- Host 仍先于 Core 启动；Host `/health` 仍只表示本地扩展数据库就绪，启动顺序和 `/health` 语义未改变。
- Reconciler 独立调用 Core `/health` 做 readiness 检查；未就绪时输出 `reconciliation_waiting_for_core`，不执行 `Sweep`，也不把等待当成业务失败。
- Core 不可用/超时、HTTP 错误、响应异常、内部错误分别使用稳定错误码；报告只包含错误码、连续失败次数、下次重试时间和 UTC 时间，不包含 Token、URL 凭据、响应正文或底层网络错误。
- 退避为 5→10→20→40→60 秒（之后保持 60 秒），成功恢复后重置；Context 取消立即停止等待。
- 生产验证使用了正常冷启动和受控 Core `SIGSTOP`/`SIGCONT`，均未修改数据库结构或业务数据。

## 代码改动

| 文件 | 改动 |
|---|---|
| `extensions/weknora/client.go` | `UpstreamError`（`Code`/脱敏 `detail`/`cause`）；`UpstreamUnreachable`、`UpstreamTimeout`、`UpstreamHTTPError`、`UpstreamInvalidResponse`；`Health(ctx)` 调用 `/health` 并校验 `{"status":"ok"}`；传输/HTTP/读取/信封错误分类。 |
| `extensions/ingest/reconcile.go` | `Reconciler.Ready`、`Backoff`；`Notice`（`Kind`/`Code`/`Consecutive`/`NextRetry`）安全报告；默认退避 5/10/20/40/60 秒；`Run` 先等待 readiness，再扫描；失败按退避重试，成功报告 recovered 并重置。 |
| `extensions/host/runtime.go` | Host 创建 `Reconciler` 时接入 `Ready: client.Health`，`Report` 改为传递脱敏后的 `error` 文本；Host `/health` 的 `services.Ready` 未改。 |
| `extensions/weknora/client_test.go` | 新增 `Health` 200/503/invalid 分类测试和连接不可达分类测试。 |
| `extensions/ingest/reconcile_test.go` | 新增退避表、Core readiness 等待/恢复、等待中取消、错误分类脱敏、重复 Sweep 幂等（观察/生命周期事件/Outbox 不重复）测试；已有恢复测试改为使用短测试退避。 |
| `extensions/host/runtime_test.go` | mock Core 补 `/health`；运行时测试仍覆盖首次列表失败、恢复和数据库关闭。 |
| `extensions/host/lifecycle_policy_test.go`、`review_test.go`、`policy_e2e_process_test.go` | 测试 mock 补充 `/health` 就绪响应。 |

未修改：`start-all.sh` 的 Host→Core 顺序、Host `/health` 语义、Outbox、Core SQLite 连接池、WAL、前端、数据库 Schema、生命周期/反馈/血缘业务逻辑。

## 静态与自动化验证

在 `extensions/` 执行：

```text
gofmt -l <changed Go files>        # 无输出
go test -mod=readonly ./... -count=1
# 全部包 PASS；host 5.751s、ingest 0.865s、weknora 0.171s、其余包均 ok
go test -mod=readonly ./ingest -run TestReconcileRepeatedSweepDoesNotDuplicateObservationsOrEvents -count=1 -v
# PASS；重复 Sweep 后 ext_ingest_observations/current、生命周期事件、Outbox 数量不变
go vet -mod=readonly ./...
# exit 0，无输出
go build -mod=readonly ./...
# exit 0，无输出
python scripts/check_module_boundary.py
# module boundary passed: 322 imports, no WeKnora dependency or active workspace
git diff --check
# exit 0
```

## 生产启动验证

生产运行：`data/launcher-run-lY5BJT`；launcher 启动日志确认 `[READY] Host` → `[START] Core` → `[READY] Core`；`start.cmd status`：

```text
wsl mode    : mirrored
MinerU :8000     UP
supervisor :8092 UP
embedding :8081  UP
rerank :8082     UP
docreader :50051 UP
Host :8090       UP
Core :18880      UP
```

Host 日志实际内容：

```text
2026/11/07 16:55:44 host_workers_started
2026/11/07 16:55:44 reconciliation_waiting_for_core code=upstream_unreachable consecutive=1 next_retry=2026-11-07T08:55:49Z
2026/11/07 16:55:50 reconciliation_recovered
```

- Core `core.log` 首行时间为 `2026-11-07 16:55:46.186`，证明 Host 首次等待确实发生在 Core 启动之前。
- `grep -c 'reconciliation_failed' data/launcher-run-lY5BJT/host.log` 返回 `0`；正常启动不再产生失败代码。
- `reconciliation_waiting_for_core` 是预期的等待状态，不是失败；Host 在等待期间保持运行，`/health` 仍返回 `{"status":"ready","scope":"local"}`。

## Core 暂时不可用与恢复验证

在不停止 launcher、Host 进程的前提下，用受控信号暂停 Core：

- Host PID `722`、Core PID `739`（`data/launcher-run-lY5BJT/pids`）。
- `kill -STOP 739` 后，Core 健康请求超时；Host 仍运行，日志新增：

```text
2026/11/07 16:58:19 reconciliation_waiting_for_core code=upstream_timeout consecutive=1 next_retry=2026-11-07T08:58:24Z
```

- `kill -CONT 739` 后，Core 恢复；Host 日志新增：

```text
2026/11/07 16:58:44 reconciliation_recovered
```

- 恢复后 `curl` Core `/health` 返回 `200`、`{"status":"ok"}`，Host `/health` 返回 `200`、`{"status":"ready","scope":"local"}`；`reconciliation_failed` 仍为 `0`。
- `ext_ingest_current` 保持 77 条，`ext_ingest_observations` 保持 537 条；`MAX(last_seen_at)=1794041966761`（本地 `2026-11-07 16:59:26.761`），说明恢复后的对账扫描已继续运行，没有重复当前指针或额外业务事件。

## 已知限制与未完成项

- A4-05 Outbox dead-letter 未实施：`ext_projection_outbox` 仍有 140 条历史 `dead` 记录（72 条 `upstream transport failed`、68 条 `canonical tag not configured in native KB`），需按独立可靠性专项排期。
- `reconciliation_waiting_for_core` 会出现在正常启动日志中，这是刻意保留的状态变化记录；验收以不再出现 `reconciliation_failed` 为准。
- 本轮未修改 Outbox、Core SQLite 连接池、WAL、前端 N+1/轮询、数据库 Schema 或生命周期/反馈/血缘逻辑；P2-B–E 仍待后续。
- 生产验证使用受控 `SIGSTOP`/`SIGCONT` 模拟 Core 短暂不可用，不等同于 Core 进程崩溃或主机断电；Host 进程本身未重启。

## 结论

P2-A A4 修复满足用户批准的 A4-01～A4-04 范围，可以进入后续 P2-B；A4-05 与 P2-B–E 继续保留为后续工作。
