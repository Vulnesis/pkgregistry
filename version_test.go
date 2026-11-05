package pkgregistry

import (
	"reflect"
	"testing"
	"time"
)

func TestSortVersionsNewestFirstNumericOnly(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{}, []string{}},
		{[]string{"1.0.0"}, []string{"1.0.0"}},
		{[]string{"1.0.0", "2.0.0"}, []string{"2.0.0", "1.0.0"}},
		{[]string{"2.0.0", "1.0.0", "1.5.0"}, []string{"2.0.0", "1.5.0", "1.0.0"}},
		{[]string{"10.0.0", "2.0.0", "1.0.0"}, []string{"10.0.0", "2.0.0", "1.0.0"}},
		{[]string{"1.0.0", "1.0.0"}, []string{"1.0.0", "1.0.0"}},
	}
	for _, tc := range cases {
		// Build same shapes independently to keep helper code out of coverage.
		in := versionsFromStrings(tc.in)
		sortVersionsNewestFirst(in)
		if got := versionStrings(in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("sortVersionsNewestFirst(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSortVersionsNewestFirstByTime(t *testing.T) {
	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	dec := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)

	in := []Version{
		{Version: "1.0.0", PublishedAt: &jan},
		{Version: "2.0.0", PublishedAt: &dec},
		{Version: "1.5.0", PublishedAt: &jun},
	}
	sortVersionsNewestFirst(in)
	want := []string{"2.0.0", "1.5.0", "1.0.0"}
	if got := versionStrings(in); !reflect.DeepEqual(got, want) {
		t.Errorf("sortVersionsNewestFirst by time = %v, want %v", got, want)
	}
}

func TestSortVersionsNewestFirstIdempotent(t *testing.T) {
	in := versionsFromStrings([]string{"1.0.0", "3.0.0", "2.0.0"})
	sortVersionsNewestFirst(in)
	first := versionStrings(in)
	sortVersionsNewestFirst(in)
	second := versionStrings(in)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("sortVersionsNewestFirst not idempotent: first %v, second %v", first, second)
	}
}

func versionsFromStrings(ss []string) []Version {
	vs := make([]Version, len(ss))
	for i, s := range ss {
		vs[i] = Version{Version: s}
	}
	return vs
}

func versionStrings(vs []Version) []string {
	ss := make([]string, len(vs))
	for i, v := range vs {
		ss[i] = v.Version
	}
	return ss
}
