package architecture_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// TestScheduledSecurityAuditWorkflow holds .github/workflows/
// scheduled-security-audit.yml to check.AuditWorkflow (PRD #175 milestone 2, ADR
// 0040): one workflow the maintainer starts by hand on main, whose three jobs
// pass two ciphertexts between them and read each secret in the one place its
// work is done. The check reads the frame (the guard condition, the concurrency
// group, permissions, the environment and the runner), the hand-offs (age, the
// two artifacts and their retention), the exact reach of every secret and the
// variable, the App's token and its permission-* inputs, the model job's
// freedom from checks and containers, the hardening step before the sandbox
// preflight, the attestation check before the image digest is trusted, and the
// pins of every action and tool. The accepted shape is the check package's
// testdata/auditworkflow/valid.yml, every case of its TestAuditWorkflow a
// mutation of it.
func TestScheduledSecurityAuditWorkflow(t *testing.T) {
	t.Parallel()

	for _, message := range check.AuditWorkflow(readRepositoryFile(t, auditWorkflow)) {
		t.Error(message)
	}
}

// auditInstalledAge is age as each job's install step puts it in place: the
// binary that step downloaded, checked against its SHA-256 pin, installed under
// the job's own bin directory in $RUNNER_TEMP, and that directory appended to
// $GITHUB_PATH.
const auditInstalledAge = "${RUNNER_TEMP}/bin/age"

// auditEncryptStep names one of the workflow's two encrypt steps and the public
// key's variable that step encrypts to.
type auditEncryptStep struct {
	name      string
	job       string
	head      string
	recipient string
}

// auditEncryptSteps are both steps that turn a result into a ciphertext another
// job decrypts: the checks job's, to the model key, and the model job's, to the
// publish key. The model job's matters most: it runs right after the audit, in
// the one job the untrusted model's own session has run in.
var auditEncryptSteps = []auditEncryptStep{
	{
		name:      "checks-results",
		job:       "checks",
		head:      `      - name: Encrypt the checks' results to the model key`,
		recipient: `${AUDIT_MODEL_PUBLIC_KEY}`,
	},
	{
		name:      "model-result",
		job:       "model",
		head:      `      - name: Encrypt the audit's result to the publish key`,
		recipient: `${AUDIT_PUBLISH_PUBLIC_KEY}`,
	},
}

// TestScheduledSecurityAuditWorkflowEncryptsWithTheInstalledAge pins that each of
// those two steps runs the installed age by its absolute path, not the bare word
// age, which the step's shell resolves through PATH: the job's install step
// appends its bin directory to $GITHUB_PATH, so a bare age is whatever the first
// age on the PATH is, and the ciphertext alone leaves the job - a binary that is
// not the one checked against the pin, or a PATH another step wrote, encrypts
// what the next job is to treat as the result (ADR 0040, issues #615, #614). The
// model job's install step runs before the model session, so a session that
// appended to $GITHUB_PATH would pick the age its own encrypt step runs.
func TestScheduledSecurityAuditWorkflowEncryptsWithTheInstalledAge(t *testing.T) {
	t.Parallel()

	lines := strings.Split(string(readRepositoryFile(t, auditWorkflow)), "\n")

	for _, tc := range auditEncryptSteps {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			start := auditStepStart(lines, tc.head)
			if start < 0 {
				t.Fatalf("%s holds no %q step: the %s job's encrypt step is the one this case reads", auditWorkflow, strings.TrimSpace(tc.head), tc.job)
			}

			step := lines[start+1 : auditStepEnd(lines, start)]

			var encrypts []string

			for _, line := range step {
				trimmed := strings.TrimSpace(line)

				if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "--encrypt") {
					continue
				}

				encrypts = append(encrypts, trimmed)
			}

			if len(encrypts) != 1 {
				t.Fatalf("%s's %s job holds %d lines that encrypt in its encrypt step (%q), want the one age command that encrypts the result", auditWorkflow, tc.job, len(encrypts), encrypts)
			}

			// The command word: the quoted absolute path of the installed age,
			// or a literal absolute path. A bare name is resolved through PATH,
			// which $GITHUB_PATH fills, and is refused; so is a variable the step
			// does not set, which would expand to an empty or inherited value.
			word := strings.Trim(strings.Fields(encrypts[0])[0], `"'`)

			if word != auditInstalledAge && !strings.HasPrefix(word, "/") {
				t.Errorf("%s's %s job encrypts with %q, whose command word is %q: want age by its absolute path, %q, the binary the install step downloaded and checked against its pin, never a bare age or an unset variable the step's shell resolves through PATH (ADR 0040, issues #615, #614)",
					auditWorkflow, tc.job, encrypts[0], word, auditInstalledAge)
			}

			want := `--encrypt --recipient "` + tc.recipient + `"`

			if !strings.Contains(encrypts[0], want) {
				t.Errorf("%s's %s job encrypts with %q, want it to encrypt to the key its variable holds, %s", auditWorkflow, tc.job, encrypts[0], want)
			}
		})
	}
}

// auditStepStart returns the index of the step whose name line is head, or -1.
func auditStepStart(lines []string, head string) int {
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") == head {
			return i
		}
	}

	return -1
}

// auditStepEnd returns the index of the first line after start that ends the
// step's block: the job's next step, or the next job.
func auditStepEnd(lines []string, start int) int {
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(lines[i], "      - ") || indent <= 2 {
			return i
		}
	}

	return len(lines)
}

// auditModelToolsDir is the directory the model job's own tools live in: root-owned
// and mode 0555, beside the /srv/audit/bin wrapper and outside every directory the
// runner's user can write. The model's session runs between the step that installs
// those tools and the steps that run them, so a program in a directory the runner's
// user can write - ${RUNNER_TEMP}/bin, which the install step appended to
// $GITHUB_PATH, first on the PATH - is a program the session could plant: git, which
// actions/checkout's post step runs by PATH while its token is in the environment,
// or the age that encrypts the result, or the artifact check before it (finding M1
// of issue #614's review, ADR 0040).
const auditModelToolsDir = "/srv/audit/tools"

// auditModelTools are the programs the model job installs there and runs by that
// absolute path: the age that decrypts the checks' results and encrypts the result
// after the session, and main's launcher and artifact check.
var auditModelTools = []string{"age", "auditrun", "auditpublish"}

// auditModelToolRun is one run of a model-job tool as the job must write it: run
// matches the line whose command word is that tool's path in auditModelToolsDir,
// and its name is what the subtest and the message that reports it call the run.
type auditModelToolRun struct {
	name string
	run  *regexp.Regexp
}

// auditModelToolRuns are the model job's four runs of its three tools, each by its
// path in the root-owned directory and by no other word: the artifact check before
// the model session, the launcher in the model step, and age's decrypt and encrypt
// around it (round 2 of issue #614's review, ADR 0040). A line that names the
// installed path without running it - the hardening step's stat line, the strings
// it compares to - meets none of them, so a run that lost its path is reported
// here rather than held up by a name in a check.
var auditModelToolRuns = []auditModelToolRun{
	{
		name: "the artifact check, before the model session",
		run: regexp.MustCompile(`(?m)^[ \t]*if ! ` + regexp.QuoteMeta(auditModelToolsDir) +
			`/auditpublish artifacts -job model `),
	},
	{
		name: "the launcher, in the model step",
		run: regexp.MustCompile(`(?m)^[ \t]*if ! ` + regexp.QuoteMeta(auditModelToolsDir) +
			`/auditrun -claude-tarball `),
	},
	{
		name: "age's decrypt, before the model session",
		run: regexp.MustCompile(`\| ` + regexp.QuoteMeta(auditModelToolsDir) +
			`/age --decrypt --identity - `),
	},
	{
		name: "age's encrypt, after the model session",
		run: regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(auditModelToolsDir) +
			`/age --encrypt --recipient "`),
	},
}

// TestScheduledSecurityAuditWorkflowRunsTheModelToolsFromTheRootOwnedDirectory
// pins the model job's half of finding M1 of issue #614's review: the step that
// installs the job's tools is a step the model session does not control, and the
// directory it installs them into is one the session cannot write. Every tool is
// installed with `sudo install -o root -g root -m 0555 <source> /srv/audit/tools/<tool>`
// after the directory itself is made root-owned with mode 0555, is never installed
// from a directory the runner's user can write, and is run by that absolute path
// alone: not by the word the shell resolves through PATH, and not by a path under
// $RUNNER_TEMP, whose bin directory the steps before the model session appended to
// $GITHUB_PATH. The model job holds no $GITHUB_PATH write at all, in any form.
func TestScheduledSecurityAuditWorkflowRunsTheModelToolsFromTheRootOwnedDirectory(t *testing.T) {
	t.Parallel()

	lines := auditCodeLines(auditJobLines(t, "model"))

	t.Run("the tools' directory is root-owned and read-only", func(t *testing.T) {
		t.Parallel()

		want := "sudo install -d -o root -g root -m 0555 " + auditModelToolsDir

		for _, line := range lines {
			if strings.TrimSpace(line) == want {
				return
			}
		}

		t.Errorf("%s's model job holds no %q line: the directory its tools live in is made root-owned and mode 0555 before anything is installed there, or the session between them could replace a tool (ADR 0040, issue #614)", auditWorkflow, want)
	})

	for _, tool := range auditModelTools {
		t.Run("installed: "+tool, func(t *testing.T) {
			t.Parallel()

			target := auditModelToolsDir + "/" + tool

			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "sudo install -o root -g root -m 0555 ") && strings.HasSuffix(trimmed, " "+target) {
					return
				}
			}

			t.Errorf("%s's model job holds no \"sudo install -o root -g root -m 0555 <source> %s\" line: %s is installed root-owned with mode 0555 there, never into a directory the runner's user can write (ADR 0040, issue #614)", auditWorkflow, target, tool)
		})

		t.Run("run by its installed path: "+tool, func(t *testing.T) {
			t.Parallel()

			want := auditModelToolsDir + "/" + tool

			for _, line := range lines {
				for _, offender := range auditToolUses(line, tool, want) {
					t.Errorf("%s's model job runs %s as %q: want %q, the tool installed root-owned in a directory the model session cannot write, never a word or a path its shell resolves (ADR 0040, issue #614)", auditWorkflow, tool, offender, want)
				}
			}
		})
	}

	for _, r := range auditModelToolRuns {
		t.Run("run line: "+r.name, func(t *testing.T) {
			t.Parallel()

			for _, line := range lines {
				if r.run.MatchString(line) {
					return
				}
			}

			t.Errorf("%s's model job holds no line that runs %s by its path in %s %q: the install step puts it there and the steps that run it use that path alone, never a word, a relative path or a path under $RUNNER_TEMP the shell resolves (ADR 0040, issue #614)", auditWorkflow, r.name, auditModelToolsDir, r.run)
		})
	}

	t.Run("no $GITHUB_PATH write", func(t *testing.T) {
		t.Parallel()

		for _, line := range lines {
			if strings.Contains(line, "GITHUB_PATH") {
				t.Errorf("%s's model job holds %q: want no $GITHUB_PATH write at all, so no step of the session's own PATH names a program the steps after it run (ADR 0040, issue #614)", auditWorkflow, strings.TrimSpace(line))
			}
		}
	})
}

// auditToolSeparators are the shell's punctuation besides whitespace, and its
// quotes: a word ends at each of them, so a tool named after a ; or a |, as the
// right side of an &&, in a $(...) or between backticks, and a tool named in an
// assignment's value are each read as the word they are (round 2 of issue #614's
// review).
const auditToolSeparators = " \t\n\"';&|()<>=`"

// auditToolUses returns, for tool, every word of line that runs it as something
// other than want: the bare word, which the step's shell resolves through PATH, a
// relative path such as ci/auditrun or bin/age, which it resolves against the
// checkout, and any absolute path that names it, ${RUNNER_TEMP}/bin's and an
// assignment's value included. A line that installs, unpacks or builds the tool is
// no use of it: the install moves a file and the build names the package it
// compiles, and neither runs the tool.
func auditToolUses(line, tool, want string) []string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "sudo install ") || strings.HasPrefix(trimmed, "install ") ||
		strings.HasPrefix(trimmed, "tar ") || strings.Contains(trimmed, "go build ") {
		return nil
	}

	fields := strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(auditToolSeparators, r) })

	var uses []string

	for i, word := range fields {
		// A build's output is where the binary is made, not a program the
		// step runs.
		if i > 0 && fields[i-1] == "-o" {
			continue
		}

		switch {
		case word == want:
			continue
		case word == tool:
			uses = append(uses, word)
		case strings.HasSuffix(word, "/"+tool):
			uses = append(uses, word)
		}
	}

	return uses
}

// auditJobLines returns the lines of the job key, from its own "  <key>:" line to
// the line before the next job.
func auditJobLines(t *testing.T, key string) []string {
	t.Helper()

	lines := strings.Split(string(readRepositoryFile(t, auditWorkflow)), "\n")

	start := auditStepStart(lines, "  "+key+":")
	if start < 0 {
		t.Fatalf("%s holds no %s job: the job's own lines are what this check reads", auditWorkflow, key)
	}

	end := len(lines)

	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if indent <= 2 {
			end = i

			break
		}
	}

	return lines[start:end]
}

// auditCodeLines returns the lines that are no comment line: a comment names no
// command, and what a job's comments say about a tool is documentation.
func auditCodeLines(lines []string) []string {
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, line)
		}
	}

	return out
}

// TestScheduledSecurityAuditWorkflowDecryptsTheModelResultWithTheInstalledAge
// pins the publish job's half of the same finding: its decrypt step runs age too,
// and a bare age is whatever the first age on the PATH is. The publish job is a
// fresh runner with no untrusted step before that decrypt, so the install step's
// own bin directory in $RUNNER_TEMP is where its age goes, and the decrypt step
// runs that binary by its absolute path.
func TestScheduledSecurityAuditWorkflowDecryptsTheModelResultWithTheInstalledAge(t *testing.T) {
	t.Parallel()

	lines := strings.Split(string(readRepositoryFile(t, auditWorkflow)), "\n")

	start := auditStepStart(lines, `      - name: Decrypt the model's result`)
	if start < 0 {
		t.Fatalf("%s holds no publish job decrypt step: the step is the one this case reads", auditWorkflow)
	}

	step := lines[start+1 : auditStepEnd(lines, start)]

	var decrypts []string

	for _, line := range step {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "--decrypt") {
			continue
		}

		decrypts = append(decrypts, trimmed)
	}

	if len(decrypts) != 1 {
		t.Fatalf("%s's publish job holds %d lines that decrypt in its decrypt step (%q), want the one age command that decrypts the model's result", auditWorkflow, len(decrypts), decrypts)
	}

	// The command word: the first word after the last pipe, which is where the
	// private key on stdin comes from. A bare name is resolved through PATH and
	// is refused, as is a variable the step does not set.
	command := decrypts[0]
	piped := command[strings.LastIndex(command, "|")+1:]
	word := strings.Trim(strings.Fields(piped)[0], `"'`)

	if word != auditInstalledAge && !strings.HasPrefix(word, "/") {
		t.Errorf("%s's publish job decrypts with %q, whose command word is %q: want age by its absolute path, %q, the binary the install step downloaded and checked against its pin, never a bare age the step's shell resolves through PATH (ADR 0040, issues #615, #614)",
			auditWorkflow, command, word, auditInstalledAge)
	}
}

// auditPublishPathFlags are the flags of an auditpublish invocation whose value is
// a path: the result, the reports directory, the two programs the step runs and
// the run's temporary directory. -job is the one flag whose value is no path.
var auditPublishPathFlags = map[string]bool{
	"-result":   true,
	"-reports":  true,
	"-gh":       true,
	"-gitleaks": true,
	"-tmp":      true,
}

// auditInvocation is one auditpublish invocation of the workflow: the line it
// runs on, its subcommand, and its command line's words after the subcommand,
// each as the shell splits it, its quotes kept.
type auditInvocation struct {
	line   int
	sub    string
	words  []string
	source string
}

// TestScheduledSecurityAuditWorkflowNamesPublishPathsAbsolutely pins that every
// path flag of every auditpublish invocation of the workflow is absolute: a
// value that starts with /, ${RUNNER_TEMP}/ or ${GITHUB_WORKSPACE}/, quoted or
// not (issue #640).
//
// The reports directory is the one that matters: LoadPrevious refuses a
// directory that is not absolute and clean (publish.ErrConfig), so a relative
// -reports docs/security/audits fails every scheduled run before it reads a
// report, and the run's one line names the configuration alone. The step runs
// in the checkout, so the absolute form of that directory is the checkout's own,
// ${GITHUB_WORKSPACE}/docs/security/audits.
func TestScheduledSecurityAuditWorkflowNamesPublishPathsAbsolutely(t *testing.T) {
	t.Parallel()

	invocations := auditPublishInvocations(string(readRepositoryFile(t, auditWorkflow)))
	if len(invocations) == 0 {
		t.Fatalf("%s holds no auditpublish invocation: the workflow runs the publish program in its three jobs", auditWorkflow)
	}

	seen := map[string]bool{}

	for _, inv := range invocations {
		seen[inv.sub] = true

		if len(inv.words)%2 != 0 {
			t.Errorf("%s line %d: %s holds the words %q after its subcommand, want a flag and a value each: the line is %q",
				auditWorkflow, inv.line, inv.sub, inv.words, inv.source)

			continue
		}

		for i := 0; i < len(inv.words); i += 2 {
			flag, value := inv.words[i], inv.words[i+1]

			if flag == "-job" {
				continue
			}

			if !auditPublishPathFlags[flag] {
				t.Errorf("%s line %d: %s holds the flag %q, which this check does not know: the path flags are %v, so a new flag is classified here as a path or as no path",
					auditWorkflow, inv.line, inv.sub, flag, auditPublishPathFlags)

				continue
			}

			if !absoluteShellPath(value) {
				t.Errorf("%s line %d: %s gives %s the value %q, want an absolute path: one that starts with /, ${RUNNER_TEMP}/ or ${GITHUB_WORKSPACE}/, quoted or not (issue #640: the core refuses a relative path, so the run fails before it reads or publishes anything)\n  %s",
					auditWorkflow, inv.line, inv.sub, flag, value, inv.source)
			}
		}
	}

	if !seen["publish"] {
		t.Errorf("%s's auditpublish invocations %v hold no publish one: the publish job's step is the one -reports belongs to", auditWorkflow, invocations)
	}
}

// auditPublishRun matches one auditpublish invocation: the program, whose path
// may be quoted, then the subcommand, then its command line.
var auditPublishRun = regexp.MustCompile(`auditpublish"?\s+(artifacts|image|publish)\b(.*)$`)

// auditPublishInvocations returns every auditpublish invocation of the
// document, in its order. A comment line runs nothing, and a word that ends the
// command (a pipe, &&, ||, ; or a shell keyword) ends the invocation's words.
func auditPublishInvocations(document string) []auditInvocation {
	var out []auditInvocation

	for i, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		m := auditPublishRun.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}

		out = append(out, auditInvocation{
			line:   i + 1,
			sub:    m[1],
			words:  shellWordsUntilEnd(m[2]),
			source: trimmed,
		})
	}

	return out
}

// shellWordsUntilEnd splits a shell command's words at its spaces, keeping a
// double-quoted run together and its quotes, and stops at the word that ends
// the command: a pipe, &&, ||, ; or one of the shell's keywords.
func shellWordsUntilEnd(command string) []string {
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
		case r == '"':
			quoted = !quoted
			word.WriteRune(r)
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		case !quoted && (r == ';' || r == '|' || r == '&'):
			flush()

			return out
		default:
			word.WriteRune(r)
		}
	}

	flush()

	return out
}

// absoluteShellPath reports whether value is an absolute shell path, quoted or
// not: one that starts with /, ${RUNNER_TEMP}/ or ${GITHUB_WORKSPACE}/, or is one
// of the two variables whole (its own directory). The two variables are the
// runner's own absolute directories.
func absoluteShellPath(value string) bool {
	unquoted := strings.TrimSuffix(strings.TrimPrefix(value, `"`), `"`)
	unquoted = strings.TrimSuffix(strings.TrimPrefix(unquoted, `'`), `'`)

	for _, prefix := range []string{"/", "${RUNNER_TEMP}/", "${GITHUB_WORKSPACE}/"} {
		if strings.HasPrefix(unquoted, prefix) {
			return true
		}
	}

	return unquoted == "${RUNNER_TEMP}" || unquoted == "${GITHUB_WORKSPACE}"
}
