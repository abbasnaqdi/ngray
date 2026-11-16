package cores

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

// TestInstallDownloadedReplacesAtomically guards the hysteria install path:
// the download must land next to the destination, not in /tmp, so a separate
// /tmp filesystem cannot make the final rename fail with EXDEV.
func TestInstallDownloadedReplacesAtomically(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("new-binary"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "bin", "hysteria")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	sys := &system.Env{HTTP: srv.Client()}
	if err := installDownloaded(context.Background(), sys, srv.URL, dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("dest content = %q, want new-binary", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("dest mode = %o, want 755", info.Mode().Perm())
	}
	if _, err := os.Stat(dest + ".new"); !os.IsNotExist(err) {
		t.Fatalf("temporary file left behind: %v", err)
	}
}
