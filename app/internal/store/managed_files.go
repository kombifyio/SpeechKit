package store

import (
	"errors"
	"io/fs"
	"os"

	"github.com/kombifyio/SpeechKit/app/internal/safepath"
)

// ManagedFileRoots is implemented by stores that keep audio and snapshot
// files on disk. The paths a store returns come from database rows, and a
// shared or foreign Postgres backend lets someone else write those rows, so
// every path is checked against these roots before it is read, served or
// deleted.
type ManagedFileRoots interface {
	ManagedFileRoots() []string
}

// ManagedFileRoots lists the directories this store writes files into.
func (s *sqlStore) ManagedFileRoots() []string {
	return []string{s.audioDir, s.snapshotDir}
}

// ContainedFile resolves a store-supplied path (symlinks followed) and
// returns it only when it lies inside one of st's managed directories. A
// store that does not report its directories owns no files.
func ContainedFile(st any, path string) (string, error) {
	roots, _ := st.(ManagedFileRoots)
	if roots == nil {
		return "", safepath.ErrNoAllowedRoots
	}
	return safepath.Contained(path, roots.ManagedFileRoots()...)
}

// removeManagedFile deletes a store-supplied path only when it is one of
// this store's own files. A file that is already gone is not an error.
func (s *sqlStore) removeManagedFile(path string) error {
	resolved, err := safepath.Contained(path, s.ManagedFileRoots()...)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(resolved); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
