package config

import (
	"bytes"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	customtemplates "github.com/kombifyio/SpeechKit/app/internal/customize/templates"
	"github.com/kombifyio/SpeechKit/app/internal/productid"
	"github.com/kombifyio/SpeechKit/app/internal/voiceagentprofile"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

var (
	lastPolicyMu     sync.Mutex
	lastPolicy       PolicyValues
	lastLoadRecovery string

	// saveMu serializes Save: settings routes, profile activation and
	// background jobs can save concurrently, and the temp-file + rename
	// sequence must not interleave.
	saveMu sync.Mutex

	// readPolicyValues is the registry seam; tests replace it to simulate a
	// machine policy on any platform.
	readPolicyValues = ReadPolicyValues
)

// LastPolicy returns the PolicyValues applied during the most recent Load call.
// This accessor lets the app layer read policy metadata (Origin, KeysFound)
// for the policy.applied audit event without changing Load's return signature.
// Returns a zero-value PolicyValues if Load has not yet been called.
func LastPolicy() PolicyValues {
	lastPolicyMu.Lock()
	defer lastPolicyMu.Unlock()
	return lastPolicy
}

// LastLoadRecovery returns a user-facing notice when the most recent Load had
// to set an unreadable config.toml aside, or "" when it did not.
func LastLoadRecovery() string {
	lastPolicyMu.Lock()
	defer lastPolicyMu.Unlock()
	return lastLoadRecovery
}

// attachLoadedPolicy reads the registry policy and binds it to cfg. Every
// Load path runs it — a missing or unreadable config.toml must not bypass a
// machine policy.
func attachLoadedPolicy(cfg *Config) {
	policy := readPolicyValues()
	AttachPolicy(cfg, policy)
	lastPolicyMu.Lock()
	lastPolicy = policy
	lastPolicyMu.Unlock()
	if policy.KeysFound > 0 {
		slog.Info("config: policy overlay applied",
			"origin", policy.Origin,
			"keys_locked", policy.KeysFound,
			"machine_keys", policy.MachineKeys,
			"user_keys", policy.UserKeys)
	}
	if policy.IgnoredUserKeys > 0 {
		slog.Warn("config: ignored per-user registry values that would loosen settings",
			"count", policy.IgnoredUserKeys)
	}
	logPolicyRejections(policy.Rejected)
}

func setLoadRecovery(notice string) {
	lastPolicyMu.Lock()
	lastLoadRecovery = notice
	lastPolicyMu.Unlock()
}

// normalizeFreshConfig runs the normalizers a config built from defaults
// needs (no file, or an unreadable one).
func normalizeFreshConfig(cfg *Config) {
	NormalizeSpeechDefaults(cfg)
	NormalizeCustomizationDefaults(cfg)
	NormalizeHandsFreeConfig(cfg, true)
	NormalizeOutputConfig(cfg)
	NormalizeCaptureConfig(cfg)
	NormalizeDeepgramSTTSettings(cfg)
	EnableAlwaysOnLLM(cfg)
	normalizeWakewordConfig(cfg)
	EnforcePolicy(cfg)
	if netsec.FullLocal() {
		ApplyEgressPolicy(cfg)
	}
}

// Load reads config from the given path. Falls back to defaults if file not found.
// Without a registered product identity it returns ErrNoIdentity: the machine
// policy of an unnamed product cannot be read, and reading none would be
// fail-open.
func Load(path string) (*Config, error) {
	if err := netsec.BindBootProfile(); err != nil {
		return nil, err
	}
	if _, err := productid.Current(); err != nil {
		return nil, err
	}
	cfg := defaults()
	setLoadRecovery("")

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is the application config path supplied by startup/config plumbing.
	if err != nil {
		if os.IsNotExist(err) {
			attachLoadedPolicy(cfg)
			normalizeFreshConfig(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Warn (but do not block) when the config file is accessible to group or
	// other users on POSIX systems. No-op on Windows where Go's os.FileMode
	// does not reflect NTFS ACLs.
	if warning, permErr := checkConfigFilePermissions(path); permErr == nil && warning != "" {
		slog.Warn("config file permissions are loose", "msg", warning)
	}

	if ManagedMode() {
		// On a managed host config.toml is untrusted: only the user-editable
		// keys reach cfg; everything else stays at policy or default.
		filtered, ferr := untrustedManagedConfig(data)
		if ferr != nil {
			return recoverUnreadableConfig(path, data, ferr), nil
		}
		data = filtered
	}
	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		return recoverUnreadableConfig(path, data, err), nil
	}
	if err := rejectRemovedConfigAliases(meta); err != nil {
		return nil, err
	}
	cfg.definedKeys = map[string]bool{}
	for _, key := range operatorKeys {
		if meta.IsDefined(strings.Split(key, ".")...) {
			cfg.definedKeys[key] = true
		}
	}
	if err := NormalizePrivacyConfig(cfg); err != nil {
		return nil, err
	}
	if err := NormalizeMeetingCaptureChannels(cfg); err != nil {
		return nil, err
	}

	// Apply registry policy overlay (ADMX/GPO). Must run BEFORE the backfill
	// chain so that policy values are in place when backfill logic reads
	// them. On non-Windows platforms the overlay is empty (policy_other.go).
	attachLoadedPolicy(cfg)

	// Bridge legacy [feedback] to [store] if store.backend is not explicitly set.
	if cfg.Store.Backend == "" || cfg.Store.Backend == "sqlite" {
		if cfg.Feedback.DBPath != "" && !meta.IsDefined("store", "sqlite_path") && cfg.Store.SQLitePath == "" {
			cfg.Store.SQLitePath = cfg.Feedback.DBPath
		}
		if cfg.Feedback.MaxAudioStorageMB > 0 && !meta.IsDefined("store", "max_audio_storage_mb") && cfg.Store.MaxAudioStorageMB == 0 {
			cfg.Store.MaxAudioStorageMB = cfg.Feedback.MaxAudioStorageMB
		}
		if cfg.Feedback.AudioRetentionDays > 0 && !meta.IsDefined("store", "audio_retention_days") && cfg.Store.AudioRetentionDays == 0 {
			cfg.Store.AudioRetentionDays = cfg.Feedback.AudioRetentionDays
		}
		if !meta.IsDefined("store", "save_audio") {
			cfg.Store.SaveAudio = cfg.Feedback.SaveAudio
		}
	}

	migrateMeetingFallbackPolicy(meta, cfg)
	NormalizeMeetingGeneration(cfg)
	backfillLegacyAssistModels(meta, cfg)
	backfillLegacyModeHotkeys(meta, cfg)
	backfillStartupBehavior(meta, cfg)
	backfillVoiceAgentPromptLayers(cfg)
	normalizeSpeechDefaults(cfg, meta.IsDefined("speech"), meta.IsDefined("speech", "language"), meta.IsDefined("general", "language"))
	migrateSeededSpeechLanguage(cfg)
	NormalizeCustomizationDefaults(cfg)
	NormalizeHandsFreeConfig(cfg, meta.IsDefined("hands_free"))
	NormalizeOutputConfig(cfg)
	NormalizeCaptureConfig(cfg)
	NormalizeDeepgramSTTSettings(cfg)
	pinLegacyFoundryDeployments(meta, cfg)
	MigrateRetiredModels(cfg)
	EnableAlwaysOnLLM(cfg)
	normalizeWakewordConfig(cfg)
	cfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
	cfg.VoiceAgent.AgentSequenceID = strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID)
	cfg.VoiceAgent.CloseBehavior = NormalizeVoiceAgentCloseBehavior(
		cfg.VoiceAgent.CloseBehavior,
		VoiceAgentCloseBehaviorContinue,
	)
	cfg.VoiceAgent.BargeIn = NormalizeVoiceAgentBargeIn(
		cfg.VoiceAgent.BargeIn,
		VoiceAgentBargeInAuto,
	)
	cfg.UI.AssistOverlayMode = NormalizeOverlayFeedbackMode(
		cfg.UI.AssistOverlayMode,
		OverlayFeedbackModeSmallFeedback,
	)
	cfg.UI.VoiceAgentOverlayMode = NormalizeOverlayFeedbackMode(
		cfg.UI.VoiceAgentOverlayMode,
		OverlayFeedbackModeSmallFeedback,
	)
	// The backfill chain above may copy legacy values over pinned ones (the
	// [feedback] save_audio bridge, for one); the policy has the last word.
	EnforcePolicy(cfg)
	if netsec.FullLocal() {
		ApplyEgressPolicy(cfg)
	}
	return cfg, nil
}

// recoverUnreadableConfig handles a config.toml that does not decode (a
// crash or power loss mid-write, a bad hand edit, or a deliberate attempt to
// shed policy). It fails closed: the broken file is kept aside for the user,
// SpeechKit starts from defaults in the device_only network scope, the
// machine policy is applied as on every other path, and the recovered config
// is written back so a restart (which would otherwise re-seed an open-scope
// template) stays closed. The user widens the scope again in Settings.
// operatorKeys are the keys whose explicit presence in config.toml a runtime
// default must respect (Config.keyDefined): the server's self-hosted STT
// default leaves the routing strategy and [local] alone when the operator set
// them.
var operatorKeys = []string{"routing.strategy", "local.enabled", "local.model_path"}

func recoverUnreadableConfig(path string, data []byte, decodeErr error) *Config {
	slog.Warn("malformed config.toml: setting it aside and starting from defaults in the device_only network scope", "err", decodeErr)
	aside := fmt.Sprintf("%s.corrupt-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(path, aside); err != nil {
		// Keep a copy even when the original cannot be moved; the next Save
		// replaces it.
		if writeErr := os.WriteFile(aside, data, 0o600); writeErr != nil { // #nosec G703 -- aside is derived from the application config path.
			slog.Warn("malformed config.toml could not be kept aside", "rename_err", err, "copy_err", writeErr)
			aside = ""
		}
	}

	cfg := defaults()
	cfg.Privacy.NetworkScope = NetworkScopeDeviceOnly
	attachLoadedPolicy(cfg)
	normalizeFreshConfig(cfg)

	notice := "Your settings file could not be read, so SpeechKit started with default settings and network access limited to this device."
	if aside != "" {
		notice += " The unreadable file was kept as " + filepath.Base(aside) + "."
	}
	setLoadRecovery(notice)
	if err := save(path, cfg); err != nil {
		slog.Warn("could not write the recovered config.toml", "err", err)
	}
	return cfg
}

// save writes cfg to path atomically: the TOML goes to an owner-only temp
// file in the same directory, is fsynced, and replaces the target with a
// rename, so a crash or power loss leaves either the old or the new file and
// never a truncated one. Policy-pinned values are written as the user's own
// pre-policy values (see persistable), and the in-memory cfg is re-pinned to
// the policy so no code path can save its way around it. Outside this
// package every write goes through Shared, which calls it under its lock.
func save(path string, cfg *Config) error {
	if err := cfg.ValidateFullLocalSelections(); err != nil {
		return err
	}
	if path == "" {
		path = defaultConfigPath()
	}
	NormalizeSpeechDefaults(cfg)
	NormalizeCustomizationDefaults(cfg)
	NormalizeHandsFreeConfig(cfg, true)
	NormalizeOutputConfig(cfg)
	NormalizeCaptureConfig(cfg)
	NormalizeDeepgramSTTSettings(cfg)
	normalizeWakewordConfig(cfg)
	if err := NormalizePrivacyConfig(cfg); err != nil {
		return err
	}
	if err := NormalizeMeetingCaptureChannels(cfg); err != nil {
		return err
	}
	EnforcePolicy(cfg)

	var buf bytes.Buffer
	if err := cfg.ValidateFullLocalSelections(); err != nil {
		return err
	}
	if err := toml.NewEncoder(&buf).Encode(persistable(cfg)); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data := buf.Bytes()
	if ManagedMode() {
		// A managed host writes the user-editable keys only; nothing else in
		// config.toml would be read back.
		filtered, _, err := filterManagedTOML(data)
		if err != nil {
			return err
		}
		data = filtered
	}

	saveMu.Lock()
	defer saveMu.Unlock()
	// Write through a symlinked config.toml (dotfile setups) instead of
	// replacing the link with a regular file.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic replaces path with data via temp file + fsync + rename.
// os.CreateTemp creates the file 0600. On Windows os.Rename is MoveFileEx
// with MOVEFILE_REPLACE_EXISTING, which replaces an existing target.
func writeFileAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync config: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err = os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	if err = renameReplace(tmpPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	syncDir(dir)
	return nil
}

// renameReplace retries briefly: on Windows a scanner or indexer holding the
// target open makes MoveFileEx fail with a sharing violation for a moment.
func renameReplace(from, to string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return err
}

// syncDir makes the rename durable on POSIX filesystems. Windows has no
// directory fsync (NTFS journals the rename), so it is skipped there.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	d, err := os.Open(dir) // #nosec G304 -- dir is the application config directory.
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func NormalizeCaptureConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.General.DictationProcessingMode = NormalizeDictationProcessingMode(
		cfg.General.DictationProcessingMode,
		DictationProcessingModeAuto,
	)
	cfg.General.DictationLiveCommit = NormalizeDictationLiveCommit(
		cfg.General.DictationLiveCommit,
		DictationLiveCommitPassage,
	)
	cfg.Audio.InputSource = NormalizeAudioInputSource(
		cfg.Audio.InputSource,
		AudioInputSourceMicrophone,
	)
}

func NormalizeSpeechDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	normalizeSpeechDefaults(cfg, true, strings.TrimSpace(cfg.Speech.Language) != "", strings.TrimSpace(cfg.General.Language) != "")
}

func NormalizeCustomizationDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Customization.ActiveTemplateIDs = customtemplates.NormalizeActiveTemplateIDs(cfg.Customization.ActiveTemplateIDs)
	terms := make([]customtemplates.OperatorTerm, 0, len(cfg.Customization.Terms))
	for _, term := range cfg.Customization.Terms {
		terms = append(terms, customtemplates.OperatorTerm{Term: term.Term, SoundsLike: term.SoundsLike, Language: term.Language})
	}
	if len(terms) > 0 {
		customtemplates.SetOperatorTerms(terms)
	}
	// Always set, so a reload without vocabulary_ref drops the previous name.
	customtemplates.SetOperatorVocabularyRef(cfg.Customization.VocabularyRef)
}

// normalizeWakewordConfig pins the wake-word backend to a canonical,
// bundle-consistent value at load/save time so every reader (resolveWakeword,
// the settings snapshot, the self-test, and the wake-word enable route) sees
// the same backend. An empty or unrecognized value would otherwise resolve
// lazily and inconsistently at each call site — the ambiguity behind the
// "wake-word silently stopped firing" class of regressions.
func normalizeWakewordConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	resolved := NormalizeWakewordBackend(cfg.Wakeword.Backend)
	if resolved != cfg.Wakeword.Backend {
		slog.Info("wakeword backend normalized", "from", cfg.Wakeword.Backend, "to", resolved)
	}
	cfg.Wakeword.Backend = resolved
}

func normalizeSpeechDefaults(cfg *Config, speechDefined, speechLanguageDefined, generalLanguageDefined bool) {
	if cfg == nil {
		return
	}
	cfg.Speech.Language = strings.TrimSpace(cfg.Speech.Language)
	cfg.General.Language = strings.TrimSpace(cfg.General.Language)
	switch {
	case !speechDefined || !speechLanguageDefined:
		cfg.Speech.Language = firstNonEmptyConfig(cfg.General.Language, cfg.Speech.Language, stt.LanguageMulti)
	case !generalLanguageDefined:
		cfg.General.Language = firstNonEmptyConfig(cfg.Speech.Language, cfg.General.Language, stt.LanguageMulti)
	}
	if cfg.General.Language == "" {
		cfg.General.Language = firstNonEmptyConfig(cfg.Speech.Language, stt.LanguageMulti)
	}
	if cfg.Speech.Language == "" {
		cfg.Speech.Language = cfg.General.Language
	}
	if cfg.Speech.EndpointingMs < 0 {
		cfg.Speech.EndpointingMs = 0
	}
	if cfg.Speech.Speed <= 0 {
		cfg.Speech.Speed = 1
	}
	cfg.Speech.AudioFormat = strings.TrimSpace(cfg.Speech.AudioFormat)
	if cfg.Speech.AudioFormat == "" {
		cfg.Speech.AudioFormat = "mp3"
	}
}

// Who set the speech language (SpeechDefaultsConfig.LanguageSource).
const (
	LanguageSourceUser    = "user"
	LanguageSourceDefault = "default"
)

// legacySeededSpeechLanguage is the language the shipped config template
// wrote into [general] and [speech] before fresh installs became
// multilingual (#1013). It is an implicit default, not a user choice.
const legacySeededSpeechLanguage = "de"

// migrateSeededSpeechLanguage moves a config that still carries the
// installer-seeded German pin onto multilingual dictation (ADR 0006,
// decision of 2026-10-21): a pinned language drops every word spoken in
// another one. A language the user chose (LanguageSourceUser) is never
// rewritten. A config written before the marker existed cannot tell a
// deliberate German pin from the seeded one; it moves to multilingual, which
// still transcribes German completely, and German can be chosen again.
func migrateSeededSpeechLanguage(cfg *Config) {
	if cfg == nil {
		return
	}
	source := strings.ToLower(strings.TrimSpace(cfg.Speech.LanguageSource))
	switch source {
	case LanguageSourceUser, LanguageSourceDefault:
		cfg.Speech.LanguageSource = source
		return
	}
	if strings.EqualFold(cfg.Speech.Language, legacySeededSpeechLanguage) &&
		strings.EqualFold(cfg.General.Language, legacySeededSpeechLanguage) {
		cfg.Speech.Language = stt.LanguageMulti
		cfg.General.Language = stt.LanguageMulti
		cfg.Speech.DetectLanguage = true
		if strings.EqualFold(strings.TrimSpace(cfg.Providers.Deepgram.STTLanguage), legacySeededSpeechLanguage) {
			cfg.Providers.Deepgram.STTLanguage = stt.LanguageMulti
		}
		cfg.Speech.LanguageSource = LanguageSourceDefault
		slog.Info("speech language migrated from the seeded German pin to multilingual")
		return
	}
	if stt.IsMultilanguage(cfg.Speech.Language) {
		cfg.Speech.LanguageSource = LanguageSourceDefault
		return
	}
	cfg.Speech.LanguageSource = LanguageSourceUser
}

func firstNonEmptyConfig(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultConfigPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.toml")
}
