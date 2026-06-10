// Package customize defines SpeechKit's public Words/Replacements contract.
package customize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Kind string

const (
	KindSubstitution Kind = "substitution"
	KindSynonym      Kind = "synonym"
	KindSnippet      Kind = "snippet"
	KindCommand      Kind = "command"
	KindTemplate     Kind = "template"
)

type Stage string

const (
	StagePreRecognitionCleanup Stage = "pre_recognition_cleanup"
	StagePostSTT               Stage = "post_stt"
	StagePostLLM               Stage = "post_llm"
	StageOutput                Stage = "output"
)

type Mode string

const (
	ModeDictation  Mode = "dictation"
	ModeAssist     Mode = "assist"
	ModeVoiceAgent Mode = "voice_agent"
)

type ScopeKind string

const (
	ScopeBuiltin   ScopeKind = "builtin"
	ScopeApp       ScopeKind = "app"
	ScopeInstall   ScopeKind = "install"
	ScopeOrg       ScopeKind = "org"
	ScopeWorkspace ScopeKind = "workspace"
	ScopeUser      ScopeKind = "user"
	ScopeSession   ScopeKind = "session"
)

type ScopeRef struct {
	Kind ScopeKind `json:"kind"`
	Key  string    `json:"key,omitempty"`
}

type MatchType string

const (
	MatchLiteral     MatchType = "literal"
	MatchPhrase      MatchType = "phrase"
	MatchRegex       MatchType = "regex"
	MatchSpokenAlias MatchType = "spoken_alias"
)

type Match struct {
	Type          MatchType `json:"type"`
	Pattern       string    `json:"pattern"`
	CaseSensitive bool      `json:"case_sensitive,omitempty"`
	WordBoundary  bool      `json:"word_boundary,omitempty"`
}

type ReplacementOutput struct {
	Text     string         `json:"text,omitempty"`
	Intent   string         `json:"intent,omitempty"`
	Template string         `json:"template,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

type Word struct {
	ID         string    `json:"id"`
	Term       string    `json:"term"`
	SoundsLike []string  `json:"sounds_like,omitempty"`
	Language   string    `json:"language,omitempty"`
	Weight     float64   `json:"weight,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Scope      *ScopeRef `json:"scope,omitempty"`
	Source     string    `json:"source,omitempty"`
	Enabled    bool      `json:"enabled"`
	UsageCount int       `json:"usage_count,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type Replacement struct {
	ID         string            `json:"id"`
	Kind       Kind              `json:"kind"`
	Match      Match             `json:"match"`
	Output     ReplacementOutput `json:"output"`
	Language   string            `json:"language,omitempty"`
	Modes      []Mode            `json:"modes,omitempty"`
	Stage      Stage             `json:"stage"`
	Priority   int               `json:"priority,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Source     string            `json:"source,omitempty"`
	Scope      *ScopeRef         `json:"scope,omitempty"`
	Enabled    bool              `json:"enabled"`
	UsageCount int               `json:"usage_count,omitempty"`
	CreatedAt  time.Time         `json:"created_at,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at,omitempty"`
}

type Lexicon struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Language    string    `json:"language,omitempty"`
	WordIDs     []string  `json:"word_ids,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Source      string    `json:"source,omitempty"`
	Scope       *ScopeRef `json:"scope,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type Ruleset struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Language       string    `json:"language,omitempty"`
	ReplacementIDs []string  `json:"replacement_ids,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Source         string    `json:"source,omitempty"`
	Scope          *ScopeRef `json:"scope,omitempty"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type Context struct {
	Mode              Mode       `json:"mode,omitempty"`
	Language          string     `json:"language,omitempty"`
	Stage             Stage      `json:"stage,omitempty"`
	Tags              []string   `json:"tags,omitempty"`
	Persona           string     `json:"persona,omitempty"`
	ActiveTemplateIDs []string   `json:"active_template_ids,omitempty"`
	ScopeOrder        []ScopeRef `json:"scope_order,omitempty"`
}

type ListOptions struct {
	Language        string
	Mode            Mode
	Stage           Stage
	IncludeDisabled bool
	Source          string
	Tags            []string
}

type ReplaceOptions struct {
	Language string
	Source   string
}

type Pack struct {
	SchemaVersion string          `json:"schema_version"`
	ID            string          `json:"id,omitempty"`
	Name          string          `json:"name,omitempty"`
	Description   string          `json:"description,omitempty"`
	Words         []Word          `json:"words,omitempty"`
	Replacements  []Replacement   `json:"replacements,omitempty"`
	Lexicons      []Lexicon       `json:"lexicons,omitempty"`
	Rulesets      []Ruleset       `json:"rulesets,omitempty"`
	Attachments   json.RawMessage `json:"attachments,omitempty"`
	CreatedAt     time.Time       `json:"created_at,omitempty"`
}

const PackSchemaVersion = "speechkit.customization.pack.v1"

func NormalizeLanguage(language string) string {
	language = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(language, "_", "-")))
	if len(language) >= 2 {
		return language[:2]
	}
	return language
}

func NormalizeMode(mode Mode) Mode {
	switch strings.TrimSpace(strings.ToLower(string(mode))) {
	case "dictation", "dictate", "transcribe", "stt":
		return ModeDictation
	case "assist":
		return ModeAssist
	case "voice-agent", "voice_agent", "voiceagent", "realtime_voice":
		return ModeVoiceAgent
	default:
		return ""
	}
}

func NormalizeScopeKind(kind ScopeKind) ScopeKind {
	switch strings.TrimSpace(strings.ToLower(string(kind))) {
	case string(ScopeBuiltin):
		return ScopeBuiltin
	case string(ScopeApp):
		return ScopeApp
	case string(ScopeInstall):
		return ScopeInstall
	case "org", "tenant":
		return ScopeOrg
	case string(ScopeWorkspace):
		return ScopeWorkspace
	case string(ScopeUser):
		return ScopeUser
	case string(ScopeSession):
		return ScopeSession
	default:
		return ""
	}
}

func NormalizeScopeRef(ref ScopeRef) ScopeRef {
	ref.Kind = NormalizeScopeKind(ref.Kind)
	ref.Key = strings.TrimSpace(ref.Key)
	return ref
}

func DefaultDeviceScopeOrder() []ScopeRef {
	return []ScopeRef{
		{Kind: ScopeBuiltin},
		{Kind: ScopeApp},
		{Kind: ScopeInstall},
		{Kind: ScopeUser},
		{Kind: ScopeSession},
	}
}

func DefaultServerScopeOrder() []ScopeRef {
	return []ScopeRef{
		{Kind: ScopeBuiltin},
		{Kind: ScopeApp},
		{Kind: ScopeInstall},
		{Kind: ScopeOrg},
		{Kind: ScopeUser},
		{Kind: ScopeSession},
	}
}

func NormalizeKind(kind Kind) Kind {
	switch strings.TrimSpace(strings.ToLower(string(kind))) {
	case string(KindSubstitution):
		return KindSubstitution
	case string(KindSynonym):
		return KindSynonym
	case string(KindSnippet):
		return KindSnippet
	case string(KindCommand):
		return KindCommand
	case string(KindTemplate):
		return KindTemplate
	default:
		return ""
	}
}

func NormalizeStage(stage Stage) Stage {
	switch strings.TrimSpace(strings.ToLower(string(stage))) {
	case string(StagePreRecognitionCleanup):
		return StagePreRecognitionCleanup
	case string(StagePostSTT):
		return StagePostSTT
	case string(StagePostLLM):
		return StagePostLLM
	case string(StageOutput):
		return StageOutput
	default:
		return ""
	}
}

func NormalizeMatchType(matchType MatchType) MatchType {
	switch strings.TrimSpace(strings.ToLower(string(matchType))) {
	case "", string(MatchPhrase):
		return MatchPhrase
	case string(MatchLiteral):
		return MatchLiteral
	case string(MatchRegex):
		return MatchRegex
	case string(MatchSpokenAlias):
		return MatchSpokenAlias
	default:
		return ""
	}
}

func ValidateWord(word Word) error {
	if strings.TrimSpace(word.Term) == "" {
		return fmt.Errorf("customize: word term is required")
	}
	if word.Weight < 0 {
		return fmt.Errorf("customize: word weight must be >= 0")
	}
	return nil
}

func ValidateReplacement(replacement Replacement) error {
	if NormalizeKind(replacement.Kind) == "" {
		return fmt.Errorf("customize: unsupported replacement kind %q", replacement.Kind)
	}
	if NormalizeStage(replacement.Stage) == "" {
		return fmt.Errorf("customize: unsupported replacement stage %q", replacement.Stage)
	}
	if NormalizeMatchType(replacement.Match.Type) == "" {
		return fmt.Errorf("customize: unsupported match type %q", replacement.Match.Type)
	}
	if strings.TrimSpace(replacement.Match.Pattern) == "" {
		return fmt.Errorf("customize: replacement match pattern is required")
	}
	if replacement.Kind == KindSubstitution || replacement.Kind == KindSynonym {
		if strings.TrimSpace(replacement.Output.Text) == "" {
			return fmt.Errorf("customize: replacement output text is required for %s", replacement.Kind)
		}
	}
	for _, mode := range replacement.Modes {
		if NormalizeMode(mode) == "" {
			return fmt.Errorf("customize: unsupported replacement mode %q", mode)
		}
	}
	return nil
}

func ValidatePack(pack Pack) error {
	if strings.TrimSpace(pack.SchemaVersion) != "" && pack.SchemaVersion != PackSchemaVersion {
		return fmt.Errorf("customize: unsupported pack schema %q", pack.SchemaVersion)
	}
	for _, word := range pack.Words {
		if err := ValidateWord(word); err != nil {
			return err
		}
	}
	for _, replacement := range pack.Replacements {
		if err := ValidateReplacement(replacement); err != nil {
			return err
		}
	}
	for _, lexicon := range pack.Lexicons {
		if strings.TrimSpace(lexicon.Name) == "" {
			return fmt.Errorf("customize: lexicon name is required")
		}
	}
	for _, ruleset := range pack.Rulesets {
		if strings.TrimSpace(ruleset.Name) == "" {
			return fmt.Errorf("customize: ruleset name is required")
		}
	}
	if len(pack.Attachments) > 0 && !json.Valid(pack.Attachments) {
		return fmt.Errorf("customize: pack attachments must be valid JSON")
	}
	return nil
}

func WithDefaultsWord(word Word) Word {
	word.Term = strings.TrimSpace(word.Term)
	word.Language = NormalizeLanguage(word.Language)
	word.ID = strings.TrimSpace(word.ID)
	if word.ID == "" {
		word.ID = StableID("word", word.Language, word.Term)
	}
	if strings.TrimSpace(word.Source) == "" {
		word.Source = "settings"
	}
	return word
}

func WithDefaultsReplacement(replacement Replacement) Replacement {
	replacement.ID = strings.TrimSpace(replacement.ID)
	replacement.Kind = NormalizeKind(replacement.Kind)
	replacement.Stage = NormalizeStage(replacement.Stage)
	replacement.Match.Type = NormalizeMatchType(replacement.Match.Type)
	replacement.Match.Pattern = strings.TrimSpace(replacement.Match.Pattern)
	replacement.Language = NormalizeLanguage(replacement.Language)
	if strings.TrimSpace(replacement.Source) == "" {
		replacement.Source = "settings"
	}
	if replacement.ID == "" {
		replacement.ID = StableID("replacement", string(replacement.Kind), replacement.Language, string(replacement.Stage), string(replacement.Match.Type), replacement.Match.Pattern)
	}
	return replacement
}

func StableID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(part))))
		_, _ = h.Write([]byte{0})
	}
	return strings.TrimSpace(prefix) + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}
