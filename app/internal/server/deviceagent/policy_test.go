package deviceagent

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyAuthorizeReturnsServerOwnedTier1Command(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	policy, err := NewPolicy(
		validPolicyRule(now, ActionTurnOn),
		RuleOptions{
			RuleID:      "kitchen-light-off",
			DeviceID:    "device-kitchen",
			RoomID:      "room-kitchen",
			TriggerText: "Küchenlicht aus",
			Locale:      "de-DE",
			Action:      ActionTurnOff,
			EntityID:    "light.kitchen",
			NotBefore:   now.Add(-time.Hour),
			ExpiresAt:   now.Add(time.Hour),
		},
	)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	command, denial := policy.Authorize(
		" device-kitchen ",
		"room-kitchen",
		"kitchen-light-on",
		"KÜCHENLICHT\t  AN",
		"de_DE",
		now,
	)
	if denial != nil {
		t.Fatalf("Authorize denial=%#v", denial)
	}
	want := AuthorizedCommand{
		RuleID:        "kitchen-light-on",
		Utterance:     "Küchenlicht an",
		Locale:        "de-de",
		EntityID:      "light.kitchen",
		ExpectedState: ExpectedStateOn,
	}
	if command != want {
		t.Fatalf("Authorize command=%#v, want %#v", command, want)
	}
	if command.Utterance == "KÜCHENLICHT AN" {
		t.Fatal("authorized command reused caller-owned text")
	}

	off, denial := policy.Authorize("device-kitchen", "room-kitchen", "kitchen-light-off", "Küchenlicht aus", "de-DE", now)
	if denial != nil {
		t.Fatalf("Authorize turn_off denial=%#v", denial)
	}
	if off.ExpectedState != ExpectedStateOff || off.EntityID != "light.kitchen" {
		t.Fatalf("turn_off command=%#v", off)
	}
}

func TestPolicyDefaultDenyMissingRule(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		policy *Policy
	}{
		{name: "nil policy"},
		{name: "empty policy", policy: mustPolicy(t)},
		{name: "configured policy unknown id", policy: mustPolicy(t, validPolicyRule(now, ActionTurnOn))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, denial := test.policy.Authorize("device-kitchen", "room-kitchen", "missing-rule", "Küchenlicht an", "de-DE", now)
			assertPolicyDenial(t, command, denial, PolicyReasonRuleNotFound)
		})
	}
}

func TestPolicyAuthorizeEnforcesTimeWindow(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	rule := validPolicyRule(now, ActionTurnOn)
	policy := mustPolicy(t, rule)

	tests := []struct {
		name   string
		now    time.Time
		reason string
	}{
		{name: "missing time", now: time.Time{}, reason: PolicyReasonTimeInvalid},
		{name: "not yet valid", now: rule.NotBefore.Add(-time.Nanosecond), reason: PolicyReasonNotYetValid},
		{name: "expired at boundary", now: rule.ExpiresAt, reason: PolicyReasonExpired},
		{name: "expired after boundary", now: rule.ExpiresAt.Add(time.Second), reason: PolicyReasonExpired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, denial := policy.Authorize("device-kitchen", "room-kitchen", rule.RuleID, rule.TriggerText, rule.Locale, test.now)
			assertPolicyDenial(t, command, denial, test.reason)
		})
	}

	for _, boundary := range []time.Time{rule.NotBefore, rule.ExpiresAt.Add(-time.Nanosecond)} {
		if _, denial := policy.Authorize("device-kitchen", "room-kitchen", rule.RuleID, rule.TriggerText, rule.Locale, boundary); denial != nil {
			t.Fatalf("active boundary %s denied: %#v", boundary, denial)
		}
	}
}

func TestPolicyAuthorizeRequiresExactNormalizedBindings(t *testing.T) {
	now := time.Date(2026, time.July, 19, 1, 0, 0, 0, time.UTC)
	rule := validPolicyRule(now, ActionTurnOn)
	policy := mustPolicy(t, rule)

	tests := []struct {
		name     string
		deviceID string
		roomID   string
		text     string
		locale   string
		reason   string
	}{
		{name: "device mismatch", deviceID: "device-bedroom", roomID: rule.RoomID, text: rule.TriggerText, locale: rule.Locale, reason: PolicyReasonDeviceMismatch},
		{name: "room mismatch", deviceID: rule.DeviceID, roomID: "room-bedroom", text: rule.TriggerText, locale: rule.Locale, reason: PolicyReasonRoomMismatch},
		{name: "text synonym rejected", deviceID: rule.DeviceID, roomID: rule.RoomID, text: "Mach das Küchenlicht an", locale: rule.Locale, reason: PolicyReasonTextMismatch},
		{name: "text punctuation rejected", deviceID: rule.DeviceID, roomID: rule.RoomID, text: "Küchenlicht an!", locale: rule.Locale, reason: PolicyReasonTextMismatch},
		{name: "locale mismatch", deviceID: rule.DeviceID, roomID: rule.RoomID, text: rule.TriggerText, locale: "en-US", reason: PolicyReasonLocaleMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, denial := policy.Authorize(test.deviceID, test.roomID, rule.RuleID, test.text, test.locale, now)
			assertPolicyDenial(t, command, denial, test.reason)
		})
	}
}

func TestNewPolicyRejectsDuplicateRuleIDs(t *testing.T) {
	now := time.Now().UTC()
	rule := validPolicyRule(now, ActionTurnOn)
	duplicate := rule
	duplicate.DeviceID = "device-bedroom"
	duplicate.RoomID = "room-bedroom"
	duplicate.EntityID = "light.bedroom"
	if _, err := NewPolicy(rule, duplicate); !errors.Is(err, ErrPolicyRuleDuplicate) {
		t.Fatalf("NewPolicy duplicate error=%v, want %v", err, ErrPolicyRuleDuplicate)
	}
}

func TestNewPolicyRejectsNonTier1ActionsAndDomains(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		action string
		entity string
	}{
		{name: "toggle action", action: "toggle", entity: "light.kitchen"},
		{name: "unlock action", action: "unlock", entity: "light.kitchen"},
		{name: "lock domain", action: ActionTurnOn, entity: "lock.front_door"},
		{name: "switch domain", action: ActionTurnOff, entity: "switch.coffee_machine"},
		{name: "cover domain", action: ActionTurnOn, entity: "cover.garage_door"},
		{name: "media domain", action: ActionTurnOff, entity: "media_player.living_room"},
		{name: "nested light entity", action: ActionTurnOn, entity: "light.kitchen.extra"},
		{name: "uppercase light entity", action: ActionTurnOn, entity: "light.Kitchen"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := validPolicyRule(now, test.action)
			rule.Action = test.action
			rule.EntityID = test.entity
			if _, err := NewPolicy(rule); !errors.Is(err, ErrPolicyRuleInvalid) {
				t.Fatalf("NewPolicy error=%v, want %v", err, ErrPolicyRuleInvalid)
			}
		})
	}
}

func TestNewPolicyRequiresBoundedWindow(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*RuleOptions)
	}{
		{name: "missing not before", mutate: func(rule *RuleOptions) { rule.NotBefore = time.Time{} }},
		{name: "missing expiry", mutate: func(rule *RuleOptions) { rule.ExpiresAt = time.Time{} }},
		{name: "equal window", mutate: func(rule *RuleOptions) { rule.ExpiresAt = rule.NotBefore }},
		{name: "reverse window", mutate: func(rule *RuleOptions) { rule.ExpiresAt = rule.NotBefore.Add(-time.Second) }},
		{name: "window too long", mutate: func(rule *RuleOptions) { rule.ExpiresAt = rule.NotBefore.Add(31*24*time.Hour + time.Second) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := validPolicyRule(now, ActionTurnOn)
			test.mutate(&rule)
			if _, err := NewPolicy(rule); !errors.Is(err, ErrPolicyRuleInvalid) {
				t.Fatalf("NewPolicy error=%v, want %v", err, ErrPolicyRuleInvalid)
			}
		})
	}
}

func validPolicyRule(now time.Time, action string) RuleOptions {
	return RuleOptions{
		RuleID:      "kitchen-light-on",
		DeviceID:    "device-kitchen",
		RoomID:      "room-kitchen",
		TriggerText: "  Küchenlicht   an  ",
		Locale:      "de-DE",
		Action:      action,
		EntityID:    "light.kitchen",
		NotBefore:   now.Add(-time.Hour),
		ExpiresAt:   now.Add(time.Hour),
	}
}

func mustPolicy(t *testing.T, rules ...RuleOptions) *Policy {
	t.Helper()
	policy, err := NewPolicy(rules...)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

func assertPolicyDenial(t *testing.T, command AuthorizedCommand, denial *Denial, reason string) {
	t.Helper()
	if command != (AuthorizedCommand{}) {
		t.Fatalf("denied request returned command %#v", command)
	}
	if denial == nil {
		t.Fatalf("denied request returned nil denial; want %q", reason)
	}
	if denial.ErrorCode != PolicyErrorCodeDenied || denial.ReasonCode != reason || denial.UserGuidance == "" {
		t.Fatalf("denial=%#v, want error=%q reason=%q with guidance", denial, PolicyErrorCodeDenied, reason)
	}
}
