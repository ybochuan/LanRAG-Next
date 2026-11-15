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
go test -mod=readonly ./business -count=1
ok github.com/ybochuan/LanRAG-Next/extensions/business
```

测试覆盖：

1. `revision_create_binary` 创建待审建议；
2. `revision_approve` 批准；
3. `revision_bind_binary` 绑定 native 目标文档；
4. 目标版本漂移时 `Publish` 拒绝并记录 `binary_source_version_changed`，恢复后重试发布；
5. `Publish` 发布并验证源文档 `superseded`、提案目标文档为绑定文档。

## 边界

- 本批不新增原生上传接口；目标文档必须由用户通过原生上传路径先进入同一知识库。
- 工作台已提供正式文件上传、解析等待和自动绑定面板；真实 Core/Host 上传联合 E2E、未知 ACK、回滚与源原件保留仍需后续补齐。
- `go test -mod=readonly ./... -count=1` 扩展全量通过；前端 `npm run type-check`/`npm run build` 通过。
