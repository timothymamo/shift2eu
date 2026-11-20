package review_test

import (
	"errors"
	"strings"
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
// line. The model's category is normalised before it is matched (issue #292):
// white space around it trimmed, lower-cased, every underscore and space turned
// into a hyphen, so " Correctness " is correctness and "Prompt_Injection" is
// prompt-injection, and a category that still holds "inject" or "prompt" is
// CategoryPromptInjection whatever else it says, so a misspelt prompt-injection
// finding keeps blocking even when the misspelling loses the word injection.
// A category the model left out, wrote as null or wrote
// outside the constants is kept as CategoryUncategorised, with the finding's
// other fields unchanged, so one unknown category costs no other finding. An
// answer with no findings is valid and its findings are empty, not nil, so a
// caller can range over them.
func TestParseAnswerReadsTheModelsAnswer(t *testing.T) {
	t.Parallel()

	// The answer of one finding whose category the model wrote as raw, and the
	// same answer once the category is read as want: the finding's other fields
	// are the same in both.
	withCategory := func(raw string) string {
		return `{"summary":"The change does what the issue asks.","findings":[` +
			`{"severity":"suggestion","category":"` + raw + `","path":"internal/x/y.go","line":3,` +
			`"title":"Off by one","body":"The loop ends one short."}]}`
	}

	readAs := func(want review.Category) review.Answer {
		return review.Answer{
			Summary: "The change does what the issue asks.",
			Findings: []review.Finding{{
				Severity: review.SeveritySuggestion,
				Category: want,
				Path:     "internal/x/y.go",
				Line:     3,
				Title:    "Off by one",
				Body:     "The loop ends one short.",
			}},
		}
	}

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
			name: "a finding with an unknown category is kept as uncategorised",
			text: `{"summary":"The change does what the issue asks.","findings":[` +
				`{"severity":"suggestion","category":"correctness","path":"internal/x/y.go","line":3,` +
				`"title":"Name it","body":"A clearer name would help."},` +
				`{"severity":"blocker","category":"style","path":"internal/x/y.go","line":9,` +
				`"title":"Off by one","body":"The loop ends one short."}]}`,
			want: review.Answer{
				Summary: "The change does what the issue asks.",
				Findings: []review.Finding{
					{
						Severity: review.SeveritySuggestion,
						Category: review.CategoryCorrectness,
						Path:     "internal/x/y.go",
						Line:     3,
						Title:    "Name it",
						Body:     "A clearer name would help.",
					},
					{
						Severity: review.SeverityBlocker,
						Category: review.CategoryUncategorised,
						Path:     "internal/x/y.go",
						Line:     9,
						Title:    "Off by one",
						Body:     "The loop ends one short.",
					},
				},
			},
		},
		{
			name: "a finding with no category is kept as uncategorised",
			text: `{"summary":"The change does what the issue asks.","findings":[` +
				`{"severity":"nit","path":"internal/x/y.go","line":5,"title":"Name it","body":"A clearer name would help."}]}`,
			want: review.Answer{
				Summary: "The change does what the issue asks.",
				Findings: []review.Finding{{
					Severity: review.SeverityNit,
					Category: review.CategoryUncategorised,
					Path:     "internal/x/y.go",
					Line:     5,
					Title:    "Name it",
					Body:     "A clearer name would help.",
				}},
			},
		},
		{
			name: "a finding whose category is null is kept as uncategorised",
			text: `{"summary":"The change does what the issue asks.","findings":[` +
				`{"severity":"nit","category":null,"title":"Name it","body":"A clearer name would help."}]}`,
			want: review.Answer{
				Summary: "The change does what the issue asks.",
				Findings: []review.Finding{{
					Severity: review.SeverityNit,
					Category: review.CategoryUncategorised,
					Title:    "Name it",
					Body:     "A clearer name would help.",
				}},
			},
		},
		{
			name: "the model's own uncategorised category is the fallback too",
			text: `{"summary":"The change does what the issue asks.","findings":[` +
				`{"severity":"nit","category":"uncategorised","title":"Name it","body":"A clearer name would help."}]}`,
			want: review.Answer{
				Summary: "The change does what the issue asks.",
				Findings: []review.Finding{{
					Severity: review.SeverityNit,
					Category: review.CategoryUncategorised,
					Title:    "Name it",
					Body:     "A clearer name would help.",
				}},
			},
		},
		{
			name: "a category with surrounding white space is that category",
			text: withCategory(" Correctness "),
			want: readAs(review.CategoryCorrectness),
		},
		{
			name: "a category in another case is that category",
			text: withCategory("Security"),
			want: readAs(review.CategorySecurity),
		},
		{
			name: "a category with an underscore is the category it spells",
			text: withCategory("Prompt_Injection"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "a category with a space is the category it spells",
			text: withCategory("prompt injection"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "an unknown category in another case and with an underscore is uncategorised",
			text: withCategory(" Unknown_Category"),
			want: readAs(review.CategoryUncategorised),
		},
		{
			name: "a misspelt prompt-injection category is still prompt-injection",
			text: withCategory("promptinjection"),
			want: readAs(review.CategoryPromptInjection),
		},
		// The next three cases pin the parser's fail-safe for a spelling that
		// loses the word injection altogether: an off-list category that still
		// names a prompt is read as prompt-injection too, so a typo in the
		// category never lets an instruction the diff carries through as an
		// ordinary finding (issue #292).
		{
			name: "a misspelt prompt-injection category that loses inject is still prompt-injection",
			text: withCategory("prompt-injetion"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "a truncated prompt-injection category is still prompt-injection",
			text: withCategory("prompt-inj"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "a misspelt prompt-injection category in another case and with a space is prompt-injection",
			text: withCategory("Prompt Injektion"),
			want: readAs(review.CategoryPromptInjection),
		},
		// A known residual of the rule above: an off-list category that names
		// neither a prompt nor an injection is uncategorised. Reading an off-list
		// jailbreak category as prompt-injection is out of scope for issue #292,
		// so this case pins that a later widening has to be a deliberate change.
		{
			name: "an off-list jailbreak category is uncategorised",
			text: withCategory("jailbreak"),
			want: readAs(review.CategoryUncategorised),
		},
		{
			name: "a bare injection category is prompt-injection",
			text: withCategory("injection"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "an injection word in a longer category is prompt-injection",
			text: withCategory("prompt-injection-attempt"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "an injection category in another case and with a space is prompt-injection",
			text: withCategory("Code Injection"),
			want: readAs(review.CategoryPromptInjection),
		},
		// The next three cases pin an over-match the parser makes on purpose: a
		// category off the accepted list that only names injection (an SQL
		// injection, say, which says nothing about the model's own answer) is
		// still read as prompt-injection, so a category the model got wrong
		// blocks rather than fails open. Narrowing this to the spellings of
		// prompt-injection alone must be a deliberate decision, not a side
		// effect of a later editing of readCategory.
		{
			name: "an off-list sql-injection category is prompt-injection",
			text: withCategory("sql-injection"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "an off-list command_injection category is prompt-injection",
			text: withCategory("command_injection"),
			want: readAs(review.CategoryPromptInjection),
		},
		{
			name: "an off-list Dependency Injection category is prompt-injection",
			text: withCategory("Dependency Injection"),
			want: readAs(review.CategoryPromptInjection),
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

// TestParseAnswerNamesTheFindingsCheckThatFailed pins issue #376: an answer with
// no findings field and one whose findings is null are both refused — the
// program cannot tell an answer that found nothing from one that lost its
// findings — but the two checks are named apart, so the retry notice and the
// skipped outcome of a recurrence say which one the model wrote. An empty array
// is the answer that found nothing, and is valid.
func TestParseAnswerNamesTheFindingsCheckThatFailed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		// wantEnd is the phrase the error's text must end with: the check that
		// failed, never text of the answer.
		wantEnd string
	}{
		{name: "an answer with no findings field", text: `{"summary":"s"}`, wantEnd: "missing findings"},
		{name: "an answer whose findings is null", text: `{"summary":"s","findings":null}`, wantEnd: "findings is null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer, err := review.ParseAnswer(tc.text)
			if !errors.Is(err, review.ErrMalformedAnswer) {
				t.Fatalf("ParseAnswer(%q) = %+v, %v; want an error wrapping ErrMalformedAnswer", tc.text, answer, err)
			}

			if !strings.HasSuffix(err.Error(), tc.wantEnd) {
				t.Errorf("ParseAnswer(%q) = %v, want an error whose text ends %q: the two findings checks are named apart, so the notice and the skipped outcome say which one it was (issue #376)", tc.text, err, tc.wantEnd)
			}
		})
	}

	// The answer of nothing to report is the one #376's prompt now shows.
	answer, err := review.ParseAnswer(`{"summary":"s","findings":[]}`)
	if err != nil {
		t.Fatalf("ParseAnswer of an answer with an empty findings array: %v; want it accepted, with no finding", err)
	}

	if len(answer.Findings) != 0 {
		t.Errorf("the answer with an empty findings array holds %d finding(s), want 0", len(answer.Findings))
	}
}

// TestParseAnswerClassifiesAMalformedAnswer pins issue #375: an answer ParseAnswer
// refuses for its shape is classified by a constant class, never by text of the
// answer, so the failure the job's public ::error line reports can be diagnosed
// — broken JSON, an unknown field, a field of the wrong type, no findings field
// — without the decoder's message, which quotes what it could not read (a field
// name the model chose, a character of its answer, issue #366). A broken JSON
// answer's class carries the byte offset *json.SyntaxError names, the answer's
// own byte position, so a maintainer can see where the answer went wrong; the
// class is the phrase alone otherwise, whatever the answer holds.
func TestParseAnswerClassifiesAMalformedAnswer(t *testing.T) {
	t.Parallel()

	// answerMarker375 stands for the model's own words. Each case plants it where
	// the decoder's message would quote it, so a class built from that message
	// fails the case rather than passing it.
	const answerMarker375 = "MRK375-THE-ANSWER-TEXT"

	cases := []struct {
		name string
		text string
		// want is the class the error's text must hold: a constant phrase, and
		// for broken JSON the answer's byte offset, counting from 1.
		want string
	}{
		{
			name: "broken JSON names the byte offset of the first byte it cannot read",
			// The "o" of [oops] is the answer's byte 50.
			text: `{"summary":"` + answerMarker375 + `", "findings":[oops]}`,
			want: "syntax at byte 50",
		},
		{
			// The unknown field's own name is the marker: the strict decoder names
			// it in full, which is exactly what the class must not do.
			name: "an unknown field",
			text: `{"summary":"s","findings":[],"` + answerMarker375 + `":1}`,
			want: "unknown field",
		},
		{
			name: "a field of the wrong type",
			text: `{"summary":["` + answerMarker375 + `"],"findings":[]}`,
			want: "wrong type",
		},
		{
			name: "no findings field",
			text: `{"summary":"` + answerMarker375 + `"}`,
			want: "missing findings",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer, err := review.ParseAnswer(tc.text)
			if !errors.Is(err, review.ErrMalformedAnswer) {
				t.Fatalf("ParseAnswer(%q) = %+v, %v; want an error wrapping ErrMalformedAnswer", tc.text, answer, err)
			}

			// The text of this error is the data of the job's ::error line
			// (ci/aireview's fail), so it holds the class and nothing of the answer.
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseAnswer(%q) = %v, want an error whose text holds the class %q: a failure is diagnosed by its class, never by the decoder's message (issue #375)", tc.text, err, tc.want)
			}

			for _, unwanted := range []string{answerMarker375, "json:", "Go struct field"} {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("ParseAnswer(%q) = %v, which holds %q: the class is a constant of this package and the error quotes neither the answer nor the decoder's message (issues #366, #375)", tc.text, err, unwanted)
				}
			}
		})
	}
}

// TestParseAnswerRefusesWhatItCannotTrust pins the answers ParseAnswer refuses
// with ErrMalformedAnswer: anything around the object, a second value, an
// unknown field at any level, a missing or empty required text, a severity
// outside the constants (compared exactly), and a line or path a pull request
// could not hold, a path holding a control character included: a line break in a
// path would put the rest of it on a line of its own, where an @ it holds
// notifies, and the GitHub API could not name that file anyway. A refused answer
// fails the run and posts nothing. The category is the one field that is not
// strict: a missing, null or unknown one keeps the finding as uncategorised
// (TestParseAnswerReadsTheModelsAnswer), while a broken severity, title, body,
// path or line still refuses the whole answer.
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
		{name: "findings that are null", text: `{"summary":"s","findings":null}`},
		{name: "findings that are not an array", text: `{"summary":"s","findings":{}}`},
		{name: "a finding without a title", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","body":"b"}]}`},
		{name: "an empty title", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","title":"","body":"b"}]}`},
		{name: "an empty body", text: `{"summary":"s","findings":[{"severity":"nit","category":"readability","title":"t","body":""}]}`},
		{name: "a severity in another case", text: `{"summary":"s","findings":[{"severity":"Blocker","category":"correctness","title":"t","body":"b"}]}`},
		{name: "an unknown severity", text: `{"summary":"s","findings":[{"severity":"critical","category":"correctness","title":"t","body":"b"}]}`},
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
		{name: "a valid finding next to a broken severity", text: `{"summary":"s","findings":[` + finding + `,{"severity":"critical","category":"style","title":"t","body":"b"}]}`},
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
