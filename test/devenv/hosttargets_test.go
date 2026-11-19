// Pins that every host target - every make target that runs on the maintainer's Mac, with the maintainer's
// credentials in reach - runs only from the host clone, the clone outside the workspace the Lima VM mounts
// (docs/adr/0031, docs/dev-environment.md, The host clone), and that make in the workspace refuses one before it
// runs anything: no prerequisite, no sub-make, no limactl, no git config write, no recipe line (issue #227).
//
// The refusal is a guard against the maintainer's mistakes, not a security boundary: the workspace's Makefile is
// agent-editable, so the protection comes from the maintainer running make in the host clone only. What these
// tests pin is what that maintainer sees - the exit status, the message naming the target, the host clone and
// the docs, and that nothing ran - and that the targets the agents run inside the VM keep working in the
// workspace without WORKSPACE_REPO at all.
//
// Every run is a sandbox run: a fake limactl and the fakes of launchctl, sudo, gh, plutil, sleep and systemctl
// record their argv and run nothing of the machine the tests run on, and a recording git lets the Makefile's own
// parse-time git reads through while it shows that no recipe wrote git config. No test reaches a real Lima, a
// real launchd or a real GitHub (CLAUDE.md rule 3).
package devenv_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// hostTargets are the make targets that run on the maintainer's Mac: every target whose recipe calls limactl,
// launchctl or sudo, gate-check, which reads GitHub as the maintainer through a script of dev/, and the three
// targets that run the agents App's token script with the App's key in reach (docs/dev-environment.md, The host
// clone). Each must refuse to run from anywhere in the workspace (issue #227).
// TestHostTargetsAreEveryTargetThatRunsOnTheMac keeps the list from falling behind the Makefile.
var hostTargets = []string{
	"agents-token",
	"agents-token-refresh",
	"agents-token-refresh-install",
	"agents-token-refresh-start",
	"agents-token-refresh-uninstall",
	"deck",
	"deck-restart",
	"gate-check",
	"vm-claude-settings",
	"vm-create",
	"vm-delete",
	"vm-env",
	"vm-env-check",
	"vm-gh-wrapper",
	"vm-init",
	"vm-shell",
	"vm-stop",
	"vm-up",
}

// hostTargetTools are the tools a host target runs as the maintainer: a refused run must reach none of them, and
// the sandbox's fake of each records its argv before it does anything.
var hostTargetTools = []string{"curl", "gh", "launchctl", "limactl", "openssl", "plutil", "sleep", "sudo", "systemctl"}

// hostTargetInstance is the LIMA_INSTANCE the runs work on: the Makefile's default, from the sandbox's baseEnv.
const hostTargetInstance = "shift2eu"

// hostLayout is one sandbox in the layout the maintainer's host has (issue #227, ADR 0031): the workspace the VM
// mounts, which holds the repository's own checkout that WORKSPACE_REPO names, and the host clone make runs in,
// a clone outside that workspace. The sandbox's fakes record every command a run lets through.
type hostLayout struct {
	sb   *sandbox
	env  map[string]string
	root string // the host clone: where a host target may run
}

// newHostLayout builds the layout, with a recording limactl that runs nothing and the recording fakes of the
// other tools a host target reaches.
func newHostLayout(t *testing.T) *hostLayout {
	t.Helper()

	sb := newSandbox(t)
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	// The identity vm-init hands the VM, so a run pins it instead of reading the machine's git config.
	env["GIT_USER_NAME"] = "Test Maintainer"
	env["GIT_USER_EMAIL"] = "maintainer@example.com"

	writeFileMode(t, filepath.Join(sb.binDir, "limactl"), []byte(recordingLimactl), 0o700)
	writeFileMode(t, filepath.Join(sb.binDir, "git"),
		[]byte(fmt.Sprintf(recordingGit, shQuote(realTool(t, "git")))), 0o700)
	sb.useFakeLaunchctl(t, env, "113")
	sb.useLogOnlySudo(t)
	sb.useFakePlutil(t, env, "0")
	sb.useFakeSleep(t)
	sb.useFakeSystemctl(t)

	return &hostLayout{sb: sb, env: env, root: writeClone(t, filepath.Join(sb.root, "host", "shift2eu"))}
}

// workspaceCheckout is the repository's own checkout inside the workspace, which WORKSPACE_REPO names: a clone
// of the repository, as the maintainer's workspace holds one.
func (l *hostLayout) workspaceCheckout(t *testing.T) string {
	t.Helper()

	return writeClone(t, l.sb.workspaceRepo())
}

// workspace is the directory the VM mounts: the parent of the checkout WORKSPACE_REPO names.
func (l *hostLayout) workspace() string {
	return filepath.Dir(l.env["WORKSPACE_REPO"])
}

// recordingGit stands in for git in the host-target tests: it records its argv in the sandbox's log and then
// runs the machine's own git, so the Makefile's parse-time reads (git describe, git rev-parse, git config
// user.name) answer as they do, while a test can prove that no recipe ran `git config core.hooksPath .githooks`
// - the write make git-hooks makes, which vm-up's prerequisite used to make in the checkout make runs in.
const recordingGit = `#!/bin/sh
{
  printf 'tool=git\n'
  for a in "$@"; do printf 'arg=%%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
exec %s "$@"
`

// TestHostTargetsRefuseOutsideTheHostClone pins the refusal (issue #227): run from the workspace - the directory
// the VM mounts - or from a checkout inside it, every host target exits non-zero before anything runs, and says
// on stderr which target refused, that it runs from the host clone, and where that is documented. No fake tool
// recorded a call, vm-up's git-hooks prerequisite wrote no git config and left .git/config as it was, and
// vm-env's vm-env-check prerequisite never ran (its message about the missing credentials is absent).
//
// WORKSPACE_REPO is the make variable the host clone's direnv .envrc sets. Without it, with a relative one, with
// one naming the checkout make runs from, with the checkout inside the workspace it names, and with the
// workspace inside the checkout make runs from, the run must refuse: each of those leaves make's code - which
// the agents can edit - running on the Mac with the maintainer's credentials in reach (ADR 0031).
func TestHostTargetsRefuseOutsideTheHostClone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// layout returns the directory make runs in and sets WORKSPACE_REPO the way the case needs.
		layout func(t *testing.T, l *hostLayout) string
	}{
		{
			name: "WORKSPACE_REPO is unset",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				delete(l.env, "WORKSPACE_REPO")

				return l.workspaceCheckout(t)
			},
		},
		{
			name: "WORKSPACE_REPO is a relative path",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				l.env["WORKSPACE_REPO"] = hostTargetInstance

				return l.workspaceCheckout(t)
			},
		},
		{
			name: "WORKSPACE_REPO is the checkout make runs from",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				l.env["WORKSPACE_REPO"] = l.sb.workspaceRepo()

				return l.workspaceCheckout(t)
			},
		},
		{
			name: "make runs in a checkout inside the workspace",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				l.env["WORKSPACE_REPO"] = filepath.Join(l.sb.workspaceRoot(), "another-clone")

				return l.workspaceCheckout(t)
			},
		},
		{
			name: "the workspace is inside the checkout make runs from",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				l.env["WORKSPACE_REPO"] = filepath.Join(l.root, "ws", hostTargetInstance)

				return l.root
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, target := range hostTargets {
				t.Run(target, func(t *testing.T) {
					t.Parallel()

					l := newHostLayout(t)
					dir := tc.layout(t, l)

					assertHostTargetRefused(t, l, dir, target)
				})
			}
		})
	}
}

// assertHostTargetRefused fails unless make refused target in dir: a non-zero exit, a message naming the target,
// the host clone and the docs on stderr, no tool of the machine touched, no git config written, and none of the
// prerequisites of a host target run.
func assertHostTargetRefused(t *testing.T, l *hostLayout, dir, target string) {
	t.Helper()

	gitConfig := filepath.Join(dir, ".git", "config")
	before := readFileOrEmpty(gitConfig)

	stdout, stderr, err := runMakeIn(t, dir, []string{target}, l.env)
	if err == nil {
		t.Errorf("make %s in %s exited 0: a host target must refuse to run from the workspace, whose Makefile, dev/ and .git/config the agents can edit, and run from the host clone instead (issue #227, docs/dev-environment.md, The host clone)\nstdout: %s\nstderr: %s",
			target, dir, stdout, stderr)
	}

	for _, want := range []string{target, "host clone", "dev-environment.md"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("make %s's stderr does not say %q when it refuses in %s, so a maintainer cannot tell which target refused, that it runs from the host clone, or where that is documented (issue #227)\nstdout: %s\nstderr: %s",
				target, want, dir, stdout, stderr)
		}
	}

	log := l.sb.argvLogText(t)

	for _, tool := range hostTargetTools {
		if calls := invocations(log, tool); len(calls) > 0 {
			t.Errorf("make %s ran %s %v although it refused: the refusal must come before any recipe line, so nothing of the machine the maintainer works on is reached (issue #227)\nstderr: %s",
				target, tool, calls[0], stderr)
		}
	}

	for _, args := range invocations(log, "git") {
		if slices.Contains(args, "core.hooksPath") {
			t.Errorf("make %s ran git %v although it refused: vm-up's git-hooks prerequisite must not run, or the refusal is too late to keep make's code off the maintainer's git config (issue #227)\nstderr: %s",
				target, args, stderr)
		}
	}

	if after := readFileOrEmpty(gitConfig); after != before {
		t.Errorf("make %s changed %s although it refused:\nbefore:\n%s\nafter:\n%s",
			target, gitConfig, before, after)
	}

	// vm-env-check is the prerequisite of vm-env and of vm-create. The sandbox's environment holds none of the
	// credentials it checks, so a check that ran would refuse them in these words: the refusal of the host
	// target must come first (issue #227).
	if strings.Contains(stderr, "not set or empty") {
		t.Errorf("make %s ran vm-env-check although it refused: the run must stop before any prerequisite, or a make -i, -k or a sub-make still reaches the host work the refusal is about (issue #227)\nstderr: %s",
			target, stderr)
	}
}

// TestHostTargetsRunFromTheHostClone pins the other half of issue #227: from the maintainer's layout - make in
// the host clone, WORKSPACE_REPO naming the repository's own checkout inside the workspace - the host targets
// run, and hand limactl the workspace's paths instead of the host clone's: make deck and make vm-shell open the
// VM at WORKSPACE_REPO, make vm-claude-settings and make vm-init are given the workspace and WORKSPACE_REPO, and
// make vm-up mounts the workspace, the parent of WORKSPACE_REPO, rather than a directory of the host clone.
func TestHostTargetsRunFromTheHostClone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target string
		// want is the argv of every limactl call the run makes, in call order.
		want func(l *hostLayout) [][]string
	}{
		{
			name:   "deck opens the VM at the workspace's checkout",
			target: "deck",
			want: func(l *hostLayout) [][]string {
				return [][]string{{"shell", "--workdir", l.env["WORKSPACE_REPO"], hostTargetInstance, "dot-agent-deck"}}
			},
		},
		{
			name:   "vm-shell opens a shell at the workspace's checkout",
			target: "vm-shell",
			want: func(l *hostLayout) [][]string {
				return [][]string{{"shell", "--workdir", l.env["WORKSPACE_REPO"], hostTargetInstance}}
			},
		},
		{
			name:   "vm-claude-settings is given the workspace",
			target: "vm-claude-settings",
			want: func(l *hostLayout) [][]string {
				return [][]string{{"shell", "--workdir", l.env["WORKSPACE_REPO"], hostTargetInstance, "dev/claude-settings.sh", l.workspace()}}
			},
		},
		{
			name:   "vm-init is given the workspace and its checkout",
			target: "vm-init",
			want: func(l *hostLayout) [][]string {
				return [][]string{{
					"shell", "--workdir", l.env["WORKSPACE_REPO"], hostTargetInstance,
					"bash", "-l", "dev/vm-init.sh",
					l.workspace(), l.env["WORKSPACE_REPO"], "Test Maintainer", "maintainer@example.com", "dark",
				}}
			},
		},
		{
			name:   "vm-up mounts the workspace",
			target: "vm-up",
			want: func(l *hostLayout) [][]string {
				return [][]string{
					// The existence check answers that the instance is not listed, so the create line follows.
					{"list", "--quiet"},
					{
						"start", "--name=" + hostTargetInstance, "--mount-only", l.workspace() + ":w",
						"--tty=false", "dev/lima.yaml",
					},
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			l := newHostLayout(t)

			stdout, stderr, err := runMakeIn(t, l.root, []string{tc.target}, l.env)
			if err != nil {
				t.Fatalf("make %s from the host clone refused or failed, although it runs there (issue #227): %v\nstdout: %s\nstderr: %s",
					tc.target, err, stdout, stderr)
			}

			if diff := cmp.Diff(tc.want(l), invocations(l.sb.argvLogText(t), "limactl")); diff != "" {
				t.Errorf("make %s from the host clone ran the wrong limactl calls (-want +got):\n%s\nthe paths it hands the VM are the workspace's, WORKSPACE_REPO and its parent, never the host clone's (issue #227)",
					tc.target, diff)
			}
		})
	}
}

// TestVMTargetsStillRunInTheWorkspace pins that the refusal keeps to the host targets (issue #227): the targets
// the agents run inside the VM work in the workspace, with no WORKSPACE_REPO at all. A refusal that reached them
// would stop every agent's make check, make test and make worktree.
func TestVMTargetsStillRunInTheWorkspace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want []string // substrings the run's output must hold
	}{
		{
			name: "make help",
			args: []string{"help"},
			want: []string{"Show this help", "vm-up", "check"},
		},
		{
			name: "make -n test",
			args: []string{"-n", "test"},
			want: []string{"go test -race -count=1 ./..."},
		},
		{
			name: "make -n worktree",
			args: []string{"-n", "worktree", "BRANCH=feat/12-short-name"},
			want: []string{"dev/worktree.sh", "feat/12-short-name"},
		},
		{
			// git-hooks itself runs in the VM too: the agents' make worktree sets the workspace's hooks there.
			name: "make -n git-hooks",
			args: []string{"-n", "git-hooks"},
			want: []string{"git config core.hooksPath .githooks"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			env := sb.baseEnv()
			delete(env, "WORKSPACE_REPO")

			// The VM targets run in the workspace's checkout, which is where the agents run make.
			stdout, stderr, err := runMakeIn(t, writeClone(t, sb.workspaceRepo()), tc.args, env)
			if err != nil {
				t.Fatalf("make %s in the workspace exited non-zero although it is a target the agents run there, with no WORKSPACE_REPO: %v\nstdout: %s\nstderr: %s",
					strings.Join(tc.args, " "), err, stdout, stderr)
			}

			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("make %s in the workspace does not print %q, so the refusal reached a target that runs in the VM (issue #227):\nstdout: %s\nstderr: %s",
						strings.Join(tc.args, " "), want, stdout, stderr)
				}
			}
		})
	}
}

// TestHostTargetsAreEveryTargetThatRunsOnTheMac keeps hostTargets complete (issue #227): a target whose recipe
// calls limactl, launchctl or sudo runs on the Mac as the maintainer, so it must refuse a run from the workspace
// like the others - a new vm-* or deck target that is forgotten here would otherwise be the one that runs the
// workspace's code with the maintainer's credentials. gate-check, agents-token and agents-token-refresh call no
// such tool themselves, only a script of dev/, and are in the table whatever this scan finds.
func TestHostTargetsAreEveryTargetThatRunsOnTheMac(t *testing.T) {
	t.Parallel()

	recipes := makeRecipes(t, readRepoFile(t, makefilePath))

	for _, name := range slices.Sorted(maps.Keys(recipes)) {
		if !mentionsAny(recipes[name], "limactl", "launchctl", "sudo") {
			continue
		}

		if !slices.Contains(hostTargets, name) {
			t.Errorf("make %s runs limactl, launchctl or sudo on the Mac, but hostTargets in this file does not list it: every host target must refuse a run from the workspace (issue #227)", name)
		}
	}

	for _, target := range hostTargets {
		if _, ok := recipes[target]; !ok {
			t.Errorf("hostTargets lists %s, which the Makefile does not declare as a target, so the lists have drifted apart (issue #227)", target)
		}
	}
}

// makeRecipes returns the recipe of every target body declares, keyed by the target's name: the tab-indented
// lines after its declaration, up to the next declaration, without the commented ones.
func makeRecipes(t *testing.T, body string) map[string]string {
	t.Helper()

	declaration := regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*:(.*)$`)

	recipes := map[string]string{}
	current := ""

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "\t"):
			if current != "" && !strings.HasPrefix(trimmed, "#") {
				recipes[current] += line + "\n"
			}
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			// A blank line or a comment between a target's lines leaves it the target it belongs to.
		default:
			match := declaration.FindStringSubmatch(line)
			current = ""

			// A ':=' or '::=' assignment is a variable, not a target.
			if match != nil && !strings.HasPrefix(match[2], "=") {
				current = match[1]
			}
		}
	}

	return recipes
}

// mentionsAny reports whether text holds any of the words, as a word: "sudo" is in "sudo install" and not in a
// longer name.
func mentionsAny(text string, words ...string) bool {
	for _, word := range words {
		if regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_.-])`).MatchString(text) {
			return true
		}
	}

	return false
}

// readFileOrEmpty is a file's content, and the empty string when it is not there.
func readFileOrEmpty(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	return string(body)
}
