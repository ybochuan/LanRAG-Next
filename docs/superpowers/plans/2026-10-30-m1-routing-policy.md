# M1 首批：纯路由规则 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 先交付可独立验证的Frozen页级信号→解析档位决策，不触碰原生代码或真实解析副作用。

**Architecture:** 沿用用户已批准的Frozen第7节及独立解析代理方向。纯Go函数消费预检信号，不调用MinerU、读文件、访问数据库或更改Host；未来在代理中使用。后端已核实为用户现有MinerU4.0.1，代理及scope仍按Source Map单独POC。

**Tech Stack:** 扩展Go1.26 module、标准库、testing；不引入依赖。

**Spec:** `docs/design/AgentRAG 技术方案 v1.0 — Frozen Architecture.md`第7/15节；`docs/design/M1-Source-Mapping-2026-10-30.md`。

## Global Constraints

- `extensions/`独立module，不import WeKnora internal，不修改根依赖/Frozen/BASELINE、原生算法或M0门卫。
- PDF/Image支持flash/basic/standard/advanced；Office及其他native类型不进入四档。未知类型报错，不猜测成Flash。
- 没有页级预检的PDF/Image报错；native类型不要求页信号。函数不会创建任何parse job。
- 页级最大需求决定文档档位；OCR与DenseGraphics须在同页才升Advanced。稀疏页比例严格>50%才标记人工复核，不自动删除或阻断文档。
- 无自动commit/push；本轮只在已核实干净的当前仓库工作，用codex/m1-parser-routing分支区分M0基点，不创建额外worktree或修改机器环境。

## Review Focus

- OCR四类硬信号不能遗漏，否则可能把扫描/乱码文档错误降到Flash。
- 不同页的OCR和图形信号不能被简单全局OR而错误升Advanced。
- 单页达到Advanced也要继续统计全部稀疏页，避免提前返回漏掉复核标记。
- Office即使误带OCR/布局信号也必须保留native路径。
- 空页列表不能被当作零复杂信号，50%边界不能提前标记复核。

### Task 1：锁定源映射与边界

**Files:** 本计划、M1 Source Map、`TASK_BACKLOG.md`、`PROJECT_CONTEXT.md`。

- [x] 核实M0提交d1c62cd、原生映射及本机4.0.1；记录C-11/C-12/C-13及环境实连未验。
- [x] 将M1标为进行中，02与03/04/05/06/07分别管理；不重新打开已验收M0。

### Task 2：路由规则RED→GREEN

**Files:** Create `extensions/parser/router.go`、`extensions/parser/router_test.go`。

**Interfaces:**

- `type PageSignals struct`：Scanned、NoText、Garbled、VectorText、MultiColumn、TableLikely、DenseGraphics、EmbeddedImages、SparseText均为bool。
- `type Decision struct`：Path、Tier、ReviewRequired；Path为native/mineru，native的Tier为空；Tier为flash/basic/standard/advanced。
- `func Decide(fileType string, pages []PageSignals) (Decision, error)`；fileType接受规范后缀（大小写、空白及单个前导点归一化），不接受路径/MIME猜测。
- PDF及原生广告的常见栅格后缀走MinerU决策；本批明确支持pdf/png/jpg/jpeg/bmp/tif/tiff。Office/OpenDocument/EPUB/OFD/HTML/MHTML/CSV/TSV和txt/md/rst/tex保留native，不宣称这些格式的真实后端解析已验收。

- [x] 先写表驱动测试，期望值直接来自Frozen：零信号Flash；四类OCR各Basic；布局各Standard；OCR+布局Standard；同页OCR+DenseGraphics Advanced；跨页最大需求；稀疏页1/2不复核、2/3复核；native无tier；空预检和未知类型报错。
- [x] `go test -mod=readonly ./parser -count=1`，确认因缺失实现失败。
- [x] 最小实现Decide，不添加持久化、HTTP、进程执行、标定配置或Scope接口；不得用它宣称C-12已解决。
- [x] 同一命令通过；执行完整扩展测试，确认未破坏其他组件。

### Task 3：验证与交付

- [x] 在extensions运行gofmt只读检查、完整test/build/vet、模块边界与Python回归；Linux具备工具链时跑本批parser包race。
- [x] 最后仅一次整体只读代码复核；如有Important，仅做一轮RED→GREEN修复并重跑相应完整suite；其余范围不顺手扩展。
- [x] 更新backlog：02完成只代表决策函数，M1仍进行中；预检、代理、真实单job、产物和标定未验。
- [x] git diff --check、完整status/name-only及Frozen/根依赖hash核对。上游全量/前端、真实GPU/解析E2E本批不执行、不宣称通过。

## 本批结果

2026-10-30完成首批，M1总体仍进行中。实际独立复核一次无实质问题；最初不可用模型调用不算评审。完整十三包最后退出0，但本轮也重现既有Outbox报告计数失败一次，已补记M0-06C，未修复。文件预检、HTTP/scope、真实job、产物及每库标定均未因此完成；具体命令、结果和边界见TASK_BACKLOG最新记录。
