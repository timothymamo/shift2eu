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

		for name, value := range install.pinNames {
			if want, ok := claudeCodePinOutputs[name]; ok {
				if value != want {
					t.Errorf("%s: line %d: the step that installs the CLI sets %s to %q, want %q: the version and the integrity reach the installer as the output of the step that reads %s, never as a value of the workflow's own (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, name, value, want, claudeCodePinFile)
				}

				continue
			}

			t.Errorf("%s: line %d: the step that installs the CLI sets %s to %q; the version and the integrity live in %s alone, which Renovate bumps and ci/claudecodepin reads, and reach the installer as steps.%s.outputs.* (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, name, value, claudeCodePinFile, claudeCodePinStepName)
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
// installer: one of its lines starts with the script's path. A step that only
// names the path, in a case pattern of a diff or in a comment, installs nothing.
func claudeCodeRunsInstaller(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		words := aiReviewShellWords(line)

		if len(words) > 0 && words[0] == claudeCodeInstallScript {
			return true
		}
	}

	return false
}

// claudeCodeReadsPin reports whether a step's run: script reads the pin file:
// one line that is no comment runs grep -E over one of the file's anchored
// lines, and one line that is no comment names the file. A comment reads
// nothing, and neither does a line that only mentions the path, in an echo or
// any other word, so a step that only names the file is no step that read it
// (issue #277, rounds J and K).
func claudeCodeReadsPin(text string) bool {
	grepped, named := false, false

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		if claudeCodePinRead.MatchString(line) {
			grepped = true
		}

		if strings.Contains(line, claudeCodePinFile) {
			named = true
		}
	}

	return grepped && named
}

// claudeCodeWritesOutput reports whether a step's run: script writes name's
// value to $GITHUB_OUTPUT: one line that is no comment writes it as a quoted
// output line (printf 'version=%s', echo "version=..."), and one line that is
// no comment appends to $GITHUB_OUTPUT. A comment writes nothing, and neither
// does a script that only computes the value into a shell variable and writes
// it nowhere: the assignment's own name= is no output line (issue #277, round
// K).
func claudeCodeWritesOutput(run, name string) bool {
	writes, output := false, false

	for _, line := range strings.Split(run, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		if strings.Contains(line, "'"+name+"=") || strings.Contains(line, "\""+name+"=") {
			writes = true
		}

		if strings.Contains(line, "GITHUB_OUTPUT") {
			output = true
		}
	}

	return writes && output
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
// pin file into the step's outputs: it must read the file with grep -E outside
// a comment, and write both the version and the integrity to $GITHUB_OUTPUT
// (issue #277, round K).
func claudeCodePinRunProblems(run string) []string {
	var problems []string

	if !claudeCodeReadsPin(run) {
		problems = append(problems, fmt.Sprintf("its run: script reads %s with neither an anchored grep -E outside a comment nor a line that is no comment naming it", claudeCodePinFile))
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
