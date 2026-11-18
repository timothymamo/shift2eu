package review

// ParseAnswer parses the model's answer text strictly: exactly one JSON object,
// with surrounding whitespace only, of the form
//
//	{"summary": "...", "findings": [{"severity": "blocker", "category": "correctness",
//	  "path": "internal/x/y.go", "line": 12, "title": "...", "body": "..."}]}
//
// summary, findings, and each finding's severity, category, title and body are
// required; path and line are optional. It returns an error wrapping
// ErrMalformedAnswer for: text before or after the object (a code fence
// included), a second value, an unknown field at any level, a missing or empty
// summary, title or body, a findings that is not an array, a severity or
// category outside the constants of this package (compared exactly, so
// "Blocker" is refused), a negative line, a line above 0 without a path, a path
// that is absolute or holds a ".." element or a backslash. An answer with no
// findings is valid, findings then being empty, not nil.
func ParseAnswer(text string) (Answer, error) {
	_ = text // the skeleton's stub (step 1); step 3 parses it

	return Answer{}, errNotImplemented
}
