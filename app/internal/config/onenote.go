package config

import (
	"fmt"
	"strings"
)

// OneNoteScopePersonal is the [onenote] allowed_notebook_scope value for the
// signed-in user's own notebooks (their OneDrive). Any other value names one
// SharePoint site by its Microsoft Graph site id.
const OneNoteScopePersonal = "personal"

// OneNoteConfig controls the OneNote export of meeting notes. Desktop-only;
// the Server-Target ignores this block.
type OneNoteConfig struct {
	// Enabled lets meetings be written to OneNote: a meeting linked to a
	// notebook is synced when its review is ready, and the export routes
	// accept the onenote target. Off by default.
	Enabled bool `toml:"enabled"`
	// AllowedNotebookScope is where pages may be written: "personal" (the
	// default, also for an empty value) or the Graph id of one SharePoint
	// site. A destination elsewhere is refused before anything is sent.
	AllowedNotebookScope string `toml:"allowed_notebook_scope"`
}

// AllowedSite is the SharePoint site id destinations must live in; "" is
// the user's own notebooks.
func (c OneNoteConfig) AllowedSite() string {
	scope := strings.TrimSpace(c.AllowedNotebookScope)
	if scope == "" || strings.EqualFold(scope, OneNoteScopePersonal) {
		return ""
	}
	return scope
}

// AllowsSite reports whether a destination in the given site ("" for the
// user's own notebooks) lies inside the allowed scope.
func (c OneNoteConfig) AllowsSite(site string) bool {
	return strings.TrimSpace(site) == c.AllowedSite()
}

// validateOneNoteScope normalizes an allowed notebook scope: "personal" or a
// Graph site id (host name and GUIDs separated by commas). A value with a
// path, query or space separator is refused, so it cannot reach another
// Graph resource.
func validateOneNoteScope(v string) (string, error) {
	scope := strings.TrimSpace(v)
	if scope == "" || strings.EqualFold(scope, OneNoteScopePersonal) {
		return OneNoteScopePersonal, nil
	}
	if len(scope) > 512 || strings.ContainsAny(scope, `/\?#%& `+"\t\r\n") {
		return "", fmt.Errorf("not %q or a Microsoft Graph site id", OneNoteScopePersonal)
	}
	return scope, nil
}
