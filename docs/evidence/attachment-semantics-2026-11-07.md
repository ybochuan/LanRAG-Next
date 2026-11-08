# 附件语义识别与正文引用映射（ATTACH-07A）

日期：2026-11-07
状态：代码、测试、生产部署与回填已完成（2026-11-07）；登录态聊天 UI 已目视验收（2026-11-08）。

## 范围

按用户选择先交付 ATTACH-07A 最小闭环：DOCX/Office 附件真实标题识别、主文档正文附件引用映射、扩展库语义存储、Bridge DTO 可选字段、聊天卡片语义展示与现有附件回填。管理端映射修正、PDF/PPTX 文件级标题识别留待后续。

## 实现

- `extensions/parser/attachment/semantic.go`
  - `Analyze(parentName, parentData, parentTitle, files)` 返回 `[]Semantic`。
  - DOCX 父文档：解析段落/标题样式与“附件N”行、`《...》`引用、附件编号和最近章节；附件标题取文档首个有效标题段落（支持多段拼接，跳过文号与表单标签）。
  - XLSX 取工作表名，PPTX 取首屏文本；PDF 暂回退原文件名。
  - 确定性匹配：标题完全一致 1.0；附件编号一致 0.90–0.92；模糊相似 >=0.86 为 0.75+；同引用被多个附件命中降为 0.85，状态为 `candidate`。不做 LLM 最终裁决。
- `extensions/parser/attachment/importer.go`
  - `WithSemantics(db *store.DB)` 注入扩展库；附件上传后写入语义，失败只记录日志，不阻断附件提取/上传。
  - 父文档标题与 native `file_hash`（32 位 MD5）优先写入 `ParentFileHash`。
- `extensions/store/migrations/014_attachment_semantics.sql`
  - 新表 `ext_attachment_semantics`，主键 `(tenant_id, attachment_id)`，含原始文件名、显示名、序号、正文引用标题/位置/摘录、状态/置信度、索引策略、父版本 hash 与父标题。
- `extensions/store/attachment_semantics.go`
  - `SaveAttachmentSemantics` 事务 upsert；`AttachmentSemantics` 按 tenant + ID 批量读取。
- `extensions/retrieval/attachments.go`
  - `AttachmentLinks` 批量查询语义；父文档 `FileHash` 与语义记录不一致时清空语义字段，避免父文档新版本继承旧映射。
  - 结果按 `AttachmentSequence`、`OriginalFilename`、`FileName` 稳定排序。
- `extensions/contract/attachments.go`、`internal/extensionbridge/attachments.go`
  - `RelatedAttachment` 增加可选语义字段（`omitempty`）；原有五个必填字段与下载 URL 不变。
- `frontend/src/components/RelatedAttachments.vue`、`frontend/src/utils/relatedAttachments.ts`
  - 标题“相关附件（N）”与“可下载”；已映射卡片显示语义名称、原文件、对应正文/章节、来源制度；`candidate` 显示“待确认”；未匹配项进入“其他附件”并显示“具体用途待识别”。
- `extensions/cmd/attachment-backfill/`
  - 支持 `-ext-db`、`-native-db`、`-files-root`、`-tenant`、`-kb`、`-include-disabled`、`-dry-run`；默认跳过 deleting/disabled 附件。

## 真实数据验证

生产 native 库 23 条附件、7 个父文档，其中 17 条 completed/enabled 可回填，6 条 annex-01..06 为 deleting/disabled（父文档 678a8d05）。

在 `/tmp/agentrag_ext_attach07a_test.db`（生产扩展库副本）执行回填：

```text
parents=6 analyzed=17 written=17
ext_schema_migrations max version = 14
ext_attachment_semantics rows = 17
```

代表性结果：

- 0fa411f8《企业电子银行业务操作规程（2025年修订版）》：4 个附件均为 confirmed，标题分别为企业电子银行数字证书/密码信封交接登记簿、企业电子银行人员维护申请表、企业电子银行客户/账户/用户机构变更申请表、企业电子银行转账手续费欠费记录消除申请表。
- 678a8d05《公司业务上门服务管理办法（2026年修订版）》：6 个附件在测试库中因 deleting/disabled 默认跳过；其标题识别结果包括中国邮政储蓄银行上门人员备案表、公司业务上门服务审批单、公司业务上门服务申请书（annex-03/04 同引用 candidate 0.85）、上门服务记录清单、上门影像特殊情况说明。

## 验证命令

```text
cd extensions
gofmt -l .                         # 无输出
go build -mod=readonly ./...       # 通过
go test -mod=readonly ./... -count=1  # 全部包通过
go vet -mod=readonly ./...         # 无输出
python3 scripts/check_module_boundary.py
# module boundary passed: 298 imports, no WeKnora dependency or active workspace
```

```text
cd frontend
npm run type-check                 # 通过
npm test                           # 2406 tests, 2401 pass, 4 environment-only failures, 1 skipped
npx tsx --test src/utils/relatedAttachments.test.ts  # 5/5 pass (sorting)
npm run build                      # 成功
```

## 生产部署结果

- 部署前备份：`data/backups/attach07a-20261107-2238/agentrag_ext.db`，`pragma integrity_check=ok`，schema v13，`ext_lifecycle_current` 69 行。
- 生产扩展库：schema v14，`ext_attachment_semantics` 17 行（confirmed 11、candidate 5、unmatched 1）。回填输出 `parents=6 analyzed=17 written=17 dry_run=false`。
- 前端：`web/` 已由 `frontend/dist` 同步；`web/index.html` 引用 `assets/main-BsRel67T.js`、`assets/index-CDdayOmU.js`，`LineageView-D3omO1vF.js` 存在。
- 服务：Host 8090、Core 18880 健康；新 Host 二进制 `data/launcher-run-1wLAAm/extension-host`（24,584,507 B，2026-11-07 23:13），launcher 以 detached `setsid` 方式运行。
- 接口验证：`/bridge/after-retrieve` 对 `0fa411f8-b935-4e58-ac41-9c00401e720d` 返回 4 条附件，均含 `display_name`、`original_filename`、`attachment_sequence`、`reference_title`、`reference_location`、`parent_title`、`mapping_status=confirmed`、`mapping_confidence=1`；对 `678a8d05-295e-4aa6-92d3-aaf931056ae7` 返回 0 条（6 个附件为 deleting/disabled，符合预期）。
- 前端 bundle 已包含 `具体用途待识别`、`相关附件`、`原文件`、`对应正文` 等语义卡片文案（`web/assets/index-BgqzT48j.js`）。
- 父文档 678a8d05 的 6 个附件当前为 deleting/disabled，部署后仍不会出现在附件卡片中；这是生命周期状态导致，不是语义映射缺失。
- 真实聊天页面登录态目视验收已完成（2026-11-08）：打开会话 `6d319c6d-88c8-43c0-99d8-6f9f045f4be5`，发送“境外账户开户需要哪些表格？请只用相关附件卡片回答。”，新 assistant 消息 `8d552a72-36f9-4de0-99c5-0c1b31f7c064` 的 `agent_steps` 中 `related_attachments` 出现 34 次并包含全部语义字段。
- 页面截图显示“相关附件（7） 可下载”，每张卡片展示语义标题、`原文件：annex-XX.docx`、`对应正文：...`、`来源：...` 与下载按钮；`candidate` 项显示“待确认”。截图见 `docs/evidence/attachment-semantics-ui-2026-11-08.png`。
- 卡片排序修复（2026-11-08）：`frontend/src/utils/relatedAttachments.ts` 按父文档首次出现顺序分组后，按 `attachment_sequence` 升序、`original_filename`/`file_name` 稳定排序；新增单元测试 `cards group by parent and sort by attachment sequence` 通过（`npx tsx --test src/utils/relatedAttachments.test.ts`，5/5）。前端 `npm run type-check` 与 `npm run build` 通过，`web/` 已同步新构建；服务 `/` 已返回 `assets/main-BsRel67T.js`。
