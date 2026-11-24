package site_test

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// This file holds the checks of the pages' links: every relative link resolves
// to a file of the bundle, every loaded resource is a same-origin relative path
// (no third party, and the bundle runs unchanged on any origin), every page
// links to the privacy page, the verification page, the repository and the
// inventory guide, the verification page links to its guide, and no absolute URL
// names a file of the site (PRD #297, Layer 2 and Layer 3).

// repositoryURL is the site's repository, which every page links to.
const repositoryURL = "https://github.com/timothymamo/shift2eu"

// guidePath is the inventory guide the pages link to, from PRD #297's Page
// content.
const guidePath = "docs/guide/inventory.md"

// verifyGuidePath is the release verification guide the verification page
// summarises and links to.
const verifyGuidePath = "docs/guide/verify-releases.md"

// guideURL returns the GitHub URL of a guide at the ref a build shows: the
// release's tag when the build is tagged, and main when it is untagged.
func guideURL(ref, path string) string {
	return repositoryURL + "/blob/" + ref + "/" + path
}

// versionRef returns the git ref a build's links name: the development build's
// is main, a tagged build's is its tag (PRD #297, Page content).
func versionRef(b *bundle) string {
	if b.version == "" {
		return "main"
	}

	return b.version
}

// isAbsoluteURL reports whether a reference leaves the document's own origin,
// either with a scheme or protocol-relative.
func isAbsoluteURL(ref string) bool {
	if strings.HasPrefix(ref, "//") {
		return true
	}

	parsed, err := url.Parse(ref)

	return err == nil && parsed.IsAbs()
}

// referenceSchemes are the schemes an absolute reference of a page may name: the
// web's http and https, which a page loads or navigates to, and the contact
// points' mailto, which names an address rather than a resource. A
// protocol-relative reference spells no scheme at all, so the document's own
// gives it one of the web's and the check passes it without a scheme to compare.
var referenceSchemes = []string{"http", "https", "mailto"}

// absoluteReferenceProblem says why the reference ref is no reference a page of
// the site may make, and "" when it is one: an absolute reference names the web
// (http or https, or a protocol-relative path) or the mailto address of a
// contact point, and no other scheme. A file:, ftp:, ws:, blob: or javascript:
// reference names a resource or a navigation the site does not mean, and the
// browser would follow it where a reader cannot see what it does. A relative
// reference is left to the bundle checks: it names a file of the bundle.
func absoluteReferenceProblem(ref string) string {
	if !isAbsoluteURL(ref) {
		return ""
	}

	parsed, err := url.Parse(ref)
	if err != nil {
		return fmt.Sprintf("it is no URL: %v", err)
	}

	if parsed.Scheme == "" && strings.HasPrefix(ref, "//") {
		return ""
	}

	if slices.Contains(referenceSchemes, parsed.Scheme) {
		return ""
	}

	return fmt.Sprintf("it uses the scheme %q, want one of %v: an absolute reference names the web, a protocol-relative path, or the mailto address of a contact point",
		parsed.Scheme, referenceSchemes)
}

// siteFileProblem says what about the absolute reference ref is a file of the
// bundle addressed at an absolute URL, and "" when it addresses none. The mailto
// scheme is exempt, because it names an address rather than a path of the site,
// and so is every opaque reference, such as javascript:alert(1) or
// tel:+31201234567, which names no path at all: absoluteReferenceProblem decides
// those. Every other scheme's path is read as one, so an uppercase HTTPS:// and
// a protocol-relative //host are checked exactly like a file:///index.html a
// later page might spell. An empty path names the bundle's own index.html only
// when the reference names no host: a URL's root at a host is that site's own
// index, which is no file of this bundle (the maintainer's bio links to
// https://fika.works/ and https://timothymamo.com, issue #528), and a page that
// means its own index spells it as the relative index.html.
func siteFileProblem(bundle *bundle, ref string) string {
	parsed, err := url.Parse(ref)
	if err != nil {
		return fmt.Sprintf("it is no URL: %v", err)
	}

	if parsed.Scheme == "mailto" {
		return ""
	}

	if parsed.Opaque != "" {
		return ""
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	if path == "" {
		if parsed.Host != "" {
			return ""
		}

		path = "index.html"
	}

	if !slices.Contains(bundle.files, path) {
		return ""
	}

	return fmt.Sprintf("it addresses the bundle's own %s at the absolute URL: every reference to a site file is a same-origin relative path", path)
}

// bundleFile resolves a same-origin relative reference to the bundle file it
// names, failing the test when the reference is absolute, leaves the bundle or
// names a file the bundle does not hold.
func bundleFile(t *testing.T, bundle *bundle, ref string) string {
	t.Helper()

	file, err := resolveBundleFile(bundle, ref)
	if err != nil {
		t.Fatalf("%s", err)
	}

	return file
}

// resolveBundleFile resolves a same-origin relative reference to the bundle file
// it names, or says why it names none.
func resolveBundleFile(bundle *bundle, ref string) (string, error) {
	if isAbsoluteURL(ref) {
		return "", fmt.Errorf("%q is an absolute URL: every reference in the built files is a same-origin relative path, so the bundle runs unchanged on any origin (PRD #297, Layer 2)", ref)
	}

	path := ref
	if cut := strings.IndexAny(path, "?#"); cut >= 0 {
		path = path[:cut]
	}

	path = strings.TrimPrefix(path, "./")

	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return "", fmt.Errorf("%q does not name a file of the bundle: a reference is a relative path inside it", ref)
	}

	if !slices.Contains(bundle.files, path) {
		return "", fmt.Errorf("%q names %s, which the bundle does not hold", ref, path)
	}

	return path, nil
}

// TestRelativeLinksResolve pins that every relative link and every loaded
// resource of every page names a file the bundle holds: a page's own links are
// all it can reach, since the site has no server to answer for a path it does
// not ship.
func TestRelativeLinksResolve(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, page)

			for _, anchor := range doc.find("a") {
				href, ok := anchor.attr("href")
				if !ok {
					t.Errorf("%s holds an <a> element with no href", page)

					continue
				}

				if strings.HasPrefix(href, "#") || isAbsoluteURL(href) {
					continue
				}

				bundleFile(t, bundle, href)
			}

			for _, resource := range loadedResources(doc) {
				bundleFile(t, bundle, resource.ref)
			}
		})
	}
}

// loadedResource is one resource a page loads, rather than one it links to.
type loadedResource struct {
	// element is the element that loads it.
	element string
	// attribute is the attribute that names it.
	attribute string
	// ref is the reference as the page spells it.
	ref string
}

// loadedResources returns every resource the document loads: the scripts, the
// stylesheet and the elements that would load an image, a font, media or
// another document.
func loadedResources(doc *document) []loadedResource {
	var resources []loadedResource

	add := func(el *element, attribute string) {
		if value, ok := el.attr(attribute); ok && strings.TrimSpace(value) != "" {
			resources = append(resources, loadedResource{element: el.tag, attribute: attribute, ref: strings.TrimSpace(value)})
		}
	}

	for _, el := range doc.all() {
		switch el.tag {
		case "script":
			add(el, "src")
		case "link":
			add(el, "href")
		case "img":
			add(el, "src")
		case "iframe", "frame", "embed", "track", "audio", "source", "input":
			add(el, "src")
		case "video":
			add(el, "src")
			add(el, "poster")
		case "object":
			add(el, "data")
		}
	}

	return resources
}

// referenceAttributes are the attributes of a page that name a URL: the links,
// the loaded resources, and the form, citation and ping targets. Both walks of
// this file read them all, so a reference cannot hide in an attribute only one
// of them knew. A srcset names several candidates and a ping several URLs.
var referenceAttributes = []string{"href", "src", "action", "data", "formaction", "poster", "srcset", "cite", "ping"}

// pageReference is one reference a page makes, with the element and the
// attribute that spell it.
type pageReference struct {
	element   string
	attribute string
	ref       string
}

// pageReferences returns every reference the element names in the attributes a
// page names a URL in: one per attribute, one per candidate of a srcset, whose
// candidate's URL is its first field, and one per URL of a space-separated ping.
func pageReferences(el *element) []pageReference {
	var references []pageReference

	add := func(attribute, ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}

		references = append(references, pageReference{element: el.tag, attribute: attribute, ref: ref})
	}

	for _, attribute := range referenceAttributes {
		value, ok := el.attr(attribute)
		if !ok {
			continue
		}

		switch attribute {
		case "srcset":
			for _, candidate := range strings.Split(value, ",") {
				if fields := strings.Fields(candidate); len(fields) > 0 {
					add(attribute, fields[0])
				}
			}
		case "ping":
			for _, ref := range strings.Fields(value) {
				add(attribute, ref)
			}
		default:
			add(attribute, value)
		}
	}

	return references
}

// absoluteReferences returns every reference of the document that leaves the
// page's own origin, in document order.
func absoluteReferences(doc *document) []pageReference {
	var references []pageReference

	for _, el := range doc.all() {
		for _, reference := range pageReferences(el) {
			if isAbsoluteURL(reference.ref) {
				references = append(references, reference)
			}
		}
	}

	return references
}

// TestLoadedResourcesAreSameOrigin pins that no page loads anything from another
// origin: no third-party script, stylesheet, font, image or frame. A link to
// another site is a plain <a href> navigation, which the page does not load
// (PRD #297, Layer 2).
func TestLoadedResourcesAreSameOrigin(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			for _, resource := range loadedResources(bundle.html(t, page)) {
				if isAbsoluteURL(resource.ref) {
					t.Errorf("%s loads %s=%q on <%s>: the page loads only its own files, by a same-origin relative path", page, resource.attribute, resource.ref, resource.element)
				}
			}
		})
	}
}

// TestPagesLinkToTheSite pins that every page links to the privacy page, the
// verification page, the repository and the inventory guide, and that the
// guide's link names the ref the build shows: the release's tag, or main when
// the build is untagged (PRD #297, Page content).
func TestPagesLinkToTheSite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bundle *bundle
	}{
		{name: "untagged", bundle: theBundle(t)},
		{name: "tagged", bundle: theTaggedBundle(t)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bundle := tc.bundle
			bundleRef := versionRef(bundle)
			inventoryGuide := guideURL(bundleRef, guidePath)

			for _, page := range pageNames {
				doc := bundle.html(t, page)

				links := make(map[string]bool, 16)

				for _, anchor := range doc.find("a") {
					if href, ok := anchor.attr("href"); ok {
						links[strings.TrimPrefix(href, "./")] = true
					}
				}

				for _, want := range []string{"privacy.html", "verify.html", repositoryURL, inventoryGuide} {
					if !links[want] {
						t.Errorf("%s does not link to %s: every page links to the privacy page, the verification page, the repository and the inventory guide (PRD #297, Page content)", page, want)
					}
				}
			}
		})
	}
}

// TestRepositoryLinkOpensInANewTab pins every page's Repository link (issue
// #373, item 01): it opens in a new tab and tells the browser that the page it
// opens gets no handle on this one. The repository is another site, so a reader
// who follows it keeps the assessment page they were reading.
func TestRepositoryLinkOpensInANewTab(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			links := 0

			for _, anchor := range bundle.html(t, page).find("a") {
				if href, _ := anchor.attr("href"); href != repositoryURL {
					continue
				}

				links++

				if target, _ := anchor.attr("target"); target != "_blank" {
					t.Errorf("%s's link to the repository carries target=%q, want \"_blank\": it opens in a new tab (issue #373, item 01)", page, target)
				}

				if rel, _ := anchor.attr("rel"); !slices.Equal(strings.Fields(rel), []string{"noopener", "noreferrer"}) {
					t.Errorf("%s's link to the repository carries rel=%q, want \"noopener noreferrer\": the page it opens gets no handle on this one (issue #373, item 01)", page, rel)
				}
			}

			if links == 0 {
				t.Errorf("%s holds no link to the repository, want one that opens in a new tab (issue #373, item 01)", page)
			}
		})
	}
}

// TestVerificationPageLinksItsGuide pins the verification page's link to the
// guide's path on GitHub at the ref the build shows: main when the build is
// untagged, and the release's tag otherwise (PRD #297, Layer 3).
func TestVerificationPageLinksItsGuide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bundle *bundle
		want   string
	}{
		{name: "untagged", bundle: theBundle(t), want: guideURL("main", verifyGuidePath)},
		{name: "tagged", bundle: theTaggedBundle(t), want: guideURL(taggedVersion, verifyGuidePath)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			links := make(map[string]bool, 16)

			for _, anchor := range tc.bundle.html(t, "verify.html").find("a") {
				if href, ok := anchor.attr("href"); ok {
					links[href] = true
				}
			}

			if !links[tc.want] {
				t.Errorf("verify.html does not link to %s: the verification page summarises the guide and links to its path at the release's tag, or main when untagged", tc.want)
			}
		})
	}
}

// TestLinkChecksAcceptAMailtoLink pins what this file's two checks do with the
// page's contact points, both `mailto:info@shift2eu.com` (issue #373): the
// reference is absolute, so TestRelativeLinksResolve never resolves it as a file
// of the bundle, and the site-file check exempts mailto rather than reading the
// address as a path inside the bundle. Neither check refuses it. The contact
// address is opaque, so the exemption is the one every opaque reference gets
// (see TestSiteFileAtAnAbsoluteURLIsRefused).
func TestLinkChecksAcceptAMailtoLink(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	tests := []struct {
		name string
		ref  string
	}{
		{name: "the contact address", ref: contactHref},
		{name: "the contact address with a subject", ref: contactHref + "?subject=Assessment"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !isAbsoluteURL(tc.ref) {
				t.Errorf("isAbsoluteURL(%q) = false, want true: a mailto: reference is no path of the bundle, so no check resolves it as one", tc.ref)
			}

			if _, err := resolveBundleFile(bundle, tc.ref); err == nil {
				t.Errorf("resolveBundleFile(%q) resolved the reference to a bundle file, want it left alone: the link loads nothing", tc.ref)
			}

			if problem := siteFileProblem(bundle, tc.ref); problem != "" {
				t.Errorf("the site-file check refuses %q: %s, want it left alone: such a reference names no file", tc.ref, problem)
			}
		})
	}
}

// TestNoAbsoluteURLToASiteFile pins that no page addresses a file of its own
// bundle at an absolute URL: the bundle runs unchanged on any origin, a local
// static file server included (PRD #297, Layer 2 and Layer 3). A link to another
// site is no site file and stays allowed, its root included (the maintainer's
// bio links to https://fika.works/ and https://timothymamo.com, issue #528), and
// so does a `mailto:` reference, which names an address rather than a path; an
// opaque reference such as javascript:alert(1) names no path either and is left
// to the scheme check below. No other scheme is exempt, so a file:, ftp:, ws: or
// blob: reference is read as a path too (see TestSiteFileAtAnAbsoluteURLIsRefused).
func TestNoAbsoluteURLToASiteFile(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			for _, reference := range absoluteReferences(bundle.html(t, page)) {
				if problem := siteFileProblem(bundle, reference.ref); problem != "" {
					t.Errorf("%s holds %s=%q on <%s>, and %s", page, reference.attribute, reference.ref, reference.element, problem)
				}
			}
		})
	}
}

// TestSiteFileAtAnAbsoluteURLIsRefused pins what the site-file check reads from
// an absolute reference of any scheme: a mailto address is one exemption, and an
// opaque reference such as javascript:alert(1) or tel:+31201234567 another,
// since it names no path at all; an uppercase HTTPS:// and a protocol-relative
// //host are read as the paths they spell, so a later file:///index.html or
// ftp://host/style.css is caught rather than skipped. A reference whose path is
// the root names the bundle's own index.html when it names no host, and another
// site's index when it does, which is no file of this bundle. Each case names
// the bundle file the check must refuse, or none when the reference names no
// site file.
func TestSiteFileAtAnAbsoluteURLIsRefused(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	tests := []struct {
		name string
		ref  string
		// wantFile is the bundle file the check must refuse, or "" when the
		// reference names none.
		wantFile string
	}{
		{name: "an uppercase HTTPS URL to a site file", ref: "HTTPS://example.com/index.html", wantFile: "index.html"},
		{name: "a protocol-relative URL to a site file", ref: "//example.com/index.html", wantFile: "index.html"},
		{name: "a file URL to a site file", ref: "file:///index.html", wantFile: "index.html"},
		{name: "an ftp URL to a site file", ref: "ftp://example.com/privacy.html", wantFile: "privacy.html"},
		{name: "a host-less URL of the bundle's root", ref: "file:///", wantFile: "index.html"},
		{name: "the contact address", ref: contactHref, wantFile: ""},
		{name: "an address with a subject", ref: contactHref + "?subject=Assessment", wantFile: ""},
		{name: "the repository", ref: repositoryURL, wantFile: ""},
		{name: "an external site's root", ref: "https://fika.works/", wantFile: ""},
		{name: "an external site's root without a trailing slash", ref: "https://timothymamo.com", wantFile: ""},
		{name: "a javascript URL", ref: "javascript:alert(1)", wantFile: ""},
		{name: "a telephone address", ref: "tel:+31201234567", wantFile: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problem := siteFileProblem(bundle, tc.ref)

			if tc.wantFile == "" {
				if problem != "" {
					t.Errorf("siteFileProblem(%q) = %q, want none: the reference names no file of the bundle", tc.ref, problem)
				}

				return
			}

			if !strings.Contains(problem, tc.wantFile) {
				t.Errorf("siteFileProblem(%q) = %q, want a problem naming %q: an absolute URL to a site file is refused, whatever its scheme",
					tc.ref, problem, tc.wantFile)
			}
		})
	}
}

// TestAbsoluteReferencesAreOfTheWeb pins that every absolute reference of every
// page names the web (http or https, or a protocol-relative path) or the mailto
// address of a contact point: no page holds a file:, ftp:, ws:, blob: or
// javascript: reference, which a browser would follow where the site means
// nothing of the kind.
func TestAbsoluteReferencesAreOfTheWeb(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			references := absoluteReferences(bundle.html(t, page))

			for _, reference := range references {
				if problem := absoluteReferenceProblem(reference.ref); problem != "" {
					t.Errorf("%s holds %s=%q on <%s>, and %s", page, reference.attribute, reference.ref, reference.element, problem)
				}
			}

			if len(references) == 0 {
				t.Errorf("%s holds no absolute reference, so the check read nothing: every page links to the repository's HTTPS URL", page)
			}
		})
	}
}

// TestReferenceWalksCatchANestedURLAttribute pins that both walks read the
// attributes a page names a URL in beyond href, src, action and data, taking a
// srcset candidate as its own URL: a button's formaction with a javascript: URL
// is refused by the scheme check, and an image's srcset candidate with a file:
// URL by both, so a later page cannot hide a reference in either (issue #373).
func TestReferenceWalksCatchANestedURLAttribute(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	tests := []struct {
		name string
		// markup holds the one element the page might add.
		markup string
		// ref is the reference the walk must read and a check refuse.
		ref string
		// wantWebProblem is the text absoluteReferenceProblem must name, and
		// wantSiteFileProblem the one siteFileProblem must name; "" means the
		// check must leave the reference alone.
		wantWebProblem      string
		wantSiteFileProblem string
	}{
		{
			name:                "a formaction that navigates with a javascript URL",
			markup:              `<form action="index.html"><button formaction="javascript:alert(1)">Go</button></form>`,
			ref:                 "javascript:alert(1)",
			wantWebProblem:      `the scheme "javascript"`,
			wantSiteFileProblem: "",
		},
		{
			name:                "a srcset candidate that names a file",
			markup:              `<img src="style.css" srcset="style.css 1x, file:///index.html 2x" alt="">`,
			ref:                 "file:///index.html",
			wantWebProblem:      `the scheme "file"`,
			wantSiteFileProblem: "index.html",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc := parseHTML(t, "fixture.html", []byte("<html><body>"+tc.markup+"</body></html>"))

			found := false

			for _, reference := range absoluteReferences(doc) {
				if reference.ref == tc.ref {
					found = true
				}
			}

			if !found {
				t.Fatalf("the walk reads no %q from %s: both checks read every attribute a page names a URL in, a srcset candidate taken as its own URL",
					tc.ref, tc.markup)
			}

			if problem := absoluteReferenceProblem(tc.ref); tc.wantWebProblem == "" && problem != "" {
				t.Errorf("absoluteReferenceProblem(%q) = %q, want none: an opaque reference names no resource the site loads", tc.ref, problem)
			} else if tc.wantWebProblem != "" && !strings.Contains(problem, tc.wantWebProblem) {
				t.Errorf("absoluteReferenceProblem(%q) = %q, want a problem naming %q: the reference leaves the web", tc.ref, problem, tc.wantWebProblem)
			}

			if problem := siteFileProblem(bundle, tc.ref); tc.wantSiteFileProblem == "" && problem != "" {
				t.Errorf("siteFileProblem(%q) = %q, want none: the reference names no file of the bundle", tc.ref, problem)
			} else if tc.wantSiteFileProblem != "" && !strings.Contains(problem, tc.wantSiteFileProblem) {
				t.Errorf("siteFileProblem(%q) = %q, want a problem naming %q: the reference addresses a file of the bundle", tc.ref, problem, tc.wantSiteFileProblem)
			}
		})
	}
}

// TestAbsoluteReferenceSchemes pins what the scheme check reads from an inline
// reference: a javascript: and a file: reference fail, and the web's http and
// https, an uppercase HTTPS://, a protocol-relative //host and the contact
// points' mailto pass. A relative reference is no absolute reference and is left
// to the bundle checks, and so is one that cannot be parsed, which the scheme
// check refuses rather than passes.
func TestAbsoluteReferenceSchemes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{name: "a javascript reference", ref: "javascript:alert(1)", want: false},
		{name: "a file reference", ref: "file:///index.html", want: false},
		{name: "an ftp reference", ref: "ftp://example.com/style.css", want: false},
		{name: "a blob reference", ref: "blob:https://example.com/6f2c", want: false},
		{name: "the contact address", ref: contactHref, want: true},
		{name: "an https reference", ref: "https://example.com/index.html", want: true},
		{name: "an uppercase HTTPS reference", ref: "HTTPS://example.com/index.html", want: true},
		{name: "a protocol-relative reference", ref: "//example.com/index.html", want: true},
		{name: "a malformed protocol-relative reference", ref: "//[bad", want: false},
		{name: "a relative reference", ref: "index.html", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problem := absoluteReferenceProblem(tc.ref)

			if tc.want && problem != "" {
				t.Errorf("absoluteReferenceProblem(%q) = %q, want none: the reference names the web or a mailto address", tc.ref, problem)
			}

			if !tc.want && problem == "" {
				t.Errorf("absoluteReferenceProblem(%q) = none, want a problem: the check must refuse a reference of another scheme, and one it cannot parse", tc.ref)
			}
		})
	}
}
