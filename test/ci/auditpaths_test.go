// Pins the fence of PRD #175 milestone 2 around the audit App's report pull requests:
// .github/scripts/audit-paths.sh.
//
// The check's contract, run with AUDIT_PATHS_AUTHOR, AUDIT_PATHS_BASE and AUDIT_PATHS_HEAD in the
// environment and the current directory inside a git repository that holds both commits:
//
//   - It compares the tree of BASE with the tree of HEAD, renames not detected: a rename counts as the
//     deletion of the old path and the addition of the new one.
//   - It passes, exit 0, for every author but shift2eu-audit[bot], whatever the change: the check exists
//     for that one App's report pull requests and costs nothing elsewhere.
//   - For shift2eu-audit[bot] it passes when every changed path is under docs/security/ and every added or
//     modified entry there is a regular, non-executable file, git mode 100644: the App opens docs-only pull
//     requests that add docs/security/audits/YYYY-MM-DD-<run_id>.md.
//   - It fails, non-zero, otherwise, and its output names each offending path. A path holding a newline or
//     another control character may be escaped however the check likes; what it may not do is pass.
//   - It fails closed, non-zero, when the author is unset or empty, and when BASE or HEAD is unset, empty
//     or not a commit of the repository: a run that cannot tell who opened the pull request, or what it
//     changes, is no pass.
//
// Each case builds its own repository below its t.TempDir(): git init, a base commit holding README.md,
// internal/x.go and docs/security/README.md, then a head commit with the case's change. git runs in an
// environment this harness builds for the case alone - HOME below the case's temporary directory, the
// system configuration off (GIT_CONFIG_NOSYSTEM), the global one empty (GIT_CONFIG_GLOBAL=/dev/null) and
// an identity of the harness's own - so neither the developer's git configuration nor a hook it points at
// can change a commit. The check runs in the same environment, which childEnv builds from nothing of this
// process's own but PATH: PATH, HOME, LANG and LC_ALL as C, TMPDIR, and the case's variables. Nothing a
// credential of the developing machine lives in reaches the run, so none can appear in a failure's output.
//
// The test needs bash and git, and no network.
package ci_test

import (
	"bytes"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// auditPathsScript is the check under test, run through bash as its workflow runs it.
	auditPathsScript = ".github/scripts/audit-paths.sh"

	// auditPathsAuthor is the one author the check applies to: the audit App's login (PRD #175, Decision 1).
	auditPathsAuthor = "shift2eu-audit[bot]"

	// The three variables the workflow hands the check; it reads no other.
	auditPathsAuthorEnv = "AUDIT_PATHS_AUTHOR"
	auditPathsBaseEnv   = "AUDIT_PATHS_BASE"
	auditPathsHeadEnv   = "AUDIT_PATHS_HEAD"

	// auditPathsMissingSHA is a commit SHA no case's repository holds, as a BASE or HEAD that names no
	// commit of the repository is.
	auditPathsMissingSHA = "0000000000000000000000000000000000000000"
)

// auditPathsBaseFiles are the files the base commit holds, at slash-separated paths: a document, a Go
// file outside docs/security/ and the App's own directory, which a case's head commit changes.
var auditPathsBaseFiles = map[string]string{
	"README.md":               "# A repository fixture for the path check.\n",
	"internal/x.go":           "package internal\n",
	"docs/security/README.md": "# Security\n\nA fixture the audit App may change.\n",
}

// auditPathsGitEnv is what a case's git commands and the check itself run with, besides the harness's own
// PATH, HOME, LANG, LC_ALL and TMPDIR: no system configuration and an empty global one, so no setting or
// hook of the developer's can change a commit, and an identity that names no developer.
var auditPathsGitEnv = map[string]string{
	"GIT_CONFIG_NOSYSTEM": "1",
	"GIT_CONFIG_GLOBAL":   os.DevNull,
	"GIT_AUTHOR_NAME":     "shift2eu audit-paths test",
	"GIT_AUTHOR_EMAIL":    "audit-paths@example.com",
	"GIT_COMMITTER_NAME":  "shift2eu audit-paths test",
	"GIT_COMMITTER_EMAIL": "audit-paths@example.com",
	"GIT_TERMINAL_PROMPT": "0",
}

// auditPathsChange is the change a case's head commit makes in repo, whose git commands run with env.
type auditPathsChange func(t *testing.T, repo string, env []string)

// auditPathsCase is one run of the check: the head commit's change, the environment the run gets, and what
// the run must do.
type auditPathsCase struct {
	name string
	// change makes the head commit's change; nil commits no change at all, so both commits hold one tree.
	change auditPathsChange
	// author is AUDIT_PATHS_AUTHOR; empty means the audit App's login, which most cases audit.
	author string
	// authorEmpty sets AUDIT_PATHS_AUTHOR to the empty string, and authorUnset leaves it out of the
	// environment: both must fail closed.
	authorEmpty bool
	authorUnset bool
	// unsetBase and unsetHead leave AUDIT_PATHS_BASE or AUDIT_PATHS_HEAD out of the environment, and
	// missingBase and missingHead set one to a SHA the repository does not hold: each must fail closed.
	unsetBase   bool
	unsetHead   bool
	missingBase bool
	missingHead bool
	// wantPass says the run must exit 0; false says it must exit non-zero.
	wantPass bool
	// wantOutput holds substrings the run's output must contain: an offending path, for a failing run.
	wantOutput []string
}

// TestAuditPathsScriptFencesAuditPullRequests runs the check's cases against repositories the test builds.
func TestAuditPathsScriptFencesAuditPullRequests(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH: %s needs it", tool, auditPathsScript)
		}
	}

	for _, tc := range auditPathsCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runAuditPathsCase(t, tc)
		})
	}
}

// auditPathsCases returns the cases, in the order the contract above lists them.
func auditPathsCases() []auditPathsCase {
	// report is a dated report's path, the one shape of pull request the App opens.
	const report = "docs/security/audits/2026-11-22-123.md"

	// outside changes README.md and adds a workflow under .github/: what no report pull request may touch.
	outside := func(t *testing.T, repo string, _ []string) {
		t.Helper()
		auditPathsWrite(t, repo, "README.md", "# A changed README.\n")
		auditPathsWrite(t, repo, ".github/workflows/x.yml", "name: x\n")
	}

	// add returns a change that adds path with content.
	add := func(path, content string) auditPathsChange {
		return func(t *testing.T, repo string, _ []string) {
			t.Helper()
			auditPathsWrite(t, repo, path, content)
		}
	}

	return []auditPathsCase{
		{
			name:     "renovate[bot] changing the README and a workflow",
			author:   "renovate[bot]",
			change:   outside,
			wantPass: true,
		},
		{
			name:     "octocat changing the README and a workflow",
			author:   "octocat",
			change:   outside,
			wantPass: true,
		},
		{
			name:     "the agents' App changing the README and a workflow",
			author:   "shift2eu-agents[bot]",
			change:   outside,
			wantPass: true,
		},
		{
			// A login that ends in [bot] is one GitHub owns, so a user cannot register one: shift2eu-audit
			// and shift2eu-audit[bot] are two accounts, and only the second one's pull requests are fenced.
			name:     "shift2eu-audit without [bot] changing the README",
			author:   "shift2eu-audit",
			change:   add("README.md", "# A changed README.\n"),
			wantPass: true,
		},
		{
			// The comparison is exact, not case-folded: Shift2eu-audit[bot] is another login.
			name:     "Shift2eu-audit[bot] in another case changing the README",
			author:   "Shift2eu-audit[bot]",
			change:   add("README.md", "# A changed README.\n"),
			wantPass: true,
		},
		{
			name:     "the audit App adding a report",
			change:   add(report, "# The report\n"),
			wantPass: true,
		},
		{
			name:     "the audit App modifying docs/security/README.md",
			change:   add("docs/security/README.md", "# Security\n\nA link to the new report.\n"),
			wantPass: true,
		},
		{
			name:     "the audit App with no change at all",
			wantPass: true,
		},
		{
			name:       "the audit App changing the README",
			change:     add("README.md", "# A changed README.\n"),
			wantOutput: []string{"README.md"},
		},
		{
			name: "the audit App adding a report and changing a Go file",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()
				auditPathsWrite(t, repo, report, "# The report\n")
				auditPathsWrite(t, repo, "internal/x.go", "package internal\n\n// A change beside a report.\n")
			},
			wantOutput: []string{"internal/x.go"},
		},
		{
			name:       "the audit App adding a file under a directory that only starts with docs/security",
			change:     add("docs/securityx/a.md", "# A report\n"),
			wantOutput: []string{"docs/securityx/a.md"},
		},
		{
			name:       "the audit App adding docs/security.md, a file and not the directory",
			change:     add("docs/security.md", "# A file beside the directory\n"),
			wantOutput: []string{"docs/security.md"},
		},
		{
			// Renames are not detected, so this is a deletion of internal/x.go and an addition beside
			// docs/security/, and the deletion alone fails the run.
			name: "the audit App renaming internal/x.go under docs/security",
			change: func(t *testing.T, repo string, env []string) {
				t.Helper()
				auditPathsGit(t, env, repo, "mv", "internal/x.go", "docs/security/x.go")
			},
			wantOutput: []string{"internal/x.go"},
		},
		{
			name: "the audit App deleting internal/x.go",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()
				if err := os.Remove(filepath.Join(repo, "internal/x.go")); err != nil {
					t.Fatalf("remove internal/x.go: %v", err)
				}
			},
			wantOutput: []string{"internal/x.go"},
		},
		{
			name: "the audit App making a file outside docs/security executable",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()

				//nolint:gosec // G302: the case pins a mode change, so the executable bit is its point
				if err := os.Chmod(filepath.Join(repo, "README.md"), 0o755); err != nil {
					t.Fatalf("chmod README.md: %v", err)
				}
			},
			wantOutput: []string{"README.md"},
		},
		{
			name: "the audit App adding an executable file under docs/security",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()
				writeFile(t, filepath.Join(repo, "docs/security/run.sh"), []byte("#!/bin/sh\n"), 0o755)
			},
			wantOutput: []string{"docs/security/run.sh"},
		},
		{
			name: "the audit App adding a symlink under docs/security",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()
				if err := os.Symlink("../../README.md", filepath.Join(repo, "docs/security/link.md")); err != nil {
					t.Fatalf("symlink docs/security/link.md: %v", err)
				}
			},
			wantOutput: []string{"docs/security/link.md"},
		},
		{
			// A newline in a name is how a line-based diff hides a path: the second line of this one
			// reads as a report under docs/security/. The name is not pinned - the check may escape it as
			// it likes - but the run may not pass it.
			name:   "the audit App adding a file whose name holds a newline",
			change: add("evil\ndocs/security/a.md", "# A report\n"),
		},
		{
			name:     "the audit App adding a file whose name holds a space",
			change:   add("docs/security/with space.md", "# A report\n"),
			wantPass: true,
		},
		{
			name:        "no author at all",
			authorUnset: true,
		},
		{
			name:        "an empty author",
			authorEmpty: true,
		},
		{
			name:      "no base commit",
			unsetBase: true,
		},
		{
			name:      "no head commit",
			unsetHead: true,
		},
		{
			name:        "a base commit the repository does not hold",
			missingBase: true,
		},
		{
			name:        "a head commit the repository does not hold",
			missingHead: true,
		},
	}
}

// runAuditPathsCase stages one case in a temporary repository, runs the check and checks what it did.
func runAuditPathsCase(t *testing.T, tc auditPathsCase) {
	t.Helper()

	base := t.TempDir()
	repo := filepath.Join(base, "repo")

	dirs := childDirs{
		bin:  filepath.Join(base, "bin"),
		home: filepath.Join(base, "home"),
		tmp:  filepath.Join(base, "tmp"),
	}

	for _, dir := range []string{dirs.bin, dirs.home, dirs.tmp, repo} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	env := auditPathsEnv(t, dirs, nil)

	auditPathsGit(t, env, repo, "init", "--quiet")

	for path, content := range auditPathsBaseFiles {
		auditPathsWrite(t, repo, path, content)
	}

	auditPathsGit(t, env, repo, "add", "--all")
	auditPathsGit(t, env, repo, "commit", "--quiet", "--message", "the base commit")

	baseSHA := auditPathsGit(t, env, repo, "rev-parse", "HEAD")

	if tc.change != nil {
		tc.change(t, repo, env)
	}

	auditPathsGit(t, env, repo, "add", "--all")
	auditPathsGit(t, env, repo, "commit", "--quiet", "--allow-empty", "--message", "the head commit")

	headSHA := auditPathsGit(t, env, repo, "rev-parse", "HEAD")

	variables := map[string]string{}

	switch {
	case tc.authorUnset:
	case tc.authorEmpty:
		variables[auditPathsAuthorEnv] = ""
	case tc.author != "":
		variables[auditPathsAuthorEnv] = tc.author
	default:
		variables[auditPathsAuthorEnv] = auditPathsAuthor
	}

	// setSHA hands the run one commit, or leaves the variable out, or names a SHA no repository holds.
	setSHA := func(name, sha string, unset, missing bool) {
		switch {
		case unset:
		case missing:
			variables[name] = auditPathsMissingSHA
		default:
			variables[name] = sha
		}
	}

	setSHA(auditPathsBaseEnv, baseSHA, tc.unsetBase, tc.missingBase)
	setSHA(auditPathsHeadEnv, headSHA, tc.unsetHead, tc.missingHead)

	output, exit := auditPathsRun(t, auditPathsEnv(t, dirs, variables), repo)

	if gotPass := exit == 0; gotPass != tc.wantPass {
		if tc.wantPass {
			t.Errorf("exit status = %d, want 0: the check fences %s alone\noutput:\n%s", exit, auditPathsAuthor, output)
		} else {
			t.Errorf("exit status = 0, want non-zero: the run must fail, and fail closed when it cannot tell\noutput:\n%s", output)
		}
	}

	for _, want := range tc.wantOutput {
		if !strings.Contains(output, want) {
			t.Errorf("output does not name %q, a path the run changes:\n%s", want, output)
		}
	}
}

// auditPathsWrite writes content to path under repo, creating the directories it needs.
func auditPathsWrite(t *testing.T, repo, path, content string) {
	t.Helper()

	writeFile(t, filepath.Join(repo, filepath.FromSlash(path)), []byte(content), 0o644)
}

// auditPathsEnv returns the environment a case's git commands and the check run in: the harness's childEnv
// over auditPathsGitEnv, plus the case's variables, which win on a repeated name.
func auditPathsEnv(t *testing.T, dirs childDirs, variables map[string]string) []string {
	t.Helper()

	extra := maps.Clone(auditPathsGitEnv)
	for name, value := range variables {
		extra[name] = value
	}

	return childEnv(t, os.Environ(), dirs, extra)
}

// auditPathsGit runs git -C repo with args in env and returns its standard output without the line end,
// failing the case when git exits non-zero.
func auditPathsGit(t *testing.T, env []string, repo string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), repo, err, out)
	}

	return strings.TrimSpace(string(out))
}

// auditPathsRun runs the check in repo with env, and returns its standard output and standard error
// together and its exit status.
func auditPathsRun(t *testing.T, env []string, repo string) (string, int) {
	t.Helper()

	cmd := exec.Command("bash", auditPathsScriptPath(t))
	cmd.Dir = repo
	cmd.Env = env

	var out bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run %s: %v", auditPathsScript, err)
	}

	return out.String(), exit.ExitCode()
}

// auditPathsScriptPath returns the check's absolute path, and fails the case when it does not exist.
func auditPathsScriptPath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(moduleRoot, auditPathsScript))
	if err != nil {
		t.Fatalf("resolve %s: %v", auditPathsScript, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s does not exist: %v; PRD #175 milestone 2 adds it as the required path check", auditPathsScript, err)
	}

	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", auditPathsScript)
	}

	return path
}
