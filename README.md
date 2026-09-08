# Wippy Builder

Assemble standalone Wippy applications from a pinned runtime, application packs,
and native Go components.

Implementation is in progress. The local builder and unrelated hello fixture
have been built and run; there is no stable release yet.
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

See [implementation requirements](docs/IMPLEMENTATION.md).

## Local use

Python 3.12, Git, the manifest's Go toolchain, and a C compiler are required.

```sh
make check
python3 builder.py validate path/to/wippy.build.json
python3 builder.py build path/to/wippy.build.json --output dist/my-app
```

The manifest lists exact pack identities, versions and SHA-256 checksums, a
runtime commit and Go version, optional checksummed runtime patches, and native
Go module versions with exported component factories. See
[the hello manifest](examples/hello/wippy.build.json).

After intentionally regenerating input packs, `python3 builder.py seal MANIFEST`
refreshes their checksums. Normal builds only verify checksums. A build emits the
executable and a JSON provenance record. `WIPPY_BUILD_RUNTIME_REPOSITORY` may
select a local Git mirror for development; the builder still checks out the exact
manifest commit and never consumes the mirror's working files.

The composite GitHub action accepts `manifest` and `output` inputs. Consumers
should pin this repository to a reviewed commit. Platform release workflows and
full Bee/native-module acceptance are still being implemented.

Use `builder.py toolchain MANIFEST --output dist/wippy` to build the same native
component selection for source linting, tests and pack generation. This step does
not require pack files to exist yet. After packing, seal the input hashes and
build the application executable. Native modules from private repositories must
set `private: true`; the builder adds only those module prefixes to Go's private
fetch/checksum configuration and uses normal Git credential handling.
