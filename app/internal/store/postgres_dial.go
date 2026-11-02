package store

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func openPostgres(dsn string) (*sql.DB, error) {
	// The migration CLI intentionally does not load a serving configuration.
	// Bind its immutable ceiling too, before pgx can open a connection.
	if err := netsec.BindBootProfile(); err != nil {
		return nil, err
	}
	if !netsec.FullLocal() {
		return sql.Open("pgx", dsn)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// Keep pgx's parsed TLS, SNI and fallback settings. Resolve only inside
	// the guarded actual dial, rather than leaking a preliminary DNS query.
	config.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil }
	config.DialFunc = netsec.DialContext
	return stdlib.OpenDB(*config), nil
}
