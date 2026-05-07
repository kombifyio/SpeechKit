package store

import (
	"fmt"
	"strings"
)

// SECURITY INVARIANT — read before adding a new WHERE clause anywhere
// in this package.
//
// Every element appended to a `clauses` slice MUST be either a
// hard-coded SQL fragment OR a fragment templated with positional
// parameter indices ($1, $2, ?). User-supplied values MUST go into
// the `args` slice and be referenced via the placeholder. Never
// embed user data directly into a clause string.
//
// All WHERE construction goes through buildWhereClause below, which
// is the single audited choke point. Reviewers: any new clause
// string in this package needs to be a literal you can grep for;
// dynamic clause strings whose CONTENTS depend on user input are a
// SQL-injection bug.

// buildWhereClause concatenates pre-vetted internal clause snippets
// into a SQL "WHERE x AND y AND z" tail. Returns the empty string
// when clauses is empty so callers can append unconditionally.
//
// This is the only place in the store package that runs strings.Join
// on a clauses slice. All gosec G202 scrutiny is centralised here.
func buildWhereClause(clauses []string) string {
	if len(clauses) == 0 {
		return ""
	}
	// #nosec G202 -- per the SECURITY INVARIANT above, every clause is
	// a hard-coded internal snippet; user values are parameterized via
	// the args slice. This is the only strings.Join call in the package.
	return " WHERE " + strings.Join(clauses, " AND ")
}

func appendSQLiteNormalizedLanguageFilter(clauses []string, args []any, language string) ([]string, []any) {
	language = normalizeDictionaryLanguage(language)
	if language == "" {
		return clauses, args
	}
	clauses = append(clauses, `(lower(language) = ? OR lower(CASE
		WHEN instr(language, '-') > 0 THEN substr(language, 1, instr(language, '-') - 1)
		WHEN instr(language, '_') > 0 THEN substr(language, 1, instr(language, '_') - 1)
		ELSE language
	END) = ?)`)
	args = append(args, language, language)
	return clauses, args
}

func appendSQLiteOwnerFilter(clauses []string, args []any, opts ListOpts) ([]string, []any) {
	if opts.IncludeAllOwners {
		return clauses, args
	}
	userID := strings.TrimSpace(opts.OwnerUserID)
	orgID := strings.TrimSpace(opts.OwnerOrgID)
	if userID == "" && orgID == "" {
		return clauses, args
	}
	if opts.IncludeOwnerless {
		clauses = append(clauses, `((COALESCE(owner_user_id, '') = ? AND COALESCE(owner_org_id, '') = ?) OR (COALESCE(owner_user_id, '') = '' AND COALESCE(owner_org_id, '') = ''))`)
	} else {
		clauses = append(clauses, `(COALESCE(owner_user_id, '') = ? AND COALESCE(owner_org_id, '') = ?)`)
	}
	args = append(args, userID, orgID)
	return clauses, args
}

func appendPostgresNormalizedLanguageFilter(clauses []string, args []any, language string) ([]string, []any) {
	language = normalizeDictionaryLanguage(language)
	if language == "" {
		return clauses, args
	}
	args = append(args, language)
	param := len(args)
	clauses = append(clauses, fmt.Sprintf(`(lower(language) = $%[1]d OR lower(split_part(replace(language, '_', '-'), '-', 1)) = $%[1]d)`, param))
	return clauses, args
}

func appendPostgresOwnerFilter(clauses []string, args []any, opts ListOpts) ([]string, []any) {
	if opts.IncludeAllOwners {
		return clauses, args
	}
	userID := strings.TrimSpace(opts.OwnerUserID)
	orgID := strings.TrimSpace(opts.OwnerOrgID)
	if userID == "" && orgID == "" {
		return clauses, args
	}
	args = append(args, userID)
	userParam := len(args)
	args = append(args, orgID)
	orgParam := len(args)
	if opts.IncludeOwnerless {
		clauses = append(clauses, fmt.Sprintf(`((COALESCE(owner_user_id, '') = $%d AND COALESCE(owner_org_id, '') = $%d) OR (COALESCE(owner_user_id, '') = '' AND COALESCE(owner_org_id, '') = ''))`, userParam, orgParam))
	} else {
		clauses = append(clauses, fmt.Sprintf(`(COALESCE(owner_user_id, '') = $%d AND COALESCE(owner_org_id, '') = $%d)`, userParam, orgParam))
	}
	return clauses, args
}
