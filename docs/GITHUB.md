# GitHub setup

The repository is public. Its documentation entry point is
[docs/README.md](README.md); no GitHub Pages site is configured.

## Protection and automation

| Setting | Configuration |
|---|---|
| Main | Pull request, one code-owner review, stale approval dismissal, resolved conversations, current base |
| Required checks | `Builder CI`, produced by GitHub Actions |
| Administrators | Main protection applies |
| History | Linear; force pushes and deletion blocked |
| Release tags | Creation limited to administrators; updates and deletion blocked for everyone |
| Default Actions token | Read-only; cannot approve pull requests |
| External actions | Full commit SHA required |
| Secret protection | GitHub scanning and push protection enabled |
| Dependencies | Vulnerability alerts and security updates enabled; weekly grouped version updates |

CODEOWNERS names the repository maintainers. Pull requests use local issue and
review templates, [contribution guidance](../CONTRIBUTING.md), and the
[organization code of conduct](https://github.com/wippyai/.github/blob/main/.github/CODE_OF_CONDUCT.md).
See [SECURITY.md](../SECURITY.md) for private reports.

## Credential boundary

Builder has no repository secrets. Its public action accepts a GitHub token for
private dependency fetching and otherwise uses the caller's workflow token. The
token is passed through the process environment; manifests and build provenance
do not record it.

Checkout steps set `persist-credentials: false`. Write access is limited to
release jobs that create draft releases. The repositories have no deploy keys or
webhooks. No signing key is configured.

## Verification

On 2026-09-08, Gitleaks v8.30.1 found no credentials in 20 reachable commits,
including available PR heads. The scan used default provider rules plus a Wippy
Hub token rule. GitHub secret and dependency alert lists were empty when checked.
These are scan results for the inspected material, not a guarantee against every
possible secret format.

`make repository-check` validates workflows and scans Git history and current
files with redacted output. It is part of the required CI and local release gates.

## Release boundaries

Builder creates draft releases after its required platform checks pass. Version tags
must belong to main and use the documented semantic version format. See
[releasing](RELEASING.md) for artifact contents and publication.

Anonymous installation requires a public release repository. Executable signing
and signed build attestations are not configured. The Hub update acceptance fixture uses a
local test service; it does not establish production Hub access.
