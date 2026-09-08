#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Run a built fixture without source, developer tools, or user caches."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

binary = Path(sys.argv[1]).resolve()
with tempfile.TemporaryDirectory(prefix="wippy-standalone-") as temporary:
    root = Path(temporary)
    cwd = root / "empty directory"
    cwd.mkdir()
    state = root / "application state"
    env = {**os.environ, "HOME": str(root), "XDG_CONFIG_HOME": str(root / "config"), "PATH": "/nonexistent"}
    cases = [(["run", "Ada"], "Hello, Ada!"), (["run", "Again"], "Hello, Again!"), (["--base", "run", "Recovery"], "Hello, Recovery!")]
    bootstrap = "--bootstrap" in sys.argv[2:]
    if bootstrap:
        cases.pop()
    for args, expected in cases:
        result = subprocess.run([str(binary), "--state-dir", str(state), *args], cwd=cwd, env=env, text=True, capture_output=True, timeout=20)
        if result.returncode or expected not in result.stdout:
            raise SystemExit(f"Standalone run failed: {result.returncode}\n{result.stdout}\n{result.stderr}")
    if bootstrap:
        result = subprocess.run([str(binary), "--state-dir", str(state), "--base"], cwd=cwd, env=env, text=True, capture_output=True, timeout=20)
        if result.returncode == 0 or "bootstrap applications do not expose a base deployment" not in result.stderr:
            raise SystemExit("Bootstrap unexpectedly exposed base recovery: " + result.stderr)
    if list(cwd.iterdir()):
        raise SystemExit("Standalone runtime wrote application state into the caller directory")
    if not (state / "deployment" / "wippy.lock").is_file():
        raise SystemExit("Missing canonical deployment lock")
print("Standalone source-free boot and restart passed; " + ("bootstrap rejects base recovery" if bootstrap else "base recovery passed"))
