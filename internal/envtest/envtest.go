//go:build envtest

// Package envtest starts a real Kubernetes API server for tests that need one.
// It is the middle layer: faster and more honest than e2e, and able to check the
// things a pure test cannot, that the admission policy compiles and denies, and
// that the watcher discovers what it should.
package envtest

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Start brings up an API server with the minimal CAPI CRDs and returns its config.
// The binaries come from `make envtest`, which pins the Kubernetes version in
// versions.env.
func Start(t *testing.T) *rest.Config {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is unset; run: make test-envtest")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(root(t), "testdata", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	config, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	return config
}

func root(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Tests run from their own package directory; the repo root is two levels up
	// from internal/<pkg>.
	return filepath.Dir(filepath.Dir(wd))
}
