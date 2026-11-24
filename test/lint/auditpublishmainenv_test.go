package lint_test

import "testing"

// The group audit-publish-main-env (auditPublishMainEnvTag in exclusions_test.go),
// pinned by TestForbidigoExclusionsDropOneGroup with the rules that scope it, is
// forbidigo's group for the calls the publish entry point,
// ci/auditpublish/main.go, may not make (PRD #175 milestone 2, ADR 0040, "The
// publish program's GitHub side"). depguard's audit-publish-main allows os there,
// which the entry point writes its summary and its line to and exits with, and
// refuses net, syscall and os/exec. os also reads and hands over the environment,
// where credentials live (CLAUDE.md rule 3), locates a home, config, cache or temp
// directory from it, and starts a process.
//
// The entry point reads the step's environment through one call only, the
// os.LookupEnv main.go passes to run; that name has an entry of its own, because
// the exclusion rule drops its msg in main.go alone. Everything else is refused:
// another way to read a variable or the whole environment, os.Expand, a process
// start through os, the lookups of a home, config, cache or temp directory, a
// temp file made in TMPDIR through os.CreateTemp, and exec.Cmd.Environ, the
// method that reads the whole process environment. os.Expand reads no variable by
// itself, but a mapping of os.Getenv makes it os.ExpandEnv; the env groups refuse
// it alike (issue #516).
//
// Scope: the files of ci/auditpublish/ outside its internal/ tree, _test.go files
// included, as the three linters.exclusions.rules rules of the group say and
// TestForbidigoExclusionsDropOneGroup checks.

// auditPublishMainEnvRefused lists every name the group must refuse, as
// forbidigo's analyze-types matching renders it: a package, the type the call is
// made on, and the function. A name is refused when an entry of the group matches
// its pkg and its rendered name. The refusal names the entry that must refuse it:
// auditPublishLookupEnvMsg for os.LookupEnv, the exception entry the main.go rule
// narrows on, and forbidGroupAnyEntry for the rest, which none of the group's
// exceptions may refuse.
var auditPublishMainEnvRefused = []forbidGroupRefusal{
	{rootName{"os", "os.Getenv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.LookupEnv"}, auditPublishLookupEnvMsg},
	{rootName{"os", "os.ExpandEnv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.Expand"}, forbidGroupAnyEntry},
	{rootName{"os", "os.Environ"}, forbidGroupAnyEntry},
	{rootName{"os", "os.CreateTemp"}, forbidGroupAnyEntry},
	{rootName{"os", "os.StartProcess"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserHomeDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserConfigDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserCacheDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.TempDir"}, forbidGroupAnyEntry},
	// A method is rendered as pkg.Type.Method, the pointer receiver dropped
	// (docs/architecture.md, How forbidigo renders a name). depguard refuses
	// os/exec in the entry point (audit-publish-main); the group refuses the
	// one call of its command type that reads the process environment.
	{rootName{"os/exec", "exec.Cmd.Environ"}, forbidGroupAnyEntry},
}

// auditPublishMainEnvExceptions lists the group's exception entries: the one whose
// msg the main.go rule quotes, and the one name it may refuse. It may refuse
// os.LookupEnv and no other, because that rule drops its whole msg in main.go
// alone, and a name it refused as well would pass there too.
var auditPublishMainEnvExceptions = []forbidGroupException{
	{auditPublishLookupEnvMsg, rootName{"os", "os.LookupEnv"}},
}

// auditPublishMainEnvAllowed are names no entry of the group may refuse: the os
// calls the entry point and its tests read and write their files with, the
// signals, the arguments and where they write, the names that only extend a
// refused one (os.Getenvs, os.LookupEnvs, os.ExpandEnvs, os.Environments,
// os.TempDirs, os.CreateTemps, os.UserHomeDirs, os.UserConfigDirs,
// os.UserCacheDirs, exec.Cmd.Environments), and a package named os elsewhere. An
// extension is listed so that an entry left unanchored fails here: a pattern that
// matches os.Expand as a prefix refuses os.ExpandEnv again, and one that matches
// exec.Cmd.Environ refuses its longer sibling.
var auditPublishMainEnvAllowed = []rootName{
	{"os", "os.Args"},
	{"os", "os.Exit"},
	{"os", "os.Interrupt"},
	{"os", "os.Stdout"},
	{"os", "os.Stderr"},
	{"os", "os.File"},
	{"os", "os.OpenFile"},
	{"os", "os.ReadFile"},
	{"os", "os.WriteFile"},
	{"os", "os.Mkdir"},
	{"os", "os.Symlink"},
	{"os", "os.Executable"},
	{"os", "os.ErrNotExist"},
	{"os", "os.O_APPEND"},
	{"os", "os.O_CREATE"},
	{"os", "os.O_WRONLY"},
	{"os", "os.Getenvs"},
	{"os", "os.LookupEnvs"},
	{"os", "os.Environments"},
	{"os", "os.TempDirs"},
	{"os", "os.CreateTemps"},
	{"os", "os.UserHomeDirs"},
	{"os", "os.UserConfigDirs"},
	{"os", "os.UserCacheDirs"},
	{"os", "os.ExpandEnvs"},
	{"os/exec", "exec.Cmd.Environments"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/otheros", "os.LookupEnv"},
}

// TestAuditPublishMainEnvForbidigoPatterns checks the entries of the
// audit-publish-main-env group: every name the entry point may not use is refused
// by the entry its refusal names, os.LookupEnv's exception entry refuses that name
// and no other, no name it may use is refused by an entry, and every entry refuses
// a listed name. Without it, os.Environ, os.CreateTemp or os.StartProcess could be
// moved into the os.LookupEnv exception entry by widening that entry's pattern,
// and lint would stay green, while the main.go rule, which narrows on its msg,
// would let any of them through in ci/auditpublish/main.go.
func TestAuditPublishMainEnvForbidigoPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupPatterns(t, auditPublishMainEnvTag, auditPublishMainEnvRefused, auditPublishMainEnvExceptions, auditPublishMainEnvAllowed)
}
