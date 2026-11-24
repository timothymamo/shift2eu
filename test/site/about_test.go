package site_test

import (
	"strings"
	"testing"
)

// This file holds the checks of the About page (issue #528): the maintainer's
// bio, word for word as the issue gives it, the links to the maintainer's other
// places on the web, and the avatar. The avatar is inline SVG because the site
// loads no image at all (img-src 'none', ADR 0036): no <img>, no data: URL and
// no external href, which the page's own checks of this package already refuse
// for every page, and which the avatar's checks pin again so a traced avatar
// cannot arrive as a placeholder or as an image in disguise.
//
// The page is one of pageNames, so every per-page rule of this package (the
// policy, the scripts, the footer, the no-image scan, the bundle's links) holds
// for it too; this file pins only what the page itself must say.
//
// The words below are constants, one per statement, so a rewording the
// maintainer makes in review is one edit and the check still says what it wants.

// aboutPageName is the bundle's and site/'s name of the About page.
const aboutPageName = "about.html"

// The About page's words, word for word (issue #528).
const (
	aboutHeading  = "Timothy Mamo"
	aboutTagline  = "Platform Specialist, Amsterdam"
	aboutBioStart = "Timothy Mamo helps growing companies make the most of the cloud, with a focus on Cloud Native technologies and practices. His path into the cloud was a varied one: he studied aerospace engineering and worked in the automotive industry before moving into the world of cloud platforms. He enjoys working with others and helping them improve and understand, at times with some Mediterranean gusto."
	aboutBioFika  = "Timothy is a member of FikaWorks, a consultancy of senior professionals, collectively owned as a cooperative."
	aboutBioShift = "He builds shift2eu in the open and provides it free to use."
)

// The avatar's size budget (issue #528): a traced SVG small enough to ship on a
// page that loads nothing else, and at least a kilobyte, so a placeholder or a
// stray empty element does not pass for a portrait.
const (
	avatarMinBytes = 1 << 10
	avatarMaxBytes = 30 << 10
)

// TestAboutPageWords pins the About page's words (issue #528): one <h1> reading
// the maintainer's name, the tagline, and the three paragraphs of the bio, each
// word for word as its own <p>, in the order the issue gives them. A page that
// dropped a paragraph, merged two or reworded one fails here.
func TestAboutPageWords(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, aboutPageName)

	headings := doc.find("h1")
	if len(headings) != 1 {
		t.Fatalf("%s holds %d <h1> elements, want exactly one reading %q: the page's title is the maintainer's name (issue #528)",
			aboutPageName, len(headings), aboutHeading)
	}

	if got := collapseSpace(headings[0].textContent()); got != aboutHeading {
		t.Errorf("%s's <h1> reads %q, want %q (issue #528)", aboutPageName, got, aboutHeading)
	}

	order := documentOrder(doc)
	after := order[headings[0]]

	for _, want := range []string{aboutTagline, aboutBioStart, aboutBioFika, aboutBioShift} {
		found := 0

		var at int

		for _, paragraph := range doc.find("p") {
			if collapseSpace(paragraph.textContent()) != want {
				continue
			}

			found++
			at = order[paragraph]
		}

		if found != 1 {
			t.Errorf("%s holds %d <p> elements reading exactly %q, want exactly one: the page states it word for word (issue #528)",
				aboutPageName, found, want)

			continue
		}

		if at < after {
			t.Errorf("%s states %q before the statement that comes before it, want the tagline and then the three paragraphs of the bio in the issue's order (issue #528)",
				aboutPageName, want)
		}

		after = at
	}
}

// TestAboutPageLinks pins the About page's list of external links (issue #528):
// the maintainer's LinkedIn profile, FikaWorks, the blog and the repository, each
// exactly once inside one list, each opening in a new tab with rel="noopener
// noreferrer" as the site's other links to another site do, and each telling a
// visitor who cannot see the new tab that it opens one, with the site's
// visually-hidden span. The nav links the repository too, so only the list counts
// towards the four: the page is a list of the maintainer's places on the web, and
// its nav is the site's (reviewer of issue #528).
func TestAboutPageLinks(t *testing.T) {
	t.Parallel()

	const opensInANewTab = "(opens in a new tab)"

	doc := theBundle(t).html(t, aboutPageName)

	want := []string{
		"https://www.linkedin.com/in/timothymamo/",
		"https://fika.works/",
		"https://timothymamo.com",
		"https://github.com/timothymamo/shift2eu",
	}

	// The one list holding the four links is the ancestor list of the first
	// wanted link that sits in one: the nav's Repository item is no part of it,
	// so the nav may link the repository without the page linking it twice.
	var list *element

	for _, href := range want {
		for _, anchor := range doc.find("a") {
			if got, _ := anchor.attr("href"); got != href {
				continue
			}

			if ancestor := ancestorList(anchor); ancestor != nil {
				list = ancestor

				break
			}
		}

		if list != nil {
			break
		}
	}

	if list == nil {
		t.Fatalf("%s holds none of the four links the About page lists in a <ul> or <ol>, so the check read nothing (issue #528)", aboutPageName)
	}

	links := make(map[string][]*element, len(want))

	for _, anchor := range doc.find("a") {
		if !isDescendant(anchor, list) {
			continue
		}

		href, _ := anchor.attr("href")

		links[href] = append(links[href], anchor)
	}

	for _, href := range want {
		found := links[href]

		if len(found) != 1 {
			t.Errorf("%s's list of links holds %q %d times, want exactly once: the page is a list of the maintainer's places on the web (issue #528)",
				aboutPageName, href, len(found))

			continue
		}

		anchor := found[0]

		if target, _ := anchor.attr("target"); target != "_blank" {
			t.Errorf("%s's link to %s carries target=%q, want \"_blank\": it opens another site in a new tab (issue #528)", aboutPageName, href, target)
		}

		if rel, _ := anchor.attr("rel"); !hasNoopenerNoreferrer(rel) {
			t.Errorf("%s's link to %s carries rel=%q, want \"noopener noreferrer\": the page it opens gets no handle on this one (issue #528)", aboutPageName, href, rel)
		}

		if strings.TrimSpace(collapseSpace(anchor.textContent())) == "" {
			t.Errorf("%s's link to %s holds no text, so a visitor cannot read where it goes (issue #528)", aboutPageName, href)
		}

		hidden := 0

		for _, span := range anchor.find("span") {
			if hasClass(span, "visually-hidden") && collapseSpace(span.textContent()) == opensInANewTab {
				hidden++
			}
		}

		if hidden != 1 {
			t.Errorf("%s's link to %s holds %d visually-hidden spans reading %q, want exactly one: the site tells a visitor who cannot see the new tab that the link opens one (issue #528)",
				aboutPageName, href, hidden, opensInANewTab)
		}
	}
}

// hasNoopenerNoreferrer reports whether rel names noopener and noreferrer and
// nothing else, as the site's links to another site spell it.
func hasNoopenerNoreferrer(rel string) bool {
	fields := strings.Fields(rel)

	if len(fields) != 2 {
		return false
	}

	seen := map[string]bool{}

	for _, field := range fields {
		seen[strings.ToLower(field)] = true
	}

	return seen["noopener"] && seen["noreferrer"]
}

// ancestorList returns the nearest <ul> or <ol> that holds el, or nil when no
// list does.
func ancestorList(el *element) *element {
	for ancestor := el.parent; ancestor != nil; ancestor = ancestor.parent {
		if ancestor.tag == "ul" || ancestor.tag == "ol" {
			return ancestor
		}
	}

	return nil
}

// TestAboutPageAvatar pins the avatar (issue #528, the maintainer's decision of
// 2026-11-24): one inline <svg> with role="img" and an aria-label a screen
// reader can read, and no image in any other shape, under a policy that refuses
// every image request. Its markup, from <svg to </svg>, stays inside the size
// budget, so a traced portrait ships rather than an embedded photograph.
func TestAboutPageAvatar(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)
	doc := bundle.html(t, aboutPageName)

	avatars := 0

	var avatar *element

	for _, el := range doc.all() {
		if el.tag != "svg" {
			continue
		}

		if role, _ := el.attr("role"); !strings.EqualFold(strings.TrimSpace(role), "img") {
			continue
		}

		avatars++
		avatar = el
	}

	if avatars != 1 {
		t.Fatalf("%s holds %d <svg role=\"img\"> elements, want exactly one: the maintainer's avatar, an image to assistive technology (issue #528)",
			aboutPageName, avatars)
	}

	if label, _ := avatar.attr("aria-label"); strings.TrimSpace(label) == "" {
		t.Errorf("%s's avatar carries no aria-label, want a name such as \"Portrait of %s\": an <svg role=\"img\"> with no accessible name is announced as nothing (issue #528)",
			aboutPageName, aboutHeading)
	}

	for _, el := range doc.all() {
		if el.tag == "image" {
			t.Errorf("%s holds an SVG <image> element: the avatar is inline shapes and paths, and an SVG <image> loads a resource (issue #528)", aboutPageName)
		}
	}

	for _, attribute := range avatarAttrs(avatar) {
		if strings.HasPrefix(strings.TrimSpace(attribute.value), "#") {
			continue
		}

		if attribute.name == "href" || attribute.name == "xlink:href" {
			t.Errorf("%s's avatar carries %s=%q, want a same-document \"#fragment\" reference or none: the policy refuses every other image request (issue #528)",
				aboutPageName, attribute.name, attribute.value)
		}
	}

	markup := avatarMarkup(t, string(bundle.read(t, aboutPageName)))

	for _, refused := range []string{"<image", "data:"} {
		if at := strings.Index(strings.ToLower(markup), refused); at >= 0 {
			t.Errorf("%s's avatar holds %q at byte %d: the avatar is inline SVG with no embedded or loaded image (issue #528)",
				aboutPageName, refused, at)
		}
	}

	if len(markup) < avatarMinBytes {
		t.Errorf("%s's avatar is %d bytes of markup, want at least %d: a portrait that traces the maintainer's photo is not a placeholder (issue #528)",
			aboutPageName, len(markup), avatarMinBytes)
	}

	if len(markup) > avatarMaxBytes {
		t.Errorf("%s's avatar is %d bytes of markup, over the budget of %d: the avatar ships on every load of the page, so it is traced small (issue #528)",
			aboutPageName, len(markup), avatarMaxBytes)
	}
}

// avatarAttrs returns the URL attributes of the avatar and of every element
// inside it: href and xlink:href, which is how an SVG element names a resource.
func avatarAttrs(avatar *element) []attribute {
	var out []attribute

	stack := []*element{avatar}

	for len(stack) > 0 {
		el := stack[0]
		stack = stack[1:]

		for _, attr := range el.attrs {
			switch attr.name {
			case "href", "xlink:href":
				out = append(out, attr)
			}
		}

		stack = append(stack, el.children...)
	}

	return out
}

// avatarMarkup returns the avatar's markup, from its <svg start tag to the
// </svg> that closes it, as the built page holds it: the size budget is a budget
// on the bytes a visitor loads, which the parsed document no longer has. It
// fails the test when the page holds no <svg role="img"> in its source or more
// than one.
func avatarMarkup(t *testing.T, source string) string {
	t.Helper()

	lower := strings.ToLower(source)
	var found []string

	for at := 0; at < len(lower); {
		open := strings.Index(lower[at:], "<svg")
		if open < 0 {
			break
		}

		start := at + open

		endOfStartTag := strings.IndexByte(lower[start:], '>')
		if endOfStartTag < 0 {
			break
		}

		endOfStartTag += start

		closeAt := strings.Index(lower[endOfStartTag+1:], "</svg>")
		if closeAt < 0 {
			break
		}

		end := endOfStartTag + 1 + closeAt + len("</svg>")

		if strings.Contains(lower[start:endOfStartTag+1], `role="img"`) {
			found = append(found, source[start:end])
		}

		at = end
	}

	if len(found) != 1 {
		t.Fatalf("%s's source holds %d <svg role=\"img\"> elements, want exactly one to measure: the avatar (issue #528)", aboutPageName, len(found))
	}

	return found[0]
}
