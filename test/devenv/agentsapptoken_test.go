// Pins the host side of the agents App's credential (PRD #161 milestone 1): dev/agents-app-token.sh mints
// an installation token from the App's private key into the VM, writes the agents' bot identity beside it,
// refreshes the token before it expires, and keeps the token, the JWT and the key off every command line,
// log line and output. The design is .dot-agent-deck/prd161m1-design.md.
package devenv_test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mintFixture is one fake environment for a run of dev/agents-app-token.sh: a sandbox, an invented key
// written into it, and the fake GitHub API the script mints from.
type mintFixture struct {
	sb  *sandbox
	api *fakeGitHub
	key inventedKey
	env map[string]string
}

// newMintFixture builds a fixture whose environment names the invented key, the fake API and the fake VM.
func newMintFixture(t *testing.T) *mintFixture {
	t.Helper()

	sb := newSandbox(t)
	key := writeInventedKey(t, filepath.Join(sb.root, "key", "agents-app.pem"), 0o600)
	api := newFakeGitHub(t, key.public)

	env := sb.baseEnv()
	env["SHIFT2EU_AGENTS_APP_ID"] = api.appID
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = api.installationID
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = key.path
	env["GITHUB_API_URL"] = api.url

	return &mintFixture{sb: sb, api: api, key: key, env: env}
}

// secrets are the values no command line, log line or output of a run may hold: every line of the invented
// key, the tokens the fake API handed out, and the JWT it saw.
func (f *mintFixture) secrets() []secret {
	secrets := keySecrets(f.key.pem)

	for _, token := range f.api.minted() {
		secrets = append(secrets, secret{kind: "App installation token", value: token})
	}

	for _, jwt := range f.api.bearerTokens() {
		secrets = append(secrets, secret{kind: "App JWT", value: jwt})
	}

	return secrets
}

// assertNoLeaks fails when a secret reached a command line, a log line, an output, or a file of the host
// machine. The VM's home holds the token on purpose, and the invented key and the shims hold what they are.
func (f *mintFixture) assertNoLeaks(t *testing.T, stdout, stderr string) {
	t.Helper()

	secrets := f.secrets()
	assertNoSecrets(t, "the script's stdout", stdout, secrets)
	assertNoSecrets(t, "the script's stderr", stderr, secrets)
	assertNoSecrets(t, "the argv of the commands the script ran", f.sb.argvLogText(t), secrets)
	assertNoSecretsInTree(t, f.sb.root, []string{
		f.sb.vmHome,
		filepath.Dir(f.key.path),
		f.sb.binDir,
		f.sb.argvLog,
	}, secrets)
}

// readTwoLines reads a file the script wrote into the VM and checks its mode: exactly two lines.
func readTwoLines(t *testing.T, path string, mode os.FileMode) [2]string {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s is not there: %v", path, err)
	}

	if got := info.Mode().Perm(); got != mode {
		t.Errorf("%s is mode %04o, want %04o: the VM user reads it, and no other account may", path, got, mode)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	lines := strings.Split(string(body), "\n")
	if len(lines) == 3 && lines[2] == "" {
		lines = lines[:2]
	}

	if len(lines) != 2 {
		t.Fatalf("%s must hold exactly two lines, not %d: the design gives each file exactly two lines", path, len(lines))
	}

	return [2]string{lines[0], lines[1]}
}

// tokenLine returns the first line of a file, ok false when the file is not there yet or does not hold two
// lines: the refresh test polls with it, so it never fails a test on a file being written.
func tokenLine(path string) (string, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 2 {
		return "", false
	}

	return lines[0], true
}

func TestAgentsAppTokenMintWritesTheTokenAndTheBotIdentity(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	startedAt := time.Now().Unix()

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint exited non-zero: %v\nstdout: %s\nstderr: %s\nthe fake API refused: %s", err, stdout, stderr, strings.Join(f.api.recordedComplaints(), "; "))
	}

	f.assertNoLeaks(t, stdout, stderr)

	if complaints := f.api.recordedComplaints(); len(complaints) != 0 {
		t.Fatalf("the fake API refused a request: %s", strings.Join(complaints, "; "))
	}

	tokens := f.api.minted()
	if len(tokens) != 1 {
		t.Fatalf("the fake API minted %d tokens, want one: mint writes one installation token", len(tokens))
	}

	wantPosts := []string{"/app/installations/" + f.api.installationID + "/access_tokens"}
	if got := f.api.posted(); !slices.Equal(got, wantPosts) {
		t.Errorf("the mint posted to %v, want %v: the token is minted for the installation the installation ID names", got, wantPosts)
	}

	if accepts := f.api.accepted(); len(accepts) == 0 ||
		!slices.ContainsFunc(accepts, func(accept string) bool {
			return strings.Contains(accept, "application/vnd.github+json")
		}) {
		t.Errorf("no request sent Accept: application/vnd.github+json, the media type the design gives every call")
	}

	token := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)
	if token[0] != tokens[0] {
		t.Errorf("%s does not hold the installation token the fake API handed out", agentsTokenFile)
	}

	expiry, err := strconv.ParseInt(token[1], 10, 64)
	if err != nil {
		t.Fatalf("%s's second line is not the expiry as Unix seconds: %v", agentsTokenFile, err)
	}

	if expiry <= startedAt {
		t.Errorf("%s's expiry %d is not after the mint started (%d): the VM would hold an expired token", agentsTokenFile, expiry, startedAt)
	}

	if apiExpiry := f.api.expiryOf(0); expiry > apiExpiry {
		t.Errorf("%s's expiry %d is later than the expires_at the fake API returned (%d)", agentsTokenFile, expiry, apiExpiry)
	}

	identity := readTwoLines(t, f.sb.vmPath(agentsIdentityFile), 0o600)

	wantName := f.api.slug + "[bot]"
	if identity[0] != wantName {
		t.Errorf("%s's first line is %q, want the App's bot user.name %q", agentsIdentityFile, identity[0], wantName)
	}

	// GitHub attributes <bot user id>+<slug>[bot]@users.noreply.github.com to an App's bot, and the number
	// is the bot user's id from GET /users/<slug>[bot], not the App ID (design, Identity).
	wantEmail := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)
	if identity[1] != wantEmail {
		t.Errorf("%s's second line is %q, want %q: the number is the bot user's id, %d, not the App ID %s",
			agentsIdentityFile, identity[1], wantEmail, f.api.botUserID, f.api.appID)
	}

	if _, err := os.Stat(filepath.Join(f.sb.hostHome, filepath.FromSlash(agentsTokenFile))); err == nil {
		t.Errorf("the mint wrote %s into the host's home; the token belongs in the VM and nowhere else", agentsTokenFile)
	}
}

// TestAgentsAppTokenMintRefusesBadInput pins that a mint without its inputs, and with a key any other account
// can read, stops before it talks to GitHub and names the input it refused.
func TestAgentsAppTokenMintRefusesBadInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		mutate     func(t *testing.T, f *mintFixture)
		wantStderr string
	}{
		{
			name:       "no App ID",
			mutate:     func(_ *testing.T, f *mintFixture) { delete(f.env, "SHIFT2EU_AGENTS_APP_ID") },
			wantStderr: "SHIFT2EU_AGENTS_APP_ID",
		},
		{
			name:       "no installation ID",
			mutate:     func(_ *testing.T, f *mintFixture) { delete(f.env, "SHIFT2EU_AGENTS_APP_INSTALLATION_ID") },
			wantStderr: "SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		},
		{
			name:       "no key file",
			mutate:     func(_ *testing.T, f *mintFixture) { delete(f.env, "SHIFT2EU_AGENTS_APP_KEY_FILE") },
			wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
		},
		{
			name: "world readable key",
			mutate: func(t *testing.T, f *mintFixture) {
				t.Helper()

				writeFileMode(t, f.key.path, f.key.pem, 0o604)
			},
			wantStderr: "SHIFT2EU_AGENTS_APP_KEY_FILE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			tc.mutate(t, f)

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err == nil {
				t.Fatalf("mint exited 0 with %s; it must refuse it and exit non-zero\nstdout: %s", tc.name, stdout)
			}

			if !strings.Contains(stderr, tc.wantStderr) {
				t.Errorf("the refusal does not name %s: stderr: %s", tc.wantStderr, stderr)
			}

			if got := f.api.posted(); len(got) != 0 {
				t.Errorf("mint posted to %v before it refused %s: a refused mint sends nothing to GitHub", got, tc.name)
			}

			if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
				t.Errorf("mint wrote %s even though it refused %s", agentsTokenFile, tc.name)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenMintReportsARefusedMint pins that a 401 from GitHub fails the mint, writes no token, and
// prints a message a maintainer can act on, never the bare status and never a secret.
func TestAgentsAppTokenMintReportsARefusedMint(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	f.api.refuseMintsWith(http.StatusUnauthorized)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err == nil {
		t.Fatalf("mint exited 0 although the fake API answered 401\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if got := f.api.posted(); len(got) != 1 {
		t.Errorf("the mint posted to %v, want one access_tokens request", got)
	}

	message := strings.TrimSpace(stderr)
	if message == "" {
		t.Fatal("mint printed nothing to stderr about the 401")
	}

	if message == "401" || message == "HTTP 401" || message == "Unauthorized" {
		t.Errorf("mint's stderr is the bare status %q: it must say what failed", message)
	}

	if !strings.Contains(strings.ToLower(message), "token") {
		t.Errorf("mint's stderr does not say that no token could be minted: %s", message)
	}

	if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
		t.Errorf("mint wrote %s although the fake API answered 401", agentsTokenFile)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenRefreshReplacesTheTokenAndSaysHowToStartItAgain pins the loop make deck starts: it mints
// again before the token expires, replaces the VM's copy, and on SIGTERM says it stopped and names the
// command that starts it again.
func TestAgentsAppTokenRefreshReplacesTheTokenAndSaysHowToStartItAgain(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	f.env["SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS"] = "1"
	// A margin larger than any token's lifetime keeps the loop minting at every check, so the test sees a
	// refresh within a few seconds and needs no long sleep.
	f.env["SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS"] = strconv.Itoa(24 * 60 * 60)

	startedAt := time.Now().Unix()

	cmd := exec.Command("sh", scriptPath(t, agentsMintScript), "refresh")
	cmd.Env = envList(f.env)
	cmd.Dir = f.sb.hostHome

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start refresh: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	t.Cleanup(func() {
		if complaints := f.api.recordedComplaints(); len(complaints) != 0 {
			t.Logf("the fake API refused a request: %s", strings.Join(complaints, "; "))
		}
	})

	waitFor(t, 30*time.Second, "the refresh to replace the VM's first token", func() bool {
		tokens := f.api.minted()
		if len(tokens) < 2 {
			return false
		}

		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return ok && line != tokens[0]
	})

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to the refresh: %v", err)
	}

	// A signal may end the loop with 0 or with a signal's status; only a failed mint is specified non-zero,
	// so the exit status is not pinned here. What it says before it exits is.
	_ = cmd.Wait()

	stopped := stderr.String()
	if !strings.Contains(stopped, "agents App token refresh stopped") {
		t.Errorf("the refresh's stderr does not say 'agents App token refresh stopped', so a stopped refresh is invisible: %s", stopped)
	}

	if !strings.Contains(stopped, "make agents-token-refresh") {
		t.Errorf("the refresh's stderr does not name make agents-token-refresh, the command that starts it again: %s", stopped)
	}

	tokens := f.api.minted()
	if len(tokens) < 2 {
		t.Fatalf("the fake API minted %d tokens, want at least two: the refresh replaces the token before it expires", len(tokens))
	}

	token := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)

	refreshed := slices.Index(tokens, token[0])
	if refreshed < 0 {
		t.Fatalf("%s does not hold a token the fake API handed out", agentsTokenFile)
	}

	if refreshed == 0 {
		t.Errorf("%s still holds the first token: each newer token must replace the file, so a running agent's next gh uses it", agentsTokenFile)
	}

	expiry, err := strconv.ParseInt(token[1], 10, 64)
	if err != nil {
		t.Fatalf("%s's second line is not the expiry as Unix seconds: %v", agentsTokenFile, err)
	}

	if expiry <= startedAt {
		t.Errorf("%s's expiry %d is not after the refresh started (%d)", agentsTokenFile, expiry, startedAt)
	}

	if apiExpiry := f.api.expiryOf(refreshed); expiry > apiExpiry {
		t.Errorf("%s's expiry %d is later than the expires_at of the token it holds (%d)", agentsTokenFile, expiry, apiExpiry)
	}

	f.assertNoLeaks(t, stdout.String(), stopped)
}
