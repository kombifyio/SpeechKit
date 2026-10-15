package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// policy_keys.go holds the per-kind behaviour behind the policyKeys table in
// policy.go: reading a value from a source, the HKCU trust rule, precedence,
// the refresh comparison and the config pin. Adding a key never touches this
// file.

// policySource is one place policy values are read from: a registry root on
// Windows, the managed-preferences plist on macOS. key is the canonical
// `Section\Value` name. An absent value returns its zero result and no error;
// a present value of the wrong type returns an error.
type policySource interface {
	boolValue(key string) (*bool, error)
	intValue(key string) (*int, error)
	// stringValue returns the trimmed value; "" means not set.
	stringValue(key string) (string, error)
	// listValue returns nil when absent and a non-nil (possibly empty) slice
	// when present.
	listValue(key string) ([]string, error)
}

// errPolicyType is returned by a source for a value of the wrong type.
var errPolicyType = errors.New("unexpected value type")

type userRule int

const (
	// machineOnly ignores (and counts) every HKCU value for the key.
	machineOnly userRule = iota
	// userMayRestrict accepts the restrictive value of a boolean from HKCU.
	userMayRestrict
	// userMayTighten accepts a value from HKCU only when the key's tighter
	// rule says it is stricter than the machine value (or than no pin).
	userMayTighten
)

type userMergeResult int

const (
	userNone userMergeResult = iota
	userContributed
	userIgnored
)

// refreshVerdict says how a newly read value relates to the bound one.
type refreshVerdict struct {
	tightened bool // applied at once
	deferred  bool // waits for the next start
}

type policyOps interface {
	load(src policySource, key string, p *PolicyValues) error
	set(p PolicyValues) bool
	take(dst *PolicyValues, src PolicyValues)
	same(a, b PolicyValues) bool
	userMerge(result *PolicyValues, user PolicyValues) userMergeResult
	// refresh decides cur -> next for a running process and writes what may
	// apply at once into out (a copy of cur).
	refresh(cur, next PolicyValues, cfg *Config, out *PolicyValues) refreshVerdict
	apply(c *Config, p PolicyValues)
	cfgEqual(a, b *Config) bool
	cfgCopy(dst, src *Config)
}

type policyKey struct {
	name  string // `Section\Value`
	field string // config key the pin locks
	ops   policyOps
	pin   *policyField // set for derived pins; nil pins ops' config field
}

func buildPolicyFields(keys []policyKey) []policyField {
	fields := make([]policyField, 0, len(keys))
	for _, k := range keys {
		if k.pin != nil {
			fields = append(fields, *k.pin)
			continue
		}
		fields = append(fields, policyField{
			name:   k.field,
			pinned: k.ops.set,
			apply:  k.ops.apply,
			equal:  k.ops.cfgEqual,
			copy:   k.ops.cfgCopy,
		})
	}
	return fields
}

// ── booleans and ints ──────────────────────────────────────────────────────

// ptrPolicy is a DWORD-style value. restrictive is the value that only ever
// narrows what SpeechKit does (nil: no direction, any change is neutral).
type ptrPolicy[T comparable] struct {
	slot        func(*PolicyValues) **T
	target      func(*Config) *T // nil for derived pins
	restrictive *T
	user        userRule
	read        func(policySource, string) (*T, error)
	// valid rejects a value at read time (nil: any value is accepted).
	valid func(T) error
	// tighter reports whether next is strictly stricter than cur (nil: the
	// key has no ordering besides restrictive).
	tighter func(cur, next T) bool
}

// withIntRules adds read-time validation and a strictness order to an int
// key. The order also lets HKCU contribute a stricter value.
func (k policyKey) withIntRules(valid func(int) error, tighter func(cur, next int) bool) policyKey {
	o, ok := k.ops.(ptrPolicy[int])
	if !ok {
		panic("config: withIntRules on a key that is not an int key: " + k.name)
	}
	o.valid, o.tighter, o.user = valid, tighter, userMayTighten
	k.ops = o
	return k
}

func boolKey(name, field string, slot func(*PolicyValues) **bool, target func(*Config) *bool, restrictive bool, user userRule) policyKey {
	return policyKey{name: name, field: field, ops: ptrPolicy[bool]{
		slot: slot, target: target, restrictive: &restrictive, user: user,
		read: policySource.boolValue,
	}}
}

// derivedBoolKey is a boolean whose pin sets a config value other than a
// plain copy (EnforceLocalOnly pins the routing strategy).
func derivedBoolKey(name string, slot func(*PolicyValues) **bool, restrictive bool, pin policyField) policyKey {
	return policyKey{name: name, field: pin.name, pin: &pin, ops: ptrPolicy[bool]{
		slot: slot, restrictive: &restrictive, user: userMayRestrict,
		read: policySource.boolValue,
	}}
}

func intKey(name, field string, slot func(*PolicyValues) **int, target func(*Config) *int) policyKey {
	return policyKey{name: name, field: field, ops: ptrPolicy[int]{
		slot: slot, target: target, user: machineOnly,
		read: policySource.intValue,
	}}
}

func (o ptrPolicy[T]) load(src policySource, key string, p *PolicyValues) error {
	v, err := o.read(src, key)
	if err != nil {
		return err
	}
	if v != nil && o.valid != nil {
		if err := o.valid(*v); err != nil {
			return err
		}
	}
	if v != nil {
		*o.slot(p) = v
	}
	return nil
}

func (o ptrPolicy[T]) get(p PolicyValues) *T { return *o.slot(&p) }

func (o ptrPolicy[T]) set(p PolicyValues) bool { return o.get(p) != nil }

func (o ptrPolicy[T]) take(dst *PolicyValues, src PolicyValues) { *o.slot(dst) = o.get(src) }

func (o ptrPolicy[T]) same(a, b PolicyValues) bool {
	x, y := o.get(a), o.get(b)
	if x == nil || y == nil {
		return x == y
	}
	return *x == *y
}

func (o ptrPolicy[T]) isRestrictive(v *T) bool {
	return v != nil && o.restrictive != nil && *v == *o.restrictive
}

func (o ptrPolicy[T]) userMerge(result *PolicyValues, user PolicyValues) userMergeResult {
	v := o.get(user)
	if v == nil {
		return userNone
	}
	if o.user == userMayTighten && o.tighter != nil {
		var cur T
		if m := o.get(*result); m != nil {
			cur = *m
		}
		if !o.tighter(cur, *v) {
			return userIgnored
		}
		*o.slot(result) = v
		return userContributed
	}
	if o.user != userMayRestrict || !o.isRestrictive(v) {
		return userIgnored
	}
	if o.set(*result) {
		return userNone // the machine value takes precedence
	}
	*o.slot(result) = v
	return userContributed
}

func (o ptrPolicy[T]) refresh(cur, next PolicyValues, cfg *Config, out *PolicyValues) refreshVerdict {
	if o.same(cur, next) {
		return refreshVerdict{}
	}
	c, n := o.get(cur), o.get(next)
	if n == nil {
		return refreshVerdict{deferred: true}
	}
	// The restrictive value applies at once; a new pin of the value the
	// config already holds only locks it.
	if o.isRestrictive(n) || (c == nil && o.target != nil && *o.target(cfg) == *n) || o.stricterThanBound(c, n, cfg) {
		*o.slot(out) = n
		return refreshVerdict{tightened: true}
	}
	return refreshVerdict{deferred: true}
}

// stricterThanBound reports whether n is stricter than the value in force:
// the bound pin, or the configured value when nothing is pinned yet.
func (o ptrPolicy[T]) stricterThanBound(c, n *T, cfg *Config) bool {
	if o.tighter == nil {
		return false
	}
	switch {
	case c != nil:
		return o.tighter(*c, *n)
	case o.target != nil:
		return o.tighter(*o.target(cfg), *n)
	default:
		return false
	}
}

func (o ptrPolicy[T]) apply(c *Config, p PolicyValues) { *o.target(c) = *o.get(p) }
func (o ptrPolicy[T]) cfgEqual(a, b *Config) bool      { return *o.target(a) == *o.target(b) }
func (o ptrPolicy[T]) cfgCopy(dst, src *Config)        { *o.target(dst) = *o.target(src) }

// ── strings ────────────────────────────────────────────────────────────────

// stringRules configures a string key. Strings are machine-only.
type stringRules struct {
	// order lists the values from least to most strict. Empty: neutral, a
	// changed value is neither tighter nor looser.
	order []string
	// strictness ranks a value (higher is stricter) for a key whose values
	// cannot be listed in order, such as a URL. It replaces order.
	strictness func(string) int
	// validate normalizes a value or rejects it.
	validate func(string) (string, error)
	// invalid is the value a rejected entry fails closed to; "" drops it.
	invalid string
	// userMay is the one value the user hive (HKCU) may set, and only when no
	// machine value is set. "" means strings are machine-only.
	userMay string
}

type stringPolicy struct {
	slot   func(*PolicyValues) *string
	target func(*Config) *string
	rules  stringRules
}

func stringKey(name, field string, slot func(*PolicyValues) *string, target func(*Config) *string, rules stringRules) policyKey {
	return policyKey{name: name, field: field, ops: stringPolicy{slot: slot, target: target, rules: rules}}
}

func (o stringPolicy) load(src policySource, key string, p *PolicyValues) error {
	v, err := src.stringValue(key)
	if err != nil || v == "" {
		return err
	}
	if o.rules.validate != nil {
		normalized, verr := o.rules.validate(v)
		if verr != nil {
			if o.rules.invalid != "" {
				*o.slot(p) = o.rules.invalid
				return fmt.Errorf("%w (failed closed to %q)", verr, o.rules.invalid)
			}
			return verr
		}
		v = normalized
	}
	if len(o.rules.order) > 0 && o.rank(v) < 0 {
		return fmt.Errorf("unknown value; expected one of %s", strings.Join(o.rules.order, ", "))
	}
	*o.slot(p) = v
	return nil
}

func (o stringPolicy) get(p PolicyValues) string { return *o.slot(&p) }
func (o stringPolicy) set(p PolicyValues) bool   { return o.get(p) != "" }
func (o stringPolicy) take(dst *PolicyValues, src PolicyValues) {
	*o.slot(dst) = o.get(src)
}
func (o stringPolicy) same(a, b PolicyValues) bool { return o.get(a) == o.get(b) }

func (o stringPolicy) userMerge(result *PolicyValues, user PolicyValues) userMergeResult {
	if !o.set(user) {
		return userNone
	}
	if o.rules.userMay == "" || !strings.EqualFold(o.get(user), o.rules.userMay) {
		return userIgnored
	}
	if o.set(*result) {
		return userNone // the machine value takes precedence
	}
	*o.slot(result) = o.rules.userMay
	return userContributed
}

// rank is the strictness of v in order, -1 when unknown.
func (o stringPolicy) rank(v string) int {
	v = strings.TrimSpace(v)
	if o.rules.validate != nil {
		if normalized, err := o.rules.validate(v); err == nil {
			v = normalized
		}
	}
	if o.rules.strictness != nil {
		return o.rules.strictness(v)
	}
	for i, candidate := range o.rules.order {
		if strings.EqualFold(candidate, v) {
			return i
		}
	}
	return -1
}

func (o stringPolicy) refresh(cur, next PolicyValues, cfg *Config, out *PolicyValues) refreshVerdict {
	if o.same(cur, next) {
		return refreshVerdict{}
	}
	if !o.set(next) {
		return refreshVerdict{deferred: true}
	}
	n := o.get(next)
	var tighter bool
	switch {
	case o.ordered() && o.set(cur):
		tighter = o.rank(n) > o.rank(o.get(cur))
	case o.ordered():
		// A new pin applies at once when it is at least as strict as what
		// the user configured.
		tighter = o.rank(n) >= o.rank(*o.target(cfg))
	default:
		// A neutral value (an endpoint, an ID) only applies at once when it
		// locks what the config already holds; a different value would
		// change where SpeechKit connects during the process lifetime.
		tighter = !o.set(cur) && *o.target(cfg) == n
	}
	if !tighter {
		return refreshVerdict{deferred: true}
	}
	*o.slot(out) = n
	return refreshVerdict{tightened: true}
}

func (o stringPolicy) apply(c *Config, p PolicyValues) { *o.target(c) = o.get(p) }
func (o stringPolicy) cfgEqual(a, b *Config) bool      { return *o.target(a) == *o.target(b) }
func (o stringPolicy) cfgCopy(dst, src *Config)        { *o.target(dst) = *o.target(src) }

// ── lists ──────────────────────────────────────────────────────────────────

// listPolicy is a list of identifiers. HKCU may only remove entries from the
// machine list (set intersection); a user list without a machine list is
// ignored. Entries compare case-insensitively and are stored lower-case.
//
// validate, when set, checks a read list; a rejected list is stored as
// failClosed and the read reports the error. failClosed is also what an empty
// pin applies as (an HKCU intersection can empty it), so a key whose empty
// list would mean "the default" cannot fall back to it.
//
// A deny list (denyOnly) inverts the direction: every entry narrows what
// SpeechKit does, so HKCU may only add entries (set union, also without a
// machine list) and a refresh applies added entries at once while a removed
// entry waits for the next start.
type listPolicy struct {
	slot       func(*PolicyValues) *[]string
	target     func(*Config) *[]string
	validate   func([]string) ([]string, error)
	failClosed []string
	denyOnly   bool
}

func listKey(name, field string, slot func(*PolicyValues) *[]string, target func(*Config) *[]string) policyKey {
	return policyKey{name: name, field: field, ops: listPolicy{slot: slot, target: target}}
}

// denyListKey is a list key whose entries each forbid something (see
// listPolicy).
func denyListKey(name, field string, slot func(*PolicyValues) *[]string, target func(*Config) *[]string) policyKey {
	return policyKey{name: name, field: field, ops: listPolicy{slot: slot, target: target, denyOnly: true}}
}

func (o listPolicy) load(src policySource, key string, p *PolicyValues) error {
	v, err := src.listValue(key)
	if err != nil || v == nil {
		return err
	}
	if o.validate != nil {
		checked, verr := o.validate(v)
		if verr != nil {
			*o.slot(p) = slices.Clone(o.failClosed)
			return fmt.Errorf("%w (failed closed to %v)", verr, o.failClosed)
		}
		v = checked
	}
	*o.slot(p) = normalizePolicyList(v)
	return nil
}

func (o listPolicy) get(p PolicyValues) []string { return *o.slot(&p) }
func (o listPolicy) set(p PolicyValues) bool     { return o.get(p) != nil }
func (o listPolicy) take(dst *PolicyValues, src PolicyValues) {
	*o.slot(dst) = slices.Clone(o.get(src))
}
func (o listPolicy) same(a, b PolicyValues) bool {
	x, y := o.get(a), o.get(b)
	if (x == nil) != (y == nil) {
		return false
	}
	return sameSet(x, y)
}

func (o listPolicy) userMerge(result *PolicyValues, user PolicyValues) userMergeResult {
	u := o.get(user)
	if u == nil {
		return userNone
	}
	if o.denyOnly {
		machine := o.get(*result)
		merged := unionPolicyList(machine, u)
		if machine != nil && len(merged) == len(machine) {
			return userNone // nothing the machine list does not already hold
		}
		*o.slot(result) = merged
		return userContributed
	}
	machine := o.get(*result)
	if machine == nil {
		return userIgnored
	}
	*o.slot(result) = intersectPolicyList(machine, u)
	return userNone
}

func (o listPolicy) refresh(cur, next PolicyValues, _ *Config, out *PolicyValues) refreshVerdict {
	if o.same(cur, next) {
		return refreshVerdict{}
	}
	c, n := o.get(cur), o.get(next)
	if n == nil {
		return refreshVerdict{deferred: true}
	}
	if c == nil {
		// A new allowlist only narrows what was allowed before; a new deny
		// list only adds to what is refused.
		*o.slot(out) = slices.Clone(n)
		return refreshVerdict{tightened: true}
	}
	if o.denyOnly {
		// Entries added to a deny list apply at once. An entry the new read
		// dropped stays refused until the next start.
		merged := unionPolicyList(c, n)
		*o.slot(out) = merged
		return refreshVerdict{tightened: len(merged) > len(c), deferred: len(merged) > len(n)}
	}
	narrowed := intersectPolicyList(c, n)
	verdict := refreshVerdict{
		tightened: len(narrowed) < len(c),
		deferred:  len(narrowed) < len(n),
	}
	*o.slot(out) = narrowed
	return verdict
}

func (o listPolicy) apply(c *Config, p PolicyValues) {
	if o.denyOnly {
		// The pinned entries add to the user's own: a deny list never
		// loosens what config.toml already refuses.
		*o.target(c) = unionPolicyList(*o.target(c), o.get(p))
		return
	}
	v := slices.Clone(o.get(p))
	if len(v) == 0 && o.failClosed != nil {
		v = slices.Clone(o.failClosed)
	}
	*o.target(c) = v
}
func (o listPolicy) cfgEqual(a, b *Config) bool { return sameSet(*o.target(a), *o.target(b)) }
func (o listPolicy) cfgCopy(dst, src *Config)   { *o.target(dst) = slices.Clone(*o.target(src)) }

// splitPolicyList splits a REG_SZ list on semicolons. A present value always
// yields a non-nil slice, so an empty string is an empty list, not "unset".
func splitPolicyList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ";") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizePolicyList(in []string) []string {
	out := []string{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// unionPolicyList returns the entries of base followed by the entries of other
// that base lacks. The result is non-nil.
func unionPolicyList(base, other []string) []string {
	out := normalizePolicyList(base)
	for _, v := range normalizePolicyList(other) {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// intersectPolicyList keeps the entries of base that also occur in other, in
// base's order. The result is non-nil.
func intersectPolicyList(base, other []string) []string {
	out := []string{}
	for _, v := range base {
		if slices.ContainsFunc(other, func(o string) bool { return strings.EqualFold(o, v) }) {
			out = append(out, v)
		}
	}
	return out
}

// sameSet compares two lists as case-insensitive sets.
func sameSet(a, b []string) bool {
	x, y := normalizePolicyList(a), normalizePolicyList(b)
	return len(x) == len(y) && len(intersectPolicyList(x, y)) == len(x)
}

// ordered reports whether the key's values have a strictness order.
func (o stringPolicy) ordered() bool { return len(o.rules.order) > 0 || o.rules.strictness != nil }

// ── catalog ────────────────────────────────────────────────────────────────

// PolicyKind is the value type of a policy key.
type PolicyKind string

const (
	PolicyKindBool   PolicyKind = "bool"   // DWORD 0 or 1
	PolicyKindInt    PolicyKind = "int"    // DWORD
	PolicyKindString PolicyKind = "string" // REG_SZ
	PolicyKindList   PolicyKind = "list"   // REG_MULTI_SZ, or REG_SZ separated by semicolons
)

// PolicyUserRule says what the per-user hive (HKCU) may contribute to a key.
type PolicyUserRule string

const (
	// PolicyUserNone: every HKCU value is ignored.
	PolicyUserNone PolicyUserRule = "machine-only"
	// PolicyUserRestrict: HKCU may set the restrictive value of a boolean.
	PolicyUserRestrict PolicyUserRule = "restrict"
	// PolicyUserTighten: HKCU may set a value stricter than the machine value.
	PolicyUserTighten PolicyUserRule = "tighten"
	// PolicyUserIntersect: HKCU may only remove entries from the machine list.
	PolicyUserIntersect PolicyUserRule = "intersect"
	// PolicyUserAdd: HKCU may only add entries to a deny list, never remove one.
	PolicyUserAdd PolicyUserRule = "add"
	// PolicyUserSet: HKCU may set the one value in UserValue, and only when no
	// machine value exists.
	PolicyUserSet PolicyUserRule = "set"
)

// PolicyKeySpec describes one policy key for tools that document the policy
// surface (the ADMX generator, the runbook). It is read-only metadata derived
// from the policyKeys table; the table stays the single source.
type PolicyKeySpec struct {
	// Name is the canonical `Section\Value` registry name, Section and Value
	// its parts.
	Name    string
	Section string
	Value   string
	// ID is Name without the separator (`Update\Enabled` is UpdateEnabled): the
	// stable identifier documentation entries are keyed by.
	ID string
	// ConfigKey is the config key the pin locks.
	ConfigKey string
	Kind      PolicyKind
	// Restrictive is the boolean value that only narrows what the product
	// does; nil for other kinds.
	Restrictive *bool
	// Values lists the accepted values of a string key from the least to the
	// most strict; nil when the key takes free text.
	Values []string
	// Validated is true when the value is normalized or rejected by a
	// validator at read time.
	Validated bool
	UserRule  PolicyUserRule
	// UserValue is the one value HKCU may set when UserRule is PolicyUserSet.
	UserValue string
}

// PolicyCatalog returns one entry per policy key, in table order. The result
// is a copy; changing it changes nothing.
func PolicyCatalog() []PolicyKeySpec {
	out := make([]PolicyKeySpec, 0, len(policyKeys))
	for _, k := range policyKeys {
		spec := PolicyKeySpec{Name: k.name, ConfigKey: k.field}
		spec.Section, spec.Value, _ = strings.Cut(k.name, `\`)
		spec.ID = spec.Section + spec.Value
		switch o := k.ops.(type) {
		case ptrPolicy[bool]:
			spec.Kind = PolicyKindBool
			if o.restrictive != nil {
				v := *o.restrictive
				spec.Restrictive = &v
			}
			spec.UserRule = catalogUserRule(o.user)
			spec.Validated = o.valid != nil
		case ptrPolicy[int]:
			spec.Kind = PolicyKindInt
			spec.UserRule = catalogUserRule(o.user)
			spec.Validated = o.valid != nil
		case stringPolicy:
			spec.Kind = PolicyKindString
			spec.Values = slices.Clone(o.rules.order)
			spec.Validated = o.rules.validate != nil
			spec.UserRule = PolicyUserNone
			if o.rules.userMay != "" {
				spec.UserRule, spec.UserValue = PolicyUserSet, o.rules.userMay
			}
		case listPolicy:
			spec.Kind = PolicyKindList
			spec.UserRule = PolicyUserIntersect
			if o.denyOnly {
				spec.UserRule = PolicyUserAdd
			}
			spec.Validated = o.validate != nil
		default:
			panic("config: policy key with an unknown kind: " + k.name)
		}
		out = append(out, spec)
	}
	return out
}

func catalogUserRule(r userRule) PolicyUserRule {
	switch r {
	case userMayRestrict:
		return PolicyUserRestrict
	case userMayTighten:
		return PolicyUserTighten
	default:
		return PolicyUserNone
	}
}
