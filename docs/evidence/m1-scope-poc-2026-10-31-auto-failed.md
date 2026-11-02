# 首次 auto 模式试跑：失败摘要

这是从本轮工具输出及私有诊断重建的摘要，不是完整原始终端日志；原脚本在finally清理异常后未能落盘报告。

- 时间：2026-10-31 00:35:30 Asia/Shanghai。
- 后端：用户现有MinerU4.0.1，临时回环API、Flash、私有MINERU_HOME/uploads；合成文本层PDF。
- 输入模式：ocr_mode=auto。HTTP上传与job创建成功；TestScopeProxyRealMinerU401在45.15秒报告real parse deadline，退出1。
- 诊断：私有日志出现资源获取进度；私有models目录中实际产生1780590字节的ch_PP-OCRv6_tiny_det_infer.onnx，另一OCR资源仍未完成。不能把这次试跑称为零资源获取或OCR就绪。
- 清理：首次TemporaryDirectory.cleanup遇到backend-private.log的WinError32，随后抛NotADirectoryError。后续核查未发现Python进程，不能据此断言存在孤儿进程或确定文件占用方。
- 后续处理：增加私有目录路径验证及限时清理重试；采用local/offline资源设置，对已知文本层测试PDF使用txt模式。成功报告单独保存在m1-scope-poc-2026-10-31.md，未把其结果外推到auto/OCR或四档模型。
- 此次所有已查见的资源产物均在脚本拥有的临时目录。显式清理请求被自动审批以“blocked by policy”拒绝，未提供更细原因；未改用其他工具绕过，残留目录为`C:/Users/ybochuan/AppData/Local/Temp/agentrag-m1-scope-5uoknq6h`。未删除用户模型缓存、venv或原件。后续成功试跑自己的新临时目录已正常清理。
