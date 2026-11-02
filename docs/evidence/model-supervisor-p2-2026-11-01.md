# 模型进程管理器 P2（控制台界面）— 2026-11-01

上位文档：`../design/AgentRAG 模型进程管理器-实现规格 (v1.2).md`（P1 六步）、
`../design/AgentRAG 模型进程管理器设计方案 (v1.1-Draft).md` §12 / §13（UI）。

P2 在 P1 六步全绿后解锁。本文件记录 P2 的范围、决定、实测结果与已知限制。

## 1. 范围与关键决定

| 项 | 决定 | 理由 |
|---|---|---|
| 可达性显示 | **卡片每次刷新都调 `GET /extensions/models/{id}/probe`** | `/list` 里的 `reachability` 是启动时的快照，实例起来后不会再更新；直接渲染会让卡片永远停在 `unverified`。用户明确要求按次探测 |
| 前端取数 | 新增 `frontend/src/api/modelSupervisor.ts`，**不复用 `@/utils/request`** | 后者会拼 `/api/v1` 前缀、带 Core 的 JWT、并把请求打到 Core。supervisor 是独立回环进程，有自己的令牌；且 Core 不可用时仍要能启停模型 |
| 跨源 | supervisor 增加 CORS 允许列表，默认 `http://127.0.0.1:18880,http://localhost:18880` | 控制台在 18880，supervisor 在 8092，浏览器视为跨源。预检**必须在鉴权之前**处理，否则无令牌的 OPTIONS 会得到 401，浏览器只会报网络错误 |
| 令牌 | 页面自带令牌门，令牌存 localStorage，与 Core 登录态**互不相通** | 两套凭据语义不同；不把 Core JWT 复用给 supervisor |
| 页面文案 | 硬编码中文，不走 i18n | 与 `LifecycleView.vue`（M2-UI 先例）一致；只有菜单项需要 `menu.*` 键 |

## 2. 改动清单

### 扩展（Go）

| 文件 | 改动 |
|---|---|
| `extensions/modelsupervisor/http.go` | `Server.AllowedOrigins []string`；`cors` 中间件包住 mux；`originAllowed`；新路由 `GET /extensions/models/{id}/probe` → `handleProbeOne` |
| `extensions/modelsupervisor/config.go` | `DefaultAllowedOrigins`；`Config.AllowedOrigins`；`MODEL_SUPERVISOR_ALLOWED_ORIGINS`（逗号分隔）；`Validate` 用 `net/url` 校验 scheme/host、只允许 http(s)、不得带 path、不得含通配 |
| `extensions/modelsupervisor/run.go` | 把 `cfg.AllowedOrigins` 传给 server |
| `extensions/modelsupervisor/http_cors_test.go` | 新建，6 个用例 |

### 前端（Vue）

| 文件 | 改动 |
|---|---|
| `frontend/src/api/modelSupervisor.ts` | 新建。裸 `fetch` 客户端；`SupervisorError` 携带 status/code/detail；`isCapacityRefusal` 识别 428 |
| `frontend/src/views/model-supervisor/ModelSupervisorView.vue` | 新建。令牌门、显存条、实例卡片（含逐卡探测）、日志面板（`since` 游标轮询）、新增实例表单、428 强行启动确认 |
| `frontend/src/router/index.ts` | `/platform` children 新增 `model-supervisor` 路由 |
| `frontend/src/stores/menu.ts` | `menuArr` 新增菜单项，`icon: 'model'` |
| `frontend/src/components/menu.vue` | 图标映射链新增 `model` 分支 + `modelIcon` ref |
| `frontend/src/assets/img/model.svg` | 新建，芯片字形 |
| 6 个 locale | 新增 `menu.modelSupervisor`（zh-CN / zh-TW / en-US / ja-JP / ko-KR / ru-RU） |

## 3. 验证

| 检查 | 命令 | 结果 |
|---|---|---|
| 扩展格式 | `gofmt -l .` | 无输出 |
| 扩展构建 | `go build -mod=readonly ./...` | 退出 0 |
| 扩展静态检查 | `go vet -mod=readonly ./...` | 退出 0 |
| 扩展测试 | `go test -mod=readonly ./... -count=1` | 退出 0 |
| 模块门禁 | `python scripts/check_module_boundary.py` | 退出 0，315 imports |
| 前端类型 | `npm run type-check` | 退出 0 |
| 前端测试 | `npm test` | 2369 用例：2368 通过 / 1 跳过 / 0 失败 |
| 前端构建 | `npm run build` | 退出 0，产出 `dist/`，再同步到 `web/` |

### 运行期（真实 GPU：RTX 4060 Laptop，8188 MB）

| 检查 | 结果 |
|---|---|
| CORS 预检（`Origin: http://127.0.0.1:18880`） | `204`，回显 `Access-Control-Allow-Origin`、`Vary: Origin`、`-Max-Age: 600` |
| 真实响应 | 同样回显 `Access-Control-Allow-Origin` |
| 页面会发的 4 类请求 | `/list`、`/host-resources`、`/{id}/probe`、`/{id}/logs?since=0` 均 200 |
| 单实例探测 | `embedding` → `reachability: loopback`、`embedding_dim: 2560`、`health_latency_ms: 1`；`rerank` → `loopback` |
| 显存读数 | `free_gb 7.77 / total_gb 8 / reserve_gb 1.5` |
| 日志接口 | 返回 llama.cpp 原始行（`embeddings enabled with n_batch ...`），带 `seq`/`time`/`count` |
| SPA 路由 | `GET /platform/model-supervisor` → 200，`web/assets/ModelSupervisorView-*.js` 已生成 |

## 4. 本轮修复的两个前端问题

1. **`{{ baseUrl() }}` 在模板里不可见**：`<script setup>` 只导入了 `* as api`，模板只能访问导入的绑定。
   `vue-tsc` 报 `TS2339: Property 'baseUrl' does not exist`。改为 `{{ api.baseUrl() }}`。
2. **样式守卫两轮失败**（`src/assets/theme/styleGuard.test.mjs`）：
   - `font-size-literal`：25 处字面量超过基线 33。改为 `var(--app-text-2xs … 4xl)`。
   - `td-token-fallback`（基线 0）与 `radius-literal`（基线 160）：`var(--td-*, #666)` 的兜底值和 `border-radius: 6/8/10/999px` 均为违规。改为不带兜底的 `var(--td-*)` 与 `var(--app-radius-sm/md/lg/pill)`。

## 5. 模型目录与三种任务类型（2026-11-01 追加）

新增 `GET /extensions/models/catalog`（`ScanCatalog`）：只列 `data/models/` 下的 `.gguf`（大小写不敏感、跳过目录与非常规文件、按名排序），并报告 `binary_dir` / `binary_ready`。模型目录不存在不是错误，用 `note` 说明——新装机器合法地没有模型，前端该解释而不是报错横幅。

控制台「新增模型实例」的模型字段由自由文本改成下拉，选项带文件大小，另有「重新扫描模型目录」按钮。

`TaskType` 从两种扩到三种：

| 任务 | llama-server 开关 | 默认端口 | 探测路径 | 探测超时 |
|---|---|---|---|---|
| `embedding` | `--embedding` | 8081 | `/v1/embeddings` | 3s |
| `rerank` | `--rerank` | 8082 | `/v1/rerank` | 3s |
| `chat` | **无**（llama-server 默认模式） | 8083 | `/v1/chat/completions` | 45s |

`chat` 额外带 `--jinja`，让 llama-server 使用 GGUF 自带的对话模板；不带会退回 llama.cpp 内置默认模板，角色与工具格式会被静默改写。8081/8082 与 Core 数据库里已存的 `base_url` 一致，8083 是紧邻的空位。

实测（`Qwen3.5-9B-Q4_K_M-imatrix.gguf`，`use_gpu:false` 即 `-ngl 0`）：

| 检查 | 结果 |
|---|---|
| `GET /catalog` | 3 个模型，`binary_ready: true`，含 5,889,811,552 字节的 chat 模型 |
| `POST /start`（chat, CPU） | 202；命令行确认 `--jinja --host 127.0.0.1 --port 8083 -ngl 0 -c 4096 -t 8` |
| `/health` | 200 |
| `GET /extensions/models/chat/probe` | `reachability: loopback`、`note: reachable from both sides` |
| 真实补全 `POST /v1/chat/completions` | 返回 `choices[0]`，`prompt_n 11 / prompt_ms 2442`（CPU 4.5 tok/s），`model: chat` |
| 停止后 | `status: stopped`，spec 保留（下次可在控制台直接启动） |
| Core 检索复核 | 7 条、`candidate_count 49`、`rerank.applied true`、`top_score 0.9995` |

## 6. GPU 层数参数（`gpu_layers`）

`Spec.GPULayers *int`（json `gpu_layers,omitempty`）直接对应 llama-server 的 `-ngl`。解析规则集中在 `Spec.GPULayerCount()`：显式值优先，否则 `use_gpu == false` 给 0，其余给 `DefaultGPULayers = 99`。`usesGPU()` 改为 `GPULayerCount() > 0`，于是显存防呆自然只对真正用显卡的实例生效。

`use_gpu:false` 与 `gpu_layers > 0` 同时出现直接拒绝（`ErrConflictingGPU`）：两个字段描述同一个决策，矛盾组合会让 `-ngl` 取决于字段求值顺序。`-ngl` / `--gpu-layers` / `--n-gpu-layers` 加入 `supervisorFlags`，调用方不能再用 `extra_args` 重述。

`View.GPULayers` 回传的是**解析后**的值，前端卡片因此能直接显示 `-ngl 99` / `-ngl 0`，读者不必重演优先级。

| 检查 | 结果 |
|---|---|
| `gofmt` / `go build` / `go vet` / `go test ./modelsupervisor/...` | 无输出 / 0 / 0 / `ok 0.566s` |
| `npm run type-check` / `npm test` / `npm run build` | 0 / 2370 用例 0 失败 / 0 |
| 显式 `gpu_layers:30` 启动 embedding | 响应 `gpu_layers: 30`；`Get-CimInstance Win32_Process` 确认命令行 `-ngl 30`；probe 仍 `loopback` + `embedding_dim 2560` |
| 默认启动 rerank | 响应 `gpu_layers: 99`，命令行 `-ngl 99` |
| 无 `gpu_layers` 字段的旧 state 加载 | 兼容：embedding/rerank → 99，chat（`use_gpu:false`）→ 0 |

## 7. 本轮修复的两个后端问题（容量防呆与探测超时）

1. **CPU 实例被显存防呆拦下**：`handleStart` 无条件做 `CheckCapacity`，而 `use_gpu:false` 的实例根本不碰显卡。实测启动 CPU chat 时返回 `428 capacity_exceeded`（`estimated 6.09 GB exceeds the -1.40 GB budget`）。修复：只在 `spec.usesGPU()` 为真时做容量预检——防呆必须衡量实例真正消耗的资源。回归测试 `TestStartSkipsCapacityCheckForCPUOnlyInstance`。
2. **chat 探测超时把正常服务报成故障**：`Prober` 的客户端固定 3s，而 chat 在 CPU 上仅 prompt 处理就要 2.4s。实测 probe 返回 `unverified` + `context deadline exceeded`，而同一时刻直接 curl 是成功的。修复：按任务取超时（`TaskType.taskTimeout()`，chat 45s），`handleProbeOne` 的上下文预算从 10s 提到 60s。

## 8. 五项运行期加固（2026-11-01 晚）

前一轮交付后列了 8 条建议，用户批准实施 1–5；第 6 条（supervisor 地址由 `config.js` 注入）涉及 Core 前端，本轮未做；7、8 明确不做——不让 supervisor 写 Core 的模型配置（跨模块静默写入），也不做自动重启自愈（会掩盖显存不足、模型文件损坏这类真实故障）。

| # | 问题 | 改法 |
|---|---|---|
| 1 | `extra_args` 只挡了记住的几个开关，`-c`/`-t` 仍能双写结构化字段 | `supervisorFlags` 改名 `managedFlags` 并补齐 `-c/--ctx-size/-t/--threads`；规则写成「我们代管的全部参数都受保护」 |
| 2 | 启动失败只有退出码，看不出为什么 | `LogBuffer.Tail(n,maxRunes)`；`await` 等 `drain` 收尾（上限 2s）后取最后 3 行拼进 `last_error` |
| 3 | `/list` 的 `reachability` 恒为启动快照 `unverified`，与 `/probe` 的 `loopback` 打架 | `Address` 只剩 `url`；可达性移入 `Health`（带 `checked_at`，未测量时字段不存在）；`ViewOf` 不再写可达性 |
| 4 | 端口被外部占用时仍返回 202，一秒后变 `failed` | `Start` 前 `checkPortFree` 绑定探测；冲突 `409 port_in_use`，且不留下半成品实例 |
| 5 | llama-server 活着但不响应时无人发现 | `Manager.Observe` + `sweep` 每 30s 巡检 `running` 实例；`MODEL_SUPERVISOR_SWEEP_INTERVAL`（0 关闭）；`sweepBudget=60s` |

### 实施过程中暴露的三个缺陷（都不在原计划里）

1. **端点缓存在实例上，重启后全变空**：`instance.endpoint` 只在 `Start` 时赋值，`Load`（从状态文件恢复）不赋值，于是 supervisor 重启后 `/list` 对每个实例都返回 `endpoint.url = ""`。改成不缓存、由 `ViewOf(spec)` 从端口派生——地址是 spec 的纯函数，缓存只会带来过期和缺省两种错误。回归测试断言恢复后的 `endpoint` 等于 `http://127.0.0.1:8081/v1`。
2. **`taskTimeout()` 只对 chat 生效**：`taskClient` 里 `if t != TaskChat { return base }` 直接返回 3s 客户端，于是「给 embedding 更长预算」的改动完全无效，健康服务继续被报成不可达。改成无条件按任务设置超时。回归测试对三种任务逐一断言 `taskClient(t).Timeout == t.taskTimeout()`。
3. **测试套件依赖宿主端口**：默认端口检查会真去 bind 8081，本机正在跑 llama-server 时测试直接失败。测试默认注入空检查（`noPortCheck`），真实绑定由 `TestCheckPortFreeSeesBoundSocket` 单独覆盖——测试不该依赖宿主机此刻在监听什么。

### 真机验证

| 检查 | 结果 |
|---|---|
| 占用 8099 后 `POST /start` | `409 {"code":"port_in_use"}` |
| `extra_args: ["-c","8192"]` / `["--ctx-size=8192"]` / `["-t","2"]` / `["--threads","2"]` / `["--host","0.0.0.0"]` | 全部 `400 invalid_request`（`extra_args must not set a supervisor-owned flag`） |
| `gpu_layers: -1`、`use_gpu:false` + `gpu_layers:10` | 全部 `400 invalid_request` |
| 失败启动的 `last_error` | 带 stderr 尾部（夹具进程 `os.Exit(3)` 前打印 `failed to load model`） |
| supervisor 重启后 `/list` | `endpoint.url` 三个实例都正确（此前的缺陷） |
| 等待两个巡检周期后 `/list` | `embedding: loopback / embedding_dim 2560 / 2ms`；`rerank: loopback / 1ms`；`chat`（stopped）无 `health` 字段 |
| 冷启动探测（实测三次） | `12.27s` → `4.98s` → `0.10s`；3s 预算时前两次必失败，20s 预算后首次即成功 |
| 巡检不改变状态 | 实例 `running` 时探测失败只写 `health.reachability=unverified` + `note`，`status` 保持 `running` |
| `gofmt -l .` / `go build` / `go vet` / `go test ./...` | 无输出 / 0 / 0 / 0 |
| `python scripts/check_module_boundary.py` | 0，317 imports，无 WeKnora 依赖、无启用 workspace |
| `npm run type-check` / `npm test` / `npm run build` | 0 / 2370 用例 0 失败 / 0 |
| Core 端到端检索（改造后） | 7 条，`rerank.applied=true`，`top_score=0.99946`，`candidate_count` 正常 |

## 9. 已知限制

- **日志是 2 秒轮询**，不是推送；面板一次最多保留 500 行。
- **令牌由用户手填**，没有与 `MODEL_SUPERVISOR_TOKEN` 的握手流程；本轮实测用 `smoke-token-123`，正式使用应换成随机值。
- **supervisor 仍不随 `start-all.sh` 启动**（P1 的决定不变）；Core/Host 与它是三套独立进程。
- **未执行 Linux race**：本机 WSL 无 Go 工具链，`go test -race` 无法运行。
- 页面未做端到端浏览器自动化：密码输入被驱动策略拒绝，登录态由用户手工建立。
- `web/` 是构建产物且已被 `.gitignore` 忽略，`npm run build` + 同步 `web/` 会本地覆盖它。
