# T4-04 实际文件写入 producer 接线（2026-11-16）

## 范围

本文件记录 T4-04「实际文件写入 producer：真实写→归档→跨轮读，��盖不缩源，ACK 未知不重放，重启撤销」的接线实现与生产环境验证。验证使用本机真实生产环境（生产 Core 18880、生产 Host 8090、生产扩展库），未建立隔离环境或虚拟环境。

## 实现前的实际缺口

扩展侧与原生侧的协议组件早已存在，但**没有任何调用者**：

- `extensions/provenance/producer.go`：`ReserveProducer` / `CommitProducer`，要求绑定一个 `kind == "write"` 的开放 step。
- `extensions/provenance/copy_http.go`：`POST /provenance/producers`，`action` 为 `reserve` 或 `commit`。
- `internal/extensionbridge/source_copy.go`：`ReserveSourceProducer` / `CommitSourceProducer` 薄客户端，从 context 取 run handle 与当前 step。
- `internal/agent/tools/{sandbox_write,sandbox_edit}.go`：真实的沙箱文件写入点。

生产扩展库 `ext_provenance_steps` 当时只有 153 行，`kind` 仅有 `model`（75）与 `tool`（78），**从未出现过 `write`**——这与「实际写入路径没有预留 producer」一致。

## 接线内容

新增 `internal/agent/tools/sandbox_producer.go`，并在 `sandbox_write.go` 与 `sandbox_edit.go` 的实际写入点前后调用。遵循仓库既有的可测试接线惯例（`internal/agent/tools/governance.go` 的变量替换模式）：

```go
var reserveSandboxProducer = extensionbridge.ReserveSourceProducer
var commitSandboxProducer = extensionbridge.CommitSourceProducer
var beginSandboxProducerStep = extensionbridge.BeginSourceStep
```

写入流程：

1. 在文件锁内、写入前，用 `BeginSourceStep(ctx, "write")` 打开 write step，再用 `ReserveSourceProducer` 预留 producer，绑定 run、step、copy id、相对路径与目标字节的 SHA256。
2. 执行原有写入。
3. 写入后**回读实际字节**，只有回读摘要与预留摘要一致时才 `CommitSourceProducer`；不一致则不提交。

两条硬边界：

- **没有 provenance handle 的 run 不阻断写入**。此时没有 producer，文件保持「未归属」，而不是被虚假地认领。普通沙箱使用不受影响。
- **producer ACK 丢失不重放写入**。提交失败只记一行日志（`noteSandboxProducerOutcome`），绝不重试底层写入。

路径与字节的构造规则：Host 拒绝绝对路径与上级穿越，因此发送前把沙箱路径裁成 layout 相对形式（`/workspace/output/report.md` → `report.md`）；copy id 与幂等键都由相对路径派生，同一路径重试指向同一 copy。

## 生产环境验证

用当轮构建的 Core（`tmp/WeKnora-lite-remediation`，SHA256 `4b5d26f9a6b70c56a8e15837bf4197a3bf6ef4f9a3b172d2783b47218603ef26`）与 Host 重启生产栈（`launcher-run-PnExa7`），直接对生产 Host 发起 provenance 协议调用：

| 步骤 | 结果 |
|---|---|
| `POST /provenance/runs/begin`（含授权文档） | 成功，返回 run_id 与 capability |
| `POST /provenance/runs/steps`，`kind: "write"` | **成功**，返回 step_id |
| 生产库 `ext_provenance_steps` 的 kind 分布 | 由 model 77 / tool 80 **新增 write 1** |
| `POST /provenance/producers`（reserve） | 拒绝：run 尚无已登记的输入来源 |

生产库因此**首次出现 `kind=write` 的 step**（run_id `472e64c76473d5a7fa42125672c07663e9c51cd871a14b99`，step_id `03390e80ba3540bebaed571d223f62ea56b0fec6b94f5cd7`，state `open`），这是 producer 预留的必要前提，此前生产中不存在。

reserve 被拒绝是设计意图而非缺陷：`ReserveProducer` 要求 run 拥有已登记且权限通过的输入来源（`len(v.Sources) == 0` 时返回 `ErrEvidence`）。本轮探针直接构造 run 而未登记真实原生输入，因此被正确拒绝。绕过它需要伪造来源，那正是该机制要阻止的事。

守卫行为也在生产中逐项确认：`GET` 返回 405、错误 key 返回 401、缺版本头返回 400，说明 `available()` 为真、服务已挂载、认证与版本门禁均按预期生效。

## 端到端写入未完成的部分

完整的 `reserve → 写入 → commit` 尚未在生产环境跑通，原因是外部条件而非实现缺陷：

- `reserve` 需要真实的原生输入证据（`VerifyInput` 要核对原生快照中某字段的真实哈希），只有真实检索链能提供。
- 生产 Agent 的 `write_sandbox_file` 触发人工确认门（`ask_user`），自动化会话无法完成该写入。
- 云端生成模型账户余额耗尽（402），本地 9B 模型在 Agentic 评估中未选择 `write_sandbox_file` 工具（本地小模型的工具选择能力弱，实测走了 `read_file` → `ask_user`）。

因此本文件证明的是：接线已就位、write step 前提在生产中成立、守卫按预期工作；**未**证明完整 reserve/commit 闭环，也未证明跨轮读与撤销。

## 验证命令与结果

```text
gofmt -l internal/agent/tools/            → 无输出
CGO_ENABLED=1 go build -mod=readonly ./internal/agent/...     → 退出 0
CGO_ENABLED=1 go vet -mod=readonly ./internal/agent/tools/... → 退出 0（仅第三方 C 库既有警告）
CGO_ENABLED=1 go test -mod=readonly ./internal/agent/tools/ -count=1 → ok 9.0s
```

新增测试（`internal/agent/tools/sandbox_producer_test.go`，7 例）：无 provenance handle 时不预留也不开 step；路径裁剪为相对形式；copy id 按路径稳定；回读摘要不符时拒绝提交；无预留时提交返回不可用；回读一致时提交并带出实际摘要；producer 失败被记录但不抛出。

注意：原生包的构建与 vet 在 `CGO_ENABLED=0` 下因 `pg_query` 与 DuckDB bindings 不可用而失败，这是仓库既有的环境基线，不是本次改动引起；`CGO_ENABLED=1` 通过。

## 边界

- 本轮改动限于 `internal/agent/tools/` 三个文件，未改原生检索、解析、Agent 调度算法，未改扩展库 schema，未改根 `go.mod`/`go.sum`。
- 未改 `ToolRegistry`、存储或调度逻辑。
- 未声称跨轮读、撤销或并发写入的完整验收；这些属 P9（完整故障矩阵）与 P7（副本正向链路）。
- 生产数据未修改；探针创建的 run 与 write step 是可识别的探测记录，扩展库中 producer 表仍为 0 行。
