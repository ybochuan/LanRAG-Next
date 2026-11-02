# AgentRAG 模型进程管理器 — 实现规格 (v1.2)

> 上位文档：`AgentRAG 模型进程管理器设计方案 (v1.1-Draft).md`
> 本文只写 v1.1 没覆盖到实现层的部分：包布局、接口、Windows Job Object、持久化、配置、启动集成，以及 §16 未决问题的定案。
> 边界声明沿用 v1.1 §1：本组件不参与治理决策，不写 `ext_*`，不改 lifecycle / lineage / governance truth。

---

## 0. P0 门禁状态：已通过

v1.1 §14 要求 P0 先行。该验证已于 2026-11-01 完成，证据在 `docs/evidence/m2-e2e-mirrored-2026-11-01.md`：

| P0 判据 | 实测 |
|---|---|
| Windows 手工起 embedding / rerank | 8081 / 8082，`llama-server 0.2.0-dev (build 10709)` |
| Core（WSL）配置地址后跑真实检索 | `POST /api/v1/knowledge-search` |
| **检索返回结果** | 7 条，`candidate_count=49`，`rerank.applied=true`，`top_score=0.9996` |

结论：P1 的前提成立。同时该轮暴露了一个 P1 必须处理的运维事实——**Core 里保存的模型 base_url 会因网络模式切换而变成死地址，且失败是静默的**（降级为纯关键词、HTTP 200、空结果）。见 §8.3。

---

## 1. 包布局

```
extensions/modelsupervisor/            # 领域逻辑，可跨平台编译，不含 syscall
    config.go        Config 定义、加载/保存、校验
    spec.go          TaskType、InstanceSpec、InstanceView、Status 常量
    cmdline.go       argv 组装、参数白名单、注入拒绝
    manager.go       Manager：Start / Stop / List / Get、状态机、并发
    store.go         状态快照持久化（原子写 JSON）
    logbuf.go        环形缓冲 + 连续重复合并 + 时间戳
    probe.go         分层探针（TCP → /health → 任务探针 → 跨 OS）
    resources.go     nvidia-smi 解析、三态（ok / unverified / error）
    http.go          HTTP 路由、Bearer 鉴权、CORS、DTO 编解码
    *_test.go        见 §9 验收映射

extensions/modelsupervisor/winjob/     # 仅 Windows
    job_windows.go   Job Object 封装（//go:build windows）
    job_other.go     非 Windows 返回 ErrUnsupported（//go:build !windows）

extensions/cmd/model-supervisor/
    main.go          组装、配置定位、信号处理、优雅退出
```

**分层理由**：`modelsupervisor` 主包不带 build tag，`go test ./...` 在 Windows 与 WSL 上都能跑；只有 Job Object 进 `winjob` 子包。这样 §9 的门禁在两边都成立，不需要"只在 Windows 上才能测"的例外。

---

## 2. 依赖与构建

- 只新增一个**直接**依赖：`golang.org/x/sys`（已在 `go.sum`，`v0.49.0`，目前是 indirect）。不引入 `golang.org/x/exp`、不引入第三方 HTTP 框架——用 `net/http`。
- 不引入 SQLite。理由见 §5。
- 构建目标固定 `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`。`modernc.org/sqlite` 虽在模块里，但本命令不 import 它，产物不膨胀。

```bash
cd extensions
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -o ../data/bin/model-supervisor.exe ./cmd/model-supervisor
```

产物落在 `data/bin/`（已被 `data/.gitignore` 的 `bin/` 覆盖），与 `llama-server.exe` 同目录。

---

## 3. 状态机与并发

```
                 ┌──────────┐
   Start ───────►│ starting │──── 探针失败/进程退出 ──►┌────────┐
                 └────┬─────┘                          │ failed │
                      │ 探针全绿                        └────┬───┘
                      ▼                                     │ Stop
                 ┌──────────┐                               ▼
                 │ running  │── Stop ──►┌──────────┐  ┌──────────┐
                 └──────────┘            │ stopping │─►│ stopped  │
                                         └──────────┘  └──────────┘
```

- 合法转移只有上表这几条，其余返回 `409`。转移集中在 `manager.go` 一个 `func (m *Manager) transition(id, from, to Status) error`，不散落在各处。
- 并发：`Manager` 持一把 `sync.Mutex` 保护实例表与状态；**探针与进程等待在锁外执行**（否则一次 180 秒的启动会把 `/healthz` 也堵死）。
- 同一 `id` 的 `Start` 幂等键是状态：`starting`/`running` 时再 `Start` 直接 `409`（对应 A7）。
- `Stop` 等待进程退出最多 10 秒，超时 `Kill()`，再等 2 秒。

---

## 4. Windows Job Object（v1.1 的 P0-5 落地）

```go
// winjob/job_windows.go
func New() (*Job, error)                       // CreateJobObject + KILL_ON_JOB_CLOSE
func (j *Job) Assign(p *os.Process) error      // AssignProcessToJobObject
func (j *Job) Close() error
```

要点：

1. `SetInformationJobObject(JobObjectExtendedLimitInformation, {BasicLimitInformation.LimitFlags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE})`。supervisor 被任务管理器强杀时内核关闭句柄 → 组内进程一并终止（A4）。
2. **整个 supervisor 生命周期共用一个 Job**，不是每实例一个。这样一次关句柄就清空所有子进程。
3. 赋值时机：`cmd.Start()` 之后立即 `Assign`。`os/exec` 不暴露主线程句柄，无法做 `CREATE_SUSPENDED → Assign → ResumeThread`。残留窗口是微秒级，且 `llama-server.exe` 不派生子进程，所以不构成实际风险——**这个取舍要写进注释**，不要假装没有。
4. 非 Windows 构建下 `winjob.New()` 返回 `ErrUnsupported`，`main.go` 据此直接退出并打印"本程序必须在 Windows 上运行"。防止有人误在 WSL 里启动它，产生 §S3 那个 Linux PID 语义错误。

---

## 5. 持久化

**用 JSON 快照，不用 SQLite。**

- 位置：`data/model-supervisor-state.json`（与 `data/agentrag-launcher.json` 同级，已被 `data/.gitignore` 覆盖？——**没有，需补一行**，见 §8.1）。
- 内容只存"应该存在什么"，不存运行时易变值：

```json
{
  "schema": 1,
  "instances": [
    { "id": "embedding", "model": "Qwen3-Embedding-4B-Q6_K.gguf",
      "task_type": "embedding", "port": 8081, "use_gpu": true,
      "ctx_size": 4096, "threads": 8, "extra_args": [] }
  ]
}
```

- 原子写：`tmp` + `os.Rename`。启动时若文件损坏 → 记录错误、以空表启动、**不覆盖原文件**（保留现场）。
- 不存 PID 作为真相：重启后按端口 + 二进制名做孤儿探测（§7），PID 只作提示。
- 理由：状态量 ≤ 8 条，无并发读，无迁移需求；supervisor 不应把治理层的 SQLite 栈拖进来，也不该有自己的 schema 版本债。

---

## 6. 命令行契约（补 v1.1 §5 的可执行细节）

### 6.1 端口默认值：必须对齐现状

**默认 `embedding=8081`、`rerank=8082`**，不是 v1.1 §13 写的 8001/8002。

理由：`data/weknora.db` 的 `models.parameters` 当前就指向 `http://127.0.0.1:8081/v1` 与 `:8082/v1`。若 supervisor 默认换端口，等于让每一个照文档操作的用户重新踩一遍 §8.3 那个静默失效。默认值必须与已验收的现场一致。

### 6.2 受保护参数（不可被 `extra_args` 覆盖）

`--host --port --model --alias --api-key --api-key-file --embedding --embeddings --rerank --reranking --no-webui --no-ui`

出现任一即 `400 invalid_extra_args`。允许放行的示例：`--flash-attn`、`--no-mmap`、`--cache-type-k`、`--cache-type-v`、`--batch-size`、`--ubatch-size`、`--n-cpu-moe`。白名单是**允许列表**而不是黑名单——黑名单永远漏。

### 6.3 组装出的 argv（受保护参数由 supervisor 独占写入）

```
<llama-server.exe> --model <abs model path> <task flag> --host 127.0.0.1 --port <port>
                   --alias <id> --no-webui --api-key <generated> -ngl <99|0> -c <ctx> -t <threads> [extra...]
```

- `--host` 恒为 `127.0.0.1`，无例外。镜像网络下 WSL 已可直连（`docs/operations/AgentRAG-启动与运维指南.md` §5.1 实测）。
- `--api-key` 由 supervisor 生成并持久化到 `data/model-supervisor.json`，**不回显到日志、不出现在 `/instances` 响应里**。这一条直接消掉 llama.cpp 启动时那句 `CORS is set to allow all origins ('*') and no API key is set`。

---

## 7. 探针（v1.1 §16.1 定案）

分四层，前一层不过就不做下一层：

| 层 | 做法 | 失败语义 |
|---|---|---|
| L1 | `net.DialTimeout("tcp", "127.0.0.1:"+port, 1s)` | 进程还没监听 |
| L2 | `GET /health`，1s 超时 | 监听了但没就绪 |
| L3 | 任务探针：embedding → `POST /v1/embeddings` 取 `len(data[0].embedding)`；rerank → `POST /v1/rerank` 取 `results[0].relevance_score` | 能应答但语义不对（F1/F2 的兜底） |
| L4 | **跨 OS**：`exec.Command("wsl.exe", "-d", "Ubuntu", "-e", "curl", "-s", "-m", "2", "-o", "/dev/null", "-w", "%{http_code}", url)` | 见下 |

**L4 定案：用 `wsl.exe -d Ubuntu -e curl`。**

- 不选"由 Host 代探"：那会把 supervisor 和治理层耦合，Host 不可用时连模型状态都看不见——正是 v1.1 §2 要消除的故障域污染。
- 不选"只报 Windows 视角"：那等于退回 P0-1 的错误，用户复制到一个 WSL 打不开的地址。
- 降级：`wsl.exe` 不存在、发行版名不符、curl 缺失 → L4 记为 `unverified`，**不判失败**。三态里 `unverified` 是合法结果，不是错误。
- 发行版名不硬编码：读 `$WSL_DISTRO_NAME` 不可靠（supervisor 是 Windows 进程），改为配置项 `wsl_distro`，默认 `Ubuntu`，可在 `data/model-supervisor.json` 覆盖。

**探针结果直接决定 `/instances` 里地址的三态**（`loopback` / `gateway` / `unverified`），UI 据此决定是否给复制按钮。这是 v1.1 §6.2 的执行侧。

---

## 8. 三个必须现在决定的边界

### 8.1 `data/.gitignore` 要补一行

`model-supervisor-state.json` 与 `model-supervisor.json` 都含凭据/运行态。前者当前**没有被忽略**，会污染 git。加：

```
model-supervisor.json
model-supervisor-state.json
```

（`model-supervisor.json` 已在，`-state` 那条要补。）

### 8.2 启动集成：supervisor 是常驻服务，不由 `start-all.sh` 拉起

- `start-all.sh` 跑在 WSL，要拉起 Windows 进程只能经 `powershell.exe`/`wsl.exe` interop，**Ctrl+C 清理时杀不掉它**，会留下 §P1-2 那种"进程还活着但没人管"的状态。
- 定案：supervisor 由用户在 Windows 侧启动一次（或加计划任务开机自启）。`start-all.sh` 只做**探活 + 提示**：`GET 127.0.0.1:8091/healthz` 不通就打印"模型管理器未运行，请先在 Windows 侧启动 model-supervisor.exe"，然后照常启动 Core/Host——**不阻塞**，因为模型服务可以独立于 supervisor 存在。
- 这条也回答了"谁先谁后"：没有顺序依赖。Core 只依赖模型端口，不依赖 supervisor。

### 8.3 supervisor 必须能诊断"Core 里存的是死地址"

P0 那轮的真实故障：网络模式从 NAT 换到 mirrored 后，`models.parameters` 里还是 `http://172.26.0.1:8081/v1`，embedding 请求 `i/o timeout`，Core **静默降级为纯关键词检索**，返回 HTTP 200 + 空结果。用户只看到"没搜到"。

supervisor 侧能做的两件事（都不越界）：

1. `/instances` 的地址三态里，`unverified` 明确写"仅 Windows 侧可达"，并在 UI 红字提示"若 Core 显示检索为空，请核对 Core 模型设置中的 base_url"。
2. `/instances/{id}/probe` 返回里带 `observed_ports`：本机哪些端口在监听、分别是不是自己管的。用户能立刻看出"我配的 8081 其实没人监听"。

**不做**：supervisor 不去读 `weknora.db`、不调 Core 的模型配置 API。那是 Core 的事实，扩展写它就是 v1.0 §10.2 明确放弃的静默写入。

---

## 9. 验收映射（v1.1 §15.2 的测试落点）

| 编号 | 落点 |
|---|---|
| A1 | `cmdline_test.go`：`--version` 执行，断言含 `0.2.0-dev` |
| A2 | `manager_test.go`：起 embedding，断言 `202` → `running`，`/v1/embeddings` 维度 **2560** |
| A3 | `manager_test.go`：Stop 后端口拒绝连接 |
| A3b | `manager_test.go`：rerank 相关/无关文档分数分离 |
| A4 | **手工**（需真实 Job Object）：任务管理器杀 supervisor，`tasklist` 无残留 |
| A5 | `cmdline_test.go`：`..\\..\\windows\\system32\\cmd.exe` → 400 |
| A6 | `cmdline_test.go`：`["--host","0.0.0.0"]` → 400 |
| A7 | `manager_test.go`：并发两次 Start，一 `202` 一 `409` |
| A8 | `manager_test.go`：超配 ctx → `428`，含 `estimated_gb`/`free_gb`/`reserve_gb` |
| A9 | `http_test.go`：无 Bearer → `401` |
| A10 | `http_test.go`：跨源 `Origin` 不在白名单 → 拒绝 |
| A11 | `store_test.go`：状态文件往返；`manager_test.go`：重启后按端口接管存活进程 |

A4 无法自动化，必须在证据文档里单独记录手工步骤与截图，不得用单元测试冒充。

---

## 10. 分步落地顺序

| 步 | 内容 | 完成判据 |
|---|---|---|
| 1 | `spec.go` + `cmdline.go` + 测试 | A5/A6 过；`go test` 全绿 |
| 2 | `store.go` + `logbuf.go` + 测试 | 往返一致、去重正确 |
| 3 | `winjob` + `manager.go`（不含探针） | 起停可用，A7 过 |
| 4 | `probe.go` + `resources.go` | A2/A3/A3b/A8 过 |
| 5 | `http.go` + `main.go` | A9/A10/A11 过，`/healthz` 可达 |
| 6 | 手工 A4 + 真实检索回归 | 证据文档记录 |

UI（v1.1 §12/§13）不进本规格，属于 P2，等 P1 六步全绿后再开。

---

## 11. 与 v1.1 的差异汇总

| v1.1 | v1.2 定案 | 原因 |
|---|---|---|
| §13 端口默认 8001/8002 | **8081/8082** | 与已验收现场和 Core 现存配置一致 |
| §16.1 探测路径未决 | `wsl.exe -d Ubuntu -e curl`，失败降级 `unverified` | 不耦合治理层；`unverified` 是合法态 |
| §11 基址 8091 | 保持 8091 | 与 8090/8081/8082/18880/8000 均不冲突 |
| 未提持久化格式 | JSON 快照 + 原子写 | 状态量极小，不值得引入 schema 债 |
| 未提启动集成 | 常驻服务，`start-all.sh` 只探活不拉起 | 跨 OS 进程清理不可靠 |
| 未提死地址诊断 | `/probe` 返回 `observed_ports` | P0 实测的真实故障模式 |
