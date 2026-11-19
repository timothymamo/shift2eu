// Pins the VM's gh wrapper (PRD #161 milestone 1, and milestone 3 for the fallback's removal): dev/gh-app-token.sh
// hands the installation token of the VM's token file to the real gh, unsets GITHUB_TOKEN where it does, refuses
// when the token file is absent - GITHUB_TOKEN in the environment is no fallback of the agents' any more - and
// refuses a token file that is expired, malformed or readable by another account without running gh, telling the
// agent to ask the maintainer to refresh the token rather than to touch the file itself (review item 10).
// The design is .dot-agent-deck/prd161m1-design.md.
package devenv_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// unsetMarker is what the fake real gh records for a variable it was not handed.
const unsetMarker = "__unset__"

// fakeRealGh stands in for the real gh: it records the tokens and the GitHub token variables it was handed, its
// argv and its stdin, so a test can see what the wrapper did without gh, the network or an account. Every
// variable the gh CLI reads a token from is recorded, GitHub Enterprise Cloud's spellings included: a stale one
// inherited from the VM's environment would let gh act as its holder (PRD #161 milestone 3).
const fakeRealGh = `#!/bin/sh
# Stands in for the real gh in the devenv tests.
{
  printf 'GH_TOKEN=%s\n' "${GH_TOKEN-__unset__}"
  printf 'GITHUB_TOKEN=%s\n' "${GITHUB_TOKEN-__unset__}"
  printf 'GH_ENTERPRISE_TOKEN=%s\n' "${GH_ENTERPRISE_TOKEN-__unset__}"
  printf 'GITHUB_ENTERPRISE_TOKEN=%s\n' "${GITHUB_ENTERPRISE_TOKEN-__unset__}"
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
  printf 'stdin:\n'
  cat
} > "${SHIFT2EU_TEST_GH_RECORD:?set SHIFT2EU_TEST_GH_RECORD}"
`

// ghRecord is what the fake real gh saw.
type ghRecord struct {
	ghToken               string
	githubToken           string
	ghEnterpriseToken     string
	githubEnterpriseToken string
	argv                  []string
	stdin                 string
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
		case strings.HasPrefix(line, "GH_ENTERPRISE_TOKEN="):
			record.ghEnterpriseToken = strings.TrimPrefix(line, "GH_ENTERPRISE_TOKEN=")
		case strings.HasPrefix(line, "GITHUB_ENTERPRISE_TOKEN="):
			record.githubEnterpriseToken = strings.TrimPrefix(line, "GITHUB_ENTERPRISE_TOKEN=")
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

	writeFileMode(t, path, []byte(fakeRealGh), 0o700)

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

	// The token file holds whichever format GitHub answered with: the new one it mints since 2026 and the old
	// one a VM may still hold must both reach gh unchanged (issue #232).
	cases := []struct {
		name   string
		token  string
		format appTokenFormat
	}{
		{name: "the new format GitHub mints now", token: newFormatAppToken(7), format: newFormatTokens},
		{name: "the old format GitHub minted before", token: oldFormatAppToken(7), format: oldFormatTokens},
		{name: "a token longer than the new format's 520 characters", token: newFormatAppTokenWithClaims(7, 1050), format: newFormatTokens},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertAppTokenFormat(t, "the token in "+agentsTokenFile, tc.token, tc.format)

			sb := newSandbox(t)
			record := filepath.Join(sb.root, "gh-record")

			personal := fmt.Sprintf("ghp_%036x", 9)
			enterprise := fakeEnvValue("GH_ENTERPRISE_TOKEN")
			githubEnterprise := fakeEnvValue("GITHUB_ENTERPRISE_TOKEN")

			expiry := time.Now().Add(45 * time.Minute).Unix()

			writeVMTokenFile(t, sb, tc.token+"\n"+strconv.FormatInt(expiry, 10)+"\n", 0o600)

			env := sb.ghEnv(writeFakeRealGh(t, sb), record)
			env["GITHUB_TOKEN"] = personal
			env["GH_ENTERPRISE_TOKEN"] = enterprise
			env["GITHUB_ENTERPRISE_TOKEN"] = githubEnterprise

			stdout, stderr, err := sb.run(t, scriptPath(t, ghWrapperScript), []string{"auth", "git-credential", "get"}, env, helperStdin)
			if err != nil {
				t.Fatalf("the wrapper exited non-zero with a token file that is present, well formed, not expired and in %s: %v\nstdout: %s\nstderr: %s", tc.name, err, stdout, stderr)
			}

			got := readGHRecord(t, record)

			if got.ghToken != tc.token {
				t.Errorf("the real gh was not handed the token of %s as GH_TOKEN: it saw %d characters, want the %d the file holds (issue #232)", agentsTokenFile, len(got.ghToken), len(tc.token))
			}

			if got.githubToken != unsetMarker {
				t.Errorf("the real gh still had GITHUB_TOKEN: with a token file present the wrapper hands only GH_TOKEN, so gh cannot fall back to the maintainer's token")
			}

			// The gh CLI reads an enterprise token from these two as well: a stale one inherited from the VM's
			// environment would let gh act as its holder, although the agents have only the App's token (PRD #161
			// milestone 3).
			for _, inherited := range []struct {
				name string
				got  string
			}{
				{name: "GH_ENTERPRISE_TOKEN", got: got.ghEnterpriseToken},
				{name: "GITHUB_ENTERPRISE_TOKEN", got: got.githubEnterpriseToken},
			} {
				if inherited.got != unsetMarker {
					t.Errorf("the real gh still had %s: the wrapper must unset every GitHub token variable it does not hand gh itself, so a stale enterprise token cannot stand in for the agents App's token (PRD #161 milestone 3)", inherited.name)
				}
			}

			wantArgs := []string{"auth", "git-credential", "get"}
			if diff := cmp.Diff(wantArgs, got.argv); diff != "" {
				t.Errorf("the wrapper did not pass its arguments through unchanged (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(helperStdin, got.stdin); diff != "" {
				t.Errorf("the wrapper did not pass stdin through unchanged, and gh auth git-credential reads it (-want +got):\n%s", diff)
			}

			secrets := []secret{{kind: "App installation token", value: tc.token}, {kind: "maintainer token", value: personal}, {kind: "enterprise token", value: enterprise}, {kind: "GitHub enterprise token", value: githubEnterprise}}
			assertNoSecrets(t, "the wrapper's stdout", stdout, secrets)
			assertNoSecrets(t, "the wrapper's stderr", stderr, secrets)
		})
	}
}

// refreshTarget is the target the maintainer runs in the host clone on the Mac to refresh the agents App's
// token, and the two words that place it: every refusal of the wrapper names them, so an agent whose gh stops
// working reads what to ask for instead of a bare 401 (PRD #161 milestone 1, and milestone 3, which removed the
// GITHUB_TOKEN fallback this message replaces).
const (
	refreshTarget      = "make agents-token-refresh-start"
	refreshTargetPlace = "host clone on the Mac"
)

// refusedForMissingTokenFile is the message a wrapper without a token file must print: the refresh target, where
// to run it, and the file it could not read. The wording around these fragments is not pinned, they are.
func assertRefusalNamesTheRefreshTarget(t *testing.T, stderr string) {
	t.Helper()

	for _, want := range []string{refreshTarget, refreshTargetPlace} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the wrapper's stderr does not hold %q, so the next agent whose gh stops working cannot tell what to ask the maintainer for: %s", want, stderr)
		}
	}
}

// TestGHAppTokenRefusesWithoutATokenFile pins PRD #161 milestone 3's first acceptance criterion: without the
// App's token file, gh fails - even with GITHUB_TOKEN in its environment - and the error names the refresh
// target. Milestone 1 fell back to GITHUB_TOKEN here; with the maintainer's token out of the VM there is
// nothing left to fall back to, so a silent fallback would be a gh that quietly acts as nobody.
func TestGHAppTokenRefusesWithoutATokenFile(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	record := filepath.Join(sb.root, "gh-record")

	personal := fmt.Sprintf("ghp_%036x", 9)

	env := sb.ghEnv(writeFakeRealGh(t, sb), record)
	env["GITHUB_TOKEN"] = personal

	stdout, stderr, err := sb.run(t, scriptPath(t, ghWrapperScript), []string{"auth", "git-credential", "get"}, env, helperStdin)
	if err == nil {
		t.Fatalf("the wrapper exited 0 with no token file: without the agents App's token gh must fail and name the refresh target, even when GITHUB_TOKEN is in the environment (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if _, statErr := os.Stat(record); statErr == nil {
		t.Fatalf("the wrapper ran the real gh with no token file: with the maintainer's token out of the VM there is no fallback, so gh must not run at all (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	assertRefusalNamesTheRefreshTarget(t, stderr)

	if !strings.Contains(stderr, agentsTokenFile) {
		t.Errorf("the wrapper's stderr does not name %s, the file it could not read, so a maintainer cannot tell which file the refresh has to write: %s", agentsTokenFile, stderr)
	}

	if strings.Contains(stderr, personal) || strings.Contains(stdout, personal) {
		t.Errorf("the wrapper printed the GITHUB_TOKEN it was given: a value must never reach a pane or a log (CLAUDE.md rule 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

// TestGitCredentialHelperRefusesWithoutATokenFile pins the other half of the same criterion: git push goes
// through the credential helper the VM configures, which runs the wrapper (`!<gh> auth git-credential`), so
// with no token file `git credential fill` fails too and carries the wrapper's message - never a credential
// made from GITHUB_TOKEN.
func TestGitCredentialHelperRefusesWithoutATokenFile(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	record := filepath.Join(sb.root, "gh-record")
	wrapper := scriptPath(t, ghWrapperScript)

	personal := fmt.Sprintf("ghp_%036x", 9)

	// The helper line dev/lima.yaml and make vm-gh-wrapper write into the VM user's global git config.
	helper := "!" + wrapper + " auth git-credential"
	writeFileMode(t, filepath.Join(sb.vmHome, ".gitconfig"),
		[]byte("[credential \"https://github.com\"]\n\thelper = \""+helper+"\"\n"), 0o600)

	env := sb.ghEnv(writeFakeRealGh(t, sb), record)
	env["GITHUB_TOKEN"] = personal
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GIT_TERMINAL_PROMPT"] = "0"

	stdout, stderr, err := runGitCredentialFill(t, sb, env)
	if err == nil {
		t.Fatalf("git credential fill succeeded with no token file: the helper runs the wrapper, so a git push must fail and name the refresh target, even when GITHUB_TOKEN is in the environment (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if _, statErr := os.Stat(record); statErr == nil {
		t.Fatalf("the credential helper ran the real gh with no token file: with the maintainer's token out of the VM there is no fallback, so gh must not run at all (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	assertRefusalNamesTheRefreshTarget(t, stdout+stderr)
}

// runGitCredentialFill runs `git credential fill` with the VM user's home, so git reads the helper this test
// wrote, and with the helper's standard input.
func runGitCredentialFill(t *testing.T, sb *sandbox, env map[string]string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("git", "credential", "fill")
	cmd.Env = envList(env)
	cmd.Dir = sb.hostHome
	cmd.Stdin = strings.NewReader(helperStdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// TestGHAppTokenRefusesABadTokenFile pins that an expired, malformed or group-readable token file makes the
// wrapper fail without running gh, name make agents-token-refresh-start in the host clone on the Mac, and never
// print the file's content.
func TestGHAppTokenRefusesABadTokenFile(t *testing.T) {
	t.Parallel()

	token := oldFormatAppToken(11)
	newToken := newFormatAppToken(11)
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
		// The format GitHub mints since 2026 is accepted, so the checks around it must still hold (issue #232).
		{name: "expired in the new format", content: newToken + "\n" + past + "\n", mode: 0o600, token: newToken},
		{name: "one line in the new format", content: newToken + "\n", mode: 0o600, token: newToken},
		{name: "group readable in the new format", content: newToken + "\n" + future + "\n", mode: 0o640, token: newToken},
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

			// Third review, item 4: the refusal names the command AND where to run it - the host clone on the
			// Mac, the only place the App's key and its path live.
			for _, want := range []string{refreshTarget, refreshTargetPlace} {
				if !strings.Contains(stderr, want) {
					t.Errorf("the wrapper's stderr does not hold %q, so a maintainer cannot tell where to start the refresh again (third review, item 4): %s", want, stderr)
				}
			}

			// Review item 10: the refusal must not teach an agent to delete the token file or to fall back to the
			// maintainer's GITHUB_TOKEN; both are a maintainer's steps, and the rollback lives in the docs.
			lowered := strings.ToLower(stderr)

			for _, never := range []string{"delete", "github_token"} {
				if strings.Contains(lowered, never) {
					t.Errorf("the wrapper's stderr says %q: an agent must not delete the token file or reach for GITHUB_TOKEN, it must ask the maintainer (review item 10): %s", never, stderr)
				}
			}

			if !strings.Contains(stderr, "maintainer") {
				t.Errorf("the wrapper's stderr does not say who can fix it: ask the maintainer to run make agents-token-refresh-start in the host clone on the Mac (review item 10): %s", stderr)
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

// TestGHAppTokenRefusesADangerousToken pins the other half of the new token format (issue #232): accepting
// GitHub's longer, punctuated tokens must not weaken the check that keeps a value from breaking the two-line
// token file, a header or the shell. An empty token, whitespace, a quote, a backslash, $, ; and a control
// character are still refused without running gh, and the token is never printed. A newline cannot sit in a
// token of this file at all: it reaches the file as a line of its own, so the last case pins that a file with
// a line between the token and its expiry is refused too.
func TestGHAppTokenRefusesADangerousToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		token string
	}{
		{name: "an empty token", token: ""},
		{name: "a space", token: newFormatAppToken(3) + " x"},
		{name: "a tab", token: newFormatAppToken(3) + "\tx"},
		{name: "a single quote", token: newFormatAppToken(3) + "'x"},
		{name: "a double quote", token: newFormatAppToken(3) + `"x`},
		{name: "a backslash", token: newFormatAppToken(3) + `\x`},
		{name: "a dollar sign", token: newFormatAppToken(3) + "$x"},
		{name: "a semicolon", token: newFormatAppToken(3) + ";x"},
		{name: "a control character", token: newFormatAppToken(3) + "\x01"},
		{name: "a line between the token and its expiry", token: newFormatAppToken(3) + "\nx"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			record := filepath.Join(sb.root, "gh-record")
			future := strconv.FormatInt(time.Now().Add(45*time.Minute).Unix(), 10)

			writeVMTokenFile(t, sb, tc.token+"\n"+future+"\n", 0o600)

			env := sb.ghEnv(writeFakeRealGh(t, sb), record)

			stdout, stderr, err := sb.run(t, scriptPath(t, ghWrapperScript), []string{"auth", "git-credential", "get"}, env, helperStdin)
			if err == nil {
				t.Fatalf("the wrapper exited 0 with a token that holds %s: a token file that cannot be read back whole must be refused (issue #232)", tc.name)
			}

			if _, err := os.Stat(record); err == nil {
				t.Fatalf("the wrapper ran the real gh with a token that holds %s: it must refuse the file and exit without calling gh (issue #232)", tc.name)
			}

			if strings.TrimSpace(stderr) == "" {
				t.Errorf("the wrapper says nothing about refusing a token that holds %s", tc.name)
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
