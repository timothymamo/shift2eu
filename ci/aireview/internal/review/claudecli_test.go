package review_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// fakeOAuthToken is the subscription token the tests hand the CLI: an obvious
// fake, never a real credential (CLAUDE.md rule 3).
const fakeOAuthToken = "fake-oauth-token-123"

// untrustedMarker is the distinctive text of the tests' user prompt: it stands
// for the pull request's data, which must reach the CLI on stdin alone.
const untrustedMarker = "UNTRUSTED-PR-CONTENT-7f3a"

// cliSuccess is what the CLI prints for --output-format json when its turn ends
// well (ADR 0033).
const cliSuccess = `{"type":"result","subtype":"success","is_error":false,"result":"ANSWER TEXT"}`

// claudeFake is one fake CLI's behaviour: what it prints on each stream, how it
// exits, and, for the timeout test, a command it runs before it records
// anything. started, when set, is the path of a FIFO the fake writes one line
// to once it runs and then blocks on: the test reads that line as the signal
// that the CLI started, and no sleep or poll stands between the two.
type claudeFake struct {
	stdout  string
	stderr  string
	exit    int
	before  string
	started string
}

// claudeRecording is the directory the fake CLI records into: a directory of its
// own, so the recording names cannot collide with the fake's own files.
type claudeRecording struct {
	dir string
}

// newFakeClaudeCLI writes an executable fake `claude` that records what the
// reviewer handed it and then behaves as behaviour says, and returns its path
// and the directory it records into.
func newFakeClaudeCLI(t *testing.T, behaviour claudeFake) (string, claudeRecording) {
	t.Helper()

	rec := claudeRecording{dir: t.TempDir()}

	if err := os.WriteFile(filepath.Join(rec.dir, "canned-stdout"), []byte(behaviour.stdout), 0o600); err != nil {
		t.Fatalf("write the fake CLI's stdout: %v", err)
	}

	if err := os.WriteFile(filepath.Join(rec.dir, "canned-stderr"), []byte(behaviour.stderr), 0o600); err != nil {
		t.Fatalf("write the fake CLI's stderr: %v", err)
	}

	path := filepath.Join(t.TempDir(), "claude")

	writeExecutable(t, path, []byte(claudeFakeScript(rec.dir, behaviour)))

	return path, rec
}

// claudeFakeScript returns the text of the fake CLI: a shell script that records
// its arguments, its stdin, its environment, its working directory, the entries
// of its working directory and of $HOME, and the file its
// --system-prompt-file names, then prints its canned answer, exits as told.
// Every path it records into is baked in: the script reads nothing of the test
// process's environment but $HOME, which the reviewer sets. A fake told to
// signal its start writes one line to the FIFO that path names, which the test
// reads, and then replaces itself with a sleep the reviewer's cancellation
// kills: the run is blocked in the CLI, not in a test's timer.
func claudeFakeScript(rec string, behaviour claudeFake) string {
	return fmt.Sprintf(`#!/bin/sh
rec=%q
%s
for a in "$@"; do printf '%%s\n' "$a"; done > "$rec/args"
cat > "$rec/stdin"
env > "$rec/env"
pwd > "$rec/pwd"
ls -A > "$rec/cwdls"
ls -A "$HOME" > "$rec/homels"
prev=
for a in "$@"; do
	if [ "$prev" = "--system-prompt-file" ]; then cat "$a" > "$rec/system"; fi
	prev="$a"
done
%s
cat "$rec/canned-stdout"
cat "$rec/canned-stderr" >&2
exit %d
`, rec, behaviour.before, claudeStartSignal(behaviour.started), behaviour.exit)
}

// claudeStartSignal returns the script line that writes the start signal to the
// FIFO at path and blocks the fake until the reviewer kills it, or "" when path
// is empty. exec replaces the shell, so the process the reviewer kills is the
// sleep itself and the CLI's output pipes close with it.
func claudeStartSignal(path string) string {
	if path == "" {
		return ""
	}

	return fmt.Sprintf("printf 'started\\n' > %q\nexec sleep 60", path)
}

// read returns the text the fake CLI recorded under name, and fails the test
// when it recorded nothing: a missing file is what a run that never started
// leaves.
func (r claudeRecording) read(t *testing.T, name string) string {
	t.Helper()

	text, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		t.Fatalf("read the fake CLI's %s: %v; the reviewer did not run it as the test expects", name, err)
	}

	return string(text)
}

// args returns the arguments the fake CLI received, one per line, the empty
// argument included: the script writes a line for each.
func (r claudeRecording) args(t *testing.T) []string {
	t.Helper()

	recorded := r.read(t, "args")
	if recorded == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(recorded, "\n"), "\n")
}

// env returns the environment the fake CLI ran with, as name → value, reading
// the script's own env output. A value of the tests' fakes holds no line break,
// so a line splits into one name and one value.
func (r claudeRecording) env(t *testing.T) map[string]string {
	t.Helper()

	values := map[string]string{}

	for _, line := range strings.Split(r.read(t, "env"), "\n") {
		if line == "" {
			continue
		}

		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("the fake CLI's env holds the line %q, want name=value", line)
		}

		values[name] = value
	}

	return values
}

// TestClaudeCLIAskPresentsThePromptAsTheCLIReadsIt pins how the reviewer
// hands a review to the Claude Code CLI (ADR 0033): the trusted instructions in
// a file the --system-prompt-file flag names, the untrusted pull request on
// stdin alone, every tool refused, the model ADR 0033 names, and the CLI run in
// an empty directory of its own with an empty HOME, which the previous Messages
// API client did not need.
func TestClaudeCLIAskPresentsThePromptAsTheCLIReadsIt(t *testing.T) {
	t.Parallel()

	cli, rec := newFakeClaudeCLI(t, claudeFake{stdout: cliSuccess})

	prompt := review.Prompt{
		System: "the reviewer's instructions, trusted and whole",
		User:   review.UntrustedOpenTag + untrustedMarker + review.UntrustedCloseTag,
	}

	// The pin that the CLI asker is what Run takes (Asker), and not a type of
	// its own beside it.
	var asker review.Asker = review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second)

	got, err := asker.Ask(t.Context(), prompt)
	if err != nil {
		t.Fatalf("Ask: %v; the fake CLI's stderr: %s", err, rec.read(t, "canned-stderr"))
	}

	args := rec.args(t)

	t.Run("the answer is the CLI's result text", func(t *testing.T) {
		t.Parallel()

		if got != "ANSWER TEXT" {
			t.Errorf("Ask = %q, want the result the CLI printed, %q", got, "ANSWER TEXT")
		}
	})

	t.Run("the model is the one ADR 0033 names", func(t *testing.T) {
		t.Parallel()

		value, ok := valueAfter(args, "--model")
		if !ok {
			t.Fatalf("the CLI's arguments %q hold no --model, so it asks its own default model rather than %q", args, review.Model)
		}

		if value != review.Model {
			t.Errorf("--model is followed by %q, want %q", value, review.Model)
		}
	})

	t.Run("every tool, MCP server and slash command is refused", func(t *testing.T) {
		t.Parallel()

		values := map[string]string{
			"--model":           review.Model,
			"--output-format":   "json",
			"--tools":           "",
			"--setting-sources": "",
			// --strict-mcp-config reads a configuration it is never given unless
			// --mcp-config names one: the empty one is ADR 0040's model-job amendment, which
			// adds it for the audit and the reviewer alike (issue #277).
			"--mcp-config": `{"mcpServers":{}}`,
		}

		recorded := map[string]string{}

		for flag := range values {
			if value, ok := valueAfter(args, flag); ok {
				recorded[flag] = value
			}
		}

		if diff := cmp.Diff(values, recorded); diff != "" {
			t.Errorf("the CLI's arguments for the flags that take a value (-want +got):\n%s", diff)
		}

		for _, flag := range []string{"-p", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence"} {
			if !slices.Contains(args, flag) {
				t.Errorf("the CLI's arguments %q hold no %s; the reviewer runs the CLI once, non-interactively, with no MCP server, no slash command and no stored session (ADR 0033)", args, flag)
			}
		}
	})

	t.Run("the CLI runs in safe mode, which leaves CLAUDE.md, skills, plugins, hooks, MCP servers and custom agents off", func(t *testing.T) {
		t.Parallel()

		// --safe-mode (Claude Code 2.1.327) turns off CLAUDE.md, skills,
		// plugins, hooks, MCP servers and custom agents for the run, and keeps
		// the subscription's authentication. The empty working directory and
		// HOME the test above pins are not enough on their own.
		if !slices.Contains(args, "--safe-mode") {
			t.Errorf("the CLI's arguments %q hold no --safe-mode; the reviewer runs the CLI with CLAUDE.md, skills, plugins, hooks, MCP servers and custom agents off, so nothing of the checkout or the runner's home reaches the model (ADR 0033)", args)
		}
	})

	t.Run("the system prompt arrives in the file the flag names", func(t *testing.T) {
		t.Parallel()

		path, ok := valueAfter(args, "--system-prompt-file")
		if !ok {
			t.Fatalf("the CLI's arguments %q hold no --system-prompt-file, so the trusted instructions never reach it", args)
		}

		if !filepath.IsAbs(path) {
			t.Errorf("--system-prompt-file is %q, want an absolute path", path)
		}

		if got := rec.read(t, "system"); got != prompt.System {
			t.Errorf("the file --system-prompt-file names held %q, want the prompt's system %q", got, prompt.System)
		}

		if cwd := strings.TrimSuffix(rec.read(t, "pwd"), "\n"); filepath.Dir(path) == cwd {
			t.Errorf("the system prompt file %q sits in the CLI's working directory %q; the working directory is empty, so the file lives elsewhere", path, cwd)
		}
	})

	t.Run("the untrusted prompt arrives on stdin alone", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(prompt.User, untrustedMarker) {
			t.Fatalf("the test's user prompt %q holds no %s", prompt.User, untrustedMarker)
		}

		if got := rec.read(t, "stdin"); got != prompt.User {
			t.Errorf("the CLI's stdin held %q, want the prompt's user %q", got, prompt.User)
		}

		for _, arg := range args {
			if strings.Contains(arg, untrustedMarker) {
				t.Errorf("the CLI's argument %q holds the pull request's text; it reaches the CLI on stdin only, never on the command line (ADR 0033)", arg)
			}
		}
	})

	t.Run("the CLI runs in an empty directory that is not the test's", func(t *testing.T) {
		t.Parallel()

		cwd := strings.TrimSuffix(rec.read(t, "pwd"), "\n")
		if cwd == "" {
			t.Fatal("the fake CLI recorded no working directory")
		}

		own, err := os.Getwd()
		if err != nil {
			t.Fatalf("the test's working directory: %v", err)
		}

		if cwd == own {
			t.Errorf("the CLI ran in %q, the test's own working directory; it runs in a directory of its own, so it can read nothing of main's checkout", cwd)
		}

		if entries := rec.read(t, "cwdls"); entries != "" {
			t.Errorf("the CLI's working directory %q holds %q, want nothing: it is empty", cwd, entries)
		}
	})

	t.Run("the CLI's HOME is an empty directory", func(t *testing.T) {
		t.Parallel()

		// The test process's own HOME is never read here: forbidigo's
		// ai-reviewer-env group refuses os.Getenv in this package, a test
		// included, and the same group keeps the library from locating a home
		// at all. An empty HOME is the observable: the test process's home
		// holds files, a fresh one holds none.
		home := rec.env(t)["HOME"]
		if home == "" {
			t.Fatalf("the CLI ran without HOME; it needs an empty home of its own")
		}

		if !filepath.IsAbs(home) {
			t.Errorf("the CLI's HOME is %q, want an absolute path", home)
		}

		if entries := rec.read(t, "homels"); entries != "" {
			t.Errorf("the CLI's HOME %q holds %q, want nothing: it is empty, so the CLI reads no credentials file of the runner's", home, entries)
		}

		if cwd := strings.TrimSuffix(rec.read(t, "pwd"), "\n"); home == cwd {
			t.Errorf("the CLI's HOME is its working directory %q; the two are separate, so neither holds the system prompt file", cwd)
		}
	})
}

// TestClaudeCLIAskGivesTheCLIAnExplicitEnvironment pins the CLI's environment
// (ADR 0033): an allow list of its own, with the subscription token, HOME and
// PATH, and nothing inherited. An inherited ANTHROPIC_API_KEY would switch
// Claude Code from the subscription to pay-as-you-go billing.
func TestClaudeCLIAskGivesTheCLIAnExplicitEnvironment(t *testing.T) {
	// t.Setenv, so no t.Parallel: the test changes the process's environment,
	// which every parallel test of it shares.
	t.Setenv("ANTHROPIC_API_KEY", "fake-api-key")
	t.Setenv("SOME_OTHER_VAR", "x")

	cli, rec := newFakeClaudeCLI(t, claudeFake{stdout: cliSuccess})

	if _, err := review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second).Ask(t.Context(), review.Prompt{System: "s", User: "u"}); err != nil {
		t.Fatalf("Ask: %v; the fake CLI's stderr: %s", err, rec.read(t, "canned-stderr"))
	}

	env := rec.env(t)

	// The shell adds these three of its own; they say nothing about what the
	// reviewer passed.
	for _, added := range []string{"PWD", "SHLVL", "_"} {
		delete(env, added)
	}

	allowed := []string{
		"CLAUDE_CODE_OAUTH_TOKEN",
		"HOME",
		"PATH",
		"DISABLE_AUTOUPDATER",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
		"DISABLE_TELEMETRY",
		"DISABLE_ERROR_REPORTING",
	}

	for _, name := range slices.Sorted(maps.Keys(env)) {
		if !slices.Contains(allowed, name) {
			t.Errorf("the CLI's environment holds %s, which is not on the reviewer's allow list %v: the CLI inherits nothing of the runner's environment (ADR 0033)", name, allowed)
		}
	}

	for _, refused := range []string{"ANTHROPIC_API_KEY", "SOME_OTHER_VAR"} {
		if _, ok := env[refused]; ok {
			t.Errorf("the CLI's environment holds %s from the test process; the reviewer passes an explicit allow list, so no variable of the runner's reaches the CLI (ADR 0033)", refused)
		}
	}

	if got := env["CLAUDE_CODE_OAUTH_TOKEN"]; got != fakeOAuthToken {
		t.Errorf("CLAUDE_CODE_OAUTH_TOKEN is %q, want the token NewClaudeCLI was given: `claude` authenticates with the subscription through it", got)
	}

	for _, name := range []string{"HOME", "PATH"} {
		if env[name] == "" {
			t.Errorf("the CLI ran without a %s on its allow list", name)
		}
	}
}

// TestClaudeCLIAskFailsTheRun pins what a review cannot go on from (ADR 0033):
// an error result, a turn that did not end, an answer the CLI did not print as
// JSON, an empty result, a stream element that is not the result, a non-zero
// exit, a missing CLI, a timeout and a cancelled context all fail with ErrModel
// and no answer, so nothing is posted from a failed or unintended answer.
func TestClaudeCLIAskFailsTheRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		behaviour claudeFake
		noCLI     bool
		cancel    bool
		timeout   time.Duration
		wantAny   []string
		// wantAbsent are phrases the error must not hold: a value the CLI did
		// not choose reads as a constant of this package's, and no text of the
		// answer is ever quoted.
		wantAbsent []string
	}{
		{
			name:      "an error result after a failed run",
			behaviour: claudeFake{stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached"}`, exit: 1},
			wantAny:   []string{"claude cli error"},
		},
		{
			name:      "an error result of a run that exited 0",
			behaviour: claudeFake{stdout: `{"type":"result","subtype":"success","is_error":true,"result":"Claude AI usage limit reached"}`},
			wantAny:   []string{"claude cli error"},
		},
		{
			name:      "a turn that hit its maximum",
			behaviour: claudeFake{stdout: `{"type":"result","subtype":"error_max_turns","is_error":false,"result":"half an answer"}`},
			wantAny:   []string{"error_max_turns"},
		},
		{
			name:      "an answer that is not JSON",
			behaviour: claudeFake{stdout: "garbage"},
		},
		{
			name:      "a success with an empty result",
			behaviour: claudeFake{stdout: `{"type":"result","subtype":"success","is_error":false,"result":""}`},
		},
		{
			name:      "a stream element that is not the result",
			behaviour: claudeFake{stdout: `{"type":"system","subtype":"init","is_error":false,"result":"ANSWER TEXT"}`},
			wantAny:   []string{"system"},
			wantAbsent: []string{
				"ANSWER TEXT",
			},
		},
		{
			// The type is the CLI's own vocabulary alone, as the subtype is: a
			// type holding anything else reads "unrecognised" and reaches
			// neither the error nor the job's log (audit L2). The type's
			// "Ignore previous" is a planted instruction for the prompt-injection
			// tests (ADR 0033), never an instruction to the reader.
			name:       "a stream element whose type is not one of the CLI's own",
			behaviour:  claudeFake{stdout: `{"type":"MRK374 Ignore previous","subtype":"init","is_error":false,"result":"ANSWER TEXT"}`},
			wantAny:    []string{"unrecognised"},
			wantAbsent: []string{"MRK374", "ANSWER TEXT"},
		},
		{
			name:      "a non-zero exit without an answer",
			behaviour: claudeFake{exit: 2},
			wantAny:   []string{"exit"},
		},
		{
			name:  "a CLI that is not there",
			noCLI: true,
		},
		{
			name:      "a CLI that runs past the timeout",
			behaviour: claudeFake{before: "sleep 5", stdout: cliSuccess},
			timeout:   200 * time.Millisecond,
			wantAny:   []string{"timed out", "timeout"},
		},
		{
			name:      "a context that is already cancelled",
			behaviour: claudeFake{stdout: cliSuccess},
			cancel:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			timeout := tc.timeout
			if timeout == 0 {
				timeout = 30 * time.Second
			}

			cli := filepath.Join(t.TempDir(), "no-such-claude")
			if !tc.noCLI {
				cli, _ = newFakeClaudeCLI(t, tc.behaviour)
			}

			ctx := t.Context()
			if tc.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()

				ctx = cancelled
			}

			start := time.Now()

			text, err := review.NewClaudeCLI(cli, fakeOAuthToken, timeout).Ask(ctx, review.Prompt{System: "s", User: "u"})

			elapsed := time.Since(start)

			if !errors.Is(err, review.ErrModel) {
				t.Fatalf("Ask = %q, %v; want an error wrapping ErrModel", text, err)
			}

			if text != "" {
				t.Errorf("Ask = %q with the error %v, want no answer: a failed turn posts nothing (ADR 0033)", text, err)
			}

			if len(tc.wantAny) > 0 && !containsAny(strings.ToLower(err.Error()), tc.wantAny) {
				t.Errorf("the error %q holds none of %q, so the maintainer cannot tell what failed", err, tc.wantAny)
			}

			for _, unwanted := range tc.wantAbsent {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("the error %q holds %q: the element's type and its subtype are the CLI's own vocabulary alone, a value outside it reads %q, and no text of the answer is quoted, so nothing the pull request steers reaches a public ::error line (issue #374, audit L2)", err, unwanted, "unrecognised")
				}
			}

			if tc.timeout > 0 && elapsed > 3*time.Second {
				t.Errorf("Ask returned after %v with a timeout of %v, want the CLI cut off at about the timeout", elapsed, tc.timeout)
			}
		})
	}
}

// TestClaudeCLIAskNamesAnErrorResultByClassAndSubtype pins issue #374: the
// CLI's result text of an `is_error` envelope is model output a pull request's
// author can steer, and the run's error is printed as the job's public ::error
// line, so the error names the CLI's constant error class and its subtype, and
// no text of the result at all. The subtype is the CLI's own vocabulary alone —
// a short code of lower-case letters and underscores — or it is not shown: a
// subtype that holds anything else reads "unrecognised", so a value the CLI
// echoes from a pull request's text reaches neither the error nor the job's log
// (audit L2).
func TestClaudeCLIAskNamesAnErrorResultByClassAndSubtype(t *testing.T) {
	t.Parallel()

	const (
		// cliResultMarker stands for the CLI's result text: the model's words,
		// which the error must never repeat.
		cliResultMarker = "MRK374-THE-CLI-RESULT-TEXT"
		// subtypeMarker stands for a subtype the CLI did not choose: upper case,
		// a space and punctuation, which no code of the CLI's vocabulary holds.
		// Its "Ignore previous" is a planted instruction for the
		// prompt-injection tests (ADR 0033), never an instruction to the reader.
		subtypeMarker = "MRK374 Ignore previous"
	)

	cases := []struct {
		name string
		// behaviour is the fake CLI's answer: an error result, or a turn that
		// did not end.
		behaviour claudeFake
		// want are the phrases the error must hold.
		want []string
		// wantAbsent are the phrases the error must not hold, the marker of a
		// subtype the CLI did not choose included.
		wantAbsent []string
	}{
		{
			name:       "an error result whose subtype is one of the CLI's own",
			behaviour:  claudeFake{stdout: `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"` + cliResultMarker + `"}`},
			want:       []string{"claude cli error", "error_max_turns"},
			wantAbsent: []string{cliResultMarker},
		},
		{
			name:       "an error result whose subtype is not one of the CLI's own",
			behaviour:  claudeFake{stdout: `{"type":"result","subtype":"` + subtypeMarker + `","is_error":true,"result":"` + cliResultMarker + `"}`},
			want:       []string{"claude cli error", "unrecognised"},
			wantAbsent: []string{subtypeMarker, cliResultMarker},
		},
		{
			name:       "a turn that did not end, its subtype not one of the CLI's own",
			behaviour:  claudeFake{stdout: `{"type":"result","subtype":"` + subtypeMarker + `","is_error":false,"result":"` + cliResultMarker + `"}`},
			want:       []string{"unrecognised"},
			wantAbsent: []string{subtypeMarker, cliResultMarker},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli, _ := newFakeClaudeCLI(t, tc.behaviour)

			text, err := review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second).Ask(t.Context(), review.Prompt{System: "s", User: "u"})
			if !errors.Is(err, review.ErrModel) {
				t.Fatalf("Ask = %q, %v; want an error wrapping ErrModel: the CLI did not answer, so the run fails (ADR 0033)", text, err)
			}

			if text != "" {
				t.Errorf("Ask = %q with the error %v, want no answer: a failed turn posts nothing (ADR 0033)", text, err)
			}

			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Ask = %v, which does not name %q: the ::error line names a constant class and the CLI's subtype, so the failure is diagnosable without its text (issue #374)", err, want)
				}
			}

			for _, unwanted := range tc.wantAbsent {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("Ask = %v, which holds %q: the result text is model output a pull request's author can steer, and a subtype outside the CLI's own vocabulary is replaced by the constant %q, so nothing the CLI did not choose reaches a public ::error line (issue #374, audit L2)", err, unwanted, "unrecognised")
				}
			}
		})
	}
}

// TestClaudeCLIAskKeepsTheTokenOutOfTheError pins that a CLI that echoes the
// subscription token, on stdout and on stderr, puts nothing in the run's error:
// a failing check's annotation is public (ADR 0033, CLAUDE.md rule 3).
func TestClaudeCLIAskKeepsTheTokenOutOfTheError(t *testing.T) {
	t.Parallel()

	cli, _ := newFakeClaudeCLI(t, claudeFake{
		stdout: `{"type":"result","subtype":"success","is_error":true,"result":"bad ` + fakeOAuthToken + `"}`,
		stderr: "and " + fakeOAuthToken + " on stderr\n",
		exit:   1,
	})

	_, err := review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second).Ask(t.Context(), review.Prompt{System: "s", User: "u"})
	if !errors.Is(err, review.ErrModel) {
		t.Fatalf("Ask = %v, want an error wrapping ErrModel", err)
	}

	if strings.Contains(err.Error(), fakeOAuthToken) {
		t.Errorf("the error %q prints the token; every text the CLI wrote is redacted, so no credential reaches a public check's annotation (CLAUDE.md rule 3)", err)
	}
}

// TestClaudeCLIAskCapsTheCLIErrorText pins that a CLI failure of any size fails
// the run with a short error: a 10,000-character result stays far below the
// annotation GitHub accepts, and the message names the failure rather than
// burying it.
func TestClaudeCLIAskCapsTheCLIErrorText(t *testing.T) {
	t.Parallel()

	cli, _ := newFakeClaudeCLI(t, claudeFake{
		stdout: `{"type":"result","subtype":"success","is_error":true,"result":"` + strings.Repeat("x", 10_000) + `"}`,
		exit:   1,
	})

	_, err := review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second).Ask(t.Context(), review.Prompt{System: "s", User: "u"})
	if !errors.Is(err, review.ErrModel) {
		t.Fatalf("Ask = %v, want an error wrapping ErrModel", err)
	}

	if len(err.Error()) >= 2000 {
		t.Errorf("the error is %d bytes, want fewer than 2000: the CLI's text is capped, so one failing run writes one readable annotation (ADR 0033)", len(err.Error()))
	}
}

// awaitFakeCLIStart blocks until the fake CLI has written its one start line to
// the FIFO at fifo, and fails the test when the CLI never started. The read is
// bounded, so a run that never starts the CLI fails the test instead of
// blocking it for ever; the bound is no synchronisation, the FIFO's one line is
// (issue #303). Both tests that reach a run's mid-CLI arm share it, so the
// handshake is written once.
func awaitFakeCLIStart(t *testing.T, fifo string) {
	t.Helper()

	started := make(chan error, 1)

	go func() {
		f, err := os.Open(fifo)
		if err != nil {
			started <- fmt.Errorf("open the fake CLI's start signal: %w", err)

			return
		}
		defer f.Close()

		line, err := bufio.NewReader(f).ReadString('\n')
		if err != nil {
			started <- fmt.Errorf("read the fake CLI's start signal: %w", err)

			return
		}

		if got := strings.TrimSpace(line); got != "started" {
			started <- fmt.Errorf("the fake CLI signalled %q, want %q", got, "started")

			return
		}

		started <- nil
	}()

	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the fake CLI never started: Ask did not run the CLI at its path")
	}
}

// TestClaudeCLIAskReportsACancellationAfterTheCLIStarted drives Ask through its
// OutcomeCancelled arm (issue #303): the CLI has started and is still running
// when the caller's context is cancelled, so the run has no result of its own
// to report and Ask answers with the cancellation of the caller's context,
// wrapping ErrModel so the run fails visibly. The two are synchronised on a
// FIFO the fake writes one line to as it starts: no sleep and no poll decides
// the order, so the cancellation cannot come before the CLI ran.
func TestClaudeCLIAskReportsACancellationAfterTheCLIStarted(t *testing.T) {
	t.Parallel()

	// A FIFO is what makes the start of the CLI observable: the fake's write
	// blocks until this test opens it, and this test's open blocks until the
	// fake writes. It is made in a file split by build constraint, because
	// syscall.Mkfifo is Unix-only and make lint typechecks this package's tests
	// for every release target, Windows included (issue #303).
	fifo := newStartFIFO(t)

	cli, rec := newFakeClaudeCLI(t, claudeFake{started: fifo, stdout: cliSuccess})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	type askResult struct {
		text string
		err  error
	}

	done := make(chan askResult, 1)

	go func() {
		text, err := review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second).Ask(ctx, review.Prompt{System: "s", User: "u"})
		done <- askResult{text: text, err: err}
	}()

	awaitFakeCLIStart(t, fifo)

	// The CLI is running and blocked now, as a real one is mid-turn.
	cancel()

	select {
	case got := <-done:
		if got.text != "" {
			t.Errorf("Ask = %q after the cancellation, want no answer: a cancelled run has none", got.text)
		}

		if !errors.Is(got.err, review.ErrModel) {
			t.Fatalf("Ask = %v after the cancellation, want an error wrapping ErrModel, so the check fails and posts nothing", got.err)
		}

		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("Ask = %v after the cancellation, want an error wrapping context.Canceled: the caller cancelled and the CLI is not at fault", got.err)
		}

		if !strings.Contains(strings.ToLower(got.err.Error()), "cancelled") {
			t.Errorf("Ask = %v after the cancellation, want the error to say the CLI was cancelled: a cancellation the caller made still reads as the cancellation it is (issue #316)", got.err)
		}
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Ask did not return within 10s of the cancellation: a cancelled run is killed and reported at once")
	}

	// The CLI ran: the recordings of the run that reached the cancellation are
	// there, so the arm was reached by a CLI that started and not by one Ask
	// refused to start.
	if args := rec.args(t); !slices.Contains(args, "-p") {
		t.Errorf("the fake CLI recorded the arguments %q, want the ones Ask passes: the arm is reached by a CLI that started", args)
	}
}

// runDeadlineReached is the parent context of a run whose deadline passes
// while the CLI is mid-turn: this test closes done once the fake CLI has
// started, and Err then reports context.DeadlineExceeded, as a context made
// with context.WithDeadline does once its deadline is past. Nothing of a clock
// or a sleep decides when it passes: the fake's start signal does (issue #316).
type runDeadlineReached struct {
	context.Context
	done chan struct{}
	once sync.Once
}

// pass lets the run's deadline pass: it closes done, and exactly once, so a
// test that reports a failure after letting it pass cannot close it twice (a
// second close of a channel panics, which would hide the failure the test
// reports).
func (c *runDeadlineReached) pass() {
	c.once.Do(func() { close(c.done) })
}

// Done returns the channel this test closes when the run's deadline passes.
func (c *runDeadlineReached) Done() <-chan struct{} { return c.done }

// Err reports context.DeadlineExceeded once the deadline has passed, and nil
// before, as a context with a deadline does.
func (c *runDeadlineReached) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// Deadline reports no deadline: this context's deadline is not one it knows in
// advance, only that it has passed once Done is closed.
func (c *runDeadlineReached) Deadline() (time.Time, bool) { return time.Time{}, false }

// TestClaudeCLIAskReportsTheRunDeadlineAfterTheCLIStarted pins what a run says
// when its overall deadline passes, not when someone cancels it (issue #316):
// ci/aireview gives the run a deadline of 15 minutes, and a run that reaches it
// has not been cancelled by anybody. Ask therefore answers with an error saying
// the deadline passed and never with the cancelled wording, which would send
// the maintainer looking for a cancellation that never happened. The state is
// reached the way the cancelled arm's test reaches its own (issue #303): on a
// FIFO the fake writes one line to as it starts, so no sleep and no poll
// decides that the CLI ran first.
func TestClaudeCLIAskReportsTheRunDeadlineAfterTheCLIStarted(t *testing.T) {
	t.Parallel()

	fifo := newStartFIFO(t)

	cli, rec := newFakeClaudeCLI(t, claudeFake{started: fifo, stdout: cliSuccess})

	ctx := &runDeadlineReached{Context: t.Context(), done: make(chan struct{})}

	type askResult struct {
		text string
		err  error
	}

	done := make(chan askResult, 1)

	go func() {
		text, err := review.NewClaudeCLI(cli, fakeOAuthToken, 30*time.Second).Ask(ctx, review.Prompt{System: "s", User: "u"})
		done <- askResult{text: text, err: err}
	}()

	awaitFakeCLIStart(t, fifo)

	// The CLI is running and blocked now, as a real one is mid-turn: the run's
	// deadline passes while it is.
	ctx.pass()

	select {
	case got := <-done:
		if got.text != "" {
			t.Errorf("Ask = %q once the run's deadline passed, want no answer: a run that reached its deadline has none", got.text)
		}

		if !errors.Is(got.err, review.ErrModel) {
			t.Fatalf("Ask = %v once the run's deadline passed, want an error wrapping ErrModel, so the check fails and posts nothing", got.err)
		}

		message := strings.ToLower(got.err.Error())

		if !strings.Contains(message, "deadline") {
			t.Errorf("Ask = %v once the run's deadline passed, want the error to name the deadline: the run stopped because its 15 minutes ran out, and the maintainer has to read that (issue #316)", got.err)
		}

		if strings.Contains(message, "was cancelled") {
			t.Errorf("Ask = %v once the run's deadline passed, want no cancelled wording: nobody cancelled this run, its deadline passed, and a cancellation sends the maintainer looking for a caller that does not exist (issue #316)", got.err)
		}
	case <-time.After(10 * time.Second):
		ctx.pass()
		t.Fatal("Ask did not return within 10s of the run's deadline passing: a run whose deadline passed is killed and reported at once")
	}

	// The CLI ran: the recordings of the run that reached the deadline are
	// there, so the arm was reached by a CLI that started and not by one Ask
	// refused to start.
	if args := rec.args(t); !slices.Contains(args, "-p") {
		t.Errorf("the fake CLI recorded the arguments %q, want the ones Ask passes: the arm is reached by a CLI that started", args)
	}
}

// valueAfter returns the argument that follows flag in args, and false when args
// holds no flag. The order of the arguments is not pinned, only that a flag is
// immediately followed by its value.
func valueAfter(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg != flag || i+1 >= len(args) {
			continue
		}

		return args[i+1], true
	}

	return "", false
}

// containsAny reports whether text holds at least one of the texts, all lower
// case.
func containsAny(text string, texts []string) bool {
	for _, want := range texts {
		if strings.Contains(text, want) {
			return true
		}
	}

	return false
}
