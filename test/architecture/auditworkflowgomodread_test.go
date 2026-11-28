package architecture_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// This file runs the audit workflow's read of go.mod's Go, and the checks that
// follow it, as the checks job runs them (issue #710). A go list that fails in
// the install's prefix assignment does not end the step under set -euo pipefail:
// it leaves the install with GOTOOLCHAIN=go, and the install then fails with a
// confusing go error instead of a message of its own, while a read that gives two
// lines - or one line that is no Go version - builds govulncheck with a value go
// fetches no toolchain for. The fragment is extracted from the workflow and run
// with a fake go command, so the test pins what the step does and not a copy of
// it, and it never reaches the network.

// auditStepScript returns the lines of a step's run: | block, the shell script
// the step runs: the step's YAML keys and its env: entries are no part of it
// (issue #710).
func auditStepScript(t *testing.T, step auditJobStep) []string {
	t.Helper()

	for i, line := range step.lines {
		if strings.TrimSpace(line) != "run: |" {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		var script []string

		for _, rest := range step.lines[i+1:] {
			if strings.TrimSpace(rest) == "" {
				script = append(script, rest)

				continue
			}

			if len(rest)-len(strings.TrimLeft(rest, " \t")) <= indent {
				break
			}

			script = append(script, rest)
		}

		return script
	}

	t.Fatalf("%s's checks job, step %q, runs no run: | block: the step's shell script is what this test reads (issue #710)",
		auditWorkflow, step.name)

	return nil
}

// auditGoModReadFragment returns the step's script from the line that reads
// go.mod's Go to the line before the install that builds with it, which is the
// fragment this file runs (issue #710). It fails the test when the step reads
// go.mod on the install line alone, and when the fragment holds a line that is
// none of the exact shapes the step uses: the fragment runs in this test, so it
// may download and install nothing (issue #713).
func auditGoModReadFragment(t *testing.T, script []string) []string {
	t.Helper()

	fragment, problem := auditGoModReadFragmentOf(script)
	if problem != "" {
		t.Fatal(problem)
	}

	return fragment
}

// auditGoModReadFragmentOf returns the fragment auditGoModReadFragment runs, and
// the reason a step's script holds none this test may run, as the message that
// reason deserves. Every non-comment line of the fragment runs here, so every one
// of them - the read, the checks and their echoes, and the write of the checked
// value to $GITHUB_ENV - is held to the exact shapes the step uses, and nothing
// else, before this test runs it (issues #710 and #713).
func auditGoModReadFragmentOf(script []string) ([]string, string) {
	install := -1

	for i, line := range script {
		if strings.Contains(line, "go install") && strings.Contains(line, auditGovulncheckModule+"@") {
			install = i

			break
		}
	}

	if install < 0 {
		return nil, fmt.Sprintf("%s's checks job runs no go install of %s@: this test runs the read that feeds the install (issue #710)",
			auditWorkflow, auditGovulncheckModule)
	}

	read := -1

	for i := install - 1; i >= 0; i-- {
		if _, ok := auditGoModReadAssignment(script[i]); ok {
			read = i

			break
		}
	}

	if read < 0 {
		return nil, fmt.Sprintf("%s's install step reads go.mod's Go on no line of its own above the install: the line %q reads it inline, or not at all. Read go.mod's go line once into a variable there, with GOWORK=off so no go.work of the checkout answers instead, and check the value in an if block of its own - an ::error and an exit 1 - for no value, for more than one line and for anything that is no Go version, so the step fails with a message instead of a bare non-zero exit (issue #710)",
			auditWorkflow, strings.TrimSpace(script[install]))
	}

	fragment := script[read:install]

	for _, line := range fragment {
		trimmed, printable := auditStepLineOf(line)
		if !printable {
			return nil, auditPrintableASCIIProblemOf(
				fmt.Sprintf(`%s's install step holds %q between its read of go.mod's Go and the install`, auditWorkflow, trimmed),
				`the whole fragment - the read, the checks and their echoes, and the write of the checked value to $GITHUB_ENV`)
		}

		if auditIsComment(trimmed) {
			continue
		}

		if _, ok := auditLineShapeOf(auditFragmentLineShapes, trimmed); ok {
			continue
		}

		return nil, fmt.Sprintf(`%s's install step holds %q between its read of go.mod's Go and the install, and that line is no shape this test runs: this test runs the whole fragment - the read, the checks and their echoes, and the write of the checked value to $GITHUB_ENV - with only the system paths on PATH, so every line of it must match one of the exact shapes the step uses, in full, and nothing else. A list of words that fetch cannot bound a fragment this test runs: a command of another name, a tab where the list counted a space, or a substitution inside a message all reach the network without naming anything such a list holds (issues #710 and #713)`,
			auditWorkflow, trimmed)
	}

	return fragment, ""
}

// auditFragmentLineShape is one of the exact line shapes a step's script this file
// runs may hold: a name for the reader, and the expression every line of that shape
// matches in full (issue #713).
type auditFragmentLineShape struct {
	name    string
	pattern *regexp.Regexp
}

// auditFragmentLineShapes are the exact line shapes the fragment may hold: the
// read of go.mod's Go, the three checks that fail the run on what it read, their
// ::error, exit 1 and fi lines, and the write of the checked value to
// $GITHUB_ENV (issue #713). Every other line is refused: this test runs the
// fragment with only the system paths on PATH, and no list of words that fetch
// can bound the commands a line might run.
var auditFragmentLineShapes = []auditFragmentLineShape{
	{
		name:    "the read of go.mod's Go",
		pattern: regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*="\$\(GOWORK=off go list -m -f '\{\{\.GoVersion\}\}'\)" \|\| [A-Za-z_][A-Za-z0-9_]*=""$`),
	},
	{
		name:    "the check that the read gave a value",
		pattern: regexp.MustCompile(`^if \[ -z "\$\{[A-Za-z_][A-Za-z0-9_]*\}" \]; then$`),
	},
	{
		name:    "the check that the read gave one line",
		pattern: regexp.MustCompile(`^if \[ "\$\(printf '%s\\n' "\$\{[A-Za-z_][A-Za-z0-9_]*\}" \| wc -l\)" -ne 1 \]; then$`),
	},
	{
		name:    "the check that the read gave a Go version",
		pattern: regexp.MustCompile(`^if ! \[\[ "\$\{[A-Za-z_][A-Za-z0-9_]*\}" =~ \^1\\\.\[0-9\]\+\(\\\.\[0-9\]\+\)\?\$ \]\]; then$`),
	},
	{
		name:    "the ::error of a check",
		pattern: regexp.MustCompile(`^echo "::error title=[A-Za-z0-9 ]+::[^"$\x60\\]*"$`),
	},
	{
		name:    "the exit 1 of a check",
		pattern: regexp.MustCompile(`^exit 1$`),
	},
	{
		name:    "the fi that closes a check",
		pattern: regexp.MustCompile(`^fi$`),
	},
	{
		name:    "the write of the checked value to the job's environment",
		pattern: auditGoModGoEnvWrite,
	},
}

// auditLineShapeOf returns the shape of shapes trimmed matches, and false for a
// line none of them holds (issue #713).
func auditLineShapeOf(shapes []auditFragmentLineShape, trimmed string) (auditFragmentLineShape, bool) {
	for _, shape := range shapes {
		if shape.pattern.MatchString(trimmed) {
			return shape, true
		}
	}

	return auditFragmentLineShape{}, false
}

// auditIsComment reports whether a line, already trimmed as auditStepLineOf
// trims it, is no line of a step's script: empty, or a comment. The allow lists
// and the static pins read a step's lines through this one rule, so a line is a
// comment only where bash reads one - after its ASCII spaces and tabs, with no
// Unicode space or control byte leading it (issue #713).
func auditIsComment(trimmed string) bool {
	return trimmed == "" || strings.HasPrefix(trimmed, "#")
}

// auditPrintableASCIIProblemOf returns the reason this file refuses a line that
// holds a byte outside printable ASCII, given what holds the line (subject, which
// quotes it) and the whole the caller keeps to the step's own lines (whole)
// (issues #710 and #713).
func auditPrintableASCIIProblemOf(subject, whole string) string {
	return fmt.Sprintf(`%s, and that line holds a byte outside printable ASCII: bash treats no Unicode space, no vertical tab, no form feed and no carriage return as a blank, while strings.TrimSpace strips them all, so a line that begins with one of them and then "#" reads as a comment to a TrimSpace and runs as a command under bash. Every line this test runs must be printable ASCII, and a line is a comment only when it begins with "#" after its ASCII spaces and tabs, so %s stays what the step's own lines are (issues #710 and #713)`,
		subject, whole)
}

// auditModulesLineShapes are the exact line shapes the checks job's "Download the
// Go modules" step may hold (issue #713): the shell's strict-mode line, the guard
// that fails the run when the value the install step checked never reached the
// step, the line that takes that value, the two module downloads, and the three
// checks that hold every Go command after them to it, with their ::error, exit 1
// and fi lines. This file runs the whole step under make test, so every line of it
// must match one of these in full, and nothing else: a list of words that fetch
// cannot bound a script this test runs, as a curl line in the step proved.
var auditModulesLineShapes = []auditFragmentLineShape{
	{
		name:    "the shell's strict mode",
		pattern: regexp.MustCompile(`^set -euo pipefail$`),
	},
	{
		name:    "the guard that the install step's value reached the step",
		pattern: regexp.MustCompile(`^if \[ -z "\$\{gomod_go:-\}" \]; then$`),
	},
	{
		name:    "the line that takes the value from the job's environment",
		pattern: regexp.MustCompile(`^want="go\$\{gomod_go\}"$`),
	},
	{
		name:    "the download of the root module's modules",
		pattern: regexp.MustCompile(`^go mod download$`),
	},
	{
		name:    "the download of the tools module's modules",
		pattern: regexp.MustCompile(`^go -C tools mod download$`),
	},
	{
		name:    "the check that the go command is go.mod's Go",
		pattern: regexp.MustCompile(`^if \[ "\$\(go env GOVERSION\)" != "\$\{want\}" \]; then$`),
	},
	{
		name:    "the reset of the exit code a timed run records",
		pattern: regexp.MustCompile(`^rc=0$`),
	},
	{
		name:    "the timed read of govulncheck's version",
		pattern: regexp.MustCompile(`^timeout 1m govulncheck -version > "\$\{RUNNER_TEMP\}/govulncheck-version\.txt" 2>&1 \|\| rc=\$\?$`),
	},
	{
		name:    "the check that govulncheck -version names go.mod's Go",
		pattern: regexp.MustCompile(`^if \[ "\$\{rc\}" -ne 0 \] \|\| ! grep -qxF "Go: \$\{want\}" "\$\{RUNNER_TEMP\}/govulncheck-version\.txt"; then$`),
	},
	{
		name:    "the read of the built tool's Go",
		pattern: regexp.MustCompile(`^built="\$\(go version "\$\{RUNNER_TEMP\}/bin/govulncheck"\)"$`),
	},
	{
		name:    "the check that the tool was built with go.mod's Go",
		pattern: regexp.MustCompile(`^if \[ "\$\{built##\* \}" != "\$\{want\}" \]; then$`),
	},
	{
		name:    "the ::error of a check",
		pattern: regexp.MustCompile(`^echo "::error title=[A-Za-z0-9 ]+::([^"$\x60\\]|\$\{want\})*"$`),
	},
	{
		name:    "the exit 1 of a check",
		pattern: regexp.MustCompile(`^exit 1$`),
	},
	{
		name:    "the fi that closes a check",
		pattern: regexp.MustCompile(`^fi$`),
	},
}

// auditStepLineOf returns line trimmed of its leading and trailing ASCII spaces
// and tabs, which is how bash reads a blank, and false for a line that holds a
// byte outside printable ASCII (0x20 to 0x7e) anywhere inside it, a tab there
// included (issue #713). strings.TrimSpace is no trim for a line this test runs:
// it strips U+00A0, \v, \f, U+0085, U+2003 and a trailing \r, which bash treats
// as no blank, so " <U+00A0># || curl x" would read as a comment here and run as a
// command under bash.
func auditStepLineOf(line string) (string, bool) {
	trimmed := strings.Trim(line, " \t")

	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] < 0x20 || trimmed[i] > 0x7e {
			return trimmed, false
		}
	}

	return trimmed, true
}

// auditModulesStepScriptOf returns the script auditModulesStepScript runs, and the
// reason the step's script holds none this test may run, as the message that
// reason deserves: every non-comment line of it must match one of the exact shapes
// the step uses before this test runs any of them (issue #713).
func auditModulesStepScriptOf(script []string) ([]string, string) {
	for _, line := range script {
		trimmed, printable := auditStepLineOf(line)
		if !printable {
			return nil, auditPrintableASCIIProblemOf(
				fmt.Sprintf(`%s's checks job, step %q, holds %q`, auditWorkflow, auditGoModulesStepName, trimmed),
				`the whole step - the guard, the value it takes and the checks that hold every Go command to it`)
		}

		if auditIsComment(trimmed) {
			continue
		}

		if _, ok := auditLineShapeOf(auditModulesLineShapes, trimmed); ok {
			continue
		}

		return nil, fmt.Sprintf(`%s's checks job, step %q, holds %q, and that line is no shape this test runs: this test runs the whole step - the guard, the value it takes and the checks that hold every Go command to it - under set -euo pipefail, so every line of it must match one of the exact shapes the step uses, in full, and nothing else. A list of words that fetch cannot bound a script this test runs: a command of another name, a tab where the list counted a space, or a substitution inside a message all reach the network without naming anything such a list holds (issues #710 and #713)`,
			auditWorkflow, auditGoModulesStepName, trimmed)
	}

	return script, ""
}

// auditModulesStepScript returns the modules step's script, its every line held to
// the exact shapes the step uses before this file runs it (issue #713).
func auditModulesStepScript(t *testing.T, step auditJobStep) []string {
	t.Helper()

	script, problem := auditModulesStepScriptOf(auditStepScript(t, step))
	if problem != "" {
		t.Fatal(problem)
	}

	return script
}

// auditGoModGoEnvWrite matches the line that hands the install step's checked
// value to the job's later steps: echo "gomod_go=${gomod_go}" >> "$GITHUB_ENV"
// or its braces spelling, and neither a brace it never opened nor one it never
// closes, which is a path of another name that hands the job nothing (issue
// #713).
var auditGoModGoEnvWrite = regexp.MustCompile(`^echo "([A-Za-z_][A-Za-z0-9_]*)=\$\{([A-Za-z_][A-Za-z0-9_]*)\}" >> "(\$GITHUB_ENV|\$\{GITHUB_ENV\})"$`)

// auditGoModGoVersionShape is the one shape go.mod's go line may take, as the
// install step's own check names it: a release, 1.<n> or 1.<n>.<p>, and no
// prerelease (issue #713).
const auditGoModGoVersionShape = `^1\.[0-9]+(\.[0-9]+)?$`

// auditGoModulesStepName is the checks job's step that fetches the modules and
// holds every Go command after it to go.mod's Go (issue #713).
const auditGoModulesStepName = "Download the Go modules"

// auditGoModulesValueAssignment matches the line that takes the version from the
// job's environment, which the install step's checked value reaches it through:
// want="go${gomod_go}" (issue #713).
var auditGoModulesValueAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)="go\$\{gomod_go(:-)?\}"$`)

// auditGoListCommand matches a go list in a step's script: the command that reads
// go.mod's go line, which only the install step runs, with GOWORK=off and its own
// checks (issue #713).
var auditGoListCommand = regexp.MustCompile(`\bgo list\b`)

// auditGoModGoShapeCheck returns the if block between a step's read of go.mod's Go
// and the line that uses it at which the step holds the value to the shapes
// go.mod's go line takes, and false when the step holds none (issue #713). The
// line that opens the block is compared with the one form the check takes, in
// full: a check that only holds the shape somewhere lets every value through with
// one alternation beside it, as ^1\.[0-9]+(\.[0-9]+)?$|^.*$ does (issue #713).
func auditGoModGoShapeCheck(lines []string, readAt, usedAt int, name string) (auditShellIfBlock, bool) {
	want := fmt.Sprintf(`if ! [[ "${%s}" =~ %s ]]; then`, name, auditGoModGoVersionShape)

	for _, block := range auditShellIfBlocks(lines) {
		if block.open <= readAt || block.open >= usedAt {
			continue
		}

		if trimmed, _ := auditStepLineOf(lines[block.open]); trimmed != want {
			continue
		}

		return block, true
	}

	return auditShellIfBlock{}, false
}

// auditInstallGoModGoShapeCheck returns what the install step lacks to hold the
// value it read to the shapes go.mod's go line takes before anything builds with
// it (issue #713): the empty-value and one-line checks of issue #710 let a word or
// a prerelease through, and GOTOOLCHAIN="go<that value>" names a toolchain go
// cannot fetch, so the step would fail with a go error instead of a message of its
// own.
func auditInstallGoModGoShapeCheck(lines []string, readAt, usedAt int, name, where string) []string {
	block, ok := auditGoModGoShapeCheck(lines, readAt, usedAt, name)
	if !ok {
		return []string{fmt.Sprintf(`%s holds no if block between its read of go.mod's Go and the line that uses it which tests %s against %s: the step needs it as if ! [[ "${%s}" =~ %s ]]; then, with an ::error and an exit 1 of its own, before anything builds with the value (issue #713)`,
			where, name, auditGoModGoVersionShape, name, auditGoModGoVersionShape)}
	}

	var problems []string

	if !strings.Contains(strings.Join(block.body, "\n"), "::error") {
		problems = append(problems, fmt.Sprintf("%s's Go-version check holds no ::error: the run must say that go.mod's go line read as no Go version, or it ends with a bare non-zero exit nobody can read (issue #713)", where))
	}

	if !auditBlockExitsOne(block.body) {
		opened, _ := auditStepLineOf(lines[block.open])

		problems = append(problems, fmt.Sprintf("%s's Go-version check holds no exit 1 of its own (the block opens on %q): the exit belongs inside the same if block as its ::error, or the step goes on and builds govulncheck with a value that is no Go version (issue #713)", where, opened))
	}

	return problems
}

// auditInstallGoModGoEnvWrite returns what the install step lacks to hand the
// value it checked to the job's later steps (issue #713): after its checks have
// passed, the step writes it to $GITHUB_ENV, which is how the steps after it read
// one value and not a copy of their own.
func auditInstallGoModGoEnvWrite(lines []string, readAt, installAt int, name, where string) []string {
	checksEnd := -1

	for _, block := range auditShellIfBlocks(lines) {
		if block.open <= readAt || block.open >= installAt {
			continue
		}

		if end := block.open + len(block.body) - 1; end > checksEnd {
			checksEnd = end
		}
	}

	var (
		problems []string
		writeAt  = -1
	)

	for i := readAt + 1; i < installAt; i++ {
		trimmed, _ := auditStepLineOf(lines[i])
		if auditIsComment(trimmed) {
			continue
		}

		m := auditGoModGoEnvWrite.FindStringSubmatch(trimmed)
		if m == nil {
			if strings.Contains(trimmed, "GITHUB_ENV") {
				problems = append(problems, fmt.Sprintf(`%s writes to $GITHUB_ENV as %q, which hands the job another value than %s, the one this step checked: the line is echo "%s=${%s}" >> "$GITHUB_ENV", so the step after it reads exactly the value this one checked (issue #713)`, where, trimmed, name, name, name))
			}

			continue
		}

		if m[1] != name || m[2] != name {
			problems = append(problems, fmt.Sprintf(`%s writes %q to $GITHUB_ENV, which hands the job another value than %s, the one this step checked: the line is echo "%s=${%s}" >> "$GITHUB_ENV" (issue #713)`, where, trimmed, name, name, name))

			continue
		}

		if writeAt < 0 {
			writeAt = i
		}
	}

	if writeAt < 0 {
		return append(problems, fmt.Sprintf(`%s never hands %s to the job's later steps: after its checks pass it writes the value to the environment the next step reads, as echo "%s=${%s}" >> "$GITHUB_ENV"; without it the modules step reads go.mod a second time, with no GOWORK=off and no check of its own (issue #713)`, where, name, name, name))
	}

	if checksEnd >= 0 && writeAt < checksEnd {
		problems = append(problems, fmt.Sprintf("%s writes %s to $GITHUB_ENV on line %d of its script, before the check that ends on line %d has passed: a value written before the checks reaches every later step even when this step then fails, so the write belongs after them all (issue #713)",
			where, name, writeAt+1, checksEnd+1))
	}

	return problems
}

// auditGoModulesStep returns the checks job's "Download the Go modules" step, and
// whether the job holds it (issue #713).
func auditGoModulesStep(steps []auditJobStep) (auditJobStep, bool) {
	for _, step := range steps {
		if step.name == auditGoModulesStepName {
			return step, true
		}
	}

	return auditJobStep{}, false
}

// auditGoModulesStepFromEnv returns what the modules step lacks to take go.mod's
// Go from the install step's checked value (issue #713): the version reaches the
// shell through the job's environment and no ${{ }} expression, the step runs no
// go list of its own - which the install step alone runs, with GOWORK=off and the
// checks that fail the run on what it read - and a guard above the first use fails
// the run with its own ::error and exit 1 when the value is unset or empty, in the
// ${gomod_go:-} form that set -u cannot end the step on first. No line above that
// guard reads the value, whichever form the read takes: a default of its own above
// the guard is a read the guard has not checked yet, so the step holds no shape
// for one.
func auditGoModulesStepFromEnv(t *testing.T, step auditJobStep) []string {
	t.Helper()

	script := auditStepScript(t, step)
	where := fmt.Sprintf("%s's checks job, step %q", auditWorkflow, step.name)

	var problems []string

	for _, line := range script {
		trimmed, _ := auditStepLineOf(line)
		if auditIsComment(trimmed) {
			continue
		}

		if auditGoListCommand.MatchString(trimmed) {
			problems = append(problems, fmt.Sprintf(`%s runs %q: go.mod's Go is read once, by the install step, which reads it with GOWORK=off and fails the run when the read gives nothing, more than one line or no Go version. A second go list here has no GOWORK=off and no check of its own, so a failed read ends this step with no message and an empty one fails the run with a misleading %q. Read ${gomod_go} from the job's environment instead (issue #713)`, where, trimmed, "the go command is not go.mod's"))
		}
	}

	valueAt := -1

	for i, line := range script {
		trimmed, _ := auditStepLineOf(line)
		if auditIsComment(trimmed) || valueAt >= 0 {
			continue
		}

		if auditGoModulesValueAssignment.MatchString(trimmed) {
			valueAt = i
		}
	}

	if valueAt < 0 {
		problems = append(problems, fmt.Sprintf(`%s holds no line that takes the version from the job's environment: the step names go.mod's Go as want="go${gomod_go}", the value the install step checked and wrote to $GITHUB_ENV, rather than reading go.mod a second time (issue #713)`, where))
	}

	guardAt, guardBody := -1, []string(nil)

	for _, block := range auditShellIfBlocks(script) {
		if auditChecksEmptyVariable(auditIfBlockCondition(block.body), "gomod_go") {
			guardAt, guardBody = block.open, block.body

			break
		}
	}

	if guardAt < 0 {
		return append(problems, fmt.Sprintf(`%s holds no if block that fails the run when gomod_go is unset or empty: the value reaches the step through the job's environment, and a step that never received it must fail with its own ::error and exit 1, as if [ -z "${gomod_go:-}" ]; then ... fi above the first use (issue #713)`, where))
	}

	condition := auditIfBlockCondition(guardBody)
	guardLine, _ := auditStepLineOf(script[guardAt])

	if !strings.Contains(condition, "${gomod_go:-}") && !strings.Contains(condition, "${gomod_go-}") {
		problems = append(problems, fmt.Sprintf(`%s checks the value as %q, which set -u ends the step on before its ::error when the variable is unset: write the empty-or-unset form ${gomod_go:-}, so a step that never received the value fails with the message and not with "unbound variable" (issue #713)`, where, guardLine))
	}

	if !strings.Contains(strings.Join(guardBody, "\n"), "::error") {
		problems = append(problems, fmt.Sprintf("%s's empty-value guard holds no ::error: the run must say that the value the install step read did not reach this step, or it ends with a bare non-zero exit nobody can read (issue #713)", where))
	}

	if !auditBlockExitsOne(guardBody) {
		problems = append(problems, fmt.Sprintf("%s's empty-value guard holds no exit 1 of its own (the block opens on %q): the exit belongs inside the same if block as its ::error, or the step goes on and compares go.mod's Go with nothing (issue #713)", where, guardLine))
	}

	for i := 0; i < guardAt; i++ {
		trimmed, _ := auditStepLineOf(script[i])
		if auditIsComment(trimmed) || !auditNamesVariable(trimmed, "gomod_go") {
			continue
		}

		problems = append(problems, fmt.Sprintf("%s reads gomod_go on %q, above the guard that fails the run when the value is unset or empty: the guard comes first, so a step the value never reached fails with its ::error and not on the value it reads (issue #713)", where, trimmed))
	}

	if valueAt >= 0 && guardAt > valueAt {
		problems = append(problems, fmt.Sprintf("%s takes the value on line %d of its script, above the guard on line %d that fails the run when it is unset or empty: the guard comes first, so nothing uses a value this step never received (issue #713)", where, valueAt+1, guardAt+1))
	}

	return problems
}

// auditGoModGoOncePerJob returns what the checks job lacks to read go.mod's Go
// once and hand that one checked value to the step that holds every Go command to
// it (issue #713). The install step already reads it once, with GOWORK=off, and
// checks it; without the write to $GITHUB_ENV and the guard that reads it there,
// the modules step computes it a second time, with neither the GOWORK=off nor the
// checks, and fails the run with a misleading message for an empty read or no
// message at all for a failed one.
func auditGoModGoOncePerJob(t *testing.T, steps []auditJobStep) []string {
	t.Helper()

	step, installAt, _, ok := auditGovulncheckInstall(steps)
	if !ok {
		return []string{fmt.Sprintf("%s's checks job holds no go install of %s@: the step's read of go.mod's Go is the value the job reads once (issue #713)",
			auditWorkflow, auditGovulncheckModule)}
	}

	where := fmt.Sprintf("%s's checks job, step %q", auditWorkflow, step.name)

	readAt, name := -1, ""

	for i := installAt - 1; i >= 0; i-- {
		if assigned, ok := auditGoModReadAssignment(step.lines[i]); ok {
			readAt, name = i, assigned

			break
		}
	}

	if readAt < 0 {
		return []string{fmt.Sprintf("%s reads go.mod's Go on no line of its own above the install: read it once into a variable with GOWORK=off go list -m -f '{{.GoVersion}}', check it, and hand it on through $GITHUB_ENV (issue #713)", where)}
	}

	problems := auditInstallGoModGoShapeCheck(step.lines, readAt, installAt, name, where)
	problems = append(problems, auditInstallGoModGoEnvWrite(step.lines, readAt, installAt, name, where)...)

	modules, ok := auditGoModulesStep(steps)
	if !ok {
		return append(problems, fmt.Sprintf("%s's checks job holds no %q step: that step holds the Go commands to go.mod's Go, and it takes the value the install step checked instead of reading go.mod a second time (issue #713)",
			auditWorkflow, auditGoModulesStepName))
	}

	return append(problems, auditGoModulesStepFromEnv(t, modules)...)
}

// auditFakeGoExitOne and the bodies below are what the fake go command of a
// case prints: a failed read that gives nothing, a read that exits cleanly with
// no output, a read that gives two versions, the two shapes go.mod's go line
// really takes, and the one-line reads that are no Go version - a go prefix
// beside it, a word beside it (issue #710).
const (
	auditFakeGoExitOne   = "exit 1"
	auditFakeGoEmpty     = "exit 0"
	auditFakeGoTwoLines  = `printf '%s\n' 1.27.4 1.27.4`
	auditFakeGoOneLine   = `printf '%s\n' 1.27.4`
	auditFakeGoMinorOnly = `printf '%s\n' 1.27`
	auditFakeGoPrefixed  = `printf '%s\n' go1.27.4`
	auditFakeGoWithAWord = `printf '%s\n' '1.27.4 x'`
)

// TestAuditGoModReadFragmentAllowsOnlyTheShapesTheInstallStepUses pins which
// lines of a step's script the read fragment may hold (issues #710 and #713): the
// test runs the whole fragment, so every non-comment line of it must match one of
// the exact shapes the install step uses, in full - the read of go.mod's Go, the
// three checks that fail the run on what it read, their ::error, exit 1 and fi
// lines, and the write of the checked value to $GITHUB_ENV. The deny list this
// replaced let a command of another name through, and a tab where it counted a
// space, so a line that is none of those shapes is refused for what it is and not
// for a word a list happened to hold.
func TestAuditGoModReadFragmentAllowsOnlyTheShapesTheInstallStepUses(t *testing.T) {
	t.Parallel()

	const install = `GOTOOLCHAIN="go${gomod_go}" GOBIN="${bin}" go install "golang.org/x/vuln/cmd/govulncheck@v1.1.4"`

	for _, tc := range []struct {
		name   string
		script []string
		line   string // the offending line, for a fragment this test may not run
	}{
		{
			name:   "the read and its checks",
			script: slices.Concat(auditGoModReadStep, []string{install}),
		},
		{
			name: "the read, its checks and the write of the checked value to $GITHUB_ENV",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "gomod_go=${gomod_go}" >> "$GITHUB_ENV"`,
				install,
			}),
		},
		{
			name: "the read, the Go-version check the workflow runs and the write in its braces spelling",
			script: slices.Concat(auditGoModReadStep, []string{
				`if ! [[ "${gomod_go}" =~ ^1\.[0-9]+(\.[0-9]+)?$ ]]; then`,
				`  echo "::error title=Audit checks::go.mod's go line read as no Go version: see runbook step 11"`,
				`  exit 1`,
				`fi`,
				`echo "gomod_go=${gomod_go}" >> "${GITHUB_ENV}"`,
				install,
			}),
		},
		{
			name: "an if block that downloads behind the checks",
			script: slices.Concat(auditGoModReadStep, []string{
				`if curl -fsSLo "${dl}/trivy.tar.gz" "https://example.com/trivy.tar.gz"; then`,
				`  echo "::error title=Audit checks::trivy could not be downloaded"`,
				`  exit 1`,
				`fi`,
				install,
			}),
			line: `if curl -fsSLo "${dl}/trivy.tar.gz" "https://example.com/trivy.tar.gz"; then`,
		},
		{
			name: "an if block whose condition runs another program",
			script: slices.Concat(auditGoModReadStep, []string{
				`if python3 -c 'x'; then`,
				`  echo "::error title=Audit checks::nothing to report"`,
				`  exit 1`,
				`fi`,
				install,
			}),
			line: `if python3 -c 'x'; then`,
		},
		{
			name:   "a git fetch behind a literal tab",
			script: slices.Concat(auditGoModReadStep, []string{"git\tfetch", install}),
			line:   "git\tfetch",
		},
		{
			name:   "a comment line led by a no-break space",
			script: slices.Concat(auditGoModReadStep, []string{"\u00a0# || curl x", install}),
			line:   "\u00a0# || curl x",
		},
		{
			name:   "a comment line led by a vertical tab",
			script: slices.Concat(auditGoModReadStep, []string{"\v# || curl x", install}),
			line:   "\v# || curl x",
		},
		{
			name:   "a comment line led by a form feed",
			script: slices.Concat(auditGoModReadStep, []string{"\f# || curl x", install}),
			line:   "\f# || curl x",
		},
		{
			name:   "a comment line led by a next line",
			script: slices.Concat(auditGoModReadStep, []string{"\u0085# || curl x", install}),
			line:   "\u0085# || curl x",
		},
		{
			name:   "a comment line led by an em space",
			script: slices.Concat(auditGoModReadStep, []string{"\u2003# || curl x", install}),
			line:   "\u2003# || curl x",
		},
		{
			name:   "a comment line that ends in a carriage return",
			script: slices.Concat(auditGoModReadStep, []string{"# || curl x\r", install}),
			line:   "# || curl x\r",
		},
		{
			name: "a read line that downloads as well",
			script: slices.Concat([]string{
				`gomod_go="$(GOWORK=off go list -m -f '{{.GoVersion}}'; curl -fsSL https://example.com/go)" || gomod_go=""`,
			}, auditGoModReadStep[1:], []string{install}),
			line: `gomod_go="$(GOWORK=off go list -m -f '{{.GoVersion}}'; curl -fsSL https://example.com/go)" || gomod_go=""`,
		},
		{
			name: "an echo that downloads",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "::error title=Audit checks::$(curl -fsSL https://example.com/go)"`,
				install,
			}),
			line: `echo "::error title=Audit checks::$(curl -fsSL https://example.com/go)"`,
		},
		{
			name:   "a command line that ends in )",
			script: slices.Concat(auditGoModReadStep, []string{`./script.sh $(true)`, install}),
			line:   `./script.sh $(true)`,
		},
		{
			name:   "a ;; line",
			script: slices.Concat(auditGoModReadStep, []string{";;", install}),
			line:   ";;",
		},
		{
			name: "a go mod download in an if condition",
			script: slices.Concat(auditGoModReadStep, []string{
				`if go mod download; then`,
				`  echo "::error title=Audit checks::the modules could not be downloaded"`,
				`  exit 1`,
				`fi`,
				install,
			}),
			line: `if go mod download; then`,
		},
		{
			name: "a go get in an echo's command substitution",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "::error title=Audit checks::$(go get example.com/x@v1)"`,
				install,
			}),
			line: `echo "::error title=Audit checks::$(go get example.com/x@v1)"`,
		},
		{
			name:   "a git fetch line",
			script: slices.Concat(auditGoModReadStep, []string{`git fetch origin`, install}),
			line:   `git fetch origin`,
		},
		{
			name:   "a gh api line",
			script: slices.Concat(auditGoModReadStep, []string{`gh api repos/example/repo`, install}),
			line:   `gh api repos/example/repo`,
		},
		{
			name: "a write of another value to the job's environment",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "want=$(go list -m -f '{{.GoVersion}}')" >> "$GITHUB_ENV"`,
				install,
			}),
			line: `echo "want=$(go list -m -f '{{.GoVersion}}')" >> "$GITHUB_ENV"`,
		},
		{
			name: "a write of the value to a file the job does not read",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "gomod_go=${gomod_go}" >> "${RUNNER_TEMP}/gomod_go"`,
				install,
			}),
			line: `echo "gomod_go=${gomod_go}" >> "${RUNNER_TEMP}/gomod_go"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fragment, problem := auditGoModReadFragmentOf(tc.script)

			if tc.line == "" {
				if problem != "" {
					t.Fatalf("auditGoModReadFragmentOf(a step whose fragment holds the read and its checks) = %q, want the fragment this test runs: the read, the checks that fail the run on what it read and the write of the checked value to $GITHUB_ENV are the shapes the step uses (issues #710 and #713)", problem)
				}

				if len(fragment) == 0 {
					t.Fatalf("auditGoModReadFragmentOf(a step whose fragment holds the read and its checks) returned no fragment: the test runs the step's own read and checks, never a copy of them (issue #710)")
				}

				return
			}

			if problem == "" {
				t.Fatalf("auditGoModReadFragmentOf(a step whose fragment holds %q) reported no problem, want that line refused: this test runs the fragment with only the system paths on PATH, and every line of it must match one of the exact shapes the install step uses, in full (issues #710 and #713)", tc.line)
			}

			if len(fragment) != 0 {
				t.Errorf("auditGoModReadFragmentOf(a step whose fragment holds %q) returned the fragment %q beside its problem %q, want none: a fragment the test may not run is no fragment to run (issue #713)", tc.line, fragment, problem)
			}

			if !strings.Contains(problem, fmt.Sprintf("%q", tc.line)) {
				t.Errorf("auditGoModReadFragmentOf(a step whose fragment holds %q) = %q, want a message quoting that line: the refusal must say which line of the fragment the test may not run (issue #713)", tc.line, problem)
			}
		})
	}
}

// TestAuditFragmentLineShapesHoldTheFragmentToTheStepsOwnLines pins the allow list
// itself (issue #713): every line of the install step's read and its checks
// matches a shape, both spellings of the write of the checked value to $GITHUB_ENV
// match one, and a line that differs from a shape - in its command, its quoting,
// its redirection or a substitution inside a message - matches none.
func TestAuditFragmentLineShapesHoldTheFragmentToTheStepsOwnLines(t *testing.T) {
	t.Parallel()

	for _, shape := range auditFragmentLineShapes {
		if shape.name == "" {
			t.Errorf("auditFragmentLineShapes holds %q with no name: a shape is named for the reader of a refusal (issue #713)", shape.pattern)
		}
	}

	for _, line := range auditGoModReadStep {
		trimmed, _ := auditStepLineOf(line)

		if _, ok := auditLineShapeOf(auditFragmentLineShapes, trimmed); !ok {
			t.Errorf("auditLineShapeOf(auditFragmentLineShapes, %q) = no shape, want one: every line of the read and the checks is a shape the fragment this file runs holds (issue #713)", trimmed)
		}
	}

	for _, tc := range []struct {
		name   string
		line   string
		shaped bool
	}{
		{
			name:   "the check that the read gave a Go version",
			line:   `if ! [[ "${gomod_go}" =~ ^1\.[0-9]+(\.[0-9]+)?$ ]]; then`,
			shaped: true,
		},
		{
			name:   "the write of the checked value to $GITHUB_ENV",
			line:   `echo "gomod_go=${gomod_go}" >> "$GITHUB_ENV"`,
			shaped: true,
		},
		{
			name:   "the write of the checked value to $GITHUB_ENV, in its braces spelling",
			line:   `echo "gomod_go=${gomod_go}" >> "${GITHUB_ENV}"`,
			shaped: true,
		},
		{
			name: "a write that closes a brace it never opened",
			line: `echo "gomod_go=${gomod_go}" >> "$GITHUB_ENV}"`,
		},
		{
			name: "a write that opens a brace it never closes",
			line: `echo "gomod_go=${gomod_go}" >> "${GITHUB_ENV"`,
		},
		{
			name: "an if block whose condition runs another program",
			line: `if python3 -c 'x'; then`,
		},
		{
			name: "a git fetch behind a literal tab",
			line: "git\tfetch",
		},
		{
			name: "a git fetch line",
			line: `git fetch origin`,
		},
		{
			name: "a go mod download",
			line: `go mod download`,
		},
		{
			name: "a ::error whose message holds a substitution",
			line: `echo "::error title=Audit checks::$(curl -fsSL https://example.com/go)"`,
		},
		{
			name: "a ::error written to a file rather than printed",
			line: `echo "::error title=Audit checks::x" >> "${GITHUB_ENV}"`,
		},
		{
			name: "a write of another value to the job's environment",
			line: `echo "want=$(go list -m -f '{{.GoVersion}}')" >> "$GITHUB_ENV"`,
		},
		{
			name: "a write of the value to a file the job does not read",
			line: `echo "gomod_go=${gomod_go}" >> "${RUNNER_TEMP}/gomod_go"`,
		},
		{
			name: "the modules step's line that takes the value",
			line: `want="go${gomod_go}"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shape, ok := auditLineShapeOf(auditFragmentLineShapes, tc.line)
			if ok != tc.shaped {
				t.Errorf("auditLineShapeOf(auditFragmentLineShapes, %q) matched the shape %q: %v, want a shape: %v: the fragment holds the exact shapes the install step uses, in full, so a line that differs from one in its command, its quoting, its redirection or a substitution inside a message is no shape at all (issue #713)",
					tc.line, shape.name, ok, tc.shaped)
			}
		})
	}
}

// auditWriteFakeGo writes a go command that answers as body says and records the
// GOWORK it saw in gowork, in the directory the case puts first on PATH (issue
// #710). The paths are single-quoted: a subtest's name holds spaces, and a
// t.TempDir() path carries it.
func auditWriteFakeGo(t *testing.T, dir, body string) {
	t.Helper()

	auditWriteFakeProgram(t, dir, "go",
		"printf '%s' \"${GOWORK-none}\" > '"+filepath.Join(dir, "gowork")+"'\n"+body)
}

// TestAuditGovulncheckGoModReadFailsTheStepClearly runs the checks job's read of
// go.mod's Go as the step runs it, under set -euo pipefail, against a fake go
// command (issue #710). A read that fails must reach the step's ::error and not
// only its exit status, and so must a read that exits cleanly with no output; a
// read that gives two lines must fail too, because the install builds govulncheck
// with GOTOOLCHAIN=go<value> and a value that holds two versions is no toolchain
// go can fetch. A single line that is no Go version - a go prefix beside it, a
// word beside it - fails the same way, while the two shapes go.mod's go line
// really takes, 1.27.4 and a minor-only 1.27, pass. The go command the read ran
// saw GOWORK=off, so no go.work file of the checkout can answer for another
// module, and a read the checks pass hands its value to the job's later steps
// through $GITHUB_ENV (issue #713).
func TestAuditGovulncheckGoModReadFailsTheStepClearly(t *testing.T) {
	t.Parallel()

	steps := auditJobSteps(auditJobLines(t, "checks"))

	step, _, _, ok := auditGovulncheckInstall(steps)
	if !ok {
		t.Fatalf("%s's checks job holds no go install of %s@: the step's read of go.mod's Go is the fragment this test runs (issue #710)",
			auditWorkflow, auditGovulncheckModule)
	}

	fragment := auditGoModReadFragment(t, auditStepScript(t, step))

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("find bash on PATH: %v; the checks job's steps run under bash, and this test runs the step's own fragment with it (issue #710)", err)
	}

	for _, tc := range []struct {
		name      string
		fakeGo    string
		wantValue string // the value the step hands the job, for a read its checks pass
		wantError bool
		wantFail  bool
	}{
		{
			name:      "a read that fails reaches the ::error, not only a non-zero exit",
			fakeGo:    auditFakeGoExitOne,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "a read that exits 0 with no output reaches the ::error",
			fakeGo:    auditFakeGoEmpty,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "a read that gives two lines reaches the ::error",
			fakeGo:    auditFakeGoTwoLines,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "a read whose one line carries a word beside the version reaches the ::error",
			fakeGo:    auditFakeGoWithAWord,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "a read whose one line carries a go prefix reaches the ::error",
			fakeGo:    auditFakeGoPrefixed,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "a read that gives one line passes and hands the job its value",
			fakeGo:    auditFakeGoOneLine,
			wantValue: "1.27.4",
		},
		{
			name:      "a read that gives go.mod's minor-only version passes and hands the job its value",
			fakeGo:    auditFakeGoMinorOnly,
			wantValue: "1.27",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			auditWriteFakeGo(t, dir, tc.fakeGo)

			envFile := filepath.Join(t.TempDir(), "github_env")

			cmd := exec.CommandContext(t.Context(), bash, "-c", "set -euo pipefail\n"+strings.Join(fragment, "\n"))
			// Only what the fragment needs: the fake go command's
			// directory first on PATH, the system ones behind it for the
			// shell's own tools such as wc, a HOME of the case, and the
			// environment file the runner gives the step for the value
			// it hands on. The developer's environment, its go, its
			// GOFLAGS and its GOTOOLCHAIN reach neither the step nor the
			// fake.
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin",
				"HOME=" + t.TempDir(),
				"GITHUB_ENV=" + envFile,
			}

			var stdout, stderr bytes.Buffer

			cmd.Stdout, cmd.Stderr = &stdout, &stderr

			err := cmd.Run()

			if tc.wantFail {
				var exitErr *exec.ExitError
				if err == nil {
					t.Errorf("the step's fragment exited 0 when the go command it ran was %q (%s): the step must fail the run, with an ::error and an exit 1 (issue #710)", tc.fakeGo, tc.name)
				} else if !errors.As(err, &exitErr) {
					t.Fatalf("run bash on the step's fragment: %v", err)
				}
			} else if err != nil {
				t.Errorf("the step's fragment failed when the go command it ran was %q (%s) (stdout %q, stderr %q): %v; a read that gives go.mod's one Go version is what the install holds to, so the checks must let it through (issue #710)",
					tc.fakeGo, tc.name, stdout.String(), stderr.String(), err)
			}

			got := strings.Contains(stdout.String(), "::error")
			if got != tc.wantError {
				t.Errorf("the step's fragment printed an ::error: %v, want %v (stdout %q, stderr %q): the checks job's step runs under set -euo pipefail, so a read the fragment checks must fail the run with the workflow command that says which read failed, on stdout, and a read that gave one version must print none (issue #710)",
					got, tc.wantError, stdout.String(), stderr.String())
			}

			recorded, readErr := os.ReadFile(filepath.Join(dir, "gowork"))
			if readErr != nil {
				t.Errorf("the fake go command did not run: %v; the fragment must read go.mod's Go with GOWORK=off go list -m -f '{{.GoVersion}}' (issue #710)", readErr)
			} else if string(recorded) != "off" {
				t.Errorf("the go command the read ran saw GOWORK=%q, want %q: with a go.work file in the checkout the go command answers for another module's go.mod, and the install would build govulncheck with a Go go.mod does not name (issue #710)",
					string(recorded), "off")
			}

			if tc.wantFail {
				written, readErr := os.ReadFile(envFile)

				switch {
				case readErr != nil && !os.IsNotExist(readErr):
					t.Fatalf("read the environment file the step's write goes to: %v", readErr)
				case len(written) != 0:
					t.Errorf("the step wrote %q to $GITHUB_ENV (%s), want no file or an empty one: the step's checks failed the read, and a failed read must hand nothing to the job's later steps, or a step after it builds with a value this one refused (issue #713)",
						string(written), tc.name)
				}

				return
			}

			if tc.wantValue == "" {
				return
			}

			written, writeErr := os.ReadFile(envFile)
			if writeErr != nil {
				t.Errorf("the step wrote no environment file: %v; a read its checks pass must hand the value it checked to the job's later steps, as echo \"gomod_go=${gomod_go}\" >> \"$GITHUB_ENV\", so the modules step reads one checked value and not a second go list of its own (issue #713)", writeErr)

				return
			}

			if want := "gomod_go=" + tc.wantValue + "\n"; string(written) != want {
				t.Errorf("the step wrote %q to $GITHUB_ENV, want %q: the value the step checked is the one the modules step reads, so the whole job holds every Go command to go.mod's one Go (issue #713)", string(written), want)
			}
		})
	}
}

// auditWriteFakeProgram writes name into dir as an executable shell script: the
// fake command a step's PATH finds, so the step this test runs downloads and
// builds nothing (issue #713).
func auditWriteFakeProgram(t *testing.T, dir, name, body string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o600); err != nil {
		t.Fatalf("write the fake %s command into %s: %v", name, dir, err)
	}

	// The 0755 is what makes the file a command the step's PATH finds: it
	// lives in the case's own t.TempDir(), so the write stays 0600 and the
	// mode follows it.
	if err := os.Chmod(path, os.FileMode(0o755)); err != nil {
		t.Fatalf("make the fake %s command %s executable: %v", name, path, err)
	}
}

// TestAuditGoModulesStepTakesTheInstallStepsCheckedValue runs the checks job's
// "Download the Go modules" step as the job runs it, under set -euo pipefail,
// with a fake go command and a fake govulncheck so the step downloads nothing
// (issue #713). The version the step holds the Go commands to comes from the
// install step, through the job's environment: a step that never received it -
// unset or empty - fails the run with its own ::error and exit 1, and the value
// the install step checked lets the step through past its guard, after which its
// go commands run with no go list of their own. The step's lines are held to the
// exact shapes the step uses before any of them runs, so this test runs nothing
// the step does not do (issues #710 and #713).
func TestAuditGoModulesStepTakesTheInstallStepsCheckedValue(t *testing.T) {
	t.Parallel()

	steps := auditJobSteps(auditJobLines(t, "checks"))

	step, ok := auditGoModulesStep(steps)
	if !ok {
		t.Fatalf("%s's checks job holds no %q step: that step holds every Go command to go.mod's Go, and the value it holds them to is the install step's (issue #713)",
			auditWorkflow, auditGoModulesStepName)
	}

	script := auditModulesStepScript(t, step)

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("find bash on PATH: %v; the checks job's steps run under bash, and this test runs the step's own script with it (issue #713)", err)
	}

	for _, tc := range []struct {
		name      string
		value     string
		unset     bool // the install step's write never happened, so the job's environment holds no gomod_go at all
		wantError bool
		wantFail  bool
	}{
		{
			name:      "a job that never set the value fails the run with its own ::error",
			unset:     true,
			wantError: true,
			wantFail:  true,
		},
		{
			name:      "an empty value fails the run with its own ::error",
			wantError: true,
			wantFail:  true,
		},
		{
			name:  "the value the install step checked lets the step through",
			value: "1.27.4",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")

			// A go command that downloads nothing: it records how it was
			// called, answers the step's own questions with the Go the
			// checks pass, and fails a go list, which the install step
			// alone runs.
			auditWriteFakeProgram(t, dir, "go",
				"printf '%s\\n' \"$*\" >> '"+calls+"'\n"+
					"case \"$1\" in\n"+
					"  mod) exit 0 ;;\n"+
					"  -C) exit 0 ;;\n"+
					"  env) printf '%s\\n' 'go1.27.4' ;;\n"+
					"  version) printf '%s\\n' '/tmp/bin/govulncheck: go1.27.4' ;;\n"+
					"  list) printf '%s\\n' '0.0.0'; exit 1 ;;\n"+
					"esac")
			auditWriteFakeProgram(t, dir, "govulncheck", `printf '%s\n' 'Go: go1.27.4'`)

			env := []string{
				"PATH=" + dir + ":/usr/bin:/bin",
				"HOME=" + t.TempDir(),
				"RUNNER_TEMP=" + t.TempDir(),
				"GITHUB_ENV=" + filepath.Join(t.TempDir(), "github_env"),
			}
			if !tc.unset {
				env = append(env, "gomod_go="+tc.value)
			}

			cmd := exec.CommandContext(t.Context(), bash, "-c", "set -euo pipefail\n"+strings.Join(script, "\n"))
			// Only what the step needs, as for the install step's
			// fragment: the two fakes first on PATH, the system paths
			// behind them for timeout and grep, and a RUNNER_TEMP and
			// GITHUB_ENV of the case. The developer's go reaches neither
			// the step nor the fake.
			cmd.Env = env

			var stdout, stderr bytes.Buffer

			cmd.Stdout, cmd.Stderr = &stdout, &stderr

			err := cmd.Run()

			if tc.wantFail {
				var exitErr *exec.ExitError
				if err == nil {
					t.Errorf("the step exited 0 with gomod_go %q (%s) (stdout %q, stderr %q): the step must fail the run with its own ::error and an exit 1 when the value the install step checked never reached it (issue #713)",
						tc.value, tc.name, stdout.String(), stderr.String())
				} else if !errors.As(err, &exitErr) {
					t.Fatalf("run bash on the step's script: %v", err)
				}
			} else if err != nil {
				t.Errorf("the step failed with gomod_go %q (%s) (stdout %q, stderr %q): %v; the value the install step checked and wrote to $GITHUB_ENV is what this step holds the Go commands to, so its guard must let it through (issue #713)",
					tc.value, tc.name, stdout.String(), stderr.String(), err)
			}

			got := strings.Contains(stdout.String(), "::error")
			if got != tc.wantError {
				t.Errorf("the step printed an ::error: %v, want %v (stdout %q, stderr %q): a step that never received the value the install step checked must fail the run with the workflow command that says so, on stdout, and a step that received it must print none (issue #713)",
					got, tc.wantError, stdout.String(), stderr.String())
			}

			recorded, readErr := os.ReadFile(calls)
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatalf("read the fake go command's calls: %v", readErr)
			}

			for _, call := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
				if strings.HasPrefix(call, "list") {
					t.Errorf("the go command the step ran was called as %q: go.mod's Go is read once, by the install step, with GOWORK=off and the checks that fail the run on what it read, and this step takes that value from the job's environment (issue #713)", call)
				}
			}

			if tc.wantFail {
				if strings.TrimSpace(string(recorded)) != "" {
					t.Errorf("the fake go command was called as %q with gomod_go %q (%s): the guard that the install step's checked value reached this step comes before the module downloads, so a step that never received it runs no go command at all (issue #713)",
						strings.TrimSpace(string(recorded)), tc.value, tc.name)
				}

				return
			}

			if !strings.Contains(string(recorded), "mod download") {
				t.Errorf("the step ran no go mod download (%q): the value the install step checked must let the step past its guard to the work it does (issue #713)", string(recorded))
			}
		})
	}
}

// TestAuditGoModulesStepAllowsOnlyTheShapesTheStepUses pins which lines of the
// checks job's "Download the Go modules" step this file runs (issue #713): the
// step's script runs under make test, so every one of its lines must match one of
// the exact shapes the step uses, in full, and a line of another shape is refused
// before any of them runs. A curl line added to the step ran with every test
// passing before this pin: the shapes hold the test to the step's own lines and
// nothing else.
func TestAuditGoModulesStepAllowsOnlyTheShapesTheStepUses(t *testing.T) {
	t.Parallel()

	steps := auditJobSteps(auditJobLines(t, "checks"))

	step, ok := auditGoModulesStep(steps)
	if !ok {
		t.Fatalf("%s's checks job holds no %q step: that step's script is what this test reads (issue #713)",
			auditWorkflow, auditGoModulesStepName)
	}

	script := auditStepScript(t, step)

	// The line goes straight after set -euo pipefail, where a line the step
	// grows would land.
	withLine := func(line string) []string {
		return slices.Concat(script[:1], []string{"          " + line}, script[1:])
	}

	for _, tc := range []struct {
		name   string
		script []string
		line   string // the offending line, for a step this test may not run
	}{
		{
			name:   "the step's own script",
			script: script,
		},
		{
			name:   "a curl line in the step",
			script: withLine(`curl --version > "${RUNNER_TEMP}/curl-ran"`),
			line:   `curl --version > "${RUNNER_TEMP}/curl-ran"`,
		},
		{
			name:   "a git fetch line in the step",
			script: withLine(`git fetch origin`),
			line:   `git fetch origin`,
		},
		{
			name:   "a git fetch behind a literal tab",
			script: withLine("git\tfetch"),
			line:   "git\tfetch",
		},
		{
			name:   "a comment line led by a no-break space",
			script: withLine("\u00a0# || curl x"),
			line:   "\u00a0# || curl x",
		},
		{
			name:   "a comment line led by a vertical tab",
			script: withLine("\v# || curl x"),
			line:   "\v# || curl x",
		},
		{
			name:   "a comment line led by a form feed",
			script: withLine("\f# || curl x"),
			line:   "\f# || curl x",
		},
		{
			name:   "a comment line led by a next line",
			script: withLine("\u0085# || curl x"),
			line:   "\u0085# || curl x",
		},
		{
			name:   "a comment line led by an em space",
			script: withLine("\u2003# || curl x"),
			line:   "\u2003# || curl x",
		},
		{
			name:   "a comment line that ends in a carriage return",
			script: withLine("# || curl x\r"),
			line:   "# || curl x\r",
		},
		{
			name:   "a go list in the step",
			script: withLine(`go list -m -f '{{.GoVersion}}'`),
			line:   `go list -m -f '{{.GoVersion}}'`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, problem := auditModulesStepScriptOf(tc.script)

			if tc.line == "" {
				if problem != "" {
					t.Fatalf("auditModulesStepScriptOf(the step's own script) = %q, want no problem: every line of the step the checks job runs is one of the shapes this test holds it to (issue #713)", problem)
				}

				if len(got) == 0 {
					t.Fatalf("auditModulesStepScriptOf(the step's own script) returned no script: this test runs the step's own lines, never a copy of them (issue #713)")
				}

				return
			}

			if problem == "" {
				t.Fatalf("auditModulesStepScriptOf(a step whose script holds %q) reported no problem, want that line refused: this test runs the whole step, so every line of it must match one of the exact shapes the step uses, in full (issue #713)", tc.line)
			}

			if len(got) != 0 {
				t.Errorf("auditModulesStepScriptOf(a step whose script holds %q) returned the script %q beside its problem %q, want none: a script the test may not run is no script to run (issue #713)", tc.line, got, problem)
			}

			if !strings.Contains(problem, fmt.Sprintf("%q", tc.line)) {
				t.Errorf("auditModulesStepScriptOf(a step whose script holds %q) = %q, want a message quoting that line: the refusal must say which line of the step the test may not run (issue #713)", tc.line, problem)
			}
		})
	}
}

// auditGoModGoShapeCheckLines is the install step's check that go.mod's Go is a Go
// version and the write of the checked value to $GITHUB_ENV, as the step has them
// (issue #713): the lines a case of
// TestAuditGoModGoOncePerJobRejectsALoosenedShapeOrWrite keeps, or mutates in the
// one line it changes.
var auditGoModGoShapeCheckLines = []string{
	`if ! [[ "${gomod_go}" =~ ^1\.[0-9]+(\.[0-9]+)?$ ]]; then`,
	`  echo "::error title=Audit checks::go.mod's go line read as no Go version"`,
	`  exit 1`,
	`fi`,
	`echo "gomod_go=${gomod_go}" >> "${GITHUB_ENV}"`,
}

// auditGoModulesStepLines is the "Download the Go modules" step's guard and the
// line that takes the install step's checked value, as the step has them (issue
// #713).
var auditGoModulesStepLines = []string{
	`set -euo pipefail`,
	`if [ -z "${gomod_go:-}" ]; then`,
	`  echo "::error title=Audit checks::go.mod's Go read by the install step did not reach this step"`,
	`  exit 1`,
	`fi`,
	`want="go${gomod_go}"`,
}

// auditGoModGoSteps returns the checks job a case of
// TestAuditGoModGoOncePerJobRejectsALoosenedShapeOrWrite reads: the
// install step, with the read and its two checks of issue #710 and the case's own
// lines above the install, and the modules step with the case's script (issue
// #713).
func auditGoModGoSteps(install, modules []string) []auditJobStep {
	installStep := auditGovulncheckToolchainStep(auditGovulncheckToolchainCase{
		derive:  slices.Concat(auditGoModReadStep, install),
		install: auditGoModReadInstall,
	})

	modulesStep := auditJobStep{
		name: auditGoModulesStepName,
		lines: []string{
			"      - name: " + auditGoModulesStepName,
			"        run: |",
		},
	}

	for _, line := range modules {
		modulesStep.lines = append(modulesStep.lines, "          "+line)
	}

	return []auditJobStep{installStep, modulesStep}
}

// TestAuditGoModGoOncePerJobRejectsALoosenedShapeOrWrite pins the two checks the
// job's one read of go.mod's Go adds to issue #710's (issue #713): the
// install step holds the value it read to the shapes go.mod's go line takes, and
// writes that checked value to $GITHUB_ENV for the modules step. A check that
// keeps the shape somewhere in its line lets every value through with an
// alternation beside it, as ^1\.[0-9]+(\.[0-9]+)?$|^.*$ does, and a write whose
// braces do not match is a path of another name that hands the job nothing: each
// is a case here. The modules step is held to one rule too: every line above its
// guard that reads gomod_go is refused, whichever form the read takes, and a line
// is a comment only where bash reads one, so a line led by a byte that is no blank
// to bash reads the value the guard has not checked yet.
func TestAuditGoModGoOncePerJobRejectsALoosenedShapeOrWrite(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		install []string // the install step's lines from the shape check on
		modules []string // the modules step's lines, auditGoModulesStepLines when nil
		want    int
	}{
		{
			name:    "the step holds the value to a Go version and writes it to $GITHUB_ENV",
			install: auditGoModGoShapeCheckLines,
		},
		{
			name: "the Go-version check lets every value through with an alternation beside the shape",
			install: slices.Concat(
				[]string{`if ! [[ "${gomod_go}" =~ ^1\.[0-9]+(\.[0-9]+)?$|^.*$ ]]; then`},
				auditGoModGoShapeCheckLines[1:],
			),
			want: 1,
		},
		{
			name: "the Go-version check tests another variable than the read filled",
			install: slices.Concat(
				[]string{`if ! [[ "${want}" =~ ^1\.[0-9]+(\.[0-9]+)?$ ]]; then`},
				auditGoModGoShapeCheckLines[1:],
			),
			want: 1,
		},
		{
			name: "the write closes a brace it never opened",
			install: slices.Concat(
				auditGoModGoShapeCheckLines[:4],
				[]string{`echo "gomod_go=${gomod_go}" >> "$GITHUB_ENV}"`},
			),
			want: 2,
		},
		{
			name: "the write opens a brace it never closes",
			install: slices.Concat(
				auditGoModGoShapeCheckLines[:4],
				[]string{`echo "gomod_go=${gomod_go}" >> "${GITHUB_ENV"`},
			),
			want: 2,
		},
		{
			name:    "a default line that only restates the value above the guard",
			install: auditGoModGoShapeCheckLines,
			modules: slices.Concat(
				auditGoModulesStepLines[:1],
				[]string{`gomod_go="${gomod_go:-}"`},
				auditGoModulesStepLines[1:],
			),
			want: 1,
		},
		{
			name:    "a line that only looks like a comment above the guard, led by a no-break space",
			install: auditGoModGoShapeCheckLines,
			modules: slices.Concat(
				auditGoModulesStepLines[:1],
				[]string{"\u00a0# ${gomod_go}"},
				auditGoModulesStepLines[1:],
			),
			want: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			modules := tc.modules
			if modules == nil {
				modules = auditGoModulesStepLines
			}

			steps := auditGoModGoSteps(tc.install, modules)

			problems := auditGoModGoOncePerJob(t, steps)
			if len(problems) != tc.want {
				t.Errorf("auditGoModGoOncePerJob(the steps this case builds - the install step from %q on, the modules step %q) reported %d problem(s), want %d:\n%s",
					tc.install, modules, len(problems), tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}
