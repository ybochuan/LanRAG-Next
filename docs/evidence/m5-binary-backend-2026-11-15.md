# M5-BINARY 后端链路证据（2026-11-15）

## 范围

- `revision_create_binary`：为 binary_source 建立修订建议，不伪造 Markdown 正文。
- `revision_bind_binary`：绑定用户已上传到同一 knowledge base 的 native 文档，并记录其当前观察版本。
- `revision_approve` 后 `Publish`：校验目标当前版本，直接原子 finalize，源文档进入 `superseded`；不覆盖源原件。

## 实现文件

- `extensions/business/revision.go`
- `extensions/business/service.go`
- `extensions/business/revision_binary_test.go`
- `frontend/src/views/business/BinaryRevisionPanel.vue`（正式文件上传、解析等待、自动创建并绑定）
- `frontend/src/views/business/BinaryRevisionPanel.test.ts`（上传→创建→绑定顺序回归）

## 验证命令与结果

```text
go test -mod=readonly ./business -run 'TestBinary' -count=1
ok github.com/ybochuan/LanRAG-Next/extensions/business
```

测试覆盖：

1. `revision_create_binary` 创建待审建议；
2. `revision_approve` 批准；
3. `revision_bind_binary` 绑定 native 目标文档；
4. 目标版本漂移时 `Publish` 拒绝并记录 `binary_source_version_changed`，恢复后重试发布；
5. 目标版本观察失败时 `Publish` 记录 `binary_source_version_unknown`，不把未知结果当作已发布；
6. 最终业务审计触发器失败时，提案、source 生命周期、关键边及源原件均保持未提交；
7. 发布成功及失败路径均断言 source 文档版本与源快照内容不变，源文档仅在成功 finalize 后 `superseded`；
8. `Publish` 发布并验证提案目标文档为绑定文档。

本轮二进制测试命令通过；完整扩展构建与全量测试结果见 `m5-acceptance-2026-11-16.md`。

## 边界

- 本批不新增原生上传接口；目标文档必须由用户通过原生上传路径先进入同一知识库。
- 工作台已提供正式文件上传、解析等待和自动绑定面板；上述故障场景为真实 SQLite 单元/事务测试，未在生产环境注入远端 ACK 丢失或进程崩溃。
- `go test -mod=readonly ./... -count=1` 扩展全量通过；前端 `npm run type-check`/`npm run build` 通过。
