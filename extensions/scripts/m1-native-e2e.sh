#!/usr/bin/env bash
set -euo pipefail
# WSL entry; the Windows owner starts/stops the installed Windows MinerU.
repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
windows_python="${MINERU_WINDOWS_PYTHON:-/mnt/d/mineru-venv/Scripts/python.exe}"
exec "$windows_python" -B "$(wslpath -w "$repo_dir/extensions/scripts/m1_native_e2e.py")" "$@"
