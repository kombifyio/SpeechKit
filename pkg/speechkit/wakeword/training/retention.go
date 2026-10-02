package training

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// captureNamePattern matches the files Capture writes
// (<2006-01-02T15-04-05.000Z>_<phrase>_<score>.wav|.json). Pruning touches
// only these, so a capture dir pointed at a shared folder never loses
// unrelated files.
var captureNamePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}Z_[^/\\]+_-?\d+\.\d{2}\.(?:wav|json)$`)

// PruneCaptureDir enforces the local retention limits on a capture
// directory: activation pairs (WAV + JSON sidecar) older than maxAge are
// deleted, and when more than maxFiles pairs remain the oldest are deleted
// until maxFiles are left. A zero maxAge or maxFiles disables that limit.
// A pair's age is its oldest file's modification time, so rewriting the
// sidecar (e.g. marking it uploaded) never extends retention. It returns
// the number of pairs removed; the first deletion error is returned after
// the sweep completes.
func PruneCaptureDir(dir string, maxAge time.Duration, maxFiles int, now time.Time) (int, error) {
	if strings.TrimSpace(dir) == "" || (maxAge <= 0 && maxFiles <= 0) {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	type pair struct {
		files  []string
		oldest time.Time
	}
	pairs := map[string]*pair{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !captureNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		p := pairs[key]
		if p == nil {
			p = &pair{oldest: info.ModTime()}
			pairs[key] = p
		}
		p.files = append(p.files, filepath.Join(dir, e.Name()))
		if info.ModTime().Before(p.oldest) {
			p.oldest = info.ModTime()
		}
	}
	ordered := make([]*pair, 0, len(pairs))
	for _, p := range pairs {
		ordered = append(ordered, p)
	}
	// Newest first, so everything past maxFiles is the oldest surplus.
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].oldest.After(ordered[j].oldest) })

	removed := 0
	var firstErr error
	for i, p := range ordered {
		expired := maxAge > 0 && now.Sub(p.oldest) > maxAge
		surplus := maxFiles > 0 && i >= maxFiles
		if !expired && !surplus {
			continue
		}
		for _, f := range p.files {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
		}
		removed++
	}
	return removed, firstErr
}

// EraseCaptureDir deletes every activation clip and sidecar in dir, for a
// privacy erasure. Like PruneCaptureDir it touches only files named the way
// Capture names them. It returns the number of files removed; the first
// deletion error is returned after the sweep completes.
func EraseCaptureDir(dir string) (int, error) {
	if strings.TrimSpace(dir) == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	var firstErr error
	for _, e := range entries {
		if !e.Type().IsRegular() || !captureNamePattern.MatchString(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
