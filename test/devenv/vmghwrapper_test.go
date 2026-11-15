// Pins the script make vm-gh-wrapper hands the VM (PRD #161 milestone 1 second review items 2 and 3): it
// installs the gh wrapper whole or not at all. The generated script is one function called on its last line,
// so a stream cut short runs nothing; the wrapper it writes is checked with sh -n and for its last exec line,
// installed to a temporary file and moved into place, and it ends by printing the line the make recipe
// requires. These tests run the generated script against a temporary root the way the VM would, with
// SHIFT2EU_VM_BIN_GH and SHIFT2EU_VM_LIBEXEC_GH naming that root and a fake sudo on PATH, so no test touches
// /usr/local/bin of the machine it runs on.
package devenv_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// doneLine is the line the VM-side script ends with and the make recipe requires: without it, make cannot tell
// a finished installation from a stream that stopped half-way (second review, item 2).
const doneLine = "vm-gh-wrapper: done"

// vmBinHooks are the hooks the VM-side script reads the paths it installs to, so a test installs into its own
// temporary root (second review, item 3).
const (
	vmBinHook     = "SHIFT2EU_VM_BIN_GH"
	vmLibexecHook = "SHIFT2EU_VM_LIBEXEC_GH"
)

// realGHBinary is a placeholder for the real gh a VM created before the wrapper has at /usr/local/bin/gh: two
// bytes that are not a "#!", which is what the script tells a gh to move from a script it refuses.
const realGHBinary = "REAL-GH-BINARY\n"

// newerGHBinary is a placeholder for the real gh a newer VM already has in libexec.
const newerGHBinary = "NEWER-REAL-GH-BINARY\n"

// vmRoot is one temporary stand-in for the VM's installation: where the wrapper and the real gh live.
type vmRoot struct {
	bin     string // ${SHIFT2EU_VM_BIN_GH}: the wrapper's path
	libexec string // ${SHIFT2EU_VM_LIBEXEC_GH}: the real gh's path
}

// newVMRoot builds a fresh temporary root for one case.
func newVMRoot(t *testing.T) vmRoot {
	t.Helper()

	root := filepath.Join(t.TempDir(), "vm")

	return vmRoot{
		bin:     filepath.Join(root, "usr", "local", "bin", "gh"),
		libexec: filepath.Join(root, "usr", "local", "libexec", "shift2eu", "gh"),
	}
}

// generatorEnv is the environment dev/vm-gh-wrapper.sh runs in: it only reads the wrapper it is given.
func (sb *sandbox) generatorEnv() map[string]string {
	return sb.baseEnv()
}

// vmEnv is the environment the generated script runs in, the way the VM user's shell would have it: the two
// path hooks, the VM user's home for the git credential helper, and the fake sudo first on PATH.
func (sb *sandbox) vmEnv(r vmRoot) map[string]string {
	env := sb.baseEnv()
	env["HOME"] = sb.vmHome
	env[vmBinHook] = r.bin
	env[vmLibexecHook] = r.libexec

	return env
}

// vmGHWrapperStream runs dev/vm-gh-wrapper.sh for wrapperPath and returns the script it prints, which is what
// make vm-gh-wrapper pipes into limactl.
func vmGHWrapperStream(t *testing.T, sb *sandbox, wrapperPath string) string {
	t.Helper()

	stdout, stderr, err := sb.run(t, scriptPath(t, vmGHWrapperScript), []string{wrapperPath}, sb.generatorEnv(), "")
	if err != nil {
		t.Fatalf("dev/%s exited non-zero for %s: %v\nstderr: %s", vmGHWrapperScript, wrapperPath, err, stderr)
	}

	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("dev/%s printed nothing for %s: the VM would run an empty script", vmGHWrapperScript, wrapperPath)
	}

	return stdout
}

// installed returns the content of path, or "" when it is not there.
func installed(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return string(body)
}

// assertWrapperInstalled fails unless path holds the gh wrapper: it reads no SHIFT2EU_REAL_GH from the
// environment it runs in, it hands the token to the real gh, and it is executable.
func assertWrapperInstalled(t *testing.T, path, where string) {
	t.Helper()

	body := installed(t, path)
	if body == "" {
		t.Fatalf("%s installed no wrapper at %s", where, path)
	}

	if !strings.Contains(body, "GH_TOKEN=") || !strings.Contains(body, `exec "${real}"`) {
		t.Errorf("%s installed something else than the gh wrapper at %s: the wrapper gives the real gh the installation token and runs it", where, path)
	}

	if strings.Contains(body, "${SHIFT2EU_REAL_GH") {
		t.Errorf("%s installed a wrapper that reads SHIFT2EU_REAL_GH, so an agent could point the agents' token at another binary (review item 6): %s", where, path)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s installed %s as mode %04o: every agent runs it, so it must be executable", where, path, info.Mode().Perm())
	}
}

// TestVMGHWrapperScriptInstallsIntoTheVMsTree pins the whole-installation rule of second review item 3: the
// generated script runs against a VM's tree the way the VM would run it, for a VM created before the wrapper,
// one that already has it, the half-way case between them, and the two refusals.
func TestVMGHWrapperScriptInstallsIntoTheVMsTree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		binBefore     string // "" leaves ${SHIFT2EU_VM_BIN_GH} absent
		libexecBefore string // "" leaves ${SHIFT2EU_VM_LIBEXEC_GH} absent
		wantRefused   bool
		wantLibexec   string // the real gh's content after the run; "" means it must not be there
		wantWrapper   bool   // whether the bin path must hold the wrapper afterwards
		runTwice      bool
	}{
		{
			name:        "a VM created before the wrapper",
			binBefore:   realGHBinary,
			wantLibexec: realGHBinary,
			wantWrapper: true,
		},
		{
			name:          "a VM that already has the wrapper's real gh",
			libexecBefore: newerGHBinary,
			wantLibexec:   newerGHBinary,
			wantWrapper:   true,
		},
		{
			name:          "half-way: the real gh is in libexec and bin still holds it",
			binBefore:     realGHBinary,
			libexecBefore: newerGHBinary,
			wantLibexec:   newerGHBinary,
			wantWrapper:   true,
		},
		{
			name:        "bin is a script and libexec holds no real gh",
			binBefore:   "#!/bin/sh\nexit 0\n",
			wantRefused: true,
		},
		{
			name:        "bin is missing",
			wantRefused: true,
		},
		{
			name:        "a second run changes nothing",
			binBefore:   realGHBinary,
			wantLibexec: realGHBinary,
			wantWrapper: true,
			runTwice:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			sb.useFakeSudo(t)

			r := newVMRoot(t)
			if tc.binBefore != "" {
				writeFileMode(t, r.bin, []byte(tc.binBefore), 0o700)
			}

			if tc.libexecBefore != "" {
				writeFileMode(t, r.libexec, []byte(tc.libexecBefore), 0o755)
			}

			stream := vmGHWrapperStream(t, sb, scriptPath(t, ghWrapperScript))
			env := sb.vmEnv(r)

			runOnce := func() (string, string, error) {
				t.Helper()

				return sb.runStream(t, env, stream)
			}

			stdout, stderr, err := runOnce()

			if tc.wantRefused {
				if err == nil {
					t.Fatalf("%s exited 0, want a refusal: nothing may be installed over a gh the script cannot vouch for\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
				}

				if strings.Contains(stdout+stderr, doneLine) {
					t.Errorf("%s prints %q although it refused: make would take that for a finished installation\nstdout: %s\nstderr: %s", tc.name, doneLine, stdout, stderr)
				}

				if got := installed(t, r.libexec); got != tc.libexecBefore {
					t.Errorf("%s changed %s to %q although it refused (second review, item 3)", tc.name, r.libexec, got)
				}

				if got := installed(t, r.bin); got != tc.binBefore {
					t.Errorf("%s changed %s to %q although it refused (second review, item 3)", tc.name, r.bin, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("%s exited non-zero: %v\nstdout: %s\nstderr: %s", tc.name, err, stdout, stderr)
			}

			if !strings.Contains(stdout+stderr, doneLine) {
				t.Errorf("%s does not end by printing %q, the line the make recipe requires (second review, item 2)\nstdout: %s\nstderr: %s", tc.name, doneLine, stdout, stderr)
			}

			if got := installed(t, r.libexec); got != tc.wantLibexec {
				t.Errorf("%s left %q at %s, want %q: the real gh is moved there once and never overwritten", tc.name, got, r.libexec, tc.wantLibexec)
			}

			if tc.wantWrapper {
				assertWrapperInstalled(t, r.bin, tc.name)
			}

			// No half-installed copy stays behind: the wrapper arrives in a temporary file and is moved into
			// place, so the bin directory holds the wrapper and nothing else.
			entries, err := os.ReadDir(filepath.Dir(r.bin))
			if err != nil {
				t.Fatalf("read %s: %v", filepath.Dir(r.bin), err)
			}

			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}

			if !slices.Equal(names, []string{"gh"}) {
				t.Errorf("%s left %v in %s, want only the wrapper: a temporary file the script did not move into place is a gh an agent may run (second review, item 2)", tc.name, names, filepath.Dir(r.bin))
			}

			// The git credential helper runs the wrapper, so `git push` gets the token too.
			helper := gitCredentialHelper(t, sb.vmHome)
			if !strings.Contains(helper, r.bin) {
				t.Errorf("%s leaves the git credential helper as %q, want it to run the wrapper at %s, so git pushes carry the agents App token", tc.name, helper, r.bin)
			}

			if !tc.runTwice {
				return
			}

			firstBin := installed(t, r.bin)

			secondOut, secondErr, err := runOnce()
			if err != nil {
				t.Fatalf("the second run exited non-zero: %v\nstdout: %s\nstderr: %s", err, secondOut, secondErr)
			}

			if got := installed(t, r.bin); got != firstBin {
				t.Errorf("the second run changed %s: make vm-gh-wrapper must be safe to run again", r.bin)
			}

			if got := installed(t, r.libexec); got != tc.wantLibexec {
				t.Errorf("the second run changed %s to %q, want %q", r.libexec, got, tc.wantLibexec)
			}
		})
	}
}

// lineIndex returns the index of the first line of stream that is want, ignoring the indentation the generated
// script gives it, and fails the test when there is none: a cut point the test cannot find is no cut point at
// all.
func lineIndex(t *testing.T, stream, want string) int {
	t.Helper()

	for i, line := range strings.Split(stream, "\n") {
		if strings.TrimSpace(line) == want {
			return i
		}
	}

	t.Fatalf("the generated script has no line %q, so this test cannot cut the stream there:\n%s", want, stream)

	return -1
}

// TestVMGHWrapperScriptRunsNothingOnATruncatedStream pins the first half of second review item 2 and the third
// review's item 5: the generated script is one function called on its last line, so a stream that stops anywhere
// before that call - inside the function, inside the SHIFT2EU_GH_WRAPPER here-document that carries the wrapper,
// or just before the call itself - installs nothing and prints no done line. A stream cut inside a function or a
// here-document is a syntax error, and without the call nothing runs at all.
func TestVMGHWrapperScriptRunsNothingOnATruncatedStream(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// cutAt is the line that is matched exactly to find the cut point: the last line to arrive, unless
		// cutBefore says the stream stops just before it.
		cutAt     string
		cutBefore bool
		reason    string // what the cut leaves behind, for the failure message
	}{
		{
			name:   "inside the here-document, after the wrapper's real gh line",
			cutAt:  "real=/usr/local/libexec/shift2eu/gh",
			reason: "the here-document the wrapper travels in never reaches its delimiter",
		},
		{
			name:   "inside the here-document, near the wrapper's end",
			cutAt:  "unset GITHUB_TOKEN token",
			reason: "the here-document stops two lines before its delimiter",
		},
		{
			name:   "inside the function, before the wrapper is moved into place",
			cutAt:  `sudo mv -f "${new_gh}" "${bin_gh}"`,
			reason: "the function is never closed",
		},
		{
			name:   "inside the function, after the done line it prints",
			cutAt:  `echo "vm-gh-wrapper: done"`,
			reason: "the function is never closed, and the done line arrived without the installation",
		},
		{
			name:      "just before the call that runs the function",
			cutAt:     "vm_gh_wrapper",
			cutBefore: true,
			reason:    "the script is whole but nothing calls the function",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			sb.useFakeSudo(t)

			r := newVMRoot(t)
			writeFileMode(t, r.bin, []byte(realGHBinary), 0o700)

			stream := vmGHWrapperStream(t, sb, scriptPath(t, ghWrapperScript))
			lines := strings.Split(stream, "\n")

			cut := lineIndex(t, stream, tc.cutAt)
			if tc.cutBefore {
				cut--
			}

			truncated := strings.Join(lines[:cut+1], "\n") + "\n"

			stdout, stderr, _ := sb.runStream(t, sb.vmEnv(r), truncated)

			if got := installed(t, r.bin); got != realGHBinary {
				t.Errorf("a stream cut %s replaced %s with %q, although %s: a transfer cut short must install nothing (third review, item 5)\nstdout: %s\nstderr: %s",
					tc.name, r.bin, got, tc.reason, stdout, stderr)
			}

			if _, err := os.Stat(r.libexec); err == nil {
				t.Errorf("a stream cut %s moved the real gh to %s: a transfer cut short must change nothing", tc.name, r.libexec)
			}

			if strings.Contains(stdout+stderr, doneLine) {
				t.Errorf("a stream cut %s prints %q, although %s: make would take that for a finished installation\nstdout: %s\nstderr: %s",
					tc.name, doneLine, tc.reason, stdout, stderr)
			}

			if helper := gitCredentialHelper(t, sb.vmHome); helper != "" {
				t.Errorf("a stream cut %s pointed the git credential helper at %q: a transfer cut short must change nothing", tc.name, helper)
			}

			entries, err := os.ReadDir(filepath.Dir(r.bin))
			if err != nil {
				t.Fatalf("read %s: %v", filepath.Dir(r.bin), err)
			}

			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}

			if !slices.Equal(names, []string{"gh"}) {
				t.Errorf("a stream cut %s left %v in %s, want only the gh that was there: nothing half-installed may stay behind", tc.name, names, filepath.Dir(r.bin))
			}
		})
	}
}

// TestVMGHWrapperScriptRefusesABrokenWrapper pins the other half: the wrapper the script installs is checked
// before it is moved into place, so a wrapper that does not parse, or one that does not end with the real gh's
// exec line, is never installed (second review, item 2).
func TestVMGHWrapperScriptRefusesABrokenWrapper(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		wrapper string
	}{
		{
			name:    "the wrapper does not parse",
			wrapper: "#!/bin/sh\nreal=/usr/local/libexec/shift2eu/gh\nif [ ; then\n",
		},
		{
			name:    "the wrapper does not end with its exec line",
			wrapper: "#!/bin/sh\nreal=/usr/local/libexec/shift2eu/gh\necho 'the exec line is lost'\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			sb.useFakeSudo(t)

			r := newVMRoot(t)
			writeFileMode(t, r.bin, []byte(realGHBinary), 0o700)

			broken := filepath.Join(t.TempDir(), "gh-app-token.sh")
			writeFileMode(t, broken, []byte(tc.wrapper), 0o755)

			stdout, stderr, genErr := sb.run(t, scriptPath(t, vmGHWrapperScript), []string{broken}, sb.generatorEnv(), "")
			if genErr == nil {
				// The generator printed a stream: run it the way the VM would, and it must refuse.
				stdout, stderr, _ = sb.runStream(t, sb.vmEnv(r), stdout)
			}

			if got := installed(t, r.bin); got != realGHBinary {
				t.Errorf("%s installed %q at %s: the script checks the wrapper before it moves it into place (second review, item 2)", tc.name, got, r.bin)
			}

			if _, err := os.Stat(r.libexec); err == nil {
				t.Errorf("%s moved the real gh to %s although it refused the wrapper", tc.name, r.libexec)
			}

			if strings.Contains(stdout+stderr, doneLine) {
				t.Errorf("%s prints %q although it refused the wrapper\nstdout: %s\nstderr: %s", tc.name, doneLine, stdout, stderr)
			}
		})
	}
}

// TestMakeVMGHWrapperInstallsIntoATemporaryRoot pins the recipe end to end (second review, item 2): make
// vm-gh-wrapper prints the done line the recipe requires, the real gh lands in libexec, the wrapper in bin,
// and the git credential helper runs the wrapper, all inside the temporary root the path hooks name.
func TestMakeVMGHWrapperInstallsIntoATemporaryRoot(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	sb.useFakeSudo(t)

	r := newVMRoot(t)
	writeFileMode(t, r.bin, []byte(realGHBinary), 0o700)

	env := sb.vmEnv(r)
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"

	stdout, stderr, err := runMake(t, []string{"vm-gh-wrapper"}, env)
	if err != nil {
		t.Fatalf("make vm-gh-wrapper: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if !strings.Contains(stdout+stderr, doneLine) {
		t.Errorf("make vm-gh-wrapper does not print %q, the line its recipe requires of the script it runs (second review, item 2)\nstdout: %s\nstderr: %s", doneLine, stdout, stderr)
	}

	if got := installed(t, r.libexec); got != realGHBinary {
		t.Errorf("make vm-gh-wrapper left %q at %s, want the real gh it moved off the PATH", got, r.libexec)
	}

	assertWrapperInstalled(t, r.bin, "make vm-gh-wrapper")

	if helper := gitCredentialHelper(t, sb.vmHome); !strings.Contains(helper, r.bin) {
		t.Errorf("make vm-gh-wrapper leaves the git credential helper as %q, want it to run the wrapper at %s", helper, r.bin)
	}
}

// TestMakeVMGHWrapperFailsWhenTheScriptDoesNotArrive pins the rest of second review item 2: the recipe
// generates the script first and pipes it second, so a generator that fails fails make before the VM is
// touched, and a script that ends without the done line fails make too, instead of looking like a success.
func TestMakeVMGHWrapperFailsWhenTheScriptDoesNotArrive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		generator string
		// silent makes the fake limactl answer like a connection that dropped after the stream arrived:
		// it takes the whole script and prints nothing, so sh -s exits 0.
		silent bool
		// wantLimactl is whether the recipe may reach the VM at all: only for a stream it generates whole.
		wantLimactl bool
	}{
		{
			name:      "the generator fails",
			generator: "#!/bin/sh\necho 'vm-gh-wrapper: the wrapper is not there' >&2\nexit 1\n",
		},
		{
			name:      "the script ends without the done line",
			generator: "#!/bin/sh\nprintf 'set -eu\\necho installed nothing\\n'\n",
		},
		{
			// The stream is whole, so the recipe hands it over; the VM runs nothing and says nothing, as
			// when the connection drops after the stream arrived and sh -s still exits 0. Only the done
			// line tells make that the installation finished (second review, item 2).
			name:        "the VM prints no done line",
			generator:   "#!/bin/sh\nprintf 'set -eu\\nvm_gh_wrapper() {\\n  echo installed nothing\\n}\\nvm_gh_wrapper\\n'\n",
			silent:      true,
			wantLimactl: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			clone := cloneMakeTree(t, sb, tc.generator)

			env := sb.baseEnv()
			env["SHIFT2EU_TEST_LIMA_CAPTURE"] = filepath.Join(sb.root, "lima-stdin.log")

			if tc.silent {
				env["SHIFT2EU_TEST_LIMA_SILENT"] = "1"
			}

			stdout, stderr, err := runMakeIn(t, clone, []string{"vm-gh-wrapper"}, env)
			if err == nil {
				t.Fatalf("make vm-gh-wrapper exited 0 although %s\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			called := len(invocations(sb.argvLogText(t), "limactl")) > 0

			switch {
			case !tc.wantLimactl && called:
				for _, args := range invocations(sb.argvLogText(t), "limactl") {
					t.Errorf("make vm-gh-wrapper called limactl with %v before it had a script to hand it: the recipe must generate the script first (second review, item 2)", args)
				}
			case tc.wantLimactl && !called:
				t.Errorf("make vm-gh-wrapper never handed the whole script to limactl although its last line is the call that runs it\nstdout: %s\nstderr: %s", stdout, stderr)
			case tc.wantLimactl && !strings.Contains(stdout+stderr, doneLine):
				t.Errorf("make vm-gh-wrapper fails without naming %q, so a maintainer cannot tell that the VM never reported the installation done\nstdout: %s\nstderr: %s", doneLine, stdout, stderr)
			}
		})
	}
}

// cloneMakeTree copies the Makefile and dev/ into a temporary directory, with vm-gh-wrapper.sh replaced by
// generator, so one recipe can be run against a stand-in the tests write instead of the worktree's.
func cloneMakeTree(t *testing.T, sb *sandbox, generator string) string {
	t.Helper()

	root := filepath.Join(sb.root, "clone")
	writeFileMode(t, filepath.Join(root, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)
	writeFileMode(t, filepath.Join(root, vmGHWrapperScript), []byte(generator), 0o755)
	writeFileMode(t, filepath.Join(root, ghWrapperScript), []byte(readRepoFile(t, ghWrapperScript)), 0o755)

	return root
}

// gitCredentialHelper returns the git credential helper of the VM user's home.
func gitCredentialHelper(t *testing.T, home string) string {
	t.Helper()

	cmd := exec.Command("git", "config", "--global", "--get", "credential.https://github.com.helper")
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}
