package pkgregistry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// crates.io sparse index: newline-delimited JSON, oldest-first.
const cargoIndexFixture = `{"name":"time","vers":"0.3.30","deps":[{"name":"libc","req":"^0.2","kind":"normal","optional":true},{"name":"quickcheck","req":"^1.0","kind":"dev","optional":false}],"yanked":false}
{"name":"time","vers":"0.3.31","deps":[{"name":"libc","req":"^0.2.5","kind":"normal","optional":false}],"yanked":false}
{"name":"time","vers":"0.4.0-rc.1","deps":[],"yanked":false}
{"name":"time","vers":"0.3.29","deps":[],"yanked":true}
`

func cargoTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanity: the sparse path prefix for "time" (4+ chars) is /ti/me/time.
		if r.URL.Path != "/ti/me/time" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(cargoIndexFixture))
	}))
}

func TestCargoIndexPath(t *testing.T) {
	cases := map[string]string{
		"a":     "/1/a",
		"ab":    "/2/ab",
		"abc":   "/3/a/abc",
		"serde": "/se/rd/serde",
		"Time":  "/ti/me/time",
	}
	for in, want := range cases {
		if got := cargoIndexPath(in); got != want {
			t.Errorf("cargoIndexPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCargoVersionsAndLatest(t *testing.T) {
	srv := cargoTestServer(t)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemCargo, srv.URL)).Client(EcosystemCargo)

	versions, err := c.Versions(context.Background(), "time")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 4 {
		t.Fatalf("got %d versions, want 4", len(versions))
	}
	// Latest stable excludes the prerelease (0.4.0-rc.1) and the yanked 0.3.29.
	latest, err := c.Latest(context.Background(), "time")
	if err != nil {
		t.Fatal(err)
	}
	if latest != "0.3.31" {
		t.Fatalf("Latest = %q, want 0.3.31", latest)
	}
}

func TestCargoIndexPathEmpty(t *testing.T) {
	if got := cargoIndexPath(""); got != "" {
		t.Errorf("cargoIndexPath(\"\") = %q, want empty string", got)
	}
}

func FuzzCargoIndexPath(f *testing.F) {
	seeds := []string{"", "a", "ab", "abc", "serde", "Time", "my-crate", "ASCII"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got := cargoIndexPath(name)

		n := strings.ToLower(strings.TrimSpace(name))
		switch len(n) {
		case 0:
			if got != "" {
				t.Errorf("cargoIndexPath(%q) = %q, want empty", name, got)
			}
		case 1:
			if got != "/1/"+n {
				t.Errorf("cargoIndexPath(%q) = %q, want /1/%s", name, got, n)
			}
		case 2:
			if got != "/2/"+n {
				t.Errorf("cargoIndexPath(%q) = %q, want /2/%s", name, got, n)
			}
		case 3:
			if got != "/3/"+n[:1]+"/"+n {
				t.Errorf("cargoIndexPath(%q) = %q, want /3/%s/%s", name, got, n[:1], n)
			}
		default:
			if got != "/"+n[:2]+"/"+n[2:4]+"/"+n {
				t.Errorf("cargoIndexPath(%q) = %q, want /%s/%s/%s", name, got, n[:2], n[2:4], n)
			}
		}

		// Case-stability: lowercasing input lowercases output.
		if strings.ToLower(cargoIndexPath(name)) != cargoIndexPath(strings.ToLower(name)) {
			t.Errorf("cargoIndexPath not case-stable for %q", name)
		}
	})
}

func TestCargoDeclaredRangesAndScopes(t *testing.T) {
	srv := cargoTestServer(t)
	defer srv.Close()
	c, _ := New(WithBaseURL(EcosystemCargo, srv.URL)).Client(EcosystemCargo)

	meta, err := c.VersionMetadata(context.Background(), "time", "0.3.30")
	if err != nil {
		t.Fatal(err)
	}
	libc, ok := findDep(meta.Dependencies, "libc")
	if !ok || libc.Range != "^0.2" || libc.Scope != "optional" {
		t.Fatalf("libc = %+v, want range ^0.2 optional", libc)
	}
	qc, ok := findDep(meta.Dependencies, "quickcheck")
	if !ok || qc.Scope != "dev" {
		t.Fatalf("quickcheck = %+v, want dev scope", qc)
	}
}
