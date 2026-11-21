// Pins where the Chromium `make test-browser` starts opens DevTools (issue #368, ADR 0036; issue #423 item 1).
// The recipe of the target must pass --remote-debugging-port=0, so the kernel picks the port, and
// --remote-debugging-address=127.0.0.1, so DevTools listens on the loopback interface only: the fixed,
// unauthenticated port 9222 was the audit finding issue #368 removed, and until now only a Makefile comment and
// ADR 0036 recorded the decision, so an edit that brought the port back passed every check. The check reads the
// test-browser target's recipe alone, so neither a comment nor another target's flags count, and it reports a
// fixed or missing port, an address that is not the loopback one (0.0.0.0, ::, localhost) or missing, and a flag
// the recipe passes twice with two different values.
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
			makefile: ".PHONY: test-browser\ntest-browser: site\n",
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

// browserRecipeMakefile is a stand-in Makefile whose one target is test-browser and whose recipe starts Chromium
// with flags, as the real recipe's docker run line does, after the target it depends on. comments are
// tab-indented recipe comment lines before that command line.
func browserRecipeMakefile(flags string, comments ...string) string {
	makefile := "## Build the site's bundle and run its headless-browser smoke test.\n" +
		".PHONY: " + testBrowserTarget + "\n" +
		testBrowserTarget + ": site\n" +
		"\t@echo site\n" +
		"\t@set -eu; \\\n" +
		"\t$(DOCKER) run -d --entrypoint /headless-shell/headless-shell \\\n"

	for _, comment := range comments {
		makefile += "\t# " + comment + "\n"
	}

	return makefile + "\t\t$(HEADLESS_SHELL) " + flags + " >/dev/null\n"
}
