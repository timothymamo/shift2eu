package site_test

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// contactHrefAssessment is the href of the assessment page's "Next steps" card
// (issue #585): the visitor has just read a report, so the letter it opens says
// so and offers the three things the card offers, and the two encoded CRLF line
// breaks after "Hello," keep the greeting on a line of its own.
const contactHrefAssessment = "mailto:info@shift2eu.com?subject=Help%20with%20my%20shift2eu%20assessment&body=Hello%2C%0D%0A%0D%0AI%20ran%20an%20assessment%20on%20shift2eu.com%20and%20would%20like%20help%20with%3A%0D%0A-%20reading%20the%20report%0D%0A-%20choosing%20an%20EU%20provider%0D%0A-%20planning%20the%20move%0D%0A%0D%0A%28Attach%20the%20downloaded%20report%20if%20you%27d%20like%20us%20to%20look%20at%20it.%29%0D%0A%0D%0AThanks%2C"

// contactHrefGeneral is the href of every other contact link of the site (issue
// #585): the visitor is asking a question, and the letter says only that, so the
// maintainer reads which of the two links brought the mail.
const contactHrefGeneral = "mailto:info@shift2eu.com?subject=Question%20about%20shift2eu&body=Hello%2C%0D%0A%0D%0AI%20have%20a%20question%20about"

// contactHrefs are the two hrefs every `mailto:` link of the site spells: the
// site names one address and two letters, and no third (issue #585).
var contactHrefs = []string{contactHrefAssessment, contactHrefGeneral}

// contactAddress is the address both hrefs write to, as the pages name it.
const contactAddress = "info@shift2eu.com"

// maxContactHrefLength is the longest a contact href may be: a mail client cuts
// a longer one, and some refuse it, so the letters stay short.
const maxContactHrefLength = 1500

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

		if href = htmlUnescape(strings.TrimSpace(href)); !slices.Contains(contactHrefs, href) {
			t.Errorf("index.html links the mailto: reference %q, want one of %q: the page spells one of the site's two contact hrefs and no other (issue #585)", href, contactHrefs)
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

	if href, _ := contact[0].attr("href"); htmlUnescape(strings.TrimSpace(href)) != contactHrefAssessment {
		t.Errorf("the \"Next steps\" card's link spells %q, want the assessment's letter %q: the visitor has a report to read, so the letter says so (issue #585)",
			htmlUnescape(strings.TrimSpace(href)), contactHrefAssessment)
	}

	if href, _ := contact[1].attr("href"); htmlUnescape(strings.TrimSpace(href)) != contactHrefGeneral {
		t.Errorf("the band's link spells %q, want the general question %q: the band asks whether the visitor wants help, not how the report reads (issue #585)",
			htmlUnescape(strings.TrimSpace(href)), contactHrefGeneral)
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
				if link := newContactLink(anchor); link.href == contactHrefGeneral {
					links++
				}
			}

			if links != 1 {
				t.Errorf("%s's contact band holds %d links to %q, want exactly one: the band's button is how a visitor writes to the address (issue #373, item 06, issue #585)",
					page, links, contactHrefGeneral)
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

// contactLink is one `mailto:` link of a page: the anchor that carries it and the
// href a browser reads from it, with the character references of the HTML
// decoded (`&amp;` between the subject and the body is `&`).
type contactLink struct {
	anchor *element
	href   string
}

// newContactLink reads one anchor as a contact link.
func newContactLink(anchor *element) contactLink {
	href, _ := anchor.attr("href")

	return contactLink{anchor: anchor, href: htmlUnescape(strings.TrimSpace(href))}
}

// contactLinks returns every `mailto:` link of page, in document order.
func contactLinks(t *testing.T, bundle *bundle, page string) []contactLink {
	t.Helper()

	var links []contactLink

	for _, anchor := range bundle.html(t, page).find("a") {
		if href, ok := anchor.attr("href"); ok && strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "mailto:") {
			links = append(links, newContactLink(anchor))
		}
	}

	return links
}

// contactHrefName names one of the site's two contact hrefs for a failure
// message, and quotes the href when it is neither.
func contactHrefName(href string) string {
	switch href {
	case contactHrefAssessment:
		return "the assessment's letter"
	case contactHrefGeneral:
		return "the general question"
	}

	return fmt.Sprintf("%q", href)
}

// TestEveryContactLinkSpellsOneOfTheTwoHrefs pins the site's six contact links
// (issue #585): the assessment page's "Next steps" card writes the letter that
// says the visitor has a report to read, every other link writes the general
// question, and no page spells a third href, an address alone, a redirect or a
// form the reader cannot read before sending. Each of the six carries the
// address as visible text beside its button, so the link says where it writes
// before it is followed, and each stays under the mail clients' length.
func TestEveryContactLinkSpellsOneOfTheTwoHrefs(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	tests := []struct {
		page string
		// want names each of the page's contact links' hrefs in document order,
		// as contactHrefName names it.
		want []string
	}{
		{page: "index.html", want: []string{"the assessment's letter", "the general question"}},
		{page: "about.html", want: []string{"the general question"}},
		{page: "inventory.html", want: []string{"the general question"}},
		{page: "privacy.html", want: []string{"the general question"}},
		{page: "verify.html", want: []string{"the general question"}},
	}

	for _, tc := range tests {
		t.Run(tc.page, func(t *testing.T) {
			t.Parallel()

			var got []string

			for _, link := range contactLinks(t, bundle, tc.page) {
				got = append(got, contactHrefName(link.href))

				if !slices.Contains(contactHrefs, link.href) {
					t.Errorf("%s links the mailto: reference %q, want one of the site's two contact hrefs: the site names one address and two letters, and no third (issue #585)",
						tc.page, link.href)
				}

				if len(link.href) >= maxContactHrefLength {
					t.Errorf("%s's contact link is %d characters long, want fewer than %d: a mail client cuts or refuses a longer mailto: reference (issue #585)",
						tc.page, len(link.href), maxContactHrefLength)
				}

				if !namesTheContactAddress(link.anchor) {
					t.Errorf("%s's contact link reads %q and the element around it %q, want the address %s written beside every button: the page says where a link writes before it is followed (issue #585)",
						tc.page, collapseSpace(link.anchor.textContent()), collapseSpace(parentText(link.anchor)), contactAddress)
				}
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("%s's contact links, in document order, differ from the site's two hrefs (-want +got):\n%s", tc.page, diff)
			}
		})
	}
}

// namesTheContactAddress reports whether the anchor or the element that holds it
// writes the contact address as text, which is what tells a reader where the
// button writes before they follow it.
func namesTheContactAddress(anchor *element) bool {
	return strings.Contains(strings.ToLower(anchor.textContent()), contactAddress) ||
		strings.Contains(strings.ToLower(parentText(anchor)), contactAddress)
}

// parentText returns the text content of the element that holds el, or "" when
// el is the document's own node.
func parentText(el *element) string {
	if el.parent == nil {
		return ""
	}

	return el.parent.textContent()
}

// TestContactHrefsCarryTheirSubjectAndBody pins what the two hrefs decode to
// (issue #585): both write to the one address, each with the subject the issue
// gives it, and the body the issue writes, line break for line break. A mail
// client shows the decoded subject and body, so a change in the encoded form
// that a reader would see - a stray trailing line break, a lost comma, a space
// where the issue has one - fails here, and so does an href too long for a mail
// client to carry.
func TestContactHrefsCarryTheirSubjectAndBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		href    string
		subject string
		body    string
	}{
		{
			name:    "the assessment's letter",
			href:    contactHrefAssessment,
			subject: "Help with my shift2eu assessment",
			body: "Hello,\r\n\r\nI ran an assessment on shift2eu.com and would like help with:\r\n" +
				"- reading the report\r\n- choosing an EU provider\r\n- planning the move\r\n\r\n" +
				"(Attach the downloaded report if you'd like us to look at it.)\r\n\r\nThanks,",
		},
		{
			name:    "the general question",
			href:    contactHrefGeneral,
			subject: "Question about shift2eu",
			body:    "Hello,\r\n\r\nI have a question about",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			address, values, err := decodeContactHref(tc.href)
			if err != nil {
				t.Fatalf("decode %q: %v: every contact link is a mailto: URL a mail client reads (RFC 6068), so net/url must decode it", tc.href, err)
			}

			if address != contactAddress {
				t.Errorf("%s writes to %q, want %q: the site names one address", tc.name, address, contactAddress)
			}

			if got := values.Get("subject"); got != tc.subject {
				t.Errorf("%s's subject decodes to %q, want %q (issue #585)", tc.name, got, tc.subject)
			}

			if got := values.Get("body"); got != tc.body {
				t.Errorf("%s's body decodes to %q, want %q: the body is the issue's text, line break for line break, with no trailing line break (issue #585)",
					tc.name, got, tc.body)
			}

			if len(tc.href) >= maxContactHrefLength {
				t.Errorf("%s's href is %d characters long, want fewer than %d: a mail client cuts or refuses a longer mailto: reference (issue #585)",
					tc.name, len(tc.href), maxContactHrefLength)
			}
		})
	}
}

// decodeContactHref returns the address and the query of a mailto: href, both
// decoded with net/url: the address as a browser's mail client reads it, and the
// subject and the body as the message it opens holds them.
func decodeContactHref(href string) (string, url.Values, error) {
	parsed, err := url.Parse(href)
	if err != nil {
		return "", nil, err
	}

	if parsed.Scheme != "mailto" {
		return "", nil, fmt.Errorf("the scheme is %q, want mailto", parsed.Scheme)
	}

	address, rawQuery, _ := strings.Cut(strings.TrimPrefix(href, "mailto:"), "?")

	decoded, err := url.PathUnescape(address)
	if err != nil {
		return "", nil, fmt.Errorf("the address: %w", err)
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", nil, fmt.Errorf("the query: %w", err)
	}

	return decoded, values, nil
}
