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

// FuzzSatisfies probes Satisfies for the constraint-parsing invariants the
// fixed tables cannot cover: wildcard and empty constraints, operator round
// trips, and no-panic on arbitrary input. Seed with the existing happy-path
// cases plus pathological inputs.
func FuzzSatisfies(f *testing.F) {
	semverSeeds := []string{
		"^1.19.0", "~1.2.0", ">=1.21.1 <2.0.0", "1.2.3", "2.x || 3.x",
		"1.4.x", "*", "", " ", " 1.2.3 ", "=1.2.3", ">1.2.3", "<=1.2.3",
		"^0.2.5", "1.*", "1.2.*", "1.X", ">=1.0.0,<2.0.0", "~>1.2",
	}
	pep440Seeds := []string{
		">=1.21.1,<3", ">=2.0", "~=1.4", "!=2.0.0", "==2.0.0", "===2.0.0",
		"==2.0.*", "2.0.0", "*", "", " ", "<2.0", ">1.0", "<=3.0",
	}
	versions := []string{"0.0.0", "1.2.3", "2.0.0", "3.1.4", "9.9.9", "1.4.5", "2.31.0", "v1.2.3"}
	for _, v := range versions {
		for _, c := range semverSeeds {
			f.Add(EcosystemNPM, v, c)
		}
		for _, c := range pep440Seeds {
			f.Add(EcosystemPyPI, v, c)
		}
	}

	f.Fuzz(func(t *testing.T, eco, ver, constraint string) {
		if eco != EcosystemNPM && eco != EcosystemPyPI {
			t.Skip("fuzz corpus covers SemVer and PEP 440 ecosystems only")
		}
		// Invariant 5: arbitrary generated constraints must not panic.
		Satisfies(eco, ver, constraint)
		// Invariants 1 and 2: wildcard and empty constraints admit any version.
		if !Satisfies(eco, ver, "*") {
			t.Fatalf("Satisfies(%q, %q, %q) = false, want true for wildcard", eco, ver, "*")
		}
		if !Satisfies(eco, ver, "") {
			t.Fatalf("Satisfies(%q, %q, %q) = false, want true for empty", eco, ver, "")
		}
		if eco == EcosystemNPM {
			// Invariant 3: "=" acceptance implies the version compares equal to
			// itself, i.e. the round trip is consistent.
			if Satisfies(eco, ver, "="+ver) && compareVersions(ver, ver) != 0 {
				t.Fatalf("Satisfies(npm, %q, %q) = true but compareVersions(%q, %q) != 0", ver, "="+ver, ver, ver)
			}
		}
		if eco == EcosystemPyPI && isCanonicalPEP440(ver) {
			// Invariant 4: == admits and != rejects a canonical version.
			if !Satisfies(eco, ver, "=="+ver) {
				t.Fatalf("Satisfies(pypi, %q, %q) = false, want true", ver, "=="+ver)
			}
			if Satisfies(eco, ver, "!="+ver) {
				t.Fatalf("Satisfies(pypi, %q, %q) = true, want false", ver, "!="+ver)
			}
		}
	})
}

// isCanonicalPEP440 reports whether v is a plain numeric PEP 440 release with
// dots, so "=="+v and "!="+v are unambiguous round trips.
func isCanonicalPEP440(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if (v[i] < '0' || v[i] > '9') && v[i] != '.' {
			return false
		}
	}
	return true
}
