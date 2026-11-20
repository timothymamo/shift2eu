// Pins the Makefile's part in the agents App's credential (PRD #161 milestone 1): VM_ENV names no
// SHIFT2EU_AGENTS_APP_* variable, make vm-env refuses one a maintainer adds rather than copying it into the
// VM, make agents-token / make agents-token-refresh run the mint and the refresh, make
// agents-token-refresh-start starts one refresher per VM in the background from the host clone and warns about
// the instance's LaunchDaemon - and about a loaded job that runs another clone's script - make
// agents-token-refresh-install installs the LaunchDaemon after checking the instance's mounts and linting the
// plist, make agents-token-refresh-uninstall removes it, make deck runs from the host clone with the App's
// variables in its environment without saying anything about them and starts nothing, and make vm-gh-wrapper
// installs the pinned wrapper an existing VM lacks, whole or not at all (.dot-agent-deck/prd161m1-fixes.md items
// 3, 7 and 8 and .dot-agent-deck/prd161m1-fixes2.md items 1 and 2, issue #229; issue #227 for what make deck does
// with the App's variables now that it runs from the host clone).
package devenv_test

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// agentsAppPrefix is the prefix of the three inputs dev/agents-app-token.sh reads from the maintainer's
// direnv .envrc: the App ID, the installation ID and the path of the App's key. None may enter the VM.
const agentsAppPrefix = "SHIFT2EU_AGENTS_APP_"

// A script of dev/ is run by its path and shebang, never through an explicit `sh` or a leading `./`, which ignore
// the shebang and need no executable bit (issues #242 and #337). Both matches are at a word boundary, so another
// command's `foo.sh dev/x.sh` argument is no run of a dev script. The convention has one exception, and the matcher
// is why it is not flagged: the vm-init recipe runs the script inside the VM as `bash -l dev/vm-init.sh`, because
// only a login shell loads the credentials vm-env wrote to /etc/profile.d/shift2eu.sh (Makefile, make vm-init), and
// the script's own #!/bin/sh would not load them. So the matcher covers `sh` and `./` and deliberately not `bash`:
// a `bash dev/x.sh` anywhere else would not be caught here either, which is the price of that one allowed line.
var (
	devScriptThroughShell    = regexp.MustCompile(`(^|[^[:alnum:]_.-])sh +dev/`)
	devScriptThroughDotSlash = regexp.MustCompile(`(^|[^[:alnum:]_.-])\./dev/`)
)

// devScriptsRunThroughShellOrPath returns the lines of text that run a script of dev/ through `sh` or `./`, in
// order: make -n output and recipe bodies are both checked line by line.
func devScriptsRunThroughShellOrPath(text string) []string {
	var lines []string

	for _, line := range strings.Split(text, "\n") {
		if devScriptThroughShell.MatchString(line) || devScriptThroughDotSlash.MatchString(line) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}

	return lines
}

// devScriptName matches a script of dev/ that a recipe names: a path below dev/ that ends in .sh.
var devScriptName = regexp.MustCompile(`(^|[^[:alnum:]_.-])(dev/[[:alnum:]_./-]+\.sh)`)

// devScriptsIn returns the scripts of dev/ that text names, sorted and each once: every script a recipe runs, since
// a script of dev/ is run by its path.
func devScriptsIn(text string) []string {
	scripts := map[string]bool{}

	for _, match := range devScriptName.FindAllStringSubmatch(text, -1) {
		scripts[match[2]] = true
	}

	return slices.Sorted(maps.Keys(scripts))
}

// TestDevScriptsRunThroughShellOrPathMatchesWholeCommands pins the matcher the two checks below use (issues #242
// and #337): `sh dev/x.sh` and `./dev/x.sh` are a dev script run the wrong way, where another script's
// `foo.sh dev/x.sh` argument and a run by path are not.
func TestDevScriptsRunThroughShellOrPathMatchesWholeCommands(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "a script through sh",
			line: "\tsh dev/worktree.sh \"$(BRANCH)\"",
			want: []string{`sh dev/worktree.sh "$(BRANCH)"`},
		},
		{
			name: "a script through sh in a command substitution",
			line: "\t@script=\"$$(sh dev/vm-gh-wrapper.sh dev/gh-app-token.sh)\" || exit 1; \\",
			want: []string{`@script="$$(sh dev/vm-gh-wrapper.sh dev/gh-app-token.sh)" || exit 1; \`},
		},
		{
			name: "a script through sh with more than one space",
			line: "\tcd x && sh  dev/vm-agents-token-ssh.sh --uninstall",
			want: []string{"cd x && sh  dev/vm-agents-token-ssh.sh --uninstall"},
		},
		{
			name: "a path relative to the current directory",
			line: "\t@./dev/worktree.sh \"$(BRANCH)\"",
			want: []string{`@./dev/worktree.sh "$(BRANCH)"`},
		},
		{
			name: "another script's argument is no run",
			line: "\tfoo.sh dev/worktree.sh",
		},
		{
			name: "a run by path",
			line: "\tdev/worktree.sh \"$(BRANCH)\"",
		},
		{
			name: "a script below another directory",
			line: "\t/bin/sh \"$$clone/dev/agents-token-launchd.sh\" plist",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, devScriptsRunThroughShellOrPath(tc.line)); diff != "" {
				t.Errorf("devScriptsRunThroughShellOrPath(%q) (-want +got):\n%s", tc.line, diff)
			}
		})
	}
}

func TestMakefileVMENVNamesNoAgentsAppVariable(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, makefilePath)

	value, ok := makeVariable(body, "VM_ENV")
	if !ok {
		t.Fatalf("%s has no 'VM_ENV ?=' assignment", makefilePath)
	}

	names := strings.Split(value, ",")
	if len(names) == 0 || strings.TrimSpace(value) == "" {
		t.Fatalf("VM_ENV's default is empty: vm-env copies the variables it lists from your shell into the VM")
	}

	for _, name := range names {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, agentsAppPrefix) {
			t.Errorf("VM_ENV's default names %s: the App's key and its path stay on the Mac, and only the installation token enters the VM (PRD #161 milestone 1)", name)
		}
	}
}

// TestMakeVMEnvRefusesAnAgentsAppVariable pins the second half: a maintainer who adds an App variable to
// VM_ENV, or passes one on the command line, gets a refusal and not a copy of the key's path into the VM.
func TestMakeVMEnvRefusesAnAgentsAppVariable(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)

	env := sb.baseEnv()
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = "/home/tester/invented/agents-app.pem"

	stdout, stderr, err := runMake(t, []string{"vm-env", "VM_ENV=SHIFT2EU_AGENTS_APP_KEY_FILE"}, env)
	if err == nil {
		t.Fatalf("make vm-env exited 0 with VM_ENV=SHIFT2EU_AGENTS_APP_KEY_FILE: vm-env must refuse an App variable, so neither the key nor its path enters the VM\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if log := sb.argvLogText(t); log != "" {
		t.Errorf("make vm-env called limactl before it refused an App variable, so something may have entered the VM:\n%s", log)
	}

	if !strings.Contains(stdout+stderr, "SHIFT2EU_AGENTS_APP_KEY_FILE") {
		t.Errorf("the refusal does not name SHIFT2EU_AGENTS_APP_KEY_FILE, so a maintainer cannot tell what vm-env refused:\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if _, err := os.Stat(sb.vmPath(".config/dot-agent-deck/daemon.env")); err == nil {
		t.Errorf("make vm-env wrote the VM's ~/.config/dot-agent-deck/daemon.env although it refused an App variable")
	}
}

// TestMakefileHasTheAgentsTokenTargets pins the command lines of the agents-token targets (issue #242): the
// scripts of dev/ are run by their path and shebang, as every other dev script the Makefile runs is, never through
// an explicit `sh`, which ignores the script's shebang and its executable bit. The background start keeps its
// `env -u ... nohup` prefix, so the refresh it starts has the same command line as the one started by hand.
func TestMakefileHasTheAgentsTokenTargets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		target string
		want   string
	}{
		{target: "agents-token", want: "dev/agents-app-token.sh mint"},
		{target: "agents-token-refresh", want: "dev/agents-app-token.sh refresh"},
		// The background start the maintainer runs from the host clone (second review, item 1): the `nohup`
		// prefix stays, and only the `sh ` in front of the script goes (issue #242).
		{target: "agents-token-refresh-start", want: "nohup dev/agents-app-token.sh refresh"},
		// The plist generator the install runs is a script of dev/ too, run the same way (issue #242).
		{target: "agents-token-refresh-install", want: "dev/agents-token-launchd.sh plist"},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := runMake(t, []string{"-n", tc.target}, makeEnv(t, nil))
			if err != nil {
				t.Fatalf("make -n %s: %v\nstderr: %s", tc.target, err, stderr)
			}

			if !strings.Contains(stdout, tc.want) {
				t.Errorf("make -n %s does not run %q:\n%s", tc.target, tc.want, stdout)
			}

			// Running a script through `sh` or `./` reads no shebang and needs no executable bit, so a recipe that
			// still does it runs whatever interpreter the maintainer's `sh` is, and a change to the script's shebang
			// never reaches it (issues #242 and #337). The match is at a word boundary, so another command's
			// `foo.sh dev/x` argument is no offender.
			if lines := devScriptsRunThroughShellOrPath(stdout); len(lines) != 0 {
				t.Errorf("make -n %s runs a script of dev/ through `sh` or `./`, which ignores its shebang and executable bit: run it by its path, as the other dev scripts are (issues #242 and #337):\n%s",
					tc.target, strings.Join(lines, "\n"))
			}
		})
	}
}

// TestMakefileRunsNoDevScriptThroughShOrDotSlash pins the convention across every recipe at once, over the recipes
// themselves rather than their make -n output, so a target no other test names is held to it too: no recipe runs a
// script of dev/ through an explicit `sh` or a leading `./`, which ignore the script's shebang and executable bit
// (issues #242 and #337). The LaunchDaemon's own `/bin/sh dev/agents-app-token.sh refresh` program argument is not a
// recipe and is left as issue #229 pins it.
func TestMakefileRunsNoDevScriptThroughShOrDotSlash(t *testing.T) {
	t.Parallel()

	recipes := makeRecipes(t, readRepoFile(t, makefilePath))

	// Only a broken makeRecipes trips this, never a Makefile change on its own: it is kept so that such a parser
	// cannot make the test pass on nothing (issue #396, item 6).
	if len(recipes) == 0 {
		t.Fatal("the Makefile declares no target with a recipe, so this test pins nothing")
	}

	for _, name := range slices.Sorted(maps.Keys(recipes)) {
		if lines := devScriptsRunThroughShellOrPath(recipes[name]); len(lines) != 0 {
			t.Errorf("the recipe of make %s runs a script of dev/ through `sh` or `./`, which ignores its shebang and executable bit: run it by its path, as every dev script the Makefile runs is (issues #242 and #337):\n%s",
				name, strings.Join(lines, "\n"))
		}
	}
}

// TestEveryDevScriptARecipeRunsIsExecutableInGit pins the other half of the convention (issues #242 and #337): a
// script of dev/ is run by its path and shebang, so the executable bit git checks it out with is what makes that run
// work. Only git's record counts, not the working tree's, so a mode changed in a checkout without `git update-index
// --chmod=+x` fails here.
func TestEveryDevScriptARecipeRunsIsExecutableInGit(t *testing.T) {
	t.Parallel()

	modes := gitFileModes(t, "dev")

	recipes := makeRecipes(t, readRepoFile(t, makefilePath))

	for _, name := range slices.Sorted(maps.Keys(recipes)) {
		for _, script := range devScriptsIn(recipes[name]) {
			mode, tracked := modes[script]
			if !tracked {
				t.Errorf("the recipe of make %s names %s, which git does not track: a script a recipe runs is committed, and executable (issues #242 and #337)",
					name, script)

				continue
			}

			if mode != "100755" {
				t.Errorf("the recipe of make %s names %s, which git holds as mode %s, not 100755: the recipe runs it by its path and shebang, so git must check it out executable (issues #242 and #337)",
					name, script, mode)
			}
		}
	}

	// The scripts the convention was adopted for, by name: make vm-gh-wrapper and make worktree run these by path.
	for _, script := range []string{"dev/vm-gh-wrapper.sh", "dev/worktree.sh"} {
		if mode := modes[script]; mode != "100755" {
			t.Errorf("%s is mode %s in git, not 100755: make vm-gh-wrapper and make worktree run it by its path and shebang, which needs the executable bit (issues #242 and #337)",
				script, mode)
		}
	}
}

// gitFileModes returns the mode git has recorded for the files below dir, keyed by their path from the repository
// root: git ls-files reads the index, so the test needs no network and no particular working-tree mode.
func gitFileModes(t *testing.T, dir string) map[string]string {
	t.Helper()

	cmd := exec.Command("git", "ls-files", "-s", "--", dir)
	cmd.Dir = moduleRoot

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files -s -- %s: %v: the test reads the modes git recorded", dir, err)
	}

	modes := map[string]string{}

	for _, line := range strings.Split(string(out), "\n") {
		// 100755 <object> <stage>\t<path>
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}

		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}

		modes[path] = fields[0]
	}

	return modes
}

// TestMakeVMGHWrapperInstallsThePinnedWrapper pins make vm-gh-wrapper (review item 3): a VM created before the
// wrapper existed has the real gh on /usr/local/bin/gh, so the target moves it off the PATH, installs the
// pinned wrapper above it and points the git credential helper at the wrapper, idempotently, with the wrapper
// going over standard input and never onto a command line.
func TestMakeVMGHWrapperInstallsThePinnedWrapper(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"

	capture := filepath.Join(sb.root, "lima-stdin.log")
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = capture

	t.Run("make -n shows the wrapper going over standard input", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runMake(t, []string{"-n", "vm-gh-wrapper"}, env)
		if err != nil {
			t.Fatalf("make -n vm-gh-wrapper: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "gh-app-token.sh") {
			t.Errorf("make -n vm-gh-wrapper does not read dev/%s, the wrapper every VM must run:\n%s", ghWrapperScript, stdout)
		}

		// An agent can read every command line in the VM: the wrapper's text must travel on stdin.
		for _, line := range strings.Split(readRepoFile(t, ghWrapperScript), "\n") {
			line = strings.TrimSpace(line)

			if line == "" || strings.HasPrefix(line, "#") || len(line) < 12 {
				continue
			}

			if strings.Contains(stdout, line) {
				t.Errorf("make -n vm-gh-wrapper puts the wrapper's line %q on a command line; the recipe must hand the wrapper over standard input (review item 3):\n%s", line, stdout)
			}
		}
	})

	t.Run("make vm-gh-wrapper installs the pinned wrapper and keeps doing so", func(t *testing.T) {
		t.Parallel()

		// The fake limactl runs nothing, so no provision command of this recipe touches the machine the
		// tests run on; what it records is what the VM would get.
		runOnce := func() string {
			t.Helper()

			if err := os.Remove(capture); err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("remove %s: %v", capture, err)
			}

			_, stderr, err := runMake(t, []string{"vm-gh-wrapper"}, env)
			if err != nil {
				t.Fatalf("make vm-gh-wrapper: %v\nstderr: %s", err, stderr)
			}

			body, err := os.ReadFile(capture)
			if err != nil {
				t.Fatalf("make vm-gh-wrapper called limactl nothing, or handed it no standard input: %v", err)
			}

			return string(body)
		}

		first := runOnce()
		second := runOnce()

		if first != second {
			t.Errorf("make vm-gh-wrapper gives the VM different input on a second run, so it is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
		}

		wrapper := wrapperBlock(t, first, "make vm-gh-wrapper")
		assertPinnedRealGH(t, "make vm-gh-wrapper", wrapper)

		for _, want := range []string{
			"/usr/local/libexec/shift2eu/gh",
			"/usr/local/bin/gh",
			"credential.https://github.com.helper",
			"auth git-credential",
		} {
			if !strings.Contains(first, want) {
				t.Errorf("make vm-gh-wrapper never mentions %q: it must move the real gh to /usr/local/libexec/shift2eu/gh, install the wrapper at /usr/local/bin/gh and point the git credential helper at it:\n%s", want, first)
			}
		}

		// And the wrapper's text never reaches a command line.
		argv := sb.argvLogText(t)

		for _, line := range strings.Split(wrapper, "\n") {
			line = strings.TrimSpace(line)

			if line == "" || strings.HasPrefix(line, "#") || len(line) < 12 {
				continue
			}

			if strings.Contains(argv, "arg="+line+"\n") {
				t.Errorf("the wrapper's line %q reached a limactl command line; it must go over standard input (review item 3)", line)
			}
		}
	})
}

// TestMakeDeckLeavesTheTokenPlumbingAloneWithoutTheAppVariables pins that make deck behaves as before on a Mac
// that has no App: without the three SHIFT2EU_AGENTS_APP_* variables it opens the deck and starts no refresher
// (review item 8).
func TestMakeDeckLeavesTheTokenPlumbingAloneWithoutTheAppVariables(t *testing.T) {
	t.Parallel()

	sb := newSandbox(t)
	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = filepath.Join(sb.root, "lima-stdin.log")

	stdout, stderr, err := runMake(t, []string{"deck"}, env)
	if err != nil {
		t.Fatalf("make deck without the App's variables: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// The deck itself ran: the fake limactl was asked for it and ran nothing.
	var ranDeck bool

	for _, args := range invocations(sb.argvLogText(t), "limactl") {
		if slices.Contains(args, "dot-agent-deck") {
			ranDeck = true
		}
	}

	if !ranDeck {
		t.Errorf("make deck never opened the deck:\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// And nothing of the token plumbing happened: no pid file, no log, no state directory.
	entries, err := os.ReadDir(sb.stateDir())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read %s: %v", sb.stateDir(), err)
		}

		return
	}

	for _, entry := range entries {
		t.Errorf("make deck without the App's variables left %s in %s: it must start no agents App token refresh", entry.Name(), sb.stateDir())
	}
}

// TestMakeDeckKeepsTheAppVariablesOutOfTheVM pins what issue #227 changed about make deck: the deck runs from the
// host clone, whose .envrc holds the agents App's three variables, so make deck with them set says nothing about
// them any more - the note about a workspace shell it used to print belonged to the layout the issue removed -
// opens the deck anyway, and hands limactl neither their names nor their values: the deck itself never needed
// them, and make agents-token-refresh-start or agents-token-refresh-install, in the host clone, are what do.
func TestMakeDeckKeepsTheAppVariablesOutOfTheVM(t *testing.T) {
	t.Parallel()

	f := newTokenFixture(t, "shift2eu")

	// The three values in the fixture's environment, the fake App's own: none may reach the VM.
	values := make([]string, 0, 3)

	for _, name := range []string{
		"SHIFT2EU_AGENTS_APP_ID",
		"SHIFT2EU_AGENTS_APP_INSTALLATION_ID",
		"SHIFT2EU_AGENTS_APP_KEY_FILE",
	} {
		values = append(values, f.env[name])
	}

	// make runs in the host clone, a clone outside the workspace, as a maintainer runs it (issue #227).
	stdout, stderr, err := runMakeIn(t, writeClone(t, filepath.Join(f.sb.root, "host", "shift2eu")), []string{"deck"}, f.env)
	if err != nil {
		t.Fatalf("make deck from the host clone with the App's variables in its environment: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if strings.Contains(stderr, agentsAppPrefix) {
		t.Errorf("make deck says something about %s on stderr: with make deck running from the host clone, whose .envrc holds those variables, that is the normal case and needs no note (issue #227)\nstdout: %s\nstderr: %s",
			agentsAppPrefix, stdout, stderr)
	}

	// It still opens the deck - the variables change nothing about it - and hands the VM neither the names nor
	// the values: no agents App variable belongs in the VM, which gets only the one-hour token.
	var ranDeck bool

	for _, args := range invocations(f.sb.argvLogText(t), "limactl") {
		if !slices.Contains(args, "dot-agent-deck") {
			continue
		}

		ranDeck = true

		for _, arg := range args {
			if strings.Contains(arg, agentsAppPrefix) {
				t.Errorf("make deck handed limactl %q, one of the App's variables: the deck never needs them, and the VM must only ever get the one-hour token make agents-token writes (issue #227)\nstdout: %s\nstderr: %s",
					arg, stdout, stderr)
			}

			for _, value := range values {
				if value != "" && strings.Contains(arg, value) {
					t.Errorf("make deck handed limactl %q, which carries the value of one of the App's variables: the deck never needs them, and the VM must only ever get the one-hour token make agents-token writes (issue #227)\nstdout: %s\nstderr: %s",
						arg, stdout, stderr)
				}
			}
		}
	}

	if !ranDeck {
		t.Errorf("make deck did not open the deck:\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	for _, name := range envNamesSet(t, f.sb.argvLogText(t)) {
		if strings.HasPrefix(name, agentsAppPrefix) {
			t.Errorf("make deck left %s in the environment of the process it runs in the VM: the App's key, its path and its IDs stay on the Mac, and a process in the VM needs none of them (issue #227, ADR 0031)",
				name)
		}
	}

	// And it starts nothing of the token plumbing: no refresher, no state file, no mint (second review, item 1:
	// the refresh belongs to make agents-token-refresh-start in the host clone).
	entries, err := os.ReadDir(f.state)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read %s: %v", f.state, err)
		}
	}

	for _, entry := range entries {
		t.Errorf("make deck with the App's variables set left %s in %s: it must start no agents App token refresh", entry.Name(), f.state)
	}

	if got := len(f.api.minted()); got != 0 {
		t.Errorf("make deck with the App's variables set minted %d token(s): the refresh is not its job any more", got)
	}
}

// tokenFixture is one environment for a run of make agents-token-refresh-start, or of make deck, with the
// App's three variables set: a sandbox whose limactl records what it is given and runs nothing, the fake
// GitHub API, and a state directory inside the sandbox, so no test leaves a refresher or a state file behind.
type tokenFixture struct {
	sb    *sandbox
	api   *fakeGitHub
	env   map[string]string
	state string // ${XDG_STATE_HOME}/shift2eu
}

// newTokenFixture builds the fixture for one VM instance and stops the refresher a target starts with the test.
func newTokenFixture(t *testing.T, instance string) *tokenFixture {
	t.Helper()

	sb := newSandbox(t)
	key := writeInventedKey(t, filepath.Join(sb.root, "key", "agents-app.pem"), 0o600)
	api := newFakeGitHub(t, key.public)

	env := sb.baseEnv()
	env["PATH"] = sb.binDir + ":/usr/local/bin:/usr/bin:/bin"
	env["SHIFT2EU_AGENTS_APP_ID"] = api.appID
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = api.installationID
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = key.path
	env["SHIFT2EU_AGENTS_GITHUB_API_URL"] = api.url
	env["SHIFT2EU_VM_REAL_GH_PATH"] = filepath.Join(sb.root, "vm-real-gh")
	env["SHIFT2EU_TEST_LIMA_CAPTURE"] = filepath.Join(sb.root, "lima-stdin.log")
	env["SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS"] = "1"
	env["SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS"] = "60"
	env["LIMA_INSTANCE"] = instance

	f := &tokenFixture{sb: sb, api: api, env: env, state: sb.stateDir()}
	t.Cleanup(func() { f.stopRefresher(t) })

	return f
}

// statePath is a file in the state directory the start target keeps the refresh's pid and log in.
func (f *tokenFixture) statePath(name string) string {
	return filepath.Join(f.state, name)
}

// writeStateFile writes one file of the state directory, with the mode the start target gives it.
func (f *tokenFixture) writeStateFile(t *testing.T, name, content string, mode os.FileMode) {
	t.Helper()

	writeFileMode(t, f.statePath(name), []byte(content), mode)
}

// stopRefresher ends every refresher a target started in this fixture, so no test leaves a loop minting
// against a server the test has closed.
func (f *tokenFixture) stopRefresher(t *testing.T) {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(f.state, "*.pid"))
	if err != nil {
		t.Errorf("look for the pid files of %s: %v", f.state, err)

		return
	}

	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
		if err != nil || pid <= 1 {
			continue
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}

		_ = proc.Signal(syscall.SIGTERM)

		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && proc.Signal(syscall.Signal(0)) == nil {
			time.Sleep(25 * time.Millisecond)
		}

		_ = proc.Kill()
	}
}

// readPid is the pid the pid file holds.
func readPid(t *testing.T, path string) int {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is not there, so make deck started no refresher: %v", path, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("%s does not hold a pid: %q", path, strings.TrimSpace(string(body)))
	}

	return pid
}

// pidAlive reports whether a process with this pid is running.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	return proc.Signal(syscall.Signal(0)) == nil
}

// deadPid is the pid of a process that has exited, for a pid file no process owns.
func deadPid(t *testing.T) int {
	t.Helper()

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run a process that exits at once: %v", err)
	}

	return cmd.Process.Pid
}

// TestMakeAgentsTokenRefreshStartKeepsOneRefresherPerVM pins second review item 1, and with it the
// per-instance pid and log files of the first review's item 7 and the lock's pid (N-new-2): make
// agents-token-refresh-start warns about a refresher that stopped and starts a new one, leaves a running one
// alone, honours a refresher that holds the lock without a pid file, says where the log is, and names the
// files after the VM instance, so two VMs never share a pid file.
func TestMakeAgentsTokenRefreshStartKeepsOneRefresherPerVM(t *testing.T) {
	t.Parallel()

	t.Run("a pid file whose process is dead warns and starts a new refresher", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		stale := deadPid(t)
		f.writeStateFile(t, "agents-token-refresh-shift2eu.pid", strconv.Itoa(stale)+"\n", 0o600)
		f.writeStateFile(t, "agents-token-refresh-shift2eu.log", "agents-token: wrote a token\n", 0o600)

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; !strings.Contains(combined, strconv.Itoa(stale)) {
			t.Errorf("make deck says nothing about the refresher whose pid %d it found dead, so a maintainer cannot see that the last one stopped before the token ran out:\n%s", stale, combined)
		}

		pidPath := f.statePath("agents-token-refresh-shift2eu.pid")
		pid := readPid(t, pidPath)

		if pid == stale {
			t.Errorf("the pid file still holds the dead pid %d: make deck must start a refresher", pid)
		}

		if !pidAlive(pid) {
			t.Errorf("the pid file holds %d, which is not running: make deck must leave a refresher running", pid)
		}

		info, err := os.Stat(pidPath)
		if err != nil {
			t.Fatalf("stat %s: %v", pidPath, err)
		}

		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("the pid file is mode %04o, want 0600: it is written under umask 077 like the log (review item 7, N7)", got)
		}

		// A background start is only usable if it says where the log is: the refresher writes nothing else.
		if logPath := f.statePath("agents-token-refresh-shift2eu.log"); !strings.Contains(stdout+stderr, logPath) {
			t.Errorf("make agents-token-refresh-start does not print the log's path %s, so a maintainer cannot read what the refresher says:\nstdout: %s\nstderr: %s", logPath, stdout, stderr)
		}

		waitFor(t, 60*time.Second, "the refresher make agents-token-refresh-start started to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})
	})

	t.Run("a refresher that is running is left alone", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		if _, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env); err != nil {
			t.Fatalf("the first make agents-token-refresh-start: %v\nstderr: %s", err, stderr)
		}

		waitFor(t, 60*time.Second, "the refresher make agents-token-refresh-start started to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})

		pidPath := f.statePath("agents-token-refresh-shift2eu.pid")
		pid := readPid(t, pidPath)
		mints := len(f.api.minted())

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("the second make agents-token-refresh-start: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; !strings.Contains(combined, strconv.Itoa(pid)) {
			t.Errorf("make agents-token-refresh-start does not say which refresher it found running (pid %d):\n%s", pid, combined)
		}

		if got := readPid(t, pidPath); got != pid {
			t.Errorf("make agents-token-refresh-start replaced the running refresher: the pid file held %d, now %d", pid, got)
		}

		if got := len(f.api.minted()); got != mints {
			t.Errorf("make agents-token-refresh-start started a second refresher: the fake API minted %d token(s) instead of %d", got, mints)
		}
	})

	t.Run("a refresher that holds the lock is left alone even with no pid file", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")

		// A refresher started by hand (make agents-token-refresh) holds the instance's lock and writes no pid
		// file, so the start target must read the lock's pid and not only its own pid file (review N-new-2).
		held := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, f.env)
		lock := f.sb.refreshLockDir("shift2eu")

		waitFor(t, 30*time.Second, "the refresh started by hand to take the lock", func() bool {
			_, err := os.Stat(lock)

			return err == nil
		})

		// The refresher takes the lock before it mints (take it after and two starters could both mint),
		// so the lock alone does not say it has a token yet: wait for the mint it makes under the lock and
		// only then take the baseline this case counts from.
		waitFor(t, 60*time.Second, "the refresh started by hand to mint its first token", func() bool {
			return len(f.api.minted()) >= 1
		})

		mints := len(f.api.minted())

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start while another refresher holds the lock: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; !strings.Contains(combined, strconv.Itoa(held.pid())) {
			t.Errorf("make agents-token-refresh-start does not name the refresher that holds the lock (pid %d), so a maintainer cannot tell why nothing started:\n%s", held.pid(), combined)
		}

		if _, err := os.Stat(f.statePath("agents-token-refresh-shift2eu.pid")); err == nil {
			t.Errorf("make agents-token-refresh-start wrote a pid file although a refresher already holds the lock: it started a second one")
		}

		if !held.running() {
			t.Fatalf("the refresh that held the lock exited because the start target ran; stderr:\n%s", held.errOut())
		}

		if got := len(f.api.minted()); got != mints {
			t.Errorf("make agents-token-refresh-start minted %d token(s) although a refresher already holds the lock", got-mints)
		}

		held.signal(t, syscall.SIGTERM)

		if !held.waitExit(30 * time.Second) {
			t.Fatalf("the refresh that held the lock did not stop on SIGTERM; stderr:\n%s", held.errOut())
		}
	})

	t.Run("the pid and log files are named after the VM instance", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu-other")

		if _, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env); err != nil {
			t.Fatalf("make agents-token-refresh-start: %v\nstderr: %s", err, stderr)
		}

		for _, name := range []string{"agents-token-refresh-shift2eu-other.pid", "agents-token-refresh-shift2eu-other.log"} {
			if _, err := os.Stat(f.statePath(name)); err != nil {
				t.Errorf("make agents-token-refresh-start wrote no %s: the refresh's pid and log are named after the instance, so two VMs keep two loops (review item 7)", name)
			}
		}

		if _, err := os.Stat(f.statePath("agents-token-refresh-shift2eu.pid")); err == nil {
			t.Errorf("make agents-token-refresh-start named its pid file after another VM instance than shift2eu-other")
		}

		waitFor(t, 60*time.Second, "the refresher of the other instance to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})
	})

	// Issue #229: the LaunchDaemon runs one refresh per instance, so a second one started by hand beside it is a
	// second loop minting for the same VM. Once `launchctl print` answers for the instance's label, the target
	// warns and starts nothing. The fake launchctl answers that print, so these two cases pin both halves on a
	// machine without launchd, which is what Linux CI has.
	t.Run("a loaded LaunchDaemon is warned about and no refresher starts", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")
		f.sb.useFakeLaunchctl(t, f.env, "0")

		label := launchdLabelPrefix + "shift2eu"

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start with the LaunchDaemon %s loaded: %v\nstdout: %s\nstderr: %s", label, err, stdout, stderr)
		}

		for _, want := range []string{"LaunchDaemon", label} {
			if !strings.Contains(stderr, want) {
				t.Errorf("make agents-token-refresh-start does not name %q on stderr although launchd runs the refresh as %s: a maintainer would start a second loop and never learn why not to (issue #229)\nstdout: %s\nstderr: %s", want, label, stdout, stderr)
			}
		}

		// It asked launchd about this instance's job, so the warning is about the job that runs this VM's
		// refresh and not about another instance's. launchctl takes the service target as one argument,
		// `launchctl print system/<label>`, so the same invocation must carry `print` and that target.
		var askedLabel bool

		for _, args := range invocations(f.sb.argvLogText(t), "launchctl") {
			if slices.Contains(args, "print") && slices.Contains(args, "system/"+label) {
				askedLabel = true
			}
		}

		if !askedLabel {
			t.Errorf("make agents-token-refresh-start never ran launchctl print system/%s, so it cannot know the LaunchDaemon runs the refresh:\n%s", label, f.sb.argvLogText(t))
		}

		// The warning is a refusal to start a second loop: no pid file, no log, and no mint.
		entries, err := os.ReadDir(f.state)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read %s: %v", f.state, err)
		}

		for _, entry := range entries {
			t.Errorf("make agents-token-refresh-start left %s in %s although the LaunchDaemon runs the refresh: it must start nothing beside the job (issue #229)", entry.Name(), f.state)
		}

		if got := len(f.api.minted()); got != 0 {
			t.Errorf("make agents-token-refresh-start started a refresher beside the LaunchDaemon: the fake API minted %d token(s) (issue #229)", got)
		}
	})

	t.Run("no loaded LaunchDaemon leaves the start target as it was", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")
		// 113 is what launchctl returns for a job it does not know: the instance's LaunchDaemon is not
		// loaded, so the refresh is still the maintainer's to start by hand.
		f.sb.useFakeLaunchctl(t, f.env, "113")

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start with no LaunchDaemon loaded: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if combined := stdout + stderr; strings.Contains(combined, "LaunchDaemon") {
			t.Errorf("make agents-token-refresh-start warns about a LaunchDaemon although launchctl knows no job for this instance (issue #229):\n%s", combined)
		}

		pid := readPid(t, f.statePath("agents-token-refresh-shift2eu.pid"))

		if !pidAlive(pid) {
			t.Errorf("the pid file holds %d, which is not running: with no LaunchDaemon loaded the target still starts the refresher it always started (issue #229)", pid)
		}
	})

	// A shell that never exported LIMA_INSTANCE still gets the default instance's refresher: the recipe exports
	// LIMA_INSTANCE := $(LIMA_INSTANCE), and the Makefile defaults it to shift2eu, so the pid and log files are
	// named after that VM and not after an empty name (issue #229, sixth review).
	t.Run("no LIMA_INSTANCE in the environment starts the default instance's refresher", func(t *testing.T) {
		t.Parallel()

		f := newTokenFixture(t, "shift2eu")
		delete(f.env, "LIMA_INSTANCE")

		f.sb.useFakeLaunchctl(t, f.env, "113")

		stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
		if err != nil {
			t.Fatalf("make agents-token-refresh-start with no LIMA_INSTANCE in its environment: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		pidPath := f.statePath("agents-token-refresh-shift2eu.pid")
		pid := readPid(t, pidPath)

		if !pidAlive(pid) {
			t.Errorf("the pid file %s holds %d, which is not running: a start with no LIMA_INSTANCE in the environment starts the default instance's refresher", pidPath, pid)
		}

		if _, err := os.Stat(f.statePath("agents-token-refresh-.pid")); err == nil {
			t.Errorf("make agents-token-refresh-start named the pid file after an empty instance: with no LIMA_INSTANCE in the environment it must use the Makefile's default, shift2eu")
		}

		waitFor(t, 60*time.Second, "the default instance's refresher to mint a token", func() bool {
			return len(f.api.minted()) >= 1
		})
	})
}

// TestMakeAgentsTokenRefreshStartPrintsOnlyANumericPid pins issue #320: the start target prints the pid file it
// reads as it reads it, and that file is untrusted text in the refresh's state directory - a terminal escape in it
// drives the maintainer's terminal and lands in the log. It prints the pid only when it is a number, as the
// refresh's own show_pid does, and the word unknown or invalid otherwise; a numeric stale pid is printed as before.
func TestMakeAgentsTokenRefreshStartPrintsOnlyANumericPid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// pid is what the pid file holds, and want is what the warning prints for it. garbage is the text no line
		// of the output may carry.
		pid     string
		want    string
		garbage string
	}{
		{
			name: "a stale pid that is a number is printed as before",
			want: "<pid>", // the dead pid this case writes, told apart from the started refresher's
		},
		{
			name:    "a stale pid file holding text and an escape sequence prints neither",
			pid:     "abc\x1b[31mX",
			want:    "unknown",
			garbage: "abc",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, "shift2eu")

			// A pid file whose process is gone: the refresher it names has stopped, which is the warning this test
			// reads. The numeric case uses a pid no process owns, so the target cannot leave the refresher alone.
			stale := tc.pid
			if stale == "" {
				dead := deadPid(t)
				stale = strconv.Itoa(dead)
				tc.want = strconv.Itoa(dead)
			}

			f.writeStateFile(t, "agents-token-refresh-shift2eu.pid", stale+"\n", 0o600)
			f.writeStateFile(t, "agents-token-refresh-shift2eu.log", "agents-token: wrote a token\n", 0o600)

			stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
			if err != nil {
				t.Fatalf("make agents-token-refresh-start: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			combined := stdout + stderr

			line, ok := warningAboutTheStoppedRefresher(combined)
			if !ok {
				t.Fatalf("make agents-token-refresh-start says nothing about the refresher whose pid file it found, so a maintainer cannot see that the last one stopped before the token ran out (issue #320):\n%s",
					combined)
			}

			if !strings.Contains(line, tc.want) {
				t.Errorf("the warning about the refresher that stopped reads %q, want it to name %q: the pid file's pid when it is a number, and a word for it when it is not (issue #320)\noutput:\n%s",
					line, tc.want, combined)
			}

			if strings.ContainsRune(combined, '\x1b') {
				t.Errorf("make agents-token-refresh-start printed a raw ESC, copied out of the refresh's pid file: a value out of that file must not reach the maintainer's terminal, where it drives it instead of being read (issue #320)\noutput:\n%q",
					combined)
			}

			if tc.garbage != "" && strings.Contains(combined, tc.garbage) {
				t.Errorf("make agents-token-refresh-start printed %q out of the refresh's pid file (issue #320)\noutput:\n%q",
					tc.garbage, combined)
			}

			if tc.garbage != "" {
				lower := strings.ToLower(line)
				if !strings.Contains(lower, "unknown") && !strings.Contains(lower, "invalid") {
					t.Errorf("the warning about the refresher that stopped reads %q, want the word unknown or invalid where the pid would be: the refresh's own show_pid prints those, and this recipe must not print the file's text (issue #320)\noutput:\n%s",
						line, combined)
				}
			}
		})
	}
}

// warningAboutTheStoppedRefresher returns the line of the start target's output that warns about the refresher it
// found stopped: the one naming that it had stopped, which is the only line that prints the pid file's content
// (issue #320).
func warningAboutTheStoppedRefresher(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "had stopped") {
			return line, true
		}
	}

	return "", false
}

// TestMakeAgentsTokenRefreshStartRefusesAnInvalidInstance pins the fifth review's finding: the start target uses
// LIMA_INSTANCE in the paths of its pid, log and lock files and in the label it asks launchd about, and make splices
// the value into the shell text it runs. An instance name with a space, a slash or a quote is no name this
// Makefile supports (the install and the uninstall already refuse one), so the start target refuses it before it
// asks launchctl anything and before it starts a refresher, and the refusal names the variable.
func TestMakeAgentsTokenRefreshStartRefusesAnInvalidInstance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		instance string
	}{
		{name: "a name with a space", instance: "a b"},
		{name: "a name with a slash", instance: "a/b"},
		{name: "a name with a quote", instance: `x"y`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, "shift2eu")

			stdout, stderr, err := runMake(t, []string{"LIMA_INSTANCE=" + tc.instance, "agents-token-refresh-start"}, f.env)
			if err == nil {
				t.Fatalf("make agents-token-refresh-start with LIMA_INSTANCE=%q exited 0: the value becomes part of the pid, log and lock file names and of the label launchd is asked about, and make splices it into the shell text it runs, so a name this Makefile does not support must be refused (issue #229, fifth review)\nstdout: %s\nstderr: %s", tc.instance, stdout, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "launchctl"); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-start asked launchctl about LIMA_INSTANCE=%q before it refused it, and the check of the name must come before any use: %v\nstdout: %s\nstderr: %s", tc.instance, calls, stdout, stderr)
			}

			if !strings.Contains(stderr, "LIMA_INSTANCE") {
				t.Errorf("the refusal does not name LIMA_INSTANCE, so a maintainer cannot tell which value to fix:\nstderr: %s", stderr)
			}

			pids, globErr := filepath.Glob(f.statePath("*.pid"))
			if globErr != nil {
				t.Fatalf("look for pid files in %s: %v", f.state, globErr)
			}

			if len(pids) != 0 {
				t.Errorf("make agents-token-refresh-start started a refresher for LIMA_INSTANCE=%q and left %v: a name it refuses must start nothing (issue #229, fifth review)", tc.instance, pids)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallTakesOverFromARefreshStartedByHand pins the maintainer's first comment in issue
// #396 and the decision taken for it: a hand-started refresher must not keep the instance's lock while the job
// waits out its ThrottleInterval, so that tokens arrive from a process that does not survive a reboot. The start
// target records the refresher it started in the instance's pid file, so an install that finds the lock's pid in
// that file stops it - TERM, the signal its traps expect, then wait for it to exit - and says so naming its pid,
// before it loads the job. Any other holder - a refresh started in the foreground with make agents-token-refresh,
// say, which nothing recorded and which the maintainer is watching - is none of the install's to stop: it refuses
// before it installs anything, naming the pid and the command that stops it. With no other refresher the install
// is what it always was.
func TestMakeAgentsTokenRefreshInstallTakesOverFromARefreshStartedByHand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// lockHeld says a refresher of this instance holds the instance's lock when the install runs.
		lockHeld bool
		// recorded says the start target's pid file names that refresher, which is how the install knows it may
		// stop it.
		recorded bool
		// ignoresTERM says the recorded refresher leaves TERM ignored, as a process in a stop it does not expect
		// can: the install must wait, warn and load the job anyway.
		ignoresTERM bool
		// wantRefused says the install must refuse and install nothing.
		wantRefused bool
	}{
		{
			// The handover the issue is about: make agents-token-refresh-start started the refresher, then the
			// maintainer installed the job. The refresher holds the lock as long as it runs and the job exits at
			// every ThrottleInterval, so the install stops it and the job takes the lock.
			name:     "the refresher the start target recorded holds the lock",
			lockHeld: true,
			recorded: true,
		},
		{
			// A refresh started in the foreground holds the lock and no pid file names it: the install cannot know
			// that stopping it is what the maintainer wants, and it must not.
			name:        "another refresher holds the lock",
			lockHeld:    true,
			wantRefused: true,
		},
		{
			// TERM ignored: the install cannot stop the recorded refresher, so it warns and loads the job, which
			// takes the lock as soon as that refresher exits (reviewer's test nit).
			name:        "the recorded refresher ignores TERM",
			lockHeld:    true,
			recorded:    true,
			ignoresTERM: true,
		},
		{name: "no refresher holds the lock"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")

			var refresh *runningScript

			pid := ""

			if tc.lockHeld {
				if tc.ignoresTERM {
					refresh = standInForARefreshIgnoringTERM(t, f.sb)
				} else {
					refresh = standInForARefresh(t, f.sb)
				}

				pid = strconv.Itoa(refresh.pid())

				writeFileMode(t, filepath.Join(f.sb.refreshLockDir(f.instance), "pid"), []byte(pid+"\n"), 0o600)
			}

			pidFile := filepath.Join(f.sb.stateDir(), "agents-token-refresh-"+f.instance+".pid")

			if tc.recorded {
				writeFileMode(t, pidFile, []byte(pid+"\n"), 0o600)
			}

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			combined := stdout + stderr

			if tc.wantRefused {
				if err == nil {
					t.Fatalf("make agents-token-refresh-install exited 0 with the lock of %s held by pid %s, which no pid file of the start target names: the install must refuse rather than stop a refresh it never started, and say what stops it (issue #396)\nstdout: %s\nstderr: %s",
						f.instance, pid, stdout, stderr)
				}

				var named bool

				for _, line := range strings.Split(combined, "\n") {
					if strings.Contains(line, pid) && strings.Contains(line, "kill") {
						named = true
					}
				}

				if !named {
					t.Errorf("the install refuses while pid %s holds the lock of %s but does not name both the pid and the command that stops it (`kill %s`), so a maintainer cannot free the lock the job waits for (issue #396)\nstdout: %s\nstderr: %s",
						pid, f.instance, pid, stdout, stderr)
				}

				// Refusing before anything is installed: a plist left where launchd reads it is a job at the next boot.
				for _, args := range f.sudoCommands(t) {
					if len(args) > 0 && args[0] == "install" {
						t.Errorf("the install ran `sudo install` and then refused because pid %s holds the lock: the refusal must come before the plist is installed, or a job the run refused to load is there at the next boot (issue #396): %v", pid, args)
					}
				}

				if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
					t.Errorf("the install left %s behind although it refuses while pid %s holds the lock: launchd loads it at the next boot (issue #396) (stat: %v)", f.dest(), pid, statErr)
				}

				if calls := f.bootstrapCalls(t); len(calls) != 0 {
					t.Errorf("the install ran launchctl bootstrap although it refuses while pid %s holds the lock (issue #396): %v", pid, calls)
				}

				if !refresh.running() {
					t.Errorf("the install stopped pid %s, the refresher it did not start: only the refresher the start target recorded is the install's to stop (issue #396)", pid)
				}

				return
			}

			if err != nil {
				t.Fatalf("make agents-token-refresh-install: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 1 {
				t.Errorf("the install ran launchctl bootstrap %d time(s), want one: the job must be loaded once whoever holds the lock (issue #396): %v", len(calls), calls)
			}

			if !tc.recorded {
				// No other refresher: the install is what it always was, and says nothing about a pid to kill.
				if _, statErr := os.Stat(f.dest()); statErr != nil {
					t.Errorf("the install did not write the plist %s although no other refresher holds the lock: %v", f.dest(), statErr)
				}

				if strings.Contains(strings.ToLower(combined), "kill") {
					t.Errorf("the install speaks of killing a refresh although no other refresher holds the lock of %s (issue #396)\nstdout: %s\nstderr: %s", f.instance, stdout, stderr)
				}

				return
			}

			if tc.ignoresTERM {
				// TERM did nothing, so the install cannot have stopped it: it warns with the pid and goes on to
				// load the job, which takes the lock once that refresh exits.
				var warned bool

				for _, line := range strings.Split(combined, "\n") {
					if strings.Contains(line, pid) && strings.Contains(line, "still runs") && strings.Contains(strings.ToLower(line), "warning") {
						warned = true
					}
				}

				if !warned {
					t.Errorf("the install does not warn that pid %s still runs after TERM, the recorded refresher that ignores it: a maintainer never learns that the job waits for a process that may never exit (reviewer's test nit)\nstdout: %s\nstderr: %s",
						pid, stdout, stderr)
				}

				if !refresh.running() {
					t.Errorf("the install stopped pid %s even though it ignores TERM: nothing the install sends ends it, so this case no longer stands for a refresher that outlives its TERM (reviewer's test nit)\nstdout: %s\nstderr: %s",
						pid, stdout, stderr)
				}

				return
			}

			// The recorded refresher is stopped, named by its pid, before the job is loaded.
			var named bool

			for _, line := range strings.Split(combined, "\n") {
				if strings.Contains(line, pid) && strings.Contains(strings.ToLower(line), "stopped") {
					named = true
				}
			}

			if !named {
				t.Errorf("the install does not say that it stopped pid %s, the refresher make agents-token-refresh-start recorded, so a maintainer who started it by hand never learns that the job took over from it (issue #396)\nstdout: %s\nstderr: %s",
					pid, stdout, stderr)
			}

			for _, line := range strings.Split(combined, "\n") {
				if strings.Contains(line, pid) && strings.Contains(line, "kill") {
					t.Errorf("the install tells the maintainer to kill pid %s, the refresher it just stopped: the line is stale the moment it is printed (issue #396)\nline: %s", pid, line)
				}
			}

			// The pid file named the refresher the install stopped, and that refresher is gone: the file must not
			// outlive it, or the next start warns about a pid that no longer runs.
			if _, statErr := os.Stat(pidFile); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("the install left the pid file %s after it stopped pid %s, the refresher that file named: the next make agents-token-refresh-start warns that a refresh that does not run had stopped (issue #396) (stat: %v)",
					pidFile, pid, statErr)
			}

			if !poll(10*time.Second, func() bool { return !refresh.running() }) {
				t.Errorf("pid %s, the refresher the start target recorded, still runs after the install loaded the job: the install must wait for it to exit, or the job finds the lock taken and exits at every ThrottleInterval (issue #396)\nstdout: %s\nstderr: %s",
					pid, stdout, stderr)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshDistinguishesAFailedForeignJobsCheck pins the security audit's second finding: every
// non-zero status of dev/agents-token-launchd.sh foreign-jobs used to read as "a foreign job was found, named
// above", so a check that could not run told the maintainer to remove a job that does not exist - and taught them
// that the check is noise. Only status 3, the one the script gives a job it named, is that refusal; any other
// failure says the check itself failed and names its status, and nothing is installed or started. The failure here
// is the check's own account lookup (id -u), which its script's `uid="$(id -u)"` turns into status 1.
func TestMakeAgentsTokenRefreshDistinguishesAFailedForeignJobsCheck(t *testing.T) {
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

			// The check cannot tell which account runs it, so it fails before it has any verdict about launchd.
			f.sb.useFakeIDThatFails(t)

			stdout, stderr, err := f.run(t, tc.target)
			if err == nil {
				t.Fatalf("make %s exited 0 although dev/agents-token-launchd.sh foreign-jobs could not tell which account runs it (id -u failed): the check did not run, and nothing may be installed or started on its word (security audit L2)\nstdout: %s\nstderr: %s",
					tc.target, stdout, stderr)
			}

			combined := stdout + stderr

			if strings.Contains(combined, "another launchd job runs") {
				t.Errorf("make %s prints the refusal for a foreign job although the check itself failed: a maintainer then looks for a job that is not there and runs the removal commands on nothing (security audit L2)\nstdout: %s\nstderr: %s",
					tc.target, stdout, stderr)
			}

			for _, want := range []string{"foreign-jobs check failed", "status 1", "nothing was"} {
				if !strings.Contains(combined, want) {
					t.Errorf("make %s does not say %q when dev/agents-token-launchd.sh foreign-jobs fails on its own account lookup (id -u, status 1): the maintainer must be able to tell a failed check from a foreign job, and must know that nothing was installed or started (security audit L2)\nstdout: %s\nstderr: %s",
						tc.target, want, stdout, stderr)
				}
			}

			// A failed check installs, loads and starts nothing.
			if calls := f.installedWithSudo(t); len(calls) != 0 {
				t.Errorf("make %s ran `sudo install` although the foreign-jobs check failed: a plist left where launchd reads it is a job at the next boot whatever the check said (security audit L2): %v",
					tc.target, calls)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 0 {
				t.Errorf("make %s ran `sudo launchctl bootstrap` although the foreign-jobs check failed: %v (security audit L2)", tc.target, calls)
			}

			if pids := f.pidFiles(t); len(pids) != 0 {
				t.Errorf("make %s started a refresher although the foreign-jobs check failed: %v (security audit L2)", tc.target, pids)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallDoesNotStopARefresherThePidFileNoLongerNames pins the security audit's fourth
// finding: the install re-reads the lock and the process right before the TERM, but not the pid file, and the sudo
// prompt plus the plist install can be long. In that window the recorded refresh can exit and another refresh can
// take the lock with a reused pid, and the install would stop a refresh the start target never recorded - the very
// rule its refusal keeps. The fake sudo rewrites the pid file during `sudo install`, the window the audit names:
// the install may then refuse or warn, but it must not stop the holder and must not say it did.
func TestMakeAgentsTokenRefreshInstallDoesNotStopARefresherThePidFileNoLongerNames(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	refresh := standInForARefresh(t, f.sb)
	pid := strconv.Itoa(refresh.pid())

	writeFileMode(t, filepath.Join(f.sb.refreshLockDir(f.instance), "pid"), []byte(pid+"\n"), 0o600)

	pidFile := filepath.Join(f.sb.stateDir(), "agents-token-refresh-"+f.instance+".pid")
	writeFileMode(t, pidFile, []byte(pid+"\n"), 0o600)

	// The sudo window: by the time the TERM is due, the pid file names another process, as it does when the
	// recorded refresh exited and another one took the lock first.
	f.env["SHIFT2EU_TEST_SUDO_INSTALL_REWRITE_PID_FILE"] = pidFile
	f.env["SHIFT2EU_TEST_SUDO_INSTALL_REWRITE_PID"] = strconv.Itoa(os.Getpid())

	stdout, stderr, _ := f.run(t, "agents-token-refresh-install")
	combined := stdout + stderr

	// The premise: the window the audit describes really opened, so the case does not pass for want of a rewrite.
	f.subCommand(t, "install")

	if !refresh.running() {
		t.Errorf("the install stopped pid %s although the pid file no longer named it when the TERM was due (it named another process after `sudo install`): only the refresher the start target recorded is the install's to stop, and the file is how it knows (security audit L4)\nstdout: %s\nstderr: %s",
			pid, stdout, stderr)
	}

	for _, line := range strings.Split(combined, "\n") {
		if strings.Contains(line, "stopped pid "+pid) {
			t.Errorf("the install says it stopped pid %s although the pid file no longer named it when the TERM was due: the message sends the maintainer away from the refresh that does hold the lock (security audit L4)\nline: %s",
				pid, line)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallSaysWhenTheTakeoverStoppedTheRefreshAndTheJobDidNotLoad pins the reviewer's
// fifth finding: when the hand-started refresh was stopped and launchctl then refuses both bootstraps, the
// instance is left with no refresher at all. The error must say that the takeover stopped it and send the
// maintainer to make agents-token-refresh-start, not only to install again.
func TestMakeAgentsTokenRefreshInstallSaysWhenTheTakeoverStoppedTheRefreshAndTheJobDidNotLoad(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	refresh := standInForARefresh(t, f.sb)
	pid := strconv.Itoa(refresh.pid())

	writeFileMode(t, filepath.Join(f.sb.refreshLockDir(f.instance), "pid"), []byte(pid+"\n"), 0o600)
	writeFileMode(t, filepath.Join(f.sb.stateDir(), "agents-token-refresh-"+f.instance+".pid"), []byte(pid+"\n"), 0o600)

	// Both bootstraps fail, so after the takeover stopped the recorded refresh no refresher runs at all.
	f.env["SHIFT2EU_TEST_LAUNCHCTL_BOOTSTRAP_STATUS"] = "1"

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although both launchctl bootstraps failed: no token refresh job of %s is loaded (reviewer finding 5)\nstdout: %s\nstderr: %s",
			f.instance, stdout, stderr)
	}

	combined := stdout + stderr

	for _, want := range []string{"the hand-started refresh was stopped", "make agents-token-refresh-start"} {
		if !strings.Contains(combined, want) {
			t.Errorf("the error after the failed bootstrap does not say %q although the takeover stopped the hand-started refresh and no other runs: a maintainer does not learn that the instance has no refresher until the next boot (reviewer finding 5)\nstdout: %s\nstderr: %s",
				want, stdout, stderr)
		}
	}

	if refresh.running() {
		t.Errorf("pid %s, the refresher the start target recorded, still runs after the install stopped it: the failed bootstrap must come after the takeover, or this case pins no stopped refresher (reviewer finding 5)", pid)
	}
}

// TestMakeAgentsTokenRefreshInstallWarnsOnlyAboutAHandStartedRefresh pins the fifth review's finding: after
// `launchctl bootstrap` the job starts and the refresh it runs takes the instance's lock at once, so the lock's
// pid file names that refresh by the time the install reads it. Telling the maintainer that "a refresh you
// started by hand (pid N) holds the lock" and to kill N would then point at the job's own loop - the one process
// the job exists to keep running, and one launchd starts again five minutes later. A refresh that held the lock
// before the install and is not the job's own is no warning any more: issue #396 made the install stop the
// refresher the start target recorded and refuse any other, which
// TestMakeAgentsTokenRefreshInstallTakesOverFromARefreshStartedByHand pins.
func TestMakeAgentsTokenRefreshInstallWarnsOnlyAboutAHandStartedRefresh(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		lockHeldBefore bool // a refresh started by hand holds the lock before make runs
		jobRunsLock    bool // launchctl print reports the lock's pid as the job's own after the bootstrap
	}{
		{
			name: "the job's own refresh takes the lock as it loads",
		},
		{
			// The lock can belong to the job's own refresh even when the maintainer started that refresh by
			// hand: the project's job is loaded before the install runs and runs that very pid, so
			// `launchctl print` reports it. The install must take it for the job's own refresh - not stop it,
			// not refuse it for the start target never having recorded it, not warn about it - and replace the
			// loaded job as usual (issue #229, sixth review; issue #396).
			name:           "the job runs the very pid the lock holds",
			lockHeldBefore: true,
			jobRunsLock:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")

			// A running refresh of the instance, as the lock names one: it is the pid the lock holds when the
			// install reads the file, and it belongs to the job when the lock appears with the bootstrap.
			refresh := standInForARefresh(t, f.sb)
			pid := strconv.Itoa(refresh.pid())
			lockPid := filepath.Join(f.sb.refreshLockDir(f.instance), "pid")

			if tc.lockHeldBefore {
				writeFileMode(t, lockPid, []byte(pid+"\n"), 0o600)
			} else {
				f.sb.useFakeLaunchctlTakingTheLockAtLoad(t, f.env, lockPid, refresh.pid())
			}

			if tc.jobRunsLock {
				// The project's job is loaded before the install runs, and `launchctl print` names the refresh's
				// own pid as the job's: the lock holds the very process the job runs, however that refresh was
				// started.
				f.sb.useFakeLaunchctlJobPrintingPid(t, f.env, f.label(), refresh.pid())

				// A job that is loaded cannot be loaded again, so the install boots the old job out first and waits
				// for it to go: launchd is to have it loaded now (its state file exists), to unload it at that
				// bootout (the fake removes the file) and to load it again at the bootstrap (the fake creates it,
				// which useFakeLaunchctlJobPrintingPid set up).
				writeFileMode(t, f.env["SHIFT2EU_TEST_LAUNCHCTL_LOADED_FILE"], nil, 0o600)
			}

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err != nil {
				t.Fatalf("make agents-token-refresh-install with the lock of %s held by pid %s: %v\nstdout: %s\nstderr: %s", f.instance, pid, err, stdout, stderr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 1 {
				t.Fatalf("the install ran launchctl bootstrap %d time(s), want one: %v", len(calls), calls)
			}

			combined := stdout + stderr

			if tc.jobRunsLock {
				// The premise of this case: the job was loaded before the install ran, so the install had an old job
				// to boot out. Without it the case would stand for the same state as the one above (issue #396).
				if line := "+ sudo launchctl bootout system/" + f.label(); !strings.Contains(combined, line) {
					t.Fatalf("the install did not print %q, so the job %s was not loaded before it ran and this case no longer stands for a loaded job that runs the lock's holder (issue #396)\nstdout: %s\nstderr: %s", line, f.label(), stdout, stderr)
				}
			}

			// The lock belongs to the job's own refresh: the install must not warn about it, and must not send the
			// maintainer to kill the loop it just loaded.
			if strings.Contains(combined, "started by hand") {
				t.Errorf("the install warns about a refresh started by hand although the lock of %s holds pid %s, the refresh its own bootstrap loaded: the maintainer would kill the job's loop, and launchd would start it again 5 minutes later (issue #229, fifth review)\nstdout: %s\nstderr: %s", f.instance, pid, stdout, stderr)
			}

			for _, line := range strings.Split(combined, "\n") {
				if strings.Contains(line, "kill") && strings.Contains(line, pid) {
					t.Errorf("the install tells the maintainer to kill pid %s, the refresh the job it just loaded runs: that is the process the job exists to keep running (issue #229, fifth review)\nline: %s", pid, line)
				}
			}

			// The holder is the job's own refresh, so it is no refresher of the install's to stop, and the job
			// keeps running it: only the recorded refresher of
			// TestMakeAgentsTokenRefreshInstallTakesOverFromARefreshStartedByHand is the install's to stop
			// (issue #396).
			if !refresh.running() {
				t.Errorf("the install stopped pid %s, the process the very job it replaced runs: only the refresher the start target recorded in the instance's pid file is the install's to stop (issue #396)\nstdout: %s\nstderr: %s", pid, stdout, stderr)
			}

			// And it installed the new plist as it always does, beside loading it once.
			if _, statErr := os.Stat(f.dest()); statErr != nil {
				t.Errorf("the install did not write the plist %s, although the pid the lock holds is the job's own and the install has nothing to refuse or wait for (issue #396): %v", f.dest(), statErr)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesToRunAsRoot pins the sixth review's finding: the plist names the
// account that rendered it (id -un) as the job's UserName, so an install run under sudo -E would install a job
// that runs the host clone's script as root - a script the maintainer can still edit, and everything it reaches.
// The install must refuse such a run before it runs anything as root, prints no plist and installs nothing, and
// must say to run it as the maintainer and not under sudo.
func TestMakeAgentsTokenRefreshInstallRefusesToRunAsRoot(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	f.sb.useFakeIDAsRoot(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 with id -u answering 0: the plist it renders would name root as the job's UserName, and the job would run the host clone's script - which the maintainer can still edit - as root, at every boot\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// The refusal must be the install recipe's own, before it runs anything: dev/agents-token-launchd.sh refuses
	// a root run as well (agents-token-launchd: refused: ...), so a recipe whose own check is gone still exits
	// non-zero, and a test that only looks for a refusal passes on the generator's, after the recipe read the
	// VM's mounts as root (issue #229, seventh review).
	firstLine, _, _ := strings.Cut(stderr, "\n")
	if !strings.HasPrefix(firstLine, "agents-token-refresh-install: refused") {
		t.Errorf("the first line of stderr is not the install recipe's own refusal, which starts with %q: the generator dev/agents-token-launchd.sh refuses a root run too, so the refusal may come from it alone, after the recipe's own check - and the work before it - ran as root (issue #229, seventh review)\nfirst line: %q\nstdout: %s\nstderr: %s", "agents-token-refresh-install: refused", firstLine, stdout, stderr)
	}

	refusal := strings.ToLower(stderr)

	for _, want := range []string{"maintainer", "sudo"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not mention %q, so a maintainer who ran sudo -E make agents-token-refresh-install cannot tell how to run it instead:\nstdout: %s\nstderr: %s", want, stdout, stderr)
		}
	}

	// Refusing means doing nothing: no VM is asked about its mounts, no plist is rendered or linted, no file is
	// installed and no job is loaded, so a refused run leaves the machine exactly as it was (issue #229, sixth
	// and seventh reviews).
	for _, tool := range []string{"sudo", "limactl", "launchctl", "plutil"} {
		if calls := invocations(f.sb.argvLogText(t), tool); len(calls) != 0 {
			t.Errorf("the install ran %s although it refuses to run as root, so the refusal comes after work that root should never have been asked for: %v\nstdout: %s\nstderr: %s", tool, calls, stdout, stderr)
		}
	}

	if _, err := os.Stat(f.dest()); err == nil {
		t.Errorf("the install wrote the plist %s although it refuses to run as root: then a refused run still leaves a job behind for root to load at the next boot", f.dest())
	}
}

// standInForARefresh starts a harmless process whose command line ends in agents-app-token.sh refresh, the
// shape the install and the start target look for in the lock's pid: they compare the command line of the pid the
// lock names, so a test needs a process that looks like a refresh and never mints. It is a stand-in copied into
// the sandbox, not the repository's script, and it only sleeps until the test ends - which kills it.
func standInForARefresh(t *testing.T, sb *sandbox) *runningScript {
	t.Helper()

	path := filepath.Join(sb.root, "a-copy", "dev", "agents-app-token.sh")
	writeFileMode(t, path, []byte("#!/bin/sh\n# A stand-in for the refresh the job runs: it runs, and does nothing else.\ntrap 'exit 0' TERM\nwhile :; do sleep 1; done\n"), 0o755)

	r := sb.start(t, path, []string{"refresh"}, sb.baseEnv())

	// The lock is only about a running refresh: a stand-in that died would make the install skip the lock, and
	// the test would pass for the wrong reason.
	if !r.running() {
		t.Fatalf("the stand-in refresh %s exited at once; stderr:\n%s", path, r.errOut())
	}

	return r
}

// standInForARefreshIgnoringTERM is standInForARefresh with TERM ignored (trap ” TERM), as a process that does
// not expect to be stopped leaves it: the install's wait runs out, so a test can pin the warning the takeover
// gives instead of "stopped pid N" (reviewer's test nit).
func standInForARefreshIgnoringTERM(t *testing.T, sb *sandbox) *runningScript {
	t.Helper()

	path := filepath.Join(sb.root, "a-copy", "dev", "agents-app-token.sh")
	writeFileMode(t, path, []byte("#!/bin/sh\n# A stand-in for the refresh the job runs: it runs, ignores TERM, and does nothing else.\ntrap '' TERM\nwhile :; do sleep 1; done\n"), 0o755)

	r := sb.start(t, path, []string{"refresh"}, sb.baseEnv())

	if !r.running() {
		t.Fatalf("the stand-in refresh %s exited at once; stderr:\n%s", path, r.errOut())
	}

	return r
}

// TestMakefileHasTheAgentsTokenLaunchDaemonTargets pins the install target's shape only: it renders the
// instance's plist through dev/agents-token-launchd.sh, which launchdplist_test.go pins in full. Installing
// and uninstalling need sudo and a real launchctl, so neither runs here.
func TestMakefileHasTheAgentsTokenLaunchDaemonTargets(t *testing.T) {
	t.Parallel()

	// The plist goes where launchd reads LaunchDaemons, and a variable holds that directory: a test points it
	// at its own tree, so no test writes into /Library (issue #229, second review).
	if dir, ok := makeVariable(readRepoFile(t, makefilePath), "AGENTS_TOKEN_LAUNCHD_DIR"); !ok || dir != "/Library/LaunchDaemons" {
		t.Errorf("the Makefile's AGENTS_TOKEN_LAUNCHD_DIR is %q (declared: %v), want the default /Library/LaunchDaemons: the install and the uninstall write and remove the job's plist under that directory, and a test must be able to point it at its own tree (issue #229, second review)", dir, ok)
	}

	stdout, stderr, err := runMake(t, []string{"-n", "agents-token-refresh-install"}, makeEnv(t, nil))
	if err != nil {
		t.Fatalf("make -n agents-token-refresh-install: %v\nstderr: %s", err, stderr)
	}

	for _, want := range []string{"agents-token-launchd.sh", "plist"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("make -n agents-token-refresh-install does not mention %q: the target installs the plist dev/agents-token-launchd.sh renders for this VM instance, so one generator and one test cover every target that writes one (issue #229)\n%s", want, stdout)
		}
	}
}

// launchdMountsFormat is the limactl --format argument the install reads the instance's name and mount
// locations with: the instance's name on the first line and one mount location per line after it. The install
// needs the name to tell that limactl answered about the instance it asked about, and the locations to tell
// whether the clone it runs from is one of the directories the VM mounts and can edit (issue #229). The fake
// limactl answers the same shape.
const launchdMountsFormat = `{{.Name}}{{"\n"}}{{range .Config.Mounts}}{{.Location}}{{"\n"}}{{end}}`

// launchdInstallFixture is one environment for a run of make agents-token-refresh-install or
// agents-token-refresh-uninstall: a sandbox with a temporary HOME, a state directory inside it, the log-only
// fake sudo and the fake launchctl, plutil and limactl first on PATH, an invented key, the three App variables
// and a LaunchDaemons directory inside the sandbox, handed to make on its command line. No test reaches the
// machine's sudo, launchctl or plutil, and nothing writes into /Library.
type launchdInstallFixture struct {
	sb         *sandbox
	key        inventedKey
	env        map[string]string
	instance   string
	repo       string // the clone make runs in, symlinks resolved: what the plist names and the mount check reads
	dir        string // where make runs: the repository, or a clone a test grew inside its sandbox
	launchdDir string // where launchd reads LaunchDaemons from: the make command line's AGENTS_TOKEN_LAUNCHD_DIR
}

// launchdInstallOption changes the environment an install fixture runs make with, before the test uses it.
type launchdInstallOption func(t *testing.T, f *launchdInstallFixture)

// withoutOnPath leaves tool off the fixture's PATH, for a subtest that pins what a recipe does on a host that
// has no such tool, such as Linux CI, which has no launchctl (issue #229). The fake tool the fixture installed
// in binDir is removed, so it cannot be found there either, and every other fake tool stays reachable: the
// log-only sudo included, so a refusal that runs it would still be caught.
func withoutOnPath(tool string) launchdInstallOption {
	return func(t *testing.T, f *launchdInstallFixture) {
		t.Helper()

		path := filepath.Join(f.sb.binDir, tool)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove the fake %s from %s: %v", tool, f.sb.binDir, err)
		}

		f.env["PATH"] = f.sb.binDir + ":" + pathWithout(t, tool)
	}
}

// launchdClone writes a clone of the repository at dir: the Makefile and the two scripts an install reads. A
// test that needs the clone somewhere of its own - inside its HOME, for the mount locations the install expands
// `~` into - grows one here, and the real recipe still runs, from the sandbox (issue #229).
func launchdClone(t *testing.T, dir string) string {
	t.Helper()

	writeFileMode(t, filepath.Join(dir, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)

	for _, script := range []string{launchdScript, agentsMintScript} {
		writeFileMode(t, filepath.Join(dir, script), []byte(readRepoFile(t, script)), 0o755)
	}

	return dir
}

// resolvedPath returns path with its symlinks resolved, the way the install's own pwd -P resolves the clone it
// runs from, so a test can compare the refusal with the path the install prints.
func resolvedPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve the symlinks of %s: %v", path, err)
	}

	return resolved
}

// inAClone makes make run in a clone grown inside the sandbox instead of in the repository, so the mount check
// reads the clone a test built rather than this worktree.
func inAClone(where func(f *launchdInstallFixture) string) launchdInstallOption {
	return func(t *testing.T, f *launchdInstallFixture) {
		t.Helper()

		clone := launchdClone(t, where(f))
		f.dir, f.repo = clone, resolvedPath(t, clone)
	}
}

// inACloneInsideHome grows the clone under the fixture's HOME, so a mount location `~` or `~/clone` names a
// directory that really holds the clone (issue #229, second review).
func inACloneInsideHome() launchdInstallOption {
	return inAClone(func(f *launchdInstallFixture) string { return filepath.Join(f.env["HOME"], "clone") })
}

// newLaunchdInstallFixture builds the fixture for one VM instance, with a VM that mounts nothing near the clone.
func newLaunchdInstallFixture(t *testing.T, instance string, opts ...launchdInstallOption) *launchdInstallFixture {
	t.Helper()

	sb := newSandbox(t)
	key := writeInventedKey(t, filepath.Join(sb.root, "keys", "agents-app.pem"), 0o600)

	env := sb.baseEnv()
	env["LIMA_INSTANCE"] = instance
	env["SHIFT2EU_AGENTS_APP_ID"] = launchdAppID
	env["SHIFT2EU_AGENTS_APP_INSTALLATION_ID"] = launchdInstallationID
	env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = key.path

	// Where launchd reads LaunchDaemons from, pointed at the sandbox: the recipe writes the plist under this
	// directory and no test may reach the real /Library (issue #229). It travels on make's command line, never
	// in the environment, since a stale export would install where launchd never looks at boot (issue #229,
	// fourth review).
	launchdDir := filepath.Join(sb.root, "LaunchDaemons")
	if err := os.MkdirAll(launchdDir, 0o700); err != nil {
		t.Fatalf("create %s: %v", launchdDir, err)
	}

	sb.useLogOnlySudo(t)
	// 113 is what launchctl returns for a job it does not know: no job of this instance is loaded yet.
	sb.useFakeLaunchctl(t, env, "113")
	sb.useFakePlutil(t, env, "0")
	sb.useFakeLimactlMounts(t, env, nil)

	dir, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	f := &launchdInstallFixture{sb: sb, key: key, env: env, instance: instance, repo: launchdRepoRoot(t), dir: dir, launchdDir: launchdDir}

	for _, opt := range opts {
		opt(t, f)
	}

	return f
}

// label is the LaunchDaemon's label of the fixture's instance.
func (f *launchdInstallFixture) label() string { return launchdLabelPrefix + f.instance }

// dest is the file launchd reads the job from: inside the sandbox, since the recipe writes it under
// AGENTS_TOKEN_LAUNCHD_DIR and nothing here may reach /Library (issue #229).
func (f *launchdInstallFixture) dest() string {
	return filepath.Join(f.launchdDir, f.label()+".plist")
}

// run runs one target in the fixture's clone with the fixture's environment, never the test process's own, and
// with AGENTS_TOKEN_LAUNCHD_DIR on make's command line, which is one of the only two places the recipe reads it
// from (issue #229, fourth review).
func (f *launchdInstallFixture) run(t *testing.T, target string) (string, string, error) {
	t.Helper()

	return runMakeIn(t, f.dir, []string{"AGENTS_TOKEN_LAUNCHD_DIR=" + f.launchdDir, target}, f.env)
}

// runWithTheDirectoryInTheEnvironment runs one target with AGENTS_TOKEN_LAUNCHD_DIR exported in the environment
// and not on make's command line, the way a stale export in a maintainer's shell would reach it (issue #229,
// fourth review).
func (f *launchdInstallFixture) runWithTheDirectoryInTheEnvironment(t *testing.T, target string) (string, string, error) {
	t.Helper()

	env := make(map[string]string, len(f.env)+1)
	for name, value := range f.env {
		env[name] = value
	}

	env["AGENTS_TOKEN_LAUNCHD_DIR"] = f.launchdDir

	return runMakeIn(t, f.dir, []string{target}, env)
}

// sudoCommands returns the argv of every command the recipe ran through sudo, in call order.
func (f *launchdInstallFixture) sudoCommands(t *testing.T) [][]string {
	t.Helper()

	return invocations(f.sb.argvLogText(t), "sudo")
}

// bootstrapCalls returns the argv of every `sudo launchctl bootstrap` the recipe ran, in call order.
func (f *launchdInstallFixture) bootstrapCalls(t *testing.T) [][]string {
	t.Helper()

	var calls [][]string

	for _, args := range f.sudoCommands(t) {
		if len(args) > 1 && args[0] == "launchctl" && args[1] == "bootstrap" {
			calls = append(calls, args)
		}
	}

	return calls
}

// script is the host clone's refresh script the installed job runs.
func (f *launchdInstallFixture) script() string {
	return filepath.Join(f.repo, "dev", "agents-app-token.sh")
}

// subCommand returns the first sudo invocation whose command is name, failing the test when there is none.
func (f *launchdInstallFixture) subCommand(t *testing.T, name string) []string {
	t.Helper()

	for _, args := range invocations(f.sb.argvLogText(t), "sudo") {
		if len(args) > 0 && args[0] == name {
			return args
		}
	}

	t.Fatalf("the install ran no `sudo %s`; the commands it did run as root: %v", name, invocations(f.sb.argvLogText(t), "sudo"))

	return nil
}

// TestMakeAgentsTokenRefreshInstallRefusesACloneInsideTheVMsMounts pins the reviewer's finding: the clone the
// job runs from must not be one of the directories the VM mounts, and no mount may reach into the clone either.
// The agents write in the mounted workspace, so a job that ran a mounted clone would run their code at every
// boot, as the maintainer, with the App's key in reach. The same goes for a mount the clone itself holds: it
// lets the agents edit the clone's dev/. The install reads the instance's name and mounts with limactl, expands
// `~` and resolves a symlinked location, and refuses before any sudo.
func TestMakeAgentsTokenRefreshInstallRefusesACloneInsideTheVMsMounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		opts          []launchdInstallOption
		mounts        func(t *testing.T, f *launchdInstallFixture) []string
		wantInstalled bool // the install proceeds and installs the job
	}{
		{
			name:   "the clone itself is a mount of the VM",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string { return []string{f.repo} },
		},
		{
			name:   "the clone sits under a mount of the VM",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string { return []string{filepath.Dir(f.repo)} },
		},
		{
			name: "one of several mounts holds the clone",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string {
				return []string{filepath.Join(f.sb.root, "unrelated"), filepath.Dir(f.repo)}
			},
		},
		{
			name: "the VM mounts nothing near the clone",
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string {
				return []string{filepath.Join(f.sb.root, "unrelated")}
			},
			wantInstalled: true,
		},
		{
			// A mount inside the clone is as bad as a mount above it: through the VM the agents can edit the
			// clone's own dev/agents-app-token.sh, which the job runs at every boot (issue #229, second
			// review).
			name:   "a mount inside the clone",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, f *launchdInstallFixture) []string { return []string{filepath.Join(f.repo, "dev")} },
		},
		{
			name:   "a mount inside the clone, written with ~",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, _ *launchdInstallFixture) []string { return []string{"~/clone/dev"} },
		},
		{
			// A bare ~ is HOME, and HOME holds the clone: the install must expand it before it compares.
			name:   "a mount that is HOME, which holds the clone",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, _ *launchdInstallFixture) []string { return []string{"~"} },
		},
		{
			name:   "a mount HOME holds the clone in, written with ~",
			opts:   []launchdInstallOption{inACloneInsideHome()},
			mounts: func(_ *testing.T, _ *launchdInstallFixture) []string { return []string{"~/clone"} },
		},
		{
			// A mount location that is a symlink to a directory holding the clone: the install resolves it,
			// as the VM does when it mounts the directory the link names.
			name: "a mount location that is a symlink to a directory holding the clone",
			opts: []launchdInstallOption{inACloneInsideHome()},
			mounts: func(t *testing.T, f *launchdInstallFixture) []string {
				t.Helper()

				link := filepath.Join(f.sb.root, "home-link")
				if err := os.Symlink(f.env["HOME"], link); err != nil {
					t.Fatalf("symbolic link %s to %s: %v", link, f.env["HOME"], err)
				}

				return []string{link}
			},
		},
		{
			// A mount beside the clone whose path begins with the clone's: a common text prefix is no parent,
			// and the install must not refuse a clone that merely shares a beginning with a mount.
			name:          "a mount beside the clone whose path begins with the clone's",
			opts:          []launchdInstallOption{inACloneInsideHome()},
			mounts:        func(_ *testing.T, f *launchdInstallFixture) []string { return []string{f.repo + "-sibling"} },
			wantInstalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu", tc.opts...)
			f.sb.useFakeLimactlMounts(t, f.env, tc.mounts(t, f))

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")

			// The install asks this instance for its name and mounts, with a format whose output is the
			// instance's name on the first line and one location per line after it: anything else and it cannot
			// tell that limactl answered about this instance, nor whether the clone is among the mounts.
			wantLimactl := []string{"list", "--format", launchdMountsFormat, "shift2eu"}
			if diff := cmp.Diff([][]string{wantLimactl}, invocations(f.sb.argvLogText(t), "limactl")); diff != "" {
				t.Errorf("the install's limactl calls differ from `limactl list --format ... shift2eu`, which prints the instance's name and then one mount location per line (-want +got):\n%s", diff)
			}

			if tc.wantInstalled {
				if err != nil {
					t.Fatalf("make agents-token-refresh-install refused with no mount holding the clone: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
				}

				if args := f.subCommand(t, "install"); args[len(args)-1] != f.dest() {
					t.Errorf("the install put the plist at %q, want %q (issue #229)", args[len(args)-1], f.dest())
				}

				return
			}

			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although a mount of the VM reaches the clone it runs from: the agents can edit that clone through the mount, and the job would run their code at every boot, as the maintainer, with the App's key in reach (issue #229)\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-install ran sudo although a mount of the VM reaches the clone, and every root command it runs must come after that check: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
			}

			// The refusal has to say what to do, and where: the host clone. A maintainer who reads only
			// "refused" would have no idea where to run it instead.
			if !strings.Contains(stderr, "host clone") {
				t.Errorf("the refusal does not say to install the job from the host clone, so a maintainer cannot tell where to run it instead:\nstderr: %s", stderr)
			}

			if !strings.Contains(stderr, f.repo) {
				t.Errorf("the refusal does not name the clone %s it refused, so a maintainer cannot tell which directory the VM mounts:\nstderr: %s", f.repo, stderr)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesAListOfAnotherInstance pins the second review's finding: the install
// must know that limactl answered about the instance it asked about. The format prints the instance's name on
// the first line, so a limactl that fails, answers nothing at all (an instance it does not know) or names
// another instance leaves the install unable to check the clone: it refuses before any sudo, and says which
// instance it could not check.
func TestMakeAgentsTokenRefreshInstallRefusesAListOfAnotherInstance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		answer func(t *testing.T, f *launchdInstallFixture)
	}{
		{
			name: "limactl fails",
			answer: func(t *testing.T, f *launchdInstallFixture) {
				t.Helper()

				f.sb.useFakeLimactlMounts(t, f.env, nil)
				f.sb.useFakeLimactlListFailing(f.env)
			},
		},
		{
			name: "limactl answers nothing, as for an instance it does not know",
			answer: func(t *testing.T, f *launchdInstallFixture) {
				t.Helper()

				f.sb.useFakeLimactlMounts(t, f.env, nil)
				f.sb.useFakeLimactlListEmpty(f.env)
			},
		},
		{
			// The mounts would hold the clone, but they are another instance's: trusting them would let a VM
			// the maintainer never asked about decide whether the install proceeds.
			name: "limactl answers about another instance",
			answer: func(t *testing.T, f *launchdInstallFixture) {
				t.Helper()

				f.sb.useFakeLimactlMounts(t, f.env, []string{filepath.Dir(f.repo)})
				f.sb.useFakeLimactlListNamed(f.env, "another-vm")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")
			tc.answer(t, f)

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although limactl gave it no list of this instance's mounts: it cannot tell whether the clone it runs from is one the VM can edit, so it must refuse\nstdout: %s\nstderr: %s", stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-install ran sudo although it never got a usable list of the instance's mounts, and every root command must come after that check: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
			}

			for _, want := range []string{"limactl", f.instance, "host clone"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("the refusal does not name %q, so a maintainer cannot tell which instance's mounts were unreadable nor where to run the install: \nstderr: %s", want, stderr)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesABadPlistBeforeSudo pins that a plist the generator refuses reaches no
// sudo: root must never be handed a half-rendered or bad job.
func TestMakeAgentsTokenRefreshInstallRefusesABadPlistBeforeSudo(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// launchd starts the job from no directory of its own, so a relative key path is refused.
	f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"] = filepath.Join("keys", "agents-app.pem")

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although the plist generator refuses the relative key path %q\nstdout: %s\nstderr: %s", f.env["SHIFT2EU_AGENTS_APP_KEY_FILE"], stdout, stderr)
	}

	if calls := invocations(f.sb.argvLogText(t), "sudo"); len(calls) != 0 {
		t.Errorf("make agents-token-refresh-install ran sudo although the plist generator refused its input, so root got a job the tests never saw whole: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
	}

	if !strings.Contains(stderr, "SHIFT2EU_AGENTS_APP_KEY_FILE") {
		t.Errorf("the refusal does not name SHIFT2EU_AGENTS_APP_KEY_FILE, so a maintainer cannot tell which value to fix:\nstderr: %s", stderr)
	}
}

// TestMakeAgentsTokenRefreshInstallLintsThePlistBeforeSudo pins that the rendered plist is linted, and the very
// file that lint accepted is the one root installs: a plist launchd cannot parse would load no job, and a
// maintainer would believe the refresh runs at boot when nothing does.
func TestMakeAgentsTokenRefreshInstallLintsThePlistBeforeSudo(t *testing.T) {
	t.Parallel()

	t.Run("the linter sees the plist root installs", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")

		stdout, stderr, err := f.run(t, "agents-token-refresh-install")
		if err != nil {
			t.Fatalf("make agents-token-refresh-install with a plist plutil accepts: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		lints := invocations(f.sb.argvLogText(t), "plutil")
		if len(lints) != 1 {
			t.Fatalf("the install ran plutil %d time(s), want exactly one `plutil -lint <plist>`: %v", len(lints), lints)
		}

		if len(lints[0]) != 2 || lints[0][0] != "-lint" {
			t.Fatalf("the install ran plutil as %v, want `plutil -lint <plist>`: only -lint checks what launchd will parse", lints[0])
		}

		install := f.subCommand(t, "install")
		if len(install) != 9 {
			t.Fatalf("the install ran `sudo %s`, want `sudo install -m 0644 -o root -g wheel <plist> %s`", strings.Join(install, " "), f.dest())
		}

		if install[7] != lints[0][1] {
			t.Errorf("the install lints %q but installs %q: root must install the very plist the linter accepted", lints[0][1], install[7])
		}

		logText := f.sb.argvLogText(t)
		if linted, firstSudo := strings.Index(logText, "tool=plutil"), strings.Index(logText, "tool=sudo"); linted < 0 || firstSudo < 0 || linted > firstSudo {
			t.Errorf("the plist is not linted before the first sudo command: the plutil call is at %d and the first sudo call at %d in the argv log, want plutil first (issue #229)\n%s", linted, firstSudo, logText)
		}
	})

	t.Run("a plist the linter refuses is not installed", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")
		f.sb.useFakePlutil(t, f.env, "1")

		stdout, stderr, err := f.run(t, "agents-token-refresh-install")
		if err == nil {
			t.Fatalf("make agents-token-refresh-install exited 0 although plutil -lint refused the plist: launchd would load no job, and the maintainer would believe the refresh runs at boot\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if calls := invocations(f.sb.argvLogText(t), "sudo"); len(calls) != 0 {
			t.Errorf("make agents-token-refresh-install ran sudo although plutil refused the plist, so root installed a job launchd cannot read: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
		}
	})
}

// TestMakeAgentsTokenRefreshInstallPrintsItsSudoCommands pins what root is asked to do, exactly, and that the
// maintainer sees each of those command lines: an install that changes /Library without showing what it runs
// leaves no way to tell what happened.
func TestMakeAgentsTokenRefreshInstallPrintsItsSudoCommands(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// No job of the instance is loaded, so the install has nothing to boot out first: exactly two root
	// commands, the install of the plist and the bootstrap of the job.
	calls := invocations(f.sb.argvLogText(t), "sudo")
	if len(calls) != 2 {
		t.Fatalf("the install ran %d sudo command(s), want the two that install and load the job: %v", len(calls), calls)
	}

	install := f.subCommand(t, "install")
	if len(install) != 9 {
		t.Fatalf("the install ran `sudo %s`, want `sudo install -m 0644 -o root -g wheel <plist> %s`: the plist is root's and world-readable, as launchd requires of a LaunchDaemon", strings.Join(install, " "), f.dest())
	}

	if diff := cmp.Diff([]string{"install", "-m", "0644", "-o", "root", "-g", "wheel"}, install[:7]); diff != "" {
		t.Errorf("the install's options differ from the ones a LaunchDaemon needs (-want +got):\n%s", diff)
	}

	tmpDir := filepath.Join(f.sb.root, "tmp")
	if !strings.HasPrefix(install[7], tmpDir+string(filepath.Separator)) {
		t.Errorf("the install copies %q, want the plist rendered into %s: nothing else may become the job", install[7], tmpDir)
	}

	if install[8] != f.dest() {
		t.Errorf("the install puts the plist at %q, want %q (issue #229)", install[8], f.dest())
	}

	if diff := cmp.Diff([]string{"launchctl", "bootstrap", "system", f.dest()}, f.subCommand(t, "launchctl")); diff != "" {
		t.Errorf("the install loads the job with a command that differs from `launchctl bootstrap system <plist>` (-want +got):\n%s\nsystem is the domain of a LaunchDaemon: a job bootstrapped into a user's domain starts only when that user logs in (issue #229)", diff)
	}

	// And each of those command lines is printed, exactly as it runs.
	for _, args := range calls {
		want := "+ sudo " + strings.Join(args, " ")
		if !strings.Contains(stdout, want) {
			t.Errorf("the install does not print %q before it runs it, so a maintainer never sees what changed /Library:\nstdout: %s\nstderr: %s", want, stdout, stderr)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallSaysWhichScriptTheJobRuns pins the reviewer's finding: the install says,
// before it runs any sudo command, which script the job will run and that it must be the host clone's. The job
// runs that script at every boot, as the maintainer, with the App's key in reach, and the plist names it, but
// the plist is a wall of XML a maintainer does not read before typing their password.
func TestMakeAgentsTokenRefreshInstallSaysWhichScriptTheJobRuns(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	firstSudo := strings.Index(stdout, "+ sudo install")

	var named bool

	for _, line := range strings.Split(stdout, "\n") {
		if !strings.Contains(line, f.script()) || !strings.Contains(strings.ToLower(line), "host clone") {
			continue
		}

		named = true

		if at := strings.Index(stdout, line); firstSudo >= 0 && at > firstSudo {
			t.Errorf("the install names the script the job runs only after it printed the first sudo command, so a maintainer reads it too late:\nstdout: %s", stdout)
		}
	}

	if !named {
		t.Errorf("the install never says, before the first sudo command, that the job runs %s from this host clone: the job runs that script at every boot, as the maintainer, with the App's key in reach (issue #229)\nstdout: %s\nstderr: %s", f.script(), stdout, stderr)
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesAPlistRootChanged pins the second review's finding: when the file
// root installed is not the plist the install printed, the install fails closed. It removes the plist again,
// loads nothing, and says both, so a maintainer never ends up with a job, or a file named like one, that
// launchd was never asked about - and never with a stale job the next boot would run.
func TestMakeAgentsTokenRefreshInstallRefusesAPlistRootChanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		install string // what the fake root install leaves behind
	}{
		{
			// A file that changed between the hash check and the install: another writer, or a symlink rooted
			// somewhere else, replaced the plist root was handed.
			name:    "root installed other bytes than the plist it was handed",
			install: "different",
		},
		{
			// Nothing readable landed there at all: no file to compare, and none to load.
			name:    "root installed no file at all",
			install: "nothing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")
			f.env["SHIFT2EU_TEST_SUDO_INSTALL"] = tc.install

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although %s: launchd would load a job the maintainer never saw as the printed plist (issue #229)\nstdout: %s\nstderr: %s", tc.name, stdout, stderr)
			}

			if line := "+ sudo rm -f " + f.dest(); !strings.Contains(stdout, line) {
				t.Errorf("the install does not print %q before it runs it, so a maintainer never sees that the plist it could not check was removed again (issue #229, second review)\nstdout: %s\nstderr: %s", line, stdout, stderr)
			}

			if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("the plist %s is still there after the install refused it (stat: %v): the file launchd reads must not stay behind, or the next boot runs a job nobody loaded and nobody checked (issue #229, second review)", f.dest(), statErr)
			}

			if calls := f.bootstrapCalls(t); len(calls) != 0 {
				t.Errorf("the install loaded the job anyway (%v): a plist it could not check must never become a loaded job (issue #229, second review)", calls)
			}

			lower := strings.ToLower(stderr)

			if strings.Contains(lower, "warning") {
				t.Errorf("the install only warns that it cannot check %s: a plist it cannot verify must fail the install, not leave a maybe-job behind:\nstderr: %s", f.dest(), stderr)
			}

			for _, want := range []string{"removed", "no token refresh job", f.instance} {
				if !strings.Contains(lower, strings.ToLower(want)) {
					t.Errorf("the failure does not say %q: a maintainer has to learn that the plist was removed again and that no job of the instance is loaded now (issue #229, second review)\nstderr: %s", want, stderr)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallReplacesALoadedJob pins the reinstall: when the instance's job is loaded
// already, the install boots it out, installs the new plist and bootstraps it again, in that order, and the
// wait for the old job to unload ends as soon as launchctl reports it gone.
func TestMakeAgentsTokenRefreshInstallReplacesALoadedJob(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// The job of this instance is loaded now, and a bootout really unloads it: every launchctl print before
	// the bootout succeeds, every one after it fails, as it does for a job that is gone.
	f.sb.useFakeLaunchctlLoaded(t, f.env, false)
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install over a loaded job: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	want := [][]string{
		{"launchctl", "bootout", "system/" + f.label()},
		f.subCommand(t, "install"),
		{"launchctl", "bootstrap", "system", f.dest()},
	}

	if diff := cmp.Diff(want, f.sudoCommands(t)); diff != "" {
		t.Errorf("the install's sudo commands over a loaded job differ from unloading the old job, installing the new plist and loading it again, in that order (-want +got):\n%s\nloading a plist over a loaded label fails, and the old job would keep running the plist the maintainer replaced (issue #229)", diff)
	}

	install := f.subCommand(t, "install")
	if len(install) != 9 || install[8] != f.dest() {
		t.Errorf("the install ran `sudo %s`, want `sudo install -m 0644 -o root -g wheel <plist> %s`", strings.Join(install, " "), f.dest())
	}

	for _, args := range want {
		if line := "+ sudo " + strings.Join(args, " "); !strings.Contains(stdout, line) {
			t.Errorf("the install does not print %q before it runs it, so a maintainer never sees what changed /Library:\nstdout: %s", line, stdout)
		}
	}

	// The old job unloaded at the first check after the bootout, so nothing had to be waited for.
	if waits := invocations(f.sb.argvLogText(t), "sleep"); len(waits) != 0 {
		t.Errorf("the install waited %d time(s) although the bootout had already unloaded the old job: the wait must end as soon as launchctl reports it gone (issue #229, second review): %v", len(waits), waits)
	}

	if body, readErr := os.ReadFile(f.dest()); readErr != nil || !strings.Contains(string(body), "<plist") {
		t.Errorf("the install left no plist at %s after replacing the loaded job (read: %v): a bootout without a new file leaves the instance with no job at all (issue #229)", f.dest(), readErr)
	}
}

// TestMakeAgentsTokenRefreshInstallStopsWhenTheOldJobStaysLoaded pins the second review's finding: a job that
// never unloads must stop the install before it touches the new plist, after a bounded wait. A second plist
// installed while the old job still runs is a job launchd never loads, and a message that claims no job is
// loaded would be a lie.
func TestMakeAgentsTokenRefreshInstallStopsWhenTheOldJobStaysLoaded(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// The old job is loaded and a bootout leaves it loaded: every launchctl print keeps succeeding.
	f.sb.useFakeLaunchctlLoaded(t, f.env, true)
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although the old job never unloaded: it would install a plist launchd loads over a loaded label, if at all, and report a job the maintainer cannot see running (issue #229, second review)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	want := [][]string{{"launchctl", "bootout", "system/" + f.label()}}
	if diff := cmp.Diff(want, f.sudoCommands(t)); diff != "" {
		t.Errorf("the install ran more root commands than the bootout of the old job although that job stayed loaded (-want +got):\n%s\nnothing may be installed or loaded while the old job still runs (issue #229, second review)\nstdout: %s", diff, stdout)
	}

	if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("the install put a plist at %s although the old job stayed loaded: launchd loads the file when the job next starts, so the maintainer would run a plist nothing ever checked was loaded (issue #229, second review)", f.dest())
	}

	// The wait is bounded: a bootout that never unloads must not hang the install.
	if waits := len(invocations(f.sb.argvLogText(t), "sleep")); waits == 0 || waits > 10 {
		t.Errorf("the install waited %d time(s) for the old job to unload, want between 1 and 10: the wait must be bounded, and it must happen at all (issue #229, second review)", waits)
	}

	lower := strings.ToLower(stderr)

	if !strings.Contains(lower, "still loaded") {
		t.Errorf("the failure does not say that the old job is still loaded, so a maintainer cannot tell why nothing was installed:\nstderr: %s", stderr)
	}

	if strings.Contains(lower, "no token refresh job") {
		t.Errorf("the failure says no token refresh job is loaded although the old one still is: the maintainer would stop looking for a job that is running (issue #229, second review)\nstderr: %s", stderr)
	}
}

// TestMakeAgentsTokenRefreshLaunchdDirComesOnlyFromTheCommandLine pins the fourth review's finding:
// AGENTS_TOKEN_LAUNCHD_DIR may come from make's command line or from the Makefile, never from the environment. A
// stale export in a maintainer's shell (a test's, an earlier session's) would otherwise make root write the plist
// into a directory launchd never reads at boot, and the job would silently not exist after the next reboot. Both
// targets refuse such a run before any sudo, and the refusal names the variable and the environment it came from.
func TestMakeAgentsTokenRefreshLaunchdDirComesOnlyFromTheCommandLine(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"agents-token-refresh-install", "agents-token-refresh-uninstall"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")

			stdout, stderr, err := f.runWithTheDirectoryInTheEnvironment(t, target)
			if err == nil {
				t.Fatalf("make %s exited 0 although AGENTS_TOKEN_LAUNCHD_DIR came from the environment (%s): a stale export would install the job where launchd never looks at boot, and the refresh would not come back after a reboot (issue #229, fourth review)\nstdout: %s\nstderr: %s", target, f.launchdDir, stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make %s ran sudo although AGENTS_TOKEN_LAUNCHD_DIR came from the environment, and the check of where root writes must come first: %v\nstdout: %s\nstderr: %s", target, calls, stdout, stderr)
			}

			if !strings.Contains(stderr, "AGENTS_TOKEN_LAUNCHD_DIR") {
				t.Errorf("the refusal does not name AGENTS_TOKEN_LAUNCHD_DIR, so a maintainer cannot tell which value to fix:\nstderr: %s", stderr)
			}

			if !strings.Contains(strings.ToLower(stderr), "environment") {
				t.Errorf("the refusal does not say that the value came from the environment, so a maintainer would keep looking for it in the Makefile or on the command line:\nstderr: %s", stderr)
			}
		})
	}
}

// TestMakeAgentsTokenRefreshInstallRetriesTheBootstrapOnce pins the second review's finding: a bootstrap that
// fails is retried once, never more, and a failure both times says that no job is loaded now and names both
// ways to restore one. The VM's token lives an hour, so a silent failure costs the agents their credentials.
// The plist root installed stays where launchd reads it, and the failure says that too: the maintainer has to
// know that the next boot loads the job they were told was not loaded (fourth review).
func TestMakeAgentsTokenRefreshInstallRetriesTheBootstrapOnce(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// launchctl bootstrap fails: the job does not load, whatever the plist says.
	f.env["SHIFT2EU_TEST_LAUNCHCTL_BOOTSTRAP_STATUS"] = "1"
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although launchctl bootstrap failed: the maintainer would believe the refresh runs at boot while the VM's token expires within the hour (issue #229)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if calls := f.bootstrapCalls(t); len(calls) != 2 {
		t.Errorf("the install ran launchctl bootstrap %d time(s) with a launchctl that fails, want exactly two, one retry: %v\nstdout: %s\nstderr: %s", len(calls), calls, stdout, stderr)
	}

	lower := strings.ToLower(stderr)

	for _, want := range []string{"no token refresh job", "agents-token-refresh-install", "agents-token-refresh-start"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the failure does not name %q: a maintainer has to learn that no token refresh job is loaded now, and both ways to restore one - run the install again, or start a refresh by hand (issue #229)\nstderr: %s", want, stderr)
		}
	}

	// The plist root installed stays in the directory launchd reads: it was checked and it is there, so the
	// next boot loads it even though the install could not load it now. A maintainer who is not told would
	// think the job is gone for good.
	for _, want := range []string{"next boot", f.dest()} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("the failure does not say %q: the plist stays in the directory launchd reads, so it loads at the next boot although no job is loaded now, and a maintainer who is not told believes the refresh is gone until they install it again (issue #229, fourth review)\nstderr: %s", want, stderr)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallSaysTheJobIsLoadedWhenTheBootstrapLoadedIt pins the fourth review's finding:
// launchctl bootstrap can load the job and still exit non-zero (it loaded the job and then could not answer with
// success). The retry finds the label loaded and fails as well. Saying "no token refresh job is loaded now" would
// then be a lie, and a maintainer would either reinstall over a job that runs or start a second refresh by hand
// beside it. The install says that the job is loaded, names the label and the commands that show it, and exits 0.
func TestMakeAgentsTokenRefreshInstallSaysTheJobIsLoadedWhenTheBootstrapLoadedIt(t *testing.T) {
	t.Parallel()

	f := newLaunchdInstallFixture(t, "shift2eu")
	// The first bootstrap loads the job and still exits non-zero; the retry finds it loaded and exits non-zero
	// too, so both attempts fail although the job is loaded afterwards.
	f.sb.useFakeLaunchctlBootstrapThatLoadsAndFails(t, f.env)
	f.sb.useFakeSleep(t)

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err != nil {
		t.Fatalf("make agents-token-refresh-install exited %v although the job is loaded afterwards, which is what the install is for: a run that loaded the job must not fail and send the maintainer to install it again (issue #229, fourth review)\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if calls := f.bootstrapCalls(t); len(calls) != 2 {
		t.Errorf("the install ran launchctl bootstrap %d time(s) with a bootstrap that fails, want exactly two, one retry: %v\nstdout: %s\nstderr: %s", len(calls), calls, stdout, stderr)
	}

	lower := strings.ToLower(stderr)

	if strings.Contains(lower, "no token refresh job") {
		t.Errorf("the install says no token refresh job is loaded although its own bootstrap loaded one: the maintainer would install it again over the job that runs, or start a second refresh beside it (issue #229, fourth review)\nstderr: %s", stderr)
	}

	for _, want := range []string{"loaded", f.label()} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("the install does not say %q on stderr, although the job is loaded: a maintainer has to learn that the Token refresh job runs now, and which label to look at with launchctl print (issue #229, fourth review)\nstderr: %s", want, stderr)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesAPlistItCannotRead pins the other half of the read-back the second
// review added: a file root installed that is there but cannot be read is as unloaded as no file at all, and the
// install must fail closed for it too. It removes the plist again (a plist it cannot check must not stay behind
// for the next boot), loads nothing, and says both. The unreadable file is the one branch `nothing` only reached
// by absence, which is a different failure: a file that is there and unreadable is one root installed and nobody
// checked.
func TestMakeAgentsTokenRefreshInstallRefusesAPlistItCannotRead(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions, so this test cannot make the installed plist unreadable")
	}

	f := newLaunchdInstallFixture(t, "shift2eu")
	// The fake root install copies the plist and takes every permission away: the file is there, and the
	// install's read-back, run as the maintainer, cannot open it.
	f.env["SHIFT2EU_TEST_SUDO_INSTALL"] = "unreadable"

	stdout, stderr, err := f.run(t, "agents-token-refresh-install")
	if err == nil {
		t.Fatalf("make agents-token-refresh-install exited 0 although the plist root installed cannot be read: launchd would load a job the maintainer never saw as the printed plist, and the next boot would run it (issue #229)\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	if line := "+ sudo rm -f " + f.dest(); !strings.Contains(stdout, line) {
		t.Errorf("the install does not print %q before it runs it, so a maintainer never sees that the plist it could not read was removed again (issue #229, second review)\nstdout: %s\nstderr: %s", line, stdout, stderr)
	}

	if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("the plist %s is still there after the install refused it (stat: %v): the file launchd reads must not stay behind, or the next boot runs a job nobody checked (issue #229, second review)", f.dest(), statErr)
	}

	if calls := f.bootstrapCalls(t); len(calls) != 0 {
		t.Errorf("the install loaded the job anyway (%v): a plist it could not read must never become a loaded job (issue #229, second review)", calls)
	}

	lower := strings.ToLower(stderr)

	for _, want := range []string{"could not be read", "no token refresh job"} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("the failure does not say %q: a maintainer has to learn that the plist could not be read back, and that no job of the instance is loaded now (issue #229, second review)\nstderr: %s", want, stderr)
		}
	}
}

// TestMakeAgentsTokenRefreshInstallRefusesASymlinkedRefreshScript pins the second review's finding: the job runs
// <clone>/dev/agents-app-token.sh at every boot, as the maintainer, with the App's key in reach. A dev/ that is
// a symlink, or a script that is one, points the job at a file the install never checked, so the install refuses
// both before any sudo.
func TestMakeAgentsTokenRefreshInstallRefusesASymlinkedRefreshScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		symlink string                           // the path inside the clone that is a symlink
		build   func(t *testing.T, clone string) // the rest of the clone, on top of the Makefile
		want    string                           // the symlink the refusal must name, below the clone
	}{
		{
			name: "the refresh script is a symlink",
			build: func(t *testing.T, clone string) {
				t.Helper()

				writeFileMode(t, filepath.Join(clone, launchdScript), []byte(readRepoFile(t, launchdScript)), 0o755)

				if err := os.Symlink(scriptPath(t, agentsMintScript), filepath.Join(clone, agentsMintScript)); err != nil {
					t.Fatalf("symbolic link %s: %v", filepath.Join(clone, agentsMintScript), err)
				}
			},
			want: filepath.Join("dev", "agents-app-token.sh"),
		},
		{
			name: "the clone's dev directory is a symlink",
			build: func(t *testing.T, clone string) {
				t.Helper()

				if err := os.Symlink(filepath.Dir(scriptPath(t, agentsMintScript)), filepath.Join(clone, "dev")); err != nil {
					t.Fatalf("symbolic link %s: %v", filepath.Join(clone, "dev"), err)
				}
			},
			want: "dev",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newLaunchdInstallFixture(t, "shift2eu")

			clone := filepath.Join(f.sb.root, "clone")
			writeFileMode(t, filepath.Join(clone, makefilePath), []byte(readRepoFile(t, makefilePath)), 0o600)
			tc.build(t, clone)

			f.dir, f.repo = clone, resolvedPath(t, clone)

			stdout, stderr, err := f.run(t, "agents-token-refresh-install")
			if err == nil {
				t.Fatalf("make agents-token-refresh-install exited 0 although %s is a symlink: the job would run a file outside the clone at every boot, as the maintainer, with the App's key in reach (issue #229, second review)\nstdout: %s\nstderr: %s", tc.want, stdout, stderr)
			}

			if calls := f.sudoCommands(t); len(calls) != 0 {
				t.Errorf("make agents-token-refresh-install ran sudo although %s is a symlink, and every root command must come after that check: %v\nstdout: %s\nstderr: %s", tc.want, calls, stdout, stderr)
			}

			lower := strings.ToLower(stderr)

			for _, want := range []string{"symlink", f.repo} {
				if !strings.Contains(lower, strings.ToLower(want)) {
					t.Errorf("the refusal does not name %q: a maintainer has to learn that the clone's own path is a symlink, and which clone was refused (issue #229, second review)\nstderr: %s", want, stderr)
				}
			}
		})
	}
}

// TestMakeAgentsTokenRefreshUninstall pins that uninstalling needs a launchctl - the LaunchDaemon is a macOS
// thing, and removing the file without booting the job out of launchd leaves it running - and that it boots the
// job out and removes its file through sudo, one label per instance.
func TestMakeAgentsTokenRefreshUninstall(t *testing.T) {
	t.Parallel()

	t.Run("no launchctl is a refusal before any sudo", func(t *testing.T) {
		t.Parallel()

		// The fake launchctl is not on this fixture's PATH, so make agents-token-refresh-uninstall runs on a
		// host that has none, as Linux CI does; the log-only sudo stays reachable.
		f := newLaunchdInstallFixture(t, "shift2eu", withoutOnPath("launchctl"))

		stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
		if err == nil {
			t.Fatalf("make agents-token-refresh-uninstall exited 0 on a host with no launchctl: the LaunchDaemon is for the Mac, and the target must say so and run nothing\nstdout: %s\nstderr: %s", stdout, stderr)
		}

		if calls := invocations(f.sb.argvLogText(t), "sudo"); len(calls) != 0 {
			t.Errorf("make agents-token-refresh-uninstall ran sudo on a host with no launchctl: %v\nstdout: %s\nstderr: %s", calls, stdout, stderr)
		}

		for _, want := range []string{"LaunchDaemon", "Mac"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("the refusal does not name %q, so a maintainer on another host cannot tell why nothing was removed:\nstderr: %s", want, stderr)
			}
		}
	})

	t.Run("with launchctl: boot the job out and remove its plist through sudo", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")

		stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
		if err != nil {
			t.Fatalf("make agents-token-refresh-uninstall: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		want := [][]string{
			{"launchctl", "bootout", "system/" + f.label()},
			{"rm", "-f", f.dest()},
		}

		if diff := cmp.Diff(want, invocations(f.sb.argvLogText(t), "sudo")); diff != "" {
			t.Errorf("the uninstall's sudo commands differ from booting the instance's job out of the system domain and removing its plist (-want +got):\n%s\nbootout takes the job's service target, system/<label>, so the job that stops is this instance's, and rm removes only the plist this instance's label names: another instance's job and file stay (issue #229)", diff)
		}

		for _, args := range want {
			if line := "+ sudo " + strings.Join(args, " "); !strings.Contains(stdout, line) {
				t.Errorf("the uninstall does not print %q before it runs it:\nstdout: %s\nstderr: %s", line, stdout, stderr)
			}
		}
	})

	t.Run("a stuck bootout: the job is still loaded, and the plist goes anyway", func(t *testing.T) {
		t.Parallel()

		f := newLaunchdInstallFixture(t, "shift2eu")
		// The job is loaded and a bootout leaves it loaded: launchctl print keeps succeeding after the bootout,
		// as it does for a job launchd never unloaded (issue #229, fourth review).
		f.sb.useFakeLaunchctlLoaded(t, f.env, true)
		f.sb.useFakeSleep(t)

		// A plist is there to remove, so "the plist goes anyway" is a removal and not a file that never existed.
		writeFileMode(t, f.dest(), []byte("<plist/>"), 0o644)

		stdout, stderr, err := f.run(t, "agents-token-refresh-uninstall")
		// Exit 0: the uninstall's purpose is to leave no plist behind for the next boot, and it did. The job that
		// is still loaded is the one thing the maintainer has to finish by hand, and re-running the target would
		// change nothing: a failure would send them looking for a retry that cannot work. The message names the
		// command that does (issue #229, fourth review).
		if err != nil {
			t.Fatalf("make agents-token-refresh-uninstall exited %v although it removed the plist and only a bootout the maintainer runs by hand is left\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if line := "+ sudo rm -f " + f.dest(); !strings.Contains(stdout, line) {
			t.Errorf("the uninstall does not print %q before it runs it, so a maintainer never sees that the plist was removed although the job stays loaded (issue #229, fourth review)\nstdout: %s\nstderr: %s", line, stdout, stderr)
		}

		if _, statErr := os.Stat(f.dest()); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("the plist %s is still there although the uninstall ran (stat: %v): a job that cannot be unloaded must not leave its plist for the next boot to load again (issue #229, fourth review)", f.dest(), statErr)
		}

		lower := strings.ToLower(stderr)

		if strings.Contains(lower, "was not loaded") {
			t.Errorf("the uninstall says the job was not loaded although launchctl still reports it loaded: the maintainer would believe the refresh is stopped while it keeps running with the App's key in reach (issue #229, fourth review)\nstderr: %s", stderr)
		}

		// The wait is bounded: a bootout that never unloads must not hang the uninstall, and it must happen at all.
		if waits := len(invocations(f.sb.argvLogText(t), "sleep")); waits == 0 || waits > 10 {
			t.Errorf("the uninstall waited %d time(s) for the job to unload, want between 1 and 10: the wait must be bounded, and it must happen at all (issue #229, fourth review)", waits)
		}

		for _, want := range []string{"still loaded", "sudo launchctl bootout", f.label()} {
			if !strings.Contains(lower, strings.ToLower(want)) {
				t.Errorf("the uninstall does not say %q: the job is still loaded, and the command that unloads it is the one thing the maintainer has left to run (issue #229, fourth review)\nstderr: %s", want, stderr)
			}
		}
	})
}

// launchctlPrintHeader renders the lines `launchctl print system/<label>` prints for any loaded LaunchDaemon,
// with the `pid = N` line it adds for a job whose process is running: pid 0 prints no line, as launchctl prints
// none for a loaded job whose process is not running. Every test builds its print output from this header, so the
// block's shape lives here.
func launchctlPrintHeader(label string, pid int) string {
	header := "system/" + label + " = {\n" +
		"\tactive count = 1\n" +
		"\tpath = /Library/LaunchDaemons/" + label + ".plist\n" +
		"\ttype = LaunchDaemon\n" +
		"\tstate = running\n"

	if pid != 0 {
		header += "\tpid = " + strconv.Itoa(pid) + "\n"
	}

	return header
}

// launchctlPrintBlock renders what `launchctl print system/<label>` prints for a loaded job: the header
// launchctlPrintHeader renders and, below it, a `program` line and an `arguments` block that holds one argument
// per line, tab-indented, the way the real command prints a job's arguments. The start target reads the script
// the job runs from that block.
func launchctlPrintBlock(label, script, log string) string {
	return launchctlPrintHeader(label, 0) +
		"\n" +
		"\tprogram = /bin/sh\n" +
		"\targuments = {\n" +
		"\t\t/bin/sh\n" +
		"\t\t-c\n" +
		"\t\tumask 077 && mkdir -p \"${1%/*}\" && exec /bin/sh \"$0\" refresh >>\"$1\" 2>&1\n" +
		"\t\t" + script + "\n" +
		"\t\t" + log + "\n" +
		"\t}\n" +
		"}\n"
}

// TestMakeAgentsTokenRefreshStartWarnsAboutAnotherClonesJob pins the reviewer's finding: launchctl print says
// which script the loaded job runs, and when that is not this clone's dev/agents-app-token.sh the warning says
// so and names the script. Otherwise a maintainer reads "a LaunchDaemon runs this VM's refresh" and believes it
// runs the clone they edit.
func TestMakeAgentsTokenRefreshStartWarnsAboutAnotherClonesJob(t *testing.T) {
	t.Parallel()

	label := launchdLabelPrefix + "shift2eu"
	repo := launchdRepoRoot(t)
	otherScript := filepath.Join(t.TempDir(), "another-clone", "dev", "agents-app-token.sh")

	cases := []struct {
		name       string
		script     string
		wantWarned bool
	}{
		{name: "the job runs another clone's script", script: otherScript, wantWarned: true},
		{name: "the job runs this clone's script", script: filepath.Join(repo, "dev", "agents-app-token.sh")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newTokenFixture(t, "shift2eu")
			log := filepath.Join(f.state, "agents-token-refresh-shift2eu.log")
			f.sb.useFakeLaunchctlPrint(t, f.env, "0", launchctlPrintBlock(label, tc.script, log))

			stdout, stderr, err := runMake(t, []string{"agents-token-refresh-start"}, f.env)
			if err != nil {
				t.Fatalf("make agents-token-refresh-start with the LaunchDaemon %s loaded: %v\nstdout: %s\nstderr: %s", label, err, stdout, stderr)
			}

			// The warning about the loaded job is there either way, and it still starts nothing.
			for _, want := range []string{"LaunchDaemon", label} {
				if !strings.Contains(stderr, want) {
					t.Errorf("make agents-token-refresh-start does not name %q on stderr although launchd runs the refresh as %s (issue #229)\nstdout: %s\nstderr: %s", want, label, stdout, stderr)
				}
			}

			if tc.wantWarned {
				var named bool

				for _, line := range strings.Split(stderr, "\n") {
					if !strings.Contains(line, tc.script) {
						continue
					}

					named = true

					if !strings.Contains(strings.ToLower(line), "another clone") {
						t.Errorf("the warning names %s but does not say it is another clone's script, so a maintainer still believes the job runs this clone's:\nstderr: %s", tc.script, stderr)
					}
				}

				if !named {
					t.Errorf("make agents-token-refresh-start does not name %s on stderr, the script the loaded LaunchDaemon runs, so a maintainer edits one clone while the job runs another's (issue #229)\nstdout: %s\nstderr: %s", tc.script, stdout, stderr)
				}
			} else if strings.Contains(strings.ToLower(stderr), "another clone") {
				t.Errorf("make agents-token-refresh-start warns about another clone's script although the loaded job runs this clone's %s:\nstderr: %s", tc.script, stderr)
			}

			if _, err := os.Stat(f.statePath("agents-token-refresh-shift2eu.pid")); err == nil {
				t.Errorf("make agents-token-refresh-start started a second refresher beside the LaunchDaemon: it wrote a pid file (issue #229)")
			}
		})
	}
}

// runMake runs make in the repository root with args and env alone, never the test process's own
// environment.
func runMake(t *testing.T, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}

	return runMakeIn(t, root, args, env)
}

// runMakeIn runs make in dir with args and env alone, never the test process's own environment: the
// vm-gh-wrapper tests copy the Makefile and the script it runs into a temporary directory, so one recipe can
// be run against a stand-in generator.
func runMakeIn(t *testing.T, dir string, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	if _, err := exec.LookPath("make"); err != nil {
		t.Fatalf("make is not installed: the repository's commands, and this test, need it")
	}

	cmd := exec.Command("make", append([]string{"-C", dir, "--no-print-directory"}, args...)...)
	cmd.Env = envList(env)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// makeEnv is the environment make runs with in a test: a PATH of the machine's own and a temporary HOME, so
// the Makefile's $(shell git config ...) reads nothing of the maintainer's, and a WORKSPACE_REPO naming a
// checkout inside a temporary workspace, so a host target runs in the host clone layout a maintainer has and
// not in the workspace it refuses (docs/dev-environment.md, The host clone, issue #227).
func makeEnv(t *testing.T, extra map[string]string) map[string]string {
	t.Helper()

	workspaceRepo := filepath.Join(t.TempDir(), "ws", "shift2eu")

	if err := os.MkdirAll(workspaceRepo, 0o700); err != nil {
		t.Fatalf("create %s: %v", workspaceRepo, err)
	}

	env := map[string]string{
		"PATH":           "/usr/local/bin:/usr/bin:/bin",
		"HOME":           t.TempDir(),
		"WORKSPACE_REPO": workspaceRepo,
	}

	for name, value := range extra {
		env[name] = value
	}

	return env
}

// makeVariable returns the value a substitution assignment in the Makefile gives name.
func makeVariable(body, name string) (string, bool) {
	match := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*[?:]?=\s*(.*)$`).FindStringSubmatch(body)
	if match == nil {
		return "", false
	}

	return strings.TrimSpace(match[1]), true
}
