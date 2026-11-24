// Pins pi's refusal of the commands that print the environment (CLAUDE.md
// rule 3, issue #540).
//
// The tester runs pi with --approve, so nothing asks it for permission: the
// only layer between it and an API key in its pane is the refusal pinned here.
// On 2026-11-24 a tester ran `env | grep` while looking for its scratchpad
// directory and printed DEEPSEEK_API_KEY, which then had to be rotated.
//
// The extension .pi/extensions/deny-env-printing.ts reads its patterns from
// .pi/deny-env-printing.json and refuses a bash tool call whose command matches
// any of them. The extension cannot run here, so the list is pinned as data and
// held to the two tables below: every command that prints the environment is
// refused, and no command the agents run routinely is.
//
// One property is out of reach of this test: pi matches the patterns with
// JavaScript's regular expressions, and a pattern Go accepts but JavaScript
// does not (a lookahead, a lookbehind, a backreference) would refuse nothing in
// pi and pass every test here. The patterns are therefore written to the
// narrowest common syntax (no lookaround, no backreference), which a reviewer
// has to hold; nothing here can.
package architecture_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
)

// The two files that make pi refuse a command: the pattern list the extension
// reads, and the extension itself.
const (
	piEnvDenyListPath  = ".pi/deny-env-printing.json"
	piEnvDenyExtension = ".pi/extensions/deny-env-printing.ts"
)

// piEnvDenyList is the document .pi/deny-env-printing.json holds: the reason a
// refusal gives the agent, so it can stop instead of looking for another way to
// read the environment, and the regular expressions that make one.
type piEnvDenyList struct {
	Reason   string   `json:"reason"`
	Patterns []string `json:"patterns"`
}

// TestPiEnvDenyListIsARefusalDocument pins the pattern list as a document: it
// decodes, it holds no key the extension does not read (a misspelt `pattern`
// would leave the extension with no pattern at all), it gives a reason and at
// least one pattern, and every pattern is a regular expression Go compiles.
func TestPiEnvDenyListIsARefusalDocument(t *testing.T) {
	t.Parallel()

	data := readRepositoryFile(t, piEnvDenyListPath)

	var list piEnvDenyList

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&list); err != nil {
		t.Fatalf("%s: %v", piEnvDenyListPath, err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		t.Fatalf("%s: the file holds more than one JSON value", piEnvDenyListPath)
	}

	if strings.TrimSpace(list.Reason) == "" {
		t.Errorf("%s: reason is empty, so a refused agent is told nothing about why (issue #540)", piEnvDenyListPath)
	}

	if len(list.Patterns) == 0 {
		t.Fatalf("%s: patterns is empty, so the extension refuses no command at all (issue #540)", piEnvDenyListPath)
	}

	for i, pattern := range list.Patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			t.Errorf("%s: patterns[%d] = %q is no regular expression: %v", piEnvDenyListPath, i, pattern, err)
		}
	}
}

// TestPiEnvDenyExtensionHooksTheToolCall pins the extension's wiring: it reads
// the pattern list by name and it hooks pi's `tool_call` event, the one that
// sees a bash command before the command runs. An extension that hooks no event
// (or another one) is loaded and refuses nothing. It also pins what a refusal is
// made of (a block with a reason), the file tools it holds beside bash, the
// `/proc` and `daemon.env` paths a file tool must not read, and the reason the
// fail-closed branch gives; and it keeps the extension off pi's package at run
// time, which it must not import: it is loaded before the tool call it guards,
// so a value import would make the guard depend on the very module whose version
// is the reason it exists (`import type` is erased and names the package alone).
// A `console.` call is refused too: the command it would log is the command that
// holds the key.
func TestPiEnvDenyExtensionHooksTheToolCall(t *testing.T) {
	t.Parallel()

	text := string(readRepositoryFile(t, piEnvDenyExtension))

	for _, want := range []struct {
		name string
		text string
	}{
		{name: "the pattern list", text: "deny-env-printing.json"},
		{name: "the tool_call event", text: "tool_call"},
		{name: "the block", text: "block: true"},
		{name: "the read tool", text: `"read"`},
		{name: "the grep tool", text: `"grep"`},
		{name: "the fail-closed reason", text: "could not be loaded"},
		{name: "the /proc path", text: "/proc"},
		{name: "the deck's environment file", text: "daemon.env"},
	} {
		if !strings.Contains(text, want.text) {
			t.Errorf("%s: the extension does not name %s (%q), so it refuses no command (issue #540)", piEnvDenyExtension, want.name, want.text)
		}
	}

	for _, statement := range piImportStatements(text) {
		if !strings.Contains(statement, piPackage) {
			continue
		}

		if !strings.HasPrefix(statement, "import type") {
			t.Errorf("%s: the statement %q imports %s for a value at run time, so the guard depends on the package it guards (issue #540); write `import type` instead", piEnvDenyExtension, statement, piPackage)
		}
	}

	if strings.Contains(text, "console.") {
		t.Errorf("%s: the extension calls console., and a command it logs is a command that may hold the key (issue #540)", piEnvDenyExtension)
	}
}

// piPackage is the package a pi extension may name only in a type-only import.
const piPackage = "@earendil-works/pi-coding-agent"

// piImportStatements returns the extension's import and require statements, each
// joined onto one line, so that a statement written over several lines is read
// whole: the package a multi-line import names sits on its closing line, where a
// line-at-a-time reader would not see the `import type` that makes it safe.
func piImportStatements(text string) []string {
	var statements []string

	var current strings.Builder

	joining := false

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)

		if !joining {
			if !strings.HasPrefix(line, "import") && !strings.Contains(line, "require(") {
				continue
			}

			// A statement that names its module on this line is whole.
			if !strings.HasPrefix(line, "import") || strings.Contains(line, " from ") || strings.HasSuffix(line, ";") {
				statements = append(statements, line)

				continue
			}

			joining = true
			current.Reset()
			current.WriteString(line)

			continue
		}

		current.WriteString(" ")
		current.WriteString(line)

		if strings.Contains(line, " from ") || strings.HasSuffix(line, ";") {
			statements = append(statements, current.String())

			joining = false
		}
	}

	if joining {
		statements = append(statements, current.String())
	}

	return statements
}

// TestPiEnvDenyListRefusesPrintingTheEnvironment pins what the patterns refuse:
// every form here prints, or reads as a file, values CLAUDE.md rule 3 keeps out
// of a pane, whatever the command does around it (a pipeline, a separator, a
// subshell, an absolute path to the binary).
func TestPiEnvDenyListRefusesPrintingTheEnvironment(t *testing.T) {
	t.Parallel()

	patterns := piEnvDenyPatterns(t)

	cases := []string{
		"env",
		"env | grep KEY",
		"env -0",
		"env | sort",
		"/usr/bin/env",
		"ls; env",
		"make check && env",
		"true || printenv",
		"(env)",
		"echo $(env)",
		"printenv",
		"printenv HOME",
		"printenv | grep -i key",
		"set",
		"set | grep KEY",
		"ls && set",
		"export",
		"export -p",
		"declare -p",
		"declare -x",
		"declare -px",
		"typeset -p",
		"compgen -v",
		"compgen -e",
		"cat /proc/self/environ",
		"tr '\\0' '\\n' < /proc/1/environ",
		"strings /proc/$$/environ",
		"xargs -0 < /proc/self/environ",
		// `env` with an assignment and no command prints instead of running a
		// command, and so does a leading assignment in front of a printer.
		"env FOO=bar",
		"env FOO=bar | grep KEY",
		"env A=1 printenv",
		"env X=1 env",
		"FOO=1 env",
		"FOO=1 printenv | grep KEY",
		"LC_ALL=C set",
		// A shell keyword around the printer: pi runs the command as it is given,
		// and bash runs the printer inside the keyword.
		"if true; then env; fi",
		"for i in 1; do printenv; done",
		"true && ! env",
		"{ env; }",
		// Wrappers that run another command as their argument or through exec.
		"timeout 5 env",
		"nice env",
		"stdbuf -oL env",
		"xargs -0 env",
		"sudo -E env",
		"command -p env",
		"time env",
		// Quoting and nesting: the printer is still the command bash runs.
		"bash -c env",
		"bash -c 'env'",
		`sh -c "printenv"`,
		"eval env",
		"echo `env`",
		// A newline separates commands as surely as `;` does.
		"make check\nenv",
		// The shell's other printers: the variables and exports by name, and
		// `export` itself, whose pipeline form prints them too.
		"compgen -A variable",
		"compgen -A export",
		"declare",
		"typeset",
		"export | grep KEY",
		"/bin/printenv",
		// ps prints a process's environment, and make prints the environment of
		// its targets.
		"ps eww $$",
		"ps auxeww",
		"ps e",
		"make -p",
		"make -pn",
		"make --print-data-base",
		// /proc holds the environment of every process, listed or read.
		"cd /proc/self && cat environ",
		"cat /proc/self/env*",
		"ls /proc/1",
		// The file the deck keeps the agents' keys in.
		"cat ~/.config/dot-agent-deck/daemon.env",
		"grep KEY /home/u/.config/dot-agent-deck/daemon.env",
		// A single key, expanded by name.
		"echo $DEEPSEEK_API_KEY",
		`printf %s "$GH_TOKEN"`,
		"echo ${AWS_SECRET_ACCESS_KEY}",
		// An interpreter that reads the environment itself, in the cheap form an
		// agent would type when the shell's printers are refused.
		`awk 'BEGIN{for(k in ENVIRON) print k}'`,
		`python3 -c 'import os; print(os.environ)'`,
		`node -e 'console.log(process.env)'`,
	}

	for _, command := range cases {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			if matched := piEnvDenyMatches(patterns, command); len(matched) == 0 {
				t.Errorf("no pattern of %s refuses the command %q, which prints values CLAUDE.md rule 3 keeps out of a pane (issue #540)", piEnvDenyListPath, command)
			}
		})
	}
}

// TestPiEnvDenyListAllowsTheWork pins what the patterns must leave alone: each
// command here reads no value of the environment, and a refusal of one of them
// would stop the agents' work. `env VAR=value cmd` is the case the patterns have
// to tell from bare `env`, and the assignments, options and arguments of `set`,
// `export` and `go env` are the cases they have to tell from the printing forms.
func TestPiEnvDenyListAllowsTheWork(t *testing.T) {
	t.Parallel()

	patterns := piEnvDenyPatterns(t)

	cases := []string{
		"make check",
		"make test-e2e",
		"go test ./...",
		"go env GOPATH",
		"git -C /x status",
		"git grep -n printenv",
		"grep -rn environment docs/",
		"gh pr view 1",
		"dot-agent-deck work-done --task x",
		"env GOTOOLCHAIN=local make check",
		"set -euo pipefail; make test",
		"set -e",
		"export FOO=bar",
		"cat internal/x/env.go",
		"make vm-env VM_ENV=X",
		"go test -run TestEnv ./...",
		"cd /x && make check",
		// `env` that runs a command: no argument holds an assignment, so it prints
		// nothing, and its options drop or set a variable for that command alone.
		"/usr/bin/env bash dev/x.sh",
		"env -u FOO make test",
		"env -C dir make",
		// ps for a process's start time and arguments, and its bare forms.
		"ps -o lstart= -p 123",
		"ps -o lstart=,args= -p 123",
		"ps -ef",
		"ps aux",
		// The commands whose message and body live in a scratch file.
		"git commit -F .scratch/msg.txt",
		"gh pr create --body-file .scratch/body.md",
		"dot-agent-deck work-done --task-file '.dot-agent-deck/x.md'",
		// make's options that print nothing.
		"make worktree BRANCH=chore/1-x",
		"make -C dir check",
		"make -j4 check",
		"GOTOOLCHAIN=local make check",
		"set -e; make check",
	}

	for _, command := range cases {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			for _, matched := range piEnvDenyMatches(patterns, command) {
				t.Errorf("the pattern %q of %s refuses the command %q, which reads no value of the environment, so the refusal would stop the agents' work (issue #540)", matched, piEnvDenyListPath, command)
			}
		})
	}
}

// piEnvDenyPatterns compiles the patterns of the extension's list, and fails the
// test when the list cannot be read or a pattern is no regular expression: the
// tables above would otherwise pass, refusing nothing, because no pattern
// compiled.
func piEnvDenyPatterns(t *testing.T) []*regexp.Regexp {
	t.Helper()

	data := readRepositoryFile(t, piEnvDenyListPath)

	var list piEnvDenyList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("%s: %v", piEnvDenyListPath, err)
	}

	patterns := make([]*regexp.Regexp, 0, len(list.Patterns))

	for _, pattern := range list.Patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("%s: %q is no regular expression: %v", piEnvDenyListPath, pattern, err)
		}

		patterns = append(patterns, re)
	}

	return patterns
}

// piEnvDenyMatches returns the patterns that match command, in the order the
// list writes them: the extension refuses command when this is not empty.
func piEnvDenyMatches(patterns []*regexp.Regexp, command string) []string {
	var matched []string

	for _, re := range patterns {
		if re.MatchString(command) {
			matched = append(matched, re.String())
		}
	}

	return matched
}
