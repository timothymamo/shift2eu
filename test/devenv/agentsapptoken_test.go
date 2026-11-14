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
	env["SHIFT2EU_AGENTS_GITHUB_API_URL"] = api.url
	// The gh the VM runs, checked by mint before it warns that this VM predates the wrapper: a path that is
	// not there unless a test creates it keeps the warning independent of what this machine has installed
	// (PRD #161 milestone 1 review, item 3).
	env["SHIFT2EU_VM_REAL_GH_PATH"] = filepath.Join(sb.root, "vm-real-gh")

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
		{
			// Second review item 8: a key reached through a symlink is refused with the message for a
			// symlink, not with the one for a file that is not readable, so a maintainer knows which of the
			// two it is. The key behind the link is a regular 0600 file, so only the link is wrong.
			name: "a symlinked key file",
			mutate: func(t *testing.T, f *mintFixture) {
				t.Helper()

				link := filepath.Join(f.sb.root, "key", "agents-app-link.pem")
				if err := os.Symlink(f.key.path, link); err != nil {
					t.Fatalf("symlink %s: %v", link, err)
				}

				f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = link
			},
			wantStderr: "symlink",
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

// TestAgentsAppTokenMintWarnsAboutAVMWithoutTheGHWrapper pins review item 3: a VM created before the gh wrapper
// existed keeps gh only where dev/lima.yaml puts it, so the git credential helper runs the real gh and no token
// reaches it. mint must say so, name the target that upgrades the VM, and still write the token.
func TestAgentsAppTokenMintWarnsAboutAVMWithoutTheGHWrapper(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		prepare func(t *testing.T, path string)
		want    bool
	}{
		{
			name:    "the VM predates the gh wrapper",
			prepare: func(*testing.T, string) {},
			want:    true,
		},
		{
			name: "the VM has the wrapper's real gh",
			prepare: func(t *testing.T, path string) {
				t.Helper()

				writeFileMode(t, path, []byte("#!/bin/sh\n"), 0o755)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			tc.prepare(t, f.env["SHIFT2EU_VM_REAL_GH_PATH"])

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err != nil {
				t.Fatalf("mint exited non-zero: a VM that predates the gh wrapper is a warning, not a failure: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			// The warning is worth nothing if the token was not written: the agents' work goes on either way.
			readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)

			got := strings.Contains(stderr, "predates the gh wrapper")
			if got != tc.want {
				t.Fatalf("mint says %s the VM predates the gh wrapper, want %v: a VM created before dev/lima.yaml installed the wrapper has no token plumbing, and one that has it must not see the warning\nstderr: %s",
					map[bool]string{true: "nothing about", false: "that"}[got], tc.want, stderr)
			}

			if !tc.want {
				return
			}

			for _, want := range []string{"make vm-gh-wrapper", "make vm-delete vm-create"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("the warning does not name %q, so a maintainer cannot upgrade the VM (review item 3): %s", want, stderr)
				}
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenMintWritesAnExpiryInsideTheMargin pins review item 4: the expiry the VM holds is a margin
// short of both the Mac's own hour and GitHub's expires_at, so a VM whose clock lags does not lose the token
// before the refresh replaces it.
func TestAgentsAppTokenMintWritesAnExpiryInsideTheMargin(t *testing.T) {
	t.Parallel()

	// 300 seconds of the token's life are given up (PRD #161 milestone 1 review, item 4), and the script reads
	// the clock on its own, so the bounds allow a few seconds of drift between this test's clock reads and the
	// script's.
	const (
		margin    int64 = 300
		tolerance int64 = 3
	)

	cases := []struct {
		name     string
		lifetime time.Duration
	}{
		{name: "the hour GitHub hands out", lifetime: time.Hour},
		{name: "a token GitHub says lasts half an hour", lifetime: 30 * time.Minute},
		{name: "a token GitHub says lasts two hours", lifetime: 2 * time.Hour},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			f.api.setTokenLifetime(tc.lifetime)

			startedAt := time.Now().Unix()

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err != nil {
				t.Fatalf("mint exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			token := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)

			expiry, err := strconv.ParseInt(token[1], 10, 64)
			if err != nil {
				t.Fatalf("%s's second line is not the expiry as Unix seconds: %v", agentsTokenFile, err)
			}

			apiExpiry := f.api.expiryOf(0)
			want := min(startedAt+3600-margin, apiExpiry-margin)

			if expiry > want+tolerance {
				t.Errorf("%s's expiry is %d, want at most %d: that is the earlier of the Mint's own now - %d (%d) and expires_at - %d (%d). A token that outlives the margin in the VM fails the moment the VM's clock lags the Mac's (review item 4)",
					agentsTokenFile, expiry, want+tolerance, margin, startedAt+3600-margin, margin, apiExpiry-margin)
			}

			if expiry < want-tolerance {
				t.Errorf("%s's expiry is %d, want at least %d: the token is given up earlier than the margin alone explains", agentsTokenFile, expiry, want-tolerance)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenRefusesADangerousAPIURL pins review item 9: the API URL hook is renamed to
// SHIFT2EU_AGENTS_GITHUB_API_URL, and a value that could make curl talk to another host (an @, a quote, a
// backslash, whitespace, a newline) or plain http to anything but this machine is refused before any request.
func TestAgentsAppTokenRefusesADangerousAPIURL(t *testing.T) {
	t.Parallel()

	refused := []struct {
		name string
		url  string
	}{
		{name: "plain http to a remote host", url: "http://example.com/"},
		{name: "plain http to a remote host with a port", url: "http://example.com:8080/api/v3"},
		//nolint:gosec // G101: this is a URL the script must refuse, not a credential
		{name: "an @ that makes the authority look like a user name", url: "http://localhost:8080@evil.example/"},
		{name: "an @ over https", url: "https://agents@evil.example/"},
		{name: "a double quote", url: `http://127.0.0.1:8080/"`},
		{name: "a backslash", url: `http://127.0.0.1:8080/\`},
		{name: "a space", url: "http://127.0.0.1:8080/a b"},
		{name: "a tab", url: "http://127.0.0.1:8080/a\tb"},
		{name: "a newline", url: "http://127.0.0.1:8080/a\nb"},
		{name: "no scheme at all", url: "api.github.com"},
	}

	for _, tc := range refused {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			f.env["SHIFT2EU_AGENTS_GITHUB_API_URL"] = tc.url

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err == nil {
				t.Fatalf("mint exited 0 with the API URL %q, which can send the App's JWT to another host\nstdout: %s", tc.url, stdout)
			}

			if !strings.Contains(stderr, "SHIFT2EU_AGENTS_GITHUB_API_URL") {
				t.Errorf("the refusal does not name SHIFT2EU_AGENTS_GITHUB_API_URL, so a maintainer cannot tell what to fix: %s", stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "curl"); len(calls) != 0 {
				t.Errorf("mint ran curl %d time(s) with the API URL %q: a refused URL must not reach the network", len(calls), tc.url)
			}

			if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
				t.Errorf("mint wrote %s although it refused the API URL", agentsTokenFile)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}

	t.Run("accepts the fake API on this machine", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)

		stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
		if err != nil {
			t.Fatalf("mint exited non-zero with the fake API's own URL %q: %v\nstdout: %s\nstderr: %s", f.env["SHIFT2EU_AGENTS_GITHUB_API_URL"], err, stdout, stderr)
		}

		readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)
	})

	t.Run("accepts https://api.github.com", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)
		f.env["SHIFT2EU_AGENTS_GITHUB_API_URL"] = "https://api.github.com"

		// curl cannot leave this machine: the test must never reach the real GitHub API, and the URL being
		// refused is what it proves cannot have happened.
		f.sb.blockCurl(t)

		stdout, stderr, _ := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")

		if strings.Contains(stderr, "SHIFT2EU_AGENTS_GITHUB_API_URL") {
			t.Fatalf("mint refused https://api.github.com, the API the App is meant to talk to: %s", stderr)
		}

		if calls := invocations(f.sb.argvLogText(t), "curl"); len(calls) == 0 {
			t.Fatalf("mint never ran curl with https://api.github.com, so this test cannot tell whether the URL was accepted\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		f.assertNoLeaks(t, stdout, stderr)
	})
}

// TestAgentsAppTokenGivesCurlTheQFirst pins review item 11 (N1): curl must not read the maintainer's ~/.curlrc,
// which can redirect a request or add a header, so -q comes before every other argument.
func TestAgentsAppTokenGivesCurlTheQFirst(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	calls := invocations(f.sb.argvLogText(t), "curl")
	if len(calls) == 0 {
		t.Fatalf("mint ran no curl at all")
	}

	for i, args := range calls {
		if len(args) == 0 || args[0] != "-q" {
			t.Errorf("curl call %d starts with %v, want -q first: otherwise curl reads ~/.curlrc, which can send the App's JWT somewhere else (review item 11, N1)", i+1, args)
		}
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenRefusesToWriteThroughASymlink pins review item 11 (N4) and the second review's item 7: the
// VM's write script refuses a symlinked ~/.config/shift2eu and a token or identity file that is a symlink or
// not a regular file, and a ~/.config/shift2eu that resolves, with pwd -P, to a directory outside the VM
// user's real home, so nothing an agent points at receives the token.
func TestAgentsAppTokenRefusesToWriteThroughASymlink(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		prepare func(t *testing.T, sb *sandbox)
		check   func(t *testing.T, sb *sandbox)
	}{
		{
			name: "the config directory is a symlink",
			prepare: func(t *testing.T, sb *sandbox) {
				t.Helper()

				elsewhere := filepath.Join(sb.vmHome, "elsewhere")
				if err := os.MkdirAll(elsewhere, 0o700); err != nil {
					t.Fatalf("create %s: %v", elsewhere, err)
				}

				if err := os.MkdirAll(sb.vmPath(".config"), 0o700); err != nil {
					t.Fatalf("create %s/.config: %v", sb.vmHome, err)
				}

				if err := os.Symlink(elsewhere, sb.vmPath(filepath.Join(".config", "shift2eu"))); err != nil {
					t.Fatalf("symlink the config directory: %v", err)
				}
			},
			check: func(t *testing.T, sb *sandbox) {
				t.Helper()

				for _, name := range []string{"agents-app-token", "agents-app-identity"} {
					if _, err := os.Stat(filepath.Join(sb.vmHome, "elsewhere", name)); err == nil {
						t.Errorf("the mint wrote %s through the symlinked config directory, so an agent that points it elsewhere receives the token", name)
					}
				}
			},
		},
		{
			name: "the token file is a symlink",
			prepare: func(t *testing.T, sb *sandbox) {
				t.Helper()

				target := filepath.Join(sb.root, "outside-token")
				writeFileMode(t, target, []byte("unchanged\n"), 0o600)

				if err := os.MkdirAll(filepath.Dir(sb.vmPath(agentsTokenFile)), 0o700); err != nil {
					t.Fatalf("create the config directory: %v", err)
				}

				if err := os.Symlink(target, sb.vmPath(agentsTokenFile)); err != nil {
					t.Fatalf("symlink %s: %v", agentsTokenFile, err)
				}
			},
			check: func(t *testing.T, sb *sandbox) {
				t.Helper()

				body, err := os.ReadFile(filepath.Join(sb.root, "outside-token"))
				if err != nil {
					t.Fatalf("read the symlink's target: %v", err)
				}

				if string(body) != "unchanged\n" {
					t.Errorf("the mint wrote through the symlinked token file, so the token landed wherever an agent pointed it")
				}
			},
		},
		{
			name: "the token file is not a regular file",
			prepare: func(t *testing.T, sb *sandbox) {
				t.Helper()

				if err := os.MkdirAll(sb.vmPath(agentsTokenFile), 0o700); err != nil {
					t.Fatalf("create %s as a directory: %v", agentsTokenFile, err)
				}
			},
			check: func(t *testing.T, sb *sandbox) {
				t.Helper()

				info, err := os.Lstat(sb.vmPath(agentsTokenFile))
				if err != nil {
					t.Fatalf("stat %s: %v", agentsTokenFile, err)
				}

				if !info.IsDir() {
					t.Errorf("the mint replaced the directory at %s, which is not a token file it may overwrite", agentsTokenFile)
				}
			},
		},
		{
			// Second review item 7: the config directory is not itself a symlink, but it resolves, with pwd
			// -P, to a directory outside the VM user's real home: an accidental symlink one level up must not
			// carry the token out of the home the VM keeps it in.
			name: "the config directory resolves outside the VM user's home",
			prepare: func(t *testing.T, sb *sandbox) {
				t.Helper()

				outside := filepath.Join(sb.root, "outside-config")
				if err := os.MkdirAll(outside, 0o700); err != nil {
					t.Fatalf("create %s: %v", outside, err)
				}

				if err := os.Symlink(outside, sb.vmPath(".config")); err != nil {
					t.Fatalf("symlink %s/.config: %v", sb.vmHome, err)
				}
			},
			check: func(t *testing.T, sb *sandbox) {
				t.Helper()

				for _, name := range []string{"agents-app-token", "agents-app-identity"} {
					if _, err := os.Stat(filepath.Join(sb.root, "outside-config", name)); err == nil {
						t.Errorf("the mint wrote %s outside the VM user's home, through a .config that resolves elsewhere: the VM writes the token only under its own home (second review, item 7)", name)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			tc.prepare(t, f.sb)

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err == nil {
				t.Fatalf("mint exited 0 although %s: the VM's write script must refuse it\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if strings.TrimSpace(stderr) == "" {
				t.Errorf("mint says nothing about refusing to write: %s", tc.name)
			}

			tc.check(t, f.sb)
			f.assertNoLeaks(t, stdout, stderr)
		})
	}
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

// TestAgentsAppTokenMintLooksTheBotUserUpWithTheInstallationToken pins the second review's item 4: the bot user is
// looked up after the token is minted, with that installation token. GitHub refuses the App's JWT on /users, and a
// lookup without any authorization is no better.
func TestAgentsAppTokenMintLooksTheBotUserUpWithTheInstallationToken(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	// This is the fake API GitHub gives an App: GET /users/<slug>[bot] refuses the App's JWT, so the bot user's
	// id can only come from the installation token the mint just received (second review, item 4).
	f.api.refuseJWTOnUsers()

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint exited non-zero: %v\nstdout: %s\nstderr: %s\nthe fake API refused: %s",
			err, stdout, stderr, strings.Join(f.api.recordedComplaints(), "; "))
	}

	if complaints := f.api.recordedComplaints(); len(complaints) != 0 {
		t.Fatalf("the fake API refused a request: %s. The bot user is looked up with the installation token the mint just got, because GitHub does not accept the App's JWT on /users (second review, item 4)",
			strings.Join(complaints, "; "))
	}

	if got := f.api.botUserLookups(); got != 1 {
		t.Errorf("the fake API answered %d bot user lookups with the bot user, want one: the identity is read from the one lookup", got)
	}

	requests := f.api.requestOrder()

	post := slices.Index(requests, "POST /app/installations/"+f.api.installationID+"/access_tokens")
	lookup := slices.Index(requests, "GET /users/"+f.api.slug+"[bot]")

	if post < 0 || lookup < 0 {
		t.Fatalf("the fake API saw the requests %v, want a token mint and a bot user lookup", requests)
	}

	if lookup < post {
		t.Errorf("the bot user was looked up before the token was minted (requests: %v): the lookup needs the installation token, so it comes after the mint (second review, item 4)", requests)
	}

	identity := readTwoLines(t, f.sb.vmPath(agentsIdentityFile), 0o600)

	if want := f.api.slug + "[bot]"; identity[0] != want {
		t.Errorf("%s's first line is %q, want the App's bot user.name %q", agentsIdentityFile, identity[0], want)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenIgnoresAClockThatIsNotAClock pins the second review's item 6: the clock= value a VM prints is
// accepted only as 1 to 12 digits, so a longer reading (an overflow, a nanosecond timestamp, whatever an agent
// leaves behind) is not compared with the Mac's clock, while a 12-digit reading still is.
func TestAgentsAppTokenIgnoresAClockThatIsNotAClock(t *testing.T) {
	t.Parallel()

	// The offset warning the script prints when it compared the VM's clock with the Mac's.
	const offsetWarning = "off the Mac's"

	cases := []struct {
		name        string
		clock       string
		wantWarning bool
	}{
		{name: "a 14-digit reading is no clock", clock: "17000000000000000"},
		{name: "a 13-digit reading is no clock", clock: "17000000000000"},
		{name: "a 12-digit reading is the last one accepted", clock: "170000000000", wantWarning: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			f.sb.useFakeVMClock(t, f.env, tc.clock)

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err != nil {
				t.Fatalf("mint exited non-zero with the VM's clock reading %s: a reading the script cannot use is no reason to leave the VM without a token: %v\nstdout: %s\nstderr: %s",
					tc.clock, err, stdout, stderr)
			}

			readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)

			if got := strings.Contains(stderr, offsetWarning); got != tc.wantWarning {
				t.Errorf("the mint compares the VM's clock reading %s with the Mac's = %v, want %v: only 1 to 12 digits are a clock the script may compare (second review, item 6)\nstderr: %s",
					tc.clock, got, tc.wantWarning, stderr)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenKeepsTheAPIOffCurlsArgv pins the second review's item 10: the API URL, and with it every
// header curl is configured with, reaches curl on its standard input and never on its command line, where every
// agent in the VM can read a command line and the JWT it carries.
func TestAgentsAppTokenKeepsTheAPIOffCurlsArgv(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	log := f.sb.argvLogText(t)

	calls := invocations(log, "curl")
	if len(calls) == 0 {
		t.Fatalf("mint ran no curl at all")
	}

	for i, args := range calls {
		for _, arg := range args {
			if strings.Contains(arg, f.api.url) {
				t.Errorf("curl call %d carries the API URL %q on its argv (%v): the URL and its headers go to curl on standard input (second review, item 10)", i+1, f.api.url, args)
			}
		}
	}

	if got, want := strings.Count(log, "curl-stdin-url=yes"), len(calls); got != want {
		t.Errorf("%d of the %d curl calls read the API URL from standard input, want every one: that is where the URL and the headers it carries belong (second review, item 10)", got, want)
	}

	f.assertNoLeaks(t, stdout, stderr)
}
