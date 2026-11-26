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
// release accepts for it (ADR 0040).

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

// auditGovulncheckToolchainAnnotation is the Renovate comment the install line
// must sit under (CLAUDE.md, rule 7): the generic customManager's matchString
// that reads a GOTOOLCHAIN=go<version> line names the golang-version datasource
// and the depName go, so it moves the pin with go.mod's go line (issue #658).
const auditGovulncheckToolchainAnnotation = "# renovate: datasource=golang-version depName=go"

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

// auditNamesVariable reports whether line reads the shell variable name, in
// either of the shell's forms: "${name}" and "$name".
func auditNamesVariable(line, name string) bool {
	return strings.Contains(line, "${"+name+"}") || strings.Contains(line, "$"+name+" ") ||
		strings.Contains(line, "$"+name+"\"") || strings.Contains(line, "$"+name+"\t") ||
		strings.HasSuffix(strings.TrimSpace(line), "$"+name)
}

// auditComparisons are the shell's ways of comparing two words: the [ ] and
// [[ ]] operators of strings and of numbers. A line that only prints the built
// Go, or that assigns it, holds none of them.
var auditComparisons = []string{"!=", "-ne", "==", "-eq"}

// auditComparesVariable reports whether line compares something with the shell
// variable name: a comparison operator, and a read of the variable.
func auditComparesVariable(line, name string) bool {
	if !auditNamesVariable(line, name) {
		return false
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
// version from go.mod, runs go version on the govulncheck binary itself, compares
// what that prints with go.mod's Go, and fails the run - exit 1, after an ::error
// naming govulncheck that the run's log names the cause with - on a mismatch, so
// a govulncheck built with another Go than go.mod's stops the run instead of
// scanning with it (issue #658).
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
		compared := false

		for _, line := range lines[goVersion:] {
			if auditComparesVariable(line, modVar) {
				compared = true

				break
			}
		}

		if !compared {
			problems = append(problems, fmt.Sprintf("%s runs %q but no line after it compares what it prints with go.mod's Go (%q, the variable %q the step reads go.mod into): without that comparison the built tool's Go is never held to go.mod's %q, so a govulncheck built with another Go scans anyway (issue #658)", where, builtBy, "go"+want, modVar, want))
		}

		// The failure after it: an ::error that names govulncheck, then exit 1.
		namesCause, causeAt := false, -1

		for i, line := range lines[goVersion+1:] {
			if strings.Contains(line, "::error") && strings.Contains(line, "govulncheck") {
				namesCause, causeAt = true, goVersion+1+i

				break
			}
		}

		if !namesCause {
			problems = append(problems, fmt.Sprintf("%s runs go version on the govulncheck binary but holds no ::error after that line naming govulncheck: the run's log names the cause, so a govulncheck built with another Go is a failure that says so, not a bare non-zero exit (issue #658)", where))
		} else {
			fails := false

			for _, line := range lines[causeAt+1:] {
				if strings.Contains(line, "exit 1") {
					fails = true

					break
				}
			}

			if !fails {
				problems = append(problems, fmt.Sprintf("%s names the govulncheck cause in an ::error but no exit 1 follows it: the run must fail when the built tool's Go is not go.mod's, never scan with it (issue #658)", where))
			}
		}
	}

	return problems
}

// TestScheduledSecurityAuditWorkflowBuildsGovulncheckWithGoModsGo pins issue
// #658's first fix: the checks job installs govulncheck as a Go module, and the
// go command that builds it decides which standard library its findings are
// about. The install ran before the step that switches the job to go.mod's
// toolchain, so the tool was built with an older Go and could not load the
// module at all. The install must carry go.mod's Go itself as
// GOTOOLCHAIN=go<version> on its own line, under the Renovate comment that reads
// it, and a step before the scan must hold the built tool to go.mod's Go and fail
// the run when the two differ, so a build that slipped back to another Go is a
// failure and not a scan whose findings are about a standard library the module
// does not use (ADR 0040).
func TestScheduledSecurityAuditWorkflowBuildsGovulncheckWithGoModsGo(t *testing.T) {
	t.Parallel()

	steps := auditJobSteps(auditJobLines(t, "checks"))
	wantGo := auditGoModGo(t)
	wantToolchain := "GOTOOLCHAIN=go" + wantGo

	// The install line itself names go.mod's Go: a GOTOOLCHAIN anywhere else in
	// the step, an echo of it or a longer version such as go1.27.40, is no
	// toolchain the install runs with (issue #658).
	t.Run("the install names go.mod's Go on its own line, under its Renovate comment", func(t *testing.T) {
		t.Parallel()

		install, at, line, ok := auditGovulncheckInstall(steps)
		if !ok {
			t.Fatalf("%s's checks job holds no go install of %s@: the job builds the tool its govulncheck rows run", auditWorkflow, auditGovulncheckModule)
		}

		if rest, found := strings.CutPrefix(line, wantToolchain); !found || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			t.Errorf("%s's checks job installs govulncheck on %q, which does not start with %s followed by a space or tab: govulncheck is a Go program, so the go command that builds it decides which standard library its findings are about, and a govulncheck built with another Go cannot load the module at all. Give the install line itself %s, the Go go.mod's go line names, never a GOTOOLCHAIN elsewhere in the step and never a longer version (issue #658)",
				auditWorkflow, line, wantToolchain, wantToolchain)
		}

		above := ""

		for i := at - 1; i >= 0; i-- {
			if trimmed := strings.TrimSpace(install.lines[i]); trimmed != "" {
				above = trimmed

				break
			}
		}

		if above != auditGovulncheckToolchainAnnotation {
			t.Errorf("%s's checks job installs govulncheck under the line %q, want the Renovate annotation %q directly above it: the pin is a pinned version like any other, so a comment Renovate reads as the golang-version pin of depName go is what moves it with go.mod's go line, and a pin without one stays where it is while go.mod moves (CLAUDE.md rule 7, issue #658)",
				auditWorkflow, above, auditGovulncheckToolchainAnnotation)
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
