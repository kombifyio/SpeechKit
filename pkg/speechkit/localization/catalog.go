// Package localization resolves stable SpeechKit message IDs against the
// repository-owned locale catalogs.
package localization

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/language"
)

// MessageID is a stable semantic identifier for customer-facing prose.
type MessageID string

const (
	CompanionHomeAssistantNotConfigured MessageID = "companion.home_assistant.not_configured"
	CompanionHomeAssistantCommandEmpty  MessageID = "companion.home_assistant.command.empty"
	CompanionHomeAssistantNotMatched    MessageID = "companion.home_assistant.command.not_matched"
	CompanionHomeAssistantRejected      MessageID = "companion.home_assistant.command.rejected"
	CompanionHomeAssistantUnavailable   MessageID = "companion.home_assistant.unavailable"

	// Network-scope display names shown wherever a scope is named to the user.
	PrivacyScopeOpenName         MessageID = "sk.privacy.scope.open"
	PrivacyScopeLocalNetworkName MessageID = "sk.privacy.scope.local_network"
	PrivacyScopeDeviceOnlyName   MessageID = "sk.privacy.scope.device_only"

	// Disabled-reason prose for the stable reason IDs attached by the
	// network-scope policy (see NetworkScopeReason* in pkg/speechkit).
	PrivacyDisabledCloudProvider   MessageID = "sk.privacy.disabled.cloud_provider"
	PrivacyDisabledLocalService    MessageID = "sk.privacy.disabled.local_service_in_device_only"
	PrivacyDisabledServerScope     MessageID = "sk.privacy.disabled.server_in_device_only"
	PrivacyDisabledServerNotLocal  MessageID = "sk.privacy.disabled.server_url_not_local"
	PrivacyDisabledAgentBridge     MessageID = "sk.privacy.disabled.agent_bridge"
	PrivacyDisabledHomeAssistant   MessageID = "sk.privacy.disabled.home_assistant"
	PrivacyDisabledEdgeBeta        MessageID = "sk.privacy.disabled.edge_beta"
	PrivacyDisabledSetupTraffic    MessageID = "sk.privacy.disabled.setup_traffic"
	PrivacyDisabledTelemetry       MessageID = "sk.privacy.disabled.telemetry"
	PrivacyDisabledCloudAccount    MessageID = "sk.privacy.disabled.cloud_account"
	PrivacyDisabledVoiceAgentCloud MessageID = "sk.privacy.disabled.voice_agent_cloud"

	// Retention-scope display names and disabled reasons (see RetentionScope
	// and RetentionScopeReason* in pkg/speechkit). The retention axis is
	// orthogonal to the network scope: it says what outlives the work, not
	// where the process may reach.
	PrivacyRetentionRetainName      MessageID = "sk.privacy.retention.retain"
	PrivacyRetentionEphemeralName   MessageID = "sk.privacy.retention.ephemeral"
	PrivacyDisabledRetentionEphemer MessageID = "sk.privacy.disabled.retention_ephemeral"
	PrivacyDisabledProviderRetains  MessageID = "sk.privacy.disabled.provider_retains"

	// Meeting Mode screenshot status prose. These mirror the stable status
	// codes emitted by internal/meetingsnapshot so the device UI can surface
	// selecting/capturing/saving/saved/cancelled/error accessibly and
	// translated. Captures stay local; none of these strings name a window.
	MeetingSnapshotSelecting MessageID = "meeting.snapshot.selecting"
	MeetingSnapshotCapturing MessageID = "meeting.snapshot.capturing"
	MeetingSnapshotSaving    MessageID = "meeting.snapshot.saving"
	MeetingSnapshotSaved     MessageID = "meeting.snapshot.saved"
	MeetingSnapshotCancelled MessageID = "meeting.snapshot.cancelled"
	MeetingSnapshotError     MessageID = "meeting.snapshot.error"
)

var messageIDs = []MessageID{
	CompanionHomeAssistantNotConfigured,
	CompanionHomeAssistantCommandEmpty,
	CompanionHomeAssistantNotMatched,
	CompanionHomeAssistantRejected,
	CompanionHomeAssistantUnavailable,
	PrivacyScopeOpenName,
	PrivacyScopeLocalNetworkName,
	PrivacyScopeDeviceOnlyName,
	PrivacyDisabledCloudProvider,
	PrivacyDisabledLocalService,
	PrivacyDisabledServerScope,
	PrivacyDisabledServerNotLocal,
	PrivacyDisabledAgentBridge,
	PrivacyDisabledHomeAssistant,
	PrivacyDisabledEdgeBeta,
	PrivacyDisabledSetupTraffic,
	PrivacyDisabledTelemetry,
	PrivacyDisabledCloudAccount,
	PrivacyDisabledVoiceAgentCloud,
	PrivacyRetentionRetainName,
	PrivacyRetentionEphemeralName,
	PrivacyDisabledRetentionEphemer,
	PrivacyDisabledProviderRetains,
	MeetingSnapshotSelecting,
	MeetingSnapshotCapturing,
	MeetingSnapshotSaving,
	MeetingSnapshotSaved,
	MeetingSnapshotCancelled,
	MeetingSnapshotError,
}

var supportedLocales = []string{"en", "de", "es", "zh-Hans", "hi", "ar"}

// Review state per catalog lives in docs/localization/review-evidence.md
// (locale, sha256, review_state, reviewer, date). review_evidence_test.go
// fails when a catalog changes without its evidence row changing with it, so
// a non-English catalog is never silently presented as human-reviewed; rows
// marked "proposal" are machine translations awaiting a named reviewer.
//
//go:embed catalogs/*.json
var catalogFiles embed.FS

var catalogs = mustLoadCatalogs()

// SupportedLocales returns the initial SpeechKit locale set. English is the
// source locale and final fallback.
func SupportedLocales() []string {
	return append([]string(nil), supportedLocales...)
}

// ResolveLocale performs deterministic BCP-47 negotiation for the registered
// catalogs. Supported regional variants fall back to their registered base
// language. Simplified Chinese variants resolve to zh-Hans; traditional
// Chinese and unsupported languages fall back to English.
func ResolveLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "en"
	}
	tag, err := language.Parse(locale)
	if err != nil {
		return "en"
	}
	base, _ := tag.Base()
	switch base.String() {
	case "en", "de", "es", "hi", "ar":
		return base.String()
	case "zh":
		_, rawScript, _ := tag.Raw()
		if rawScript.String() != "Zzzz" {
			if rawScript.String() == "Hans" {
				return "zh-Hans"
			}
			return "en"
		}
		script, confidence := tag.Script()
		if confidence != language.No && script.String() == "Hans" {
			return "zh-Hans"
		}
		return "en"
	default:
		return "en"
	}
}

// Message is one catalog result with the negotiated locale that must also be
// used for result metadata and TTS.
type Message struct {
	ID     MessageID
	Locale string
	Text   string
}

// Resolve negotiates the locale once and resolves id from that exact catalog.
func Resolve(locale string, id MessageID) Message {
	resolvedLocale := ResolveLocale(locale)
	text := catalogs[resolvedLocale][id]
	if text == "" {
		resolvedLocale = "en"
		text = catalogs[resolvedLocale][id]
	}
	return Message{ID: id, Locale: resolvedLocale, Text: text}
}

// Text returns the localized text for id. Missing locale entries fall back to
// English. An unknown message ID returns an empty string so callers cannot
// accidentally surface the identifier itself as customer prose.
func Text(locale string, id MessageID) string {
	return Resolve(locale, id).Text
}

func mustLoadCatalogs() map[string]map[MessageID]string {
	loaded := make(map[string]map[MessageID]string, len(supportedLocales))
	for _, locale := range supportedLocales {
		path := fmt.Sprintf("catalogs/%s.json", locale)
		raw, err := catalogFiles.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("speechkit localization: read %s: %v", path, err))
		}
		catalog, err := decodeCatalog(raw)
		if err != nil {
			panic(fmt.Sprintf("speechkit localization: decode %s: %v", path, err))
		}
		if len(catalog) != len(messageIDs) {
			panic(fmt.Sprintf("speechkit localization: %s contains %d messages, want %d", path, len(catalog), len(messageIDs)))
		}
		for _, id := range messageIDs {
			if strings.TrimSpace(catalog[id]) == "" {
				panic(fmt.Sprintf("speechkit localization: %s is missing %q", path, id))
			}
		}
		loaded[locale] = catalog
	}
	return loaded
}

func decodeCatalog(raw []byte) (map[MessageID]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := opening.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("catalog root must be an object")
	}

	catalog := make(map[MessageID]string, len(messageIDs))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		keyText, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("catalog key must be a string")
		}
		id := MessageID(keyText)
		if !knownMessageID(id) {
			return nil, fmt.Errorf("unknown message ID %q", id)
		}
		if _, duplicate := catalog[id]; duplicate {
			return nil, fmt.Errorf("duplicate message ID %q", id)
		}
		var text string
		if err := decoder.Decode(&text); err != nil {
			return nil, fmt.Errorf("message %q: %w", id, err)
		}
		catalog[id] = text
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := closing.(json.Delim); !ok || delim != '}' {
		return nil, fmt.Errorf("catalog object is not closed")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("catalog contains a trailing JSON value")
		}
		return nil, fmt.Errorf("catalog trailing data: %w", err)
	}
	return catalog, nil
}

func knownMessageID(candidate MessageID) bool {
	for _, id := range messageIDs {
		if candidate == id {
			return true
		}
	}
	return false
}
