# Windows 桌面版外部服务部署指引（docreader、MinerU）

本文适用于已安装的 LanRAG Lite Windows x64 桌面版，说明除模型服务外需要部署和配置的文档解析服务。桌面版随包提供 Core、Extension Host、模型进程管理器、llama-server、网页、SQLite 和中文分词资源；docreader、MinerU 及其运行环境由用户准备。

本文不涉及模型服务：Embedding、Rerank 和生成模型在桌面版的“模型进程管理”或原生模型设置中配置。Lite 使用 SQLite 与内存队列，不需要 PostgreSQL、Redis、Neo4j。

当前桌面版的解析链路有两条：

- **非 PDF 文件**：按知识库的解析引擎配置，走内置 Go 解析器或已配置的 docreader。
- **PDF 文件**：如果在 `.env.lite` 中设置 `AGENTRAG_PARSER_UPSTREAM_URL`，Core 会强制把本地 PDF 交给 Desktop 内置 Host，再由 Host 调用 MinerU；未设置时才按知识库配置选择 docreader 或其他 PDF 引擎。设置该变量会覆盖知识库对 PDF 引擎的选择。

## 1. 先判断需要哪些服务

| 要解析的内容 | 是否必需 | 使用的服务 |
| --- | --- | --- |
| TXT、Markdown、CSV、JSON | 否 | 内置 Go SimpleFormatReader |
| PDF | 是（二选一） | MinerU（推荐），或 docreader 的 builtin PDF 解析器 |
| Word（doc/docx）、PPT（ppt/pptx）、Excel（xls/xlsx）、EPUB、HTML/MHTML、XMind | 是 | docreader |
| 图片、音频 | 否 | 内置 Go 处理；OCR/转写依赖模型配置 |
| 网页 URL 抓取 | 按需 | docreader |
| Excel/spatial 的 DuckDB 扩展 | 否 | 不是服务，见第 9 节 |

> 说明：docreader 自身不做 OCR，也不做视觉模型推理；扫描页和插图会作为图片回传给 Go App，由模型配置负责 OCR 和图片描述。MinerU 也支持部分 Office 格式，但桌面版的 Host 代理只接管 PDF；如果要对非 PDF 文件使用 MinerU，需要在知识库解析引擎中单独选择 MinerU 并填写 MinerU 地址。

本机常用路径：

| 用途 | 路径 |
| --- | --- |
| 用户数据目录 | `%LOCALAPPDATA%\LanRAG Lite` |
| 用户配置文件 | `%LOCALAPPDATA%\LanRAG Lite\.env.lite` |
| 安装资源示例 | `C:\Users\ybochuan\AppData\Local\Programs\LanRAG Lite\resources\.env.lite.example` |
| MinerU 虚拟环境 | `D:\mineru-venv` |
| 本仓库 | `D:\LanRAG-Next` |

## 2. 端口和进程

| 服务 | 运行位置 | 默认地址 | 说明 |
| --- | --- | --- | --- |
| docreader | Docker Desktop 的 Linux 容器 | `127.0.0.1:50051`（gRPC） | 复杂格式解析；容器名 `WeKnora-docreader` |
| MinerU | Windows 本机进程 | `http://127.0.0.1:8000` | 自建 MinerU 4.0.1，PDF 解析 |
| Extension Host | Desktop 随包进程 | `http://127.0.0.1:8090` | 桌面版自动启动，不对外 |

这些服务都只绑定本机回环地址即可，不需要暴露到局域网。

## 3. 部署 docreader

docreader 是运行在 Linux 环境下的 Python gRPC 服务；本文使用 Docker Desktop 的 Linux 容器方式部署，不能用 Windows 容器模式。若改用 WSL 源码运行，可参考[文档解析服务文档](../../website-docs/03-features/03-document-parsing.md)中的源码启动说明。

### 3.1 启动 Docker Desktop

先确认 Docker CLI 和 Linux 引擎可用：

~~~powershell
docker version
~~~

如果报 `failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine`，说明 Docker Desktop 没启动。启动后等托盘图标稳定，再执行：

~~~powershell
Start-Process 'C:\Program Files\Docker\Docker\Docker Desktop.exe'
docker info
~~~

`docker info` 能输出 Server 信息才算就绪。

### 3.2 启动 docreader 容器

**方式 A（本仓库推荐）**：仓库已经提供把 50051 发布到回环地址的 Compose override。在 PowerShell 中执行：

~~~powershell
Set-Location 'D:\LanRAG-Next'
docker compose -f docker-compose.yml -f docker-compose.docreader-host.yml up -d --wait --no-build docreader
~~~

`--wait` 会等到健康检查通过。首次启动或镜像较大时可能要几分钟。

如果 Docker Hub 拉取失败，而本机已经有可用的 SWR 镜像，先拉取并固定镜像名：

~~~powershell
docker pull swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/wechatopenai/weknora-docreader:v0.8.0
docker tag swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/wechatopenai/weknora-docreader:v0.8.0 wechatopenai/weknora-docreader:latest
~~~

也可以在启动 Compose 前指定镜像：

~~~powershell
$env:AGENTRAG_DOCREADER_IMAGE = 'swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/wechatopenai/weknora-docreader:v0.8.0'
docker compose -f docker-compose.yml -f docker-compose.docreader-host.yml up -d --wait --no-build docreader
~~~

**方式 B（没有本仓库时）**：用等价的 `docker run`。容器名和 Compose 方式相同，二选一，不要同时创建。

~~~powershell
docker run -d --name WeKnora-docreader --restart unless-stopped `
  -p 127.0.0.1:50051:50051 `
  -e DOCREADER_GRPC_PORT=50051 `
  -e DOCREADER_GRPC_MAX_WORKERS=4 `
  -e DOCREADER_GRPC_MAX_FILE_SIZE_MB=50 `
  -v WeKnora-docreader-tmp:/tmp/docreader `
  --health-cmd "grpc_health_probe -addr=localhost:50051" `
  --health-interval 30s --health-timeout 10s --health-retries 3 --health-start-period 60s `
  wechatopenai/weknora-docreader:latest
~~~

### 3.3 验证

~~~powershell
docker ps --filter name=WeKnora-docreader --format "table {{.Names}}\t{{.Status}}"
docker inspect --format "{{.State.Health.Status}}" WeKnora-docreader
Test-NetConnection 127.0.0.1 -Port 50051
~~~

预期：容器状态为 `Up ... (healthy)`，健康状态为 `healthy`，50051 端口 TCP 可达。首次启动时 `health` 会在 `start_period` 内显示 `starting`，等待即可。

### 3.4 可选：认证和 TLS

默认 Compose 配置没有启用 gRPC 认证和 TLS，只在本机回环使用是安全的。如果设置了容器的 `GRPC_AUTH_TOKEN`，需要在 `.env.lite` 中写入同一个 `GRPC_AUTH_TOKEN`；如果启用 TLS/mTLS，按 docreader 文档把 `GRPC_TLS_ENABLED`、`GRPC_TLS_CA`、`GRPC_TLS_CERT`、`GRPC_TLS_KEY`、`GRPC_TLS_SERVER_NAME` 等同名变量写入 `.env.lite`。这些值只放用户配置目录，不要写进安装目录的示例文件。

## 4. 部署 MinerU

### 4.1 确认本机环境

本机已经有 `D:\mineru-venv`，实测版本为 MinerU 4.0.1、Python 3.11.3。先确认：

~~~powershell
& 'D:\mineru-venv\Scripts\mineru.exe' --version
~~~

预期输出包含 `MinerU version: 4.0.1`。如果需要在新机器上重建环境，可用 Python 3.11 创建虚拟环境，再安装固定版本；本机已有环境不需要重新安装：

~~~powershell
py -3.11 -m venv D:\mineru-venv
& 'D:\mineru-venv\Scripts\python.exe' -m pip install --upgrade pip
& 'D:\mineru-venv\Scripts\python.exe' -m pip install 'mineru==4.0.1'
~~~

本机当前使用基础安装（未安装 `torch`/`lmdeploy`），足以启动 MinerU API。不要默认安装 `mineru[all]`：在 Windows 上它会带入 `lmdeploy`、`qwen-vl-utils` 等较大的附加依赖。需要 GPU/VLM 后端时，再按 MinerU 官方说明选择对应的 extra。

### 4.2 启动 MinerU API

在 **Windows PowerShell** 中前台启动，便于观察日志：

~~~powershell
& 'D:\mineru-venv\Scripts\mineru-api.exe' --host 127.0.0.1 --port 8000
~~~

如果 MinerU 需要鉴权，增加 `--api-key`；该值必须与 `.env.lite` 的 `AGENTRAG_PARSER_UPSTREAM_KEY` 完全一致：

~~~powershell
& 'D:\mineru-venv\Scripts\mineru-api.exe' --host 127.0.0.1 --port 8000 --api-key 'replace-with-a-long-random-key'
~~~

常用可选参数：

- `--tier flash`：文本层 PDF 优先走轻量档；`--tier standard` 适合扫描件、复杂版式，但更慢、更吃资源。默认会广告 advanced 能力，资源不足时可用 `--no-advanced` 关闭。
- `--concurrency 1`：默认并发为 1；不要在不了解显存和内存的情况下盲目增大。
- `--preload-models`：启动时预加载模型，避免第一次解析等待，但会延长启动时间并占用资源。

首次解析可能需要下载或加载模型，耗时较长。请保持窗口运行；关闭窗口或按 Ctrl+C 会停止服务。

### 4.3 验证

~~~powershell
curl.exe --fail --silent --show-error http://127.0.0.1:8000/v1/health
Test-NetConnection 127.0.0.1 -Port 8000
~~~

预期返回类似 `{"status":"ok","version":"4.0.1"}`。注意 MinerU 4.0 的健康地址是 `/v1/health`，不是 `/health`。

### 4.4 可选：登录时自动启动

MinerU 不由桌面应用托管。如果希望登录 Windows 后自动启动，可在当前用户的启动文件夹放一个最小化快捷方式：

~~~powershell
$startup = [Environment]::GetFolderPath('Startup')
$ws = New-Object -ComObject WScript.Shell
$lnk = $ws.CreateShortcut((Join-Path $startup 'MinerU API.lnk'))
$lnk.TargetPath = 'D:\mineru-venv\Scripts\mineru-api.exe'
$lnk.Arguments = '--host 127.0.0.1 --port 8000'
$lnk.WorkingDirectory = 'D:\mineru-venv'
$lnk.WindowStyle = 7
$lnk.Save()
~~~

如果使用 `--api-key` 或其他参数，把它们一并写进 `$lnk.Arguments`。自动启动失败时，仍可按 4.2 手工启动排查。

## 5. 配置 `.env.lite`

桌面版只在启动时读取用户数据目录下的 `.env.lite`。不要修改安装目录里的 `.env.lite.example`，它只是模板。

MinerU 代理由 Desktop 内置的 Host 承载；Host 需要当前本地账号至少有一个可访问的知识库才会启动。如果还没有知识库，先在原生 Lite 界面创建一个空知识库，再退出并重新打开应用。之后增删纳管知识库，也需要退出重开，让 Host 重新生成 parser scope。

如果还没有配置文件，先复制：

~~~powershell
$dataDir = Join-Path $env:LOCALAPPDATA 'LanRAG Lite'
$example = Join-Path $env:LOCALAPPDATA 'Programs\LanRAG Lite\resources\.env.lite.example'
if (-not (Test-Path (Join-Path $dataDir '.env.lite'))) {
  Copy-Item $example (Join-Path $dataDir '.env.lite')
}
notepad (Join-Path $dataDir '.env.lite')
~~~

如果你已经有 `.env.lite`，只补充或修改外部服务相关行，不要改动其中已有的 `MODEL_SUPERVISOR_MODEL_DIR` 和模型配置。按实际启用的服务填写下面几行；不用的服务留空即可：

~~~dotenv
# docreader（可选）
DOCREADER_ADDR=127.0.0.1:50051
DOCREADER_TRANSPORT=grpc

# MinerU 自建服务（可选；设置后本地 PDF 强制走 Host -> MinerU）
AGENTRAG_PARSER_UPSTREAM_URL=http://127.0.0.1:8000
AGENTRAG_PARSER_UPSTREAM_KEY=

# 可选：长文档超时（Go duration；默认 1000s）
# WEKNORA_MINERU_TIMEOUT=30m
# AGENTRAG_PARSER_BULK_TIMEOUT=30m

# 保持模板默认
MODEL_SUPERVISOR_MODEL_DIR=
DUCKDB_SKIP_EXTENSION_LOAD=1
~~~

各变量的含义：

| 变量 | 填写规则 |
| --- | --- |
| `DOCREADER_ADDR` | `主机:端口`，不要带 `http://`；本机为 `127.0.0.1:50051`。不启用 docreader 时留空。 |
| `DOCREADER_TRANSPORT` | 固定为 `grpc`。 |
| `AGENTRAG_PARSER_UPSTREAM_URL` | MinerU 的 origin，只填 `http://127.0.0.1:8000`；不要带 `/v1`、`/parser/native` 或末尾斜杠。不启用 MinerU 时留空。 |
| `AGENTRAG_PARSER_UPSTREAM_KEY` | 与 `mineru-api --api-key` 完全一致；MinerU 未启用鉴权时留空。 |
| `WEKNORA_MINERU_TIMEOUT` | 可选，单个 PDF 的解析超时，默认 1000 秒；长文档可设 `30m`。 |
| `AGENTRAG_PARSER_BULK_TIMEOUT` | 可选，Host 的批量解析超时，默认 1000 秒；长文档可设 `30m`。 |
| `DUCKDB_SKIP_EXTENSION_LOAD` | 默认 `1`，跳过 DuckDB 扩展下载；需要 Excel/spatial 且可访问扩展仓库时才改为 `0`。 |

**不要自行设置** `AGENTRAG_PARSER_PROXY_ADDR`、`AGENTRAG_PARSER_PROXY_KEY`、`AGENTRAG_PARSER_SCOPES`、`AGENTRAG_PARSER_ARTIFACT_ROOT`：这些由 Desktop 启动 Host 时生成或注入。`AGENTRAG_PARSER_PROXY_KEY` 是 Core 与 Host 之间的共享密钥，和 MinerU 的 `--api-key` 不是一回事。

保存后必须**完全退出并重新打开 LanRAG Lite**，让 Core 和 Host 重新读取配置。只关闭窗口后如果仍有 `LanRAG Lite.exe`、`LanRAG Desktop.exe` 或 `extension-host.exe` 在任务管理器中运行，先结束这些残留进程，再重新打开。

## 6. 在界面中启用和选择引擎

### 6.1 docreader

打开知识库设置 → 解析引擎。`builtin` 一栏应显示“已连接 gRPC”和 `DOCREADER_ADDR` 提示。没有显示时，按第 8 节检查地址、容器和重启状态。

docreader 提供的引擎通常包括 `builtin`、`markitdown`、`opendataloader`。解析引擎是**每个知识库**的配置，修改后只影响之后入库的文档。

### 6.2 MinerU

设置 `AGENTRAG_PARSER_UPSTREAM_URL` 后，本地 PDF 会自动走 Host → MinerU，知识库里的 PDF 引擎选择不再生效，不需要在界面里重复填写 MinerU 地址。

如果还要对图片、Word、PPT 等非 PDF 文件使用 MinerU，在知识库解析引擎中选择 MinerU，并填写 MinerU 的真实地址 `http://127.0.0.1:8000` 和 `--api-key` 对应的服务端 Key。不要把 `http://127.0.0.1:8090` 填到 MinerU endpoint：8090 是 Host 的 Bridge 地址，不是 MinerU 服务地址。

## 7. 验收清单

1. `docker ps --filter name=WeKnora-docreader` 显示容器为 `Up ... (healthy)`。
2. `curl.exe --fail http://127.0.0.1:8000/v1/health` 返回 `status=ok`。
3. `Test-NetConnection 127.0.0.1 -Port 50051` 和 `-Port 8000` 均可达。
4. 退出并重新打开 LanRAG Lite；`http://127.0.0.1:8090/readyz` 正常，Core 的 `/health` 正常。
5. 用一个 DOCX/XLSX 文件验证 docreader：知识库选择 `builtin` 或其他 docreader 引擎，上传并确认解析出文本和图片。
6. 用一个文本层 PDF 验证 MinerU：保持 `AGENTRAG_PARSER_UPSTREAM_URL`，上传 PDF，确认 `core.log` 没有该文件的解析错误；如果未启用 docreader，日志中的 `[DocConverter] No DOCREADER_ADDR configured, starting disconnected` 是正常的，不影响 MinerU 代理路径。同时确认 `%LOCALAPPDATA%\LanRAG Lite\parser_artifacts` 下产生对应产物。
7. 用一个扫描件 PDF 验证 OCR 路径；如果 OCR 或图片描述失败，检查视觉模型配置，不要只怀疑 docreader/MinerU。

> 解析成功不等于入库完成。入库还需要 Embedding 模型可用、向量维度匹配；问答还需要生成模型和重排配置。模型问题不在本文范围。

## 8. 排障

| 现象 | 检查和处理 |
| --- | --- |
| `docker: command not found` 或找不到 Docker Desktop | 确认 Docker Desktop 已安装；把 `C:\Program Files\Docker\Docker\resources\bin` 加入 PATH，或从开始菜单启动 Docker Desktop。 |
| `failed to connect to the docker API ... dockerDesktopLinuxEngine` | Docker Desktop 没有运行，或 Linux 引擎没有启动；启动 Docker Desktop，等 `docker info` 成功。 |
| `docker compose` 拉取镜像超时或 403 | 使用 3.2 的 SWR 镜像并 `docker tag`，或用 `AGENTRAG_DOCREADER_IMAGE` 指定镜像。 |
| `port is already allocated` / 50051 被占用 | `Get-NetTCPConnection -LocalPort 50051 -State Listen` 找到占用进程；停止旧的 docreader 容器或改用其他端口并同步修改 `DOCREADER_ADDR`。 |
| docreader 一直 `starting` 或 `unhealthy` | `docker logs --tail 200 WeKnora-docreader` 查看启动错误；首次启动 60 秒内属于正常。 |
| 解析引擎页显示“未连接 gRPC” | 检查 `.env.lite` 是否在 `%LOCALAPPDATA%\LanRAG Lite`、`DOCREADER_ADDR` 是否为空、容器是否健康；Core 只在启动时读地址，修改后必须重启应用。 |
| docreader 报 `RESOURCE_EXHAUSTED` 或大文件失败 | 默认单文件上限约 50MB；同时提高 `MAX_FILE_SIZE_MB` 和 `DOCREADER_GRPC_MAX_FILE_SIZE_MB`，重建容器后重试。 |
| MinerU `/v1/health` 返回 404 | 用了 `/health` 或旧版路径；MinerU 4.0 使用 `/v1/health`。 |
| MinerU `/v1/health` 返回 503 | 服务已启动但模型未就绪；看 `mineru-api` 前台日志，等待模型加载或首次下载。 |
| MinerU 报认证失败 401/403 | `.env.lite` 的 `AGENTRAG_PARSER_UPSTREAM_KEY` 与 `mineru-api --api-key` 不一致；未启用鉴权时将该变量留空。 |
| Host 启动时报 `upstream URL must be an origin` 或类似错误 | `AGENTRAG_PARSER_UPSTREAM_URL` 带了路径、查询串或末尾斜杠；只保留 `http://127.0.0.1:8000`。 |
| PDF 始终走 MinerU，即使知识库选了 docreader | 这是 `AGENTRAG_PARSER_UPSTREAM_URL` 的预期行为；清空该变量并重启才会恢复知识库的 PDF 引擎选择。 |
| 大 PDF 解析超时 | 同时设置 `WEKNORA_MINERU_TIMEOUT=30m` 和 `AGENTRAG_PARSER_BULK_TIMEOUT=30m`，重启应用；再检查 MinerU 日志和资源占用。 |
| MinerU 重启后旧任务 404 | MinerU 4.0 的上传和任务状态在进程内存中；重启后重新上传文档。 |

## 9. 可选组件

### 9.1 qpdf（仅 MinerU Cloud 大 PDF 拆分需要）

自建 MinerU 不需要 qpdf。只有使用 MinerU Cloud 且需要自动拆分大 PDF 时，才从 qpdf 官方 release 下载 Windows ZIP，解压后把 `bin` 目录加入用户 PATH，重启 LanRAG Lite，然后验证：

~~~powershell
qpdf --version
~~~

### 9.2 DuckDB 扩展

桌面版默认 `DUCKDB_SKIP_EXTENSION_LOAD=1`，冷启动不依赖 DuckDB 扩展服务器。确实需要 Excel 或 spatial 能力，并且能访问扩展仓库时，在 `.env.lite` 中改为 `DUCKDB_SKIP_EXTENSION_LOAD=0`，重启应用后单独验证相应表格操作；下载失败时保持默认值。

## 10. 参考

- [Windows 桌面版构建与运维](Windows-Lite-Desktop.md)
- [AgentRAG 启动与运维指南](AgentRAG-启动与运维指南.md)
- [docreader README](../../docreader/README.md)
- [文档解析服务文档](../../website-docs/03-features/03-document-parsing.md)
- [Windows Lite 配置示例](../../deploy/windows-lite.env.example)

---

本文按 2026-11-21 的桌面安装版和 MinerU 4.0.1、docreader v0.8.0 实测环境编写。更换镜像、MinerU 版本或模型配置后，应重新执行第 7 节验收。