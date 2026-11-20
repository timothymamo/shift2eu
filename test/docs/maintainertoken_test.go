// Pins PRD #161 milestone 3 in the documentation: after the switch-over the agents' GitHub credential is the
// agents App's installation token, which a job on the Mac mints (docs/dev-environment.md, "The agents App"), so
// the maintainer's fine-grained personal token is no longer theirs. The docs, CLAUDE.md and the skills therefore
// do not tell a maintainer to create one for the agents, and do not name GITHUB_TOKEN as a credential an agent
// holds. The ADRs (docs/adr/) and prds/ are left alone: they record decisions and plans as they were written.
package docs_test

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// devEnvironmentPage is the page the VM is set up from, and the one whose switch-over runbook tells the
// maintainer which variable to delete.
const devEnvironmentPage = "docs/dev-environment.md"

// maintainerTokenFiles lists the files a maintainer and an agent read about the VM's credentials: the CLAUDE.md
// every agent loads, README.md and every page under docs/ except the ADRs, and the skills.
func maintainerTokenFiles(t *testing.T, fsys fs.FS) []string {
	t.Helper()

	files := append([]string{"CLAUDE.md"}, markdownFiles(t, fsys, "docs", ".claude/skills")...)

	kept := make([]string, 0, len(files))

	for _, file := range files {
		if strings.HasPrefix(file, "docs/adr/") {
			continue
		}

		kept = append(kept, file)
	}

	return kept
}

// creatingAPersonalToken matches the phrase that names the kind of token the agents used to need, and
// agentsCredentialContext the words that make a line about a token a line about the agents' credential: the
// maintainer's own admin token for the gate check is a fine-grained token too, and that one stays.
var (
	creatingAPersonalToken  = regexp.MustCompile(`(?i)fine-grained`)
	agentsCredentialContext = regexp.MustCompile(`(?i)(VM_ENV|vm-env|daemon\.env|\bagents\b|\bexport\b)`)
)

// TestTheDocsNameNoPersonalTokenOfTheAgents pins PRD #161 milestone 3's second acceptance criterion: the docs no
// longer tell the maintainer to create a fine-grained personal access token for the agents. No page a maintainer
// or an agent reads names GITHUB_TOKEN, except the switch-over runbook on docs/dev-environment.md, whose step 6
// tells the maintainer by name which variable to delete - the only GITHUB_TOKEN left in the repository is GitHub
// Actions' own `secrets.GITHUB_TOKEN` in the workflows, which no page here describes - and none pairs a
// fine-grained token with the agents, the VM's environment or an export.
func TestTheDocsNameNoPersonalTokenOfTheAgents(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)

	for _, file := range maintainerTokenFiles(t, fsys) {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		lines := strings.Split(string(data), "\n")

		mentions, err := tokenMentions(file, lines)
		if err != nil {
			t.Error(err)

			continue
		}

		for _, mention := range mentions {
			t.Errorf("%s:%d names GITHUB_TOKEN: the agents' GitHub credential is the agents App's installation token, which the mint on the Mac writes into the VM, so the maintainer's personal token is no part of the VM any more (PRD #161 milestone 3): %s",
				mention.file, mention.line, mention.text)
		}

		for i, line := range lines {
			if creatingAPersonalToken.MatchString(line) && agentsCredentialContext.MatchString(line) {
				t.Errorf("%s:%d pairs a fine-grained personal token with the agents' credentials: the docs must not tell the maintainer to create one for the agents any more (PRD #161 milestone 3): %s",
					file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// tokenMention is one line of a page that names the maintainer's personal token where it may not.
type tokenMention struct {
	file string
	line int
	text string
}

// tokenMentions returns every line that names GITHUB_TOKEN where it may not, and an error when the page's fences
// do not balance. Only the switch-over runbook of docs/dev-environment.md is allowed to name the variable: step 6
// has the maintainer delete it from the .envrc above the workspace and from any VM_ENV override, and naming it is
// the only way to say which one. Everywhere else - the rest of that page, the other pages, CLAUDE.md and the
// skills - the ban stays.
func tokenMentions(file string, lines []string) ([]tokenMention, error) {
	allowed := make([]bool, len(lines))

	if file == devEnvironmentPage {
		var unclosedAt int

		allowed, unclosedAt = runbookSectionLines(lines)
		if unclosedAt > 0 {
			return nil, fmt.Errorf("unclosed code fence in %s opened on line %d: a fence left open in this page leaves no way to say where the switch-over runbook section ends, so no line is allowed to name GITHUB_TOKEN until it is closed (issue #360)", file, unclosedAt)
		}
	}

	var mentions []tokenMention

	for i, line := range lines {
		if !strings.Contains(line, "GITHUB_TOKEN") || allowed[i] {
			continue
		}

		mentions = append(mentions, tokenMention{file: file, line: i + 1, text: strings.TrimSpace(line)})
	}

	return mentions, nil
}

// runbookSectionLines reports, line by line, which lines of page fall inside its "The switch-over runbook"
// section: from that heading to the next heading of the same or a higher level. The lines of a fenced code
// block are no Markdown, so a runbook's `# Mac, host clone` comment neither ends the section nor, before it,
// opens one (issue #360). It also reports the 1-based line of a fenced code block the page leaves open at its
// end, or 0 when every fence closes: such a fence would otherwise look like part of the section to the end of
// the page and let a line under a later heading pass the check.
func runbookSectionLines(lines []string) (inside []bool, unclosedAt int) {
	const heading = "The switch-over runbook"

	inside = make([]bool, len(lines))
	level := 0
	var marker byte
	var width int
	inFence := false

	for i, line := range lines {
		if inFence {
			if closesFence(line, marker, width) {
				inFence, unclosedAt = false, 0
			}

			inside[i] = level > 0

			continue
		}

		if m, w, _, ok := openingFence(line); ok {
			inFence, marker, width, unclosedAt = true, m, w, i+1
			inside[i] = level > 0

			continue
		}

		if n, title, ok := markdownHeading(line); ok {
			switch {
			case level == 0 && strings.Contains(title, heading):
				level = n
				inside[i] = true

				continue
			case level > 0 && n <= level:
				level = 0
			}
		}

		inside[i] = level > 0
	}

	return inside, unclosedAt
}

// TestTheRunbookSectionSkipsFencedCodeBlocks pins how the section scan finds the end of the switch-over
// runbook (issue #360): the runbooks quote shell comment lines that start with "#", and a fenced code
// block's body is no Markdown, so such a line neither ends the section nor opens one, while a real heading
// after the block still does.
func TestTheRunbookSectionSkipsFencedCodeBlocks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		page         []string
		want         []int
		wantUnclosed bool
	}{
		{
			name: "a shell comment in a fenced block does not end the section, and the heading after it does",
			page: []string{
				"# Development environment",
				"## Credentials",
				"### The switch-over runbook",
				"Delete the GITHUB_TOKEN line from the `.envrc`.",
				"```bash",
				"# Mac, host clone",
				"make vm-env",
				"```",
				"Then revoke the token on GitHub.",
				"## Agent permissions",
				"GITHUB_TOKEN is no credential of the agents.",
			},
			want: []int{3, 4, 5, 6, 7, 8, 9},
		},
		{
			name: "a shell comment in a tilde-fenced block does not end the section, and the heading after it does",
			page: []string{
				"# Development environment",
				"### The switch-over runbook",
				"~~~bash",
				"# VM",
				"journalctl -u shift2eu",
				"~~~",
				"### The gate",
				"GITHUB_TOKEN is not the gate check's token.",
			},
			want: []int{2, 3, 4, 5, 6},
		},
		{
			name: "a heading inside a fenced block before the section does not open it",
			page: []string{
				"# Development environment",
				"```bash",
				"# The switch-over runbook",
				"```",
				"GITHUB_TOKEN is no credential of the agents.",
			},
			want: nil,
		},
		{
			name: "a fence the page leaves open is reported",
			page: []string{
				"# Development environment",
				"### The switch-over runbook",
				"```bash",
				"make vm-env",
			},
			want:         []int{2, 3, 4},
			wantUnclosed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []int

			inside, unclosedAt := runbookSectionLines(strings.Split(strings.Join(tc.page, "\n"), "\n"))

			for i, in := range inside {
				if in {
					got = append(got, i+1)
				}
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("runbookSectionLines() marked the lines inside the switch-over runbook (-want +got):\n%s", diff)
			}

			if unclosed := unclosedAt != 0; unclosed != tc.wantUnclosed {
				t.Errorf("runbookSectionLines() reported the unclosed fence on line %d, want unclosed: %t", unclosedAt, tc.wantUnclosed)
			}
		})
	}
}

// TestTheDocsDoNotWidenTheRunbookOnAnUnclosedFence pins the failure mode of the scan above (issue #360): a fence
// the switch-over runbook opens and never closes used to keep the scan inside the section to the end of the page,
// so a GITHUB_TOKEN line under the next heading again passed the check. A broken fence must not widen the
// allowance: the page is reported instead.
func TestTheDocsDoNotWidenTheRunbookOnAnUnclosedFence(t *testing.T) {
	t.Parallel()

	lines := strings.Split(strings.Join([]string{
		"# Development environment",
		"### The switch-over runbook",
		"6. **Remove your token from the VM:** delete the GITHUB_TOKEN variable from the `.envrc`.",
		"```bash",
		"make vm-env",
		"## Agent permissions",
		"GITHUB_TOKEN is no credential of the agents.",
	}, "\n"), "\n")

	mentions, err := tokenMentions(devEnvironmentPage, lines)
	if err == nil {
		t.Fatalf("tokenMentions(%s) accepted a page whose code fence is never closed: a fence left open in this page must be reported, not followed to the end of the page (issue #360)", devEnvironmentPage)
	}

	if !strings.Contains(err.Error(), "unclosed code fence in "+devEnvironmentPage) {
		t.Errorf("tokenMentions(%s) reported %q, want a message naming the unclosed code fence in that page", devEnvironmentPage, err)
	}

	if mentions != nil {
		t.Errorf("tokenMentions(%s) returned mentions alongside the error: %v, want none: a page whose runbook section cannot be delimited names no allowed line (issue #360)", devEnvironmentPage, mentions)
	}
}

// markdownHeading returns the level and title of an ATX heading line such as "## Credentials" or "### The
// switch-over runbook".
func markdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")

	hashes := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
	if hashes < 1 || hashes > 6 || len(trimmed) <= hashes || trimmed[hashes] != ' ' {
		return 0, "", false
	}

	return hashes, strings.TrimSpace(trimmed[hashes:]), true
}

// TestTheDocsAllowTheTokenOnlyInTheSwitchOverRunbook pins where the docs may name the variable step 6 tells the
// maintainer to delete: inside that one section, on docs/dev-environment.md, and nowhere else.
func TestTheDocsAllowTheTokenOnlyInTheSwitchOverRunbook(t *testing.T) {
	t.Parallel()

	lines := strings.Split(strings.Join([]string{
		"# Development environment",
		"## Credentials",
		"Set GITHUB_TOKEN in the .envrc above the workspace.",
		"### The switch-over runbook",
		"6. **Remove your token from the VM:** delete the GITHUB_TOKEN variable from the `.envrc`.",
		"#### Rolling back",
		"Put GITHUB_TOKEN back in the `.envrc` and run `make vm-env`.",
		"### The gate",
		"GITHUB_TOKEN is not the gate check's token.",
		"## Agent permissions",
		"GITHUB_TOKEN is no credential of the agents.",
	}, "\n"), "\n")

	cases := []struct {
		name string
		file string
		want []int
	}{
		{
			name: "the switch-over runbook may name the variable it tells the maintainer to delete",
			file: devEnvironmentPage,
			want: []int{3, 9, 11},
		},
		{
			name: "no other page may name it, even in a section of the same name",
			file: "docs/contributing/agents.md",
			want: []int{3, 5, 7, 9, 11},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got []int

			mentions, err := tokenMentions(tc.file, lines)
			if err != nil {
				t.Fatalf("tokenMentions(%s) failed on a page whose fences balance: %v", tc.file, err)
			}

			for _, mention := range mentions {
				got = append(got, mention.line)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("tokenMentions(%s) reported the lines %v, want %v", tc.file, got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("tokenMentions(%s) reported the lines %v, want %v", tc.file, got, tc.want)
				}
			}
		})
	}
}

// TestTheDocsHaveNoGateTransitionMode pins the documentation of gate check's contract (PRD #161 milestone 3): the
// transition flag is gone from the Makefile and the script, so the page that describes them must not offer
// GATE_TRANSITION or --transition, nor a `transition:` line to quote. The release-tag skill's copy of the same
// contract is pinned in test/devenv (gatefiles_test.go).
func TestTheDocsHaveNoGateTransitionMode(t *testing.T) {
	t.Parallel()

	page := devEnvironmentPage

	fsys := os.DirFS(repoRoot)

	data, err := fs.ReadFile(fsys, page)
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}

	if !strings.Contains(string(data), "make gate-check") {
		t.Fatalf("%s no longer describes the gate check, so this test cannot pin how it documents the check's result (PRD #161 milestone 2)", page)
	}

	for i, line := range strings.Split(string(data), "\n") {
		for _, gone := range []string{"GATE_TRANSITION", "--transition", "transition:"} {
			if strings.Contains(line, gone) {
				t.Errorf("%s:%d mentions %q: the gate check has no transition mode any more - a gap fails it, always, and the switch-over is done (PRD #161 milestone 3): %s",
					page, i+1, gone, strings.TrimSpace(line))
			}
		}
	}
}
