// Pins how the scheduled audit's two Go packages are held (ADR 0040's
// milestone-2 amendment, issue #277): ci/internal/audit, the audit's library,
// and ci/auditrun, the launcher the model step runs. Each gets a strict depguard
// rule of named standard packages - an allow list, not $gostd, so a standard
// package nobody thought of is refused until the list names it - and each may
// import one package of the module: audit-library the shared launcher
// ci/internal/claudecode, and audit-main the library ci/internal/audit it runs
// (issue #277, round M, AI 8).
//
// A lax rule names who may import the library: ci/auditrun, the wrapper
// ci/auditexec and the library's own tests, and nobody else. Go's internal rule
// already keeps ci/internal/ out of the product's reach.
//
// The exact entries of each rule are pinned by pinnedDepguardRules in
// depguardpin_test.go, which the commit that edits .golangci.yml updates; this
// file holds the properties those entries have to keep, so a rule rewritten
// entry by entry still has to say the same thing.
package architecture_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
)

// The rule names, package paths and file scopes ADR 0040's amendment gives the
// audit's two packages.
const (
	auditLibraryRule    = "audit-library"
	auditMainRule       = "audit-main"
	auditImportersRule  = "audit-library-importers"
	auditLibraryPackage = "github.com/timothymamo/shift2eu/ci/internal/audit"
	auditMainPackage    = "github.com/timothymamo/shift2eu/ci/auditrun"
	claudeCodeLauncher  = "github.com/timothymamo/shift2eu/ci/internal/claudecode"
	auditLibraryScope   = "**/ci/internal/audit/**"
	auditMainScope      = "**/ci/auditrun/**"
	allFilesSelector    = "$all"
	// gostdPackage is the allow entry that would admit every standard package.
	gostdPackage = "$gostd"
	// The importer rule's exceptions: the library's own tests, the launcher and
	// the wrapper, which import it.
	notTheLibraryScope = "!**/ci/internal/audit/**"
	notTheMainScope    = "!**/ci/auditrun/**"
	notTheWrapperScope = "!**/ci/auditexec/**"
)

// TestAuditPackagesAreHeldByStrictRules pins the two strict rules: each names
// the packages of the audit's own imports one by one, admits no $gostd, denies
// what would put the audit's decisions in code nobody reviewed, and lets each
// package reach what it runs with - the library the shared Claude Code launcher,
// the launcher the library. The library imports os/exec on purpose, to run the
// wrapper and the CLI, so it is no package of the refused list.
func TestAuditPackagesAreHeldByStrictRules(t *testing.T) {
	t.Parallel()

	rules, err := check.DepguardRules(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf("DepguardRules(.golangci.yml): %v", err)
	}

	cases := []struct {
		name string
		// scope is the packages pattern the rule holds.
		scope string
		// held is the package that pattern holds: the one the rule is about, and
		// the one a failure names.
		held string
		// wantAllowed is the package of the module the rule has to allow.
		wantAllowed string
		// why says what wantAllowed is, so a failure names the package the rule
		// is missing (issue #277, round J nit 7).
		why string
	}{
		{
			name:        auditLibraryRule,
			scope:       auditLibraryScope,
			held:        auditLibraryPackage,
			wantAllowed: claudeCodeLauncher,
			why:         "the shared launcher of the CLI the audit runs",
		},
		{
			name:        auditMainRule,
			scope:       auditMainScope,
			held:        auditMainPackage,
			wantAllowed: auditLibraryPackage,
			why:         "the audit's library, whose preflight and run the launcher calls",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rule := auditRuleNamed(t, rules, tc.name)

			if rule.ListMode != "strict" {
				t.Errorf("the depguard rule %s has list-mode %q, want strict: an allow list says what %s may import, so a package added to the standard library later is refused until the list names it", tc.name, rule.ListMode, tc.held)
			}

			if !slices.Contains(rule.Files, tc.scope) {
				t.Errorf("the depguard rule %s holds %v, want it to hold %q: the rule is what keeps %s to its own list", tc.name, rule.Files, tc.scope, tc.held)
			}

			allows := allowsPackages(rule.Allow)

			if slices.Contains(rule.Allow, gostdPackage) {
				t.Errorf("the depguard rule %s allows $gostd, which admits every standard package; name the packages %s imports today, each ending in $", tc.name, tc.held)
			}

			for _, entry := range rule.Allow {
				if !strings.HasSuffix(entry, "$") && entry != gostdPackage {
					t.Errorf("the depguard rule %s allows %q, which is a prefix of a package path; name the package exactly, ending in $", tc.name, entry)
				}
			}

			if !allows(tc.wantAllowed) {
				t.Errorf("the depguard rule %s does not allow %s, %s: the rule is what keeps %s to the packages its own list names (ADR 0040)", tc.name, tc.wantAllowed, tc.why, tc.held)
			}

			// The audit's containment is what these packages hold: a raw system
			// call, a pointer taken apart or a library loaded into the process
			// would put the audit's decisions in code nobody reviewed.
			for _, denied := range []string{"syscall", "unsafe", "plugin", "runtime/cgo"} {
				if allows(denied) {
					t.Errorf("the depguard rule %s allows %s, which %s may not import", tc.name, denied, tc.held)
				}
			}
		})
	}
}

// TestAuditLibraryImportersAreNamed pins the lax rule around the audit's
// library: it denies the library everywhere, the library's own tests, the
// launcher and the wrapper excepted, so no other package of ci/ can borrow the
// audit's checks and run them under its own rules.
func TestAuditLibraryImportersAreNamed(t *testing.T) {
	t.Parallel()

	rules, err := check.DepguardRules(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf("DepguardRules(.golangci.yml): %v", err)
	}

	rule := auditRuleNamed(t, rules, auditImportersRule)

	denied := false

	for _, pkg := range rule.Deny {
		if strings.TrimSuffix(pkg, "$") == auditLibraryPackage {
			denied = true
		}
	}

	if !denied {
		t.Errorf("the depguard rule %s denies %v, want it to deny %s: only ci/auditrun and ci/auditexec import the audit's library (ADR 0040)", auditImportersRule, rule.Deny, auditLibraryPackage)
	}

	if !slices.Contains(rule.Files, allFilesSelector) {
		t.Errorf("the depguard rule %s holds %v, want it to start from %s: the rule is about every file, the exceptions aside", auditImportersRule, rule.Files, allFilesSelector)
	}

	for _, scope := range []string{notTheLibraryScope, notTheMainScope, notTheWrapperScope} {
		if !slices.Contains(rule.Files, scope) {
			t.Errorf("the depguard rule %s holds %v, want it to except %s: that package imports the audit's library (ADR 0040)", auditImportersRule, rule.Files, scope)
		}
	}
}

// auditRuleNamed returns the named rule of rules, and fails the test when the
// document holds none: a rule that went missing leaves a package unheld.
func auditRuleNamed(t *testing.T, rules []check.DepguardRule, name string) check.DepguardRule {
	t.Helper()

	for _, rule := range rules {
		if rule.Name == name {
			return rule
		}
	}

	t.Fatalf(".golangci.yml holds no depguard rule %s, which ADR 0040's amendment gives the audit's packages", name)

	return check.DepguardRule{}
}

// TestTheAuditsLibraryImportsTheSharedLauncher pins the other half of how the
// audit reaches the CLI: ci/internal/claudecode's own importer rule names the
// two callers the ADR gives it, the AI reviewer's library and the audit's, so
// the audit's import is not refused and no other package may make it.
func TestTheAuditsLibraryImportsTheSharedLauncher(t *testing.T) {
	t.Parallel()

	rules, err := check.DepguardRules(readRepositoryFile(t, ".golangci.yml"))
	if err != nil {
		t.Fatalf("DepguardRules(.golangci.yml): %v", err)
	}

	const launcherImportersRule = "claude-code-launcher-importers"

	rule := auditRuleNamed(t, rules, launcherImportersRule)

	for _, scope := range []string{"!**/ci/aireview/internal/review/**", notTheLibraryScope} {
		if !slices.Contains(rule.Files, scope) {
			t.Errorf("the depguard rule %s holds %v, want it to except %s: that package is one of the two callers of the shared launcher (ADR 0040)", launcherImportersRule, rule.Files, scope)
		}
	}
}

// allowsPackages returns whether a rule with the allow entries admits pkg, as
// depguard reads them: an entry ending in $ is exact, and any other is a prefix.
func allowsPackages(allow []string) func(pkg string) bool {
	return func(pkg string) bool {
		for _, entry := range allow {
			if exact, ok := strings.CutSuffix(entry, "$"); ok {
				if exact == pkg {
					return true
				}

				continue
			}

			if strings.HasPrefix(pkg, entry) {
				return true
			}
		}

		return false
	}
}
