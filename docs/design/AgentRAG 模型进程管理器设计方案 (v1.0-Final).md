# AgentRAG 模型进程管理器设计方案 (v1.0-Final)


## 1. 定位与目标

在 `extensions/` 下构建一个轻量级的模型进程管理器及独立控制台。允许用户通过 WeKnora 界面一键拉起本地 `llama-server.exe` 加载 `.gguf` 模型，并实时监控健康度、运行日志与系统资源。

**工程红线**：

- 只在 `extensions/` 下写代码，**绝对不碰 `internal/` 核心**。
- 绝对**禁止使用 CGO**，纯用 Go 标准库 `os/exec` 拉进程。
- 前端页面作为 `extensions/ui/` 插件挂载，严禁直接修改原生 Vue 页面代码。

## 2. 目录与环境规范

```text
D:\LanRAG-Next\
├── extensions\
│   ├── models\             # Go 后端（进程管理、资源监控 API）
│   └── ui\                 # 前端 Vue 组件（本地模型控制台）
├── bin\                    # llama-server.exe 及所有依赖 DLL
└── data\models\            # .gguf 模型文件
```

## 3. 核心数据结构 (Go 后端)

```go
type ModelConfig struct {
    ID         string   
    BinaryPath string   
    ModelPath  string   
    Port       int      // 前端可传 0，表示由后端自动分配空闲端口
    TaskType   string   // "embedding" / "reranker"
    UseGPU     bool     // -ngl 99 / -ngl 0
    CtxSize    int      // --ctx-size
    Threads    int      // --threads
    ExtraArgs  []string 
}

type ModelInstance struct {
    Config          ModelConfig
    Status          ModelStatus // stopped, starting, running, error
    Pid             int
    Port            int         // 后端实际分配并启动的端口
    StartedAt       time.Time
    LoadingProgress int         // 加载进度 (0-100)
    HealthLatencyMs int64
    LastError       string
    ExitCode        int
    LogBuffer       []LogLine   // 环形缓冲，支持去重
    cmd             *exec.Cmd
    mu              sync.Mutex
}

type LogLine struct {
    Content string
    Count   int // 用于日志智能去重
}
```

## 4. WeKnora 侧边栏注入设计

不重写原生 `Sidebar.vue`，通过菜单状态和路由实现**插件化注入**。

### 4.1 侧边栏视觉布局

```text
┌───────────────────────────────┐
│  🟢 WeKnora / LanRAG-Next     │
│                               │
│  💬 聊天 (Chat)               │
│  📚 知识库 (Knowledge)        │
│  🤖 智能体 (Agents)           │
│                               │
│  ─────────────────────────    │  <-- 原生分隔线
│                               │
│  🖥️ 本地模型引擎 (NEW)        │  <-- 👈 插件注入菜单 (CpuIcon)
│                               │
│  ─────────────────────────    │
│                               │
│  ⚙️ 设置 (Settings)           │
└───────────────────────────────┘
```

### 4.2 代码落位与 PATCH 纪律

**新增页面 (定制区)**：创建 `extensions/ui/views/ModelSupervisor.vue`。
**菜单/路由注入 (PATCH)**：在原生 `useMenuStore.ts` 和 `router/index.ts` 中添加配置，严格标记 `// PATCH:`。

## 5. UI 主页面与控制台设计

### 5.1 主页面 (Dashboard) 设计

顶部为资源仪表盘，下方为实例卡片。**卡片内部直接展示 API URL 并支持一键复制。**

```text
┌────────────────────────────────────────────────────────────────────────┐
│  ⚙️ 本地模型引擎                                  [+ 启动新模型]       │
├────────────────────────────────────────────────────────────────────────┤
│  💻 宿主机资源实时监控:                                                │
│  ├─ 🧠 内存: [██████░░░░] 12.4 GB / 32.0 GB                            │
│  ├─ 🎮 显存: [█████████░] 6.8 GB / 8.0 GB (RTX 4060 Laptop) ⚠️接近满载 │
├────────────────────────────────────────────────────────────────────────┤
│  🟢 Qwen3-Embedding-4B (Embedding)                                     │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ 状态: Running (PID: 14820) | 端口: 8001 | 已运行: 1h 15m         │  │
│  │ 健康度: 200 OK (8ms) | 设备: GPU | 上下文: 8192                  │  │
│  │                                                                  │  │
│  │ 🔗 API 接入地址:                                                 │  │
│  │ http://127.0.0.1:8001/v1                                 [📋 复制] │  │
│  │                                                                  │  │
│  │ [查看日志] [停止]                                                │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                        │
│  🟡 Qwen3-Reranker-0.6B (Reranker)                                     │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ 状态: Starting | 端口: 8002 | 正在加载 GGUF...                   │  │
│  │ 加载进度: [████████░░░░░░░░] 45%                                 │  │
│  │ 🔗 API 接入地址: 等待服务就绪...                                 │  │
│  │ [强制停止]                                                       │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

### 5.2 启动配置抽屉 (右侧滑出)

```text
┌───────────────────────────────────────────────┐
│ 🚀 启动新模型                           [关闭]│
├───────────────────────────────────────────────┤
│ 1. 模型选择                                   │
│ 模型文件: [ Qwen3-Reranker-0.6b-q8_0.gguf ▼]  │
│ 任务类型: [ 重排 Reranker ▼ ]                 │
│ 服务端口: [ 0 (自动分配) ]                    │
│ 2. 运行参数                                   │
│ 运行设备: [ 🖥️ CPU (不占用显存) ▼ ]           │
│ 上下文长度: [ 4096 ▼ ]                        │
│ CPU 线程数: [ 6 ]                             │
│ 3. 显存预估                                   │
│ 💡 预估占用: 0 MB 显存 / 约 1.2 GB 内存       │
│ [ 🚀 确认启动 ]                               │
└───────────────────────────────────────────────┘
```

### 5.3 日志控制台 (底部上拉面板)

```text
┌───────────────────────────────────────────────┐
│ 📜 llama-server 实时日志 (PID: 14820)         │
├───────────────────────────────────────────────┤
│ [22:15:01] [INFO] loading model...            │
│ [22:15:03] [INFO] offloading 32 layers to GPU │
│ [22:15:10] [ERROR] CUDA OOM (重复 5 次)       │
└───────────────────────────────────────────────┘
```

## 6. 显存防呆与资源预检机制 (Safety Margin)

在 `POST /start` 接口中，加入严格的启动前预检：

1. **获取资源**：执行 `nvidia-smi` 获取当前空闲显存。
2. **预留安全水位**：实际可用显存 = `当前空闲显存 - 1.5 GB`（留给系统、KV Cache 和 WSL）。
3. **预估占用**：`预估显存 = 模型文件大小(GB) + (CtxSize / 1024) * 0.15GB`。
4. **防呆拦截**：如果 `预估占用 > 实际可用显存`，返回 `428 Precondition Required`。
5. **前端弹窗**：提示“显存不足，建议切换 CPU 模式或减少上下文长度”，用户可选择 `强行启动` 或 `取消`。

## 7. 后端启动与探活流程

1. **动态端口分配**：如果前端传 `Port = 0`，后端通过 `net.Listen("tcp", "127.0.0.1:0")` 申请空闲端口，将实际端口回写给前端。
2. **异步拉起**：`cmd.Start()` 后，接口立即返回 `202 Accepted`，防阻塞。
3. **设置工作目录**：必须设置 `cmd.Dir = filepath.Dir(config.BinaryPath)`，防止找不到依赖 DLL。
4. **健康探针**：后台轮询 `http://127.0.0.1:{Port}/health`，记录延迟。成功转 `Running`，超时（180s）转 `Error` 并强杀。

## 8. 日志智能提取与去重机制

在后台读取 `stdout`/`stderr` 时：

1. **进度提取**：使用正则 `r"loaded (\d+)/(\d+) tensors"` 解析加载进度，更新 `ModelInstance.LoadingProgress`，推送给前端展示进度条。
2. **智能去重**：遇到连续重复的报错（如死循环的 CUDA error），将日志合并为 `LogLine{Content: err, Count: N}`，防止拖垮 Extension Host 和前端渲染。

## 9. 进程终止与防僵尸（保命逻辑）

1. **常规停止**：`cmd.Process.Kill()`。
2. **Windows 进程树兜底**：调用 `exec.Command("taskkill", "/PID", pid, "/T", "/F")` 强杀，防止显存泄露。
3. **宿主退出清理**：监听 `SIGTERM`，退出前遍历清理所有运行的 ModelInstance。

## 10. API 交互与 WeKnora 联动

### 10.1 扩展 API 契约

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/extensions/models/host-resources` | 获取显存/内存实时状态 |
| `GET` | `/extensions/models/list` | 返回实例列表、健康度、加载进度 |
| `POST` | `/extensions/models/start` | 执行防呆预检，异步拉起 |
| `POST` | `/extensions/models/stop` | 强杀进程返回 |
| `GET` | `/extensions/models/{id}/logs` | 获取去重后的日志尾 |

### 10.2 零侵入接入机制

模型状态变为 `Running` 后，UI 卡片直接展示 `http://127.0.0.1:{Port}/v1`。
点击 `[📋 复制]`，前端调用剪贴板，并弹出提示：“已复制！请前往左侧「设置 - 模型」点击添加并粘贴该地址。”
**彻底放弃跨模块 API 静默写入，保证核心数据绝对安全。**

## 11. 前端分层纪律 (防坑红线)

1. **禁止修改原生 Views**：所有页面代码必须在 `extensions/ui/` 下。
2. **路由极薄注入**：在 `frontend/src/router/index.ts` 与 `useMenuStore.ts` 添加配置（严格标记 `// PATCH:`）。
3. **样式隔离**：使用 TDesign 组件，但必须使用独立的 CSS class 前缀，避免污染全局。

