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
	"time"

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

// workspaceRepo is the canonical path of the checkout inside the workspace, the value WORKSPACE_REPO names:
// a case may write it with a trailing slash or with a .. component, and the paths a run hands limactl are this
// one either way.
func (l *hostLayout) workspaceRepo() string {
	return l.sb.workspaceRepo()
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
// one naming the checkout make runs from, with the checkout inside the workspace it names, with the workspace
// inside the checkout make runs from, and with one reached through a symlink into the workspace, the run must
// refuse: each of those leaves make's code - which the agents can edit - running on the Mac with the maintainer's
// credentials in reach (ADR 0031). A workspace of / refuses too: every checkout is inside it. A value the
// recipes could not paste inside quotes is refused before it is expanded - whitespace, $, a quote, a backslash,
// %, and a : (Lima splits --mount-only on ':') - and the message describes what a value may hold instead of
// echoing it, which would put a credential typed where a path goes on the terminal. A , and a ~ are refused the
// same way: Lima's --mount-only is a pflag StringSlice, which splits the value on commas and expands a leading
// ~ of every part, so /Users/x/ws,~/shift2eu would mount the home directory read-write while the workspace was
// checked. A relative value is refused without being echoed either: it is no path, and may be a credential
// typed where a path goes.
//
// The refusal is make code that runs while make reads this file, so no make flag and no way of writing the goal
// may carry a host target past it: make -k, make -i and make -n, a command line with another goal beside the
// host target, .DEFAULT_GOAL on the command line, which leaves MAKECMDGOALS empty, and MAKECMDGOALS= on the
// command line, which empties it, all stop before the first recipe line. So does a goal the environment hides:
// MAKECMDGOALS set in the environment, which make reads in place of the goals of its command line, and
// .DEFAULT_GOAL set there, which make -e runs as its only goal in the workspace and in the host clone alike.
// Nothing of git is needed for it either: the refusal holds on a PATH without git, where REPO_ROOT falls back to
// CURDIR.
func TestHostTargetsRefuseOutsideTheHostClone(t *testing.T) {
	t.Parallel()

	// workspaceWithoutRepo is the layout of a maintainer who runs make in the workspace's own checkout with no
	// WORKSPACE_REPO at all: the checkout make runs in is the one the agents work in.
	workspaceWithoutRepo := func(t *testing.T, l *hostLayout) string {
		t.Helper()

		delete(l.env, "WORKSPACE_REPO")

		return l.workspaceCheckout(t)
	}

	// workspaceRepoHolding sets WORKSPACE_REPO to value, for a run in the workspace's checkout.
	workspaceRepoHolding := func(value func(l *hostLayout) string) func(t *testing.T, l *hostLayout) string {
		return func(t *testing.T, l *hostLayout) string {
			t.Helper()

			dir := l.workspaceCheckout(t)
			l.env["WORKSPACE_REPO"] = value(l)

			return dir
		}
	}

	// hostClone is the layout make accepts: make in the host clone, WORKSPACE_REPO naming the repository's
	// checkout in the workspace - and, when value is set, WORKSPACE_REPO held to that value instead. Neither
	// where make runs nor where the value points refuses a run here, so a case using it pins what make does
	// about the value itself and about the goals alone (issue #227).
	hostClone := func(value func(l *hostLayout) string) func(t *testing.T, l *hostLayout) string {
		return func(t *testing.T, l *hostLayout) string {
			t.Helper()

			l.workspaceCheckout(t)

			if value != nil {
				l.env["WORKSPACE_REPO"] = value(l)
			}

			return l.root
		}
	}

	// What the refusal of a value the recipes could not paste inside quotes must say, and what it must no longer
	// say: the old message named whitespace and a list of characters, which says nothing about what a value may
	// hold, and about a path nothing can be learned from it.
	const (
		notAllowed    = "a character other than letters, digits and"
		wasTheOldWord = "whitespace or one of"
	)

	cases := []refusalCase{
		{
			name:   "WORKSPACE_REPO is unset",
			layout: workspaceWithoutRepo,
		},
		{
			name:   "WORKSPACE_REPO is a relative path",
			layout: workspaceRepoHolding(func(*hostLayout) string { return hostTargetInstance }),
		},
		{
			name:   "WORKSPACE_REPO is the checkout make runs from",
			layout: workspaceRepoHolding(func(l *hostLayout) string { return l.workspaceRepo() }),
		},
		{
			name: "the workspace is reached through a symlink from the checkout make runs in",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				dir := l.workspaceCheckout(t)
				link := filepath.Join(l.sb.root, "ws-link")

				if err := os.Symlink(l.sb.workspaceRoot(), link); err != nil {
					t.Fatalf("symlink %s to %s: %v", link, l.sb.workspaceRoot(), err)
				}

				l.env["WORKSPACE_REPO"] = filepath.Join(link, hostTargetInstance)

				return dir
			},
		},
		{
			name:   "make runs in a checkout inside the workspace",
			layout: workspaceRepoHolding(func(l *hostLayout) string { return filepath.Join(l.sb.workspaceRoot(), "another-clone") }),
		},
		{
			name: "the workspace is inside the checkout make runs from",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				l.env["WORKSPACE_REPO"] = filepath.Join(l.root, "ws", hostTargetInstance)

				return l.root
			},
		},
		{
			name:   "the workspace is the root directory",
			layout: workspaceRepoHolding(func(*hostLayout) string { return "/x" }),
		},
		{
			name:    "WORKSPACE_REPO holds a space",
			layout:  workspaceRepoHolding(func(l *hostLayout) string { return filepath.Join(l.sb.workspaceRoot(), "shift 2eu") }),
			want:    []string{notAllowed},
			notWant: []string{wasTheOldWord, "shift 2eu"},
		},
		{
			name:    "WORKSPACE_REPO holds a $",
			layout:  workspaceRepoHolding(func(l *hostLayout) string { return filepath.Join(l.sb.workspaceRoot(), "$x") }),
			want:    []string{notAllowed},
			notWant: []string{wasTheOldWord, "$x"},
		},
		{
			name:    "WORKSPACE_REPO holds a colon, which Lima splits a --mount-only value on",
			layout:  workspaceRepoHolding(func(l *hostLayout) string { return filepath.Join(l.sb.workspaceRoot(), "shift2eu:1") }),
			want:    []string{notAllowed},
			notWant: []string{wasTheOldWord, "shift2eu:1"},
		},
		{
			// Lima's --mount-only is a pflag StringSlice: it splits the value on commas, so a comma lets one
			// value mount a second directory, which the workspace check here never sees (issue #227).
			name:    "WORKSPACE_REPO holds a comma, which splits a --mount-only value in two",
			layout:  hostClone(func(l *hostLayout) string { return filepath.Join(l.sb.workspaceRoot(), "shift2eu,1") }),
			args:    []string{"vm-up"},
			goal:    "vm-up",
			want:    []string{notAllowed},
			notWant: []string{wasTheOldWord, "shift2eu,1"},
		},
		{
			// The same goes for a leading ~, which pflag expands per part: a value holding one mounts the home
			// directory read-write, with the maintainer's credentials in reach (issue #227).
			name:    "WORKSPACE_REPO holds a tilde, which Lima expands to a home directory",
			layout:  hostClone(func(l *hostLayout) string { return filepath.Join(l.sb.workspaceRoot(), "~", "shift2eu") }),
			args:    []string{"vm-up"},
			goal:    "vm-up",
			want:    []string{notAllowed},
			notWant: []string{wasTheOldWord, "~/shift2eu"},
		},
		{
			// A relative value is no path, so the refusal says so without printing it: the maintainer may have
			// typed a credential where a path goes, and the terminal keeps what it is shown (issue #227).
			name:    "WORKSPACE_REPO is relative, which its refusal must not echo",
			layout:  hostClone(func(*hostLayout) string { return "fake-not-a-path-value" }),
			args:    []string{"vm-up"},
			goal:    "vm-up",
			want:    []string{"WORKSPACE_REPO", "absolute path"},
			notWant: []string{"fake-not-a-path-value"},
		},
		{
			name:   "make -k",
			layout: workspaceWithoutRepo,
			flags:  []string{"-k"},
		},
		{
			name:   "make -i",
			layout: workspaceWithoutRepo,
			flags:  []string{"-i"},
		},
		{
			name:   "make -n",
			layout: workspaceWithoutRepo,
			flags:  []string{"-n"},
		},
		{
			name:        "make check vm-up, with another goal beside the host target",
			layout:      workspaceWithoutRepo,
			args:        []string{"check", "vm-up"},
			goal:        "vm-up",
			emptyStdout: true,
		},
		{
			name:        "make .DEFAULT_GOAL=vm-up, which make runs as its only goal",
			layout:      workspaceWithoutRepo,
			args:        []string{".DEFAULT_GOAL=vm-up"},
			goal:        "vm-up",
			emptyStdout: true,
		},
		{
			name:        "make MAKECMDGOALS= vm-up, which empties the goals make reads",
			layout:      workspaceWithoutRepo,
			args:        []string{"MAKECMDGOALS=", "vm-up"},
			goal:        "vm-up",
			emptyStdout: true,
		},
		{
			name: "make with no git on the PATH",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				dir := l.workspaceCheckout(t)
				l.env["WORKSPACE_REPO"] = l.workspaceRepo()
				l.env["PATH"] = pathWithout(t, "git")

				return dir
			},
		},
	}

	// The goals the environment hides from make (issue #227): MAKECMDGOALS holds the goals make runs, so a value
	// of it from anywhere but the command line leaves the real goal - vm-up here - unseen, and .DEFAULT_GOAL from
	// the environment is the goal make -e runs instead of this file's help. A shell cannot set a name holding a
	// dot, so a run sets .DEFAULT_GOAL the way it sets MAKECMDGOALS: through the environment of the process.
	// Every one of them must be refused where make runs - in the workspace, where the host targets must never
	// run, and in the host clone, where make cannot know the goal either.
	hiddenGoals := []struct {
		name string
		env  map[string]string
		args []string
		// says is the variable whose environment value hid the goals, which the refusal must name.
		says string
	}{
		{
			name: "MAKECMDGOALS= in the environment",
			env:  map[string]string{"MAKECMDGOALS": ""},
			args: []string{"vm-up"},
			says: "MAKECMDGOALS",
		},
		{
			name: "MAKECMDGOALS=help in the environment",
			env:  map[string]string{"MAKECMDGOALS": "help"},
			args: []string{"vm-up"},
			says: "MAKECMDGOALS",
		},
		{
			name: "MAKECMDGOALS=x in the environment, which make -e honours",
			env:  map[string]string{"MAKECMDGOALS": "x"},
			args: []string{"-e", "vm-up"},
			says: "MAKECMDGOALS",
		},
		{
			name: ".DEFAULT_GOAL=vm-up in the environment, which make -e runs",
			env:  map[string]string{".DEFAULT_GOAL": "vm-up"},
			args: []string{"-e"},
			says: ".DEFAULT_GOAL",
		},
	}

	// The layouts those runs must be refused in: the workspace's own checkout, and the host clone.
	where := []struct {
		name   string
		layout func(t *testing.T, l *hostLayout) string
	}{
		{
			name: "in the workspace",
			layout: func(t *testing.T, l *hostLayout) string {
				t.Helper()

				return l.workspaceCheckout(t)
			},
		},
		{
			name:   "in the host clone",
			layout: hostClone(nil),
		},
	}

	for _, hidden := range hiddenGoals {
		for _, layout := range where {
			cases = append(cases, refusalCase{
				name:        "goals hidden by " + hidden.name + ", " + layout.name,
				layout:      layout.layout,
				env:         hidden.env,
				args:        hidden.args,
				want:        []string{hidden.says},
				emptyStdout: true,
			})
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, run := range tc.runs() {
				t.Run(run.name, func(t *testing.T) {
					t.Parallel()

					l := newHostLayout(t)
					dir := tc.layout(t, l)

					// The layout's environment is the maintainer's own; the run may set more of it, such as the
					// MAKECMDGOALS or the .DEFAULT_GOAL make also reads from the environment.
					for name, value := range run.env {
						l.env[name] = value
					}

					assertHostTargetRefused(t, l, dir, run)
				})
			}
		})
	}
}

// refusalCase is one reason the refusal fires (issue #227): a layout that sets WORKSPACE_REPO the way the case
// needs and the directory make runs in, and either make's own flags - every host target runs with them - or one
// whole command line, with what the refusal's message must and must not add.
type refusalCase struct {
	name string
	// layout returns the directory make runs in and sets WORKSPACE_REPO the way the case needs.
	layout func(t *testing.T, l *hostLayout) string
	// env holds the variables the run sets in the process environment beyond the layout's own, such as a
	// MAKECMDGOALS or a .DEFAULT_GOAL the run hides its goals behind.
	env map[string]string
	// flags are make's own flags, placed before the goal.
	flags []string
	// args, when set, is the whole command line of a single run, whose goal's refusal the case pins with goal.
	args []string
	goal string
	// want and notWant are what the refusal's stderr must and must not say beyond the goal, the host clone and
	// the docs.
	want    []string
	notWant []string
	// emptyStdout says the run prints nothing on stdout: no recipe line, no prerequisite and no other goal of
	// its command line ran.
	emptyStdout bool
}

// runs is the runs the case makes: one whole command line, or every host target with the case's flags.
func (tc refusalCase) runs() []refusedRun {
	if tc.args != nil {
		return []refusedRun{{
			name:        strings.Join(tc.args, " "),
			args:        tc.args,
			goal:        tc.goal,
			env:         tc.env,
			want:        tc.want,
			notWant:     tc.notWant,
			emptyStdout: tc.emptyStdout,
		}}
	}

	runs := make([]refusedRun, 0, len(hostTargets))

	for _, target := range hostTargets {
		runs = append(runs, refusedRun{
			name:        target,
			args:        append(slices.Clone(tc.flags), target),
			goal:        target,
			env:         tc.env,
			want:        tc.want,
			notWant:     tc.notWant,
			emptyStdout: tc.emptyStdout,
		})
	}

	return runs
}

// refusedRun is one run of a refusal case: make's arguments, the environment the run hides its goals in, the
// goal the refusal must name, and what its message must and must not say beyond that.
type refusedRun struct {
	name string
	args []string
	// goal is the goal the refusal's message must name. It is empty for a run whose goals the environment
	// hides: make never sees them, so the message names the variable that hid them instead (run.want).
	goal        string
	env         map[string]string
	want        []string
	notWant     []string
	emptyStdout bool
}

// assertHostTargetRefused fails unless make refused the run in dir: a non-zero exit, a message naming the goal,
// the host clone and the docs on stderr, no tool of the machine touched, no git config written, and none of the
// prerequisites of a host target run.
func assertHostTargetRefused(t *testing.T, l *hostLayout, dir string, run refusedRun) {
	t.Helper()

	gitConfig := filepath.Join(dir, ".git", "config")
	before := readFileOrEmpty(gitConfig)
	name := "make " + strings.Join(run.args, " ")

	stdout, stderr, err := runMakeIn(t, dir, run.args, l.env)
	if err == nil {
		t.Errorf("%s in %s exited 0: a host target must refuse to run from the workspace, whose Makefile, dev/ and .git/config the agents can edit, and run from the host clone instead (issue #227, docs/dev-environment.md, The host clone)\nstdout: %s\nstderr: %s",
			name, dir, stdout, stderr)
	}

	named := []string{"host clone", "dev-environment.md"}
	if run.goal != "" {
		named = append(named, run.goal)
	}

	for _, want := range append(named, run.want...) {
		if !strings.Contains(stderr, want) {
			t.Errorf("%s's stderr does not say %q when it refuses in %s, so a maintainer cannot tell which target refused, that it runs from the host clone, where that is documented, or what a WORKSPACE_REPO may hold (issue #227)\nstdout: %s\nstderr: %s",
				name, want, dir, stdout, stderr)
		}
	}

	for _, forbidden := range run.notWant {
		if strings.Contains(stderr, forbidden) {
			t.Errorf("%s's refusal says %q on stderr: it must not echo the value, which may be a credential typed where a path goes, and it must describe what a value may hold (issue #227)\nstdout: %s\nstderr: %s",
				name, forbidden, stdout, stderr)
		}
	}

	if run.emptyStdout && strings.TrimSpace(stdout) != "" {
		t.Errorf("%s printed on stdout although it refused:\n%s\nThe refusal comes while make reads this file, so no other goal of the command line, no prerequisite and no recipe line ran (issue #227)",
			name, stdout)
	}

	log := l.sb.argvLogText(t)

	for _, tool := range hostTargetTools {
		if calls := invocations(log, tool); len(calls) > 0 {
			t.Errorf("%s ran %s %v although it refused: the refusal must come before any recipe line, so nothing of the machine the maintainer works on is reached (issue #227)\nstderr: %s",
				name, tool, calls[0], stderr)
		}
	}

	for _, args := range invocations(log, "git") {
		if slices.Contains(args, "core.hooksPath") {
			t.Errorf("%s ran git %v although it refused: vm-up's git-hooks prerequisite must not run, or the refusal is too late to keep make's code off the maintainer's git config (issue #227)\nstderr: %s",
				name, args, stderr)
		}
	}

	if after := readFileOrEmpty(gitConfig); after != before {
		t.Errorf("%s changed %s although it refused:\nbefore:\n%s\nafter:\n%s",
			name, gitConfig, before, after)
	}

	// vm-env-check is the prerequisite of vm-env and of vm-create. The sandbox's environment holds none of the
	// credentials it checks, so a check that ran would refuse them in these words: the refusal of the host
	// target must come first (issue #227).
	if strings.Contains(stderr, "not set or empty") {
		t.Errorf("%s ran vm-env-check although it refused: the run must stop before any prerequisite, or a make -i, -k or a sub-make still reaches the host work the refusal is about (issue #227)\nstderr: %s",
			name, stderr)
	}
}

// TestHostTargetsRunFromTheHostClone pins the other half of issue #227: from the maintainer's layout - make in
// the host clone, WORKSPACE_REPO naming the repository's own checkout inside the workspace - the host targets
// run, and hand limactl the workspace's paths instead of the host clone's: make deck and make vm-shell open the
// VM at WORKSPACE_REPO, make vm-claude-settings and make vm-init are given the workspace and WORKSPACE_REPO, and
// make vm-up mounts the workspace, the parent of WORKSPACE_REPO, rather than a directory of the host clone.
//
// The value may be written in more than one way and must still name the same paths: a trailing slash and a ..
// component resolve to the checkout inside the workspace, never to a path of the host clone and never to
// something the shell or make reads as two words.
func TestHostTargetsRunFromTheHostClone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target string
		// setup changes WORKSPACE_REPO before the run, for a value the Makefile must resolve to the same paths.
		setup func(t *testing.T, l *hostLayout)
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
			name:   "WORKSPACE_REPO ends with a slash",
			target: "deck",
			setup: func(t *testing.T, l *hostLayout) {
				t.Helper()

				l.env["WORKSPACE_REPO"] = l.workspaceRepo() + "/"
			},
			want: func(l *hostLayout) [][]string {
				return [][]string{{"shell", "--workdir", l.workspaceRepo(), hostTargetInstance, "dot-agent-deck"}}
			},
		},
		{
			name:   "WORKSPACE_REPO spells a .. component",
			target: "deck",
			setup: func(t *testing.T, l *hostLayout) {
				t.Helper()

				repo := l.workspaceRepo()
				l.env["WORKSPACE_REPO"] = filepath.Dir(repo) + "/no-such-directory/../" + filepath.Base(repo)
			},
			want: func(l *hostLayout) [][]string {
				return [][]string{{"shell", "--workdir", l.workspaceRepo(), hostTargetInstance, "dot-agent-deck"}}
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

			// The workspace holds the repository's own checkout, which WORKSPACE_REPO names: vm-up checks that
			// it is a directory holding .git before it mounts the workspace (issue #227).
			l.workspaceCheckout(t)

			if tc.setup != nil {
				tc.setup(t, l)
			}

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

// TestMakeVMUpRefusesAWorkspaceRepoThatIsNoCheckout pins issue #227's check of the value vm-up mounts: the
// workspace is mounted read-write, so WORKSPACE_REPO must be the repository's own checkout inside it - a
// directory holding .git - and the workspace must be listable, because the stray-entry check reads it before the
// VM is created. A vm-up that carried on would mount a directory that is no checkout, or compare against a
// listing it never got: `ls ... || true` swallowed that, and a stray entry went unseen into the VM. The refusal
// comes before any limactl call, so nothing is mounted, created or started, and it names WORKSPACE_REPO and the
// directory to fix.
func TestMakeVMUpRefusesAWorkspaceRepoThatIsNoCheckout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// workspaceRepo builds what vm-up reads and returns the value and the directories stderr may name for
		// it: the checkout it wants and the workspace that holds it.
		workspaceRepo func(t *testing.T, f *vmUpFixture) (string, []string)
	}{
		{
			name: "WORKSPACE_REPO names a directory without .git",
			workspaceRepo: func(t *testing.T, f *vmUpFixture) (string, []string) {
				t.Helper()

				dir := filepath.Join(f.sb.root, "ws-without-a-checkout", "a-checkout")

				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}

				return dir, []string{dir}
			},
		},
		{
			name: "WORKSPACE_REPO names no directory at all",
			workspaceRepo: func(t *testing.T, f *vmUpFixture) (string, []string) {
				t.Helper()

				path := filepath.Join(f.sb.root, "ws-with-a-file", "a-checkout")
				writeFileMode(t, path, []byte("not a checkout\n"), 0o600)

				return path, []string{path}
			},
		},
		{
			name: "the workspace cannot be listed",
			workspaceRepo: func(t *testing.T, f *vmUpFixture) (string, []string) {
				t.Helper()

				// The workspace is a file: ls has nothing to list, and no checkout can be inside it.
				workspace := filepath.Join(f.sb.root, "workspace-is-a-file")
				writeFileMode(t, workspace, []byte("not a workspace\n"), 0o600)

				path := filepath.Join(workspace, "a-checkout")

				return path, []string{path, workspace}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			repo, paths := tc.workspaceRepo(t, f)
			f.env["WORKSPACE_REPO"] = repo

			stdout, stderr, err := f.run(t, "vm-up")
			if err == nil {
				t.Errorf("make vm-up with WORKSPACE_REPO=%s exited 0: the workspace is mounted read-write, so the value must be a directory holding .git inside it, and the workspace must be listable before anything is mounted or started (issue #227)\nstdout: %s\nstderr: %s",
					repo, stdout, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) > 0 {
				t.Errorf("make vm-up with WORKSPACE_REPO=%s called limactl although it must refuse the value first: no VM may be created, mounted or started from a directory vm-up has not checked (issue #227):%s\nstderr: %s",
					repo, describeCalls(calls), stderr)
			}

			named := false

			for _, path := range paths {
				if strings.Contains(stderr, path) {
					named = true
				}
			}

			if !strings.Contains(stderr, "WORKSPACE_REPO") || !named {
				t.Errorf("make vm-up with WORKSPACE_REPO=%s says nothing about WORKSPACE_REPO and %s on stderr, so a maintainer cannot tell which value to fix (issue #227)\nstdout: %s\nstderr: %s",
					repo, strings.Join(paths, " or "), stdout, stderr)
			}
		})
	}
}

// TestMakeVMUpMatchesTheRepositoryNameLiterally pins vm-up's stray-entry check (issue #227, issue #237): the
// workspace may hold only the repository whose name is WORKSPACE_REPO's last component, and that repository's
// dispatch worktrees. The name is matched as it is, never as a grep pattern, so a repository called a.b does not
// take a stray axb for itself - the workspace check would pass and the VM would mount axb read-write - while
// a.b's own dispatch worktrees (a.b-dispatch-<unit>) still pass.
func TestMakeVMUpMatchesTheRepositoryNameLiterally(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry string
		stray bool // the entry is none of the repository's, so vm-up refuses it
	}{
		{name: "a stray entry the repository's dotted name would match as a pattern", entry: "axb", stray: true},
		{name: "a dispatch worktree of the dotted repository", entry: "a.b-dispatch-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newVMUpFixture(t, "", "")

			// The workspace holds one repository, whose name carries a dot: only it and its dispatch worktrees
			// belong beside it.
			workspace := filepath.Join(f.sb.root, "ws-dotted")
			f.env["WORKSPACE_REPO"] = writeClone(t, filepath.Join(workspace, "a.b"))

			if err := os.MkdirAll(filepath.Join(workspace, tc.entry), 0o700); err != nil {
				t.Fatalf("create %s: %v", filepath.Join(workspace, tc.entry), err)
			}

			stdout, stderr, err := f.run(t, "vm-up")

			if !tc.stray {
				if err != nil {
					t.Fatalf("make vm-up with %s in the workspace exited non-zero: a repository's own dispatch worktrees belong beside it, so the check must allow them (issue #227): %v\nstdout: %s\nstderr: %s",
						tc.entry, err, stdout, stderr)
				}

				if !slices.Contains(f.creating(t), workspace+":w") {
					t.Errorf("make vm-up with %s in the workspace does not mount %s:w, so the dispatch worktree check does not hold (issue #227):\nstdout: %s\nstderr: %s",
						tc.entry, workspace, stdout, stderr)
				}

				return
			}

			if err == nil {
				t.Errorf("make vm-up with %s in the workspace exited 0: the repository is called a.b, so axb is no entry of its, and a pattern match would mount it read-write into the VM (issue #227)\nstdout: %s\nstderr: %s",
					tc.entry, stdout, stderr)
			}

			if !strings.Contains(stderr, tc.entry) {
				t.Errorf("make vm-up's refusal does not name %s on stderr, so a maintainer does not know which entry to move out of the workspace (issue #227)\nstdout: %s\nstderr: %s",
					tc.entry, stdout, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "limactl"); len(calls) > 0 {
				t.Errorf("make vm-up called limactl although the workspace holds the stray entry %s: the check runs before anything is mounted or started (issue #227):%s\nstderr: %s",
					tc.entry, describeCalls(calls), stderr)
			}
		})
	}
}

// TestMakeVMCreateHandsItsSubMakesTheWorkspaceRepo pins that WORKSPACE_REPO reaches every sub-make of vm-create
// (issue #227): vm-create runs make vm-up, make vm-env, make vm-claude-settings and make vm-init, and each works
// on the workspace through that value. The fixture's environment names one workspace and the make command line
// another, the way a maintainer passes it on the command line, so a sub-make that fell back to the environment
// would mount, and hand the VM's scripts, the wrong directory.
func TestMakeVMCreateHandsItsSubMakesTheWorkspaceRepo(t *testing.T) {
	t.Parallel()

	f := newVMUpFixture(t, "", "")

	// The workspace the make command line names: a checkout of its own, beside the one the fixture's
	// environment names.
	repo := writeClone(t, filepath.Join(f.sb.root, "ws-from-the-command-line", "shift2eu"))
	workspace := filepath.Dir(repo)

	f.env["GIT_USER_NAME"] = "Test Maintainer"
	f.env["GIT_USER_EMAIL"] = "maintainer@example.com"

	stdout, stderr, err := f.run(t, "vm-create", "WORKSPACE_REPO="+repo)
	if err != nil {
		t.Fatalf("make vm-create WORKSPACE_REPO=%s: %v\nstdout: %s\nstderr: %s", repo, err, stdout, stderr)
	}

	if got := f.creating(t); !slices.Contains(got, workspace+":w") {
		t.Errorf("make vm-create WORKSPACE_REPO=%s mounts %v, not %s:w: its make vm-up must get the value from the command line, not the fixture's other workspace, or the VM mounts a directory the maintainer does not work in (issue #227)",
			repo, got, workspace)
	}

	callWith := func(script string) []string {
		t.Helper()

		for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
			if slices.Contains(args, script) {
				return args
			}
		}

		t.Fatalf("make vm-create ran no limactl call for %s, so the step it belongs to never ran:\nstdout: %s\nstderr: %s", script, stdout, stderr)

		return nil
	}

	if diff := cmp.Diff(
		[]string{"shell", "--workdir", repo, f.env["LIMA_INSTANCE"], "dev/claude-settings.sh", workspace},
		callWith("dev/claude-settings.sh"),
	); diff != "" {
		t.Errorf("make vm-create WORKSPACE_REPO=%s gives make vm-claude-settings the wrong arguments (-want +got):\n%s\nthe step must be given the workspace and its checkout the maintainer named (issue #227)", repo, diff)
	}

	if diff := cmp.Diff(
		[]string{
			"shell", "--workdir", repo, f.env["LIMA_INSTANCE"], "bash", "-l", "dev/vm-init.sh",
			workspace, repo, "Test Maintainer", "maintainer@example.com", "dark",
		},
		callWith("dev/vm-init.sh"),
	); diff != "" {
		t.Errorf("make vm-create WORKSPACE_REPO=%s gives make vm-init the wrong arguments (-want +got):\n%s\nvm-init trusts the workspace, sets the workspace checkout's git hooks and sets the deck's default_dir to its second argument, so both must be the paths the maintainer named (issue #227)", repo, diff)
	}
}

// TestHostTargetsStripCredentialsTheProcessDoesNotNeed pins issue #227's part in what a host process is given:
// what runs on the Mac runs with the maintainer's environment, so every process a host target starts must get
// only the credentials its own work needs. The deck and the VM's setup commands never need the agents App's
// key, its path or its IDs - they only ever hand the VM a token - and the background token refresher never needs
// the two credentials vm-env copies into the VM. The fakes record whether a name is set, never a value
// (limactlEnvProbe), so a failure here never prints a credential.
func TestHostTargetsStripCredentialsTheProcessDoesNotNeed(t *testing.T) {
	t.Parallel()

	// The App's variables stay on the Mac: no process of the VM's gets them.
	appNames := []string{
		"SHIFT2EU_AGENTS_APP_ID",
		"SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		"SHIFT2EU_AGENTS_APP_KEY_FILE",
	}

	// The two credentials vm-env copies into the VM, which nothing the refresher does needs.
	vmEnvNames := []string{"CLAUDE_CODE_OAUTH_TOKEN", "DEEPSEEK_API_KEY"}

	for _, target := range []string{"deck", "deck-restart", "vm-claude-settings", "vm-init", "vm-shell"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			l := newHostLayout(t)

			// Obviously fake values, never a credential of the machine (CLAUDE.md rule 3): what the test pins
			// is that the names are not passed on, not their values.
			l.env["SHIFT2EU_AGENTS_APP_ID"] = "fake-app-id"
			l.env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = "fake-installation-id"
			l.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = "/home/tester/invented/agents-app.pem"

			if _, stderr, err := runMakeIn(t, l.root, []string{target}, l.env); err != nil {
				t.Fatalf("make %s from the host clone: %v\nstderr: %s", target, err, stderr)
			}

			log := l.sb.argvLogText(t)

			if calls := invocations(log, "limactl"); len(calls) == 0 {
				t.Fatalf("make %s ran no limactl call, so this test pins nothing about what the VM's process is given", target)
			}

			for _, name := range envNamesSet(t, log) {
				if slices.Contains(appNames, name) {
					t.Errorf("make %s left %s in the environment of the process it runs in the VM: the App's key, its path and its IDs stay on the Mac, and the VM only ever gets the one-hour token make agents-token writes (issue #227, ADR 0031)",
						target, name)
				}
			}
		})
	}

	t.Run("agents-token-refresh-start's background refresh", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		// The two credentials a shell that ran direnv holds, with obviously fake values: the refresh needs the
		// App's three variables, never these (CLAUDE.md rule 3).
		f.env["CLAUDE_CODE_OAUTH_TOKEN"] = "fake-oauth-token"
		f.env["DEEPSEEK_API_KEY"] = "fake-deepseek-key"

		if _, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env); err != nil {
			t.Fatalf("make agents-token-refresh-start: %v\nstderr: %s", err, stderr)
		}

		waitFor(t, 60*time.Second, "the refresher make agents-token-refresh-start started to reach the VM", func() bool {
			return len(invocations(f.sb.argvLogText(t), "limactl")) > 0
		})

		for _, name := range envNamesSet(t, f.sb.argvLogText(t)) {
			if slices.Contains(vmEnvNames, name) {
				t.Errorf("the agents App token refresh that make agents-token-refresh-start started in the background holds %s in its environment: the refresher mints a token and writes it into the VM, and needs neither credential, which the VM gets from make vm-env alone (issue #227, CLAUDE.md rule 3)",
					name)
			}
		}
	})
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
			// git-hooks itself runs in the VM too: vm-init sets the workspace checkout's hooks there (issue #227).
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

// hostTargetRecipeWords are the tools that make a recipe a host target's, whatever else it does (issue #227):
// the tools it runs as the maintainer - limactl, launchctl, sudo, gh with the maintainer's credentials, curl and
// openssl with the App's key, plutil on a plist root installs.
var hostTargetRecipeWords = []string{"limactl", "launchctl", "sudo", "gh", "curl", "openssl", "plutil"}

// hostTargetRecipeScripts are the scripts of dev/ that a recipe runs on the Mac: the token script with the App's
// key in reach, the gate check, which reads GitHub as the maintainer, and the gh wrapper's generator.
var hostTargetRecipeScripts = []string{"dev/agents-", "dev/protect-main-check.sh", "dev/vm-gh-wrapper.sh"}

// runsOnTheMac reports whether a recipe runs one of the tools a host target runs, as a word, or names one of the
// scripts of dev/ that work on the Mac.
func runsOnTheMac(recipe string) bool {
	if mentionsAny(recipe, hostTargetRecipeWords...) {
		return true
	}

	for _, script := range hostTargetRecipeScripts {
		if strings.Contains(recipe, script) {
			return true
		}
	}

	return false
}

// TestHostTargetsAreEveryTargetThatRunsOnTheMac keeps hostTargets complete (issue #227): a target whose recipe
// calls limactl, launchctl, sudo, gh, curl, openssl or plutil, or runs one of the scripts of dev/ that work on
// the Mac, runs there as the maintainer, so it must refuse a run from the workspace like the others - a new
// vm-* or deck target that is forgotten here would otherwise be the one that runs the workspace's code with the
// maintainer's credentials. So must a target whose recipe runs a host target as a sub-make: make vm-create runs
// make vm-up, make vm-env and make vm-init, and a host target reached only that way is still a host target.
func TestHostTargetsAreEveryTargetThatRunsOnTheMac(t *testing.T) {
	t.Parallel()

	recipes := makeRecipes(t, readRepoFile(t, makefilePath))

	for _, name := range slices.Sorted(maps.Keys(recipes)) {
		if !runsOnTheMac(recipes[name]) {
			continue
		}

		if !slices.Contains(hostTargets, name) {
			t.Errorf("make %s runs one of %s or %s on the Mac, but hostTargets in this file does not list it: every host target must refuse a run from the workspace (issue #227)",
				name, strings.Join(hostTargetRecipeWords, ", "), strings.Join(hostTargetRecipeScripts, ", "))
		}
	}

	// A host target a sub-make runs is a host target: its sub-make refuses too, with its own goal (issue #227).
	for _, name := range slices.Sorted(maps.Keys(recipes)) {
		for _, target := range hostTargets {
			if !strings.Contains(recipes[name], "$(MAKE) "+target) {
				continue
			}

			if !slices.Contains(hostTargets, name) {
				t.Errorf("make %s runs make %s, a host target, but hostTargets in this file does not list it: every host target must refuse a run from the workspace, whether make or a sub-make starts it (issue #227)",
					name, target)
			}
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
