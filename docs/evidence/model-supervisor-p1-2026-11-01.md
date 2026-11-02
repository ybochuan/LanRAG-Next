# 模型进程管理器 P1 实施与验收（2026-11-01）

上位文档：`docs/design/AgentRAG 模型进程管理器-实现规格 (v1.2).md`。
本轮范围：v1.2 §10 的步骤 1–6（P1 全部），不含 P2 的 UI 与 `start-all.sh` 集成。

## 1. 新增代码

| 路径 | 作用 |
|---|---|
| `extensions/modelsupervisor/spec.go` | `Spec`/`View`/`TaskType`/`Status`/`Reachability`，校验与默认值 |
| `extensions/modelsupervisor/cmdline.go` | `llama-server.exe` 命令行契约、路径包含校验、`--api-key` 脱敏 |
| `extensions/modelsupervisor/logbuf.go` | 有界日志环 + 连续重复折叠（保留首末时间窗） |
| `extensions/modelsupervisor/store.go` | JSON 状态快照，`CreateTemp`→`Sync`→`Rename` 原子写 |
| `extensions/modelsupervisor/manager.go` | 状态机、进程启动/停止/等待、持久化 |
| `extensions/modelsupervisor/probe.go` | 四层探测：TCP → `/health` → 任务端点 → WSL 侧复核 |
| `extensions/modelsupervisor/resources.go` | `nvidia-smi` 采样、容量估算、`unverified` 降级 |
| `extensions/modelsupervisor/http.go` | 回环 HTTP 契约、鉴权、版本头、错误码映射 |
| `extensions/modelsupervisor/config.go` | 环境配置与两条不可放宽的校验 |
| `extensions/modelsupervisor/run.go` | 装配、监听、有序关闭 |
| `extensions/modelsupervisor/winjob/` | Windows Job Object（`KILL_ON_JOB_CLOSE`），非 Windows 返回 `ErrUnsupported` |
| `extensions/cmd/model-supervisor/main.go` | 独立进程入口，与 Extension Host 故障域隔离 |

二进制构建到 `data/bin/model-supervisor.exe`（`data/bin/` 已被 `data/.gitignore` 忽略）。

## 2. 验收对照（全部实测，非推断）

| 编号 | 验收项 | 结果 |
|---|---|---|
| A1 | 启动 → 健康 → 停止 | `POST /start` 202；`/health` 经 000→503→200，第 3 次轮询就绪（约 9 秒） |
| A2 | 真实向量维度 | `embedding_dim: 2560`（Qwen3-Embedding-4B-Q6_K） |
| A3 | 两侧可达 | `reachability: "loopback"`，`note: "reachable from both sides"` |
| A3b | 降级态 | `wsl.exe` 失败时返回 `unverified` 并带原因，不误报成功 |
| A4 | 孤儿进程 | `taskkill /PID 33576 /F` 杀掉 supervisor 后，两个 `llama-server.exe` 同时消失，8081/8082 立即拒绝连接 |
| A5 | 命令行契约 | 单元测试覆盖受保护 flag、`-ngl 99/0`、`--api-key` 脱敏 |
| A6 | 路径逃逸 | `../secrets.gguf` → 400；`ResolveUnder` 词法 + `EvalSymlinks` 双重校验 |
| A7 | 并发启动 | 8 并发同 id → 1 成功、7 个 `ErrDuplicateID` |
| A8 | 容量防呆 | 真实 GPU 下 `HTTP 428`，`estimated_gb 3.68 / free_gb 0.20 / reserve_gb 1.50`；`force:true` → 202 且响应含 `"forced":true` |
| A9 | 鉴权 | 无凭证 401、错误 key 401、错误 bearer 401；`X-API-Key` 与 `Authorization: Bearer` 均可用 |
| A10 | 输入校验 | 未知模型 400、路径穿越 400、版本头 `9` → 400 |
| A11 | 幂等与未知 | 未知 id → 404；连续两次 stop → 均 200 |

补充：`GET /extensions/models/host-resources` 返回真实 `nvidia-smi` 数据（RTX 4060 Laptop，8188 MB）。
`GET /extensions/models/probe` 返回 `observed_ports: [8081, 8082]`。

## 3. 真实链路复核

supervisor 拉起的两个模型被 Core 直接使用：

```
POST /api/v1/knowledge-search  → HTTP 200
results 7, candidate_count 49, rerank.applied true, top_score 0.9995
```

即 Core(WSL) → embedding(Windows 8081) → 向量召回 49 → rerank(Windows 8082) 全链路在 supervisor 托管下仍然成立。

## 4. 实测中发现并修复的两个缺陷

### 4.1 持久化实例重启后无法再启动（功能缺陷）

`Manager.Start` 对已存在的 id 一律返回 `ErrDuplicateID`。但 `Load()` 会把持久化实例恢复为 `stopped`，
于是 supervisor 重启后**每一个已配置实例都永久 409**，持久化反而成了死锁。
修复：只有 `starting/running/stopping` 占用 id；`stopped/failed` 允许重新启动。
端口冲突仍对全部实例检查（状态文件必须保持端口唯一），但排除自身 id。
回归测试：`TestStartAcceptsPersistedInstanceAfterReload`。

### 4.2 跨 OS 探测静默降级（配置缺陷）

`wsl.exe -e curl ...` 不指定发行版时走默认发行版；本机默认发行版是 `docker-desktop`，没有 `curl`，
于是每次探测都以 `exit status 1` 结束、地址永远是 `unverified`。
修复：新增 `MODEL_SUPERVISOR_WSL_DISTRO`（默认 `Ubuntu`），并在 `config.go` 注释里写明原因。
修复前 `note: "cross-OS probe unavailable: exit status 1"`；修复后 `reachability: "loopback"`。

### 4.3 默认落在 CPU 推理（静默降级）

`Spec.UseGPU` 是普通 `bool`，零值 `false`，于是**未指定的实例一律以 `-ngl 0` 启动**——在有独立显卡的机器上
悄悄跑 CPU 推理。它照样 `/health` 200、照样输出 2560 维向量，只是慢一个数量级；而容量防呆此时还在检查显存，
检查的对象根本不是真正被使用的资源。实测进程命令行确认为 `-ngl 0`。

修复：`UseGPU` 改为 `*bool`（`json:"use_gpu,omitempty"`），未设置即视为开启 GPU；`use_gpu:false` 才显式走 CPU。
新增 `TestSpecUsesGPUByDefault`。修复后实测两个实例命令行均为 `-ngl 99`。

该缺陷同时暴露了状态文件的兼容问题：旧状态文件里写着 `"use_gpu": false`，指针化之后会被解释为「显式 CPU」。
本轮直接删除该开发态状态文件重新登记，未写迁移——字段语义变更时保留旧值反而会固化错误结论。

## 5. 命令与退出状态

| 命令（`extensions/`） | 退出状态 |
|---|---|
| `gofmt -l .` | 0，无输出 |
| `go build -mod=readonly ./...` | 0 |
| `go vet -mod=readonly ./...` | 0 |
| `go test -mod=readonly ./... -count=1` | 0，全部包通过 |
| `python scripts/check_module_boundary.py` | 0，315 imports，无 WeKnora 依赖 |

## 6. 未验证项（如实记录）

- **Linux race 未跑**：本机 WSL 没有 Go 工具链（`command -v go` 为空），`go test -race` 无法执行。
  `winjob` 在非 Windows 下只返回 `ErrUnsupported`，无并发面；`manager`/`http` 的并发测试已在 Windows 下覆盖。
- **Job Object 的崩溃场景只测了强杀**：`taskkill /F` 已验证；真实崩溃（访问违例）未构造。
- **未接入 `start-all.sh`**：supervisor 目前手工启动，P2 再做探活与顺序。
- **未做 UI**：P3。
- **未在真实 OOM 下验证 `failed` + `exit_code`**：本轮 GPU 余量足够，未故意触发 CUDA OOM。

## 7. 当前运行态

- supervisor：`127.0.0.1:8092`，`data/model-supervisor-state.json` 中持久化 `embedding`(8081) 与 `rerank`(8082)。
- 两个 `llama-server.exe` 由 supervisor 托管，`--host 127.0.0.1`，带 Job Object。
- Core 18880、Host 8090 未受影响。
