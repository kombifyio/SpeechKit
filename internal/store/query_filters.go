package store

import (
	"fmt"
	"strings"
)

func appendSQLiteNormalizedLanguageFilter(clauses []string, args []any, language string) ([]string, []any) {
	language = normalizeDictionaryLanguage(language)
	if language == "" {
		return clauses, args
	}
	clauses = append(clauses, "language_base = ?")
	args = append(args, language)
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
	clauses = append(clauses, fmt.Sprintf("language_base = $%d", param))
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
