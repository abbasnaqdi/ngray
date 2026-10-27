package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckoutRootFind walks up from nested dirs to the .git marker, exactly
// the case of a server running `sudo ngray update --self` from ~ itself while
// the checkout lives in ~/ngray.
func TestCheckoutRootFind(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd) //nolint
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}
	if got := checkoutRoot(); got != root {
		t.Errorf("checkoutRoot = %q, want %q", got, root)
	}
	// Outside any checkout it must return "" (no crash).
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := checkoutRoot(); got != "" {
		t.Errorf("checkoutRoot outside a repo = %q, want \"\"", got)
	}
}
