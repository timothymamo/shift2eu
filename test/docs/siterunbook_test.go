// Pins issue #535: on the maintainer's first R5 run the bucket answered its AccessDenied in the HTML form a bucket's
// website endpoint returns ("<li>Code: AccessDenied</li>"), and docs/site-runbook.md knew only the S3 API's XML form
// ("<Code>AccessDenied</Code>"), so it took a definite answer for "still provisioning" and would have retried for
// thirty minutes. The section is checked as text, from its heading up to the next top-level heading: the retry loop
// stops on either form, matched on the markup each form answers with, the lines that print an attempt and the body
// cut unprintable bytes (a redirect's or a binary body's would otherwise reach the terminal and the report) and never
// use head (with two files head prints "==> file <==" headers into the output), and each outcome item says which read
// rule a 200 means - public_read false, and the recorded fallback under public_read true.
//
// It pins issue #567's follow-up of PR #582's review on R4 as well: the wait loop that refreshes the state until
// edge_endpoint is set records a refresh-only that failed before it breaks, so the check line after it prints a FAIL
// naming that case beside the timeout FAIL, and infra/site/outputs.tf's container_endpoint tolerates the container
// being absent, since R4's refresh-only reads every output while the first apply's precondition can leave the
// container uncreated. The R4 loop is read as text too and pinned on the property rather than on one shell spelling:
// the branch after `apply -refresh-only` must assign a variable before it breaks, and the variable name is the
// maintainer's choice.
package docs_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// readRuleHeading is the R5 section of siteRunbookPath (declared in runbookscwjson_test.go) this file pins
// (PRD #297, issue #535).
const readRuleHeading = "## R5. The read rule and the origins"

// runbookCheck is one pin on the section's text: the items the check reads (a line, a comment block whose
// consecutive comment lines are joined, or an outcome item), the substrings at least one of them must carry, and the
// substrings none of them may carry.
type runbookCheck struct {
	name     string                  // the subtest's name
	reads    func([]string) []string // the lines, comment blocks or outcome items the check reads of the section
	want     []string                // substrings one read item must carry
	absent   []string                // substrings no read item may carry
	expected string                  // what the section must say, for the failure message
}

// TestSiteRunbookReadRuleStopsOnBothAccessDeniedForms pins issue #535 on docs/site-runbook.md's R5: the bucket's
// AccessDenied counts as the answer in both the S3 API's XML form and the HTML form its website endpoint returns, so
// the retry loop breaks on the first try that gets one, and each outcome item says which read rule a 200 means.
func TestSiteRunbookReadRuleStopsOnBothAccessDeniedForms(t *testing.T) {
	t.Parallel()

	section := runbookSectionOf(t, readRuleHeading)

	cases := []runbookCheck{
		{
			name:     "the loop's break line matches both AccessDenied forms",
			reads:    func(section []string) []string { return runbookLinesStartingWith(section, `[ "$C" = 403 ]`) },
			want:     []string{`-e '<Code>AccessDenied</Code>'`, `-e '<li>Code: AccessDenied</li>'`},
			expected: "the retry loop's break line must grep for both forms, `grep -q -e '<Code>AccessDenied</Code>' -e '<li>Code: AccessDenied</li>'`: the S3 API answers an XML body, the website endpoint the bucket's HTML page, whose markup carries the code in a list item (issue #535)",
		},
		{
			name:     "a comment names the website endpoint's HTML form",
			reads:    runbookCommentBlocks,
			want:     []string{"Code: AccessDenied", "website endpoint"},
			expected: "a comment of the section must name the second form, `Code: AccessDenied`, and say that it is the bucket's website endpoint that returns it (issue #535)",
		},
		{
			name:     "the try line prints only printable bytes, one line, without head's file headers",
			reads:    func(section []string) []string { return runbookLinesContaining(section, `echo "try $i:`) },
			want:     []string{`LC_ALL=C tr -c '[:print:]' ' '`, "cut -c1-200"},
			absent:   []string{"head "},
			expected: "the line that prints an attempt must print only printable bytes, truncated, and must not use head: with the error and the body as its two files, head prints `==> <file> <==` headers into the output, so `LC_ALL=C tr -c '[:print:]' ' ' | cut -c1-200` them (issue #535)",
		},
		{
			name:     "the body line prints only printable bytes",
			reads:    func(section []string) []string { return runbookLinesContaining(section, `echo "status $C"`) },
			want:     []string{`LC_ALL=C tr -c '[:print:]\n' ' '`},
			expected: "the line after the loop that prints status, headers and body must cut unprintable bytes with `LC_ALL=C tr -c '[:print:]\\n' ' '`, keeping a redirect's or a binary body's bytes out of the terminal and the report (issue #535)",
		},
		{
			name:     "the PASS 200 outcome item keeps public_read false",
			reads:    runbookOutcomeItems,
			want:     []string{"PASS 200 with public_read false", "keep public_read false"},
			expected: "the outcome item for a 200 with public_read false must say it means Edge Services reads the private objects, so keep public_read false (issue #535)",
		},
		{
			name:     "a PASS 200 outcome item names the 200 under public_read true as the recorded fallback",
			reads:    runbookOutcomeItems,
			want:     []string{"PASS 200 with public_read true", "fallback"},
			expected: "the outcome item for a 200 with public_read true must say it is the recorded fallback, so a maintainer who set public_read = true knows which read rule they are passing (issue #535)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			items := tc.reads(section)
			if len(items) == 0 {
				t.Fatalf("%s: %s holds nothing this check reads of R5, so it would pass on a section that lost it: %s", siteRunbookPath, readRuleHeading, tc.expected)
			}

			for _, item := range items {
				for _, substring := range tc.absent {
					if strings.Contains(item, substring) {
						t.Errorf("%s: %s: %s\n  read: %q", siteRunbookPath, readRuleHeading, tc.expected, item)
					}
				}
			}

			if len(tc.want) == 0 || slices.ContainsFunc(items, func(item string) bool { return carriesAll(item, tc.want) }) {
				return
			}

			t.Errorf("%s: %s: %s\n  read:\n    %s", siteRunbookPath, readRuleHeading, tc.expected, strings.Join(items, "\n    "))
		})
	}
}

// carriesAll reports whether item carries every one of the substrings.
func carriesAll(item string, substrings []string) bool {
	for _, substring := range substrings {
		if !strings.Contains(item, substring) {
			return false
		}
	}

	return true
}

// hostingHeading is R4 of siteRunbookPath (issue #567): the apply that creates the hosting and the wait loop after it,
// whose lines this file pins as text.
const hostingHeading = "## R4. Create the hosting"

// TestSiteRunbookHostingWaitLoopNamesARefreshOnlyFailure pins the follow-up of PR #582's review on R4 (issue #567): the
// wait loop refreshes the state until Scaleway has assigned the pipeline's default_fqdn, and a refresh-only that fails
// there - an absent scaleway_container.site after the expected precondition FAIL is one way - must not look like the
// timeout. The loop therefore records the failure before it breaks, and the check line after it prints a FAIL that
// names that case apart from "FAIL edge_endpoint still empty after 10 minutes". Both are pinned as properties of the
// section's text, so the loop's variable name and the FAIL's wording are the maintainer's choice.
func TestSiteRunbookHostingWaitLoopNamesARefreshOnlyFailure(t *testing.T) {
	t.Parallel()

	section := runbookSectionOf(t, hostingHeading)

	t.Run("the loop records a refresh-only failure before it breaks", func(t *testing.T) {
		t.Parallel()

		loops := runbookLinesContaining(section, "apply -refresh-only")
		if len(loops) == 0 {
			t.Fatalf("%s: %s runs no `apply -refresh-only`, so this check pins nothing: the wait loop refreshes the state until edge_endpoint is set, and the refresh is the line whose failure the loop must record (issue #567)",
				siteRunbookPath, hostingHeading)
		}

		for _, line := range loops {
			if problem := refreshOnlyFailureUnrecorded(line); problem != "" {
				t.Errorf("%s: %s: the wait loop's `apply -refresh-only` %s, want the loop to record the failure before it breaks, as `|| { refresh_failed=1; break; }` does, so the check line after it can print a FAIL that names that case rather than the timeout's (issue #567)\n  read: %q",
					siteRunbookPath, hostingHeading, problem, line)
			}
		}
	})

	t.Run("the check line names the refresh-only failure apart from the timeout", func(t *testing.T) {
		t.Parallel()

		// timeoutFAIL is the FAIL the check line prints when the loop ran out of tries: it must stay, and it must not
		// be the only FAIL the line can print.
		const timeoutFAIL = "FAIL edge_endpoint still empty after 10 minutes"

		var timeoutNamed, failureNamed bool

		for _, line := range runbookLinesContaining(section, "FAIL") {
			if strings.Contains(line, timeoutFAIL) {
				timeoutNamed = true
			}

			if !strings.Contains(strings.ToLower(line), "refresh-only") {
				continue
			}

			// The FAIL that names the refresh-only case must be a phrase of its own: a line that carries the timeout
			// FAIL beside the case's name still prints the misleading FAIL alone when only the loop records the error.
			if strings.Contains(strings.ReplaceAll(line, timeoutFAIL, ""), "FAIL") {
				failureNamed = true
			}
		}

		if !timeoutNamed {
			t.Errorf("%s: %s no longer prints the timeout FAIL %q, want it kept: a loop that ran its 20 tries without edge_endpoint set is still a FAIL of its own (issue #567)",
				siteRunbookPath, hostingHeading, timeoutFAIL)
		}

		if !failureNamed {
			t.Errorf("%s: %s prints no FAIL that names a refresh-only failure apart from %q, want one such FAIL, as `FAIL refresh-only failed: read the error above` is: the loop's refresh can fail for a reason of its own, and the check line must not report it as ten minutes of an empty edge_endpoint (issue #567)\n  read:\n    %s",
				siteRunbookPath, hostingHeading, timeoutFAIL, strings.Join(runbookLinesContaining(section, "FAIL"), "\n    "))
		}
	})
}

// refreshOnlyFailureUnrecorded says why the runbook line that runs `apply -refresh-only` hands the command's failure to
// no branch that records it, and "" when it does. The branch after the command's `||` must assign a variable before it
// breaks, so the loop remembers that it broke on an error and the check line after it can print a FAIL naming that
// case; a bare `break` leaves the check line to print the timeout FAIL, which is misleading.
func refreshOnlyFailureUnrecorded(line string) string {
	_, rest, ok := strings.Cut(line, "apply -refresh-only")
	if !ok {
		return "runs on no such line"
	}

	_, branch, ok := strings.Cut(rest, "||")
	if !ok {
		return "hands its failure to no `||` branch"
	}

	before, _, broke := strings.Cut(branch, "break")
	if !broke {
		return "hands its failure to a branch that does not break"
	}

	if !shellAssignment.MatchString(before) {
		return "breaks without recording the failure first"
	}

	return ""
}

// shellAssignment matches an assignment of a shell variable, the way R4's loop records the failure it broke on: the
// loop names the variable as it likes.
var shellAssignment = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*=`)

// TestContainerEndpointOutputToleratesAnAbsentContainer pins the other half of the same follow-up (issue #567): R4's
// refresh-only reads every output of infra/site, and the first apply can leave scaleway_container.site uncreated (its
// precondition fails while the pipeline's default_fqdn is still empty), so the container_endpoint output must read the
// container's endpoint when it exists and the empty string when it does not, rather than failing the whole refresh.
func TestContainerEndpointOutputToleratesAnAbsentContainer(t *testing.T) {
	t.Parallel()

	const (
		outputsPath = "infra/site/outputs.tf"
		want        = `value = try(scaleway_container.site.public_endpoint, "")`
	)

	var values []string

	for _, line := range strings.Split(outputBlock(t, readDoc(t, os.DirFS(repoRoot), outputsPath), "container_endpoint"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "value") {
			values = append(values, strings.Join(strings.Fields(line), " "))
		}
	}

	if len(values) != 1 {
		t.Fatalf("%s: the container_endpoint output states %d value lines, want exactly one", outputsPath, len(values))
	}

	if values[0] != want {
		t.Errorf("%s: the container_endpoint output's value is %q, want %q: R4's refresh-only reads every output, so a container the first apply could not create yet would fail the refresh there instead of leaving R4 to wait for edge_endpoint (issue #567)",
			outputsPath, values[0], want)
	}
}

// outputBlock returns the text of the `output "<name>" {` block of an OpenTofu file, from its opening line to the line
// that closes it.
func outputBlock(t *testing.T, src, name string) string {
	t.Helper()

	lines := strings.Split(src, "\n")

	start := slices.IndexFunc(lines, func(line string) bool {
		return strings.TrimSpace(line) == `output "`+name+`" {`
	})
	if start < 0 {
		t.Fatalf("the OpenTofu file holds no output %q", name)
	}

	end := slices.IndexFunc(lines[start+1:], func(line string) bool {
		return strings.TrimSpace(line) == "}"
	})
	if end < 0 {
		t.Fatalf("the output %q never closes", name)
	}

	return strings.Join(lines[start:start+end+2], "\n")
}

// runbookSectionOf returns the lines under the heading of the runbook, from the line after the heading to the line of
// the next heading of the same or a higher level, or the end of the page. A heading inside a fenced code block is no
// heading, so a comment of a shell block that starts with "# " does not end the section.
func runbookSectionOf(t *testing.T, heading string) []string {
	t.Helper()

	lines := strings.Split(readDoc(t, os.DirFS(repoRoot), siteRunbookPath), "\n")

	start, end, fenced := -1, len(lines), false

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced

			continue
		}

		if fenced {
			continue
		}

		if start < 0 {
			if strings.TrimSpace(line) == heading {
				start = i
			}

			continue
		}

		if level, _, ok := markdownHeading(line); ok && level <= 2 {
			end = i

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s: the heading %q is gone, so this test pins nothing: it checks the section a maintainer follows to check the site's read rule (issue #535)", siteRunbookPath, heading)
	}

	return lines[start+1 : end]
}

// runbookLinesStartingWith returns the section's lines that start with prefix after their leading whitespace, trimmed.
func runbookLinesStartingWith(section []string, prefix string) []string {
	var found []string

	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			found = append(found, trimmed)
		}
	}

	return found
}

// runbookLinesContaining returns the section's lines that contain substring, trimmed.
func runbookLinesContaining(section []string, substring string) []string {
	var found []string

	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, substring) {
			found = append(found, trimmed)
		}
	}

	return found
}

// runbookCommentBlocks returns the section's comments, each block of consecutive comment lines joined by a space: a
// runbook comment often wraps over several lines, so a rule it states may straddle them. A trailing comment on a
// shell line is no comment block of its own.
func runbookCommentBlocks(section []string) []string {
	var (
		blocks []string
		block  []string
	)

	flush := func() {
		if len(block) > 0 {
			blocks = append(blocks, strings.Join(block, " "))
			block = nil
		}
	}

	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			block = append(block, trimmed)

			continue
		}

		flush()
	}

	flush()

	return blocks
}

// runbookOutcomeItems returns the section's outcome items: each comment line that opens an outcome (# PASS, # FAIL or
// # 403) joined by a space with the indented comment lines that continue it. A runbook's outcome block states one
// outcome per item, so a rule an item states is read there, not in the comments around it.
func runbookOutcomeItems(section []string) []string {
	var (
		items []string
		item  []string
	)

	flush := func() {
		if len(item) > 0 {
			items = append(items, strings.Join(item, " "))
			item = nil
		}
	}

	for _, line := range section {
		trimmed := strings.TrimSpace(line)

		switch {
		case runbookOpensOutcome(trimmed):
			flush()

			item = append(item, trimmed)
		case len(item) > 0 && runbookContinuesOutcome(line):
			item = append(item, trimmed)
		default:
			flush()
		}
	}

	flush()

	return items
}

// runbookOpensOutcome reports whether the trimmed comment line starts an outcome item.
func runbookOpensOutcome(trimmed string) bool {
	for _, prefix := range []string{"# PASS", "# FAIL", "# 403"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}

	return false
}

// runbookContinuesOutcome reports whether line continues the outcome item above it: a comment line whose marker is
// followed by more than one space (`#   ...`).
func runbookContinuesOutcome(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}

	rest := strings.TrimPrefix(trimmed, "#")

	return strings.HasPrefix(rest, "  ") && strings.TrimSpace(rest) != ""
}
