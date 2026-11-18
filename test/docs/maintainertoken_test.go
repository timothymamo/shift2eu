// Pins PRD #161 milestone 3 in the documentation: after the switch-over the agents' GitHub credential is the
// agents App's installation token, which a job on the Mac mints (docs/dev-environment.md, "The agents App"), so
// the maintainer's fine-grained personal token is no longer theirs. The docs, CLAUDE.md and the skills therefore
// do not tell a maintainer to create one for the agents, and do not name GITHUB_TOKEN as a credential an agent
// holds. The ADRs (docs/adr/) and prds/ are left alone: they record decisions and plans as they were written.
package docs_test

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

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
// or an agent reads names GITHUB_TOKEN at all - the only GITHUB_TOKEN left in the repository is GitHub Actions'
// own `secrets.GITHUB_TOKEN` in the workflows, which no page here describes - and none pairs a fine-grained token
// with the agents, the VM's environment or an export.
func TestTheDocsNameNoPersonalTokenOfTheAgents(t *testing.T) {
	t.Parallel()

	fsys := os.DirFS(repoRoot)

	for _, file := range maintainerTokenFiles(t, fsys) {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "GITHUB_TOKEN") {
				t.Errorf("%s:%d names GITHUB_TOKEN: the agents' GitHub credential is the agents App's installation token, which the mint on the Mac writes into the VM, so the maintainer's personal token is no part of the VM any more (PRD #161 milestone 3): %s",
					file, i+1, strings.TrimSpace(line))
			}

			if creatingAPersonalToken.MatchString(line) && agentsCredentialContext.MatchString(line) {
				t.Errorf("%s:%d pairs a fine-grained personal token with the agents' credentials: the docs must not tell the maintainer to create one for the agents any more (PRD #161 milestone 3): %s",
					file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// TestTheDocsHaveNoGateTransitionMode pins the documentation of gate check's contract (PRD #161 milestone 3): the
// transition flag is gone from the Makefile and the script, so the page that describes them must not offer
// GATE_TRANSITION or --transition, nor a `transition:` line to quote. The release-tag skill's copy of the same
// contract is pinned in test/devenv (gatefiles_test.go).
func TestTheDocsHaveNoGateTransitionMode(t *testing.T) {
	t.Parallel()

	const page = "docs/dev-environment.md"

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
