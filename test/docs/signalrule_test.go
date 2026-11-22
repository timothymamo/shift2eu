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
// the agent at the rule: the rule's title without the bold markers.
const signalRulePhrase = "Signal only what you started"

// checkPhrases are the fragments that make a skill tell an agent to run the
// repository's checks.
var checkPhrases = []string{"make check", "make test", "go test"}

// skillGlob matches every skill's file, the one page an agent loads for a
// skill, and nothing else in the skills directory (such as prd-create's
// template.md).
const skillGlob = ".claude/skills/*/SKILL.md"

// skillsThatMustRunChecks are two skills the matched set must hold, so the
// check over the skills cannot pass because no skill matched at all: the coder
// and the tester are told to run the checks by their skills.
var skillsThatMustRunChecks = []string{
	".claude/skills/go-implement/SKILL.md",
	".claude/skills/go-tdd/SKILL.md",
}

// numberedItemStart matches the first line of a numbered list item and keeps
// the text that follows its marker.
var numberedItemStart = regexp.MustCompile(`^\d+\.\s+(.*)$`)

// numberedItem is one numbered list item of a Markdown section: the 0-based
// index of its first line within the section's body, and its text, the item's
// lines joined into one space-separated line.
type numberedItem struct {
	line int
	text string
}

// rulesSection returns the body of CLAUDE.md's `## Rules` section and the
// 0-based index of its heading line: the section runs from the heading to the
// next `## ` heading.
func rulesSection(data string) (body []string, start int, ok bool) {
	lines := strings.Split(data, "\n")
	for start, line := range lines {
		if strings.TrimSpace(line) != rulesHeading {
			continue
		}
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
				end = i
				break
			}
		}
		return lines[start:end], start, true
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
			items = append(items, numberedItem{line: i, text: m[1]})
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

// runsChecks reports whether a skill's text tells an agent to run the
// repository's checks.
func runsChecks(data string) bool {
	for _, phrase := range checkPhrases {
		if strings.Contains(data, phrase) {
			return true
		}
	}
	return false
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

	body, start, ok := rulesSection(readDoc(t, os.DirFS(repoRoot), claudeMD))
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
	var (
		rule  *numberedItem
		found bool
	)
	for i, item := range items {
		if strings.HasPrefix(item.text, signalRuleTitle) {
			rule, found = &items[i], true
			break
		}
	}
	if !found {
		t.Fatalf("%s:%d: the %q section has no numbered item that starts with %q (issue #465); the rules are:\n%s",
			claudeMD, start+1, rulesHeading, signalRuleTitle, strings.Join(itemTitles(items), "\n"))
	}

	// line is the 0-based index of the heading, so the item's first line is one
	// line past the heading plus the item's own offset.
	at := start + rule.line + 1
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
// an agent to run the checks (`make check`, `make test` or `go test`) does not
// say "Signal only what you started", the phrase that points the agent at
// CLAUDE.md's rule against signalling a process it did not start (issue #465).
// A skill that sends an agent to the checks is a skill whose run can leave
// processes behind, and the agent must read there that it may signal only what
// it started, never a process it found by name: the runs of the other agents on
// the machine have the same names. The list of skills is read from the
// repository, so a new skill that runs the checks is held to the rule without
// an edit here; go-tdd and go-implement must be in it, so a repository with no
// skill left to match cannot make this test pass without checking anything.
func TestSkillsThatRunChecksCiteTheSignalRule(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)
	skills, err := fs.Glob(fsys, skillGlob)
	if err != nil {
		t.Fatalf("listing %s: %v", skillGlob, err)
	}
	if len(skills) == 0 {
		t.Fatalf("%s: no skill matches %q, so no skill can point at the rule %q (issue #465)", repoRoot, skillGlob, signalRulePhrase)
	}

	var matched, missing []string
	for _, skill := range skills {
		data := readDoc(t, fsys, skill)
		if !runsChecks(data) {
			continue
		}
		matched = append(matched, skill)
		if !strings.Contains(data, signalRulePhrase) {
			missing = append(missing, skill)
		}
	}
	t.Logf("skills that tell an agent to run the checks, from %q: %s", skillGlob, strings.Join(matched, ", "))

	// The two skills below are checked anyway, so an empty or mis-globbed
	// matched set fails instead of passing with nothing to look at.
	for _, skill := range skillsThatMustRunChecks {
		if !slices.Contains(matched, skill) {
			t.Fatalf("%s: %s does not look like a skill that tells an agent to run the checks, but it runs them: the check over the matched skills would pass without checking the skills that matter (issue #465)",
				skill, skill)
		}
	}

	if diff := cmp.Diff([]string{}, missing); diff != "" {
		t.Errorf("skill texts that run the checks but never say %q (-want +got):\n%s\nEvery one of these skills must point at CLAUDE.md's rule that an agent signals only the processes it started itself (issue #465)",
			signalRulePhrase, diff)
	}
}
