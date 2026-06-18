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
