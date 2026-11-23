# 桌面端 B2 下载记录与通知闭环验证报告

- 验证日期：2026-11-23（Asia/Shanghai）
- 验证对象：安装版 **LanRAG Lite**（Wails 桌面程序，真实生产数据，非隔离环境）
- 知识库：测试库 `5080353b-55a2-41f9-9759-869ec14ddd72`（tenant 1，17 文档）
- 结论：**B2 下载记录 + Core `download.completed` 通知闭环通过（PASS）**

---

## 一、环境与对象

桌面端是独立栈，和 WSL 侧服务不共享数据库或进程：

| 组件 | 实际值 |
|---|---|
| 安装目录 | `C:\Users\ybochuan\AppData\Local\Programs\LanRAG Lite` |
| 桌面可执行文件 | `LanRAG Desktop.exe`（454,042,658 bytes） |
| 桌面 Web 资源 | 安装目录下 `resources/web`（由 `frontend/dist` 24 MB 部署） |
| Core（内嵌 Web App） | `http://127.0.0.1:52882`（启动时随机端口，以 `core.url` 为准） |
| 治理 Host | `extension-host.exe`，绑定 `127.0.0.1:8090` |
| 治理 DB | `C:\Users\ybochuan\AppData\Local\LanRAG Lite\agentrag_ext.db` |
| 业务 DB | `weknora.db`，知识库「测试库」`5080353b-55a2-41f9-9759-869ec14ddd72` |
| 对话模型 | 云端 DeepSeek `deepseek-flash`（`https://api.deepseek.com/v1`）；不使用本地生成模型 |

> 说明：本次只做桌面端只读检查与 UI 操作，不修改用户既有环境或设置；WSL 侧旁支服务未重启，状态仅作旁证。

## 二、B2 实现范围

- 桌面端业务文档/附件/产物下载：Wails 原生「保存文件」对话框 + Go 写文件 + 本地 `downloads.json` 记录。
- 桌面端 Core 新增 `download.completed` 通知，铃铛「下载记录」展示本地记录；Core 通知历史以 read-only 方式保存。
- 浏览器/无原生 bridge 场景：走浏览器下载，不写本地记录；`isManagedDownloadAvailable()` 仅在 `isPackagedDesktop() && nativeBridge()` 时为真。
- 文件上限：256 MiB；超过上限走浏览器回退且不记录（源码契约测试覆盖）。

关键源码位置：

- `frontend/src/components/GlobalInvitationBell.vue:142`：下载工具导入
- `frontend/src/components/GlobalInvitationBell.vue:151-153`：桌面端隐藏 Core 重复历史，浏览器保留
- `frontend/src/utils/desktopDownload.ts:60-63`：`isManagedDownloadAvailable`
- `cmd/desktop/downloads.go:369-399`：`OpenBusinessDownload` / `RevealBusinessDownload`

## 三、验证方法

1. **真实桌面 UI**：pyautogui 驱动安装版 WebView2 窗口（Wails/WebView2 屏蔽了 CDP，`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` 被 go-webview2 的 `preventEnvAndRegistryOverrides()` 清空），点击知识库、文档详情、下载图标、原生保存对话框、铃铛。
2. **Web 页面交叉验证**：Playwright 1.62.1 + Chrome 驱动桌面 Core 的同源 Web App，注入桌面 JWT 后检查通知面板。
3. **API 实测**：用桌面 JWT 调用 Core `/api/v1/notifications` 与 `/api/v1/notifications/download`，检查幂等与 `read_at`。
4. **本机文件与 DB 只读**：核对 `downloads.json`、保存文件大小、`core.log`、`agentrag_ext.db` 记录。

## 四、B2 验收清单

| # | 验证项 | 方法 | 结果 | 证据 |
|---|---|---|---|---|
| B2-01 | 文档详情下载弹出原生保存对话框 | 点击第一份文档抽屉右上下载图标 | PASS | `lite-save2-dialog.png` |
| B2-02 | 文件真实落盘、大小正确 | 保存到 `D:\LanRAG-Next\tmp\b2-downloads\b2-doc-test.docx`，核对 42,486 bytes | PASS | 文件系统 |
| B2-03 | `downloads.json` 记录字段完整 | 核对 id/文件名/size/sourceType/sourceId/sourceTitle/link/createdAt/syncState | PASS | `downloads.json` |
| B2-04 | 桌面铃铛显示「下载记录」 | 点击铃铛，显示文件名、来源、41 KB、已保存到本机、打开文件/所在文件夹 | PASS | `lite-bell.png`、`lite-bell2.png` |
| B2-05 | Core 创建 `download.completed` | `GET /api/v1/notifications` 从 7 条增至 10 条；3 条为 download.completed | PASS | 接口返回 |
| B2-06 | 通知为 read-only、不占未读 | 新通知 `read_at` 自动设置；未读数为其他待办 | PASS | 接口返回 |
| B2-07 | 打开文件 | 点「打开文件」后 Word 打开 `b2-doc-test.docx [兼容模式]` | PASS | `lite-openfile.png` |
| B2-08 | 打开所在文件夹 | 点「打开所在文件夹」后 Explorer 打开 `b2-downloads` | PASS | `lite-reveal.png` |
| B2-09 | 下载通知幂等 | 同一 body 两次 POST `/notifications/download`，第二次 `duplicate:true`、id 相同 | PASS | 接口返回 |
| B2-10 | 重建后原生下载路径仍有效 | 重新 build/deploy 后再次下载 `b2-doc-test2.docx`，42,486 bytes，`syncState:synced` | PASS | `downloads.json` |
| B2-11 | 重建后桌面铃铛显示 2 条 | `下载记录 2`：b2-doc-test2.docx / b2-doc-test.docx | PASS | `lite-bell2.png` |
| B2-12 | 普通浏览器显示 Core 下载历史 | Playwright：`.notification-panel__downloads=0`，`.notification-panel__history .notification-item=9`，首条为 b2-doc-test.docx/文件已下载 | PASS | `web-bell2.png` |
| B2-13 | 桌面端隐藏 Core 重复历史 | 桌面铃铛只显示本地「下载记录」，不重复显示 Core 历史行 | PASS | `lite-bell2.png` |
| B2-14 | 256 MiB 上限与浏览器回退 | `desktopDownload.test.ts` 源码契约测试：256 MiB cap / browser fallback | PASS（契约测试） | 测试输出 |
| B2-15 | 无 native bridge 时不写本地记录 | 浏览器环境 `isManagedDownloadAvailable()=false`，下载走 Web 回退 | PASS（实现/契约） | `desktopDownload.test.ts` |

## 五、本轮测试与构建结果

- `npm run type-check`：通过（无诊断输出，命令继续执行到测试阶段）。
- `npm run build`：通过，Vite 49.12s，`EXIT:0`；只有既有大 chunk 警告。
- `npm test`：2492 tests / 2487 pass / 3 fail / 2 skip；3 个失败均为既有 Windows POSIX shell 环境问题：`Error: POSIX shell tests require the Git for Windows usr/bin shells`（`frontend/src/test-utils/posixShell.ts:15:11`）。
- `desktopDownload.test.ts`：4 条源码契约测试全部通过（256 MiB cap/browser fallback、packaged native bridge、link/source_id sanitize + createDownloadNotification + MarkSynced、2 MiB chunks + Abort）。
- 定向 Go 测试：`go test -mod=readonly ./internal/application/service ./internal/handler -run "TestNotifyDownloadOnceIsIdempotentAndReadStateInsensitive|TestCreateDownloadNotificationIsIdempotent" -count=1 -v` → **PASS**（两个 B2 测试均通过，EXIT:0）。
- 相关 Go 包全量测试：`go test -mod=readonly ./internal/application/service ./internal/handler -count=1` → **FAIL**（失败集中在既有 Python skill verifier、knowledge download、data source cron 等无关用例：`tenant_skill_verify_python_test.go`、`knowledge_download_test.go`、`TestDataSourceScheduleAPIRejectsInvalidCron` 等；B2 两个定向测试通过，但不得把全包失败说成全绿）。

## 六、最终状态

- `downloads.json` records：2
  - `b2-doc-test2.docx`（`dl-300e9e52ac6518b72e62ce27af472a11`，synced）
  - `b2-doc-test.docx`（`dl-44ddb60082553864f10cec0086ddddaa`，synced）
- Core 通知：10 条；最新两条为上述两次下载，read_at 分别为 `2026-11-23T01:39:35.8976351+08:00`、`2026-11-23T01:26:41.0568755+08:00`。
- 桌面端 Core：`http://127.0.0.1:52882`，`/health` 返回 200 `{status:"ok"}`；Host `127.0.0.1:8090`，`/bridge/healthz` 返回 200 `{status:"ok"}`。

## 七、未执行 / 限制

- 未用真实超过 256 MiB 的文件做现场下载；该分支由 `desktopDownload.test.ts` 的源码契约测试覆盖。
- 未在无头浏览器中实际完成一次浏览器下载（会触发浏览器保存行为）；无 native bridge 的回退路径由实现检查与单元测试覆盖。
- `npm test` 的 3 个失败是既有环境问题，不是本次 B2 改动引入；命令退出码为 1，必须如实记录。
- `go test` 的全包运行存在既有无关失败（见第五节）；B2 的两个定向测试已单独通过，但不得把全包失败说成全绿。
- WSL 侧生产旁支未在本次重启；当前只读探测显示 MinerU/supervisor/embedding/rerank/docreader/Core:18880 DOWN，Host:8090 由桌面 `extension-host` 占用。桌面端是本次 B2 验证对象，且与 WSL 侧独立。

## 八、截图索引

目录 `D:\LanRAG-Next\tmp\`：

- `lite-save2-dialog.png`：原生保存文件对话框
- `lite-bell.png`：首次下载后的桌面铃铛「下载记录 1」
- `lite-bell2.png`：重建后桌面铃铛「下载记录 2」
- `lite-openfile.png`：Word 打开下载文件
- `lite-reveal.png`：Explorer 定位下载目录
- `web-bell2.png`：普通浏览器通知历史显示 `download.completed`

## 九、与既有全量报告的关系

既有桌面端 24 项增设功能清单见 `docs/evidence/desktop-full-verification-2026-11-22.md`（M2 来源门禁、文档工作台 9 视图、版本族、关联关系、附件引用、关系图、MCP、通知、知识库/文档管理、导航）。本轮 B2 改动仅涉及下载记录与通知过滤；重建后已回归知识库进入、文档详情、下载、铃铛与 Web 通知历史，未发现旧功能回退。

## 十、追加验收：下载完成 Toast + 铃铛本地未读角标（2026-11-23 晚）

- 构建：`LanRAG Desktop.exe` revision `fd013c2310e048755ced3ab3fcd858f0e288f01efe6cb762869ae5a7a3c6ec95`（454,046,367 bytes）；安装版 Core `http://127.0.0.1:57086`，PID LanRAG Desktop 29300 / LanRAG Lite 16184。
- 方案：`DownloadRecord.Unread` 写入本地 `downloads.json`；管理下载成功后 `MessagePlugin.success('notifications.downloadsSaved')` 显示 3 秒 toast；铃铛 `badgeCount = pendingCount + unread + desktopDownloadUnread`；打开面板调用 `MarkBusinessDownloadsSeen` 并本地清零。
- 说明：旧记录缺少 `unread` 字段按已读处理；`unread:false` 因 `omitempty` 不写回文件。

| # | 验证项 | 方法 | 结果 | 证据 |
|---|---|---|---|---|
| D-01 | 下载前角标基线 | 登录测试库，铃铛显示既有 Core 未读 1 | PASS | `lite-toast-badge-start.png` |
| D-02 | 真实下载产生 Toast | 打开《中国邮政储蓄银行公司业务上门服务管理办法（2026年修订版）》详情，点下载，保存为 `toast-badge-test.docx`；1 秒内截图显示绿色成功 toast「已保存到本机：toast-badge-test.docx」 | PASS | `lite-toast-visible.png` |
| D-03 | Toast 自动消失 | 4 秒后截图 toast 已消失，角标仍为 2 | PASS | `lite-toast-after.png` |
| D-04 | 角标 +1 | 同一截图角标由 1 增至 2（本地未读 1 + 既有 Core 未读 1） | PASS | `lite-toast-visible.png` / `lite-toast-after.png` |
| D-05 | 打开铃铛清零本地未读 | 点铃铛，面板「下载记录 5」首条为 `toast-badge-test.docx`（来源标题、41 KB、已保存到本机、打开文件/打开所在文件夹）；角标降为 1 | PASS | `lite-bell-panel.png` |
| D-06 | 关闭面板后不反弹 | 点面板外关闭后角标保持 1（本地记录已持久化为已读） | PASS | `lite-bell-closed.png` |
| D-07 | 文件与本地记录 | 文件 42,486 bytes；`downloads.json` 首条 id `dl-f707947ae796958d35750f7df9e3909d`、syncState=synced、无 `unread` 字段（false） | PASS | 文件系统 / `downloads.json` |
| D-08 | Core 通知落库 | `weknora.db.notifications` 首条 id 与下载记录一致，kind=`download.completed`、title=`toast-badge-test.docx`、read_at=created_at=`2026-11-23 20:32:40.9576535+08:00` | PASS | 本地 DB 只读查询 |

### 本轮门禁

- `npm run type-check`：退出 0。
- `npm run build`：退出 0（Vite 44.55s）；产物已部署到安装目录 `resources/web`，入口 `/assets/main-Bn5khWtz.js`。
- 定向前端测试：`npx tsx --test src/components/GlobalInvitationBell.test.ts src/utils/desktopDownload.test.ts` → 17 tests 全 pass。
- `npm run check-i18n`：15 tests 全 pass，六语键集一致。
- `npm test`：2496 tests / 2491 pass / 3 fail / 2 skip；3 个失败均为既有 Windows POSIX shell 环境问题（`frontend/src/test-utils/posixShell.ts:15:11`），非本次改动引入。
- Go 定向测试：`go test -mod=readonly ./cmd/desktop -run "Test(FinishBusinessDownloadMarksRecordUnread|MarkBusinessDownloadsSeenOnlyClearsRequestedRecords|LegacyDownloadRecordWithoutUnreadIsRead)$" -count=1` → PASS（ok 0.695s）。
- `go test -mod=readonly ./cmd/desktop -count=1`：仅 4 条既有 Windows 偏好路径测试失败（`prefs_test.go:37/45/68/107`），本次新增 3 条下载测试通过。

### 截图索引（追加）

- `lite-toast-badge-start.png`：下载前角标 1
- `lite-toast-visible.png`：成功 toast 与角标 2
- `lite-toast-after.png`：toast 消失后角标仍为 2
- `lite-bell-panel.png`：铃铛「下载记录 5」首条新下载
- `lite-bell-closed.png`：关闭面板后角标 1
