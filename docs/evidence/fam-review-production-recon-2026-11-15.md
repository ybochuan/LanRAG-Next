# FAM-REVIEW 生产库对账证据（2026-11-15）

## 范围

- 只读连接生产扩展库与原生库，未启动、停止或重启生产服务，未执行任何写入。
- 扩展库：`/home/ybochuan/.local/share/agentrag/agentrag_ext.db`
- 原生库：`/home/ybochuan/.local/share/agentrag/weknora.db`

## 完整性

| 检查 | 结果 |
|---|---|
| `PRAGMA integrity_check`（扩展库） | `ok` |
| `PRAGMA foreign_key_check`（扩展库） | 无违规 |

## 对账结果

| 检查 | 结果 |
|---|---|
| 业务族 | 9 |
| 族成员 | 25，全部在原生库存在，原生解析状态均为 `completed`，`deleted_at` 均为空 |
| 族成员历史 | 25 条 `active` |
| 业务发布头 | 5，全部在原生库存在，原生解析状态均为 `completed` |
| 发布头 availability 缓存 | `active` 3；无缓存记录 2（原生文档存在，工作台读取时会触发刷新） |
| 成员 availability 缓存 | 25 名成员中 18 条有 `active` 缓存，7 条暂无缓存记录（原生文档均存在） |
| `ext_document_availability` | 18 条，状态分布均为 `active` |
| 打开中的复核提醒 | 0 |
| 因生命周期/可用性应隐藏但仍在 inbox 的提醒 | 0 |

## 结论

- 生产库中业务族、族成员与业务发布头没有发现原生文档缺失或删除残留。
- 2 个发布头尚未写入 availability 缓存，但原生文档存在；`FamilyWorkspace`/`FamilyMembers` 的读取路径会调用 `refreshFamilyAvailability`，首次打开工作台时应补齐缓存，不应把“无缓存”误报为删除。
- 本次为只读盘点，不修改生产数据；后续如出现原生删除，应以工作台读取后的 `missing`/`deleted` 对账结果为准。

## 复现

使用 Python `sqlite3` 以 `file:...?mode=ro` 只读 URI 连接两库，查询 `ext_business_document_families`、`ext_business_document_family_members`、`ext_business_family_published_heads`、`ext_document_availability`、`ext_review_alerts` 与原生 `knowledges` 的存在性/状态计数。完整查询脚本和输出保留在本次执行记录中。
