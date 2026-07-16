package store

import (
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path by creating a temp file in the same
// directory and renaming it over the destination. Same-directory is
// load-bearing: os.Rename is only atomic within a single filesystem, so a
// temp file elsewhere (e.g. the system /tmp) could silently break that
// guarantee.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
