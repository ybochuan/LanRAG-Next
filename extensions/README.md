# AgentRAG Extension Host

独立Go module，基于原生公开HTTP API工作，不引用WeKnora internal。运行命令的工作目录为本目录；环境示例见`.env.example`，程序不会自动加载该文件。

## 默认透传（probe模式）

```powershell
go run -mod=readonly ./cmd/extension-host
```

仅绑定`127.0.0.1:8090`，不创建数据库、不调用WeKnora。若当前环境已提供worker参数，须明确选择worker模式或移除这些参数，不能以probe静默忽略配置。

```powershell
curl.exe --fail http://127.0.0.1:8090/healthz
curl.exe -i http://127.0.0.1:8090/readyz
```

存活探针返回200；Host默认启用M0检索前Pass-Through：协议及字段合法、显式非空的原生授权文档范围返回200，原样保留候选且不新增排除。格式错误400、空范围403；省略/null文档范围不接受，whole-KB候选转换仍属C-09后续项。`readyz`及其余三个业务Hook仍503，不确认摄取成功；worker模式启用相同的默认检索前策略。

## 持久化worker模式

该模式会对明确配置范围内已有Outbox意图执行原生标签PUT。提供具备相应读取/写入能力和KB权限的API key，并预先通过原生UI/API准备各KB的规范gov标签；Host不会自动创建标签。

```powershell
$env:AGENTRAG_MODE = 'worker'
$env:AGENTRAG_EXT_DB = 'D:/AgentRAG-data/agentrag_ext.db'
$env:AGENTRAG_WEKNORA_URL = 'http://127.0.0.1:8080'
$env:AGENTRAG_TENANT_ID = '7'
$env:AGENTRAG_KB_IDS = '<实际KB_ID_1>,<实际KB_ID_2>'
# 在当前进程环境安全提供AGENTRAG_WEKNORA_API_KEY，不将凭据写入仓库。
go run -mod=readonly ./cmd/extension-host
```

全部worker参数必须完整；HTTP仅允许回环地址，其他配置源须HTTPS。API origin不能带用户信息、路径、query或fragment。日志只报告固定事件码，不打印API key、KB列表、响应body或原始错误。

启动顺序为配置校验→固定端口监听→ext DB迁移→唯一Tag Writer→周期对账和scoped Outbox Worker。端口占用时不打开DB、不启动任务。含非ext表的数据库会被拒绝，不能将原生weknora.db作为扩展数据库。

对账立即执行，之后周期重扫；只保存origin=reconcile的当前处理观察，不把completed转换为治理active、不恢复未观察的历史或推断删除。Outbox只认领配置tenant/KB的任务，包括该范围内过期lease；其他范围的pending/processing不变。新空DB不会自行产生审批或生命周期投影事件。

| 可选环境变量 | 默认值 | 范围／用途 |
|---|---|---|
| AGENTRAG_RECONCILE_INTERVAL | 1m | 对账周期 |
| AGENTRAG_OUTBOX_POLL_INTERVAL | 1s | 空闲/失败后的轮询 |
| AGENTRAG_OUTBOX_LEASE | 1m | 单次投影租约及Handler超时 |
| AGENTRAG_OUTBOX_RETRY_DELAY | 5s | 失败后的退避 |
| AGENTRAG_OUTBOX_MAX_ATTEMPTS | 5 | 1..100，超限进入dead |
| AGENTRAG_SHUTDOWN_TIMEOUT | 10s | HTTP连接的优雅关闭期限 |

所有可配置时长为1ms..24h；KB列表为1..1000个不同ID。极短lease可能无法完成HTTP投影，应按实际服务延迟配置。

Ctrl+C或SIGTERM触发取消：HTTP停止接受请求，对账/Worker退出，等待当前Writer操作结束，再关闭DB。HTTP排空超时会强制关连接并报告错误；后台任务遵循自身取消、HTTP和数据库超时，等待完成后才关闭持久资源，不强杀事务。pending及未完成的processing由下次启动继续处理/过期回收。

固定日志事件包括`host_probe_started`、`host_workers_started`、`reconciliation_failed`、`projection_failed`、`host_stopped`。启动事件不等于业务ready；失败详情保留在既有Outbox/tag failure表，治理/运维HTTP入口尚未实现。

## 当前限制与验证

C-10（Baseline Limitation，用户已接受）：原生`tags:null`无法区分无标签和加载失败，Host拒绝替换并记录重试；无标签文档首次投影仍受限，后续独立标签只读API解决。C-02：唯一Writer仅约束扩展调用，不能约束原生UI或超时后远端未知结果，回读不能替代CAS。

共享检索前DTO/JSON位于`contract/`和`../contracts/bridge/v1/`。Host通过`NewServerWithServices`接入`NewPassThroughRetrieveHandler`；不配置治理排除、也不把解析completed视为active。固定scope的`NewRetrieveHandler`仍可单独配置，保留tenant/KB绑定及收窄结果契约。原生M0客户端仅检查HTTP状态，所以默认透传用403拒绝空文档范围，不能依靠200+deny_all阻断。请求最大1MiB、10000文档，完整读取期限5秒；实际调用仍受原生500ms超时限制。复杂治理/响应消费、其他Hook和MCP后续实施。

```powershell
go test -mod=readonly ./... -count=1
go build -mod=readonly ./...
go vet -mod=readonly ./...
python scripts/check_module_boundary.py
python -m unittest discover -s scripts -p '*_test.py' -v
```

Host组件测试使用真实回环监听、原生格式HTTP fixture和临时SQLite验证启动/恢复/隔离/关闭。下述E2E另行验证真实核心HTTP门卫；两者均不替代MCP、混合ingest、生产模型/负载及远端CI验收。完整Linux race曾因Outbox报告计数断言失败，用户接受为M0非阻塞后续项，详见根TASK_BACKLOG的M0-06C，不声称已修复。

## WSL真实核心E2E

在仓库根目录，通过WSL/Linux运行：

```bash
bash extensions/scripts/e2e-test.sh
# 可选：--core /path/to/WeKnora-lite --source-db /path/to/weknora.db --report /path/to/report.log
```

需要已编译且包含门卫的Linux核心、Python 3.11+、Linux Go 1.26+，8080/8090未被其他进程占用。脚本拒绝停止未知端口所有者；已有核心运行时，先明确安排停止，不能同时在同端口启动隔离副本。

脚本调用标准库Python驱动`e2e_test.py`。对源数据库只读，用SQLite backup创建私有临时副本；在副本通过原生注册/登录得到JWT，并创建测试模型、KB、已完成文档和会话。使用本地OpenAI兼容模型夹具，无外部模型凭据/费用，不代表生产LLM验收。测试账号/数据仅存在于副本。真实核心在8080启动，真实Host以`go run -mod=readonly ./cmd/extension-host`在8090启动；从不使用Bridge 200桩。

RBAC=false只影响角色检查；API仍需Bearer JWT或有效API key。普通CLI不能匿名auto-setup；该接口受Lite edition及桌面专用token约束。二进制文件名不会设置edition，构建需注入`github.com/Tencent/WeKnora/internal/handler.Edition=lite`；未注入时可用显式WEKNORA_WEB_DIR启用前端静态资源，ENV_FILE显式加载已有配置，不通过关闭Auth解决根页面401。

脚本给核心设置显式前端目录，并仅为本地模型夹具将127.0.0.1加入SSRF_WHITELIST_EXTRA；不关闭SSRF或Auth，不改.env。JWT只发给核心API，凭据不进入终端/报告。用请求ID关联核心日志，按字节读取阶段证据；SIGTERM/Ctrl+C和正常退出清理脚本拥有的进程及副本。报告写入`docs/evidence/m0-e2e-时间.log`，终端打印同一份完整脱敏日志。

SSE HTTP200不能证明问答成功：必须检查错误及实际答案。2026-10-30接入默认Pass-Through后，真实E2E退出0：认证通过；断网出现指定门卫日志、答案生成0次；通网Host收到同一请求并返回answer/references/complete，答案生成1次。通过报告：[完整终端日志](../docs/evidence/m0-e2e-2026-10-30-pass-through.log)。早先无策略503的失败报告保留。本次按用户确认的HTTP门卫+默认透传范围完成M0，不代表Frozen全部治理目标已实现。
