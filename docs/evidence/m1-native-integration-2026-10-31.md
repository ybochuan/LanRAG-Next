# M1 原生PDF薄接线：2026-10-31验证证据

本批原生PDF转发/Host接收端已实测；M1总体仍进行中。核心基点fa6353e8c821daa5eeb667dc46140e4d0d32f179，WeKnora v0.9.0基线；三处生产薄补丁，未改原生结果消费/分块/调度。下方完整终端输出来自实际脚本文件，保留失败过程。

## 结果与边界

- 最终夹具上传与真实模型模式均退出0：真实WSL CGO核心/Host、安装的Windows MinerU4.0.1，真实PDF预检flash/txt、job1、文档身份归档、原生分块。
- 用户授权重启8081/8082补`--embeddings`/`--reranking`，实际embedding2560维；明确DeepSeek用于生成式问答。显式原生knowledge-search重排outcome=ok；问答返回正确答案和上传PDF引用，QA自身重排调用0，两个API的验证分开记录。
- 新DB、随机账号、合成PDF、测试回环中继；未读写原数据库、改全局网络或放宽Host监听。模型服务保留运行。凭据没有写入仓库；仅测试进程环境/私有stdin/已清理的私有DB，报告脱敏。
- MinerU Python审计零外网尝试、私有目录无ONNX；不表示全系统网络隔离，且已授权的真实DeepSeek调用单独经过测试中继。
- SQL产物/重启与崩溃恢复、128 scope容量释放、所有失败场景幂等、真实语料/每库标定及OCR/其他档位未验收。原生失败重试观察到多job及归档冲突，见C-13；禁止将正常单job误写成跨失败/重启exactly-once。
- M0问答断网/通网未重跑；上游service完整测试失败，不能声称全量绿色。

## 命令与退出状态

以下命令在所属module执行，WSL明确选择Ubuntu，PATH包含`/usr/local/go/bin`。

| 验证 | 命令或范围 | 退出状态 |
|---|---|---|
| 原生Lite重编译 | 根目录，`CGO_ENABLED=1 GOWORK=off go build -mod=readonly -tags sqlite_fts5 -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn' -o ./tmp/m1-native-build-83iqcjjt/WeKnora-lite ./cmd/server` | 0 |
| 原生解析完整包 | 根目录，Ubuntu，`CGO_ENABLED=1 GOWORK=off go test -mod=readonly -tags sqlite_fts5 ./internal/infrastructure/docparser -count=1` | 0，包含最终401/503/不可达/跨地址测试 |
| 原生service完整包 | 根目录，Ubuntu，同设置，`go test -mod=readonly -tags sqlite_fts5 ./internal/application/service -count=1`；另一次`-json`诊断 | 1、1，环境相关失败见下文 |
| 扩展完整测试 | extensions，`go test -mod=readonly ./... -count=1` | 0，十三包 |
| 扩展构建 | extensions，Windows和Ubuntu，`CGO_ENABLED=0 go build -mod=readonly ./...` | 0、0 |
| 静态检查 | extensions，`go vet -mod=readonly ./...` | 0 |
| 格式 | extensions `gofmt -l .`，根新增client/test显式检查 | 0，无输出 |
| 并发检查 | extensions，Ubuntu CGO=1，`go test -mod=readonly -race ./parser ./host ./bridge -count=1` | 0，不是全module race |
| 独立module | extensions，`python -B scripts/check_module_boundary.py`；`go env GOMOD GOWORK` | 0，306 imports，无核心依赖/启用workspace |
| 脚本测试 | extensions，`python -B -m unittest discover -s scripts -p '*_test.py' -v` | Windows0：25例/skip1；Ubuntu0：25例/skip2 |
| 默认真实上传 | 根目录，`python -B extensions/scripts/m1_native_e2e.py --core /mnt/d/LanRAG-Next/tmp/m1-native-build-83iqcjjt/WeKnora-lite --report docs/evidence/m1-native-e2e-2026-10-31-final.log` | 0 |
| 真实模型模式 | 同命令加`--live-models`，key仅进程环境，报告m1-native-live-e2e-2026-10-31-explicit-search.log | 0 |
| 范围核对 | `git diff --check`、status全untracked、diff names/core、四个冻结文件SHA256 | 0；保留开工前改动，根依赖/BASELINE/Frozen不变 |

原生service结构化复跑有49个失败叶子案例：48个日志包含受限/假IP出站错误；`TestQueryTemplatesReplaceDesktopFailureIsFatal`为expected1/actual0调用断言。示例：`TestDataSourceReplacementCommitsBeforeEnqueueAndCleanup/url`、`file_url`、`TestUpdateMCPService_AppliesNonScalarUpdateWithoutName`、多个tenant_sandbox_config测试。example.com/api.e2b.app在当前环境解析到198.18.0.0/15，触发原有SSRF拒绝。未改这些上游代码、DNS或白名单；私有结构化原始日志保留在已忽略tmp目录，不纳入报告。根全量、前端/远端CI/负载未执行。

Ubuntu第一次不指定发行版的调用及默认bash曾落到无Go/bash的WSL发行版而退出1；改用明确`wsl -d Ubuntu --exec`后构建/测试通过，不修改系统默认发行版。初次Windows无CGO的历史原生问题仍不宣称修复。

## 独立复核及修复

一次只读独立复核：Critical0、Important1、Minor0。确认parser继承Bridge整请求5秒及上游30秒会误拒绝慢文件；唯一修复轮次只在extensions设置独立bulk/body默认1000秒，可配置，M0仍5秒。真实5.2秒分段上传先HTTP400后204，bulk/metadata分离与取消测试先失败后通过；修复后十三包suite与Linux目标race通过。没有第二轮复核；用户随后追加的真实模型脚本由主任务回归及实际运行验证。

复核未判定/后续范围逐项已落入TASK_BACKLOG：SQL/重启/容量释放；OCR/Unicode/标定；Host独立回查权限；已有doc换内容；语料检索质量；完整M0/预检重审。当前保持可信核心身份、冲突拒绝和安全容量上限，不把这些范围算作已完成。Deferred minors:none。

## 完整脱敏终端记录

下方从各次`.log`原样汇入，以便即使通用`*.log`忽略规则存在，也能审阅完整证据。日志中的身份均是新建测试资料。

### m1-native-e2e-2026-10-31-initial.log

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[ERROR] RuntimeError: port 8080 is already owned; refusing to stop or replace its process
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-e2e-2026-10-31-isolated.log

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned ephemeral core/Host loopback ports; existing 8080/8090 services preserved
[AUTH] Native registration/login succeeded; credentials redacted
[ERROR] RuntimeError: identity/archive/chunk/job acceptance failed
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-e2e-2026-10-31-diagnostic.log

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[ERROR] FileNotFoundError: [Errno 2] No such file or directory: '/tmp/agentrag-m1-native-build-6q6lj2vd/WeKnora-lite'
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-e2e-2026-10-31-diagnostic2.log

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=0 normalized_marker_chunks=1 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[ERROR] RuntimeError: identity/archive/chunk/job acceptance failed
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-e2e-2026-10-31-pass1.log

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=0 normalized_marker_chunks=1 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text=1
[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt
[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1
[INDEX] PASS: original native pipeline completed and persisted real PDF chunks; this test does not exercise the RAG/M0 gate
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESOURCES] PASS: zero audited Python external attempts; zero private ONNX weights
[RESULT] PASS
```

### m1-native-live-e2e-2026-10-31-initial.log

```text
[MODELS] Live embeddings ready; dimension=2560; generation=DeepSeek/deepseek-flash; credentials redacted
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[ERROR] RuntimeError: native upload did not complete: Document parsing failed. Check the file format and the DocReader logs for this attempt.
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-live-e2e-2026-10-31-diagnostic.log

```text
[MODELS] Live embeddings ready; dimension=2560; generation=DeepSeek/deepseek-flash; credentials redacted
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=43821 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[ERROR] RuntimeError: native upload did not complete: Document parsing failed. Check the file format and the DocReader logs for this attempt.
[DIAG] mineru: calls={'/v1/uploads': 4, '/v1/uploads/upload_ac70cb0a551152e58d9b5c0e/content': 1, '/v1/uploads/upload_ac70cb0a551152e58d9b5c0e/complete': 1, '/v1/parse/jobs': 4, '/v1/parse/jobs/job_ff6bb6090d8dc9776a2d9748': 2, '/v1/files/file-5d479687d4b73bdba0825f72/content': 1, '/v1/parse/jobs/job_06c230a65f67b38c33d086ca': 1, '/v1/files/file-fc57a83254186ffa9c604f39/content': 1, '/v1/parse/jobs/job_078f69ac8af45f462b373cd8': 1, '/v1/files/file-81506a505e56621d1cac38cd/content': 1, '/v1/parse/jobs/job_ff2e514409572be7abe1c12a': 1, '/v1/files/file-7087163192ca931a0b93b578/content': 1}; successful={'/v1/uploads': 4, '/v1/uploads/upload_ac70cb0a551152e58d9b5c0e/content': 1, '/v1/uploads/upload_ac70cb0a551152e58d9b5c0e/complete': 1, '/v1/parse/jobs': 4, '/v1/parse/jobs/job_ff6bb6090d8dc9776a2d9748': 2, '/v1/files/file-5d479687d4b73bdba0825f72/content': 1, '/v1/parse/jobs/job_06c230a65f67b38c33d086ca': 1, '/v1/files/file-fc57a83254186ffa9c604f39/content': 1, '/v1/parse/jobs/job_078f69ac8af45f462b373cd8': 1, '/v1/files/file-81506a505e56621d1cac38cd/content': 1, '/v1/parse/jobs/job_ff2e514409572be7abe1c12a': 1, '/v1/files/file-7087163192ca931a0b93b578/content': 1}; errors=[]
[DIAG] model0: calls={}; successful={}; errors=['ValueError /embeddings', 'ValueError /embeddings', 'ValueError /embeddings', 'ValueError /embeddings', 'ValueError /embeddings']
[DIAG] model1: calls={}; successful={}; errors=[]
[DIAG] model2: calls={}; successful={}; errors=[]
[CORE-DIAG] 2026-10-31 11:36:47.328 ERROR  BatchEmbedWithPool attempt 1/5 failed: API request failed with status 502:  document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:36:47.531 ERROR  BatchEmbedWithPool attempt 2/5 failed: API request failed with status 502:  document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:36:47.934 ERROR  BatchEmbedWithPool attempt 3/5 failed: API request failed with status 502:  document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:36:48.737 ERROR  BatchEmbedWithPool attempt 4/5 failed: API request failed with status 502:  document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:36:50.340 ERROR  BatchEmbedWithPool attempt 5/5 failed: API request failed with status 502:  document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:36:50.342 ERROR  Repository sqlite failed to batch save: API request failed with status 502:  document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:36:56.176 ERROR  [convert] DocReader failed knowledge=07072942-221d-4c09-a801-668bbb6b1fc2 code=DOCREADER_PARSE_FAILED: MinerU V1: download zip artifact: MinerU API status 503: artifact_commit_failed: parser proxy request rejected document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:37:07.170 ERROR  [convert] DocReader failed knowledge=07072942-221d-4c09-a801-668bbb6b1fc2 code=DOCREADER_PARSE_FAILED: MinerU V1: download zip artifact: MinerU API status 503: artifact_commit_failed: parser proxy request rejected document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:37:22.958 ERROR  [convert] DocReader failed knowledge=07072942-221d-4c09-a801-668bbb6b1fc2 code=DOCREADER_PARSE_FAILED: MinerU V1: download zip artifact: MinerU API status 503: artifact_commit_failed: parser proxy request rejected document_process=07072942-221d-4c09-a801-668bbb6b1fc2
[CORE-DIAG] 2026-10-31 11:37:22.997 ERROR  [SyncTask] Task failed (exhausted retries) type=document:process id=5b7a7372-0d58-48ed-9e26-cd55054222d6 elapsed=39.263939938s err=Document parsing failed. Check the file format and the DocReader logs for this attempt.
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-live-e2e-2026-10-31-v1.log

```text
[MODELS] Live embeddings ready; dimension=2560; generation=DeepSeek/deepseek-flash; credentials redacted
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=1 normalized_marker_chunks=2 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text=1
[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt
[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1
[INDEX] PASS: original native pipeline completed and persisted real PDF chunks
[RAG] HTTP=200; SSE kinds=['agent_query', 'answer', 'complete', 'references', 'thinking', 'tool_call', 'tool_result']; successful embedding=3; rerank=0; generation=3
[ERROR] RuntimeError: live RAG did not prove cited DeepSeek answer and actual model calls
[DIAG] mineru: calls={'/v1/uploads': 1, '/v1/uploads/upload_298b69bbe32c09464fb100b2/content': 1, '/v1/uploads/upload_298b69bbe32c09464fb100b2/complete': 1, '/v1/parse/jobs': 1, '/v1/parse/jobs/job_2cdb1570e2b2ba157b8b82a6': 2, '/v1/files/file-9fe69a291ba5e95b87ab8297/content': 1}; successful={'/v1/uploads': 1, '/v1/uploads/upload_298b69bbe32c09464fb100b2/content': 1, '/v1/uploads/upload_298b69bbe32c09464fb100b2/complete': 1, '/v1/parse/jobs': 1, '/v1/parse/jobs/job_2cdb1570e2b2ba157b8b82a6': 2, '/v1/files/file-9fe69a291ba5e95b87ab8297/content': 1}; errors=[]
[DIAG] model0: calls={'/v1/embeddings': 3}; successful={'/v1/embeddings': 3}; errors=[]
[DIAG] model1: calls={}; successful={}; errors=[]
[DIAG] model2: calls={'/v1/chat/completions': 3}; successful={'/v1/chat/completions': 3}; errors=[]
[CORE-DIAG] 2026-10-31 11:39:00.620 INFO 61274e51-f40a-45b7-8d39-13eb63f39802 [PIPELINE] stage=SearchParallel action=chunk_search_done has_error=false result_count=7
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-live-e2e-2026-10-31-rerank.log

```text
[MODELS] Live embeddings ready; dimension=2560; generation=DeepSeek/deepseek-flash; credentials redacted
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=1 normalized_marker_chunks=2 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text=1
[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt
[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1
[INDEX] PASS: original native pipeline completed and persisted real PDF chunks
[RAG] HTTP=200; SSE kinds=['agent_query', 'answer', 'complete', 'references', 'thinking', 'tool_call', 'tool_result']; successful embedding=3; rerank=0; generation=3
[RAG] Answer from synthetic PDF: Two reviewers are required before publication.<kb doc="native-fixture.pdf" chunk_id="4e5821c7-33b4-4935-ba0b-285cbb04e656" kb_id="f8cbb1ea-4587-4138-84b3-8fca6acaf389" />
[ERROR] RuntimeError: live RAG did not prove cited DeepSeek answer and actual model calls
[DIAG] mineru: calls={'/v1/uploads': 1, '/v1/uploads/upload_2fa923d2592dd9bd3758bf4a/content': 1, '/v1/uploads/upload_2fa923d2592dd9bd3758bf4a/complete': 1, '/v1/parse/jobs': 1, '/v1/parse/jobs/job_3ba193b8846a05a34a6056b6': 3, '/v1/files/file-3956951bb54b43957beff9a1/content': 1}; successful={'/v1/uploads': 1, '/v1/uploads/upload_2fa923d2592dd9bd3758bf4a/content': 1, '/v1/uploads/upload_2fa923d2592dd9bd3758bf4a/complete': 1, '/v1/parse/jobs': 1, '/v1/parse/jobs/job_3ba193b8846a05a34a6056b6': 3, '/v1/files/file-3956951bb54b43957beff9a1/content': 1}; errors=[]
[DIAG] model0: calls={'/v1/embeddings': 3}; successful={'/v1/embeddings': 3}; errors=[]
[DIAG] model1: calls={}; successful={}; errors=[]
[DIAG] model2: calls={'/v1/chat/completions': 3}; successful={'/v1/chat/completions': 3}; errors=[]
[CORE-DIAG] 2026-10-31 11:40:54.373 INFO 459a537f-1955-4e01-a2a2-6c314071f689 [PIPELINE] stage=SearchParallel action=chunk_search_done has_error=false result_count=7
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESULT] FAIL
```

### m1-native-live-e2e-2026-10-31-explicit-search.log

```text
[MODELS] Live embeddings ready; dimension=2560; generation=DeepSeek/deepseek-flash; credentials redacted
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=8080 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=1 normalized_marker_chunks=2 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text=1
[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt
[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1
[INDEX] PASS: original native pipeline completed and persisted real PDF chunks
[SEARCH] PASS: explicit native knowledge-search applied the real local reranker; outcome=ok
[RAG] HTTP=200; SSE kinds=['agent_query', 'answer', 'complete', 'heartbeat', 'references', 'thinking', 'tool_call', 'tool_result']; successful embedding=4; rerank=1; generation=3
[RAG] Rerank calls during QA itself=0; explicit search and QA are separate native API checks
[RAG] Answer from synthetic PDF: Two reviewers are required before publication.<kb doc="native-fixture.pdf" chunk_id="414f8e6e-01ee-4ece-8180-a82c436f1e67" kb_id="8eb23a84-b761-4c93-882d-0423fc75a4df" />
[RAG] PASS: uploaded PDF retrieved with real local embeddings; DeepSeek generated a terminal cited answer requiring two reviewers; local reranker separately proved by explicit native search
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESOURCES] PASS: zero audited Python external attempts; zero private ONNX weights
[RESULT] PASS
```

### m1-native-e2e-2026-10-31-final.log

```text
[TOPOLOGY] Actual WSL CGO core + actual WSL Host + installed Windows MinerU4.0.1; owned loopback HTTP/interop relay, no WSL/firewall/listener configuration changes
[PORTS] Owned core=54357 Host=8090; unknown existing owners are never stopped by this script
[AUTH] Native registration/login succeeded; credentials redacted
[VERIFY] chunks=2 raw_marker_chunks=0 normalized_marker_chunks=1 jobs=1 policy=['flash', 'txt'] pages=1
[VERIFY] public identity fields={'id': True, 'tenant_id': True, 'knowledge_base_id': True} manifest scope matches={'doc_id': True, 'tenant_id': True, 'knowledge_base_id': True}
[UPLOAD] PASS: native multipart upload -> completed; native chunks containing PDF text=1
[PROXY] PASS: authenticated native identity; real PDF preflight; one actual MinerU job; effective flash/txt
[ARCHIVE] PASS: native doc_id/tenant/KB match manifest; original middle.json schema2.0 / MinerU4.0.1
[INDEX] PASS: original native pipeline completed and persisted real PDF chunks
[RAG] Not exercised in default fixture upload mode
[CLEANUP] Owned Linux processes stopped; fresh isolated DB only, no original DB touched
[CLEANUP] Owned processes stopped
[RESOURCES] PASS: zero audited Python external attempts; zero private ONNX weights
[RESULT] PASS
```
