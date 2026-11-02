# M2 T3 补充依赖交付与验收（2026-11-01）

本轮已获用户明确批准T3管理薄接口、扩展和界面联动；依照`../design/M2-Supplement-Integration-2026-11-01.md`实施。T3交付不等于M2整体验收：T4完整运行来源封存/真实副本门卫、T5生成中撤销仍待对应实施和验收。

## 交付

| 路径 | 结果与验证 |
|---|---|
| `extensions/store/migrations/009_supplements.sql`、`lineage/supplements.go` | 显式同库父文档0–8个、版本绑定、CAS/幂等；声明、父列表、current、critical supplements edge/linkage和audit同事务。16真实并发写者1成功/15冲突；6个SQL故障点全回滚；子进程提交中exit77、提交后ACK前exit78，重开后无半提交且重试只有一份事实。 |
| `extensions/dependencies/assessment.go` | 单事务读取完整依赖闭包，拒绝缺失/落后current、scope错误、环、深度>10和唯一节点>64；共享祖先深路径也受限。父非active、日期失效或引用版本不再当前均使依赖无效。 |
| `extensions/lifecycle/{machine,automation}.go` | 依赖失效决策在状态机事务内重查，把pending/scheduled/active转stale；保留event/current/outbox原子性。不复活终态；父review只提醒。 |
| `extensions/retrieval/policy.go`、`host/{runtime,dependencies}.go` | worker使用真实Core快照Registry；每次许可先核验绑定源，最终DB快照重查状态、版本和依赖闭包。Core/Host/DB错误拒绝。每日先刷新来源再扫描；Core离线只上报刷新失败，不伪造永久失效；初次local ready不依赖Core。 |
| `extensions/review/` | 取自身与父级最早复核日期，扫描/列表/投递均核对当前日期；不写父子标注、不改变生命周期。009增加review subject，迁移保留005旧提醒，避免为继承提醒虚构子标注。只读工具支持真实schema5–9，未知10拒绝且不写库。 |
| `extensions/management/server.go` | 服务端凭据/版本/scope/只读守卫后调用声明服务；返回当前关系revision和幂等receipt。 |
| `internal/handler/extension_lifecycle.go`、`internal/extensionbridge/management.go`、`internal/router/routes_knowledge.go` | 仅增加GET/PUT supplements的JWT管理转发；actor由原生登录上下文生成，逐个核对child和所有parent归属，浏览器不能传tenant/actor/CanWrite。无原生业务算法或表变更。 |
| `frontend/src/views/lifecycle/SupplementEditor.vue`、同目录model及`frontend/src/api/lifecycle.ts` | 详情抽屉增加同库active候选分页/本页筛选、最多8篇选择、当前父状态/日期与版本失效原因、CAS保存与冲突重载。日期与标签不被依赖编辑覆盖。 |

## 本轮验证

所有命令从仓库根或注明目录执行，WSL使用Go1.26；不更换生产Core/Host，不迁移真实用户库。

| 命令／范围 | 结果 |
|---|---|
| extensions：`go build -mod=readonly ./...`、`go vet -mod=readonly ./...`、隔离网络中 `go test -mod=readonly -p 1 ./... -count=1` | 全通过、退出0；20个包有通过测试，3个包无测试文件。最后完整test见`tmp/t3-extension-isolated.log`。 |
| extensions：隔离Linux网络空间，`go test -mod=readonly -race -p 1 ./... -count=1` | 退出0，`tmp/t3-race-isolated.log`；完整包覆盖，未跳过模型管理器。普通并行test/race曾因已有modelsupervisor `TestObservePortsReportsOnlyListening`/`TestProbeReportsNothingListening` 的关闭端口探测而失败；无DATA RACE报告，未改其代码/断言，隔离串行复测通过。 |
| 原生：`CGO_ENABLED=1 SSRF_DISABLE=false go test -mod=readonly -tags sqlite_fts5 ./internal/handler ./internal/router ./internal/extensionbridge ./internal/application/service/chat_pipeline ./internal/agent ./internal/agent/tools ./internal/application/service ./internal/mcpserver -count=1` | 8包通过、退出0；新增supplements身份/只读/父越权/伪造actor/缺CAS/null/超限体额外定向复测退出0。 |
| frontend：`npm test`、`npm run type-check`、`npm run build` | 2370测试：2369通过、1既有跳过、0失败；类型/构建退出0。构建保留大chunk警告。 |
| extensions：`python3 scripts/check_module_boundary.py`、`python3 -m unittest discover -s scripts -p '*_test.py' -v` | module边界退出0；GOMOD为extensions/go.mod、GOWORK空；33个Python用例中31通过/2平台或opt-in跳过。 |
| 候选Lite构建：`CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' -o tmp/t3-WeKnora-lite ./cmd/server` | 退出0；仅候选输出，不替换正在运行的二进制。 |

### 真实上传与模型输入验收

```bash
unshare -Urn /bin/bash -c 'ip link set lo up && CORE_BINARY=/mnt/d/LanRAG-Next/tmp/t3-WeKnora-lite python3 extensions/scripts/m2_supplement_e2e.py'
unshare -Urn /bin/bash -c 'ip link set lo up && CORE_BINARY=/mnt/d/LanRAG-Next/tmp/t3-WeKnora-lite python3 extensions/scripts/governance_e2e.py'
```

两者最后完整运行均退出0。前者完整非敏感输出保存在`m2-supplement-e2e-2026-11-01.log`；后者日志`tmp/t3-governance-e2e.log`。

- 独立Core/worker Host/SQLite，公开注册登录，真实上传/原生分块/Observer active/快照版本/管理声明；精确重试幂等、旧CAS409。
- 有效父级时，子正文进入RAG与Agent search/read，描述进入Agent list的后续模型请求，证明正向能力可用。
- 父仍active但原生chunk被编辑，实时源核验发现变化；同会话/已知ID下RAG和Agent search/read/list的所有模型请求均不含子正文、描述或旧答案标记，重排输入也无子内容。
- 另上传新子文档验证父日期过期；立即拒绝内容，实际Machine扫描使子stale；用户标签保留。版本变化后已转stale的先前子文档不为复用测试而复活。
- 原有治理联合E2E涵盖active/真实stale/fallback摘要/Agent工具/Host离线；离线answer generation为0。
- 脚本曾把Observer异步建档前404当终止错误，现按状态码等候；未降低业务断言。模型为可捕获所有请求的本地确定性夹具，不声称云模型验收。

## 真实组件界面点击验证

使用临时Vite入口加载仓库中的原版`SupplementEditor.vue`及原版API/request代码，接口仅返回合成文档，不连接用户服务。通过内置浏览器实际点击：

1. 未声明 → 选择父文档 → 保存，关系revision0→1且显示成功；
2. 模拟另一写者使服务端revision前进，本地旧revision提交收到409，显示“尚未覆盖”并禁用保存；
3. 点击重新载入，读取最新revision2，允许继续编辑；
4. 移除已选父文档并保存，revision3、父列表为空；重新选择保存后revision4；
5. 切换只读props，父证据仍展示，候选列表与保存按钮均不出现。

截图：`m2-supplement-ui-saved-2026-11-01.jpg`、`m2-supplement-ui-conflict-2026-11-01.jpg`、`m2-supplement-ui-readonly-2026-11-01.jpg`。临时页和服务已关闭。该验证是实际组件+合成HTTP接口，前述上传E2E是实际Core/Host；不混称真实用户库的完整浏览器验收。

## 复核、兼容与边界

一次独立最终复核发现共享祖先深度绕过、并发父闭包扩大缺本次来源核验两项Important；均新增测试先RED，再补缓存子树高度/最终来源覆盖检查，相关套件GREEN，之后重新跑全量及真实E2E。

007 SHA256仍为`125B9261D2A8D00DB7D560511514BF57385EEBCE5EFA95397C14556506993663`；008仍为`FCB187944A77C326554A61479BA69BB52F8178C247B30439557709B3D3AFA0F4`；根go.mod/go.sum保持本批起始hash。未修改Frozen、原生DB、RRF/检索/Agent/分块算法，未提交推送。

全仓`git diff --check`退出2：既有/并行前端menu、i18n、router/store等行尾空白，未顺手格式化。T3文件格式及定向范围检查通过，最终status/name列表保留于tmp。

未执行真实生产库升级、完整原生App的真实用户库浏览器逐项点击、根模块全部`go test ./...`/make test、PostgreSQL服务或物理断电验收。显式复制副本、完整run源集、生成中持续撤销、M4语义关系/图谱不因T3交付而宣称已受控。依赖闭包过大或500ms总许可时限不足时安全拒绝，不回退透传。
