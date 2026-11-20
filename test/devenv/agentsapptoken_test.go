// Pins the host side of the agents App's credential (PRD #161 milestone 1): dev/agents-app-token.sh mints
// an installation token from the App's private key into the VM, writes the agents' bot identity beside it,
// refreshes the token before it expires, and keeps the token, the JWT and the key off every command line,
// log line and output. The design is .dot-agent-deck/prd161m1-design.md.
package devenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
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
	// The mint sets the fake VM user's global git identity (issue #241), and the git it runs for that reads the
	// machine's system configuration unless it is off: the fake VM home is the only git configuration a test
	// reads, and the machine's own never reaches the script.
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GIT_CONFIG_SYSTEM"] = os.DevNull

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
// machine. The VM's home holds the token on purpose, and the invented key and the shims hold what they are. The
// fake ssh's record of its standard input is the VM's side of the write, like the VM's home, so it is skipped
// too: the token must arrive there, and the argv log above still pins that it never reaches a command line.
func (f *mintFixture) assertNoLeaks(t *testing.T, stdout, stderr string) {
	t.Helper()

	secrets := f.secrets()
	assertNoSecrets(t, "the script's stdout", stdout, secrets)
	assertNoSecrets(t, "the script's stderr", stderr, secrets)
	assertNoSecrets(t, "the argv of the commands the script ran", f.sb.argvLogText(t), secrets)

	skip := []string{
		f.sb.vmHome,
		filepath.Dir(f.key.path),
		f.sb.binDir,
		f.sb.argvLog,
	}
	if f.sb.sshRecord != "" {
		skip = append(skip, f.sb.sshRecord)
	}

	assertNoSecretsInTree(t, f.sb.root, skip, secrets)
}

// vmGit runs git in the fake VM user's home, the way the VM's own git sees it: HOME is that home, so the global
// configuration is the one the mint writes there, and the machine's system configuration is off, so a test never
// reads or writes the maintainer's git identity (issue #241). It runs from the sandbox's host home, which is no
// repository, so no repository's own configuration reaches a reading either.
func (f *mintFixture) vmGit(t *testing.T, args ...string) (string, error) {
	t.Helper()

	return f.vmGitFrom(t, f.sb.hostHome, args...)
}

// vmGitFrom runs git in the fake VM user's home from dir, so a test can read the identity a commit in one of that
// home's repositories would use: no repository's own configuration reaches a reading taken from the sandbox's host
// home, and a conditional include applies in the workspace's repositories alone (issue #241).
func (f *mintFixture) vmGitFrom(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is not installed: the test needs it")
	}

	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + f.sb.vmHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"PATH=" + toolDir(t, "git") + ":/usr/bin:/bin",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\nstderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// vmGitValues returns every value of one global key in the fake VM user's home: --get-all, so a key the mint
// wrote twice shows both values, and a key it never wrote shows none.
func (f *mintFixture) vmGitValues(t *testing.T, key string) []string {
	t.Helper()

	out, err := f.vmGit(t, "config", "--global", "--get-all", key)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil // git's status for a key the global configuration does not hold
	}

	if err != nil {
		t.Fatalf("read the fake VM's global %s: %v", key, err)
	}

	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}

	return strings.Split(out, "\n")
}

// setVMGitIdentity gives the fake VM user's home a global identity, the way an earlier vm-init or the maintainer
// leaves one there (issue #241).
func (f *mintFixture) setVMGitIdentity(t *testing.T, name, email string) {
	t.Helper()

	if _, err := f.vmGit(t, "config", "--global", "user.name", name); err != nil {
		t.Fatalf("set the fake VM's global user.name: %v", err)
	}

	if _, err := f.vmGit(t, "config", "--global", "user.email", email); err != nil {
		t.Fatalf("set the fake VM's global user.email: %v", err)
	}
}

// addVMGitValue adds one more value of a key to the fake VM user's global git configuration: the way a home that
// has seen a few git config calls ends up holding several values for one key (issue #241).
func (f *mintFixture) addVMGitValue(t *testing.T, key, value string) {
	t.Helper()

	if _, err := f.vmGit(t, "config", "--global", "--add", key, value); err != nil {
		t.Fatalf("add a value to the fake VM's global %s: %v", key, err)
	}
}

// vmEffectiveGitValue returns the value a commit in the fake VM reads for one key: no --global, so every
// configuration file git reads there counts, ~/.config/git/config (XDG) as well as ~/.gitconfig, and the last one
// wins, as it does for a commit (issue #241).
func (f *mintFixture) vmEffectiveGitValue(t *testing.T, key string) string {
	t.Helper()

	return f.vmEffectiveGitValueIn(t, f.sb.hostHome, key)
}

// vmEffectiveGitValueIn returns the value a commit in dir reads for one key, with the fake VM user's home: for a
// repository of that home every configuration file git reads there counts, conditional includes included, which is
// the identity a conditional include is about (issue #241).
func (f *mintFixture) vmEffectiveGitValueIn(t *testing.T, dir, key string) string {
	t.Helper()

	out, err := f.vmGitFrom(t, dir, "config", "--get", key)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "" // git's status for a key no configuration file of the fake VM holds
	}

	if err != nil {
		t.Fatalf("read the fake VM's effective %s: %v", key, err)
	}

	return strings.TrimRight(out, "\n")
}

// vmGitConfigLeftovers returns what a mint left beside the fake VM user's ~/.gitconfig: the copy of it the mint
// edits - it creates `$HOME/.gitconfig.lock` exclusively, writes the bot identity into that copy and moves it over
// ~/.gitconfig (an older version used a mktemp copy, whose name is matched too, so a test cannot miss one) - so a
// run that refused and a run that succeeded must leave none of them behind: a mint that leaves a lock behind makes
// every later `git config` in the VM fail with "could not lock config file" (issue #241).
func (f *mintFixture) vmGitConfigLeftovers(t *testing.T) []string {
	t.Helper()

	var leftovers []string

	for _, pattern := range []string{".gitconfig.agents-token.*", ".gitconfig.lock"} {
		found, err := filepath.Glob(filepath.Join(f.sb.vmHome, pattern))
		if err != nil {
			t.Fatalf("look for what a mint left beside the fake VM's .gitconfig (%s): %v", pattern, err)
		}

		leftovers = append(leftovers, found...)
	}

	return leftovers
}

// initVMGitRepo makes dir, in the fake VM user's home, a git repository: the workspace the agents run in, and the
// only place a conditional include of that home's ~/.gitconfig applies (issue #241).
func (f *mintFixture) initVMGitRepo(t *testing.T, dir string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	if _, err := f.vmGitFrom(t, f.sb.vmHome, "init", "--quiet", dir); err != nil {
		t.Fatalf("make %s a git repository in the fake VM's home: %v", dir, err)
	}

	return dir
}

// vmPathWithoutGit writes a PATH directory holding every tool of this machine's /usr/bin and /bin except git, and
// returns it. The fake limactl runs the VM's sh -c on this machine and inherits the mint's PATH, so a mint run with
// this directory on its PATH stands in front of a VM that has no git (issue #241).
func vmPathWithoutGit(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "vm-bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}

	for _, source := range []string{"/usr/bin", "/bin"} {
		entries, err := os.ReadDir(source)
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			// git, and the git-* commands git runs from its exec path, are the VM's own tools, and this VM lacks them.
			if name == "git" || strings.HasPrefix(name, "git-") {
				continue
			}

			link := filepath.Join(dir, name)
			if _, err := os.Lstat(link); err == nil {
				continue // /bin and /usr/bin hold the same tools on most machines
			}

			if err := os.Symlink(filepath.Join(source, name), link); err != nil {
				t.Fatalf("link %s: %v", link, err)
			}
		}
	}

	if _, err := os.Lstat(filepath.Join(dir, "git")); err == nil {
		t.Fatalf("%s holds a git after all: the test cannot stand for a VM without git", dir)
	}

	for _, tool := range []string{"sh", "sed", "grep", "date", "find", "id", "mktemp"} {
		if _, err := os.Lstat(filepath.Join(dir, tool)); err != nil {
			t.Fatalf("%s holds no %s, which the mint itself needs on PATH: %v", dir, tool, err)
		}
	}

	return dir
}

// vmWriteScript returns the text dev/agents-app-token.sh runs inside the VM: the value of its vm_write assignment,
// with the shell's quoting taken off. The script cannot be read any other way - sourcing it runs the mint at its end -
// so a test that pins the VM's own checks reads them out of the assignment and runs them with sh -c, exactly as the
// fake limactl runs them (issue #241).
func vmWriteScript(t *testing.T) string {
	t.Helper()

	const assignment = "vm_write='"

	body := readRepoFile(t, agentsMintScript)

	at := strings.Index(body, assignment)
	if at < 0 {
		t.Fatalf("%s holds no vm_write= assignment, so the tests cannot reach the script the VM runs (issue #241)", agentsMintScript)
	}

	rest := body[at+len(assignment):]

	var script strings.Builder

	for {
		end := strings.IndexByte(rest, '\'')
		if end < 0 {
			t.Fatalf("%s's vm_write assignment is not closed: the text after vm_write=' does not end in a quote (issue #241)",
				agentsMintScript)
		}

		script.WriteString(rest[:end])
		rest = rest[end+1:]

		// A quote inside the single-quoted value is written '\'': the value's quote closes it, \' is that escaped
		// quote, and the quote after it opens the value again.
		if after, escaped := strings.CutPrefix(rest, `\''`); escaped {
			script.WriteString("'")
			rest = after

			continue
		}

		break
	}

	text := script.String()

	// What the VM's own script must hold, so a test that reads it out of the file knows it read the whole thing: the
	// options it sets, the trap it removes a half-written copy with (a quote, so the unquoting above worked), the
	// line that says it set the identity and the last line of the script, which sets the end of the text a quoting
	// change would otherwise truncate without a test noticing (issue #241).
	for _, want := range []string{"set -eu", `trap 'rm -f "$gitnew"' EXIT`, "git-identity=set", "gh-wrapper=missing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the text read out of %s's vm_write assignment holds no %q, so the test is not running the script the VM runs (issue #241):\n%s",
				agentsMintScript, want, text)
		}
	}

	return text
}

// vmWriteInVM runs the script dev/agents-app-token.sh hands the VM, from the fake VM user's home and with its
// standard input, and returns what it printed and how it exited: the way the fake limactl runs it, except that a test
// pinning the VM's own checks calls it directly, with no mint in front of it to refuse the input first (issue #241).
func (f *mintFixture) vmWriteInVM(t *testing.T, stdin string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("sh", "-c", vmWriteScript(t), "vm-write", filepath.Join(f.sb.root, "vm-real-gh"))
	cmd.Env = envList(map[string]string{
		"HOME":                f.sb.vmHome,
		"PATH":                f.sb.binDir + ":/usr/local/bin:/usr/bin:/bin",
		"TMPDIR":              filepath.Join(f.sb.root, "tmp"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_SYSTEM":   os.DevNull,
	})
	cmd.Dir = f.sb.hostHome
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// identityNotice returns a line of the mint's output that says it set the VM's git identity: the bot's address
// and the words git and identity. The wording is not pinned, only that a maintainer reading the run sees the
// identity went in (issue #241).
func identityNotice(email string, streams ...string) (string, bool) {
	for _, stream := range streams {
		for _, line := range strings.Split(stream, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(line, email) && strings.Contains(lower, "git") && strings.Contains(lower, "identity") {
				return line, true
			}
		}
	}

	return "", false
}

// identityRefusedNotice tells whether one of the mint's lines says the identity a commit in the VM would use is
// not the bot's: it names the other address, the include that brings it, or the effective identity. The wording is
// not pinned, only that a maintainer reading the refused run learns why (issue #241).
func identityRefusedNotice(email string, streams ...string) bool {
	for _, stream := range streams {
		for _, line := range strings.Split(stream, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "effective") || strings.Contains(lower, "include") || strings.Contains(line, email) {
				return true
			}
		}
	}

	return false
}

// botNameRefusedNotice tells whether one of the mint's lines says the bot name was refused by a name check: it
// quotes the name, or says what a name check says (the bot name, a name that is not a bot's). The wording is not
// pinned, only that the refusal is the name check's, not git reading the name as an option (issue #241).
func botNameRefusedNotice(name string, streams ...string) bool {
	for _, stream := range streams {
		for _, line := range strings.Split(stream, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, strings.ToLower(name)) || strings.Contains(lower, "bot name") || strings.Contains(lower, "name is not") {
				return true
			}
		}
	}

	return false
}

// lockRemovalNotice returns a line of the mint's output that tells a maintainer how to clear a lock that no git
// holds: it names ~/.gitconfig.lock and removing or deleting it, or names the case the way out is for, no git
// running in the VM. The wording is not pinned. A line that merely holds the word lock is no evidence: a test's own
// temporary directory carries its name, and one of these names ends in LockOn, so the way out has to be said about
// the lock itself (issue #241).
func lockRemovalNotice(streams ...string) (string, bool) {
	for _, stream := range streams {
		for _, line := range strings.Split(stream, "\n") {
			lower := strings.ToLower(line)

			namesTheFile := strings.Contains(lower, ".gitconfig")
			removesIt := strings.Contains(lower, "remove") || strings.Contains(lower, "delete")

			switch {
			case namesTheFile && removesIt,
				strings.Contains(lower, "no git is running"),
				strings.Contains(lower, "no git runs"),
				strings.Contains(lower, "no git in the vm"):
				return line, true
			}
		}
	}

	return "", false
}

// staleGitLockBody is what a git config that was killed before it could remove its lock leaves in
// ~/.gitconfig.lock: a lock file with no writer behind it (issue #241).
const staleGitLockBody = "a git config that was killed before it removed its lock\n"

// writeStaleGitLock gives the fake VM user's home a ~/.gitconfig.lock that no writer holds, and returns its path.
func (f *mintFixture) writeStaleGitLock(t *testing.T) string {
	t.Helper()

	lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
	writeFileMode(t, lock, []byte(staleGitLockBody), 0o600)

	return lock
}

// gitConfigBody returns a ~/.gitconfig body holding exactly one user.name and one user.email, and nothing else.
func gitConfigBody(name, email string) []byte {
	return []byte("[user]\n\tname = " + name + "\n\temail = " + email + "\n")
}

// mintWithUmask runs one mint from a shell whose umask is umask, so the modes the VM user's tools create are the
// ones that user's umask gives them: a VM user with 0002 leaves the group and other bits on a file unless the
// script creates it 0600 itself (issue #241).
func (f *mintFixture) mintWithUmask(t *testing.T, umask string) (string, string, error) {
	t.Helper()

	// exec sh "$@": a shell that sets the umask and then becomes the mint, so only this run sees it.
	cmd := exec.Command("sh", "-c", "umask "+umask+"\nexec sh \"$@\"", "mint-under-umask",
		scriptPath(t, agentsMintScript), "mint")
	cmd.Env = envList(f.env)
	cmd.Dir = f.sb.hostHome

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
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

	// Issue #241: the identity file alone left the VM committing as the maintainer, because vm-init ran before the
	// first mint. The mint applies the identity to the VM's global git configuration, and says so.
	if _, ok := identityNotice(wantEmail, stdout, stderr); !ok {
		t.Errorf("the mint's output says nothing about setting the VM's git identity: no line holds the bot's address %q together with the words git and identity, so a maintainer cannot tell from the run whether the agents commit as the bot (issue #241)\nstdout:\n%s\nstderr:\n%s",
			wantEmail, stdout, stderr)
	}

	if _, err := os.Stat(filepath.Join(f.sb.hostHome, filepath.FromSlash(agentsTokenFile))); err == nil {
		t.Errorf("the mint wrote %s into the host's home; the token belongs in the VM and nowhere else", agentsTokenFile)
	}
}

// TestAgentsAppTokenMintAppliesTheBotIdentityToTheVMGit pins issue #241: writing the identity file is not enough,
// because vm-init runs before the first mint and finds no file, so nothing applies the bot identity then and every
// agent commit carries the maintainer's. The mint that writes the bot identity also sets the VM user's global git
// identity, replacing whatever was there, and a later mint leaves exactly one value per key.
func TestAgentsAppTokenMintAppliesTheBotIdentityToTheVMGit(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"
	)

	maintainer := func(t *testing.T, f *mintFixture) {
		t.Helper()

		f.setVMGitIdentity(t, maintainerName, maintainerEmail)
	}

	cases := []struct {
		name   string
		before func(t *testing.T, f *mintFixture)
		mints  int
		// check runs after the identity itself is pinned, for what one case says beyond it.
		check func(t *testing.T, f *mintFixture, wantName, wantEmail string)
	}{
		{
			name:  "the VM home holds no git identity yet",
			mints: 1,
		},
		{
			name:   "the VM home holds the maintainer's identity",
			before: maintainer,
			mints:  1,
		},
		{
			// The bot replaces any other identity, as dev/vm-git-identity.sh does with the identity file present, so a
			// second mint replaces its own: no key is written twice.
			name:   "two mints in a row",
			before: maintainer,
			mints:  2,
		},
		{
			// Review finding: git refuses to replace a key that holds several values ("cannot overwrite multiple
			// values with a single value", status 5), so a ~/.gitconfig a few git config calls left with two values
			// per key must still end with exactly one value per key, the bot's (issue #241).
			name: "the VM home holds several values for each key",
			before: func(t *testing.T, f *mintFixture) {
				t.Helper()

				for _, name := range []string{maintainerName, "Other Maintainer"} {
					f.addVMGitValue(t, "user.name", name)
				}

				for _, email := range []string{maintainerEmail, "other@example.com"} {
					f.addVMGitValue(t, "user.email", email)
				}
			},
			mints: 1,
		},
		{
			// Review finding: git reads $XDG_CONFIG_HOME/git/config (~/.config/git/config) as well as
			// ~/.gitconfig, so the identity a commit in the VM uses must be the bot's whatever the XDG file holds
			// (issue #241).
			name: "the maintainer's identity sits in the XDG config",
			before: func(t *testing.T, f *mintFixture) {
				t.Helper()

				writeFileMode(t, filepath.Join(f.sb.vmHome, ".config", "git", "config"),
					[]byte("[user]\n\tname = "+maintainerName+"\n\temail = "+maintainerEmail+"\n"), 0o600)
			},
			mints: 1,
			check: func(t *testing.T, f *mintFixture, wantName, wantEmail string) {
				t.Helper()

				if got := f.vmEffectiveGitValue(t, "user.name"); got != wantName {
					t.Errorf("the fake VM's effective git user.name is %q, want %q: an XDG config the maintainer left must not keep the agents committing as them (issue #241)",
						got, wantName)
				}

				if got := f.vmEffectiveGitValue(t, "user.email"); got != wantEmail {
					t.Errorf("the fake VM's effective git user.email is %q, want %q: an XDG config the maintainer left must not keep the agents committing as them (issue #241)",
						got, wantEmail)
				}
			},
		},
		{
			// Review finding: the mint replaces the identity in place, so whatever else the VM's ~/.gitconfig holds
			// survives it and the file stays a regular file: a maintainer's editor and credential helper are not the
			// mint's to drop (issue #241).
			name: "the git config holds other settings",
			before: func(t *testing.T, f *mintFixture) {
				t.Helper()

				writeFileMode(t, filepath.Join(f.sb.vmHome, ".gitconfig"), []byte(
					"[core]\n\teditor = vi\n[credential]\n\thelper = store\n[user]\n\tname = "+maintainerName+"\n\temail = "+maintainerEmail+"\n"), 0o600)
			},
			mints: 1,
			check: func(t *testing.T, f *mintFixture, _, _ string) {
				t.Helper()

				if got := f.vmGitValues(t, "core.editor"); !slices.Equal(got, []string{"vi"}) {
					t.Errorf("the fake VM's global core.editor is %v, want exactly [vi]: the mint replaces the identity, not the rest of a git config the VM already has (issue #241)",
						got)
				}

				if got := f.vmGitValues(t, "credential.helper"); !slices.Equal(got, []string{"store"}) {
					t.Errorf("the fake VM's global credential.helper is %v, want exactly [store]: the mint replaces the identity, not the rest of a git config the VM already has (issue #241)",
						got)
				}

				info, err := os.Lstat(filepath.Join(f.sb.vmHome, ".gitconfig"))
				if err != nil {
					t.Fatalf("stat the fake VM's .gitconfig: %v", err)
				}

				if !info.Mode().IsRegular() {
					t.Errorf("the fake VM's .gitconfig is %s, want a regular file: the mint writes the git identity into the file, and a VM's git reads no other (issue #241)",
						info.Mode().Type())
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			if tc.before != nil {
				tc.before(t, f)
			}

			for i := 1; i <= tc.mints; i++ {
				stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
				if err != nil {
					t.Fatalf("mint %d exited non-zero: %v\nstdout: %s\nstderr: %s\nthe fake API refused: %s",
						i, err, stdout, stderr, strings.Join(f.api.recordedComplaints(), "; "))
				}

				f.assertNoLeaks(t, stdout, stderr)
			}

			wantName := f.api.slug + "[bot]"
			wantEmail := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

			if got := f.vmGitValues(t, "user.name"); !slices.Equal(got, []string{wantName}) {
				t.Errorf("the fake VM's global user.name is %v, want exactly [%q]: the mint that writes the bot identity sets the VM user's git identity too, and a later mint replaces it rather than writing the key twice (issue #241)",
					got, wantName)
			}

			if got := f.vmGitValues(t, "user.email"); !slices.Equal(got, []string{wantEmail}) {
				t.Errorf("the fake VM's global user.email is %v, want exactly [%q]: the mint that writes the bot identity sets the VM user's git identity too, and a later mint replaces it rather than writing the key twice (issue #241)",
					got, wantEmail)
			}

			if tc.check != nil {
				tc.check(t, f, wantName, wantEmail)
			}

			// The mint writes the identity into a copy of ~/.gitconfig and moves that copy over the original, so the
			// write is atomic: a copy left in the VM user's home is one the maintainer's home did not get, and a home that
			// mints twice a day fills with them (issue #241).
			if leftovers := f.vmGitConfigLeftovers(t); len(leftovers) != 0 {
				t.Errorf("the mint left %v in the fake VM's home: the copy of ~/.gitconfig it writes the identity into must not outlive the mint that wrote it (issue #241)",
					leftovers)
			}
		})
	}
}

// TestAgentsAppTokenMintRefusesAVMWithoutGit pins the review finding that a VM without git would leave the agents
// committing as nobody: with a bot identity to apply, the mint refuses (status 3 on the VM side, a failed mint on
// the host's) before it writes anything, rather than reporting a success the VM cannot honour (issue #241).
func TestAgentsAppTokenMintRefusesAVMWithoutGit(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := maps.Clone(f.env)
	// The fake limactl runs the VM's script on this machine and inherits the mint's PATH: a PATH without git stands
	// in front of a VM without git, and the mint's own tools, curl and openssl among them, stay on it.
	env["PATH"] = f.sb.binDir + ":" + vmPathWithoutGit(t)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, env, "")
	if err == nil {
		t.Fatalf("mint exited 0 in a VM that has no git, so nothing set the bot identity although the VM holds one: the mint must refuse a VM whose agents cannot commit as the bot (issue #241)\nstdout: %s\nstderr: %s",
			stdout, stderr)
	}

	if !strings.Contains(strings.ToLower(stderr), "git") {
		t.Errorf("the mint says nothing about git although the VM has none: a maintainer cannot tell why the agents commit as nobody (issue #241)\nstderr:\n%s",
			stderr)
	}

	// The token is not pinned here: the mint writes it before it knows the bot user, and a failed identity write keeps
	// it by design (TestAgentsAppTokenMintExitsOneWhenTheBotUserLookupFails), so only the identity file must stay
	// away: the refusal is about setting the identity, never about the token it already holds.
	if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
		t.Errorf("the mint wrote %s although the VM has no git: the VM must refuse the identity before it writes that file (issue #241)",
			agentsIdentityFile)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenMintRefusesAGitIncludeThatOverridesTheBotIdentity pins the review findings that git reads
// [include] and [includeIf] files too and the last value wins: a ~/.gitconfig that takes the bot's identity but
// includes a file with another address leaves the agents committing as someone else, so the mint must refuse rather
// than report success. A conditional include makes this the sharper: the mint's own check runs outside any
// repository, where git never evaluates an includeIf, while the agents commit inside one of the workspace's, where
// it does (issue #241).
func TestAgentsAppTokenMintRefusesAGitIncludeThatOverridesTheBotIdentity(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"
		otherEmail      = "other@example.com"
	)

	// writeIncluded gives the fake VM's home a file that sets user.email to address, and returns its path.
	writeIncluded := func(t *testing.T, f *mintFixture, address string) string {
		t.Helper()

		included := filepath.Join(f.sb.vmHome, "git-identity.d", "other.gitconfig")
		writeFileMode(t, included, []byte("[user]\n\temail = "+address+"\n"), 0o600)

		return included
	}

	cases := []struct {
		name string
		// gitConfig writes the fake VM's ~/.gitconfig and what it includes, and proves the include really applies
		// before the mint runs against it: a fixture that does not is no test of the refusal.
		gitConfig func(t *testing.T, f *mintFixture)
		// check runs on the refused mint's output, for what one case pins beyond the refusal itself.
		check func(t *testing.T, stdout, stderr string)
	}{
		{
			name: "an include that overrides the address",
			gitConfig: func(t *testing.T, f *mintFixture) {
				t.Helper()

				included := writeIncluded(t, f, otherEmail)
				writeFileMode(t, filepath.Join(f.sb.vmHome, ".gitconfig"), []byte(
					"[user]\n\tname = "+maintainerName+"\n\temail = "+maintainerEmail+"\n[include]\n\tpath = "+included+"\n"), 0o600)

				if got := f.vmEffectiveGitValue(t, "user.email"); got != otherEmail {
					t.Fatalf("the fake VM's effective git user.email is %q, want %q, from the included file: the fixture itself is wrong",
						got, otherEmail)
				}
			},
		},
		{
			// The second review's finding: git evaluates a conditional include only inside a repository the condition
			// matches, and the mint's check runs outside all of them, so it read the bot's identity back from a file
			// that hands the workspace another one (issue #241).
			name: "a conditional include that overrides the address in the workspace",
			gitConfig: func(t *testing.T, f *mintFixture) {
				t.Helper()

				included := writeIncluded(t, f, otherEmail)
				writeFileMode(t, filepath.Join(f.sb.vmHome, ".gitconfig"), []byte(
					"[user]\n\tname = "+maintainerName+"\n\temail = "+maintainerEmail+"\n"+
						"[includeIf \"gitdir:~/ws/\"]\n\tpath = "+included+"\n"), 0o600)

				repo := f.initVMGitRepo(t, filepath.Join(f.sb.vmHome, "ws", "example"))
				if got := f.vmEffectiveGitValueIn(t, repo, "user.email"); got != otherEmail {
					t.Fatalf("a commit in the fake VM's %s reads user.email %q, want %q from the conditional include: the fixture itself is wrong",
						repo, got, otherEmail)
				}

				// Outside a repository the condition is never evaluated, which is the whole reason the check read the
				// bot's identity back from this file and let the write through.
				if got := f.vmEffectiveGitValue(t, "user.email"); got != maintainerEmail {
					t.Fatalf("the fake VM reads user.email %q outside a repository, want the maintainer's %q: this case needs a conditional include that only the workspace applies (issue #241)",
						got, maintainerEmail)
				}
			},
			check: func(t *testing.T, stdout, stderr string) {
				t.Helper()

				// A maintainer reading the refused mint must learn what to remove, and an includeIf is not an include:
				// telling them to look for [include] sends them past the line that does it.
				text := strings.ToLower(stdout + stderr)
				if !strings.Contains(text, "includeif") && !strings.Contains(text, "conditional include") {
					t.Errorf("the mint refuses a ~/.gitconfig whose [includeIf] hands the workspace %q, but says nothing about a conditional include, so a maintainer cannot find the line to remove (issue #241)\nstdout:\n%s\nstderr:\n%s",
						otherEmail, stdout, stderr)
				}
			},
		},
		{
			// A second review's finding on the refusal itself: the address comes out of a file in the VM, and a value
			// that is printed raw can drive a terminal - clearing the line above, colouring the rest - so the refusal
			// strips the control characters instead of copying them into the maintainer's log (issue #241).
			name: "an include whose address holds a control character",
			gitConfig: func(t *testing.T, f *mintFixture) {
				t.Helper()

				included := writeIncluded(t, f, otherEmail+"\x1b[31m")
				writeFileMode(t, filepath.Join(f.sb.vmHome, ".gitconfig"), []byte(
					"[user]\n\tname = "+maintainerName+"\n\temail = "+maintainerEmail+"\n[include]\n\tpath = "+included+"\n"), 0o600)

				if got := f.vmEffectiveGitValue(t, "user.email"); got != otherEmail+"\x1b[31m" {
					t.Fatalf("the fake VM's effective git user.email is %q, want the included value with its ESC: the fixture itself is wrong",
						got)
				}
			},
			check: func(t *testing.T, _, stderr string) {
				t.Helper()

				if strings.ContainsRune(stderr, '\x1b') {
					t.Errorf("the refused mint's stderr holds a raw ESC, copied out of the included user.email: a value out of a file in the VM must not reach the maintainer's terminal, where it drives it instead of being read (issue #241)\nstderr:\n%q",
						stderr)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			tc.gitConfig(t, f)

			before, err := os.ReadFile(filepath.Join(f.sb.vmHome, ".gitconfig"))
			if err != nil {
				t.Fatalf("read the fake VM's .gitconfig before the mint: %v", err)
			}

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err == nil {
				t.Fatalf("mint exited 0 although a commit in the VM still reads an address from an include of ~/.gitconfig, not the bot's: the agents would commit as someone else while the mint reports the bot's identity (issue #241, %s)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if !identityRefusedNotice(otherEmail, stdout, stderr) {
				t.Errorf("the mint refuses (%s) but says nothing about the git identity a commit in the VM would use: no line names the effective identity, the include that overrides it, or %q (issue #241)\nstdout:\n%s\nstderr:\n%s",
					tc.name, otherEmail, stdout, stderr)
			}

			if tc.check != nil {
				tc.check(t, stdout, stderr)
			}

			// A refusal is no half-success: the mint writes the identity into a copy of ~/.gitconfig and only moves it
			// over the original once it read the bot's identity back, so the refused run must leave the VM's own git
			// configuration exactly as it found it, byte for byte (issue #241).
			after, err := os.ReadFile(filepath.Join(f.sb.vmHome, ".gitconfig"))
			if err != nil {
				t.Fatalf("read the fake VM's .gitconfig after the refused mint: %v", err)
			}

			if !bytes.Equal(after, before) {
				t.Errorf("the refused mint changed the fake VM's .gitconfig, although nothing set the bot identity:\nbefore:\n%s\nafter:\n%s",
					before, after)
			}

			if leftovers := f.vmGitConfigLeftovers(t); len(leftovers) != 0 {
				t.Errorf("the refused mint left %v in the fake VM's home: the copy of ~/.gitconfig it writes the identity into must be removed when it refuses, or the VM user's home keeps a file nothing reads (issue #241)",
					leftovers)
			}

			// Whether a run refused this way still keeps the token is the design's call, so the token is not pinned here.
			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenMintRefusesAGitConfigWhoseIncludesItCannotRead pins the first half of issue #264: the VM's
// no_conditional reads ~/.gitconfig's [includeIf] entries with `git config --includes --file ... --name-only
// --get-regexp`, and git's status 1 (no match) is the only status that means "no conditional include". Any other
// status - a git wrapper that fails, a configuration git cannot parse - left the mint unable to tell an include
// check that passed from one that never ran, so the VM refused with status 3 before it wrote anything, and the
// mint reported it. The test fails that one call with a git shim and hands every other call to the machine's git:
// once driven directly, at the VM's own script, and once through the mint.
func TestAgentsAppTokenMintRefusesAGitConfigWhoseIncludesItCannotRead(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"

		// The status the shim fails the include lookup with: git's own status for a configuration it cannot read,
		// and no status the check reads as "no conditional include" (that is 1).
		lookupStatus = 128
	)

	// refuse prepares a fake VM home with the maintainer's identity and the failing include lookup, and returns
	// the fixture and the git config as it was before.
	refuse := func(t *testing.T) (*mintFixture, []byte) {
		t.Helper()

		f := newMintFixture(t)
		f.setVMGitIdentity(t, maintainerName, maintainerEmail)

		before, err := os.ReadFile(filepath.Join(f.sb.vmHome, ".gitconfig"))
		if err != nil {
			t.Fatalf("read the fake VM's .gitconfig before the mint: %v", err)
		}

		shim := fmt.Sprintf(failingIncludesGitShim, lookupStatus, strconv.Quote(realTool(t, "git")))
		writeFileMode(t, filepath.Join(f.sb.binDir, "git"), []byte(shim), 0o700)

		return f, before
	}

	t.Run("the VM's script refuses it before it writes anything", func(t *testing.T) {
		t.Parallel()

		f, before := refuse(t)

		name := f.api.slug + "[bot]"
		email := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

		// A token and an expiry with the identity, as the mint that knows the bot user sends them: the script
		// refuses the identity before it writes any of it.
		stdout, stderr, err := f.vmWriteInVM(t, "ghs_inventedtoken\n1700000000\n"+name+"\n"+email+"\n")

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("the VM's script exited %v although git cannot read the includes of ~/.gitconfig (status %d), want status 3, the refusal the host reads as permanent (issue #264)\nstdout: %s\nstderr: %s",
				err, lookupStatus, stdout, stderr)
		}

		if code := exitErr.ExitCode(); code != 3 {
			t.Fatalf("the VM's script exited with %d although git cannot read the includes of ~/.gitconfig (status %d), want 3: an include lookup the script cannot trust is a refusal and never a write (issue #264)\nstdout: %s\nstderr: %s",
				code, lookupStatus, stdout, stderr)
		}

		for _, want := range []string{"cannot read the includes of", fmt.Sprintf("(git config exited %d)", lookupStatus)} {
			if !strings.Contains(stderr, want) {
				t.Errorf("the refusal does not say %q, so a maintainer cannot tell an include lookup that failed from a ~/.gitconfig that holds a conditional include (issue #264)\nstderr:\n%s",
					want, stderr)
			}
		}

		for _, file := range []string{agentsTokenFile, agentsIdentityFile} {
			if _, err := os.Stat(f.sb.vmPath(file)); err == nil {
				t.Errorf("the VM's script wrote %s although git cannot read the includes of ~/.gitconfig: it must refuse before it writes anything (issue #264)", file)
			}
		}

		after, err := os.ReadFile(filepath.Join(f.sb.vmHome, ".gitconfig"))
		if err != nil {
			t.Fatalf("read the fake VM's .gitconfig after the refused run: %v", err)
		}

		if !bytes.Equal(after, before) {
			t.Errorf("the refused run changed the fake VM's .gitconfig although git cannot read its includes:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}
	})

	t.Run("the mint reports the VM's refusal", func(t *testing.T) {
		t.Parallel()

		f, before := refuse(t)

		stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
		if err == nil {
			t.Fatalf("mint exited 0 although git cannot read the includes of ~/.gitconfig (status %d): the mint cannot tell whether an [include] or [includeIf] hands the agents another identity, so it must refuse (issue #264)\nstdout: %s\nstderr: %s",
				lookupStatus, stdout, stderr)
		}

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Errorf("mint exited with %v after the VM's git failed the include lookup (status %d), want status 1, the status of a failed mint (issue #264)\nstderr: %s",
				err, lookupStatus, stderr)
		}

		for _, want := range []string{"cannot read the includes of", fmt.Sprintf("(git config exited %d)", lookupStatus)} {
			if !strings.Contains(stderr, want) {
				t.Errorf("the refusal does not say %q, so a maintainer cannot tell an include lookup that failed from a ~/.gitconfig that holds a conditional include (issue #264)\nstderr:\n%s",
					want, stderr)
			}
		}

		// The bot identity is what this write was about, and it must not land: the token the mint wrote before it
		// looked the bot user up is the design's (a failed lookup keeps it), the identity is this refusal's.
		if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
			t.Errorf("the mint wrote %s although git cannot read the includes of ~/.gitconfig: it must refuse the identity before it writes that file (issue #264)", agentsIdentityFile)
		}

		after, err := os.ReadFile(filepath.Join(f.sb.vmHome, ".gitconfig"))
		if err != nil {
			t.Fatalf("read the fake VM's .gitconfig after the refused mint: %v", err)
		}

		if !bytes.Equal(after, before) {
			t.Errorf("the refused mint changed the fake VM's .gitconfig although git cannot read its includes:\nbefore:\n%s\nafter:\n%s",
				before, after)
		}

		f.assertNoLeaks(t, stdout, stderr)
	})
}

// failingIncludesGitShim is a git for the VM that fails one call and only one: the include lookup of ~/.gitconfig,
// with the status the test gives it (issue #264). Every other call is the machine's own git, so the mint's reads
// and writes of the identity are the real ones.
const failingIncludesGitShim = `#!/bin/sh
if [ "$#" -eq 7 ] && [ "$1" = config ] && [ "$2" = --includes ] && [ "$3" = --file ] &&
  [ "$4" = "${HOME:?}/.gitconfig" ] && [ "$5" = --name-only ] && [ "$6" = --get-regexp ] &&
  [ "$7" = '^includeif\..*\.path$' ]; then
  echo 'git: the include lookup is broken by the devenv tests' >&2
  exit %d
fi
exec %s "$@"
`

// TestAgentsAppTokenMintRefusesASlugBeginningWithADashOnTheHost pins the review finding that a slug that begins with
// - reaches git config as an argument shaped like an option, which no App's slug does: the mint refuses it before a
// token is minted, so nothing that would carry such a name is ever written. The VM's own check is what the test below
// pins, driven directly, because the host refuses the slug before the VM sees it (issue #241).
func TestAgentsAppTokenMintRefusesASlugBeginningWithADashOnTheHost(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	const slug = "-example-agents"

	f.api.setSlug(slug)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err == nil {
		t.Fatalf("mint exited 0 although the App's slug begins with -, which is no App slug and reaches git config as an argument shaped like an option: the slug must be refused (issue #241)\nstdout: %s\nstderr: %s",
			stdout, stderr)
	}

	if name := slug + "[bot]"; !botNameRefusedNotice(name, stdout, stderr) {
		t.Errorf("the mint refuses the slug %q but says nothing about the bot name itself: no line quotes %q or says a name check refused it, so the refusal reads as a git failure rather than a refused name (issue #241)\nstdout:\n%s\nstderr:\n%s",
			slug, name, stdout, stderr)
	}

	// The host refuses the slug before it asks for a token: nothing was minted for a name the VM would refuse.
	if posted := f.api.posted(); len(posted) != 0 {
		t.Errorf("the mint posted to %v although the App's slug begins with -: the slug is refused before a token is minted, never after (issue #241)",
			posted)
	}

	for _, file := range []string{agentsTokenFile, agentsIdentityFile} {
		if _, err := os.Stat(f.sb.vmPath(file)); err == nil {
			t.Errorf("the mint wrote %s although it refused the bot name %q: it must refuse before it writes anything (issue #241)",
				file, slug+"[bot]")
		}
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenVMSideNameCheckRefusesASlugBeginningWithADash pins the VM's own name check, driven directly.
// The host refuses a slug that begins with - before the VM sees it
// (TestAgentsAppTokenMintRefusesASlugBeginningWithADashOnTheHost), so no mint can tell whether the VM would refuse
// one too, and the VM is where the name reaches git config: the script it runs takes a forged name beginning with -
// and refuses it with status 3, writes no identity file and leaves ~/.gitconfig as it found it (issue #241).
func TestAgentsAppTokenVMSideNameCheckRefusesASlugBeginningWithADash(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"
		slug            = "-example-agents"
	)

	f := newMintFixture(t)
	f.setVMGitIdentity(t, maintainerName, maintainerEmail)

	gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")

	before, err := os.ReadFile(gitconfig)
	if err != nil {
		t.Fatalf("read the fake VM's .gitconfig before the VM's script runs: %v", err)
	}

	// The four lines the host sends the VM's script: no token and no expiry, so only the identity is written, and the
	// name and the address a slug beginning with - makes.
	name := slug + "[bot]"
	email := "12345678+" + name + "@users.noreply.github.com"

	stdout, stderr, err := f.vmWriteInVM(t, "\n\n"+name+"\n"+email+"\n")

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the VM's script exited %v for the forged bot name %q, want status 3: a name the VM refuses is a refusal the host reports as a permanent one, never a success (issue #241)\nstdout: %s\nstderr: %s",
			err, name, stdout, stderr)
	}

	if code := exitErr.ExitCode(); code != 3 {
		t.Fatalf("the VM's script exited with %d for the forged bot name %q, want 3, the status the host reads as a refusal of the write (issue #241)\nstdout: %s\nstderr: %s",
			code, name, stdout, stderr)
	}

	if !botNameRefusedNotice(name, stdout, stderr) {
		t.Errorf("the VM's script refuses the forged bot name %q but says nothing about the name itself: no line quotes it or says a name check refused it, so the refusal reads as a git failure (issue #241)\nstdout:\n%s\nstderr:\n%s",
			name, stdout, stderr)
	}

	if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
		t.Errorf("the VM's script wrote %s although it refused the bot name %q: it writes no identity it refused (issue #241)",
			agentsIdentityFile, name)
	}

	after, err := os.ReadFile(gitconfig)
	if err != nil {
		t.Fatalf("read the fake VM's .gitconfig after the VM's script refused the name: %v", err)
	}

	if !bytes.Equal(after, before) {
		t.Errorf("the VM's script changed the fake VM's .gitconfig although it refused the bot name %q:\nbefore:\n%s\nafter:\n%s",
			name, before, after)
	}
}

// TestAgentsAppTokenMintLeavesAGitConfigThatIsAlreadyTheBotsAlone pins the second review's finding that every mint
// replaced the VM user's ~/.gitconfig even when it already held exactly the bot's identity: a new inode and a new
// mode every hour, for a file that reads the same before and after, loses whatever a maintainer hung on the inode and
// rewrites a file nothing changed. The mint still says the identity is set, because it is (issue #241).
func TestAgentsAppTokenMintLeavesAGitConfigThatIsAlreadyTheBotsAlone(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	name := f.api.slug + "[bot]"
	email := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

	// The helper writes the file mode 0640, a mode the mint's own copy does not have (it creates the lock 0600), so
	// a rewrite that replaces the file shows up in the mode as well as in the inode.
	gitconfig := f.configTheBotIdentity(t, name, email)

	before, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig before the mint: %v", err)
	}

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint exited non-zero although the fake VM's ~/.gitconfig already held the bot's identity %s <%s>: %v\nstdout: %s\nstderr: %s",
			name, email, err, stdout, stderr)
	}

	after, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig after the mint: %v", err)
	}

	if !os.SameFile(before, after) {
		t.Errorf("the mint replaced the fake VM's ~/.gitconfig although it already held the bot's identity %s <%s>: a file whose content needs no change must not be rewritten, so it keeps its inode (issue #241)",
			name, email)
	}

	if got, want := after.Mode().Perm(), before.Mode().Perm(); got != want {
		t.Errorf("the fake VM's ~/.gitconfig is mode %04o after the mint, was %04o before it: the identity needed no change, so a rewrite that also changes the mode must not happen (issue #241)",
			got, want)
	}

	// Whether the file was written or left alone, a maintainer reading the run must learn the agents commit as the
	// bot: the notice is about the identity, not about the write.
	if _, ok := identityNotice(email, stdout, stderr); !ok {
		t.Errorf("the mint's output says nothing about setting the VM's git identity although it already was the bot's: no line holds %q together with the words git and identity (issue #241)\nstdout:\n%s\nstderr:\n%s",
			email, stdout, stderr)
	}

	if leftovers := f.vmGitConfigLeftovers(t); len(leftovers) != 0 {
		t.Errorf("the mint left %v in the fake VM's home although it had nothing to write: no copy of ~/.gitconfig and no lock of git's may outlive it (issue #241)",
			leftovers)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenMintIgnoresAStaleGitLockWhenTheIdentityIsAlreadyTheBots pins the third review's finding that
// the mint took ~/.gitconfig.lock before it looked at ~/.gitconfig: a git that was killed leaves its lock behind,
// and while it sits there every mint of a VM that already commits as the bot refused - although nothing needed
// writing. The check for "already the bot's" runs read-only on ~/.gitconfig first, so the mint succeeds, writes its
// token and its identity file, and leaves the lock alone (issue #241).
func TestAgentsAppTokenMintIgnoresAStaleGitLockWhenTheIdentityIsAlreadyTheBots(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	name := f.api.slug + "[bot]"
	email := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

	gitconfig := f.configTheBotIdentity(t, name, email)

	before, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig before the mint: %v", err)
	}

	lock := f.writeStaleGitLock(t)

	lockBefore, err := os.Stat(lock)
	if err != nil {
		t.Fatalf("stat the fake VM's %s before the mint: %v", lock, err)
	}

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err != nil {
		t.Fatalf("mint exited non-zero although the fake VM's ~/.gitconfig already held exactly the bot's identity %s <%s>, so nothing needed writing, and the stale %s beside it belongs to no writer: %v\nstdout: %s\nstderr: %s",
			name, email, lock, err, stdout, stderr)
	}

	// The token and the identity file are written whether ~/.gitconfig needed a write or not.
	readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)

	identity := readTwoLines(t, f.sb.vmPath(agentsIdentityFile), 0o600)
	if identity[0] != name || identity[1] != email {
		t.Errorf("%s holds %q <%q>, want the bot %q <%q>: a mint that writes no git configuration still writes the identity file (issue #241)",
			agentsIdentityFile, identity[0], identity[1], name, email)
	}

	after, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig after the mint: %v", err)
	}

	if !os.SameFile(before, after) {
		t.Errorf("the mint replaced the fake VM's ~/.gitconfig although it already held exactly the bot's identity %s <%s>: a file whose content needs no change must not be rewritten, so it keeps its inode (issue #241)",
			name, email)
	}

	body, err := os.ReadFile(gitconfig)
	if err != nil {
		t.Fatalf("read the fake VM's .gitconfig after the mint: %v", err)
	}

	if want := gitConfigBody(name, email); !bytes.Equal(body, want) {
		t.Errorf("the fake VM's ~/.gitconfig holds\n%s\nafter the mint, want\n%s\nalthough it already held the bot's identity (issue #241)",
			body, want)
	}

	// The lock is nobody's: the mint never took it, so it neither removes it nor waits on it.
	lockAfter, err := os.Stat(lock)
	if err != nil {
		t.Errorf("the mint removed %s although the fake VM's ~/.gitconfig already held the bot's identity and the mint took no lock: a stale lock belongs to no writer, and removing one the mint did not create is not its to do (issue #241): %v",
			lock, err)
	} else if !os.SameFile(lockBefore, lockAfter) {
		t.Errorf("the mint replaced the stale %s although it had no reason to touch it: the identity needed no write, so no lock is taken (issue #241)", lock)
	}

	held, err := os.ReadFile(lock)
	if err != nil {
		t.Fatalf("read the fake VM's %s after the mint: %v", lock, err)
	}

	if string(held) != staleGitLockBody {
		t.Errorf("the fake VM's %s holds %q after the mint, want %q: the mint leaves a lock it never took exactly as it found it (issue #241)",
			lock, held, staleGitLockBody)
	}

	if _, ok := identityNotice(email, stdout, stderr); !ok {
		t.Errorf("the mint's output says nothing about setting the VM's git identity although it already was the bot's: no line holds %q together with the words git and identity (issue #241)\nstdout:\n%s\nstderr:\n%s",
			email, stdout, stderr)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// configTheBotIdentity gives the fake VM's ~/.gitconfig exactly the bot's identity, mode 0640, and returns its path:
// the state every mint after the first leaves a VM in, and the one a stale lock must not block. The mode is one the
// mint's own copy does not have, so a rewrite that replaces the file shows up in the mode as well as in the inode
// (issue #241).
func (f *mintFixture) configTheBotIdentity(t *testing.T, name, email string) string {
	t.Helper()

	gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")
	writeFileMode(t, gitconfig, gitConfigBody(name, email), 0o640)

	return gitconfig
}

// TestAgentsAppTokenMintSaysWhetherItSetTheVMGitIdentity pins issue #263: the VM step printed git-identity=set on
// every mint, so a mint that found ~/.gitconfig already holding the bot's identity and wrote nothing said it had
// set it - about every 45 minutes, in the refresher's log. The VM step says set only when it wrote the file and
// unchanged when the file already held the bot's identity, and the mint says the same: set for the first mint,
// and "already" for the one that changed nothing, which leaves the file byte for byte and its mtime as it was.
func TestAgentsAppTokenMintSaysWhetherItSetTheVMGitIdentity(t *testing.T) {
	t.Parallel()

	// The two halves of what a maintainer reads: the first mint's word that it set the identity, and the second
	// mint's that it was already there. The instance is the one the sandbox gives every run.
	const (
		setNotice       = "set the VM shift2eu's git identity"
		unchangedNotice = "the VM shift2eu's git identity is already"
	)

	// The mtime the test gives ~/.gitconfig before the second mint, in the past, so a rewrite that keeps the
	// content would still show in the mtime (issue #263).
	past := time.Unix(1700000000, 0)

	// botIdentity is the identity the fake App makes, and the four lines the host sends the VM's script: no
	// token and no expiry, so only the identity is written.
	botIdentity := func(t *testing.T, f *mintFixture) (name, email, stdin string) {
		t.Helper()

		name = f.api.slug + "[bot]"
		email = fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

		return name, email, "\n\n" + name + "\n" + email + "\n"
	}

	setTheMtime := func(t *testing.T, gitconfig string) {
		t.Helper()

		if err := os.Chtimes(gitconfig, past, past); err != nil {
			t.Fatalf("set the mtime of the fake VM's .gitconfig: %v", err)
		}
	}

	assertUnchanged := func(t *testing.T, gitconfig string, before []byte) {
		t.Helper()

		after, err := os.ReadFile(gitconfig)
		if err != nil {
			t.Fatalf("read the fake VM's .gitconfig: %v", err)
		}

		if !bytes.Equal(after, before) {
			t.Errorf("the fake VM's .gitconfig holds\n%s\nafter the second mint, want the bytes it already held\n%s\n(issue #263)",
				after, before)
		}

		info, err := os.Stat(gitconfig)
		if err != nil {
			t.Fatalf("stat the fake VM's .gitconfig: %v", err)
		}

		if got := info.ModTime(); !got.Equal(past) {
			t.Errorf("the fake VM's .gitconfig has the mtime %s after the second mint, want %s: a mint that had nothing to write must not touch the file, not even with a copy that reads the same (issue #263)",
				got, past)
		}
	}

	t.Run("the VM step says set, and unchanged for the home that already holds the bot's identity", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)
		name, email, stdin := botIdentity(t, f)

		gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")

		stdout, stderr, err := f.vmWriteInVM(t, stdin)
		if err != nil {
			t.Fatalf("the VM's script exited non-zero on a home without the bot's identity: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if !strings.Contains(stdout, "git-identity=set") {
			t.Errorf("the VM's script printed no git-identity=set although it wrote the bot identity %s <%s> into a home that had none (issue #263)\nstdout:\n%s\nstderr:\n%s",
				name, email, stdout, stderr)
		}

		if got := f.vmGitValues(t, "user.name"); !slices.Equal(got, []string{name}) {
			t.Fatalf("the fake VM's global user.name is %v, want [%q]: the fixture's first run must have set the bot identity (issue #263)", got, name)
		}

		before, err := os.ReadFile(gitconfig)
		if err != nil {
			t.Fatalf("read the fake VM's .gitconfig: %v", err)
		}

		setTheMtime(t, gitconfig)

		stdout, stderr, err = f.vmWriteInVM(t, stdin)
		if err != nil {
			t.Fatalf("the VM's script exited non-zero on the home it had just given the bot's identity: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if strings.Contains(stdout, "git-identity=set") {
			t.Errorf("the VM's script printed git-identity=set a second time, although the fake VM's ~/.gitconfig already held the bot's identity and nothing was written (issue #263)\nstdout:\n%s\nstderr:\n%s",
				stdout, stderr)
		}

		if !strings.Contains(stdout, "git-identity=unchanged") {
			t.Errorf("the VM's script printed no git-identity=unchanged although the fake VM's ~/.gitconfig already held the bot's identity, so the mint cannot tell a write from no write (issue #263)\nstdout:\n%s\nstderr:\n%s",
				stdout, stderr)
		}

		assertUnchanged(t, gitconfig, before)
	})

	t.Run("the mint says set, and already for the second mint into the same home", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)
		name, email, _ := botIdentity(t, f)

		stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
		if err != nil {
			t.Fatalf("the first mint exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if first := stdout + stderr; !strings.Contains(first, setNotice) {
			t.Errorf("the first mint's output does not say %q although it wrote the bot identity %s <%s> into a home that had none (issue #263)\nstdout:\n%s\nstderr:\n%s",
				setNotice, name, email, stdout, stderr)
		}

		gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")

		before, err := os.ReadFile(gitconfig)
		if err != nil {
			t.Fatalf("read the fake VM's .gitconfig after the first mint: %v", err)
		}

		setTheMtime(t, gitconfig)

		stdout, stderr, err = f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
		if err != nil {
			t.Fatalf("the second mint exited non-zero: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		second := stdout + stderr

		if strings.Contains(second, setNotice) {
			t.Errorf("the second mint says %q although the fake VM's ~/.gitconfig already held the bot's identity and nothing was written: with a refresh minting about every 45 minutes the log claims a change that did not happen (issue #263)\nstdout:\n%s\nstderr:\n%s",
				setNotice, stdout, stderr)
		}

		if !strings.Contains(second, unchangedNotice) {
			t.Errorf("the second mint's output does not say %q, so a maintainer cannot tell from the run that the VM's git identity was already the bot's (issue #263)\nstdout:\n%s\nstderr:\n%s",
				unchangedNotice, stdout, stderr)
		}

		assertUnchanged(t, gitconfig, before)

		f.assertNoLeaks(t, stdout, stderr)
	})
}

// TestAgentsAppTokenMintRefusesAGitConfigAnotherWriterHoldsALockOn pins the second review's finding that the mint
// moves its copy over ~/.gitconfig without honouring git's own lock: a `git config` another writer runs in the VM
// holds ~/.gitconfig.lock and renames its own copy over the file when it is done, so a mint that writes meanwhile
// either loses its write or is lost by the other one. The mint refuses, and never removes a lock it did not create -
// unlike its own copy, which it does remove (issue #241).
func TestAgentsAppTokenMintRefusesAGitConfigAnotherWriterHoldsALockOn(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"
		lockBody        = "a git config another writer runs in the VM holds this lock\n"
	)

	f := newMintFixture(t)

	gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")
	gitconfigBefore := []byte("[user]\n\tname = " + maintainerName + "\n\temail = " + maintainerEmail + "\n")
	writeFileMode(t, gitconfig, gitconfigBefore, 0o600)

	lock := gitconfig + ".lock"
	writeFileMode(t, lock, []byte(lockBody), 0o600)

	stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
	if err == nil {
		t.Fatalf("mint exited 0 although another writer holds %s: the mint's write would race with that writer's, which renames its own copy over ~/.gitconfig when it is done (issue #241)\nstdout: %s\nstderr: %s",
			lock, stdout, stderr)
	}

	// A maintainer reading the refused run must be able to tell it apart from a failure of git or of the VM.
	if text := strings.ToLower(stdout + stderr); !strings.Contains(text, "lock") {
		t.Errorf("the mint refuses because %s is held, but no line mentions a lock, so a maintainer cannot tell what to wait for (issue #241)\nstdout:\n%s\nstderr:\n%s",
			lock, stdout, stderr)
	}

	// A lock no git holds is stale - a git that was killed leaves one behind - and it blocks every later mint for
	// good unless a maintainer removes it: the refusal must name that way out (issue #241).
	if _, ok := lockRemovalNotice(stdout, stderr); !ok {
		t.Errorf("the mint refuses because %s is held, but no line says to remove it when no git runs in the VM: a git that was killed leaves its lock behind, and without that way out every later mint refuses for good (issue #241)\nstdout:\n%s\nstderr:\n%s",
			lock, stdout, stderr)
	}

	after, err := os.ReadFile(gitconfig)
	if err != nil {
		t.Fatalf("read the fake VM's .gitconfig after the refused mint: %v", err)
	}

	if !bytes.Equal(after, gitconfigBefore) {
		t.Errorf("the refused mint changed the fake VM's .gitconfig, although another writer holds its lock:\nbefore:\n%s\nafter:\n%s",
			gitconfigBefore, after)
	}

	// The lock is the other writer's: a mint that removes it makes that writer's write fail, or lets a third one in.
	held, err := os.ReadFile(lock)
	if err != nil {
		t.Fatalf("the refused mint removed %s, a lock it did not create: another writer's git config must survive the mint untouched (%v)",
			lock, err)
	}

	if string(held) != lockBody {
		t.Errorf("%s holds %q after the refused mint, want %q: the mint leaves another writer's lock exactly as it found it (issue #241)",
			lock, held, lockBody)
	}

	if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
		t.Errorf("the mint wrote %s although it never set the git identity: the identity file says what ~/.gitconfig holds (issue #241)",
			agentsIdentityFile)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenMintWritesAGitConfigOnlyItsOwnerCanRead pins that a ~/.gitconfig the mint rewrites ends mode
// 0600 whatever the VM user's umask is: the copy the mint writes the identity into is the lock it created, and a
// file created under a umask of 0002 keeps its group and other bits unless the script takes them off (issue #241).
func TestAgentsAppTokenMintWritesAGitConfigOnlyItsOwnerCanRead(t *testing.T) {
	t.Parallel()

	const (
		maintainerName  = "Example Maintainer"
		maintainerEmail = "maintainer@example.com"
	)

	f := newMintFixture(t)

	gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")
	writeFileMode(t, gitconfig, gitConfigBody(maintainerName, maintainerEmail), 0o644)

	stdout, stderr, err := f.mintWithUmask(t, "0002")
	if err != nil {
		t.Fatalf("mint exited non-zero under the fake VM user's umask 0002: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	wantName := f.api.slug + "[bot]"
	wantEmail := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

	if got := f.vmGitValues(t, "user.name"); !slices.Equal(got, []string{wantName}) {
		t.Errorf("the fake VM's global user.name is %v under umask 0002, want exactly [%q]: the mint rewrites the maintainer's identity (issue #241)",
			got, wantName)
	}

	if got := f.vmGitValues(t, "user.email"); !slices.Equal(got, []string{wantEmail}) {
		t.Errorf("the fake VM's global user.email is %v under umask 0002, want exactly [%q]: the mint rewrites the maintainer's identity (issue #241)",
			got, wantEmail)
	}

	info, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig after the mint: %v", err)
	}

	// The identity is no secret, but a file the mint writes is the owner's: a umask of 0002 must not leave the
	// group and other bits on it, because a later mint writes the same file again.
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the fake VM's ~/.gitconfig is mode %04o after the mint, want 0600: the mint rewrites the file under whatever umask the VM user has, 0002 here, so it must set the mode itself (issue #241)",
			got)
	}

	f.assertNoLeaks(t, stdout, stderr)
}

// TestAgentsAppTokenMintWritesTheTokensGitHubAnswersWithUnchanged pins issue #232: GitHub's installation tokens
// changed from ghs_ plus 36 alphanumerics to ghs_<App ID>_<JWT>, about 520 characters with . and - in them, and
// the mint must write whatever token it was handed into the VM unchanged - the new format, the old one a VM may
// still hold, and a token longer than today's, which no length cap may refuse.
func TestAgentsAppTokenMintWritesTheTokensGitHubAnswersWithUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		prepare func(api *fakeGitHub)
		check   func(t *testing.T, token string)
	}{
		{
			name:    "the new format GitHub mints now",
			prepare: func(api *fakeGitHub) { api.mintNewFormatTokens() },
			check: func(t *testing.T, token string) {
				t.Helper()

				assertNewFormatAppToken(t, "the token the mint wrote", token)
			},
		},
		{
			name:    "the old format GitHub minted before",
			prepare: func(api *fakeGitHub) { api.mintOldFormatTokens() },
			check: func(t *testing.T, token string) {
				t.Helper()

				assertOldFormatAppToken(t, "the token the mint wrote", token)
			},
		},
		{
			// A token longer than the one GitHub sends today: no length cap may refuse it, because the format's
			// length is GitHub's to choose (issue #232).
			name:    "a token longer than the new format's 520 characters",
			prepare: func(api *fakeGitHub) { api.answerMintsWith(newFormatAppTokenWithClaims(5, 1050)) },
			check: func(t *testing.T, token string) {
				t.Helper()

				assertNewFormatAppToken(t, "the token the mint wrote", token)

				if len(token) < 1400 {
					t.Fatalf("the token the mint wrote is %d characters long, want about 1500: this case pins that a length cap does not refuse a longer token (issue #232)", len(token))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			tc.prepare(f.api)

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err != nil {
				t.Fatalf("mint exited non-zero with %s, a token GitHub answers with: %v\nstdout: %s\nstderr: %s\nthe fake API refused: %s",
					tc.name, err, stdout, stderr, strings.Join(f.api.recordedComplaints(), "; "))
			}

			tokens := f.api.minted()
			if len(tokens) != 1 {
				t.Fatalf("the fake API minted %d tokens, want one: mint writes one installation token", len(tokens))
			}

			tc.check(t, tokens[0])

			written := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)
			if written[0] != tokens[0] {
				t.Fatalf("%s holds a %d-character first line, want the %d characters the fake API handed out: the mint writes the token into the VM unchanged (issue #232)",
					agentsTokenFile, len(written[0]), len(tokens[0]))
			}

			if _, err := strconv.ParseInt(written[1], 10, 64); err != nil {
				t.Errorf("%s's second line is not the expiry as Unix seconds: %v", agentsTokenFile, err)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenMintRefusesATokenGitHubMustNeverHandOut pins the other half of the new token format (issue
// #232): a check that accepts the longer, punctuated tokens must still refuse a value that could break the
// two-line token file, a header or the shell - an empty token, whitespace, a quote, a backslash, $, ;, a control
// character or a newline - and never write it or print it. GitHub escapes some of these in its JSON answer, so
// the script sees the escape; either way it must refuse.
func TestAgentsAppTokenMintRefusesATokenGitHubMustNeverHandOut(t *testing.T) {
	t.Parallel()

	// The fake API answers in JSON, so only an empty token, a space, a single quote, $ and ; reach the script
	// as themselves and exercise the allow list directly: a tab, a double quote, a backslash, a control
	// character and a newline arrive as JSON escapes and are refused for the backslash that spells them.
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
		{name: "a newline", token: newFormatAppToken(3) + "\nx"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			f.api.answerMintsWith(tc.token)

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err == nil {
				t.Fatalf("mint exited 0 with a token that holds %s: it cannot be written into the two-line token file the wrapper reads back, so the mint must refuse it (issue #232)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
				t.Errorf("mint wrote %s although GitHub's answer held a token that holds %s: no such token may reach the VM (issue #232)", agentsTokenFile, tc.name)
			}

			if !strings.Contains(strings.ToLower(stderr), "token") {
				t.Errorf("the refusal does not say that no token was minted, so a maintainer cannot tell what failed: %s", stderr)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
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
			// Issue #241: the mint that writes the bot identity also sets the VM user's global git identity, in
			// ~/.gitconfig, so a ~/.gitconfig that is a symlink is refused the way a symlinked token file is: the
			// bot's identity must not be written wherever an agent points the link, and no token goes with it.
			name: "the global git config is a symlink",
			prepare: func(t *testing.T, sb *sandbox) {
				t.Helper()

				target := filepath.Join(sb.root, "outside-gitconfig")
				writeFileMode(t, target, []byte("unchanged\n"), 0o600)

				if err := os.Symlink(target, filepath.Join(sb.vmHome, ".gitconfig")); err != nil {
					t.Fatalf("symlink the fake VM's .gitconfig: %v", err)
				}
			},
			check: func(t *testing.T, sb *sandbox) {
				t.Helper()

				body, err := os.ReadFile(filepath.Join(sb.root, "outside-gitconfig"))
				if err != nil {
					t.Fatalf("read the symlink's target: %v", err)
				}

				if string(body) != "unchanged\n" {
					t.Errorf("the mint wrote the bot's git identity through the symlinked .gitconfig, so it landed wherever an agent pointed the link (issue #241)")
				}

				if _, err := os.Stat(sb.vmPath(agentsTokenFile)); err == nil {
					t.Errorf("the mint wrote %s although the fake VM's .gitconfig is a symlink: it must refuse before it writes the token (issue #241)", agentsTokenFile)
				}
			},
		},
		{
			// Issue #241: a ~/.gitconfig that is a directory is not a git configuration the mint may write into.
			name: "the global git config is not a regular file",
			prepare: func(t *testing.T, sb *sandbox) {
				t.Helper()

				if err := os.MkdirAll(filepath.Join(sb.vmHome, ".gitconfig"), 0o700); err != nil {
					t.Fatalf("create the fake VM's .gitconfig as a directory: %v", err)
				}
			},
			check: func(t *testing.T, sb *sandbox) {
				t.Helper()

				info, err := os.Lstat(filepath.Join(sb.vmHome, ".gitconfig"))
				if err != nil {
					t.Fatalf("stat the fake VM's .gitconfig: %v", err)
				}

				if !info.IsDir() {
					t.Errorf("the mint replaced the directory at the fake VM's .gitconfig, which is not a git configuration it may write (issue #241)")
				}

				if _, err := os.Stat(sb.vmPath(agentsTokenFile)); err == nil {
					t.Errorf("the mint wrote %s although the fake VM's .gitconfig is a directory: it must refuse before it writes the token (issue #241)", agentsTokenFile)
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

// TestAgentsAppTokenMintExitsOneWhenTheBotUserLookupFails pins the reviewer's N-b and the third review's item 3
// for mint: the token is written before the bot user is looked up, so a failed lookup costs the identity, never
// the token, and the run still fails with status 1 - the status a failed mint has (design, host side) - because
// a caller and the maintainer reading the log have to see that the identity is not there.
func TestAgentsAppTokenMintExitsOneWhenTheBotUserLookupFails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
	}{
		{name: "the lookup answers 404", status: http.StatusNotFound},
		{name: "the lookup answers 503", status: http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			f.api.failNextUserLookups(tc.status, 1)

			// The VM holds an identity from before this mint, as a VM an earlier vm-init set up does (issue #241).
			f.setVMGitIdentity(t, "Example Maintainer", "maintainer@example.com")

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")

			if err == nil {
				t.Fatalf("mint exited 0 although the bot user lookup answered HTTP %d: the identity file is not written, and no retry in this run can write it\nstdout: %s\nstderr: %s",
					tc.status, stdout, stderr)
			}

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Errorf("mint exited with %v after a bot user lookup that answered HTTP %d, want status 1: a failed mint is a failed run (design, host side)\nstderr: %s",
					err, tc.status, stderr)
			}

			tokens := f.api.minted()
			if len(tokens) == 0 {
				t.Fatalf("the fake API minted no token, so this test cannot tell whether a minted token reaches the VM; stderr:\n%s", stderr)
			}

			if token := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600); token[0] != tokens[0] {
				t.Errorf("%s does not hold the token the fake API minted before the bot user lookup failed: a failed lookup must never discard a minted token (third review, item 3)\nstderr:\n%s",
					agentsTokenFile, stderr)
			}

			if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
				t.Errorf("%s is there although no bot user lookup succeeded", agentsIdentityFile)
			}

			// Issue #241: this mint sends no identity (the token is written before the bot user is known), so it sets
			// no git identity: what the VM had stays as it is.
			if got := f.vmGitValues(t, "user.name"); !slices.Equal(got, []string{"Example Maintainer"}) {
				t.Errorf("the fake VM's global user.name is %v, want [\"Example Maintainer\"]: this mint wrote a token and no identity, so it must leave the VM's git identity alone (issue #241)", got)
			}

			if got := f.vmGitValues(t, "user.email"); !slices.Equal(got, []string{"maintainer@example.com"}) {
				t.Errorf("the fake VM's global user.email is %v, want [\"maintainer@example.com\"]: this mint wrote a token and no identity, so it must leave the VM's git identity alone (issue #241)", got)
			}

			f.assertNoLeaks(t, stdout, stderr)
		})
	}
}

// TestAgentsAppTokenRefreshReplacesTheTokenAndSaysHowToStartItAgain pins the loop make deck starts: it mints
// again before the token expires, replaces the VM's copy, and on SIGTERM says it stopped and names the
// command that starts it again. It does so for whichever format GitHub answers with, the new one it mints since
// 2026 and the old one a VM may still hold (issue #232).
func TestAgentsAppTokenRefreshReplacesTheTokenAndSaysHowToStartItAgain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		format appTokenFormat
	}{
		{name: "the new format GitHub mints now", format: newFormatTokens},
		{name: "the old format GitHub minted before", format: oldFormatTokens},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			switch tc.format {
			case oldFormatTokens:
				f.api.mintOldFormatTokens()
			default:
				f.api.mintNewFormatTokens()
			}

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

			assertStopLineNamesWhereToRestart(t, stopped)

			tokens := f.api.minted()
			if len(tokens) < 2 {
				t.Fatalf("the fake API minted %d tokens, want at least two: the refresh replaces the token before it expires", len(tokens))
			}

			for i, minted := range tokens {
				assertAppTokenFormat(t, fmt.Sprintf("the token the fake API minted %d", i+1), minted, tc.format)
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
		})
	}
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
		name string
		// clock builds the fake VM's reading when the subtest runs, not when the table is built: the script
		// compares the reading with the Mac's clock and warns when the two are more than a minute apart, so a
		// reading taken here would be a minute stale once the package is slow enough under t.Parallel(), and
		// the zero-padded case would warn where it expects no warning (issue #354).
		clock       func() string
		wantWarning bool
	}{
		{name: "a 14-digit reading is no clock", clock: func() string { return "17000000000000000" }},
		{name: "a 13-digit reading is no clock", clock: func() string { return "17000000000000" }},
		{name: "a 12-digit reading is the last one accepted", clock: func() string { return "170000000000" }, wantWarning: true},
		// A zero-padded reading with an 8 or a 9: the shell would read the leading 0 as octal and end on the
		// digit, so it is read as the number its digits are, which is the Mac's clock and no warning at all
		// (reviewer N-a, auditor L-A).
		{name: "a zero-padded reading with an 8 or a 9 is the clock it says", clock: func() string {
			return zeroPadWithAnEightOrNine(time.Now().Unix())
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)

			// Read right before the mint runs, so the script's own clock read is seconds behind it at most.
			clock := tc.clock()
			f.sb.useFakeVMClock(t, f.env, clock)

			stdout, stderr, err := f.sb.run(t, scriptPath(t, agentsMintScript), []string{"mint"}, f.env, "")
			if err != nil {
				t.Fatalf("mint exited non-zero with the VM's clock reading %s: a reading the script cannot use is no reason to leave the VM without a token: %v\nstdout: %s\nstderr: %s",
					clock, err, stdout, stderr)
			}

			readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)

			if got := strings.Contains(stderr, offsetWarning); got != tc.wantWarning {
				t.Errorf("the mint compares the VM's clock reading %s with the Mac's = %v, want %v: only 1 to 12 digits are a clock the script may compare (second review, item 6)\nstderr: %s",
					clock, got, tc.wantWarning, stderr)
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
