// Pins where the Claude Code CLI's version and integrity live: one committed
// file, .github/claude-code.yaml, which every workflow that installs the CLI
// reads and no workflow holds a copy of (ADR 0040's milestone-2 amendment, issue
// #277, PRD #175 milestone 2).
//
// Two workflows install the CLI from one pin: the AI reviewer's, and the
// scheduled security audit's model job, whose workflow PRD #175 milestone 2 adds
// in a later pull request. Both run the same CLI on the same subscription, so a
// bump is reviewed once; two pins could drift apart with no check comparing
// them. The pin check (ci/claudecodepin, issue #276) reads that one file, and a
// workflow that carried its own version and integrity would install a CLI no
// check holds to the registry.
package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

const (
	// claudeCodeInstallScript is the one installer both workflows run, with the
	// directory the CLI is extracted into, its platform package at the pinned
	// version and the pinned integrity (ADR 0033).
	claudeCodeInstallScript = ".github/scripts/install-claude-code.sh"

	// claudeCodeAuditWorkflowPath is the scheduled audit's workflow, whose model
	// job installs the same CLI (PRD #175 milestone 2, ADR 0040). A later pull
	// request of that milestone adds it; until then only the reviewer installs
	// the CLI.
	claudeCodeAuditWorkflowPath = ".github/workflows/scheduled-security-audit.yml"

	// claudeCodePinFile is the one committed file the Claude Code pin lives in:
	// the version under its # renovate: comment and the integrity beside it,
	// which both workflows' install steps read and ci/claudecodepin checks
	// against the npm registry (ADR 0040's milestone-2 amendment, issue #277).
	claudeCodePinFile = ".github/claude-code.yaml"

	// claudeCodePinStepName is the id: of the one step that reads that file's
	// two lines: the install step's env: takes what it exports, never a value of
	// the workflow's own (issue #277, round J).
	claudeCodePinStepName = "claude-code-pin"
)

// claudeCodePinOutputs are the values the install step's env: gives the pin: the
// whole output of the step that read the file, one per name, so neither the
// version nor the integrity is a value the workflow carries itself (ADR 0040's
// milestone-2 amendment, issue #277, round J).
var claudeCodePinOutputs = map[string]string{
	"CLAUDE_CODE_PIN_VERSION":   "${{ steps." + claudeCodePinStepName + ".outputs.version }}",
	"CLAUDE_CODE_PIN_INTEGRITY": "${{ steps." + claudeCodePinStepName + ".outputs.integrity }}",
}

// claudeCodePinEnvNames are the pin's old env: names, which no workflow may set:
// the version and the integrity live in the file alone, and reach the installer
// as the pin step's outputs under the names of claudeCodePinOutputs.
var claudeCodePinEnvNames = []string{"CLAUDE_CODE_VERSION", "CLAUDE_CODE_INTEGRITY"}

// claudeCodePinStepOutputs are the names the pin step writes to $GITHUB_OUTPUT
// for the install step's env: to read: both of the file's values, each written
// where GitHub reads the step's outputs, never one alone (issue #277, round K).
var claudeCodePinStepOutputs = []string{"version", "integrity"}

// claudeCodePinRead matches the command that reads the pin file's two lines:
// grep -E over an anchored ^version: or ^integrity: pattern. A step that only
// names the file's path, in a comment or an echo, reads nothing (issue #277,
// round J).
var claudeCodePinRead = regexp.MustCompile(`\bgrep\s+-E\s+'(\^version:|\^integrity:)`)

// claudeCodeWorkflowVersionLit matches a pin a workflow wrote itself: an exact
// X.Y.Z version, the shape the pin file's version: line holds.
var claudeCodeWorkflowVersionLit = regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)

// claudeCodeWorkflowIntegrityLit matches an npm integrity a workflow wrote
// itself: sha512-<base64 of a digest>, the shape the pin file's integrity: line
// holds.
var claudeCodeWorkflowIntegrityLit = regexp.MustCompile(`sha512-[A-Za-z0-9+/]{40,}={0,2}`)

// claudeCodeInstallStep is one step of one workflow that runs the installer.
type claudeCodeInstallStep struct {
	// workflow is the file the step belongs to, relative to the module root.
	workflow string
	// job is the key of the job the step belongs to: the steps a step before it
	// may read are its own job's alone (issue #277, round F finding 7).
	job string
	// num is the line of the step's run:.
	num int
	// run is the step's run: text.
	run string
	// pinNames are the env: names the step sets for the CLI's pin, and the
	// values it sets them to: the pin step's outputs, or a pin of the workflow's
	// own, which the test refuses.
	pinNames map[string]string
}

// claudeCodeWorkflowJob is one job of one workflow: its key and its steps.
type claudeCodeWorkflowJob struct {
	key   string
	steps []yamlread.Item
}

// TestEveryWorkflowThatInstallsTheCLIReadsTheOnePinFile pins the two workflows
// that install the Claude Code CLI: each reads .github/claude-code.yaml before
// its install step, so the version and the integrity it passes are the file's,
// and neither pins a version or an integrity of its own. The reviewer's
// workflow is required; the audit's, which PRD #175 milestone 2 adds in a later
// pull request, is checked once it is there.
func TestEveryWorkflowThatInstallsTheCLIReadsTheOnePinFile(t *testing.T) {
	t.Parallel()

	installs := claudeCodeInstallSteps(t)

	saw := map[string]bool{}

	for _, install := range installs {
		saw[install.workflow] = true

		// The install step's env: names both values the step that read the pin
		// file wrote, each exactly as that step's output (issue #277, round L,
		// AI 5): a step that set one alone would hand the installer an empty
		// version or an empty integrity, and the CLI would install unverified
		// or not at all.
		for name, want := range claudeCodePinOutputs {
			value, ok := install.pinNames[name]
			if !ok {
				t.Errorf("%s: line %d: the step that installs the CLI sets no %s; its env: must name both of the values the step that reads %s wrote, %q, or the installer runs with an empty version or an empty integrity (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, name, claudeCodePinFile, want)

				continue
			}

			if value != want {
				t.Errorf("%s: line %d: the step that installs the CLI sets %s to %q, want %q: the version and the integrity reach the installer as the output of the step that reads %s, never as a value of the workflow's own (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, name, value, want, claudeCodePinFile)
			}
		}

		for _, name := range claudeCodePinEnvNames {
			if value, ok := install.pinNames[name]; ok {
				t.Errorf("%s: line %d: the step that installs the CLI sets %s to %q; the version and the integrity live in %s alone, which Renovate bumps and ci/claudecodepin reads, and reach the installer as steps.%s.outputs.* (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, name, value, claudeCodePinFile, claudeCodePinStepName)
			}
		}

		for _, problem := range claudeCodePinStepProblems(t, install) {
			t.Errorf("%s: line %d: %s (ADR 0040, issue #277, round K)", install.workflow, install.num, problem)
		}

		// No word of the install step's line is a pin written into the workflow:
		// the package reaches the installer as
		// @anthropic-ai/claude-code-<platform>@<version> and the integrity as
		// itself, both from the value the step read out of the file.
		for _, word := range aiReviewShellWords(install.run) {
			switch {
			case claudeCodeWorkflowIntegrityLit.MatchString(word):
				t.Errorf("%s: line %d: the install step passes %q, an integrity written into the workflow; the integrity reaches it from %s, so Renovate's bump of the version and the integrity it needs stay in the one file (ADR 0033, CLAUDE.md rule 7, issue #277)", install.workflow, install.num, word, claudeCodePinFile)
			case !strings.Contains(word, "$") && claudeCodeWorkflowVersionLit.MatchString(word):
				t.Errorf("%s: line %d: the install step passes %q, a version written into the workflow; the version reaches it from %s, so Renovate bumps the one text both workflows read (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, word, claudeCodePinFile)
			}
		}
	}

	if !saw[aiReviewWorkflowPath] {
		t.Errorf("no step of %s runs %s, so the reviewer installs no CLI at all (ADR 0033)", aiReviewWorkflowPath, claudeCodeInstallScript)
	}

	if _, err := os.Stat(filepath.Join(moduleRoot(t), claudeCodeAuditWorkflowPath)); err != nil {
		t.Logf("%s is not in this tree yet: PRD #175 milestone 2 adds the audit's model job, and this test holds it to the one pin once it is there", claudeCodeAuditWorkflowPath)

		return
	}

	if !saw[claudeCodeAuditWorkflowPath] {
		t.Errorf("no step of %s runs %s, so the audit installs no CLI: its model job runs the same pinned CLI as the reviewer (ADR 0040)", claudeCodeAuditWorkflowPath, claudeCodeInstallScript)
	}
}

// TestClaudeCodeReadsThePinOnTheLineThatNamesIt pins the reader half of the pin
// check on its own (issue #277, round L, AI 6): a step reads the pin file when
// one line that is no comment greps one of the file's anchored lines and names
// the file on that same line, as its own path or through a variable the same
// script assigns the path to. A step that echoes the path and greps another file
// reads no pin, so the grep and the path are one line's business.
func TestClaudeCodeReadsThePinOnTheLineThatNamesIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  string
		want bool
	}{
		{
			// The reviewer's own step: the pin's path is a variable the step
			// assigns, and each grep reads the file through it.
			name: "the reviewer's step",
			run: strings.Join([]string{
				`pin=` + claudeCodePinFile,
				`version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' "${pin}" || true)"`,
				`integrity="$(grep -E '^integrity: sha512-[A-Za-z0-9+/]{86}==$' "${pin}" || true)"`,
			}, "\n"),
			want: true,
		},
		{
			name: "the pin file's path on the grep's own line",
			run:  `version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' ` + claudeCodePinFile + `)"`,
			want: true,
		},
		{
			// Round J's bypass in its round L form: the path is named, but by
			// another line, and the grep reads another file.
			name: "an echo of the path beside a grep of another file",
			run: strings.Join([]string{
				`echo "reading ` + claudeCodePinFile + `"`,
				`version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' .github/other.yaml || true)"`,
			}, "\n"),
			want: false,
		},
		{
			name: "a comment naming the path beside a grep of another file",
			run: strings.Join([]string{
				`# ` + claudeCodePinFile + ` holds the version`,
				`version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' .github/other.yaml || true)"`,
			}, "\n"),
			want: false,
		},
		{
			// The variable is another step's, or a name no line of this step
			// assigns: nothing of this step names the file.
			name: "a grep of a variable the step never assigns",
			run:  `version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' "${pin}" || true)"`,
			want: false,
		},
		{
			// Round M, audit nit 1: the path is on the line, but before the
			// grep, where no argument of it comes, and the grep reads another
			// file.
			name: "an echo of the path beside a grep of another file on one line",
			run:  `echo ` + claudeCodePinFile + `; version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' .github/other.yaml || true)"`,
			want: false,
		},
		{
			name: "a variable assigned another file",
			run: strings.Join([]string{
				`pin=.github/other.yaml`,
				`version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' "${pin}" || true)"`,
			}, "\n"),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := claudeCodeReadsPin(tc.run); got != tc.want {
				t.Errorf("claudeCodeReadsPin(%q) = %t, want %t: a step reads %s when one line greps it and names it on that same line (ADR 0040's milestone-2 amendment, issue #277)", tc.run, got, tc.want, claudeCodePinFile)
			}
		})
	}
}

// TestClaudeCodeWritesEachOutputWhereItsRedirectIs pins the writer half of the
// pin check on its own (issue #277, round M, AI 7): a step writes an output when
// the same line it prints the output line on, or the { ...; } block that line
// stands in, is the one that carries the redirect to $GITHUB_OUTPUT. A version
// printed to the step's own log with only the integrity going to the output
// writes no version, and a value that stays in a shell variable writes nothing
// at all.
func TestClaudeCodeWritesEachOutputWhereItsRedirectIs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  string
		// output is the name the row is about, and want whether the script
		// writes that output where GitHub reads it.
		output string
		want   bool
	}{
		{
			// The reviewer's own step: one block whose closing line carries
			// both outputs' redirect.
			name: "the reviewer's block",
			run: strings.Join([]string{
				`version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' "${pin}" || true)"`,
				`integrity="$(grep -E '^integrity: sha512-[A-Za-z0-9+/]{86}==$' "${pin}" || true)"`,
				`{`,
				`  printf 'version=%s\n' "${version#version: }"`,
				`  printf 'integrity=%s\n' "${integrity#integrity: }"`,
				`} >> "${GITHUB_OUTPUT}"`,
			}, "\n"),
			output: "version",
			want:   true,
		},
		{
			name:   "an output line and the redirect on one line",
			run:    `printf 'integrity=%s\n' "${integrity}" >> "${GITHUB_OUTPUT}"`,
			output: "integrity",
			want:   true,
		},
		{
			name:   "an output line in a comment",
			run:    "# printf 'integrity=%s\\n' \"${integrity}\"\n" + `printf 'version=%s\n' "${version}" >> "${GITHUB_OUTPUT}"`,
			output: "integrity",
			want:   false,
		},
		{
			// The version goes to the step's own log, and the block's
			// redirect carries the integrity alone (round M, AI 7).
			name: "the version printed to the log, only the integrity redirected",
			run: strings.Join([]string{
				`printf 'version=%s\n' "${version}"`,
				`{`,
				`  printf 'integrity=%s\n' "${integrity}"`,
				`} >> "${GITHUB_OUTPUT}"`,
			}, "\n"),
			output: "version",
			want:   false,
		},
		{
			name:   "a value that stays in a shell variable",
			run:    `version="$(grep -E '^version: [0-9]+\.[0-9]+\.[0-9]+$' "${pin}" || true)"`,
			output: "version",
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := claudeCodeWritesOutput(tc.run, tc.output); got != tc.want {
				t.Errorf("claudeCodeWritesOutput(%q, %q) = %t, want %t: a step writes an output where the same line, or the same block, prints it and redirects it to $GITHUB_OUTPUT (issue #277, round M, AI 7)", tc.run, tc.output, got, tc.want)
			}
		})
	}
}

// TestClaudeCodeRunsInstallerSeesEveryForm pins what counts as the install step
// (issue #277, round L, AI 6): the installer's own line, the line that reaches it
// through bash or sh, the line that reaches it through env and that command's own
// options and NAME=VALUE words, the same script named with a ./ before it, and
// the command an interpreter's -c takes as one quoted argument (issue #277,
// round M, AI 7). A step that only names the path where no command runs - a case
// pattern of a diff, an echo, a comment - installs nothing. A case block is read
// as the shell reads it (issue #277, round N, AI 1): an arm's pattern list ends at
// its first ), the commands after it on the same line run, and the esac ends the
// block, so a command after it is seen again.
func TestClaudeCodeRunsInstallerSeesEveryForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  string
		want bool
	}{
		{
			name: "the reviewer's own line",
			run:  claudeCodeInstallScript + ` "${RUNNER_TEMP}/claude-code" "@anthropic-ai/claude-code-linux-x64@${CLAUDE_CODE_PIN_VERSION}" "${CLAUDE_CODE_PIN_INTEGRITY}"`,
			want: true,
		},
		{
			name: "through bash",
			run:  `bash ` + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			want: true,
		},
		{
			name: "through sh",
			run:  `sh ` + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			want: true,
		},
		{
			name: "through env",
			run:  `env ` + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			want: true,
		},
		{
			name: "through env, an option and an assignment before bash",
			run:  `env -i TMPDIR=/tmp bash ` + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			want: true,
		},
		{
			// Round M, AI 7: a command whose own word comes first still reaches
			// the script, so the check reads every word of the line.
			name: "through sudo",
			run:  `sudo ` + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			want: true,
		},
		{
			// Round M, AI 7: the same script, named with the ./ a shell
			// accepts.
			name: "with ./ before the script's path",
			run:  "./" + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			want: true,
		},
		{
			// Round M, AI 7: an interpreter's -c takes the whole command as
			// one quoted argument, which holds the script's path all the same.
			name: "through bash -c",
			run:  `bash -c '` + claudeCodeInstallScript + ` "$dir" "pkg" "integrity"'`,
			want: true,
		},
		{
			// claude-code-pin.yml's changes step names the script in a case
			// pattern of its diff, and installs nothing.
			name: "a case pattern of a diff",
			run:  "case \"$path\" in\n  " + claudeCodeInstallScript + ") echo changed ;;\nesac",
			want: false,
		},
		{
			// Round N, AI 1: the arm's pattern ends at its first ), and what
			// the arm runs comes after it. The pattern itself installs
			// nothing, and the command beside it does.
			name: "an install in a case arm",
			run: strings.Join([]string{
				`case "$path" in`,
				"  .github/claude-code.yaml) " + claudeCodeInstallScript + ` "$dir" "pkg" "integrity" ;;`,
				`esac`,
			}, "\n"),
			want: true,
		},
		{
			// Round N, AI 1: the block ends at its esac, and a step's command
			// after it is a command again. Before, one arm's pattern list
			// swallowed every line that followed it.
			name: "an install after a case block",
			run: strings.Join([]string{
				`case "$path" in`,
				"  " + claudeCodeInstallScript + `) echo changed ;;`,
				`esac`,
				claudeCodeInstallScript + ` "$dir" "pkg" "integrity"`,
			}, "\n"),
			want: true,
		},
		{
			// Round N, AI 1: claude-code-pin.yml's own pattern list runs over
			// two lines and names the script in the first of them. The list is
			// compared and never run, so it installs nothing, and neither the
			// arm's body nor the step's line after the esac does.
			name: "the pin workflow's pattern list over two lines",
			run: strings.Join([]string{
				`case "$path" in`,
				`  .github/claude-code.yaml | .github/workflows/ai-review.yml | ` + claudeCodeInstallScript + ` | \`,
				`    .github/workflows/claude-code-pin.yml | go.mod | ci/claudecodepin/*)`,
				`    echo "This pull request changes a path of the Claude Code pin: checking the pin."`,
				`    ;;`,
				`esac`,
				`echo "pin=false" >> "$GITHUB_OUTPUT"`,
			}, "\n"),
			want: false,
		},
		{
			name: "an echo of the path",
			run:  `echo "run ` + claudeCodeInstallScript + ` yourself"`,
			want: false,
		},
		{
			name: "a comment naming the path",
			run:  `# ` + claudeCodeInstallScript + ` installs the CLI`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := claudeCodeRunsInstaller(tc.run); got != tc.want {
				t.Errorf("claudeCodeRunsInstaller(%q) = %t, want %t: a step runs "+claudeCodeInstallScript+" where a command's own name comes (ADR 0040's milestone-2 amendment, issue #277)", tc.run, got, tc.want)
			}
		})
	}
}

// claudeCodeInstallSteps returns every step of every workflow of .github that
// runs the installer, with the env: names it sets for the pin.
func claudeCodeInstallSteps(t *testing.T) []claudeCodeInstallStep {
	t.Helper()

	root := moduleRoot(t)

	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("list the workflows of .github/workflows: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("found no workflow under .github/workflows, want the repository's workflows")
	}

	var installs []claudeCodeInstallStep

	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("resolve %s: %v", path, err)
		}

		rel = filepath.ToSlash(rel)

		for _, job := range claudeCodeWorkflowJobs(t, rel) {
			for _, step := range job.steps {
				fields := aiReviewStepFields(t, step)

				run, ok := aiReviewFind(fields, "run")
				if !ok {
					continue
				}

				text := aiReviewText(run)
				if !claudeCodeRunsInstaller(text) {
					continue
				}

				pinNames := map[string]string{}

				if env, ok := aiReviewFind(fields, "env"); ok {
					for _, line := range env.Block {
						key, value, ok := strings.Cut(line.Text, ":")
						if !ok {
							continue
						}

						name := strings.TrimSpace(key)

						if _, ok := claudeCodePinOutputs[name]; ok {
							pinNames[name] = strings.TrimSpace(value)

							continue
						}

						if slices.Contains(claudeCodePinEnvNames, name) {
							pinNames[name] = strings.TrimSpace(value)
						}
					}
				}

				installs = append(installs, claudeCodeInstallStep{workflow: rel, job: job.key, num: run.Num, run: text, pinNames: pinNames})
			}
		}
	}

	return installs
}

// claudeCodeRunsInstaller reports whether a step's run: script runs the
// installer: one line that is no comment holds a word whose cleaned path ends in
// the script's name (issue #277, rounds L and M, AI 6 and 7). The check fails
// closed: the word may stand anywhere on the line, after sudo or an
// interpreter's -c, with a ./ before it or with the shell's own quoting around
// it. A comment installs nothing, and neither does a case pattern of a diff: the
// patterns a case's in lists, which the shell compares and never runs, are no
// command. A block is read as the shell reads it (round N, AI 1): an arm's
// pattern list ends at its first ), the commands after it on the same line run,
// and the esac ends the block, so a line after it is no pattern any more.
func claudeCodeRunsInstaller(text string) bool {
	inCase := false
	patterns := false

	for _, line := range claudeCodeNonCommentLines(text) {
		trimmed := strings.TrimSpace(line)

		// A case block ends at its esac, which closes a pattern list too: a
		// case may hold no arm at all.
		if trimmed == "esac" {
			inCase, patterns = false, false

			continue
		}

		if patterns {
			// An arm's pattern list ends at its first ): a line of the list
			// before it is compared and never run, and what comes after it on
			// the same line is the arm's command.
			end := strings.Index(line, ")")
			if end < 0 {
				continue
			}

			patterns = false

			line = line[end+1:]
			trimmed = strings.TrimSpace(line)
		} else if claudeCodeOpensCasePatterns(aiReviewShellWords(line)) {
			inCase, patterns = true, true

			continue
		}

		if claudeCodeRunsInstallerLine(aiReviewShellWords(line)) {
			return true
		}

		// An arm's body ends at its ;;, and the next arm's pattern list starts
		// after it.
		if inCase && strings.HasSuffix(trimmed, ";;") {
			patterns = true
		}
	}

	return false
}

// claudeCodeOpensCasePatterns reports whether words open a case's list of
// patterns: the shell's case keyword and the in that ends the word it selects
// on.
func claudeCodeOpensCasePatterns(words []string) bool {
	if len(words) < 2 || words[0] != "case" {
		return false
	}

	return slices.Contains(words[1:], "in")
}

// claudeCodeRunsInstallerLine reports whether one line's words run the
// installer: any word whose cleaned path ends in the script's name, wherever it
// stands on the line, so a line that reaches the script through sudo, an
// interpreter's -c option or a ./ prefix is read rather than skipped over (issue
// #277, round M, AI 7). The check fails closed: a word the cleaning leaves
// ending in the script's name counts, and the cleaning takes off only what a
// shell's own quoting adds, so a case pattern's word, which ends in ")", and an
// echo's quoted sentence, which is no path at all, install nothing.
func claudeCodeRunsInstallerLine(words []string) bool {
	for _, word := range words {
		if claudeCodeIsInstallerWord(word) {
			return true
		}
	}

	return false
}

// claudeCodeIsInstallerWord reports whether word names the installer's path: a
// leading quote character the shell words keep and a leading ./ taken off, it
// ends in the script's path.
func claudeCodeIsInstallerWord(word string) bool {
	word = strings.TrimLeft(word, `'"`)
	word = strings.TrimPrefix(word, "./")

	return strings.HasSuffix(word, claudeCodeInstallScript)
}

// claudeCodeReadsPin reports whether a step's run: script reads the pin file:
// one line that is no comment runs grep -E over one of the file's anchored
// lines and names the file among that grep's own arguments, as its own path or
// through a shell variable the same script assigns the path to (issue #277,
// rounds J, K, L and M). A comment reads nothing, and neither does a word
// before the grep or one of a command that follows it: a path an echo prints
// beside a grep of another file reads no pin.
func claudeCodeReadsPin(text string) bool {
	lines := claudeCodeNonCommentLines(text)
	variables := claudeCodePinVariables(lines)

	for _, line := range lines {
		match := claudeCodePinRead.FindStringIndex(line)
		if match == nil {
			continue
		}

		arguments := claudeCodeGrepArguments(line[match[1]:])

		if strings.Contains(arguments, claudeCodePinFile) {
			return true
		}

		for _, name := range variables {
			if claudeCodeNamesVariable(arguments, name) {
				return true
			}
		}
	}

	return false
}

// claudeCodeGrepArguments returns what a run: line holds of the grep -E
// command from the end of its matched start to the separator that ends it: the
// ;, | or && that begins the next command.
func claudeCodeGrepArguments(rest string) string {
	if i := strings.IndexAny(rest, ";|&"); i >= 0 {
		return rest[:i]
	}

	return rest
}

// claudeCodeNonCommentLines returns the lines of a run: script that are no
// comment, a comment being a line whose first non-blank byte is #.
func claudeCodeNonCommentLines(text string) []string {
	var lines []string

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		lines = append(lines, line)
	}

	return lines
}

// claudeCodePinVariables returns the names a run: script assigns the pin file's
// path to, in the form name=<the file>, an export before it taken as the
// assignment's own keyword: the variables of that script a grep line may name
// the file through.
func claudeCodePinVariables(lines []string) []string {
	var names []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

		name, value, ok := strings.Cut(line, "=")
		if !ok || !claudeCodeIsShellName(name) || !strings.Contains(value, claudeCodePinFile) {
			continue
		}

		names = append(names, name)
	}

	return names
}

// claudeCodeIsShellName reports whether name reads as a shell variable's name:
// 1 to 64 bytes of ASCII letters, digits and '_', not starting with a digit.
func claudeCodeIsShellName(name string) bool {
	if name == "" || len(name) > 64 || (name[0] >= '0' && name[0] <= '9') {
		return false
	}

	for i := range len(name) {
		if !claudeCodeIsShellNameByte(name[i]) {
			return false
		}
	}

	return true
}

// claudeCodeIsShellNameByte reports whether b may be part of a shell variable's
// name.
func claudeCodeIsShellNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// claudeCodeNamesVariable reports whether line reads the variable name, as $name
// or ${name}, with a byte after it that is no part of a name: $pin is no read of
// a variable named pinpath.
func claudeCodeNamesVariable(line, name string) bool {
	for _, form := range []string{"${" + name + "}", "$" + name} {
		for rest := line; ; {
			i := strings.Index(rest, form)
			if i < 0 {
				break
			}

			rest = rest[i+len(form):]
			if rest == "" || !claudeCodeIsShellNameByte(rest[0]) {
				return true
			}
		}
	}

	return false
}

// claudeCodeWritesOutput reports whether a step's run: script writes name's
// value to $GITHUB_OUTPUT: one line that is no comment holds a quoted
// name= output line (printf 'version=%s', echo "version=...") and that same
// line, or the { ...; } block the line stands in, carries the redirect, which
// is the block's own closing line (issue #277, rounds K and M, AI 7). A comment
// writes nothing. A value the script prints to its own log, or keeps in a shell
// variable, while the redirect carries another output is no output of the step:
// the write and the redirect are one line's, or one block's, business.
func claudeCodeWritesOutput(run, name string) bool {
	inBlock, blockWrites := false, false

	for _, line := range strings.Split(run, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		switch {
		case !inBlock && trimmed == "{":
			inBlock, blockWrites = true, false

			continue
		case inBlock && strings.HasPrefix(trimmed, "}"):
			if blockWrites && strings.Contains(trimmed, "GITHUB_OUTPUT") {
				return true
			}

			inBlock, blockWrites = false, false

			continue
		}

		if !claudeCodeNamesOutput(trimmed, name) {
			continue
		}

		if strings.Contains(trimmed, "GITHUB_OUTPUT") {
			return true
		}

		if inBlock {
			blockWrites = true
		}
	}

	return false
}

// claudeCodeNamesOutput reports whether line holds an output line for name: the
// name and its = inside a quote, as printf and echo print one. An assignment's
// own name= is outside any quote, so no line that only computes the value names
// an output.
func claudeCodeNamesOutput(line, name string) bool {
	return strings.Contains(line, "'"+name+"=") || strings.Contains(line, "\""+name+"=")
}

// claudeCodePinStepProblems returns every reason the install step's own job
// holds no step that reads the pin file into the outputs the install step's
// env: names: the step with id: claude-code-pin must be in that job, before the
// install step, and its run: must read the file with grep -E outside a comment
// and write both the version and the integrity to $GITHUB_OUTPUT (issue #277,
// round K). A step of another job is no such step: it runs on another runner,
// and nothing it wrote reaches the installer. An empty list is such a step.
func claudeCodePinStepProblems(t *testing.T, install claudeCodeInstallStep) []string {
	t.Helper()

	var problems []string

	found := false

	for _, job := range claudeCodeWorkflowJobs(t, install.workflow) {
		if job.key != install.job {
			continue
		}

		for _, step := range job.steps {
			fields := aiReviewStepFields(t, step)

			id, ok := aiReviewFind(fields, "id")
			if !ok || aiReviewScalar(t, id) != claudeCodePinStepName {
				continue
			}

			found = true

			run, ok := aiReviewFind(fields, "run")
			if !ok {
				problems = append(problems, fmt.Sprintf("the step with id: %s has no run: that reads %s", claudeCodePinStepName, claudeCodePinFile))

				continue
			}

			if run.Num > install.num {
				problems = append(problems, fmt.Sprintf("the step with id: %s is at line %d, after the install step at line %d: the install step's env: reads what it wrote, so it must come first", claudeCodePinStepName, run.Num, install.num))
			}

			problems = append(problems, claudeCodePinRunProblems(aiReviewText(run))...)
		}
	}

	if !found {
		problems = append(problems, fmt.Sprintf("no step of the install step's job has id: %s, the one step whose outputs the install step's env: reads", claudeCodePinStepName))
	}

	return problems
}

// claudeCodePinRunProblems returns every reason run is no script that reads the
// pin file into the step's outputs: one line that is no comment must grep one of
// the file's anchored lines and name the file there, as its own path or through
// a variable the step assigns the path to, and both the version and the
// integrity must be written to $GITHUB_OUTPUT (issue #277, rounds K and L).
func claudeCodePinRunProblems(run string) []string {
	var problems []string

	if !claudeCodeReadsPin(run) {
		problems = append(problems, fmt.Sprintf("its run: script reads %s with no line that is no comment grepping one of its anchored lines and naming the file on that same line, as its own path or through a variable the step assigns the path to", claudeCodePinFile))
	}

	for _, output := range claudeCodePinStepOutputs {
		if !claudeCodeWritesOutput(run, output) {
			problems = append(problems, fmt.Sprintf("its run: script writes no %s to $GITHUB_OUTPUT", output))
		}
	}

	return problems
}

// claudeCodeWorkflowJobs returns every job of the workflow at rel with its steps,
// rel a slash-separated path from the module root.
func claudeCodeWorkflowJobs(t *testing.T, rel string) []claudeCodeWorkflowJob {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	lines, err := yamlread.SplitLines(src)
	if err != nil {
		t.Fatalf("%s: read the workflow: %v", rel, err)
	}

	jobs, ok := aiReviewEntry(t, lines, "jobs")
	if !ok {
		t.Fatalf("%s: the workflow has no jobs:", rel)
	}

	jobFields, err := yamlread.Mapping(jobs.Block)
	if err != nil {
		t.Fatalf("%s: read jobs:: %v", rel, err)
	}

	workflowJobs := make([]claudeCodeWorkflowJob, 0, len(jobFields))

	for _, job := range jobFields {
		stepsEntry, ok := aiReviewFind(aiReviewFields(t, job.Block, "job "+job.Key), "steps")
		if !ok {
			continue
		}

		items, err := yamlread.Sequence(stepsEntry.Block)
		if err != nil {
			t.Fatalf("%s: job %s: read steps:: %v", rel, job.Key, err)
		}

		workflowJobs = append(workflowJobs, claudeCodeWorkflowJob{key: job.Key, steps: items})
	}

	return workflowJobs
}
