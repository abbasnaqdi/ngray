package system

import "testing"

func TestParseSemver(t *testing.T) {
	cases := map[string]string{
		"shoes 0.3.1":                  "0.3.1",
		"sing-box version 1.10.7":      "1.10.7",
		"Xray 25.1.30 (Xray, Penetra)": "25.1.30",
		"v2.6.0":                       "2.6.0",
		"no version here":              "",
	}
	for in, want := range cases {
		if got := ParseSemver(in); got != want {
			t.Errorf("ParseSemver(%q) = %q, want %q", in, got, want)
		}
	}
}
