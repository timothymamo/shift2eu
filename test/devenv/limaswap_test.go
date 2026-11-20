// Pins the swap file the Lima VM provisions (issue #294): the param that sizes it, and the system provision
// step that creates, enables and reuses it, keeps a low vm.swappiness and steps aside when the guest's disk
// cannot hold it.
//
// The step's contract, which this file pins: dev/lima.yaml's `param:` block holds `swapSize: 4GiB` (a whole
// number of GiB; `0GiB` means no swap file), and exactly one provision script names PARAM_swapSize, in a step
// whose mode is system, so it runs as root at every boot and at every re-provision.
//
// The step must also meet the seam below, so it can run unprivileged against a fake guest:
//
//   - SHIFT2EU_SWAP_ROOT, read in the guest, prefixes every path the step writes and is empty in the guest,
//     as SHIFT2EU_VM_BIN_GH and SHIFT2EU_VM_LIBEXEC_GH do for the gh wrapper (dev/vm-gh-wrapper.sh). The
//     tests point it at a temporary directory and touch nothing of the machine they run on.
//   - The swap file is ${SHIFT2EU_SWAP_ROOT}${swapFilePath}, and the swappiness drop-in is
//     ${SHIFT2EU_SWAP_ROOT}/etc/sysctl.d/<something>swap<something>.conf; the step creates that directory.
//   - Which swap is active is read from `swapon --show` (any form: --show, --show=NAME, --noheadings), never
//     from /proc/swaps, which the Mac the tests can run on does not have. The fake prints the active paths,
//     one per line, under --noheadings, and a NAME TYPE SIZE USED PRIO header and a row per path without it.
//   - The free space of the filesystem that holds the swap file is read from `df`, whose last line of output
//     is the number of free bytes, as `df --output=avail -B1 <path> | tail -n1` prints it. The fake prints
//     that whatever its arguments, so the step must pass whatever -B/--output it likes but take the last
//     line as bytes. Free space is not read from mountpoint, findmnt or lsblk: they are not faked.
//   - The step creates the file with `fallocate -l <size> <file>` and sets its mode with `chmod`; the test
//     fakes both. The faked fallocate succeeds, so a `dd` fallback is never reached.
//   - A message the step prints to standard error names the swap it declined (the word "swap"): the step runs
//     under `set -x`, which makes stderr never empty, so a bare non-empty check would pin nothing.
//   - The step stays portable to the Mac the tests can run on: read the file's size with `wc -c` (or
//     `stat`'s BSD form), not GNU-only `stat -c %s`, and keep to sh and bash 3.2 (macOS's bash): no ${var^^},
//     no arrays, no mapfile.
//
// The test replaces the commands that would change the machine with fakes first on PATH, which record their
// argv to $SHIFT2EU_TEST_ARGV_LOG: fallocate, chmod, mkswap, swapon, swapoff, sysctl and df. fallocate makes
// a sparse file of the length it is given, so the size assertions read a real file, and the faked chmod runs
// the machine's own chmod after logging, so the mode assertion is real too. The faked swapon and swapoff
// keep the active swap in $SHIFT2EU_TEST_SWAP_STATE, one path per line, which a case pre-fills to stand for a
// later boot; the faked df prints $SHIFT2EU_TEST_FREE_BYTES as the free space.
//
// The step's path in the guest is pinned by swapFilePath below alone: change it there if the implementation
// picks another.
//
// The tests need no root: run them as an unprivileged user, so a step that ignored SHIFT2EU_SWAP_ROOT fails
// instead of writing the machine's own /swapfile and /etc/sysctl.d.
package devenv_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// swapSizeParamName is the name of the template's param that sizes the swap file, which Lima hands the
	// step as PARAM_swapSize.
	swapSizeParamName = "swapSize"
	// swapSizeEnv is how Lima passes the param's value to a provision script: PARAM_<name>.
	swapSizeEnv = "PARAM_" + swapSizeParamName
	// swapDefaultSize is the param's value in dev/lima.yaml: what a VM with no override provisions.
	swapDefaultSize = "4GiB"
	// swapFilePath is the swap file's path inside the guest, relative to the root SHIFT2EU_SWAP_ROOT names.
	swapFilePath = "/swapfile"
	// swapRootEnv prefixes every path the step writes, read in the guest (dev/vm-gh-wrapper.sh's
	// SHIFT2EU_VM_BIN_GH is the pattern); the tests point it at a temporary directory.
	swapRootEnv = "SHIFT2EU_SWAP_ROOT"
	// swapGiB is one GiB: the unit the param is written in.
	swapGiB = int64(1) << 30

	// The variables the fakes read.
	swapArgvLogEnv   = "SHIFT2EU_TEST_ARGV_LOG"   // where every fake appends its argv
	swapStateEnv     = "SHIFT2EU_TEST_SWAP_STATE" // the active swap, one path per line
	swapFreeBytesEnv = "SHIFT2EU_TEST_FREE_BYTES" // what the fake df prints as free space
)

// sysctlSwappiness matches the drop-in line that sets vm.swappiness to 10, with or without a trailing comment.
var sysctlSwappiness = regexp.MustCompile(`(?m)^[ \t]*vm\.swappiness[ \t]*=[ \t]*10[ \t]*(?:#.*)?$`)

// limaParamValue matches one entry of the template's param: block: its name and its value without the quotes
// and any trailing comment.
var limaParamValue = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_]+):[ \t]*"?([^"#\r\n]*)"?[ \t]*(?:#.*)?$`)

// limaProvisionMode matches a provision entry's mode line, such as "  - mode: system".
var limaProvisionMode = regexp.MustCompile(`^\s*-\s*mode:\s*"?([A-Za-z]+)"?\s*$`)

// The fake tools. Each one records its argv to $SHIFT2EU_TEST_ARGV_LOG - one tool= line and one arg= line
// per argument - so a test can assert what the step ran and in which order.
const (
	// fakeFallocateBody makes a sparse file of the length it is given (-l 4G, -l 4GiB or -l 4294967296),
	// through truncate, so a size assertion reads the size the step asked for.
	fakeFallocateBody = `size=""
file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
  -l | --length) size="$2"; shift 2 ;;
  --length=*) size="${1#--length=}"; shift ;;
  -*) shift ;;
  *) file="$1"; shift ;;
  esac
done
if [ -z "${size}" ] || [ -z "${file}" ]; then
  echo 'fake fallocate: want -l <length> <file>' >&2
  exit 64
fi
case "${size}" in
*[!0-9]*)
  num="${size%%[!0-9]*}"
  suf="${size#"${num}"}"
  case "${suf}" in
  k | K | KiB | KB) mult=1024 ;;
  m | M | MiB | MB) mult=1048576 ;;
  g | G | GiB | GB) mult=1073741824 ;;
  *) echo "fake fallocate: unknown length ${size}" >&2; exit 64 ;;
  esac
  case "${num}" in
  '' | *[!0-9]*) echo "fake fallocate: unknown length ${size}" >&2; exit 64 ;;
  esac
  size=$((num * mult))
  ;;
esac
truncate -s "${size}" "${file}"
`
	// fakeSwaponBody answers a query of the active swap from $SHIFT2EU_TEST_SWAP_STATE, one path per line,
	// with a header and a row per path unless --noheadings is given, and records an activation there.
	fakeSwaponBody = `show=0
noheadings=0
file=""
for arg in "$@"; do
  case "${arg}" in
  --show*) show=1 ;;
  -s | --summary) show=1 ;;
  --noheadings | -n) noheadings=1 ;;
  -*) ;;
  *) file="${arg}" ;;
  esac
done
state="${SHIFT2EU_TEST_SWAP_STATE:?set SHIFT2EU_TEST_SWAP_STATE}"
if [ "${show}" -eq 1 ]; then
  [ -e "${state}" ] || exit 0
  if [ "${noheadings}" -eq 1 ]; then
    cat "${state}"
  else
    echo 'NAME TYPE SIZE USED PRIO'
    while IFS= read -r name; do
      [ -z "${name}" ] || echo "${name} file 4G 0B -2"
    done < "${state}"
  fi
  exit 0
fi
if [ -z "${file}" ]; then
  echo 'fake swapon: no file to activate' >&2
  exit 64
fi
printf '%s\n' "${file}" >> "${state}"
`
	// fakeSwapoffBody removes the file it is given from the active swap, and everything on swapoff -a.
	fakeSwapoffBody = `file=""
all=0
for arg in "$@"; do
  case "${arg}" in
  -a | --all) all=1 ;;
  -*) ;;
  *) file="${arg}" ;;
  esac
done
state="${SHIFT2EU_TEST_SWAP_STATE:?set SHIFT2EU_TEST_SWAP_STATE}"
[ -e "${state}" ] || exit 0
if [ "${all}" -eq 1 ] || [ -z "${file}" ]; then
  : > "${state}"
  exit 0
fi
grep -vxF "${file}" "${state}" > "${state}.tmp" || true
mv "${state}.tmp" "${state}"
`
	// fakeDfBody prints the free space a case gives it as `df --output=avail -B1 <path>` prints it: a header
	// line, then the number of bytes on the last line, whatever the arguments.
	fakeDfBody = `printf 'Avail\n%s\n' "${SHIFT2EU_TEST_FREE_BYTES:?set SHIFT2EU_TEST_FREE_BYTES}"
`
)

// fakeSwapTool returns the source of one fake: it records its argv, then runs body under sh.
func fakeSwapTool(name, body string) string {
	return "#!/bin/sh\n" +
		"{\n" +
		"  printf 'tool=" + name + "\\n'\n" +
		`  for a in "$@"; do printf 'arg=%s\n' "$a"; done` + "\n" +
		`} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"` + "\n" +
		body
}

// writeSwapFakes writes the fakes the step must find first on PATH into binDir. The faked chmod runs the
// machine's own chmod after logging, so the mode assertion is real.
func writeSwapFakes(t *testing.T, binDir string) {
	t.Helper()

	realChmod := realTool(t, "chmod")

	fakes := []struct {
		name string
		body string
	}{
		{"fallocate", fakeFallocateBody},
		{"chmod", "exec " + strconv.Quote(realChmod) + ` "$@"` + "\n"},
		{"mkswap", ""},
		{"swapon", fakeSwaponBody},
		{"swapoff", fakeSwapoffBody},
		{"sysctl", ""},
		{"df", fakeDfBody},
	}

	for _, fake := range fakes {
		writeFileMode(t, filepath.Join(binDir, fake.name), []byte(fakeSwapTool(fake.name, fake.body)), 0o700)
	}
}

// swapStep is the template's swap provision step.
type swapStep struct {
	mode   string   // the mode of the provision entry it belongs to: system, for a step that runs as root
	script []string // its script, dedented as the YAML block scalar dedents it
}

// swapProvisionStep returns the one provision script of dev/lima.yaml that names PARAM_swapSize, with the mode
// of its provision entry. Exactly one script may name it: Lima refuses a param no script names, and two steps
// could provision two swap files.
func swapProvisionStep(t *testing.T, template string) swapStep {
	t.Helper()

	lines := strings.Split(template, "\n")

	var found []swapStep

	for i, line := range lines {
		if !limaScriptHeader.MatchString(line) {
			continue
		}

		script := limaBlockContent(t, lines, i)
		if !slices.ContainsFunc(script, func(line string) bool { return strings.Contains(line, swapSizeEnv) }) {
			continue
		}

		found = append(found, swapStep{mode: provisionMode(t, lines, i), script: script})
	}

	switch len(found) {
	case 0:
		t.Fatalf("%s has no provision script that names %s: the swap file's step reads the %s param, and Lima refuses a param no script names",
			limaTemplateRepoPath, swapSizeEnv, swapSizeParamName)
	case 1:
		return found[0]
	default:
		t.Fatalf("%s has %d provision scripts that name %s, want exactly one: the swap file is provisioned once",
			limaTemplateRepoPath, len(found), swapSizeEnv)
	}

	return swapStep{}
}

// provisionMode returns the mode of the provision entry whose script header is lines[header]: the nearest
// "- mode:" line above it.
func provisionMode(t *testing.T, lines []string, header int) string {
	t.Helper()

	for i := header - 1; i >= 0; i-- {
		if match := limaProvisionMode.FindStringSubmatch(lines[i]); match != nil {
			return match[1]
		}
	}

	t.Fatalf("%s line %d is a provision script with no `- mode:` above it", limaTemplateRepoPath, header+1)

	return ""
}

// swapSizeParam returns the value of the template's swapSize param, without its quotes.
func swapSizeParam(t *testing.T, template string) string {
	t.Helper()

	lines := strings.Split(template, "\n")

	start := slices.Index(lines, "param:")
	if start < 0 {
		t.Fatalf("%s has no top-level param: block, where the swap file's size belongs", limaTemplateRepoPath)
	}

	var values []string

	for _, line := range lines[start+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if !strings.HasPrefix(line, "  ") {
			break
		}

		match := limaParamValue.FindStringSubmatch(line)
		if match == nil || match[1] != swapSizeParamName {
			continue
		}

		values = append(values, strings.TrimSpace(match[2]))
	}

	switch len(values) {
	case 0:
		t.Fatalf("%s's param: block has no %s: the VM's swap file is sized by a param, so it can be changed without editing the script, and %s is the default",
			limaTemplateRepoPath, swapSizeParamName, swapDefaultSize)
	case 1:
		return values[0]
	default:
		t.Fatalf("%s's param: block has %d %s entries, want exactly one", limaTemplateRepoPath, len(values), swapSizeParamName)
	}

	return ""
}

// TestLimaTemplateHasASwapFileStep pins the template's half of the swap file: the param that sizes it and the
// one system step that provisions it.
func TestLimaTemplateHasASwapFileStep(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)

	t.Run("param-sizes-the-swap-file", func(t *testing.T) {
		t.Parallel()

		if got := swapSizeParam(t, template); got != swapDefaultSize {
			t.Errorf("%s's %s param is %q, want %q: the VM's swap file is %s of disk (issue #294), and VM_DISK must hold it",
				limaTemplateRepoPath, swapSizeParamName, got, swapDefaultSize, swapDefaultSize)
		}
	})

	t.Run("one-system-step", func(t *testing.T) {
		t.Parallel()

		step := swapProvisionStep(t, template)

		if step.mode != "system" {
			t.Errorf("%s's swap step has mode %q, want system: creating a swap file, mkswap and swapon need root, and a system step runs as root at every boot and re-provision",
				limaTemplateRepoPath, step.mode)
		}
	})
}

// dropinWant says what a case pins about the vm.swappiness drop-in.
type dropinWant int

const (
	dropinAny     dropinWant = iota // the case pins nothing about the drop-in
	dropinPresent                   // the drop-in is there, sets vm.swappiness to 10 and is applied
	dropinAbsent                    // the step wrote no drop-in
)

// swapCase is one run of the swap step: the guest it starts from, the param it is given and what it must do.
type swapCase struct {
	name string
	size string // the value of PARAM_swapSize
	// freeBytes is what the fake df reports as the free space of the filesystem that holds the swap file.
	freeBytes int64
	// existingSize is the size of a swap file the guest has before the run, in bytes; 0 has none.
	existingSize int64
	// active says the existing file is already active swap, as it is at a later boot.
	active bool
	// wantFailure says the step must exit non-zero, refusing the value before it changes anything.
	wantFailure bool
	// wantMessage says stderr must name the swap the step declined.
	wantMessage bool
	// wantCalls are the commands that change the guest's swap that the step must run, in this order.
	wantCalls []string
	// wantFileSize is the size of the swap file the step must leave, in bytes: 0 for none.
	wantFileSize int64
	// wantFileMode is the mode the step must leave on a swap file it created: 0 pins no mode.
	wantFileMode os.FileMode
	dropin       dropinWant
}

// swapCases are the runs the step must handle: a fresh guest, a later boot, a changed param, too little disk,
// no swap at all and values that are no size. Wanting no calls is written as an empty slice, so the diff of
// the calls reads plainly.
var swapCases = []swapCase{
	{
		name:         "fresh-guest",
		size:         swapDefaultSize,
		freeBytes:    100 * swapGiB,
		wantCalls:    []string{"fallocate", "chmod", "mkswap", "swapon"},
		wantFileSize: 4 * swapGiB,
		wantFileMode: 0o600,
		dropin:       dropinPresent,
	},
	{
		name:         "second-boot-already-active",
		size:         swapDefaultSize,
		freeBytes:    100 * swapGiB,
		existingSize: 4 * swapGiB,
		active:       true,
		wantCalls:    []string{},
		wantFileSize: 4 * swapGiB,
		dropin:       dropinPresent,
	},
	{
		name:         "right-size-inactive",
		size:         swapDefaultSize,
		freeBytes:    100 * swapGiB,
		existingSize: 4 * swapGiB,
		wantCalls:    []string{"swapon"},
		wantFileSize: 4 * swapGiB,
		dropin:       dropinPresent,
	},
	{
		name:         "wrong-size-active-after-a-param-change",
		size:         swapDefaultSize,
		freeBytes:    100 * swapGiB,
		existingSize: 2 * swapGiB,
		active:       true,
		wantCalls:    []string{"swapoff", "fallocate", "chmod", "mkswap", "swapon"},
		wantFileSize: 4 * swapGiB,
		wantFileMode: 0o600,
		dropin:       dropinPresent,
	},
	{
		name:         "too-little-disk",
		size:         swapDefaultSize,
		freeBytes:    swapGiB,
		wantCalls:    []string{},
		wantMessage:  true,
		wantFileSize: 0,
		dropin:       dropinAny,
	},
	{
		name:         "no-swap-at-zero",
		size:         "0GiB",
		freeBytes:    100 * swapGiB,
		wantCalls:    []string{},
		wantFileSize: 0,
		dropin:       dropinAbsent,
	},
	{
		name:         "invalid-4G",
		size:         "4G",
		freeBytes:    100 * swapGiB,
		wantFailure:  true,
		wantMessage:  true,
		wantCalls:    []string{},
		wantFileSize: 0,
		dropin:       dropinAbsent,
	},
	{
		name:         "invalid-abc",
		size:         "abc",
		freeBytes:    100 * swapGiB,
		wantFailure:  true,
		wantMessage:  true,
		wantCalls:    []string{},
		wantFileSize: 0,
		dropin:       dropinAbsent,
	},
	{
		name:         "invalid-negative",
		size:         "-1GiB",
		freeBytes:    100 * swapGiB,
		wantFailure:  true,
		wantMessage:  true,
		wantCalls:    []string{},
		wantFileSize: 0,
		dropin:       dropinAbsent,
	},
	{
		// A value that holds a shell control character must reach the script as data and stop at the check.
		name:         "invalid-shell-metacharacters",
		size:         "4GiB; rm",
		freeBytes:    100 * swapGiB,
		wantFailure:  true,
		wantMessage:  true,
		wantCalls:    []string{},
		wantFileSize: 0,
		dropin:       dropinAbsent,
	},
}

// swapGuest is one guest for a run of the step: a temporary directory standing for the VM's root, the
// directory of the fakes first on PATH, and the files the fakes read and write.
type swapGuest struct {
	root       string // what SHIFT2EU_SWAP_ROOT names: the fake guest's root
	binDir     string // first on PATH: the fakes
	argvLog    string // what every fake appends its argv to
	statePath  string // the active swap, one path per line, which the fake swapon and swapoff keep
	scriptPath string // the step's script, written out for bash
	swapFile   string // the swap file's path inside the fake root
}

// newSwapGuest builds a guest as a case needs it: the fakes, a swap file of the case's size if it has one, and
// the active swap of a guest whose swap is on.
func newSwapGuest(t *testing.T, tc swapCase) *swapGuest {
	t.Helper()

	base := t.TempDir()
	guest := &swapGuest{
		root:       filepath.Join(base, "guest"),
		binDir:     filepath.Join(base, "bin"),
		argvLog:    filepath.Join(base, "argv.log"),
		statePath:  filepath.Join(base, "swap.state"),
		scriptPath: filepath.Join(base, "swap-step.sh"),
	}
	guest.swapFile = filepath.Join(guest.root, swapFilePath)

	if err := os.MkdirAll(guest.root, 0o700); err != nil {
		t.Fatalf("create the fake guest %s: %v", guest.root, err)
	}

	writeSwapFakes(t, guest.binDir)

	if tc.existingSize > 0 {
		// Mode 0644, so a case that pins 0600 proves the step set it.
		writeFileMode(t, guest.swapFile, nil, 0o644)

		if err := os.Truncate(guest.swapFile, tc.existingSize); err != nil {
			t.Fatalf("grow the swap file %s: %v", guest.swapFile, err)
		}
	}

	active := ""
	if tc.active {
		active = guest.swapFile + "\n"
	}

	writeFileMode(t, guest.statePath, []byte(active), 0o600)

	return guest
}

// run runs the step in the guest, with only the variables it needs, and returns its output.
func (g *swapGuest) run(t *testing.T, tc swapCase) (stdout, stderr string, err error) {
	t.Helper()

	cmd := exec.Command("bash", g.scriptPath)
	cmd.Dir = g.root
	cmd.Env = envList(map[string]string{
		"PATH":           g.binDir + ":/usr/local/bin:/usr/bin:/bin",
		swapRootEnv:      g.root,
		swapSizeEnv:      tc.size,
		swapArgvLogEnv:   g.argvLog,
		swapStateEnv:     g.statePath,
		swapFreeBytesEnv: strconv.FormatInt(tc.freeBytes, 10),
	})

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err = cmd.Run()

	return out.String(), errOut.String(), err
}

// argvLogText is what the fakes have logged so far.
func (g *swapGuest) argvLogText(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(g.argvLog)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ""
		}

		t.Fatalf("read %s: %v", g.argvLog, err)
	}

	return string(body)
}

// swapTool is one call of a fake, as the argv log records it.
type swapTool struct {
	name string
	args []string
}

// swapToolCalls returns the calls that change the guest's swap, in call order. A query of the active swap
// (swapon --show, swapon -s) is left out: the step asks at every boot, and the cases pin the commands that
// change something, not the queries. df and sysctl are left out too: no case pins a place of theirs among
// these.
func swapToolCalls(log string) []swapTool {
	var (
		all  []swapTool
		cur  swapTool
		open bool
	)

	for _, line := range strings.Split(log, "\n") {
		if name, ok := strings.CutPrefix(line, "tool="); ok {
			if open {
				all = append(all, cur)
			}

			cur, open = swapTool{name: name}, true

			continue
		}

		if !open {
			continue
		}

		if arg, ok := strings.CutPrefix(line, "arg="); ok {
			cur.args = append(cur.args, arg)
		}
	}

	if open {
		all = append(all, cur)
	}

	changes := make([]swapTool, 0, len(all))

	for _, call := range all {
		switch call.name {
		case "chmod", "dd", "fallocate", "mkswap", "swapoff":
			changes = append(changes, call)
		case "swapon":
			if !swapShowQuery(call.args) {
				changes = append(changes, call)
			}
		}
	}

	return changes
}

// swapShowQuery reports whether a swapon call only asks which swap is active.
func swapShowQuery(args []string) bool {
	for _, arg := range args {
		if arg == "-s" || arg == "--summary" || strings.HasPrefix(arg, "--show") {
			return true
		}
	}

	return false
}

// swapToolNames returns the names of the calls, in order.
func swapToolNames(calls []swapTool) []string {
	names := make([]string, 0, len(calls))

	for _, call := range calls {
		names = append(names, call.name)
	}

	return names
}

// swapDropins returns the names of the swappiness drop-ins the step wrote, sorted: the *.conf files of the
// guest's /etc/sysctl.d whose name says they are about swap.
func swapDropins(t *testing.T, root string) []string {
	t.Helper()

	dir := filepath.Join(root, "etc", "sysctl.d")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		t.Fatalf("read %s: %v", dir, err)
	}

	var names []string

	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".conf") || !strings.Contains(entry.Name(), "swap") {
			continue
		}

		names = append(names, entry.Name())
	}

	slices.Sort(names)

	return names
}

// swapDropin returns the path and content of the one swappiness drop-in the step wrote.
func swapDropin(t *testing.T, root string) (string, string) {
	t.Helper()

	names := swapDropins(t, root)
	if len(names) != 1 {
		t.Fatalf("%s/etc/sysctl.d holds %v, want exactly one *swap*.conf: the step sets vm.swappiness in a drop-in of its own, so it changes nothing else of the guest's sysctl settings",
			root, names)
	}

	path := filepath.Join(root, "etc", "sysctl.d", names[0])

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return path, string(body)
}

// appliesSwappiness reports whether one sysctl call applies the drop-in: sysctl --system, sysctl -p <file>,
// or one whose arguments name the drop-in's path.
func appliesSwappiness(calls [][]string, dropin string) bool {
	base := filepath.Base(dropin)

	for _, args := range calls {
		for _, arg := range args {
			if arg == "--system" || arg == "--load" || arg == "-p" || strings.Contains(arg, base) {
				return true
			}
		}
	}

	return false
}

// assertSwapFile pins the swap file a case leaves behind: none, or one of the size the param names and, when
// the step had to create it, mode 0600.
func assertSwapFile(t *testing.T, g *swapGuest, tc swapCase) {
	t.Helper()

	info, err := os.Stat(g.swapFile)

	switch {
	case tc.wantFileSize == 0:
		if err == nil {
			t.Errorf("%s: the step left %s (%d bytes), want no swap file for swapSize %q", tc.name, g.swapFile, info.Size(), tc.size)
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v", g.swapFile, err)
		}
	case err != nil:
		t.Errorf("%s: %s: %v; the step must leave a swap file of %d bytes there, for swapSize %q", tc.name, g.swapFile, err, tc.wantFileSize, tc.size)
	case info.Size() != tc.wantFileSize:
		t.Errorf("%s: the swap file is %d bytes, want %d (swapSize %q): a file of another size is recreated, a change of the param included",
			tc.name, info.Size(), tc.wantFileSize, tc.size)
	case tc.wantFileMode != 0 && info.Mode().Perm() != tc.wantFileMode:
		t.Errorf("%s: the swap file's mode is %04o, want %04o: mkswap reads a file only its owner may write",
			tc.name, info.Mode().Perm(), tc.wantFileMode)
	}
}

// swapStepScript returns the template's swap step as the text bash runs it.
func swapStepScript(t *testing.T) string {
	t.Helper()

	return strings.Join(swapProvisionStep(t, readRepoFile(t, limaTemplateRepoPath)).script, "\n") + "\n"
}

// TestSwapProvisionStep runs the template's swap step against a fake guest and pins what it does there.
func TestSwapProvisionStep(t *testing.T) {
	t.Parallel()

	script := swapStepScript(t)

	for _, tc := range swapCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runSwapCase(t, script, tc)
		})
	}
}

// runSwapCase runs script, the step's text, in a guest a case describes, and pins what it did there.
func runSwapCase(t *testing.T, script string, tc swapCase) {
	t.Helper()

	guest := newSwapGuest(t, tc)
	writeFileMode(t, guest.scriptPath, []byte(script), 0o600)

	stdout, stderr, err := guest.run(t, tc)

	if tc.wantFailure {
		if err == nil {
			t.Errorf("%s: the step exited 0 for swapSize %q, want a non-zero exit: it refuses a value that is no whole number of GiB before it changes anything\nstdout: %s\nstderr: %s",
				tc.name, tc.size, stdout, stderr)
		}
	} else if err != nil {
		t.Errorf("%s: the step failed, want exit 0: %v\nstdout: %s\nstderr: %s", tc.name, err, stdout, stderr)
	}

	// The step runs under set -x, so stderr is never empty: a message must name the swap it declined.
	if tc.wantMessage && !strings.Contains(strings.ToLower(stderr), "swap") {
		t.Errorf("%s: stderr does not name the swap the step declined (swapSize %q), so the run reads as a silent one:\n%s",
			tc.name, tc.size, stderr)
	}

	log := guest.argvLogText(t)

	if diff := cmp.Diff(tc.wantCalls, swapToolNames(swapToolCalls(log))); diff != "" {
		t.Errorf("%s: the commands that change the guest's swap (-want +got):\n%s\n"+
			"A new file is fallocate, chmod 0600, mkswap, swapon, in that order; one of the right size that is not active is swapon alone; an active file of the wrong size is swapoff first; a later boot runs none. "+
			"The test fakes fallocate, chmod, mkswap, swapon, swapoff, sysctl and df, so the step must find them on PATH; a swapon --show query, df and sysctl are not counted, and a faked fallocate succeeds, so a dd fallback is never reached.",
			tc.name, diff)
	}

	assertSwapFile(t, guest, tc)

	switch tc.dropin {
	case dropinPresent:
		path, body := swapDropin(t, guest.root)

		if !sysctlSwappiness.MatchString(body) {
			t.Errorf("%s: %s does not set vm.swappiness to 10, so the guest swaps under pressure as it always has:\n%s", tc.name, path, body)
		}

		if !appliesSwappiness(invocations(log, "sysctl"), path) {
			t.Errorf("%s: the step ran sysctl %v, want a call that applies %s (sysctl --system, or sysctl -p %s): a drop-in changes nothing until it is loaded",
				tc.name, invocations(log, "sysctl"), path, path)
		}
	case dropinAbsent:
		if names := swapDropins(t, guest.root); len(names) != 0 {
			t.Errorf("%s: the step wrote %v into the guest's /etc/sysctl.d for swapSize %q, want nothing: it changes nothing before it has accepted the param",
				tc.name, names, tc.size)
		}
	case dropinAny:
	}
}
