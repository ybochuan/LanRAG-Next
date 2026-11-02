# M1 解析层 Source Mapping 与任务清单

最新进展：2026-10-31已按用户授权完成原生PDF薄接线和Host接收端，真实上传→预检→归档→原生分块通过。下文旧日期段落保留历史快照；最新边界见末尾Native Integration及TASK_BACKLOG。Frozen原文未改。

## 基点与范围

- M0已由用户验收并提交；本轮工作基点为`d1c62cd900f76823b7a9fca0279a97426682e25a`，上游基线仍为WeKnora v0.9.0 / `f17ee8a`。开工工作区干净。
- 依据Frozen Architecture第7节、第15节，以及用户已确认的C-04：解析扩展放在独立代理，不重构原生解析器。
- PDF/Image先Cheap Preflight，后选flash/basic/standard/advanced，单次正常流程只创建一个正式parse job。Office等保留native/builtin路径；txt/md/rst/tex保留原生直接读取。
- 每库独立验收：准确率≥85%、高估率≤10%、低估率≤15%。保存原始产物及artifact_format、artifact_schema_version、mineru_version；不把content_list.json当固定契约。
- 本轮首批仅实现消费页级信号的纯路由规则；尚不实现文件预检、HTTP代理、数据库迁移或真实解析。M0治理、标签API、MCP及M1.5检索优化不混入本批。

## 本机可复用环境

用户提供`D:/mineru-venv`；已用其`Scripts/python.exe`读取包元数据，确认为MinerU **4.0.1**、pypdf 6.19.0、pypdfium2 5.13.0。只读检查本地`mineru/parser/api_server.py`，确认存在Uploads/Jobs、middle_json、structured_content，以及来自包版本的parser_version。

以上证明安装包及静态协议存在，不证明API Server正在监听、模型齐全或四档推理已就绪。本轮未启动GPU服务、下载模型、安装依赖或修改该环境。WSL默认Python没有上述PDF库，不能把Windows虚拟环境自动当作Linux Host依赖；预检部署/取消行为另做POC。Docker守护进程不可连接，未据此推断是否安装过MinerU容器。

## Requirement → Source → 接入方式

行号针对本轮基点重新读取；下列核心文件均为只读映射。

| 能力 | 原生相对路径／函数 | 大致行号 | 复用与边界 |
|---|---|---|---|
| 上传级解析覆盖 | `internal/handler/knowledge.go`，上传处理中的process_config解码；`internal/types/knowledge_process.go`的KnowledgeProcessOverrides | 399–405；34–37 | 现有parser_engine_overrides，无需新增原生字段；不能假设任意新key都会被MinerUReader发送 |
| 配置合并 | `internal/application/service/knowledge_process_config.go`，MergeParserEngineOverrides | 371–382 | upload覆盖tenant；没有自动将KB/document身份发送给MinerU |
| 原生解析入口 | `internal/application/service/knowledge_process.go`，knowledgeService.convert / callDocReaderWithTimeout | 4313–4457；4468起 | 选择引擎、读取文件并调用reader；不在这里改路由算法或重试机制 |
| 引擎分发 | `internal/infrastructure/docparser/engine_registry.go`，NewReader；`engines.go`，mineruEngine.NewReader | 69–83；214–216 | mineru直接使用Go原生适配器；仅改DOCREADER_ADDR不能截获这条路径 |
| endpoint与tier | `internal/infrastructure/docparser/mineru_converter.go`，NewMinerUReader / Read / readV1 | 54–71；86–115；144–163 | 复用mineru_endpoint、mineru_server_api_key、mineru_tier；优先POC endpoint指向扩展代理 |
| V1协议与上传 | `internal/infrastructure/docparser/mineru_v1_client.go`，detectMinerUProtocol / upload / putBytes | 157–203；283–361 | health探测、创建上传、PUT、complete；代理必须处理服务返回的upload_url，不能让文件字节绕过预检 |
| 正式job及恢复 | 同文件，createJob / waitJob / cancelJob | 364–433 | POST /v1/parse/jobs、轮询、取消；createJob没有原生幂等键，不能宣称故障重试天然只产生一个job |
| 原始产物 | 同文件，ParseWithLayout / downloadFile；`internal/types/docparser.go`，ReadResult | 239–280；455–482；24–38 | 原生下载zip后只交付Markdown、ImageRefs、SourceBlocks等；代理应在原始下载路径保存产物，不能依赖解析后Hook补取或二次parse |
| 备选HTTP DocReader | `internal/infrastructure/docparser/http_parser.go`，HTTPDocumentReader.Read | 198–240 | 有file_content/config/request_id，但没有tenant/KB/document身份；不是默认替换方案 |
| ext持久化 | `extensions/store/migrate.go`，DB.migrate；`migrations/003_parser.sql` | 见当前源码 | schema v3：文档snapshot、formal job intent/receipt、原始产物及FK；只在独立ext数据库实施，不改原生数据库 |

## 冲突点与处理建议（重要）

| 编号／分类 | 冲突证据 | 建议及实施门禁 |
|---|---|---|
| C-04／已确认的Implementation Difference | 四类Bridge Hook没有解析前字节或原始job/zip句柄 | 沿用独立MinerU endpoint代理方向；先证明原生适配器完全兼容，禁止向convert塞入新的路由业务 |
| C-11／协议版本风险，当前选择已明确 | 原生使用Uploads/Jobs；当前官方4.1已改Tasks并删除旧接口；本机4.0.1仍有旧接口 | M1锁定用户已有4.0.1；版本升级前重做兼容验证。不得使用宽泛的mineru>=4.0或latest镜像冒充锁定；当前未实施4.1转换 |
| C-12／作用域缺失，接线前必须解决 | 原生ReadRequest无tenant/KB/document字段；MinerU V1请求也不转发这些身份 | POC现有上传override中的专属endpoint前缀与服务端预配置scope绑定。未知scope拒绝；不能仅凭文件名/hash推断原生文档归属。普通UI、其他导入来源及自动KB绑定尚未覆盖；若必须新增核心身份DTO，先提交精确补丁建议，取得对应授权 |
| C-13／故障重试的单次解析边界 | 原生任务会重试convert，V1 createJob无幂等键；超时可能发生在远端接受job之后 | 正常流程检查正式job计数=1，预检计数=0；失败后优先恢复已知job，结果未知不得盲目POST。区分单流程一次与跨崩溃exactly-once，不伪造后者保证；POC后记录远端可恢复能力及剩余限制 |

外部版本证据：[当前官方V1 HTTP API／4.1迁移说明](https://opendatalab.github.io/MinerU/usage/http_api/)。该在线页面描述当前版本，不能代替本机4.0.1的固定源码契约。

## 文件级任务与验证

下列记录2026-10-30的计划路径；最新实际交付见末尾进展与TASK_BACKLOG，文件名不代表已实现。

| ID | 期望结果／涉及路径 | 验证方法 | 依据／何时重审 |
|---|---|---|---|
| M1-00 | 本映射、首批实施计划、backlog切换到M1 | HEAD/工作区/包版本核实；所有映射指向实际函数；Frozen与核心diff无新增 | M1启动；基点或后端版本变化时 |
| M1-01 | 4.0.1兼容及scope POC：计划`extensions/parser/scope.go`、`mineru_proxy_test.go` | 临时监听和隔离资源走health→upload→complete→job→poll→zip；两个显式scope不能交叉取file/job；未知scope拒绝；核对upload_url不绕过代理 | C-04/C-11/C-12；真实HTTP接线前 |
| M1-02 | 纯决策规则：`extensions/parser/router.go`、`router_test.go` | 表驱动验证四档、每种OCR信号、跨页最大需求、OCR与密集布局须同页才升Advanced；Office/native无tier；空预检拒绝；稀疏页严格>50%才复核 | Frozen7.1/7.2；规则变化及预检接线时 |
| M1-03 | 用户2026-10-31改用纯Go pdfcpu：`extensions/parser/preflight.go`、`preflight_test.go`、`router.go`及代理接线/测试 | 真实PDF对象/文本显示/ActualText/Form统计；预检本身不调用MinerU；有文本强制flash/txt，全纯图才OCR，不确定/坏/取消/超限拒绝；缓存字节同样预检，实际4.0.1验证无下载；不冒充乱码/布局标定 | 用户新要求覆盖Frozen PDF升档；预算、文件支持范围、真实语料及OS变化时 |
| M1-04 | V1代理及Host接线：计划`extensions/parser/mineru_proxy.go`、测试；候选`extensions/host/config.go`、`runtime.go`、README | 正常请求仅创建一个选定tier job；native格式保持原路径；传输失败/取消/重复提交/未知远端结果分测，原生API及M0门卫回归；生产接线须先通过01/03 | C-04/C-12/C-13；接线与重试策略变化时 |
| M1-05 | 版本化产物：计划`extensions/parser/artifacts.go`、测试、`extensions/store/migrations/003_parser_artifacts.sql`及migrate.go | v2→v3保留原数据；真实zip/middle_json/structured_content记录实际格式/schema/4.0.1版本；丢响应后复用同job；并发写唯一性、取消/崩溃恢复、SQL与文件完整性；未知schema保留原件并标记不可解释，不伪造版本 | Frozen7.4；实际产物与归属契约核实后 |
| M1-06 | 每库标定：计划`extensions/parser/calibrate.go`、测试、`extensions/testdata/parser/manifest.json` | 独立人工标注与保留验证集；按tenant/KB分别统计准确率≥85%、高估≤10%、低估≤15%；空样本、跨库混入和数据泄漏明确失败，不以合成样本宣称真实达标 | Frozen7.3；真实语料、阈值或模型变化时 |
| M1-07 | 原生闭环与M1验收：计划`extensions/scripts/m1-e2e-test.sh`、`m1_e2e_test.py`、报告 | 隔离核心DB与测试资料；原生上传→预检→唯一job→产物→原生分块/索引；另验证M0断网/通网；检查Office无tier job、四档真实模型能力、每库指标及已知限制 | PA-1；01–06交付及真实模型/语料齐备后 |

本轮纯路由函数不是预检器或校准结果；布尔页信号由未来预检提供。选择DenseGraphics作为同页OCR升Advanced的输入条件，是Frozen允许的高密度布局规则的首批具体化，不代表已标定图形密度阈值。

## 2026-10-31：M1-01 显式Scope/V1协议POC

本批基点fa6353e，M1首批已提交，开工clean；重新核对原生V1客户端上传、缓存、job、轮询与取消路径，未修改核心。新增scope.go、mineru_proxy.go、mineru_upload.go、mineru_jobs.go及HTTP测试，提供NewScopeProxy/ResolveScope；不启用生产Host。tenant/KB由静态配置及每scope凭据绑定，返回身份副本；这是操作者声明，不是原生权限或document归属证明。

代理为冷/热源均发放本地pending upload句柄；原生请求形状保持PUT→complete，先验证实际字节/长度/SHA256再授予input file能力。后端upload_url不得越出配置origin；前端上传URL由配置生成，不信任Host头。源file、job及输出file按scope隔离，结果文件不自动成为已验证输入。仅允许原生单file_id+ZIP子集，限制JSON/源/结果大小及内存记录容量；不会代理任意URL source或后端管理/list接口。

**原生配置格式：** `mineru_endpoint`应填`http://127.0.0.1:端口/parser/scopes/{route}`，末尾不带`/v1`。原生客户端自己追加`/v1/health`、`/v1/uploads`等；代理HTTP路由的完整前缀才是`/parser/scopes/{route}/v1`。本批只有构造器/测试，没有可直接填写的生产监听端口，不要把M0的8090当作已启用的解析代理。

实际执行`python extensions/scripts/m1_scope_poc.py --mineru-python D:/mineru-venv/Scripts/python.exe --report docs/evidence/m1-scope-poc-2026-10-31.md`：真实4.0.1、Flash、合成文本层PDF、ocr_mode=txt、local/offline、随机回环端口。两scope分别产生一个正式job，后端热缓存命中有直接计数证据，跨scope job/ZIP访问拒绝，返回版本4.0.1及有效ZIP；退出0。完整脱敏终端记录见该报告。没有把fixture代替真实后端，也未宣称原生上传API已闭环。

同进程同请求保留已知job回执，创建结果未知则拒绝再次POST；并发重复请求不重复创建。状态不持久，重启后的exactly-once/恢复仍未实现。core原生SDK定向测试在Windows因pg_query/CGO构建限制未执行断言，随后在WSL CGO=1通过；这些原生自有fixture测试不等于原生API→代理→真实服务的联合E2E。

### C-14／实际环境前置条件：Flash PDF auto也可能获取OCR资源

首次auto试跑触发ONNX OCR资源获取并超时，证据见`docs/evidence/m1-scope-poc-2026-10-31-auto-failed.md`；不能假设Flash对所有PDF均无需资源。后续txt离线实测通过且私有home未产生ONNX权重，只证明文本层协议路径，不证明auto/OCR、Basic/Standard/Advanced或真实语料效果。正式M1验收前须核实资源位置、版本、模型能力及下载授权；不通过改原生解析器或静默改档来硬凑。首次失败目录的清理被自动审批拒绝，残留路径和限制已单独记录。

ScopeProxy仍为内存POC构造器，生产Host及M0门卫不变。C-12的普通UI自动绑定、原生权限/Doc ID关联，以及C-13的持久恢复继续保留。M1-03真实文件Cheap Preflight、M1-04生产策略/Host、M1-05产物及M1-06标定均不能据此完成，M1总体仍进行中。

## 2026-10-31：M1-03 PDF Cheap Preflight（当前）

用户明确以纯Go pdfcpu实现预检，依赖只加入extensions module（v0.16.4、Go1.26）。preflight.go读取实际对象/内容流，计算总页、结构文本层页和纯图页；原生算法不改。普通文本显示、TJ/hex/隐藏OCR文本、压缩Form和直接/命名ActualText已有回归；结构树MCID未解析、inline或未知操作符/不可确认文本安全拒绝OCR。预算/取消是预检资源约束，不是OCR/Unicode质量或每库标定证明。

### C-15／Implementation Difference：用户PDF禁下载规则覆盖Frozen7.2升档

- 有任意确认文本页即flash/txt；混合PDF需复核，扫描部分不自动补OCR；只有全部页面确认为纯图才basic/ocr，其他情况422。复杂文本布局暂不升Standard/Advanced，图片原四档信号规则保留；Frozen原件不修改。
- pdfcpu没有完整Unicode/乱码提取器；本次以实际文本显示/编码字节和ActualText结构证据判层，不把Font/BT标记当文字，不承诺全PDF支持。每库85%/10%/15%指标仍需真实语料及新规则确认。
- 代理在冷/热字节校验后预检，并在job fingerprint之前覆盖tier/ocr_mode；调用方auto/ocr/advanced不能绕过，原生上传/分块/索引仍只读。

### C-12补充：原生doc_id与文件产物

原生V1没有doc_id。WithArtifactRoot显式开启文件归档，BindDocument仅接受可信进程内来源断言；缺绑定409且不创建job。完成ZIP正式成员middle_json.json原始字节存为data/parser_artifacts/{doc_id}/middle.json（根目录由配置确定），manifest保存格式/schema/MinerU版本、scope/source/hash。原子目录发布、相同重试幂等和冲突拒绝不代表SQL/FK、断电/崩溃/重启恢复。

同scope同后端缓存file_id只能绑定一个doc_id，第二原生文档安全拒绝；不能宣称支持重复内容多文档，需后续可信文档消费身份（M1-04D），不擅改原生协议。Host尚未挂载构造器。

真实4.0.1最终报告：[完整日志](../evidence/m1-preflight-2026-10-31-final.md)。合成1页文本文件传入auto/ocr+advanced，实际flash/txt，两个scope两个job且命中源缓存、各显式测试doc_id的middle/schema2.0落盘；Python受审计外部DNS/TCP尝试0、私有目录ONNX0。仅这个隔离夹具成立，不扩展为所有PDF/系统级网络隔离或原生API→索引E2E。最新命令/退出状态（含先前Outbox基线失败）、唯一一次独立复核及修复记录见TASK_BACKLOG。

## 2026-10-31 Native Integration（用户已授权落地）

| 修改点 | 实际相对路径／函数／大致行号 | 范围与验证 |
|---|---|---|
| 原生文件分派薄入口 | `internal/application/service/knowledge_process.go`，knowledgeService.convert，4373 | 仅PDF文件且环境配置时切到薄客户端；其他路径保持原resolveDocReader；不改ReadRequest或分块算法 |
| 请求身份及HTTP传输 | `internal/infrastructure/docparser/parser_proxy.go`，NewParserProxyReader，44 | 取Knowledge持久身份；每条V1请求及独立cancel加四个Header/Bearer，限定origin/路径，禁重定向/环境代理，错误不回退；新增单测覆盖并发/拒绝/断网/401/503/跨origin |
| 原生结果复用 | `internal/infrastructure/docparser/mineru_converter.go`，MinerUReader.readV1，145 | 仅支持可选request-local V1 transport；原ZIP/Markdown/image/layout转换继续复用 |
| Host身份接收 | `extensions/parser/native.go`，NewNativeHandler/ServeHTTP，48/69 | `/parser/native/v1/...`，唯一Header/major1/Bearer及配置tenant/KB；每doc独立scope，不从file hash猜归属；同字节不同doc归档测试 |
| 冷/热字节预检与绑定 | `extensions/parser/mineru_upload.go`，ScopeProxy.putUpload/completeUpload，101起 | native入口必需PDF，验证字节及预检；ACK前将file绑定可信doc；非PDF伪装/坏PDF拒绝 |
| 生产挂载与生命周期 | `extensions/host/config.go` LoadConfig，43；runtime.go Serve，40；bridge/server.go NewServerWithServices | 完整解析配置才启用，监听仍固定127.0.0.1:8090，排空后释放连接；probe/M0行为保持 |
| 自动原生上传 | `extensions/scripts/m1_native_e2e.py`、m1_native_e2e_linux.py、m1-native-e2e.sh | 新DB/公开认证/真实PDF→实际4.0.1单job→身份manifest/middle→原生分块；默认本地模型夹具，可显式真实模型模式 |

### C-12 当前处理

本批用户已批准三处生产薄补丁：convert从Knowledge提取tenant/KB/doc，传入薄客户端，由Header弥补原生V1缺身份；不重写原生DTO/消费或算法。Host显式tenant/KB许可、每doc独立子scope，解决本入口同字节多doc的问题。Header为可信核心断言，不代表Host自行验证原生权限，也未覆盖URL/Office/其他导入流程的全来源治理。

### C-13 补充实测：下游失败后原生重试仍有限制

真实模型初次测试的embedding地址错误引发原生整个入库任务重试；实际出现4次parse POST，后续ZIP归档因已有文档内容/身份冲突而503，原件未被覆盖。不能把当前按后端file_id+有效策略的同进程回执复用宣传为所有原生重试路径只解析一次。后续将内容/文档/已知job与SQL产物登记协调并优先reconcile；不通过改写原生调度、覆盖旧middle或盲目重试凑验收。正常流程仍要求实际job计数为1。

### C-16／Baseline Limitation：有限内存文档scope

此条为原接线时的容量限制记录。2026-10-31后续已交付受控驱逐/恢复：128是同时驻留上限，空闲且持久状态成功的scope可释放，正在执行/排队者不驱逐。130完整文档与活跃任务保护、真实生产Host重启测试通过。全活跃、写失败以及同doc自身上传上限仍安全拒绝；没有无界内存或TTL清空。见M1-04R与新证据。

### C-13 持久恢复接线结果（2026-10-31后续）

新recovery.go/003_parser.sql使用已验证内容hash、可信tenant/KB/doc、有效策略和后端origin，消除后端临时file ID变化造成的重复；先持久intent，再创建远端job。已知job回执及文档权限恢复后可读同job，原生完整重试不再因新upload/file ID而新建相同任务。当前实际MinerU/Host重启证据为POST总数1，失败响应/并发/直接退出由定向测试覆盖。

**重要冲突仍保留：远端已接收但本机未持久得到job ID时，MinerU4.0.1 V1没有可信的原生doc幂等键/唯一关联。** 对该内容/文档/策略组合安全503，不能自动重POST，也不能从全局job列表猜归属。需要人工或未来可证明唯一性的对账，且不改原生队列来“硬凑”exactly-once。后端/产物根配置更换不视作透明迁移；需操作员核对既有任务。

文件先原子发布，随后独立SQL事务登记原始middle JSON及format/schema/MinerU版本；登记失败不给ZIP成功，重启后核验同文件并修复登记。SQLite FULL同步不等于文件夹元数据/设备断电验收；物理掉电测试仍为M1-05P。真实标定语料用户确认暂缺，M1-06只有工具与空清单，不报告虚构指标。

### M1-05P：发布屏障 Implementation Difference / Baseline Limitation

`extensions/parser/artifacts.go`的persist保持原件核验及FS→SQL顺序，补暂存目录Sync、Rename后产物根Sync，已有文件的重试也补文档/根Sync。`artifact_sync.go`在非Windows执行实际目录File.Sync并传播错误；`artifact_sync_windows.go`保留既有文件Sync/rename，不承诺Windows目录刷新。该差异不涉及原生解析/分块/调度或Frozen原文变更。

`artifact_sync_test.go`及`artifact_crash_test.go`验证同步失败不登记/不ACK、四个真实退出窗口、单job恢复、SQL/原件/FK与跨doc拒绝，保留旧pending。已在Windows、WSL默认tmpfs及额外/mnt/d v9fs隔离目录通过；完整原生上传/M0联合回归通过。物理断电、产物根/祖先目录创建和目标设备持久性仍未验收，不能由进程退出或v9fs syscall成功替代。

### C-17／运行拓扑与验证边界

用户明确允许原生环境地址指定宿主IP，客户端允许显式HTTP(S) origin并保持请求固定目的地。Host监听不放宽；WSL NAT访问不到Windows127.0.0.1时，仅换宿主IP不能自动提供端口可达性。本轮真实E2E使用自有测试回环HTTP/WindowsPython互操作中继，不修改WSL、系统转发或防火墙，不声称已提供生产隧道。

独立复核发现挂载解析后沿用Bridge的整请求5秒及上游30秒期限会误伤慢文件。唯一修复轮次为解析设置独立默认1000秒bulk/body期限（可配置），metadata仍30秒、M0仍5秒。真实5.2秒分段上传测试先HTTP400后204；独立bulk/metadata期限和取消测试通过。该修复均在extensions内，不改原生业务。

完整原生service测试在当前DNS环境出现example.com/api.e2b.app被解析到198.18.0.0/15而被SSRF拒绝的失败；未修改SSRF规则或这些上游测试。当前已通过的docparser定向包及真实上传证据不能替代上游完整suite。
