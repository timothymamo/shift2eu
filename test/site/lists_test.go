package site_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// This file holds the checks of the pages' lists (PR #379 review, issue #373): a
// list whose markers the stylesheet hides or replaces is announced as a list, and
// no marker is a quoted string a screen reader would read aloud.
//
// Safari and VoiceOver drop the list role, and with it the item count, of a
// `ul`/`ol` whose markers are hidden, so a stripped list needs `role="list"` to
// stay a list. A quoted marker (`list-style-type: "\25B8"`) is worse than a
// stripped one: the browser draws the string as text, and no empty alternative
// text can be stated beside it, so a screen reader reads the glyph aloud. The
// pages draw such a glyph with a `::before` box whose `content` states `/ ""`
// instead, and keep the marker off.
//
// A list whose markers a rule takes away also passes that state on: list-style is
// inherited, so a `ul` the report nests inside a markerless item inherits
// `list-style: none` unless a rule whose subject is the nested list itself states
// a visible marker.

// TestListsWhoseMarkersTheStylesheetHidesAreAnnounced pins that every `ul` and
// `ol` of every page whose markers a stylesheet rule hides or replaces carries
// `role="list"`, and names the lists the pages hold: the navigation of all three
// pages (`.nav`), the assessment page's chips (`.chips`), the privacy page's
// layers (`ol.layers`) and caveats (`ul.caveats`, whose items the stylesheet
// strips) and the verification page's steps (`ol.steps`). The named lists are
// pinned in the table below, so a stylesheet change that stops hiding a list's
// markers still fails here until the role and the stylesheet agree, and the sweep
// over the stylesheet catches a list that is added later.
func TestListsWhoseMarkersTheStylesheetHidesAreAnnounced(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	name, data := bundle.asset(t, "style", ".css")
	rules := parseCSS(t, name, data)

	// The lists each page holds whose markers the stylesheet hides, named by
	// their element and class, with the rule that hides them.
	want := map[string][]pinnedList{
		"index.html": {
			{tag: "ul", class: "nav", why: "the stylesheet's `.nav` rule sets list-style: none"},
			{tag: "ul", class: "chips", why: "the stylesheet's `.chips` rule sets list-style: none"},
		},
		"privacy.html": {
			{tag: "ul", class: "nav", why: "the stylesheet's `.nav` rule sets list-style: none"},
			{tag: "ol", class: "layers", why: "the stylesheet's `ol.layers` rule sets list-style: none"},
			{tag: "ul", class: "caveats", why: "the stylesheet's `ul.caveats > li` rule takes the items' markers away"},
		},
		"verify.html": {
			{tag: "ul", class: "nav", why: "the stylesheet's `.nav` rule sets list-style: none"},
			{tag: "ol", class: "steps", why: "the stylesheet's `ol.steps` rule sets list-style: none"},
		},
	}

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			// The pinned lists must all be found by the sweep, or this check
			// would read a page whose stylesheet changed under it.
			var found []*element

			for _, list := range listsOf(bundle.html(t, page)) {
				reasons := markerHidingRules(rules, list)
				if len(reasons) == 0 {
					continue
				}

				found = append(found, list)

				if role, _ := list.attr("role"); !strings.EqualFold(strings.TrimSpace(role), "list") {
					t.Errorf("%s's %s hides its markers (%s) but carries no role=\"list\": Safari and VoiceOver drop the list role and the item count of a list whose list-style is none, so an unannounced stripped list is read as plain text (PR #379 review, issue #373, Accessibility)",
						page, describeElement(list), strings.Join(reasons, "; "))
				}
			}

			for _, pin := range want[page] {
				if !slices.ContainsFunc(found, pin.matches) {
					t.Errorf("%s holds no %s.%s among the lists this check found whose markers the stylesheet hides (%v), but %s: the list is announced as a list, so the sweep and the stylesheet must agree on it (PR #379 review, issue #373)",
						page, pin.tag, pin.class, listNames(found), pin.why)
				}
			}
		})
	}
}

// TestNoListMarkerIsAQuotedString pins that no rule of the stylesheet states a
// marker as a string, in `list-style-type` or in the `list-style` shorthand (PR
// #379 review, issue #373): a quoted marker cannot carry an alternative text, so
// a screen reader reads the glyph aloud. The glyph is drawn with a `::before`
// box whose `content` states `/ ""` instead, and the list keeps `role="list"`.
func TestNoListMarkerIsAQuotedString(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")

	for _, rule := range parseCSS(t, name, data) {
		for _, property := range []string{"list-style", "list-style-type"} {
			value, ok := rule.decl(property)
			if !ok || !strings.ContainsAny(value, `"'`) {
				continue
			}

			t.Errorf("%s's rule %q sets %s: %s, a marker stated as a string: a screen reader reads the glyph aloud and no alternative text can be stated beside it, so draw it with a ::before box whose content is `\"<glyph>\" / \"\"` and keep the marker off with list-style: none (PR #379 review, issue #373, Accessibility)",
				name, strings.Join(rule.selectors, ", "), property, value)
		}
	}
}

// TestNestedListsInTheReportKeepTheirMarkers pins that the stylesheet states a
// visible marker on a list nested in a Blockers or a Cost estimate item (PR #379
// review, issue #373). list-style is inherited: `#report .part-blockers > ul > li`
// and `#report .part-cost > ul` state `list-style: none`, so a nested `ul` -- the
// `ul` the reporter writes inside such an item for a blocker's per-target reasons
// or an estimate's basis (fragment.go's writeList) -- inherits none, and a list
// with no markers and no role="list" loses its list role and its item count in
// Safari and VoiceOver. Each item's glyph is drawn by a `::before` box whose
// selector keeps the child combinator `> ul > li`, so it stays off the nested
// items and the nested list needs a marker of its own.
//
// The fragment below is the markup the htmlreport writes around a nested list, as
// testdata/comparison.golden shows it for the overview's "Blockers on every
// target"; no golden holds a nested list inside a `.part-blockers` or a
// `.part-cost` part yet, so the fragment stands in for the report's shape there.
func TestNestedListsInTheReportKeepTheirMarkers(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")
	rules := parseCSS(t, name, data)

	doc := parseHTML(t, "report-fragment.html", []byte(nestedListFragment))

	tests := []struct {
		name string
		// part is the report part the nested list sits in, by class.
		part string
		// why names the rule that hides the markers the nested list inherits.
		why string
		// glyphSelector is the item glyph rule that must not reach the nested
		// items, and "" for a part whose items draw no glyph.
		glyphSelector string
	}{
		{
			name:          "a blocker's nested list",
			part:          "part-blockers",
			why:           "`#report .part-blockers > ul > li { list-style: none }` takes the items' markers away",
			glyphSelector: "#report .part-blockers > ul > li",
		},
		{
			name: "the cost estimate's nested list",
			part: "part-cost",
			why:  "`#report .part-cost > ul { list-style: none }` takes the list's markers away",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nested := nestedListOf(t, doc, tc.part)

			if tc.glyphSelector != "" {
				for _, item := range nestedItemsOf(t, nested) {
					if selectorChainMatches(tc.glyphSelector, item) {
						t.Errorf("%s's rule %q reaches the %s part's nested items too, want its child combinator to keep the item glyph off them: a glyph on every nested item would stand where the marker this check asks for belongs (PR #379 review, issue #373)",
							name, tc.glyphSelector, tc.part)
					}
				}
			}

			statements := markerStatements(rules, nested)

			if len(statements) == 0 {
				t.Errorf("%s states no list-style or list-style-type on a rule whose subject is the %s of the %s part's nested list, so the nested list inherits list-style: none (%s) and loses its markers: a list without markers and without role=\"list\" loses its list role and its item count in Safari and VoiceOver, so state a marker on the nested list itself, as in `#report .%s li ul { list-style: disc; }` (PR #379 review, issue #373)",
					name, describeElement(nested), tc.part, tc.why, tc.part)

				return
			}

			for _, statement := range statements {
				if why := markerReplacement(statement.value); why != "" {
					t.Errorf("%s's rule `%s { %s: %s }` applies to the %s part's nested list and %s, so the nested list stays markerless: a list without markers and without role=\"list\" loses its list role and its item count in Safari and VoiceOver (PR #379 review, issue #373)",
						name, statement.selector, statement.property, statement.value, tc.part, why)
				}
			}
		})
	}
}

// nestedListFragment is the markup the reporter writes around a nested list: a
// plain `<ul>` inside an item of a Blockers or a Cost estimate list, as
// fragment.go's writeList writes it.
const nestedListFragment = `<div id="report">` +
	`<section class="part part-blockers"><h3>Blockers</h3><ul role="list">` +
	`<li><code>fn-example</code>: no offering in the catalog` +
	`<ul><li><code>OVHcloud</code>: no offering in the <code>OVHcloud</code> catalog</li></ul></li>` +
	`</ul></section>` +
	`<section class="part part-cost"><h3>Cost estimate</h3><ul role="list">` +
	`<li class="total">Total: €45.00 per month for 3 of 4 resources</li>` +
	`<li>Hours: plan price × count × hours / 730` +
	`<ul><li>A resource that states no hours counts 730</li></ul></li>` +
	`</ul></section>` +
	`</div>`

// markerStatement is one declaration of a rule that states the markers of a
// nested list itself.
type markerStatement struct {
	// selector is the rule's selector that matched the list.
	selector string
	// property is the declaration's property: list-style or list-style-type.
	property string
	// value is the declaration's value.
	value string
}

// markerStatements returns the declarations of the stylesheet's rules that state
// the markers of list itself, that is of a rule whose selector chain matches the
// list. A declaration on an item of the list is not one of them: a nested list
// inherits through the list and the item it sits in, so only a rule whose subject
// is the list itself beats the value it inherits.
func markerStatements(rules []cssRule, list *element) []markerStatement {
	var statements []markerStatement

	for _, rule := range rules {
		for _, selector := range rule.selectors {
			if !selectorChainMatches(selector, list) {
				continue
			}

			for _, property := range []string{"list-style", "list-style-type"} {
				value, ok := rule.decl(property)
				if !ok {
					continue
				}

				statements = append(statements, markerStatement{selector: selector, property: property, value: value})
			}
		}
	}

	return statements
}

// nestedItemsOf returns the items of the nested list, failing the test when it
// holds none: the glyph check over them would then read nothing.
func nestedItemsOf(t *testing.T, list *element) []*element {
	t.Helper()

	var items []*element

	for _, child := range list.children {
		if child.tag == "li" {
			items = append(items, child)
		}
	}

	if len(items) == 0 {
		t.Fatalf("the synthetic report fragment's nested %s holds no item, so the glyph check over them would read nothing", list.tag)
	}

	return items
}

// nestedListOf returns the first list of doc's part of class part that sits
// inside an item of another list, which is where the reporter writes a blocker's
// per-target reasons and an estimate's basis, and fails the test when the part
// holds none.
func nestedListOf(t *testing.T, doc *document, part string) *element {
	t.Helper()

	var section *element

	for _, el := range doc.all() {
		if el.tag == "section" && hasClass(el, "part") && hasClass(el, part) {
			section = el

			break
		}
	}

	if section == nil {
		t.Fatalf("the synthetic report fragment holds no section of the %q part class, so the check would read nothing", part)
	}

	for _, list := range listsOf(doc) {
		if list.parent != nil && list.parent.tag == "li" && isDescendant(list, section) {
			return list
		}
	}

	t.Fatalf("the synthetic report fragment holds no list nested in an item of its %s part, so the check would read nothing", part)

	return nil
}

// TestElementSelectorMatches pins the selector match the list sweep uses, in both
// directions, so the sweep cannot pass vacuously: a rule whose subject is the
// list or one of its items matches, a rule of another list, of another page or of
// the report does not, and a selector this reader cannot read matches nothing.
func TestElementSelectorMatches(t *testing.T) {
	t.Parallel()

	const source = `<body><nav aria-label="Site"><ul class="nav"><li><a href="privacy.html">Privacy</a></li></ul></nav>` +
		`<div class="prose"><ul class="card caveats"><li>A caveat.</li></ul></div>` +
		`<ol class="layers"><li><strong>A layer.</strong></li></ol></body>`

	doc := parseHTML(t, "synthetic.html", []byte(source))

	nav := doc.find("ul")[0]
	caveats := doc.find("ul")[1]
	caveatItem := doc.find("li")[1]
	layerItem := doc.find("li")[2]

	tests := []struct {
		selector string
		element  *element
		want     bool
	}{
		{selector: ".nav", element: nav, want: true},
		{selector: ".nav", element: caveats, want: false},
		{selector: "ul.nav", element: nav, want: true},
		{selector: "ol.layers", element: doc.find("ol")[0], want: true},
		{selector: "section.layers", element: doc.find("ol")[0], want: false},
		{selector: "ul.caveats > li", element: caveatItem, want: true},
		{selector: "ul.caveats > li", element: layerItem, want: false},
		{selector: "div.prose ul.caveats > li", element: caveatItem, want: true},
		{selector: "li", element: layerItem, want: true},
		{selector: "ol.layers > li::marker", element: layerItem, want: false},
		{selector: "#report .part-blockers > ul > li", element: caveatItem, want: false},
		{selector: "#report .shift2eu-report > h2 + ul", element: caveats, want: false},
		{selector: "li:first-child", element: caveatItem, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.selector, func(t *testing.T) {
			t.Parallel()

			if got := selectorChainMatches(tc.selector, tc.element); got != tc.want {
				t.Errorf("selectorChainMatches(%q, <%s>) = %t, want %t", tc.selector, tc.element.tag, got, tc.want)
			}
		})
	}
}

// pinnedList is one of a page's lists whose markers the stylesheet hides: how
// the check names it, and the rule that hides them.
type pinnedList struct {
	// tag is the list's element: "ul" or "ol".
	tag string
	// class is one class the list carries.
	class string
	// why is the stylesheet rule that hides the list's markers.
	why string
}

// matches reports whether list is the pinned list.
func (p pinnedList) matches(list *element) bool {
	return list.tag == p.tag && hasClass(list, p.class)
}

// listsOf returns the `ul` and `ol` elements of doc, in document order.
func listsOf(doc *document) []*element {
	var lists []*element

	for _, el := range doc.all() {
		if el.tag == "ul" || el.tag == "ol" {
			lists = append(lists, el)
		}
	}

	return lists
}

// listName names a list by its element and its classes, such as "ul.nav", so a
// page's lists are pinned without pinning their order.
func listName(list *element) string {
	classes, _ := list.attr("class")

	name := list.tag
	for _, class := range strings.Fields(classes) {
		name += "." + class
	}

	return name
}

// listNames names lists by their element and classes, such as "ul.nav", sorted,
// for a failure message.
func listNames(lists []*element) []string {
	names := make([]string, 0, len(lists))

	for _, list := range lists {
		names = append(names, listName(list))
	}

	slices.Sort(names)

	return names
}

// markerHidingRules returns one reason per stylesheet rule that hides or replaces
// the markers of list or of its items: a rule whose subject matches the list, or
// a direct item of it, and whose `list-style` or `list-style-type` states none or
// a quoted string.
func markerHidingRules(rules []cssRule, list *element) []string {
	items := make([]*element, 0, len(list.children))

	for _, child := range list.children {
		if child.tag == "li" {
			items = append(items, child)
		}
	}

	var reasons []string

	for _, rule := range rules {
		for _, selector := range rule.selectors {
			if !selectorChainMatches(selector, list) && !slices.ContainsFunc(items, func(item *element) bool {
				return selectorChainMatches(selector, item)
			}) {
				continue
			}

			for _, property := range []string{"list-style", "list-style-type"} {
				value, ok := rule.decl(property)
				if !ok {
					continue
				}

				if why := markerReplacement(value); why != "" {
					reasons = append(reasons, fmt.Sprintf("`%s { %s: %s }` %s", selector, property, value, why))
				}
			}
		}
	}

	return reasons
}

// markerReplacement returns why value hides or replaces a list's markers, and ""
// when it leaves a marker of the browser's own in place: none, and a quoted
// string, which the browser draws as text a screen reader reads.
func markerReplacement(value string) string {
	if strings.Fields(strings.ToLower(value + " "))[0] == "none" {
		return "hides the markers"
	}

	if strings.ContainsAny(value, `"'`) {
		return "replaces the markers with a quoted string"
	}

	return ""
}

// selectorChainMatches reports whether selector's chain of compounds matches el,
// reading the descendant and child combinators by walking the element's parents.
// It fails closed: a selector with any other combinator, or with a compound this
// reader does not read (a pseudo-class, a pseudo-element, an attribute selector
// other than [hidden]), matches nothing.
func selectorChainMatches(selector string, el *element) bool {
	compounds, combinators, ok := splitSelectorChain(selector)
	if !ok {
		return false
	}

	return matchChain(compounds, combinators, len(compounds)-1, el)
}

// matchChain reports whether the compounds from index i leftwards match el and
// its ancestors, with the combinators between them.
func matchChain(compounds, combinators []string, i int, el *element) bool {
	if !compoundMatches(compounds[i], el) {
		return false
	}

	if i == 0 {
		return true
	}

	switch combinators[i-1] {
	case " ":
		for ancestor := el.parent; ancestor != nil && ancestor.tag != "#document"; ancestor = ancestor.parent {
			if matchChain(compounds, combinators, i-1, ancestor) {
				return true
			}
		}

		return false
	case ">":
		return el.parent != nil && el.parent.tag != "#document" && matchChain(compounds, combinators, i-1, el.parent)
	default:
		return false
	}
}

// splitSelectorChain splits selector into its compounds and the combinators
// between them: "ol.layers > li" is ["ol.layers", "li"] and [">"], and "div p" is
// ["div", "p"] and [" "]. It reports false for a selector that is empty, that
// starts or ends with a combinator, or that has two of them in a row.
func splitSelectorChain(selector string) (compounds, combinators []string, ok bool) {
	var tokens []string

	var current strings.Builder

	flush := func() {
		if compound := strings.TrimSpace(current.String()); compound != "" {
			tokens = append(tokens, compound)
		}

		current.Reset()
	}

	depth := 0

	for i := 0; i < len(selector); i++ {
		c := selector[i]

		switch {
		case c == '[' || c == '(':
			depth++
		case c == ']' || c == ')':
			if depth == 0 {
				return nil, nil, false
			}

			depth--
		case depth == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			flush()

			continue
		case depth == 0 && (c == '>' || c == '+' || c == '~'):
			flush()
			tokens = append(tokens, string(c))

			continue
		}

		current.WriteByte(c)
	}

	flush()

	expectCompound := true

	for _, token := range tokens {
		switch token {
		case ">", "+", "~":
			if expectCompound {
				return nil, nil, false
			}

			combinators = append(combinators, token)
			expectCompound = true
		default:
			if !expectCompound {
				// Two compounds with nothing between them: white space is
				// the descendant combinator.
				combinators = append(combinators, " ")
			}

			compounds = append(compounds, token)
			expectCompound = false
		}
	}

	if len(compounds) == 0 || expectCompound || len(combinators) != len(compounds)-1 {
		return nil, nil, false
	}

	return compounds, combinators, true
}
