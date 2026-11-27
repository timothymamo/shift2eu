package publish_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// docTool, docCheck, docChanges, docFinding and docResult mirror result.json:
// the field names and value shapes ADR 0040's schema table names. The tests
// build every document from them, so a field this schema has and the core's own
// tags miss is a document the tests hand ParseResult.
type docTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type docCheck struct {
	ID     string `json:"id"`
	Ran    bool   `json:"ran"`
	NotRun string `json:"not_run"`
}

type docChanges struct {
	Dependencies []string `json:"dependencies"`
	Narrative    string   `json:"narrative"`
}

type docFinding struct {
	Check     string `json:"check"`
	Rule      string `json:"rule"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Sink      string `json:"sink"`
	Severity  string `json:"severity"`
	Confirmed bool   `json:"confirmed"`
	Category  string `json:"category"`
	Private   bool   `json:"private"`
	Scenario  string `json:"scenario"`
	Evidence  string `json:"evidence"`
	Fix       string `json:"fix"`
	Dismissal string `json:"dismissal"`
}

type docResult struct {
	Schema      int          `json:"schema"`
	Commit      string       `json:"commit"`
	ReleaseTag  string       `json:"release_tag"`
	ImageDigest string       `json:"image_digest"`
	Tools       []docTool    `json:"tools"`
	Checks      []docCheck   `json:"checks"`
	Findings    []docFinding `json:"findings"`
	Changes     docChanges   `json:"changes"`
	Limits      []string     `json:"limits"`
}

// docRenderedChecks returns one check per check id, in the table's order, each
// one ran.
func docRenderedChecks() []docCheck {
	checks := make([]docCheck, 0, len(checkIDs))
	for _, id := range checkIDs {
		checks = append(checks, docCheck{ID: id, Ran: true})
	}

	return checks
}

// minimalSpec is the smallest result the schema accepts: every field present,
// no tool, no finding, no dependency and no limit.
func minimalSpec() docResult {
	return docResult{
		Schema:      1,
		Commit:      testCommit,
		ReleaseTag:  testTag,
		ImageDigest: testDigest,
		Tools:       []docTool{},
		Checks:      docRenderedChecks(),
		Findings:    []docFinding{},
		Changes:     docChanges{Dependencies: []string{}},
		Limits:      []string{},
	}
}

// oneFinding is a valid public finding of the tree review.
func oneFinding() docFinding {
	return docFinding{
		Check:     "tree-review",
		Rule:      "G304",
		File:      "internal/cli/run.go",
		Line:      12,
		Sink:      "os.Open",
		Severity:  sevLow,
		Confirmed: true,
		Category:  catHardening,
		Scenario:  "a scenario",
		Evidence:  "a trace",
		Fix:       "a fix",
	}
}

// withFinding returns the document with exactly these findings.
func (s docResult) withFinding(findings ...docFinding) docResult {
	s.Findings = findings

	return s
}

// with is s after f changed it.
func (s docResult) with(f func(*docResult)) docResult {
	f(&s)

	return s
}

// json returns the document's text, compact, with every absent list an empty
// one: the schema refuses a null.
func (s docResult) json(t *testing.T) []byte {
	t.Helper()

	if s.Tools == nil {
		s.Tools = []docTool{}
	}

	if s.Checks == nil {
		s.Checks = []docCheck{}
	}

	if s.Findings == nil {
		s.Findings = []docFinding{}
	}

	if s.Changes.Dependencies == nil {
		s.Changes.Dependencies = []string{}
	}

	if s.Limits == nil {
		s.Limits = []string{}
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshalling the fixture document: %v", err)
	}

	return data
}

// want returns the Result the document must parse to.
func (s docResult) want() publish.Result {
	result := publish.Result{
		Schema:      s.Schema,
		Commit:      s.Commit,
		ReleaseTag:  s.ReleaseTag,
		ImageDigest: s.ImageDigest,
		Changes:     publish.Changes{Dependencies: append([]string{}, s.Changes.Dependencies...), Narrative: s.Changes.Narrative},
		Limits:      append([]string{}, s.Limits...),
	}

	for _, tool := range s.Tools {
		result.Tools = append(result.Tools, publish.Tool{Name: tool.Name, Version: tool.Version})
	}

	for _, check := range s.Checks {
		result.Checks = append(result.Checks, publish.Check{ID: check.ID, Ran: check.Ran, NotRun: check.NotRun})
	}

	for _, f := range s.Findings {
		result.Findings = append(result.Findings, publish.Finding{
			Check:     f.Check,
			Rule:      f.Rule,
			File:      f.File,
			Line:      f.Line,
			Sink:      f.Sink,
			Severity:  f.Severity,
			Confirmed: f.Confirmed,
			Category:  f.Category,
			Private:   f.Private,
			Scenario:  f.Scenario,
			Evidence:  f.Evidence,
			Fix:       f.Fix,
			Dismissal: f.Dismissal,
		})
	}

	return result
}

// plain returns the document's text.
func plain(s docResult) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		return s.json(t)
	}
}

// edited returns the document's text with one substring replaced, and fails the
// test when the substring is not there exactly once.
func edited(s docResult, old, replaced string) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		doc := string(s.json(t))
		if n := strings.Count(doc, old); n != 1 {
			t.Fatalf("the fixture holds %q %d times, want exactly one: %s", old, n, doc)
		}

		return []byte(strings.Replace(doc, old, replaced, 1))
	}
}

// followed returns the document's text with suffix after it.
func followed(s docResult, suffix string) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		return []byte(string(s.json(t)) + suffix)
	}
}

// resultCase is one result.json ParseResult reads, and what it must do with it.
type resultCase struct {
	// name says what the document breaks.
	name string
	doc  func(*testing.T) []byte
	// commit is the commit argument when it is not the fixture's.
	commit string
	// field is a field path the error must name, when the ADR fixes one.
	field string
	// value is a value of the document the error must not echo.
	value string
}

// TestParseResultRefuses pins every refusal rule of ADR 0040's schema section,
// one row each: the size, trailing data, unknown and duplicate fields, missing
// fields, null, invalid UTF-8, every control, bidi and zero-width class, LF and
// TAB outside the multi-line fields, every cap and shape, the commit mismatch,
// a finding on a check that did not run, and one finding over the limit. Each
// refusal wraps ErrResultInvalid, and no error echoes the value that broke it.
func TestParseResultRefuses(t *testing.T) {
	t.Parallel()

	cases := []resultCase{
		// Size and trailing data.
		{
			name: "one byte over MaxResultBytes",
			doc: func(t *testing.T) []byte {
				t.Helper()

				doc := minimalSpec().json(t)
				pad := publish.MaxResultBytes + 1 - len(doc)
				if pad <= 0 {
					t.Fatalf("MaxResultBytes is %d, smaller than the fixture's %d bytes, so this row cannot pin the limit", publish.MaxResultBytes, len(doc))
				}

				return []byte(strings.Repeat(" ", pad) + string(doc))
			},
		},
		{
			name: "an object after the result",
			doc:  followed(minimalSpec(), " {}"),
		},
		{
			name: "a byte after the result",
			doc:  followed(minimalSpec(), "x"),
		},
		{
			name: "a second array after the result",
			doc:  followed(minimalSpec(), `[{"schema":1}]`),
		},

		// Unknown and duplicate fields.
		{
			name: "an unknown top-level field",
			doc:  edited(minimalSpec(), `"schema":1`, `"schema":1,"summary":"x"`),
		},
		{
			name:  "an unknown field of a finding",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"line":12`, `"line":12,"notes":"x"`),
			field: "findings[0]",
		},
		{
			name: "an unknown field of a check",
			doc:  edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`, `{"id":"dependencies","ran":true,"not_run":"","tool":"go"}`),
		},
		{
			name: "an unknown field of the changes",
			doc:  edited(minimalSpec(), `"narrative":""`, `"narrative":"","summary":"x"`),
		},
		{
			name: "a duplicate top-level key",
			doc:  edited(minimalSpec(), `"schema":1`, `"schema":1,"schema":1`),
		},
		{
			name:  "a duplicate severity, hiding behind the first",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"severity":"low"`, `"severity":"low","severity":"medium"`),
			field: "findings[0]",
		},
		{
			name: "a duplicate key in the changes",
			doc:  edited(minimalSpec(), `"narrative":""`, `"narrative":"","narrative":"x"`),
		},

		// Missing fields and null.
		{
			name: "a missing top-level field",
			doc:  edited(minimalSpec(), `,"limits":[]`, ``),
		},
		{
			name:  "a missing field of a finding",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `,"fix":"a fix"`, ``),
			field: "findings[0]",
		},
		{
			name: "a missing field of a check",
			doc:  edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`, `{"id":"dependencies","ran":true}`),
		},
		{
			name: "a null list",
			doc:  edited(minimalSpec(), `"limits":[]`, `"limits":null`),
		},
		{
			name: "a null string",
			doc:  edited(minimalSpec().withFinding(oneFinding()), `"severity":"low"`, `"severity":null`),
		},
		{
			name: "a null number",
			doc:  edited(minimalSpec(), `"schema":1`, `"schema":null`),
		},

		// Invalid UTF-8, in a field a finding carries.
		{
			name: "an invalid UTF-8 byte",
			doc: edited(minimalSpec().withFinding(oneFinding()),
				`"scenario":"a scenario"`, `"scenario":"`+"\xff"+`"`),
			field: "findings[0].scenario",
			value: "\xff",
		},
		{
			name: "a truncated UTF-8 sequence",
			doc: edited(minimalSpec().withFinding(oneFinding()),
				`"scenario":"a scenario"`, `"scenario":"`+"\xe2\x82"+`"`),
			field: "findings[0].scenario",
		},

		// Shapes.
		{
			name:  "a schema other than 1",
			doc:   edited(minimalSpec(), `"schema":1`, `"schema":2`),
			field: "schema",
		},
		{
			name:  "a schema of the wrong type",
			doc:   edited(minimalSpec(), `"schema":1`, `"schema":"1"`),
			field: "schema",
		},
		{
			name:  "a schema that is not an integer",
			doc:   edited(minimalSpec(), `"schema":1`, `"schema":1.5`),
			field: "schema",
		},
		{
			name:   "a commit that is not the argument",
			doc:    plain(minimalSpec()),
			commit: "fedcba9876543210fedcba9876543210fedcba98",
			field:  "commit",
		},
		{
			name:  "a commit of the wrong length",
			doc:   edited(minimalSpec(), `"commit":"`+testCommit+`"`, `"commit":"`+testCommit[:39]+`"`),
			field: "commit",
		},
		{
			name:  "an uppercase commit",
			doc:   edited(minimalSpec(), `"commit":"`+testCommit+`"`, `"commit":"`+strings.ToUpper(testCommit)+`"`),
			field: "commit",
		},
		{
			name:  "a non-hex commit",
			doc:   edited(minimalSpec(), `"commit":"`+testCommit+`"`, `"commit":"g`+testCommit[:39]+`"`),
			field: "commit",
		},
		{
			name:  "a release tag without the v prefix",
			doc:   edited(minimalSpec(), `"release_tag":"`+testTag+`"`, `"release_tag":"0.1.0"`),
			field: "release_tag",
		},
		{
			name:  "a release tag with a leading zero",
			doc:   edited(minimalSpec(), `"release_tag":"`+testTag+`"`, `"release_tag":"v01.0.0"`),
			field: "release_tag",
		},
		{
			name:  "a release tag with four parts",
			doc:   edited(minimalSpec(), `"release_tag":"`+testTag+`"`, `"release_tag":"v1.2.3.4"`),
			field: "release_tag",
		},
		{
			name:  "a release tag with a five-digit part",
			doc:   edited(minimalSpec(), `"release_tag":"`+testTag+`"`, `"release_tag":"v10000.0.0"`),
			field: "release_tag",
		},
		{
			name: "" +
				"an image digest one byte short",
			doc:   edited(minimalSpec(), `"image_digest":"`+testDigest+`"`, `"image_digest":"sha256:`+strings.Repeat("1", 63)+`"`),
			field: "image_digest",
		},
		{
			name:  "an uppercase image digest",
			doc:   edited(minimalSpec(), `"image_digest":"`+testDigest+`"`, `"image_digest":"sha256:`+strings.Repeat("A", 64)+`"`),
			field: "image_digest",
		},
		{
			name:  "an image digest with another algorithm",
			doc:   edited(minimalSpec(), `"image_digest":"`+testDigest+`"`, `"image_digest":"sha512:`+strings.Repeat("1", 64)+`"`),
			field: "image_digest",
		},
		{
			name:  "an unknown tool name",
			doc:   edited(minimalSpec(), `"tools":[]`, `"tools":[{"name":"npm","version":"1.0.0"}]`),
			field: "tools[0].name",
			value: "npm",
		},
		{
			name:  "one tool name twice",
			doc:   edited(minimalSpec(), `"tools":[]`, `"tools":[{"name":"go","version":"1.27.4"},{"name":"go","version":"1.27.5"}]`),
			field: "tools",
			value: "1.27.5",
		},
		{
			name:  "a tool version of five parts",
			doc:   edited(minimalSpec(), `"tools":[]`, `"tools":[{"name":"go","version":"1.2.3.4.5"}]`),
			field: "tools[0].version",
			value: "1.2.3.4.5",
		},
		{
			name:  "a tool version with a five-digit part",
			doc:   edited(minimalSpec(), `"tools":[]`, `"tools":[{"name":"go","version":"1.12345"}]`),
			field: "tools[0].version",
			value: "1.12345",
		},
		{
			name:  "a tool version with a prefix that is not v",
			doc:   edited(minimalSpec(), `"tools":[]`, `"tools":[{"name":"go","version":"go1.27"}]`),
			field: "tools[0].version",
			value: "go1.27",
		},
		{
			name:  "an empty tool version",
			doc:   edited(minimalSpec(), `"tools":[]`, `"tools":[{"name":"go","version":""}]`),
			field: "tools[0].version",
		},
		{
			name: "a check id that is not one of the fifteen",
			doc: edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`,
				`{"id":"dependency-review","ran":true,"not_run":""}`),
			field: "checks",
			value: "dependency-review",
		},
		{
			name: "a check id twice",
			doc: edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`,
				`{"id":"dependencies","ran":true,"not_run":""},{"id":"dependencies","ran":true,"not_run":""}`),
			field: "checks",
		},
		{
			name: "a missing check id",
			doc:  edited(minimalSpec(), `,{"id":"agent-surface","ran":true,"not_run":""}`, ``),
		},
		{
			name: "a check that ran with a reason",
			doc: edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`,
				`{"id":"dependencies","ran":true,"not_run":"timed-out"}`),
			field: "checks[0].not_run",
			value: "timed-out",
		},
		{
			name: "a check that did not run without a reason",
			doc: edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`,
				`{"id":"dependencies","ran":false,"not_run":""}`),
			field: "checks[0].not_run",
		},
		{
			name: "a check that did not run with an unknown reason",
			doc: edited(minimalSpec(), `{"id":"dependencies","ran":true,"not_run":""}`,
				`{"id":"dependencies","ran":false,"not_run":"too-slow"}`),
			field: "checks[0].not_run",
			value: "too-slow",
		},
		{
			name:  "a rule of 81 bytes",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"rule":"G304"`, `"rule":"G`+strings.Repeat("a", 80)+`"`),
			field: "findings[0].rule",
		},
		{
			name:  "a rule that starts with a dash",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"rule":"G304"`, `"rule":"-G304"`),
			field: "findings[0].rule",
		},
		{
			name:  "a rule with a space",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"rule":"G304"`, `"rule":"G 304"`),
			field: "findings[0].rule",
		},
		{
			name:  "a file of 201 bytes",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"file":"internal/cli/run.go"`, `"file":"`+strings.Repeat("f", 201)+`"`),
			field: "findings[0].file",
		},
		{
			name:  "an empty file",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"file":"internal/cli/run.go"`, `"file":""`),
			field: "findings[0].file",
		},
		{
			name:  "a file with a parent segment",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"file":"internal/cli/run.go"`, `"file":"internal/../run.go"`),
			field: "findings[0].file",
		},
		{
			name:  "a file with a current segment",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"file":"internal/cli/run.go"`, `"file":"internal/./run.go"`),
			field: "findings[0].file",
		},
		{
			name:  "a file with an empty segment",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"file":"internal/cli/run.go"`, `"file":"internal//run.go"`),
			field: "findings[0].file",
		},
		{
			name:  "a file with a byte the schema does not allow",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"file":"internal/cli/run.go"`, `"file":"internal/cli/run.go|x"`),
			field: "findings[0].file",
		},
		{
			name:  "a negative line",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"line":12`, `"line":-1`),
			field: "findings[0].line",
		},
		{
			name:  "a line past 9999999",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"line":12`, `"line":10000000`),
			field: "findings[0].line",
		},
		{
			name:  "a line that is not an integer",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"line":12`, `"line":12.5`),
			field: "findings[0].line",
		},
		{
			name:  "a sink of 121 bytes",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"sink":"os.Open"`, `"sink":"`+strings.Repeat("s", 121)+`"`),
			field: "findings[0].sink",
		},
		{
			name:  "a sink with a byte the schema does not allow",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"sink":"os.Open"`, `"sink":"os.Open;rm"`),
			field: "findings[0].sink",
		},
		{
			name:  "a severity of 17 bytes",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"severity":"low"`, `"severity":"`+strings.Repeat("s", 17)+`"`),
			field: "findings[0].severity",
		},
		{
			name:  "a category of 33 bytes",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"category":"hardening"`, `"category":"`+strings.Repeat("c", 33)+`"`),
			field: "findings[0].category",
		},
		{
			name:  "an empty scenario",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"scenario":"a scenario"`, `"scenario":""`),
			field: "findings[0].scenario",
		},
		{
			name: "a scenario of 2001 bytes",
			doc: edited(minimalSpec().withFinding(oneFinding()),
				`"scenario":"a scenario"`, `"scenario":"`+strings.Repeat("a", 2001)+`"`),
			field: "findings[0].scenario",
		},
		{
			name: "an evidence of 16001 bytes",
			doc: edited(minimalSpec().withFinding(oneFinding()),
				`"evidence":"a trace"`, `"evidence":"`+strings.Repeat("a", 16001)+`"`),
			field: "findings[0].evidence",
		},
		{
			name:  "an empty fix",
			doc:   edited(minimalSpec().withFinding(oneFinding()), `"fix":"a fix"`, `"fix":""`),
			field: "findings[0].fix",
		},
		{
			name: "a fix of 1001 bytes",
			doc: edited(minimalSpec().withFinding(oneFinding()),
				`"fix":"a fix"`, `"fix":"`+strings.Repeat("a", 1001)+`"`),
			field: "findings[0].fix",
		},
		{
			name: "a dismissal of 201 bytes",
			doc: edited(minimalSpec().withFinding(oneFinding()),
				`"dismissal":""`, `"dismissal":"`+strings.Repeat("d", 201)+`"`),
			field: "findings[0].dismissal",
		},
		{
			name: "a finding on a check that did not run",
			doc: func(t *testing.T) []byte {
				t.Helper()

				spec := minimalSpec().withFinding(oneFinding())
				spec.Checks = checksWithNotRunDoc("tree-review", "timed-out")

				return spec.json(t)
			},
			field: "findings[0].check",
		},
		{
			name: "a finding on a check the schema does not name",
			doc: func(t *testing.T) []byte {
				t.Helper()

				spec := minimalSpec().withFinding(oneFinding())
				spec.Findings[0].Check = "dependency-review"

				return spec.json(t)
			},
			field: "findings[0].check",
			value: "dependency-review",
		},
		{
			name:  "a dependency of 161 bytes",
			doc:   edited(minimalSpec(), `"dependencies":[]`, `"dependencies":["`+strings.Repeat("d", 161)+`"]`),
			field: "changes.dependencies",
		},
		{
			name:  "an empty dependency",
			doc:   edited(minimalSpec(), `"dependencies":[]`, `"dependencies":[""]`),
			field: "changes.dependencies[0]",
		},
		{
			name:  "a narrative of 2001 bytes",
			doc:   edited(minimalSpec(), `"narrative":""`, `"narrative":"`+strings.Repeat("n", 2001)+`"`),
			field: "changes.narrative",
		},
		{
			name:  "a limit of 301 bytes",
			doc:   edited(minimalSpec(), `"limits":[]`, `"limits":["`+strings.Repeat("l", 301)+`"]`),
			field: "limits[0]",
		},
		{
			name:  "an empty limit",
			doc:   edited(minimalSpec(), `"limits":[]`, `"limits":[""]`),
			field: "limits[0]",
		},
		{
			name: "more findings than the limit allows",
			doc: func(t *testing.T) []byte {
				t.Helper()

				findings := make([]docFinding, 0, publish.FindingLimit+1)
				for i := 0; i <= publish.FindingLimit; i++ {
					f := oneFinding()
					f.Scenario = "scenario " + strconv.Itoa(i)
					findings = append(findings, f)
				}

				return minimalSpec().withFinding(findings...).json(t)
			},
		},
		{
			name: "more limits than the schema allows",
			doc: func(t *testing.T) []byte {
				t.Helper()

				limits := make([]string, 0, 41)
				for i := 0; i < 41; i++ {
					limits = append(limits, "limit "+strconv.Itoa(i))
				}

				return minimalSpec().with(func(s *docResult) { s.Limits = limits }).json(t)
			},
		},
		{
			name: "more dependencies than the schema allows",
			doc: func(t *testing.T) []byte {
				t.Helper()

				deps := make([]string, 0, 51)
				for i := 0; i < 51; i++ {
					deps = append(deps, "example.com/dependency"+strconv.Itoa(i))
				}

				return minimalSpec().with(func(s *docResult) { s.Changes.Dependencies = deps }).json(t)
			},
		},
	}

	// Every control, bidi and zero-width class the schema refuses, one row each.
	for _, cp := range []struct {
		name string
		r    rune
	}{
		{"a NUL byte", 0x0000},
		{"a C0 control, start of heading", 0x0001},
		{"a C0 control, escape", 0x001b},
		{"a C0 control, unit separator", 0x001f},
		{"DEL", 0x007f},
		{"a C1 control, next line", 0x0085},
		{"a C1 control, application program command", 0x009f},
		{"the line separator", 0x2028},
		{"the paragraph separator", 0x2029},
		{"the Arabic letter mark", 0x061c},
		{"the left-to-right mark", 0x200e},
		{"the right-to-left mark", 0x200f},
		{"the left-to-right embedding", 0x202a},
		{"the right-to-left embedding", 0x202b},
		{"the pop directional formatting", 0x202c},
		{"the left-to-right override", 0x202d},
		{"the right-to-left override", 0x202e},
		{"the left-to-right isolate", 0x2066},
		{"the right-to-left isolate", 0x2067},
		{"the first strong isolate", 0x2068},
		{"the pop directional isolate", 0x2069},
		{"the zero-width space", 0x200b},
		{"the zero-width non-joiner", 0x200c},
		{"the zero-width joiner", 0x200d},
		{"the word joiner", 0x2060},
		{"the zero-width no-break space", 0xfeff},
	} {
		cases = append(cases, resultCase{
			name: cp.name,
			doc:  runeInScenario(cp.r),
			// The JSON escape spells the character, so the error may not hold
			// the character itself.
			value: string(cp.r),
		})
	}

	// The invisible characters no field may hold: every format, private-use and
	// unassigned code point, the noncharacters, the variation selectors, the tag
	// characters, every space that renders blank and the named invisible ones,
	// each in a single-line field and in a multi-line one. A report a maintainer
	// cannot see is no gate at all, and a rune that renders as nothing or as a
	// blank the eye reads as U+0020 carries a token as well as a zero-width one
	// does.
	invisible := []struct {
		name string
		r    rune
	}{
		{"a tag character", 0xe0041},
		{"a variation selector", 0xfe0f},
		{"a variation selector supplement", 0xe0100},
		{"a Mongolian free variation selector", 0x180b},
		{"a soft hyphen", 0x00ad},
		{"the combining grapheme joiner", 0x034f},
		{"the Hangul choseong filler", 0x115f},
		{"the Hangul jungseong filler", 0x1160},
		{"a Khmer inherent vowel", 0x17b4},
		{"an invisible operator", 0x2062},
		{"the invisible plus", 0x2064},
		{"the Arabic number sign", 0x0600},
		{"the Hangul filler", 0x3164},
		{"the halfwidth Hangul filler", 0xffa0},
		{"a private-use character", 0xe000},
		{"a supplementary private-use character", 0xf0000},
		{"a noncharacter", 0xfdd0},
		{"a noncharacter in the last plane", 0x10fffe},
		{"an unassigned code point", 0x0378},
		{"an unassigned code point past the invisibles", 0x2065},
		// The blanks: every rune of category Zs but U+0020, and the two
		// symbols and the one filler that render as nothing at all. Which
		// blank a model picks between two words is a channel of its own.
		{"a no-break space", 0x00a0},
		{"an Ogham space mark", 0x1680},
		{"an en quad", 0x2000},
		{"an em quad", 0x2001},
		{"an en space", 0x2002},
		{"an em space", 0x2003},
		{"a three-per-em space", 0x2004},
		{"a four-per-em space", 0x2005},
		{"a six-per-em space", 0x2006},
		{"a figure space", 0x2007},
		{"a punctuation space", 0x2008},
		{"a thin space", 0x2009},
		{"a hair space", 0x200a},
		{"a narrow no-break space", 0x202f},
		{"a medium mathematical space", 0x205f},
		{"an ideographic space", 0x3000},
		{"a braille pattern blank", 0x2800},
		{"a Khitan filler", 0x16fe4},
		{"a musical symbol null notehead", 0x1d159},
	}

	for _, cp := range invisible {
		cases = append(cases,
			resultCase{
				name:  cp.name + " in a single-line field",
				doc:   runeInDismissal(cp.r),
				field: "findings[0].dismissal",
				value: string(cp.r),
			},
			resultCase{
				name:  cp.name + " in a multi-line field",
				doc:   runeInEvidence(cp.r),
				field: "findings[0].evidence",
				value: string(cp.r),
			},
		)
	}

	// Everything outside printable ASCII, each in a single-line field and in a
	// multi-line one: a lookalike letter, a mark with no glyph of its own, a
	// typographic mark the eye reads as an ASCII one and a precomposed letter
	// carry a token past the maintainer who reviews the report, so the rule is
	// the allow list of U+0020 to U+007E, not a list of characters to refuse.
	nonASCII := []struct {
		name string
		r    rune
	}{
		{"a Cyrillic small a, a lookalike for a", 0x0430},
		{"a Greek small omicron, a lookalike for o", 0x03bf},
		{"a fullwidth a", 0xff41},
		{"a combining acute accent, a mark with no glyph of its own", 0x0301},
		{"a Tifinagh consonant joiner", 0x2d7f},
		{"a Khmer sign coeng", 0x17d2},
		{"a right single quotation mark", 0x2019},
		{"a hyphen", 0x2010},
		{"an em dash", 0x2014},
		{"a precomposed e acute", 0x00e9},
	}

	for _, cp := range nonASCII {
		cases = append(cases,
			resultCase{
				name:  cp.name + " in a single-line field",
				doc:   runeInDismissal(cp.r),
				field: "findings[0].dismissal",
				value: string(cp.r),
			},
			resultCase{
				name:  cp.name + " in a multi-line field",
				doc:   runeInEvidence(cp.r),
				field: "findings[0].evidence",
				value: string(cp.r),
			},
		)
	}

	// LF and TAB outside the multi-line fields.
	lfOutside := func(mutate func(*docResult)) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			t.Helper()

			spec := minimalSpec().withFinding(oneFinding())
			mutate(&spec)

			return spec.json(t)
		}
	}

	cases = append(cases,
		resultCase{
			name:  "a severity with LF",
			doc:   lfOutside(func(s *docResult) { s.Findings[0].Severity = "low\nhigh" }),
			field: "findings[0].severity",
		},
		resultCase{
			name:  "a severity with TAB",
			doc:   lfOutside(func(s *docResult) { s.Findings[0].Severity = "low\thigh" }),
			field: "findings[0].severity",
		},
		resultCase{
			name:  "a sink with LF",
			doc:   lfOutside(func(s *docResult) { s.Findings[0].Sink = "os.Open\nos.Create" }),
			field: "findings[0].sink",
		},
		resultCase{
			name:  "a dismissal with LF",
			doc:   lfOutside(func(s *docResult) { s.Findings[0].Dismissal = "not exploitable\nreally" }),
			field: "findings[0].dismissal",
		},
		resultCase{
			name:  "a rule with TAB",
			doc:   lfOutside(func(s *docResult) { s.Findings[0].Rule = "G304\tG305" }),
			field: "findings[0].rule",
		},
		resultCase{
			name:  "a limit with TAB",
			doc:   lfOutside(func(s *docResult) { s.Limits = []string{"a limit\twith a tab"} }),
			field: "limits[0]",
		},
		resultCase{
			name:  "a dependency with LF",
			doc:   lfOutside(func(s *docResult) { s.Changes.Dependencies = []string{"example.com/a\nexample.com/b"} }),
			field: "changes.dependencies[0]",
		},
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			commit := testCommit
			if tc.commit != "" {
				commit = tc.commit
			}

			result, err := publish.ParseResult(tc.doc(t), commit)
			if err == nil {
				t.Fatalf("ParseResult accepted the document and returned %+v, want an error wrapping ErrResultInvalid", result)
			}

			if !errors.Is(err, publish.ErrResultInvalid) {
				t.Errorf("ParseResult: error %v does not wrap ErrResultInvalid", err)
			}

			if tc.field != "" && !strings.Contains(err.Error(), tc.field) {
				t.Errorf("ParseResult: error %q does not name the field %q", err.Error(), tc.field)
			}

			if tc.value != "" && strings.Contains(err.Error(), tc.value) {
				t.Errorf("ParseResult: error %q echoes the offending value %q, want a field and a rule only", err.Error(), tc.value)
			}
		})
	}
}

// checksWithNotRunDoc returns the fifteen checks with id not run for why.
func checksWithNotRunDoc(id, why string) []docCheck {
	checks := docRenderedChecks()

	for i := range checks {
		if checks[i].ID == id {
			checks[i].Ran = false
			checks[i].NotRun = why
		}
	}

	return checks
}

// printableASCII returns every printable ASCII character, U+0020 to U+007E.
func printableASCII() string {
	chars := make([]rune, 0, 0x7f-0x20)

	for r := rune(0x20); r <= 0x7e; r++ {
		chars = append(chars, r)
	}

	return string(chars)
}

// runeInScenario returns a document whose finding's scenario holds r.
func runeInScenario(r rune) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		spec := minimalSpec().withFinding(oneFinding())
		spec.Findings[0].Scenario = "see " + string(r) + " here"

		return spec.json(t)
	}
}

// runeInDismissal returns a document whose finding's dismissal holds r, a
// single-line field.
func runeInDismissal(r rune) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		spec := minimalSpec().withFinding(oneFinding())
		spec.Findings[0].Dismissal = "see " + string(r) + " here"

		return spec.json(t)
	}
}

// runeInEvidence returns a document whose finding's evidence holds r, a
// multi-line field.
func runeInEvidence(r rune) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		spec := minimalSpec().withFinding(oneFinding())
		spec.Findings[0].Evidence = "see " + string(r) + " here"

		return spec.json(t)
	}
}

// TestParseResultJSONDepthBound pins the parser's nesting bound independently
// of what the schema allows: a document whose deepest value sits eight levels
// below its top object is parsed and then refused by the schema for the field's
// type, and a document one level deeper is refused by the parser itself as not
// one JSON value (ADR 0040, "The result schema").
func TestParseResultJSONDepthBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// arrays is how many arrays nest inside the top object, so the
		// document's deepest value sits arrays+2 levels down.
		arrays int
		levels int
		// holds is the rule the error must name: the schema's when the parser
		// read the document, the parser's when it refused to.
		holds string
		// lacks is the rule the error must not name.
		lacks string
	}{
		{
			name:   "eight levels are parsed, then refused by the schema",
			arrays: 6,
			levels: 8,
			holds:  "schema: not an integer",
			lacks:  "not one JSON value",
		},
		{
			name:   "nine levels are refused by the parser",
			arrays: 7,
			levels: 9,
			holds:  "not one JSON value",
			lacks:  "schema",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nested := strings.Repeat("[", tc.arrays) + "1" + strings.Repeat("]", tc.arrays)

			result, err := publish.ParseResult(edited(minimalSpec(), `"schema":1`, `"schema":`+nested)(t), testCommit)
			if err == nil {
				t.Fatalf("ParseResult accepted a document nested %d levels deep and returned %+v, want it refused", tc.levels, result)
			}

			if !errors.Is(err, publish.ErrResultInvalid) {
				t.Errorf("ParseResult: error %v does not wrap ErrResultInvalid", err)
			}

			if !strings.Contains(err.Error(), tc.holds) {
				t.Errorf("ParseResult over %d levels: error %q does not name %q: the depth bound must sit at eight levels", tc.levels, err.Error(), tc.holds)
			}

			if strings.Contains(err.Error(), tc.lacks) {
				t.Errorf("ParseResult over %d levels: error %q names %q, want %q alone", tc.levels, err.Error(), tc.lacks, tc.holds)
			}
		})
	}
}

// TestParseResultAccepts pins what the schema lets through: the smallest and
// the largest result, an unknown severity and category (unknown is private, not
// a refusal), LF and TAB in the multi-line fields, and the caps measured in
// bytes at their limit.
func TestParseResultAccepts(t *testing.T) {
	t.Parallel()

	maximalFindings := func(t *testing.T) []docFinding {
		t.Helper()

		findings := make([]docFinding, 0, publish.FindingLimit)

		for i := 0; i < publish.FindingLimit; i++ {
			f := oneFinding()
			f.Scenario = "scenario " + strconv.Itoa(i)
			findings = append(findings, f)
		}

		return findings
	}

	tests := []struct {
		name string
		spec docResult
	}{
		{
			name: "the smallest result",
			spec: minimalSpec(),
		},
		{
			name: "no image was pulled",
			spec: minimalSpec().with(func(s *docResult) { s.ImageDigest = "" }),
		},
		{
			name: "an unknown severity is accepted",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) { s.Findings[0].Severity = "High" }),
		},
		{
			name: "an unknown category is accepted",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) { s.Findings[0].Category = "runtime sink" }),
		},
		{
			name: "an empty severity is accepted",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) { s.Findings[0].Severity = "" }),
		},
		{
			name: "LF and TAB in the multi-line fields",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) {
				s.Findings[0].Scenario = "line one\nline two\tindented"
				s.Findings[0].Evidence = "trace\n\tat main.go:1"
				s.Findings[0].Fix = "fix one\nfix two"
				s.Changes.Narrative = "before\n\tafter"
			}),
		},
		{
			name: "every printable ASCII character in a single-line field",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) {
				s.Findings[0].Dismissal = printableASCII()
			}),
		},
		{
			name: "every printable ASCII character, LF and TAB in a multi-line field",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) {
				s.Findings[0].Scenario = printableASCII() + "\n\t" + printableASCII()
			}),
		},
		{
			name: "the caps measured in bytes, at their limit",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) {
				s.Findings[0].Rule = "G" + strings.Repeat("a", 79)
				s.Findings[0].File = strings.Repeat("f", 200)
				s.Findings[0].Line = 9999999
				s.Findings[0].Sink = strings.Repeat("s", 120)
				s.Findings[0].Severity = strings.Repeat("s", 16)
				s.Findings[0].Category = strings.Repeat("c", 32)
				// 2000 bytes at the cap: a field holds printable ASCII alone,
				// so no multi-byte rune can fill it.
				s.Findings[0].Scenario = strings.Repeat("s", 2000)
				s.Findings[0].Evidence = strings.Repeat("e", 16000)
				s.Findings[0].Fix = strings.Repeat("x", 1000)
				s.Findings[0].Dismissal = strings.Repeat("d", 200)
				s.Changes.Dependencies = []string{strings.Repeat("d", 160)}
				s.Changes.Narrative = strings.Repeat("n", 2000)
				s.Limits = []string{strings.Repeat("l", 300)}
			}),
		},
		{
			name: "a line of 0, a location without one",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) { s.Findings[0].Line = 0 }),
		},
		{
			name: "a history secret names the commit as its file",
			spec: minimalSpec().withFinding(oneFinding()).with(func(s *docResult) {
				s.Findings[0].Check = "history-secrets"
				s.Findings[0].File = testCommit
				s.Findings[0].Rule = "gitleaks:generic-api-key"
			}),
		},
		{
			name: "every tool the schema names",
			spec: minimalSpec().with(func(s *docResult) {
				s.Tools = []docTool{
					{Name: "actionlint", Version: "v1.7.7"},
					{Name: "age", Version: "v1.2.1"},
					{Name: "claude-code", Version: "2.1.330"},
					{Name: "cosign", Version: "v2.4.1"},
					{Name: "gitleaks", Version: "8.29.0"},
					{Name: "go", Version: "1.27.4"},
					{Name: "govulncheck", Version: "v1.1.4"},
					{Name: "osv-scanner", Version: "v2.2.2"},
					{Name: "scorecard", Version: "v5.1.1"},
					{Name: "trivy", Version: "0.58.1"},
					{Name: "zizmor", Version: "v1.0.0"},
				}
			}),
		},
		{
			name: "every check did not run, with every why id",
			spec: minimalSpec().with(func(s *docResult) {
				whys := []string{
					"needs-admin-read", "no-fuzz-targets", "timed-out", "tool-failed",
					"results-missing", "read-refused", "image-not-pulled", "declined",
				}
				s.Checks = docRenderedChecks()

				for i := range s.Checks {
					s.Checks[i].Ran = false
					s.Checks[i].NotRun = whys[i%len(whys)]
				}
			}),
		},
		{
			name: "the largest result: the finding limit, every tool, every limit",
			spec: minimalSpec().with(func(s *docResult) {
				s.Findings = maximalFindings(t)

				deps := make([]string, 0, 50)
				for i := 0; i < 50; i++ {
					deps = append(deps, "example.com/dependency"+strconv.Itoa(i))
				}

				s.Changes.Dependencies = deps

				limits := make([]string, 0, 40)
				for i := 0; i < 40; i++ {
					limits = append(limits, "limit "+strconv.Itoa(i))
				}

				s.Limits = limits
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := publish.ParseResult(tc.spec.json(t), testCommit)
			if err != nil {
				t.Fatalf("ParseResult: %v", err)
			}

			if diff := cmp.Diff(tc.spec.want(), got); diff != "" {
				t.Errorf("ParseResult mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// fileInFinding returns a document whose one finding's file is file.
func fileInFinding(file string) func(*testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()

		spec := minimalSpec().withFinding(oneFinding())
		spec.Findings[0].File = file

		return spec.json(t)
	}
}

// TestParseResultFilePath pins the schema's file rule over a finding's file:
// 1 to 200 bytes of the allowed set, split at / into segments that are none of
// empty, `.` and `..`. A segment that starts with a dot is a real path of this
// repository, `.github/workflows/ci.yml` and `.dot-agent-deck.toml` among them,
// and the report renders such a finding like any other; a segment that is a dot
// itself, and the paths that hold one, are refused, as is every character
// outside the allowed set and every byte the text rule refuses. Only the two
// segments `.` and `..` traverse, so a segment of dots and more, `...` among
// them, is a file name like any other and is accepted. Each refusal
// wraps ErrResultInvalid, names the field and echoes no value.
func TestParseResultFilePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		// wantRefused is whether the schema refuses the file.
		wantRefused bool
	}{
		// Accepted: a dot that starts a segment is part of a real path.
		{name: "the control, a path of the publish core", file: "ci/auditpublish/internal/publish/result.go"},
		{name: "a workflow of the top directory", file: ".github/workflows/x.yml"},
		{name: "the deck's configuration at the top", file: ".dot-agent-deck.toml"},
		{name: "an agent's settings file", file: ".claude/settings.json"},
		{name: "an extension of the agent that runs this audit", file: ".pi/extensions/x.ts"},
		{name: "a dot-leading segment in the middle", file: "a/.hidden/b"},
		{name: "a dot inside a name, never at its start", file: "docs/adr/0040-scheduled-security-audit.md"},
		{name: "every segment a dotfile", file: ".github/.hidden/.env.example"},
		{name: "a segment of three dots, a legal file name", file: "..."},
		{name: "a three-dot directory and a file", file: ".../x"},
		{name: "a segment of three dots at the end", file: "a/..."},
		{name: "a segment of dots and a letter", file: ".a..b"},
		{name: "a dot then a dash", file: ".-x"},
		{name: "a dot then a plus", file: ".+x"},
		{name: "a dot then a colon", file: ".:x"},
		{name: "a dot then an at sign", file: ".@x"},
		// 200 bytes, the file's cap: the boundary the 201-byte row below sits
		// on, in a dot-leading name no shorter than the cap.
		{name: "200 bytes, a dot-leading name at the cap", file: "." + strings.Repeat("f", 199)},

		// Refused: a segment that is a dot, and the paths that hold one.
		{name: "a lone dot", file: ".", wantRefused: true},
		{name: "a lone double dot", file: "..", wantRefused: true},
		{name: "a current segment and a name", file: "./x", wantRefused: true},
		{name: "a parent segment and a name", file: "../x", wantRefused: true},
		{name: "a parent segment in the middle", file: "a/../b", wantRefused: true},
		{name: "a current segment in the middle", file: "a/./b", wantRefused: true},
		{name: "a parent segment at the end", file: "a/..", wantRefused: true},
		{name: "a current segment at the end", file: "a/.", wantRefused: true},
		{name: "a current segment first and a current segment last", file: "./.", wantRefused: true},
		{name: "a current segment and a parent segment", file: "./..", wantRefused: true},
		{name: "three dots inside a traverse", file: ".../../x", wantRefused: true},
		{name: "an absolute path", file: "/x", wantRefused: true},
		{name: "an empty segment", file: "a//b", wantRefused: true},
		{name: "a trailing slash", file: "a/", wantRefused: true},
		{name: "the empty string", file: "", wantRefused: true},
		{name: "201 bytes", file: strings.Repeat("f", 201), wantRefused: true},

		// Refused: a character outside the allowed set.
		{name: "a space", file: "a b", wantRefused: true},
		{name: "a backslash", file: `a\b`, wantRefused: true},
		{name: "a non-ASCII character", file: "café/x.go", wantRefused: true},
		{name: "a leading dash", file: "-x", wantRefused: true},
		{name: "a newline", file: "a\nb", wantRefused: true},
		{name: "a tab", file: "a\tb", wantRefused: true},
		{name: "a DEL byte", file: "a\x7fb", wantRefused: true},
		{name: "a pipe, which the table escapes", file: "a|b", wantRefused: true},
		{name: "a query mark", file: "a?b", wantRefused: true},
		{name: "a dot-leading segment and a byte outside the set", file: ".a b", wantRefused: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := publish.ParseResult(fileInFinding(tc.file)(t), testCommit)

			if !tc.wantRefused {
				if err != nil {
					t.Fatalf("ParseResult refused the file %q: %v, want it accepted: a dot that starts a segment is a real repository path", tc.file, err)
				}

				if got := result.Findings[0].File; got != tc.file {
					t.Errorf("ParseResult: the finding's file is %q, want %q", got, tc.file)
				}

				return
			}

			if err == nil {
				t.Fatalf("ParseResult accepted the file %q and returned %+v, want an error wrapping ErrResultInvalid", tc.file, result)
			}

			if !errors.Is(err, publish.ErrResultInvalid) {
				t.Errorf("ParseResult over %q: error %v does not wrap ErrResultInvalid", tc.file, err)
			}

			if !strings.Contains(err.Error(), "findings[0].file") {
				t.Errorf("ParseResult over %q: error %q does not name the field findings[0].file", tc.file, err.Error())
			}
		})
	}
}

// TestParseResultAcceptsAtMaxResultBytes pins the size limit's boundary: a
// document of exactly MaxResultBytes passes, and the row of the refusal table
// that is one byte longer fails.
func TestParseResultAcceptsAtMaxResultBytes(t *testing.T) {
	t.Parallel()

	doc := minimalSpec().json(t)
	pad := publish.MaxResultBytes - len(doc)
	if pad < 0 {
		t.Fatalf("MaxResultBytes is %d, smaller than the fixture's %d bytes, so this test cannot pin the limit", publish.MaxResultBytes, len(doc))
	}

	padded := []byte(strings.Repeat(" ", pad) + string(doc))
	if len(padded) != publish.MaxResultBytes {
		t.Fatalf("the fixture is %d bytes, want exactly MaxResultBytes", len(padded))
	}

	if _, err := publish.ParseResult(padded, testCommit); err != nil {
		t.Errorf("ParseResult over exactly MaxResultBytes bytes: %v, want the document accepted", err)
	}
}

// TestSchemaConstants pins the values the schema and the scan are stated in:
// a wrong cap or limit would pass every other test in this package.
func TestSchemaConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"SchemaVersion", float64(publish.SchemaVersion), 1},
		{"MaxResultBytes is 4 MiB", float64(publish.MaxResultBytes), 4 * 1024 * 1024},
		{"FindingLimit", float64(publish.FindingLimit), 100},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}
