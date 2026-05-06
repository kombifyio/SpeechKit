package main

import (
	"net/http"

	desktopsettings "github.com/kombifyio/SpeechKit/internal/desktop/settings"
)

func boolFormValue(req *http.Request, key string, fallback bool) bool {
	return desktopsettings.BoolValue(req, key, fallback)
}

func intFormValue(req *http.Request, key string, fallback int) int {
	return desktopsettings.IntValue(req, key, fallback)
}

func nonNegativeIntFormValue(req *http.Request, key string, fallback int) int {
	return desktopsettings.NonNegativeIntValue(req, key, fallback)
}

func trimmedFormValue(req *http.Request, key string) string {
	return desktopsettings.TrimmedValue(req, key)
}

func postFormIncludes(req *http.Request, key string) bool {
	return desktopsettings.Includes(req, key)
}

func firstNonEmpty(values ...string) string {
	return desktopsettings.FirstNonEmpty(values...)
}

func valueOrDefault(value, fallback string) string {
	return desktopsettings.ValueOrDefault(value, fallback)
}
