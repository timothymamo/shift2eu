package lint_test

import "testing"

// The group site-deploy-env (siteDeployEnvTag in exclusions_test.go), pinned by
// TestForbidigoExclusionsDropOneGroup with the three rules that scope it, is
// forbidigo's group for the calls the site's deploy program, ci/sitedeploy and
// its library, may not make (PRD #297 milestone 3). depguard's site-deploy-main
// allows os in the entry point, which reads the workflow's files and writes its
// summary; os also reads and hands over the environment, where credentials live
// (CLAUDE.md rule 3), locates a home, config, cache or temp directory from it,
// and starts a process.
//
// The deploy program reads the step's environment through one call only, the
// os.LookupEnv ci/sitedeploy/main.go passes to environment; that name has an
// entry of its own, because the ci/sitedeploy/main.go rule drops its msg in that
// file alone. Package os/exec has an entry of its own as well (issue #514):
// ci/sitedeploy/internal/deploy/livepage_test.go builds the real site bundle with
// go run ./ci/sitebuild, the one program ci/sitedeploy runs, and the
// livepage_test.go rule drops that entry's msg in that one file alone.
// Everything else is refused: another way to read a variable or the whole
// environment, os.Expand, a process start through os, and the four lookups of a
// home, config, cache or temp directory. os.Expand reads no variable by itself,
// but a mapping of os.Getenv turns it into os.ExpandEnv, so every env group
// refuses it alike (issue #516).
//
// Scope: every file under ci/sitedeploy/, _test.go files included, as the group's
// linters.exclusions.rules rules say and TestForbidigoExclusionsDropOneGroup
// checks. Two of those rules narrow on one entry's msg, so each entry's refusal
// set is pinned here entry by entry, each named by its msg and the pkg its pkg
// matches, as compositionrootnoexec_test.go pins composition-root-no-exec: an
// entry says which calls it refuses and no other entry of the group, instead of
// the name set deciding which entry may refuse what.

// siteDeployEnvOSMsg is the msg of the group's broad entry: the os reads of a
// variable and of the whole environment, the process start through os, and the
// four lookups that locate a directory from an unnamed variable. The msgs of the
// group's two entries whose msg an exclusion rule narrows on,
// siteDeployLookupEnvMsg and siteDeployExecMsg, are in exclusions_test.go, beside
// the rules that quote them.
const siteDeployEnvOSMsg = "site-deploy-env: the site deploy program reads no environment variable but through the os.LookupEnv main.go passes to environment, locates no home, config, cache or temp directory and starts no process (PRD #297, CLAUDE.md rule 3)"

// siteDeployEnvEntries lists every entry of the group, named by its msg and the
// package its pkg matches, with the exact set of names its pattern must refuse:
// the broad os entry's nine names, os.LookupEnv alone in the entry
// ci/sitedeploy/main.go's rule narrows on, and the uses of package os/exec in
// the entry livepage_test.go's rule narrows on. That entry's pattern admits the
// whole package as ^exec\., which its nine names pin in more than the four the
// test itself uses (exec.Command, exec.CommandContext, exec.Cmd, exec.LookPath):
// exec.ErrNotFound, exec.ExitError and exec.Error are values of the package no
// process start needs, and exec.Cmd.Run and exec.Cmd.Environ are methods of one of
// its types, the latter the one call that hands a child the whole environment of
// the test process (issue #185; the .golangci.yml comment says the entry refuses
// it with the rest of the package, unlike in the audit publish groups), so a
// pattern narrowed to those four no longer passes; a name outside package os/exec
// may not join it.
var siteDeployEnvEntries = []forbidGroupEntry{
	{
		msg: siteDeployEnvOSMsg,
		pkg: "os",
		refuses: []rootName{
			{"os", "os.Getenv"},
			{"os", "os.ExpandEnv"},
			{"os", "os.Expand"},
			{"os", "os.Environ"},
			{"os", "os.StartProcess"},
			{"os", "os.UserHomeDir"},
			{"os", "os.UserConfigDir"},
			{"os", "os.UserCacheDir"},
			{"os", "os.TempDir"},
		},
	},
	{
		msg: siteDeployLookupEnvMsg,
		pkg: "os",
		refuses: []rootName{
			{"os", "os.LookupEnv"},
		},
	},
	{
		msg: siteDeployExecMsg,
		pkg: "os/exec",
		refuses: []rootName{
			{"os/exec", "exec.Command"},
			{"os/exec", "exec.CommandContext"},
			{"os/exec", "exec.Cmd"},
			{"os/exec", "exec.LookPath"},
			{"os/exec", "exec.ErrNotFound"},
			{"os/exec", "exec.ExitError"},
			{"os/exec", "exec.Error"},
			{"os/exec", "exec.Cmd.Run"},
			{"os/exec", "exec.Cmd.Environ"},
		},
	},
}

// siteDeployEnvAllowed are names no entry of the group may refuse: the os calls
// the program and its library read, write and exit with, the signal it catches,
// the names that only extend a refused one (os.Getenvs, os.LookupEnvs,
// os.ExpandEnvs, os.Environments, os.StartProcesses, os.UserHomeDirs,
// os.UserConfigDirs, os.UserCacheDirs, os.TempDirs), a package named os
// elsewhere, and a package named exec elsewhere, which the os/exec entry's pkg
// keeps out.
var siteDeployEnvAllowed = []rootName{
	{"os", "os.Args"},
	{"os", "os.Chmod"},
	{"os", "os.DirFS"},
	{"os", "os.Exit"},
	{"os", "os.File"},
	{"os", "os.FileMode"},
	{"os", "os.Interrupt"},
	{"os", "os.IsNotExist"},
	{"os", "os.MkdirAll"},
	{"os", "os.O_APPEND"},
	{"os", "os.O_CREATE"},
	{"os", "os.O_WRONLY"},
	{"os", "os.OpenFile"},
	{"os", "os.ReadFile"},
	{"os", "os.Stderr"},
	{"os", "os.Stdout"},
	{"os", "os.WriteFile"},
	{"os", "os.Getenvs"},
	{"os", "os.LookupEnvs"},
	{"os", "os.ExpandEnvs"},
	{"os", "os.Environments"},
	{"os", "os.StartProcesses"},
	{"os", "os.UserHomeDirs"},
	{"os", "os.UserConfigDirs"},
	{"os", "os.UserCacheDirs"},
	{"os", "os.TempDirs"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/otheros", "os.LookupEnv"},
	{"example.com/otheros", "os.Environ"},
	{"example.com/otherexec", "exec.Command"},
	{"example.com/otherexec", "exec.CommandContext"},
}

// TestSiteDeployEnvEntryPatterns checks the entries of the site-deploy-env group
// one by one: the entry each pin names refuses every name the pin lists and no
// other listed name, no entry refuses a name siteDeployEnvAllowed lists, and every
// entry of the group is pinned. Without it os.Environ or os.StartProcess could be
// moved into the os.LookupEnv entry, or os.Getenv into the os/exec entry, and lint
// would drop the moved name in the one file that entry's rule covers
// (ci/sitedeploy/main.go, livepage_test.go) while staying green everywhere else.
func TestSiteDeployEnvEntryPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupEntries(t, siteDeployEnvTag, siteDeployEnvEntries, siteDeployEnvAllowed)
}
