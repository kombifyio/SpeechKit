package claimstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite" // Register the repository's CGo-free SQLite driver.
)

// Open opens or creates the durable claim ledger. In-memory databases and
// non-regular paths are rejected because they cannot provide crash recovery.
func Open(ctx context.Context, options Options) (*Ledger, error) {
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	absPath, err := prepareDatabaseFile(normalized.Path)
	if err != nil {
		return nil, err
	}
	normalized.Path = absPath

	db, err := sql.Open("sqlite", sqliteDSN(absPath))
	if err != nil {
		return nil, fmt.Errorf("open claim database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping claim database: %w", err)
	}
	if err := migrateAndValidate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Ledger{db: db, options: normalized}, nil
}

func normalizeOptions(options Options) (Options, error) {
	options.Path = strings.TrimSpace(options.Path)
	if options.Path == "" || options.Path == ":memory:" || strings.Contains(strings.ToLower(options.Path), "mode=memory") {
		return Options{}, fmt.Errorf("%w: a durable filesystem path is required", ErrInvalidOptions)
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = defaultMaxEntries
	}
	if options.Retention == 0 {
		options.Retention = defaultRetention
	}
	if options.MaxRequestAge == 0 {
		options.MaxRequestAge = defaultMaxRequestAge
	}
	if options.FutureSkew == 0 {
		options.FutureSkew = defaultFutureSkew
	}
	if options.CleanupBatch == 0 {
		options.CleanupBatch = defaultCleanupBatch
	}
	if options.MaxEntries < 1 || options.Retention <= 0 || options.MaxRequestAge <= 0 || options.FutureSkew < 0 || options.CleanupBatch < 1 {
		return Options{}, ErrInvalidOptions
	}
	if options.Retention <= options.MaxRequestAge+options.FutureSkew {
		return Options{}, ErrUnsafeRetention
	}
	return options, nil
}

func prepareDatabaseFile(path string) (string, error) {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve claim database path: %w", err)
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create claim database directory: %w", err)
	}
	info, err := os.Lstat(absPath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: database path must be a regular file", ErrInvalidOptions)
		}
	case errors.Is(err, os.ErrNotExist):
		// The caller-selected state path was made absolute and rejected above
		// when it already named a symlink or non-regular file.
		file, openErr := os.OpenFile(absPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) //nolint:gosec // G304: validated local state path is the intended file input.
		if openErr != nil {
			return "", fmt.Errorf("create claim database file: %w", openErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", fmt.Errorf("close new claim database file: %w", closeErr)
		}
	default:
		return "", fmt.Errorf("inspect claim database file: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(absPath, 0o600); err != nil {
			return "", fmt.Errorf("restrict claim database permissions: %w", err)
		}
	}
	return absPath, nil
}

func sqliteDSN(path string) string {
	uriPath := filepath.ToSlash(path)
	// A Windows drive path must become file:///C:/...; without the leading
	// slash URL parsing treats "C:" as an authority and SQLite rejects it.
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := &url.URL{Scheme: "file", Path: uriPath}
	query := uri.Query()
	for _, pragma := range []string{
		"journal_mode(WAL)",
		"synchronous(FULL)",
		"busy_timeout(5000)",
		"foreign_keys(ON)",
		"secure_delete(FAST)",
		"journal_size_limit(4194304)",
		"wal_autocheckpoint(1000)",
	} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")
	query.Set("_dqs", "false")
	uri.RawQuery = query.Encode()
	return uri.String()
}

// Close releases the database. It does not remove the durable ledger.
func (l *Ledger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}
