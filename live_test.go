package pkgregistry

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live integration tests against the real public registries. Opt-in (network):
// run with VULNETIX_LIVE_REGISTRY=1. Skipped by default so CI stays hermetic.
func liveOrSkip(t *testing.T) {
	t.Helper()
	if os.Getenv("VULNETIX_LIVE_REGISTRY") != "1" {
		t.Skip("set VULNETIX_LIVE_REGISTRY=1 to run live registry tests")
	}
}

func liveCtx(t *testing.T) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func TestLiveNpm(t *testing.T) {
	liveOrSkip(t)
	ctx, cancel := liveCtx(t)
	defer cancel()
	c, _ := New().Client(EcosystemNPM)

	for _, pkg := range []string{"express", "@babel/core"} { // include a scoped name
		versions, err := c.Versions(ctx, pkg)
		if err != nil {
			t.Fatalf("npm Versions(%s): %v", pkg, err)
		}
		if len(versions) < 10 {
			t.Fatalf("npm Versions(%s): got %d, want many", pkg, len(versions))
		}
		latest, err := c.Latest(ctx, pkg)
		if err != nil || latest == "" {
			t.Fatalf("npm Latest(%s): %q err=%v", pkg, latest, err)
		}
	}

	meta, err := c.VersionMetadata(ctx, "express", "4.18.2")
	if err != nil {
		t.Fatalf("npm VersionMetadata: %v", err)
	}
	if _, ok := findDep(meta.Dependencies, "body-parser"); !ok {
		t.Fatalf("npm express@4.18.2 should declare body-parser; got %d deps", len(meta.Dependencies))
	}
}

func TestLivePyPI(t *testing.T) {
	liveOrSkip(t)
	ctx, cancel := liveCtx(t)
	defer cancel()
	c, _ := New().Client(EcosystemPyPI)

	versions, err := c.Versions(ctx, "requests")
	if err != nil || len(versions) < 10 {
		t.Fatalf("pypi Versions(requests): %d err=%v", len(versions), err)
	}
	latest, err := c.Latest(ctx, "requests")
	if err != nil || latest == "" {
		t.Fatalf("pypi Latest: %q err=%v", latest, err)
	}
	meta, err := c.VersionMetadata(ctx, "requests", "2.31.0")
	if err != nil {
		t.Fatalf("pypi VersionMetadata: %v", err)
	}
	if _, ok := findDep(meta.Dependencies, "urllib3"); !ok {
		t.Fatalf("pypi requests==2.31.0 should declare urllib3; got %d deps", len(meta.Dependencies))
	}
}

func TestLiveCargo(t *testing.T) {
	liveOrSkip(t)
	ctx, cancel := liveCtx(t)
	defer cancel()
	c, _ := New().Client(EcosystemCargo)

	versions, err := c.Versions(ctx, "serde")
	if err != nil || len(versions) < 10 {
		t.Fatalf("cargo Versions(serde): %d err=%v", len(versions), err)
	}
	latest, err := c.Latest(ctx, "serde")
	if err != nil || latest == "" {
		t.Fatalf("cargo Latest: %q err=%v", latest, err)
	}
}

func TestLiveParentUpgradeTarget(t *testing.T) {
	liveOrSkip(t)
	ctx, cancel := liveCtx(t)
	defer cancel()
	r := New()
	// express historically declared a caret range on body-parser; resolving a
	// realistic safe child must not error and should return a usable parent or "".
	got, err := r.ParentUpgradeTarget(ctx, EcosystemNPM, "express", "4.17.1", "body-parser", "1.20.1")
	if err != nil {
		t.Fatalf("ParentUpgradeTarget: %v", err)
	}
	t.Logf("express parent-upgrade target for body-parser@1.20.1 = %q", got)
}
