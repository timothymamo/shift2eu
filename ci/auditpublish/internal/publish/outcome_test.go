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
			// The entry point's state, not its error: a run whose private
			// findings could not be filed must fail the job, whatever the
			// error it classifies.
			name:    "published, with no private finding filed and no public one",
			counts:  true,
			public:  0,
			private: "private findings: not filed",
			code:    5,
			summary: "private findings not filed: see runbook",
		},
		{
			name:    "published, with no private finding filed and three public ones",
			counts:  true,
			public:  3,
			private: "private findings: not filed",
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

// TestOutcomeOfTakesTheMarkOfItsOwnClass pins the rules of the walk that finds
// the check naming a refused run (publish.Refuse and OutcomeOf): the class
// decides the line, and only a mark of that class names it. An error's marks
// are walked depth first, each wrapper before what it wraps, and a joined
// error's parts in order (ADR 0040: the class order is scan, result, not
// filed, duplicates, previous, GitHub, configuration).
func TestOutcomeOfTakesTheMarkOfItsOwnClass(t *testing.T) {
	t.Parallel()

	// marked is a class sentinel refused by check c, as a call of the core or
	// of its GitHub side refuses one.
	marked := func(c publish.FailedCheck, class error) error {
		return publish.Refuse(c, fmt.Errorf("the call's own text: %w", class))
	}

	t.Run("a mark, over no error at all, refuses nothing", func(t *testing.T) {
		t.Parallel()

		if err := publish.Refuse(publish.CheckToken, nil); err != nil {
			t.Fatalf("Refuse(%v, nil) = %v, want nil", publish.CheckToken, err)
		}
	})

	for _, tc := range []struct {
		name string
		err  error
		// want is the one line the entry point prints, and code its exit code.
		want string
		code int
	}{
		{
			// The class order lists GitHub before configuration, so the joined
			// error is GitHub's and its own mark names the call, not the
			// configuration's mark beside it.
			name: "a GitHub mark joined with a configuration mark",
			err: errors.Join(
				marked(publish.CheckCreateIssue, publish.ErrGitHub),
				marked(publish.CheckRunID, publish.ErrConfig),
			),
			want: "GitHub call failed: creating the audit issue: see runbook",
			code: 8,
		},
		{
			name: "two joined marks of one class",
			err: errors.Join(
				marked(publish.CheckResultPath, publish.ErrConfig),
				marked(publish.CheckReportsPath, publish.ErrConfig),
			),
			want: "configuration invalid: the result's path not absolute and clean: see runbook",
			code: 9,
		},
		{
			// A mark adds no class of its own: the run is GitHub's, and the
			// outer mark, whose class is the scan's, is passed over for the
			// inner mark of the class the run does have.
			name: "an outer mark of another class over an inner mark",
			err: publish.Refuse(
				publish.CheckScanReport,
				marked(publish.CheckListIssues, publish.ErrGitHub),
			),
			want: "GitHub call failed: listing the audit issues: see runbook",
			code: 8,
		},
		{
			name: "an outer mark of the class over an inner mark of it",
			err: publish.Refuse(
				publish.CheckCreateIssue,
				marked(publish.CheckListIssues, publish.ErrGitHub),
			),
			want: "GitHub call failed: creating the audit issue: see runbook",
			code: 8,
		},
		{
			// A check the table does not name has no phrase: the line is the
			// class's alone.
			name: "a mark outside the check table",
			err:  publish.Refuse(publish.FailedCheck(200), publish.ErrConfig),
			want: "configuration invalid: see runbook",
			code: 9,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			outcome := publish.OutcomeOf(tc.err)

			if got := outcome.Summary(); got != tc.want {
				t.Errorf("OutcomeOf(%v).Summary() = %q, want %q", tc.err, got, tc.want)
			}

			if got := outcome.Code(); got != tc.code {
				t.Errorf("OutcomeOf(%v).Code() = %d, want %d (%q)", tc.err, got, tc.code, tc.want)
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

// TestOutcomePublishedKeepsNoCountOfPrivateFindings pins that a published
// summary says that private findings exist and never how many: the
// private-findings line holds one bit. Only the two states that publish, with
// code 0, reach such a summary: the not-filed state is outcome 5, which
// TestOutcomeOf pins.
func TestOutcomePublishedKeepsNoCountOfPrivateFindings(t *testing.T) {
	t.Parallel()

	lines := []string{
		"private findings: none",
		"private findings: handled privately",
	}

	for _, line := range lines {
		outcome := publish.OutcomeOf(nil).WithCounts(0, privateStateOf(t, line))

		if got := outcome.Code(); got != 0 {
			t.Errorf("the published outcome with %q exits %d, want 0", line, got)
		}

		if got := outcome.Summary(); !strings.Contains(got, line) {
			t.Errorf("the published summary %q does not hold %q", got, line)
		}
	}
}
