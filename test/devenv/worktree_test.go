// Pins dev/worktree.sh, the script make worktree runs (CLAUDE.md rule 4, issue #573): the branch names it
// accepts, the worktree it creates or resumes for one, and every refusal it prints in such a worktree's way.
//
// A branch of the pull request form <type>/<issue>-<slug> gets .worktrees/<slug> (the branch with every / replaced
// by -): a new one cut from the latest origin/main without an upstream, so it can never push to main, or an
// existing one resumed, tracking origin/<branch> when it was pushed from elsewhere. A
// name of no such form gets the usage refusal, and every worktree in the branch's way is refused rather than
// reused: the main checkout, a worktree outside a .worktrees/, a directory of the slug's name that is no
// worktree, that directory as another branch's worktree, one on a detached HEAD, and a registered worktree whose
// directory was removed.
//
// Renovate's branches are named renovate/<name>, with dots in it (renovate/anthropic-ai-claude-code-linux-x64-2.x),
// and a unit finishing a Renovate pull request's manual step must reopen one the supported way. So a name
// renovate/<name> - <name> a lower-case word of letters, digits, dots, underscores and hyphens, holding no .. -
// is accepted too, but resumed only: when origin/renovate/<name> exists the script prints
// .worktrees/renovate-<name>, a worktree on the local branch tracking that remote at its commit, and the second
// run prints the same path. A local branch left by an earlier run is resumed only when it is the fetched remote
// branch itself, at its commit and tracking it; a local branch at another commit, or tracking something else (the
// checkout's autoSetupMerge often leaves one tracking origin/main), is refused as differing from origin, so no
// unit ever works on a stale base. When the remote branch does not exist, only a local one does, origin is missing
// or unreachable, or the name holds anything the pattern or git's own ref rules refuse, the script refuses the
// name: it never creates a Renovate branch, and its own local refs never stand in for origin's.
//
// Every case builds its own throwaway repository in t.TempDir(): a bare origin holding main, a seed clone that
// pushes the branches which come from elsewhere, and the checkout the script runs in. No case touches this
// worktree, another checkout's origin or a real renovate/ branch, and none needs the network.
package devenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// worktreeScript is the script behind make worktree, run by its path and shebang, as the recipe runs it.
const worktreeScript = "dev/worktree.sh"

// worktreeRunBound bounds one run of the script: a git command the throwaway repository leaves waiting would
// otherwise block its subtest until go test's package timeout, which names nothing (issue #512).
const worktreeRunBound = 2 * time.Minute

// worktreeRenovateBranch is a branch name as Renovate writes it, with a dot in its version: the pull request
// form <type>/<issue>-<slug> refuses it, and a unit finishing that pull request's manual step needs it resumed
// (issue #573).
const worktreeRenovateBranch = "renovate/anthropic-ai-claude-code-linux-x64-2.x"

// worktreeGitConfig is the throwaway global git configuration of every case: a commit needs an identity, and
// the machine's own git configuration must reach no run of the script.
const worktreeGitConfig = `[user]
	name = shift2eu devenv test
	email = test@example.com
`

// worktreeFixture is one throwaway repository for a case of these tests: a bare origin holding main, a seed
// clone that pushes what comes from elsewhere, and the checkout the script runs in, which owns origin as its
// remote as a maintainer's checkout does.
type worktreeFixture struct {
	root     string            // the directory holding every repository of the case
	origin   string            // the bare repository the seed clone and the checkout share
	seed     string            // the clone that pushes main and the branches which come from elsewhere
	checkout string            // the checkout the script runs in, its links resolved as git reports it
	git      string            // the machine's git
	env      map[string]string // the environment every git of the case and the script run under
}

// newWorktreeFixture builds the case's throwaway repository: origin holding one commit of main, and the
// checkout, a clone of it, in which every run of the script starts.
func newWorktreeFixture(t *testing.T) *worktreeFixture {
	t.Helper()

	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is not installed: the script and its throwaway repository need it (issue #573)")
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash is not installed: the script's shebang runs it (issue #573)")
	}

	root := t.TempDir()

	f := &worktreeFixture{
		root:     root,
		origin:   filepath.Join(root, "origin.git"),
		seed:     filepath.Join(root, "seed"),
		checkout: filepath.Join(root, "checkout"),
		git:      git,
		env: map[string]string{
			"PATH":                filepath.Dir(git) + ":/usr/local/bin:/usr/bin:/bin",
			"HOME":                filepath.Join(root, "home"),
			"GIT_CONFIG_GLOBAL":   filepath.Join(root, "gitconfig"),
			"GIT_CONFIG_SYSTEM":   "/dev/null",
			"GIT_CONFIG_NOSYSTEM": "1",
			"GIT_TERMINAL_PROMPT": "0",
			"TMPDIR":              filepath.Join(root, "tmp"),
		},
	}

	for _, dir := range []string{f.env["HOME"], f.env["TMPDIR"]} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writeFileMode(t, f.env["GIT_CONFIG_GLOBAL"], []byte(worktreeGitConfig), 0o600)

	// The bare origin starts on main, as the maintainer's does, before anything clones it.
	f.mustGitIn(t, root, "init", "--quiet", "--bare", f.origin)
	f.mustGitIn(t, root, "init", "--quiet", f.seed)
	f.mustGitIn(t, f.seed, "symbolic-ref", "HEAD", "refs/heads/main")
	f.mustGitIn(t, f.seed, "remote", "add", "origin", f.origin)
	f.mustGitIn(t, f.seed, "commit", "--quiet", "--allow-empty", "-m", "the first commit of main")
	f.mustGitIn(t, f.seed, "push", "--quiet", "origin", "main")
	f.mustGitIn(t, f.origin, "symbolic-ref", "HEAD", "refs/heads/main")
	f.mustGitIn(t, root, "clone", "--quiet", f.origin, f.checkout)

	// git reports the checkout's physical path, which differs from t.TempDir's when the temporary directory is
	// a link (macOS's /var to /private/var), and the script prints the path of git's answer.
	resolved, err := filepath.EvalSymlinks(f.checkout)
	if err != nil {
		t.Fatalf("resolve %s: %v", f.checkout, err)
	}

	f.checkout = resolved

	return f
}

// gitIn runs git in dir under the case's throwaway environment and returns what it wrote on stdout.
func (f *worktreeFixture) gitIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(f.git, args...)
	cmd.Dir = dir
	cmd.Env = envList(f.env)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %w\nstderr: %s", strings.Join(args, " "), dir, err, stderr.String())
	}

	return stdout.String(), nil
}

// mustGitIn is gitIn for a command the case cannot go on without, and returns its output without the
// newline, as git prints a revision.
func (f *worktreeFixture) mustGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := f.gitIn(t, dir, args...)
	if err != nil {
		t.Fatal(err)
	}

	return strings.TrimSpace(out)
}

// publishBranch pushes a branch of the case's seed clone into origin, as a branch pushed from elsewhere, and
// returns its commit: the checkout sees it only after the script's own fetch.
func (f *worktreeFixture) publishBranch(t *testing.T, branch string) string {
	t.Helper()

	f.mustGitIn(t, f.seed, "checkout", "--quiet", "--detach", "main")
	f.mustGitIn(t, f.seed, "commit", "--quiet", "--allow-empty", "-m", "a commit on "+branch)
	sha := f.mustGitIn(t, f.seed, "rev-parse", "HEAD")
	f.mustGitIn(t, f.seed, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch)

	return sha
}

// advanceMain pushes one more commit of main into origin and returns it, so that a branch cut from
// origin/main is told apart from one cut from the checkout's own, older main.
func (f *worktreeFixture) advanceMain(t *testing.T) string {
	t.Helper()

	f.mustGitIn(t, f.seed, "checkout", "--quiet", "main")
	f.mustGitIn(t, f.seed, "commit", "--quiet", "--allow-empty", "-m", "a further commit of main")
	sha := f.mustGitIn(t, f.seed, "rev-parse", "HEAD")
	f.mustGitIn(t, f.seed, "push", "--quiet", "origin", "main")

	return sha
}

// worktreeDir is the directory the script puts branch's worktree in: the slug replaces every / of the branch.
func (f *worktreeFixture) worktreeDir(branch string) string {
	return filepath.Join(f.checkout, ".worktrees", strings.ReplaceAll(branch, "/", "-"))
}

// branchAt returns the commit the checkout's local branch points at.
func (f *worktreeFixture) branchAt(t *testing.T, branch string) string {
	t.Helper()

	return f.mustGitIn(t, f.checkout, "rev-parse", "refs/heads/"+branch)
}

// upstreamOf returns the upstream of the checkout's local branch as git names it, or "" without one.
func (f *worktreeFixture) upstreamOf(t *testing.T, branch string) string {
	t.Helper()

	return f.mustGitIn(t, f.checkout, "for-each-ref", "--format=%(upstream:short)", "refs/heads/"+branch)
}

// hasBranch reports whether the checkout has the local branch.
func (f *worktreeFixture) hasBranch(t *testing.T, branch string) bool {
	t.Helper()

	_, err := f.gitIn(t, f.checkout, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false
	}

	if err != nil {
		t.Fatal(err)
	}

	return true
}

// worktreeRun is one run of the script: what it printed and the code it exited with.
type worktreeRun struct {
	stdout string
	stderr string
	code   int
}

// path is the worktree path the run printed: its stdout without the line's newline.
func (r worktreeRun) path() string {
	return strings.TrimSuffix(r.stdout, "\n")
}

// run runs the script with branch in the checkout, as make worktree does, and returns its output and exit
// code.
func (f *worktreeFixture) run(t *testing.T, branch string) worktreeRun {
	t.Helper()

	cmd := exec.Command(scriptPath(t, worktreeScript), branch)
	cmd.Dir = f.checkout
	cmd.Env = envList(f.env)

	stdout, stderr, err := runBounded(cmd, worktreeRunBound)

	code := 0

	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run dev/worktree.sh %q in %s: %v\nstdout: %s\nstderr: %s", branch, f.checkout, err, stdout, stderr)
		}

		code = exit.ExitCode()
	}

	return worktreeRun{stdout: stdout, stderr: stderr, code: code}
}

// TestWorktreeRefusesANameThatIsNoPullRequestBranch pins the usage refusal of every name that is neither a pull
// request branch nor a Renovate one: exit 2, the word usage first on stderr, and nothing on stdout, so no
// caller takes a message for a worktree's path (issue #573).
func TestWorktreeRefusesANameThatIsNoPullRequestBranch(t *testing.T) {
	t.Parallel()

	names := []struct {
		name   string
		branch string
	}{
		{name: "no name at all", branch: ""},
		{name: "main itself", branch: "main"},
		{name: "a type without an issue", branch: "feat/x"},
		{name: "an issue without a slug", branch: "feat/12"},
		{name: "a type in capitals", branch: "Feat/12-x"},
		{name: "an unknown type", branch: "wip/12-x"},
		{name: "a capital in the slug", branch: "feat/12-X"},
	}

	for _, tc := range names {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorktreeFixture(t)

			got := f.run(t, tc.branch)

			if got.code != 2 {
				t.Errorf("dev/worktree.sh %q exited %d, want the usage refusal's 2 (issue #573)\nstdout: %s\nstderr: %s", tc.branch, got.code, got.stdout, got.stderr)
			}

			if !strings.HasPrefix(got.stderr, "usage:") {
				t.Errorf("dev/worktree.sh %q printed %q on stderr, want a message starting with \"usage:\" (issue #573)", tc.branch, got.stderr)
			}

			if got.stdout != "" {
				t.Errorf("dev/worktree.sh %q printed %q on stdout, want nothing: a refused name names no worktree (issue #573)", tc.branch, got.stdout)
			}
		})
	}
}

// TestWorktreeCreatesANewBranch pins a new branch's worktree: the path the script prints, the branch checked out
// there, and that it is cut from the latest origin/main, which the script fetches, without an upstream, so it
// can never push to main (issue #573).
func TestWorktreeCreatesANewBranch(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	main := f.advanceMain(t)

	got := f.run(t, "feat/12-x")

	if got.code != 0 {
		t.Fatalf("dev/worktree.sh feat/12-x exited %d, want 0 (issue #573)\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	dir := f.worktreeDir("feat/12-x")
	if got.path() != dir {
		t.Errorf("dev/worktree.sh feat/12-x printed %q, want %q (issue #573)", got.path(), dir)
	}

	if on := f.mustGitIn(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); on != "feat/12-x" {
		t.Errorf("the worktree %s is on %q, want the branch feat/12-x (issue #573)", dir, on)
	}

	if at := f.branchAt(t, "feat/12-x"); at != main {
		t.Errorf("the new branch feat/12-x is at %s, want origin/main's %s, the commit the script fetched: a new branch is cut from the latest main, not from the checkout's older one (issue #573)", at, main)
	}

	if upstream := f.upstreamOf(t, "feat/12-x"); upstream != "" {
		t.Errorf("the new branch feat/12-x tracks %q, want no upstream: it must never push to main (issue #573)", upstream)
	}
}

// TestWorktreeResumesABranchPushedFromElsewhere pins that a branch that exists only on origin is resumed from
// it: the script's own fetch finds it, the worktree's branch tracks it and is at its commit (issue #573).
func TestWorktreeResumesABranchPushedFromElsewhere(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	sha := f.publishBranch(t, "feat/12-x")

	got := f.run(t, "feat/12-x")

	if got.code != 0 {
		t.Fatalf("dev/worktree.sh feat/12-x exited %d, want 0 (issue #573)\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
	}

	dir := f.worktreeDir("feat/12-x")
	if got.path() != dir {
		t.Errorf("dev/worktree.sh feat/12-x printed %q, want %q (issue #573)", got.path(), dir)
	}

	if upstream := f.upstreamOf(t, "feat/12-x"); upstream != "origin/feat/12-x" {
		t.Errorf("the resumed branch feat/12-x tracks %q, want origin/feat/12-x: a branch pushed from elsewhere is resumed tracking it (issue #573)", upstream)
	}

	if head := f.mustGitIn(t, dir, "rev-parse", "HEAD"); head != sha {
		t.Errorf("the worktree %s is at %s, want the remote branch's commit %s (issue #573)", dir, head, sha)
	}
}

// TestWorktreeRefusesAWorktreeInTheBranchsWay pins every worktree the script refuses rather than reuses: exit 1,
// a message starting with worktree: that names what stands in the way, and nothing on stdout (issue #573).
func TestWorktreeRefusesAWorktreeInTheBranchsWay(t *testing.T) {
	t.Parallel()

	const branch = "feat/12-x"

	cases := []struct {
		name    string
		setup   func(t *testing.T, f *worktreeFixture)
		message string // a stable fragment of the refusal
	}{
		{
			name: "the branch is checked out in the main checkout",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mustGitIn(t, f.checkout, "checkout", "--quiet", "--no-track", "-b", branch, "origin/main")
			},
			message: "main checkout",
		},
		{
			name: "the branch is checked out in a worktree outside .worktrees",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mustGitIn(t, f.checkout, "worktree", "add", "--quiet", "--no-track", "-b", branch, filepath.Join(f.root, "elsewhere"), "origin/main")
			},
			message: "not a pull request worktree",
		},
		{
			name: ".worktrees/feat-12-x exists but is not a worktree",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mkdir(t, f.worktreeDir(branch))
			},
			message: "is not a worktree",
		},
		{
			name: ".worktrees/feat-12-x is the worktree of another branch",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mustGitIn(t, f.checkout, "worktree", "add", "--quiet", "--no-track", "-b", "feat/12-y", f.worktreeDir(branch), "origin/main")
			},
			message: "is the worktree of 'feat/12-y'",
		},
		{
			name: ".worktrees/feat-12-x is a worktree on a detached HEAD",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mustGitIn(t, f.checkout, "worktree", "add", "--quiet", "--detach", f.worktreeDir(branch), "origin/main")
			},
			message: "detached HEAD",
		},
		{
			name: "the worktree of the branch lost its directory",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				dir := f.worktreeDir(branch)
				f.mustGitIn(t, f.checkout, "worktree", "add", "--quiet", "--no-track", "-b", branch, dir, "origin/main")

				if err := os.RemoveAll(dir); err != nil {
					t.Fatalf("remove %s: %v", dir, err)
				}
			},
			message: "git worktree remove",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorktreeFixture(t)
			tc.setup(t, f)

			got := f.run(t, branch)

			if got.code != 1 {
				t.Errorf("dev/worktree.sh %s exited %d, want the refusal's 1 (issue #573)\nstdout: %s\nstderr: %s", branch, got.code, got.stdout, got.stderr)
			}

			if !strings.HasPrefix(got.stderr, "worktree: ") {
				t.Errorf("dev/worktree.sh %s printed %q on stderr, want a message starting with \"worktree: \" (issue #573)", branch, got.stderr)
			}

			if !strings.Contains(got.stderr, tc.message) {
				t.Errorf("the refusal of %s printed %q, want it to say %q (issue #573)", branch, got.stderr, tc.message)
			}

			if got.stdout != "" {
				t.Errorf("dev/worktree.sh %s printed %q on stdout, want nothing: a refusal names no worktree (issue #573)", branch, got.stdout)
			}
		})
	}
}

// TestWorktreeResumesARenovateBranch pins that an existing remote Renovate branch is resumed as any other
// existing branch is, the second run printing the same path, and that the worktree is the remote branch's
// commit, not a new branch from main (issue #573).
func TestWorktreeResumesARenovateBranch(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	sha := f.publishBranch(t, worktreeRenovateBranch)

	got := f.run(t, worktreeRenovateBranch)

	if got.code != 0 {
		t.Fatalf("dev/worktree.sh %s exited %d, want 0: an existing remote Renovate branch is resumed (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, got.code, got.stdout, got.stderr)
	}

	dir := f.worktreeDir(worktreeRenovateBranch)
	if got.path() != dir {
		t.Errorf("dev/worktree.sh %s printed %q, want %q (issue #573)", worktreeRenovateBranch, got.path(), dir)
	}

	if on := f.mustGitIn(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); on != worktreeRenovateBranch {
		t.Errorf("the worktree %s is on %q, want the branch %s (issue #573)", dir, on, worktreeRenovateBranch)
	}

	if upstream := f.mustGitIn(t, f.checkout, "rev-parse", "--abbrev-ref", worktreeRenovateBranch+"@{upstream}"); upstream != "origin/"+worktreeRenovateBranch {
		t.Errorf("the branch %s tracks %q, want origin/%s: a resumed branch tracks the remote it was pushed to (issue #573)", worktreeRenovateBranch, upstream, worktreeRenovateBranch)
	}

	if head := f.mustGitIn(t, dir, "rev-parse", "HEAD"); head != sha {
		t.Errorf("the worktree %s is at %s, want the remote branch's commit %s: the script resumes that branch, it does not cut a new one from main (issue #573)", dir, head, sha)
	}

	again := f.run(t, worktreeRenovateBranch)

	if again.code != 0 {
		t.Fatalf("the second run of dev/worktree.sh %s exited %d, want the resume's 0 (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, again.code, again.stdout, again.stderr)
	}

	if again.path() != dir {
		t.Errorf("the second run printed %q, want the same worktree %q: a branch that has its worktree gets that worktree's path (issue #573)", again.path(), dir)
	}

	// An existing worktree is resumed before the script looks at origin, so a rerun still prints its path once
	// origin is gone: the work a unit did after the first run stays reachable (issue #573).
	f.mustGitIn(t, f.checkout, "remote", "remove", "origin")

	withoutOrigin := f.run(t, worktreeRenovateBranch)

	if withoutOrigin.code != 0 {
		t.Fatalf("dev/worktree.sh %s exited %d with origin removed, want 0: an existing worktree is resumed without looking at origin (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, withoutOrigin.code, withoutOrigin.stdout, withoutOrigin.stderr)
	}

	if withoutOrigin.path() != dir {
		t.Errorf("the run with origin removed printed %q, want the same worktree %q (issue #573)", withoutOrigin.path(), dir)
	}
}

// TestWorktreeRefusesARenovateNameThatIsNoRenovateBranch pins the usage refusal of a renovate/ name that is no
// Renovate branch: exit 2 and a message that mentions the form, so a unit reads what to type instead (issue
// #573).
func TestWorktreeRefusesARenovateNameThatIsNoRenovateBranch(t *testing.T) {
	t.Parallel()

	names := []struct {
		name   string
		branch string
	}{
		{name: "an empty name", branch: "renovate/"},
		{name: "a name starting with a dot", branch: "renovate/.x"},
		{name: "a name starting with a dot-dot", branch: "renovate/..x"},
		{name: "a name holding a dot-dot", branch: "renovate/a..b"},
		{name: "a name starting with a hyphen", branch: "renovate/-x"},
		{name: "a name in capitals", branch: "renovate/Claude-Code"},
		{name: "a name with a further slash", branch: "renovate/a/b"},
		{name: "a name git refuses for its .lock ending", branch: "renovate/x.lock"},
		{name: "a name git refuses for its dot ending", branch: "renovate/x."},
	}

	for _, tc := range names {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorktreeFixture(t)

			got := f.run(t, tc.branch)

			if got.code != 2 {
				t.Errorf("dev/worktree.sh %q exited %d, want the usage refusal's 2 (issue #573)\nstdout: %s\nstderr: %s", tc.branch, got.code, got.stdout, got.stderr)
			}

			if !strings.HasPrefix(got.stderr, "usage:") {
				t.Errorf("dev/worktree.sh %q printed %q on stderr, want a message starting with \"usage:\" (issue #573)", tc.branch, got.stderr)
			}

			if !strings.Contains(got.stderr, "renovate/") {
				t.Errorf("the usage message printed %q, want it to name the renovate/<name> form, so a unit reads what to type for a Renovate branch (issue #573)", got.stderr)
			}

			if got.stdout != "" {
				t.Errorf("dev/worktree.sh %q printed %q on stdout, want nothing: a refused name names no worktree (issue #573)", tc.branch, got.stdout)
			}
		})
	}
}

// TestWorktreeRefusesARenovateBranchItCannotResume pins that the script creates no Renovate branch: an origin
// without it, an origin that is unreachable, and a checkout without an origin each get a refusal that names the
// branch, and neither a local branch nor a worktree is left behind. The last two say that whether
// origin/<branch> exists is unknown rather than absent, because the script never guesses from its own refs
// (issue #573).
func TestWorktreeRefusesARenovateBranchItCannotResume(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		setup   func(t *testing.T, f *worktreeFixture)
		message string // a stable fragment of the refusal
	}{
		{
			name:    "origin has no such Renovate branch",
			setup:   func(*testing.T, *worktreeFixture) {},
			message: "no such remote",
		},
		{
			name: "origin is unreachable",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mustGitIn(t, f.checkout, "remote", "set-url", "origin", filepath.Join(f.root, "gone.git"))
			},
			message: "could not be fetched",
		},
		{
			name: "the checkout has no origin",
			setup: func(t *testing.T, f *worktreeFixture) {
				t.Helper()

				f.mustGitIn(t, f.checkout, "remote", "remove", "origin")
			},
			message: "could not be fetched",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorktreeFixture(t)
			tc.setup(t, f)

			got := f.run(t, worktreeRenovateBranch)

			if got.code != 1 {
				t.Fatalf("dev/worktree.sh %s exited %d, want the refusal's 1: the remote Renovate branch cannot be confirmed and the script creates none (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, got.code, got.stdout, got.stderr)
			}

			// The refusal is the script's own line. Git may print its diagnostics first: a failed fetch of an
			// unreachable origin writes its fatal: lines to the same stderr.
			if !strings.HasPrefix(got.stderr, "worktree: ") && !strings.Contains(got.stderr, "\nworktree: ") {
				t.Errorf("dev/worktree.sh %s printed %q on stderr, want a refusal line starting with \"worktree: \" (issue #573)", worktreeRenovateBranch, got.stderr)
			}

			if !strings.Contains(got.stderr, worktreeRenovateBranch) {
				t.Errorf("the refusal printed %q, want it to name the branch %s (issue #573)", got.stderr, worktreeRenovateBranch)
			}

			if !strings.Contains(got.stderr, tc.message) {
				t.Errorf("the refusal printed %q, want it to say %q (issue #573)", got.stderr, tc.message)
			}

			if got.stdout != "" {
				t.Errorf("dev/worktree.sh %s printed %q on stdout, want nothing: a refusal names no worktree (issue #573)", worktreeRenovateBranch, got.stdout)
			}

			if f.hasBranch(t, worktreeRenovateBranch) {
				t.Errorf("dev/worktree.sh %s created the local branch %s, want none: it never creates a Renovate branch (issue #573)", worktreeRenovateBranch, worktreeRenovateBranch)
			}

			if _, err := os.Stat(f.worktreeDir(worktreeRenovateBranch)); err == nil {
				t.Errorf("dev/worktree.sh %s left the worktree %s behind, want none (issue #573)", worktreeRenovateBranch, f.worktreeDir(worktreeRenovateBranch))
			}
		})
	}
}

// TestWorktreeRefusesALocalRenovateBranchWithoutTheRemoteOne pins that only an origin's Renovate branch is
// resumed: a local one the remote lacks is refused, as a refusal rather than a usage error, and no worktree is
// made of it (issue #573).
func TestWorktreeRefusesALocalRenovateBranchWithoutTheRemoteOne(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	f.mustGitIn(t, f.checkout, "branch", worktreeRenovateBranch, "origin/main")

	got := f.run(t, worktreeRenovateBranch)

	if got.code != 1 {
		t.Fatalf("dev/worktree.sh %s exited %d, want the refusal's 1: only origin's Renovate branch is resumed, never a local one (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, got.code, got.stdout, got.stderr)
	}

	if !strings.HasPrefix(got.stderr, "worktree: ") {
		t.Errorf("dev/worktree.sh %s printed %q on stderr, want a message starting with \"worktree: \" (issue #573)", worktreeRenovateBranch, got.stderr)
	}

	if !strings.Contains(got.stderr, worktreeRenovateBranch) {
		t.Errorf("the refusal printed %q, want it to name the branch %s (issue #573)", got.stderr, worktreeRenovateBranch)
	}

	if !strings.Contains(got.stderr, "no such remote") {
		t.Errorf("the refusal printed %q, want it to say no such remote Renovate branch exists, and that the script creates none (issue #573)", got.stderr)
	}

	if _, err := os.Stat(f.worktreeDir(worktreeRenovateBranch)); err == nil {
		t.Errorf("dev/worktree.sh %s made a worktree of the local branch, %s, want none (issue #573)", worktreeRenovateBranch, f.worktreeDir(worktreeRenovateBranch))
	}
}

// TestWorktreeRefusesALocalRenovateBranchThatDiffersFromOrigin pins that a local Renovate branch left by an earlier
// run is resumed only when it is origin's branch: at another commit than the fetched origin/renovate/<name>, or not
// tracking it, the script refuses the name as differing from origin, prints nothing on stdout, makes no worktree
// and leaves the local branch at its own commit, so a unit is never handed a stale base it would have to force
// (issue #573).
func TestWorktreeRefusesALocalRenovateBranchThatDiffersFromOrigin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// setup leaves the local branch as the case needs it, with origin/renovate/<name> at another commit when
		// the case wants one, and returns the commit the branch must still be at after the refusal.
		setup func(t *testing.T, f *worktreeFixture) (leftAt string)
	}{
		{
			name: "the local branch is at an older commit and tracks origin/main",
			setup: func(t *testing.T, f *worktreeFixture) string {
				t.Helper()

				f.mustGitIn(t, f.checkout, "branch", "--track", worktreeRenovateBranch, "origin/main")
				leftAt := f.branchAt(t, worktreeRenovateBranch)
				f.publishBranch(t, worktreeRenovateBranch)

				return leftAt
			},
		},
		{
			name: "the local branch is at an older commit and has no upstream",
			setup: func(t *testing.T, f *worktreeFixture) string {
				t.Helper()

				f.mustGitIn(t, f.checkout, "branch", "--no-track", worktreeRenovateBranch, "origin/main")
				leftAt := f.branchAt(t, worktreeRenovateBranch)
				f.publishBranch(t, worktreeRenovateBranch)

				return leftAt
			},
		},
		{
			name: "the local branch is at origin's commit and has no upstream",
			setup: func(t *testing.T, f *worktreeFixture) string {
				t.Helper()

				sha := f.publishBranch(t, worktreeRenovateBranch)
				f.mustGitIn(t, f.checkout, "fetch", "--quiet", "origin")
				f.mustGitIn(t, f.checkout, "branch", "--no-track", worktreeRenovateBranch, "origin/"+worktreeRenovateBranch)

				return sha
			},
		},
		{
			name: "the local branch is at origin's commit and tracks origin/main",
			setup: func(t *testing.T, f *worktreeFixture) string {
				t.Helper()

				sha := f.publishBranch(t, worktreeRenovateBranch)
				f.mustGitIn(t, f.checkout, "fetch", "--quiet", "origin")
				f.mustGitIn(t, f.checkout, "branch", "--no-track", worktreeRenovateBranch, "origin/"+worktreeRenovateBranch)
				f.mustGitIn(t, f.checkout, "branch", "--set-upstream-to=origin/main", worktreeRenovateBranch)

				return sha
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorktreeFixture(t)
			leftAt := tc.setup(t, f)

			got := f.run(t, worktreeRenovateBranch)

			if got.code != 1 {
				t.Fatalf("dev/worktree.sh %s exited %d, want the refusal's 1: a local branch that is not origin's, at its commit and tracking it, is no base to work on (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, got.code, got.stdout, got.stderr)
			}

			// The refusal is the script's own line. Git may print its diagnostics first: a failed fetch writes its
			// fatal: lines to the same stderr.
			if !strings.HasPrefix(got.stderr, "worktree: ") && !strings.Contains(got.stderr, "\nworktree: ") {
				t.Errorf("dev/worktree.sh %s printed %q on stderr, want a refusal line starting with \"worktree: \" (issue #573)", worktreeRenovateBranch, got.stderr)
			}

			if !strings.Contains(got.stderr, worktreeRenovateBranch) {
				t.Errorf("the refusal printed %q, want it to name the branch %s (issue #573)", got.stderr, worktreeRenovateBranch)
			}

			if !strings.Contains(got.stderr, "differs from origin") {
				t.Errorf("the refusal printed %q, want it to say the local branch differs from origin, so a unit reads why it is not handed that base (issue #573)", got.stderr)
			}

			if got.stdout != "" {
				t.Errorf("dev/worktree.sh %s printed %q on stdout, want nothing: a refusal names no worktree (issue #573)", worktreeRenovateBranch, got.stdout)
			}

			if _, err := os.Stat(f.worktreeDir(worktreeRenovateBranch)); err == nil {
				t.Errorf("dev/worktree.sh %s left the worktree %s behind, want none: the local branch is no base to work on (issue #573)", worktreeRenovateBranch, f.worktreeDir(worktreeRenovateBranch))
			}

			if at := f.branchAt(t, worktreeRenovateBranch); at != leftAt {
				t.Errorf("dev/worktree.sh %s moved the local branch %s to %s, want it left at its own commit %s: a refused branch is never rewritten (issue #573)", worktreeRenovateBranch, worktreeRenovateBranch, at, leftAt)
			}
		})
	}
}

// TestWorktreeResumesALocalRenovateBranchTrackingOrigin pins the state an earlier run leaves once its worktree is
// removed: a local renovate/<name> at origin's commit and tracking it is resumed as it is - the path the script
// prints, the worktree on the branch at that commit, and the branch still tracking origin/renovate/<name>, never
// a new branch from main (issue #573).
func TestWorktreeResumesALocalRenovateBranchTrackingOrigin(t *testing.T) {
	t.Parallel()

	f := newWorktreeFixture(t)
	sha := f.publishBranch(t, worktreeRenovateBranch)

	// A worktree of the branch, tracking origin's, removed again: what a finished unit leaves behind.
	dir := f.worktreeDir(worktreeRenovateBranch)
	f.mustGitIn(t, f.checkout, "fetch", "--quiet", "origin")
	f.mustGitIn(t, f.checkout, "worktree", "add", "--quiet", "--track", "-b", worktreeRenovateBranch, dir, "origin/"+worktreeRenovateBranch)
	f.mustGitIn(t, f.checkout, "worktree", "remove", dir)

	got := f.run(t, worktreeRenovateBranch)

	if got.code != 0 {
		t.Fatalf("dev/worktree.sh %s exited %d, want 0: a local branch at origin's commit and tracking it is resumed (issue #573)\nstdout: %s\nstderr: %s", worktreeRenovateBranch, got.code, got.stdout, got.stderr)
	}

	if got.path() != dir {
		t.Errorf("dev/worktree.sh %s printed %q, want %q (issue #573)", worktreeRenovateBranch, got.path(), dir)
	}

	if on := f.mustGitIn(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); on != worktreeRenovateBranch {
		t.Errorf("the worktree %s is on %q, want the branch %s (issue #573)", dir, on, worktreeRenovateBranch)
	}

	if head := f.mustGitIn(t, dir, "rev-parse", "HEAD"); head != sha {
		t.Errorf("the worktree %s is at %s, want origin's commit %s: the resumed branch is not rewritten (issue #573)", dir, head, sha)
	}

	if upstream := f.upstreamOf(t, worktreeRenovateBranch); upstream != "origin/"+worktreeRenovateBranch {
		t.Errorf("the resumed branch %s tracks %q, want origin/%s (issue #573)", worktreeRenovateBranch, upstream, worktreeRenovateBranch)
	}
}

// mkdir creates dir, a directory a case puts in the branch's way, and fails the test when it cannot.
func (f *worktreeFixture) mkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}
