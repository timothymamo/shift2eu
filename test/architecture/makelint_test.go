package architecture_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/releasetarget"
	"github.com/timothymamo/shift2eu/test/internal/shellcheck"
)

// fakeLinter is the golangci-lint make lint finds on PATH. It records the
// environment and the arguments of every call it gets, one tab-separated line
// per call, and fails only for the run a case names: a run of one target can
// then be made to fail without stopping the runs before or after it, which is
// what tells a lint that tries every target from one that gives up on the first.
// SHIFT2EU_FAKE_LINT_FAIL names a goos/goarch pair or, for the one run with cgo
// on, `native`.
const fakeLinter = `#!/bin/sh
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "${GOOS}" "${GOARCH}" "${CGO_ENABLED}" "${GOLANGCI_LINT_CACHE}" "${GOFLAGS}" "${GOEXPERIMENT}" "${GOAMD64}" "${GOARM64}" "${GOWORK}" "$*" >> "${SHIFT2EU_FAKE_LINT_RECORD}"
if [ "${SHIFT2EU_FAKE_LINT_FAIL}" = "native" ] && [ "${CGO_ENABLED}" != "0" ]; then
	exit 3
fi
if [ "${GOOS}/${GOARCH}" = "${SHIFT2EU_FAKE_LINT_FAIL}" ]; then
	exit 3
fi
exit 0
`

// fakeGo is the go make lint finds on PATH. A go test call, the test/lint step
// make lint ends with, is recorded in the same record as the linter runs, as a
// line "#go\t<GOOS>\t<GOARCH>\t<CGO_ENABLED>\t<SHIFT2EU_LINT_PROBES>\t<arguments>", so the record
// tells whether it ran and whether it ran after them; it fails when
// SHIFT2EU_FAKE_LINT_FAIL is test/lint. A go tool call, the actionlint step, is
// recorded as "#tool\t<GOOS>\t<GOARCH>\t<CGO_ENABLED>\t<SHELLCHECK_OPTS>\t<arguments>",
// SHELLCHECK_OPTS as unsetShellcheckOpts when it is not set, since actionlint's
// shellcheck would inherit it, and fails when
// SHIFT2EU_FAKE_LINT_FAIL is actionlint. Every other call is handed to the real go
// command, whose path replaces %s, single-quoted.
const fakeGo = `#!/bin/sh
if [ "$1" = tool ]; then
	printf '#tool\t%%s\t%%s\t%%s\t%%s\t%%s\n' "${GOOS}" "${GOARCH}" "${CGO_ENABLED}" "${SHELLCHECK_OPTS-<unset>}" "$*" >> "${SHIFT2EU_FAKE_LINT_RECORD}"
	if [ "${SHIFT2EU_FAKE_LINT_FAIL}" = "actionlint" ]; then
		exit 3
	fi
	exit 0
fi
if [ "$1" = test ]; then
	printf '#go\t%%s\t%%s\t%%s\t%%s\t%%s\n' "${GOOS}" "${GOARCH}" "${CGO_ENABLED}" "${SHIFT2EU_LINT_PROBES}" "$*" >> "${SHIFT2EU_FAKE_LINT_RECORD}"
	if [ "${SHIFT2EU_FAKE_LINT_FAIL}" = "test/lint" ]; then
		exit 3
	fi
	exit 0
fi
exec %s "$@"
`

// wantLintTestArgs is the whole argument list of the go call make lint ends
// with: the test/lint tests, uncached.
const wantLintTestArgs = "test -count=1 ./test/lint/"

// fakeShellcheck is the shellcheck make lint finds on PATH. It records each
// call as a line "#shellcheck", a tab and SHELLCHECK_OPTS (unsetShellcheckOpts
// when it is not set), then a tab and each argument, and fails when
// SHIFT2EU_FAKE_LINT_FAIL is shellcheck.
const fakeShellcheck = `#!/bin/sh
{ printf '#shellcheck\t%s' "${SHELLCHECK_OPTS-<unset>}"; printf '\t%s' "$@"; printf '\n'; } >> "${SHIFT2EU_FAKE_LINT_RECORD}"
if [ "${SHIFT2EU_FAKE_LINT_FAIL}" = "shellcheck" ]; then
	exit 3
fi
exit 0
`

// unsetShellcheckOpts is what the fakes record for SHELLCHECK_OPTS when it is
// not set. shellcheck puts that variable's words before its own arguments, and
// --norc does not stop it, so an --exclude there would silence a check in a
// developer's make lint and not in CI: the lint scripts unset it (issue #164).
const unsetShellcheckOpts = "<unset>"

// wantActionlintArgs is the whole argument list of the go call that runs
// actionlint when shellcheck is the shellcheck on PATH: the version tools/go.mod
// pins, a module of its own (ADR 0022), on .github/workflows, its default, with
// shellcheck named by the path dev/actionlint.sh found. Given a shellcheck it
// cannot run, actionlint switches its shellcheck check off and still passes, so
// the script resolves the path itself and fails without one.
func wantActionlintArgs(shellcheck string) string {
	return "tool -modfile=tools/go.mod actionlint -shellcheck=" + shellcheck
}

// wantShellcheckFlags are the arguments make lint hands shellcheck before the
// scripts: no .shellcheckrc, which could silence a check on one host and not
// in CI; style, every finding, as SC2086 (word splitting) is only info; and
// "--", so no script name is read as an option (issue #164).
var wantShellcheckFlags = []string{"--norc", "--severity=style", "--"}

// goToolRun is one go tool call make lint made, as the fake go recorded it.
type goToolRun struct {
	// After is how many golangci-lint runs the record held before it.
	After          int
	GOOS           string
	GOARCH         string
	CGOEnabled     string
	ShellcheckOpts string
	Args           string
}

// shellcheckRun is one shellcheck call make lint made.
type shellcheckRun struct {
	// After is how many golangci-lint runs the record held before it.
	After          int
	ShellcheckOpts string
	Args           []string
}

// The two ways a case sets GOLANGCI_LINT_CACHE for make.
const (
	// cacheSet hands make a directory of the test's own, so every run's cache
	// directory must sit directly under it.
	cacheSet = "set"
	// cacheUnset leaves the variable out of make's environment, so make falls
	// back to whatever the host offers and the runs only have to keep apart.
	cacheUnset = "unset"
)

// What every run of golangci-lint make lint makes must be called with and must
// see. Neither may come from the host: make lint runs seven times in a row and
// several worktrees lint at once on one machine, and a developer's own build
// settings would otherwise decide which files a run type-checks.
const (
	// wantArgs is the whole argument list of a run. golangci-lint takes a
	// global lock and, by default, exits with an error when another
	// golangci-lint holds it, so a run must wait for the lock instead of
	// failing; no other flag may override the build tags .golangci.yml sets.
	wantArgs = "run --allow-serial-runners"
	// wantGOFLAGS is the whole GOFLAGS of a run: a developer's GOFLAGS,
	// exported or in the go env file, could add a build tag and take files
	// out of lint.
	wantGOFLAGS = "-mod=readonly"
)

// goTestRun is one go test call make lint made, as the fake go recorded it.
type goTestRun struct {
	// After is how many golangci-lint runs the record held before it.
	After      int
	GOOS       string
	GOARCH     string
	CGOEnabled string
	Probes     string // SHIFT2EU_LINT_PROBES
	Args       string
}

// lintRun is one call of golangci-lint make lint made, as the fake recorded it.
type lintRun struct {
	GOOS         string
	GOARCH       string
	CGOEnabled   string
	Cache        string
	GOFLAGS      string
	GOExperiment string
	GOAMD64      string
	GOARM64      string
	GOWORK       string
	Args         string
}

// TestMakeLintRunsEveryReleaseTarget pins make lint against the release: one
// golangci-lint run per goos/goarch pair .goreleaser.yaml builds with cgo off,
// plus one native run with cgo at the host's default, each with a cache
// directory of its own, each waiting for golangci-lint's lock and each seeing
// only the build settings make lint sets. A file behind another GOOS, another
// GOARCH or `//go:build !cgo` is only type-checked, and so only linted, by the
// run for the platform it builds on; without these runs such a file ships with
// every depguard rule unseen (issue #39).
func TestMakeLintRunsEveryReleaseTarget(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	goreleaser, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}

	// .goreleaser.yaml is the one source of the release platforms: the test
	// reads the list from it rather than pinning a copy here, so a target added
	// to the release is a target make lint must gain without this test changing.
	targets, err := releasetarget.Read(goreleaser)
	if err != nil {
		t.Fatalf("releasetarget.Read(.goreleaser.yaml): %v", err)
	}

	if len(targets) == 0 {
		t.Fatal("releasetarget.Read(.goreleaser.yaml) returned no target, so this test would pin nothing")
	}

	// The native run lints with cgo at the host's default. With cgo off, it
	// is a CGO_ENABLED=0 run, which this test would count as a release run and
	// report as a confusing target diff, so say what is wrong instead.
	if cgo := hostCGO(t, root); cgo != "1" {
		t.Fatalf("cgo is off on this host (go env CGO_ENABLED = %q, as make lint sees it), so make lint's native run cannot lint the cgo build make test's -race compiles: install a C compiler, or set CC to one", cgo)
	}

	// wantRelease is the goos/goarch of every release target, sorted: what the
	// runs with CGO_ENABLED=0 must be, once each.
	wantRelease := make([]string, 0, len(targets))
	for _, target := range targets {
		wantRelease = append(wantRelease, target.GOOS+"/"+target.GOARCH)
	}

	slices.Sort(wantRelease)

	scripts := trackedShellScripts(t, root)

	tests := []struct {
		name string
		// cache is cacheSet or cacheUnset: how the case gives make a
		// GOLANGCI_LINT_CACHE.
		cache string
		// fail is the goos/goarch the fake linter fails on, `native` for the
		// one run with cgo on, empty for none.
		fail string
		// env holds further variables to hand make on top of the ones the
		// test sets itself, exported as they would be from a developer's
		// shell: make lint must not let any of them change a run.
		env map[string]string
		// wantExit is whether make lint must succeed.
		wantExit bool
	}{
		{
			name:     "every release target and one native run",
			cache:    cacheSet,
			wantExit: true,
		},
		{
			name:     "the cache directories are distinct without GOLANGCI_LINT_CACHE",
			cache:    cacheUnset,
			wantExit: true,
		},
		{
			name:     "a failing target fails make lint after every run",
			cache:    cacheSet,
			fail:     "windows/amd64",
			wantExit: false,
		},
		{
			name:     "a failing native run fails make lint after every run",
			cache:    cacheSet,
			fail:     "native",
			wantExit: false,
		},
		{
			name:     "a failing test/lint fails make lint",
			cache:    cacheSet,
			fail:     "test/lint",
			wantExit: false,
		},
		{
			name:     "a failing shellcheck fails make lint after every run",
			cache:    cacheSet,
			fail:     "shellcheck",
			wantExit: false,
		},
		{
			name:     "a failing actionlint fails make lint after every run",
			cache:    cacheSet,
			fail:     "actionlint",
			wantExit: false,
		},
		{
			name:     "an exported platform and build settings change nothing",
			cache:    cacheSet,
			wantExit: true,
			env: map[string]string{
				"GOOS":            "windows",
				"GOARCH":          "386",
				"CGO_ENABLED":     "0",
				"GOFLAGS":         "-tags=sneaky",
				"GOAMD64":         "v3",
				"GOEXPERIMENT":    "fieldtrack",
				"GOWORK":          "/elsewhere/go.work",
				"RELEASE_TARGETS": "linux/amd64",
			},
		},
		{
			name:     "an exported SHELLCHECK_OPTS reaches neither shellcheck nor actionlint",
			cache:    cacheSet,
			wantExit: true,
			env: map[string]string{
				"SHELLCHECK_OPTS": "--exclude=SC2086 --severity=error",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The record the fake appends to, and the directory it sits in,
			// are the test's own: the fake is on PATH for this make run only.
			record := filepath.Join(t.TempDir(), "record")
			fakeDir := t.TempDir()

			writeFakeLinter(t, fakeDir)
			writeFakeGo(t, fakeDir)
			writeFakeShellcheck(t, fakeDir)

			env := map[string]string{
				"PATH":                      fakeDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"SHIFT2EU_FAKE_LINT_RECORD": record,
			}

			for name, value := range tc.env {
				env[name] = value
			}

			// cacheRoot is the directory make's environment names, or empty
			// when the case leaves the variable unset.
			var cacheRoot string

			switch tc.cache {
			case cacheSet:
				cacheRoot = t.TempDir()
				env["GOLANGCI_LINT_CACHE"] = cacheRoot
			case cacheUnset:
				// Nothing real may be written: whatever directory
				// golangci-lint falls back to must be one of these.
				env["HOME"] = t.TempDir()
				env["XDG_CACHE_HOME"] = t.TempDir()
			}

			if tc.fail != "" {
				env["SHIFT2EU_FAKE_LINT_FAIL"] = tc.fail
			}

			out, err := runMakeLint(t, root, env)

			switch {
			case tc.wantExit && err != nil:
				t.Fatalf("make lint: %v, want it to succeed\n%s", err, out)
			case !tc.wantExit && err == nil:
				t.Fatalf("make lint succeeded, want it to fail on %s, so one make lint reports every target's issues\n%s",
					tc.fail, out)
			}

			runs, goTests, goTools, shellchecks := readLintRecord(t, record)

			// shellcheck and actionlint do not depend on the target, so make
			// lint runs each once, after the linter runs (issue #164).
			// shellcheck must see exactly the scripts IsShellScript picks, the
			// rule test/lint holds their directives to, so dev/shellcheck.sh
			// and test/lint cannot drift apart.
			if len(shellchecks) != 1 {
				t.Errorf("make lint ran shellcheck %d times, want exactly once, after the linter runs: %+v", len(shellchecks), shellchecks)
			} else if run := shellchecks[0]; run.After != len(runs) {
				t.Errorf("make lint ran shellcheck after %d of %d linter runs, want after all of them", run.After, len(runs))
			} else if run.ShellcheckOpts != unsetShellcheckOpts {
				t.Errorf("make lint ran shellcheck with SHELLCHECK_OPTS=%q, want it unset: shellcheck puts its words before its own arguments, and --norc does not stop them",
					run.ShellcheckOpts)
			} else if diff := cmp.Diff(append(slices.Clone(wantShellcheckFlags), scripts...), run.Args); diff != "" {
				t.Errorf("make lint ran shellcheck with the wrong arguments, want %q and every tracked shell script, as test/internal/shellcheck's IsShellScript picks them, in git's order (-want +got):\n%s",
					wantShellcheckFlags, diff)
			}

			wantActionlint := wantActionlintArgs(filepath.Join(fakeDir, "shellcheck"))
			if len(goTools) != 1 {
				t.Errorf("make lint made %d go tool calls, want exactly one, %q, after the linter runs: %+v", len(goTools), wantActionlint, goTools)
			} else if run := goTools[0]; run.Args != wantActionlint || run.After != len(runs) {
				t.Errorf("make lint called go %q after %d of %d linter runs, want go %q after all of them",
					run.Args, run.After, len(runs), wantActionlint)
			} else if run.GOOS != "" || run.GOARCH != "" || run.CGOEnabled != "" {
				t.Errorf("make lint ran actionlint with GOOS=%q GOARCH=%q CGO_ENABLED=%q, want all three unset, so go builds actionlint for the host it runs on",
					run.GOOS, run.GOARCH, run.CGOEnabled)
			} else if run.ShellcheckOpts != unsetShellcheckOpts {
				t.Errorf("make lint ran actionlint with SHELLCHECK_OPTS=%q, want it unset: the shellcheck actionlint starts would inherit it",
					run.ShellcheckOpts)
			}

			// test/lint holds the rules golangci-lint cannot (nolint
			// directives that would hide a depguard issue), so make lint must
			// run it once, after every linter run, for the host's platform.
			if len(goTests) != 1 {
				t.Errorf("make lint made %d go test calls, want exactly one, %q, after the linter runs: %+v",
					len(goTests), wantLintTestArgs, goTests)
			} else if run := goTests[0]; run.Args != wantLintTestArgs || run.After != len(runs) {
				t.Errorf("make lint called go %q after %d of %d linter runs, want go %q after all of them",
					run.Args, run.After, len(runs), wantLintTestArgs)
			} else if run.GOOS != "" || run.GOARCH != "" || run.CGOEnabled != "" {
				t.Errorf("make lint ran test/lint with GOOS=%q GOARCH=%q CGO_ENABLED=%q, want all three unset, so it runs for the host",
					run.GOOS, run.GOARCH, run.CGOEnabled)
			} else if run.Probes != "require" {
				t.Errorf("make lint ran test/lint with SHIFT2EU_LINT_PROBES=%q, want require, so the forbidigo probes fail rather than skip without golangci-lint",
					run.Probes)
			}

			// Every release target plus the native run, so a make lint that
			// stops at the first failure leaves a run out and says so here
			// as well as through the lists below.
			if len(runs) != len(wantRelease)+1 {
				t.Errorf("make lint recorded %d runs of golangci-lint, want %d: one per release target plus the native run\n%s",
					len(runs), len(wantRelease)+1, formatRuns(runs))
			}

			// A run's CGO_ENABLED=0 places it on a release target and any
			// other value, the host's default included, places it on the
			// native one; together the two lists must account for every run.
			var release []string

			var native []lintRun

			for _, run := range runs {
				if run.Args != wantArgs {
					t.Errorf("the %q/%q run with CGO_ENABLED=%q was called with %q, want exactly %q: a run must wait for golangci-lint's lock, and no flag may override the e2e and integration build tags .golangci.yml sets",
						run.GOOS, run.GOARCH, run.CGOEnabled, run.Args, wantArgs)
				}

				if run.GOFLAGS != wantGOFLAGS {
					t.Errorf("the %q/%q run with CGO_ENABLED=%q has GOFLAGS=%q, want exactly %q: a GOFLAGS of the developer's, exported or in the go env file, could add a build tag and take files out of lint",
						run.GOOS, run.GOARCH, run.CGOEnabled, run.GOFLAGS, wantGOFLAGS)
				}

				if run.GOExperiment != "" {
					t.Errorf("the %q/%q run with CGO_ENABLED=%q has GOEXPERIMENT=%q, want it empty: the release builds no experiment, and an experiment decides which files the run type-checks",
						run.GOOS, run.GOARCH, run.CGOEnabled, run.GOExperiment)
				}

				if run.GOWORK != "off" {
					t.Errorf("the %q/%q run with CGO_ENABLED=%q has GOWORK=%q, want %q: a go.work in a parent directory, or one GOWORK names, would switch the run into workspace mode and lint other modules' view of this one",
						run.GOOS, run.GOARCH, run.CGOEnabled, run.GOWORK, "off")
				}

				if run.GOAMD64 != "" && run.GOAMD64 != "v1" {
					t.Errorf("the %q/%q run with CGO_ENABLED=%q has GOAMD64=%q, want empty or %q, the level the release builds at",
						run.GOOS, run.GOARCH, run.CGOEnabled, run.GOAMD64, "v1")
				}

				if run.GOARM64 != "" && run.GOARM64 != "v8.0" {
					t.Errorf("the %q/%q run with CGO_ENABLED=%q has GOARM64=%q, want empty or %q, the level the release builds at",
						run.GOOS, run.GOARCH, run.CGOEnabled, run.GOARM64, "v8.0")
				}

				if run.CGOEnabled == "0" {
					release = append(release, run.GOOS+"/"+run.GOARCH)

					continue
				}

				native = append(native, run)
			}

			slices.Sort(release)

			if diff := cmp.Diff(wantRelease, release); diff != "" {
				t.Errorf("runs with CGO_ENABLED=0 (-want +got):\n%s\nrecorded runs:\n%s",
					diff, formatRuns(runs))
			}

			if len(native) != 1 {
				t.Errorf("%d runs without CGO_ENABLED=0, want exactly one native run with cgo at the host's default:\n%s",
					len(native), formatRuns(runs))
			} else if run := native[0]; run.GOOS != runtime.GOOS || run.GOARCH != runtime.GOARCH {
				// The native run is the one make test's -race build compiles, so
				// it takes the host's platform: a run that left GOOS or GOARCH
				// empty would lint whatever platform it happens to be on, and
				// one that named another platform is a release run. A native run
				// with CGO_ENABLED=0 is a release run to this test, so it lands
				// in the release list, which then holds a platform twice or
				// misses one, and fails the diff above.
				t.Errorf("the native run has GOOS=%q GOARCH=%q, want the host's %q/%q:\n%s",
					run.GOOS, run.GOARCH, runtime.GOOS, runtime.GOARCH, formatRuns(runs))
			}

			// An earlier probe saw an entry of a GOOS=windows run hide the
			// darwin result, so each run gets a cache directory of its own.
			seen := make(map[string]bool, len(runs))

			for _, run := range runs {
				if run.Cache == "" {
					t.Errorf("the %q/%q run recorded no GOLANGCI_LINT_CACHE, so it may share a cache with another target:\n%s",
						run.GOOS, run.GOARCH, formatRuns(runs))

					continue
				}

				if seen[run.Cache] {
					t.Errorf("two runs share GOLANGCI_LINT_CACHE %q:\n%s", run.Cache, formatRuns(runs))
				}

				seen[run.Cache] = true

				if cacheRoot != "" && filepath.Dir(run.Cache) != cacheRoot {
					t.Errorf("the %q/%q run used GOLANGCI_LINT_CACHE %q, which is not a direct child of %q",
						run.GOOS, run.GOARCH, run.Cache, cacheRoot)
				}
			}
		})
	}
}

// writeFakeLinter writes the fake golangci-lint into dir, executable, so make
// lint finds it on the PATH the test builds.
func writeFakeLinter(t *testing.T, dir string) {
	t.Helper()

	writeExecutable(t, filepath.Join(dir, "golangci-lint"), []byte(fakeLinter))
}

// writeFakeShellcheck writes the fake shellcheck into dir, executable.
func writeFakeShellcheck(t *testing.T, dir string) {
	t.Helper()

	writeExecutable(t, filepath.Join(dir, "shellcheck"), []byte(fakeShellcheck))
}

// writeExecutable writes content to path, mode 0700, for a test to run. Every executable the package's tests
// run is written here (issue #267), holding syscall.ForkLock for reading while the file is open for writing.
// Go opens the file O_CLOEXEC, but a parallel test that forks in that window gives its child a copy of the
// write descriptor, which the child holds until its own exec. Linux refuses to exec a file any process holds
// open for writing, so this test's exec of path then fails with ETXTBSY ("text file busy") and no output.
// Every fork in the process takes ForkLock for writing, so no fork happens while the descriptor is open.
// Writing to a temporary name and renaming it into place would not help: the child's descriptor is on the
// same inode. test/devenv's writeFileMode does the same.
func writeExecutable(t *testing.T, path string, content []byte) {
	t.Helper()

	syscall.ForkLock.RLock()
	err := os.WriteFile(path, content, 0o600) //nolint:gosec // path is under the test's own temporary directory
	syscall.ForkLock.RUnlock()

	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // a script the test runs must be executable
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// trackedShellScripts returns the files git tracks in root that
// shellcheck.IsShellScript calls shell scripts, in git's order: what make lint must
// hand shellcheck.
func trackedShellScripts(t *testing.T, root string) []string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files in %s: %v", root, err)
	}

	var scripts []string

	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}

		file := filepath.Join(root, filepath.FromSlash(rel))

		// Tracked but not in this checkout, as a sparse one has it, or not a
		// regular file (a symbolic link or a submodule): dev/shellcheck.sh
		// skips the same files.
		info, err := os.Lstat(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}

		if shellcheck.IsShellScript(rel, src) {
			scripts = append(scripts, rel)
		}
	}

	if len(scripts) == 0 {
		t.Fatal("git tracks no shell script shellcheck.IsShellScript picks, so the shellcheck pin would pin nothing")
	}

	return scripts
}

// TestLintScriptsRefuseAMissingShellcheck pins that dev/shellcheck.sh and
// dev/actionlint.sh fail, and say how to install shellcheck, when none is on
// the PATH, before they run anything: actionlint would otherwise switch its
// shellcheck check off and pass (issue #164). The PATH holds only bash, which
// the scripts' shebang finds, and the fake go, which records any call.
func TestLintScriptsRefuseAMissingShellcheck(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("find bash on PATH: %v", err)
	}

	for _, script := range []string{"dev/shellcheck.sh", "dev/actionlint.sh"} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()

			fakeDir := t.TempDir()
			record := filepath.Join(t.TempDir(), "record")

			writeFakeGo(t, fakeDir)

			if err := os.Symlink(bash, filepath.Join(fakeDir, "bash")); err != nil {
				t.Fatalf("link bash into the PATH: %v", err)
			}

			cmd := exec.CommandContext(t.Context(), filepath.Join(root, filepath.FromSlash(script)))
			cmd.Dir = root
			cmd.Env = makeEnv(map[string]string{"PATH": fakeDir, "SHIFT2EU_FAKE_LINT_RECORD": record})

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("%s succeeded with no shellcheck on the PATH, want it to fail\n%s", script, out)
			}

			const want = "shellcheck is not installed: re-provision the VM or see docs/contributing/development.md"
			if !strings.Contains(string(out), want) {
				t.Errorf("%s without shellcheck said %q, want it to say %q", script, out, want)
			}

			if data, err := os.ReadFile(record); err == nil {
				t.Errorf("%s without shellcheck still called go: %q", script, data)
			} else if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("read the fake go's record: %v", err)
			}
		})
	}
}

// TestActionlintScriptRefusesAConfigFile pins that dev/actionlint.sh fails, and
// names the file, when .github/actionlint.yaml or .github/actionlint.yml exists,
// before it runs actionlint: actionlint reads either one whenever it exists,
// tracked or not, and it can ignore whole rules, so an untracked one would make
// a developer's make lint report less than CI's. test/lint refuses a tracked one
// (issue #164). The script runs from a directory of its own holding a copy of
// it, so the repository's .github is never touched; a symbolic link counts too,
// dangling or not, as actionlint would follow one.
func TestActionlintScriptRefusesAConfigFile(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %q: %v", repoRoot, err)
	}

	script, err := os.ReadFile(filepath.Join(root, "dev", "actionlint.sh"))
	if err != nil {
		t.Fatalf("read dev/actionlint.sh: %v", err)
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("find bash on PATH: %v", err)
	}

	tests := []struct {
		name   string
		config string
		// link makes the config a dangling symbolic link instead of a file.
		link bool
	}{
		{name: "actionlint.yaml", config: ".github/actionlint.yaml"},
		{name: "actionlint.yml", config: ".github/actionlint.yml"},
		{name: "a dangling symbolic link", config: ".github/actionlint.yaml", link: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			work := t.TempDir()
			fakeDir := t.TempDir()
			record := filepath.Join(t.TempDir(), "record")

			for _, dir := range []string{"dev", ".github"} {
				if err := os.Mkdir(filepath.Join(work, dir), 0o700); err != nil {
					t.Fatalf("make %s: %v", dir, err)
				}
			}

			writeExecutable(t, filepath.Join(work, "dev", "actionlint.sh"), script)

			config := filepath.Join(work, filepath.FromSlash(tc.config))

			var createErr error
			if tc.link {
				createErr = os.Symlink(filepath.Join(work, "missing.yaml"), config)
			} else {
				createErr = os.WriteFile(config, []byte("self-hosted-runner:\n  labels: []\n"), 0o600)
			}

			if createErr != nil {
				t.Fatalf("create %s: %v", tc.config, createErr)
			}

			writeFakeGo(t, fakeDir)
			writeFakeShellcheck(t, fakeDir)

			if err := os.Symlink(bash, filepath.Join(fakeDir, "bash")); err != nil {
				t.Fatalf("link bash into the PATH: %v", err)
			}

			cmd := exec.CommandContext(t.Context(), filepath.Join(work, "dev", "actionlint.sh"))
			cmd.Dir = work
			cmd.Env = makeEnv(map[string]string{"PATH": fakeDir, "SHIFT2EU_FAKE_LINT_RECORD": record})

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("dev/actionlint.sh succeeded with %s present, want it to fail\n%s", tc.config, out)
			}

			want := "actionlint: " + tc.config + " exists: it can silence actionlint's checks"
			if !strings.Contains(string(out), want) {
				t.Errorf("dev/actionlint.sh with %s present said %q (%v), want it to say %q", tc.config, out, err, want)
			}

			if data, err := os.ReadFile(record); err == nil {
				t.Errorf("dev/actionlint.sh with %s present still called go: %q", tc.config, data)
			} else if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("read the fake go's record: %v", err)
			}
		})
	}
}

// writeFakeGo writes the fake go into dir, executable, delegating every call
// but go test to the go command the test finds on PATH.
func writeFakeGo(t *testing.T, dir string) {
	t.Helper()

	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go on PATH: %v", err)
	}

	realGo, err = filepath.Abs(realGo)
	if err != nil {
		t.Fatalf("resolve the go command: %v", err)
	}

	if strings.Contains(realGo, "'") {
		t.Fatalf("the go command's path %q holds a single quote, which the fake go cannot quote", realGo)
	}

	writeExecutable(t, filepath.Join(dir, "go"), []byte(fmt.Sprintf(fakeGo, "'"+realGo+"'")))
}

// runMakeLint runs make lint in root and returns its combined output and error.
// Its environment is the test's own minus what would decide the platform, the
// linter cache or the build flags of every run, with env set on top.
func runMakeLint(t *testing.T, root string, env map[string]string) ([]byte, error) {
	t.Helper()

	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("find make on PATH: %v; CI runs this test through make cover, so make is installed there", err)
	}

	cmd := exec.CommandContext(t.Context(), makePath, "-s", "-C", root, "lint")
	cmd.Env = makeEnv(env)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("make -s -C %s lint: %w", root, err)
	}

	return out, nil
}

// makeEnv returns make's environment: the inherited one without the variables
// that would pick a platform, a linter cache, build flags or the linter's own
// configuration for the runs, with env set on top.
func makeEnv(env map[string]string) []string {
	drop := map[string]bool{
		"MAKEFLAGS": true,
		"MFLAGS":    true,
		"MAKELEVEL": true,
		// GNU make reads both: make ignores its own environment
		// assignment only in this file, and -e would let an exported
		// RELEASE_TARGETS replace the Makefile's list.
		"GNUMAKEFLAGS":        true,
		"MAKEFILES":           true,
		"GOOS":                true,
		"GOARCH":              true,
		"CGO_ENABLED":         true,
		"GOLANGCI_LINT_CACHE": true,
		"GOFLAGS":             true,
		"GOEXPERIMENT":        true,
		"GOAMD64":             true,
		"GOARM64":             true,
		"GOWORK":              true,
		// The fake linter's own knobs: what this test sets for its case
		// is the only thing that may decide which run it fails.
		"SHIFT2EU_FAKE_LINT_FAIL":   true,
		"SHIFT2EU_FAKE_LINT_RECORD": true,
		// Only the Makefile may set it for test/lint: an inherited value
		// would hide a Makefile that no longer does.
		"SHIFT2EU_LINT_PROBES": true,
	}

	out := make([]string, 0, len(os.Environ())+len(env))

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}

		if _, ok := env[name]; ok {
			continue
		}

		out = append(out, entry)
	}

	for name, value := range env {
		out = append(out, name+"="+value)
	}

	return out
}

// readLintRecord reads the lines the fake linter, the fake go and the fake
// shellcheck appended: the linter runs in the order make made them, and every
// go test call, go tool call and shellcheck call with how many linter runs came
// before it.
func readLintRecord(t *testing.T, path string) ([]lintRun, []goTestRun, []goToolRun, []shellcheckRun) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fake linter's record: %v", err)
	}

	trimmed := strings.TrimSuffix(string(data), "\n")
	if trimmed == "" {
		t.Fatal("make lint recorded no call of golangci-lint, so its lint target did not run the linter")
	}

	lines := strings.Split(trimmed, "\n")

	runs := make([]lintRun, 0, len(lines))

	var (
		goTests     []goTestRun
		goTools     []goToolRun
		shellchecks []shellcheckRun
	)

	for i, line := range lines {
		if rest, ok := strings.CutPrefix(line, "#tool\t"); ok {
			fields := strings.SplitN(rest, "\t", 5)
			if len(fields) != 5 {
				t.Fatalf("record line %d = %q, want #tool and 5 tab-separated fields: GOOS, GOARCH, CGO_ENABLED, SHELLCHECK_OPTS and the arguments", i+1, line)
			}

			goTools = append(goTools, goToolRun{
				After: len(runs), GOOS: fields[0], GOARCH: fields[1], CGOEnabled: fields[2], ShellcheckOpts: fields[3], Args: fields[4],
			})

			continue
		}

		if rest, ok := strings.CutPrefix(line, "#shellcheck\t"); ok {
			fields := strings.Split(rest, "\t")
			shellchecks = append(shellchecks, shellcheckRun{After: len(runs), ShellcheckOpts: fields[0], Args: fields[1:]})

			continue
		}

		if rest, ok := strings.CutPrefix(line, "#go\t"); ok {
			fields := strings.SplitN(rest, "\t", 5)
			if len(fields) != 5 {
				t.Fatalf("record line %d = %q, want #go and 5 tab-separated fields: GOOS, GOARCH, CGO_ENABLED, SHIFT2EU_LINT_PROBES and the arguments", i+1, line)
			}

			goTests = append(goTests, goTestRun{
				After: len(runs), GOOS: fields[0], GOARCH: fields[1], CGOEnabled: fields[2], Probes: fields[3], Args: fields[4],
			})

			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 10 {
			t.Fatalf("record line %d = %q, want 10 tab-separated fields: GOOS, GOARCH, CGO_ENABLED, GOLANGCI_LINT_CACHE, GOFLAGS, GOEXPERIMENT, GOAMD64, GOARM64, GOWORK and the arguments",
				i+1, line)
		}

		runs = append(runs, lintRun{
			GOOS:         fields[0],
			GOARCH:       fields[1],
			CGOEnabled:   fields[2],
			Cache:        fields[3],
			GOFLAGS:      fields[4],
			GOExperiment: fields[5],
			GOAMD64:      fields[6],
			GOARM64:      fields[7],
			GOWORK:       fields[8],
			Args:         fields[9],
		})
	}

	return runs, goTests, goTools, shellchecks
}

// formatRuns renders the recorded runs, one per line, for an assertion message.
func formatRuns(runs []lintRun) string {
	lines := make([]string, 0, len(runs))

	for _, run := range runs {
		lines = append(lines, fmt.Sprintf("GOOS=%q GOARCH=%q CGO_ENABLED=%q GOLANGCI_LINT_CACHE=%q GOFLAGS=%q GOEXPERIMENT=%q GOAMD64=%q GOARM64=%q GOWORK=%q args=%q",
			run.GOOS, run.GOARCH, run.CGOEnabled, run.Cache, run.GOFLAGS, run.GOExperiment, run.GOAMD64, run.GOARM64, run.GOWORK, run.Args))
	}

	return strings.Join(lines, "\n")
}

// hostCGO returns go env CGO_ENABLED in root, in the environment make lint
// runs in: the host's default, with no CGO_ENABLED of the test's.
func hostCGO(t *testing.T, root string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "env", "CGO_ENABLED")
	cmd.Dir = root
	cmd.Env = makeEnv(nil)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env CGO_ENABLED: %v", err)
	}

	return strings.TrimSpace(string(out))
}
