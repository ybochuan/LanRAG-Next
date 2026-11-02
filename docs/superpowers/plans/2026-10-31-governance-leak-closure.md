# M2 治理泄漏封堵计划

用户已授权立即修复摘要与Agent工具绕过，并接受对无完整来源的历史/恢复/Wiki/图谱等内容暂时安全拒绝；不删除原始存储。

## 已确认入口与冲突

- RAG：`session_knowledge_qa.go buildKBDocumentListing`在空检索兜底中枚举文档Description；独立于SearchParallel。
- Agent：`search_knowledge.Execute`独立HybridSearch；`read_document`仅检查原生访问scope；`list_documents`直接输出description；data_schema/data_analysis能读取表格内容。
- Agent预置metadata：`agent_service.go getKnowledgeBaseInfos/getSelectedDocumentInfos`注入recent docs/FAQ及KB聚合profile。C-21：聚合profile无完整逐文档来源，M2不放入模型上下文，保留UI和事实存储。
- C-22：旧答案/推理/compaction/checkpoint/resume、Wiki混合页/邻页摘要、graph关系缺完整来源。用户批准本阶段拒绝此类回放/读取，后续具备完整来源后再恢复。
- 其他同类工具：search_conversations、database_query、管理operation知识读取、MCP原生list/wiki_index，不能遗漏。外部MCP/任意网络/用户自行提供文本与模型先验须单列边界，不能声称由文档ID治理消除。

## 实施与验证

1. 先写失败测试：真实文档读取不得在无Host许可时列chunk；RAG兜底不得把未知状态description送给模型。
2. `internal/extensionbridge/`添加纯DTO适配：按原生授权候选请求现有before_retrieve，固定回环500ms，严格验证响应，绝不引入环境绕过。状态判断继续在extensions持久策略。
3. RAG：兜底listing受Host和原生文档范围双重限制，HTTP失败向上传递；into_chat_message在格式化描述、正文、FAQ和图片前再校验最终结果，防止后续拼接重引入。
4. Agent：search在原生有权限候选形成后校验，结果在rerank/图像处理前过滤；不复制全库枚举/分页或改检索算法，Host失败不得产生工具数据输出；read/doc或chunk旧handle先解析owner后校验；list/schema/data读前或模型输出前校验。未知来源读取在各工具入口拒绝，不重构Registry/检索/SQL算法。
5. Prompt metadata只保留本轮原生授权且Host允许的文档；KB聚合文本省略。历史与恢复在任何LLM/工具重放前安全拒绝或不装载，不能只删除引用。
6. 测试显式注入策略夹具以隔离原生算法单测；真实协议、Host故障和核心进程E2E另行覆盖。必须分别测试active/非active/unknown、超时/503/畸形响应、旧chunk句柄、越权KB、模型实际输入及外部rerank输入。
7. 使用获准账号仅经官方API登录；E2E在隔离副本/测试数据中执行，不更改用户现有文档治理状态，凭据不落代码/日志。
8. 根模块定向Go测试、build/vet及适当race；扩展模块独立边界回归；检查git diff范围并更新PATCHES/上下文/backlog/验收证据。保留M2未完事项，未运行项如实说明。

本计划不改变原生RRF、分块、排序和任务调度算法；每个核心入口只做范围DTO转换、HTTP许可与结果过滤/拒绝。
