package config

import (
	"errors"
	"slices"
	"sync"
)

// Cause says why the shared configuration changed.
type Cause int

const (
	// CauseUserEdit is an edit made through the settings, a route or a job
	// acting for the user. It is refused when it changes a value the policy
	// pins (CheckPolicyEdit).
	CauseUserEdit Cause = iota
	// CausePolicyRefresh is a running re-read of the machine policy that
	// tightened something (RefreshPolicy). It is the policy itself, so no
	// policy-edit check applies.
	CausePolicyRefresh
	// CauseStartup is a derivation the composition root makes while it
	// starts. The bound policy is re-applied after it.
	CauseStartup
)

func (c Cause) String() string {
	switch c {
	case CauseUserEdit:
		return "user_edit"
	case CausePolicyRefresh:
		return "policy_refresh"
	case CauseStartup:
		return "startup"
	}
	return "unknown"
}

// Change is one committed change of the shared configuration. Old and New
// are copies a subscriber may keep; neither is the live value.
type Change struct {
	Old, New *Config
	Cause    Cause
}

// PolicyKeysChanged names the policy-pinnable config keys (dotted TOML
// paths, as PolicyLockedFields names them) whose value differs between Old
// and New.
func (c Change) PolicyKeysChanged() []string {
	if c.Old == nil || c.New == nil {
		return nil
	}
	var keys []string
	for _, f := range policyFields {
		if !f.equal(c.Old, c.New) && !slices.Contains(keys, f.name) {
			keys = append(keys, f.name)
		}
	}
	return keys
}

// Shared owns the configuration a running host shares between its handlers,
// jobs and watchers. Every write goes through it: Update applies an edit to a
// clone of the current value under the write lock, so an edit can never undo
// a policy tightening that landed while the caller prepared it, and a failed
// edit leaves the current value untouched. Readers take a Snapshot.
//
// The live *Config handed to NewShared stays the same pointer for the life
// of the owner: a commit writes the new value into it. Code that still reads
// that pointer directly sees every committed change (without the lock); it
// must not write it.
type Shared struct {
	mu   sync.RWMutex
	path string
	cur  *Config

	subMu  sync.Mutex
	subSeq int
	subs   []subscription
}

type subscription struct {
	id int
	fn func(Change)
}

// NewShared makes s the owner of cfg, saved to path ("" is the default path
// next to the executable). cfg must not be nil.
func NewShared(path string, cfg *Config) *Shared {
	if cfg == nil {
		panic("config: NewShared needs a config")
	}
	return &Shared{path: path, cur: cfg}
}

// Path is the file Save writes.
func (s *Shared) Path() string { return s.path }

// Snapshot returns a deep copy of the current configuration, safe to keep
// and to read while the shared value changes. A nil owner has none.
func (s *Shared) Snapshot() *Config {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur.Clone()
}

// Update runs fn on a clone of the current configuration and commits the
// result. When fn fails, or the result changes a value the bound policy (or a
// managed host) refuses, nothing changes and the error is returned; a refused
// edit is a *PolicyLockedError. Subscribers run after the commit, outside the
// lock, on the caller's goroutine. fn must not call back into s.
func (s *Shared) Update(fn func(*Config) error) (Change, error) {
	return s.commit(CauseUserEdit, fn, false)
}

// UpdateAndSave is Update that also writes the result to the config file
// before it commits it: when the write fails, nothing changes.
func (s *Shared) UpdateAndSave(fn func(*Config) error) (Change, error) {
	return s.commit(CauseUserEdit, fn, true)
}

// UpdateFrom commits an edit a caller prepared on edited, a changed copy of
// base (a Snapshot), for writers that build runtimes from the edited value
// before they commit it. Every setting where edited differs from base is
// copied onto the current configuration; every other setting keeps its
// current value, so a change committed in the meantime (a policy refresh
// included) is kept. Otherwise it is Update.
func (s *Shared) UpdateFrom(base, edited *Config) (Change, error) {
	return s.Update(func(c *Config) error {
		mergeEdited(c, base, edited)
		return nil
	})
}

// UpdateAndSaveFrom is UpdateFrom that also writes the result to the config
// file, as UpdateAndSave does.
func (s *Shared) UpdateAndSaveFrom(base, edited *Config) (Change, error) {
	return s.UpdateAndSave(func(c *Config) error {
		mergeEdited(c, base, edited)
		return nil
	})
}

// UpdateAtStartup commits a derivation of the composition root (CauseStartup)
// without the policy-edit check; the bound policy is re-applied after fn.
func (s *Shared) UpdateAtStartup(fn func(*Config) error) (Change, error) {
	return s.commit(CauseStartup, fn, false)
}

// Save writes the current configuration to the config file. It takes the
// write lock because writing normalises the value in place.
func (s *Shared) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return save(s.path, s.cur)
}

// RefreshPolicy applies next, a fresh read of the machine policy, through the
// owner (see the package function RefreshPolicy): a tightening is committed
// with CausePolicyRefresh and its subscribers run, a loosening waits for the
// next start. The policy-edit check does not apply: a refresh is the policy
// itself.
func (s *Shared) RefreshPolicy(next PolicyValues) PolicyRefresh {
	var refresh PolicyRefresh
	_, _ = s.commit(CausePolicyRefresh, func(c *Config) error {
		refresh = RefreshPolicy(c, next)
		if len(refresh.Tightened) == 0 {
			return errNothingTightened
		}
		return nil
	}, false)
	return refresh
}

// errNothingTightened ends a refresh commit that has nothing to apply.
var errNothingTightened = errors.New("config: the policy refresh tightened nothing")

// Subscribe registers fn to run after every committed change, in the order
// of subscription, on the goroutine of the writer and outside the lock. The
// returned func removes it.
func (s *Shared) Subscribe(fn func(Change)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	s.subMu.Lock()
	s.subSeq++
	id := s.subSeq
	s.subs = append(s.subs, subscription{id: id, fn: fn})
	s.subMu.Unlock()
	return func() {
		s.subMu.Lock()
		s.subs = slices.DeleteFunc(s.subs, func(sub subscription) bool { return sub.id == id })
		s.subMu.Unlock()
	}
}

func (s *Shared) commit(cause Cause, fn func(*Config) error, persist bool) (Change, error) {
	s.mu.Lock()
	next := s.cur.Clone()
	if err := fn(next); err != nil {
		s.mu.Unlock()
		return Change{}, err
	}
	switch cause {
	case CauseUserEdit:
		if err := s.cur.CheckPolicyEdit(next); err != nil {
			s.mu.Unlock()
			return Change{}, err
		}
	case CauseStartup:
		EnforcePolicy(next)
	case CausePolicyRefresh:
	}
	if persist {
		if err := save(s.path, next); err != nil {
			s.mu.Unlock()
			return Change{}, err
		}
	}
	old := *s.cur
	*s.cur = *next.Clone()
	s.mu.Unlock()

	change := Change{Old: &old, New: next, Cause: cause}
	s.notify(change)
	return change, nil
}

func (s *Shared) notify(change Change) {
	s.subMu.Lock()
	subs := slices.Clone(s.subs)
	s.subMu.Unlock()
	for _, sub := range subs {
		sub.fn(change)
	}
}
