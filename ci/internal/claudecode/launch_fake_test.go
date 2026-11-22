// Pins what the launcher does with a config beyond its arguments and its
// environment (ADR 0040's milestone-2 amendment, issue #277): the CLI runs in
// the working directory and with the HOME the config names, the system prompt
// reaches it in the file the flag names, and a transcript gets every byte the
// CLI printed on stdout while the answer is read from the last result element.
// A fake CLI records what it was handed, as the reviewer's own test does.
package claudecode_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/internal/claudecode"
)

// streamedRun is what a fake CLI prints for --output-format stream-json: one
// element per line, with two result elements, so the answer can only come from
// the last one.
const streamedRun = `{"type":"system","subtype":"init"}
{"type":"assistant","subtype":"text"}
{"type":"result","subtype":"success","is_error":false,"result":"the first result"}
{"type":"result","subtype":"success","is_error":false,"result":"the last result"}
`

// claudeFake is one fake CLI's behaviour, and the bytes it holds.
type claudeFake struct {
	stdout string
	stderr string
	exit   int
}

// claudeRecording is the directory the fake CLI records into.
type claudeRecording struct {
	dir string
}

// newFakeClaudeCLI writes an executable fake `claude` recording into a directory
// of its own and returns its path and that directory.
func newFakeClaudeCLI(t *testing.T, behaviour claudeFake) (string, claudeRecording) {
	t.Helper()

	rec := claudeRecording{dir: t.TempDir()}

	writeFakeFile(t, filepath.Join(rec.dir, "canned-stdout"), []byte(behaviour.stdout), 0o600)
	writeFakeFile(t, filepath.Join(rec.dir, "canned-stderr"), []byte(behaviour.stderr), 0o600)

	path := filepath.Join(t.TempDir(), "claude")
	writeFakeFile(t, path, []byte(claudeFakeScript(rec.dir, behaviour)), 0o700)

	return path, rec
}

// claudeFakeScript returns the fake CLI: a shell script that records its
// arguments, its stdin, its environment, its working directory and the file
// either system prompt flag names, then prints its canned output and exits as
// told. Every path it writes is baked in, and its only read of the environment
// is $HOME, which the launcher sets.
func claudeFakeScript(rec string, behaviour claudeFake) string {
	return fmt.Sprintf(`#!/bin/sh
rec=%q
for a in "$@"; do printf '%%s\n' "$a"; done > "$rec/args"
cat > "$rec/stdin"
env > "$rec/env"
pwd > "$rec/pwd"
ls -A > "$rec/workls"
ls -A "$HOME" > "$rec/homels"
prev=
for a in "$@"; do
	case "$prev" in
	--system-prompt-file|--append-system-prompt-file) cat "$a" > "$rec/system" ;;
	esac
	prev="$a"
done
cat "$rec/canned-stdout"
cat "$rec/canned-stderr" >&2
exit %d
`, rec, behaviour.exit)
}

// writeFakeFile writes content to path with mode, written 0600 first so a script
// is no more readable than it must be while it is written.
func writeFakeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s to %v: %v", path, mode, err)
	}
}

// read returns the text the fake CLI recorded under name.
func (r claudeRecording) read(t *testing.T, name string) string {
	t.Helper()

	text, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		t.Fatalf("read the fake CLI's %s: %v; the launcher did not run it as the test expects", name, err)
	}

	return string(text)
}

// args returns the arguments the fake CLI received, one per line, the empty
// argument included.
func (r claudeRecording) args(t *testing.T) []string {
	t.Helper()

	recorded := r.read(t, "args")
	if recorded == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(recorded, "\n"), "\n")
}

// env returns the environment the fake CLI ran with, as name → value.
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

// valueAfter returns the argument that follows flag in args.
func valueAfter(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}

	return args[i+1], true
}

// TestAskRunsTheConfiguredLaunch pins the audit's launch as the CLI sees it: the
// working directory and HOME the launcher was given, the extra environment
// entries beside its own, the system prompt appended in the file the flag names,
// and a transcript holding every byte the CLI printed, while the answer is the
// last result element of the stream.
func TestAskRunsTheConfiguredLaunch(t *testing.T) {
	t.Parallel()

	cli, rec := newFakeClaudeCLI(t, claudeFake{stdout: streamedRun})

	work, home := t.TempDir(), t.TempDir()

	var transcript bytes.Buffer

	launcher := claudecode.New(claudecode.Config{
		Path:               cli,
		Token:              "fake-subscription-value-123",
		Model:              auditModel,
		Tools:              strings.Split(auditTools, ","),
		Effort:             "high",
		Settings:           auditSettings,
		PermissionMode:     "dontAsk",
		AppendSystemPrompt: true,
		SafeModeOff:        true,
		WorkDir:            work,
		Home:               home,
		Env:                []string{"TMPDIR=" + filepath.Join(home, "tmp")},
		SearchPath:         "/usr/bin:/bin",
		Transcript:         &transcript,
		Timeout:            30 * time.Second,
	})

	// A canary of the test's own: the launcher's environment is built from its
	// allow list, so no variable of this process reaches the CLI (CLAUDE.md rule
	// 3). The test states it rather than setting one: tests-no-process-state
	// refuses t.Setenv, and an inherited variable would have to be one of the
	// allow list's names to appear at all.
	const canary = "SHIFT2EU_TEST_CANARY"

	prompt := claudecode.Prompt{System: "the audit's skill, appended to the CLI's own prompt", User: "the audit's fixed user prompt"}

	got, err := launcher.Ask(t.Context(), prompt)
	if err != nil {
		t.Fatalf("Ask: %v; the fake CLI's stderr: %s", err, rec.read(t, "canned-stderr"))
	}

	if got != "the last result" {
		t.Errorf("Ask = %q, want %q: a streamed run's answer is its last result element", got, "the last result")
	}

	if diff := transcript.String(); diff != streamedRun {
		t.Errorf("the transcript holds %q, want every byte the CLI printed, %q", diff, streamedRun)
	}

	if pwd := strings.TrimSuffix(rec.read(t, "pwd"), "\n"); pwd != work {
		t.Errorf("the CLI ran in %q, want the config's working directory %q", pwd, work)
	}

	env := rec.env(t)

	if env["HOME"] != home {
		t.Errorf("the CLI's HOME is %q, want the config's %q", env["HOME"], home)
	}

	if env["PATH"] != "/usr/bin:/bin" {
		t.Errorf("the CLI's PATH is %q, want the config's %q", env["PATH"], "/usr/bin:/bin")
	}

	if _, ok := env["TMPDIR"]; !ok {
		t.Errorf("the CLI's environment holds no TMPDIR, want the config's entry beside the launcher's own: %v", env)
	}

	if _, ok := env[canary]; ok {
		t.Errorf("the CLI's environment holds %s, a variable of the test process: the launcher passes its allow list and nothing of the caller's (CLAUDE.md rule 3)", canary)
	}

	if entries := rec.read(t, "homels"); entries != "" {
		t.Errorf("the CLI's HOME %q holds %q, want nothing: the launcher writes no file into a HOME it was given", home, entries)
	}

	systemFile, ok := valueAfter(rec.args(t), "--append-system-prompt-file")
	if !ok {
		t.Fatalf("the CLI's arguments %q hold no --append-system-prompt-file, so the audit's skill never reaches it", rec.args(t))
	}

	if !filepath.IsAbs(systemFile) {
		t.Errorf("--append-system-prompt-file is %q, want an absolute path", systemFile)
	}

	if got := rec.read(t, "system"); got != prompt.System {
		t.Errorf("the file --append-system-prompt-file names held %q, want the prompt's system %q", got, prompt.System)
	}

	if got := rec.read(t, "stdin"); got != prompt.User {
		t.Errorf("the CLI's stdin held %q, want the prompt's user %q", got, prompt.User)
	}
}

// TestAskWritesTheTranscriptOfAFailedRunToo pins that the transcript is the CLI's
// stdout as it was, whatever the run's outcome: a run that ends in an error
// result still leaves the operator the whole stream to read, and the error stays
// the constant class the reviewer's errors are.
func TestAskWritesTheTranscriptOfAFailedRunToo(t *testing.T) {
	t.Parallel()

	const failedRun = `{"type":"system","subtype":"init"}
{"type":"result","subtype":"error_max_turns","is_error":true,"result":"the model's untrusted words"}
`

	cli, _ := newFakeClaudeCLI(t, claudeFake{stdout: failedRun})

	var transcript bytes.Buffer

	launcher := claudecode.New(claudecode.Config{
		Path:       cli,
		Token:      "fake-subscription-value-123",
		Model:      auditModel,
		Transcript: &transcript,
		Timeout:    30 * time.Second,
	})

	if _, err := launcher.Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"}); err == nil {
		t.Fatal("Ask = nil, want an error: the run ended with an error result")
	}

	if transcript.String() != failedRun {
		t.Errorf("the transcript holds %q, want every byte the CLI printed, %q", transcript.String(), failedRun)
	}
}
