# M0 Bridge Foundation Implementation Plan

> 执行：本会话按 executing-plans 直接实施；根目录 TASK_BACKLOG.md 保存任务生命周期和执行记录。原批次为协议和门禁；用户随后明确C-01至C-04的M0取舍，本计划追加Task3–5的扩展组件，不创建额外worktree、提交或推送。

**Goal:** 为独立Host补协议/门禁，并按用户取舍实现周期对账、标签串行投影和检索排除计算组件。

**Architecture:** 标准库、本机回环、独立module。没有实际治理/持久层时四个业务端点明确拒绝，不返回ready或ingest accepted。

**Tech Stack:** Go1.26、net/http、httptest；Python标准库；GitHub Actions。

**Spec:** `docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md` 3.2/3.6/3.7/15；`docs/design/M0-Change-Map-2026-10-29.md`。

## Global Constraints

- 不改internal、原生业务逻辑、根go.mod/go.sum、原有BASELINE和冻结方案。
- extensions module名称不变、不import WeKnora、不加第三方依赖或go.work。
- 保留127.0.0.1:8090与GET /bridge/healthz；增加/healthz、/readyz。
- `X-AgentRAG-Bridge-Version: 1`；缺失/错误版本400；缺服务503；不伪造许可或持久化ACK。
- Host单测不替代原生Fail-Closed；扩展检查不替代Lite/SQLite混合并发验收。

## Review Focus

- 缺失、空、多值、未知major版本不得进入业务处理。
- 没有治理时不得许可，没有持久化时ingest不得ACK。
- 存活不等于就绪；错误不泄露输入/配置。
- 模块门禁调用真实Go查询，不能只搜索import源文本。
- CI进入嵌套module；race仅在有CGO的Linux，本机无C编译器时如实报告。
- 对账完整分页后才观察，不伪造历史；对账失败可观察并在下周期重试。
- 扩展标签单队列不宣称解决原生UI并发；原生tag ID与命名空间标签名分开。
- 排除只从已授权集合做减法，跨tenant/KB和空结果必须拒绝；未接线不宣称过滤已在核心生效。

## Task 1: M0-02A Protocol boundary

**Files:** 修改 `extensions/bridge/server.go`；新增 `protocol.go`、`server_test.go`。

**Interfaces:** 保留 `bridge.NewServer() *http.Server`；公开版本常量。当前固定拒绝，不提前冻结业务DTO/策略接口。

- [x] 写真实HTTP测试 TestBridgeHealthAndReadiness、TestBridgeVersionGate、TestBridgeUnavailableServices。
- [x] 运行bridge测试，确认新增路由/版本gate缺失导致预期失败。
- [x] 实现存活别名、readyz503、版本校验、四个未配置业务端点503，错误只含固定code。
- [x] Go suite通过；37个HTTP子用例覆盖旧探针、重复版本、合法版本未配置拒绝、405/404。

Expected：健康200、就绪503、错误版本400、业务未配置503、方法不支持405、未知路径404。

## Task 2: M0-06A Independent module guard / CI

**Files:** 新增 `extensions/scripts/check_module_boundary.py`、`check_module_boundary_test.py`、`.github/workflows/extensions.yml`；更新AGENTS对应检查命令。

**Interfaces:** `python scripts/check_module_boundary.py [--module-dir PATH]`：合法0，违规非0；测试使用临时module。

- [x] 先写合法、错误身份、WeKnora require/replace（含replacement target和本地别名）、workspace的7个CLI fixture测试。
- [x] Python测试确认CLI未实现导致预期失败。
- [x] 实现只读go env、go mod edit -json、go list -mod=readonly -deps；拒绝错误身份/workspace/WeKnora module/import。
- [x] 新增Ubuntu/Windows专用workflow：GOWORK=off、build/test/vet/format/门禁/Python测试；仅Ubuntu跑race。
- [x] 本地实际module正向检查、完整Python/Go suite、格式检查通过；远端CI未运行。

Expected：合法module通过；负向fixture因指定边界拒绝。workflow本地可解析不等于远端已执行。

## Task 3: M0-03A Reconciliation component

**Files:** `extensions/ingest/reconcile.go`、`reconcile_test.go`。

**Interfaces:** `Source.List(ctx, scope, page) (Page,error)`、`Observer.Observe(ctx,Observation) error`；`Reconciler.Sweep`和`Run`。Observer须经未来持久WriteExecutor幂等处理；当前不连接真实公共API或自动推进治理状态。

- [x] 先写分页/错误/重复文档/跨scope/周期失败后恢复测试；运行后因尚无实现失败。
- [x] 实现完整扫描校验后交付origin=reconcile观察，错误报告、下周期重试和取消退出。
- [x] 补空非终页案例，确认RED（错误接受并写入），修正后完整Go suite GREEN。

Expected：不完整扫描写入计数0；周期失败可观察且后续恢复；不推断删除、不伪造中间历史。实际HTTP Source/持久Observer/重启状态收敛后续另验。

## Task 4: M0-04A Tag projection queue component

**Files:** `extensions/taggov/writer.go`、`writer_test.go`。

**Interfaces:** `Store.ReadTags`/`ReplaceTags`；应用层共享一个 `Writer`，通过 `Apply`排队；`Close`停止并返回未完成队列的错误。原生ID写入，命名空间依据KB已解析的Tag.Name。

- [x] 先写业务标签/其他gov保留、1000并发更新、上游错误、关闭拒绝、命名空间越界测试并运行RED。
- [x] 实现单worker完整read/merge/replace/verify、固定gov命名空间校验和错误传回；不增加原生CAS。
- [x] Go suite通过：1000次实际组件Apply，Store最大并发1、写入计数1000；这是组件接口测试，不是真实DB写入。

Expected：只序列化扩展写入；原生UI/自动标签竞态仍为接受的残余风险。实际公共API Store、跨库ID映射、Outbox待接线。

## Task 5: M0-02B Exclusion set component

**Files:** `extensions/retrieval/exclusions.go`、`exclusions_test.go`。

**Interfaces:** `Exclusions.Apply(scope, authorizedIDs) (Selection,error)`；Selection包含显式KnowledgeIDs和DenyAll。不调用核心、不枚举整库，也不将gov状态与parse_status混为一谈。

- [x] 先写集合减法/未知ID不扩权/去重/空集合拒绝/跨tenant与KB拒绝并运行RED。
- [x] 实现纯扩展集合计算，Go suite GREEN。

Expected：结果是已授权集合子集；空集合DenyAll。原生未贯通excluded_document_ids，薄适配和端到端验证后续实施，不宣称全工具受控。

## Completion

- [x] 更新TASK_BACKLOG细分状态/验证证据/用户决定；PROJECT_CONTEXT只更新已实现事实和已确认取舍。
- [x] 完成全批只读复核；独立Go suite通过，无代码阻断，已修正文档M0范围不一致。
- [x] 报告C-09字段接线差异和未验证项；不擅自修改原生代码补齐。完整M0仍未完成。
