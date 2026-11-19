package site_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file holds the checks that the site loads nothing it does not need and
// runs no inline code: no image, no data: URL and no CSS url() or @import
// anywhere in the bundle or in the HTML reporter's goldens, and no inline
// script, <style> element, style attribute or on* handler in either (PRD #297,
// Layer 2).
//
// The HTML reporter's goldens are part of it because they are what the page
// inserts into the report: a fragment that carried an image, a style attribute
// or a data: URL would break the policy from inside the page. The hostile
// goldens matter most: they hold inventory values that try exactly that.

// htmlSource is one HTML document the content checks read: a built page or one
// of the HTML reporter's goldens.
type htmlSource struct {
	// name says where the document comes from, for a failure message.
	name string
	// doc is the parsed document.
	doc *document
	// golden is true for a reporter golden, which is a fragment and holds no
	// script at all.
	golden bool
}

// hostileGoldens must be among the goldens the checks read: the reporter's
// hostile fixtures are the ones that try to smuggle markup through a value.
var hostileGoldens = []string{"comparison-hostile.golden", "report-hostile.golden"}

// htmlSources returns the built pages and every HTML reporter golden, failing
// the test when a hostile golden is missing, so the check never passes because
// it read the harmless ones alone.
func htmlSources(t *testing.T) []htmlSource {
	t.Helper()

	bundle := theBundle(t)

	sources := make([]htmlSource, 0, len(pageNames)+8)

	for _, page := range pageNames {
		sources = append(sources, htmlSource{name: page, doc: bundle.html(t, page)})
	}

	goldenDir := filepath.Join(theRepositoryRoot(t), "internal", "adapter", "report", "htmlreport", "testdata")

	goldens, err := filepath.Glob(filepath.Join(goldenDir, "*.golden"))
	if err != nil {
		t.Fatalf("list the HTML reporter's goldens in %s: %v", goldenDir, err)
	}

	names := make([]string, 0, len(goldens))

	for _, golden := range goldens {
		names = append(names, filepath.Base(golden))
	}

	slices.Sort(names)

	for _, want := range hostileGoldens {
		if !slices.Contains(names, want) {
			t.Fatalf("the HTML reporter holds no %s golden (%v), so this check would read the harmless ones alone", want, names)
		}
	}

	for _, golden := range goldens {
		data, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("read the HTML reporter's golden %s: %v", golden, err)
		}

		name := "htmlreport/testdata/" + filepath.Base(golden)
		sources = append(sources, htmlSource{name: name, doc: parseHTML(t, name, data), golden: true})
	}

	return sources
}

// svgExternalHrefElements are the SVG elements that load another resource
// through their href or xlink:href.
var svgExternalHrefElements = []string{"image", "use", "feimage"}

// iconLinkRels are the <link rel> tokens that make the browser load an image.
var iconLinkRels = []string{"icon", "shortcut", "apple-touch-icon", "apple-touch-icon-precomposed", "mask-icon"}

// TestNoImageAnywhere pins that the site loads no image: img-src is 'none', not
// 'self', because an own-origin image request carries data in its path or query
// to the host's logs (PRD #297, Layer 2). The check fails on an <img>, a
// <picture>, an <input type="image">, a <link rel="icon"> or another image
// link, an SVG <image>, <use> or <feImage> whose href or xlink:href is no
// fragment, and on any data: URL in any attribute, in every built page and in
// every HTML reporter golden, the hostile ones included.
func TestNoImageAnywhere(t *testing.T) {
	t.Parallel()

	for _, source := range htmlSources(t) {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()

			for _, el := range source.doc.all() {
				switch el.tag {
				case "img", "picture":
					t.Errorf("%s holds a <%s> element: the site loads no image, and any icon is inline SVG markup", source.name, el.tag)
				case "input":
					if kind, _ := el.attr("type"); strings.EqualFold(kind, "image") {
						t.Errorf("%s holds an <input type=\"image\">: it loads an image", source.name)
					}
				case "link":
					rel, _ := el.attr("rel")

					for _, token := range strings.Fields(strings.ToLower(rel)) {
						if slices.Contains(iconLinkRels, token) {
							t.Errorf("%s holds <link rel=%q>: it makes the browser load an icon, and the site ships no image", source.name, rel)
						}
					}
				}

				if slices.Contains(svgExternalHrefElements, el.tag) {
					for _, attribute := range []string{"href", "xlink:href"} {
						href, ok := el.attr(attribute)
						if ok && !strings.HasPrefix(strings.TrimSpace(href), "#") {
							t.Errorf("%s holds <%s %s=%q>: an SVG element that loads a resource outside the document", source.name, el.tag, attribute, href)
						}
					}
				}

				for _, attr := range el.attrs {
					if strings.Contains(strings.ToLower(attr.value), "data:") {
						t.Errorf("%s holds %s=%q on <%s>: a data: URL loads a resource inside the document, which img-src 'none' refuses anyway", source.name, attr.name, attr.value, el.tag)
					}
				}
			}
		})
	}
}

// TestStylesheetLoadsNothing pins the other half of img-src 'none': a CSS url()
// loads under the directive of what it loads, so the stylesheet the page loads
// under style-src 'self' references no url() and no @import, and the site's
// fonts are the system's (PRD #297, Layer 2).
func TestStylesheetLoadsNothing(t *testing.T) {
	t.Parallel()

	name, data := theBundle(t).asset(t, "style", ".css")
	stylesheet := string(data)

	lower := strings.ToLower(stylesheet)

	if at := strings.Index(lower, "url("); at >= 0 {
		t.Errorf("%s holds a url() at byte %d (%q): the stylesheet loads no image, font or other resource, and img-src is 'none'", name, at, excerpt(stylesheet, at))
	}

	if at := strings.Index(lower, "@import"); at >= 0 {
		t.Errorf("%s holds an @import at byte %d (%q): the stylesheet loads no other stylesheet", name, at, excerpt(stylesheet, at))
	}
}

// TestNoInlineCodeAnywhere pins that no page and no reporter golden holds inline
// script, a <style> element, a style attribute or an inline event handler: with
// style-src 'self' and script-src 'self' 'wasm-unsafe-eval' the CSP refuses
// every one, so a page that carried one would be refused rather than run, and
// the rule is what makes an injection through a report value inert (PRD #297,
// Layer 2).
func TestNoInlineCodeAnywhere(t *testing.T) {
	t.Parallel()

	for _, source := range htmlSources(t) {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()

			for _, el := range source.doc.all() {
				switch el.tag {
				case "style":
					t.Errorf("%s holds a <style> element: no page and no report fragment carries inline CSS", source.name)
				case "script":
					if source.golden {
						t.Errorf("%s holds a <script> element: the report fragment carries no script", source.name)

						continue
					}

					if _, ok := el.attr("src"); !ok {
						t.Errorf("%s holds an inline <script> with no src: the page runs no inline script", source.name)
					}
				}

				for _, attr := range el.attrs {
					if attr.name == "style" {
						t.Errorf("%s holds a style attribute on <%s>: %q: no page or report fragment carries inline CSS", source.name, el.tag, strings.TrimSpace(attr.value))
					}

					if isEventHandler(attr.name) {
						t.Errorf("%s holds the inline event handler %s=%q on <%s>: no page or report fragment carries one", source.name, attr.name, attr.value, el.tag)
					}
				}
			}
		})
	}
}

// isEventHandler reports whether the attribute name is an inline event handler,
// such as onclick or onerror.
func isEventHandler(name string) bool {
	if !strings.HasPrefix(name, "on") || len(name) <= 2 {
		return false
	}

	for _, r := range name[2:] {
		if r < 'a' || r > 'z' {
			return false
		}
	}

	return true
}

// excerpt returns a short window of s around byte at, for a failure message.
func excerpt(s string, at int) string {
	start := max(0, at-20)
	end := min(len(s), at+40)

	return s[start:end]
}
