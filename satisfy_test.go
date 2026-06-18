package pkgregistry

import (
	"context"
	"testing"
)

func TestSatisfiesSemver(t *testing.T) {
	cases := []struct {
		ver, constraint string
		want            bool
	}{
		{"1.20.1", "^1.19.0", true},
		{"2.0.0", "^1.19.0", false},
		{"1.19.0", "^1.19.0", true},
		{"1.2.5", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"1.26.18", ">=1.21.1 <2.0.0", true},
		{"2.0.0", ">=1.21.1 <2.0.0", false},
		{"1.2.3", "1.2.3", true},
		{"1.2.4", "1.2.3", false},
		{"3.1.0", "2.x || 3.x", true},
		{"1.5.0", "2.x || 3.x", false},
		{"1.4.9", "1.4.x", true},
		{"1.5.0", "1.4.x", false},
		{"9.9.9", "*", true},
	}
	for _, tc := range cases {
		if got := Satisfies(EcosystemNPM, tc.ver, tc.constraint); got != tc.want {
			t.Errorf("Satisfies(npm, %q, %q) = %v, want %v", tc.ver, tc.constraint, got, tc.want)
		}
	}
}

func TestSatisfiesPEP440(t *testing.T) {
	cases := []struct {
		ver, constraint string
		want            bool
	}{
		{"1.26.18", ">=1.21.1,<3", true},
		{"3.0.0", ">=1.21.1,<3", false},
		{"2.31.0", ">=2.0", true},
		{"1.4.5", "~=1.4", true},
		{"2.0.0", "~=1.4", false},
		{"2.0.0", "!=2.0.0", false},
		{"2.0.1", "!=2.0.0", true},
	}
	for _, tc := range cases {
		if got := Satisfies(EcosystemPyPI, tc.ver, tc.constraint); got != tc.want {
			t.Errorf("Satisfies(pypi, %q, %q) = %v, want %v", tc.ver, tc.constraint, got, tc.want)
		}
	}
}

func TestNpmParentUpgradeTarget(t *testing.T) {
	srv := npmTestServer(t)
	defer srv.Close()
	r := New(WithBaseURL(EcosystemNPM, srv.URL))

	// fixture: express 4.17.1 needs body-parser 1.19.0; 4.18.2 needs 1.20.1.
	// Safe child 1.20.1 → smallest parent >= 4.17.1 admitting it is 4.18.2.
	got, err := r.ParentUpgradeTarget(context.Background(), EcosystemNPM, "express", "4.17.1", "body-parser", "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "4.18.2" {
		t.Fatalf("ParentUpgradeTarget = %q, want 4.18.2", got)
	}
}

func TestCargoParentUpgradeTarget(t *testing.T) {
	srv := cargoTestServer(t)
	defer srv.Close()
	r := New(WithBaseURL(EcosystemCargo, srv.URL))

	// fixture: time 0.3.30 requires libc ^0.2; 0.3.31 requires libc ^0.2.5.
	// Safe child 0.2.5 → 0.3.30 (^0.2 admits 0.2.5) is the smallest admitting parent.
	got, err := r.ParentUpgradeTarget(context.Background(), EcosystemCargo, "time", "0.3.30", "libc", "0.2.5")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.3.30" {
		t.Fatalf("ParentUpgradeTarget = %q, want 0.3.30", got)
	}
}
