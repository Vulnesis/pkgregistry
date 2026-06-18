package pkgregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pypiProjectFixture = `{
  "info": {"version": "2.31.0", "yanked": false},
  "releases": {
    "2.30.0": [{"upload_time_iso_8601": "2023-05-22T00:00:00Z", "yanked": false}],
    "2.31.0": [{"upload_time_iso_8601": "2023-05-22T00:00:00Z", "yanked": false}],
    "2.32.0rc1": [{"upload_time_iso_8601": "2024-01-01T00:00:00Z", "yanked": false}]
  }
}`

const pypiVersionFixture = `{
  "info": {
    "version": "2.31.0",
    "requires_dist": [
      "urllib3 (>=1.21.1,<3)",
      "certifi>=2017.4.17",
      "chardet<5,>=3.0.2 ; extra == 'use_chardet_on_py3'"
    ]
  },
  "releases": {"2.31.0": [{"upload_time_iso_8601": "2023-05-22T00:00:00Z"}]}
}`

func pypiTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// /pypi/<pkg>/json vs /pypi/<pkg>/<version>/json
		parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
		if len(parts) == 3 {
			_, _ = w.Write([]byte(pypiProjectFixture))
			return
		}
		_, _ = w.Write([]byte(pypiVersionFixture))
	}))
}

func TestPypiVersions(t *testing.T) {
	srv := pypiTestServer(t)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemPyPI, srv.URL)).Client(EcosystemPyPI)
	versions, err := c.Versions(context.Background(), "requests")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	var rc Version
	for _, v := range versions {
		if v.Version == "2.32.0rc1" {
			rc = v
		}
	}
	if !rc.Prerelease {
		t.Fatalf("2.32.0rc1 should be prerelease: %+v", rc)
	}
}

func TestPypiRequiresDistParsing(t *testing.T) {
	srv := pypiTestServer(t)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemPyPI, srv.URL)).Client(EcosystemPyPI)
	meta, err := c.VersionMetadata(context.Background(), "requests", "2.31.0")
	if err != nil {
		t.Fatal(err)
	}
	urllib3, ok := findDep(meta.Dependencies, "urllib3")
	if !ok || urllib3.Range != ">=1.21.1,<3" || urllib3.Scope != "runtime" {
		t.Fatalf("urllib3 = %+v, want range >=1.21.1,<3 runtime", urllib3)
	}
	certifi, ok := findDep(meta.Dependencies, "certifi")
	if !ok || certifi.Range != ">=2017.4.17" {
		t.Fatalf("certifi = %+v, want range >=2017.4.17", certifi)
	}
	chardet, ok := findDep(meta.Dependencies, "chardet")
	if !ok || chardet.Scope != "optional" {
		t.Fatalf("chardet = %+v, want optional scope (extra marker)", chardet)
	}
}
