# 生产环境启动与实际端点验证（2026-11-16）

## 启动

执行 `start.cmd` 启动本机实际 AgentRAG 栈，`start.cmd status` 显示：

- WSL network：mirrored
- MinerU :8000：UP
- supervisor :8092：UP
- embedding :8081：UP
- rerank :8082：UP
- docreader :50051：UP
- Host :8090：UP
- Core :18880：UP

## 只读端点

| 检查 | 结果 |
|---|---|
| `http://127.0.0.1:18880/health` | HTTP 200 |
| `http://127.0.0.1:18880/` | HTTP 200 |
| `http://127.0.0.1:8090/health` | HTTP 200 |
| `http://127.0.0.1:8090/mcp` 无凭据 | HTTP 401（预期拒绝） |
| MCP `initialize`（Bearer 管理凭据） | HTTP 200，协议 `2025-03-26` |
| MCP `tools/list` | HTTP 200，返回 `query_related_docs` 和 `search_archived_documents` |

## MCP 查询边界

使用生产 MCP 端点对两个已确认处于 active 的文档执行 `query_related_docs`，均返回 `isError:true` 的“文档不可用或超出授权范围”结果，未返回关联内容。该结果作为生产 MCP 读取失败关闭记录保留；未执行任何文档修改、删除或生命周期写操作。

## 限制

- 本次仅使用机器 API key 和管理凭据做只读检查；生产用户 JWT 未提供，因此未执行 Agent 真实模型自主调用或受治理的业务写操作。
- M4-MCP 真实模型 E2E 和 M5-BINARY 生产上传/绑定/发布验证仍需登录态与完整业务数据授权。
- 生产服务保持运行，未执行迁移、删除或清理。
