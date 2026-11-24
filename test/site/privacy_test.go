package site_test

import (
	"slices"
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

// aggregateRequestCounts is the sentence the privacy page must carry about what
// the maintainer learns from the hosting provider (PRD #297 milestone 4, ADR
// 0045): the site moves behind a Serverless Container whose configuration turns
// no access log on, so the one thing the maintainer reads is the provider's
// aggregate request counts, and the page says which facts are not in them and
// for how long they are kept.
const aggregateRequestCounts = "To see how much the site is used, the maintainer reads the hosting provider's aggregate request counts (no IP address, no user agent, no cookie, kept 31 days)."

// TestPrivacyPageNamesTheAggregateRequestCounts pins that sentence on the built
// privacy page: the maintainer reads the hosting provider's aggregate request
// counts, and the page says what they hold none of and for how long they are
// kept.
func TestPrivacyPageNamesTheAggregateRequestCounts(t *testing.T) {
	t.Parallel()

	page := theBundle(t).html(t, privacyPageName)
	text := collapseSpace(page.body(t).textContent())

	if !strings.Contains(text, aggregateRequestCounts) {
		t.Errorf("the privacy page does not read %q, want it to say that the maintainer reads the hosting provider's aggregate request counts, with no IP address, no user agent and no cookie in them, kept 31 days (PRD #297 milestone 4, ADR 0045)",
			aggregateRequestCounts)
	}
}

// TestPrivacyPageClaimsNoPathCountsAndNoAccessLog pins the other side of that
// sentence: the maintainer reads aggregate counts only, so the page may not say
// the site counts requests per path or keeps an access log. Both the container's
// configuration, which turns no log directive on, and the sentence above
// promise it, so a page that claimed either would be wrong about the site.
//
// The assertion is on the words alone, both ways: an occurrence of "per path"
// or "access log" passes only when the word right before it is a negation, so
// the page may add "no access log" but never state one, and a page that mentions
// neither passes too. Nothing has to be present, so a rewrite of the prose
// around the sentences cannot silently stop this from checking anything, and the
// negation-aware form leaves the page free to name what it does not do.
func TestPrivacyPageClaimsNoPathCountsAndNoAccessLog(t *testing.T) {
	t.Parallel()

	page := theBundle(t).html(t, privacyPageName)
	text := collapseSpace(page.body(t).textContent())

	for _, phrase := range []string{"per path", "per-path", "access log", "access-log"} {
		for _, claim := range unnegatedClaims(text, phrase) {
			t.Errorf("the privacy page says %q, want the words %q only in a negation, if at all: the site keeps no access log and counts requests in aggregate, never per path (PRD #297 milestone 4, ADR 0045)",
				claim, phrase)
		}
	}
}

// unnegatedClaims returns each occurrence of phrase in text that no negation is
// the word right before, with the three words before it, so "no access log" is
// no claim and "keeps an access log" is.
func unnegatedClaims(text, phrase string) []string {
	var claims []string

	lower := strings.ToLower(text)

	for rest := lower; ; {
		i := strings.Index(rest, phrase)
		if i < 0 {
			return claims
		}

		words := strings.Fields(rest[:i])
		if len(words) == 0 || !isNegation(words[len(words)-1]) {
			before := words
			if len(before) > 3 {
				before = before[len(before)-3:]
			}

			claims = append(claims, strings.Join(append(append([]string(nil), before...), phrase), " "))
		}

		rest = rest[i+len(phrase):]
	}
}

// isNegation reports whether word, with the punctuation around it trimmed, is
// the negation that turns a claim into its absence in English prose.
func isNegation(word string) bool {
	switch strings.Trim(word, `,.;:()"'`) {
	case "no", "not", "nor", "never", "without":
		return true
	}

	return false
}

// TestUnnegatedClaims pins the helper the privacy page's negative assertion
// rests on, so a bug in it cannot leave that assertion checking nothing: a claim
// must be caught, a negated one let through, and a phrase the page does not hold
// is no claim at all.
func TestUnnegatedClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		text   string
		phrase string
		want   []string
	}{
		{
			name:   "a claim",
			text:   "The maintainer keeps an access log of every visit.",
			phrase: "access log",
			want:   []string{"maintainer keeps an access log"},
		},
		{
			name:   "a negated claim",
			text:   "The maintainer keeps no access log.",
			phrase: "access log",
		},
		{
			name:   "a claim after a negation about something else",
			text:   "No cookie is set, and the provider counts requests per path.",
			phrase: "per path",
			want:   []string{"provider counts requests per path"},
		},
		{
			name:   "a phrase the text does not hold",
			text:   "The maintainer reads aggregate request counts.",
			phrase: "access log",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := unnegatedClaims(tc.text, tc.phrase); !slices.Equal(got, tc.want) {
				t.Errorf("unnegatedClaims(%q, %q) = %q, want %q", tc.text, tc.phrase, got, tc.want)
			}
		})
	}
}
