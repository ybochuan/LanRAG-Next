#!/usr/bin/env bash
# Run in WSL/Linux. Credentials never go into shell arguments or terminal logs.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
exec python3 "$script_dir/e2e_test.py" "$@"
