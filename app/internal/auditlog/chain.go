package auditlog

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// RecordHash computes the tamper-evidence hash for rec: HMAC-SHA256 over the
// record's canonical JSON when key is non-empty, or plain SHA-256 when key is
// empty (integrity/ordering only, not forgery-proof). The Hash field is
// excluded from its own input; PrevHash is included, so each Hash transitively
// binds the entire preceding chain. The result is hex-encoded.
//
// rec is taken by value, so the caller's Hash field is never mutated.
func RecordHash(key []byte, rec Record) (string, error) {
	rec.Hash = "" // a record's hash never covers itself
	input, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("auditlog: marshal for hash: %w", err)
	}
	if len(key) > 0 {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(input)
		return hex.EncodeToString(mac.Sum(nil)), nil
	}
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyChain checks records (in file order) for tamper-evidence: every
// record's Hash must recompute under key, and its PrevHash must equal the
// previous record's Hash. expectedPrev is the Hash the first record must chain
// from ("" for a genesis or standalone file).
//
// It returns the zero-based index of the first broken record, or -1 when the
// chain is fully intact.
func VerifyChain(records []Record, key []byte, expectedPrev string) (int, error) {
	prev := expectedPrev
	for i, rec := range records {
		if rec.PrevHash != prev {
			return i, fmt.Errorf("record %d (%s): prev_hash %q does not chain from %q — reorder or deletion", i, rec.Event, rec.PrevHash, prev)
		}
		want, err := RecordHash(key, rec)
		if err != nil {
			return i, err
		}
		if rec.Hash != want {
			return i, fmt.Errorf("record %d (%s): hash mismatch — content was altered or the integrity key is wrong", i, rec.Event)
		}
		prev = rec.Hash
	}
	return -1, nil
}

// ParseLog reads newline-delimited audit Records from r, in file order.
func ParseLog(r io.Reader) ([]Record, error) {
	var records []Record
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, fmt.Errorf("auditlog: line %d: %w", line, err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// normalizeRecord round-trips rec through JSON so its in-memory Go types (e.g.
// int vs float64 in Resource) match exactly what a verifier reconstructs from
// disk. AppendEvent hashes and writes the normalized form so write-time and
// verify-time hashes are byte-for-byte consistent.
func normalizeRecord(rec Record) (Record, error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return Record{}, err
	}
	var out Record
	if err := json.Unmarshal(b, &out); err != nil {
		return Record{}, err
	}
	return out, nil
}
