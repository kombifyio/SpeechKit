package deviceagent

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/deviceagent/claimstore"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

func (b *Bridge) assist(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.AssistRequest
	if !b.decode(w, r, &body) {
		return
	}
	if !b.requireBoundDevice(w, binding, body.DeviceID, body.RoomID) {
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	body.CommandID = strings.TrimSpace(body.CommandID)
	body.Locale = strings.TrimSpace(body.Locale)
	body.SessionID = strings.TrimSpace(body.SessionID)
	if body.Text == "" || len(body.Text) > maxAssistTextBytes || body.CommandID == "" || len(body.CommandID) > 128 || body.Locale == "" || len(body.Locale) > 64 || body.SessionID == "" || len(body.SessionID) > 128 {
		b.writeError(w, http.StatusUnprocessableEntity, "assist_request_invalid", "assist_request_fields_invalid", false, "no", "Provide bounded command_id, text, locale, and session_id fields.")
		return
	}
	now := b.now().UTC()
	if reason := validateUUIDv7Window(body.RequestID, now, b.maxRequestAge, b.futureSkew); reason != "" {
		b.writeError(w, http.StatusUnprocessableEntity, "request_id_invalid", reason, false, "no", "Generate a fresh UUIDv7 request_id on the paired device.")
		return
	}
	command, denial := b.policy.Authorize(binding.deviceID, binding.roomID, body.CommandID, body.Text, body.Locale, now)
	if denial != nil {
		b.writeError(w, http.StatusForbidden, denial.ErrorCode, denial.ReasonCode, false, "no", denial.UserGuidance)
		return
	}
	key := ClaimKey{PairingID: binding.pairingID, RequestID: body.RequestID}
	digest, err := assistDigest(binding.token, key, command, turnInputSHA256(r.Context()))
	if err != nil {
		b.writeError(w, http.StatusUnprocessableEntity, "assist_request_invalid", "assist_request_digest_invalid", false, "no", "Use the paired device identity and bounded documented request fields.")
		return
	}
	decision, err := b.claims.Claim(r.Context(), key, digest, now)
	if err != nil {
		b.writeError(w, http.StatusServiceUnavailable, "claim_store_unavailable", "durable_claim_failed", false, "no", "The local safety ledger is unavailable; retry after repairing server storage.")
		return
	}
	switch decision.Disposition {
	case ClaimReplayCompleted:
		if decision.Result == nil {
			b.writeError(w, http.StatusInternalServerError, "claim_store_invalid", "completed_result_missing", false, "unknown", "Inspect the local safety ledger before issuing another command.")
			return
		}
		b.writeStoredResult(w, body.RequestID, *decision.Result, true)
		return
	case ClaimIndeterminate:
		b.writeError(w, http.StatusConflict, "request_outcome_indeterminate", "prior_dispatch_outcome_unknown", false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	case ClaimDigestConflict:
		b.writeError(w, http.StatusConflict, "request_conflict", "request_digest_mismatch", false, "no", "Use a new UUIDv7 request_id for different command content.")
		return
	case ClaimDispatchNew:
		// The durable claim is committed. This is the only branch allowed to
		// dispatch to HA, and it never retries.
	default:
		b.writeError(w, http.StatusInternalServerError, "claim_store_invalid", "claim_disposition_unknown", false, "no", "Inspect the local safety ledger configuration.")
		return
	}

	haResult, dispatchErr := b.ha.Converse(r.Context(), command.Utterance, command.Locale)
	if dispatchErr != nil {
		var classified *HomeAssistantDispatchError
		if !errors.As(dispatchErr, &classified) || classified.ActionExecuted == "unknown" {
			reason := "ha_dispatch_indeterminate"
			if classified != nil && classified.ReasonCode != "" {
				reason = classified.ReasonCode
			}
			_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
			b.writeError(w, http.StatusBadGateway, "home_assistant_unavailable", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
			return
		}
		stored := StoredResult{
			Status:         "denied",
			Language:       command.Locale,
			ErrorCode:      "home_assistant_rejected",
			ReasonCode:     classified.ReasonCode,
			Retryable:      classified.Retryable,
			ActionExecuted: "no",
		}
		if err := b.claims.Complete(r.Context(), key, digest, stored, b.now().UTC()); err != nil {
			b.writeError(w, http.StatusInternalServerError, "claim_commit_failed", "result_commit_indeterminate", false, "unknown", "Verify the Home Assistant state manually before issuing another command.")
			return
		}
		b.writeStoredResult(w, body.RequestID, stored, false)
		return
	}
	if haResult == nil {
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, "ha_response_missing", b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_unavailable", "ha_response_missing", false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	responseType := strings.ToLower(strings.TrimSpace(haResult.ResponseType))
	if responseType == "error" && haResult.ActionExecuted == claimstore.ActionExecutedNo {
		stored := StoredResult{
			Status:         "denied",
			ConversationID: haResult.ConversationID,
			ResponseType:   responseType,
			Speech:         boundedString(haResult.Speech, 8192),
			Language:       command.Locale,
			ErrorCode:      firstNonEmpty(haResult.ErrorCode, "home_assistant_rejected"),
			ReasonCode:     firstNonEmpty(haResult.ReasonCode, "ha_request_rejected"),
			Retryable:      false,
			ActionExecuted: claimstore.ActionExecutedNo,
		}
		if err := b.claims.Complete(r.Context(), key, digest, stored, b.now().UTC()); err != nil {
			b.writeError(w, http.StatusInternalServerError, "claim_commit_failed", "result_commit_indeterminate", false, "unknown", "Verify the Home Assistant state manually before issuing another command.")
			return
		}
		b.writeStoredResult(w, body.RequestID, stored, false)
		return
	}
	if responseType != "action_done" {
		reason := firstNonEmpty(haResult.ReasonCode, "ha_response_semantics_unknown")
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_response_indeterminate", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	if !exactAuthorizedTarget(haResult.SuccessTargets, haResult.FailedTargets, command.EntityID) {
		reason := "ha_authorized_target_unverified"
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_response_indeterminate", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	verifyCtx, cancel := context.WithTimeout(r.Context(), b.stateVerifyTimeout)
	verifyErr := b.ha.VerifyState(verifyCtx, command.EntityID, command.ExpectedState)
	cancel()
	if verifyErr != nil {
		reason := "ha_state_verification_failed"
		var classified *HomeAssistantDispatchError
		if errors.As(verifyErr, &classified) && classified.ReasonCode != "" {
			reason = classified.ReasonCode
		}
		_ = b.claims.MarkIndeterminate(r.Context(), key, digest, reason, b.now().UTC())
		b.writeError(w, http.StatusBadGateway, "home_assistant_response_indeterminate", reason, false, "unknown", "Verify the Home Assistant state manually before issuing a new command.")
		return
	}
	stored := StoredResult{
		Status:         "success",
		ConversationID: haResult.ConversationID,
		ResponseType:   responseType,
		Speech:         boundedString(haResult.Speech, 8192),
		Language:       command.Locale,
		ErrorCode:      "",
		ReasonCode:     "",
		Retryable:      false,
		ActionExecuted: claimstore.ActionExecutedYes,
	}
	if stored.Speech == "" {
		stored.Status = "denied"
		stored.ErrorCode = "home_assistant_response_invalid"
		stored.ReasonCode = "ha_spoken_response_missing"
	}
	if err := b.claims.Complete(r.Context(), key, digest, stored, b.now().UTC()); err != nil {
		b.writeError(w, http.StatusInternalServerError, "claim_commit_failed", "result_commit_indeterminate", false, "unknown", "Verify the Home Assistant state manually before issuing another command.")
		return
	}
	b.writeStoredResult(w, body.RequestID, stored, false)
}

func exactAuthorizedTarget(success, failed []HomeAssistantTarget, entityID string) bool {
	if len(success) != 1 || len(failed) != 0 {
		return false
	}
	target := success[0]
	return target.Type == "entity" && target.ID == entityID
}

func assistDigest(pairingToken string, key ClaimKey, command AuthorizedCommand, inputSHA256 string) ([32]byte, error) {
	digest, err := claimstore.HMACDigest([]byte(pairingToken), claimstore.CanonicalRequest{
		PairedDeviceID: key.PairingID,
		RequestID:      key.RequestID,
		RuleID:         command.RuleID,
		Locale:         strings.ToLower(strings.TrimSpace(command.Locale)),
		Text:           strings.TrimSpace(command.Utterance),
		EntityID:       command.EntityID,
		ExpectedState:  command.ExpectedState,
		InputSHA256:    inputSHA256,
	})
	if err != nil {
		return [32]byte{}, err
	}
	return [32]byte(digest), nil
}
