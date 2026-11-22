// Pins how the scheduled audit's two Go packages are held (ADR 0040's
// milestone-2 amendment, issue #277): ci/internal/audit, the audit's library,
// and ci/auditrun, the launcher the model step runs. Each gets a strict depguard
// rule of named standard packages - an allow list, not $gostd, so a standard
// package nobody thought of is refused until the list names it - and the
// library's launcher, ci/internal/claudecode, is the one package of the module
// they may import.
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
// the packages that run a program outside the launcher, and lets the audit reach
// the shared Claude Code launcher.
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
		// wantAllowed is the package of the module the rule has to allow.
		wantAllowed string
	}{
		{name: auditLibraryRule, scope: auditLibraryScope, wantAllowed: claudeCodeLauncher},
		{name: auditMainRule, scope: auditMainScope, wantAllowed: auditLibraryPackage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rule := auditRuleNamed(t, rules, tc.name)

			if rule.ListMode != "strict" {
				t.Errorf("the depguard rule %s has list-mode %q, want strict: an allow list says what the audit's packages may import, so a package added to the standard library later is refused until the list names it", tc.name, rule.ListMode)
			}

			if !slices.Contains(rule.Files, tc.scope) {
				t.Errorf("the depguard rule %s holds %v, want it to hold %q: the rule is what keeps that package to its own list", tc.name, rule.Files, tc.scope)
			}

			allows := allowsPackages(rule.Allow)

			if slices.Contains(rule.Allow, gostdPackage) {
				t.Errorf("the depguard rule %s allows $gostd, which admits every standard package; name the packages the package imports today, each ending in $", tc.name)
			}

			for _, entry := range rule.Allow {
				if !strings.HasSuffix(entry, "$") && entry != gostdPackage {
					t.Errorf("the depguard rule %s allows %q, which is a prefix of a package path; name the package exactly, ending in $", tc.name, entry)
				}
			}

			if !allows(tc.wantAllowed) {
				t.Errorf("the depguard rule %s does not allow %s, which the audit's code imports: the launcher of the CLI the audit runs (ADR 0040)", tc.name, tc.wantAllowed)
			}

			// The audit's containment is what these packages hold: a raw system
			// call, a pointer taken apart or a library loaded into the process
			// would put the audit's decisions in code nobody reviewed.
			for _, denied := range []string{"syscall", "unsafe", "plugin", "runtime/cgo"} {
				if allows(denied) {
					t.Errorf("the depguard rule %s allows %s, which the audit's packages may not import", tc.name, denied)
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

// findDepguardRule returns the named rule of rules, and fails the test when the
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

			if pkg == entry || strings.HasPrefix(pkg, entry) {
				return true
			}
		}

		return false
	}
}
