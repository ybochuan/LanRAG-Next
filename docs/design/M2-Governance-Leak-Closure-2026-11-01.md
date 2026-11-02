# M2 模型来源泄漏修复与验收

日期：2026-11-01。源码：`fa6353e8c821daa5eeb667dc46140e4d0d32f179`，`v0.9.0-2-gfa6353e`；工作区包含此前M1/M2未提交改动，本报告仅说明本轮增量。Frozen Architecture未改写，根go.mod/go.sum未变。

## 结论与补丁范围

指定链路验收通过：真实Core/Host、原生上传与索引、Observer初始化active、日期扫描变stale、普通RAG及Agent search/read/list、同会话历史/已知文档ID，以及Host断开。模型为本地可捕获全部输入的协议夹具，不冒充DeepSeek/MinerU联合验收。真实账号的官方API认证单独验证过，用户原有文档未用于此隔离测试，也未修改其治理状态。

| 入口 | 文件／方法 | 本轮行为 |
|---|---|---|
| 共享文档许可 | `internal/extensionbridge/documents.go`：AllowedDocuments / AllowedDocumentsInScope | 复用before_retrieve固定回环500ms客户端；保留原授权KB/doc范围、非空session/trace及响应完整分割校验。无状态缓存/DB迁移/扩展依赖。 |
| RAG空检索回退 | `internal/application/service/session_knowledge_qa.go`：buildKBDocumentListing / renderFallbackPrompt | 只从原授权文档候选中过滤active后拼接title/description；错误不进入动态fallback模型。 |
| RAG最终输入与数据分析 | `chat_pipeline/governance.go`、`into_chat_message.go`、`data_analysis.go` | 正文/描述进入最终prompt或提前的数据分析判断前再校验，保留原生WebSearch类型；不改变检索/RRF/分块算法。 |
| Agent搜索 | `internal/agent/tools/search_knowledge.go`：Execute | 原生检索候选在deduplicate/rerank/MMR/工具返回之前按原scope和Host许可过滤；拒绝内容不发送重排模型。 |
| Agent直接读／列举 | `read_document.go`、`list_documents.go`、`data_schema.go`、`data_analysis.go` | 原生身份/scope解析后校验；文档及旧chunk句柄一样约束，先校验再读块/邻块/文件。列表逐页过滤，不重写原生全局分页算法。 |
| Agent预置元数据 | `internal/application/service/{governance.go,agent_service.go}` | selected-doc metadata过滤；KB聚合description/profile和自动recent-doc枚举省略，静态KB ID/name/type/capability保留；自动页面上下文省略。 |
| 原生MCP | `internal/mcpserver/{tools_retrieve.go,tools_wiki.go}` | 独立文档列表先过滤再序列化；省略KB聚合描述；搜索/直接读复用共享工具，Wiki索引安全拒绝。 |

表中未写全前缀的工具路径均在`internal/agent/tools/`，管线路径在`internal/application/service/chat_pipeline/`。具体native变动可用 `git diff -- internal/` 审阅，其中SearchParallel/parser/SSRF等补丁已在本轮开始前存在。新增文档适配器和governance测试文件尚为untracked，审阅时应一并查看，不能只看tracked diff。

## 冲突点与用户批准的处置

| 编号 | 当前原生设计冲突 | M2处置／后续建议 |
|---|---|---|
| C-21 | KB profile/description、recent docs和FAQ聚合文本没有完整逐文档来源；只过滤chunk不能控制它们。 | 模型上下文省略聚合文本及KB-wide recent-doc枚举；UI和存储保留。将来由带来源的summary重新构建后恢复。 |
| C-22 | 历史回答、reasoning、compaction/checkpoint、resume、Wiki混合页/邻页摘要、图谱关系没有完整可验证来源。 | 用户明确批准安全拒绝。RAG不重放旧历史且标记HistoryLoaded；Agent不加载历史，恢复在读取快照之前拒绝。Wiki各工具及其确认前预读、图谱读取、任意SQL/会话检索/通用操作、不透明外部MCP结果暂拒绝。数据不删除。 |
| C-23 | 查询改写的“常查资料”来源于旧答案引用的document affinity，早于SearchParallel；页面read/fill/select工具能自动读取屏幕上的Wiki或文档。 | memoryBackground在读取记忆之前省略；read_page/fill_page/select_tab在PageActor之前拒绝。测试覆盖标题标记和无依赖入口拒绝。 |
| C-24／Baseline Limitation | 已签发资源权限、显式复制到workspace/resource/文件/记忆的内容无完整原文档血缘；原生REST管理与模型访问不是同一边界。 | 不宣称这些副本已撤回。审计未发现原生Knowledge自动复制到持久workspace的路径；data_analysis使用临时文件并清理，但用户/模型可主动保存副本。后续补来源与资源撤销设计，禁止靠重写权限框架或删除用户数据硬凑。 |
| C-25／Baseline Limitation | 当前校验是读取/拼接时的许可快照；生成已开始后，生命周期变更不会撤回已发送的token或Agent本轮已读内容。 | 未来定义许可版本、租约/取消和撤销SLA；本轮不声称持续撤销。模型自身知识、用户粘贴内容和独立active副本也不属于原文档级过滤。 |
| C-26／本机部署冲突，已绕开 | Windows TCP排除范围7991–8090覆盖8080/8090；WSL内服务200，Windows本机转发失败。获准重启WSL后范围仍存在。 | 不删除系统保留范围；Core改18880并同步Host的Core URL，私密配置已保存。Bridge保持WSL回环8090/500ms。Windows UI/健康/官方登录/登录后知识库API全部200。 |

本轮是来源适配与安全拒绝，没有实现新的after_retrieve/before_generate协议，也没有把状态机搬进原生。无完整DTO来源时的拒绝不是在核心复制生命周期规则。

## 自动化验证

以下均为本轮实际命令。原生命令在WSL Ubuntu、仓库根执行，Go位于`/usr/local/go/bin`；扩展命令在`extensions/`执行。记录不包含账号密码、token、配置或用户文档。

| 检查 | 命令／范围 | 退出状态 |
|---|---|---|
| 原生定向及race | `CGO_ENABLED=1 go test -mod=readonly -race -tags sqlite_fts5 ./internal/agent/tools ./internal/application/service ./internal/application/service/chat_pipeline ./internal/agent ./internal/mcpserver ./internal/extensionbridge -run 'TestGovernance\|TestAllowedDocuments\|TestMCPSearchValidKnowledgeBaseSelectors\|TestMCPExposureWithoutMentionReachesProviderAndExecutes\|TestQuickAnswerDataAnalysisUsesWorkspaceStorage' -count=1` | 0，六包通过。 |
| 原生静态检查 | `CGO_ENABLED=1 go vet -mod=readonly -tags sqlite_fts5` 加上述六包 | 0。 |
| Lite构建 | `CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' -o tmp/governance-WeKnora-lite ./cmd/server` | 0。无根依赖修改。 |
| 扩展检查 | `go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、`go test -mod=readonly ./... -count=1`、`go test -mod=readonly -race ./... -count=1` | 均0。独立模块GOMOD正确、GOWORK为空。 |
| 模块边界 | `python extensions/scripts/check_module_boundary.py` | 0，无WeKnora依赖/workspace。 |
| Python回归 | `python -m unittest discover -s extensions/scripts -p '*_test.py' -v` | 0，33项，Windows平台跳过3项；Linux真实E2E另行执行。 |
| 真实隔离E2E | 下方命令 | 0，完整脱敏输出见证据日志。 |
| 六个原生包全量 | `CGO_ENABLED=1 go test -mod=readonly -tags sqlite_fts5` 加上述六包和`-count=1` | 1；chat_pipeline/agent/mcpserver/extensionbridge通过，tools与service存在Python恢复、MCP/Sandbox配置相关失败。 |

针对三个代表性失败，用Go `-overlay`将本轮修改文件恢复到HEAD、移除本轮新增源码/测试，并保留原有M1/M2补丁，仍复现 `TestSkillPythonPackageRecoveryInstallsWithoutPip`、`TestUpdateMCPService_AppliesNonScalarUpdateWithoutName`、`TestQueryTemplatesEnsuresMissingWeKnoraTemplate`，退出1。其他全量失败未逐项归因，不能笼统宣称全部都是基线。未运行根`go test ./...`、前端全量、生产云模型或真实MinerU联合测试。

```bash
# 不占用用户的8090，完整服务在私有网络空间内运行
wsl -d Ubuntu --exec unshare -Urn bash -lc 'ip link set lo up; export PATH=/usr/local/go/bin:$PATH; cd /mnt/d/LanRAG-Next && python3 extensions/scripts/governance_e2e.py'
```

脚本`extensions/scripts/governance_e2e.py`使用真实原生API创建隔离测试账号/KB/模型/会话、上传并索引TXT。正文BODYONLY、描述DESCONLY、旧答案ANSWERONLY彼此独立；RAG和Agent正向答案都实际植入旧答案标记。Agent正向还要求角色为tool的真实返回含相应标记，防止预置metadata造成假阳性。stale由测试标注日期驱动真实Host扫描，不直接改current/event。随后复用会话与已知ID，检查所有模型请求（包含查询改写）及重排输入。断Host时允许原生先做只含当前问题的改写，要求答案生成次数为0并出现原门卫日志。

完整脱敏终端输出：[m2-governance-leak-e2e-2026-11-01.log](../evidence/m2-governance-leak-e2e-2026-11-01.log)。请求/模型输入的原始诊断保存在忽略的私密tmp目录，不提交。

指定原生源码补丁：[m2-governance-sources-2026-11-01.patch](../evidence/m2-governance-sources-2026-11-01.patch)，包含本轮29个tracked原生文件与14个新增适配/测试文件，不包含之前的M1/SSRF/SearchParallel改动。四个此前全量通过的包（chat_pipeline/agent/mcpserver/extensionbridge）在最终记忆/页面修复后再次完整运行，均退出0。

## 独立复审与后续

独立只读复审发现并推动修复了旧答案亲和标题、页面快照和E2E旧答案正向标记缺口；修复后对指定路径收束为无剩余阻断问题。未将普通管理REST、资源授权、主动复制副本或持续撤销作为完成项。

M2整体仍有配置鉴权/UI、完整来源链、资源撤销及原生全量失败等门槛；本次仅验收上述指定来源治理链路。部署运行状态另记证据，不作为永久项目事实。
