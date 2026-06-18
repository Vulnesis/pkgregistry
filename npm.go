package pkgregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
)

const npmDefaultBaseURL = "https://registry.npmjs.org"

type npmClient struct{ reg *Registry }

func (c *npmClient) Ecosystem() string { return EcosystemNPM }

// packumentURL builds the full-metadata document URL. Scoped names (@scope/name)
// are escaped so the "/" becomes %2F as the registry expects.
func (c *npmClient) packumentURL(pkg string) string {
	return c.reg.baseURL(EcosystemNPM, npmDefaultBaseURL) + "/" + url.PathEscape(pkg)
}

type npmPackument struct {
	DistTags map[string]string     `json:"dist-tags"`
	Versions map[string]npmVersion `json:"versions"`
	Time     map[string]string     `json:"time"`
}

type npmVersion struct {
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	Deprecated           *string           `json:"deprecated"`
}

func (c *npmClient) fetch(ctx context.Context, pkg string) (*npmPackument, error) {
	body, err := c.reg.get(ctx, c.packumentURL(pkg), "application/json")
	if err != nil {
		return nil, err
	}
	var pm npmPackument
	if err := json.Unmarshal(body, &pm); err != nil {
		return nil, fmt.Errorf("pkgregistry: decode npm packument for %q: %w", pkg, err)
	}
	return &pm, nil
}

func (c *npmClient) Versions(ctx context.Context, pkg string) ([]Version, error) {
	pm, err := c.fetch(ctx, pkg)
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(pm.Versions))
	for ver := range pm.Versions {
		versions = append(versions, Version{
			Version:     ver,
			PublishedAt: parseTime(pm.Time[ver]),
			Prerelease:  isPrerelease(ver),
		})
	}
	sortVersionsNewestFirst(versions)
	c.reg.persistVersions(ctx, EcosystemNPM, pkg, versions)
	return versions, nil
}

func (c *npmClient) Latest(ctx context.Context, pkg string) (string, error) {
	pm, err := c.fetch(ctx, pkg)
	if err != nil {
		return "", err
	}
	if l := pm.DistTags["latest"]; l != "" {
		return l, nil
	}
	versions := make([]Version, 0, len(pm.Versions))
	for ver := range pm.Versions {
		versions = append(versions, Version{Version: ver, Prerelease: isPrerelease(ver)})
	}
	if v := maxStableVersion(versions); v != "" {
		return v, nil
	}
	return "", ErrNotFound
}

func (c *npmClient) VersionMetadata(ctx context.Context, pkg, version string) (*VersionMeta, error) {
	pm, err := c.fetch(ctx, pkg)
	if err != nil {
		return nil, err
	}
	vd, ok := pm.Versions[version]
	if !ok {
		return nil, ErrNotFound
	}
	meta := &VersionMeta{
		Version:     version,
		PublishedAt: parseTime(pm.Time[version]),
		Deprecated:  vd.Deprecated != nil && *vd.Deprecated != "",
	}
	meta.Dependencies = npmDeclaredDeps(vd)
	c.reg.persistDeps(ctx, EcosystemNPM, pkg, version, meta.Dependencies)
	return meta, nil
}

func npmDeclaredDeps(vd npmVersion) []DeclaredDep {
	var deps []DeclaredDep
	add := func(m map[string]string, scope string) {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			deps = append(deps, DeclaredDep{Name: n, Range: m[n], Ecosystem: EcosystemNPM, Scope: scope})
		}
	}
	add(vd.Dependencies, "runtime")
	add(vd.PeerDependencies, "peer")
	add(vd.OptionalDependencies, "optional")
	add(vd.DevDependencies, "dev")
	return deps
}

// sortVersionsNewestFirst orders by publish time when known, falling back to a
// SemVer-ish comparison, so persisted/returned lists are newest-first.
func sortVersionsNewestFirst(vs []Version) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.PublishedAt != nil && b.PublishedAt != nil && !a.PublishedAt.Equal(*b.PublishedAt) {
			return a.PublishedAt.After(*b.PublishedAt)
		}
		return compareVersions(a.Version, b.Version) > 0
	})
}
