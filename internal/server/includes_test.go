package server

import (
	"strconv"
	"testing"
)

func TestListenPortParse(t *testing.T) {
	for line, want := range map[string]int{
		"listen 80 default_server;": 80,
		"listen [::]:80;":           80,
		"listen 127.0.0.1:8080;":    8080,
		"listen 443 ssl http2;":     443,
		"listen unix:/run/x.sock;":  -1,
		"server {":                  -1,
	} {
		m := listenPort.FindStringSubmatch(line)
		if want == -1 {
			if m != nil {
				t.Fatalf("%q should not match, got %v", line, m)
			}
			continue
		}
		if m == nil || m[1] != strconv.Itoa(want) {
			t.Fatalf("%q parsed %v, want %d", line, m, want)
		}
	}
}
