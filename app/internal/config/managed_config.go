package config

// managed_config.go makes config.toml untrusted on a managed host
// (ManagedMode). A user can write that file, so on a managed host it may
// carry personal preferences only: the keys in managedUserEditableKeys. Every
// other value comes from the machine policy or the compiled default. A
// non-editable key found in the file is dropped before decoding (one log line
// per key at start), never applied and never written back: Save writes the
// editable keys only.

import (
	"bytes"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// managedUserEditableKeys is the allowlist of config.toml keys a managed host
// takes from the user, as dotted TOML paths; an entry naming a table covers
// everything below it. The rule for adding a key: it changes how SpeechKit
// looks or is operated on this desk (hotkeys and their behaviour, UI
// language and appearance, overlay placement, audio devices, the user's own
// vocabulary and voice-command phrases), or it records a consent the user
// gives (transcript grants, which only enable what policy already allows).
// It never selects a provider, an endpoint, a credential, a model, a scope or
// a retention, logging, audit, update or storage setting.
var managedUserEditableKeys = []string{
	// Hotkeys and their behaviour.
	"general.hotkey",
	"general.dictate_hotkey",
	"general.assist_hotkey",
	"general.voice_agent_hotkey",
	"general.agent_hotkey",
	"general.hotkey_mode",
	"general.dictate_hotkey_behavior",
	"general.assist_hotkey_behavior",
	"general.voice_agent_hotkey_behavior",
	"meeting.screenshot_hotkey",
	// Language.
	"general.language",
	"speech.language",
	"meeting.summary_language",
	"meeting.additional_summary_languages",
	// Appearance, overlay position and other UI preferences.
	"ui",
	"general.start_at_login",
	"meeting.always_on_top",
	"meeting.compact_on_start",
	// Audio devices.
	"audio.device_id",
	"audio.device_name",
	"audio.output_device_id",
	// The user's own words and voice-command phrases.
	"vocabulary",
	"shortcuts",
	// Consents the user gives; each only enables what policy allows.
	"copilot.transcript_grant_provider",
	"copilot.transcript_grant_version",
	"copilot.transcript_grant_granted_at",
	"meeting.foundry_transcript_grant_version",
	"meeting.foundry_transcript_grant_granted_at",
	"microsoft365.transcript_grant_version",
	"microsoft365.transcript_grant_granted_at",
}

// ManagedUserEditableKeys returns a copy of the keys a managed host takes
// from config.toml.
func ManagedUserEditableKeys() []string {
	return append([]string(nil), managedUserEditableKeys...)
}

// managedKeyEditable reports whether path (a dotted TOML path) is an
// editable key or lies below one.
func managedKeyEditable(path string) bool {
	for _, key := range managedUserEditableKeys {
		if path == key || strings.HasPrefix(path, key+".") {
			return true
		}
	}
	return false
}

// managedKeyHasEditableChild reports whether some editable key lies below
// the table at path, so the walk has to descend into it.
func managedKeyHasEditableChild(path string) bool {
	for _, key := range managedUserEditableKeys {
		if strings.HasPrefix(key, path+".") {
			return true
		}
	}
	return false
}

// filterManagedTOML keeps the editable keys of a TOML document and returns
// it re-encoded, with the dotted paths of every dropped value (sorted).
func filterManagedTOML(data []byte) ([]byte, []string, error) {
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, nil, err
	}
	var dropped []string
	kept := filterManagedTable("", doc, &dropped)
	sort.Strings(dropped)
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(kept); err != nil {
		return nil, nil, fmt.Errorf("encode managed config: %w", err)
	}
	return buf.Bytes(), dropped, nil
}

func filterManagedTable(prefix string, table map[string]any, dropped *[]string) map[string]any {
	kept := map[string]any{}
	for key, value := range table {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if managedKeyEditable(path) {
			kept[key] = value
			continue
		}
		if sub, ok := value.(map[string]any); ok && managedKeyHasEditableChild(path) {
			if child := filterManagedTable(path, sub, dropped); len(child) > 0 {
				kept[key] = child
			}
			continue
		}
		*dropped = append(*dropped, flattenedTOMLKeys(path, value)...)
	}
	return kept
}

// flattenedTOMLKeys lists the leaf paths below value (value itself when it
// is not a table).
func flattenedTOMLKeys(path string, value any) []string {
	sub, ok := value.(map[string]any)
	if !ok || len(sub) == 0 {
		return []string{path}
	}
	var out []string
	for key, child := range sub {
		out = append(out, flattenedTOMLKeys(path+"."+key, child)...)
	}
	return out
}

// untrustedManagedConfig drops the non-editable keys from a config.toml read
// on a managed host and logs each one.
func untrustedManagedConfig(data []byte) ([]byte, error) {
	filtered, dropped, err := filterManagedTOML(data)
	if err != nil {
		return nil, err
	}
	for _, key := range dropped {
		slog.Warn("config: managed host ignores a config.toml value that only policy or the compiled default may set", "key", key)
	}
	return filtered, nil
}

// managedEditViolations names, sorted, every non-editable key whose value
// differs between cur and next; nil when every difference is editable.
func managedEditViolations(cur, next *Config) []string {
	a, errA := flattenedConfig(cur)
	b, errB := flattenedConfig(next)
	if errA != nil || errB != nil {
		return []string{"config"}
	}
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var out []string
	for _, key := range keys {
		if a[key] != b[key] && !managedKeyEditable(key) {
			out = append(out, key)
		}
	}
	return out
}

// flattenedConfig renders cfg as dotted TOML paths with printed values.
func flattenedConfig(cfg *Config) (map[string]string, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return nil, err
	}
	var doc map[string]any
	if _, err := toml.Decode(buf.String(), &doc); err != nil {
		return nil, err
	}
	out := map[string]string{}
	flattenInto(out, "", doc)
	return out, nil
}

func flattenInto(out map[string]string, prefix string, table map[string]any) {
	for key, value := range table {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if sub, ok := value.(map[string]any); ok {
			flattenInto(out, path, sub)
			continue
		}
		out[path] = fmt.Sprint(value)
	}
}
