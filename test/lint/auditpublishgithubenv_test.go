package lint_test

import (
	"slices"
	"strings"
	"testing"
)

// The group auditPublishGitHubEnvTag, pinned by TestForbidigoExclusionsDropOneGroup
// with the rules that scope it, is forbidigo's group for the calls the publish
// program's GitHub side, ci/auditpublish/internal/github, may not make (PRD #175
// milestone 2, ADR 0040, "The publish program's GitHub side"). depguard's
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
// Scope: every file under ci/auditpublish/internal/github/, _test.go files
// included, as the two linters.exclusions.rules rules of the group say and
// TestForbidigoExclusionsDropOneGroup checks.

// auditPublishGitHubEnvRefused lists every name an entry of the group must
// refuse, as forbidigo's analyze-types matching renders it: a package, the type
// the call is made on, and the function. A name is refused when an entry of the
// group matches its pkg and its rendered name; the wording of a msg is not pinned
// here, the tag is.
var auditPublishGitHubEnvRefused = []rootName{
	{"os", "os.Getenv"},
	{"os", "os.LookupEnv"},
	{"os", "os.ExpandEnv"},
	{"os", "os.Expand"},
	{"os", "os.Environ"},
	{"os", "os.UserHomeDir"},
	{"os", "os.UserConfigDir"},
	{"os", "os.UserCacheDir"},
	{"os", "os.TempDir"},
	{"os", "os.StartProcess"},
	{"os/exec", "exec.Command"},
	{"os/exec", "exec.LookPath"},
}

// auditPublishGitHubEnvAllowed are names no entry of the group may refuse: the
// one process start the library makes, with a context, the names that only extend
// a refused one (os.Environments, exec.Commands, exec.LookPaths), the os calls the
// library reads gh's output and the merged reports with and exits with, and a
// package named os or os/exec elsewhere.
var auditPublishGitHubEnvAllowed = []rootName{
	{"os/exec", "exec.CommandContext"},
	{"os/exec", "exec.Cmd"},
	{"os/exec", "exec.ExitError"},
	{"os/exec", "exec.Commands"},
	{"os/exec", "exec.LookPaths"},
	{"os", "os.ReadFile"},
	{"os", "os.Open"},
	{"os", "os.WriteFile"},
	{"os", "os.Exit"},
	{"os", "os.Getenvs"},
	{"os", "os.Environments"},
	{"os", "os.TempDirs"},
	{"example.com/otheros", "os.Getenv"},
	{"example.com/otheros/exec", "exec.Command"},
}

// TestAuditPublishGitHubEnvForbidigoPatterns checks the entries of the
// audit-publish-github-env group, read from .golangci.yml, offline and without
// golangci-lint: every name the GitHub side may not use is refused by an entry of
// the group, no name it may use is refused by one, and every entry refuses a
// listed name. It is the pin a reviewer reads when the group changes: without it
// an entry could be dropped from .golangci.yml and lint would stay green, as
// nothing else refuses exec.Command, which no depguard rule can see, in the
// library.
func TestAuditPublishGitHubEnvForbidigoPatterns(t *testing.T) {
	t.Parallel()

	entries := groupEntries(t, moduleRoot(t), auditPublishGitHubEnvTag)
	refuses := func(e forbidEntry, n rootName) bool {
		return e.pkg.MatchString(n.pkg) && e.pattern.MatchString(n.name)
	}

	for _, r := range auditPublishGitHubEnvRefused {
		var msgs []string

		for _, e := range entries {
			if refuses(e, r) {
				msgs = append(msgs, e.msg)
			}
		}

		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, auditPublishGitHubEnvTag+": ") }) {
			t.Errorf("%s (pkg %q) must be refused by an entry of group %s; refused by %q", r.name, r.pkg, auditPublishGitHubEnvTag, msgs)
		}
	}

	for _, n := range auditPublishGitHubEnvAllowed {
		for _, e := range entries {
			if refuses(e, n) {
				t.Errorf("%s (pkg %q) must pass the publish program's GitHub side, but the entry with msg %q refuses it", n.name, n.pkg, e.msg)
			}
		}
	}

	for _, e := range entries {
		if !slices.ContainsFunc(auditPublishGitHubEnvRefused, func(r rootName) bool { return refuses(e, r) }) {
			t.Errorf("the entry with msg %q refuses no name of the group's list; add the names it refuses", e.msg)
		}
	}
}
