# M2 生命周期内核验证 — 2026-10-31

结论：M2内核组件交付；M2整阶段尚未验收。未新增原生补丁，保留工作区既有M1改动；未提交或推送。基点 `fa6353e8c821daa5eeb667dc46140e4d0d32f179`，describe `v0.9.0-2-gfa6353e`。

## 已验证行为

- 标注表驱动默认矩阵：pending→active/scheduled、scheduled到期→active、active超过截止→stale；提前激活拒绝。截止与review严格按now > deadline，review不改变状态。
- 未配置永久；默认日期只在Initialize补缺、显式日期优先。KB行完整覆盖tenant；修订CAS拒绝旧配置。
- 精确命令在后续规则/时间/状态改变后仍无副作用重放；同事件改trigger等数据拒绝。expected head防ABA，annotation revision及resolved token在同一事务校验。
- 24个真实SQLite并发迁移仅1提交；24个并发标注替换仅1提交。不是仅靠race判定事务正确。
- annotations、decision、event、current、Outbox五处SQL触发器故障全部回滚。
- 真实Apply子进程在current写入前通过SQLite函数os.Exit(77)，重开五表均0；提交后os.Exit(78)，重开五表均1。两者foreign_key_check无违规；仅证明进程崩溃恢复，不代替物理断电。
- v1/v2/v3升级至schema4；v3已有MinerU产物字节/版本与旧生命周期head保留，不捏造标注；跨tenant决策FK拒绝。
- Document/Guidance stale独立；补充通知显式同库parent evidence评估，跨tenant拒绝，没有声称完成M3权威血缘查询。
- Outbox取消误报先RED（report1而期望0），修复后GREEN；业务失败join cancellation仍报告，旧drain严格断言未放宽。

## 本轮完整命令与结果

除注明外工作目录为 `D:/LanRAG-Next/extensions`。

| 命令 | 退出状态／结果 |
|---|---|
| `go test -mod=readonly ./outbox -run TestParentCancellation -count=1`（修复前） | 1；graceful cancellation reported as 1 projection errors，预期RED |
| `go test -mod=readonly ./lifecycle -count=1`（实现前） | 1；Machine/Configuration/Annotations未定义，预期RED |
| `go test -mod=readonly ./store -count=1`（更新升级断言前） | 1；实际schema4，旧断言仍要求3；随后更新并补v3真实数据保留测试 |
| `go test -mod=readonly ./... -count=1` | 0；所有有测试的包通过；cmd/parser-calibrate无单独测试文件，其行为测试位于parser包 |
| `go build -mod=readonly ./...` | 0 |
| `go vet -mod=readonly ./...` | 0 |
| `wsl -d Ubuntu --exec bash -lc 'cd /mnt/d/LanRAG-Next/extensions && PATH=/usr/local/go/bin:$PATH go test -mod=readonly -race ./... -count=1'` | 0；包括原Outbox drain严格断言与M2实际事务/子进程测试；无DATA RACE |
| `python scripts/check_module_boundary.py` | 0；308 imports，无WeKnora依赖/启用workspace |
| `python -m unittest discover -s scripts -p '*_test.py' -v`（Windows） | 0；26项，SIGTERM Linux语义1项skip，其余通过 |
| 同一Python回归在WSL Ubuntu以python3执行 | 0；26项，Windows sharing/deletion语义1项skip，其余通过，SIGTERM用例实际通过 |
| `gofmt -l .` | 无输出 |
| `go env GOMOD GOWORK` | extensions/go.mod；GOWORK空 |
| 根目录 `git diff --check` | 0；仅Git换行符提示，无空白错误 |

## 一次独立复核

只读复核M2切片（未全面审查原有M1工作），Critical0 / Important0 / Minor0；独立 `go test -mod=readonly ./lifecycle ./store ./outbox -count=1` 退出0。没有第二次复核或待修复项。

## 范围与剩余门禁

本批生产代码只新增/修改extension内核、扩展迁移及Outbox取消分类。根模块、BASELINE、Frozen SHA256与开工核对值相同；`internal/` tracked diff仍仅既有M1的knowledge_process.go(12+/1-)和mineru_converter.go(5+/1-)，既有未跟踪parser_proxy客户端/测试保留。

未运行原生/前端构建与全量测试、MinerU上传/RAG E2E、生产负载、物理断电或远端CI。本批未接Observer、周期扫描、review告警投递、配置鉴权API、active-only检索。当前Host不会自动应用M2业务迁移；不能据内核通过宣称M2整阶段或M1已验收。

实施决定：日期默认补齐锚定初始化时钟（valid_days优先从effective_from算），后续规则不追溯改写；已过期pending/scheduled安全拒绝激活，其直接stale处置规则留待明确。legacy Store.Append保持低层兼容，不能作为全局政策门禁。这些边界均同步到设计与backlog。
