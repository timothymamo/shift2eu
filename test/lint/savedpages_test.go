package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The saved-page rule and its allow list live in savedpages_rule_test.go
// (issue #212): a saved price page, a browser copy of a target's pricing page
// kept because the site refuses automated clients, belongs in a scratch
// directory outside the repository and is never committed. Committing one
// would put a large third-party page, tracking scripts and session data
// included, next to a catalog snapshot. The convention is stated in
// internal/adapter/target/upcloud/SOURCES.md and in the add-cloud-provider
// skill.

// TestSavedPageProblems pins the checker the rule runs, on a probe tree held as
// a list of slash-separated paths relative to the module root. A path the allow
// list does not name is refused when its extension is that of a saved page
// (.html, .htm, .xhtml, .xht, .shtml, .mhtml, .mht, .webarchive, in any case) or
// when a directory segment ends in _files, in any case, the asset directory a
// browser writes beside a "Webpage, Complete" copy. Each refused path gets
// exactly one problem, in input order, naming the path and pointing to the
// SOURCES.md convention.
func TestSavedPageProblems(t *testing.T) {
	t.Parallel()

	allowedPricingPage := map[string]bool{
		"internal/adapter/target/upcloud/pricing.html": true,
	}

	tests := []struct {
		name    string
		paths   []string
		allowed map[string]bool
		want    []string // the path each problem must name, in order
	}{
		{
			name:  "no path in the tree is an html page",
			paths: []string{"README.md", "internal/adapter/target/upcloud/catalog.json", "main.go"},
			want:  nil,
		},
		{
			name:  "empty tree",
			paths: nil,
			want:  nil,
		},
		{
			name:  "saved price page next to the catalog snapshot",
			paths: []string{"internal/adapter/target/upcloud/pricing.html"},
			want:  []string{"internal/adapter/target/upcloud/pricing.html"},
		},
		{
			name:  "htm extension in capitals",
			paths: []string{"docs/x.HTM"},
			want:  []string{"docs/x.HTM"},
		},
		{
			name:  "mixed-case html extension",
			paths: []string{"a/b.Html"},
			want:  []string{"a/b.Html"},
		},
		{
			name:  "html in the middle of a name",
			paths: []string{"a.html.txt"},
			want:  nil,
		},
		{
			name:  "go file whose name starts with html",
			paths: []string{"htmlfile.go"},
			want:  nil,
		},
		{
			name:  "go file in a directory called html",
			paths: []string{"a/html/b.go"},
			want:  nil,
		},
		{
			name:  "xhtml extension",
			paths: []string{"x.xhtml"},
			want:  []string{"x.xhtml"},
		},
		{
			name:  "xht extension",
			paths: []string{"x.xht"},
			want:  []string{"x.xht"},
		},
		{
			name:  "shtml extension",
			paths: []string{"x.shtml"},
			want:  []string{"x.shtml"},
		},
		{
			name:  "mhtml extension",
			paths: []string{"x.mhtml"},
			want:  []string{"x.mhtml"},
		},
		{
			name:  "mht extension in capitals",
			paths: []string{"x.MHT"},
			want:  []string{"x.MHT"},
		},
		{
			name:  "webarchive extension",
			paths: []string{"x.webarchive"},
			want:  []string{"x.webarchive"},
		},
		{
			name:  "asset directory of a complete web page copy",
			paths: []string{"internal/adapter/target/upcloud/pricing_files/analytics.js"},
			want:  []string{"internal/adapter/target/upcloud/pricing_files/analytics.js"},
		},
		{
			name:  "asset directory suffix in capitals",
			paths: []string{"docs/Pricing_FILES/a.css"},
			want:  []string{"docs/Pricing_FILES/a.css"},
		},
		{
			name:  "file whose name ends in _files, not a directory",
			paths: []string{"docs/x_files"},
			want:  nil,
		},
		{
			name:  "plain files directory",
			paths: []string{"docs/files/a.css"},
			want:  nil,
		},
		{
			name:  "directory whose name merely contains _files",
			paths: []string{"docs/my_filesystem/a.go"},
			want:  nil,
		},
		{
			name:  "page inside an asset directory, refused once",
			paths: []string{"a_files/b.html"},
			want:  []string{"a_files/b.html"},
		},
		{
			name:    "allowed page beside a refused one",
			paths:   []string{"internal/adapter/target/upcloud/pricing.html", "docs/saved.html"},
			allowed: allowedPricingPage,
			want:    []string{"docs/saved.html"},
		},
		{
			name:    "only an allowed page",
			paths:   []string{"internal/adapter/target/upcloud/pricing.html"},
			allowed: allowedPricingPage,
			want:    nil,
		},
		{
			name:  "allowed path inside an asset directory is not refused",
			paths: []string{"internal/adapter/target/upcloud/pricing_files/pricing.xhtml"},
			allowed: map[string]bool{
				"internal/adapter/target/upcloud/pricing_files/pricing.xhtml": true,
			},
			want: nil,
		},
		{
			name: "several refused pages, each named in input order",
			paths: []string{
				"internal/adapter/target/ovhcloud/pricing.html",
				"internal/adapter/target/upcloud/pricing.htm",
				"docs/saved/scaleWAY.HTML",
			},
			want: []string{
				"internal/adapter/target/ovhcloud/pricing.html",
				"internal/adapter/target/upcloud/pricing.htm",
				"docs/saved/scaleWAY.HTML",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := savedPageProblems(tc.paths, tc.allowed)
			if len(got) != len(tc.want) {
				t.Fatalf("savedPageProblems(%q, %v) returned %d problems, want %d: %q",
					tc.paths, tc.allowed, len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("problem %d = %q, want it to name %s", i, got[i], want)
				}
				if !strings.Contains(got[i], "SOURCES.md") {
					t.Errorf("problem %d = %q, want it to point to the SOURCES.md convention", i, got[i])
				}
			}
		})
	}
}

// TestNoTrackedSavedPages is the rule: git tracks no saved price page. A saved
// page is read from outside the repository and never committed, so this test
// passes on the module as it is, with an empty allow list, and the branch that
// commits one fails here.
func TestNoTrackedSavedPages(t *testing.T) {
	t.Parallel()

	for _, problem := range savedPageProblems(trackedFiles(t), allowedSavedPages) {
		t.Error(problem)
	}
}

// catalogScratchDir is the directory of a worktree in which catalog units keep
// the vendor pages they fetch (add-cloud-provider, the targets' SOURCES.md).
const catalogScratchDir = "/scratch/"

// TestGitignoreIgnoresTheCatalogScratchDirectory pins the first guard in front
// of TestNoTrackedSavedPages: .gitignore ignores the worktree's scratch/
// directory, so a `git add -A` cannot stage a saved vendor page there and the
// rule above never has to catch one after it was committed (issue #688).
func TestGitignoreIgnoresTheCatalogScratchDirectory(t *testing.T) {
	t.Parallel()

	gitignore, err := os.ReadFile(filepath.Join(moduleRoot(t), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range strings.Split(string(gitignore), "\n") {
		if strings.TrimSpace(line) == catalogScratchDir {
			return
		}
	}

	t.Errorf(".gitignore: no %q line, so the saved vendor pages catalog units keep in a worktree's scratch/ can be staged by git add -A (issue #688)", catalogScratchDir)
}
