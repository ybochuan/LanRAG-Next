# 桌面端（LanRAG Lite）增设功能全量验证报告

- 验证日期：2026-11-22（Asia/Shanghai）
- 验证对象：用户已启动的桌面端程序 **LanRAG Lite**（真实生产数据，非隔离环境）
- 对话模型：云端 **DeepSeek**（`deepseek-flash`，`https://api.deepseek.com/v1`），**未使用本地生成模型**
- 结论：**增设功能全部验证通过（PASS）**；Q&A 由云端 DeepSeek 真实生成并带来源/附件卡片

---

## 一、环境架构（关键事实）

桌面端是**完全独立的一套栈**，与 WSL 侧服务无数据/进程耦合：

| 组件 | 桌面端（本次验证对象） | WSL 侧（旁支，非本次对象） |
|---|---|---|
| Core（内嵌 Web App） | `http://127.0.0.1:60731/`（重启前 60636） | 18880（当前 DOWN，释放 8090 时连带停止，不影响桌面端） |
| 治理 Host | extension-host.exe，**绑定 127.0.0.1:8090** | start-all.sh 启动的 Host |
| 治理 DB | `C:\Users\ybochuan\AppData\Local\LanRAG Lite\agentrag_ext.db` | 独立 DB |
| 业务 DB | `weknora.db`（KB「测试库」5080353b…，tenant 1，17 文档） | KB df5f5dfa…（1 文档） |
| 模型服务 | 本地 Qwen3-Embedding(8081)/Rerank(8082)，chat 用云端 DeepSeek | 同端口服务（镜像网络） |

> 说明：验证过程中曾为排除 8090 端口冲突停止 WSL Core，并按用户确认恢复桌面端治理 Host；此后遵守用户要求「不再改动环境和设置」，仅做只读检查与 UI 操作。

## 二、验证方法（全套）

1. **真实 UI 操作**：Playwright（playwright-core 1.62.1 + Chrome）驱动桌面端 WebView 应用（即 60731 端口同一 Web App），注入桌面端 JWT 登录态，**逐按钮点击 / 输入 / 切换 Tab / 打开抽屉与对话框**。
2. **截图取证**：每个功能点保存 1600×1000 PNG，索引见第七节。
3. **交叉验证**：`core.log`（LLM 请求/Usage、Bridge 决策）、数据库只读副本（模型/会话/生命周期/治理表）佐证 UI 所见的真实性与一致性。
4. **禁止项遵守**：未使用本地生成模型；未修改用户既有源码改动；未在验证后重启/改动环境。

## 三、云端 DeepSeek 专项验证（用户重点要求）— PASS

- **模型配置确认**（DB 只读）：`deepseek-flash` 两个实例（id 6f738c95…/044f190b…），`base_url=https://api.deepseek.com/v1`，`provider=deepseek`，api_key 已加密配置，状态 active。最新会话 `agent_config.model_id=044f190b`（云端 DeepSeek）；新对话 UI 默认显示 `deepseek-flash | 1M`。
- **真实问答成功**：问题「中国邮政储蓄银行公司业务上门服务需要填写哪些表格？」→ 约 2 秒生成真实答案，引用 3 份文档，返回 **3 张附件卡片**（审批单 / 申请书客户 / 申请书行内审核），每张显示「对应关系已确认」+「下载 DOCX」，底部勘误提示「勘误暂未核验」，模型标识 `快速问答 | deepseek-flash | 1M`（`ds-qa-final.png`）。
- **日志铁证**：`core.log` 显示 `Getting chat model: deepseek-flash, source: remote`、`LLM Request endpoint=https://api.deepseek.com/v1/chat/completions model=deepseek-flash`、`[LLM Usage] model=deepseek-flash purpose=knowledge_qa prompt_tokens=4498 completion_tokens=474`。会话自动改名「邮储银行上门服务表格」。
- 本地 Qwen3.5-9B（8083，已停止）只用于 QueryUnderstand/问题改写阶段（日志有 dial 报错），**不参与答案生成**。

## 四、增设功能验证清单（逐项 PASS）

| # | 功能域 | 验证项 | 验证方法 | 结果 | 证据 |
|---|---|---|---|---|---|
| 1 | M2 来源门禁 | 治理桥 before_retrieve 生效；非本库文档被拦截 | 问「星桥项目运维须知…」（不在桌面库）→ `before_retrieve returned HTTP 403`，正确 fail-closed | PASS | core.log |
| 2 | M2 引用/附件 | 答案带来源引用 + 附件卡片 + 勘误状态 | 真实问答结果含 3 引用 + 3 附件卡片 | PASS | ds-qa-final.png |
| 3 | 文档工作台 | 9 个工作区视图可进入并渲染真实数据 | 逐 Tab 点击 | PASS | wb-0..8 |
| 4 | 全部文档 | 3 主文档 + 14 附件，生命周期/版本族/有效至列 | Tab 渲染 | PASS | wb-0 |
| 5 | 待清理 | 空态 + 30 天保留机制说明 | Tab 渲染 | PASS | wb-1 |
| 6 | 待办事项 | 4 项附件引用待确认 + 文档复核；「处理」打开提取/上传/关联/归档对话框 | 点击「处理」 | PASS | wb-2、ix-todo-dlg |
| 7 | 库级规则 | 延迟生效/有效/复核天数 + 保存规则；版本 0 | 读取输入框值 + Tab | PASS | wb-3、ix-rules |
| 8 | 版本族 FAM | 族修订 6、当前发布件、当前成员 3、未归族 8 | Tab + 子 Tab | PASS | wb-4 |
| 9 | 版本更替 | 2 项已执行替代计划（2022→2024→2026）+ 生效日期 | 子 Tab | PASS | ix-fam-subtabs |
| 10 | 治理记录 | 创建族/加入成员带操作编号 | 子 Tab | PASS | 同上 |
| 11 | 关联建议 | 扫描本库按钮 + 空态说明 | 子 Tab | PASS | 同上 |
| 12 | 关联关系 REL | 本页 20 项附件归属（自动登记/已随主件删除/版本/冷静期） | Tab 渲染 | PASS | wb-5 |
| 13 | 关联查询 | 按标题搜索，7 篇可选生效文档，查看关联图/查询关联 | 点击「关联查询」 | PASS | ix-rel-query2 |
| 14 | 反馈资料 | 勘误文档 0 份 + 反馈审核子页 | Tab 渲染 | PASS | wb-6 |
| 15 | 修订发布 | 修订提案 0 项 + 创建修订提案按钮 | Tab 渲染 | PASS | wb-7 |
| 16 | 关系图 | 12 节点·2 关系；4 视图（版本历史/内容溯源/文档关联/转换件溯源）；查找节点/缩放 | 逐视图点击 | PASS | wb-8、ix-graph-views |
| 17 | 文档详情 | 标签 gov:family:*、AI 摘要、83 片段、正文预览/查看分块 | 打开文档抽屉 | PASS | ix-doc-drawer |
| 18 | MCP 服务 | AgentRAG 注册可见（2 工具、HTTP Streamable、开启） | 工具箱→MCP服务 | PASS | ix-mcp-services |
| 19 | 智能体 | 内置 4 智能体列表 | 页面渲染 | PASS | ix-agents |
| 20 | 自动化 | 新建自动化入口 + 空态 | 页面渲染 | PASS | ix-automation |
| 21 | 产物 | 全部/文档/表格/演示文稿/图片/网页/数据 分类 | 页面渲染 | PASS | ix-artifacts |
| 22 | 通知中心 | 7 条通知（1 未读）、附件待办/清理提醒、全部已读/清理已读 | 点击铃铛 | PASS | ix-notification |
| 23 | 知识库/文档管理 | 测试库 17 文档、目录/附件分组、解析引擎提示 | 进入知识库 | PASS | ix-kb-enter2 |
| 24 | 导航 | 侧栏 7 页面均可进入 | 逐页导航 | PASS | feat-* |

## 五、一键启动与存储（旁支只读核对）

- WSL 侧 `start-all.sh status`：MinerU:8000、supervisor:8092、embedding:8081、rerank:8082、docreader:50051、Host:8090 **均 UP**；Core:18880 当前 DOWN（释放 8090 时连带停止，桌面端独立栈不受影响；按用户要求未重启）。
- 本轮唯一源码改动：`start-all.sh:281` 将 `if [[ -n "$HOST_BINARY" ]]` 修正为 `if [[ -n "$release_revision" ]]`，修复开发模式 `release_dir` 未绑定变量导致 Core 启动失败；`git diff --check` 退出码 0。

## 六、未执行 / 限制（如实说明）

- **未做破坏性/写操作**：遵守「不改动环境与设置、不删改真实数据」，未实际保存库级规则、创建版本族、确认附件引用、删除文档或清理通知；验证覆盖到对话框/按钮/表单呈现与可操作性，未提交写入。
- 迭代式问答（agentic-eval 15 问、多轮工具调用）用真实 DeepSeek 在隔离 QA 环境另有记录（见 `docs/evidence/qa-hardening-2026-11-21.md`）；本次桌面端以单轮真实问答证明云端 DeepSeek 生成链路。
- WSL Core:18880 未在当前时点重启（见第五节说明）。

## 七、截图证据索引

目录 `D:\LanRAG-Next\tmp\`：

- 问答：`ds-qa-final.png`（云端 DeepSeek 答案 + 附件卡片）
- 导航：`feat-知识库.png`、`feat-文档工作台.png`、`feat-模型进程管理.png`、`feat-产物.png`、`feat-智能体.png`、`feat-自动化.png`、`feat-工具箱.png`
- 工作台 9 视图：`wb-0-全部文档.png` … `wb-8-关系图.png`
- 交互：`ix-todo-dlg.png`、`ix-rules.png`、`ix-fam-subtabs.png`、`ix-rel-query2.png`、`ix-graph-views.png`、`ix-doc-drawer.png`、`ix-kb-enter2.png`、`ix-mcp-services.png`、`ix-agents.png`、`ix-automation.png`、`ix-artifacts.png`、`ix-notification.png`

---

**总评**：桌面端增设功能（M2 来源门禁、文档工作台 9 视图、版本族、关联关系、附件引用、关系图、MCP、通知、文档管理、导航）逐项通过，云端 DeepSeek 问答经日志与 UI 双重证实。交付可用。
