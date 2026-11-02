#!/usr/bin/env python3
"""Run trusted native snapshot verification inside the existing owned UI fixture."""
import os
from pathlib import Path
import secrets
import subprocess

import lifecycle_ui_e2e as lifecycle


def verify_source(http, key, kb, doc, work):
    nonce = secrets.token_hex(24)
    (work / ".source-e2e-owned").write_text(nonce, encoding="utf-8")
    env = dict(os.environ, AGENTRAG_SOURCE_E2E_KEY=key,
               AGENTRAG_SOURCE_E2E_ORIGIN=http.base_url,
               AGENTRAG_SOURCE_E2E_TENANT=str(kb["tenant_id"]),
               AGENTRAG_SOURCE_E2E_KB=kb["id"], AGENTRAG_SOURCE_E2E_DOC=doc["id"],
               AGENTRAG_SOURCE_E2E_OWNED_DIR=str(work), AGENTRAG_SOURCE_E2E_NONCE=nonce,
               CGO_ENABLED="0", GOWORK="off")
    subprocess.run(["/usr/local/go/bin/go", "test", "-mod=readonly", "./lineage",
                    "-run", "^TestNativeSourceOwnedIntegration$", "-count=1", "-v"],
                   cwd=Path(__file__).resolve().parents[1], env=env, check=True)


if __name__ == "__main__":
    lifecycle.main(verify_source)
