package config

// Server-Target (cmd/speechkit-server) configuration types: server core,
// OIDC, security headers, debug, LiveKit, features, training-data, and the
// TOML-seeded Persona/Role/Sequence Voice Agent definitions.

// ServerConfig configures the standalone Linux server binary. Used only by
// cmd/speechkit-server; the desktop app never reads these values.
type ServerConfig struct {
	ListenAddr        string   `toml:"listen_addr"`          // e.g. ":8080"
	PublicURL         string   `toml:"public_url"`           // external API base URL, e.g. https://speechkit.example.com/api
	Modes             []string `toml:"modes"`                // subset of ["dictation","assist","voiceagent"]; empty = all
	AuthMode          string   `toml:"auth_mode"`            // "none" | "bearer" | "edge_hmac" | "bearer_or_edge"
	BearerTokenEnv    string   `toml:"bearer_token_env"`     // env var name holding the bearer token
	BearerRole        string   `toml:"bearer_role"`          // optional role for static bearer callers, e.g. "admin"
	AdminAuthEnabled  bool     `toml:"admin_auth_enabled"`   // enables setup/admin UI username/password login
	AdminUsername     string   `toml:"admin_username"`       // setup/admin UI username; not used by API clients
	AdminPasswordHash string   `toml:"admin_password_hash"`  // bcrypt hash for setup/admin UI login
	EdgeAuthSecretEnv string   `toml:"edge_auth_secret_env"` // env var name holding the HMAC secret
	// SmokeTokenEnv names an optional env var that holds a public-friendly
	// demo bearer token. When set, the smoke UI on `/` embeds the token in
	// the rendered HTML so visitors can run all three modes without
	// configuring credentials. The smoke identity is tagged Source="smoke"
	// (Plan="demo") so handlers and rate-limiters can distinguish demo
	// traffic. Leave empty to disable smoke-from-page entirely; operators
	// must then paste their bearer token in the UI manually.
	SmokeTokenEnv      string   `toml:"smoke_token_env"`
	PublicBaseURL      string   `toml:"public_base_url"`     // public server URL used for returned client URLs
	TrustedProxyCIDRs  []string `toml:"trusted_proxy_cidrs"` // proxies allowed to supply X-Forwarded-* headers
	CORSAllowedOrigins []string `toml:"cors_allowed_origins"`
	RateLimitRPS       float64  `toml:"rate_limit_rps"`
	RateLimitBurst     int      `toml:"rate_limit_burst"`
	// RateLimitEndpointCosts assigns per-endpoint token costs so
	// expensive handlers (LLM, transcription, voice-agent session
	// create) drain the bucket faster than cheap ones. Keys are
	// either "METHOD PATH" (e.g. "POST /v1/dictation/transcribe")
	// or bare PATH. Missing entries default to 1.0. Audit S-4.
	RateLimitEndpointCosts map[string]float64 `toml:"rate_limit_endpoint_costs"`
	// DemoDailyQuota caps how many requests a Plan="demo" identity
	// (the smoke-token surface) may make per UTC day, keyed by
	// UserID + client IP. Zero disables the quota. Audit S-5.
	DemoDailyQuota         int `toml:"demo_daily_quota"`
	MaxUploadMB            int `toml:"max_upload_mb"`
	ReadHeaderTimeoutSec   int `toml:"read_header_timeout_sec"`
	ReadTimeoutSec         int `toml:"read_timeout_sec"`
	IdleTimeoutSec         int `toml:"idle_timeout_sec"`
	MaxHeaderBytes         int `toml:"max_header_bytes"`
	MaxDecodedAudioSeconds int `toml:"max_decoded_audio_seconds"`
	MaxVoiceAgentSessions  int `toml:"max_voiceagent_sessions"` // global cap
	MaxSessionsPerUser     int `toml:"max_sessions_per_user"`
	TicketTTLSec           int `toml:"ticket_ttl_sec"` // Voice Agent WS ticket TTL
	// VoiceAgentIdleTimeoutSec terminates a Voice Agent WebSocket session
	// after N seconds without any client- or provider-side activity.
	// Defaults to 900 (15 min). Set to 0 to disable the server-side idle
	// timeout (kernel-level idle handling stays in effect either way).
	VoiceAgentIdleTimeoutSec int `toml:"voiceagent_idle_timeout_sec"`
	// VoiceAgentMaxSessionSec hard-caps a single Voice Agent WebSocket
	// session. Zero disables the hard cap for normal self-hosted installs;
	// public beta deployments should set a finite budget because realtime
	// sessions are cost-correlated with wall-clock duration.
	VoiceAgentMaxSessionSec int `toml:"voiceagent_max_session_sec"`
	// WSReadLimitBytes caps the per-frame size the Voice Agent WebSocket
	// will read from a client. Zero or negative defaults to 64 KiB,
	// which leaves ample headroom over real PCM chunk sizes (well under
	// 4 KB) without giving a single frame a 1 MiB memory amplification
	// vector. Bumped only for non-standard payloads.
	WSReadLimitBytes int64                `toml:"ws_read_limit_bytes"`
	LiveKit          ServerLiveKitConfig  `toml:"livekit"`
	WhisperBinary    string               `toml:"whisper_binary"` // absolute path inside container
	WhisperPort      int                  `toml:"whisper_port"`   // loopback port for whisper.cpp server
	ModelDir         string               `toml:"model_dir"`      // persistent volume, e.g. /var/lib/speechkit/models
	LogFormat        string               `toml:"log_format"`     // "json" | "text"
	LogLevel         string               `toml:"log_level"`      // "debug" | "info" | "warn" | "error"
	Features         ServerFeaturesConfig `toml:"features"`

	// TrainingData configures the server-side wake-word activation
	// collection endpoint. Default OFF so an operator must explicitly
	// accept training-data uploads from clients. See
	// docs/wakeword-training-data.md.
	TrainingData ServerTrainingDataConfig `toml:"training_data"`

	// Security configures the HTTP security-header middleware (CSP,
	// X-Frame-Options, Referrer-Policy, optional HSTS). Headers are on by
	// default; the zero value yields a strict baseline.
	Security ServerSecurityConfig `toml:"security"`

	// Debug gates runtime debugging surfaces (pprof). Off by default.
	Debug ServerDebugConfig `toml:"debug"`

	// OIDC configures JWT validation against an external identity provider,
	// used only when auth_mode = "oidc".
	OIDC ServerOIDCConfig `toml:"oidc"`
}

// ServerOIDCConfig configures Bearer-JWT validation against an external
// identity provider (Azure AD, Okta, Google Workspace, Auth0, ...). Used only
// when [server] auth_mode = "oidc". JWKSURL, Issuer, and Audience are required
// in that mode; the *Claim fields map token claims onto the caller identity.
type ServerOIDCConfig struct {
	JWKSURL          string `toml:"jwks_url"`
	Issuer           string `toml:"issuer"`
	Audience         string `toml:"audience"`
	ClockSkewSeconds int    `toml:"clock_skew_seconds"` // tolerated exp/nbf skew; default 60
	OrgClaim         string `toml:"org_claim"`          // claim -> OrgID; default "org_id"
	RoleClaim        string `toml:"role_claim"`         // claim -> Role; default "role"
}

// ServerSecurityConfig configures the HTTP security-header middleware. All
// headers are emitted by default; an operator only sets fields here to relax
// or extend the baseline (e.g. enable HSTS behind TLS, or supply a custom CSP).
type ServerSecurityConfig struct {
	Disabled              bool   `toml:"disabled"`                // turn the middleware off entirely (not recommended)
	ContentSecurityPolicy string `toml:"content_security_policy"` // override the default strict API CSP
	FrameOptions          string `toml:"frame_options"`           // override X-Frame-Options (default "DENY")
	ReferrerPolicy        string `toml:"referrer_policy"`         // override Referrer-Policy (default "no-referrer")
	HSTS                  bool   `toml:"hsts"`                    // emit Strict-Transport-Security (only meaningful behind TLS)
	HSTSMaxAgeSeconds     int    `toml:"hsts_max_age_seconds"`    // HSTS max-age; default 63072000 (2y) when HSTS is on
}

// ServerDebugConfig gates runtime debugging surfaces. pprof is OFF by default
// and, when on, refuses to mount on a non-loopback listener unless PprofPublic
// is also set (strongly discouraged on a public listener).
type ServerDebugConfig struct {
	PprofEnabled bool `toml:"pprof_enabled"`
	PprofPublic  bool `toml:"pprof_public"`
}

// VoiceAgentLimitsConfig configures Voice Agent session capacity. Zero values
// are treated as unset and fall back to the legacy [server] limits.
type VoiceAgentLimitsConfig struct {
	MaxGlobalSessions      int `toml:"max_global_sessions"`
	MaxPerIdentitySessions int `toml:"max_per_identity_sessions"`
}

// ServerTrainingDataConfig governs the server-side wake-word
// activation pipeline. AcceptUploads defaults to false so POST
// /v1/wakeword/activations returns 503 until an operator explicitly
// opts in.
type ServerTrainingDataConfig struct {
	// AcceptUploads gates POST /v1/wakeword/activations. When false
	// the endpoint returns 503 with a clear "feature disabled"
	// payload so device-side uploaders back off gracefully. Default
	// false.
	AcceptUploads bool `toml:"accept_uploads"`

	// AudioDir is the filesystem root where uploaded audio files
	// are stored. Empty resolves to <data>/wakeword-activations/ in
	// the container. Files land under <audio_dir>/<org>/<user>/<id>.wav.
	AudioDir string `toml:"audio_dir"`

	// PerUserQuotaBytes caps how many bytes one user can have on
	// disk before the server rejects further uploads with 413. Zero
	// = unlimited. Default 1 GiB (1073741824).
	PerUserQuotaBytes int64 `toml:"per_user_quota_bytes"`

	// RetentionDays auto-deletes uploaded clips older than this
	// many days via the maintenance worker. Zero = no auto-delete.
	// Default 180.
	RetentionDays int `toml:"retention_days"`
}

type ServerLiveKitConfig struct {
	Enabled      bool   `toml:"enabled"`
	URL          string `toml:"url"`            // e.g. wss://livekit.example.com
	APIKeyEnv    string `toml:"api_key_env"`    // env var name holding the LiveKit API key
	APISecretEnv string `toml:"api_secret_env"` // env var name holding the LiveKit API secret
	TokenTTLSec  int    `toml:"token_ttl_sec"`  // join-token TTL
	RoomPrefix   string `toml:"room_prefix"`    // room name prefix for SpeechKit-managed rooms
}

type ServerFeaturesConfig struct {
	Catalog      bool `toml:"catalog"`
	StorageReads bool `toml:"storage_reads"`
	Vocabulary   bool `toml:"vocabulary"`
	TTSDirect    bool `toml:"tts_direct"`
}

// PersonaConfig is a TOML-seeded Voice Agent persona. DB entries with the same
// ID override the TOML seed at runtime.
type PersonaConfig struct {
	ID              string            `toml:"id"`
	DisplayName     string            `toml:"display_name"`
	Description     string            `toml:"description"`
	Voice           string            `toml:"voice"`
	Locale          string            `toml:"locale"`
	DefaultRole     string            `toml:"default_role"`
	DefaultSequence string            `toml:"default_sequence"`
	Tags            []string          `toml:"tags"`
	Metadata        map[string]string `toml:"metadata"`
}

// RoleConfig is a TOML-seeded Voice Agent role. Roles are referenced from
// Personas via ID and compose the LiveConfig prompt layers.
type RoleConfig struct {
	ID                          string   `toml:"id"`
	DisplayName                 string   `toml:"display_name"`
	SystemPrompt                string   `toml:"system_prompt"`
	RefinementPrompt            string   `toml:"refinement_prompt"`
	Locale                      string   `toml:"locale"`
	VocabularyHint              string   `toml:"vocabulary_hint"`
	ToolAllowlist               []string `toml:"tool_allowlist"`
	Temperature                 float64  `toml:"temperature"`
	ThinkingEnabled             bool     `toml:"thinking_enabled"`
	ThinkingLevel               string   `toml:"thinking_level"`
	IncludeThoughts             bool     `toml:"include_thoughts"`
	ThinkingBudget              int      `toml:"thinking_budget"`
	AutomaticActivityDetection  bool     `toml:"automatic_activity_detection"`
	VADStartSensitivity         string   `toml:"vad_start_sensitivity"`
	VADEndSensitivity           string   `toml:"vad_end_sensitivity"`
	VADPrefixPaddingMs          int      `toml:"vad_prefix_padding_ms"`
	VADSilenceDurationMs        int      `toml:"vad_silence_duration_ms"`
	ActivityHandling            string   `toml:"activity_handling"`
	TurnCoverage                string   `toml:"turn_coverage"`
	ContextCompressionEnabled   bool     `toml:"context_compression_enabled"`
	ContextCompressionTriggerTk int64    `toml:"context_compression_trigger_tokens"`
	ContextCompressionTargetTk  int64    `toml:"context_compression_target_tokens"`
	EnableAffectiveDialog       bool     `toml:"enable_affective_dialog"`
}

// SequenceConfig is a TOML-seeded multi-step Voice Agent workflow.
type SequenceConfig struct {
	ID          string               `toml:"id"`
	DisplayName string               `toml:"display_name"`
	Description string               `toml:"description"`
	Completion  string               `toml:"completion"` // "all_steps" | "explicit_close" | "max_turns"
	MaxTurns    int                  `toml:"max_turns"`
	Steps       []SequenceStepConfig `toml:"steps"`
}

// SequenceStepConfig is a single step inside a SequenceConfig.
type SequenceStepConfig struct {
	ID           string   `toml:"id"`
	Instruction  string   `toml:"instruction"`
	ExitCriteria string   `toml:"exit_criteria"`
	RequireTools []string `toml:"require_tools"`
	MaxTurns     int      `toml:"max_turns"`
}
