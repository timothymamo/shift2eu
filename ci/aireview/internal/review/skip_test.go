package review_test

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// answerMarker376 is the text the unusable answers of issue #376 carry. It
// stands for the model's own words, which no part of the skipped outcome ever
// repeats: no text of the answer reaches the pull request, the log or the
// skip's message.
const answerMarker376 = "MRK376-THE-ANSWER-TEXT"

// noticeLine is one GitHub workflow command line: ::notice followed by an
// optional title, then the text, all on one line (AnnotationData escapes what
// would end it).
var noticeLine = regexp.MustCompile(`^::notice(?: [^:\n]*)?::.*$`)

// TestRunSkipsWhenTheModelGivesNoUsableAnswerTwice pins the other cause of the
// skipped outcome (issue #376, ADR 0033): an answer with no findings field, or
// one whose findings is null, has the wrong shape, is asked once more with the
// same prompt (issue #316), and when the second answer has the wrong shape too
// the run posts one COMMENT review that says the review was skipped and that
// the model gave no usable answer twice, prints its one retry notice, and exits
// 0 — a green check, told apart from a review by the review and the notice. The
// answer is asked twice and no more, and no text of it is repeated.
func TestRunSkipsWhenTheModelGivesNoUsableAnswerTwice(t *testing.T) {
	t.Parallel()

	pr := splitPR()

	cases := []struct {
		name string
		// answer is the answer the model gives every time.
		answer string
		// cancelAfterSecondAnswer ends the run's context once the model has
		// answered twice, as a run whose deadline passes between the second
		// answer and the post does.
		cancelAfterSecondAnswer bool
	}{
		{
			name:   "an answer with no findings field",
			answer: `{"summary":"` + answerMarker376 + `"}`,
		},
		{
			name:   "an answer whose findings is null",
			answer: `{"summary":"` + answerMarker376 + `","findings":null}`,
		},
		{
			// The skip's review is the maintainer's only sign of two spent model
			// calls, so it is posted on a context the run's cancellation does not
			// reach (issue #316, reviewer m3).
			name:                    "an answer asked twice just as the run's context ends",
			answer:                  `{"summary":"` + answerMarker376 + `"}`,
			cancelAfterSecondAnswer: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{
				pr:    pr,
				diff:  cleanDiff,
				files: []review.ChangedFile{{Path: "internal/x/y.go", Status: "modified", Additions: 1}},
			}

			model := &fakeModel{answer: tc.answer}

			ctx := t.Context()

			if tc.cancelAfterSecondAnswer {
				cancelled, cancel := context.WithCancel(ctx)
				defer cancel()

				model.afterAsk = func() {
					if model.asked == 2 {
						cancel()
					}
				}
				ctx = cancelled
			}

			var log bytes.Buffer

			result, err := review.Run(ctx, gh, model, review.Config{
				Number:       pr.Number,
				BotLogin:     testBotLogin,
				Instructions: splitInstructions,
				Notices:      &log,
			})
			if err != nil {
				t.Fatalf("Run: %v; want no error: two unusable answers skip the review, they do not fail the check (issue #376)", err)
			}

			if model.asked != 2 {
				t.Errorf("the model was asked %d times, want 2: an answer of the wrong shape is asked once more, and once more only (issues #316, #376)", model.asked)
			}

			if result.Skip == nil {
				t.Fatalf("Result.Skip = nil, want a skip: the model gave no usable answer twice (issue #376)")
			}

			if result.Skip.Reason != review.SkipNoUsableAnswer {
				t.Errorf("Result.Skip.Reason = %q, want %q", result.Skip.Reason, review.SkipNoUsableAnswer)
			}

			if result.Skip.Message == "" {
				t.Error("Result.Skip.Message is empty: the skip says that the model gave no usable answer twice")
			}

			// The retry is announced once, in one workflow command line.
			written := log.String()
			if !strings.HasSuffix(written, "\n") {
				t.Errorf("the run's log is %q, which does not end its line with a line break: a workflow command is one line", written)
			}

			lines := strings.Split(strings.TrimSuffix(written, "\n"), "\n")
			if len(lines) != 1 || !noticeLine.MatchString(lines[0]) {
				t.Errorf("the run wrote %d log line(s) (%q), want one ::notice line: the retry of the second answer is announced, and no text of the answer reaches the log (issues #343, #366)", len(lines), written)
			}

			if len(gh.posts) != 1 {
				t.Fatalf("the run posted %d reviews, want 1: a skipped review says so on the pull request (issue #376)", len(gh.posts))
			}

			post := gh.posts[0]

			if post.review.Event != review.EventComment {
				t.Errorf("the skipped review's Event = %q, want %q: a skip is never a change request", post.review.Event, review.EventComment)
			}

			if len(post.review.Comments) != 0 {
				t.Errorf("the skipped review holds %d inline comments, want none: a skip invents no finding", len(post.review.Comments))
			}

			text := strings.ToLower(post.review.Body)

			for _, want := range []string{"skipped", "no usable answer twice"} {
				if !strings.Contains(text, want) {
					t.Errorf("the skipped review does not say %q; body:\n%s", want, post.review.Body)
				}
			}

			// The whole diff was reviewed in one part, and it gave no usable
			// answer: no other part was reviewed, so the review leaves no
			// finding of a part out, says nothing of any and holds no cue to
			// review another part by hand (issue #367).
			for _, unwanted := range []string{"findings are not shown", "other part", "review that part by hand", "review those parts by hand"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("the skipped review of a diff of one part says %q, want no word of other parts: no other part was reviewed, so there is no finding of another part to review by hand (issue #367); body:\n%s", unwanted, post.review.Body)
				}
			}

			// The skip of two unusable answers warns of the one cause a pull
			// request itself can fix: content in the diff that makes the model
			// answer unusably, a prompt injection among it (issue #376).
			if !strings.Contains(text, "prompt injection") {
				t.Errorf("the skipped review does not warn that content in the diff can make the model answer unusably, in a sentence that names prompt injection (issue #376); body:\n%s", post.review.Body)
			}

			for _, text := range []string{post.review.Body, log.String(), result.Skip.Message} {
				if strings.Contains(text, answerMarker376) {
					t.Errorf("the skipped outcome repeats %q, text of the model's answer: neither the review, nor the log, nor the skip's message holds any of it (issues #366, #376); text: %q",
						answerMarker376, text)
				}
			}

			if len(gh.dismissals) != 0 {
				t.Errorf("the skipped run dismissed %d review(s), want none: a skip is no finding, so nothing is withdrawn (issues #316, #376)", len(gh.dismissals))
			}
		})
	}
}

// cliAnswer returns what the fake Claude Code CLI prints for --output-format
// json when the model's answer is answer: the CLI's own result envelope around
// it (claudecli_test.go pins the envelope itself).
func cliAnswer(t *testing.T, answer string) string {
	t.Helper()

	encoded, err := json.Marshal(struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}{Type: "result", Subtype: "success", IsError: false, Result: answer})
	if err != nil {
		t.Fatalf("encode the fake CLI's answer: %v", err)
	}

	return string(encoded)
}

// TestRunSkipsNoUsableAnswerTwiceThroughTheCLI pins issue #376 through the
// Claude Code CLI a whole run asks: a CLI that answers twice with an answer that
// has no findings field, or one whose findings is null, ends the run in the
// skipped outcome — the retry notice names the shape check that failed ("no
// findings field" or "findings is null", the two checks of #376), the pull
// request gets one COMMENT review that says it was skipped, the run returns no
// error, and no text of either answer is repeated.
func TestRunSkipsNoUsableAnswerTwiceThroughTheCLI(t *testing.T) {
	t.Parallel()

	pr := splitPR()

	cases := []struct {
		name string
		// answer is the model's answer, as the CLI's result carries it.
		answer string
		// wantCheck is the shape check the retry notice must name.
		wantCheck string
	}{
		{
			name:      "an answer with no findings field",
			answer:    `{"summary":"` + answerMarker376 + `"}`,
			wantCheck: "no findings field",
		},
		{
			name:      "an answer whose findings is null",
			answer:    `{"summary":"` + answerMarker376 + `","findings":null}`,
			wantCheck: "findings is null",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli, _ := newFakeClaudeCLI(t, claudeFake{stdout: cliAnswer(t, tc.answer)})

			gh := &fakeGitHub{
				pr:    pr,
				diff:  cleanDiff,
				files: []review.ChangedFile{{Path: "internal/x/y.go", Status: "modified", Additions: 1}},
			}

			var log bytes.Buffer

			result, err := review.Run(t.Context(), gh, review.NewClaudeCLI(cli, testModelToken, time.Minute), review.Config{
				Number:       pr.Number,
				BotLogin:     testBotLogin,
				Instructions: splitInstructions,
				Notices:      &log,
			})
			if err != nil {
				t.Fatalf("Run: %v; want no error: two unusable answers skip the review, they do not fail the check (issue #376)", err)
			}

			if result.Skip == nil {
				t.Fatalf("Result.Skip = nil, want a skip: the CLI gave no usable answer twice (issue #376)")
			}

			if result.Skip.Reason != review.SkipNoUsableAnswer {
				t.Errorf("Result.Skip.Reason = %q, want %q", result.Skip.Reason, review.SkipNoUsableAnswer)
			}

			// One notice is the sign of the one retry: the CLI answered twice.
			written := log.String()
			lines := strings.Split(strings.TrimSuffix(written, "\n"), "\n")
			if len(lines) != 1 || !noticeLine.MatchString(lines[0]) {
				t.Fatalf("the run wrote %d log line(s) (%q), want one ::notice line: the run asked the CLI twice, so it announces the one retry (issues #343, #376)", len(lines), written)
			}

			if !strings.Contains(lines[0], tc.wantCheck) {
				t.Errorf("the retry notice %q does not name the shape check %q: the two findings checks are named apart, so the notice says which one failed (issue #376)", lines[0], tc.wantCheck)
			}

			if len(gh.posts) != 1 {
				t.Fatalf("the run posted %d reviews, want 1: a skipped review says so on the pull request (issue #376)", len(gh.posts))
			}

			post := gh.posts[0]

			if post.review.Event != review.EventComment {
				t.Errorf("the skipped review's Event = %q, want %q", post.review.Event, review.EventComment)
			}

			text := strings.ToLower(post.review.Body)

			for _, want := range []string{"skipped", "no usable answer twice"} {
				if !strings.Contains(text, want) {
					t.Errorf("the skipped review does not say %q; body:\n%s", want, post.review.Body)
				}
			}

			for _, text := range []string{post.review.Body, written, result.Skip.Message} {
				if strings.Contains(text, answerMarker376) {
					t.Errorf("the skipped outcome repeats %q, text of the model's answer: neither the review, nor the log, nor the skip's message holds any of it (issues #366, #376); text: %q",
						answerMarker376, text)
				}
			}
		})
	}
}

// TestSkipAnnotation pins the warning a skipped run prints (issue #367, ADR
// 0033): one line, a workflow command with the title "AI review skipped", whose
// data is the escaped message — "%" as %25 first, CR as %0D and LF as %0A, as
// every annotation of this package escapes its data — so the line cannot end
// early or start a command of its own.
func TestSkipAnnotation(t *testing.T) {
	t.Parallel()

	const prefix = "::warning title=AI review skipped::"

	cases := []struct {
		name string
		skip review.Skip
		// wantTail is the escaped message the annotation must end with.
		wantTail string
	}{
		{
			name:     "a plain message",
			skip:     review.Skip{Reason: review.SkipTooLarge, Message: "the diff is over the limit"},
			wantTail: "the diff is over the limit",
		},
		{
			name:     "a percent",
			skip:     review.Skip{Reason: review.SkipTooLarge, Message: "100% of 300,000 bytes"},
			wantTail: "100%25 of 300,000 bytes",
		},
		{
			name:     "a carriage return and a line feed",
			skip:     review.Skip{Reason: review.SkipNoUsableAnswer, Message: "the model gave\r\nno usable answer twice"},
			wantTail: "the model gave%0D%0Ano usable answer twice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := review.SkipAnnotation(tc.skip)

			if !strings.HasPrefix(got, prefix) {
				t.Errorf("SkipAnnotation = %q, want it to start with %q: the warning names the cause in the run's annotation", got, prefix)
			}

			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("SkipAnnotation = %q, which holds a raw CR or LF: the warning is one workflow command line, so its data is escaped (ADR 0033)", got)
			}

			if !strings.HasSuffix(got, tc.wantTail) {
				t.Errorf("SkipAnnotation = %q, want it to end with %q: the message is escaped as the data of a workflow command (%%, CR and LF)", got, tc.wantTail)
			}
		})
	}
}

// TestSkipSummaryLine pins the job summary line of a skipped run (issue #367,
// ADR 0033): one line, so appending it to $GITHUB_STEP_SUMMARY adds one line,
// saying the AI review was skipped so the run says so away from its log too.
func TestSkipSummaryLine(t *testing.T) {
	t.Parallel()

	for _, skip := range []review.Skip{
		{Reason: review.SkipTooLarge, Message: "the diff is over the limit"},
		{Reason: review.SkipNoUsableAnswer, Message: "the model gave no usable answer twice"},
	} {
		line := review.SkipSummaryLine(skip)

		if line == "" {
			t.Fatalf("SkipSummaryLine(%+v) is empty: the job summary says the review was skipped", skip)
		}

		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("SkipSummaryLine(%+v) = %q, which holds a raw CR or LF: it is one line of the job summary", skip, line)
		}

		if !strings.Contains(strings.ToLower(line), "skip") {
			t.Errorf("SkipSummaryLine(%+v) = %q, which does not say the review was skipped: the maintainer tells a skipped review apart from a review by the review, the warning and this line (issue #367)", skip, line)
		}
	}
}
