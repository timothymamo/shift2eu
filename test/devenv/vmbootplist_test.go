// Pins the LaunchDaemon that starts the Lima VM at boot (issue #398): `sh dev/vm-boot-launchd.sh plist` prints
// the property list launchd loads so the instance comes back after a reboot or a power cut, with nobody logged
// in, without ever enabling Lima's own autostart job - `limactl autostart enable` writes a job that runs
// `limactl start <name> --foreground`, the form that can fall back to template:default, a VM that mounts the
// home directory (issue #237). The job therefore runs `limactl shell --start --workdir / <instance> true`, the
// command line make vm-up starts an existing instance with since #254: it starts a Stopped instance, only runs
// the command on a Running one, and fails with `does not exist` for an instance that has gone, so it can never
// create a VM.
//
// The job runs as the account that renders the plist (UserName, from id -un), never as root, with a fixed
// environment (/usr/bin/env -i, HOME, a PATH that finds limactl, LIMA_INSTANCE) and no credential of any kind:
// the VM needs none to boot, and the agents App's refresh job delivers the token (ADR 0034). It runs once at
// load (RunAtLoad, no KeepAlive) and logs to ~/.local/state/shift2eu/vm-boot-<instance>.log.
//
// A value the plist cannot carry, an instance name that cannot be part of a label, and a run as root are
// refused before anything is printed, so make never installs half a plist and never installs a job that runs as
// root. Every run goes through the sandbox's fakes (sandbox_test.go): the test process's own environment never
// reaches the generator, and rendering a plist reaches no limactl, no launchctl and no sudo.
package devenv_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// vmBootScript is the plist generator issue #398 adds, next to the Makefile targets that install and remove the
// job it renders.
var vmBootScript = filepath.Join("dev", "vm-boot-launchd.sh")

// vmBootLabelPrefix is the boot job's label prefix: one job per VM instance, in the project's own namespace, so
// two VMs never share one.
const vmBootLabelPrefix = "io.github.timothymamo.shift2eu.vm-boot."

// The labels Lima's own autostart jobs carry (issues #398 and #315): io.lima-vm.autostart.<instance> in the
// user's domain with `limactl autostart enable`, io.lima-vm.daemon.<instance> in the system domain with
// --condition=boot. Both run `limactl start <name> --foreground`, the form that can fall back to
// template:default (issue #237), so neither may exist beside the project's own boot job.
const (
	vmBootLimaAutostartLabelPrefix = "io.lima-vm.autostart."
	vmBootLimaDaemonLabelPrefix    = "io.lima-vm.daemon."
)

// vmBootPlistFixture is one controlled environment for a run of dev/vm-boot-launchd.sh: a sandbox with a
// temporary HOME and a bin directory first on PATH that holds the fake limactl, the four variables the boot job
// needs, and obviously fake values for the three App variables the job must never carry (CLAUDE.md rule 3), so a
// generator that copied one into the plist fails here instead of on the maintainer's Mac.
type vmBootPlistFixture struct {
	sb       *sandbox
	env      map[string]string
	instance string
}

// newVMBootPlistFixture builds the fixture for one VM instance.
func newVMBootPlistFixture(t *testing.T, instance string) *vmBootPlistFixture {
	t.Helper()

	sb := newSandbox(t)

	env := sb.baseEnv()
	env["LIMA_INSTANCE"] = instance

	// The App's variables are no business of the boot job: the VM needs no credential to boot, and the refresh
	// job delivers the token (ADR 0034). They are set here, with obviously fake values, so a generator that
	// handed them on would put them in the plist and fail this test's scan for them.
	env["SHIFT2EU_AGENTS_APP_ID"] = "fake-app-id"
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = "fake-installation-id"
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = filepath.Join(sb.root, "invented", "agents-app.pem")

	// The documented log path is $HOME/.local/state/shift2eu/vm-boot-<instance>.log, the fallback
	// ${XDG_STATE_HOME:-$HOME/.local/state} names: leaving XDG_STATE_HOME unset pins that path either way.
	delete(env, "XDG_STATE_HOME")

	return &vmBootPlistFixture{sb: sb, env: env, instance: instance}
}

// plist runs `sh dev/vm-boot-launchd.sh plist` under the fixture's environment alone and returns its stdout, its
// stderr and its status.
func (f *vmBootPlistFixture) plist(t *testing.T) (string, string, error) {
	t.Helper()

	return f.sb.run(t, scriptPath(t, vmBootScript), []string{"plist"}, f.env, "")
}

// logPath is the log the job writes, as issue #398 documents it.
func (f *vmBootPlistFixture) logPath() string {
	return filepath.Join(f.env["HOME"], ".local", "state", "shift2eu", "vm-boot-"+f.instance+".log")
}

// TestVMBootLaunchdPlistDescribesTheJob pins the whole of the generated boot job: its label per instance, the
// maintainer it runs as, the environment it starts the VM with, the limactl command line that starts an existing
// instance and never creates one, the log path, and that it runs once (RunAtLoad, no KeepAlive).
func TestVMBootLaunchdPlistDescribesTheJob(t *testing.T) {
	t.Parallel()

	for _, instance := range []string{"shift2eu", "other-repo", "shift2eu.local_1"} {
		t.Run(instance, func(t *testing.T) {
			t.Parallel()

			f := newVMBootPlistFixture(t, instance)

			stdout, stderr, err := f.plist(t)
			if err != nil {
				t.Fatalf("sh %s plist exited %d with LIMA_INSTANCE=%q\nstdout: %s\nstderr: %s",
					vmBootScript, exitStatusOf(t, err), instance, stdout, stderr)
			}

			dict := parsePlist(t, stdout)

			wantLabel := vmBootLabelPrefix + instance
			if got := plistString(t, dict, "Label"); got != wantLabel {
				t.Errorf("the plist's Label is %q, want %q: one boot job per VM instance, in the project's own namespace, so two VMs never share one (issue #398)", got, wantLabel)
			}

			if got, want := plistString(t, dict, "UserName"), currentUsername(t); got != want {
				t.Errorf("the plist's UserName is %q, want %q: the job starts the maintainer's VM as the maintainer, at boot and with nobody logged in (issue #398)", got, want)
			}

			args := plistStrings(t, plistList(t, dict, "ProgramArguments"))

			switch {
			case len(args) < 2:
				t.Fatalf("the plist's ProgramArguments hold %d argument(s), want at least /usr/bin/env -i and the limactl command line: a launchd job inherits no environment, so the job runs a fixed one (issue #398)\nstdout:\n%s", len(args), stdout)
			case args[0] != "/usr/bin/env" || args[1] != "-i":
				t.Errorf("the plist's ProgramArguments start with %q, want /usr/bin/env -i: a launchd job inherits no shell environment, so the plist supplies exactly the one the job needs (issue #398)\nstdout:\n%s", args[:2], stdout)
			}

			for _, want := range []string{"HOME=" + f.env["HOME"], "LIMA_INSTANCE=" + instance} {
				if !slices.Contains(args, want) {
					t.Errorf("the plist's ProgramArguments hold no %q, so the job starts without it: /usr/bin/env -i hands the job these variables and nothing else (issue #398)\nstdout:\n%s", want, stdout)
				}
			}

			// limactl comes from Homebrew, which a launchd job's default PATH does not hold: without its
			// directory the job could not run the command it exists for.
			jobPath := ""
			for _, arg := range args {
				if path, ok := strings.CutPrefix(arg, "PATH="); ok {
					jobPath = path
				}
			}

			if jobPath == "" {
				t.Errorf("the plist's ProgramArguments hold no PATH=, so the job runs with launchd's own and finds no limactl (issue #398)\nstdout:\n%s", stdout)
			} else {
				for _, want := range []string{filepath.Dir(f.env["LIMACTL"]), "/usr/bin"} {
					if !strings.Contains(jobPath, want) {
						t.Errorf("the plist's PATH is %q and holds no %s: limactl comes from Homebrew, and the plist's PATH is the one the job has (issue #398)\nstdout:\n%s", jobPath, want, stdout)
					}
				}
			}

			joined := strings.Join(args, " ")

			// The one command line that starts an existing instance without ever creating one (issue #254).
			command := "limactl shell --start --workdir / " + instance + " true"
			if !strings.Contains(joined, command) {
				t.Errorf("the plist's ProgramArguments hold no %q: the job starts the existing instance the way make vm-up does, which fails for an instance that is gone and never creates one (issues #254, #398)\nstdout:\n%s", command, stdout)
			}

			if strings.Contains(joined, "limactl start") {
				t.Errorf("the plist's ProgramArguments hold `limactl start`: limactl start <name> creates a VM from template:default when the instance has gone, a VM that mounts the home directory (issues #237, #398)\nstdout:\n%s", stdout)
			}

			// The log lives under the maintainer's home, beside the refresh's, never inside ~/.lima, where a
			// log launchd creates would bring a deleted instance's directory back (issue #229's note from #237).
			logged := strings.Contains(joined, f.logPath())
			for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
				if value, ok := dict[key]; ok && value.kind == "string" && value.text == f.logPath() {
					logged = true
				}
			}

			if !logged {
				t.Errorf("the plist names no log %q on its command line or in StandardOutPath/StandardErrorPath, so a boot that failed leaves nothing to read (issue #398)\nstdout:\n%s", f.logPath(), stdout)
			}

			if kind := plistKind(t, dict, "RunAtLoad"); kind != "true" {
				t.Errorf("the plist's RunAtLoad is a <%s>, want <true>: the job exists to start the VM at boot (issue #398)\nstdout:\n%s", kind, stdout)
			}

			// A stopped job is restarted by KeepAlive, and the boot job starts the VM once: it must not run
			// again every time it exits, as the refresh's job does (issue #229).
			if _, ok := dict["KeepAlive"]; ok {
				t.Errorf("the plist has a KeepAlive key: the boot job runs once at load and exits, and KeepAlive would restart it whenever it ends (issue #398)\nstdout:\n%s", stdout)
			}

			// The job carries no credential of any kind (ADR 0034, issue #398): the VM boots without one.
			for _, forbidden := range []string{"SHIFT2EU_AGENTS_APP_", "TOKEN", ".pem", "GH_", "GITHUB_"} {
				for _, stream := range []string{stdout, stderr} {
					if strings.Contains(stream, forbidden) {
						t.Errorf("the plist generator's output holds %q: the boot job runs with no credential, and the refresh job delivers the token (ADR 0034, issue #398)\n%s", forbidden, stream)
					}
				}
			}

			// Rendering a plist reaches neither the VM nor launchd.
			for _, tool := range []string{"limactl", "launchctl", "sudo"} {
				if calls := invocations(f.sb.argvLogText(t), tool); len(calls) != 0 {
					t.Errorf("rendering the plist ran %s (%v): printing a plist starts no VM and loads no job (issue #398)", tool, calls)
				}
			}
		})
	}
}

// TestVMBootLaunchdPlistOpensTheLogTheJobWrites pins how the job opens its own log (issue #398, ADR 0034): launchd
// may open StandardOutPath and StandardErrorPath as root, before UserName applies, in a directory the maintainer
// writes, so both name /dev/null and the job opens ${XDG_STATE_HOME:-$HOME/.local/state}/shift2eu/vm-boot-
// <instance>.log itself, as the maintainer, through a shell line. The log and every word of the limactl command
// line are arguments of /bin/sh -c of their own - $0 is the log, "$@" the command - so no path ever becomes shell
// text, and the line appends both streams to $0 and execs the command, so the job's exit status is limactl's and
// every line limactl writes, stderr included, lands in the log. limactl itself is named by the absolute path the
// generator found: a launchd job starts from no directory of its own, so no relative name can be resolved.
func TestVMBootLaunchdPlistOpensTheLogTheJobWrites(t *testing.T) {
	t.Parallel()

	f := newVMBootPlistFixture(t, "shift2eu")

	stdout, stderr, err := f.plist(t)
	if err != nil {
		t.Fatalf("sh %s plist exited %d\nstdout: %s\nstderr: %s", vmBootScript, exitStatusOf(t, err), stdout, stderr)
	}

	dict := parsePlist(t, stdout)
	args := plistStrings(t, plistList(t, dict, "ProgramArguments"))

	// The shell line is the argument after /bin/sh -c, and the arguments after it are $0 and the command.
	line, at := "", -1

	for i, arg := range args {
		if arg == "/bin/sh" && i+1 < len(args) && args[i+1] == "-c" {
			if i+2 >= len(args) {
				t.Fatalf("the plist runs /bin/sh -c with no line to run (issue #398)\nstdout:\n%s", stdout)
			}

			line, at = args[i+2], i+2
		}
	}

	if at < 0 {
		t.Fatalf("the plist runs no /bin/sh -c line, so the job cannot open its own log as the maintainer (issue #398, ADR 0034)\nstdout:\n%s", stdout)
	}

	// Both streams are appended to the log, and exec hands the job's exit status over to limactl.
	if want := `exec "$@" >>"$0" 2>&1`; !strings.Contains(line, want) {
		t.Errorf("the job's shell line holds no %q: launchd's own StandardOutPath and StandardErrorPath are /dev/null, so a line that does not append both streams of the command to $0 leaves what limactl printed - a failed boot's message included - nowhere to read (issue #398, ADR 0034)\nline: %q", want, line)
	}

	// The log and the command are arguments, never spliced into the shell text.
	if strings.Contains(line, f.logPath()) {
		t.Errorf("the job's shell line holds the log path %q itself: the log is an argument of the line ($0), so a path with a character the shell reads stays a path (issue #398)\nline: %q", f.logPath(), line)
	}

	if at+1 >= len(args) || args[at+1] != f.logPath() {
		got := ""
		if at+1 < len(args) {
			got = args[at+1]
		}

		t.Errorf("the argument after the job's shell line is %q, want the log %q: it is $0 of the line, the file the job appends both streams to (issue #398)\nstdout:\n%s",
			got, f.logPath(), stdout)

		return
	}

	wantLimactl := f.env["LIMACTL"]

	switch {
	case at+2 >= len(args):
		t.Errorf("the job's shell line has no command after the log: \"$@\" is empty, so the job exits 0 without starting anything (issue #398)\nstdout:\n%s", stdout)
	case args[at+2] != wantLimactl:
		t.Errorf("the job runs %q, want limactl by its absolute path %q: launchd starts the job from no directory of its own, and the PATH it gets is the plist's (issue #398)\nstdout:\n%s", args[at+2], wantLimactl, stdout)
	case !filepath.IsAbs(args[at+2]):
		t.Errorf("the job runs limactl as %q, which is no absolute path (issue #398)\nstdout:\n%s", args[at+2], stdout)
	case at+3 >= len(args) || args[at+3] != "shell":
		t.Errorf("the job runs %q after limactl, want the shell subcommand: \"$@\" must be the command line that starts an existing instance (issue #254, #398)\nstdout:\n%s", args[at+3:], stdout)
	}

	// launchd may open these as root, before UserName applies, in a directory the maintainer writes (ADR 0034).
	for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
		if got := plistString(t, dict, key); got != "/dev/null" {
			t.Errorf("the plist's %s is %q, want /dev/null: launchd may open it as root, before UserName applies, in a directory the maintainer writes, and the job opens its own log as the maintainer instead (ADR 0034, issue #398)\nstdout:\n%s", key, got, stdout)
		}
	}
}

// TestVMBootLaunchdPlistCarriesLimaHomeOnlyWhenSet pins that the job's fixed environment holds LIMA_HOME exactly
// when the maintainer set it (issue #398): Lima keeps an instance under LIMA_HOME when it is set and under ~/.lima
// otherwise, so a job that carried an empty or an unset-but-present LIMA_HOME would look for the VM in the wrong
// directory, and one that dropped a set LIMA_HOME would not find it at all.
func TestVMBootLaunchdPlistCarriesLimaHomeOnlyWhenSet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// set says the case puts LIMA_HOME in the generator's environment.
		set bool
	}{
		{name: "LIMA_HOME is unset"},
		{name: "LIMA_HOME names the directory the instance lives in", set: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMBootPlistFixture(t, "shift2eu")

			limaHome := filepath.Join(f.sb.root, "lima-home")
			delete(f.env, "LIMA_HOME")

			if tc.set {
				f.env["LIMA_HOME"] = limaHome
			}

			stdout, stderr, err := f.plist(t)
			if err != nil {
				t.Fatalf("sh %s plist exited %d with %s\nstdout: %s\nstderr: %s",
					vmBootScript, exitStatusOf(t, err), tc.name, stdout, stderr)
			}

			args := plistStrings(t, plistList(t, parsePlist(t, stdout), "ProgramArguments"))

			var limaHomeArgs []string

			for _, arg := range args {
				if strings.HasPrefix(arg, "LIMA_HOME=") {
					limaHomeArgs = append(limaHomeArgs, arg)
				}
			}

			if !tc.set {
				if len(limaHomeArgs) != 0 {
					t.Errorf("the plist gives the job %v although LIMA_HOME is unset: Lima keeps the instance under $HOME/.lima then, and /usr/bin/env -i hands the job exactly the variables the plist names (issue #398)\nstdout:\n%s",
						limaHomeArgs, stdout)
				}

				return
			}

			want := "LIMA_HOME=" + limaHome
			if diff := cmp.Diff([]string{want}, limaHomeArgs); diff != "" {
				t.Errorf("the plist gives the job the wrong LIMA_HOME arguments (-want +got):\n%s\nthe instance lives under LIMA_HOME when it is set, so the job must be told it, once (issue #398)\nstdout:\n%s", diff, stdout)
			}
		})
	}
}

// TestVMBootLaunchdPlistRefusesBadInput pins the refusals: a missing, empty or malformed LIMA_INSTANCE fails the
// run before it prints anything, and the message names the variable to fix. The instance name becomes part of the
// job's label, so anything a label cannot hold is refused; `limactl shell` is handed the name as an argument, so
// a name with a slash, a space or a metacharacter is refused too.
func TestVMBootLaunchdPlistRefusesBadInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(f *vmBootPlistFixture)
	}{
		{name: "no LIMA_INSTANCE", mutate: func(f *vmBootPlistFixture) { delete(f.env, "LIMA_INSTANCE") }},
		{name: "an empty LIMA_INSTANCE", mutate: func(f *vmBootPlistFixture) { f.env["LIMA_INSTANCE"] = "" }},
		{name: "an instance with a slash", mutate: func(f *vmBootPlistFixture) { f.env["LIMA_INSTANCE"] = "a/b" }},
		{name: "an instance with a space", mutate: func(f *vmBootPlistFixture) { f.env["LIMA_INSTANCE"] = "a b" }},
		{name: "an instance with a less-than sign", mutate: func(f *vmBootPlistFixture) { f.env["LIMA_INSTANCE"] = "a<b" }},
		{name: "an instance beginning with a dash", mutate: func(f *vmBootPlistFixture) { f.env["LIMA_INSTANCE"] = "-x" }},
		{name: "an instance beginning with a dot", mutate: func(f *vmBootPlistFixture) { f.env["LIMA_INSTANCE"] = ".hidden" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMBootPlistFixture(t, "shift2eu")
			tc.mutate(f)

			stdout, stderr, err := f.plist(t)
			if err == nil {
				t.Fatalf("sh %s plist exited 0 with %s; it must refuse it and exit non-zero\nstdout: %s", vmBootScript, tc.name, stdout)
			}

			if stdout != "" {
				t.Errorf("the plist generator wrote to stdout although it refused %s: a refused run prints no plist, so make cannot install half of one:\n%s", tc.name, stdout)
			}

			if !strings.Contains(stderr, "LIMA_INSTANCE") {
				t.Errorf("the refusal of %s does not name LIMA_INSTANCE, so a maintainer cannot tell which value to fix:\nstderr: %s", tc.name, stderr)
			}
		})
	}
}

// TestVMBootLaunchdPlistRefusesToRunAsRoot pins that a plist rendered under `sudo -E make vm-boot-install` is
// refused before it prints anything: its UserName comes from id -un, so the job would run as root at every boot.
func TestVMBootLaunchdPlistRefusesToRunAsRoot(t *testing.T) {
	t.Parallel()

	f := newVMBootPlistFixture(t, "shift2eu")
	f.sb.useFakeIDAsRoot(t)

	stdout, stderr, err := f.plist(t)
	if err == nil {
		t.Fatalf("sh %s plist exited 0 with id -u answering 0: the plist would name root as the job's UserName, and the job would start the maintainer's VM as root at every boot\nstdout: %s\nstderr: %s", vmBootScript, stdout, stderr)
	}

	if stdout != "" {
		t.Errorf("the plist generator printed a plist although it refused to run as root, so make could install a job that runs as root:\n%s", stdout)
	}

	refusal := strings.ToLower(stderr)

	for _, want := range []string{"maintainer", "sudo"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not mention %q, so a maintainer who ran sudo -E make vm-boot-install cannot tell how to run it instead:\nstderr: %s", want, stderr)
		}
	}
}
