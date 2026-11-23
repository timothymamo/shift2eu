// Pins what the VM's apt step of dev/lima.yaml (issue #498) DOES, by running it, rather than only what its text
// says: the drop-in of apt's timeouts and retries, and the rewrite of the archive URIs of
// /etc/apt/sources.list.d/ubuntu.sources to the ubuntuMirror param.
//
// limaapt_test.go pins the step's half in the template: one system step writes the drop-in before the first
// apt-get call, reads PARAM_ubuntuMirror, and leaves security.ubuntu.com alone. This file pins the same step's
// behaviour on the files it is given, because two of its defects are invisible in its text: a `URIs:` line that
// names two archive URIs gets only the first one rewritten, and a file with no archive URI to rewrite (a ports
// file, a country mirror) makes the step warn and exit 0, so the VM silently keeps the archive the maintainer
// asked to replace.
//
// The step must meet the seam below, so it can run unprivileged against a fake guest, as the swap step of
// issue #294 does through SHIFT2EU_SWAP_ROOT:
//
//   - SHIFT2EU_APT_ROOT, read in the guest, prefixes every path the step writes and is empty in the guest. The
//     two paths are ${SHIFT2EU_APT_ROOT}/etc/apt/apt.conf.d/80shift2eu-timeouts and
//     ${SHIFT2EU_APT_ROOT}/etc/apt/sources.list.d/ubuntu.sources. The tests point it at a temporary directory and
//     touch nothing of the machine they run on; they need no root, and the step only runs once its text names
//     the variable, so a step that ignored the seam fails these cases instead of writing the machine's own
//     /etc/apt. That guard is the reason every case is RED until the seam is there, whatever else the step gets
//     wrong.
//   - PARAM_ubuntuMirror is the mirror, as Lima passes the template's ubuntuMirror param. An archive URI, an
//     http:// or https:// URL on archive.ubuntu.com's /ubuntu path, becomes the mirror with one trailing slash
//     and nothing else of the line; the URI of a country mirror (de.archive.ubuntu.com) is no archive URI of
//     that host and is never rewritten.
//   - A non-default mirror that finds no archive URI to rewrite - a ports file, a country mirror, a file that
//     already names the mirror is fine - stops the step with a non-zero exit and a message on standard error
//     that names ubuntu.sources, and leaves the file byte for byte as it was: exiting 0 after a warning leaves
//     the VM fetching from the archive the maintainer replaced. It is RED until the step does that.
//   - A default mirror (http://archive.ubuntu.com/ubuntu, with or without a trailing slash) changes nothing and
//     exits 0, even with no ubuntu.sources at all; a non-default mirror with no ubuntu.sources fails, naming the
//     file. An invalid mirror (not an http:// or https:// URL of letters, digits and . - / : _ ~ only) fails and
//     leaves the file alone.
//   - The step is idempotent, as Lima runs it at every boot: a second run on its own output exits 0 and leaves
//     the file byte for byte equal.
//
// The step runs under bash with only PATH, the root and the mirror in its environment, and nothing is faked:
// mkdir, sed and grep are the machine's own, so the cases pin what the step does with them.
package devenv_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// aptRootEnv prefixes every path the apt step writes, read in the guest: empty there, a temporary directory
	// in these tests (the swap step's SHIFT2EU_SWAP_ROOT is the pattern).
	aptRootEnv = "SHIFT2EU_APT_ROOT"
	// aptMirrorNL is the mirror the cases set: the country mirror the maintainer switched to when
	// archive.ubuntu.com trickled on 2026-11-23 (issue #498).
	aptMirrorNL = "http://nl.archive.ubuntu.com/ubuntu"
	// aptStepPath is what the step finds on PATH. Nothing is faked: it needs mkdir, sed and grep and nothing
	// else, so the cases pin what it does with the machine's own tools.
	aptStepPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

	// The parts of a deb822 stanza of ubuntu.sources that are not its URIs line, as cloud-init writes them.
	aptArchiveSuites  = "noble noble-updates noble-backports"
	aptSecuritySuites = "noble-security"
	aptComponents     = "main universe restricted multiverse"
	aptKeyring        = "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
	aptPortsKeyring   = "/usr/share/keyrings/ubuntu-ports-archive-keyring.gpg"
)

// aptStanza is one deb822 stanza of ubuntu.sources: the file's shape around the URIs line a case pins.
func aptStanza(uris, suites, components, signedBy string) string {
	return "Types: deb\n" +
		"URIs: " + uris + "\n" +
		"Suites: " + suites + "\n" +
		"Components: " + components + "\n" +
		"Signed-By: " + signedBy + "\n"
}

var (
	// aptArchiveStanza is the stanza cloud-init writes for the archive, the one the mirror rewrites.
	aptArchiveStanza = aptStanza("http://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring)
	// aptMirrorStanza is aptArchiveStanza once aptMirrorNL has rewritten it.
	aptMirrorStanza = aptStanza(aptMirrorNL+"/", aptArchiveSuites, aptComponents, aptKeyring)
	// aptSecurityStanza is the security stanza, whose URI the rewrite leaves alone.
	aptSecurityStanza = aptStanza("http://security.ubuntu.com/ubuntu/", aptSecuritySuites, aptComponents, aptKeyring)
	// aptPortsStanza is a ports file: it names no archive URI the rewrite can change.
	aptPortsStanza = aptStanza("http://ports.ubuntu.com/ubuntu-ports", aptArchiveSuites, aptComponents, aptPortsKeyring)
	// aptCountryStanza names a country mirror, whose host only ends in archive.ubuntu.com's name: the rewrite
	// matches the host that follows ://, so this URI is no archive URI of archive.ubuntu.com.
	aptCountryStanza = aptStanza("http://de.archive.ubuntu.com/ubuntu", aptArchiveSuites, aptComponents, aptKeyring)
)

// aptCase is one run of the apt step: the guest it starts from, the mirror it is given and what it must do.
type aptCase struct {
	name string
	// mirror is the value of PARAM_ubuntuMirror.
	mirror string
	// sources is what the guest's ubuntu.sources holds before the run, unless noSources says there is none.
	sources string
	// noSources says the guest has no ubuntu.sources: a VM whose cloud-init wrote none.
	noSources bool
	// runs is how many times the step runs, in order; 0 runs it once. A second run starts from the first run's
	// output and must exit 0 and change nothing.
	runs int
	// wantFailure says the step must exit non-zero: the mirror cannot be honoured, and a warning would leave
	// the VM on the archive the maintainer replaced.
	wantFailure bool
	// wantStderr is what a message of the step's own on standard error must contain, when it is not empty: the
	// trace of set -x never counts, as it names the file of every line it adds.
	wantStderr string
	// wantSources is what ubuntu.sources must hold after the run, ignored when wantAbsent.
	wantSources string
	// wantAbsent says the run must leave no ubuntu.sources, as it started with none.
	wantAbsent bool
	// wantDropin says the run must write the drop-in of apt's timeouts and retries under the root.
	wantDropin bool
}

// aptCases are the runs the step must handle: the drop-in, the rewrite of the archive URIs, the files it must
// leave alone, the ones it cannot rewrite, a second run and the values it must refuse.
var aptCases = []aptCase{
	{
		// The drop-in covers every apt-get call of every step, so it is written whatever the mirror is: the
		// mirror only decides what the archive URIs become.
		name:        "the-drop-in-with-a-mirror",
		mirror:      aptMirrorNL,
		sources:     aptArchiveStanza,
		wantSources: aptMirrorStanza,
		wantDropin:  true,
	},
	{
		name:        "the-drop-in-with-the-default-mirror",
		mirror:      ubuntuMirrorDefault,
		sources:     aptArchiveStanza,
		wantSources: aptArchiveStanza,
		wantDropin:  true,
	},
	{
		// cloud-init writes the URIs with a trailing slash; a mirror is a host and a path, so the step must
		// take either form and leave the mirror's own trailing slash.
		name:        "an-archive-uri-without-a-trailing-slash",
		mirror:      aptMirrorNL,
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu", aptArchiveSuites, aptComponents, aptKeyring),
		wantSources: aptMirrorStanza,
	},
	{
		name:        "an-archive-uri-with-a-trailing-slash",
		mirror:      aptMirrorNL,
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
		wantSources: aptMirrorStanza,
	},
	{
		name:        "an-https-archive-uri",
		mirror:      aptMirrorNL,
		sources:     aptStanza("https://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
		wantSources: aptMirrorStanza,
	},
	{
		// One URIs line may name more than one URI. Rewriting only the first one leaves apt fetching half its
		// packages from the archive the maintainer replaced.
		name:        "two-archive-uris-on-one-line",
		mirror:      aptMirrorNL,
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu http://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
		wantSources: aptStanza(aptMirrorNL+"/ "+aptMirrorNL+"/", aptArchiveSuites, aptComponents, aptKeyring),
	},
	{
		// More than two on one line: a match takes the whitespace after its URI, so the step's second
		// substitution rewrites the URIs the first one skipped (every other one), and a count beyond two must
		// not leave the rest on the archive the maintainer replaced.
		name:        "four-archive-uris-on-one-line",
		mirror:      aptMirrorNL,
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu http://archive.ubuntu.com/ubuntu/ https://archive.ubuntu.com/ubuntu http://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
		wantSources: aptStanza(aptMirrorNL+"/ "+aptMirrorNL+"/ "+aptMirrorNL+"/ "+aptMirrorNL+"/", aptArchiveSuites, aptComponents, aptKeyring),
	},
	{
		// The security updates stay on security.ubuntu.com: a mirror picked for speed must not take them with
		// it. Everything else of both stanzas is what cloud-init wrote.
		name:        "a-security-stanza-beside-the-archive",
		mirror:      aptMirrorNL,
		sources:     aptSecurityStanza + "\n" + aptArchiveStanza,
		wantSources: aptSecurityStanza + "\n" + aptMirrorStanza,
	},
	{
		// A ports file has no archive URI at all, so the mirror never reaches apt. Exiting 0 after a warning
		// leaves the VM fetching from ports.ubuntu.com at full speed whatever UBUNTU_MIRROR said.
		name:        "a-ports-only-file",
		mirror:      aptMirrorNL,
		sources:     aptPortsStanza,
		wantFailure: true,
		wantStderr:  "ubuntu.sources",
		wantSources: aptPortsStanza,
	},
	{
		// The same for a file that names another country's archive mirror: only archive.ubuntu.com's own host
		// is rewritten, so the mirror cannot be honoured and the run must fail rather than warn.
		name:        "a-country-mirror-uri",
		mirror:      aptMirrorNL,
		sources:     aptCountryStanza,
		wantFailure: true,
		wantStderr:  "ubuntu.sources",
		wantSources: aptCountryStanza,
	},
	{
		// Lima runs the step at every boot and every re-provision, so its own output is its next input.
		name:        "running-twice",
		mirror:      aptMirrorNL,
		sources:     aptArchiveStanza,
		runs:        2,
		wantSources: aptMirrorStanza,
	},
	{
		// The default mirror is where cloud-init points the archive: it must change nothing at all, so a VM
		// created without UBUNTU_MIRROR fetches from where it does today.
		name:        "the-default-mirror-changes-nothing",
		mirror:      ubuntuMirrorDefault,
		sources:     aptArchiveStanza,
		wantSources: aptArchiveStanza,
	},
	{
		name:        "the-default-mirror-with-a-trailing-slash-changes-nothing",
		mirror:      ubuntuMirrorDefault + "/",
		sources:     aptArchiveStanza,
		wantSources: aptArchiveStanza,
	},
	{
		// Nothing to rewrite is what the default mirror is: no ubuntu.sources at all is not a failure.
		name:       "no-ubuntu-sources-with-the-default-mirror",
		mirror:     ubuntuMirrorDefault,
		noSources:  true,
		wantAbsent: true,
	},
	{
		// A mirror that cannot be set is a VM that silently keeps the archive it has: the step must say so and
		// fail, so make vm-create stops with the reason instead of handing over a VM that ignores UBUNTU_MIRROR.
		name:        "no-ubuntu-sources-with-a-mirror",
		mirror:      aptMirrorNL,
		noSources:   true,
		wantFailure: true,
		wantStderr:  "ubuntu.sources",
		wantAbsent:  true,
	},
	{
		// Not http(s), and no URL the rewrite can put into the file: refused before sed sees the value, which
		// is why the characters sed and its ',' delimiter would trip on are refused too.
		name:        "an-invalid-mirror-of-another-scheme",
		mirror:      "ftp://x",
		sources:     aptArchiveStanza,
		wantFailure: true,
		wantSources: aptArchiveStanza,
	},
	{
		name:        "an-invalid-mirror-with-a-comma",
		mirror:      "http://a,b",
		sources:     aptArchiveStanza,
		wantFailure: true,
		wantSources: aptArchiveStanza,
	},
	{
		// A '&' in sed's replacement is the whole match, so the check is what keeps it out of the file.
		name:        "an-invalid-mirror-with-an-ampersand",
		mirror:      "http://x&y",
		sources:     aptArchiveStanza,
		wantFailure: true,
		wantSources: aptArchiveStanza,
	},
	{
		// UBUNTU_MIRROR= on the make command line: an empty mirror is no mirror, and it must not reach the file.
		name:        "an-empty-mirror",
		mirror:      "",
		sources:     aptArchiveStanza,
		wantFailure: true,
		wantSources: aptArchiveStanza,
	},
}

// aptMirrorStep returns the one provision script of dev/lima.yaml that rewrites ubuntu.sources: the step Lima
// hands PARAM_ubuntuMirror. Exactly one step may read it - Lima refuses a param no script names, and two steps
// would rewrite the sources twice - so a second one fails the test here.
func aptMirrorStep(t *testing.T, template string) provisionScript {
	t.Helper()

	var found []provisionScript

	for _, p := range provisionScripts(t, template) {
		if strings.Contains(p.text(), ubuntuMirrorEnv) {
			found = append(found, p)
		}
	}

	switch len(found) {
	case 0:
		t.Fatalf("%s has no provision script that names %s, so there is no step whose behaviour these cases could pin: make vm-create UBUNTU_MIRROR has nothing to change (issue #498)",
			limaTemplateRepoPath, ubuntuMirrorEnv)
	case 1:
		return found[0]
	default:
		t.Fatalf("%s has %d provision scripts that name %s, want exactly one: the mirror is rewritten once, from the one step that runs before the first apt-get call (issue #498)",
			limaTemplateRepoPath, len(found), ubuntuMirrorEnv)
	}

	return provisionScript{}
}

// requireAPTRootSeam fails the case unless the step prefixes the paths it writes with SHIFT2EU_APT_ROOT. It runs
// before the step does: without the prefix the step writes this machine's own /etc/apt, so no case may run it
// until the step names the root it is given.
func requireAPTRootSeam(t *testing.T, name, script string) {
	t.Helper()

	if strings.Contains(script, aptRootEnv) {
		return
	}

	t.Fatalf("%s: %s's step that reads %s never names %s, so it writes %s and %s directly and these cases cannot run it without root and without touching this machine: the step must read root=\"${%s:-}\" and prefix both paths with it, empty in the guest, as the swap step does with SHIFT2EU_SWAP_ROOT (issue #498). No case runs the step until it does.",
		name, limaTemplateRepoPath, ubuntuMirrorEnv, aptRootEnv, aptTimeoutsDropin, ubuntuSourcesPath, aptRootEnv)
}

// aptGuest is one guest for a run of the step: a temporary directory standing for the VM's root, and the two
// files of apt the step writes, under it.
type aptGuest struct {
	root        string // what SHIFT2EU_APT_ROOT names: the fake guest's root
	scriptPath  string // the step's script, written out for bash
	sourcesPath string // root + ubuntuSourcesPath: the file the mirror rewrites
	dropinPath  string // root + aptTimeoutsDropin: the timeouts and retries
}

// newAPTGuest builds a guest as a case needs it: the root, and the ubuntu.sources the case starts from, unless
// it starts with none.
func newAPTGuest(t *testing.T, tc aptCase) *aptGuest {
	t.Helper()

	base := t.TempDir()

	guest := &aptGuest{
		root:        filepath.Join(base, "guest"),
		scriptPath:  filepath.Join(base, "apt-step.sh"),
		sourcesPath: filepath.Join(base, "guest", ubuntuSourcesPath),
		dropinPath:  filepath.Join(base, "guest", aptTimeoutsDropin),
	}

	if err := os.MkdirAll(guest.root, 0o700); err != nil {
		t.Fatalf("create the fake guest %s: %v", guest.root, err)
	}

	if !tc.noSources {
		// Mode 0644, as cloud-init writes it: what apt reads.
		writeFileMode(t, guest.sourcesPath, []byte(tc.sources), 0o644)
	}

	return guest
}

// run runs the step in the guest, with only the variables it needs, and returns its output.
func (g *aptGuest) run(t *testing.T, mirror string) (stdout, stderr string, err error) {
	t.Helper()

	cmd := exec.Command("bash", g.scriptPath)
	cmd.Dir = g.root
	cmd.Env = envList(map[string]string{
		"PATH":          aptStepPath,
		aptRootEnv:      g.root,
		ubuntuMirrorEnv: mirror,
	})

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err = cmd.Run()

	return out.String(), errOut.String(), err
}

// TestAPTSourcesStep runs the template's apt step against a fake guest and pins what it does there: the drop-in
// it writes, the archive URIs it rewrites, the files it leaves alone, the ones it refuses to leave half
// rewritten, and a second run.
func TestAPTSourcesStep(t *testing.T) {
	t.Parallel()

	script := aptMirrorStep(t, readRepoFile(t, limaTemplateRepoPath)).text() + "\n"

	for _, tc := range aptCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runAPTCase(t, script, tc)
		})
	}
}

// runAPTCase runs script, the step's text, in a guest a case describes, and pins what it did there.
func runAPTCase(t *testing.T, script string, tc aptCase) {
	t.Helper()

	// The seam first: without it this test would run a script that writes the machine's own /etc/apt.
	requireAPTRootSeam(t, tc.name, script)

	guest := newAPTGuest(t, tc)
	writeFileMode(t, guest.scriptPath, []byte(script), 0o600)

	runs := tc.runs
	if runs == 0 {
		runs = 1
	}

	for run := 1; run <= runs; run++ {
		stdout, stderr, err := guest.run(t, tc.mirror)

		// A case that wants a failure wants it of the first run: a further run starts from that run's output,
		// which the step must then accept.
		if tc.wantFailure && run == 1 {
			if err == nil {
				t.Errorf("%s: run %d of the step exited 0 for ubuntuMirror %q, want a non-zero exit: a mirror the step cannot put into %s must stop provisioning with a message, not warn and hand over a VM that fetches from the archive the maintainer replaced (issue #498)\nstdout: %s\nstderr: %s",
					tc.name, run, tc.mirror, ubuntuSourcesPath, stdout, stderr)
			}
		} else if err != nil {
			t.Errorf("%s: run %d of the step failed for ubuntuMirror %q, want exit 0: %v\nstdout: %s\nstderr: %s",
				tc.name, run, tc.mirror, err, stdout, stderr)
		}

		// The step runs under set -x, whose lines name ubuntu.sources on every command it traces: a message must
		// be one of the step's own.
		if tc.wantStderr != "" && run == 1 && !strings.Contains(nonTraceLines(stderr), tc.wantStderr) {
			t.Errorf("%s: no message of the step's own on standard error names %s for ubuntuMirror %q, so the run reads as a silent one and nobody can tell why the mirror was not set:\n%s",
				tc.name, tc.wantStderr, tc.mirror, stderr)
		}
	}

	assertAPTSources(t, guest, tc)

	if tc.wantDropin {
		assertAPTDropin(t, guest, tc)
	}
}

// assertAPTSources pins ubuntu.sources after the run: the file a case expects, byte for byte, or none.
func assertAPTSources(t *testing.T, g *aptGuest, tc aptCase) {
	t.Helper()

	body, err := os.ReadFile(g.sourcesPath)

	if tc.wantAbsent {
		if err == nil {
			t.Errorf("%s: the step wrote %s, want none: the guest had no ubuntu.sources, and a run that cannot set the mirror creates none either",
				tc.name, g.sourcesPath)
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("read %s: %v", g.sourcesPath, err)
		}

		return
	}

	if err != nil {
		t.Fatalf("%s: read %s: %v; the step must leave the file the case describes there. A step that ignored %s writes the machine's own %s instead",
			tc.name, g.sourcesPath, err, aptRootEnv, ubuntuSourcesPath)
	}

	if diff := cmp.Diff(tc.wantSources, string(body)); diff != "" {
		t.Errorf("%s: %s after the run for ubuntuMirror %q (-want +got):\n%s\n"+
			"Every archive URI - every one of several on a URIs line - becomes the mirror with one trailing slash and the rest of the file stays byte for byte as cloud-init wrote it; the security URIs, and a URI whose host only ends in archive.ubuntu.com, are not archive URIs of that host. With the default mirror, and for a mirror the step refuses before it rewrites anything, the file is unchanged.",
			tc.name, ubuntuSourcesPath, tc.mirror, diff)
	}
}

// assertAPTDropin pins the drop-in the step writes under the root: apt's timeouts and retries, so a mirror that
// trickles or stops fails with apt's own error instead of hanging until Lima's probe times out (issue #498).
func assertAPTDropin(t *testing.T, g *aptGuest, tc aptCase) {
	t.Helper()

	body, err := os.ReadFile(g.dropinPath)
	if err != nil {
		t.Fatalf("%s: read %s: %v; the step writes the drop-in of apt's timeouts and retries under %s, which is empty in the guest and this temporary directory here. A step that ignored it writes the machine's own %s instead",
			tc.name, g.dropinPath, err, aptRootEnv, aptTimeoutsDropin)
	}

	for _, name := range aptAcquireOptions {
		match := aptAcquireOption(name).FindStringSubmatch(string(body))
		if match == nil {
			t.Errorf("%s: the drop-in %s names no Acquire::%s, so apt waits on a mirror that trickles or stops for ever (issue #498):\n%s",
				tc.name, g.dropinPath, name, body)

			continue
		}

		value, err := strconv.Atoi(match[1])
		if err != nil || value <= 0 {
			t.Errorf("%s: the drop-in sets Acquire::%s to %q, want a positive whole number such as 30 seconds for a timeout and 5 for Retries (issue #498):\n%s",
				tc.name, name, match[1], body)
		}
	}
}
