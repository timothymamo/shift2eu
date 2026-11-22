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
	"os"
	"path/filepath"
	"regexp"
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
)

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
	// values it sets them to.
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
			t.Errorf("%s: line %d: the step that installs the CLI sets %s to %q; the version and the integrity live in %s alone, which Renovate bumps and ci/claudecodepin reads (CLAUDE.md rule 7, issue #277)", install.workflow, install.num, name, value, claudeCodePinFile)
		}

		if !strings.Contains(install.run, claudeCodePinFile) && !claudeCodeStepBeforeReadsPin(t, install) {
			t.Errorf("%s: line %d: neither the install step nor one before it in its job reads %s, so neither the version nor the integrity reaches the installer (ADR 0040, issue #277)", install.workflow, install.num, claudeCodePinFile)
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

						switch strings.TrimSpace(key) {
						case "CLAUDE_CODE_VERSION", "CLAUDE_CODE_INTEGRITY":
							pinNames[strings.TrimSpace(key)] = strings.TrimSpace(value)
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

// claudeCodeStepBeforeReadsPin reports whether a step of the install step's own
// job, at or before it, reads the pin file: the one step that reads the file's
// shape exports the version and the integrity, and the install step passes what
// it exported. A step of another job of the same workflow is no such step: it
// runs on another runner, and nothing it read reaches the installer (issue #277,
// round F finding 7).
func claudeCodeStepBeforeReadsPin(t *testing.T, install claudeCodeInstallStep) bool {
	t.Helper()

	for _, job := range claudeCodeWorkflowJobs(t, install.workflow) {
		if job.key != install.job {
			continue
		}

		for _, step := range job.steps {
			run, ok := aiReviewFind(aiReviewStepFields(t, step), "run")
			if !ok || run.Num > install.num {
				continue
			}

			if strings.Contains(aiReviewText(run), claudeCodePinFile) {
				return true
			}
		}
	}

	return false
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
