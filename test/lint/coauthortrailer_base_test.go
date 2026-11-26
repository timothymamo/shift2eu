// Pins issue #672: the co-author trailer check reads the allow list,
// .github/co-author-addresses.txt, from the base commit of the range it checks,
// never from the working tree.
//
// The check runs on a pull request in the pull request's own checkout, so a list
// read from the working tree is a list the pull request itself could widen: a
// branch that adds an address to the list and commits with that address would
// pass its own check, leaving the maintainer's review as the only gate. The base
// commit's list is the list main holds until the change is merged, so a change
// to the list counts only after it lands (issue #672, follow-up to #663 and PR
// #670).
//
// Each case builds its own repository below its t.TempDir(): git init on branch
// main, a base commit that holds the list, then head commits that widen the list
// and commit with the address they added, so the working tree holds the widened
// list a check must not read. git runs in an environment this harness builds for
// the case alone - a HOME below the case's temporary directory, the system
// configuration off and the global one empty, and an identity and date of the
// harness's own - so neither the developer's git configuration nor a hook it
// points at can change a commit. The test needs git, and no network.
package lint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// The fixture's two addresses, both on example.com, which belongs to nobody
// (CLAUDE.md, rule 2): the base commit's list names coAuthorBaseAddress, and the
// head widens the list with coAuthorAddedAddress and commits with it.
const (
	coAuthorBaseAddress  = "a@example.com"
	coAuthorAddedAddress = "b@example.com"
)

// TestCoAuthorRangeFindingsReadTheBaseAllowList pins the check's reading of the
// allow list over a range: the list is the one .github/co-author-addresses.txt
// holds at the range's base commit, read with git show, so the commit that adds
// coAuthorAddedAddress to the list and the one after it, authored by that
// address, are both refused. The working tree, which this fixture leaves at the
// widened list, holds the added address and must not be read: the check used to
// read it (issue #672).
//
// The commits the base's list does allow pass: every committer, and the author
// of the commit that widens the list, are coAuthorBaseAddress.
func TestCoAuthorRangeFindingsReadTheBaseAllowList(t *testing.T) {
	t.Parallel()

	repo, env := newCoAuthorRepo(t)

	base := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress, "docs: the base commit", map[string]string{
		"README.md":           "the fixture\n",
		coAuthorAllowListPath: coAuthorBaseAddress + "\n",
	})

	// The head widens the list with the address it commits with, once in the
	// trailer of the commit that widens it and once as the author of the commit
	// after it.
	widens := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress,
		"feat: widen the allow list\n\nCo-Authored-By: B <"+coAuthorAddedAddress+">\n",
		map[string]string{coAuthorAllowListPath: coAuthorBaseAddress + "\n" + coAuthorAddedAddress + "\n"})

	byAdded := coAuthorRepoCommit(t, env, repo, coAuthorAddedAddress,
		"docs: a commit the base's list refuses", map[string]string{"README.md": "the fixture, again\n"})

	// The premise: the working tree holds the added address, so a check that
	// reads it would allow both commits below and this case would pin nothing.
	worktree, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(coAuthorAllowListPath)))
	if err != nil {
		t.Fatalf("read the fixture's working tree list: %v", err)
	}

	listed, err := parseCoAuthorAllowList(worktree)
	if err != nil {
		t.Fatalf("parse the fixture's working tree list: %v", err)
	}

	if !slices.Contains(listed, coAuthorAddedAddress) {
		t.Fatalf("the fixture's working tree lists %v, want %s among them", listed, coAuthorAddedAddress)
	}

	spec := base + ".." + byAdded

	commits, findings, err := coAuthorRangeFindings(repo, spec)
	if err != nil {
		t.Fatalf("coAuthorRangeFindings(%s): %v", spec, err)
	}

	if len(commits) != 2 {
		t.Fatalf("coAuthorRangeFindings(%s) read %d commits, want the range's 2", spec, len(commits))
	}

	// The base's list names coAuthorBaseAddress alone, so the findings are the
	// added address as an author and as a trailer, in git log's order, newest
	// first: the list change counts only once it is merged.
	want := []coAuthorFinding{
		{SHA: byAdded, Trailer: "author " + coAuthorAddedAddress},
		{SHA: widens, Trailer: "Co-Authored-By: B <" + coAuthorAddedAddress + ">"},
	}

	diff := cmp.Diff(want, findings, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(coAuthorFinding{}, "Reason"))
	if diff != "" {
		t.Errorf("coAuthorRangeFindings(%s) mismatch (-want +got):\n%s\n"+
			"Want .github/co-author-addresses.txt as of the range's base commit read, never the working tree's: "+
			"a pull request cannot widen the list it is checked against (issue #672)", spec, diff)
	}
}

// TestCoAuthorRangeFindingsFailClosedOnTheBaseList pins the other half of that
// demand: a base whose list the run cannot read is an error, never a pass. A
// base commit that holds no .github/co-author-addresses.txt, a base commit whose
// list holds a line that is no address, and a base revision the repository does
// not hold all fail, although the working tree's list and the range's own commit
// would allow the added address: a check that read nothing, or read the working
// tree instead, must never report a pass.
func TestCoAuthorRangeFindingsFailClosedOnTheBaseList(t *testing.T) {
	t.Parallel()

	// absent names no commit of the fixture: a base revision git show and git log
	// cannot read.
	const absent = "1111111111111111111111111111111111111111"

	tests := []struct {
		name      string
		baseFiles map[string]string
		baseRef   string
	}{
		{
			name:      "the base commit holds no allow list",
			baseFiles: map[string]string{"README.md": "the fixture\n"},
		},
		{
			name: "the base's allow list is no list",
			baseFiles: map[string]string{
				"README.md":           "the fixture\n",
				coAuthorAllowListPath: "this is no address\n",
			},
		},
		{
			name:    "the base revision is not in the repository",
			baseRef: absent,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo, env := newCoAuthorRepo(t)

			files := tc.baseFiles
			if files == nil {
				files = map[string]string{
					"README.md":           "the fixture\n",
					coAuthorAllowListPath: coAuthorBaseAddress + "\n",
				}
			}

			base := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress, "docs: the base commit", files)

			// The head widens the list and commits with the address it added, so
			// a check that fell back to the working tree, or to an empty list,
			// would read a list that allows every commit of the range.
			head := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress,
				"feat: widen the allow list\n\nCo-Authored-By: B <"+coAuthorAddedAddress+">\n",
				map[string]string{coAuthorAllowListPath: coAuthorBaseAddress + "\n" + coAuthorAddedAddress + "\n"})

			if tc.baseRef != "" {
				base = tc.baseRef
			}

			spec := base + ".." + head

			commits, findings, err := coAuthorRangeFindings(repo, spec)
			if err == nil {
				t.Fatalf("coAuthorRangeFindings(%s) = %d commits and %v, want an error: a base whose allow list "+
					"the run cannot read is no pass (issue #672)", spec, len(commits), findings)
			}
		})
	}
}

// newCoAuthorRepo creates an empty repository below the case's temporary
// directory, on branch main, and returns it with the environment its git
// commands run in.
func newCoAuthorRepo(t *testing.T) (repo string, env []string) {
	t.Helper()

	base := t.TempDir()
	repo = filepath.Join(base, "repo")

	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("create %s: %v", repo, err)
	}

	env = coAuthorRepoEnv(filepath.Join(base, "home"))

	coAuthorRepoGit(t, env, repo, "init", "--quiet", "--initial-branch=main")

	return repo, env
}

// coAuthorRepoEnv is the environment every git command of a fixture runs in: the
// developer's configuration out of the way (no global file, no system file, a
// HOME of the fixture's own), an identity of the fixture's own and one fixed
// date, so a history is the same on every machine and nothing of the host
// reaches a commit. The committer is coAuthorBaseAddress; a commit's author is
// the one coAuthorRepoCommit passes.
func coAuthorRepoEnv(home string) []string {
	return []string{
		"HOME=" + home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"TZ=UTC",
		"GIT_AUTHOR_NAME=shift2eu test",
		"GIT_AUTHOR_EMAIL=" + coAuthorBaseAddress,
		"GIT_COMMITTER_NAME=shift2eu test",
		"GIT_COMMITTER_EMAIL=" + coAuthorBaseAddress,
		"GIT_AUTHOR_DATE=2024-01-02T03:04:05+0000",
		"GIT_COMMITTER_DATE=2024-01-02T03:04:05+0000",
	}
}

// coAuthorRepoGit runs git with args, an argv and never a shell, in dir and in
// env, and returns its standard output without the trailing newline, or fails
// the test with git's own output.
func coAuthorRepoGit(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = childEnv(env...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v:\n%s", strings.Join(args, " "), dir, err, out)
	}

	return strings.TrimSpace(string(out))
}

// coAuthorRepoCommit writes files below dir, stages everything and commits it
// with message as authorAddr's commit, and returns the commit's full SHA. A path
// in files is slash-separated and relative to dir, as coAuthorAllowListPath is.
func coAuthorRepoCommit(t *testing.T, env []string, dir, authorAddr, message string, files map[string]string) string {
	t.Helper()

	for path, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(path))

		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("create the directory of %s: %v", path, err)
		}

		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	authored := append(slices.Clone(env), "GIT_AUTHOR_EMAIL="+authorAddr)

	coAuthorRepoGit(t, authored, dir, "add", "--all")
	coAuthorRepoGit(t, authored, dir, "commit", "--quiet", "--message", message)

	return coAuthorRepoGit(t, env, dir, "rev-parse", "HEAD")
}
