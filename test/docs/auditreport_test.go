package docs_test

// The audit report the publish core renders (PRD #175 milestone 2, "Report and
// triage"): test/docs checks a page under docs/security/audits/ for its one #
// title and for the links it holds, and the report ci/auditpublish publishes is
// such a page. Its golden file is read from where it lives in the tree and
// checked as the page it becomes; ci/... is never imported (Go's internal rule
// and depguard forbid it), so the file is read.

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/timothymamo/shift2eu/test/internal/mdlink"
)

// goldenReportPath is the publish core's golden report, slash-separated and
// relative to the repository's root.
const goldenReportPath = "ci/auditpublish/internal/publish/testdata/report.golden.md"

// goldenReportPage is where the golden's report lives in the documentation: the
// path the renderer gives it, docs/security/audits/<date>-<run id>.md. Its
// relative links resolve from there.
const goldenReportPage = "docs/security/audits/2026-12-03-1234567890.md"

// TestGoldenAuditReportIsAPage pins the report the publish core renders as the
// page it is published as: the one # title a page starts with, and relative
// links that resolve from docs/security/audits/, where test/docs checks the
// repository's own reports. The pages the report links to are the only others
// the check reads.
func TestGoldenAuditReportIsAPage(t *testing.T) {
	t.Parallel()

	body, err := fs.ReadFile(os.DirFS(repoRoot), goldenReportPath)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenReportPath, err)
	}

	// The title check TestPagesHaveOneTitle applies to every page under docs/.
	if !strings.HasPrefix(string(body), "# ") {
		t.Errorf("%s: the first line is not the page's # title", goldenReportPath)
	}

	for _, h := range mdlink.Headings(body) {
		if h.Level == 1 && h.Line != 1 {
			t.Errorf("%s:%d: a second # heading, %q; a page has one title", goldenReportPath, h.Line, h.Text)
		}
	}

	// The link check TestRelativeLinksResolve applies to the pages under docs/,
	// with the report at the path it is published at and the page it links for
	// the audit's description beside it.
	pages := fstest.MapFS{
		goldenReportPage:          {Data: body},
		"docs/security/README.md": {Data: []byte("# Security\n")},
	}

	problems, err := mdlink.Check(pages, []string{goldenReportPage})
	if err != nil {
		t.Fatalf("checking the links of %s: %v", goldenReportPath, err)
	}

	for _, p := range problems {
		t.Errorf("%s (as %s):%d: link %q: %s", goldenReportPath, p.File, p.Line, p.Link, p.Reason)
	}
}
