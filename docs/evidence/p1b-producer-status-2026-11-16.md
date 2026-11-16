# P1b（T4-04）producer 正向闭环：现状与阻塞（2026-11-16）

## 已有实现

写入侧 producer 接线已在 `b47cd88` 合入：

- `internal/agent/tools/sandbox_producer.go`：`reserveSandboxWrite` 在真实写入前预留 producer，`commitSandboxWrite` 只对**写后读回的字节**提交，摘要不匹配则拒绝提交。
- `internal/agent/tools/sandbox_write.go`：写入前 reserve，写入后读回并 commit；无 provenance handle 的普通会话跳过 producer 但照常写入（不会伪造归属）。
- `internal/extensionbridge/source_copy.go`：`ReserveSourceProducer` / `CommitSourceProducer` 走既有 `/provenance/producers` 协议。

定向测试（本轮实跑，全部 PASS）：

```text
TestReserveSandboxWriteWithoutProvenanceHandleIsUnavailable
TestReserveSandboxWriteWithoutHandleOpensNoStep
TestSandboxProducerPathIsRelativeAndCanonical
TestSandboxProducerCopyIDIsStablePerPath
TestCommitSandboxWriteRefusesMismatchedReadBack
TestCommitSandboxWriteWithoutReservationIsUnavailable
TestCommitSandboxWriteAcceptsMatchingReadBack
TestNoteSandboxProducerOutcomeSwallowsCommitErrors
ok  github.com/Tencent/WeKnora/internal/agent/tools  0.451s
```

## 生产为什么一条 producer 都没有

`ext_provenance_producers` 在生产库中为 **0 行**。逐层排查结论：写工具在本部署中根本无法被授予。

1. 智能体配置只有 `allowed_tools` 里显式列出的工具可用；`write_sandbox_file`、`edit_sandbox_file`、`shell_exec` 属于 **switch-managed**（由沙箱配置决定，而不是 `allowed_tools`）。
2. 实测创建一个带写工具的探针 agent，接口返回：`allowed_tools_warnings.dropped = [write_sandbox_file, edit_sandbox_file, shell_exec]`——三个都被丢弃。
3. 开关来源是 `config.sandbox_config_id`；`GET /api/v1/sandbox-configs` 返回 `{"data":[]}`，本部署没有任何沙箱配置。
4. 沙箱后端只支持 `cube` / `e2b` / `docker` 三种外部提供商（`TenantSandboxConfig.SandboxType`），不存在“本地目录”沙箱。

因此：没有沙箱提供商 → 没有 `sandbox_config_id` → 写工具不可见 → 没有真实写入 → 没有 producer 预留与提交。这是基础设施前置条件缺失，不是代码缺陷。

## 结论

- P1b 的**代码接线已完成并有定向测试覆盖**；`reserve→写→读回→commit` 的失败语义（无 handle 不建步骤、读回摘要不符拒绝、未知 ACK 不重放）均已验证。
- P1b 的**生产端到端闭环仍未完成**，因为它需要一个真实沙箱提供商配置（cube / e2b / docker 之一）以及一次真实 agent 写文件。
- 本轮未改任何生产代码或生产数据；探针 agent 已删除。

## 需要的决定

1. 是否为本机部署接入一个沙箱提供商（需要 cube / e2b / docker 的地址与凭据）；或
2. 是否授权一条“宿主机工作区”写入路径（当前架构没有该后端），这属于产品变更，不是薄接线。

在此之前，producer 正向链路只能在夹具层验证，不能声称生产已闭环。
