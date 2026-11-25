// Pins what the launcher does with a config beyond its arguments and its
// environment (ADR 0040's model-job amendment, issue #277): the CLI runs in
// the working directory and with the HOME the config names, the system prompt
// reaches it in the file the flag names, and a transcript gets every byte the
// CLI printed on stdout while the answer is read from the last result element.
// A fake CLI records what it was handed, as the reviewer's own test does.
package claudecode_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

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

// streamedDontAskRun is streamedRun with an init element that reports the
// permission mode dontAsk: a streamed run of a config that asks for a mode
// is checked against the session's init element (issue #592).
const streamedDontAskRun = `{"type":"system","subtype":"init","permissionMode":"dontAsk"}
{"type":"assistant","subtype":"text"}
{"type":"result","subtype":"success","is_error":false,"result":"the first result"}
{"type":"result","subtype":"success","is_error":false,"result":"the last result"}
`

// answeredRun is what a fake CLI prints for --output-format json: the one result
// element the launcher reads.
const answeredRun = `{"type":"result","subtype":"success","is_error":false,"result":"the last result"}`

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
// is no more readable than it must be while it is written. Every fake CLI a test
// runs is written here, holding syscall.ForkLock for reading while the file is
// open for writing (issue #175, as test/devenv's writeFileMode does since #267):
// Go opens the file O_CLOEXEC, but a parallel test that forks in that window
// gives its child a copy of the write descriptor, and the fork itself takes
// ForkLock for writing, so no fork happens while it is open. Linux refuses to
// exec a file any process holds open for writing, so the launcher's exec of the
// fake CLI would otherwise fail at random with ETXTBSY ("text file busy").
// Writing it to a temporary name and renaming it into place would not help: the
// child's descriptor is on the same inode.
func writeFakeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()

	syscall.ForkLock.RLock()
	err := os.WriteFile(path, content, 0o600)
	syscall.ForkLock.RUnlock()

	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s to %v: %v", path, mode, err)
	}
}

// reached reports whether the fake CLI wrote its marker: the file a slow fake
// writes only after the pause that follows the elements it prints first, so a run
// the launcher stops never reaches it.
func (r claudeRecording) reached(t *testing.T) bool {
	t.Helper()

	_, err := os.Lstat(filepath.Join(r.dir, "reached"))
	switch {
	case err == nil:
		return true
	case errors.Is(err, fs.ErrNotExist):
		return false
	default:
		t.Fatalf("look for the fake CLI's marker: %v", err)
	}

	return false
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

// env returns the environment the fake CLI ran with, as name → value. A
// failure message of this file names a variable, never its value: a regression
// could put a credential of the caller's in one, and a test log is no place for
// it (CLAUDE.md rule 3, issue #277, round F finding 4).
func (r claudeRecording) env(t *testing.T) map[string]string {
	t.Helper()

	values := map[string]string{}

	for _, line := range strings.Split(r.read(t, "env"), "\n") {
		if line == "" {
			continue
		}

		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("the fake CLI's environment holds a line for %q that is no name=value pair, want name=value", name)
		}

		values[name] = value
	}

	return values
}

// newSlowFakeClaudeCLI writes an executable fake CLI that prints head, waits a
// second, writes a marker and then prints tail: a run the launcher stops at an
// element of head never reaches the marker, and one it lets run reads tail's
// result. The pause is what makes "the run was stopped" observable: the CLI would
// otherwise print everything before the launcher read its first line.
func newSlowFakeClaudeCLI(t *testing.T, head, tail string) (string, claudeRecording) {
	t.Helper()

	rec := claudeRecording{dir: t.TempDir()}

	writeFakeFile(t, filepath.Join(rec.dir, "canned-head"), []byte(head), 0o600)
	writeFakeFile(t, filepath.Join(rec.dir, "canned-tail"), []byte(tail), 0o600)

	path := filepath.Join(t.TempDir(), "claude")
	writeFakeFile(t, path, []byte(fmt.Sprintf(`#!/bin/sh
rec=%q
cat > "$rec/stdin"
cat "$rec/canned-head"
sleep 1
: > "$rec/reached"
cat "$rec/canned-tail"
`, rec.dir)), 0o700)

	return path, rec
}

// launcherVariableNames are the names the launcher sets in the CLI's
// environment: the ADR's allow list and the three entries the audit adds.
var launcherVariableNames = map[string]bool{
	"CLAUDE_CODE_OAUTH_TOKEN": true,
	"HOME":                    true,
	"PATH":                    true,
	"DISABLE_AUTOUPDATER":     true,
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": true,
	"DISABLE_TELEMETRY":                        true,
	"DISABLE_ERROR_REPORTING":                  true,
	"TMPDIR":                                   true,
	"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB":         true,
	"SHIFT2EU_AUDIT_CANARY":                    true,
}

// shellVariableNames are the names a POSIX shell sets for itself in the
// environment it hands the commands it runs, beside the ones it inherited: the
// fake CLI is the shell script newFakeClaudeCLI writes.
var shellVariableNames = map[string]bool{"PWD": true, "SHLVL": true, "_": true, "OLDPWD": true}

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

	cli, rec := newFakeClaudeCLI(t, claudeFake{stdout: streamedDontAskRun})

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
		Env: []string{
			"TMPDIR=" + filepath.Join(home, "tmp"),
			"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1",
			"SHIFT2EU_AUDIT_CANARY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		SearchPath: "/usr/bin:/bin",
		Transcript: &transcript,
		Timeout:    30 * time.Second,
	})

	prompt := claudecode.Prompt{System: "the audit's skill, appended to the CLI's own prompt", User: "the audit's fixed user prompt"}

	got, err := launcher.Ask(t.Context(), prompt)
	if err != nil {
		t.Fatalf("Ask: %v; the fake CLI's stderr: %s", err, rec.read(t, "canned-stderr"))
	}

	if got != "the last result" {
		t.Errorf("Ask = %q, want %q: a streamed run's answer is its last result element", got, "the last result")
	}

	if diff := transcript.String(); diff != streamedDontAskRun {
		t.Errorf("the transcript holds %q, want every byte the CLI printed, %q", diff, streamedDontAskRun)
	}

	if pwd := strings.TrimSuffix(rec.read(t, "pwd"), "\n"); pwd != work {
		t.Errorf("the CLI ran in %q, want the config's working directory %q", pwd, work)
	}

	// The CLI's environment is the launcher's allow list and the shell's own
	// variables, and nothing else: no name of this process's environment is
	// there, whatever it holds (CLAUDE.md rule 3). The check is on the names, so
	// a regression that let one through fails here without this test printing
	// its value (issue #277, round F findings 4 and 5).
	env := rec.env(t)

	for name := range env {
		if !launcherVariableNames[name] && !shellVariableNames[name] {
			t.Errorf("the CLI's environment holds %s, which is neither one of the launcher's names nor one of the shell's own: the launcher passes its allow list and nothing of the caller's (CLAUDE.md rule 3, ADR 0040)", name)
		}
	}

	// Each entry the config gave is the one the CLI sees, so the audit's three
	// names are accepted and the launcher's own are what they were (issue #277,
	// round F finding 1).
	for _, want := range []struct{ name, value string }{
		{"HOME", home},
		{"PATH", "/usr/bin:/bin"},
		{"TMPDIR", filepath.Join(home, "tmp")},
		{"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB", "1"},
		{"SHIFT2EU_AUDIT_CANARY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	} {
		if got := env[want.name]; got != want.value {
			t.Errorf("the CLI's %s is not the value the launch fixes, want %q (set: %t): the config's entries are accepted and the launcher's own are what they were", want.name, want.value, got != "")
		}
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

// TestAskWritesTheSystemPromptIntoTheCallersDirectory pins finding R11 of the
// review of issue #277: a config that fixes HOME fixes the system prompt's
// directory too, so the file lands where the caller can watch it - for the audit,
// the state area's tmp/ - and not in the temporary directory the process's TMPDIR
// names, which an earlier step of the same job can set. The file is the run's
// alone: it is gone when Ask returns.
func TestAskWritesTheSystemPromptIntoTheCallersDirectory(t *testing.T) {
	t.Parallel()

	cli, rec := newFakeClaudeCLI(t, claudeFake{stdout: answeredRun})

	work, home, prompts := t.TempDir(), t.TempDir(), t.TempDir()

	launcher := claudecode.New(claudecode.Config{
		Path:            cli,
		Token:           "fake-subscription-value-123",
		Model:           auditModel,
		WorkDir:         work,
		Home:            home,
		SystemPromptDir: prompts,
		Timeout:         30 * time.Second,
	})

	prompt := claudecode.Prompt{System: "the audit's skill, appended to the CLI's own prompt", User: "the audit's fixed user prompt"}

	if _, err := launcher.Ask(t.Context(), prompt); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	systemFile, ok := valueAfter(rec.args(t), "--system-prompt-file")
	if !ok {
		t.Fatalf("the CLI's arguments %q hold no --system-prompt-file, so the system prompt never reached it", rec.args(t))
	}

	if got := filepath.Dir(systemFile); got != prompts {
		t.Errorf("the system prompt file is %q, want it in the config's directory %q: the launcher writes nothing into the temporary directory the process's TMPDIR names when the caller names one (issue #277)", systemFile, prompts)
	}

	if got := rec.read(t, "system"); got != prompt.System {
		t.Errorf("the file --system-prompt-file names held %q, want the prompt's system %q", got, prompt.System)
	}

	left, err := os.ReadDir(prompts)
	if err != nil {
		t.Fatalf("read %s: %v", prompts, err)
	}

	if len(left) != 0 {
		var names []string
		for _, entry := range left {
			names = append(names, entry.Name())
		}

		t.Errorf("the config's directory holds %v after the run, want nothing: the launcher removes the file it wrote", names)
	}
}

// TestAskKeepsTheReviewersSystemPromptInTheTemporaryDirectory pins the other half
// of R11: the reviewer's launch fixes no directory, and its system prompt stays in
// the fresh temporary directory of its own that the launcher removes after the
// run.
func TestAskKeepsTheReviewersSystemPromptInTheTemporaryDirectory(t *testing.T) {
	t.Parallel()

	cli, rec := newFakeClaudeCLI(t, claudeFake{stdout: answeredRun})

	launcher := claudecode.New(claudecode.Config{
		Path:    cli,
		Token:   "fake-subscription-value-123",
		Model:   auditModel,
		Timeout: 30 * time.Second,
	})

	if _, err := launcher.Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	systemFile, ok := valueAfter(rec.args(t), "--system-prompt-file")
	if !ok {
		t.Fatalf("the CLI's arguments %q hold no --system-prompt-file", rec.args(t))
	}

	// The file is the launcher's own fresh directory's, the one it makes with
	// the claudecode-system- prefix when the config names none: a config that
	// fixes no directory keeps the reviewer's launch, and the file is never
	// written beside the caller's.
	systemDir := filepath.Dir(systemFile)

	if got := filepath.Base(systemDir); !strings.HasPrefix(got, "claudecode-system-") {
		t.Errorf("the system prompt file is %q, in the directory %q, want one whose name has the prefix %q: the launcher's own temporary directory for it (issue #277, review R11)", systemFile, got, "claudecode-system-")
	}

	if got := filepath.Base(systemFile); got != "system-prompt.md" {
		t.Errorf("the system prompt file is %q, want system-prompt.md in the launcher's own directory: the file the launcher removes after the run", systemFile)
	}

	if _, err := os.Lstat(systemDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the system prompt's directory %q is still there after the run (%v), want it removed: the launcher removes every directory it made", systemDir, err)
	}
}

// initCanary is the value the session's system/init elements carry in a key of
// their own and, in one row, as the permission mode itself: the launcher's error
// holds the expected mode and a known mode or a class, never a byte of the line
// (issue #592).
const initCanary = "canary-in-the-init-element"

// TestAskRefusesASessionWhoseInitReportsAnotherPermissionMode pins the check
// issue #592 adds. With CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1, Claude Code 2.1.332
// forces the permission mode to default whatever the launch asks for, so the
// launcher reads the session's system/init element and refuses one that reports
// a mode other than the one it asked for - before the CLI makes a tool call. The
// error wraps ErrModel and a *PermissionModeError whose Expected is the mode the
// config asked for and whose Reported is one of the CLI's known modes as it is,
// or the class of a report that names none: missing (no init element before the
// first assistant element or the end, or one without permissionMode), not a
// string, another mode, two init lines. The error never quotes the element.
func TestAskRefusesASessionWhoseInitReportsAnotherPermissionMode(t *testing.T) {
	t.Parallel()

	// theAnswer is the result line the fake prints after its marker: a session the
	// launcher accepts has answered, and one it stops never reaches it.
	const theAnswer = `{"type":"result","subtype":"success","is_error":false,"result":"the answer"}` + "\n"

	// assistant is an element of the session's turn after its init element.
	const assistant = `{"type":"assistant","subtype":"text"}` + "\n"

	// initElement is the CLI's system/init element, with the canary in a key of
	// its own and mode as the JSON value of permissionMode.
	initElement := func(mode string) string {
		return `{"type":"system","subtype":"init","permissionMode":` + mode + `,"cwd":"` + initCanary + `"}` + "\n"
	}

	// bareInit is an init element of the given body, for the rows whose shape is
	// not one permissionMode value.
	bareInit := func(body string) string {
		return `{"type":"system","subtype":"init",` + body + `,"cwd":"` + initCanary + `"}` + "\n"
	}

	cases := []struct {
		name string
		// head is what the fake prints before its pause: the session's elements,
		// the init one among them or not.
		head string
		// wantAnswer is whether the launcher accepts the session and returns its
		// result.
		wantAnswer bool
		// wantReported is the mode or the class *PermissionModeError names.
		wantReported string
		// wantStopped is whether the launcher must stop the run at the element it
		// refused, so the fake never reaches its marker nor its answer.
		wantStopped bool
	}{
		{
			name:       "the mode asked for is the mode the session reports",
			head:       initElement(`"default"`) + assistant,
			wantAnswer: true,
		},
		{
			name:         "the session reports dontAsk",
			head:         initElement(`"dontAsk"`) + assistant,
			wantReported: "dontAsk",
			wantStopped:  true,
		},
		{
			name:         "the session reports plan",
			head:         initElement(`"plan"`) + assistant,
			wantReported: "plan",
			wantStopped:  true,
		},
		{
			name:         "the session reports auto",
			head:         initElement(`"auto"`) + assistant,
			wantReported: "auto",
			wantStopped:  true,
		},
		{
			name:         "the session reports acceptEdits",
			head:         initElement(`"acceptEdits"`) + assistant,
			wantReported: "acceptEdits",
			wantStopped:  true,
		},
		{
			name:         "the session reports bypassPermissions",
			head:         initElement(`"bypassPermissions"`) + assistant,
			wantReported: "bypassPermissions",
			wantStopped:  true,
		},
		{
			name:         "the session reports the mode in another case",
			head:         initElement(`"Default"`) + assistant,
			wantReported: "another mode",
			wantStopped:  true,
		},
		{
			// The mode string itself holds the canary: the error names the class,
			// never the string.
			name:         "the session reports a mode the CLI does not know, and the mode string is the canary",
			head:         initElement(`"`+initCanary+`"`) + assistant,
			wantReported: "another mode",
			wantStopped:  true,
		},
		{
			name:         "the permission mode is null",
			head:         initElement(`null`) + assistant,
			wantReported: "not a string",
			wantStopped:  true,
		},
		{
			name:         "the permission mode is a number",
			head:         initElement(`5`) + assistant,
			wantReported: "not a string",
			wantStopped:  true,
		},
		{
			name:         "the permission mode is an object",
			head:         initElement(`{"mode":"default"}`) + assistant,
			wantReported: "not a string",
			wantStopped:  true,
		},
		{
			name:         "the permission mode is a boolean",
			head:         initElement(`true`) + assistant,
			wantReported: "not a string",
			wantStopped:  true,
		},
		{
			name:         "the init element names no permission mode",
			head:         `{"type":"system","subtype":"init","cwd":"` + initCanary + `"}` + "\n" + assistant,
			wantReported: "missing",
			wantStopped:  true,
		},
		{
			name:         "the session prints two init elements",
			head:         initElement(`"default"`) + initElement(`"dontAsk"`),
			wantReported: "two init lines",
			wantStopped:  true,
		},
		{
			name:         "the init element names permissionMode twice",
			head:         bareInit(`"permissionMode":"default","permissionMode":"dontAsk"`),
			wantReported: "two init lines",
			wantStopped:  true,
		},
		{
			name:         "the init element spells permissionMode in another case",
			head:         bareInit(`"PermissionMode":"default"`),
			wantReported: "two init lines",
			wantStopped:  true,
		},
		{
			name:         "an assistant element comes before any init element",
			head:         assistant,
			wantReported: "missing",
			wantStopped:  true,
		},
		{
			// The run has to end before the launcher can know no init element
			// came, so the fake reaches its marker: the refusal is at the end.
			name:         "the run prints no init element at all",
			head:         `{"type":"system","subtype":"ui_invalidate"}` + "\n",
			wantReported: "missing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli, rec := newSlowFakeClaudeCLI(t, tc.head, theAnswer)

			var transcript bytes.Buffer

			launcher := claudecode.New(claudecode.Config{
				Path:           cli,
				Token:          "fake-subscription-value-123",
				Model:          auditModel,
				PermissionMode: "default",
				Transcript:     &transcript,
				Timeout:        30 * time.Second,
			})

			answer, err := launcher.Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"})

			reached := rec.reached(t)

			if tc.wantAnswer {
				if err != nil {
					t.Fatalf("Ask: %v; the session reports the mode the launcher asked for", err)
				}

				if answer != "the answer" {
					t.Errorf("Ask = %q, want %q: the session's result is the run's answer", answer, "the answer")
				}

				if !reached {
					t.Error("the run stopped before the fake CLI's marker, want it to run to its end: the session reports the mode asked for, and the check must not refuse it")
				}

				return
			}

			if !errors.Is(err, claudecode.ErrModel) {
				t.Fatalf("Ask = %q, %v; want an error wrapping ErrModel: the session reports a mode other than %q", answer, err, "default")
			}

			var modeErr *claudecode.PermissionModeError
			if !errors.As(err, &modeErr) {
				t.Fatalf("Ask = %v, want a *claudecode.PermissionModeError: the launcher names the session's permission mode (issue #592)", err)
			}

			want := struct{ Expected, Reported string }{Expected: "default", Reported: tc.wantReported}
			got := struct{ Expected, Reported string }{Expected: modeErr.Expected, Reported: modeErr.Reported}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the permission mode error's modes (-want +got):\n%s", diff)
			}

			wantText := "the session's permission mode is not the one asked for: expected default, reported " + tc.wantReported
			if !strings.Contains(err.Error(), wantText) {
				t.Errorf("Ask's error %q does not hold %q: the refusal is the fixed sentence with the mode or the class of the report", err, wantText)
			}

			// The element carries the canary in a key of its own, and one row in its
			// mode string: the refusal is made of fixed words, never of the line's
			// bytes (issue #592).
			if strings.Contains(err.Error(), initCanary) {
				t.Errorf("Ask's error %q holds the init element's canary: the refusal names the expected mode and a known mode or a class, never a byte of the line", err)
			}

			if tc.wantStopped && reached {
				t.Error("the run reached the fake CLI's marker and its answer, want the launcher to stop it at the element it refused: a session of another permission mode must not make a tool call")
			}
		})
	}
}

// TestAskChecksThePermissionModeOnlyWithAModeAndATranscript pins the check's two
// gates (issue #592): the AI reviewer's launch asks for no permission mode and
// reads one JSON result, so it has no system/init element to check and runs as it
// did.
func TestAskChecksThePermissionModeOnlyWithAModeAndATranscript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// config is the launcher's, beside the path, token, model and timeout.
		config claudecode.Config
		// canned is what the fake CLI prints: streamedRun's init element names no
		// permission mode, which a launcher that asked for one would refuse.
		canned string
	}{
		{
			name:   "the launch asks for no permission mode",
			config: claudecode.Config{Transcript: &bytes.Buffer{}},
			canned: streamedRun,
		},
		{
			// No transcript is --output-format json: one result element and no
			// init element, which is the reviewer's launch.
			name:   "the launch reads one JSON result",
			config: claudecode.Config{PermissionMode: "default"},
			canned: answeredRun,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli, _ := newFakeClaudeCLI(t, claudeFake{stdout: tc.canned})

			cfg := tc.config
			cfg.Path, cfg.Token, cfg.Model, cfg.Timeout = cli, "fake-subscription-value-123", auditModel, time.Minute

			got, err := claudecode.New(cfg).Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"})
			if err != nil {
				t.Fatalf("Ask: %v; the permission mode's check applies to a streamed run the config asks a mode for, and to no other", err)
			}

			if got != "the last result" {
				t.Errorf("Ask = %q, want %q", got, "the last result")
			}
		})
	}
}

// TestAskRedactsEveryValueItWasGiven pins finding A5 of the audit of issue #277:
// the launcher's error quotes at most an excerpt of the CLI's stderr, and the
// excerpt holds neither the token nor a value the config adds to Redact - the
// audit's canary among them - so the stderr file the launcher writes into the
// state area holds neither either.
func TestAskRedactsEveryValueItWasGiven(t *testing.T) {
	t.Parallel()

	// token and canary are obvious fakes, never real values (CLAUDE.md rule 3).
	const (
		token  = "fake-subscription-value-123"
		canary = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)

	cli, _ := newFakeClaudeCLI(t, claudeFake{stdout: "", stderr: "the shell holds " + token + " and " + canary + "\n", exit: 1})

	launcher := claudecode.New(claudecode.Config{
		Path:    cli,
		Token:   token,
		Model:   auditModel,
		Redact:  []string{canary},
		Timeout: 30 * time.Second,
	})

	_, err := launcher.Ask(t.Context(), claudecode.Prompt{System: "s", User: "u"})
	if err == nil {
		t.Fatal("Ask = nil, want an error: the CLI printed no result and exited non-zero")
	}

	// The messages name the value they want redacted, never the value itself:
	// a redaction that failed would put it in the log through the error, which
	// is what this test guards against (issue #277, round F finding 4).
	for _, secret := range []struct{ name, value string }{
		{"the token", token},
		{"the canary", canary},
	} {
		if strings.Contains(err.Error(), secret.value) {
			t.Errorf("Ask's error holds %s, want it redacted: the error can reach the job's log and the state area's stderr file", secret.name)
		}
	}

	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("Ask's error holds no [redacted], want the excerpt to stand every value in for")
	}
}
