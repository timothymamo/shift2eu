//go:build unix

// Pins what two runs of make test-browser at once do to the site's bundle, what a run does when the container it
// started Chromium in is already gone, what it does when the container command merely fails to read that
// container's state, and what an interrupted run cleans up - including when a second signal of the same kind
// reaches it while it cleans up (issue #423 items 2 and 3 and the reviewer's findings S2 and N2, PRD #297
// milestone 2, ADR 0036). It pins too that the removal the EXIT trap runs is bounded, so a container runtime that
// never answers it cannot hold the run for good (issue #449). Each test runs the real recipe of the real Makefile, in a clone of it, with the two
// commands the recipe runs replaced by stand-ins - the container command, the recipe's $(DOCKER), and go - so no
// test needs a container runtime, a browser or a build of the site, and no test writes anything into the worktree
// it runs from. The stand-ins answer the way the tools they stand for do, whatever the recipe asks them, so the
// tests pin the recipe's behaviour and not one spelling of it.
//
// The stand-ins poll with a fraction of a second - sleep 0.05 - so they assume a sleep that takes one, as GNU
// coreutils, BSD/macOS and busybox do, and not the POSIX one, whose argument is whole seconds.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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

	// goneInspectLogLine is what the stand-in container command prints on standard error when it cannot read the
	// state of a container that is no longer there, as docker inspect does for a container it cannot find. The
	// recipe must show this line, and not the tail of a log or a status, when it gives up on a container whose
	// state it could not read three times in a row (issue #423, the reviewer's finding S2).
	goneInspectLogLine = "Error: No such object: contract-id"

	// bundlePageLine is the first line of the page the stand-in site builder writes into the bundle: a run that
	// serves the bundle's files is told from one that serves an empty directory by reading it back.
	bundlePageLine = "<!doctype html>"
)

// The containers the stand-in container command stands for.
const (
	// dockerModeRunning is a Chromium that runs and logs its DevTools address: the run a test expects to succeed.
	dockerModeRunning = "running"
	// dockerModeDead is a container that started and exited at once, with deadContainerStatus, after printing
	// deadContainerLogLine: what the recipe has to fail on at once instead of waiting for a log line that never
	// comes (issue #423 item 3).
	dockerModeDead = "dead"
	// dockerModeInspectTrouble is a Chromium that runs, but whose container the container command fails to
	// inspect on the calls SHIFT2EU_TEST_DOCKER_INSPECT_PLAN names, printing goneInspectLogLine, and whose log
	// answers nothing for the first SHIFT2EU_TEST_DOCKER_LOG_MISSES calls: the container runtime that is slow to
	// answer, or the container that is on its way out (issue #423, the reviewer's finding S2).
	dockerModeInspectTrouble = "inspect-trouble"
	// dockerModeSilent is a Chromium that runs and never logs its DevTools address: the run a test interrupts
	// with a signal, to pin what the recipe's traps clean up (issue #423, the reviewer's finding S2).
	dockerModeSilent = "silent"
	// dockerModeRmWedged is a Chromium that runs and answers every call as a working runtime answers it, except the
	// removal: its rm never returns, as the call to a wedged daemon never does. It is the container runtime issue
	// #449 is about, whose one unanswered call used to hold make test-browser in its EXIT trap for good.
	dockerModeRmWedged = "rm-wedged"
)

// One run of make test-browser gets this long before the test stops it: the stand-ins answer in milliseconds, so
// a run that is still going is one whose recipe waits for something that never comes.
const browserRecipeTimeout = 60 * time.Second

// removalDeadlineSetting is the make variable, and the value the case below gives it, that bounds the removal make
// test-browser's EXIT trap runs (issue #449): one second, so a run that bounds the removal ends in seconds where the
// guard timeout below only stops a run that waits for a removal that never comes.
const removalDeadlineSetting = "TEST_BROWSER_RM_DEADLINE=1"

// The cases a run must fail at once get this long before the test stops the run: the stand-ins answer in
// milliseconds, so the run ends well inside it, and this is only the hang guard - a recipe that waits where the test
// waits would hold the package here - never the thing under test. What such a run did is counted below, in calls of
// the stand-in container command: a count is the same on every machine, where a wall clock is not (issue #423, the
// reviewer's advice on PR #443).
const failFastTimeout = 20 * time.Second

// The counts a run that must fail at once is read back by, in calls of the stand-in container command rather than in
// seconds: each is far below what a run that waited for a DevTools line makes, so none of them can pass by waiting
// (issue #423, the reviewer's advice on PR #443).
const (
	// deadContainerPolls is how many polls of its wait loop a run may make before it notices that its container is
	// gone: one, whose log read finds no DevTools line and whose inspect then reports the container exited, is the
	// last. The second log read is the tail the failure shows its maintainer. A run that went on waiting for the
	// line - the recipe issue #423 item 3 fixed - makes the loop's 60 polls, a log read and an inspect on each, 120
	// calls where this bound allows two of either.
	deadContainerPolls = 2

	// failedInspectPolls is how many polls a run may make while the container command fails to read the container's
	// state: the third failed inspect in a row fails the run, so three polls, one log read each. A run that went on
	// waiting for the DevTools line instead makes the loop's 60 (issue #423, the reviewer's finding S2).
	failedInspectPolls = 3
)

// waitDeadline is how long a polling helper gives the state it waits for before it reports that the state never
// came: the stand-ins answer in milliseconds, so it is the guard that keeps a recipe off the rails from holding the
// test, not a duration a test asserts on.
const waitDeadline = 15 * time.Second

// exitStatusWord matches deadContainerStatus as a number of its own: "exited with status 3", "exit code 3" and
// "Error 3" all hold it, where a line number or a status that merely holds a 3 (13, 130, 403) does not.
var exitStatusWord = regexp.MustCompile(`(^|[^0-9])3([^0-9]|$)`)

// fakeContainerCommand stands in for the container command make test-browser starts Chromium with, the recipe's
// $(DOCKER), so the recipe runs on a machine that has no container runtime and starts no browser, and a test can
// say exactly what the container does. It records its argv and the pid of its parent shell, as the stand-ins of this
// package do, and answers the subcommands a recipe that starts a container, reads its log and checks that it still
// runs may use.
//
// SHIFT2EU_TEST_DOCKER_MODE says which container it stands for: dockerModeRunning, one that runs and logs
// devtoolsLogLine; dockerModeDead, one that exited at once with SHIFT2EU_TEST_DOCKER_STATUS after printing
// SHIFT2EU_TEST_DOCKER_LOG_LINE; dockerModeInspectTrouble, one that runs while the container command fails on the
// calls SHIFT2EU_TEST_DOCKER_INSPECT_PLAN names and whose log answers nothing for the first
// SHIFT2EU_TEST_DOCKER_LOG_MISSES calls; dockerModeSilent, one that runs and never logs an address; and
// dockerModeRmWedged, one that runs and answers as dockerModeRunning's does, except that its rm never returns:
// the container runtime that stopped answering the removal the EXIT trap makes (issue #449). A run in that mode
// leaves a stand-in rm running once the recipe gives up on it, so that rm writes its own pid into the file
// SHIFT2EU_TEST_DOCKER_RM_PID names before it waits, and the test ends that pid and no other.
//
// SHIFT2EU_TEST_CLEANUP_RENDEZVOUS names the stand-in that holds while the recipe's EXIT trap is in its rm call -
// container for this one, bundle for the stand-in rm - so the test can hand the run the second signal of the same
// kind there (issue #423, commit 93b859d). The hold is a pair of files, never a pause: see hold_for_the_second_signal.
//
// In the trouble mode SHIFT2EU_TEST_DOCKER_INSPECT_PLAN holds one character per inspect call - f fails the call the
// way the container command fails for a container it cannot find, printing SHIFT2EU_TEST_DOCKER_INSPECT_ERROR on
// standard error and exiting 1, o answers with the container's state - and its last character holds for the calls
// beyond its end. A run in dockerModeRunning meets the other run of the same worktree at the rendezvous inside
// run, so both runs hold what they serve at the same time, whichever of them comes first (issue #423 item 2). A
// subcommand none of these is fails the run with status 64 and names itself, rather than answering nothing and
// leaving the recipe to misread the silence.
const fakeContainerCommand = `#!/bin/sh
log="${SHIFT2EU_TEST_DOCKER_LOG:?set SHIFT2EU_TEST_DOCKER_LOG}"
mode="${SHIFT2EU_TEST_DOCKER_MODE:?set SHIFT2EU_TEST_DOCKER_MODE}"
{
	printf 'tool=docker\n'
	printf 'ppid=%s\n' "$PPID"
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
# hold_for_the_second_signal tells the test, through a file of the rendezvous directory, that the recipe's EXIT trap
# is in this call, and stays in the call until the test has written its own file back: the shell waits for this call,
# so a signal the test sends between the two files reaches it while the trap is in it. Two files order the two sides
# on every machine, where a pause in this call would only make the moment likelier.
hold_for_the_second_signal() {
	: > "${SHIFT2EU_TEST_RENDEZVOUS:?set SHIFT2EU_TEST_RENDEZVOUS}/cleanup.$$" || exit 1
	waited=0
	while [ ! -e "$SHIFT2EU_TEST_RENDEZVOUS/cleanup.go" ]; do
		waited=$((waited + 1))
		# 800 polls of 0.05 s: 40 seconds, inside the 60 seconds browserRecipeTimeout gives a run.
		if [ "$waited" -gt 800 ]; then
			echo 'fake docker: the test never released the cleanup' >&2
			exit 64
		fi
		sleep 0.05
	done
}
if [ "$mode" = dead ]; then
	running=false
	state=exited
	exitcode="${SHIFT2EU_TEST_DOCKER_STATUS:?set SHIFT2EU_TEST_DOCKER_STATUS}"
else
	running=true
	state=running
	exitcode=0
fi
# The trouble mode's two knobs: the outcome of each inspect call, and how many of its first log calls answer
# nothing before the DevTools address is there. Empty and 0 in the other modes, which answer every call.
inspect_plan="${SHIFT2EU_TEST_DOCKER_INSPECT_PLAN:-}"
log_misses="${SHIFT2EU_TEST_DOCKER_LOG_MISSES:-0}"
case "$cmd" in
run)
	if [ "$mode" = running ]; then
		: > "${SHIFT2EU_TEST_RENDEZVOUS:?set SHIFT2EU_TEST_RENDEZVOUS}/run.$$" || exit 1
		waited=0
		# A poll of the rendezvous directory, not a pause that orders anything: the two runs meet when both
		# files are there, however long either took to arrive.
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
		exit 0
	fi
	if [ "$mode" = silent ]; then
		# A Chromium that never logs an address: the run a test interrupts while it waits for one.
		exit 0
	fi
	if [ "$(grep -c '^arg=logs$' "$log" || true)" -le "$log_misses" ]; then
		# A log with nothing in it yet, the way a Chromium that is still starting logs no address: the run has
		# to keep polling until the misses are spent, and then find the address.
		exit 0
	fi
	printf '%s\n' "${SHIFT2EU_TEST_DEVTOOLS_LINE:?set SHIFT2EU_TEST_DEVTOOLS_LINE}"
	;;
inspect)
	if [ -n "$inspect_plan" ]; then
		# One character of the plan per inspect call, its last character repeating: this call's outcome is the
		# one the count of inspect calls in the log points at.
		calls=$(grep -c '^arg=inspect$' "$log" || true)
		outcome=$inspect_plan
		while [ "${#outcome}" -gt 1 ] && [ "$calls" -gt 1 ]; do
			outcome=${outcome#?}
			calls=$((calls - 1))
		done
		case "$outcome" in
		f*)
			printf '%s\n' "${SHIFT2EU_TEST_DOCKER_INSPECT_ERROR:?set SHIFT2EU_TEST_DOCKER_INSPECT_ERROR}" >&2
			exit 1
			;;
		esac
	fi
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
rm)
	# Where the trap's rm -f is, a case that hands the run a second signal of the same kind once the trap is in
	# this call: the trap has to ignore it, or its own exit ends the shell before the rest of the cleanup runs.
	if [ "${SHIFT2EU_TEST_CLEANUP_RENDEZVOUS:-}" = container ]; then
		hold_for_the_second_signal
	fi
	if [ "$mode" = rm-wedged ]; then
		# The removal that never comes back, the call this whole mode exists for: the recipe has to bound it, and a
		# run that does not is one the test's guard timeout stops. The call itself is in the log already, above; the
		# pid this stand-in writes down is what a test ends a stand-in the recipe gave up on by (issue #449).
		printf '%s\n' "$$" > "${SHIFT2EU_TEST_DOCKER_RM_PID:?set SHIFT2EU_TEST_DOCKER_RM_PID}" || exit 1
		while :; do
			sleep 0.05
		done
	fi
	;;
kill | stop | start | pull)
	:
	;;
*)
	echo "fake docker: unexpected command: $cmd" >&2
	exit 64
	;;
esac
exit 0
`

// fakeRemoveCommand stands in for rm in a sandbox a test asks for it in, so a test can hand the recipe a second
// signal of its own while the EXIT trap removes the bundle directory, the first of the two removals the fix ordered
// (issue #423, commit 93b859d). It does what rm does, and hands the rest of its work to the real rm once the test has
// let it go on. SHIFT2EU_TEST_CLEANUP_RENDEZVOUS=bundle is what a sandbox that writes this stand-in sets.
const fakeRemoveCommand = `#!/bin/sh
# The same hold as the stand-in container command's hold_for_the_second_signal, for the other removal the trap runs:
# the test writes the file it waits on once it has sent the second signal, and the real rm runs after it.
hold_for_the_second_signal() {
	: > "${SHIFT2EU_TEST_RENDEZVOUS:?set SHIFT2EU_TEST_RENDEZVOUS}/cleanup.$$" || exit 1
	waited=0
	while [ ! -e "$SHIFT2EU_TEST_RENDEZVOUS/cleanup.go" ]; do
		waited=$((waited + 1))
		if [ "$waited" -gt 800 ]; then
			echo 'fake rm: the test never released the cleanup' >&2
			exit 64
		fi
		sleep 0.05
	done
}
if [ "${SHIFT2EU_TEST_CLEANUP_RENDEZVOUS:-}" = bundle ]; then
	hold_for_the_second_signal
fi
exec /bin/rm "$@"
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
	root      string            // the directory holding everything, from newSandboxRoot()
	clone     string            // the clone make runs in, from writeClone
	binDir    string            // first on PATH: the stand-in docker and go
	env       map[string]string // the environment one run gets, never the test process's own
	goLog     string            // one line per stand-in go test run: what it was handed, and what that held
	dockerLog string            // one tool=docker line and one arg= line per stand-in container command call
}

// newBrowserRecipeSandbox builds a sandbox whose recipe runs against a container in mode, and removes it with the
// test.
func newBrowserRecipeSandbox(t *testing.T, mode string) *browserRecipeSandbox {
	t.Helper()

	root := newSandboxRoot(t)
	sb := &browserRecipeSandbox{
		root:      root,
		binDir:    filepath.Join(root, "bin"),
		goLog:     filepath.Join(root, "go.log"),
		dockerLog: filepath.Join(root, "docker.log"),
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
		"SHIFT2EU_TEST_DOCKER_LOG":      sb.dockerLog,
		"SHIFT2EU_TEST_DOCKER_MODE":     mode,
		"SHIFT2EU_TEST_DOCKER_STATUS":   deadContainerStatus,
		"SHIFT2EU_TEST_DOCKER_LOG_LINE": deadContainerLogLine,
		"SHIFT2EU_TEST_DEVTOOLS_LINE":   devtoolsLogLine,
		"SHIFT2EU_TEST_BUNDLE_PAGE":     bundlePageLine,
		"SHIFT2EU_TEST_RENDEZVOUS":      filepath.Join(root, "rendezvous"),

		// The trouble mode's knobs, quiet by default: a test that wants a failed inspect, or a log that answers
		// nothing for a while, sets them itself (issue #423, the reviewer's finding S2).
		"SHIFT2EU_TEST_DOCKER_INSPECT_PLAN":  "",
		"SHIFT2EU_TEST_DOCKER_INSPECT_ERROR": goneInspectLogLine,
		"SHIFT2EU_TEST_DOCKER_LOG_MISSES":    "0",

		// Where the stand-in rm of the wedged mode writes its pid down before it waits, so a stand-in the recipe
		// gave up on is one the test can end, and only by that pid (CLAUDE.md, rule 10; issue #449).
		"SHIFT2EU_TEST_DOCKER_RM_PID": filepath.Join(root, "rm.pid"),

		// The stand-in that holds for the second signal while the run cleans up, quiet by default: a test that pins
		// the second signal names the removal it waits in, and writes the stand-in rm when that one is the bundle's
		// (issue #423).
		"SHIFT2EU_TEST_CLEANUP_RENDEZVOUS": "",
	}

	return sb
}

// sharedBundle is the bundle make site builds, the one both runs of a worktree would share if the recipe served it.
func (sb *browserRecipeSandbox) sharedBundle() string {
	return filepath.Join(sb.clone, "bin", "site")
}

// writeRemoveStandIn puts the stand-in rm first on PATH, so a test can send the recipe a second signal of its own
// while the EXIT trap removes the bundle directory (fakeRemoveCommand, issue #423). The sandboxes of the other tests
// have no rm of their own and run the machine's.
func (sb *browserRecipeSandbox) writeRemoveStandIn(t *testing.T) {
	t.Helper()

	writeFileMode(t, filepath.Join(sb.binDir, "rm"), []byte(fakeRemoveCommand), 0o700)
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

// runBrowserMake runs program, the make makeProgram names, with args in dir, under env alone and with dir as its
// working directory, and gives up on it after timeout, killing the process group make, its recipe's shell and that
// shell's children are in: the recipe under test may wait a minute for a log line that never comes, and the
// package must not wait with it. It takes the program rather than the test, so it can run off the test's
// goroutine, where makeProgram could not fail the test.
func runBrowserMake(program, dir string, args []string, env map[string]string, timeout time.Duration) browserRecipeRun {
	cmd := exec.Command(program, append([]string{"-C", dir, "--no-print-directory"}, args...)...)
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

	program := makeProgram(t)

	for at := range results {
		wg.Add(1)

		go func(at int) {
			defer wg.Done()

			results[at] = runBrowserMake(program, sb.clone, []string{testBrowserTarget}, sb.env, browserRecipeTimeout)
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
			t.Errorf("run %d of make test-browser served %s, which held %q and whose index.html began %q, want the bundle's page %q: the run must serve the bundle its own build wrote, not an empty directory (issue #423 item 2)",
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

	run := runBrowserMake(makeProgram(t), sb.clone, []string{"site"}, sb.env, browserRecipeTimeout)
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
// and the tail of its log. That it failed at once is read back from the calls the stand-ins saw - the run never got
// past its first poll - and not from a wall clock (issue #423, the reviewer's advice on PR #443).
func TestMakeTestBrowserRecipeFailsAtOnceWhenTheContainerIsGone(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeDead)

	run := runBrowserMake(makeProgram(t), sb.clone, []string{testBrowserTarget}, sb.env, failFastTimeout)
	output := run.output()

	switch {
	case run.timedOut:
		t.Fatalf("make test-browser was still running after %s and the test stopped it, although its container had already exited with status %s: the recipe waits out its 60 seconds for a DevTools line a dead container never logs, and a maintainer waits with it (issue #423 item 3)\n%s",
			failFastTimeout, deadContainerStatus, output)
	case run.err == nil:
		t.Fatalf("make test-browser exited 0 although its container had exited at once with status %s: the run drove no browser and must not report a passing smoke test (issue #423 item 3)\n%s",
			deadContainerStatus, output)
	}

	// A run that noticed the dead container on its first poll read the log twice - once on that poll, once for the
	// tail it shows - and inspected the container twice, for its state and for the status it exited with. A run that
	// waited for the DevTools line instead makes the loop's 60 polls and so more than a hundred calls: the count
	// says which of the two happened, whatever the machine's load (issue #423, the reviewer's advice on PR #443).
	if got := sb.logCalls(t); got > deadContainerPolls {
		t.Errorf("make test-browser read the container's log %d times before it failed, want at most %d: a run that notices its container is gone makes its first poll and no more, where a run that waits for the DevTools line reads the log on every one of the recipe's 60 polls (issue #423 item 3)\n%s",
			got, deadContainerPolls, sb.dockerLogText(t))
	}

	if got := sb.inspectCalls(t); got > deadContainerPolls {
		t.Errorf("make test-browser inspected the container %d times before it failed, want at most %d: a run that notices its container is gone checks it on its first poll and no more, where a run that waits for the DevTools line inspects it on every one of the recipe's 60 polls (issue #423 item 3)\n%s",
			got, deadContainerPolls, sb.dockerLogText(t))
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

// TestMakeTestBrowserRecipeKeepsPollingAfterAFailedInspect pins the half of the reviewer's finding S2 (issue #423)
// that keeps a working run alive: a container command that fails to read the container's state is no verdict on the
// container. The recipe counts the failed inspects, so one of them alone does not fail the run, and an inspect that
// answers again resets the count, so a run whose inspects fail and answer again, three times over, still reaches
// its DevTools line and passes.
func TestMakeTestBrowserRecipeKeepsPollingAfterAFailedInspect(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeInspectTrouble)
	// f,o,f,o,f: five inspects of which three fail, three in a row only if a success did not reset the count. The
	// log answers nothing for the first five polls, so the run meets all five inspects before its DevTools line.
	sb.env["SHIFT2EU_TEST_DOCKER_INSPECT_PLAN"] = "fofof"
	sb.env["SHIFT2EU_TEST_DOCKER_LOG_MISSES"] = "5"

	run := runBrowserMake(makeProgram(t), sb.clone, []string{testBrowserTarget}, sb.env, browserRecipeTimeout)
	output := run.output()

	switch {
	case run.timedOut:
		t.Fatalf("make test-browser was still running after %s although its container ran and answered every inspect but three: a failed inspect alone is no verdict on the container, and the run must keep polling until the DevTools line is there (issue #423, the reviewer's finding S2)\n%s",
			run.elapsed, output)
	case run.err != nil:
		t.Fatalf("make test-browser failed although its container ran and its inspect merely failed three times, each followed by an inspect that answered: a failed inspect alone is no verdict on the container, and three failed inspects in a row is the only count that fails a run (issue #423, the reviewer's finding S2): %v\n%s",
			run.err, output)
	}

	if strings.Contains(output, goneInspectLogLine) {
		t.Errorf("the run gave up on the container and showed inspect's error %q, although no three inspects in a row failed: a failed inspect alone must not fail the run, and a success in between must reset the count (issue #423, the reviewer's finding S2)\n%s",
			goneInspectLogLine, output)
	}

	if got := sb.inspectCalls(t); got != 5 {
		t.Errorf("the recipe inspected the container %d times, want 5, the number of polls the stand-in's log answered nothing for: the run must check the container on every poll it waits through (issue #423, the reviewer's finding S2)\n%s",
			got, sb.dockerLogText(t))
	}

	records := sb.goTestRecords(t)

	if len(records) != 1 {
		t.Fatalf("make test-browser ran the smoke test %d times, want 1: a run whose container is there must reach its DevTools line and drive the browser once (issue #423, the reviewer's finding S2)\n%s",
			len(records), sb.goLogText(t))
	}

	if records[0].devtools == "" {
		t.Errorf("the run handed the smoke test no DevTools address, so it drove no browser: the address the container logged after the failed inspects is the one the smoke test must get (issue #423, the reviewer's finding S2)\n%s",
			sb.goLogText(t))
	}
}

// TestMakeTestBrowserRecipeFailsAtOnceWhenInspectKeepsFailing pins the other half of the reviewer's finding S2
// (issue #423): three failed inspects in a row mean the container is gone, and a run that would otherwise wait out
// its 60 seconds must fail at once, showing inspect's last error, or a maintainer watching a container runtime
// that answers nothing learns only that no DevTools line came. That it failed at once is read back from the calls
// the stand-ins saw - three polls and no more - and not from a wall clock (the reviewer's advice on PR #443).
func TestMakeTestBrowserRecipeFailsAtOnceWhenInspectKeepsFailing(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeInspectTrouble)
	// Every inspect fails, and the log answers nothing for longer than the recipe's own 60 polls: a run that
	// waited for the DevTools line instead of for the container would wait the minute out.
	sb.env["SHIFT2EU_TEST_DOCKER_INSPECT_PLAN"] = "f"
	sb.env["SHIFT2EU_TEST_DOCKER_LOG_MISSES"] = "60"

	run := runBrowserMake(makeProgram(t), sb.clone, []string{testBrowserTarget}, sb.env, failFastTimeout)
	output := run.output()

	switch {
	case run.timedOut:
		t.Fatalf("make test-browser was still running after %s and the test stopped it, although its container command had failed to read the container for every poll: the recipe must count the failed inspects and fail on the third in a row, well inside the 60 seconds it waits for a DevTools line a gone container never logs (issue #423, the reviewer's finding S2)\n%s",
			failFastTimeout, output)
	case run.err == nil:
		t.Fatalf("make test-browser exited 0 although its container command could read the container's state on no poll: the run drove no browser and must not report a passing smoke test (issue #423, the reviewer's finding S2)\n%s",
			output)
	}

	if !strings.Contains(output, goneInspectLogLine) {
		t.Errorf("the failure does not show inspect's last error, %q, so a maintainer cannot tell a container runtime that cannot answer from a recipe that broke (issue #423, the reviewer's finding S2)\n%s",
			goneInspectLogLine, output)
	}

	if got := sb.inspectCalls(t); got != failedInspectPolls {
		t.Errorf("the recipe inspected the container %d times before it failed, want %d: three failed inspects in a row are what fail the run, and neither the first nor a fourth must decide it, where a run that waited for the DevTools line makes the recipe's 60 polls and 60 inspects (issue #423, the reviewer's finding S2)\n%s",
			got, failedInspectPolls, sb.dockerLogText(t))
	}

	// Three polls, so three log reads: a run that gave up on its third failed inspect read the log no more often,
	// where one that waited for the DevTools line would have read it 60 times.
	if got := sb.logCalls(t); got > failedInspectPolls {
		t.Errorf("make test-browser read the container's log %d times before it failed, want at most %d, one for each of the three polls whose inspect failed: a run that waits for the DevTools line reads it on every one of the recipe's 60 polls (issue #423, the reviewer's finding S2)\n%s",
			got, failedInspectPolls, sb.dockerLogText(t))
	}

	if records := sb.goTestRecords(t); len(records) != 0 {
		t.Errorf("make test-browser ran the smoke test %d times although the container's state could not be read, so no browser was there to drive (issue #423, the reviewer's finding S2)\n%s",
			len(records), sb.goLogText(t))
	}
}

// TestMakeTestBrowserRecipeBoundsItsContainerRemoval pins issue #449: the EXIT trap removed the Chromium container
// with a $(DOCKER) rm -f that nothing bounded, so a container runtime that never answered that one call held make
// test-browser for good - a run whose smoke test had long passed, and a maintainer who had to end it by hand, with
// the container still there. The trap must run the removal in the background, wait for it no longer than
// TEST_BROWSER_RM_DEADLINE seconds, and then say which container it could not remove and the command that removes
// it by hand, leaving the run's own status what it was.
//
// The stand-in container command's rm never returns here (dockerModeRmWedged) and every other call answers as a
// working runtime's does, so the run reaches the cleanup of a run that passed. That the recipe bounded the removal
// is read from what it printed and from the rm call the stand-in logged, and not from a wall clock: a run that waits
// for a removal that never comes is one the guard timeout below stops, where a run that bounds it ends well inside
// that guard - so no assertion here depends on how long a second or a poll of the deadline is (the reviewer's advice
// on PR #443).
func TestMakeTestBrowserRecipeBoundsItsContainerRemoval(t *testing.T) {
	t.Parallel()

	sb := newBrowserRecipeSandbox(t, dockerModeRmWedged)

	// The stand-in the recipe backgrounds is in the group of the make this test starts, which startBrowserMake ends
	// with the test. The check after it reads the pid the stand-in recorded of itself, so a wedged stand-in still
	// there is reported rather than left running: it must not outlive the test (CLAUDE.md, rule 10).
	var proc *browserRecipeProcess

	t.Cleanup(func() {
		if proc != nil {
			sb.endWedgedRemove(t, proc.pgid())
		}
	})

	proc = startBrowserMake(t, sb.clone, []string{testBrowserTarget, removalDeadlineSetting}, sb.env)

	run := proc.wait(browserRecipeTimeout)
	output := run.output()

	switch {
	case run.timedOut:
		t.Fatalf("make test-browser was still running after %s and the test stopped it, although its container answered every call and only the rm -f of its EXIT trap never came back: the trap must run that removal with a deadline of its own, TEST_BROWSER_RM_DEADLINE, and give up on it past that, or a container runtime that stops answering that one call holds the run for good, as a wedged daemon held every run before issue #449\n%s",
			browserRecipeTimeout, output)
	case run.err != nil:
		t.Errorf("make test-browser exited with %v although its container answered and its smoke test ran, and only the rm -f of its EXIT trap never came back: a cleanup that gives up on the removal must leave the run's own status what it was, or a slow container runtime fails a smoke test that passed (issue #449)\n%s",
			run.err, output)
	}

	calls := sb.dockerCalls(t)

	name := containerName(calls)
	if name == "" {
		t.Fatalf("the stand-in container command recorded no call that started a container with a --name, so this test cannot read back whether the run named the container it gave up on: %s",
			sb.dockerLogText(t))
	}

	if got := sb.callsTo(t, "rm"); got == 0 {
		t.Errorf("the stand-in container command was never asked to remove the container, although the run ended and its EXIT trap removes it: a cleanup that gives up on a removal that does not come back must still ask for it (issue #449)\n%s",
			sb.dockerLogText(t))
	}

	// The container is still there, and it holds the name the next run of the worktree wants: a maintainer who reads
	// the run's output must find both the container it could not remove and the command that removes it by hand, on
	// one line, since nothing else in the output names that container.
	if !holdsRemovalAdvice(output, name) {
		t.Errorf("the run's output holds no line naming %s, the container it gave up removing, with the command that removes it by hand, rm -f %s: the container outlives the run, so a maintainer who read the output alone is left with a name to guess and the container still holding it (issue #449)\n%s",
			name, name, output)
	}

	// The removal is the last thing the run does: a run whose work never reached it would hold no line to read back,
	// and this says which of the two happened rather than leaving it to the failure above.
	if records := sb.goTestRecords(t); len(records) != 1 {
		t.Errorf("make test-browser ran the smoke test %d times, want 1: the run's own work is done before its EXIT trap removes the container, so the stand-in that never answers that removal must cost the run nothing but the wait its deadline cuts short (issue #449)\n%s",
			len(records), sb.goLogText(t))
	}
}

// holdsRemovalAdvice reports whether output holds the line a run that gave up on its container's removal must
// print: the target's name, test-browser:, and the command that removes the container by hand, rm -f followed by its
// name, on one line, so a maintainer reads what to do without looking anything up.
func holdsRemovalAdvice(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "test-browser:") && strings.Contains(line, "rm -f "+name) {
			return true
		}
	}

	return false
}

// endWedgedRemove is the check that nothing the recipe gave up on outlives the test. The trap backgrounds its rm, so
// the run itself does not wait for it, and a stand-in still there at the end of the test would be left behind
// silently. It signals the pid the stand-in recorded of itself, and only while that pid is still in the group of the
// run: a pid the machine has reused for a process of its own belongs to another group and is left alone, and nothing
// is ever found by name (CLAUDE.md, rule 10). startBrowserMake ends that group first, so a stand-in that is gone
// already - as it is when the guard timeout killed the group - needs no signal and gets none.
func (sb *browserRecipeSandbox) endWedgedRemove(t *testing.T, pgid int) {
	t.Helper()

	pid := sb.wedgedRemovePid(t)
	if pid == 0 || syscall.Kill(pid, 0) != nil {
		return
	}

	if group, err := syscall.Getpgid(pid); err != nil || group != pgid {
		return
	}

	_ = syscall.Kill(pid, syscall.SIGKILL)

	if !waitUntil(func() bool { return syscall.Kill(pid, 0) != nil }) {
		t.Errorf("the stand-in rm the recipe gave up on, pid %d, the pid it recorded of itself, is still running after the test stopped it: a process the test started must not outlive it (CLAUDE.md, rule 10; issue #449)",
			pid)
	}
}

// Where a case's second signal of the same kind arrives, named by the stand-in that runs that removal and holds in it
// while the run cleans up (issue #423): in the container command's rm -f, or in the rm that removes the bundle
// directory. Each is the value the sandbox sets SHIFT2EU_TEST_CLEANUP_RENDEZVOUS to.
const (
	againOnContainer = "container"
	againOnBundle    = "bundle"
)

// TestDockerCallsReadsOnlyCompleteLines pins the reader of the stand-in container command's log: a line it finds
// while the stand-in is still writing it is no line of the log, so neither a truncated pid nor a truncated argument
// is read back as a whole one (issue #423, the reviewer's finding N2).
func TestDockerCallsReadsOnlyCompleteLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string // what the stand-in has written to its log so far
		// want is one entry per call read back: the shell's pid, then the argv as the stand-in logged it, so the
		// comparison shows a field the reader got wrong without reaching into the unexported call.
		want []string
	}{
		{
			name: "a log the stand-in has finished writing",
			body: "tool=docker\nppid=12345\narg=run\narg=--name\narg=s2eu-1\ntool=docker\nppid=12345\narg=rm\n",
			want: []string{"12345: run --name s2eu-1", "12345: rm"},
		},
		{
			name: "a ppid line the stand-in is still writing",
			body: "tool=docker\nppid=12345\narg=run\ntool=docker\nppid=12",
			want: []string{"12345: run", "0: "},
		},
		{
			name: "an arg line the stand-in is still writing",
			body: "tool=docker\nppid=12345\narg=inspect\ntool=docker\nppid=12345\narg=ins",
			want: []string{"12345: inspect", "12345: "},
		},
		{
			name: "an empty log",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := &browserRecipeSandbox{dockerLog: filepath.Join(t.TempDir(), "docker.log")}
			if err := os.WriteFile(sb.dockerLog, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write %s: %v", sb.dockerLog, err)
			}

			calls := sb.dockerCalls(t)

			var got []string

			for _, call := range calls {
				got = append(got, fmt.Sprintf("%d: %s", call.shell, strings.Join(call.argv, " ")))
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("the stand-in container command's log %q is read back as different calls (-want +got):\n%s\na line the stand-in is still writing is no call and no field of one, so a test that reads the log mid-run must not take a truncated pid or argument for a whole one (issue #423, the reviewer's finding N2)",
					tc.body, diff)
			}
		})
	}
}

// TestMakeTestBrowserRecipeCleansUpOnASignal pins the reviewer's finding S2 on issue #423 that an interrupted run
// cleans up after itself, however the signal reaches it. The recipe traps a hang-up (what closing the pane sends), an
// interrupt and a termination and exits through its EXIT trap, because dash, the VM's /bin/sh, runs no EXIT trap when
// an untrapped signal kills it: an interrupted run must still remove its Chromium container and the bundle directory
// of its own, and must not report a passing smoke test. The run is interrupted in its wait loop, with a container
// that runs and never logs a DevTools address - the state a hang-up meets most often, a run that will wait its
// minute out.
//
// A second signal of the same kind must not cost the cleanup: one arriving while the EXIT trap runs used to fire
// that signal's own trap, whose exit ended the shell and abandoned the rest of the cleanup, so the bundle directory
// was left behind every time. One does arrive in the wild - make forwards a SIGTERM it receives to its children, so
// a signal to the whole process group, as closing the pane sends it, hands the recipe's shell a second one - which
// is why the trap now ignores the three signals before it removes anything (issue #423, commit 93b859d). So the
// cases below hand the run that second signal itself, once while the trap removes the container and once while it
// removes the bundle directory - each time when the stand-in that runs that removal says it is in the call, never
// after a pause - and hand one signal to the whole process group besides, and every one of them must end with the
// container and the directory gone.
func TestMakeTestBrowserRecipeCleansUpOnASignal(t *testing.T) {
	t.Parallel()

	// The signals the recipe traps.
	signals := []struct {
		name   string
		signal syscall.Signal
	}{
		{
			name:   "a hang-up",
			signal: syscall.SIGHUP,
		},
		{
			name:   "an interrupt",
			signal: syscall.SIGINT,
		},
		{
			name:   "a termination",
			signal: syscall.SIGTERM,
		},
	}

	// How the signal reaches the run, and whether a second one of the same kind follows while it cleans up.
	deliveries := []struct {
		name string
		// group sends the signal to the whole process group make, the recipe's shell and that shell's children are
		// in, as closing the pane does, instead of to the recipe's shell alone.
		group bool
		// againOn says where the run meets a second signal of the same kind: "" for a single signal, againOnContainer
		// while the container command handles the rm -f that removes the container, and againOnBundle while the rm
		// that removes the bundle directory runs.
		againOn string
	}{
		{
			name: "once, to the recipe's shell",
		},
		{
			name:    "again, while its trap removes the container",
			againOn: againOnContainer,
		},
		{
			name:    "again, while its trap removes the bundle",
			againOn: againOnBundle,
		},
		{
			name:  "to the whole process group, as closing the pane does",
			group: true,
		},
	}

	for _, sig := range signals {
		for _, delivery := range deliveries {
			t.Run(sig.name+", "+delivery.name, func(t *testing.T) {
				t.Parallel()

				sb := newBrowserRecipeSandbox(t, dockerModeSilent)

				// The removal the second signal of the same kind meets: the stand-in that runs it writes a file when
				// it is in the call and waits for the test's, so the signal arrives while the trap is there on any
				// machine. A run sent one signal must clean up as well as a run sent two, since make hands the shell a
				// second one of its own when the whole group is terminated.
				if delivery.againOn != "" {
					sb.env["SHIFT2EU_TEST_CLEANUP_RENDEZVOUS"] = delivery.againOn
				}

				if delivery.againOn == againOnBundle {
					sb.writeRemoveStandIn(t)
				}

				proc := startBrowserMake(t, sb.clone, []string{testBrowserTarget}, sb.env)
				// The recipe is in its wait loop once it has read the container's log: the traps are in place, the
				// container runs, and the bundle of the run's own is built and being served.
				sb.waitForDockerCall(t, "logs")

				served := sb.runBundleDirs(t)
				if len(served) != 1 {
					t.Fatalf("the run holds %d per-run bundle directories in its TMPDIR while it waits for a DevTools line, %v, want 1: the run builds the bundle it serves into a directory of its own, and that directory is what an interrupted run has to clean up (issue #423, the reviewer's finding S2)",
						len(served), served)
				}

				if page, err := os.ReadFile(filepath.Join(served[0], "site", "index.html")); err != nil {
					t.Fatalf("read the page of the bundle the run serves, inside %s: %v", served[0], err)
				} else if got := strings.TrimSuffix(string(page), "\n"); got != bundlePageLine {
					t.Fatalf("the bundle the run serves begins %q, want %q: the run must serve the bundle its builder wrote before the test interrupts it (issue #423, the reviewer's finding S2)",
						got, bundlePageLine)
				}

				calls := sb.dockerCalls(t)

				name := containerName(calls)
				if name == "" {
					t.Fatalf("the stand-in container command recorded no call that started a container with a --name, so this test cannot tell whether the run removed it: %s",
						sb.dockerLogText(t))
				}

				shell := shellPid(t, calls)

				if delivery.group {
					if err := syscall.Kill(-proc.pgid(), sig.signal); err != nil {
						t.Fatalf("send %v to the process group %d of make test-browser, the one closing the pane reaches: %v", sig.signal, proc.pgid(), err)
					}
				} else if err := syscall.Kill(shell, sig.signal); err != nil {
					t.Fatalf("send %v to pid %d, the shell of the make test-browser recipe: %v", sig.signal, shell, err)
				}

				if delivery.againOn != "" {
					// The trap is in the removal the stand-in holds: the shell waits for that call, so a signal sent now
					// reaches the shell while the trap is in it, which is where the fix's ignored signals are what keep
					// the rest of the cleanup running.
					sb.waitForCleanupCall(t)

					if err := syscall.Kill(shell, sig.signal); err != nil {
						t.Fatalf("send a second %v to pid %d, the shell of the make test-browser recipe, while its EXIT trap removes what it cleans up: %v", sig.signal, shell, err)
					}

					sb.releaseCleanupCall(t)
				}

				run := proc.wait(failFastTimeout)
				output := run.output()

				switch {
				case run.timedOut:
					t.Fatalf("make test-browser was still running after %s although the test interrupted it with %v (%s): a trapped signal must end the run at once, and an untrapped one ends the shell with it (issue #423, the reviewer's finding S2)\n%s",
						run.elapsed, sig.signal, delivery.name, output)
				case run.err == nil:
					t.Fatalf("make test-browser exited 0 although the test interrupted it with %v (%s) and no smoke test ever ran: an interrupted run must not report success (issue #423, the reviewer's finding S2)\n%s",
						sig.signal, delivery.name, output)
				}

				// The shell runs its EXIT trap before it exits, and make waits for the shell, so the removals are done
				// when make is: the test looks once more, up to waitDeadline, rather than race the last of them.
				cleaned := waitUntil(func() bool {
					_, err := os.Stat(served[0])

					return os.IsNotExist(err) && removedContainer(sb.dockerCalls(t), name) && len(sb.runBundleDirs(t)) == 0
				})

				if !cleaned {
					if _, err := os.Stat(served[0]); err == nil {
						t.Errorf("the run left the bundle directory %s it was serving behind when the test interrupted it with %v (%s): the trap the signal needs runs the EXIT trap, which removes it, since dash runs no EXIT trap for an untrapped signal, and a second signal of the same kind must not end the shell before that (issue #423, commit 93b859d)\n%s",
							served[0], sig.signal, delivery.name, output)
					}

					if leftovers := sb.runBundleDirs(t); len(leftovers) != 0 {
						t.Errorf("the run left the per-run bundle directories %v behind when the test interrupted it with %v (%s): every directory its mktemp -d made must be removed when the run ends, however it ends, and a second signal of the same kind must not end the shell before that (issue #423, commit 93b859d)",
							leftovers, sig.signal, delivery.name)
					}

					if !removedContainer(sb.dockerCalls(t), name) {
						t.Errorf("the run never asked the container command to remove the container %s it started, when the test interrupted it with %v (%s): a container left behind holds the name the next run of the worktree wants, and the browser with it (issue #423, commit 93b859d)\n%s",
							name, sig.signal, delivery.name, sb.dockerLogText(t))
					}
				}

				if records := sb.goTestRecords(t); len(records) != 0 {
					t.Errorf("the interrupted run ran the smoke test %d times although its container never logged a DevTools address (issue #423, the reviewer's finding S2)\n%s",
						len(records), sb.goLogText(t))
				}
			})
		}
	}
}

// waitUntil polls cond every 20 milliseconds until it holds, and gives it waitDeadline before reporting that it
// never did. It is what reads what a signalled run leaves behind, after a moment's wait rather than the instant make
// exits, so a cleanup that trails the signal by a few milliseconds is no failure.
func waitUntil(cond func() bool) bool {
	deadline := time.Now().Add(waitDeadline)

	for time.Now().Before(deadline) {
		if cond() {
			return true
		}

		time.Sleep(20 * time.Millisecond)
	}

	return cond()
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

// dockerCall is one call of the stand-in container command: the pid of the shell that ran it, and the argv it was
// given.
type dockerCall struct {
	shell int      // the pid of the parent shell, from the call's ppid line
	argv  []string // the arguments, in order
}

// dockerCalls reads back what the stand-in container command was asked, one call per tool=docker line, in the
// order the recipe made the calls.
func (sb *browserRecipeSandbox) dockerCalls(t *testing.T) []dockerCall {
	t.Helper()

	body, err := os.ReadFile(sb.dockerLog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		t.Fatalf("read %s: %v", sb.dockerLog, err)
	}

	var calls []dockerCall

	// A body the stand-in is still writing ends in a line it has not finished, and only a line ending in the newline
	// that completes it is a line of the log: the split's last element is either that unfinished line or the empty
	// string after a complete one, so it is dropped unless the body ends in a newline (issue #423, the reviewer's
	// finding N2).
	lines := strings.Split(string(body), "\n")
	if !strings.HasSuffix(string(body), "\n") {
		lines = lines[:len(lines)-1]
	}

	for _, line := range lines {
		switch {
		case line == "tool=docker":
			calls = append(calls, dockerCall{})
		case strings.HasPrefix(line, "ppid="):
			if len(calls) == 0 {
				t.Fatalf("%s holds the parent line %q before any call of the stand-in container command", sb.dockerLog, line)
			}

			// Only a complete line reaches this loop, so the number here is the whole number the stand-in wrote: a
			// "ppid=12" of a "ppid=12345" still being written never gets here to pass for the pid 12. A complete line
			// that holds no number leaves the call without a shell, and the caller that needs one reports it.
			if pid, err := strconv.Atoi(strings.TrimPrefix(line, "ppid=")); err == nil {
				calls[len(calls)-1].shell = pid
			}
		case strings.HasPrefix(line, "arg="):
			if len(calls) == 0 {
				t.Fatalf("%s holds the argument line %q before any call of the stand-in container command", sb.dockerLog, line)
			}

			calls[len(calls)-1].argv = append(calls[len(calls)-1].argv, strings.TrimPrefix(line, "arg="))
		}
	}

	return calls
}

// dockerLogText is what the stand-in container command has logged so far, for a failure message that shows what it
// was asked.
func (sb *browserRecipeSandbox) dockerLogText(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(sb.dockerLog)
	if err != nil {
		if os.IsNotExist(err) {
			return "the stand-in container command logged nothing: it was never called"
		}

		t.Fatalf("read %s: %v", sb.dockerLog, err)
	}

	return string(body)
}

// wedgedRemovePid is the pid the stand-in container command's rm recorded of itself before it waited, and 0 when it
// never reached that call: the one pid a test may end, because the process that is the stand-in wrote it down itself,
// and never a pid found by name (fakeContainerCommand, CLAUDE.md rule 10, issue #449).
func (sb *browserRecipeSandbox) wedgedRemovePid(t *testing.T) int {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(sb.root, "rm.pid"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}

		t.Fatalf("read the pid the stand-in rm recorded of itself: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("the stand-in rm recorded %q as its pid, which is no pid: %v", string(body), err)
	}

	return pid
}

// callsTo is how many times the stand-in container command was asked command - one of run, logs, inspect or rm:
// the count a test reads a run back by, which no loaded machine changes, where a duration is the machine's.
func (sb *browserRecipeSandbox) callsTo(t *testing.T, command string) int {
	t.Helper()

	calls := 0

	for _, call := range sb.dockerCalls(t) {
		if len(call.argv) > 0 && call.argv[0] == command {
			calls++
		}
	}

	return calls
}

// inspectCalls is how many times the stand-in container command was asked for the container's state: one check per
// poll the recipe waits through, and no check on the poll that finds the DevTools line.
func (sb *browserRecipeSandbox) inspectCalls(t *testing.T) int {
	t.Helper()

	return sb.callsTo(t, "inspect")
}

// logCalls is how many times the stand-in container command was asked for the container's log: one read per poll of
// the recipe's wait loop, and the read that shows the tail when the run gives up.
func (sb *browserRecipeSandbox) logCalls(t *testing.T) int {
	t.Helper()

	return sb.callsTo(t, "logs")
}

// runBundleDirs are the per-run bundle directories the recipe has left in the sandbox's TMPDIR, in name order: the
// directories its own mktemp -d made, which a run removes when it ends, however it ends.
func (sb *browserRecipeSandbox) runBundleDirs(t *testing.T) []string {
	t.Helper()

	tmp := filepath.Join(sb.root, "tmp")

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("read %s: %v", tmp, err)
	}

	var dirs []string

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "shift2eu-test-browser.") {
			dirs = append(dirs, filepath.Join(tmp, entry.Name()))
		}
	}

	return dirs
}

// waitForDockerCall waits until the stand-in container command has been asked name - run, logs, inspect - and fails
// the test when it is asked nothing of the sort within waitDeadline: a recipe that has not reached that call is not
// yet in the part of the run a test means to interrupt.
func (sb *browserRecipeSandbox) waitForDockerCall(t *testing.T, name string) {
	t.Helper()

	deadline := time.Now().Add(waitDeadline)

	for time.Now().Before(deadline) {
		for _, call := range sb.dockerCalls(t) {
			if len(call.argv) > 0 && call.argv[0] == name {
				return
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("the stand-in container command was never asked %q within %s, so make test-browser never reached that call: %s",
		name, waitDeadline, sb.dockerLogText(t))
}

// The files a case's second signal meets the stand-in that runs the removal under test by, in the sandbox's
// rendezvous directory: the stand-in writes cleanupHeld.<pid> once the recipe's EXIT trap is in its call, and the test
// writes cleanupRelease once it has sent the second signal (fakeContainerCommand, fakeRemoveCommand).
const (
	cleanupHeld    = "cleanup"
	cleanupRelease = "cleanup.go"
)

// cleanupAnnouncement matches the file the stand-in writes, cleanupHeld and its pid.
var cleanupAnnouncement = regexp.MustCompile(`^` + cleanupHeld + `\.[0-9]+$`)

// waitForCleanupCall waits until the stand-in the case named has written its file - the recipe's EXIT trap is in the
// call that removes the container or the bundle directory - and fails the test when it never does: a run that never
// reaches that call has no cleanup for a second signal to meet.
func (sb *browserRecipeSandbox) waitForCleanupCall(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(waitDeadline)

	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(filepath.Join(sb.root, "rendezvous"))
		if err != nil {
			t.Fatalf("read the rendezvous directory of the sandbox: %v", err)
		}

		for _, entry := range entries {
			if cleanupAnnouncement.MatchString(entry.Name()) {
				return
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("the stand-in that runs the removal under test never announced that the recipe's EXIT trap was in it, within %s, so the run has no cleanup for the second signal to meet: %s",
		waitDeadline, sb.dockerLogText(t))
}

// releaseCleanupCall lets the stand-in that announced its removal go on: it removes what it was asked to, and the
// shell's EXIT trap carries on with the rest of the cleanup, the signal the test sent in between still pending.
func (sb *browserRecipeSandbox) releaseCleanupCall(t *testing.T) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(sb.root, "rendezvous", cleanupRelease), nil, 0o600); err != nil {
		t.Fatalf("release the stand-in that runs the removal under test: %v", err)
	}
}

// containerName is the name the recipe gave its container, from the --name of the call that started it, and "" when
// the stand-in container command recorded no such call.
func containerName(calls []dockerCall) string {
	for _, call := range calls {
		if len(call.argv) > 0 && call.argv[0] == "run" {
			for at, arg := range call.argv {
				if arg == "--name" && at+1 < len(call.argv) {
					return call.argv[at+1]
				}
			}
		}
	}

	return ""
}

// shellPid is the pid of the recipe's shell, from the call that started the container: the recipe runs that command
// as a plain command of its own shell, so the pid the stand-in reports as its parent is the shell with the traps.
func shellPid(t *testing.T, calls []dockerCall) int {
	t.Helper()

	for _, call := range calls {
		if len(call.argv) > 0 && call.argv[0] == "run" && call.shell != 0 {
			return call.shell
		}
	}

	t.Fatalf("the stand-in container command recorded no call that started a container from a shell whose pid it logged, so the test cannot see the recipe's shell to signal")

	return 0
}

// removedContainer reports whether the container command was asked to remove the container name, as the recipe's
// EXIT trap does: rm -f, with the name among its arguments.
func removedContainer(calls []dockerCall, name string) bool {
	for _, call := range calls {
		if len(call.argv) == 0 || call.argv[0] != "rm" {
			continue
		}

		force, named := false, false

		for _, arg := range call.argv[1:] {
			switch arg {
			case "-f":
				force = true
			case name:
				named = true
			}
		}

		if force && named {
			return true
		}
	}

	return false
}

// browserRecipeProcess is one run of the sandbox's make that a test has not waited for yet: the process group make
// and its recipe's shell are in, and what the two have printed so far.
type browserRecipeProcess struct {
	cmd    *exec.Cmd
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	done   chan struct{}
	err    error
	start  time.Time
}

// startBrowserMake starts make with args in dir, as runBrowserMake does, and returns before it ends, so a test can
// signal the process group of a recipe that waits for something that never comes. The group is killed when the test
// ends, waited for or not, so no recipe outlives it.
func startBrowserMake(t *testing.T, dir string, args []string, env map[string]string) *browserRecipeProcess {
	t.Helper()

	proc := &browserRecipeProcess{
		cmd:    exec.Command(makeProgram(t), append([]string{"-C", dir, "--no-print-directory"}, args...)...),
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		done:   make(chan struct{}),
		start:  time.Now(),
	}
	proc.cmd.Env = envList(env)
	proc.cmd.Dir = dir
	proc.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	proc.cmd.Stdout = proc.stdout
	proc.cmd.Stderr = proc.stderr

	if err := proc.cmd.Start(); err != nil {
		t.Fatalf("start make %v: %v", args, err)
	}

	// The buffers are read only after done is closed, which orders that read after Wait finished writing them.
	go func() {
		proc.err = proc.cmd.Wait()
		close(proc.done)
	}()

	t.Cleanup(func() {
		// A test that failed before it waited must not leave make and its recipe's shell running.
		_ = syscall.Kill(-proc.pgid(), syscall.SIGKILL)
		<-proc.done
	})

	return proc
}

// pgid is the process group make, its recipe's shell and that shell's children are in: make leads the group the test
// put it in, so a run the test gives up on is ended with one signal to all of it.
func (proc *browserRecipeProcess) pgid() int {
	return proc.cmd.Process.Pid
}

// wait gives make until timeout to end, and reports what it printed and how it ended as runBrowserMake does for a
// run it waited for, killing the process group and reporting the timeout when make is still running then.
func (proc *browserRecipeProcess) wait(timeout time.Duration) browserRecipeRun {
	select {
	case <-proc.done:
		return browserRecipeRun{stdout: proc.stdout.String(), stderr: proc.stderr.String(), err: proc.err, elapsed: time.Since(proc.start)}
	case <-time.After(timeout):
		_ = syscall.Kill(-proc.pgid(), syscall.SIGKILL)
		<-proc.done

		return browserRecipeRun{
			stdout:   proc.stdout.String(),
			stderr:   proc.stderr.String(),
			err:      proc.err,
			timedOut: true,
			elapsed:  time.Since(proc.start),
		}
	}
}
