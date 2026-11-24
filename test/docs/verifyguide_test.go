// Pins the site's release verification page against the release verification
// guide it summarises (PRD #297 milestone 3, PR D): the page is a summary of
// the guide's "Verify the site" section, so the section alone is what the page's
// commands, flags, steps and advice are held against. A check of the whole guide
// would pass on another section's words, such as the image's own
// `gh attestation verify`, and would stay green while the site's section lost
// the command or the flags the page shows.
//
// Every command the page shows is a command the section spells out, and the
// flags the page shows for the command that verifies the bundle sit in the
// section's own `gh attestation verify` command, the one whose arguments name
// the bundle. Each step's page phrase is checked in the page, and its guide
// phrase in the section, and the page's unconditional notice and the section
// carry the site's exact URL and the first-visit advice against an attacker who
// strips HTTPS from an http:// link: a phrase either document lost fails the
// check, so neither is read and left unasserted. The notice is read as its own
// element, not as the page: the conditional notice and the steps repeat its
// words, so a check of the whole page would stay green on a notice that lost
// them.
//
// The one exception is the page's links to documents of this repository, which
// are checked against the guide as a whole: the page's other repository links
// are its navigation, pointing at the inventory guide, the repository and its
// releases, and the site section has no reason to carry them.
//
// The check reads the two documents as the repository holds them, the page's
// source and the guide's Markdown, and so needs no site bundle: test/site builds
// the bundle and pins what the page's links resolve to at a release's tag,
// which this file does not repeat.
package docs_test

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	// verifyPagePath is the verification page's source, and verifyGuidePath the
	// guide whose exact commands the page summarises (PRD #297 milestone 3).
	verifyPagePath  = "site/verify.html"
	verifyGuidePath = "docs/guide/verify-releases.md"

	// verifyGuideSection is the guide's heading of the section the page
	// summarises: the page's commands, steps and advice are checked inside it
	// and nowhere else of the guide.
	verifyGuideSection = "## Verify the site"

	// bundleAttestationCommand is the command that verifies the site bundle's
	// build provenance: the page shows it, and the section's form of it must be
	// the one whose arguments name the bundle, never another artifact's.
	bundleAttestationCommand = "gh attestation verify"

	// repositoryURL is the repository's web home: every link the page makes to
	// a document of this repository, or to the releases page, starts with it.
	repositoryURL = "https://github.com/timothymamo/shift2eu"

	// siteURL is the address the page and the guide tell a first-time visitor
	// to type themselves rather than to reach through a link (PRD #297,
	// milestone 3).
	siteURL = "https://www.shift2eu.com"
)

// TestVerificationGuideAgreesWithThePage pins that the page and the guide's
// site section say the same thing (PRD #297 milestone 3, PR D): the page's
// commands with their flags, its steps, the site's URL and the first-visit
// advice all appear in that section, which is the page's exact form of the
// check, and the repository documents the page links appear in the guide.
func TestVerificationGuideAgreesWithThePage(t *testing.T) {
	t.Parallel()

	page := readDoc(t, os.DirFS(repoRoot), verifyPagePath)
	guide := readDoc(t, os.DirFS(repoRoot), verifyGuidePath)

	// The page summarises the guide's site section, so the section is what the
	// page's claims are held against: a command, a flag or a phrase that only
	// the image's or an archive's section carries is not the site's.
	section := docSection(t, guide, verifyGuidePath, verifyGuideSection)

	codes := codeElements(page)
	if len(codes) == 0 {
		t.Fatalf("%s shows no <code> element, so no command of the page was checked against the guide", verifyPagePath)
	}

	groups := pageCommandGroups(t, codes)

	// The page's notice, the element that states the advice for every visitor:
	// the page's own side of the notice check is read from it and from no other
	// part of the page.
	pageNotice := noticeElement(t, page)

	t.Run("the section's attestation command carries the page's commands and flags", func(t *testing.T) {
		t.Parallel()

		for _, problem := range sectionCommandProblems(section, groups) {
			t.Error(problem)
		}
	})

	// The check above is only worth its claims if a deleted command or flag is
	// a problem: a section that lost the attestation's flag, or the command
	// itself, must fail it.
	t.Run("a command or a flag deleted from the section is a problem", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name    string
			section string
			want    string
		}{
			{
				name:    "the section without the attestation's flag",
				section: strings.ReplaceAll(section, " "+pageFlagDenySelfHostedRunners, ""),
				want:    pageFlagDenySelfHostedRunners,
			},
			{
				name:    "a section without the attestation command",
				section: "The bundle is attested, and its manifest lists every file.\n",
				want:    bundleAttestationCommand,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				problems := strings.Join(sectionCommandProblems(tc.section, groups), "\n")

				if !strings.Contains(problems, tc.want) {
					t.Errorf("%s reports no problem with %q, though %s does not carry it: the check must catch a section that lost what the page shows (PRD #297 milestone 3)\nproblems: %s",
						verifyGuideSection, tc.want, tc.name, problems)
				}
			})
		}
	})

	// The page's steps and notices, each with the phrase the section must carry
	// for it: the page is a summary, and a step the section lost - which release
	// to check, how a file's digest is compared, how the bundle is served - is a
	// step that reads as checked only while some other part of the guide
	// carries the words. The page's own phrase is checked too, so a step the
	// page dropped is a problem and not only a stale error message. The page is
	// templated, so a phrase a template action interrupts - the version it shows,
	// the archive's name - is pinned up to the interruption. The phrases are
	// matched without case, so a sentence may open with one.
	t.Run("the section and the page carry the steps", func(t *testing.T) {
		t.Parallel()

		steps := []struct {
			page  string
			guide string
			why   string
		}{
			{
				page:  "Note the version this page shows",
				guide: "that is the release to check",
				why:   "the page shows the version it was built from, and the section says that this version is the release to check",
			},
			{
				page:  "download that release's site bundle",
				guide: "gh release download",
				why:   "the section's first step downloads that release's bundle, the command the page's first step summarises",
			},
			{
				page:  "from the repository's releases",
				guide: "release asset",
				why:   "where the bundle comes from: the release's own asset of the repository, not an archive built or hosted elsewhere",
			},
			{
				page:  "verify its attestation with the GitHub CLI",
				guide: bundleAttestationCommand,
				why:   "the command that verifies the bundle's build provenance, which the page names",
			},
			{
				page:  "Read manifest.json in the bundle",
				guide: "manifest.json",
				why:   "the manifest is what lists the bundle's files with their digests",
			},
			{
				page:  "compare each file's SHA-256 with the manifest's",
				guide: "sha256sum --check",
				why:   "the command that compares the fetched files with the manifest's digests",
			},
			{
				page:  "The check is one point in time from one vantage point",
				guide: "one point in time from one vantage point",
				why:   "what the check shows and what it does not: the HTML is the root of trust and nothing pins it",
			},
			{
				page:  "can stay in a browser's cache for up to its max-age",
				guide: "max-age",
				why:   "the stale-cache caveat the page repeats: a released page can outlast the repair in a visitor's browser",
			},
			{
				page:  "can register a service worker",
				guide: "service worker",
				why:   "the other caveat a page uploaded with a leaked key carries, and the one that can outlast the repair",
			},
			{
				page:  "A path outside the manifests is not checked",
				guide: "outside the manifests",
				why:   "the limit of the check: a look-alike page at a path no manifest names is not caught",
			},
			{
				page:  "Run the bundle yourself",
				guide: "run the bundle yourself",
				why:   "the page's card and the section's advice: the attested files run without the site's host",
			},
			{
				page:  "serve its directory with any static file server on your own machine",
				guide: "static file server",
				why:   "the section says what serves the verified bundle, not only that it runs anywhere",
			},
			{
				page:  "then open it in your browser",
				guide: "python3 -m http.server",
				why:   "the section's runnable example of a static file server, so the page's advice is a command a reader can run",
			},
		}

		// stepProblems returns what the page and the section fail to carry of the
		// steps: a phrase the page no longer carries, and a phrase the section no
		// longer carries. Both documents are checked, so a step either lost is
		// reported, and not only a section that lost the words the page shows.
		stepProblems := func(page, section string) []string {
			pageLower := strings.ToLower(collapsed(page))
			sectionLower := strings.ToLower(collapsed(section))

			var found []string

			for _, want := range steps {
				if !strings.Contains(pageLower, strings.ToLower(want.page)) {
					found = append(found, fmt.Sprintf("%s no longer carries %q, which the guide's %q section summarises: the page's step and the section's are the same step, so the page keeps the words this check holds it to (PRD #297 milestone 3)",
						verifyPagePath, want.page, verifyGuideSection))
				}

				if !strings.Contains(sectionLower, strings.ToLower(want.guide)) {
					found = append(found, fmt.Sprintf("%s's %q section carries no %q, though %s says %q there: %s (PRD #297 milestone 3)",
						verifyGuidePath, verifyGuideSection, want.guide, verifyPagePath, want.page, want.why))
				}
			}

			return found
		}

		for _, problem := range stepProblems(page, section) {
			t.Error(problem)
		}

		// The check reads the page as well as the section, so it is worth its
		// claims only while a page that lost a step is a problem: this copy is
		// the page with its run-yourself step reworded, and the check must name
		// the step it lost. The problem asserted on is the page's own, so a
		// section-side message that happens to carry the step's words cannot
		// stand in for it.
		t.Run("a page that lost a step is a problem", func(t *testing.T) {
			t.Parallel()

			const step = "Run the bundle yourself"

			got := strings.Join(stepProblems(strings.ReplaceAll(page, step, "Run it"), section), "\n")

			if want := fmt.Sprintf("%s no longer carries %q", verifyPagePath, step); !strings.Contains(got, want) {
				t.Errorf("%s reports no problem with %q, though %s no longer carries it: the check reads the page, so a page that dropped a step must fail it with %q, not only a section that did (PRD #297 milestone 3)\nproblems: %s",
					verifyPagePath, step, verifyPagePath, want, got)
			}
		})
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

	t.Run("the page and the section carry the site's URL and the first-visit advice", func(t *testing.T) {
		t.Parallel()

		// The page's notice: type https://www.shift2eu.com yourself rather than
		// following an http:// link, because on a first visit a network attacker
		// can strip HTTPS. The page and the guide may word the advice in their own
		// style, so only these two phrases are pinned besides the URL: the
		// occasion, and the attack. They are matched without case, so the advice
		// may open a sentence.
		phrases := []struct {
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
		}

		// noticeProblems returns what a document fails to carry of the notice: the
		// site's exact URL, and the two phrases of the advice. The page's
		// unconditional notice carries the advice and so does the section that
		// repeats it, and both are checked: a notice that lost the advice fails as
		// the section that lost it does, and neither is read and left unasserted.
		noticeProblems := func(docPath, doc string) []string {
			text := collapsed(doc)

			var found []string

			if !strings.Contains(text, siteURL) {
				found = append(found, fmt.Sprintf("%s does not carry the exact URL %s: the first-visit advice is to type that URL, so %s spells it out (PRD #297 milestone 3)",
					docPath, siteURL, docPath))
			}

			lower := strings.ToLower(text)

			for _, want := range phrases {
				if !strings.Contains(lower, want.phrase) {
					found = append(found, fmt.Sprintf("%s does not carry the first-visit advice: it says no %q, though the page and the guide's %q section carry the advice (PRD #297 milestone 3): %s",
						docPath, want.phrase, verifyGuideSection, want.why))
				}
			}

			return found
		}

		for _, doc := range []struct {
			docPath string
			text    string
		}{
			{docPath: verifyPagePath, text: pageNotice},
			{docPath: verifyGuidePath, text: section},
		} {
			for _, problem := range noticeProblems(doc.docPath, doc.text) {
				t.Error(problem)
			}
		}

		// The notice's side is worth its claims only while a notice that lost the
		// advice is a problem: these copies are the page's notice without the
		// site's URL and without the occasion, and the check must name each. The
		// page's conditional notice and its steps still carry both, so a check of
		// the whole page would stay green on either copy.
		t.Run("a notice that lost the advice is a problem", func(t *testing.T) {
			t.Parallel()

			for _, tc := range []struct {
				name   string
				notice string
				want   string
			}{
				{
					name:   "a notice without the site's URL",
					notice: strings.ReplaceAll(pageNotice, siteURL, "the site"),
					want:   siteURL,
				},
				{
					name:   "a notice without the first visit",
					notice: strings.ReplaceAll(pageNotice, "on a first visit", "now"),
					want:   "first visit",
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					got := strings.Join(noticeProblems(verifyPagePath, tc.notice), "\n")

					if !strings.Contains(got, tc.want) {
						t.Errorf("%s reports no problem with %q, though its unconditional notice no longer carries it: the check reads that notice, so %s must fail it (PRD #297 milestone 3)\nproblems: %s",
							verifyPagePath, tc.want, tc.name, got)
					}
				})
			}
		})
	})
}

// docSection returns the section of a Markdown document under the top-level
// heading: its text from the heading's line to the line of the next top-level
// heading, or to the document's end. The test fails when the document holds no
// such heading of its own line: a section that moved is one the page's summary
// is no longer checked against.
func docSection(t *testing.T, doc, docPath, heading string) string {
	t.Helper()

	needle := "\n" + heading + "\n"

	start := strings.Index(doc, needle)
	if start < 0 {
		t.Fatalf("%s holds no %q heading: the page summarises that section, so the guide keeps it and this check reads it (PRD #297 milestone 3)", docPath, heading)
	}

	start += len(needle)

	if next := strings.Index(doc[start:], "\n## "); next >= 0 {
		return doc[start : start+next]
	}

	return doc[start:]
}

// collapsed returns s as one line: its fields joined by single spaces. A
// Markdown paragraph and a command the guide wraps over two lines are the same
// words, so a phrase and a command are compared on the words alone.
func collapsed(s string) string {
	return strings.Join(strings.Fields(s), " ")
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

// noticeElementPattern matches the page's unconditional notice and its content:
// the first <p class="notice ..."> element, the one every visitor reads. The
// page's other notice, the one inside its {{if .HSTSWaived}} action, carries an
// id attribute before its class and so is not matched here, and neither is any
// element that follows the notice.
var noticeElementPattern = regexp.MustCompile(`(?s)<p class="notice [^"]*">(.*?)</p>`)

// noticeElement returns the text of the page's unconditional notice. The test
// fails when the page holds no such element: the notice's advice is what the
// page's side of the notice check is held to, and a check of the whole page
// would pass on the conditional notice's or a step's words while the notice
// every visitor reads lost them.
func noticeElement(t *testing.T, page string) string {
	t.Helper()

	match := noticeElementPattern.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("%s holds no <p class=\"notice \"> element: the page states the first-visit advice in its unconditional notice, so the page keeps that element and this check reads it (PRD #297 milestone 3)", verifyPagePath)
	}

	return strings.TrimSpace(match[1])
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

// pageFlagDenySelfHostedRunners is the flag that keeps the attestation's
// verifier on GitHub's own runners: the page shows it, and the guide's command
// must carry it (PRD #297 milestone 3).
const pageFlagDenySelfHostedRunners = "--deny-self-hosted-runners"

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

// pageCommandGroup is one command of the page with the flags the page shows for
// it: the page writes the command in one <code> element and the flags it takes
// in the elements that follow it.
type pageCommandGroup struct {
	command string
	flags   []string
}

// pageCommandGroups returns the page's commands in document order, each with the
// flags that follow it. The test fails on a flag no command precedes: the page
// shows a flag only as the flag of a command.
func pageCommandGroups(t *testing.T, codes []string) []pageCommandGroup {
	t.Helper()

	var groups []pageCommandGroup

	for _, code := range codes {
		command, flags := pageCommand(code)

		if command != "" {
			groups = append(groups, pageCommandGroup{command: command, flags: flags})

			continue
		}

		if len(flags) == 0 {
			continue
		}

		if len(groups) == 0 {
			t.Fatalf("%s shows the flag %q before any command: the page shows a flag as a flag of a command, which this check reads it as", verifyPagePath, flags[0])
		}

		groups[len(groups)-1].flags = append(groups[len(groups)-1].flags, flags...)
	}

	return groups
}

// sectionCommandProblems returns what the section's commands fail to carry of
// the page's groups: a command the section never spells out, a flag the page
// shows for a command that the section's own form of it lacks, and - for the
// command that verifies the bundle - a command whose arguments name something
// else than the bundle, such as the image's OCI reference.
func sectionCommandProblems(section string, groups []pageCommandGroup) []string {
	commands := sectionCommands(section)

	var problems []string

	for _, group := range groups {
		candidates := commandsStartingWith(commands, group.command)

		if group.command == bundleAttestationCommand {
			var bundles []string

			for _, candidate := range candidates {
				if namesTheBundle(candidate) {
					bundles = append(bundles, candidate)
				}
			}

			if len(bundles) == 0 {
				problems = append(problems, fmt.Sprintf("%s's %q section carries no %q command whose arguments name the bundle: the page verifies the site's bundle, so the section's own command of that name must be the bundle's (PRD #297 milestone 3)",
					verifyGuidePath, verifyGuideSection, group.command))

				continue
			}

			candidates = bundles
		}

		if len(candidates) == 0 {
			problems = append(problems, fmt.Sprintf("%s shows the command %q, which %s's %q section does not spell out: the page summarises that section, so the section holds the command itself and no other section stands in for it (PRD #297 milestone 3)",
				verifyPagePath, group.command, verifyGuidePath, verifyGuideSection))

			continue
		}

		missing, closest := missingFlags(candidates, group.flags)
		if len(missing) == 0 {
			continue
		}

		problems = append(problems, fmt.Sprintf("%s shows the flag %s for %q, which none of the section's own commands carries: %q shows the section's command without %s, so the page and the guide's commands are held to the same flags (PRD #297 milestone 3)",
			verifyPagePath, strings.Join(missing, ", "), group.command, closest, strings.Join(missing, ", ")))
	}

	return problems
}

// missingFlags returns the flags that no candidate command carries, and the
// candidate that comes closest, which carries all but those.
func missingFlags(candidates, flags []string) (missing []string, closest string) {
	first := true

	for _, candidate := range candidates {
		fields := strings.Fields(candidate)

		var absent []string

		for _, flag := range flags {
			if !slices.Contains(fields, flag) {
				absent = append(absent, flag)
			}
		}

		if len(absent) == 0 {
			return nil, candidate
		}

		if first || len(absent) < len(missing) {
			missing, closest, first = absent, candidate, false
		}
	}

	return missing, closest
}

// bashBlockPattern matches a bash code block of the guide and its content.
var bashBlockPattern = regexp.MustCompile("(?s)```bash\n(.*?)\n```")

// sectionCommands returns the logical commands of the section's bash code
// blocks: a command the guide wraps with a trailing backslash is joined to the
// line after it, and a comment or a blank line is no command.
func sectionCommands(section string) []string {
	var commands []string

	for _, block := range bashBlockPattern.FindAllStringSubmatch(section, -1) {
		var joined string

		for _, line := range strings.Split(block[1], "\n") {
			line = strings.TrimSpace(line)

			if joined == "" && (line == "" || strings.HasPrefix(line, "#")) {
				continue
			}

			if wrapped, ok := strings.CutSuffix(line, `\`); ok {
				joined += strings.TrimSpace(wrapped) + " "

				continue
			}

			joined += line

			if command := strings.TrimSpace(joined); command != "" && !strings.HasPrefix(command, "#") {
				commands = append(commands, collapsed(command))
			}

			joined = ""
		}

		if command := strings.TrimSpace(joined); command != "" {
			commands = append(commands, collapsed(command))
		}
	}

	return commands
}

// commandsStartingWith returns the commands that are the command, or the command
// and its arguments.
func commandsStartingWith(commands []string, command string) []string {
	var found []string

	for _, candidate := range commands {
		if candidate == command || strings.HasPrefix(candidate, command+" ") {
			found = append(found, candidate)
		}
	}

	return found
}

// namesTheBundle reports whether a command's arguments name the site's bundle:
// the section's own variable for it, or the archive's name.
func namesTheBundle(command string) bool {
	return strings.Contains(command, "BUNDLE") || strings.Contains(command, "shift2eu-site_")
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
