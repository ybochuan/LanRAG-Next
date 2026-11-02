# M2 前端验收门禁修复

2026-11-01；HEAD `fa6353e8c821daa5eeb667dc46140e4d0d32f179`，`v0.9.0-2-gfa6353e`。本轮接续生命周期界面交付，处理其完整前端测试中的8项既有失败；保留全部既有M1/M2改动，未提交或推送。

## 根因与最小修改

| 失败 | 根因 | 修改及保留的检查 |
|---|---|---|
| Sandbox网络编辑器3项、PDF错误提示、Skills运行版本提示 | 测试用LF限定正则读取CRLF源文件，提取结果为空或匹配失败 | 三个测试文件只对读入文本执行CRLF→LF；凭据身份、deny-all、规则顺序、PDF错误通知、实际运行版本断言均未删除。 |
| PPTX运行时 | Windows反斜杠路径不匹配Unix路径断言 | 对断言输入规范化路径分隔符；仍以真实esbuild产物检查ESM入口、单一Vue runtime和无CJS Vue。 |
| PTY alias、CLI主机参数字面量 | Windows的bash命中WSL启动器，且/bin/sh不是Windows可执行路径 | 仅测试选择Git for Windows的实际POSIX shell；非Windows保持本机shell。PTY脚本路径通过位置参数传递，原alias与恶意URL字面量断言不变。 |

文件：六个原测试文件、`frontend/src/test-utils/posixShell.ts`、`posixShell.test.ts`。辅助器只被测试导入；不修改生产组件、Vite配置、原生代码、Docker脚本或依赖。缺失Git shell明确报错，不静默跳过测试。

原六个测试文件保存CRLF；为满足仓库字面量diff检查，仅本次新增/替换的行写入LF，未格式化或转换其他行。

## 实际验证

全部在Windows PowerShell / `frontend/` 中运行，保持现有依赖版本。

| 命令／检查 | 退出状态 | 结果 |
|---|---|---|
| 修改前，六个受影响文件的 `node --import tsx --test ...` | 1 | 稳定复现全部8项失败，未以旧日志代替。 |
| 换行修复后的三个文件 | 0 | 21项通过。 |
| `node --import tsx --test src/utils/pptxRuntime.test.ts` | 0 | 真实打包与runtime去重通过。 |
| shell辅助器新测试，首次缺实现 | 1 | ERR_MODULE_NOT_FOUND，随后补最小实现。 |
| 七个相关文件最终定向回归 | 0 | 34项全部通过，含真实shell、空格/单引号路径、alias和注入字符字面量。 |
| `npm test` | 0 | 2369项：2368通过、0失败、1既有跳过。 |
| `npm run type-check` | 0 | Vue/TypeScript通过。 |
| `npm run build` | 0 | 生产构建通过；上游大型chunk提示保留。 |
| 三项隔离变异检查 | 0 | 在临时副本中破坏凭据身份、PDF错误提示、运行版本提示，三个原测试均以对应断言退出1；wrapper核验后退出0。 |
| `git diff --check`，仓库根 | 0 | 完整工作区通过；未通过修改Git配置屏蔽空白检查。 |

七文件回归命令：

```bash
node --import tsx --test src/test-utils/posixShell.test.ts src/components/SandboxConfigEditorDrawer.network.test.mjs src/components/source-preview/pdfjsWasmAssets.test.mjs src/utils/pptxRuntime.test.ts src/views/chat/components/SandboxTerminal.test.mjs src/views/integrations/cliIntegration.test.ts src/views/settings/SkillSettings.test.mjs
```

完整测试的1项跳过是既有human-handoff浏览器交互用例；本轮没有新增skip或放宽失败断言。Linux/macOS前端测试本轮未运行，不以Windows结果替代其他平台证据。

独立复核：Critical0 / Important0 / Minor0；独立34项回归退出0。审查时指出的新增行CRLF空白检查已按上述最小方式处理，随后字面量全仓diff-check退出0。

## 边界及后续

14个生产/依赖/运行文件SHA256与任务开始一致，包含相关Vue组件、CLI命令生成器、Vite配置、Docker脚本、两Go模块依赖、前端依赖、运行Core和web入口。只生成验证用dist，未部署或重启现有服务，未接触真实数据。

本项关闭前端8项基线失败；原生后端Sandbox/MCP/Python全量问题仍独立待排查，M2整体不随此前端门禁完成。补充通知自动联动仍需M3权威血缘及其事务证据。

完整本地日志：`tmp/m2-frontend-gates-{red,full,typecheck,build,mutations,final-targeted}.log`。可提交的脱敏摘要见 `m2-frontend-gates-2026-11-01.log`。
