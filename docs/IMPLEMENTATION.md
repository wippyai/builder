# Implementation and release status

The development implementation assembles Bee and a separate hello application
on Linux amd64. Both use the same manifest and runtime host API.

## Ownership and repository layout

| Location | Responsibility |
|---|---|
| `cmd/wippy-builder` | Cobra commands, flags, help and status output |
| `internal/assemble` | Manifest validation, source preparation, Go builds and packaging |
| `examples/hello` | Standalone application and executable acceptance fixture |
| `action.yml` | Reusable GitHub action |
| `.github/workflows/check.yml` | Builder tests, application acceptance and archive verification |
| Runtime `cmd/app` package | Embedded deployment, command dispatch, updates and recovery |
| Application repository | Lua/UI code, native extensions, permissions and acceptance tests |

The generated entry point calls `app.Main`. Development toolchains call
`cmd.ExecuteWithOptions`. Runtime boot components register native services and
typed Lua modules. See the [SDK guide](SDK.md) for the authoring APIs and their
revision requirements.

## Build inputs and outputs

A manifest selects an exact runtime commit, Go toolchain, build tags,
versioned application packs and native component factories.
The assembler copies pack inputs into staging and verifies their
hashes before invoking build tools. Git operates on a temporary checkout. Go
workspaces and ambient build flags are disabled.

Runtime changes belong upstream. Manifest decoding rejects `runtime.patches`;
the assembler compiles the selected runtime with a generated command entrypoint.

Go's selected package owner and module version must match each native pin.
The build emits an executable, provenance, effective Go module files, available
dependency notices. Packaging verifies a snapshot of
that artifact set and writes an archive and checksum. Archive metadata is
normalized; binary bytes also depend on pack timestamps and the C toolchain.

The license inventory selects Go modules recorded in the executable and resolves
their source directories from the effective module graph, including replacements.
It includes root license documents in common text formats and lists missing ones.
Source files named after licenses are excluded. Missing linked-module source
metadata fails the build before artifacts are exported.

## Application deployment

Embedded packs include the graph required for first boot. Packs retain their
module identities, versions and dependency metadata. Initial boot creates a
Wippy lock and vendor deployment; subsequent boots preserve installed selections.

The standalone `update` operation stages the selected deployment, invokes Wippy's
Hub resolver and linter, verifies pack digests and activates the result after
success. Failed updates retain the previous selection. The advanced
`runtime update` command modifies the selected deployment directly.

`recover` boots shipped code with separate registry history without changing the
installed selection. It preserves application databases; the application's
migration checks govern compatibility with older code.
Activation requires a restart. Native code changes require a new executable.

## Validation

`make check` runs workflow lint, secret scanning, Go race tests, vet and formatting checks. Tests cover manifest
validation, generated source, input protection, exact native dependency ownership,
atomic file writes, artifact tampering and archive metadata.

Executable acceptance covers source-free boot, exact argument forwarding,
Hub root and dependency updates, cold restart, recovery and failed-update
preservation. CI runs first-boot acceptance with
networking disabled.

Bee's consuming workflow adds typed Lua checks, filesystem permission and event
tests, and desktop acceptance for Terminal, Settings recovery and F12. Version
tags prepare draft releases. Builder and Bee-owned code are MIT; Wippy retains
MPL-2.0 and dependencies retain their own licenses.

## Remaining release work

- Native event adapters use revision-coupled engine APIs.
- Update lint checks exports and Lua types; semantic native-version requirements
  remain unimplemented.
- CLI checks cover Linux, macOS and Windows on amd64 and arm64. Standalone
  application acceptance runs on Linux; Bee adds Linux and macOS desktop checks.
- Bee's Hub publication workflow is implemented. A completed production upload,
  Bee update proof and in-app installation remain separate acceptance work.
- Stable distribution requires complete upstream license notices and review.
