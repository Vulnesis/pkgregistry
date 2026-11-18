# pkgregistry

`pkgregistry` is a read-only Go client for public package registries. It lists a
package's versions, resolves the latest stable version, reads a specific
version's declared dependency ranges, and recommends a transitive
parent-upgrade target across ecosystems.

The library is shared by vdb-api (live SCA enrichment and cross-ecosystem
transitive parent-upgrade resolution) and package-firewall (metadata filtering),
so the fetch and parse logic for each registry lives in exactly one place.

## Supported ecosystems

`pkgregistry` ships one client per ecosystem, selected by the lowercase purl /
VDB identifiers:

| Constant | Ecosystem | Backend |
| --- | --- | --- |
| `EcosystemNPM` | `npm` | npm registry |
| `EcosystemPyPI` | `pypi` | PyPI JSON API |
| `EcosystemCargo` | `cargo` | crates.io sparse index |

Requesting any other ecosystem returns `ErrUnsupportedEcosystem`.

## Quick start

```go
import "github.com/vulnesis/pkgregistry"

r := pkgregistry.New()
client, err := r.Client(pkgregistry.EcosystemNPM)
if err != nil {
    // pkgregistry.ErrUnsupportedEcosystem
}

versions, err := client.Versions(ctx, "express")
latest, err := client.Latest(ctx, "express")
meta, err := client.VersionMetadata(ctx, "express", "4.18.2")
```

## Main types

- `Registry` holds shared configuration (HTTP client, optional persister, mirror
  overrides, user agent) and hands out per-ecosystem `Client`s.
- `Client` is the per-ecosystem reader: `Ecosystem()`, `Versions`, `Latest`, and
  `VersionMetadata`.
- `Version` is one published release: version string, publish time (nil when the
  registry does not expose one), yanked, and prerelease flags.
- `VersionMeta` is metadata for a single version, including its declared
  dependency ranges.
- `DeclaredDep` is a dependency a package version declares, with its version
  range and scope (`runtime`, `dev`, `peer`, or `optional`).
- `Persister` optionally records fetched version lists and declared ranges.
  The library imports no database driver and knows no schema; persistence is
  entirely the caller's.

Errors: `ErrUnsupportedEcosystem` (no client for the requested ecosystem) and
`ErrNotFound` (the registry has no such package or version, HTTP 404/410).

## Constructor and options

`New(opts ...Option)` builds a `Registry` with a pooled default HTTP client.

| Option | Purpose |
| --- | --- |
| `WithHTTPClient` | Override the default HTTP client, for example to share the host service's client or inject a test transport. |
| `WithPersister` | Enable best-effort persistence of fetched data through a `Persister`. |
| `WithPersistErrorHandler` | Receive persistence errors, which never fail a fetch. The default ignores them. |
| `WithBaseURL` | Override the registry base URL for one ecosystem, for example an org mirror or an `httptest` server in tests. |
| `WithUserAgent` | Set the `User-Agent` sent on registry requests. |

The default user agent is `"vulnesis-pkgregistry"`.

## HTTP behaviour

The default HTTP client uses a 15 second timeout and pooled transports. Each
request is retried up to three attempts, retrying transport errors and 5xx
responses with a short linear backoff (200 ms per prior attempt). A single
response is capped at 64 MiB to guard against pathological or hostile payloads.

## Satisfaction and parent-upgrade resolution

`Satisfies(ecosystem, version, constraint)` reports whether a version satisfies a
declared dependency constraint. It is a pragmatic, best-effort comparator: npm /
cargo SemVer (`^`, `~`, comparators, x-ranges, `||`) and PyPI PEP 440 (`>=`,
`<=`, `==`, `!=`, `~=`, comma-AND). An unparseable constraint yields `false`, so
callers fall back to a deterministic pin rather than a wrong upgrade.

`Registry.ParentUpgradeTarget` returns the recommended parent version to upgrade
to so a transitive child resolves to a safe version. It returns `""` with no
error when no parent version admits the safe child.

## Live tests

Tests run hermetically against `httptest` fixtures by default. Optional
integration tests against the real public registries are opt-in via the
environment variable:

```
VULNESIS_LIVE_REGISTRY=1 go test ./...
```

## Development

```sh
go test ./...
go vet ./...
```
### Mutation testing

[gremlins](https://github.com/go-gremlins/gremlins) mutates the package and
reports which mutants the tests fail to catch. The settings live in
`.gremlins.yaml` (timeout coefficient 10, efficacy threshold 70%), so a plain run
from the repository root uses them:

```sh
gremlins unleash
```

It exits with code 10 when test efficacy falls below the threshold. CI runs the
same command in `.github/workflows/mutation.yml`. Do not run it with the default
timeout coefficient: most mutants then time out and the report is misleading.
