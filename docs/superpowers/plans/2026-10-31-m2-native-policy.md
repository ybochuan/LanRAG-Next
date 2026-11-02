# M2 自动迁移与真实检索策略实施计划

用户授权：过期pending/scheduled直接stale；Observer完成后自动初始化；每日扫描；DB真实策略；批准原生解析排除集并过滤chunk。保留所有既有改动，不提交推送。

规则优先级：超过effective_until先stale；否则未来effective_from为scheduled；否则解析完成active。review_due仅标记。定时器还激活到期scheduled；不把failed/未知解析状态晋升active，不复活stale/archived/superseded。

- [x] 先失败测试，再补expired pending/scheduled状态边、自动化与scoped每日扫描。启动补扫，默认24h，持久事实保证重启/重试安全。
- [x] 持久Observer适配：原观察提交后调用状态机；同一快照重复仍重试未完成决策，迁移时事务内核对最新completed观察，防止过时completed盖过failed。
- [x] retrieval/policy.go查ext_lifecycle_current；仅active可用，缺失/未知/非active排除；tenant/KB和已授权文档范围保持，DB错误503。
- [x] Host worker启用持久DB策略、Observer和扫描；probe无DB返回业务503，不能透传。后台任务退出/取消均join。
- [x] 原生Bridge严格限长/500ms/禁止重定向，验证响应身份、字段及allow/exclude完整分割；deny_all不执行检索。search_parallel仅结果过滤，不改检索/RRF/Agent算法。
- [x] 故障恢复/并发核心回归、scope/真实HTTP/原生管线、真实跨进程Host/SQLite到原生管线；扩展全checks/Linux race、原生完整相关包CGO测试/vet、Lite build；一次独立复核及一轮修复；同步文档与证据。

Ruling: 缺失生命周期不允许检索 — 新策略只允许active — 原生存量文档需先对账完成，启动窗口会拒绝。
Ruling: probe不再默认Pass-Through — 本轮用户要求真实策略接管 — 旧M0验收作为历史保留，新部署须配置worker及原生API凭证。
Ruling: 只过滤此次RAG检索结果 — 用户批准对应薄补丁 — Agent直接读/图谱/既有handle和生成过程中的状态变化不在此补丁保护范围。

Review: Critical0/Important1/Minor0。scheduled Timer晋升缺失最新completed校验；failed、processing、缺失观察三个回归先RED，事务内校验修复后GREEN。主任务完整回归，不派第二轮复核。

Ruling: 普通文档结果须属于返回允许集，WebSearch保留、排除优先 — 防止目标范围比请求候选更宽时漏判 — 未授权whole-KB枚举不会因此开放。

Evidence: docs/evidence/m2-native-policy-2026-10-31.md 与同名.log。二进制tmp/m2-policy-build-9piTRr/WeKnora-lite；未部署替换已有服务，完整生产上传/LLM问答及M2其他任务未冒充验收。
