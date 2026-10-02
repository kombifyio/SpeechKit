package store

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// searchTerms splits a query into lower-cased terms; one-character terms are
// noise and dropped. At most eight terms keep the SQL bounded.
func searchTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	})
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, `"'`)
		if utf8.RuneCountInString(field) < 2 {
			continue
		}
		terms = append(terms, field)
		if len(terms) == 8 {
			break
		}
	}
	return terms
}

// likePattern escapes the LIKE wildcards in a term and wraps it for a
// substring match.
func likePattern(term string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
	return "%" + escaped + "%"
}

// SearchRecordingSessions finds the sessions of the caller's scope in which
// every term of the query appears in the title, a final transcript segment,
// the user's notes or a ready write-up. It is a plain LIKE search on the
// existing tables — no index to migrate, fast enough for the few hundred
// meetings a desktop holds — and the foundation a semantic (embedding) search
// can later rank on top of.
func (s *sqlStore) SearchRecordingSessions(ctx context.Context, query string, opts ListOpts) ([]RecordingSessionSearchHit, error) {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return []RecordingSessionSearchHit{}, nil
	}
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	sql := recordingSessionSelectSQL + ` WHERE scope_id = ?`
	args := []any{scopeID}
	if kind := strings.ToLower(strings.TrimSpace(opts.Kind)); kind != "" {
		sql += ` AND kind = ?`
		args = append(args, kind)
	}
	for _, term := range terms {
		pattern := likePattern(term)
		sql += ` AND (lower(title) LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM recording_session_segments seg WHERE seg.session_id = recording_sessions.id AND seg.is_final = 1 AND lower(seg.text) LIKE ? ESCAPE '\')
			OR EXISTS (SELECT 1 FROM recording_session_notes n WHERE n.session_id = recording_sessions.id AND lower(n.content_md) LIKE ? ESCAPE '\')
			OR EXISTS (SELECT 1 FROM recording_session_enhancements e WHERE e.session_id = recording_sessions.id AND e.status = 'ready' AND lower(e.content_md) LIKE ? ESCAPE '\'))`
		args = append(args, pattern, pattern, pattern, pattern)
	}
	sql += ` ORDER BY created_at DESC, id DESC`
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	sql += ` LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(sql), args...)
	if err != nil {
		return nil, fmt.Errorf("search recording sessions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable.

	hits := make([]RecordingSessionSearchHit, 0)
	var sessions []RecordingSession
	for rows.Next() {
		session, err := scanRecordingSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, session := range sessions {
		source, snippet := s.searchSnippet(ctx, session, terms[0])
		hits = append(hits, RecordingSessionSearchHit{Session: session, Source: source, Snippet: snippet})
	}
	return hits, nil
}

// searchSnippet finds the first place the leading term appears in a session
// and returns a short excerpt around it.
func (s *sqlStore) searchSnippet(ctx context.Context, session RecordingSession, term string) (string, string) {
	if strings.Contains(strings.ToLower(session.Title), term) {
		return "title", excerptAround(session.Title, term)
	}
	pattern := likePattern(term)
	var text string
	if err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT text FROM recording_session_segments WHERE session_id = ? AND is_final = 1 AND lower(text) LIKE ? ESCAPE '\' ORDER BY segment_index LIMIT 1`),
		session.ID, pattern).Scan(&text); err == nil && text != "" {
		return "transcript", excerptAround(text, term)
	}
	if err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT content_md FROM recording_session_notes WHERE session_id = ? AND lower(content_md) LIKE ? ESCAPE '\' LIMIT 1`),
		session.ID, pattern).Scan(&text); err == nil && text != "" {
		return "notes", excerptAround(text, term)
	}
	if err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT content_md FROM recording_session_enhancements WHERE session_id = ? AND status = 'ready' AND lower(content_md) LIKE ? ESCAPE '\' ORDER BY created_at DESC, id DESC LIMIT 1`),
		session.ID, pattern).Scan(&text); err == nil && text != "" {
		return "write-up", excerptAround(text, term)
	}
	return "", ""
}

// excerptAround returns about 140 runes of text centred on the first match of
// term (case-insensitive), with ellipses where it was cut.
func excerptAround(text, term string) string {
	const window = 70
	flat := strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(flat)
	index := strings.Index(lower, term)
	if index < 0 {
		index = 0
	}
	runes := []rune(flat)
	// byte index -> rune index
	runeIndex := utf8.RuneCountInString(flat[:index])
	start := runeIndex - window
	if start < 0 {
		start = 0
	}
	end := runeIndex + utf8.RuneCountInString(term) + window
	if end > len(runes) {
		end = len(runes)
	}
	excerpt := strings.TrimSpace(string(runes[start:end]))
	if start > 0 {
		excerpt = "…" + excerpt
	}
	if end < len(runes) {
		excerpt += "…"
	}
	return excerpt
}
