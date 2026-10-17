package onboarding

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDeviceAddCustomerHandoff(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, DeviceAddPath+"?user_code=abcd-1234&redirect_uri=https://attacker.example&device_code=private", nil)
	request.Header.Set("Accept-Language", "de-DE,de;q=0.9")
	recorder := httptest.NewRecorder()
	DeviceAddHandler(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, `value="ABCD-1234"`) || !strings.Contains(body, "Deine Box hinzufügen") {
		t.Fatalf("customer handoff did not render localized short code: status %d", recorder.Code)
	}
	if !strings.Contains(body, `action="`+deviceVerificationURI+`" method="get"`) {
		t.Fatal("confirmation must go only to Auth0 Universal Login")
	}
	for _, forbidden := range []string{"attacker.example", "private", "device_code", "/setup", "Bearer ", "<script"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("customer handoff exposed forbidden content %q", forbidden)
		}
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("pairing codes must not enter caches or referrers")
	}
}

func TestDeviceAddRejectsUntrustedCodeAndMutation(t *testing.T) {
	for _, code := range []string{`"><script>alert(1)</script>`, "ABCD-1234&redirect_uri=https://attacker.example"} {
		recorder := httptest.NewRecorder()
		DeviceAddHandler(recorder, httptest.NewRequest(http.MethodGet, DeviceAddPath+"?user_code="+url.QueryEscape(code), nil))
		if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), code) || strings.Contains(recorder.Body.String(), "attacker.example") {
			t.Fatal("untrusted code must be rejected without reflection")
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		DeviceAddHandler(recorder, httptest.NewRequest(method, DeviceAddPath, nil))
		if recorder.Code != http.StatusMethodNotAllowed || recorder.Body.Len() != 0 {
			t.Fatal("Add Device must never accept mutations")
		}
	}
	for _, path := range []string{DeviceAddPath + "/approve", "/setup", "/v1/server/admin/session"} {
		recorder := httptest.NewRecorder()
		DeviceAddHandler(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatal("handler must not serve adjacent or operator routes")
		}
	}
}

func TestDeviceAddLanguagesAndHead(t *testing.T) {
	for _, locale := range []string{"en", "de", "es", "zh-Hans", "hi", "ar"} {
		recorder := httptest.NewRecorder()
		DeviceAddHandler(recorder, httptest.NewRequest(http.MethodGet, DeviceAddPath+"?lang="+locale, nil))
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Language") != locale || !strings.Contains(recorder.Body.String(), deviceAddCatalog[locale]["sk.device.add.title"]) {
			t.Fatalf("missing localized customer flow for %s", locale)
		}
	}
	recorder := httptest.NewRecorder()
	DeviceAddHandler(recorder, httptest.NewRequest(http.MethodHead, DeviceAddPath, nil))
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatal("HEAD must return metadata without a body")
	}
}

func TestDeviceAddNegotiatesWeightedPublishedLocales(t *testing.T) {
	for _, tc := range []struct{ query, accept, want string }{
		{"", "de;q=0.2, es;q=0.9", "es"},
		{"", "de;q=0, es;q=0.5", "es"},
		{"?lang=zh-Hant", "zh-TW, de-DE;q=0.8", "de"},
		{"", "zh-Hant, de-DE;q=0.8", "de"},
		{"?lang=de-AT", "es;q=1", "de"},
		{"", "zh-CN, de;q=0.8", "zh-Hans"},
	} {
		request := httptest.NewRequest(http.MethodGet, DeviceAddPath+tc.query, nil)
		request.Header.Set("Accept-Language", tc.accept)
		recorder := httptest.NewRecorder()
		DeviceAddHandler(recorder, request)
		if got := recorder.Header().Get("Content-Language"); got != tc.want {
			t.Errorf("query %q, Accept-Language %q: got %q, want %q", tc.query, tc.accept, got, tc.want)
		}
	}
}
