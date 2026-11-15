# REL-08 收尾验收记录（2026-11-15）

## 范围

- 验证 `rendition_of` 的真实 MinerU 解析回执、绑定、受控下载、Host 重启复用和版本漂移拒绝。
- 验证管理端 `rendition_bind`/`rendition_download` 的只读、未配置和跨库权限边界。
- 验证绑定重试幂等和跨库读取拒绝。

## 真实 MinerU E2E

执行目录：仓库根目录（Windows 侧 Python 驱动，实际 WSL CGO Core + WSL Host + Windows MinerU4.0.1）

当前源码核心构建（WSL Ubuntu，仓库根目录）：

```text
GOWORK=off CGO_ENABLED=1 /usr/local/go/bin/go build -mod=readonly \
  -tags=lite,sqlite_fts5 \
  -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn' \
  -o /mnt/d/LanRAG-Next/tmp/rel08-core-20261115 ./cmd/server
```

真实 E2E：

```text
python -B extensions/scripts/m1_native_e2e.py --core tmp/rel08-core-20261115 --renditions --report docs/evidence/rel-08-e2e-current-2026-11-15.log
```

结果：退出码 `0`，`[RESULT] PASS`。日志见 [rel-08-e2e-current-2026-11-15.log](rel-08-e2e-current-2026-11-15.log)，覆盖：

- `[REL-08-BIND]`：真实 PDF + MinerU 回执、按文档 ID 的 native JWT 绑定与图、字节级下载校验、scope/credential/role 拒绝。
- `[REL-08-RESTART]`：Host 重启后复用同一绑定，下载字节不变。
- `[REL-08-DRIFT]`：原生版本变化后旧转换件不能下载或重绑，并从当前图消失。
- 上传、预检、归档、SQL 原始字节、外键和解析索引检查均 PASS。

## 单元/回归覆盖

- `extensions/lineage/rendition_test.go`：并发绑定、丢响应重放同一绑定、跨知识库读取拒绝、源字节变化、停用原件和错误产物。
- `extensions/management/server_test.go`：`rendition_bind`/`rendition_download` 的只读 403、未配置 503、跨库 scope 403。
- `go test -mod=readonly ./... -count=1`：扩展全量通过。

## 发布边界

本次验证使用隔离数据库和临时 native 文档，不修改真实用户库，也不代表生产环境已切换或上线。REL-08 的生产部署/正式发布仍需按发布流程另行执行。
