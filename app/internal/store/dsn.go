package store

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// RedactedDSNPassword is what RedactPostgresDSN puts in place of a password.
// A DSN posted back with it unchanged means "keep the stored password".
const RedactedDSNPassword = "xxxxx"

var keywordPasswordPattern = regexp.MustCompile(`(?i)(\bpassword\s*=\s*)('(?:[^'\\]|\\.)*'|\S+)`)

// RedactPostgresDSN returns dsn with its password replaced, for display in
// the settings UI, and whether there was a password. Both the URL form
// (postgres://user:pass@host/db?password=...) and the keyword form
// (host=... password=...) are handled; a URL that does not parse is not
// echoed at all.
func RedactPostgresDSN(dsn string) (string, bool) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", false
	}
	lower := strings.ToLower(dsn)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", true
		}
		hasPassword := false
		if u.User != nil {
			if _, ok := u.User.Password(); ok {
				hasPassword = true
				u.User = url.UserPassword(u.User.Username(), RedactedDSNPassword)
			}
		}
		query := u.Query()
		if query.Has("password") {
			hasPassword = true
			query.Set("password", RedactedDSNPassword)
			u.RawQuery = query.Encode()
		}
		return u.String(), hasPassword
	}
	hasPassword := keywordPasswordPattern.MatchString(dsn)
	return keywordPasswordPattern.ReplaceAllString(dsn, "${1}"+RedactedDSNPassword), hasPassword
}

// PostgresDSNHosts returns every host a DSN would connect to (the primary
// and any fallbacks; a Unix socket directory starts with "/"), and the
// password it carries.
func PostgresDSNHosts(dsn string) (hosts []string, password string, err error) {
	cfg, err := pgconn.ParseConfig(strings.TrimSpace(dsn))
	if err != nil {
		return nil, "", err
	}
	hosts = append(hosts, cfg.Host)
	for _, fallback := range cfg.Fallbacks {
		if fallback != nil {
			hosts = append(hosts, fallback.Host)
		}
	}
	return hosts, cfg.Password, nil
}
