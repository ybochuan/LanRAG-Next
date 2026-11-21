# LanRAG Lite Windows 构建与运维

Windows x64 安装包包含桌面应用、Extension Host、本地模型进程管理器、llama-server、网页、SQLite 和中文分词运行资源。Windows 的 WebView2 Runtime 提供界面。docreader、MinerU 和模型权重由用户准备和配置。

## 构建

构建机需要 Windows x64、Go 1.26（已验证为 Go 1.26.0）、Node.js 和 npm、带 GCC 和 G++ 的 Windows MinGW 工具链，以及 NSIS。将工具目录加入 PowerShell 的 PATH，确认 `go version`、`npm.cmd --version`、`gcc --version`、`g++ --version` 和 `makensis /VERSION` 能正常执行。运行端只需 Windows 和 WebView2 Runtime。Lite 使用本地 SQLite，不需要 PostgreSQL、Redis、Docker、WSL 或构建工具。

1. 在仓库根目录确认 `git rev-parse HEAD` 和 `git status --short --untracked-files=all`。用实际包含 Desktop、Host 和前端改动的源码构建。基准提交相同的候选也可能包含不同的工作区改动。
2. 前端执行 `npm ci --no-audit --no-fund`、`npm test` 和 `npm run type-check`。Windows 测试若误调用 WSL 的 bash，把 Git Bash 的 `bin` 目录提前到本次测试的 PATH。扩展在 `extensions/` 单独执行 `python scripts/check_module_boundary.py` 和 `go test -mod=readonly ./... -count=1`。
3. 回到仓库根目录运行以下命令，输出路径须是新目录。脚本重新构建生产前端，使用原生 Go 编译 Desktop、启动器、Extension Host 和模型进程管理器，放入 SQLite 迁移、分词字典、内置 llama-server 及许可，再调用 NSIS。

```powershell
.\scripts\build-windows-lite.ps1 -OutputDirectory .\tmp\release-windows
```

已有同一提交的 Linux 候选时，可以用 `-WebRelease <候选目录>` 复用它校验过的网页。脚本核对提交以及当前前端相对该提交的差异。Core 和 Host 仍在 Windows 重新构建。可以用 `-PortableOnly` 先生成程序目录，再用 NSIS 从该目录生成安装器。构建输出的 `resources\REVISION` 和 `resources\SOURCE_STATUS` 记录源码基线及状态。

4. 确认构建退出码为 0，交付输出目录里的 `LanRAG-Lite-windows-amd64-setup.exe`。检查程序目录包含 `LanRAG Lite.exe`、`LanRAG Desktop.exe`、`extension-host.exe`、`model-supervisor.exe`，并检查 `resources/web`、`resources/migrations/sqlite` 和 `resources/llama-runtime` 中的文件。交付前核对源码状态、程序目录和安装器属于同一候选。

构建需要获取 Go/npm 依赖和官方 llama.cpp 资产及许可证。下载 ZIP 失败时可用 `-LlamaRuntimeZip <官方 ZIP 路径>` 复用同版本文件，许可仍由脚本获取。只改前端时也要重新构建生产网页并放入该候选的 `resources/web`，随后重新生成安装器。

## 安装和首次运行

1. 运行 `LanRAG-Lite-windows-amd64-setup.exe`，从开始菜单打开 LanRAG Lite。已配置的 Windows 当前用户无需为应用安装额外数据库、WSL 或构建工具。
2. 桌面使用原生 Lite 本地账号初始化。先创建空知识库。Desktop 从该账号可访问的本空间知识库生成明确的 ID 列表，签发限域机器凭据并启用随包 Host。
3. 启动后增加或移除纳管库时，退出并重开应用使 Host 采用当前本地库范围。侧栏「模型进程管理」直接打开本项目的本地进程管理页面。
4. 外部解析是按需能力。将安装目录 `resources\.env.lite.example` 复制到用户数据目录并命名为 `.env.lite`，填写对应端点，退出再打开应用。随后用代表性文档确认解析功能。
5. 安装版默认跳过启动时下载 DuckDB 的 Excel 和 spatial 扩展。确实需要这些能力且可访问扩展仓库时，在同一配置文件设置 `DUCKDB_SKIP_EXTENSION_LOAD=0` 后重启，并单独验证相应表格操作。

## 本地模型进程管理

Windows 安装版默认将 `127.0.0.1` 加入 Core 地址白名单，保留部署方已有的白名单条目。该条目适用于本机回环地址上的各端口。模型实例启动和显卡探测在后台执行，模型输出可在实例的「日志」中查看。

打开 Windows 桌面应用即可扫描 GGUF、启停本机 `llama-server`、查看运行日志和探测可达性。Desktop 在打开窗口时启动管理器，与首库创建无关。窗口通过内部代理访问管理器，用户无需输入访问令牌。Windows 版使用本机回环探测，不需要 WSL。

在「新增模型实例」点击「选择 GGUF 文件」，从 Windows 文件选择器选取原位置的 GGUF。配置标识、任务和端口后点击「启动」，管理器才会运行随包的 llama-server。权重文件保留在原位置，实例配置记住其路径。旧版按模型文件名保存的实例仍可使用 `.env.lite` 的 `MODEL_SUPERVISOR_MODEL_DIR`。

随包运行时来自 llama.cpp b12431 Windows x64 Vulkan 构建，位于安装目录的 `resources/llama-runtime`。选择 GPU 时使用支持 Vulkan 的显卡驱动。构建机可通过 `-LlamaRuntimeZip` 复用同版本的官方 ZIP。配置保存在 `model-supervisor-state.json`，管理器运行日志见 `model-supervisor.log`。需要在问答或索引中使用进程时，将页面展示的模型地址配置到对应的模型槽位。 [llama.cpp b12431 官方发行页](https://github.com/ggml-org/llama.cpp/releases/tag/b12431)

## 内置 MCP

首个知识库就绪后，Desktop 自动在本地工作空间接入 AgentRAG MCP 并同步原生工具目录。用户无需注册 MCP 服务或填写单独凭据。智能体使用原生按需发现，工具调用沿用本次会话的知识库授权。

| 工具 | 能力 |
| --- | --- |
| `query_related_docs` | 查阅当前授权范围内的关联文件和附件。 |
| `search_archived_documents` | 用户明确要求查阅历史时，在授权范围内查找归档文档。 |

`core.log` 出现 `agentrag_mcp_ready` 后，在原生 MCP 的 AgentRAG 工具目录确认两个名称。重启后核对仍只有一条 AgentRAG 记录。出现 `agentrag_mcp_registration_failed` 时，查看同一日志中的失败阶段以及 Host 的 `/readyz`。用户在原生设置停用服务时，Desktop 保留该状态。

## 状态、日志和数据

| 路径或探针 | 含义 |
| --- | --- |
| `%LOCALAPPDATA%\LanRAG Lite` | 每个用户的持久数据目录。程序和版本资源位于安装目录。 |
| `weknora.db`、`agentrag_ext.db`、`files\` | 原生 SQLite、扩展 SQLite 和原件附件目录。 |
| `agentrag-desktop.json`、`.env.lite` | 私有治理配置以及用户配置的服务信息，按凭据文件管理。 |
| `core.log`、`extension-host.log` | Core 和 Host 日志，分享前检查并脱敏。 |
| `core.url` 对应地址的 `/health` | 当前 Core 监听地址及 HTTP 健康。 |
| `http://127.0.0.1:8090/readyz` | Host 本地 worker 就绪。首库建立并绑定后验证，同时检查工作台能读取该库。 |

关闭桌面窗口后，应用会关闭 Host 的父进程管道、等待排空，再关闭 Core。已有进程占用 8090 时，先从原进程的管理入口排查和处理。未配置解析或模型时，使用空库和工作台完成基础验收，再接入用户服务验证文档索引和问题答案。

## 验收及版本切换

```powershell
.\scripts\test-windows-lite.ps1
```

创建知识库后运行探针，验证 Core、Host、两库、限域凭据、未知文档拒绝，以及 Host MCP 的工具列表和匿名请求拒绝。原生 MCP 工具目录用于复核 Core 的自动接入。可传入 `-ProfileDirectory` 检查隔离验收目录。首次安装另观察 WebView 界面、退出后的端口释放和重启后的知识库状态。随后用已配置的解析和模型服务执行真实文件、问答、来源、附件及勘误验收。

升级前先退出并确认两个进程均结束。成套备份两库、原件与附件、启用的解析和勘误产物、私有配置及旧版安装包。安装新版后复核实际版本、两库和权限。需要回滚时，退出新版，恢复旧发行版与同一恢复点的数据。卸载程序保留用户数据目录。 [两库备份和回滚流程](REL-04-发布与回滚.md)

## 2026-11-21 Windows 候选验证

候选以 `ae49639` 为基线加入 Windows 安装集成，复用该提交的静态网页。Core、Host 和入口程序使用 Go 1.26.0 在 Windows 构建。NSIS 从已构建的 Windows 文件生成安装器。

- 安装器退出 0。独立用户目录内启动，Core 健康检查及网页入口返回 200。首次空库启动未建立 worker 数据库。
- 使用仅含合成账号和空库的隔离配置，通过原生登录创建第一份知识库。Core 探针为 `ok`，Host 以 `scope=local` 就绪，限域机器凭据只看到创建的库。治理工作台 API 返回 200，未知文档被拦截。
- 原生窗口标题为 `LanRAG Lite`。APP 重启后保留知识库及机器凭据。关闭窗口后，Desktop、Host 和启动入口正常退出，回环端口和运行地址记录清理完成。
- 用 SQLite backup API 分别备份两库，完整性和外键检查通过，扩展库为 schema 41。稳定安装目录中的程序可以使用恢复副本启动，原有知识库和限域机器凭据均可用。
- 扩展全量测试、vet、模块边界和 Desktop Host 管理定向测试通过。本候选的 Desktop 全套测试有四项 Windows 偏好路径失败，四项均在原始 `ae49639` 上定向复现。

复验时另用用户配置的解析和模型端点检查实际导入、向量索引、问答与附件。旧版成套数据回滚和完整界面目测单独记录。

## 内置 MCP 安装候选验收

本次 Windows x64 候选基于 `ae49639` 和当前工作区的 Desktop、MCP、前端改动，用 Go 1.26.0 重新构建，安装器退出码为 0。

- 隔离配置首次打开时没有知识库、Host 数据库或 MCP 记录。通过原生 API 创建空知识库后，Core 自动出现一条 AgentRAG 记录，原生目录同步两项工具，重启复用原服务 ID。
- 已验证原生工具目录和在线发现，授权空库可执行历史检索。缺失会话范围、关联工具收到未知文档 ID、匿名调用 Host、普通 MCP 使用其他回环路径均被拒绝。
- 已安装网页在 Chromium 中验证了空白文档工作台没有孤立的「未归属反馈」按钮。
- Core 日志记录 MCP 就绪，关闭窗口后清理运行地址并释放 Host 监听。前端类型检查、测试和生产构建通过。扩展模块门禁及 Go 目标测试通过，完整工具包和 handler 测试中的九项失败在改动前基线复现。

使用真实模型自主选择工具，以及真实文档的索引、归档命中和附件下载，仍需连接用户配置的服务验收。

## 发布前手工验收

| 操作 | 通过条件 |
| --- | --- |
| 在隔离用户数据目录打开安装版 | 桌面正常打开。点击侧栏「模型进程管理」直接显示自建启停页，无需输入令牌。 |
| 选择原位置的 GGUF，再点击「启动」 | 选文件阶段没有推理进程。点击后随包 llama-server 启动，全程无控制台弹窗，实例日志可读，探测成功。停止或关窗后端口释放。 |
| 在模型设置配置向量化、重排、对话 | 各槽位分别通过「测试连接」。模型名称取服务暴露的 ID，显示名称可以自定义。对话模型还需在「快速问答」配置里选中。 |
| 让快速问答缺少对话模型，再点「前往配置」 | 在当前窗口打开该智能体的编辑器并高亮「对话模型」。不弹出系统浏览器。 |
| 新建首个知识库，打开文档工作台和原生 MCP 目录 | Host 就绪且出现一条 AgentRAG 记录、两项工具。重启复用原记录。空工作台没有孤立的「未归属反馈」按钮。 |
| 配置外部解析和真实模型，上传样本文档并提问 | 核对上传、解析、入库、检索、答案和引用。分别记录外部服务与真实模型验收结果。 |

## 常见问题

| 现象 | 诊断和处理 |
| --- | --- |
| 点「前往配置」弹出浏览器 | 旧版的占位链接被桌面壳当成外链拦截。更新包含修正前端的安装包，再验证按钮会定位到当前智能体配置。 |
| 模型进程管理索要令牌，或者打开原生模型设置 | 检查实际打开的是否为 Windows 安装版，以及网页、模型管理器和桌面程序是否来自同一候选。安装版自建页面通过桌面代理认证。 |
| 找不到 llama-server、要求把模型搬到固定目录或启动时弹控制台 | 检查安装目录的 `resources/llama-runtime` 和包版本。当前版本内置运行时，页面用文件选择器打开原位置的 GGUF，模型和显卡探测在后台运行。 |
| 本机模型报 `SSRF validation failed` | 当前 Windows 包默认放行字面地址 `127.0.0.1`。核对 Base URL 和实际安装版本。该条目覆盖本机回环的各端口。局域网或容器服务只按需要放行实际目标。不要把全局关闭 SSRF 当作连接修复。 |
| 模型健康正常，但模型配置报模型名错误 | 请求本机服务的 `/v1/models`，把返回的 `data[].id` 填入「模型名称」。例如管理器实例标识为 `embedding` 时模型 ID 也可能是 `embedding`。 |
| `8082` 重排请求返回 `501` | 核对实例任务是否误选「向量化」。停止旧实例，从新增表单沿用相同的实例标识和端口，将任务选为「重排」，重新启动并测试。旧卡片的启动按钮会沿用保存的任务配置。 |
| 外部服务超时或拒绝连接 | 先查从 Windows Core 到目标的连通性、监听地址和端口，再查各功能的协议与凭据。docreader 的 gRPC 路径与 HTTP 解析入口分别验证。 [外部服务部署指引](Windows-Lite-外部服务部署.md) |
| 桌面上传后报空文件，网页上传正常 | 核对桌面前端是否使用注入的 Core API 地址直传 multipart 文件。验证实际上传字节和解析结果，避免把 WebView 转发问题误判成解析器故障。重打包含对应前端的候选。 |
