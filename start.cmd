@echo off
rem One-key AgentRAG launcher: Windows services first, then Core + Host in WSL.
rem Extra arguments are forwarded to scripts\onekey.ps1 (e.g. -DryRun, -Stop).
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\onekey.ps1" %*
exit /b %ERRORLEVEL%
