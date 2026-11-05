# AgentRAG Extension Host

文档内嵌文件及DOCX正文附录独立入库：见[附件提取与入库](../docs/operations/文档附件提取与入库.md)，支持PDF、DOCX/XLSX/PPTX embeddings及独立“附件N”正文标题；阶段一不建立生命周期父依赖。

本机启动、服务拆分、WSL网络、认证和数据库恢复：见[AgentRAG 启动与运维指南](../docs/operations/AgentRAG-启动与运维指南.md)。

### 治理列表搜索与 Agent 历史查阅

- `start-all.sh` 会将原生 Lite 库的绝对 `DB_PATH` 传给 Host；单独启动 worker 时也请设置同一个 `DB_PATH`，并将 `AGENTRAG_EXT_DB` 指向独立扩展库。生命周期页支持在当前知识库内按标题和状态组合筛选，筛选后再分页。联查遇到 `SQLITE_BUSY` 时，Host 用已授权的原生批量 API 取标题并在内存合并；如果 API 也不可读，会报加载错误。
- 更新 Core、Host 和前端后，在原生 MCP 设置中刷新现有 AgentRAG 服务的工具目录，并为要使用的 Agent 启用该服务及 `search_archived_documents`。也可用现有 `python3 extensions/scripts/register_related_mcp.py` 注册或刷新。
- 在 Agent 模式选中对应知识库，输入“仅查阅历史已过期或已归档的制度，查找‘双人复核’，并提醒我是否仍有效”。有匹配的原文分块时，工具结果以失效警示开头；没有匹配时返回空列表。当前仅支持本机 SQLite Lite 的关键词匹配；普通问答继续使用生效文档。


M1提供显式scope/V1代理构造器（未挂载生产Host）。PDF在冷/热源字节验证后执行纯Go预检；有文本强制flash/txt，只有全纯图才可basic/ocr，其余422。复现真实4.0.1预检/协议/产物测试：

```powershell
# 在仓库根目录运行，复用用户已有环境；仅合成资料与私有临时服务。
python extensions/scripts/m1_scope_poc.py --mineru-python D:/mineru-venv/Scripts/python.exe --report docs/evidence/m1-preflight-2026-10-31-final.md
```

测试故意传auto/ocr+advanced，断言实际HTTP为flash/txt；核对真实页数、后端缓存、两scope仅两个job、4.0.1 ZIP与文档绑定的middle.json。隔离子进程禁代理、审计Python对外DNS/TCP调用，退出后检查审计标记及私有目录ONNX；不声称系统级网络隔离。不是实际OCR、原生KB权限或完整M1验收。

预检使用pdfcpu v0.16.4，无OCR/模型/Python调用；文本判定基于真实内容流的非空显示操作（含压缩Form、隐藏OCR层、低编号字符）及直接/命名ActualText，不以Font/BT标记充数。它不完成Unicode/乱码质量评估。预算为50MiB输入、1000页、3秒、每内容解码4MiB及累计检查内容32MiB；inline image或未解析结构树文本等不确定页不能触发OCR。混合PDF使用flash/txt并标记需复核，扫描部分不会自动补OCR；PDF布局升Standard/Advanced按用户新规则暂停，图片纯信号规则保留。

归档由构造器选项`parser.WithArtifactRoot("data/parser_artifacts")`启用，目录相对服务工作目录。可信调用方在已验证sourceFileID后调用`proxy.BindDocument(routeID, sourceFileID, nativeDocID)`；启用归档但未绑定时，创建job返回409。仅完成job的ZIP可下载并落盘，middle_json.json原始字节保存至`{root}/{nativeDocID}/middle.json`，同目录manifest.json保存真实schema/MinerU版本、scope、源句柄和hash；路径/多份middle/超限/JSON/冲突错误拒绝。目录原子发布，不覆盖冲突文档；这是文件归档，不是SQL事务/断电或重启恢复。

Native V1没有doc_id，本构件不猜ID、不增加原生字段。绑定是可信进程内断言；同scope同缓存file_id的第二doc_id安全拒绝，尚不支持该重复内容多文档场景。ScopeProxy回执、预检、绑定都仅在内存；Host生产接线、原生身份、持久恢复、每库标定等仍需后续，见M1 Source Map及TASK_BACKLOG。

若服务从extensions目录启动，而产物应位于仓库根data目录，配置`WithArtifactRoot("../data/parser_artifacts")`或绝对路径`D:/LanRAG-Next/data/parser_artifacts`；不要依赖未确认的工作目录。

独立Go module，基于原生公开HTTP API工作，不引用WeKnora internal。运行命令的工作目录为本目录；环境示例见`.env.example`，程序不会自动加载该文件。

## 探针模式（probe）

```powershell
go run -mod=readonly ./cmd/extension-host
```

仅绑定`127.0.0.1:8090`，不创建数据库、不调用WeKnora。若当前环境已提供worker参数，须明确选择worker模式或移除这些参数，不能以probe静默忽略配置。

```powershell
curl.exe --fail http://127.0.0.1:8090/healthz
curl.exe -i http://127.0.0.1:8090/readyz
```

存活探针返回200；当前probe没有业务数据库，`before_retrieve`和就绪探针返回503。历史M0 Pass-Through构造器保留用于历史验收；当前worker使用持久化生命周期策略，未知和非active文档排除。worker的`/health`、`/readyz`在本地数据库/服务及首次过期扫描完成后返回200（status=ready，scope=local）；不代表上游Core或所有Hook均已就绪。

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

除扩展DB路径可省略并默认仓库根data/agentrag_ext.db外，其他worker必需参数必须完整；HTTP仅允许回环地址，其他配置源须HTTPS。API origin不能带用户信息、路径、query或fragment。日志只报告固定事件码，不打印API key、KB列表、响应body或原始错误。

启动顺序为配置校验→固定端口监听→ext DB迁移→唯一Tag Writer→周期对账和scoped Outbox Worker。端口占用时不打开DB、不启动任务。含非ext表的数据库会被拒绝，不能将原生weknora.db作为扩展数据库。

对账立即执行，之后周期重扫；观察明确标记origin=reconcile，completed经生命周期规则初始化pending后转active/scheduled/stale，不复活已有失效状态，不恢复未观察的历史或推断删除。Outbox只认领配置tenant/KB的任务，包括该范围内过期lease；其他范围的pending/processing不变。新空DB对账到已完成文档时会产生生命周期初始化事实和相应投影意图，不自动审批或推断删除。

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

## M1 原生 PDF 接线

仅PDF文件在配置`AGENTRAG_PARSER_PROXY_ADDR`时转投Host；未配置、Office、URL与音频沿用原生入口。配置后的鉴权、网络或预检失败直接使解析失败，不回退原生MinerU。原生只增加HTTP传输与身份Header，继续复用既有V1 ZIP/Markdown消费及分块/索引。

| 环境变量 | 设置位置／含义 |
|---|---|
| `AGENTRAG_PARSER_PROXY_ADDR` | 核心；显式HTTP(S) origin，如`http://127.0.0.1:8090`，不得加`/parser/native`；可配置宿主IP |
| `AGENTRAG_PARSER_PROXY_KEY` | 核心和Host；同一16–256位非空白ASCII共享凭据；不要放到上传override或输出日志 |
| `AGENTRAG_PARSER_UPSTREAM_URL` | Host；已运行的MinerU4.0.1 V1 origin，不带`/v1` |
| `AGENTRAG_PARSER_UPSTREAM_KEY` | Host；MinerU凭据，与核心共享凭据分开；后端未启用鉴权才可为空 |
| `AGENTRAG_PARSER_ARTIFACT_ROOT` | Host；必填，例如`data/parser_artifacts`，相对Host工作目录 |
| `AGENTRAG_PARSER_SCOPES` | Host；必填JSON，如`[{"tenant_id":7,"knowledge_base_id":"实际KB-ID"}]`；必须是原生所有者身份 |
| `AGENTRAG_PARSER_BULK_TIMEOUT` | Host；可选Go duration，默认`1000s`、允许1ms..24h；文件上传/下载及解析请求body期限。核心若设置`WEKNORA_MINERU_TIMEOUT`，应配合设置此值；metadata仍30s、M0仍5s |

Host未配置上述解析变量时不挂载业务路由；部分配置、错误JSON或非法身份会阻止启动。`/parser/native/v1/...`仅接受Bearer及唯一的`X-AgentRAG-Bridge-Version: 1`、`X-AgentRAG-Tenant-ID`、`X-AgentRAG-KB-ID`、`X-AgentRAG-Doc-ID`。Header取自原生持久化Knowledge记录，Host按配置的tenant/KB授权并按doc隔离句柄；它信任核心身份断言，不独立回查原生权限。

每个文档独立scope，重复字节可归档到不同doc。完成ZIP在返回核心前必须保存`{root}/{doc_id}/middle.json`与manifest；失败返回非200。日志只报告`parser_native_request`、`parser_native_preflight_passed`、`parser_native_artifact_committed`，不输出文档或凭据。进程最多128个文档scope，达到容量返回503；上传/job状态仅驻留内存，尚无自动驱逐、SQL产物登记或重启恢复。未知远端job结果不盲目重发。切勿把重启当作可靠恢复办法。

### 在 Ubuntu / WSL 重编译核心

使用Go1.26+、GCC和已有前端`web/`，在仓库根目录执行；输出新文件，避免覆盖正在运行的原二进制。本命令不重建前端：

```bash
cd /mnt/d/LanRAG-Next
mkdir -p tmp/m1-native-build
CGO_ENABLED=1 GOWORK=off go build -mod=readonly -tags sqlite_fts5 \
  -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn' \
  -o ./tmp/m1-native-build/WeKnora-lite ./cmd/server
CGO_ENABLED=1 GOWORK=off go test -mod=readonly -tags sqlite_fts5 \
  ./internal/infrastructure/docparser -count=1
```

`extensions/`可用`CGO_ENABLED=0 GOWORK=off go build -mod=readonly ./...`；只有原生核心的既存依赖需要CGO，不将纯Go扩展编译成功冒充Windows原生核心构建成功。

### 自动真实上传测试

Windows PowerShell在仓库根目录执行（本机MinerU Python默认`D:/mineru-venv/Scripts/python.exe`）：

```powershell
python -B extensions/scripts/m1_native_e2e.py `
  --core /mnt/d/LanRAG-Next/tmp/m1-native-build/WeKnora-lite `
  --report docs/evidence/m1-native-upload.log
```

也可在WSL使用`bash extensions/scripts/m1-native-e2e.sh --core /mnt/d/LanRAG-Next/tmp/m1-native-build/WeKnora-lite`。脚本优先8080/8090；被占用时为隔离测试选择空闲端口，不停止未知进程。标准8090运行真正的`cmd/extension-host`，其他端口运行相同生产`host.Serve`的测试入口。

测试创建全新私有SQLite及随机账号，经原生register/login与multipart PDF上传，使用本地OpenAI兼容embedding夹具、真正的已安装MinerU4.0.1。调用方故意要求advanced/auto，必须被预检改成flash/txt；核对后端正式job恰为1、解析completed、manifest三重身份、middle schema2.0，以及原生SQLite分块里的完整PDF句子（允许Markdown转义下划线）。不触碰原数据库、模型目录或`.env`；本地模型夹具不代表生产LLM验收。本测试不执行M0问答断网/通网，原M0脚本独立保留。

本机WSL NAT无法直接访问Windows回环，测试使用自己拥有的WSL回环HTTP/Windows Python互操作中继；不改WSL、系统转发或防火墙。结果只证明这一明确拓扑。Python socket审计零外网尝试、私有目录无ONNX，不代表系统级网络隔离或所有PDF无需模型。真实通过日志见[2026-10-31首个通过记录](../docs/evidence/m1-native-e2e-2026-10-31-pass1.log)；之前的失败日志保留。

### 日常运行的 WSL / Windows 地址

核心和Host都运行在WSL时，核心设置`AGENTRAG_PARSER_PROXY_ADDR=http://127.0.0.1:8090`及共享key。Host设置独立MinerU凭据、真实scope和归档根：

```bash
# 在 extensions/，下列三个值由你的实际环境提供，勿写入仓库。
export AGENTRAG_MODE=probe
export AGENTRAG_PARSER_PROXY_KEY="$CORE_TO_HOST_KEY"
export AGENTRAG_PARSER_UPSTREAM_URL="$REACHABLE_MINERU_ORIGIN"
export AGENTRAG_PARSER_UPSTREAM_KEY="$MINERU_API_KEY"
export AGENTRAG_PARSER_ARTIFACT_ROOT=data/parser_artifacts
export AGENTRAG_PARSER_SCOPES='[{"tenant_id":7,"knowledge_base_id":"实际KB-ID"}]'
go run -mod=readonly ./cmd/extension-host
```

Windows宿主候选地址可在WSL查看`ip route show default`；必须先确认MinerU在该私有地址的端口确实可达，再填`REACHABLE_MINERU_ORIGIN`。仅在Windows `127.0.0.1`监听的服务不会因为换成宿主IP而可达，需要用户已有的私有隧道/转发或合适的网络拓扑。Host生产监听仍固定`127.0.0.1:8090`；若Host在Windows、核心在WSL，配置宿主IP同样要求已有可达转发，不能直接假设能访问Windows回环。脚本中继仅属于隔离测试，不自动提供生产隧道。

原生PDF接线之后已补SQL产物、进程恢复与M0联合回归，见下节；真实每库标定、OCR/其他档位、复杂版面/Unicode质量和物理断电仍未满足Frozen M1全部验收，状态见根backlog。

### 可选：用户授权的真实模型测试

本机llama-server的向量模型必须以`--embeddings`启动，重排模型必须以`--reranking`启动；仅能列出模型不表示支持这两种能力。2026-10-31经用户授权重启后，8081实际向量维度为2560，8082实际`/rerank`返回排序结果。

在PowerShell进程环境中预先设置`AGENTRAG_E2E_DEEPSEEK_KEY`（不要把值保存到仓库），执行：

```powershell
python -B extensions/scripts/m1_native_e2e.py `
  --core /mnt/d/LanRAG-Next/tmp/m1-native-build/WeKnora-lite `
  --live-models --deepseek-model deepseek-flash `
  --report docs/evidence/m1-native-live-upload.log
```

真实模式将本地embedding/reranker和DeepSeek生成模型注册到全新隔离空间，显式`/knowledge-search`验证重排diagnostics.applied与真实模型响应，再用`/knowledge-chat/{session_id}`检查上传PDF的引用、正确答案、complete及无error。两项原生API检查分别记录；默认问答不调用reranker时不能声称它参与了该次生成。标准8090必须空闲以运行固定M0门卫；模型服务8081/8082由用户维护，测试结束不会停止它们。2026-10-31本机真实联合测试退出0，完整输出与历次失败记录见[集成验证证据](../docs/evidence/m1-native-integration-2026-10-31.md)。

DeepSeek凭据只经子进程环境/私有stdin传递，输出脱敏，原生测试DB/日志在拥有的临时目录清理。[官方API说明](https://api-docs.deepseek.com/)使用`https://api.deepseek.com`和OpenAI兼容协议；测试实际注册`/v1`兼容地址，通过自有回环中继访问Windows及云端，不关闭原生SSRF检查。

### M1持久恢复与联合门禁

启用parser的生产Host必须使用`store.OpenDurable`（WAL/FULL）。probe模式自动创建`${AGENTRAG_PARSER_ARTIFACT_ROOT}/.parser-state.sqlite`；worker模式复用`AGENTRAG_EXT_DB`配置的扩展DB，开启FULL同步。未启用parser的纯probe继续不打开DB。升级迁移003只改扩展数据库，包含文档状态、提交意图/回执和原始middle JSON及正式job外键；不得指向原生数据库。调用`NewNativeHandler`只是配置验证/进程内兼容入口，生产接线使用`NewPersistentNativeHandler`。

去重使用物理验证的source SHA256、可信tenant/KB/doc、有效tier/ocr及后端origin，不依赖临时file ID。提交意图先于创建job；已经确认的回执可重启恢复，未知响应安全拒绝，不能自动重发POST或宣称exactly-once。空闲且状态已提交的scope可驱逐恢复，128是驻留容量，不再是文档总数上限；全活跃或保存失败仍503，同一文档自己的128次上传上限继续保留。已归档文档的新内容/新策略仍需后续受控修订流程，不能覆盖原件。

已有旧归档但缺少任务快照（包括升级前归档或状态DB丢失）时，该文档安全拒绝，不自动重新解析/猜job ID/删除原件，需人工核对恢复记录。轮询调用者取消后，Host用独立、最多5秒上下文提交已观察状态，使原生随后用独立上下文取消已知任务仍可执行；创建job的意图/网络请求仍受调用者上下文约束。

先发布/核验文件，再事务登记SQL，两个步骤全部成功才返回ZIP。登记失败503，重试可核验原始文件并补登记；不是跨文件系统与SQLite的原子事务。

非Windows的发布顺序是两个文件各自Sync、暂存目录Sync、Rename、产物根目录Sync，再登记SQL。目录同步失败返回503且不登记成功；已有相同归档重试会核验字节，再同步文档目录与产物根。Windows保留既有文件Sync/rename，未提供同等目录刷新保证。SQLite FULL和这些屏障不等于设备断电验收，也不覆盖产物根及其祖先目录的创建；残留.pending目录不自动删除，见M1-05P。

进程崩溃窗口测试在`parser/artifact_crash_test.go`，覆盖暂存同步后、Rename后/根同步前、根同步后/SQL前、SQL提交后/HTTP ACK前，恢复时检查同job、原字节、FK和跨doc拒绝。在`extensions/`运行：

```bash
go test -mod=readonly ./parser -run 'TestArtifact(Directory|Barrier|Publication)' -count=1 -v
```

若WSL的`/tmp`是tmpfs，默认测试只验证内存文件系统上的进程退出。可把TMPDIR指向目标存储上的自有隔离目录再运行同一命令；本机已经额外在`/mnt/d`的v9fs路径通过此测试，仍不能据此宣称Windows/虚拟化底层设备断电安全。完整结果见[发布恢复证据](../docs/evidence/m1-artifact-publication-2026-10-31.md)。

在仓库根目录PowerShell运行（核心二进制替换为自己的实际Linux路径）：

```powershell
python -B extensions/scripts/m1_native_e2e.py `
  --core /mnt/d/LanRAG-Next/tmp/m1-native-build-83iqcjjt/WeKnora-lite `
  --joint-m0 --report tmp/m1-recovery-joint-e2e.log
```

本测试使用真实核心/生产Host/本机MinerU4.0.1与确定性本地模型。验证原生上传/预检/唯一job/文件与SQL原字节/版本/FK/原生分块；停Host后检查关联门卫日志与答案生成0，重启带parser配置Host恢复原job回执且远端POST仍1，再用同凭证/会话/问答验证通网答案与Host收件。标准8090需可绑定；不停止未知进程，不支持`--joint-m0`与`--live-models`组合。真实云端模型测试仍按上一节单独执行。

### M1每库标定工具（语料待填充）

`testdata/parser/manifest.json`当前是空清单，因为用户确认没有真实标注语料；工具对此返回失败，绝不报告100%。准备人工标注的PDF，按文档族隔离训练/验证集，计算每份文件的SHA256，然后填入下列格式（路径相对于`--root`，示例hash必须替换为真实值）：

```json
{
  "version": 1,
  "samples": [
    {"tenant_id":7,"knowledge_base_id":"kb-a","document_id":"family-train-a","path":"train/a.pdf","sha256":"实际64位SHA256","split":"train","expected_tier":"flash"},
    {"tenant_id":7,"knowledge_base_id":"kb-a","document_id":"family-valid-b","path":"validation/b.pdf","sha256":"实际64位SHA256","split":"validation","expected_tier":"basic"}
  ]
}
```

在`extensions/`执行：

```bash
go run -mod=readonly ./cmd/parser-calibrate --manifest testdata/parser/manifest.json --root testdata/parser
```

工具实际读取PDF并运行当前预检/路由，按tenant/KB分别统计验证集准确率、高估率、低估率与拒绝数；每库均须准确率≥85%、高估≤10%、低估≤15%且无预检拒绝才返回通过。可执行程序退出码：0通过，1指标未达标，2清单/文件无效；`go run`会将子进程非零状态再包装。每库必须同时有train和validation，重复字节、同文档族、哈希不符、越界路径、非PDF/空集均拒绝。近重复内容需人工清理；当前命令只测量，未实现自动阈值拟合，也不会在验证集调参。它不测OCR识别准确率或生产检索质量，不能以合成测试代替真实语料门禁。

## M2 lifecycle core (library only)

`lifecycle.NewConfiguration(db)` provides revision-CAS `PutRules` / `PutAnnotations`.
Rules are stored in `ext_lifecycle_rules`; a KB row replaces the tenant row completely.
Zero/unconfigured days means no default expiry. UTC dates live in
`ext_document_annotations.effective_from`, `effective_until`, `review_due`.
Defaults fill missing dates on Initialize only; explicit dates win.

`lifecycle.NewMachine(db, clock)` exposes `Snapshot` and `Apply`.
Build Command from the snapshot's current event ID, annotation revision and policy token;
set the exact trigger and Event. All are rechecked within the write transaction.
Supported edges: Initialize → pending; ParseCompleted pending → active (or scheduled
for future effective_from); Timer scheduled → active; Timer active → stale after expiry.
ReviewOverdue only flags review, never changes state. Exact command retries are no-ops.

This library is not wired to Observer, periodic timers, alert delivery or public configuration
endpoints yet. The M0 Pass-Through does not enforce active-only retrieval. See
`../docs/design/M2-Lifecycle-Core-2026-10-31.md` for boundaries and acceptance tests.

## M2 自动迁移与真实策略（2026-10-31，覆盖上文历史M0默认运行行为）

当前生产Host worker使用数据库策略；probe的before-retrieve返回503。请使用已有worker配置项：AGENTRAG_MODE=worker、AGENTRAG_EXT_DB、AGENTRAG_WEKNORA_URL、AGENTRAG_WEKNORA_API_KEY、AGENTRAG_TENANT_ID、AGENTRAG_KB_IDS。无需公开凭据。上游公共API不可达时对账报告失败并周期重试；未有生命周期事实的文档默认排除，启动阶段可能暂不可检索。

LifecycleObserver保存completed观察，再通过状态机初始化pending并按日期进入active/scheduled/stale；重复观察会恢复未完成决策。过期优先，不复活stale/archived/superseded。每日任务启动先补扫，然后每24h扫描配置scope；scheduled到期晋升同事务确认最新观察completed，failed/processing/缺失均拒绝，过期失效不受此限制。review_due仅标记，告警投递后续。

原生Bridge验证完整JSON与关联身份，Host断网、超时、503、畸形响应继续fail-closed。原生SearchParallel消费deny_all、允许集及排除集，普通文档之外的命中不转交；保留原生WebSearch类型结果。该补丁不覆盖Agent直接读、旧handle、独立图谱入口或生成中状态变化。扫描最多24h收敛，不代表精确到秒过期。

跨进程验收（WSL Ubuntu、Go/GCC已配置）：

```bash
bash extensions/scripts/m2-policy-e2e.sh
```

脚本独占测试期间的127.0.0.1:8090；若已有进程占用则失败，不会停止它。真实Host/SQLite/Observer/原生PluginSearchParallel，原生公共API列表与底层检索结果使用夹具，不调用生产LLM/写生产数据；退出清理自有进程和临时数据库。旧M0/M1脚本的probe启动假设属于历史验收，不能作为当前DB策略验收；上传/完整问答脚本worker适配仍需后续验证。

## M2 复核到期告警（schema5，本地渠道）

worker模式在启动时及每24h扫描配置tenant/KB的标注。严格超过review_due后创建持久收件箱记录和同事务Outbox，随后输出固定`document_review_overdue`日志；不改生命周期状态、head或用户标签。日志不含文档/知识库身份，具体告警用本地CLI查询。

在extensions目录执行（替换为自己的扩展库路径与scope，不能填原生weknora.db）：

```bash
go run -mod=readonly ./cmd/review-alerts \
  -db /path/to/agentrag_ext.db -tenant 1 -kb 'your-kb-id' -limit 100
```

CLI只读已有schema5–9扩展库（mode=ro/query_only），不会创建数据库或执行迁移，未知版本10及以上拒绝；本地操作员需具备文件读取权限。显式scope避免无意列全库，不是多用户HTTP鉴权。当前源码的worker启动会迁移独立扩展库到schema9；本来源基础任务没有替换运行中的Host或迁移真实库。

同期限持续逾期只通知一次；期限改变/清空使旧告警失效，恢复旧的过期期限会增加generation并重新通知。CLI只列当前仍逾期的记录，delivery_state显示pending/processing/done/dead等队列状态，delivered_at表示本地回调接受，不是人工已读。失败由现有Outbox退避/dead/限定scope的人工重试机制处理。

本地通知采用至少一次语义：日志输出后、DB提交前进程退出可能重复日志，持久告警和意图仍去重。快速本地回调不能执行网络I/O或递归写DB。外部消息渠道与UI后续接入。

C-18实施差异：复核通知新增review_alert因果事实（alert_id+generation），与已有三类标签投影分派隔离，不伪造生命周期事件。验证见`../docs/evidence/m2-review-alerts-2026-10-31.md`。

## 同步来源账本（schema6，库接口，尚未接管原生副本）

`provenance.New(db, authority)`提供`RegisterSource`、`Save`、`CheckRead`、`Snapshot`。
`Authority`必须由可信服务端提供真实当前源版本和完整封存源集；模型参数、浏览器请求或DocID本身不能充当证明。schema8的`lineage.Authority`已提供原生快照版本适配；完整运行封存尚无生产实现，Host副本路由尚未挂载。nil authority只可保存pending草稿，不能产生ready清单。

源版本和清单不可变，当前指针用revision CAS；相同operation及相同请求返回历史回执，冲突请求拒绝。清单、源/父引用、指针、回执、审计同事务提交。写失败不返回成功回执，回执不代表当前许可。副本继承完整源集，不许覆盖删源；父清单更正、来源版本变化、失效、日期越界、owner/session/hash不符或DB不可用均拒绝。每次读取重新检查，无allow缓存，也不替代原生ACL。

自动迁移及严格手工DDL前缀认领已支持schema9；旧版本Host不能读取更高schema，降级应使用升级前备份，不允许手工删除迁移历史。不要在运行中混用不同版本的写进程。来源基础阶段仅使用临时库验收，没有迁移正在使用的数据库。

```bash
go test -mod=readonly ./provenance ./store ./cmd/review-alerts -count=1
```

并发、SQL回滚、提交前/提交后强制退出恢复及范围限制见`../docs/evidence/copy-provenance-ledger-2026-11-01.md`。物理断电、原生上传到副本撤销的联合E2E及持续流撤销均未验收。

## M3 可信观察版本（schema8）

原生只读接口`GET /api/v1/knowledge-bases/:id/knowledge/:document_id/source-snapshot`在原生KB权限之后，以同一事务读取header及全部live chunks；仅返回`agentrag-native-snapshot-v1`规范化指纹。未知parse/index状态、超限或超时均拒绝。4096 chunks/64MiB hash输入/2MiB输出/500ms查询限制不代表生产容量已标定。

`lineage.NewHTTPReader(client,kbs)`使用已有固定Core origin及限定KB机器凭据；`NewRegistry(db,reader).Refresh`从采集开始至登记提交占有共享写者，防跨进程旧响应回放。family/version/evidence/audit同事务；相同当前root幂等，普通编辑生成新观察版本，不自动创建supersedes或变更生命周期。ObservationSequence仅表示已观察顺序，不是原生完整generation或未观察历史。

`lineage.NewAuthority(registry,nil)`只能提供当前源版本证据，不能封存模型运行。副本许可先核对绑定，再查询当前原生root，最终扩展view再次核对head；旧schema6证据保留但不自动升格。来源查询故障时不使用旧allow缓存。`/provenance`和`/version-ancestry`目前为独立Go查询方法，尚非Host HTTP路由；Critical Edge写入和补充通知业务待后续阶段。

Harness的007写入触发器保持；共享读取守卫另处理历史异常current：状态不符、头落后、有历史缺投影均拒绝。不得把缺投影当新文档初始化，也不自动从事件重写真实库。

隔离自测（需先构建包含T1的Core；8090空闲或使用独立网络空间）：

```bash
CORE_BINARY=/path/to/candidate python3 extensions/scripts/m3_source_e2e.py
```

脚本复用生命周期界面的隔离账号/临时数据库与本地模型夹具，验证上传→Observer active→真实快照登记→原生chunk修改→旧版本拒绝，并继续日期/CAS/权限回归。不是完整副本生成/撤销或持续流撤销验收，见`../docs/evidence/m3-trusted-sources-2026-11-01.md`。

## M2 T3 补充依赖（schema9）

生命周期详情可选择同库active父文档，0个撤销、最多8个。`lineage.Supplements`绑定真实当前观察版本，声明/父关系/critical edge/current/audit/receipt同事务，支持CAS与精确重试；父版本、状态或有效期变化会拒绝子检索。复核期限继承只形成提醒，不写子日期或标签，不复活失效子文档。

Host worker以现有Core凭据安装真实Registry；检索每次刷新证据并在最终一致读重查，Core不可达/证据不足安全拒绝。每日刷新后本地扫描，初次local ready仍不依赖Core。管理仍走原生JWT薄接口，浏览器不接触Host密钥。详情、测试命令和未覆盖边界见`../docs/evidence/m2-supplements-2026-11-01.md`；真实服务和数据库升级未由本轮执行，不混跑新旧schema写进程。
