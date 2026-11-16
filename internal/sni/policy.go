package sni

import "strings"

// iranDeniedSuffixes are registrable domains whose sites are filtered (or
// heavily interfered with) on Iranian networks. A REALITY handshake that
// borrows one of these as its SNI draws an instant RST from inside Iran, so
// they are worthless to pick even when the SERVER can reach them fast.
// Suffix-matched, lowercase, no leading dot.
var iranDeniedSuffixes = []string{
	"google.com", "googleusercontent.com", "gstatic.com", "googleapis.com",
	"googlevideo.com", "youtube.com", "ytimg.com", "ggpht.com",
	"twitter.com", "x.com", "t.co", "twimg.com",
	"facebook.com", "fbcdn.net", "instagram.com", "cdninstagram.com",
	"whatsapp.net", "whatsapp.com",
	"telegram.org", "t.me", "telegram.me", "tdesktop.com",
	"wikipedia.org", "wikimedia.org",
	"reddit.com", "redditstatic.com",
	"medium.com", "spotify.com", "soundcloud.com",
	"github.com", "githubusercontent.com", "gitlab.com",
	"openai.com", "chatgpt.com", "anthropic.com", "claude.ai",
	"netflix.com", "tiktok.com", "tiktokcdn.com", "discord.com", "discordapp.net",
	"yahoo.com", "bing.com", "duckduckgo.com",
	"blueskyweb.xyz", "bsky.app",
	"archive.org", "archive.today",
}

// MaxMedianMS is the preferred handshake latency threshold. Hosts slower than
// this are ranked after fast ones but are NOT discarded: on blocked networks a
// larger, diverse pool is worth more than a perfect handshake RTT, and the
// caller applies a hard 2s dead-weight cutoff separately.
const MaxMedianMS = 280

// IranFiltered reports whether a host borrows an Iran-blocked organisation's
// domain as its SNI.
func IranFiltered(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, s := range iranDeniedSuffixes {
		if h == s || strings.HasSuffix(h, "."+s) {
			return true
		}
	}
	return false
}

// Slow reports whether the measured latency is above the preferred threshold.
func Slow(r Result) bool { return r.MedianMS > MaxMedianMS }

// operatorFit applies the hard policy: REALITY-fit and not an Iran-filtered
// brand. Latency only affects ordering (Score), so a thin fast tier cannot
// starve the pool down to a handful of SNIs.
func operatorFit(r Result) bool { return r.Fit() && !IranFiltered(r.Host) }
