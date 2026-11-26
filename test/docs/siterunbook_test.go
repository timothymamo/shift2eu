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
//
// It pins issue #607's two points on R4 as well: the section runs `apply -refresh-only` on two lines for two reasons,
// and each command is pinned on its own line - the state check's route with `-var=pipeline_www=true`, the wait loop's
// refresh with -auto-approve - so a loose match on the command cannot read one for the other; and the exit 2 FAIL that
// keeps the state backup until its cause is explained also says to remove it, so the copy R4 takes before its state rm
// fallback does not outlive the explanation.
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
// names that case apart from "FAIL edge_endpoint still empty after 10 minutes", before it prints the PASS an empty
// edge_endpoint can no longer hide. All of it is pinned as properties of the section's text, so the loop's variable
// name and the FAIL's wording are the maintainer's choice: the check must be a command line (a comment that states the
// outcome in the runbook's prose prints nothing and proves nothing), and it must test the variable the loop assigns.
//
// It pins R4's container_endpoint check as well (the same review, the other half of the follow-up): container_endpoint
// reads "" while the container is absent, so R4 checks the variable it read and says which pass sets it.
func TestSiteRunbookHostingWaitLoopNamesARefreshOnlyFailure(t *testing.T) {
	t.Parallel()

	section := runbookSectionOf(t, hostingHeading)

	loops := runbookLinesContaining(section, "apply -refresh-only")
	if len(loops) == 0 {
		t.Fatalf("%s: %s runs no `apply -refresh-only`, so this check pins nothing: the wait loop refreshes the state until edge_endpoint is set, and the refresh is the line whose failure the loop must record (issue #567)",
			siteRunbookPath, hostingHeading)
	}

	// recorded is the variable the loop's refresh line sets before it breaks. The loop names it as it likes, and the
	// check line after the loop has to test that name, so both are read from the loop rather than spelled here.
	recorded := refreshOnlyRecordedVariables(loops[0])

	t.Run("the loop records a refresh-only failure before it breaks", func(t *testing.T) {
		t.Parallel()

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

		var timeoutNamed bool

		for _, line := range runbookLinesContaining(section, "FAIL") {
			if !isRunbookComment(line) && strings.Contains(line, timeoutFAIL) {
				timeoutNamed = true
			}
		}

		if !timeoutNamed {
			t.Errorf("%s: %s no longer prints the timeout FAIL %q, want it kept: a loop that ran its 20 tries without edge_endpoint set is still a FAIL of its own (issue #567)",
				siteRunbookPath, hostingHeading, timeoutFAIL)
		}

		printed := refreshOnlyFAILLines(section, timeoutFAIL)
		if len(printed) == 0 {
			t.Fatalf("%s: %s prints no FAIL that names a refresh-only failure apart from %q, want one such FAIL in an `echo \"...\"` on a command line, as `echo \"FAIL refresh-only failed: read the error above\"` is: the loop's refresh can fail for a reason of its own, and the check line must not report it as ten minutes of an empty edge_endpoint. A `# FAIL ...` comment states the outcome; it prints nothing, so it cannot stand in for the command (issue #567)\n  read:\n    %s",
				siteRunbookPath, hostingHeading, timeoutFAIL, strings.Join(runbookLinesContaining(section, "FAIL"), "\n    "))
		}

		if len(recorded) == 0 {
			t.Fatalf("%s: %s: the wait loop's `apply -refresh-only` sets no variable before it breaks, so no check line can test the failure it recorded:\n    %s",
				siteRunbookPath, hostingHeading, strings.Join(loops, "\n    "))
		}

		for _, line := range printed {
			if !testsRecordedVariable(line, recorded) {
				t.Errorf("%s: %s: the line that prints a FAIL naming the refresh-only failure tests none of the variables the wait loop sets before it breaks (%s), want it to test one, as `elif [ \"$refresh_failed\" -eq 1 ]` does: the FAIL must be printed for the failure the loop recorded, not for every line that carries the word, or an edge_endpoint that stayed empty has no FAIL of its own (issue #567)\n  read: %q",
					siteRunbookPath, hostingHeading, strings.Join(recorded, ", "), line)
			}
		}
	})

	t.Run("the check line tests the refresh failure before it prints the PASS", func(t *testing.T) {
		t.Parallel()

		const timeoutFAIL = "FAIL edge_endpoint still empty after 10 minutes"

		if len(recorded) == 0 {
			t.Fatalf("%s: %s: the wait loop's `apply -refresh-only` sets no variable before it breaks, so this check pins nothing: the check line after it must test the failure the loop recorded before it prints the PASS (issue #567)",
				siteRunbookPath, hostingHeading)
		}

		var checked int

		for _, line := range refreshOnlyFAILLines(section, timeoutFAIL) {
			checked++

			refreshFailedAt := testsRecordedVariableAt(line, recorded)

			passAt, printedPass := firstEchoMatching(line, func(printed string) bool {
				return strings.Contains(printed, "PASS") && strings.Contains(printed, "edge_endpoint")
			})
			if !printedPass {
				continue // the subtest above pins the check line's PASS; here only the order of the two matters.
			}

			if refreshFailedAt < 0 || refreshFailedAt > passAt {
				t.Errorf("%s: %s: the check line prints its PASS about edge_endpoint at offset %d and tests none of the variables the wait loop sets before it breaks (%s) before that, want the refresh-only test first: a refresh-only that failed leaves edge_endpoint empty too, so a check that reads edge_endpoint first prints the PASS for the one case the loop broke on, and the FAIL naming it never shows (issue #567)\n  read: %q",
					siteRunbookPath, hostingHeading, passAt, strings.Join(recorded, ", "), line)
			}
		}

		if checked == 0 {
			t.Errorf("%s: %s: no command line prints a FAIL naming the refresh-only failure, so this check pins nothing: the check line must test the failure the loop recorded before it prints the PASS that an empty edge_endpoint no longer hides (issue #567)",
				siteRunbookPath, hostingHeading)
		}
	})

	t.Run("R4 checks the container endpoint it read, naming the second pass", func(t *testing.T) {
		t.Parallel()

		read := slices.IndexFunc(section, func(line string) bool {
			return !isRunbookComment(line) && strings.Contains(line, "output -raw container_endpoint")
		})
		if read < 0 {
			t.Fatalf("%s: %s reads no `output -raw container_endpoint`, so this check pins nothing: container_endpoint reads \"\" while the container is absent, and R4 must check the value it read (issue #567)",
				siteRunbookPath, hostingHeading)
		}

		check := slices.IndexFunc(section, func(line string) bool {
			if isRunbookComment(line) || !strings.Contains(line, `$CE`) {
				return false
			}

			_, printed := firstEchoMatching(line, func(printed string) bool {
				return strings.Contains(printed, "FAIL") && strings.Contains(strings.ToLower(printed), "container_endpoint")
			})

			return printed && strings.Contains(strings.ToLower(line), "second pass")
		})

		if check < 0 {
			t.Errorf("%s: %s holds no command line that checks container_endpoint and prints a FAIL naming the second pass, want one after the lines that read it, as `[ -n \"$CE\" ] && echo \"PASS container_endpoint is set\" || echo \"FAIL container_endpoint is empty: the container does not exist yet; run R4's second pass\"` is: container_endpoint is try(..., \"\") now, so an absent container leaves an empty value and no error, and a reader who takes the empty string for a printed endpoint stops R4 silently (issue #567)\n  read:\n    %s",
				siteRunbookPath, hostingHeading, strings.Join(section, "\n    "))
			return
		}

		if check < read {
			t.Errorf("%s: %s checks container_endpoint on line %d of the section, before the line %d that reads it, want the check after the value it checks (issue #567)",
				siteRunbookPath, hostingHeading, check+1, read+1)
		}
	})

	// R4 runs `apply -refresh-only` on two lines, for two different reasons (issue #607): the state check's FAIL route
	// re-reads the state with pipeline_www = true for that run alone, so the TLS stage's create_before_destroy reaches
	// the state and the plan after it removes the stage again (issue #595), while the wait loop's refresh repeats with
	// -auto-approve until Scaleway has assigned edge_endpoint. Both commands are pinned, so a reader of the match above
	// knows which line it read and a change that swaps the flags shows as a failure rather than as another line.
	const (
		stateCheckRefresh = "dev/tofu.sh -chdir=infra/site apply -var=pipeline_www=true -refresh-only"
		waitLoopRefresh   = "dev/tofu.sh -chdir=infra/site apply -refresh-only -auto-approve"
	)

	t.Run("R4 gives each refresh-only command on a line of its own", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name       string // the subtest's name
			command    string // the command R4 gives on one line
			followedBy string // what must follow the command there: an outcome comment after the state check's route, the failure branch after the loop's refresh
			expected   string // the command's reason, for the failure message
		}{
			{
				name:       "the state check's refresh-only route",
				command:    stateCheckRefresh,
				followedBy: "#",
				expected:   "R4's state check routes its FAIL through one command, `" + stateCheckRefresh + "`, and then only the outcome comment: it re-reads the state with pipeline_www = true for that run alone, and -auto-approve belongs to the wait loop's refresh, not to this route (issues #595, #607)",
			},
			{
				name:       "the wait loop's refresh",
				command:    waitLoopRefresh,
				followedBy: "||",
				expected:   "the wait loop's one refresh is `" + waitLoopRefresh + "` followed by its `||` branch, which records the failure before it breaks: the loop repeats the refresh until Scaleway has assigned edge_endpoint, and no other line of R4 may read as that loop (issues #567, #607)",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				given := runbookLinesContaining(section, tc.command)
				if len(given) != 1 {
					t.Errorf("%s: %s holds %d lines that run %q, want exactly one: %s\n  read:\n    %s",
						siteRunbookPath, hostingHeading, len(given), tc.command, tc.expected, strings.Join(given, "\n    "))

					return
				}

				if _, rest, _ := strings.Cut(given[0], tc.command); !strings.HasPrefix(strings.TrimSpace(rest), tc.followedBy) {
					t.Errorf("%s: %s: the line that runs %q goes on with %q, want it to go on with %q: %s\n  read: %q",
						siteRunbookPath, hostingHeading, tc.command, strings.TrimSpace(rest), tc.followedBy, tc.expected, given[0])
				}
			})
		}

		if len(loops) != 1 || !strings.Contains(loops[0], waitLoopRefresh) {
			t.Errorf("%s: %s: the `apply -refresh-only` match this test reads the loop with reads %q, want the wait loop's one line, the one that runs %q: the state check's route is no wait loop, and reading it there would take pipeline_www = true for the loop's refresh (issue #607)\n  read:\n    %s",
				siteRunbookPath, hostingHeading, loops, waitLoopRefresh, strings.Join(loops, "\n    "))
		}
	})

	t.Run("the exit 2 FAIL says to remove the state backup once the cause is explained", func(t *testing.T) {
		t.Parallel()

		// R4 backs the state up before its state rm fallback (`T="$(mktemp -d)"` and `state pull > "$T/state-backup.json"`)
		// and keeps the file until the fallback's last line. That line's PASS removes the temporary directory, but its
		// FAIL path only says to keep the backup until the diff is explained, so a maintainer who explains the cause is
		// left holding the only copy of the state the state rm took the TLS stage out of, with no line that removes it
		// (issue #607).
		const backup = "state-backup.json"

		// naming collects the outcomes that name the backup; removing, those of them that go on to say to remove it
		// once the cause is explained.
		var naming, removing []string

		for _, line := range section {
			for _, outcome := range failOutcomes(strings.TrimSpace(line)) {
				if !strings.Contains(outcome, backup) {
					continue
				}

				naming = append(naming, outcome)

				if removesTheBackupOnceExplained(outcome) {
					removing = append(removing, outcome)
				}
			}
		}

		if len(naming) == 0 {
			t.Fatalf("%s: %s holds no FAIL outcome that names the state backup %q, so this check pins nothing: R4 takes the backup before the state rm fallback and its FAIL path keeps it until the cause is explained (issue #607)\n  read:\n    %s",
				siteRunbookPath, hostingHeading, backup, strings.Join(section, "\n    "))
		}

		if len(removing) == 0 {
			t.Errorf("%s: %s: no FAIL outcome that names the state backup %q says to remove it once the cause is explained, want one that does, as `# FAIL exit 2: read the diff, and keep \"$T/state-backup.json\" until it is explained, then [ -f \"$T/state-backup.json\" ] && rm -r \"$T\"` does: the backup is the only copy of the state the state rm fallback left, so a maintainer who has explained the diff has no line to remove it and the file outlives its reason (issue #607)\n  read:\n    %s",
				siteRunbookPath, hostingHeading, backup, strings.Join(naming, "\n    "))
		}
	})
}

// failOutcomes returns the FAIL outcomes a runbook line states: for each "FAIL" of the line, the text from there to the
// next PASS or FAIL marker of the line, or to its end. R4 writes an outcome as a trailing comment on the command line it
// belongs to, so a rule an outcome states is read from that outcome's own words and not from the PASS beside it: the
// line that removes the temporary directory is the PASS of an exit 2 plan, not a step of its FAIL path.
func failOutcomes(line string) []string {
	const marker = "FAIL"

	var outcomes []string

	for rest := line; ; {
		at := strings.Index(rest, marker)
		if at < 0 {
			return outcomes
		}

		rest = rest[at:]

		end := len(rest)
		for _, other := range []string{"PASS", marker} {
			if i := strings.Index(rest[len(marker):], other); i >= 0 && len(marker)+i < end {
				end = len(marker) + i
			}
		}

		outcomes = append(outcomes, strings.TrimSpace(rest[:end]))

		rest = rest[end:]
	}
}

// removesTheBackupOnceExplained reports whether a FAIL outcome that names the state backup tells the maintainer to remove
// it once the cause is explained, as `keep "$T/state-backup.json" until it is explained, then
// [ -f "$T/state-backup.json" ] && rm -r "$T"` does. The
// removal has to come after the explanation: an outcome that explains the diff and then says "do not remove it" names
// both and still leaves the backup where it is, which is the state the check reports missing (issue #607).
func removesTheBackupOnceExplained(outcome string) bool {
	_, after, explained := strings.Cut(outcome, "explain")

	return explained && saysToRemoveAFile(after)
}

// removalInstruction matches a word that tells the maintainer to remove a file: the command rm, or the imperative
// remove or delete. Only the bare word counts, so a past tense or a third person - "the backup was removed earlier",
// "the earlier run deleted the backup", "it removes" - describes a removal and tells nobody to make one (issue #626).
var removalInstruction = regexp.MustCompile(`\b(rm|remove|delete)\b`)

// saysToRemoveAFile reports whether the runbook text tells the maintainer to remove a file, as R4's state backup's FAIL
// path must once the cause is explained: the spelling is the maintainer's choice, so rm, remove and delete all count,
// as words of their own (removalInstruction). A clause that negates its verb - "do not remove it", "remove nothing" -
// tells the maintainer to keep the file, so it is no removal, and each comma-separated clause is read on its own: a
// negation in one says nothing about another's.
func saysToRemoveAFile(text string) bool {
	for _, clause := range strings.Split(text, ",") {
		verb, negated := removalInstruction.MatchString(clause), false

		for _, negation := range []string{"not ", "nothing", "never"} {
			negated = negated || strings.Contains(clause, negation)
		}

		if verb && !negated {
			return true
		}
	}

	return false
}

// refreshOnlyRecordedVariables returns the names of the shell variables the line's `||` branch assigns before it breaks:
// the wait loop's way of remembering that the refresh failed. The loop names them as it likes, so the checks that read
// the name take it from here rather than spelling it.
func refreshOnlyRecordedVariables(line string) []string {
	_, rest, ok := strings.Cut(line, "apply -refresh-only")
	if !ok {
		return nil
	}

	_, branch, ok := strings.Cut(rest, "||")
	if !ok {
		return nil
	}

	before, _, broke := strings.Cut(branch, "break")
	if !broke {
		return nil
	}

	var names []string

	for _, match := range shellVariableAssignment.FindAllStringSubmatch(before, -1) {
		names = append(names, match[1])
	}

	return names
}

// refreshOnlyFAILLines returns the section's command lines that print a FAIL naming the refresh-only failure: each line
// is no comment once trimmed, and one of its echoed strings carries "refresh-only" beside a FAIL of its own - one that
// is not the timeout FAIL, which a line may print as well. A `# FAIL ...` comment states the outcome for a maintainer in
// the runbook's prose; it prints nothing, so it is no evidence that the check line reports the case.
func refreshOnlyFAILLines(section []string, timeoutFAIL string) []string {
	var found []string

	for _, line := range section {
		trimmed := strings.TrimSpace(line)
		if isRunbookComment(trimmed) {
			continue
		}

		if _, printed := firstEchoMatching(trimmed, func(printed string) bool {
			return strings.Contains(strings.ToLower(printed), "refresh-only") &&
				strings.Contains(strings.ReplaceAll(printed, timeoutFAIL, ""), "FAIL")
		}); printed {
			found = append(found, trimmed)
		}
	}

	return found
}

// isRunbookComment reports whether the trimmed line is a comment of the runbook's shell blocks.
func isRunbookComment(trimmed string) bool {
	return strings.HasPrefix(trimmed, "#")
}

// firstEchoMatching returns the offset of the first `echo "..."` of the line whose quoted text matches, and whether the
// line holds one. The offset is where the printed text starts, so two calls on one line compare which of the two the
// shell reaches first.
func firstEchoMatching(line string, matches func(printed string) bool) (int, bool) {
	for offset := 0; ; {
		i := strings.Index(line[offset:], `echo "`)
		if i < 0 {
			return 0, false
		}

		start := offset + i + len(`echo "`)

		end := strings.Index(line[start:], `"`)
		if end < 0 {
			return 0, false
		}

		if matches(line[start : start+end]) {
			return start, true
		}

		offset = start + end + 1
	}
}

// testsRecordedVariable reports whether the line tests one of the names, as `[ "$refresh_failed" -eq 1 ]` does.
func testsRecordedVariable(line string, names []string) bool {
	return testsRecordedVariableAt(line, names) >= 0
}

// testsRecordedVariableAt returns the offset of the first test of one of the names on the line, and -1 when it tests
// none. A name must end there: `$refresh_failed` is no test of a loop variable named `refresh`.
func testsRecordedVariableAt(line string, names []string) int {
	at := -1

	for _, name := range names {
		for offset := 0; ; {
			i := strings.Index(line[offset:], "$"+name)
			if i < 0 {
				break
			}

			end := offset + i + 1 + len(name)

			if end == len(line) || !isShellNameByte(line[end]) {
				if at < 0 || offset+i < at {
					at = offset + i
				}

				break
			}

			offset = end
		}
	}

	return at
}

// isShellNameByte reports whether b continues a shell variable's name.
func isShellNameByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
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

	if !shellVariableAssignment.MatchString(before) {
		return "breaks without recording the failure first"
	}

	return ""
}

// shellVariableAssignment matches an assignment of a shell variable, the way R4's loop records the failure it broke on:
// the loop names the variable as it likes. The name is captured so the checks that read it need no spelling.
var shellVariableAssignment = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=`)

// TestRefreshOnlyFAILLines pins the helper the check line's pins rest on, so a comment cannot count as the command that
// prints the FAIL: a `# FAIL ...` line of the runbook's prose prints nothing, a line whose echoed FAIL is the timeout's
// alone names no refresh-only failure, and a command line that echoes the case's own FAIL is the one the tests read
// (issue #567).
func TestRefreshOnlyFAILLines(t *testing.T) {
	t.Parallel()

	const timeoutFAIL = "FAIL edge_endpoint still empty after 10 minutes"

	tests := []struct {
		name    string
		section []string
		want    []string
	}{
		{
			name:    "a comment that states the outcome is no printing line",
			section: []string{"# FAIL refresh-only failed: a state lock: as the apply line's FAIL says"},
		},
		{
			name:    "a line that prints the timeout FAIL alone names no refresh-only failure",
			section: []string{`[ -n "$E" ] && echo "PASS edge_endpoint is set" || echo "` + timeoutFAIL + `"`},
		},
		{
			name:    "a command line that echoes the case's own FAIL",
			section: []string{`if [ "$refresh_failed" -eq 1 ]; then echo "FAIL refresh-only failed: read the error above"; fi`},
			want:    []string{`if [ "$refresh_failed" -eq 1 ]; then echo "FAIL refresh-only failed: read the error above"; fi`},
		},
		{
			name:    "a line that prints both FAILs",
			section: []string{`if [ "$refresh_failed" -eq 1 ]; then echo "FAIL refresh-only failed: read the error above"; else echo "` + timeoutFAIL + `"; fi`},
			want:    []string{`if [ "$refresh_failed" -eq 1 ]; then echo "FAIL refresh-only failed: read the error above"; else echo "` + timeoutFAIL + `"; fi`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := refreshOnlyFAILLines(tc.section, timeoutFAIL); !slices.Equal(got, tc.want) {
				t.Errorf("refreshOnlyFAILLines(%q, %q) = %q, want %q", tc.section, timeoutFAIL, got, tc.want)
			}
		})
	}
}

// TestRemovesTheBackupOnceExplained pins how R4's exit 2 FAIL is read (issue #607): the outcome has to say to remove the
// backup after it says to explain the diff, because a wording that negates the removal leaves the file where it is just
// as the old wording did.
func TestRemovesTheBackupOnceExplained(t *testing.T) {
	t.Parallel()

	const backup = "state-backup.json"

	tests := []struct {
		name    string
		outcome string
		want    bool
	}{
		{
			name:    "the runbook's guarded wording",
			outcome: `FAIL exit 2: read the diff, and keep "$T/` + backup + `" until it is explained, then [ -f "$T/` + backup + `" ] && rm -r "$T"`,
			want:    true,
		},
		{
			name:    "an unguarded removal after the explanation",
			outcome: `FAIL exit 2: read the diff, and keep "$T/` + backup + `" until it is explained, then rm -r "$T"`,
			want:    true,
		},
		{
			name:    "a negated removal after the explanation",
			outcome: `FAIL exit 2: read the diff, and keep "$T/` + backup + `" until it is explained, do not remove it`,
		},
		{
			name:    "a removal of nothing after the explanation",
			outcome: `FAIL exit 2: keep "$T/` + backup + `" until it is explained, remove nothing`,
		},
		{
			name:    "the removal dropped",
			outcome: `FAIL exit 2: read the diff, and keep "$T/` + backup + `" until it is explained`,
		},
		{
			name:    "no explanation at all",
			outcome: `FAIL exit 2: keep "$T/` + backup + `", then [ -f "$T/` + backup + `" ] && rm -r "$T"`,
		},
		{
			name:    "the removal described rather than told: nothing says the maintainer removes the file",
			outcome: `FAIL exit 2: read the diff, which is explained above: the backup was removed earlier`,
		},
		{
			name:    "a deletion the outcome remembers rather than tells",
			outcome: `FAIL exit 2: the cause is explained, and the earlier run deleted the backup`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := removesTheBackupOnceExplained(tc.outcome); got != tc.want {
				t.Errorf("removesTheBackupOnceExplained(%q) = %t, want %t", tc.outcome, got, tc.want)
			}
		})
	}
}

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
