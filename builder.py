#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Build a standalone application from pinned Wippy and native components."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


class BuildError(ValueError):
    pass


def require(value, message):
    if not value:
        raise BuildError(message)


def matches(pattern, value):
    return isinstance(value, str) and re.fullmatch(pattern, value)


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def exact_keys(value, required, optional, where):
    require(isinstance(value, dict), f"{where} must be an object")
    require(set(required) <= value.keys(), f"{where} missing fields: {set(required) - value.keys()}")
    require(value.keys() <= set(required) | set(optional), f"{where} contains unknown fields")


def read_manifest(path):
    value = json.loads(path.read_text())
    exact_keys(value, ["schema", "name", "runtime", "application"], ["native"], "manifest")
    require(value["schema"] == 1, "unsupported manifest schema")
    require(isinstance(value["name"], str) and matches(r"[a-z][a-z0-9_-]*", value["name"]), "invalid executable name")
    runtime = value["runtime"]
    exact_keys(runtime, ["repository", "commit", "go", "tags"], ["patches"], "runtime")
    require(isinstance(runtime["repository"], str) and runtime["repository"].startswith("https://"), "runtime.repository must be an HTTPS URL")
    require(matches(r"[0-9a-f]{40}", runtime["commit"]), "runtime.commit must be an exact commit")
    require(matches(r"[0-9]+\.[0-9]+\.[0-9]+", runtime["go"]), "runtime.go must be an exact toolchain version")
    require(isinstance(runtime["tags"], list) and all(isinstance(t, str) and matches(r"[a-zA-Z0-9_]+", t) for t in runtime["tags"]), "invalid build tags")
    for patch in runtime.get("patches", []):
        exact_keys(patch, ["path", "sha256"], [], "patch")
        require(matches(r"[0-9a-f]{64}", patch["sha256"]), "patch requires SHA-256")
    app = value["application"]
    exact_keys(app, ["module", "command", "mode", "packs"], ["data_env"], "application")
    require(matches(r"[a-z][a-z0-9-]*/[a-z][a-z0-9-]*", app["module"]), "invalid application module")
    require(isinstance(app["command"], str) and bool(app["command"]), "application command is required")
    require(app["mode"] in ("base", "bootstrap"), "application.mode must be base or bootstrap")
    require(isinstance(app["packs"], list) and app["packs"], "application.packs must contain the offline graph")
    modules = set()
    for pack in app["packs"]:
        exact_keys(pack, ["module", "version", "path", "sha256"], [], "pack")
        require(matches(r"[a-z][a-z0-9-]*/[a-z][a-z0-9-]*", pack["module"]), "invalid pack module")
        require(matches(r"v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", pack["version"]), "pack version must be exact")
        require(matches(r"[0-9a-f]{64}", pack["sha256"]), "pack requires SHA-256")
        require(pack["module"] not in modules, "duplicate pack module")
        modules.add(pack["module"])
    require(app["module"] in modules, "root application pack is missing")
    require(isinstance(app.get("data_env", {}), dict), "application.data_env must be an object")
    for name, relative in app.get("data_env", {}).items():
        require(matches(r"[A-Z][A-Z0-9_]*", name), "invalid environment binding")
        require(isinstance(relative, str) and relative and not Path(relative).is_absolute() and ".." not in Path(relative).parts, "data path must be local")
    native_modules = set()
    for native in value.get("native", []):
        exact_keys(native, ["module", "version", "package", "factory"], ["private"], "native component")
        require(matches(r"[A-Za-z0-9._~/-]+", native["module"]) and "." in native["module"], "invalid native module path")
        require(native["module"] not in native_modules, "duplicate native module; expose one component factory per module")
        native_modules.add(native["module"])
        require(isinstance(native.get("private", False), bool), "native.private must be a boolean")
        require(matches(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", native["version"]), "native version must be exact")
        require(isinstance(native["package"], str) and (native["package"] == native["module"] or native["package"].startswith(native["module"] + "/")), "native package must belong to selected module")
        require(matches(r"[A-Z][A-Za-z0-9_]*", native["factory"]), "native factory must be an exported Go identifier")
    for item in runtime.get("patches", []) + app["packs"]:
        relative = item["path"]
        require(isinstance(relative, str) and relative and "\0" not in relative and not Path(relative).is_absolute() and ".." not in Path(relative).parts, "input paths must be local to the manifest")
    return value


def run(args, *, cwd=None, env=None):
    subprocess.run(args, cwd=cwd, env=env, check=True)


def go_string(value):
    return json.dumps(value, ensure_ascii=False)


def generate_main(manifest):
    app = manifest["application"]
    imports = ['"context"', '"fmt"', '"os"', '_ "embed"', '"github.com/wippyai/runtime/application"', '"github.com/wippyai/runtime/api/boot"']
    factories = []
    for index, component in enumerate(manifest.get("native", [])):
        imports.append(f'native{index} {go_string(component["package"])}')
        factories.append(f'native{index}.{component["factory"]}()')
    environment = ",".join(go_string(k)+":"+go_string(v) for k, v in app.get("data_env", {}).items())
    assets, packs = [], []
    for index, pack in enumerate(app["packs"]):
        assets.append(f'//go:embed packs/{index}.wapp\nvar pack{index} []byte')
        packs.append('{Module: '+go_string(pack["module"])+', Version: '+go_string(pack["version"])+', Digest: '+go_string('sha256:'+pack["sha256"])+f', Data: pack{index}'+'}')
    return '''// Code generated by Wippy Builder. DO NOT EDIT.
// SPDX-License-Identifier: MIT
package main
import (
'''+ '\n'.join(imports)+ '\n)\n'+ '\n'.join(assets)+ '''
func main() {
    err := application.Run(context.Background(), application.Options{
        Name: '''+go_string(manifest["name"])+''',
        Command: '''+go_string(app["command"])+''',
        Mode: '''+go_string(app["mode"])+''',
        DataEnv: map[string]string{'''+environment+'''},
        Components: []boot.Component{'''+','.join(factories)+'''},
        Bundle: application.Bundle{Root: '''+go_string(app["module"])+''', Packs: []application.Pack{'''+','.join(packs)+'''}},
    }, os.Args[1:])
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
'''


def generate_toolchain(manifest):
    imports = ['"context"', '"fmt"', '"os"', '"path/filepath"', '"github.com/wippyai/runtime/api/boot"', '"github.com/wippyai/runtime/cmd/wippy/cmd"']
    factories = []
    for index, component in enumerate(manifest.get("native", [])):
        imports.append(f'native{index} {go_string(component["package"])}')
        factories.append(f'native{index}.{component["factory"]}()')
    return """// Code generated by Wippy Builder. DO NOT EDIT.
// SPDX-License-Identifier: MIT
package main
import (
""" + '\n'.join(imports) + """
)
func main() {
    directory, err := os.Getwd()
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    config := []string{}
    if _, err := os.Stat(filepath.Join(directory, ".wippy.yaml")); err == nil {
        config = append(config, filepath.Join(directory, ".wippy.yaml"))
    } else if !os.IsNotExist(err) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    err = cmd.ExecuteWithOptions(context.Background(), cmd.ExecuteOptions{
        Args: os.Args[1:], LockFile: filepath.Join(directory, "wippy.lock"),
        ConfigFiles: config, Components: []boot.Component{""" + ','.join(factories) + """},
    })
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
"""


def build(manifest_path, output, *, toolchain=False):
    manifest_path = manifest_path.resolve()
    manifest = read_manifest(manifest_path)
    base = manifest_path.parent
    runtime = manifest["runtime"]
    # Verify all local inputs before fetching source or running a toolchain.
    for item in runtime.get("patches", []) + ([] if toolchain else manifest["application"]["packs"]):
        path = (base / item["path"]).resolve()
        require(path.is_file(), f"input does not exist: {path}")
        require(digest(path) == item["sha256"], f"input checksum mismatch: {path}")
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "GOWORK": "off", "GOTOOLCHAIN": "go"+runtime["go"], "CGO_ENABLED": "1", "GOFLAGS": ""}
    private_modules = [component["module"] for component in manifest.get("native", []) if component.get("private")]
    if private_modules:
        for key in ("GOPRIVATE", "GONOPROXY", "GONOSUMDB"):
            configured = subprocess.check_output(["go", "env", key], env=env, text=True).strip()
            env[key] = ",".join(filter(None, [configured, *private_modules]))
    with tempfile.TemporaryDirectory(prefix="wippy-build-") as temporary:
        stage = Path(temporary)
        source = stage / "runtime"
        run(["git", "clone", "--no-checkout", "--filter=blob:none", os.environ.get("WIPPY_BUILD_RUNTIME_REPOSITORY", runtime["repository"]), str(source)])
        run(["git", "checkout", "--detach", runtime["commit"]], cwd=source)
        for patch in runtime.get("patches", []):
            run(["git", "apply", "--check", str((base / patch["path"]).resolve())], cwd=source)
            run(["git", "apply", str((base / patch["path"]).resolve())], cwd=source)
        for component in manifest.get("native", []):
            run(["go", "mod", "edit", "-require="+component["module"]+"@"+component["version"]], cwd=source, env=env)
        entry = source / "cmd" / "assembled"
        (entry / "packs").mkdir(parents=True)
        for index, pack in enumerate([] if toolchain else manifest["application"]["packs"]):
            shutil.copyfile(base / pack["path"], entry / "packs" / f"{index}.wapp")
        (entry / "main.go").write_text(generate_toolchain(manifest) if toolchain else generate_main(manifest))
        if manifest.get("native"):
            run(["go", "mod", "tidy"], cwd=source, env=env)
        run(["go", "mod", "verify"], cwd=source, env=env)
        binary = stage / manifest["name"]
        run(["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-tags", ",".join(runtime["tags"]), "-o", str(binary), "./cmd/assembled"], cwd=source, env=env)
        provenance = {"schema": 1, "mode": "toolchain" if toolchain else "application", "manifest": manifest, "binary_sha256": digest(binary), "go_mod_sha256": digest(source / "go.mod"), "go_sum_sha256": digest(source / "go.sum")}
        with tempfile.NamedTemporaryFile(prefix=output.name+".", dir=output.parent, delete=False) as pending:
            pending_path = Path(pending.name)
        try:
            shutil.copy2(binary, pending_path)
            pending_path.replace(output)
        finally:
            pending_path.unlink(missing_ok=True)
        output.with_name(output.name+".provenance.json").write_text(json.dumps(provenance, indent=2)+"\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    build_parser = commands.add_parser("build")
    build_parser.add_argument("manifest", type=Path)
    build_parser.add_argument("--output", type=Path, required=True)
    toolchain_parser = commands.add_parser("toolchain")
    toolchain_parser.add_argument("manifest", type=Path)
    toolchain_parser.add_argument("--output", type=Path, required=True)
    seal_parser = commands.add_parser("seal")
    seal_parser.add_argument("manifest", type=Path)
    validate_parser = commands.add_parser("validate")
    validate_parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "build":
            build(args.manifest, args.output)
        elif args.command == "toolchain":
            build(args.manifest, args.output, toolchain=True)
        elif args.command == "seal":
            manifest = read_manifest(args.manifest)
            for pack in manifest["application"]["packs"]:
                pack["sha256"] = digest(args.manifest.parent / pack["path"])
            args.manifest.write_text(json.dumps(manifest, indent=2)+"\n")
        else:
            read_manifest(args.manifest)
    except (BuildError, OSError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"wippy-builder: {error}\n")


if __name__ == "__main__":
    main()
