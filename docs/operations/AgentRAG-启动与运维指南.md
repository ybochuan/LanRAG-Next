# AgentRAG 本机启动与运维指南

## 本机当前入口（2026-11-05）

一键入口是 Windows 的 `start.cmd`，细节见第 1 节；启动完成后访问 **http://localhost:18880/**。本机 Windows 的 TCP 保留范围包含 7991–8090，8080 无法建立 WSL→Windows 的转发，所以 Core 端口取 18880 而不是默认 8080；该值已保存在忽略的私密启动配置里，后续启动沿用。下文凡写 8080 的历史示例，在本机按 18880 理解。

Bridge 仍固定运行于 **WSL 的 127.0.0.1:8090**，原生 500ms 客户端不变；检查 Host 在 WSL 执行 `curl --fail http://127.0.0.1:8090/health`，或直接 `./start-all.sh status`。

停止：启动窗口里按 Ctrl+C 只停 Core/Host；Windows 侧（MinerU、模型、supervisor、docreader）用 `start.cmd -Stop`。启动窗口丢失时，用 `./start-all.sh stop` 清理脚本自己拉起的 Core/Host——它只认最新运行时目录里的 `pids`，不匹配的进程一律不碰。

适用目录：Windows `D:\LanRAG-Next`，对应 WSL `/mnt/d/LanRAG-Next`。当前为 WeKnora Lite + 独立 Extension Host + Windows 侧模型进程管理器；一键入口 `start.cmd`，说明更新于 2026-11-05。

## 1. 日常启动：一条命令

在 **Windows** 执行（PowerShell 或双击）：

```
D:\LanRAG-Next\start.cmd
```

它按依赖顺序把一个完整的本机环境拉起来，每一步都是幂等的（已健康的服务直接复用）：

1. 读 `data/agentrag-launcher.json`（缺失时生成）：supervisor 令牌、模型清单、Docker 服务、MCP 开关；
2. 检查 WSL 网络模式，不是 `mirrored` 就改写 `.wslconfig`（先备份为 `.wslconfig.bak`）并 `wsl --shutdown` 重启 WSL；
3. 起 Docker Desktop（未运行时）并 `docker compose up -d --wait docreader`；
4. 起 MinerU（`D:\mineru-venv\Scripts\mineru-api.exe`）并等 `/v1/health`；
5. 起 supervisor（注入令牌）→ 按计划确保 embedding/rerank 处于 running → **真实能力自检**（向量维度、rerank 分数），不只是端口通；
6. 进入 WSL 执行 `start-all.sh`：Host ready → Core → 机器凭证校验 → MCP 幂等注册；前台运行，**Ctrl+C 只停 Core/Host**，Windows 侧服务保持运行。

参数：`-DryRun` 只打印计划不动手；`-SkipDocker`、`-SkipMinerU` 跳层；`-NoNetworkFix` 不改系统网络；`-Strict` 让 Windows 侧缺失也中止；`-Stop` 停模型、supervisor 和 docreader 容器。

只想起 Core/Host（Windows 侧已就绪）时仍可直接：

```bash
cd /mnt/d/LanRAG-Next
./start-all.sh            # = start
./start-all.sh status     # 七个服务 + WSL 网络模式
./start-all.sh doctor     # 断链时给出下一步
./start-all.sh logs       # 最新一次运行目录的日志
./start-all.sh token      # 控制台所需的 supervisor 令牌（唯一会打印它的地方）
./start-all.sh stop       # 只停脚本自己拉起的 Core/Host
```

### 1.1 模型进程管理的访问令牌

`/platform/model-supervisor`（左侧「模型进程管理」）问的**不是账号密码**，是 supervisor 自己的访问令牌。它和 Core 登录态、模型 API Key 是三套互不通用的凭据：

- 启动器**首次运行随机生成一次**（43 字符 URL-safe），之后固定复用——不会每次重启都变，这是浏览器只需输入一次的原因；
- 存在 `data/agentrag-launcher.json` 的 `MODEL_SUPERVISOR_TOKEN`，该文件在 `.gitignore` 内；WSL 侧是 POSIX 0600，`/mnt/d` 位于 NTFS 时以 Windows ACL 为准（见第 6 节）；
- 只用于浏览器 → `127.0.0.1:8092`，不经过 Core，也不会被任何 Core 接口下发。

获取方式：

```bash
cd /mnt/d/LanRAG-Next
./start-all.sh token                # 只打印这一项，不写日志
./start-all.sh token | clip.exe     # 直接放进 Windows 剪贴板，粘一下即可
```

等价地从私有状态里读（脚本校验的是同一个字段）：

```bash
python3 -c "import json; print(json.load(open('data/agentrag-launcher.json'))['MODEL_SUPERVISOR_TOKEN'])"
```

用法：粘到页面的「访问令牌」输入框后保存。浏览器把它存在 localStorage，**输一次长期有效**，以后重启 supervisor 也不用重填；页面收到 401（例如哪天换了令牌）会自动清掉旧值并要求重填。

命令行自检：

```bash
curl -s -H "X-API-Key: $(./start-all.sh token)" \
     -H "X-AgentRAG-Model-Supervisor-Version: 1" \
     http://127.0.0.1:8092/extensions/models/list
```

需要主动轮换（例如怀疑外泄）：先 `start.cmd -Stop` 停掉 supervisor，删掉状态文件里的 `MODEL_SUPERVISOR_TOKEN` 字段，再 `start.cmd`；启动器会生成新值，浏览器下次会提示重填。**不要**在 supervisor 运行时手改状态文件——进程里用的还是旧值。

安全边界：这串不要贴进截图、聊天记录、提交或终端日志。`./start-all.sh token` 是唯一会打印它的入口；启动器日志、`private.env`、`plan` 输出都不含它。

MCP 首次需要一次登录来签发专用 key：

```bash
./start-all.sh mcp-key    # 或 python3 extensions/scripts/launcher_config.py mcp-key
```

之后每次启动都会幂等地创建/更新名为 `AgentRAG` 的连接并刷新工具目录；注册失败只告警，不影响系统启动。

首次运行条件未变：已编译 Linux 版 `WeKnora-lite`，原生数据库中已有用户和知识库。没有有效机器 API Key 时会提示输入一次**原生登录邮箱和密码**（密码不保存），随后复用私有配置。

已有私有配置中的范围会保留；新增知识库后应显式更新 `AGENTRAG_KB_IDS`，必要时重新登录签发覆盖新范围的机器凭证。不要把知识库名称当作 ID。

## 2. 服务分工和端口

| 服务 | 运行位置 | 默认地址 | 用途 / 启动方式 |
|---|---|---|---|
| WeKnora Core | WSL | `http://127.0.0.1:8080`（本机 18880） | 原生 UI/API、认证、分块、索引、问答；start-all.sh 管理 |
| Extension Host | 与 Core 相同的 WSL | `http://127.0.0.1:8090` | 生命周期、检索策略、解析代理；start-all.sh 构建并管理 |
| MinerU 4.0.1 | Windows | `http://<Windows地址>:8000` | PDF 解析；start.cmd 自动拉起（也可单独启动） |
| Embedding | Windows | `http://127.0.0.1:8081/v1` | 本地 Qwen3 向量服务；start.cmd 经 supervisor 拉起并验真 |
| Reranker | Windows | `http://127.0.0.1:8082` | 本地 Qwen3 重排服务；start.cmd 经 supervisor 拉起并验真 |
| docreader | Docker | `127.0.0.1:50051`（gRPC） | 复杂格式解析（docx/pdf/xlsx/pptx/图片等）；start.cmd 自动拉起，容器名 `WeKnora-docreader` |
| 生成模型 | DeepSeek 云端 | `https://api.deepseek.com/v1` | 在原生模型管理中配置账户可用模型和 API Key |

`start.cmd`（Windows 半）负责 Docker、MinerU、supervisor 与两个模型；`start-all.sh`（WSL 半）负责 Core 和 Host，并在启动前从 WSL 侧验证 Windows 服务真实可达。任何一层缺失都只降级并打印影响范围，`-Strict`/`AGENTRAG_STRICT=1` 时才中止。纯文本可由原生 Go 解析器处理；PDF 和向量检索分别需要对应后端服务。Lite 使用 SQLite、内存任务/流管理器，不需要为这套本机配置额外启动 PostgreSQL、Redis、Neo4j。

Host 必须与 Core 在同一网络空间。原生 Bridge 固定访问 `127.0.0.1:8090`，不能把 Host 放在 Windows、Core 放在隔离的 WSL，再期待它们共享回环地址。Host 始终只绑定 IPv4 回环。

上表中 Windows 侧服务的地址取决于 WSL 网络模式：**镜像网络下 MinerU 与两个模型都可以用 `127.0.0.1`**（start.cmd 会自动切到 mirrored 并重启 WSL）；NAT 模式下模型必须换成 WSL 可达的 Windows 地址，且 Windows 侧服务要相应改监听地址——一键脚本会拒绝这种半配置状态并说明原因。

## 3. 编译 WeKnora 与 Host

### 3.1 编译原生 Lite 核心（WSL）

关闭正在运行的同一路径二进制后执行：

```bash
cd /mnt/d/LanRAG-Next
export PATH=/usr/local/go/bin:$PATH
CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 \
  -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' \
  -o WeKnora-lite ./cmd/server
```

需要 Linux Go、C 编译器及 SQLite 相关构建环境。不能用 Windows `.exe` 替代 WSL ELF 二进制。此命令不重建前端；当前静态页面由 `WEKNORA_WEB_DIR=/mnt/d/LanRAG-Next/web` 提供。前端有改动时再按对应前端任务重建，不要为启动后端顺手运行 `make build-lite`。

### 3.2 编译扩展（WSL，无 CGO）

```bash
cd /mnt/d/LanRAG-Next/extensions
CGO_ENABLED=0 GOWORK=off go build -mod=readonly \
  -o ../data/bin/extension-host ./cmd/extension-host
```

首次手动编译前执行 `mkdir -p ../data/bin`。一键脚本会自动创建目录并在本次运行目录内构建 Host。扩展是独立 Go module，Go 命令在 `extensions/` 执行。

## 4. Windows 后端服务启动

这一节的服务日常都由 `start.cmd` 负责拉起和检测；下面的手工命令用于排障、临时替换参数，或在没有一键脚本的机器上复现同一套环境。手工启动时要注意：supervisor 必须跑在 Windows 侧，MinerU 与模型地址要和 WSL 网络模式匹配（第 5 节）。

### 4.1 MinerU

`start.cmd` 会在 `D:\mineru-venv\Scripts\mineru-api.exe` 存在且 `:8000/v1/health` 不通时自动拉起它（`-SkipMinerU` 可跳过）。手工启动等价于在 **Windows PowerShell** 执行：

```powershell
& 'D:\mineru-venv\Scripts\mineru-api.exe' --host 127.0.0.1 --port 8000
```

先检查 Windows 本地服务：

```powershell
curl.exe --fail http://127.0.0.1:8000/v1/health
```

如果 WSL 不能访问 Windows 的回环服务，需要使用 WSL 可达的 Windows 网卡地址。此时可把 MinerU 的 `--host` 改成该网卡地址，或在明确限制本机开发网络访问的前提下使用 `0.0.0.0`；相应配置 `MINERU_ENDPOINT`。这不涉及改变 Host 的 `127.0.0.1:8090` 绑定。

MinerU 后端如配置了鉴权，通过 `AGENTRAG_PARSER_UPSTREAM_KEY` 提供密钥。Host 与 Core 的代理共享密钥是另一项 `AGENTRAG_PARSER_PROXY_KEY`，一键脚本自动生成和保存，二者不要混用。

### 4.2 Qwen3 Embedding

`start.cmd` 通过 supervisor 拉起它并校验真实输出维度（见 4.6）。手工启动等价于在 Windows PowerShell：

```powershell
& 'D:\LanRAG-Next\data\bin\llama-server.exe' `
  --model 'D:\LanRAG-Next\data\models\Qwen3-Embedding-4B-Q6_K.gguf' `
  -a Qwen3-Embedding-4B-Q6_K `
  --host 127.0.0.1 --port 8081 --embeddings
```

原生模型类型选择 Embedding，接口使用 OpenAI 兼容格式，base URL 为 `http://<Core可达的Windows地址>:8081/v1`。本项目此前测试的输出维度为 **2560**；模型文件或服务参数改变后，应重新核对实际 `/v1/embeddings` 输出，不能只改知识库里的维度数字。

### 4.3 Qwen3 Reranker

手工启动等价于在 Windows PowerShell：

```powershell
& 'D:\LanRAG-Next\data\bin\llama-server.exe' `
  --model 'D:\LanRAG-Next\data\models\qwen3-reranker-0.6b-q8_0.gguf' `
  -a qwen3-reranker-0.6b-q8_0 `
  --host 127.0.0.1 --port 8082 --reranking
```

原生模型类型选择 Rerank，base URL 为 `http://<Core可达的Windows地址>:8082`。打开模型健康接口只证明 HTTP 服务可达；必须启用 `--embeddings` / `--reranking` 才能提供所需能力。重排模型也需要在原生检索配置中启用。

### 4.4 DeepSeek 生成模型

在原生模型管理中配置 KnowledgeQA / 生成模型，使用 OpenAI 兼容接口，base URL `https://api.deepseek.com/v1`；模型名使用该账户实际可用的标识。API Key 仅在私有配置/原生模型设置中输入，不写进本指引、启动脚本或终端日志。该 Key 与 Host 调用 WeKnora 的机器 API Key 无关。

### 4.5 可选：前端开发服务与 docreader

Lite 日常使用无需启动 Vite。开发前端时在单独 WSL 终端运行：

```bash
cd /mnt/d/LanRAG-Next/frontend
npm run dev
```

默认开发端口为 5173；依赖尚未安装时才先执行 `npm ci --no-audit --no-fund`。Core 和 Host 仍须单独运行。开发服务不会自动更新根目录 `web/`；正式前端构建/嵌入按对应前端任务执行。本轮未重建前端。

TXT 等内置 Go 解析，以及已接线的 PDF→Host→MinerU 路径，不需要额外 docreader。原生其他解析引擎/格式若配置为 docreader，则需要可达的 gRPC 后端。仓库 Compose 中的 docreader 默认只向容器网络 expose 50051，不向宿主发布端口；直接 `docker compose up -d docreader` 不能保证 WSL 原生 Core 可访问。此类部署需另行配置回环端口发布与 `DOCREADER_ADDR`，不能直接使用只有 Compose 内可解析的 `docreader:50051`。

`start.cmd` 默认把 `docreader` 列入要拉起的容器：Docker Desktop 未运行会先拉起，然后
`docker compose -f docker-compose.yml -f docker-compose.docreader-host.yml up -d --wait --no-build docreader`。
上游 compose 只把 50051 `expose` 在容器网络里（给 app 容器用 `docreader:50051`），而本机 Core 直接跑在 WSL，所以新增 host override 把端口发布到 `127.0.0.1:50051`；镜像可用 `AGENTRAG_DOCREADER_IMAGE` 固定（默认 `wechatopenai/weknora-docreader:latest`）。

Core 侧需要两个环境变量，启动器按实际可达性自动写入并随 `private.env` 传给 Core：

```
DOCREADER_ADDR=127.0.0.1:50051
DOCREADER_TRANSPORT=grpc
```

`prepare` 每次都会探测 50051：容器没起来就**不写**地址（原生设置页因此显示「未连接」，而不是假装连上后让每次上传都失败）；容器起来后再启动一次 Core 即可。地址生效后，原生「解析引擎」页会显示 `已连接 gRPC`，并列出远端引擎。

选引擎：进入知识库设置 → 解析引擎，选择 DocReader 提供的引擎（本机 v0.8.0 镜像实测提供 `builtin`/`markitdown`/`opendataloader`）。`builtin` 覆盖 docx/doc/pdf/md/xlsx/pptx/epub/html/图片/音频等 21 类。**引擎是每个知识库的配置，改完只影响之后入库的文档。**

本机 2026-11-05 实测：`docreader:v0.8.0`（华为 SWR 镜像）与 v0.9.0 Core 的 gRPC 协议兼容——`ListEngines` 返回三个引擎且全部 `available=true`；容器 healthy、`127.0.0.1:50051` 从 WSL 可达、`./start-all.sh status` 显示 `docreader :50051 UP`。

拉不到 Docker Hub 时（本机就是这种网络）：先确认本地已有镜像，再把它固定到状态里，例如

```bash
AGENTRAG_DOCREADER_IMAGE='swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/wechatopenai/weknora-docreader:v0.8.0' \
  python3 extensions/scripts/launcher_config.py prepare /tmp/private.env
```

然后重跑 `start.cmd`；没有 Docker 的机器用 `-SkipDocker` 或 `AGENTRAG_DOCKER_SERVICES=` 关掉这一项，Core/Host 不受影响。

### 4.6 可选：用模型进程管理器托管 4.2 / 4.3

4.2 与 4.3 也可以交给 `model-supervisor` 托管：它负责拉起 `llama-server.exe`、挂 Job Object 防孤儿、按需探活并给出**两侧可达性**结论。它不参与任何治理决策，故障不影响 fail-closed 判定。

**它必须运行在 Windows 侧**：`data/bin/llama-server.exe` 是 Windows 程序，孤儿清理也依赖 Windows Job Object。在 WSL 里运行 supervisor 会通过 WSL 互操作把 `/mnt/d/...` 路径原样交给 Windows 子进程，llama-server 只能报 `failed to open GGUF file (No such file or directory)`——看起来像模型缺失，其实模型一直在。现在 supervisor 会直接拒绝这种跨 OS 启动并提示改到 Windows 侧，不再让它以「加载模型失败」的形式出现。

日常不需要手工启动：`start.cmd` 会生成/复用令牌、启动 supervisor，并按计划把 embedding 和 rerank 拉起来做真实能力自检。下面的命令只在单独调试或首次编译时使用。

在 WSL 里交叉编译出 Windows 二进制（纯 Go，不需要 CGO）：

```bash
cd /d/LanRAG-Next/extensions
GOOS=windows GOARCH=amd64 go build -mod=readonly -o ../data/bin/model-supervisor.exe ./cmd/model-supervisor
```

在 **Windows PowerShell** 里手工启动（不要用 WSL bash 起它）。令牌由启动器保存在 0600 的私有状态里，用 `./start-all.sh token` 取，不要写进命令行历史：

```powershell
cd D:\LanRAG-Next
$env:MODEL_SUPERVISOR_TOKEN = (wsl.exe -d Ubuntu -- bash /mnt/d/LanRAG-Next/start-all.sh token)
$env:MODEL_SUPERVISOR_WSL_DISTRO = 'Ubuntu'
.\data\bin\model-supervisor.exe
```

控制台的「两侧可达性」仍由 Windows 侧 supervisor 调 `wsl -d Ubuntu curl ...` 判定，所以 `MODEL_SUPERVISOR_WSL_DISTRO` 必须写对；不写就是本机默认发行版（本机是 `docker-desktop`，没有 `curl`），探测会永远停在 `unverified`。

| 变量 | 默认 | 说明 |
|---|---|---|
| `MODEL_SUPERVISOR_ADDR` | `127.0.0.1:8092` | 只允许回环，非回环地址直接拒绝启动 |
| `MODEL_SUPERVISOR_TOKEN` | 无 | 必填；缺失则拒绝启动，`/healthz` 之外全部要鉴权 |
| `MODEL_SUPERVISOR_BIN_DIR` | `data/bin` | `llama-server.exe` 与 DLL |
| `MODEL_SUPERVISOR_MODEL_DIR` | `data/models` | `.gguf` |
| `MODEL_SUPERVISOR_STATE` | `data/model-supervisor-state.json` | 已配置实例快照 |
| `MODEL_SUPERVISOR_WSL_DISTRO` | `Ubuntu` | **必须写对**：本机默认发行版是 `docker-desktop`，没有 `curl`，不指定就会永远探测失败 |
| `MODEL_SUPERVISOR_LLAMA_API_KEY` | 空 | 传给 `llama-server --api-key`；为空则该端口对任何本机进程开放 |
| `MODEL_SUPERVISOR_ALLOWED_ORIGINS` | `http://127.0.0.1:18880,http://localhost:18880` | 控制台页面的跨源允许列表；逗号分隔，只接受 `http(s)://host[:port]`，不接受通配与路径。留空即关闭 CORS（无头部署的安全默认） |

鉴权头用 `X-API-Key` 或 `Authorization: Bearer` 均可，并需带 `X-AgentRAG-Model-Supervisor-Version: 1`（不匹配返回 400）。

**控制台页面**：Core 起来后打开 `http://127.0.0.1:18880/platform/model-supervisor`（左侧菜单「模型进程管理」）。页面会先要一次访问令牌——它和 Core 的登录态是两套，填 `MODEL_SUPERVISOR_TOKEN` 的值。supervisor 换了 `MODEL_SUPERVISOR_TOKEN` 重启后旧令牌会失效：页面收到 401 时自动清掉浏览器里那个失效值并重新显示输入框，右上角「令牌」按钮也可以随时更换。卡片上的「可达性」是每次刷新时**现调** `GET /extensions/models/{id}/probe` 得到的，不是启动时的快照；`loopback` 表示 Core 与浏览器两侧都能连上该地址。显存读数来自 `nvidia-smi`，取不到时显示 `unverified` 并说明容量检查已跳过。

```bash
H='-H Content-Type:application/json -H X-API-Key:'"$MODEL_SUPERVISOR_TOKEN"
# 拉起 embedding（默认端口 8081，与 Core 现有 base_url 一致）
curl.exe -s $H -X POST -d '{"id":"embedding","model":"Qwen3-Embedding-4B-Q6_K.gguf","task_type":"embedding","port":8081}' http://127.0.0.1:8092/extensions/models/start
# 看它到底从哪一侧可达、真实向量维度
curl.exe -s -H "X-API-Key: $MODEL_SUPERVISOR_TOKEN" http://127.0.0.1:8092/extensions/models/probe
# 显存不够时的 428 会带估算值；确认无碍再 force
curl.exe -s $H -X POST -d '{"id":"embedding","model":"Qwen3-Embedding-4B-Q6_K.gguf","task_type":"embedding","port":8081,"force":true}' http://127.0.0.1:8092/extensions/models/start
curl.exe -s $H -X POST -d '{"id":"embedding"}' http://127.0.0.1:8092/extensions/models/stop
```

判据：`/extensions/models/probe` 返回 `reachability: "loopback"` 与 `embedding_dim: 2560` 才算真正可用；`unverified` 说明跨 OS 探测没跑成（多数是 `MODEL_SUPERVISOR_WSL_DISTRO` 写错），此时地址只在 Windows 侧有效。

`use_gpu` 不写就是**开启** GPU（`-ngl 99`）；要强制 CPU 才写 `"use_gpu": false`。这一点不能反：默认 CPU 会让服务照样 200、照样出向量，只是慢一个数量级，而容量防呆查的还是显存。
显存防呆只对**开启 GPU** 的实例生效：`use_gpu: false` 的实例不碰显卡，不会被 428 拦下。

**退出即清理**：supervisor 被强杀时，Job Object 会连带结束它拉起的 `llama-server.exe`（已实测）。所以不要再让 supervisor 与手工启动的实例共用同一端口。

**尚未集成**：`start-all.sh` 目前不启动 supervisor，也不探活它；这一步属于后续阶段。当前需手工先起 supervisor，再起 4.2/4.3 的替代实例。

### 4.7 三种任务类型与模型目录

supervisor 托管三类任务。默认端口与 Core 数据库里已存的地址对齐，不要随意改：

| 任务 `task_type` | llama-server 开关 | 默认端口 | 探测路径 |
|---|---|---|---|
| `embedding` | `--embedding` | 8081 | `/v1/embeddings` |
| `rerank` | `--rerank` | 8082 | `/v1/rerank` |
| `chat` | **不带**模式开关（llama-server 默认模式） | 8083 | `/v1/chat/completions` |

`chat` 会自动追加 `--jinja`，让 llama-server 使用 GGUF 自带的对话模板；去掉它会退回 llama.cpp 内置默认模板，角色与工具调用格式可能被静默改写。注意 `chat` 不传模式开关：给 llama-server 传它不认识的 flag，进程会在启动阶段直接退出。

```bash
# 列出 data/models/ 下可用的 .gguf（控制台的下拉框读的就是这个）
curl.exe -s -H "X-API-Key: $MODEL_SUPERVISOR_TOKEN" http://127.0.0.1:8092/extensions/models/catalog
# 起一个 CPU 上的对话实例（不占显存，速度慢）
curl.exe -s $H -X POST -d '{"id":"chat","model":"Qwen3.5-9B-Q4_K_M-imatrix.gguf","task_type":"chat","port":8083,"use_gpu":false}' http://127.0.0.1:8092/extensions/models/start
# 单实例探测：控制台卡片每次刷新都调这个，不看 /list 里的陈旧 reachability
curl.exe -s -H "X-API-Key: $MODEL_SUPERVISOR_TOKEN" http://127.0.0.1:8092/extensions/models/chat/probe
# 停止要带 body；POST /extensions/models/chat/stop 是 404
curl.exe -s $H -X POST -d '{"id":"chat"}' http://127.0.0.1:8092/extensions/models/stop
```

`chat` 在 CPU 上仅 prompt 处理就可能超过 2 秒，所以它的探测超时是 45 秒，其余任务 3 秒。全集探测 `/extensions/models/probe` 与单实例探测 `/extensions/models/{id}/probe` 用同一套判定：`unverified` 只表示"探测没跑成"，不代表服务坏了，但**绝不能**把 `unverified` 的地址填进 Core 的模型配置。

模型目录不存在**不是错误**：`/catalog` 会返回空列表加一条 `note`，新装机器本来就可能没有模型文件。`binary_ready: false` 才说明 `data/bin/` 里缺 `llama-server.exe`，这种情况下任何 `start` 都会失败。

**GPU 层数（`gpu_layers`）**：直接对应 llama-server 的 `-ngl`，决定有多少层模型放进显存。

| 取值 | 含义 | 实际命令行 |
|---|---|---|
| 省略 | 全部卸载（默认） | `-ngl 99` |
| `"gpu_layers": N` | 部分卸载，其余层留在内存 | `-ngl N` |
| `"use_gpu": false` | 纯 CPU | `-ngl 0` |

显存装不下整个模型时用部分卸载：比纯 CPU 快，又不必换更小的量化。显存够就省略。`use_gpu` 只是它的简化开关，两者同时写会互相矛盾——`use_gpu:false` 配 `gpu_layers > 0` 会被直接拒绝（`invalid gpu_layers: conflicts with use_gpu:false`），避免 `-ngl` 的取值取决于字段求值顺序。

`-ngl` 属于受保护参数，写在 `extra_args` 里会被拒绝：这类参数必须走结构化字段，否则表单显示的和实际生效的会不一致。`/list` 回传的 `gpu_layers` 是**解析后**的真实值（99 / N / 0），不是原始输入。

### 4.8 运行期行为与故障诊断

**受保护参数**：管理器代为设置的参数一律不允许从 `extra_args` 再写一遍，包括 `--host`、`--port`、`--model`、`--alias`、`--api-key(-file)`、`--embedding(s)`、`--rerank(ing)`、`--jinja`、`--no-webui/--no-ui`、`-c/--ctx-size`、`-t/--threads`、`-ngl/--gpu-layers/--n-gpu-layers`。命中即 `400 invalid_request`（`extra_args must not set a supervisor-owned flag`）。规则是「我们代管的全部参数都受保护」，不是「记几个算几个」。

**端口冲突在启动前拦下**：`POST /extensions/models/start` 先做一次 `127.0.0.1:<port>` 绑定探测，端口被占直接 `409`（`{"code":"port_in_use"}`），不会先返回 `202` 再变成 `failed`。探测与真正监听之间仍有时间窗（TOCTOU），但把「换个端口重来」这件事提前到了提交时，实例也不会以半成品状态残留在列表里。

**启动失败要看得到原因**：进程退出后 `last_error` 会附上 stderr 的最后三行（每行截断到 400 字符），例如：

```
process exited with code 3: llama_model_load: error loading model: failed to load model | llama_init_from_gpt_params: failed to load model
```

只有退出码时无从下手——模型文件损坏、显存不足、参数拼错都在 stderr 里。控制台卡片会直接显示这一行。

**`/list` 里的健康是测量值，不是猜测**：`endpoint` 只给地址（`http://127.0.0.1:<port>/v1`），可达性在 `health` 里，带 `checked_at` 时间戳。后台每 30 秒巡检一次所有 `running` 实例，写入 `health`；实例还没被测量过时该字段**不存在**，而不是填一个 `unverified` 冒充结论。巡检只汇报健康，不改变实例状态——进程活着就是 `running`，能不能服务由 `health` 说。间隔用 `MODEL_SUPERVISOR_SWEEP_INTERVAL` 调整（默认 `30s`，`0` 关闭）。

**探测超时按任务区分**：

| 任务 | 预算 | 理由 |
|---|---|---|
| chat | 45s | CPU 上 prompt 处理就要数秒 |
| embedding | 20s | 首次请求要付一次图编译/预热成本 |
| rerank | 3s | 正常毫秒级，超时即视为卡死 |

embedding 的 20s 是实测出来的：本机模型加载完成后第一次 `/v1/embeddings` 用 **12.3s**，第二次 **5.0s**，第三次 **0.10s**。原先统一 3s 会把一个已经加载好的健康服务报成不可达——**冷启动慢不等于服务坏了**。

## 5. WSL 与 Windows 网络检查

WSL2 有两种网络模式，决定了 WSL 里的 Core 能否访问 Windows 上的 MinerU 与模型服务。**一键启动会自动把模式改成 mirrored**：检测到 `nat` 就备份 `.wslconfig`、写入 `networkingMode=mirrored` 与 `autoProxy=false`，然后 `wsl --shutdown` 重启（`-NoNetworkFix` 可禁止）。手工排查时按本节确认；想回退就恢复 `C:\Users\<用户名>\.wslconfig.bak` 再执行 `wsl --shutdown`。

启动完成后用下面这条命令一眼判断两侧是否通：

```bash
./start-all.sh status     # 七个服务 + 当前 networkingMode
./start-all.sh doctor     # 全不可达时给出下一步
```

### 5.1 镜像网络（推荐，已在本机验证）

WSL2 默认是 NAT：Windows 能访问 WSL 的回环服务，**WSL 不能访问 Windows 的 `127.0.0.1`**。开启镜像网络后两侧共享同一个回环，Windows 侧只绑 `127.0.0.1` 的 MinerU 和模型服务可以直接用 `127.0.0.1` 访问，不需要网关地址、端口转发，也不需要用 `0.0.0.0` 暴露到局域网。

在 **Windows** 用户目录创建 `C:\Users\<用户名>\.wslconfig`：

```ini
[wsl2]
networkingMode=mirrored
autoProxy=false
dnsTunneling=true
```

然后执行 `wsl --shutdown`，重新打开 WSL 终端。

**`autoProxy=false` 不是可选项。** 本机注册表残留 `ProxyServer=127.0.0.1:7897`（即使 `ProxyEnable=0`），WSL 仍会尝试镜像该代理并失败，报 `CreateInstance/CreateVm/ConfigureNetworking/0x8007054f`，**回退到 `networkingMode=none`——WSL 完全没有网络**，比 NAT 更糟。关掉自动代理继承即可正常启用。

验证（在 **WSL** 执行）：

```bash
wslinfo --networking-mode          # 必须输出 mirrored；输出 none 说明已失败回退
ip route show default              # 镜像模式下显示宿主真实网卡与网关
```

本机实测（2026-11-01，Windows 10.0.26340，WSL 2.7.13，内核 6.18.33.2）：

| 方向 | 命令 | 结果 |
|---|---|---|
| WSL → Windows 模型 | WSL 内 `curl http://127.0.0.1:8001/v1/embeddings` | 200，返回 2560 维向量 |
| Windows → WSL | Windows `curl http://127.0.0.1:9999/` | 200 |
| Windows → WSL（localhost） | Windows `curl http://localhost:9999/` | 200 |

镜像模式下 `127.0.0.1` 双向通用，因此模型服务保持 `--host 127.0.0.1` 即可，不需要暴露到局域网。

**镜像模式的注意事项**：

- 需要 Windows 11 22H2 及以上；旧版本会忽略该配置并继续用 NAT。
- 两侧共享回环意味着**端口会冲突**：Windows 和 WSL 不能同时监听同一个 `127.0.0.1:端口`。Host 固定占用 8090，Windows 侧不要在该端口启动任何服务。
- 走 Hyper-V 防火墙而非传统 Windows 防火墙；如遇拦截，检查 `Get-NetFirewallHyperVVMSetting`。
- **proxy/VPN 客户端与镜像网络冲突**：本机实测，`clash-verge` 进程存在时镜像网络起不来，报 `CreateInstance/CreateVm/ConfigureNetworking/0x8007054f` 并回退到 `networkingMode=none`——WSL 完全没有网络，比 NAT 更糟。只结束 `clash-verge` 的 GUI 进程、保留 `clash-verge-service` 服务，镜像网络即可正常启用。冲突只发生在 WSL 启动的那一刻：等 `wslinfo --networking-mode` 确认是 `mirrored` 之后再把代理打开，不影响已运行的 WSL。所以固定顺序是：**先退出代理客户端 → 启动 WSL → 确认 `mirrored` → 再恢复代理**。

**回退**：若 `wslinfo --networking-mode` 输出 `none` 或网络异常，先排除上面那条代理冲突，再删除或注释 `.wslconfig` 里的 `networkingMode=mirrored`，`wsl --shutdown` 后恢复 NAT，按 5.2 用网关地址。注意回退到 NAT 后必须同步改 Core 里保存的模型地址（见 5.3），否则检索会静默降级成纯关键词。

### 5.2 NAT 模式下的检查与兜底

未启用镜像网络时，在 **WSL** 检查实际调用方向：

```bash
# NAT 模式下可先查看默认网关，通常是候选 Windows 宿主地址。
ip route show default

# 把下面地址换成实际可达地址；镜像网络下可直接用 127.0.0.1。
curl --noproxy '*' --fail --max-time 3 http://<Windows地址>:8000/v1/health
curl --noproxy '*' --fail --max-time 3 http://<Windows地址>:8081/v1/models
curl --noproxy '*' --fail --max-time 3 http://<Windows地址>:8082/v1/models
```

Windows 能访问 `127.0.0.1:8081`，不代表 WSL 也能访问同一个地址。如果使用 NAT，模型只绑定 Windows 回环时，直接换成网关 URL 也未必能通；应调整服务绑定到可达网卡、配置受限转发，或改用 5.1 的镜像网络。不要只修改 URL 后忽略监听地址。

未显式指定 `MINERU_ENDPOINT` 时，一键脚本在 WSL 中尝试默认网关的 8000；该值只是默认地址，不能保证防火墙、服务绑定和网络模式都已正确。脚本不会自动启动远端 MinerU，也不把 Host 就绪当成 MinerU 就绪。

### 5.3 切换网络模式后必须同步 Core 里保存的模型地址

**这是切换网络模式最容易踩的坑，已在本机实际发生（2026-11-01）。**

Core 把 Embedding/Rerank 的 `base_url` 持久化在 `weknora.db` 的 `models.parameters` 里。改了 WSL 网络模式之后，这些地址不会自动跟着变：从 NAT 切到镜像，地址还停在旧网关（如 `http://172.26.0.1:8081/v1`）；从镜像切回 NAT，地址还停在 `127.0.0.1`。

后果不是报错，而是**静默降级**：

```json
{"data":[],"meta":{"rerank":{"applied":false,"outcome":"no_candidates","candidate_count":0}},"success":true}
```

HTTP 200、结果为空、前端只显示「没搜到」。Core 日志里才有真正的线索：

```
[embedding] retrying (3/3) after 4s: ... Post "http://172.26.0.1:8081/v1/embeddings": dial tcp 172.26.0.1:8081: i/o timeout
WARNING [PIPELINE] stage=Search action=group_embed_degrade_keyword error="embedding: ... i/o timeout"
INFO [PIPELINE] stage=Search action=group_plan combined_kb_count=1 individual_targets=0 vector_len=0
```

向量检索降级为纯关键词，候选直接归零。

**切换网络模式后的检查步骤**：

```bash
# 1. 看 Core 实际在调哪个地址（最快，不用查库）
grep -o 'model_key="[^"]*"' <run-dir>/core.log | sort -u
# 2. 从 WSL 里直接验证该地址
curl --noproxy '*' --fail --max-time 3 http://<地址>:8081/v1/models
```

**修正方式**（推荐前一种）：

1. 原生 UI「设置 → 模型」把 Embedding 和 Rerank 的 Base URL 改成当前可达地址。
2. 无 UI 权限时，停掉 Core 后改库再启动（`models.parameters` 是 BLOB）：

```python
# Core 必须停机；配置有缓存，热改不生效
import sqlite3
c = sqlite3.connect("data/weknora.db")
for mid, params in c.execute("SELECT id, parameters FROM models").fetchall():
    raw = bytes(params)
    if b"172.26.0.1" in raw:          # 换成旧地址
        c.execute("UPDATE models SET parameters=? WHERE id=?", (raw.replace(b"172.26.0.1", b"127.0.0.1"), mid))
c.commit()
```

**验证通过的标准**：检索返回的 `meta.rerank.applied` 为 `true`、`candidate_count` 明显大于 0、`top_score` 接近 1。只有 `data` 非空还不够——候选数才是向量链路是否真的走通的证据。

### 5.4 mirrored 启动失败：网页打不开的那种

**现象**：`start.cmd` 说服务都起来了，浏览器却连 `http://localhost:18880` 都打不开。WSL 里 `ip -br addr` 只有 `lo`，`wslinfo --networking-mode` 返回 `none`。

**当时怎么确认**：`wsl.exe --shutdown` 后重新进入，WSL 会打印真实原因：

```
wsl: 无法配置网络设置 (networkingMode Mirrored)，正在改用 networkingMode None。
错误代码: CreateInstance/CreateVm/ConfigureNetworking/0x8007054f
```

**原因**：WSL 的 Hyper-V 网络层在启动期配置 mirrored 失败（0x8007054f），于是回退。`dnsTunneling=true` 时回退目标是 `none`——**WSL 完全没有网卡**，Windows 既进不来、WSL 也出不去；把它关掉后回退成 `nat`，至少网页可用。注册表里残留 `ProxyServer=127.0.0.1:7897`（即使 `ProxyEnable=0`）是已知诱因之一。

**处理顺序**：

1. 先重启一次 Windows——这是本机唯一确证有效的修复（`wsl --shutdown` 不够，HNS 状态卡在系统层）。
2. 重启后运行 `start.cmd -FixNetwork` 一次，让它重新尝试 mirrored 并清掉失败标记。
3. 还不行就退回 NAT（`networkingMode=nat`、`dnsTunneling=false`）：网页恢复，但见下面的取舍。

**NAT 下的取舍**（`./start-all.sh status` 会如实显示）：

| 服务 | NAT 下从 WSL 是否可达 | 说明 |
|---|---|---|
| Core :18880、Host :8090 | 可达 | 都跑在 WSL 里 |
| docreader :50051 | 可达 | Docker Desktop 的端口转发在 WSL 内也可见 |
| MinerU :8000、embedding :8081、rerank :8082 | **不可达** | 它们绑在 Windows 的 `127.0.0.1`，NAT 下 WSL 只能到网关 IP |

要让本地模型在 NAT 下也能用，需要两步：把服务改绑到 WSL 网关地址（MinerU 的 `--host`、supervisor 的模型 `--host`——后者目前还没有绑定地址开关），再按 5.3 把 Core 里保存的模型 base URL 改成 `http://<网关IP>:8081/v1`。默认不自动做，因为这会改变原生模型配置并让服务对局域网可见。

## 6. 配置、目录和启动顺序

| 项目 | 默认值 / 行为 |
|---|---|
| `AGENTRAG_MODE` | 脚本固定 `worker`；实际变量名不是 `HOST_MODE` |
| `AGENTRAG_EXT_DB` | 仓库根 `data/agentrag_ext.db`，脚本转换成绝对路径 |
| `DB_PATH` | 原生数据库，默认 `data/weknora.db`；仍由 Core 使用 |
| `AGENTRAG_WEKNORA_URL` | `http://127.0.0.1:$CORE_PORT`，默认 8080，沿用已保存的端口（本机 18880） |
| `AGENTRAG_TENANT_ID` / `AGENTRAG_KB_IDS` | 明确的空间和知识库范围；KB 逗号分隔 |
| `MINERU_ENDPOINT` | Windows MinerU origin；脚本映射到 `AGENTRAG_PARSER_UPSTREAM_URL` |
| `AGENTRAG_PARSER_PROXY_ADDR` | Core → Host，固定为 `http://127.0.0.1:8090` |
| `AGENTRAG_RECONCILE_INTERVAL` | 一键脚本默认 `5s`；直接运行 Host 默认 `1m` |
| 生命周期扫描 | 启动先补扫，后续默认每 24 小时扫描 |
| `ENV_FILE` | 显式指向仓库 `.env`；原生 Lite 默认自动加载的是 `.env.lite` |
| `SSRF_DISABLE` | 本机脚本默认 `true`，沿用已批准的本地模型接入开关；可显式设 `false` 恢复原生检查 |
| `CORE_PORT` | 一键脚本默认 8080（本机保存的是 18880），可覆盖；Host 的 8090 不可覆盖 |
| `CORE_BINARY` | 默认仓库根 `WeKnora-lite`，可指向另一份已编译 Linux 核心 |
| `AGENTRAG_LAUNCHER_CONFIG` | 默认 `data/agentrag-launcher.json`，保存私有机器凭证及范围 |
| `AGENTRAG_RUN_DIR` | 默认 `data/launcher-run-*`，保存本次日志、二进制和私有运行环境 |
| `MODEL_SUPERVISOR_TOKEN` / `_ADDR` / `_BIN` / `_STATE` | 只存私有状态，**不写进 private.env**；令牌首次生成后固定，用 `./start-all.sh token` 取，`start.cmd` 负责注入 |
| `MODEL_SERVICES` / `DOCKER_SERVICES` / `MCP_ENABLED` / `MCP_ADMIN_KEY` | 一键启动的计划：模型清单（JSON）、要拉起的容器、是否自动注册 MCP、专用注册 key；均可用同名环境变量覆盖 |
| `DOCREADER_ADDR` / `DOCREADER_TRANSPORT` | 由启动器按 50051 是否真的可达决定（可达写 `127.0.0.1:50051` / `grpc`，否则留空）；显式设置时优先 |
| `AGENTRAG_DOCREADER_IMAGE` | docreader 容器镜像；docker.io 不可达时用它固定本地已有镜像 |

配置优先级为当前进程环境变量 → 已保存私有配置 → 默认值。更换已有实例的原生库路径或端口时，显式传入 `DB_PATH` / `CORE_PORT`；配置保存后下次继续沿用。若 `.env` 的 DB 路径使用复杂变量表达式，启动器会要求显式 `DB_PATH`，避免猜测后选错原生库。`AGENTRAG_RUN_DIR` 必须是空目录且不能是符号链接；每次启动使用独立目录，旧日志保留。

私有目录在 Git 忽略范围内；Linux 创建时使用限制权限。WSL 挂载盘还受 Windows ACL/挂载权限控制，不能单凭 `chmod` 就宣称完成了 Windows 侧访问隔离。不要分享整个运行目录或 `private.env`。

上述忽略规则覆盖默认路径；自定义 `AGENTRAG_LAUNCHER_CONFIG` / `AGENTRAG_RUN_DIR` 时，选择仓库外的私有目录，或自行确认该路径已忽略。

实际启动顺序（`start.cmd` 的完整链路）：

1. 读/生成私有状态（令牌、模型清单、容器清单、MCP 开关），解析原生库路径、独立扩展库、授权范围。
2. 网络：非 mirrored 时改写 `.wslconfig` 并重启 WSL。
3. Windows 侧：Docker Desktop 与 `docreader` → MinerU → supervisor → 按清单确保 embedding/rerank 处于 running 并做能力验真。
4. WSL 侧从自己这边实测 `8092/8000/8081/8082`，缺失只降级告警（`AGENTRAG_STRICT=1` 才中止），检查 18880/8090 端口。
5. 构建 Host，启动 `worker`，打开/迁移扩展库，完成首次生命周期过期扫描。
6. 等待 Host `/health` 返回 HTTP 200 且 `status=ready`，再启动 Core。
7. 等待 Core `/health`；验证机器 API Key 的知识库权限。首次登录签发新凭证时，优雅重启 Host 加载凭证，再等待就绪。
8. MCP 幂等注册/刷新（有专用 key 时；失败只告警）。
9. 输出 `[READY] AgentRAG`，维持 Host 与 Core；任一退出时清理另一个（Windows 侧服务保持运行）。

Host 的本地就绪不依赖 Core 在线，避免相互等待。原生凭证验证是 Core 启动后的独立步骤；此前对账可能报告 `reconciliation_failed`，未知文档仍按非 active 排除。

## 7. 拆分启动（调试用）

只在排查 Core/Host 本身时使用。Windows 侧（MinerU、supervisor、模型）仍需先由 `start.cmd` 或 4.1–4.6 的命令拉起，否则从 WSL 侧看不到模型，RAG 会缺少向量与重排能力。建议先完成一次一键初始化，再 Ctrl+C。之后每个 WSL 终端都加载相同的私有环境，避免 Core 和 Host 使用不同代理密钥。

终端 A，Host：

```bash
cd /mnt/d/LanRAG-Next
umask 077
export PATH=/usr/local/go/bin:$PATH
task_env=$(mktemp)
python3 extensions/scripts/launcher_config.py env "$task_env"
source "$task_env"
rm -- "$task_env"
cd extensions
CGO_ENABLED=0 go run -mod=readonly ./cmd/extension-host
```

终端 B，先检查 Host，再启动 Core：

```bash
cd /mnt/d/LanRAG-Next
umask 077
task_env=$(mktemp)
python3 extensions/scripts/launcher_config.py env "$task_env"
source "$task_env"
rm -- "$task_env"
curl --fail http://127.0.0.1:8090/health
SERVER_HOST=127.0.0.1 SERVER_PORT="$CORE_PORT" ./WeKnora-lite
```

拆分启动时每个终端自行 Ctrl+C；它们不受 `start-all.sh` 的联合清理管理。若只需启动纯原生 Core，最低命令如下，但 RAG 依然需要工作中的 Host：

```bash
cd /mnt/d/LanRAG-Next
ENV_FILE="$PWD/.env" WEKNORA_WEB_DIR="$PWD/web" SSRF_DISABLE=true ./WeKnora-lite
```

## 8. 健康检查和日志

一条命令看全部（WSL 侧 + Windows 侧 + 当前网络模式）：

```bash
cd /mnt/d/LanRAG-Next && ./start-all.sh status
```

逐项探针：

```bash
curl --fail http://127.0.0.1:8090/bridge/healthz
curl --fail http://127.0.0.1:8090/health
curl --fail http://127.0.0.1:8090/readyz
curl --fail http://127.0.0.1:18880/health      # 本机 Core 端口，见开头说明
curl --fail http://127.0.0.1:8092/healthz      # 模型 supervisor
```

`status` 里的 `MinerU/supervisor/embedding/rerank` 是从 **WSL 侧**发出的请求，正好等于 Core 实际使用的路径；Windows 侧能打开不代表 WSL 能打开。

| 探针 | 成功含义 |
|---|---|
| `/bridge/healthz`、`/healthz` | Host HTTP 存活，返回 `status=ok`；不证明业务就绪 |
| Host `/health`、`/readyz` | worker 本地数据库、服务和首次扫描已初始化，返回 `status=ready, scope=local` |
| Core `/health` | 原生 HTTP 服务已响应；不证明模型、解析、权限或 RAG 已成功 |

probe 模式没有业务数据库，就绪返回 503，`before_retrieve` 同样 503。当前 worker 使用真实持久策略，只有 active 文档可进入原生检索；未知、stale、archived、superseded、failed 等状态排除。其他尚未接入的业务 Hook 仍可能返回 503。

脚本会打印本次日志目录；在该目录查看 `host.log`、`core.log`。Core 日志可能含用户问题/文档元数据，分享前脱敏。`private.env`、私有 JSON 和旧脚本备份不能作为公开诊断日志。

## 9. 数据库初始化与恢复

**两个库必须分离：** `weknora.db` 保存原生业务，`agentrag_ext.db` 保存扩展事实。当前源库曾手动混入扩展表，旧表保留；Host 不会继续接管混合库，也不会从中自动搬运生命周期事实。

新建独立扩展库后，Observer 通过官方 API 对账已完成文档，按日期初始化状态；无法凭此恢复旧库中未迁移的治理决定或中间历史。如混合库已存在必须保留的治理事实，应先专项审核，不可把新库当作历史迁移完成。

独立扩展库如果手动执行过发布的 SQL、但缺少 `ext_schema_migrations`：迁移器只在 DDL 与某个完整已知迁移前缀严格一致、外键检查通过时，同事务补齐历史并继续升级。已存在但结构不一致、部分 SQL 执行失败、未知扩展表等情况会拒绝自动认领；不会简单跳过 `already exists`。

不要手动删除迁移记录，不要对原生库执行 reset，不要在运行中移动 `.db` 或单独删除 `-wal` / `-shm`。

若仅需另建一个本地测试扩展库，并且明确接受不加载旧扩展事实，可保留原文件并显式指定新的文件名：

```bash
AGENTRAG_EXT_DB="$PWD/data/agentrag_ext-fresh-test.db" bash start-all.sh
```

这只是新建测试库，不是数据修复或迁移。对真实治理状态不使用该方法替代恢复。

SQLite 接受相对路径，Host 会规范化绝对路径；没有“必须绝对路径”或“modernc 一律不支持 `/mnt/d` WAL”的结论。本轮 E2E 的扩展与原生测试库均实际放在 `/mnt/d/LanRAG-Next/tmp/`，已完成读写、关闭重启和外键检查。扩展仍使用 WAL、`busy_timeout=5000`、单写者；挂载模式、其他工具或跨系统同时写入仍可能引发锁问题。

如果 `/mnt/d` 持续出现 I/O/锁错误，可只把扩展库放到 WSL 的 Linux 文件系统：

```bash
mkdir -p "$HOME/.local/share/agentrag"
AGENTRAG_EXT_DB="$HOME/.local/share/agentrag/agentrag_ext.db" bash start-all.sh
```

上例选择的是新库位置；已有扩展事实需要停机后进行一致备份和单独恢复。不要让 Windows SQLite 工具与 WSL Host 同时写同一个扩展库。

## 10. 常见错误

| 现象 | 处理 |
|---|---|
| 浏览器 API 报 `401 missing authentication` | 原生仍需 JWT 或机器 API Key；`WEKNORA_TENANT_ENABLE_RBAC=false` 不关闭认证。页面登录后使用；脚本只为 Host 获取机器凭证，不替浏览器保存登录态 |
| `/auth/auto-setup` 返回 401 | 该能力由桌面宿主注入私有 token，普通 `cmd/server` Lite 不会自动注入。使用官方登录/API Key 流程，不硬造桌面 token |
| `native/mixed database refused` | 检查 `AGENTRAG_EXT_DB`；使用独立扩展库，保留原生库 |
| `untracked schema ... differs` | 已有 DDL 无法严格匹配迁移，拒绝自动认领；保留库和日志再核对，不删表硬凑 |
| `SQLite is busy` | 停止其他写者，确认进程退出，再重试；持续失败时评估 Linux 文件系统路径 |
| Host 存活、就绪 503 | 可能是 probe 模式，或初始化未完成；检查 worker 配置和 Host 日志 |
| `reconciliation_failed` | 查看原生是否已启动、机器凭证/KB 范围是否有效；初始 Core 尚未启动时允许暂时出现，持续出现需处理 |
| `[Bridge] before_retrieve failed, blocking search to prevent policy bypass` | 正常 fail-closed 证据；检查同一 WSL 中的 Host 就绪、8090 监听和响应协议，不能绕过门卫 |
| 文档解析完成但暂时搜不到 | Observer 周期对账后才有生命周期记录；脚本默认约 5 秒。未知文档仍被排除 |
| 文档 stale 后仍有普通生成回复 | 原生允许无知识结果的生成回退；验收检查过期引用和实际模型上下文，不把“调用 LLM”直接认定为检索泄漏 |
| 原生模型新增/调用失败 | 从 Core 所在 WSL 检查 URL、服务监听、能力参数、维度；本机 SSRF 开关不解决网络不可达 |
| PDF 长时间等待/下载 OCR 模型 | 先查预检/路由和 MinerU 实际档位；文本层 PDF 应走 txt/flash，扫描件才允许需要 OCR 的档位 |
| 标签投影重试 | C-10 Baseline Limitation：原生标签未知时安全拒绝替换；生命周期 truth 不因此回滚，不能为同步误删用户标签 |
| `[WARN] 从 WSL 不可达: supervisor/MinerU/embedding/rerank` | 多半是 WSL 仍是 nat 模式，或 Windows 侧服务没起。在 Windows 跑 `start.cmd`（自动切 mirrored 并拉起）；只想要 Core/Host 可忽略 |
| `container docreader failed; local candidate image(s): ...` | 拉不到 Docker Hub。按提示 `docker tag <已有镜像> wechatopenai/weknora-docreader:latest` 后重跑，或 `-SkipDocker` / `AGENTRAG_DOCKER_SERVICES=` 关掉；不影响 Core/Host |
| 模型控制台报 `missing or invalid credential` | 浏览器存的令牌与当前 supervisor 不一致。页面收到 401 会自动清空并要求重填；当前值用 `./start-all.sh token` 取（唯一会打印它的地方） |
| `runtime ... is a Windows executable but this supervisor is a linux build` | supervisor 跑在 WSL 里了。用 `start.cmd` 拉起，或按 4.6 在 Windows 侧直接运行 |
| `[READY] AgentRAG already running ... nothing to do` | 不是错误：Core/Host 已就绪，本次什么都没做（幂等） |
| 浏览器打不开 `http://localhost:18880`，但启动器说服务都起来了 | WSL 很可能回退成 `networkingMode=none`（只有 `lo` 网卡）。`./start-all.sh status` 看 `wsl mode`；处理见 5.4：重启 Windows 后 `start.cmd -FixNetwork`，或退回 NAT |
| 解析引擎页显示「未连接 gRPC」 | docreader 容器没起，或 Core 启动时 `DOCREADER_ADDR` 为空。`docker ps --filter name=WeKnora-docreader` 看容器；`./start-all.sh status` 看 `docreader :50051`；起来后需要重启 Core（`start.cmd`）地址才生效 |
| `MinerU 服务不可用 … http://172.0.0.1:8000/v1/health` | 旧启动留下的网关地址（镜像模式下根本没有这个地址）。`prepare` 现在每次都会重校验 `MINERU_ENDPOINT`，不可达且本机 8000 可用时自动改回 `127.0.0.1` 并在 stderr 说明；**地址是启动时读入 Host 环境的，改完要重启一次**（`start.cmd`，或 Ctrl+C 后重跑） |
| 端口占用：`Port 18880/8090 already in use` | 已有实例占着端口。先 `./start-all.sh status`；确认是旧实例后用 Ctrl+C 或 `./start-all.sh stop` 收尾，不要直接杀未知进程 |

## 11. 自动化自测与当前证据

### M2来源治理增量（2026-11-01）

本批原生源码需要重新构建。WSL仓库根目录执行：

```bash
export PATH=/usr/local/go/bin:$PATH
CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 \
  -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' \
  -o tmp/governance-WeKnora-lite ./cmd/server
# 使用候选二进制，沿用现有私密启动配置；需先退出正在运行的同端口实例
CORE_BINARY="$PWD/tmp/governance-WeKnora-lite" ./start-all.sh
```

普通RAG的摘要/描述、Agent search/read/list等现在需要Host当前active许可。M2暂时关闭历史重放/暂停恢复、Wiki/图谱混合读取、页面read/fill/select、旧答案亲和记忆背景、任意SQL/会话检索/通用操作及不透明外部MCP执行。遇到`complete source provenance unavailable`属于安全拒绝，原有历史/文档未删除。不要用测试放行夹具或恢复M0 Pass-Through来解决。

指定链路自动验证（真实Core/Host/上传/索引，模型为本地输入捕获夹具）：

```bash
wsl -d Ubuntu --exec unshare -Urn bash -lc 'ip link set lo up; export PATH=/usr/local/go/bin:$PATH; cd /mnt/d/LanRAG-Next && python3 extensions/scripts/governance_e2e.py'
```

命令在Windows终端执行；若已位于WSL，可直接运行`unshare -Urn bash -lc 'ip link set lo up; export PATH=/usr/local/go/bin:$PATH; cd /mnt/d/LanRAG-Next && python3 extensions/scripts/governance_e2e.py'`。脚本自身只运行隔离服务，不停用户实例。验收范围/冲突/测试限制见[来源治理报告](../design/M2-Governance-Leak-Closure-2026-11-01.md)，完整输出见[来源治理E2E日志](../evidence/m2-governance-leak-e2e-2026-11-01.log)。

在没有其他 Host 占用 8090 时：

```bash
cd /mnt/d/LanRAG-Next
python3 extensions/scripts/startup_e2e.py
```

该脚本真实执行：隔离原生账户和 KB 准备 → 停止准备进程 → 一键脚本 Host 先就绪 → Core → 官方登录签发机器凭证 → 原生 multipart TXT 上传 → 原生分块/索引 → Observer active → 正常 RAG → Ctrl+C 清理 → 测试输入有效期设为过去 → 重启 → 生产扫描 active→stale → 原生 RAG 引用和实际 LLM 上下文不含过期文档。

有效期仅在隔离库内作为测试输入写入，脚本不直接改 lifecycle current/event。原生用户数据库只核对校验值，保持不变；本地模型夹具用于稳定验证门禁，不调用 DeepSeek，不代表真实 MinerU/PDF 或生产模型联合验收。

2026-10-31 实际执行退出 0，完整脱敏终端记录见 [M2 启动 E2E 日志](../evidence/m2-startup-e2e-2026-10-31.log)。本轮没有重跑真实 Windows MinerU 或云端模型，也没有完成 M2 全部 UI/配置验收。

补充取消测试已在独立 Linux 网络命名空间实际通过，避免占用当前正在运行实例的 8090。命令和完整检查范围见 [启动修复验证记录](../evidence/m2-startup-validation-2026-10-31.md)。

## M2 生命周期界面

使用原有 `./start-all.sh` 启动，保持Host先ready后Core的顺序。当前Windows地址仍为 `http://localhost:18880`；登录后侧栏进入“生命周期”，或访问 `http://localhost:18880/platform/lifecycle`。

统一启动器自动生成至少32字节的随机 `AGENTRAG_MANAGEMENT_KEY`，仅存入私密配置并同时传给Core/Host。分开启动时两进程必须设置相同值；不要放入VITE变量、URL、日志或提交文件。旧进程需重启以装载配置。Host离线或凭据缺失时页面显示不可用，业务检索继续Fail-Closed。

管理入口沿用原生JWT和KB权限。Viewer只读；同租户Contributor须为KB创建者才能编辑；管理员/Owner及原生认可的跨库Editor按权限编辑。API Key不能调用管理入口。worker仅管理配置的tenant/KB，不自动纳管其他知识库。

文档页提供状态过滤、分页、日期和最近100条事件。日期按本机时区显示，提交UTC；保留原标签和未编辑日期精度。旧revision返回409，需重新载入；网络中断后先载入确认实际结果，不盲目重试。日期保存后独立重评估状态，暂未完成时明确提示待扫描。过期即失效，清空日期不会复活失效态。

**冲突点／Baseline Limitation**：矩阵不含active→scheduled，因此已生效文档不能改为未来生效，scheduled不能清空生效时间。服务器在同一CAS事务拒绝，日期/revision/状态不变。重新排期须先定义迁移规则。

默认规则为0–36500整数天；0不设对应默认日期，未设失效日期即永久有效。库规则覆盖租户默认值，仅影响后续初始化。待复核只告警不改状态，由扫描更新提醒；列表最多1000条并提示截断。界面不提供归档审批、谱系和Revision Publish。

隔离自测：`python3 extensions/scripts/lifecycle_ui_e2e.py`（Linux，8090须空闲，亦可放入独立网络命名空间）。脚本只创建临时账号、Core/Host数据库、上传TXT和本地模型夹具，不修改真实文档；不代表云模型/MinerU联合验收。结果见 [M2界面验证记录](../evidence/m2-lifecycle-ui-validation-2026-11-01.md) 及 [完整脱敏E2E日志](../evidence/m2-lifecycle-ui-e2e-2026-11-01.log)。
