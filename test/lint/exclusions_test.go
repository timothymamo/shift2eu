package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/yamlread"
)

// forbidigoTags are the tags of forbidigo's groups whose exclusion rules
// TestForbidigoExclusionsDropOneGroup pins entry by entry, each of which starts
// the msg of every entry in its group (.golangci.yml,
// linters.settings.forbidigo). The tags of the two composition-root groups,
// compositionRootTags, are pinned by what their rules select instead of by the
// text of a path regexp, in TestCompositionRootExclusions.
var forbidigoTags = []string{"cloud-read-only", "domain-is-pure", testutilNoEnvTag, cliNoEnvOrProcessTag, inventoryFileNoEnvTag, e2eNoEnvTag, testsNoProcessStateTag, aiReviewerEnvTag, aiReviewerOneProcessTag, siteBuilderEnvTag, claudeCodePinEnvTag, auditExecEnvTag}

// auditExecEnvTag is the tag of the audit command wrapper's environment group
// (issue #277, ADR 0040's amendment), and auditExecExecMsg the msg of its one
// entry with an exception: the syscall.Exec ci/auditexec/main.go passes to run.
const (
	auditExecEnvTag  = "audit-exec-env"
	auditExecExecMsg = "audit-exec-env: syscall.Exec is used in ci/auditexec/main.go only, passed to run (ADR 0040); a test passes a fake exec of its own"
)

// siteBuilderEnvTag is the tag of the site builder's environment group (PRD
// #297 milestone 2).
const siteBuilderEnvTag = "site-builder-env"

// claudeCodePinEnvTag is the tag of the Claude Code pin check's environment
// group (issue #276).
const claudeCodePinEnvTag = "claude-code-pin-env"

// aiReviewerEnvTag is the tag of the AI reviewer's environment group (ADR 0033),
// and aiReviewerLookupEnvMsg the msg of its one entry with an exception: the
// os.LookupEnv that ci/aireview/main.go passes to review.SettingsFromEnv.
const (
	aiReviewerEnvTag       = "ai-reviewer-env"
	aiReviewerLookupEnvMsg = "ai-reviewer-env: os.LookupEnv is used in ci/aireview/main.go only, passed to review.SettingsFromEnv (ADR 0033); a test passes a lookup function of its own"
)

// exclusionRule is one rule of linters.exclusions.rules in .golangci.yml.
type exclusionRule struct {
	line    int               // of its first key
	fields  map[string]string // each scalar key, unquoted
	linters []string          // nil when the rule names no linters
}

// TestForbidigoExclusionsDropOneGroup checks every rule of
// linters.exclusions.rules that drops forbidigo issues, read from
// .golangci.yml, offline and without golangci-lint. Such a rule names
// forbidigo alone, and its text is a text of one group, so it matches the quoted
// msg at the end of an issue and cannot drop another group's issue on a call
// that passes the tag as a string: either the group's whole form,
// 'because "<tag>: [^"]*"$', or the narrower form that instantiates it with the
// msg of one entry, which drops that entry's issues only. The rules of every
// group pinned here are pinned exactly, so a new group states its own: the
// cloud-read-only group has none, and the domain-is-pure group exactly two,
// one for every path outside internal/domain/ and one for every _test.go file.
// The composition-root groups are pinned by what their rules select:
// TestCompositionRootExclusions. A rule that names no linters would drop
// forbidigo issues too, so every rule names its linters.
func TestForbidigoExclusionsDropOneGroup(t *testing.T) {
	t.Parallel()

	got := map[string][]map[string]string{}
	for _, tag := range forbidigoTags {
		got[tag] = nil
	}
	root := moduleRoot(t)
	for _, r := range exclusionRules(t, root) {
		if r.linters == nil {
			t.Errorf(".golangci.yml:%d: an exclusion rule with no linters drops every linter's issues, forbidigo's included; name its linters", r.line)
			continue
		}
		if !slices.Contains(r.linters, "forbidigo") {
			continue
		}
		if len(r.linters) != 1 {
			t.Errorf(".golangci.yml:%d: an exclusion rule for forbidigo names linters %q; give forbidigo a rule of its own", r.line, r.linters)
			continue
		}
		text := r.fields["text"]
		tag, ok := groupOfText(t, root, text)
		if !ok {
			t.Errorf(".golangci.yml:%d: forbidigo exclusion text %q; want a text of a group of %q, either 'because \"<tag>: [^\"]*\"$' or that text with one entry's msg in place of [^\"]*",
				r.line, text, slices.Concat(forbidigoTags, compositionRootTags))
			continue
		}
		if slices.Contains(compositionRootTags, tag) {
			continue // TestCompositionRootExclusions pins its scope and its one exception
		}
		got[tag] = append(got[tag], r.fields)
	}

	domainIsPure := `because "domain-is-pure: [^"]*"$`
	want := map[string][]map[string]string{
		"cloud-read-only": nil,
		"domain-is-pure": {
			{"path-except": `^internal/domain/`, "text": domainIsPure},
			{"path": `_test\.go$`, "text": domainIsPure},
		},
		// The helpers every test shares (issue #144): one rule, for every path
		// outside internal/testutil/, so the group holds where the helpers are
		// and nowhere else.
		testutilNoEnvTag: {
			{"path-except": `^internal/testutil/`, "text": groupText(testutilNoEnvTag)},
		},
		// The CLI's production code (issue #152): one rule, for every path
		// outside internal/cli/, so the group holds where the CLI is and
		// nowhere else. The roots' own groups take over under cmd/.
		cliNoEnvOrProcessTag: {
			{"path-except": `^internal/cli/`, "text": groupText(cliNoEnvOrProcessTag)},
		},
		// The inventory-file reader (issue #152): one rule, for every path
		// outside internal/adapter/source/inventoryfile/, so the group holds
		// where the reader is and nowhere else. It holds the reader's
		// _test.go files too, as cli-no-env-or-process holds the CLI's.
		inventoryFileNoEnvTag: {
			{"path-except": `^internal/adapter/source/inventoryfile/`, "text": groupText(inventoryFileNoEnvTag)},
		},
		// The e2e tier (issue #167): one rule, for every path outside
		// test/e2e/, so the group holds there and nowhere else, _test.go
		// files included.
		e2eNoEnvTag: {
			{"path-except": `^test/e2e/`, "text": groupText(e2eNoEnvTag)},
		},
		// Every test that belongs to this project (issue #153): one rule, for
		// the two test binaries that deliberately set the go command's own
		// variables (GOCACHE, GOFLAGS, HOME) to run it in a known environment.
		// The group holds for every other path, the adapters' and the domain's
		// tests and the e2e ones included, so a future source adapter test that
		// sets AWS_PROFILE is refused rather than left to review.
		//
		// The AI reviewer's CLI asker (PRD #161 milestone 4, ADR 0033) states one
		// exception: the one test file that fills the process's environment with
		// an ANTHROPIC_API_KEY and a variable of its own, to prove that the
		// Claude Code CLI inherits neither. That test runs no parallel test
		// beside it, and the exception is that file alone: every other file of
		// the library, its own tests included, changes no process state.
		testsNoProcessStateTag: {
			{"path": `^test/(?:lint|architecture)/`, "text": groupText(testsNoProcessStateTag)},
			{"path": `^ci/aireview/internal/review/claudecli_test\.go$`, "text": groupText(testsNoProcessStateTag)},
		},
		// The AI reviewer (ADR 0033) and the Claude Code launcher it shares
		// with the scheduled audit (issue #277): one rule for every path outside
		// ci/aireview/ and ci/internal/claudecode/, so the group holds there and
		// nowhere else, _test.go files included, and the one exception, the
		// os.LookupEnv entry's msg alone, in ci/aireview/main.go alone.
		aiReviewerEnvTag: {
			{"path-except": `^ci/(?:aireview|internal/claudecode)/`, "text": groupText(aiReviewerEnvTag)},
			{"path": `^ci/aireview/main\.go$`, "text": narrowedTextOf(aiReviewerLookupEnvMsg)},
		},
		// The AI reviewer's library (PRD #161 milestone 4, ADR 0033) and the
		// shared launcher it asks through (issue #277): the launcher runs the
		// one Claude Code CLI call with exec.CommandContext, writes the system
		// prompt to a temporary file and removes the directories it made, and
		// neither reads a file. Two rules scope the group to the two libraries'
		// non-test files: one drops it in their _test.go files, which read and
		// run what they test the way a test does, and one outside the two.
		aiReviewerOneProcessTag: {
			{"path": `^ci/(?:aireview/internal/review|internal/claudecode)/.*_test\.go$`, "text": groupText(aiReviewerOneProcessTag)},
			{"path-except": `^ci/(?:aireview/internal/review|internal/claudecode)/`, "text": groupText(aiReviewerOneProcessTag)},
		},
		// The site's builder (PRD #297 milestone 2): one rule for every path
		// outside ci/sitebuild/, so the group holds there and nowhere else,
		// _test.go files included.
		siteBuilderEnvTag: {
			{"path-except": `^ci/sitebuild/`, "text": groupText(siteBuilderEnvTag)},
		},
		// The Claude Code pin check (issue #276): one rule for every path
		// outside ci/claudecodepin/, so the group holds there and nowhere else,
		// _test.go files included.
		claudeCodePinEnvTag: {
			{"path-except": `^ci/claudecodepin/`, "text": groupText(claudeCodePinEnvTag)},
		},
		// The audit's command wrapper (issue #277, ADR 0040's amendment): one
		// rule for every path outside ci/auditexec/, so the group holds there
		// and nowhere else, _test.go files included, and the one exception, the
		// syscall.Exec entry's msg alone, in ci/auditexec/main.go alone.
		auditExecEnvTag: {
			{"path-except": `^ci/auditexec/`, "text": groupText(auditExecEnvTag)},
			{"path": `^ci/auditexec/main\.go$`, "text": narrowedTextOf(auditExecExecMsg)},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the forbidigo exclusion rules of .golangci.yml, by group (-want +got):\n%s", diff)
	}
}

// groupOfText returns the tag whose exclusion text text is, in either form the
// group's rules may have: groupText(tag), the group's whole form, or narrowedText
// with the msg of one entry of the group (narrowedTextOf), the single exception a
// group may state for one function in one file (TestCompositionRootExclusions).
//
// A narrower text is no group's unless it drops exactly one entry's issue of the
// group: a body such as .* drops every entry's, and would be classified as an
// exception while it allows the whole group.
func groupOfText(t *testing.T, root, text string) (string, bool) {
	t.Helper()

	tags := slices.Concat(forbidigoTags, compositionRootTags)
	for _, tag := range tags {
		if text == groupText(tag) {
			return tag, true
		}
	}
	m := narrowedText.FindStringSubmatch(text)
	if m == nil || !slices.Contains(tags, m[1]) {
		return "", false
	}
	narrowed, err := regexp.Compile(text)
	if err != nil {
		return "", false
	}
	matched := 0
	for _, e := range forbidigoEntries(t, root) {
		if !strings.HasPrefix(e.msg, m[1]+": ") {
			continue
		}
		if narrowed.MatchString(issueText("name", e.msg)) {
			matched++
		}
	}
	return m[1], matched == 1
}

// narrowedTextOf returns the exclusion text that drops the issue of the entry
// with msg alone: the group's text with msg, regex-escaped, in place of [^"]*.
func narrowedTextOf(msg string) string {
	return `because "` + regexp.QuoteMeta(msg) + `"$`
}

// groupText returns the exclusion text that drops every issue of the group with
// tag: the documented form, which matches the quoted msg at the end of an issue.
func groupText(tag string) string {
	return `because "` + tag + `: [^"]*"$`
}

// narrowedText matches an exclusion text that names one entry's msg in place of a
// group's whole text, and captures the group's tag.
var narrowedText = regexp.MustCompile(`^because "([^":]+): [^"]*"\$$`)

// exclusionRules reads linters.exclusions.rules from .golangci.yml under root
// through parseExclusionRules, and fails the test when the file holds no rule.
func exclusionRules(t *testing.T, root string) []exclusionRule {
	t.Helper()

	config, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := parseExclusionRules(config)
	if err != nil {
		t.Fatalf("read .golangci.yml: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal(".golangci.yml holds no exclusion rule")
	}
	return rules
}

// parseExclusionRules reads linters.exclusions.rules from doc, a whole
// .golangci.yml, with the shared line reader of test/internal/yamlread, which
// refuses what it does not follow rather than guess at it: every rule is a
// block mapping of plain keys, each key's value one scalar on its line but
// linters, a list of linter names in flow ([a, b]) or block (- a) form. A rule
// written another way is an error, so it is never silently skipped.
func parseExclusionRules(doc []byte) ([]exclusionRule, error) {
	lines, err := yamlread.SplitLines(doc)
	if err != nil {
		return nil, err
	}
	block, err := yamlread.Walk(lines, "linters", "exclusions", "rules")
	if err != nil {
		return nil, err
	}
	items, err := yamlread.Sequence(block)
	if err != nil {
		return nil, fmt.Errorf("linters: exclusions: rules: %w", err)
	}

	rules := make([]exclusionRule, 0, len(items))
	for _, it := range items {
		r, err := exclusionRuleOf(it)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// exclusionRuleOf reads one entry of linters.exclusions.rules.
func exclusionRuleOf(it yamlread.Item) (exclusionRule, error) {
	entries, err := yamlread.Mapping(it.Lines)
	if err != nil {
		return exclusionRule{}, fmt.Errorf("line %d: an exclusion rule test/lint cannot read: %w", it.Num, err)
	}
	if len(entries) == 0 {
		return exclusionRule{}, fmt.Errorf("line %d: an empty exclusion rule", it.Num)
	}

	r := exclusionRule{line: it.Num, fields: map[string]string{}}
	for _, e := range entries {
		if e.Key == "linters" {
			linters, err := yamlread.List(e)
			if err != nil {
				return exclusionRule{}, fmt.Errorf("line %d: exclusion rule linters: %w", e.Num, err)
			}
			// An empty list names no linter, so it drops every linter's
			// issues, as a rule with no linters key does.
			if len(linters) > 0 {
				r.linters = linters
			}
			continue
		}
		value, err := yamlread.Single(e)
		if err != nil {
			return exclusionRule{}, fmt.Errorf("line %d: an exclusion rule key test/lint cannot read: %w", e.Num, err)
		}
		r.fields[e.Key] = value
	}
	return r, nil
}

// exclusionDoc returns a .golangci.yml whose linters.exclusions.rules hold the
// lines rules, each already indented under the rules key: the keys
// parseExclusionRules walks, so a case of TestParseExclusionRules below holds
// nothing but the rules it is about. Its first rule starts on line 4.
func exclusionDoc(rules string) string {
	return "linters:\n  exclusions:\n    rules:\n" + rules
}

// exclusionDocTrailingKeys closes the rules block with a top-level key of its
// own. A case whose one rule is a key with no value at all needs it: a rules
// key with no entry below it is a document no caller accepts, so without a key
// after the rules there would be nothing to read the empty entry from.
const exclusionDocTrailingKeys = "\nissues:\n  max-issues-per-linter: 0\n"

// TestParseExclusionRules pins parseExclusionRules, the seam that reads
// linters.exclusions.rules from .golangci.yml: exclusionRules reads the file
// and hands the document to it, so a rule no test reads from disk is still
// read here.
//
// golangci-lint matches a rule that names no linter against every linter, so an
// empty linters list must never be read as "not a forbidigo rule". It is read
// as a rule that names no linter (nil linters), not as an error: the reader
// says what the document says, and TestForbidigoExclusionsDropOneGroup is what
// rejects such a rule, with a message that says why.
//
// Every other way of writing a rule that this reader does not follow is an
// error rather than a guess, so a rule is never silently skipped: a repeated
// key, an upper-case key, a block scalar, a double-quoted text with an escape,
// an anchor or an alias, a single-quoted text that doubles a quote to escape
// it (which YAML would unescape, and this reader refuses to decode), and a
// closing bracket inside a comment, which a comment swallows.
func TestParseExclusionRules(t *testing.T) {
	t.Parallel()

	// ruleLine is the line of the one rule of a document exclusionDoc builds.
	const ruleLine = 4

	tests := []struct {
		name    string
		config  string
		want    []exclusionRule
		wantErr bool
	}{
		{
			name:   "a linters list in block form names its one linter",
			config: exclusionDoc("      - linters:\n          - forbidigo\n"),
			want: []exclusionRule{{
				line:    ruleLine,
				fields:  map[string]string{},
				linters: []string{"forbidigo"},
			}},
		},
		{
			name:   "a linters list in flow form names every linter in it, in order",
			config: exclusionDoc("      - linters: [forbidigo, gosec]\n"),
			want: []exclusionRule{{
				line:    ruleLine,
				fields:  map[string]string{},
				linters: []string{"forbidigo", "gosec"},
			}},
		},
		{
			name: "a rule reads every key and the linters together",
			config: exclusionDoc(`      - linters: [forbidigo]
        path: _test\.go$
        text: 'because "domain-is-pure: [^"]*"$'
`),
			want: []exclusionRule{{
				line: ruleLine,
				fields: map[string]string{
					"path": `_test\.go$`,
					"text": `because "domain-is-pure: [^"]*"$`,
				},
				linters: []string{"forbidigo"},
			}},
		},

		// A rule that names no linter matches every linter's issues, forbidigo's
		// included: the reader reports it as naming none, never as a rule that
		// names one or as no rule at all.
		{
			name:   "an empty linters flow list names no linter",
			config: exclusionDoc("      - linters: []\n"),
			want: []exclusionRule{{
				line:   ruleLine,
				fields: map[string]string{},
			}},
		},
		{
			name:   "an empty linters key names no linter",
			config: exclusionDoc("      - linters:\n") + exclusionDocTrailingKeys,
			want: []exclusionRule{{
				line:   ruleLine,
				fields: map[string]string{},
			}},
		},
		{
			name:   "a rule with no linters key names no linter",
			config: exclusionDoc("      - path: _test\\.go$\n"),
			want: []exclusionRule{{
				line:   ruleLine,
				fields: map[string]string{"path": `_test\.go$`},
			}},
		},

		// Every other way of writing a rule is refused, not guessed at.
		{
			name:    "a repeated key is an error",
			config:  exclusionDoc("      - linters: [forbidigo]\n        text: 'a'\n        text: 'b'\n"),
			wantErr: true,
		},
		{
			name:    "an upper-case key is an error",
			config:  exclusionDoc("      - linters: [forbidigo]\n        Text: 'a'\n"),
			wantErr: true,
		},
		{
			name:    "a literal block scalar text is an error",
			config:  exclusionDoc("      - linters: [forbidigo]\n        text: |\n          because a tag: x$\n"),
			wantErr: true,
		},
		{
			name: "a double-quoted text with an escape is an error",
			config: exclusionDoc(`      - linters: [forbidigo]
        text: "because \"x\"$"
`),
			wantErr: true,
		},
		{
			name:    "an anchored text is an error",
			config:  exclusionDoc("      - linters: [forbidigo]\n        text: &t 'a'\n"),
			wantErr: true,
		},
		{
			name:    "an aliased text is an error",
			config:  exclusionDoc("      - linters: [forbidigo]\n        text: *t\n"),
			wantErr: true,
		},
		{
			// YAML reads 'it''s' as it's; this reader refuses to decode the
			// escape rather than keep the quotes it does not decode.
			name:    "a single-quoted text with '' escaping is an error",
			config:  exclusionDoc("      - linters: [forbidigo]\n        text: 'it''s'\n"),
			wantErr: true,
		},
		{
			// A comment runs to the end of the line, so the bracket in it does
			// not close the list.
			name:    "a bracket in a comment inside a linters flow list is an error",
			config:  exclusionDoc("      - linters: [forbidigo # ]\n"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseExclusionRules([]byte(tc.config))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseExclusionRules() = %v, nil; want an error", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseExclusionRules(): %v", err)
			}

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(exclusionRule{})); diff != "" {
				t.Errorf("parseExclusionRules() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
