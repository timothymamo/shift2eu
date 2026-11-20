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

// TestNoAbsoluteURLToASiteFile pins that no page addresses a file of its own
// bundle at an absolute URL: the bundle runs unchanged on any origin, a local
// static file server included (PRD #297, Layer 2 and Layer 3). A link to another
// site is no site file and stays allowed.
func TestNoAbsoluteURLToASiteFile(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			for _, el := range bundle.html(t, page).all() {
				for _, attribute := range []string{"href", "src", "action", "data"} {
					ref, ok := el.attr(attribute)
					if !ok || !isAbsoluteURL(ref) {
						continue
					}

					parsed, err := url.Parse(ref)
					if err != nil {
						t.Errorf("%s holds %s=%q on <%s>, which is no URL: %v", page, attribute, ref, el.tag, err)

						continue
					}

					path := strings.TrimPrefix(parsed.Path, "/")
					if path == "" {
						path = "index.html"
					}

					if slices.Contains(bundle.files, path) {
						t.Errorf("%s addresses the bundle's own %s at the absolute URL %q: every reference to a site file is a same-origin relative path", page, path, ref)
					}
				}
			}
		})
	}
}
