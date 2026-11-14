// Helpers shared by the devenv tests that pin the agents App's token plumbing
// (PRD #161 milestone 1, design: .dot-agent-deck/prd161m1-design.md).
//
// Every script test runs in a sandbox: a fake HOME for the process that runs the host script, a fake HOME
// for the VM user a fake limactl stands in for, and a bin directory first on PATH that holds the fake
// limactl and a recording curl and openssl. The shims log their argv and then run the real tool, so a test
// can prove that the token, the JWT and no line of the App's key ever reach a command line. The GitHub API
// is an httptest server on 127.0.0.1 that verifies the RS256 JWT against the public half of a key the test
// invented in t.TempDir(): no test reaches the real API and no test holds a real credential (CLAUDE.md
// rule 3).
package devenv_test

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// moduleRoot is the repository root, from the package directory the tests run in.
const moduleRoot = "../.."

// The files of the development environment these tests pin, relative to the repository root.
const (
	vmInitScript = "dev/vm-init.sh"
	makefilePath = "Makefile"
)

// The two files the tokens script writes into the VM, relative to the VM user's home (design: PRD #161
// milestone 1, VM side). They are vars, not consts: a const whose name says what it is and whose value is a
// string literal trips gosec's hardcoded-credential rule, and these are paths, not credentials.
var (
	agentsMintScript   = filepath.Join("dev", "agents-app-token.sh")
	ghWrapperScript    = filepath.Join("dev", "gh-app-token.sh")
	agentsTokenFile    = filepath.Join(".config", "shift2eu", "agents-app-token")
	agentsIdentityFile = filepath.Join(".config", "shift2eu", "agents-app-identity")
)

// readRepoFile reads a file of the repository, failing when it is not there.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(moduleRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	return string(body)
}

// scriptPath returns the absolute path of a script of dev/, failing the test when the script does not exist
// yet: PRD #161 milestone 1 adds dev/agents-app-token.sh and dev/gh-app-token.sh, and a missing one is the
// RED this test exists for, never a reason to skip.
func scriptPath(t *testing.T, rel string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(moduleRoot, rel))
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s does not exist: PRD #161 milestone 1 adds it (design: .dot-agent-deck/prd161m1-design.md): %v", rel, err)
	}

	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", rel)
	}

	return path
}

// realTool returns the absolute path of a tool the sandbox does not replace, and skips the test when the
// machine has none: a stock Mac and the Linux CI both have curl and openssl.
func realTool(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed: the script under test needs it", name)
	}

	return path
}

// sandbox is one throwaway environment for a run of a host script.
type sandbox struct {
	root     string // the directory holding everything, from t.TempDir()
	hostHome string // HOME of the process that runs the host script
	vmHome   string // HOME of the fake VM user; the token and identity files must land here
	binDir   string // first on PATH: the fake limactl and the recording curl and openssl
	argvLog  string // one tool=<name> line and one arg=<arg> line per shimmed call
}

// newSandbox builds a sandbox with the fake limactl and the recording curl and openssl, and removes it with
// the test.
func newSandbox(t *testing.T) *sandbox {
	t.Helper()

	root := t.TempDir()
	sb := &sandbox{
		root:     root,
		hostHome: filepath.Join(root, "host-home"),
		vmHome:   filepath.Join(root, "vm-home"),
		binDir:   filepath.Join(root, "bin"),
		argvLog:  filepath.Join(root, "argv.log"),
	}

	for _, dir := range []string{sb.hostHome, sb.vmHome, sb.binDir, filepath.Join(root, "tmp")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	sb.writeFakeLimactl(t)
	sb.writeTripwireGH(t)
	for _, tool := range []string{"curl", "openssl"} {
		sb.writeRecordingShim(t, tool)
	}

	return sb
}

// baseEnv is the environment one run of a script gets: only the variables the scripts may read, never the
// test process's own, so no real credential of the machine can reach the script (CLAUDE.md rule 3).
func (sb *sandbox) baseEnv() map[string]string {
	return map[string]string{
		"PATH":                   sb.binDir + ":/usr/local/bin:/usr/bin:/bin",
		"HOME":                   sb.hostHome,
		"TMPDIR":                 filepath.Join(sb.root, "tmp"),
		"SHIFT2EU_TEST_ARGV_LOG": sb.argvLog,
		"SHIFT2EU_TEST_VM_HOME":  sb.vmHome,
		// The state dir of the refresh's pid, log and lock files, outside the workspace and never the
		// test machine's own (PRD #161 milestone 1 review, item 7).
		"XDG_STATE_HOME": filepath.Join(sb.root, "state"),
		"LIMA_INSTANCE":  "shift2eu",
		"LIMACTL":        filepath.Join(sb.binDir, "limactl"),
	}
}

// vmPath is a path inside the fake VM user's home, such as agentsTokenFile.
func (sb *sandbox) vmPath(rel string) string {
	return filepath.Join(sb.vmHome, filepath.FromSlash(rel))
}

// stateDir is where the refresh keeps its pid, log and lock files: ${XDG_STATE_HOME}/shift2eu.
func (sb *sandbox) stateDir() string {
	return filepath.Join(sb.root, "state", "shift2eu")
}

// argvLogText is what the recording shims and the fake limactl have logged so far.
func (sb *sandbox) argvLogText(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(sb.argvLog)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}

		t.Fatalf("read %s: %v", sb.argvLog, err)
	}

	return string(body)
}

// run runs a script of dev/ through sh with args and stdin, under env alone, from the sandbox's host home
// (so a script that writes to its working directory writes inside the sandbox, never in the repository).
func (sb *sandbox) run(t *testing.T, script string, args []string, env map[string]string, stdin string) (string, string, error) {
	t.Helper()

	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = sb.hostHome
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// envList renders an environment for os/exec, sorted so two runs see the same order.
func envList(env map[string]string) []string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}

	slices.Sort(names)

	list := make([]string, 0, len(names))
	for _, name := range names {
		list = append(list, name+"="+env[name])
	}

	return list
}

// fakeLimactl is dev/limactl for the tests: it records its argv and then runs the sh -c command the host
// script asks the VM to run, on this machine, with HOME set to the fake VM user's home. Its stdin is passed
// through, so a host script that hands the token over standard input works unchanged, and one that puts it
// on the limactl command line is caught by the argv log.
const fakeLimactl = `#!/bin/sh
# Stands in for limactl in the devenv tests: records its argv and runs the command after the instance name.
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
if [ -n "${SHIFT2EU_TEST_LIMA_CAPTURE:-}" ]; then
  # Capture mode: a test that only wants what a recipe sends over standard input (make deck, make
  # vm-gh-wrapper) records it and runs nothing on this machine.
  cat >> "${SHIFT2EU_TEST_LIMA_CAPTURE}" || exit 1
  printf '\n--- end of call ---\n' >> "${SHIFT2EU_TEST_LIMA_CAPTURE}"
  exit 0
fi
HOME="${SHIFT2EU_TEST_VM_HOME:?set SHIFT2EU_TEST_VM_HOME}"
export HOME
at=0
found=0
for a in "$@"; do
  at=$((at + 1))
  case "$a" in
  sh | bash | dash) found=1; break ;;
  esac
done
if [ "${found}" -ne 1 ]; then
  echo 'fake limactl: the argv holds no sh, bash or dash to run' >&2
  exit 64
fi
shift $((at - 1))
exec "$@"
`

// recordingShim is the recording stand-in for one tool: it logs its argv and then runs the real tool from
// the same stdin and environment.
const recordingShim = `#!/bin/sh
# Records the argv of the real %[1]s it runs, so a test can prove no secret reaches a command line.
{
  printf 'tool=%[1]s\n'
  for a in "$@"; do printf 'arg=%%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
exec %[2]s "$@"
`

func (sb *sandbox) writeFakeLimactl(t *testing.T) {
	t.Helper()

	path := filepath.Join(sb.binDir, "limactl")

	//nolint:gosec // the fake limactl is a script the tests run, so it must be executable
	if err := os.WriteFile(path, []byte(fakeLimactl), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ghTrapScript is what the gh on PATH is in a sandbox: a wrapper that runs gh from PATH instead of the
// one SHIFT2EU_REAL_GH names fails the test, and never runs whatever gh the machine has installed.
const ghTrapScript = `#!/bin/sh
echo 'the wrapper ran the gh on PATH, not the one SHIFT2EU_REAL_GH names' >&2
exit 97
`

func (sb *sandbox) writeTripwireGH(t *testing.T) {
	t.Helper()

	path := filepath.Join(sb.binDir, "gh")

	//nolint:gosec // the trap is a script the tests run, so it must be executable
	if err := os.WriteFile(path, []byte(ghTrapScript), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// blockedCurlShim logs its argv and then fails the way curl reports a connection it never made, so a test
// can prove a URL was accepted while it proves nothing reached that URL: no test calls the real GitHub API
// (PRD #161 milestone 1's security rules).
const blockedCurlShim = `#!/bin/sh
{
  printf 'tool=curl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
echo 'curl: (7) Failed to connect: blocked by the devenv tests' >&2
exit 7
`

// blockCurl makes curl unable to reach anything, so a test can pin that a URL was accepted without ever
// sending a request to it.
func (sb *sandbox) blockCurl(t *testing.T) {
	t.Helper()

	path := filepath.Join(sb.binDir, "curl")

	//nolint:gosec // the blocked curl is a script the tests run, so it must be executable
	if err := os.WriteFile(path, []byte(blockedCurlShim), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// failingLimactl stands in for a VM that is not running: it fails while the file SHIFT2EU_TEST_LIMA_FAIL_WHILE
// names exists, and runs the command otherwise, so a test can let a loop hit the failure and then recover.
const failingLimactl = `#!/bin/sh
{
  printf 'tool=limactl\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
if [ -e "${SHIFT2EU_TEST_LIMA_FAIL_WHILE:?set SHIFT2EU_TEST_LIMA_FAIL_WHILE}" ]; then
  echo 'limactl: the instance is not running' >&2
  exit 1
fi
exec "${SHIFT2EU_TEST_REAL_LIMACTL:?set SHIFT2EU_TEST_REAL_LIMACTL}" "$@"
`

// useFailingLimactl makes the sandbox's limactl fail while the file at marker exists, and returns the fake
// limactl it delegates to once the marker is gone.
func (sb *sandbox) useFailingLimactl(t *testing.T, marker string) {
	t.Helper()

	real := filepath.Join(sb.binDir, "limactl-real")

	//nolint:gosec // the fake limactl is a script the tests run, so it must be executable
	if err := os.WriteFile(real, []byte(fakeLimactl), 0o700); err != nil {
		t.Fatalf("write %s: %v", real, err)
	}

	path := filepath.Join(sb.binDir, "limactl")

	//nolint:gosec // the failing limactl is a script the tests run, so it must be executable
	if err := os.WriteFile(path, []byte(failingLimactl), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("create %s: %v", marker, err)
	}
}

func (sb *sandbox) writeRecordingShim(t *testing.T, name string) {
	t.Helper()

	real := realTool(t, name)
	path := filepath.Join(sb.binDir, name)

	shim := fmt.Sprintf(recordingShim, name, strconv.Quote(real))
	if name == "curl" {
		shim = fmt.Sprintf(recordingCurlShim, strconv.Quote(real))
	}

	//nolint:gosec // the recording shim is a script the tests run, so it must be executable
	if err := os.WriteFile(path, []byte(shim), 0o700); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// recordingCurlShim is the recording stand-in for curl. It logs its argv, and it refuses any URL that is not
// the fake API on this machine: no devenv test may reach the real GitHub API, not even when the script under
// test reads an API URL hook of another name and so falls back to https://api.github.com (PRD #161 milestone
// 1's security rules). The URL reaches curl on its standard input, so the shim reads it there and hands it on
// from a shell variable: no copy of it, and so of the JWT it carries, is ever written to a file.
const recordingCurlShim = `#!/bin/sh
{
  printf 'tool=curl\n'
  for a in "$@"; do printf 'arg=%%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
cfg="$(cat)"
if printf '%%s\n' "${cfg}" | grep -E '^[[:space:]]*url[[:space:]]*=' | grep -vE '^[[:space:]]*url[[:space:]]*=[[:space:]]*"https?://(127\.0\.0\.1|localhost)([:/"]|$)' >/dev/null; then
  echo 'the devenv tests refuse to reach any URL but the fake API on this machine' >&2
  exit 7
fi
printf '%%s\n' "${cfg}" | %s "$@"
`

// invocations returns the argv of every call of one tool the recording shims logged, in call order.
func invocations(log, tool string) [][]string {
	var (
		calls  [][]string
		cur    []string
		ofTool bool
	)

	for _, line := range strings.Split(log, "\n") {
		switch {
		case strings.HasPrefix(line, "tool="):
			if ofTool {
				calls = append(calls, cur)
			}

			cur, ofTool = nil, line == "tool="+tool
		case ofTool && strings.HasPrefix(line, "arg="):
			cur = append(cur, strings.TrimPrefix(line, "arg="))
		}
	}

	if ofTool {
		calls = append(calls, cur)
	}

	return calls
}

// nonEmptyLines counts the lines of text that hold anything.
func nonEmptyLines(text string) int {
	count := 0

	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}

	return count
}

// toolDir returns the directory of a tool the machine has, for a PATH a script needs it on.
func toolDir(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is not installed: the test needs it", name)
	}

	return filepath.Dir(path)
}

// safeBuffer is a buffer two goroutines share: one copies a running script's output into it, the test reads
// it while the script runs.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// runningScript is a script a test started and keeps watching: a refresh loop is meant to keep running, so a
// test reads its output while it runs and can see it survive a failure instead of returning.
type runningScript struct {
	cmd    *exec.Cmd
	stdout safeBuffer
	stderr safeBuffer
	exited chan struct{} // closed once Wait has returned
	status error         // what Wait returned; read it only after exited is closed
}

// start runs a script of dev/ in the background, under env alone, from the sandbox's host home, and removes
// it with the test.
func (sb *sandbox) start(t *testing.T, script string, args []string, env map[string]string) *runningScript {
	t.Helper()

	r := &runningScript{
		cmd:    exec.Command("sh", append([]string{script}, args...)...),
		exited: make(chan struct{}),
	}
	r.cmd.Env = envList(env)
	r.cmd.Dir = sb.hostHome
	r.cmd.Stdout = &r.stdout
	r.cmd.Stderr = &r.stderr

	if err := r.cmd.Start(); err != nil {
		t.Fatalf("start %s %s: %v", script, strings.Join(args, " "), err)
	}

	go func() {
		r.status = r.cmd.Wait()
		close(r.exited)
	}()

	t.Cleanup(func() { r.kill(t) })

	return r
}

// out and errOut are the script's stdout and stderr so far.
func (r *runningScript) out() string    { return r.stdout.String() }
func (r *runningScript) errOut() string { return r.stderr.String() }

// running reports whether the script is still running.
func (r *runningScript) running() bool {
	select {
	case <-r.exited:
		return false
	default:
		return true
	}
}

// waitExit waits up to timeout for the script to exit and reports whether it did.
func (r *runningScript) waitExit(timeout time.Duration) bool {
	select {
	case <-r.exited:
		return true
	case <-time.After(timeout):
		return false
	}
}

// signal sends sig to the script.
func (r *runningScript) signal(t *testing.T, sig syscall.Signal) {
	t.Helper()

	if !r.running() {
		return
	}

	if err := r.cmd.Process.Signal(sig); err != nil && r.running() {
		t.Fatalf("send %v to the script: %v", sig, err)
	}
}

// exitCode is the script's exit status: 0, the status it exited with, or -1 when a signal ended it. It waits
// for the exit, so call it after waitExit or signal.
func (r *runningScript) exitCode(t *testing.T) int {
	t.Helper()

	<-r.exited

	var exitErr *exec.ExitError

	switch {
	case r.status == nil:
		return 0
	case errors.As(r.status, &exitErr):
		return exitErr.ExitCode()
	default:
		t.Fatalf("wait for the script: %v", r.status)

		return 0
	}
}

// kill ends the script if it is still running: the cleanup of every started script.
func (r *runningScript) kill(t *testing.T) {
	t.Helper()

	if !r.running() {
		return
	}

	_ = r.cmd.Process.Kill()

	if !r.waitExit(30 * time.Second) {
		t.Errorf("a script the test started did not exit even after a kill")
	}
}

// secret is a value that must never reach a command line, a log line or an output. kind names it for the
// failure message; the value itself is never printed, so a failing test leaks nothing.
type secret struct {
	kind  string
	value string
}

// assertNoSecrets fails when text holds any secret, naming the secret's kind and never its value.
func assertNoSecrets(t *testing.T, where string, text string, secrets []secret) {
	t.Helper()

	for _, s := range secrets {
		if s.value == "" {
			continue
		}

		if strings.Contains(text, s.value) {
			t.Errorf("%s holds the %s; it must never appear there (CLAUDE.md rule 3, PRD #161 milestone 1)", where, s.kind)
		}
	}
}

// assertNoSecretsInTree fails when a regular file under dir holds a secret, skipping the paths in skip.
func assertNoSecretsInTree(t *testing.T, dir string, skip []string, secrets []secret) {
	t.Helper()

	var files []string

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.Type().IsRegular() && !isSkipped(path, skip) {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		assertNoSecrets(t, path, string(body), secrets)
	}
}

// isSkipped reports whether path is one of skip, or sits under one of the directories skip names.
func isSkipped(path string, skip []string) bool {
	return slices.ContainsFunc(skip, func(s string) bool {
		return path == s || strings.HasPrefix(path, s+string(filepath.Separator))
	})
}

// keySecrets returns one secret per non-empty line of a PEM private key, and the whole PEM, so a test can
// check that no line of the key reaches a command line, a log or an output.
func keySecrets(pemBytes []byte) []secret {
	secrets := []secret{{kind: "App private key", value: string(pemBytes)}}

	for _, line := range strings.Split(string(pemBytes), "\n") {
		if line == "" {
			continue
		}

		secrets = append(secrets, secret{kind: "line of the App private key", value: line})
	}

	return secrets
}

// inventedKey is a private key a test generated: never a real App key (PRD #161 milestone 1's security
// rules).
type inventedKey struct {
	path   string
	pem    []byte
	public *rsa.PublicKey
}

// writeInventedKey generates an RSA key and writes its PEM to path with mode, creating the directory above
// it. mode 0600 is the only one the script accepts; the tests use a wider one to pin that it refuses it.
func writeInventedKey(t *testing.T, path string, mode os.FileMode) inventedKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate an RSA key for the test: %v", err)
	}

	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	writeFileMode(t, path, encoded, mode)

	return inventedKey{path: path, pem: encoded, public: &key.PublicKey}
}

// writeFileMode writes content to path with mode, creating the directory above it. The file is created
// 0600 and then chmodded, because a plain write loses the group and world bits to the umask.
func writeFileMode(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// fakeGitHub is a stand-in for the GitHub REST API: it verifies the JWT of every request against the public
// half of the invented key, and mints a new token per access_tokens request, so a test can tell a mint from
// a refresh. It answers on 127.0.0.1 only and never reaches the real API.
type fakeGitHub struct {
	url            string
	appID          string
	installationID string
	slug           string
	botUserID      int64
	public         *rsa.PublicKey
	// status, when non-zero, is the status every access_tokens request gets instead of a token, so a test
	// can pin what the script does when GitHub refuses to mint.
	status int
	// lifetime is how long the tokens the fake API hands out last; the zero value is GitHub's hour.
	lifetime time.Duration

	mu         sync.Mutex
	requests   []string      // "<method> <path>" of every request
	posts      []string      // the path of every POST
	accepts    []string      // the Accept header of every request, in request order
	jwts       []string      // the Bearer value of every request the JWT check accepted
	complaints []string      // what was wrong with a request, for the test to report without the secret
	tokens     []string      // the tokens handed out, in order
	expiresAt  []int64       // the expires_at of each token, Unix seconds
	failures   []mintFailure // the transient failures still to answer, one per mint
}

// mintFailure is one access_tokens request the fake API answers with a failure instead of a token: a status,
// or a dropped connection, which curl reports as status 000.
type mintFailure struct {
	status int  // the status to answer with; ignored when drop is set
	drop   bool // close the connection without a reply instead of answering
}

// newFakeGitHub starts the fake API for one test and stops it with the test.
func newFakeGitHub(t *testing.T, public *rsa.PublicKey) *fakeGitHub {
	t.Helper()

	f := &fakeGitHub{
		appID:          "424242",
		installationID: "77889900",
		slug:           "shift2eu-agents",
		botUserID:      987654321,
		public:         public,
	}

	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)

	f.url = server.URL

	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	bearer, err := f.checkAuthorization(r)

	f.mu.Lock()
	refuse := f.status
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.accepts = append(f.accepts, r.Header.Get("Accept"))

	if r.Method == http.MethodPost {
		f.posts = append(f.posts, r.URL.Path)
	}

	if err != nil {
		f.complaints = append(f.complaints, err.Error())
	} else {
		f.jwts = append(f.jwts, bearer)
	}
	f.mu.Unlock()

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}` + "\n"))

		return
	}

	if refuse != 0 && r.Method == http.MethodPost {
		http.Error(w, `{"message":"Bad credentials"}`, refuse)

		return
	}

	if failure, failed := f.takeMintFailure(r.Method); failed {
		if failure.drop {
			dropConnection(w)

			return
		}

		http.Error(w, `{"message":"GitHub is having trouble"}`, failure.status)

		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/app/installations/"+f.installationID+"/access_tokens":
		f.mint(w)
	case r.Method == http.MethodGet && r.URL.Path == "/app":
		writeJSON(w, map[string]string{"slug": f.slug, "id": f.appID})
	case r.Method == http.MethodGet && r.URL.Path == "/users/"+f.slug+"[bot]":
		writeJSON(w, map[string]any{"id": f.botUserID, "login": f.slug + "[bot]"})
	default:
		http.NotFound(w, r)
	}
}

// checkAuthorization returns the Bearer value of r after verifying it is the App's JWT. Its errors never
// hold the JWT.
func (f *fakeGitHub) checkAuthorization(r *http.Request) (string, error) {
	const prefix = "Bearer "

	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", errors.New("a request carries no Bearer authorization")
	}

	raw := strings.TrimPrefix(header, prefix)

	claims, err := verifyJWT(raw, f.public)
	if err != nil {
		return "", err
	}

	if claims.iss != f.appID {
		return "", errors.New("the JWT's iss is not the App ID")
	}

	return raw, nil
}

// takeMintFailure returns the failure the next access_tokens request must get, consuming it.
func (f *fakeGitHub) takeMintFailure(method string) (mintFailure, bool) {
	if method != http.MethodPost {
		return mintFailure{}, false
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.failures) == 0 {
		return mintFailure{}, false
	}

	failure := f.failures[0]
	f.failures = f.failures[1:]

	return failure, true
}

// failNextMints makes the next count access_tokens requests answer status instead of minting: the transient
// failure a long-lived refresh must survive (PRD #161 milestone 1 review, item 1).
func (f *fakeGitHub) failNextMints(status, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for range count {
		f.failures = append(f.failures, mintFailure{status: status})
	}
}

// dropNextMints makes the next count access_tokens requests answer by closing the connection, which curl sees
// as status 000, the way a Mac that went to sleep or a network that went away shows up.
func (f *fakeGitHub) dropNextMints(count int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for range count {
		f.failures = append(f.failures, mintFailure{drop: true})
	}
}

// setTokenLifetime makes the tokens the fake API mints last d, so a test can pin that the expiry written into
// the VM follows GitHub's own expires_at as well as the local hour.
func (f *fakeGitHub) setTokenLifetime(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lifetime = d
}

// dropConnection answers a request by closing the connection with no reply at all.
func dropConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, `{"message":"cannot drop the connection"}`, http.StatusInternalServerError)

		return
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, `{"message":"cannot drop the connection"}`, http.StatusInternalServerError)

		return
	}

	_ = conn.Close()
}

// mint answers one access_tokens request with a new token, valid for the fake API's lifetime (an hour by
// default, as GitHub mints them).
func (f *fakeGitHub) mint(w http.ResponseWriter) {
	f.mu.Lock()

	lifetime := f.lifetime
	if lifetime == 0 {
		lifetime = time.Hour
	}

	expiresAt := time.Now().Add(lifetime).Unix()
	token := fmt.Sprintf("ghs_%036x", len(f.tokens)+1)
	f.tokens = append(f.tokens, token)
	f.expiresAt = append(f.expiresAt, expiresAt)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":      token,
		"expires_at": time.Unix(expiresAt, 0).UTC().Format(time.RFC3339),
	})
}

// writeJSON answers one request with body. The fake API's own answers are small and fixed, so a write
// error has no one to tell: the request's test sees a missing field instead.
func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(body)
}

// refuseMintsWith makes every access_tokens request answer status instead of minting, so a test can pin what
// the script does when GitHub refuses a token.
func (f *fakeGitHub) refuseMintsWith(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.status = status
}

// minted returns the tokens the fake API handed out, in order.
func (f *fakeGitHub) minted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.tokens)
}

// expiryOf returns the expires_at of the n-th token, in Unix seconds.
func (f *fakeGitHub) expiryOf(n int) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.expiresAt[n]
}

// posted returns the path of every POST the fake API saw.
func (f *fakeGitHub) posted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.posts)
}

// accepted returns the Accept header of every request, in request order.
func (f *fakeGitHub) accepted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.accepts)
}

// bearerTokens returns the JWT of every request the fake API accepted, for the leak assertions.
func (f *fakeGitHub) bearerTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.jwts)
}

// recordedComplaints returns why the fake API refused a request, without the request's secret.
func (f *fakeGitHub) recordedComplaints() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.complaints)
}

// jwtClaims are the claims dev/agents-app-token.sh must set (design: iat = now-60, exp = now+540, iss = the
// App ID).
type jwtClaims struct {
	iss string
	iat int64
	exp int64
}

// verifyJWT checks a compact JWS of the shape GitHub requires of an App: three dot-separated base64url
// parts, an RS256 header, and a signature over the first two parts made with the App's private key. Its
// errors never hold the JWT.
func verifyJWT(raw string, public *rsa.PublicKey) (jwtClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return jwtClaims{}, errors.New("the JWT does not consist of three dot-separated parts")
	}

	header, err := jwtPart(parts[0])
	if err != nil {
		return jwtClaims{}, fmt.Errorf("the JWT's header is not base64url: %w", err)
	}

	var head struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(header, &head); err != nil {
		return jwtClaims{}, fmt.Errorf("the JWT's header is not JSON: %w", err)
	}

	if head.Alg != "RS256" {
		return jwtClaims{}, errors.New("the JWT's alg is not RS256")
	}

	claimsPart, err := jwtPart(parts[1])
	if err != nil {
		return jwtClaims{}, fmt.Errorf("the JWT's claims are not base64url: %w", err)
	}

	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsPart, &claims); err != nil {
		return jwtClaims{}, fmt.Errorf("the JWT's claims are not JSON: %w", err)
	}

	if claims.Exp <= claims.Iat || claims.Exp-claims.Iat > 600 {
		return jwtClaims{}, fmt.Errorf("the JWT's exp - iat is %d seconds, which is neither positive nor at most 600", claims.Exp-claims.Iat)
	}

	signature, err := jwtPart(parts[2])
	if err != nil {
		return jwtClaims{}, fmt.Errorf("the JWT's signature is not base64url: %w", err)
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(public, crypto.SHA256, digest[:], signature); err != nil {
		return jwtClaims{}, errors.New("the JWT's signature does not verify with the public half of the invented key")
	}

	return jwtClaims{iss: claims.Iss, iat: claims.Iat, exp: claims.Exp}, nil
}

// jwtPart decodes one part of a JWT, padded or not.
func jwtPart(raw string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return decoded, nil
	}

	return base64.URLEncoding.DecodeString(raw)
}

// waitFor polls cond until it holds or timeout passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}
