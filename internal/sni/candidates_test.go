package sni

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCandidates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.sni")
	os.WriteFile(p, []byte("# comment\nrust-lang.org\n\nrust-lang.org\npython.org\n"), 0o644)

	got, err := LoadCandidates(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"rust-lang.org", "python.org"} // deduped, comments/blanks dropped
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
