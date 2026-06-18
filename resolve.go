package pkgregistry

import (
	"context"
	"sort"
	"strings"
)

// maxParentProbe bounds how many candidate parent versions are examined when
// resolving a transitive parent-upgrade, keeping per-version-fetch ecosystems
// (pypi) from issuing an unbounded number of requests.
const maxParentProbe = 32

// ParentResolver is implemented by clients that can resolve a transitive
// parent-upgrade: the smallest parent version >= the current one whose declared
// range for child admits safeChild. Registry.ParentUpgradeTarget uses it.
type ParentResolver interface {
	BestParentVersion(ctx context.Context, parent, parentCurrent, child, safeChild string) (string, error)
}

// ParentUpgradeTarget returns the recommended parent version to upgrade to so the
// transitive child resolves to safeChild — the cross-ecosystem replacement for
// the CLI's old direct npm-registry probe. Returns "" (no error) when no parent
// version admits the safe child, so the caller falls back to a deterministic pin.
func (r *Registry) ParentUpgradeTarget(ctx context.Context, ecosystem, parent, parentCurrent, child, safeChild string) (string, error) {
	c, err := r.Client(ecosystem)
	if err != nil {
		return "", err
	}
	pr, ok := c.(ParentResolver)
	if !ok {
		return "", ErrUnsupportedEcosystem
	}
	return pr.BestParentVersion(ctx, parent, parentCurrent, child, safeChild)
}

// candidateParents returns stable, non-yanked versions >= current, ascending
// (smallest admitting parent is least disruptive), capped at maxParentProbe.
func candidateParents(versions []Version, current string) []string {
	out := make([]string, 0, len(versions))
	for _, v := range versions {
		if v.Yanked || v.Prerelease {
			continue
		}
		if current != "" && compareVersions(v.Version, current) < 0 {
			continue
		}
		out = append(out, v.Version)
	}
	sort.Slice(out, func(i, j int) bool { return compareVersions(out[i], out[j]) < 0 })
	if len(out) > maxParentProbe {
		out = out[:maxParentProbe]
	}
	return out
}

// depRange returns the declared range a dependency set requires for child,
// preferring runtime/peer scopes over dev/optional.
func depRange(deps []DeclaredDep, child string) string {
	fallback := ""
	for _, d := range deps {
		if !nameMatch(d.Ecosystem, d.Name, child) {
			continue
		}
		if d.Scope == "runtime" || d.Scope == "peer" {
			return d.Range
		}
		if fallback == "" {
			fallback = d.Range
		}
	}
	return fallback
}

func nameMatch(ecosystem, a, b string) bool {
	if strings.EqualFold(ecosystem, EcosystemPyPI) {
		return normalizePyPIName(a) == normalizePyPIName(b)
	}
	return strings.EqualFold(a, b)
}

func normalizePyPIName(n string) string {
	return strings.ToLower(strings.NewReplacer("_", "-", ".", "-").Replace(strings.TrimSpace(n)))
}

func (c *npmClient) BestParentVersion(ctx context.Context, parent, parentCurrent, child, safeChild string) (string, error) {
	pm, err := c.fetch(ctx, parent)
	if err != nil {
		return "", err
	}
	versions := make([]Version, 0, len(pm.Versions))
	for ver := range pm.Versions {
		versions = append(versions, Version{Version: ver, Prerelease: isPrerelease(ver)})
	}
	for _, ver := range candidateParents(versions, parentCurrent) {
		rng := depRange(npmDeclaredDeps(pm.Versions[ver]), child)
		if rng != "" && Satisfies(EcosystemNPM, safeChild, rng) {
			return ver, nil
		}
	}
	return "", nil
}

func (c *cargoClient) BestParentVersion(ctx context.Context, parent, parentCurrent, child, safeChild string) (string, error) {
	lines, err := c.fetchLines(ctx, parent)
	if err != nil {
		return "", err
	}
	byVer := make(map[string]cargoIndexLine, len(lines))
	versions := make([]Version, 0, len(lines))
	for _, l := range lines {
		byVer[l.Vers] = l
		versions = append(versions, Version{Version: l.Vers, Yanked: l.Yanked, Prerelease: isPrerelease(l.Vers)})
	}
	for _, ver := range candidateParents(versions, parentCurrent) {
		for _, d := range byVer[ver].Deps {
			if nameMatch(EcosystemCargo, d.Name, child) && Satisfies(EcosystemCargo, safeChild, d.Req) {
				return ver, nil
			}
		}
	}
	return "", nil
}

func (c *pypiClient) BestParentVersion(ctx context.Context, parent, parentCurrent, child, safeChild string) (string, error) {
	versions, err := c.Versions(ctx, parent)
	if err != nil {
		return "", err
	}
	for _, ver := range candidateParents(versions, parentCurrent) {
		meta, err := c.VersionMetadata(ctx, parent, ver)
		if err != nil {
			continue
		}
		rng := depRange(meta.Dependencies, child)
		if rng != "" && Satisfies(EcosystemPyPI, safeChild, rng) {
			return ver, nil
		}
	}
	return "", nil
}
