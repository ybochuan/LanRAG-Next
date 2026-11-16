# T4-03 未适配输入组成：逐项判定与 rerank 适配（2026-11-16）

## 范围

T4-03 记录把「查询改写、快速问答重排、无法由原始字段解释的重排文本、FAQ、图像、合并、自定义元数据及 listing updated_at」列为未适配组成。本文件逐项判定这些组成是否能用原生字段作证，并完成其中一项安全适配。

## 关键前提：原生证据的边界

provenance 的输入核验要求「模型实际看到的字节」能由原生快照中的某个字段逐字节证明（`InputEvidence` + `verifyPermissionTx`）。因此：

- **原生字段**（文档正文、标题、描述、文件名）可直接作证。
- **模型派生内容**（改写后的查询、VLM 描述、重排文本、格式化元数据）没有对应的原生字段，无法直接作证；要作证就必须记录「产生它的那个模型步骤」以及该步骤自身的输入，即一条**派生输入链**。这是当前架构不具备的能力。
- **合成内容**（merge 把多个分块正文合并成一段）不对应任何单个原生分块，同样无法用单分块字段作证。

## 逐项判定

| 组成 | 判定 | 原因 |
|---|---|---|
| 快速问答重排（rerank） | **已适配** | 重排只选择与排序候选，不改写字节；最终渲染取自 `MergeResult`，每条仍逐个核对原生分块 ID 与未改正文。 |
| 查询改写（`EnableRewrite`） | 保持拒绝 | 改写后的查询是模型输出，不是原生字段。 |
| 无法由原始字段解释的重排文本 | 保持拒绝 | 属于派生文本。 |
| FAQ | 保持拒绝 | merge 会从 FAQ 数据填充答案并参与合成正文，不是单分块字段。 |
| 图像 | 保持拒绝 | 图像描述由 VLM 生成，属派生内容。 |
| 合并 | 保持拒绝 | `groupAndMergeCurrentContent` 把多个分块正文合并为复合正文，也做父分块解析与邻块扩展，结果不对应单个原生分块。 |
| 格式化自定义元数据 | 保持拒绝 | 展示前经过格式化，字节不等于原生字段。 |
| 历史／记忆／附件／引用上下文／数据分析 | 保持拒绝 | 非原生来源或派生内容。 |

## 本次改动

`internal/application/service/chat_pipeline/governance.go::sourceInputsForModel` 原先在 `RerankModelID != ""` 时无条件放弃归属。这是过度保守：重排不改变渲染字节，且函数随后仍逐条校验分块 ID 与正文，任何无法核验的候选都会让整体退回未归属（fail-closed）。已移除该条件并写明理由；查询改写等条件保持不变。

测试（`source_provenance_test.go`）：新增用例证明「配置了 rerank 模型且合并结果为原生分块」时仍产出同样的 content/title/description 绑定；新增 `query-rewrite` 用例证明开启改写后仍拒绝归属。

```text
CGO_ENABLED=1 go build -mod=readonly ./internal/application/service/chat_pipeline/  → 退出 0
unshare -Urn go test ./internal/application/service/chat_pipeline/ -count=1           → ok（整套通过）
```

注：该包有一个用例需要绑定 127.0.0.1:8090，生产 Host 运行时会占用，因此在隔离网络命名空间中执行，与运维文档对 8090 被占用时的既有做法一致。

## 生产探针（未得到封存结果）

为在生产环境验证该改动，临时把 `config/config.yaml` 的 `conversation.enable_rewrite` 由 `true` 改为 `false`（改前已备份到 `/tmp/config.yaml.p5-backup`），用当轮构建的 Core 重启后通过 `/knowledge-chat` 提出两个针对真实文档的问题。结果两次都在检索阶段返回：

```text
stage=Pipeline action=stage_fallback event="chunk_search_parallel" reason="search_nothing"
```

检索未返回任何候选，因此没有来源集，provenance 运行如实记录为 `unattributed`（2 条，revision 3，sources 0）。这是检索阶段的结果，与本次改动无关——provenance 记录发生在生成阶段，检索为空时根本不会进入输入核验。

配置随后已还原（`enable_rewrite: true`，`git diff` 为空）并重启服务。**因此 rerank 适配目前只有单元测试证据，没有生产端到端证据。**

## 结论与需要的决策

- 八项中只有 rerank 可以在不新增架构的前提下安全适配；其余七项都需要**派生输入链**（记录派生值由哪个模型步骤产生、该步骤的输入又是什么），这是新的架构能力，不是薄接线。
- 在派生输入链建立之前，这些组成的运行会继续保持 `unattributed`；这是 fail-closed 的正确行为，不是缺陷。
- T4-03 因此**仍未完成**，不标整项完成。继续推进需要用户决定：是否投入建立派生输入链，或接受这些组成长期保持未归属。

## 边界

- 本轮改动只涉及一个条件的移除与测试；未改检索、重排、合并或生成算法。
- 生产配置改动已还原；未改生产数据。
- 未声称 FAQ、图像、合并、自定义元数据或查询改写已适配。
