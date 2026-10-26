package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// maxUnpackedFile bounds one unpacked file: Phonon's container is about
// 178 MB.
const maxUnpackedFile = 1 << 30

// unpack extracts the regular files named in names from the zstd-compressed
// tar archive into dir, each written to a temporary file and renamed when
// complete. Other entries are skipped; a name missing from the archive is an
// error. Names are plain file names, so nothing lands outside dir.
func unpack(archive, dir string, names []string) error {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" || filepath.Base(n) != n || n == "." || n == ".." {
			return fmt.Errorf("unpack: %q is not a plain file name", n)
		}
		want[n] = true
	}
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	dec, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxWindow(1<<28))
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	defer dec.Close()
	tr := tar.NewReader(dec)
	for len(want) > 0 {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("unpack %s: %w", filepath.Base(archive), err)
		}
		name := filepath.Base(filepath.Clean(hdr.Name))
		if hdr.Typeflag != tar.TypeReg || !want[name] || filepath.Clean(hdr.Name) != name {
			continue
		}
		if hdr.Size < 0 || hdr.Size > maxUnpackedFile {
			return fmt.Errorf("unpack: %s is %d bytes", name, hdr.Size)
		}
		if err := writeFile(filepath.Join(dir, name), io.LimitReader(tr, hdr.Size), hdr.Size); err != nil {
			return err
		}
		delete(want, name)
	}
	for name := range want {
		return fmt.Errorf("unpack: %s is not in %s", name, filepath.Base(archive))
	}
	return nil
}

func writeFile(path string, r io.Reader, size int64) error {
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	n, err := io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != size {
		err = fmt.Errorf("%s ended after %d of %d bytes", filepath.Base(path), n, size)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("unpack: %w", err)
	}
	return nil
}
