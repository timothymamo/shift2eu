package site_test

import (
	"os"
	"path/filepath"
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
