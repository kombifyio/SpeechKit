package config

// Server-Target (cmd/speechkit-server) configuration types: server core,
// OIDC, security headers, debug, LiveKit, features, training-data, and the
// TOML-seeded Persona/Role/Sequence Voice Agent definitions.

const (
	// Device-agent request claims are intentionally short-lived. The claim
	// ledger retains them for a full day so a successfully completed Home
	// Assistant action cannot be replayed after the request acceptance window.
	DefaultServerDeviceAgentMaxRequestAgeSec  = 600
	DefaultServerDeviceAgentFutureSkewSec     = 120
	DefaultServerDeviceAgentClaimRetentionSec = 86400
	DefaultServerDeviceAgentMaxClaims         = 10000

	// Explicit bounds keep a typo from turning the replay ledger into an
	// unbounded memory/disk sink or accepting stale action requests for days.
	MaxServerDeviceAgentRequestAgeSec     = 86400
	MaxServerDeviceAgentFutureSkewSec     = 3600
	MaxServerDeviceAgentClaimRetentionSec = 2592000
	MaxServerDeviceAgentClaims            = 1000000
)

// ServerConfig configures the standalone Linux server binary. Used only by
// cmd/speechkit-server; the desktop app never reads these values.
type ServerConfig struct {
	ListenAddr        string   `toml:"listen_addr"`          // e.g. ":8080"
	PublicURL         string   `toml:"public_url"`           // external API base URL, e.g. https://speechkit.example.com/api
	Modes             []string `toml:"modes"`                // subset of ["dictation","assist","voiceagent"]; empty = all
	AuthMode          string   `toml:"auth_mode"`            // "none" | "bearer" | "edge_hmac" | "bearer_or_edge" | "oidc" | "bearer_or_oidc"
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
	WSReadLimitBytes int64 `toml:"ws_read_limit_bytes"`
	// DictationStream configures the streaming Dictation WebSocket
	// (POST /v1/dictation/stream/sessions → ticket → WS with live
	// partial transcripts). Active only while the dictation mode is on.
	DictationStream ServerDictationStreamConfig `toml:"dictation_stream"`
	LiveKit         ServerLiveKitConfig         `toml:"livekit"`
	WhisperBinary   string                      `toml:"whisper_binary"` // absolute path inside container
	WhisperPort     int                         `toml:"whisper_port"`   // loopback port for whisper.cpp server
	ModelDir        string                      `toml:"model_dir"`      // persistent volume, e.g. /var/lib/speechkit/models
	LogFormat       string                      `toml:"log_format"`     // "json" | "text"
	LogLevel        string                      `toml:"log_level"`      // "debug" | "info" | "warn" | "error"
	Features        ServerFeaturesConfig        `toml:"features"`

	// TrainingData configures the server-side wake-word activation
	// collection endpoint. Default OFF so an operator must explicitly
	// accept training-data uploads from clients. See
	// docs/wakeword-training-data.md.
	TrainingData ServerTrainingDataConfig `toml:"training_data"`

	// AssistantUI sets the default appearance of the /assistant web page
	// (the server-hosted Voice Assistant surface). Per-browser overrides
	// stay client-side; this block is the operator default.
	AssistantUI ServerAssistantUIConfig `toml:"assistant_ui"`

	// Security configures the HTTP security-header middleware (CSP,
	// X-Frame-Options, Referrer-Policy, optional HSTS). Headers are on by
	// default; the zero value yields a strict baseline.
	Security ServerSecurityConfig `toml:"security"`

	// Debug gates runtime debugging surfaces (pprof). Off by default.
	Debug ServerDebugConfig `toml:"debug"`

	// OIDC configures JWT validation against an external identity provider,
	// used when auth_mode is "oidc" or "bearer_or_oidc".
	OIDC ServerOIDCConfig `toml:"oidc"`

	// Wyoming exposes SpeechKit STT + TTS as Wyoming voice services so an
	// ESPHome voice satellite — mediated by Home Assistant's Assist pipeline —
	// can use this server as its speech backend. Default OFF: enabling it opens
	// a separate TCP listener. See docs/server/wyoming.md.
	Wyoming ServerWyomingConfig `toml:"wyoming"`

	// DeviceAgent exposes the credential-minimal, local-only HTTP bridge used
	// by paired microphone/speaker satellites. It is disabled by default and
	// has its own per-device credentials; the general server bearer, OIDC,
	// edge-HMAC, Gateway, and federation identities do not authorize it.
	DeviceAgent ServerDeviceAgentConfig `toml:"device_agent"`

	// VoiceAgent carries Server-Target-only Voice Agent wiring such as the
	// generic tool bridge ([server.voiceagent.tool_bridge]). Session behavior
	// (provider, prompts, VAD) stays in the shared [voice_agent] section.
	VoiceAgent ServerVoiceAgentConfig `toml:"voiceagent"`
}

// ServerDeviceAgentConfig configures the local speechkit-device-agent bridge.
// Home Assistant URL/token custody remains server-side in
// [assist.home_assistant]; devices receive neither value.
type ServerDeviceAgentConfig struct {
	Enabled          bool   `toml:"enabled"`
	ServerInstanceID string `toml:"server_instance_id"`
	ClaimStorePath   string `toml:"claim_store_path"`

	// The replay ledger rejects stale or future-dated action requests and
	// remembers accepted request claims beyond the full acceptance window.
	// Zero selects the conservative exported defaults above.
	MaxRequestAgeSec  int `toml:"max_request_age_sec"`
	FutureSkewSec     int `toml:"future_skew_sec"`
	ClaimRetentionSec int `toml:"claim_retention_sec"`
	MaxClaims         int `toml:"max_claims"`

	Devices  []ServerDeviceAgentDeviceConfig `toml:"devices"`
	BoxMedia ServerDeviceAgentBoxMediaConfig `toml:"box_media"`
}

// ServerDeviceAgentBoxMediaConfig binds one Waveshare/Kombify Box to one
// existing paired device and one existing G0 command. The media token is
// independently provisioned through TokenEnv; no Home Assistant, general
// server, or device-agent credential is copied to the Box.
//
// CertificateFile and PrivateKeyFile are the operator-provisioned server key
// pair. PinnedCAFile is the local CA certificate distributed out-of-band to
// the Box, and PinnedCASHA256 is the lowercase SHA-256 of its DER certificate.
// SpeechKit verifies this evidence but never creates or distributes a CA.
type ServerDeviceAgentBoxMediaConfig struct {
	Enabled bool `toml:"enabled"`

	ListenAddr      string `toml:"listen_addr"`
	CertificateFile string `toml:"certificate_file"`
	PrivateKeyFile  string `toml:"private_key_file"`
	PinnedCAFile    string `toml:"pinned_ca_file"`
	PinnedCASHA256  string `toml:"pinned_ca_sha256"`
	TokenEnv        string `toml:"token_env"`

	DeviceID   string `toml:"device_id"`
	PairingID  string `toml:"pairing_id"`
	RoomID     string `toml:"room_id"`
	Transcript string `toml:"transcript"`
	CommandID  string `toml:"command_id"`
	Locale     string `toml:"locale"`
}

// ServerDeviceAgentDeviceConfig binds one device identity to an independent
// pairing epoch, credential, authoritative room, and direct LAN source ranges.
// PairingID is stable for one credential epoch, must never be recycled, and
// must change whenever the device token rotates. The claim ledger is keyed by
// PairingID rather than DeviceID so token rotation starts a fresh replay epoch.
type ServerDeviceAgentDeviceConfig struct {
	DeviceID           string   `toml:"device_id"`
	PairingID          string   `toml:"pairing_id"`
	RoomID             string   `toml:"room_id"`
	TokenEnv           string   `toml:"token_env"`
	AllowedClientCIDRs []string `toml:"allowed_client_cidrs"`

	// LocalRules are a static, server-owned G0 safety allow-list. They are
	// deliberately not represented as Workbench/cloud standing grants and
	// cannot authorize arbitrary text or safety-critical domains.
	LocalRules []ServerDeviceAgentLocalRuleConfig `toml:"local_rules"`
}

// ServerDeviceAgentLocalRuleConfig authorizes one exact, time-bounded light
// command for one paired device and its authoritative room. Removing a rule
// and restarting the local server revokes it; later Workbench-issued standing
// grants use a separate governed replication/receipt contract.
type ServerDeviceAgentLocalRuleConfig struct {
	RuleID      string `toml:"rule_id"`
	TriggerText string `toml:"trigger_text"`
	Locale      string `toml:"locale"`
	Action      string `toml:"action"`    // turn_on | turn_off
	EntityID    string `toml:"entity_id"` // light.* only in the G0 contract
	NotBefore   string `toml:"not_before"`
	ExpiresAt   string `toml:"expires_at"`
}

// ServerDeviceAgentClaimSettings is the normalized replay-ledger policy used
// by validation and the runtime constructor. Keeping normalization here avoids
// startup validation and runtime behavior drifting apart.
type ServerDeviceAgentClaimSettings struct {
	MaxRequestAgeSec  int
	FutureSkewSec     int
	ClaimRetentionSec int
	MaxClaims         int
}

// EffectiveClaimSettings resolves zero values to the conservative defaults.
// Negative values remain negative so validation can reject them explicitly.
func (c ServerDeviceAgentConfig) EffectiveClaimSettings() ServerDeviceAgentClaimSettings {
	settings := ServerDeviceAgentClaimSettings{
		MaxRequestAgeSec:  c.MaxRequestAgeSec,
		FutureSkewSec:     c.FutureSkewSec,
		ClaimRetentionSec: c.ClaimRetentionSec,
		MaxClaims:         c.MaxClaims,
	}
	if settings.MaxRequestAgeSec == 0 {
		settings.MaxRequestAgeSec = DefaultServerDeviceAgentMaxRequestAgeSec
	}
	if settings.FutureSkewSec == 0 {
		settings.FutureSkewSec = DefaultServerDeviceAgentFutureSkewSec
	}
	if settings.ClaimRetentionSec == 0 {
		settings.ClaimRetentionSec = DefaultServerDeviceAgentClaimRetentionSec
	}
	if settings.MaxClaims == 0 {
		settings.MaxClaims = DefaultServerDeviceAgentMaxClaims
	}
	return settings
}

// ServerDictationStreamConfig configures the streaming Dictation WebSocket
// surface. It reuses the Voice Agent ticket machinery ([server].ticket_ttl_sec
// and ws_read_limit_bytes apply here too) but carries its own session caps
// because keyboard dictation sessions are much shorter-lived than realtime
// voice conversations.
type ServerDictationStreamConfig struct {
	// Enabled gates the surface. Default true; the endpoint only exists
	// while the dictation mode itself is enabled.
	Enabled bool `toml:"enabled"`
	// MaxGlobalSessions caps concurrent streaming sessions across all
	// callers. Default 100.
	MaxGlobalSessions int `toml:"max_global_sessions"`
	// MaxPerIdentitySessions caps concurrent streaming sessions per caller.
	// Default 3.
	MaxPerIdentitySessions int `toml:"max_per_identity_sessions"`
	// IdleTimeoutSec terminates a session without any client- or
	// provider-side activity. Default 300 (5 min — deliberately shorter than
	// the Voice Agent's 15 min; a keyboard reconnects cheaply). Negative
	// disables.
	IdleTimeoutSec int `toml:"idle_timeout_sec"`
	// MaxSessionSec hard-caps a single streaming session's wall-clock
	// lifetime. Zero disables (self-hosted default); hosted deployments
	// should set a finite budget.
	MaxSessionSec int `toml:"max_session_sec"`
	// MaxStreamAudioSeconds caps the cumulative uploaded audio duration per
	// session — the streaming analog of max_decoded_audio_seconds. Zero
	// disables.
	MaxStreamAudioSeconds int `toml:"max_stream_audio_seconds"`
	// Emulation is reserved for a future chunked-batch fallback mode when no
	// streaming-capable provider is configured. v1 supports only "off":
	// clients receive capabilities.streaming=false and fall back to
	// POST /v1/dictation/transcribe themselves.
	Emulation string `toml:"emulation"`
}

// ServerWyomingConfig configures the Wyoming voice-protocol adapter. Wyoming is
// a raw-TCP, HA-native protocol with NO in-protocol auth, so the listener sits
// outside the HTTP auth chain; security is network trust (bind to a LAN
// interface, firewall the port, optionally restrict AllowedClientCIDRs to the
// Home Assistant host). Provider keys stay server-side; the device never holds
// a credential.
type ServerWyomingConfig struct {
	Enabled     bool     `toml:"enabled"`      // opt-in; opens a TCP listener
	Addr        string   `toml:"addr"`         // combined asr+tts listener; empty → ":10300"
	ServiceName string   `toml:"service_name"` // Info program/attribution name; empty → "speechkit"
	Languages   []string `toml:"languages"`    // advertised languages; empty → ["en"]
	Voice       string   `toml:"voice"`        // advertised + default TTS voice
	// AllowedClientCIDRs optionally restricts which peers may connect (defense
	// in depth — e.g. ["192.168.1.10/32"] for the HA host). Empty = allow any.
	AllowedClientCIDRs []string `toml:"allowed_client_cidrs"`
}

// ServerOIDCConfig configures Bearer-JWT validation against an external
// identity provider (Azure AD, Okta, Google Workspace, Auth0, ...). Used when
// [server] auth_mode is "oidc" or "bearer_or_oidc" (the latter additionally
// keeps accepting the static service bearer — the mobile/native onboarding
// shape). JWKSURL, Issuer, and Audience are required in those modes; the
// *Claim fields map token claims onto the caller identity.
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

// ServerVoiceAgentConfig groups Server-Target-only Voice Agent integration
// settings ([server.voiceagent]). Session behavior (provider, prompts, VAD)
// stays in the shared [voice_agent] section; this block only carries wiring
// that exists exclusively on the server binary.
type ServerVoiceAgentConfig struct {
	ToolBridge ServerToolBridgeConfig `toml:"tool_bridge"`
}

// ServerToolBridgeConfig configures the generic voice-agent tool bridge
// ([server.voiceagent.tool_bridge]): an HTTP endpoint pair that supplies
// per-session tool definitions (manifest) and executes tool calls (invoke)
// under the wire contract documented in docs/server/toolbridge.v1.md
// (version "speechkit.toolbridge.v1").
//
// The bridge is fail-closed on every axis: it is disabled by default, a
// session without a bridge credential gets no tools, and any manifest or
// invoke failure degrades the session to tool-less instead of erroring.
// The credential is supplied per session by the fronting proxy via
// CredentialHeader and is held memory-only — never persisted, never logged.
type ServerToolBridgeConfig struct {
	// Enabled turns the bridge on. Default false. Deployment env can flip
	// this by setting SPEECHKIT_TOOLBRIDGE_URL (see server_deployment_env.go).
	Enabled bool `toml:"enabled"`
	// ManifestURL is the GET endpoint returning the session tool manifest.
	ManifestURL string `toml:"manifest_url"`
	// InvokeURL is the POST endpoint executing one tool call.
	InvokeURL string `toml:"invoke_url"`
	// TimeoutMs bounds one invoke round-trip. Default 10000.
	TimeoutMs int `toml:"timeout_ms"`
	// MaxCallsPerSession hard-caps bridge tool calls per voice session.
	// Default 20.
	MaxCallsPerSession int `toml:"max_calls_per_session"`
	// CredentialHeader names the request header on POST /v1/voiceagent/sessions
	// that carries the per-session bridge credential. The header is accepted
	// only when edge-HMAC authentication succeeded on the same request.
	// Default "X-Edge-Obo-Subject-Token".
	CredentialHeader string `toml:"credential_header"`
}

// ServerTrainingDataConfig governs the server-side wake-word
// activation pipeline. AcceptUploads defaults to false so POST
// ServerAssistantUIConfig is the operator default appearance for the
// /assistant web page (speechkit-voice-assistant element). Values share the
// device vocabulary: variant "aura" | "waveform", mark "rosette" | "k" |
// "none". Unknown values normalize to the defaults at read time.
type ServerAssistantUIConfig struct {
	Variant           string `toml:"variant"`
	Mark              string `toml:"mark"`
	TranscriptDefault bool   `toml:"transcript_default"`
}

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
	// WakewordModels serves the public wake-word model catalog
	// (GET /v1/wakeword/models*): openWakeWord ONNX metadata for host
	// consumers and microWakeWord v2 manifests for ESPHome / on-device
	// consumers. Default ON — the payloads are already-public model metadata
	// and redirects to already-public files. Set false to hide the surface.
	WakewordModels bool `toml:"wakeword_models"`
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
