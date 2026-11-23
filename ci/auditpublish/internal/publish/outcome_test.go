package publish_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// privateStateOf returns the PrivateState whose private-findings line is line.
func privateStateOf(t *testing.T, line string) publish.PrivateState {
	t.Helper()

	switch line {
	case "private findings: none":
		return publish.PrivateNone
	case "private findings: handled privately":
		return publish.PrivateHandled
	case "private findings: not filed":
		return publish.PrivateNotFiled
	}

	t.Fatalf("no private-findings line reads %q", line)

	return publish.PrivateNone
}

// TestOutcomeOf pins the exit-code table of ADR 0040: each class of error is
// one outcome with one exit code and one summary line, and code 2 is left to
// the Go runtime's panic exit.
func TestOutcomeOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// err is the error the entry point classifies; nil is a published run.
		err error
		// counts are set on the published outcome alone.
		counts  bool
		public  int
		private string
		code    int
		summary string
	}{
		{
			name:    "published, with a private finding handled",
			counts:  true,
			public:  3,
			private: "private findings: handled privately",
			code:    0,
			summary: "audit published: public findings 3, private findings: handled privately",
		},
		{
			name:    "published, with no private finding",
			counts:  true,
			public:  0,
			private: "private findings: none",
			code:    0,
			summary: "audit published: public findings 0, private findings: none",
		},
		{
			name:    "any other error",
			err:     errors.New("something broke"),
			code:    1,
			summary: "publish failed: see runbook",
		},
		{
			name:    "a result that does not parse",
			err:     fmt.Errorf("findings[0].scenario: %w", publish.ErrResultInvalid),
			code:    3,
			summary: "result refused: see runbook",
		},
		{
			name:    "a scan that refused",
			err:     fmt.Errorf("the report: %w", publish.ErrScanRefused),
			code:    4,
			summary: "publish refused: see runbook",
		},
		{
			name:    "private findings not filed",
			err:     publish.ErrPrivateNotFiled,
			code:    5,
			summary: "private findings not filed: see runbook",
		},
		{
			name:    "two draft advisories with one fingerprint",
			err:     publish.ErrDuplicateDrafts,
			code:    6,
			summary: "publish refused: duplicate draft advisories: see runbook step 11",
		},
		{
			name:    "a previous report the core cannot read",
			err:     fmt.Errorf("audits: %w", publish.ErrPreviousReport),
			code:    7,
			summary: "previous report unreadable: see runbook",
		},
		{
			name:    "a GitHub call that failed",
			err:     publish.ErrGitHub,
			code:    8,
			summary: "GitHub call failed: see runbook",
		},
		{
			name:    "a value the run did not configure",
			err:     fmt.Errorf("GITHUB_RUN_ID: %w", publish.ErrConfig),
			code:    9,
			summary: "configuration invalid: see runbook",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			outcome := publish.OutcomeOf(tc.err)

			if tc.counts {
				outcome = outcome.WithCounts(tc.public, privateStateOf(t, tc.private))
			}

			if got := outcome.Code(); got != tc.code {
				t.Errorf("OutcomeOf(%v).Code() = %d, want %d", tc.err, got, tc.code)
			}

			if got := outcome.Summary(); got != tc.summary {
				t.Errorf("OutcomeOf(%v).Summary() = %q, want %q", tc.err, got, tc.summary)
			}

			if tc.code == 2 {
				t.Errorf("an outcome exits 2, which is the Go runtime's panic exit")
			}
		})
	}
}

// TestOutcomeSummaryNamesNoText pins that the one line the entry point prints
// holds no error's text and no finding's text, whatever the error carries.
func TestOutcomeSummaryNamesNoText(t *testing.T) {
	t.Parallel()

	secret := "sk-ant-" + strings.Repeat("abcDEF123_-", 3)
	sentinel := "the-sentinel-value"

	wrapped := func(err error) error {
		return fmt.Errorf("the text holds %s and %s: %w", sentinel, secret, err)
	}

	errs := []error{
		wrapped(publish.ErrResultInvalid),
		wrapped(publish.ErrScanRefused),
		wrapped(publish.ErrPreviousReport),
		wrapped(publish.ErrConfig),
		wrapped(publish.ErrGitHub),
		wrapped(publish.ErrDuplicateDrafts),
		wrapped(publish.ErrPrivateNotFiled),
		wrapped(errors.New("anything else")),
	}

	for _, err := range errs {
		outcome := publish.OutcomeOf(err)

		summary := outcome.Summary()

		if summary == "" {
			t.Fatalf("the summary of %v is empty, want the rule's own words", err)
		}

		for _, text := range []string{sentinel, secret, err.Error()} {
			if strings.Contains(summary, text) {
				t.Errorf("the summary %q holds %q, want the rule's own words only", summary, text)
			}
		}
	}
}

// TestOutcomePublishedKeepsNoCountOfPrivateFindings pins that the published
// summary says that private findings exist and never how many: the three
// private-findings lines hold one bit each.
func TestOutcomePublishedKeepsNoCountOfPrivateFindings(t *testing.T) {
	t.Parallel()

	lines := []string{
		"private findings: none",
		"private findings: handled privately",
		"private findings: not filed",
	}

	for _, line := range lines {
		outcome := publish.OutcomeOf(nil).WithCounts(0, privateStateOf(t, line))

		if got := outcome.Summary(); !strings.Contains(got, line) {
			t.Errorf("the published summary %q does not hold %q", got, line)
		}
	}
}
