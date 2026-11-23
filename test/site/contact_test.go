package site_test

import (
	"slices"
	"strings"
	"testing"
)

// This file checks the two contact points the site's redesign puts on the
// assessment page (issue #373, the design brief's Contact section): a "Next
// steps" aside that follows the report and that CSS alone hides while the report
// is still empty, and a band before the footer. Both link the same address with a
// `mailto:` href, which opens the visitor's mail client and loads nothing, so the
// page sends nothing and the CSP's `default-src 'none'` still holds (ADR 0036).
//
// The markup is the page's, not the report fragment's: a link in the fragment
// would break the fragment's parity with the Markdown report, which is text.

// contactHref is the address both contact points link, spelled exactly as the
// page states it: the checks pin the address itself, so a redirect, a form or a
// tracking parameter cannot take its place.
const contactHref = "mailto:info@shift2eu.com"

// TestContactPoints pins the page's two contact points (issue #373): exactly two
// links to the contact address and no other mailto: link, the first inside the
// "Next steps" aside that follows the report, the second outside the report and
// the aside, before the footer.
func TestContactPoints(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")

	var contact []*element

	for _, anchor := range doc.find("a") {
		href, ok := anchor.attr("href")
		if !ok {
			continue
		}

		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "mailto:") {
			continue
		}

		if href != contactHref {
			t.Errorf("index.html links the mailto: address %q, want exactly %q: the page names one address and no other", href, contactHref)
		}

		contact = append(contact, anchor)
	}

	if len(contact) != 2 {
		t.Fatalf("index.html holds %d links to a mailto: address, want exactly two: the \"Next steps\" aside's and the band before the footer's (issue #373, Contact)",
			len(contact))
	}

	report := elementByID(t, doc, "report")
	aside := nextElementSibling(report)

	if aside == nil || aside.tag != "aside" || !hasClass(aside, "next-steps") {
		t.Fatalf("the element after #report is %s, want the <aside class=\"next-steps\"> contact card: #report:empty + .next-steps is what hides it until a run",
			describeElement(aside))
	}

	if !isDescendant(contact[0], aside) {
		t.Errorf("the first contact link is outside the <aside class=\"next-steps\">: the card's own button is the first of the page's two contact points")
	}

	if !strings.Contains(strings.ToLower(aside.textContent()), "next steps") {
		t.Errorf("the next-steps aside's text %q does not say \"Next steps\": the card is titled and says what it offers (issue #373, Contact)",
			collapseSpace(aside.textContent()))
	}

	if got := collapseSpace(contact[0].textContent()); !strings.EqualFold(got, "Contact us") {
		t.Errorf("the next-steps aside's link reads %q, want \"Contact us\"", got)
	}

	second := contact[1]

	if isDescendant(second, aside) || isDescendant(second, report) {
		t.Errorf("both contact links are inside %s, want the second one in the band before the footer", describeElement(aside))
	}

	if order := documentOrder(doc); order[second] < order[aside] || order[second] > order[footerOf(t, doc)] {
		t.Errorf("the second contact link does not sit between the next-steps card and the footer: want the band before the footer (issue #373, Contact)")
	}
}

// TestReportStaysEmptyUntilARun pins that #report holds nothing at all before an
// assessment runs: no element and no text, not even white space. The next-steps
// card is hidden with `#report:empty + .next-steps`, and `:empty` matches only an
// element with no child nodes and no text, so a newline between the report's tags
// would show the card on the page before there is a report to read (issue #373,
// Contact).
func TestReportStaysEmptyUntilARun(t *testing.T) {
	t.Parallel()

	report := elementByID(t, theBundle(t).html(t, "index.html"), "report")

	if len(report.children) != 0 {
		t.Errorf("#report holds %d child elements before a run, want none: page.js fills it, and a static child would be replaced by the report",
			len(report.children))
	}

	if report.text != "" {
		t.Errorf("#report holds the text %q before a run, want no text at all, white space included: :empty does not match an element with a text node, so `#report:empty + .next-steps` would never hide the card",
			report.text)
	}
}

// TestNextStepsIsHiddenWhileTheReportIsEmpty pins the one rule the page needs to
// show the next-steps card only once a report exists: no script takes part
// (page.js is unchanged), so the rule is stated in the stylesheet with a sibling
// combinator and `:empty`.
func TestNextStepsIsHiddenWhileTheReportIsEmpty(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")

	display, found := declarationOf(t, name, data, "#report:empty + .next-steps", "display")

	if !found {
		t.Fatalf("%s states no rule whose selector is %q, so the next-steps card shows before an assessment runs: hide it with `#report:empty + .next-steps { display: none; }` (issue #373, Contact)",
			name, "#report:empty + .next-steps")
	}

	if strings.ToLower(strings.TrimSpace(display)) != "none" {
		t.Errorf("%s's rule %q sets display: %q, want none: the card is hidden while the report is empty",
			name, "#report:empty + .next-steps", display)
	}
}

// declarationOf returns the value of property in the rule of the stylesheet src
// whose selector is selector, with the selector's white space ignored so
// `a + b` and `a+b` are the same rule, and whether such a rule states it.
func declarationOf(t *testing.T, name string, src []byte, selector, property string) (string, bool) {
	t.Helper()

	want := withoutSpace(selector)

	for _, rule := range parseCSS(t, name, src) {
		if !slices.ContainsFunc(rule.selectors, func(got string) bool {
			return withoutSpace(got) == want
		}) {
			continue
		}

		if value, ok := rule.decl(property); ok {
			return value, true
		}
	}

	return "", false
}

// withoutSpace returns selector without any white space, so two spellings of one
// selector compare equal.
func withoutSpace(selector string) string {
	return strings.Join(strings.Fields(selector), "")
}

// collapseSpace returns s with each run of white space as one space, trimmed.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// elementByID returns the one element of doc whose id is id, failing the test
// when the document holds none or more than one.
func elementByID(t *testing.T, doc *document, id string) *element {
	t.Helper()

	var found []*element

	for _, el := range doc.all() {
		if got, ok := el.attr("id"); ok && got == id {
			found = append(found, el)
		}
	}

	if len(found) != 1 {
		t.Fatalf("the page holds %d elements with id %q, want exactly one", len(found), id)
	}

	return found[0]
}

// nextElementSibling returns the element after el among its parent's children,
// which is the sibling a CSS combinator selects, or nil when el is the last
// one or the document's own node. Text between the two is no element and does
// not change which sibling follows.
func nextElementSibling(el *element) *element {
	if el.parent == nil {
		return nil
	}

	for i, sibling := range el.parent.children {
		if sibling != el {
			continue
		}

		if i+1 < len(el.parent.children) {
			return el.parent.children[i+1]
		}

		return nil
	}

	return nil
}

// hasClass reports whether el carries the class name in its class attribute.
func hasClass(el *element, name string) bool {
	classes, ok := el.attr("class")
	if !ok {
		return false
	}

	return slices.Contains(strings.Fields(classes), name)
}

// isDescendant reports whether el sits inside ancestor.
func isDescendant(el, ancestor *element) bool {
	return slices.Contains(el.ancestors(), ancestor)
}

// documentOrder returns each element of doc's position in the document, growing
// with source order.
func documentOrder(doc *document) map[*element]int {
	order := make(map[*element]int, len(doc.all()))

	for i, el := range doc.all() {
		order[el] = i
	}

	return order
}

// footerOf returns the page's one <footer>, failing the test when it holds none
// or more than one.
func footerOf(t *testing.T, doc *document) *element {
	t.Helper()

	footers := doc.find("footer")
	if len(footers) != 1 {
		t.Fatalf("the page holds %d <footer> elements, want exactly one", len(footers))
	}

	return footers[0]
}

// describeElement names an element and its classes in a failure message.
func describeElement(el *element) string {
	if el == nil {
		return "no element"
	}

	if classes, ok := el.attr("class"); ok {
		return "<" + el.tag + " class=\"" + classes + "\">"
	}

	return "<" + el.tag + ">"
}

// contactBand is the class of the band the assessment page carries before its
// footer: the maintainer's "footnote message", which asks whether the visitor
// wants help with the move and names the contact address.
const contactBand = "contact-band"

// TestEveryPageCarriesTheContactBand pins the maintainer's second review of the
// redesign (issue #373, item 06): the band that asks "Planning a migration?" and
// offers the contact address sat on the assessment page alone, and every page
// carries it now, with the same words, at the end of the page's content and
// before the footer.
func TestEveryPageCarriesTheContactBand(t *testing.T) {
	t.Parallel()

	const heading = "Questions about your results, or how to move forward?"

	bundle := theBundle(t)

	texts := make(map[string]string, len(pageNames))

	for _, page := range pageNames {
		doc := bundle.html(t, page)

		bands := 0

		for _, el := range doc.all() {
			if !hasClass(el, contactBand) {
				continue
			}

			bands++

			if order := documentOrder(doc); order[el] > order[footerOf(t, doc)] {
				t.Errorf("%s's contact band comes after the footer, want it before the footer, as the assessment page has it (issue #373, item 06)", page)
			}

			if mains := doc.find("main"); len(mains) == 1 && !isDescendant(el, mains[0]) {
				t.Errorf("%s's contact band sits outside <main>, want it at the end of the page's content (issue #373, item 06)", page)
			}

			text := collapseSpace(el.textContent())
			texts[page] = text

			if !strings.Contains(text, heading) {
				t.Errorf("%s's contact band reads %q, want it to ask %q, as the assessment page's does (issue #373, item 06)",
					page, excerptText(text, 200), heading)
			}

			links := 0

			for _, anchor := range el.find("a") {
				if href, _ := anchor.attr("href"); href == contactHref {
					links++
				}
			}

			if links != 1 {
				t.Errorf("%s's contact band holds %d links to %q, want exactly one: the band's button is how a visitor writes to the address (issue #373, item 06)",
					page, links, contactHref)
			}
		}

		if bands != 1 {
			t.Errorf("%s holds %d elements with the class %q, want exactly one: the maintainer's footnote message belongs on every page, not on the assessment page alone (issue #373, item 06)",
				page, bands, contactBand)
		}
	}

	for _, page := range pageNames {
		if texts[page] != "" && texts[page] != texts[pageNames[0]] {
			t.Errorf("%s's contact band reads %q, want the same words as %s's %q: every page carries the one band (issue #373, item 06)",
				page, excerptText(texts[page], 200), pageNames[0], excerptText(texts[pageNames[0]], 200))
		}
	}
}
