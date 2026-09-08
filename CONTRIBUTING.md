# Contributing to Builder

Read the [implementation guide](docs/IMPLEMENTATION.md) and [SDK](docs/SDK.md)
before changing manifest validation, generated entry points, or native module
registration. Follow the [Wippy code of conduct](https://github.com/wippyai/.github/blob/main/.github/CODE_OF_CONDUCT.md).

## Development

Install the Go version in `go.mod`. Application assembly also needs Git and a C
compiler. Use the Makefile:

```sh
make check
```

This runs race tests, vet, formatting checks, workflow lint and secret scanning.
For changes to assembly or the runtime host, also build the
[hello example](README.md#quick-start) and run `make smoke OUTPUT=dist/hello`.
Hub integration acceptance uses a local test service; it does not upload to the
production Hub.

Preserve exact version pins, hash validation, literal argument forwarding, and
atomic artifact replacement. Add tests for changed input boundaries and failure
handling. Keep tools and implementation in Go. Update the SDK or implementation
guide when a public contract changes.

## Pull requests

Describe the problem, resulting behavior, and checks performed. Keep source,
workflow and documentation changes focused on the same outcome. Main requires CI,
a review and resolved conversations. The [release protocol](docs/RELEASING.md)
describes the separate CLI and application artifacts.

Builder-owned contributions use [MIT](LICENSE). Preserve upstream licenses and
dependency notices. Follow [SECURITY.md](SECURITY.md) for vulnerabilities or exposed
credentials.
