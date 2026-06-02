package sni

import (
	"bufio"
	"os"
	"strings"
)

// LoadCandidates reads one host per line, skipping blanks and # comments.
func LoadCandidates(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hosts []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !seen[line] {
			seen[line] = true
			hosts = append(hosts, line)
		}
	}
	return hosts, sc.Err()
}
