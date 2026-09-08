# Releasing Builder

Builder versions independently from applications and native Go modules. Its
release tag is `vMAJOR.MINOR.PATCH` in this repository. Bee uses `v…` for its
executable and `native/v…` for its nested native module in the Bee repository.

## Local release

Install GoReleaser v2.18.1 and the Go version in `go.mod`, then run:

```sh
make release RELEASE_VERSION=0.1.0-dev
```

This runs race tests, vet and formatting checks, validates the release
configuration, and creates snapshot archives and `SHA256SUMS` in `dist/`.
It does not publish. Each archive contains the CLI, README and MIT license.
Linux and macOS use tar.gz; Windows uses zip. All three have amd64 and arm64
builds. The CLI uses pure Go; assembling a Wippy application also requires the
application's native compiler and dependencies.

## Pull requests and tags

Main requires the `Builder CI` check, one approving review, resolved conversations
and an up-to-date branch. Stale approvals are dismissed. Administrators follow
the same rules; force pushes and branch deletion are disabled. Merge with squash
or rebase. The aggregate check requires Linux race/format/vet checks, CLI tests
on every release platform, and Linux application acceptance covering offline boot,
argument forwarding, Hub updates and bootstrap mode.

`make check` includes `make repository-check`: actionlint validates workflows,
and Gitleaks scans history and current files with redacted output and a Wippy Hub
token rule. Standalone assembly waits for this gate. Dependabot groups weekly
Actions and Go dependency updates to limit PR runs. Actions default to read-only
permissions and require full commit pins; checkout does not retain credentials.

After merging and choosing a version, an administrator creates a `v…` tag on the reviewed main
commit. Release tags cannot be moved or deleted. The release workflow checks
that the commit belongs to main, reruns CI, and uses pinned GoReleaser to create
a **draft** GitHub release. Review its assets and notes before publication.
Development checks create no tags. Manual release runs require an existing tag.

## Application artifacts

`wippy-builder package` packages an assembled application's verified executable,
provenance, effective Go module files, dependency notice inventory and runtime
patch sources. These are separate from Builder's own CLI archives. The inventory
uses the executable's Go build metadata to select linked modules and collects
their root license documents. Source files such as `license_test.go` are excluded.
Missing source metadata fails the build; missing license documents are listed
for review. This inventory does not establish licensing for bundled native
sources with separate terms. Resolve missing notices before public distribution.
Application source selection and embedded base/bootstrap mode belong to the
application manifest. Native component updates require rebuilding the executable.

Packing runs Wippy syntax and strict type checking with the selected development
toolchain. Application builds run the same checks against the embedded packs
using the freshly compiled executable in disposable state before exporting any
artifacts. Build applications on their target platform so this validation can run.
The optional runtime style rules are separate; their current identical-expression
rule incorrectly rejects NaN guards. Go code must pass race tests, vet and gofmt.

The GitHub action requires a full commit SHA. It embeds that revision when its
downloaded source has no Git metadata. Normal checkout builds derive revision
and modification status from Go's VCS build information.

Signing and Hub publication require separate configuration. Store private keys
in restricted secret storage; they are never release assets or pack inputs.

The repository is currently private. Action sharing permits use within the Wippy
organization; public source and anonymous release downloads require public
visibility. There is no separate GitHub Pages site. See [repository setup](GITHUB.md)
for security settings and [contributing](../CONTRIBUTING.md) for review conventions.
