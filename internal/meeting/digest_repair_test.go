package meeting

import "testing"

// Meeting 24 (2026-09-03): the roll-up answer stopped at 5,921 bytes mid-array
// on every attempt. The complete part is worth keeping.
func TestRepairTruncatedJSONKeepsTheCompletePartAndClosesTheRest(t *testing.T) {
	cases := map[string]string{
		"array cut inside a string":  `{"topics":["a","b","c`,
		"object cut inside a value":  `{"topics":["a"],"decisions":[{"text":"x","who":"An`,
		"cut inside a key":           `{"topics":["a"],"deci`,
		"cut after a comma":          `{"topics":["a","b"],`,
		"cut inside a nested object": `{"chronology":[{"time":"09:00","text":"start"},{"time":"09:1`,
		"fenced and cut":             "```json\n{\"topics\":[\"a\",\"b\"],\"risks\":[\"r1\"",
		"cut after a number":         `{"count":3,"topics":["a"],"n":12`,
	}
	want := map[string]string{
		"array cut inside a string":  `{"topics":["a","b"]}`,
		"object cut inside a value":  `{"topics":["a"],"decisions":[{"text":"x"}]}`,
		"cut inside a key":           `{"topics":["a"]}`,
		"cut after a comma":          `{"topics":["a","b"]}`,
		"cut inside a nested object": `{"chronology":[{"time":"09:00","text":"start"}]}`,
		"fenced and cut":             `{"topics":["a","b"],"risks":["r1"]}`,
		"cut after a number":         `{"count":3,"topics":["a"]}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := RepairTruncatedJSON(input)
			if !ok {
				t.Fatalf("could not repair %q", input)
			}
			if got != want[name] {
				t.Fatalf("repaired = %s, want %s", got, want[name])
			}
		})
	}
}

func TestRepairTruncatedJSONRefusesWhatIsNotATruncatedDocument(t *testing.T) {
	for name, input := range map[string]string{
		"prose":            "The meeting covered budgets.",
		"complete":         `{"topics":["a"]}`,
		"nothing complete": `{"top`,
		"empty":            "",
	} {
		if got, ok := RepairTruncatedJSON(input); ok {
			t.Fatalf("%s: repaired %q into %s, want refusal", name, input, got)
		}
	}
}
