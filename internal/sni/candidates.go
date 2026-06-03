package sni

import (
	"bufio"
	"os"
	"strings"
)

// tierMarker separates the preferred tier (above) from the extended fallback
// tier (below) in the candidates file.
const tierMarker = "#@extended"

// LoadCandidates reads one host per line, skipping blanks and # comments.
func LoadCandidates(path string) ([]string, error) {
	pref, ext, err := LoadTiered(path)
	if err != nil {
		return nil, err
	}
	return append(pref, ext...), nil
}

// LoadTiered reads the candidates file split into a preferred tier (lines before
// the "#@extended" marker) and an extended fallback tier (lines after it). When
// the marker is absent every host is preferred. Both lists are de-duplicated
// against each other in file order.
func LoadTiered(path string) (preferred, extended []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	inExt := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == tierMarker {
			inExt = true
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		if inExt {
			extended = append(extended, line)
		} else {
			preferred = append(preferred, line)
		}
	}
	return preferred, extended, sc.Err()
}
