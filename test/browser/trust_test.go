//go:build browser && !js

package browser_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// This file holds the trust boundary of the page's glue (#346 part 1, the
// maintainer's decision) and the hostile inventory criterion (PRD #297, Trust
// and Assessment in the browser). What the page shows from an inventory or a
// catalog is text; only the HTML reporter's fragment is markup; and an error
// reaches the page as a string it writes with textContent, so a value that
// carries markup can neither create an element nor run anything.

// The hostile values the test tries. None of them may ever run, create an
// element or be inserted as markup; their markers are what the test looks for.
const (
	hostileMarker   = `<img src=x onerror=__injected=1>`
	hostileTag      = `<b onmouseover=__injected=2>x</b>`
	hostileScript   = `<script>__injected=3</script>`
	hostileThrown   = `<img src=x onerror=__injected=4>`
	injected        = "__injected"
	hostileInjected = 0
)

// callOutcome is what one direct call of the module's function answered, or the
// exception it threw.
type callOutcome struct {
	Name      string `json:"name"`
	Threw     bool   `json:"threw"`
	Thrown    string `json:"thrown"`
	ErrorType string `json:"errorType"`
	KindType  string `json:"kindType"`
	Error     string `json:"error"`
	Kind      string `json:"kind"`
}

// afterThrowOutcome is what the call after the throwing getter answered: whether
// it threw, and whether it returned a report.
type afterThrowOutcome struct {
	Threw  bool   `json:"threw"`
	Thrown string `json:"thrown"`
	Error  string `json:"error"`
	Kind   string `json:"kind"`
	Report bool   `json:"report"`
}

// The names of the hostile calls the test makes, so its assertions can read one
// outcome by name.
const (
	callInventoryNumber   = "the inventory is a number"
	callOptionsString     = "the options are a string"
	callTargetsString     = "targets is a string"
	callEstimateString    = "estimate is a string"
	callHostileTarget     = "a hostile target key"
	callHostileRegion     = "a hostile region"
	callThrowingGetter    = "a getter that throws"
	callHostileInventory  = "a hostile inventory"
	callOversizedDocument = "a document over the reader's size cap"
)

// hostileDocument is an inventory the reader refuses with an error that carries
// a hostile value as text: the unknown key's name is quoted in the message.
const hostileDocument = `{"schemaVersion":1,"source":"aws","resources":[{"id":"i-0123456789abcdef0","category":"compute.vm","<img src=x onerror=__injected=1>":true}]}`

// directCallsExpression calls globalThis.shift2euAssess with hostile values and
// returns what each call answered. It runs in the page, so the values reach the
// glue exactly as the page's own call would pass them; nothing here inserts
// anything, which is what the test then asserts about the page.
func directCallsExpression() string {
	return fmt.Sprintf(`(() => {
	const outcomes = [];
	function call(name, inventory, options) {
		const entry = {name: name, threw: false, thrown: "", error: "", kind: "", errorType: "", kindType: ""};
		let result;
		try {
			result = globalThis.shift2euAssess(inventory, options);
		} catch (failure) {
			entry.threw = true;
			entry.thrown = String(failure !== null && failure !== undefined && failure.message !== undefined ? failure.message : failure);
			outcomes.push(entry);
			return;
		}
		if (result !== null && typeof result === "object") {
			entry.errorType = typeof result.error;
			entry.kindType = typeof result.kind;
			entry.error = typeof result.error === "string" ? result.error : "";
			entry.kind = typeof result.kind === "string" ? result.kind : "";
		}
		outcomes.push(entry);
	}
	call(%s, 42, {});
	call(%s, "{}", "options");
	call(%s, "{}", {targets: %s});
	call(%s, "{}", {targets: ["ovhcloud"], estimate: "yes"});
	call(%s, "{}", {targets: [%s]});
	call(%s, "{}", {targets: ["ovhcloud"], regions: [{target: "ovhcloud", region: %s}]});
	call(%s, "{}", {get targets() { throw new Error(%s); }});
	call(%s, %s, {targets: ["ovhcloud"]});
	return outcomes;
})()`,
		jsString(callInventoryNumber),
		jsString(callOptionsString),
		jsString(callTargetsString), jsString(hostileTag),
		jsString(callEstimateString),
		jsString(callHostileTarget), jsString(hostileMarker),
		jsString(callHostileRegion), jsString(hostileTag),
		jsString(callThrowingGetter), jsString(hostileThrown),
		jsString(callHostileInventory), jsString(hostileDocument),
	)
}

// TestGlueReturnsTextOnlyErrors pins the trust boundary of the glue (#346 part
// 1): error and kind are plain strings, a message the glue itself makes is
// constant (so it echoes no value), a message a value reaches is text, and none
// of them is inserted anywhere. The module returns data; the page decides where
// it goes, and the page writes an error with textContent.
func TestGlueReturnsTextOnlyErrors(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	eval[bool](t, ctx, fmt.Sprintf(`globalThis.%s = %d; true`, injected, hostileInjected))

	outcomes := eval[[]callOutcome](t, ctx, directCallsExpression())

	byName := make(map[string]callOutcome, len(outcomes))
	for _, outcome := range outcomes {
		byName[outcome.Name] = outcome
	}

	if len(byName) != 8 {
		t.Fatalf("the page answered %d of the 8 hostile calls, want one outcome each: %v", len(byName), outcomes)
	}

	// Wrong types: a constant message, so the page shows nothing the caller
	// passed.
	wrongArguments := []callOutcome{byName[callInventoryNumber], byName[callOptionsString]}

	for _, outcome := range wrongArguments {
		assertConstantError(t, outcome, "internal")
	}

	if wrongArguments[0].Error != wrongArguments[1].Error {
		t.Errorf("two calls with the wrong argument types answer %q and %q, want one constant message: a message that differs echoes what the caller passed",
			wrongArguments[0].Error, wrongArguments[1].Error)
	}

	// A mistyped option: also one constant message.
	mistyped := []callOutcome{byName[callTargetsString], byName[callEstimateString]}

	for _, outcome := range mistyped {
		assertConstantError(t, outcome, "usage")
	}

	if mistyped[0].Error != mistyped[1].Error {
		t.Errorf("two mistyped option calls answer %q and %q, want one constant message", mistyped[0].Error, mistyped[1].Error)
	}

	// A hostile value the option rules refuse: the message is the CLI's, it
	// names the value as text, and it is a string and nothing else.
	hostileTarget := byName[callHostileTarget]
	if hostileTarget.Threw || hostileTarget.Kind != "usage" || hostileTarget.ErrorType != "string" {
		t.Errorf("a call whose target key is %q answered %+v, want a usage error whose message is a string", hostileMarker, hostileTarget)
	}

	if !strings.Contains(hostileTarget.Error, hostileMarker) {
		t.Errorf("the usage error for the target key %q is %q, want it to name the key the caller gave, as the CLI's --target error does", hostileMarker, hostileTarget.Error)
	}

	hostileRegion := byName[callHostileRegion]
	if hostileRegion.Threw || hostileRegion.Kind != "usage" || hostileRegion.ErrorType != "string" {
		t.Errorf("a call whose region is %q answered %+v, want a usage error whose message is a string", hostileTag, hostileRegion)
	}

	if !strings.Contains(hostileRegion.Error, hostileTag) {
		t.Errorf("the usage error for the region %q is %q, want it to name the region the caller gave, as the CLI's --region error does", hostileTag, hostileRegion.Error)
	}

	// A hostile inventory: the reader's error names the unknown key, so the
	// hostile value does reach the message — as a quoted string, which the page
	// writes with textContent. That is the path the page's alert check covers
	// below; here the assertion is that it is a string and nothing else.
	hostileInventory := byName[callHostileInventory]
	if hostileInventory.Threw || hostileInventory.Kind != "inventory" || hostileInventory.ErrorType != "string" {
		t.Errorf("a call whose inventory is %q answered %+v, want an inventory error whose message is a string", hostileDocument, hostileInventory)
	}

	for _, want := range []string{"unknown key", hostileMarker} {
		if !strings.Contains(hostileInventory.Error, want) {
			t.Errorf("the inventory error %q does not name %q, want the reader's message, which names the key at fault as text", hostileInventory.Error, want)
		}
	}

	// A getter that throws while the glue reads the options object. syscall/js
	// cannot catch a JavaScript exception, so not even the glue's recover sees
	// it: the call throws to the page, whose own call catches it and shows a
	// constant message (ADR 0036). A call that answered an internal error here
	// would mean the glue caught what it cannot catch.
	getter := byName[callThrowingGetter]
	t.Logf("the call with a throwing getter answered %+v", getter)

	if !getter.Threw {
		t.Errorf("the call with a throwing getter answered %+v, want a thrown exception: a JavaScript exception a getter throws unwinds through the module and syscall/js cannot catch it (ADR 0036)", getter)
	} else if getter.Thrown == "" {
		t.Error("the call with a throwing getter threw an empty exception, so the page could not report anything")
	}

	// The module still answers after that throw: the same page's next call, with
	// a valid inventory, returns a report rather than throwing again or leaving
	// the module dead (ADR 0036).
	after := eval[afterThrowOutcome](t, ctx, fmt.Sprintf(`(() => {
	const entry = {threw: false, thrown: "", error: "", kind: "", report: false};
	let result;
	try {
		result = globalThis.shift2euAssess(%s, {targets: ["ovhcloud"]});
	} catch (failure) {
		entry.threw = true;
		entry.thrown = String(failure);
		return entry;
	}
	if (result !== null && typeof result === "object") {
		entry.error = typeof result.error === "string" ? result.error : "";
		entry.kind = typeof result.kind === "string" ? result.kind : "";
		entry.report = typeof result.markdown === "string" && result.markdown !== "" &&
			typeof result.json === "string" && result.json !== "" &&
			typeof result.html === "string" && result.html !== "";
	}
	return entry;
})()`, jsString(schemaVersion1Inventory)))

	switch {
	case after.Threw:
		t.Errorf("the assessment after the throwing getter threw %q, want a report: the module stays usable for the page's next run (ADR 0036)", after.Thrown)
	case after.Error != "" || after.Kind != "":
		t.Errorf("the assessment after the throwing getter answered the %q error %q, want a report: a valid inventory assessed on the same page", after.Kind, after.Error)
	case !after.Report:
		t.Errorf("the assessment after the throwing getter returned %+v, want markdown, json and html for a valid inventory: the module answers normally after the throw", after)
	}

	// None of the calls inserted anything: the module returns data and the page
	// is where markup is inserted, in its one place.
	if report := reportOf(t, ctx); report.Text != "" {
		t.Errorf("the hostile calls put %q into the page's report, but the module returns data and inserts nothing", report.Text)
	}

	if alert := alertOf(t, ctx); alert.Text != "" {
		t.Errorf("the hostile calls put %q into the page's alert, but the module returns data and inserts nothing", alert.Text)
	}

	if got := eval[int](t, ctx, fmt.Sprintf(`globalThis.%s || 0`, injected)); got != hostileInjected {
		t.Errorf("a hostile value ran: globalThis.%s is %d, want %d", injected, got, hostileInjected)
	}

	if got := eval[int](t, ctx, `document.querySelectorAll("img, b, iframe").length`); got != 0 {
		t.Errorf("the hostile calls created %d elements, want none: an error is text and is shown with textContent", got)
	}
}

// assertConstantError pins one outcome: no exception, one kind, and a message
// that is a string and carries no part of the hostile values.
func assertConstantError(t *testing.T, outcome callOutcome, kind string) {
	t.Helper()

	switch {
	case outcome.Threw:
		t.Errorf("the call %q threw %q, want a %s error the page can show", outcome.Name, outcome.Thrown, kind)
	case outcome.Kind != kind:
		t.Errorf("the call %q answered kind %q, want %q", outcome.Name, outcome.Kind, kind)
	case outcome.ErrorType != "string":
		t.Errorf("the call %q answered a %q error, want a string", outcome.Name, outcome.ErrorType)
	case outcome.Error == "":
		t.Errorf("the call %q answered an empty error, want the constant message", outcome.Name)
	}

	for _, marker := range []string{hostileMarker, hostileTag, hostileScript, hostileThrown, injected} {
		if strings.Contains(outcome.Error, marker) {
			t.Errorf("the message of %q, %q, carries %q, but a constant message echoes no value the caller passed", outcome.Name, outcome.Error, marker)
		}
	}
}

// hostileInventory is an inventory whose every value tries to inject markup or
// to name a global of the page: its IDs, raw types, region, workload and
// relations all carry a tag, an attribute or a script, and two IDs name the
// module's global and the module's function, which the page reads through
// js.Global().Get and which a report element with that id or name would shadow.
const hostileInventory = `{
  "schemaVersion": 2,
  "source": "aws",
  "resources": [
    {
      "id": "<script>__injected=1</script>",
      "rawType": "<img src=x onerror=__injected=2>",
      "category": "compute.vm",
      "region": "eu-west-1",
      "capabilities": ["ipv6"],
      "size": {"vcpus": 2, "memoryGiB": 8},
      "workload": "<b onmouseover=__injected=3>x</b>",
      "relations": [{"type": "depends-on", "target": "__shift2euModule"}]
    },
    {
      "id": "__shift2euModule",
      "rawType": "AWS::S3::Bucket",
      "category": "storage.object",
      "region": "eu-west-1",
      "workload": "status"
    },
    {
      "id": "shift2euAssess",
      "rawType": "AWS::Lambda::Function",
      "category": "serverless.function",
      "region": "eu-west-1",
      "workload": "report"
    },
    {
      "id": "onerror=__injected=4",
      "rawType": "\" onmouseover=\"__injected=5",
      "category": "unclassified",
      "region": "eu-west-1"
    }
  ]
}`

// reporterTags are the elements the HTML reporter may write: its closed set of
// semantic elements, and nothing that runs, loads or leaves the page. A value
// that is escaped can open none of them, so one the reporter's set does not
// hold, or an attribute the report never carries, shows an injection.
var reporterTags = []string{
	"a", "abbr", "b", "blockquote", "br", "caption", "code", "col", "colgroup", "dd", "div", "dl",
	"dt", "em", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "i", "li", "mark",
	"ol", "p", "pre", "section", "small", "span", "strong", "sub", "sup", "table", "tbody", "td",
	"tfoot", "th", "thead", "time", "tr", "ul",
}

// injectedTags are the elements the hostile values try to open, and the ones a
// report never carries: none of them may exist in the report, whatever the
// reporter's wider set allows.
var injectedTags = []string{
	"img", "script", "iframe", "svg", "style", "link", "meta", "object", "embed", "form", "input",
	"button", "video", "audio", "source", "math", "marquee", "base", "frame", "portal",
}

// TestHostileInventoryRendersAsText pins the hostile inventory criterion (PRD
// #297, Assessment in the browser): an inventory whose values carry quotes,
// angle brackets, a <script> and event handlers renders as text, and the page
// holds no element, attribute or script it tried to inject.
func TestHostileInventoryRendersAsText(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	eval[bool](t, ctx, fmt.Sprintf(`globalThis.%s = %d; true`, injected, hostileInjected))

	scriptsBefore := eval[int](t, ctx, `document.querySelectorAll("script").length`)

	pasteInventory(t, ctx, hostileInventory)
	setTargets(t, ctx, "ovhcloud")
	runAssessment(t, ctx)

	if message := alertOf(t, ctx).Text; message != "" {
		t.Fatalf("the page refused the hostile inventory with %q, want the report: this inventory is valid, its values are hostile", message)
	}

	report := reportOf(t, ctx)

	// The values are shown, as text.
	for _, value := range []string{"<script>__injected=1</script>", "<img src=x onerror=__injected=2>", "__shift2euModule", "shift2euAssess"} {
		if !strings.Contains(report.Text, value) {
			t.Errorf("the report does not show %q as text, so a value of the inventory went missing: %q", value, report.Text)
		}
	}

	// No element outside the reporter's set, and none of the injected ones.
	for _, tag := range report.Tags {
		if !slices.Contains(reporterTags, tag) {
			t.Errorf("the report holds a <%s> element, which is not one the HTML reporter writes: the inventory injected markup", tag)
		}
	}

	for _, tag := range injectedTags {
		if report.hasTag(tag) {
			t.Errorf("the report holds a <%s> element, which the hostile inventory tried to inject: a value renders as text", tag)
		}
	}

	// No id or name from the inventory: a report element must not shadow a
	// global the glue reads.
	if report.IDs != 0 {
		t.Errorf("the report holds %d elements with an id or a name, want none: no report element may shadow globalThis.__shift2euModule or a name the glue reads", report.IDs)
	}

	// No inline event handler.
	if report.Handlers != 0 {
		t.Errorf("the report holds %d attributes starting with \"on\", want none: no value may become an event handler", report.Handlers)
	}

	// Nothing ran, and no script appeared.
	if got := eval[int](t, ctx, fmt.Sprintf(`globalThis.%s || 0`, injected)); got != hostileInjected {
		t.Errorf("a value of the hostile inventory ran: globalThis.%s is %d, want %d", injected, got, hostileInjected)
	}

	if got := eval[int](t, ctx, `document.querySelectorAll("script").length`); got != scriptsBefore {
		t.Errorf("the page holds %d scripts after the hostile inventory was assessed, want the %d it loaded: the inventory injected one", got, scriptsBefore)
	}
}

// TestInventoryErrorsNameAPositionAndStayUsable pins the error criterion (PRD
// #297, Assessment in the browser): each document the reader refuses shows one
// error naming a position or a field and never a value from the document, a bad
// target or region shows the CLI's usage error, and the page runs an assessment
// afterwards.
func TestInventoryErrorsNameAPositionAndStayUsable(t *testing.T) { //nolint:tparallel // the cases run in order on one page: each asserts that the page stays usable after the one before
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	const marker = "ACCT-MARKER-4242"

	tests := []struct {
		name string
		// document is the inventory pasted into the page.
		document string
		// mentions are what the error must name: the position, or the field.
		mentions []string
	}{
		{
			name:     "malformed JSON names its position",
			document: `{"schemaVersion":1,"source":}`,
			mentions: []string{"inventory document", "malformed JSON", "line", "column"},
		},
		{
			name:     "an unknown schema version names the field",
			document: `{"schemaVersion":3,"source":"aws","resources":[]}`,
			mentions: []string{"inventory document", "schemaVersion", "unsupported schema version"},
		},
		{
			name:     "an unknown field names the field and its position",
			document: `{"schemaVersion":1,"source":"aws","resources":[{"id":"i-0123456789abcdef0","category":"compute.vm","regoin":"` + marker + `"}]}`,
			mentions: []string{"inventory document", "unknown key", "regoin", "line"},
		},
		{
			name:     "a control character names its position and keeps the value out",
			document: `{"schemaVersion":1,"source":"aws","resources":[{"id":"i-0123456789abcdef0\u0007` + marker + `","category":"compute.vm"}]}`,
			mentions: []string{"inventory document", "resources[0].id"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pasteInventory(t, ctx, tc.document)
			runAssessment(t, ctx)

			alert := alertOf(t, ctx)

			switch {
			case alert.Text == "":
				t.Fatalf("the page shows no error for %s", tc.name)
			case !alert.TextOnly:
				t.Errorf("the page's alert holds markup for an error: its innerHTML is %q, want the message escaped (%q)", alert.HTML, alert.Text)
			case alert.Elements != 0:
				t.Errorf("the page's alert holds %d elements, want none: an error is shown with textContent", alert.Elements)
			}

			for _, want := range tc.mentions {
				if !strings.Contains(alert.Text, want) {
					t.Errorf("the error %q does not name %q, so it does not say where the document is wrong", alert.Text, want)
				}
			}

			if strings.Contains(alert.Text, marker) {
				t.Errorf("the error %q repeats the value %q from the document: the reader names a position and a field, never a value", alert.Text, marker)
			}

			if report := reportOf(t, ctx); report.Text != "" {
				t.Errorf("the page shows a report as well as the error %q: one refused document shows one error and no report", alert.Text)
			}
		})
	}

	// A document over the reader's size cap: 11 MiB, one more than the cap, of a
	// document that is not even JSON, since the size is checked first. The call
	// is the one the page's run button makes with the field's value: the smoke
	// test does not paste 11 MiB into the text area, because laying out eleven
	// million characters is the browser's work on the field and not the reader's
	// size cap, which is what this case is about.
	oversized := eval[callOutcome](t, ctx, fmt.Sprintf(`(() => {
		const entry = {name: %s, threw: false, thrown: "", error: "", kind: "", errorType: "", kindType: ""};
		try {
			const result = globalThis.shift2euAssess("x".repeat(11 * 1024 * 1024), {targets: ["ovhcloud"]});
			entry.error = typeof result.error === "string" ? result.error : "";
			entry.kind = typeof result.kind === "string" ? result.kind : "";
			entry.errorType = typeof result.error;
		} catch (failure) {
			entry.threw = true;
			entry.thrown = String(failure);
		}
		return entry;
	})()`, jsString(callOversizedDocument)))

	switch {
	case oversized.Threw:
		t.Errorf("the document over the reader's size cap threw %q, want an inventory error the page shows as text", oversized.Thrown)
	case oversized.Kind != "inventory":
		t.Errorf("the document over the reader's size cap answered kind %q, want inventory", oversized.Kind)
	case !strings.Contains(oversized.Error, "too large"):
		t.Errorf("the error for a document over the size cap is %q, want it to say the document is too large", oversized.Error)
	}

	// A bad target and a bad region: the CLI's usage errors, byte for byte.
	usage := eval[[]callOutcome](t, ctx, `(() => {
	const outcomes = [];
	function call(name, options) {
		const entry = {name: name, threw: false, thrown: "", error: "", kind: "", errorType: "", kindType: ""};
		try {
			const result = globalThis.shift2euAssess("{}", options);
			entry.error = typeof result.error === "string" ? result.error : "";
			entry.kind = typeof result.kind === "string" ? result.kind : "";
			entry.errorType = typeof result.error;
		} catch (failure) {
			entry.threw = true;
			entry.thrown = String(failure);
		}
		outcomes.push(entry);
	}
	call("target", {targets: ["nowhere"]});
	call("region", {targets: ["ovhcloud"], regions: [{target: "ovhcloud", region: "nowhere"}]});
	return outcomes;
})()`)

	if len(usage) != 2 {
		t.Fatalf("the page answered %d of the two bad choices, want two", len(usage))
	}

	for i, want := range []struct {
		name string
		args []string
	}{
		{name: "target", args: []string{"--target", "nowhere"}},
		{name: "region", args: []string{"--target", "ovhcloud", "--region", "ovhcloud=nowhere"}},
	} {
		got := usage[i]

		switch {
		case got.Threw:
			t.Errorf("the bad %s threw %q, want the CLI's usage error", want.name, got.Thrown)
			continue
		case got.Kind != "usage":
			t.Errorf("the bad %s answered kind %q, want usage", want.name, got.Kind)
		}

		expected := cliUsageMessage(t, `{"schemaVersion":1,"source":"aws","resources":[]}`, want.args...)
		if got.Error != expected {
			t.Errorf("the page's error for a bad %s is %q, want the CLI's own usage error %q", want.name, got.Error, expected)
		}
	}

	// The page stays usable: the next assessment works.
	pasteInventory(t, ctx, schemaVersion1Inventory)
	setTargets(t, ctx, "ovhcloud")
	runAssessment(t, ctx)

	if message := alertOf(t, ctx).Text; message != "" {
		t.Fatalf("the page shows the error %q after the errors above, want a report: the page stays usable", message)
	}

	if report := reportOf(t, ctx); !report.hasSection("Summary") {
		t.Fatalf("the page shows no report after the errors above: %v", report.Sections)
	}
}

// nonObjectAnswer is one value the module's function answers in the test below
// instead of its own: a value that is no object, so the run handler cannot read
// .error of it.
type nonObjectAnswer struct {
	// name is the answer in a failure message.
	name string
	// value is the JavaScript expression the page's call answers.
	value string
}

// nonObjectAnswers are the answers the module answers in the test below: the
// undefined a function without a return gives, the null a failed conversion
// gives, a string that carries markup, and a number. Reading .error of one of
// them throws inside the page's click handler, which leaves the page's alert
// empty and says nothing about what happened.
func nonObjectAnswers() []nonObjectAnswer {
	return []nonObjectAnswer{
		{name: "undefined", value: "undefined"},
		{name: "null", value: "null"},
		{name: "a string carrying markup", value: jsString(hostileMarker)},
		{name: "a number", value: "42"},
	}
}

// TestNonObjectResultShowsAConstantMessage pins the page's answer to a module
// result that is no object (PRD #297, Trust and Assessment in the browser; ADR
// 0036): the run handler reads result.error, and .error of undefined or null
// throws, so the page must check that result is a non-null object first and show
// one constant message with textContent when it is not. The module keeps its own
// function throughout: the test replaces globalThis.shift2euAssess with a function
// that answers one of the values below, which is what a module answering such a
// value looks like to the page's handler, and puts the module's function back
// before it runs the page's own assessment again.
func TestNonObjectResultShowsAConstantMessage(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	pasteInventory(t, ctx, schemaVersion1Inventory)
	setTargets(t, ctx, "ovhcloud")

	eval[bool](t, ctx, fmt.Sprintf(`globalThis.%s = %d; true`, injected, hostileInjected))

	answers := nonObjectAnswers()

	values := make([]string, 0, len(answers))
	for _, answer := range answers {
		values = append(values, answer.value)
	}

	eval[bool](t, ctx, fmt.Sprintf(`(() => {
	globalThis.__shift2euAnswers = [%s];
	globalThis.__shift2euOriginalAssess = globalThis.shift2euAssess;
	globalThis.__shift2euIndex = 0;
	globalThis.__shift2euCalled = false;
	globalThis.shift2euAssess = function () {
		globalThis.__shift2euCalled = true;
		return globalThis.__shift2euAnswers[globalThis.__shift2euIndex];
	};
	return true;
})()`, strings.Join(values, ", ")))

	message := ""

	for i, answer := range answers {
		eval[bool](t, ctx, fmt.Sprintf(`(() => {
	globalThis.__shift2euCalled = false;
	globalThis.__shift2euIndex = %d;
	return true;
})()`, i))

		do(t, ctx, chromedp.Click(chromedp.ID("run")))
		waitFor(t, ctx, "the run handler to answer "+answer.name, `globalThis.__shift2euCalled === true`)

		alert := alertOf(t, ctx)

		if alert.Text == "" {
			t.Fatalf("the page shows nothing after the module answered %s: the run handler read .error of a value that is no object, so its click threw and left the page's alert empty. A non-null object check before the read shows one constant message instead (PRD #297, Trust and Assessment in the browser)", answer.name)
		}

		if !alert.TextOnly || alert.Elements != 0 {
			t.Errorf("the page's message for %s holds markup: its innerHTML is %q, want the message escaped and no element (%q)", answer.name, alert.HTML, alert.Text)
		}

		if strings.Contains(alert.Text, hostileMarker) {
			t.Errorf("the page's message for %s is %q, which carries the value the module answered: the message is one constant, so it echoes nothing", answer.name, alert.Text)
		}

		if i == 0 {
			message = alert.Text
		} else if alert.Text != message {
			t.Errorf("the module answered %s and the page shows %q, but it showed %q for undefined: a constant message is the same for every answer, so it names no value", answer.name, alert.Text, message)
		}

		if got := eval[int](t, ctx, fmt.Sprintf(`globalThis.%s || 0`, injected)); got != hostileInjected {
			t.Errorf("a value ran: globalThis.%s is %d after the module answered %s, want %d", injected, got, answer.name, hostileInjected)
		}

		if report := reportOf(t, ctx); report.Text != "" {
			t.Errorf("the page shows the report %q after the module answered %s, want no report: a result that is no object carries no assessment", report.Text, answer.name)
		}

		if enabled := eval[bool](t, ctx, `!document.getElementById("download-markdown").disabled || !document.getElementById("download-json").disabled`); enabled {
			t.Errorf("the page enables a download after the module answered %s, want both disabled: there are no bytes", answer.name)
		}
	}

	// The page stays usable: the module's own function is put back, and the page
	// runs an assessment again.
	eval[bool](t, ctx, `(() => {
	globalThis.shift2euAssess = globalThis.__shift2euOriginalAssess;
	delete globalThis.__shift2euAnswers;
	delete globalThis.__shift2euOriginalAssess;
	return true;
})()`)

	runAssessment(t, ctx)

	if alert := alertOf(t, ctx).Text; alert != "" {
		t.Fatalf("the page shows the error %q after the module answered no object, want a report: the page stays usable", alert)
	}

	if report := reportOf(t, ctx); !report.hasSection("Summary") {
		t.Fatalf("the page shows no report after the module answered no object: %v", report.Sections)
	}
}
