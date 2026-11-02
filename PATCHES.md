# 原生薄补丁记录

## M4/M5 业务管理薄接线（2026-11-02）

- 授权：用户2026-11-02明确要求尽快接线和推进M4/M5，沿用Frozen与受控区纪律；业务实现位于独立extensions，无新依赖。
- `internal/router/routes_knowledge.go`新增GET/PUT `/:id/lifecycle/business/:business_action`；沿用Viewer/KB读和OwnedKBOrAdmin/KB写守卫。
- `internal/handler/extension_lifecycle.go`只识别读/写动作、限制请求体、解析DTO；JWT身份/actor/KB权限由原生解析，不接受浏览器伪造；GET不能执行发布，API Key继续拒绝。
- `internal/extensionbridge/management.go`仅新增独立Business原始JSON字段；沿用固定回环、版本1、服务端密钥、2s管理超时和scope响应校验。未改检索、RRF、解析、Agent、调度或原生表。
- `extensions/management/business.go`和`host/runtime.go`对接真实Registry/业务服务与原生公开manual创建API；业务状态、租约、审计、重试、remote reconcile和事务均在`extensions/business/`。
- 扩展`lifecycle/store.go`提取`AppendTx`复用原event/current/outbox实现，让修订finalize与治理审计同事务，属于扩展内复用，不是原生业务重构。
- 007/008及根go.mod/go.sum保留。family投影处理、MCP消费、真实QA反馈/指导注入、binary正式文件与前端未在本批声称完成。完整记录见`docs/evidence/m4-m5-business-2026-11-02.md`。

## M3-T4 第一批（2026-11-02，未完成整体接线）

- 用户已批准T4整批薄点与未知来源拒绝，清单见 `docs/design/M3-T4-Native-Integration-2026-11-01.md`。本批不修改原生业务表、合并/RRF、工具注册器、解析或调度算法。
- `internal/handler/extension_source_snapshot.go` 与新 `extension_source_components.go`：同一授权只读事务内提供显式可选组件视图，默认v1/root兼容，完整字段hash绑定原leaf，非空/唯一/64个上限，未知chunk拒绝，无原文返回。
- 新 `internal/extensionbridge/source_run.go`、`source_copy.go`：固定127.0.0.1:8090、500ms、版本1、无代理/重定向，强服务端凭据、trace/scope及绑定检查。只做DTO、hash、HTTP与请求内身份传递；普通QA的有限文本输入/模型步骤已接线，**实际文件生产者及可读副本正向闭环仍未接通**。
- `service/session_{knowledge,agent}_qa.go`、`chat_pipeline/{governance,chat_completion,chat_completion_stream}.go`、`agent/{engine,think,act}.go`：原生scope/owner/服务端message身份建run，实际消息/选项和原始响应hash登记，独立模型/工具步骤；RAG流消费终态、Agent真正返回后收束，异常/取消中止。未适配变换/改写/重排保持unattributed，文件写入未有producer时不能封存。
- `tools/{governance,read_document,search_knowledge,list_documents}.go`：返回字段薄DTO核验；原始内容/标题/描述等匹配当前完整root。未知FAQ/图像/自定义格式、listing updated_at及重排输入不签完整证明，保留原检索/格式化算法与M2许可。
- `service/agent_service.go`：未知外部MCP在注册/读取pin元数据前省略，避免服务摘要或schema先进入模型；现有ToolRegistry和按需发现算法未改，内置知识工具保留。
- `tools/{shell_exec,shell_exec_async,search_memory,sandbox_ls,web_fetch,read_web_page,read_file,read_artifact,workspace_reader,sandbox_write,sandbox_edit}.go`：未知来源执行/读取拒绝，分页缓存也拒绝；未知追加/编辑不读取或改写旧内容。覆盖写入仍不是可读来源证明。附件读取保留原生独立授权。
- `tools/work.go`：未知详情用cheap status且不传增量读取run ID；只保留原授权ID/状态及取消，省略标题/输出/文件，未展示内容不MarkSeen；复制结果行，不修改后台存储。
- `agent/{background,engine,act}.go`、`service/{session_agent_qa,agent_service,agent_artifact_files}.go`、`chat_pipeline/memory_recall.go`：自动memory/skill元数据省略，后台未知文本不读不ACK；未知工具图片在describeToolImages之前拒绝。持久产物保留原会话/owner查询后拒绝未知模型读取，不影响人工下载。
- 治理与持久化均在 `extensions/provenance/`；worker生产provider由真实Registry/组件核验及持久Runs组成。核验中意向、源并集、步骤/producer及回执审计有原子约束，非active/T3依赖每次重查。根go.mod/go.sum及007/008未变。
- 已验证与未验证项目、并行测试基线失败、后续模型/实际写入/资源别名与E2E接线见 `docs/evidence/m3-t4-progress-2026-11-02.md`。本批没有替换生产服务或迁移用户库，T4/M2未标完成。

## M2-GOVERNANCE-SOURCES（2026-11-01）

- 授权：用户要求修复RAG摘要、Agent搜索/直接读绕过，并明确接受无完整来源的历史、Wiki/图谱读取暂时安全拒绝。
- 适配器：新增 `internal/extensionbridge/documents.go`，复用现有严格before_retrieve客户端；包含tenant/KB/doc授权范围和非空session/trace关联，固定回环500ms，错误拒绝，不新增生命周期规则或状态缓存。
- RAG：`session_knowledge_qa.go`在fallback文档元数据注入前校验；`chat_pipeline/{governance.go,into_chat_message.go,data_analysis.go}`在最终拼接及数据分析前过滤；`query_understand.go`省略无来源的旧答案文档亲和背景。保留原生检索/分块/RRF算法及WebSearch结果。
- Agent：`tools/{governance.go,search_knowledge.go,read_document.go,list_documents.go,data_schema.go,data_analysis.go}`保留原生scope检查并追加实时许可；旧chunk句柄同样检查。`agent_service.go`过滤selected-doc metadata，省略KB聚合文本及自动页面上下文。
- 无来源拒绝：`session_agent_qa.go`、`agent/resume.go`、`tools/{database_query.go,search_conversations.go,query_knowledge_graph.go,mcp_tool.go,operations.go,page_tools.go,confirmed_tool.go}`及Wiki工具各Execute入口；Wiki确认前预读同样阻断。普通RAG关闭历史重放并设置HistoryLoaded，防止改写阶段重新装载旧消息。保存的会话不删除。
- 原生MCP：`mcpserver/tools_retrieve.go`过滤文档列表描述、省略KB聚合描述；`tools_wiki.go`拒绝无完整来源的Wiki索引。其文档搜索/读取复用共享工具。
- 限制与验证：见 `docs/design/M2-Governance-Leak-Closure-2026-11-01.md`。定向测试/race、CGO Lite构建、vet及真实Core+Host隔离上传/问答E2E通过；全原生包测试并非全绿，不能据此宣称M2整体完成。

## M2-LIFECYCLE-UI 原生管理薄接口（2026-11-01）

- 授权：用户选择“集成原生界面，授权薄接口并直接实施”。
- 新增 `internal/extensionbridge/management.go`、`internal/handler/extension_lifecycle.go`；`internal/router/routes_knowledge.go`登记六条JWT管理路由。沿用KB授权、写入归属与Editor检查；API Key无管理路由策略且handler再次拒绝。
- 固定回环 `127.0.0.1:8090/management/lifecycle`，独立2s超时、不使用代理/重定向；服务端管理凭据、Bridge版本1、响应scope绑定、请求64KiB/响应2MiB上限；异常归一503，409保留。
- 不改动检索、Agent、解析、分块、状态迁移算法及原生数据库。业务与持久化继续在独立extensions模块；根go.mod/go.sum未因本任务改变。
- 前端 `frontend/src/views/lifecycle/` 集成原生导航，不向浏览器下发Host凭据；权限和文档归属以服务端原生资源为准。
- 验证与限制：`docs/evidence/m2-lifecycle-ui-validation-2026-11-01.md`。

## SSRF-DISABLE（2026-10-31）

- 授权：用户要求本地单机模型接入可显式跳过SSRF防护，保留原安全代码。
- 文件：`internal/utils/security.go`、`internal/utils/ssrf_outbound_cache.go`；回归：`internal/utils/security_disable_test.go`。
- 配置：`SSRF_DISABLE=true`启用（忽略首尾空格与大小写）；未设置、false及其他值保持原检查。优先于SSRF_WHITELIST、SSRF_WHITELIST_EXTRA、SSRF_DNS_WHITELIST_ONLY。
- 覆盖：共享URL校验、白名单模式、HTTP出站缓存、重定向目标SSRF检查、共享TCP/gRPC/MySQL拨号。开启时不做安全DNS/IP/端口黑名单校验，普通连接仍正常解析域名；重定向次数/跨源凭据处理及HTTP协议约束保留。其他组件自行实现的独立限制不因本补丁被删除。
- 缓存：开启时在缓存读取/写入前直接放行，旧拒绝缓存不阻断，也不缓存新的绕过成功。
- 启动（需重新编译核心）：WSL/Bash `SSRF_DISABLE=true ./WeKnora-lite`；PowerShell先执行 `$env:SSRF_DISABLE="true"` 再启动核心。
- 验证：`go test -mod=readonly ./internal/utils -count=1`；Linux补充 `go test -mod=readonly -race ./internal/utils -count=1`，覆盖默认拒绝、开关放行、真实本机HTTP/TCP/重定向、旧拒绝缓存及恢复检查。
- 本轮实测：WSL定向SSRF/race、utils build/vet退出0；utils全包退出1，两项公网DNS用例因example.com解析到198.18.0.159而失败，未改动这些断言。完整本批diff见`docs/evidence/ssrf-disable-2026-10-31.patch`。

## M3-T1 原生只读来源快照（2026-11-01）

- 用户明确批准T1/T2：`docs/design/M3-Trusted-Sources-2026-11-01.md`。原生仅修改`internal/handler/knowledge.go`注入现有GORM DB、`internal/router/routes_knowledge.go`挂只读路由；新增`internal/handler/extension_source_snapshot.go`及测试。
- 路由沿用Viewer/KB read及handler内原生授权，不扩大机器凭据KB范围；同事务读取完整文档header/live chunks。SQLite一致读、PG显式repeatable-read（PG实服务未验）；只返回规范化指纹，不返回正文/标题/描述，不写原生表。
- 固定scheme v1；显式completed与ready、4096 chunks、hash输入64MiB、响应2MiB、查询500ms。正文加载前同事务检查数量/字节预算；未知/超限/错误拒绝，不能截断成功。
- 没有原生整文档generation：ext只保存已观察版本，每次恢复重新核对；不宣称原生不可漏历史、完整run封存或生成中撤销。独立module无根依赖改变；版本事实、审计及治理继续位于extensions。
- 真实隔离上传/Observer/快照/原生chunk编辑/旧证据拒绝E2E和边界测试见`docs/evidence/m3-trusted-sources-2026-11-01.md`。候选Core构建，无真实服务替换或用户库迁移。

## M2-T3 补充依赖管理薄接口（2026-11-01）

- 授权：用户明确“批准T3管理薄接线及扩展、界面联动”。原生生产补丁仅`internal/handler/extension_lifecycle.go`、`internal/extensionbridge/management.go`、`internal/router/routes_knowledge.go`，另补定向测试。
- 增加GET/PUT documents/:document_id/supplements；沿用原生JWT/KB读写权限，写前逐一核对child和parents的tenant/KB归属；服务端生成actor，拒绝浏览器tenant/actor/CanWrite、缺CAS、null父列表和过大请求。
- DTO增加parents/operation_id/actor_id，继续固定回环、2s管理请求、版本1、64KiB/2MiB和响应scope校验。2s为管理通道，before_retrieve原生500ms限制保持。
- 关系治理、版本核验、幂等/CAS、状态机、事务、重试、每日任务及提醒都在extensions；不改原生数据库或检索/RRF/Agent/解析/分块算法。界面嵌入生命周期详情，不下发Host凭据。
- 交付与验收：`docs/evidence/m2-supplements-2026-11-01.md`。候选Lite仅输出到tmp，生产服务/真实库未替换；完整run封存、副本门卫和生成中撤销仍为后续独立门禁。
