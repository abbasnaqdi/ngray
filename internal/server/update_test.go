package server

import (
	"context"
	"fmt"
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

// TestRunUpdateScriptStreams covers the fresh-clone update path: the cloned
// repo's update.sh output must reach the task log line by line.
func TestRunUpdateScriptStreams(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "update.sh")
	if err := os.WriteFile(script, []byte("echo first\necho second\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	err := runUpdateScript(context.Background(), root, func(f string, a ...any) {
		lines = append(lines, fmt.Sprintf(f, a...))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Fatalf("streamed lines = %q, want [first second]", lines)
	}
}
