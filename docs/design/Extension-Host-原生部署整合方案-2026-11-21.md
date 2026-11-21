# Extension Host 原生部署整合方案

- 文档日期：2026-11-21
- 状态：设计评审稿；本轮只新增方案文档，不修改源码
- 适用基线：WeKnora v0.9.0（BASELINE.txt: upstream_tag=v0.9.0, upstream_commit=f17ee8a）；当前 HEAD d0ce45d
- 架构依据：docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md
- 关联现状：extensions/README.md、extensions/host/config.go、extensions/host/runtime.go、docs/operations/REL-04-发布与回滚.md

## 一、结论摘要

1. **当前 Extension Host 不是可直接随原生形态交付的产品构件。** 它按开发机“两个进程 + Lite SQLite + 固定回环端口”的方式实现：Core 到 Host 的所有 Bridge 地址硬编码为 http://127.0.0.1:8090，Host 到 Core 的 weknora.Client 也只允许回环明文或 HTTPS。这个边界本身没有错，但它要求 Host 必须与 Core 共享网络命名空间，不能作为普通独立 Service 部署到另一个 Compose 容器或另一个 Kubernetes Pod。
2. **Docker Compose / Helm 是最大缺口。** 原生标准部署默认 DB_DRIVER=postgres，而 Host 的文档工作台、清理队列、附件清理和归档 MCP 读取都依赖对原生 SQLite 的只读 ATTACH 或原生 SQL。Postgres 部署下这些服务不会被创建或直接失效；这不是配置项能解决的，需要新增 Native Catalog 抽象和 Core 内部只读 API。
3. **推荐产品形态：同 Pod/同网络命名空间的 sidecar + 独立持久化卷。** Compose 用 network_mode: "service:app" 的扩展服务共享 Core 回环；Helm 在 app Deployment 内增加 extension-host sidecar；Lite/Desktop 用随包分发的受管 Host 辅助进程。无论哪种形态，Host 都只监听 127.0.0.1:8090，不发布端口、不建 Service、不进 Ingress。
4. **必须限制为单实例。** Extension Host 的扩展状态是单文件 SQLite，单写者模型；在共享存储方案完成前，app.replicaCount 必须为 1，Compose 也不得对 Host 或 app 水平扩展。多副本会导致每个 Pod 各自持有不同扩展库，治理状态分裂。
5. **分两段落地。** P0：Native Catalog 抽象 + Core 内部只读 API + Host HTTP 适配 + 同 Pod/同回环部署 + 持久卷；P1：Lite/Desktop 打包、自动 bootstrap、备份恢复命令和全量验收。P0 不完成，Compose/Helm 只能算 Core 能启动，不能宣称 AgentRAG 功能完整。

## 二、原生部署形态与现状差异

### 2.1 原生四种形态

| 形态 | 原生 Core | 原生数据库 | 原生进程/编排 | 当前 Host 接入情况 |
|---|---|---|---|---|
| Docker Compose 标准部署 | wechatopenai/weknora-app 容器，8080 | 默认 Postgres（compose 内 postgres） | frontend、app、postgres、redis、docreader | 没有 Host 服务，没有扩展库卷 |
| Kubernetes Helm | 单 app Deployment，8080 | 默认 Postgres | chart 的 app、frontend、docreader、postgres、redis、neo4j | app 只有一个 container，没有 sidecar/extraContainers 挂点 |
| Lite 单二进制 | WeKnora-lite，SQLite + 内存队列 | 本地 SQLite | 单二进制/systemd | Host 是开发脚本另起的第二个二进制，未随 Lite 包分发 |
| Desktop 应用 | Wails + 内嵌 Lite | 本地 SQLite | Wails 进程 + 内嵌 HTTP server | Desktop 不启动、不管理 Host |

### 2.2 当前实现的关键约束（审查证据）

| 约束 | 代码/事实 | 影响 |
|---|---|---|
| Bridge 固定回环 | internal/extensionbridge/client.go:43、management.go:67、attachments.go:128、attachment_delete.go:143、source_run.go:172 均为 http://127.0.0.1:8090；Host 只接受 127.0.0.1 listener（extensions/host/runtime.go:70-73） | Host 必须与 Core 共享网络命名空间；独立 Pod/Service 不可达 |
| Host 上游客户端限制 | extensions/weknora/client.go 的 NewClient 只接受 HTTPS 或 localhost/回环明文；JSON 只允许 /api/v1/；无重定向 | 同回环是唯一低改动方案；跨 Pod 必须改协议/TLS/证书，超出本方案 |
| 原生 SQLite 门控 | extensions/host/config.go:107 只在 DB_DRIVER 为空或 sqlite 时设置 NativeDBPath=DB_PATH；runtime.go:253-263 只有 NativeDBPath != "" 才创建 attachmentcleanup、cleanup、cleanupNotifier、retention | Compose/Helm 默认 Postgres 时上述服务全部为 nil |
| Management 原生联查 | extensions/management/list.go:14 的 nativeOverviewFrom 直接 FROM weknora.knowledges；server.go:41-45 只有 WithNativeSQLite | Postgres 部署下 overview 退化为扩展库生命周期行，既不是原生全量列表，也不能准确计数 |
| 清理队列原生读取 | extensions/cleanup/service.go:211-228 用 ViewNative(... weknora.knowledges ...)；cleanTask 在 service.go:646 再核对删除时间 | Postgres 无法发现原生已删除文档，30 天清理队列不能工作 |
| 附件清理原生读取 | extensions/attachmentcleanup/service.go:142、296-300、320-339 用 ViewNative + readNativeFile；reclaim.go:88-100 同样依赖原生 SQLite | Postgres 附件删除联动不可用 |
| 归档 MCP 原生联查 | extensions/mcp/server.go:30 明确 NativeDBPath 是 Lite 数据库；extensions/mcp/archive.go:26-61 同时 JOIN ext_lifecycle_current、weknora.knowledges、weknora.chunks | Postgres 无法复用该 SQL；归档检索在 Compose/Helm 缺失 |
| 原生删除附件仅 SQLite | internal/extensionbridge/attachment_delete.go:76、93 和 internal/handler/attachment_reclaim.go:27 以 DB_DRIVER=sqlite 作为前置条件 | 即使 Host 适配 Postgres，Core 侧附件删除/回收钩子仍被关闭 |
| Host 配置要求 | extensions/host/config.go:206-260：worker 必须有文件系统扩展 DB、合法 native origin/key/tenant、1..1000 个唯一 KB；probe 拒绝所有 worker 设置 | 无 KB 的全新部署不能直接进入 worker；需要 bootstrap 或 probe 到 worker 提升 |
| 部署资产缺失 | docker-compose.yml 无 extension-host；helm/templates/app.yaml 单容器；scripts/package-lite.sh 只构建 WeKnora-lite；deploy/weknora-lite.service 只启动 Core；cmd/desktop/main.go 无 Host 子进程 | 四种形态都需要新增打包/编排，不是改一个配置项 |
| 管理密钥未进入原生部署 | Core 的 AGENTRAG_MANAGEMENT_KEY 用于附件、通知、provenance；Host 也需同值。Compose/Helm 的 app env 与 Secret 均未包含 | 不配置时清理/复核通知和附件链路会 403/不可用 |

### 2.3 已具备、无需重做的部分

- Host 是独立 Go module（github.com/ybochuan/LanRAG-Next/extensions），不 import WeKnora internal/，满足边界要求。
- Bridge 已有协议版本 X-AgentRAG-Bridge-Version: 1、固定 tenant/KB scope、fail-closed 语义。
- Host 已具备固定回环绑定、单写者、启动时占用端口、SIGTERM 排空、扩展 SQLite 迁移和 health/readyz。
- 扩展库 schema 41 已有清理队列、证据保留等能力；本次部署整合不改变业务数据模型。

## 三、设计目标与硬约束

### 3.1 目标

1. 同一版本的 Core、Host、扩展 schema 作为一个 release 一起交付；用户只执行原生部署命令，不手工开两个终端。
2. Docker Compose、Helm、Lite、Desktop 四种形态均能启动 Host，并具备可用的文档工作台、生命周期、清理、附件和 MCP 能力。
3. 所有新增行为可观测、可回滚、可备份；不改变原生检索、解析、任务调度算法。
4. Host 永远只监听回环；不新增公网/集群 Service，不把管理密钥或 API key 下发给浏览器。

### 3.2 硬约束

- Frozen Architecture 不重写：不改变 Bridge 边界、五条不变量、Lineage/版本族事实源。部署整合属于 Implementation Difference，需在 M0 Change Map/本文件记录。
- Core 受控区最小改动：只允许薄 DTO 只读接口和既有 Hook 的授权/适配，不重构原生业务。
- 扩展独立 module：根 go.mod/go.sum 不因本方案引入依赖；不添加 go.work。
- 单写者：Extension Host 的扩展 SQLite 同一时刻只能有一个写者；同一 Core 实例只配一个 Host。
- 回环安全：Host 不监听 0.0.0.0、不暴露 8090 端口；管理 key 至少 32 字节；缺少密钥时 fail-closed。

## 四、目标架构

### 4.1 逻辑拓扑

    同一个 Pod / 同一个容器网络命名空间
    ┌─────────────────────────────────────────────────────────────┐
    │  WeKnora Core :8080  <── 127.0.0.1:8090 ──>  Extension Host  │
    │        │                                      :8090 loopback │
    │        │  <── /api/v1/internal/agentrag/* ──  NativeCatalog  │
    │        │                                      HTTP adapter    │
    │        ▼                                                       ▼
    │   Postgres/Redis/DocReader                 /data/agentrag/agentrag_ext.db
    └─────────────────────────────────────────────────────────────┘

- Core 到 Host：既有 Bridge/Management/Provenance 仍走 127.0.0.1:8090。
- Host 到 Core：业务 API 走 127.0.0.1:8080；新增的 Native Catalog 只读 API 也走同一回环。
- 扩展库：独立 SQLite 文件，落在 Host 专用持久卷；与原生 DB 物理分离。
- Native Catalog 的作用：把 Host 对原生文档/分块的读取从“SQLite 文件 ATTACH”替换为可替换接口。Lite/Desktop 用 SQLite 实现；Compose/Helm 的 Postgres 用 HTTP 实现。

### 4.2 部署拓扑原则

1. 同网络命名空间是硬要求：Compose 的 Host 通过 network_mode: "service:app" 加入 app 的网络；Helm 的 Host 是 app Pod 的第二个 container；Lite/Desktop 的 Host 与 Core 同机同回环。
2. 一个 Core 一个 Host：不为 Host 建 Service；Kubernetes 中 Host 不需要 ServiceAccount、Service、Ingress。
3. 不同生命周期但同 Pod 管理：Helm 的 Pod 重建/滚动更新时 Host 与 Core 一起重建；Compose 升级时使用 --force-recreate app extension-host 保持网络命名空间同步。
4. 数据面分离：Host 只挂载 /data/agentrag，不写原生文件；如需要 parser artifacts/correction，放在同一 Host 卷的子目录。
5. 管理面分离：Host 管理 key、Core 管理 key、原生 API key 三者分别配置/轮换；不共用同一个密钥。

## 五、核心设计：Native Catalog 抽象

### 5.1 目标接口

在 extensions/nativecatalog（新包）定义只读接口，避免 management、cleanup、attachmentcleanup、mcp 各自写原生 SQL：

    type Document struct {
        TenantID        uint64
        KnowledgeBaseID string
        ID              string
        Title           string
        FileName        string
        FileHash        string
        ParseStatus     string
        Metadata        []byte
        CreatedAt       time.Time
        UpdatedAt       time.Time
        DeletedAt       *time.Time
    }

    type DocumentQuery struct {
        TenantID        uint64
        KnowledgeBaseID string
        Keyword         string
        IncludeDeleted  bool
        DeletedSince    *time.Time
        Page            int
        PageSize        int
    }

    type DocumentPage struct {
        Documents []Document
        Total     int
    }

    type ArchiveChunk struct {
        DocumentID string
        ChunkID    string
        Title      string
        State      string
        Excerpt    string
    }

    type Catalog interface {
        ListDocuments(ctx context.Context, q DocumentQuery) (DocumentPage, error)
        GetDocument(ctx context.Context, tenantID uint64, kbID, documentID string) (Document, error)
        ListArchiveChunks(ctx context.Context, tenantID uint64, kbID string, documentIDs []string, query string) ([]ArchiveChunk, error)
        Health(ctx context.Context) error
    }

实现：

- SQLiteCatalog：封装现有 store.ViewNative 的 ATTACH DATABASE ... AS weknora，保持 Lite/Desktop 行为；查询 weknora.knowledges/weknora.chunks，支持 deleted_at 过滤。
- HTTPCatalog：调用 Core 内部只读 API，适用于 Compose/Helm 的 Postgres 以及其他非 SQLite 数据库。接口只传标量和 DTO，不传 SQL，不暴露原生表结构。

选择规则：cfg.NativeDBPath != "" 且 DB_DRIVER 为空/sqlite 时用 SQLiteCatalog；否则用 HTTPCatalog。Postgres 部署显式设置 DB_DRIVER=postgres，避免 Host 误把某个 DB_PATH 当原生库。

### 5.2 Core 内部只读 API（需要用户另行批准）

这些接口属于 internal/ 受控区，必须按“薄只读 DTO”实现；本节只是方案，不是实施授权。

| 方法 | 路径 | 用途 | 关键约束 |
|---|---|---|---|
| GET | /api/v1/internal/agentrag/native/documents | 文档工作台列表/计数、清理队列扫描 | 必须支持 include_deleted=1、deleted_since、分页、关键词；只返回元数据 |
| GET | /api/v1/internal/agentrag/native/documents/:id | 附件清理核对、单文档详情 | 校验 tenant/KB；返回 file_hash、parse_status、metadata、deleted_at |
| GET | /api/v1/internal/agentrag/native/documents/:id/archive-chunks | MCP 归档检索 | 只读文本 chunk；限制条数/大小；不能返回被禁用分块 |

鉴权与安全：

- 复用 AGENTRAG_MANAGEMENT_KEY：请求头 X-AgentRAG-Management-Key，长度至少 32 字节，常量时间比较；同时要求 X-AgentRAG-Bridge-Version: 1。
- 只接受回环来源：c.Request.RemoteAddr 必须是 127.0.0.1/IPv6 loopback；拒绝集群 Service 来源。Compose 的 network_mode: service:app 和 Kubernetes 同 Pod 都能满足。
- 必须校验 tenant_id、kb_id 与配置 scope；不得跨 tenant 读；不返回文档正文（archive chunk 除外）。
- 错误只返回固定 code，不返回 SQL、表名、路径或凭据。
- 端点需加入原生 auth allowlist；不要用 JWT，避免浏览器令牌参与 Host 内部调用。

数据查询必须显式使用 Unscoped() 或等价方式包含软删除行；如果只复用现有 Find，GORM 的 deleted_at IS NULL 会让清理队列永远看不到已删除文档。实现时要补：
- ListDocuments(include_deleted=true) 返回 deleted_at；
- GetDocument 对已删除文档也返回；
- 列表排序和分页必须稳定，避免工作台分页重复/漏项。

### 5.3 各服务改造点

| 服务 | 现状 | 改造 |
|---|---|---|
| extensions/management | nativeOverviewFrom 直接联查 weknora.knowledges；WithNativeSQLite 注入路径 | Server 增加 catalog nativecatalog.Catalog；overview/counts 通过 catalog 取文档集合，再在扩展库联查 lifecycle；标题搜索走 catalog 的 keyword；无 catalog 时明确 degraded_reason=native_catalog_unavailable，不能把扩展行伪装成原生全量 |
| extensions/cleanup | nativeRows、cleanTask 用 ViewNative | 改为 catalog.ListDocuments(include_deleted=true) 与 catalog.GetDocument；清理只删扩展表，原生记录/文件不动 |
| extensions/attachmentcleanup | readNativeFile 读 weknora.knowledges；reclaim 调原生 API | 改为 catalog.GetDocument 校验 tenant/KB/file_hash/deleted_at/metadata；原生删除/回收仍走现有 weknora.Client.ReclaimDeletedAttachment |
| extensions/mcp/archive.go | 混合 SQL JOIN 原生 chunks | 用 catalog.ListArchiveChunks；lifecycle 状态仍由扩展库判断，避免把原生 SQL 搬进 HTTP 接口 |
| Core internal/extensionbridge | 附件删除钩子以 DB_DRIVER=sqlite 为前提 | 先确认原生附件删除服务在 Postgres 可用，再移除门控；若不可用，保持 fail-closed，并在工作台显示“该部署不支持附件自动清理”，不能静默关闭 |

### 5.4 备选方案与取舍

| 方案 | 结论 | 原因 |
|---|---|---|
| Host 直接连 Postgres 读原生表 | 不推荐 | 需要把数据库账号交给 Host，耦合原生 schema/migration；与独立扩展、公开 API 边界冲突；查询方言/软删除语义容易漂移 |
| Host 只走现有公开 API | 不足以覆盖 | 现有知识列表/批量 API 默认过滤软删除，没有 include_deleted，清理队列和附件清理无法看到已删除文档 |
| 在 Core 内嵌 Host（同进程） | 不采纳 | 破坏独立 module/构建边界；Wails/Compose 也无法复用同一 Host 进程模型 |
| 独立 Host Service + 改 Core 地址 | 不采纳（本期） | 需要改 Frozen Bridge 回环边界和 TLS/证书，影响大；同网络命名空间可零协议改动满足 |
| Host 直接读 Postgres + 只读账号 | 不推荐 | 维护成本低于改 Core，但 schema 耦合、权限面过大、备份/升级不可控 |

决策：采用 Native Catalog 接口 + SQLite/HTTP 双实现；Core 只新增回环只读 DTO，不把原生表结构暴露给扩展。

## 六、Docker Compose 方案

### 6.1 交付物

1. docker/Dockerfile.extension-host：构建 Host 二进制（独立 extensions/go.mod，CGO_ENABLED=0 可构建，使用 modernc.org/sqlite 纯 Go 驱动）。
2. deploy/agentrag/docker-compose.agentrag.yml：不改动原生 docker-compose.yml 的基础服务，作为 overlay 使用。
3. deploy/agentrag/.env.agentrag.example：Host/Core 共享配置示例。
4. 可选：scripts/agentrag-bootstrap（P1）生成 API key/tenant/KB 配置，不在本阶段阻塞。

### 6.2 服务拓扑

基础 compose 保持原样，overlay 增加：

    services:
      app:
        healthcheck:
          test:
            - CMD-SHELL
            - >-
              curl -fsS http://localhost:8080/health &&
              curl -fsS http://localhost:8090/readyz
          interval: 30s
          timeout: 10s
          retries: 3
          start_period: 60s
        environment:
          AGENTRAG_MANAGEMENT_KEY: "${AGENTRAG_MANAGEMENT_KEY:?set AGENTRAG_MANAGEMENT_KEY}"
          AGENTRAG_TENANT_ID: "${AGENTRAG_TENANT_ID:?set AGENTRAG_TENANT_ID}"
          AGENTRAG_KB_IDS: "${AGENTRAG_KB_IDS:?set AGENTRAG_KB_IDS}"
          AGENTRAG_BEFORE_GENERATE_KEY: ""
          AGENTRAG_CORRECTION_SOURCES_KEY: ""
          AGENTRAG_PARSER_PROXY_ADDR: "http://127.0.0.1:8090"

      extension-host:
        image: wechatopenai/weknora-extension-host:latest
        build:
          context: .
          dockerfile: docker/Dockerfile.extension-host
        container_name: WeKnora-extension-host
        network_mode: "service:app"
        depends_on:
          app:
            condition: service_started
        env_file:
          - .env
        environment:
          AGENTRAG_MODE: worker
          AGENTRAG_EXT_DB: /data/agentrag/agentrag_ext.db
          AGENTRAG_WEKNORA_URL: http://127.0.0.1:8080
          AGENTRAG_WEKNORA_API_KEY: "${AGENTRAG_WEKNORA_API_KEY:?set AGENTRAG_WEKNORA_API_KEY}"
          AGENTRAG_TENANT_ID: "${AGENTRAG_TENANT_ID:?set AGENTRAG_TENANT_ID}"
          AGENTRAG_KB_IDS: "${AGENTRAG_KB_IDS:?set AGENTRAG_KB_IDS}"
          AGENTRAG_MANAGEMENT_KEY: "${AGENTRAG_MANAGEMENT_KEY:?set AGENTRAG_MANAGEMENT_KEY}"
          DB_DRIVER: postgres
        volumes:
          - agentrag-data:/data/agentrag
        read_only: true
        tmpfs:
          - /tmp
        security_opt:
          - no-new-privileges:true
        restart: unless-stopped
        healthcheck:
          test: ["CMD", "curl", "-fsS", "http://127.0.0.1:8090/readyz"]
          interval: 15s
          timeout: 3s
          retries: 3
          start_period: 30s

    volumes:
      agentrag-data:

### 6.3 启动顺序

1. app 容器启动（Core 可能暂时不健康）。
2. extension-host 以 service_started 依赖启动，接入 app 的网络命名空间；它先绑定 8090，再打开扩展 DB/启动 worker。
3. Host /readyz 通过后，app 的 healthcheck 才通过；frontend 的 depends_on app: service_healthy 因此不会把流量提前送到 Core。
4. 若 Host 未就绪，app 容器自身仍在运行，Core 的 before_retrieve fail-closed；这比“前端已可用但治理静默失效”安全。

注意：不要同时让 app 的 healthcheck 依赖 Host、Host 又依赖 app 的 service_healthy，否则 Compose 启动会互相等待。这里用 service_started + app healthcheck 串行化。

### 6.4 数据卷与权限

- agentrag-data 只挂载到 Host，不挂载到 app；避免 Core 进程/容器误写扩展库。
- 建议初始化卷权限：Compose 使用一个一次性 agentrag-volume-init（busybox，仅创建目录并 chown 到 Host 运行用户），或使用绑定目录并预先设置宿主权限。若 Host 镜像使用非 root 用户，不要以 root 启动主进程。
- 扩展库路径固定为 /data/agentrag/agentrag_ext.db；correction/parser artifacts 放 /data/agentrag/corrections、/data/agentrag/parser_artifacts，均在同一个卷内，便于备份。

### 6.5 升级/重启注意事项

- network_mode: service:app 依赖 app 容器的网络命名空间。升级时执行：
    docker compose -f docker-compose.yml -f deploy/agentrag/docker-compose.agentrag.yml up -d --force-recreate app extension-host
  只重建 app 不重建 Host 会导致 Host 留在旧命名空间，表现为 Host 无法访问 Core 或 Core 无法访问 Host。需要把这条写进运维文档和 release checklist。
- 若客户不接受共享命名空间，唯一替代是修改 Core Bridge 为可配置地址 + TLS/证书，属于新的架构评审，不在本方案 P0 范围。
- 单机 Compose 不建议水平扩展 app；Host 与 app 必须一一对应。

## 七、Kubernetes Helm 方案

### 7.1 chart 变更

新增 values：

    extensionHost:
      enabled: true
      image:
        repository: wechatopenai/weknora-extension-host
        tag: ""
        pullPolicy: IfNotPresent
      mode: worker
      extDBPath: /data/agentrag/agentrag_ext.db
      tenantId: ""
      knowledgeBaseIds: []
      existingSecret: ""
      persistence:
        enabled: true
        size: 1Gi
        storageClass: ""
      resources:
        requests:
          cpu: 50m
          memory: 128Mi
        limits:
          cpu: "500m"
          memory: 512Mi
      securityContext:
        runAsUser: 10001
        runAsGroup: 10001
        runAsNonRoot: true
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true

- app.env 条件追加：AGENTRAG_MANAGEMENT_KEY、AGENTRAG_TENANT_ID、AGENTRAG_KB_IDS、AGENTRAG_BEFORE_GENERATE_KEY、AGENTRAG_CORRECTION_SOURCES_KEY、AGENTRAG_PARSER_PROXY_ADDR=http://127.0.0.1:8090（按需）。
- helm/templates/app.yaml 在 containers: 下追加 extension-host sidecar；不要新建独立 Deployment（独立 Pod 无法共享回环）。
- helm/templates/extension-host-pvc.yaml 创建 agentrag-data PVC；或复用 dataFiles 的 PVC 模式但独立 claim，避免 Core 与 Host 共享写权限。
- helm/templates/secrets.yaml 增加 AgentRAG Secret 键；若 secrets.existingSecret 由客户提供，chart 只引用、不生成。

### 7.2 sidecar 关键片段

    - name: extension-host
      image: "wechatopenai/weknora-extension-host:latest"
      imagePullPolicy: IfNotPresent
      env:
        - name: AGENTRAG_MODE
          value: worker
        - name: AGENTRAG_EXT_DB
          value: /data/agentrag/agentrag_ext.db
        - name: AGENTRAG_WEKNORA_URL
          value: "http://127.0.0.1:8080"
        - name: AGENTRAG_TENANT_ID
          value: "TENANT_ID"
        - name: AGENTRAG_KB_IDS
          value: "KB_ID"
        - name: AGENTRAG_MANAGEMENT_KEY
          valueFrom:
            secretKeyRef:
              name: agentrag-secret
              key: AGENTRAG_MANAGEMENT_KEY
        - name: AGENTRAG_WEKNORA_API_KEY
          valueFrom:
            secretKeyRef:
              name: agentrag-secret
              key: AGENTRAG_WEKNORA_API_KEY
      ports:
        - name: agentrag
          containerPort: 8090
      volumeMounts:
        - name: agentrag-data
          mountPath: /data/agentrag
      readinessProbe:
        httpGet: { path: /readyz, port: agentrag }
        initialDelaySeconds: 10
        periodSeconds: 10
        timeoutSeconds: 3
        failureThreshold: 3
      livenessProbe:
        httpGet: { path: /healthz, port: agentrag }
        initialDelaySeconds: 30
        periodSeconds: 20
        timeoutSeconds: 3
        failureThreshold: 3

### 7.3 副本、策略与探针

- extensionHost.enabled=true 时强制 app.replicaCount=1；在模板中 fail 比部署后状态分裂安全。
- app.strategy.type 应为 Recreate（或至少确保同一 PVC 不被两个 Pod 同时挂载）。扩展 SQLite 是单写者，RollingUpdate 会在旧 Pod 未退出时启动新 Pod，两个 Host 同时写同一卷；必须避免。
- Host 的 readiness 不依赖 Core（Host 启动时 Core 可能尚未就绪；且 Lite 启动顺序是 Host 先起）。Pod Ready 由 app 的 /health 与 Host 的 /readyz 共同决定；Kubernetes 会等所有容器 readiness 通过才将 Pod 标记 Ready，因此不需要修改 Core 健康检查。
- /healthz 只表示进程/监听存活；/readyz 表示扩展 DB、迁移、worker 与首次扫描本地就绪。若要在 UI/告警中展示 Core 可用性，使用 Host 状态接口的 upstream 字段，不要让 /readyz 依赖 Core，否则 Host-first 启动会死锁。
- Host 容器使用 readOnlyRootFilesystem: true，仅 /data/agentrag 可写；/tmp 用 emptyDir。Host 不挂载原生 data-files，不接触原生文件。

### 7.4 Helm 安装示例

    helm upgrade --install weknora ./helm -f values.yaml -f deploy/agentrag/values-agentrag.yaml

验收前先执行 helm lint ./helm、helm template ...，并检查：
- app 容器 env 有 AGENTRAG_MANAGEMENT_KEY/TENANT_ID/KB_IDS；
- Host sidecar 有 AGENTRAG_EXT_DB 和持久卷；
- Host 没有 Service/Ingress；
- app.replicaCount > 1 时模板直接失败；
- Secret 不渲染到 ConfigMap。

## 八、Lite 单二进制/单机包方案

### 8.1 产品定位

Lite 的“单应用、零外部依赖”仍然保留：不依赖 Postgres/Redis/消息队列。Extension Host 作为同包的本地辅助进程运行，不需要另装服务；这不是开发机的“两个终端手工拆分”，而是发行包内的受管进程。

### 8.2 包结构

    weknora-lite/
      WeKnora-lite
      extension-host
      web/
      config/
      migrations/sqlite/
      .env.lite.example
      .env.agentrag.example
      deploy/
        weknora-lite.service
        weknora-extension-host.service
      docs/LITE.md

- scripts/package-lite.sh 增加 Host 构建：
    (cd extensions && CGO_ENABLED=0 GOWORK=off go build -mod=readonly -trimpath -o "$STAGE_DIR/extension-host" ./cmd/extension-host)
- Host 二进制不需要 CGO；不要把它和需要 CGO 的 Core 构建混在一起。
- 不改变 Core 的 Lite 构建 tag（sqlite_fts5）和启动逻辑。

### 8.3 systemd 编排

新增 deploy/weknora-extension-host.service：

    [Unit]
    Description=AgentRAG Extension Host for WeKnora Lite
    After=network.target
    Before=weknora-lite.service

    [Service]
    Type=simple
    User=weknora
    Group=weknora
    WorkingDirectory=/opt/weknora
    EnvironmentFile=/opt/weknora/.env.agentrag
    ExecStart=/opt/weknora/extension-host
    Restart=on-failure
    RestartSec=5
    NoNewPrivileges=true
    ProtectSystem=strict
    ProtectHome=true
    ReadWritePaths=/opt/weknora/data

    [Install]
    WantedBy=multi-user.target

- 在 deploy/weknora-lite.service 增加：

    [Unit]
    Wants=weknora-extension-host.service
    After=weknora-extension-host.service

  这样 Core 在 Host 本地就绪后启动；Host 的 /readyz 不依赖 Core，不会死锁。
- .env.agentrag 由 agentrag-bootstrap（P1）或发布安装脚本生成，至少包含 AGENTRAG_MODE=worker、AGENTRAG_EXT_DB=/opt/weknora/data/agentrag_ext.db、AGENTRAG_WEKNORA_URL=http://127.0.0.1:8080、AGENTRAG_TENANT_ID、AGENTRAG_KB_IDS、AGENTRAG_MANAGEMENT_KEY、AGENTRAG_WEKNORA_API_KEY。
- .env.lite 只负责原生 Core；.env.agentrag 只负责 Host。不要把 Host 的 API key/管理 key 写进 .env.lite 或提交仓库。
- 若用户还没有知识库：Host 以 AGENTRAG_MODE=probe 启动，Core 可正常使用；用户在 UI 创建首个知识库后，由 agentrag-bootstrap 生成 worker 配置并重启 Host。不要让 Host 在无 KB 时以 worker 启动后 crash-loop。

### 8.4 Lite 的启动/初始化流程

1. 安装包解压，复制 .env.lite.example 为 .env.lite，复制 .env.agentrag.example 为 .env.agentrag。
2. 首次启动 Core（或由安装脚本启动），用户创建/登录并获得知识库。
3. 运行一次 agentrag-bootstrap（P1）：
   - 读取原生 SQLite 的 tenants、knowledge_bases（只读）；
   - 校验 tenant/KB 存在；
   - 生成 AGENTRAG_MANAGEMENT_KEY（若未配置）、AGENTRAG_EXT_DB、AGENTRAG_TENANT_ID、AGENTRAG_KB_IDS；
   - 通过原生登录/租户 API 创建受限 API key（retrieve+ingest+manage_kbs，仅绑定目标 KB），写入 .env.agentrag（0600），不写日志；
   - 重启 weknora-extension-host.service 和 Core。
4. 后续启动 systemd 自动按 Host → Core 顺序拉起。

P1 的 agentrag-bootstrap 应做成 Go 命令，不依赖 Python；避免把开发机 launcher_config.py 作为产品运行时依赖。若首版不做 bootstrap，发布文档必须明确“先创建知识库并手工配置 API key”，不能把 Host 直接设为 worker。

## 九、Desktop 应用方案

### 9.1 进程模型

Desktop 的 Wails 进程是唯一 UI/主进程；它在启动内嵌 Lite HTTP server 后，启动 extension-host 子进程。Host 与 Core 同机，回环满足 Bridge 和 weknora.Client 的限制。

### 9.2 启动顺序

1. cmd/desktop/main.go 继续现有流程：设置 Edition=lite、解析应用支持目录、加载 .env、设置 DB_PATH、构建 container。
2. 在创建 HTTP listener 并得到实际端口 app.backendURL 后（cmd/desktop/main.go:282-293），启动 Host 子进程：
   - 路径：app bundle Contents/Resources/bin/extension-host（macOS 签名时一并放入 Resources）；开发模式可从 extensions/cmd/extension-host 构建到临时目录。
   - 环境：
       AGENTRAG_MODE=worker
       AGENTRAG_EXT_DB=<AppSupport>/AgentRAG/agentrag_ext.db
       AGENTRAG_WEKNORA_URL=<app.backendURL>
       AGENTRAG_WEKNORA_API_KEY=<本地受限 API key>
       AGENTRAG_TENANT_ID=<默认 tenant>
       AGENTRAG_KB_IDS=<默认 KB>
       AGENTRAG_MANAGEMENT_KEY=<本次启动生成的 32+ 字节随机值>
       DB_PATH=<desktop DB_PATH>
       DB_DRIVER=sqlite
3. Desktop 使用 /api/v1/auth/auto-setup + X-WeKnora-Desktop-Token 获得本地登录态，再调用 POST /api/v1/tenants/<id>/api-keys 创建 Host 专用受限 key；key 只保存在进程内存或 0600 应用支持文件，不显示在 UI。
4. Host 子进程 stdout/stderr 重定向到 ~/Library/Logs/WeKnora/extension-host.log，日志轮转由 Desktop 负责。
5. 退出时：Wails OnShutdown 先通知 Host 优雅退出（SIGTERM/Interrupt），等待 10 秒；超时后终止进程组。不要只 Process.Kill，否则可能中断扩展 SQLite 事务。

### 9.3 首启无知识库

- Desktop 首次启动没有 KB 时，Host 以 AGENTRAG_MODE=probe 启动；此时 Core 仍可完成用户初始化和创建首个 KB。
- Desktop 的 Go 层增加一个轻量 scope watcher：每 2–5 秒查询原生 KB 列表（JWT 或本地 service），发现首个 KB 后：
  - 生成/更新 AGENTRAG_TENANT_ID、AGENTRAG_KB_IDS；
  - 重启 Host 为 worker（或调用 Host 的受控 reload，若后续实现）。
- 没有该 watcher 时，用户创建 KB 后必须重启 Desktop 才能启用治理，产品体验不可接受；这是 Desktop 的 P1 门禁。

### 9.4 数据与备份

- 扩展库默认：~/Library/Application Support/WeKnora/AgentRAG/agentrag_ext.db。
- 与原生 data/weknora.db 分开，不把扩展库放进 app bundle/Resources（签名后不可写）。
- Desktop 提供导出备份操作（P1）：停止 Host → 使用 SQLite backup API 复制 ext DB → 恢复 Host；UI 只显示文件路径，不自动上传。
- 卸载/升级时保留扩展数据目录；版本升级前提示备份。

## 十、配置与密钥契约

### 10.1 变量矩阵

| 变量 | Core | Host | 说明 |
|---|---|---|---|
| AGENTRAG_MODE | 不需要 | 必须 worker；首启可 probe | Host 业务模式 |
| AGENTRAG_EXT_DB | 不需要 | 必须 | 扩展 SQLite 路径；不得等于原生 DB_PATH |
| AGENTRAG_WEKNORA_URL | 不需要 | 必须 | 同回环 http://127.0.0.1:8080 |
| AGENTRAG_WEKNORA_API_KEY | 不需要 | 必须 | 受限 API key；Secret |
| AGENTRAG_TENANT_ID | 必须（附件/回收钩子） | 必须 | 同一 tenant |
| AGENTRAG_KB_IDS | 必须（附件/回收钩子） | 必须 | 逗号分隔，1..1000 个；Host 校验 |
| AGENTRAG_MANAGEMENT_KEY | 必须 | 必须 | 同一值，>=32 字节；Secret |
| AGENTRAG_BEFORE_GENERATE_KEY | 可选 | 可选 | 启用生成前 Hook 时两端一致 |
| AGENTRAG_CORRECTION_SOURCES_KEY | 可选 | 可选 | 来源修正 Hook |
| AGENTRAG_PARSER_PROXY_ADDR | 可选 | 不需要 | Core 指向 http://127.0.0.1:8090 时启用 Host parser |
| AGENTRAG_PARSER_PROXY_KEY | 可选 | 可选 | 解析代理认证 |
| DB_PATH | Lite/Desktop 原生库 | SQLite 模式可继承；Postgres 不设置 | Host 仅用于原生 SQLite 读取 |
| DB_DRIVER | 原生 | Postgres 部署显式 postgres | 防止 Host 误取 DB_PATH |
| AGENTRAG_CLEANUP_RETENTION | 不需要 | 可选，默认 30d | 清理保留期 |
| AGENTRAG_EVIDENCE_RETENTION | 不需要 | 可选，默认 30d | 证据保留期 |

### 10.2 密钥生成与轮换

- 生成：openssl rand -base64 48（至少 32 字节；不要用短密码）。
- AGENTRAG_MANAGEMENT_KEY 必须同一个值注入 Core 和 Host；只放 Secret/环境变量，不进入 ConfigMap、镜像、前端 bundle。
- AGENTRAG_WEKNORA_API_KEY 只授予目标 KB 的 retrieve、ingest、manage_kbs，不要 full_access；不要把浏览器 token 复用给 Host。
- 轮换顺序：先停 Host → 更新 Secret → 同时重启 Core 与 Host；管理 key 不支持热轮换，必须让两端同时切换。API key 轮换后旧 key 立即失效，Host 重启失败时回滚 Secret。
- Host Config.String()/GoString() 已脱敏；新增日志字段不得打印 env 值、文件路径、文档标题或正文。

## 十一、数据、迁移与回滚

### 11.1 数据边界

| 数据 | 位置 | 归属 | 备份 |
|---|---|---|---|
| 原生知识库/文档/分块 | Postgres（Compose/Helm）或 weknora.db（Lite/Desktop） | 原生 Core | 原生备份流程 |
| 扩展库（生命周期、清理、附件、血缘等） | /data/agentrag/agentrag_ext.db 或 Desktop AppSupport | Host | Host 专用 SQLite backup |
| correction/parser artifacts | Host 卷子目录 | Host | 与扩展库一起备份或单独归档 |
| 管理 key/API key | Kubernetes Secret / Compose env / Desktop 0600 | 运维/安装器 | Secret 管理系统；不入库、不写日志 |

Host 不删除原生文档或原生文件；清理只删扩展表中的生命周期/投影记录。原生附件删除仍由 Core/原生存储执行。

### 11.2 扩展 schema 迁移

- Host 启动时执行 extensions/store/migrations（嵌入式迁移）；升级前必须备份扩展库。
- 单写者：升级必须停止旧 Host 并确认进程退出，不能两个版本同时打开同一个扩展库。
- 迁移采用前向迁移；旧 Host 不应打开新 schema。回滚需要恢复升级前扩展库快照，而不是让旧二进制“试着打开”。
- Kubernetes 使用 Recreate 策略；Compose 使用 docker compose stop extension-host 后再升级。
- 发布包应记录：Core 版本（VERSION/build metadata）、Host 版本、扩展 schema 最大值、兼容的最低扩展 schema。

### 11.3 备份/恢复

建议新增 extension-host backup --db <path> --out <path> 子命令（使用现有 modernc.org/sqlite 的 backup API，不依赖容器内 sqlite3）：

1. 停止 Host（确保没有写事务）。
2. extension-host backup --db /data/agentrag/agentrag_ext.db --out /backups/agentrag_ext-YYYYMMDD-HHMMSS.db。
3. 校验 PRAGMA integrity_check 和 PRAGMA foreign_key_check（Host 已有测试/迁移工具）。
4. 备份文件连同 release 版本清单保存；恢复时停止 Host，把备份放回原路径，启动同版本 Host。
5. 原生 DB 按其自身流程备份；不能用扩展库备份代替原生库。

### 11.4 回滚顺序

1. 停止 Core 与 Host（先 Host，再 Core）。
2. 恢复原生 DB（如升级涉及原生迁移）与扩展库备份。
3. 部署升级前的 Core/Host/前端镜像或二进制。
4. 先启动 Host，检查 /readyz；再启动 Core，检查 /health。
5. 验证文档工作台、生命周期、清理队列、附件下载/删除和 MCP。
6. 若 Host schema 已升级且没有备份，不允许直接回退旧 Host；必须保持新版本并修复，避免数据损坏。

## 十二、健康检查、可观测性与故障模式

### 12.1 探针语义

| 端点 | 含义 | 用途 |
|---|---|---|
| GET /healthz | Host 进程存活、监听正常；不检查 DB/上游 | liveness；失败重启容器 |
| GET /readyz | Host 本地扩展 DB 可打开、schema 迁移完成、worker/首次扫描本地就绪；JSON status=ready, scope=local | readiness；失败摘流量/不健康 |
| GET /health | 现有启动器使用的就绪别名；需保持 status=ready | systemd/Compose 兼容 |
| GET /status（建议新增） | 包含 scope=local、upstream=healthy/degraded、schema_version、version | 运维/告警，不参与 Pod readiness |

- 不要让 /readyz 依赖 Core：Lite 的启动顺序是 Host 先起、Core 后起；如果 Host readiness 等待 Core，会死锁。Core 的可用性由 Core 自己的 /health 和 Host /status 的 upstream 展示。
- Kubernetes Pod readiness 会汇总 app 与 Host 两个容器；app 的 /health 与 Host 的 /readyz 都通过才算 Ready。
- Compose 的 app healthcheck 同时检查 8080 和 8090，确保前端不早于 Host 接入流量。

### 12.2 日志与指标

- Host 输出固定事件码（host_workers_started、host_probe_started、host_stopped、cleanup_scan_failed 等），不记录文档内容、标题、路径、API key、management key。
- 容器日志统一 stdout/stderr；Compose 用 Docker logging driver，Kubernetes 用 stdout/日志采集；Desktop 写应用日志文件并轮转。
- 建议指标（后续）：Host 存活/就绪、扩展 SQLite 大小、迁移版本、Outbox 积压、cleanup 失败数、通知失败数、HTTP 5xx；不要为了指标新增管理面端口。
- 告警：Host 容器重启次数、/readyz 连续失败、Core /health 失败、扩展库大小超阈值、备份失败。

### 12.3 故障模式

| 故障 | 行为 | 恢复 |
|---|---|---|
| Host 启动失败 | Core 继续运行，before_retrieve 对候选返回非 200（fail-closed），不退回 whole-KB；app healthcheck 失败，前端不接流量 | 查 Host 日志/配置；修复后重启 Host |
| Host 运行中崩溃 | Core 的治理 Hook fail-closed；Pod/compose 重启 Host | 自动重启；检查扩展库完整性 |
| Core 不可用 | Host worker 对账/标题 API 失败；/readyz 仍可为 local ready，/status upstream=degraded | Core 恢复后 Host 重试；不要清空扩展库 |
| 扩展库损坏 | /readyz 失败；Host 退出/不健康 | 用备份恢复；不要用空库启动掩盖 |
| 管理 key 不一致 | 清理通知/附件/内部 API 403，Host 日志固定错误 code | 统一 Secret 后同时重启 |
| API key 过期/越权 | Host 对原生 API 403/401；worker 不 ready 或持续报错 | 重新签发目标 KB 受限 key |
| 扩展库写满 | Host readiness/迁移失败；清理/证据保留可能失败 | 扩容 PVC/清理备份；告警 |

## 十三、安全设计

1. 网络：Host 只监听 127.0.0.1:8090；Compose 不映射 8090；Kubernetes 不建 Service；Pod 内仅 app 与 Host 共享回环。建议 NetworkPolicy 限制 app Pod 只允许必要出站，Host 不需要入站 Service 规则。
2. 认证：Core 到 Host 使用 X-AgentRAG-Bridge-Version: 1 + Authorization: Bearer <AGENTRAG_MANAGEMENT_KEY>（现有实现）；Host 到 Core 内部只读 API 使用 X-AgentRAG-Management-Key，仅回环。
3. 授权：Host 的 native API key 仅限 tenant/KB 范围和最小 capabilities；不能使用 full-access key。内部 API 再次校验 tenant/KB，防止 Host 被错误配置后越权。
4. 容器：非 root、allowPrivilegeEscalation:false、readOnlyRootFilesystem:true、只挂载 Host 数据卷；禁止 hostPath、hostNetwork、docker.sock。
5. 数据：扩展库可能含治理事实/文档 ID/标题，按业务敏感数据保护；PVC/磁盘加密由平台提供；备份加密并限制读取。
6. 密钥生命周期：Secret 不入 ConfigMap/镜像层；不写进日志、崩溃栈、错误消息；轮换需要同时重启两端；旧密钥从 Secret 删除后确认无进程仍持有。

## 十四、实施阶段与文件清单

### 14.1 P0（没有这些不能发布）

| 编号 | 交付 | 主要文件 | 验收 |
|---|---|---|---|
| P0-1 | Native Catalog 接口与 SQLite 实现 | extensions/nativecatalog/*、extensions/management/{list,read,server}.go、extensions/cleanup/service.go、extensions/attachmentcleanup/*、extensions/mcp/archive.go | 扩展单测；SQLite 行为与现网一致 |
| P0-2 | Core 内部只读 API | 新增 internal/handler/agentrag_native.go、router、repository 薄查询、auth allowlist | 定向 handler/repo 测试；只读、tenant/KB 拒绝、软删除可见 |
| P0-3 | HTTPCatalog + 配置选择 | extensions/nativecatalog/http.go、extensions/host/config.go、runtime.go | Postgres 测试：overview/cleanup 不依赖 SQLite |
| P0-4 | Host 镜像与 Compose overlay | docker/Dockerfile.extension-host、deploy/agentrag/docker-compose.agentrag.yml、.env.agentrag.example | docker compose config；真实 Postgres 启动/健康/工作台 |
| P0-5 | Helm sidecar/PVC/Secret | helm/values.yaml、helm/templates/app.yaml、helm/templates/extension-host-pvc.yaml、helm/templates/secrets.yaml | helm lint/template；单副本校验；Pod 双容器 Ready |
| P0-6 | 附件删除的 Postgres 兼容/明确降级 | internal/extensionbridge/attachment_delete.go、internal/handler/attachment_reclaim.go、Host 服务 | 实际 Postgres 附件删除/回收测试，或明确显示“不支持”且不静默 |
| P0-7 | readiness/status 与启动顺序 | extensions/host/runtime.go、健康 handler、Compose/Helm 探针 | Host-first 不死锁；Host 停止时 Core fail-closed；前端不提前接流量 |

### 14.2 P1（产品化完善）

| 编号 | 交付 | 主要文件 | 验收 |
|---|---|---|---|
| P1-1 | Lite 打包/systemd | scripts/package-lite.sh、deploy/weknora-extension-host.service、deploy/weknora-lite.service、docs/LITE.md | 纯净 Linux 安装包安装→Host/Core 启动→文档工作台 |
| P1-2 | Desktop 子进程 | cmd/desktop/main.go、cmd/desktop/app.go、打包脚本 | 启动/退出/升级/日志/无 KB 首启 |
| P1-3 | agentrag-bootstrap | extensions/cmd/agentrag-bootstrap | 生成 tenant/KB/key，0600，不泄密 |
| P1-4 | backup/restore 命令 | extensions/cmd/extension-host 子命令或独立工具 | integrity_check、恢复演练 |
| P1-5 | 验收脚本/文档 | docs/operations/AgentRAG-原生部署指南.md、测试脚本 | 四种形态一键验收 |

本文件不包含实现授权。特别是 P0-2/P0-6 涉及 internal/ 受控区，开工前必须按 AGENTS.md 第 2、6 条获得用户明确授权并补 M0 Change Map。

## 十五、验收矩阵

| 场景 | Compose（Postgres） | Helm（Postgres） | Lite（SQLite） | Desktop（SQLite） |
|---|---|---|---|---|
| Core /health 200 | 必须 | 必须 | 必须 | 必须 |
| Host /readyz 200 | 必须 | 必须 | 必须 | 必须 |
| 8080/8090 端口暴露 | 仅 8080 发布；8090 不发布 | 仅 app Service 8080；Host 无 Service | 仅本机回环 | 仅本机回环 |
| 文档列表 total 与原生一致 | 必须 | 必须 | 必须 | 必须 |
| 待清理队列看到原生已删文档 | 必须 | 必须 | 必须 | 必须 |
| 附件删除/回收 | 必须（若无原生能力则显式降级） | 同左 | 必须 | 必须 |
| 归档 MCP 读取 | 必须 | 必须 | 必须 | 必须 |
| Host 停止时问答 fail-closed | 必须（无 whole-KB 回退） | 必须 | 必须 | 必须 |
| Host/Core 重启后状态恢复 | 必须 | 必须 | 必须 | 必须 |
| 扩展库持久化/备份恢复 | 必须 | 必须 | 必须 | 必须 |
| 单副本约束 | 必须 | 必须（模板 fail） | 不适用 | 不适用 |
| 升级/回滚演练 | 必须 | 必须 | 必须 | 必须 |

验收方法：
- Compose：docker compose ... up -d 后检查 curl 127.0.0.1:8080/health、docker compose exec extension-host curl 127.0.0.1:8090/readyz；用原生 API 创建/删除文档，核对工作台与清理队列。
- Helm：kubectl wait --for=condition=Ready pod -l ...；检查 Pod 两个容器 Ready；执行工作台/清理/附件/MCP 全链路。
- Lite：systemctl status weknora-extension-host weknora-lite；检查 /readyz；执行 SQLite 文档创建/删除。
- Desktop：启动 App，验证 Host 子进程 PID、日志、退出清理；无 KB 首启后创建 KB 并确认自动切 worker（P1）。

## 十六、风险与未决问题

1. Core 内部只读 API 的批准范围：P0-2 需要触达 internal/。若用户不批准新增内部路由，则 Compose/Helm 的 Postgres 全功能无法实现，只能发布“Core 可用、AgentRAG 部分能力降级”的受限形态；必须在发布说明中明确，不得伪装完整。
2. 附件删除的 Postgres 兼容性：当前 Core 门控是人为限制；要先验证原生 ReclaimDeletedAttachment 在 Postgres 的锁/文件语义。若不能安全支持，P0 的附件自动清理应在 UI 明确标记“不支持”，并保留人工处理入口。
3. Bootstrap：原生没有稳定的匿名管理员 CLI 入口（/auth/auto-setup 只在 Lite + Desktop token 下可用）。Compose/Helm 的 tenant/KB/API key 需要预置或使用 agentrag-bootstrap（P1）；首版发布文档要把“预创建 tenant/KB/API key”作为前置条件。
4. 单副本：扩展 SQLite 是单写者；多副本/多租户共享 Host 需要外部化扩展存储或每租户独立 Host。未完成前，Helm 模板应拒绝 replicaCount>1，Compose 的 app 不扩缩容。
5. Compose 网络命名空间：network_mode: service:app 在 app 重建时需要同步重建 Host；这是 Compose 的运维限制，需写入升级脚本。不能只重建 app。
6. Desktop 签名/打包：Host 辅助二进制必须随 app bundle 签名；否则 macOS Gatekeeper 可能阻止执行。Wails 打包脚本需要增加 helper 的签名和 notarization 流程。
7. Host 自身 readiness 与 Core 解耦：保留 scope=local 语义，避免 Lite 启动死锁；但运维必须同时观察 Core /health 与 Host /status，不能只看 Pod Ready。
8. 版本兼容矩阵：Core、Host、扩展 schema 三者必须绑定 release；旧 Host 打开新 schema 不支持。发布注释需记录 schema 版本和回滚备份路径。
9. 备份一致性：扩展 SQLite 与原生 Postgres 无法跨库事务一致；备份应停写/分别快照，并记录时间点。恢复时先恢复原生库还是扩展库需按业务影响评估，不能声称原子恢复。

## 十七、建议决策（供评审）

- D1 采用同 Pod/同回环的 sidecar，而不是独立 Host Service。理由：现有 Core Bridge 和 Host 客户端都硬编码回环，改地址/TLS 会触碰 Frozen 边界；sidecar 可零协议改动满足安全边界。
- D2 采用 Native Catalog 抽象 + Core 内部只读 API，而不是直接连 Postgres。理由：避免扩展直接耦合原生 schema/账号；软删除语义由 Core 统一提供；Lite/Postgres 共用 Host 业务代码。
- D3 扩展库独立卷，不与应用数据卷合并。理由：单写者、备份/回滚、权限和容量独立；避免 Core 容器误写。
- D4 P0 只支持单副本。理由：当前扩展存储是单文件 SQLite；多副本状态分裂风险大于收益。共享扩展存储另立设计。
- D5 Lite/Desktop 采用同包辅助进程，不把 Host 代码链接进 Core。理由：保持独立 module/构建边界；通过 systemd/Wails 生命周期管理实现“用户只启动一个产品”。

## 附录 A：Dockerfile.extension-host 参考

    # syntax=docker/dockerfile:1
    FROM golang:1.26-bookworm AS build
    WORKDIR /src
    COPY extensions/go.mod extensions/go.sum ./extensions/
    RUN cd extensions && go mod download
    COPY extensions ./extensions
    RUN cd extensions && CGO_ENABLED=0 GOWORK=off go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/extension-host ./cmd/extension-host

    FROM debian:12-slim
    RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/* && useradd --system --uid 10001 --create-home --home-dir /home/agentrag agentrag
    USER 10001:10001
    WORKDIR /home/agentrag
    COPY --from=build /out/extension-host /usr/local/bin/extension-host
    ENTRYPOINT ["/usr/local/bin/extension-host"]

镜像不包含源码、密钥、原生数据库或 root 权限；curl 仅用于容器健康检查。若使用 distroless，需要给 Host 增加内建 healthcheck 子命令，避免依赖 curl。

## 附录 B：Compose 环境变量示例

    # 生成：openssl rand -base64 48
    AGENTRAG_MANAGEMENT_KEY=
    AGENTRAG_TENANT_ID=
    AGENTRAG_KB_IDS=
    AGENTRAG_WEKNORA_API_KEY=
    AGENTRAG_BEFORE_GENERATE_KEY=
    AGENTRAG_CORRECTION_SOURCES_KEY=
    AGENTRAG_PARSER_PROXY_ADDR=http://127.0.0.1:8090

## 附录 C：验收记录模板

| 项目 | 命令/证据 | 结果 |
|---|---|---|
| Host 镜像构建 | docker build -f docker/Dockerfile.extension-host . | 待执行 |
| Compose 配置 | docker compose -f docker-compose.yml -f deploy/agentrag/docker-compose.agentrag.yml config | 待执行 |
| Compose 健康 | Core /health、Host /readyz | 待执行 |
| Helm lint/template | helm lint ./helm；helm template ... | 待执行 |
| Postgres 工作台 | overview total 与原生文档数一致 | 待执行 |
| 清理队列 | 原生删除文档后出现待清理任务 | 待执行 |
| 附件清理 | 原生删除父件后附件回收 | 待执行 |
| 归档 MCP | 归档文档 chunk 可检索 | 待执行 |
| 升级/回滚 | 扩展库备份→升级→恢复 | 待执行 |

## 附录 D：本方案不改变的范围

- 不改变原生检索、解析、Agent、任务调度算法。
- 不改变 Frozen Architecture 的四类 Hook、Lineage/版本族事实源。
- 不改变现有 extension schema 41 的清理与证据保留语义。
- 不修改当前工作区未提交的 corrections/feedback 改动；本文件本身是新增设计文档。
