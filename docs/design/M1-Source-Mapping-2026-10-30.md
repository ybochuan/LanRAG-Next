# M1 解析层 Source Mapping 与任务清单

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
| ext持久化 | `extensions/store/migrate.go`，DB.migrate | 15–55 | 当前schema v2；产物迁移候选003，只在独立ext数据库实施，不改原生数据库 |

## 冲突点与处理建议（重要）

| 编号／分类 | 冲突证据 | 建议及实施门禁 |
|---|---|---|
| C-04／已确认的Implementation Difference | 四类Bridge Hook没有解析前字节或原始job/zip句柄 | 沿用独立MinerU endpoint代理方向；先证明原生适配器完全兼容，禁止向convert塞入新的路由业务 |
| C-11／协议版本风险，当前选择已明确 | 原生使用Uploads/Jobs；当前官方4.1已改Tasks并删除旧接口；本机4.0.1仍有旧接口 | M1锁定用户已有4.0.1；版本升级前重做兼容验证。不得使用宽泛的mineru>=4.0或latest镜像冒充锁定；当前未实施4.1转换 |
| C-12／作用域缺失，接线前必须解决 | 原生ReadRequest无tenant/KB/document字段；MinerU V1请求也不转发这些身份 | POC现有上传override中的专属endpoint前缀与服务端预配置scope绑定。未知scope拒绝；不能仅凭文件名/hash推断原生文档归属。普通UI、其他导入来源及自动KB绑定尚未覆盖；若必须新增核心身份DTO，先提交精确补丁建议，取得对应授权 |
| C-13／故障重试的单次解析边界 | 原生任务会重试convert，V1 createJob无幂等键；超时可能发生在远端接受job之后 | 正常流程检查正式job计数=1，预检计数=0；失败后优先恢复已知job，结果未知不得盲目POST。区分单流程一次与跨崩溃exactly-once，不伪造后者保证；POC后记录远端可恢复能力及剩余限制 |

外部版本证据：[当前官方V1 HTTP API／4.1迁移说明](https://opendatalab.github.io/MinerU/usage/http_api/)。该在线页面描述当前版本，不能代替本机4.0.1的固定源码契约。

## 文件级任务与验证

下列是计划路径；除本轮router及测试外均未创建。每项状态以TASK_BACKLOG为准，文件名不代表已实现。

| ID | 期望结果／涉及路径 | 验证方法 | 依据／何时重审 |
|---|---|---|---|
| M1-00 | 本映射、首批实施计划、backlog切换到M1 | HEAD/工作区/包版本核实；所有映射指向实际函数；Frozen与核心diff无新增 | M1启动；基点或后端版本变化时 |
| M1-01 | 4.0.1兼容及scope POC：计划`extensions/parser/scope.go`、`mineru_proxy_test.go` | 临时监听和隔离资源走health→upload→complete→job→poll→zip；两个显式scope不能交叉取file/job；未知scope拒绝；核对upload_url不绕过代理 | C-04/C-11/C-12；真实HTTP接线前 |
| M1-02 | 纯决策规则：`extensions/parser/router.go`、`router_test.go` | 表驱动验证四档、每种OCR信号、跨页最大需求、OCR与密集布局须同页才升Advanced；Office/native无tier；空预检拒绝；稀疏页严格>50%才复核 | Frozen7.1/7.2；规则变化及预检接线时 |
| M1-03 | Cheap Preflight：计划`extensions/parser/preflight.go`、`pdf_probe.py`、对应Go/Python测试 | 真实自制文本/扫描/乱码/矢量/多栏/表格/空白PDF；只读文本层和页级信息，MinerU正式job计数=0；加密/坏文件/超限/超时拒绝；验证子进程退出及跨系统取消；使用现有venv不擅自安装 | Frozen7.2/7.3；探测器/依赖/部署OS变化时 |
| M1-04 | V1代理及Host接线：计划`extensions/parser/mineru_proxy.go`、测试；候选`extensions/host/config.go`、`runtime.go`、README | 正常请求仅创建一个选定tier job；native格式保持原路径；传输失败/取消/重复提交/未知远端结果分测，原生API及M0门卫回归；生产接线须先通过01/03 | C-04/C-12/C-13；接线与重试策略变化时 |
| M1-05 | 版本化产物：计划`extensions/parser/artifacts.go`、测试、`extensions/store/migrations/003_parser_artifacts.sql`及migrate.go | v2→v3保留原数据；真实zip/middle_json/structured_content记录实际格式/schema/4.0.1版本；丢响应后复用同job；并发写唯一性、取消/崩溃恢复、SQL与文件完整性；未知schema保留原件并标记不可解释，不伪造版本 | Frozen7.4；实际产物与归属契约核实后 |
| M1-06 | 每库标定：计划`extensions/parser/calibrate.go`、测试、`extensions/testdata/parser/manifest.json` | 独立人工标注与保留验证集；按tenant/KB分别统计准确率≥85%、高估≤10%、低估≤15%；空样本、跨库混入和数据泄漏明确失败，不以合成样本宣称真实达标 | Frozen7.3；真实语料、阈值或模型变化时 |
| M1-07 | 原生闭环与M1验收：计划`extensions/scripts/m1-e2e-test.sh`、`m1_e2e_test.py`、报告 | 隔离核心DB与测试资料；原生上传→预检→唯一job→产物→原生分块/索引；另验证M0断网/通网；检查Office无tier job、四档真实模型能力、每库指标及已知限制 | PA-1；01–06交付及真实模型/语料齐备后 |

本轮纯路由函数不是预检器或校准结果；布尔页信号由未来预检提供。选择DenseGraphics作为同页OCR升Advanced的输入条件，是Frozen允许的高密度布局规则的首批具体化，不代表已标定图形密度阈值。
