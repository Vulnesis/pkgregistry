package pkgregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// pypiParentServer serves a PyPI project whose releases declare different
// requires_dist ranges on "child", so the parent-upgrade path can exercise both
// admitting and non-admitting candidates.
func pypiParentServer(t *testing.T) *httptest.Server {
	t.Helper()
	const project = `{
	  "info": {"version": "1.2.0"},
	  "releases": {
	    "1.0.0": [{"upload_time_iso_8601": "2023-01-01T00:00:00Z", "yanked": false}],
	    "1.1.0": [{"upload_time_iso_8601": "2023-02-01T00:00:00Z", "yanked": false}],
	    "1.2.0": [{"upload_time_iso_8601": "2023-03-01T00:00:00Z", "yanked": false}]
	  }
	}`
	versionBodies := map[string]string{
		"1.0.0": `{"info": {"version": "1.0.0", "requires_dist": ["child (<2.0)"]}, "releases": {"1.0.0": [{"upload_time_iso_8601": "2023-01-01T00:00:00Z"}]}}`,
		"1.1.0": `{"info": {"version": "1.1.0", "requires_dist": ["child (>=2.0,<3)"]}, "releases": {"1.1.0": [{"upload_time_iso_8601": "2023-02-01T00:00:00Z"}]}}`,
		"1.2.0": `{"info": {"version": "1.2.0", "requires_dist": ["child (>=2.0,<3)"]}, "releases": {"1.2.0": [{"upload_time_iso_8601": "2023-03-01T00:00:00Z"}]}}`,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// /pypi/<pkg>/json vs /pypi/<pkg>/<version>/json
		parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
		if len(parts) == 3 {
			_, _ = w.Write([]byte(project))
			return
		}
		if body := versionBodies[parts[2]]; body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(`{"info": {}, "releases": {}}`))
	}))
}

func TestPypiParentUpgradeTarget(t *testing.T) {
	srv := pypiParentServer(t)
	defer srv.Close()
	r := New(WithBaseURL(EcosystemPyPI, srv.URL))

	// 1.0.0 requires child <2.0 (does not admit 2.1.0); 1.1.0 and 1.2.0 require
	// child >=2.0,<3. The smallest parent >= 1.0.0 admitting 2.1.0 is 1.1.0.
	got, err := r.ParentUpgradeTarget(context.Background(), EcosystemPyPI, "parent", "1.0.0", "child", "2.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.1.0" {
		t.Fatalf("ParentUpgradeTarget = %q, want 1.1.0", got)
	}
}

func TestParentUpgradeTargetNoAdmittingParent(t *testing.T) {
	srv := pypiParentServer(t)
	defer srv.Close()
	r := New(WithBaseURL(EcosystemPyPI, srv.URL))

	// No release admits child 3.0.0: 1.0.0 requires <2.0, 1.1.0/1.2.0 require <3.
	got, err := r.ParentUpgradeTarget(context.Background(), EcosystemPyPI, "parent", "1.0.0", "child", "3.0.0")
	if err != nil {
		t.Fatalf("ParentUpgradeTarget returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("ParentUpgradeTarget = %q, want empty", got)
	}
}
