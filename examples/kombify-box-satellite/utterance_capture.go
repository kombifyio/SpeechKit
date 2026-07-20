package main

type utteranceEndReason string

const (
	utteranceEndSilence      utteranceEndReason = "silence"
	utteranceEndNoSpeech     utteranceEndReason = "no_speech"
	utteranceEndMaxDuration  utteranceEndReason = "max_duration"
	utteranceEndSourceClosed utteranceEndReason = "source_closed"
	utteranceEndUncleanStart utteranceEndReason = "unclean_start"
)

type utteranceCaptureResult struct {
	Bytes                int
	AuthorityStartBytes  int
	BoundaryVerified     bool
	Spoke                bool
	End                  utteranceEndReason
	DroppedFrames        uint64
	PlaybackContaminated bool
}

// Authoritative is deliberately narrower than "usable for conversation".
// Smart-home authority requires a normal silence boundary and an exact,
// lossless capture; deadlines, source closure, and local overflow may still be
// sent to a conversation provider but can never authorize a side effect.
func (r utteranceCaptureResult) Authoritative() bool {
	return r.BoundaryVerified && r.AuthorityStartBytes >= 0 && r.AuthorityStartBytes < r.Bytes &&
		r.Spoke && r.End == utteranceEndSilence && r.DroppedFrames == 0 && !r.PlaybackContaminated
}

func extractAuthorityPCM(result utteranceCaptureResult, captured []byte) ([]byte, bool) {
	if !result.Authoritative() || result.Bytes != len(captured) ||
		result.AuthorityStartBytes < 0 || result.AuthorityStartBytes >= len(captured) {
		return nil, false
	}
	// The seal owns its immutable PCM snapshot; later buffer reuse or provider
	// code cannot mutate the bytes that local STT classified and hashed.
	return append([]byte(nil), captured[result.AuthorityStartBytes:]...), true
}

// utteranceBoundary advances only from ordered PCM sample counts. It never
// uses consumer wall-clock time, so scheduler delay or a queued channel cannot
// turn an unread correction into a false silence boundary.
type utteranceBoundary struct {
	maxBytes            int
	silenceCutBytes     int
	noSpeechBytes       int
	leadSilenceBytes    int
	leadObservedBytes   int
	leadSpeechObserved  bool
	bytes               int
	liveBytes           int
	silentBytes         int
	spoke               bool
	boundaryVerified    bool
	authorityStartBytes int
}

func newUtteranceBoundary(maxSeconds, silenceCutMS int) *utteranceBoundary {
	return newUtteranceBoundaryWithLeadSilence(maxSeconds, silenceCutMS, 0)
}

func newAuthorityUtteranceBoundary(maxSeconds, silenceCutMS int) *utteranceBoundary {
	const authorityLeadSilenceMS = 250
	return newUtteranceBoundaryWithLeadSilence(maxSeconds, silenceCutMS, authorityLeadSilenceMS)
}

func newUtteranceBoundaryWithLeadSilence(maxSeconds, silenceCutMS, leadSilenceMS int) *utteranceBoundary {
	return &utteranceBoundary{
		maxBytes:         maxSeconds * 16000 * 2,
		silenceCutBytes:  silenceCutMS * 16000 * 2 / 1000,
		noSpeechBytes:    5 * 16000 * 2,
		leadSilenceBytes: leadSilenceMS * 16000 * 2 / 1000,
		boundaryVerified: leadSilenceMS <= 0,
	}
}

func (b *utteranceBoundary) wouldExceed(frameBytes int) bool {
	return b == nil || frameBytes < 0 || (b.maxBytes > 0 && b.bytes+frameBytes > b.maxBytes)
}

func (b *utteranceBoundary) observe(frameBytes int, voice bool) utteranceEndReason {
	if b == nil || frameBytes <= 0 {
		return ""
	}
	b.bytes += frameBytes
	if !b.boundaryVerified {
		if voice {
			b.leadSpeechObserved = true
			b.leadObservedBytes = 0
		} else {
			b.leadObservedBytes += frameBytes
		}
		if b.maxBytes > 0 && b.bytes >= b.maxBytes {
			return utteranceEndMaxDuration
		}
		if b.leadObservedBytes >= b.leadSilenceBytes {
			if b.leadSpeechObserved {
				return utteranceEndUncleanStart
			}
			b.boundaryVerified = true
			b.authorityStartBytes = b.bytes
		}
		return ""
	}
	b.liveBytes += frameBytes
	if voice {
		b.spoke = true
		b.silentBytes = 0
	} else if b.spoke {
		b.silentBytes += frameBytes
	}
	if b.maxBytes > 0 && b.bytes >= b.maxBytes {
		return utteranceEndMaxDuration
	}
	if b.spoke && b.silenceCutBytes > 0 && b.silentBytes >= b.silenceCutBytes {
		return utteranceEndSilence
	}
	if !b.spoke && b.liveBytes >= b.noSpeechBytes {
		return utteranceEndNoSpeech
	}
	return ""
}

func (b *utteranceBoundary) result(end utteranceEndReason) utteranceCaptureResult {
	if b == nil {
		return utteranceCaptureResult{End: end}
	}
	return utteranceCaptureResult{
		Bytes:               b.bytes,
		AuthorityStartBytes: b.authorityStartBytes,
		BoundaryVerified:    b.boundaryVerified,
		Spoke:               b.spoke,
		End:                 end,
	}
}
