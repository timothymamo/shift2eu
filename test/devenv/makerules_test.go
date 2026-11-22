// The static guards of the Makefile's VM targets, over the rules the file declares rather than over a run of
// one target (issues #313 and #326).
//
// makeRules parses a Makefile into its rules: the targets one declaration names and the lines of the recipe
// under it, one entry per physical line as make hands it to the shell, with the tab of the recipe's indentation
// gone. A recipe spread over several physical lines is kept line by line, so a message names the line a
// maintainer has to edit. Three guards are built on it:
//
//   - every target whose recipe reads LIMA_INSTANCE - at run time, as $$LIMA_INSTANCE, or through the shared
//     $(LIMA_INSTANCE_CHECK) - starts its recipe with that check, because a name that is not refused first is a
//     name that reaches limactl as an option, a path or nothing at all (issue #313);
//   - no recipe line and no variable but the check's own expands $(LIMA_INSTANCE) or ${LIMA_INSTANCE} at make
//     level, where the expansion happens before any recipe line could refuse the value (issue #313);
//   - every recipe line that calls limactl puts $(VM_SHELL_ENV) before it, so the agents App's variables, which
//     the host clone's .envrc exports, reach no process in the VM (issue #326, item 2).
//
// Each guard is pinned twice: over the repository's own Makefile, which must hold all three today, and over
// small synthetic Makefile texts, one per way of breaking them, so a guard that stopped finding a violation
// fails a case here instead of passing on nothing. The guards run with the rest of the package's tests: no
// make, no shell, no network.
package devenv_test

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// limaInstanceCheck is the shared check every recipe that reads LIMA_INSTANCE runs first.
	limaInstanceCheck = "$(LIMA_INSTANCE_CHECK)"

	// vmShellEnv is what a recipe puts before limactl, so the App's variables stay on the Mac.
	vmShellEnv = "$(VM_SHELL_ENV)"
)

// limaInstanceMakeExpansions are the two spellings of a make-level expansion of LIMA_INSTANCE: either one runs the
// value while make reads the recipe, before any shell of it could refuse the value.
var limaInstanceMakeExpansions = []string{"$(LIMA_INSTANCE)", "${LIMA_INSTANCE}"}

// makeRecipeLine is one physical line of a recipe as make hands it to the shell: the line's number in the file
// (1-based) and its text, without the tab of the recipe's indentation.
type makeRecipeLine struct {
	Line int
	Text string
}

// makeRule is one rule of a Makefile: the targets its declaration names, in the order it names them, and the
// lines of its recipe, in order. A declaration without a recipe, such as a target-specific variable
// assignment, and a special target such as .PHONY are no rules here: they carry no recipe a guard reads.
type makeRule struct {
	Targets []string
	Recipe  []makeRecipeLine
}

// makeTargetDeclaration matches the target list of a declaration line and the colon after it: names of letters,
// digits and . _ - / + @ %, which is every target this repository's Makefile declares, and nothing else. The
// pattern rules out $(call ...) lines and other make code that holds a colon, and makeTargets refuses the := and
// ?= of a variable assignment by the character after the colon.
var makeTargetDeclaration = regexp.MustCompile(`^([A-Za-z0-9_./+@%-]+(?:[ \t]+[A-Za-z0-9_./+@%-]+)*)[ \t]*(::?)`)

// makeRules returns the rules of body, a Makefile, in the order it declares them.
func makeRules(body string) []makeRule {
	lines := strings.Split(body, "\n")

	var (
		rules []makeRule

		// current is the rule the next recipe lines belong to, -1 between rules.
		current = -1
	)

	for i := 0; i < len(lines); i++ {
		first := i + 1
		parts := []string{lines[i]}

		for continues(parts[len(parts)-1]) && i+1 < len(lines) {
			i++

			last := len(parts) - 1
			parts[last] = parts[last][:len(parts[last])-1]
			parts = append(parts, lines[i])
		}

		switch {
		case strings.HasPrefix(parts[0], "\t"):
			// A line belongs to a recipe only when it is the first physical line that starts with the tab: the
			// continued lines of a variable definition start with one too, and are no recipe.
			if current >= 0 {
				for at, part := range parts {
					rules[current].Recipe = append(rules[current].Recipe, makeRecipeLine{
						Line: first + at,
						Text: strings.TrimPrefix(part, "\t"),
					})
				}
			}
		case strings.TrimSpace(strings.Join(parts, " ")) == "", strings.HasPrefix(strings.TrimSpace(parts[0]), "#"):
			// A blank line or a comment between a rule's lines leaves them the rule they belong to.
		default:
			current = -1

			if targets := makeTargets(strings.Join(parts, " ")); len(targets) != 0 {
				rules = append(rules, makeRule{Targets: targets})

				current = len(rules) - 1
			}
		}
	}

	return rules
}

// makeTargets returns the targets of text, a Makefile declaration line, or nil when it declares no target: a
// variable assignment, make code of another kind, a special target such as .PHONY, or a pattern rule.
func makeTargets(text string) []string {
	match := makeTargetDeclaration.FindStringSubmatchIndex(text)
	if match == nil {
		return nil
	}

	// A ':=' or '?=' is an assignment, not a target's declaration.
	if end := match[5]; end < len(text) && text[end] == '=' {
		return nil
	}

	targets := strings.Fields(text[match[2]:match[3]])

	for _, target := range targets {
		if strings.HasPrefix(target, ".") || strings.Contains(target, "%") {
			return nil
		}
	}

	return targets
}

// limaInstanceTargets returns, sorted and without duplicates, the targets whose recipe reads LIMA_INSTANCE: at
// run time, as $$LIMA_INSTANCE, or through the shared check $(LIMA_INSTANCE_CHECK), which expands the name
// while make reads the recipe (issue #313).
func limaInstanceTargets(rules []makeRule) []string {
	var targets []string

	for _, rule := range rules {
		if !ruleReadsLimaInstance(rule) {
			continue
		}

		targets = append(targets, rule.Targets...)
	}

	slices.Sort(targets)

	return slices.Compact(targets)
}

// ruleReadsLimaInstance reports whether one of rule's recipe lines reads LIMA_INSTANCE, at run time or through
// the shared check.
func ruleReadsLimaInstance(rule makeRule) bool {
	for _, line := range rule.Recipe {
		if strings.Contains(line.Text, "$LIMA_INSTANCE") || strings.Contains(line.Text, limaInstanceCheck) {
			return true
		}
	}

	return false
}

// limaInstanceViolations returns one message per rule or variable definition of rules that breaks issue #313's
// rule, naming the target or the variable and the line: a recipe that reads LIMA_INSTANCE without the shared
// check as its first line, a recipe line that expands the name at make level, or a variable definition that
// does - the check's own definitions excepted, as expanding the name is what they are for. definitions is the
// Makefile's variable assignments, by name (makeDefinitions).
func limaInstanceViolations(rules []makeRule, definitions map[string]string) []string {
	var violations []string

	for _, rule := range rules {
		for _, target := range rule.Targets {
			violations = append(violations, ruleLimaInstanceViolations(target, rule)...)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(definitions)) {
		// The check's own definitions expand the name: that is what they are for.
		if strings.HasPrefix(name, "LIMA_INSTANCE") {
			continue
		}

		for _, expansion := range limaInstanceMakeExpansions {
			if strings.Contains(definitions[name], expansion) {
				violations = append(violations, fmt.Sprintf("variable %s expands LIMA_INSTANCE at make level with %s: a variable spliced into a recipe expands the name before the recipe's own check runs, so a name that carries a command runs it (issue #313)",
					name, expansion))
			}
		}
	}

	return violations
}

// ruleLimaInstanceViolations returns the violations of the recipe of target, a target of rule.
func ruleLimaInstanceViolations(target string, rule makeRule) []string {
	var violations []string

	readAt := 0

	for _, line := range rule.Recipe {
		if readAt == 0 && strings.Contains(line.Text, "$LIMA_INSTANCE") {
			readAt = line.Line
		}

		for _, expansion := range limaInstanceMakeExpansions {
			if strings.Contains(line.Text, expansion) {
				violations = append(violations, fmt.Sprintf("make %s: its recipe line %d (%q) expands LIMA_INSTANCE at make level with %s: make runs the value while it expands the recipe, before any recipe line could refuse it (issue #313)",
					target, line.Line, line.Text, expansion))
			}
		}
	}

	if readAt != 0 && (len(rule.Recipe) == 0 || strings.TrimSpace(rule.Recipe[0].Text) != limaInstanceCheck) {
		violations = append(violations, fmt.Sprintf("make %s: its recipe reads LIMA_INSTANCE (line %d) but its first recipe line is not %s, so a name that is not an instance name reaches limactl as an option, a path or nothing at all (issue #313)",
			target, readAt, limaInstanceCheck))
	}

	return violations
}

// vmShellEnvViolations returns one message per recipe line of rules that calls limactl without $(VM_SHELL_ENV)
// before it, naming the target and the line: the agents App's variables, which the host clone's .envrc exports,
// must reach no process in the VM, and Lima forwards no environment of its own (issue #326, item 2).
func vmShellEnvViolations(rules []makeRule) []string {
	var violations []string

	for _, rule := range rules {
		for _, line := range rule.Recipe {
			if !nakedLimactlCall(line.Text) {
				continue
			}

			for _, target := range rule.Targets {
				violations = append(violations, fmt.Sprintf("make %s: its recipe line %d calls limactl without %s before it, so the agents App's variables reach the VM with it: %q (issue #326)",
					target, line.Line, vmShellEnv, line.Text))
			}
		}
	}

	return violations
}

// callsLimactl reports whether line calls limactl: limactl stands as a command word in it, outside quotes.
func callsLimactl(line string) bool {
	return len(limactlCommandSites(line)) != 0
}

// nakedLimactlCall reports whether line calls limactl without $(VM_SHELL_ENV) before it.
func nakedLimactlCall(line string) bool {
	for _, before := range limactlCommandSites(line) {
		if !strings.HasSuffix(strings.TrimRight(before, " \t"), vmShellEnv) {
			return true
		}
	}

	return false
}

// limactlCommandSites returns the text before each place in line where limactl is a command word: outside '...'
// strings, and outside "..." strings except inside the $(...) substitutions a double-quoted string still runs.
// A string may name limactl without running it, which is why the quotes matter: the recipes here print it in
// messages. limactl has to be a word of its own too, not part of a longer name. The text before it is what decides
// whether the call puts $(VM_SHELL_ENV) in front of it: at the start of a line, first in a $(...) substitution,
// or after a ;, an && or a | of a pipeline, as the recipes here write it.
func limactlCommandSites(line string) []string {
	const tool = "limactl"

	var (
		sites []string

		quote byte

		// subst holds the quote each open $(...) resumes with once its ) closes: a command inside one runs
		// whatever the string around it is, and the string resumes after it.
		subst []byte
	)

	for i := 0; i < len(line); i++ {
		c := line[i]

		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}

			continue
		case quote == '"':
			switch {
			case c == '\\':
				i++
			case c == '"':
				quote = 0
			case strings.HasPrefix(line[i:], "$("):
				subst = append(subst, quote)
				quote = 0

				i++
			}

			continue
		case c == '\\':
			i++

			continue
		case c == '\'':
			quote = '\''

			continue
		case c == '"':
			quote = '"'

			continue
		case strings.HasPrefix(line[i:], "$("):
			subst = append(subst, 0)

			i++

			continue
		case c == ')' && len(subst) > 0:
			quote = subst[len(subst)-1]
			subst = subst[:len(subst)-1]

			continue
		}

		if !strings.HasPrefix(line[i:], tool) {
			continue
		}

		if i > 0 && isMakefileWordByte(line[i-1]) {
			continue
		}

		if end := i + len(tool); end < len(line) && isMakefileWordByte(line[end]) {
			continue
		}

		sites = append(sites, line[:i])
	}

	return sites
}

// isMakefileWordByte reports whether c continues a word in a recipe: a letter, a digit or one of . _ - /, so
// limactl is no command when it is part of a longer name.
func isMakefileWordByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	default:
		return strings.IndexByte("._-/", c) >= 0
	}
}

// TestMakeRulesFindsLimaInstanceViolations pins the parser and the LIMA_INSTANCE guard of issue #313 on small
// Makefile texts, one per way of breaking the rule: a target that reads the name without the check first, the
// check not on the first line, and the name expanded at make level in a recipe and in a variable. Each failure
// names the target or the variable, and a text that follows the rule has no violation at all.
func TestMakeRulesFindsLimaInstanceViolations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		// wantTargets is the derived target set, sorted: every target whose recipe reads LIMA_INSTANCE.
		wantTargets []string
		// wantViolations holds one entry per expected violation, each the substrings its message must name.
		wantViolations [][]string
	}{
		{
			name: "a target that checks the name first",
			text: ".PHONY: vm-stop\n" +
				"vm-stop: ## Stop the VM\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t" + vmShellEnv + " limactl stop \"$$LIMA_INSTANCE\"\n",
			wantTargets: []string{"vm-stop"},
		},
		{
			name: "a target that runs only the check",
			text: "vm-create:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t$(MAKE) vm-up\n",
			wantTargets: []string{"vm-create"},
		},
		{
			name: "a target that reads the name without the check",
			text: "vm-stop: ## Stop the VM\n" +
				"\tlimactl stop \"$$LIMA_INSTANCE\"\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"make vm-stop", "first recipe line", limaInstanceCheck}},
		},
		{
			name: "the check is not the first recipe line",
			text: "vm-stop:\n" +
				"\t@echo stopping\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\tlimactl stop \"$$LIMA_INSTANCE\"\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"make vm-stop", limaInstanceCheck}},
		},
		{
			name: "a recipe that expands the name at make level",
			text: "vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\techo \"$(LIMA_INSTANCE)\"\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"make vm-stop", "$(LIMA_INSTANCE)"}},
		},
		{
			name: "a recipe that expands the name with braces",
			text: "vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\techo \"${LIMA_INSTANCE}\"\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"make vm-stop", "${LIMA_INSTANCE}"}},
		},
		{
			name: "a variable that expands the name at make level",
			text: "STOP_ENV := echo ${LIMA_INSTANCE}\n" +
				"vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"variable STOP_ENV", "${LIMA_INSTANCE}"}},
		},
		{
			name: "the check's own definitions expand the name",
			text: "LIMA_INSTANCE_VALID = $(and $(LIMA_INSTANCE),ok)\n" +
				"LIMA_INSTANCE_CHECK = $(if $(LIMA_INSTANCE_VALID),,$(error invalid))\n" +
				"vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\tlimactl stop \"$$LIMA_INSTANCE\"\n",
			wantTargets: []string{"vm-stop"},
		},
		{
			name: "one rule with several targets, each of them a violation",
			text: "vm-stop vm-delete:\n" +
				"\tlimactl \"$$LIMA_INSTANCE\"\n",
			wantTargets:    []string{"vm-delete", "vm-stop"},
			wantViolations: [][]string{{"make vm-stop"}, {"make vm-delete"}},
		},
		{
			name: "a continued recipe line that reads the name",
			text: "vm-delete:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\tlimactl delete \\\n" +
				"\t\t--force \"$$LIMA_INSTANCE\"\n",
			wantTargets: []string{"vm-delete"},
		},
		{
			name: "a recipe that splices a macro that reads the name, without the check",
			text: "FOREIGN_JOBS_CHECK = if [ -n \"$$LIMA_INSTANCE\" ]; then echo \"another job runs $$LIMA_INSTANCE\"; exit 1; fi\n" +
				"agents-token-refresh-install:\n" +
				"\t@$(FOREIGN_JOBS_CHECK); dev/agents-token-launchd.sh foreign-jobs\n",
			wantTargets:    []string{"agents-token-refresh-install"},
			wantViolations: [][]string{{"make agents-token-refresh-install", "first recipe line", limaInstanceCheck}},
		},
		{
			name: "a recipe that splices a macro that reads the name through $(call ...), after the check",
			text: "AUTOSTART_JOBS = lima_label=\"$(AUTOSTART_PREFIX)$$LIMA_INSTANCE\"; \\\n" +
				"\techo \"$$lima_label\"\n" +
				"vm-boot-install:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t$(call AUTOSTART_JOBS,refused); launchctl print system\n",
			wantTargets: []string{"vm-boot-install"},
		},
		{
			name: "a recipe that reads the name with braces at run time",
			text: "vm-stop:\n" +
				"\tlimactl stop \"$${LIMA_INSTANCE}\"\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"make vm-stop", "first recipe line", limaInstanceCheck}},
		},
		{
			name: "a recipe that reads the name with braces and a default at run time, after the check",
			text: "vm-env:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\tdir=\"$${LIMA_HOME:-$$HOME/.lima}/$${LIMA_INSTANCE:-shift2eu}\"\n",
			wantTargets: []string{"vm-env"},
		},
		{
			name: "a variable whose name starts with LIMA_INSTANCE but that expands the name at make level",
			text: "LIMA_INSTANCE_DIR := $(HOME)/.lima/$(LIMA_INSTANCE)\n" +
				"vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\tmkdir -p \"$(LIMA_INSTANCE_DIR)\"\n",
			wantTargets:    []string{"vm-stop"},
			wantViolations: [][]string{{"variable LIMA_INSTANCE_DIR", "$(LIMA_INSTANCE)"}},
		},
		{
			name: "a target whose declaration has a prerequisite and a target-specific variable",
			text: "vm-env: export VM_ENV := $(value VM_ENV)\n" +
				"vm-env: vm-env-check ## Copy the credentials\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t" + vmShellEnv + " limactl shell --workdir / \"$$LIMA_INSTANCE\"\n",
			wantTargets: []string{"vm-env"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rules := makeRules(tc.text)

			if diff := cmp.Diff(tc.wantTargets, limaInstanceTargets(rules)); diff != "" {
				t.Errorf("the targets derived from the text are not the ones that read LIMA_INSTANCE (-want +got):\n%s\ntext:\n%s", diff, tc.text)
			}

			violations := limaInstanceViolations(rules, makeDefinitions(tc.text))
			if len(violations) != len(tc.wantViolations) {
				t.Fatalf("the text got %d violation(s), want %d, so the guard finds a break it should not or misses one (issue #313):\n%s\ntext:\n%s",
					len(violations), len(tc.wantViolations), strings.Join(violations, "\n"), tc.text)
			}

			for i, want := range tc.wantViolations {
				for _, w := range want {
					if !strings.Contains(violations[i], w) {
						t.Errorf("violation %d does not name %q, so a maintainer cannot tell what to fix: %s", i+1, w, violations[i])
					}
				}
			}
		})
	}
}

// TestMakeRulesFindsALimactlCallWithoutVMShellEnv pins the guard of issue #326's item 2 on small Makefile texts:
// a recipe line that calls limactl must put $(VM_SHELL_ENV) before it, whether limactl starts the line, stands
// in a $(...) substitution or follows a pipe, and a line that only names limactl in a message calls nothing.
func TestMakeRulesFindsALimactlCallWithoutVMShellEnv(t *testing.T) {
	t.Parallel()

	const prefixed = "\t" + vmShellEnv + " limactl "

	cases := []struct {
		name string
		text string
		// wantViolations holds one entry per expected violation, each the substrings its message must name.
		wantViolations [][]string
	}{
		{
			name: "every call puts the shell environment first",
			text: "vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				prefixed + "stop \"$$LIMA_INSTANCE\"\n" +
				"vm-env:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@out=\"$$(printf '%s\\n' \"$$env_file\" | " + vmShellEnv + " limactl shell --workdir / \"$$LIMA_INSTANCE\")\" || exit 1\n" +
				"vm-up:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@list=\"$$(" + vmShellEnv + " limactl list --quiet)\" || exit 1\n",
		},
		{
			name: "a target that calls limactl without it",
			text: "vm-delete:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\tlimactl delete --force \"$$LIMA_INSTANCE\"\n",
			wantViolations: [][]string{{"make vm-delete", vmShellEnv}},
		},
		{
			name: "a call in a $(...) substitution without it",
			text: "vm-up:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@list=\"$$(limactl list --quiet)\" || exit 1\n",
			wantViolations: [][]string{{"make vm-up", vmShellEnv}},
		},
		{
			name: "a call after a pipe without it",
			text: "vm-gh-wrapper:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@out=\"$$(printf '%s\\n' \"$$script\" | limactl shell --workdir / \"$$LIMA_INSTANCE\" sh -s)\"\n",
			wantViolations: [][]string{{"make vm-gh-wrapper", vmShellEnv}},
		},
		{
			name: "a recipe that splices a macro that calls limactl",
			text: "LIMACTL_LIST = list=\"$$(limactl list --quiet)\"; echo \"$$list\"\n" +
				"vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@$(LIMACTL_LIST)\n",
			wantViolations: [][]string{{"make vm-stop", vmShellEnv}},
		},
		{
			name: "a call in a macro spliced through $(call ...) without it",
			text: "STOP_VM = limactl stop --force \"$$LIMA_INSTANCE\"\n" +
				"vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t$(call STOP_VM,$$LIMA_INSTANCE)\n",
			wantViolations: [][]string{{"make vm-stop", vmShellEnv}},
		},
		{
			name: "a recipe that splices a macro whose call puts the shell environment first",
			text: "LIMACTL_LIST = list=\"$$(" + vmShellEnv + " limactl list --quiet)\"; echo \"$$list\"\n" +
				"vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@$(LIMACTL_LIST)\n",
		},
		{
			name: "a message whose quote a continuation carries into the next line",
			text: "vm-up:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@echo 'limactl list is how you check it \\\n" +
				"\tand limactl stop is how you stop it' >&2\n" +
				"\t" + vmShellEnv + " limactl list --quiet\n",
		},
		{
			name: "a message that names limactl calls nothing",
			text: "vm-stop:\n" +
				"\t" + limaInstanceCheck + "\n" +
				"\t@echo \"see limactl list and 'limactl start --debug'\" >&2\n",
		},
		{
			name: "one naked call among the prefixed ones of the same rule",
			text: "vm-up:\n" +
				"\t" + limaInstanceCheck + "\n" +
				prefixed + "list --quiet\n" +
				"\tlimactl shell --start --workdir / \"$$LIMA_INSTANCE\" true\n",
			wantViolations: [][]string{{"make vm-up", vmShellEnv}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			violations := vmShellEnvViolations(makeRules(tc.text))
			if len(violations) != len(tc.wantViolations) {
				t.Fatalf("the text got %d violation(s), want %d, so the guard finds a call it should not or misses one (issue #326):\n%s\ntext:\n%s",
					len(violations), len(tc.wantViolations), strings.Join(violations, "\n"), tc.text)
			}

			for i, want := range tc.wantViolations {
				for _, w := range want {
					if !strings.Contains(violations[i], w) {
						t.Errorf("violation %d does not name %q, so a maintainer cannot tell what to fix: %s", i+1, w, violations[i])
					}
				}
			}
		})
	}
}

// TestMakefileChecksLimaInstanceBeforeEveryRecipeThatReadsIt pins issue #313's rule on the repository's own
// Makefile: every target whose recipe reads LIMA_INSTANCE runs the shared check first, and nothing but the
// check's own definitions expands the name at make level. The guard exists because the hand-written list it
// replaces covered only the targets that existed when it was written: a target added later would have read the
// name unchecked, and nothing would have failed.
func TestMakefileChecksLimaInstanceBeforeEveryRecipeThatReadsIt(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, makefilePath)
	rules := makeRules(body)

	derived := limaInstanceTargets(rules)
	if len(derived) == 0 {
		t.Fatalf("no target of the Makefile reads LIMA_INSTANCE, so this guard pins nothing: either the parser stopped finding recipes or the targets went away")
	}

	for _, want := range []string{"deck", "vm-shell", "vm-stop", "vm-up"} {
		if !slices.Contains(derived, want) {
			t.Errorf("the parser derives no target %q from the Makefile, although its recipe reads LIMA_INSTANCE: a guard that misses a target pins nothing about it (issue #313)\ntargets: %v", want, derived)
		}
	}

	for _, violation := range limaInstanceViolations(rules, makeDefinitions(body)) {
		t.Error(violation)
	}
}

// TestMakefileSetsVMShellEnvForEveryLimactlCall pins issue #326's item 2 on the repository's own Makefile: every
// target whose recipe calls limactl puts $(VM_SHELL_ENV) before every call, so a line added to a recipe later
// cannot hand the VM the agents App's variables, which the host clone's .envrc exports.
func TestMakefileSetsVMShellEnvForEveryLimactlCall(t *testing.T) {
	t.Parallel()

	rules := makeRules(readRepoFile(t, makefilePath))

	derived := vmShellEnvTargets(rules)
	if len(derived) == 0 {
		t.Fatalf("no target of the Makefile calls limactl, so this guard pins nothing: either the parser stopped finding recipes or the targets went away")
	}

	for _, want := range []string{"deck", "vm-delete", "vm-stop", "vm-up"} {
		if !slices.Contains(derived, want) {
			t.Errorf("the parser derives no target %q from the Makefile, although its recipe calls limactl: a guard that misses a target pins nothing about it (issue #326)\ntargets: %v", want, derived)
		}
	}

	for _, violation := range vmShellEnvViolations(rules) {
		t.Error(violation)
	}
}

// vmShellEnvTargets returns, sorted and without duplicates, the targets whose recipe calls limactl.
func vmShellEnvTargets(rules []makeRule) []string {
	var targets []string

	for _, rule := range rules {
		for _, line := range rule.Recipe {
			if !callsLimactl(line.Text) {
				continue
			}

			targets = append(targets, rule.Targets...)

			break
		}
	}

	slices.Sort(targets)

	return slices.Compact(targets)
}
