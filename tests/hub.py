#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Run the runtime's Hub wire acceptance against the assembled fixture."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

binary, manifest_path = map(lambda value: Path(value).resolve(), sys.argv[1:])
manifest = json.loads(manifest_path.read_text())
runtime = manifest["runtime"]
pack = manifest["application"]["packs"][0]
env = {**os.environ, "GOWORK": "off", "GOTOOLCHAIN": "go" + runtime["go"],
       "WIPPY_TEST_APPLICATION_BINARY": str(binary),
       "WIPPY_TEST_APPLICATION_PACK": str(manifest_path.parent / pack["path"])}
with tempfile.TemporaryDirectory(prefix="builder-hub-") as temporary:
    subprocess.run(["git", "clone", "--no-checkout", "--filter=blob:none", runtime["repository"], temporary], check=True)
    subprocess.run(["git", "checkout", "--detach", runtime["commit"]], cwd=temporary, check=True)
    subprocess.run(["go", "test", "-tags", ",".join(runtime["tags"]), "./application", "-run", "^TestHubBinaryUpdate$", "-count=1", "-v"], cwd=temporary, env=env, check=True)
