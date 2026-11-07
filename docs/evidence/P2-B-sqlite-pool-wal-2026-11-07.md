# P2-B Core SQLite 连接池与 WAL 评估（2026-11-07）

## 结论摘要

P2-B 完成只读审计、空载读并发、ext4 WAL 写压测、隔离 Core 写读竞争和混合读负载测量。

**决定：本轮不修改 Core 的 `SetMaxOpenConns(1)`、SQLite WAL 配置或读写连接池。** 现有 ext4 + WAL 配置下：

- 空载读取正常：`knowledge_bases` 16 并发 P95 17.72ms、`knowledge_list` 16 并发 P95 75.51ms，全部 HTTP 200；
- ext4 上写 20×8MB 事务期间，隔离 Core 的并发读仍为 P50 8.4–8.7ms、P95 13.5–14.2ms，无读写锁错误；
- 混合负载下 Host `overview` 2,231/2,231 成功、`knowledge_list` 1,844/1,844 成功；
- 单连接确实串行化 Core 的数据库访问（`knowledge_list` P50 从 c1 的 9.5ms 增至 c16 的 60.8ms），但当前业务并发下 P95 仍低于 100ms、无失败，尚不足以用业务影响证明必须改 Core 连接池；
- RAG 搜索的并发延迟不能归因于 SQLite 锁：4 路搜索单独测试 P50 5.3s，混合负载 P50 11.8s，日志无 `context deadline`、`sqlite-vec query failed` 或 `database is locked`；延迟更可能来自模型/重排或请求链路，需在 P2-E/模型侧另行确认。

建议把“Core 读连接池/读写分离”留作有明确高并发目标时的独立改动，先在 P2-E 建立监控；P2-C 先做前端 N+1/轮询优化。若未来 16+ 并发下 `knowledge_list` P95 持续超过 500ms，或出现 Busy/Locked 错误，再按本报告的方法复测并评估只读连接池。

## 环境与代码事实

- 仓库 HEAD：`4671536`（P2-A 提交后）。
- 运行目录：`data/launcher-run-lY5BJT`，Core/Host/模型服务七项均 UP。
- 数据库：`/home/ybochuan/.local/share/agentrag/weknora.db`（WSL ext4），190,218,240 bytes；WAL 24,752 bytes，SHM 32,768 bytes。
- SQLite：3.46.1；`journal_mode=wal`、`synchronous=2`（FULL）、`busy_timeout=5000`、`wal_autocheckpoint=1000`、`cache_size=-2000`、`page_count=46440`、`page_size=4096`、`freelist_count=2469`、`journal_size_limit=-1`。
- 表计数：`knowledges=79`、`chunks=13,565`、`lite_embeddings=6,095`、`knowledge_processing_spans=852`、`knowledge_bases=1`、`tenants=1`、`sessions=15`、`messages=38`。
- Core 连接配置：`internal/container/container.go:1053` DSN 为 `dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"`；`internal/container/container.go:1141-1144` 注释说明 SQLite 即使 WAL 也只支持一个并发写者，随后 `sqlDB.SetMaxOpenConns(1)`。Core 进程对 `weknora.db`、`-wal`、`-shm` 各持有一个 fd，符合单连接。
- 扩展库连接配置：`extensions/store/sqlite.go:78-79` writer 为 `SetMaxOpenConns(1)/SetMaxIdleConns(1)`，`:95-96` readers 为 `4/4`；Host 进程对 `agentrag_ext.db` 有 9 个 fd（含 WAL/SHM），不属于 Core 的 `weknora.db` 连接池。

## 测量方法

脚本（均为本地诊断脚本，未修改源码）：

- `data/p2b_bench.py`：对生产 Core 的只读 GET 并发测试；
- `data/p2b_sqlite_bench.py`：直接以只读连接访问生产库，并在复制库上做 WAL 写压测；
- `data/p2b_core_write_bench.py`：在 `127.0.0.1:18881` 的隔离 Core + 数据库副本上，持续写入时测 Core 读请求；
- `data/p2b_mixed.py`：生产 Core 上 `knowledge-search`、Host `overview`、`knowledge_list` 三路混合负载；
- 原始结果：`data/p2b-read-concurrency.json`、`data/p2b-sqlite-read.json`、`data/p2b-sqlite-write.json`、`data/p2b-core-write-contention.json`、`data/p2b-mixed.json`。

所有数据库写测试均针对隔离副本；生产库只做读取。隔离 Core 的副本位于 `/home/ybochuan/.local/share/agentrag/p2b-core/weknora.db`，监听 `127.0.0.1:18881`。

## 结果

### 1. 空载读并发（生产 Core，20 次/档）

| 接口 | 并发 | P50 | P95 | 状态 |
|---|---:|---:|---:|---|
| `GET /api/v1/knowledge-bases` | 1 | 4.23ms | 6.82ms | 20/20 200 |
| 同上 | 2 | 4.44ms | 7.04ms | 20/20 200 |
| 同上 | 4 | 5.54ms | 9.38ms | 20/20 200 |
| 同上 | 8 | 8.84ms | 21.72ms | 20/20 200 |
| 同上 | 16 | 9.89ms | 17.72ms | 20/20 200 |
| `GET .../knowledge?page=1&page_size=20` | 1 | 9.50ms | 10.81ms | 20/20 200 |
| 同上 | 2 | 10.58ms | 16.43ms | 20/20 200 |
| 同上 | 4 | 17.16ms | 24.18ms | 20/20 200 |
| 同上 | 8 | 34.76ms | 51.71ms | 20/20 200 |
| 同上 | 16 | 60.80ms | 75.51ms | 20/20 200 |

直接只读 SQLite 查询（不经过 Core）的 P50 为 0.05–0.35ms，QPS 约 2,153–3,300（16 并发下 Python 线程开销使 QPS 降为约 1,380）。说明数据库本身可以并行读，Core API 的排队主要来自应用层单连接和请求/响应处理。

隔离 Core（同一数据库副本，`18881`）空载读 P50：c1 11.0ms、c4 17.85ms、c8 27.75ms、c16 34.36ms；与生产 Core 趋势一致，排除了生产 Host 或环境差异。

### 2. ext4 WAL 写压测（复制库，不写生产库）

- 12 行 ×8MB：writer 总 613.5ms（51.12ms/行）；4 个读者 P50 1.87ms、P95 4.13ms，无错误；关闭连接后 WAL 为 0（自动 checkpoint 生效）。
- 隔离 Core 副本上 20 行 ×8MB：writer 总 3,040.27ms（152.01ms/行）；4 个 Core 读客户端分别完成 357–361 次请求，全部 200，P50 8.41–8.67ms、P95 13.47–14.24ms、max 21.8–25.97ms；writer 无错误。
- 对比迁移前 9p 上的历史 xlsx embedding 日志：`INSERT INTO vec_embeddings_2560` 2,078 条 P50 408.4ms/P95 616.9ms、`DELETE` 3,943 条 P50 423.7ms/P95 991.2ms；当时还有问答 1m48s、检索 60s 超时。迁移前后差距说明 ext4 迁移是主要改善，不需要靠改连接池补。

### 3. 混合读负载（生产 Core，15 秒）

| 任务 | 并发 | 请求数 | P50 | P95 | 最大 | 状态 |
|---|---:|---:|---:|---:|---:|---|
| `knowledge-search` | 4 | 8 | 11,831ms | 13,660ms | 13,660ms | 8/8 200 |
| Host `overview` | 4 | 2,231 | 25.24ms | 39.54ms | 110.36ms | 2,231/2,231 200 |
| `knowledge_list` | 4 | 1,844 | 19.37ms | 92.55ms | 1,308.97ms | 1,844/1,844 200 |

补充：4 路 `knowledge-search` 单独运行 15 秒（10 个请求）P50 5,314ms/P95 9,824ms/max 19,777ms；单搜索约 2.2–3.0s。说明搜索延迟主要受模型/重排并发影响；`core.log` 当前 28,744 行中仅 16 条 `SLOW SQL`，无 `context deadline exceeded`、无 `sqlite-vec query failed`、无 `database is locked`。

## 决策与边界

1. **不修改 `SetMaxOpenConns(1)`**：它仍是 SQLite 单写者语义下的安全选择。当前 16 并发 API 读 P95 75.5ms、无错误；隔离写压测期间 Core 读 P95 14.2ms；没有业务失败证据支持改连接池。
2. **不调整 WAL/同步级别**：`journal_mode=wal`、`wal_autocheckpoint=1000`、`synchronous=FULL` 均按预期工作；当前 WAL 很小，自动 checkpoint 正常。`synchronous=FULL` 的 fsync 成本未证明为瓶颈，不能为了追求数字盲改。
3. **P2-B 的边界**：本轮是空载/合成 8MB 写负载/隔离副本测试，不等同于真实 XLSX 业务文件生产 E2E；RAG 搜索并发受模型服务影响，不能把搜索延迟归因于 SQLite；真实高并发用户量和 50+ 并发未测。
4. **后续触发条件**：若生产监控出现 `knowledge_list` P95 持续 >500ms、Busy/Locked 错误，或并发写入阻塞读请求，再评估 Core 只读连接/读写分离；该项应单独立项，不混入 P2-C 前端优化。

## 验证命令与工作区

在 `/mnt/d/LanRAG-Next` 下执行（均退出 0；脚本默认参数指向 `data/launcher-run-lY5BJT/private.env` 与上述数据库路径）：

```bash
python3 data/p2b_bench.py
python3 data/p2b_sqlite_bench.py --run-write
python3 data/p2b_core_write_bench.py
python3 data/p2b_mixed.py
```

所有脚本在 `data/` 下，未改动 `internal/` 源码。P2-B 期间生产栈保持七项 UP，P2-A 提交 `4671536` 未被改动。验证命令的原始输出保存在：

- `data/p2b-read-concurrency.json`
- `data/p2b-sqlite-read.json`
- `data/p2b-sqlite-write.json`
- `data/p2b-core-write-contention.json`
- `data/p2b-mixed.json`

本报告不包含 token、API key、URL 凭据或响应正文。
