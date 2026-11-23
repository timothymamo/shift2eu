// Pins where the Chromium `make test-browser` starts opens DevTools (issue #368, ADR 0036; issue #423 item 1).
// The recipe of the target must pass --remote-debugging-port=0, so the kernel picks the port, and
// --remote-debugging-address=127.0.0.1, so DevTools listens on the loopback interface only: the fixed,
// unauthenticated port 9222 was the audit finding issue #368 removed, and until now only a Makefile comment and
// ADR 0036 recorded the decision, so an edit that brought the port back passed every check. The check reads the
// test-browser target's recipe alone, so neither a comment nor another target's flags count, and it reports a
// fixed or missing port, an address that is not the loopback one (0.0.0.0, ::, localhost) or missing, and a flag
// the recipe passes twice with two different values.
//
// It pins the recipe's signal traps too (issue #464): each of the three must ignore HUP, INT and TERM as its first
// action, before it exits, so a second signal cannot end the shell between the trap's exit and the cleanup its EXIT
// trap runs. When the whole process group is terminated, as closing the pane does, make relays a SIGTERM to the
// recipe's shell, so a run does get that second one in the wild.
package devenv_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// testBrowserTarget is the Makefile target whose recipe starts the site's headless Chromium and runs the smoke
// test against it (PRD #297 milestone 2, ADR 0036).
const testBrowserTarget = "test-browser"

// devtoolsPinIssue names what the two flags are pinned for: the decision issue #368 took, which issue #423 makes
// testable. Every problem the check reports ends with it.
const devtoolsPinIssue = "issue #368 and #423, ADR 0036"

// devtoolsFlag is one Chromium DevTools flag the recipe of make test-browser must pass, the one value it may pass
// with it, and what passing another value costs.
type devtoolsFlag struct {
	flag        string
	value       string
	consequence string
}

// pinnedDevtoolsFlags are the flags the recipe must pass, in the order the check reports them: the port first,
// the port being the finding of the audit that started this.
var pinnedDevtoolsFlags = []devtoolsFlag{
	{
		flag:        "--remote-debugging-port",
		value:       "0",
		consequence: "the kernel must pick the port, or a fixed one is open to every process on the host",
	},
	{
		flag:        "--remote-debugging-address",
		value:       "127.0.0.1",
		consequence: "DevTools must listen on the loopback interface only, never on every interface of the host",
	},
}

// devtoolsFlagProblems returns what the recipe of make test-browser gets wrong about the DevTools flags
// pinnedDevtoolsFlags names, one message per fault, each naming the flag and the value the recipe passes: empty
// when the recipe passes every pinned value at least once and passes no other value with any of the flags.
func devtoolsFlagProblems(t *testing.T, makefile string) []string {
	t.Helper()

	recipe, ok := makeRecipes(t, makefile)[testBrowserTarget]
	if !ok {
		return []string{fmt.Sprintf("the Makefile declares no %s target with a recipe, so nothing pins where its Chromium opens DevTools (%s)",
			testBrowserTarget, devtoolsPinIssue)}
	}

	var problems []string

	for _, pinned := range pinnedDevtoolsFlags {
		values := devtoolsFlagValues(recipe, pinned.flag)

		if len(values) == 0 {
			problems = append(problems, fmt.Sprintf("the recipe of make %s passes no %s=%s: %s (%s)",
				testBrowserTarget, pinned.flag, pinned.value, pinned.consequence, devtoolsPinIssue))

			continue
		}

		for _, value := range values {
			if value != pinned.value {
				problems = append(problems, fmt.Sprintf("the recipe of make %s passes %s=%s, want %s=%s: %s (%s)",
					testBrowserTarget, pinned.flag, value, pinned.flag, pinned.value, pinned.consequence, devtoolsPinIssue))
			}
		}
	}

	return problems
}

// devtoolsFlagValues returns the values the recipe passes with one DevTools flag, in order and each as written.
// Chromium accepts --remote-debugging-port=9222 and --remote-debugging-port 9222 alike, so both spellings are a
// value here: a check that read the = spelling alone would let a port passed as a separate word through.
func devtoolsFlagValues(recipe, flag string) []string {
	withValue := regexp.MustCompile(regexp.QuoteMeta(flag) + `(?:=|[ \t]+)([^ \t\\]*)`)

	matches := withValue.FindAllStringSubmatch(recipe, -1)
	values := make([]string, 0, len(matches))

	for _, match := range matches {
		values = append(values, match[1])
	}

	return values
}

// TestDevtoolsFlagProblems pins the check itself, over recipes rather than the real Makefile: a recipe that
// passes the two flags as issue #368 fixed them has no problem - in a comment, in another target or passed twice
// with the value it must keep, neither - and every way of passing a fixed port, an address that is not the
// loopback one or no value at all is reported, naming the flag and the value found.
func TestDevtoolsFlagProblems(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// flags go on Chromium's command line of browserRecipeMakefile.
		flags string
		// comments are tab-indented recipe comment lines browserRecipeMakefile adds, so a comment naming a flag
		// is no flag.
		comments []string
		// makefile, when set, is the whole Makefile text, in place of browserRecipeMakefile's.
		makefile string
		want     []string
	}{
		{
			name:  "the two flags as issue #368 fixed them",
			flags: "--remote-debugging-address=127.0.0.1 --remote-debugging-port=0",
		},
		{
			name:  "a recipe comment naming a fixed port and every interface",
			flags: "--remote-debugging-address=127.0.0.1 --remote-debugging-port=0",
			comments: []string{
				"--remote-debugging-port=9222 is the port issue #368 removed",
				"--remote-debugging-address=0.0.0.0 would open DevTools to the network",
			},
		},
		{
			name:  "each flag passed twice with the value it must keep",
			flags: "--remote-debugging-port=0 --remote-debugging-address=127.0.0.1 --remote-debugging-port=0",
		},
		{
			name:  "a fixed port",
			flags: "--remote-debugging-address=127.0.0.1 --remote-debugging-port=9222",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-port=9222, want --remote-debugging-port=0: the kernel must pick the port, or a fixed one is open to every process on the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "a fixed port passed as a separate word",
			flags: "--remote-debugging-address=127.0.0.1 --remote-debugging-port 9222",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-port=9222, want --remote-debugging-port=0: the kernel must pick the port, or a fixed one is open to every process on the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "no port at all",
			flags: "--remote-debugging-address=127.0.0.1",
			want: []string{
				"the recipe of make test-browser passes no --remote-debugging-port=0: the kernel must pick the port, or a fixed one is open to every process on the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "every interface as the address",
			flags: "--remote-debugging-address=0.0.0.0 --remote-debugging-port=0",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-address=0.0.0.0, want --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "the IPv6 any address",
			flags: "--remote-debugging-address=:: --remote-debugging-port=0",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-address=::, want --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "localhost as the address",
			flags: "--remote-debugging-address=localhost --remote-debugging-port=0",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-address=localhost, want --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "no address at all",
			flags: "--remote-debugging-port=0",
			want: []string{
				"the recipe of make test-browser passes no --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "a good and a fixed port, in that order",
			flags: "--remote-debugging-port=0 --remote-debugging-address=127.0.0.1 --remote-debugging-port=9222",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-port=9222, want --remote-debugging-port=0: the kernel must pick the port, or a fixed one is open to every process on the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "a good and an exposed address, in that order",
			flags: "--remote-debugging-address=127.0.0.1 --remote-debugging-port=0 --remote-debugging-address=0.0.0.0",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-address=0.0.0.0, want --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:  "both flags wrong, the port reported first",
			flags: "--remote-debugging-address=0.0.0.0 --remote-debugging-port=9222",
			want: []string{
				"the recipe of make test-browser passes --remote-debugging-port=9222, want --remote-debugging-port=0: the kernel must pick the port, or a fixed one is open to every process on the host (issue #368 and #423, ADR 0036)",
				"the recipe of make test-browser passes --remote-debugging-address=0.0.0.0, want --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:     "no test-browser recipe at all",
			makefile: ".PHONY: site\nsite:\n\t@echo site\n",
			want: []string{
				"the Makefile declares no test-browser target with a recipe, so nothing pins where its Chromium opens DevTools (issue #368 and #423, ADR 0036)",
			},
		},
		{
			name:     "a test-browser target with no recipe",
			makefile: ".PHONY: test-browser\ntest-browser:\n",
			want: []string{
				"the Makefile declares no test-browser target with a recipe, so nothing pins where its Chromium opens DevTools (issue #368 and #423, ADR 0036)",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			makefile := tc.makefile
			if makefile == "" {
				makefile = browserRecipeMakefile(tc.flags, tc.comments...)
			}

			if diff := cmp.Diff(tc.want, devtoolsFlagProblems(t, makefile)); diff != "" {
				t.Errorf("devtoolsFlagProblems (%s) (-want +got):\n%s\nMakefile:\n%s", tc.name, diff, makefile)
			}
		})
	}
}

// TestMakeTestBrowserRecipeKeepsDevToolsOnAPickedPortOfTheLoopbackInterface argues the check no longer passes
// on its own: it runs on the real Makefile, which must pass, and then on the real Makefile with each of the two
// flags edited the way issue #368 removed, which must report that edit and nothing else. A Makefile the check
// stopped reading would pass with no problem, and the edits are how a fixed port or an open address would
// really come back.
func TestMakeTestBrowserRecipeKeepsDevToolsOnAPickedPortOfTheLoopbackInterface(t *testing.T) {
	t.Parallel()

	real := readRepoFile(t, makefilePath)

	t.Run("the real Makefile passes", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff([]string(nil), devtoolsFlagProblems(t, real)); diff != "" {
			t.Errorf("the recipe of make test-browser (-want +got):\n%s", diff)
		}
	})

	for _, tc := range []struct {
		name string
		// old is what the real Makefile passes, new what an edit brings back in its place.
		old, new string
		want     string
	}{
		{
			name: "the fixed DevTools port comes back",
			old:  "--remote-debugging-port=0",
			new:  "--remote-debugging-port=9222",
			want: "the recipe of make test-browser passes --remote-debugging-port=9222, want --remote-debugging-port=0: the kernel must pick the port, or a fixed one is open to every process on the host (issue #368 and #423, ADR 0036)",
		},
		{
			name: "the address moves off the loopback interface",
			old:  "--remote-debugging-address=127.0.0.1",
			new:  "--remote-debugging-address=0.0.0.0",
			want: "the recipe of make test-browser passes --remote-debugging-address=0.0.0.0, want --remote-debugging-address=127.0.0.1: DevTools must listen on the loopback interface only, never on every interface of the host (issue #368 and #423, ADR 0036)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			edited := strings.ReplaceAll(real, tc.old, tc.new)
			if edited == real {
				t.Fatalf("the Makefile no longer passes %s, so this edit changes nothing and the check's reading of the real recipe is unproven", tc.old)
			}

			if diff := cmp.Diff([]string{tc.want}, devtoolsFlagProblems(t, edited)); diff != "" {
				t.Errorf("the Makefile with %s in place of %s (-want +got):\n%s", tc.new, tc.old, diff)
			}
		})
	}
}

// signalTrap is one of the three signals the recipe of make test-browser traps, and the status its trap must exit
// with: 128 plus the signal's number, as a shell leaves for a command a signal killed.
type signalTrap struct {
	name   string // the signal, as trap names it
	status string // the status the trap exits with, for a run a maintainer reads the exit code of
}

// trappedSignals are the signals the recipe traps, in the order it sets them: a hang-up (what closing the pane
// sends), an interrupt and a termination.
var trappedSignals = []signalTrap{
	{name: "HUP", status: "129"},
	{name: "INT", status: "130"},
	{name: "TERM", status: "143"},
}

// signalRaceIssue names what the three traps are pinned for: the second signal a run gets in the wild (issue #464).
const signalRaceIssue = "issue #464"

// TestMakeTestBrowserRecipeIgnoresFurtherSignalsBeforeItExits pins the trap of each of the three signals the recipe
// of make test-browser sets (issue #464). A trapped signal hands the shell to the EXIT trap, which removes the
// Chromium container and the bundle directory of the run, and that trap ignores the three signals itself so a second
// one arriving while it cleans up costs none of it. The trap's own exit is the other way to lose that cleanup: it
// comes before the EXIT trap begins, and a second signal landing between the two runs the signal's trap again and
// ends the shell before the EXIT trap has run. One does arrive in the wild - make relays a SIGTERM it gets to the
// recipe's shell, so closing the pane, which signals the whole process group, hands the shell a second one - and it
// left the container and the directory behind (issue #464). So each trap must ignore the three signals as its first
// action, before it exits, and exit with 128 plus its signal's number. The reading of the real recipe is the other
// way round what TestSignalTrapProblems pins over stand-ins: there the check is the subject, here the recipe is.
func TestMakeTestBrowserRecipeIgnoresFurtherSignalsBeforeItExits(t *testing.T) {
	t.Parallel()

	recipe, ok := makeRecipes(t, readRepoFile(t, makefilePath))[testBrowserTarget]
	if !ok {
		t.Fatalf("the Makefile declares no %s target with a recipe, so nothing pins what its traps do with a second signal (%s)",
			testBrowserTarget, signalRaceIssue)
	}

	for _, trap := range trappedSignals {
		t.Run(trap.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff([]string(nil), signalTrapProblems(recipe, trap)); diff != "" {
				t.Errorf("the %s trap of make %s (%s) (-want +got):\n%s\nrecipe:\n%s",
					trap.name, testBrowserTarget, signalRaceIssue, diff, recipe)
			}
		})
	}
}

// TestSignalTrapProblems pins the check itself, over stand-in recipes rather than the real Makefile: the traps as
// issue #464 fixes them have no problem, the three names of the ignore in another order neither, and a trap that
// exits first, ignores only some of the three, exits nowhere, or is missing altogether is reported, naming the signal
// and what its trap does instead. A signal the recipe traps more than once is a fault of its own: the shell keeps the
// last trap it sets for a signal, so a later re-set turns the earlier one into dead text a check must not read. A
// check that read no trap at all, or read the ignore of the EXIT trap as the signal trap's, would pass whatever the
// recipe said, and these are the ways it went wrong while it was written.
func TestSignalTrapProblems(t *testing.T) {
	t.Parallel()

	fixed := func(action string) string {
		return `trap "" HUP INT TERM; ` + action
	}

	cases := []struct {
		name string
		// actions are the three signal traps of signalTrapStandIn, in the order HUP, INT, TERM.
		actions []string
		// recipe, when set, is the whole recipe text, in place of signalTrapStandIn's.
		recipe string
		trap   signalTrap
		want   []string
	}{
		{
			name:    "the traps as issue #464 fixes them",
			actions: []string{fixed("exit 129"), fixed("exit 130"), fixed("exit 143")},
			trap:    trappedSignals[2],
		},
		{
			name:    "the three names in another order",
			actions: []string{fixed("exit 129"), fixed("exit 130"), `trap "" INT TERM HUP; exit 143`},
			trap:    trappedSignals[2],
		},
		{
			name:    "the exit before the ignore",
			actions: []string{"exit 129; trap \"\" HUP INT TERM", fixed("exit 130"), fixed("exit 143")},
			trap:    trappedSignals[0],
			want: []string{
				`the first statement of the HUP trap of make test-browser is "exit 129", want trap "" HUP INT TERM: the trap's exit hands the shell to its EXIT trap, and a second HUP landing before that trap's own ignore ends the shell there, before it has removed the container and the bundle directory (issue #464)`,
			},
		},
		{
			name:    "only the signal's own name ignored",
			actions: []string{fixed("exit 129"), fixed("exit 130"), `trap "" TERM; exit 143`},
			trap:    trappedSignals[2],
			want: []string{
				`the first statement of the TERM trap of make test-browser is "trap \"\" TERM", want trap "" HUP INT TERM: the trap's exit hands the shell to its EXIT trap, and a second TERM landing before that trap's own ignore ends the shell there, before it has removed the container and the bundle directory (issue #464)`,
			},
		},
		{
			name:    "the signal's own status in place of the 129 of a hang-up",
			actions: []string{fixed("exit 143"), fixed("exit 130"), fixed("exit 143")},
			trap:    trappedSignals[0],
			want: []string{
				`the HUP trap of make test-browser exits with "143", want exit 129, the status of a HUP: the recipe traps each signal and exits 128 plus its number (issue #464)`,
			},
		},
		{
			name:    "a trap that ignores the signals and then exits nowhere",
			actions: []string{fixed("exit 129"), fixed("exit 130"), `trap "" HUP INT TERM; true`},
			trap:    trappedSignals[2],
			want: []string{
				`the TERM trap of make test-browser exits nowhere, want exit 143: a trapped signal must end the run through the EXIT trap that cleans up (issue #464)`,
			},
		},
		{
			name:   "no trap for the signal at all",
			recipe: "\ttrap 'trap \"\" HUP INT TERM; exit 129' HUP; \\\n\tsleep 1\n",
			trap:   trappedSignals[2],
			want: []string{
				`the recipe of make test-browser sets no trap for TERM: the run must end through its EXIT trap when it is hung up on, interrupted or terminated (issue #464)`,
			},
		},
		{
			// The shell keeps the last trap it sets for a signal, so a recipe that sets a second one for TERM turns the
			// fixed trap above it into dead text: a check reading the first trap would pin what the run never does.
			name: "the signal trapped twice, the second trap unfixed",
			recipe: "\ttrap 'trap \"\" HUP INT TERM; exit 129' HUP; \\\n" +
				"\ttrap 'trap \"\" HUP INT TERM; exit 130' INT; \\\n" +
				"\ttrap 'trap \"\" HUP INT TERM; exit 143' TERM; \\\n" +
				"\ttrap 'exit 143' TERM; \\\n" +
				"\tsleep 1\n",
			trap: trappedSignals[2],
			want: []string{
				`the recipe of make test-browser sets 2 traps for TERM, the last "exit 143": a shell keeps the last trap it sets for a signal, so only the last one runs when TERM arrives and this check must not read the traps before it (issue #464)`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recipe := tc.recipe
			if recipe == "" {
				recipe = signalTrapStandIn(tc.actions...)
			}

			if diff := cmp.Diff(tc.want, signalTrapProblems(recipe, tc.trap)); diff != "" {
				t.Errorf("signalTrapProblems (%s) (-want +got):\n%s\nrecipe:\n%s", tc.name, diff, recipe)
			}
		})
	}
}

// signalTrapProblems returns what the recipe of make test-browser gets wrong about the trap it sets for one signal,
// one message per fault: empty when the recipe traps the signal once, the trap's first statement ignores HUP, INT and
// TERM, and its action then exits with the status 128 plus the signal's number.
func signalTrapProblems(recipe string, trap signalTrap) []string {
	actions := signalTrapActions(recipe, trap.name)

	if len(actions) == 0 {
		return []string{fmt.Sprintf("the recipe of make %s sets no trap for %s: the run must end through its EXIT trap when it is hung up on, interrupted or terminated (%s)",
			testBrowserTarget, trap.name, signalRaceIssue)}
	}

	// The shell keeps the last trap it sets for a signal, so a recipe that sets a second one leaves the ones before it
	// dead: reading one would pin a trap the run never runs. Reported before the trap itself is read, whatever it says.
	if len(actions) > 1 {
		return []string{fmt.Sprintf("the recipe of make %s sets %d traps for %s, the last %q: a shell keeps the last trap it sets for a signal, so only the last one runs when %s arrives and this check must not read the traps before it (%s)",
			testBrowserTarget, len(actions), trap.name, actions[len(actions)-1], trap.name, signalRaceIssue)}
	}

	statements := trapStatements(actions[0])

	first := ""
	if len(statements) > 0 {
		first = statements[0]
	}

	if !ignoresTrappedSignals(first) {
		return []string{fmt.Sprintf("the first statement of the %s trap of make %s is %q, want trap \"\" HUP INT TERM: the trap's exit hands the shell to its EXIT trap, and a second %s landing before that trap's own ignore ends the shell there, before it has removed the container and the bundle directory (%s)",
			trap.name, testBrowserTarget, first, trap.name, signalRaceIssue)}
	}

	for _, statement := range statements[1:] {
		words := strings.Fields(statement)
		if len(words) == 0 || words[0] != "exit" {
			continue
		}

		if got := strings.Join(words[1:], " "); got != trap.status {
			return []string{fmt.Sprintf("the %s trap of make %s exits with %q, want exit %s, the status of a %s: the recipe traps each signal and exits 128 plus its number (%s)",
				trap.name, testBrowserTarget, got, trap.status, trap.name, signalRaceIssue)}
		}

		return nil
	}

	return []string{fmt.Sprintf("the %s trap of make %s exits nowhere, want exit %s: a trapped signal must end the run through the EXIT trap that cleans up (%s)",
		trap.name, testBrowserTarget, trap.status, signalRaceIssue)}
}

// ignoresTrappedSignals reports whether statement is a trap that ignores all three signals the recipe traps, trap ""
// HUP INT TERM, with the three names in any order: its first word is trap, its second the empty action as the recipe
// writes it, two quotes.
func ignoresTrappedSignals(statement string) bool {
	words := strings.Fields(statement)

	if len(words) < 5 || words[0] != "trap" || words[1] != `""` {
		return false
	}

	ignored := map[string]bool{}
	for _, name := range words[2:] {
		ignored[name] = true
	}

	for _, trap := range trappedSignals {
		if !ignored[trap.name] {
			return false
		}
	}

	return true
}

// signalTrapStatement finds one trap of a recipe that names a single one of the three signals. The lone name before the
// semicolon is what keeps a trap that lists all three, such as the EXIT trap's own ignore inside its action, from being
// read as the trap for one of them. It sees only the form the recipe writes: an action in single quotes, one signal, a
// semicolon straight after its name. A re-set written another way - trap - TERM to clear it, a double-quoted action, &&
// before the next statement instead of ; - goes unseen, and the traps the caller counts are then short of the re-sets.
var signalTrapStatement = regexp.MustCompile(`trap '([^']*)' (HUP|INT|TERM);`)

// signalTrapActions returns the action of every trap the recipe sets for one signal - what the shell runs when that
// signal arrives - in the order the recipe sets them, as the recipe writes each between the trap's quotes: empty when
// the recipe sets no trap for the signal. Every trap is returned, not just the first, because the shell keeps the
// last one it is given for a signal and the caller must see a recipe that sets more than one.
func signalTrapActions(recipe, signal string) []string {
	var actions []string

	for _, match := range signalTrapStatement.FindAllStringSubmatch(recipe, -1) {
		if match[2] == signal {
			actions = append(actions, match[1])
		}
	}

	return actions
}

// trapStatements splits the action of a trap into the statements a shell would run, on the semicolons between them, and
// trims each, the empty ones dropped. The action is the trap's own body, which the recipe writes with no semicolon inside
// a quoted word, so every semicolon separates two statements of it.
func trapStatements(action string) []string {
	var statements []string

	for _, statement := range strings.Split(action, ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, statement)
		}
	}

	return statements
}

// signalTrapStandIn is a stand-in for the recipe of make test-browser at the point the test reads it: the EXIT trap
// that cleans up, the trap of each of the three signals, with the make line continuations the real recipe has, so a
// check reading it reads the same shape. actions are the three actions, in the order HUP, INT, TERM.
func signalTrapStandIn(actions ...string) string {
	return "\ttrap 'trap \"\" HUP INT TERM; rm -rf \"$$bundle_dir\" || true' EXIT; \\\n" +
		"\ttrap '" + actions[0] + "' HUP; \\\n" +
		"\ttrap '" + actions[1] + "' INT; \\\n" +
		"\ttrap '" + actions[2] + "' TERM; \\\n" +
		"\tsleep 1\n"
}

// browserRecipeMakefile is a stand-in Makefile whose one target is test-browser, with no prerequisite: the real
// recipe builds the bundle it serves into a directory of its own, so make test-browser depends on no target that
// would build the shared bin/site (issue #423, the reviewer's finding N2). Its recipe starts Chromium with flags, as
// the real recipe's docker run line does. comments are tab-indented recipe comment lines ahead of that command line.
// A comment line follows one ending in a backslash in no recipe of this repository (issue #437), so those lines come
// before the first continued line, where they are still recipe lines of test-browser: makeRecipes skips them, and a
// flag a comment names is no flag.
func browserRecipeMakefile(flags string, comments ...string) string {
	makefile := "## Build the site's bundle into a directory of this run's own and run its headless-browser smoke test.\n" +
		".PHONY: " + testBrowserTarget + "\n" +
		testBrowserTarget + ":\n"

	for _, comment := range comments {
		makefile += "\t# " + comment + "\n"
	}

	return makefile +
		"\t@set -eu; \\\n" +
		"\t$(DOCKER) run -d --entrypoint /headless-shell/headless-shell \\\n" +
		"\t\t$(HEADLESS_SHELL) " + flags + " >/dev/null\n"
}

// watchdogOrderIssue names what the watchdog's start-up order is pinned for: the leftover sleep the reviewer found on
// issue #449, and the race in the test that pinned it by chance until issue #492 made the test deterministic.
const watchdogOrderIssue = "issue #449 and #492"

// watchdogStartUp matches the watchdog subshell the EXIT trap starts: from its own (stop= to the redirections and &
// that leave it running in the background, but for the pid the trap stores in watchdog_pid. The statements between the
// two are taken as they are: the match ends at the first closing ) that the redirections there follow, so a ) inside the
// watchdog - a $(...), a $((...)), a case pattern - does not end it early and hide the watchdog from the check.
var watchdogStartUp = regexp.MustCompile(`(?s)\(\s*stop=;.*?\) >/dev/null 2>&1 & watchdog_pid=`)

// watchdogUSR1Trap matches the watchdog's trap for the USR1 the EXIT trap sends it - a trap with an action, not the
// trap "" USR1 that makes a late USR1 harmless - as the recipe writes it.
var watchdogUSR1Trap = regexp.MustCompile(`trap "(?:[^"\\]|\\.)+" USR1`)

// watchdogSleep matches the sleep the watchdog bounds the removal with, the one whose pid it stores next: the recipe's
// deadline, as the recipe writes it before make expands the two dollars.
var watchdogSleep = regexp.MustCompile(`sleep "\$\$TEST_BROWSER_RM_DEADLINE"`)

// watchdogSleepBackground matches that sleep with the & that leaves it running in the background, the sleep whose pid a
// store after the & takes: a store before the & takes the pid of another command, or of no command of the watchdog's own.
var watchdogSleepBackground = regexp.MustCompile(`sleep "\$\$TEST_BROWSER_RM_DEADLINE" &`)

// watchdogSleepPidStore matches where the watchdog stores the pid of the sleep it started, the pid its stop check and its
// USR1 trap end.
var watchdogSleepPidStore = regexp.MustCompile(`sleep_pid=\$\$!`)

// watchdogStopCheck matches the test of the stop flag the watchdog runs after it starts its sleep, for a USR1 that came
// after its trap was set and before the sleep's pid was stored: the flag the trap sets. It is not the trap's inner test,
// which tests sleep_pid in escaped quotes and so is no match. Only the message for a watchdog that runs no stop check at
// all reads this regexp: watchdogStop reads the check together with the kill it runs.
var watchdogStopCheck = regexp.MustCompile(`\[\s+-n\s+"\$\$stop"\s+\]`)

// watchdogStop matches the check for the stop flag and the kill of the sleep it runs as one statement, as the recipe
// writes them: the kill inside the check's then, in plain quotes, not the trap's escaped ones. A kill of the sleep in a
// later statement is no match, because it runs whether the flag was set or not: the USR1 the check exists for comes
// before the store, so the check must end the sleep itself.
var watchdogStop = regexp.MustCompile(`\[\s+-n\s+"\$\$stop"\s+\];\s*then\s+kill\s+-s\s+KILL\s+"\$\$sleep_pid"`)

// watchdogTrapOrderProblems returns what the recipe of make test-browser gets wrong about the order the watchdog it
// starts begins in, one message per fault: empty when the watchdog's USR1 trap is set before its sleep starts, its stop
// check for the flag that trap sets kills the sleep inside the check, and that check runs after the sleep's pid is
// stored; and one message when the recipe starts no watchdog, starts a watchdog with no USR1 trap, starts a watchdog
// that sleeps nowhere, starts the sleep before it sets the trap that ends it, stores no pid of its sleep, stores that
// pid before it starts the sleep in the background, runs no check for the stop flag, one that kills no sleep in it, or
// one before it stores the sleep's pid.
//
// The trap-before-sleep order and the stop check are both pinned here because the dynamic test can no longer reach
// either window: its removal waits for the sleep's pid, so the USR1 always comes after the store (issue #492).
func watchdogTrapOrderProblems(recipe string) []string {
	watchdog := watchdogStartUp.FindString(recipe)
	if watchdog == "" {
		return []string{fmt.Sprintf("the recipe of make %s starts no watchdog subshell, so nothing bounds the removal its EXIT trap runs and nothing pins the order the watchdog's trap and its sleep start in (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	trap := watchdogUSR1Trap.FindStringIndex(watchdog)
	if trap == nil {
		return []string{fmt.Sprintf("the watchdog of make %s sets no trap for the USR1 the EXIT trap sends it, so nothing ends its sleep when the removal returns and the sleep outlives the run for the rest of the deadline (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	sleep := watchdogSleep.FindStringIndex(watchdog)
	if sleep == nil {
		return []string{fmt.Sprintf("the watchdog of make %s starts no sleep of the deadline TEST_BROWSER_RM_DEADLINE names, so it cannot bound the removal and nothing pins the order its trap and its sleep start in (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	if trap[0] > sleep[0] {
		return []string{fmt.Sprintf("the watchdog of make %s starts its sleep of the deadline TEST_BROWSER_RM_DEADLINE before it sets the USR1 trap that ends that sleep: a USR1 the EXIT trap sends while the watchdog is between the two reaches it with the sleep running and no trap to end it, so the run leaves the sleep behind for the rest of the deadline (%s, the reviewer's finding S1)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	store := watchdogSleepPidStore.FindStringIndex(watchdog)
	if store == nil {
		return []string{fmt.Sprintf("the watchdog of make %s stores no pid of its sleep, so neither its USR1 trap nor its stop check can end it and the sleep outlives the run for the rest of the deadline (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	if background := watchdogSleepBackground.FindStringIndex(watchdog); background == nil || store[0] < background[1] {
		return []string{fmt.Sprintf("the watchdog of make %s stores a pid of its sleep before it starts that sleep in the background, so the pid it stores is no pid of the sleep and neither its USR1 trap nor its stop check can end it, leaving the sleep to outlive the run for the rest of the deadline (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	stop := watchdogStopCheck.FindStringIndex(watchdog)
	if stop == nil {
		return []string{fmt.Sprintf("the watchdog of make %s sets no check for the stop flag its USR1 trap sets, so a USR1 the EXIT trap sends while the watchdog runs between its trap and the store of the sleep's pid sets the flag and leaves the sleep running for the rest of the deadline (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	if watchdogStop.FindString(watchdog) == "" {
		return []string{fmt.Sprintf("the watchdog of make %s checks the stop flag its USR1 trap sets but kills no sleep of the deadline then, so a USR1 that sets it between the trap and the store of the sleep's pid leaves the sleep running for the rest of the deadline (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	if stop[0] < store[0] {
		return []string{fmt.Sprintf("the watchdog of make %s runs its check for the stop flag its USR1 trap sets before it stores the sleep's pid, so a USR1 that sets the flag between the trap and the store is read against the empty sleep_pid the watchdog starts with and leaves the sleep running for the rest of the deadline (%s)",
			testBrowserTarget, watchdogOrderIssue)}
	}

	return nil
}

// watchdogStandIn is a stand-in for the recipe of make test-browser at the point this check reads it: an EXIT trap
// whose body starts a watchdog subshell holding the statements the case under test gives, in the order it gives them
// - the trap the EXIT trap's USR1 ends the watchdog's sleep by, the sleep it ends, and, where a case has them, the
// statements around them - with the make line continuations the real recipe has, so a check reading it reads the same
// shape.
func watchdogStandIn(statements ...string) string {
	return "\ttrap 'rm -rf \"$$bundle_dir\" || true; ( stop=; sleep_pid=; " + strings.Join(statements, "; ") +
		"; exit 0 ) >/dev/null 2>&1 & watchdog_pid=$$!' EXIT; \\\n" +
		"\tsleep 1\n"
}

// watchdogTrapStandIn and watchdogSleepStandIn are the two statements of the watchdog the check reads the order of, as
// the real recipe writes them: the trap that ends the sleep on the USR1 the EXIT trap sends, and the sleep itself with
// the store of its pid. watchdogStopStandIn is the check the watchdog runs once its sleep has started, for a USR1 that
// came after the trap and before the store of the sleep's pid, and watchdogStopNoKillStandIn a check that sets nothing
// running to end. watchdogLateKillStandIn is a kill of the sleep outside such a check; watchdogSleepAloneStandIn the
// sleep with the & that backgrounds it and no store, and watchdogSleepPidStoreStandIn the store alone, so a case can put
// the store before the sleep it takes the pid of. The order of the trap and the sleep, and of the store and the stop
// check, is what the check reads.
var (
	watchdogTrapStandIn          = `trap "stop=1; if [ -n \"\$$sleep_pid\" ]; then kill -s KILL \"\$$sleep_pid\" 2>/dev/null || true; exit 0; fi" USR1`
	watchdogSleepStandIn         = `sleep "$$TEST_BROWSER_RM_DEADLINE" & sleep_pid=$$!`
	watchdogStopStandIn          = `if [ -n "$$stop" ]; then kill -s KILL "$$sleep_pid" 2>/dev/null || true; exit 0; fi`
	watchdogStopNoKillStandIn    = `if [ -n "$$stop" ]; then exit 0; fi`
	watchdogLateKillStandIn      = `if wait "$$sleep_pid"; then kill -s KILL "$$sleep_pid" 2>/dev/null || true; exit 0; fi`
	watchdogSleepAloneStandIn    = `sleep "$$TEST_BROWSER_RM_DEADLINE" &`
	watchdogSleepPidStoreStandIn = `sleep_pid=$$!`
)

// TestWatchdogTrapOrderProblems pins the check itself, over stand-in watchdog texts rather than the real Makefile: the
// watchdog as issue #449 fixes it has no problem, the trap "" USR1 after its sleep neither, and a watchdog that starts
// its sleep first, sets no USR1 trap, sleeps nowhere, sets no stop check, kills the sleep only outside that check, stores
// no pid of its sleep, stores the pid before it starts the sleep, runs its check before it stores the sleep's pid, or is
// missing altogether is reported. A check that read no watchdog, read the trap "" USR1 for the set, or accepted a kill
// outside the check, would pass what the real recipe must never become. A watchdog that holds a $((...)) between its
// statements is read whole, where a match ending at the first ) would find no watchdog at all.
func TestWatchdogTrapOrderProblems(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// statements are the statements of watchdogStandIn's subshell, in the order the case has them.
		statements []string
		// recipe, when set, is the whole recipe text, in place of watchdogStandIn's.
		recipe string
		want   []string
	}{
		{
			name:       "the watchdog as issue #449 fixes it",
			statements: []string{watchdogTrapStandIn, watchdogSleepStandIn, watchdogStopStandIn, `trap "" USR1`},
		},
		{
			name:       "a watchdog that does arithmetic between its statements",
			statements: []string{watchdogTrapStandIn, `missed=$$((missed + 1))`, watchdogSleepStandIn, watchdogStopStandIn, `trap "" USR1`},
		},
		{
			name:       "a watchdog that sets no stop check",
			statements: []string{watchdogTrapStandIn, watchdogSleepStandIn},
			want: []string{
				`the watchdog of make test-browser sets no check for the stop flag its USR1 trap sets, so a USR1 the EXIT trap sends while the watchdog runs between its trap and the store of the sleep's pid sets the flag and leaves the sleep running for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "a stop check before the sleep's pid is stored",
			statements: []string{watchdogTrapStandIn, watchdogStopStandIn, watchdogSleepStandIn},
			want: []string{
				`the watchdog of make test-browser runs its check for the stop flag its USR1 trap sets before it stores the sleep's pid, so a USR1 that sets the flag between the trap and the store is read against the empty sleep_pid the watchdog starts with and leaves the sleep running for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "a stop check that kills no sleep",
			statements: []string{watchdogTrapStandIn, watchdogSleepStandIn, watchdogStopNoKillStandIn},
			want: []string{
				`the watchdog of make test-browser checks the stop flag its USR1 trap sets but kills no sleep of the deadline then, so a USR1 that sets it between the trap and the store of the sleep's pid leaves the sleep running for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "a stop check whose kill runs outside it",
			statements: []string{watchdogTrapStandIn, watchdogSleepStandIn, watchdogStopNoKillStandIn, watchdogLateKillStandIn},
			want: []string{
				`the watchdog of make test-browser checks the stop flag its USR1 trap sets but kills no sleep of the deadline then, so a USR1 that sets it between the trap and the store of the sleep's pid leaves the sleep running for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "a watchdog that stores no pid of its sleep",
			statements: []string{watchdogTrapStandIn, watchdogSleepAloneStandIn, watchdogStopStandIn},
			want: []string{
				`the watchdog of make test-browser stores no pid of its sleep, so neither its USR1 trap nor its stop check can end it and the sleep outlives the run for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "the sleep's pid stored before the sleep starts",
			statements: []string{watchdogTrapStandIn, watchdogSleepPidStoreStandIn, watchdogSleepAloneStandIn, watchdogStopStandIn},
			want: []string{
				`the watchdog of make test-browser stores a pid of its sleep before it starts that sleep in the background, so the pid it stores is no pid of the sleep and neither its USR1 trap nor its stop check can end it, leaving the sleep to outlive the run for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "the sleep started before the trap that ends it",
			statements: []string{watchdogSleepStandIn, watchdogTrapStandIn},
			want: []string{
				`the watchdog of make test-browser starts its sleep of the deadline TEST_BROWSER_RM_DEADLINE before it sets the USR1 trap that ends that sleep: a USR1 the EXIT trap sends while the watchdog is between the two reaches it with the sleep running and no trap to end it, so the run leaves the sleep behind for the rest of the deadline (issue #449 and #492, the reviewer's finding S1)`,
			},
		},
		{
			name:       "a watchdog that sets no USR1 trap",
			statements: []string{watchdogSleepStandIn, `trap "" USR1`},
			want: []string{
				`the watchdog of make test-browser sets no trap for the USR1 the EXIT trap sends it, so nothing ends its sleep when the removal returns and the sleep outlives the run for the rest of the deadline (issue #449 and #492)`,
			},
		},
		{
			name:       "a watchdog that sleeps nowhere",
			statements: []string{watchdogTrapStandIn},
			want: []string{
				`the watchdog of make test-browser starts no sleep of the deadline TEST_BROWSER_RM_DEADLINE names, so it cannot bound the removal and nothing pins the order its trap and its sleep start in (issue #449 and #492)`,
			},
		},
		{
			name:   "no watchdog at all",
			recipe: "\ttrap 'rm -rf \"$$bundle_dir\" || true' EXIT; \\\n\tsleep 1\n",
			want: []string{
				`the recipe of make test-browser starts no watchdog subshell, so nothing bounds the removal its EXIT trap runs and nothing pins the order the watchdog's trap and its sleep start in (issue #449 and #492)`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recipe := tc.recipe
			if recipe == "" {
				recipe = watchdogStandIn(tc.statements...)
			}

			if diff := cmp.Diff(tc.want, watchdogTrapOrderProblems(recipe)); diff != "" {
				t.Errorf("watchdogTrapOrderProblems (%s) (-want +got):\n%s\nrecipe:\n%s", tc.name, diff, recipe)
			}
		})
	}
}

// TestMakeTestBrowserRecipeSetsTheWatchdogTrapBeforeItsSleep pins the order on the real Makefile (issue #492): the
// watchdog sets its USR1 trap before it starts the sleep that trap ends, so no run can lose the signal to the
// watchdog's own start-up and leave the sleep behind for the whole deadline. The test that reads that leftover off the
// runs cannot see the order on its own - it waits for the sleep's pid, so the signal always comes after it - and this
// check is what keeps the recipe from inverting the two. The reading of the real recipe is argued the other way round
// in TestWatchdogTrapOrderProblems: there the check is the subject, here the recipe is.
func TestMakeTestBrowserRecipeSetsTheWatchdogTrapBeforeItsSleep(t *testing.T) {
	t.Parallel()

	recipe, ok := makeRecipes(t, readRepoFile(t, makefilePath))[testBrowserTarget]
	if !ok {
		t.Fatalf("the Makefile declares no %s target with a recipe, so nothing pins the order the watchdog of its removal starts in (%s)",
			testBrowserTarget, watchdogOrderIssue)
	}

	if diff := cmp.Diff([]string(nil), watchdogTrapOrderProblems(recipe)); diff != "" {
		t.Errorf("the watchdog of make %s (%s) (-want +got):\n%s\nrecipe:\n%s",
			testBrowserTarget, watchdogOrderIssue, diff, recipe)
	}
}
