package store

import (
	"context"
	"time"
)

// RecordingSessionStore is an optional extension for long-running dictation
// and meeting capture sessions. Segment rows can link to ordinary
// transcriptions, so the existing dashboard/library records stay reusable.
type RecordingSessionStore interface {
	SaveRecordingSession(ctx context.Context, session RecordingSession) (int64, error)
	ListRecordingSessions(ctx context.Context, opts ListOpts) ([]RecordingSession, error)
	AppendRecordingSessionSegment(ctx context.Context, sessionID int64, segment RecordingSessionSegment) (int64, error)
	UpdateRecordingSessionSummary(ctx context.Context, id int64, summary string) error
	UpdateRecordingSessionCaptureStatus(ctx context.Context, id int64, status RecordingSessionCaptureStatus, at time.Time) error
	UpdateRecordingSessionSummaryStatus(ctx context.Context, id int64, status RecordingSessionSummaryStatus, message string, at time.Time) error
	FinishRecordingSession(ctx context.Context, id int64, summary string, endedAt time.Time) error
	GetRecordingSession(ctx context.Context, id int64) (*RecordingSession, error)
	DeleteRecordingSession(ctx context.Context, id int64) error
	// SetRecordingSessionPinned keeps one meeting out of the retention sweep.
	SetRecordingSessionPinned(ctx context.Context, id int64, pinned bool) error
}

// RecordingSessionSearchStore is an optional extension: a text search over a
// scope's recording sessions — title, transcript, the user's notes and the
// finished write-ups. Every term of the query has to appear somewhere in a
// session for it to match.
type RecordingSessionSearchStore interface {
	SearchRecordingSessions(ctx context.Context, query string, opts ListOpts) ([]RecordingSessionSearchHit, error)
}

// RecordingSessionSearchHit is one matching session with the first place the
// query was found, so a list can show why the meeting is in the results.
type RecordingSessionSearchHit struct {
	Session RecordingSession `json:"session"`
	// Source names where the snippet comes from: "title", "transcript",
	// "notes" or "write-up".
	Source  string `json:"source"`
	Snippet string `json:"snippet"`
}

// RecordingSessionNotesStore is an optional extension for backends that
// persist the notes a user writes during a meeting. They are kept apart from
// the transcript and from anything a model generates, because the enhancement
// treats them as anchors and reproduces them verbatim.
type RecordingSessionNotesStore interface {
	SaveRecordingSessionNotes(ctx context.Context, sessionID int64, notes RecordingSessionNotes) error
	GetRecordingSessionNotes(ctx context.Context, sessionID int64) (*RecordingSessionNotes, error)
}

// RecordingSessionNotes is one meeting's hand-written notes.
type RecordingSessionNotes struct {
	SessionID int64 `json:"sessionId"`
	// ContentMD is the note pane as the user last left it.
	ContentMD string `json:"contentMd"`
	// Blocks splits that text into the individual notes, each stamped with the
	// point in the meeting it was written at. The enhancement uses those
	// timestamps to find the part of the conversation a note was about.
	Blocks    []RecordingSessionNoteBlock `json:"blocks"`
	CreatedAt time.Time                   `json:"createdAt,omitempty"`
	UpdatedAt time.Time                   `json:"updatedAt,omitempty"`
}

// RecordingSessionNoteBlock is a single note the user typed.
type RecordingSessionNoteBlock struct {
	// ID is stable for the lifetime of the note so an enhanced bullet can
	// point back at the note it came from.
	ID   string `json:"id"`
	Text string `json:"text"`
	// TsMs is when the note was written, relative to the meeting's start.
	TsMs int64 `json:"tsMs"`
}

// RecordingSessionSnapshotStore is an optional extension for backends that
// persist ad-hoc screen captures taken during a meeting. The store owns the
// image files: saving writes the bytes under its snapshot directory, deleting
// a snapshot or its session removes them again.
type RecordingSessionSnapshotStore interface {
	SaveRecordingSessionSnapshot(ctx context.Context, sessionID int64, input RecordingSessionSnapshotInput) (*RecordingSessionSnapshot, error)
	GetRecordingSessionSnapshot(ctx context.Context, id int64) (*RecordingSessionSnapshot, error)
	ListRecordingSessionSnapshots(ctx context.Context, sessionID int64) ([]RecordingSessionSnapshot, error)
	DeleteRecordingSessionSnapshot(ctx context.Context, id int64) error
}

// RecordingSessionSnapshotInput carries a freshly captured screenshot into the
// store.
type RecordingSessionSnapshotInput struct {
	// CapturedMs is the offset on the meeting's transcript timeline:
	// wall-clock milliseconds since the capture epoch, the same time base
	// segment StartedMs/EndedMs are stamped with (see Runtime.ElapsedMs).
	CapturedMs int64
	// Data is the encoded image; MimeType defaults to image/png.
	Data     []byte
	MimeType string
	Width    int
	Height   int
	Monitor  string
	Note     string
}

// RecordingSessionSnapshot is one stored screen capture of a meeting.
type RecordingSessionSnapshot struct {
	ID         int64 `json:"id"`
	SessionID  int64 `json:"sessionId"`
	CapturedMs int64 `json:"capturedMs"`
	// Path is the absolute local file path; images are served by ID, so the
	// path stays out of API payloads.
	Path      string `json:"-"`
	MimeType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Monitor   string `json:"monitor,omitempty"`
	Note      string `json:"note,omitempty"`
	// Description is filled by the optional vision enrichment (V2).
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RecordingSessionEnhancementStore is an optional extension for backends that
// persist written-up meeting notes. A meeting can have several: writing it up
// again with a different template produces a new one rather than replacing the
// one the user may still prefer.
type RecordingSessionEnhancementStore interface {
	CreateRecordingSessionEnhancement(ctx context.Context, sessionID int64, enhancement RecordingSessionEnhancement) (int64, error)
	UpdateRecordingSessionEnhancement(ctx context.Context, id int64, enhancement RecordingSessionEnhancement) error
	ListRecordingSessionEnhancements(ctx context.Context, sessionID int64) ([]RecordingSessionEnhancement, error)
}

type RecordingSessionEnhancementStatus string

const (
	RecordingSessionEnhancementIdle      RecordingSessionEnhancementStatus = "idle"
	RecordingSessionEnhancementPending   RecordingSessionEnhancementStatus = "pending"
	RecordingSessionEnhancementRunning   RecordingSessionEnhancementStatus = "running"
	RecordingSessionEnhancementPartial   RecordingSessionEnhancementStatus = "partial"
	RecordingSessionEnhancementReady     RecordingSessionEnhancementStatus = "ready"
	RecordingSessionEnhancementFailed    RecordingSessionEnhancementStatus = "failed"
	RecordingSessionEnhancementCancelled RecordingSessionEnhancementStatus = "cancelled"
)

// RecordingSessionEnhancement is one written-up version of a meeting.
type RecordingSessionEnhancement struct {
	ID           int64  `json:"id"`
	SessionID    int64  `json:"sessionId"`
	TemplateSlug string `json:"templateSlug"`
	// TemplateSnapshot is the template as it was when this write-up ran, so an
	// old result stays explicable after the template's wording changes.
	TemplateSnapshot string                            `json:"templateSnapshot,omitempty"`
	Status           RecordingSessionEnhancementStatus `json:"status"`
	Error            string                            `json:"error,omitempty"`
	Provider         string                            `json:"provider,omitempty"`
	Model            string                            `json:"model,omitempty"`
	Stage            string                            `json:"stage,omitempty"`
	Progress         int                               `json:"progress"`
	Attempt          int                               `json:"attempt"`
	InputFingerprint string                            `json:"inputFingerprint,omitempty"`
	ErrorKind        string                            `json:"errorKind,omitempty"`
	Retryable        bool                              `json:"retryable"`
	ConsentVersion   int                               `json:"consentVersion,omitempty"`
	// Structured is false when the model could not produce citable structure
	// and the notes are prose. Callers surface that rather than implying the
	// bullets can be traced back to the transcript.
	Structured bool `json:"structured"`
	// ContentJSON is the structured document; ContentMD is it rendered for
	// reading, copying and export.
	ContentJSON string    `json:"contentJson,omitempty"`
	ContentMD   string    `json:"contentMd,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type MeetingSummaryBatchStatus string

const (
	MeetingSummaryBatchSealed      MeetingSummaryBatchStatus = "sealed"
	MeetingSummaryBatchQueued      MeetingSummaryBatchStatus = "queued"
	MeetingSummaryBatchSummarizing MeetingSummaryBatchStatus = "summarizing"
	MeetingSummaryBatchReady       MeetingSummaryBatchStatus = "ready"
	MeetingSummaryBatchDelayed     MeetingSummaryBatchStatus = "delayed"
	MeetingSummaryBatchFailed      MeetingSummaryBatchStatus = "failed"
	MeetingSummaryBatchSuperseded  MeetingSummaryBatchStatus = "superseded"
)

// MeetingSummaryBatch is transcript-derived and follows its meeting's deletion
// and retention lifecycle through the database foreign key.
type MeetingSummaryBatch struct {
	ID                int64                     `json:"id"`
	SessionID         int64                     `json:"sessionId"`
	BatchKey          string                    `json:"batchKey"`
	Level             int                       `json:"level"`
	StartSegmentID    int64                     `json:"startSegmentId"`
	EndSegmentID      int64                     `json:"endSegmentId"`
	SourceFingerprint string                    `json:"sourceFingerprint"`
	Status            MeetingSummaryBatchStatus `json:"status"`
	DigestJSON        string                    `json:"digestJson,omitempty"`
	Provider          string                    `json:"provider,omitempty"`
	Model             string                    `json:"model,omitempty"`
	ErrorKind         string                    `json:"errorKind,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
}

type MeetingSummaryBatchStore interface {
	UpsertMeetingSummaryBatch(ctx context.Context, batch MeetingSummaryBatch) (MeetingSummaryBatch, error)
	ListMeetingSummaryBatches(ctx context.Context, sessionID int64) ([]MeetingSummaryBatch, error)
}

// RecordingSessionImportKind names what is imported into a meeting from an
// outside service.
type RecordingSessionImportKind string

const (
	// RecordingSessionImportTeamsTranscript is the transcript Microsoft Teams
	// produced for the meeting. Its cues become segments of the session on
	// the RecordingSegmentChannelTeams channel.
	RecordingSessionImportTeamsTranscript RecordingSessionImportKind = "teams_transcript"
	// RecordingSessionImportCopilotRecap is the meeting notes and action
	// items Microsoft 365 Copilot wrote for the meeting, kept as JSON in
	// ContentJSON next to SpeechKit's own review.
	RecordingSessionImportCopilotRecap RecordingSessionImportKind = "copilot_recap"
)

// RecordingSessionImportStatus is where one import stands.
type RecordingSessionImportStatus string

const (
	// RecordingSessionImportWaiting: scheduled for NextAttemptAt.
	RecordingSessionImportWaiting RecordingSessionImportStatus = "waiting"
	// RecordingSessionImportImported: done.
	RecordingSessionImportImported RecordingSessionImportStatus = "imported"
	// RecordingSessionImportUnavailable: the service has nothing to import
	// or refuses access; ErrorKind says why. Final.
	RecordingSessionImportUnavailable RecordingSessionImportStatus = "unavailable"
	// RecordingSessionImportFailed: an error retrying will not fix. Final.
	RecordingSessionImportFailed RecordingSessionImportStatus = "failed"
	// RecordingSessionImportCancelled: the user withdrew the permission or
	// turned the import off before it finished. Final.
	RecordingSessionImportCancelled RecordingSessionImportStatus = "cancelled"
)

// RecordingSegmentChannelTeams marks segments imported from a Microsoft Teams
// transcript. Their Speaker is the name Teams attributed the words to.
const RecordingSegmentChannelTeams = "teams"

// RecordingSessionImport tracks one import for one meeting. Content that
// becomes segments is not repeated here; ContentJSON holds only what has no
// other home.
type RecordingSessionImport struct {
	ID                int64                        `json:"id"`
	SessionID         int64                        `json:"sessionId"`
	Kind              RecordingSessionImportKind   `json:"kind"`
	Status            RecordingSessionImportStatus `json:"status"`
	Attempts          int                          `json:"attempts"`
	NextAttemptAt     time.Time                    `json:"nextAttemptAt,omitempty"`
	DeadlineAt        time.Time                    `json:"deadlineAt,omitempty"`
	ExternalMeetingID string                       `json:"externalMeetingId,omitempty"`
	ExternalItemID    string                       `json:"externalItemId,omitempty"`
	Subject           string                       `json:"subject,omitempty"`
	ContentJSON       string                       `json:"contentJson,omitempty"`
	ErrorKind         string                       `json:"errorKind,omitempty"`
	CreatedAt         time.Time                    `json:"createdAt"`
	UpdatedAt         time.Time                    `json:"updatedAt"`
}

// RecordingSessionImportStore is an optional extension for backends that
// persist imports into meetings. One row exists per session and kind.
type RecordingSessionImportStore interface {
	UpsertRecordingSessionImport(ctx context.Context, item RecordingSessionImport) (RecordingSessionImport, error)
	ListRecordingSessionImports(ctx context.Context, sessionID int64) ([]RecordingSessionImport, error)
	// ListDueRecordingSessionImports returns waiting imports whose next
	// attempt is due at now, oldest first, in the caller's scope.
	ListDueRecordingSessionImports(ctx context.Context, now time.Time, limit int) ([]RecordingSessionImport, error)
}

type RecordingSessionKind string

const (
	RecordingSessionKindDictation RecordingSessionKind = "dictation"
	RecordingSessionKindMeeting   RecordingSessionKind = "meeting"
)

type RecordingSessionStatus string

const (
	RecordingSessionStatusActive   RecordingSessionStatus = "active"
	RecordingSessionStatusFinished RecordingSessionStatus = "finished"
	RecordingSessionStatusFailed   RecordingSessionStatus = "failed"
)

type RecordingSessionCaptureStatus string

const (
	RecordingSessionCaptureIdle      RecordingSessionCaptureStatus = "idle"
	RecordingSessionCaptureRecording RecordingSessionCaptureStatus = "recording"
	RecordingSessionCapturePaused    RecordingSessionCaptureStatus = "paused"
	RecordingSessionCaptureStopped   RecordingSessionCaptureStatus = "stopped"
)

type RecordingSessionSummaryStatus string

const (
	RecordingSessionSummaryIdle    RecordingSessionSummaryStatus = "idle"
	RecordingSessionSummaryRunning RecordingSessionSummaryStatus = "running"
	RecordingSessionSummaryReady   RecordingSessionSummaryStatus = "ready"
	RecordingSessionSummaryFailed  RecordingSessionSummaryStatus = "failed"
)

type RecordingSession struct {
	ID               int64                         `json:"id"`
	ExternalID       string                        `json:"externalId,omitempty"`
	Kind             RecordingSessionKind          `json:"kind"`
	Status           RecordingSessionStatus        `json:"status"`
	CaptureStatus    RecordingSessionCaptureStatus `json:"captureStatus"`
	SummaryStatus    RecordingSessionSummaryStatus `json:"summaryStatus"`
	SummaryError     string                        `json:"summaryError,omitempty"`
	Title            string                        `json:"title,omitempty"`
	Language         string                        `json:"language"`
	Provider         string                        `json:"provider,omitempty"`
	Model            string                        `json:"model,omitempty"`
	InputSource      string                        `json:"inputSource,omitempty"`
	ProcessingMode   string                        `json:"processingMode,omitempty"`
	Summary          string                        `json:"summary,omitempty"`
	StartedAt        time.Time                     `json:"startedAt"`
	EndedAt          time.Time                     `json:"endedAt,omitempty"`
	CaptureStartedAt time.Time                     `json:"captureStartedAt,omitempty"`
	CapturePausedAt  time.Time                     `json:"capturePausedAt,omitempty"`
	CaptureStoppedAt time.Time                     `json:"captureStoppedAt,omitempty"`
	SummaryUpdatedAt time.Time                     `json:"summaryUpdatedAt,omitempty"`
	CreatedAt        time.Time                     `json:"createdAt"`
	UpdatedAt        time.Time                     `json:"updatedAt"`
	OwnerUserID      string                        `json:"ownerUserId,omitempty"`
	OwnerOrgID       string                        `json:"ownerOrgId,omitempty"`
	OwnerSource      string                        `json:"ownerSource,omitempty"`
	Segments         []RecordingSessionSegment     `json:"segments,omitempty"`
	// Notes are the user's own notes for this meeting. Only loaded where they
	// matter — the session detail and subject exports — not in list responses.
	Notes *RecordingSessionNotes `json:"notes,omitempty"`
	// Snapshots are the screen captures taken during this meeting. Loaded like
	// Notes only on the session detail, not in list responses.
	Snapshots []RecordingSessionSnapshot `json:"snapshots,omitempty"`
	// WriteUps are the model's written-up versions of this meeting and
	// SummaryBatches the rolling digests derived from its transcript. Both are
	// loaded only for subject exports, where everything derived from the
	// user's words belongs in the answer.
	WriteUps       []RecordingSessionEnhancement `json:"writeUps,omitempty"`
	SummaryBatches []MeetingSummaryBatch         `json:"summaryBatches,omitempty"`
	// Imports are what was brought in from outside services, such as a
	// Microsoft Teams transcript. Loaded, like WriteUps, for subject exports.
	Imports []RecordingSessionImport `json:"imports,omitempty"`
	// RetentionPinned keeps this meeting even once it is past the retention
	// window.
	RetentionPinned bool `json:"retentionPinned,omitempty"`
}

// Meeting capture records who a segment came from without acoustic
// diarization: the microphone channel is the local user, the system loopback
// channel is everyone else on the call. Acoustic diarization refines the
// loopback side into individual speakers later.
const (
	RecordingSegmentSpeakerMe     = "me"
	RecordingSegmentSpeakerOthers = "them"
)

type RecordingSessionSegment struct {
	ID        int64 `json:"id"`
	SessionID int64 `json:"sessionId"`
	// SegmentIndex orders segments within one session. Pass a negative value
	// to AppendRecordingSessionSegment to have the store allocate the next
	// free index, which is what concurrent capture channels need; an explicit
	// index upserts the row at that position (the segment-edit path).
	SegmentIndex    int    `json:"segmentIndex"`
	TranscriptionID int64  `json:"transcriptionId,omitempty"`
	ProviderItemID  string `json:"providerItemId,omitempty"`
	Text            string `json:"text"`
	IsFinal         bool   `json:"isFinal"`
	// Channel names the capture source this segment was transcribed from
	// (see speechkit.CaptureChannel*). Empty for single-source sessions.
	Channel string `json:"channel,omitempty"`
	// Speaker labels who spoke, derived from Channel today.
	Speaker   string    `json:"speaker,omitempty"`
	StartedMs int64     `json:"startedMs"`
	EndedMs   int64     `json:"endedMs"`
	CreatedAt time.Time `json:"createdAt"`
}
