package system

import (
	"os"
	"path/filepath"
)

// Path applies the sandbox root prefix to an absolute path.
func (e *Env) Path(p string) string {
	if e.Root == "" {
		return p
	}
	return filepath.Join(e.Root, p)
}

// WriteFile creates parent dirs and writes data at the (root-prefixed) path.
// Dirs are 0755 (traverse-allowed): nginx workers run as www-data, and a
// root:root 0750 /etc/ngray once made every subscription file unreadable
// ("404 Not Found nginx" on ALL sub URLs). Files keep their own modes.
// An existing too-tight dir is healed to 0755 on every write.
func (e *Env) WriteFile(path string, data []byte, mode os.FileMode) error {
	p := e.Path(path)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() && info.Mode().Perm()&0o005 != 0o005 {
		_ = os.Chmod(dir, 0o755) // heal a too-tight dir so nginx workers can traverse
	}
	return os.WriteFile(p, data, mode)
}

func (e *Env) WriteString(path, data string, mode os.FileMode) error {
	return e.WriteFile(path, []byte(data), mode)
}

func (e *Env) MkdirAll(path string) error { return os.MkdirAll(e.Path(path), 0o755) }

func (e *Env) ReadFile(path string) ([]byte, error) { return os.ReadFile(e.Path(path)) }

func (e *Env) Stat(path string) (os.FileInfo, error) { return os.Stat(e.Path(path)) }

func (e *Env) Exists(path string) bool {
	_, err := e.Stat(path)
	return err == nil
}

func (e *Env) Remove(path string) error { return os.Remove(e.Path(path)) }

func (e *Env) RemoveAll(path string) error { return os.RemoveAll(e.Path(path)) }

// CopyFile copies src to dst (both root-prefixed), preserving mode.
func (e *Env) CopyFile(src, dst string) error {
	b, err := e.ReadFile(src)
	if err != nil {
		return err
	}
	return e.WriteFile(dst, b, 0o644)
}
