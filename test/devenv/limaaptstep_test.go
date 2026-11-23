// Pins what the VM's apt step of dev/lima.yaml (issue #498) DOES, by running it, rather than only what its text
// says: the drop-in of apt's timeouts and retries, and the rewrite of the archive URIs of
// /etc/apt/sources.list.d/ubuntu.sources to the ubuntuMirror param.
//
// limaapt_test.go pins the step's half in the template: one system step writes the drop-in before the first
// apt-get call, reads PARAM_ubuntuMirror, and leaves security.ubuntu.com alone. This file pins the same step's
// behaviour on the files it is given, because its defects are invisible in its text: a `URIs:` line that names
// two archive URIs gets only the first one rewritten, a file with no archive URI to rewrite (a ports file, a
// country mirror) makes the step warn and exit 0, a comment or a field of another name that names the archive URI
// is rewritten as if it were one, and an in-place `sed` leaves a backup file in the directory apt reads.
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
//   - The rewrite works per field and only on the URIs field: the field begins on a line in column 0 whose name
//     is the field's in any case (`URIs:`, `uris:`, `uRiS:`), and it continues on the lines after it that start
//     with a space or a tab, as deb822 reads a field; the first line that starts with no whitespace ends it. A
//     field's name is a name in column 0 only: an indented `URIs:` line is a continuation of the field before it,
//     not a URIs field, and apt fetches nothing from it. Each whitespace-separated field of the URIs field - the
//     first line's, and a continuation line's - that is exactly an http(s)://archive.ubuntu.com/ubuntu URI, with
//     or without its trailing slash, becomes the mirror with one trailing slash, however many such fields the
//     field holds and whatever separates them (a tab as well as a space), in one run whatever the mirror is - an
//     archive mirror of archive.ubuntu.com's own host included, which a second pass would match again. A
//     continuation's leading whitespace is part of its line and stays. Every other line, a comment and a field
//     of another name included, stays byte for byte: neither is a URI apt fetches from, and a file whose URIs
//     field holds no archive URI is a file the mirror cannot be set in, which fails as a ports file does.
//   - A non-default mirror that finds no archive URI to rewrite - a ports file, a country mirror, a comment or a
//     field of another name that names one, a file that already names the mirror is fine - stops the step with a
//     non-zero exit and a message on standard error that names ubuntu.sources and prints the file's `URIs:` lines
//     (the maintainer's clue to what the file does fetch from), and leaves the file byte for byte as it was:
//     exiting 0 after a warning leaves the VM fetching from the archive the maintainer replaced. It is RED until
//     the step does that.
//   - The step keeps to the portable tools the swap step keeps to: `sed -i` is GNU-only, and BSD sed - the Mac
//     these cases can run on, and CI's job for it - takes the word after `-i` for a backup suffix, which leaves
//     ubuntu.sources-e beside the file. The rewrite writes a temporary file and copies it back, and
//     sources.list.d holds ubuntu.sources and nothing else after a run that copies it back.
//   - A copy-back that fails halfway truncates ubuntu.sources and leaves the temporary file its only complete
//     copy, so that run stops with a non-zero exit and keeps ubuntu.sources.shift2eu-new, naming it on standard
//     error: apt reads only the `*.list` and `*.sources` names of sources.list.d, so the kept file is no source,
//     and it is what repairs the truncation. A copy that succeeds leaves no temporary file.
//   - A default mirror (http://archive.ubuntu.com/ubuntu, with or without a trailing slash) changes nothing and
//     exits 0, even with no ubuntu.sources at all; a non-default mirror with no ubuntu.sources fails, naming the
//     file. An invalid mirror (not an http:// or https:// URL of letters, digits and . - / : _ ~ only) fails and
//     leaves the file alone.
//   - The step is idempotent, as Lima runs it at every boot: a second run on its own output exits 0 and leaves
//     the file byte for byte equal.
//
// The step runs under bash with only PATH, the root and the mirror in its environment, and nothing is faked but
// the one `cat` the copy-back case puts first on PATH: mkdir, awk, grep and cat are the machine's own, so the cases
// pin what the step does with them.
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
	// aptStepPath is what the step finds on PATH. Nothing is faked but the failing `cat` of
	// "a-copy-back-that-stops-halfway": the step needs mkdir, awk, cat and grep and nothing else, so the cases
	// pin what it does with the machine's own tools.
	aptStepPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	// aptRewrittenTempName is the name of the temporary file the rewrite writes beside ubuntu.sources and copies
	// back: the file a copy that stopped halfway must leave behind, with the rewritten file's content.
	aptRewrittenTempName = "ubuntu.sources.shift2eu-new"

	// The parts of a deb822 stanza of ubuntu.sources that are not its URIs line, as cloud-init writes them.
	aptArchiveSuites  = "noble noble-updates noble-backports"
	aptSecuritySuites = "noble-security"
	aptComponents     = "main universe restricted multiverse"
	aptKeyring        = "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
	aptPortsKeyring   = "/usr/share/keyrings/ubuntu-ports-archive-keyring.gpg"
)

// aptStanza is one deb822 stanza of ubuntu.sources: the file's shape around the URIs line a case pins.
func aptStanza(uris, suites, components, signedBy string) string {
	return aptStanzaNamed("URIs", uris, suites, components, signedBy)
}

// aptStanzaNamed is aptStanza with the URIs field's name spelled as field: deb822 reads `uris:`, `URIs:` and
// `uRiS:` alike, so a case pins the spelling as well as the URIs.
func aptStanzaNamed(field, uris, suites, components, signedBy string) string {
	return "Types: deb\n" +
		field + ": " + uris + "\n" +
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
	// wantStderrURIs is the URIs line of the file the message must also print, when it is not empty: a mirror
	// that cannot be set is a question of what the file does fetch from, so the message carries those lines.
	wantStderrURIs string
	// wantSources is what ubuntu.sources must hold after the run, ignored when wantAbsent.
	wantSources string
	// wantAbsent says the run must leave no ubuntu.sources, as it started with none.
	wantAbsent bool
	// wantDropin says the run must write the drop-in of apt's timeouts and retries under the root.
	wantDropin bool
	// failingCat says the guest's PATH holds a `cat` first that writes nothing and exits 1: the copy-back of the
	// rewrite stops halfway, which is the run the temporary file must survive.
	failingCat bool
	// wantKeptTemp says the run must keep aptRewrittenTempName beside ubuntu.sources, holding wantSources: a copy
	// that stopped halfway truncated ubuntu.sources, and the rewritten file is that run's only complete copy.
	wantKeptTemp bool
	// wantSourcesUnpinned says the case pins no content of ubuntu.sources: a copy that stopped halfway leaves it
	// as far as the copy got, which the kept file is what repairs, so the case pins the exit, the kept file and
	// the message instead.
	wantSourcesUnpinned bool
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
		// The mirror is valid and is not the default: it is archive.ubuntu.com's own host over https. A second
		// sed pass matches the URI the first one rewrote, so the pass that would rewrite its neighbour never
		// comes: every other URI stays on http:// for ever, and a VM whose mirror this is fetches half its
		// packages from the archive the maintainer replaced. Rewriting per field rewrites the line in one run,
		// and the second run, which Lima makes at every boot, finds its own output.
		name:        "two-archive-uris-under-an-https-archive-mirror",
		mirror:      "https://archive.ubuntu.com/ubuntu",
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu http://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
		runs:        2,
		wantSources: aptStanza("https://archive.ubuntu.com/ubuntu/ https://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
	},
	{
		name:        "three-archive-uris-under-an-https-archive-mirror",
		mirror:      "https://archive.ubuntu.com/ubuntu",
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu http://archive.ubuntu.com/ubuntu/ http://archive.ubuntu.com/ubuntu", aptArchiveSuites, aptComponents, aptKeyring),
		runs:        2,
		wantSources: aptStanza("https://archive.ubuntu.com/ubuntu/ https://archive.ubuntu.com/ubuntu/ https://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
	},
	{
		// deb822 allows any whitespace between the fields of a URIs line, and a tab is whitespace to the shell
		// as a space is: the rewrite must not depend on how the fields are separated.
		name:        "tab-separated-archive-uris-on-one-line",
		mirror:      aptMirrorNL,
		sources:     aptStanza("http://archive.ubuntu.com/ubuntu\thttp://archive.ubuntu.com/ubuntu/", aptArchiveSuites, aptComponents, aptKeyring),
		wantSources: aptStanza(aptMirrorNL+"/\t"+aptMirrorNL+"/", aptArchiveSuites, aptComponents, aptKeyring),
	},
	{
		// deb822 reads a field's name in any case, so `uris:` is the URIs line as `URIs:` is: apt fetches from
		// that stanza. A rewrite that matches only `URIs:` leaves it on the archive the maintainer replaced while
		// it rewrites the stanza beside it, and the run looks like a success.
		name:        "a-lowercase-uris-field-beside-the-archive",
		mirror:      aptMirrorNL,
		sources:     aptStanzaNamed("uris", "http://archive.ubuntu.com/ubuntu", aptArchiveSuites, aptComponents, aptKeyring) + "\n" + aptArchiveStanza,
		wantSources: aptStanzaNamed("uris", aptMirrorNL+"/", aptArchiveSuites, aptComponents, aptKeyring) + "\n" + aptMirrorStanza,
	},
	{
		// deb822 continues a field on the lines after it that start with a space or a tab, so a URIs field spread
		// over two lines is one field and apt fetches from both its URIs. A rewrite that stops at the field's
		// first line leaves the continuation on the archive the maintainer replaced, and the leading whitespace
		// of that line is part of the continuation, not of the URI.
		name:   "an-archive-uri-on-a-continuation-line",
		mirror: aptMirrorNL,
		sources: "Types: deb\n" +
			"URIs: http://archive.ubuntu.com/ubuntu\n" +
			" http://archive.ubuntu.com/ubuntu/\n" +
			"Suites: " + aptArchiveSuites + "\n" +
			"Components: " + aptComponents + "\n" +
			"Signed-By: " + aptKeyring + "\n",
		wantSources: "Types: deb\n" +
			"URIs: " + aptMirrorNL + "/\n" +
			" " + aptMirrorNL + "/\n" +
			"Suites: " + aptArchiveSuites + "\n" +
			"Components: " + aptComponents + "\n" +
			"Signed-By: " + aptKeyring + "\n",
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
		// An archive URI in a comment is no URI apt fetches from, so a ports file whose comment names one has
		// nothing the mirror can be set in: the step must fail as it does for any ports file, and leave the
		// comment - and the rest of the file - byte for byte. A guard that matches the whole file rewrites the
		// comment, exits 0 and hands over a VM on ports.ubuntu.com.
		name:           "a-ports-file-whose-comment-names-the-archive-uri",
		mirror:         aptMirrorNL,
		sources:        "# http://archive.ubuntu.com/ubuntu\n" + aptPortsStanza,
		wantFailure:    true,
		wantStderr:     "ubuntu.sources",
		wantStderrURIs: "URIs: http://ports.ubuntu.com/ubuntu-ports",
		wantSources:    "# http://archive.ubuntu.com/ubuntu\n" + aptPortsStanza,
	},
	{
		// Only a URIs line carries URIs apt fetches from. A field of another name that names the archive URI
		// is the file's own text: the URIs line becomes the mirror and that field stays byte for byte.
		name:        "an-archive-uri-off-the-uris-line",
		mirror:      aptMirrorNL,
		sources:     aptArchiveStanza + "X-Note: http://archive.ubuntu.com/ubuntu\n",
		wantSources: aptMirrorStanza + "X-Note: http://archive.ubuntu.com/ubuntu\n",
	},
	{
		// A continuation ends at the first line that starts with no whitespace: the field of another name after
		// it is no part of the URIs field, and a URI in it is no URI apt fetches from. A rewrite that carries its
		// continuation state past that line - or a guard that scans the whole file - rewrites the X-Note and exits
		// 0, and hands over a VM still fetching from ports.ubuntu.com.
		name:           "an-archive-uri-after-a-ports-uris-field-has-ended",
		mirror:         aptMirrorNL,
		sources:        "Types: deb\nURIs: http://ports.ubuntu.com/ubuntu-ports\nSuites: resolute\nComponents: " + aptComponents + "\nSigned-By: " + aptPortsKeyring + "\nX-Note: http://archive.ubuntu.com/ubuntu\n",
		wantFailure:    true,
		wantStderr:     "ubuntu.sources",
		wantStderrURIs: "URIs: http://ports.ubuntu.com/ubuntu-ports",
		wantSources:    "Types: deb\nURIs: http://ports.ubuntu.com/ubuntu-ports\nSuites: resolute\nComponents: " + aptComponents + "\nSigned-By: " + aptPortsKeyring + "\nX-Note: http://archive.ubuntu.com/ubuntu\n",
	},
	{
		// A field's name is a name in the first column only: an indented `URIs:` line after another field is that
		// field's continuation, as deb822 reads it, not a URIs field, so its archive URI is no URI apt fetches
		// from. The ports URI is the file's only URIs field, the mirror cannot be set in it, and the run must fail
		// closed and leave every line, the indented one included, byte for byte. A guard that trims a line before
		// matching its name reads the continuation as a URIs line, rewrites it, exits 0 and hands over a VM on
		// ports.ubuntu.com.
		name:   "an-indented-uris-line-after-another-field",
		mirror: aptMirrorNL,
		sources: "Types: deb\n" +
			"URIs: http://ports.ubuntu.com/ubuntu-ports\n" +
			"Suites: " + aptArchiveSuites + "\n" +
			"  URIs: http://archive.ubuntu.com/ubuntu\n" +
			"Components: " + aptComponents + "\n" +
			"Signed-By: " + aptPortsKeyring + "\n",
		wantFailure:    true,
		wantStderr:     "ubuntu.sources",
		wantStderrURIs: "URIs: http://ports.ubuntu.com/ubuntu-ports",
		wantSources: "Types: deb\n" +
			"URIs: http://ports.ubuntu.com/ubuntu-ports\n" +
			"Suites: " + aptArchiveSuites + "\n" +
			"  URIs: http://archive.ubuntu.com/ubuntu\n" +
			"Components: " + aptComponents + "\n" +
			"Signed-By: " + aptPortsKeyring + "\n",
	},
	{
		// A ports file has no archive URI at all, so the mirror never reaches apt. Exiting 0 after a warning
		// leaves the VM fetching from ports.ubuntu.com at full speed whatever UBUNTU_MIRROR said.
		name:        "a-ports-only-file",
		mirror:      aptMirrorNL,
		sources:     aptPortsStanza,
		wantFailure: true,
		wantStderr:  "ubuntu.sources",
		// What the file does fetch from is the maintainer's only clue to why the mirror was not set.
		wantStderrURIs: "URIs: http://ports.ubuntu.com/ubuntu-ports",
		wantSources:    aptPortsStanza,
	},
	{
		// The same for a file that names another country's archive mirror: only archive.ubuntu.com's own host
		// is rewritten, so the mirror cannot be honoured and the run must fail rather than warn.
		name:           "a-country-mirror-uri",
		mirror:         aptMirrorNL,
		sources:        aptCountryStanza,
		wantFailure:    true,
		wantStderr:     "ubuntu.sources",
		wantStderrURIs: "URIs: http://de.archive.ubuntu.com/ubuntu",
		wantSources:    aptCountryStanza,
	},
	{
		// The mirror's name in a comment is the file's own text: apt fetches nothing from a comment, so a ports
		// file whose comment names the mirror is still a file the mirror cannot be set in. A guard that greps the
		// whole file for the mirror exits 0 and hands over a VM on ports.ubuntu.com.
		name:           "a-ports-file-whose-comment-names-the-mirror",
		mirror:         aptMirrorNL,
		sources:        "# " + aptMirrorNL + "/\n" + aptPortsStanza,
		wantFailure:    true,
		wantStderr:     "ubuntu.sources",
		wantStderrURIs: "URIs: http://ports.ubuntu.com/ubuntu-ports",
		wantSources:    "# " + aptMirrorNL + "/\n" + aptPortsStanza,
	},
	{
		// The mirror's path is a prefix of this ports URI's: nl.archive.ubuntu.com/ubuntu-ports starts with
		// nl.archive.ubuntu.com/ubuntu. A guard that matches a prefix takes the ports file for one that already
		// names the mirror, exits 0, and hands over a VM that fetches nothing from the mirror - the same silent
		// failure as a country mirror, with the mirror's own host on it.
		name:           "a-ports-uri-that-only-starts-with-the-mirror",
		mirror:         aptMirrorNL,
		sources:        aptStanza("http://nl.archive.ubuntu.com/ubuntu-ports", aptArchiveSuites, aptComponents, aptPortsKeyring),
		wantFailure:    true,
		wantStderr:     "ubuntu.sources",
		wantStderrURIs: "URIs: http://nl.archive.ubuntu.com/ubuntu-ports",
		wantSources:    aptStanza("http://nl.archive.ubuntu.com/ubuntu-ports", aptArchiveSuites, aptComponents, aptPortsKeyring),
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
		// A copy that stops halfway - here the step's `cat` writes nothing and fails - truncates ubuntu.sources,
		// and the rewritten temporary file is then its only complete copy: deleting that file on the way out, as
		// an EXIT trap that always removes it does, loses the file apt reads for good. The run must stop with a
		// non-zero exit, keep the temporary file holding the rewritten file, and say where it is. apt reads only
		// the *.list and *.sources names of sources.list.d, so the kept name is no source of its own.
		name:                "a-copy-back-that-stops-halfway",
		mirror:              aptMirrorNL,
		sources:             aptArchiveStanza,
		wantSources:         aptMirrorStanza,
		wantSourcesUnpinned: true,
		failingCat:          true,
		wantKeptTemp:        true,
		wantFailure:         true,
		wantStderr:          aptRewrittenTempName,
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
	fakeBinPath string // the directory of the fake `cat` the script finds first on PATH, empty when none is faked
}

// newAPTGuest builds a guest as a case needs it: the root, the ubuntu.sources the case starts from unless it
// starts with none, and the fake tool of a run that pins a copy-back stopping halfway.
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

	if tc.failingCat {
		// The machine's own cat truncates the sources file and writes it back; this one truncates it and fails,
		// writing nothing, so the copy-back stops exactly as a full disk or a killed run would leave it.
		guest.fakeBinPath = filepath.Join(base, "fake-bin")
		writeFileMode(t, filepath.Join(guest.fakeBinPath, "cat"), []byte("#!/bin/sh\n# The copy-back of the apt step's rewrite: fail after writing nothing.\nexit 1\n"), 0o700)
	}

	return guest
}

// run runs the step in the guest, with only the variables it needs, and returns its output.
func (g *aptGuest) run(t *testing.T, mirror string) (stdout, stderr string, err error) {
	t.Helper()

	path := aptStepPath
	if g.fakeBinPath != "" {
		path = g.fakeBinPath + ":" + aptStepPath
	}

	cmd := exec.Command("bash", g.scriptPath)
	cmd.Dir = g.root
	cmd.Env = envList(map[string]string{
		"PATH":          path,
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

		// And it prints the file's URIs lines, the only thing that says what the file does fetch from: a
		// message that only says there is no archive URI leaves the maintainer guessing between a ports file,
		// a country mirror and a comment that names one.
		if tc.wantStderrURIs != "" && run == 1 && !strings.Contains(nonTraceLines(stderr), tc.wantStderrURIs) {
			t.Errorf("%s: the failure message for ubuntuMirror %q does not print the file's URIs line %q, want the file's `URIs:` lines on standard error: they are what the maintainer compares with the mirror to see why it cannot be set:\n%s",
				tc.name, tc.mirror, tc.wantStderrURIs, stderr)
		}
	}

	assertAPTSources(t, guest, tc)
	assertAPTKeptTemp(t, guest, tc)
	assertAPTNoLeftovers(t, guest, tc)

	if tc.wantDropin {
		assertAPTDropin(t, guest, tc)
	}
}

// assertAPTSources pins ubuntu.sources after the run: the file a case expects, byte for byte, or none. A case
// whose copy-back stopped halfway pins none of it: the failed copy left as much of the rewritten file there as it
// wrote, and the kept temporary file is what repairs it.
func assertAPTSources(t *testing.T, g *aptGuest, tc aptCase) {
	t.Helper()

	if tc.wantSourcesUnpinned {
		return
	}

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

// assertAPTKeptTemp pins the temporary file of a copy-back that stopped halfway: it is still there, holding the
// whole rewritten file, so a run that truncated ubuntu.sources left the maintainer a complete copy to put back.
// apt reads only the `*.list` and `*.sources` names of sources.list.d and ignores every other name with a notice,
// so a kept ubuntu.sources.shift2eu-new is no source of its own.
func assertAPTKeptTemp(t *testing.T, g *aptGuest, tc aptCase) {
	t.Helper()

	if !tc.wantKeptTemp {
		return
	}

	path := g.sourcesPath + ".shift2eu-new"

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: read %s: %v; a copy-back that stopped halfway truncated ubuntu.sources, so this file is the run's only complete copy and the step must keep it, naming it on standard error rather than deleting it with every other exit (issue #498)",
			tc.name, path, err)
	}

	if diff := cmp.Diff(tc.wantSources, string(body)); diff != "" {
		t.Errorf("%s: the kept %s after the run for ubuntuMirror %q (-want +got):\n%s\nIt is the file the failed copy-back would have written into ubuntu.sources, so it must hold the whole rewrite, byte for byte: the maintainer copies it back to repair the truncation (issue #498)",
			tc.name, path, tc.mirror, diff)
	}
}

// assertAPTNoLeftovers pins what the step left beside ubuntu.sources: nothing, but the temporary file of the one
// case whose copy-back stopped halfway. apt reads only the files named `*.list` and `*.sources` in sources.list.d
// and ignores every other name there with a notice, so a backup file of an in-place sed (BSD sed takes the word
// after -i for a suffix, and leaves ubuntu.sources-e) or the temporary file a rewrite copies back is no source of
// its own. The pin stays anyway, on cleanliness and because a leftover that ends in `.sources` - a backup named
// ubuntu.sources.bak.sources, say - would be read as a source. An empty directory is what the cases that start
// with no ubuntu.sources may leave, as the step writes none either.
func assertAPTNoLeftovers(t *testing.T, g *aptGuest, tc aptCase) {
	t.Helper()

	dir := filepath.Dir(g.sourcesPath)

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		// No sources.list.d at all: the guest had none and the step wrote none.
		return
	}

	if err != nil {
		t.Fatalf("%s: read %s: %v", tc.name, dir, err)
	}

	for _, entry := range entries {
		if entry.Name() == filepath.Base(g.sourcesPath) {
			continue
		}

		if tc.wantKeptTemp && entry.Name() == aptRewrittenTempName {
			// The one file a failed copy-back must keep, pinned by assertAPTKeptTemp.
			continue
		}

		t.Errorf("%s: the step left %s in %s beside ubuntu.sources, want only ubuntu.sources there: a backup file of an in-place edit (BSD sed takes the word after -i for a suffix, and leaves ubuntu.sources-e) or the temporary file a successful rewrite copies back stays in sources.list.d, where apt reads the *.sources and *.list names and a run that left one behind could leave a file apt takes for a source of its own (issue #498)",
			tc.name, entry.Name(), dir)
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

// TestAPTSourcesStepKeepsToPortableTools pins the step's text: it never runs `sed -i`. The cases above run the
// step with the machine's own tools, but a Mac's sed is BSD's and CI runs this package there too
// (.github/workflows/ci.yml, "Makefile tests under make 3.81"): BSD sed - an in-place edit's suffix takes the word
// after -i, and GNU's -i takes no argument at all - reads `sed -i -e <expr> <file>` as a suffix "-e" and leaves
// ubuntu.sources-e beside the file, which the cases above only see on such a machine. The name is no apt source
// (apt reads only the `*.list` and `*.sources` names of sources.list.d, and ignores the rest with a notice), so
// the pin is here for cleanliness too: a leftover that ends in `.sources` would be read. The pin is on the text,
// so the Linux test run catches the call as well. It follows the portability rule the swap step is held to in
// limaswap_test.go (no GNU-only stat -c %s, no bash 4 the Mac's bash 3.2 lacks): the rewrite writes a temporary
// file and copies it back with tools both have.
func TestAPTSourcesStepKeepsToPortableTools(t *testing.T) {
	t.Parallel()

	script := aptMirrorStep(t, readRepoFile(t, limaTemplateRepoPath)).text()

	for _, command := range aptSedCommands(script) {
		for _, arg := range command {
			if !aptSedOptionEditsInPlace(arg) {
				continue
			}

			t.Errorf("%s's apt step runs sed with %s, an in-place edit, want the rewrite written to a temporary file and copied back: sed -i is GNU-only, and BSD sed (the Mac these cases can run on, and CI's job for it) takes the word after it for a backup suffix, so the step fails there and leaves ubuntu.sources-e beside %s. apt reads only the `*.list` and `*.sources` names of that directory, so such a leftover is no source, but the step has no business leaving it there (issue #498)",
				limaTemplateRepoPath, strings.Join(append([]string{"sed"}, command...), " "), filepath.Dir(ubuntuSourcesPath))
		}
	}
}

// TestAPTSedCommandsSeesEverySedCall pins the scan TestAPTSourcesStepKeepsToPortableTools reads the step's text
// with: a call the step writes without a space after a shell operator is a call all the same, and a scan that
// only finds a word `sed` standing on its own misses `f="$(sed -i ...)"`, `|sed -i`, `;sed -i`, `&&sed -i`. A
// step that wrote one of those would pass the pin above unwatched, and leave ubuntu.sources-e beside the file on a
// Mac as surely as a plain `sed -i` would.
func TestAPTSedCommandsSeesEverySedCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want [][]string
	}{
		{"a-word-of-its-own", `sed -i -e 's/a/b/' "$f"`, [][]string{{"-i", "-e"}}},
		{"a-command-substitution", `f="$(sed -i -e 's/a/b/' "$f")"`, [][]string{{"-i", "-e"}}},
		{"a-pipe", `printf '%s' "${x}" |sed -i -e 's/a/b/' "$f"`, [][]string{{"-i", "-e"}}},
		{"a-semicolon", `set -e;sed -i -e 's/a/b/' "$f"`, [][]string{{"-i", "-e"}}},
		{"a-conditional-and", `[ -f "$f" ]&&sed -i -e 's/a/b/' "$f"`, [][]string{{"-i", "-e"}}},
		{"an-xargs-call", `printf '%s\n' "$f" | xargs sed -i -e 's/a/b/'`, [][]string{{"-i", "-e"}}},
		{"an-in-place-edit-with-a-backup-suffix", `sed -i.bak -e 's/a/b/' "$f"`, [][]string{{"-i.bak", "-e"}}},
		{"a-call-with-no-in-place-option", `sed -e 's/a/b/' "$f"`, [][]string{{"-e"}}},
		{"another-tool-whose-name-holds-sed", `: "$(sedx -i -e 's/a/b/')"`, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, aptSedCommands("#!/bin/bash\nset -eux -o pipefail\n"+tc.line+"\n")); diff != "" {
				t.Errorf("aptSedCommands(%q) (-want +got):\n%s", tc.line, diff)
			}
		})
	}
}

// aptSedCommands returns the words that follow every sed the step's code runs, option words only: the comment
// lines are dropped (a comment that names `sed -i`, or forbids it, pins nothing) and a backslash continuation is
// joined, as bash reads the script, so the options of a call spread over two lines are seen together.
func aptSedCommands(script string) [][]string {
	var commands [][]string

	for _, line := range aptStepCodeLines(script) {
		words := aptShellWords(line)

		for i, word := range words {
			if word != "sed" {
				continue
			}

			var options []string

			for _, arg := range words[i+1:] {
				// The first word that is no option is sed's first script, and every word after it names a
				// file: the options are all before it.
				if !strings.HasPrefix(arg, "-") {
					break
				}

				options = append(options, arg)
			}

			commands = append(commands, options)
		}
	}

	return commands
}

// aptShellWords returns the words of one line of shell code: the fields a shell breaks on whitespace, and on the
// operators and substitutions that start a new command - `;`, `&&`, `|`, `$(`, `(`, a backtick - so a call
// written as `$(sed`, `|sed`, `;sed` or `&&sed` holds the word `sed` as a plain `sed` does. A `sed` inside quotes
// is no command, but the scan only looks for a call, and the step's text is written by hand: one quoted `sed` in
// it would be a finding of its own.
func aptShellWords(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		switch r {
		case ' ', '\t', ';', '|', '&', '(', ')', '`':
			return true
		}

		return false
	})
}

// aptStepCodeLines returns the step's script as bash reads its commands: one line per command, the comment of each
// line dropped and the backslash continuations joined.
func aptStepCodeLines(script string) []string {
	var (
		lines     []string
		continued strings.Builder
	)

	for _, line := range strings.Split(script, "\n") {
		code := strings.TrimSpace(aptStepLineCode(line))
		if code == "" {
			continue
		}

		if trimmed, cut := strings.CutSuffix(code, "\\"); cut {
			continued.WriteString(trimmed)
			continued.WriteString(" ")

			continue
		}

		continued.WriteString(code)
		lines = append(lines, continued.String())
		continued.Reset()
	}

	return lines
}

// aptStepLineCode returns line without its comment: the `#` that starts a word outside quotes is bash's comment,
// so the rest of the line is text and the shell never runs it.
func aptStepLineCode(line string) string {
	var quote rune

	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}

	return line
}

// aptSedOptionEditsInPlace reports whether arg, one option of a sed call, makes sed edit its file in place: -i, the
// letters cluster that holds it (-Ei, -ni), its BSD backup-suffix form (-i.bak) and GNU's --in-place.
func aptSedOptionEditsInPlace(arg string) bool {
	if arg == "--in-place" || strings.HasPrefix(arg, "--in-place=") {
		return true
	}

	if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}

	for _, r := range arg[1:] {
		if r == 'i' {
			return true
		}

		// A suffix follows the option's letters (-i.bak, or GNU's -iSUFFIX): only what comes before it is
		// this option's cluster.
		if r < 'A' || r > 'Z' && r < 'a' || r > 'z' {
			return false
		}
	}

	return false
}
