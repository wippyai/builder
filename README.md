![Wippy Builder — standalone applications from pinned inputs](docs/assets/banner.png)

# Wippy Builder

[![Build checks](https://github.com/wippyai/builder/actions/workflows/check.yml/badge.svg)](https://github.com/wippyai/builder/actions/workflows/check.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-edbd59)](LICENSE)

Build a standalone Wippy executable from a pinned runtime, versioned application
packs, and native Go components. The generated entry point calls Wippy's
`application.Run` API.

[Quick start](#quick-start) · [Build inputs](#build-inputs) · [GitHub Actions](#github-actions) · [SDK](docs/SDK.md)

**Development preview.** Linux amd64 assembly and executable acceptance are
verified. The runtime host APIs are implemented in upstream PRs
[667](https://github.com/wippyai/runtime/pull/667) and
[668](https://github.com/wippyai/runtime/pull/668), pending review and merge.

## Quick start

Requires Go 1.27.0, Git, and a C compiler. From this checkout:

```sh
make tools

# Build Wippy with the native components selected by the manifest.
dist/wippy-builder toolchain examples/hello/wippy.build.json --output dist/wippy

# Lint and pack the example, then assemble its executable.
make example-pack WIPPY="$PWD/dist/wippy"
make build MANIFEST=examples/hello/wippy.build.json OUTPUT=dist/hello

./dist/hello run Ada
# Hello, Ada!
```

The executable contains the runtime and application packs. First boot seeds a
local deployment; later launches preserve installed application updates.
[The hello example](examples/hello) and [Bee](https://github.com/wippyai/bee) use
the same assembly path.

## Build inputs

[`wippy.build.json`](examples/hello/wippy.build.json) describes the complete build:

| Input | Selected by the manifest |
|---|---|
| Runtime | Git commit, Go version, build tags, and checksummed patches |
| Application | Module identity, command, base/bootstrap mode, and data paths |
| Packs | Exact module versions, local pack files, and SHA-256 checksums |
| Native components | Go module versions, import paths, and exported boot factories |

The builder copies inputs into staging, verifies their hashes, and checks out the
selected runtime commit. Go's resolved package owner and version must match each
native pin. Native modules use Wippy boot registration, typed Lua exports, and
process permissions.

UI code, assets, and published configuration belong in application packs.
Native components are compiled into the executable. The [SDK guide](docs/SDK.md)
covers both paths, including filesystem notifications and argument forwarding.

## Commands

| Command | Purpose |
|---|---|
| `validate MANIFEST` | Check manifest fields and version pins |
| `toolchain MANIFEST --output PATH` | Build Wippy with the selected native components |
| `pack MANIFEST --toolchain PATH` | Pack a source root and record its checksum |
| `seal MANIFEST` | Refresh checksums after intentionally replacing input packs |
| `build MANIFEST --output PATH` | Assemble the standalone application |
| `package BINARY --output ARCHIVE` | Verify and archive the build artifacts |

Run `wippy-builder COMMAND --help` for flags. Status output uses terminal colors
and honors `NO_COLOR`; redirected logs remain plain text.

`pack` prepares one self-contained source root. Multi-module builds supply
independently prepared dependency packs. Private native modules set `private: true`
and use the host's Git credentials. `WIPPY_BUILD_RUNTIME_REPOSITORY` can
select a local Git mirror; the manifest commit still determines the source.

## GitHub Actions

With packs prepared and their checksums recorded, add this step after checkout:

```yaml
- name: Build application
  uses: wippyai/builder@6109b4f13fa9e715d7e80e58e19840ae596a02f7
  with:
    manifest: wippy.build.json
    output: dist/my-app
```

The action installs Go and builds the assembler. Set `mode: toolchain` to produce
the native development runtime. Its `builder` output provides the assembler path
for subsequent packaging steps. Private dependencies can use the `token` input.

The [example workflow](.github/workflows/check.yml) demonstrates toolchain and pack
preparation, offline execution, Hub updates, base/bootstrap checks, and packaging.
Bee's [release workflow](https://github.com/wippyai/bee/blob/feat/native-ioevents/.github/workflows/native.yml)
adds desktop acceptance and tag-triggered draft releases.

## Release artifacts

```sh
dist/wippy-builder package dist/hello --output dist/hello-linux-amd64.tar.gz
```

The archive contains the executable, provenance, effective `go.mod` and `go.sum`,
available dependency notices, and runtime patch sources. A separate SHA-256 file
covers the archive.

Provenance records the manifest, assembler revision, source modification status,
and artifact hashes. Packaging verifies a snapshot of every artifact before
writing the archive atomically. Archive ownership and timestamps are normalized;
binary bytes also depend on pack timestamps and the C toolchain.

## Updates

Hub updates replace the installed application pack graph. Base mode provides
explicit recovery from embedded code; bootstrap mode seeds only the first
deployment. Application databases retain their normal migration checks.

Native changes require a new executable. The runtime update gate checks Lua
exports and types against the compiled modules. Semantic native-version
requirements and additional platform acceptance remain pending.

## Development

See [releasing](docs/RELEASING.md) for local archives, platform checks and the
GitHub draft-release protocol.

```sh
make check
make smoke OUTPUT=dist/hello
```

`make check` runs race tests, vet, and formatting checks. Executable acceptance
covers empty-directory boot, exact argument forwarding, Hub updates, restart,
base recovery, and failed-update preservation.

Code lives in [`cmd/wippy-builder`](cmd/wippy-builder) and
[`internal/assemble`](internal/assemble). See the [implementation guide](docs/IMPLEMENTATION.md)
for ownership, validation, and remaining release work.

## License

Builder is [MIT licensed](LICENSE). Applications, Wippy, and native dependencies
retain their own licenses. The generated notice inventory lists missing root
license files for review before public distribution.
