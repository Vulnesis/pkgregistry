package pkgregistry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
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

// fakeDoer is a test httpDoer that returns scripted responses and records the
// requests it receives so tests can assert retry behaviour and headers.
type fakeDoer struct {
	mu    sync.Mutex
	calls []*http.Request
	steps []fakeDoerStep
	next  int
}

type fakeDoerStep struct {
	response *http.Response
	err      error
}

func newFakeDoer() *fakeDoer {
	return &fakeDoer{}
}

func (f *fakeDoer) addResponse(status int, body []byte) {
	f.steps = append(f.steps, fakeDoerStep{
		response: &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{},
		},
	})
}

func (f *fakeDoer) addError(err error) {
	f.steps = append(f.steps, fakeDoerStep{err: err})
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.next >= len(f.steps) {
		return nil, errors.New("fakeDoer: no scripted response")
	}
	step := f.steps[f.next]
	f.next++
	return step.response, step.err
}

func (f *fakeDoer) request(n int) *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n >= len(f.calls) {
		return nil
	}
	return f.calls[n]
}

func (f *fakeDoer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
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

func TestGetRetryOn5xxThenSuccess(t *testing.T) {
	fake := newFakeDoer()
	fake.addResponse(http.StatusServiceUnavailable, nil)
	fake.addResponse(http.StatusOK, []byte(`{"versions":{"1.0.0":{}}}`))

	userAgent := "test-retry"
	r := New(WithHTTPClient(fake), WithUserAgent(userAgent))
	c, _ := r.Client(EcosystemNPM)
	ctx := context.Background()
	versions, err := c.Versions(ctx, "pkg")
	if err != nil {
		t.Fatalf("Versions = %v, want nil", err)
	}
	if len(versions) != 1 || versions[0].Version != "1.0.0" {
		t.Fatalf("Versions = %v, want [1.0.0]", versions)
	}
	if fake.callCount() != 2 {
		t.Fatalf("retry test made %d calls, want 2", fake.callCount())
	}
	for i := 0; i < fake.callCount(); i++ {
		req := fake.request(i)
		if req == nil {
			t.Fatalf("request %d was nil", i)
		}
		if got := req.Header.Get("Accept"); got != "application/json" {
			t.Errorf("request %d Accept = %q, want application/json", i, got)
		}
		if got := req.Header.Get("User-Agent"); got != userAgent {
			t.Errorf("request %d User-Agent = %q, want %q", i, got, userAgent)
		}
	}
}

func TestGetTransportErrorThenSuccess(t *testing.T) {
	fake := newFakeDoer()
	fake.addError(errors.New("boom"))
	fake.addResponse(http.StatusOK, []byte(`{"versions":{"2.0.0":{}}}`))

	r := New(WithHTTPClient(fake))
	c, _ := r.Client(EcosystemNPM)
	versions, err := c.Versions(context.Background(), "pkg")
	if err != nil {
		t.Fatalf("Versions = %v, want nil", err)
	}
	if len(versions) != 1 || versions[0].Version != "2.0.0" {
		t.Fatalf("Versions = %v, want [2.0.0]", versions)
	}
	if fake.callCount() != 2 {
		t.Fatalf("retry test made %d calls, want 2", fake.callCount())
	}
}

func TestGetNonRetryableStatus(t *testing.T) {
	fake := newFakeDoer()
	fake.addResponse(http.StatusForbidden, nil)
	// Add a successful response that should never be reached.
	fake.addResponse(http.StatusOK, []byte(`{"versions":{"3.0.0":{}}}`))

	r := New(WithHTTPClient(fake))
	c, _ := r.Client(EcosystemNPM)
	_, err := c.Versions(context.Background(), "pkg")
	if err == nil {
		t.Fatalf("Versions = nil, want error")
	}
	if fake.callCount() != 1 {
		t.Fatalf("non-retryable test made %d calls, want 1", fake.callCount())
	}
}

func TestGetContextCancelledDuringRetry(t *testing.T) {
	fake := newFakeDoer()
	fake.addResponse(http.StatusServiceUnavailable, nil)
	fake.addResponse(http.StatusOK, []byte(`{"versions":{"4.0.0":{}}}`))

	r := New(WithHTTPClient(fake))
	c, _ := r.Client(EcosystemNPM)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first request returns so the second attempt observes
	// ctx.Done() during its 200ms backoff.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := c.Versions(ctx, "pkg")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Versions = %v, want context.Canceled", err)
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

// erroringPersister is a Persister that always fails, so tests can assert the
// error handler is invoked without a live registry round trip.
type erroringPersister struct {
	err error
}

func (p *erroringPersister) PersistVersions(_ context.Context, _, _ string, _ []Version) error {
	return p.err
}

func (p *erroringPersister) PersistDeclaredDeps(_ context.Context, _, _, _ string, _ []DeclaredDep) error {
	return p.err
}

func TestNormalizePyPIName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "lowercase", in: "Requests", want: "requests"},
		{name: "dots", in: "ruamel.yaml", want: "ruamel-yaml"},
		{name: "underscores", in: "zope_interface", want: "zope-interface"},
		{name: "whitespace", in: "  Requests  ", want: "requests"},
		{name: "mixed separators", in: "Zope.Interface_Help", want: "zope-interface-help"},
		{name: "empty", in: "", want: ""},
		{name: "whitespace only", in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePyPIName(tt.in); got != tt.want {
				t.Fatalf("normalizePyPIName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRegistryOptionDefaults(t *testing.T) {
	r := New(WithHTTPClient(nil), WithUserAgent(""), WithPersistErrorHandler(nil), WithBaseURL(EcosystemPyPI, ""))

	if r.httpClient == nil {
		t.Fatal("WithHTTPClient(nil) cleared the default HTTP client")
	}
	if r.userAgent != "vulnetix-pkgregistry" {
		t.Fatalf("userAgent = %q, want default %q", r.userAgent, "vulnetix-pkgregistry")
	}
	if r.onPersistErr != nil {
		t.Fatal("WithPersistErrorHandler(nil) set a handler")
	}
	if got := r.baseURL(EcosystemPyPI, "https://pypi.org/simple"); got != "https://pypi.org/simple" {
		t.Fatalf("baseURL with empty override = %q, want default", got)
	}
}

func TestPersistErrorHandlerInvoked(t *testing.T) {
	persistErr := errors.New("persist failed")
	var got error
	r := New(
		WithPersister(&erroringPersister{err: persistErr}),
		WithPersistErrorHandler(func(err error) { got = err }),
	)
	r.persistVersions(context.Background(), EcosystemNPM, "pkg", []Version{{Version: "1.0.0"}})
	if !errors.Is(got, persistErr) {
		t.Fatalf("handler received %v, want %v", got, persistErr)
	}
}
