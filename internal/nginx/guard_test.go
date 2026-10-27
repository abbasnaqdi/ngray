package nginx

import "strings"
import "testing"

// The subscription locations must reject any token other than the CURRENT one,
// so rotated links die immediately (user: "لینک عوض شد ← قدیمی در دسترس نباشن").
func TestRenderTokenGuard(t *testing.T) {
	out := Config{
		Port: 443, Sink: 8442,
		Sub: &Subscription{Dir: "/etc/ngray", BundleFile: "bundle", SingboxFile: "sb.json", Token: "tokNOW"},
	}.Render()
	for _, want := range []string{
		`if ($subtok != "tokNOW") { return 404; }`,
		`if ($subsb != "tokNOW") { return 404; }`,
		"try_files /bundle =404;",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

// The CDN server block guards /s and /sb with the token as well.
func TestRenderCDNSubTokenGuard(t *testing.T) {
	out := Config{
		Port: 443, Sink: 8442,
		CDNSub: &CDNSub{Domain: "www.example.com", Backend: 8444, Dir: "/etc/ngray",
			BundleFile: "bundle", SingboxFile: "sb.json", Token: "tok77"},
	}.Render()
	if !strings.Contains(out, `if ($subtok != "tok77") { return 404; }`) ||
		!strings.Contains(out, `if ($subsb != "tok77") { return 404; }`) {
		t.Fatalf("CDN sub block lacks token guard:\n%s", out)
	}
}

// The mixed text route (/sm/) is token-guarded exactly like /s/ and /sb/.
func TestRenderMixedGuard(t *testing.T) {
	out := Config{
		Port: 443, Sink: 8442,
		Sub: &Subscription{Dir: "/etc/ngray", BundleFile: "bundle",
			SingboxFile: "sb.json", MixedFile: "mixed.txt", Token: "tokNOW"},
	}.Render()
	for _, want := range []string{
		`^/sm/(?<subsm>[0-9a-f]+)$`,
		`if ($subsm != "tokNOW") { return 404; }`,
		"try_files /mixed.txt =404;",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
