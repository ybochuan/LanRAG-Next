@echo off
rem One-key AgentRAG launcher: Windows services first, then Core + Host in WSL.
rem Extra arguments go to scripts\onekey.ps1: status|logs|doctor|token|mcp-key, or switches (-DryRun, -Stop).
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\onekey.ps1" %*
exit /b %ERRORLEVEL%
