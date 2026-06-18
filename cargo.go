package pkgregistry

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

const cargoDefaultBaseURL = "https://index.crates.io"

type cargoClient struct{ reg *Registry }

func (c *cargoClient) Ecosystem() string { return EcosystemCargo }

// cargoIndexLine is one newline-delimited JSON record in a crate's sparse index.
type cargoIndexLine struct {
	Name   string          `json:"name"`
	Vers   string          `json:"vers"`
	Deps   []cargoIndexDep `json:"deps"`
	Yanked bool            `json:"yanked"`
}

type cargoIndexDep struct {
	Name     string `json:"name"`
	Req      string `json:"req"`
	Kind     string `json:"kind"` // "normal" | "dev" | "build"
	Optional bool   `json:"optional"`
}

// cargoIndexPath maps a crate name to its sparse-index path, per the cargo
// registry layout (1/2/3/<a>/ and <ab>/<cd>/ prefixes), lowercased.
func cargoIndexPath(name string) string {
	n := strings.ToLower(name)
	switch len(n) {
	case 0:
		return ""
	case 1:
		return "/1/" + n
	case 2:
		return "/2/" + n
	case 3:
		return "/3/" + n[:1] + "/" + n
	default:
		return "/" + n[:2] + "/" + n[2:4] + "/" + n
	}
}

func (c *cargoClient) fetchLines(ctx context.Context, pkg string) ([]cargoIndexLine, error) {
	p := cargoIndexPath(pkg)
	if p == "" {
		return nil, ErrNotFound
	}
	body, err := c.reg.get(ctx, c.reg.baseURL(EcosystemCargo, cargoDefaultBaseURL)+p, "")
	if err != nil {
		return nil, err
	}
	var lines []cargoIndexLine
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var line cargoIndexLine
		if err := json.Unmarshal(raw, &line); err != nil || line.Vers == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func (c *cargoClient) Versions(ctx context.Context, pkg string) ([]Version, error) {
	lines, err := c.fetchLines(ctx, pkg)
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(lines))
	for _, l := range lines {
		versions = append(versions, Version{
			Version:    l.Vers,
			Yanked:     l.Yanked,
			Prerelease: isPrerelease(l.Vers),
		})
	}
	sortVersionsNewestFirst(versions)
	c.reg.persistVersions(ctx, EcosystemCargo, pkg, versions)
	return versions, nil
}

func (c *cargoClient) Latest(ctx context.Context, pkg string) (string, error) {
	versions, err := c.Versions(ctx, pkg)
	if err != nil {
		return "", err
	}
	if v := maxStableVersion(versions); v != "" {
		return v, nil
	}
	return "", ErrNotFound
}

func (c *cargoClient) VersionMetadata(ctx context.Context, pkg, version string) (*VersionMeta, error) {
	lines, err := c.fetchLines(ctx, pkg)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		if l.Vers != version {
			continue
		}
		meta := &VersionMeta{Version: version}
		for _, d := range l.Deps {
			meta.Dependencies = append(meta.Dependencies, DeclaredDep{
				Name:      d.Name,
				Range:     d.Req,
				Ecosystem: EcosystemCargo,
				Scope:     cargoScope(d),
			})
		}
		c.reg.persistDeps(ctx, EcosystemCargo, pkg, version, meta.Dependencies)
		return meta, nil
	}
	return nil, ErrNotFound
}

func cargoScope(d cargoIndexDep) string {
	if d.Optional {
		return "optional"
	}
	if strings.EqualFold(d.Kind, "dev") {
		return "dev"
	}
	return "runtime"
}
