# SQLite 存储专项完整联合回归验收（2026-11-07）

范围：用户 2026-11-07 批准的 SQLite 存储性能专项 P0/P1 完整联合回归。P0 已单独提交 `d7418f1`，P1 已提交 `c7330f4`、`a6aadb8`；本文件只记录验收结果，不修改 Core 连接池、WAL 或前端轮询，不进入 P2。

## 结论

**P0/P1 联合回归通过，未发现阻塞 P2 的存储或接口回归，允许进入 P2。** P2-A 应首先处理启动时的 `reconciliation_failed`；其余残余项见文末“已知限制与风险”。P2 代码尚未开始，本文件也不代表 P2 已实现。

## 环境与版本

- Windows `10.0.26340.9843`；WSL `3.0.3.0` / Ubuntu；Go `1.26.0`。
- Core `127.0.0.1:18880`，Host `127.0.0.1:8090`，docreader `127.0.0.1:50051`，MinerU `127.0.0.1:8000`，supervisor `127.0.0.1:8092`，embedding `127.0.0.1:8081`，rerank `127.0.0.1:8082`。
- WSL 网络模式 `mirrored`；当前运行目录 `data/launcher-run-8p8Mbr`（验收阶段另有 `data/launcher-run-kO9ksL`、`data/launcher-run-XxwMNA`）。
- Core/Host 数据库均为 WSL ext4：`/home/ybochuan/.local/share/agentrag/weknora.db`、`/home/ybochuan/.local/share/agentrag/agentrag_ext.db`。原始 `/mnt/d/LanRAG-Next/data/weknora.db` 与 `agentrag_ext.db` 保留，备份在 `data/backups/p1-20261107-143930/`。
- 验收开始时 `start.cmd status` 退出 0，七项服务全部 UP；`start.cmd -Stop` 用于隔离 E2E 时退出 0，停止 WSL Core/Host、embedding、rerank、supervisor、MinerU、docreader；E2E 完成后重新 `start.cmd`，再次七项全部 UP。

## 阶段一：基础环境与数据库（P0）

- Schema/记录数：`PRAGMA quick_check=ok`；native `knowledges=79`、`chunks=13,565`、`lite_embeddings=6,095`、`knowledge_bases=1`；ext `ext_lifecycle_current=69`（67 active / 2 stale）、`ext_lifecycle_events=140`、`ext_document_annotations=69`、`ext_review_alerts=0`。
- 重启后数据仍正常：停服再启动后上述计数不变，Core/Host 环境与打开 fd 均指向 ext4 路径。
- Phase1 探针（`data/sqlite-accept-phase1-20261107.json`）：`GET /api/v1/knowledge-bases` 10/10 HTTP 200，P50=15.7ms/P95=19.0ms；`GET /api/v1/knowledge-bases/{kb}/knowledge?page=1&page_size=20` 10/10 200，P50=17.5ms/P95=19.7ms；Host `overview` 10/10 200，P50=18.2ms/P95=23.1ms，`degraded=false`、`total=67`（67 active）。
- `host.log` 启动时有一条 `reconciliation_failed`（当前运行 `16:18:15`），之后未见周期性重复；该单条启动事件留待 P2-A 定位，不阻塞本次 P0/P1 验收。

## 阶段二：文档上传与解析（P0）

- **PDF / MinerU**：`data/sqlite-accept-logs/m1-native.log`，`[RESULT] PASS`。真实 Windows MinerU 4.0.1 + WSL CGO Core：`[UPLOAD] PASS`（multipart→completed，chunks 含 PDF 文本）、`[PROXY] PASS`（实际 MinerU job，flash/txt）、`[ARCHIVE] PASS`（middle.json schema2.0/MinerU4.0.1，字节一致）、`[SQL] PASS`（原始 middle JSON 逐字节、正式 job FK、无外键违规）、`[INDEX] PASS`。
- **Host 重启任务恢复**：`data/sqlite-accept-logs/m1-native-joint.log` 的 `[RECOVERY] PASS`：重启生产 Host 后恢复已验证来源能力、原 job receipt 保留，实际 MinerU POST 次数仍为 1。该联合日志整体 `[RESULT] FAIL` 仅因 `[JOINT-ONLINE]` 子场景使用 `AGENTRAG_MODE=probe`、无业务 DB 时 `before_retrieve` 正确返回 HTTP 503（fail-closed）；不是产品回归，详见“已知限制”。
- **DOCX 附件提取/独立入库**：`data/sqlite-accept-logs/attachment.log`，`python3 attachment_e2e.py --expected-annexes 2 --expected-tables 1 --governance` 原生日志 PASS：`[NATIVE-LIST]` 1 parent + 2 个独立 DOCX annex；`[CONTENT]` 1 个表格保留、父子均完成 chunk/index；`[REPLAY]` 重复提取复用同一 ID、不新建文档、不递归；`[RETRIEVAL]` RAG/Agent 命中并带下载元数据；`[PARENT-STALE]` 父过期→child 仍 active、链接消失、旧下载 URL 403；`[HOST-OFFLINE]` 旧下载 URL 同样 403。
- **XLSX 附件提取**：`go test -mod=readonly ./parser/attachment -run TestOfficeRegistryFiltersActualTypesAndStrictSize -count=1 -v` PASS；`parent.docx`、`parent.xlsx`、`parent.pptx` 三个子测试均通过（0.13s），覆盖 XLSX 父容器的附件类型过滤、严格 20MiB 限制与嵌套文件排除。真实 XLSX 业务文件的端到端上传未单独执行（见已知限制）。
- **失败任务不误标 active**：xlsx `e52c8c3a-bde9-4dff-bc7a-da6379d5a2b5` 保持 `parse_status=failed`、`enable_status=disabled`，未进入 active；Lifecycle E2E 的过期/失败状态见阶段四。

## 阶段三：RAG 与 Agentic RAG（P0）

`data/sqlite-accept-logs/governance.log` 全量 PASS（`[RESULT] Governance leak E2E PASS; exit=0`），真实 patched Core + worker Host、隔离 SQLite、本地确定性模型夹具：

- `[RAG-ACTIVE]` active 文档正文到达生成 prompt；`[PROVENANCE-RAG]` 原生身份与实际模型调用由真实 worker Host 持久化；`[RAG-FALLBACK-ACTIVE]` 空检索 fallback 仅收到独立 description canary。
- `[AGENT-SEARCH-ACTIVE]`、`[AGENT-READ-ACTIVE]`、`[AGENT-LIST-ACTIVE]`：真实 Agent tool 运行，允许来源进入后续 prompt。
- `[M5-QA-RERANK]`：重排器消费 active 来源，sealed Agent answer 可接受 staged feedback。
- stale 段：`[RAG-STALE]`、`[RAG-FALLBACK-STALE]`、`[AGENT-SEARCH-STALE]`、`[AGENT-READ-STALE]`、`[AGENT-LIST-STALE]` 均在相同 session/已知 ID 下确认正文、description、旧答案 canary 未进入任何模型请求；`[PROVENANCE-TERMINAL]` 无未关闭 run。
- offline 段：`[RAG-OFFLINE]`、`[AGENT-SEARCH-OFFLINE]` Host 不可用时 `answer_generation_calls=0`，未泄漏受治理来源内容。
- `[AGENT-ARCHIVE]`：原生 discovery + MCP 调用仅交付授权 stale excerpt 与 warning（实际 MCP 服务注册未签发 key，见已知限制）。

## 阶段四：生命周期与文档治理（P0）

- `governance.log`：`[M6-LINEAGE]` JWT graph 保留真实 published `supersedes` 方向、ancestry 与 provenance 分离；`[M4-RELATED]` 原生 JWT declaration/query/revoke 与 current version/active permit/critical edge 事务；`[M5-FACTS]` superseded source、single published proposal、family projection intents、FK 完整；`[LIFECYCLE]` 真实 startup sweep active→stale；`[GOVERNANCE-LIST]` 认证 Core API 向只读 join 转发 title/state；`[PROVENANCE-TERMINAL]` 完成请求无 open run。
- `docs/evidence/m2-lifecycle-ui-e2e-2026-11-01.log`：`[AUTH]` native register/login；`[OFFLINE]` Host 停时 management 503；`[UPLOAD]` 真实上传→native chunks→Observer active→带标题 Overview；`[DATES]` past expiry→stale、labels 保留、event timeline、stale CAS 409；`[POLICY]` 日期修改后真实 DB policy 立即排除；`[CONCURRENCY]` 8 个真实 writer 1×200+7×409，无 lost update；`[AUTHORIZATION]` missing 401/machine key 403/foreign doc 404/viewer 只读；整体 PASS。
- `docs/evidence/m2-supplement-e2e-2026-11-01.log`：`[SUPPLEMENT]` scoped current、native JWT declaration/replay/CAS；`[POSITIVE]` child body/description 到达真实 RAG/Agent 请求；`[VERSION]` active parent edit→fresh-root mismatch→child 从所有模型请求排除；`TestSupplementOwnedSweep PASS`；`[EXPIRY]` parent expiry→child stale/event/outbox、labels retained；整体 PASS。
- `tmp/m3-source-e2e-final.log`：`TestNativeSourceOwnedIntegration` PASS；`[SOURCE]` 真实授权 Core snapshot→immutable observed version→source ledger；native chunk edit→new current root、old source refused、lifecycle active；`[BOUNDARY]` 无 sealed-run authority 时拒绝 model complete claim。
- Reviews 到期提醒：`go test -mod=readonly ./review ./lifecycle ./dependencies ./host -count=1` 全部 PASS（`review 0.248s`、`lifecycle 0.370s`、`dependencies 0.065s`、`host 0.349s`）；当前生产 `ext_review_alerts=0`，未构造真实到期提醒业务样本。
- 附件父文档失效拦截已由 `attachment.log` 的 `[PARENT-STALE]`/`[HOST-OFFLINE]` 覆盖（父 stale、child active、旧下载 URL 403）。

## 阶段五：反馈与正式文档修订（P1）

`governance.log` 相关行全部 PASS：

- `[M5-QA-FEEDBACK]`：Quick QA 答案绑定精确 native chunk IDs；重试只产生一个 staged feedback。
- `[M5-GUIDANCE]`：默认 Quick QA 收到 guidance 且保持 unattributed；精确 RAG/Agent sealed；foreign/quarantined instructions 不进入；持久预算生效。
- `[M5-FEEDBACK]`：真实 staging/approval→early activation 409→fixture deadline advance→真实 API activation/guidance；不宣称真实等待 48h。
- `[M5-PUBLISH]`：activated feedback guidance→reviewed proposal→真实 native Markdown ingest/index→supersedes；重复 publish 保留 target。
- `[M5-FAMILY-TAG]`：真实 worker 将共享 native family ID 投影到两个版本并保留 business tag。

## 阶段六：稳定性与性能（P1）

`data/sqlite-accept-logs/phase6.json`（生产栈，16:09 左右采集）：

| 检查 | 结果 |
|---|---|
| `knowledge_bases` | 30/30 HTTP 200，P50 3.98ms，P95 4.39ms，max 5.17ms |
| `knowledge_list` | 30/30 200，P50 8.77ms，P95 9.37ms，max 11.32ms |
| 并发 `knowledge_list`（8 线程/40 请求） | 40/40 200，wall 160.75ms，P50 26.43ms，P95 47.18ms |
| Host `overview` | 30/30 200，P50 7.34ms，P95 8.65ms，max 9.77ms，degraded=false |
| business relation/feedback/guidance/revision list | 各 10/10 200，P50 1.32–1.78ms，P95 1.94–2.25ms |
| `knowledge-search` | 5/5 200，P50 2139.0ms，P95 2209.23ms，max 2911.73ms |
| SQLite | ext4，journal_mode=wal，page_count 46,440，page_size 4096，WAL 45,352 bytes，`quick_check=ok`，knowledges=79/chunks=13,565/lite_embeddings=6,095 |

- 检索期间上传由 `governance_e2e` 的真实上传/查询流水与 attachment E2E 的父/子上传覆盖；模型推理期间重启 Host 由 `m1-native-joint` 的 `[RECOVERY] PASS` 覆盖。
- 生产 `start.cmd status` 当前七项全 UP；`data/launcher-run-8p8Mbr/host.log` 只有启动时一条 `reconciliation_failed`，无周期性重复。

## xlsx `e52c8c3a...` 专项核查

- DB：`parse_status=failed`、`enable_status=disabled`、`deleted_at=2026-11-07 06:01:25.726731873+00:00`、`knowledge_base_id=164bfd73-d231-4d5c-994e-057ac0cdf48a`、`chunks=6,896`、`lite_embeddings=0`；未删除任何 chunks。
- `GET /api/v1/knowledge/e52c8c3a-...` 返回 404 `Knowledge not found`（软删除）。
- `data/sqlite-accept-logs/xlsx-retrieval.json`（hybrid-search 三入口各 30 条）：`keyword_only` match_type 全为 1（FTS）、`vector_only` 全为 0（向量）、`hybrid` 全为 0；三者 `e52_hits=0`。带 `knowledge_ids:[e52]` 的 keyword/vector/hybrid 均 HTTP 200、0 条。
- 高层 `/api/v1/knowledge-search` 不带 filter 查询 200、2,207.2ms、6 条、`e52_hits=0`；带 `knowledge_ids` 的请求被 API key scope 以 403 `API key scope does not allow knowledge_ids without a verified knowledge base` 拒绝，未作为排除证据（hybrid-search 的结果已覆盖原始检索入口）。

## 命令与退出码

- `cmd.exe /c start.cmd status`：exit 0，七项 UP；`cmd.exe /c start.cmd -Stop`：exit 0，全部停止。
- `AGENTRAG_GOVERNANCE_REPORT=... python3 governance_e2e.py`：exit 0，`[RESULT] Governance leak E2E PASS`。
- `python3 m1_native_e2e.py`（非 joint）：exit 0，`[RESULT] PASS`；`m1_native_e2e.py --joint-m0`：exit 1，`[JOINT-ONLINE]` 子场景因 probe Host 503 失败，`[RECOVERY]`/上传/解析/归档全部 PASS。
- `python3 attachment_e2e.py --expected-annexes 2 --expected-tables 1 --governance`：exit 0，`[RESULT] Attachment stage-two/three E2E PASS`。
- `python3 lifecycle_ui_e2e.py`、`python3 m2_supplement_e2e.py`、`python3 m3_source_e2e.py`：各自 `[RESULT] ... PASS`，exit 0（日志见 `docs/evidence/m2-lifecycle-ui-e2e-2026-11-01.log`、`docs/evidence/m2-supplement-e2e-2026-11-01.log`、`tmp/m3-source-e2e-final.log`）。
- `go test -mod=readonly ./parser/attachment -run TestOfficeRegistryFiltersActualTypesAndStrictSize -count=1 -v`：exit 0。
- `go test -mod=readonly ./review ./lifecycle ./dependencies ./host -count=1`：exit 0。
- `go test -mod=readonly ./... -count=1`（在 `extensions/`）：全部包通过，`management`、`store`、`parser/attachment` 等均 ok；无源码改动。

## 已知限制与风险

1. `m1_native_e2e.py --joint-m0` 的 `[JOINT-ONLINE]` 在 Host 以 `AGENTRAG_MODE=probe` 重启时因无业务 DB 返回 503，这是 M2 fail-closed 策略与旧 joint-M0 fixture 的预期冲突；不影响非 joint M1 的 `[RESULT] PASS`，但该组合场景不能计为完整在线 RAG 通过。
2. `MCP_ADMIN_KEY` 仍为空，启动器输出 `[WARN] MCP 未注册`；`governance.log` 的 `[AGENT-ARCHIVE]` 覆盖了 native discovery/MCP 调用路径，但真实 MCP 服务注册、目录刷新与凭据签发未验证。
3. 真实 XLSX 业务文件的端到端上传/解析未单独执行；当前证据是 `parser/attachment` 的 XLSX 容器/附件类型单测，以及生产库中该 xlsx 的软删除/disabled 检索排除。
4. `ext_review_alerts=0`，Reviews 到期提醒只有单元/包测试覆盖，未构造真实到期提醒业务样本；不影响 P0/P1 存储验收，P2/P3 前端验证时补业务样本。
5. 启动时 `reconciliation_failed` 单条仍存在，P2-A 需要定位，不能作为“完全无失败”宣传。
6. P2 的 Core 连接池/WAL、前端批量标题/N+1、5 秒轮询尚未修改；本文件未对 P2 做任何实现或性能承诺。
