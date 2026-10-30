# Bridge v1：before_retrieve契约

协议头为`X-AgentRAG-Bridge-Version: 1`。本目录仅存JSON样例和说明，供独立Go module及未来原生薄适配分别消费，不建立Go module依赖。

原生RAG已接入500ms HTTP状态门卫，未消费收窄结果。Host默认配置M0 Pass-Through：严格校验后保留非空原生授权候选，不新增治理排除；空文档范围403，格式错误400。其余业务端点及readyz仍503。单独使用未配置Services的`bridge.NewServer()`仍拒绝业务调用，不代表实际Host的默认配置。

## 请求

见`before-retrieve.request.json`。冻结字段名及语义：

| 字段 | 当前检索前契约 |
|---|---|
| request_id / session_id | 非空关联ID，1..256字节 |
| mode / agent_round | rag或agent；轮次非负 |
| tenant_id | 非零uint64，JSON number，不经float64中转 |
| knowledge_base_ids | 1..1000个不同KB ID；必须是原生已授权范围，固定scope handler还要求在其allowlist内 |
| knowledge_ids | 已枚举的原生授权文档ID集合，0..10000个不同ID；必须显式数组，省略/null拒绝 |
| tag_scopes | 按KB保存原生tag ID数组；key须在knowledge_base_ids内，不能用它扩大权限；总计不超过10000个tag ID |
| original_query / rewritten_query | 原始与改写查询，各最多64KiB |
| token_budget | 非负；0表示未提供正预算 |
| metadata | JSON object，值原样保留，包括大整数和嵌套值；不承载隐式权限覆盖 |

ID为1..256字节的非空单段，拒绝空白、控制字符及`/\?#%`等路径分隔/转义字符；关联ID不要求路径段格式。可选tag_scopes/metadata省略表示无附加值，提供时必须是object，不能为null。round、query、budget省略时取Go零值；请求关联、mode、tenant/KB与显式文档范围必须存在并合法。

整个请求最大1MiB；JSON嵌套最多64层。只接受单个顶层object，字段名严格使用表中拼写，拒绝未知字段、大小写别名、任何层级的重复key、无效UTF-8、错误类型和拼接的第二个JSON。metadata可用新key扩展，顶层扩展先更新契约并验证双方消费者。超过限额必须拒绝，不能截断候选或回退整库。

`before-retrieve.empty.json`是明确空范围，不表示whole-KB。固定scope handler返回200+deny_all且不调用策略；M0默认Pass-Through返回403，因为现有原生门卫只检查HTTP状态，不能让200中的拒绝被忽略。省略/null也拒绝，未实现whole-KB候选枚举。原生调用方负责权限与tag过滤，本端不替代原生授权。

## 响应和错误

见`before-retrieve.response.json`。knowledge_ids是按原输入顺序保留的显式子集；excluded_document_ids也是输入子集，两者互斥并覆盖输入集合；全部排除时返回`knowledge_ids: []`和`deny_all: true`。空输出和空排除列表均使用数组，不输出null。request_id、tenant_id、knowledge_base_ids保持原请求关联和范围。

排除策略必须遵循请求context，仅返回输入候选内不同ID；策略报错、返回候选外/重复ID或请求取消，统一503且无许可DTO。策略输入切片即使被修改也不能扩大响应范围。这里不定义M2治理规则，不把解析completed解释为active。

| 状态 | 语义 |
|---|---|
| 200 | M0默认透传允许非空候选继续；固定scope handler的收窄决定仍需解释deny_all |
| 400 | 版本/JSON/字段不合法；不调用策略 |
| 403 | M0默认透传收到空文档范围，或固定scope handler的tenant/KB超出配置；不调用策略 |
| 413 | 请求体超1MiB；不调用策略 |
| 503 | 无策略、策略故障/坏结果/取消；不许可检索 |

Bridge服务请求头与整个请求读取期限均为5秒；原生客户端已采用500ms调用超时，策略须遵循context。健康接口200只表示存活；单个typed handler不令readyz变200。错误仅包含固定`error.code`，不回显查询、metadata或内部错误。

## 后续收窄结果消费者的必验项

- 版本、request/tenant/KB关联、响应数组存在性、唯一性、子集关系和deny_all一致性都必须验证；失败按before_retrieve fail-closed。
- 保留原授权与tag过滤，将收窄结果转换为显式document目标。原生whole-KB目标不会因为填了knowledge_ids就自动收窄；单独发送excluded_document_ids也不会生效（C-09）。
- 空范围必须停止检索，不能变成全库；Host不可用/超时/坏响应时验证chunk/entity/fallback调用计数为0。
- 多KB的原生目标分组必须保留；本批扁平候选DTO不替代调用方的文档到KB授权映射。大库完整枚举、IPC限额与原生目标转换在C-09薄适配POC中实测，不能静默少列候选。

results[]用于检索后契约，guidance用于生成前能力，ingest事件接收端必须先持久提交才能ACK；这三项本批尚未实现，不以空增强或伪ACK补齐。
