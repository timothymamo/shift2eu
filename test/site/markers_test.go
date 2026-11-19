package site_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// This file holds the checks of the statements the pages carry and of the
// controls a user works with: the markers PRD #297 requires (downgrade-risk,
// hsts-waived, loading-notice) must be present, non-empty and shown, "shown"
// meaning exactly what the Privacy statement's Hidden definition says; the
// no-JavaScript message must name JavaScript, WebAssembly and the CLI; every
// control must have a label; and an element with role="alert" must exist so an
// error is announced.

// TestDowngradeRiskIsShown pins the first-visit downgrade statement (PRD #297,
// Privacy statement): the privacy page carries the element whose id is
// downgrade-risk, its text is not empty and nothing hides it, in the untagged
// build and in the waived one. Only a change made after the domain is on the
// HSTS preload list may remove it.
func TestDowngradeRiskIsShown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		bundle      *bundle
		page        string
		marker      string
		shown       bool
		statement   string
		requirement string
	}{
		{
			name:        "downgrade-risk is shown on the privacy page",
			bundle:      theBundle(t),
			page:        "privacy.html",
			marker:      "downgrade-risk",
			shown:       true,
			statement:   "the first-visit downgrade risk",
			requirement: "the privacy page states the first-visit downgrade risk next to its nothing-leaves-the-browser statement until shift2eu.com is on the HSTS preload list in shipping browsers",
		},
		{
			name:        "downgrade-risk is shown on the privacy page of the waived build",
			bundle:      theTaggedBundle(t),
			page:        "privacy.html",
			marker:      "downgrade-risk",
			shown:       true,
			statement:   "the first-visit downgrade risk",
			requirement: "a waiver of HSTS does not remove the first-visit statement",
		},
		{
			name:        "loading-notice is shown on the assessment page",
			bundle:      theBundle(t),
			page:        "index.html",
			marker:      "loading-notice",
			shown:       true,
			statement:   "the static loading notice",
			requirement: "the HTML shows a static loading notice until the module is instantiated, so no script delays it",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkMarker(t, tc.bundle, tc.page, tc.marker, tc.shown, tc.statement, tc.requirement)
		})
	}
}

// TestHSTSStatementFollowsTheRecord pins the branch the HSTS record decides
// (PRD #297, Privacy statement): the HSTS record site/records/hsts.json is not
// in the repository yet, and a missing record means "not waived", so the default
// build must not show the hsts-waived statement; a build of a fixture copy whose
// record says waived must show it on the privacy page and on the verification
// page.
func TestHSTSStatementFollowsTheRecord(t *testing.T) {
	t.Parallel()

	t.Run("a missing record is not waived", func(t *testing.T) {
		t.Parallel()

		checkMarker(t, theBundle(t), "privacy.html", "hsts-waived", false,
			"the HSTS waiver statement",
			"site/records/hsts.json does not exist yet, and a missing record reads as not waived, so the page states no waiver")
		checkMarker(t, theBundle(t), "verify.html", "hsts-waived", false,
			"the HSTS waiver statement",
			"site/records/hsts.json does not exist yet, and a missing record reads as not waived, so the page states no waiver")
	})

	t.Run("a waived record is shown on both pages", func(t *testing.T) {
		t.Parallel()

		waived := theTaggedBundle(t)

		checkMarker(t, waived, "privacy.html", "hsts-waived", true,
			"the HSTS waiver statement",
			"the HSTS record says waived, so the privacy page states that without HSTS the downgrade risk covers returning visitors too")
		checkMarker(t, waived, "verify.html", "hsts-waived", true,
			"the HSTS waiver statement",
			"the HSTS record says waived, so the verification page states it too")
	})
}

// checkMarker pins one marked statement: when shown, the page must hold the
// element, its text must not be empty and nothing may hide it; when not shown,
// the page must hold no visible element of that id.
func checkMarker(t *testing.T, bundle *bundle, page, marker string, shown bool, statement, requirement string) {
	t.Helper()

	doc := bundle.html(t, page)
	_, stylesheet := bundle.asset(t, "style", ".css")

	rules := parseCSS(t, "stylesheet", stylesheet)
	props := customProperties(rules)

	var found []*element

	for _, el := range doc.all() {
		if id, ok := el.attr("id"); ok && id == marker {
			found = append(found, el)
		}
	}

	if !shown {
		for _, el := range found {
			if reason := hiddenReason(t, el, rules, props); reason == "" {
				t.Errorf("%s shows the element with id %q, but %s: %s", page, marker, requirement, statement)
			}
		}

		return
	}

	if len(found) == 0 {
		t.Errorf("%s holds no element with id %q: %s needs a stable marker, and %s", page, marker, statement, requirement)

		return
	}

	if len(found) > 1 {
		t.Errorf("%s holds %d elements with id %q, want one", page, len(found), marker)
	}

	el := found[0]

	if text := strings.TrimSpace(el.textContent()); text == "" {
		t.Errorf("%s's element with id %q holds no text: %s is a statement, and %s", page, marker, statement, requirement)
	}

	if reason := hiddenReason(t, el, rules, props); reason != "" {
		t.Errorf("%s's element with id %q is hidden because %s: %s must be shown by default", page, marker, reason, statement)
	}
}

// TestHiddenReasonFiresOnAHiddenMarker pins the Hidden definition in both
// directions on synthetic pages, so a marker check cannot pass vacuously: a
// checkMarker that answered "shown" to everything would let a hidden statement
// through. Every construct PRD #297's Privacy statement lists as hidden is
// reported, and a plainly shown statement is not. The synthetic pages keep this
// test independent of the built bundle, which is what checkMarker reads.
func TestHiddenReasonFiresOnAHiddenMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		stylesheet string
		hidden     bool
	}{
		{
			name:   "a statement in plain markup is shown",
			source: `<body><p id="marker">A statement.</p></body>`,
		},
		{
			name:   "the hidden attribute on the statement hides it",
			source: `<body><p id="marker" hidden>A statement.</p></body>`,
			hidden: true,
		},
		{
			name:   "the hidden attribute on an ancestor hides it",
			source: `<body><div hidden><p id="marker">A statement.</p></div></body>`,
			hidden: true,
		},
		{
			name:   "aria-hidden=\"true\" on an ancestor hides it",
			source: `<body><section aria-hidden="true"><p id="marker">A statement.</p></section></body>`,
			hidden: true,
		},
		{
			name:   "a closed details ancestor hides it",
			source: `<body><details><p id="marker">A statement.</p></details></body>`,
			hidden: true,
		},
		{
			name:   "an open details ancestor does not hide it",
			source: `<body><details open><p id="marker">A statement.</p></details></body>`,
		},
		{
			name:       "a rule on the statement's id setting display: none hides it",
			source:     `<body><p id="marker">A statement.</p></body>`,
			stylesheet: `#marker { display: none; }`,
			hidden:     true,
		},
		{
			name:       "a rule on an ancestor's class setting visibility: hidden hides it",
			source:     `<body><div class="notice"><p id="marker">A statement.</p></div></body>`,
			stylesheet: `.notice { visibility: hidden; }`,
			hidden:     true,
		},
		{
			name:       "a rule on an ancestor setting a zero height hides it",
			source:     `<body><div class="fold"><p id="marker">A statement.</p></div></body>`,
			stylesheet: `.fold { height: 0; }`,
			hidden:     true,
		},
		{
			name:       "a rule that matches nothing leaves it shown",
			source:     `<body><p id="marker">A statement.</p></body>`,
			stylesheet: `.other { display: none; }`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := parseHTML(t, "synthetic.html", []byte(tc.source))

			var marker *element

			for _, el := range doc.all() {
				if id, ok := el.attr("id"); ok && id == "marker" {
					marker = el
				}
			}

			if marker == nil {
				t.Fatalf("the reader found no element with id %q in %q, so this check would test nothing", "marker", tc.source)
			}

			rules := parseCSS(t, "stylesheet", []byte(tc.stylesheet))
			reason := hiddenReason(t, marker, rules, customProperties(rules))

			switch {
			case tc.hidden && reason == "":
				t.Errorf("hiddenReason reports nothing about %q, but PRD #297's Privacy statement counts it as hidden: a marker behind it must not pass as shown", tc.source)
			case !tc.hidden && reason != "":
				t.Errorf("hiddenReason reports %q about %q, but nothing hides the statement there", reason, tc.source)
			}
		})
	}
}

// hiddenReason returns why el is hidden, as PRD #297's Privacy statement defines
// Hidden, and "" when nothing hides it: a hidden or aria-hidden attribute on the
// element or an ancestor, a closed <details> ancestor, or a stylesheet rule that
// matches the element or an ancestor and sets display: none, visibility: hidden,
// a zero width or height, or a position off-screen.
func hiddenReason(t *testing.T, el *element, rules []cssRule, props map[string]string) string {
	t.Helper()

	for _, node := range append([]*element{el}, el.ancestors()...) {
		if node.hasAttr("hidden") {
			return fmt.Sprintf("the <%s> element carries the hidden attribute", node.tag)
		}

		if aria, ok := node.attr("aria-hidden"); ok && strings.EqualFold(strings.TrimSpace(aria), "true") {
			return fmt.Sprintf("the <%s> element carries aria-hidden=\"true\"", node.tag)
		}

		if node.tag == "details" && !node.hasAttr("open") {
			return "a closed <details> element contains it"
		}
	}

	for _, rule := range rules {
		for _, selector := range rule.selectors {
			if !selectorMatches(selector, el) {
				continue
			}

			for _, decl := range rule.decls {
				if reason := hidingDeclaration(t, decl, props); reason != "" {
					return fmt.Sprintf("the stylesheet's rule %q sets %s", strings.Join(rule.selectors, ", "), reason)
				}
			}
		}
	}

	return ""
}

// selectorMatches reports whether the subject of selector, the last compound,
// matches el or one of its ancestors: by id, by class, by tag name, by the
// universal selector or by [hidden]. Pseudo-classes are ignored and a
// pseudo-element matches nothing; a part this reader does not match leaves the
// compound matching nothing.
func selectorMatches(selector string, el *element) bool {
	compound := selector

	if at := strings.LastIndexAny(compound, " >+~"); at >= 0 {
		compound = compound[at+1:]
	}

	compound = strings.TrimSpace(compound)
	if compound == "" || strings.Contains(compound, "::") {
		return false
	}

	// Pseudo-classes carry no name of the element's own: drop them, with
	// their arguments.
	for {
		at := strings.Index(compound, ":")
		if at < 0 {
			break
		}

		end := at + 1
		for end < len(compound) && (isSelectorNameByte(compound[end]) || compound[end] == '-') {
			end++
		}

		if end < len(compound) && compound[end] == '(' {
			depth := 0

			for end < len(compound) {
				if compound[end] == '(' {
					depth++
				}

				if compound[end] == ')' {
					depth--

					if depth == 0 {
						end++

						break
					}
				}

				end++
			}
		}

		compound = compound[:at] + compound[end:]
	}

	for _, node := range append([]*element{el}, el.ancestors()...) {
		if compoundMatches(compound, node) {
			return true
		}
	}

	return false
}

// isSelectorNameByte reports whether b may appear in a selector name.
func isSelectorNameByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// compoundMatches reports whether one compound selector matches el.
func compoundMatches(compound string, el *element) bool {
	if compound == "" || compound == "*" {
		return true
	}

	rest := compound

	for len(rest) > 0 {
		switch {
		case strings.HasPrefix(rest, "[hidden]"):
			if !el.hasAttr("hidden") {
				return false
			}

			rest = rest[len("[hidden]"):]
		case strings.HasPrefix(rest, "#"):
			name, remaining := readSelectorName(rest[1:])

			if id, _ := el.attr("id"); id != name {
				return false
			}

			rest = remaining
		case strings.HasPrefix(rest, "."):
			name, remaining := readSelectorName(rest[1:])

			classes, _ := el.attr("class")
			if !slices.Contains(strings.Fields(classes), name) {
				return false
			}

			rest = remaining
		case strings.HasPrefix(rest, "["):
			// An attribute selector this reader does not match: fail closed.
			return false
		default:
			name, remaining := readSelectorName(rest)
			if name == "" || name != el.tag {
				return false
			}

			rest = remaining
		}
	}

	return true
}

// readSelectorName reads a selector's name at the front of s and returns it with
// the rest.
func readSelectorName(s string) (string, string) {
	i := 0

	for i < len(s) && (isSelectorNameByte(s[i]) || s[i] == '-') {
		i++
	}

	return s[:i], s[i:]
}

// hidingDeclaration returns what about the declaration hides an element, and ""
// when it hides nothing.
func hidingDeclaration(t *testing.T, decl cssDecl, props map[string]string) string {
	t.Helper()

	value := strings.ToLower(resolveValue(t, "stylesheet", decl.value, props))
	first := strings.Fields(value + " ")[0]

	switch decl.property {
	case "display":
		if first == "none" {
			return "display: none"
		}
	case "visibility":
		if first == "hidden" || first == "collapse" {
			return "visibility: " + first
		}
	case "width", "height", "max-width", "max-height", "inline-size", "block-size":
		if isZeroLength(value) {
			return decl.property + ": " + decl.value
		}
	case "left", "top", "right", "bottom", "margin-left", "margin-top", "text-indent":
		if isNegativeLength(value) {
			return decl.property + ": " + decl.value
		}
	case "transform":
		if strings.Contains(value, "translate") && strings.Contains(value, "-") {
			return "transform: " + decl.value
		}
	case "clip-path", "clip":
		if strings.Contains(value, "inset(50%)") || strings.Contains(value, "rect(0") {
			return decl.property + ": " + decl.value
		}
	}

	return ""
}

// isZeroLength reports whether value is a zero length, with or without a unit.
func isZeroLength(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))

	if value == "0" {
		return true
	}

	for _, unit := range []string{"px", "rem", "em", "%", "vh", "vw", "vmin", "vmax", "ch", "ex", "pt", "cm", "mm", "in"} {
		if value == "0"+unit {
			return true
		}
	}

	return false
}

// isNegativeLength reports whether value is a negative length.
func isNegativeLength(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))

	return strings.HasPrefix(value, "-") && value != "-0" && !strings.HasPrefix(value, "-0px")
}

// TestNoScriptMessage pins the message the assessment page shows without
// JavaScript (PRD #297, Page content): a <noscript> element that says the
// assessment needs JavaScript and WebAssembly and points to the CLI.
func TestNoScriptMessage(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	noscripts := bundle.html(t, "index.html").find("noscript")

	if len(noscripts) != 1 {
		t.Fatalf("index.html holds %d <noscript> elements, want exactly one: without JavaScript the page says what the assessment needs", len(noscripts))
	}

	message := strings.ToLower(noscripts[0].textContent())

	for _, want := range []string{"javascript", "webassembly", "cli"} {
		if !strings.Contains(message, want) {
			t.Errorf("index.html's <noscript> message %q does not name %q: without JavaScript the page says the assessment needs JavaScript and WebAssembly and points to the CLI",
				strings.Join(strings.Fields(message), " "), want)
		}
	}
}

// TestControlsHaveLabels pins that every control on the assessment page has a
// label an assistive technology reads (PRD #297, Accessibility): a <label>
// element around it, a <label for> its id, an aria-label, or an aria-labelledby
// that names existing elements; a button's own text counts as its label too. A
// hidden input needs none.
func TestControlsHaveLabels(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")

	ids := make(map[string]bool, 32)
	labelFor := make(map[string]bool, 16)

	for _, el := range doc.all() {
		if id, ok := el.attr("id"); ok {
			ids[id] = true
		}

		if el.tag == "label" {
			if forID, ok := el.attr("for"); ok {
				labelFor[strings.TrimSpace(forID)] = true
			}
		}
	}

	controls := 0

	for _, el := range doc.all() {
		switch el.tag {
		case "input", "select", "textarea", "button":
		default:
			continue
		}

		if el.tag == "input" {
			if kind, _ := el.attr("type"); strings.EqualFold(strings.TrimSpace(kind), "hidden") {
				continue
			}
		}

		controls++

		if controlLabel(t, el, labelFor, ids) != "" {
			continue
		}

		t.Errorf("index.html's <%s> control %s has no label: give it a <label for> its id, wrap it in a <label>, or name it with aria-label or aria-labelledby (PRD #297, Accessibility)",
			el.tag, describeControl(el))
	}

	if controls == 0 {
		t.Error("index.html holds no input, select, textarea or button, so the check found no control: the page takes an inventory, targets, a region and two switches")
	}
}

// controlLabel returns why the control has a label, and "" when it has none.
func controlLabel(t *testing.T, control *element, labelFor, ids map[string]bool) string {
	t.Helper()

	for _, ancestor := range control.ancestors() {
		if ancestor.tag == "label" {
			return "wrapped in a label"
		}
	}

	if id, ok := control.attr("id"); ok && labelFor[id] {
		return "labelled by a <label for>"
	}

	if label, ok := control.attr("aria-label"); ok && strings.TrimSpace(label) != "" {
		return "named by aria-label"
	}

	if namedBy, ok := control.attr("aria-labelledby"); ok {
		names := strings.Fields(namedBy)
		if len(names) != 0 && !slices.ContainsFunc(names, func(name string) bool { return !ids[name] }) {
			return "named by aria-labelledby"
		}
	}

	if control.tag == "button" && strings.TrimSpace(control.textContent()) != "" {
		return "named by its own text"
	}

	return ""
}

// describeControl names a control in a failure message: its id, its name or its
// element, whichever the page gives.
func describeControl(control *element) string {
	if id, ok := control.attr("id"); ok {
		return "id " + id
	}

	if name, ok := control.attr("name"); ok {
		return "name " + name
	}

	if label, ok := control.attr("aria-label"); ok {
		return "with aria-label " + label
	}

	return "with no id and no name"
}

// TestErrorsAreAnnounced pins that the assessment page holds an element with
// role="alert", so an error the page shows is announced to assistive technology
// (PRD #297, Accessibility).
func TestErrorsAreAnnounced(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")

	alerts := 0

	for _, el := range doc.all() {
		if role, ok := el.attr("role"); ok && strings.EqualFold(strings.TrimSpace(role), "alert") {
			alerts++
		}
	}

	if alerts == 0 {
		t.Error("index.html holds no element with role=\"alert\": the error an assessment returns is announced to assistive technology")
	}
}
