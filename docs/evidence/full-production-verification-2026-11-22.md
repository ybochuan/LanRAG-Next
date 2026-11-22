# 真实生产环境全功能验证报告（2026-11-22）

## 1. 验证范围与环境

- **环境**：真实生产栈（非隔离环境）。WSL Ubuntu 镜像网络；Windows 宿主机与 WSL 均可访问。
- **代码版本**：HEAD `63fda66`，`git describe v0.9.0-128-g63fda66`，分支 `codex/m1-parser-routing`。
- **生产知识库（实测）**：`df5f5dfa-6f33-48fb-8768-666fa65c839e`（名称：生产验收知识库（部署验证），tenant_id=1）。
  - 说明：历史验收文档中的 `164bfd73-d231-4d5c-994e-057ac0cdf48a` 在当前运行环境中不存在；本次以实际存在的 KB 为准。
- **在库文档**：1 篇 active 文档 `2f3ded15-e359-4680-80ae-717e5c965c73`（星桥项目运维须知.md）。
- **扩展库版本族**：`ext_document_version_family`=1，`ext_document_version`=1（原生版本族已登记）。
- **对话模型**：`deepseek-flash`（id=b66e93c8-b3c6-4cd6-852a-f1e1aa9cc4de），`parameters.base_url=https://api.deepseek.com/v1`，`capabilities.provider=deepseek`，`api=openai-completions`，`is_default=true`，`default_for=[KnowledgeQA, Utility]`，`credentials.api_key.configured=true`。

## 2. 本次唯一源码改动

- 文件：`start-all.sh:281`，函数 `start_core()`。
- 改动：`if [[ -n "$HOST_BINARY" ]]; then` → `if [[ -n "$release_revision" ]]; then`。
- 原因：正常开发模式下 `HOST_BINARY` 会被设置为新编译的 extension-host（约 :310），导致 :282 在 `set -Eeuo pipefail` 下引用未定义的 `release_dir` 并退出；`release_revision` 仅在发布构建时非空，dev 模式的 `WEKNORA_WEB_DIR` 由 private.env 提供。
- 修复前现象：`start-all.sh: line 282: release_dir: unbound variable`、`wait_for: No record of process 1006`、`[ERROR] Core exited before readiness`。
- 会话内其他 55 个已修改/未跟踪文件均为用户既有改动，本次未改动。

## 3. 服务启动与验证方式

| 服务 | 端口 | 验证方式 | 结果 |
|---|---|---|---|
| MinerU API | 8000 | Windows netstat / WSL socket connect | PASS |
| Model supervisor | 8092 | Windows netstat / WSL socket connect | PASS |
| Embedding (Qwen3-Embedding-4B-Q6_K, dim=2560) | 8081 | socket + 启动日志能力校验 | PASS |
| Rerank (qwen3-reranker-0.6b-q8_0) | 8082 | socket + 启动日志能力校验 | PASS |
| docreader (Docker) | 50051 | WSL socket + 容器已运行 | PASS |
| Extension Host | 8090 | `GET /bridge/healthz` → `{"status":"ok"}` | PASS |
| Core (WeKnora-lite) | 18880 | `GET /api/v1/models` 等管理 API | PASS |

- 启动命令：`cd /d/LanRAG-Next && cmd.exe /c start.cmd`（后台任务 `bash-86e8a54740ed46b2`）。
- 启动日志目录：`/mnt/d/LanRAG-Next/data/launcher-run-0pJmW4`。
- **注意**：该前台任务保持运行；终止它会连带停止 Windows 侧 MinerU/supervisor/embedding/rerank/docreader。

## 4. 全功能验证清单

验证方法分四类：① 端口/进程探针；② 原生登录 JWT 调 Core API；③ 管理密钥/X-API-Key 调 Host 与只读 API；④ 浏览器加载与真实 SSE 问答。

### 4.1 基础与基础设施

| # | 检查项 | 方法 | 结果 |
|---|---|---|---|
| 1 | 七个服务全部监听 | socket connect + netstat | PASS (7/7) |
| 2 | Docker docreader 容器运行 | 启动日志 + 端口 | PASS |
| 3 | Host 健康 `/bridge/healthz` | curl | PASS |
| 4 | 原生登录 `POST /api/v1/auth/login` | Python urllib | PASS（顶层 token 字段） |
| 5 | 机器 API Key 作用域 | `X-API-Key` + `X-Tenant-ID` | PASS（仅只读 API；Bearer 会 401；不能调管理接口） |
| 6 | 管理接口鉴权 | Bearer JWT | PASS |

### 4.2 模型与对话（云端 DeepSeek）

| # | 检查项 | 方法 | 结果 |
|---|---|---|---|
| 7 | 模型清单（4 个） | `GET /api/v1/models` | PASS |
| 8 | DeepSeek 默认/配置 | 模型详情 `parameters`/`capabilities` | PASS |
| 9 | Agent 绑定 | `GET /api/v1/agents`：builtin-quick-answer / builtin-smart-reasoning 均绑定 deepseek-flash | PASS |
| 10 | **真实云端生成连通** | `POST /api/v1/knowledge-chat/{session}` SSE，收到 thinking/answer 事件；Core 日志 `[LLM Usage] model=deepseek-flash purpose=knowledge_qa` | PASS（无 402 余额不足、无 DNS 超时） |
| 11 | **M2 治理下的 Agent 问答** | `POST /api/v1/agent-chat/{session}`，正确 KB；78 SSE 事件，`query_understand`+`knowledge_search` 成功、命中 1 个 chunk、`references` 事件带 `<kb doc=...>` 引用，`correction_check=applied` | PASS |
| 12 | 错误 KB 作用域拒绝 | 传入不存在的历史 KB id → `before_retrieve returned HTTP 403`，fail-closed、不回退 whole-KB | PASS（符合冻结方案） |

> 实测答复示例：`【已审核勘误】星桥项目变更审批需提前两个工作日，由值班负责人林澈审批；部署窗口为每月第二个周三 02:00–04:00（UTC+8）……回滚保留：最近三个发布版本。` 并附 `<kb doc="星桥项目运维须知" chunk_id="..." kb_id="..." />` 引用。

### 4.3 知识库与检索

| # | 检查项 | 方法 | 结果 |
|---|---|---|---|
| 13 | KB 清单 | `GET /api/v1/knowledge-bases` | PASS（1 个 KB） |
| 14 | 文档清单 | `GET /api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=50` | PASS（1 篇 active，parse_status=completed） |
| 15 | 混合检索 | `POST /api/v1/knowledge-bases/{kb}/hybrid-search`，body 用 `query_text` | PASS（命中 1） |
| 16 | FAQ 检索 | `POST .../faq/search` | 不适用：当前 KB 非 FAQ 库，原生返回 400「仅 FAQ 知识库支持该操作」（预期行为） |
| 17 | `/schema` 路径 | GET | 该路径在当前版本不存在（404），非产品缺陷 |

### 4.4 M4/M5 业务与生命周期（只读）

| # | 检查项 | 方法 | 结果 |
|---|---|---|---|
| 18 | relation_list | `GET .../lifecycle/business/relation_list` | PASS（200，0 条） |
| 19 | feedback_list | `GET .../feedback_list` | PASS（200，0 条） |
| 20 | guidance_list | `GET .../guidance_list` | PASS（200，0 条） |
| 21 | revision_list | `GET .../revision_list` | PASS（200，0 条） |
| 22 | related 关系查询 | `GET .../related?document_id=...` | PASS（200，0 条） |
| 23 | 生命周期 current | 扩展库 `ext_lifecycle_current`：1 行 state=active | PASS |
| 24 | 生命周期 events | 扩展库 `ext_lifecycle_events`：2 行（initialize→parse_completed） | PASS |
| 25 | 版本族 | `ext_document_version_family`=1，`ext_document_version`=1 | PASS |
| 26 | 业务族 | `family_list` 返回空（生产库尚未创建业务族） | 信息项 |

### 4.5 MCP 与桌面/Web 程序

| # | 检查项 | 方法 | 结果 |
|---|---|---|---|
| 27 | MCP 注册 | `python3 extensions/scripts/register_related_mcp.py` | PASS：`[MCP] AgentRAG registered; tool directory refreshed` |
| 28 | MCP 服务目录 | `GET /api/v1/mcp-services` | PASS：1 项，id=c518d652-0fe6-43d2-8e1c-2225abc170a6，name=AgentRAG，url=http://127.0.0.1:8090/mcp |
| 29 | Agent 挂载 MCP | `GET /api/v1/agents`：builtin-smart-reasoning `mcp_services=[c518d652...]` | PASS |
| 30 | Web UI 加载 | 浏览器打开 `http://127.0.0.1:18880/` → 重定向 `/login`，WeKnora 登录页正常渲染（中文、邮箱/密码框、登录按钮） | PASS |
| 31 | 登录后的业务页面 | 通用浏览器驱动拒绝填写密码字段（`CREDENTIAL_ENTRY_REJECTED`），未做 UI 内登录 | **未验证**（见第 6 节） |

## 5. 复现命令（不含任何密钥值）

```bash
# 启动
cd /d/LanRAG-Next && cmd.exe /c start.cmd
wsl.exe -d Ubuntu -- /mnt/d/LanRAG-Next/start-all.sh status

# 登录取 JWT（凭据只从本地私有文件读取，不在输出中显示）
wsl.exe -d Ubuntu -- python3 /mnt/d/LanRAG-Next/tmp/login_probe.py

# 全量 API 检查
wsl.exe -d Ubuntu -- python3 /mnt/d/LanRAG-Next/tmp/prod_verify_full.py

# 真实 Agent 问答
wsl.exe -d Ubuntu -- python3 /mnt/d/LanRAG-Next/tmp/agent_chat_probe2.py

# MCP 注册
cd /mnt/d/LanRAG-Next && export AGENTRAG_CORE_JWT="$(cat /tmp/agentrag-remediation-jwt)" \
  && python3 extensions/scripts/register_related_mcp.py
```

> 本次生成的检查脚本位于 `tmp/login_probe.py`、`tmp/chat_probe.py`、`tmp/agent_chat_probe2.py`、`tmp/prod_verify_full.py`、`tmp/verify_extra.py`、`tmp/kb_probe.py`；其中 `tmp/core-jwt.sh` 为本地测试账号凭据文件，禁止输出其内容。

## 6. 限制与未验证项

1. **UI 登录后的页面级核对未完成**：通用浏览器驱动明确拒绝密码字段（`password entry is not accepted by the generic browser driver`）。已通过 API 与 SSE 完整验证相同功能；若需 UI 内逐页点击核对，需人工登录或允许凭据输入。
2. **生产数据规模**：当前生产 KB 仅 1 篇 active 文档、0 条业务关系/反馈/指导/修订记录。历史验收文档所述的 75 篇文档与 33 对关系不在当前运行实例中，因此多关系、多版本族场景无法在真实数据上复验。
3. **只读原则**：M4/M5 写入动作（relation_declare、feedback_*、revision_* 等）会修改真实生产数据，本次未执行；其实现与门禁由既有测试证据和只读端点覆盖。
4. **上一轮 402 问题已消失**：本次云端 DeepSeek 实际生成成功，未复现余额不足/DNS 超时。
5. **启动脚本修复需纳入版本管理**：本轮 `start-all.sh` 的修复为未提交改动，部署其他实例时需同步。

## 7. 结论

真实生产栈 7/7 服务启动并保持健康；云端 DeepSeek 作为默认对话模型已完成真实 RAG 生成与 Agent 工具链验证；M2 来源门禁在错误 KB 作用域下正确 fail-closed、在正确作用域下放行并给出可追溯引用；MCP 注册与 Agent 挂载完成；Web UI 可加载。核心功能均已验证通过，剩余限制已在第 6 节如实列明。
