//go:build unix

// Pins what two runs of make test-browser at once do to the site's bundle, and what a run does when the container
// it started Chromium in is already gone (issue #423 items 2 and 3, PRD #297 milestone 2, ADR 0036). Both run the
// real recipe of the real Makefile, in a clone of it, with the two commands the recipe runs replaced by
// stand-ins - the container command, the recipe's $(DOCKER), and go - so no test needs a container runtime, a
// browser or a build of the site, and no test writes anything into the worktree it runs from. The stand-ins
// answer the way the tools they stand for do, whatever the recipe asks them, so the tests pin the recipe's
// behaviour and not one spelling of it.
//
// Unix only, as the run kills the whole process group of a recipe it gives up on (syscall.SysProcAttr.Setpgid):
// a recipe that waits out its 60 seconds would otherwise hold the test there, for the minute in which the case
// under test is the wait itself. Lint type-checks the package for the windows release targets too, where a
// process group is no such thing.
package devenv_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// What the stand-ins answer with, and what the tests read back from their answers.
const (
	// devtoolsLogLine is what the stand-in Chromium logs when its container runs, in the form the recipe reads the
	// DevTools address back from (Makefile, make test-browser). No browser exists here, so the address leads
	// nowhere: the recipe only has to read it and hand it on.
	devtoolsLogLine = "DevTools listening on ws://127.0.0.1:39411/devtools/browser/0a1b2c3d-0000-4000-8000-000000000000"

	// deadContainerLogLine stands in the log of a container that exited at once, and is the tail the recipe must
	// show a maintainer who finds that its Chromium never came up.
	deadContainerLogLine = "headless-shell: failed to launch the browser process: cannot open display :0"

	// deadContainerStatus is the status the stand-in container of the dead mode exits with: 3, and not 1, so a
	// message that names the recipe's own failure status cannot be mistaken for one that names the container's.
	deadContainerStatus = "3"

	// bundlePageLine is the first line of the page the stand-in site builder writes into the bundle: a run that
	// serves the bundle's files is told from one that serves an empty directory by reading it back.
	bundlePageLine = "<!doctype html>"
)

// The two containers the stand-in container command stands for.
const (
	// dockerModeRunning is a Chromium that runs and logs its DevTools address: the run a test expects to succeed.
	dockerModeRunning = "running"
	// dockerModeDead is a container that started and exited at once, with deadContainerStatus, after printing
	// deadContainerLogLine: what the recipe has to fail on at once instead of waiting for a log line that never
	// comes (issue #423 item 3).
	dockerModeDead = "dead"
)

// One run of make test-browser gets this long before the test stops it: the stand-ins answer in milliseconds, so
// a run that is still going is one whose recipe waits for something that never comes.
const browserRecipeTimeout = 60 * time.Second

// The dead container's cases are bounded well under the recipe's own 60 seconds: the recipe must notice the exit
// and fail, and a test that waited the minute would report the wait this case exists to remove.
const (
	deadContainerTimeout  = 20 * time.Second
	deadContainerDeadline = 15 * time.Second
)

// exitStatusWord matches deadContainerStatus as a number of its own: "exited with status 3", "exit code 3" and
// "Error 3" all hold it, where a line number or a status that merely holds a 3 (13, 130, 403) does not.
var exitStatusWord = regexp.MustCompile(`(^|[^0-9])3([^0-9]|$)`)

// fakeContainerCommand stands in for the container command make test-browser starts Chromium with, the recipe's
// $(DOCKER), so the recipe runs on a machine that has no container runtime and starts no browser, and a test can
// say exactly what the container does. It records its argv, as the stand-ins of this package do, and answers the
// subcommands a recipe that starts a container, reads its log and checks that it still runs may use.
//
// SHIFT2EU_TEST_DOCKER_MODE says which container it stands for: dockerModeRunning, one that runs and logs
// devtoolsLogLine, and dockerModeDead, one that exited at once with SHIFT2EU_TEST_DOCKER_STATUS after printing
// SHIFT2EU_TEST_DOCKER_LOG_LINE. A run in dockerModeRunning meets the other run of the same worktree at the
// rendezvous inside run, so both runs hold what they serve at the same time, whichever of them comes first
// (issue #423 item 2). A subcommand none of these is fails the run with status 64 and names itself, rather than
// answering nothing and leaving the recipe to misread the silence.
const fakeContainerCommand = `#!/bin/sh
log="${SHIFT2EU_TEST_DOCKER_LOG:?set SHIFT2EU_TEST_DOCKER_LOG}"
mode="${SHIFT2EU_TEST_DOCKER_MODE:?set SHIFT2EU_TEST_DOCKER_MODE}"
{
	printf 'tool=docker\n'
	for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "$log"
if [ $# -eq 0 ]; then
	echo 'fake docker: no command' >&2
	exit 64
fi
cmd=$1
shift
case "$cmd" in
container | image)
	cmd=$1
	shift
	;;
esac
if [ "$mode" = dead ]; then
	running=false
	state=exited
	exitcode="${SHIFT2EU_TEST_DOCKER_STATUS:?set SHIFT2EU_TEST_DOCKER_STATUS}"
else
	running=true
	state=running
	exitcode=0
fi
case "$cmd" in
run)
	if [ "$mode" = running ]; then
		: > "${SHIFT2EU_TEST_RENDEZVOUS:?set SHIFT2EU_TEST_RENDEZVOUS}/run.$$" || exit 1
		waited=0
		while [ "$(ls -A "$SHIFT2EU_TEST_RENDEZVOUS" | wc -l)" -lt 2 ]; do
			waited=$((waited + 1))
			# 800 polls of 0.05 s: 40 seconds, still inside the 60 seconds browserRecipeTimeout gives a run,
			# and long enough that a loaded CI runner under -race does not reach it before the other run does.
			if [ "$waited" -gt 800 ]; then
				echo 'fake docker: the second run of the worktree never reached its container' >&2
				exit 64
			fi
			sleep 0.05
		done
	fi
	printf 'contract-id\n'
	;;
logs)
	if [ "$mode" = dead ]; then
		printf '%s\n' "${SHIFT2EU_TEST_DOCKER_LOG_LINE:?set SHIFT2EU_TEST_DOCKER_LOG_LINE}"
	else
		printf '%s\n' "${SHIFT2EU_TEST_DEVTOOLS_LINE:?set SHIFT2EU_TEST_DEVTOOLS_LINE}"
	fi
	;;
inspect)
	case "$*" in
	*ExitCode*) printf '%s\n' "$exitcode" ;;
	*Running*) printf '%s\n' "$running" ;;
	*Status*) printf '%s\n' "$state" ;;
	*) printf '[{"Id":"contract-id","State":{"Status":"%s","Running":%s,"ExitCode":%s}}]\n' "$state" "$running" "$exitcode" ;;
	esac
	;;
wait)
	printf '%s\n' "$exitcode"
	;;
ps)
	case "$*" in
	*State*) [ "$mode" = dead ] || printf 'running\n' ;;
	*) [ "$mode" = dead ] || printf 'contract-id\n' ;;
	esac
	;;
rm | kill | stop | start | pull)
	:
	;;
*)
	echo "fake docker: unexpected command: $cmd" >&2
	exit 64
	;;
esac
exit 0
`

// fakeGoCommand stands in for the go command the two targets run: make site's builder, which gets the directory
// and the archive to write, and make test-browser's smoke test, which gets the bundle and the DevTools address.
// The builder answers the way ci/sitebuild does: it refuses a -out directory that holds anything, as its
// prepareOut does, and it takes a moment between creating that directory and writing the page into it, so a
// second builder, or the rm -rf of a second make site in the same worktree, shows up as the failure the real
// builder reports ("write index.html: ... no such file or directory", "the bundle directory ... is not empty")
// instead of passing because the two writes happened to fall in a safe order (issue #423). The builder writes a
// page and the archive, so make site still produces what a release publishes; the smoke test writes one line to
// the log the test reads, holding what it was handed and what the directory it was handed holds at that moment -
// the run is serving that directory then, and the test asks what became of it afterwards.
const fakeGoCommand = `#!/bin/sh
log="${SHIFT2EU_TEST_GO_LOG:?set SHIFT2EU_TEST_GO_LOG}"
if [ $# -eq 0 ]; then
	echo 'fake go: no command' >&2
	exit 64
fi
cmd=$1
shift
case "$cmd" in
run)
	out=
	archive=
	while [ $# -gt 0 ]; do
		case "$1" in
		-out | --out)
			out=$2
			shift 2
			;;
		-archive | --archive)
			archive=$2
			shift 2
			;;
		*)
			shift
			;;
		esac
	done
	if [ -z "$out" ]; then
		echo 'fake go: go run names no -out directory, so the site has no bundle to go into' >&2
		exit 64
	fi
	# ci/sitebuild's prepareOut creates the bundle directory, refusing one that holds anything: a stray file
	# would be deployed without a manifest entry. A second builder meeting a directory the first one is filling
	# is refused here the same way, rather than quietly adding to a bundle another run is building.
	if [ -d "$out" ] && [ -n "$(ls -A "$out" 2>/dev/null)" ]; then
		echo "fake go: the bundle directory $out is not empty" >&2
		exit 1
	fi
	mkdir -p "$out" || exit 1
	# The real builder takes a moment between creating the bundle directory and writing the pages into it. A run
	# that removes and rebuilds that directory has that moment to delete it from under the builder, where the
	# real one then fails to write its page; without the pause the two writes could always fall in a safe order.
	sleep 0.2
	printf '%s\n' "${SHIFT2EU_TEST_BUNDLE_PAGE:?set SHIFT2EU_TEST_BUNDLE_PAGE}" > "$out/index.html" || exit 1
	if [ -n "$archive" ]; then
		printf 'archive\n' > "$archive" || exit 1
	fi
	;;
test)
	bundle="${SHIFT2EU_SITE_BUNDLE:-}"
	files=$(ls -A "$bundle" 2>/dev/null | tr '\n' ',')
	page=$(head -n 1 "$bundle/index.html" 2>/dev/null)
	printf 'bundle=%s\tfiles=%s\tpage=%s\tdevtools=%s\n' \
		"$bundle" "$files" "$page" "${SHIFT2EU_DEVTOOLS_URL:-}" >> "$log"
	;;
*)
	echo "fake go: unexpected command: $cmd" >&2
	exit 64
	;;
esac
exit 0
`

// browserRecipeSandbox is a clone of the repository's Makefile with the two commands make test-browser runs
// replaced by stand-ins, in a directory of its own: the Makefile make runs, the clone the recipe runs in, the
// stand-in tools first on PATH, and the environment one run gets, alone.
type browserRecipeSandbox struct {
	clone  string            // the clone make runs in, from writeClone
	binDir string            // first on PATH: the stand-in docker and go
	env    map[string]string // the environment one run gets, never the test process's own
	goLog  string            // one line per stand-in go test run: what it was handed, and what that held
}

// newBrowserRecipeSandbox builds a sandbox whose recipe runs against a container in mode, and removes it with the
// test.
func newBrowserRecipeSandbox(t *testing.T, mode string) *browserRecipeSandbox {
	t.Helper()

	root := newSandboxRoot(t)
	sb := &browserRecipeSandbox{
		binDir: filepath.Join(root, "bin"),
		goLog:  filepath.Join(root, "go.log"),
	}

	sb.clone = writeClone(t, filepath.Join(root, "clone"))

	// The workspace the VM mounts, with the checkout WORKSPACE_REPO names, as a clone on the maintainer's Mac has
	// it: the Makefile resolves its host paths from it while it is read.
	for _, dir := range []string{
		sb.binDir,
		filepath.Join(root, "home"),
		filepath.Join(root, "tmp"),
		filepath.Join(root, "rendezvous"),
		filepath.Join(root, "ws", "shift2eu"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	writeFileMode(t, filepath.Join(sb.binDir, "docker"), []byte(fakeContainerCommand), 0o700)
	writeFileMode(t, filepath.Join(sb.binDir, "go"), []byte(fakeGoCommand), 0o700)

	sb.env = map[string]string{
		// The machine's own tools, with the two stand-ins first: make resolves the recipe's container command with
		// command -v, and the recipe's shell finds sh, sed, head, sleep, grep and git behind them.
		"PATH":           sb.binDir + ":/usr/local/bin:/usr/bin:/bin",
		"HOME":           filepath.Join(root, "home"),
		"TMPDIR":         filepath.Join(root, "tmp"),
		"WORKSPACE_REPO": filepath.Join(root, "ws", "shift2eu"),

		"SHIFT2EU_TEST_GO_LOG":          sb.goLog,
		"SHIFT2EU_TEST_DOCKER_LOG":      filepath.Join(root, "docker.log"),
		"SHIFT2EU_TEST_DOCKER_MODE":     mode,
		"SHIFT2EU_TEST_DOCKER_STATUS":   deadContainerStatus,
		"SHIFT2EU_TEST_DOCKER_LOG_LINE": deadContainerLogLine,
		"SHIFT2EU_TEST_DEVTOOLS_LINE":   devtoolsLogLine,
		"SHIFT2EU_TEST_BUNDLE_PAGE":     bundlePageLine,
		"SHIFT2EU_TEST_RENDEZVOUS":      filepath.Join(root, "rendezvous"),
	}

	return sb
}

// sharedBundle is the bundle make site builds, the one both runs of a worktree would share if the recipe served it.
func (sb *browserRecipeSandbox) sharedBundle() string {
	return filepath.Join(sb.clone, "bin", "site")
}

// goTestRecord is one run of the stand-in go test: the bundle directory it was handed, the files that directory
// held then, the first line of its page, and the DevTools address it was handed.
type goTestRecord struct {
	bundle   string
	files    string
	page     string
	devtools string
}

// goTestRecords reads the lines the stand-in go command logged, one per go test run, in the order they were
// written; there are as many as there were runs of the smoke test.
func (sb *browserRecipeSandbox) goTestRecords(t *testing.T) []goTestRecord {
	t.Helper()

	body, err := os.ReadFile(sb.goLog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		t.Fatalf("read %s: %v", sb.goLog, err)
	}

	var records []goTestRecord

	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		if line == "" {
			continue
		}

		record := goTestRecord{}

		for _, field := range strings.Split(line, "\t") {
			name, value, ok := strings.Cut(field, "=")
			if !ok {
				t.Fatalf("the stand-in go command logged %q, which is no field=value pair", line)
			}

			switch name {
			case "bundle":
				record.bundle = value
			case "files":
				record.files = value
			case "page":
				record.page = value
			case "devtools":
				record.devtools = value
			default:
				t.Fatalf("the stand-in go command logged the unknown field %q in %q", name, line)
			}
		}

		records = append(records, record)
	}

	return records
}

// browserRecipeRun is one run of the sandbox's make: what it printed, how it ended, and how long it took.
type browserRecipeRun struct {
	stdout   string
	stderr   string
	err      error
	timedOut bool
	elapsed  time.Duration
}

// output is what the run printed on both streams, which is what make and its recipe say together.
func (run browserRecipeRun) output() string {
	return run.stdout + run.stderr
}

// runBrowserMake runs make with args in dir, under env alone and with dir as its working directory, and gives up
// on it after timeout, killing the process group make, its recipe's shell and that shell's children are in: the
// recipe under test may wait a minute for a log line that never comes, and the package must not wait with it.
func runBrowserMake(dir string, args []string, env map[string]string, timeout time.Duration) browserRecipeRun {
	cmd := exec.Command("make", append([]string{"-C", dir, "--no-print-directory"}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()

	if err := cmd.Start(); err != nil {
		return browserRecipeRun{err: fmt.Errorf("start make %v: %w", args, err), elapsed: time.Since(start)}
	}

	// The buffers are read only after done is closed, which orders that read after Wait finished writing them.
	var status error

	done := make(chan struct{})

	go func() {
		status = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return browserRecipeRun{stdout: stdout.String(), stderr: stderr.String(), err: status, elapsed: time.Since(start)}
	case <-time.After(timeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done

		return browserRecipeRun{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			err:      status,
			timedOut: true,
			elapsed:  time.Since(start),
		}
	}
}

// TestMakeTestBrowserRecipeServesABundleOfItsOwn pins issue #423 item 2: a second make test-browser in the same
// worktree could delete the bundle the first run's browser was serving, because both runs served the shared
// bin/site that make site removes and rebuilds. So two runs at once, as two agents in one worktree start them,
// must both pass, and each must serve a directory of its own - not bin/site, not a directory inside it - which it
// cleans up when it is done. bin/site is the release's and the deploy's, make site's alone: a run of make
// test-browser neither creates nor touches it, not even through a site prerequisite, which is what would remove
// and rebuild it under the other run's browser.
func TestMakeTestBrowserRecipeServesABundleOfItsOwn(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeRunning)

	const runs = 2

	// bin/site is what make site builds, and a run of make test-browser must have no business with it: it is
	// absent before the two runs, so whether either run built, removed or touched it is read from whether it is
	// there afterwards. A sandbox that already held it would hide that.
	shared := sb.sharedBundle()

	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Fatalf("%s is already there before either run of make test-browser, so this test cannot tell whether a run created it: %v",
			shared, err)
	}

	// Both runs go at once: the stand-in container holds each one at its docker run until the other arrives, so
	// both serve their bundle at the same moment, which is the moment the shared one used to be deleted from
	// under the first run (fakeContainerCommand, the rendezvous).
	results := make([]browserRecipeRun, runs)

	var wg sync.WaitGroup

	for at := range results {
		wg.Add(1)

		go func(at int) {
			defer wg.Done()

			results[at] = runBrowserMake(sb.clone, []string{testBrowserTarget}, sb.env, browserRecipeTimeout)
		}(at)
	}

	wg.Wait()

	for at, run := range results {
		switch {
		case run.timedOut:
			t.Errorf("run %d of make test-browser was still running after %s and the test stopped it: two runs at once in one worktree must both finish, so neither run may wait for the other's bundle (issue #423 item 2)\n%s",
				at+1, browserRecipeTimeout, run.output())
		case run.err != nil:
			t.Errorf("run %d of make test-browser failed although its container and its smoke test answered as they do on a working machine, so two runs at once in one worktree do not both pass (issue #423 item 2): %v\n%s",
				at+1, run.err, run.output())
		}
	}

	records := sb.goTestRecords(t)

	if len(records) != runs {
		t.Fatalf("the stand-in go command ran the smoke test %d times, want %d: every run of make test-browser runs it once against the bundle it serves (issue #423 item 2)\n%s",
			len(records), runs, sb.goLogText(t))
	}

	for at, record := range records {
		served := filepath.Clean(record.bundle)

		if record.devtools == "" {
			t.Errorf("run %d of make test-browser handed the smoke test no DevTools address: the recipe must read Chromium's address back from its log and pass it on\n%s",
				at+1, sb.goLogText(t))
		}

		if served == shared || strings.HasPrefix(served, shared+string(filepath.Separator)) {
			t.Errorf("run %d of make test-browser served %s, the bundle make site builds: a second run's make site removes and rebuilds that directory, under the browser the first run is driving (issue #423 item 2)",
				at+1, served)

			continue
		}

		if !strings.Contains(record.files, "index.html") || record.page != bundlePageLine {
			t.Errorf("run %d of make test-browser served %s, which held %q and whose index.html began %q, want the bundle's page %q: the run must serve the bundle make site built, not an empty directory of its own (issue #423 item 2)",
				at+1, served, record.files, record.page, bundlePageLine)
		}

		if _, err := os.Stat(served); err == nil {
			t.Errorf("run %d of make test-browser left the bundle it served, %s, behind: each run's own bundle must be cleaned up when the run ends, or every run of the worktree adds a directory to it (issue #423 item 2)",
				at+1, served)
		}
	}

	if records[0].bundle == records[1].bundle {
		t.Errorf("both runs of make test-browser served %s: each run must serve a bundle of its own, or the second run's make site deletes the one the first run's browser reads (issue #423 item 2)",
			records[0].bundle)
	}

	// make site is the release's and the deploy's: a run of make test-browser builds the bundle it serves into a
	// directory of its own, and leaves bin/site alone. The recipe has no site prerequisite any more, so this reads
	// whether a run built, removed or touched bin/site all the same, as an edit that put the prerequisite back - the
	// very race under test - or a recipe that built the shared bundle would.
	if _, err := os.Stat(shared); err == nil {
		t.Errorf("make test-browser created %s, the bundle make site builds: the recipe builds the bundle it serves into a directory of its own, so a run must neither create nor remove that directory, which the release publishes and the deploy serves, and which a second run's make site would rebuild under the first run's browser (issue #423 item 2)",
			shared)
	} else if !os.IsNotExist(err) {
		t.Errorf("look for %s after two runs of make test-browser: %v", shared, err)
	}
}

// TestMakeSiteStillWritesTheSharedBundle pins that make site itself is unchanged by issue #423 item 2: a per-run
// bundle is what a run of make test-browser serves, and make site keeps writing bin/site and its archive beside
// it, which the release publishes and the deploy serves.
func TestMakeSiteStillWritesTheSharedBundle(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeRunning)

	run := runBrowserMake(sb.clone, []string{"site"}, sb.env, browserRecipeTimeout)
	if run.timedOut {
		t.Fatalf("make site was still running after %s and the test stopped it\n%s", browserRecipeTimeout, run.output())
	}

	if run.err != nil {
		t.Fatalf("make site failed against the stand-in builder that writes what it is told to: %v\n%s", run.err, run.output())
	}

	shared := sb.sharedBundle()

	page, err := os.ReadFile(filepath.Join(shared, "index.html"))
	if err != nil {
		t.Fatalf("make site wrote no bundle page into %s: %v", shared, err)
	}

	if got := strings.TrimSuffix(string(page), "\n"); got != bundlePageLine {
		t.Errorf("make site's bundle page in %s begins %q, want %q", shared, got, bundlePageLine)
	}

	// The archive goes beside the bundle, named for the site and the version it holds: this asks that make site wrote
	// one there, not which version its name spells - untagged here, the recipe names it for dev.
	candidates, err := filepath.Glob(filepath.Join(sb.clone, "bin", "shift2eu-site*.tar.gz"))
	if err != nil {
		t.Fatalf("look for make site's archive: %v", err)
	}

	if len(candidates) != 1 {
		t.Errorf("make site wrote %d archives matching %s, want 1: the bundle's archive goes beside it, bin/shift2eu-site_<version>.tar.gz (PRD #297)",
			len(candidates), filepath.Join(sb.clone, "bin", "shift2eu-site*.tar.gz"))
	}
}

// TestMakeTestBrowserRecipeFailsAtOnceWhenTheContainerIsGone pins issue #423 item 3: the recipe waited up to 60
// seconds for Chromium's DevTools line, so a container that exited at once cost the full minute and then said the
// line never came. It must notice that the container is gone, and fail at once, naming the status it exited with
// and the tail of its log.
func TestMakeTestBrowserRecipeFailsAtOnceWhenTheContainerIsGone(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeDead)

	run := runBrowserMake(sb.clone, []string{testBrowserTarget}, sb.env, deadContainerTimeout)
	output := run.output()

	switch {
	case run.timedOut:
		t.Fatalf("make test-browser was still running after %s although its container had already exited with status %s: the recipe waits out its 60 seconds for a DevTools line a dead container never logs, and a maintainer waits with it (issue #423 item 3)\n%s",
			run.elapsed, deadContainerStatus, output)
	case run.err == nil:
		t.Fatalf("make test-browser exited 0 although its container had exited at once with status %s: the run drove no browser and must not report a passing smoke test (issue #423 item 3)\n%s",
			deadContainerStatus, output)
	}

	if run.elapsed > deadContainerDeadline {
		t.Errorf("make test-browser took %s to fail on a container that exited at once, want under %s, well inside the 60 seconds the recipe waits for a log line (issue #423 item 3)\n%s",
			run.elapsed, deadContainerDeadline, output)
	}

	if !exitStatusWord.MatchString(output) {
		t.Errorf("the failure does not name the status %s its container exited with, so a maintainer cannot tell a dead Chromium from a broken recipe (issue #423 item 3)\n%s",
			deadContainerStatus, output)
	}

	if !strings.Contains(output, deadContainerLogLine) {
		t.Errorf("the failure does not show the tail of the container's log, %q, which says why Chromium never came up (issue #423 item 3)\n%s",
			deadContainerLogLine, output)
	}

	if records := sb.goTestRecords(t); len(records) != 0 {
		t.Errorf("make test-browser ran the smoke test %d times although its container had exited at once, so no browser was there to drive (issue #423 item 3)\n%s",
			len(records), sb.goLogText(t))
	}
}

// goLogText is what the stand-in go command has logged so far, for a failure message that shows why a test found
// no record where it wanted one.
func (sb *browserRecipeSandbox) goLogText(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(sb.goLog)
	if err != nil {
		if os.IsNotExist(err) {
			return "the stand-in go command logged nothing: it never ran as a test"
		}

		t.Fatalf("read %s: %v", sb.goLog, err)
	}

	return string(body)
}
