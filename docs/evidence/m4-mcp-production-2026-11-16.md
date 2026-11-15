# M4-MCP 生产环境真实模型验证（2026-11-16）

## 环境

- Core：`http://127.0.0.1:18880`（生产，实际数据库）
- Host：`http://127.0.0.1:8090`
- 知识库：`164bfd73-d231-4d5c-994e-057ac0cdf48a`
- Agent：`builtin-smart-reasoning`
- MCP 服务：`c518d652-0fe6-43d2-8e1c-2225abc170a6`（AgentRAG），验证后保持启用

## 真实模型自主调用

使用生产登录态创建会话并发送查询，真实模型（DeepSeek）实际产生的工具调用序列包含：

- `discover_mcp_tools`
- `call_mcp_tool`
- `mcp_agentrag_query_related_docs`

说明真实模型能够按 MCP 服务目录发现并调用 `query_related_docs`，不是仅由本地夹具模拟。

## 服务停用

通过生产 JWT 将 AgentRAG MCP 服务临时设为 `enabled:false` 后重新发起同类查询，实际工具调用仅出现：

- `list_documents`
- `ask_user`

未出现任何 `query_related_docs`/MCP 工具调用。验证结束后已通过生产 API 恢复 `enabled:true`。

## 边界

- 本次仅验证 M4-MCP 的真实模型调用与服务停用/恢复；未执行文档修改。
- Host 停机场景尚未单独验证。
- M5-BINARY 生产上传→绑定→发布仍需独立在扩展 Host 的受管知识库范围内执行。
