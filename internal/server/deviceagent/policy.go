package deviceagent

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ActionTurnOn  = "turn_on"
	ActionTurnOff = "turn_off"

	ExpectedStateOn  = "on"
	ExpectedStateOff = "off"

	PolicyErrorCodeDenied = "local_policy_denied"

	PolicyReasonRuleNotFound     = "local_rule_not_found"
	PolicyReasonDeviceMismatch   = "local_rule_device_mismatch"
	PolicyReasonRoomMismatch     = "local_rule_room_mismatch"
	PolicyReasonTimeInvalid      = "local_rule_time_invalid"
	PolicyReasonNotYetValid      = "local_rule_not_yet_valid"
	PolicyReasonExpired          = "local_rule_expired"
	PolicyReasonTextMismatch     = "local_rule_text_mismatch"
	PolicyReasonLocaleMismatch   = "local_rule_locale_mismatch"
	maxPolicyIdentifierBytes     = 128
	maxPolicyTriggerTextBytes    = 512
	maxPolicyLocaleBytes         = 35
	maxPolicyEntityObjectIDBytes = 128
)

var (
	ErrPolicyRuleInvalid   = errors.New("device-agent local policy: invalid rule")
	ErrPolicyRuleDuplicate = errors.New("device-agent local policy: duplicate rule id")
)

// RuleOptions defines one static, server-owned G0 authorization rule. A rule
// is a local per-request allowlist entry, not a Workbench approval, Cloud
// standing grant, delegated identity, or federation capability.
type RuleOptions struct {
	RuleID      string
	DeviceID    string
	RoomID      string
	TriggerText string
	Locale      string
	Action      string
	EntityID    string
	NotBefore   time.Time
	ExpiresAt   time.Time
}

// AuthorizedCommand is assembled exclusively from the configured rule after
// every binding has matched. Utterance is the canonical server-owned trigger,
// never the caller-provided string.
type AuthorizedCommand struct {
	RuleID        string `json:"rule_id"`
	Utterance     string `json:"utterance"`
	Locale        string `json:"locale"`
	EntityID      string `json:"entity_id"`
	ExpectedState string `json:"expected_state"`
}

// Denial is the stable fail-closed reason envelope returned for every request
// that does not match one active local rule exactly.
type Denial struct {
	ErrorCode    string `json:"error_code"`
	ReasonCode   string `json:"reason_code"`
	UserGuidance string `json:"user_guidance"`
}

type policyRule struct {
	ruleID         string
	deviceID       string
	roomID         string
	utterance      string
	normalizedText string
	locale         string
	entityID       string
	expectedState  string
	notBefore      time.Time
	expiresAt      time.Time
}

// Policy is immutable after construction and therefore safe for concurrent
// authorization checks. An empty or nil Policy denies every request.
type Policy struct {
	rules map[string]policyRule
}

// NewPolicy validates and copies a static local allowlist. Only reversible
// Tier-1 light turn_on/turn_off rules are accepted.
func NewPolicy(options ...RuleOptions) (*Policy, error) {
	policy := &Policy{rules: make(map[string]policyRule, len(options))}
	for index, option := range options {
		rule, err := newPolicyRule(option)
		if err != nil {
			return nil, fmt.Errorf("%w at index %d: %w", ErrPolicyRuleInvalid, index, err)
		}
		if _, exists := policy.rules[rule.ruleID]; exists {
			return nil, fmt.Errorf("%w: %q", ErrPolicyRuleDuplicate, rule.ruleID)
		}
		policy.rules[rule.ruleID] = rule
	}
	return policy, nil
}

func newPolicyRule(option RuleOptions) (policyRule, error) {
	ruleID := strings.TrimSpace(option.RuleID)
	deviceID := strings.TrimSpace(option.DeviceID)
	roomID := strings.TrimSpace(option.RoomID)
	if !validPolicyIdentifier(ruleID) {
		return policyRule{}, errors.New("rule_id must be a bounded safe identifier")
	}
	if !validPolicyIdentifier(deviceID) {
		return policyRule{}, errors.New("device_id must be a bounded safe identifier")
	}
	if !validPolicyIdentifier(roomID) {
		return policyRule{}, errors.New("room_id must be a bounded safe identifier")
	}

	utterance, normalizedText := normalizePolicyText(option.TriggerText)
	if utterance == "" || len(utterance) > maxPolicyTriggerTextBytes {
		return policyRule{}, fmt.Errorf("trigger_text must contain 1..%d UTF-8 bytes", maxPolicyTriggerTextBytes)
	}
	locale, ok := normalizePolicyLocale(option.Locale)
	if !ok {
		return policyRule{}, errors.New("locale must be a bounded language tag")
	}

	action := strings.TrimSpace(option.Action)
	expectedState := ""
	switch action {
	case ActionTurnOn:
		expectedState = ExpectedStateOn
	case ActionTurnOff:
		expectedState = ExpectedStateOff
	default:
		return policyRule{}, errors.New("action must be turn_on or turn_off")
	}
	entityID := strings.TrimSpace(option.EntityID)
	if !validTier1LightEntityID(entityID) {
		return policyRule{}, errors.New("entity_id must be light.<safe-id>")
	}
	if option.NotBefore.IsZero() || option.ExpiresAt.IsZero() {
		return policyRule{}, errors.New("not_before and expires_at are required")
	}
	if !option.ExpiresAt.After(option.NotBefore) {
		return policyRule{}, errors.New("expires_at must be after not_before")
	}
	if option.ExpiresAt.Sub(option.NotBefore) > 31*24*time.Hour {
		return policyRule{}, errors.New("authorization window must not exceed 31 days")
	}

	return policyRule{
		ruleID:         ruleID,
		deviceID:       deviceID,
		roomID:         roomID,
		utterance:      utterance,
		normalizedText: normalizedText,
		locale:         locale,
		entityID:       entityID,
		expectedState:  expectedState,
		notBefore:      option.NotBefore.UTC(),
		expiresAt:      option.ExpiresAt.UTC(),
	}, nil
}

// Authorize resolves one request against one explicitly named local rule. It
// performs no NLP, fuzzy matching, intent inference, fallback, or rule search.
// A nil Denial means the command is authorized; otherwise the zero command is
// returned and callers must not dispatch an action.
func (p *Policy) Authorize(deviceID, roomID, ruleID, clientText, locale string, now time.Time) (AuthorizedCommand, *Denial) {
	if p == nil {
		return AuthorizedCommand{}, policyDenial(PolicyReasonRuleNotFound, "Configure an active local rule for this paired device and room.")
	}
	rule, exists := p.rules[strings.TrimSpace(ruleID)]
	if !exists {
		return AuthorizedCommand{}, policyDenial(PolicyReasonRuleNotFound, "Use an explicitly configured active local rule.")
	}
	if strings.TrimSpace(deviceID) != rule.deviceID {
		return AuthorizedCommand{}, policyDenial(PolicyReasonDeviceMismatch, "Use a rule assigned to this paired device.")
	}
	if strings.TrimSpace(roomID) != rule.roomID {
		return AuthorizedCommand{}, policyDenial(PolicyReasonRoomMismatch, "Use a rule assigned to this device room.")
	}
	if now.IsZero() {
		return AuthorizedCommand{}, policyDenial(PolicyReasonTimeInvalid, "Synchronize the local server clock before retrying.")
	}
	now = now.UTC()
	if now.Before(rule.notBefore) {
		return AuthorizedCommand{}, policyDenial(PolicyReasonNotYetValid, "Wait until the local rule becomes active.")
	}
	if !now.Before(rule.expiresAt) {
		return AuthorizedCommand{}, policyDenial(PolicyReasonExpired, "Create a new time-bounded local rule before retrying.")
	}
	_, normalizedText := normalizePolicyText(clientText)
	if normalizedText != rule.normalizedText {
		return AuthorizedCommand{}, policyDenial(PolicyReasonTextMismatch, "Repeat the exact configured local phrase.")
	}
	normalizedLocale, ok := normalizePolicyLocale(locale)
	if !ok || normalizedLocale != rule.locale {
		return AuthorizedCommand{}, policyDenial(PolicyReasonLocaleMismatch, "Use the locale configured for this local rule.")
	}
	return AuthorizedCommand{
		RuleID:        rule.ruleID,
		Utterance:     rule.utterance,
		Locale:        rule.locale,
		EntityID:      rule.entityID,
		ExpectedState: rule.expectedState,
	}, nil
}

func policyDenial(reasonCode, guidance string) *Denial {
	return &Denial{
		ErrorCode:    PolicyErrorCodeDenied,
		ReasonCode:   reasonCode,
		UserGuidance: guidance,
	}
}

func normalizePolicyText(value string) (canonical, comparison string) {
	if !utf8.ValidString(value) {
		return "", ""
	}
	canonical = strings.Join(strings.Fields(value), " ")
	return canonical, strings.ToLower(canonical)
}

func normalizePolicyLocale(value string) (string, bool) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
	if value == "" || len(value) > maxPolicyLocaleBytes {
		return "", false
	}
	parts := strings.Split(value, "-")
	if len(parts[0]) < 2 || len(parts[0]) > 3 || !asciiLetters(parts[0]) {
		return "", false
	}
	for _, part := range parts[1:] {
		if len(part) < 2 || len(part) > 8 || !asciiLettersOrDigits(part) {
			return "", false
		}
	}
	return value, true
}

func validPolicyIdentifier(value string) bool {
	if value == "" || len(value) > maxPolicyIdentifierBytes || !asciiLetterOrDigit(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !asciiLetterOrDigit(character) && character != '.' && character != '_' && character != '-' && character != ':' {
			return false
		}
	}
	return true
}

func validTier1LightEntityID(entityID string) bool {
	const prefix = "light."
	if !strings.HasPrefix(entityID, prefix) || len(entityID) <= len(prefix) || len(entityID) > len(prefix)+maxPolicyEntityObjectIDBytes {
		return false
	}
	objectID := entityID[len(prefix):]
	if !asciiLowerLetterOrDigit(objectID[0]) {
		return false
	}
	for index := 1; index < len(objectID); index++ {
		character := objectID[index]
		if !asciiLowerLetterOrDigit(character) && character != '_' {
			return false
		}
	}
	return true
}

func asciiLetters(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

func asciiLettersOrDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if !asciiLetterOrDigit(value[index]) {
			return false
		}
	}
	return true
}

func asciiLetterOrDigit(character byte) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9')
}

func asciiLowerLetterOrDigit(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
}
