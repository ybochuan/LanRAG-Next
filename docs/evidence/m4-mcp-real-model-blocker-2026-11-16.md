# M4-MCP 真实模型环境记录（2026-11-16）

## 已解除

- 用户提供了测试用 DeepSeek key；`m1_native_e2e.py --live-models` 已识别真实 embedding（2560 维）和 DeepSeek 生成配置。
- 本地模型 supervisor、embedding、rerank 和 chat（`Qwen3.5-9B-Q4_K_M-imatrix.gguf`）均已启动并通过端点检查。
- 2026-11-16 在生产 Core/Host 上使用真实 DeepSeek Agent 验证了 `discover_mcp_tools`→`call_mcp_tool`→`mcp_agentrag_query_related_docs`；临时停用 MCP 服务后未出现 MCP 工具调用，随后已恢复启用。证据见[`docs/evidence/m4-mcp-production-2026-11-16.md`](m4-mcp-production-2026-11-16.md)。

## 剩余

- Host 停机场景尚未单独验证。
- M5-BINARY 的生产上传→绑定→发布仍需在扩展 Host 的受管知识库范围内单独执行。
