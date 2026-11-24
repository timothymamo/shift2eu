// Pins issue #535: on the maintainer's first R5 run the bucket answered its AccessDenied in the HTML form a bucket's
// website endpoint returns ("Code: AccessDenied"), and docs/site-runbook.md knew only the S3 API's XML form
// ("<Code>AccessDenied</Code>"), so it took a definite answer for "still provisioning" and would have retried for
// thirty minutes. The section is checked as text, from its heading up to the next top-level heading: the retry loop
// stops on either form, a comment names both, the line that prints an attempt does not use head (with two files head
// prints "==> file <==" headers into the output), and the PASS comments say which read rule a 200 means - public_read
// false, and the recorded fallback under public_read true.
package docs_test

import (
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	// siteRunbookPath is the runbook the site's hosting is created and checked with, and readRuleHeading the R5
	// section this file pins (PRD #297, issue #535).
	siteRunbookPath = "docs/site-runbook.md"
	readRuleHeading = "## R5. The read rule and the origins"
)

// runbookCheck is one pin on the section's text: the items the check reads (a line, or a comment block whose
// consecutive comment lines are joined), the substrings at least one of them must carry, and the substrings none of
// them may carry.
type runbookCheck struct {
	name     string                  // the subtest's name
	reads    func([]string) []string // the lines or comment blocks the check reads of the section
	want     []string                // substrings one read item must carry
	absent   []string                // substrings no read item may carry
	expected string                  // what the section must say, for the failure message
}

// TestSiteRunbookReadRuleStopsOnBothAccessDeniedForms pins issue #535 on docs/site-runbook.md's R5: the bucket's
// AccessDenied counts as the answer in both the S3 API's XML form and the HTML form its website endpoint returns, so
// the retry loop breaks on the first try that gets one, and the outcome comments say which read rule a 200 means.
func TestSiteRunbookReadRuleStopsOnBothAccessDeniedForms(t *testing.T) {
	t.Parallel()

	section := runbookSectionOf(t, readRuleHeading)

	cases := []runbookCheck{
		{
			name:     "the loop's break line matches both AccessDenied forms",
			reads:    func(section []string) []string { return runbookLinesStartingWith(section, `[ "$C" = 403 ]`) },
			want:     []string{"-e '<Code>AccessDenied</Code>'", "-e 'Code: AccessDenied'"},
			expected: "the retry loop's break line must grep for both forms, `grep -q -e '<Code>AccessDenied</Code>' -e 'Code: AccessDenied'`: the S3 API answers an XML body, the website endpoint the bucket's HTML page (issue #535)",
		},
		{
			name:     "a comment names the website endpoint's HTML form",
			reads:    runbookCommentBlocks,
			want:     []string{"Code: AccessDenied", "website endpoint"},
			expected: "a comment of the section must name the second form, `Code: AccessDenied`, and say that it is the bucket's website endpoint that returns it (issue #535)",
		},
		{
			name:     "the try line prints the error and the body without head's file headers",
			reads:    func(section []string) []string { return runbookLinesContaining(section, `echo "try $i:`) },
			absent:   []string{"head "},
			expected: "the line that prints an attempt must not use head: with the error and the body as its two files, head prints `==> <file> <==` headers into the output, so print them another way (issue #535)",
		},
		{
			name:     "the PASS 200 comment keeps public_read false",
			reads:    func(section []string) []string { return runbookCommentBlocksContaining(section, "PASS 200") },
			want:     []string{"public_read false"},
			expected: "the comment for a 200 must say it means Edge Services reads the private objects, so keep public_read false (issue #535)",
		},
		{
			name:     "a comment names the 200 under public_read true as the recorded fallback",
			reads:    func(section []string) []string { return runbookCommentBlocksContaining(section, "public_read true") },
			want:     []string{"fallback"},
			expected: "a comment must say that a 200 under public_read true is the recorded fallback, so a maintainer who set public_read = true knows which read rule they are passing (issue #535)",
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

// runbookCommentBlocksContaining returns the section's comment blocks that contain substring.
func runbookCommentBlocksContaining(section []string, substring string) []string {
	return slices.DeleteFunc(runbookCommentBlocks(section), func(block string) bool {
		return !strings.Contains(block, substring)
	})
}
