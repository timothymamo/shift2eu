// Pins what make agents-token-refresh-install and make agents-token-refresh-start do about a launchd job that runs
// this VM instance's refresh and is not the project's own (issue #396, the maintainer's second comment). An older,
// hand-made LaunchDaemon (label local.agent-host.shift2eu.agents-token-refresh, plist
// /Library/LaunchDaemons/shift2eu.agents-token-refresh.plist, arguments `direnv exec <host clone> make
// agents-token-refresh`, KeepAlive) was still loaded on the Mac Mini after ADR 0034. It restarted its refresher
// seconds after every kill, so it kept the instance's lock and the project's job only ever found the lock taken and
// exited 0 - nothing told the maintainer that the job never took over.
//
// Both targets must refuse such a job, in the system domain and in the user domain, before they load or start
// anything, and name the job with the commands that remove it. Only the project's own job is no foreign job: the
// install replaces it as it always has, and the start target keeps its warning and starts nothing.
//
// The jobs launchd has loaded are the fake launchctl's (sandbox_test.go, SHIFT2EU_TEST_LAUNCHCTL_JOBS_DIR): it
// serves `launchctl list`, `launchctl print <domain>` and `launchctl print <domain>/<label>` with the jobs the
// tests set up, and the launchd plist directories are the sandbox's, through AGENTS_TOKEN_LAUNCHD_SYSTEM_DIR and
// AGENTS_TOKEN_LAUNCHD_USER_DIR on make's command line, so no test reads the machine's /Library or its own
// LaunchAgents. Every launchctl, sudo, plutil and limactl call is recorded and runs nothing of the machine's.
package devenv_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// foreignJobLabel is the label of the job the issue found loaded beside the project's own. It carries the refresh's
// name but no prefix of the project's (launchdLabelPrefix), so it is exactly the label a check for "any job that
// runs this instance's refresh, other than the project's own" has to catch (issue #396).
const foreignJobLabel = "local.agent-host.shift2eu.agents-token-refresh"

// launchdJobs is the set of jobs the fake launchctl serves for one test, and the launchd plist directories the
// targets may read them from.
type launchdJobs struct {
	sb        *sandbox
	dir       string // SHIFT2EU_TEST_LAUNCHCTL_JOBS_DIR: <dir>/system and <dir>/gui/<uid> hold the jobs' files
	systemDir string // AGENTS_TOKEN_LAUNCHD_SYSTEM_DIR: the system domain's plists
	userDir   string // AGENTS_TOKEN_LAUNCHD_USER_DIR: the user domain's plists
}

// newLaunchdJobs builds the fixture for the jobs a test loads, inside the sandbox, and puts the recording launchctl
// first on PATH.
func newLaunchdJobs(t *testing.T, sb *sandbox) *launchdJobs {
	t.Helper()

	j := &launchdJobs{
		sb:        sb,
		dir:       filepath.Join(sb.root, "launchctl-jobs"),
		systemDir: filepath.Join(sb.root, "system-LaunchDaemons"),
		userDir:   filepath.Join(sb.hostHome, "Library", "LaunchAgents"),
	}

	for _, dir := range []string{filepath.Join(j.dir, "system"), j.systemDir, j.userDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	sb.writeFakeLaunchctl(t)

	return j
}

// attach hands the fixture to one target's environment: the jobs the fake launchctl serves.
func (j *launchdJobs) attach(env map[string]string) {
	env["SHIFT2EU_TEST_LAUNCHCTL_JOBS_DIR"] = j.dir
}

// args are the make command-line assignments that point the targets' launchd plist directories inside the sandbox.
func (j *launchdJobs) args() []string {
	return []string{
		"AGENTS_TOKEN_LAUNCHD_SYSTEM_DIR=" + j.systemDir,
		"AGENTS_TOKEN_LAUNCHD_USER_DIR=" + j.userDir,
	}
}

// jobDomain names the two domains a test loads a job in.
type jobDomain string

const (
	systemDomain jobDomain = "system"
	userDomain   jobDomain = "user"
)

// dirOf is the directory of domain's jobs the fake launchctl reads.
func (j *launchdJobs) dirOf(t *testing.T, domain jobDomain) string {
	t.Helper()

	switch domain {
	case systemDomain:
		return filepath.Join(j.dir, "system")
	case userDomain:
		return filepath.Join(j.dir, "gui", strconv.Itoa(os.Getuid()))
	}

	t.Fatalf("unknown launchd domain %q", domain)

	return ""
}

// plistDirOf is where launchd reads domain's plists from: the sandbox's stand-ins for /Library/LaunchDaemons and
// ~/Library/LaunchAgents.
func (j *launchdJobs) plistDirOf(t *testing.T, domain jobDomain) string {
	t.Helper()

	switch domain {
	case systemDomain:
		return j.systemDir
	case userDomain:
		return j.userDir
	}

	t.Fatalf("unknown launchd domain %q", domain)

	return ""
}

// domainName is the domain as launchctl names it, which a refusal has to name too: `system` for the system domain,
// `gui/<uid>` for the user's.
func (j *launchdJobs) domainName(domain jobDomain) string {
	if domain == systemDomain {
		return "system"
	}

	return "gui/" + strconv.Itoa(os.Getuid())
}

// add loads one job: the fake launchctl serves it in its domain, and its plist is where launchd reads it, so a
// target that looks at either finds the same job. pid 0 is a job launchd runs no process for. It returns the
// plist's path.
func (j *launchdJobs) add(t *testing.T, domain jobDomain, label string, pid int, args []string) string {
	t.Helper()

	plist := filepath.Join(j.plistDirOf(t, domain), label+".plist")
	writeFileMode(t, plist, []byte(jobPlist(label, args)), 0o600)

	pidLine := "-"
	if pid != 0 {
		pidLine = strconv.Itoa(pid)
	}

	job := pidLine + "\n" + plist + "\n" + strings.Join(args, "\n") + "\n"
	writeFileMode(t, filepath.Join(j.dirOf(t, domain), label), []byte(job), 0o600)

	return plist
}

// jobPlist renders a plist as launchd reads it for the job: its label, its program arguments and the KeepAlive the
// hand-made job had, which is what makes it start its refresher again after every kill (issue #396).
func jobPlist(label string, args []string) string {
	arguments := make([]string, 0, len(args))
	for _, arg := range args {
		arguments = append(arguments, "\t\t<string>"+arg+"</string>")
	}

	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + label + `</string>
	<key>ProgramArguments</key>
	<array>
` + strings.Join(arguments, "\n") + `
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`
}

// refreshRunningJob is one target that must look at the loaded launchd jobs: the sandbox and environment make
// runs with, the jobs the fake launchctl serves, the fake GitHub the start target mints from, and a way to run one
// target.
type refreshRunningJob struct {
	sb   *sandbox
	jobs *launchdJobs
	api  *fakeGitHub // nil for the install fixture, which runs no refresh and mints nothing
	repo string      // the clone make runs in: the script a job of this clone runs
	run  func(t *testing.T, target string) (string, string, error)
}

// newInstallJobFixture is the fixture of make agents-token-refresh-install: the launchd install fixture (the fake
// sudo, launchctl, plutil and limactl, the invented key and the launchd directory inside the sandbox) plus the jobs
// the test loads.
func newInstallJobFixture(t *testing.T) *refreshRunningJob {
	t.Helper()

	f := newLaunchdInstallFixture(t, "shift2eu")
	jobs := newLaunchdJobs(t, f.sb)
	jobs.attach(f.env)
	f.extraArgs = append(f.extraArgs, jobs.args()...)

	return &refreshRunningJob{sb: f.sb, jobs: jobs, repo: f.repo, run: f.run}
}

// newStartJobFixture is the fixture of make agents-token-refresh-start: the token fixture (the fake GitHub, the
// invented key and the state directory inside the sandbox) in a clone of the Makefile, plus the jobs the test loads.
func newStartJobFixture(t *testing.T) *refreshRunningJob {
	t.Helper()

	f := newTokenFixture(t, "shift2eu")

	dir := filepath.Join(f.sb.root, "clone")
	launchdClone(t, dir)

	jobs := newLaunchdJobs(t, f.sb)
	jobs.attach(f.env)

	r := &refreshRunningJob{sb: f.sb, jobs: jobs, api: f.api, repo: dir}
	r.run = func(t *testing.T, target string) (string, string, error) {
		t.Helper()

		return runMakeIn(t, dir, append(jobs.args(), target), f.env)
	}

	return r
}

// instanceRefreshArgs are the arguments of a job that runs this clone's refresh through /bin/sh, the way the
// project's own job does.
func (f *refreshRunningJob) instanceRefreshArgs() []string {
	return []string{"/bin/sh", filepath.Join(f.repo, "dev", "agents-app-token.sh"), "refresh"}
}

// bootstrapCalls returns the argv of every `sudo launchctl bootstrap` the fixture recorded, in call order.
func (f *refreshRunningJob) bootstrapCalls(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range invocations(f.sb.argvLogText(t), "sudo") {
		if len(args) > 1 && args[0] == "launchctl" && args[1] == "bootstrap" {
			calls = append(calls, args)
		}
	}

	return calls
}

// installedWithSudo reports whether the fixture ran `sudo install`, the command that puts the plist where launchd
// loads it at the next boot.
func (f *refreshRunningJob) installedWithSudo(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range invocations(f.sb.argvLogText(t), "sudo") {
		if len(args) > 0 && args[0] == "install" {
			calls = append(calls, args)
		}
	}

	return calls
}

// pidFiles are the pid files a started refresher leaves in the state directory, empty when none was started.
func (f *refreshRunningJob) pidFiles(t *testing.T) []string {
	t.Helper()

	pids, err := filepath.Glob(filepath.Join(f.sb.stateDir(), "*.pid"))
	if err != nil {
		t.Fatalf("look for pid files in %s: %v", f.sb.stateDir(), err)
	}

	return pids
}

// TestMakeAgentsTokenRefreshRefusesAForeignLaunchdJob pins the maintainer's second comment in issue #396: while a
// job other than the project's own runs this instance's refresh, the project's job can never take the instance's
// lock - the foreign one takes it again seconds after every kill - so the install must refuse to load its job and
// the start target must refuse to start a refresher, before either does anything, naming the job and the commands
// that remove it. That holds for a job launchd loaded in the system domain and for one in the user's, and the two
// shapes the issue names are both caught: arguments that run `make agents-token-refresh` (the issue's own job) and
// ones that run dev/agents-app-token.sh refresh.
func TestMakeAgentsTokenRefreshRefusesAForeignLaunchdJob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// target is the make target that must refuse.
		target string
		// fixture builds the environment the target runs in.
		fixture func(t *testing.T) *refreshRunningJob
		// domain is the launchd domain the foreign job is loaded in.
		domain jobDomain
		// args are the job's program arguments, the shape the issue found or the other one it names.
		args func(f *refreshRunningJob) []string
	}{
		{
			name:    "the install, a system job that runs make agents-token-refresh",
			target:  "agents-token-refresh-install",
			fixture: newInstallJobFixture,
			domain:  systemDomain,
			// The issue's job: `direnv exec <host clone> make agents-token-refresh`, started again by KeepAlive.
			args: func(f *refreshRunningJob) []string {
				return []string{"/usr/local/bin/direnv", "exec", f.repo, "make", "agents-token-refresh"}
			},
		},
		{
			name:    "the install, a user job that runs the refresh script",
			target:  "agents-token-refresh-install",
			fixture: newInstallJobFixture,
			domain:  userDomain,
			args:    func(f *refreshRunningJob) []string { return f.instanceRefreshArgs() },
		},
		{
			name:    "the start, a system job that runs make agents-token-refresh",
			target:  "agents-token-refresh-start",
			fixture: newStartJobFixture,
			domain:  systemDomain,
			args: func(f *refreshRunningJob) []string {
				return []string{"/usr/local/bin/direnv", "exec", f.repo, "make", "agents-token-refresh"}
			},
		},
		{
			name:    "the start, a user job that runs the refresh script",
			target:  "agents-token-refresh-start",
			fixture: newStartJobFixture,
			domain:  userDomain,
			args:    func(f *refreshRunningJob) []string { return f.instanceRefreshArgs() },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := tc.fixture(t)
			plist := f.jobs.add(t, tc.domain, foreignJobLabel, 0, tc.args(f))

			stdout, stderr, err := f.run(t, tc.target)
			if err == nil {
				t.Fatalf("make %s exited 0 although launchd has %s loaded in the %s domain and it runs this instance's refresh (issue #396): the foreign job takes the instance's lock again seconds after every kill, so the project's job never takes over and nothing says so\nstdout: %s\nstderr: %s",
					tc.target, foreignJobLabel, f.jobs.domainName(tc.domain), stdout, stderr)
			}

			combined := stdout + stderr

			// The refusal has to name the job, the domain it is in and what removes it: a maintainer who reads it
			// has to be able to remove the job, not only learn that one is there.
			for _, want := range []string{
				foreignJobLabel,
				f.jobs.domainName(tc.domain) + "/" + foreignJobLabel,
				"bootout",
				"disable",
				plist,
			} {
				if !strings.Contains(combined, want) {
					t.Errorf("make %s does not say %q although launchd has %s loaded in the %s domain and it runs this instance's refresh: the refusal must name the job and the commands that remove it, or a maintainer cannot free the instance's lock (issue #396)\nstdout: %s\nstderr: %s",
						tc.target, want, foreignJobLabel, f.jobs.domainName(tc.domain), stdout, stderr)
				}
			}

			// Refusing means doing nothing: nothing is installed for launchd to load at the next boot either.
			if calls := f.installedWithSudo(t); len(calls) != 0 {
				t.Errorf("make %s ran `sudo install` although it refuses to load anything while %s runs this instance's refresh: a plist left in /Library/LaunchDaemons is a job launchd loads at the next boot whatever this run decided (issue #396): %v\nstdout: %s\nstderr: %s",
					tc.target, foreignJobLabel, calls, stdout, stderr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 0 {
				t.Errorf("make %s ran `sudo launchctl bootstrap` although it refuses to load anything while %s runs this instance's refresh (issue #396): %v\nstdout: %s\nstderr: %s",
					tc.target, foreignJobLabel, calls, stdout, stderr)
			}

			if pids := f.pidFiles(t); len(pids) != 0 {
				t.Errorf("make %s started a refresher although it refuses to run beside %s (issue #396): %v\nstdout: %s\nstderr: %s",
					tc.target, foreignJobLabel, pids, stdout, stderr)
			}

			if f.api != nil {
				if minted := f.api.minted(); len(minted) != 0 {
					t.Errorf("make %s minted %d token(s) although it refuses to run beside %s: the refusal comes before anything is started (issue #396)", tc.target, len(minted), foreignJobLabel)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshAcceptsOnlyItsOwnLaunchdJob pins the other half of the refusal: the project's own job is
// no foreign job. The install replaces it as it always has (booting the loaded one out and loading the new plist),
// and the start target keeps its warning that the job is the instance's refresher, starts nothing beside it and
// exits 0. A check that refused the project's own job would leave a maintainer unable to install or start anything
// (issue #396).
func TestMakeAgentsTokenRefreshAcceptsOnlyItsOwnLaunchdJob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		target  string
		fixture func(t *testing.T) *refreshRunningJob
	}{
		{name: "the install", target: "agents-token-refresh-install", fixture: newInstallJobFixture},
		{name: "the start", target: "agents-token-refresh-start", fixture: newStartJobFixture},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := tc.fixture(t)
			label := launchdLabelPrefix + "shift2eu"
			f.jobs.add(t, systemDomain, label, 0, f.instanceRefreshArgs())

			stdout, stderr, err := f.run(t, tc.target)
			if err != nil {
				t.Fatalf("make %s: %v although the only loaded job is the project's own %s, which runs this instance's refresh through dev/agents-app-token.sh: a job of the project's own label is the instance's refresher, never a foreign one (issue #396)\nstdout: %s\nstderr: %s",
					tc.target, err, label, stdout, stderr)
			}

			// Nothing foreign means today's behaviour: the install replaces the job it found loaded, and the start
			// target warns about the LaunchDaemon and starts no refresher of its own.
			if strings.Contains(tc.target, "install") {
				if calls := f.bootstrapCalls(t); len(calls) != 1 {
					t.Errorf("the install ran launchctl bootstrap %d time(s), want one, although the only loaded job is its own %s: the install unloads that one and loads the new plist (issue #396): %v", len(calls), label, calls)
				}

				return
			}

			if !strings.Contains(stdout+stderr, label) || !strings.Contains(stdout+stderr, "LaunchDaemon") {
				t.Errorf("make %s does not warn that the LaunchDaemon %s runs the instance's refresh any more, so a maintainer starts one although the job is there (issue #396)\nstdout: %s\nstderr: %s",
					tc.target, label, stdout, stderr)
			}

			if pids := f.pidFiles(t); len(pids) != 0 {
				t.Errorf("make %s started a refresher beside the LaunchDaemon %s (issue #396): %v", tc.target, label, pids)
			}
		})
	}
}
