package auditlog

// Gap kinds reported by CheckSequence.
const (
	// GapMissing: sequence numbers between AfterSeq and NextSeq are absent.
	GapMissing = "missing"
	// GapReset: an audit.started record continued from a lower sequence than
	// the records before it, so local records were removed and the writer
	// started over from what was left.
	GapReset = "reset"
	// GapOutOfOrder: a record repeats or goes back behind an earlier one.
	GapOutOfOrder = "out_of_order"
)

// SequenceGap is one break in sequence continuity. The records with
// sequence numbers strictly between AfterSeq and NextSeq are missing.
type SequenceGap struct {
	Index    int    `json:"index"`
	AfterSeq uint64 `json:"after_seq"`
	NextSeq  uint64 `json:"next_seq"`
	Kind     string `json:"kind"`
}

// SequenceRestart is one audit.started record: a process start or a
// reconfiguration, not a gap.
type SequenceRestart struct {
	Index   int    `json:"index"`
	Seq     uint64 `json:"seq"`
	LastSeq uint64 `json:"last_seq"`
	Reason  string `json:"reason"`
}

// SequenceReport is the outcome of CheckSequence.
type SequenceReport struct {
	FirstSeq uint64            `json:"first_seq"`
	LastSeq  uint64            `json:"last_seq"`
	Gaps     []SequenceGap     `json:"gaps,omitempty"`
	Restarts []SequenceRestart `json:"restarts,omitempty"`
	// Unnumbered counts records written before sequence numbers existed;
	// they are skipped.
	Unnumbered int `json:"unnumbered,omitempty"`
}

// CheckSequence checks that records (in file order) carry consecutive
// sequence numbers. prevSeq is the sequence of the record before the first
// one (0 when unknown, e.g. for the first file of an install).
//
// The rule an off-box consumer applies is the same: every record's seq is
// the previous record's seq plus one, an audit.started record included (its
// last_seq names the record it continues from). A larger step is a gap; a
// step back on audit.started is a reset of the local log.
func CheckSequence(records []Record, prevSeq uint64) SequenceReport {
	var report SequenceReport
	prev := prevSeq
	for i, rec := range records {
		if rec.Seq == 0 {
			report.Unnumbered++
			continue
		}
		if report.FirstSeq == 0 {
			report.FirstSeq = rec.Seq
		}
		report.LastSeq = rec.Seq
		if rec.Event == EventAuditStarted {
			restart := SequenceRestart{Index: i, Seq: rec.Seq, LastSeq: resourceUint(rec.Resource, "last_seq")}
			if reason, ok := rec.Resource["reason"].(string); ok {
				restart.Reason = reason
			}
			report.Restarts = append(report.Restarts, restart)
		}
		switch {
		case prev == 0 || rec.Seq == prev+1:
		case rec.Seq > prev+1:
			report.Gaps = append(report.Gaps, SequenceGap{Index: i, AfterSeq: prev, NextSeq: rec.Seq, Kind: GapMissing})
		case rec.Event == EventAuditStarted:
			report.Gaps = append(report.Gaps, SequenceGap{Index: i, AfterSeq: prev, NextSeq: rec.Seq, Kind: GapReset})
		default:
			report.Gaps = append(report.Gaps, SequenceGap{Index: i, AfterSeq: prev, NextSeq: rec.Seq, Kind: GapOutOfOrder})
		}
		prev = rec.Seq
	}
	return report
}

// resourceUint reads a non-negative number from a resource map that came
// from JSON (float64) or from Go code (integer types).
func resourceUint(resource map[string]any, key string) uint64 {
	switch v := resource[key].(type) {
	case float64:
		if v > 0 {
			return uint64(v)
		}
	case uint64:
		return v
	case int:
		if v > 0 {
			return uint64(v)
		}
	case int64:
		if v > 0 {
			return uint64(v)
		}
	}
	return 0
}
