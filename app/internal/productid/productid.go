// Package productid holds the one product identity a process runs as. Every
// name the host writes into the operating system comes from it: policy and
// registry roots, the macOS preference domain and Keychain service, data
// directory names, single-instance names, token-cache and secret-record
// prefixes, the update target and the display name.
//
// The composition root registers the identity once, before anything reads
// it. A reader in a process that registered none gets ErrNoIdentity and fails
// closed: no shared default name, no relative path, no policy read as "none".
// Test binaries get ForTests automatically, so a package test never needs a
// root of its own and never touches the names of a real install.
package productid

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"
)

// ErrNoIdentity is returned by every identity reader in a process whose
// composition root registered no product identity.
var ErrNoIdentity = errors.New("productid: no product identity registered")

// ErrInvalid is returned by Register for an identity a registry path, a file
// name or an object name cannot safely be built from.
var ErrInvalid = errors.New("productid: invalid product identity")

// ErrAlreadyRegistered is returned by Register when a different identity was
// registered before. Registering the same identity again is a no-op.
var ErrAlreadyRegistered = errors.New("productid: a different product identity is already registered")

// Icons are the images the desktop shell shows. All are optional; a process
// without a tray or window (server, CLI) leaves them empty.
type Icons struct {
	// App is the window and taskbar icon (ICO on Windows, PNG elsewhere).
	App []byte
	// TrayIdle is the colour tray icon on Windows and Linux.
	TrayIdle []byte
	// TrayTemplate is the monochrome macOS menu-bar template image.
	TrayTemplate []byte
}

// Product is one product identity.
type Product struct {
	// Vendor and Product name the registry roots
	// HKLM\SOFTWARE\Policies\<Vendor>\<Product>, HKLM\SOFTWARE\<Vendor>\<Product>
	// and HKCU\Software\<Vendor>\<Product>.
	Vendor  string
	Product string
	// BundleID is the reverse-DNS macOS preference domain, Keychain service
	// and LaunchAgent label.
	BundleID string
	// AppName is the short name users see: tray strings, window titles, the
	// Windows Run value. DisplayName adds the vendor.
	AppName string
	// DataDirName is the directory under %APPDATA%, %LOCALAPPDATA% and
	// ~/Library/Application Support.
	DataDirName string
	// SingleInstanceID names the single-instance lock and the show-UI event.
	SingleInstanceID string
	// CachePrefix starts every Entra token-cache name; RecordPrefix every
	// secret-store record name.
	CachePrefix  string
	RecordPrefix string
	// ExecutableName is the file the updater installs and restarts.
	ExecutableName string
	// EnvPrefix starts the environment overrides (<EnvPrefix>_LOG_LEVEL).
	// Empty means the product reads no such overrides.
	EnvPrefix string
	// FeedbackURL is the tray's feedback link; empty hides the item.
	FeedbackURL string
	Icons       Icons
}

// Reference is the identity of the reference client and of the server, CLI,
// MCP and tool binaries that share its data. The names are the ones every
// earlier release used, so existing installs, token caches, secret records,
// the Keychain item and the data directory stay valid.
var Reference = Product{
	Vendor:           "kombify",
	Product:          "SpeechKit",
	BundleID:         "com.kombify.speechkit",
	AppName:          "SpeechKit",
	DataDirName:      "SpeechKit",
	SingleInstanceID: "com.kombify.speechkit",
	CachePrefix:      "kombify-speechkit",
	RecordPrefix:     "SPEECHKIT",
	ExecutableName:   "SpeechKit.exe",
	EnvPrefix:        "SPEECHKIT",
	FeedbackURL:      "https://github.com/kombifyio/SpeechKit/issues",
}

// ForTests is the identity a test binary runs as when its test registered
// none. Every name that reaches shared operating-system state (registry
// roots, preference domain, Keychain, single-instance objects, token caches,
// secret records) differs from Reference, so a test never reads the
// machine's real policy or touches a real install's sign-in. The data
// directory, display names and environment prefix match Reference, because
// tests redirect the data root through the environment and assert on it.
var ForTests = Product{
	Vendor:           "kombify-test",
	Product:          "SpeechKitTest",
	BundleID:         "com.kombify.speechkit.test",
	AppName:          "SpeechKit",
	DataDirName:      "SpeechKit",
	SingleInstanceID: "com.kombify.speechkit.test",
	CachePrefix:      "speechkit-test",
	RecordPrefix:     "SPEECHKIT_TEST",
	ExecutableName:   "SpeechKit.exe",
	EnvPrefix:        "SPEECHKIT",
}

var (
	mu         sync.RWMutex
	registered *Product
	// testOverride is set by UseForTest and ClearForTest; testCleared makes
	// Current report ErrNoIdentity even in a test binary.
	testOverride *Product
	testCleared  bool
)

// Register sets the process identity. Call it once from the composition root
// before anything reads it. Registering an equal identity again is a no-op;
// a different one is ErrAlreadyRegistered.
func Register(p Product) error {
	p = p.normalized()
	if err := p.Validate(); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if registered != nil {
		if registered.equal(p) {
			return nil
		}
		return fmt.Errorf("%w: %s/%s", ErrAlreadyRegistered, registered.Vendor, registered.Product)
	}
	registered = &p
	return nil
}

// Current returns the registered identity, or ErrNoIdentity.
func Current() (Product, error) {
	mu.RLock()
	defer mu.RUnlock()
	switch {
	case testCleared:
		return Product{}, ErrNoIdentity
	case testOverride != nil:
		return *testOverride, nil
	case registered != nil:
		return *registered, nil
	case testing.Testing():
		return ForTests, nil
	default:
		return Product{}, ErrNoIdentity
	}
}

// UseForTest makes p the identity for the rest of the test and restores the
// previous state afterwards. It panics outside a test binary.
func UseForTest(tb testing.TB, p Product) {
	tb.Helper()
	requireTestBinary()
	p = p.normalized()
	if err := p.Validate(); err != nil {
		tb.Fatal(err)
	}
	swapForTest(tb, &p, false)
}

// ClearForTest makes Current report ErrNoIdentity for the rest of the test,
// as in a root that registered nothing. It panics outside a test binary.
func ClearForTest(tb testing.TB) {
	tb.Helper()
	requireTestBinary()
	swapForTest(tb, nil, true)
}

func swapForTest(tb testing.TB, p *Product, cleared bool) {
	mu.Lock()
	prevOverride, prevCleared := testOverride, testCleared
	testOverride, testCleared = p, cleared
	mu.Unlock()
	tb.Cleanup(func() {
		mu.Lock()
		testOverride, testCleared = prevOverride, prevCleared
		mu.Unlock()
	})
}

func requireTestBinary() {
	if !testing.Testing() {
		panic("productid: test helper called outside a test binary")
	}
}

// DisplayName is the vendor and app name, e.g. "kombify SpeechKit": the
// Windows Event Log source, the Wails application name and the Keychain item
// label.
func (p Product) DisplayName() string {
	return p.Vendor + " " + p.AppName
}

// ExecutableStem is ExecutableName without its extension ("SpeechKit" for
// "SpeechKit.exe"): the macOS bundle is <stem>.app and installer assets are
// named <stem>-Setup-v<version>.exe.
func (p Product) ExecutableStem() string {
	return strings.TrimSuffix(p.ExecutableName, path.Ext(p.ExecutableName))
}

// FileStem is ExecutableStem in lower case ("speechkit"): the stem of the
// update runner script and of scratch files the host creates.
func (p Product) FileStem() string {
	return strings.ToLower(p.ExecutableStem())
}

// Validate reports whether every name can be used in a registry path, a file
// name, a kernel object name and a Keychain item.
func (p Product) Validate() error {
	invalid := func(field, value string) error {
		return fmt.Errorf("%w: %s %q", ErrInvalid, field, value)
	}
	switch {
	case !namePart(p.Vendor, true):
		return invalid("vendor", p.Vendor)
	case !namePart(p.Product, true):
		return invalid("product", p.Product)
	case !reverseDNS(p.BundleID):
		return invalid("bundle id", p.BundleID)
	case !displayText(p.AppName):
		return invalid("app name", p.AppName)
	case !namePart(p.DataDirName, true):
		return invalid("data directory name", p.DataDirName)
	case !reverseDNS(p.SingleInstanceID):
		return invalid("single-instance id", p.SingleInstanceID)
	case !namePart(p.CachePrefix, false):
		return invalid("cache prefix", p.CachePrefix)
	case !namePart(p.RecordPrefix, false):
		return invalid("record prefix", p.RecordPrefix)
	case !namePart(p.ExecutableName, false):
		return invalid("executable name", p.ExecutableName)
	case p.EnvPrefix != "" && !envName(p.EnvPrefix):
		return invalid("environment prefix", p.EnvPrefix)
	case p.FeedbackURL != "" && !httpsURL(p.FeedbackURL):
		return invalid("feedback URL", p.FeedbackURL)
	}
	return nil
}

func (p Product) normalized() Product {
	for _, s := range p.names() {
		*s = strings.TrimSpace(*s)
	}
	return p
}

// names lists every name field, in a fixed order.
func (p *Product) names() []*string {
	return []*string{
		&p.Vendor, &p.Product, &p.BundleID, &p.AppName, &p.DataDirName, &p.SingleInstanceID,
		&p.CachePrefix, &p.RecordPrefix, &p.ExecutableName, &p.EnvPrefix, &p.FeedbackURL,
	}
}

func (p Product) equal(o Product) bool {
	pn, on := p.names(), o.names()
	for i := range pn {
		if *pn[i] != *on[i] {
			return false
		}
	}
	return bytes.Equal(p.Icons.App, o.Icons.App) &&
		bytes.Equal(p.Icons.TrayIdle, o.Icons.TrayIdle) &&
		bytes.Equal(p.Icons.TrayTemplate, o.Icons.TrayTemplate)
}

// namePart accepts a single path or registry key element: letters, digits,
// '.', '-', '_' and (when allowed) spaces, never "." or "..".
func namePart(s string, allowSpace bool) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		case r == ' ' && allowSpace:
		default:
			return false
		}
	}
	return true
}

func reverseDNS(s string) bool {
	return namePart(s, false) && strings.Contains(s, ".") &&
		!strings.Contains(s, "..") && !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".")
}

func displayText(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			return false
		}
	}
	return true
}

func envName(s string) bool {
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return s != ""
}

func httpsURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
