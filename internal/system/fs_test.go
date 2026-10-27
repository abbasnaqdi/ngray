package system

import (
	"os"
	"path/filepath"
	"testing"
)

// subscription 404s were caused by 0755-hostile parent dirs: the nginx worker
// (www-data) could not traverse /etc/ngray, so try_files always failed.
func TestWriteFileHealsTightDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "ngray")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	e := &Env{}
	p := sub + "/bundle"
	if err := e.WriteString(p, "data", 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sub)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o005 != 0o005 {
		t.Fatalf("dir perm was not healed: %o — nginx worker still cannot traverse", perm)
	}
	if b, _ := os.ReadFile(p); string(b) != "data" {
		t.Fatalf("content = %q", b)
	}
}

func TestWriteFileCreatesTraversableDirs(t *testing.T) {
	e := &Env{}
	p := filepath.Join(t.TempDir(), "deep", "nested", "f.txt")
	if err := e.WriteString(p, "x", 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o005 != 0o005 {
		t.Fatalf("created dir perm %o — must be world-traversable (10/404 lesson)", perm)
	}
}
