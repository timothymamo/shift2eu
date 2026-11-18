// Pins the repository files the Protect main gate rests on (PRD #161 milestones 2 and 3): the Makefile's
// gate-check target, which the maintainer runs and the release-tag skill runs before a release, with no
// transition flag any more - the switch-over is done, so a gap always fails; .github/CODEOWNERS, which names the
// maintainer for every path and nobody else; and .github/renovate.json5, which never automerges, so a Renovate
// pull request needs the maintainer's approval like any other.
package devenv_test

import (
	"regexp"
	"strings"
	"testing"
)

// The repository files the gate rests on, relative to the repository root.
const (
	codeownersPath = ".github/CODEOWNERS"
	renovatePath   = ".github/renovate.json5"
)

// codeownersRule is one rule of a CODEOWNERS file: the pattern and the owners after it.
type codeownersRule struct {
	pattern string
	owners  []string
}

// parseCodeowners reads a CODEOWNERS file the way GitHub does: a '#' starts a comment that runs to the end of
// the line, a blank line holds no rule, the first field is the pattern and the rest are the owners. It is the
// reading the gate check makes.
func parseCodeowners(body string) []codeownersRule {
	var rules []codeownersRule

	for _, line := range strings.Split(body, "\n") {
		if at := strings.Index(line, "#"); at >= 0 {
			line = line[:at]
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		rules = append(rules, codeownersRule{pattern: fields[0], owners: fields[1:]})
	}

	return rules
}

// The release-tag skill runs the same gate check in its step 2, so a maintainer who reads it reads the same
// contract the script has.
const releaseTagSkill = ".claude/skills/release-tag/SKILL.md"

func TestMakefileHasAGateCheckTarget(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, makefilePath)

	// make help lists a target only when its line ends with "## " and its text, so a maintainer finds the gate
	// check there (the Makefile's help target).
	if !regexp.MustCompile(`(?m)^gate-check:[^\n]*## \S`).MatchString(body) {
		t.Errorf("%s has no 'gate-check: ... ## <help text>' line, so make help does not list the gate check", makefilePath)
	}

	// PRD #161 milestone 3 removed the transition flag entirely: not only is it no longer passed, there is no
	// variable left through which a run could ask for it. `make gate-check GATE_TRANSITION=0` used to be the
	// documented way to check as the gate would after the switch-over; it is a variable make does not know now.
	if transition, ok := makeVariable(body, "GATE_TRANSITION"); ok {
		t.Errorf("%s still assigns GATE_TRANSITION (%q): the transition mode is gone, so the gate check fails on any gap, always, and a variable that could pass a gap would let a release through it (PRD #161 milestone 3)",
			makefilePath, transition)
	}

	// Whatever a run is given - nothing, or the two values the old variable took - the recipe runs the script
	// without the flag it no longer accepts.
	cases := []struct {
		name string
		args []string
	}{
		{name: "the default run"},
		{name: "with the old GATE_TRANSITION=0 on the command line", args: []string{"GATE_TRANSITION=0"}},
		{name: "with the old GATE_TRANSITION=1 on the command line", args: []string{"GATE_TRANSITION=1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stdout, stderr, err := runMake(t, append([]string{"-n", "gate-check"}, tc.args...), makeEnv(t, nil))
			if err != nil {
				t.Fatalf("make -n gate-check %s: %v\nstderr: %s", strings.Join(tc.args, " "), err, stderr)
			}

			if !strings.Contains(stdout, "protect-main-check.sh") {
				t.Fatalf("make -n gate-check %s does not run dev/protect-main-check.sh:\n%s", strings.Join(tc.args, " "), stdout)
			}

			if strings.Contains(stdout, "--transition") {
				t.Errorf("make -n gate-check %s still passes --transition, which the script no longer accepts (PRD #161 milestone 3): a gap must fail the release in every run\n%s",
					strings.Join(tc.args, " "), stdout)
			}
		})
	}
}

// TestReleaseTagSkillRunsTheGateCheckWithoutATransitionFlag pins the other place the contract is written down:
// the release-tag skill's step 2 runs `make gate-check` and acts on its exit status, so it must not tell a
// maintainer or an agent to quote a `transition:` line, nor offer GATE_TRANSITION or --transition as an option
// (PRD #161 milestone 3).
func TestReleaseTagSkillRunsTheGateCheckWithoutATransitionFlag(t *testing.T) {
	t.Parallel()

	body := readRepoFile(t, releaseTagSkill)

	if !strings.Contains(body, "make gate-check") {
		t.Errorf("%s no longer runs the gate check, so this test cannot pin how the skill reads its result (PRD #161 milestone 2)", releaseTagSkill)
	}

	for _, forbidden := range []string{"--transition", "GATE_TRANSITION", "transition:"} {
		for i, line := range strings.Split(body, "\n") {
			if strings.Contains(line, forbidden) {
				t.Errorf("%s:%d still mentions %q: the transition mode is gone (PRD #161 milestone 3), so the gate check fails on any gap and has no line of a fourth kind to quote: %s",
					releaseTagSkill, i+1, forbidden, strings.TrimSpace(line))
			}
		}
	}
}

func TestCodeownersNamesOnlyTheMaintainer(t *testing.T) {
	t.Parallel()

	rules := parseCodeowners(readRepoFile(t, codeownersPath))

	if len(rules) == 0 {
		t.Fatalf("%s holds no rule, so a code-owner review is required from nobody", codeownersPath)
	}

	var everyPath bool

	for _, rule := range rules {
		if rule.pattern == "*" {
			everyPath = true
		}

		if len(rule.owners) == 0 {
			t.Errorf("%s gives the rule %q no owner: a rule without an owner guards nothing, and Protect main's code-owner review is then satisfied by nobody", codeownersPath, rule.pattern)
		}

		for _, owner := range rule.owners {
			if !strings.EqualFold(owner, gateMaintainer) {
				t.Errorf("%s names %q as an owner, want only %s: an approval from any other account, a team or a bot does not satisfy Protect main's code-owner review (PRD #161 milestone 2)",
					codeownersPath, owner, gateMaintainer)
			}
		}
	}

	if !everyPath {
		t.Errorf("%s has no '*' rule, so a path is owned by nobody, and a change to it needs no code owner's approval (PRD #161 milestone 2)", codeownersPath)
	}
}

func TestRenovateNeverAutomeres(t *testing.T) {
	t.Parallel()

	// JSON5 allows the key unquoted and JSON quotes it: both spellings are read, and any value but false fails.
	settings := regexp.MustCompile(`(?m)['"]?automerge['"]?\s*:\s*([A-Za-z]+)`).FindAllStringSubmatch(stripJSON5Comments(readRepoFile(t, renovatePath)), -1)

	for _, setting := range settings {
		if setting[1] != "false" {
			t.Errorf("%s sets automerge: %s: a Renovate pull request needs the maintainer's approval like any other, and gets no bypass of Protect main (PRD #161 milestone 2)", renovatePath, setting[1])
		}
	}
}

// stripJSON5Comments removes the // and /* */ comments of a JSON5 file and leaves its strings alone, so a
// comment that mentions automerge is not read as a setting.
func stripJSON5Comments(body string) string {
	var out strings.Builder

	var quote byte

	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case quote != 0:
			out.WriteByte(c)

			if c == '\\' && i+1 < len(body) {
				i++
				out.WriteByte(body[i])
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			out.WriteByte(c)
		case c == '/' && i+1 < len(body) && body[i+1] == '/':
			for i < len(body) && body[i] != '\n' {
				i++
			}

			if i < len(body) {
				out.WriteByte('\n')
			}
		case c == '/' && i+1 < len(body) && body[i+1] == '*':
			for i += 2; i+1 < len(body) && (body[i] != '*' || body[i+1] != '/'); i++ {
			}

			i++
		default:
			out.WriteByte(c)
		}
	}

	return out.String()
}
