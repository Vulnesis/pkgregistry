package pkgregistry

import (
	"context"
	"errors"
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

func pypiProjectServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

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

func TestPypiLatest(t *testing.T) {
	srv := pypiProjectServer(t, pypiProjectFixture)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemPyPI, srv.URL)).Client(EcosystemPyPI)
	got, err := c.Latest(context.Background(), "requests")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2.31.0" {
		t.Fatalf("Latest = %q, want 2.31.0", got)
	}

	empty := pypiProjectServer(t, `{"info": {}, "releases": {}}`)
	defer empty.Close()
	ce, _ := New(WithBaseURL(EcosystemPyPI, empty.URL)).Client(EcosystemPyPI)
	if _, err := ce.Latest(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Latest with unset info.version = %v, want ErrNotFound", err)
	}
}

func TestPypiVersionsEmptyRelease(t *testing.T) {
	srv := pypiProjectServer(t, `{"info": {}, "releases": {"1.0.0": []}}`)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemPyPI, srv.URL)).Client(EcosystemPyPI)
	versions, err := c.Versions(context.Background(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("got %d versions, want 1", len(versions))
	}
	if versions[0].Yanked {
		t.Fatalf("release with zero files should not be yanked: %+v", versions[0])
	}
}

func TestParseRequiresDistEdges(t *testing.T) {
	tests := []struct {
		name string
		reqs []string
		want []DeclaredDep
	}{
		{name: "empty list", reqs: nil, want: nil},
		{name: "unparseable spec", reqs: []string{">=1.0"}, want: nil},
		{
			name: "extra== marker form",
			reqs: []string{"chardet<5 ; extra=='use_chardet_on_py3'"},
			want: []DeclaredDep{{Name: "chardet", Range: "<5", Ecosystem: EcosystemPyPI, Scope: "optional"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRequiresDist(tt.reqs)
			if len(got) != len(tt.want) {
				t.Fatalf("parseRequiresDist(%v) = %+v, want %+v", tt.reqs, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("parseRequiresDist(%v)[%d] = %+v, want %+v", tt.reqs, i, got[i], tt.want[i])
				}
			}
		})
	}
}
