package voice_companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestWeatherSkill_IntentIsWeather(t *testing.T) {
	if NewWeatherSkill().Intent() != shortcuts.IntentWeather {
		t.Errorf("expected IntentWeather")
	}
}

func TestExtractCity_StripsLeadingPrepositions(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"in berlin", "berlin"},
		{"fuer muenchen", "muenchen"},
		{"für münchen", "münchen"},
		{"for london", "london"},
		{"at paris", "paris"},
		{"hamburg", "hamburg"},
		{"hamburg?", "hamburg"},
		{"hamburg.", "hamburg"},
		{"", ""},
	}
	for _, c := range cases {
		got := extractCity(c.in)
		if got != c.want {
			t.Errorf("extractCity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDescribeWMO_DECoversAllBuckets(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "der Himmel ist klar"},
		{2, "teilweise bewoelkt"},
		{45, "neblig"},
		{53, "es regnet"},
		{73, "es schneit"},
		{82, "Schauer ziehen durch"},
		{96, "ein Gewitter zieht auf"},
		{999, "das Wetter ist wechselhaft"},
	}
	for _, c := range cases {
		got := describeWMO(c.code, "de")
		if got != c.want {
			t.Errorf("describeWMO(%d, de) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestDescribeWMO_ENCoversAllBuckets(t *testing.T) {
	if got := describeWMO(0, "en"); got != "the sky is clear" {
		t.Errorf("WMO 0 EN = %q", got)
	}
	if got := describeWMO(82, "en"); got != "showers are passing" {
		t.Errorf("WMO 82 EN = %q", got)
	}
}

// fakeOpenMeteo returns an httptest server that mirrors Open-Meteo's
// JSON shape for `/v1/search` (geocode) and `/v1/forecast` (current
// conditions). The handler dispatches based on URL path so a single
// server can answer both endpoints — same arrangement Open-Meteo
// itself uses with different hostnames.
func fakeOpenMeteo(t *testing.T, geoLat, geoLon float64, geoName, geoCountry string, temp float64, wmo int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"latitude": geoLat, "longitude": geoLon, "name": geoName, "country": geoCountry},
				},
			})
		case strings.Contains(r.URL.Path, "/forecast"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"current": map[string]any{
					"temperature_2m": temp,
					"weather_code":   wmo,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestWeatherSkill_E2E_DE(t *testing.T) {
	srv := fakeOpenMeteo(t, 52.52, 13.41, "Berlin", "Germany", 18.4, 2)
	defer srv.Close()

	skill := NewWeatherSkill().
		WithEndpoints(srv.URL+"/search", srv.URL+"/forecast")

	got, err := skill.Execute(context.Background(), assist.ToolCall{
		Payload: "in berlin",
		Locale:  "de",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "In Berlin sind es 18 Grad und teilweise bewoelkt."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
	if got.Action != "respond" {
		t.Errorf("Action = %q, want respond", got.Action)
	}
}

func TestWeatherSkill_E2E_EN(t *testing.T) {
	srv := fakeOpenMeteo(t, 51.51, -0.13, "London", "United Kingdom", 12.7, 53)
	defer srv.Close()

	skill := NewWeatherSkill().
		WithEndpoints(srv.URL+"/search", srv.URL+"/forecast")

	got, err := skill.Execute(context.Background(), assist.ToolCall{
		Payload: "for london",
		Locale:  "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "In London it is 13 degrees and raining."
	if got.Text != want {
		t.Errorf("Text = %q, want %q", got.Text, want)
	}
}

func TestWeatherSkill_EmptyPayloadIsSilent(t *testing.T) {
	skill := NewWeatherSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "silent" {
		t.Errorf("empty payload should be silent, got Action=%q", got.Action)
	}
}

func TestWeatherSkill_CityNotFound_GracefulMessage(t *testing.T) {
	emptyGeocode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer emptyGeocode.Close()

	skill := NewWeatherSkill().
		WithEndpoints(emptyGeocode.URL+"/search", emptyGeocode.URL+"/forecast")

	got, err := skill.Execute(context.Background(), assist.ToolCall{
		Payload: "atlantis",
		Locale:  "de",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got.Text, "atlantis") {
		t.Errorf("expected 'not found' message to mention 'atlantis', got %q", got.Text)
	}
	if got.Action != "respond" {
		t.Errorf("Action = %q, want respond", got.Action)
	}
}

func TestWeatherSkill_GeocodeError_ReturnsError(t *testing.T) {
	failGeocode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failGeocode.Close()

	skill := NewWeatherSkill().
		WithEndpoints(failGeocode.URL+"/search", failGeocode.URL+"/forecast")

	_, err := skill.Execute(context.Background(), assist.ToolCall{
		Payload: "berlin",
		Locale:  "en",
	})
	if err == nil {
		t.Fatal("expected an error on 500 geocode response")
	}
}
