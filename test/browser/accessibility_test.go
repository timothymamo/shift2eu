//go:build browser

package browser_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// This file holds the accessibility criterion (PRD #297, Accessibility) as a
// browser answers it: every control of the main flow has an accessible name, the
// flow's controls take focus in the order a keyboard user expects, and the focus
// is visible.

// focusStep is one element the keyboard reached, and how it looked when it had
// the focus.
type focusStep struct {
	ID           string `json:"id"`
	FocusVisible bool   `json:"focusVisible"`
	OutlineStyle string `json:"outlineStyle"`
	OutlineWidth string `json:"outlineWidth"`
}

// recordFocus records every element the keyboard focuses, with the styles that
// show the focus.
const recordFocus = `(() => {
	globalThis.__focusOrder = [];
	document.addEventListener("focusin", function (event) {
		const el = event.target;
		const style = getComputedStyle(el);
		globalThis.__focusOrder.push({
			id: el.id || el.tagName.toLowerCase(),
			focusVisible: el.matches(":focus-visible"),
			outlineStyle: style.outlineStyle,
			outlineWidth: style.outlineWidth,
		});
	});
	return true;
})()`

// mainFlow is the order the main flow's controls take focus in, the one the page
// lays out: the inventory, the targets with their regions, the estimate switch,
// the run button and the two downloads.
var mainFlow = []string{
	"inventory", "inventory-file", "example", "load-example",
	"target-ovhcloud", "region-ovhcloud",
	"target-scaleway", "region-scaleway",
	"target-upcloud", "region-upcloud",
	"estimate", "run", "download-markdown", "download-json",
}

// TestTabOrderOfTheMainFlow pins the keyboard criterion (PRD #297,
// Accessibility): the whole flow works with the keyboard alone, in the expected
// order, and the focus is visible on every control of it. The tab order is read
// from the browser: a Tab key event through the DevTools protocol is a keyboard
// event, so :focus-visible applies as it does for a user.
func TestTabOrderOfTheMainFlow(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	// The downloads exist only after a run, and a disabled button is not
	// focusable, so the flow is read once an assessment has been made.
	loadExample(t, ctx, "aws")
	setTargets(t, ctx, "ovhcloud")
	runAssessment(t, ctx)

	if message := alertOf(t, ctx).Text; message != "" {
		t.Fatalf("the page shows the error %q, want the report", message)
	}

	if !eval[bool](t, ctx, recordFocus) {
		t.Fatal("the focus recorder could not be installed")
	}

	// The keyboard walks the page from the top; the walk stops at the last
	// control of the flow, so no element is recorded twice.
	pressed := 0

	for range 40 {
		do(t, ctx, chromedp.KeyEvent(kb.Tab))
		pressed++

		if eval[string](t, ctx, `document.activeElement === null ? "" : (document.activeElement.id || document.activeElement.tagName.toLowerCase())`) == mainFlow[len(mainFlow)-1] {
			break
		}
	}

	steps := eval[[]focusStep](t, ctx, `globalThis.__focusOrder`)

	reached := make([]string, 0, len(steps))
	for _, step := range steps {
		if slices.Contains(mainFlow, step.ID) {
			reached = append(reached, step.ID)
		}
	}

	if !slices.Equal(reached, mainFlow) {
		t.Errorf("the keyboard reached the main flow's controls as %v after %d Tab presses, want %v: the whole flow works with the keyboard alone, in the page's order",
			reached, pressed, mainFlow)
	}

	for _, step := range steps {
		if !slices.Contains(mainFlow, step.ID) {
			continue
		}

		if !step.FocusVisible {
			t.Errorf("the control %q does not match :focus-visible when the keyboard reached it, so a keyboard user cannot see where the focus is", step.ID)
		}

		if step.OutlineStyle == "none" || step.OutlineWidth == "0px" {
			t.Errorf("the control %q has no visible focus ring (outline-style %q, outline-width %q), want the stylesheet's outline",
				step.ID, step.OutlineStyle, step.OutlineWidth)
		}
	}
}

// TestControlsHaveAccessibleNames pins the label criterion (PRD #297,
// Accessibility) with the browser's own accessible-name computation: every
// control of the page in the accessibility tree has a name.
func TestControlsHaveAccessibleNames(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	result, err := chromedp.Call(ctx, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{})
	if err != nil {
		t.Fatalf("read the accessibility tree: %v", err)
	}

	// The roles a user controls the flow with.
	controls := map[string]bool{
		"textbox": true, "searchbox": true, "combobox": true, "checkbox": true,
		"button": true, "radio": true, "switch": true, "listbox": true,
	}

	checked := 0

	for _, node := range result.Nodes {
		if node.Ignored || node.Role == nil {
			continue
		}

		role := axValue(t, node.Role)
		if !controls[role] {
			continue
		}

		checked++

		if name := axValue(t, node.Name); strings.TrimSpace(name) == "" {
			t.Errorf("the page's %s control (DOM node %d) has no accessible name, want a label, an aria-label or its own text",
				role, node.BackendDOMNodeID)
		}
	}

	if checked < 12 {
		t.Fatalf("the accessibility tree holds %d controls, want every control of the main flow (%d): the names of the rest went unchecked",
			checked, len(mainFlow))
	}

	t.Logf("checked the accessible names of %d controls", checked)
}

// axValue returns a string value of the accessibility tree.
func axValue(t *testing.T, value *accessibility.Value) string {
	t.Helper()

	if value == nil || len(value.Value) == 0 {
		return ""
	}

	var s string

	if err := json.Unmarshal([]byte(value.Value), &s); err != nil {
		t.Fatalf("decode the accessibility value %s: %v", value.Value, err)
	}

	return s
}

// TestReportIsSemanticHTML pins the report's markup criterion (PRD #297,
// Accessibility) in the browser: the report is the HTML reporter's semantic
// element set — headings, a table with header cells and lists — and the page
// inserts no report table of its own.
func TestReportIsSemanticHTML(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	loadExample(t, ctx, "aws")
	setTargets(t, ctx, "ovhcloud")
	runAssessment(t, ctx)

	report := reportOf(t, ctx)

	if !report.hasTag("section") {
		t.Errorf("the report holds no <section> element: it is the HTML reporter's semantic fragment, not text assembled by the script")
	}

	headings := eval[int](t, ctx, `document.querySelectorAll("#report h2, #report h3, #report h4, #report h5").length`)
	if headings < 3 {
		t.Errorf("the report holds %d headings, want the report's title and its sections as headings", headings)
	}

	if scoped := eval[int](t, ctx, `document.querySelectorAll("#report table thead th[scope], #report table th[scope]").length`); scoped == 0 {
		t.Error("the report's tables hold no header cells with a scope, want the reporter's semantic tables")
	}

	if lists := eval[int](t, ctx, `document.querySelectorAll("#report ul, #report ol").length`); lists == 0 {
		t.Error("the report holds no list, want the reporter's lists of blockers and required changes")
	}

	if built := eval[int](t, ctx, `document.querySelectorAll("#report div[class], #report table[data-report]").length`); built != 0 {
		t.Errorf("the report holds %d elements the script could have built, want only the reporter's own", built)
	}
}
