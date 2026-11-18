package review_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// parsedAnswer is the answer every "an answer ParseAnswer accepts" case is
// compared against, as the model writes it.
const parsedAnswer = `{"summary":"The change does what the issue asks.",` +
	`"findings":[{"severity":"blocker","category":"correctness","path":"internal/x/y.go","line":3,` +
	`"title":"Off by one","body":"The loop ends one short."}]}`

// TestParseAnswerReadsTheModelsAnswer pins the strict parser of ADR 0033: one
// JSON object with surrounding whitespace only, a summary, a findings array and
// each finding's severity, category, title and body, with an optional path and
// line. An answer with no findings is valid and its findings are empty, not nil,
// so a caller can range over them.
func TestParseAnswerReadsTheModelsAnswer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want review.Answer
	}{
		{
			name: "one finding",
			text: parsedAnswer,
			want: review.Answer{
				Summary: "The change does what the issue asks.",
				Findings: []review.Finding{{
					Severity: review.SeverityBlocker,
					Category: review.CategoryCorrectness,
					Path:     "internal/x/y.go",
					Line:     3,
					Title:    "Off by one",
					Body:     "The loop ends one short.",
				}},
			},
		},
		{
			name: "an answer with no findings",
			text: `{"summary":"Nothing to report.","findings":[]}`,
			want: review.Answer{Summary: "Nothing to report.", Findings: []review.Finding{}},
		},
		{
			name: "a finding with no position",
			text: `{"summary":"Nothing to point at.","findings":[{"severity":"nit","category":"readability",` +
				`"title":"Name it","body":"A clearer name would help."}]}`,
			want: review.Answer{
				Summary: "Nothing to point at.",
				Findings: []review.Finding{{
					Severity: review.SeverityNit,
					Category: review.CategoryReadability,
					Title:    "Name it",
					Body:     "A clearer name would help.",
				}},
			},
		},
		{
			name: "surrounding whitespace",
			text: "\n  " + parsedAnswer + "\n\t\n",
			want: review.Answer{
				Summary: "The change does what the issue asks.",
				Findings: []review.Finding{{
					Severity: review.SeverityBlocker,
					Category: review.CategoryCorrectness,
					Path:     "internal/x/y.go",
					Line:     3,
					Title:    "Off by one",
					Body:     "The loop ends one short.",
				}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := review.ParseAnswer(tc.text)
			if err != nil {
				t.Fatalf("ParseAnswer(%q): %v", tc.text, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseAnswer(%q) (-want +got):\n%s", tc.text, diff)
			}

			if got.Findings == nil {
				t.Errorf("ParseAnswer(%q) returned findings nil, want an empty slice", tc.text)
			}
		})
	}
}

// TestParseAnswerRefusesWhatItCannotTrust pins the answers ParseAnswer refuses
// with ErrMalformedAnswer: anything around the object, a second value, an
// unknown field at any level, a missing or empty required text, a severity or
// category outside the constants (compared exactly), and a line or path a pull
// request could not hold, a path holding a control character included: a line
// break in a path would put the rest of it on a line of its own, where an @ it
// holds notifies, and the GitHub API could not name that file anyway. A refused
// answer fails the run and posts nothing.
func TestParseAnswerRefusesWhatItCannotTrust(t *testing.T) {
	t.Parallel()

	const (
		prose   = "Sure! Here is my review: "
		fence   = "```json\n"
		finding = `{"severity":"nit","category":"readability","title":"Name it","body":"A clearer name."}`
	)

	// The control characters in a path are spelt as the JSON escapes the model
	// writes, since a Go escape such as \x01 is no JSON and the parser would then
	// refuse the answer for its JSON, not for its path.
	cases := []struct {
		name string
		text string
	}{
		{name: "text before the object", text: prose + parsedAnswer},
		{name: "text after the object", text: parsedAnswer + "\nThat is all."},
		{name: "a code fence around the object", text: fence + parsedAnswer + "\n```"},
		{name: "a second JSON value", text: parsedAnswer + "\n" + parsedAnswer},
		{name: "an empty answer", text: ""},
		{name: "an unknown field of the answer", text: `{"summary":"s","findings":[],"confidence":0.9}`},
		{name: "an unknown field of a finding", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","title":"t","body":"b","note":"x"}]}`},
		{name: "no summary", text: `{"findings":[]}`},
		{name: "an empty summary", text: `{"summary":"","findings":[]}`},
		{name: "no findings", text: `{"summary":"s"}`},
		{name: "findings that are not an array", text: `{"summary":"s","findings":{}}`},
		{name: "a finding without a title", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","body":"b"}]}`},
		{name: "an empty title", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","title":"","body":"b"}]}`},
		{name: "an empty body", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","title":"t","body":""}]}`},
		{name: "a severity in another case", text: `{"summary":"s","findings":[{"severity":"Blocker","category":"correctness","title":"t","body":"b"}]}`},
		{name: "an unknown severity", text: `{"summary":"s","findings":[{"severity":"critical","category":"correctness","title":"t","body":"b"}]}`},
		{name: "a category in another case", text: `{"summary":"s","findings":[{"severity":"nit","category":"Readability","title":"t","body":"b"}]}`},
		{name: "an unknown category", text: `{"summary":"s","findings":[{"severity":"nit","category":"style","title":"t","body":"b"}]}`},
		{name: "a negative line", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/x/y.go","line":-1,"title":"t","body":"b"}]}`},
		{name: "a line without a path", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","line":5,"title":"t","body":"b"}]}`},
		{name: "an absolute path", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"/etc/passwd","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a .. element", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/../x.go","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a backslash", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal\\x\\y.go","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a line feed", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/x/y.go\n","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a line feed and a mention", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"x.go\n\n@someone please look","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a carriage return", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/x/y.go\r","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a tab", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/x/y.go\t","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with a control character below 0x20", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/x/y.go\u0001","line":1,"title":"t","body":"b"}]}`},
		{name: "a path with DEL", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","path":"internal/x/y.go\u007f","line":1,"title":"t","body":"b"}]}`},
		{name: "a finding that is not an object", text: `{"summary":"s","findings":["a finding"]}`},
		{name: "a valid finding next to a broken one", text: `{"summary":"s","findings":[` + finding + `,{"severity":"nit","category":"style","title":"t","body":"b"}]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer, err := review.ParseAnswer(tc.text)
			if !errors.Is(err, review.ErrMalformedAnswer) {
				t.Fatalf("ParseAnswer(%q) = %+v, %v; want an error wrapping ErrMalformedAnswer", tc.text, answer, err)
			}
		})
	}
}
