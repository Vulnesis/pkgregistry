package pkgregistry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordingPersister captures what the clients persist so tests can assert that
// fetched data is written through when a Persister is configured.
type recordingPersister struct {
	versions map[string][]Version
	deps     map[string][]DeclaredDep
}

func newRecordingPersister() *recordingPersister {
	return &recordingPersister{versions: map[string][]Version{}, deps: map[string][]DeclaredDep{}}
}

func (p *recordingPersister) PersistVersions(_ context.Context, ecosystem, pkg string, v []Version) error {
	p.versions[ecosystem+":"+pkg] = v
	return nil
}

func (p *recordingPersister) PersistDeclaredDeps(_ context.Context, ecosystem, pkg, version string, d []DeclaredDep) error {
	p.deps[ecosystem+":"+pkg+"@"+version] = d
	return nil
}

func TestRegistryClientDispatch(t *testing.T) {
	r := New()
	for _, eco := range []string{EcosystemNPM, EcosystemPyPI, EcosystemCargo, "NPM"} {
		if _, err := r.Client(eco); err != nil {
			t.Fatalf("Client(%q) unexpected error: %v", eco, err)
		}
	}
	if _, err := r.Client("conda"); !errors.Is(err, ErrUnsupportedEcosystem) {
		t.Fatalf("Client(conda) = %v, want ErrUnsupportedEcosystem", err)
	}
}

// failingPersister always returns a fixed error. It is used to verify that
// persistence errors are delivered to the handler but do not fail the fetch.
type failingPersister struct{ err error }

func (p *failingPersister) PersistVersions(_ context.Context, _, _ string, _ []Version) error {
	return p.err
}

func (p *failingPersister) PersistDeclaredDeps(_ context.Context, _, _, _ string, _ []DeclaredDep) error {
	return p.err
}

func TestPersistErrorHandlerInvoked(t *testing.T) {
	srv := npmTestServer(t)
	defer srv.Close()

	sentinel := errors.New("persistversions failed")
	p := &failingPersister{err: sentinel}

	var gotErr error
	r := New(
		WithBaseURL(EcosystemNPM, srv.URL),
		WithPersister(p),
		WithPersistErrorHandler(func(err error) { gotErr = err }),
	)
	c, _ := r.Client(EcosystemNPM)

	versions, err := c.Versions(context.Background(), "express")
	if err != nil {
		t.Fatalf("Versions returned error: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	if !errors.Is(gotErr, sentinel) {
		t.Fatalf("handler received %v, want %v", gotErr, sentinel)
	}
}

// panicPersister fails the test if any persistence method is called.
type panicPersister struct{ t *testing.T }

func (p *panicPersister) PersistVersions(_ context.Context, _, _ string, _ []Version) error {
	p.t.Fatalf("PersistVersions should not be called for empty slices")
	return nil
}

func (p *panicPersister) PersistDeclaredDeps(_ context.Context, _, _, _ string, _ []DeclaredDep) error {
	p.t.Fatalf("PersistDeclaredDeps should not be called for empty slices")
	return nil
}

// fixtureWithNoDeps returns a packument whose requested version has no
// dependency maps, so npmClient produces an empty []DeclaredDep.
func fixtureWithNoDeps(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"dist-tags": {"latest": "1.0.0"},
			"versions": {"1.0.0": {"version": "1.0.0"}}
		}`))
	}))
}

func TestPersistSkipsEmpty(t *testing.T) {
	srv := fixtureWithNoDeps(t)
	defer srv.Close()

	p := &panicPersister{t: t}
	c, _ := New(WithBaseURL(EcosystemNPM, srv.URL), WithPersister(p)).Client(EcosystemNPM)

	meta, err := c.VersionMetadata(context.Background(), "no-deps", "1.0.0")
	if err != nil {
		t.Fatalf("VersionMetadata returned error: %v", err)
	}
	if meta == nil {
		t.Fatal("VersionMetadata returned nil metadata")
	}
	if len(meta.Dependencies) != 0 {
		t.Fatalf("got %d deps, want 0", len(meta.Dependencies))
	}
}

func TestGetReturnsNotFoundOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := New(WithBaseURL(EcosystemNPM, srv.URL))
	c, _ := r.Client(EcosystemNPM)
	if _, err := c.Versions(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Versions on 404 = %v, want ErrNotFound", err)
	}
}

func findDep(deps []DeclaredDep, name string) (DeclaredDep, bool) {
	for _, d := range deps {
		if d.Name == name {
			return d, true
		}
	}
	return DeclaredDep{}, false
}
