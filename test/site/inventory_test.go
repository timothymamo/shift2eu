package site_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// This file holds the checks of the site's inventory page (issue #373, item 15):
// the page that tells a visitor how to build inventory.json for each source. Its
// commands are the inventory guide's commands, never a hand-typed copy: the
// source page site/inventory.html carries a placeholder, and ci/sitebuild fills
// it in from docs/guide/inventory.md at build time. The check reads both and
// compares them, so the two cannot drift.
//
// The page chooses a source with three buttons, never a dropdown, and with no
// script all three sources are shown: the buttons only hide the sections a
// visitor did not choose.

// inventoryPageName is the bundle's and site/'s name of the inventory page.
const inventoryPageName = "inventory.html"

// guideProgramsDirectory is where the guide's programs sit, relative to the
// repository root.
const guideProgramsDirectory = "docs/guide/inventory/"

// guideSource is one source the page documents: the key its section and its
// program are named by; the name its button shows; and the guide's two headings
// whose bash blocks are that source's commands.
type guideSource struct {
	key     string
	name    string
	collect string
	build   string
}

// guideSources are the three sources, in the order the page lists them: AWS,
// GCP and Azure, which is the guide's order too.
var guideSources = []guideSource{
	{key: "aws", name: "AWS", collect: "Collect from AWS", build: "Build the AWS inventory"},
	{key: "gcp", name: "GCP", collect: "Collect from GCP", build: "Build the GCP inventory"},
	{key: "azure", name: "Azure", collect: "Collect from Azure", build: "Build the Azure inventory"},
}

// commandPlaceholder matches the template action site/inventory.html renders a
// source's commands through: ci/sitebuild fills the page in at build time, so
// the source page names the field rather than holding the commands.
var commandPlaceholder = regexp.MustCompile(`\{\{[^}]*Commands[^}]*\}\}`)

// TestInventoryPageIsInTheBundle pins that the page shipped, and that it is
// reachable from every page like the privacy and verification pages are: the nav
// and the footer of each page link it (issue #373, item 15). The bundle's own
// list of pages, pageNames, holds it too, so every per-page check of this package
// covers it.
func TestInventoryPageIsInTheBundle(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	if !slices.Contains(bundle.files, inventoryPageName) {
		t.Fatalf("the bundle holds no %s, want the inventory page in it (issue #373, item 15)", inventoryPageName)
	}

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, page)

			navs := doc.find("nav")
			if len(navs) == 0 {
				t.Fatalf("%s holds no <nav>, so the check cannot read its navigation", page)
			}

			for _, where := range []*element{navs[0], footerOf(t, doc)} {
				links := 0

				for _, anchor := range where.find("a") {
					if href, _ := anchor.attr("href"); href == inventoryPageName {
						links++
					}
				}

				if links != 1 {
					t.Errorf("%s's <%s> holds %d links to %s, want exactly one: the inventory page is in every page's nav and footer, as the privacy and verification pages are (issue #373, item 15)",
						page, where.tag, links, inventoryPageName)
				}
			}
		})
	}
}

// TestInventoryPageSourceButtons pins the page's own source control (issue #373,
// item 15): three buttons, AWS, GCP and Azure, each of type="button" with
// aria-pressed, exactly one of them pressed, each naming with aria-controls the
// section it shows, and no <select> anywhere on the page, since the maintainer
// asked for buttons and not a dropdown. The check reads the built page and the
// source page, so the pressed state is the page's own markup rather than a value
// a script writes.
func TestInventoryPageSourceButtons(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	tests := []struct {
		name string
		page *document
	}{
		{name: "the built page", page: bundle.html(t, inventoryPageName)},
		{name: "the source page", page: parseHTML(t, inventoryPageName, readSiteFile(t, inventoryPageName))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if selects := tc.page.find("select"); len(selects) != 0 {
				t.Errorf("%s holds %d <select> elements, want none: a source is chosen with three buttons, not a dropdown (issue #373, item 15)", tc.name, len(selects))
			}

			pressed, buttons := 0, 0

			for _, button := range tc.page.find("button") {
				pressedValue, ok := button.attr("aria-pressed")
				if !ok {
					continue
				}

				buttons++

				switch strings.TrimSpace(pressedValue) {
				case "true":
					pressed++
				case "false":
				default:
					t.Errorf("a source button carries aria-pressed=%q, want \"true\" or \"false\" (issue #373, item 15)", pressedValue)
				}

				if kind, _ := button.attr("type"); strings.TrimSpace(kind) != "button" {
					t.Errorf("a source button carries type=%q, want type=\"button\": the page's buttons submit no form (issue #373, item 15)", kind)
				}

				controls, _ := button.attr("aria-controls")

				if elementByIDOrNil(tc.page, controls) == nil {
					t.Errorf("the source button reading %q controls %q, which is no element of the page: each button names the section it shows (issue #373, item 15)",
						collapseSpace(button.textContent()), controls)
				}
			}

			if buttons != len(guideSources) {
				t.Errorf("%s holds %d buttons with aria-pressed, want %d: one per source (issue #373, item 15)", tc.name, buttons, len(guideSources))
			}

			if pressed != 1 {
				t.Errorf("%s holds %d buttons with aria-pressed=\"true\", want exactly one: the page shows one source at a time, and the one it starts on is pressed (issue #373, item 15)",
					tc.name, pressed)
			}

			for _, source := range guideSources {
				if !slices.ContainsFunc(tc.page.find("button"), func(button *element) bool {
					return collapseSpace(button.textContent()) == source.name
				}) {
					t.Errorf("%s holds no button reading %q, want one per source: AWS, GCP and Azure (issue #373, item 15)",
						tc.name, source.name)
				}
			}
		})
	}
}

// TestInventorySectionsAreShownWithoutScript pins that all three sections are in
// the HTML and none is hidden (issue #373, item 15): with no script the page
// still shows every source's commands, as the brief requires, so neither the
// markup nor the stylesheet hides a section a visitor has not chosen.
func TestInventorySectionsAreShownWithoutScript(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)
	doc := bundle.html(t, inventoryPageName)

	_, stylesheet := bundle.asset(t, "style", ".css")
	rules := parseCSS(t, "stylesheet", stylesheet)
	props := customProperties(rules)

	for _, source := range guideSources {
		t.Run(source.key, func(t *testing.T) {
			t.Parallel()

			section := inventorySection(t, doc, source)

			if section.hasAttr("hidden") {
				t.Errorf("%s's %s section carries the hidden attribute, want no section hidden: with no script all three show (issue #373, item 15)",
					inventoryPageName, source.name)
			}

			if reason := hiddenReason(t, section, rules, props); reason != "" {
				t.Errorf("%s's %s section is hidden because %s, want every section shown: with no script all three show (issue #373, item 15)",
					inventoryPageName, source.name, reason)
			}
		})
	}
}

// TestInventoryPickerIsHiddenWithoutScript pins the picker with no script (issue
// #373, item 15, review findings 10 and 14): the three source buttons do nothing
// until the page script wires them, so their container carries the hidden
// attribute and the script removes it. With no script a visitor sees the three
// sources' sections, which all show, and no button that announces AWS as pressed
// and then does nothing when it is pressed.
//
// The attribute alone is not enough: an author rule that sets display beats the
// browser's own [hidden] rule, so the stylesheet has to hide the picker itself
// while the attribute is set. The check reads the built bundle's stylesheet, so
// it fails when the rule is missing rather than trusting the attribute.
func TestInventoryPickerIsHiddenWithoutScript(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	_, stylesheet := bundle.asset(t, "style", ".css")
	rules := parseCSS(t, "stylesheet", stylesheet)
	props := customProperties(rules)

	tests := []struct {
		name string
		page *document
	}{
		{name: "the built page", page: bundle.html(t, inventoryPageName)},
		{name: "the source page", page: parseHTML(t, inventoryPageName, readSiteFile(t, inventoryPageName))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			picker := sourcePicker(t, tc.page)

			if !picker.hasAttr("hidden") {
				t.Errorf("%s's <%s class=%q> carries no hidden attribute, want it hidden: with no script the three source buttons do nothing, so the page shows none of them and every section's commands (issue #373, item 15)\n%v",
					tc.name, picker.tag, classAttr(picker), picker.attrs)
			}

			if display, rule := displayedBy(t, picker, rules, props); display != "none" {
				t.Errorf("%s's <%s class=%q hidden> still shows: %s, want the stylesheet to give it display: none while the hidden attribute is set: an author rule that sets display beats the browser's own [hidden] rule, so the three buttons that do nothing without script would still show (issue #373, item 15)",
					tc.name, picker.tag, classAttr(picker), rule)
			}
		})
	}
}

// TestInventoryCommandsPerStep pins how many commands each source's two steps
// show (issue #373, item 15, audit finding 3): the numbers are written down here,
// by hand, rather than read back through a parser of the guide, so a page that
// silently lost a command to a truncated extraction fails a check of its own and
// not only the parity check both sides share. The counts are what the guide holds
// once a comment heads only the command it describes (review finding 12): the
// page shows one <pre><code> per command, in the guide's order. AWS gained three
// commands with PRD #571's classification: `opensearch list-domain-names` with the
// `describe-domains` loop it feeds, `opensearchserverless list-collections` with
// the `batch-get-collection` loop it feeds, and `kafka list-clusters-v2` (the
// shape S3's and EKS's list-with-follow-up commands already take). The search and
// Kafka instance types join the existing "The vCPU and memory of the instance
// types in use" command, whose jq list reads their calls' output too.
func TestInventoryCommandsPerStep(t *testing.T) {
	t.Parallel()

	want := []struct {
		key     string
		collect int
		build   int
	}{
		{key: "aws", collect: 18, build: 1},
		{key: "gcp", collect: 11, build: 1},
		{key: "azure", collect: 8, build: 1},
	}

	doc := theBundle(t).html(t, inventoryPageName)

	for _, tc := range want {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()

			section := inventorySection(t, doc, guideSourceByKey(t, tc.key))
			steps := inventorySteps(t, section)

			if got := len(steps["collect"]); got != tc.collect {
				t.Errorf("%s's %q step shows %d commands, want %d: the guide's \"Collect from %s\" section holds that many, one per command, and a comment heads only the command it describes (issue #373, item 15)\n%s",
					inventoryPageName, "01 Collect the output", got, tc.collect, strings.ToUpper(tc.key), describeCommands(steps["collect"]))
			}

			if got := len(steps["build"]); got != tc.build {
				t.Errorf("%s's %q step shows %d commands, want %d: the guide's \"Build the %s inventory\" section holds that many (issue #373, item 15)\n%s",
					inventoryPageName, "02 Build the inventory", got, tc.build, strings.ToUpper(tc.key), describeCommands(steps["build"]))
			}
		})
	}
}

// TestInventoryCommentHeadsOnlyItsCommand pins that a comment heads the command
// it describes and no other (issue #373, item 15, review finding 12): the guide's
// block runs a comment into the calls that follow it when it has no blank line
// after the call it describes, and the page copies whatever the block holds. A
// visitor who copies the commented command gets that one call, not a page of
// unrelated ones.
func TestInventoryCommentHeadsOnlyItsCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		// comment is the first line of the command, the comment that describes it.
		comment string
		// describes is the call the comment describes.
		describes string
		// others are the calls that belong to another command.
		others []string
	}{
		{
			name:      "the AWS instance types' vCPU and memory",
			key:       "aws",
			comment:   "# The vCPU and memory of the instance types in use.",
			describes: "aws ec2 describe-instance-types",
			others:    []string{"aws ec2 describe-volumes", "aws ec2 describe-snapshots", "aws autoscaling describe-auto-scaling-groups"},
		},
		{
			name:      "the one GCP label the program reads",
			key:       "gcp",
			comment:   "# Keep only the one label gcp.jq reads, not the values of any other label.",
			describes: "gcloud asset search-all-resources",
			others:    []string{"gcloud compute instances list"},
		},
		{
			name:      "GCP's machine types, with no instances to ask for",
			key:       "gcp",
			comment:   "# With no instances there are no types to ask for: write an empty list instead.",
			describes: "gcloud compute machine-types list",
			others: []string{
				"gcloud compute disks list",
				"gcloud compute instance-groups managed list",
				"gcloud compute snapshots list",
				"gcloud sql instances list",
				"gcloud storage buckets list",
				"gcloud container clusters list",
				"gcloud pubsub subscriptions list",
			},
		},
	}

	doc := theBundle(t).html(t, inventoryPageName)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			section := inventorySection(t, doc, guideSourceByKey(t, tc.key))
			steps := inventorySteps(t, section)

			var headed []string

			for _, step := range []string{"collect", "build"} {
				for _, command := range steps[step] {
					if strings.HasPrefix(strings.TrimSpace(command), tc.comment) {
						headed = append(headed, command)
					}
				}
			}

			if len(headed) == 0 {
				t.Fatalf("%s's %s section shows no command starting with %q, so this check read nothing: the guide's comment heads a command of its own (issue #373, item 15)\n%s",
					inventoryPageName, strings.ToUpper(tc.key), tc.comment, describeCommands(steps["collect"]))
			}

			if len(headed) > 1 {
				t.Fatalf("%s's %s section shows %d commands starting with %q, want one: a comment heads one command (issue #373, item 15)",
					inventoryPageName, strings.ToUpper(tc.key), len(headed), tc.comment)
			}

			if !strings.Contains(headed[0], tc.describes) {
				t.Errorf("%s's command headed by %q does not hold %q, want the call the comment describes (issue #373, item 15)\n%s",
					inventoryPageName, tc.comment, tc.describes, headed[0])
			}

			for _, other := range tc.others {
				if strings.Contains(headed[0], other) {
					t.Errorf("%s's command headed by %q also holds %q, want the call the comment describes and no other: the guide needs a blank line between the two, so each is copied on its own (issue #373, item 15)\n%s",
						inventoryPageName, tc.comment, other, headed[0])
				}
			}
		})
	}
}

// sourcePicker returns the element that holds the page's three source buttons:
// the narrowest element the script shows and hides. It fails the test when the
// page holds no button carrying aria-controls, or when no element holds them all.
func sourcePicker(t *testing.T, doc *document) *element {
	t.Helper()

	var buttons []*element

	for _, button := range doc.find("button") {
		if _, ok := button.attr("aria-controls"); ok {
			buttons = append(buttons, button)
		}
	}

	if len(buttons) == 0 {
		t.Fatalf("%s holds no button carrying aria-controls, so this check read nothing: each source button names the section it shows (issue #373, item 15)",
			inventoryPageName)
	}

	for _, candidate := range buttons[0].ancestors() {
		holds := true

		for _, button := range buttons {
			if !isDescendant(button, candidate) {
				holds = false
			}
		}

		if holds {
			return candidate
		}
	}

	t.Fatalf("%s's source buttons sit in no element that holds all %d of them, want them grouped in one container the script shows (issue #373, item 15)",
		inventoryPageName, len(buttons))

	return nil
}

// classAttr returns the element's class attribute as it spells it, for a failure
// message.
func classAttr(el *element) string {
	class, _ := el.attr("class")

	return class
}

// displayedBy returns the display value the stylesheet gives el, and the rule
// that sets it, for a failure message: among the top-level rules that match the
// element and set display it takes the last one in source order, a declaration
// with !important beating the ones without. It compares no specificity and no
// inheritance: a matching ancestor's rule counts as the element's, and a later
// rule beats a more specific one only because it comes later. It reads no rule
// inside an at-rule, since such a rule holds only under a condition and so
// cannot be what hides the element. It returns "" and a sentence that says no
// rule sets display when the browser's own value is the one that applies.
func displayedBy(t *testing.T, el *element, rules []cssRule, props map[string]string) (string, string) {
	t.Helper()

	var (
		value     string
		rule      string
		important bool
	)

	for _, candidate := range rules {
		if len(candidate.atRules) != 0 {
			continue
		}

		if !slices.ContainsFunc(candidate.selectors, func(selector string) bool {
			return selectorMatches(selector, el)
		}) {
			continue
		}

		for _, decl := range candidate.decls {
			if decl.property != "display" {
				continue
			}

			resolved, err := resolveValue("stylesheet", decl.value, props)
			if err != nil {
				t.Fatalf("the checks read no display value for the <%s class=%q>: %v", el.tag, classAttr(el), err)
			}

			isImportant := strings.Contains(strings.ToLower(resolved), "important")
			if important && !isImportant {
				continue
			}

			value = firstWord(strings.ReplaceAll(strings.ToLower(resolved), "!important", ""))
			important = isImportant
			rule = fmt.Sprintf("the stylesheet's rule %q sets display: %s", strings.Join(candidate.selectors, ", "), decl.value)
		}
	}

	if rule == "" {
		return "", "no rule of the stylesheet sets its display, so the browser's own value applies"
	}

	return value, rule
}

// firstWord returns the first white-space separated word of value, lower case,
// or "" for a value of no words.
func firstWord(value string) string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return ""
	}

	return strings.ToLower(words[0])
}

// guideSourceByKey returns the source the page lists by key, failing the test for
// a key no source has.
func guideSourceByKey(t *testing.T, key string) guideSource {
	t.Helper()

	for _, source := range guideSources {
		if source.key == key {
			return source
		}
	}

	t.Fatalf("the page documents no source with the key %q, so this check read nothing: the sources are %v", key, guideSources)

	return guideSource{}
}

// inventorySteps returns the commands of a source's section, by the step the page
// shows above them: "collect" for the page's "01 Collect the output" and "build"
// for its "02 Build the inventory". A command shown before either step fails the
// test, so a page that lost its steps cannot pass on nothing.
func inventorySteps(t *testing.T, section *element) map[string][]string {
	t.Helper()

	steps := map[string][]string{}
	step := ""

	var walk func(el *element)

	walk = func(el *element) {
		if el.tag == "pre" {
			if step == "" {
				t.Fatalf("%s's %q section shows a command above its first step heading, so this check cannot tell its collect commands from its build commands (issue #373, item 15)\n%s",
					inventoryPageName, collapseSpace(section.textContent()), collapseSpace(el.textContent()))
			}

			steps[step] = append(steps[step], el.textContent())

			return
		}

		for _, child := range el.children {
			walk(child)
		}
	}

	for _, child := range section.children {
		if child.tag == "h3" {
			heading := collapseSpace(child.textContent())

			switch {
			case strings.Contains(heading, "Collect"):
				step = "collect"
			case strings.Contains(heading, "Build"):
				step = "build"
			default:
				t.Fatalf("%s's %q section shows the step heading %q, want one that says Collect or Build (issue #373, item 15)",
					inventoryPageName, collapseSpace(section.textContent()), heading)
			}

			continue
		}

		walk(child)
	}

	return steps
}

// describeCommands renders commands for a failure message: the command's first
// line and how many lines it holds.
func describeCommands(commands []string) string {
	var out strings.Builder

	for i, command := range commands {
		lines := strings.Split(command, "\n")

		fmt.Fprintf(&out, "  [%02d] (%d lines) %s\n", i+1, len(lines), strings.TrimSpace(lines[0]))
	}

	return out.String()
}

// TestInventoryCommandsAreTheGuides pins the page's one source of truth (issue
// #373, item 15): the commands of each section, in order, are the commands of
// docs/guide/inventory.md under that source's "Collect from" and "Build the"
// headings, character for character. A command is one run of lines of a bash
// block: a blank line ends it, and a line whose first character is "#" starts
// one, so a comment is copied together with the command it describes and never
// forms a command of its own.
func TestInventoryCommandsAreTheGuides(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)
	doc := bundle.html(t, inventoryPageName)
	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	for _, source := range guideSources {
		t.Run(source.key, func(t *testing.T) {
			t.Parallel()

			guideText := guideCommands(t, guide, source)
			if len(guideText) == 0 {
				t.Fatalf("docs/guide/inventory.md holds no bash block under %q and %q, so the check read nothing",
					source.collect, source.build)
			}

			want := make([]string, 0, len(guideText))
			for _, command := range guideText {
				want = append(want, trimCommand(command))
			}

			section := inventorySection(t, doc, source)

			got := make([]string, 0, len(want))

			for _, pre := range section.find("pre") {
				codes := pre.find("code")
				if len(codes) != 1 {
					t.Errorf("%s's %s section holds a <pre> with %d <code> elements, want exactly one per command (issue #373, item 15)",
						inventoryPageName, source.name, len(codes))

					continue
				}

				got = append(got, trimCommand(codes[0].textContent()))
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("%s's %s section shows commands that differ from docs/guide/inventory.md's (-guide +page):\n%s",
					inventoryPageName, source.name, diff)
			}
		})
	}
}

// TestInventoryPageHasNoHandTypedCommand pins the build's contract (issue #373,
// item 15): the source page site/inventory.html holds a placeholder that
// ci/sitebuild fills with the guide's commands, and no command text of its own.
// The placeholder is a Go template action whose body names the commands field, so
// the page cannot carry a copy of the guide that would drift from it.
func TestInventoryPageHasNoHandTypedCommand(t *testing.T) {
	t.Parallel()

	source := readSiteFile(t, inventoryPageName)

	if !commandPlaceholder.Match(source) {
		t.Errorf("site/%s holds no Go template action naming a Commands field (such as {{range .Commands}}), want the placeholder ci/sitebuild fills with the guide's commands: the page carries no hand-typed copy of them (issue #373, item 15)",
			inventoryPageName)
	}

	doc := parseHTML(t, inventoryPageName, source)

	for _, tag := range []string{"pre", "code"} {
		for _, el := range doc.find(tag) {
			text := collapseSpace(el.textContent())

			for _, verb := range []string{"aws ", "gcloud ", "az "} {
				if strings.HasPrefix(text, verb) {
					t.Errorf("site/%s holds a hand-typed command in a <%s>: %q, want a placeholder the build fills from docs/guide/inventory.md (issue #373, item 15)",
						inventoryPageName, tag, text)
				}
			}
		}
	}

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	for _, command := range guideCommands(t, guide, guideSources[0]) {
		for _, line := range strings.Split(command, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.Contains(string(source), line) {
				t.Errorf("site/%s holds the guide's command line %q, want a placeholder instead: the commands come from docs/guide/inventory.md at build time (issue #373, item 15)",
					inventoryPageName, line)
			}
		}
	}
}

// TestInventoryCommandsHaveACopyButton pins the copy control (issue #373, item
// 15): every command sits in its own <pre>, and the element that holds the
// command also holds a <button type="button"> beside it whose accessible name
// says it copies, so each command is copied on its own rather than one button
// copying them all.
func TestInventoryCommandsHaveACopyButton(t *testing.T) {
	t.Parallel()

	doc := theBundle(t).html(t, inventoryPageName)

	for _, source := range guideSources {
		section := inventorySection(t, doc, source)

		copies := 0

		for _, button := range section.find("button") {
			if isCopyButton(button) {
				copies++
			}
		}

		pres := section.find("pre")

		if len(pres) == 0 {
			t.Errorf("%s's %s section holds no command, want the guide's commands in it (issue #373, item 15)", inventoryPageName, source.name)
		}

		if copies != len(pres) {
			t.Errorf("%s's %s section holds %d copy buttons for %d commands, want one copy button per command (issue #373, item 15)",
				inventoryPageName, source.name, copies, len(pres))
		}

		for _, pre := range pres {
			if pre.parent == nil {
				continue
			}

			beside := false

			for _, sibling := range pre.parent.children {
				if isCopyButton(sibling) {
					beside = true
				}
			}

			if !beside {
				t.Errorf("%s's %s section holds a command in a <pre> whose element holds no copy button beside it, want one per command: each command is copied on its own (issue #373, item 15)",
					inventoryPageName, source.name)
			}
		}
	}
}

// TestInventoryCommandsAreEscaped pins that a command reaches the page as text
// (issue #373, item 15): the markup between <code> and </code> holds no raw "<"
// and no raw "&", so a command holding either is escaped rather than read as
// markup. The guide's commands hold neither character today, and the check still
// reads every command the build writes, so a command that gains one is covered.
func TestInventoryCommandsAreEscaped(t *testing.T) {
	t.Parallel()

	page := string(theBundle(t).read(t, inventoryPageName))

	// A command's block: a <pre> and the one <code> it holds, whatever classes
	// or other attributes the page gives them.
	block := regexp.MustCompile(`(?s)<pre[^>]*>\s*<code[^>]*>(.*?)</code>`)

	matches := block.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		t.Fatalf("%s writes no <pre><code> command, so the check read nothing: each command is its own <pre><code> block (issue #373, item 15)", inventoryPageName)
	}

	for _, match := range matches {
		for _, problem := range unescapedMarkup(match[1]) {
			t.Errorf("%s's command holds the raw %q, want it escaped: a command is text, so a < or an & in one never becomes markup (issue #373, item 15)\n%s",
				inventoryPageName, problem, match[1])
		}
	}
}

// TestInventoryPageLinksTheGuideAndItsPrograms pins what the page offers for the
// full text (issue #373, item 15): links to the guide and to the three programs
// on GitHub, each an absolute https URL at the ref the build shows, each opening
// in a new tab with rel="noopener noreferrer", and none of them loaded into the
// page.
func TestInventoryPageLinksTheGuideAndItsPrograms(t *testing.T) {
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

			ref := versionRef(tc.bundle)

			want := []string{guideURL(ref, guidePath)}
			for _, source := range guideSources {
				want = append(want, guideURL(ref, guideProgramsDirectory+source.key+".jq"))
			}

			doc := tc.bundle.html(t, inventoryPageName)

			for _, url := range want {
				anchors := 0

				for _, anchor := range doc.find("a") {
					if href, _ := anchor.attr("href"); href != url {
						continue
					}

					anchors++

					if target, _ := anchor.attr("target"); target != "_blank" {
						t.Errorf("%s's link to %s carries target=%q, want \"_blank\": the full text opens in a new tab (issue #373, item 15)",
							inventoryPageName, url, target)
					}

					if rel, _ := anchor.attr("rel"); !slices.Equal(strings.Fields(rel), []string{"noopener", "noreferrer"}) {
						t.Errorf("%s's link to %s carries rel=%q, want \"noopener noreferrer\": the page it opens gets no handle on this one (issue #373, item 15)",
							inventoryPageName, url, rel)
					}
				}

				if anchors == 0 {
					t.Errorf("%s links no %s, want the guide and the three programs of %s (issue #373, item 15)",
						inventoryPageName, url, guideProgramsDirectory)
				}
			}

			for _, resource := range loadedResources(doc) {
				for _, url := range want {
					if resource.ref == url {
						t.Errorf("%s loads %s=%q, want a link only: the page fetches neither the guide nor a program (issue #373, item 15)",
							inventoryPageName, resource.attribute, resource.ref)
					}
				}
			}
		})
	}
}

// inventorySection returns the section the source's button controls, failing the
// test when the page holds none: the check reads the section the page itself says
// the button shows.
func inventorySection(t *testing.T, doc *document, source guideSource) *element {
	t.Helper()

	for _, button := range doc.find("button") {
		if text := collapseSpace(button.textContent()); text != source.name {
			continue
		}

		controls, ok := button.attr("aria-controls")
		if !ok || strings.TrimSpace(controls) == "" {
			t.Fatalf("the %s button carries no aria-controls, so the check cannot tell which section it shows (issue #373, item 15)", source.name)
		}

		if section := elementByIDOrNil(doc, controls); section != nil {
			return section
		}

		t.Fatalf("the %s button controls %q, which is no element of the page (issue #373, item 15)", source.name, controls)
	}

	t.Fatalf("%s holds no button reading %q, so the check cannot tell which section holds its commands (issue #373, item 15)",
		inventoryPageName, source.name)

	return nil
}

// elementByIDOrNil returns the element of doc whose id is id, or nil.
func elementByIDOrNil(doc *document, id string) *element {
	if strings.TrimSpace(id) == "" {
		return nil
	}

	for _, el := range doc.all() {
		if got, ok := el.attr("id"); ok && got == id {
			return el
		}
	}

	return nil
}

// isCopyButton reports whether the element is a button whose accessible name says
// it copies a command.
func isCopyButton(el *element) bool {
	if el.tag != "button" {
		return false
	}

	if kind, ok := el.attr("type"); ok && !strings.EqualFold(strings.TrimSpace(kind), "button") {
		return false
	}

	return strings.Contains(strings.ToLower(accessibleName(el)), "copy")
}

// accessibleName returns the name a screen reader gives a control: its
// aria-label, or else the text it shows.
func accessibleName(el *element) string {
	if label, ok := el.attr("aria-label"); ok && strings.TrimSpace(label) != "" {
		return collapseSpace(label)
	}

	return collapseSpace(el.textContent())
}

// readSiteFile reads a file of the site's sources under site/.
func readSiteFile(t *testing.T, name string) []byte {
	t.Helper()

	return readRepositoryFile(t, filepath.Join("site", filepath.FromSlash(name)))
}

// readRepositoryFile reads a file of this checkout, relative to the repository
// root.
func readRepositoryFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(theRepositoryRoot(t), path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

// headingText returns the words of a Markdown heading line, without its #s, or
// "" for a line that is no heading.
func headingText(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return ""
	}

	return strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
}

// guideCommands returns the commands of the bash blocks of the guide under the
// source's two headings, in the guide's order.
func guideCommands(t *testing.T, guide string, source guideSource) []string {
	t.Helper()

	var commands []string

	for _, heading := range []string{source.collect, source.build} {
		for _, block := range fencedBashBlocks(t, guide, heading) {
			commands = append(commands, splitCommands(block)...)
		}
	}

	return commands
}

// fencedBashBlocks returns the lines of each ```bash block of the guide under the
// heading, up to the next heading: one section of the guide holds the commands of
// one source.
func fencedBashBlocks(t *testing.T, guide, heading string) [][]string {
	t.Helper()

	lines := strings.Split(guide, "\n")

	start := -1

	for i, line := range lines {
		if headingText(line) == heading {
			start = i + 1

			break
		}
	}

	if start < 0 {
		return nil
	}

	var (
		blocks  [][]string
		block   []string
		inBlock bool
	)

	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)

		if !inBlock && strings.HasPrefix(trimmed, "#") {
			break
		}

		switch {
		case !inBlock && trimmed == "```bash":
			inBlock = true
			block = nil
		case inBlock && trimmed == "```":
			inBlock = false

			blocks = append(blocks, block)
		case inBlock:
			block = append(block, line)
		}
	}

	if inBlock {
		t.Fatalf("docs/guide/inventory.md's %q section leaves a bash block open", heading)
	}

	return blocks
}

// splitCommands splits one bash block into the commands a visitor copies: a blank
// line ends a command, and a line whose first character is "#" starts one, so a
// comment is part of the command it describes and never a command of its own.
// The leading and trailing blank lines of a command are dropped.
func splitCommands(block []string) []string {
	var (
		commands []string
		current  []string
	)

	hasCode := func() bool {
		for _, line := range current {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				return true
			}
		}

		return false
	}

	flush := func() {
		for len(current) > 0 && strings.TrimSpace(current[0]) == "" {
			current = current[1:]
		}

		for len(current) > 0 && strings.TrimSpace(current[len(current)-1]) == "" {
			current = current[:len(current)-1]
		}

		if len(current) > 0 {
			commands = append(commands, strings.Join(current, "\n"))
		}

		current = nil
	}

	for _, line := range block {
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			// A blank line ends a command that holds code; a run of comments
			// waits for the command it describes.
			if hasCode() {
				flush()
			}
		case strings.HasPrefix(trimmed, "#"):
			if hasCode() {
				flush()
			}

			current = append(current, line)
		default:
			current = append(current, line)
		}
	}

	flush()

	return commands
}

// trimCommand returns a command as the two sides compare it: each line's trailing
// white space removed, the leading and trailing blank lines dropped, and every
// other character kept, since a command is copied as the guide writes it.
func trimCommand(command string) string {
	lines := strings.Split(command, "\n")

	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n")
}

// unescapedMarkup returns the markup characters of text that a command written as
// HTML cannot hold: a "<" anywhere, and an "&" that is no part of a character
// reference.
func unescapedMarkup(text string) []string {
	var problems []string

	entity := regexp.MustCompile(`^&(amp|lt|gt|quot|apos|#39|#34|#43|#[0-9]+|#[xX][0-9a-fA-F]+);`)

	for i, r := range text {
		switch r {
		case '<':
			problems = append(problems, "<")
		case '&':
			if !entity.MatchString(text[i:]) {
				problems = append(problems, "&")
			}
		}
	}

	return problems
}

// awsSearchAndMessagingCalls are the calls the guide's AWS section makes to
// classify the search and messaging resources of PRD #571, and the only keys
// each call's --query may name: the list under *Only the fields the program
// reads*. A key outside its call's list is a field neither the program nor the
// report reads, which the raw output would then keep in the scratch directory
// the guide tells the reader to delete.
var awsSearchAndMessagingCalls = map[string][]string{
	"aws opensearch list-domain-names": {
		"DomainNames",
		"DomainName",
	},
	"aws opensearch describe-domains": {
		"DomainStatusList",
		"DomainName",
		"EngineVersion",
		"ClusterConfig",
		"InstanceType",
		"InstanceCount",
		"ZoneAwarenessEnabled",
		"MultiAZWithStandbyEnabled",
		"EBSOptions",
		"EBSEnabled",
		"VolumeSize",
		"EncryptionAtRestOptions",
		"Enabled",
	},
	"aws opensearchserverless list-collections": {
		"collectionSummaries",
		"id",
		"name",
	},
	"aws opensearchserverless batch-get-collection": {
		"collectionDetails",
		"name",
		"type",
		"standbyReplicas",
	},
	"aws kafka list-clusters-v2": {
		"ClusterInfoList",
		"ClusterName",
		"ClusterType",
		"Provisioned",
		"NumberOfBrokerNodes",
		"BrokerNodeGroupInfo",
		"InstanceType",
		"ClientSubnets",
		"StorageInfo",
		"EbsStorageInfo",
		"VolumeSize",
		"EncryptionInfo",
		"EncryptionAtRest",
		"DataVolumeKMSKeyId",
		"Serverless",
		"VpcConfigs",
		"SubnetIds",
	},
}

// awsExcludedKeys are the fields no projection of the search and messaging
// calls may name (PRD #571, *Only the fields the program reads*): an access
// policy carries the account's principals, a security option its settings, an
// endpoint or a bootstrap address a host with the account's name in it.
var awsExcludedKeys = []string{
	"AccessPolicies",
	"AdvancedSecurityOptions",
	"CognitoOptions",
	"Endpoint",
	"Endpoints",
	"DomainEndpointOptions",
	"collectionEndpoint",
	"dashboardEndpoint",
	"bootstrapAddress",
	"osProfile",
	"connectivityEndpoints",
}

// awsExcludedKeyPrefixes are the fields no projection may name whatever its
// suffix: a cluster's ZooKeeper connection strings and its broker addresses.
var awsExcludedKeyPrefixes = []string{"ZookeeperConnectString", "BootstrapBroker"}

// awsSearchAndMessagingCall finds one search or messaging call of an AWS
// command: `aws <service> <operation>` in the guide's spelling.
var awsSearchAndMessagingCall = regexp.MustCompile(`aws (?:opensearchserverless|opensearch|kafka) [a-z0-9-]+`)

// quotedString finds a double-quoted string, the only kind a jq projection can
// hold: the projection itself is the shell's single-quoted argument.
var quotedString = regexp.MustCompile(`"[^"]*"`)

// identifier finds an identifier of a jq projection.
var identifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// jqWords are jq's own words, which a projection may use beside the fields it
// reads: functions and literals are no field of the raw output.
var jqWords = map[string]bool{
	"add": true, "and": true, "ascii_downcase": true, "ascii_upcase": true,
	"del": true, "elif": true, "else": true, "empty": true, "end": true,
	"endswith": true, "false": true, "first": true, "floor": true,
	"from_entries": true, "getpath": true, "group_by": true, "gsub": true,
	"has": true, "if": true, "index": true, "join": true, "keys": true,
	"last": true, "length": true, "map": true, "map_values": true,
	"max": true, "min": true, "not": true, "null": true, "or": true,
	"paths": true, "select": true, "sort": true, "sort_by": true,
	"split": true, "startswith": true, "sub": true, "test": true,
	"then": true, "to_entries": true, "tonumber": true, "tostring": true,
	"true": true, "type": true, "unique": true, "values": true,
}

// TestInventoryGuideSearchAndKafkaCallsKeepOnlyTheFieldsTheyRead pins the other
// half of PRD #571's *Only the fields the program reads*: the recorded fixtures
// hold only that call's fields (`test/examples`, which a fixture cannot pass
// while the guide's command kept the whole response), and this check reads the
// guide's own command text, so a command that dropped its projection cannot
// hide behind a fixture that never held the keys. Every `aws opensearch`,
// `aws opensearchserverless` and `aws kafka` command has a --query, that
// --query is a single-quoted projection, and the projection names only the
// fields the PRD lists.
func TestInventoryGuideSearchAndKafkaCallsKeepOnlyTheFieldsTheyRead(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	seen := map[string]bool{}

	for _, command := range guideCommands(t, guide, guideSources[0]) {
		for _, call := range awsSearchAndMessagingCallsIn(t, command) {
			seen[call.name] = true

			allowed := awsSearchAndMessagingCalls[call.name]
			if allowed == nil {
				t.Errorf("docs/guide/inventory.md runs %q, a call PRD #571 does not list: the search and Kafka classification is `aws opensearch list-domain-names` and `describe-domains`, `aws opensearchserverless list-collections` and `batch-get-collection`, and `aws kafka list-clusters-v2`\n%s",
					call.name, command)

				continue
			}

			for _, key := range projectionKeys(call.query) {
				if !slices.Contains(allowed, key) {
					t.Errorf("%q's --query names %q, want only the fields PRD #571 reads of that call (%s): a key the program does not read would keep a field the guide's raw output should drop\n%s",
						call.name, key, strings.Join(allowed, ", "), command)
				}
			}

			for _, excluded := range awsExcludedKeys {
				if strings.Contains(call.query, excluded) {
					t.Errorf("%q's --query names %q, want no such field: PRD #571's *Only the fields the program reads* leaves it out of every projection\n%s",
						call.name, excluded, command)
				}
			}

			for _, prefix := range awsExcludedKeyPrefixes {
				if strings.Contains(call.query, prefix) {
					t.Errorf("%q's --query names a %s* field, want none: PRD #571's *Only the fields the program reads* leaves ZooKeeper connection strings and broker addresses out of every projection\n%s",
						call.name, prefix, command)
				}
			}
		}
	}

	for name := range awsSearchAndMessagingCalls {
		if !seen[name] {
			t.Errorf("docs/guide/inventory.md runs no %q, want one: PRD #571's AWS classification reads the search domains, the collections and the Kafka clusters from those five calls", name)
		}
	}
}

// awsCall is one search or messaging call of an AWS command, with the --query
// projection it passes; the projection is empty when the command passes none.
//
// A command is one run of lines, and a call in a pipeline (a list feeding an
// xargs describe) is a call of its own, so both are read here.
type awsCall struct {
	name  string
	query string
}

// awsSearchAndMessagingCallsIn returns the search and messaging calls of one
// AWS command, failing the test when a call passes no --query or passes one
// that is no single-quoted projection: the page shows the command whole, so a
// call that kept the whole response would put the response in the scratch
// directory the guide deletes.
func awsSearchAndMessagingCallsIn(t *testing.T, command string) []awsCall {
	t.Helper()

	var calls []awsCall

	for _, location := range awsSearchAndMessagingCall.FindAllStringIndex(command, -1) {
		name := strings.Join(strings.Fields(command[location[0]:location[1]]), " ")

		rest := command[location[1]:]

		at := strings.Index(rest, "--query")
		if at < 0 {
			t.Errorf("%q passes no --query, want an explicit projection that keeps only the fields PRD #571 reads\n%s",
				name, command)

			calls = append(calls, awsCall{name: name})

			continue
		}

		projection := strings.TrimLeft(rest[at+len("--query"):], " ")

		if projection == "" || projection[0] != '\'' {
			t.Errorf("%q's --query is %q, want a single-quoted jq projection such as --query '{...}'\n%s",
				name, firstLine(projection), command)

			calls = append(calls, awsCall{name: name})

			continue
		}

		end := strings.Index(projection[1:], "'")
		if end < 0 {
			t.Errorf("%q's --query leaves its single quote open, so the check cannot read the projection\n%s", name, command)

			calls = append(calls, awsCall{name: name})

			continue
		}

		calls = append(calls, awsCall{name: name, query: projection[1 : 1+end]})
	}

	return calls
}

// projectionKeys returns the field names a jq projection names: its
// identifiers, without its double-quoted strings and without jq's own words.
func projectionKeys(projection string) []string {
	var keys []string

	for _, key := range identifier.FindAllString(quotedString.ReplaceAllString(projection, ""), -1) {
		if !jqWords[key] {
			keys = append(keys, key)
		}
	}

	return keys
}

// firstLine returns the first line of text, for a failure message.
func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}

	return text
}

// TestInventoryGuideAWSPolicyCoversTheSearchAndKafkaCalls pins the policy the
// guide tells a reader to create (PRD #571, milestone 2): the search and Kafka
// calls need their list and describe actions, and the policy names no
// data-plane or configuration action, which would let its holder read the
// cluster's data or its credentials instead of its configuration.
func TestInventoryGuideAWSPolicyCoversTheSearchAndKafkaCalls(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	actions := guideAWSPolicyActions(t, guide)

	for _, action := range []string{
		"es:ListDomainNames",
		"es:DescribeDomains",
		"aoss:ListCollections",
		"aoss:BatchGetCollection",
		"kafka:ListClustersV2",
	} {
		if !slices.Contains(actions, action) {
			t.Errorf("the guide's AWS policy does not allow %q, want it: PRD #571's classification makes the call that action permits, and the policy is the only permission the guide asks for",
				action)
		}
	}

	for _, action := range actions {
		if strings.HasPrefix(action, "es:ESHttp") {
			t.Errorf("the guide's AWS policy allows %q, want no data-plane action: PRD #571 makes no data-plane call, and the action would let its holder read the domain's documents",
				action)
		}

		if strings.HasPrefix(action, "kafka-cluster:") {
			t.Errorf("the guide's AWS policy allows %q, want no data-plane action: PRD #571 makes no data-plane call, and the action would let its holder read and write the cluster's topics",
				action)
		}

		if action == "aoss:APIAccessAll" {
			t.Errorf("the guide's AWS policy allows %q, want no data-plane action: PRD #571 makes no data-plane call", action)
		}

		if action == "kafka:GetBootstrapBrokers" {
			t.Errorf("the guide's AWS policy allows %q, want no connection-string action: the program reads no broker address, and PRD #571 keeps one out of every projection",
				action)
		}

		if action == "kafka:ListScramSecrets" {
			t.Errorf("the guide's AWS policy allows %q, want no credentials action: the guide reads no secret, and the program needs none",
				action)
		}
	}
}

// guideAWSPolicyActions returns the actions of the AWS permissions policy the
// guide tells a reader to create.
func guideAWSPolicyActions(t *testing.T, guide string) []string {
	t.Helper()

	block := fencedBlockAfter(t, guide, "AWS permissions", "json")

	var policy struct {
		Statement []struct {
			Action []string `json:"Action"`
		} `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(block), &policy); err != nil {
		t.Fatalf("the guide's AWS permissions policy is not JSON: %v\n%s", err, block)
	}

	var actions []string

	for _, statement := range policy.Statement {
		actions = append(actions, statement.Action...)
	}

	if len(actions) == 0 {
		t.Fatalf("the guide's AWS permissions policy lists no action, so this check read nothing\n%s", block)
	}

	return actions
}

// fencedBlockAfter returns the body of the first fenced block written in the
// language, under the heading, of a Markdown document.
func fencedBlockAfter(t *testing.T, document, heading, language string) string {
	t.Helper()

	lines := strings.Split(document, "\n")

	start := -1

	for i, line := range lines {
		if headingText(line) == heading {
			start = i + 1

			break
		}
	}

	if start < 0 {
		t.Fatalf("docs/guide/inventory.md holds no heading %q, so this check read nothing", heading)
	}

	fence := "```" + language

	var block []string

	inBlock := false

	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)

		switch {
		case !inBlock && trimmed == fence:
			inBlock = true
		case inBlock && trimmed == "```":
			return strings.Join(block, "\n")
		case inBlock:
			block = append(block, line)
		}
	}

	t.Fatalf("docs/guide/inventory.md's %q section holds no closed %s block, so this check read nothing", heading, fence)

	return ""
}
