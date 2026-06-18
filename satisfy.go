package pkgregistry

import (
	"strconv"
	"strings"
)

// Satisfies reports whether version satisfies a declared dependency constraint
// for the ecosystem. It is a pragmatic, best-effort comparator covering the
// common forms used in registry metadata — npm/cargo SemVer (^, ~, comparators,
// x-ranges, OR with "||") and PyPI PEP 440 (>=, <=, ==, !=, ~=, comma-AND). It is
// not a full constraint solver; an unparseable constraint yields false so callers
// fall back to a deterministic pin rather than a wrong upgrade.
func Satisfies(ecosystem, version, constraint string) bool {
	c := strings.TrimSpace(constraint)
	if c == "" || c == "*" || c == "latest" {
		return true
	}
	switch strings.ToLower(ecosystem) {
	case EcosystemPyPI:
		return satisfiesPEP440(version, c)
	default: // npm, cargo, and other SemVer-style ecosystems
		return satisfiesSemver(version, c)
	}
}

// satisfiesSemver handles "||"-separated alternatives; each alternative is a set
// of space- or comma-separated comparators that must all hold.
func satisfiesSemver(version, constraint string) bool {
	for _, alt := range strings.Split(constraint, "||") {
		if satisfiesSemverClause(version, alt) {
			return true
		}
	}
	return false
}

func satisfiesSemverClause(version, clause string) bool {
	fields := strings.FieldsFunc(clause, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		lo, hi, hasLo, hasHi := comparatorBounds(f)
		if hasLo && compareVersions(version, lo) < 0 {
			return false
		}
		if hasHi && compareVersions(version, hi) >= 0 {
			return false
		}
	}
	return true
}

// comparatorBounds turns one SemVer comparator into an inclusive lower bound and
// exclusive upper bound (either may be absent).
func comparatorBounds(cmp string) (lo, hi string, hasLo, hasHi bool) {
	cmp = strings.TrimSpace(cmp)
	switch {
	case strings.HasPrefix(cmp, "^"):
		base := cmp[1:]
		return base, caretUpper(base), true, true
	case strings.HasPrefix(cmp, "~"):
		base := strings.TrimPrefix(cmp, "~")
		base = strings.TrimPrefix(base, ">") // tolerate "~>"
		return base, tildeUpper(base), true, true
	case strings.HasPrefix(cmp, ">="):
		return strings.TrimSpace(cmp[2:]), "", true, false
	case strings.HasPrefix(cmp, "<="):
		return "", nextPatch(strings.TrimSpace(cmp[2:])), false, true
	case strings.HasPrefix(cmp, ">"):
		return nextPatch(strings.TrimSpace(cmp[1:])), "", true, false
	case strings.HasPrefix(cmp, "<"):
		return "", strings.TrimSpace(cmp[1:]), false, true
	case strings.HasPrefix(cmp, "="):
		v := strings.TrimSpace(cmp[1:])
		return v, nextPatch(v), true, true
	default:
		// Bare version or x-range. "1.2.x"/"1.*" → range; exact → [v, nextPatch).
		if isXRange(cmp) {
			lo, hi := xRangeBounds(cmp)
			return lo, hi, true, true
		}
		return cmp, nextPatch(cmp), true, true
	}
}

func caretUpper(v string) string {
	parts := numericParts(v)
	switch {
	case parts[0] > 0 || len(strings.Split(strings.TrimPrefix(v, "v"), ".")) == 1:
		return itoa(parts[0]+1) + ".0.0"
	case parts[1] > 0:
		return "0." + itoa(parts[1]+1) + ".0"
	default:
		return "0.0." + itoa(parts[2]+1)
	}
}

func tildeUpper(v string) string {
	segs := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	p := numericParts(v)
	if len(segs) >= 2 {
		return itoa(p[0]) + "." + itoa(p[1]+1) + ".0"
	}
	return itoa(p[0]+1) + ".0.0"
}

func isXRange(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	return strings.Contains(v, "x") || strings.Contains(v, "X") || strings.HasSuffix(v, ".*") || v == "*"
}

func xRangeBounds(v string) (lo, hi string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	segs := strings.Split(v, ".")
	major, _ := strconv.Atoi(seg(segs, 0))
	if len(segs) < 2 || isWildcard(seg(segs, 1)) {
		return itoa(major) + ".0.0", itoa(major+1) + ".0.0"
	}
	minor, _ := strconv.Atoi(seg(segs, 1))
	return itoa(major) + "." + itoa(minor) + ".0", itoa(major) + "." + itoa(minor+1) + ".0"
}

func isWildcard(s string) bool { return s == "" || s == "x" || s == "X" || s == "*" }

// nextPatch returns v with its last numeric segment incremented, used to make an
// inclusive bound exclusive (e.g. "<=1.2.3" → "<1.2.4").
func nextPatch(v string) string {
	p := numericParts(v)
	return itoa(p[0]) + "." + itoa(p[1]) + "." + itoa(p[2]+1)
}

func numericParts(v string) [3]int {
	v = stripBuildMetadata(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, s := range strings.Split(v, ".") {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(s)
	}
	return out
}

func seg(segs []string, i int) string {
	if i < len(segs) {
		return segs[i]
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }

// satisfiesPEP440 handles comma-separated PEP 440 comparators (all must hold).
func satisfiesPEP440(version, constraint string) bool {
	for _, part := range strings.Split(constraint, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !pep440Clause(version, part) {
			return false
		}
	}
	return true
}

func pep440Clause(version, clause string) bool {
	op, want := splitPEP440(clause)
	cmp := compareVersions(version, want)
	switch op {
	case "==", "===":
		return cmp == 0 || strings.HasPrefix(strings.TrimPrefix(version, "v"), strings.TrimSuffix(want, ".*"))
	case "!=":
		return cmp != 0
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	case "~=":
		// Compatible release: >= want AND < next-minor of want.
		return cmp >= 0 && compareVersions(version, tildeUpper(want)) < 0
	default:
		return cmp == 0
	}
}

func splitPEP440(clause string) (op, version string) {
	for _, o := range []string{"===", "==", "~=", "!=", ">=", "<=", ">", "<"} {
		if strings.HasPrefix(clause, o) {
			return o, strings.TrimSpace(strings.TrimPrefix(clause, o))
		}
	}
	return "==", strings.TrimSpace(clause)
}
