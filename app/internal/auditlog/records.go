package auditlog

import "context"

// The record helpers below are the one copy of the policy.blocked,
// data.export and data.delete records the desktop host writes from several
// packages. They carry policy names, actions, config keys, ids, kinds and
// sizes only, never content, titles or file paths. A write error is dropped,
// as AppendEvent reports it through OnWriteError.

// PolicyBlocked records one policy.blocked event: the policy that refused,
// the refused action and an optional detail (a config key or a reason).
func PolicyBlocked(ctx context.Context, policy, action, detail string) {
	resource := map[string]any{"policy": policy, "action": action}
	if detail != "" {
		resource["detail"] = detail
	}
	_ = AppendEvent(ctx, Record{
		Event:    EventPolicyBlocked,
		Resource: resource,
		Outcome:  OutcomeFailure,
	})
}

// PolicyKeysBlocked records an action a policy refused because it pins
// config keys: one policy.blocked event per key, with the key as detail and
// the policy policyFor names for it ("managed_config" when it names none or
// policyFor is nil). Without keys it records one "machine_policy" event
// without a detail.
func PolicyKeysBlocked(ctx context.Context, action string, keys []string, policyFor func(key string) string) {
	if len(keys) == 0 {
		PolicyBlocked(ctx, "machine_policy", action, "")
		return
	}
	for _, key := range keys {
		policy := ""
		if policyFor != nil {
			policy = policyFor(key)
		}
		if policy == "" {
			policy = "managed_config"
		}
		PolicyBlocked(ctx, policy, action, key)
	}
}

// DataExport records that one stored item left the store as a copy.
func DataExport(ctx context.Context, itemKind string, itemID int64, action, destination string, bytes int) {
	_ = AppendEvent(ctx, Record{
		Event: EventDataExport,
		Resource: map[string]any{
			"item_kind":   itemKind,
			"item_id":     itemID,
			"action":      action,
			"destination": destination,
			"bytes":       bytes,
		},
	})
}

// Stages of a meeting export record (MeetingExport).
const (
	MeetingExportStarted = "started"
	MeetingExportSynced  = "synced"
	MeetingExportFailed  = "failed"
)

// MeetingExport records one stage of the notes export of a meeting to an
// export target. statusID is the stable id of a failure and empty otherwise.
func MeetingExport(ctx context.Context, sessionID int64, target, stage, statusID string) {
	resource := map[string]any{"session_id": sessionID, "target": target, "stage": stage}
	outcome := OutcomeSuccess
	if stage == MeetingExportFailed {
		resource["status_id"] = statusID
		outcome = OutcomeFailure
	}
	_ = AppendEvent(ctx, Record{Event: EventMeetingExport, Resource: resource, Outcome: outcome})
}

// DataDelete records the deletion of one stored item. errorCode is empty on
// success.
func DataDelete(ctx context.Context, itemKind string, itemID int64, errorCode string) {
	resource := map[string]any{"item_kind": itemKind, "item_id": itemID}
	outcome := OutcomeSuccess
	if errorCode != "" {
		resource["error_code"] = errorCode
		outcome = OutcomeFailure
	}
	_ = AppendEvent(ctx, Record{
		Event:    EventDataDelete,
		Resource: resource,
		Outcome:  outcome,
	})
}
