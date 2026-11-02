# M2自动迁移、数据库策略与原生消费 — 2026-10-31

本轮用户批准三项接线及原生极薄补丁。已交付本批范围；M2整个阶段未因此完成，M1未验收事项保持。HEAD为fa6353e8c821daa5eeb667dc46140e4d0d32f179，无提交推送。

## 行为与验证路径

| 能力 | 实现与证据 |
|---|---|
| 过期规则 | lifecycle/machine.go、automation.go；pending/scheduled/active超过effective_until直接stale，过期优先；复核不自动迁移 |
| Observer | ingest/lifecycle.go适配持久Observer；完成观察初始化pending，再按标注进入active/scheduled/stale。重试不重复事实；观察已提交但业务失败时同观察恢复；迁移同事务核对最新completed来源时间 |
| 每日任务 | Host worker启动立即补扫，之后每24h分页扫描配置tenant/KB；同一Machine事务与CAS，取消时join后关DB。不是精确到秒或本地零点cron，过期收敛最多一个周期 |
| 定时激活安全 | scheduled→active同决策事务确认最新持久解析观察completed；failed/processing/缺失拒绝；三例先RED复现错误active，修复后GREEN。过期转stale不依赖解析成功 |
| DB策略 | retrieval/policy.go查询ext_lifecycle_current；只保留当前active，非active/未知/缺失均排除；tenant/KB/已授权候选有界，DB故障不能permit |
| Host部署 | worker使用独立FULL SQLite、真实策略/观察/扫描；无DB的probe返回业务503，健康200；不再默认生产透传 |
| 原生消费 | extensionbridge/client.go、response.go；500ms、固定回环、禁止重定向、1MiB上限、严格非空身份/六字段JSON/不重叠完整分割；非200/断网/超时/坏响应硬错误。deny_all不执行检索 |
| 原生过滤 | search_parallel.go结果合并后执行排除集，普通文档还须在返回允许集；原生WebSearch类型保留。真实管线夹具检索4命中（active、两个stale、一个请求外文档）最终只有1个active命中；不改变检索/RRF算法 |

跨进程实际输出：[完整终端日志](m2-native-policy-2026-10-31.log)。入口为`extensions/scripts/m2-policy-e2e.sh`；真实独立Host/SQLite/Observer和原生PluginSearchParallel互通；源列表/HybridSearch与embedding返回夹具，不是生产LLM问答或原生数据库全文检索验收。

## 命令与退出状态

扩展命令目录`D:/LanRAG-Next/extensions`，原生命令目录为根目录。

| 命令 | 结果 |
|---|---|
| `go test -mod=readonly ./lifecycle ./ingest ./retrieval -count=1`（实现前） | 1，缺Sweep/NewLifecycleObserver/NewPolicy；预期RED |
| `go test -mod=readonly ./host -run TestProbeServes -count=1`（切换前） | 1，旧probe仍400/透传，新的业务503断言失败；切换后通过 |
| 原生客户端新测试（实现前） | 1，BeforeRetrieve还只有error返回，预期RED；严格响应实现后通过 |
| 原生范围补强回归（修复前） | 1，4检索结果过滤后还留2而非1，请求外文档未被判断；允许集补强后通过 |
| `go test -mod=readonly ./ingest ./lifecycle -run 'TestDailyScanNever\|TestSweepRefusesScheduled' -count=1`（独立复核修复前，同等Go正则） | 1，failed、processing、缺失观察三例错误激活；事务内检查后通过 |
| `go test -mod=readonly ./... -count=1` | 0；全有测试包通过。cmd/parser-calibrate无单独测试；可选实际Host进程/LiveHost用例平时skip，已由专用跨进程脚本执行 |
| `go build -mod=readonly ./...` | 0，Windows CGO_ENABLED=0的独立扩展编译通过 |
| `go vet -mod=readonly ./...` | 初次发现新增测试6处跨包未具名结构体，改具名后退出0，未放宽检查 |
| WSL Ubuntu `go test -mod=readonly -race ./... -count=1` | 独立复核修复后完整命令退出0，无DATA RACE |
| `python scripts/check_module_boundary.py` | 0，308 imports，无WeKnora依赖或启用workspace |
| `python -m unittest discover -s scripts -p '*_test.py' -v` | 0，Windows26项、Linux SIGTERM语义1项skip，本批Python源码未改 |
| WSL `CGO_ENABLED=1 go test -mod=readonly -tags sqlite_fts5 ./internal/extensionbridge ./internal/application/service/chat_pipeline -count=1` | 0，两个完整相关包；Host断网/超时/503、合法响应/真实过滤/deny_all均覆盖 |
| WSL同环境 `go vet -mod=readonly -tags sqlite_fts5 ./internal/extensionbridge ./internal/application/service/chat_pipeline` | 0 |
| `wsl -d Ubuntu --exec bash /mnt/d/LanRAG-Next/extensions/scripts/m2-policy-e2e.sh` | 0，实际跨进程测试PASS；不是只测200响应 |
| `gofmt -l`涉及文件、`bash -n`新脚本、`git diff --check` | 收尾格式/语法/范围检查，无上游批量格式化 |

## Lite构建

WSL Ubuntu已执行退出0：

```bash
CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 \
  -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' \
  -o /mnt/d/LanRAG-Next/tmp/m2-policy-build-9piTRr/WeKnora-lite ./cmd/server
```

构建未运行前端make、不更新web、未替换现有服务进程。部署需要同时使用本批新版Host，并提供worker配置的独立DB、原生公共API凭证和scope；不能只启动probe。

## 独立复核与边界

一次独立复核Critical0/Important1/Minor0。发现scheduled激活绕过最新解析观察；实施者一轮修复，三例RED→GREEN＋修复后全扩展suite/build/vet/race与跨进程验证。没有第二次独立复核；新增跨进程验收夹具由实施者验证。

保留M0/M1已有改动，根go.mod/go.sum、BASELINE、Frozen不修改。原生本批生产修改仅client.go、response.go、search_parallel.go（以及定向测试）；原有M1解析薄补丁不扩张。数据库迁移仍使用已实现的扩展schema4，无原生DB变更或新增依赖。

Agent直接读/旧handle/独立图谱、其他Hook、whole-KB授权文档枚举、生成中停用及历史对话事实不在本过滤证明内。C-10标签null仍拒绝，不因投影失败改变truth。review告警/配置API/权威补充通知血缘及完整生产上传/问答E2E后续；旧M0/M1脚本probe启动假设是历史验收，需要worker适配，不能据此宣称M1/M2整体验收。
