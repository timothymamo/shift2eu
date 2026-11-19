package review_test

import (
	"testing"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// TestFindingBlocking pins what blocks a merge (ADR 0033): a finding of
// severity blocker, or any prompt-injection finding whatever severity the model
// gave it. The program decides the review's event from this, never the model.
func TestFindingBlocking(t *testing.T) {
	t.Parallel()

	findings := []struct {
		name    string
		finding review.Finding
		want    bool
	}{
		{
			name:    "a blocker is blocking",
			finding: review.Finding{Severity: review.SeverityBlocker, Category: review.CategoryCorrectness},
			want:    true,
		},
		{
			name:    "a prompt-injection finding is blocking whatever its severity",
			finding: review.Finding{Severity: review.SeverityNit, Category: review.CategoryPromptInjection},
			want:    true,
		},
		{
			name:    "a blocker of category prompt-injection is blocking",
			finding: review.Finding{Severity: review.SeverityBlocker, Category: review.CategoryPromptInjection},
			want:    true,
		},
		{
			name:    "a suggestion is not blocking",
			finding: review.Finding{Severity: review.SeveritySuggestion, Category: review.CategoryReadability},
			want:    false,
		},
		{
			name:    "a nit is not blocking",
			finding: review.Finding{Severity: review.SeverityNit, Category: review.CategoryTests},
			want:    false,
		},
	}

	for _, tc := range findings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.finding.Blocking(); got != tc.want {
				t.Errorf("%+v.Blocking() = %t, want %t", tc.finding, got, tc.want)
			}
		})
	}
}

// TestDecideReturnsTheEventTheProgramChooses pins the switch of ADR 0033:
// REQUEST_CHANGES only when a finding is blocking and AI_REVIEW_REQUEST_CHANGES
// is on; COMMENT otherwise. The model never names the event.
func TestDecideReturnsTheEventTheProgramChooses(t *testing.T) {
	t.Parallel()

	blocking := review.Finding{Severity: review.SeverityBlocker, Category: review.CategoryCorrectness, Title: "t", Body: "b"}
	injection := review.Finding{Severity: review.SeveritySuggestion, Category: review.CategoryPromptInjection, Title: "t", Body: "b"}
	suggestion := review.Finding{Severity: review.SeveritySuggestion, Category: review.CategoryReadability, Title: "t", Body: "b"}

	cases := []struct {
		name           string
		findings       []review.Finding
		requestChanges bool
		want           review.Event
	}{
		{
			name:           "a blocking finding with the switch on requests changes",
			findings:       []review.Finding{blocking},
			requestChanges: true,
			want:           review.EventRequestChanges,
		},
		{
			name:           "a blocking finding with the switch off is a comment",
			findings:       []review.Finding{blocking},
			requestChanges: false,
			want:           review.EventComment,
		},
		{
			name:           "a prompt-injection finding with the switch on requests changes",
			findings:       []review.Finding{suggestion, injection},
			requestChanges: true,
			want:           review.EventRequestChanges,
		},
		{
			name: "an uncategorised blocker with the switch on requests changes",
			findings: []review.Finding{{
				Severity: review.SeverityBlocker,
				Category: review.CategoryUncategorised,
				Title:    "t",
				Body:     "b",
			}},
			requestChanges: true,
			want:           review.EventRequestChanges,
		},
		{
			name: "an uncategorised blocker with the switch off is a comment",
			findings: []review.Finding{{
				Severity: review.SeverityBlocker,
				Category: review.CategoryUncategorised,
				Title:    "t",
				Body:     "b",
			}},
			requestChanges: false,
			want:           review.EventComment,
		},
		{
			name:           "no blocking finding with the switch on is a comment",
			findings:       []review.Finding{suggestion},
			requestChanges: true,
			want:           review.EventComment,
		},
		{
			name:           "no blocking finding with the switch off is a comment",
			findings:       []review.Finding{suggestion},
			requestChanges: false,
			want:           review.EventComment,
		},
		{
			name:           "no findings with the switch on is a comment",
			findings:       nil,
			requestChanges: true,
			want:           review.EventComment,
		},
		{
			name:           "no findings with the switch off is a comment",
			findings:       nil,
			requestChanges: false,
			want:           review.EventComment,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := review.Decide(tc.findings, tc.requestChanges); got != tc.want {
				t.Errorf("Decide(%+v, %t) = %q, want %q", tc.findings, tc.requestChanges, got, tc.want)
			}
		})
	}
}
