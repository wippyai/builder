# Application and native module SDK

This is the authoring contract for the pinned development runtime. The runtime
owns the Go APIs, Lua types, scheduler and boot lifecycle. Builder selects and
compiles those APIs.
The application host is pending upstream in runtime
[PR 787](https://github.com/wippyai/runtime/pull/787). Go integrations require the
documented runtime revision.

## Choose what goes into a pack

| Input | Configuration | Update boundary |
|---|---|---|
| Lua application, libraries and UI | Source registry entries and their imports | Versioned application pack |
| Static UI assets | `wippy.yaml` `embed` selection and filesystem entries | Versioned application pack |
| Published runtime defaults and profiles | `wippy.yaml` `publish` allow-lists | Versioned application pack |
| Command and state-relative data paths | `wippy.build.json` `application` | New executable |
| Native Go components and their Lua exports | `wippy.build.json` `native` | New executable |
| Runtime revision, patches and build tags | `wippy.build.json` `runtime` | New executable |
| User preferences and application databases | Application-owned persistence | Preserved across code updates |

UI composition uses application code and registry configuration. Include the
desired UI in the pack and declare
its normal imports, processes and resources. For Bee, preserve the host's app
admission boundary when changing the bundled app selection.

Packing uses Wippy's `pack` command. Its `wippy.yaml` configuration selects
embedded assets and publishable configuration. For example, a source
application can opt in to shipping its shutdown defaults:

```yaml
# wippy.yaml
organization: example
module: hello
publish:
  runtime:
    source: .wippy.yaml
    sections: [shutdown]
```

`publish.profiles` selects the profile source and included profile names;
`publish.runtime.vars` selects publishable variable declarations. Check the
[runtime configuration contract](https://github.com/wippyai/runtime/blob/b8c7a9324256dd40a034f29c4a8b25457587fceb/boot/deps/config/config.go)
for the exact fields. Runtime-selected deployment and history paths take
precedence over pack settings. Credentials and machine-specific paths belong in
host configuration. Application-specific settings need their own typed decoder.

`pack` prepares one self-contained source root. A manifest with multiple modules
supplies independently prepared packs for its complete dependency graph.

## Compile a native module

A native package exports a zero-argument factory returning `boot.Component`:

```go
func Component() boot.Component
```

Select that package in the build manifest. This fragment assumes the module has
actually published the named version:

```json
{
  "native": [{
    "module": "example.com/team/native",
    "version": "v1.0.0",
    "package": "example.com/team/native/fsnotify",
    "factory": "Component"
  }]
}
```

The builder verifies the package's actual Go module and selected version. Set
`private: true` for a private module. A manifest can select multiple distinct
package and factory pairs from one Go module, provided every selection pins the
same module version. Every factory call must create its own service state.

Use these runtime-owned primitives:

| Responsibility | Go API |
|---|---|
| Component identity, ordering and lifecycle | `api/boot.Component`, `boot.New(boot.P{...})` |
| Declare Lua dependency | `boot/components/runtime/lua.EngineName` |
| Register values and linter types together | `boot/components/runtime/lua.GetCodeManager`, `AddModules` |
| Module exports, classifications and yields | `api/runtime/lua.ModuleDef`, `YieldType` |
| Typed exports | `go-lua/types/io.Manifest`, `go-lua/types/typ` |
| Register asynchronous operations | `api/dispatcher`, `boot/components/dispatchers.DispatcherName` |
| Filesystem resource lookup | `api/fs.GetRegistry`, `Registry.GetFS` |
| Current process identity | `api/runtime.GetFramePID` |

Import paths in the table are relative to `github.com/wippyai/runtime`, except
the type packages, which are under `github.com/wippyai/go-lua`.

In `Load`, check required services, register command handlers, then register the
module through `AddModules`. Declare each dependency in `DependsOn`. Fail boot
on registration errors. Use `Start` for activation and `Stop` to cancel and join
owned work. A shared `ModuleDef` may describe immutable exports; mutable service
state belongs to the component instance.

The generated entry point passes selected components to `app.Main`. A native
entry marked `"host": true` is instantiated once and reused as both the
executable host and a boot component.
The runtime rejects duplicate component names, including built-in replacements.
Source tools use the same native selection as the application executable so
lint and pack generation see the same exports.

Lua entries declare the module explicitly:

```yaml
modules: [ioevents]
```

They can then use `require("ioevents")`. An import declares availability; the
host separately selects permissions for the process. Publish accurate module
types, optional/error returns and nondeterministic I/O classifications.

## Asynchronous events and filesystem authority

The working reference implementation is Bee's
[I/O events component](https://github.com/wippyai/bee/tree/70917fa75697/native/ioevents).
It connects an MIT-licensed notification backend to named Wippy filesystem
resources and normal typed process channels:

```lua
local ioevents = require("ioevents")
local watch, err = ioevents.watch("workspace:files", ".")
if not watch then
    error(err)
end
local events = watch:channel()
-- Receive or select through Wippy channels, then reconcile directory contents.
-- watch:close() cancels early; process exit also releases the watch.
```

The host must grant both `fs.get` and `ioevents.watch` for that resource. The
native boundary resolves the registered filesystem and validates relative paths
inside its root. The current backend requires `api/fs.HostPathFS`, watches one
directory, bounds watches and queues, and emits change hints plus rescan events.
Unsupported providers return an error. Consumers must tolerate lost and coalesced
notifications. If runtime message retention overflows, the channel closes with an
error and the producer stops; reopen the watch and rescan to recover.

The event adapter currently uses exported runtime implementation APIs:
`runtime/lua/engine` subscriptions, subscription frames and channel types,
`runtime/lua/engine/value` userdata, and `runtime/security.IsAllowed`. These are
**revision-coupled integration APIs**. Pin and test them with the selected runtime.

Yield blocking setup through the dispatcher. Route Go payloads through the
runtime relay; construct Lua values only on the scheduler. Use subscription
epochs and generations, bounded retention and process-owned cancellation. Test
owner exit, stale delivery, denied permissions, overload and component shutdown.

Attaching watching directly to `filesystem:watch()` requires a runtime API
extension. Its design needs an
optional provider capability, resource identity for authorization, defined
unsupported-provider behavior and scheduler-owned cancellation. It should be
reviewed in the runtime with provider and permission tests. Do not mutate the
shared `fs` Lua table or userdata metatable from an external boot component.

## Build, verify and update

```sh
make check tools
dist/wippy-builder toolchain wippy.build.json --output dist/wippy
dist/wippy-builder pack wippy.build.json --toolchain dist/wippy
dist/wippy-builder build wippy.build.json --output dist/my-app
dist/wippy-builder package dist/my-app --output dist/my-app-linux-amd64.tar.gz
```

Run application lint and acceptance using the generated source toolchain before
assembly. Verify real native behavior and denied permissions, then start the
assembled executable from an empty working directory. The reference consumer
provides these checks through Bee's `make native-check native-binary-check`.

Host flags precede the operation; application arguments follow `run`:

```sh
./dist/my-app --state /path/to/state run --app-option "value with spaces" ""
./dist/my-app --state /path/to/state recover
./dist/my-app --state /path/to/state wippy run --silent -- another-command argument
```

Arguments are forwarded as Go argument slices through the runtime CLI.
Executable acceptance covers
empty values, newlines, Unicode, quotes, `--`, and host-looking application flags.

Hub updates replace the selected pack graph, which later boots preserve. They
cannot install Go code. `recover` starts the embedded graph without rolling back
user databases.
The update lint gate detects missing exports and incompatible Lua types;
semantic native-module version requirements are not implemented yet.
