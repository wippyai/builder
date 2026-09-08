# Standalone application implementation requirements

Status: development implementation with tested Linux assembly; not a stable released interface.

Implemented: pinned assembler and toolchain action, source-free Bee/hello boot,
base/bootstrap modes, canonical staged Hub updates, native I/O events and draft
release archives. The Hub protocol fixture verifies root and dependency updates
and retained selections. Explicit semantic native-version requirements, additional
platform acceptance, Bee Hub publication and complete upstream notice review
remain pending. Lint currently gates native module API/type compatibility.

## Ownership

- Runtime: public application boot, component composition, pack/deployment loading,
  normal Hub resolution and updates, source introspection, shutdown and recovery.
- Builder: validated manifest, pinned sources and Go dependency graph, generated
  small entry point and embedded assets, build provenance and release packaging.
- Application: Lua source and published package identity, native extensions,
  workspace data policy, admission and permissions, application acceptance tests.

Bee is the first consumer. The builder must also build a second, unrelated
fixture application through exactly the same manifest and execution path.
Do not embed Bee-specific names, startup code or modules in builder logic.

## Distribution and updates

The executable contains every application pack needed for first offline boot.
Packs preserve canonical module IDs, versions, metadata and dependency ownership.
Generated code calls a public runtime API; it does not copy private CLI boot code.

A fresh deployment is seeded atomically from the embedded graph. Existing state
is validated and retained. A selected update is authoritative on later launches.
Base mode retains an explicit bundled recovery option; bootstrap mode only seeds
initial state. Recovery must not silently downgrade code against newer workspace
migrations. Workspace databases and registry history remain separate.

Updates use the normal Hub client, authentication, resolver, digest verification,
lock format and admission boundaries. Failed download, verification or activation
must leave the previous deployment usable. Native requirements are checked before
activation; a Lua update cannot add a Go module. Native executable replacement
has a separate restart boundary. Do not promise live core replacement.

Keep application source introspection and canonical workspace replacements
available. Source editing, package discovery and installation never grant
application processes direct registry publication authority.

## Native I/O events

Bee needs an MIT-owned native module, initially supporting filesystem events.
Evaluate github.com/syncthing/notify (MIT), pin its exact revision, preserve its
license, and test its overflow and platform semantics before selecting it.
Use Wippy module types, scheduler yields/channels, error kinds, process-owned
resources and component lifecycle. No Lua callbacks from watcher goroutines.

Subscribe using an explicitly authorized filesystem resource and contained
relative path. Validate at the native boundary; import declarations do not grant
access. Bound watches and event queues, support cancellation and automatic owner
exit cleanup, and represent loss of synchronization explicitly. Filesystem
notifications are hints requiring reconciliation, not a durable ordered log.
Do not promise recursive or network-filesystem semantics without acceptance tests.

## Build and release gates

- Exact runtime revision, checksummed patches if temporarily required, locked Go
  dependencies and toolchain; no ambient Go workspace or dirty adjacent checkout.
- A small manifest and one reusable GitHub build path for applications.
- Local Makefile build, unit and integration checks used by CI.
- Native target builds with required C toolchains; validate Linux first and add
  macOS/ARM targets only with actual builds and platform tests. Windows needs its
  own terminal/application acceptance before being advertised.
- Offline first boot from a directory without source or preinstalled Wippy.
- Native component import, behavior, denied access and process-exit cleanup.
- Real Hub update to a newer test package, cold restart, embedded version retained
  according to mode, failed update recovery, incompatible native requirement denial.
- Bee source/pack acceptance plus standalone terminal, persistence and recovery.
- Release archives, checksums, build provenance, dependency/license notices, and
  GitHub release workflow with publication permissions scoped to the release job.
- Runtime changes prepared as focused PRs, preserving MPL-2.0 notices. Builder
  and Bee-owned code are MIT. No prior-project comparisons in code or comments.
