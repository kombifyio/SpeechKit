package config

import "sort"

// PolicyOriginRejected is the origin PolicySnapshot reports for a key whose
// value was present but not applied.
const PolicyOriginRejected = "rejected"

// PolicyEntry is one policy key in force and where its value came from.
type PolicyEntry struct {
	Key    string
	Origin string
}

// Snapshot lists the policy keys this overlay sets (and the ones it rejected)
// with their sources, sorted by key. It carries no values: a value can be an
// endpoint, a path or a tenant, and the snapshot is what a support bundle
// shows.
func (p PolicyValues) Snapshot() []PolicyEntry {
	out := make([]PolicyEntry, 0, len(p.origins)+len(p.Rejected))
	for key, origin := range p.origins {
		out = append(out, PolicyEntry{Key: key, Origin: origin})
	}
	for _, r := range p.Rejected {
		out = append(out, PolicyEntry{Key: r.Key, Origin: PolicyOriginRejected})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
