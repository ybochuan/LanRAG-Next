"""Check the standalone Extension module using read-only Go tool queries."""

import argparse
import json
from pathlib import Path
import subprocess
import sys


EXPECTED_MODULE = "github.com/ybochuan/LanRAG-Next/extensions"
CORE_MODULE = "github.com/Tencent/WeKnora"


def run_go(module_dir, *arguments):
    return subprocess.run(
        ["go", *arguments],
        cwd=module_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        timeout=60,
        check=True,
    ).stdout


def reject_core(path):
    if path == CORE_MODULE or path.startswith(CORE_MODULE + "/"):
        raise ValueError(f"forbidden module or import: {path}")


def check_boundary(module_dir):
    environment = json.loads(run_go(module_dir, "env", "-json", "GOMOD", "GOWORK"))
    if environment.get("GOWORK") not in ("", "off"):
        raise ValueError("active Go workspace: run the Extension as a standalone module")
    manifest = module_dir / "go.mod"
    if Path(environment["GOMOD"]).resolve() != manifest.resolve():
        raise ValueError("unexpected go.mod path: module directory is not independent")

    module = json.loads(run_go(module_dir, "mod", "edit", "-json"))
    if (module.get("Module") or {}).get("Path") != EXPECTED_MODULE:
        raise ValueError("unexpected module identity")
    for requirement in module.get("Require") or []:
        reject_core(requirement["Path"])
    for replacement in module.get("Replace") or []:
        reject_core(replacement["Old"]["Path"])
        target = replacement["New"]
        reject_core(target["Path"])
        if not target.get("Version"):
            local_manifest = (module_dir / target["Path"] / "go.mod").resolve()
            if local_manifest.is_file():
                local = json.loads(run_go(module_dir, "mod", "edit", "-json", str(local_manifest)))
                reject_core((local.get("Module") or {}).get("Path", ""))

    dependencies = run_go(
        module_dir, "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}}", "./..."
    ).splitlines()
    for dependency in dependencies:
        reject_core(dependency)
    return len(dependencies)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--module-dir", type=Path, default=Path(__file__).resolve().parents[1])
    arguments = parser.parse_args()
    try:
        count = check_boundary(arguments.module_dir.resolve())
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f"module boundary failed: {error}", file=sys.stderr)
        return 1
    print(f"module boundary passed: {count} imports, no WeKnora dependency or active workspace")
    return 0


if __name__ == "__main__":
    sys.exit(main())
