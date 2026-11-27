package architecture_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// This file holds the two checks issue #658 adds to the audit workflow's pins:
// that the checks job builds govulncheck with go.mod's Go and holds the built
// tool to it, and that every trivy subcommand receives only flags the pinned
// release accepts for it (ADR 0040). Issue #687 reads the Go of the first one
// from go.mod at run time: the install derives it, so the workflow names no
// version of its own anywhere.

// auditGoDirective matches go.mod's go line: the Go version every build of the
// module uses, and the one the checks job must build govulncheck with (issue
// #658).
var auditGoDirective = regexp.MustCompile(`(?m)^go[ \t]+([0-9][0-9A-Za-z.-]*)[ \t]*$`)

// auditGoModGo returns go.mod's go version, read from go.mod itself: the test
// pins no version of its own, so a bump of the module's Go needs no change here
// (issue #658).
func auditGoModGo(t *testing.T) string {
	t.Helper()

	mod := strings.ReplaceAll(string(readRepositoryFile(t, "go.mod")), "\r\n", "\n")

	m := auditGoDirective.FindStringSubmatch(mod)
	if m == nil {
		t.Fatalf("go.mod holds no go line: the checks job builds govulncheck with go.mod's Go (issue #658)")
	}

	return m[1]
}

// auditJobStep is one step of a job: its position among the job's steps, the
// name its line gives it when it has one, and its own lines.
type auditJobStep struct {
	index int
	name  string
	lines []string
}

// auditJobSteps splits a job's lines into the steps it runs: each starts at a
// "      - " line and runs to the line before the next one, so a step's run
// block, its arrays and its flags are read on their own (issue #658).
func auditJobSteps(lines []string) []auditJobStep {
	const head = "      - "

	var steps []auditJobStep

	for i, line := range lines {
		if !strings.HasPrefix(line, head) {
			continue
		}

		end := len(lines)

		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], head) {
				end = j

				break
			}
		}

		step := auditJobStep{index: len(steps), lines: lines[i:end]}

		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "- name: "); ok {
			step.name = name
		}

		steps = append(steps, step)
	}

	return steps
}

// auditCommand returns the words after the first word of line that runs tool: a
// word that names the tool alone, or a path or variable that ends in it, then
// the command's own arguments as the shell splits them. A comment line runs
// nothing.
func auditCommand(line, tool string) ([]string, bool) {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return nil, false
	}

	words := strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(auditToolSeparators, r) })

	for i, word := range words {
		if word == tool || strings.HasSuffix(word, "/"+tool) {
			return words[i+1:], true
		}
	}

	return nil, false
}

// auditGovulncheckModule is the module the checks job installs govulncheck
// from. It is a Go program, so the go command that builds it decides which
// standard library its findings are about (issue #658).
const auditGovulncheckModule = "golang.org/x/vuln/cmd/govulncheck"

// auditGovulncheckInstall returns the step that installs govulncheck in a job,
// the index of the line that installs it among that step's lines, the line
// itself trimmed, and whether the job installs it at all.
func auditGovulncheckInstall(steps []auditJobStep) (auditJobStep, int, string, bool) {
	for _, step := range steps {
		for i, line := range step.lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}

			if strings.Contains(line, "go install") && strings.Contains(line, auditGovulncheckModule+"@") {
				return step, i, strings.TrimSpace(line), true
			}
		}
	}

	return auditJobStep{}, 0, "", false
}

// auditGovulncheckScanStep returns the first step of a job that scans with
// govulncheck rather than asking it its version: a step that runs it with
// -format, which every scan of the workflow passes and no version check does.
func auditGovulncheckScanStep(steps []auditJobStep) (auditJobStep, bool) {
	for _, step := range steps {
		for _, line := range auditCodeLines(step.lines) {
			if args, ok := auditCommand(line, "govulncheck"); ok && slices.Contains(args, "-format") {
				return step, true
			}
		}
	}

	return auditJobStep{}, false
}

// auditRunsGovulncheckVersion reports whether step runs govulncheck -version.
func auditRunsGovulncheckVersion(step auditJobStep) bool {
	for _, line := range auditCodeLines(step.lines) {
		if args, ok := auditCommand(line, "govulncheck"); ok && slices.Contains(args, "-version") {
			return true
		}
	}

	return false
}

// auditGoModGoAssignment matches a shell assignment's name: the variable a
// step's line gives the value it reads.
var auditGoModGoAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)

// auditGoModGoVariable returns the name of the shell variable a step holds
// go.mod's Go in, and whether it holds one: the name an assignment that reads
// go.mod's go line - naming go.mod, GoVersion or go mod edit - gives its value.
// The version guard compares the built tool's Go with that variable, never with
// a version written into the workflow (issue #658).
func auditGoModGoVariable(lines []string) (string, bool) {
	for _, line := range auditCodeLines(lines) {
		if !strings.Contains(line, "go.mod") && !strings.Contains(line, "GoVersion") && !strings.Contains(line, "go mod edit") {
			continue
		}

		if m := auditGoModGoAssignment.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1], true
		}
	}

	return "", false
}

// auditVariableOperators are the shell's operators a braced expansion applies
// to the variable's value, and the brace that ends one without an operator:
// ${name}, ${name##pattern}, ${name%pattern}, ${name:-word}. A read of the
// variable is a read whatever follows its name, and the workflow's own guard
// compares ${built##* }, the binary's build word alone (issue #658).
const auditVariableOperators = "}:#%/^,@-=+?*"

// auditNamesVariable reports whether line reads the shell variable name, in
// either the unbraced form, $name, or a braced expansion of it, ${name} and the
// forms that apply an operator to its value.
func auditNamesVariable(line, name string) bool {
	if strings.Contains(line, "$"+name+" ") || strings.Contains(line, "$"+name+"\"") ||
		strings.Contains(line, "$"+name+"\t") || strings.HasSuffix(strings.TrimSpace(line), "$"+name) {
		return true
	}

	for _, op := range auditVariableOperators {
		if strings.Contains(line, "${"+name+string(op)) {
			return true
		}
	}

	return false
}

// auditComparisons are the shell's ways of comparing two words: the [ ] and
// [[ ]] operators of strings and of numbers. A line that only prints the built
// Go, or that assigns it, holds none of them.
var auditComparisons = []string{"!=", "-ne", "==", "-eq"}

// auditComparesVariables reports whether line compares the shell variables names
// with one another: a comparison operator, and a read of every one of the names.
// A comparison that reads only go.mod's Go holds the built tool to nothing (issue
// #658).
func auditComparesVariables(line string, names ...string) bool {
	for _, name := range names {
		if !auditNamesVariable(line, name) {
			return false
		}
	}

	for _, op := range auditComparisons {
		if strings.Contains(line, op) {
			return true
		}
	}

	return false
}

// auditGovulncheckVersionGuard returns what a step that runs govulncheck
// -version lacks to hold the tool to go.mod's Go: the step reads the wanted
// version from go.mod, runs go version on the govulncheck binary itself, holds
// what that prints in a variable of its own, compares that variable with
// go.mod's in the if that is the first code line after the assignment, and
// fails the run inside that block - exit 1, after an ::error naming govulncheck
// that the run's log names the cause with - on a mismatch, so a govulncheck
// built with another Go than go.mod's stops the run instead of scanning with it
// (issue #658). A comparison that reads go.mod's Go alone holds the built tool
// to nothing, so the comparison must name both variables. The block's own
// bounds decide what the ::error and the exit 1 are: an ::error or an exit 1
// after the block's fi is another check's line, and a guard that lost its own
// would then only warn (issue #658).
//
// go version on the binary is what catches a tool built with another Go:
// govulncheck -version's "Go:" line is the go command's own go env GOVERSION,
// which says which Go the tool would scan for, never which Go the go command
// that built it used, and only the binary's build information tells the two
// apart.
func auditGovulncheckVersionGuard(step auditJobStep, want string) []string {
	var (
		problems []string
		where    = fmt.Sprintf("%s's checks job, step %q", auditWorkflow, step.name)
	)

	lines := auditCodeLines(step.lines)

	modVar, namesGoMod := auditGoModGoVariable(step.lines)
	if !namesGoMod {
		problems = append(problems, fmt.Sprintf("%s runs govulncheck -version but holds no line that reads go.mod's go line into a variable (one naming go.mod, GoVersion or go mod edit): the wanted Go is go.mod's own, never a version written into the workflow (issue #658)", where))
	}

	// go version on the govulncheck binary: a path that ends in /govulncheck.
	goVersion, builtBy := -1, ""

	for i, line := range lines {
		args, ok := auditCommand(line, "go")
		if !ok || len(args) == 0 || args[0] != "version" {
			continue
		}

		for _, arg := range args[1:] {
			if strings.HasSuffix(strings.TrimRight(arg, `"')`), "/govulncheck") {
				goVersion, builtBy = i, arg

				break
			}
		}

		if goVersion >= 0 {
			break
		}
	}

	if goVersion < 0 {
		problems = append(problems, fmt.Sprintf("%s runs govulncheck -version but holds no line that runs go version on the govulncheck binary itself (a path ending in /govulncheck): govulncheck -version names the Go the go command reports for it, not the Go that built it, so only go version on the binary shows a tool built with another Go than go.mod's %q, and a govulncheck built with an older Go cannot load the module at all (issue #658)", where, want))
	} else {
		// go version's output goes into a variable the comparison names: the
		// comparison is what holds the built tool to go.mod's Go, and a
		// comparison that reads go.mod's Go alone passes for it whatever the
		// binary was built with (issue #658).
		builtVar := ""

		if m := auditGoModGoAssignment.FindStringSubmatch(strings.TrimSpace(lines[goVersion])); m != nil {
			builtVar = m[1]
		}

		// The comparison is the first code line after the assignment, blank and
		// comment lines aside: that if block, and no line further down the step,
		// is the guard (issue #658).
		first := -1

		for i := goVersion + 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "" {
				first = i

				break
			}
		}

		compared, guard := false, -1

		if first >= 0 && strings.HasPrefix(strings.TrimSpace(lines[first]), "if ") {
			guard = first
			compared = auditComparesVariables(lines[first], modVar, builtVar)
		}

		switch {
		case builtVar == "":
			problems = append(problems, fmt.Sprintf("%s runs go version on the govulncheck binary (%q) without holding what it prints in a variable of its own (an assignment such as built=\"$(go version %s)\"): no comparison then reads the built tool's Go, so any line that reads go.mod's %q passes for holding it there (issue #658)", where, strings.TrimSpace(lines[goVersion]), builtBy, want))
		case first < 0:
			problems = append(problems, fmt.Sprintf("%s runs go version on the govulncheck binary into %q but holds no line after it at all, so no comparison names both it and go.mod's Go (%q, the variable %q the step reads go.mod into): the step must compare what the binary prints with go.mod's Go, right after it (issue #658)", where, builtVar, "go"+want, modVar))
		case guard < 0:
			problems = append(problems, fmt.Sprintf("%s runs go version on the govulncheck binary into %q and then runs %q before any if that compares the two: the comparison must be the first code line after the go version line, blank and comment lines aside, or the ::error and exit 1 this guard needs are lines of another check further down the step and a mismatch only warns (issue #658)", where, builtVar, strings.TrimSpace(lines[first])))
		case !compared:
			problems = append(problems, fmt.Sprintf("%s runs go version on the govulncheck binary into %q but holds no comparison that names both it and go.mod's Go (%q, the variable %q the step reads go.mod into): the comparison must read both, or a line such as [ \"${%s}\" != \"\" ] reads go.mod's Go alone and holds the built tool to nothing, so a govulncheck built with another Go than go.mod's %q scans anyway (issue #658)", where, builtVar, "go"+want, modVar, modVar, want))
		}

		// The failure the block holds: an ::error naming govulncheck, then exit
		// 1, both inside the block that if opens and before its fi. One after
		// the fi is whatever the step runs next, so the guard's own lines must
		// be the ones inside the block (issue #658).
		if guard >= 0 {
			// The block runs to its fi: the first line after the if that is a
			// lone fi, which is the end of a block that opens no if of its own.
			end := len(lines)

			for i := guard + 1; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "fi" {
					end = i

					break
				}
			}

			cause := -1

			for i := guard + 1; i < end; i++ {
				if strings.Contains(lines[i], "::error") && strings.Contains(lines[i], "govulncheck") {
					cause = i

					break
				}
			}

			fails := false

			if cause >= 0 {
				for i := cause + 1; i < end; i++ {
					if strings.Contains(lines[i], "exit 1") {
						fails = true

						break
					}
				}
			}

			switch {
			case cause < 0:
				problems = append(problems, fmt.Sprintf("%s runs go version on the govulncheck binary but holds no ::error naming govulncheck inside the if block that compares the two (between that if and its fi): the run's log names the cause, and an ::error after the block's fi is another check's, so a mismatch would only warn instead of failing the run (issue #658)", where))
			case !fails:
				problems = append(problems, fmt.Sprintf("%s names the govulncheck cause in an ::error inside the if block but no exit 1 follows it before the block's fi: an exit 1 after the fi is another check's, so the run would not fail on a mismatch and would scan with a govulncheck built with another Go than go.mod's %q (issue #658)", where, want))
			}
		}
	}

	return problems
}

// auditGovulncheckGuardCase is one case of
// TestAuditGovulncheckVersionGuardHoldsTheBuiltGoItself: the go version line a
// step holds, the comparison the case puts after it, an unrelated code line a
// case slips between the two when it has one, whether the case leaves the
// guard's ::error or its exit 1 to after the block's fi, and the count of
// problems the guard must then report (issue #658).
type auditGovulncheckGuardCase struct {
	name         string
	goVersion    string
	comparison   string
	between      string
	errorAfterFi bool
	exitAfterFi  bool
	want         int
}

// auditGovulncheckGuardStep returns a checks job step shaped like the workflow's
// own "Download the Go modules" step, with its go version line, the comparison
// after it and the guard's block written as a case asks: every other line is
// what the guard needs to find inside that block, so a problem it reports is
// about the lines the case moved (issue #658).
func auditGovulncheckGuardStep(c auditGovulncheckGuardCase) auditJobStep {
	lines := []string{
		"      - name: Download the Go modules",
		"        run: |",
		"          set -euo pipefail",
		`          want="go$(go list -m -f '{{.GoVersion}}')"`,
		"          " + c.goVersion,
	}

	if c.between != "" {
		lines = append(lines, "          "+c.between)
	}

	lines = append(lines, "          "+c.comparison)

	// The block's failure, each line where the case puts it: inside the if
	// block that compares the two, or after its fi, where it belongs to
	// whatever comes next in the step instead (issue #658).
	if !c.errorAfterFi {
		lines = append(lines, `            echo "::error title=Audit checks::govulncheck was not built with go.mod's ${want}: see runbook step 11"`)
	}

	if !c.exitAfterFi {
		lines = append(lines, "            exit 1")
	}

	lines = append(lines, "          fi")

	if c.errorAfterFi {
		lines = append(lines, `          echo "::error title=Audit checks::govulncheck was not built with go.mod's ${want}: see runbook step 11"`)
	}

	if c.exitAfterFi {
		lines = append(lines, "          exit 1")
	}

	return auditJobStep{index: 1, name: "Download the Go modules", lines: lines}
}

// TestAuditGovulncheckVersionGuardHoldsTheBuiltGoItself pins the guard's
// comparison to go version's own output (round 2 of issue #658's review): a line
// that compared go.mod's Go with anything at all - the empty string above all -
// passed for holding the built tool to it, so the version check's three lines
// could be weakened to nothing and the test stay green. The go version line must
// assign what it prints to a variable of its own, and the comparison must name
// both that variable and go.mod's.
//
// It also pins the guard's block itself (round 3 of issue #658's review): the
// comparison must be the first code line after the go version line, and the
// ::error naming govulncheck and the exit 1 after it must be inside the block
// that if opens, before its fi. A guard checked against any later line of the
// step passed with its own exit 1 dropped: an unrelated exit 1 further down the
// same step stood in for it, and a mismatch only warned.
func TestAuditGovulncheckVersionGuardHoldsTheBuiltGoItself(t *testing.T) {
	t.Parallel()

	const (
		builtLine = `built="$(go version "${RUNNER_TEMP}/bin/govulncheck")"`
		bothLine  = `if [ "${built##* }" != "${want}" ]; then`
	)

	for _, tc := range []auditGovulncheckGuardCase{
		{
			name:       "the comparison names the built tool's Go and go.mod's",
			goVersion:  builtLine,
			comparison: bothLine,
		},
		{
			name:       "the comparison names go.mod's Go alone",
			goVersion:  builtLine,
			comparison: `if [ "${want}" != "" ]; then`,
			want:       1,
		},
		{
			name:       "the comparison names the built tool's Go alone",
			goVersion:  builtLine,
			comparison: `if [ "${built##* }" != "" ]; then`,
			want:       1,
		},
		{
			name:       "go version's output is held in no variable",
			goVersion:  `go version "${RUNNER_TEMP}/bin/govulncheck"`,
			comparison: bothLine,
			want:       1,
		},
		{
			name:       "an unrelated code line sits between the go version line and the comparison",
			goVersion:  builtLine,
			comparison: bothLine,
			between:    `echo "checking the built govulncheck"`,
			want:       1,
		},
		{
			name:         "the ::error sits after the block's fi, with an exit 1 after it",
			goVersion:    builtLine,
			comparison:   bothLine,
			errorAfterFi: true,
			exitAfterFi:  true,
			want:         1,
		},
		{
			name:        "the exit 1 sits after the block's fi",
			goVersion:   builtLine,
			comparison:  bothLine,
			exitAfterFi: true,
			want:        1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := auditGovulncheckVersionGuard(auditGovulncheckGuardStep(tc), "1.25.0")
			if len(problems) != tc.want {
				t.Errorf("auditGovulncheckVersionGuard(%q, %q, between %q, ::error after fi %t, exit 1 after fi %t) reported %d problem(s), want %d:\n%s",
					tc.goVersion, tc.comparison, tc.between, tc.errorAfterFi, tc.exitAfterFi, len(problems), tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// auditGoModReadMarkers are the words a line that reads go.mod's go line names:
// the go commands that print it, and the file itself (issue #687).
var auditGoModReadMarkers = []string{"go mod edit", "go list -m", "go.mod", "GoVersion"}

// auditReadsGoModsGo reports whether line runs a command that reads go.mod's go
// line: a command substitution, $(...), whose command names one of the markers
// above. A line that only prints the word go.mod, or that reads a variable such
// as GOMOD, runs no command and reads nothing (issue #687).
func auditReadsGoModsGo(line string) bool {
	sub := strings.Index(line, "$(")
	if sub < 0 {
		return false
	}

	for _, marker := range auditGoModReadMarkers {
		if strings.Contains(line[sub:], marker) {
			return true
		}
	}

	return false
}

// auditShellExpansion matches the first shell expansion of a value: the $( that
// opens a command substitution, whose command the rest of the line then holds,
// or a read of a variable, $name or ${name} (issue #687).
var auditShellExpansion = regexp.MustCompile(`\$\(|\$\{?[A-Za-z_][A-Za-z0-9_]*\}?`)

// auditSoleVariableRead matches an expansion that is one variable's value alone:
// $name or ${name}, with nothing before it and nothing after it (issue #687).
var auditSoleVariableRead = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)

// auditGoToolchainLiteral matches a Go toolchain version written out, go1.<n> or
// go1.<n>.<n>: the GOTOOLCHAIN=go<version> the checks job carried before issue
// #687, and any other copy of go.mod's go line in the workflow.
var auditGoToolchainLiteral = regexp.MustCompile(`go1\.[0-9]+(?:\.[0-9]+)?`)

// auditToolchainAssignment returns the value a line gives GOTOOLCHAIN, from an
// assignment at a word start to the end of the line, and whether it assigns it
// at all: the checks job's install line, or the line that writes the toolchain
// into the round's environment (issue #687).
func auditToolchainAssignment(line string) (string, bool) {
	const name = "GOTOOLCHAIN="

	for i := 0; i < len(line); i++ {
		if i > 0 && !strings.ContainsRune(" \t\"'", rune(line[i-1])) {
			continue
		}

		if rest, ok := strings.CutPrefix(line[i:], name); ok {
			return rest, true
		}
	}

	return "", false
}

// auditAssignmentValue returns the value a shell assignment gives, with the
// quotes around it trimmed.
func auditAssignmentValue(line string) string {
	_, rest, _ := strings.Cut(strings.TrimSpace(line), "=")

	return strings.Trim(strings.TrimSpace(rest), `"'`)
}

// auditToolchainDerivation returns what a GOTOOLCHAIN value lacks to name the Go
// go.mod's go line names, read at run time rather than written into the workflow
// (issue #687). The value is read as go<version>, the form go takes: the go
// prefix stands in the value itself, go$(...) and go${name}, or in the variable
// it reads, ${name} whose assignment starts with go. A literal version is no
// derivation, a command that reads another version than go.mod's is none either,
// and a variable no earlier line of the step sets from go.mod is a value that
// comes from nowhere. lines are the step's own lines and at is the index of the
// line that carries the value in them, so the assignment the value reads must be
// one of the lines above it. where names the step in every message.
func auditToolchainDerivation(value string, lines []string, at int, where, want string) []string {
	if literal := auditGoToolchainLiteral.FindString(value); literal != "" {
		return []string{fmt.Sprintf("%s gives GOTOOLCHAIN the literal %q: govulncheck is a Go program, so the go command that builds it decides which standard library its findings are about, and go.mod's go line is the one Go the module uses (%q). Derive it at run time instead, as GOTOOLCHAIN=\"go$(go mod edit -json | jq -r .Go)\", so go.mod holds the one version and no copy of it drifts (issue #687)", where, literal, want)}
	}

	loc := auditShellExpansion.FindStringIndex(value)
	if loc == nil {
		return []string{fmt.Sprintf("%s gives GOTOOLCHAIN the value %q, which runs no command and reads no variable: want go<version> derived from go.mod's go line, such as GOTOOLCHAIN=\"go$(go mod edit -json | jq -r .Go)\", never local or auto, which build the tool with whatever Go the runner has (issue #687)", where, strings.TrimSpace(value))}
	}

	var (
		problems []string
		prefix   = strings.Trim(strings.TrimSpace(value[:loc[0]]), `"'`)
		token    = auditShellExpansion.FindString(value)
	)

	if strings.HasPrefix(token, "$(") {
		if !auditReadsGoModsGo(value[loc[0]:]) {
			problems = append(problems, fmt.Sprintf("%s derives GOTOOLCHAIN's value with %q, which runs no command that reads go.mod's go line (go mod edit, go list -m, go.mod or the module's GoVersion): a toolchain read from anywhere else is a Go the module may not use, and go.mod's go line is where the one version lives (issue #687)", where, strings.TrimSpace(value[loc[0]:])))
		}

		if prefix != "go" {
			problems = append(problems, fmt.Sprintf("%s gives GOTOOLCHAIN %q, which does not read as the go<version> form go takes: write the derivation as go$(...), since a bare version such as 1.27.4 is no toolchain name go.mod's go line could be (issue #687)", where, strings.TrimSpace(value)))
		}

		return problems
	}

	name := ""
	if m := auditSoleVariableRead.FindStringSubmatch(token); m != nil {
		name = m[1]
	}

	if name == "" {
		return append(problems, fmt.Sprintf("%s gives GOTOOLCHAIN %q, whose expansion this check cannot read: want a command substitution that reads go.mod's go line, or a variable the step sets from it (issue #687)", where, strings.TrimSpace(value)))
	}

	assigned := -1

	for i := at - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := auditGoModGoAssignment.FindStringSubmatch(trimmed); m != nil && m[1] == name {
			assigned = i

			break
		}
	}

	switch {
	case assigned < 0:
		problems = append(problems, fmt.Sprintf("%s reads GOTOOLCHAIN's value from %s, a variable no earlier line of the step sets: the Go govulncheck is built with must be read from go.mod's go line in this step, as \"go$(go mod edit -json | jq -r .Go)\", and not come from nowhere (issue #687)", where, token))
	case !auditReadsGoModsGo(lines[assigned]):
		problems = append(problems, fmt.Sprintf("%s sets %s on %q, which runs no command that reads go.mod's go line (go mod edit, go list -m, go.mod or the module's GoVersion): the toolchain must be go.mod's own, read at run time (issue #687)", where, name, strings.TrimSpace(lines[assigned])))
	case prefix != "go" && !strings.HasPrefix(auditAssignmentValue(lines[assigned]), "go"):
		problems = append(problems, fmt.Sprintf("%s gives GOTOOLCHAIN %q from %q with no go prefix anywhere: go takes the go<version> form, so either the value or the variable must stand as go1.<n>, and a bare version is no toolchain name (issue #687)", where, strings.TrimSpace(value), strings.TrimSpace(lines[assigned])))
	}

	return problems
}

// auditGovulncheckToolchainFromGoMod returns what the checks job's install of
// govulncheck lacks to build the tool with the Go go.mod's go line names, read at
// run time (issue #687): the install must name GOTOOLCHAIN as a derivation from
// go.mod, which is either on its install line or written into the round's
// environment by a step of its own that the install then runs with. The version
// exists once, in go.mod, so no literal and no Renovate matchString keeps a copy
// of it in step.
func auditGovulncheckToolchainFromGoMod(steps []auditJobStep, want string) []string {
	step, at, line, ok := auditGovulncheckInstall(steps)
	if !ok {
		return []string{fmt.Sprintf("%s's checks job holds no go install of %s@: the job builds the tool its govulncheck rows run (issue #658)", auditWorkflow, auditGovulncheckModule)}
	}

	where := fmt.Sprintf("%s's checks job, step %q", auditWorkflow, step.name)

	value, assigned := auditToolchainAssignment(line)
	if assigned {
		return auditToolchainDerivation(value, step.lines, at, where, want)
	}

	// The job may read go.mod's go line once, in a step of its own, and hand
	// what it reads to the later steps through the environment.
	for _, other := range steps {
		for i, candidate := range other.lines {
			if strings.HasPrefix(strings.TrimSpace(candidate), "#") {
				continue
			}

			derived, ok := auditToolchainAssignment(candidate)
			if !ok || !strings.Contains(candidate, "GITHUB_ENV") {
				continue
			}

			return auditToolchainDerivation(derived, other.lines, i, fmt.Sprintf("%s's checks job, step %q", auditWorkflow, other.name), want)
		}
	}

	return []string{fmt.Sprintf("%s installs govulncheck with no GOTOOLCHAIN of its own, and no step of the job writes one into the round's environment: go install pkg@version picks the toolchain from the module's own go line, not go.mod's, and a govulncheck built with another Go than go.mod's %q cannot load the module at all. Give the install go.mod's Go as GOTOOLCHAIN=\"go$(go mod edit -json | jq -r .Go)\", or read it once and write it to the environment the install runs with (issue #687)", where, want)}
}

// auditGovulncheckToolchainCase is one case of
// TestAuditGovulncheckToolchainDerivationReadsGoMod: the lines of a step of its
// own that hands the job the toolchain, the lines of the install step above the
// install, the install line itself, and the count of problems the derivation must
// then report (issue #687).
type auditGovulncheckToolchainCase struct {
	name    string
	envStep string
	derive  []string
	install string
	want    int
}

// auditGovulncheckInstallWords are the words the install line carries after its
// GOTOOLCHAIN assignment: the install of the govulncheck module into the step's
// bin directory (issue #687).
const auditGovulncheckInstallWords = `GOBIN="${bin}" go install "` + auditGovulncheckModule + `@${GOVULNCHECK_VERSION}"`

// auditGovulncheckToolchainStep returns a checks job install step for a case: a
// step shaped like the workflow's own "Download the pinned tools and verify their
// checksums", with the case's derivation lines above its install line (issue
// #687).
func auditGovulncheckToolchainStep(c auditGovulncheckToolchainCase) auditJobStep {
	lines := []string{
		"      - name: Download the pinned tools and verify their checksums",
		"        run: |",
		"          set -euo pipefail",
		`          bin="${RUNNER_TEMP}/bin"`,
	}

	for _, line := range c.derive {
		lines = append(lines, "          "+line)
	}

	lines = append(lines, "          "+c.install)

	return auditJobStep{name: "Download the pinned tools and verify their checksums", lines: lines}
}

// auditGovulncheckToolchainEnvStep returns the step of its own a case may put
// before the install: a run block that writes the case's line into the round's
// environment, which the later steps run with (issue #687).
func auditGovulncheckToolchainEnvStep(line string) auditJobStep {
	return auditJobStep{name: "Read go.mod's Go", lines: []string{
		"      - name: Read go.mod's Go",
		"        run: |",
		"          set -euo pipefail",
		"          " + line,
	}}
}

// TestAuditGovulncheckToolchainDerivationReadsGoMod pins how the install's
// GOTOOLCHAIN value is read (issue #687): the Go govulncheck is built with is
// go.mod's, derived at run time from go.mod's go line, so the install accepts a
// command substitution over go.mod, inline or through a variable the step sets,
// and the go prefix go's own toolchain names need. A literal version, a command
// that reads another Go than go.mod's, a variable nothing sets from go.mod, and a
// value without the go prefix are the four ways the derivation stops being
// go.mod's Go, and each is reported.
func TestAuditGovulncheckToolchainDerivationReadsGoMod(t *testing.T) {
	t.Parallel()

	for _, tc := range []auditGovulncheckToolchainCase{
		{
			name:    "the install derives go.mod's Go inline",
			install: `GOTOOLCHAIN="go$(go mod edit -json | jq -r .Go)" ` + auditGovulncheckInstallWords,
		},
		{
			name:    "the step derives go.mod's Go into a variable the install reads",
			derive:  []string{`toolchain="go$(go list -m -f '{{.GoVersion}}')"`},
			install: `GOTOOLCHAIN="${toolchain}" ` + auditGovulncheckInstallWords,
		},
		{
			name:    "the go prefix stands in the value and the version in a variable",
			derive:  []string{`version="$(go mod edit -json | jq -r .Go)"`},
			install: `GOTOOLCHAIN="go${version}" ` + auditGovulncheckInstallWords,
		},
		{
			name:    "a step of its own writes the derivation into the round's environment",
			envStep: `echo "GOTOOLCHAIN=go$(go mod edit -json | jq -r .Go)" >> "${GITHUB_ENV}"`,
			install: auditGovulncheckInstallWords,
		},
		{
			name:    "the install names go.mod's Go as the literal version",
			install: `GOTOOLCHAIN=go1.27.4 ` + auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "a step of its own writes the literal version into the round's environment",
			envStep: `echo "GOTOOLCHAIN=go1.27.4" >> "${GITHUB_ENV}"`,
			install: auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "the install names no toolchain at all",
			install: auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "the install leaves the toolchain to whatever Go the runner has",
			install: "GOTOOLCHAIN=local " + auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "the derivation reads the go command's own version, not go.mod's",
			install: `GOTOOLCHAIN="go$(go env GOVERSION)" ` + auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "the derivation drops the go prefix go's toolchain names need",
			install: `GOTOOLCHAIN="$(go mod edit -json | jq -r .Go)" ` + auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "the variable the install reads is set nowhere",
			install: `GOTOOLCHAIN="${toolchain}" ` + auditGovulncheckInstallWords,
			want:    1,
		},
		{
			name:    "the variable the install reads is set from another source than go.mod",
			derive:  []string{`toolchain="go$(go env GOVERSION)"`},
			install: `GOTOOLCHAIN="${toolchain}" ` + auditGovulncheckInstallWords,
			want:    1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var steps []auditJobStep

			if tc.envStep != "" {
				steps = append(steps, auditGovulncheckToolchainEnvStep(tc.envStep))
			}

			steps = append(steps, auditGovulncheckToolchainStep(tc))

			problems := auditGovulncheckToolchainFromGoMod(steps, "1.25.0")
			if len(problems) != tc.want {
				t.Errorf("auditGovulncheckToolchainFromGoMod(env step %q, before %q, install %q) reported %d problem(s), want %d:\n%s",
					tc.envStep, tc.derive, tc.install, len(problems), tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestScheduledSecurityAuditWorkflowBuildsGovulncheckWithGoModsGo pins issue
// #658's first fix: the checks job installs govulncheck as a Go module, and the
// go command that builds it decides which standard library its findings are
// about. The install ran before the step that switches the job to go.mod's
// toolchain, so the tool was built with an older Go and could not load the
// module at all. Since issue #687 the install derives that Go from go.mod's go
// line at run time, so the workflow names no version of its own, and a step
// before the scan holds the built tool to go.mod's Go and fails the run when the
// two differ: a build that slipped back to another Go is a failure and not a scan
// whose findings are about a standard library the module does not use (ADR 0040).
func TestScheduledSecurityAuditWorkflowBuildsGovulncheckWithGoModsGo(t *testing.T) {
	t.Parallel()

	steps := auditJobSteps(auditJobLines(t, "checks"))
	wantGo := auditGoModGo(t)

	// The install derives the toolchain from go.mod at run time (issue #687):
	// the version exists once, in go.mod's go line, so neither the workflow nor
	// a Renovate matchString keeps a copy of it in step.
	t.Run("the install derives go.mod's Go from go.mod at run time", func(t *testing.T) {
		t.Parallel()

		for _, problem := range auditGovulncheckToolchainFromGoMod(steps, wantGo) {
			t.Error(problem)
		}
	})

	// No literal Go version anywhere: a GOTOOLCHAIN=go<version> line, a copy in
	// a comment or any other go1.<n> is a second version that only a hand keeps
	// in step with go.mod's go line (issue #687).
	t.Run("the workflow names no Go version of its own", func(t *testing.T) {
		t.Parallel()

		document := string(readRepositoryFile(t, auditWorkflow))

		for i, line := range strings.Split(document, "\n") {
			if literal := auditGoToolchainLiteral.FindString(line); literal != "" {
				t.Errorf("%s line %d names the Go toolchain %q: the audit's govulncheck is built with the Go go.mod's go line names, read at run time, so go.mod holds the one version and no copy of it can drift away from it. Derive it, as GOTOOLCHAIN=\"go$(go mod edit -json | jq -r .Go)\", rather than writing the version here (issue #687)\n  %s",
					auditWorkflow, i+1, literal, strings.TrimSpace(line))
			}
		}
	})

	// The value reaches the shell through go.mod and the step's own env: or a
	// file it writes, never through a ${{ }} expression: Actions expands that
	// before the step's shell runs, so what it names is no value read from
	// go.mod at run time (issue #687).
	t.Run("no ${{ }} expression hands the shell the toolchain", func(t *testing.T) {
		t.Parallel()

		for _, step := range steps {
			for _, line := range step.lines {
				if !strings.Contains(line, "GOTOOLCHAIN") || !strings.Contains(line, "${{") {
					continue
				}

				t.Errorf("%s's checks job, step %q, holds %q: a ${{ }} expression is expanded before the step's shell runs, so the toolchain it names is no value read from go.mod at run time. Read go.mod's go line in the step and pass the value on through env: or a file of the step's own (issue #687)",
					auditWorkflow, step.name, strings.TrimSpace(line))
			}
		}
	})

	// The step before the scan holds the built tool to go.mod's Go. Its
	// govulncheck -version line names the Go the go command reports for the tool,
	// never the Go that built it, so the step runs go version on the binary
	// itself, compares what that prints with go.mod's value, and fails the run
	// with an ::error naming govulncheck and an exit 1 when the two differ (issue
	// #658).
	t.Run("govulncheck's built Go is held to go.mod's Go before the scan", func(t *testing.T) {
		t.Parallel()

		scan, ok := auditGovulncheckScanStep(steps)
		if !ok {
			t.Fatalf("%s's checks job runs no govulncheck scan: a step that runs it with -format is the scan the version check guards", auditWorkflow)
		}

		for _, step := range steps {
			if step.index >= scan.index {
				break
			}

			if !auditRunsGovulncheckVersion(step) {
				continue
			}

			for _, problem := range auditGovulncheckVersionGuard(step, wantGo) {
				t.Error(problem)
			}

			return
		}

		t.Errorf("%s's checks job holds no step before its govulncheck scan (step %q) that runs govulncheck -version: the run must fail when the built tool's Go is not go.mod's, before it scans with it (issue #658)",
			auditWorkflow, scan.name)
	})
}

// auditTrivyFlagsCheckedAgainst is the trivy release the flag table below was
// read from. The test refuses to hold a workflow that pins another version to
// this table, so a Renovate bump of trivy is a failing test and not a scan that
// silently stops running (issue #658).
const auditTrivyFlagsCheckedAgainst = "v0.76.2"

// auditTrivySubcommandFlags is one trivy subcommand the workflow runs and the
// flags that release accepts for it.
type auditTrivySubcommandFlags struct {
	sub      string
	accepted map[string]bool
}

// auditFlagSet returns flags as a set of flag names, so a lookup of a flag the
// workflow passes reads as one.
func auditFlagSet(flags ...string) map[string]bool {
	set := make(map[string]bool, len(flags))

	for _, flag := range flags {
		set[flag] = true
	}

	return set
}

// auditTrivyAcceptedFlags holds, for each trivy subcommand the workflow runs,
// the flags trivy v0.76.2 accepts for it. The lists are that release's own
// reference, docs/guide/references/configuration/cli/trivy_image.md and
// trivy_sbom.md at tag v0.76.2: every flag the workflow passes, beside the
// neighbouring ones a scan of the audit's kind is likely to grow.
// --skip-check-update is trivy image's flag, for the Rego check bundle, and
// trivy sbom does not register it: the SBOM scan there is a vulnerability scan
// alone, and the flag made it exit 1 before it scanned (issue #658).
var auditTrivyAcceptedFlags = []auditTrivySubcommandFlags{
	{
		sub: "image",
		accepted: auditFlagSet(
			"--cache-dir", "--config", "--debug", "--quiet", "--timeout",
			"--exit-code", "--severity", "--ignore-unfixed",
			"--format", "--scanners", "--input", "--output", "--download-db-only",
			"--offline-scan", "--no-progress",
			"--skip-db-update", "--skip-java-db-update", "--skip-check-update", "--skip-version-check",
			"--disable-telemetry",
		),
	},
	{
		sub: "sbom",
		accepted: auditFlagSet(
			"--cache-dir", "--config", "--debug", "--quiet", "--timeout",
			"--exit-code", "--severity", "--ignore-unfixed",
			"--format", "--scanners", "--output", "--download-db-only",
			"--offline-scan", "--no-progress",
			"--skip-db-update", "--skip-java-db-update", "--skip-version-check",
			"--disable-telemetry",
		),
	},
}

// auditTrivyVersionPin matches the workflow's trivy pin: the Renovate comment
// that names the release, then the line that pins it. The attachment comment of
// the checksum names the same release and is no pin of its own, so the comment
// ends at trivy.
var auditTrivyVersionPin = regexp.MustCompile(`(?m)^[ \t]*# renovate: datasource=github-releases depName=aquasecurity/trivy[ \t]*\r?\n[ \t]*TRIVY_VERSION:[ \t]*"?(v?[0-9][^"\s]*)"?`)

// auditPinnedTrivyVersion returns the release the workflow pins trivy to, and
// fails the test when the workflow pins none.
func auditPinnedTrivyVersion(t *testing.T, document string) string {
	t.Helper()

	m := auditTrivyVersionPin.FindStringSubmatch(document)
	if m == nil {
		t.Fatalf("%s holds no TRIVY_VERSION pin under its Renovate comment: the flag table is keyed to the pinned release (issue #658)", auditWorkflow)
	}

	return m[1]
}

// auditTrivyRun matches one trivy invocation: the character before the command
// word, which bounds the word, the command word itself, which may be quoted and
// may be a path or a variable rather than the bare name, and then the
// subcommand.
var auditTrivyRun = regexp.MustCompile(`(^|[\s|(;])"?[^\s|;&()]*trivy"?[ \t]+([a-z][a-z0-9-]*)`)

// auditArrayDefinition matches a shell array's definition, name=(elements), as
// the step that expands it writes it.
var auditArrayDefinition = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_]*)=\((.*)\)[ \t]*$`)

// auditArrayExpansion matches a word that is one element-wise expansion of a
// shell array, ${name[@]}, quoted or not.
var auditArrayExpansion = regexp.MustCompile(`^"?\$\{([A-Za-z_][A-Za-z0-9_]*)\[@\]\}"?$`)

// auditYAMLHead matches a workflow line that is a YAML key or a sequence item
// rather than a line of a run block. The name of a step says what the scan is,
// as "trivy on the image and its SBOM" does, and names no command.
var auditYAMLHead = regexp.MustCompile(`^[ \t]*(-[ \t]|[A-Za-z_][A-Za-z0-9_-]*:[ \t])`)

// auditTrivyInvocation is one trivy invocation of the workflow: the line it runs
// on, its subcommand, the flags it hands that subcommand, the array expansions
// this check could not read, and the line itself for the message that reports a
// problem with it.
type auditTrivyInvocation struct {
	line       int
	sub        string
	flags      []string
	unresolved []string
	source     string
}

// auditShellWords splits a shell command's words at its spaces, keeping a
// quoted run together with its quotes, and stops at the word after which the
// command ends: a pipe, &&, || or ;, or a redirection, which the workflow's trivy
// lines use for their stderr.
func auditShellWords(command string) []string {
	var (
		out    []string
		word   strings.Builder
		quoted bool
	)

	flush := func() {
		if word.Len() == 0 {
			return
		}

		out = append(out, word.String())
		word.Reset()
	}

	for _, r := range command {
		switch {
		case r == '"' || r == '\'':
			quoted = !quoted
			word.WriteRune(r)
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		case !quoted && (r == ';' || r == '|' || r == '&' || r == '>'):
			flush()

			return out
		default:
			word.WriteRune(r)
		}
	}

	flush()

	return out
}

// auditTrivyInvocations returns every trivy invocation of the workflow, in its
// order, with the flags it hands the subcommand. A "${name[@]}" word is replaced
// by the words of that array's definition earlier in the same step, so a shared
// list of flags is read as the scan receives it; a word naming an array the step
// does not define is kept as unresolved, so a scan whose flags this check cannot
// see is reported rather than read as one with no flags.
func auditTrivyInvocations(document string) []auditTrivyInvocation {
	var (
		out    []auditTrivyInvocation
		arrays = map[string][]string{}
	)

	for i, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)

		// A step's arrays are its own: the run block that expands one defines it
		// in the same step.
		if strings.HasPrefix(line, "      - ") {
			arrays = map[string][]string{}
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || auditYAMLHead.MatchString(line) {
			continue
		}

		if m := auditArrayDefinition.FindStringSubmatch(line); m != nil {
			arrays[m[1]] = auditShellWords(m[2])

			continue
		}

		m := auditTrivyRun.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}

		inv := auditTrivyInvocation{line: i + 1, sub: line[m[4]:m[5]], source: trimmed}

		var words []string

		for _, word := range auditShellWords(line[m[1]:]) {
			expanded := auditArrayExpansion.FindStringSubmatch(word)
			if expanded == nil {
				words = append(words, word)

				continue
			}

			elements, ok := arrays[expanded[1]]
			if !ok {
				inv.unresolved = append(inv.unresolved, word)

				continue
			}

			words = append(words, elements...)
		}

		for _, word := range words {
			if !strings.HasPrefix(word, "-") {
				continue
			}

			flag, _, _ := strings.Cut(word, "=")
			inv.flags = append(inv.flags, flag)
		}

		out = append(out, inv)
	}

	return out
}

// auditTrivyFlagHint says which subcommand the flag belongs to, when another of
// the table's subcommands accepts it: the workflow's shared offline list is how a
// flag of one scan reached another (issue #658).
func auditTrivyFlagHint(sub, flag string) string {
	for _, other := range auditTrivyAcceptedFlags {
		if other.sub != sub && other.accepted[flag] {
			return fmt.Sprintf(": trivy %s accepts %s and trivy %s does not, so it belongs in the invocation that needs it, not in a list both scans share", other.sub, flag, sub)
		}
	}

	return ""
}

// TestScheduledSecurityAuditWorkflowPassesTrivyOnlyTheFlagsThePinAccepts pins
// issue #658's second fix: the offline flags the audit's two trivy scans share
// held --skip-check-update, which trivy image accepts and trivy sbom rejects as
// an unknown flag, so the SBOM half of row 10 exited 1 before it scanned and
// every later run lost it. Each subcommand's flags are held to the table of the
// flags the pinned release accepts for it, and the table is keyed to that
// release: a Renovate bump of trivy fails this test until the table is
// re-checked against the new release's help, so no bump can drop a scan (ADR
// 0040).
func TestScheduledSecurityAuditWorkflowPassesTrivyOnlyTheFlagsThePinAccepts(t *testing.T) {
	t.Parallel()

	document := string(readRepositoryFile(t, auditWorkflow))

	if version := auditPinnedTrivyVersion(t, document); version != auditTrivyFlagsCheckedAgainst {
		t.Fatalf("%s pins trivy %s, but the flag table was checked against %s: re-check each subcommand's flags against trivy %s --help (trivy %s image --help and trivy %s sbom --help) and update auditTrivyAcceptedFlags, so a flag the new release dropped, renamed or no longer registers for a subcommand is not a scan that silently stops running (issue #658)",
			auditWorkflow, version, auditTrivyFlagsCheckedAgainst, version, version, version)
	}

	invocations := auditTrivyInvocations(document)
	if len(invocations) == 0 {
		t.Fatalf("%s holds no trivy invocation: row 2 downloads the vulnerability database with it and row 10 scans the image and its SBOM with it (ADR 0040)", auditWorkflow)
	}

	known := map[string]bool{}

	for _, tc := range auditTrivyAcceptedFlags {
		known[tc.sub] = true
	}

	for _, inv := range invocations {
		for _, word := range inv.unresolved {
			t.Errorf("%s line %d runs trivy %s with %s, an array this check cannot read in that step, so it cannot see the flags the scan receives: define the array in the same step, or pass the flags on the line (issue #658)\n  %s",
				auditWorkflow, inv.line, inv.sub, word, inv.source)
		}

		if !known[inv.sub] {
			t.Errorf("%s line %d runs trivy %s, which the flag table does not know: add the subcommand with the flags trivy %s %s accepts (issue #658)",
				auditWorkflow, inv.line, inv.sub, auditTrivyFlagsCheckedAgainst, inv.sub)
		}
	}

	for _, tc := range auditTrivyAcceptedFlags {
		t.Run(tc.sub, func(t *testing.T) {
			t.Parallel()

			for _, inv := range invocations {
				if inv.sub != tc.sub {
					continue
				}

				for _, flag := range inv.flags {
					if tc.accepted[flag] {
						continue
					}

					t.Errorf("%s line %d passes %s to trivy %s, which trivy %s does not accept there%s: remove it from that scan's flags (issue #658)\n  %s",
						auditWorkflow, inv.line, flag, tc.sub, auditTrivyFlagsCheckedAgainst, auditTrivyFlagHint(tc.sub, flag), inv.source)
				}
			}
		})
	}
}
