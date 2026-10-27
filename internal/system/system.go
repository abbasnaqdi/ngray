// Package system is the dependency-injected boundary to the host: command
// execution, package installation, downloads, services and kernel tuning.
package system

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Env is the dependency-injected host boundary. Root prefixes every filesystem
// path so a sandbox can redirect writes; DryRun logs mutating commands instead
// of executing them so testing never touches the real system.
type Env struct {
	HTTP   *http.Client
	Root   string               // path prefix; "" means the real filesystem
	DryRun bool                 // log+skip external commands instead of running
	Logf   func(string, ...any) // sink for skipped actions
}

func New() *Env {
	return &Env{HTTP: &http.Client{Timeout: 120 * time.Second}}
}

// NewSandbox returns an Env that writes under root and skips host mutations.
func NewSandbox(root string, log func(string, ...any)) *Env {
	return &Env{HTTP: &http.Client{Timeout: 120 * time.Second}, Root: root, DryRun: true, Logf: log}
}

func (e *Env) logf(format string, a ...any) {
	if e.Logf != nil {
		e.Logf(format, a...)
	}
}

// Run executes a command and returns trimmed stdout; stderr is folded into the
// error. In DryRun mode it logs and skips, returning empty success.
func (e *Env) Run(ctx context.Context, name string, args ...string) (string, error) {
	if e.DryRun {
		e.logf("[dry] %s %s", name, strings.Join(args, " "))
		return "", nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// RunStdin executes a command with the given string piped to its stdin.
func (e *Env) RunStdin(ctx context.Context, stdin string, name string, args ...string) error {
	if e.DryRun {
		e.logf("[dry] %s %s (with stdin)", name, strings.Join(args, " "))
		return nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return nil
}

// OK reports whether the command exits zero.
func (e *Env) OK(ctx context.Context, name string, args ...string) bool {
	_, err := e.Run(ctx, name, args...)
	return err == nil
}

func (e *Env) Which(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func (e *Env) AptUpdate(ctx context.Context) error {
	return e.aptRun(ctx, "update", "-qq")
}

func (e *Env) AptInstall(ctx context.Context, pkgs ...string) error {
	if e.DryRun {
		e.logf("[dry] apt-get install %s", strings.Join(pkgs, " "))
		return nil
	}
	return e.aptRun(ctx, append([]string{"install", "-y", "-qq"}, pkgs...)...)
}

// aptRun retries when another dpkg/apt process holds the frontend lock
// (unattended-upgrades, a parallel install) instead of failing the whole
// provisioning run.
func (e *Env) aptRun(ctx context.Context, args ...string) error {
	const attempts = 8
	var last string
	for i := 0; i < attempts; i++ {
		cmd := exec.CommandContext(ctx, "apt-get", args...)
		cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		last = strings.TrimSpace(string(out))
		if !aptLocked(last) {
			return fmt.Errorf("apt %v: %w: %s", args, err, last)
		}
		if i == attempts-1 {
			break
		}
		wait := time.Duration(5*(i+1)) * time.Second
		e.logf("apt is busy (lock held by another process); retrying in %s…", wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return fmt.Errorf("apt %v: package manager stayed locked: %s", args, last)
}

func aptLocked(out string) bool {
	return strings.Contains(out, "Could not get lock") ||
		strings.Contains(out, "Unable to acquire the dpkg frontend lock") ||
		strings.Contains(out, "is another process using it")
}

// HTTPGet fetches url and returns the response body.
func (e *Env) HTTPGet(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return b, nil
}

// PublicIP returns the server's public IPv4, trying several echo services.
func (e *Env) PublicIP(ctx context.Context) string {
	for _, url := range []string{"https://api.ipify.org", "https://ifconfig.me", "https://icanhazip.com"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		resp, err := e.HTTP.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if ip := strings.TrimSpace(string(b)); net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}
