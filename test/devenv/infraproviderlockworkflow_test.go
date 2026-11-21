// Pins the verify-provider-lock job of the "Infra tools" workflow (issue #434, the follow-up review of PR
// #433): the job regenerates infra/site/.terraform.lock.hcl with the pinned OpenTofu in a scratch copy of the
// root, then fails unless dev/infra-lock-compare.sh finds its hash set equal to the committed one. Every fact
// below is what makes that comparison worth its run:
//
//   - the scratch copy holds the whole root (cp -R infra/site/.), not a glob of it: a local module the root
//     loads would otherwise be missing, and tofu would lock a configuration the repository does not have;
//   - the copy holds neither the committed lock file nor .terraform before tofu runs, and the step refuses to
//     go on when the lock file is still there: a copied lock file would be compared with itself;
//   - tofu runs as dev/infra-check.sh runs it, under env -i with TF_CLI_CONFIG_FILE=/dev/null, so no CLI
//     config and no credential of the runner reach it;
//   - exactly the three platforms infra/site/README.md names (darwin_arm64, linux_amd64, linux_arm64) are
//     locked, so a dropped or an added -platform cannot shrink or widen what the comparison covers;
//   - dev/infra-lock-compare.sh runs after the lock, naming the committed file first and the scratch one
//     second, or a refusal would name the wrong file;
//   - the job holds no more than contents: read and its checkout keeps no credential, since the job reads a
//     provider's public keys and writes nothing back.
//
// The workflow is YAML, but the module's dependencies hold no YAML reader, so the test reads the workflow as
// the job writes it: the job's own lines, its checkout step and the step's run script, whose continuation
// lines it joins as the shell runs them. It reads files only: no tofu runs and nothing reaches the network.
package devenv_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	// providerLockWorkflowJob is the job that regenerates the provider lock file in a scratch copy of the
	// site's root and compares it with the committed one.
	providerLockWorkflowJob = "verify-provider-lock"
	// providerLockScratch is the shell variable of that job's step that names the scratch copy.
	providerLockScratch = `"${work}/"`
)

// TestInfraProviderLockWorkflowStep pins what the workflow's lock-file comparison must do to mean anything:
// the scratch copy of the whole root, the removals before tofu runs, the guard that refuses a copy still
// holding a lock file, the tofu call under env -i and its three platforms, the comparison after it, and the
// job's read-only permissions.
func TestInfraProviderLockWorkflowStep(t *testing.T) {
	t.Parallel()

	for _, problem := range providerLockWorkflowProblems(readRepoFile(t, tofuWorkflow)) {
		t.Error(problem)
	}
}

// providerLockWorkflowProblems returns one line per fact the workflow's verify-provider-lock job breaks, and
// nothing when every fact holds. It reads the text it is given and nothing else, so a mutated copy of the
// workflow can be checked the same way the real one is.
func providerLockWorkflowProblems(workflow string) []string {
	job := workflowJobLines(workflow, providerLockWorkflowJob)
	if job == nil {
		return []string{tofuWorkflow + " holds no " + providerLockWorkflowJob + " job, so nothing regenerates the provider lock file and compares its hash set"}
	}

	var problems []string

	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	permissions := workflowJobPermissions(job)
	if !strings.Contains(permissions, "contents: read") {
		fail("%s's %s job does not hold permissions: contents: read (its permissions block is %q): the job reads a provider's public keys and must write nothing back",
			tofuWorkflow, providerLockWorkflowJob, permissions)
	}

	if strings.Contains(permissions, ": write") {
		fail("%s's %s job grants a write permission (%q): it reads a provider's public keys and must write nothing back",
			tofuWorkflow, providerLockWorkflowJob, permissions)
	}

	if problem := workflowCheckoutProblem(job); problem != "" {
		fail("%s's %s job: %s", tofuWorkflow, providerLockWorkflowJob, problem)
	}

	script, ok := workflowStepScript(job)
	if !ok {
		fail("%s's %s job holds no run step, so it regenerates no lock file", tofuWorkflow, providerLockWorkflowJob)

		return problems
	}

	statements := shellStatements(script)

	// at is the index of the statement that holds every token of want, or -1.
	at := func(want ...string) int {
		for i, statement := range statements {
			holds := true

			for _, token := range want {
				if !strings.Contains(statement, token) {
					holds = false

					break
				}
			}

			if holds {
				return i
			}
		}

		return -1
	}

	// before reports a statement that runs after the one it must precede.
	before := func(what string, whatAt int, then string, thenAt int) {
		if whatAt >= 0 && thenAt >= 0 && whatAt > thenAt {
			fail("the step runs %s after %s: want it before", what, then)
		}
	}

	lockAt := at(tofuDownloadScript, "providers lock")
	if lockAt < 0 {
		fail("the step never runs `tofu providers lock` through %s, so the comparison has no regenerated hash set to compare", tofuDownloadScript)
	}

	copyAt := at("cp -R infra/site/.", providerLockScratch)
	if copyAt < 0 {
		fail("the step does not copy the whole root into the scratch directory: want a statement holding \"cp -R infra/site/.\" and %s, so every file the root loads - its *.tf, *.tofu and its local modules - is the one tofu locks",
			providerLockScratch)
	}

	commentedLockAt := at(`rm -f "${work}/.terraform.lock.hcl"`)
	if commentedLockAt < 0 {
		fail("the step does not remove the copied lock file (want a statement holding %q): the regenerated file would start as the committed one, and the comparison would compare it with itself",
			`rm -f "${work}/.terraform.lock.hcl"`)
	}

	dotTerraformAt := at(`rm -rf "${work}/.terraform"`)
	if dotTerraformAt < 0 {
		fail("the step does not remove the copied .terraform directory (want a statement holding %q): its providers would carry into the regenerated lock file",
			`rm -rf "${work}/.terraform"`)
	}

	guardAt, guardProblems := lockGuardProblems(statements)
	for _, problem := range guardProblems {
		fail("%s", problem)
	}

	before("the lock file's removal", commentedLockAt, "the guard that refuses a lock file still there", guardAt)
	before("the .terraform directory's removal", dotTerraformAt, "the guard that refuses a lock file still there", guardAt)
	before("the guard that refuses a lock file still there", guardAt, "`providers lock`", lockAt)
	before("the lock file's removal", commentedLockAt, "`providers lock`", lockAt)
	before("the .terraform directory's removal", dotTerraformAt, "`providers lock`", lockAt)

	if lockAt >= 0 {
		assertProviderLockCommand(fail, statements[lockAt])
	}

	compareAt := at("dev/infra-lock-compare.sh")
	if compareAt < 0 {
		fail("the step never runs dev/infra-lock-compare.sh, so the regenerated lock file is compared with nothing")
	} else {
		want := `dev/infra-lock-compare.sh infra/site/.terraform.lock.hcl "` + "${work}/.terraform.lock.hcl" + `"`
		if !strings.Contains(statements[compareAt], want) {
			fail("the step's comparison is %q: want %q, the committed file first and the scratch copy second, or the refusal names the wrong file",
				statements[compareAt], want)
		}

		before("`providers lock`", lockAt, "dev/infra-lock-compare.sh", compareAt)
	}

	return problems
}

// lockGuardProblems returns where the statements check the scratch copy for a lock file before tofu locks
// anything, and one line per fact that check breaks: no such check, one that does not close, or one that does
// not stop the step.
func lockGuardProblems(statements []string) (int, []string) {
	const want = `if [ -e "${work}/.terraform.lock.hcl" ]`

	guardAt := -1

	for i, statement := range statements {
		if strings.HasPrefix(statement, want) {
			guardAt = i

			break
		}
	}

	if guardAt < 0 {
		return -1, []string{fmt.Sprintf("the step never refuses a scratch copy that still holds a lock file (want a statement starting %q): the comparison would compare the committed file with itself", want)}
	}

	closedAt := -1

	for i := guardAt + 1; i < len(statements); i++ {
		if statements[i] == "fi" {
			closedAt = i

			break
		}
	}

	if closedAt < 0 {
		return guardAt, []string{"the step's check for a lock file still in the scratch copy never closes with fi, so the step's steps after it are the guard's body"}
	}

	exits := slices.ContainsFunc(statements[guardAt+1:closedAt], func(statement string) bool {
		return regexp.MustCompile(`^exit [1-9][0-9]*$`).MatchString(statement)
	})
	if !exits {
		return guardAt, []string{"the step's check for a lock file still in the scratch copy does not exit non-zero, so tofu locks a copy that already holds the committed file"}
	}

	return guardAt, nil
}

// assertProviderLockCommand pins the tofu call itself: tofu through dev/tofu.sh under env -i with no CLI
// config, and exactly the three platforms the site's README names.
func assertProviderLockCommand(fail func(string, ...any), command string) {
	if !strings.HasPrefix(command, "env -i ") {
		fail("the tofu command does not run under env -i (%q): the runner's environment, and any credential or CLI config in it, would reach tofu", command)
	}

	for _, token := range []string{tofuDownloadScript, "TF_CLI_CONFIG_FILE=/dev/null", "providers lock"} {
		if !strings.Contains(command, token) {
			fail("the tofu command holds no %q (%q): it must run %s with no CLI config, as dev/infra-check.sh runs it",
				token, command, tofuDownloadScript)
		}
	}

	want := []string{"darwin_arm64", "linux_amd64", "linux_arm64"}

	got := providerLockPlatforms(command)
	if !slices.Equal(got, want) {
		fail("the tofu command locks the platforms %v (%q), want exactly %v: the platforms infra/site/README.md names decide what the comparison covers",
			got, command, want)
	}
}

// providerLockPlatform matches one -platform flag of a tofu command and the platform it names, whether it is
// written as "-platform <name>" or as "-platform=<name>".
var providerLockPlatform = regexp.MustCompile(`-platform[= ]([a-z0-9_]+)`)

// providerLockPlatforms returns the platforms a tofu command locks, sorted, so a case compares them as a set.
func providerLockPlatforms(command string) []string {
	var platforms []string

	for _, match := range providerLockPlatform.FindAllStringSubmatch(command, -1) {
		platforms = append(platforms, match[1])
	}

	slices.Sort(platforms)

	return platforms
}

// workflowJobPermissions returns the text of the job's permissions block, or "" when the job holds none.
// Only the job's own block counts: the workflow's top-level permissions do not apply to one job alone.
func workflowJobPermissions(job []string) string {
	for i, line := range job {
		if strings.TrimSpace(line) != "permissions:" {
			continue
		}

		indent := workflowIndentation(line)

		var text []string

		for j := i + 1; j < len(job); j++ {
			if strings.TrimSpace(job[j]) == "" {
				continue
			}

			if workflowIndentation(job[j]) <= indent {
				break
			}

			text = append(text, strings.TrimSpace(job[j]))
		}

		return strings.Join(text, "\n")
	}

	return ""
}

// workflowCheckoutProblem reports the job's checkout step keeping a credential in the tree, or holding no
// checkout step at all, and "" when it keeps none.
func workflowCheckoutProblem(job []string) string {
	for i, line := range job {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- uses: actions/checkout@") {
			continue
		}

		indent := workflowIndentation(line)

		for j := i + 1; j < len(job); j++ {
			if strings.TrimSpace(job[j]) == "" {
				continue
			}

			if workflowIndentation(job[j]) <= indent {
				break
			}

			if strings.TrimSpace(job[j]) == "persist-credentials: false" {
				return ""
			}
		}

		return fmt.Sprintf("its checkout step (%q) does not set persist-credentials: false, so the job leaves the checkout's credential in the tree it runs tests in", trimmed)
	}

	return "it holds no actions/checkout step, so nothing pins that its checkout keeps no credential"
}

// workflowJobLines returns the lines of the named job of a workflow - its own indented block only, so a pin
// cannot hold on another job's line - and nil when the workflow holds no such job.
func workflowJobLines(workflow, job string) []string {
	lines := strings.Split(workflow, "\n")

	start := slices.Index(lines, "  "+job+":")
	if start < 0 {
		return nil
	}

	end := len(lines)

	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}

		if workflowIndentation(lines[i]) <= 2 {
			end = i

			break
		}
	}

	return lines[start:end]
}

// workflowStepScript returns the lines of the job's run scripts, their YAML block indentation removed, in the
// order the shell reads them, and whether the job holds one at all.
func workflowStepScript(job []string) ([]string, bool) {
	var script []string

	found := false

	for i, line := range job {
		if strings.TrimSpace(line) != "run: |" {
			continue
		}

		found = true

		runIndent := workflowIndentation(line)

		var body []string

		for j := i + 1; j < len(job); j++ {
			if strings.TrimSpace(job[j]) != "" && workflowIndentation(job[j]) <= runIndent {
				break
			}

			body = append(body, job[j])
		}

		script = append(script, dedentLines(body)...)
	}

	return script, found
}

// dedentLines removes the indentation the least indented non-blank line of lines holds, so a YAML block
// scalar's body is read as the shell reads it.
func dedentLines(lines []string) []string {
	cut := -1

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if indent := workflowIndentation(line); cut < 0 || indent < cut {
			cut = indent
		}
	}

	if cut <= 0 {
		return lines
	}

	dedented := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) >= cut {
			dedented = append(dedented, line[cut:])

			continue
		}

		dedented = append(dedented, line)
	}

	return dedented
}

// shellStatements joins a run script's lines into the statements the shell runs: a line ending in a backslash
// continues on the next one. Each statement's whitespace is collapsed to one space, so a pin reads tokens
// rather than the layout, and a comment line is dropped because it runs nothing.
func shellStatements(lines []string) []string {
	var (
		statements []string
		current    strings.Builder
	)

	flush := func() {
		if statement := strings.Join(strings.Fields(current.String()), " "); statement != "" {
			statements = append(statements, statement)
		}

		current.Reset()
	}

	for _, line := range lines {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "#") {
			continue
		}

		if continued, ok := strings.CutSuffix(text, "\\"); ok {
			current.WriteString(continued)
			current.WriteString(" ")

			continue
		}

		current.WriteString(text)
		flush()
	}

	flush()

	return statements
}

// workflowIndentation returns the number of spaces a workflow line starts with.
func workflowIndentation(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}
