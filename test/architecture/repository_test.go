package architecture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/architecture/internal/check"
	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
)

const (
	// repoRoot is the module root, as the package directory go test runs in
	// reaches it.
	repoRoot = "../.."

	// modulePath is the module the check reads import paths under.
	modulePath = "github.com/timothymamo/shift2eu"
)

// testDocsRule is the strict depguard rule that holds the test trees' own
// documentation packages, test/architecture, test/container,
// test/devenv, test/docs and test/lint (issue #152, the auditor's finding H-1):
// each is a non-test package a composition root can import, and Go's internal
// rule keeps only the nested internal trees out of reach. It is skipped below,
// with the reason the package check cannot read it as a layer.
const testDocsRule = "test-docs-are-pure"

// skippedStrictRules are the strict rules of .golangci.yml the package check
// cannot hold, each with the reason it cannot. Every other strict rule is one
// the check reads a layer from: TestEveryStrictRuleIsChecked requires that, and
// TestReleaseTargetDependencies lists the packages of its directories. A strict
// rule in neither fails TestEveryStrictRuleIsChecked. Since issue #152 neither
// cli-gets-adapters-from-cmd nor version-is-build-metadata is skipped: both are
// strict allow lists of exact entries, which the check reads package by package.
var skippedStrictRules = map[string]string{
	"e2e-is-black-box":  "its allow list names $gostd, which check.AllowList refuses as it is no import path, so the package check cannot read the rule's imports; its production files are test/e2e/doc.go alone, which test-docs-are-pure holds since issue #152 (the reviewer's B1, the auditor's L-1): doc.go carries no build tag, so test/e2e is a package go list lists, and the e2e test files, which import go-cmp and internal/testutil, are the rule's own business",
	testDocsRule:        "the doc.go-only test packages import nothing, so there is no import of theirs to hold, and the pattern that would list them, ./test/architecture/..., also reaches test/architecture/internal/check, which reads os, os/exec and test/internal/releasetarget: a rule's files: list holds a directory, not a package, so holding ./test/architecture/... to this allow list would flag that package and the test/internal/... packages it imports, which Go's internal rule keeps out of reach of the roots and no depguard rule holds. The property that does hold these packages, TestEveryModulePackageIsHeldByAStrictRule, reads .golangci.yml directly rather than through go list",
	compositionRootRule: "go list -deps ./cmd/shift2eu/... lists the composition root's whole dependency tree, which the shared layer listing does not read: the adapters, internal/cli, internal/version and the standard library stand in no rule directory the listing holds, so listing the root beside the layers would flag them. TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget holds cmd/shift2eu to this rule's allow list instead, on every release target. What it reaches is held too, but not by this listing: TestEveryModulePackageIsHeldByAStrictRule holds every non-test module package a composition root can import to a strict rule of its own, so a package the root reaches cannot fall through unheld (issue #152)",
}

// sdkRule is the strict depguard rule that holds every cloud source adapter, and
// every helper directory under internal/adapter/source, to its cloud's SDK: its
// allow list names the SDK roots as import prefixes because depguard reads an
// entry without $ as one, and its files: list takes two directories out with
// negated globs and holds test files too (issue #79).
const sdkRule = "cloud-adapters-reach-the-cloud-through-the-sdk"

// compositionRootRule is the strict depguard rule that holds the composition
// root, cmd/shift2eu, and nothing beside it (issue #101): main wires every adapter
// into the application, so it may import every package of the module, and no
// third-party package, the lax cloud SDK and go-cmp denies aside. Its files:
// list names cmd/shift2eu alone, so a sibling composition root gets a rule of
// its own, written the same way, and not a wider glob.
const compositionRootRule = "composition-root"

// compositionRootDir is the directory the composition-root rule holds: the one
// package of the repository that may import every package of the module.
const compositionRootDir = "cmd/shift2eu"

// compositionRootStds are the standard packages cmd/shift2eu imports today, each
// allowed exactly as the rule writes it: context for the context a run takes, os
// for os.Interrupt, os.Args and os.Exit, os/signal for signal.NotifyContext and
// syscall for SIGTERM and SIGHUP. A standard package the composition root starts
// to import needs an entry in .golangci.yml, in the pin and here.
var compositionRootStds = []string{"context", "os", "os/signal", "syscall"}

// compositionRoot is one composition root of the module: the strict depguard rule
// that holds it, the directory the rule holds, and the standard packages the
// binary imports, each allowed exactly as its rule writes it.
type compositionRoot struct {
	rule string
	dir  string
	stds []string
}

// compositionRoots returns the composition roots, in the order .golangci.yml
// holds their rules: cmd/shift2eu (issue #101), the one root whose allow list
// names the module path as a prefix (cmd/shift2eu-web's rules name each package
// and are read as layers). Its strict allow list refuses a third-party import
// rather than review alone. Another such root gets a row here and a rule beside
// composition-root, written the same way.
func compositionRoots() []compositionRoot {
	return []compositionRoot{
		{rule: compositionRootRule, dir: compositionRootDir, stds: compositionRootStds},
	}
}

// prefixEntryRules are the strict rules whose allow list may hold a prefix
// entry, each named on purpose: the SDK-only rule names the roots of the cloud
// SDKs, whose every package an adapter may reach its cloud through, and the
// composition-root rule names the module path, whose every package main may
// import (issue #101). Every other strict rule names the packages it
// allows one by one, where an entry that lost its trailing $ is a widening the
// reviewer must see (os$ -> os allows os/exec) rather than a prefix.
var prefixEntryRules = []string{sdkRule, compositionRootRule}

// sdkRuleExcludes are the directories the SDK-only rule's files: list takes out
// of it with negated globs: the inventory-file reader holds a rule of its own,
// and so does the contract suite the adapters share.
var sdkRuleExcludes = []string{
	"internal/adapter/source/inventoryfile",
	"internal/adapter/source/sourcetest",
}

// sdkRulePrefixes are the rule's allow entries written as an import prefix: the
// three SDK roots, and go-cmp's package directory, whose packages the adapters'
// test files import. Every other entry ends in $ and allows one package alone.
var sdkRulePrefixes = []string{
	"cloud.google.com/go/",
	"github.com/Azure/azure-sdk-for-go/sdk/",
	"github.com/aws/aws-sdk-go-v2/",
	"github.com/google/go-cmp/cmp/",
}

// cgoMode is one build the checks read: .golangci.yml lints with cgo off and
// on, and the release builds with it off, so both are checked.
type cgoMode struct {
	name    string
	enabled bool
}

// cgoModes are the cgo settings every release target is checked with, shared by
// the shared layer listing (TestReleaseTargetDependencies) and by the
// composition root's own (TestCompositionRootIsHeldToItsRuleForEveryReleaseTarget),
// so a mode added here is read by both.
var cgoModes = []cgoMode{
	{name: "cgo=0", enabled: false},
	{name: "cgo=1", enabled: true},
}

// tagSet is one build's build tags: none, or the custom tags .golangci.yml
// loads.
type tagSet struct {
	name string
	tags []string
}

// tagSets are the tag sets every release target is checked with, shared by the
// layer listing and by the composition root's own: the e2e and integration files
// are behind build tags, make lint loads them, so the checks read the
// dependencies they bring in as well, and the build the release makes as well as
// a developer's test build.
var tagSets = []tagSet{
	{name: "tags=none", tags: nil},
	{name: "tags=e2e", tags: []string{"e2e"}},
	{name: "tags=integration", tags: []string{"integration"}},
	{name: "tags=e2e+integration", tags: []string{"e2e", "integration"}},
}

// TestRepositoryHasNoLineDirectives pins that no Go file of this repository
// carries a line directive, hand-written or generated: the linter matches
// depguard's file globs against the position a directive names, so one hides a
// file from the architecture rules (issue #40). A symlink among the files git
// tracks or would add fails this test too: it is reported rather than read
// through. What git ignores and does not track is not looked at.
func TestRepositoryHasNoLineDirectives(t *testing.T) {
	t.Parallel()

	directives, err := check.FindLineDirectives(t.Context(), repoRoot)
	if err != nil {
		t.Fatalf("FindLineDirectives(%q): %v", repoRoot, err)
	}

	if len(directives) != 0 {
		t.Errorf("the repository's Go files hold %d line directives or symlinks, which hide a file from the linter:\n%s",
			len(directives), strings.Join(directives, "\n"))
	}
}

// TestReleaseTargetDependencies pins the checks against the repository: for
// every platform .goreleaser.yaml builds, with cgo off and on and with the e2e
// and integration files off and on, the domain, the application layer and the
// shared readers import nothing the depguard rules refuse, and the release
// binary, the CLI, is built from packages of the main module
// alone, each in the directory its import path names and holding no source but
// Go files.
func TestReleaseTargetDependencies(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	golangci := readRepositoryFile(t, ".golangci.yml")
	goreleaser := readRepositoryFile(t, ".goreleaser.yaml")

	// .goreleaser.yaml is the one source of the release platforms: the check
	// reads the list from it rather than pinning a copy here.
	targets, err := releasetarget.Read(goreleaser)
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so the check below would run on nothing")
	}

	if !slices.Contains(targets, releasetarget.Target{GOOS: "linux", GOARCH: "amd64"}) {
		t.Errorf("releasetarget.Read(.goreleaser.yaml) = %v, want it to include linux/amd64", targets)
	}

	rules, patterns := depguardRules(t, golangci)

	for _, target := range targets {
		for _, cgo := range cgoModes {
			for _, tags := range tagSets {
				t.Run(fmt.Sprintf("%s/%s/%s/%s", target.GOOS, target.GOARCH, cgo.name, tags.name), func(t *testing.T) {
					t.Parallel()

					checkLayers(t, root, target, cgo.enabled, tags.tags, rules, patterns)
					for _, bin := range releaseBinaries {
						checkReleaseBinary(t, root, target, cgo.enabled, tags.tags, bin.main, bin.required)
					}
				})
			}
		}
	}
}

// depguardRules returns the rules the package check holds, read from the
// .golangci.yml document: for every strict rule the check can hold, one
// check.Rule per directory its files: list names, with that rule's allow list,
// and the go list pattern of that directory. Nothing is copied by hand, so a
// directory or a rule added to .golangci.yml is checked without an edit here.
func depguardRules(t *testing.T, golangci []byte) ([]check.Rule, []string) {
	t.Helper()

	strict, err := check.StrictRules(golangci)
	if err != nil {
		t.Fatalf("StrictRules(.golangci.yml): %v", err)
	}

	if len(strict) == 0 {
		t.Fatal("StrictRules(.golangci.yml) returned no rule, so the check below would run on nothing")
	}

	var (
		rules    []check.Rule
		patterns []string
	)

	for _, name := range strict {
		if _, skipped := skippedStrictRules[name]; skipped {
			continue
		}

		for _, rule := range rulesOf(t, golangci, name) {
			rules = append(rules, rule)
			patterns = append(patterns, "./"+rule.Dir+"/...")
		}
	}

	if len(rules) == 0 {
		t.Fatal("no strict rule gave the check a layer, so it would hold nothing")
	}

	return rules, patterns
}

// rulesOf returns what the package check holds for one strict depguard rule: one
// check.Rule per directory its files: list names, with the directories its
// negated globs take out of the rule, whether it holds test files, and that
// rule's allow list. Nothing is copied by hand, so a directory, an exclusion, a
// prefix allow entry or a rule added to .golangci.yml is checked without an edit
// here.
func rulesOf(t *testing.T, golangci []byte, name string) []check.Rule {
	t.Helper()

	files, err := check.RuleDirs(golangci, name)
	if err != nil {
		t.Fatalf("RuleDirs(.golangci.yml, %q): %v", name, err)
	}

	allow, err := check.AllowList(golangci, name)
	if err != nil {
		t.Fatalf("AllowList(.golangci.yml, %q): %v", name, err)
	}

	// An empty allow list is one the rule writes on purpose, allow: [], and
	// holds its directories to no import at all: AllowList refuses an allow
	// key left null, so no rule parsed as empty by mistake reaches here
	// (issue #166).

	rules := make([]check.Rule, 0, len(files.Dirs))

	for _, dir := range files.Dirs {
		rules = append(rules, check.Rule{
			Name:      name,
			Dir:       dir,
			Excludes:  files.Excludes,
			Tests:     files.Tests,
			TestsOnly: files.TestsOnly,
			Allow:     allow,
		})
	}

	return rules
}

// checkLayers lists the layer patterns for one build and fails when the check
// flags a package, when the listing fails, or when the listing did not hold the
// layers it was asked for.
func checkLayers(
	t *testing.T,
	root string,
	target releasetarget.Target,
	cgo bool,
	tags []string,
	rules []check.Rule,
	patterns []string,
) {
	t.Helper()

	pkgs, err := check.ListPackages(t.Context(), root, target, cgo, tags, patterns...)
	if err != nil {
		t.Fatalf("ListPackages(%v, cgo=%t, tags=%v): %v", target, cgo, tags, err)
	}

	if messages := check.Packages(root, modulePath, rules, pkgs); len(messages) != 0 {
		t.Errorf("Packages(%v, cgo=%t, tags=%v) flagged %d package(s):\n%s",
			target, cgo, tags, len(messages), strings.Join(messages, "\n"))
	}

	var listed []string

	for _, pkg := range pkgs {
		if !pkg.Standard {
			listed = append(listed, pkg.ImportPath)
		}
	}

	// A layer `go list` never read would pass the check above without being
	// looked at, so the directory of every rule must hold a listed package. The
	// four below are named as well: the rules are read from .golangci.yml, and
	// a rule quietly narrowed would otherwise narrow this guard with it.
	required := make([]string, 0, len(rules)+4)
	for _, rule := range rules {
		required = append(required, rule.Dir)
	}

	required = append(required,
		"internal/adapter/target/ovhcloud",
		"internal/adapter/target/scaleway",
		"internal/adapter/target/upcloud",
		"internal/adapter/internal/jsondoc",
	)

	seen := make(map[string]bool, len(required))

	for _, dir := range required {
		if seen[dir] {
			continue
		}

		seen[dir] = true

		prefix := modulePath + "/" + dir

		if !slices.ContainsFunc(listed, func(path string) bool {
			return path == prefix || strings.HasPrefix(path, prefix+"/")
		}) {
			t.Errorf("go list did not list a package of %s, so the check above read nothing of that layer; non-standard packages listed: %s",
				dir, strings.Join(listed, ", "))
		}
	}
}

// releaseBinaries are the binaries the release ships, each with the packages
// its listing must hold: without them the listing would pass over a binary
// that carries none of the packages the check exists for. The CLI, Main, is
// built from internal/cli and carries the three target adapters and the build
// metadata, internal/version, which the strict rule version-is-build-metadata
// holds (issue #152). It is the one release binary since the HTTP API was
// retired (issue #393, ADR 0037).
var releaseBinaries = []struct {
	main     string
	required []string
}{
	{
		main: releasetarget.Main,
		required: []string{
			modulePath + "/internal/adapter/target/ovhcloud",
			modulePath + "/internal/adapter/target/scaleway",
			modulePath + "/internal/adapter/target/upcloud",
			modulePath + "/internal/cli",
			modulePath + "/internal/version",
		},
	},
}

// checkReleaseBinary lists what one release binary, mainPkg, is built from for
// one build and fails when a package of the module breaks the source rules:
// Sources checks every package whose import path is the module path or under
// it and requires that it belongs to the main module, so a nested module under
// that path is flagged, that its directory with symlinks resolved is the one
// its import path names, and that it holds no source but Go files: no cgo, C,
// C++, header, Fortran, Objective-C, assembly, SWIG or syso file. Imports are
// not checked here, and the standard library and packages outside the module
// path (third-party code) are skipped. It fails too when go list did not list
// one of the required packages, since the checks above would then read nothing
// of them.
func checkReleaseBinary(t *testing.T, root string, target releasetarget.Target, cgo bool, tags []string, mainPkg string, required []string) {
	t.Helper()

	pkgs, err := check.ListPackages(t.Context(), root, target, cgo, tags, mainPkg)
	if err != nil {
		t.Fatalf("ListPackages(%s, %v, cgo=%t, tags=%v): %v", mainPkg, target, cgo, tags, err)
	}

	if messages := check.Sources(root, modulePath, pkgs); len(messages) != 0 {
		t.Errorf("Sources(%s, %v, cgo=%t, tags=%v) flagged %d package(s):\n%s",
			mainPkg, target, cgo, tags, len(messages), strings.Join(messages, "\n"))
	}

	for _, want := range required {
		if !slices.ContainsFunc(pkgs, func(pkg check.Package) bool { return pkg.ImportPath == want }) {
			t.Errorf("go list did not list %s in %s, so the check above read nothing of it", want, mainPkg)
		}
	}
}

// TestEveryStrictRuleIsChecked pins that every strict depguard rule of
// .golangci.yml is either one the package check reads a layer from (RuleDirs for
// its directories, AllowList for its imports) or a skip with a reason: a new
// strict rule in neither would refuse imports no test looks at, or the reverse,
// a rule quietly turned lax would leave its layer unchecked. A skip of a rule
// that is gone, or no longer strict, is an error too.
func TestEveryStrictRuleIsChecked(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	strict, err := check.StrictRules(golangci)
	if err != nil {
		t.Fatalf("StrictRules(.golangci.yml): %v", err)
	}

	if len(strict) == 0 {
		t.Fatal("StrictRules(.golangci.yml) returned no rule, so this test would check nothing")
	}

	for _, name := range strict {
		reason, skipped := skippedStrictRules[name]
		if skipped {
			if reason == "" {
				t.Errorf("%s is skipped without a reason: say why the package check cannot hold it", name)
			}

			continue
		}

		if files, err := check.RuleDirs(golangci, name); err != nil {
			t.Errorf("the strict depguard rule %s is not one the package check can hold: %v; give its files: list the shape RuleDirs reads, or add it to skippedStrictRules with the reason the check cannot hold it", name, err)
		} else if len(files.Dirs) == 0 {
			t.Errorf("RuleDirs(.golangci.yml, %s) returned no directory, so the rule holds no layer", name)
		}

		if _, err := check.AllowList(golangci, name); err != nil {
			t.Errorf("the strict depguard rule %s has no allow list the package check can read: %v", name, err)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(skippedStrictRules)) {
		if !slices.Contains(strict, name) {
			t.Errorf("skippedStrictRules holds %s, which .golangci.yml no longer holds with list-mode: strict, so the skip is stale: remove it, or restore the rule", name)
		}
	}
}

// TestCloudAdapterSDKRuleIsChecked pins that the SDK-only rule is no longer a
// skip (issue #79): the package check reads its files: list, its exclusions and
// its allow list, so TestEveryStrictRuleIsChecked and
// TestReleaseTargetDependencies hold every cloud source adapter to it, package
// by package, on every release target. The rule covers internal/adapter/source
// but the two directories it takes out with negated globs, holds test files,
// and allows the three SDK roots as import prefixes.
//
// The directories, the exclusions and the prefix entries are compared as whole
// sets: an exclusion or an exact entry that turned into a prefix without a
// reviewer seeing it is how a rule widens.
func TestCloudAdapterSDKRuleIsChecked(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	if reason, skipped := skippedStrictRules[sdkRule]; skipped {
		t.Errorf("%s is still skipped (%s); the package check reads prefix allow entries, negated files globs and test-file imports now: remove the skip", sdkRule, reason)
	}

	strict, err := check.StrictRules(golangci)
	if err != nil {
		t.Fatalf("StrictRules(.golangci.yml): %v", err)
	}

	if !slices.Contains(strict, sdkRule) {
		t.Fatalf("StrictRules(.golangci.yml) = %v, want it to hold %s", strict, sdkRule)
	}

	files, err := check.RuleDirs(golangci, sdkRule)
	if err != nil {
		t.Fatalf("RuleDirs(.golangci.yml, %q): %v", sdkRule, err)
	}

	if diff := cmp.Diff(sortedCopy([]string{"internal/adapter/source"}), sortedCopy(files.Dirs)); diff != "" {
		t.Errorf("RuleDirs(.golangci.yml, %q).Dirs mismatch (-want +got):\n%s", sdkRule, diff)
	}

	if diff := cmp.Diff(sortedCopy(sdkRuleExcludes), sortedCopy(files.Excludes)); diff != "" {
		t.Errorf("RuleDirs(.golangci.yml, %q).Excludes mismatch (-want +got):\n%s: the rule takes these two directories out, and no other", sdkRule, diff)
	}

	if !files.Tests {
		t.Errorf("RuleDirs(.golangci.yml, %q).Tests = false, want true: the rule names no !$test, so it holds test files, the integration ones included", sdkRule)
	}

	allow, err := check.AllowList(golangci, sdkRule)
	if err != nil {
		t.Fatalf("AllowList(.golangci.yml, %q): %v", sdkRule, err)
	}

	var prefixes []string

	for _, entry := range allow {
		if entry.Prefix {
			prefixes = append(prefixes, entry.Path)
		}
	}

	if diff := cmp.Diff(sortedCopy(sdkRulePrefixes), sortedCopy(prefixes)); diff != "" {
		t.Errorf("AllowList(.golangci.yml, %q) prefix entries mismatch (-want +got):\n%s: the SDK roots are the entries the rule allows whole trees through", sdkRule, diff)
	}

	// The entries that end in $ stay exact: depguard allows that package alone
	// through one.
	if !slices.ContainsFunc(allow, func(e check.AllowEntry) bool { return !e.Prefix && e.Path == "errors" }) {
		t.Errorf("AllowList(.golangci.yml, %q) = %v, want the exact entry errors$", sdkRule, allow)
	}
}

// TestOnlyNamedRulesHoldPrefixEntries is the second tripwire for a dropped $:
// before issue #79 a strict allow entry without one was refused, so `os$` that
// lost its dollar was caught twice over, and now a prefix entry is a shape the
// check reads. Only the rules named in prefixEntryRules may hold one (the SDK
// rule since issue #79, the composition-root rule since issue #101); in every
// other strict rule a prefix entry is a widening, and this fails, naming the
// rule and the entry.
//
// A rule the package check skips is not read here: AllowList refuses its $gostd,
// and TestDepguardRulesArePinned pins that list entry by entry.
func TestOnlyNamedRulesHoldPrefixEntries(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	strict, err := check.StrictRules(golangci)
	if err != nil {
		t.Fatalf("StrictRules(.golangci.yml): %v", err)
	}

	checked := 0

	for _, name := range strict {
		if _, skipped := skippedStrictRules[name]; skipped {
			continue
		}

		checked++

		allow, err := check.AllowList(golangci, name)
		if err != nil {
			t.Errorf("AllowList(.golangci.yml, %q): %v", name, err)

			continue
		}

		for _, entry := range allow {
			if !entry.Prefix || slices.Contains(prefixEntryRules, name) {
				continue
			}

			t.Errorf("the strict depguard rule %s allows %s by prefix, and only %s may hold a prefix entry: write the package exact (%s$) or, for a whole tree, end the entry in /",
				name, entry.Path, strings.Join(prefixEntryRules, " and "), entry.Path)
		}
	}

	if checked == 0 {
		t.Fatal("no strict rule was read, so this test would check nothing")
	}

	for _, name := range prefixEntryRules {
		if !slices.Contains(strict, name) {
			t.Errorf("prefixEntryRules holds %s, which .golangci.yml no longer holds with list-mode: strict, so the exception is stale: remove it, or restore the rule", name)
		}
	}
}

// TestCloudAdapterSDKRuleHoldsItsProbeForEveryReleaseTarget is the issue's probe
// (issue #79), one per platform the release builds: a fixture adapter under
// internal/adapter/source that imports net/http directly fails the package check
// with the rule read from .golangci.yml, on every GOOS/GOARCH the release
// targets list, exactly as depguard refuses it. The adapter package is listed
// for the platform first, so a run that read nothing of it cannot pass.
func TestCloudAdapterSDKRuleHoldsItsProbeForEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	targets, err := releasetarget.Read(readRepositoryFile(t, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so the probe below would run on nothing")
	}

	rules := rulesOf(t, golangci, sdkRule)

	root, err := filepath.Abs(filepath.FromSlash("testdata/adapterprobe"))
	if err != nil {
		t.Fatalf("resolve the probe module: %v", err)
	}

	const (
		probeModule = "example.com/adapterprobe"
		probePkg    = probeModule + "/internal/adapter/source/probe"
	)

	for _, target := range targets {
		t.Run(target.GOOS+"/"+target.GOARCH, func(t *testing.T) {
			t.Parallel()

			pkgs, err := check.ListPackages(t.Context(), root, target, false, nil, "./internal/adapter/source/...")
			if err != nil {
				t.Fatalf("ListPackages(adapterprobe, %v): %v", target, err)
			}

			if !slices.ContainsFunc(pkgs, func(pkg check.Package) bool { return pkg.ImportPath == probePkg }) {
				t.Fatalf("ListPackages(adapterprobe, %v) did not list %s, so the check below would read nothing of the probe", target, probePkg)
			}

			messages := check.Packages(root, probeModule, rules, pkgs)

			if len(messages) != 1 {
				t.Fatalf("Packages(adapterprobe, %v) flagged %d package(s), want exactly the probe's net/http import:\n%s",
					target, len(messages), strings.Join(messages, "\n"))
			}

			for _, want := range []string{probePkg + ": ", "net/http", sdkRule} {
				if !strings.Contains(messages[0], want) {
					t.Errorf("message %q does not name %q", messages[0], want)
				}
			}
		})
	}
}

// TestCloudAdapterSDKRuleHoldsItsSDKProbeForEveryReleaseTarget pins the other
// half of issue #79's SDK rule: an adapter that reaches its cloud through the
// SDK, as the rule requires, must pass, because depguard lints the adapter's
// files and never the SDK's. go list -deps lists the SDK package the adapter
// imports and every package that package imports, so the package check sees a
// third-party package under the rule's prefix entry and one that no entry names,
// and must report neither: otherwise every adapter fails the rule the day it
// imports its cloud's SDK.
//
// The fixture's SDK is two stand-in modules the go.mod replaces with local
// directories, so the listing needs no network and no go.sum entry.
func TestCloudAdapterSDKRuleHoldsItsSDKProbeForEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	targets, err := releasetarget.Read(readRepositoryFile(t, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so the probe below would run on nothing")
	}

	rules := rulesOf(t, golangci, sdkRule)

	root, err := filepath.Abs(filepath.FromSlash("testdata/sdkprobe"))
	if err != nil {
		t.Fatalf("resolve the probe module: %v", err)
	}

	const (
		probeModule = "example.com/sdkprobe"
		probePkg    = probeModule + "/internal/adapter/source/aws"
		sdkPkg      = "github.com/aws/aws-sdk-go-v2/service/ec2"
		sdkDepPkg   = "github.com/aws/smithy-go"
	)

	for _, target := range targets {
		t.Run(target.GOOS+"/"+target.GOARCH, func(t *testing.T) {
			t.Parallel()

			pkgs, err := check.ListPackages(t.Context(), root, target, false, nil, "./internal/adapter/source/...")
			if err != nil {
				t.Fatalf("ListPackages(sdkprobe, %v): %v", target, err)
			}

			// The probe, its SDK package and the package that one imports are
			// all listed: a run that read nothing of them could not pass this.
			for _, want := range []string{probePkg, sdkPkg, sdkDepPkg} {
				if !slices.ContainsFunc(pkgs, func(pkg check.Package) bool { return pkg.ImportPath == want }) {
					t.Fatalf("ListPackages(sdkprobe, %v) did not list %s, so the check below would read nothing of it", target, want)
				}
			}

			if messages := check.Packages(root, probeModule, rules, pkgs); len(messages) != 0 {
				t.Errorf("Packages(sdkprobe, %v) flagged %d package(s), want none: the probe reaches its cloud through the SDK the rule allows, and depguard lints no SDK file:\n%s",
					target, len(messages), strings.Join(messages, "\n"))
			}
		})
	}
}

// TestCompositionRootRulesAreChecked pins that each composition root of the
// module is held by a strict rule of its own: one whose files: list names that
// root and no sibling beside it, whose allow list is the module path as the one
// prefix entry, so main may import any package of the module, and the standard
// packages the binary imports today, each exact, so no third-party package gets
// in. Before cmd/shift2eu's rule, a new dependency of main was refused by no rule but
// the lax cloud-sdks-only-in-provider-adapters and go-cmp-only-in-tests denies, so
// it was held by review of the import and of go.mod alone: the gap issue #79 left,
// and ADR 0012 records. A sibling root with a module-prefix allow list would get
// the same rule, written the same way.
//
// Each rule is a reasoned skip of the shared layer listing (skippedStrictRules):
// go list -deps on a composition root lists its whole dependency tree, packages
// no rule the check reads holds, so the listing would flag them. This test reads
// each rule itself, and
// TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget holds each root
// to its own rule on every platform the release builds.
func TestCompositionRootRulesAreChecked(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	strict, err := check.StrictRules(golangci)
	if err != nil {
		t.Fatalf("StrictRules(.golangci.yml): %v", err)
	}

	if len(compositionRoots()) == 0 {
		t.Fatal("compositionRoots() returned no root, so this test would check nothing")
	}

	for _, root := range compositionRoots() {
		t.Run(root.rule, func(t *testing.T) {
			t.Parallel()

			if !slices.Contains(strict, root.rule) {
				t.Fatalf("StrictRules(.golangci.yml) = %v, want it to hold %s: no other rule refuses a third-party import in %s", strict, root.rule, root.dir)
			}

			if reason, skipped := skippedStrictRules[root.rule]; !skipped || reason == "" {
				t.Errorf("%s must be a reasoned skip of the shared layer listing, and is not: go list -deps ./%s/... lists packages no rule the check reads holds", root.rule, root.dir)
			}

			files, err := check.RuleDirs(golangci, root.rule)
			if err != nil {
				t.Fatalf("RuleDirs(.golangci.yml, %q): %v", root.rule, err)
			}

			if diff := cmp.Diff([]string{root.dir}, files.Dirs); diff != "" {
				t.Errorf("RuleDirs(.golangci.yml, %q).Dirs mismatch (-want +got):\n%s: the rule holds %s and nothing beside it, so each root gets a rule of its own rather than one widened glob", root.rule, diff, root.dir)
			}

			if len(files.Excludes) != 0 {
				t.Errorf("RuleDirs(.golangci.yml, %q).Excludes = %v, want none: the composition root has no directory under it to take out", root.rule, files.Excludes)
			}

			allow, err := check.AllowList(golangci, root.rule)
			if err != nil {
				t.Fatalf("AllowList(.golangci.yml, %q): %v", root.rule, err)
			}

			var exact, prefixes []string

			for _, entry := range allow {
				if entry.Prefix {
					prefixes = append(prefixes, entry.Path)

					continue
				}

				exact = append(exact, entry.Path)
			}

			if diff := cmp.Diff(sortedCopy(root.stds), sortedCopy(exact)); diff != "" {
				t.Errorf("AllowList(.golangci.yml, %q) exact entries mismatch (-want +got):\n%s: these are the standard packages %s imports today, and the only packages outside the module it may import", root.rule, diff, root.dir)
			}

			if diff := cmp.Diff([]string{modulePath + "/"}, prefixes); diff != "" {
				t.Errorf("AllowList(.golangci.yml, %q) prefix entries mismatch (-want +got):\n%s: the one prefix entry is the module's own path, so main may import any package of the module and no third-party package", root.rule, diff)
			}
		})
	}
}

// mainModuleMessages returns one message per package of pkgs whose import path
// is module or under it and that does not belong to the main module, named
// module. A nested module under that path is such a package: a composition-root
// rule allows the module path as a prefix entry, so the listing must be shown to
// hold none, or a module added under it would be let in unseen (the security
// audit's LOW-1, issue #101).
func mainModuleMessages(module string, pkgs []check.Package) []string {
	var messages []string

	for _, pkg := range pkgs {
		if pkg.ImportPath != module && !strings.HasPrefix(pkg.ImportPath, module+"/") {
			continue
		}

		switch m := pkg.Module; {
		case m == nil:
			messages = append(messages, pkg.ImportPath+" belongs to no module, want the main module "+module)
		case !m.Main || m.Path != module:
			messages = append(messages, pkg.ImportPath+" belongs to module "+m.Path+", want the main module "+module)
		}
	}

	return messages
}

// TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget holds each real
// composition root to the allow list of its rule read from .golangci.yml, on every
// platform the release builds, with cgo off and on and with the e2e and
// integration files off and on, as TestReleaseTargetDependencies reads them: the
// package must be listed, so a run that read nothing of it cannot pass, it must
// import nothing the list refuses, and every package the listing reaches under the
// module path must belong to the main module, since each rule's one prefix entry is
// that path (the security audit's LOW-1, issue #101). The same listing must also
// reach no package of the module whose path below the module path holds an element
// the go command skips when it walks ./...: testdata, or one starting with _ or .
// (check.HiddenPathMessages, issue #206). Such a package is built all the same when
// a package imports it by explicit path, and depguard and test/lint walk the tree
// the way the go command does, so it would reach a release binary with no
// architecture or lint rule applied to it; the probe of
// TestHiddenPathProbeIsRefusedForEveryReleaseTarget proves the check reports one. The
// rules are skipped by the shared layer listing (see skippedStrictRules), so this
// test reads the packages each rule holds and no other: they are the composition
// root's own, and every other package of the module is held by the rule of its
// layer in TestReleaseTargetDependencies.
func TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	golangci := readRepositoryFile(t, ".golangci.yml")

	targets, err := releasetarget.Read(readRepositoryFile(t, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so the check below would run on nothing")
	}

	if len(compositionRoots()) == 0 {
		t.Fatal("compositionRoots() returned no root, so this test would check nothing")
	}

	root := repositoryRoot(t)

	for _, rootCase := range compositionRoots() {
		rules := rulesOf(t, golangci, rootCase.rule)
		want := modulePath + "/" + rootCase.dir

		for _, target := range targets {
			for _, cgo := range cgoModes {
				for _, tags := range tagSets {
					t.Run(fmt.Sprintf("%s/%s/%s/%s/%s", rootCase.rule, target.GOOS, target.GOARCH, cgo.name, tags.name), func(t *testing.T) {
						t.Parallel()

						pkgs, err := check.ListPackages(t.Context(), root, target, cgo.enabled, tags.tags, "./"+rootCase.dir+"/...")
						if err != nil {
							t.Fatalf("ListPackages(%v, ./%s/..., cgo=%t, tags=%v): %v", target, rootCase.dir, cgo.enabled, tags.tags, err)
						}

						// A module added under the module path would be listed under that
						// path and the rule's prefix entry would allow its packages, so
						// the listing must not hold one.
						if messages := mainModuleMessages(modulePath, pkgs); len(messages) != 0 {
							t.Errorf("go list -deps ./%s/... (%v, cgo=%t, tags=%v) listed %d package(s) under %s that belong to no main module, which the rule's prefix entry would allow:\n%s",
								rootCase.dir, target, cgo.enabled, tags.tags, len(messages), modulePath, strings.Join(messages, "\n"))
						}

						// The listing must reach no package the go command builds only
						// by explicit path, which no rule walking the tree sees (issue
						// #206).
						if messages := check.HiddenPathMessages(modulePath, rootCase.dir, importPaths(pkgs)); len(messages) != 0 {
							t.Errorf("%s (%v, cgo=%t, tags=%v) reaches %d hidden-path package(s), want none:\n%s",
								rootCase.dir, target, cgo.enabled, tags.tags, len(messages), strings.Join(messages, "\n"))
						}

						held := heldByRules(modulePath, rules, pkgs)

						if !slices.ContainsFunc(held, func(pkg check.Package) bool { return pkg.ImportPath == want }) {
							t.Fatalf("go list did not list %s, so the check below would read nothing of the composition root", want)
						}

						if messages := check.Packages(root, modulePath, rules, held); len(messages) != 0 {
							t.Errorf("Packages(%s, %v, cgo=%t, tags=%v) flagged %d package(s), want none: the composition root imports only the standard packages its rule names and the module's own packages:\n%s",
								rootCase.dir, target, cgo.enabled, tags.tags, len(messages), strings.Join(messages, "\n"))
						}
					})
				}
			}
		}
	}
}

// compositionRootProbe is one probe of the composition-root rules (issue #101):
// the rule the probe is held to, the fixture package that rule holds,
// the sibling fixture package it must not hold, and the imports it must refuse for
// the package it holds.
type compositionRootProbe struct {
	root compositionRoot
	// probePkg is the fixture package root.rule holds.
	probePkg string
	// siblingPkg is the other fixture composition root, which root.rule holds not
	// at all: a rule widened to **/cmd/** or one whose directory is cmd fails here.
	siblingPkg string
	// wantRefused lists the imports of probePkg the rule must refuse, in the order
	// the check reports them by import path.
	wantRefused []string
	// wantMainModuleMessages is set on the probe that imports a nested module under
	// the fixture's module path: the listing must report exactly one package that is
	// not the main module, so the assertion the real roots get cannot pass by reading
	// nothing (the security audit's LOW-1, issue #101).
	wantMainModuleMessages bool
}

// TestCompositionRootRulesHoldTheirProbesForEveryReleaseTarget is the issue's probe
// (issue #101), one per platform the release builds and one per composition root: a
// fixture module whose composition roots import a third-party library and a cloud
// SDK, as a composition root must not, fails the package check with the rule read
// from .golangci.yml.
//
// The fixture's cmd/shift2eu also imports the standard packages cmd/shift2eu imports today
// and a nested module, example.com/cmdprobe/nested, whose path is under the probe's
// module path: the listing must report it as a package that is not the main module.
// The fixture's cmd/shift2eu-web stands in for a sibling root and imports the
// same third-party library and cloud SDK: the rule holds its own root and no
// sibling, so a files: list widened to **/cmd/** fails here, both by the packages
// it holds and by the sibling's refused imports.
//
// The fixture's stand-in modules are local directories its go.mod replaces, so the
// listing needs no network and no go.sum entry, as sdkprobe's do.
func TestCompositionRootRulesHoldTheirProbesForEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	const (
		probeModule = "example.com/cmdprobe"
		probeDir    = "testdata/cmdprobe"
		loggingPkg  = "example.com/logging"
		sdkPkg      = "github.com/aws/aws-sdk-go-v2/service/ec2"
		nestedPkg   = probeModule + "/nested"
		cliPkg      = probeModule + "/cmd/shift2eu"
		siblingPkg  = probeModule + "/cmd/shift2eu-web"
	)

	roots := compositionRoots()

	if len(roots) != 1 {
		t.Fatalf("compositionRoots() = %v, want the one root the cmdprobe fixture stands in for", roots)
	}

	probes := []compositionRootProbe{
		{
			root:                   roots[0],
			probePkg:               cliPkg,
			siblingPkg:             siblingPkg,
			wantRefused:            []string{loggingPkg, nestedPkg, sdkPkg},
			wantMainModuleMessages: true,
		},
	}

	golangci := readRepositoryFile(t, ".golangci.yml")

	targets, err := releasetarget.Read(readRepositoryFile(t, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so the probes below would run on nothing")
	}

	root, err := filepath.Abs(filepath.FromSlash(probeDir))
	if err != nil {
		t.Fatalf("resolve the probe module: %v", err)
	}

	for _, probe := range probes {
		rules := rulesOf(t, golangci, probe.root.rule)

		for _, target := range targets {
			t.Run(probe.root.rule+"/"+target.GOOS+"/"+target.GOARCH, func(t *testing.T) {
				t.Parallel()

				pkgs, err := check.ListPackages(t.Context(), root, target, false, nil, "./cmd/...")
				if err != nil {
					t.Fatalf("ListPackages(cmdprobe, %v): %v", target, err)
				}

				// Each is listed, so a run that read nothing of it cannot pass.
				for _, want := range []string{probe.probePkg, probe.siblingPkg} {
					if !slices.ContainsFunc(pkgs, func(pkg check.Package) bool { return pkg.ImportPath == want }) {
						t.Fatalf("ListPackages(cmdprobe, %v) did not list %s, so the check below would read nothing of it", target, want)
					}
				}

				if probe.wantMainModuleMessages {
					moduleMessages := mainModuleMessages(probeModule, pkgs)

					if len(moduleMessages) != 1 {
						t.Fatalf("mainModuleMessages(cmdprobe, %v) reported %d package(s), want exactly the fixture's nested module:\n%s",
							target, len(moduleMessages), strings.Join(moduleMessages, "\n"))
					}

					for _, want := range []string{nestedPkg, "want the main module " + probeModule} {
						if !strings.Contains(moduleMessages[0], want) {
							t.Errorf("message %q does not name %q", moduleMessages[0], want)
						}
					}
				}

				held := heldByRules(probeModule, rules, pkgs)

				heldPaths := make([]string, 0, len(held))
				for _, pkg := range held {
					heldPaths = append(heldPaths, pkg.ImportPath)
				}

				if diff := cmp.Diff([]string{probe.probePkg}, heldPaths); diff != "" {
					t.Fatalf("the %s rule holds these packages of the probe (-want +got):\n%s: it holds its own composition root and no sibling beside it",
						probe.root.rule, diff)
				}

				messages := check.Packages(root, probeModule, rules, held)

				if len(messages) != len(probe.wantRefused) {
					t.Fatalf("Packages(cmdprobe, %v) under %s flagged %d package(s), want exactly %v:\n%s",
						target, probe.root.rule, len(messages), probe.wantRefused, strings.Join(messages, "\n"))
				}

				for _, imp := range probe.wantRefused {
					if !slices.ContainsFunc(messages, func(message string) bool {
						return strings.Contains(message, probe.probePkg+": imports "+imp) &&
							strings.Contains(message, probe.root.rule)
					}) {
						t.Errorf("no message names the import %s of %s under the rule %s, want it refused:\n%s",
							imp, probe.probePkg, probe.root.rule, strings.Join(messages, "\n"))
					}
				}
			})
		}
	}
}

// hiddenPathProbeModule is the module path of the fixture of issue #206, the
// probe whose composition root imports packages the go command builds only by
// explicit path.
const hiddenPathProbeModule = "example.com/hiddenpathprobe"

// hiddenPathProbeDir is the fixture module's directory, under testdata so the
// go command skips it when it walks this module's ./... and lint never reads it.
const hiddenPathProbeDir = "testdata/hiddenpathprobe"

// hiddenPathProbeRoot is the composition root of the fixture, relative to its
// module root: the package whose go list -deps listing must report the hidden
// packages it reaches.
const hiddenPathProbeRoot = "cmd/shift2eu"

// TestHiddenPathProbeIsRefusedForEveryReleaseTarget is the probe of issue #206,
// for every platform the release builds, with cgo off and on and with the e2e
// and integration files off and on: the fixture module hiddenpathprobe's
// cmd/shift2eu imports example.com/hiddenpathprobe/testdata/x,
// example.com/hiddenpathprobe/_x and example.com/hiddenpathprobe/.x, so its go
// list -deps listing must report all three, each message naming the composition
// root and the package. The fixture's own go.mod sits under this repository's
// testdata, so this module's go command skips it and lint never reads it.
func TestHiddenPathProbeIsRefusedForEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	const (
		testdataPkg   = hiddenPathProbeModule + "/testdata/x"
		underscorePkg = hiddenPathProbeModule + "/_x"
		dotPkg        = hiddenPathProbeModule + "/.x"
	)

	// The finding format, pinned whole: the root's directory, the package's
	// import path and what the go command does with it.
	wantMessages := []string{
		"cmd/shift2eu reaches example.com/hiddenpathprobe/.x, which ./... never walks but go builds when a package imports it by explicit path: no lint or architecture rule sees it",
		"cmd/shift2eu reaches example.com/hiddenpathprobe/_x, which ./... never walks but go builds when a package imports it by explicit path: no lint or architecture rule sees it",
		"cmd/shift2eu reaches example.com/hiddenpathprobe/testdata/x, which ./... never walks but go builds when a package imports it by explicit path: no lint or architecture rule sees it",
	}

	targets, err := releasetarget.Read(readRepositoryFile(t, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so the probe below would run on nothing")
	}

	root, err := filepath.Abs(filepath.FromSlash(hiddenPathProbeDir))
	if err != nil {
		t.Fatalf("resolve the probe module: %v", err)
	}

	for _, target := range targets {
		for _, cgo := range cgoModes {
			for _, tags := range tagSets {
				t.Run(fmt.Sprintf("%s/%s/%s/%s", target.GOOS, target.GOARCH, cgo.name, tags.name), func(t *testing.T) {
					t.Parallel()

					pkgs, err := check.ListPackages(t.Context(), root, target, cgo.enabled, tags.tags, "./"+hiddenPathProbeRoot+"/...")
					if err != nil {
						t.Fatalf("ListPackages(hiddenpathprobe, %v, cgo=%t, tags=%v): %v", target, cgo.enabled, tags.tags, err)
					}

					// The root itself is listed, so a run that read nothing of the
					// probe cannot pass.
					wantRoot := hiddenPathProbeModule + "/" + hiddenPathProbeRoot
					if !slices.ContainsFunc(pkgs, func(pkg check.Package) bool { return pkg.ImportPath == wantRoot }) {
						t.Fatalf("ListPackages(hiddenpathprobe, %v) did not list %s, so the check below would read nothing of it", target, wantRoot)
					}

					imports := importPaths(pkgs)

					wantHidden := []string{dotPkg, underscorePkg, testdataPkg}
					if diff := cmp.Diff(wantHidden, check.HiddenPathPackages(hiddenPathProbeModule, imports)); diff != "" {
						t.Errorf("HiddenPathPackages(hiddenpathprobe) mismatch (-want +got):\n%s: the probe's cmd/shift2eu imports all three, and go list -deps lists a package the go command builds by explicit path", diff)
					}

					if diff := cmp.Diff(wantMessages, check.HiddenPathMessages(hiddenPathProbeModule, hiddenPathProbeRoot, imports)); diff != "" {
						t.Errorf("HiddenPathMessages(hiddenpathprobe) mismatch (-want +got):\n%s: each finding names the composition root and the package it reaches", diff)
					}
				})
			}
		}
	}
}

// importPaths returns the import paths of pkgs, in go list's order, for the
// checks that read a listing as paths alone.
func importPaths(pkgs []check.Package) []string {
	imports := make([]string, 0, len(pkgs))

	for _, pkg := range pkgs {
		imports = append(imports, pkg.ImportPath)
	}

	return imports
}

// heldByRules returns the packages of pkgs that sit in a directory one of rules
// holds and none it excludes, the way check.Packages holds a package. It is for
// the composition root alone: go list -deps on its directory lists every package
// of the module it reaches, each of which a strict rule of its own holds
// (cli-gets-adapters-from-cmd since issue #152, version-is-build-metadata for the
// build metadata), and the shared layer listing reads no rule's dependency tree,
// so passing the whole listing would flag them for being outside every directory
// it holds. Every other layer's listing reaches only layers, which
// TestReleaseTargetDependencies holds as a whole.
func heldByRules(module string, rules []check.Rule, pkgs []check.Package) []check.Package {
	var held []check.Package

	for _, pkg := range pkgs {
		rel, inModule := strings.CutPrefix(pkg.ImportPath, module+"/")
		if !inModule {
			continue
		}

		if slices.ContainsFunc(rules, func(rule check.Rule) bool {
			return underDir(rel, rule.Dir) &&
				!slices.ContainsFunc(rule.Excludes, func(dir string) bool { return underDir(rel, dir) })
		}) {
			held = append(held, pkg)
		}
	}

	return held
}

// underDir reports whether rel, a package directory relative to the module root,
// is dir or a directory below it.
func underDir(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// TestReleaseWorkflowIsClosed pins that the release is closed to what the
// check lists: the workflow holds exactly the ten jobs the check knows, test,
// latest, goreleaser, attest-archives and publish (issues #156, #163), image,
// sbom and sign for the CLI's image, verify and verify-archives, with exactly
// their steps, so goreleaser
// runs with exactly release --clean after no repository code and with no
// environment of its own for the go command, only a commit on main is released,
// and the publishing privileges stay split: the image job publishes what it
// builds, the SBOM job only reads the registry and signs nothing, and the sign
// job runs sigstore's and GitHub's actions alone (check.ReleaseWorkflow). The
// release ships the CLI alone, so no line may name the HTTP API's binary or its
// OpenAPI document, which are gone (issue #393). Nor
// may any other goreleaser document, go.work, go.work.sum or vendor sit at the
// root that git could commit (check.StrayReleaseFiles).
func TestReleaseWorkflowIsClosed(t *testing.T) {
	t.Parallel()

	stray, err := check.StrayReleaseFiles(t.Context(), repositoryRoot(t))
	if err != nil {
		t.Fatalf("StrayReleaseFiles(): %v", err)
	}

	for _, message := range stray {
		t.Error(message)
	}

	for _, message := range check.ReleaseWorkflow(readRepositoryFile(t, ".github/workflows/release.yml")) {
		t.Error(message)
	}
}

// TestReleaseArchivesHoldOneBinary pins issue #147 as issue #393 leaves it:
// the release ships the CLI's binary in one archive of its own.
// .goreleaser.yaml holds one archive entry, limited by ids: to the CLI's
// build, keeping the name it has always had (check.GoReleaserArchives), with
// tar.gz everywhere, zip on Windows, and one checksums.txt covering it; and
// ci.yml's test job, the dry run that runs on every pull request, installs the
// GoReleaser that release.yml runs and builds that archive with it, failing
// unless it holds exactly its own binary (check.DryRunVerifiesArchives). The
// files the archive ships are LICENSE, NOTICE (issue #493), README.md,
// SECURITY.md (issue #450),
// the pages under docs/ (issue #149) and the sample inventories under
// examples/ the walkthrough runs (issue #188). No second archive, the HTTP
// API's, is published any more, and neither is the bundled OpenAPI document
// (issue #393).
func TestReleaseArchivesHoldOneBinary(t *testing.T) {
	t.Parallel()

	for _, message := range check.GoReleaserArchives(readRepositoryFile(t, ".goreleaser.yaml")) {
		t.Error(message)
	}

	ci := readRepositoryFile(t, ".github/workflows/ci.yml")
	release := readRepositoryFile(t, ".github/workflows/release.yml")

	for _, message := range check.DryRunVerifiesArchives(ci, release) {
		t.Error(message)
	}
}

// TestRCTagsArePreReleases pins how .goreleaser.yaml publishes the GitHub
// release: release.draft: true, so the release is published last, by
// release.yml (issue #156), and release.prerelease: auto, so a tag with a
// pre-release suffix (v0.1.0-rc.2) is a GitHub pre-release and never the
// latest release, as it gets no X.Y and no latest image tag (issue #92, ADR
// 0015). It holds nothing else: the bundled OpenAPI document the release used
// to publish beside the archives is gone (issue #393).
func TestRCTagsArePreReleases(t *testing.T) {
	t.Parallel()

	if err := check.GitHubRelease(readRepositoryFile(t, ".goreleaser.yaml")); err != nil {
		t.Error(err)
	}
}

// TestReleasePleaseProposesTheNextVersion pins issues #142, #157 and #159:
// release-please keeps one release PR open on main, computes the next version
// from the Conventional Commits since the last tag with the release-tag skill's
// rules, and maintains CHANGELOG.md; merging the PR pushes the vX.Y.Z tag with
// the release-tag App's short-lived token, and that tag starts
// .github/workflows/release.yml exactly as it runs today. The workflow is held
// to check.ReleasePleaseWorkflow and the document it reads to
// check.ReleasePleaseConfig, so the version rules, the tag form, the two Apps'
// permissions and the one-release-at-a-time group stay read from the files the
// workflow reads. ci.yml is held to check.ReleasePleaseDryRun, so the dry run of
// the workflow's guard, tag and relabel steps runs on every pull request and
// every push to main, with no write and no secret.
func TestReleasePleaseProposesTheNextVersion(t *testing.T) {
	t.Parallel()

	for _, message := range check.ReleasePleaseWorkflow(readRepositoryFile(t, ".github/workflows/release-please.yml")) {
		t.Error(message)
	}

	for _, message := range check.ReleasePleaseConfig(readRepositoryFile(t, "release-please-config.json")) {
		t.Error(message)
	}

	for _, message := range check.ReleasePleaseDryRun(readRepositoryFile(t, ".github/workflows/ci.yml")) {
		t.Error(message)
	}
}

// TestRecordedReleaseAppIdentifiersAreTheSame pins the two documents that
// record the two Apps' identifiers, .claude/skills/release-tag/SKILL.md and ADR
// 0021: the release-tag skill fails closed on the release-tag App's ID, and
// both documents compare the tag rulesets and the release PR's author against
// the recorded slugs, so the two records must agree. The release-tag App's ID
// is a number or the maintainer's placeholder and nothing else, neither of its
// values is the release-please App's, and the release-please App's own ID and
// slug are recorded in both (#157).
func TestRecordedReleaseAppIdentifiersAreTheSame(t *testing.T) {
	t.Parallel()

	skill := readRepositoryFile(t, ".claude/skills/release-tag/SKILL.md")
	adr := readRepositoryFile(t, "docs/adr/0021-propose-releases-with-release-please-and-tag-with-a-github-app.md")

	for _, message := range check.ReleaseAppIdentifiers(skill, adr) {
		t.Error(message)
	}
}

// TestWorkflowPrivilegesAreClosed pins the audit's finding of issue #72: no
// workflow but .github/workflows/release.yml may hold a packages: write,
// id-token: write or attestations: write permission, a write-all, or an
// on: push: tags: trigger. A workflow that pushes to a registry, mints an OIDC
// identity or runs on a tag publishes releases, and the release workflow is the
// one place that is reviewed for it.
func TestWorkflowPrivilegesAreClosed(t *testing.T) {
	t.Parallel()

	const dir = ".github/workflows"

	entries, err := os.ReadDir(filepath.Join(repositoryRoot(t), filepath.FromSlash(dir)))
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	if len(entries) == 0 {
		t.Fatalf("%s holds no workflow, so this test would check nothing", dir)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := dir + "/" + entry.Name()

		for _, message := range check.WorkflowPrivileges(name, readRepositoryFile(t, name)) {
			t.Error(message)
		}
	}
}

// TestCIImageJobIsTheReleaseDryRun pins the review's finding of issue #72:
// ci.yml's image job is the dry run of release.yml's image and sbom jobs, for
// the CLI's image, the one the release publishes (issue #393), and of the
// container run it gets in the verify job (issue #99), so a
// pull request fails when the release's platforms, annotations, provenance and
// SBOM settings, tag rules, syft version, action pins, BuildKit image or
// container run drift from what the dry run builds and checks
// (check.DryRunMirrorsRelease).
func TestCIImageJobIsTheReleaseDryRun(t *testing.T) {
	t.Parallel()

	ci := readRepositoryFile(t, ".github/workflows/ci.yml")
	release := readRepositoryFile(t, ".github/workflows/release.yml")

	for _, message := range check.DryRunMirrorsRelease(ci, release) {
		t.Error(message)
	}
}

// apacheNotice is the NOTICE Apache-2.0 section 4(d) requires a release to ship
// with the work: who holds the copyright, and that the product includes
// software developed as shift2eu (issue #493).
const apacheNotice = `shift2eu
Copyright 2026 Timothy Mamo

This product includes software developed as shift2eu
(https://github.com/timothymamo/shift2eu).
`

// apacheLicenseSHA256 is the SHA-256 of the Apache License 2.0's official text,
// https://www.apache.org/licenses/LICENSE-2.0.txt, as apache.org publishes it.
// GitHub detects a repository's license from the unmodified text, so the root
// LICENSE must hash to it (issue #493).
const apacheLicenseSHA256 = "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30"

// TestTheProjectIsApacheLicensed pins issue #493: the project is licensed under
// the Apache License 2.0, and says so where the release and its image say it.
// The root LICENSE hashes to the official text's SHA-256, so it is the license
// unmodified and GitHub detects Apache-2.0 rather than a paraphrase of it; the
// root NOTICE holds exactly the attributions Apache-2.0 section 4(d) asks a
// work to carry, with its trailing newline; and the image's
// org.opencontainers.image.licenses label says Apache-2.0 too, so the image and
// the archive it ships beside tell a reader the same thing.
func TestTheProjectIsApacheLicensed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// file is the repository file the case reads.
		file string
		// exact, when set, is the whole file the case wants, its trailing
		// newline included.
		exact string
		// holds are the substrings the file must hold.
		holds []string
		// sha256Hex, when set, is the SHA-256 the whole file must hash to.
		sha256Hex string
	}{
		{
			name:  "the root NOTICE",
			file:  "NOTICE",
			exact: apacheNotice,
		},
		{
			name:      "the root LICENSE",
			file:      "LICENSE",
			sha256Hex: apacheLicenseSHA256,
		},
		{
			name:  "the image's license label",
			file:  "Dockerfile",
			holds: []string{`org.opencontainers.image.licenses="Apache-2.0"`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			contents := readRepositoryFile(t, tc.file)
			got := string(contents)

			if tc.sha256Hex != "" {
				sum := sha256.Sum256(contents)
				if h := hex.EncodeToString(sum[:]); h != tc.sha256Hex {
					t.Errorf("%s must be the unmodified text of apache.org's LICENSE-2.0.txt (issue #493), so GitHub detects Apache-2.0: sha256 %s, want %s", tc.file, h, tc.sha256Hex)
				}
			}

			if tc.exact != "" {
				if diff := cmp.Diff(tc.exact, got); diff != "" {
					t.Errorf("%s mismatch (-want +got):\n%s", tc.file, diff)
				}
			}

			for _, want := range tc.holds {
				if !strings.Contains(got, want) {
					t.Errorf("%s does not hold %q: want the project licensed under Apache-2.0 (issue #493)", tc.file, want)
				}
			}
		})
	}
}

// repositoryRoot returns the repository root as an absolute path, for the calls
// that run go list in it.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	return root
}

// sortedCopy returns a sorted copy of values, so a comparison of two sets from
// .golangci.yml does not depend on the order its lists are written in.
func sortedCopy(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)

	return out
}

// readRepositoryFile reads the file name under the repository root, and fails
// the test when it cannot: every test here reads what the release reads.
func readRepositoryFile(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return data
}
