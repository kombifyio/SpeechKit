package main

import "testing"

func TestResolveBindingForActiveMode(t *testing.T) {
	tests := []struct {
		name       string
		sameCombo  bool
		activeMode string
		binding    string
		want       string
	}{
		{name: "different combos keep dictate", sameCombo: false, activeMode: "dictate", binding: "dictate", want: "dictate"},
		{name: "different combos keep agent", sameCombo: false, activeMode: "dictate", binding: "agent", want: "agent"},
		{name: "same combos route to active dictate", sameCombo: true, activeMode: "dictate", binding: "agent", want: ""},
		{name: "same combos route to active agent", sameCombo: true, activeMode: "agent", binding: "agent", want: "agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveBindingForActiveMode(tt.sameCombo, tt.activeMode, tt.binding); got != tt.want {
				t.Fatalf("resolveBindingForActiveMode(%v, %q, %q) = %q, want %q", tt.sameCombo, tt.activeMode, tt.binding, got, tt.want)
			}
		})
	}
}
