// Pins the Protect main gate check (PRD #161 milestone 2, and milestone 3 for the transition flag's removal).
// dev/protect-main-check.sh reads the repository, the `Protect main` ruleset and .github/CODEOWNERS on main
// (with GitHub's CODEOWNERS errors for it) through gh's read-only API, prints one line per check of three kinds
// - `ok: ...`, `gap: ...` and `unverified: ...` - and fails closed: after the switch-over a gap fails the check,
// and the `--transition` mode of milestone 2 is an unknown argument now. The `unverified:` kind is for what only
// the maintainer's own token may read: an installation token cannot see the ruleset's bypass list, so an empty
// one proves nothing. It exits 2 whenever it cannot make the checks at all, and an argument it does not know -
// `--transition` included - is such a case.
//
// The recorded answers are GitHub's documented REST shapes with invented identifiers, in
// testdata/gatecheck/; a fake gh on PATH serves them, refuses any endpoint that is not the pinned repository
// and records its argv, so no test reaches the network or an account and every call the script makes is
// pinned.
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

// gateEndpoints are the five reads the gate check makes, in the order its contract lists them: the rulesets
// list (paginated), the Protect main ruleset, the repository - whose .permissions.admin says whether this
// token may read the ruleset's bypass list at all - the CODEOWNERS file on main, and GitHub's CODEOWNERS
// errors for that file.
var gateEndpoints = []string{
	gateListEndpoint,
	gateRulesetEndpoint,
	gateRepositoryEndpoint,
	gateCodeownersEndpoint,
	gateErrorsEndpoint,
}

const (
	// gateRepositoryEndpoint is the repository the check must mean, spelled out: gh fills {owner} and {repo}
	// from the checkout's remote, so a placeholder would let a fork's remote, another remote or GH_REPO decide
	// what is checked.
	gateRepositoryEndpoint = "repos/timothymamo/shift2eu"

	gateListEndpoint       = gateRepositoryEndpoint + "/rulesets"
	gateRulesetEndpoint    = gateListEndpoint + "/" + gateRulesetID
	gateCodeownersEndpoint = gateRepositoryEndpoint + "/contents/.github/CODEOWNERS?ref=main"
	gateErrorsEndpoint     = gateRepositoryEndpoint + "/codeowners/errors?ref=main"
)

// The lines the gate check prints on a repository whose gate is complete, read with the maintainer's own
// token, in the order its contract prints them.
const (
	okEnforced             = "ok: Protect main is enforced"
	okTargetsBranches      = "ok: Protect main targets branches"
	okIncludesMain         = "ok: Protect main includes main"
	okDoesNotExcludeMain   = "ok: Protect main does not exclude main"
	okRequiresPullRequest  = "ok: Protect main requires a pull request"
	okApprovals            = "ok: required_approving_review_count is at least 1"
	okCodeOwnerReview      = "ok: require_code_owner_review is true"
	okDismissStale         = "ok: dismiss_stale_reviews_on_push is true"
	okLastPushApproval     = "ok: require_last_push_approval is true"
	okBypassEmpty          = "ok: bypass_actors is empty"
	okCannotOverride       = "ok: this token cannot bypass Protect main"
	okCodeownersRules      = "ok: CODEOWNERS on main has at least one rule"
	okCodeownersStar       = "ok: CODEOWNERS on main has a * rule"
	okCodeownersOwners     = "ok: every CODEOWNERS rule has an owner"
	okCodeownersMaintainer = "ok: every CODEOWNERS owner is @timothymamo"
	okCodeownersNoErrors   = "ok: GitHub reports no CODEOWNERS errors on main"
)

// The lines today's ruleset changes, one gap each.
const (
	gapApprovals       = "gap: required_approving_review_count is 0, want at least 1"
	gapCodeOwnerReview = "gap: require_code_owner_review is false"
	gapDismissStale    = "gap: dismiss_stale_reviews_on_push is false"
	gapLastPush        = "gap: require_last_push_approval is false"
)

// unverifiedActors is the one line a token that is not a repository admin gets in place of the bypass check:
// an empty bypass list proves nothing when the token could not have read a non-empty one.
const unverifiedActors = "unverified: bypass_actors cannot be verified with this token (not a repository admin): run make gate-check as the maintainer"

// reportPrefixes are the only prefixes a line of the report may start with.
var reportPrefixes = []string{"ok: ", "gap: ", "unverified: "}

// gateOkLines is the report of a gate that is complete in every respect but nothing else, in contract order.
func gateOkLines() []string {
	return []string{
		okEnforced,
		okTargetsBranches,
		okIncludesMain,
		okDoesNotExcludeMain,
		okRequiresPullRequest,
		okApprovals,
		okCodeOwnerReview,
		okDismissStale,
		okLastPushApproval,
		okBypassEmpty,
		okCannotOverride,
		okCodeownersRules,
		okCodeownersStar,
		okCodeownersOwners,
		okCodeownersMaintainer,
		okCodeownersNoErrors,
	}
}

// gateFakeGH stands in for gh in the gate check's tests: it records its argv the way the sandbox's other
// shims do, finds the endpoint among its arguments (gh api allows flags on either side of it), refuses one
// that is not the pinned repository and answers with the recorded file the endpoint names. A file the test
// did not write is the API's failure, which gh reports as a non-zero exit.
const gateFakeGH = `#!/bin/sh
# Serves the recorded answers of testdata/gatecheck/ to the gate check.
{
  printf 'tool=gh\n'
  for a in "$@"; do printf 'arg=%s\n' "$a"; done
} >> "${SHIFT2EU_TEST_ARGV_LOG:?set SHIFT2EU_TEST_ARGV_LOG}"
serve="${SHIFT2EU_TEST_GH_SERVE:?set SHIFT2EU_TEST_GH_SERVE}"
pinned='repos/timothymamo/shift2eu'
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
"${pinned}")
  rest=""
  ;;
"${pinned}"/*)
  rest="${endpoint#"${pinned}"}"
  ;;
*)
  echo "fake gh: the gate check reads ${endpoint}, which is not the pinned repository ${pinned}: a placeholder endpoint lets a fork or GH_REPO decide what is checked" >&2
  exit 64
  ;;
esac
case "${rest}" in
'')
  file="${serve}/repository.json"
  ;;
/rulesets)
  file="${serve}/rulesets.json"
  ;;
/rulesets/*)
  file="${serve}/ruleset-${rest##*/}.json"
  ;;
'/contents/.github/CODEOWNERS?ref=main')
  file="${serve}/codeowners.json"
  ;;
'/codeowners/errors?ref=main')
  file="${serve}/codeowners-errors.json"
  ;;
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

// gateBrokenJQ stands in for a jq the machine has but cannot run: an Intel binary on an Apple-silicon Mac answers
// "Bad CPU type in executable" and the shell exits 126, which is what the check has to report instead of blaming
// GitHub's answer, the way it took several rounds to find on the maintainer's admin Mac (issue #321). Its message
// carries a terminal escape sequence, which is jq's own text and must not reach the maintainer's terminal either.
const gateBrokenJQ = `#!/bin/sh
printf 'Bad CPU type in executable\033[31m\n' >&2
exit 126
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

// linesWithPrefix returns the lines of a run's stdout that start with prefix, in the order it printed them.
func linesWithPrefix(stdout, prefix string) []string {
	var lines []string

	for _, line := range outputLines(stdout) {
		if strings.HasPrefix(line, prefix) {
			lines = append(lines, line)
		}
	}

	return lines
}

// gapLines returns the gap: lines of a run's stdout, in the order it printed them.
func gapLines(stdout string) []string {
	return linesWithPrefix(stdout, "gap: ")
}

// assertReport fails unless run printed exactly want on stdout and nothing on stderr.
func assertReport(t *testing.T, want []string, stdout, stderr string) {
	t.Helper()

	if diff := cmp.Diff(want, outputLines(stdout)); diff != "" {
		t.Errorf("the gate check printed unexpected lines (-want +got):\n%s", diff)
	}

	if stderr != "" {
		t.Errorf("the gate check writes to stderr when it can make every check; its report is stdout: %s", stderr)
	}
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

// unserve removes one recorded answer, which is the API failing that one read: the fake gh reports it the way
// gh reports a 404.
func (f *gateFixture) unserve(t *testing.T, name string) {
	t.Helper()

	if err := os.Remove(filepath.Join(f.serve, name)); err != nil {
		t.Fatalf("remove the fake API's %s answer: %v", name, err)
	}
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

// serveRulesetWith writes the switched-over ruleset as the answer of the ruleset endpoint, with change applied
// to it: one case changes the one field it pins.
func (f *gateFixture) serveRulesetWith(t *testing.T, change func(t *testing.T, ruleset map[string]any)) {
	t.Helper()

	ruleset := readGateRuleset(t, "ruleset-switched-over.json")
	change(t, ruleset)
	f.serveRuleset(t, ruleset)
}

// recordedRepository returns the recorded answer of the repository endpoint.
func recordedRepository(t *testing.T) map[string]any {
	t.Helper()

	repository, ok := readGateFixture(t, "repository.json").(map[string]any)
	if !ok {
		t.Fatalf("%s/repository.json is not an object: gh api answers the repository with one", gateFixtureDir)
	}

	return repository
}

// serveRepository writes the answer of the repository endpoint: the recorded one, which is the maintainer's
// own token with repository administration (admin), or the same answer without its permissions object, which
// is one of the two shapes an App installation token's answer takes.
func (f *gateFixture) serveRepository(t *testing.T, admin bool) {
	t.Helper()

	repository := recordedRepository(t)
	if !admin {
		delete(repository, "permissions")
	}

	f.serveJSON(t, "repository.json", repository)
}

// serveRepositoryWithoutAdmin writes the repository endpoint's answer for an installation token whose
// permissions object is there but says admin false, which is the other shape its answer takes. Both leave the
// ruleset's bypass list unreadable.
func (f *gateFixture) serveRepositoryWithoutAdmin(t *testing.T) {
	t.Helper()

	repository := recordedRepository(t)
	repository["permissions"] = map[string]any{
		"admin":    false,
		"maintain": false,
		"push":     true,
		"triage":   true,
		"pull":     true,
	}

	f.serveJSON(t, "repository.json", repository)
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

// serveCodeownersErrors writes the answer of the codeowners/errors endpoint: GitHub's report on the CODEOWNERS
// file on main, empty when the file is valid.
func (f *gateFixture) serveCodeownersErrors(t *testing.T, errs []any) {
	t.Helper()

	f.serveJSON(t, "codeowners-errors.json", map[string]any{"errors": errs})
}

// serveNoCodeownersErrors writes the recorded answer of the codeowners/errors endpoint, which holds no errors.
func (f *gateFixture) serveNoCodeownersErrors(t *testing.T) {
	t.Helper()

	f.serveJSON(t, "codeowners-errors.json", readGateFixture(t, "codeowners-errors.json"))
}

// serveSwitchedOver produces the answers of a repository whose gate is complete: the recorded rulesets list,
// the recorded switched-over ruleset and the recorded CODEOWNERS file. The token's answers are not part of it:
// serveSwitchedOverAs adds them.
func (f *gateFixture) serveSwitchedOver(t *testing.T) {
	t.Helper()

	f.serveRulesetList(t, readGateRulesetList(t))
	f.serveRuleset(t, readGateRuleset(t, "ruleset-switched-over.json"))
	f.serveRecordedCodeowners(t)
}

// serveSwitchedOverAs writes every answer of a repository whose gate is complete, read by the maintainer's own
// token (admin) or by an App installation token (not admin).
func (f *gateFixture) serveSwitchedOverAs(t *testing.T, admin bool) {
	t.Helper()

	f.serveSwitchedOver(t)
	f.serveRepository(t, admin)
	f.serveNoCodeownersErrors(t)
}

// serveTodayAs writes every answer of today's repository: the recorded rulesets list, the recorded today
// ruleset and the recorded CODEOWNERS file, read by the maintainer's own token (admin) or by an App
// installation token (not admin).
func (f *gateFixture) serveTodayAs(t *testing.T, admin bool) {
	t.Helper()

	f.serveRulesetList(t, readGateRulesetList(t))
	f.serveRuleset(t, readGateRuleset(t, "ruleset-today.json"))
	f.serveRecordedCodeowners(t)
	f.serveRepository(t, admin)
	f.serveNoCodeownersErrors(t)
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

// refNameConditions returns a ruleset's conditions.ref_name object, which says which refs it targets.
func refNameConditions(t *testing.T, ruleset map[string]any) map[string]any {
	t.Helper()

	conditions, ok := ruleset["conditions"].(map[string]any)
	if !ok {
		t.Fatalf("the fixture ruleset holds no conditions object: %v", ruleset["conditions"])
	}

	refName, ok := conditions["ref_name"].(map[string]any)
	if !ok {
		t.Fatalf("the fixture ruleset's conditions hold no ref_name object: %v", conditions["ref_name"])
	}

	return refName
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

func TestProtectMainCheckPassesOnASwitchedOverRepository(t *testing.T) {
	t.Parallel()

	f := newGateFixture(t)
	f.serveSwitchedOverAs(t, true)

	stdout, stderr, status := f.run(t)

	if status != 0 {
		t.Fatalf("the gate check exited %d on a switched-over repository, want 0: no check is a gap and none is unverified\nstdout: %s\nstderr: %s", status, stdout, stderr)
	}

	assertReport(t, gateOkLines(), stdout, stderr)
}

// TestProtectMainCheckCannotVerifyTheBypassListWithoutAnAdminToken pins the third kind of line: an installation
// token's repository answer holds no permissions object, so the token may not read the ruleset's bypass list,
// and an empty list proves nothing. It is not a gap - the token says nothing about the repository - but it is
// no passing check either, so the check exits 3.
func TestProtectMainCheckCannotVerifyTheBypassListWithoutAnAdminToken(t *testing.T) {
	t.Parallel()

	// Every other check holds; only the bypass list cannot be verified with this token.
	want := slices.Concat(gateOkLines()[:9], []string{unverifiedActors}, gateOkLines()[10:])

	// An installation token's answer takes one of two shapes, and neither proves the token may administer the
	// repository: one with no permissions object at all, one whose admin is false.
	noPermissions := func(t *testing.T, f *gateFixture) {
		t.Helper()

		f.serveSwitchedOver(t)
		f.serveRepository(t, false)
		f.serveNoCodeownersErrors(t)
	}

	notAdmin := func(t *testing.T, f *gateFixture) {
		t.Helper()

		f.serveSwitchedOver(t)
		f.serveRepositoryWithoutAdmin(t)
		f.serveNoCodeownersErrors(t)
	}

	cases := []struct {
		name       string
		prepare    func(t *testing.T, f *gateFixture)
		args       []string
		wantStatus int
		want       []string
	}{
		{
			name:       "the answer holds no permissions object",
			prepare:    noPermissions,
			wantStatus: 3,
			want:       want,
		},
		{
			name:       "the answer's permissions say admin false",
			prepare:    notAdmin,
			wantStatus: 3,
			want:       want,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			tc.prepare(t, f)

			stdout, stderr, status := f.run(t)

			if status != tc.wantStatus {
				t.Fatalf("the gate check exited %d on a switched-over repository it cannot fully verify, want %d\nstdout: %s\nstderr: %s",
					status, tc.wantStatus, stdout, stderr)
			}

			assertReport(t, tc.want, stdout, stderr)
		})
	}
}

func TestProtectMainCheckReportsTodaysGaps(t *testing.T) {
	t.Parallel()

	// Today's ruleset: no required approval, none of the three review rules, no bypass actor, and a CODEOWNERS
	// file that is already complete. Four gaps, and - read with the maintainer's own token - no unverified line.
	wantAdmin := []string{
		okEnforced,
		okTargetsBranches,
		okIncludesMain,
		okDoesNotExcludeMain,
		okRequiresPullRequest,
		gapApprovals,
		gapCodeOwnerReview,
		gapDismissStale,
		gapLastPush,
		okBypassEmpty,
		okCannotOverride,
		okCodeownersRules,
		okCodeownersStar,
		okCodeownersOwners,
		okCodeownersMaintainer,
		okCodeownersNoErrors,
	}

	// An installation token gets the unverified line in place of the bypass check: one more thing it cannot
	// establish, and no gap either way.
	wantApp := slices.Concat(wantAdmin[:9], []string{unverifiedActors}, wantAdmin[10:])

	cases := []struct {
		name       string
		admin      bool
		wantStatus int
		want       []string
	}{
		{
			name:       "a gap fails the check",
			admin:      true,
			wantStatus: 1,
			want:       wantAdmin,
		},
		{
			name:       "an App installation token's gaps fail it too",
			wantStatus: 1,
			want:       wantApp,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			f.serveTodayAs(t, tc.admin)

			stdout, stderr, status := f.run(t)

			if status != tc.wantStatus {
				t.Fatalf("the gate check exited %d on today's ruleset, want %d\nstdout: %s\nstderr: %s", status, tc.wantStatus, stdout, stderr)
			}

			assertReport(t, tc.want, stdout, stderr)

			if diff := cmp.Diff([]string{gapApprovals, gapCodeOwnerReview, gapDismissStale, gapLastPush}, gapLines(stdout)); diff != "" {
				t.Errorf("the four gaps of today's ruleset, and no other, are what a maintainer must close (-want +got):\n%s", diff)
			}
		})
	}
}

// TestProtectMainCheckRejectsTheTransitionFlag pins PRD #161 milestone 3: once the switch-over is done the
// check has no transition mode any more, so its own flag is an unknown argument. It is refused before any read
// - exit 2 with nothing that looks like a report, as any other argument the script does not know - and the gaps
// it does find always fail: there is no mode in which a gap passes.
//
// The gate's own state decides nothing here either: a repository whose gate is on and one whose gate is off both
// get the same refusal, so a Makefile that still passed the flag could not hold a gap back.
func TestProtectMainCheckRejectsTheTransitionFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		prepare func(t *testing.T, f *gateFixture)
	}{
		{
			name:    "on a switched-over repository",
			prepare: func(t *testing.T, f *gateFixture) { t.Helper(); f.serveSwitchedOverAs(t, true) },
		},
		{
			// Today's ruleset, before the switch-over: the gaps must fail, and the flag that used to pass them
			// is no longer something the script accepts at all.
			name:    "on a repository with gaps",
			prepare: func(t *testing.T, f *gateFixture) { t.Helper(); f.serveTodayAs(t, true) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			tc.prepare(t, f)

			stdout, stderr, status := f.run(t, "--transition")

			if status != 2 {
				t.Fatalf("the gate check exited %d with --transition (%s), want 2: the transition mode is gone in PRD #161 milestone 3, so the flag is an unknown argument, as every unknown argument is a check the script cannot make\nstdout: %s\nstderr: %s",
					status, tc.name, stdout, stderr)
			}

			if !strings.Contains(stderr, "--transition") {
				t.Errorf("the refusal does not name --transition, so a maintainer who still passes it cannot tell that it is what the script does not know: %s", stderr)
			}

			if lines := outputLines(stdout); len(lines) != 0 {
				t.Errorf("the gate check printed %v before refusing --transition: an argument it does not know is refused before it reads anything, so no line of a report can be mistaken for a verdict on the gate", lines)
			}

			if calls := invocations(f.sb.argvLogText(t), "gh"); len(calls) != 0 {
				t.Errorf("the gate check ran gh %d time(s) with --transition: the argument is refused before any read, so a maintainer does not wait on the API to learn that the flag is gone", len(calls))
			}

			assertCleanReport(t, "the refusal's stderr", stderr)
		})
	}
}

// TestProtectMainCheckReportsAVisibleBypassActorWhateverTheToken pins that a bypass actor that can be read is a
// gap even for a token that may not read the list: a visible bypass actor skips every rule of the ruleset.
func TestProtectMainCheckReportsAVisibleBypassActorWhateverTheToken(t *testing.T) {
	t.Parallel()

	bypass := func(t *testing.T, ruleset map[string]any) {
		t.Helper()

		ruleset["bypass_actors"] = []any{map[string]any{
			"actor_id":    float64(67890),
			"actor_type":  "Team",
			"bypass_mode": "always",
		}}
	}

	cases := []struct {
		name  string
		admin bool
	}{
		{name: "with the maintainer's own token", admin: true},
		{name: "with an App installation token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			f.serveSwitchedOverAs(t, tc.admin)
			f.serveRulesetWith(t, bypass)

			stdout, stderr, status := f.run(t)

			if status != 1 {
				t.Fatalf("the gate check exited %d with a bypass actor, want 1: a visible bypass actor is a gap\nstdout: %s\nstderr: %s", status, stdout, stderr)
			}

			want := []string{"gap: bypass_actors is not empty: Team 67890 (always)"}
			if diff := cmp.Diff(want, gapLines(stdout)); diff != "" {
				t.Errorf("the gate check reported other gaps than the bypass actor (-want +got):\n%s", diff)
			}

			if unverified := linesWithPrefix(stdout, "unverified: "); len(unverified) != 0 {
				t.Errorf("the gate check reported %s as unverified although it read it:\n%s", strings.Join(unverified, ", "), stdout)
			}
		})
	}
}

// TestProtectMainCheckReportsEachGapOnItsOwn pins one gap at a time, so a check that silently passes a gap, or
// reports it as another, fails on its own case. Every case runs with the maintainer's own token, so no case is
// unverified instead.
func TestProtectMainCheckReportsEachGapOnItsOwn(t *testing.T) {
	t.Parallel()

	gapActor := "gap: bypass_actors is not empty: Team 67890 (always)"

	cases := []struct {
		name    string
		prepare func(t *testing.T, f *gateFixture)
		wantGap []string
		// wantNotPrinted names lines the run must not print, for a case whose gap would otherwise let a
		// contradicting ok: line stand beside it.
		wantNotPrinted []string
	}{
		{
			name: "0 required approvals",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					pullRequestParameters(t, ruleset)["required_approving_review_count"] = float64(0)
				})
			},
			wantGap: []string{gapApprovals},
		},
		{
			name: "require_code_owner_review false",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					pullRequestParameters(t, ruleset)["require_code_owner_review"] = false
				})
			},
			wantGap: []string{gapCodeOwnerReview},
		},
		{
			name: "dismiss_stale_reviews_on_push false",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					pullRequestParameters(t, ruleset)["dismiss_stale_reviews_on_push"] = false
				})
			},
			wantGap: []string{gapDismissStale},
		},
		{
			name: "require_last_push_approval false",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					pullRequestParameters(t, ruleset)["require_last_push_approval"] = false
				})
			},
			wantGap: []string{gapLastPush},
		},
		{
			name: "a bypass actor",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					ruleset["bypass_actors"] = []any{map[string]any{
						"actor_id":    float64(67890),
						"actor_type":  "Team",
						"bypass_mode": "always",
					}}
				})
			},
			wantGap: []string{gapActor},
		},
		{
			// The maintainer's own token reads the list, so a missing one is not a token limitation: it is a
			// ruleset without the field, which a maintainer must not leave unchecked.
			name: "no bypass_actors field with the maintainer's own token",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					delete(ruleset, "bypass_actors")
				})
			},
			wantGap: []string{"gap: bypass_actors is missing although this token is a repository admin"},
		},
		{
			// Any token reads current_user_can_bypass: if this one may bypass the ruleset, every other rule of
			// it is worth nothing.
			name: "this token can bypass Protect main",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					ruleset["current_user_can_bypass"] = "always"
				})
			},
			wantGap: []string{"gap: this token can bypass Protect main: current_user_can_bypass is always"},
		},
		{
			name: "no current_user_can_bypass field",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					delete(ruleset, "current_user_can_bypass")
				})
			},
			wantGap: []string{"gap: this token can bypass Protect main: current_user_can_bypass is missing"},
		},
		{
			// A ruleset that targets tags protects no branch at all.
			name: "a ruleset that targets tags",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					ruleset["target"] = "tag"
				})
			},
			wantGap: []string{"gap: Protect main targets tag, not branches"},
		},
		{
			name: "no target field",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					delete(ruleset, "target")
				})
			},
			wantGap: []string{"gap: Protect main targets missing, not branches"},
		},
		{
			name: "an include that does not cover main",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					refNameConditions(t, ruleset)["include"] = []any{"refs/heads/release"}
				})
			},
			wantGap: []string{"gap: Protect main does not include main"},
		},
		{
			name: "an exclude that matches main",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					refNameConditions(t, ruleset)["exclude"] = []any{"refs/heads/main"}
				})
			},
			wantGap: []string{"gap: Protect main excludes main: refs/heads/main"},
		},
		{
			name: "an exclude of every branch",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					refNameConditions(t, ruleset)["exclude"] = []any{"~ALL"}
				})
			},
			wantGap: []string{"gap: Protect main excludes main: ~ALL"},
		},
		// GitHub matches a ruleset's ref_name patterns as shell globs, so a character class, a brace
		// expansion or an escape can select main. The check reads the pattern with its own glob matcher and
		// prints only the pattern back, so it cannot tell whether such a pattern excludes main: it must say
		// it may, so that a pattern it cannot read never reads as a gate that holds.
		{
			name: "an exclude with a character class",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					refNameConditions(t, ruleset)["exclude"] = []any{"refs/heads/[m]ain"}
				})
			},
			wantGap: []string{
				"gap: Protect main cannot read exclude pattern refs/heads/[m]ain, so it may exclude main",
			},
			wantNotPrinted: []string{okDoesNotExcludeMain},
		},
		{
			name: "an exclude with a brace expansion",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					refNameConditions(t, ruleset)["exclude"] = []any{"refs/heads/{main,dev}"}
				})
			},
			wantGap: []string{
				"gap: Protect main cannot read exclude pattern refs/heads/{main,dev}, so it may exclude main",
			},
			wantNotPrinted: []string{okDoesNotExcludeMain},
		},
		{
			name: "an exclude with an escape",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					refNameConditions(t, ruleset)["exclude"] = []any{`refs/heads/ma\in`}
				})
			},
			wantGap: []string{
				`gap: Protect main cannot read exclude pattern refs/heads/ma\in, so it may exclude main`,
			},
			wantNotPrinted: []string{okDoesNotExcludeMain},
		},
		{
			name: "a ruleset that is not enforced",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
					t.Helper()

					ruleset["enforcement"] = "evaluate"
				})
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
			// A CODEOWNERS file GitHub itself reports errors for gives no rule to depend on.
			name: "CODEOWNERS errors",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveCodeownersErrors(t, []any{
					map[string]any{
						"kind":     "Invalid owner",
						"path":     ".github/CODEOWNERS",
						"line":     float64(2),
						"bad_line": ".github/ @example-org/team",
					},
					map[string]any{
						"kind":     "Invalid owner",
						"path":     ".github/CODEOWNERS",
						"line":     float64(3),
						"bad_line": "docs/ docs@example.com",
					},
				})
			},
			wantGap: []string{"gap: GitHub reports 2 CODEOWNERS errors on main"},
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
			f.serveSwitchedOverAs(t, true)
			tc.prepare(t, f)

			stdout, stderr, status := f.run(t)

			if status != 1 {
				t.Fatalf("the gate check exited %d with %s, want 1: a gap fails it\nstdout: %s\nstderr: %s", status, tc.name, stdout, stderr)
			}

			if diff := cmp.Diff(tc.wantGap, gapLines(stdout)); diff != "" {
				t.Errorf("the gate check reported other gaps than %s is (-want +got):\n%s", tc.name, diff)
			}

			printed := outputLines(stdout)
			for _, line := range tc.wantNotPrinted {
				if slices.Contains(printed, line) {
					t.Errorf("the gate check printed %q with %s: it contradicts the gap it reported\nstdout: %s", line, tc.name, stdout)
				}
			}

			if unverified := linesWithPrefix(stdout, "unverified: "); len(unverified) != 0 {
				t.Errorf("the gate check reported %s as unverified although the token is a repository admin:\n%s", strings.Join(unverified, ", "), stdout)
			}
		})
	}
}

// TestProtectMainCheckCannotCheckExitsTwo pins that a check the script cannot make is no passing check: it
// exits 2, so neither a release nor the maintainer's manual run reads a gap it could not read as a gate that
// holds. An unknown argument - `--transition` included, now that the transition mode is gone - is one of them.
func TestProtectMainCheckCannotCheckExitsTwo(t *testing.T) {
	t.Parallel()

	// served writes every answer of a complete gate with the maintainer's token, then removes the one the case
	// makes fail: whichever order the check reads them in, that one read is the failing one.
	served := func(without ...string) func(t *testing.T, f *gateFixture) {
		return func(t *testing.T, f *gateFixture) {
			t.Helper()

			f.serveSwitchedOverAs(t, true)

			for _, name := range without {
				f.unserve(t, name)
			}
		}
	}

	cases := []struct {
		name       string
		prepare    func(t *testing.T, f *gateFixture)
		extraArgs  []string
		wantStderr []string // the strings the refusal must name, so a maintainer knows what to fix
	}{
		{
			// Nothing is served at all: the first read fails, whichever it is, and it must name its endpoint.
			name:       "nothing is served",
			prepare:    func(t *testing.T, _ *gateFixture) { t.Helper() },
			wantStderr: []string{gateRepositoryEndpoint},
		},
		{
			name:       "gh fails on the rulesets list",
			prepare:    served("rulesets.json"),
			wantStderr: []string{gateListEndpoint},
		},
		{
			name:       "gh fails on the ruleset",
			prepare:    served("ruleset-" + gateRulesetID + ".json"),
			wantStderr: []string{gateRulesetEndpoint},
		},
		{
			name:       "gh fails on the repository",
			prepare:    served("repository.json"),
			wantStderr: []string{gateRepositoryEndpoint},
		},
		{
			name:       "gh fails on the CODEOWNERS file",
			prepare:    served("codeowners.json"),
			wantStderr: []string{gateCodeownersEndpoint},
		},
		{
			name:       "gh fails on the CODEOWNERS errors",
			prepare:    served("codeowners-errors.json"),
			wantStderr: []string{gateErrorsEndpoint},
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
				f.serveRepository(t, true)
				f.serveNoCodeownersErrors(t)
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
				f.serveRepository(t, true)
				f.serveNoCodeownersErrors(t)
			},
			wantStderr: []string{"Protect main"},
		},
		{
			// A hostile id must not reach the terminal: the refusal is a line of its own, and the id is data
			// from the API.
			name: "a hostile ruleset id",
			prepare: func(t *testing.T, f *gateFixture) {
				t.Helper()

				f.serveRulesetList(t, []any{map[string]any{
					"id":          "123\u001b[31m456\u009b\ngap: forged",
					"name":        "Protect main",
					"target":      "branch",
					"enforcement": "active",
				}})
				f.serveRuleset(t, readGateRuleset(t, "ruleset-switched-over.json"))
				f.serveRecordedCodeowners(t)
				f.serveRepository(t, true)
				f.serveNoCodeownersErrors(t)
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
			prepare:   served(),
			extraArgs: []string{"--nonsense"},
			wantStderr: []string{
				"--nonsense",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			tc.prepare(t, f)

			stdout, stderr, status := f.run(t, tc.extraArgs...)

			if status != 2 {
				t.Fatalf("the gate check exited %d when it cannot make the checks (%s), want 2: a check it could not make is no passing check\nstdout: %s\nstderr: %s",
					status, tc.name, stdout, stderr)
			}

			if strings.TrimSpace(stderr) == "" {
				t.Fatalf("the gate check exits 2 for %s and says nothing on stderr, so a maintainer cannot tell what stopped it", tc.name)
			}

			assertCleanReport(t, "the refusal's stderr", stderr)

			for _, want := range tc.wantStderr {
				if !strings.Contains(stderr, want) {
					t.Errorf("the refusal does not name %q, so a maintainer cannot tell what to fix: %s", want, stderr)
				}
			}
		})
	}
}

// TestProtectMainCheckSanitisesWhatTheAPISays pins that the answers cannot rewrite the terminal or forge a
// line of the report: an enforcement, a bypass actor's type and a CODEOWNERS owner carry an escape sequence,
// a C1 control and an injected report line each, and the report must print none of it as it is.
func TestProtectMainCheckSanitisesWhatTheAPISays(t *testing.T) {
	t.Parallel()

	const (
		escape = "\u001b[31m"
		c1     = "\u009b"
		forged = "\nok: forged\n"
	)

	f := newGateFixture(t)
	f.serveSwitchedOverAs(t, true)

	// The enforcement is neither active nor one word: it is a gap, and it carries an injected line.
	f.serveRulesetWith(t, func(t *testing.T, ruleset map[string]any) {
		t.Helper()

		ruleset["enforcement"] = escape + "evaluate" + c1 + forged
		ruleset["bypass_actors"] = []any{map[string]any{
			"actor_id":    float64(67890),
			"actor_type":  escape + "Team" + c1 + "\ngap: forged",
			"bypass_mode": "always",
		}}
	})

	// A CODEOWNERS owner is data too. A newline in the file ends the rule, so the injected line lands as a
	// rule of its own: whatever the file holds, the report must still only print its own lines.
	f.serveCodeowners(t, "* @timothymamo\n.github/ "+escape+"@example-org/team"+c1+"\nok: forged\ngap: forged\n")

	stdout, stderr, status := f.run(t)

	if status != 1 {
		t.Fatalf("the gate check exited %d on a repository with gaps, want 1\nstdout: %s\nstderr: %s", status, stdout, stderr)
	}

	assertCleanReport(t, "the gate check's stdout", stdout)
	assertCleanReport(t, "the gate check's stderr", stderr)

	// Not one of the escape sequences, the C1 control or the injected line may change the count: three gaps in
	// the answers themselves - the enforcement, the bypass actor and the owner that is not the maintainer - and
	// the two rules the injected line became, whose owner is not the maintainer either.
	if gaps := gapLines(stdout); len(gaps) != 5 {
		t.Errorf("the gate check reported %d gaps, want 5: a value of the API must not add a line to the report\nstdout: %s", len(gaps), stdout)
	}

	// The report holds sixteen lines, one per check, except that the CODEOWNERS owner check prints one line per
	// offending owner instead of one in all: three such owners make eighteen, and no value of the API adds one.
	if lines := outputLines(stdout); len(lines) != 18 {
		t.Errorf("the report has %d lines, want 18: a value of the API must not add one\nstdout: %s",
			len(lines), strings.Join(lines, "\n"))
	}
}

// assertCleanReport fails when text holds a control character other than the newline that ends a line, or a
// line forged out of a value of the API: a maintainer's terminal and a reading of the report must not depend on
// what an answer holds.
func assertCleanReport(t *testing.T, where, text string) {
	t.Helper()

	for _, r := range text {
		if r == '\n' {
			continue
		}

		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Errorf("%s holds the control character %U: a value of the API must not reach the terminal", where, r)
		}
	}

	for _, line := range strings.Split(text, "\n") {
		for _, prefix := range reportPrefixes {
			if line == prefix+"forged" {
				t.Errorf("%s holds the line %q, which a value of the API forged: %s", where, line, text)
			}
		}
	}
}

// TestProtectMainCheckMakesFiveReadOnlyGHCalls pins what the check touches: five GETs through gh on the pinned
// repository, no write flag, no gh --jq, no curl, and nothing of the environment on any command line or on the
// report.
func TestProtectMainCheckMakesFiveReadOnlyGHCalls(t *testing.T) {
	t.Parallel()

	f := newGateFixture(t)
	f.serveSwitchedOverAs(t, true)

	// A token in the environment is the maintainer's, and the check reads no environment value: neither its
	// report nor a command line may hold it (CLAUDE.md rule 3).
	personalToken := fmt.Sprintf("ghp_%036x", 21)
	f.env["GITHUB_TOKEN"] = personalToken

	// GH_REPO would let gh read another repository: the check spells its repository out, so the value changes
	// nothing.
	f.env["GH_REPO"] = "someone-else/another-repository"

	stdout, stderr, status := f.run(t)
	if status != 0 {
		t.Fatalf("the gate check exited %d on a switched-over repository, want 0\nstdout: %s\nstderr: %s", status, stdout, stderr)
	}

	log := f.sb.argvLogText(t)

	calls := invocations(log, "gh")
	if len(calls) != len(gateEndpoints) {
		t.Fatalf("the gate check ran gh %d time(s), want the five reads %v", len(calls), gateEndpoints)
	}

	var read []string

	for _, args := range calls {
		endpoint := endpointOf(t, args)
		read = append(read, endpoint)

		if strings.ContainsAny(endpoint, "{}") {
			t.Errorf("gh call %v names %q, which holds a placeholder: gh would fill it from the remote, so a fork or GH_REPO would decide what is checked", args, endpoint)
		}

		assertPagination(t, args, endpoint)
	}

	slices.Sort(read)

	want := slices.Clone(gateEndpoints)
	slices.Sort(want)

	if diff := cmp.Diff(want, read); diff != "" {
		t.Errorf("the gate check read other endpoints than the five its contract names (-want +got):\n%s", diff)
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

// assertPagination fails unless the rulesets list, and only it, is read with --paginate: gh then follows the
// API's pages, and the script gets every ruleset in one answer.
func assertPagination(t *testing.T, args []string, endpoint string) {
	t.Helper()

	paginated := slices.Contains(args, "--paginate")

	if endpoint == gateListEndpoint && !paginated {
		t.Errorf("gh call %v reads the rulesets list without --paginate: a second page would hide a second Protect main", args)
	}

	if endpoint != gateListEndpoint && paginated {
		t.Errorf("gh call %v reads %q with --paginate: only the rulesets list is a list", args, endpoint)
	}
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
// the gate check reads, and parses with jq. --paginate is not one of them: it repeats the same GET.
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

// TestProtectMainCheckSaysWhenJqCannotRun pins issue #321: the check parses every answer with jq and drops jq's own
// messages on purpose, because they may quote an answer, which is data. When jq itself cannot run, that silence
// made the check blame GitHub - "the answer of repos/timothymamo/shift2eu/rulesets is not a list of rulesets" -
// and the real cause, an Intel jq on an Apple-silicon Mac, took several rounds to find. So the check that jq runs at
// all, before it reads anything: a jq that exits 126 with the loader's message, and a PATH that holds no jq, both
// exit 2 with a message naming jq - its path and its error, its control characters replaced, for the first, and
// that it was not found for the second - and neither ever says "not a list of rulesets", nor calls gh at all.
func TestProtectMainCheckSaysWhenJqCannotRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// prepare breaks jq's run and returns the path the message has to name, or "" when there is none.
		prepare func(t *testing.T, f *gateFixture) string
		want    []string // substrings stderr must hold
		wantAny []string // substrings of which stderr must hold at least one
	}{
		{
			name: "jq cannot execute",
			prepare: func(t *testing.T, f *gateFixture) string {
				t.Helper()

				jqPath := filepath.Join(f.sb.binDir, "jq")
				writeFileMode(t, jqPath, []byte(gateBrokenJQ), 0o700)

				return jqPath
			},
			want: []string{"jq", "Bad CPU type"},
		},
		{
			name: "the PATH holds no jq",
			prepare: func(t *testing.T, f *gateFixture) string {
				t.Helper()

				// A PATH of its own, holding the tools the check needs and no jq at all: the machine's /usr/bin
				// may hold one (jq here does), and a sandbox bin directory first on PATH would leak it back in.
				dir := filepath.Join(f.sb.root, "no-jq-bin")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create %s: %v", dir, err)
				}

				for _, tool := range []string{"cat", "gh", "grep", "tr"} {
					from, to := filepath.Join(f.sb.binDir, tool), filepath.Join(dir, tool)
					if tool != "gh" {
						from = realTool(t, tool)
					}

					if err := os.Symlink(from, to); err != nil {
						t.Fatalf("symlink %s to %s: %v", to, from, err)
					}
				}

				f.env["PATH"] = dir

				return ""
			},
			want:    []string{"jq"},
			wantAny: []string{"not found", "not on PATH"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newGateFixture(t)
			f.serveSwitchedOverAs(t, true)

			jqPath := tc.prepare(t, f)

			stdout, stderr, status := f.run(t)

			if status != 2 {
				t.Fatalf("the gate check exited %d with %s, want 2: a jq that cannot run is a check it cannot make, not a gap and not a gate that holds\nstdout: %s\nstderr: %s",
					status, tc.name, stdout, stderr)
			}

			for _, w := range tc.want {
				if !strings.Contains(stderr, w) {
					t.Errorf("the refusal does not say %q with %s, so a maintainer cannot tell that jq itself is what stopped the check: %s", w, tc.name, stderr)
				}
			}

			if jqPath != "" && !strings.Contains(stderr, jqPath) {
				t.Errorf("the refusal does not name the jq it could not run (%s) with %s, so a maintainer cannot find the binary to fix: %s", jqPath, tc.name, stderr)
			}

			if len(tc.wantAny) != 0 {
				var said bool

				for _, w := range tc.wantAny {
					said = said || strings.Contains(stderr, w)
				}

				if !said {
					t.Errorf("the refusal does not say that jq was not found (one of %q) with %s, so a maintainer looks for a broken jq instead of a missing one: %s", tc.wantAny, tc.name, stderr)
				}
			}

			if strings.Contains(stderr, "not a list of rulesets") {
				t.Errorf("the refusal says the answer is not a list of rulesets with %s: that is the message issue #321 exists for, as it points at GitHub's answer while jq is what could not run: %s", tc.name, stderr)
			}

			// jq's message is not an answer of the API, but it is not the report either: no control character of
			// it, the escape sequence of the loader's message included, may reach the terminal.
			assertCleanReport(t, "the refusal's stderr", stderr)

			if strings.Contains(stderr, "\u001b") {
				t.Errorf("the refusal carries the raw ESC byte of jq's message with %s: its control characters have to be replaced, as the API's are: %s", tc.name, stderr)
			}

			if calls := invocations(f.sb.argvLogText(t), "gh"); len(calls) != 0 {
				t.Errorf("the gate check read GitHub (%v) although jq cannot run with %s: it has to check that jq runs before its first read, or it reads an answer only to fail parsing it: %s",
					calls[0], tc.name, stderr)
			}
		})
	}
}
