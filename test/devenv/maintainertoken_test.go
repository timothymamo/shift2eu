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
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// gitHubTokens are the variables a maintainer's personal GitHub token can arrive in: the two the gh CLI reads
// (GITHUB_TOKEN and GH_TOKEN) and their GitHub Enterprise Cloud spellings. None is a credential of the agents
// any more (PRD #161 milestone 3): the VM's gh wrapper hands the real gh the agents App's token and unsets these
// before it runs it (dev/gh-app-token.sh), so nothing here may copy one into the VM or keep a stale one there.
var gitHubTokens = []string{"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_ENTERPRISE_TOKEN"}

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

	// The subtest names say GITHUB_TOKEN although the fixture echoes the t.TempDir() paths it puts them in: the
	// check below strips those paths first, so it reads what the run says about the token, never its own test's
	// temporary directory.
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

			if strings.Contains(withoutTempPaths(stdout+stderr, f.sb.root), "GITHUB_TOKEN") {
				t.Errorf("make vm-create names GITHUB_TOKEN although it is no credential of the agents any more: the rebuild must neither require it nor ask a maintainer for it (PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if starts := f.starts(t); len(starts) == 0 {
				t.Errorf("make vm-create never reached limactl start: the credential check refused a VM it has no reason to refuse, so an unattended rebuild stops on a token the agents do not use (issue #258, PRD #161 milestone 3)\nstdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// TestMakeVMEnvRefusesAGitHubToken pins that no target that copies credentials into the VM accepts one of the
// GitHub token names in VM_ENV (PRD #161 milestone 3): an `make vm-env VM_ENV=CLAUDE_CODE_OAUTH_TOKEN,
// DEEPSEEK_API_KEY,GITHUB_TOKEN` on the command line is refused before the VM is touched - non-zero, a message
// that names the variable and points at the agents App's token, never the variable's value, no limactl call and
// the VM's daemon.env byte for byte as it was.
func TestMakeVMEnvRefusesAGitHubToken(t *testing.T) {
	t.Parallel()

	targets := []struct {
		name string
		args []string
	}{
		{name: "vm-env-check", args: []string{"vm-env-check"}},
		{name: "vm-env", args: []string{"vm-env"}},
	}

	for _, target := range targets {
		for _, token := range gitHubTokens {
			t.Run(target.name+" refuses a VM_ENV that names "+token, func(t *testing.T) {
				t.Parallel()

				sb := newSandbox(t)
				env := sb.vmEnvEnv()

				// The value is invented and set, so the refusal cannot be the one a missing credential gets: make
				// must refuse the name itself, and it must never print what the shell held.
				value := fakeEnvValue(token)
				env[token] = value

				seeded := sb.seedDaemonEnv(t)

				vmEnv := strings.Join(vmEnvCredentials, ",") + "," + token

				stdout, stderr, err := runMake(t, append(slices.Clone(target.args), "VM_ENV="+vmEnv), env)
				if err == nil {
					t.Fatalf("make %s exited 0 with VM_ENV=%s: naming %s must be refused, so the maintainer's personal token cannot be copied into the VM (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
						target.name, vmEnv, token, stdout, stderr)
				}

				// The refusal happens on the host, before anything reaches the VM: no limactl call, so nothing is
				// created and no line is written (the daemon.env below is unchanged).
				sb.assertNoLimaCalls(t, "VM_ENV names "+token)

				text := stdout + stderr

				if !strings.Contains(text, token) {
					t.Errorf("make %s's refusal does not name %s, so a maintainer cannot tell which credential it refused (PRD #161 milestone 3):\nstdout: %s\nstderr: %s",
						target.name, token, stdout, stderr)
				}

				assertPointsAtTheAppToken(t, "make "+target.name+"'s refusal of "+token, text)

				assertNoSecrets(t, "make "+target.name+"'s refusal", text, []secret{{kind: "the value of " + token + " it was given", value: value}})

				got, readErr := os.ReadFile(sb.daemonEnvPath())
				if readErr != nil {
					t.Fatalf("read the VM's daemon.env after a refused make %s: %v", target.name, readErr)
				}

				if diff := cmp.Diff(string(seeded), string(got)); diff != "" {
					t.Errorf("make %s rewrote the VM's daemon.env although it refused %s: the file must be byte for byte what it was (PRD #161 milestone 3) (-want +got):\n%s",
						target.name, token, diff)
				}
			})
		}
	}
}

// TestMakeVMCreateRefusesAGitHubToken pins the same refusal on the target that builds a VM from scratch: make
// vm-create runs vm-env-check before any limactl call, so a VM_ENV that names a GitHub token creates nothing at
// all rather than a VM that holds the maintainer's personal token (PRD #161 milestone 3).
func TestMakeVMCreateRefusesAGitHubToken(t *testing.T) {
	t.Parallel()

	for _, token := range gitHubTokens {
		t.Run("vm-create refuses a VM_ENV that names "+token, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			value := fakeEnvValue(token)
			f.env[token] = value

			vmEnv := strings.Join(vmEnvCredentials, ",") + "," + token

			stdout, stderr, err := f.run(t, "vm-create", "VM_ENV="+vmEnv)
			if err == nil {
				t.Fatalf("make vm-create exited 0 with VM_ENV=%s: naming %s must be refused before the VM is created (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					vmEnv, token, stdout, stderr)
			}

			if starts := f.starts(t); len(starts) != 0 {
				t.Errorf("make vm-create reached limactl start with VM_ENV=%s: the check must run first, so a rebuild that would copy %s into the VM creates none (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					vmEnv, token, stdout, stderr)
			}

			text := stdout + stderr

			if !strings.Contains(text, token) {
				t.Errorf("make vm-create's refusal does not name %s, so a maintainer cannot tell which credential it refused (PRD #161 milestone 3):\nstdout: %s\nstderr: %s",
					token, stdout, stderr)
			}

			assertPointsAtTheAppToken(t, "make vm-create's refusal of "+token, text)

			assertNoSecrets(t, "make vm-create's refusal", text, []secret{{kind: "the value of " + token + " it was given", value: value}})

			// Nothing was created, so nothing was written either: a daemon.env holds no line for the refused token.
			if body, readErr := os.ReadFile(f.sb.daemonEnvPath()); readErr == nil && containsPrefixed(sortedEnvLines(string(body)), token+"=") {
				t.Errorf("make vm-create wrote a %s line into the VM although it refused it: the check runs before vm-env, so a refused run writes no credential (PRD #161 milestone 3). The lines it wrote: %v",
					token, sortedEnvLines(string(body)))
			}
		})
	}
}

// TestMakeVMEnvMergeDropsAGitHubToken pins the other way a token can reach the VM: a daemon.env a previous
// maintainer (or an older Makefile) filled with GITHUB_TOKEN=... and its spellings. A narrowed `make vm-env
// VM_ENV=DEEPSEEK_API_KEY` merges - it keeps the plain NAME=value lines it does not list - so it must drop these
// four: the rewritten daemon.env no longer holds them, the other lines it keeps stay as they were, and the run
// still exits 0 (PRD #161 milestone 3).
//
// The drop is not silent (PRD #161 milestone 3, reviewer and auditor): the line sat in the VM's daemon.env,
// which the login shell sources, so every agent the VM started could read the maintainer's personal token.
// make must therefore name the variable it dropped and say that the token is to be revoked - and it must do so
// without ever printing the value, which is the credential itself. A daemon.env that holds no such line gets no
// such note.
func TestMakeVMEnvMergeDropsAGitHubToken(t *testing.T) {
	t.Parallel()

	// One of the names VM_ENV still lists (PRD #161 milestone 3), so the merge has something to replace and the
	// line it keeps is pinned next to the ones it drops.
	const narrowed = "DEEPSEEK_API_KEY"

	// mergeCase is one daemon.env a narrowed vm-env meets: one holding a stale line of token, or - with token
	// empty - the two credentials of a VM that never held a GitHub token at all.
	type mergeCase struct {
		name  string
		token string
	}

	cases := make([]mergeCase, 0, len(gitHubTokens)+1)
	for _, token := range gitHubTokens {
		cases = append(cases, mergeCase{name: "a merge drops a stale " + token, token: token})
	}

	cases = append(cases, mergeCase{name: "a merge of a daemon.env with no GitHub token says nothing about one"})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.vmEnvEnv()
			delete(env, "CLAUDE_CODE_OAUTH_TOKEN")

			stale := ""
			seeded := []string{}

			if tc.token != "" {
				stale = fakeEnvValue(tc.token)
				seeded = append(seeded, tc.token+"="+stale)
			}

			sb.seedDaemonEnvWith(t, seeded...)

			stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=" + narrowed}, env)
			if err != nil {
				t.Fatalf("make vm-env VM_ENV=%s with a daemon.env that holds %q: %v\nstdout: %s\nstderr: %s",
					narrowed, tc.token, err, stdout, stderr)
			}

			body, readErr := os.ReadFile(sb.daemonEnvPath())
			if readErr != nil {
				t.Fatalf("read the VM's daemon.env after make vm-env VM_ENV=%s: %v", narrowed, readErr)
			}

			lines := sortedEnvLines(string(body))

			for _, token := range gitHubTokens {
				if containsPrefixed(lines, token+"=") {
					t.Errorf("make vm-env VM_ENV=%s kept the %s line it found in the VM's daemon.env: a merge must drop a GitHub token, so the login shell that sources the file never hands one to an agent (PRD #161 milestone 3). The lines it wrote: %v",
						narrowed, token, lines)
				}
			}

			want := []string{
				"CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
				narrowed + "=" + fakeEnvValue(narrowed),
			}

			if diff := cmp.Diff(sortedEnvLines(strings.Join(want, "\n")), lines); diff != "" {
				t.Errorf("make vm-env VM_ENV=%s wrote the wrong lines to the VM's daemon.env: it must drop a GitHub token line and keep every other plain line the file held (PRD #161 milestone 3) (-want +got):\n%s",
					narrowed, diff)
			}

			text := stdout + stderr

			if tc.token == "" {
				for _, token := range gitHubTokens {
					if strings.Contains(text, token) {
						t.Errorf("make vm-env VM_ENV=%s dropped no GitHub token line - the VM's daemon.env held none - yet it names %s: a note about a token that was not there sends a maintainer to revoke a credential the VM never had (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
							narrowed, token, stdout, stderr)
					}
				}

				if strings.Contains(strings.ToLower(text), "revoke") {
					t.Errorf("make vm-env VM_ENV=%s dropped no GitHub token line - the VM's daemon.env held none - yet its output talks about revoking a token: the note must appear only when a line was really dropped, or a maintainer learns to ignore it (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
						narrowed, stdout, stderr)
				}

				return
			}

			if !strings.Contains(text, tc.token) {
				t.Errorf("make vm-env VM_ENV=%s dropped the stale %s line from the VM's daemon.env without telling the maintainer: that line sat where the login shell of every agent the VM starts read it, so make must name the variable it dropped (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					narrowed, tc.token, stdout, stderr)
			}

			if !strings.Contains(strings.ToLower(text), "revoke") {
				t.Errorf("make vm-env VM_ENV=%s dropped the stale %s line from the VM's daemon.env without saying to revoke that token: every agent in the VM could read it, so removing the line is not enough - the maintainer must revoke it at GitHub (PRD #161 milestone 3)\nstdout: %s\nstderr: %s",
					narrowed, tc.token, stdout, stderr)
			}

			assertNoSecrets(t, "make vm-env VM_ENV="+narrowed+"'s output", text, []secret{
				{kind: "the stale " + tc.token + " line's value", value: stale},
			})
		})
	}
}

// assertPointsAtTheAppToken fails when a refusal of one of the GitHub token names does not point a maintainer at
// the credential the agents do use: the agents App's token, which make agents-token writes into the VM. The
// wording around these words is not pinned, that a refusal sends a maintainer there is.
func assertPointsAtTheAppToken(t *testing.T, where, text string) {
	t.Helper()

	if !strings.Contains(text, "App") {
		t.Errorf("%s does not point at the agents App's token, so a maintainer whose gh needs a credential is sent to a token the agents no longer use: %s", where, text)
	}

	if !strings.Contains(text, "agents-token") && !strings.Contains(text, "agents-app-token") {
		t.Errorf("%s does not name make agents-token or the App's token file, the one the agents' gh reads, so a maintainer cannot tell which credential replaces the one it refused: %s", where, text)
	}
}

// withoutTempPaths removes the temporary directories a run echoes from text. t.TempDir() puts the subtest's name
// in them, so a check that a run never names a variable (GITHUB_TOKEN, say) would otherwise trip on its own
// test's paths: what the run says is the subject, where the sandbox happens to live is not. The resolved path is
// removed too, because git reports a symlinked temporary directory by its real path.
func withoutTempPaths(text string, roots ...string) string {
	for _, root := range roots {
		if root == "" {
			continue
		}

		text = strings.ReplaceAll(text, root, "<temp>")

		if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
			text = strings.ReplaceAll(text, real, "<temp>")
		}
	}

	return text
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
			if !strings.Contains(line, "GITHUB_TOKEN") || onlyUnsetsTheToken(line) {
				continue
			}

			t.Errorf("%s:%d names GITHUB_TOKEN: the maintainer's personal token leaves the VM - the gh wrapper has no fallback to it and make vm-env no longer copies it in - so nothing here may pass it on or ask a maintainer to set it (PRD #161 milestone 3): %s",
				file, i+1, strings.TrimSpace(line))
		}
	}
}

// onlyUnsetsTheToken reports whether line is a shell `unset` statement whose only effect on the maintainer's
// token is to drop it from the environment. dev/gh-app-token.sh unsets GITHUB_TOKEN (and its own token) before
// running the real gh, so a stale variable inherited from the caller never reaches it - defence in depth, pinned
// by TestGHAppTokenHandsTheAppTokenToTheRealGH - and dev/lima.yaml embeds that wrapper byte for byte, so the
// same line appears there. Any other use - exporting, reading, checking, listing it as a credential, falling
// back to it - is still a mention these files may not make.
func onlyUnsetsTheToken(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "unset" {
		return false
	}

	unsetsToken := false

	for _, name := range fields[1:] {
		if !isShellName(name) {
			return false
		}

		if name == "GITHUB_TOKEN" {
			unsetsToken = true
		}
	}

	return unsetsToken
}

// isShellName reports whether s is a plain shell variable name, which is all an `unset` may list.
func isShellName(s string) bool {
	if s == "" {
		return false
	}

	for i, r := range s {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}

	return true
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
