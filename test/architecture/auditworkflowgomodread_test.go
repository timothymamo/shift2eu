package architecture_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// neither the read, a comment, nor part of a check that fails the run: the
// fragment runs in this test, so it may download and install nothing.
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
// reason deserves. Every non-comment line of the fragment runs here, so every
// one of them - the read, the bodies of the checks and an assignment of the
// fragment's own alike - is held to no download, install or build before the
// line-kind switch decides what it is (issue #710).
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

	// caseDepth counts the case blocks the walk below stands inside: a
	// branch's pattern, a branch's ;; and the esac that closes the block are
	// lines of a case block alone, and outside one each of them is a command
	// line this test would run (issue #710).
	caseDepth := 0

	for _, line := range fragment {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// The check runs on every line of the fragment, before the kind of
		// line is read: the read line, the if and case lines, and the echoes
		// run here too, so a second download hidden in one of them would
		// reach the network in the default tests, with only the system
		// paths on PATH (issue #710).
		if word := auditFragmentDownload(trimmed); word != "" {
			return nil, fmt.Sprintf(`%s's install step holds %q, which names %q, between its read of go.mod's Go and the install: this test runs that fragment, so no line of it - the read, a check line or an echo - may download, install or build anything (issue #710)`,
				auditWorkflow, trimmed, word)
		}

		if _, ok := auditGoModReadAssignment(line); ok {
			continue
		}

		if auditOpensCase(trimmed) {
			caseDepth++

			continue
		}

		// Inside a case block: its branch patterns, their ;;, the echoes
		// and exits of a branch that fails the run, and the esac that
		// closes the block.
		if caseDepth > 0 {
			if trimmed == "esac" {
				caseDepth--

				continue
			}

			if auditFragmentLineIsCaseBody(trimmed) || auditFragmentLineIsCheck(trimmed) {
				continue
			}

			return nil, fmt.Sprintf(`%s's install step holds %q between its read of go.mod's Go and the install, inside a case block: this test runs that fragment, so a case block may hold its patterns, their ;;, the echoes and exits that fail the run on what the read gave, and nothing else (issue #710)`,
				auditWorkflow, trimmed)
		}

		// Outside every case block a branch pattern's closing ), a lone ;; and
		// an esac are no part of a check: each is the shape of a case block's
		// own line and nothing else, so a line that looks like one here is the
		// command line it would run as (issue #710).
		if auditFragmentLineIsCaseBody(trimmed) {
			return nil, fmt.Sprintf(`%s's install step holds %q between its read of go.mod's Go and the install, and that line has the shape of a case block's own - a branch pattern that ends in ), a branch's ;; or the esac that closes it - while no case ... in line stands open above it: outside a case block such a line is a command line this test would run, with only the system paths on PATH, so the fragment may hold it only between a case ... in and its esac (issue #710)`,
				auditWorkflow, trimmed)
		}

		if auditFragmentLineIsCheck(trimmed) {
			continue
		}

		// A plain assignment of the fragment's own, such as a line count
		// kept in a variable, runs only its command substitution, which the
		// download check above has already held to no download or install.
		if auditGoModGoAssignment.MatchString(trimmed) {
			continue
		}

		return nil, fmt.Sprintf(`%s's install step holds %q between its read of go.mod's Go and the install: this test runs that fragment, so it may hold the read, the checks that fail the run on what it read, and the assignments they use, and nothing else (issue #710)`,
			auditWorkflow, trimmed)
	}

	return fragment, ""
}

// auditFragmentDownloads are the words a line of the fragment may not hold: the
// fragment runs under this test, so no line of it may download, install or
// build anything - with go's fetching verbs, git and gh beside the file
// downloads, since this test's PATH holds the system paths and the network is
// reachable from them (issue #710).
var auditFragmentDownloads = []string{
	"curl", "wget", "sha256sum", "mkdir", "sudo", "tar ", "install ", "rm ", "cp ", "mv ", "timeout ",
	"go install", "go build", "go mod download", "go get", "go run", "git ", "gh ",
}

// auditFragmentDownload returns the word of a line that would download, install
// or build something, and the empty string for a line that reads go.mod and
// checks what it read (issue #710).
func auditFragmentDownload(line string) string {
	for _, word := range auditFragmentDownloads {
		if auditFragmentNamesWord(line, word) {
			return word
		}
	}

	return ""
}

// auditFragmentNamesWord reports whether line holds word where word stands
// whole, with no letter, digit or underscore right before the word's first word
// byte or right after its last one: the read's go list -m names no go verb that
// fetches, and a word that merely ends in git says nothing about git (issue #710).
// A word that begins or ends with a space has no boundary to check there, so git
// names a line that goes on to name a subcommand.
func auditFragmentNamesWord(line, word string) bool {
	firstIsWord := auditFragmentIsWordByte(word, 0)
	lastIsWord := auditFragmentIsWordByte(word, len(word)-1)

	for at := 0; at < len(line); {
		found := strings.Index(line[at:], word)
		if found < 0 {
			return false
		}

		found += at

		before := !firstIsWord || !auditFragmentIsWordByte(line, found-1)
		after := !lastIsWord || !auditFragmentIsWordByte(line, found+len(word))

		if before && after {
			return true
		}

		at = found + 1
	}

	return false
}

// auditFragmentIsWordByte reports whether the byte at at of line is one word
// characters are made of, and false for the byte before the line and the one
// past its end (issue #710).
func auditFragmentIsWordByte(line string, at int) bool {
	if at < 0 || at >= len(line) {
		return false
	}

	c := line[at]

	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// auditOpensCase reports whether a trimmed shell line opens a case block: the
// word itself, or the case ... in form the fragment really takes (issue #710).
func auditOpensCase(trimmed string) bool {
	return trimmed == "case" || strings.HasPrefix(trimmed, "case ") || strings.HasPrefix(trimmed, "case\t")
}

// auditFragmentLineIsCaseBody reports whether a trimmed shell line is one of a
// case block's own lines, which the walk of the fragment lets through only
// between the case ... in that opens the block and the esac that closes it: a
// branch's pattern such as 1.[0-9]*) ends in its ), a branch ends in ;;, and the
// block ends in esac. Outside a case block each of them is no check but the
// command line it would run as, so the walk refuses it there (issue #710).
func auditFragmentLineIsCaseBody(trimmed string) bool {
	return trimmed == "esac" || trimmed == ";;" || strings.HasSuffix(trimmed, ";;") || strings.HasSuffix(trimmed, ")")
}

// auditFragmentLineIsCheck reports whether a trimmed shell line, outside every
// case block, is part of a check that fails the run on what the read gave: an if
// block - the line that opens one, its then, its body of echoes and exits, and
// the fi, elif or else that closes or continues it. A case block's own lines are
// no part of this, and the walk of the fragment lets them through only inside a
// case block (issue #710).
func auditFragmentLineIsCheck(trimmed string) bool {
	if auditOpensIf(trimmed) {
		return true
	}

	for _, word := range []string{"then", "fi", "else"} {
		if trimmed == word {
			return true
		}
	}

	for _, word := range []string{"elif ", "exit ", "echo ", "printf "} {
		if strings.HasPrefix(trimmed, word) {
			return true
		}
	}

	return false
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

// TestAuditGoModReadFragmentHoldsNoLineThatWouldDownload pins which lines of a
// step's script the read fragment may hold (issue #710): the test runs the whole
// fragment, so every non-comment line of it is held to no download, install or
// build - the read line and the lines of the checks included, not only an
// assignment of the fragment's own - and to the words that fetch on their own
// rather than inside another one. The lines of the check that tests the value's
// shape are let through: an if block anywhere in the fragment, and a case block's
// own lines - its patterns, their ;; and its esac - only between the case ... in
// that opens the block and the esac that closes it, since outside one such a line
// is the command line it would run as.
func TestAuditGoModReadFragmentHoldsNoLineThatWouldDownload(t *testing.T) {
	t.Parallel()

	const install = `GOTOOLCHAIN="go${gomod_go}" GOBIN="${bin}" go install "golang.org/x/vuln/cmd/govulncheck@v1.1.4"`

	for _, tc := range []struct {
		name    string
		script  []string
		refusal string // the word the refusal must name, for a fragment the test may not run
		reason  string // the part of the refusal that says why, for a line whose own words hold the word
	}{
		{
			name:   "the read and its checks",
			script: slices.Concat(auditGoModReadStep, []string{install}),
		},
		{
			name: "the read and a case block that tests the value's shape",
			script: slices.Concat(auditGoModReadStep, []string{
				`case "${gomod_go}" in`,
				`  1.[0-9]*) ;;`,
				`  *)`,
				`    echo "::error title=Audit checks::go.mod's go line is no Go version"`,
				`    exit 1`,
				`    ;;`,
				`esac`,
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
			refusal: "curl",
		},
		{
			name: "a read line that downloads as well",
			script: slices.Concat([]string{
				`gomod_go="$(GOWORK=off go list -m -f '{{.GoVersion}}'; curl -fsSL https://example.com/go)" || gomod_go=""`,
			}, auditGoModReadStep[1:], []string{install}),
			refusal: "curl",
		},
		{
			name: "an echo that downloads",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "::error title=Audit checks::$(curl -fsSL https://example.com/go)"`,
				install,
			}),
			refusal: "curl",
		},
		{
			name: "the read and the shape check the workflow runs",
			script: slices.Concat(auditGoModReadStep, []string{
				`if ! [[ "${gomod_go}" =~ ^1\.[0-9]+(\.[0-9]+)?$ ]]; then`,
				`  echo "::error title=Audit checks::go.mod's go line read as no Go version: see runbook step 11"`,
				`  exit 1`,
				`fi`,
				install,
			}),
		},
		{
			name: "a command line that ends in ) outside any case block",
			script: slices.Concat(auditGoModReadStep[:5], []string{
				`./script.sh $(true)`,
			}, auditGoModReadStep[5:], []string{install}),
			refusal: "./script.sh",
			reason:  "no case ... in line stands open above it",
		},
		{
			name: "a command line that quotes a substitution, outside any case block",
			script: slices.Concat(auditGoModReadStep[:5], []string{
				`./script.sh "$(true)"`,
			}, auditGoModReadStep[5:], []string{install}),
			refusal: "./script.sh",
		},
		{
			name:    "a ;; line outside any case block",
			script:  slices.Concat(auditGoModReadStep, []string{";;", install}),
			refusal: ";;",
			reason:  "no case ... in line stands open above it",
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
			refusal: "go mod download",
			reason:  `which names "go mod download"`,
		},
		{
			name: "a go get in an echo's command substitution",
			script: slices.Concat(auditGoModReadStep, []string{
				`echo "::error title=Audit checks::$(go get example.com/x@v1)"`,
				install,
			}),
			refusal: "go get",
			reason:  `which names "go get"`,
		},
		{
			name:    "a git fetch line",
			script:  slices.Concat(auditGoModReadStep, []string{`git fetch origin`, install}),
			refusal: "git ",
			reason:  `which names "git "`,
		},
		{
			name:    "a gh api line",
			script:  slices.Concat(auditGoModReadStep, []string{`gh api repos/example/repo`, install}),
			refusal: "gh ",
			reason:  `which names "gh "`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fragment, problem := auditGoModReadFragmentOf(tc.script)

			if tc.refusal == "" {
				if problem != "" {
					t.Fatalf("auditGoModReadFragmentOf(a step whose fragment holds the read and its checks) = %q, want the fragment this test runs: the checks that fail the run on what the read gave are no download, and the test runs them (issue #710)", problem)
				}

				if len(fragment) == 0 {
					t.Fatalf("auditGoModReadFragmentOf(a step whose fragment holds the read and its checks) returned no fragment: the test runs the step's own read and checks, never a copy of them (issue #710)")
				}

				return
			}

			if problem == "" {
				t.Fatalf("auditGoModReadFragmentOf(a step whose fragment holds %s) reported no problem, want the line refused with a message naming %q: this test runs the fragment with only the system paths on PATH, so no line of it may download, install or build anything (issue #710)", tc.refusal, tc.refusal)
			}

			if len(fragment) != 0 {
				t.Errorf("auditGoModReadFragmentOf(a step whose fragment holds %s) returned the fragment %q beside its problem %q, want none: a fragment the test may not run is no fragment to run (issue #710)", tc.refusal, fragment, problem)
			}

			if !strings.Contains(problem, tc.refusal) {
				t.Errorf("auditGoModReadFragmentOf(a step whose fragment holds %s) = %q, want a message naming %q: the refusal must say which line would reach the network (issue #710)", tc.refusal, problem, tc.refusal)
			}

			// A line whose own text holds the refusal word - a git
			// command, a pattern that ends in ) - is refused by the
			// message it gets as well, so the reason the case pins
			// stands in the message and not in the quoted line.
			if tc.reason != "" && !strings.Contains(problem, tc.reason) {
				t.Errorf("auditGoModReadFragmentOf(a step whose fragment holds %s) = %q, want a message saying %q: without it the line is refused as a command line the fragment may not hold, not as the line it is (issue #710)", tc.refusal, problem, tc.reason)
			}
		})
	}
}

// TestAuditFragmentDownloadNamesOnlyTheWordsThatFetch pins the words the
// fragment's download check refuses (issue #710): the verbs that fetch - go's
// download, get and run, with git and gh, beside the file downloads - each name a
// line the test may not run, while the words the read and its checks need, go
// list -m among them, name none. A word counts only where it stands whole, so a
// word that merely ends in git or begins with go get names no fetch.
func TestAuditFragmentDownloadNamesOnlyTheWordsThatFetch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{
			name: "the read the workflow really runs",
			line: `gomod_go="$(GOWORK=off go list -m -f '{{.GoVersion}}')" || gomod_go=""`,
		},
		{
			name: "a go mod download",
			line: `go mod download`,
			want: "go mod download",
		},
		{
			name: "a go get",
			line: `go get example.com/x@v1`,
			want: "go get",
		},
		{
			name: "a go run",
			line: `go run example.com/x@v1`,
			want: "go run",
		},
		{
			name: "a git fetch",
			line: `git fetch origin`,
			want: "git ",
		},
		{
			name: "a gh api call",
			line: `gh api repos/example/repo`,
			want: "gh ",
		},
		{
			name: "a word that ends in git inside another word",
			line: `echo digit fetch`,
		},
		{
			name: "a word beside gh",
			line: `echo sorghum tail`,
		},
		{
			name: "a word that begins with go get",
			line: `go getter`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := auditFragmentDownload(tc.line); got != tc.want {
				t.Errorf("auditFragmentDownload(%q) = %q, want %q: the fragment runs under this test with the system paths on PATH, so a line that fetches in full is refused for the word it names, and a word the read or its checks hold inside another one names no fetch (issue #710)",
					tc.line, got, tc.want)
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

	script := "#!/bin/sh\n" +
		"printf '%s' \"${GOWORK-none}\" > '" + filepath.Join(dir, "gowork") + "'\n" +
		body + "\n"

	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write the fake go command into %s: %v", dir, err)
	}

	// A shell script the fragment's PATH finds needs its execute bit.
	// The write itself stays at 0600, which gosec wants, and the mode comes
	// after it.
	if err := os.Chmod(path, os.FileMode(0o755)); err != nil {
		t.Fatalf("make the fake go command %s executable: %v", path, err)
	}
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
// module.
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
			name:   "a read that gives one line passes",
			fakeGo: auditFakeGoOneLine,
		},
		{
			name:   "a read that gives go.mod's minor-only version passes",
			fakeGo: auditFakeGoMinorOnly,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			auditWriteFakeGo(t, dir, tc.fakeGo)

			cmd := exec.CommandContext(t.Context(), bash, "-c", "set -euo pipefail\n"+strings.Join(fragment, "\n"))
			// Only what the fragment needs: the fake go command's
			// directory first on PATH, the system ones behind it for the
			// shell's own tools such as wc, and a HOME of the case. The
			// developer's environment, its go, its GOFLAGS and its
			// GOTOOLCHAIN reach neither the step nor the fake.
			cmd.Env = []string{
				"PATH=" + dir + ":/usr/bin:/bin",
				"HOME=" + t.TempDir(),
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
		})
	}
}
