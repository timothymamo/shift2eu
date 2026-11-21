// Pins the guard issue #402 asks for: the host targets that load the VM credentials refuse to run when the
// maintainer's direnv setup reads a .envrc from somewhere other than the host clone's own .envrc, or when a
// .envrc or .env sits in the workspace the VM mounts or in a directory above it. docs/dev-environment.md, The
// host clone, says the VM's credentials (CLAUDE_CODE_OAUTH_TOKEN, DEEPSEEK_API_KEY) live only in the host
// clone's .envrc, never in a .envrc in or above the workspace: the workspace is the directory the VM mounts, so
// a .envrc there - or above it, which direnv finds with source_up - hands the credentials to every Mac shell the
// maintainer opens in it, and the VM's credentials are meant for the VM alone (issue #227, ADR 0031).
//
// Every row runs one of the six host targets that load the credentials in a sandbox layout - the host clone make
// runs in, and the workspace whose checkout WORKSPACE_REPO names - and pins one of two things: a refusal (a
// non-zero exit, a message on stderr naming the file the maintainer must move, and no call of any tool the fakes
// record, so the guard ran before anything else), or a pass (the target reaches its next step, which is what the
// guard must not stop). The message of a refusal holds names and paths only: both files the tests write hold
// made-up credential values, and no output may carry either of them (CLAUDE.md rule 3, issue #402).
//
// The host targets that load the credentials are vm-env, the three that run it or its check (vm-up, vm-create,
// vm-init) and the two that start or install the agents App token refresh (agents-token-refresh-start,
// agents-token-refresh-install). The tests refuse rather than warn: vm-env copies the credentials into the VM,
// so a run with a .envrc in the wrong place is the moment to stop it.
package devenv_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// envrcGuardTargets are the six host targets issue #402 gives the guard to: the ones that load the VM's
// credentials, directly (vm-env) or through the checks and sub-makes around it, and the two that run the agents
// App's token refresh with the App's key in reach.
var envrcGuardTargets = []string{
	"agents-token-refresh-install",
	"agents-token-refresh-start",
	"vm-create",
	"vm-env",
	"vm-init",
	"vm-up",
}

// The made-up credential values the tests plant in every file they write, each on the line of a credential
// docs/dev-environment.md keeps in the host clone's .envrc. Neither is a credential (CLAUDE.md rule 3), and a
// guard that read one of them must never print it (issue #402).
const (
	plantedOAuthValue    = "planted-oauth-value-402"
	plantedDeepseekValue = "planted-deepseek-value-402"
)

// plantedValues are the two made-up values, as a message may not hold them.
var plantedValues = []string{plantedOAuthValue, plantedDeepseekValue}

// plantedEnvrcLines are the two lines every .envrc and .env the tests write holds beside the lines the case is
// about: the credentials, written the way the maintainer's direnv .envrc holds them. Neither value may appear in
// what a refused run prints (issue #402).
func plantedEnvrcLines() string {
	return "CLAUDE_CODE_OAUTH_TOKEN=" + plantedOAuthValue + "\n" +
		"DEEPSEEK_API_KEY=" + plantedDeepseekValue + "\n"
}

// envrcGuardTools are the tools a refused run must not have reached: every tool a host target runs as the
// maintainer, and the account check's and SSH mode's tools, which the two token targets run first. The fakes of
// the sandbox record each call, so one call of any of them proves the guard did not run first (issue #402).
var envrcGuardTools = []string{"dscl", "dseditgroup", "ssh"}

// envrcNextStep is what one of the six targets reaches once the guard lets it through: the first tool it runs, or
// the message of the credential check it prints when the sandbox sets no agents App variable, which is as far as
// a target the sandbox cannot take further gets. Reaching it is what "the guard passed" means here.
type envrcNextStep struct {
	tool    string
	message string
}

// envrcNextSteps is the next step of each of the six targets.
var envrcNextSteps = map[string]envrcNextStep{
	"agents-token-refresh-install": {message: "SHIFT2EU_AGENTS_APP_ID"},
	"agents-token-refresh-start":   {message: "SHIFT2EU_AGENTS_APP_ID"},
	"vm-create":                    {tool: "limactl"},
	"vm-env":                       {tool: "limactl"},
	"vm-init":                      {tool: "limactl"},
	"vm-up":                        {tool: "limactl"},
}

// envrcLayoutCase is one layout the guard is run in, for one or more of the six targets: the .envrc files a
// maintainer's machine would have, and the files a refusal must name.
type envrcLayoutCase struct {
	name string
	// hostEnvrc is what the host clone's .envrc holds beside the planted lines, and empty leaves the clone
	// without one.
	hostEnvrc string
	// setup writes the files of the layout hostEnvrc cannot spell, such as a file outside the clone whose path
	// only the sandbox knows, and returns the paths a refusal must name.
	setup func(t *testing.T, l *hostLayout) []string
	// targets narrows the case to some of the six targets, and empty runs it against all of them.
	targets []string
	// want is what a refusal's stderr must say beyond the paths, such as the docs section that holds the fix.
	want []string
}

// runs is the targets the case runs against.
func (tc envrcLayoutCase) runs() []string {
	if len(tc.targets) > 0 {
		return tc.targets
	}

	return envrcGuardTargets
}

// TestHostTargetsRefuseAHostCloneEnvrcThatReadsAParent pins the first half of issue #402: the host clone's .envrc
// may hold the maintainer's credentials, but it may not read a parent. `source_up` and `source_up_if_exists` find
// a .envrc in a directory above the clone, and `source_env`, `source_env_if_exists`, `dotenv` and
// `dotenv_if_exists` name one: the credentials would then live in a file outside the clone, which direnv loads
// in every shell the maintainer opens there, and which no review of this repository reaches. Every one of the six
// targets must refuse such a clone, and the refusal must name the clone's .envrc and send the maintainer to
// docs/dev-environment.md, The host clone, where every line belongs.
func TestHostTargetsRefuseAHostCloneEnvrcThatReadsAParent(t *testing.T) {
	t.Parallel()

	cases := []envrcLayoutCase{
		{
			name:      "the host clone's .envrc says source_up",
			hostEnvrc: "source_up\n",
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_up_if_exists",
			hostEnvrc: "source_up_if_exists\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_up_if_exists with a file name",
			hostEnvrc: "source_up_if_exists .envrc.private\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_env ..",
			hostEnvrc: "source_env ..\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_env ../secrets.env",
			hostEnvrc: "source_env ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says dotenv ../x.env",
			hostEnvrc: "dotenv ../x.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says dotenv_if_exists ../secrets.env",
			hostEnvrc: "dotenv_if_exists ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			name:      "the host clone's .envrc says source_env_if_exists ../secrets.env",
			hostEnvrc: "source_env_if_exists ../secrets.env\n",
			targets:   []string{"vm-env"},
			want:      []string{"The host clone"},
		},
		{
			// An absolute path outside the clone is as far outside it as `..` is: a file of the workspace's
			// parent, which only the sandbox knows the path of.
			name: "the host clone's .envrc names an absolute path outside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				outside := filepath.Join(l.sb.root, "outside", "secrets.env")
				writeFileMode(t, outside, []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source_env "+outside+"\n")

				return []string{filepath.Join(l.root, ".envrc")}
			},
			targets: []string{"vm-env"},
			want:    []string{"The host clone"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range tc.runs() {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					run := makeInEnvrcLayout(t, target, tc)
					run.assertRefused(t, target, tc.want)
				})
			}
		})
	}
}

// TestHostTargetsRefuseAnEnvrcFileInOrAboveTheWorkspace pins the second half of issue #402: no .envrc and no .env
// may sit in the workspace the VM mounts - the parent directory of WORKSPACE_REPO - or in a directory above it,
// where direnv finds it and loads the credentials in every shell the maintainer opens in the workspace. A file
// there is refused by its path alone, and the refusal names it, so the maintainer knows which file to move into
// the host clone's .envrc. The host clone's own .envrc is clean here, which pins that the check reaches beyond
// the clone it runs in.
func TestHostTargetsRefuseAnEnvrcFileInOrAboveTheWorkspace(t *testing.T) {
	t.Parallel()

	cases := []envrcLayoutCase{
		{
			name: "the workspace holds a .envrc",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 1, ".envrc")}
			},
		},
		{
			name: "the workspace holds a .env",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 1, ".env")}
			},
		},
		{
			name: "a directory above the workspace holds a .envrc",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 2, ".envrc")}
			},
		},
		{
			name: "a directory above the workspace holds a .env",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeHostEnvrc(t, l, "")

				return []string{writeAboveWorkspace(t, l, 2, ".env")}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range tc.runs() {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					run := makeInEnvrcLayout(t, target, tc)
					run.assertRefused(t, target, tc.want)
				})
			}
		})
	}
}

// TestHostTargetsPassACleanEnvrcLayout pins what the guard must not stop: the maintainer's layout when the host
// clone's .envrc reads no parent - it holds its lines itself, or it sources a file inside the clone, written as a
// relative or an absolute path - and when no .envrc and no .env sits in the workspace or above it. Each of the six
// targets must reach its next step, so the guard does not refuse the layout the docs ask for.
func TestHostTargetsPassACleanEnvrcLayout(t *testing.T) {
	t.Parallel()

	cases := []envrcLayoutCase{
		{
			name: "the host clone has no .envrc",
		},
		{
			name:      "the host clone's .envrc sources a file inside the clone",
			hostEnvrc: "source_env ./host-secrets.env\ndotenv ./.env.local\n",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				writeFileMode(t, filepath.Join(l.root, "host-secrets.env"), []byte(plantedEnvrcLines()), 0o600)
				writeFileMode(t, filepath.Join(l.root, ".env.local"), []byte(plantedEnvrcLines()), 0o600)

				return nil
			},
		},
		{
			name: "the host clone's .envrc names an absolute path inside the clone",
			setup: func(t *testing.T, l *hostLayout) []string {
				t.Helper()

				inside := filepath.Join(l.root, "host-secrets.env")
				writeFileMode(t, inside, []byte(plantedEnvrcLines()), 0o600)
				writeHostEnvrc(t, l, "source_env "+inside+"\n")

				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range tc.runs() {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					run := makeInEnvrcLayout(t, target, tc)
					run.assertPassed(t, target)
				})
			}
		})
	}
}

// envrcRun is one run of one host target in a layout the guard is run in: what make printed and returned, the
// sandbox whose fakes recorded what ran, and the files a refusal must name.
type envrcRun struct {
	layout *hostLayout
	// files are the .envrc or .env files the layout holds where the guard must look, and the ones a refusal must
	// name: the host clone's own .envrc and every file the case wrote in the workspace or above it.
	files  []string
	stdout string
	stderr string
	err    error
}

// makeInEnvrcLayout builds a host-clone layout for the case, writes the case's .envrc files with the planted
// credentials beside their lines, and runs target in the host clone. WORKSPACE_REPO names the checkout inside the
// sandbox's workspace, as the host clone's direnv .envrc sets it, and the environment holds invented values for
// the two credentials make vm-env copies, so vm-env and vm-create reach their next step instead of refusing a
// missing credential.
func makeInEnvrcLayout(t *testing.T, target string, tc envrcLayoutCase) envrcRun {
	t.Helper()

	l := newHostLayout(t)

	// The workspace holds the repository's own checkout, which WORKSPACE_REPO names.
	l.workspaceCheckout(t)

	var files []string

	if tc.hostEnvrc != "" {
		files = append(files, writeHostEnvrc(t, l, tc.hostEnvrc))
	}

	if tc.setup != nil {
		files = append(files, tc.setup(t, l)...)
	}

	env := maps.Clone(l.env)

	for _, name := range vmEnvCredentials {
		env[name] = fakeEnvValue(name)
	}

	stdout, stderr, err := runMakeIn(t, l.root, []string{target}, env)

	return envrcRun{layout: l, files: files, stdout: stdout, stderr: stderr, err: err}
}

// assertRefused fails unless the run refused the layout: a non-zero exit, stderr naming every file the guard must
// point the maintainer at and saying what want asks for beyond the paths, no call of a tool a host target reaches
// - the guard ran before anything else - and no planted credential value in either stream.
func (r envrcRun) assertRefused(t *testing.T, target string, want []string) {
	t.Helper()

	out := "make " + target

	if r.err == nil {
		t.Errorf("%s in %s exited 0: it must refuse a layout whose .envrc is not only the host clone's own, because the VM's credentials would be loaded outside the clone and handed to every shell opened in the workspace (issue #402)\nstdout: %s\nstderr: %s",
			out, r.layout.root, r.stdout, r.stderr)
	}

	for _, file := range r.files {
		if !namesAnywhere(r.stderr, pathForms(t, file)) {
			t.Errorf("%s's refusal does not name %s on stderr, so a maintainer cannot tell which file to move into the host clone's .envrc (issue #402)\nstdout: %s\nstderr: %s",
				out, file, r.stdout, r.stderr)
		}
	}

	for _, text := range want {
		if !strings.Contains(r.stderr, text) {
			t.Errorf("%s's refusal does not say %q on stderr, so a maintainer does not learn where the credentials belong: docs/dev-environment.md, The host clone (issue #402)\nstdout: %s\nstderr: %s",
				out, text, r.stdout, r.stderr)
		}
	}

	log := r.layout.sb.argvLogText(t)

	for _, tool := range append(slices.Clone(hostTargetTools), envrcGuardTools...) {
		if calls := invocations(log, tool); len(calls) > 0 {
			t.Errorf("%s ran %s %d time(s), starting with %v, although it must refuse the layout first: the guard runs before any limactl, launchctl, sudo or other command of the target, or a run with a .envrc in the wrong place still reaches the machine with the credentials in reach (issue #402)\nstderr: %s",
				out, tool, len(calls), leadingArgs(calls[0], 2), r.stderr)
		}
	}

	r.assertNoPlantedValue(t, out)
}

// assertPassed fails unless the guard let the layout through: the target reaches its next step - the first tool it
// runs, or the message of its own credential check - no message names a .envrc of the layout, and no planted
// credential value appears in either stream.
func (r envrcRun) assertPassed(t *testing.T, target string) {
	t.Helper()

	out := "make " + target

	skipIfAnEnvrcIsAboveTheSandbox(t, r.layout)

	step, ok := envrcNextSteps[target]
	if !ok {
		t.Fatalf("envrcNextSteps holds no next step for make %s, so this test pins nothing about it", target)
	}

	if step.tool != "" {
		if r.err != nil {
			t.Errorf("%s from the host clone exited non-zero although the layout holds nothing the guard refuses, so the guard stopped a layout docs/dev-environment.md asks for (issue #402): %v\nstdout: %s\nstderr: %s",
				out, r.err, r.stdout, r.stderr)
		}

		if calls := invocations(r.layout.sb.argvLogText(t), step.tool); len(calls) == 0 {
			t.Errorf("%s ran no %s: it must get past the guard and reach its first %s, or the guard stopped a layout the docs ask for (issue #402)\nstdout: %s\nstderr: %s",
				out, step.tool, step.tool, r.stdout, r.stderr)
		}
	} else if !strings.Contains(r.stderr, step.message) {
		t.Errorf("%s's stderr does not hold %q, the message of its own credential check, which it prints once the guard lets it through: the guard must not stop a layout docs/dev-environment.md asks for (issue #402)\nstdout: %s\nstderr: %s",
			out, step.message, r.stdout, r.stderr)
	}

	for _, file := range r.files {
		if named := namesAnywhere(r.stderr, pathForms(t, file)); named {
			t.Errorf("%s's stderr names %s although the layout holds nothing the guard refuses: the refusal belongs to a .envrc read from outside the host clone (issue #402)\nstdout: %s\nstderr: %s",
				out, file, r.stdout, r.stderr)
		}
	}

	r.assertNoPlantedValue(t, out)
}

// assertNoPlantedValue fails when either stream holds one of the two made-up credential values the layout's
// .envrc files hold: the guard reads those files, and a message that echoed a line of one would put a credential
// on the maintainer's terminal and into the log (CLAUDE.md rule 3, issue #402).
func (r envrcRun) assertNoPlantedValue(t *testing.T, out string) {
	t.Helper()

	for _, value := range plantedValues {
		for _, stream := range []struct{ name, text string }{{"stdout", r.stdout}, {"stderr", r.stderr}} {
			if strings.Contains(stream.text, value) {
				t.Errorf("%s printed %q on %s, a value out of a .envrc it read: the guard's messages hold names and paths only, never a value (CLAUDE.md rule 3, issue #402)\n%s:\n%s",
					out, value, stream.name, stream.name, stream.text)
			}
		}
	}
}

// writeHostEnvrc writes the host clone's .envrc with content beside the planted credential lines, and returns its
// path: the file the guard reads first, and the one a refusal of a clone that reads a parent must name.
func writeHostEnvrc(t *testing.T, l *hostLayout, content string) string {
	t.Helper()

	path := filepath.Join(l.root, ".envrc")

	writeFileMode(t, path, []byte(content+plantedEnvrcLines()), 0o600)

	return path
}

// writeAboveWorkspace writes a .envrc- or .env-shaped file holding the planted credential lines in the directory
// levels above the workspace that holds WORKSPACE_REPO: level 1 is the workspace itself, the parent directory of
// WORKSPACE_REPO, and level 2 the directory above it. It returns the file's path.
func writeAboveWorkspace(t *testing.T, l *hostLayout, levels int, name string) string {
	t.Helper()

	dir := l.workspace()

	for level := 1; level < levels; level++ {
		dir = filepath.Dir(dir)
	}

	path := filepath.Join(dir, name)

	writeFileMode(t, path, []byte(plantedEnvrcLines()), 0o600)

	return path
}

// pathForms returns the spellings a message may use for path: the one the test wrote, and the one with the path's
// symlinks resolved, which is what make's $(CURDIR) and git's REPO_ROOT print on macOS, where /var is
// /private/var. A refusal of a path is right in either spelling.
func pathForms(t *testing.T, path string) []string {
	t.Helper()

	forms := []string{path}

	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		forms = append(forms, resolved)
	}

	return forms
}

// namesAnywhere reports whether text holds any of the forms.
func namesAnywhere(text string, forms []string) bool {
	for _, form := range forms {
		if strings.Contains(text, form) {
			return true
		}
	}

	return false
}

// leadingArgs returns the first n arguments of a recorded call, so a failure names the call without printing a
// whole recipe's shell text (make vm-env hands limactl all of it).
func leadingArgs(args []string, n int) []string {
	if len(args) <= n {
		return args
	}

	return append(slices.Clone(args[:n]), "...")
}

// skipIfAnEnvrcIsAboveTheSandbox skips a case the guard must pass when the machine the tests run on keeps a .envrc
// or a .env in a directory above the sandbox, up to /: the guard must refuse such a layout, so the case would pin
// the machine's own files instead of the layout the test built, and fail for a reason that is none of the guard's.
// It reads no file, only whether one is there.
func skipIfAnEnvrcIsAboveTheSandbox(t *testing.T, l *hostLayout) {
	t.Helper()

	for dir := filepath.Dir(l.sb.root); ; {
		for _, name := range []string{".envrc", ".env"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				t.Skipf("this machine keeps %s above the sandbox, which the guard must refuse, so this case cannot pin the layout it built",
					path)
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}

		dir = parent
	}
}
