package publish_test

// The report the core renders, pinned as one golden file (PRD #175 milestone 2,
// "Report and triage"): the page an audit of this repository is, which
// test/docs reads and checks as a page under docs/security/audits/. The other
// rendering tests read the report section by section; this one pins its whole
// text, so an escaped fragment, a table separator or a missing newline that no
// section test names still fails.

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// update rewrites the golden file with the rendered report instead of
// comparing, the -update flag the repository's golden tests share: a golden file
// is written and reviewed by hand, and -update is the reviewer's convenience,
// never something CI runs.
var update = flag.Bool("update", false, "rewrite testdata/report.golden.md with the rendered report instead of comparing")

// goldenReportPath is the golden report, relative to this package's directory:
// the one test/docs reads from the repository's root.
const goldenReportPath = "testdata/report.golden.md"

// goldenReportInput returns the input of the report the golden file holds: a
// run of the fixture repository on 2026-12-03, its ID 1234567890, and a public
// finding of each status the findings table renders - a kept one, an
// informational one and a suspected one under needs investigation.
func goldenReportInput(t *testing.T) publish.ReportInput {
	t.Helper()

	result := fixtureResult(
		finding("G304", sevLow, true, catHardening),
		finding("G307", sevInformational, true, catSupplyChain),
		finding("G308", sevLow, false, catDenialOfSvc),
	)

	return publish.ReportInput{
		Result:  result,
		Run:     testRun(t, "2026-12-03", "1234567890"),
		Private: publish.PrivateNone,
	}
}

// TestRenderReportGolden pins the report's whole text: the fixture run's report
// is exactly testdata/report.golden.md, the page test/docs checks. A difference
// names the first line that changed; -update accepts it.
func TestRenderReportGolden(t *testing.T) {
	t.Parallel()

	rendered := renderReport(t, goldenReportInput(t))

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenReportPath), 0o700); err != nil {
			t.Fatalf("make the golden file's directory: %v", err)
		}

		if err := os.WriteFile(goldenReportPath, []byte(rendered.Body), 0o600); err != nil {
			t.Fatalf("write %s: %v", goldenReportPath, err)
		}

		return
	}

	want, err := os.ReadFile(goldenReportPath) //nolint:gosec // a test golden file, not user input
	if err != nil {
		t.Fatalf("read %s: %v", goldenReportPath, err)
	}

	if diff := cmp.Diff(string(want), rendered.Body); diff != "" {
		t.Errorf("the rendered report does not match %s (-golden +rendered):\n%s\nrun the test with -update to accept it",
			goldenReportPath, diff)
	}
}
