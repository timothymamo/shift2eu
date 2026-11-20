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
// serves `launchctl print <domain>` and `launchctl print <domain>/<label>` with the jobs the tests set up,
// including each job's arguments and environment, so no test reads the machine's launchd. Every
// launchctl, sudo, plutil and limactl call is recorded and runs nothing of the machine's.
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

// launchdJobs is the set of jobs the fake launchctl serves for one test.
type launchdJobs struct {
	sb  *sandbox
	dir string // SHIFT2EU_TEST_LAUNCHCTL_JOBS_DIR: <dir>/system and <dir>/gui/<uid> hold the jobs' files
}

// newLaunchdJobs builds the fixture for the jobs a test loads, inside the sandbox, and puts the recording launchctl
// first on PATH.
func newLaunchdJobs(t *testing.T, sb *sandbox) *launchdJobs {
	t.Helper()

	sb.writeFakeLaunchctl(t)

	return &launchdJobs{sb: sb, dir: filepath.Join(sb.root, "launchctl-jobs")}
}

// attach hands the fixture to one target's environment: the jobs the fake launchctl serves.
func (j *launchdJobs) attach(env map[string]string) {
	env["SHIFT2EU_TEST_LAUNCHCTL_JOBS_DIR"] = j.dir
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

// domainName is the domain as launchctl names it, which a refusal has to name too: `system` for the system domain,
// `gui/<uid>` for the user's.
func (j *launchdJobs) domainName(domain jobDomain) string {
	if domain == systemDomain {
		return "system"
	}

	return "gui/" + strconv.Itoa(os.Getuid())
}

// loadedJob is one job the fake launchctl serves: the process launchd runs for it, where its plist is, its program
// arguments and its environment.
type loadedJob struct {
	pid         int      // 0 is a job launchd runs no process for
	plist       string   // the plist launchd loaded it from; empty for a path of the fixture's own
	args        []string // the job's program arguments
	environment []string // KEY=VALUE, one per variable of the job's environment
}

// add loads one job: the fake launchctl serves it in its domain. It returns the path of the plist launchctl names
// for it, which is what a refusal repeats. Nothing is written there: no target reads a launchd plist directory,
// only what `launchctl print <domain>/<label>` says about the job, so the fixture writes no plist at all.
func (j *launchdJobs) add(t *testing.T, domain jobDomain, label string, job loadedJob) string {
	t.Helper()

	plist := job.plist
	if plist == "" {
		plist = filepath.Join(j.sb.root, "plists", label+".plist")
	}

	pidLine := "-"
	if job.pid != 0 {
		pidLine = strconv.Itoa(job.pid)
	}

	lines := append([]string{pidLine, plist}, job.args...)
	for _, entry := range job.environment {
		lines = append(lines, "env "+entry)
	}

	writeFileMode(t, filepath.Join(j.dirOf(t, domain), label), []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	return plist
}

// refreshRunningJob is one target that must look at the loaded launchd jobs: the sandbox and environment make
// runs with, the jobs the fake launchctl serves, the fake GitHub the start target mints from, and a way to run one
// target.
type refreshRunningJob struct {
	sb   *sandbox
	jobs *launchdJobs
	env  map[string]string // the environment make runs with, which a test may add to
	api  *fakeGitHub       // nil for the install fixture, which runs no refresh and mints nothing
	repo string            // the clone make runs in: the script a job of this clone runs
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

	return &refreshRunningJob{sb: f.sb, jobs: jobs, env: f.env, repo: f.repo, run: f.run}
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

	r := &refreshRunningJob{sb: f.sb, jobs: jobs, env: f.env, api: f.api, repo: dir}
	r.run = func(t *testing.T, target string) (string, string, error) {
		t.Helper()

		return runMakeIn(t, dir, []string{target}, f.env)
	}

	return r
}

// foreignJobRun is one direct run of dev/agents-token-launchd.sh foreign-jobs: a sandbox with the fake launchctl
// first on PATH and the jobs it serves, so a test pins the check's status and its words without a make recipe
// between them.
type foreignJobRun struct {
	sb   *sandbox
	jobs *launchdJobs
	env  map[string]string
}

// newForeignJobRun builds the fixture for the VM instance a direct run of the check works on.
func newForeignJobRun(t *testing.T, instance string) *foreignJobRun {
	t.Helper()

	sb := newSandbox(t)
	jobs := newLaunchdJobs(t, sb)

	env := sb.baseEnv()
	env["LIMA_INSTANCE"] = instance
	jobs.attach(env)

	return &foreignJobRun{sb: sb, jobs: jobs, env: env}
}

// run runs dev/agents-token-launchd.sh foreign-jobs in the fixture's environment.
func (r *foreignJobRun) run(t *testing.T) (string, string, error) {
	t.Helper()

	return r.sb.run(t, scriptPath(t, launchdScript), []string{"foreign-jobs"}, r.env, "")
}

// refreshArgs are the arguments of a job that runs an instance's refresh through /bin/sh, the way the project's
// own job does and a hand-made one did (issue #396).
func (r *foreignJobRun) refreshArgs() []string {
	return []string{"/bin/sh", "/Users/example/shift2eu/dev/agents-app-token.sh", "refresh"}
}

// firstControlByte is the first byte of text that a terminal reads as a control and is neither a line break nor a
// tab, or 0 when there is none: the bytes a refusal must never repeat from launchctl (security audit L1).
func firstControlByte(text string) byte {
	for index := 0; index < len(text); index++ {
		switch b := text[index]; {
		case b == '\n' || b == '\t':
		case b < 0x20 || b == 0x7f:
			return b
		}
	}

	return 0
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
			plist := f.jobs.add(t, tc.domain, foreignJobLabel, loadedJob{args: tc.args(f)})

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
			f.jobs.add(t, systemDomain, label, loadedJob{args: f.instanceRefreshArgs()})

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

// TestAgentsTokenLaunchdForeignJobsDoesNotRepeatAnUnsafePlistPath pins the security audit's first finding: the
// path launchctl names for a foreign job comes from the job's plist, and a same-uid LaunchAgent can put anything
// in its file name. Printed as it comes it lets escape sequences reach the terminal, and pasted into the
// `rm <plist>` the refusal suggests, a space or a `;` splits that command into another one. So a path outside /
// and [A-Za-z0-9._-] is never repeated: the refusal says to remove the plist and where to see its path instead
// (`launchctl print <domain>/<label>`), and still names the job and the commands that unload it. A safe path
// keeps the `rm <path>` line the refusal always printed.
func TestAgentsTokenLaunchdForeignJobsDoesNotRepeatAnUnsafePlistPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// domain is the launchd domain the foreign job is loaded in.
		domain jobDomain
		// plist is what `launchctl print <domain>/<label>` names as the job's plist.
		plist string
		// wantSafe says the path holds only the characters a command may name as it is.
		wantSafe bool
	}{
		{
			// A space splits the `rm` the refusal suggests into two paths, and the second one is not the job's.
			name:   "a path with a space",
			domain: systemDomain,
			plist:  "/Library/LaunchDaemons/my job.plist",
		},
		{
			// An escape byte reaches the maintainer's terminal unfiltered and can rewrite what it shows.
			name:   "a path with an escape byte",
			domain: userDomain,
			plist:  "/Users/example/Library/LaunchAgents/\x1b[31mred.plist",
		},
		{
			name:     "a path of safe characters",
			domain:   systemDomain,
			plist:    "/Library/LaunchDaemons/" + foreignJobLabel + ".plist",
			wantSafe: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newForeignJobRun(t, "shift2eu")
			plist := r.jobs.add(t, tc.domain, foreignJobLabel, loadedJob{plist: tc.plist, args: r.refreshArgs()})

			_, stderr, err := r.run(t)
			if status := exitStatusOf(t, err); status == 0 {
				t.Fatalf("foreign-jobs exited 0 although launchctl names %s as the plist of %s, which runs this instance's refresh: the job holds the instance's lock and the project's job never takes over (issue #396)\nstderr: %s",
					plist, foreignJobLabel, stderr)
			}

			if tc.wantSafe {
				if !strings.Contains(stderr, "rm "+plist) {
					t.Errorf("the refusal does not print `rm %s` for a path of safe characters: a maintainer has to be able to remove the plist with the command the refusal gives, or they have to guess its path (security audit L1)\nstderr: %s", plist, stderr)
				}

				return
			}

			// The unsafe path must not reach the terminal at all, so no `rm` can name it and no byte of it can
			// drive the terminal.
			if strings.Contains(stderr, plist) {
				t.Errorf("the refusal repeats the path launchctl named, %q, although it holds a character outside / and [A-Za-z0-9._-]: pasted into the `rm` a maintainer runs, that path names another file or another command (security audit L1)\nstderr: %s",
					plist, stderr)
			}

			if control := firstControlByte(stderr); control != 0 {
				t.Errorf("the refusal holds the control byte %#x, which launchctl's path put there: a refusal must never repeat a control byte to the maintainer's terminal (security audit L1)\nstderr: %s",
					control, stderr)
			}

			// Dropping the path must not drop the job: the refusal still names it, its domain and the commands
			// that unload it, and says where its path can be seen.
			for _, want := range []string{
				foreignJobLabel,
				r.jobs.domainName(tc.domain) + "/" + foreignJobLabel,
				"bootout",
				"disable",
				"remove its plist",
				"launchctl print " + r.jobs.domainName(tc.domain) + "/" + foreignJobLabel,
			} {
				if !strings.Contains(stderr, want) {
					t.Errorf("the refusal does not say %q although it holds the path of %s back: a maintainer still has to be able to find the job, see its plist and remove both (security audit L1)\nstderr: %s",
						want, foreignJobLabel, stderr)
				}
			}
		})
	}
}

// TestAgentsTokenLaunchdForeignJobsExitStatus pins the status the check gives its callers: 3 when it found a
// foreign job and named it, 0 when there is none, and another status on its own failure. The make recipes print
// "refused: another launchd job runs ..." only for 3 (TestMakeAgentsTokenRefreshDistinguishesAFailedForeignJobsCheck),
// so a check that could not run never sends a maintainer looking for a job that is not there (security audit L2).
func TestAgentsTokenLaunchdForeignJobsExitStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		instance   string
		load       func(t *testing.T, r *foreignJobRun)
		wantStatus int
		wantWords  []string
	}{
		{
			name:     "a foreign job is found",
			instance: "shift2eu",
			load: func(t *testing.T, r *foreignJobRun) {
				t.Helper()
				r.jobs.add(t, systemDomain, foreignJobLabel, loadedJob{args: r.refreshArgs()})
			},
			wantStatus: 3,
			wantWords:  []string{foreignJobLabel},
		},
		{
			name:       "no foreign job",
			instance:   "shift2eu",
			wantStatus: 0,
		},
		{
			// Its own failure: the name it needs is no name this script takes. It is no verdict about launchd,
			// so it must not read as "a foreign job was found".
			name:       "the check cannot read its instance",
			instance:   "a b",
			wantStatus: 1,
			wantWords:  []string{"LIMA_INSTANCE"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newForeignJobRun(t, tc.instance)
			if tc.load != nil {
				tc.load(t, r)
			}

			stdout, stderr, err := r.run(t)
			if status := exitStatusOf(t, err); status != tc.wantStatus {
				t.Fatalf("foreign-jobs exited with %d, want %d: 3 means it found and named a foreign job, 0 that there is none, and any other status is the check's own failure (security audit L2)\nstdout: %s\nstderr: %s",
					status, tc.wantStatus, stdout, stderr)
			}

			if tc.wantStatus == 0 && (stdout != "" || stderr != "") {
				t.Errorf("foreign-jobs printed something with no foreign job to report (status 0), so its caller cannot tell silence from a verdict\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			for _, want := range tc.wantWords {
				if !strings.Contains(stderr, want) {
					t.Errorf("foreign-jobs exited with %d but does not say %q, so its caller and a maintainer cannot tell what it found or failed on (security audit L2)\nstderr: %s",
						tc.wantStatus, want, stderr)
				}
			}
		})
	}
}

// TestAgentsTokenLaunchdForeignJobsWarnsWhenTheSystemDomainCannotBeListed pins the reviewer's second finding: a
// `launchctl print system` that fails or answers without a services block used to read as "no jobs loaded", so
// the check passed without a word although a system LaunchDaemon is exactly the job issue #396 was about. It now
// says once, on stderr, that it could not list the system domain, and still exits 0: the install or the start may
// go on. The user's domain is different - with no GUI login there is nothing to list and nothing to say.
func TestAgentsTokenLaunchdForeignJobsWarnsWhenTheSystemDomainCannotBeListed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		domain   string
		variable string
		wantWarn bool
	}{
		{
			name:     "launchctl cannot print the system domain",
			domain:   "system",
			variable: "SHIFT2EU_TEST_LAUNCHCTL_UNLISTABLE_DOMAIN",
			wantWarn: true,
		},
		{
			name:     "the system domain's print holds no services block",
			domain:   "system",
			variable: "SHIFT2EU_TEST_LAUNCHCTL_NO_SERVICES_DOMAIN",
			wantWarn: true,
		},
		{
			// No GUI login is no failure: the check must not warn about a domain a session has none of.
			name:     "launchctl cannot print the user domain",
			domain:   "gui/" + strconv.Itoa(os.Getuid()),
			variable: "SHIFT2EU_TEST_LAUNCHCTL_UNLISTABLE_DOMAIN",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newForeignJobRun(t, "shift2eu")
			r.env[tc.variable] = tc.domain

			stdout, stderr, err := r.run(t)
			if status := exitStatusOf(t, err); status != 0 {
				t.Fatalf("foreign-jobs exited with %d although it could not list %s: a domain it cannot read is no foreign job, and the install or the start has to go on (reviewer finding 2)\nstdout: %s\nstderr: %s",
					status, tc.domain, stdout, stderr)
			}

			warned := strings.Contains(stderr, "could not list the system domain")
			if warned != tc.wantWarn {
				t.Errorf("foreign-jobs warned about the system domain: %v, want %v, when launchctl prints %s the way the case says: without the warning the check passes in silence although a LaunchDaemon would go unnoticed, and with it for the user's domain it warns where there is nothing to list (reviewer finding 2)\nstderr: %s",
					warned, tc.wantWarn, tc.domain, stderr)
			}
		})
	}

	t.Run("the install still installs", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")
		f.env["SHIFT2EU_TEST_LAUNCHCTL_UNLISTABLE_DOMAIN"] = "system"

		stdout, stderr, err := f.run(t, "agents-token-refresh-install")
		if err != nil {
			t.Fatalf("make agents-token-refresh-install: %v although the system domain could not be listed, which is no foreign job (reviewer finding 2)\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if calls := f.bootstrapCalls(t); len(calls) != 1 {
			t.Errorf("the install ran launchctl bootstrap %d time(s), want one, although a system domain it could not list is no foreign job to refuse (reviewer finding 2): %v", len(calls), calls)
		}

		if _, statErr := os.Stat(f.dest()); statErr != nil {
			t.Errorf("the install did not write %s although a system domain it could not list is no foreign job to refuse (reviewer finding 2): %v", f.dest(), statErr)
		}

		if !strings.Contains(stdout+stderr, "could not list the system domain") {
			t.Errorf("make agents-token-refresh-install does not say that it could not list the system domain, so a maintainer whose LaunchDaemon went unnoticed is never told (reviewer finding 2)\nstdout: %s\nstderr: %s", stdout, stderr)
		}
	})

	t.Run("the start still starts", func(t *testing.T) {
		t.Parallel()

		f := newStartJobFixture(t)
		f.env["SHIFT2EU_TEST_LAUNCHCTL_UNLISTABLE_DOMAIN"] = "system"

		stdout, stderr, err := f.run(t, "agents-token-refresh-start")
		if err != nil {
			t.Fatalf("make agents-token-refresh-start: %v although the system domain could not be listed, which is no foreign job (reviewer finding 2)\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if pids := f.pidFiles(t); len(pids) != 1 {
			t.Errorf("make agents-token-refresh-start left %d pid file(s), want one, although a system domain it could not list is no foreign job to refuse (reviewer finding 2): %v", len(pids), pids)
		}
	})
}

// TestAgentsTokenLaunchdForeignJobsIgnoresJobsThatAreNoForeignJobOfThisInstance pins the exclusions the check's
// own regexes and environment block make (reviewer finding 3): a job that runs the refresh of another instance
// (its environment names that instance), a job that runs `make agents-token-refresh-start` (not the refresh
// target, so not the lock's holder), and an Apple job (skipped by its com.apple. prefix before launchctl is asked
// anything). The same job with this instance in its environment is refused, so the environment is what decides.
func TestAgentsTokenLaunchdForeignJobsIgnoresJobsThatAreNoForeignJobOfThisInstance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// label is the job's label; empty makes it foreignJobLabel.
		label string
		// runsRefresh says the job runs an instance's refresh through /bin/sh.
		runsRefresh bool
		// args are the job's arguments when it runs no refresh of the project's shape.
		args        []string
		environment []string
		wantStatus  int
	}{
		{
			name:        "a job whose environment names another instance",
			runsRefresh: true,
			environment: []string{"LIMA_INSTANCE=other"},
		},
		{
			name: "a job that runs make agents-token-refresh-start",
			args: []string{"/usr/local/bin/make", "agents-token-refresh-start"},
		},
		{
			name:        "an Apple job that runs the refresh",
			label:       "com.apple.something",
			runsRefresh: true,
		},
		{
			name:        "a job whose environment names this instance",
			runsRefresh: true,
			environment: []string{"LIMA_INSTANCE=shift2eu"},
			wantStatus:  3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newForeignJobRun(t, "shift2eu")

			label := tc.label
			if label == "" {
				label = foreignJobLabel
			}

			job := loadedJob{args: tc.args, environment: tc.environment}
			if tc.runsRefresh {
				job.args = r.refreshArgs()
			}

			r.jobs.add(t, systemDomain, label, job)

			_, stderr, err := r.run(t)
			if status := exitStatusOf(t, err); status != tc.wantStatus {
				t.Fatalf("foreign-jobs exited with %d, want %d, for %s: the check counts a job that is no foreign job of this instance (another instance's refresh, the start target, an Apple job), or misses one that is (reviewer finding 3)\nstderr: %s",
					status, tc.wantStatus, label, stderr)
			}

			if tc.wantStatus == 3 && !strings.Contains(stderr, label) {
				t.Errorf("foreign-jobs refuses %s but does not name it, so a maintainer cannot find the job that holds the instance's lock (reviewer finding 3)\nstderr: %s", label, stderr)
			}
		})
	}
}

// TestAgentsTokenLaunchdForeignJobsCountsTheUnusualLabelsItDoesNotCheck pins the reviewer's seventh finding: a
// loaded label outside [A-Za-z0-9._-] is skipped (it cannot safely be part of a `launchctl print` argument or a
// command a maintainer runs), but silently skipping it is a false negative nobody sees. The check counts such
// labels and says how many it did not look at, on stderr, without printing any of them - a label is untrusted
// text too, and one with a space must be read whole, not as the last word of launchctl's line.
func TestAgentsTokenLaunchdForeignJobsCountsTheUnusualLabelsItDoesNotCheck(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		labels    []string
		wantCount string
	}{
		{
			name:      "one label with a space",
			labels:    []string{"spaced label name"},
			wantCount: "1",
		},
		{
			name:      "one label with an escape byte",
			labels:    []string{"esc\x1blabel"},
			wantCount: "1",
		},
		{
			name:      "two unusual labels",
			labels:    []string{"spaced label name", "esc\x1blabel"},
			wantCount: "2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newForeignJobRun(t, "shift2eu")

			for _, label := range tc.labels {
				r.jobs.add(t, systemDomain, label, loadedJob{args: r.refreshArgs()})
			}

			stdout, stderr, err := r.run(t)
			if status := exitStatusOf(t, err); status != 0 {
				t.Fatalf("foreign-jobs exited with %d for a job whose label it skips: an unusual label is not checked, never a refusal (reviewer finding 7)\nstdout: %s\nstderr: %s",
					status, stdout, stderr)
			}

			counted := ""

			for _, line := range strings.Split(stderr, "\n") {
				if strings.Contains(line, "with unusual labels not checked") {
					counted = line
				}
			}

			if counted == "" {
				t.Fatalf("foreign-jobs says nothing about the %d job(s) whose labels it did not check, so a maintainer never learns that the check looked past them (reviewer finding 7)\nstdout: %s\nstderr: %s",
					len(tc.labels), stdout, stderr)
			}

			if !strings.Contains(counted, tc.wantCount) {
				t.Errorf("the line that says labels were not checked does not count them (want %s), so a maintainer cannot tell one skipped job from several (reviewer finding 7)\nline: %s\nstderr: %s",
					tc.wantCount, counted, stderr)
			}

			for _, label := range tc.labels {
				if strings.Contains(stdout+stderr, label) {
					t.Errorf("foreign-jobs prints the unusual label %q, which a job's plist controls: the label is untrusted text and must only be counted (reviewer finding 7)\nstdout: %s\nstderr: %s",
						label, stdout, stderr)
				}
			}

			if control := firstControlByte(stderr); control != 0 {
				t.Errorf("foreign-jobs prints the control byte %#x a job's label holds, although such a label is not checked and never printed (reviewer finding 7)\nstderr: %s", control, stderr)
			}
		})
	}
}
