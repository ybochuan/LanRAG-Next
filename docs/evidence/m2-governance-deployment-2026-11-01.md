# M2来源治理部署验证（2026-11-01）

结果：新Core已安装并后台运行，浏览器入口为 **http://localhost:18880/**。原Core二进制已备份，原有文档与治理状态未人为改动；Root依赖与Frozen Architecture未改。本记录不含账号、token、密码或文档内容。

## 本轮终端验证摘录

```text
Lite candidate build: exit=0
Native targeted race (6 packages): exit=0
Native vet (6 packages): exit=0
Extension build/vet/test/race: exit=0
Module boundary: PASS, no WeKnora dependency or active workspace
Python: Ran 33 tests, OK (skipped=3)
Final full tests: chat_pipeline/agent/mcpserver/extensionbridge PASS
Full 6-package suite: exit=1; tools/service baseline-related failures retained
Independent final source review: no remaining blocker in specified governed paths
Governance Core+Host upload/index/RAG/Agent/stale/offline E2E: PASS, exit=0
```

独立E2E完整输出另见[日志](m2-governance-leak-e2e-2026-11-01.log)；失败的原生全量与代表overlay测试的准确限制见[报告](../design/M2-Governance-Leak-Closure-2026-11-01.md)。

## 实时启动与网络结果

```text
Initial WSL Core 8080 /health: HTTP 200
Initial WSL Host 8090 /health: HTTP 200
Windows 8080/8090: unavailable
Windows TCP exclusion range observed: 7991-8090
User explicitly authorized WSL restart; owned services stopped gracefully
WSL restarted; reserved range persisted
Windows 18880 local bind preflight: PASS
Core port=18880; Host Core URL=http://127.0.0.1:18880
[READY] Host
[READY] Core
[AUTH] Existing scoped machine credential verified
[READY] AgentRAG: http://localhost:18880
Windows http://localhost:18880/health: HTTP 200
Windows http://localhost:18880/: HTTP 200, text/html
Official original-account login: HTTP 200
Authenticated knowledge-base API: HTTP 200
Persisted launcher port / Host Core address: correct
```

Host仍只在WSL的`127.0.0.1:8090`监听。Windows保留8090不影响WSL内原生Bridge调用；没有改变固定500ms与fail-closed，没有删除系统端口保留、开放公网监听或修改防火墙。

原二进制备份与私密启动日志放在忽略的`data/governance-runtime-20261101-092201/`。当前Linux启动器PID由该目录`active-launcher.pid`记录；停止前必须核对进程命令与仓库路径，指引中提供了有校验的停止命令。私密运行目录由`data/.gitignore`明确排除。

未执行提交/推送。本次WSL重启只自动恢复本项目Core/Host，不承诺其他WSL或Docker工作负载已经恢复。未进行生产云模型或真实MinerU/PDF联合重验，也不将M2整体标为完成。
