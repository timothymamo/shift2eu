// Package lint holds rules on the repository's Go source that golangci-lint
// cannot express, written as plain tests, and the probe that proves a
// golangci-lint rule still fires. make lint runs them after golangci-lint, and
// as they carry no build tag, make test, make check and make cover (CI's Test
// job) run them too. It holds doc.go and _test.go files only, which the strict
// depguard rule test-docs-are-pure keeps so: the checker the shellcheck
// directive rule below runs lives in test/internal/shellcheck (issue #177).
//
// TestTrackedShellScriptsNameTheirShellcheckCodes holds every shell script git
// tracks (shellcheck.IsShellScript: a .sh file, or a sh or bash shebang, the
// hooks under .githooks among them) to the rule that a shellcheck disable
// directive names the codes it silences, each SC and four digits, never all,
// and gives its reason after a second "#" on the same line (issue #164), as
// nolintlint does for a Go nolint directive. shellcheck.CheckDirectives is the
// checker; make lint runs shellcheck and actionlint themselves.
//
// TestSourcesHideNothingFromDepguard reads every Go file the go command's
// ./... pattern reaches, whatever its build constraints, so every file make
// lint lints in any of its golangci-lint runs (native and one per release
// target), and fails on each way a file could hide code from depguard, which
// enforces the dependency rule (docs/architecture.md), or from forbidigo,
// which enforces the cloud-read-only rule (CLAUDE.md rule 1):
//
//   - A nolint directive that would hide a depguard or a forbidigo issue: one
//     that names depguard or forbidigo, one that names all, and one that names
//     no linter. nolintlint, configured in .golangci.yml, requires a directive
//     to name its linters and give a reason, but it cannot refuse one named
//     linter, and it lets some directives that silence every linter through,
//     such as //nolint: all with a space after the colon.
//   - A //go:build or // +build line that negates the e2e, integration,
//     browser or sitefront tag. Every lint run sets all four, so lint would
//     never read a file behind !e2e that a build without the tag compiles.
//   - A Go file that is a symbolic link: golangci-lint lints it with the
//     directives of its target, wherever that lies.
//
// The negated tags are those of run.build-tags in .golangci.yml, which
// TestNegatableTagsMatchGolangci keeps in step; the go command runs below take
// their -tags from the same list. test/architecture refuses the other ways:
// TestRepositoryHasNoLineDirectives fails on a //line directive, whose
// position depguard's file globs would match, and on any symlink git tracks or
// would add. Refusing symlinked Go files here too is deliberate defence in
// depth: this walk does not depend on git and also reaches files git ignores.
// Every test here that reads Go files, of the module or of a probe, reads them
// through this one walk, walkGoFiles, so each refuses a symlinked Go file.
// docs/architecture.md lists every way and its check in one place.
//
// TestCloudReadOnlyProbe runs the golangci-lint version CI pins, forbidigo and
// depguard, on probe source adapters over stub cloud SDKs
// (testdata/cloudreadonly), offline and with no credentials. Each probe line
// that ends in a want comment must be refused with the messages it names, no
// other line may be, and every entry of forbidigo's cloud-read-only group must
// refuse a line of its own, so a pattern that silently stops matching fails.
// The harness, runForbidigoProbe, serves any forbidigo group. Where
// golangci-lint is not on PATH (CI's Test job) it skips, unless
// SHIFT2EU_LINT_PROBES=require, which make lint and every entry of CI's
// lint-target matrix (the Lint check) set. Only CI fails on a golangci-lint
// version other than the pinned one; elsewhere the probe runs and logs a
// warning. A probe with no forbidigo tag runs depguard alone:
// TestAdaptersBuiltInCmdProbe (testdata/adaptersbuiltincmd) imports an adapter
// from each subtree of internal/adapter, and each subtree's root package, in
// packages outside the composition roots, and must be refused by
// adapters-are-built-in-cmd, so a deny entry shadowed by another, as in issue
// #119, fails. TestAppTestsUseFakesNotAdaptersProbe (testdata/apptestimports)
// imports, in an application-layer _test.go file, the standard packages that
// do file, network or process I/O, os and net among them and those that need
// neither (io/ioutil, text/template, debug/elf, runtime/debug, go/parser,
// mime/multipart, runtime/coverage, unsafe, ...), and packages on no list
// (encoding/xml, expvar) or pure but unneeded (runtime, go/build/constraint),
// and must be refused by the strict app-tests-use-fakes-not-adapters rule
// (issue #144), while every package its allow list names (bytes, errors,
// strings, context, testing, go-cmp, internal/testutil, ...) must not: the
// rule refuses everything its allow list leaves out, so the probe pins both
// the allow list and that no deny entry shadows an allowed package. fmt and
// time, which the allow list no longer names, must be refused as well. The
// probe runs twice: for the host with cgo on, and for darwin/amd64 with cgo
// off, a release target that is not the host's. TestTestutilImportsProbe
// (testdata/testutilimports) holds internal/testutil, production and _test.go
// files alike, to the strict testutil-imports rule (net/http and os/exec are
// refused, os and path/filepath for the golden files pass) and runs forbidigo's
// testutil-no-env group on env.go, whose environment reads, home, config,
// cache and temp lookups and os.StartProcess must each be refused (the writes
// are tests-no-process-state's since issue #153); TestTestutilNoEnvForbidigoPatterns
// pins that group's pattern against lists of refused and allowed names.
// TestCLIImportsProbe (testdata/cliimports) holds internal/cli to the strict
// cli-gets-adapters-from-cmd rule (issue #152): os/exec, syscall and unsafe are
// refused in its production files, the packages the CLI imports today pass; the
// forbidigo group cli-no-env-or-process refuses the CLI's environment reads,
// home, config, cache and temp lookups, os.StartProcess and, for its tests,
// which import syscall for syscall.Mkfifo, the roots' syscall names, on the
// internal/cli of TestCompositionRootForbidigoProbe's module (an aliased os
// import included), and TestCLINoEnvOrProcessForbidigoPatterns pins its
// patterns, the windows names offline.
// TestCLITestImportersProbe (testdata/clitestimporters) holds the lax
// cli-only-in-named-tests rule (issue #345): the test files of internal/cli
// and the wire package import internal/cli, and a test of
// internal/web, cmd/shift2eu, the site's glue or a package
// below internal/cli that imports it is refused; internal/web's imports
// internal/cli/sub too, for the rule's prefix deny entry. The rule's globs
// match any path that ends in the named directories, so test/architecture's
// TestOnlyNamedPackagesTestTheCLI holds the exact set of importers.
// TestVersionImportsProbe (testdata/versionimports) holds internal/version to
// the strict version-is-build-metadata rule: fmt passes, os and os/exec are
// refused. TestContractSuitesArePureProbe (testdata/contractsuites) holds the
// contract suites sourcetest and targettest to the strict
// contract-suites-are-pure rule (issue #152): net, os, os/exec, syscall and
// unsafe are refused, the packages they import today pass.
// TestInventoryFileNoEnvProbe (testdata/inventoryfile) runs the forbidigo group
// inventoryfile-no-env (issue #152) on the inventory-file reader, whose
// environment reads, home, config, cache and temp lookups and os.StartProcess
// must each be refused, and TestInventoryFileNoEnvForbidigoPatterns pins its
// pattern.
// TestE2ENoEnvProbe (testdata/e2enoenv) runs the forbidigo group e2e-no-env
// (issue #167) on the e2e tier, whose os and syscall environment reads, process
// starts and raw system calls, and (*exec.Cmd).Environ (issue #185), must each
// be refused while os/exec stays allowed, and TestE2ENoEnvForbidigoPatterns pins
// its patterns. TestE2EImportsProbe (testdata/e2eimports) runs depguard on the
// e2e tier (issue #185): a non-test file importing os/exec, unsafe or os/user is
// refused by test-docs-are-pure, and a _test.go file importing os/exec is not.
// An import list says nothing of a file that needs no import: an assembly file
// beside a bodyless Go func can issue a raw system call. So
// TestDocPackagesHoldNoNonGoSource (issue #194) refuses every non-Go source the
// Go toolchain builds beside a package (assembly, C, C++, Objective-C, Fortran,
// SWIG and .syso objects) in the trees test-docs-are-pure holds, which docTrees
// reads from the rule's files: list in .golangci.yml with test/internal/yamlread.
// The go command leaves testdata, _ and . directories out of ./..., and that
// exemption covers non-Go files only: a non-Go file there is no source of the
// held package. The check refuses every symlink in, or above, a held tree,
// whatever its name and wherever it sits, inside testdata, _ and . directories
// included: the walk (docEntries) reads the link, not what it points at, yet an
// import of its path follows it and the toolchain builds the source it finds
// there. A package under testdata or _x is still built when it is imported by
// path, which this check does not cover (the follow-up to #194).
// TestDocPackageNonGoSourceProbe runs the check on testdata/docsources/mod,
// whose test/e2e/probe_amd64.s must be refused and test/e2e/testdata copy must
// not, TestDocPackageSymlinkProbe on a symlinked directory it builds in a
// temporary directory, and TestDocSourceProblems and TestDocTrees pin its
// helpers.
//
// TestE2ETestsSetExecEnv and TestTestTreeNamesNoProcEnviron close what the
// e2e tier's two rules above cannot see (issue #195). The e2e _test.go files
// start children, so depguard cannot keep os/exec from them, and e2e-no-env
// refuses the one call that dumps the environment but no command built with a
// nil Env, which inherits all of it: the agents' keys and tokens live there,
// and the child's output can reach a t.Log call or a golden file.
// TestE2ETestsSetExecEnv runs execenv.CheckExecEnv over every _test.go file
// under test/e2e, walked recursively so a sub-package the tier grows is
// scanned too, testdata directories skipped. The rule fails closed: apart
// from the four gaps below, a shape it cannot follow is a finding. It follows
// a command only when exec.Command or exec.CommandContext builds it as the
// whole right-hand side of a declaration in a function (`ident :=` or
// `var ident =`), and refuses every other construction, exec.Command or
// exec.CommandContext used as a function value, a followed name rebound to
// anything else, every use of the command before its Env is set (a run call,
// an alias, an argument, a method value or expression, a capture in a
// function literal), an Env assigned, through any receiver or as an Env key
// of any composite literal, a value it cannot prove is not nil, an Env
// assigned after the command was aliased, stored, passed, captured or
// deferred, the address of an Env field, every exec.Cmd type expression not
// under a `*`, a *exec.Cmd anywhere but as the whole type of a struct field,
// a parameter, a result, a var, a type assertion or a type-switch case, an
// embedded *exec.Cmd, an exec.Cmd composite literal without a proven Env, a
// generic function, a blank or dot import of os/exec, and, beside os/exec, an
// import of reflect or unsafe, one finding per import line. No array, slice,
// map or chan element type is a *exec.Cmd, so no elided literal can build one;
// a command reaches a struct field only through a use the check vouches for.
// The proven Env values are a composite literal, a make, an append of one, and
// a call to a helper of the same file, with no named result and no defer, that
// returns one. The exemption the test declares (e2eToolchainCommands) is keyed on the
// name and the subcommand, both literals: `go build` and `go list`, which need
// that environment and print none of it, and only while every literal flag
// after the subcommand is one of the allow-list e2eToolchainFlags (-trimpath,
// -o and -deps). A run chained to its construction is never exempt, and a
// spread argument and a Path or Args assigned on the command take the
// exemption away too. Four gaps are accepted and left to review: the rule
// follows the source, not the flow (an Env assigned in a branch that never
// runs, or in a closure, counts); a path built at run time; a command built in,
// or handed to, another file or package (a decoder such as json.Unmarshal, or
// a helper of a sibling file that imports reflect but not os/exec, writing Env
// through a command that escaped into it); and a non-literal toolchain
// argument that holds a flag, alongside which a Dir set on an exempt command
// keeps the exemption, the go command then reading that directory's go.mod
// and go.work.
// TestTestTreeNamesNoProcEnviron runs execenv.CheckEnvironLiterals over every
// Go file under test/, testdata directories included and no file left out,
// which reads the same environment without a child at all, in two tiers:
// under test/e2e, which holds no fixture literals, a literal with a proc or
// dev/fd path segment is refused (execenv.EnvironmentPathIn, the matcher it
// shares with TestRootsAndCLINameNoEnvironmentPath); elsewhere a literal whose
// cleaned value is a /proc/.../environ path is. The matcher's own test reads
// the paths it pins from a JSON file under test/internal/execenv/testdata. A
// path built at run time (filepath.Join, concatenation, fmt.Sprintf,
// os.DirFS("/proc").Open) is beyond a rule of literals and left to review, and
// in test/e2e to forbidigo's e2e-no-env. Both checkers and their probes are in
// test/internal/execenv, whose probes are .go.txt files, so this scan never
// meets a refused literal it planted itself.
// TestTestsNoProcessStateProbe (testdata/testsprocessstate) runs the forbidigo
// group tests-no-process-state (issue #153) on t.Setenv, t.Chdir, os.Setenv,
// os.Unsetenv, os.Clearenv, os.Chdir, syscall.Unsetenv, syscall.Clearenv and
// syscall.Fchdir, and on a struct that embeds
// *testing.T, in the application layer, the test helpers, the driving
// adapters, the roots, a source adapter, the domain and the e2e tier, where
// each must be refused, while t.TempDir, os.MkdirTemp and os.CreateTemp pass;
// its first entry matches any Setenv or Chdir whatever the receiver.
// TestTestsNoProcessStateForbidigoPatterns pins its patterns.
// forbidigo renders no call through an interface literal or a type
// parameter, so TestTestsChangeNoProcessStateByNoSelector refuses every
// selector named Setenv, Chdir, Unsetenv, Clearenv or Fchdir in the group's
// scope, every file but those of test/lint and test/architecture, on the AST,
// and
// TestProcessStateSelectorFindings pins that check on fixtures.
// TestCloudReadOnlyPatterns and
// TestCloudReadOnlyPackages check the group's allowlist patterns, read from
// .golangci.yml, against lists of write and read names and of import paths,
// without golangci-lint. TestForbidigoExclusionsDropOneGroup checks every
// exclusion rule that drops forbidigo issues: it names forbidigo alone, its
// text matches only one group's quoted msg, and the rules of each group are
// exactly those it states: none for cloud-read-only, and for domain-is-pure
// one path and one path-except. Two groups hold the composition root
// cmd/shift2eu (issue #111): composition-root-no-exec
// refuses starting a process and dumping the environment (os.StartProcess,
// os/exec, syscall.Exec, ForkExec, StartProcess, os.Environ, syscall.Environ),
// the windows entry points (syscall.CreateProcess, CreateProcessAsUser,
// GetEnvironmentStrings, LoadDLL, MustLoadDLL, NewLazyDLL, LoadLibrary,
// GetProcAddress), the raw system calls (syscall.Syscall,
// Syscall6, 9, 12, 15, 18 and N, RawSyscall, RawSyscall6) and syscall.SYS_*,
// and any use of unsafe; composition-root-env refuses reading a named variable
// (os.Getenv, os.LookupEnv, os.ExpandEnv, syscall.Getenv,
// syscall.GetEnvironmentVariable) and, since issue #152, locating a home,
// config, cache or temp directory (os.UserHomeDir, os.UserConfigDir,
// os.UserCacheDir, os.TempDir). The probe lints the roots natively and for
// windows/amd64. Those names are defence in depth: the comprehensive guard is
// TestCompositionRootsUseSyscallOnlyForSignals, an AST check under which the
// roots, tests included, use package syscall for a ^SIG[A-Z0-9]+$ constant and
// the type syscall.Signal only, and TestRootsAndCLILinkNoForeignSymbol
// refuses //go:linkname in them, in the driving adapter internal/cli and in
// the inventory-file reader, and
// TestRootsAndCLINameNoEnvironmentPath refuses a literal holding the path
// segment proc or dev/fd, at its start or after a /, with or without a
// trailing /, but not myproc, in the root, the driving adapter and the
// inventory-file reader (proc/self/environ read through os.DirFS("/") has no
// leading slash, and os.DirFS("/proc") no trailing one); no group can state
// either.
// The depguard rule composition-root-tests refuses go/build, net/http/cgi,
// os/exec, os/user, plugin, runtime/cgo, syscall/js and unsafe in the roots'
// _test.go files, which the strict composition-root rules skip, and in the
// driving adapters' _test.go files; TestCompositionRootTestsReachNoForeignCodeProbe
// runs it on a probe module (testdata/compositionroottests). TestCompositionRootExclusions
// pins their one path-except each and that neither group has an exception;
// TestCompositionRootForbidigoPatterns and TestCompositionRootForbidigoProbe
// pin their entries. It reads linters.exclusions.rules with
// test/internal/yamlread, the strict line reader test/architecture reads
// .golangci.yml with, so a rule written in a way it does not follow fails.
//
// TestNoTrackedSavedPages refuses every file git tracks that is a saved web
// page or part of one and that the allow list allowedSavedPages does not name
// (issue #212): a file whose final extension, compared in any case, is .html,
// .htm, .xhtml, .xht, .shtml, .mhtml, .mht or .webarchive, or a file under a
// directory whose name ends in _files, in any case, the asset directory a
// "Webpage, Complete" save writes. A saved price page, a browser copy of a
// target's pricing page kept because the site refuses automated clients, is
// read from a scratch directory outside the repository and never committed
// (internal/adapter/target/upcloud/SOURCES.md). The rule reads git ls-files,
// so files git ignores are never read. savedPageProblems is the checker, and
// TestSavedPageProblems pins it.
//
// TestNoTrackedFileSpellsTheInlineAllowComment (inlineallow_test.go, PRD #175
// milestone 2, ADR 0040) refuses every file git tracks that holds gitleaks'
// inline allow comment, the tool's name followed by a colon and the word
// allow, in any letter case. gitleaks skips a line holding it, so the
// scheduled audit's publish program refuses a run whose report holds it; the
// model reads this repository, and a tracked line it quoted would refuse the
// audit's own run. A test that feeds a scanner the marker builds it in code
// from its two parts, as the rule itself does.
//
// TestLintLoadsEveryGoFile and TestLintVisitsEveryImportedPackage keep every
// Go file within lint's reach. Lint loads, for the host and every release
// target of .goreleaser.yaml, what builds with the tags e2e and integration,
// and ./... skips testdata, _ and . directories, so a file behind another build
// tag, or an imported package in a skipped directory, would run unchecked, with
// real credentials in an integration test. The tests run the go command
// offline. TestLintLoadsEveryGoFile reads the release targets from
// .goreleaser.yaml with test/internal/releasetarget, the reader
// test/architecture holds the release to, and counts a file as loaded when the
// go command loads it for any of them with cgo off and the baseline GOAMD64
// and GOARM64 levels, as the release builds them; a file only for one release
// platform passes, and a platform the release gains is one the test holds
// files to. A file only a cgo build compiles is refused on purpose: the
// release builds with CGO_ENABLED=0, and only the release targets count
// toward loaded, not make lint's native run with cgo on, which the release
// never ships. TestUnloadedGoFilesProbe pins
// its helper on a probe module (testdata/platformprobe).
//
// TestNoInterfaceDeclaresACloudWrite and TestNoTypeHidesACloudSDKType close
// what forbidigo's cloud-read-only group cannot see. An interface a cloud
// source adapter declares, generic constraints and interface literals
// included, may declare only read verbs or unexported methods, and an interface
// elsewhere no method named for a write; no struct embeds an SDK type.
// TestInterfaceFindingsProbe runs the interface rule on the probe, and
// TestWriteVerbsMatchForbidigo keeps the write verbs the same here and in
// .golangci.yml.
//
// TestBranchCoAuthorTrailers (coauthortrailer_rule_test.go, issue #663) reads
// the branch's own commits with git log, the range coAuthorRange picks
// (SHIFT2EU_COAUTHOR_RANGE, or origin/main..HEAD, or a skip), and fails on
// every line that starts with Co-Authored-By: (spaces or tabs allowed before
// the colon, as git accepts them) and carries an address
// .github/co-author-addresses.txt does not list, or a malformed one: GitHub
// credits that address's account, and a squash merge copies the trailers onto
// main. A commit's author and committer address must be on the list too, since
// a squash merge credits every distinct author, so a commit authored with a
// third-party address fails with no trailer at all. CI's coauthor-trailers
// job, part of the Lint check, passes the pull request's base..head over a full
// clone with SHIFT2EU_COAUTHOR_CHECK=require, so it never skips there and fails
// a range that reads no commit (coAuthorCommitCount); every other run on a pull
// request's shallow checkout skips. TestCoAuthorFindings,
// TestParseCoAuthorAllowList, TestCoAuthorRange, TestCoAuthorCommitCount and
// TestCoAuthorAllowListFile pin its helpers and the list.
//
// TestOpenTofuRequiredVersionRuleStripsTheV (renovateopentofu_test.go, issue
// #434) reads .github/renovate.json5 as JSON5, with the reader of
// aireviewrenovate_test.go, and pins that the packageRule pointing
// infra/site/versions.tf's required_version at OpenTofu's releases sets an
// extractVersion that strips the tag's v, so Renovate proposes 1.14.0, the
// format required_version and dev/infra-check.sh want, not v1.14.0.
package lint
