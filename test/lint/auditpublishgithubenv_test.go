package lint_test

import "testing"

// The group audit-publish-github-env (auditPublishGitHubEnvTag in
// exclusions_test.go), pinned by TestForbidigoExclusionsDropOneGroup with the
// rules that scope it, is forbidigo's group for the calls the publish program's
// GitHub side, ci/auditpublish/internal/github, may not make (PRD #175 milestone
// 2, ADR 0040, "The publish program's GitHub side"). depguard's
// audit-publish-github allows os and os/exec, which the library reads gh's output
// and the merged reports with and starts gh and gitleaks with, each by its
// absolute path. os also reads and hands over the environment, where credentials
// live (CLAUDE.md rule 3), and exec.Command starts a process with no context, so
// nothing can time it out or cancel it, while exec.LookPath finds a program
// through PATH rather than by the absolute path the library is given.
//
// The group refuses every way to read a variable, the whole environment and the
// home, config, cache and temp lookups, and os.StartProcess; os.Environ has an
// entry of its own, because the fakes of the library's tests record the
// environment they were started with, in fake_test.go alone. It refuses
// exec.Command and exec.LookPath, and keeps exec.CommandContext, which starts gh
// and gitleaks under the deadline the run gives them.
//
// It also refuses os.CreateTemp, which makes a temp file in os.TempDir, TMPDIR by
// default: the library's temp files are made in the directory its Config names,
// which checkTempDir checks first, and a test that needs one has t.TempDir. And
// it refuses exec.Cmd.Environ, the method that reads the whole environment of the
// process, which is the one thing an explicit cmd.Env keeps from a child (issue
// #516).
//
// Scope: every file under ci/auditpublish/internal/github/, _test.go files
// included, as the two linters.exclusions.rules rules of the group say and
// TestForbidigoExclusionsDropOneGroup checks.

// auditPublishGitHubEnvRefused lists every name the group must refuse, as
// forbidigo's analyze-types matching renders it: a package, the type the call is
// made on, and the function. A name is refused when an entry of the group matches
// its pkg and its rendered name. The refusal names the entry that must refuse it:
// auditPublishEnvironMsg for os.Environ, the exception entry the fake_test.go
// rule narrows on, and forbidGroupAnyEntry for the rest, which none of the
// group's exceptions may refuse.
var auditPublishGitHubEnvRefused = []forbidGroupRefusal{
	{rootName{"os", "os.Getenv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.LookupEnv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.ExpandEnv"}, forbidGroupAnyEntry},
	{rootName{"os", "os.Expand"}, forbidGroupAnyEntry},
	{rootName{"os", "os.Environ"}, auditPublishEnvironMsg},
	{rootName{"os", "os.UserHomeDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserConfigDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.UserCacheDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.TempDir"}, forbidGroupAnyEntry},
	{rootName{"os", "os.CreateTemp"}, forbidGroupAnyEntry},
	{rootName{"os", "os.StartProcess"}, forbidGroupAnyEntry},
	{rootName{"os/exec", "exec.Command"}, forbidGroupAnyEntry},
	{rootName{"os/exec", "exec.LookPath"}, forbidGroupAnyEntry},
	// A method is rendered as pkg.Type.Method, the pointer receiver dropped
	// (docs/architecture.md, How forbidigo renders a name).
	{rootName{"os/exec", "exec.Cmd.Environ"}, forbidGroupAnyEntry},
}

// auditPublishGitHubEnvExceptions lists the group's exception entries: the one
// whose msg the fake_test.go rule quotes, and the one name it may refuse. It may
// refuse os.Environ and no other, because that rule drops its whole msg in that
// file, and a name it refused as well would pass there.
var auditPublishGitHubEnvExceptions = []forbidGroupException{
	{auditPublishEnvironMsg, rootName{"os", "os.Environ"}},
}

// auditPublishGitHubEnvAllowed are names no entry of the group may refuse: the
// one process start the library makes, with a context, the names that only extend
// a refused one (os.Environments, os.CreateTemps, exec.Commands, exec.LookPaths,
// exec.Cmd.Environments), the os calls the library reads gh's output and the
// merged reports with and exits with, and a package named os or os/exec
// elsewhere. An extension is listed so that an entry left unanchored fails here:
// a pattern that matches exec.Cmd.Environ as a prefix refuses its longer sibling
// too.
var auditPublishGitHubEnvAllowed = []rootName{
	{"os/exec", "exec.CommandContext"},
	{"os/exec", "exec.Cmd"},
	{"os/exec", "exec.ExitError"},
	{"os/exec", "exec.Commands"},
	{"os/exec", "exec.LookPaths"},
	{"os/exec", "exec.Cmd.Environments"},
	{"os", "os.ReadFile"},
	{"os", "os.Open"},
	{"os", "os.WriteFile"},
	{"os", "os.Exit"},
	{"os", "os.Getenvs"},
	{"os", "os.Environments"},
	{"os", "os.TempDirs"},
	{"os", "os.CreateTemps"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/otheros/exec", "exec.Command"},
}

// TestAuditPublishGitHubEnvForbidigoPatterns checks the entries of the
// audit-publish-github-env group: every name the GitHub side may not use is
// refused by the entry its refusal names, os.Environ's exception entry refuses
// that name and no other, no name it may use is refused by an entry, and every
// entry refuses a listed name. It is the pin a reviewer reads when the group
// changes: without it an entry could be dropped from .golangci.yml, or an
// exception entry's pattern widened to refuse a name it must leave alone (its pkg
// too, for exec.Command, whose pkg is os/exec), and lint would stay green, as
// nothing else refuses exec.Command, which no depguard rule can see, in the
// library.
func TestAuditPublishGitHubEnvForbidigoPatterns(t *testing.T) {
	t.Parallel()

	checkForbidigoGroupPatterns(t, auditPublishGitHubEnvTag, auditPublishGitHubEnvRefused, auditPublishGitHubEnvExceptions, auditPublishGitHubEnvAllowed)
}

// TestAuditPublishGitHubEnvForbidigoProbe runs forbidigo, with the repository's
// .golangci.yml, on a module of the GitHub side's tree (testdata/
// auditpublishgithubenv): a line there for every name the group refuses, each
// refused with a msg of the group, so that forbidigo matches the names the pin
// above reads from the patterns, os.Expand, os.CreateTemp, exec.Cmd.Environ and
// the os/exec calls included, and no other line of the tree is refused. The pin
// proves which entry refuses what; the probe proves that forbidigo renders the
// names it is asked about as the pin spells them.
func TestAuditPublishGitHubEnvForbidigoProbe(t *testing.T) {
	t.Parallel()

	runForbidigoProbe(t, forbidigoProbe{
		dir:  "auditpublishgithubenv",
		tags: []string{auditPublishGitHubEnvTag},
	})
}
