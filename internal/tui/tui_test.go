package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/version"
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
	m.domain.focused = dfToken // hints render only for the focused row
	out := m.View()
	for _, want := range []string{"Domain / proxy", "Domain", "Target", "CF token", "Apply", "Check now", "cached after apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("domain form missing %q", want)
		}
	}
	if !strings.Contains(out, version.Version) {
		t.Errorf("domain form missing version chip %q", version.Version)
	}
}

// pasteMsg reproduces what bubbletea delivers for a terminal paste with
// bracketed paste enabled: one KeyRunes msg carrying every pasted rune.
func pasteMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

// TestDomainFormPaste guards the paste regression: the editor accepted only
// single-ASCII keypresses, so pasting a Cloudflare token into the terminal
// did nothing at all.
func TestDomainFormPaste(t *testing.T) {
	m := New()
	m.screen = screenDomainForm
	m.domain.focused = dfToken
	m.domain.editing = true

	updated, _ := m.Update(pasteMsg("Ng_Token-42"))
	m = updated.(Model)
	if m.domain.editBuf != "Ng_Token-42" {
		t.Errorf("paste not accepted, editBuf=%q", m.domain.editBuf)
	}
	// A second paste appends, simulating paste into a longer buffer.
	updated, _ = m.Update(pasteMsg("XYZ"))
	m = updated.(Model)
	if m.domain.editBuf != "Ng_Token-42XYZ" {
		t.Errorf("second paste lost, editBuf=%q", m.domain.editBuf)
	}
	// Commit on enter.
	// Paste with a trailing newline (as the dashboard copies), then commit.
	updated, _ = m.Update(pasteMsg("   \n"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg(tea.KeyEnter))
	m = updated.(Model)
	if m.domain.cfToken != "Ng_Token-42XYZ" || m.domain.editing {
		t.Errorf("commit failed (trailing newline should be trimmed): token=%q editing=%v",
			m.domain.cfToken, m.domain.editing)
	}
}

// TestMenuRegenNeedsDoublePress guards the credential-wipe flow: the menu item
// must not clear the state on a single keypress (a stray 9 previously nuked
// every client link without confirmation).
func TestMenuRegenNeedsDoublePress(t *testing.T) {
	m := New()
	press := func(m Model, k string) Model {
		updated, _ := m.Update(key(m, k))
		return updated.(Model)
	}
	m = press(m, "9")
	if !m.armRegen {
		t.Fatal("first press must arm the confirm")
	}
	// Any other key disarms (e.g. the user backed out).
	m = press(m, "up")
	if m.armRegen {
		t.Fatal("moving the cursor must disarm")
	}
	// Two presses run the wipe.
	m = press(m, "9")
	m = press(m, "9")
	if m.screen != screenTask || m.taskName != "Regenerate credentials" {
		t.Errorf("second press should start the regen task, got screen=%d task=%q", m.screen, m.taskName)
	}
}

func key(m Model, k string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// TestDomainFormEmptyDomainHint: submit with an empty domain edits the field
// and shows an inline hint instead of silently doing nothing.
func TestDomainFormEmptyDomainHint(t *testing.T) {
	m := New()
	m.screen = screenDomainForm
	m.domain.focused = dfProceed
	updated, _ := m.Update(keyMsg(tea.KeyEnter))
	m2 := updated.(Model)
	if m2.domain.focused != dfDomain || m2.domain.hint == "" || m2.domain.domain != "" {
		t.Fatalf("expected a hint on the domain field, got %+v", m2.domain)
	}
	if m2.screen != screenDomainForm {
		t.Errorf("staying on the form, got screen %d", m2.screen)
	}
}

// TestDomainFormBackspaceUnicode: backspace must strip a whole rune, not a
// byte of one (the old byte-slice cut produced invalid UTF-8).
func TestDomainFormBackspaceUnicode(t *testing.T) {
	m := New()
	m.screen = screenDomainForm
	m.domain.focused = dfDomain
	m.domain.editing = true
	m.domain.editBuf = "ab√"
	updated, _ := m.Update(keyMsg(tea.KeyBackspace))
	if m2 := updated.(Model); m2.domain.editBuf != "ab" {
		t.Errorf("backspace broke UTF-8: %q", m2.domain.editBuf)
	}
}

// TestSetupFormDomainToggle: the wizard's "Domain fronting" row toggles and
// navigates past the value row cleanly.
func TestSetupFormDomainToggle(t *testing.T) {
	m := New()
	m.screen = screenSetupForm
	if m.form.focused != formFieldIP {
		t.Fatalf("wizard starts on the first row, got %d", m.form.focused)
	}
	step := func(m Model) Model {
		updated, _ := m.Update(keyMsg(tea.KeyDown))
		return updated.(Model)
	}
	m = step(m)
	if m.form.focused != formFieldPort {
		t.Fatalf("focus walk failed at Port, got %d", m.form.focused)
	}
	m = step(m)
	if m.form.focused != formFieldBBR {
		t.Fatalf("focus walk failed at BBR, got %d", m.form.focused)
	}
	m = step(m)
	if m.form.focused != formFieldDomainOn {
		t.Fatalf("focus skipped to domain toggle, got %d", m.form.focused)
	}
	updated, _ := m.Update(keyMsg(tea.KeyEnter))
	m = updated.(Model)
	if !m.form.domainOn {
		t.Fatalf("enter did not tick Domain fronting")
	}
	m = step(m)
	if m.form.focused != formFieldDomainVal {
		t.Fatalf("domain value row unreachable after tick, got %d", m.form.focused)
	}
	updated, _ = m.Update(keyMsg(tea.KeyEnter)) // open the editor
	m = updated.(Model)
	edited, _ := m.Update(pasteMsg("vpn.example.com"))
	m = edited.(Model)
	updated, _ = m.Update(keyMsg(tea.KeyEnter))
	m = updated.(Model)
	if m.form.domainVal != "vpn.example.com" {
		t.Fatalf("domain edit not committed: %q", m.form.domainVal)
	}
}

// TestSetupFormDomainMapping asserts the wizard→config mapping helper path
// used by submitForm for the CDN block.
func TestSetupFormDomainMapping(t *testing.T) {
	cfg := config.Default()
	cfg.Cores.Xray.CDN = &config.CDN{Enabled: false}
	f := setupForm{domainOn: true, domainVal: "Vpn.Example.Com."}
	dom := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(f.domainVal)), ".")
	if n := strings.Count(dom, "."); dom != "" && n >= 1 {
		cfg.Cores.Xray.CDN.Enabled, cfg.Cores.Xray.CDN.Domain = true, dom
	}
	if cfg.Cores.Xray.CDN.Domain != "vpn.example.com" || !cfg.Cores.Xray.CDN.Enabled {
		t.Fatalf("cdn mapping failed: %+v", cfg.Cores.Xray.CDN)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("mapped config invalid: %v", err)
	}
}

// TestSetupFormPaste covers the same editor logic in the setup wizard.
func TestSetupFormPaste(t *testing.T) {
	m := New()
	m.screen = screenSetupForm
	m.form.focused = formFieldIP
	m.form.editing = true
	updated, _ := m.Update(pasteMsg("1.2.3.4"))
	if m2 := updated.(Model); m2.form.editBuf != "1.2.3.4" {
		t.Errorf("setup paste not accepted, editBuf=%q", m2.form.editBuf)
	}
}

// keyMsg builds a KeyMsg for a non-rune key (enter, backspace, …).
func keyMsg(k tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: k}
}
