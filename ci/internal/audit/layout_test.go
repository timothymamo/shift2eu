// Pins the audit directory's layout, the third step of an audit run (ADR
// 0040's milestone-2 amendment, issue #277, "The audit directory"): the
// directory exists where the model-free step made it, with the checkout copy
// and the checks' results holding no write bit, an empty work area with its
// tmp/, an empty state home/ and tmp/, the state area's bin/ and out/, and the
// wrapper at bin/audit-exec with mode 0555.
//
// Every path the layout puts something of its own at is checked as an entry: a
// symbolic link there - the state area's home/ in particular, which holds the
// CLI's configuration and transcripts - is a layout the model could read from
// elsewhere, so it is refused rather than followed. So is an entry of a type a
// checkout copy and the checks' results never hold, a FIFO or a socket among
// them: those two cases are in layout_unix_test.go, behind the unix build tag,
// because only a Unix host can lay them down. The library takes a root standing
// in for /, so a test lays the tree out under a temporary root.
//
// The checkout copy's git configuration is an allow list (issue #277, round D
// finding 1): the keys and sections a clone carries pass, whatever case they are
// written in: section and key name are compared without regard to case, the
// subsection exactly. Every other key, every key a line the parser cannot place
// yields, and the three files git reads a configuration or a repository out of
// beside .git/config are refused.
package audit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/ci/internal/audit"
)

// auditPath is the audit directory's path under a root that stands in for /.
const auditPath = "srv/audit"

// writeBitsOfEveryone are the permission bits that let anyone write a file.
const writeBitsOfEveryone os.FileMode = 0o222

// gitConfig is the checkout copy's own git configuration, the one local config
// every git command the wrapper runs reads (issue #277, review R14).
var gitConfig = []string{"checkout", ".git", "config"}

// gitDir is the checkout copy's own git directory, which holds that
// configuration and the other files git reads a repository out of.
var gitDir = []string{"checkout", ".git"}

// benignGitConfig is the configuration a clone leaves: nothing the layout
// refuses.
const benignGitConfig = `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
`

// checkoutGitConfig is the configuration actions/checkout writes for the clone
// it makes with persist-credentials: false (issue #277, round D finding 1):
// the keys of a fresh clone, and no credential, no program and no include.
const checkoutGitConfig = `[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
[remote "origin"]
	url = https://github.com/example/repo
	fetch = +refs/heads/*:refs/remotes/origin/*
[gc]
	auto = 0
[branch "main"]
	remote = origin
	merge = refs/heads/main
`

// mixedCaseGitConfig is that configuration in another case, which the allow list
// reads as the same one: the section and the key name are compared without
// regard to case, while a subsection such as origin is compared exactly.
const mixedCaseGitConfig = `[CORE]
	RepositoryFormatVersion = 0
	Filemode = true
	Bare = false
	LogAllRefUpdates = true
[Remote "origin"]
	Url = https://github.com/example/repo
	Fetch = +refs/heads/*:refs/remotes/origin/*
[GC]
	Auto = 0
[BRANCH "main"]
	Remote = origin
	Merge = refs/heads/main
`

// commentedGitConfig is the configuration with the two forms of comment git
// reads and the blank lines a config may hold between its sections.
const commentedGitConfig = `# the checkout's own configuration
; the same comment in the other form

[core]
	repositoryformatversion = 0

	filemode = true
`

// partialCloneGitConfig is the configuration of a partial clone: the two keys
// git writes beside url and fetch, and the object format extensions names.
const partialCloneGitConfig = `[core]
	repositoryformatversion = 1
	filemode = true
	bare = false
	logallrefupdates = true
[extensions]
	objectformat = sha1
[remote "origin"]
	url = https://github.com/example/repo
	fetch = +refs/heads/*:refs/remotes/origin/*
	promisor = true
	partialclonefilter = blob:none
`

// The audit directory's paths under a root, each a part of the layout.
var (
	checkout  = []string{"checkout"}
	checks    = []string{"checks"}
	work      = []string{"work"}
	workTmp   = []string{"work", "tmp"}
	state     = []string{"state"}
	home      = []string{"state", "home"}
	stateTmp  = []string{"state", "tmp"}
	stateBin  = []string{"state", "bin"}
	stateOut  = []string{"state", "out"}
	wrapper   = []string{"bin", "audit-exec"}
	claudeBin = []string{"state", "bin", "claude"}
	binDir    = []string{"bin"}
)

// TestCheckLayoutTakesTheADRsDirectory pins the layout that passes. Nothing is
// created by the check itself: the model-free step lays the directory out, so a
// check that made a missing directory would hide the step that did not run.
func TestCheckLayoutTakesTheADRsDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAuditTree(t, root)

	if err := audit.CheckLayout(root); err != nil {
		t.Fatalf("CheckLayout(%q) error = %v, want none: the tree is the one ADR 0040 lays out", root, err)
	}
}

// TestCheckLayoutTakesTheCheckoutsGitConfig pins the other half of the git
// config check (issue #277, round D finding 1): the configuration a checkout
// carries is an allow list of keys and sections, and the configuration
// actions/checkout writes with persist-credentials: false passes whole, in
// whatever case it is written, with the comments and blank lines a config may
// hold, and with no config at all.
func TestCheckLayoutTakesTheCheckoutsGitConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// mutate lays the configuration the case is about in the checkout copy.
		mutate func(t *testing.T, root string)
	}{
		{name: "the config actions/checkout writes", mutate: gitConfigWith(checkoutGitConfig)},
		{name: "the same config in another case", mutate: gitConfigWith(mixedCaseGitConfig)},
		{name: "the config with comments and blank lines", mutate: gitConfigWith(commentedGitConfig)},
		{name: "a partial clone's remote", mutate: gitConfigWith(partialCloneGitConfig)},
		{
			name: "no config at all",
			mutate: mutateTree(checkout, func(t *testing.T, root string) {
				t.Helper()
				removeAll(t, pathOf(root, gitConfig))
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeAuditTree(t, root)
			tc.mutate(t, root)

			if err := audit.CheckLayout(root); err != nil {
				t.Errorf("CheckLayout(%q) = %v, want none: %s is a configuration a checkout carries", root, err, tc.name)
			}
		})
	}
}

// layoutCase is one way the tree can differ from the ADR's: the case's name and
// the mutation that breaks the part of the tree it is about. It is a named type
// so that the cases only a Unix host can lay down, kept in layout_unix_test.go
// behind the unix build tag, can be appended to the table.
type layoutCase struct {
	name string
	// mutate breaks the tree the case is about.
	mutate func(t *testing.T, root string)
}

// TestCheckLayoutRefusesAnythingElse pins every way the tree can differ from
// the ADR's: a directory or the wrapper missing, a write bit on the checkout
// copy or the checks' results, an empty area holding something, a wrapper of
// another mode, and a symbolic link wherever the layout puts something of its
// own.
func TestCheckLayoutRefusesAnythingElse(t *testing.T) {
	t.Parallel()

	cases := []layoutCase{
		{name: "the checkout copy is missing", mutate: gone(checkout)},
		{name: "the checks' results are missing", mutate: gone(checks)},
		{name: "the work area is missing", mutate: gone(work)},
		{name: "the work area's tmp is missing", mutate: gone(workTmp)},
		{name: "the state area is missing", mutate: gone(state)},
		{name: "the state area's home is missing", mutate: gone(home)},
		{name: "the state area's tmp is missing", mutate: gone(stateTmp)},
		{name: "the state area's bin is missing", mutate: gone(stateBin)},
		{name: "the state area's out is missing", mutate: gone(stateOut)},
		{name: "the wrapper is missing", mutate: gone(wrapper)},

		// 0700 keeps the write bit the ADR's layout must not have, without
		// handing it to group or other.
		{name: "the checkout copy keeps a write bit", mutate: chmodPath(0o700, checkout)},
		{name: "the checks' results keep a write bit", mutate: chmodPath(0o700, checks)},
		{name: "the wrapper is writable", mutate: chmodPath(0o700, wrapper)},
		{name: "the wrapper is read-only", mutate: chmodPath(0o500, wrapper)},

		{
			name: "the work area holds an entry of its own",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, work), "result.json"), "{}", 0o600)
			},
		},
		{
			name: "the state area's home is not empty",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, home), ".claude.json"), "{}", 0o600)
			},
		},
		{
			name: "the state area's tmp is not empty",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(pathOf(root, stateTmp), "leftover"), "x", 0o600)
			},
		},

		{name: "the wrapper is a symbolic link", mutate: linkElsewhere(wrapper)},
		{name: "the checkout copy is a symbolic link", mutate: linkElsewhere(checkout)},
		{name: "the checks' results are a symbolic link", mutate: linkElsewhere(checks)},
		{name: "the work area is a symbolic link", mutate: linkElsewhere(work)},
		{name: "the work area's tmp is a symbolic link", mutate: linkElsewhere(workTmp)},
		{name: "the state area is a symbolic link", mutate: linkElsewhere(state)},
		{name: "the state area's home is a symbolic link", mutate: linkElsewhere(home)},
		{name: "the state area's tmp is a symbolic link", mutate: linkElsewhere(stateTmp)},
		{name: "the state area's out is a symbolic link", mutate: linkElsewhere(stateOut)},

		// The write bit is not only the top directory's: every write bit under
		// the checkout copy and the checks' results is refused, a file's and a
		// subdirectory's alike (issue #277, review R10 and audit A7).
		{name: "the checkout copy holds a writable file", mutate: chmodPath(0o644, gitConfig)},
		{
			name:   "the checkout copy holds a writable subdirectory",
			mutate: writableDir([]string{"checkout", "lib"}, checkout),
		},
		{
			name:   "the checks' results hold a writable file",
			mutate: writableFile([]string{"checks", "report.json"}, checks),
		},

		// A link out of the tree is how a read of the checkout copy reaches a
		// path the settings deny (issue #277, review R10 and audit A7).
		{
			name: "the checkout copy holds a link to another tree",
			mutate: mutateTree(checkout, func(t *testing.T, root string) {
				t.Helper()
				makeSymlink(t, "../state", pathOf(root, []string{"checkout", "escape"}))
			}),
		},
		{
			name: "the checks' results hold an absolute link",
			mutate: mutateTree(checks, func(t *testing.T, root string) {
				t.Helper()
				makeSymlink(t, t.TempDir(), pathOf(root, []string{"checks", "elsewhere"}))
			}),
		},
		{
			name: "the checkout copy holds a link that climbs out of the root",
			mutate: mutateTree(checkout, func(t *testing.T, root string) {
				t.Helper()
				makeSymlink(t, "../../../../../../etc", pathOf(root, []string{"checkout", "etc"}))
			}),
		},

		// The checkout copy's .git/config is the one local git configuration
		// the wrapper's git reads: a key or a section that names another
		// program, another file or a credential gives the containment away
		// (issue #277, review R14).
		{
			name: "the git config sets an extraheader",
			mutate: gitConfigWith(`[core]
	repositoryformatversion = 0
[http]
	extraheader = AUTHORIZATION: basic c2VjcmV0
`),
		},
		{
			name: "the git config sets a case-insensitive extraheader",
			mutate: gitConfigWith(`[CORE]
	repositoryformatversion = 0
[HTTP]
	ExtraHeader = AUTHORIZATION: basic c2VjcmV0
`),
		},
		{
			name:   "the git config sets fsmonitor",
			mutate: gitConfigWith("[core]\n\tfsmonitor = /srv/audit/work/fsmonitor\n"),
		},
		{
			name:   "the git config sets fsmonitor in another case",
			mutate: gitConfigWith("[core]\n\tFSMonitor = /srv/audit/work/fsmonitor\n"),
		},
		{
			name:   "the git config sets sshCommand",
			mutate: gitConfigWith("[core]\n\tsshCommand = /srv/audit/work/ssh\n"),
		},
		{
			name:   "the git config sets hooksPath",
			mutate: gitConfigWith("[core]\n\thooksPath = /srv/audit/work/hooks\n"),
		},
		{
			name:   "the git config holds a filter driver",
			mutate: gitConfigWith("[filter \"lfs\"]\n\tclean = /srv/audit/work/filter\n"),
		},
		{
			name:   "the git config holds a diff driver",
			mutate: gitConfigWith("[diff \"words\"]\n\ttextconv = /srv/audit/work/textconv\n"),
		},

		// The check is an allow list, not a list of the keys a report named: the
		// keys and sections a checkout's config holds are the whole rule, so
		// every other key is refused whatever its name says (issue #277, round D
		// finding 1).
		{
			name:   "the git config sets include.path",
			mutate: gitConfigWith("[include]\n\tpath = /srv/audit/work/extra\n"),
		},
		{
			name:   "the git config sets an includeIf",
			mutate: gitConfigWith("[includeIf \"gitdir:/srv/audit/work/\"]\n\tpath = /srv/audit/work/extra\n"),
		},
		{
			name:   "the git config sets gpg.program",
			mutate: gitConfigWith("[gpg]\n\tprogram = /srv/audit/work/gpg\n"),
		},
		{
			name:   "the git config sets gpg.ssh.program",
			mutate: gitConfigWith("[gpg \"ssh\"]\n\tprogram = /srv/audit/work/gpg-ssh\n"),
		},
		{
			name:   "the git config sets extensions.worktreeConfig",
			mutate: gitConfigWith("[extensions]\n\tworktreeConfig = true\n"),
		},
		{
			name:   "the git config sets an http section's extraheader",
			mutate: gitConfigWith("[http \"https://example.com\"]\n\textraHeader = AUTHORIZATION: basic c2VjcmV0\n"),
		},
		{
			name:   "the git config holds a merge driver",
			mutate: gitConfigWith("[merge \"theirs\"]\n\tdriver = /srv/audit/work/merge\n"),
		},
		{
			name:   "the git config sets core.pager",
			mutate: gitConfigWith("[core]\n\tpager = /srv/audit/work/pager\n"),
		},
		{
			name:   "the git config sets a key no report named",
			mutate: gitConfigWith("[user]\n\tname = the audit's model\n"),
		},

		// The parser places every line of a form git writes and refuses the
		// rest, rather than guessing what a line it cannot place means (issue
		// #277, round D finding 1).
		{
			name:   "the git config begins with a UTF-8 BOM",
			mutate: gitConfigWith("\ufeff[core]\n\trepositoryformatversion = 0\n"),
		},
		{
			name:   "the git config puts a key on the header's line",
			mutate: gitConfigWith("[core] filemode = true\n"),
		},
		{
			name:   "the git config puts two headers on one line",
			mutate: gitConfigWith("[core][gc]\n\tauto = 0\n"),
		},
		{
			name:   "the git config holds a subsection with a ] and a key after it",
			mutate: gitConfigWith("[branch \"a]b\"] remote = origin\n"),
		},
		{
			name:   "the git config leaves a subsection's quote open",
			mutate: gitConfigWith("[branch \"main]\n\tremote = origin\n"),
		},
		{
			name:   "the git config continues a value on the next line",
			mutate: gitConfigWith("[core]\n\texcludesfile = /srv/audit/work/ignore\\\n"),
		},
		{
			name:   "the git config holds a bare word",
			mutate: gitConfigWith("filemode true\n"),
		},

		// The .git directory holds one config file: a worktree's own, a
		// common-dir pointer and a gitdir pointer are the other files git reads a
		// configuration or a repository out of, and each is refused by its
		// presence, in any file type (issue #277, round D finding 1).
		{name: "the checkout copy's .git holds a config.worktree", mutate: gitFileIn("config.worktree", "[core]\n\tbare = false\n")},
		{name: "the checkout copy's .git holds a commondir", mutate: gitFileIn("commondir", "../state\n")},
		{name: "the checkout copy's .git holds a gitdir", mutate: gitFileIn("gitdir", "/srv/audit/state/elsewhere\n")},
		{name: "the checkout copy's .git holds a commondir as a link", mutate: gitLinkIn("commondir", "../.claude")},

		{
			name: "the checkout copy's .git is a file",
			mutate: mutateTree(checkout, func(t *testing.T, root string) {
				t.Helper()
				removeAll(t, pathOf(root, []string{"checkout", ".git"}))
				writeFile(t, pathOf(root, []string{"checkout", ".git"}), "gitdir: /srv/audit/state/elsewhere\n", 0o444)
			}),
		},
		{
			name: "the checkout copy's .git is a symbolic link",
			mutate: mutateTree(checkout, func(t *testing.T, root string) {
				t.Helper()
				removeAll(t, pathOf(root, []string{"checkout", ".git"}))
				makeSymlink(t, t.TempDir(), pathOf(root, []string{"checkout", ".git"}))
			}),
		},
	}

	// The entry types a checkout copy and the checks' results never hold, which
	// only a Unix host lays down: a FIFO and a unix socket (issue #277, round D
	// finding 3).
	cases = append(cases, unixLayoutCases...)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeAuditTree(t, root)
			tc.mutate(t, root)

			if err := audit.CheckLayout(root); err == nil {
				t.Errorf("CheckLayout(%q) = nil, want a refusal: %s is no layout ADR 0040 gives", root, tc.name)
			}
		})
	}
}

// TestCheckLayoutTakesALinkThatStaysInItsTree pins the other half of the walk
// over the checkout copy and the checks' results: a relative link that resolves
// inside its own tree is no way out of it, so it is not refused.
func TestCheckLayoutTakesALinkThatStaysInItsTree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeAuditTree(t, root)

	mutateTree(checks, func(t *testing.T, root string) {
		t.Helper()
		writeFile(t, pathOf(root, []string{"checks", "report.json"}), "{}", 0o444)
		makeSymlink(t, "report.json", pathOf(root, []string{"checks", "latest.json"}))
	})(t, root)

	if err := audit.CheckLayout(root); err != nil {
		t.Fatalf("CheckLayout(%q) = %v, want none: a relative link to a file of its own tree stays inside that tree", root, err)
	}
}

// pathOf returns the path of parts under root's audit directory.
func pathOf(root string, parts []string) string {
	return filepath.Join(append([]string{root, auditPath}, parts...)...)
}

// gone returns a mutation that deletes the entry parts names.
func gone(parts []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()
		removeAll(t, pathOf(root, parts))
	}
}

// chmodPath returns a mutation that gives the entry parts names mode.
func chmodPath(mode os.FileMode, parts []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		if err := os.Chmod(pathOf(root, parts), mode); err != nil {
			t.Fatalf("chmod %v to %#o: %v", parts, mode, err)
		}
	}
}

// linkElsewhere returns a mutation that replaces the entry parts names with a
// symbolic link to a directory of its own, so a check that followed it would
// read a tree that is not the audit directory's.
func linkElsewhere(parts []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		removeAll(t, pathOf(root, parts))

		target := filepath.Join(t.TempDir(), "elsewhere")
		makeDir(t, target)
		makeSymlink(t, target, pathOf(root, parts))
	}
}

// gitConfigWith returns a mutation that replaces the checkout copy's .git/config
// with body, mode 0444, so the tree holds the git configuration the case is
// about.
func gitConfigWith(body string) func(t *testing.T, root string) {
	return mutateTree(checkout, func(t *testing.T, root string) {
		t.Helper()

		removeAll(t, pathOf(root, gitConfig))
		writeFile(t, pathOf(root, gitConfig), body, 0o444)
	})
}

// gitFileIn returns a mutation that writes body to name inside the checkout
// copy's .git directory, mode 0444: one of the files git reads a configuration,
// a common directory or a repository out of, which the layout refuses beside
// .git/config.
func gitFileIn(name, body string) func(t *testing.T, root string) {
	return mutateTree(checkout, func(t *testing.T, root string) {
		t.Helper()
		writeFile(t, filepath.Join(pathOf(root, gitDir), name), body, 0o444)
	})
}

// gitLinkIn returns a mutation that makes name inside the checkout copy's .git
// directory a symbolic link to target: the same file in another form, which the
// layout refuses too.
func gitLinkIn(name, target string) func(t *testing.T, root string) {
	return mutateTree(checkout, func(t *testing.T, root string) {
		t.Helper()
		makeSymlink(t, target, filepath.Join(pathOf(root, gitDir), name))
	})
}

// unlock returns a mutation that makes the tree at parts writable, applies mutate
// and returns: the case decides which write bits the tree keeps.
func unlock(parts []string, mutate func(t *testing.T, root string)) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		makeWritable(t, pathOf(root, parts))
		mutate(t, root)
	}
}

// makeWritable gives every entry of the tree at path a write bit, a symbolic link
// followed nowhere: a mutation replaces entries deep in a read-only tree, and a
// 0555 directory cannot be emptied.
func makeWritable(t *testing.T, path string) {
	t.Helper()

	err := filepath.WalkDir(path, func(entry string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.Chmod(entry, info.Mode().Perm()|0o200) //nolint:gosec // G302: the owner's write bit, put back so a test can change the tree and remove it
	})
	if err != nil {
		t.Fatalf("make the tree under %s writable: %v", path, err)
	}
}

// mutateTree is unlock followed by makeReadOnly: every write bit the mutation
// added is gone again, so the defect a case names is its one entry, link or
// configuration and not a mode it left behind.
func mutateTree(parts []string, mutate func(t *testing.T, root string)) func(t *testing.T, root string) {
	apply := unlock(parts, mutate)

	return func(t *testing.T, root string) {
		t.Helper()

		apply(t, root)
		makeReadOnly(t, pathOf(root, parts))
	}
}

// writableFile returns a mutation that makes the file at parts mode 0644 and its
// tree read-only but for it: the one write bit the case is about is the file's.
func writableFile(parts []string, tree []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		mutateTree(tree, func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, pathOf(root, parts), "{}", 0o444)
		})(t, root)

		if err := os.Chmod(pathOf(root, parts), 0o644); err != nil { //nolint:gosec // G302: the file's own write bit is the one defect this case names
			t.Fatalf("chmod %v: %v", parts, err)
		}
	}
}

// writableDir returns a mutation that makes the directory at parts mode 0750 and
// its tree read-only but for it: the one write bit the case is about is the
// directory's.
func writableDir(parts []string, tree []string) func(t *testing.T, root string) {
	return func(t *testing.T, root string) {
		t.Helper()

		mutateTree(tree, func(t *testing.T, root string) {
			t.Helper()
			makeDir(t, pathOf(root, parts))
		})(t, root)

		if err := os.Chmod(pathOf(root, parts), 0o750); err != nil { //nolint:gosec // G302: the write bit this case is about
			t.Fatalf("chmod %v: %v", parts, err)
		}
	}
}

// makeReadOnly removes every write bit from the tree at path, a symbolic link
// followed nowhere: the mode the model-free step leaves the checkout copy and
// the checks' results in.
func makeReadOnly(t *testing.T, path string) {
	t.Helper()

	err := filepath.WalkDir(path, func(entry string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return os.Chmod(entry, info.Mode().Perm()&^writeBitsOfEveryone) //nolint:gosec // G122: the tree is this test's own temporary directory, walked so that every entry loses its write bits as the model-free step leaves it
	})
	if err != nil {
		t.Fatalf("remove the write bits under %s: %v", path, err)
	}
}

// writeAuditTree lays the audit directory out under root as the model-free step
// leaves it before the launcher runs: every path the ADR's table names, the
// binary the checksum step extracted into the state area's bin/, and the
// wrapper the model job built there.
func writeAuditTree(t *testing.T, root string) {
	t.Helper()

	for _, parts := range [][]string{
		checkout,
		checks,
		work,
		workTmp,
		state,
		home,
		stateTmp,
		stateBin,
		stateOut,
		binDir,
	} {
		makeDir(t, pathOf(root, parts))
	}

	writeFile(t, pathOf(root, wrapper), "#!/bin/sh\nexit 126\n", 0o555)
	writeFile(t, pathOf(root, claudeBin), claudeScript, 0o500)

	// The checkout copy is a clone, so it carries its own git configuration: a
	// benign one, which the layout takes.
	writeFile(t, pathOf(root, gitConfig), benignGitConfig, 0o444)

	// The audit's skill is the checkout copy's, and its system prompt is what
	// the audit's prompt reads.
	writeFile(t, pathOf(root, []string{"checkout", ".claude", "skills", "security-audit-run", "SKILL.md"}), "# The scheduled security audit\n\nRead the checkout, the checks and the work area.\n", 0o444)

	// The checkout copy and the checks' results are read-only to the model,
	// their contents too: the model-free step removes every write bit, and the
	// check holds the whole tree to it.
	for _, parts := range [][]string{checkout, checks} {
		makeReadOnly(t, pathOf(root, parts))
	}

	// The tree is laid out read-only, as the model-free step leaves it, and a
	// 0555 directory cannot be emptied: the test puts the write bits back before
	// the temporary directory is removed.
	t.Cleanup(func() { makeWritable(t, root) })
}

// removeAll deletes path and everything under it, a read-only tree included.
func removeAll(t *testing.T, path string) {
	t.Helper()

	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		makeWritable(t, path)
	}

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}
