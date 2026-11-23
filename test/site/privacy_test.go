package site_test

import (
	"strings"
	"testing"
)

// outsideTheManifests is the sentence the privacy page must carry about paths
// the site does not own (PRD #297, Layer 3; milestone 3's privacy statement):
// the site is only the files a release's manifest names, and any other path on
// the host is not part of it, even when it answers, so the live checks that
// compare a page with its manifest cannot vouch for it.
const outsideTheManifests = "The site is only the files named in a release's manifest: any other path on the host, even one that answers, is not part of the site."

// TestPrivacyPageNamesThePathsOutsideTheManifests pins that statement on the
// built privacy page, as one sentence of its text: the page says what the live
// checks do and do not compare, so a reader knows a look-alike page uploaded at a
// new path with a leaked key is outside what a release attests.
func TestPrivacyPageNamesThePathsOutsideTheManifests(t *testing.T) {
	t.Parallel()

	page := theBundle(t).html(t, privacyPageName)

	if text := collapseSpace(page.body(t).textContent()); !strings.Contains(text, outsideTheManifests) {
		t.Errorf("the privacy page does not read %q, want it to say that the site is only the files a release's manifest names and that any other path on the host, even one that answers, is not part of the site: the live checks compare the manifests, / and the service-worker paths only, and the page must say so (PRD #297, Layer 3)",
			outsideTheManifests)
	}
}
