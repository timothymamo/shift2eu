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
// page's footer must carry the site's privacy promise; every control must have a
// label, a step number must be hidden from assistive technology, the report's
// glyphs must state an empty alternative text, and an element with role="alert"
// must exist so an error is announced.

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
// (PRD #297, Privacy statement): the committed HSTS record site/records/hsts.json
// says met with preload false, so the default build, which reads that record from
// the repository, must not show the hsts-waived statement; a build of a fixture
// copy whose record says waived must show it on the privacy page and on the
// verification page; and a build of a fixture copy with no records directory at
// all must show it on neither, because the builder reads a missing record as not
// waived (ci/sitebuild's readHSTSWaived, ADR 0042).
func TestHSTSStatementFollowsTheRecord(t *testing.T) {
	t.Parallel()

	t.Run("the committed met record is not waived", func(t *testing.T) {
		t.Parallel()

		checkMarker(t, theBundle(t), "privacy.html", "hsts-waived", false,
			"the HSTS waiver statement",
			"the committed site/records/hsts.json says met, so the page states no waiver")
		checkMarker(t, theBundle(t), "verify.html", "hsts-waived", false,
			"the HSTS waiver statement",
			"the committed site/records/hsts.json says met, so the page states no waiver")
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

	t.Run("a missing record is not waived", func(t *testing.T) {
		t.Parallel()

		unrecorded := theUnrecordedBundle(t)

		checkMarker(t, unrecorded, "privacy.html", "hsts-waived", false,
			"the HSTS waiver statement",
			"the fixture holds no site/records/hsts.json, and the builder reads a missing record as not waived (ADR 0042), so the page states no waiver")
		checkMarker(t, unrecorded, "verify.html", "hsts-waived", false,
			"the HSTS waiver statement",
			"the fixture holds no site/records/hsts.json, and the builder reads a missing record as not waived (ADR 0042), so the page states no waiver")
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

	resolved, err := resolveValue("stylesheet", decl.value, props)
	if err != nil {
		t.Fatalf("the checks read no value for %s: %v", decl.property, err)
	}

	value := strings.ToLower(resolved)
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

// TestEveryPageCarriesTheFooterStatement pins the one line every page shares in
// its footer (issue #373, item 09): "No cookies, no tracking, no requests: the
// page cannot connect anywhere." The claim is the site's privacy promise, and the
// footer is the part a visitor reads on every page, so the build writes the same
// statement into each page's <footer>.
func TestEveryPageCarriesTheFooterStatement(t *testing.T) {
	t.Parallel()

	const statement = "No cookies, no tracking, no requests: the page cannot connect anywhere."

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			footer := collapseSpace(footerOf(t, bundle.html(t, page)).textContent())

			if !strings.Contains(footer, statement) {
				t.Errorf("%s's footer reads %q, want it to carry %q: every page states the site's privacy promise in its footer (issue #373, item 09)",
					page, footer, statement)
			}
		})
	}
}

// TestFootersLinkTheSiteAndShowTheVersion pins the rest of the footer (issue
// #373, item 09): the privacy and verification links the other pages already
// carry, and the about link every other page carries it as it carries those two
// (issue #528), and the version the build was given, never a hard-coded one. Both
// builds are read, so the check tells the untagged version from the tagged one.
// The version sits in the one element whose id is site-version (PRD #297 milestone 3,
// issue #346): the live site check reads the deployed version from it and matches
// it against the repository's tag list, so the text is the version alone, as the
// page renders it.
func TestFootersLinkTheSiteAndShowTheVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bundle  *bundle
		version string
	}{
		{name: "untagged", bundle: theBundle(t), version: devVersion(t)},
		{name: "tagged", bundle: theTaggedBundle(t), version: taggedVersion},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, page := range pageNames {
				t.Run(page, func(t *testing.T) {
					t.Parallel()

					doc := tc.bundle.html(t, page)
					footer := footerOf(t, doc)

					links := make(map[string]string, 4)

					for _, anchor := range footer.find("a") {
						href, _ := anchor.attr("href")
						links[href] = collapseSpace(anchor.textContent())
					}

					for href, want := range map[string]string{"privacy.html": "Privacy", "verify.html": "Verify this site"} {
						if got, ok := links[href]; !ok || got != want {
							t.Errorf("%s's footer links %q with the text %q, want a link to %s reading %q (issue #373, item 09)",
								page, href, got, href, want)
						}
					}

					// Every other page's footer links the about page, as it links the
					// privacy and verification pages (issue #528). The about page's own
					// footer needs no link to itself.
					if page != aboutPageName {
						if _, ok := links[aboutPageName]; !ok {
							t.Errorf("%s's footer holds no link to %s, want the about page in every other page's footer, as the privacy and verification pages are (issue #528)",
								page, aboutPageName)
						}
					}

					versionSpans := 0

					var versionElement *element

					for _, candidate := range doc.all() {
						if id, _ := candidate.attr("id"); id != "site-version" {
							continue
						}

						versionSpans++
						versionElement = candidate
					}

					if versionSpans != 1 {
						t.Fatalf("%s holds %d elements with id=\"site-version\", want exactly one: the live site check reads the deployed version from that element (PRD #297 milestone 3, issue #346)",
							page, versionSpans)
					}

					if !isDescendant(versionElement, footer) {
						t.Errorf("%s's element with id=\"site-version\" is not in the page's footer, want the footer's version span: every page shows the version there (issue #373, item 09; issue #346)", page)
					}

					if got := collapseSpace(versionElement.textContent()); got != tc.version {
						t.Errorf("%s's element with id=\"site-version\" reads %q, want %q: its text is the version alone, which the live site check matches against the tag list (issue #346)",
							page, got, tc.version)
					}

					text := collapseSpace(footer.textContent())

					if !strings.Contains(text, tc.version) {
						t.Errorf("%s's footer reads %q, want it to show the version %q: the footer shows the version the build was given, never a hard-coded one (issue #373, item 09)",
							page, text, tc.version)
					}

					if tc.version != "v0.10.0" && strings.Contains(text, "v0.10.0") {
						t.Errorf("%s's footer reads %q, want no hard-coded v0.10.0: the footer shows the version the build was given, %q (issue #373, item 09)",
							page, text, tc.version)
					}
				})
			}
		})
	}
}

// TestStepNumbersAreSilent pins that the numbers the assessment page's legends
// show are decoration and hidden from assistive technology (issue #373): a
// <legend> holds its number in a <span class="step" aria-hidden="true">, so a
// screen reader names the fieldset "Inventory", "EU targets" or "Options"
// rather than "01 Inventory".
func TestStepNumbersAreSilent(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")

	steps := 0

	for _, legend := range doc.find("legend") {
		for _, child := range legend.children {
			if child.tag != "span" || !hasClass(child, "step") {
				continue
			}

			steps++

			if got, ok := child.attr("aria-hidden"); !ok || strings.TrimSpace(got) != "true" {
				t.Errorf("a <legend> holds the step number %q in a <span class=\"step\"> with aria-hidden=%q (present: %t), want \"true\": the number is decoration, and the fieldset is named by the rest of the legend (issue #373, Accessibility)",
					collapseSpace(child.textContent()), got, ok)
			}
		}
	}

	if steps == 0 {
		t.Error("index.html holds no <legend> with a <span class=\"step\">, so the check read nothing: the assessment page's three fieldsets show their step number there")
	}
}

// TestNoMarkerPseudoElementDeclaresABackground pins what a ::marker may state
// (PR #379 review, issue #373): ::marker draws the text and the glyph of its own
// box and ignores a background, so a background declared on a ::marker rule
// reaches no visitor. Such a declaration is worse than dead: the contrast check
// then reads a foreground/background pair the browser never paints, and passes a
// marker whose glyph is unreadable against the surface it really sits on.
//
// One declared background that paints nothing is accepted, and the contrast check
// relies on it: `.mark-stars` and `.mark-arrow` are `<g>` groups of the brand's
// SVG, which draws its glyphs with `fill: currentColor` and `stroke:
// currentColor`, so their `background-color: var(--mast)` never paints. It is
// declared so the contrast test pairs each glyph's colour with the masthead the
// glyph sits on, `--mast` (light) or its dark-scheme value, rather than with the
// page's background the SVG is not drawn on. ::marker stays refused because there
// the glyph is drawn by the list item's own box, on the item's background: a
// background declared on the ::marker rule is false about that pair, so the pair
// it would teach the contrast check would mislead it.
func TestNoMarkerPseudoElementDeclaresABackground(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")

	for _, problem := range markerBackgrounds(parseCSS(t, name, data)) {
		t.Errorf("%s's rule %s: a ::marker pseudo-element ignores a background, so the declaration paints nothing and a contrast check over that pair measures a colour no browser draws: state only the glyph's colour on the ::marker rule, and draw the item's glyph with a ::before box when it needs a background (issue #373, PR #379 review)",
			name, problem)
	}
}

// TestMarkerBackgroundCheckFires pins that the ::marker background scan reports
// the declaration it exists for, so the check over the stylesheet cannot pass
// vacuously: a background on a ::before box is no problem, and neither is a
// colour on a ::marker.
func TestMarkerBackgroundCheckFires(t *testing.T) {
	t.Parallel()

	const source = `
li::marker { color: #111111; background-color: #ffffff; }
li::before { content: "\2715"; background: #ffffff; }
p::marker { color: #111111; }
`

	problems := markerBackgrounds(parseCSS(t, "synthetic.css", []byte(source)))

	if len(problems) != 1 {
		t.Fatalf("the scan reported %d problems for a stylesheet with one background on a ::marker, want exactly one: %v", len(problems), problems)
	}

	if want := "background-color"; !strings.Contains(problems[0], want) {
		t.Errorf("the scan reported %q, want it to name the %s declaration the ::marker rule makes", problems[0], want)
	}
}

// markerBackgrounds returns one problem for every background a rule whose
// selector names the ::marker pseudo-element declares, naming the rule's
// selectors and the declaration.
func markerBackgrounds(rules []cssRule) []string {
	var problems []string

	for _, rule := range rules {
		if !slices.ContainsFunc(rule.selectors, func(selector string) bool {
			return strings.Contains(strings.ToLower(selector), "::marker")
		}) {
			continue
		}

		for _, property := range []string{"background", "background-color"} {
			if value, ok := rule.decl(property); ok {
				problems = append(problems, fmt.Sprintf("%s declares %s: %s", strings.Join(rule.selectors, ", "), property, value))
			}
		}
	}

	return problems
}

// TestBlockerGlyphContentCarriesAnEmptyAlternative pins the decorated content the
// redesigned report draws (issue #373): a heading and a blocker item show a glyph,
// and the declaration states the empty alternative text `/ ""` beside it, so a
// screen reader announces the heading's words and nothing of the glyph. A glyph
// without an alternative is read aloud as text. The blocker item's glyph is a
// ::before box rather than its ::marker, because Safari ignores `content` on
// ::marker and falls back to a disc (PR #379 review). The privacy page's caveat
// triangle is drawn the same way: a quoted `list-style-type` replaces the marker
// with text no alternative text can be stated beside, so the glyph moves to a
// ::before box with `/ ""` and the items keep `list-style: none` (PR #379 review).
func TestBlockerGlyphContentCarriesAnEmptyAlternative(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")

	tests := []struct {
		name     string
		selector string
		glyph    string
	}{
		{name: "a blocker item's glyph", selector: "#report .part-blockers > ul > li::before", glyph: `\2715`},
		{name: "a blockers heading's glyph", selector: "#report .part-blockers > h3::before", glyph: `\2715`},
		{name: "a required change's heading glyph", selector: "#report .part-changes > h3::before", glyph: `\25d0`},
		{name: "a caveat item's triangle", selector: "ul.caveats > li::before", glyph: `\25B8`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content, found := declarationOf(t, name, data, tc.selector, "content")
			if !found {
				t.Fatalf("%s states no `content` for %q, want a glyph and its empty alternative text: `content: %s\u00a0/ \"\"` (issue #373)",
					name, tc.selector, tc.glyph)
			}

			glyph, alternative, ok := strings.Cut(content, "/")
			if !ok {
				t.Fatalf("%s's %q sets content: %s, want the glyph and an empty alternative text beside it: a screen reader reads a bare glyph aloud (issue #373)",
					name, tc.selector, content)
			}

			if !strings.Contains(strings.ToUpper(glyph), strings.ToUpper(tc.glyph)) {
				t.Errorf("%s's %q sets content: %s, want it to draw the glyph %s", name, tc.selector, content, tc.glyph)
			}

			if got := strings.TrimSpace(alternative); got != `""` {
				t.Errorf("%s's %q states the alternative text %s, want an empty `\"\"`: the glyph is decoration, and the heading's words are what a screen reader reads (issue #373, Accessibility)",
					name, tc.selector, got)
			}
		})
	}
}
