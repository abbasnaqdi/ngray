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

func TestLoadTiered(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.sni")
	os.WriteFile(p, []byte("rust-lang.org\npython.org\n#@extended\n# fallback\nkraken.com\nrust-lang.org\n"), 0o644)

	pref, ext, err := LoadTiered(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pref) != 2 || pref[0] != "rust-lang.org" || pref[1] != "python.org" {
		t.Errorf("preferred = %v", pref)
	}
	// kraken.com is extended; the duplicate rust-lang.org (already preferred) drops.
	if len(ext) != 1 || ext[0] != "kraken.com" {
		t.Errorf("extended = %v", ext)
	}
}

func TestRankedTieredPrefersPreferred(t *testing.T) {
	// An extended host with lower latency must still rank behind a slower
	// preferred host.
	results := []Result{
		{Host: "ext-fast.com", Status: StatusOK, MedianMS: 10},
		{Host: "pref-slow.org", Status: StatusOK, MedianMS: 200},
		{Host: "dead.invalid", Status: StatusBlocked},
	}
	ranked := RankedTiered(results, map[string]bool{"pref-slow.org": true})
	if len(ranked) != 2 || ranked[0].Host != "pref-slow.org" || ranked[1].Host != "ext-fast.com" {
		t.Errorf("tier order wrong: %+v", ranked)
	}
}
