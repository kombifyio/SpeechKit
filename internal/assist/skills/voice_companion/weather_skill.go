package voice_companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// WeatherSkill answers "what's the weather in <city>" / "wie wird das
// wetter in <stadt>" by calling Open-Meteo's free geocoding + forecast
// APIs. Open-Meteo requires no key and is licensed CC-BY-4.0
// (https://open-meteo.com/en/license) — making it the cleanest "ships
// in OSS, works offline-key-free" weather provider for SpeechKit.
//
// The HTTP client is injectable for tests. Default uses a 5s-timeout
// shared client.
type WeatherSkill struct {
	client      *http.Client
	geocodeURL  string
	forecastURL string
}

// NewWeatherSkill returns a skill with the default Open-Meteo endpoints
// and a 5s HTTP client. Use WithClient/WithEndpoints to override for
// tests or alternative providers.
func NewWeatherSkill() *WeatherSkill {
	return &WeatherSkill{
		client:      &http.Client{Timeout: 5 * time.Second},
		geocodeURL:  "https://geocoding-api.open-meteo.com/v1/search",
		forecastURL: "https://api.open-meteo.com/v1/forecast",
	}
}

// WithClient returns a copy of the skill backed by the given HTTP
// client. Use in tests with httptest.NewServer.
func (s *WeatherSkill) WithClient(c *http.Client) *WeatherSkill {
	out := *s
	if c != nil {
		out.client = c
	}
	return &out
}

// WithEndpoints swaps the geocode + forecast base URLs. Test-only.
func (s *WeatherSkill) WithEndpoints(geocode, forecast string) *WeatherSkill {
	out := *s
	if geocode != "" {
		out.geocodeURL = geocode
	}
	if forecast != "" {
		out.forecastURL = forecast
	}
	return &out
}

// Intent reports the shortcuts.Intent this skill handles.
func (s *WeatherSkill) Intent() shortcuts.Intent { return shortcuts.IntentWeather }

// Execute runs the skill against a single ToolCall. Payload should hold
// the city name; empty payload falls through to the LLM via silent
// result.
func (s *WeatherSkill) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	city := extractCity(call.Payload)
	if city == "" {
		return silentResult(call.Locale), nil
	}

	lat, lon, displayName, err := s.geocode(ctx, city, call.Locale)
	if err != nil {
		return assist.ToolResult{}, fmt.Errorf("weather: geocode %q: %w", city, err)
	}
	if displayName == "" {
		// City not found — speak a polite "didn't find it" in-locale
		// instead of failing the pipeline.
		return assist.ToolResult{
			Text:      formatCityNotFound(city, call.Locale),
			SpeakText: formatCityNotFound(city, call.Locale),
			Action:    "respond",
			Locale:    call.Locale,
			Surface:   assist.ResultSurfacePanel,
			Kind:      assist.ResultKindAnswer,
		}, nil
	}

	current, err := s.fetchCurrent(ctx, lat, lon)
	if err != nil {
		return assist.ToolResult{}, fmt.Errorf("weather: forecast %q: %w", city, err)
	}

	text := formatWeather(displayName, current, call.Locale)
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    "respond",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfacePanel,
		Kind:      assist.ResultKindAnswer,
	}, nil
}

// extractCity strips leading "in" / "for" / "fuer" tokens from the
// payload so "wetter in berlin" → "berlin" and "weather for london" →
// "london". The lexicon's prefix-match already removed the wake-prefix
// ("wie wird das wetter") and the leading "in"/"fuer" survives.
func extractCity(payload string) string {
	city := strings.TrimSpace(payload)
	city = strings.TrimSpace(strings.TrimSuffix(city, "?"))
	city = strings.TrimSpace(strings.TrimSuffix(city, "."))
	lower := strings.ToLower(city)
	for _, prefix := range []string{"in ", "fuer ", "für ", "for ", "at "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(city[len(prefix):])
		}
	}
	return city
}

// geocode calls Open-Meteo's geocoding API and returns the top
// candidate's coordinates + canonical display name. Locale steers the
// `language` parameter so DE searches return "München" rather than
// "Munich" for downstream voice playback.
func (s *WeatherSkill) geocode(ctx context.Context, city, locale string) (float64, float64, string, error) {
	q := url.Values{}
	q.Set("name", city)
	q.Set("count", "1")
	q.Set("language", openMeteoLanguage(locale))
	q.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.geocodeURL+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return 0, 0, "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, 0, "", err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, 0, "", fmt.Errorf("geocode status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, 0, "", fmt.Errorf("geocode decode: %w", err)
	}
	if len(payload.Results) == 0 {
		return 0, 0, "", nil
	}
	r := payload.Results[0]
	display := r.Name
	if r.Country != "" && !strings.EqualFold(r.Name, r.Country) {
		display = r.Name
	}
	return r.Latitude, r.Longitude, display, nil
}

// currentWeather captures the forecast subset we render to the user.
type currentWeather struct {
	Temperature float64
	WeatherCode int
}

// fetchCurrent calls Open-Meteo's current-conditions endpoint for the
// given coordinates. We use the `current` query (sub-hourly) so the
// answer reflects "right now" rather than a daily summary.
func (s *WeatherSkill) fetchCurrent(ctx context.Context, lat, lon float64) (currentWeather, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.4f", lat))
	q.Set("longitude", fmt.Sprintf("%.4f", lon))
	q.Set("current", "temperature_2m,weather_code")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.forecastURL+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return currentWeather{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return currentWeather{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return currentWeather{}, fmt.Errorf("forecast status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			WeatherCode int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return currentWeather{}, fmt.Errorf("forecast decode: %w", err)
	}
	if payload.Current.WeatherCode == 0 && payload.Current.Temperature == 0 {
		// Open-Meteo returns 0 for missing fields silently; this is a
		// best-effort sanity check.
		return currentWeather{}, errors.New("forecast returned empty current block")
	}
	return currentWeather{
		Temperature: payload.Current.Temperature,
		WeatherCode: payload.Current.WeatherCode,
	}, nil
}

// openMeteoLanguage maps the SpeechKit locale to Open-Meteo's language
// codes. Open-Meteo accepts ISO-639-1 lowercase; default to EN.
func openMeteoLanguage(locale string) string {
	switch normalizeLocale(locale) {
	case "de":
		return "de"
	default:
		return "en"
	}
}

// formatWeather renders the temperature + WMO weather-code description
// for the user. WMO codes follow https://open-meteo.com/en/docs (table
// "Weather variable documentation" — Weather code (WMO 4677)).
func formatWeather(city string, w currentWeather, locale string) string {
	desc := describeWMO(w.WeatherCode, locale)
	temp := w.Temperature
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("In %s sind es %.0f Grad und %s.", city, temp, desc)
	default:
		return fmt.Sprintf("In %s it is %.0f degrees and %s.", city, temp, desc)
	}
}

func formatCityNotFound(city, locale string) string {
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("Den Ort %q konnte ich nicht finden.", city)
	default:
		return fmt.Sprintf("I could not find the city %q.", city)
	}
}

// describeWMO maps WMO 4677 weather-codes to short user-facing phrases.
// Buckets reflect Open-Meteo's documentation:
//
//	0      Clear sky
//	1-3    Mainly clear, partly cloudy, overcast
//	45-48  Fog
//	51-67  Drizzle / rain
//	71-77  Snow
//	80-86  Rain showers / snow showers
//	95-99  Thunderstorm
func describeWMO(code int, locale string) string {
	de := normalizeLocale(locale) == "de"
	switch {
	case code == 0:
		if de {
			return "der Himmel ist klar"
		}
		return "the sky is clear"
	case code >= 1 && code <= 3:
		if de {
			return "teilweise bewoelkt"
		}
		return "partly cloudy"
	case code >= 45 && code <= 48:
		if de {
			return "neblig"
		}
		return "foggy"
	case code >= 51 && code <= 67:
		if de {
			return "es regnet"
		}
		return "raining"
	case code >= 71 && code <= 77:
		if de {
			return "es schneit"
		}
		return "snowing"
	case code >= 80 && code <= 86:
		if de {
			return "Schauer ziehen durch"
		}
		return "showers are passing"
	case code >= 95 && code <= 99:
		if de {
			return "ein Gewitter zieht auf"
		}
		return "a thunderstorm is forming"
	default:
		if de {
			return "das Wetter ist wechselhaft"
		}
		return "the weather is mixed"
	}
}
