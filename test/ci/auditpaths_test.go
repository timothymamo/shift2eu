// Pins the fence of PRD #175 milestone 2 around the audit App's report pull requests:
// .github/scripts/audit-paths.sh.
//
// The check's contract, run with AUDIT_PATHS_AUTHOR, AUDIT_PATHS_SENDER, AUDIT_PATHS_BASE,
// AUDIT_PATHS_HEAD, AUDIT_PATHS_BASE_REF and AUDIT_PATHS_HEAD_REF in the environment and the current
// directory inside a git repository that holds both commits:
//
//   - It compares the tree of the merge base of BASE and HEAD with the tree of HEAD, renames not
//     detected: a rename counts as the deletion of the old path and the addition of the new one. A branch
//     main has moved past is therefore judged on what it lands, not on what main did meanwhile.
//   - The fence applies to an audit pull request: one whose author is exactly shift2eu-audit[bot], one
//     whose sender is exactly shift2eu-audit[bot] - the App's token holds contents write, so it can push a
//     commit onto another author's pull request - and one whose head branch starts with security-audit/,
//     the branches the App pushes its reports from.
//   - An audit pull request passes, exit 0, when its base branch is exactly main and every changed path is
//     under docs/security/ with no path component starting with a dot, and every added or modified entry
//     there is a regular, non-executable file, git mode 100644: the App opens docs-only pull requests that
//     add docs/security/audits/YYYY-MM-DD-<run_id>.md. A deletion under docs/security/ passes. A path
//     component starting with a dot fails even though the entry is a regular, unexecutable file: a
//     .gitattributes under docs/security/ could set export-ignore or export-subst on the reports that
//     ship in the release archives.
//   - An audit pull request into any other base branch fails, whatever it changes and wherever it changes
//     it: the App could otherwise open a docs-only pull request into a branch that holds code, take the
//     green check and retarget the base to main.
//   - Every other pull request passes, whatever the change: the check exists for that one App's report
//     pull requests and costs nothing elsewhere.
//   - It fails, non-zero, otherwise, and its output names each offending path. A path holding a newline or
//     another control character may be escaped however the check likes; what it may not do is pass, and no
//     line of the run's output starts with ::, a workflow command, whatever a path holds.
//   - It fails closed, non-zero, when the author, the sender or the head branch is unset or empty - for any
//     pull request, since the run cannot then tell whether the fence applies - when the base branch is
//     unset or empty for an audit pull request, and when BASE or HEAD is unset, empty, not a full commit
//     SHA (HEAD, a branch or a short SHA is not one, whatever it resolves to) or not a commit of the
//     repository: a run that cannot tell who opened the pull request, or what it changes, is no pass.
//
// Each case builds its own repository below its t.TempDir(): git init on branch main, a base commit
// holding README.md, internal/x.go and docs/security/README.md, then a head commit with the case's change.
// The branch main is the fixture's own, so a case can hand the check the branch name main as a commit. A
// case that sets none of the branch names gets main as the base branch, the App's report branch as the
// head branch when the App authors the pull request and another branch when it does not, and the author
// as the sender. git runs in an
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

	// The six variables the workflow hands the check; it reads no other.
	auditPathsAuthorEnv  = "AUDIT_PATHS_AUTHOR"
	auditPathsSenderEnv  = "AUDIT_PATHS_SENDER"
	auditPathsBaseEnv    = "AUDIT_PATHS_BASE"
	auditPathsHeadEnv    = "AUDIT_PATHS_HEAD"
	auditPathsBaseRefEnv = "AUDIT_PATHS_BASE_REF"
	auditPathsHeadRefEnv = "AUDIT_PATHS_HEAD_REF"

	// auditPathsMainBranch is the fixture's branch, and the base branch a report pull request lands on.
	// auditPathsReportBranch is a head branch the App pushes its reports from, and auditPathsFeatureBranch
	// one it does not: a pull request from that one is no audit pull request unless its author or its
	// sender says so.
	auditPathsMainBranch    = "main"
	auditPathsReportBranch  = "security-audit/2026-11-22-1"
	auditPathsFeatureBranch = "feat/a-change"

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
	// sender is AUDIT_PATHS_SENDER, the login of the account that pushed the head commit: on an opened pull
	// request it is the author, and the App's contents write lets it push onto another author's pull
	// request. Empty means the author in force, and the App when the case leaves the author out, so that
	// such a case pins the author alone.
	sender string
	// senderEmpty sets AUDIT_PATHS_SENDER to the empty string, and senderUnset leaves it out of the
	// environment: both must fail closed, whatever the author.
	senderEmpty bool
	senderUnset bool
	// baseBranch and headBranch are AUDIT_PATHS_BASE_REF and AUDIT_PATHS_HEAD_REF, the branch the pull
	// request is based on and the branch its commits are on. Empty means main, and the App's report branch
	// for a pull request the App authors, another branch otherwise.
	baseBranch string
	headBranch string
	// baseBranchEmpty and headBranchEmpty set AUDIT_PATHS_BASE_REF or AUDIT_PATHS_HEAD_REF to the empty
	// string, and baseBranchUnset and headBranchUnset leave it out of the environment: each must fail
	// closed. A base branch is pinned for an audit pull request, whose fence it decides: a pull request the
	// fence does not apply to passes whatever its base branch.
	baseBranchEmpty bool
	headBranchEmpty bool
	baseBranchUnset bool
	headBranchUnset bool
	// unsetBase and unsetHead leave AUDIT_PATHS_BASE or AUDIT_PATHS_HEAD out of the environment, and
	// missingBase and missingHead set one to a SHA the repository does not hold: each must fail closed.
	unsetBase   bool
	unsetHead   bool
	missingBase bool
	missingHead bool
	// baseAbbreviated and headAbbreviated hand AUDIT_PATHS_BASE or AUDIT_PATHS_HEAD the first twelve
	// characters of the commit, the short SHA git resolves: the check must fail closed rather than diff
	// what it names.
	baseAbbreviated bool
	headAbbreviated bool
	// baseLiteral and headLiteral hand AUDIT_PATHS_BASE or AUDIT_PATHS_HEAD a value that is not a full
	// commit SHA, such as the ref name HEAD or the fixture's branch name: whatever it resolves to, the
	// check must fail closed rather than diff it.
	baseLiteral string
	headLiteral string
	// baseRef names a ref the case's change creates, whose commit is handed to the run as AUDIT_PATHS_BASE
	// instead of the base commit: an audit pull request whose branch main has moved past since, so BASE is
	// no ancestor of HEAD and the two commits' merge base is what the check judges from.
	baseRef string
	// changeCommits says the change commits the head commit itself, for the cases git add cannot stage: a
	// gitlink names a commit of another repository, and no tree in the working directory holds it.
	changeCommits bool
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
			// The App's token holds contents write, so it can push a commit onto another author's pull
			// request: the sender decides that the fence applies as much as the author does.
			name:       "octocat's pull request whose sender is the audit App changing the README",
			author:     "octocat",
			sender:     auditPathsAuthor,
			change:     add("README.md", "# A changed README.\n"),
			wantOutput: []string{"README.md"},
		},
		{
			// The App pushes its reports from security-audit/** branches: a pull request from such a branch
			// is an audit pull request even when another account opened it.
			name:       "octocat's pull request from a branch under security-audit changing the README",
			author:     "octocat",
			headBranch: auditPathsReportBranch,
			change:     add("README.md", "# A changed README.\n"),
			wantOutput: []string{"README.md"},
		},
		{
			// The fence applies and the change keeps to it: the App's own report pull requests look like
			// this one, whether the App opened it or only pushed to it.
			name:       "octocat's pull request from a branch under security-audit adding a report",
			author:     "octocat",
			headBranch: auditPathsReportBranch,
			change:     add(report, "# The report\n"),
			wantPass:   true,
		},
		{
			name:     "the audit App adding a report",
			change:   add(report, "# The report\n"),
			wantPass: true,
		},
		{
			// The App could open a docs-only pull request into a security-audit branch that holds code, take
			// the green check and retarget the base to main: an audit pull request into any other base branch
			// is no pass, however little it changes.
			name:       "the audit App adding a report to a pull request into another branch",
			baseBranch: "security-audit/x",
			change:     add(report, "# The report\n"),
		},
		{
			name:     "the audit App modifying docs/security/README.md",
			change:   add("docs/security/README.md", "# Security\n\nA link to the new report.\n"),
			wantPass: true,
		},
		{
			// The branch is cut from the base commit and pushed to after main moved on: main's own change to
			// README.md is no part of what the branch lands, so it is no part of what the check judges.
			name: "the audit App adding a report on a branch main has moved past",
			change: func(t *testing.T, repo string, env []string) {
				t.Helper()
				auditPathsGit(t, env, repo, "checkout", "--quiet", "-b", "main-advanced")
				auditPathsWrite(t, repo, "README.md", "# A changed README.\n")
				auditPathsGit(t, env, repo, "commit", "--quiet", "--all", "--message", "main moves on")
				auditPathsGit(t, env, repo, "checkout", "--quiet", "-")
				auditPathsWrite(t, repo, report, "# The report\n")
			},
			baseRef:  "main-advanced",
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
			// A .gitattributes under docs/security/ could set export-ignore or export-subst on the reports
			// that ship in the release archives, which is no change a report pull request makes.
			name:       "the audit App adding docs/security/.gitattributes",
			change:     add("docs/security/.gitattributes", "audits/*.md export-ignore\n"),
			wantOutput: []string{"docs/security/.gitattributes"},
		},
		{
			// Any component of a path, not the first one alone: a dot file inside docs/security/audits/
			// hides a report from the pages that list the directory.
			name:       "the audit App adding a dot file under docs/security/audits",
			change:     add("docs/security/audits/.hidden.md", "# A hidden report\n"),
			wantOutput: []string{"docs/security/audits/.hidden.md"},
		},
		{
			// A deletion needs no mode to check: the App prunes a report main no longer carries.
			name: "the audit App deleting a file under docs/security",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()

				if err := os.Remove(filepath.Join(repo, "docs/security/README.md")); err != nil {
					t.Fatalf("remove docs/security/README.md: %v", err)
				}
			},
			wantPass: true,
		},
		{
			// A gitlink, mode 160000, is a submodule: a commit of another repository that a release archive
			// would pin and a later build could change. git add cannot stage one, since no tree in the
			// working directory holds it, so the case writes the index entry and commits it itself.
			name: "the audit App adding a gitlink under docs/security",
			change: func(t *testing.T, repo string, env []string) {
				t.Helper()

				sha := auditPathsGit(t, env, repo, "rev-parse", "HEAD")
				auditPathsGit(t, env, repo, "update-index", "--add", "--cacheinfo", "160000,"+sha+",docs/security/sub")
				auditPathsGit(t, env, repo, "commit", "--quiet", "--message", "the head commit")
			},
			changeCommits: true,
			wantOutput:    []string{"docs/security/sub"},
		},
		{
			// A mode change is a modification of the entry, not an addition: one chmod of a report is
			// enough to make the pull request no report pull request.
			name: "the audit App making docs/security/README.md executable",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()

				//nolint:gosec // G302: the case pins a mode change, so the executable bit is its point
				if err := os.Chmod(filepath.Join(repo, "docs/security/README.md"), 0o755); err != nil {
					t.Fatalf("chmod docs/security/README.md: %v", err)
				}
			},
			wantOutput: []string{"docs/security/README.md"},
		},
		{
			// git reports a regular file replaced by a symlink at the same path as a type change, T, whose
			// new mode is 120000: the entry is a symlink whatever the old one was.
			name: "the audit App replacing a file under docs/security with a symlink",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()

				link := filepath.Join(repo, "docs/security/README.md")
				if err := os.Remove(link); err != nil {
					t.Fatalf("remove docs/security/README.md: %v", err)
				}

				if err := os.Symlink("../../README.md", link); err != nil {
					t.Fatalf("symlink docs/security/README.md: %v", err)
				}
			},
			wantOutput: []string{"docs/security/README.md"},
		},
		{
			// Renames are not detected either: this is a deletion of docs/security/README.md and an
			// addition of docs/security/README2.md, both inside the fence, and a deletion there passes.
			name: "the audit App renaming a file inside docs/security",
			change: func(t *testing.T, repo string, env []string) {
				t.Helper()

				auditPathsGit(t, env, repo, "mv", "docs/security/README.md", "docs/security/README2.md")
			},
			wantPass: true,
		},
		{
			// Every commit of the pull request counts, not its last one alone: the report the head commit
			// adds no more undoes the change the one before it made.
			name: "the audit App changing a Go file and then adding a report in a second commit",
			change: func(t *testing.T, repo string, env []string) {
				t.Helper()

				auditPathsWrite(t, repo, "internal/x.go", "package internal\n\n// A change the report does not undo.\n")
				auditPathsGit(t, env, repo, "commit", "--quiet", "--all", "--message", "a change outside the fence")
				auditPathsWrite(t, repo, report, "# The report\n")
			},
			wantOutput: []string{"internal/x.go"},
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
			// it likes - but the run may not pass it, and no escaped line may start a workflow command.
			name: "the audit App adding files whose names hold a newline",
			change: func(t *testing.T, repo string, _ []string) {
				t.Helper()

				auditPathsWrite(t, repo, "evil\ndocs/security/a.md", "# A report\n")
				auditPathsWrite(t, repo, "docs/x\n::error::x", "# A file\n")
			},
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
			// The sender decides whether the fence applies: an audit pull request whose sender the run
			// cannot read is no pass, and neither is another author's pull request, which the App may have
			// pushed to.
			name:        "no sender for an audit pull request",
			senderUnset: true,
		},
		{
			name:        "an empty sender for an audit pull request",
			senderEmpty: true,
		},
		{
			name:        "no sender for octocat's pull request",
			author:      "octocat",
			senderUnset: true,
		},
		{
			name:        "an empty sender for octocat's pull request",
			author:      "octocat",
			senderEmpty: true,
		},
		{
			// A head branch under security-audit/ is the App's, so a run that cannot read the head branch
			// cannot tell whether the fence applies either.
			name:            "no head branch for an audit pull request",
			headBranchUnset: true,
		},
		{
			name:            "an empty head branch for an audit pull request",
			headBranchEmpty: true,
		},
		{
			name:            "no head branch for octocat's pull request",
			author:          "octocat",
			headBranchUnset: true,
		},
		{
			name:            "an empty head branch for octocat's pull request",
			author:          "octocat",
			headBranchEmpty: true,
		},
		{
			// An audit pull request lands on main alone, so its base branch is part of the fence. A pull
			// request the fence does not apply to passes whatever its base branch, and no case pins that.
			name:            "no base branch for an audit pull request",
			baseBranchUnset: true,
		},
		{
			name:            "an empty base branch for an audit pull request",
			baseBranchEmpty: true,
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
		{
			// HEAD resolves to a commit of the repository, and so does a branch name or a short SHA: none of
			// them is the full commit SHA the workflow hands the check, and a value git reads as an option is
			// no commit at all.
			name:        "a base commit given as the ref name HEAD",
			baseLiteral: "HEAD",
		},
		{
			name:        "a head commit given as the ref name HEAD",
			headLiteral: "HEAD",
		},
		{
			// A short SHA resolves to the commit as surely as the full one does, and it is still no full
			// commit SHA: what git resolves it to may be another commit tomorrow.
			name:            "a base commit given as an abbreviated SHA",
			baseAbbreviated: true,
		},
		{
			name:            "a head commit given as an abbreviated SHA",
			headAbbreviated: true,
		},
		{
			// main is the fixture's branch, so this value names a commit of the repository: a branch is no
			// full commit SHA all the same, and it moves.
			name:        "a base commit given as the branch name main",
			baseLiteral: "main",
		},
		{
			name:        "a head commit given as the branch name main",
			headLiteral: "main",
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

	auditPathsGit(t, env, repo, "init", "--quiet", "--initial-branch="+auditPathsMainBranch)

	for path, content := range auditPathsBaseFiles {
		auditPathsWrite(t, repo, path, content)
	}

	auditPathsGit(t, env, repo, "add", "--all")
	auditPathsGit(t, env, repo, "commit", "--quiet", "--message", "the base commit")

	baseSHA := auditPathsGit(t, env, repo, "rev-parse", "HEAD")

	if tc.change != nil {
		tc.change(t, repo, env)
	}

	// A case whose change commits the head commit itself, such as the gitlink's, leaves the commit to its
	// own change: git add would stage no tree for it.
	if !tc.changeCommits {
		auditPathsGit(t, env, repo, "add", "--all")
		auditPathsGit(t, env, repo, "commit", "--quiet", "--allow-empty", "--message", "the head commit")
	}

	headSHA := auditPathsGit(t, env, repo, "rev-parse", "HEAD")

	// A case whose main moved on hands that commit to the run instead of the base commit.
	if tc.baseRef != "" {
		baseSHA = auditPathsGit(t, env, repo, "rev-parse", tc.baseRef)
	}

	// The author in force: the case's own, or the App, which most cases audit.
	author := auditPathsAuthor
	if tc.author != "" {
		author = tc.author
	}

	// The sender is the account that pushed the head commit, which on an opened pull request is its
	// author. A case that leaves the author out keeps the App as the sender, so it pins that one variable.
	sender := tc.sender
	if sender == "" {
		sender = author
	}

	// The App pushes its reports from a security-audit/** branch; a pull request from any other branch is
	// no audit pull request unless its author or its sender says so.
	headBranch := tc.headBranch
	if headBranch == "" {
		headBranch = auditPathsFeatureBranch
		if author == auditPathsAuthor {
			headBranch = auditPathsReportBranch
		}
	}

	baseBranch := tc.baseBranch
	if baseBranch == "" {
		baseBranch = auditPathsMainBranch
	}

	variables := map[string]string{}

	// set hands the run one variable: its value, the empty string, or nothing at all.
	set := func(name, value string, empty, unset bool) {
		switch {
		case unset:
		case empty:
			variables[name] = ""
		default:
			variables[name] = value
		}
	}

	set(auditPathsAuthorEnv, author, tc.authorEmpty, tc.authorUnset)
	set(auditPathsSenderEnv, sender, tc.senderEmpty, tc.senderUnset)
	set(auditPathsBaseRefEnv, baseBranch, tc.baseBranchEmpty, tc.baseBranchUnset)
	set(auditPathsHeadRefEnv, headBranch, tc.headBranchEmpty, tc.headBranchUnset)

	// setSHA hands the run one commit, or a short SHA or a value that is no full SHA, or leaves the
	// variable out, or names a SHA no repository holds.
	setSHA := func(name, sha, literal string, abbreviated, unset, missing bool) {
		switch {
		case literal != "":
			variables[name] = literal
		case abbreviated:
			variables[name] = sha[:12]
		case unset:
		case missing:
			variables[name] = auditPathsMissingSHA
		default:
			variables[name] = sha
		}
	}

	setSHA(auditPathsBaseEnv, baseSHA, tc.baseLiteral, tc.baseAbbreviated, tc.unsetBase, tc.missingBase)
	setSHA(auditPathsHeadEnv, headSHA, tc.headLiteral, tc.headAbbreviated, tc.unsetHead, tc.missingHead)

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

	// A line of a script's output that starts with :: is a workflow command, and a path comes from the
	// pull request's tree: no case's run may let one start a line, whatever the case's names hold.
	for i, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "::") {
			t.Errorf("output line %d is %q, which starts with ::, a workflow command: every line the check prints starts with the check's own text", i+1, line)
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
