#!/usr/bin/env bash
# Build a Linux/WSL release from one committed tree. The output is independent of live data.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
    echo "Usage: bash $0 NEW_OUTPUT_DIRECTORY [GIT_REVISION]" >&2
    exit 2
fi

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
revision="$(git -C "$root" rev-parse --verify "${2:-HEAD}^{commit}")"
parent="$(cd -- "$(dirname -- "$1")" && pwd)"
output="$parent/$(basename -- "$1")"
if [[ -e "$output" || -L "$output" ]]; then
    echo 'Choose a new release output directory.' >&2
    exit 2
fi

work="$(mktemp -d "$parent/.agentrag-release.XXXXXX")"
trap 'rm -rf -- "$work"' EXIT
source_tree="$work/source"
bundle="$work/bundle"
mkdir -- "$source_tree" "$bundle"
git -C "$root" archive "$revision" | tar -x -C "$source_tree"

export GITHUB_SHA="$revision" VITE_FRONTEND_COMMIT="$revision" EDITION=lite
(
    cd "$source_tree/frontend"
    npm ci --no-audit --no-fund
    npm run build
)
mv -- "$source_tree/frontend/dist" "$bundle/web"
(
    cd "$source_tree"
    ldflags="$(bash scripts/get_version.sh ldflags) -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn"
    CGO_ENABLED=1 GOWORK=off go build -mod=readonly -buildvcs=false -trimpath -tags sqlite_fts5 \
        -ldflags "$ldflags" -o "$bundle/WeKnora-lite" ./cmd/server
)
(
    cd "$source_tree/extensions"
    CGO_ENABLED=0 GOWORK=off go build -mod=readonly -buildvcs=false -trimpath \
        -o "$bundle/extension-host" ./cmd/extension-host
)
printf '%s\n' "$revision" > "$bundle/REVISION"
(
    cd "$bundle"
    sha256sum REVISION WeKnora-lite extension-host
    find web -type f -print0 | sort -z | xargs -0 sha256sum
) > "$bundle/SHA256SUMS"

mv -T -- "$bundle" "$output"
printf 'AgentRAG release %s built at %s\n' "$revision" "$output"
