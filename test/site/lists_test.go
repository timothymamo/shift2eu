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
