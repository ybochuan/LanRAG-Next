# query_related_docs 启用及验收

Host worker现在提供`http://127.0.0.1:8090/mcp`，Streamable HTTP、无状态JSON响应；沿用`AGENTRAG_MANAGEMENT_KEY` Bearer鉴权。只提供只读工具`query_related_docs(document_id)`，不启用stdio，不修改原生ToolRegistry发现算法。

## 实现差异

当前schema8的`ext_lineage_edge.edge_type`不支持`attachment_of`，且阶段一明确没有写附件边。本批不改已有迁移：补充通知查询现有`supplements`边及其当前声明，附件复用`is_attachment/parent_document_id` Metadata和既有下载策略。撤销的旧声明和旧版本边不返回。需要真正的attachment_of事实时另行设计迁移，不将Metadata冒充血缘。

查询主文档和返回文档须通过当前active许可；附件继续要求父active。返回`supplements`、`attachments`及供现有聊天卡片使用的`related_attachments`。原生每次调用注入自己的会话授权范围，覆盖模型参数；该范围不入库、不改变MCP目录指纹。整库或明确文档范围可用，未解析的标签授权不会扩大为整库。

## 启用

1. 重新构建/启动本批Core、Host和前端，沿用原启动指南；Core与Host的`AGENTRAG_MANAGEMENT_KEY`必须相同且至少32字节。保持本机模型/MinerU配置。候选Core构建命令：

```bash
CGO_ENABLED=1 go build -mod=readonly -tags=lite,sqlite_fts5 \
  -ldflags='-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' \
  -o tmp/WeKnora-lite-related-mcp ./cmd/server
(cd extensions && go build -mod=readonly -o ../tmp/extension-host-related-mcp ./cmd/extension-host)
```

需要anydoc的部署保留既有anydoc标签。候选构建不会自动覆盖生产二进制。前端按既有流程type-check/build并部署，运维入口见[启动指引](AgentRAG-启动与运维指南.md)。

2. 注册连接。一键启动已经接管这一步：首次执行一次

```bash
cd /mnt/d/LanRAG-Next
./start-all.sh mcp-key     # 交互输入一次原生登录；签发专用 full-access key 存入 0600 私有状态
```

之后每次 `start.cmd` 都会幂等地创建/更新名为 `AgentRAG`、URL 为 `http://127.0.0.1:8090/mcp` 的连接并刷新工具目录；注册失败只告警，不影响启动。`mcp-key` 只需一次，密码不保存。

手工/旧路线仍可用（需要管理员 JWT，不要把值写进仓库）：

```bash
export AGENTRAG_WEKNORA_URL=http://127.0.0.1:18880
export AGENTRAG_CORE_JWT='<管理员 JWT>'
python3 extensions/scripts/register_related_mcp.py
```

脚本优先用私有状态里的专用 key（`X-API-Key` + `X-Tenant-ID`），没有才回退到 `AGENTRAG_CORE_JWT`；不会输出凭据。也可在原生 MCP 设置手工创建：传输 `http-streamable`，Header `Authorization: Bearer <已有共享密钥>`，刷新目录后应看到唯一工具 `query_related_docs`。保留现有本地 SSRF 地址配置；本批不另行关闭 SSRF。

3. 在智能体设置启用该MCP服务（MCP选择“全部”或显式选中AgentRAG，不能是“禁用”），确保该工具启用；沿用原生审批/只读权限。无需把动态MCP工具名手工写进allowed_tools。

## 人工验收

- 准备一个active制度及其已提取附件。验证补充通知时，在生命周期页通过既有T3功能声明同库通知的父制度，确保通知也active。
- 打开新会话、开启Agent模式，选中该知识库或明确主文档，询问：**“请说明这项制度的办理要求，给我需要使用的登记表，并核对有没有补充通知。”** 无需在用户提问中指定工具名。
- 系统提示词已引导调用；预期先出现原生`discover_mcp_tools`的发现/描述动作，再出现包含`query_related_docs`的工具调用。实际注册名带服务前缀/hash，以发现结果为准。
- 工具结果中查看`supplements`与`attachments`；回答下方应出现关联文件下载卡片，附件下载沿用现有登录和父状态校验。
- 将通知变为stale后重新提问，通知应不再返回；主制度过期后调用应报告不可用。停Host后不得返回旧数据；其他服务上同名工具或本服务上的未知工具仍不能利用该白名单。

模型是否选择调用属于实际模型验收，不能用提示词存在代替；本轮没有调用真实云模型、注册生产连接或替换生产服务。旧/files/签名资源、已输出答案及完整来源封存不随白名单自动恢复。

## 本轮验证

- 扩展完整`go test -mod=readonly -p 1 ./... -count=1`、build/vet退出0。
- `./mcp`一个集成测试实际启动HTTP服务，覆盖initialize、tools/list、鉴权、附件查询、会话越界、父stale拒绝；未以此宣称真实补充通知数据或模型E2E已验证。
- 原生`go test -mod=readonly -tags=sqlite_fts5 ./internal/agent/tools ./internal/agent ./internal/application/service -run MCP -count=1`退出0；另有本机来源/精确工具名白名单及运行范围不持久化测试通过。
- Core构建退出0，前端relatedAttachments四项定向测试、type-check退出0。模块门禁295 imports通过，根依赖及迁移未改。
