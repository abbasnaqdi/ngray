package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

var semverRE = regexp.MustCompile(`v?(\d+\.\d+\.\d+)`)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (e *Env) latestRelease(ctx context.Context, repo string) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github %s: %s", repo, resp.Status)
	}
	var r ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// LatestVersion returns the newest release's semver (e.g. "app/v2.9.2" -> "2.9.2").
func (e *Env) LatestVersion(ctx context.Context, repo string) (string, error) {
	r, err := e.latestRelease(ctx, repo)
	if err != nil {
		return "", err
	}
	if v := ParseSemver(r.TagName); v != "" {
		return v, nil
	}
	return strings.TrimPrefix(r.TagName, "v"), nil
}

// LatestTag returns the newest release's tag name (e.g. "v0.7.1-abc1234").
func (e *Env) LatestTag(ctx context.Context, repo string) (string, error) {
	r, err := e.latestRelease(ctx, repo)
	if err != nil {
		return "", err
	}
	return r.TagName, nil
}

// AssetURL returns the download URL of the latest-release asset matching pattern.
func (e *Env) AssetURL(ctx context.Context, repo, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	r, err := e.latestRelease(ctx, repo)
	if err != nil {
		return "", err
	}
	for _, a := range r.Assets {
		if re.MatchString(a.Name) {
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("no asset matching %q in %s", pattern, repo)
}

func (e *Env) Download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	// Atomic: write to a temp file in the same dir, then rename.
	tmp := dest + ".part"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ParseSemver extracts the first x.y.z version from arbitrary tool output.
func ParseSemver(s string) string {
	if m := semverRE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}
