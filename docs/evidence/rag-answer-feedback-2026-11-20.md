---
title: 主流 RAG 问答反馈与 LanRAG-Next 的差异
file: rag-answer-feedback-20261120
lang: zh
cols: 2
---

Dify、Open WebUI 和 Amazon Q Business 的入口都围绕一条回答收集评价。RAGFlow 也先保存回答评价，并可基于引用分块调整后续检索排序。LanRAG-Next 的「评价与反馈」直接进入治理流程，要求回答来源能够核验，并用审核定稿指导后续生成。

## A 用户提交什么，反馈最后用在哪里 {span=2}

| 产品 | 提交对象与交互 | 提交后的用途 |
| --- | --- | --- |
| Dify 1.20.0 | 对本人在该应用中的回答消息赞、踩、留可选文字。API 以 `null` 撤回。 | 应用日志查看用户与团队评价。独立的 Annotation Reply 用审核者预写的答复处理相似问题。 |
| RAGFlow v1.0.0 | 在普通单聊中评价助手消息，公开分享页关闭该入口。服务端要求聊天读取和会话更新权限。 | 保存到消息记录。显式启用分块反馈后，调整引用分块的 PageRank 权重，供后续排序使用。 |
| Open WebUI | 对回复评分，按界面配置补充原因或文字。反馈关联聊天、消息、模型及上下文快照。 | 独立评价记录可供管理员查看、导出。模型比较反馈还可用于 Elo 排行。 |
| Amazon Q Business | 对单条回答赞或踩。踩可选「来源有误或缺失」「未基于公司文档」等原因。API 另支持可选评论。 | 运营可在 Insights 查看赞踩趋势和负反馈原因，并下钻到具体问题。 |
| LanRAG-Next | 一次反馈绑定本人在单一知识库内已封存的回答运行和已核验分块。表单可记录问题、回答摘录及修改建议。 | 审核定稿经冷静期和风险门禁成为指导。后续模型步骤命中原分块及其来源版本时，才可能追加指导。 |

## B 即时评价、分块权重与审核指导 {span=2}

:::flow LR
(回答) -> 逐条评价 -> 日志或评估
逐条评价 -> 引用分块权重: RAGFlow 启用时
(回答) -> 核验运行与来源 -> 审核定稿 -> 指导后续生成
审核定稿 --> 修订提案: 单独发起
:::

**Dify 的两条路径独立。** 赞踩进入应用日志。管理员显式启用 Annotation Reply 后，系统可在应用的标注库中匹配相似问题，命中时直接返回预写答复。赞踩提交会保存消息评价，标注回复需另外创建。 [提交接口](https://docs.dify.ai/en/api-reference/feedback/submit-message-feedback) · [列表接口](https://docs.dify.ai/en/api-reference/feedback/list-app-feedbacks) · [日志](https://docs.dify.ai/en/self-host/use-dify/monitor/logs) · [标注回复](https://docs.dify.ai/en/self-host/use-dify/monitor/annotation-reply) · [1.20.0 源码](https://github.com/langgenius/dify/blob/7bef4e1b2dc21b2c694d3243425dd3fa6288f916/api/controllers/service_api/app/message.py#L161-L216)

**RAGFlow 可按已存引用更新分块权重。** 单聊界面踩时要求填写说明，API 的说明字段可选。API 接受布尔赞踩，重复提交可以覆盖票值。 [v1.0.0 接口](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/docs/references/http_api_reference.md#L3679-L3745) · [单聊入口](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/web/src/pages/next-chats/chat/chat-box/single-chat-box.tsx#L85-L110) · [分享页限制](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/web/src/pages/next-chats/share/index.tsx#L80-L97) · [点踩表单](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/web/src/components/feedback-dialog.tsx#L45-L62) · [权限及保存](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/internal/service/chat_session.go#L587-L690)

反馈先持久化。存在引用且已显式开启 `CHUNK_FEEDBACK_ENABLED` 时，首次评分或翻票会尝试调整对应分块的权重。翻票时先尝试抵消旧票。权重默认按相关度分摊，可切为均匀分摊，取值为 0 至 100。重排会将 `pagerank_fea` 计入分块评分。 [开关和权重策略](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/internal/service/chat_session.go#L696-L793) · [分块写入](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/internal/service/chat_session.go#L905-L973) · [重排评分](https://github.com/infiniflow/ragflow/blob/db1cd5929a7885a3074547791f5ecc4d2f891739/internal/service/nlp/reranker.go#L614-L627)

**Open WebUI 关联对话上下文，支持评估与导出。** 用户提交和修改反馈走独立评价接口，管理员有独立导出端点。Elo 排行处理模型比较数据。 [前端构造](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/src/lib/components/chat/Messages/ResponseMessage.svelte#L494-L575) · [导出](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/routers/evaluations.py#L333-L342) · [用户提交及更新](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/routers/evaluations.py#L403-L459) · [模型排行](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/routers/evaluations.py#L205-L243)

**Amazon Q Business 把来源问题做成原因选项。** API 提供可选评论，Insights 可下钻问题、踩因和处理建议。启用日志投递后可导出反馈事件。AWS 已停止接收新客户，此处用作托管企业问答的交互先例。 [Web 体验](https://docs.aws.amazon.com/amazonq/latest/qbusiness-ug/using-web-experience.html#provide-feedback) · [提交接口](https://docs.aws.amazon.com/amazonq/latest/api-reference/API_PutFeedback.html) · [评价字段](https://docs.aws.amazon.com/amazonq/latest/api-reference/API_MessageUsefulnessFeedback.html) · [Insights 指标](https://docs.aws.amazon.com/amazonq/latest/qbusiness-ug/analytics-dashboard-metrics.html) · [反馈日志](https://docs.aws.amazon.com/amazonq/latest/qbusiness-ug/cw-log-examples.html) · [服务可用性](https://docs.aws.amazon.com/amazonq/latest/qbusiness-ug/qbusiness-availability-change.html)

## C 我们现有入口为何覆盖更少 {span=2}

1. **前端先看回答形态。** 「评价与反馈」仅在完整聊天的快速问答里显示。回答需要已保存的消息 ID、完整正文和同库引用。兜底及分支回答不显示。 [聊天挂载](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/frontend/src/views/chat/components/botmsg.vue#L134) · [可见性条件](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/frontend/src/views/chat/components/AnswerFeedback.vue#L55)
2. **提交还过两道检查。** 当前路由要求知识库业务写入权限。Host 用本人会话和回答 ID 查唯一已封存运行。成功的模型或工具步骤必须有实际检索分块。一次运行只能绑定一条治理反馈。 [路由](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/internal/router/routes_knowledge.go#L246) · [写入权限](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/internal/handler/extension_lifecycle.go#L59) · [来源绑定](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/extensions/business/qa_feedback.go#L11)
3. **合格的反馈进入治理。** 审核员可定稿指导。低、中风险审核后经过冷静期自动激活，高风险需要显式二次确认。低风险指导抽样复核，同主题两次负反馈可触发熔断。 [审核状态](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/extensions/business/feedback.go#L119) · [项目约定](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/PROJECT_CONTEXT.md#L121)
4. **指导作用于后续模型输入。** 当前步骤需要命中至少一个已核验的同一分块及其来源文档版本。正式修订由独立流程发起。 [指导匹配](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/extensions/guidance/inject.go#L14) · [修订流程](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/extensions/business/revision.go#L59)

历史隔离验证中，受限配置的快速问答和指定 Agent 路径能够封存，默认快速问答样例仍为 `unattributed`。Agent 后端通过来源测试，也不代表聊天页提供 Agent 反馈按钮。前端可见性、业务写入权限与来源封存分别决定能否看到、能否提交。 [Core 与 Host 验证](https://github.com/ybochuan/LanRAG-Next/blob/24d7e4704f7d7c0b898c98949fd688ad2ccc27b7/docs/evidence/m5-qa-2026-11-05.md#L29-L39)

## D 借鉴的边界 {span=2}

**想收集普通问答体验评价**，可以借鉴逐条回答赞踩、原因选择和可选说明。提交权限围绕本人会话，来源缺失也可以被报告。

**想让某条纠错影响后续回答**，使用现有治理语义。先核验来源，审核后发布指导，命中同一来源版本时注入。知识源真正有误时，由修订流程处理。

比较基于 2026-11-20 所查公开文档、列出的上游源码版本和本仓库 `24d7e47` 工作树。四个样本覆盖开源 RAG、模型评价界面和托管问答。未登录第三方产品实测，也未在用户实际环境复测入口。
