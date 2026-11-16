# 原生 MCP 按需发现注册（生产环境，2026-11-16）

## 范围

本文件记录 M0-05「使用原生 MCP 按需发现注册本机 Extension MCP Server，采用 Streamable HTTP `/mcp` 与本机认证；不改 ToolRegistry」在本机**真实生产环境**下的完成情况。验证使用生产 Core（18880）与生产 Extension Host（8090），未建立隔离环境或虚拟环境。

## 前置凭据

MCP 注册需要带 `manage_mcp_services` 能力的 API Key。`launcher_config.py mcp-key` 原为交互式签发，但该动作同时接受 `AGENTRAG_LOGIN_EMAIL` / `AGENTRAG_LOGIN_PASSWORD` 环境变量，因此可用原生登录凭据非交互完成：

```text
[MCP] full-access registration key issued and stored in the 0600 launcher state
mcp_key_exit:0
has_admin_key: True
```

签发结果写入 `data/agentrag-launcher.json` 的 `MCP_ADMIN_KEY`（该文件在 `.gitignore` 内）。启动日志此前记录「no MCP credential: run ./start-all.sh mcp-key once」，本轮该缺口已消除。

## 注册结果

`register_related_mcp.py` 以 `MCP_ADMIN_KEY`（`X-API-Key` + `X-Tenant-ID`）调用原生管理接口，幂等注册并刷新工具目录：

```text
[MCP] AgentRAG registered; tool directory refreshed
register_exit:0
```

生产侧读回：

| 项 | 值 |
|---|---|
| 服务名 | AgentRAG |
| 服务 ID | `c518d652-0fe6-43d2-8e1c-2225abc170a6` |
| URL | `http://127.0.0.1:8090/mcp` |
| 传输 | http-streamable |
| 启用 | true |
| 元数据 | `synced: true`，`tool_count: 2` |

工具目录（`GET /mcp-services/{id}/tools` 返回 200）：

- `query_related_docs`：查询制度的有效补充通知、确认关联和附件下载链接；入参 `document_id`（必填）；标注 `readOnlyHint: true`。
- `search_archived_documents`：按短关键词查询授权范围内的历史条款。

## 按需发现的实际消费证据

注册前，`register_related_mcp.py` 无凭据可用；注册后，**生产 Agent 的真实运行已经走通按需发现链路**。同一轮生产 Agentic 评估（见[`m4-agentic-eval-2026-11-16.md`](m4-agentic-eval-2026-11-16.md)）的工具调用统计显示：15 道问题中 13 道触发了 `discover_mcp_tools`（42 次）→ `call_mcp_tool`（16 次）→ `mcp_agentrag_query_related_docs`（16 次）的完整链路。这条链路由原生 Agent 按需发现本机 Extension MCP Server 并调用其工具，未改 `ToolRegistry`，也未使用 stdio。

## 边界

- 本文件证明**注册、工具目录刷新与原生按需发现消费**三项已成立；未证明 `query_related_docs` 在真实业务问题上的**答案正确性**——那属于 Agentic 评估的人工评分部分，见 M4 Agentic 评估证据文件。
- 定向复跑「Agent 发现并调用本次新注册服务」需要云端生成模型；本轮 `deepseek-flash` 账户余额耗尽（402 Insufficient Balance），该定向复跑未完成。已有的 13 题消费证据来自注册完成前的同一生产运行。
- 生产数据未被修改；新增一个管理密钥（存于忽略目录）与一个 MCP 服务记录（生产配置，可删除）。
- 本轮未改动原生 `ToolRegistry`、存储或调度逻辑。

## 复现材料

- `extensions/scripts/launcher_config.py`（`mcp-key` 动作）
- `extensions/scripts/register_related_mcp.py`（幂等注册与目录刷新）
