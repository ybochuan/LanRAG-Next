# M5-BINARY 生产环境验证（2026-11-16）

## 环境

- Core：`http://127.0.0.1:18880`（生产实际数据库）
- 知识库：`164bfd73-d231-4d5c-994e-057ac0cdf48a`
- 使用生产测试账户登录并调用原生管理接口

## 流程

使用两个临时测试文档（标题前缀 `AgentRAG_M5BINARY_*`）：

1. 原生文件上传 source/target，等待 `parse_status=completed` 且扩展生命周期 `active`；
2. `revision_create_binary` 创建建议：`pending_review`；
3. `revision_approve`：`approved`；
4. `revision_bind_binary`：绑定 target 文档，状态 `approved`；
5. `revision_publish`：`published`，目标文档为绑定的 target；
6. 验证 source 生命周期为 `superseded`；
7. 删除两个临时测试文档，均返回 HTTP 200。

## 结果

- `revision_create_binary`、`revision_bind_binary`、`revision_publish` 均成功。
- 发布结果：

```text
published = {
  "status": "published",
  "target_document_id": "6259bb0d-...",
  "revision": 5
}
source_state = ["superseded"]
```

- 测试文档清理后生产知识库未保留上述临时文档。

## 本批补充的故障覆盖

生产环境本轮完成的是主链验证，不包含故障注入。代码侧新增并通过的故障覆盖见
[`m5-binary-backend-2026-11-15.md`](m5-binary-backend-2026-11-15.md)：

- 目标版本观察失败返回 `binary_source_version_unknown`，不再与版本漂移混淆；
- 版本漂移返回 `binary_source_version_changed`，source 保持 `active`，无关键边；
- finalize 审计失败整体回滚，source 与源快照不变；
- 恢复后重试可发布，且源原件内容始终未被覆盖。

## 边界

- 测试文档为文本 fixture，用于验证生产环境二进制修订发布链路；不代表 PDF/DOCX 解析质量验收。
- 生产环境未注入远端 ACK 丢失、SQLite 进程崩溃或并发负载；这些场景由真实 SQLite 单元/事务测试覆盖，见 M5 验收证据。
- 生产 M4-MCP 真实模型与服务停用已在 [m4-mcp-production-2026-11-16.md](m4-mcp-production-2026-11-16.md) 记录。
