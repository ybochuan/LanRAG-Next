# T4-01/02 故障矩阵补全（2026-11-16）

## 范围

本文件补上 T4-01/02 记录中「seal/producer 完整 SQL 故障、多进程/容量矩阵及真实原生 E2E 尚未全部覆盖」的缺口。改动只新增测试，未改生产代码。

## 新增矩阵

### 1. 封存（seal）完整 SQL 故障矩阵

`extensions/provenance/matrix_test.go::TestSealSQLFailureLeavesTheOpenRunUntouched`：对封存事务写入的每张表（`ext_provenance_runs`、`ext_provenance_run_audit`）逐张注入失败，断言：

- 封存返回错误，不返回快照；
- run 仍为 `open`、revision 不变；
- 所有 run 相关表的行数与故障前逐表相等（含步骤、输入、输入意向、生产者），且外键检查为空；
- 移除故障后同一请求可成功封存为 `sealed`。

### 2. 生产者（producer）完整 SQL 故障矩阵

`TestProducerSQLFailureLeavesNoReservationOrCommit`：对 reserve 与 commit 两条路径分别注入 `ext_provenance_runs`、`ext_provenance_run_audit`、`ext_provenance_producers`：

- reserve 失败：不产生 producer 行、head revision 不变、各表计数不变；移除故障后同一 reservation 可成功重试。
- commit 失败：head revision 不变，producer 仍为 `reserved`（不是 `committed`）；移除故障后可直接重试提交，无需重复底层写入。

### 3. 容量矩阵

`TestRunAuthorizationCapacityLimit` 与 `TestProducerCapacityAndPathRules`：

- 授权文档 257 个被拒（`ErrInvalid`），且不写任何 run 行；空授权集合同样被拒（空集不等于整库授权）；256 个被接受。
- 生产者父副本 17 个被拒（上限 16）。
- 路径规则：绝对路径、`../` 逃逸、反斜杠、冒号流、超过 1024 字节均被拒；规范相对路径通过。

### 4. 跨进程 CAS

`TestSealCASAcrossProcesses`：父进程准备一个待封存 run 后关闭数据库，启动真正的子进程（`os/exec`，同一测试二进制）完成封存并以退出码报告结果；父进程重新打开数据库后必须看到 `sealed` 且 revision 前进，再用旧 revision 封存必须得到 `ErrConflict`。这验证 CAS 跨操作系统进程成立，而不只是跨事务或跨连接。

## 一个被测试暴露的真实问题

故障矩阵的第一版只注入 `BEFORE INSERT` 触发器，结果 `ext_provenance_runs` 与提交路径的 `ext_provenance_producers` 用例「通过」了——因为封存和提交对 run head 与 producer 是 **UPDATE**，INSERT 触发器根本不会触发。测试若照此发布，会给出虚假的绿色。已改为同时注入 INSERT 与 UPDATE 触发器，重跑后六个子用例全部真实经过故障路径并通过。

这本身是这次补矩阵的价值：旧记录里「seal/producer SQL 故障未完全覆盖」的缺口，正是指这类只在部分写路径生效的故障注入。

## 真实原生 E2E 证据（生产库）

生产扩展库中已存在一条**真实原生 agent 运行**产生的封存记录：

| 项 | 值 |
|---|---|
| run | `d25ddecb601934e2…` |
| 状态 / revision | `sealed` / 246 |
| 封存时间 | 2026-11-16T07:47:12Z |
| 授权类型 | `knowledge_base` |
| 来源 | 2 个真实文档版本（`9e63c31f…`、`aecd03d7…`） |
| 步骤 | 3 个 model success、2 个 tool success |
| 已登记输入 | 117 条 |

同一时刻的 run 状态分布：`sealed` 1、`open` 6、`unattributed` 62；`ext_provenance_producers` 仍为 0 行——写入 producer 的生产闭环尚未跑通（属 P1b，需要真实检索输入证据）。

## 验证命令与结果

```text
gofmt -l extensions/                               → 无输出
CGO_ENABLED=0 go build -mod=readonly ./...          → 退出 0
CGO_ENABLED=0 go vet -mod=readonly ./...            → 退出 0
CGO_ENABLED=0 go test -mod=readonly ./... -count=1  → 退出 0（含新增 10 个子用例）
python3 scripts/check_module_boundary.py            → module boundary passed: 302 imports
```

新增测试清单：封存 2 个子用例、生产者 6 个子用例、容量 2 个用例、跨进程 1 个用例，共 11 个。

## 边界

- 本轮只新增测试，未改生产实现；因此不改变任何运行时行为。
- 容量矩阵在测试库上验证上限，不代表生产负载压测；生产并发/容量结论仍需专门压测。
- 跨进程用例是同一主机上的子进程，覆盖 SQLite 文件级 CAS；不覆盖多主机或网络文件系统。
- 真实原生 E2E 的封存记录来自一次真实 agent 运行，但该运行的模型是本地 Qwen3.5-9B，不是云端模型；它证明的是 provenance 链路，不是模型质量。
