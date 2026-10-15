package auditlog

import "time"

// Head is the position of the audit chain: the sequence number and chain hash
// of the newest record in a log directory and when it was written. It carries
// no record content, so it can leave the device in a support bundle; compared
// with the forwarded copy it shows whether the local log was cut.
type Head struct {
	Seq  uint64
	Hash string
	Time time.Time
}

// ReadHead returns the head of the audit log in dir. ok is false when dir
// holds no readable record.
func ReadHead(dir string) (Head, bool) {
	rec, ok := newestRecordInDir(dir)
	if !ok {
		return Head{}, false
	}
	return Head{Seq: rec.Seq, Hash: rec.Hash, Time: rec.Timestamp}, true
}
