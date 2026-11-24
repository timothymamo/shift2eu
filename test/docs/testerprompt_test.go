// Pins the tester's prompt in .dot-agent-deck.toml and the scratch directory it
// names (CLAUDE.md rule 3, issue #540).
//
// On 2026-11-24 a tester ran `env | grep` while looking for its scratchpad
// directory and printed DEEPSEEK_API_KEY, which then had to be rotated. Nothing
// told it where the directory is. The prompt now names one, a path under the
// worktree that .gitignore ignores, and repeats the rule that forbids printing
// the environment in one line, so a tester has neither a reason to look in the
// environment nor to forget the rule.
//
// The role's block is found by its name in the TOML's text, not by a TOML
// decoder: the repository takes no dependency it does not need, and one block
// with a name and one delimited prompt is all this test reads.
package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The deck's configuration and the scratch directory the tester's prompt names.
const (
	deckConfig       = ".dot-agent-deck.toml"
	testerRole       = "tester"
	testerScratchDir = ".scratch/"
	gitignoreFile    = ".gitignore"
)

// scratchWord is the word `scratch`: the path alone would also pass for a
// directory named something else that happens to hold the letters.
var scratchWord = regexp.MustCompile(`\bscratch\b`)

// TestTesterPromptNamesItsScratchDirectory pins the line that keeps a tester
// from going to look for a place to write: its prompt names the directory, and
// the word `scratch` says that this is the directory the task's instructions
// mean by "your scratch directory".
func TestTesterPromptNamesItsScratchDirectory(t *testing.T) {
	t.Parallel()

	prompt := testerPrompt(t)

	if !strings.Contains(prompt, testerScratchDir) {
		t.Errorf("%s: the %s role's prompt never names its scratch directory %q, so a tester looks for one itself, and looking in the environment is how the last one did it (issue #540)", deckConfig, testerRole, testerScratchDir)
	}

	if !scratchWord.MatchString(prompt) {
		t.Errorf("%s: the %s role's prompt never says the word %q, so the directory it names is not recognisably the task's scratch directory (issue #540)", deckConfig, testerRole, "scratch")
	}
}

// TestTesterPromptRepeatsRule3 pins the rule in the tester's own prompt: the
// agent reads CLAUDE.md, but its prompt is the text it starts from and the one
// line it repeats is the one a tester broke. Only words hold the rule for the
// tester, which runs with --approve and is asked for nothing.
func TestTesterPromptRepeatsRule3(t *testing.T) {
	t.Parallel()

	prompt := testerPrompt(t)

	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, "printenv") && strings.Contains(line, "rule 3") {
			return
		}
	}

	t.Errorf("%s: the %s role's prompt holds no line with both %q and %q, so the rule that a tester broke is not repeated in the text it starts from (CLAUDE.md, rule 3; issue #540)", deckConfig, testerRole, "printenv", "rule 3")
}

// TestGitignoreIgnoresTheTesterScratchDirectory pins the other half of naming
// the directory: a scratch file must not show in git status, or a tester could
// commit one, and nothing under it belongs in git.
func TestGitignoreIgnoresTheTesterScratchDirectory(t *testing.T) {
	t.Parallel()

	gitignore := readDoc(t, os.DirFS(repoRoot), gitignoreFile)

	for _, line := range strings.Split(gitignore, "\n") {
		switch strings.TrimSpace(line) {
		case "/" + testerScratchDir, testerScratchDir:
			return
		}
	}

	t.Errorf("%s: no %q line, so the scratch directory the %s role's prompt names shows in git status and its files could be committed (issue #540)", gitignoreFile, "/"+testerScratchDir, testerRole)
}

// testerPrompt returns the tester role's prompt_template from the deck's
// configuration. The role blocks are split on their table header, and the block
// named `tester` is the one holder of the prompt.
func testerPrompt(t *testing.T) string {
	t.Helper()

	const (
		roleHeader = "[[orchestrations.roles]]"
		promptOpen = "prompt_template = \"\"\""
		promptEnd  = "\"\"\""
	)

	blocks := strings.Split(readDoc(t, os.DirFS(repoRoot), deckConfig), roleHeader)

	for _, block := range blocks[1:] {
		if !strings.Contains(block, "\nname = \""+testerRole+"\"") {
			continue
		}

		start := strings.Index(block, promptOpen)
		if start < 0 {
			t.Fatalf("%s: the %s role holds no %s", deckConfig, testerRole, promptOpen)
		}

		rest := block[start+len(promptOpen):]

		end := strings.Index(rest, promptEnd)
		if end < 0 {
			t.Fatalf("%s: the %s role's prompt_template never closes", deckConfig, testerRole)
		}

		return rest[:end]
	}

	t.Fatalf("%s: no role block is named %q", deckConfig, testerRole)

	return ""
}
