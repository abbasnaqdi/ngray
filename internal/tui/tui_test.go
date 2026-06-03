package tui

import (
	"strings"
	"testing"
)

func TestViewRenders(t *testing.T) {
	m := New()
	m.updates = map[string]string{"shoes": "0.3.0"}
	m.checked = true
	out := m.View()
	for _, want := range []string{"ng-ray", "Server setup", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}
