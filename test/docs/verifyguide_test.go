// Pins the site's release verification page against the release verification
// guide it summarises (PRD #297 milestone 3, PR D): the page is a summary of
// the guide, so every command the page shows is a command the guide spells out
// with the same flags, every document of this repository the page links is one
// the guide links too, and both carry the site's exact URL and the first-visit
// advice against an attacker who strips HTTPS from an http:// link.
//
// The check reads the two documents as the repository holds them, the page's
// source and the guide's Markdown, and so needs no site bundle: test/site builds
// the bundle and pins what the page's links resolve to at a release's tag,
// which this file does not repeat.
package docs_test

import (
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

const (
	// verifyPagePath is the verification page's source, and verifyGuidePath the
	// guide whose exact commands the page summarises (PRD #297 milestone 3).
	verifyPagePath  = "site/verify.html"
	verifyGuidePath = "docs/guide/verify-releases.md"

	// repositoryURL is the repository's web home: every link the page makes to
	// a document of this repository, or to the releases page, starts with it.
	repositoryURL = "https://github.com/timothymamo/shift2eu"

	// siteURL is the address the page and the guide tell a first-time visitor
	// to type themselves rather than to reach through a link (PRD #297,
	// milestone 3).
	siteURL = "https://www.shift2eu.com"
)

// TestVerificationGuideAgreesWithThePage pins that the page and the guide say
// the same thing (PRD #297 milestone 3, PR D): the page's commands with their
// flags, the repository documents the page links, the site's URL and the
// first-visit advice all appear in the guide, which is the page's exact form of
// the check.
func TestVerificationGuideAgreesWithThePage(t *testing.T) {
	t.Parallel()

	page := readDoc(t, os.DirFS(repoRoot), verifyPagePath)
	guide := readDoc(t, os.DirFS(repoRoot), verifyGuidePath)

	// A command the page wraps over two lines is one line in the guide, and a
	// Markdown page wraps its prose wherever the line ends: compare the words,
	// not the line breaks.
	guideWords := strings.Join(strings.Fields(guide), " ")

	t.Run("every command the page shows is a command of the guide", func(t *testing.T) {
		t.Parallel()

		codes := codeElements(page)
		if len(codes) == 0 {
			t.Fatalf("%s shows no <code> element, so no command of the page was checked against the guide", verifyPagePath)
		}

		for _, code := range codes {
			command, flags := pageCommand(code)

			if command != "" && !strings.Contains(guideWords, command) {
				t.Errorf("%s shows the command %q, which %s does not spell out: the page summarises the guide, so the guide holds the command itself", verifyPagePath, command, verifyGuidePath)
			}

			for _, flag := range flags {
				if !strings.Contains(guideWords, flag) {
					t.Errorf("%s shows the flag %q, which %s does not carry: the page and the guide's commands are held to the same flags", verifyPagePath, flag, verifyGuidePath)
				}
			}
		}
	})

	t.Run("every repository document the page links is linked by the guide", func(t *testing.T) {
		t.Parallel()

		guideDocs := guideDocumentLinks(guide)

		for _, href := range pageRepositoryLinks(page) {
			doc, ok := repositoryDocumentPath(href)
			if !ok {
				// A page of the repository that is no document of it, such as
				// its releases page: the guide names it by the same URL.
				if !strings.Contains(guide, href) {
					t.Errorf("%s links %s, which %s does not name: what the page points a visitor at, the guide does too", verifyPagePath, href, verifyGuidePath)
				}

				continue
			}

			if doc == verifyGuidePath {
				// The page links the guide itself, a link the guide cannot make;
				// test/site's TestVerificationPageLinksItsGuide pins it.
				continue
			}

			if !guideDocs[doc] {
				t.Errorf("%s links %s, which %s does not link: the page points at the guide for the exact commands, so the guide carries the page's own pointers too", verifyPagePath, doc, verifyGuidePath)
			}
		}
	})

	t.Run("the guide carries the site's URL and the first-visit advice", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(guide, siteURL) {
			t.Errorf("%s does not carry the exact URL %s: the first-visit advice is to type that URL, so the guide spells it out as the page does (PRD #297 milestone 3)", verifyGuidePath, siteURL)
		}

		// The page's notice: type https://www.shift2eu.com yourself rather than
		// following an http:// link, because on a first visit a network attacker
		// can strip HTTPS. The guide may word the advice in its own style, so
		// only these two phrases are pinned: the occasion, and the attack. They
		// are matched without case, so the advice may open a sentence.
		guideLower := strings.ToLower(guide)

		for _, want := range []struct {
			phrase string
			why    string
		}{
			{
				phrase: "first visit",
				why:    "the advice is about a first visit, when a browser has seen no HSTS for the site and an attacker on the network can still replace the page",
			},
			{
				phrase: "strip https",
				why:    "the attack the advice names: an http:// link lets a network attacker strip HTTPS and serve a look-alike page",
			},
		} {
			if !strings.Contains(guideLower, want.phrase) {
				t.Errorf("%s does not carry the first-visit advice: it says no %q, though %s carries the advice (PRD #297 milestone 3)", verifyGuidePath, want.phrase, verifyPagePath)
			}
		}
	})
}

// codeElementPattern matches a <code> element of the page and its content. The
// page is templated, but no template action stands inside a <code> element, and
// none nests another element in one.
var codeElementPattern = regexp.MustCompile(`(?s)<code>(.*?)</code>`)

// codeElements returns the text of every <code> element of the page, its HTML
// entities decoded, in document order.
func codeElements(page string) []string {
	matches := codeElementPattern.FindAllStringSubmatch(page, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, strings.TrimSpace(codeEntities.Replace(match[1])))
	}

	return codes
}

// codeEntities decodes the HTML entities a command's text is escaped with: the
// angle brackets of the page's placeholders, and an ampersand.
var codeEntities = strings.NewReplacer(
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
	"&amp;", "&",
)

// pageCommand returns the command one <code> element of the page shows and the
// flags it shows with it: the words up to the first flag are the command, and
// every word from there that starts with a hyphen is a flag. What the page
// writes as a flag's argument, such as `refs/tags/<tag>`, and the backslash of
// a wrapped line are not checked: the guide spells them for the shell the
// command runs in, with a variable of its own.
func pageCommand(code string) (command string, flags []string) {
	fields := strings.Fields(code)

	first := len(fields)
	for i, field := range fields {
		if strings.HasPrefix(field, "-") {
			first = i

			break
		}
	}

	var words []string

	for _, field := range fields[:first] {
		if field != `\` {
			words = append(words, field)
		}
	}

	for _, field := range fields[first:] {
		if strings.HasPrefix(field, "-") {
			flags = append(flags, field)
		}
	}

	return strings.Join(words, " "), flags
}

// anchorPattern matches an anchor element of the page and the href it carries.
var anchorPattern = regexp.MustCompile(`<a\s[^>]*href="([^"]*)"`)

// pageRepositoryLinks returns the hrefs of the page's links that name this
// repository, in document order: its documents, its releases page and the
// repository itself.
func pageRepositoryLinks(page string) []string {
	var hrefs []string

	for _, match := range anchorPattern.FindAllStringSubmatch(page, -1) {
		if strings.HasPrefix(match[1], repositoryURL) {
			hrefs = append(hrefs, match[1])
		}
	}

	return hrefs
}

// repositoryDocumentPath returns the path of the repository document a link to
// this repository names: the part after the /blob/<ref>/ of a GitHub blob URL.
// ok is false for a link that names no document, which is the repository's home
// and its releases page, and for the ref the page names, which is the build's
// own (`{{.Ref}}` in the source, the release's tag or main once built).
func repositoryDocumentPath(href string) (doc string, ok bool) {
	rest, found := strings.CutPrefix(href, repositoryURL)
	if !found {
		return "", false
	}

	blob, found := strings.CutPrefix(rest, "/blob/")
	if !found {
		return "", false
	}

	_, doc, found = strings.Cut(blob, "/")

	return doc, found
}

// markdownLinkPattern matches a Markdown link's target. The guide holds no
// image, and no link target with a parenthesis in it.
var markdownLinkPattern = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// guideDocumentLinks returns the repository paths the guide links, resolved
// against the guide's own directory: a relative target names a document of this
// repository, and an absolute one names it only when it is a GitHub blob URL of
// the repository. A link to another site, such as cosign's, names no document
// here.
func guideDocumentLinks(guide string) map[string]bool {
	links := make(map[string]bool)

	for _, match := range markdownLinkPattern.FindAllStringSubmatch(guide, -1) {
		target, _, _ := strings.Cut(match[1], "#")

		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			if doc, ok := repositoryDocumentPath(target); ok {
				links[doc] = true
			}

			continue
		}

		links[path.Join(path.Dir(verifyGuidePath), target)] = true
	}

	return links
}
