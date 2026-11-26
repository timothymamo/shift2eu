// Pins README.md's pointer to the site and the advice it owes a first-time
// visitor (PRD #297 milestone 4): the site's exact URL as a link, and the
// advice to type it rather than follow an http:// link, because on a first
// visit a network attacker can strip HTTPS from such a link and serve a
// look-alike page.
//
// The advice's phrasing is not this file's own copy: firstVisitAdviceProblems,
// below, is the rule test/docs/verifyguide_test.go holds the verification page
// and its guide section to as well, so the README, the page and the guide
// cannot drift apart. A README that lost the URL or one of the two phrases
// fails exactly as the page or the section would.
package docs_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// readmePath is the repository's README, the page a visitor of the repository
// reads first.
const readmePath = "README.md"

// firstVisitAdvicePhrases are the two phrases of the first-visit advice the
// verification page, its guide section and README.md share: the occasion and
// the attack. They are matched without case, so the advice may open a sentence.
var firstVisitAdvicePhrases = []struct{ phrase, why string }{
	{
		phrase: "first visit",
		why:    "the advice is about a first visit, when a browser has seen no HSTS for the site and an attacker on the network can still replace the page",
	},
	{
		phrase: "strip https",
		why:    "the attack the advice names: an http:// link lets a network attacker strip HTTPS and serve a look-alike page",
	},
}

// firstVisitAdviceProblems returns what doc, named docPath, fails to carry of
// the first-visit advice: the site's exact URL and the two phrases above. It is
// the one rule the verification page, its guide section and README.md are held
// to, so none of the three can lose the advice while another keeps it.
func firstVisitAdviceProblems(docPath, doc string) []string {
	text := collapsed(doc)

	var found []string

	if !strings.Contains(text, siteURL) {
		found = append(found, fmt.Sprintf("%s does not carry the exact URL %s: the first-visit advice is to type that URL rather than follow an http:// link, so %s spells it out (PRD #297 milestone 3)",
			docPath, siteURL, docPath))
	}

	lower := strings.ToLower(text)

	for _, want := range firstVisitAdvicePhrases {
		if !strings.Contains(lower, want.phrase) {
			found = append(found, fmt.Sprintf("%s does not carry the first-visit advice: it says no %q (PRD #297 milestones 3 and 4): %s",
				docPath, want.phrase, want.why))
		}
	}

	return found
}

// readmeSiteLink matches a Markdown link or an autolink whose target is the
// site's exact URL: the README points at the site with a link, not with bare
// text a reader cannot click.
var readmeSiteLink = regexp.MustCompile(`\]\(https://www\.shift2eu\.com/?\)|<https://www\.shift2eu\.com/?>`)

// TestReadmeCarriesTheSiteLinkAndTheFirstVisitAdvice pins README.md's pointer to
// the site: the site's exact URL as a link, the first-visit advice in the
// phrasing the verification page and its guide section carry, and no
// plain-HTTP link to the site, which would invite the very downgrade the advice
// warns against.
func TestReadmeCarriesTheSiteLinkAndTheFirstVisitAdvice(t *testing.T) {
	t.Parallel()

	readme := readDoc(t, os.DirFS(repoRoot), readmePath)

	t.Run("the site's exact URL and the first-visit advice", func(t *testing.T) {
		t.Parallel()

		for _, problem := range firstVisitAdviceProblems(readmePath, readme) {
			t.Error(problem)
		}
	})

	t.Run("the site's URL is a link", func(t *testing.T) {
		t.Parallel()

		if !readmeSiteLink.MatchString(readme) {
			t.Errorf("%s does not link %s: the site's address is a link a reader can follow, and the advice beside it says to type it rather than arrive through an http:// link (PRD #297 milestone 4)", readmePath, siteURL)
		}
	})

	t.Run("no plain-HTTP link to the site", func(t *testing.T) {
		t.Parallel()

		for _, plain := range []string{"http://www.shift2eu.com", "http://shift2eu.com"} {
			if strings.Contains(readme, plain) {
				t.Errorf("%s holds %q, want no plain-HTTP link to the site: following one is what the first-visit advice warns against (PRD #297 milestone 4)", readmePath, plain)
			}
		}
	})
}
