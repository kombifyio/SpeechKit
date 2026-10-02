package netsec

import (
	"net/url"
	"regexp"
)

// RedactURLError scrubs credentials from URLs carried by err so the error can
// be logged or shown to a user. net/http strips a userinfo password from a
// *url.Error, but not a query parameter such as ?key=, ?sig= or
// ?access_token=.
//
// Every *url.Error reachable through Unwrap() error / Unwrap() []error has
// its URL rewritten in place (http.Client returns a fresh one per request, so
// the caller owns it). Wrappers created with fmt.Errorf have already rendered
// their message, so when err.Error() still contains a URL query, fragment or
// userinfo, the result is a wrapper whose Error() is scrubbed and whose
// Unwrap() returns err, keeping errors.Is and errors.As working. Scheme, host
// and path stay for diagnostics.
func RedactURLError(err error) error {
	if err == nil {
		return nil
	}
	redactURLErrors(err, 0)
	msg := err.Error()
	if scrubbed := redactURLsInText(msg); scrubbed != msg {
		return &redactedError{msg: scrubbed, err: err}
	}
	return err
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func redactURLErrors(err error, depth int) {
	if err == nil || depth > 32 {
		return
	}
	if ue, ok := err.(*url.Error); ok && ue != nil { //nolint:errorlint // the walk below visits every wrapped error
		ue.URL = redactURLString(ue.URL)
	}
	switch u := err.(type) { //nolint:errorlint // this walks the Unwrap tree itself
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			redactURLErrors(e, depth+1)
		}
	case interface{ Unwrap() error }:
		redactURLErrors(u.Unwrap(), depth+1)
	}
}

var (
	urlUserinfoPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/?#@"'<>]+@`)
	urlQueryPattern    = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^\s?#"'<>]*)[?#][^\s"'<>]*`)
)

// redactURLsInText removes userinfo, query strings and fragments from every
// absolute URL in s.
func redactURLsInText(s string) string {
	s = urlUserinfoPattern.ReplaceAllString(s, "$1")
	return urlQueryPattern.ReplaceAllString(s, "$1")
}

// redactURLString returns raw without userinfo, query and fragment. A string
// that does not parse is replaced entirely: it cannot be shown safely.
func redactURLString(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "[redacted-url]"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}
