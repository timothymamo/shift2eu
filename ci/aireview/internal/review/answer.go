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

// The severities and categories ParseAnswer accepts, compared exactly.
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
	src := strings.NewReader(text)
	dec := json.NewDecoder(src)
	dec.DisallowUnknownFields()

	var raw rawAnswer
	if err := dec.Decode(&raw); err != nil {
		return Answer{}, fmt.Errorf("%w: not one JSON object of the answer's form: %w", ErrMalformedAnswer, err)
	}

	// What the decoder buffered but did not use, then what it never read.
	rest, err := io.ReadAll(io.MultiReader(dec.Buffered(), src))
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return Answer{}, fmt.Errorf("%w: text after the JSON object", ErrMalformedAnswer)
	}

	if raw.Summary == nil || strings.TrimSpace(*raw.Summary) == "" {
		return Answer{}, fmt.Errorf("%w: no summary", ErrMalformedAnswer)
	}

	if raw.Findings == nil {
		return Answer{}, fmt.Errorf("%w: no findings array", ErrMalformedAnswer)
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

// finding validates rf and returns it as a Finding.
func (rf rawFinding) finding() (Finding, error) {
	if rf.Severity == nil || !slices.Contains(severities, Severity(*rf.Severity)) {
		return Finding{}, errors.New("missing or unknown severity")
	}

	if rf.Category == nil || !slices.Contains(categories, Category(*rf.Category)) {
		return Finding{}, errors.New("missing or unknown category")
	}

	if rf.Title == nil || strings.TrimSpace(*rf.Title) == "" {
		return Finding{}, errors.New("missing or empty title")
	}

	if rf.Body == nil || strings.TrimSpace(*rf.Body) == "" {
		return Finding{}, errors.New("missing or empty body")
	}

	f := Finding{
		Severity: Severity(*rf.Severity),
		Category: Category(*rf.Category),
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
		return Finding{}, errors.New("a path that is absolute, holds a .. element or a backslash")
	}

	return f, nil
}

// validPath reports whether p is a slash-separated path relative to the
// repository root with no ".." element and no backslash.
func validPath(p string) bool {
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return false
	}

	return !slices.Contains(strings.Split(p, "/"), "..")
}
