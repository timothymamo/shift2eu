package claudecode

import (
	"bytes"
	"encoding/json"
	"strings"
)

// PermissionModeError is a session whose system/init element reports another
// permission mode than the launch asked for, or none (issue #592). Its text is
// fixed words alone: the expected mode and a known mode or a class, never a
// byte of the element.
type PermissionModeError struct {
	// Expected is the config's PermissionMode.
	Expected string
	// Reported is one of knownPermissionModes as the element names it, or one
	// of the classes ReportMissing, ReportNotAString, ReportOtherMode and
	// ReportTwice.
	Reported string
}

// Error is the refusal's one sentence.
func (e *PermissionModeError) Error() string {
	return "the session's permission mode is not the one asked for: expected " + e.Expected + ", reported " + e.Reported
}

// The classes of a report that names no known mode.
const (
	// ReportMissing is no init element before the session's first element of
	// another type, or before the run's end, or one that names no
	// permissionMode.
	ReportMissing = "missing"
	// ReportNotAString is a permissionMode that is null, a number, a boolean,
	// an object or a list.
	ReportNotAString = "not a string"
	// ReportOtherMode is a string that is none of knownPermissionModes,
	// compared exactly, case and all.
	ReportOtherMode = "another mode"
	// ReportTwice is a second init element, or an element that names type,
	// subtype or permissionMode twice or in another case: encoding/json would
	// read such a key case-insensitively and keep the last one.
	ReportTwice = "two init lines"
)

// knownPermissionModes are the modes a report names as they are: Claude
// Code's own. Any other string is ReportOtherMode, so no byte of the element
// reaches the error.
var knownPermissionModes = []string{"default", "acceptEdits", "plan", "dontAsk", "bypassPermissions", "auto"}

// ModeCheck reads a session's stream-json lines, in order, and decides whether
// its system/init element reports the permission mode the launch asked for.
// The launcher runs it over the CLI's stdout as it arrives, and the audit's
// judgement over the preflight's transcript, so both read the element the same
// way. It refuses, at the line that decides it: an init element whose
// permissionMode is not exactly the expected one, a second init element, an
// element that names type, subtype or permissionMode twice or in another case,
// and an element other than a system one before any init element - the
// session's turn has started under a mode nobody reported. A line that is not
// a JSON object is skipped: it is no element, and no tool call.
type ModeCheck struct {
	expected string
	seenInit bool
	refused  string
}

// NewModeCheck returns a check that wants the init element to report
// expected.
func NewModeCheck(expected string) *ModeCheck {
	return &ModeCheck{expected: expected}
}

// Line reads one line of the stream, its newline trimmed or not, and reports
// whether the session is refused, by this line or an earlier one.
func (m *ModeCheck) Line(line []byte) bool {
	if m.refused != "" {
		return true
	}

	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' || !json.Valid(line) {
		return false
	}

	keys, ok := initKeys(line)

	switch {
	case !ok:
		m.refused = ReportTwice
	case keys.elementType == "system" && keys.subtype == "init":
		if m.seenInit {
			m.refused = ReportTwice

			break
		}

		m.seenInit = true

		if reported := classifyMode(keys.mode); reported != m.expected {
			m.refused = reported
		}
	case keys.elementType != "system" && !m.seenInit:
		m.refused = ReportMissing
	}

	return m.refused != ""
}

// Cut reads a line the reader could not keep whole: before the init element it
// refuses the session as ReportMissing, since the line may be an element of
// its turn; after it, the line is skipped.
func (m *ModeCheck) Cut() bool {
	if m.refused == "" && !m.seenInit {
		m.refused = ReportMissing
	}

	return m.refused != ""
}

// Err returns the refusal once the stream has ended: nil when the init element
// reported the expected mode, else the *PermissionModeError naming what it
// reported, ReportMissing for a stream that held none.
func (m *ModeCheck) Err() *PermissionModeError {
	switch {
	case m.refused != "":
		return &PermissionModeError{Expected: m.expected, Reported: m.refused}
	case !m.seenInit:
		return &PermissionModeError{Expected: m.expected, Reported: ReportMissing}
	default:
		return nil
	}
}

// refusedEarly returns the refusal decided at a line, nil when no line has
// decided one yet.
func (m *ModeCheck) refusedEarly() *PermissionModeError {
	if m.refused == "" {
		return nil
	}

	return m.Err()
}

// elementKeys are what ModeCheck reads of an element's top-level object: its
// type and subtype when each is a JSON string, "" otherwise, and the raw value
// of permissionMode, nil when absent.
type elementKeys struct {
	elementType string
	subtype     string
	mode        json.RawMessage
}

// The keys ModeCheck reads, each refused when named twice or in another case.
var initKeyNames = []string{"type", "subtype", "permissionMode"}

// initKeys walks the top-level object of line, valid JSON that starts with
// '{', once, token by token, as the audit's judgement walks an element's
// (issue #590), and returns its type, subtype and raw permissionMode. It
// returns false for an object that names one of initKeyNames twice or in
// another case. The decoder's errors are never formatted: they can quote the
// line.
func initKeys(line []byte) (elementKeys, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()

	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return elementKeys{}, false
	}

	var (
		keys elementKeys
		seen = map[string]bool{}
	)

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return elementKeys{}, false
		}

		key, isKey := tok.(string)
		if !isKey {
			return elementKeys{}, false
		}

		name, named := initKeyName(key)

		if !named {
			if err := skipJSONValue(dec); err != nil {
				return elementKeys{}, false
			}

			continue
		}

		// A known key in another case, or named twice, is refused.
		if name != key || seen[name] {
			return elementKeys{}, false
		}

		seen[name] = true

		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return elementKeys{}, false
		}

		switch name {
		case "type":
			keys.elementType = jsonString(raw)
		case "subtype":
			keys.subtype = jsonString(raw)
		default:
			keys.mode = raw
		}
	}

	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return elementKeys{}, false
	}

	return keys, true
}

// initKeyName returns the one of initKeyNames that key names, in any case.
func initKeyName(key string) (string, bool) {
	for _, name := range initKeyNames {
		if strings.EqualFold(key, name) {
			return name, true
		}
	}

	return "", false
}

// jsonString returns raw's string when raw is a JSON string, else "".
func jsonString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return ""
	}

	return s
}

// classifyMode returns the report of an init element's raw permissionMode: a
// known mode as it is, or a class.
func classifyMode(raw json.RawMessage) string {
	switch {
	case raw == nil:
		return ReportMissing
	case raw[0] != '"':
		return ReportNotAString
	}

	mode := jsonString(raw)
	if contains(knownPermissionModes, mode) {
		return mode
	}

	return ReportOtherMode
}

// skipJSONValue reads one JSON value of dec, token by token, without keeping
// it.
func skipJSONValue(dec *json.Decoder) error {
	depth := 0

	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}

		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}

		if depth == 0 {
			return nil
		}
	}
}
