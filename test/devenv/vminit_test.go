// Pins that vm-init runs the git identity step of PRD #161 milestone 1 through dev/vm-git-identity.sh, whose
// behaviour is tested for real in vmgitidentity_test.go (review item 5), and that it sets the deck's
// default_dir to the main checkout (issue #234).
//
// vm-init itself cannot be run in make test: its first step refuses to run outside the Lima VM unless
// /etc/profile.d/shift2eu.sh exists, and a test cannot create that file without root and without the VM's own
// provisioning. So the file's calls - the identity step's and the deck's default_dir - are pinned here, and the
// behaviour they feed is checked by each script's own tests.
package devenv_test

import (
	"slices"
	"strings"
	"testing"
)

func TestVMInitRunsTheGitIdentityScript(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, vmInitScript)

	// The identity step is a script of its own, so its switch-over cases can be tested without a VM: vm-init
	// must not carry a copy of that logic, or the tested script is not the one that runs.
	at := strings.Index(body, "vm-git-identity.sh")
	if at < 0 {
		t.Fatalf("%s does not run dev/vm-git-identity.sh: with an identity file in the VM the agents commit as the App's bot, and with it gone they commit as the identity vm-init was given (PRD #161 milestone 1, review item 5)", vmInitScript)
	}

	// The two fallbacks git user.name and user.email are the script's arguments: the line that runs it must
	// hand it both, or the rollback to the maintainer's identity has nothing to fall back to.
	line := body[strings.LastIndex(body[:at], "\n")+1:]
	if end := strings.Index(line, "\n"); end >= 0 {
		line = line[:end]
	}

	if got := strings.Count(line, "$"); got < 2 {
		t.Errorf("%s runs the identity step without two arguments: %q. It passes the fallback user.name and user.email, which the step writes when the VM has no identity file", vmInitScript, strings.TrimSpace(line))
	}
}

// TestVMInitSetsTheDeckDefaultDir pins issue #234's first half: vm-init sets the deck's default_dir to the main
// checkout it is given as its second argument, with `dot-agent-deck config set default_dir`, after the deck
// itself answers and before the daemon service is installed or started. Without it, Ctrl+N in a new VM opens
// the picker in the VM user's home instead of the repository, and an agent opened there has no CLAUDE.md, no
// roles and no dispatcher memory.
//
// vm-init cannot be run in make test (see the package comment above), so the call, its argument, its place in
// the run and the fact that it is made on every run are pinned in the file, the way the identity step's call
// is.
func TestVMInitSetsTheDeckDefaultDir(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, vmInitScript)
	lines := strings.Split(body, "\n")

	// lineWith returns the number of the one line that holds substr, and -1 when there is none.
	lineWith := func(substr string) int {
		t.Helper()

		return slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, substr) })
	}

	// The main checkout is the second argument (Makefile's vm-init target passes it): the default_dir must be
	// that directory, not the workspace and not a path vm-init works out for itself.
	if lineWith(`main_checkout="$2"`) < 0 && !strings.Contains(body, `"$2"`) {
		t.Errorf("%s does not take the main checkout from its second argument, so the default_dir it sets is not the directory a maintainer's Ctrl+N must start in (issue #234)", vmInitScript)
	}

	set := lineWith("config set default_dir")
	if set < 0 {
		t.Fatalf("%s never runs `dot-agent-deck config set default_dir`: a new VM's Ctrl+N opens the picker in the VM user's home, which holds only go/, so every agent has to climb to / and walk down to the checkout, and one opened in the home gets no CLAUDE.md, no roles from .dot-agent-deck.toml and no dispatcher memory (issue #234)", vmInitScript)
	}

	setLine := lines[set]
	if !strings.Contains(setLine, "main_checkout") && !strings.Contains(setLine, `"$2"`) {
		t.Errorf("%s sets default_dir with %q, which does not name the main checkout the script is given as its second argument: Ctrl+N must start in the repository, an absolute path that exists in the VM (issue #234)", vmInitScript, strings.TrimSpace(setLine))
	}

	if !strings.Contains(setLine, `"`) {
		t.Errorf("%s sets default_dir without quoting the path: %q. A checkout whose path holds a space would set half of it (issue #234)", vmInitScript, strings.TrimSpace(setLine))
	}

	// The deck answers before its configuration is set - the `validate` step - and the daemon service, which
	// starts the deck and reads the setting, comes last: setting default_dir after the daemon install would
	// leave the first deck started with the old value.
	validate := lineWith("dot-agent-deck validate")
	if validate < 0 {
		t.Fatalf("%s no longer validates .dot-agent-deck.toml, so this test cannot pin that default_dir is set once the deck works", vmInitScript)
	}

	if set < validate {
		t.Errorf("%s sets default_dir at line %d, before it checks that the deck works at line %d: the setting belongs after the deck's own steps (issue #234)", vmInitScript, set+1, validate+1)
	}

	daemon := lineWith("is-enabled dot-agent-deck.service")
	if daemon < 0 {
		t.Fatalf("%s no longer installs the daemon service, so this test cannot pin that default_dir is set before it starts", vmInitScript)
	}

	if set > daemon {
		t.Errorf("%s sets default_dir at line %d, after the daemon service step at line %d: the service starts the deck, so the setting must be in place before it is installed or the script must restart the service afterwards so it reads the value (issue #234)", vmInitScript, set+1, daemon+1)
	}

	// Re-running make vm-init is harmless and keeps the value: the setting is made on every run, with the same
	// argument, so the script never reads it back and leaves a changed value in the VM (issue #234).
	if at := lineWith("config get default_dir"); at >= 0 {
		t.Errorf("%s reads default_dir back with `dot-agent-deck config get` at line %d: every run must make the same `config set default_dir` call with the same argument, so a re-run is harmless and never skips the setting (issue #234)", vmInitScript, at+1)
	}
}
