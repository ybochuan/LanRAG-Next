# AgentRAG 工作规则

本文件是仓库内 Codex 工作的强制约束。规则归本文件，稳定事实归 `PROJECT_CONTEXT.md`，任务及状态归 `TASK_BACKLOG.md`。

## 1. 开始工作与范围控制

1. 开始前读取本文件、`PROJECT_CONTEXT.md`、`TASK_BACKLOG.md`，以及本次任务涉及的更深层 `AGENTS.md`；执行 `git status --short`、`git diff --stat`，识别并保留已有改动和未跟踪文件。
2. 对照 `BASELINE.txt`、`git rev-parse HEAD`、`git describe --tags --always` 核实源码版本；架构依据是 `docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`。版本变化后重新核实 Hook，不沿用旧行号。
3. 每次写入前先说明具体文件和改动内容。用户要求只读扫描时，只读取和报告；不得顺手修复、格式化或生成源码。
4. 只执行用户本轮授权范围内的任务；backlog 中的条目不自动构成实施授权。想采用超出已授权方案的“更好写法”，先说明并取得用户同意。
5. 完成后执行 `git diff --check`、`git status --short --untracked-files=all`、`git diff --name-only`；逐项核对改动范围，报告验证命令、退出状态及未验证项。

## 2. 核心受控区与独立扩展区

1. `internal/` 等上游核心源码是受控区，绝对禁止重构现有核心业务逻辑，禁止修改原生 RRF、检索、Agent、解析和任务调度算法来承载定制功能。
2. 默认只读扫描核心。只有用户明确授权对应 Hook 接入时，才允许在确认过的调用点做极薄 DTO 转换与本机 HTTP 调用；治理、状态机、持久化、重试及策略必须实现在 `extensions/`。用 `git diff -- internal/` 检查实际补丁。
3. `extensions/` 是独立 Go module：`github.com/ybochuan/LanRAG-Next/extensions`。不得 import WeKnora 的 `internal/` 包，不得复制核心业务逻辑或通过 `replace`、`go:linkname` 等方式绕过边界。
4. 上游根 `go.mod` / `go.sum` 不得因扩展模块引入依赖而改变；不得添加把两模块耦合起来的 `go.work`。在 `extensions/` 单独执行 Go 命令，并用 `go env GOMOD GOWORK` 确认模块归属。
5. 在 `extensions/` 执行 `go list -deps -f '{{.ImportPath}}' ./...`，检查依赖不得出现 `github.com/Tencent/WeKnora/internal/`；检查 `go.mod` 中不得出现指向 WeKnora 的 `require` / `replace`。仅根目录的 `go test ./...` 不会覆盖嵌套扩展模块。
6. Source Mapping 中发现的问题按 Source Mapping / Implementation Difference / Baseline Limitation 记录。不得擅自改写 Frozen Architecture 或假设已经完成 Hook 接入。

## 3. 构建、测试和类型检查

以下命令按改动范围执行。Go 声明版本以各模块 `go.mod` 为准；构建本身承担 Go 类型检查。不得为修复环境问题而擅自升级依赖或修改上游。

| 场景 | 工作目录 | 命令／通过标准 |
|---|---|---|
| 扩展格式检查（只读） | `extensions/` | `gofmt -l .`；无输出 |
| 扩展格式修正 | `extensions/` | `gofmt -w` 后接本次修改的具体 Go 文件；不格式化上游 |
| 扩展构建／类型检查 | `extensions/` | `go build -mod=readonly ./...` |
| 扩展测试 | `extensions/` | `go test -mod=readonly ./... -count=1`；无测试文件时明确报告，不声称已有测试覆盖 |
| 扩展静态检查 | `extensions/` | `go vet -mod=readonly ./...` |
| 独立模块门禁 | `extensions/` | `python scripts/check_module_boundary.py`；拒绝 WeKnora require/replace/import、错误 module 身份及启用的 workspace |
| 模块门禁回归 | `extensions/` | `python -m unittest discover -s scripts -p '*_test.py' -v`；在临时真实 Go module 上运行正反案例 |
| 扩展 race 检查 | `extensions/`，具备 C 编译器的 Linux | `go test -mod=readonly -race ./... -count=1`；不能代替真实 SQLite 并发／事务验证 |
| 扩展启动 | `extensions/` | `go run ./cmd/extension-host` |
| 扩展健康探针（另一个终端） | 任意 | `curl.exe --fail http://127.0.0.1:8090/bridge/healthz`；HTTP 200、JSON `status=ok` |
| 已授权核心改动的定向测试 | 仓库根目录 | `go test -mod=readonly ./实际改动包路径 -count=1`；替换为真实包路径 |
| 上游后端构建（PowerShell） | 仓库根目录 | `go build -mod=readonly -o "$env:TEMP/WeKnora.exe" ./cmd/server`；需要可用的 CGO/C 编译工具链 |
| 上游 Lite 构建（Bash + make） | 仓库根目录 | `make build-lite`；需要 CGO、C 编译器、Node/npm，会构建前端并更新 `web/`，仅经授权执行 |
| 上游完整 Go 测试 | 仓库根目录 | `go test -mod=readonly ./... -count=1`；部分包依赖服务与配置，逐项报告失败 |
| 上游 make 测试（Bash + make） | 仓库根目录 | `make test`；还包含 `scripts/docker-entrypoint_test.sh`，不等同于仅运行 Go 测试 |
| 前端依赖安装 | `frontend/` | `npm ci --no-audit --no-fund`；仅在授权前端任务且缺少依赖时执行 |
| 前端测试 | `frontend/` | `npm test` |
| 前端类型检查 | `frontend/` | `npm run type-check` |
| 前端构建 | `frontend/` | `npm run build`；构建不能代替类型检查 |

当任务仅修改扩展或根文档时，执行扩展检查及范围核对；无需启动上游数据库、LLM 或全量前端构建。任何未执行的上游检查必须如实注明。

## 4. 代码风格与验证纪律

1. Go 代码使用 `gofmt`，遵循所在模块现有命名和注释风格；公开类型、字段和函数注明语义。不要为了复用少量代码引入跨模块耦合。
2. DTO 使用显式 JSON 字段名、独立公开类型；时间使用 `time.Time` 并约定传输时间为 UTC。不要把核心模型类型暴露到扩展接口。
3. 只添加当前任务所需功能；禁止顺手整理无关代码、批量改名、调整目录或格式化整个仓库。用 diff 验证每个改动都有对应任务依据。
4. 针对业务行为、边界和故障恢复采用有意义的测试；不得只测实现细节或为简单声明堆叠镜像测试。HTTP 骨架至少实际启动并检查探针和监听地址。
5. “通过”“完成”必须有本轮完整命令及退出状态支持；无测试文件、未运行、依赖缺失、基线失败分别说明，不能互相替代。

## 5. 安全与冻结边界

1. Bridge 默认只绑定 `127.0.0.1:8090`，不得改成 `:8090`、`0.0.0.0` 或公网监听；健康接口不得输出配置、凭据、知识库或文档内容。
2. 四类业务 Hook 后续接入时使用 `X-AgentRAG-Bridge-Version: 1`，不支持的 major version 必须快速拒绝；健康探针不代表业务 Hook 或持久层已就绪。
3. `before_retrieve` 不可用时必须 fail-closed；`after_retrieve` / `before_generate` 后续按冻结方案 fail-open。用户2026-10-30批准M0默认Pass-Through：合法且显式非空的原生授权文档范围返回200，不新增治理排除；复杂过滤、完整active/直接读/图谱门禁后移，不可宣称已生效。原生M0门卫只检查HTTP状态，默认透传对空文档范围必须直接返回非200，不能只返回200+deny_all；不得回退whole-KB。基线未贯通 `excluded_document_ids`，不得只发送字段就当作过滤生效。
4. 用户已确认M0不改原生数据库，以周期对账兜底Lite内存队列。必须上报失败、周期重试，区分reconcile观察与原始事件；不得声称恢复了无法观察的中间历史。扩展事务尚未持久提交时，ingest端点不得返回成功ACK。
5. 扩展标签投影共用单写者队列，当前truth读取、ID解析、读取/合并/替换/回读属于同一队列操作。只修改目标gov命名空间，以KB已解析的原生tag ID写入。原生`tags:null`或缺失数组表示未知，禁止当空数组全量替换；记录失败供Outbox重试（C-10）。用户已接受该安全拒绝并将C-10归为Baseline Limitation；后续独立标签只读API另行接入，不得为追求同步而删除用户标签。队列不保证原生UI/自动标签的并发安全，必须保留该限制，不要求M0擅加上游CAS。
6. 生命周期 event + current + outbox 同事务提交，`last_event_id` 必须有 FK；治理状态与 audit 同事务；critical lineage edge 与相应治理事实同事务。验收需能注入崩溃并检查数据库状态。
7. Revision Publish 采用幂等 Saga，远端结果未知时先 reconcile，再决定是否重试 ingest；不得直接重放副作用。反馈只写指导层，不修改原始知识或事实索引。
8. 投影不是事实源；投影失败不得改变 lifecycle / lineage / governance 的 truth。独立解析代理已由用户明确放到后续阶段，不能混入M0或重写原生解析器。
9. MCP 使用上游原生按需发现；禁止为了挂载扩展重构 `ToolRegistry` 或启用 stdio。mDNS 广播不包含知识库 ID、名称或领域；跨库语义关联需用户明确确认。
10. 不在输出、日志或提交中暴露 `.env`、token、密钥及用户数据。不擅自删除原件、迁移真实数据、执行破坏性命令或提交／推送；保留用户已有改动。
