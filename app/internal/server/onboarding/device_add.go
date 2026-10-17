package onboarding

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"regexp"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"golang.org/x/text/language"
)

const DeviceAddPath = "/v1/devices/add"

//go:embed assets/device-add.html
var deviceAddHTML string

//go:embed assets/device-add.messages.json
var deviceAddMessages []byte

var deviceCodePattern = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)
var deviceAddTemplate = template.Must(template.New("device-add").Parse(deviceAddHTML))

const deviceVerificationOrigin = "https://" + config.CloudDefaultAuthDomain
const deviceVerificationURI = deviceVerificationOrigin + "/activate"

var deviceAddCatalog = func() map[string]map[string]string {
	var catalog map[string]map[string]string
	if err := json.Unmarshal(deviceAddMessages, &catalog); err != nil {
		panic(err)
	}
	return catalog
}()

// DeviceAddHandler serves only a customer handoff to Auth0 Universal Login.
// It neither exchanges credentials nor registers/approves a device. Auth0
// retains account choice and the final approval, including an existing login.
func DeviceAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != DeviceAddPath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action "+deviceVerificationOrigin+"; base-uri 'none'; frame-ancestors 'none'")
	locale := deviceAddLocale(r)
	w.Header().Set("Content-Language", locale)
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("user_code")))
	invalid := code != "" && !deviceCodePattern.MatchString(code)
	if invalid {
		code = ""
	}
	data := struct {
		Locale, Direction, Code string
		VerificationURI         string
		Messages                map[string]string
		Invalid                 bool
	}{Locale: locale, Direction: "ltr", Code: code, VerificationURI: deviceVerificationURI, Messages: deviceAddCatalog[locale], Invalid: invalid}
	if locale == "ar" {
		data.Direction = "rtl"
	}
	var body bytes.Buffer
	if err := deviceAddTemplate.Execute(&body, data); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if invalid {
		w.WriteHeader(http.StatusBadRequest)
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func deviceAddLocale(r *http.Request) string {
	if locale, ok := deviceAddAvailableLocale(r.URL.Query().Get("lang")); ok {
		return locale
	}
	// ParseAcceptLanguage sorts the full weighted list and removes q=0 tags.
	tags, _, err := language.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
	if err == nil {
		for _, tag := range tags {
			if locale, ok := deviceAddAvailableLocale(tag.String()); ok {
				return locale
			}
		}
	}
	return "en"
}

func deviceAddAvailableLocale(candidate string) (string, bool) {
	tag, err := language.Parse(strings.TrimSpace(candidate))
	if err != nil || strings.TrimSpace(candidate) == "" {
		return "", false
	}
	locale := localization.ResolveLocale(tag.String())
	base, _ := tag.Base()
	// ResolveLocale's English fallback is not a match for an unsupported tag:
	// e.g. Traditional Chinese must allow a later German browser preference.
	if locale == "en" && base.String() != "en" {
		return "", false
	}
	_, available := deviceAddCatalog[locale]
	return locale, available
}
