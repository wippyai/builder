# Documentation

| Topic | Guide |
|---|---|
| Build an application and use the CLI | [Quick start](../README.md#quick-start) |
| Packs, UI, native modules and argument forwarding | [SDK](SDK.md) |
| Manifest validation, staging and artifact ownership | [Implementation](IMPLEMENTATION.md) |
| Platform checks, version tags and draft releases | [Releasing](RELEASING.md) |
| GitHub protections and credential handling | [GitHub setup](GITHUB.md) |
| Development and pull requests | [Contributing](../CONTRIBUTING.md) |
| Vulnerabilities and credential handling | [Security](../SECURITY.md) |

The [hello example](../examples/hello) contains a complete build manifest.
It uses the merged upstream application host. Runtime changes belong in Wippy;
Builder configures its APIs and the application's native components.
