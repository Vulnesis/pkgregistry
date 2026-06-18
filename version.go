package pkgregistry

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// parseTime parses an RFC3339 timestamp, returning nil on empty/invalid input.
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}

// isPrerelease reports whether a version string carries a SemVer prerelease tag.
func isPrerelease(v string) bool {
	return strings.ContainsAny(stripBuildMetadata(v), "-")
}

var pep440PostSegment = regexp.MustCompile(`\.?post\d*`)

// isPyPIPrerelease reports whether a PEP 440 version is a pre-release or dev
// release (e.g. 2.32.0rc1, 1.0a1, 1.0.dev3). A final release is digits-and-dots
// only once the epoch, local, and post segments are stripped; any remaining
// letter marks a pre/dev release. Plain post-releases (1.0.post1) are not
// pre-releases.
func isPyPIPrerelease(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	if i := strings.IndexByte(s, '+'); i >= 0 { // drop local version
		s = s[:i]
	}
	if i := strings.IndexByte(s, '!'); i >= 0 { // drop epoch
		s = s[i+1:]
	}
	s = pep440PostSegment.ReplaceAllString(s, "")
	return strings.ContainsFunc(s, func(r rune) bool { return r >= 'a' && r <= 'z' })
}

func stripBuildMetadata(v string) string {
	if i := strings.IndexByte(v, '+'); i >= 0 {
		return v[:i]
	}
	return v
}

// compareVersions does a best-effort SemVer-ish comparison: numeric release
// segments compared numerically, a prerelease ordered below its release. It is
// used only to pick a fallback "latest" when the registry exposes no dist-tag;
// it is not a full SemVer implementation.
func compareVersions(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")

	aCore, aPre := splitPrerelease(a)
	bCore, bPre := splitPrerelease(b)

	if c := compareNumericDotted(aCore, bCore); c != 0 {
		return c
	}
	// Equal cores: a release outranks a prerelease; otherwise lexical on the tag.
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	case aPre < bPre:
		return -1
	case aPre > bPre:
		return 1
	default:
		return 0
	}
}

func splitPrerelease(v string) (core, pre string) {
	v = stripBuildMetadata(v)
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func compareNumericDotted(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := max(len(as), len(bs))
	for i := range n {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// maxStableVersion returns the highest non-prerelease, non-yanked version, or ""
// when none qualifies.
func maxStableVersion(versions []Version) string {
	best := ""
	for _, v := range versions {
		if v.Yanked || v.Prerelease {
			continue
		}
		if best == "" || compareVersions(v.Version, best) > 0 {
			best = v.Version
		}
	}
	return best
}
