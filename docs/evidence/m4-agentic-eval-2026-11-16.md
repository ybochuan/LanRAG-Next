# Agentic 15–20 多跳评估（生产环境，2026-11-16）

## 范围

本文件记录冻结架构 M4「每库 15–20 多跳问题，轮数、工具序列、人工评分」在本机**真实生产环境**下的首次执行。生产知识库 `164bfd73-d231-4d5c-994e-057ac0cdf48a`（银行知识库，75 篇 active 且解析完成的文档），Agent 为 `builtin-smart-reasoning`（smart-reasoning / rag-qa）。未建立隔离环境或虚拟环境，未复制或修改生产业务数据。

问题集 15 道，全部由生产库真实文档构成，依据是库中客观存在的版本族与制度关系：现金管理业务管理办法 5 版、公司结算业务操作规程 2 版、发票管理办法 5 版、单位账户管理办法 3 版、交易银行单证业务管理办法 3 版，以及办法与其补充通知、监管文件与行内制度、公司业务上门服务管理办法 2 版、企业电子银行业务管理办法与操作规程、境外机构单位账户管理办法、客户受益所有人识别管理办法、深圳市业主大会和业主委员会备案管理办法 2 文件。

## 凭据与前置更正

上一份记录把 Agentic 评估的阻塞写成「生产 `AGENTRAG_WEKNORA_API_KEY` 对 `/api/v1/models`、`/api/v1/agents`、`/api/v1/sessions` 返回 403，`start-all.sh` 的 token 动作只签发模型 supervisor token，取不到 Core JWT」。**该描述不准确，此处更正**：

- Core JWT 可以取得。原生登录接口 `POST /api/v1/auth/login` 返回的 JSON **顶层** `token` 字段即访问令牌（本机测试账户）；用该 JWT 请求 `/api/v1/models`、`/api/v1/agents`、`/api/v1/sessions` 均为 **200**。上一份记录的 403 来自 API Key 本身的作用域：API Key 不能调用管理接口，需要原生登录态 JWT，这不是评估的阻塞。
- 真实阻塞是**云端生成模型账户余额**：用 `builtin-smart-reasoning`（绑定 `deepseek-flash`）执行时，Core 日志记录 `API request failed with status 402: Insufficient Balance`，Agent 在第 1 轮思考即失败。同一时刻另有 `dial tcp: lookup api.deepseek.com: i/o timeout` 的网络抖动，两者都属外部条件。

## 工具

新增 `extensions/cmd/agentic-eval`：一个纯评估客户端，为每道问题创建独立会话、发起一次 `POST /api/v1/agent-chat/:session_id` 流式请求，并从服务端自己的 SSE 流中读取答案与工具调用序列。它不写任何治理状态，也不改生产配置。

报告字段：

| 字段 | 含义 |
|---|---|
| `rounds` | Agent 实际轮次数，按服务端 `tool_call` 帧自报的 `iteration` 去重计数（同一轮内的并行工具调用只算一轮） |
| `turns` | 工具调用总次数（与轮次不同：一次轮次可含多个工具调用） |
| `tool_sequence` | 按发生顺序的工具名序列 |
| `distinct_tools` | 本题用到的不同工具数 |
| `elapsed_ms` | 单题耗时 |

## 执行结果

### 第一轮（DeepSeek，云端模型）

15 道问题全部发起，14 道产出答案，1 道（Q01）在工具调用中途收到 error frame。指标：

- 工具调用总数 **376**，单题最多 58 次，最少 12 次，平均 25.1 次。
- 工具构成：`read_document` 162、`search_knowledge` 90、`list_documents` 50、`discover_mcp_tools` 42、`call_mcp_tool` 16、`mcp_agentrag_query_related_docs` 16。
- 每题用到的不同工具数 3–6，均包含 `search_knowledge` 与 `read_document`；13 道题触达 MCP 路径（`discover_mcp_tools` → `call_mcp_tool` → `mcp_agentrag_query_related_docs`），说明跨库关联查询工具在真实 Agent 运行中被实际调用。
- 单题耗时 10.9–79.0 秒。

该轮的口径缺陷：初次实现的 `turns` 统计的是工具调用数而非轮次数，直接与冻结架构「轮次上限 3–4」对比会得出错误结论。修正后需重跑，本轮数据不作为轮次结论。

### 第二轮（本地 Qwen3.5-9B，用于排除模型因素）

DeepSeek 余额耗尽后，改用本机模型 `Qwen3.5-9B`（supervisor 的 `chat` 实例，`http://127.0.0.1:8083/v1`）验证评估链路本身。因 `internal/handler/session/qa.go` 中 `modelID = agent.Config.ModelID` 优先于请求体的 `model_id`，请求级覆盖无效，故新建专用 Agent `07dd876d-2a27-431f-9f28-55fb8f4b2cf9`（`max_iterations: 4`、绑定本生产 KB）来固定模型。

3 道冒烟题结果：Q02 成功产出 994 字答案、4 次工具调用；Q01、Q03 收到 error frame。逐题失败原因在 Core 日志中各不相同——首轮为 `n_ctx 4096` 小于提示词加工具定义的 8376 tokens；将 `chat` 实例以 `ctx_size: 16384` 重启后，剩余失败转为云端路径的 402 与 DNS 超时。

该轮确认：**评估工具、多跳问题集、生产 Core 会话与 SSE 观测链路全部可用**，失败只来自外部条件（模型上下文配置与云端账户）。

## 轮次上限的现状核对

- `builtin-smart-reasoning` 实际 `max_iterations` 为 **50**（生产配置读回值），不是代码里 smart-reasoning 默认的 -1；`internal/types/custom_agent.go` 的 -1 分支只在 `MaxIterations == 0` 时被 smart-reasoning 预设改写，而内置 Agent 落库时已带 50。
- 冻结架构要求轮次上限 3–4。当前生产内置 Agent 的 50 与该要求不一致，这是一处**待决策的对齐项**，不是本轮可自行改动的配置。
- 本轮工具调用数最多 58 次，出现在单题多轮检索场景；工具调用数与轮次数是两个不同量纲，不能相互替代。

## 边界

- 本文件证明评估工具链、问题集、生产会话与工具序列观测已跑通；**不证明 15 道题的答案正确性**：冻结架构要求的人工评分尚未进行，工具序列不等于答案正确。
- 本轮未做人工评分，未计算 Recall/MRR 等答案质量指标。
- 生产环境未注入任何故障；DeepSeek 402 与 DNS 超时都是外部条件，不构成对治理链路的验证证据。
- 生产数据未被修改；新建了一个专用评估 Agent（`07dd876d-2a27-431f-9f28-55fb8f4b2cf9`），可删除。
- 本地 `chat` 模型实例在评估后已停止（仅为无网时的备用模型，平时使用 DeepSeek API）；GPU 显存回到 2367 MiB。

## 复现材料

- `extensions/cmd/agentic-eval/main.go`（评估工具）
- `tmp/agentic-eval-manifest.json`（15 道问题及期望文档）
- `tmp/agentic-eval-report.json`（第一轮 DeepSeek 报告）
- `tmp/agentic-eval-smoke-report.json`（第二轮本地模型冒烟报告）
