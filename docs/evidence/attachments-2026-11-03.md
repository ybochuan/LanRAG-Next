# 附件阶段一验收（2026-11-03）

基线`5c03ea645b3f22f287cc0b0977bc4f717162c6e6`，`v0.9.0-4-g5c03ea6`。开始工作树干净，依赖、SQL迁移及前端未改变；只新增已授权Office原生reader薄包装。用户原件只读，未复制文档内容/提取文件到Git，未提交/推送或替换生产进程。

## 测试边界

- 提取器实际运行既有pdfcpu和标准库ZIP/XML。PDF测试由pdfcpu写入真实内嵌DOCX后字节核对；Office覆盖三种embeddings路径、类型/大小过滤；正文覆盖普通引用不分割、段落/表格保留。
- 用户指定DOCX没有真正的ZIP embeddings，但正文含6个独立附录，合计4表格。实测完成后源文件SHA256保持不变；不输出或存储用户正文到证据文档。
- E2E使用本轮CGO候选Core、真实worker Host、全新SQLite、官方认证/上传/列表/下载API。Office DocReader转换和模型为本地夹具，测试真实DOCX字节，不调用生产MinerU/DeepSeek；不冒称真实Office解析引擎或生产界面部署验收。
- 测试在自有网络命名空间启用loopback，生产Core/Host及8081/8082服务未停止或替换。列表可见是隔离库的实际API结果，用户生产库未新增这些测试副本。

## 完整E2E输出

合成DOCX（退出0）：

```text
[SCOPE] Real candidate Core + worker Host + private SQLite; local model/Office DocReader fixtures; original sample read-only
[NATIVE-LIST] PASS: one parent + 2 independent DOCX annexes visible through native list API; forced parent metadata verified
[CONTENT] PASS: 1 tables preserved in independent DOCX packages; parent and every child completed native chunking/indexing
[REPLAY] PASS: repeat extraction reconciled same independent IDs; no extra documents; child extraction did not recurse
[RESULT] Attachment stage-one E2E PASS; exit=0; no lineage edges or production services changed
```

用户样本（退出0）：

```text
[SCOPE] Real candidate Core + worker Host + private SQLite; local model/Office DocReader fixtures; original sample read-only
[NATIVE-LIST] PASS: one parent + 6 independent DOCX annexes visible through native list API; forced parent metadata verified
[CONTENT] PASS: 4 tables preserved in independent DOCX packages; parent and every child completed native chunking/indexing
[REPLAY] PASS: repeat extraction reconciled same independent IDs; no extra documents; child extraction did not recurse
[RESULT] Attachment stage-one E2E PASS; exit=0; no lineage edges or production services changed
```

早期脚本失败与修正：首次未等待候选构建完成，报二进制不存在；随后漏设`DOCREADER_TRANSPORT=http`导致原生按gRPC解释HTTP地址，已修正测试环境；第一次列表/分块通过后，脚本错误将`resource://`当本地路径，现改为原生授权下载接口核对真实字节；PowerShell向WSL传`$1`丢失，改用临时参数数组启动样本测试。没有为这些测试问题修改原生算法。

## 本轮命令与结果

Linux命令的PATH为`/usr/local/go/bin:/usr/bin:/bin`，Go1.26。完整扩展测试在`unshare -Urn`自有网络中启用回环后执行，全部命令按各模块工作目录运行。

| 工作目录 | 命令 | 退出状态／证据 |
|---|---|---|
| 根 | `CGO_ENABLED=1 go build -mod=readonly -tags=lite,sqlite_fts5 -ldflags='-X github.com/Tencent/WeKnora/internal/handler.Edition=lite' -o tmp/WeKnora-lite-attachments-check ./cmd/server` | 0，候选Core；未覆盖生产 |
| 根 | `CGO_ENABLED=1 go test -mod=readonly -tags=sqlite_fts5 ./internal/infrastructure/docparser -count=1` | 0，完整docparser包；非anydoc链接构建 |
| extensions | `CGO_ENABLED=0 go build -mod=readonly ./...` | 0，无新增CGO依赖 |
| extensions | `go test -mod=readonly -p 1 ./... -count=1` | 0，完整扩展；无测试文件包仍按Go输出，不冒称覆盖 |
| extensions | `go vet -mod=readonly ./...` | 0 |
| extensions | `CGO_ENABLED=1 go test -mod=readonly -race ./parser/attachment ./parser ./weknora ./host -count=1` | 0，四个受影响包；非全仓race |
| extensions | `python3 scripts/check_module_boundary.py` | 0，294 imports，无WeKnora依赖/启用workspace |
| extensions | `python3 -m unittest discover -s scripts -p '*_test.py' -v` | 0，33例，2既有平台/opt-in skip |
| extensions | `go env GOMOD GOWORK` | 0，GOMOD为extensions/go.mod，GOWORK空 |
| extensions | `gofmt -l .`、`go list -deps -f '{{.ImportPath}}' ./...` | 格式无输出；依赖中无WeKnora/internal |
| 根 | `CORE_BINARY=... python3 extensions/scripts/attachment_e2e.py`（自有netns） | 0，合成2附录/1表格 |
| 根 | 同上加`--sample "$SAMPLE"`（默认期望6/4，自有netns） | 0，用户真实DOCX、原生API及分块 |

未运行上游全仓Go测试、make test、前端检查、生产负载或完整云模型/生产Office引擎联合验收。未宣称治理父依赖、深层OLE、复杂意图路由、跨父文件复用或跨Host exactly-once。正文包沿用原资源；动态页码/跨节交叉引用不承诺自动重算。

## 一次独立复核与单轮修复

只读审查Critical0/Important1/Minor0。Important为PDF中一个超20MiB附件导致整个提取失败；新增混合小附件、恰好20MiB及20MiB+1的真实PDF夹具，`go test -mod=readonly ./parser/attachment -run TestPDFSkipsOversized -count=1`先退出1（`PDF attachment extraction failed`），再修改为逐项提取、仅跳过pdfcpu解码大小超限后退出0。预算没有取消，其他解析错误仍拒绝。修复后完整扩展suite、build/vet及四包race全部重新执行退出0，不派二次审查。Deferred minors: none。
