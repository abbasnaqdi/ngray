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
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, tr)
		return err
	}
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
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, rc) //nolint:gosec // trusted release archive
		return err
	}
	return fmt.Errorf("no member ending %q in %s", suffix, archive)
}
