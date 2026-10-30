# M0 Retrieval Contract Implementation Plan

> **For agentic workers:** 使用superpowers:executing-plans本人逐项实施，先测试后实现，最后一次独立只读复核。现有用户已授权推进下一项；不重新要求对既有M0路线审批。

**Goal:** 完成M0-02B的扩展端公开检索契约与typed HTTP handler，证明响应只能收窄已枚举的原授权文档集合。

**Architecture:** 新建独立contract DTO和仓库共享JSON样例。可显式配置BeforeRetrieve handler的Bridge构造器；handler绑定tenant/KB allowlist，调用注入的排除策略，再输出显式knowledge_ids和deny_all。生产Host暂不配置策略，仍503；其余Hook/readyz不提前就绪。后续持久策略、原生薄适配与两module契约消费单列。

**Tech Stack:** 扩展Go 1.26.0声明、标准库encoding/json/net/http；无新依赖或迁移。

**Spec:** Frozen Architecture 3.2/3.4/3.6、TASK_BACKLOG M0-02B、C-03/C-09及用户C-10决定。

## Global Constraints

- 不修改internal/、根go.mod/go.sum、BASELINE或Frozen原文；不提交、推送、重构核心。
- extensions独立module，不导入WeKnora；contracts/bridge/v1仅存可跨module消费的JSON和说明。
- request_id/session_id/mode/agent_round、tenant_id/knowledge_base_ids/knowledge_ids/tag_scopes、original_query/rewritten_query/token_budget/metadata使用冻结字段名；results[]留检索后契约，不在检索前虚构结果。
- C-10安全拒绝作为Baseline Limitation，后续独立标签只读API，不在本批实施。
- handler不是原生Fail-Closed证明；不配置治理策略，不把parse completed转换为active，不开放ingest ACK。

## Review Focus

1. 省略/null knowledge_ids与显式空数组不能混淆；前者400，后者200 deny_all且不调用策略。
2. tenant/KB/tag scope越界、策略返回候选外ID或坏ID、取消后返回结果均不得得到检索许可。
3. JSON多对象、重复字段、未知/大小写别名字段、类型错误、超1MiB body必须拒绝且无策略调用；错误不能泄露原始输入。
4. 响应保留请求关联和作用域；knowledge_ids按原顺序收窄，空集合绝不表示whole-KB。tag filter/原权限由后续核心适配继续取交集。
5. 默认NewServer/Host仍503；配置handler只开放对应业务端点，readyz/其他Hook保持503；真实服务和核心调用点本批不验证。

## Task 1: Public DTO and fixtures

Files: 新建`extensions/contract/retrieval.go`、`retrieval_test.go`；`contracts/bridge/v1/before-retrieve.request.json`、`before-retrieve.response.json`、`before-retrieve.empty.json`、`README.md`。

Interfaces: `Context`包含冻结检索前字段；`BeforeRetrieveRequest{Context}`；`BeforeRetrieveResponse{RequestID,TenantID,KnowledgeBaseIDs,KnowledgeIDs,ExcludedDocumentIDs,DenyAll}`；`DecodeBeforeRetrieve([]byte)(BeforeRetrieveRequest,error)`与`Validate()error`。tenant uint64、scope与IDs显式数组，metadata为原始JSON值的map；mode为rag/agent，round/budget非负。最多1000 KB、10000文档/标签ID，每个ID1..256字节非空单段；req/session ID为非空1..256字节；query各不超过64KiB。严格顶层字段/重复字段检查，metadata扩展值保留。

- [x] 先写测试：共享样例成功往返、agent/multi-KB上下文保留；省略/null文档、scope重复/空/坏ID、负round/budget拒绝；空[]可表达deny-all；重复/未知/别名/拼接/错误类型JSON拒绝。
- [x] Run `go test -mod=readonly ./contract -count=1`。Expected: 缺少contract实现而失败（RED），实际退出1：no non-test Go files。
- [x] 实现DTO/验证/解码，运行相同命令。Expected: PASS（GREEN），实际退出0。修正测试按JSON语义比较map键序，保留原始大整数断言。

## Task 2: Typed before_retrieve HTTP boundary

Files: 新建`extensions/bridge/retrieve.go`、`retrieve_test.go`；修改`server.go`，复用已有protocol.go。

Interfaces: `RetrievePolicy.ExcludedDocumentIDs(context.Context,contract.BeforeRetrieveRequest)([]string,error)`；`NewRetrieveHandler(tenantID uint64,kbs []string,policy RetrievePolicy)(*RetrieveHandler,error)`；`Services{BeforeRetrieve *RetrieveHandler}`和`NewServerWithServices(Services)*http.Server`。现有NewServer()委托空Services。构造器要求非空且合法allowlist和策略；复制配置，禁止每请求扩权限。handler限制body为1MiB；400输入错误、413超限、403 scope越界、503策略失败/坏结果/取消；响应只含标准错误码，不带原始错误。支持取消的策略收到请求context。

- [x] 先写真实httptest测试：绑定策略实参保留；成功只留doc-a/doc-c；全排除/显式空产生deny_all；错误、未知ID、取消均503；跨scope403；坏body/版本/超限无策略调用；未配置与其他Hook仍503。
- [x] Run `go test -mod=readonly ./bridge -count=1`。Expected: 新接口缺失（RED），实际退出1：NewRetrieveHandler/NewServerWithServices/Services尚未实现。
- [x] 实现最薄HTTP边界；不增加治理算法，原生适配留后续。Expected: 相同命令PASS（GREEN），实际退出0。

Task 2: Ruling: 新增5秒完整请求读取期限 — 只有ReadHeaderTimeout不能约束停滞body，typed handler必须能终止读取 — 代价是极慢本机请求会被拒绝并需重试。真实连接测试先在7秒客户端deadline失败，再配置ReadTimeout后400且策略未调用、正常请求恢复；RED→GREEN。

Task 1/2: complete（未提交）；`go test -mod=readonly ./... -count=1`十二包、build/vet、实际模块门禁244 imports及Python七例回归均退出0，gofmt无输出。原件/根依赖hash未变；实际Git枚举看得到所有新增契约文件，tracked核心diff为空；64个未跟踪文本文件额外空白检查通过。

## Completion and ledger

- [x] Go完整test/build/vet/gofmt、Python边界门禁及7例回归，原件hash/实际Git未跟踪文件可见性与范围核对。
- [x] 一次独立只读复核，无Critical/Important/Minor实质问题，无代码修复；复核者重跑contract/bridge退出0。六项范围裁定及代价逐项记录至TASK_BACKLOG。
- [x] 更新PROJECT_CONTEXT/TASK_BACKLOG/Change Map/运行说明；公开契约仍未被原生module消费，生产端点仍503，不声明02B全部或端到端治理完成。

Final review: 本批通过；没有deferred minor。完成本地扩展端契约与typed handler：2026-10-30。所有Final: Ruling逐项见TASK_BACKLOG本批执行记录；后续依赖未被本次通过解除。

执行约定：用户要求保留未提交工作，跳过技能中的commit/worktree迁移和清理步骤；验证直接运行本模块命令并在此记录结果，不使用依赖Git提交范围的任务脚本。
