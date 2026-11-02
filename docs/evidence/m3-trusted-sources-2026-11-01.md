# M3 T1/T2 可信来源基础验收

2026-11-01，HEAD `fa6353e8c821daa5eeb667dc46140e4d0d32f179`、`v0.9.0-2-gfa6353e`。用户先选择连同M3基础完成M2，再明确批准T1/T2原生只读接口及扩展观察版本。

**T1/T2通过；M2整体与M3整体尚未完成。** T3补充通知接线已另获批准并继续实施；完整run来源封存、原生副本门卫、生成中撤销仍是后续验收。

## 交付与真实边界

- 原生新增 `internal/handler/extension_source_snapshot.go`，既有handler只注入现有DB、路由只挂一次GET。原生授权之后单次一致读事务，指纹包含模型可见header、所有live chunk类型与显式字段；只返回hash，未写原生表或改算法。
- schema8及`extensions/lineage/`保存不可变已观察版本/family/evidence/audit。采集从BEGIN IMMEDIATE后开始，跨独立DB writer串行；普通编辑只推进观察head，不伪造supersedes或发布事实。
- `lineage.Authority`通过真实原生快照获得当前版本；来源许可先校验精确绑定，再实时核对原生root，最终DB view重查权威头。Go字段改为ObservationSequence，旧native_sequence物理列和原事实保留；旧证据不自动升级，新的真实核验可承接旧序号上界重新登记。
- `/provenance`和`/version-ancestry`为分边类型的Go查询接口，带深度/节点预算与环防护；未挂Host HTTP API。derived_from只提供可重建投影方法，不承担许可。
- 完整运行封存尚无生产provider；nil sealed明确拒绝模型complete声明。真实资源/文件/记忆副本门卫未接管，图片/对象存储字节也不能只凭DB指纹证明。检查后的新变更、云端已收到的字节和持续流撤销不计入本验收。

原生输入限制：4096 chunks、hash输入64MiB、响应2MiB、查询500ms；completed之外、index状态非显式ready、缺失/超限/错误都拒绝。同事务先统计chunk数量与字节再加载正文，不截断成功。PostgreSQL设置repeatable-read，但本轮实际并发/E2E仅SQLite；物理断电未验。

## Harness 007/008 核对与C-37

007为Harness生命周期current写入守卫，未由本任务修改。008当前仍是本任务来源版本表，未发现同名迁移或表冲突；注册、手工前缀认领和CLI已适配schema8。两个当前SHA256：

```text
007 125B9261D2A8D00DB7D560511514BF57385EEBCE5EFA95397C14556506993663
008 FCB187944A77C326554A61479BA69BB52F8178C247B30439557709B3D3AFA0F4
```

新发现C-37：写入触发器不修复旧损坏投影。临时库首先复现“active状态与事件不符”“active头落后于最新stale事件”仍检索/读取副本；复核再发现“有stale历史但缺current”会被状态机当新文档初始化。均先RED，再由`store/lifecycle_current.go`共享读取验证修复：不一致拒绝，不当作新文档，不重写事件或自动修复真实库。生产检索、来源许可、M2状态机/Store和M3登记均使用同一验证。

CLI兼容测试真实构造schema5/6/7/8，只读查询通过、主DB字节不变；未知schema9拒绝。schema6/7含旧source、ready manifest及引用的升级保留原字节/FK，不生成可信证据。

## 并发、故障与代码复核

- 16并发刷新、两个独立SQLite writer，当前相同root只登记一份version/audit；新root只推进观察序号。
- 五个版本/证据/audit SQL abort点全部回滚；真实子进程事务内退出77和提交后ACK前退出78，重开核对全部事实/FK，重试仅一份事实。
- 原生SQLite写者在header读完、chunks读取前事务更新二者：快照仍完整读到旧状态，随后请求完整读到新状态，不混合。
- 共享规范样例`contracts/bridge/v1/native-source-snapshot.json`由原生合成fixture生成，两模块分别核验；不包含真实用户数据。
- 一次独立代码复核发现两个Important：缺投影被当新文档、空index状态被当ready。二者均有RED→GREEN修复及完整回归。未以第二轮全面审查替代验证。

## 最终验证

| 工作目录／命令 | 结果 |
|---|---|
| extensions：`go build -mod=readonly ./...` | 0 |
| extensions：`go vet -mod=readonly ./...` | 0 |
| extensions：`go test -mod=readonly ./... -count=1` | 0，19包通过、3包无测试；真实native集成测试默认opt-in跳过，另行E2E实际运行 |
| WSL/extensions：`CGO_ENABLED=1 go test -mod=readonly -race ./... -count=1` | 0，19包通过；不能替代SQL事务/并发检查 |
| extensions：`python scripts/check_module_boundary.py` | 0，316 imports，无WeKnora依赖/workspace |
| extensions：`python -m unittest discover -s scripts -p '*_test.py' -v` | 0，33项中30通过、3平台/opt-in跳过 |
| 根/WSL：`CGO_ENABLED=1 go test -mod=readonly -tags sqlite_fts5 ./internal/handler ./internal/router ./internal/extensionbridge ./internal/application/service/chat_pipeline ./internal/agent ./internal/agent/tools ./internal/application/service ./internal/mcpserver -count=1`（SSRF_DISABLE=false） | 0，完整8包通过 |
| frontend：`npm test` | 0，2369项、2368通过、1既有skip、0失败 |
| frontend：`npm run type-check`／`npm run build` | 均0；构建有既有大bundle提示 |
| 根/WSL：`CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' -o tmp/m2-m3-WeKnora-lite ./cmd/server` | 0，候选未安装 |
| 新快照包定向及共享规范/并发测试 | 0 |

原始私有记录在忽略目录`tmp/m3-{all-extension-tests,all-extension-race,native-packages-final,frontend-tests,frontend-types,frontend-build,python-final}.log`，相应完整命令退出状态已实际核对。根go.mod/go.sum哈希与开工一致。

## 两项真实隔离E2E

在Windows执行（原实例8090不受影响）：

```powershell
wsl -d Ubuntu --cd /mnt/d/LanRAG-Next --exec unshare -Urn /usr/bin/env PATH=/usr/local/go/bin:/usr/bin:/bin CORE_BINARY=/mnt/d/LanRAG-Next/tmp/m2-m3-WeKnora-lite /bin/bash -c 'ip link set lo up && python3 extensions/scripts/m3_source_e2e.py'
wsl -d Ubuntu --cd /mnt/d/LanRAG-Next --exec unshare -Urn /usr/bin/env PATH=/usr/local/go/bin:/usr/bin:/bin CORE_BINARY=/mnt/d/LanRAG-Next/tmp/m2-m3-WeKnora-lite /bin/bash -c 'ip link set lo up && python3 extensions/scripts/governance_e2e.py'
```

均退出0，使用真实候选Core、真实worker Host、临时账号/SQLite、原生上传/分块与输入捕获模型夹具。没有访问真实用户文档或调用DeepSeek/MinerU，不作生产模型联合验收声明。

第一项证明上传→Observer active→真实授权快照→不可变观察版本→来源账本；原生修改chunk后新root/版本产生，旧证据拒绝，普通编辑不改变active。随后原界面测试日期stale、标签保留、事件、真实8写者1个200/7个409、跨scope/角色拒绝均通过。日志：`tmp/m3-source-e2e-final.log`。

第二项RAG摘要/正文、Agent search/read/list active正向通过；真实stale后同会话/已知ID在所有模型和重排输入均无正文/描述/旧答案canary；断Host后回答生成调用0。日志：`tmp/m3-governance-e2e.log`。WSL输出一次systemd用户会话提示，但隔离服务和脚本均成功完成，不将其混作业务失败或修复。

## 原生测试环境清理

原49项顶层失败：46个Sandbox测试改用DNS-free TEST-NET夹具、DataSource/MCP两个测试精确允许example.com，WSL补齐python3.14-venv/ensurepip。业务断言和生产保护均保留；无须改原生算法。

扩大至8包还发现4项：能力清单测试先去CRLF再去逗号；解析密钥保留测试加精确域名fixture；两个MCP测试先验证生产拒绝，再以私有context/确切scope fixture保留原协议、审批、RBAC、缓存和OAuth检查。Wiki继续精确拒绝，底层读取0。修复均仅测试文件，最终8包组合退出0；不以忽略失败声称通过。

完整上游全仓Go测试、make test、真实云模型、PG和物理断电未执行。工作区另有并发前端改动造成全局`git diff --check`尾随空白诊断，保留未改；本任务具体文件范围单独核对。无提交/推送、真实库迁移或服务替换。
