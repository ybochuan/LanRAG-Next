# 附件阶段二／三接线

用户2026-11-03明确授权检索附件Metadata、父生命周期联动、TDesign聊天卡片及必要原生薄接线。保留阶段一全部未提交改动；基线5c03ea6。

- [x] 扩展复用原生batch/list读取附件Metadata，不增表/依赖；before_retrieve在最终读事务检查同库父active，缺失/非active/读失败关闭。
- [x] after_retrieve返回同scope的附件名称和原生文档下载URL，普通RAG/Agent结果Metadata携带related_attachments；增强失败只省略卡片。
- [x] 原生附件下载沿用权限及文件服务，增加现有before_retrieve许可检查；链接点击重新校验父状态。
- [x] TDesign卡片、去重、登录下载及Markdown点击；RAG/Agent通用展示。
- [x] 定向/完整必要检查、真实隔离Core/Host/API正反验收、前端组件/类型/构建；更新事实/任务/补丁及证据。

实现选择：原生实际下载API为/api/v1/knowledge/{id}/download，需登录凭证；不使用匿名签发或把凭据放URL。卡片链接和Markdown使用该原生路径，由现有登录下载函数读取Blob。附件关联使用阶段一parent_document_id，不写ext_lineage_edge；父active依赖在每次策略读取判断，不做额外状态迁移或后台任务。

验证：扩展完整suite/build/vet、相关四包race、模块门禁、Python33例（2skip）、原生四包定向及CGO候选Core构建退出0；前端2378pass/1skip、type-check/build退出0。附件阶段二/三真实E2E及仓库要求的M2治理E2E均退出0；实际组件浏览器下载字节和403提示确认。完整命令/范围见`docs/evidence/attachment-downloads-2026-11-03.md`。

一次独立只读审查Critical0/Important0/Minor1：新Vue行的CRLF不满足diff-check。保留原始行字节，仅新增行用LF后检查退出0，两个聊天文件实际各新增10/8行，没有整文件格式化；未二次复核。范围外：原生/files、旧signed grant、已下载字节、生成中持续撤销、生产服务替换和真实云模型/Office引擎联合验证。
