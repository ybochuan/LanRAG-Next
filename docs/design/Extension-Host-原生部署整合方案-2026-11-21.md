# LanRAG Lite 原生部署整合方案

- 修订日期：2026-11-21
- 状态：修订方案。本文记录当前实现和后续交付要求。
- 核对版本：LanRAG-Next `f2043a6`，上游 WeKnora `v0.9.0`（`f17ee8a`）。当前源码内嵌扩展迁移最高为 schema 41。
- 架构依据：[AgentRAG Frozen Architecture](<AgentRAG 技术方案 v1.0 — Frozen Architecture.md>)、[项目上下文](../../PROJECT_CONTEXT.md)及[仓库工作规则](../../AGENTS.md)。

## 1. 产品定位与交付边界

LanRAG Lite 是基于 WeKnora Lite 的单人、单机知识库。用户安装一个应用，通过一个入口启动。应用内由 Lite Core 和 Extension Host 共同运行，沿用原生 SQLite、本地文件存储和内存任务队列。

| 归属 | 内容 |
| --- | --- |
| LanRAG Lite 安装包 | Lite Core、Extension Host、前端、配置模板、SQLite 迁移资源、安装或启动入口、版本信息及使用说明。 |
| 用户自备并配置 | docreader、MinerU、模型提供方及其连接配置。模型权重、推理运行时和相关凭据由用户管理。生成、向量、重排按启用功能分别配置。 |
| 开发和评估工具 | `start.cmd`、WSL 开发环境、`model-supervisor`、标定及维护工具。开发工具按各自用途运行。 |

**应用本体应能在外部服务尚未配置时安装、启动、登录、建库和打开配置界面。** 一项功能需要解析或模型服务时，按该功能提示用户配置。文件内容是否发送到外部，由用户选择的解析和模型端点决定。

本期产品范围为 SQLite Lite。标准版 Compose、PostgreSQL 适配、Helm、多副本和 Native Catalog 从本期实施与验收清单移出。

## 2. 运行架构

```text
本机浏览器或 Desktop → Lite Core → 原生 SQLite 和原件目录
                            │
                 Bridge v1 │ 127.0.0.1:8090
                            ▼
                      Extension Host → 独立扩展 SQLite
                            │
                     原生授权 API 及原生库只读访问

Core 或 Host 按已启用能力连接用户配置的解析、生成、向量或重排服务
```

- Core 继续负责页面、登录、知识库、原生入库、检索和问答。Host 负责治理、生命周期、关系、反馈、扩展事务和后台任务。沿用已有 Core 薄 Hook、管理接口和独立 Go module 边界。
- Core 和 Host 运行在同一操作系统的网络环境。Host 绑定 `127.0.0.1:8090`，一个安装实例由一个 Host 写入扩展库。端口占用时，中止启动并显示冲突，现有监听进程由其原管理入口处理。
- 原生库 `weknora.db` 与扩展库 `agentrag_ext.db` 使用不同的持久路径。Host 在 SQLite 模式下使用与 Core 一致的 `DB_PATH` 只读访问原生库，原生资源更新沿用已授权的 Core API。程序目录、用户数据和日志各有稳定路径，安装升级时保留用户数据。WSL 开发环境沿用已经验证的 ext4 数据目录。
- `before_retrieve` 沿用原生授权范围和 500 ms 回环协议。Host 未就绪或协议校验未通过时，受管知识读取关闭。`before_generate` 或附件增强的局部服务失败按已有降级语义处理。产品就绪需在探针之外验证业务请求。
- 同一 Core/Host 发行组合保留 Bridge v1 及各 Hook 的凭据边界。新增 Core 受控区接口或算法调整按仓库规则单独评审。

[Host 入口与装配](../../extensions/host/runtime.go)、[SQLite 只读联查](../../extensions/store/native_view.go)、[Core 检索门卫](../../internal/extensionbridge/client.go)、[数据目录和备份说明](../operations/AgentRAG-启动与运维指南.md#数据库操作)。

## 3. 现有扩展、实现链路与效果

下表是当前代码中的能力。表内条件决定安装后何时可用。首版仅将通过对应验收的能力列入发布说明。

| 功能 | 实现链路 | 用户可见效果和条件 |
| --- | --- | --- |
| 文档工作台 | 前端 `/platform/documents` → Core 校验知识库权限 → Host 管理接口 → 原生 SQLite 台账与扩展治理属性 | 查看原生文档、有效期、待复核、规则、版本、关系、审计、附件和反馈。按标题与状态筛选。Host worker 需要与 Core 相同的原生库路径。 |
| 生命周期和复核 | `ingest` 接收事件与周期对账 → `lifecycle` 同事务保存事件、当前状态及 Outbox → 到期扫描 | 可配置生效、失效和复核日期，查看状态及提醒。对账补扫当前可观察事实，支持失效历史查阅。 |
| RAG 和 Agent 治理 | Core 确定原生授权范围 → `before_retrieve` → `retrieval` 逐项许可 → Core 过滤结果 | 受管知识检索及读取只消费当前授权且有效的文档。Host 中断时关闭受管路径。RAG 验收需要已配置的生成和向量能力。 |
| 来源与血缘 | `lineage` 记录文档观察版本和关系，`provenance` 核验受支持的模型输入与工具步骤 | 可查文档版本、来源及其关系图，追溯受支持的文本问答分块。文件产物跨轮复用和全输入来源封存仍按对应待办验收。 |
| 业务关系、补充依赖与条款 | `business` 维护关系与裁决，`lineage` 记录边，`dependencies` 重查父件状态 | 查看引用、实施、模板、修订、解读、补充依赖及已授权的跨库关系。支持按版本与分块审核条款例外与冲突。 |
| 文档族与受控发布 | `business` 管理归族、审批与受控替代 → 原生 API 创建或绑定修订件 → 索引与对账 → 提交发布事实 | 支持 Markdown 修订、绑定已上传原件、拆分发布或废止。替代后可查旧版本，常规问答按当前许可使用有效版本。 |
| 附件提取与读取 | `parser/attachment` 提取受支持的内嵌文件或 DOCX 附录 → 原生 API 入库 → 记录父子归属 → 下载时复验 | 支持范围内的附件可独立管理、匹配并显示下载卡片。主件失效或权限变化后，点击旧卡片仍需复验。依赖对应解析配置。 |
| 附件清理与证据保留 | `attachmentcleanup` 跟踪父件删除及重试，`cleanup` 复核软删除，`retention` 按规则裁剪扩展证据 | 可查看清理待办和重试状态，按现有入口执行保留或确认。证据保留按实际引用关系处理，扩展治理标签由 Outbox 投影。 |
| PDF 解析代理及转换件溯源 | Core 传入原生文档身份 → Host 预检和路由 → 用户提供的 MinerU → 持久登记任务、产物和原件版本 → Core 消费解析结果 | 配置对应上游后可处理支持的 PDF。远端结果未知时先保留待核对状态，转换件可关联其原件版本。 |
| 问答反馈与审核通知 | 原生工具栏提交 → 校验本人答复与单库来源 → 保存反馈 → Host 向 Core 递送待审通知 → 审核 | 用户可评价符合条件的答复、提交建议，审核员可查待办。当前通知使用进程内队列，当前版本的页面与铃铛一起验收。 |
| 审核指导和勘误 | `guidance`、`corrections` 保存审核结果 → 校验当前知识库、分块、版本及适用条件 → 生成前引用 | 匹配条件的答复可以采用已生效指导或勘误。可查勘误历史和 Markdown。已有隔离证据覆盖单库纯文本场景。 |
| 关联和历史 MCP | 原生按需注册 Host `/mcp` → 使用当前授权范围查询关系、附件或归档文本 | 注册后可查询有效关联资料。显式查阅历史时，SQLite 路径可返回授权范围内的关键词匹配，并提示历史内容失效。 |

[工作台、关系与版本族](../../PROJECT_CONTEXT.md#文档工作台)、[Host 服务](../../extensions/host/runtime.go)、[解析任务恢复](../../extensions/parser/recovery.go)、[反馈通知](../../extensions/host/feedback_notifier.go)、[MCP 工具](../../extensions/mcp/server.go)、[问答隔离验收](../evidence/qa-hardening-2026-11-21.md)。

`model-supervisor` 用于开发或自建模型实例的控制与探测。其余按需工具可从扩展模块单独构建，包括附件盘点与回填 `attachment-backfill`、指定任务重试 `outbox-retry`、复核提醒查询 `review-alerts`、解析标定 `parser-calibrate`、归族标定 `family-calibrate`、检索评估 `retrieval-eval` 和 Agent 评估 `agentic-eval`。回填应用和任务重排按运维流程单独执行。 [扩展命令目录](../../extensions/cmd/) · [本机开发服务](../operations/AgentRAG-启动与运维指南.md)

## 4. 安装形态与开发方式

| 形态 | 定位 | 实现步骤 |
| --- | --- | --- |
| Linux Lite Web | 优先用于构建和干净环境候选验证，也适用于 Linux 单机使用。 | 复用原生 Lite 的发行资源，将 Core、Host 和前端装入同包。使用同一启动入口编排首启与正常启动。采用 systemd 时，在启动次序之外使用有界就绪等待或进程就绪通知，确认两服务的启停与退出。 |
| Windows Lite Desktop | 面向 Windows 单人使用，作为桌面安装成品目标。 | 复用现有 Desktop 与 NSIS 入口。同一提交构建 GUI 和 `extension-host.exe`，由 Desktop 管理辅助进程。将 Core 实际监听端口、统一数据目录和受限配置传给 Host，验收首启、关闭后的进程回收与升级后读取。 |
| Windows + WSL | 继续作为当前开发和联调环境。 | 保留 `start.cmd`、`start-all.sh`、本地模型及解析调试。开发可以跨系统运行服务，正式包在目标系统内管理 Core 和 Host。 |

Linux 候选可先验真治理、安装资源和恢复流程。Windows 成品使用真实 Windows 安装流程获取独立验收证据，并验证在没有 WSL、开发工具和外部解析服务时的基础功能。 [原生 Lite 打包](../../scripts/package-lite.sh) · [现有 Windows 打包工作流](../../.github/workflows/release-lite.yml) · [Desktop 启动实现](../../cmd/desktop/main.go)

## 5. 构建流程

### 5.1 当前可执行的 Linux / WSL 候选构建

准备两个模块声明的 Go 1.26 工具链、C 编译器、Bash、Git、Node 和 npm。前端可采用当前前端 CI 的 Node 24。整包脚本从指定 Git 提交取源码。候选构建使用已核对的提交，输出目录须尚不存在。

```bash
cd /mnt/d/LanRAG-Next
revision="$(git rev-parse --short HEAD)"
release="$PWD/tmp/releases/$revision"
mkdir -p "$(dirname "$release")"

bash scripts/build-agentrag-release.sh "$release" HEAD
(cd "$release" && sha256sum -c SHA256SUMS)
```

1. 用 `git archive` 导出提交到临时目录。
2. 在 `frontend/` 执行 `npm ci`、`npm run build`，生成 `web/`。
3. 在根模块以 Lite 构建标识、`CGO_ENABLED=1`、`sqlite_fts5` 构建 `./cmd/server`，生成 `WeKnora-lite`。
4. 在 `extensions/` 以 `CGO_ENABLED=0`、`GOWORK=off` 构建 `./cmd/extension-host`，生成 `extension-host`。
5. 保存 `REVISION` 和 `SHA256SUMS`，校验同一批 Core、Host 和网页。

在已配置的开发工作树验证候选时，可以使用 `AGENTRAG_RELEASE_DIR="$release" bash start-all.sh`，或从 Windows 调用 `start.cmd -ReleaseDir /mnt/d/LanRAG-Next/tmp/releases/<提交短号>`。这是现有开发启动路径。 [候选构建](../../scripts/build-agentrag-release.sh) · [启动候选的现有流程](../operations/AgentRAG-启动与运维指南.md#初始化后发布) · [前端 CI](../../.github/workflows/frontend.yml)

### 5.2 正式安装包实施项

1. Linux 复用原生 Lite 打包中的 `config/`、`migrations/sqlite/`、配置示例和服务资源，补齐配套 Host、安装启动入口和配置说明。
2. Windows 在原生 Desktop 构建配置中加入同提交的 Host 产物，明确安装位置、用户数据位置、卸载与保留数据行为。把旧发布工作流中的 Go 版本声明与实际 `go.mod` 对齐，并按目标平台验证 CGO 构建。
3. 发布包记录同一版本的 Core、Host、前端、Bridge major、扩展 schema 和目标系统。发布时使用成套产物，客户机直接运行已构建程序。
4. 分开准备运行配置和开发样例。应用配置默认支持外部服务未绑定的状态，开发样例保留开发机的解析和模型启动方案。

## 6. 配置、首次启动和功能启用

### 6.1 配置契约

| 配置组 | 要求 |
| --- | --- |
| 原生 Lite | 使用 SQLite 检索与数据存储，原件和前端目录按发行形态确定。运行目录变动后仍能定位数据。 |
| Host worker | `AGENTRAG_MODE=worker`，独立 `AGENTRAG_EXT_DB`，与 Core 相同的原生 `DB_PATH`。从 Core 实际地址、选定租户和 KB 范围生成 `AGENTRAG_WEKNORA_URL`、`AGENTRAG_TENANT_ID`、`AGENTRAG_KB_IDS` 及受限机器凭据。 |
| 治理凭据 | Core 和 Host 对同一个 Hook 使用匹配凭据。启用生成前与勘误来源独立凭据时，管理、生成前和勘误来源三把凭据均按当前校验单独配置。凭据保存在用户私有配置。 |
| docreader | 安装初始状态为未配置。用户需要对应的文档解析能力时配置端点、传输和认证，再以实际文档验证。 |
| MinerU | 按需成套配置 Core 代理地址、Host 上游地址、代理凭据、scope 和产物目录。未配置时使用原生 Lite 的现有解析路径，入库仍需满足所选知识库的模型要求。 |
| 模型 | 用户在原生模型配置中填写服务地址、凭据和生成、向量、重排等所需能力。基于向量的入库及查询需校验向量模型与索引维度，重排按配置启用。 |
| MCP | 由用户选择启用并完成原生注册。工具调用继续遵循调用方当前授权范围。 |

产品化时核对现有 `.env.lite.example` 的 docreader 默认地址和开发启动器传入的解析代理变量。正式模板以未配置的外部能力为起点。需要额外服务的操作显示具体配置入口或可诊断错误。 [Lite 配置示例](../../.env.lite.example) · [Host 配置校验](../../extensions/host/config.go) · [现有开发配置生成](../../extensions/scripts/launcher_config.py)

### 6.2 启动流程

1. **空库首次启动。** 打开本机原生初始化界面，完成登录并创建或选择首个空知识库。文档索引和问答在后续按知识库要求配置模型。待纳管范围绑定前，受管知识问答按现有门禁拦截。
2. **确认纳管范围。** 使用原生授权接口核对租户和知识库，签发限域机器凭据并保存配置。复用当前启动器的行为，正式入口按目标系统承接该流程。
3. **进入正常工作模式。** 在 Linux Web 形态按现有方式先检查 Host 本地就绪，再启动 Core 并验证原生授权调用。在 Desktop 形态先取得内嵌 Core 的实际监听地址，再管理 Host，治理入口以联通性验证为准。两种形态都检查首轮对账。Core 故障恢复后，由 Host 重新连接并继续对账。
4. **启用外部能力。** 在界面完成用户服务配置后，用样本文档和真实能力请求验证。模型或解析服务不可达时，保留应用基础操作并显示对应故障。
5. **退出与恢复。** 通过产品入口停止挂接的进程，等待 Host 排空并关闭数据库。重启后核对已保存的治理事实及待处理任务。

`/bridge/healthz` 用于检查 Host 存活，`/readyz` 用于检查 worker 本地就绪。Core `/health` 说明原生 HTTP 可响应。启动验收还需经过授权读取与业务动作。 [现有首库与授权流程](../../start-all.sh) · [运维探针说明](../operations/AgentRAG-启动与运维指南.md)

## 7. 数据备份、升级和回滚

- 每次切换以同一恢复点保存原生 SQLite、扩展 SQLite、原件与附件、启用的解析或勘误产物、私有配置和上一发行版产物。对 SQLite 使用备份 API 或已有停写备份流程，并检查完整性和扩展外键。
- 切换前先停写、关闭相关服务并确认上一实例退出，再使用同版本 Core 与 Host 启动。Host 前向迁移后，复核实际 schema、工作台、文档读取和失败任务恢复。
- 回滚使用之前成套备份和对应的应用产物，按原版本恢复后验收原件、权限、治理历史与问答。旧版 Host 遇到更新的扩展 schema 会拒绝打开，按备份恢复流程执行。
- 开发、候选和用户实例分别使用各自的数据和配置。功能 E2E 使用临时库或经授权的数据副本。

[扩展迁移](../../extensions/store/migrate.go) · [REL-04 发布与回滚](../operations/REL-04-发布与回滚.md)。

## 8. 验收门禁与后续开发

| 阶段 | 验收项 |
| --- | --- |
| 源码与构建 | 扩展 module 边界、构建、测试、静态检查。针对授权 Core 改动运行定向检查。前端执行依赖安装、类型检查、测试及构建。核对同一提交产物和校验清单。 |
| 干净环境安装 | 无预配 docreader、MinerU 和模型服务时安装成功，可以登录、建库、查看配置及重启。完成首库绑定，验证台账授权、本地探针和业务就绪。 |
| 按能力联验 | 接入用户侧服务后，使用真实样例验证文档解析、向量入库、正常问答和引用。承诺的附件、PDF、Office、重排和 MCP 能力分别验收，按实际结果写入发布说明。 |
| 治理和故障 | 有效文档可用，失效文档在受管问答中排除。断 Host 时检查受管路径零答案生成。检查附件父件失效、版本发布、审核通知以及勘误启停。外部解析或模型故障时可定位到对应服务。 |
| 升级和回滚 | 使用上一支持版本的数据副本演练，包括两库、原件、相关产物和配置的成套恢复。复核资料、治理事实及应用版本。 |
| Windows 安装 | 用真实安装包验证可独立安装、Core 与 Host 启停、中文及带空格路径、旧数据读取和卸载的数据处理选项。 |

阶段顺序建议为 **Lite 候选包与干净 Linux 首装验证 → Windows 桌面安装与启停闭环 → 发布版功能及升级回滚验收**。日常开发保留 Windows + WSL，业务实现继续放在 `extensions/`，Core 接线沿用冻结边界。开发启用外部服务的测试与产品干净安装检查各用对应配置。

截至本方案核对版本，已有同提交候选构建入口、开发环境首库初始化流程及阶段性业务验证。11 月 21 日问答记录覆盖较早隔离候选的单库纯文本勘误及修订发布。当前提交的通知变更和最终安装包列入下一候选的联合验收。验收记录应注明源码提交、产物、系统、外部服务配置状况、运行命令和退出状态。 [扩展 CI](../../.github/workflows/extensions.yml) · [治理 E2E](../../extensions/scripts/governance_e2e.py) · [附件 E2E](../../extensions/scripts/attachment_e2e.py) · [现有证据](../evidence/qa-hardening-2026-11-21.md) · [发布待办](../../TASK_BACKLOG.md)
