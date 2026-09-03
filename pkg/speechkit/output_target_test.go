package speechkit

import "testing"

type hostWindowTarget struct{ handle uintptr }

func (hostWindowTarget) TargetKind() string { return TargetKindWindow }

// Hosts route on the kind of whatever the pipeline hands them; nil, host
// targets, TargetRef values and pre-contract untyped values must all classify
// without a panic or a type assertion on host internals.
func TestTargetKindClassifiesHostTargets(t *testing.T) {
	cases := map[string]struct {
		target any
		want   string
	}{
		"nil is the default destination":    {target: nil, want: TargetKindNone},
		"host type reports its kind":        {target: hostWindowTarget{handle: 42}, want: TargetKindWindow},
		"TargetRef reports its kind":        {target: TargetRef{Kind: TargetKindClipboard}, want: TargetKindClipboard},
		"product kinds pass through":        {target: TargetRef{Kind: "companion.chat", ID: "general"}, want: "companion.chat"},
		"legacy untyped values are flagged": {target: "editor", want: "opaque"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := TargetKind(tc.target); got != tc.want {
				t.Fatalf("TargetKind(%#v) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}
