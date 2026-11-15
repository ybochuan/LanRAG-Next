# M4-MCP 真实模型 E2E 环境阻塞记录（2026-11-16）

## 检查

- `AGENTRAG_E2E_DEEPSEEK_KEY`：未设置。
- 本机 `data/agentrag-launcher.json` 的 `MODEL_SERVICES`：只有 embedding（8081）和 rerank（8082），没有 chat 模型。
- `MCP_ADMIN_KEY`：未签发。
- 因此无法在本机执行 M4-MCP 的“真实模型自主调用”和“真实模型下服务停用” E2E。

## 已完成的替代验证

- `extensions/mcp/server_test.go` 已覆盖 MCP 原生上游不可达时的 fail-closed 组件行为。
- M5-BINARY 改动已提交：`4985d22 feat(m5-binary): add binary revision workflow and workbench upload`。

## 真实 E2E 所需条件

1. 配置真实 chat 模型凭据（例如 `AGENTRAG_E2E_DEEPSEEK_KEY`）并启动模型服务；
2. 启动可用的 Core/Host 完整环境；
3. 在原生 MCP 服务启用、停用和 Host 停止三种状态下分别执行 Agent 工具调用；
4. 记录实际模型请求、工具调用序列、停用/离线后的零生成结果。

当前不把组件测试冒充真实模型 E2E，也不把该阻塞记录当作 M4-MCP 验收通过。
