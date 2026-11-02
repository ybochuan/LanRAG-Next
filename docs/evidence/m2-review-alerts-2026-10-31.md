# M2-REVIEW 本地复核告警交付与验证 — 2026-10-31

本轮范围：用户要求继续下一项，按清单推进复核告警。未新增原生补丁、依赖或公开HTTP入口，未启动真实数据库升级、外部消息发送、模型服务或部署。既有M1/M2/SSRF及用户文件保留，不提交推送。

## 交付

- schema5 `ext_review_alerts`：同scope+UTC期限去重，单文档最多一条open；告警与Outbox同事务，FK绑定标注scope。
- 严格超过review_due才告警；不修改生命周期event/current/head。无期限、未来或刚好到期不告警。
- 同期限持续逾期不重复；无关标注revision不重发；期限变更/清空在标注写事务关闭旧告警，查询及投递都复查当前标注。即使未来→旧期限发生在两次扫描之间，也能重开并增加generation。
- Outbox独立review_alert分派；Host固定日志`document_review_overdue`无tenant/KB/doc/正文/凭据。回调为快速本地信号，不能网络I/O或递归写DB；delivered_at表示本地回调已被接受，不代表人工已读。
- 启动补扫后每24h扫配置scope；扫描失败报告并留下一周期重试。unknown投影类型拒绝，不误送标签接口。
- 只读CLI读取现有独立扩展DB，显式tenant/KB，结果上限1..1000；不建库、不迁移，native DB拒绝。CLI是本地操作员工具，不是多用户鉴权API。

## 测试证据

| 验证 | 本轮结果 |
|---|---|
| service/CLI/dispatcher实现前 | 未定义Service/run/projectionDispatcher，退出1；随后实现GREEN |
| 日期往返回归 | 初次generation仍1而期望2，退出1；标注事务关闭旧期限后通过 |
| 同期限重复与无关revision | 单告警/单意图；严格边界只在now > deadline告警 |
| 24并发Check及24重复投递 | 单告警/单意图；已提交的本地信号仅一次 |
| 257条分页＋其他tenant/KB | 全配置页处理，其他scope不入队 |
| Outbox SQL触发器故障 | inbox与intent全部回滚 |
| 通知失败、dead、人工重试 | 不错误记delivered；dead可见；错误scope不能重试；正确重试后done，代次不重复 |
| 真实子进程exit77 | alert写入后、入队前退出；重开两表均0，无部分提交 |
| exit78 | 事务提交后退出；重开两表均1，待办继续投递 |
| exit79 | 本地信号已输出、回执提交前退出；回执回滚，租约过期后恢复投递；证明可能重复信号的至少一次边界，不承诺外部exactly-once |
| Host集成 | 真回环listener/临时SQLite/fixture来源，固定信号1次、review队列done，无native标签调用，无生命周期事实变更 |
| 只读CLI | 按scope输出；主DB字节不变；缺失路径不建库；schema4只读不升级、原生DB和写SQL拒绝 |
| v1/v2/v3/v4→5升级 | 既有数据保留；v4标注revision、日期、标签不变，不在迁移时生成告警 |

Host集成测试曾在初始化未完成前打开监视连接，遇到短暂schema锁；改为等待`host_workers_started`，验证建立在正确就绪条件上，未放宽业务断言。

## 完整命令与退出状态

工作目录除注明外为`D:/LanRAG-Next/extensions`。

| 命令 | 退出状态 |
|---|---|
| `go test -mod=readonly ./... -count=1` | 0；所有有测试包通过；cmd/parser-calibrate无单独测试文件 |
| `go build -mod=readonly ./...` | 0；Windows独立扩展CGO0可构建 |
| `go vet -mod=readonly ./...` | 0 |
| `gofmt -l .` | 无输出 |
| WSL Ubuntu `go test -mod=readonly -race ./... -count=1` | 0，完整扩展无DATA RACE |
| `python scripts/check_module_boundary.py` | 0，310 imports，无WeKnora依赖或workspace |
| `python -m unittest discover -s scripts -p '*_test.py' -v` | 0，26项，Windows的Linux SIGTERM场景1项skip；本轮Python未改 |
| `go run -mod=readonly ./cmd/review-alerts -help` | 0，实际命令启动并显示四个参数 |
| `go env GOMOD GOWORK` | 扩展go.mod，GOWORK空 |

一次独立只读复核Critical0/Important0/Minor0；独立定向review/CLI/readonly/dispatcher/migration测试退出0；未进行第二次复核。

## 冲突与剩余范围

**C-18 / Implementation Difference**：Frozen4.6原三类因果来源属于已有标签投影；新增review_alert因果事实以alert_id+generation绑定通知意图，不伪造生命周期自迁移。原三类及Frozen文件不改，Host显式分流。

本批不覆盖邮件/webhook/UI、人工已读确认、配置HTTP鉴权、M3补充通知权威血缘、生产负载、物理断电或真实LLM问答。事件/当前/Outbox原不变量及C-10标签安全拒绝不变。根模块、BASELINE和Frozen SHA256与开工核对一致，internal/补丁与开工前一致。

使用新版Host worker时独立扩展库会自动升到schema5；CLI本身不升级。实际生产数据库本轮未迁移，正在运行的服务未替换。M2-REVIEW本地范围完成；M2整体仍进行中，下一项是M2-CONFIG。
