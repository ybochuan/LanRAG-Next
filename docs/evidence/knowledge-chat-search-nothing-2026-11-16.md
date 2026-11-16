# knowledge-chat 检索为空的根因与修复（2026-11-16）

## 现象

`knowledge-chat` 对生产 KB（`164bfd73…`，75 篇可解析文档）稳定返回「参考材料为空」，日志为：

```text
stage=Pipeline action=stage_fallback event="chunk_search_parallel" reason="search_nothing"
```

同一时刻 `hybrid-search` 直连返回 50 条。整条链路在 **5 毫秒**内结束，且没有任何 `stage=SearchParallel` 日志——说明检索根本没有开始，而是被门卫直接拒绝。

## 结论：不是阈值问题

修复后实测的完整数字链：

| 阶段 | 数量 |
|---|---|
| 枚举的原生候选文档 | 75 |
| 策略放行 / 排除 | 64 / 11 |
| 原始召回（`chunk_search_done result_count`） | **30** |
| 合并后（`Merge action=output merged_total`） | 14 |
| FilterTopK 输出（`merge_cnt`） | 13 |
| 进入聊天消息（`IntoChatMessage merge_result_cnt`） | **13** |

- `vector_threshold: 0.2` 没有把召回清零：向量+关键词召回稳定给出 30 条。
- `rerank_threshold: 0.3` 根本没有参与：本次链路没有执行 `chunk_rerank`（`rerank_cnt=0`，无 `stage=Rerank` 日志），因此不存在「本地 Qwen3-Reranker 打分偏低被 0.3 全过滤」的情况。
- 原始召回为 30 而非 0，说明 embedding 服务、向量索引和关键词检索都正常。

## 两个真实根因

### 根因一：整库请求没有枚举文档，Bridge 只能回答 deny_all

`contract/retrieval.go` 明确要求 `knowledge_ids` 必须是**已经枚举好的原生授权文档集合，不能是整库哨兵**。但 `PluginSearchParallel` 直接把 `chatManage.KnowledgeIDs` 传给 `before_retrieve`，而整库聊天在原生侧该字段是空切片。Host 的持久策略只能从「候选集」里做减法，拿到空集就只能返回 `deny_all`；原生适配层按第 6.1 条对 `deny_all` 硬失败，于是 5ms 内 `search_nothing`，连检索都不启动。

修复：在调用门卫前，把纯整库目标展开为该 KB 的文档 ID 集合（仅 `enabled` 且 `parse_status=completed`，上限 200 个）。标签范围、文档范围、服务错误或超限一律返回空，保持原有的 fail-closed 行为，不放宽任何授权。

### 根因二：空但非 nil 的 `KnowledgeIDs` 被当成「什么都不允许」

`guardPromptResults` 用 `cm.KnowledgeIDs == nil` 判断「请求没有文档级限制」。整库请求里这个切片是**空但非 nil**，判断为假，于是 `authorized` 空表使每一条结果都被判为未授权，14 条合并结果在进入提示词前被全部丢弃——这正是日志里 `Merge merged_total=14` 紧跟 `IntoChatMessage merge_result_cnt=0` 的原因。

修复：改为 `len(cm.KnowledgeIDs) == 0`。空集合表示没有文档级限制，而不是一个空允许表；候选集已由前面的 Bridge 决策收窄。

## 修复后的端到端结果

向原生 `/knowledge-chat` 提问「深圳市业主大会和业主委员会备案管理办法规定了哪些备案要求？」，回答基于文档并带原生引用标记：

```text
《深圳市业主大会和业主委员会备案管理办法》（深建规〔2020〕13号）所称备案，是指……
<kb doc="深圳市业主大会和业主委员会备案管理办法.txt" chunk_id="b84ee009-…" kb_id="164bfd73-…" />
```

阶段日志：

```text
stage=SearchParallel action=enumerated_documents count=75 targets=1
stage=SearchParallel action=policy_decision allowed=64 candidates=75 deny_all=false excluded=11
stage=SearchParallel action=chunk_search_done has_error=false result_count=30
stage=Merge action=output merged_total=14
stage=FilterTopK action=output merge_cnt=13 rerank_cnt=0 search_cnt=27
stage=IntoChatMessage action=input merge_result_cnt=13
```

## 验证

```text
CGO_ENABLED=1 go build ./internal/application/service/chat_pipeline/   → 退出 0
CGO_ENABLED=1 go test  ./internal/application/service/chat_pipeline/   → ok（含新增回归用例）
CGO_ENABLED=1 go build -tags=lite,sqlite_fts5 -o tmp/WeKnora-lite-remediation ./cmd/server → 退出 0
生产 /knowledge-chat 实测                                     → 30 条召回、13 条进入提示词、回答带 kb 引用
```

新增回归用例 `TestGovernanceTreatsEmptyKnowledgeIDsAsNoDocumentRestriction`：空非 nil 的 `KnowledgeIDs` 不再丢弃结果。

## 现场还原

- 验证期间曾临时把 `Qwen3.5-9B` 置为默认知识问答模型以便本地生成答案，现已改回（`is_default` 全部为 0），并重启服务。
- 未修改 `config/config.yaml` 中的任何阈值；`enable_rewrite` 已在上一步恢复为 `true`。

## 边界

- 枚举上限 200 个文档；超过此规模的整库请求仍会 fail-closed（返回空候选 → deny_all），这是刻意保留的安全边界。
- 带标签范围的请求不做枚举，行为与修复前一致（fail-closed）。
- 本轮只修检索为空；`guardPromptResults` 与门卫之间的职责划分未变。
