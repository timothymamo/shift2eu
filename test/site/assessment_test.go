package site_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file holds the checks of the words and the controls the assessment page
// shows (issue #373, the maintainer's feedback): the one-line privacy bar, the
// box about the assessment module that was removed, the two labels of the
// inventory field and the file control, and the link to the new page that tells
// a visitor how to build an inventory.
//
// Every check reads the built page, so it pins what a release serves. The page
// script is read as the source file site/page.js, since one of the checks is
// about a message the script writes rather than the page's own HTML.

// privacyBar is the sentence the privacy page's info bar carries and the
// assessment page's bar must carry too (issue #373, item 02): the maintainer
// replaced both wordy paragraphs with it.
const privacyBar = "No cookies, no local storage, no analytics, nothing loaded from a third party."

// TestPrivacyBarIsOneSentence pins the info bar of the assessment page and of
// the privacy page (issue #373, item 02): one element whose text is exactly the
// sentence, and no page that still carries the long paragraph the sentence
// replaces.
func TestPrivacyBarIsOneSentence(t *testing.T) {
	t.Parallel()

	// oldParagraph is the wording the bar replaced: the site's privacy statement
	// in full, with the layers that back it, which the privacy page repeats in
	// its technical note.
	const oldParagraph = "runs no analytics and loads nothing from a third party"

	bundle := theBundle(t)

	for _, page := range []string{"index.html", "privacy.html"} {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, page)

			bars := 0

			for _, el := range doc.all() {
				if collapseSpace(el.textContent()) == privacyBar {
					bars++
				}
			}

			if bars == 0 {
				t.Errorf("%s holds no element whose text is exactly %q, want the one-sentence info bar of the privacy artifact (issue #373, item 02)",
					page, privacyBar)
			}

			if text := collapseSpace(doc.body(t).textContent()); strings.Contains(text, oldParagraph) {
				t.Errorf("%s still carries the long privacy paragraph %q, want the one-sentence info bar instead (issue #373, item 02)",
					page, oldParagraph)
			}
		})
	}
}

// TestAssessmentModuleNoticeIsGone pins that the box beginning "The assessment
// module" is gone (issue #373, item 03-1): the maintainer removed it from the
// assessment page, so neither the page nor the script that runs it says it. A
// loading notice stays, as the mock-up has one, but it says nothing about an
// "assessment module".
func TestAssessmentModuleNoticeIsGone(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	const box = "The assessment module"

	if text := collapseSpace(bundle.html(t, "index.html").body(t).textContent()); strings.Contains(text, box) {
		t.Errorf("index.html reads %q, want no box beginning %q: the maintainer removed it (issue #373, item 03-1)",
			text, box)
	}

	script, err := os.ReadFile(filepath.Join(theRepositoryRoot(t), "site", "page.js"))
	if err != nil {
		t.Fatalf("read site/page.js: %v", err)
	}

	if strings.Contains(string(script), box) {
		t.Errorf("site/page.js writes a message beginning %q, want none: the page script must not bring the removed box back after it loads (issue #373, item 03-1)",
			box)
	}
}

// TestInventoryFieldLabel pins the label of the page's inventory textarea (issue
// #373, item 03-3-1): "Paste Inventory JSON", exactly, instead of the sentence
// that named the document's type and schema version.
func TestInventoryFieldLabel(t *testing.T) {
	t.Parallel()

	const want = "Paste Inventory JSON"

	doc := theBundle(t).html(t, "index.html")

	labels := 0

	for _, label := range doc.find("label") {
		if got, _ := label.attr("for"); got != "inventory" {
			continue
		}

		labels++

		if text := collapseSpace(label.textContent()); text != want {
			t.Errorf("the label of the inventory textarea reads %q, want exactly %q (issue #373, item 03-3-1)", text, want)
		}
	}

	if labels != 1 {
		t.Fatalf("index.html holds %d labels for the inventory textarea, want exactly one reading %q", labels, want)
	}
}

// TestFileControlWording pins the file control's two visible words (issue #373,
// item 04-1): a label saying "Or upload a file" and a label the visitor clicks
// that says "Choose a file", which is the wording a browser's own file button
// cannot be given, so the page styles a label of its own. The file input itself
// stays, since it is what reads the file.
func TestFileControlWording(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")

	inputs := 0

	for _, input := range doc.find("input") {
		if kind, _ := input.attr("type"); strings.EqualFold(kind, "file") {
			inputs++
		}
	}

	if inputs != 1 {
		t.Fatalf("index.html holds %d file inputs, want exactly one: it is what reads an inventory from a file (issue #373, item 04-1)", inputs)
	}

	want := []string{"Or upload a file", "Choose a file"}

	for _, text := range want {
		found := false

		for _, el := range doc.all() {
			if el.tag != "label" && el.tag != "button" {
				continue
			}

			if collapseSpace(el.textContent()) == text {
				found = true
			}
		}

		if !found {
			t.Errorf("index.html holds no <label> or <button> reading exactly %q, want the file control's wording (issue #373, item 04-1)", text)
		}
	}
}

// TestInventoryGuideLink pins the page's sentence about the inventory guide
// (issue #373, item 04-3): the artifact's wording, with "inventory guide" a link
// to the new page of the site, opened in the same tab because it is a page of
// the site rather than another site.
func TestInventoryGuideLink(t *testing.T) {
	t.Parallel()

	const sentence = "No inventory yet? The inventory guide turns read-only CLI output into one."

	doc := theBundle(t).html(t, "index.html")

	paragraphs := 0

	for _, el := range doc.all() {
		if el.tag != "p" && el.tag != "div" && el.tag != "span" {
			continue
		}

		if collapseSpace(el.textContent()) != sentence {
			continue
		}

		paragraphs++

		links := 0

		for _, anchor := range el.find("a") {
			if linkText := collapseSpace(anchor.textContent()); linkText != "inventory guide" {
				continue
			}

			links++

			if href, _ := anchor.attr("href"); href != inventoryPageName {
				t.Errorf("the inventory guide link opens %q, want %q: it is a page of the site (issue #373, item 04-3)", href, inventoryPageName)
			}

			if target, ok := anchor.attr("target"); ok && strings.TrimSpace(target) != "" {
				t.Errorf("the inventory guide link carries target=%q, want none: the page is one of the site's own, so it opens in the same tab (issue #373, item 04-3)", target)
			}
		}

		if links != 1 {
			t.Errorf("the sentence %q holds %d links reading \\\"inventory guide\\\", want exactly one (issue #373, item 04-3)", sentence, links)
		}
	}

	if paragraphs == 0 {
		t.Errorf("index.html holds no element reading exactly %q, want the artifact's wording (issue #373, item 04-3)", sentence)
	}
}

// find returns every descendant of el whose tag is tag, in document order.
func (el *element) find(tag string) []*element {
	var out []*element

	for _, child := range el.children {
		if child.tag == tag {
			out = append(out, child)
		}

		out = append(out, child.find(tag)...)
	}

	return out
}

// privacyPageName is the bundle's and site/'s name of the privacy page: the page
// that states the site's promise in full.
const privacyPageName = "privacy.html"

// TestPrivacyBarIsThePrivacyPagesInformationBox pins the box the one-sentence
// privacy bar sits in (issue #373, item 02-2): the maintainer asked for the
// wording and an icon together in the blue information box the privacy page
// already carries, because the bar alone reads as a line of ordinary text.
func TestPrivacyBarIsThePrivacyPagesInformationBox(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	// The privacy page is where the artifact puts the sentence in the information
	// box; the assessment page carries the same box.
	privacy := boxReading(t, bundle.html(t, privacyPageName), privacyBar)
	assessment := boxReading(t, bundle.html(t, "index.html"), privacyBar)

	if !hasSilentIcon(assessment) {
		t.Errorf("the assessment page's box %s holds no icon hidden from assistive technology, want an inline <svg> with aria-hidden=\"true\", as the privacy page's box %s has (issue #373, item 02-2)",
			describeElement(assessment), describeElement(privacy))
	}

	for _, image := range assessment.find("img") {
		t.Errorf("the assessment page's box holds the image %s, want an inline <svg> or a text glyph: the page loads no image or font file (issue #373, item 02-2)",
			describeElement(image))
	}

	got, want := classesOf(assessment), classesOf(privacy)
	if !slices.Equal(got, want) {
		t.Errorf("the assessment page's box carries the classes %q, want the privacy page's box's %q: the sentence sits in the same information box on both pages (issue #373, item 02-2)",
			strings.Join(got, " "), strings.Join(want, " "))
	}
}

// TestAssessmentIntroDropsTheHowTheSiteSentence pins the sentence the maintainer
// removed from the assessment page's introduction (issue #373, item 02-3): the
// promise it made is the link inside the sentence that says the assessment runs
// in the browser, so the page does not say it twice.
func TestAssessmentIntroDropsTheHowTheSiteSentence(t *testing.T) {
	t.Parallel()

	const removed = "How the site keeps that promise"

	text := collapseSpace(theBundle(t).html(t, "index.html").body(t).textContent())

	if strings.Contains(text, removed) {
		t.Errorf("the assessment page reads %q, want no sentence beginning %q: the maintainer removed it, and the promise is the link inside the sentence that begins \"The assessment runs\" (issue #373, item 02-3)",
			excerptText(text, 400), removed)
	}
}

// TestAssessmentIntroLinksThePrivacyPageInTheSentence pins where the removed
// sentence's link went (issue #373, item 02-3): inside the sentence that says the
// assessment runs in the browser, so the promise is part of that sentence rather
// than one of its own after a full stop.
func TestAssessmentIntroLinksThePrivacyPageInTheSentence(t *testing.T) {
	t.Parallel()

	const sentence = "The assessment runs"

	doc := theBundle(t).html(t, "index.html")

	var leads []*element

	for _, paragraph := range doc.find("p") {
		if strings.Contains(collapseSpace(paragraph.textContent()), sentence) {
			leads = append(leads, paragraph)
		}
	}

	if len(leads) != 1 {
		t.Fatalf("the assessment page holds %d paragraphs reading %q, want exactly one: the lead that says the assessment runs in this browser (issue #373, item 02-3)",
			len(leads), sentence)
	}

	lead := leads[0]

	var anchor *element

	links := 0

	for _, candidate := range lead.find("a") {
		if href, _ := candidate.attr("href"); href == privacyPageName {
			anchor = candidate
			links++
		}
	}

	if links != 1 {
		t.Fatalf("the lead holds %d links to %s, want exactly one: the promise links the page that keeps it (issue #373, item 02-3)",
			links, privacyPageName)
	}

	before := textBeforeChild(t, lead, anchor)

	at := strings.Index(before, sentence)
	if at < 0 {
		t.Fatalf("the link sits before %q in the lead %q, want it inside that sentence (issue #373, item 02-3)",
			sentence, collapseSpace(lead.textContent()))
	}

	if tail := strings.TrimSpace(before[at+len(sentence):]); strings.ContainsAny(tail, ".!?") {
		t.Errorf("the lead reads %q with %q before its privacy link, want the link inside the sentence that begins %q: a full stop before it leaves the promise a sentence of its own, which the maintainer removed (issue #373, item 02-3)",
			collapseSpace(lead.textContent()), tail, sentence)
	}
}

// boxReading returns the outermost element of doc whose text content is exactly
// text: the box the sentence sits in, rather than a paragraph inside it. It fails
// the test when the page holds none or more than one.
func boxReading(t *testing.T, doc *document, text string) *element {
	t.Helper()

	var found []*element

	for _, el := range doc.all() {
		if collapseSpace(el.textContent()) != text {
			continue
		}

		inside := false

		for _, ancestor := range el.ancestors() {
			if collapseSpace(ancestor.textContent()) == text {
				inside = true
			}
		}

		if !inside {
			found = append(found, el)
		}
	}

	if len(found) != 1 {
		t.Fatalf("the page holds %d outermost elements reading exactly %q, want exactly one: the box the sentence sits in (issue #373, item 02-2)",
			len(found), text)
	}

	return found[0]
}

// hasSilentIcon reports whether el holds an icon the page draws itself and hides
// from assistive technology: an inline <svg> or a text glyph in a span, carrying
// aria-hidden="true".
func hasSilentIcon(el *element) bool {
	for _, descendant := range el.find("svg") {
		if hidden, ok := descendant.attr("aria-hidden"); ok && hidden == "true" {
			return true
		}
	}

	for _, span := range el.find("span") {
		if hidden, ok := span.attr("aria-hidden"); ok && hidden == "true" && strings.TrimSpace(span.textContent()) != "" {
			return true
		}
	}

	return false
}

// classesOf returns the class names of el, sorted, so two spellings of one
// element's class set compare equal.
func classesOf(el *element) []string {
	classes, ok := el.attr("class")
	if !ok {
		return nil
	}

	out := strings.Fields(classes)
	slices.Sort(out)

	return out
}

// textBeforeChild returns el's text content before child, in document order, as
// a browser's textContent reads it: it tells a link that ends a sentence from one
// the sentence itself holds.
func textBeforeChild(t *testing.T, el, child *element) string {
	t.Helper()

	var b strings.Builder

	var walk func(*element) bool

	walk = func(node *element) bool {
		next := 0

		for i, c := range node.children {
			for ; next < len(node.texts) && node.texts[next].at <= i; next++ {
				b.WriteString(node.texts[next].text)
			}

			if c == child {
				return true
			}

			if walk(c) {
				return true
			}
		}

		for ; next < len(node.texts); next++ {
			b.WriteString(node.texts[next].text)
		}

		return false
	}

	if !walk(el) {
		t.Fatalf("the element %s holds no such child %s", describeElement(el), describeElement(child))
	}

	return htmlUnescape(b.String())
}

// introText is the Assessment page's intro paragraph, word for word (issue
// #528): what the site does, why it was built, and who provides it, in the
// sentence the maintainer wrote. It is one constant so a rewording in review is
// one edit.
const introText = "shift2eu compares an AWS, Azure or GCP inventory with what OVHcloud, Scaleway and UpCloud offer, so you can see how far a move to an EU cloud would go before you plan it. It runs entirely in your browser, and nothing you paste leaves it. shift2eu is provided free to use by Timothy Mamo."

// TestAssessmentPageIntro pins the intro paragraph the Assessment page carries
// between its title and its lead paragraph (issue #528): it says what the site
// does and who provides it free to use, and its last words, the maintainer's
// name, link the About page of the site, in the same tab, as the inventory guide
// link does.
func TestAssessmentPageIntro(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, "index.html")

	headings := doc.find("h1")
	if len(headings) != 1 {
		t.Fatalf("index.html holds %d <h1> elements, want exactly one: the page's title, which the intro follows (issue #528)", len(headings))
	}

	leads := 0

	var lead *element

	for _, paragraph := range doc.find("p") {
		if !hasClass(paragraph, "lead") {
			continue
		}

		leads++
		lead = paragraph
	}

	if leads != 1 {
		t.Fatalf("index.html holds %d elements with the class %q, want exactly one: the lead paragraph the intro precedes (issue #528)", leads, "lead")
	}

	intro := 0

	var paragraph *element

	for _, candidate := range doc.find("p") {
		if collapseSpace(candidate.textContent()) != introText {
			continue
		}

		intro++
		paragraph = candidate
	}

	if intro != 1 {
		t.Fatalf("index.html holds %d <p> elements reading the intro %q, want exactly one: a paragraph between the title and the lead that says what the site does, why, and who provides it free to use (issue #528)",
			intro, introText)
	}

	order := documentOrder(doc)

	if order[paragraph] < order[headings[0]] {
		t.Errorf("the intro paragraph comes before the page's <h1>, want it after the title %q (issue #528)", collapseSpace(headings[0].textContent()))
	}

	if order[paragraph] > order[lead] {
		t.Errorf("the intro paragraph comes after the lead paragraph %q, want it between the title and the lead (issue #528)", excerptText(collapseSpace(lead.textContent()), 120))
	}

	links := paragraph.find("a")
	if len(links) != 1 {
		t.Fatalf("the intro paragraph holds %d links, want exactly one: the maintainer's name, linking the about page (issue #528)", len(links))
	}

	link := links[0]

	if got, want := collapseSpace(link.textContent()), aboutHeading; got != want {
		t.Errorf("the intro's link reads %q, want %q: the paragraph ends \"provided free to use by\" and the name links the about page (issue #528)", got, want)
	}

	if href, _ := link.attr("href"); href != aboutPageName {
		t.Errorf("the intro's link to the maintainer opens %q, want %q: it is a page of the site (issue #528)", href, aboutPageName)
	}

	if target, ok := link.attr("target"); ok && strings.TrimSpace(target) != "" {
		t.Errorf("the intro's link carries target=%q, want none: the about page is one of the site's own, so it opens in the same tab (issue #528)", target)
	}
}
