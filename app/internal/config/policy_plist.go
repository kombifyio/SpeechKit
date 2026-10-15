package config

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// macOS managed preferences. MDM delivers a configuration profile's payload
// for the app as /Library/Managed Preferences/<BundleID>.plist (see
// PolicyIdentity), a
// root-owned directory a user cannot write. That file is the only policy
// source on macOS; a plist in the user's own ~/Library/Preferences is never
// read, because the user can write it. Keys use the registry names with a dot
// (`Providers.Allowed`, `Foundry.EntraClientId`); booleans are <true/> or
// <false/> (an <integer> is accepted too), lists are an <array> of <string>
// or one semicolon-separated <string>.
//
// go.mod carries no plist library, so this parses the XML property-list
// subset with encoding/xml. A binary plist (bplist00) is rejected with
// ErrManagedPreferencesFormat; deliver the payload as XML or convert it with
// `plutil -convert xml1`.

// ManagedPreferencesDir is the only directory managed preferences are trusted
// from. The file name is the registered PolicyIdentity's BundleID.
const ManagedPreferencesDir = "/Library/Managed Preferences"

var (
	// ErrManagedPreferencesFormat is returned for a managed-preferences file
	// that is not an XML property list of the supported subset.
	ErrManagedPreferencesFormat = errors.New("managed preferences: unsupported property list")
	// ErrManagedPreferencesUntrusted is returned for a managed-preferences
	// file a non-administrator could have written.
	ErrManagedPreferencesUntrusted = errors.New("managed preferences: file is not protected")
)

// readManagedPreferencesFor reads the managed preferences of the registered
// PolicyIdentity from dir, and nothing when no identity is registered.
func readManagedPreferencesFor(dir string, trust func(fs.FileInfo) error) PolicyValues {
	id, ok := RegisteredPolicyIdentity()
	if !ok {
		return noIdentityPolicy()
	}
	return readManagedPreferences(dir, id.BundleID, trust)
}

// readManagedPreferences reads <dir>/<bundleID>.plist. A missing file is "no
// policy". trust, when set, vets the file before it is parsed (on macOS:
// owned by root and not writable by anyone else). A file that is present but
// untrusted or unreadable yields no values and a rejection.
func readManagedPreferences(dir, bundleID string, trust func(fs.FileInfo) error) PolicyValues {
	path := filepath.Join(dir, bundleID+".plist")
	reject := func(err error) PolicyValues {
		return PolicyValues{
			Origin:   "none",
			Rejected: []PolicyRejection{{Key: path, Err: fmt.Errorf("%w: %w", ErrPolicyValueInvalid, err)}},
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return PolicyValues{Origin: "none"}
	}
	if err != nil {
		return reject(err)
	}
	if !info.Mode().IsRegular() {
		return reject(fmt.Errorf("%w: not a regular file", ErrManagedPreferencesUntrusted))
	}
	if trust != nil {
		if err := trust(info); err != nil {
			return reject(err)
		}
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the fixed managed-preferences location for a validated bundle id.
	if err != nil {
		return reject(err)
	}
	values, err := parsePlistDict(data)
	if err != nil {
		return reject(err)
	}
	return singleSourcePolicy(readPolicyHive(plistSource(values)), PolicySourceManagedPreferences)
}

// plistValue is one top-level value of the supported subset: bool, int64,
// string or []string. unsupported marks any other element.
type plistValue struct {
	kind        string // "bool" | "integer" | "string" | "array"
	b           bool
	i           int64
	s           string
	list        []string
	unsupported string
}

// parsePlistDict parses an XML property list whose root is a <dict> into its
// top-level entries.
func parsePlistDict(data []byte) (map[string]plistValue, error) {
	if bytes.HasPrefix(data, []byte("bplist")) {
		return nil, fmt.Errorf("%w: binary property list", ErrManagedPreferencesFormat)
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	// Strict XML; the DOCTYPE naming Apple's DTD is a directive token and is
	// never fetched.
	dec.Strict = true
	var root bool
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: no <dict>", ErrManagedPreferencesFormat)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrManagedPreferencesFormat, err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "plist":
			root = true
		case "dict":
			if !root {
				return nil, fmt.Errorf("%w: <dict> outside <plist>", ErrManagedPreferencesFormat)
			}
			return parsePlistTopDict(dec)
		default:
			return nil, fmt.Errorf("%w: unexpected <%s>", ErrManagedPreferencesFormat, start.Name.Local)
		}
	}
}

func parsePlistTopDict(dec *xml.Decoder) (map[string]plistValue, error) {
	out := map[string]plistValue{}
	key := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrManagedPreferencesFormat, err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "dict" {
				return out, nil
			}
		case xml.StartElement:
			if t.Name.Local == "key" {
				var k string
				if err := dec.DecodeElement(&k, &t); err != nil {
					return nil, fmt.Errorf("%w: %w", ErrManagedPreferencesFormat, err)
				}
				key = strings.TrimSpace(k)
				continue
			}
			if key == "" {
				return nil, fmt.Errorf("%w: value without <key>", ErrManagedPreferencesFormat)
			}
			v, err := parsePlistValue(dec, t)
			if err != nil {
				return nil, err
			}
			out[key] = v
			key = ""
		}
	}
}

func parsePlistValue(dec *xml.Decoder, start xml.StartElement) (plistValue, error) {
	wrap := func(err error) (plistValue, error) {
		return plistValue{}, fmt.Errorf("%w: <%s>: %w", ErrManagedPreferencesFormat, start.Name.Local, err)
	}
	switch start.Name.Local {
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return wrap(err)
		}
		return plistValue{kind: "bool", b: start.Name.Local == "true"}, nil
	case "integer":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return wrap(err)
		}
		i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return wrap(err)
		}
		return plistValue{kind: "integer", i: i}, nil
	case "string":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return wrap(err)
		}
		return plistValue{kind: "string", s: s}, nil
	case "array":
		var arr struct {
			Strings []string `xml:"string"`
			Other   []struct {
				XMLName xml.Name
			} `xml:",any"`
		}
		if err := dec.DecodeElement(&arr, &start); err != nil {
			return wrap(err)
		}
		if len(arr.Other) > 0 {
			return plistValue{unsupported: "array of " + arr.Other[0].XMLName.Local}, nil
		}
		return plistValue{kind: "array", list: append([]string{}, arr.Strings...)}, nil
	default:
		if err := dec.Skip(); err != nil {
			return wrap(err)
		}
		return plistValue{unsupported: start.Name.Local}, nil
	}
}

// plistSource serves the parsed entries under the registry key names:
// `Providers\Allowed` is read from the plist key `Providers.Allowed`.
type plistSource map[string]plistValue

func (p plistSource) lookup(key string) (plistValue, bool, error) {
	v, ok := p[strings.Replace(key, `\`, ".", 1)]
	if !ok {
		return plistValue{}, false, nil
	}
	if v.unsupported != "" {
		return plistValue{}, false, fmt.Errorf("%w: <%s>", errPolicyType, v.unsupported)
	}
	return v, true, nil
}

func (p plistSource) boolValue(key string) (*bool, error) {
	v, ok, err := p.lookup(key)
	if !ok || err != nil {
		return nil, err
	}
	switch v.kind {
	case "bool":
		return &v.b, nil
	case "integer":
		b := v.i != 0
		return &b, nil
	}
	return nil, errPolicyType
}

func (p plistSource) intValue(key string) (*int, error) {
	v, ok, err := p.lookup(key)
	if !ok || err != nil {
		return nil, err
	}
	if v.kind != "integer" {
		return nil, errPolicyType
	}
	if v.i < 0 || v.i > int64(int(^uint(0)>>1)) {
		return nil, errors.New("value out of range")
	}
	i := int(v.i) // #nosec G115 -- guarded against int overflow before conversion.
	return &i, nil
}

func (p plistSource) stringValue(key string) (string, error) {
	v, ok, err := p.lookup(key)
	if !ok || err != nil {
		return "", err
	}
	if v.kind != "string" {
		return "", errPolicyType
	}
	return strings.TrimSpace(v.s), nil
}

func (p plistSource) listValue(key string) ([]string, error) {
	v, ok, err := p.lookup(key)
	if !ok || err != nil {
		return nil, err
	}
	switch v.kind {
	case "array":
		return splitPolicyList(strings.Join(v.list, ";")), nil
	case "string":
		return splitPolicyList(v.s), nil
	}
	return nil, errPolicyType
}
