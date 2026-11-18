package pkgregistry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const npmPackumentFixture = `{
  "dist-tags": {"latest": "4.18.2"},
  "versions": {
    "4.17.1": {"version": "4.17.1", "dependencies": {"body-parser": "1.19.0"}},
    "4.18.2": {"version": "4.18.2", "dependencies": {"body-parser": "1.20.1"}, "peerDependencies": {"node": ">=14"}},
    "5.0.0-beta.1": {"version": "5.0.0-beta.1", "dependencies": {"body-parser": "2.0.0"}, "deprecated": "use 5.0.0"}
  },
  "time": {
    "4.17.1": "2019-05-25T00:00:00Z",
    "4.18.2": "2022-10-08T00:00:00Z",
    "5.0.0-beta.1": "2024-01-01T00:00:00Z"
  }
}`

func npmTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newFixtureHandlerServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(npmPackumentFixture))
	}))
}

func TestNpmScopedPackumentURLAndHeaders(t *testing.T) {
	var gotRequestURI, gotAccept, gotUserAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI is the raw wire form; r.URL.Path already decodes %2F.
		gotRequestURI = r.RequestURI
		gotAccept = r.Header.Get("Accept")
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(npmPackumentFixture))
	}))
	defer srv.Close()

	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL)).Client(EcosystemNPM)
	if _, err := c.Versions(context.Background(), "@babel/core"); err != nil {
		t.Fatal(err)
	}

	wantRequestURI := "/@babel%2Fcore"
	if gotRequestURI != wantRequestURI {
		t.Fatalf("request URI = %q, want %q", gotRequestURI, wantRequestURI)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q, want application/json", gotAccept)
	}
	if gotUserAgent != "vulnetix-pkgregistry" {
		t.Fatalf("User-Agent = %q, want vulnetix-pkgregistry", gotUserAgent)
	}
}

func TestNpmVersionsAndPersist(t *testing.T) {
	srv := npmTestServer(t)
	defer srv.Close()
	p := newRecordingPersister()
	r := New(WithBaseURL(EcosystemNPM, srv.URL), WithPersister(p))
	c, _ := r.Client(EcosystemNPM)

	versions, err := c.Versions(context.Background(), "express")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	// Newest-first by publish time.
	if versions[0].Version != "5.0.0-beta.1" || !versions[0].Prerelease {
		t.Fatalf("first version = %+v, want 5.0.0-beta.1 prerelease", versions[0])
	}
	if versions[0].PublishedAt == nil {
		t.Fatal("expected publish time to be parsed")
	}
	if got := p.versions[EcosystemNPM+":express"]; len(got) != 3 {
		t.Fatalf("persister recorded %d versions, want 3", len(got))
	}
}

func TestNpmLatestPrefersDistTag(t *testing.T) {
	srv := npmTestServer(t)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL)).Client(EcosystemNPM)
	got, err := c.Latest(context.Background(), "express")
	if err != nil {
		t.Fatal(err)
	}
	if got != "4.18.2" {
		t.Fatalf("Latest = %q, want 4.18.2", got)
	}
}

func TestNpmVersionMetadataDeclaredRanges(t *testing.T) {
	srv := npmTestServer(t)
	defer srv.Close()
	p := newRecordingPersister()
	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL), WithPersister(p)).Client(EcosystemNPM)

	meta, err := c.VersionMetadata(context.Background(), "express", "4.18.2")
	if err != nil {
		t.Fatal(err)
	}
	bp, ok := findDep(meta.Dependencies, "body-parser")
	if !ok || bp.Range != "1.20.1" || bp.Scope != "runtime" {
		t.Fatalf("body-parser dep = %+v, want range 1.20.1 runtime", bp)
	}
	node, ok := findDep(meta.Dependencies, "node")
	if !ok || node.Scope != "peer" {
		t.Fatalf("node dep = %+v, want peer scope", node)
	}
	if meta.Deprecated {
		t.Fatal("4.18.2 should not be deprecated")
	}
	if got := p.deps[EcosystemNPM+":express@4.18.2"]; len(got) == 0 {
		t.Fatal("declared deps not persisted")
	}

	// Deprecated version is flagged.
	dep, err := c.VersionMetadata(context.Background(), "express", "5.0.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	if !dep.Deprecated {
		t.Fatal("5.0.0-beta.1 should be deprecated")
	}
}

const npmNoDistTagsFixture = `{
  "versions": {
    "4.17.1": {"version": "4.17.1"},
    "4.18.2": {"version": "4.18.2"},
    "5.0.0-beta.1": {"version": "5.0.0-beta.1"}
  },
  "time": {
    "4.17.1": "2019-05-25T00:00:00Z",
    "4.18.2": "2022-10-08T00:00:00Z",
    "5.0.0-beta.1": "2024-01-01T00:00:00Z"
  }
}`

const npmEmptyVersionsFixture = `{
  "dist-tags": {},
  "versions": {},
  "time": {}
}`

func npmFallbackTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestNpmLatestFallback(t *testing.T) {
	srv := npmFallbackTestServer(t, npmNoDistTagsFixture)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL)).Client(EcosystemNPM)
	got, err := c.Latest(context.Background(), "express")
	if err != nil {
		t.Fatal(err)
	}
	if got != "4.18.2" {
		t.Fatalf("Latest = %q, want 4.18.2", got)
	}
}

func TestNpmLatestNotFound(t *testing.T) {
	srv := npmFallbackTestServer(t, npmEmptyVersionsFixture)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL)).Client(EcosystemNPM)
	got, err := c.Latest(context.Background(), "express")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Latest = (%q, %v), want ErrNotFound", got, err)
	}
}

func TestNpmVersionMetadataNotFound(t *testing.T) {
	srv := npmFallbackTestServer(t, npmNoDistTagsFixture)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL)).Client(EcosystemNPM)
	_, err := c.VersionMetadata(context.Background(), "express", "99.99.99")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("VersionMetadata = %v, want ErrNotFound", err)
	}
}
