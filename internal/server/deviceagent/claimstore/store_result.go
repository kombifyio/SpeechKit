package claimstore

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

func normalizeCompletedResult(result CompletedResult) (CompletedResult, error) {
	result.Outcome = strings.TrimSpace(result.Outcome)
	result.ConversationID = strings.TrimSpace(result.ConversationID)
	result.SpeechText = strings.TrimSpace(result.SpeechText)
	result.Language = strings.TrimSpace(result.Language)
	result.ResponseType = strings.TrimSpace(result.ResponseType)
	result.ErrorCode = strings.TrimSpace(result.ErrorCode)
	result.ReasonCode = strings.TrimSpace(result.ReasonCode)
	result.ActionExecuted = strings.TrimSpace(result.ActionExecuted)

	if len(result.ConversationID) > maxConversationIDBytes || len(result.SpeechText) > maxSpeechTextBytes || len(result.Language) > maxLanguageBytes ||
		len(result.ResponseType) > maxCodeBytes || len(result.ErrorCode) > maxCodeBytes || len(result.ReasonCode) > maxCodeBytes {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.Language != "" && !validLanguage(result.Language) {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.ResponseType != "" && !validCode(result.ResponseType) {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.ErrorCode != "" && !validCode(result.ErrorCode) {
		return CompletedResult{}, ErrInvalidResult
	}
	if result.ReasonCode != "" && !validCode(result.ReasonCode) {
		return CompletedResult{}, ErrInvalidResult
	}
	switch result.Outcome {
	case OutcomeSuccess:
		if result.SpeechText == "" || result.ErrorCode != "" || result.ReasonCode != "" {
			return CompletedResult{}, ErrInvalidResult
		}
	case OutcomeRejected:
		if result.ErrorCode == "" || result.ReasonCode == "" {
			return CompletedResult{}, ErrInvalidResult
		}
	default:
		return CompletedResult{}, ErrInvalidResult
	}
	switch result.ActionExecuted {
	case ActionExecutedYes, ActionExecutedNo, ActionNotApplicable:
	default:
		return CompletedResult{}, ErrInvalidResult
	}
	return result, nil
}

func validCode(code string) bool {
	if code == "" || len(code) > maxCodeBytes {
		return false
	}
	for _, character := range code {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-':
		default:
			return false
		}
	}
	return true
}

func validLanguage(language string) bool {
	if language == "" || len(language) > maxLanguageBytes {
		return false
	}
	for _, character := range language {
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

func digestCompletedResult(result CompletedResult) Digest {
	return digestCompletedResultWithDomain(resultDigestDomain, result, true)
}

func digestCompletedResultV1(result CompletedResult) Digest {
	return digestCompletedResultWithDomain(resultDigestDomainV1, result, false)
}

func digestCompletedResultWithDomain(domain string, result CompletedResult, includeConversationID bool) Digest {
	hash := sha256.New()
	writeResultField := func(value string) {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(value))
	}
	writeResultField(domain)
	writeResultField(result.Outcome)
	if includeConversationID {
		writeResultField(result.ConversationID)
	}
	writeResultField(result.SpeechText)
	writeResultField(result.Language)
	writeResultField(result.ResponseType)
	writeResultField(result.ErrorCode)
	writeResultField(result.ReasonCode)
	if result.Retryable {
		writeResultField("true")
	} else {
		writeResultField("false")
	}
	writeResultField(result.ActionExecuted)
	var digest Digest
	copy(digest[:], hash.Sum(nil))
	return digest
}
