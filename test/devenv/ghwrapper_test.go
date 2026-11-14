// Pins the VM's gh wrapper (PRD #161 milestone 1): dev/gh-app-token.sh hands the installation token of the
// VM's token file to the real gh, unsets GITHUB_TOKEN where it does, falls back to GITHUB_TOKEN where the
// file is absent, and refuses a token file that is expired, malformed or readable by another account without
// running gh. The design is .dot-agent-deck/prd161m1-design.md.
package devenv_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// unsetMarker is what the fake real gh records for a variable it was not handed.
const unsetMarker = "__unset__"

// fakeRealGh stands in for the real gh: it records the tokens it was handed, its argv and its stdin, so a
// test can see what the wrapper did without gh, the network or an account.
const fakeRealGh = `#!/bin/sh
# Stands in for the real gh in the devenv tests.
{
  printf 'GH_TOKEN=%s\n' "${GH_TOKEN-__unset__}"
  printf 'GITHUB_TOKEN=%s\n' "${GITHUB_TOKEN-__unset__}"
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
  printf 'stdin:\n'
  cat
} > "${SHIFT2EU_TEST_GH_RECORD:?set SHIFT2EU_TEST_GH_RECORD}"
`

// ghRecord is what the fake real gh saw.
type ghRecord struct {
	ghToken     string
	githubToken string
	argv        []string
	stdin       string
}

// readGHRecord parses what the fake real gh wrote. It reconstructs stdin from the recorded lines, so the
// tests pass stdin that ends in a newline.
func readGHRecord(t *testing.T, path string) ghRecord {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the wrapper did not run the real gh: %v", err)
	}

	var (
		record ghRecord
		stdin  []string
	)

	inStdin := false

	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		switch {
		case inStdin:
			stdin = append(stdin, line)
		case line == "stdin:":
			inStdin = true
		case strings.HasPrefix(line, "GH_TOKEN="):
			record.ghToken = strings.TrimPrefix(line, "GH_TOKEN=")
		case strings.HasPrefix(line, "GITHUB_TOKEN="):
			record.githubToken = strings.TrimPrefix(line, "GITHUB_TOKEN=")
		case strings.HasPrefix(line, "arg="):
			record.argv = append(record.argv, strings.TrimPrefix(line, "arg="))
		}
	}

	if inStdin {
		record.stdin = strings.Join(stdin, "\n") + "\n"
	}

	return record
}

// writeFakeRealGh writes the stand-in for the real gh into the sandbox.
func writeFakeRealGh(t *testing.T, sb *sandbox) string {
	t.Helper()

	path := filepath.Join(sb.binDir, "real-gh")

	//nolint:gosec // the fake real gh is a script the tests run, so it must be executable
	if err := os.WriteFile(path, []byte(fakeRealGh), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

// ghEnv is the environment the wrapper has inside the VM: HOME is the fake VM user's home, where the token
// file lives, and the real gh is the one SHIFT2EU_REAL_GH names.
func (sb *sandbox) ghEnv(realGH, record string) map[string]string {
	env := sb.baseEnv()
	env["HOME"] = sb.vmHome
	env["SHIFT2EU_REAL_GH"] = realGH
	env["SHIFT2EU_TEST_GH_RECORD"] = record

	return env
}

// writeVMTokenFile writes the agents' token file into the fake VM user's home.
func writeVMTokenFile(t *testing.T, sb *sandbox, content string, mode os.FileMode) {
	t.Helper()

	writeFileMode(t, sb.vmPath(agentsTokenFile), []byte(content), mode)
}

// helperStdin is what git hands the credential helper.
var helperStdin = "protocol=https\nhost=github.com\n\n"

func TestGHAppTokenHandsTheAppTokenToTheRealGH(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	record := filepath.Join(sb.root, "gh-record")

	token := fmt.Sprintf("ghs_%036x", 7)
	personal := fmt.Sprintf("ghp_%036x", 9)
	expiry := time.Now().Add(45 * time.Minute).Unix()

	writeVMTokenFile(t, sb, token+"\n"+strconv.FormatInt(expiry, 10)+"\n", 0o600)

	env := sb.ghEnv(writeFakeRealGh(t, sb), record)
	env["GITHUB_TOKEN"] = personal

	stdout, stderr, err := sb.run(t, scriptPath(t, ghWrapperScript), []string{"auth", "git-credential", "get"}, env, helperStdin)
	if err != nil {
		t.Fatalf("the wrapper exited non-zero with a token file that is present, well formed and not expired: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got := readGHRecord(t, record)

	if got.ghToken != token {
		t.Errorf("the real gh was not handed the token of %s as GH_TOKEN", agentsTokenFile)
	}

	if got.githubToken != unsetMarker {
		t.Errorf("the real gh still had GITHUB_TOKEN: with a token file present the wrapper hands only GH_TOKEN, so gh cannot fall back to the maintainer's token")
	}

	wantArgs := []string{"auth", "git-credential", "get"}
	if diff := cmp.Diff(wantArgs, got.argv); diff != "" {
		t.Errorf("the wrapper did not pass its arguments through unchanged (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(helperStdin, got.stdin); diff != "" {
		t.Errorf("the wrapper did not pass stdin through unchanged, and gh auth git-credential reads it (-want +got):\n%s", diff)
	}

	secrets := []secret{{kind: "App installation token", value: token}, {kind: "maintainer token", value: personal}}
	assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
	assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
}

func TestGHAppTokenFallsBackToGITHUBToken(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	record := filepath.Join(sb.root, "gh-record")

	personal := fmt.Sprintf("ghp_%036x", 9)

	env := sb.ghEnv(writeFakeRealGh(t, sb), record)
	env["GITHUB_TOKEN"] = personal

	stdout, stderr, err := sb.run(t, scriptPath(t, ghWrapperScript), []string{"auth", "git-credential", "get"}, env, helperStdin)
	if err != nil {
		t.Fatalf("the wrapper exited non-zero with no token file, which milestone 1 must still allow: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got := readGHRecord(t, record)

	if got.ghToken != unsetMarker {
		t.Errorf("the wrapper set GH_TOKEN with no token file: without one it runs the real gh unchanged")
	}

	if got.githubToken != personal {
		t.Errorf("the real gh did not see GITHUB_TOKEN as the wrapper got it: with no token file the wrapper falls back to it (milestone 3 removes the fallback)")
	}

	wantArgs := []string{"auth", "git-credential", "get"}
	if diff := cmp.Diff(wantArgs, got.argv); diff != "" {
		t.Errorf("the wrapper did not pass its arguments through unchanged (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(helperStdin, got.stdin); diff != "" {
		t.Errorf("the wrapper did not pass stdin through unchanged (-want +got):\n%s", diff)
	}

	assertNoSecrets(t, "the wrapper's stderr", stderr, []secret{{kind: "maintainer token", value: personal}})
}

// TestGHAppTokenRefusesABadTokenFile pins that an expired, malformed or group-readable token file makes the
// wrapper fail without running gh, name make agents-token-refresh, and never print the file's content.
func TestGHAppTokenRefusesABadTokenFile(t *testing.T) {
	t.Parallel()

	token := fmt.Sprintf("ghs_%036x", 11)
	future := strconv.FormatInt(time.Now().Add(45*time.Minute).Unix(), 10)
	past := strconv.FormatInt(time.Now().Add(-90*time.Minute).Unix(), 10)

	cases := []struct {
		name    string
		content string
		mode    os.FileMode
		token   string // the token the file holds, for the leak assertion; empty when it holds none
	}{
		{name: "expired", content: token + "\n" + past + "\n", mode: 0o600, token: token},
		{name: "one line", content: token + "\n", mode: 0o600, token: token},
		{name: "expiry that is not Unix seconds", content: token + "\nsoon\n", mode: 0o600, token: token},
		{name: "empty", content: "", mode: 0o600},
		{name: "group readable", content: token + "\n" + future + "\n", mode: 0o640, token: token},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			record := filepath.Join(sb.root, "gh-record")
			writeVMTokenFile(t, sb, tc.content, tc.mode)

			env := sb.ghEnv(writeFakeRealGh(t, sb), record)

			stdout, stderr, err := sb.run(t, scriptPath(t, ghWrapperScript), []string{"auth", "git-credential", "get"}, env, helperStdin)
			if err == nil {
				t.Fatalf("the wrapper exited 0 with a token file that is %s", tc.name)
			}

			if _, err := os.Stat(record); err == nil {
				t.Fatalf("the wrapper ran the real gh with a token file that is %s: it must refuse it and exit without calling gh", tc.name)
			}

			if !strings.Contains(stderr, "make agents-token-refresh") {
				t.Errorf("the wrapper's stderr does not name make agents-token-refresh, the command that mints a new token: %s", stderr)
			}

			if tc.token == "" {
				return
			}

			secrets := []secret{{kind: "App installation token", value: tc.token}}
			assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
			assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
		})
	}
}
