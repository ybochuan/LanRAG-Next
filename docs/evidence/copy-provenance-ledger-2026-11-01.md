# 同步来源账本：扩展内验收记录

日期：2026-11-01。源码HEAD：`fa6353e8c821daa5eeb667dc46140e4d0d32f179`，`v0.9.0-2-gfa6353e`，已有工作区修改保留。

结论：**用户批准的COPY-01与COPY-02A扩展基础已完成，离线验证通过；原生副本治理尚未接线，M2整体未完成。** 没有启动新Host、替换Core、升级真实数据库或提交/推送。

## 范围与文件

| 路径 | 职责 |
|---|---|
| `extensions/provenance/types.go` | 独立scope、binding、source/copy proof、请求/回执/决策和可信Authority端口 |
| `extensions/provenance/storage.go` | 请求规范化、事务读取、幂等回执、审计及持久对象解析 |
| `extensions/provenance/source.go` | 不可变文档源版本、单调native_sequence、当前源指针CAS和可信证据绑定 |
| `extensions/provenance/manifest.go` | pending/ready清单、父源合并、禁止删源/换owner、清单指针CAS及原子提交 |
| `extensions/provenance/policy.go` | 每次读取检查当前清单/祖先版本、完整源集、active、日期、owner/session/resource/版本/hash |
| `extensions/store/migrations/006_copy_provenance.sql` | 8张独立ext表；源/清单/父引用/回执的组合scope FK；审计和操作唯一性 |
| `extensions/store/{migrate,manual_schema}.go`及迁移测试 | schema5→6、严格手工DDL前缀认领，保留已有事实 |
| `extensions/provenance/{ledger,transactions,recovery}_test.go` | 业务、真实SQLite并发/SQL故障、真实子进程退出恢复 |
| `extensions/cmd/review-alerts/{main,main_test}.go` | 只读CLI显式兼容schema5/6、拒绝未知7；不迁移、不改DB |

没有改动根module依赖，没有新增HTTP路由或可信生产适配。schema6只在临时测试库应用；下一次使用新源码启动worker会按既有迁移器升级独立扩展库。旧版本写进程不能混用，降级需使用升级前备份。

## 信任边界与未覆盖项

`Authority.CurrentSource`必须提供真实当前文档版本证据，`Authority.SealedCopy`必须来自可信协调层的完整源集封存；它们不是允许浏览器/模型自报`complete=true`的接口。当前只有测试夹具实现，生产适配留待原生接线及M3版本权威性设计。nil authority只可保存pending草稿，不能登记可信源或生成ready。

`CheckRead`只收窄许可，不替代原生tenant/user/session/文件授权。回执证明历史提交，不是当前读取许可；重放回执不能跳过重新检查。所有证据都在扩展模块内，不能据此宣称真实workspace/resource/记忆/shell/Web缓存已受控。

未验证：物理断电与硬件持久性、源版本权威适配、封存过程完整性、跨模块文件上传原子性、Host业务契约、真实原生副本撤销、旧grant/人工下载和生成中撤销。来源更正会保守阻断旧子副本；不会自动补写或恢复历史副本。

## 定向行为与故障验收

| 场景 | 验证方法与实际结果 |
|---|---|
| 缺证据及伪造 | nil authority仅pending；proof的scope/doc/copy/binding/evidence不符、空源、缺版本均拒绝，无法发布ready |
| Fail-Closed | pending、未知、非active、版本变化、绑定不符、父版本更正、日期越界、关闭DB均拒绝；DB错误返回`ErrUnavailable`且Allowed=false |
| 当前日期 | active尚未被每日扫描转stale时，effective_until已过仍拒绝；未来effective_from也拒绝 |
| 当前源版本 | v1→v2使旧副本失效；旧native_sequence不能覆盖新head；跨tenant/KB的FK和查询不混用 |
| 继承与更正 | 多源及副本再复制合并完整源集，覆盖删源拒绝；父清单添加来源后旧子清单失效；候选head导致祖先版本失配/当前依赖环时整个写入回滚 |
| 同键并发 | 16个并发相同operation请求只产生一份事实、回执和审计；改变payload复用同键拒绝 |
| CAS并发 | 16个不同operation竞争同一revision，1成功/15冲突；源指针也验证；同一SQLite文件上两个独立DB writer再测单赢家 |
| SQL故障 | 清单/源引用/父引用/head/回执/审计6个故障点以及源版本/head/回执/审计4个故障点注入SQLite abort；行数/FK验证没有部分提交，修复后原操作可重试；失败返回零回执 |
| 真实进程退出 | 自有临时DB子进程在事务内审计前`os.Exit(77)`，或提交后ACK前`os.Exit(78)`；重新打开验证0或1次提交、FK有效、重试只保留1份事实 |
| 迁移 | 真实有历史的schema5库升级6保留生命周期/规则事实；完整DDL前缀6可严格认领；不清空历史或吞掉不一致 |
| 只读工具兼容 | schema5/6查询通过、未知7拒绝且无输出；查询前后主DB字节不变 |

以上均为实际测试，未以race代替事务或并发验证。跨scope拒绝不代表已经实现跨库授权流程；当前默认不支持跨库/租户来源。

## 验证命令与退出状态

除特别标注外，目录为仓库`extensions/`。最后一次完整运行结果：

| 命令 | 退出状态与结果 |
|---|---|
| `go test -mod=readonly ./cmd/review-alerts ./provenance ./store -count=1` | 0，三个包通过 |
| `go build -mod=readonly ./...` | 0 |
| `go vet -mod=readonly ./...` | 0 |
| `go test -mod=readonly ./... -count=1` | 0，17个包通过；parser-calibrate无Go测试文件，未当作覆盖 |
| WSL Ubuntu：`CGO_ENABLED=1 go test -mod=readonly -race ./... -count=1` | 0，全部有测试的包通过；使用WSL Go/GCC |
| `gofmt -l .` | 0，无输出 |
| `python scripts/check_module_boundary.py` | 0，312 imports，无WeKnora依赖、无启用workspace |
| `python -m unittest discover -s scripts -p '*_test.py' -v` | 0，33项中30通过、3项平台/显式启用测试跳过 |
| `go env GOMOD GOWORK` | 0，GOMOD为`D:\LanRAG-Next\extensions\go.mod`、GOWORK为空 |
| 仓库根：`git diff --check`、`git status --short --untracked-files=all`、`git diff --name-only` | 0；15个新增准确路径均为扩展或设计/验收文档，无既有状态路径丢失；根go.mod/go.sum的SHA256与开工一致 |

私有原始终端记录保留在忽略目录：`tmp/copy-ledger-extension-tests-final.log`、`tmp/copy-ledger-race-final.log`、`tmp/copy-ledger-python-final.log`。第一次完整回归发现review-alerts仅接受schema5，Windows/Linux均退出1；显式兼容5/6并保留7拒绝测试后，上表完整回归退出0，没有忽略该失败。

只读检查现有Core的`127.0.0.1:18880/health`、WSL内Host的`127.0.0.1:8090/health`均HTTP200。这只证明旧服务存活，不是新账本已部署或业务接线完成。

一次独立只读实现复核未发现Critical/Important；只读CLI兼容修复另作定向复核，结论一致。它不审核尚未实现的生产Authority或原生策略边界。

原生后端另行完整两包回归退出1，49个顶层失败尚待逐组归因；见`m2-native-baseline-2026-11-01.md`。前端本轮没有新改动，未重复完整构建或浏览器验证；已有结果见`m2-frontend-gates-2026-11-01.md`。

下一步必须先具体审阅并确认COPY-02B/03原生与可信来源适配清单，再谈真实副本E2E。当前不关闭任何现有副本能力，不把离线ready状态等同于已向原生发布。
