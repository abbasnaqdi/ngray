package sni

import "testing"

func TestIranFiltered(t *testing.T) {
	for host, want := range map[string]bool{
		"www.google.com": true, "gstatic.com": true, "assets.site.google.com": true,
		"www.debian.org": false, "python.org": false, "cdn.jsdelivr.net": false,
		"x.com": true, "t.me": true, "apexrt.me": false, "WWW.TWITTER.COM.": true,
	} {
		if got := IranFiltered(host); got != want {
			t.Errorf("IranFiltered(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestOperatorFitGate(t *testing.T) {
	ok := Result{Host: "fast.example.com", Status: StatusOK, MedianMS: 40}
	if !operatorFit(ok) {
		t.Fatal("fit fast host must survive")
	}
	slow := ok
	slow.MedianMS = 900
	if operatorFit(slow) {
		t.Fatal("ping beyond MaxMedianMS must be excluded")
	}
	filtered := ok
	filtered.Host = "cdn.telegram.org"
	if operatorFit(filtered) {
		t.Fatal("Iran-filtered brand must be excluded")
	}
}
