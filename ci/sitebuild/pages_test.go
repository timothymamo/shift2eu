package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// This file pins the build's page list (issue #528): pages is the site's set of
// HTML pages, each a template under -site and a file of the bundle of the same
// name, so a page that the site ships but the list does not name is never built,
// and a name the list holds but site/ does not must fail the build rather than
// ship a page of nothing.

// TestPagesHoldTheAboutPage pins the About page in the build's page list (issue
// #528): the site ships about.html, and the builder must write it into the
// bundle, its manifest and the archive with the other pages.
func TestPagesHoldTheAboutPage(t *testing.T) {
	t.Parallel()

	if !slices.Contains(pages, "about.html") {
		t.Errorf("the builder's page list is %v, want it to hold %q: the About page is one of the site's pages (issue #528)", pages, "about.html")
	}
}

// TestEveryPageHasASource pins that every page of the list is a template under
// site/: a name the builder would render from a file that is not there fails the
// build, so the list and the site's sources must agree.
func TestEveryPageHasASource(t *testing.T) {
	t.Parallel()

	site := filepath.Join("..", "..", "site")

	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			if _, err := os.Stat(filepath.Join(site, page)); err != nil {
				t.Errorf("the builder's page list holds %q, which %s does not hold: every page is a template under -site (issue #528)", page, site)
			}
		})
	}
}
