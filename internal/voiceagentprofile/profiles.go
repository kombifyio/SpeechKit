package voiceagentprofile

import (
	"strings"

	"github.com/kombifyio/SpeechKit/internal/voicebehavior"
)

const (
	DefaultID                = voicebehavior.DefaultID
	BrainstormingCompanionID = voicebehavior.BrainstormingCompanionID
	HumorCompanionID         = voicebehavior.HumorCompanionID
	SupportCompanionID       = voicebehavior.SupportCompanionID
)

type Profile struct {
	ID                string   `json:"id"`
	DisplayName       string   `json:"displayName"`
	Description       string   `json:"description,omitempty"`
	Voice             string   `json:"voice,omitempty"`
	FrameworkPrompt   string   `json:"frameworkPrompt,omitempty"`
	RoleID            string   `json:"roleId,omitempty"`
	DefaultSequenceID string   `json:"defaultSequenceId,omitempty"`
	BuiltIn           bool     `json:"builtIn,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

func BuiltInProfiles() []Profile {
	source := voicebehavior.BuiltInProfiles()
	out := make([]Profile, len(source))
	for i, profile := range source {
		out[i] = Profile{
			ID:                profile.ID,
			DisplayName:       profile.DisplayName,
			Description:       profile.Description,
			Voice:             profile.Voice,
			FrameworkPrompt:   profile.FrameworkPrompt,
			RoleID:            profile.RoleID,
			DefaultSequenceID: profile.DefaultSequenceID,
			BuiltIn:           profile.BuiltIn,
			Tags:              append([]string(nil), profile.Tags...),
		}
	}
	return out
}

func Resolve(id string) (Profile, bool) {
	normalized := strings.TrimSpace(id)
	for _, profile := range BuiltInProfiles() {
		if profile.ID == normalized {
			return profile, true
		}
	}
	return Profile{}, false
}

func NormalizeID(id string) string {
	if profile, ok := Resolve(id); ok {
		return profile.ID
	}
	return DefaultID
}
