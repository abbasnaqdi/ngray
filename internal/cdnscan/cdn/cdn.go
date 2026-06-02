package cdn

import (
	"net/http"
	"strings"
)

// Provider describes a CDN: its name, IP ranges, front domain for testing,
// per-CDN header fingerprints, and output file name.
type Provider struct {
	Name        string
	Ranges      []string
	FrontDomain string
	OutputFile  string
	IsEdge      func(*http.Response) bool
}

// Registry holds all supported CDN providers in display order.
var Registry = []*Provider{
	Akamai,
	Cloudflare,
	Fastly,
	ArvanCloud,
	Gcore,
	CloudFront,
}

// ──────────────────────────────────────────────────────────────────────────────
// Akamai  (ASN 20940, 16625, 30675)
// ──────────────────────────────────────────────────────────────────────────────

var Akamai = &Provider{
	Name: "Akamai",
	Ranges: []string{
		"2.16.0.0/13",
		"23.0.0.0/12",
		"23.32.0.0/11",
		"23.192.0.0/11",
		"72.246.0.0/15",
		"96.6.0.0/15",
		"96.16.0.0/15",
		"104.64.0.0/10",
		"172.224.0.0/12",
		"173.222.0.0/15",
		"184.24.0.0/13",
		"184.50.0.0/15",
		"184.84.0.0/14",
	},
	FrontDomain: "www.akamai.com",
	OutputFile:  "akamai_clean.txt",
	IsEdge:      isAkamaiEdge,
}

func isAkamaiEdge(resp *http.Response) bool {
	if strings.Contains(strings.ToLower(resp.Header.Get("Server")), "akamaighost") {
		return true
	}
	for _, h := range []string{
		"X-Akam-SW-Version",
		"Akamai-GRN",
		"Akamai-Origin-Hop",
		"X-Akamai-Transformed",
		"X-Check-Cacheable",
		"X-Serial",
		"X-True-Cache-Key",
	} {
		if resp.Header.Get(h) != "" {
			return true
		}
	}
	if st := resp.Header.Get("Server-Timing"); strings.Contains(st, "ak_") {
		return true
	}
	for _, c := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(c, "ak_bmsc") || strings.Contains(c, "akavpau_") {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// Cloudflare  (ASN 13335)
// ──────────────────────────────────────────────────────────────────────────────

var Cloudflare = &Provider{
	Name: "Cloudflare",
	Ranges: []string{
		"103.21.244.0/22",
		"103.22.200.0/22",
		"103.31.4.0/22",
		"104.16.0.0/13",
		"104.24.0.0/14",
		"108.162.192.0/18",
		"131.0.72.0/22",
		"141.101.64.0/18",
		"141.101.112.0/20",
		"162.158.0.0/15",
		"172.64.0.0/13",
		"188.114.96.0/20",
		"190.93.240.0/20",
		"197.234.240.0/22",
		"198.41.128.0/17",
	},
	FrontDomain: "cloudflare.com",
	OutputFile:  "cloudflare_clean.txt",
	IsEdge:      isCloudflareEdge,
}

func isCloudflareEdge(resp *http.Response) bool {
	server := strings.ToLower(resp.Header.Get("Server"))
	if server == "cloudflare" || strings.HasPrefix(server, "cloudflare") {
		return true
	}
	for _, h := range []string{"CF-Ray", "cf-cache-status", "CF-Request-ID", "Cf-Polished"} {
		if resp.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// Fastly  (ASN 54113)
// ──────────────────────────────────────────────────────────────────────────────

var Fastly = &Provider{
	Name: "Fastly",
	Ranges: []string{
		"23.235.32.0/20",
		"23.235.48.0/20",
		"43.249.72.0/22",
		"104.156.80.0/20",
		"146.75.0.0/16",
		"151.101.0.0/16",
		"157.52.64.0/18",
		"167.82.0.0/17",
		"172.111.64.0/18",
		"185.31.16.0/22",
		"199.27.72.0/21",
		"199.232.0.0/16",
	},
	FrontDomain: "www.fastly.com",
	OutputFile:  "fastly_clean.txt",
	IsEdge:      isFastlyEdge,
}

func isFastlyEdge(resp *http.Response) bool {
	for _, h := range []string{"X-Served-By", "X-Fastly-Request-ID", "Fastly-Debug-Digest"} {
		if resp.Header.Get(h) != "" {
			return true
		}
	}
	server := strings.ToLower(resp.Header.Get("Server"))
	return strings.HasPrefix(server, "varnish")
}

// ──────────────────────────────────────────────────────────────────────────────
// ArvanCloud  (ASN 47447, 205540 — Iranian CDN, often whitelisted by ISPs)
// ──────────────────────────────────────────────────────────────────────────────

var ArvanCloud = &Provider{
	Name: "ArvanCloud",
	Ranges: []string{
		"5.34.206.0/23",
		"5.34.208.0/21",
		"5.34.216.0/21",
		"37.32.16.0/21",
		"37.32.24.0/21",
		"37.32.32.0/21",
		"37.32.40.0/21",
		"85.133.128.0/19",
		"92.114.16.0/20",
		"185.215.232.0/22",
		"194.5.176.0/22",
		"213.32.7.0/24",
	},
	FrontDomain: "www.arvancloud.ir",
	OutputFile:  "arvancloud_clean.txt",
	IsEdge:      isArvanCloudEdge,
}

func isArvanCloudEdge(resp *http.Response) bool {
	server := strings.ToLower(resp.Header.Get("Server"))
	if strings.Contains(server, "arvan") {
		return true
	}
	for _, h := range []string{"X-Arvan-Cache", "Ar-Powered-By", "X-Arvan-Edge-Location-Timeout"} {
		if resp.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// Gcore  (ASN 199524)
// ──────────────────────────────────────────────────────────────────────────────

var Gcore = &Provider{
	Name: "Gcore",
	Ranges: []string{
		"5.188.64.0/19",
		"46.235.164.0/22",
		"92.223.116.0/22",
		"185.45.80.0/22",
		"185.68.152.0/22",
		"195.133.144.0/21",
		"213.239.128.0/21",
	},
	FrontDomain: "gcore.com",
	OutputFile:  "gcore_clean.txt",
	IsEdge:      isGcoreEdge,
}

func isGcoreEdge(resp *http.Response) bool {
	server := strings.ToLower(resp.Header.Get("Server"))
	if strings.Contains(server, "gcore") || strings.Contains(server, "g-core") {
		return true
	}
	// Gcore uses X-Id for internal routing ID
	if resp.Header.Get("X-Id") != "" {
		return true
	}
	// CDN-Cache-Control is used by Gcore
	if resp.Header.Get("CDN-Cache-Control") != "" {
		return true
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// AWS CloudFront  (ASN 16509)
// ──────────────────────────────────────────────────────────────────────────────

var CloudFront = &Provider{
	Name: "CloudFront",
	Ranges: []string{
		"13.32.0.0/15",
		"13.35.0.0/16",
		"52.46.0.0/18",
		"54.182.0.0/16",
		"64.252.64.0/18",
		"99.84.0.0/16",
		"130.176.0.0/17",
		"143.204.0.0/16",
		"204.246.164.0/22",
		"205.251.192.0/19",
		"216.137.32.0/19",
	},
	FrontDomain: "d111111abcdef8.cloudfront.net",
	OutputFile:  "cloudfront_clean.txt",
	IsEdge:      isCloudFrontEdge,
}

func isCloudFrontEdge(resp *http.Response) bool {
	for _, h := range []string{"X-Amz-Cf-Id", "X-Amz-Cf-Pop"} {
		if resp.Header.Get(h) != "" {
			return true
		}
	}
	via := strings.ToLower(resp.Header.Get("Via"))
	if strings.Contains(via, "cloudfront") {
		return true
	}
	xCache := strings.ToLower(resp.Header.Get("X-Cache"))
	return strings.Contains(xCache, "cloudfront")
}
