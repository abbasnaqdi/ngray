package system

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
)

// UntarFile extracts the first tar.gz member whose name ends with suffix to
// dest (0755). Used to pull a single static binary out of a release archive.
func UntarFile(archive, suffix, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("no member ending %q in %s", suffix, archive)
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || !strings.HasSuffix(h.Name, suffix) {
			continue
		}
		return writeBinary(dest, tr)
	}
}

// writeBinary streams r into a sibling temp file then atomically renames it
// over dest. The rename swaps the directory entry, so overwriting a binary
// that is currently running never hits ETXTBSY ("text file busy").
func writeBinary(dest string, r io.Reader) error {
	tmp := dest + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil { //nolint:gosec // trusted release archive
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// UnzipFile extracts the first zip member whose name ends with suffix to dest
// (0755). Used to pull a single static binary out of a release zip.
func UnzipFile(archive, suffix, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, suffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeBinary(dest, rc)
	}
	return fmt.Errorf("no member ending %q in %s", suffix, archive)
}
