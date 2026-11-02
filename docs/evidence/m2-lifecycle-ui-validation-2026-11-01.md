# M2 生命周期界面验证记录

后续补证（2026-11-01）：下方交付时记录的8项前端基线失败现已完成跨平台测试适配；完整 `npm test` 退出0（2368通过/0失败/1既有skip）。保留原始交付记录，最新根因与证据见 [前端门禁修复](m2-frontend-gates-2026-11-01.md)。

日期2026-11-01；源码HEAD `fa6353e8c821daa5eeb667dc46140e4d0d32f179`，上游v0.9.0。保留本轮前已有M1/M2/治理补丁；本任务无提交或推送。

## 交付范围

原生导航 `/platform/lifecycle`：库切换、状态分布与过滤分页、日期编辑、最近100条事件、待复核和库级默认规则。管理JWT入口沿用原生KB权限；薄接口仅鉴权、归属核对、DTO与本机HTTP。治理和数据库仍在extensions，未更改原生检索/解析/分块算法。

Host管理请求使用独立服务端凭据、版本1、固定回环。浏览器不能提供tenant、KB权限或can_write；API Key管理访问拒绝。普通读权限不升级为编辑权限，RBAC开关关闭时handler仍检查。完整标注CAS保留标签，规则正文及本库编辑revision同一读事务。

## 实测

| 命令／工作目录 | 退出状态 | 结果及边界 |
|---|---|---|
| `go test -mod=readonly ./... -count=1`，extensions，Windows | 0 | 全扩展模块通过，含管理认证、scope、日期失效、不复活、并发规则与日期守卫。 |
| `go build -mod=readonly ./...`、`go vet -mod=readonly ./...`，extensions | 各0 | 无新依赖，无CGO要求。 |
| `gofmt -l .`，extensions | 0 | 无输出。 |
| `python scripts/check_module_boundary.py`，extensions | 0 | 独立模块，311 imports无WeKnora依赖或活动workspace。 |
| `python -m unittest discover -s scripts -p '*_test.py' -v`，extensions | 0 | 33项，3项按环境跳过；不把跳过视为通过。 |
| `go test -mod=readonly -race ./... -count=1`，extensions，WSL Linux | 0 | 全模块通过；不代替真实SQLite并发验证。 |
| `go test -mod=readonly ./internal/extensionbridge ./internal/handler ./internal/router -run 'TestManagement\|TestExtensionLifecycle' -count=1`，根，WSL | 0 | 固定边界/响应scope、错误JSON/503/超时、权限与文档归属测试；router此筛选无用例，未声称完整原生测试通过。 |
| `CGO_ENABLED=1 go build -mod=readonly -tags sqlite_fts5 -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Edition=lite -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn' -o tmp/m2-ui-WeKnora-lite ./cmd/server`，根，WSL | 0 | 真实Lite核心构建。 |
| `node --import tsx --test src/views/lifecycle/model.test.ts src/assets/theme/styleGuard.test.mjs src/i18n/localeKeyAudit.test.ts`，frontend | 0 | 30项通过：日期/UTC/精度/标签、规则边界/来源、过时请求、样式守卫、国际化菜单。 |
| `npm run type-check`，frontend | 0 | Vue/TypeScript独立检查。 |
| `npm run build`，frontend | 0 | 生产产物；存在上游大型chunk提示，未改分包算法。 |
| `npm test`，frontend | 1 | 2368项：2359通过、8失败、1跳过。失败为NetworkSettings3项、PDF提示、PPTX运行时、终端alias、Skills参数和安装状态。原始HEAD前端隔离快照复现；终端另补原始HEAD的docker脚本单测复现同一失败。快照缺少其他目录的额外失败单独排除，未弱化断言或擅修无关代码。 |
| `python3 extensions/scripts/lifecycle_ui_e2e.py`，独立Linux网络命名空间 | 0 | 真实Core/Host/SQLite、原生TXT上传、本地确定性模型夹具；不占生产8090，不修改真实文档。 |

真实E2E完整脱敏日志：`m2-lifecycle-ui-e2e-2026-11-01.log`。覆盖登录、Host离线503、上传完成Observer→active、日期过期→stale、真实检索排除、8并发HTTP写入1成功+7冲突、未登录401、API Key403、跨库文档404、Viewer只读与写403。

独立审查发现并修复两项Important：规则正文/版本分离读取可能丢失更新；现有Timer不支持部分生效日期编辑。新增同事务快照、同CAS写事务日期守卫；复核通过，无剩余Critical/Important。

## 运行与限制

已备份Core/web并安装到原18880服务；隐藏托管启动器先Host ready后Core ready，Windows健康和生命周期HTML均200。通过已授权测试账号在浏览器查看导航、状态、日期详情、时间线、待复核与库级规则，没有保存真实文档的修改。原`.env`保持不变；私密运行配置和备份均在ignored data目录，无凭据入库。

最终静态目录替换遇到Windows挂载盘目录句柄拒绝后，核对归属并优雅停止本启动器的Core/Host，通过WSL完成目录替换，再启动同一服务。最终Windows健康/页面均200，`web/index.html`与最终构建产物一致；没有重启WSL或其他服务。浏览器再次确认未配置规则来源显示永久有效。

**冲突点／Baseline Limitation**：矩阵没有active→scheduled，故拒绝active的未来effective_from；scheduled不能清空effective_from。拒绝发生在同一CAS写事务，不改变日期、revision或状态。失效状态不会被清空日期复活。需要重新排期/复活时须先确认新状态矩阵。

日期保存和状态重评估是两个独立步骤；重评估暂时失败明确显示pending，等待扫描，不伪称跨事务原子。review_due只告警，后台扫描更新提醒；列表最多1000条并标出截断。未配置规则显示永久有效，默认规则修改不追溯已有文档。管理写请求结果不确定时先重新载入，不自动重放。

本次交付仅完成M2配置/生命周期界面；不代表M2剩余补充通知/后续lineage、归档审批、完整M6工作台或真实MinerU/云模型联合验收完成。原生全包历史失败也未由此消除。
