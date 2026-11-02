# AgentRAG 模型进程管理器设计方案 (v1.1-Draft)

> **状态**：Draft，**取代 v1.0-Final**（v1.0-Final 存在三处结构性错误，按原文实现无法运行，见 §0）。
> **修订日期**：2026-11-01
> **实测基线**：`llama-server` `0.2.0-dev (build 10709, commit 9a9394a89)`，MSVC 19.44.35228.0，Windows AMD64。文中所有命令行参数均已在本机 `--help` 实测核对。
> **架构依据**：`docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`

---

## 0. 修订摘要（相对 v1.0-Final）

| # | v1.0-Final 的问题 | v1.1 处理 | 级别 |
|---|---|---|---|
| S1 | "前端页面作为 `extensions/ui/` 插件挂载"没有任何构建/加载机制；`.vue` 放在该目录既不会被 Vite 编译，也不会被 `web/` 服务 | §12 引入单一 registry bootstrap + Vite alias | 结构性 |
| S2 | API `/extensions/models/*` 未说明挂载点；挂 Host 会污染 fail-closed 故障域，挂 Core 会破 `internal/` 红线 | §2 拆为独立 Windows 进程 `model-supervisor` | 结构性 |
| S3 | 代码若随 Host 跑在 WSL，`cmd.Process.Pid` 是 Linux PID，`taskkill /PID <linuxpid>` 会打到无关 Windows 进程 | §2/§9 明确 Windows 原生二进制 + Job Object | 结构性 |
| F1 | `TaskType` 未映射到任何命令行参数，embedding/rerank 模型起来后端点不可用 | §5 给出实测 flag 映射 | 功能 |
| F2 | 仅用 `/health` 判定就绪，检测不到 pooling/维度错误 | §6 追加真实向量探针 | 功能 |
| F3 | `Port=0` 自动分配 + 手动粘贴地址，重启换端口后 Core 配置静默失效 | §6 默认固定端口 | 功能 |
| F4 | `BinaryPath`/`ModelPath` 直接来自前端 = 任意命令执行 | §10 目录白名单 + 路径逃逸校验 | 安全 |
| N1–N14 | 无鉴权、`ExtraArgs` 注入面、i18n 成本未计、并发无保护、日志无上限、丢时间戳、1.5GB 绝对值、180s 硬编码、无持久化、`nvidia-smi` 无降级、SIGTERM 在 Windows 不存在、DTO 混用、无验收章节、标题名不副实 | 分别落入 §4/§7/§8/§9/§10/§11/§15 | 工程 |

**保留 v1.0 的正确判断**（不要改掉）：§10.2 放弃跨模块 API 静默写入、§1 禁止 CGO、§8 日志去重、§5.1 显存仪表盘。

---

## 1. 边界声明（实施前必读）

本管理器**不参与任何治理决策与 fail-closed 判定**：

1. 不写入 `weknora.db`，不写入任何 `ext_*` 表。
2. 不产生 lifecycle / lineage / governance 事实；其运行状态不进入 `ext_lifecycle_current`、`ext_lineage_*`、`ext_governance_*`。
3. 它的故障、崩溃、重启**不得改变**上述 truth。
4. 模型以错误参数启动导致的检索质量下降属于**模型配置故障**，不属于治理层缺陷。
5. 本管理器与 Extension Host **不同进程、不同端口**，任何情况下不得合并。

---

## 2. 部署形态

### 2.1 拓扑

```
┌─ Windows ──────────────────────────────────────────────────┐
│  model-supervisor.exe   (Go, GOOS=windows, CGO_ENABLED=0)   │
│    ├─ 127.0.0.1:8091   HTTP API（Bearer + 版本头 + CORS 白名单）│
│    ├─ 进程生命周期: os/exec + Job Object                     │
│    ├─ 状态持久化: data/model-supervisor.json                 │
│    └─ 拉起 ↓                                                 │
│  llama-server.exe #1  embedding  127.0.0.1:8001             │
│  llama-server.exe #2  rerank     127.0.0.1:8002             │
└────────────────────────────────────────────────────────────┘
                              ↕  Windows→WSL 通 / WSL→Windows 不通
┌─ WSL ──────────────────────────────────────────────────────┐
│  Core :8080            Host :8090        ← 两者完全不动       │
│  （Core 通过 Windows 网卡地址访问 :8001/:8002）                │
└────────────────────────────────────────────────────────────┘
```

### 2.2 为什么必须是 Windows 原生二进制

| 理由 | 说明 |
|---|---|
| **PID 语义** | WSL 里 `exec` 一个 `.exe`，Linux 侧拿到的 PID 与 Windows PID 不同命名空间。`taskkill /PID <linuxpid>` 会命中无关进程。 |
| **杀树可靠性** | 通过 WSL interop 的 `Process.Kill()` 对孙进程不可靠，CUDA 上下文可能不退出，显存不释放。 |
| **显存读数准确性** | WSL 侧 `nvidia-smi` 看不到 Windows 侧进程占用，预检会失真。 |
| **Job Object** | 仅 Windows 原生进程能把自己与子进程绑到 Job Object，实现"管理器死则模型死"。 |

工程形态参照 `extensions/cmd/` 现有先例（`extension-host`、`parser-calibrate`、`review-alerts`），新增 `extensions/cmd/model-supervisor`。

### 2.3 约束

- 不得引入 CGO；`Job Object` 用 `golang.org/x/sys/windows`（纯 Go）。
- 不得在 `internal/` 增加任何路由、包或类型。
- 不得与 Extension Host 共享进程、端口或配置文件。

---

## 3. 目录与环境规范

```text
D:\LanRAG-Next\
├── extensions\
│   ├── cmd\model-supervisor\   # Go 入口（GOOS=windows）
│   ├── supervisor\             # 进程管理、预检、日志、持久化
│   └── ui\                     # Vue 组件（经 §12 registry 挂载）
└── data\
    ├── bin\                    # llama-server 运行时（已 gitignore）
    ├── models\                 # .gguf 模型（已 gitignore）
    └── model-supervisor.json   # 实例状态（已 gitignore）
```

**已落盘**：`data/.gitignore` 已加入 `models/`（`bin/` 原有）。禁止把运行时与模型提交进仓库。

### 3.1 运行时文件清单（实测必需）

`data/bin/` 需要且仅需以下 13 个文件，合计约 1.1 GB：

| 文件 | 大小 | 必需性 |
|---|---|---|
| `llama-server.exe` | 10 KB | 必需 |
| `llama-server-impl.dll` | 7.5 MB | 必需 |
| `llama-common.dll` | 9.5 MB | 必需 |
| `llama.dll` | 2.5 MB | 必需 |
| `ggml.dll` | 67 KB | 必需 |
| `ggml-base.dll` | 674 KB | 必需 |
| `ggml-cpu.dll` | 941 KB | 必需 |
| `ggml-cuda.dll` | 561 MB | GPU 必需 |
| `ggml-rpc.dll` | 124 KB | 必需 |
| `mtmd.dll` | — | **必需**（缺失时进程直接 `exit 127`，无任何错误输出） |
| `cublas64_12.dll` | 100 MB | GPU 必需 |
| `cublasLt64_12.dll` | 473 MB | GPU 必需 |
| `cudart64_12.dll` | 553 KB | GPU 必需 |

> **踩坑记录**：只复制 `llama-*`/`ggml-*`/`cuda*` 而漏掉 `mtmd.dll` 时，`llama-server.exe` 静默退出、退出码 127、stderr 为空。排查方式是逐文件对比源目录的 `*.dll` 集合。验收脚本必须包含"复制后能打印 version"这一步。

### 3.2 模型文件

| 模型 | 路径 | 用途 | 实测 |
|---|---|---|---|
| `Qwen3-Embedding-4B-Q6_K.gguf` | `data/models/` | embedding | 3.08 GB，**向量维度 2560** |
| `qwen3-reranker-0.6b-q8_0.gguf` | `data/models/` | rerank | 0.60 GB |

> **已实测（2026-11-01）**：
> - embedding：以 §5.2 模板启动，**7 秒**进入 `health=200`，`/v1/embeddings` 返回 **2560 维**向量，`/v1/models` 别名解析正常，停止后端口立即释放。未显式传 `--pooling`，使用 GGUF 元数据默认值。
> - rerank：以 §5.2 模板启动，**4 秒**进入 `health=200`，`/v1/rerank` 返回 `relevance_score`，相关文档 `0.1176` vs 无关文档 `4.2e-05`，判别正确。
>
> 两者合计证明 `--embedding` / `--rerank` 两个 flag 与 `data/bin/` 运行时完整可用，P0 的"参数映射"假设成立。

模型根目录可配置（`model_roots`），允许指向仓库外的绝对路径；但必须显式配置，不做隐式搜索。

---

## 4. 进程管理核心

### 4.1 状态机

```
stopped ──start──> starting ──ready──> running ──stop──> stopping ──> stopped
                       │                   │
                       └──fail──> error <──┘
                                    │
                                    └──start──> starting（允许重试）
```

约束：

- 同一实例在 `starting`/`stopping` 期间拒绝并发操作，返回 `409 Conflict`（N8）。
- `stopping` 必须有超时（默认 10s），超时升级为强杀。
- 任何状态转换写入 `data/model-supervisor.json`，写入失败不阻塞状态机但必须记日志。

### 4.2 数据结构（内部态与 DTO 分离，修正 N12）

```go
// 内部态：只在 supervisor 进程内存在，绝不序列化给前端
type instance struct {
    mu       sync.Mutex
    cmd      *exec.Cmd
    job      windows.Handle
    logRing  *ringBuffer
    cfg      InstanceConfig
    state    State
    // ...
}

// 传输态：显式 JSON 字段名，独立公开类型，不含 cmd/mu/handle
type InstanceView struct {
    ID              string    `json:"id"`
    DisplayName     string    `json:"display_name"`
    TaskType        string    `json:"task_type"`         // embedding | rerank
    Status          string    `json:"status"`            // stopped|starting|running|stopping|error
    PID             int       `json:"pid"`
    Port            int       `json:"port"`
    StartedAt       time.Time `json:"started_at"`        // UTC
    LoadingProgress int       `json:"loading_progress"`  // 0-100
    HealthLatencyMs int64     `json:"health_latency_ms"`
    LastError       string    `json:"last_error,omitempty"`
    ExitCode        int       `json:"exit_code,omitempty"`
    Endpoints       Endpoints `json:"endpoints"`         // §6.2
    EmbeddingDim    int       `json:"embedding_dim,omitempty"` // 探针实测，供用户填 Core
}

type Endpoints struct {
    Loopback string `json:"loopback"` // http://127.0.0.1:8001/v1
    Gateway  string `json:"gateway"`  // http://172.x.x.x:8001/v1（Windows 侧网关地址）
    State    string `json:"state"`    // verified | unverified | unreachable
}
```

时间统一 `time.Time` + UTC 传输（AGENTS §4.2）。

---

## 5. 命令行契约（修正 F1）

### 5.1 TaskType → flag 映射（本机实测）

| 参数 | 实测写法 | 说明 |
|---|---|---|
| embedding 模式 | `--embedding`（别名 `--embeddings`） | 仅支持 embedding 用例，必须与专用 embedding 模型搭配 |
| rerank 模式 | `--rerank`（别名 `--reranking`） | 启用 reranking 端点 |
| 池化 | `--pooling {none,mean,cls,last,rank}` | **默认不传**，用 GGUF 元数据；仅在日志提示缺失时显式指定并记录 |
| 归一化 | `--embd-normalize N` | 默认 2（L2）；改动必须记录理由 |
| GPU 层数 | `-ngl, --gpu-layers N` | 默认 1024（等于全部层）；`UseGPU=false` 时传 `-ngl 0` |
| 上下文 | `-c, --ctx-size N` | 默认 0 = 从模型读取 |
| 线程 | `-t, --threads N` | 默认 -1 |
| 监听 | `--host HOST` / `--port PORT` | 默认 `127.0.0.1` / `8080` |
| 鉴权 | `--api-key KEY` | 启用 Bearer 校验 |
| Web UI | `--no-webui`（别名 `--no-ui`） | 关闭内置 UI |
| 别名 | `-a, --alias STRING` | 供 `/v1/models` 返回可读名称 |
| 指标 | `--metrics` | Prometheus 端点，默认关闭 |

**硬性规则**：`TaskType` 必须落到实际参数上。embedding 实例不带 `--embedding`、rerank 实例不带 `--rerank` 时，服务能启动、`/health` 能过，但 Core 调用会 404 —— 这是 v1.0 最隐蔽的功能缺陷。

### 5.2 基线命令模板

```bash
# embedding
llama-server.exe --model data/models/Qwen3-Embedding-4B-Q6_K.gguf \
  --embedding --host 127.0.0.1 --port 8001 --no-webui \
  --alias qwen3-embedding-4b --api-key <per-instance-key> \
  -ngl 99 -c 4096 -t 8

# rerank
llama-server.exe --model data/models/qwen3-reranker-0.6b-q8_0.gguf \
  --rerank --host 127.0.0.1 --port 8002 --no-webui \
  --alias qwen3-reranker-0.6b --api-key <per-instance-key> \
  -ngl 99 -c 4096 -t 8
```

### 5.3 安全参数强制（修正 N2）

以下参数**禁止**由前端传入，`ExtraArgs` 命中即 `400`：

- `--host`（固定 `127.0.0.1`，永不 `0.0.0.0`）
- `--port`（由 supervisor 决定）
- `--model` / `-m`（由 `ModelPath` 决定）
- `--api-key`（由 supervisor 生成）
- 任何含换行、`%`、`&`、`|`、`>`、`<` 的参数值

`ExtraArgs` 采用白名单而非黑名单：只允许 `-ngl`、`-c`、`-t`、`-b`、`-ub`、`--pooling`、`--embd-normalize`、`--threads-batch` 及其值。

---

## 6. 端口与地址策略（修正 F3 + 跨网络）

### 6.1 固定端口，不用 `:0`

| 实例 | 端口 |
|---|---|
| embedding | `8001` |
| rerank | `8002` |

理由：地址要人工粘贴进 Core 的模型配置，端口漂移会让配置静默失效。`Port=0` 保留为高级选项，但 UI 必须显式警告"重启后地址会变化，需重新配置"。

### 6.2 三态地址（解决 WSL/Windows 跨网络）

supervisor 启动实例后，**分别从 Windows 侧和 WSL 侧各探测一次** `/v1/models`：

| 结果 | `state` | UI 表现 |
|---|---|---|
| 两侧都通 | `verified` | 显示地址 + 复制按钮 |
| 仅 Windows 通 | `unreachable` | **红字**："该服务仅 Windows 可达，WSL 中的 Core 无法访问"，不给复制按钮 |
| 未探测 | `unverified` | 显示端口，提示按运维指南配置 |

WSL 侧探测由 supervisor 通过 `wsl.exe -e curl ...` 或调用 Host 的探针完成；失败不得阻塞启动。

> 依据：`docs/operations/AgentRAG-启动与运维指南.md` 已说明 Windows 回环与 WSL 回环不是同一个地址；`extensions/README.md` 亦明确"仅在 Windows `127.0.0.1` 监听的服务不会因为换成宿主 IP 而可达"。

**已实测（2026-11-01）：WSL2 镜像网络可用，`127.0.0.1` 双向互通。**

| 方向 | 结果 |
|---|---|
| WSL → Windows `127.0.0.1:8001` | 200，返回 2560 维向量 |
| Windows → WSL `127.0.0.1:9999` | 200 |

因此在本机拓扑下，`Endpoints.Loopback`（`http://127.0.0.1:8001/v1`）即为 `verified` 地址，`Gateway` 字段仅在 NAT 模式下有意义。运维指南 §5.1 记录了完整配置。

**关键坑（supervisor 不负责，但必须写进运维文档）**：启用镜像网络时 `.wslconfig` 必须带 `autoProxy=false`。本机注册表残留 `ProxyServer=127.0.0.1:7897`（`ProxyEnable=0` 也一样），WSL 会尝试镜像该代理并失败，报 `ConfigureNetworking/0x8007054f` 后**回退到 `networkingMode=none`——WSL 完全没有网络**。supervisor 在 WSL 侧探测失败时，应先检查 `wslinfo --networking-mode` 是否为 `none`，而不是直接判定"模型不可达"。

### 6.3 不得触碰的回环

`AGENTRAG_PARSER_PROXY_ADDR=http://127.0.0.1:8090` 与 Bridge 的 `127.0.0.1:8090` 属于 **Core 与 Host 同在 WSL** 的同网络空间回环，本方案不得修改。

---

## 7. 显存防呆（修正 N7 / N10）

1. 取 `nvidia-smi --query-gpu=memory.free,memory.total --format=csv,noheader,nounits`。
2. **水位按比例**：`reserve = max(1.5 GB, 10% × total)`。
3. 预估：`模型大小(GB) + (CtxSize / 1024) × 0.15 GB`。
4. 若 `预估 > (free − reserve)` → `428 Precondition Required`，响应体返回 `{estimated_gb, free_gb, reserve_gb}` 供 UI 展示。
5. **降级路径**：`nvidia-smi` 不存在 / 返回非零 / 输出无法解析时，标记 `resources.state = "unverified"`，**不拦截也不静默放行**——前端显示"显存状态未知"，用户自行决定。禁止把未知当充足。
6. `force: true` 允许绕过，但必须写入 `LastError` 与日志（可审计）。

---

## 8. 日志（修正 N5 / N6）

```go
type LogLine struct {
    Seq     uint64    `json:"seq"`
    Time    time.Time `json:"time"`     // UTC，去重后保留首次出现时间
    Stream  string    `json:"stream"`   // stdout | stderr
    Level   string    `json:"level"`    // INFO | WARN | ERROR
    Content string    `json:"content"`
    Count   int       `json:"count"`    // 连续重复合并计数
    LastAt  time.Time `json:"last_at"`  // 最后一次重复的时间
}
```

- 环形缓冲固定容量 **2000 条**，超出丢最旧。
- 连续重复行合并为 `Count`，但 `Time` 与 `LastAt` 必须保留时间窗（诊断 CUDA OOM 的关键）。
- 进度提取正则 `loaded (\d+)/(\d+) tensors` 绑定到具体 llama.cpp build；build 变化时须重新核对，正则失配时降级为"加载中（无进度）"而非卡在 0%。
- 日志接口按 `seq` 增量拉取，支持 `?since=<seq>`，避免全量重传。

---

## 9. 生命周期与孤儿清理（修正 S3 / N9 / N11）

1. **Job Object**：进程创建时加入 Job，设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。supervisor 崩溃或被任务管理器强杀时，Windows 自动带走全部 llama-server 子进程，显存必然释放。
2. **SIGTERM 在 Windows 不存在**，v1.0 的 §9.3 无效，由 Job Object 取代。
3. **`taskkill /PID /T /F` 仅作为兜底**，且 PID 必须来自 Windows 原生 `exec.Cmd`（这也是必须 Windows 原生的原因）。
4. **持久化**：每次状态变更写 `data/model-supervisor.json`（原子替换：临时文件 + rename）。启动时读取，若记录的 PID 仍存活则接管为 `running`，否则标记 `stopped` 并清理。
5. **孤儿检测**：启动时扫描 `llama-server.exe` 中监听 `8001`/`8002` 的进程，不属于任何已知实例的标记为 `orphan` 并提示用户清理（不自动杀，避免误伤用户手工启动的实例）。

---

## 10. 安全模型（修正 F4 / N1）

| 项 | 要求 |
|---|---|
| 绑定 | 仅 `127.0.0.1:8091`，禁止 `0.0.0.0` / 局域网 |
| 鉴权 | 启动时生成随机 key，写入 `data/model-supervisor.json`（权限 0600）；除 `/healthz` 外全部要求 `Authorization: Bearer <key>` |
| 版本头 | `X-AgentRAG-Bridge-Version: 1`，不支持 major 版本快速拒绝 |
| CORS | 仅允许 `http://localhost:8080`、`http://127.0.0.1:8080`；不得 `*` |
| 路径白名单 | `BinaryPath` 必须位于 `data/bin/` 且 basename ∈ `{llama-server.exe}`；`ModelPath` 必须位于已配置的 `model_roots` 内 |
| 路径逃逸 | `filepath.Clean` 后 `filepath.Rel(root, p)` 结果不得以 `..` 开头，且必须 `EvalSymlinks` 后再校验 |
| 子进程暴露面 | 强制 `--host 127.0.0.1 --no-webui --api-key`；llama-server 自身默认 CORS `*` 且无 key（实测启动日志会打印该警告），必须由 supervisor 补齐 |
| 健康接口 | `/healthz` 不输出配置、路径、模型名或 key |

---

## 11. HTTP API 契约

基址 `http://127.0.0.1:8091`，除 `/healthz` 外均需 Bearer。

| 方法 | 路径 | 说明 | 状态码 |
|---|---|---|---|
| `GET` | `/healthz` | 存活探针 | 200 |
| `GET` | `/resources` | 显存/内存 + `state` | 200 |
| `GET` | `/instances` | 实例列表（`[]InstanceView`） | 200 |
| `POST` | `/instances` | 启动；`{id, model, task_type, port?, use_gpu, ctx_size, threads, extra_args?, force?}` | 202 / 400 / 409 / 428 |
| `DELETE` | `/instances/{id}` | 停止 | 200 / 404 / 409 |
| `GET` | `/instances/{id}/logs?since=<seq>` | 增量日志 | 200 / 404 |
| `POST` | `/instances/{id}/probe` | 重跑双向可达性与向量探针 | 200 |

状态码语义：`202` 已接受（异步启动）、`409` 状态冲突、`428` 资源预检未通过、`400` 参数非法（含路径逃逸与参数注入）。

---

## 12. 前端集成（修正 S1 / N3）

### 12.1 单一 bootstrap（满足 Frozen 3.8「1 UI route bootstrap」）

原生改动**仅两处，一次性**：

```ts
// 1) frontend/vite.config.ts —— 一行 alias
resolve: { alias: { '@ext': resolve(__dirname, '../extensions/ui') } }

// 2) frontend/src/extensions/registry.ts —— 唯一 bootstrap
export interface ExtensionPage {
  path: string; title: string; icon: string;
  component: () => Promise<any>;
}
export const EXTENSION_PAGES: ExtensionPage[] = [
  { path: 'model-supervisor', title: '本地模型引擎', icon: 'CpuIcon',
    component: () => import('@ext/views/ModelSupervisor.vue') },
]
```

`router/index.ts` 与 `stores/menu.ts` 各加一行展开：

```ts
...EXTENSION_PAGES.map(p => ({ path: p.path, component: p.component, meta: { title: p.title } }))
```

**验收标准**：新增第二个扩展页面时，只需在 `extensions/ui/` 内加文件并往 `EXTENSION_PAGES` 追加一项，**原生文件零改动**。达不到这条就是没满足 Frozen 3.8。

### 12.2 已知约束

- `frontend/src/stores/menu.ts` 中 `MenuItem` 的 `title` 为必填、`titleKey` 可选。使用字面 `title` 可避开 7 个 locale 文件（`embed`/`en-US`/`ja-JP`/`ko-KR`/`ru-RU`/`zh-CN`/`zh-TW`）的同步修改，代价是不随语言切换。**本项目选择字面 title**，并在文档中记录该取舍。
- `frontend/src/router/settingsRoute.test.mjs` 断言 router 不变量，改动路由后必须跑 `npm test`。
- `extensions/ui/` 会被打进前端构建产物；**不得**在其中引入 Node 侧依赖。

---

## 13. UI 设计

保留 v1.0 §5 的版式（资源仪表盘 + 实例卡片 + 启动抽屉 + 日志面板），修正如下：

1. 卡片地址区改为**三态**（§6.2），`unreachable` 时红字提示且禁用复制。
2. 卡片增加 **`EmbeddingDim` 显示**：探针实测维度，用户据此填写 Core 的模型配置。
3. 日志面板显示 `Time`（首次）与 `LastAt`（末次），重复行显示 `×N`。
4. 显存条显示 `free / total` 与 `reserve` 数值，`unverified` 时置灰并标注"未知"。
5. 启动抽屉的端口字段默认填 `8001`/`8002`，"自动分配"折叠为高级选项。
6. 样式使用独立 CSS 前缀 `msv-`，不污染全局。

---

## 14. 落地阶段

| 阶段 | 内容 | 通过判据 |
|---|---|---|
| **P0 手工验证**（先做，半天） | 手工在 Windows 起 embedding 与 rerank 实例，Core 配置地址后跑一次真实检索 | **检索返回结果**；失败则本方案全部前提不成立 |
| **P1 supervisor MVP** | `extensions/cmd/model-supervisor`：固定端口 + start/stop/list/logs + 白名单 + 鉴权 + Job Object + 持久化。**不做 UI** | §15 的 A1–A6 全过 |
| **P2 UI 接入** | registry bootstrap + `ModelSupervisor.vue` + 三态地址 | 侧边栏出现入口；`verified` 地址在 WSL 内 `curl` 通 |
| **P3 增强** | 显存比例水位、日志去重 + 时间戳、孤儿接管、加载进度、`--metrics` | 各自独立测试 |

> P0 不是可选项。在确认"Windows 上的 llama-server 能被 WSL 的 Core 调通"之前，其余设计都建立在未验证假设上。

---

## 15. 验收

### 15.1 扩展模块门禁

```bash
cd extensions
gofmt -l .                          # 无输出
go build -mod=readonly ./...        # 退出 0
go vet -mod=readonly ./...          # 退出 0
go test -mod=readonly ./... -count=1
python scripts/check_module_boundary.py
```

Windows 交叉编译（不引入 CGO）：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/model-supervisor.exe ./cmd/model-supervisor
```

### 15.2 行为验收

| 编号 | 用例 | 期望 |
|---|---|---|
| A1 | `data/bin/` 内 `llama-server.exe --version` | 打印 `0.2.0-dev (build 10709...)`，退出 0 |
| A2 | 启动 embedding 实例 | `202`；随后 `/instances` 为 `running`；`/v1/embeddings` 返回 **2560 维**非空向量（Qwen3-Embedding-4B-Q6_K 实测值） |
| A3 | 停止实例 | `200`；`tasklist` 中无对应 `llama-server.exe`；显存回落 |
| A3b | rerank 实例 `POST /v1/rerank` | 返回 `relevance_score`，相关文档分数显著高于无关文档（实测 0.1176 vs 4.2e-05） |
| A4 | 任务管理器强杀 supervisor | Job Object 生效，`llama-server.exe` 一并消失 |
| A5 | `BinaryPath = "..\\..\\windows\\system32\\cmd.exe"` | `400` |
| A6 | `extra_args = ["--host","0.0.0.0"]` | `400` |
| A7 | 同一 id 连续两次 `POST /instances` | 一次 `202`、一次 `409` |
| A8 | 故意超配 ctx 触发预检 | `428`，响应含 `estimated_gb`/`free_gb`/`reserve_gb` |
| A9 | 无 Bearer 调用 `/instances` | `401` |
| A10 | 浏览器 `fetch` 到 `/instances`（跨源） | 仅白名单 origin 通过 |
| A11 | 重启 supervisor | 实例列表恢复；存活进程被接管为 `running` |

---

## 16. 未决问题

> 实现层细节（包布局、接口、Job Object、持久化、启动集成）见 `AgentRAG 模型进程管理器-实现规格 (v1.2).md`。

1. ~~**WSL 侧探测的实现路径**~~ **已定案（v1.2 §7）**：用 `wsl.exe -d <distro> -e curl`；`wsl.exe` 缺失或发行版名不符时降级为 `unverified`，不判失败。不采用"由 Host 代探"——那会把 supervisor 与治理层耦合，正是 §2 要消除的故障域污染。
2. **`--pooling` 语义正确性**：已实测不传 `--pooling` 即可用（走 GGUF 元数据），且日志不打印 pooling 类型。但"能出向量"不等于"pooling 正确"——P0 必须用 Core 真实检索验证召回质量；若召回异常，按 `--pooling {mean,cls,last,rank}` 逐个比对。
3. **模型热切换**：本版本不支持，需停止再启动。
4. **多 GPU**：`nvidia-smi` 多卡场景下的选择策略未定义。
5. **`--metrics` 是否接入**：当前不接，若接入需明确采集端。

---

## 17. 与 Frozen Architecture 的关系

本组件不属于治理层，不触及不变量 A（事实状态一致性）、B（治理动作可审计）、D（治理关键关系原子化）、E（投影不是事实源）。与 C（跨系统发布幂等）无交集——它不发布、不同步远端。

唯二间接风险及处置：

1. **故障域污染** → 由 §2 独立进程 + 独立端口消除。
2. **质量归因错位** → 由 §1 边界声明 + §6.2 三态地址 + §15 向量探针消除。

本文件不修改 Frozen Architecture 任何条款。
