package publish_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// coverageNames are the checks as ADR 0040 renders them, in the coverage
// table's order.
var coverageNames = map[string]string{
	"dependencies":           "govulncheck (source) and osv-scanner",
	"history-secrets":        "gitleaks on the full history",
	"workflows":              "zizmor and actionlint",
	"scorecard":              "OpenSSF scorecard",
	"scorecard-admin":        "OpenSSF scorecard checks that need admin read",
	"release-integrity":      "Release checksums, attestations and signatures",
	"image-pull":             "Image pulls from GHCR",
	"binary-vulnerabilities": "govulncheck on the release binaries",
	"image-scan":             "trivy on the image and its SBOM",
	"image-runtime":          "The image's runtime settings",
	"hostile-inventories":    "The CLI on hostile inventory files",
	"fuzzing":                "Fuzzing of the committed `Fuzz*` targets",
	"supply-chain":           "Supply-chain reads",
	"tree-review":            "The security-audit checklist on the whole tree",
	"agent-surface":          "What the agents may run",
}

// TestCoverage pins the Coverage table: one row per check id in the ADR's
// order, named as the ADR names it, reading findings only when the check ran
// and a kept finding of it is public or needs investigation, run when it ran
// otherwise, and not run (<why>) from the fixed vocabulary when it did not.
func TestCoverage(t *testing.T) {
	t.Parallel()

	public := finding("G304", sevLow, true, catHardening)

	private := finding("CLI-hostile-inventory", sevMedium, true, catDependency)
	private.Check = "hostile-inventories"

	tests := []struct {
		name     string
		checks   []publish.Check
		findings []publish.Finding
		private  publish.PrivateState
		// coverage is the value of each check's row, in the table's order.
		coverage []string
	}{
		{
			name:     "every check ran, one produced a public finding",
			checks:   ranChecks(),
			findings: []publish.Finding{public},
			coverage: ranThen("tree-review", "findings"),
		},
		{
			// The PRD's named case: one private finding from the CLI's hostile
			// inventories reads exactly as a check with no finding.
			name:     "a check whose only finding is private reads run",
			checks:   ranChecks(),
			findings: []publish.Finding{private},
			private:  publish.PrivateHandled,
			coverage: ranThen("", ""),
		},
		{
			name:     "a check with a public and a private finding reads findings",
			checks:   ranChecks(),
			findings: []publish.Finding{public, private},
			private:  publish.PrivateHandled,
			coverage: ranThen("tree-review", "findings"),
		},
		{
			name: "every why id is rendered",
			checks: checksWithNotRun(map[string]string{
				"dependencies":      "timed-out",
				"workflows":         "tool-failed",
				"scorecard-admin":   "needs-admin-read",
				"release-integrity": "results-missing",
				"image-pull":        "image-not-pulled",
				"fuzzing":           "no-fuzz-targets",
				"supply-chain":      "read-refused",
				"tree-review":       "declined",
			}),
			coverage: []string{
				"not run (timed out)",
				"run",
				"not run (tool failed)",
				"run",
				"not run (needs admin read)",
				"not run (results missing)",
				"not run (image not pulled)",
				"run",
				"run",
				"run",
				"run",
				"not run (no `Fuzz*` targets)",
				"not run (read refused)",
				"not run (declined)",
				"run",
			},
		},
		{
			name:     "a finding that needs investigation is a public finding too",
			checks:   ranChecks(),
			findings: []publish.Finding{finding("G309", sevLow, false, catInfoDisclosure)},
			coverage: ranThen("tree-review", "findings"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := fixtureResult(tc.findings...)
			result.Checks = tc.checks

			body := renderReport(t, publish.ReportInput{
				Result:  result,
				Run:     testRun(t, testDate, testRunID),
				Private: tc.private,
			}).Body

			want := make([][]string, 0, len(checkIDs)+1)
			want = append(want, []string{"Check", "Coverage"})

			for i, id := range checkIDs {
				want = append(want, []string{coverageNames[id], tc.coverage[i]})
			}

			if diff := cmp.Diff(want, tableOf(t, body, "Coverage")); diff != "" {
				t.Errorf("the Coverage table mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ranThen returns the coverage of fifteen checks that all ran, with the check
// id's row reading value when id is not empty.
func ranThen(id, value string) []string {
	out := make([]string, len(checkIDs))

	for i, check := range checkIDs {
		out[i] = "run"

		if id != "" && check == id {
			out[i] = value
		}
	}

	return out
}

// TestCoverageVocabularyIsComplete pins the rendered Coverage table against the
// checks the ADR lists: exactly fifteen rows, one per check, in the ADR's
// order, each with a coverage value. A check the core renders without a name,
// or a name without a row, would leave the table short, out of order, or with
// a row the report does not have.
func TestCoverageVocabularyIsComplete(t *testing.T) {
	t.Parallel()

	if len(checkIDs) != 15 {
		t.Fatalf("the test's check ids number %d, want the ADR's fifteen", len(checkIDs))
	}

	body := renderReport(t, publish.ReportInput{
		Result:  fixtureResult(),
		Run:     testRun(t, testDate, testRunID),
		Private: publish.PrivateNone,
	}).Body

	rows := tableOf(t, body, "Coverage")

	if len(rows) != len(checkIDs)+1 {
		t.Fatalf("the Coverage table holds %d rows besides its header, want the ADR's %d checks, one row each:\n%v", len(rows)-1, len(checkIDs), rows)
	}

	want := make([][]string, 0, len(checkIDs))
	for _, id := range checkIDs {
		want = append(want, []string{coverageNames[id], "run"})
	}

	// Every check ran in the fixture, so each row's coverage reading is run.
	if diff := cmp.Diff(want, rows[1:]); diff != "" {
		t.Errorf("the Coverage table's checks mismatch (-want +got):\n%s", diff)
	}
}
