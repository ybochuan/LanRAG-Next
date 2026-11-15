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

## 边界

- 测试文档为文本 fixture，用于验证生产环境二进制修订发布链路；不代表 PDF/DOCX 解析质量验收。
- 生产 M4-MCP 真实模型与服务停用已在 [m4-mcp-production-2026-11-16.md](m4-mcp-production-2026-11-16.md) 记录。
