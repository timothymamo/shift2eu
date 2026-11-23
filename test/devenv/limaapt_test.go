// Pins that the VM's provisioning cannot hang on a stalled Ubuntu mirror (issue #498). On 2026-11-23 the first
// system step's `apt-get update` sat on a trickling archive.ubuntu.com for 14 minutes, nothing after it ran, and
// the only symptom was Lima's generic 600-second probe timeout. Two things fix that, and this file pins both:
//
//   - apt runs with a transfer timeout and a retry count, from ONE drop-in under /etc/apt/apt.conf.d/ that is
//     written before the first apt-get call, so a mirror that trickles or stops fails provisioning with apt's
//     own error in cloud-init-output.log instead of blocking for ever. No apt-get carries the options itself:
//     one drop-in covers update, install and remove in every step, and Lima runs them in this order.
//   - the template's ubuntuMirror param (default http://archive.ubuntu.com/ubuntu, today's behaviour) is read by
//     provisioning as PARAM_ubuntuMirror - Lima refuses a param no script names - and rewrites only the
//     archive.ubuntu.com URIs of /etc/apt/sources.list.d/ubuntu.sources before the first `apt-get update`;
//     security.ubuntu.com stays, so a mirror the maintainer picks for speed never takes the security updates
//     with it. make vm-up passes it to limactl from UBUNTU_MIRROR, validated like the VM_* values of issue #228.
//
// The rewrite's behaviour is pinned by running the step in limaaptstep_test.go, which needs the step to prefix the
// files it writes with SHIFT2EU_APT_ROOT, as the swap step of issue #294 does with SHIFT2EU_SWAP_ROOT: this file
// reads the template, that one runs the step against a fake guest and pins what it writes there.
package devenv_test

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// ubuntuMirrorVar is the make variable that picks the mirror: make vm-up UBUNTU_MIRROR=<url> (issue #498).
	ubuntuMirrorVar = "UBUNTU_MIRROR"
	// ubuntuMirrorParam is the template's param that carries the mirror into provisioning, which Lima hands the
	// scripts as PARAM_ubuntuMirror.
	ubuntuMirrorParam = "ubuntuMirror"
	// ubuntuMirrorEnv is how Lima passes the param's value to a provision script.
	ubuntuMirrorEnv = "PARAM_" + ubuntuMirrorParam
	// ubuntuMirrorDefault is the param's value in dev/lima.yaml: the archive cloud-init already writes, so a VM
	// created without UBUNTU_MIRROR fetches from where it does today.
	ubuntuMirrorDefault = "http://archive.ubuntu.com/ubuntu"
	// ubuntuSourcesPath is the file cloud-init fills with the Ubuntu archive: the rewrite's only target.
	ubuntuSourcesPath = "/etc/apt/sources.list.d/ubuntu.sources"
	// ubuntuArchiveHost is the host the rewrite changes to the mirror.
	ubuntuArchiveHost = "archive.ubuntu.com"
	// ubuntuSecurityHost is the host the rewrite must leave alone.
	ubuntuSecurityHost = "security.ubuntu.com"
	// aptTimeoutsDropin is the one apt drop-in that carries the timeouts and retries. apt.conf.d reads every
	// file whose name holds no period, in order: the same "80" slot the ecosystem uses for a site override, and
	// a name of this project's own (dev/lima.yaml's sysctl drop-in is 99-shift2eu-swap.conf, which sysctl orders
	// by prefix; apt has no such convention, so this is the whole name).
	aptTimeoutsDropin = "/etc/apt/apt.conf.d/80shift2eu-timeouts"
)

// aptAcquireOptions are the options the drop-in must set: a transfer timeout for each protocol apt fetches over,
// so a connection that trickles or stops fails instead of waiting for ever, and a retry count, so one bad mirror
// is retried rather than failing the whole provisioning at the first stall.
var aptAcquireOptions = []string{"http::Timeout", "https::Timeout", "Retries"}

// aptAcquireOption matches the value of the Acquire:: option name as an apt drop-in writes it, in either syntax:
// `Acquire::http::Timeout "30";` of apt.conf, or the `=30` apt-get takes with -o.
func aptAcquireOption(name string) *regexp.Regexp {
	return regexp.MustCompile(`Acquire::` + regexp.QuoteMeta(name) + `[ \t]*[=]?[ \t]*"?[ \t]*([0-9]+)`)
}

// provisionScript is one entry of dev/lima.yaml's provision: list: the mode of the entry and its script.
type provisionScript struct {
	mode   string   // the entry's mode line, empty for an entry that names none
	header int      // the script header's line number in the template, for a failure message
	script []string // the script, dedented as the YAML block scalar dedents it
}

// text is the script as one string, which is what the shell reads.
func (p provisionScript) text() string {
	return strings.Join(p.script, "\n")
}

// provisionScripts returns every provision script of dev/lima.yaml, in the order Lima runs them. The probe
// scripts that follow probes: are not provisioning, and the drop-ins and the mirror rewrite belong to a step that
// runs before the first apt-get call of the VM's life.
func provisionScripts(t *testing.T, template string) []provisionScript {
	t.Helper()

	lines := strings.Split(template, "\n")

	start := slices.Index(lines, "provision:")
	if start < 0 {
		t.Fatalf("%s has no top-level provision: block, so nothing is installed in the VM", limaTemplateRepoPath)
	}

	end := len(lines)

	if probes := slices.Index(lines, "probes:"); probes > start {
		end = probes
	}

	var scripts []provisionScript

	for i := start + 1; i < end; i++ {
		if !limaScriptHeader.MatchString(lines[i]) {
			continue
		}

		scripts = append(scripts, provisionScript{
			mode:   provisionModeOf(t, lines, start, i),
			header: i + 1,
			script: limaBlockContent(t, lines, i),
		})
	}

	if len(scripts) == 0 {
		t.Fatalf("%s's provision: block holds no `script: |` entry", limaTemplateRepoPath)
	}

	return scripts
}

// provisionModeOf returns the mode of the provision entry whose script header is lines[header]: the mode line of
// the nearest `- ` list item above it, empty for an entry that names none.
func provisionModeOf(t *testing.T, lines []string, provisionStart, header int) string {
	t.Helper()

	for i := header - 1; i > provisionStart; i-- {
		if !strings.HasPrefix(lines[i], "  - ") {
			continue
		}

		for j := i; j < header; j++ {
			if match := limaProvisionMode.FindStringSubmatch(lines[j]); match != nil {
				return match[1]
			}
		}

		return ""
	}

	t.Fatalf("%s line %d is a provision script that is not an item of the provision: list", limaTemplateRepoPath, header+1)

	return ""
}

// provisionLine is one line of dev/lima.yaml's provisioning, with its place in the order Lima runs the scripts:
// a step that runs earlier writes everything it writes before anything the later steps do.
type provisionLine struct {
	step int    // the index into provisionScripts
	line int    // the index into that script
	text string // the line, comments and all
}

// provisionLines returns every line of every provision script, in the order Lima runs them.
func provisionLines(scripts []provisionScript) []provisionLine {
	var lines []provisionLine

	for step, p := range scripts {
		for line, text := range p.script {
			lines = append(lines, provisionLine{step: step, line: line, text: text})
		}
	}

	return lines
}

// aptGetCall returns the words of the first apt-get command on a shell line - apt-get and its arguments - and nil
// for a line that runs none. Comments are dropped, so a comment naming apt-get is not a call.
func aptGetCall(line string) []string {
	for _, words := range commandWords(line) {
		if at := slices.Index(words, "apt-get"); at >= 0 {
			return words[at:]
		}
	}

	return nil
}

// aptGetCalls returns every apt-get command of the provisioning, in the order Lima runs them, with the line it is
// on.
func aptGetCalls(scripts []provisionScript) []provisionLine {
	var calls []provisionLine

	for _, line := range provisionLines(scripts) {
		if aptGetCall(line.text) != nil {
			calls = append(calls, line)
		}
	}

	return calls
}

// firstLine returns the place of the first line of the provisioning, in the order Lima runs them, whose text
// holds substr on a line of code, and whether there is one. Comments are dropped, so a comment mentioning a path
// or a host never passes for the code that writes or rewrites it.
func firstLine(scripts []provisionScript, substr string) (provisionLine, bool) {
	for _, line := range provisionLines(scripts) {
		if hash := strings.Index(line.text, "#"); hash >= 0 {
			line.text = line.text[:hash]
		}

		if strings.Contains(line.text, substr) {
			return line, true
		}
	}

	return provisionLine{}, false
}

// before says whether a comes before b in the order Lima runs the provisioning's lines.
func before(a, b provisionLine) bool {
	if a.step != b.step {
		return a.step < b.step
	}

	return a.line < b.line
}

// describePlace renders one provisioning line for a failure message: its step, its line in that script, and the
// line itself.
func describePlace(scripts []provisionScript, place provisionLine) string {
	return "step " + strconv.Itoa(place.step+1) + ", line " + strconv.Itoa(place.line+1) + ": " + strings.TrimSpace(place.text) +
		" (dev/lima.yaml line " + strconv.Itoa(scripts[place.step].header+place.line+1) + ")"
}

// withoutComments returns a script with every shell comment removed, so a comment that says security.ubuntu.com
// stays does not read as code that names it.
func withoutComments(text string) string {
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if hash := strings.Index(line, "#"); hash >= 0 {
			lines[i] = line[:hash]
		}
	}

	return strings.Join(lines, "\n")
}

// limaParam returns the value of the template's param name, without its quotes.
func limaParam(t *testing.T, template, name string) string {
	t.Helper()

	lines := strings.Split(template, "\n")

	start := slices.Index(lines, "param:")
	if start < 0 {
		t.Fatalf("%s has no top-level param: block, where the %s param belongs", limaTemplateRepoPath, name)
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
		if match == nil || match[1] != name {
			continue
		}

		values = append(values, strings.TrimSpace(match[2]))
	}

	switch len(values) {
	case 0:
		t.Fatalf("%s's param: block has no %s: the VM's Ubuntu mirror is a param, so it can be changed from make without editing the template (issue #498)",
			limaTemplateRepoPath, name)
	case 1:
		return values[0]
	default:
		t.Fatalf("%s's param: block has %d %s entries, want exactly one", limaTemplateRepoPath, len(values), name)
	}

	return ""
}

// TestLimaProvisioningRunsAPTWithTimeoutsAndRetries pins issue #498's first gap: apt has no stall timeout of its
// own, so one mirror that trickles or stops blocked the VM's provisioning for ever, and Lima's 600-second probe
// timeout was the only symptom. One drop-in under /etc/apt/apt.conf.d/ sets the timeouts and the retries for
// every apt-get call of every step, written by system provisioning because /etc/apt is root's.
func TestLimaProvisioningRunsAPTWithTimeoutsAndRetries(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)
	scripts := provisionScripts(t, template)
	calls := aptGetCalls(scripts)

	// The drop-in's step, and the line that writes it.
	var (
		writing    []provisionLine
		dropinStep = -1
	)

	for _, line := range provisionLines(scripts) {
		if hash := strings.Index(line.text, "#"); hash >= 0 {
			line.text = line.text[:hash]
		}

		if !strings.Contains(line.text, aptTimeoutsDropin) {
			continue
		}

		writing = append(writing, line)

		if dropinStep < 0 {
			dropinStep = line.step
		}
	}

	t.Run("one-system-step-writes-the-drop-in", func(t *testing.T) {
		t.Parallel()

		if len(calls) == 0 {
			t.Fatalf("%s's provisioning runs no apt-get at all, so this test pins nothing: the system step installs the VM's packages with apt-get (issue #498)", limaTemplateRepoPath)
		}

		if len(writing) == 0 {
			t.Fatalf("%s never names %s, so nothing sets apt's timeouts and retries: a mirror that trickles or stops keeps the first apt-get call blocked until Lima's 600-second probe timeout gives up, and the only symptom is the generic 'User probe' failure of issue #498",
				limaTemplateRepoPath, aptTimeoutsDropin)
		}

		steps := map[int]bool{}
		for _, line := range writing {
			steps[line.step] = true
		}

		if len(steps) != 1 {
			t.Fatalf("%s writes %s from %d provision steps, want exactly one: one drop-in is what covers update, install and remove in every step (issue #498)", limaTemplateRepoPath, aptTimeoutsDropin, len(steps))
		}

		if mode := scripts[dropinStep].mode; mode != "system" {
			t.Errorf("%s writes %s in a provision step whose mode is %q, want system: /etc/apt/apt.conf.d is root's, so a user step cannot write the drop-in at all (issue #498)",
				limaTemplateRepoPath, aptTimeoutsDropin, mode)
		}
	})

	t.Run("the-drop-in-sets-a-timeout-and-retries", func(t *testing.T) {
		t.Parallel()

		if dropinStep < 0 {
			t.Fatalf("%s never names %s, so there is no drop-in whose options this pins (issue #498)", limaTemplateRepoPath, aptTimeoutsDropin)
		}

		text := scripts[dropinStep].text()

		for _, name := range aptAcquireOptions {
			match := aptAcquireOption(name).FindStringSubmatch(text)
			if match == nil {
				t.Errorf("%s's drop-in %s names no Acquire::%s: apt then waits on a mirror that trickles or stops for ever, and provisioning hangs until Lima's probe times out (issue #498)",
					limaTemplateRepoPath, aptTimeoutsDropin, name)

				continue
			}

			value, err := strconv.Atoi(match[1])
			if err != nil || value <= 0 {
				t.Errorf("%s's drop-in %s sets Acquire::%s to %q, want a positive whole number such as 30 seconds for a timeout and 5 for Retries (issue #498)",
					limaTemplateRepoPath, aptTimeoutsDropin, name, match[1])
			}
		}
	})

	t.Run("no-apt-get-carries-the-options-itself", func(t *testing.T) {
		t.Parallel()

		for _, line := range calls {
			if strings.Contains(withoutComments(line.text), "Acquire::") {
				t.Errorf("%s runs %q with an Acquire:: option of its own: the timeouts and retries come from the one drop-in %s, which covers every apt-get call, update, install and remove alike (issue #498)",
					limaTemplateRepoPath, strings.TrimSpace(line.text), aptTimeoutsDropin)
			}
		}
	})
}

// TestLimaProvisioningWritesTheAPTTimeoutsBeforeAnyAPTGet pins issue #498's ordering: the drop-in is written
// before the first apt-get call of the VM's life, across the provision entries in the order Lima runs them, so no
// apt-get runs without it - and a later entry, or a later apt-get in the same step, is not enough.
func TestLimaProvisioningWritesTheAPTTimeoutsBeforeAnyAPTGet(t *testing.T) {
	t.Parallel()

	scripts := provisionScripts(t, readRepoFile(t, limaTemplateRepoPath))

	dropin, ok := firstLine(scripts, aptTimeoutsDropin)
	if !ok {
		t.Fatalf("%s never names %s, so the VM's apt-get calls run without a timeout or retries: one stalled Ubuntu mirror blocks provisioning until Lima's 600-second probe timeout gives up (issue #498)",
			limaTemplateRepoPath, aptTimeoutsDropin)
	}

	calls := aptGetCalls(scripts)
	if len(calls) == 0 {
		t.Fatalf("%s's provisioning runs no apt-get at all, so this ordering pins nothing (issue #498)", limaTemplateRepoPath)
	}

	for _, call := range calls {
		if !before(dropin, call) {
			t.Errorf("%s's %s, at %s, comes before the drop-in of %s, at %s: that apt-get call runs with apt's own timeouts, which are none, so a mirror that trickles or stops blocks provisioning until Lima gives up (issue #498)",
				limaTemplateRepoPath, strings.Join(aptGetCall(call.text), " "), describePlace(scripts, call), aptTimeoutsDropin, describePlace(scripts, dropin))
		}
	}
}

// TestLimaTemplateHasAUbuntuMirrorParam pins issue #498's second gap: the mirror was fixed, cloud-init wrote
// archive.ubuntu.com into ubuntu.sources, and the only way to pick another mirror was to edit that file inside
// the half-provisioned VM by hand. The template's ubuntuMirror param is that mirror, read raw as
// PARAM_ubuntuMirror - Lima refuses a param no script names - and the step that reads it rewrites only the
// archive.ubuntu.com URIs, leaving security.ubuntu.com to the security updates.
func TestLimaTemplateHasAUbuntuMirrorParam(t *testing.T) {
	t.Parallel()

	template := readRepoFile(t, limaTemplateRepoPath)
	scripts := provisionScripts(t, template)

	t.Run("the-param-defaults-to-todays-archive", func(t *testing.T) {
		t.Parallel()

		if got := limaParam(t, template, ubuntuMirrorParam); got != ubuntuMirrorDefault {
			t.Errorf("%s's %s param is %q, want %q: a VM created without UBUNTU_MIRROR must fetch from where cloud-init points it today, so the param changes nothing until a maintainer sets it (issue #498)",
				limaTemplateRepoPath, ubuntuMirrorParam, got, ubuntuMirrorDefault)
		}
	})

	// Lima refuses a param no script names, so exactly one step reads it: two would rewrite the sources twice.
	var reading []provisionScript

	for _, p := range scripts {
		if slices.ContainsFunc(p.script, func(line string) bool { return strings.Contains(line, ubuntuMirrorEnv) }) {
			reading = append(reading, p)
		}
	}

	if len(reading) != 1 {
		t.Fatalf("%s has %d provision scripts that name %s, want exactly one: Lima refuses a param no script names, and the mirror is rewritten once (issue #498)",
			limaTemplateRepoPath, len(reading), ubuntuMirrorEnv)
	}

	step := reading[0]
	code := withoutComments(step.text())

	t.Run("the-step-rewrites-only-the-archive-uris", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(code, ubuntuSourcesPath) {
			t.Errorf("%s's step that reads %s never names %s: that is the file cloud-init fills with the Ubuntu archive, and the one the mirror rewrites (issue #498)",
				limaTemplateRepoPath, ubuntuMirrorEnv, ubuntuSourcesPath)
		}

		if !strings.Contains(code, ubuntuArchiveHost) {
			t.Errorf("%s's step that reads %s never names %s: it must rewrite the archive URIs to the mirror, and nothing else (issue #498)",
				limaTemplateRepoPath, ubuntuMirrorEnv, ubuntuArchiveHost)
		}

		if strings.Contains(code, ubuntuSecurityHost) {
			t.Errorf("%s's step that reads %s names %s: the rewrite must leave the security URIs alone, so a mirror a maintainer picked for speed never carries the security updates (issue #498)",
				limaTemplateRepoPath, ubuntuMirrorEnv, ubuntuSecurityHost)
		}
	})

	t.Run("the-mirror-is-what-the-archive-uris-become", func(t *testing.T) {
		t.Parallel()

		for _, line := range step.script {
			if hash := strings.Index(line, "#"); hash >= 0 {
				line = line[:hash]
			}

			if strings.Contains(line, ubuntuArchiveHost) && strings.Contains(line, ubuntuMirrorEnv) {
				return
			}
		}

		t.Errorf("%s's step that reads %s has no line of code that names both %s and %s, so nothing shows the archive URIs become the mirror: the rewrite replaced them with something else (issue #498)",
			limaTemplateRepoPath, ubuntuMirrorEnv, ubuntuArchiveHost, ubuntuMirrorEnv)
	})
}

// TestLimaProvisioningRewritesTheMirrorBeforeTheFirstAPTGetUpdate pins issue #498's ordering for the mirror: the
// rewrite comes before the first `apt-get update`, so that call already fetches from the mirror and a maintainer
// whose archive.ubuntu.com is slow does not have to stop the VM, edit ubuntu.sources and create it again.
func TestLimaProvisioningRewritesTheMirrorBeforeTheFirstAPTGetUpdate(t *testing.T) {
	t.Parallel()

	scripts := provisionScripts(t, readRepoFile(t, limaTemplateRepoPath))

	mirror, ok := firstLine(scripts, ubuntuMirrorEnv)
	if !ok {
		t.Fatalf("%s never names %s, so make vm-create UBUNTU_MIRROR has nothing to change: the VM fetches from cloud-init's archive.ubuntu.com whatever the param says (issue #498)",
			limaTemplateRepoPath, ubuntuMirrorEnv)
	}

	var updates []provisionLine

	for _, call := range aptGetCalls(scripts) {
		if len(call.text) > 0 && strings.Contains(strings.Join(aptGetCall(call.text), " "), "apt-get update") {
			updates = append(updates, call)
		}
	}

	if len(updates) == 0 {
		t.Fatalf("%s's provisioning has no `apt-get update`, so this ordering pins nothing (issue #498)", limaTemplateRepoPath)
	}

	if !before(mirror, updates[0]) {
		t.Errorf("%s's first `apt-get update`, at %s, comes before the mirror rewrite of %s, at %s: that update still fetches from cloud-init's archive.ubuntu.com, so UBUNTU_MIRROR changes nothing for the call that matters (issue #498)",
			limaTemplateRepoPath, describePlace(scripts, updates[0]), ubuntuMirrorEnv, describePlace(scripts, mirror))
	}
}

// withoutMirrorArg returns the creation argv without the one argument that carries the ubuntuMirror param, and
// that argument: the flag's spelling is limactl's business, the mirror reaching limactl is this test's.
func withoutMirrorArg(t *testing.T, argv []string, why string) ([]string, string) {
	t.Helper()

	var (
		rest  []string
		found []string
	)

	for _, arg := range argv {
		if strings.Contains(arg, ubuntuMirrorParam) {
			found = append(found, arg)

			continue
		}

		rest = append(rest, arg)
	}

	if len(found) != 1 {
		t.Fatalf("%s: the creation argv holds %d argument(s) naming the %s param, want exactly one, so limactl creates the VM from dev/lima.yaml's default mirror instead of the one the maintainer named (issue #498); the argv was:%s",
			why, len(found), ubuntuMirrorParam, describeCalls([][]string{argv}))
	}

	return rest, found[0]
}

// TestMakeVMUpPassesTheUbuntuMirrorToLimactlStart pins issue #498's make half: UBUNTU_MIRROR travels to the
// ubuntuMirror param of dev/lima.yaml, read raw like the VM_* values of issue #228, and without it the creation
// command line is unchanged - dev/lima.yaml's default is today's archive, so a VM created without it fetches
// from where it does today.
func TestMakeVMUpPassesTheUbuntuMirrorToLimactlStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mirror string            // UBUNTU_MIRROR on the make command line, empty when it is left unset
		env    map[string]string // UBUNTU_MIRROR set in the environment instead of on the command line
	}{
		{name: "unset"},
		{
			// The workaround of issue #498, as a flag: the country mirror that answered when the primary did not.
			name:   "a country mirror",
			mirror: "http://nl.archive.ubuntu.com/ubuntu",
		},
		{name: "an https mirror", mirror: "https://mirrors.edge.kernel.org/ubuntu"},
		{name: "a mirrored release", mirror: "https://mirrors.edge.kernel.org/ubuntu-ports"},
		{name: "a mirror with a port", mirror: "https://mirror.example.com:8443/ubuntu"},
		{name: "a mirror whose path holds a dash and an underscore", mirror: "http://ftp.halifax.rwth-aachen.de/ubuntu_mirror"},
		{name: "a mirror whose path holds a tilde and dots", mirror: "https://mirror.example.com/~shift2eu/ubuntu.24.04"},
		{name: "a trailing slash", mirror: "http://nl.archive.ubuntu.com/ubuntu/"},
		{name: "in the environment", env: map[string]string{ubuntuMirrorVar: "http://de.archive.ubuntu.com/ubuntu"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			for name, value := range tc.env {
				f.env[name] = value
			}

			args := []string{"vm-up"}
			setting := "with " + ubuntuMirrorVar + " unset"
			mirror := tc.mirror

			if mirror != "" {
				args = append(args, ubuntuMirrorVar+"="+mirror)
				setting = ubuntuMirrorVar + "=" + strconv.Quote(mirror) + " on the command line"
			}

			for name, value := range tc.env {
				setting = name + "=" + strconv.Quote(value) + " in the environment"
				mirror = value
			}

			stdout, stderr, err := f.run(t, args...)
			if err != nil {
				t.Fatalf("make vm-up %s: %v\nstdout: %s\nstderr: %s", setting, err, stdout, stderr)
			}

			got := f.creating(t)

			if mirror == "" {
				if diff := cmp.Diff(f.creationArgs(), got); diff != "" {
					t.Errorf("make vm-up %s creates the VM with another command line than before UBUNTU_MIRROR existed: without it dev/lima.yaml's %s param keeps its default, %s, so the flags of the VM_* values of issue #228 are the only ones added (-want +got):\n%s",
						setting, ubuntuMirrorParam, ubuntuMirrorDefault, diff)
				}

				return
			}

			rest, arg := withoutMirrorArg(t, got, "make vm-up "+setting)

			if !strings.Contains(arg, mirror) {
				t.Errorf("make vm-up %s hands limactl %q, which does not carry the mirror: limactl then creates the VM from dev/lima.yaml's default mirror, and the VM fetches where the maintainer did not ask (issue #498)",
					setting, arg)
			}

			if diff := cmp.Diff(f.creationArgs(), rest); diff != "" {
				t.Errorf("make vm-up %s changes the creation command line beyond the mirror: only the %s param is added, the VM_* flags of issue #228 and the template last stay as they are (-want +got):\n%s",
					setting, ubuntuMirrorParam, diff)
			}
		})
	}
}

// TestMakeVMCreatePassesTheUbuntuMirrorToVMUp pins issue #498's one-command acceptance: make vm-create
// UBUNTU_MIRROR=<mirror> hands the mirror to its make vm-up, so the VM is created with it rather than with the
// template's default.
func TestMakeVMCreatePassesTheUbuntuMirrorToVMUp(t *testing.T) {
	t.Parallel()

	f := newVMUpFixture(t, "", "")

	mirror := "http://nl.archive.ubuntu.com/ubuntu"

	stdout, stderr, err := f.run(t, "vm-create", ubuntuMirrorVar+"="+mirror)
	if err != nil {
		t.Fatalf("make vm-create %s=%s: %v\nstdout: %s\nstderr: %s", ubuntuMirrorVar, mirror, err, stdout, stderr)
	}

	_, arg := withoutMirrorArg(t, f.creating(t), "make vm-create "+ubuntuMirrorVar+"="+mirror)

	if !strings.Contains(arg, mirror) {
		t.Errorf("make vm-create %s=%s does not hand the mirror to its make vm-up: the VM is created from dev/lima.yaml's default mirror, %s, and a maintainer whose archive.ubuntu.com is slow still has to create it again (issue #498)",
			ubuntuMirrorVar, mirror, ubuntuMirrorDefault)
	}
}
