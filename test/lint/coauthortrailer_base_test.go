// Pins issue #672: the co-author trailer check reads the allow list,
// .github/co-author-addresses.txt, from the base commit of the range it checks,
// never from the working tree.
//
// The check runs on a pull request in the pull request's own checkout, so a list
// read from the working tree is a list the pull request itself could widen: a
// branch that adds an address to the list and commits with that address would
// pass its own check, leaving the maintainer's review as the only gate. The base
// commit's list is the base branch's list, main's for a pull request to main, so
// a change to the list counts only after it lands (issue #672, follow-up to #663
// and PR #670).
//
// Each case builds its own repository below its t.TempDir(): git init on branch
// main, a base commit that holds the list, then head commits that widen the list
// and commit with the address they added, so the working tree holds the widened
// list a check must not read. git runs in an environment this harness builds for
// the case alone - a HOME below the case's temporary directory, the system
// configuration off and the global one empty, and an identity and date of the
// harness's own - so neither the developer's git configuration nor a hook it
// points at can change a commit. The test needs git, and no network.
//
// The same fixture pins the other half of the reading: every git call of the
// check runs with --no-replace-objects, so a replace ref (git replace), which a
// checkout can hold uncommitted and unshared, can change neither the commit the
// base resolves to, nor the list the base holds, nor the commits of the range.
//
// It pins the configuration the same three git calls run in too (issue #689):
// they run with GIT_CONFIG_GLOBAL=/dev/null and GIT_CONFIG_NOSYSTEM=1, so a
// developer's global git configuration and the host's system one can change
// neither the commit the base resolves to, nor the list the base holds, nor the
// commits and authors the range reads.
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
			"a pull request that changes only the list is checked against the base's list (issue #672)", spec, diff)
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

// TestCoAuthorRangeFindingsIgnoreReplaceRefs pins the --no-replace-objects of
// all three git calls coAuthorRangeFindings makes: rev-parse resolves the base,
// show reads the base's allow list and log reads the commits of the range, and
// each runs with it (issue #672). A replace ref would otherwise swap the commit
// the base names, the tree the base holds or the commits the range walks, so a
// pull request could widen the base's list, or hide the very commit a finding
// names, with one git replace in its own checkout and no change to the review
// it shows.
//
// Each case points a replace ref, with a git command of the fixture's own that
// passes no --no-replace-objects, at an object the range reads, and at a commit
// whose allow list also names coAuthorAddedAddress, whose author is
// coAuthorBaseAddress and whose message holds no trailer: anything read through
// the ref allows the added address and hides what the range really finds. The
// base commit, the annotated tag that names it and the range's head each keep
// their own reading.
func TestCoAuthorRangeFindingsIgnoreReplaceRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// replace points the fixture's replace ref at replacement and returns the
		// base side of the range and the object whose replacement a git command
		// without --no-replace-objects reads.
		replace func(t *testing.T, env []string, repo, base, head, replacement string) (baseSide, replaced string)
	}{
		{
			name: "the base commit is replaced",
			replace: func(t *testing.T, env []string, repo, base, _, replacement string) (string, string) {
				t.Helper()

				coAuthorRepoReplace(t, env, repo, base, replacement)
				return base, base
			},
		},
		{
			name: "the tag that names the base is replaced",
			replace: func(t *testing.T, env []string, repo, base, _, replacement string) (string, string) {
				t.Helper()

				const tag, replacementTag = "tag-of-base", "replacement-tag"

				// git replace holds the types equal, so the tag the range's base
				// resolves to is replaced by a tag of the replacement commit.
				coAuthorRepoGit(t, env, repo, "tag", "--annotate", "--message", "the base of the range", tag, base)
				tagged := coAuthorRepoGit(t, env, repo, "rev-parse", tag)

				coAuthorRepoGit(t, env, repo, "tag", "--annotate", "--message", "the replacement of the base",
					replacementTag, replacement)
				coAuthorRepoReplace(t, env, repo, tagged, coAuthorRepoGit(t, env, repo, "rev-parse", replacementTag))

				return tag, tag
			},
		},
		{
			name: "the range's head is replaced",
			replace: func(t *testing.T, env []string, repo, base, head, replacement string) (string, string) {
				t.Helper()

				coAuthorRepoReplace(t, env, repo, head, replacement)
				return base, head
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo, env := newCoAuthorRepo(t)

			base := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress, "docs: the base commit", map[string]string{
				"README.md":           "the fixture\n",
				coAuthorAllowListPath: coAuthorBaseAddress + "\n",
			})

			// The head widens the list and commits with the address it added, as
			// the case above does: the base's list refuses both, and a replace ref
			// that widened that list or hid those commits would refuse neither.
			widens := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress,
				"feat: widen the allow list\n\nCo-Authored-By: B <"+coAuthorAddedAddress+">\n",
				map[string]string{coAuthorAllowListPath: coAuthorBaseAddress + "\n" + coAuthorAddedAddress + "\n"})

			byAdded := coAuthorRepoCommit(t, env, repo, coAuthorAddedAddress,
				"docs: a commit the base's list refuses", map[string]string{"README.md": "the fixture, again\n"})

			replacement := coAuthorRepoReplacement(t, env, repo)

			baseSide, replaced := tc.replace(t, env, repo, base, byAdded, replacement)

			// The premise: a git command of the fixture's, which passes no
			// --no-replace-objects, reads the replacement's widened list through
			// the ref, so a check that did the same would allow the added address
			// and this case would pin nothing.
			through := coAuthorRepoGit(t, env, repo, "show", replaced+":"+coAuthorAllowListPath)

			listed, err := parseCoAuthorAllowList([]byte(through))
			if err != nil {
				t.Fatalf("parse the fixture's list as git show %s reads it: %v", replaced, err)
			}

			if !slices.Contains(listed, coAuthorAddedAddress) {
				t.Fatalf("git show %s:%s lists %v, want %s among them: the fixture holds no replace ref the check "+
					"could read", replaced, coAuthorAllowListPath, listed, coAuthorAddedAddress)
			}

			spec := baseSide + ".." + byAdded

			commits, findings, err := coAuthorRangeFindings(repo, spec)
			if err != nil {
				t.Fatalf("coAuthorRangeFindings(%s): %v", spec, err)
			}

			if len(commits) != 2 {
				t.Fatalf("coAuthorRangeFindings(%s) read %d commits, want the range's 2", spec, len(commits))
			}

			// The base's own list, read from the base's own commit, names
			// coAuthorBaseAddress alone, so the findings are the added address as
			// an author and as a trailer, in git log's order, newest first.
			want := []coAuthorFinding{
				{SHA: byAdded, Trailer: "author " + coAuthorAddedAddress},
				{SHA: widens, Trailer: "Co-Authored-By: B <" + coAuthorAddedAddress + ">"},
			}

			diff := cmp.Diff(want, findings, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(coAuthorFinding{}, "Reason"))
			if diff != "" {
				t.Errorf("coAuthorRangeFindings(%s) mismatch (-want +got):\n%s\n"+
					"Want the replace ref ignored: rev-parse, show and log run with --no-replace-objects, so a replace "+
					"ref cannot change the base's list or the commits of the range (issue #672)", spec, diff)
			}
		})
	}
}

// TestCoAuthorRangeFindingsIsolateTheGitConfig pins the configuration the three
// git calls coAuthorRangeFindings makes run in (issue #689, follow-up to #672):
// rev-parse resolves the base, show reads the base's allow list and log reads
// the commits of the range, and each runs with GIT_CONFIG_GLOBAL=/dev/null and
// GIT_CONFIG_NOSYSTEM=1, so neither the developer's global git configuration
// nor the host's system one decides what the check reads. A developer whose
// global configuration reads commit messages in another encoding would
// otherwise change what a pull request's check finds, on their machine alone,
// while the same run on CI finds something else.
//
// The case plants a global configuration that would change the reading if git
// read it: i18n.commitEncoding = ISO-8859-1, the encoding a git log reads a
// commit message in when the commit declares none, so the message the check
// reads holds different bytes than the fixture committed, and the trailer it
// reports is a different trailer. The planted configuration sits in a HOME of
// the case's own, which is how a global configuration reaches the check's git
// calls today, and the case hands the same file to GIT_CONFIG_GLOBAL and
// GIT_CONFIG_SYSTEM as well, so an environment that forwards either to the git
// calls reads it too.
//
// GIT_CONFIG_GLOBAL is what the case observes: a global configuration arrives in
// a git process through its HOME, and this harness passes HOME on. The system
// configuration GIT_CONFIG_NOSYSTEM=1 switches off is not observable from here:
// GIT_CONFIG_SYSTEM and GIT_CONFIG_NOSYSTEM never reach a child of this harness
// (module_test.go's passedEnv), and /etc/gitconfig is not a file a test may
// write, so only a change to the check's own child environment would show it.
// TestCoAuthorGitEnvSwitchesOffTheSystemConfig pins that half where it is
// observable: over runGitEnv's extra env.
func TestCoAuthorRangeFindingsIsolateTheGitConfig(t *testing.T) {
	// No t.Parallel: this case sets the process's HOME for the git calls the
	// check makes, and t.Setenv refuses a parallel test.
	repo, env := newCoAuthorRepo(t)

	base := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress, "docs: the base commit", map[string]string{
		"README.md":           "the fixture\n",
		coAuthorAllowListPath: coAuthorBaseAddress + "\n",
	})

	// The head widens the list with the address it commits with, as the cases
	// above do: the base's list refuses the added address as an author and as a
	// trailer, so a reading that changed either leaves a different result. The
	// trailer's name carries a character outside ASCII, which another message
	// encoding reads as another byte.
	widens := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress,
		"feat: widen the allow list\n\nCo-Authored-By: Bøb <"+coAuthorAddedAddress+">\n",
		map[string]string{coAuthorAllowListPath: coAuthorBaseAddress + "\n" + coAuthorAddedAddress + "\n"})

	byAdded := coAuthorRepoCommit(t, env, repo, coAuthorAddedAddress,
		"docs: a commit the base's list refuses", map[string]string{"README.md": "the fixture, again\n"})

	spec := base + ".." + byAdded

	// The reading with nothing planted by the case is the fixture's own: the
	// added address as an author and as a trailer, in git log's order, newest
	// first.
	cleanCommits, cleanFindings, err := coAuthorRangeFindings(repo, spec)
	if err != nil {
		t.Fatalf("coAuthorRangeFindings(%s): %v", spec, err)
	}

	if len(cleanCommits) != 2 {
		t.Fatalf("coAuthorRangeFindings(%s) read %d commits, want the range's 2", spec, len(cleanCommits))
	}

	want := []coAuthorFinding{
		{SHA: byAdded, Trailer: "author " + coAuthorAddedAddress},
		{SHA: widens, Trailer: "Co-Authored-By: Bøb <" + coAuthorAddedAddress + ">"},
	}

	if diff := cmp.Diff(want, cleanFindings, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(coAuthorFinding{}, "Reason")); diff != "" {
		t.Fatalf("coAuthorRangeFindings(%s) with nothing planted (-want +got):\n%s\n"+
			"Want the fixture's own findings, so that a planted configuration that changes them is observable", spec, diff)
	}

	home, _ := coAuthorPlantedGitConfig(t)

	// A developer's global configuration reaches a git process through its HOME,
	// which the check's git calls inherit today, so the case plants it in a HOME of
	// its own. GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are not set here: this
	// harness forwards no GIT_* variable to a child (module_test.go's passedEnv), so
	// a value the case set would be inert, and reading the planted file is what
	// GIT_CONFIG_GLOBAL=/dev/null, which the check's own git calls pass
	// (coAuthorGitEnv), must prevent.
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	// The premise: a git command of the fixture's own, with the identity and the
	// HOME the case planted and no isolation of its own, reads the planted
	// configuration, which makes it read the commit's message in another
	// encoding: the message it prints is not the one the fixture stored. A
	// configuration git ignored would pin nothing.
	committed := coAuthorRepoGit(t, env, repo, "log", "-1", "--format=%B", "--end-of-options", widens)
	plantedLog := coAuthorRepoGit(t, coAuthorRepoGlobalEnv(home), repo, "log", "-1", "--format=%B", "--end-of-options", widens)

	if plantedLog == committed {
		t.Fatalf("git log --format=%%B %s prints the committed message with the planted encoding configuration, want the "+
			"configuration read, so that a reading that ignored it is observable", widens)
	}

	commits, findings, err := coAuthorRangeFindings(repo, spec)
	if err != nil {
		t.Fatalf("coAuthorRangeFindings(%s) with a planted global git configuration: %v", spec, err)
	}

	if len(commits) != 2 {
		t.Fatalf("coAuthorRangeFindings(%s) read %d commits with a planted global git configuration, want the range's 2",
			spec, len(commits))
	}

	diff := cmp.Diff(cleanFindings, findings, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(coAuthorFinding{}, "Reason"))
	if diff != "" {
		t.Errorf("coAuthorRangeFindings(%s) with a planted global git configuration (-without +with):\n%s\n"+
			"Want the three git calls of the check to run with GIT_CONFIG_GLOBAL=/dev/null and GIT_CONFIG_NOSYSTEM=1: "+
			"a developer's global configuration must not decide which commits, and which authors, the check reads "+
			"(issue #689)", spec, diff)
	}
}

// TestCoAuthorGitEnvSwitchesOffTheSystemConfig pins coAuthorGitEnv exactly (issue
// #689, follow-up to #672): the three git calls of the check run with
// GIT_CONFIG_GLOBAL=/dev/null and GIT_CONFIG_NOSYSTEM=1, so neither the
// developer's global git configuration nor the host's system one decides which
// commits, which allow list and which authors the check reads.
//
// The global half is observable end to end
// (TestCoAuthorRangeFindingsIsolateTheGitConfig): a global configuration reaches
// a git process through the HOME this harness passes on. The system half is not:
// passedEnv forwards no GIT_* variable and /etc/gitconfig is no file a test may
// write, so no system configuration reaches the check's own git calls. The case
// takes the one door this harness has for one, runGitEnv's extra env: it plants a
// system configuration and pins that coAuthorGitEnv's GIT_CONFIG_NOSYSTEM=1 wins
// over it, so a check whose git calls read the host's system configuration does
// not pass either. The value itself is pinned beside that, exactly: a variable
// dropped from coAuthorGitEnv is a decision this test asks for.
func TestCoAuthorGitEnvSwitchesOffTheSystemConfig(t *testing.T) {
	t.Parallel()

	if diff := cmp.Diff([]string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}, coAuthorGitEnv); diff != "" {
		t.Errorf("coAuthorGitEnv (-want +got):\n%s\nWant the check's git calls to run with no global and no system git "+
			"configuration (issue #689)", diff)
	}

	// The planted file the case names as the system configuration.
	_, planted := coAuthorPlantedGitConfig(t)

	root := t.TempDir()

	// The premise: named as the system configuration, with nothing switching the
	// system scope off, git reads the planted file, so a reading that did not
	// switch it off reads it too.
	withSystem, err := runGitEnv(t.Context(), root,
		[]string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=" + planted},
		"config", "--get", "i18n.commitEncoding")
	if err != nil {
		t.Fatalf("git config --get i18n.commitEncoding with %s as the system configuration: %v: the fixture plants no "+
			"system configuration a reading could pick up", planted, err)
	}

	if got := strings.TrimSpace(string(withSystem)); got != "ISO-8859-1" {
		t.Fatalf("git config --get i18n.commitEncoding with %s as the system configuration printed %q, want ISO-8859-1: "+
			"the fixture plants no system configuration a reading could pick up", planted, got)
	}

	// The check's own environment, with the same file named as the system
	// configuration: coAuthorGitEnv's GIT_CONFIG_NOSYSTEM=1 must win, so nothing is
	// read.
	out, err := runGitEnv(t.Context(), root, append(slices.Clone(coAuthorGitEnv), "GIT_CONFIG_SYSTEM="+planted),
		"config", "--get", "i18n.commitEncoding")
	if err == nil {
		t.Errorf("git config --get i18n.commitEncoding with %s as the system configuration and coAuthorGitEnv read %q, want "+
			"nothing: GIT_CONFIG_NOSYSTEM=1 switches the system scope off, so the host's system git configuration cannot "+
			"decide what the co-author check reads (issue #689)", planted, strings.TrimSpace(string(out)))
	}
}

// coAuthorPlantedGitConfig plants, below a directory of its own, a global git
// configuration that would change what coAuthorRangeFindings reads if git read
// it: i18n.commitEncoding = ISO-8859-1, the encoding a git log reads a commit
// message in when the commit declares none, so a message whose trailer's name
// carries ø, which the fixture commits as UTF-8, comes back as another byte. It
// returns the HOME the configuration sits in and ~/.gitconfig's path.
func coAuthorPlantedGitConfig(t *testing.T) (home, global string) {
	t.Helper()

	home = t.TempDir()
	config := "[i18n]\n\tcommitEncoding = ISO-8859-1\n"

	// git reads the global configuration from $XDG_CONFIG_HOME/git/config and
	// ~/.gitconfig: plant both, so a case that sets either reads it.
	global = filepath.Join(home, ".gitconfig")

	for _, path := range []string{global, filepath.Join(home, "xdg", "git", "config")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("create the directory of %s: %v", path, err)
		}

		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatalf("write the planted global git configuration %s: %v", path, err)
		}
	}

	return home, global
}

// coAuthorRepoGlobalEnv is the environment coAuthorRepoEnv builds for home, with
// the three variables that isolate the git configuration removed: what a git
// command of a fixture runs in when it is meant to read the global configuration
// the case planted in home, as the check's own git calls do.
func coAuthorRepoGlobalEnv(home string) []string {
	return slices.DeleteFunc(coAuthorRepoEnv(home), func(v string) bool {
		return strings.HasPrefix(v, "GIT_CONFIG_GLOBAL=") ||
			strings.HasPrefix(v, "GIT_CONFIG_SYSTEM=") ||
			strings.HasPrefix(v, "GIT_CONFIG_NOSYSTEM=")
	})
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

// coAuthorRepoReplacement commits, on a branch of its own that descends from no
// commit, a tree whose allow list also names coAuthorAddedAddress, and returns
// the commit's SHA: the object every case of
// TestCoAuthorRangeFindingsIgnoreReplaceRefs points its replace ref at. Its
// author is coAuthorBaseAddress and its message holds no trailer, so a reading
// that went through a replace ref would hide the range's findings rather than
// add some, and its being no ancestor of the range keeps the replace ref from
// changing which commits the range holds.
func coAuthorRepoReplacement(t *testing.T, env []string, repo string) string {
	t.Helper()

	coAuthorRepoGit(t, env, repo, "checkout", "--quiet", "--orphan", "replacement")

	sha := coAuthorRepoCommit(t, env, repo, coAuthorBaseAddress, "docs: a replacement of the base", map[string]string{
		"README.md":           "the replacement\n",
		coAuthorAllowListPath: coAuthorBaseAddress + "\n" + coAuthorAddedAddress + "\n",
	})

	coAuthorRepoGit(t, env, repo, "checkout", "--quiet", "main")

	return sha
}

// coAuthorRepoReplace points a replace ref at replacement for object, with the
// fixture's git and no --no-replace-objects of its own: the ref is live for
// every git command that does not pass the option, as one in a pull request's
// checkout is for the check (issue #672).
func coAuthorRepoReplace(t *testing.T, env []string, repo, object, replacement string) {
	t.Helper()

	coAuthorRepoGit(t, env, repo, "replace", object, replacement)
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
