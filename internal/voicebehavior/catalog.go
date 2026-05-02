// Package voicebehavior contains the shared Voice Agent behavior catalog used
// by both the local desktop runtime and the Linux server target.
package voicebehavior

import (
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultID                = "default"
	BrainstormingCompanionID = "brainstorming_companion"
	HumorCompanionID         = "humor_companion"
	SupportCompanionID       = "support_companion"

	BrainstormingCompanionRoleID = BrainstormingCompanionID + "_role"
	HumorCompanionRoleID         = HumorCompanionID + "_role"
	SupportCompanionRoleID       = SupportCompanionID + "_role"

	BrainstormingCompanionSequenceID = BrainstormingCompanionID + "_sequence"
	HumorCompanionSequenceID         = HumorCompanionID + "_sequence"
	SupportCompanionSequenceID       = SupportCompanionID + "_sequence"
)

var (
	ErrPersonaNotFound  = errors.New("voicebehavior: persona not found")
	ErrRoleNotFound     = errors.New("voicebehavior: role not found")
	ErrSequenceNotFound = errors.New("voicebehavior: sequence not found")
	ErrStepNotFound     = errors.New("voicebehavior: sequence step not found")
)

type Catalog struct {
	Personas  []Persona
	Roles     []Role
	Sequences []Sequence
}

type Persona struct {
	ID              string
	DisplayName     string
	Description     string
	Voice           string
	Locale          string
	DefaultRole     string
	DefaultSequence string
	Tags            []string
	Metadata        map[string]string
}

type Role struct {
	ID              string
	DisplayName     string
	SystemPrompt    string
	ToolAllowlist   []string
	ThinkingLevel   string
	ThinkingEnabled bool
}

type Sequence struct {
	ID          string
	DisplayName string
	Description string
	Completion  string
	MaxTurns    int
	Steps       []SequenceStep
}

type SequenceStep struct {
	ID           string
	Instruction  string
	ExitCriteria string
	RequireTools []string
	MaxTurns     int
}

type ResolvedBehavior struct {
	PersonaID           string
	RoleID              string
	SequenceID          string
	SequenceCompletion  string
	SequenceMaxTurns    int
	Voice               string
	Locale              string
	SystemPrompt        string
	StepID              string
	StepIndex           int
	StepCount           int
	StepInstruction     string
	StepExitCriteria    string
	StepMaxTurns        int
	StepRequiredToolIDs []string
}

type Profile struct {
	ID                string
	DisplayName       string
	Description       string
	Voice             string
	FrameworkPrompt   string
	RoleID            string
	DefaultSequenceID string
	BuiltIn           bool
	Tags              []string
}

func BuiltInCatalog() Catalog {
	return cloneCatalog(Catalog{
		Personas: []Persona{
			{
				ID:          DefaultID,
				DisplayName: "Default Voice Agent",
				Description: "Current Voice Mode behavior with the configured default voice and prompts.",
				Tags:        []string{"default"},
			},
			{
				ID:              BrainstormingCompanionID,
				DisplayName:     "Brainstorming Companion",
				Description:     "A creative sparring partner that challenges assumptions and looks for missing angles.",
				Voice:           "Aoede",
				DefaultRole:     BrainstormingCompanionRoleID,
				DefaultSequence: BrainstormingCompanionSequenceID,
				Tags:            []string{"brainstorming", "creative", "critical"},
			},
			{
				ID:              HumorCompanionID,
				DisplayName:     "Humor Companion",
				Description:     "A playful conversation profile for light, entertaining dialogue.",
				Voice:           "Puck",
				DefaultRole:     HumorCompanionRoleID,
				DefaultSequence: HumorCompanionSequenceID,
				Tags:            []string{"humor", "entertainment", "conversation"},
			},
			{
				ID:              SupportCompanionID,
				DisplayName:     "Support Companion",
				Description:     "A warm, solution-oriented helper that pays attention to the user's emotional context.",
				Voice:           "Charon",
				DefaultRole:     SupportCompanionRoleID,
				DefaultSequence: SupportCompanionSequenceID,
				Tags:            []string{"support", "solution-oriented", "emotion-aware"},
			},
		},
		Roles: []Role{
			{
				ID:           BrainstormingCompanionRoleID,
				DisplayName:  "Brainstorming Companion",
				SystemPrompt: brainstormingPrompt,
			},
			{
				ID:           HumorCompanionRoleID,
				DisplayName:  "Humor Companion",
				SystemPrompt: humorPrompt,
			},
			{
				ID:           SupportCompanionRoleID,
				DisplayName:  "Support Companion",
				SystemPrompt: supportPrompt,
			},
		},
		Sequences: []Sequence{
			{
				ID:          BrainstormingCompanionSequenceID,
				DisplayName: "Brainstorming Companion Session",
				Description: "Explore, challenge, and converge on concrete next moves.",
				Completion:  "all_steps",
				Steps: []SequenceStep{
					{ID: "frame", Instruction: "Clarify the user's goal, constraints, and what a useful outcome would look like. Ask one focused question if the goal is underspecified.", ExitCriteria: "The goal and useful outcome are clear enough to explore.", MaxTurns: 2},
					{ID: "diverge", Instruction: "Generate alternatives, challenge assumptions, point out blind spots, and invite the user to react to the strongest or riskiest idea.", ExitCriteria: "At least two alternatives, one assumption, and one risk or blind spot have been surfaced.", MaxTurns: 4},
					{ID: "converge", Instruction: "Synthesize the discussion into a concise recommendation, decision options, or concrete next moves.", ExitCriteria: "The user has a clear next step or decision frame.", MaxTurns: 2},
				},
			},
			{
				ID:          HumorCompanionSequenceID,
				DisplayName: "Humor Companion Session",
				Description: "Keep the exchange playful while preserving the user's intent.",
				Completion:  "explicit_close",
				Steps: []SequenceStep{
					{ID: "read-room", Instruction: "Pick up the user's intent and emotional tone before joking. Keep humor kind and situational.", ExitCriteria: "The tone is understood and the user is engaged.", MaxTurns: 2},
					{ID: "play", Instruction: "Offer quick-witted, light responses or playful options while still answering the actual request.", ExitCriteria: "The exchange has a playful rhythm without losing the user's goal.", MaxTurns: 4},
					{ID: "land", Instruction: "Bring the thread back to a useful answer, decision, or friendly closing beat.", ExitCriteria: "The user has either the requested help or a satisfying closing.", MaxTurns: 2},
				},
			},
			{
				ID:          SupportCompanionSequenceID,
				DisplayName: "Support Companion Session",
				Description: "Acknowledge, clarify, and guide toward a practical next step.",
				Completion:  "all_steps",
				Steps: []SequenceStep{
					{ID: "check-in", Instruction: "Acknowledge the user's situation and, when natural, briefly check how they are feeling.", ExitCriteria: "The user's emotional context and immediate concern are acknowledged.", MaxTurns: 2},
					{ID: "clarify", Instruction: "Clarify the practical problem, constraints, and what support would be most useful. Ask one focused question if needed.", ExitCriteria: "The concrete problem and constraint are clear.", MaxTurns: 3},
					{ID: "next-step", Instruction: "Offer one or two concrete next steps. Prefer practical help over generic reassurance.", ExitCriteria: "The user has a concrete next step.", MaxTurns: 2},
				},
			},
		},
	})
}

func (c Catalog) Persona(id string) (Persona, bool) {
	id = strings.TrimSpace(id)
	for _, p := range c.Personas {
		if p.ID == id {
			return clonePersona(p), true
		}
	}
	return Persona{}, false
}

func (c Catalog) Role(id string) (Role, bool) {
	id = strings.TrimSpace(id)
	for _, r := range c.Roles {
		if r.ID == id {
			return cloneRole(r), true
		}
	}
	return Role{}, false
}

func (c Catalog) Sequence(id string) (Sequence, bool) {
	id = strings.TrimSpace(id)
	for _, s := range c.Sequences {
		if s.ID == id {
			return cloneSequence(s), true
		}
	}
	return Sequence{}, false
}

func (c Catalog) Resolve(personaID, roleID, sequenceID string, stepIndex int) (ResolvedBehavior, error) {
	persona, ok := c.Persona(personaID)
	if !ok {
		return ResolvedBehavior{}, ErrPersonaNotFound
	}
	if roleID = strings.TrimSpace(roleID); roleID == "" {
		roleID = persona.DefaultRole
	}
	if sequenceID = strings.TrimSpace(sequenceID); sequenceID == "" {
		sequenceID = persona.DefaultSequence
	}

	var role Role
	if roleID != "" {
		role, ok = c.Role(roleID)
		if !ok {
			return ResolvedBehavior{}, fmt.Errorf("%w: %q", ErrRoleNotFound, roleID)
		}
	}

	resolved := ResolvedBehavior{
		PersonaID:    persona.ID,
		RoleID:       roleID,
		Voice:        persona.Voice,
		Locale:       persona.Locale,
		SystemPrompt: strings.TrimSpace(role.SystemPrompt),
	}

	if sequenceID == "" {
		return resolved, nil
	}
	sequence, ok := c.Sequence(sequenceID)
	if !ok {
		return ResolvedBehavior{}, fmt.Errorf("%w: %q", ErrSequenceNotFound, sequenceID)
	}
	if stepIndex < 0 || stepIndex >= len(sequence.Steps) {
		return ResolvedBehavior{}, fmt.Errorf("%w: index %d out of range for sequence %q", ErrStepNotFound, stepIndex, sequenceID)
	}
	step := sequence.Steps[stepIndex]
	resolved.SequenceID = sequence.ID
	resolved.SequenceCompletion = sequence.Completion
	resolved.SequenceMaxTurns = sequence.MaxTurns
	resolved.StepID = step.ID
	resolved.StepIndex = stepIndex
	resolved.StepCount = len(sequence.Steps)
	resolved.StepInstruction = step.Instruction
	resolved.StepExitCriteria = step.ExitCriteria
	resolved.StepMaxTurns = step.MaxTurns
	resolved.StepRequiredToolIDs = append([]string(nil), step.RequireTools...)
	resolved.SystemPrompt = ComposePrompt(resolved.SystemPrompt, step.ID, step.Instruction)
	return resolved, nil
}

func BuiltInProfiles() []Profile {
	catalog := BuiltInCatalog()
	out := make([]Profile, 0, len(catalog.Personas))
	for _, persona := range catalog.Personas {
		profile := Profile{
			ID:                persona.ID,
			DisplayName:       persona.DisplayName,
			Description:       persona.Description,
			Voice:             persona.Voice,
			RoleID:            persona.DefaultRole,
			DefaultSequenceID: persona.DefaultSequence,
			BuiltIn:           true,
			Tags:              append([]string(nil), persona.Tags...),
		}
		if role, ok := catalog.Role(persona.DefaultRole); ok {
			profile.FrameworkPrompt = role.SystemPrompt
		}
		out = append(out, profile)
	}
	return out
}

func ResolveProfile(id string) (Profile, bool) {
	normalized := strings.TrimSpace(id)
	for _, profile := range BuiltInProfiles() {
		if profile.ID == normalized {
			return profile, true
		}
	}
	return Profile{}, false
}

func NormalizeID(id string) string {
	if profile, ok := ResolveProfile(id); ok {
		return profile.ID
	}
	return DefaultID
}

func ComposePrompt(rolePrompt, stepID, stepInstruction string) string {
	rolePrompt = strings.TrimSpace(rolePrompt)
	stepInstruction = strings.TrimSpace(stepInstruction)
	if stepInstruction == "" {
		return rolePrompt
	}
	if rolePrompt == "" {
		return stepInstruction
	}
	return rolePrompt + "\n\n[Current step: " + stepID + "]\n" + stepInstruction
}

const brainstormingPrompt = `You are the SpeechKit Brainstorming Companion in realtime voice mode.
Respond in the user's language. Help the user think beyond the obvious while staying practical.
Challenge assumptions with curiosity, ask what is missing, point out blind spots, and surface constraints or second-order effects.
Balance critical questioning with creative leaps: offer alternatives, reframes, unusual examples, and one or two concrete next moves.
Keep turns conversational and concise so the user can interrupt and build on ideas quickly.`

const humorPrompt = `You are the SpeechKit Humor Companion in realtime voice mode.
Respond in the user's language. Make the conversation entertaining, warm, and quick-witted without derailing the user's intent.
Use humor, playful observations, and lively phrasing, but keep jokes kind, non-hurtful, and appropriate to the situation.
If the user asks for help, still help; if the user just wants to chat, keep the rhythm light and conversational.
Keep replies short enough for natural voice dialogue and leave room for the user to jump in.`

const supportPrompt = `You are the SpeechKit Support Companion in realtime voice mode.
Respond in the user's language. Start new conversations with a brief check-in about how the user is feeling when it fits naturally.
Be emotionally aware, steady, and practical: acknowledge the feeling, clarify the problem, and guide the user toward the next useful step.
Prefer concrete help over generic reassurance. Ask one focused question when the situation is unclear.
Keep replies calm, concise, and action-oriented so the user feels supported and can move forward.`

func cloneCatalog(c Catalog) Catalog {
	out := Catalog{
		Personas:  make([]Persona, len(c.Personas)),
		Roles:     make([]Role, len(c.Roles)),
		Sequences: make([]Sequence, len(c.Sequences)),
	}
	for i, p := range c.Personas {
		out.Personas[i] = clonePersona(p)
	}
	for i, r := range c.Roles {
		out.Roles[i] = cloneRole(r)
	}
	for i, s := range c.Sequences {
		out.Sequences[i] = cloneSequence(s)
	}
	return out
}

func clonePersona(p Persona) Persona {
	p.Tags = append([]string(nil), p.Tags...)
	if len(p.Metadata) > 0 {
		p.Metadata = cloneStringMap(p.Metadata)
	}
	return p
}

func cloneRole(r Role) Role {
	r.ToolAllowlist = append([]string(nil), r.ToolAllowlist...)
	return r
}

func cloneSequence(s Sequence) Sequence {
	steps := s.Steps
	s.Steps = make([]SequenceStep, len(steps))
	for i, step := range steps {
		step.RequireTools = append([]string(nil), step.RequireTools...)
		s.Steps[i] = step
	}
	return s
}

func cloneStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
