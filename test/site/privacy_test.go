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

// clearSiteDataSentence is the privacy page's sentence about Clear-Site-Data
// (issue #567, item 1): the site does not send the header, so a page served from
// a poisoned cache stays in the browser's cache until its max-age runs out, and
// the page says what holds rather than what it cannot do.
const clearSiteDataSentence = "since the site does not send Clear-Site-Data."

// coopSentence is the privacy page's Technical note on Cross-Origin-Opener-Policy
// (issue #567, item 2): the site does not send it, so a page that opened this one
// keeps its handle, and it says that rather than that it cannot.
const coopSentence = "One gap remains: the site does not send Cross-Origin-Opener-Policy, so"

// apexRedirectSentence is the downgrade risk's account of the apex redirect (issue
// #567, item 3; issue #601): the site's own front answers http://shift2eu.com with
// a plain-HTTP 308 that carries no HSTS, not the apex redirect's provider, whose
// HSTS the site cannot promise. The scheme is named because only that redirect
// lacks HSTS: the apex's HTTPS answer carries it (apexHTSTSSentence). The
// paragraph keeps the downgrade-risk marker the live check reads.
const apexRedirectSentence = "(answered by the site's own front, the same server as www.shift2eu.com, with a plain-HTTP 308 redirect that carries no HSTS)"

// apexHTSTSSentence is the downgrade risk's account of where HSTS already
// protects a visit (issue #601): https://shift2eu.com answers with its 308 to
// www.shift2eu.com and carries Strict-Transport-Security: max-age=31536000 for
// shift2eu.com alone, with no includeSubDomains, so a browser that has already
// been to the apex over HTTPS is protected there too. A page that left that out
// would understate the site's protection, and one that said the apex's redirect
// carried no HSTS would be false of the HTTPS one.
const apexHTSTSSentence = "at www.shift2eu.com or at https://shift2eu.com, whose 308 redirect to www.shift2eu.com carries HSTS for shift2eu.com alone"

// frameAncestorsSentence is the privacy page's framing guard (issue #567, item
// 4): the Content-Security-Policy header every HTTPS response carries refuses
// the framing, and the page script's own guard travels with the bundle. The
// header rides only on the TLS answers, which is why the sentence names HTTPS
// rather than every response: the scheme's plain redirect carries no headers.
const frameAncestorsSentence = "Every HTTPS response from www.shift2eu.com carries frame-ancestors 'none' in its Content-Security-Policy header, so a browser refuses to show the site inside another page. A meta Content Security Policy cannot carry frame-ancestors, so the page script also stops, and says why, when another page frames it: that guard travels with the bundle to any other origin."

// logsSentence is the privacy page's paragraph on what is logged (issue #567,
// item 5): the front keeps no access log and passes neither the visitor's
// address nor their cookies on, while Scaleway's platform - the layer that ends
// the encrypted connection and so receives every request before the front - can
// log the visitor's address, path and country, kept 1 day once the maintainer
// has set it and 7 until then. The page says what holds until the runbook's
// log-retention step runs, because that step can run only after Scaleway has
// logged a request; and it names the platform, not the front, as the layer that
// answers first, since the front sees only what the platform forwards (ADR 0045,
// What the server answers).
const logsSentence = "The site's front keeps no access log and passes neither your IP address nor your cookies on to the rest of the hosting. Scaleway's platform, which ends the encrypted connection and receives your request before the front does, can log your IP address, the path you request and your country. The maintainer sets Scaleway's logs to be kept 1 day, the shortest Scaleway allows, once the site has served its first requests (Scaleway creates its log store only then); until that is done, Scaleway's default of 7 days applies."

// oldPrivacyClaims are the claims the page made before issue #567 and must not
// make again, each with why it is gone: the site cannot send Clear-Site-Data and
// cannot send Cross-Origin-Opener-Policy were both false of the hosting after
// ADR 0045 (the site does not send either header, which is a decision, not a
// gap), the apex redirect's provider was the wrong server once the container
// answered the apex itself, the front answered no request first once Scaleway's
// Serverless Containers platform ended the TLS connection ahead of it and kept
// the logs that can name a visitor, and the page no longer says HSTS protects
// only a browser that visited www.shift2eu.com over HTTPS, since the apex's own
// HTTPS redirect carries it too (issue #601).
var oldPrivacyClaims = []struct{ claim, reason string }{
	{
		claim:  "cannot send Clear-Site-Data",
		reason: "the hosting sends no such header, which is a decision rather than a gap (issue #567, item 1)",
	},
	{
		claim:  "cannot send Cross-Origin-Opener-Policy",
		reason: "the hosting sends no such header either, which is a decision rather than a gap (issue #567, item 2)",
	},
	{
		claim:  "apex redirect's provider",
		reason: "the container holds both hostnames and answers the apex itself (issue #567, item 3)",
	},
	{
		claim:  "the server that answers every request first",
		reason: "Scaleway's platform ends the TLS connection ahead of the front and keeps the logs that can name a visitor (issue #567, item 5)",
	},
	{
		claim:  "HSTS on www.shift2eu.com protects only a browser",
		reason: "the apex's HTTPS 308 to www.shift2eu.com carries Strict-Transport-Security: max-age=31536000 for shift2eu.com alone, so a browser that has been to https://shift2eu.com is protected at the apex too; only the plain-HTTP redirect carries no HSTS (issue #601)",
	},
}

// TestPrivacyPageStatesWhatTheFrontAndTheLogsDo pins the five statements issue
// #567 corrects or adds to the built privacy page and the one issue #601 adds:
// what the site does not send (Clear-Site-Data, Cross-Origin-Opener-Policy), who
// answers the apex and which of its redirects carries HSTS, the framing guard the
// policy header carries, and what the front and Scaleway's logs hold. The
// statements are matched after collapsing white space, as the rest of this file
// does, so the page may wrap them as it likes.
func TestPrivacyPageStatesWhatTheFrontAndTheLogsDo(t *testing.T) {
	t.Parallel()

	text := collapseSpace(theBundle(t).html(t, privacyPageName).body(t).textContent())

	tests := []struct {
		name     string
		sentence string
		expected string
	}{
		{
			name:     "Clear-Site-Data is not sent",
			sentence: clearSiteDataSentence,
			expected: "the page must say that the site does not send Clear-Site-Data: the hosting sends no such header, so the words describe the site rather than a gap it cannot close (issue #567, item 1)",
		},
		{
			name:     "Cross-Origin-Opener-Policy is not sent",
			sentence: coopSentence,
			expected: "the page must say that the site does not send Cross-Origin-Opener-Policy: the hosting sends no such header, so a page that opened this one keeps its handle because the site chose not to send it (issue #567, item 2)",
		},
		{
			name:     "the apex redirect is the site's own front",
			sentence: apexRedirectSentence,
			expected: "the downgrade-risk statement must say that the site's own front answers http://shift2eu.com with a plain-HTTP 308 that carries no HSTS, not the apex redirect's provider: the container holds both hostnames and answers the apex itself, and only the plain-HTTP 308 lacks HSTS (issue #567, item 3; issue #601)",
		},
		{
			name:     "the apex's HTTPS redirect carries HSTS",
			sentence: apexHTSTSSentence,
			expected: "the downgrade-risk statement must say that https://shift2eu.com's 308 redirect to www.shift2eu.com carries HSTS for shift2eu.com alone, so a browser that has been to the apex over HTTPS is protected there too: the apex's HTTPS answer sends Strict-Transport-Security: max-age=31536000 without includeSubDomains (issue #601)",
		},
		{
			name:     "frame-ancestors is in the header",
			sentence: frameAncestorsSentence,
			expected: "the Technical note must say that every HTTPS response's Content-Security-Policy header carries frame-ancestors 'none', so a browser refuses to frame the site, and that the page script's guard travels with the bundle (issue #567, item 4)",
		},
		{
			name:     "the front and Scaleway's logs",
			sentence: logsSentence,
			expected: "the \"What the site cannot promise\" list must say, after the hosting-provider paragraph, that the front keeps no access log and passes no IP address and no cookie on, that Scaleway's platform, which ends the connection before the front, can log the IP address, the path and the country, and that the maintainer sets those logs to 1 day once the site has served its first requests, Scaleway's default of 7 until then (issue #567, item 5)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(text, collapseSpace(tc.sentence)) {
				t.Errorf("the privacy page does not read %q: %s", tc.sentence, tc.expected)
			}
		})
	}
}

// TestPrivacyPageDropsTheOldClaims pins the other side of issues #567 and #601:
// the claims the page used to make are gone, so a rewrite that keeps one beside
// the corrected sentence cannot leave the page saying both that the site cannot
// send a header and that it does not, or that HSTS protects only a browser that
// has visited www.shift2eu.com over HTTPS.
func TestPrivacyPageDropsTheOldClaims(t *testing.T) {
	t.Parallel()

	text := theBundle(t).html(t, privacyPageName).body(t).textContent()

	for _, tc := range oldPrivacyClaims {
		t.Run(tc.claim, func(t *testing.T) {
			t.Parallel()

			if strings.Contains(collapseSpace(text), tc.claim) {
				t.Errorf("the privacy page still says %q, want it gone: %s", tc.claim, tc.reason)
			}
		})
	}
}
