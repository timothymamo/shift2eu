package docs_test

import (
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// rulesHeading is the CLAUDE.md heading whose numbered items are the rules
// every agent follows. The section runs from it to the next `## ` heading.
const rulesHeading = "## Rules"

// signalRuleTitle opens the numbered rule that says an agent signals only the
// processes it started itself.
const signalRuleTitle = "**Signal only what you started.**"

// signalRuleCommands are the process-selection commands the rule forbids: they
// pick a process by name, and the runs of the other agents on the machine have
// names too.
var signalRuleCommands = []string{"pgrep", "pidof", "pkill"}

// signalRulePhrase is the phrase a skill that runs the checks uses to point
// the agent at the rule: the rule's title without the bold markers. It is only
// the start of the citation, which also names the rule's number (see
// citation).
const signalRulePhrase = "Signal only what you started"

// checkPhrases are the fragments that make a skill tell an agent to run the
// repository's checks: the make targets, the Go commands and the name under
// which CLAUDE.md's `### Dispatcher` table gives the dispatcher's checks. The
// dispatch skill never names those commands itself, it runs `<checks>` and says
// they "must pass on a merged scratch tree", so the table's name is matched by
// the fragment it writes.
var checkPhrases = []string{"make check", "make test", "go test", "make lint", "govulncheck", "merged scratch tree"}

// skillGlob matches every skill's file, the one page an agent loads for a
// skill, and nothing else in the skills directory (such as prd-create's
// template.md).
const skillGlob = ".claude/skills/*/SKILL.md"

// skillsThatMustRunChecks are the skills the matched set must hold, so the
// check over the skills cannot pass because no skill matched at all: these
// skills send an agent to the checks and every one of them must point at the
// rule. Only a skill that tells an agent to run a check itself belongs here.
// review-change is not one: it mentions `make lint` to describe what the lint
// run catches, and if that description is reworded the skill may stop matching
// checkPhrases altogether. It is checked whenever it does match, like every
// other skill, and holding it here would then fail with a message about a skill
// that runs the checks, which it does not.
var skillsThatMustRunChecks = []string{
	".claude/skills/dispatch/SKILL.md",
	".claude/skills/go-implement/SKILL.md",
	".claude/skills/go-tdd/SKILL.md",
	".claude/skills/security-audit/SKILL.md",
}

// numberedItemStart matches the first line of a numbered list item and keeps
// the number the list gives it and the text that follows its marker.
var numberedItemStart = regexp.MustCompile(`^(\d+)\.\s+(.*)$`)

// numberedItem is one numbered list item of a Markdown section: the number the
// list gives it, the 0-based index of its first line within the section's
// body, and its text, the item's lines joined into one space-separated line.
type numberedItem struct {
	number string
	line   int
	text   string
}

// rulesSection returns the body of CLAUDE.md's `## Rules` section and the
// 0-based index of its heading line: the section runs from the heading to the
// next `## ` heading.
func rulesSection(data string) (body []string, start int, ok bool) {
	lines := strings.Split(data, "\n")
	for heading, line := range lines {
		if strings.TrimSpace(line) != rulesHeading {
			continue
		}
		end := len(lines)
		for i := heading + 1; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
				end = i
				break
			}
		}
		return lines[heading:end], heading, true
	}
	return nil, 0, false
}

// numberedItems returns the numbered list items of a section's body, in the
// order they are written. A line that does not start a new item continues the
// current one until a blank line ends it; the body's own heading line is not
// an item.
func numberedItems(body []string) []numberedItem {
	var items []numberedItem
	open := -1
	for i, line := range body {
		if m := numberedItemStart.FindStringSubmatch(line); m != nil {
			items = append(items, numberedItem{number: m[1], line: i, text: m[2]})
			open = len(items) - 1
			continue
		}
		if strings.TrimSpace(line) == "" {
			open = -1
			continue
		}
		if open >= 0 {
			items[open].text += " " + strings.TrimSpace(line)
		}
	}
	for i := range items {
		items[i].text = strings.Join(strings.Fields(items[i].text), " ")
	}
	return items
}

// TestNumberedItemsReadsARuleWithANestedList fails when numberedItems stops
// reading a rule whose body is a nested Markdown list as one item (issue #469).
// Rule 10 is written as its bold title followed by a tight `-` list, with some
// of those sub-items carrying a deeper sub-item and inline code that holds
// backticks, braces, `$!` and `&&`: the item must keep its own number and its
// first line, and its text must hold the title and the text of every sub-item
// in the order they are written. A following numbered item still starts a new
// item, and a blank line still ends one, so a paragraph below the rule is no
// part of it. The old one-paragraph shape stays covered, including a rule
// wrapped over two lines.
func TestNumberedItemsReadsARuleWithANestedList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []numberedItem
	}{
		{
			name: "a rule with two levels of sub-items and inline code",
			body: strings.Join([]string{
				"## Rules",
				"",
				"10. **Signal only what you started.**",
				"    - Never: `kill`, `pkill`, `killall` on a process you did not start.",
				"    - To be able to stop a run of your own: prefer your agent's tool's background mode,",
				"      which stops the whole run.",
				"      - Otherwise start it as `cd <path> && { <cmd> < /dev/null > <log> 2>&1 & echo $!; }`",
				"        and note the pid and its start time.",
				"    - Why: other agents' runs share this machine.",
				"11. **Change files with your file-editing tool.**",
				"    - No heredoc, `cat >`, `tee` or `sed -i`.",
			}, "\n"),
			want: []numberedItem{
				{
					number: "10",
					line:   2,
					text: "**Signal only what you started.**" +
						" - Never: `kill`, `pkill`, `killall` on a process you did not start." +
						" - To be able to stop a run of your own: prefer your agent's tool's background mode," +
						" which stops the whole run." +
						" - Otherwise start it as `cd <path> && { <cmd> < /dev/null > <log> 2>&1 & echo $!; }`" +
						" and note the pid and its start time." +
						" - Why: other agents' runs share this machine.",
				},
				{
					number: "11",
					line:   9,
					text:   "**Change files with your file-editing tool.** - No heredoc, `cat >`, `tee` or `sed -i`.",
				},
			},
		},
		{
			name: "a blank line ends the rule, so a paragraph below it is no part of the item",
			body: strings.Join([]string{
				"## Rules",
				"",
				"10. **First rule.**",
				"    - a sub-item.",
				"",
				"A paragraph that follows the rule.",
				"11. **Second rule.**",
			}, "\n"),
			want: []numberedItem{
				{number: "10", line: 2, text: "**First rule.** - a sub-item."},
				{number: "11", line: 6, text: "**Second rule.**"},
			},
		},
		{
			name: "a rule in the old one-paragraph shape, wrapped over two lines",
			body: strings.Join([]string{
				"## Rules",
				"",
				"1. **Never write to a cloud.** Source adapters use read-only APIs only.",
				"   No create, update, delete or tag calls.",
				"",
			}, "\n"),
			want: []numberedItem{
				{
					number: "1",
					line:   2,
					text:   "**Never write to a cloud.** Source adapters use read-only APIs only. No create, update, delete or tag calls.",
				},
			},
		},
		{
			name: "a section with no numbered item at all",
			body: strings.Join([]string{
				"## Rules",
				"",
				"Everything here is prose.",
				"",
			}, "\n"),
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := numberedItems(strings.Split(tc.body, "\n"))
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(numberedItem{})); diff != "" {
				t.Errorf("numberedItems() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// runsChecks reports whether a skill's text tells an agent to run the
// repository's checks.
func runsChecks(text string) bool {
	for _, phrase := range checkPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// squash turns every run of whitespace into a single space, so a phrase is
// still found when Markdown wraps it over two lines: a pointer may be written
// as "... what you started" and "(CLAUDE.md rule 10)" on the next line, which
// is the same sentence to the agent reading it.
func squash(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// findSignalRule returns the numbered rule of CLAUDE.md's `## Rules` section
// that opens with signalRuleTitle, and the 1-based number of the line it starts
// on. start is the 0-based index of the heading, so the item's 1-based line is
// start + item.line + 1.
func findSignalRule(t *testing.T, data string) (rule numberedItem, line int) {
	t.Helper()

	body, start, ok := rulesSection(data)
	if !ok {
		t.Fatalf("%s: no %q section, which holds the rule against signalling a process by name (issue #465)", claudeMD, rulesHeading)
	}
	items := numberedItems(body)
	if len(items) == 0 {
		t.Fatalf("%s:%d: the %q section lists no numbered rule, so it cannot hold the rule that opens with %q (issue #465)",
			claudeMD, start+1, rulesHeading, signalRuleTitle)
	}

	// The headline of the item is what an agent scanning the rules sees first,
	// so the item must open with it rather than mention it in passing.
	for _, item := range items {
		if strings.HasPrefix(item.text, signalRuleTitle) {
			return item, start + item.line + 1
		}
	}
	t.Fatalf("%s:%d: the %q section has no numbered item that starts with %q (issue #465); the rules are:\n%s",
		claudeMD, start+1, rulesHeading, signalRuleTitle, strings.Join(itemTitles(items), "\n"))
	return numberedItem{}, 0
}

// citation returns the phrase a skill must contain to point at the rule
// findSignalRule returns: the rule's title without the bold markers, followed
// by the number the rules list gives it. The number is read from CLAUDE.md
// rather than written here, so renumbering the rules fails every skill that
// still points at the old number instead of leaving the pointer wrong.
func citation(t *testing.T, data string) string {
	t.Helper()

	rule, _ := findSignalRule(t, data)
	return signalRulePhrase + " (CLAUDE.md rule " + rule.number + ")"
}

// TestCLAUDERulesForbidSignallingByProcessName fails when CLAUDE.md's
// `## Rules` section stops holding the rule that an agent signals only the
// processes it started itself: a numbered item that opens with
// `**Signal only what you started.**` and names `pgrep`, `pidof` and `pkill`
// (issue #465). Picking a process by name is what went wrong for the unit
// whose coder ran `pgrep -x make` and sent SIGTERM to the process group it
// found: the name matched another unit's `make`, whose run the coder did not
// start. The rule is not the whole protection, only the sentence the agents
// read, so it must name the commands that choose a process by name.
func TestCLAUDERulesForbidSignallingByProcessName(t *testing.T) {
	t.Parallel()

	rule, at := findSignalRule(t, readDoc(t, os.DirFS(repoRoot), claudeMD))
	for _, command := range signalRuleCommands {
		t.Run("names "+command, func(t *testing.T) {
			t.Parallel()

			if strings.Contains(rule.text, command) {
				return
			}
			t.Errorf("%s:%d: the rule that starts with %q does not name %q, one of the commands that pick a process by name (issue #465): the rule reads %q",
				claudeMD, at, signalRuleTitle, command, rule.text)
		})
	}
}

// itemTitles returns the first sentence of every item's text, so a failure
// names the rules that are there instead of only the one that is not.
func itemTitles(items []numberedItem) []string {
	titles := make([]string, len(items))
	for i, item := range items {
		title, _, _ := strings.Cut(item.text, ". ")
		titles[i] = "  - " + title
	}
	return titles
}

// TestSkillsThatRunChecksCiteTheSignalRule fails when a skill whose text tells
// an agent to run the checks (`make check`, `make test`, `go test`, `make lint`,
// `govulncheck`, or the dispatcher's `Checks on a merged scratch tree`) does not
// say "Signal only what you started (CLAUDE.md rule N)", where N is the number
// CLAUDE.md's `## Rules` section gives the rule against signalling a process it
// did not start (issue #465). A skill that sends an agent to the checks is a
// skill whose run can leave processes behind, and the agent must read there that
// it may signal only what it started, never a process it found by name: the runs
// of the other agents on the machine have the same names. The rule's number is
// part of the phrase, so renumbering the rules cannot leave a skill pointing at
// a rule that no longer says this. The list of skills is read from the
// repository, so a new skill that runs the checks is held to the rule without an
// edit here; the skills in skillsThatMustRunChecks must be in it, so a
// repository with no skill left to match cannot make this test pass without
// checking anything.
func TestSkillsThatRunChecksCiteTheSignalRule(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	cite := citation(t, readDoc(t, fsys, claudeMD))
	skills, err := fs.Glob(fsys, skillGlob)
	if err != nil {
		t.Fatalf("listing %s: %v", skillGlob, err)
	}
	if len(skills) == 0 {
		t.Fatalf("%s: no skill matches %q, so no skill can point at the rule %q (issue #465)", repoRoot, skillGlob, cite)
	}

	var matched, missing []string
	for _, skill := range skills {
		text := squash(readDoc(t, fsys, skill))
		if !runsChecks(text) {
			continue
		}
		matched = append(matched, skill)
		if !strings.Contains(text, cite) {
			missing = append(missing, skill)
		}
	}
	t.Logf("skills that tell an agent to run the checks, from %q: %s", skillGlob, strings.Join(matched, ", "))
	t.Logf("the phrase they must contain: %q", cite)

	// The skills below are checked anyway, so an empty or mis-globbed matched
	// set fails instead of passing with nothing to look at.
	for _, skill := range skillsThatMustRunChecks {
		if !slices.Contains(matched, skill) {
			t.Fatalf("%s: %s does not look like a skill that tells an agent to run the checks, but it runs them: the check over the matched skills would pass without checking the skills that matter (issue #465)",
				skill, skill)
		}
	}

	// missing holds no skill when every one of them points at the rule: an
	// empty and a nil slice are the same absence, so the length decides and the
	// failure names the skills that are silent.
	if len(missing) > 0 {
		t.Errorf("skill texts that run the checks but never say %q: %s\nEvery one of these skills must point at CLAUDE.md's rule that an agent signals only the processes it started itself (issue #465)",
			cite, strings.Join(missing, ", "))
	}
}
