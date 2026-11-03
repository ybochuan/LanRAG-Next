# 内嵌附件阶段一实施计划

> For agentic workers: use superpowers:executing-plans；按已授权设计直接实施，不提交或推送。

**Goal:** PDF及Office内嵌白名单文件独立上传到原生KB，强制附件/父文档Metadata，原生列表可见。

**Architecture:** Registry使用既有pdfcpu与archive/zip；Host校验原生身份后提取并调用既有weknora.Client。PDF在上传字节校验后、预检前执行；Office在原生convert文件读取后调用独立附件端点，正文继续原生reader。扩展无新增依赖/迁移，不写ext_lineage_edge。

**Spec:** 用户本轮设计及Office薄接线明确批准。基线5c03ea6 / v0.9.0-4-g5c03ea6，开始工作树干净。

## Tasks

- [x] 1. `extensions/parser/attachment/{registry,pdf,office,annex}.go`与测试：接口Supports/Extract、PDF原始附件、三种Office目录、真实类型与严格小于20MiB、无附件/越限/损坏/过滤。
- [x] 2. `extensions/weknora/attachment.go`与测试：原生multipart文件上传、强制字符串metadata、身份核对、409仅承认同父/同hash附件；复用既有固定origin/凭证/超时。未知应答不自动重复POST。
- [x] 3. `extensions/parser/{attachments,native,mineru_upload,scope,recovery}.go`和`host/runtime.go`：配置Importer、认证POST attachments、PDF预检前提取；Office原生薄点及定向测试。只提取一层，附件子文档不递归。
- [x] 4. 合成带附件PDF/Office与用户DOCX正文附录回归；真实隔离Core/Host上传、列表/Metadata验证；扩展build/test/vet/boundary及原生定向/候选构建；更新事实/任务/补丁/验收。

## Decisions

- 原生metadata是map[string]string：is_attachment写字符串"true"，parent_document_id保持原生ID；类型适配不改变原生表。
- Office请求只用于附件提取；不把Office强行送入现有PDF-only MinerU链路。
- 单附件严格小于20MiB；源文件沿用50MiB上限，提取批次最多32件/64MiB。白名单由扩展名及PDF/OOXML内容共同确认，OLE .bin不解包。
- 原生相同字节去重返回409。仅同tenant/KB/parent/附件hash才承认为同一入库回执；不同父或普通原件冲突不篡改Metadata，不宣称exactly-once。
- 本阶段Metadata只是父子标记，不构成治理依赖；父文档失效不会据此自动撤销附件。
- 用户追加批准正文附录拆分：仅独立“附件N”段落作为分界，保留对应段落、表格和包资源；普通引用不拆。指定DOCX无word/embeddings成员，但实测有6个独立附录、4张表格；原件只读，不复制用户内容到Git。
- Ruling: 附件Metadata仅标记独立文档归属，不创建治理依赖 — 用户明确排除ext_lineage_edge — 父文档失效不会自动撤销子文档。

## 验证及最终复核

- Task 1 complete：Registry及真实样本测试退出0；6附录/4表格。PDF原始附件、Office严格20MiB及引用不拆测试通过。
- Task 2 complete：weknora/Importer回归退出0；8并发共享1个原生上传回执；不同父不覆盖，无未知POST自动重发。
- Task 3 complete：parser/host与原生完整docparser测试退出0，CGO候选Core构建退出0；解析算法未改。
- Task 4 complete：完整扩展suite、CGO=0 build、vet、模块边界、Python33例（2skip）退出0；四包race退出0；合成及用户真实样本的隔离Core/Host链路退出0。命令/边界见`docs/evidence/attachments-2026-11-03.md`。
- Final review：一次只读独立审查，Critical0 / Important1 / Minor0；不派二次复核。
- Final fixed：PDF超限附件使同批小附件失败 — `TestPDFSkipsOversizedAttachmentsWithoutLosingSmallFiles`先复现退出1，再逐项提取并只跳过pdfcpu大小超限错误后退出0；保留解码预算，完整扩展suite及受影响四包race再次退出0。
- Final Ruling: OLE `.bin`不解包 — 用户白名单阶段按真实Office/PDF文件处理 — OLE包装附件不会独立入库。
- Final Ruling: 跨父/普通原件去重冲突拒绝 — 原生没有附件专用去重键，不覆盖他人Metadata — 同字节多父场景需后续设计。
- Final Ruling: 不承诺多Host或未知结果窗口exactly-once — 当前只保证本Host串行及已知回执对账 — 未知/部分成功需排查和原生重试。
- Final Ruling: 生产Office/MinerU效果不以夹具验收替代 — 本轮实际Core/Host/API/存储/分块经过测试，转换与模型为夹具 — 生产引擎、运行服务替换仍需部署时确认。
- Deferred minors: none。
