# 文档工作台轻量 authority 端点交付（2026-11-17）

## 背景

11-13 工作台审计的 P2-2 指出：`BusinessView` 与 `FamilyWorkspaceView` 仅为获取 `can_write` 就调用完整 `overview`，后者会连带查询 counts、文档分页、annotations 与 rules，在大库上是明显浪费。审计时标记未做；本轮完成。

## 实现

- **后端**（受控区薄接线）：新增 `GET /api/v1/knowledge-bases/:id/lifecycle/authority`，复用 `ExtensionLifecycle` 的 Viewer/KB 授权；handler 识别 FullPath → `action="authority"`；`extensions/management/server.go` 的 `execute` 增加 `case "authority"`，**零 DB 查询**，只回传 handler 已算好的 `can_write` 与作用域。
- **前端 API**：`frontend/src/api/lifecycle.ts` 新增 `authority(kb)`，类型含 `can_write/tenant_id/knowledge_base_id`。
- **调用方切换**：`BusinessView.loadAuthority()` 与 `FamilyWorkspaceView.accessPromise` 均从 `overview(scope,1,'','',1)` 改为 `authority(scope)`。BusinessView 原先搭便车用 overview 的 documents 填标题，已移除——标题由 `loadRecords()` 的 `loadRecordTitles` 正常加载。

## 验证

- 新增组件契约测试：断言两个视图调用 `authority(scope)` 且不再出现 `overview(scope,1`，防止回退。
- `npm run type-check` 退出 0；相关前端测试 **27/27 PASS**；`npm run build` 退出 0。
- 扩展 `go build ./...`、Core `go build ./internal/handler/ ./internal/router/` 均退出 0。
- **生产验证**：`GET .../lifecycle/authority` 返回 200、`can_write: true`、**仅 3 个字段**；同库 `overview` 返回 9 个字段，两者 `can_write` 一致。新前端产物已部署到生产 `web/`（备份 `web-backup-20261117-192207`），服务 chunk 200。
- 回归：真实 `/knowledge-chat` 提问返回 1036 字中文回答并带 `<kb>` 引用，问答链路未受影响。

## 效果

权限读取从"1 次 overview（counts + 分页 + annotations + rules 多表查询）"降为"1 次零 DB 的 DTO 回传"，响应字段从 9 个降到 3 个。对每次工作台刷新都触发的权限检查，这是纯收益且无语义变化。

## 边界

只优化权限读取；overview 本身仍保留完整数据供需要统计/列表的视图使用。未改生产业务数据。
