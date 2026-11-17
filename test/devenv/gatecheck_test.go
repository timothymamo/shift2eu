// Pins the Protect main gate check (PRD #161 milestone 2). dev/protect-main-check.sh reads the `Protect main`
// ruleset and .github/CODEOWNERS on main through gh's read-only API, prints one `ok:` or `gap:` line per
// check, fails closed (exit 1) once the switch-over is done and reports without failing (exit 0, a line about
// the transition holding the number of gaps) before it, and exits 2 whenever it cannot make the checks at
// all. The recorded answers are GitHub's documented REST shapes with invented identifiers, in
// testdata/gatecheck/; a fake gh on PATH serves them and records its argv, so no test reaches the network or
// an account and every call the script makes is pinned.
package devenv_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	// protectMainScript is the gate check the maintainer runs by hand, make gate-check runs and the
	// release-tag skill runs before a release.
	protectMainScript = "dev/protect-main-check.sh"

	// gateFixtureDir holds the fake API's recorded answers.
	gateFixtureDir = "test/devenv/testdata/gatecheck"

	// gateRulesetID is the id of the `Protect main` ruleset in the recorded list.
	gateRulesetID = "123456"

	// gateMaintainer is the only owner .github/CODEOWNERS may name.
	gateMaintainer = "@timothymamo"
)

// gateEndpoints are the three reads the gate check makes, in the order its contract lists them. gh fills the
// placeholders, so the fake gh sees them as they are.
var gateEndpoints = []string{
	"repos/{owner}/{repo}/rulesets",
	"repos/{owner}/{repo}/rulesets/" + gateRulesetID,
	"repos/{owner}/{repo}/contents/.github/CODEOWNERS?ref=main",
}

// gateFakeGH stands in for gh in the gate check's tests: it records its argv the way the sandbox's other
// shims do, finds the endpoint among its arguments (gh api allows flags on either side of it) and answers with
// the recorded file the endpoint names. A file the test did not write is the API's failure, which gh reports
// as a non-zero exit.
const gateFakeGH = `#!/bin/sh
# Serves the recorded answers of testdata/gatecheck/ to the gate check.
{
  printf 'tool=gh\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
serve="${SHIFT2EU_TEST_GH_SERVE:?set SHIFT2EU_TEST_GH_SERVE}"
endpoint=""
for a in "$@"; do
  case "$a" in
  repos/*)
    endpoint="$a"
    break
    ;;
  esac
done
if [ -z "${endpoint}" ]; then
  echo 'fake gh: the argv holds no repos/ endpoint' >&2
  exit 64
fi
case "${endpoint}" in
*"/contents/.github/CODEOWNERS?ref=main") file="${serve}/codeowners.json" ;;
*"/rulesets/"*) file="${serve}/ruleset-${endpoint##*/}.json" ;;
*"/rulesets") file="${serve}/rulesets.json" ;;
*)
  echo "fake gh: the gate check reads the unexpected endpoint ${endpoint}" >&2
  exit 64
  ;;
esac
if [ ! -f "${file}" ]; then
  echo "fake gh: the recorded answer ${file##*/} is not there, as a 404 is not" >&2
  exit 1
fi
cat "${file}"
`

// gateFixture is one run's fake environment: the sandbox whose gh serves the recorded answers, the directory
// the answers are served from, and the environment of the run.
type gateFixture struct {
	sb    *sandbox
	serve string
	env   map[string]string
}

// newGateFixture builds a fixture whose gh is the stand-in above and whose PATH holds jq, which parses the
// answers: only a machine without jq skips, as the scripts' other tools do.
func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()

	jq := realTool(t, "jq")

	sb := newSandbox(t)

	f := &gateFixture{sb: sb, serve: filepath.Join(sb.root, "serve")}
	if err := os.MkdirAll(f.serve, 0o700); err != nil {
		t.Fatalf("create %s: %v", f.serve, err)
	}

	// The sandbox's gh is a tripwire for the token wrapper's tests; the gate check only reads the API, so the
	// stand-in gh takes its place.
	writeFileMode(t, filepath.Join(sb.binDir, "gh"), []byte(gateFakeGH), 0o700)

	f.env = sb.baseEnv()
	f.env["PATH"] = sb.binDir + ":" + filepath.Dir(jq) + ":/usr/local/bin:/usr/bin:/bin"
	f.env["SHIFT2EU_TEST_GH_SERVE"] = f.serve

	return f
}

// run runs the gate check with args and returns its stdout, its stderr and its exit status.
func (f *gateFixture) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	stdout, stderr, err := f.sb.run(t, scriptPath(t, protectMainScript), args, f.env, "")

	return stdout, stderr, exitStatus(t, err)
}

// exitStatus is what a run exited with.
func exitStatus(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run the gate check: %v", err)
	}

	return exitErr.ExitCode()
}

// outputLines splits a run's stdout into its lines, dropping the empty remainder after the last newline.
func outputLines(stdout string) []string {
	trimmed := strings.TrimSuffix(stdout, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

// gapLines returns the gap: lines of a run's stdout, in the order it printed them.
func gapLines(stdout string) []string {
	var gaps []string

	for _, line := range outputLines(stdout) {
		if strings.HasPrefix(line, "gap: ") {
			gaps = append(gaps, line)
		}
	}

	return gaps
}

// serveJSON writes one answer of the fake API, under the file name the fake gh serves for its endpoint.
func (f *gateFixture) serveJSON(t *testing.T, name string, doc any) {
	t.Helper()

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode the fake API's %s answer: %v", name, err)
	}

	writeFileMode(t, filepath.Join(f.serve, name), append(body, '\n'), 0o600)
}

// serveRaw writes one answer of the fake API unchanged, for an answer that is not valid JSON.
func (f *gateFixture) serveRaw(t *testing.T, name, body string) {
	t.Helper()

	writeFileMode(t, filepath.Join(f.serve, name), []byte(body), 0o600)
}

// readGateFixture decodes a recorded answer of testdata/gatecheck.
func readGateFixture(t *testing.T, name string) any {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(moduleRoot, gateFixtureDir, name))
	if err != nil {
		t.Fatalf("read %s/%s: %v", gateFixtureDir, name, err)
	}

	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode %s/%s: %v", gateFixtureDir, name, err)
	}

	return doc
}

// readGateRulesetList decodes the recorded rulesets list.
func readGateRulesetList(t *testing.T) []any {
	t.Helper()

	rulesets, ok := readGateFixture(t, "rulesets.json").([]any)
	if !ok {
		t.Fatalf("%s/rulesets.json is not a list: gh api answers the rulesets endpoint with one", gateFixtureDir)
	}

	return rulesets
}

// readGateRuleset decodes a recorded ruleset.
func readGateRuleset(t *testing.T, name string) map[string]any {
	t.Helper()

	ruleset, ok := readGateFixture(t, name).(map[string]any)
	if !ok {
		t.Fatalf("%s/%s is not an object: gh api answers a ruleset with one", gateFixtureDir, name)
	}

	return ruleset
}

// serveRulesetList writes the answer of the rulesets list endpoint.
func (f *gateFixture) serveRulesetList(t *testing.T, rulesets []any) {
	t.Helper()

	f.serveJSON(t, "rulesets.json", rulesets)
}

// serveRuleset writes the answer of the single-ruleset endpoint for the id the list holds.
func (f *gateFixture) serveRuleset(t *testing.T, ruleset map[string]any) {
	t.Helper()

	f.serveJSON(t, "ruleset-"+gateRulesetID+".json", ruleset)
}

// serveCodeowners writes the answer of the contents endpoint for a CODEOWNERS file holding content: base64
// encoded and wrapped over several lines, as GitHub answers it.
func (f *gateFixture) serveCodeowners(t *testing.T, content string) {
	t.Helper()

	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	var wrapped strings.Builder

	for len(encoded) > 60 {
		wrapped.WriteString(encoded[:60])
		wrapped.WriteString("\n")
		encoded = encoded[60:]
	}

	wrapped.WriteString(encoded)
	wrapped.WriteString("\n")

	f.serveJSON(t, "codeowners.json", map[string]any{
		"name":     "CODEOWNERS",
		"path":     ".github/CODEOWNERS",
		"type":     "file",
		"encoding": "base64",
		"content":  wrapped.String(),
	})
}

// serveRecordedCodeowners writes the recorded contents answer, the CODEOWNERS file main holds today.
func (f *gateFixture) serveRecordedCodeowners(t *testing.T) {
	t.Helper()

	f.serveJSON(t, "codeowners.json", readGateFixture(t, "codeowners.json"))
}

// serveSwitchedOver makes the fake API answer as a repository whose gate is complete: the recorded rulesets
// list, the recorded switched-over ruleset and the recorded CODEOWNERS file.
func (f *gateFixture) serveSwitchedOver(t *testing.T) {
	t.Helper()

	f.serveRulesetList(t, readGateRulesetList(t))
	f.serveRuleset(t, readGateRuleset(t, "ruleset-switched-over.json"))
	f.serveRecordedCodeowners(t)
}

// pullRequestParameters returns the parameters of a ruleset's pull_request rule, for a case that changes one
// of them.
func pullRequestParameters(t *testing.T, ruleset map[string]any) map[string]any {
	t.Helper()

	for _, rule := range rulesOf(t, ruleset) {
		if rule["type"] == "pull_request" {
			parameters, ok := rule["parameters"].(map[string]any)
			if !ok {
				t.Fatalf("the fixture's pull_request rule holds no parameters: %v", rule)
			}

			return parameters
		}
	}

	t.Fatalf("the fixture ruleset holds no pull_request rule")

	return nil
}

// rulesOf returns a ruleset's rules.
func rulesOf(t *testing.T, ruleset map[string]any) []map[string]any {
	t.Helper()

	raw, ok := ruleset["rules"].([]any)
	if !ok {
		t.Fatalf("the fixture ruleset holds no rules list: %v", ruleset["rules"])
	}

	rules := make([]map[string]any, 0, len(raw))

	for _, rule := range raw {
		object, ok := rule.(map[string]any)
		if !ok {
			t.Fatalf("a fixture rule is not an object: %v", rule)
		}

		rules = append(rules, object)
	}

	return rules
}

// withoutRule returns ruleset with the rules of type ruleType removed.
func withoutRule(t *testing.T, ruleset map[string]any, ruleType string) map[string]any {
	t.Helper()

	kept := make([]any, 0, len(rulesOf(t, ruleset)))

	for _, rule := range rulesOf(t, ruleset) {
		if rule["type"] != ruleType {
			kept = append(kept, rule)
		}
	}

	ruleset["rules"] = kept

	return ruleset
}

// gateSwitchedOverStdout is what the gate check prints on a repository whose gate is complete: every check
// holds, one ok: line each.
func gateSwitchedOverStdout() []string {
	return []string{
		"ok: Protect main is enforced",
		"ok: Protect main requires a pull request",
		"ok: required_approving_review_count is at least 1",
		"ok: require_code_owner_review is true",
		"ok: dismiss_stale_reviews_on_push is true",
		"ok: require_last_push_approval is true",
		"ok: bypass_actors is empty",
		"ok: CODEOWNERS on main has at least one rule",
		"ok: CODEOWNERS on main has a * rule",
		"ok: every CODEOWNERS rule has an owner",
		"ok: every CODEOWNERS owner is @timothymamo",
	}
}

func TestProtectMainCheckPassesOnASwitchedOverRepository(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{name: "fails closed and passes when nothing is a gap"},
		{name: "with --transition, which changes nothing when nothing is a gap", args: []string{"--transition"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			f.serveSwitchedOver(t)

			stdout, stderr, status := f.run(t, tc.args...)

			if status != 0 {
				t.Fatalf("the gate check exited %d on a switched-over repository, want 0: no check is a gap\nstdout: %s\nstderr: %s", status, stdout, stderr)
			}

			if diff := cmp.Diff(gateSwitchedOverStdout(), outputLines(stdout)); diff != "" {
				t.Errorf("the gate check printed unexpected lines (-want +got):\n%s", diff)
			}

			if stderr != "" {
				t.Errorf("the gate check writes to stderr although every check holds; its report is stdout: %s", stderr)
			}
		})
	}
}

func TestProtectMainCheckReportsTodaysGaps(t *testing.T) {
	t.Parallel()

	// Today's ruleset: no required approval, none of the three review rules, no bypass actor, and a CODEOWNERS
	// file that is already complete. Four gaps.
	wantStdout := []string{
		"ok: Protect main is enforced",
		"ok: Protect main requires a pull request",
		"gap: required_approving_review_count is 0, want at least 1",
		"gap: require_code_owner_review is false",
		"gap: dismiss_stale_reviews_on_push is false",
		"gap: require_last_push_approval is false",
		"ok: bypass_actors is empty",
		"ok: CODEOWNERS on main has at least one rule",
		"ok: CODEOWNERS on main has a * rule",
		"ok: every CODEOWNERS rule has an owner",
		"ok: every CODEOWNERS owner is @timothymamo",
	}

	// The line the transition flag adds, holding the number of gaps it reports without failing.
	transition := "transition: gaps=4, not failing the release"

	cases := []struct {
		name       string
		args       []string
		wantStatus int
	}{
		{name: "before the switch-over a gap fails the check", wantStatus: 1},
		{name: "with --transition it reports the gaps and passes", args: []string{"--transition"}, wantStatus: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			f.serveRulesetList(t, readGateRulesetList(t))
			f.serveRuleset(t, readGateRuleset(t, "ruleset-today.json"))
			f.serveRecordedCodeowners(t)

			stdout, stderr, status := f.run(t, tc.args...)

			if status != tc.wantStatus {
				t.Fatalf("the gate check exited %d on today's ruleset, want %d\nstdout: %s\nstderr: %s", status, tc.wantStatus, stdout, stderr)
			}

			want := wantStdout
			if tc.wantStatus == 0 {
				want = slices.Concat(wantStdout, []string{transition})
			}

			if diff := cmp.Diff(want, outputLines(stdout)); diff != "" {
				t.Errorf("the gate check printed unexpected lines (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(wantStdout[2:6], gapLines(stdout)); diff != "" {
				t.Errorf("the four gaps of today's ruleset, and no other, are what a maintainer must close (-want +got):\n%s", diff)
			}
		})
	}
}

// TestProtectMainCheckReportsEachGapOnItsOwn pins one gap at a time, so a check that silently passes a gap, or
// reports it as another, fails on its own case.
func TestProtectMainCheckReportsEachGapOnItsOwn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		prepare func(t *testing.T, f *gateFixture)
		wantGap []string
	}{
		{
			name: "0 required approvals",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				pullRequestParameters(t, ruleset)["required_approving_review_count"] = float64(0)
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: required_approving_review_count is 0, want at least 1"},
		},
		{
			name: "require_code_owner_review false",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				pullRequestParameters(t, ruleset)["require_code_owner_review"] = false
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: require_code_owner_review is false"},
		},
		{
			name: "dismiss_stale_reviews_on_push false",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				pullRequestParameters(t, ruleset)["dismiss_stale_reviews_on_push"] = false
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: dismiss_stale_reviews_on_push is false"},
		},
		{
			name: "require_last_push_approval false",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				pullRequestParameters(t, ruleset)["require_last_push_approval"] = false
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: require_last_push_approval is false"},
		},
		{
			name: "a bypass actor",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				ruleset["bypass_actors"] = []any{map[string]any{
					"actor_id":    float64(67890),
					"actor_type":  "Team",
					"bypass_mode": "always",
				}}
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: bypass_actors is not empty: Team 67890 (always)"},
		},
		{
			// A token without Administration read gets the ruleset without its bypass list, so a missing list
			// is not a complete one: the maintainer must make this check.
			name: "no bypass_actors field",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				delete(ruleset, "bypass_actors")
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: bypass_actors could not be read: the token cannot read the ruleset's bypass list, so run make gate-check as the maintainer"},
		},
		{
			name: "a ruleset that is not enforced",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				ruleset := readGateRuleset(t, "ruleset-switched-over.json")
				ruleset["enforcement"] = "evaluate"
				f.serveRuleset(t, ruleset)
			},
			wantGap: []string{"gap: Protect main is not enforced: enforcement is evaluate"},
		},
		{
			// Without the rule the four review settings cannot be read at all, so the check reports the
			// missing rule alone instead of inventing their values.
			name: "no pull_request rule",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRuleset(t, withoutRule(t, readGateRuleset(t, "ruleset-switched-over.json"), "pull_request"))
			},
			wantGap: []string{"gap: Protect main has no pull_request rule"},
		},
		{
			name: "CODEOWNERS names another user",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveCodeowners(t, "* @timothymamo\n.github/ @someone-else\n")
			},
			wantGap: []string{"gap: CODEOWNERS owner @someone-else is not @timothymamo"},
		},
		{
			name: "CODEOWNERS names a team",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveCodeowners(t, "* @timothymamo\n.github/ @example-org/team\n")
			},
			wantGap: []string{"gap: CODEOWNERS owner @example-org/team is not @timothymamo"},
		},
		{
			name: "CODEOWNERS names an email address",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveCodeowners(t, "* @timothymamo\n.github/ docs@example.com\n")
			},
			wantGap: []string{"gap: CODEOWNERS owner docs@example.com is not @timothymamo"},
		},
		{
			name: "a CODEOWNERS rule with no owner",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveCodeowners(t, "* @timothymamo\n.github/\n")
			},
			wantGap: []string{"gap: CODEOWNERS rule .github/ has no owner"},
		},
		{
			name: "no CODEOWNERS rule for every path",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveCodeowners(t, "# The maintainer owns .github/, and nothing owns the rest.\n.github/ @timothymamo\n")
			},
			wantGap: []string{"gap: CODEOWNERS on main has no * rule"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			f.serveSwitchedOver(t)
			tc.prepare(t, f)

			stdout, stderr, status := f.run(t)

			if status != 1 {
				t.Fatalf("the gate check exited %d with %s, want 1: a gap fails it before the switch-over\nstdout: %s\nstderr: %s", status, tc.name, stdout, stderr)
			}

			if diff := cmp.Diff(tc.wantGap, gapLines(stdout)); diff != "" {
				t.Errorf("the gate check reported other gaps than %s is (-want +got):\n%s", tc.name, diff)
			}
		})
	}
}

// TestProtectMainCheckCannotCheckExitsTwo pins that a check the script cannot make is no passing check: it
// exits 2 in both modes, so neither a release nor the maintainer's manual run reads a gap it could not read as
// a gate that holds.
func TestProtectMainCheckCannotCheckExitsTwo(t *testing.T) {
	t.Parallel()

	serveBoth := func(t *testing.T, f *gateFixture) {
		t.Helper()

		f.serveRulesetList(t, readGateRulesetList(t))
		f.serveRuleset(t, readGateRuleset(t, "ruleset-switched-over.json"))
	}

	cases := []struct {
		name       string
		prepare    func(t *testing.T, f *gateFixture)
		extraArgs  []string
		wantStderr []string // the strings the refusal must name, so a maintainer knows what to fix
	}{
		{
			// Nothing is served at all: the first read fails, which is where the check must stop.
			name:       "gh fails on the rulesets list",
			prepare:    func(t *testing.T, _ *gateFixture) { t.Helper() },
			wantStderr: []string{gateEndpoints[0]},
		},
		{
			name: "gh fails on the ruleset",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetList(t, readGateRulesetList(t))
			},
			wantStderr: []string{gateEndpoints[1]},
		},
		{
			name:    "gh fails on the CODEOWNERS file",
			prepare: serveBoth,
			wantStderr: []string{
				".github/CODEOWNERS",
			},
		},
		{
			name: "no ruleset is named Protect main",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetList(t, []any{map[string]any{
					"id":          float64(200001),
					"name":        "Release tags",
					"target":      "tag",
					"enforcement": "active",
				}})
				f.serveRuleset(t, readGateRuleset(t, "ruleset-switched-over.json"))
				f.serveRecordedCodeowners(t)
			},
			wantStderr: []string{"Protect main"},
		},
		{
			name: "two rulesets are named Protect main",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				hijacker := map[string]any{
					"id":          float64(200002),
					"name":        "Protect main",
					"target":      "branch",
					"enforcement": "evaluate",
				}

				f.serveRulesetList(t, slices.Concat(readGateRulesetList(t), []any{hijacker}))
				f.serveRuleset(t, readGateRuleset(t, "ruleset-switched-over.json"))
				f.serveRecordedCodeowners(t)
			},
			wantStderr: []string{"Protect main"},
		},
		{
			name: "the rulesets list is not JSON",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRaw(t, "rulesets.json", "[{\n")
			},
		},
		{
			name:      "an unknown argument",
			prepare:   serveBoth,
			extraArgs: []string{"--nonsense"},
			wantStderr: []string{
				"--nonsense",
			},
		},
	}

	modes := []struct {
		name string
		args []string
	}{
		{name: "without --transition"},
		{name: "with --transition", args: []string{"--transition"}},
	}

	for _, tc := range cases {
		for _, mode := range modes {
			t.Run(tc.name+", "+mode.name, func(t *testing.T) {
				t.Parallel()

				f := newGateFixture(t)
				tc.prepare(t, f)

				stdout, stderr, status := f.run(t, slices.Concat(mode.args, tc.extraArgs)...)

				if status != 2 {
					t.Fatalf("the gate check exited %d when it cannot make the checks (%s), want 2 in both modes: a check it could not make is no passing check\nstdout: %s\nstderr: %s",
						status, tc.name, stdout, stderr)
				}

				if strings.TrimSpace(stderr) == "" {
					t.Fatalf("the gate check exits 2 for %s and says nothing on stderr, so a maintainer cannot tell what stopped it", tc.name)
				}

				for _, want := range tc.wantStderr {
					if !strings.Contains(stderr, want) {
						t.Errorf("the refusal does not name %q, so a maintainer cannot tell what to fix: %s", want, stderr)
					}
				}
			})
		}
	}
}

// TestProtectMainCheckMakesThreeReadOnlyGHCalls pins what the check touches: three GETs through gh, no write
// flag, no gh --jq, no curl, and nothing of the environment on any command line or on the report.
func TestProtectMainCheckMakesThreeReadOnlyGHCalls(t *testing.T) {
	t.Parallel()

	f := newGateFixture(t)
	f.serveSwitchedOver(t)

	// A token in the environment is the maintainer's, and the check reads no environment value: neither its
	// report nor a command line may hold it (CLAUDE.md rule 3).
	personalToken := fmt.Sprintf("ghp_%036x", 21)
	f.env["GITHUB_TOKEN"] = personalToken

	stdout, stderr, status := f.run(t)
	if status != 0 {
		t.Fatalf("the gate check exited %d on a switched-over repository, want 0\nstdout: %s\nstderr: %s", status, stdout, stderr)
	}

	log := f.sb.argvLogText(t)

	calls := invocations(log, "gh")
	if len(calls) != len(gateEndpoints) {
		t.Fatalf("the gate check ran gh %d time(s), want the three reads %v", len(calls), gateEndpoints)
	}

	var read []string

	for _, args := range calls {
		read = append(read, endpointOf(t, args))
	}

	slices.Sort(read)

	want := slices.Clone(gateEndpoints)
	slices.Sort(want)

	if diff := cmp.Diff(want, read); diff != "" {
		t.Errorf("the gate check read other endpoints than the three its contract names (-want +got):\n%s", diff)
	}

	assertReadOnlyGHCalls(t, calls)

	if curls := invocations(log, "curl"); len(curls) != 0 {
		t.Errorf("the gate check ran curl %d time(s): it reads GitHub through gh only", len(curls))
	}

	secrets := []secret{{kind: "personal access token", value: personalToken}}
	assertNoSecrets(t, "the gate check's stdout", stdout, secrets)
	assertNoSecrets(t, "the gate check's stderr", stderr, secrets)
	assertNoSecrets(t, "the argv of the commands the gate check ran", log, secrets)
}

// endpointOf returns the GitHub endpoint among a gh call's arguments.
func endpointOf(t *testing.T, args []string) string {
	t.Helper()

	for _, arg := range args {
		if strings.HasPrefix(arg, "repos/") {
			return arg
		}
	}

	t.Fatalf("a gh call carries no repos/ endpoint: %v", args)

	return ""
}

// assertReadOnlyGHCalls fails on any gh api argument that would write, send a body or parse the answer in gh:
// the gate check reads, and parses with jq.
func assertReadOnlyGHCalls(t *testing.T, calls [][]string) {
	t.Helper()

	writeFlags := []string{"-f", "-F", "--field", "--raw-field", "--input", "--jq"}

	for i, args := range calls {
		for j, arg := range args {
			var offending bool

			for _, flag := range writeFlags {
				offending = offending || arg == flag || strings.HasPrefix(arg, flag+"=")
			}

			// -f and -F also take their value in the same argument.
			offending = offending || (len(arg) > 2 && (strings.HasPrefix(arg, "-f") || strings.HasPrefix(arg, "-F")))

			if offending {
				t.Errorf("gh call %d passes %q: the gate check writes nothing and parses with jq, never with gh %v", i+1, arg, args)
			}

			assertGetMethod(t, i, args, j, arg)
		}
	}
}

// assertGetMethod fails unless the method flag at index j of a gh call, when there is one, asks for GET.
func assertGetMethod(t *testing.T, call int, args []string, j int, arg string) {
	t.Helper()

	switch {
	case arg == "--method" || arg == "-X":
		if j+1 >= len(args) || args[j+1] != "GET" {
			t.Errorf("gh call %d asks for the method after %q, which is not GET: the gate check reads only %v", call+1, arg, args)
		}
	case strings.HasPrefix(arg, "--method="):
		if arg != "--method=GET" {
			t.Errorf("gh call %d passes %q: the gate check reads only", call+1, arg)
		}
	case strings.HasPrefix(arg, "-X"):
		if arg != "-XGET" {
			t.Errorf("gh call %d passes %q: the gate check reads only", call+1, arg)
		}
	}
}
