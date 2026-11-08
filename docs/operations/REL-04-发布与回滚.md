# REL-04 正式发布与回滚

## 2026-11-08 本机发布结果

- 停写后通过 SQLite backup API 备份原生库和扩展库，并保存旧版 Core、Host、静态资源及私有启动配置。两份备份完整性检查通过，旧扩展库 schema 为 14。回滚包位于 WSL 私有目录 `/home/ybochuan/agentrag-rel04-rollback-y4x3a7dr/`，不得对外共享。
- 正式 Core（18880）和 Host（8090）已通过原一键启动器上线，运行中二进制与候选 SHA-256 一致。`/platform/associations` 返回候选网页，原生机器凭据校验通过，扩展库升至 schema 18，两库完整性及扩展外键检查通过。MinerU、Office 解析、本地向量及重排均已启动并可达。
- 正式登录态浏览器打开「文档关联」并显示关联表单、当前关系查询、三种新关系和附件归属。合成 DOCX 经正式服务解析出两份附件，验证分块、真实混合检索和流式问答返回两张下载卡片的数据，附件原生下载返回 DOCX 字节。主件过期后两个仍处于 active 的子件下载返回 403。删除主件后两份附件及活跃分块和向量记录清理完成，独立模板及另一条关系保留。验收文档与会话已清理。详见 `tmp/rel04-034e2104/live-rag-smoke.log`。
- 使用两库停写备份副本，在隔离网络中实际启动旧版 Core、Host 和旧网页，验证 schema 14、健康和已授权知识库读取。正式运行保持 schema 18。

## 候选包与验证

候选包位于 `D:/LanRAG-Next/tmp/rel04-034e2104/`。其中的 `WeKnora-lite`、`extension-host` 和 `attachment-backfill` 为 WSL/Linux 二进制，`web/` 为候选前端。构建基于 `5dd40b9` 及本地工作树上的改造。使用同一目录中的 `SHA256SUMS` 核对部署文件。

隔离端到端测试使用候选 Core、worker Host、临时 SQLite、模型和 Office 解析夹具，退出码为 0。日志见候选目录的 `e2e.log`。

| 核对项 | 结果 |
|---|---|
| 旧 schema 升级 | 合成 schema 16 通过实际 Host 升至 18，既有关联、归属和 schema 16 备份保留。真实 schema 14 扩展库的只读副本也升至 18，两次均通过完整性及外键检查。 |
| 关联工作台 | 候选 Core 返回 `/platform/associations`。真实 JWT API 验证 `cites`、`implements`、`template_for` 的查询和图方向、撤销引用，以及两份制度共用独立模板。 |
| 附件与检索 | DOCX 两份附录独立入库并分块。真实 RAG、Agent API 返回下载元数据。主件过期或 Host 离线时，旧下载 URL 返回 403。 |
| 重解析与删除 | 隔离库准备替换源文件后调用原生 `reparse`，验证附件复用、新增和停用，普通问答及下载只见当前附件。删除主件清理当前及历史附件和活跃索引，独立模板及另一条关系保留。单删、批删、清空、断 Host 后恢复也已验证。 |

当前原生源码有换版服务方法，但无公开的换版上传入口。换版用上述隔离验证复现，公开换版上传待原生入口提供。

从仓库根目录在 WSL 复跑隔离测试：

```bash
export PATH="/usr/local/go/bin:$PATH"
release="$PWD/tmp/rel04-034e2104"
(cd "$release" && sha256sum -c --quiet SHA256SUMS)
unshare -Urn bash -c 'set -e; ip link set lo up
  CORE_BINARY="$1/WeKnora-lite" AGENTRAG_ATTACHMENT_HOST_BINARY="$1/extension-host" \
  AGENTRAG_ATTACHMENT_WEB_DIR="$1/web" AGENTRAG_E2E_TMPDIR=/tmp \
  python3 extensions/scripts/attachment_e2e.py --release-relations' bash "$release"
```

## 历史附件

当前部署配置覆盖一个知识库。回填器已支持同租户、同文档的 `resource://` 原生源文件绑定，限定活跃、本地且无路径前缀的存储后端，再通过同文档路径、原生文件哈希和重新提取的 SHA-256 核验。新版工具位于 `tmp/rel04-034e2104/attachment-backfill-verified`，校验文件为 `SHA256SUMS.backfill`。

2026-11-08 已在正式扩展库回填 23 条历史归属。停写后重新提取，23 条均与批准清单逐项一致，跳过 0 条。工具 `-apply-legacy-ownership` 退出 0，原生库在回填期间无写入。两库完整性和扩展外键检查通过。重启后 Core 健康、Host 就绪，管理接口读回全部 23 条 `attachment_of`。归属盘点为已验证 23、未确认旧归属 0、冲突及不匹配 0。本次使用只读接口验收这些归属，随主件删除沿用此前合成文档的验收证据。

本批停写备份位于 WSL 私有目录 `/home/ybochuan/agentrag-rel04-backfill-d0rq13pa/`，含 `native.db` 和 `extension.db`。批准的原始清单与带文件名的审核副本保留在 `/home/ybochuan/agentrag-rel04-reviewed-plan-om31oayg/`。操作汇总见 `tmp/rel04-034e2104/backfill-live.log`。首次在线查询使用了超过接口上限的分页大小，收到 HTTP 400。按每页 20 条复查通过，期间没有再次执行回填。

后续批次仍需先审核清单，再停止 Core、Host 和其他可能写入原件、原生资源绑定或扩展库的进程，重新备份两库。停写期间生成新清单，与审核的条目逐项核对后再执行应用。若清单改变，重新审核。应用和数据库检查完成后恢复正常启动。见 [文档附件提取与入库](文档附件提取与入库.md)。

## 正式切换

1. 预留维护窗口，暂停上传、删除和治理写入。确认实际解析服务及选用的模型可达，记录 Core 与 Host 的旧运行目录和进程身份，保留原部署配置。候选验收时的夹具不能作为正式服务。依赖启动见 [启动与运维指南](AgentRAG-启动与运维指南.md)。
2. 在服务仍运行时，在 WSL 终端执行 `umask 077; backup="$(mktemp -d "$HOME/agentrag-rel04-rollback-XXXXXX")"`。按已核对的进程身份保存旧 Core、旧 Host 的实际可执行文件及其静态资源到这个目录。保留本次运行的私有启动参数。随后在原启动终端停止旧进程，确认 Core、Host 监听都已退出，再做停写备份。
3. 在 WSL 从仓库根目录使用 SQLite backup API 各备份一份，检查两份备份的完整性。备份路径只保存在操作终端，配置里的凭据保持私有。

   ```bash
   python3 - "$backup" <<'PY'
   import json, sqlite3, sys
   from contextlib import closing
   from pathlib import Path

   config = json.loads(Path("data/agentrag-launcher.json").read_text())
   for key, filename in (("DB_PATH", "native.db"), ("AGENTRAG_EXT_DB", "extension.db")):
       source = Path(config[key]).resolve(strict=True)
       target = Path(sys.argv[1]) / filename
       with closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True)) as src:
           with closing(sqlite3.connect(target)) as dst:
               src.backup(dst)
               if dst.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
                   raise SystemExit(f"Backup integrity check failed for {key}")
               if key == "AGENTRAG_EXT_DB":
                   if dst.execute("PRAGMA foreign_key_check").fetchone():
                       raise SystemExit("Extension backup foreign key check failed")
                   print("Extension backup schema:", dst.execute(
                       "SELECT MAX(version) FROM ext_schema_migrations").fetchone()[0])
   PY
   ```

4. 按原运行配置启动本次校验过的二进制。分别在两个 WSL 终端设置 `repo=/mnt/d/LanRAG-Next`、`release="$repo/tmp/rel04-034e2104"` 和 `runtime_env`（原启动器已生成的私有 `private.env` 的绝对路径）。保留原运行的存储目录、端口和 Lite 参数。先启动 Host：

   ```bash
   source "$runtime_env"
   cd "$repo/extensions"
   exec "$release/extension-host"
   ```

   在第二个终端确认 `http://127.0.0.1:8090/health` 返回 `status=ready`，再启动 Core。本机默认文件目录为 `$repo/data/files`，若原运行采用自定义目录，先设置为原值。

   ```bash
   source "$runtime_env"
   cd "$repo"
   export DB_DRIVER=sqlite RETRIEVE_DRIVER=sqlite STORAGE_TYPE=local
   export STREAM_MANAGER_TYPE=memory NEO4J_ENABLE=false ENABLE_GRAPH_RAG=false
   export LOCAL_STORAGE_BASE_DIR="${LOCAL_STORAGE_BASE_DIR:-$repo/data/files}"
   export SERVER_HOST=127.0.0.1 SERVER_PORT="$CORE_PORT" PORT="$CORE_PORT"
   export WEKNORA_WEB_DIR="$release/web"
   exec "$release/WeKnora-lite"
   ```

   保持两个终端或由现有进程管理器托管。原有 `start-all.sh` 会重编 Host 并把网页目录设为仓库根下的 `web/`。若使用该脚本切换，需先部署候选网页并核对它构建的 Host。
5. 核对候选 Core 与 Host 健康、生产扩展库 schema 18、正式模型及解析服务可达。用已授权范围内的临时测试文档登录验收。在「文档关联」创建和撤销关系，核对图方向、问答附件下载、主件到期拒绝下载，以及删除主件后附件消失且独立模板保留。确认正常后再开放业务写入。

## 回滚

若 Host 未就绪、数据库检查失败或正式验收出现数据异常，保持停写并停止候选 Core 与 Host。保留候选数据库及其 WAL/SHM 文件用于排查。将经核对的停写备份恢复到新路径，通过旧启动参数指定恢复后的 `DB_PATH` 与 `AGENTRAG_EXT_DB`。用步骤 2 保存的旧 Host、Core 及旧静态资源，按 Host 就绪后再启动 Core 的顺序恢复。旧 Host 需要与它兼容的扩展 schema。

恢复后核对健康、登录、知识库和正常问答。若维护窗口外已经有真实业务写入，先核对新写入及文件变化，再决定恢复范围。数据库快照不包含原件，禁止用真实文档演练删除。
