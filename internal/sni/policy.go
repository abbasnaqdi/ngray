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

// MaxMedianMS caps the handshake latency the SERVER (typically in Europe)
// measures toward the SNI destination. Beyond this the origin usually sits on
// another continent (US/AU), which inflates the already-long
// Iran→Europe→origin route — camouflage is not worth that round trip.
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

// Slow reports whether the measured latency disqualifies the host per
// MaxMedianMS.
func Slow(r Result) bool { return r.MedianMS > MaxMedianMS }

// operatorFit is the full selection policy: REALITY-fit, latency-capped and
// not an Iran-filtered brand.
func operatorFit(r Result) bool { return r.Fit() && !Slow(r) && !IranFiltered(r.Host) }
