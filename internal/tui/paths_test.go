package tui

import (
	"strings"
	"testing"
)

func TestParseAndValidatePathPorts(t *testing.T) {
	if got := parsePathPorts(" 20128 , 3000 ,, "); len(got) != 2 || got[0] != 20128 || got[1] != 3000 {
		t.Fatalf("parse = %v", got)
	}
	for bad, want := range map[string]string{
		"abc":       "numbers",
		"20128, x":  "numbers",
		"70000":     "out of range",
		"80":        "subscription handler",
		"20128, 80": "subscription handler",
	} {
		if got := validatePathPorts(bad); got == "" || !strings.Contains(got, want) {
			t.Fatalf("validatePathPorts(%q) = %q, want containing %q", bad, got, want)
		}
	}
	if got := validatePathPorts("20128"); got != "" {
		t.Fatalf("valid list rejected: %q", got)
	}
}
