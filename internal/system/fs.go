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
func (e *Env) WriteFile(path string, data []byte, mode os.FileMode) error {
	p := e.Path(path)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, data, mode)
}

func (e *Env) WriteString(path, data string, mode os.FileMode) error {
	return e.WriteFile(path, []byte(data), mode)
}

func (e *Env) MkdirAll(path string) error { return os.MkdirAll(e.Path(path), 0o750) }

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
