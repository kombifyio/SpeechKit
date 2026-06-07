package store

import "strings"

// Filter helpers append `?`-placeholder WHERE clauses shared by both backends.
// The caller rebinds the assembled query to the backend's placeholder style
// (sqlDialect.rebind), so these helpers stay dialect-neutral.

func appendNormalizedLanguageFilter(clauses []string, args []any, language string) ([]string, []any) {
	language = normalizeDictionaryLanguage(language)
	if language == "" {
		return clauses, args
	}
	clauses = append(clauses, "language_base = ?")
	args = append(args, language)
	return clauses, args
}

func appendOwnerFilter(clauses []string, args []any, opts ListOpts) ([]string, []any) {
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
