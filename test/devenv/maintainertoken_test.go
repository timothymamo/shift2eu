// Pins PRD #161 milestone 3 (docs/dev-environment.md, "The switch-over runbook" step 6): the maintainer's
// personal token leaves the VM. The agents push as the agents App, and the gh wrapper hands the real gh the
// App's installation token on every call (dev/gh-app-token.sh), so GITHUB_TOKEN is no credential of the agents
// any more: it is not in the Makefile's VM_ENV, `make vm-env-check`, `make vm-env` and `make vm-create` neither
// require nor name it, and the two files the VM is built from - dev/lima.yaml, which embeds the wrapper, and
// dev/vm-init.sh, which checks the credential - do not pass or reference it.
//
// The VM-side runs use the fake limactl of the vm-up tests (vmUpFixture): no test reaches a real Lima, a real
// account or a real token. The one GITHUB_TOKEN value any case holds is invented (CLAUDE.md rules 3 and 4).
package devenv_test

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestMakeVMEnvNamesNoMaintainerToken pins the one place the default is decided: the Makefile's VM_ENV lists
// exactly the credentials make vm-env copies into the VM, and the maintainer's personal token is not one of
// them (PRD #161 milestone 3). vmEnvCredentials is the same list every other devenv test builds its
// environment from, so a name added to the Makefile alone fails here too.
func TestMakeVMEnvNamesNoMaintainerToken(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, makefilePath)

	value, ok := makeVariable(body, "VM_ENV")
	if !ok {
		t.Fatalf("%s has no 'VM_ENV ?=' assignment, so this test cannot pin which credentials make vm-env copies into the VM (issue #234, PRD #161 milestone 3)", makefilePath)
	}

	names := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })

	if diff := cmp.Diff(vmEnvCredentials, names); diff != "" {
		t.Errorf("%s's default VM_ENV names the wrong credentials (-want +got):\n%s\nIt must list exactly the credentials the agents' VM still needs, in this order: the maintainer's personal GITHUB_TOKEN left the VM in PRD #161 milestone 3, when the agents began pushing as the agents App", makefilePath, diff)
	}

	for _, name := range names {
		if name == "GITHUB_TOKEN" {
			t.Errorf("%s's default VM_ENV still names GITHUB_TOKEN: the maintainer's personal token must not enter the VM any more, and nothing reads it there - the gh wrapper fails without the agents App's token file and has no fallback (PRD #161 milestone 3)", makefilePath)
		}
	}
}

// TestMakeVMEnvNeedsNoMaintainerToken pins the two host-side targets that used to require it: with GITHUB_TOKEN
// unset, or set to the empty string, `make vm-env-check` and `make vm-env` run, and with a stale value in the
// environment it never reaches the VM's daemon.env (PRD #161 milestone 3, issue #234). None of them prints it,
// and a run that succeeds writes no line the login shell would still read into the VM.
//
// The VM side runs on this machine through the sandbox's fake limactl, so what a VM user's daemon.env holds
// afterwards is read where make vm-env wrote it (the fake VM user's home) and no VM is needed.
func TestMakeVMEnvNeedsNoMaintainerToken(t *testing.T) {
	t.Parallel()

	stale := fakeEnvValue("GITHUB_TOKEN")

	cases := []struct {
		name string
		args []string
		// github holds the value make's environment gives GITHUB_TOKEN; nil leaves it out of the environment
		// entirely, and the empty string makes it the empty string, which vm-env used to refuse.
		github *string
		// writesDaemonEnv is whether the target runs make vm-env, so the VM's daemon.env exists afterwards.
		writesDaemonEnv bool
	}{
		{name: "vm-env-check with GITHUB_TOKEN unset", args: []string{"vm-env-check"}},
		{name: "vm-env with GITHUB_TOKEN unset", args: []string{"vm-env"}, writesDaemonEnv: true},
		{name: "vm-env with GITHUB_TOKEN set to an empty string", args: []string{"vm-env"}, github: ptrTo(""), writesDaemonEnv: true},
		{
			name:            "vm-env with a stale GITHUB_TOKEN in the environment",
			args:            []string{"vm-env"},
			github:          ptrTo(stale),
			writesDaemonEnv: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()

			if tc.github == nil {
				delete(env, "GITHUB_TOKEN")
			} else {
				env["GITHUB_TOKEN"] = *tc.github
			}

			stdout, stderr, err := runMake(t, tc.args, env)
			if err != nil {
				t.Fatalf("make %s exited non-zero although the agents no longer need the maintainer's token: the VM is set up without GITHUB_TOKEN (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), stdout, stderr)
			}

			text := stdout + stderr

			if strings.Contains(text, "GITHUB_TOKEN") {
				t.Errorf("make %s names GITHUB_TOKEN although it is no credential of the agents any more: the target must neither require it nor ask a maintainer for it (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), stdout, stderr)
			}

			if tc.github != nil && *tc.github != "" {
				assertNoSecrets(t, "make "+strings.Join(tc.args, " ")+"'s output", text,
					[]secret{{kind: "the stale GITHUB_TOKEN it was given", value: *tc.github}})
			}

			if !tc.writesDaemonEnv {
				return
			}

			body, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("make %s wrote no daemon.env in the VM: %v", strings.Join(tc.args, " "), readErr)
			}

			if lines := sortedEnvLines(string(body)); containsPrefixed(lines, "GITHUB_TOKEN=") {
				t.Errorf("make %s wrote a GITHUB_TOKEN line into the VM's daemon.env: the login shell sources that file, so the maintainer's personal token would sit in the VM and every agent it starts (PRD #161 milestone 3). The lines it wrote: %v",
					strings.Join(tc.args, " "), lines)
			}
		})
	}
}

// TestMakeVMCreateNeedsNoMaintainerToken pins the rebuild that used to stop on the maintainer's token: with
// GITHUB_TOKEN unset, and with it set to the empty string, make vm-create passes its credential check, names
// GITHUB_TOKEN nowhere, and reaches limactl start - so an unattended rebuild no longer stops on a credential the
// agents do not use (issue #258, PRD #161 milestone 3).
func TestMakeVMCreateNeedsNoMaintainerToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		github *string // nil leaves GITHUB_TOKEN out of make's environment
	}{
		{name: "GITHUB_TOKEN unset"},
		{name: "GITHUB_TOKEN set to an empty string", github: ptrTo("")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			if tc.github == nil {
				delete(f.env, "GITHUB_TOKEN")
			} else {
				f.env["GITHUB_TOKEN"] = *tc.github
			}

			stdout, stderr, err := f.run(t, "vm-create")
			if err != nil {
				t.Fatalf("make vm-create exited non-zero with %s: the credential check runs before vm-up, so a token the agents no longer use must not stop a rebuild that can otherwise finish (issue #258, PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					tc.name, stdout, stderr)
			}

			if strings.Contains(stdout+stderr, "GITHUB_TOKEN") {
				t.Errorf("make vm-create names GITHUB_TOKEN although it is no credential of the agents any more: the rebuild must neither require it nor ask a maintainer for it (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if starts := f.starts(t); len(starts) == 0 {
				t.Errorf("make vm-create never reached limactl start: the credential check refused a VM it has no reason to refuse, so an unattended rebuild stops on a token the agents do not use (issue #258, PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// TestTheVMFilesNameNoMaintainerToken pins the two files the VM is built from, whose text a maintainer reads
// before running them: dev/lima.yaml, which embeds dev/gh-app-token.sh and installs it as the VM's gh, and
// dev/vm-init.sh, whose gh step checked the token. Neither may pass GITHUB_TOKEN to the VM or tell a maintainer
// to set it (PRD #161 milestone 3). dev/gh-app-token.sh itself is pinned by behaviour, in ghtwrapper_test.go,
// and by dev/lima.yaml, whose embedded copy must stay identical to it (limaghwrapper_test.go).
func TestTheVMFilesNameNoMaintainerToken(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"dev/lima.yaml", vmInitScript} {
		body := readRepoFile(t, file)

		for i, line := range strings.Split(body, "\n") {
			if strings.Contains(line, "GITHUB_TOKEN") {
				t.Errorf("%s:%d names GITHUB_TOKEN: the maintainer's personal token leaves the VM - the gh wrapper has no fallback to it and make vm-env no longer copies it in - so nothing here may pass it on or ask a maintainer to set it (PRD #161 milestone 3): %s",
					file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// ptrTo returns a pointer to s, for a case that hands make's environment a value it must read as set.
func ptrTo(s string) *string {
	return &s
}

// containsPrefixed reports whether any line starts with prefix.
func containsPrefixed(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}
