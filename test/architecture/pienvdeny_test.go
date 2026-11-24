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
// (or another one) is loaded and refuses nothing.
func TestPiEnvDenyExtensionHooksTheToolCall(t *testing.T) {
	t.Parallel()

	text := string(readRepositoryFile(t, piEnvDenyExtension))

	for _, want := range []struct {
		name string
		text string
	}{
		{name: "the pattern list", text: "deny-env-printing.json"},
		{name: "the tool_call event", text: "tool_call"},
	} {
		if !strings.Contains(text, want.text) {
			t.Errorf("%s: the extension does not name %s (%q), so it refuses no command (issue #540)", piEnvDenyExtension, want.name, want.text)
		}
	}
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
