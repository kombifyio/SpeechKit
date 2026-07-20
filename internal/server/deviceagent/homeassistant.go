package deviceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const (
	homeAssistantConversationPath = "/api/conversation/process"
	homeAssistantStatePath        = "/api/states/"

	maxHAResponseBodyBytes = 64 << 10
	maxHAEntityIDBytes     = 255
	maxHAStateBytes        = 255
	maxHALanguageBytes     = 64
	maxHATargets           = 256
	maxHATargetNameBytes   = 255
	maxHATargetIDBytes     = 255
)

type HomeAssistantOptions struct {
	BaseURL  string
	Token    string
	AgentID  string
	Language string
	Timeout  time.Duration
}

// HomeAssistantTarget is the documented target projection returned in the
// Conversation API response data.success and data.failed arrays.
type HomeAssistantTarget struct {
	Name string `json:"name"`
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type HomeAssistantResult struct {
	ConversationID string
	ResponseType   string
	Speech         string
	Language       string
	SuccessTargets []HomeAssistantTarget
	FailedTargets  []HomeAssistantTarget
	ErrorCode      string
	ReasonCode     string
	ActionExecuted string // yes | no | not_applicable | unknown
}

// HomeAssistantDispatchError carries only stable bridge classification. It
// deliberately excludes response bodies and credentials from Error().
type HomeAssistantDispatchError struct {
	ReasonCode     string
	Retryable      bool
	ActionExecuted string // no | unknown
	Cause          error
}

func (e *HomeAssistantDispatchError) Error() string {
	if e == nil {
		return "home assistant dispatch failed"
	}
	return "home assistant dispatch failed: " + e.ReasonCode
}

func (e *HomeAssistantDispatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type HomeAssistantClient struct {
	baseURL  *url.URL
	token    string
	agentID  string
	language string
	client   *http.Client
}

func NewHomeAssistantClient(opts HomeAssistantOptions) (*HomeAssistantClient, error) {
	baseRaw := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseRaw == "" {
		return nil, errors.New("home assistant bridge URL is required")
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return nil, errors.New("home assistant bridge token is required")
	}
	if len(token) > 4096 || containsControlByte(token) || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("home assistant bridge token must be a bounded bearer credential")
	}
	validation := localBridgeValidation()
	if err := netsec.ValidateProviderURL(baseRaw, validation); err != nil {
		return nil, fmt.Errorf("validate local Home Assistant URL: %w", err)
	}
	baseURL, err := url.Parse(baseRaw)
	if err != nil {
		return nil, fmt.Errorf("parse Home Assistant URL: %w", err)
	}
	if baseURL.Opaque != "" || baseURL.RawQuery != "" || baseURL.Fragment != "" || (baseURL.Path != "" && baseURL.Path != "/") {
		return nil, errors.New("home assistant URL must be an origin without path, query, or fragment")
	}
	agentID := strings.TrimSpace(opts.AgentID)
	if agentID != "" && !validHAAgentID(agentID) {
		return nil, errors.New("home assistant agent id must be a bounded stable identifier")
	}
	language := strings.TrimSpace(opts.Language)
	if language != "" && !validHALanguage(language) {
		return nil, errors.New("home assistant language must be a bounded language tag")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	// Do not accept an injected client here. The Home Assistant bearer may only
	// traverse the resolve-time local-address validator with proxies disabled
	// and redacted observability headers supplied by netsec.
	client := netsec.NewSafeHTTPClient(netsec.ClientOptions{
		Timeout:        timeout,
		DialValidation: &validation,
	})
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HomeAssistantClient{
		baseURL:  baseURL,
		token:    token,
		agentID:  agentID,
		language: language,
		client:   client,
	}, nil
}

func (c *HomeAssistantClient) Probe(ctx context.Context) error {
	if c == nil {
		return errors.New("home assistant bridge client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolveLocal(c.baseURL, "/api/"), http.NoBody)
	if err != nil {
		return fmt.Errorf("build Home Assistant probe: %w", err)
	}
	c.authorize(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return &HomeAssistantDispatchError{ReasonCode: "ha_probe_unreachable", Retryable: true, ActionExecuted: "no", Cause: err}
	}
	defer resp.Body.Close() //nolint:errcheck // bounded probe response is discarded
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	reason := "ha_probe_rejected"
	retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		reason = "ha_credentials_rejected"
	}
	return &HomeAssistantDispatchError{ReasonCode: reason, Retryable: retryable, ActionExecuted: "no"}
}

func (c *HomeAssistantClient) Converse(ctx context.Context, text, locale string) (*HomeAssistantResult, error) {
	if c == nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_bridge_unconfigured", ActionExecuted: "no"}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_utterance_missing", ActionExecuted: "no"}
	}
	body := map[string]string{
		"text":     text,
		"language": c.effectiveLanguage(locale),
	}
	if c.agentID != "" {
		body["agent_id"] = c.agentID
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_request_encode_failed", ActionExecuted: "no", Cause: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveLocal(c.baseURL, homeAssistantConversationPath), bytes.NewReader(encoded))
	if err != nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_request_build_failed", ActionExecuted: "no", Cause: err}
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		// Once Do starts, transport errors cannot prove whether HA executed the
		// command. The durable caller must mark the outcome indeterminate.
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_dispatch_indeterminate", Retryable: false, ActionExecuted: "unknown", Cause: err}
	}
	defer resp.Body.Close() //nolint:errcheck // response body is fully consumed below
	raw, readErr := readHAResponseBody(resp.Body)
	if readErr != nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_response_indeterminate", Retryable: false, ActionExecuted: "unknown", Cause: readErr}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHAHTTPFailure(resp.StatusCode)
	}
	var payload struct {
		ConversationID string `json:"conversation_id"`
		Response       struct {
			Speech struct {
				Plain *struct {
					Speech string `json:"speech"`
				} `json:"plain"`
				SSML *struct {
					Speech string `json:"speech"`
				} `json:"ssml"`
			} `json:"speech"`
			ResponseType string `json:"response_type"`
			Language     string `json:"language"`
			Data         struct {
				Code    string                `json:"code"`
				Success []HomeAssistantTarget `json:"success"`
				Failed  []HomeAssistantTarget `json:"failed"`
			} `json:"data"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_response_indeterminate", Retryable: false, ActionExecuted: "unknown", Cause: err}
	}
	speech := ""
	if payload.Response.Speech.Plain != nil {
		speech = strings.TrimSpace(payload.Response.Speech.Plain.Speech)
	}
	if speech == "" && payload.Response.Speech.SSML != nil {
		// HA remains the semantic authority, but SpeechKit deliberately returns
		// only its bounded spoken text rather than persisting raw response JSON.
		speech = strings.TrimSpace(payload.Response.Speech.SSML.Speech)
	}
	responseType := strings.ToLower(strings.TrimSpace(payload.Response.ResponseType))
	language := strings.TrimSpace(payload.Response.Language)
	if len(language) > maxHALanguageBytes || containsControlByte(language) {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_response_indeterminate", Retryable: false, ActionExecuted: "unknown", Cause: errors.New("home assistant response language is invalid")}
	}
	successTargets, err := normalizeHATargets(payload.Response.Data.Success)
	if err != nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_response_indeterminate", Retryable: false, ActionExecuted: "unknown", Cause: err}
	}
	failedTargets, err := normalizeHATargets(payload.Response.Data.Failed)
	if err != nil {
		return nil, &HomeAssistantDispatchError{ReasonCode: "ha_response_indeterminate", Retryable: false, ActionExecuted: "unknown", Cause: err}
	}
	errorCode, reasonCode, actionExecuted := classifyHAConversationResponse(
		responseType,
		payload.Response.Data.Code,
		successTargets,
		failedTargets,
	)
	return &HomeAssistantResult{
		ConversationID: strings.TrimSpace(payload.ConversationID),
		ResponseType:   responseType,
		Speech:         speech,
		Language:       language,
		SuccessTargets: successTargets,
		FailedTargets:  failedTargets,
		ErrorCode:      errorCode,
		ReasonCode:     reasonCode,
		ActionExecuted: actionExecuted,
	}, nil
}

// VerifyState reads the documented Home Assistant state resource from the
// already-validated origin and proves both the entity identity and exact state.
func (c *HomeAssistantClient) VerifyState(ctx context.Context, entityID, expectedState string) error {
	if c == nil {
		return &HomeAssistantDispatchError{ReasonCode: "ha_bridge_unconfigured", ActionExecuted: "unknown"}
	}
	entityID = strings.TrimSpace(entityID)
	expectedState = strings.TrimSpace(expectedState)
	if !validHAEntityID(entityID) || expectedState == "" || len(expectedState) > maxHAStateBytes || containsControlByte(expectedState) {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_verification_request_invalid", ActionExecuted: "unknown"}
	}

	endpoint := *c.baseURL
	endpoint.Path = homeAssistantStatePath + entityID
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_verification_request_invalid", ActionExecuted: "unknown", Cause: err}
	}
	c.authorize(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_verification_unreachable", ActionExecuted: "unknown", Cause: err}
	}
	defer resp.Body.Close() //nolint:errcheck // response body is fully consumed below
	raw, readErr := readHAResponseBody(resp.Body)
	if readErr != nil {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_verification_invalid", ActionExecuted: "unknown", Cause: readErr}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reason := "ha_state_verification_unavailable"
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			reason = "ha_credentials_rejected"
		case http.StatusNotFound:
			reason = "ha_state_not_found"
		}
		return &HomeAssistantDispatchError{ReasonCode: reason, ActionExecuted: "unknown"}
	}

	var state struct {
		EntityID string `json:"entity_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_verification_invalid", ActionExecuted: "unknown", Cause: err}
	}
	if !validHAEntityID(state.EntityID) || state.State == "" || len(state.State) > maxHAStateBytes || containsControlByte(state.State) {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_verification_invalid", ActionExecuted: "unknown"}
	}
	if state.EntityID != entityID {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_identity_mismatch", ActionExecuted: "unknown"}
	}
	if state.State != expectedState {
		return &HomeAssistantDispatchError{ReasonCode: "ha_state_mismatch", ActionExecuted: "unknown"}
	}
	return nil
}

func (c *HomeAssistantClient) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
}

func (c *HomeAssistantClient) effectiveLanguage(locale string) string {
	if c.language != "" {
		return normalizeHALanguage(c.language)
	}
	return normalizeHALanguage(locale)
}

func normalizeHALanguage(locale string) string {
	language := strings.ToLower(strings.TrimSpace(locale))
	if language == "" {
		return "en"
	}
	if i := strings.IndexAny(language, "-_"); i > 0 {
		language = language[:i]
	}
	return language
}

func classifyHAHTTPFailure(status int) *HomeAssistantDispatchError {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
		reason := "ha_request_rejected"
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			reason = "ha_credentials_rejected"
		}
		return &HomeAssistantDispatchError{ReasonCode: reason, Retryable: false, ActionExecuted: "no"}
	default:
		// Redirects, conflicts, rate limits, server failures, and undocumented
		// statuses arrive only after dispatch and cannot prove non-execution.
		return &HomeAssistantDispatchError{ReasonCode: "ha_dispatch_indeterminate", Retryable: false, ActionExecuted: "unknown"}
	}
}

func classifyHAConversationResponse(responseType, rawCode string, success, failed []HomeAssistantTarget) (string, string, string) {
	code := strings.ToLower(strings.TrimSpace(rawCode))
	switch responseType {
	case "action_done":
		if len(success) > 0 && len(failed) == 0 {
			// A Conversation response proves only what HA reported. The bridge
			// must bind the exact authorized target and read the resulting state
			// before it may expose action_executed=yes.
			return "", "ha_state_verification_required", "unknown"
		}
		return "home_assistant_response_indeterminate", "ha_action_outcome_unverified", "unknown"
	case "query_answer":
		return "", "", "not_applicable"
	case "error":
		switch code {
		case "no_intent_match", "no_valid_targets":
			return code, "ha_" + code, "no"
		case "failed_to_handle", "unknown":
			return "home_assistant_response_indeterminate", "ha_" + code, "unknown"
		case "":
			return "home_assistant_response_indeterminate", "ha_conversation_error_unknown", "unknown"
		default:
			return "home_assistant_response_indeterminate", "ha_conversation_error_unknown", "unknown"
		}
	default:
		return "home_assistant_response_indeterminate", "ha_response_type_unknown", "unknown"
	}
}

func normalizeHATargets(raw []HomeAssistantTarget) ([]HomeAssistantTarget, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxHATargets {
		return nil, errors.New("home assistant response contains too many targets")
	}
	targets := make([]HomeAssistantTarget, 0, len(raw))
	for _, target := range raw {
		target.Name = strings.TrimSpace(target.Name)
		target.Type = strings.ToLower(strings.TrimSpace(target.Type))
		target.ID = strings.TrimSpace(target.ID)
		if target.Name == "" || len(target.Name) > maxHATargetNameBytes || containsControlByte(target.Name) ||
			!validHATargetType(target.Type) || len(target.ID) > maxHATargetIDBytes || containsControlByte(target.ID) {
			return nil, errors.New("home assistant response contains an invalid target")
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func validHATargetType(value string) bool {
	switch value {
	case "area", "floor", "domain", "device_class", "device", "entity", "custom":
		return true
	default:
		return false
	}
}

func validHAEntityID(value string) bool {
	if value == "" || len(value) > maxHAEntityIDBytes || strings.Count(value, ".") != 1 {
		return false
	}
	domain, objectID, _ := strings.Cut(value, ".")
	return validHAEntityPart(domain, false) && validHAEntityPart(objectID, true)
}

func validHAAgentID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == ':':
		default:
			return false
		}
	}
	return true
}

func validHALanguage(value string) bool {
	if value == "" || len(value) > maxHALanguageBytes {
		return false
	}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-':
		default:
			return false
		}
	}
	return true
}

func validHAEntityPart(value string, allowLeadingDigit bool) bool {
	if value == "" || value[0] == '_' || value[len(value)-1] == '_' {
		return false
	}
	for index := range len(value) {
		character := value[index]
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9' && (allowLeadingDigit || index > 0):
		case character == '_':
		default:
			return false
		}
	}
	return true
}

func containsControlByte(value string) bool {
	for index := range len(value) {
		if value[index] < 0x20 || value[index] == 0x7f {
			return true
		}
	}
	return false
}

func readHAResponseBody(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxHAResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxHAResponseBodyBytes {
		return nil, errors.New("home assistant response exceeds the size limit")
	}
	return raw, nil
}

func localBridgeValidation() netsec.ValidationOptions {
	return netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     false,
		RequireLocal:  true,
	}
}

func resolveLocal(base *url.URL, path string) string {
	return base.ResolveReference(&url.URL{Path: path}).String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
