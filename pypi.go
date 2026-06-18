package pkgregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const pypiDefaultBaseURL = "https://pypi.org"

type pypiClient struct{ reg *Registry }

func (c *pypiClient) Ecosystem() string { return EcosystemPyPI }

type pypiJSON struct {
	Info     pypiInfo                 `json:"info"`
	Releases map[string][]pypiRelease `json:"releases"`
}

type pypiInfo struct {
	Version      string   `json:"version"`
	RequiresDist []string `json:"requires_dist"`
	Yanked       bool     `json:"yanked"`
}

type pypiRelease struct {
	UploadTimeISO string `json:"upload_time_iso_8601"`
	Yanked        bool   `json:"yanked"`
}

func (c *pypiClient) projectURL(pkg string) string {
	return c.reg.baseURL(EcosystemPyPI, pypiDefaultBaseURL) + "/pypi/" + url.PathEscape(pkg) + "/json"
}

func (c *pypiClient) versionURL(pkg, version string) string {
	return c.reg.baseURL(EcosystemPyPI, pypiDefaultBaseURL) + "/pypi/" + url.PathEscape(pkg) + "/" + url.PathEscape(version) + "/json"
}

func (c *pypiClient) fetch(ctx context.Context, u, pkg string) (*pypiJSON, error) {
	body, err := c.reg.get(ctx, u, "application/json")
	if err != nil {
		return nil, err
	}
	var doc pypiJSON
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("pkgregistry: decode pypi json for %q: %w", pkg, err)
	}
	return &doc, nil
}

func (c *pypiClient) Versions(ctx context.Context, pkg string) ([]Version, error) {
	doc, err := c.fetch(ctx, c.projectURL(pkg), pkg)
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(doc.Releases))
	for ver, files := range doc.Releases {
		v := Version{Version: ver, Prerelease: isPyPIPrerelease(ver), Yanked: true}
		// A release is yanked only if every file is yanked; its publish time is the
		// earliest file upload.
		anyFile := false
		for _, f := range files {
			anyFile = true
			if !f.Yanked {
				v.Yanked = false
			}
			if t := parseTime(f.UploadTimeISO); t != nil {
				if v.PublishedAt == nil || t.Before(*v.PublishedAt) {
					v.PublishedAt = t
				}
			}
		}
		if !anyFile {
			v.Yanked = false
		}
		versions = append(versions, v)
	}
	sortVersionsNewestFirst(versions)
	c.reg.persistVersions(ctx, EcosystemPyPI, pkg, versions)
	return versions, nil
}

func (c *pypiClient) Latest(ctx context.Context, pkg string) (string, error) {
	doc, err := c.fetch(ctx, c.projectURL(pkg), pkg)
	if err != nil {
		return "", err
	}
	if doc.Info.Version != "" {
		return doc.Info.Version, nil
	}
	return "", ErrNotFound
}

func (c *pypiClient) VersionMetadata(ctx context.Context, pkg, version string) (*VersionMeta, error) {
	doc, err := c.fetch(ctx, c.versionURL(pkg, version), pkg)
	if err != nil {
		return nil, err
	}
	meta := &VersionMeta{Version: version, Dependencies: parseRequiresDist(doc.Info.RequiresDist)}
	if files := doc.Releases[version]; len(files) > 0 {
		if t := parseTime(files[0].UploadTimeISO); t != nil {
			meta.PublishedAt = t
		}
	}
	c.reg.persistDeps(ctx, EcosystemPyPI, pkg, version, meta.Dependencies)
	return meta, nil
}

// requiresDistName captures the project name (and optional extras) at the start
// of a PEP 508 requirement string.
var requiresDistName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)\s*(?:\[[^\]]*\])?`)

// parseRequiresDist turns PEP 508 requirement strings into DeclaredDep. Examples:
//
//	"urllib3 (>=1.21.1,<1.27)"                         → runtime, range ">=1.21.1,<1.27"
//	"chardet<5,>=3.0.2 ; extra == 'use_chardet_on_py3'" → optional, range "<5,>=3.0.2"
//	"requests"                                          → runtime, range ""
func parseRequiresDist(reqs []string) []DeclaredDep {
	var deps []DeclaredDep
	for _, raw := range reqs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		marker := ""
		if i := strings.Index(spec, ";"); i >= 0 {
			marker = spec[i+1:]
			spec = strings.TrimSpace(spec[:i])
		}
		m := requiresDistName.FindStringSubmatch(spec)
		if m == nil {
			continue
		}
		name := m[1]
		rng := strings.TrimSpace(spec[len(m[0]):])
		rng = strings.TrimSpace(strings.Trim(rng, "()"))
		scope := "runtime"
		if strings.Contains(marker, "extra ==") || strings.Contains(marker, "extra==") {
			scope = "optional"
		}
		deps = append(deps, DeclaredDep{Name: name, Range: rng, Ecosystem: EcosystemPyPI, Scope: scope})
	}
	return deps
}
