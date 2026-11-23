package lint_test

import "testing"

// The group site-deploy-env (siteDeployEnvTag in exclusions_test.go), pinned by
// TestForbidigoExclusionsDropOneGroup with the two rules that scope it, is
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
// file alone. Everything else is refused: another way to read a variable or the
// whole environment, a process start through os, and the four lookups of a home,
// config, cache or temp directory.
//
// Scope: every file under ci/sitedeploy/, _test.go files included, as the group's
// two linters.exclusions.rules rules say and TestForbidigoExclusionsDropOneGroup
// checks.

// siteDeployEnvRefused lists every name the group must refuse, as forbidigo's
// analyze-types matching renders it: a package, the type the call is made on, and
// the function. A name is refused when an entry of the group matches its pkg and
// its rendered name. The refusal names the entry that must refuse it:
// siteDeployLookupEnvMsg for os.LookupEnv, the exception entry the main.go rule
// narrows on, and forbidGroupAnyEntry for the rest, which none of the group's
// exceptions may refuse.
var siteDeployEnvRefused = []forbidGroupRefusal{
	{rootName{"os", "os.Getenv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.LookupEnv"}, siteDeployLookupEnvMsg},
	{rootName{"os", "os.ExpandEnv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.Environ"}, forbidGroupAnyEntry},
	{rootName{"os", "os.StartProcess"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserHomeDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserConfigDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserCacheDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.TempDir"}, forbidGroupAnyEntry},
}

// siteDeployEnvExceptions lists the group's exception entries: the one whose msg
// the ci/sitedeploy/main.go rule quotes, and the one name it may refuse. It may
// refuse os.LookupEnv and no other, because that rule drops its whole msg in that
// file, and a name it refused as well would pass there too.
var siteDeployEnvExceptions = []forbidGroupException{
	{siteDeployLookupEnvMsg, rootName{"os", "os.LookupEnv"}},
}

// siteDeployEnvAllowed are names no entry of the group may refuse: the os calls
// the program and its library read, write and exit with, the signal it catches,
// the names that only extend a refused one (os.Getenvs, os.LookupEnvs,
// os.ExpandEnvs, os.Environments, os.StartProcesses, os.UserHomeDirs,
// os.UserConfigDirs, os.UserCacheDirs, os.TempDirs), and a package named os
// elsewhere.
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
}

// TestSiteDeployEnvForbidigoPatterns checks the entries of the site-deploy-env
// group: every name the deploy program may not use is refused by the entry its
// refusal names, os.LookupEnv's exception entry refuses that name and no other,
// no name it may use is refused by an entry, and every entry refuses a listed
// name. Without it, os.Environ or os.StartProcess could be moved into the
// os.LookupEnv exception entry by widening that entry's pattern, and lint would
// stay green, while the ci/sitedeploy/main.go rule, which narrows on its msg,
// would let either through in ci/sitedeploy/main.go.
func TestSiteDeployEnvForbidigoPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupPatterns(t, siteDeployEnvTag, siteDeployEnvRefused, siteDeployEnvExceptions, siteDeployEnvAllowed)
}
