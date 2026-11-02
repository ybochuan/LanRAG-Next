# 本机启动修复验证记录（2026-10-31）

主链路完整终端输出：[m2-startup-e2e-2026-10-31.log](m2-startup-e2e-2026-10-31.log)。该次运行退出0，使用真实Linux核心、真实worker Host、隔离的原生/扩展SQLite文件和真实multipart TXT上传；模型使用本地确定性夹具。

## 已执行检查

| 命令 / 环境 | 结果 |
|---|---|
| WSL：仓库根`python3 extensions/scripts/startup_e2e.py` | 退出0；Host先Ready；官方登录签发限定KB机器凭证；2个原生chunk；Observer active；正常RAG引用；到期输入由真实启动扫描生成active→stale事件；过期引用及实际模型上下文均不含文档专属随机串；Ctrl+C清理与源库校验通过 |
| Windows：extensions内`go test -mod=readonly ./... -count=1` | 退出0；parser-calibrate命令包无测试文件，不据此宣称全包均有测试覆盖 |
| Windows：extensions内`go build -mod=readonly ./...`、`go vet -mod=readonly ./...` | 各退出0 |
| extensions内`gofmt -l .` | 退出0，无输出 |
| extensions内`python scripts/check_module_boundary.py` | 退出0；310 imports；无WeKnora依赖或启用workspace |
| WSL：extensions内`CGO_ENABLED=1 go test -mod=readonly -race ./... -count=1` | 退出0；之后最后的DDL指纹改动涉及store，已再次执行下行定向race |
| WSL：extensions内`CGO_ENABLED=1 go test -mod=readonly -race ./store ./host ./bridge -count=1` | 退出0 |
| Windows：extensions内`python -m unittest discover -s scripts -p '*_test.py' -q` | 退出0；33项，3项平台/显式开启限制跳过 |
| WSL：extensions内`PATH=/usr/local/go/bin:$PATH python3 -m unittest discover -s scripts -p '*_test.py' -q` | 退出0；33项，3项平台/显式开启限制跳过；Go门禁需要Linux Go在PATH中，不能依赖Windows路径中的同名条目 |
| WSL：`bash -n start-all.sh` | 退出0 |
| 仓库根`git diff --check` | 退出0；仅Git行尾提示，无空白错误 |

## 异常取消

8090被另一个本机启动器实例占用后，补充取消测试使用独立用户/网络命名空间，不杀该实例：

```bash
unshare -Urn bash -lc 'ip link set lo up; cd /mnt/d/LanRAG-Next/extensions && AGENTRAG_RUN_STARTUP_FAULT_TEST=1 python3 -m unittest discover -s scripts -p "startup_e2e_test.py" -v'
```

实际结果：`Ran 1 test in 11.526s`、`OK`，命令退出0。测试向E2E runner发送SIGTERM，runner按预期失败退出并清理其启动器、Core和Host，随后测试验证两个私有监听端口均释放。该取消不是RAG验收失败。

## 恢复与审查

- 手动执行001、001–003、001–005各完整DDL前缀但缺迁移历史时，严格指纹验证后自动补齐历史、升级至5；已有规则事实保留。不兼容DDL拒绝且不盖迁移历史。
- 审查发现的配置重启丢失、合法dotenv解析、取消清理、既有文件权限/链接问题均修正；配置优先级、dotenv常见写法/复杂表达式拒绝、私有文件权限和链接拒绝均有回归。
- 新私有JSON、旧脚本备份、运行目录、默认扩展库、解析产物由`data/.gitignore`覆盖，已实际用`git check-ignore`核对。
- 本轮未修改原生源码；现存原生diff是此前授权的M1/M2/SSRF补丁。根go.mod、go.sum与BASELINE.txt校验值保持不变。
- 本轮沿用已编译Linux核心，未重新构建上游核心/前端、未跑上游全量测试、未重跑真实MinerU/DeepSeek/本地生产模型联合测试。M2整体和UI配置验收仍保持未完成。
