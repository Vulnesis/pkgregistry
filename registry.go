// Package pkgregistry provides read-only, per-ecosystem clients for public
// package registries. Each client can list a package's versions, resolve the
// latest version, and read a specific version's declared dependency ranges.
//
// It is shared by vdb-api (live SCA enrichment + cross-ecosystem transitive
// parent-upgrade resolution) and package-firewall (metadata filtering), so the
// fetch/parse logic for each registry lives in exactly one place.
//
// A caller may inject a Persister to record fetched data (for example, vdb-api
// upserts version lists and declared ranges into the VDB Dependency tables so the
// GUI reflects the same data the CLI scan used). The library itself imports no
// database driver and knows no schema — persistence is entirely the caller's.
package pkgregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ecosystem identifiers, lowercase, matching the purl / VDB conventions.
const (
	EcosystemNPM   = "npm"
	EcosystemPyPI  = "pypi"
	EcosystemCargo = "cargo"
)

// ErrUnsupportedEcosystem is returned by Registry.Client for an ecosystem that
// has no client implementation yet.
var ErrUnsupportedEcosystem = errors.New("pkgregistry: unsupported ecosystem")

// ErrNotFound indicates the registry has no such package or version (HTTP 404/410).
var ErrNotFound = errors.New("pkgregistry: not found")

// Version is one published release of a package.
type Version struct {
	Version     string
	PublishedAt *time.Time // nil when the registry does not expose a publish time
	Yanked      bool
	Prerelease  bool
}

// DeclaredDep is a dependency a package version declares, with the version range
// (constraint) it requires — the input needed to decide which parent version
// admits a given safe child version.
type DeclaredDep struct {
	Name      string
	Range     string
	Ecosystem string
	Scope     string // runtime | dev | peer | optional
}

// VersionMeta is the metadata for a single package version.
type VersionMeta struct {
	Version      string
	PublishedAt  *time.Time
	Deprecated   bool
	Dependencies []DeclaredDep
}

// Client reads one ecosystem's registry. Implementations are safe for concurrent
// use by multiple goroutines (they hold no per-call mutable state).
type Client interface {
	Ecosystem() string
	// Versions lists every published version of pkg.
	Versions(ctx context.Context, pkg string) ([]Version, error)
	// Latest returns the registry's canonical latest (stable) version of pkg.
	Latest(ctx context.Context, pkg string) (string, error)
	// VersionMetadata returns metadata for a specific version, including declared
	// dependency ranges where the registry exposes them.
	VersionMetadata(ctx context.Context, pkg, version string) (*VersionMeta, error)
}

// Persister optionally records fetched registry data. Methods must tolerate empty
// slices and should be idempotent (callers may re-fetch the same package).
type Persister interface {
	PersistVersions(ctx context.Context, ecosystem, pkg string, versions []Version) error
	PersistDeclaredDeps(ctx context.Context, ecosystem, pkg, version string, deps []DeclaredDep) error
}

// Registry holds shared configuration (HTTP client, optional persister, mirror
// overrides) and hands out per-ecosystem Clients.
type Registry struct {
	httpClient   httpDoer
	persister    Persister
	onPersistErr func(error)
	baseURLs     map[string]string
	userAgent    string
}

// Option configures a Registry.
type Option func(*Registry)

// WithHTTPClient overrides the pooled default HTTP client (e.g. to share one with
// the host service or to inject a test transport).
func WithHTTPClient(c httpDoer) Option {
	return func(r *Registry) {
		if c != nil {
			r.httpClient = c
		}
	}
}

// WithPersister enables best-effort persistence of fetched data. When set, the
// clients write version lists and declared ranges through it after each fetch.
func WithPersister(p Persister) Option { return func(r *Registry) { r.persister = p } }

// WithPersistErrorHandler receives persistence errors (which never fail a fetch).
// vdb-api uses this to log; the default ignores them.
func WithPersistErrorHandler(fn func(error)) Option {
	return func(r *Registry) { r.onPersistErr = fn }
}

// WithBaseURL overrides the registry base URL for an ecosystem (e.g. an org
// mirror, or an httptest server in tests).
func WithBaseURL(ecosystem, baseURL string) Option {
	return func(r *Registry) { r.baseURLs[strings.ToLower(ecosystem)] = baseURL }
}

// WithUserAgent sets the User-Agent sent on registry requests.
func WithUserAgent(ua string) Option {
	return func(r *Registry) {
		if ua != "" {
			r.userAgent = ua
		}
	}
}

// New builds a Registry with a pooled default HTTP client.
func New(opts ...Option) *Registry {
	r := &Registry{
		httpClient: defaultHTTPClient(),
		baseURLs:   map[string]string{},
		userAgent:  "vulnesis-pkgregistry",
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Client returns the reader for an ecosystem, or ErrUnsupportedEcosystem.
func (r *Registry) Client(ecosystem string) (Client, error) {
	switch strings.ToLower(ecosystem) {
	case EcosystemNPM:
		return &npmClient{reg: r}, nil
	case EcosystemPyPI:
		return &pypiClient{reg: r}, nil
	case EcosystemCargo:
		return &cargoClient{reg: r}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedEcosystem, ecosystem)
	}
}

func (r *Registry) baseURL(ecosystem, def string) string {
	if u, ok := r.baseURLs[ecosystem]; ok && u != "" {
		return strings.TrimRight(u, "/")
	}
	return def
}

func (r *Registry) persistVersions(ctx context.Context, ecosystem, pkg string, versions []Version) {
	if r.persister == nil || len(versions) == 0 {
		return
	}
	if err := r.persister.PersistVersions(ctx, ecosystem, pkg, versions); err != nil && r.onPersistErr != nil {
		r.onPersistErr(err)
	}
}

func (r *Registry) persistDeps(ctx context.Context, ecosystem, pkg, version string, deps []DeclaredDep) {
	if r.persister == nil || len(deps) == 0 {
		return
	}
	if err := r.persister.PersistDeclaredDeps(ctx, ecosystem, pkg, version, deps); err != nil && r.onPersistErr != nil {
		r.onPersistErr(err)
	}
}
