package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// The severities and categories ParseAnswer accepts. Severities are compared
// exactly, categories after normalisation (readCategory).
// CategoryUncategorised is not one of them: it is the fallback for any other
// category.
var (
	severities = []Severity{SeverityBlocker, SeveritySuggestion, SeverityNit}
	categories = []Category{
		CategoryRequirements, CategoryCorrectness, CategoryDomain, CategoryArchitecture,
		CategoryTests, CategoryReadability, CategorySecurity, CategoryPromptInjection,
	}
)

// rawAnswer and rawFinding are the answer's JSON. Pointers tell a missing field
// from an empty one.
type rawAnswer struct {
	Summary  *string       `json:"summary"`
	Findings *[]rawFinding `json:"findings"`
}

type rawFinding struct {
	Severity *string `json:"severity"`
	Category *string `json:"category"`
	Path     *string `json:"path"`
	Line     *int    `json:"line"`
	Title    *string `json:"title"`
	Body     *string `json:"body"`
}

// ParseAnswer parses the model's answer text strictly: exactly one JSON object,
// with surrounding whitespace only, of the form
//
//	{"summary": "...", "findings": [{"severity": "blocker", "category": "correctness",
//	  "path": "internal/x/y.go", "line": 12, "title": "...", "body": "..."}]}
//
// summary, findings, and each finding's severity, title and body are required;
// category, path and line are optional. It returns an error wrapping
// ErrMalformedAnswer for: text before or after the object (a code fence
// included), a second value, an unknown field at any level, a missing or empty
// summary, title or body, a findings that is not an array, a severity outside
// the constants of this package (compared exactly, so "Blocker" is refused), a
// negative line, a line above 0 without a path, a path that is absolute or
// holds a ".." element, a backslash or a control character. The category is
// the one field that is not strict. It is normalised first (surrounding white
// space trimmed, lower-cased, every "_" and space turned into "-"), so
// "Readability" and "prompt_injection" are accepted categories. A normalised
// category that is still not accepted but contains "inject" or "prompt" is
// read as CategoryPromptInjection, so a near-miss spelling of it still
// blocks. Any other, and a missing or null one, keeps the finding with
// CategoryUncategorised, so one unknown category costs no finding. The raw
// category text is never kept. An answer with no findings is valid, findings
// then being empty, not nil. An error for the answer's shape, refused before
// any one finding is validated, is a shapeError, which Run asks again for
// (issue #316).
func ParseAnswer(text string) (Answer, error) {
	src := strings.NewReader(text)
	dec := json.NewDecoder(src)
	dec.DisallowUnknownFields()

	var raw rawAnswer
	// The decoder's error is dropped: it quotes the answer (shapeError).
	if dec.Decode(&raw) != nil {
		return Answer{}, newShapeError(checkNotOneObject)
	}

	// What the decoder buffered but did not use, then what it never read.
	rest, err := io.ReadAll(io.MultiReader(dec.Buffered(), src))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return Answer{}, newShapeError(checkTextAfter)
	}

	if raw.Summary == nil || strings.TrimSpace(*raw.Summary) == "" {
		return Answer{}, newShapeError(checkNoSummary)
	}

	if raw.Findings == nil {
		return Answer{}, newShapeError(checkNoFindings)
	}

	answer := Answer{Summary: *raw.Summary, Findings: make([]Finding, 0, len(*raw.Findings))}

	for i, rf := range *raw.Findings {
		f, err := rf.finding()
		if err != nil {
			return Answer{}, fmt.Errorf("%w: finding %d: %w", ErrMalformedAnswer, i+1, err)
		}

		answer.Findings = append(answer.Findings, f)
	}

	return answer, nil
}

// shapeError is a malformed answer refused for its shape, before any one
// finding is validated: no single JSON object of the answer's form (not JSON, a
// top level that is no object, a findings that is no array, an unknown field or
// a field of the wrong type), text around the object, no summary or no
// findings array. Run asks the model once more for such an answer (issue
// #316); a finding ParseAnswer refuses is no shapeError and is not asked again.
// It wraps ErrMalformedAnswer as every malformed answer's error does. check is
// the phrase of the shape check that failed, one of this package's constants
// and never text of the answer, which Run's retry notice names (issue #343).
// The error's text is ErrMalformedAnswer's and check's alone, and it wraps
// nothing else: the decoder's own error quotes what it could not read (a field
// name the model chose, a character of its answer), and the error reaches the
// job's public ::error line, so it is dropped rather than wrapped (issue #366).
type shapeError struct {
	check string
}

// The shape checks ParseAnswer makes, as its errors and the retry notice name
// them.
const (
	checkNotOneObject = "not one JSON object of the answer's form"
	checkTextAfter    = "text after the JSON object"
	checkNoSummary    = "no summary"
	checkNoFindings   = "no findings array"
)

// newShapeError returns the shapeError of check.
func newShapeError(check string) shapeError {
	return shapeError{check: check}
}

func (e shapeError) Error() string { return ErrMalformedAnswer.Error() + ": " + e.check }

func (e shapeError) Unwrap() error { return ErrMalformedAnswer }

// isShapeError reports whether err is, or wraps, a shapeError.
func isShapeError(err error) bool {
	_, ok := shapeCheck(err)

	return ok
}

// shapeCheck returns the phrase of the shape check err failed, and false when
// err is no shapeError and wraps none.
func shapeCheck(err error) (string, bool) {
	var shape shapeError
	if !errors.As(err, &shape) {
		return "", false
	}

	return shape.check, true
}

// finding validates rf and returns it as a Finding.
func (rf rawFinding) finding() (Finding, error) {
	if rf.Severity == nil || !slices.Contains(severities, Severity(*rf.Severity)) {
		return Finding{}, errors.New("missing or unknown severity")
	}

	category := CategoryUncategorised
	if rf.Category != nil {
		category = readCategory(*rf.Category)
	}

	if rf.Title == nil || strings.TrimSpace(*rf.Title) == "" {
		return Finding{}, errors.New("missing or empty title")
	}

	if rf.Body == nil || strings.TrimSpace(*rf.Body) == "" {
		return Finding{}, errors.New("missing or empty body")
	}

	f := Finding{
		Severity: Severity(*rf.Severity),
		Category: category,
		Title:    *rf.Title,
		Body:     *rf.Body,
	}

	if rf.Path != nil {
		f.Path = *rf.Path
	}

	if rf.Line != nil {
		f.Line = *rf.Line
	}

	if f.Line < 0 {
		return Finding{}, errors.New("negative line")
	}

	if f.Line > 0 && f.Path == "" {
		return Finding{}, errors.New("a line without a path")
	}

	if f.Path != "" && !validPath(f.Path) {
		return Finding{}, errors.New("a path that is absolute, holds a .. element, a backslash or a control character")
	}

	return f, nil
}

// readCategory reads the model's category leniently, so one category the model
// got wrong costs no finding (issue #292). raw is normalised: surrounding white
// space trimmed, lower-cased, every "_" and space turned into "-". A normalised
// value equal to an accepted category is that category. Otherwise one that
// contains "inject" or "prompt" is CategoryPromptInjection, so a near-miss
// spelling of it still blocks rather than failing open, even one that loses
// the word injection ("prompt-inj"); anything else is CategoryUncategorised.
// raw itself is never kept.
func readCategory(raw string) Category {
	normalised := strings.Map(func(r rune) rune {
		if r == '_' || r == ' ' {
			return '-'
		}

		return r
	}, strings.ToLower(strings.TrimSpace(raw)))

	if c := Category(normalised); slices.Contains(categories, c) {
		return c
	}

	if strings.Contains(normalised, "inject") || strings.Contains(normalised, "prompt") {
		return CategoryPromptInjection
	}

	return CategoryUncategorised
}

// validPath reports whether p is a slash-separated path relative to the
// repository root with no ".." element, no backslash and no control character
// (below 0x20, or DEL): git quotes a path holding one in a diff, so a path of
// the diff never holds one, and a line break would give the rest of the path a
// line of its own in the review.
func validPath(p string) bool {
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) ||
		strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7F }) {
		return false
	}

	return !slices.Contains(strings.Split(p, "/"), "..")
}
