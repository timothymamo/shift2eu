// Pins the refusals of .github/scripts/install-claude-code.sh (issue #275), the installer of the Claude Code
// CLI the AI reviewer asks its model through (PRD #161 milestone 4, ADR 0033), which .github/workflows/ai-review.yml
// runs before any secret is in a step's environment.
//
// The installer's contract, run as `install-claude-code.sh <directory> <platform package>@<X.Y.Z> <integrity>`:
//
//   - It refuses, with exit 2, a package that is not an exact version of a Claude Code platform package
//     (`@anthropic-ai/claude-code-<platform>@X.Y.Z`): `@latest`, a range, no version, a partial version, the meta
//     package @anthropic-ai/claude-code - which only picks a platform package in a postinstall script - and any
//     other package.
//   - It refuses, with exit 2, an integrity that is not an npm `sha512-<base64>` value: another algorithm, an
//     empty base64 body, a value with characters base64 does not hold, the empty string, and a bare hex digest.
//   - It downloads the tarball with `npm pack --ignore-scripts --silent --pack-destination <dir> <package>`, which
//     runs no install script, and refuses, with exit 1, a tarball whose sha512 does not match the pinned
//     integrity. Nothing of a refused tarball is extracted.
//   - It refuses, with exit 1, a runner whose /etc/claude-code exists, a directory or a plain file: the CLI applies
//     an admin-managed policy from there under every flag.
//   - The managed-policy check runs before the argument checks, so a runner that carries a policy is refused as
//     such even when the arguments would be refused too, and no download is attempted either way.
//   - A tarball that matches installs its one binary, package/claude, executable, into <directory> - the other
//     files of the tarball stay out - and the installer prints the version line the binary prints.
//   - CLAUDE_CODE_INSTALL_ROOT, which only a test sets, stands in for / in the managed-policy check, and
//     GITHUB_ACTIONS cannot weaken that check: with it set and the root overridden, the installer refuses too, so
//     no workflow step can drop the check by setting the variable. It refuses the variable when it is empty as
//     well, since a step could set it to nothing to make the check read / again without unsetting it. A trailing
//     slash on the root is stripped, so the path the installer checks and names is <root>/etc/claude-code, never
//     <root>//etc/claude-code. GITHUB_ACTIONS alone is no refusal.
//
// Every case runs the real script in an environment the harness builds for that case alone, from nothing of this
// process's own but PATH: PATH with the fake npm's directory first, HOME as the case's temporary directory, LANG
// and LC_ALL as C, TMPDIR as a directory below the case's t.TempDir(), which holds what the installer's mktemp -d
// leaves behind, and the fake npm's variables. No other variable of the developing machine's environment reaches
// the run, so neither a shell hook nor a tool's configuration that a BASH_ENV or an OPENSSL_CONF would point at can
// change a case, and no credential the environment holds can appear in a failure's output. A fake npm first on PATH
// copies a tarball the test builds at run time (no binary blob is committed) into the pack destination and prints
// its file name, and it touches a marker file so a case can assert npm was not called; the rest of PATH stays, so
// bash, tar, openssl, base64, mktemp, cp, sh and gzip are the machine's own.
//
// The test needs no root and no network, and the root it overrides is a fresh t.TempDir(); a case that needs the
// machine's real /etc/claude-code absent skips when it exists.
package ci_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// moduleRoot is the module root, as this package's directory reaches it.
	moduleRoot = "../.."

	// installScript is the installer under test, run through bash as the workflow runs it.
	installScript = ".github/scripts/install-claude-code.sh"

	// installRootEnv overrides the / the managed-policy check reads, for tests only: the installed CLI's
	// /etc/claude-code is the runner's own and cannot be created unprivileged.
	installRootEnv = "CLAUDE_CODE_INSTALL_ROOT"

	// hostPolicyPath is the managed policy directory on the machine the test runs on, which the installer reads
	// when installRootEnv is unset.
	hostPolicyPath = "/etc/claude-code"

	// policyDir is the managed policy directory below the install root.
	policyDir = "etc/claude-code"

	// packageArg is the platform package the workflow installs, at an exact version.
	packageArg = "@anthropic-ai/claude-code-linux-x64@2.1.0"

	// tarballName is the name npm pack gives that package's tarball, and what the fake npm prints.
	tarballName = "anthropic-ai-claude-code-linux-x64-2.1.0.tgz"

	// claudePath is the one file of the tarball the installer extracts, relative to the destination.
	claudePath = "package/claude"

	// versionLine is what the fixture's package/claude prints, as the real one prints "2.1.0 (Claude Code)".
	versionLine = "2.1.0 (Claude Code)"

	// githubActionsEnv is the variable GitHub sets in every workflow step; the installer may not let it weaken
	// the managed-policy check.
	githubActionsEnv = "GITHUB_ACTIONS"

	// The variables the fake npm reads: the tarball to serve, the file name to print, and the marker it touches
	// so a case can assert it was called.
	npmTarballEnv = "SHIFT2EU_TEST_NPM_TARBALL"
	npmNameEnv    = "SHIFT2EU_TEST_NPM_TARBALL_NAME"
	npmMarkerEnv  = "SHIFT2EU_TEST_NPM_MARKER"
)

// fakeNPMBody is the fake npm: it records the call, copies the fixture tarball into the pack destination and
// prints the tarball's file name, which is what `npm pack --silent` writes on stdout.
const fakeNPMBody = `#!/bin/sh
set -eu
marker="${SHIFT2EU_TEST_NPM_MARKER:?}"
tarball="${SHIFT2EU_TEST_NPM_TARBALL:?}"
name="${SHIFT2EU_TEST_NPM_TARBALL_NAME:?}"
: > "${marker}"
pack=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		--pack-destination)
			pack="$2"
			shift 2
			;;
		*)
			shift
			;;
	esac
done
if [ -z "${pack}" ]; then
	echo "fake npm: no --pack-destination" >&2
	exit 1
fi
cp "${tarball}" "${pack}/${name}"
printf '%s\n' "${name}"
`

// policyState is what a case puts at <install root>/etc/claude-code.
type policyState int

const (
	policyAbsent policyState = iota
	policyDirectory
	policyFile
)

// fixture is the tarball the fake npm serves and the npm integrity of its bytes.
type fixture struct {
	tarball   []byte
	integrity string
}

// installCase is one run of the installer: the arguments it gets, the environment it runs in, and what it must
// leave behind.
type installCase struct {
	name string
	// args returns the installer's arguments for a destination and the fixture.
	args func(dest string, fx fixture) []string
	// policy is what the case creates at <install root>/etc/claude-code.
	policy policyState
	// githubActions sets GITHUB_ACTIONS=true for the run.
	githubActions bool
	// noInstallRoot leaves CLAUDE_CODE_INSTALL_ROOT unset, so the installer reads the machine's own
	// /etc/claude-code; those cases skip when it exists.
	noInstallRoot bool
	// emptyInstallRoot sets CLAUDE_CODE_INSTALL_ROOT to the empty string, which the installer treats as set.
	emptyInstallRoot bool
	// trailingSlashInstallRoot sets CLAUDE_CODE_INSTALL_ROOT to the root with a trailing slash, which the installer
	// must strip before it appends etc/claude-code.
	trailingSlashInstallRoot bool

	// wantExit is the exit status the installer must end with.
	wantExit int
	// wantNPM says whether npm may be called; a case that refuses before downloading says false.
	wantNPM bool
	// wantFiles is the exact set of files the case leaves under the destination, as slash-separated paths.
	wantFiles []string
	// wantExecutable says package/claude must be executable.
	wantExecutable bool
	// wantOutput holds substrings of the run's stdout and stderr together that must appear.
	wantOutput []string
	// wantRootOutput holds paths below the case's install root: each must appear in the output resolved against
	// that root, so a case pins the exact path the installer checked and named, not a suffix of it.
	wantRootOutput []string
}

// TestInstallClaudeCodeRefusesUnverifiedPackages runs the installer's cases against a fixture tarball.
func TestInstallClaudeCodeRefusesUnverifiedPackages(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"bash", "tar", "openssl", "base64", "mktemp", "cp", "sh", "gzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH: %s needs it", tool, installScript)
		}
	}

	// The installer spells the tarball's digest with base64 -w0, which only GNU coreutils' base64 has: the BSD one
	// rejects -w and would otherwise fail the integrity comparison on wrapped lines.
	probe := exec.Command("base64", "-w0")
	probe.Stdin = strings.NewReader("")

	if out, err := probe.Output(); err != nil {
		t.Skipf("base64 -w0 </dev/null failed (%v, output %q): %s needs GNU coreutils' base64", err, out, installScript)
	}

	fx := newFixture(t)

	for _, tc := range installCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runInstallCase(t, tc, fx)
		})
	}
}

// TestChildEnvCarriesNothingFromTheParent pins the environment a case hands the installer: the harness builds it
// itself, so no variable of the developer's own environment - a configuration file a tool reads, a shell hook, a
// preloaded library, a proxy, a credential of the agents' - can change a case's result or reach a failure's output.
// The parent environment is a parameter, so the test states what a case must ignore instead of changing the
// environment of the whole test binary (t.Setenv is forbidden, CLAUDE.md rule 3).
func TestChildEnvCarriesNothingFromTheParent(t *testing.T) {
	base := t.TempDir()

	// The parent's environment stands in for what a developing machine's carries: configuration a tool reads, a
	// hook bash sources, a preloaded library, a proxy, a credential, and the variables a case decides itself.
	parent := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/canary/home",
		"TMPDIR=/canary/tmp",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"SHELL=/bin/canary-sh",
		"OPENSSL_CONF=/canary/openssl.cnf",
		"BASH_ENV=/canary/hook.sh",
		"ENV=/canary/hook.sh",
		"LD_PRELOAD=/canary/libcanary.so",
		"NODE_OPTIONS=--require=/canary/hook.js",
		"HTTPS_PROXY=http://canary.invalid:3128",
		"ANTHROPIC_API_KEY=a canary that must not reach the run",
		"GITHUB_TOKEN=a canary that must not reach the run",
		githubActionsEnv + "=true",
		installRootEnv + "=/nonexistent",
		"A NAME WITHOUT A VALUE",
	}

	dirs := childDirs{
		bin:  filepath.Join(base, "bin"),
		home: filepath.Join(base, "home"),
		tmp:  filepath.Join(base, "tmp"),
	}

	marker := filepath.Join(base, "npm-called")

	env := childEnv(t, parent, dirs, map[string]string{npmMarkerEnv: marker})

	// Only names and duplicates are reported, never the value of a variable the environment turned out to hold: a
	// leaked credential must not reach a failure's output, which is the leak this test pins shut.
	names := make([]string, 0, len(env))
	values := make(map[string]string, len(env))

	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			t.Errorf("%q is not a NAME=value pair", kv)

			continue
		}

		if _, twice := values[name]; twice {
			t.Errorf("%s is set twice", name)
		}

		names = append(names, name)
		values[name] = value
	}

	slices.Sort(names)

	wantNames := []string{"HOME", "LANG", "LC_ALL", "PATH", "TMPDIR", npmMarkerEnv}
	slices.Sort(wantNames)

	if diff := cmp.Diff(wantNames, names); diff != "" {
		t.Errorf("child environment's variables (-want +got):\n%s", diff)
	}

	for _, want := range []struct{ name, value string }{
		{name: "PATH", value: dirs.bin + string(os.PathListSeparator) + "/usr/bin:/bin"},
		{name: "HOME", value: dirs.home},
		{name: "LANG", value: "C"},
		{name: "LC_ALL", value: "C"},
		{name: "TMPDIR", value: dirs.tmp},
		{name: npmMarkerEnv, value: marker},
	} {
		if got := values[want.name]; got != want.value {
			t.Errorf("%s = %q, want %q", want.name, got, want.value)
		}
	}
}

// installCases returns the cases, in the order the installer's contract above lists them.
func installCases() []installCase {
	// validArgs passes the matching platform package and the fixture's integrity.
	validArgs := func(dest string, fx fixture) []string {
		return []string{dest, packageArg, fx.integrity}
	}

	// withPackage passes pkg with the fixture's integrity.
	withPackage := func(pkg string) func(string, fixture) []string {
		return func(dest string, fx fixture) []string {
			return []string{dest, pkg, fx.integrity}
		}
	}

	// withIntegrity passes the matching package with integrity.
	withIntegrity := func(integrity string) func(string, fixture) []string {
		return func(dest string, _ fixture) []string {
			return []string{dest, packageArg, integrity}
		}
	}

	// otherSum is a sha512 of bytes the fixture is not, spelled as an npm integrity and as a bare hex digest.
	otherSum := sha512.Sum512([]byte("a tarball the fake npm never serves"))
	otherIntegrity := "sha512-" + base64.StdEncoding.EncodeToString(otherSum[:])
	otherHex := hex.EncodeToString(otherSum[:])
	sha256Sum := sha256.Sum256([]byte("a tarball the fake npm never serves"))

	return []installCase{
		{
			name:     "no arguments",
			args:     func(string, fixture) []string { return nil },
			wantExit: 2,
		},
		{
			name:     "two arguments",
			args:     func(dest string, _ fixture) []string { return []string{dest, packageArg} },
			wantExit: 2,
		},
		{
			name:     "the package at latest",
			args:     withPackage("@anthropic-ai/claude-code-linux-x64@latest"),
			wantExit: 2,
		},
		{
			name:     "the package as a caret range",
			args:     withPackage("@anthropic-ai/claude-code-linux-x64@^2.1.0"),
			wantExit: 2,
		},
		{
			name:     "the package with no version",
			args:     withPackage("@anthropic-ai/claude-code-linux-x64"),
			wantExit: 2,
		},
		{
			name:     "the package with a partial version",
			args:     withPackage("@anthropic-ai/claude-code-linux-x64@2.1"),
			wantExit: 2,
		},
		{
			name:     "the meta package",
			args:     withPackage("@anthropic-ai/claude-code@2.1.0"),
			wantExit: 2,
		},
		{
			name:     "an unrelated package",
			args:     withPackage("left-pad@1.0.0"),
			wantExit: 2,
		},
		{
			name:     "an integrity of another algorithm",
			args:     withIntegrity("sha256-" + base64.StdEncoding.EncodeToString(sha256Sum[:])),
			wantExit: 2,
		},
		{
			name:     "an integrity with an empty body",
			args:     withIntegrity("sha512-"),
			wantExit: 2,
		},
		{
			name:     "an integrity that is no base64",
			args:     withIntegrity("sha512-not base64!"),
			wantExit: 2,
		},
		{
			name:     "an empty integrity",
			args:     withIntegrity(""),
			wantExit: 2,
		},
		{
			name:     "a bare hex digest",
			args:     withIntegrity(otherHex),
			wantExit: 2,
		},
		{
			name:       "a tarball that does not match the integrity",
			args:       withIntegrity(otherIntegrity),
			wantExit:   1,
			wantNPM:    true,
			wantOutput: []string{"does not match"},
		},
		{
			name:           "a managed policy directory on the runner",
			args:           validArgs,
			policy:         policyDirectory,
			wantExit:       1,
			wantRootOutput: []string{policyDir},
		},
		{
			name:           "a managed policy file on the runner",
			args:           validArgs,
			policy:         policyFile,
			wantExit:       1,
			wantRootOutput: []string{policyDir},
		},
		{
			name:                     "a managed policy directory under a root with a trailing slash",
			args:                     validArgs,
			policy:                   policyDirectory,
			trailingSlashInstallRoot: true,
			wantExit:                 1,
			wantRootOutput:           []string{policyDir},
		},
		{
			// The managed-policy check runs before the argument checks: this case would exit 2 for its package,
			// so exit 1 with the policy message pins the order.
			name:           "a managed policy directory and bad arguments",
			args:           withPackage("@anthropic-ai/claude-code-linux-x64@latest"),
			policy:         policyDirectory,
			wantExit:       1,
			wantRootOutput: []string{policyDir},
		},
		{
			name:           "a matching tarball",
			args:           validArgs,
			wantExit:       0,
			wantNPM:        true,
			wantFiles:      []string{claudePath},
			wantExecutable: true,
			wantOutput:     []string{versionLine},
		},
		{
			name:          "GITHUB_ACTIONS cannot weaken the managed-policy check",
			args:          validArgs,
			githubActions: true,
			wantExit:      1,
			wantOutput:    []string{installRootEnv},
		},
		{
			name:             "GITHUB_ACTIONS refuses an empty install root",
			args:             validArgs,
			githubActions:    true,
			emptyInstallRoot: true,
			wantExit:         1,
			wantOutput:       []string{installRootEnv},
		},
		{
			name:           "GITHUB_ACTIONS alone is no refusal",
			args:           validArgs,
			githubActions:  true,
			noInstallRoot:  true,
			wantExit:       0,
			wantNPM:        true,
			wantFiles:      []string{claudePath},
			wantExecutable: true,
			wantOutput:     []string{versionLine},
		},
	}
}

// newFixture builds the tarball at run time and its npm integrity.
func newFixture(t *testing.T) fixture {
	t.Helper()

	tarball := buildTarball(t)

	return fixture{tarball: tarball, integrity: integrityOf(tarball)}
}

// buildTarball returns a gzipped tar like the one npm pack makes of a Claude Code platform package: the one
// binary, package/claude, executable and printing a version line, and two files of the package the installer must
// not extract.
func buildTarball(t *testing.T) []byte {
	t.Helper()

	entries := []struct {
		name string
		body string
		mode int64
	}{
		{name: "package/README.md", body: "A fixture package for the installer's tests.\n", mode: 0o644},
		{name: claudePath, body: "#!/bin/sh\necho '" + versionLine + "'\n", mode: 0o755},
		{name: "package/package.json", body: `{"name":"@anthropic-ai/claude-code-linux-x64","version":"2.1.0"}` + "\n", mode: 0o644},
	}

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, entry := range entries {
		hdr := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Size:     int64(len(entry.body)),
			Typeflag: tar.TypeReg,
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write the header of %s: %v", entry.name, err)
		}

		if _, err := io.WriteString(tw, entry.body); err != nil {
			t.Fatalf("write %s: %v", entry.name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close the tar writer: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close the gzip writer: %v", err)
	}

	return buf.Bytes()
}

// integrityOf returns the npm integrity of tarball's bytes: "sha512-" and their base64 sha512.
func integrityOf(tarball []byte) string {
	sum := sha512.Sum512(tarball)

	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// runInstallCase stages one case in a temporary directory, runs the installer and checks what it left behind.
func runInstallCase(t *testing.T, tc installCase, fx fixture) {
	t.Helper()

	if tc.noInstallRoot {
		if _, err := os.Stat(hostPolicyPath); err == nil {
			t.Skipf("%s exists on this machine, so the installer must refuse: this case proves only that GITHUB_ACTIONS alone is no refusal", hostPolicyPath)
		}
	}

	base := t.TempDir()

	binDir := filepath.Join(base, "bin")
	writeFakeNPM(t, binDir)

	tarballPath := filepath.Join(base, tarballName)
	writeFile(t, tarballPath, fx.tarball, 0o600)

	marker := filepath.Join(base, "npm-called")

	// The destination does not exist yet: the installer creates it, as the workflow's RUNNER_TEMP path does not.
	dest := filepath.Join(base, "claude-code")

	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create the install root %s: %v", root, err)
	}

	writePolicy(t, root, tc.policy)

	// The installer's mktemp -d honours TMPDIR and never removes what it made, so the case hands it a directory
	// below its own temp dir instead of the machine's.
	tmpDir := filepath.Join(base, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		t.Fatalf("create the case's TMPDIR %s: %v", tmpDir, err)
	}

	env := childEnv(t, os.Environ(), childDirs{bin: binDir, home: base, tmp: tmpDir}, map[string]string{
		npmTarballEnv: tarballPath,
		npmNameEnv:    tarballName,
		npmMarkerEnv:  marker,
	})

	switch {
	case tc.noInstallRoot:
	case tc.emptyInstallRoot:
		env = append(env, installRootEnv+"=")
	case tc.trailingSlashInstallRoot:
		env = append(env, installRootEnv+"="+root+"/")
	default:
		env = append(env, installRootEnv+"="+root)
	}

	if tc.githubActions {
		env = append(env, githubActionsEnv+"=true")
	}

	output, exit := runInstaller(t, env, tc.args(dest, fx)...)

	if exit != tc.wantExit {
		t.Errorf("exit status = %d, want %d\noutput:\n%s", exit, tc.wantExit, output)
	}

	for _, want := range tc.wantOutput {
		if !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}

	for _, want := range tc.wantRootOutput {
		if want = filepath.Join(root, want); !strings.Contains(output, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}

	if diff := cmp.Diff(tc.wantFiles, filesUnder(t, dest)); diff != "" {
		t.Errorf("files under the destination (-want +got):\n%s", diff)
	}

	if got := npmCalled(t, marker); got != tc.wantNPM {
		t.Errorf("npm called = %t, want %t", got, tc.wantNPM)
	}

	if tc.wantExecutable {
		info, err := os.Stat(filepath.Join(dest, claudePath))
		if err != nil {
			t.Fatalf("stat %s: %v", claudePath, err)
		}

		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s has mode %v, want an executable one", claudePath, info.Mode())
		}
	}
}

// writePolicy creates what a case puts at <root>/etc/claude-code.
func writePolicy(t *testing.T, root string, state policyState) {
	t.Helper()

	path := filepath.Join(root, policyDir)

	switch state {
	case policyAbsent:
		return
	case policyDirectory:
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
	case policyFile:
		writeFile(t, path, []byte("a managed policy this machine must never carry\n"), 0o600)
	}
}

// writeFakeNPM writes the fake npm into dir, the first directory on the case's PATH.
func writeFakeNPM(t *testing.T, dir string) {
	t.Helper()

	writeFile(t, filepath.Join(dir, "npm"), []byte(fakeNPMBody), 0o700)
}

// writeFile writes content to path, creating its directory, and gives it mode. The content is written with
// 0600 and the mode then set explicitly, as the e2e tests' helpers do, so a script the case makes executable is
// no more readable than it needs to be while it is written.
func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s to %v: %v", path, mode, err)
	}
}

// childDirs are the directories a case hands the installer's run: the fake npm first on PATH, the case's HOME,
// and the TMPDIR the installer's mktemp -d leaves behind.
type childDirs struct {
	bin  string
	home string
	tmp  string
}

// childEnv returns the environment the installer runs in, built from nothing but the harness: the parent's PATH
// after dirs.bin, which comes first, for the tools the installer and the fake npm call, dirs.home as HOME, LANG and
// LC_ALL as C for a run that neither a locale nor a message catalogue of the developer's can change, dirs.tmp as
// TMPDIR, and extra besides. Every other variable of parent is dropped, so neither the developer's configuration
// nor a credential held there can change a case or reach a failure's output.
func childEnv(t *testing.T, parent []string, dirs childDirs, extra map[string]string) []string {
	t.Helper()

	path := dirs.bin

	for _, kv := range parent {
		if value, ok := strings.CutPrefix(kv, "PATH="); ok && value != "" {
			path += string(os.PathListSeparator) + value
		}
	}

	env := []string{"PATH=" + path, "HOME=" + dirs.home, "LANG=C", "LC_ALL=C", "TMPDIR=" + dirs.tmp}

	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		env = append(env, name+"="+extra[name])
	}

	return env
}

// runInstaller runs the installer with env and args, and returns its stdout and stderr together and its exit
// status.
func runInstaller(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command("bash", append([]string{scriptPath(t)}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = env

	var out bytes.Buffer

	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run %s: %v", installScript, err)
	}

	return out.String(), exit.ExitCode()
}

// scriptPath returns the installer's absolute path.
func scriptPath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(moduleRoot, installScript))
	if err != nil {
		t.Fatalf("resolve %s: %v", installScript, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s does not exist: %v", installScript, err)
	}

	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", installScript)
	}

	return path
}

// filesUnder returns every regular file below dir, as slash-separated paths relative to it and sorted. A dir that
// does not exist holds no file.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()

	var files []string

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		files = append(files, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		t.Fatalf("walk %s: %v", dir, err)
	}

	slices.Sort(files)

	return files
}

// npmCalled reports whether the fake npm ran, which it records by touching marker.
func npmCalled(t *testing.T, marker string) bool {
	t.Helper()

	_, err := os.Stat(marker)
	if err == nil {
		return true
	}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat %s: %v", marker, err)
	}

	return false
}
