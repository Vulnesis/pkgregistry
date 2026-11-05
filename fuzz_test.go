package pkgregistry

import (
	"strings"
	"testing"
)

// FuzzCompareVersions checks the comparator's antisymmetry: comparing (a, b)
// must always be the negation of comparing (b, a).
func FuzzCompareVersions(f *testing.F) {
	for _, seed := range [][2]string{
		{"1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0-alpha"},
		{"1.0.0-alpha", "1.0.0"},
		{"v1.2.3", "1.2.3"},
		{"1.0+build", "1.0.0"},
		{"1.0.0", "2.0.0"},
		{"1.0.0-rc.1", "1.0.0-rc.2"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		ab := compareVersions(a, b)
		ba := compareVersions(b, a)
		if ab != -ba {
			t.Fatalf("compareVersions not antisymmetric: compareVersions(%q, %q) = %d, compareVersions(%q, %q) = %d", a, b, ab, b, a, ba)
		}
	})
}

// FuzzCompareVersionsTransitive checks the comparator orders versions
// transitively: a <= b and b <= c implies a <= c.
func FuzzCompareVersionsTransitive(f *testing.F) {
	for _, seed := range [][3]string{
		{"1.0.0", "1.0.0-alpha", "2.0.0"},
		{"v1.2.3", "1.2.3", "1.2.4"},
		{"1.0+build", "1.0.0", "1.0.0-alpha"},
		{"1.0.0-rc.1", "1.0.0-rc.2", "1.0.0"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, a, b, c string) {
		ab := compareVersions(a, b)
		bc := compareVersions(b, c)
		ac := compareVersions(a, c)
		if ab <= 0 && bc <= 0 && ac > 0 {
			t.Fatalf("compareVersions not transitive: %q <= %q and %q <= %q but %q > %q", a, b, b, c, a, c)
		}
	})
}

// FuzzIsPrerelease exercises the prerelease helpers on arbitrary input: none may
// panic, and stripBuildMetadata must remove the build suffix.
func FuzzIsPrerelease(f *testing.F) {
	for _, seed := range []string{
		"",
		"1.0.0",
		"1.0.0-alpha",
		"v1.2.3",
		"1.0+build",
		"1.0.0-rc.1+build.5",
		"1.0.0+",
		"1.0.0-",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, v string) {
		_ = isPrerelease(v)
		_ = isPyPIPrerelease(v)
		if got := stripBuildMetadata(v); strings.Contains(got, "+") {
			t.Fatalf("stripBuildMetadata(%q) = %q still carries build metadata", v, got)
		}
	})
}

// FuzzNormalizePyPIName checks that package-name normalization is idempotent:
// normalizing an already-normalized name leaves it unchanged.
func FuzzNormalizePyPIName(f *testing.F) {
	for _, seed := range []string{
		"",
		"requests",
		"Requests",
		"requests-html",
		"requests_html",
		"zope.interface",
		"  requests  ",
		"REST._framework",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		once := normalizePyPIName(name)
		if twice := normalizePyPIName(once); once != twice {
			t.Fatalf("normalizePyPIName not idempotent: %q -> %q -> %q", name, once, twice)
		}
	})
}
