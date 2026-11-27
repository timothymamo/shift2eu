// Package architecture holds the architecture checks that depguard cannot make
// (issue #40, ADR 0012). depguard reads the imports of the Go files
// golangci-lint analyses and picks the rule for a file by its file name, so a
// file name that does not say where the code is gets past it: an assembly file
// that a bodyless Go declaration calls, a line directive that names another
// file, and a symlinked package directory, which lint checks under its real
// path.
//
// The package holds only this file and the repository tests. They run the
// checks of internal/check against this repository: the platforms
// .goreleaser.yaml builds and how it publishes the GitHub release, the strict depguard rules of .golangci.yml, the
// packages go list reports for the layers and for the release binary, and
// every Go file under the root. The code lives in internal/check, so no
// package outside test/architecture can import it. The release platforms are
// read by test/internal/releasetarget and the YAML by test/internal/yamlread,
// which test/lint shares, so both packages read one list with one reader.
//
// Two of the tests hold the lint runs to the release platforms (issue #39).
// TestMakeLintRunsEveryReleaseTarget runs make lint with a fake golangci-lint
// and compares the runs it records with .goreleaser.yaml: one per release
// target with cgo off and one native run, each with a cache directory of its
// own and only the build settings make lint sets.
// TestCILintMatrixCoversEveryReleaseTarget reads the top level, the
// lint-target matrix and the Lint aggregator of .github/workflows/ci.yml as a
// closed schema, pins the golangci-lint step to make lint's build settings and
// the aggregator's script whole, and compares the matrix with .goreleaser.yaml.
// It pins the coauthor-trailers job the aggregator needs step by step, in
// order (issues #663 and #689): each step's keys are a closed set, its with:,
// env: and run: are pinned line by line, and every ${{ expression stays in an
// env: block, setup-go's go-version-file excepted.
//
// TestDepguardRulesArePinned pins every depguard rule of .golangci.yml, strict
// or not, against the copy pinnedDepguardRules: its list-mode, its files and
// allow lists and the pkg of each deny entry (issue #80). A merge conflict
// resolution once dropped the reporters-stay-offline rule and the exact
// jsondoc$ allow entry, and no check noticed. Adding, removing or changing a
// rule now means updating pinnedDepguardRules in the same commit, on purpose,
// where review sees it. TestDepguardPinCatchesMutations proves the pin
// notices, with 736 mutations of the real file: for every rule it deletes the
// rule and each of its files, allow and deny entries (every deny entry, a
// shared list's too), drops the trailing $ of each exact allow entry, adds an
// allow entry os and a negated files glob, and rewrites, deletes or adds the
// list-mode. A self-check fails if the walk misses an entry, and a document the
// reader refuses counts as caught only for the one mutation expected to break
// it: deleting app-uses-ports-not-adapters, which removes the &app-deny anchor
// a later alias reads.
//
// TestDepguardEntriesShadowNothing fails on an allow or deny entry without $
// that is a strict prefix of another entry of the same list: depguard compares
// an import only with the entry that sorts nearest before it, so the shorter
// entry decides nothing under the longer one (issue #119, where
// adapters-are-built-in-cmd reported nothing). TestShadowedEntries pins its
// helper. TestAdaptersBuiltInCmdNamesEveryAdapterSubtree fails on a directory
// under internal/adapter that adapters-are-built-in-cmd does not name by a deny
// entry of its own, or a file placed there directly, so a new adapter subtree
// cannot escape the rule.
//
// TestOnlyNamedPackagesTestTheCLI holds the lax rule cli-only-in-named-tests to
// the exact set it means (issue #345): depguard matches its exemptions, such as
// !**/internal/cli/*_test.go, against absolute paths with no anchor at the
// module root, so a directory whose path ends in the same segments would be
// exempt too. The test lists ./... natively with no tag and with every tag of
// run.build-tags in .golangci.yml, on windows with those tags and on js/wasm
// with and without them. It fails, naming the build, on a package outside
// internal/cli and cmd/shift2eu-web/internal/wire whose test files
// (TestImports, XTestImports) import internal/cli or a package below it in
// that build, and on a package of those two that does so in no build: the
// must-import direction is checked once, over the union of the builds, so a
// parity test behind a build constraint is no stale exemption (issue #382).
// check.CLIImporterProblems makes that decision, and TestCLIImporterProblems
// and TestCLITestImportersOf pin it and the helper.
//
// TestDepguardDescsKeepTheirWholeMessage fails on a depguard desc: in
// .golangci.yml that is a plain, unquoted YAML scalar holding " #", which YAML
// would cut at the # as a comment (issue #382).
//
// TestGolangciSettingsHideNoIssue covers what the pin does not: whether the
// rules run and what lint reports (issue #81). It fails, naming the setting, on
// linters.exclusions.paths or paths-except, a linters.exclusions.generated that
// is missing or not disable, an issues cap or filter, a run.issues-exit-code
// outside 1..125 (os.Exit truncates it to 8 bits, so 0, 256 or -256 exits 0),
// linters.default other than standard or all, run.tests not true, depguard or
// forbidigo missing from linters.enable or in linters.disable, and a
// linters.exclusions.rules entry naming depguard, naming forbidigo without a
// text or with an empty one, or naming no linter. The document's shape is an
// allow list that fails closed: version must be "2", and a key, a default or a
// preset the check does not know is refused. TestRepositoryReadsOnlyGolangciYML
// refuses a .golangci.json, .golangci.toml or .golangci.yaml at the root that
// git could commit, which golangci-lint would read in place of .golangci.yml.
// The golangci-lint command lines are pinned already, so neither can point
// lint at another config: TestMakeLintRunsEveryReleaseTarget wants exactly
// run --allow-serial-runners, and TestCILintMatrixCoversEveryReleaseTarget
// wants only version and skip-cache in the golangci-lint step's with: block.
//
// app-tests-use-fakes-not-adapters, the strict allow list of the application
// layer's _test.go files, is no skip either (issue #144): RuleDirs reads its
// pair of test-file globs as the test files of internal/app, and
// TestReleaseTargetDependencies holds the test imports of every package there
// to its allow list on every release platform. cli-gets-adapters-from-cmd, the
// strict allow list of internal/cli's production files, is no skip since issue
// #152, which replaced its $gostd with exact entries: the check holds the CLI's
// imports on every release platform. version-is-build-metadata, the strict allow
// list of the build metadata, is read the same way: TestVersionRuleIsBuildMetadata
// pins its shape and TestReleaseTargetDependencies holds internal/version's
// imports to it, while the CLI's release listing must name the package.
//
// TestCloudAdapterSDKRuleIsChecked pins that the SDK-only rule,
// cloud-adapters-reach-the-cloud-through-the-sdk, is no skip (issue #79): the
// check reads its SDK-root prefix entries, the two directories it excludes and
// its test files, and TestCloudAdapterSDKRuleHoldsItsProbeForEveryReleaseTarget
// fails a fixture adapter that imports net/http on every release platform.
// TestCloudAdapterSDKRuleHoldsItsSDKProbeForEveryReleaseTarget passes a fixture
// adapter that imports a stand-in SDK: the SDK package and what it imports are
// third-party code an allowed import reaches, which depguard never lints, so
// the check exempts them, while a third-party package that a package no
// strict rule holds imports directly, or that no allow list reaches, is still
// reported. The exemption reads the strict allow lists only: the lax deny
// rules (go-cmp-only-in-tests, cloud-sdks-only-in-provider-adapters,
// composition-root-tests) are held by depguard in make lint alone. A third-party import in internal/cli is
// refused by depguard alone, whose strict rule there is an allow list of exact
// entries and named module packages (issue #152, which took its $gostd away so
// the rule is no skip). One in internal/version is refused by depguard alone too:
// version-is-build-metadata, a strict allow list of fmt and nothing else, because
// the driving adapters may import the build metadata and no rule held what it does
// (the same issue). One in cmd/shift2eu is refused by the strict rule
// composition-root (issue #101), which allows the module path as its one prefix
// entry and the standard packages cmd/shift2eu imports today, each exact:
// TestCompositionRootRuleIsChecked pins its shape and that it is a reasoned
// skip of the shared layer listing, since go list -deps ./cmd/shift2eu/... also
// lists internal/cli, internal/version and the adapters, which each stand in a
// dependency tree the shared layer listing does not read.
// TestCompositionRootIsHeldToItsRuleForEveryReleaseTarget holds the real
// cmd/shift2eu to it on every release platform, with cgo off and on and every tag
// set TestReleaseTargetDependencies reads (cgoModes and tagSets, shared so the
// two cannot drift), and fails any package of its listing under the module path
// that does not belong to the main module: the rule's prefix entry would let a
// module nested under that path in (the security audit's LOW-1).
// TestCompositionRootRuleHoldsItsProbeForEveryReleaseTarget fails a fixture
// cmd/shift2eu that imports a third-party library, a stand-in SDK and a nested
// module under the fixture's own module path, which the main-module assertion
// must report, while the rule holds a sibling fixture, cmd/shift2eu-web, not at
// all. TestCompositionRootRulesAreChecked pins the rule's shape, and
// TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget holds the
// real cmd/shift2eu to it on every release platform.
// The root allows every module package through that prefix, so it can
// hand a call it may not make to any of them. Two properties close the class
// (issue #152), over every package go list reports for every release target
// with the e2e and integration tags: TestEveryModulePackageIsHeldByAStrictRule
// fails for a non-test module package a root can import that no strict
// depguard rule holds (only a package under an internal directory the roots
// cannot import is left out); TestEveryOsAllowingRuleHasAnEnvironmentGroup fails
// for a package whose strict rule allows os but which no forbidigo group in
// scope holds to the nine os reads (Getenv, LookupEnv, ExpandEnv, Environ,
// StartProcess, UserHomeDir, UserConfigDir, UserCacheDir, TempDir), and since
// issue #167 for one whose rule allows syscall but is not held to the syscall
// reads (Getenv, Environ, Exec, ForkExec, StartProcess and the raw system
// calls), $gostd counting as allowing both; and
// TestNoPackageIsHeldOnlyByAGostdRule fails for such a package whose only
// holding strict rules allow $gostd, and so os/exec, syscall and unsafe, unless
// gostdOnlyExempt (empty) names it with a reason. That last one found test/e2e:
// its doc.go carries no build tag, and e2e-is-black-box allows $gostd, so
// test-docs-are-pure holds it as well. TestStrictRuleCoverageHolds pins their
// helpers. The properties brought three strict rules. config-is-pure and
// target-docs-are-pure hold internal/config and the doc-only
// internal/adapter/target to an allow list of nothing, allow: [], and the one
// deny entry, os, as the config-is-pure comment in .golangci.yml explains
// (issue #166). contract-suites-are-pure holds sourcetest and targettest,
// which any _test.go file may import, to what they import today.
// The exemption trusts the import path, which is sound because nothing can
// substitute other code for an allowed path: TestModulesHaveNoReplace reads
// the root go.mod, and every other tracked one outside testdata, with the go
// command's own parser (go mod edit -json) and finds no replace, and TestRepositoryHasNoWorkspaceOrVendor finds no go.work,
// go.work.sum or vendor directory. TestOnlyNamedRulesHoldPrefixEntries keeps
// prefix entries, which must end in /, to that rule and to composition-root,
// whose one prefix entry is the module path.
//
// TestScriptsRunDirectlyAreExecutable fails on a script under dev/ or
// .github/scripts/, a file whose staged content starts with #!, that is not
// 100755 in the git index: the Makefile runs the scripts under dev/ directly,
// and the rename in #117 lost dev/claude-settings.sh's executable bit without
// any check noticing (issue #125); CI runs
// .github/scripts/release-please-dry-run.sh directly (issue #142). It reads
// mode and content from the index, not the checkout's file system.
// CI runs .github/scripts/verify-archives.sh directly too (issue #147).
//
// TestReleaseArchivesHoldOneBinary holds .goreleaser.yaml to one archive per
// binary, each with its own id and name and one checksums.txt for all
// (check.GoReleaserArchives), and ci.yml's test job to installing the
// GoReleaser release.yml runs, building those archives with it and checking
// each holds exactly its own binary (check.DryRunVerifiesArchives), issue #147.
//
// TestTheProjectIsApacheLicensed holds the project to the Apache License 2.0
// (issue #493, ADR 0041): the root NOTICE word for word, the root LICENSE
// hashing to the SHA-256 of apache.org's official LICENSE-2.0.txt, so GitHub
// detects Apache-2.0 rather than a paraphrase, and the image's
// org.opencontainers.image.licenses label saying Apache-2.0.
//
// TestCompositionRootRulesAreHeldToTheirRuleForEveryReleaseTarget also holds
// cmd/shift2eu, on every release target, cgo mode and
// tag set and on the same go list -deps listing as its main-module check, to
// reaching no package of the module whose path below the module path holds an
// element the go command skips when it walks ./...: testdata, or one starting
// with _ or a dot (check.HiddenPathPackages, check.HiddenPathMessages, issue
// #206). The go command still builds such a package when another imports it
// by explicit path, and depguard and test/lint walk the tree as the go command
// does, so no rule would see it reach a release binary. Each finding says
// "<root> reaches <pkg>", since the listing holds transitive imports too.
// TestHiddenPathProbeIsRefusedForEveryReleaseTarget proves the check fires on
// testdata/hiddenpathprobe, whose cmd/shift2eu imports testdata/x, _x and .x.
// A symlinked directory is another spelling ./... skips, and this check does
// not close it: TestRepositoryHasNoLineDirectives refuses every symlink git
// tracks or would add, and TestReleaseTargetDependencies (check.Sources)
// requires every module package a release binary is built from to sit in the
// directory its import path names, symlinks resolved. A change to either check
// would reopen issue #206.
//
// The checks are tests: they run in make test, make check and CI's make cover,
// with no workflow of their own. They need no credentials, and no network
// beyond the go command's own: TestNoProductBinaryReachesChromedp lists the
// browser-tagged test/browser, which go test ./... never builds, so on an empty
// module cache that listing fetches chromedp through GOPROXY, verified by
// go.sum, as the go test build fetches every other module (issue #391).
package architecture
