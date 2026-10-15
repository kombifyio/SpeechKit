package productid

import (
	"errors"
	"testing"
)

// resetRegistration gives Register a clean slate and restores it after.
func resetRegistration(t *testing.T) {
	t.Helper()
	mu.Lock()
	prev := registered
	registered = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		registered = prev
		mu.Unlock()
	})
}

// The identity is set once: the same value again is accepted, another product
// is refused and the first one stays in force.
func TestRegisterSetsTheIdentityOnce(t *testing.T) {
	resetRegistration(t)
	if err := Register(Reference); err != nil {
		t.Fatal(err)
	}
	if err := Register(Reference); err != nil {
		t.Fatalf("registering the same identity again: %v", err)
	}
	other := Reference
	other.Vendor, other.Product, other.DataDirName = "Contoso", "Notes", "Notes"
	if err := Register(other); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("a second, different identity: want ErrAlreadyRegistered, got %v", err)
	}
	got, err := Current()
	if err != nil || got.Product != Reference.Product || got.DataDirName != Reference.DataDirName {
		t.Fatalf("Current = %+v, %v; want the first registration", got, err)
	}
}

// A name that would leave its registry key, directory or object namespace is
// refused before anything can build a path from it.
func TestRegisterRefusesUnsafeNames(t *testing.T) {
	resetRegistration(t)
	cases := map[string]func(*Product){
		"vendor with a separator":  func(p *Product) { p.Vendor = `kombify\..\Policies` },
		"parent data directory":    func(p *Product) { p.DataDirName = ".." },
		"empty record prefix":      func(p *Product) { p.RecordPrefix = "" },
		"bundle id with a slash":   func(p *Product) { p.BundleID = "com.example/../x" },
		"lower-case env prefix":    func(p *Product) { p.EnvPrefix = "speechkit" },
		"plain-http feedback link": func(p *Product) { p.FeedbackURL = "http://example.com" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := Reference
			mutate(&p)
			if err := Register(p); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

// The file stems the host derives scratch and runner names from.
func TestExecutableStems(t *testing.T) {
	if got := Reference.ExecutableStem(); got != "SpeechKit" {
		t.Errorf("ExecutableStem = %q", got)
	}
	if got := Reference.FileStem(); got != "speechkit" {
		t.Errorf("FileStem = %q", got)
	}
}
