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
	for _, want := range []string{"ngray", "Server setup", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestDomainFormView(t *testing.T) {
	m := New()
	m.screen = screenDomainForm
	m.domain.domain = "app.example.com"
	m.domain.target = "http://127.0.0.1:8080"
	out := m.View()
	for _, want := range []string{"Domain / proxy", "Domain", "Target", "CF token", "Apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("domain form missing %q", want)
		}
	}
}
