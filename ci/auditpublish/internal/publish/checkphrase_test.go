package publish_test

// The summary line names the refused check (issue #640). A refused run prints
// exactly one line, and that line says which check failed, in fixed words:
// "<class line>: <check phrase>: see runbook". The phrase is fixed English,
// never a value, a token, a key, a file path, a number the run read or a
// finding's text, so the maintainer's runbook step 11 can name it.
//
// This file is the fixed list of the core's lines: one row per refusal the
// core makes, each driven by the real function that makes it, with the class
// it wraps, its one line and its exit code. The GitHub side's and the entry
// point's tests hold the rows of their own refusals in the same shape.
//
// Issue #640's second half is the class change this file pins too: an error
// about the result's content is the result's class, "result refused" (exit 3),
// never "configuration invalid" (exit 9), which is the run's configuration
// alone.

import (
	"errors"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestCoreRefusalNamesTheFailedCheck pins the core's fixed list: every
// refusal's class, one line naming the check, and its exit code.
func TestCoreRefusalNamesTheFailedCheck(t *testing.T) {
	t.Parallel()

	// leakedFingerprint is a value of the shape of no fingerprint: a phrase
	// holds no value, so no line below may hold it.
	const leakedFingerprint = publish.Fingerprint("fp-ffffffffffff00")

	zeroKey := publish.FingerprintKey{}

	for _, tc := range []struct {
		name string
		// source is the refusal's site in the core.
		source string
		// class is the error class the refusal wraps: the class change of
		// issue #640 moves the result's content to the result's class.
		class error
		// want is the one line the entry point prints, and code its exit code.
		want string
		code int
		// drive makes the refusal through the real function and returns its
		// error.
		drive func(t *testing.T) error
	}{
		// The run's configuration: the run itself, the commit argument and
		// the fingerprint key.
		{
			name:   "the server URL is not https and a host name",
			source: "run.go: NewRun",
			class:  publish.ErrConfig,
			want:   "configuration invalid: GITHUB_SERVER_URL is https:// and a host name: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.NewRun("http://"+testRepository, testRepository, testRunID, testDate)

				return err
			},
		},
		{
			name:   "the repository is not owner/name",
			source: "run.go: NewRun",
			class:  publish.ErrConfig,
			want:   "configuration invalid: GITHUB_REPOSITORY is owner/name: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.NewRun(testServerURL, "shift2eu", testRunID, testDate)

				return err
			},
		},
		{
			name:   "the repository holds a dot segment",
			source: "run.go: NewRun",
			class:  publish.ErrConfig,
			want:   "configuration invalid: GITHUB_REPOSITORY holds a dot segment: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.NewRun(testServerURL, "example/..", testRunID, testDate)

				return err
			},
		},
		{
			name:   "the run ID is not a decimal of up to 20 digits",
			source: "run.go: NewRun",
			class:  publish.ErrConfig,
			want:   "configuration invalid: GITHUB_RUN_ID is a decimal of up to 20 digits: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.NewRun(testServerURL, testRepository, "12ab", testDate)

				return err
			},
		},
		{
			name:   "the run's date is not a real date",
			source: "run.go: NewRun",
			class:  publish.ErrConfig,
			want:   "configuration invalid: the run's date is a real YYYY-MM-DD: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.NewRun(testServerURL, testRepository, testRunID, "2026-02-30")

				return err
			},
		},
		{
			name:   "the commit argument is no commit",
			source: "result.go: ParseResult",
			class:  publish.ErrConfig,
			want:   "configuration invalid: the commit argument is 40 lowercase hex characters: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.ParseResult([]byte(`{}`), "0123")

				return err
			},
		},
		{
			name:   "the fingerprint key is short",
			source: "fingerprint.go: NewFingerprintKey",
			class:  publish.ErrConfig,
			want:   "configuration invalid: AUDIT_FINGERPRINT_KEY unset or shorter than 32 bytes: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.NewFingerprintKey([]byte(strings.Repeat("k", 31)))

				return err
			},
		},
		{
			name:   "the fingerprint key was never set",
			source: "fingerprint.go: KeyedFingerprint",
			class:  publish.ErrConfig,
			want:   "configuration invalid: AUDIT_FINGERPRINT_KEY unset or shorter than 32 bytes: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.KeyedFingerprint(zeroKey, "G309", "internal/cli/run.go", "os.Open")

				return err
			},
		},
		{
			name:   "the advisory scan's fingerprint key was never set",
			source: "scan.go: ScanAdvisory",
			class:  publish.ErrConfig,
			want:   "configuration invalid: AUDIT_FINGERPRINT_KEY unset or shorter than 32 bytes: see runbook",
			code:   9,
			drive: func(t *testing.T) error {
				t.Helper()

				return publish.ScanAdvisory("a text of an advisory", zeroKey)
			},
		},

		// The result's content: the result's class, exit 3.
		{
			name:   "the result is not the schema's",
			source: "result.go: ParseResult",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the result does not match the schema: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.ParseResult([]byte(`{}`), testCommit)

				return err
			},
		},
		{
			name:   "the private state is none of the three",
			source: "plan.go: RenderReport",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the private state is none of the three: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.RenderReport(publish.ReportInput{
					Result:  fixtureResult(),
					Run:     testRun(t, testDate, testRunID),
					Private: publish.PrivateState(99),
				})

				return err
			},
		},
		{
			name:   "a finding is private but the private state is none",
			source: "plan.go: RenderReport",
			class:  publish.ErrResultInvalid,
			want:   "result refused: a finding is private but the private state is none: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.RenderReport(publish.ReportInput{
					Result:  fixtureResult(advisoryFinding()),
					Run:     testRun(t, testDate, testRunID),
					Private: publish.PrivateNone,
				})

				return err
			},
		},
		{
			name:   "the private state is set but no finding is private",
			source: "plan.go: RenderReport",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the private state is set but no finding is private: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.RenderReport(publish.ReportInput{
					Result:  fixtureResult(),
					Run:     testRun(t, testDate, testRunID),
					Private: publish.PrivateHandled,
				})

				return err
			},
		},
		{
			name:   "the finding is not private",
			source: "advisory.go: RenderAdvisory",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the finding is not private: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				public := advisoryFinding()
				public.Severity, public.Category, public.Private = sevLow, catHardening, false

				_, err := publish.RenderAdvisory(
					publish.PrivateFinding{Finding: public, Fingerprint: advisoryFingerprint},
					publish.AdvisoryNew,
					testRun(t, testDate, testRunID),
				)

				return err
			},
		},
		{
			name:   "the fingerprint is no fingerprint",
			source: "advisory.go: RenderAdvisory",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the fingerprint is no fingerprint: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.RenderAdvisory(
					publish.PrivateFinding{Finding: advisoryFinding(), Fingerprint: leakedFingerprint},
					publish.AdvisoryNew,
					testRun(t, testDate, testRunID),
				)

				return err
			},
		},
		{
			name:   "the advisory status is not new, open or regressed",
			source: "advisory.go: RenderAdvisory",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the advisory status is new, open or regressed: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.RenderAdvisory(
					publish.PrivateFinding{Finding: advisoryFinding(), Fingerprint: advisoryFingerprint},
					publish.AdvisoryNotReproduced,
					testRun(t, testDate, testRunID),
				)

				return err
			},
		},
		{
			name:   "the advisory status is none of the four",
			source: "advisory.go: SetAdvisoryStatus",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the advisory status is one of the four: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.SetAdvisoryStatus("a body\n\nAudit status: open\n", publish.AdvisoryRegressed+1)

				return err
			},
		},
		{
			name:   "the advisory description holds no status line",
			source: "advisory.go: SetAdvisoryStatus",
			class:  publish.ErrResultInvalid,
			want:   "result refused: the advisory description holds a status line: see runbook",
			code:   3,
			drive: func(t *testing.T) error {
				t.Helper()

				_, err := publish.SetAdvisoryStatus("a body with no status line", publish.AdvisoryNotReproduced)

				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.drive(t)
			if err == nil {
				t.Fatalf("%s refused nothing, want an error wrapping %v", tc.source, tc.class)
			}

			if !errors.Is(err, tc.class) {
				t.Errorf("%s wraps %v, want it to wrap %v: the class decides the exit code", tc.source, err, tc.class)
			}

			outcome := publish.OutcomeOf(err)

			if got := outcome.Summary(); got != tc.want {
				t.Errorf("%s: the summary line is %q, want %q", tc.source, got, tc.want)
			}

			if got := outcome.Code(); got != tc.code {
				t.Errorf("%s: the exit code is %d, want %d (%q)", tc.source, got, tc.code, tc.want)
			}

			if strings.Contains(outcome.Summary(), string(leakedFingerprint)) {
				t.Errorf("%s: the summary %q holds a value, want the check's own fixed words", tc.source, outcome.Summary())
			}

			if !strings.HasSuffix(outcome.Summary(), ": see runbook") {
				t.Errorf("%s: the summary %q does not end with %q: every line points at the runbook",
					tc.source, outcome.Summary(), ": see runbook")
			}

			if !strings.HasPrefix(outcome.Summary(), classLine(t, tc.class)+": ") {
				t.Errorf("%s: the summary %q does not start with its class's line %q",
					tc.source, outcome.Summary(), classLine(t, tc.class)+": ")
			}
		})
	}
}

// classLine returns the class line the core's summary starts with for class:
// the fixed words ADR 0040's table gives the class, never an error's text.
func classLine(t *testing.T, class error) string {
	t.Helper()

	switch {
	case errors.Is(class, publish.ErrConfig):
		return "configuration invalid"
	case errors.Is(class, publish.ErrResultInvalid):
		return "result refused"
	}

	t.Fatalf("no class line is pinned for %v", class)

	return ""
}
