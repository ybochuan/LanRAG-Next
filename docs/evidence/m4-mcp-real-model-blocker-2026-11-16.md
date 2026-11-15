# M4-MCP 真实模型 E2E 环境阻塞记录（2026-11-16）

## 检查

- 2026-11-16 用户提供了测试用 `AGENTRAG_E2E_DEEPSEEK_KEY`；`m1_native_e2e.py --live-models` 已识别真实 embedding（2560 维）和 DeepSeek 生成配置。
- 本机模型 supervisor 已启动，embedding/rerank 端口响应正常；另发现本地 chat 模型 `Qwen3.5-9B-Q4_K_M-imatrix.gguf`。
- `m1_native_e2e.py --live-models` 因 Windows 保留端口范围占用 canonical Host `8090`，无法获得要求的 Host 端口，未进入真实联合 E2E。
- `MCP_ADMIN_KEY`：仍未签发。
- 因此 M4-MCP 的“真实模型自主调用”和“真实模型下服务停用” E2E 仍未执行。

## 已完成的替代验证

- `extensions/mcp/server_test.go` 已覆盖 MCP 原生上游不可达时的 fail-closed 组件行为。
- M5-BINARY 改动已提交：`4985d22 feat(m5-binary): add binary revision workflow and workbench upload`。

## 真实 E2E 所需条件

1. 配置真实 chat 模型凭据（例如 `AGENTRAG_E2E_DEEPSEEK_KEY`）并启动模型服务；
2. 启动可用的 Core/Host 完整环境；
3. 在原生 MCP 服务启用、停用和 Host 停止三种状态下分别执行 Agent 工具调用；
4. 记录实际模型请求、工具调用序列、停用/离线后的零生成结果。

当前不把组件测试冒充真实模型 E2E，也不把该阻塞记录当作 M4-MCP 验收通过。
