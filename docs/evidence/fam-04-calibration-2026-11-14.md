# FAM-04 校准 CLI 验证（2026-11-14）

## 验证命令

```bash
cd extensions
go run ./cmd/family-calibrate ../tmp/fam04-calibration-smoke.json
```

`tmp/fam04-calibration-smoke.json` 为 24 组**合成**用例：16 组 train、8 组 holdout，正负例各半，主题组 train/holdout 不交叉。该文件仅用于 CLI 与算法路径冒烟，不是真实语料标注，不构成正式 precision/recall 验收。

## 结果

- CLI 退出 0，输出 `accepted: true`。
- train：16 样本，precision 1.0，recall 1.0。
- holdout：8 样本，precision 1.0，recall 1.0。
- 已选阈值 0.70，权重 0.60/0.25/0.15。

## 真实验收缺口

仓库未发现真实知识库的 20–50 组人工标注 manifest。正式启用前仍需按 `docs/superpowers/plans/2026-11-10-fam-04.md` 的字段准备真实清单，并由业务标注人复核训练/留出分组、精确率≥0.70、召回率≥0.60。

2026-11-16 补充：已使用生产 KB 的 33 组文档元数据/标题/描述/分块哈希构造清单并运行校准，train precision/recall 1.0/1.0、holdout 1.0/0.625、accepted=true；标签仍非独立双人人工标注，见[`m4-calibration-production-2026-11-16.md`](m4-calibration-production-2026-11-16.md)。
