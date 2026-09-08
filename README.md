# Wippy Builder

Assemble standalone Wippy applications from a pinned runtime, application packs,
and native Go components.

The assembler builds Bee and an unrelated hello fixture through the same manifest.
Source-free boot, Hub protocol updates, restart and base/bootstrap modes have local
acceptance checks and GitHub CI; there is no stable release yet.
The runtime boot and deployment APIs are being prepared upstream; this repository
must consume those APIs rather than maintain a second runtime or Hub resolver.

## Intended application manifest

An application selects its executable name, exact runtime revision and build
profile, versioned Hub root, bundled dependency graph, native component factories,
and whether the embedded application is a recoverable base or a bootstrap seed.
Native components are compiled into the executable. They retain normal Wippy
registration, typed module declarations, scheduler integration and host-selected
permission checks.

The embedded pack is a deployment input. Starting an already initialized
application uses its installed lock graph, including explicit Hub updates; it
must never overwrite an updated application just because a bundled pack exists.

See the [application and native module SDK](docs/SDK.md) for pack configuration,
boot registration, typed modules, filesystem events and argument passing, and
[implementation requirements](docs/IMPLEMENTATION.md) for the release boundary.

## Local use

Go, Git and a C compiler are required. The assembler uses Wippy's Cobra CLI library
and the Go standard library, and selects the runtime toolchain pinned by the manifest.

```sh
make check tools
dist/wippy-builder validate path/to/wippy.build.json
dist/wippy-builder build path/to/wippy.build.json --output dist/my-app
```

The manifest lists exact pack identities, versions and SHA-256 checksums, a
runtime commit and Go version, optional checksummed runtime patches, and native
Go module versions with exported component factories. See
[the hello manifest](examples/hello/wippy.build.json).

After intentionally regenerating input packs, `dist/wippy-builder seal MANIFEST`
refreshes their checksums. Normal builds only verify checksums. A build emits the
executable, JSON provenance, module files, license inventory and runtime patches. `WIPPY_BUILD_RUNTIME_REPOSITORY` may
select a local Git mirror for development; the builder still checks out the exact
manifest commit and never consumes the mirror's working files.

The composite GitHub action accepts `manifest` and `output` inputs. Consumers
should pin this repository to a reviewed commit. Bee supplies a consuming Linux amd64 workflow with foundation/native acceptance,
offline PTY checks, archives and tag-triggered draft releases.

Use `dist/wippy-builder toolchain MANIFEST --output dist/wippy` to build the same native
component selection for source linting, tests and pack generation. This step does
not require pack files to exist yet. After packing, seal the input hashes and
build the application executable. Native modules from private repositories must
set `private: true`; the builder adds only those module prefixes to Go's private
fetch/checksum configuration and uses normal Git credential handling.

## Release artifacts

`dist/wippy-builder package dist/my-app --output dist/my-app-linux-amd64.tar.gz`
collects the executable, manifest provenance, effective Go module graph, dependency
license inventory and runtime patches, and emits a SHA-256 checksum file. Archive
ownership and timestamps are normalized. This does not promise identical binaries:
application pack timestamps and the native C toolchain also affect build bytes.

The inventory includes the Go license and available module license files; modules
without root license files are explicitly listed for downstream review. Application
publishers must retain their own license and resolve missing upstream notices before
public distribution. The builder verifies that Go's selected native versions equal
the manifest; a dependency upgrade or replacement cannot silently change them.

The CLI emits terminal-aware status colors and honors `NO_COLOR`. Redirected logs
remain plain text. `wippy-builder pack MANIFEST --toolchain PATH --version VERSION`
prepares a self-contained source pack and seals its checksum; dependency packs
are supplied independently in a multi-module manifest.

The artifact set has one named definition shared by build validation, provenance
and packaging. Packaging snapshots every file, verifies the recorded hashes, then
writes the archive atomically. Provenance records the assembler's Git revision and
whether its source was modified. Native imports are checked against the Go module
that actually owns them, including nested-module and replacement rejection.
