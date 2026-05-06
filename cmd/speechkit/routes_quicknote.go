package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/textactions"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func registerQuickNoteRoutes(mux *http.ServeMux, cfg *config.Config, state *appState, feedbackStore store.Store) {
	routes := newQuickNoteRouteHandlers(cfg, state, feedbackStore)
	mux.HandleFunc("/quicknotes/create", routes.create)
	mux.HandleFunc("/quicknotes/update", routes.update)
	mux.HandleFunc("/quicknotes/delete", routes.delete)
	mux.HandleFunc("/quicknotes/pin", routes.pin)
	mux.HandleFunc("/quicknotes/summary", routes.summary)
	mux.HandleFunc("/quicknotes/email", routes.email)
	mux.HandleFunc("/quicknotes/get", routes.get)
	mux.HandleFunc("/quicknotes/record-mode", routes.recordMode)
	mux.HandleFunc("/quicknotes/open-editor", routes.openEditor)
	mux.HandleFunc("/quicknotes/open-capture", routes.openCapture)
	mux.HandleFunc("/quicknotes/close-capture", routes.closeCapture)
}

type quickNoteRouteHandlers struct {
	cfg           *config.Config
	state         *appState
	feedbackStore store.Store
	service       desktopQuickNoteService
}

func newQuickNoteRouteHandlers(cfg *config.Config, state *appState, feedbackStore store.Store) quickNoteRouteHandlers {
	return quickNoteRouteHandlers{
		cfg:           cfg,
		state:         state,
		feedbackStore: feedbackStore,
		service: desktopQuickNoteService{
			cfg:           cfg,
			state:         state,
			feedbackStore: feedbackStore,
			host:          wailsQuickNoteHost{state: state},
		},
	}
}

func (h quickNoteRouteHandlers) create(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) || !h.requireStore(w) {
		return
	}
	form, ok := h.parseLimitedForm(w, r)
	if !ok {
		return
	}
	text := strings.TrimSpace(form.Get("text"))
	if text == "" {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Text is required"})
		return
	}
	id, err := h.feedbackStore.SaveQuickNote(r.Context(), text, h.cfg.General.Language, "manual", 0, 0, nil)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Save failed: %v", err)})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "message": "Quick Note saved"})
}

func (h quickNoteRouteHandlers) update(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) || !h.requireStore(w) {
		return
	}
	form, ok := h.parseLimitedForm(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(form.Get("id"), 10, 64)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid ID"})
		return
	}
	text := strings.TrimSpace(form.Get("text"))
	if err := h.feedbackStore.UpdateQuickNote(r.Context(), id, text); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Update failed: %v", err)})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func (h quickNoteRouteHandlers) delete(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) || !h.requireStore(w) {
		return
	}
	form, ok := h.parseLimitedForm(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(form.Get("id"), 10, 64)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid ID"})
		return
	}
	if err := h.feedbackStore.DeleteQuickNote(r.Context(), id); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Delete failed: %v", err)})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

func (h quickNoteRouteHandlers) pin(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) || !h.requireStore(w) {
		return
	}
	form, ok := h.parseLimitedForm(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(form.Get("id"), 10, 64)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid ID"})
		return
	}
	if err := h.feedbackStore.PinQuickNote(r.Context(), id, form.Get("pinned") == "1"); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Pin failed: %v", err)})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func (h quickNoteRouteHandlers) summary(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) {
		return
	}
	note, err := resolveQuickNoteFromRequest(r, h.feedbackStore)
	if err != nil {
		writeQuickNoteError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"summary": summarizeQuickNote(r.Context(), h.state, note.Text, note.Language),
	})
}

func (h quickNoteRouteHandlers) email(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) {
		return
	}
	note, err := resolveQuickNoteFromRequest(r, h.feedbackStore)
	if err != nil {
		writeQuickNoteError(w, err)
		return
	}
	summary := summarizeQuickNote(r.Context(), h.state, note.Text, note.Language)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"email": draftQuickNoteEmail(note.Text, summary),
	})
}

func (h quickNoteRouteHandlers) get(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodGet) {
		return
	}
	if h.feedbackStore == nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
		return
	}
	n, err := h.feedbackStore.GetQuickNote(r.Context(), id)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id": n.ID, "text": n.Text, "language": n.Language,
		"provider": n.Provider, "durationMs": n.DurationMs, "audio": n.Audio,
		"createdAt": n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h quickNoteRouteHandlers) recordMode(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) {
		return
	}
	noteID, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err := dispatchQuickNoteCommand(r.Context(), h.state, speechkit.Command{
		Type:   speechkit.CommandArmQuickNoteRecording,
		NoteID: noteID,
	}); err != nil {
		h.service.ArmRecording(noteID)
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Quick Note recording armed"})
}

func (h quickNoteRouteHandlers) openEditor(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) {
		return
	}
	noteID := quickNoteIDFromQuery(r)
	err := dispatchQuickNoteCommand(r.Context(), h.state, speechkit.Command{
		Type:   speechkit.CommandOpenQuickNote,
		NoteID: noteID,
	})
	if errors.Is(err, speechkit.ErrCommandHandlerUnavailable) {
		err = h.service.OpenEditor(noteID) //nolint:contextcheck // OpenEditor is a Wails window operation that does not accept context.
	}
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Failed to open editor"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Editor opened"})
}

func (h quickNoteRouteHandlers) openCapture(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) {
		return
	}
	err := dispatchQuickNoteCommand(r.Context(), h.state, speechkit.Command{
		Type: speechkit.CommandOpenQuickCapture,
	})
	if errors.Is(err, speechkit.ErrCommandHandlerUnavailable) {
		_, err = h.service.OpenCapture(r.Context())
	}
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Failed to create note"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Capture opened"})
}

func (h quickNoteRouteHandlers) closeCapture(w http.ResponseWriter, r *http.Request) {
	if !h.beginJSON(w, r, http.MethodPost) {
		return
	}
	err := dispatchQuickNoteCommand(r.Context(), h.state, speechkit.Command{
		Type: speechkit.CommandCloseQuickCapture,
	})
	if errors.Is(err, speechkit.ErrCommandHandlerUnavailable) {
		err = h.service.CloseCapture() //nolint:contextcheck // CloseCapture is a Wails window operation that does not accept context.
	}
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Failed to close capture"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Closed"})
}

func (h quickNoteRouteHandlers) beginJSON(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return true
}

func (h quickNoteRouteHandlers) requireStore(w http.ResponseWriter) bool {
	if h.feedbackStore != nil {
		return true
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Store not available"})
	return false
}

func (h quickNoteRouteHandlers) parseLimitedForm(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": msgFormParseError})
		return nil, false
	}
	return r.Form, true
}

func quickNoteIDFromQuery(r *http.Request) int64 {
	idParam := r.URL.Query().Get("id")
	if idParam == "" {
		return 0
	}
	noteID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return 0
	}
	return noteID
}

// --- QuickNote request helpers ---

type quickNoteRequestError struct {
	message string
	status  int
}

func (e quickNoteRequestError) Error() string {
	return e.message
}

func resolveQuickNoteFromRequest(r *http.Request, feedbackStore store.Store) (*store.QuickNote, error) {
	if feedbackStore == nil {
		return nil, quickNoteRequestError{message: "Store not available", status: http.StatusServiceUnavailable}
	}
	if err := r.ParseForm(); err != nil {
		return nil, quickNoteRequestError{message: msgFormParseError, status: http.StatusBadRequest}
	}

	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || id <= 0 {
		return nil, quickNoteRequestError{message: "Invalid ID", status: http.StatusBadRequest}
	}

	note, err := feedbackStore.GetQuickNote(r.Context(), id)
	if err != nil {
		return nil, quickNoteRequestError{message: "Quick Note not found", status: http.StatusNotFound}
	}
	return note, nil
}

func writeQuickNoteError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Request failed"
	var reqErr quickNoteRequestError
	if errors.As(err, &reqErr) {
		status = reqErr.status
		message = reqErr.message
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func dispatchQuickNoteCommand(ctx context.Context, state *appState, command speechkit.Command) error {
	if state == nil || state.engine == nil {
		return speechkit.ErrCommandHandlerUnavailable
	}
	return state.engine.Commands().Dispatch(ctx, command)
}

// --- QuickNote text helpers ---

func summarizeQuickNote(ctx context.Context, state *appState, text, language string) string {
	input := textactions.ResolveSummarizeContext(textactions.SummarizeContext{
		Selection: text,
		Locale:    language,
	})

	if state != nil && state.summarizeFlow != nil {
		summary, err := (&textactions.FlowSummarizer{Flow: state.summarizeFlow}).Summarize(ctx, input)
		if err == nil {
			trimmed := strings.TrimSpace(summary)
			if trimmed != "" {
				return trimmed
			}
		} else {
			slog.Warn("quick note summarize fallback", "err", err)
		}
	}

	return fallbackQuickNoteSummary(input.Text)
}

func draftQuickNoteEmail(noteText, summary string) string {
	normalized := normalizeQuickNoteText(noteText)
	if summary == "" {
		summary = fallbackQuickNoteSummary(normalized)
	}
	highlights := quickNoteHighlights(normalized)
	lines := []string{
		fmt.Sprintf("Betreff: %s", quickNoteSubject(normalized)),
		"",
		"Hallo,",
		"",
		"hier ist die Zusammenfassung der Quick Note:",
		summary,
		"",
		"Wichtige Punkte:",
	}
	for _, highlight := range highlights {
		lines = append(lines, "- "+highlight)
	}
	lines = append(lines, "", "Viele Gruesse")
	return strings.Join(lines, "\n")
}

func fallbackQuickNoteSummary(text string) string {
	normalized := normalizeQuickNoteText(text)
	if normalized == "" {
		return ""
	}

	sentences := quickNoteHighlights(normalized)
	switch len(sentences) {
	case 0:
		return ""
	case 1:
		return sentences[0]
	default:
		return strings.Join(sentences[:minInt(2, len(sentences))], " ")
	}
}

func quickNoteSubject(text string) string {
	normalized := normalizeQuickNoteText(text)
	if normalized == "" {
		return "Quick Note Follow-up"
	}
	words := strings.Fields(normalized)
	if len(words) > 7 {
		words = words[:7]
	}
	subject := strings.Join(words, " ")
	if len(subject) > 64 {
		subject = truncateAtWord(subject, 64)
	}
	return subject
}

func quickNoteHighlights(text string) []string {
	normalized := normalizeQuickNoteText(text)
	if normalized == "" {
		return []string{"Keine Details verfuegbar"}
	}

	sentences := splitQuickNoteSentences(normalized)
	if len(sentences) == 0 {
		return []string{truncateAtWord(normalized, 160)}
	}

	highlights := make([]string, 0, minInt(3, len(sentences)))
	for _, sentence := range sentences {
		sentence = truncateAtWord(sentence, 160)
		if sentence == "" {
			continue
		}
		highlights = append(highlights, sentence)
		if len(highlights) == 3 {
			break
		}
	}
	if len(highlights) == 0 {
		return []string{truncateAtWord(normalized, 160)}
	}
	return highlights
}

func splitQuickNoteSentences(text string) []string {
	replacer := strings.NewReplacer("!", ".", "?", ".", "\n", ".")
	parts := strings.Split(replacer.Replace(text), ".")
	sentences := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := normalizeQuickNoteText(part)
		if normalized == "" {
			continue
		}
		sentences = append(sentences, normalized)
	}
	return sentences
}

func normalizeQuickNoteText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func truncateAtWord(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	truncated := text[:maxLen]
	if idx := strings.LastIndex(truncated, " "); idx >= maxLen/2 {
		truncated = truncated[:idx]
	}
	return strings.TrimSpace(truncated) + "..."
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
