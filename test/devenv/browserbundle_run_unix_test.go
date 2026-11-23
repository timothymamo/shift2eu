//go:build unix

// Pins the TEST_BROWSER_BUNDLE variable the release's site-browser job gives make test-browser (PRD #297
// milestone 3): with it set, the recipe smoke-tests the bundle it is given, resolved to an absolute path,
// instead of building one of its own; it refuses a path it cannot serve before it starts anything, with exit
// status 2 and one line on standard error; it reads the value raw, so neither make nor its shell runs what the
// value holds; and it never removes, moves or writes the given bundle, an interrupted run included, since the
// removal its EXIT trap runs is its own temporary directory's.
//
// Each test runs the real recipe of the real Makefile in a clone of it, with the container command and go
// replaced by the stand-ins browserrecipe_run_unix_test.go builds, so no test needs a container runtime, a
// browser or a build of the site.
package devenv_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// givenBundleVariable is the make variable the release's site-browser job sets: the bundle make test-browser
// smoke-tests instead of building one of its own (PRD #297 milestone 3).
const givenBundleVariable = "TEST_BROWSER_BUNDLE"

// givenBundlePage is the page a given bundle holds, so a record of what the smoke test served tells a bundle
// the test wrote from one the recipe built.
const givenBundlePage = "<!doctype html>"

// writeGivenBundle creates a bundle of the shape ci/sitebuild writes, manifest.json included, under dir, and
// returns the path.
func writeGivenBundle(t *testing.T, dir string) string {
	t.Helper()

	writeFileMode(t, filepath.Join(dir, "manifest.json"), []byte("{}\n"), 0o600)
	writeFileMode(t, filepath.Join(dir, "index.html"), []byte(givenBundlePage+"\n"), 0o600)

	return dir
}

// bundleContents reads every file under dir, relative to it, with what it holds. It walks a file system rooted
// at dir, so no entry it reads can point outside the bundle.
func bundleContents(t *testing.T, dir string) map[string]string {
	t.Helper()

	fsys := os.DirFS(dir)
	contents := map[string]string{}

	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		body, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		contents[path] = string(body)

		return nil
	})
	if err != nil {
		t.Fatalf("read the bundle %s: %v", dir, err)
	}

	return contents
}

// buildRecords reads the lines the stand-in go command logged for each run of ci/sitebuild, one per build.
func buildRecords(t *testing.T, path string) []string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		t.Fatalf("read %s: %v", path, err)
	}

	var records []string

	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		if line != "" {
			records = append(records, line)
		}
	}

	return records
}

// TestMakeTestBrowserRecipeSmokeTestsTheGivenBundle pins issue #297's TEST_BROWSER_BUNDLE: with the variable set
// to a bundle directory, make test-browser serves that bundle, resolved to an absolute path, and builds none of
// its own - so the release's site-browser job smoke-tests the very bundle the release attests and publishes.
func TestMakeTestBrowserRecipeSmokeTestsTheGivenBundle(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModePassing)

	buildLog := filepath.Join(sb.root, "build.log")
	sb.env["SHIFT2EU_TEST_BUILD_LOG"] = buildLog

	bundle := writeGivenBundle(t, filepath.Join(sb.root, "release-bundle"))
	before := bundleContents(t, bundle)

	sb.env[givenBundleVariable] = bundle

	run := runBrowserMake(makeProgram(t), sb.clone, []string{testBrowserTarget}, sb.env, browserRecipeTimeout)
	if run.timedOut {
		t.Fatalf("make test-browser was still running after %s and the test stopped it\n%s", browserRecipeTimeout, run.output())
	}

	if run.err != nil {
		t.Fatalf("make test-browser failed against a given bundle of the shape ci/sitebuild writes: %v\n%s", run.err, run.output())
	}

	if builds := buildRecords(t, buildLog); len(builds) != 0 {
		t.Errorf("make test-browser built %d bundles of its own with %s set to %q, the first into %s: with a bundle given, the recipe smoke-tests that bundle and builds none (PRD #297 milestone 3)\n%s",
			len(builds), givenBundleVariable, bundle, builds[0], run.output())
	}

	records := sb.goTestRecords(t)
	if len(records) != 1 {
		t.Fatalf("the stand-in go command ran the smoke test %d times, want exactly once: a run of make test-browser with a bundle given still smoke-tests it (PRD #297 milestone 3)\n%s",
			len(records), sb.goLogText(t))
	}

	if want, got := resolvedPath(t, bundle), filepath.Clean(records[0].bundle); got != want {
		t.Errorf("make test-browser handed the smoke test %q, want the given bundle resolved to %q: the recipe serves the bundle it was given, by its absolute path (PRD #297 milestone 3)",
			got, want)
	}

	if got := bundleContents(t, bundle); len(got) != len(before) || got["manifest.json"] != before["manifest.json"] || got["index.html"] != before["index.html"] {
		t.Errorf("make test-browser left the given bundle %s holding %v, want exactly what it held before the run, %v: a run serves that bundle and never writes it (PRD #297 milestone 3)",
			bundle, got, before)
	}
}

// TestMakeTestBrowserRecipeResolvesARelativeGivenBundle pins that a relative value is resolved from the
// directory make runs in, as make resolves its own paths: the recipe passes the smoke test an absolute path.
func TestMakeTestBrowserRecipeResolvesARelativeGivenBundle(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModePassing)

	sb.env["SHIFT2EU_TEST_BUILD_LOG"] = filepath.Join(sb.root, "build.log")

	bundle := writeGivenBundle(t, filepath.Join(sb.root, "release-bundle"))
	sb.env[givenBundleVariable] = "../release-bundle"

	run := runBrowserMake(makeProgram(t), sb.clone, []string{testBrowserTarget}, sb.env, browserRecipeTimeout)
	if run.timedOut {
		t.Fatalf("make test-browser was still running after %s and the test stopped it\n%s", browserRecipeTimeout, run.output())
	}

	if run.err != nil {
		t.Fatalf("make test-browser failed against the relative bundle ../release-bundle from %s: %v\n%s", sb.clone, run.err, run.output())
	}

	records := sb.goTestRecords(t)
	if len(records) != 1 {
		t.Fatalf("the stand-in go command ran the smoke test %d times, want exactly once\n%s", len(records), sb.goLogText(t))
	}

	if got, want := filepath.Clean(records[0].bundle), resolvedPath(t, bundle); got != want {
		t.Errorf("make test-browser handed the smoke test %q for the relative value ../release-bundle, want the resolved path %q: a relative bundle is resolved from the directory make runs in (PRD #297 milestone 3)",
			got, want)
	}
}

// TestMakeTestBrowserRecipeRefusesAGivenBundleItCannotServe pins the values the recipe refuses before it starts
// anything: a path that is not there, one that is a file, and one that is a directory holding no manifest.json.
// Each is exit status 2 and one line on standard error naming the value, no container and no smoke test.
func TestMakeTestBrowserRecipeRefusesAGivenBundleItCannotServe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// value returns the path the case gives, built under the sandbox root.
		value func(t *testing.T, sb *browserRecipeSandbox) string
	}{
		{
			name: "a path that is not there",
			value: func(t *testing.T, sb *browserRecipeSandbox) string {
				t.Helper()

				return filepath.Join(sb.root, "missing-bundle")
			},
		},
		{
			name: "a file",
			value: func(t *testing.T, sb *browserRecipeSandbox) string {
				t.Helper()

				path := filepath.Join(sb.root, "bundle.tar.gz")
				writeFileMode(t, path, []byte("not a bundle\n"), 0o600)

				return path
			},
		},
		{
			name: "a directory without a manifest",
			value: func(t *testing.T, sb *browserRecipeSandbox) string {
				t.Helper()

				dir := filepath.Join(sb.root, "empty-bundle")
				writeFileMode(t, filepath.Join(dir, "index.html"), []byte(givenBundlePage+"\n"), 0o600)

				return dir
			},
		},
		{
			name: "a value that is no directory once a shell would have expanded it",
			value: func(t *testing.T, _ *browserRecipeSandbox) string {
				t.Helper()

				return "$(printf bundle)"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newBrowserRecipeSandbox(t, dockerModePassing)

			value := tc.value(t, sb)
			sb.env[givenBundleVariable] = value
			sb.env["SHIFT2EU_TEST_BUILD_LOG"] = filepath.Join(sb.root, "build.log")

			run := runBrowserMake(makeProgram(t), sb.clone, []string{testBrowserTarget}, sb.env, failFastTimeout)
			output := run.output()

			if run.timedOut {
				t.Fatalf("make test-browser was still running after %s and the test stopped it, with %s set to %q: the recipe must decide on the value before it starts anything\n%s",
					failFastTimeout, givenBundleVariable, value, output)
			}

			status := -1

			var exitErr *exec.ExitError
			if errors.As(run.err, &exitErr) {
				status = exitErr.ExitCode()
			}

			if status != 2 {
				t.Errorf("make test-browser exited with status %d, want 2, with %s set to %q: a bundle the recipe cannot serve is refused before the run starts, with the status that tells a refusal from a failed run (PRD #297 milestone 3)\n%s",
					status, givenBundleVariable, value, output)
			}

			if !refusedValueLine(run.stderr, givenBundleVariable, value) {
				t.Errorf("the run's standard error holds no line naming %s and the %q it refused, so a maintainer who mistyped the bundle reads what was refused (PRD #297 milestone 3)\n%s",
					givenBundleVariable, value, output)
			}

			// The refusal is one line: the recipe says what it could not serve, and nothing else.
			if lines := strings.Split(strings.TrimSuffix(strings.TrimSpace(run.stderr), "\n"), "\n"); len(lines) != 1 {
				t.Errorf("the refusal printed %d lines on standard error, want 1: %q (PRD #297 milestone 3)",
					len(lines), run.stderr)
			}

			if got := sb.callsTo(t, "run"); got != 0 {
				t.Errorf("make test-browser started a container %d times although it refused %s=%q: the value is decided on before anything runs (PRD #297 milestone 3)\n%s",
					got, givenBundleVariable, value, sb.dockerLogText(t))
			}

			if records := sb.goTestRecords(t); len(records) != 0 {
				t.Errorf("make test-browser ran the smoke test %d times although it refused %s=%q (PRD #297 milestone 3)\n%s",
					len(records), givenBundleVariable, value, sb.goLogText(t))
			}

			if builds := buildRecords(t, filepath.Join(sb.root, "build.log")); len(builds) != 0 {
				t.Errorf("make test-browser built a bundle although it refused %s=%q: %v (PRD #297 milestone 3)",
					givenBundleVariable, value, builds)
			}
		})
	}
}

// expansionMarker is the text a value holding a make function would print were make, or its shell, to run what the
// value holds: no run may print it, since the recipe reads the value raw.
const expansionMarker = "EXPANDED-BY-MAKE"

// TestMakeTestBrowserRecipeReadsACommandLineGivenBundleRaw pins that the value is read raw whichever way it reaches
// make (PRD #297 milestone 3): a value given on make's command line is a recursive variable in its origin, so make
// expands it - runs what it holds - wherever the recipe names it plainly, as the environment's copy once did. The
// recipe takes it with $(value ...), as it takes the deadline and SITE_VERSION, and refuses a value holding a make
// function before it starts anything: a call in it is never expanded, by make or by its shell, and the raw value is
// what the one line on standard error names, with exit status 2.
func TestMakeTestBrowserRecipeReadsACommandLineGivenBundleRaw(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// value is what make's command line holds: a make function call, which make runs as it expands the
		// variable, and one whose own shell would run if the value reached a shell unrefused.
		value string
	}{
		{name: "a value holding an info function", value: "$(info " + expansionMarker + ")"},
		{name: "a value holding a shell function", value: "$(shell echo " + expansionMarker + " >&2)x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newBrowserRecipeSandbox(t, dockerModePassing)

			buildLog := filepath.Join(sb.root, "build.log")
			sb.env["SHIFT2EU_TEST_BUILD_LOG"] = buildLog

			// The value comes as make's own command-line argument, as the release's workflow and a maintainer write
			// it: make's recursive variable, which it expands.
			args := []string{testBrowserTarget, givenBundleVariable + "=" + tc.value}

			run := runBrowserMake(makeProgram(t), sb.clone, args, sb.env, failFastTimeout)
			output := run.output()

			if run.timedOut {
				t.Fatalf("make test-browser was still running after %s and the test stopped it, with %s given as %q on its command line: the recipe must decide on the value before it starts anything\n%s",
					failFastTimeout, givenBundleVariable, tc.value, output)
			}

			// Make prints what a function it expanded returned on standard output, and its shell's own output on
			// standard error: neither may hold the marker, whatever else the refusal says.
			if strings.Contains(run.stdout, expansionMarker) {
				t.Errorf("make test-browser printed %q on standard output, so make expanded the %s it was given as %q on its command line and ran the function it holds: the value is read raw, as SITE_VERSION's is, and a value holding one is refused unexpanded (PRD #297 milestone 3)\n%s",
					expansionMarker, givenBundleVariable, tc.value, output)
			}

			status := -1

			var exitErr *exec.ExitError
			if errors.As(run.err, &exitErr) {
				status = exitErr.ExitCode()
			}

			if status != 2 {
				t.Errorf("make test-browser exited with status %d, want 2, with %s given as %q on its command line: a value holding a make function is refused before the run starts, with the status that tells a refusal from a failed run (PRD #297 milestone 3)\n%s",
					status, givenBundleVariable, tc.value, output)
			}

			// The refusal is one line: the expansion's own output is not beside it, and the raw value is named.
			if lines := strings.Split(strings.TrimSuffix(strings.TrimSpace(run.stderr), "\n"), "\n"); len(lines) != 1 {
				t.Errorf("the refusal printed %d lines on standard error, want 1: %q: the value is refused unexpanded, so no output of a function it holds stands beside the refusal (PRD #297 milestone 3)",
					len(lines), run.stderr)
			}

			if !refusedValueLine(run.stderr, givenBundleVariable, tc.value) {
				t.Errorf("the run's standard error holds no line naming %s and the %q it refused, so a maintainer who mistyped the bundle reads what was refused, not what make made of it (PRD #297 milestone 3)\n%s",
					givenBundleVariable, tc.value, output)
			}

			if got := sb.callsTo(t, "run"); got != 0 {
				t.Errorf("make test-browser started a container %d times although it refused %s=%q: the value is decided on before anything runs (PRD #297 milestone 3)\n%s",
					got, givenBundleVariable, tc.value, sb.dockerLogText(t))
			}

			if records := sb.goTestRecords(t); len(records) != 0 {
				t.Errorf("make test-browser ran the smoke test %d times although it refused %s=%q (PRD #297 milestone 3)\n%s",
					len(records), givenBundleVariable, tc.value, sb.goLogText(t))
			}

			if builds := buildRecords(t, buildLog); len(builds) != 0 {
				t.Errorf("make test-browser built a bundle although it refused %s=%q: %v (PRD #297 milestone 3)",
					givenBundleVariable, tc.value, builds)
			}
		})
	}
}

// TestMakeTestBrowserRecipeKeepsTheGivenBundleOnASignal pins that an interrupted run leaves the bundle it was
// given exactly as it found it: the EXIT trap removes the run's own temporary directory alone.
func TestMakeTestBrowserRecipeKeepsTheGivenBundleOnASignal(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeSilent)

	bundle := writeGivenBundle(t, filepath.Join(sb.root, "release-bundle"))
	before := bundleContents(t, bundle)

	sb.env[givenBundleVariable] = bundle

	proc := startBrowserMake(t, sb.clone, []string{testBrowserTarget}, sb.env)
	// The recipe waits for a DevTools line once it has read the container's log: its traps are in place and the
	// bundle it was given is the one it is about to smoke-test.
	sb.waitForDockerCall(t, "logs")

	if err := syscall.Kill(-proc.pgid(), syscall.SIGTERM); err != nil {
		t.Fatalf("terminate the run of make test-browser: %v", err)
	}

	run := proc.wait(browserRecipeTimeout)
	if run.timedOut {
		t.Fatalf("the run of make test-browser was still going %s after it was terminated\n%s", browserRecipeTimeout, run.output())
	}

	if got := bundleContents(t, bundle); len(got) != len(before) || got["manifest.json"] != before["manifest.json"] || got["index.html"] != before["index.html"] {
		t.Errorf("an interrupted make test-browser left the bundle it was given, %s, holding %v, want exactly what it held before the run, %v: the EXIT trap removes the run's own temporary directory alone (PRD #297 milestone 3)",
			bundle, got, before)
	}
}
