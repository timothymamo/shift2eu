package site_test

import (
	"encoding/json"
	"errors"
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
// types in use" command, whose jq list reads their calls' output too. AWS gained
// three more with PRD #581's Route 53 classification: `list-hosted-zones`, the
// `get-dnssec` loop it feeds, and the record-set loop that counts each public
// zone's routed record sets, which is a command of its own so that a reader whose
// policy leaves `route53:ListResourceRecordSets` out can skip it.
func TestInventoryCommandsPerStep(t *testing.T) {
	t.Parallel()

	want := []struct {
		key     string
		collect int
		build   int
	}{
		{key: "aws", collect: 21, build: 1},
		// GCP's collect step gains PRD #571's Managed Service for Apache Kafka
		// list, one command of its own as its other service lists are, and PRD
		// #581's two Cloud DNS commands: `dns managed-zones list`, and the
		// record-set loop that counts each public zone's routed record sets,
		// a command of its own so that a reader whose policy leaves
		// `dns.resourceRecordSets.list` out can skip it.
		{key: "gcp", collect: 14, build: 1},
		// Azure's collect step gains PRD #581's DNSSEC loop as its ninth
		// command: `az network dns dnssec-config show`, run once per public DNS
		// zone the Resource Graph pages hold, its own command so that a reader
		// whose policy leaves it out can skip it.
		{key: "azure", collect: 9, build: 1},
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
//
// A command copied into the page by hand shows up line for line, so a whole line
// is what the last check compares: a substring match would flag any page that is
// laid out conventionally, since a block's `fi` sits inside "first", its `}`
// inside a template action's `}}`, and its `do` inside "download".
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

	for _, line := range pastedCommandLines(guideCommands(t, guide, guideSources[0]), pageLines(doc, source)) {
		t.Errorf("site/%s holds the guide's command line %q on a line of its own, want a placeholder instead: the commands come from docs/guide/inventory.md at build time (issue #373, item 15)",
			inventoryPageName, line)
	}

	t.Run("flags a pasted command line and nothing else", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			commands []string
			page     string
			want     []string
		}{{
			name:     "a one-line command pasted as it is",
			commands: []string{"aws opensearchserverless list-collections --output json --max-results 100"},
			page:     "<pre><code>aws opensearchserverless list-collections --output json --max-results 100</code></pre>\n",
			want:     []string{"aws opensearchserverless list-collections --output json --max-results 100"},
		}, {
			name:     "one line of a multi-line command pasted as it is",
			commands: []string{"f=\"$out/opensearchserverless-collections.json\"\n: > \"$f\"\ntoken=\nwhile :; do\ndone"},
			page:     "<pre><code>: &gt; &quot;$f&quot;\n</code></pre>\n",
			want:     []string{`: > "$f"`},
		}, {
			name:     "a conventionally laid-out block whose short lines stand inside other words",
			commands: []string{"if ! page=$(aws opensearch list-domain-names); then\n  fi\n}\ndo\ndone\n;;\nesac"},
			page:     "<p>The first page</p>\n<meta content=\"{{.Ref}}\">\n<p>elsewhere</p>\n",
		}, {
			name:     "a line the page holds only as part of a longer line",
			commands: []string{"aws ec2 describe-volumes"},
			page:     "<pre><code>aws ec2 describe-volumes --output json</code></pre>\n",
		}, {
			name:     "a line of three words the page holds only inside another line",
			commands: []string{"printf '%s\\n' \"$page\""},
			page:     "<pre><code>printf '%s\\n' &quot;$page&quot; &gt;&gt; &quot;$out/opensearchserverless-collections.json&quot;</code></pre>\n",
		}}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				source := []byte(tc.page)

				got := pastedCommandLines(tc.commands, pageLines(parseHTML(t, tc.name, source), source))
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("pastedCommandLines() differs from the lines the page holds (-want +got):\n%s", diff)
				}
			})
		}
	})
}

// pageLines returns the lines of the page a hand-typed command could land on:
// the trimmed lines of its source, and the trimmed lines of the text of each
// <pre> and <code> element, which is where a command is written. A line of fewer
// than two words is kept here, since whole-line comparison makes a short line
// harmless, and the shell reserved words are skipped by pastedCommandLines.
func pageLines(doc *document, source []byte) map[string]bool {
	lines := map[string]bool{}

	add := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				lines[trimmed] = true
			}
		}
	}

	add(string(source))

	for _, tag := range []string{"pre", "code"} {
		for _, el := range doc.find(tag) {
			add(el.textContent())
		}
	}

	return lines
}

// shellReservedWords are the lines of a shell block that run no command of their
// own. They are the shortest lines a block has, so a substring match on one flags
// any page at all: not compared whole, they are skipped, and a command a visitor
// could paste is never one of them.
var shellReservedWords = map[string]bool{
	"do": true, "done": true, "then": true, "else": true, "elif": true,
	"fi": true, "{": true, "}": true, ";;": true, "esac": true,
}

// pastedCommandLines returns the lines of commands that the page holds as lines
// of its own, in the guide's order: a command copied into the page by hand shows
// up line for line, so a whole line is what this compares and a line the page
// holds only as part of a longer one is no copy. A line of fewer than two words,
// and a shell reserved word, names no command a visitor would paste, and skipping
// them keeps a conventionally laid-out block from being flagged.
func pastedCommandLines(commands []string, lines map[string]bool) []string {
	var pasted []string

	for _, command := range commands {
		for _, line := range strings.Split(command, "\n") {
			trimmed := strings.TrimSpace(line)

			switch {
			case trimmed == "", strings.HasPrefix(trimmed, "#"):
				continue
			case len(strings.Fields(trimmed)) < 2, shellReservedWords[trimmed]:
				continue
			case lines[trimmed]:
				pasted = append(pasted, trimmed)
			}
		}
	}

	return pasted
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
		"ARN",
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
		// Deleted: a domain AWS is deleting is no resource to move, as a
		// terminated instance is none.
		"Deleted",
	},
	"aws opensearchserverless list-collections": {
		"collectionSummaries",
		"id",
		"name",
		// nextToken: the call has no CLI paginator, so the guide's loop keeps the
		// token of each page to ask for the next one.
		"nextToken",
	},
	"aws opensearchserverless batch-get-collection": {
		"collectionDetails",
		"name",
		"arn",
		"type",
		"standbyReplicas",
		// status: a collection AWS is deleting, or whose creation failed, is no
		// resource to move.
		"status",
	},
	"aws kafka list-clusters-v2": {
		"ClusterInfoList",
		"ClusterName",
		"ClusterType",
		"ClusterArn",
		// State: a cluster AWS is deleting, or whose creation failed, is no
		// resource to move.
		"State",
		"Provisioned",
		"NumberOfBrokerNodes",
		"BrokerNodeGroupInfo",
		"InstanceType",
		"ClientSubnets",
		"StorageInfo",
		"EbsStorageInfo",
		"VolumeSize",
	},
}

// excludedKeysPath is the one list of the fields no projection or fixture of
// the guide's search and messaging calls may name (PRD #571, *Only the fields the
// program reads*). test/examples' fixture check reads the same file, so the two
// halves of the promise, the guide's --query and the recorded output, cannot
// drift apart (review finding on PR #639). It is the one list of every source, not
// an AWS one, so neither it nor its reader is named after a source (review nit on
// PR #685).
const excludedKeysPath = "test/examples/testdata/inventory_guide_excluded_keys.txt"

// excludedKeysRecordTablePath is the table of record documents both readers of
// that list are given, with the records or the rejection each expects of them
// (review finding on PR #654). The readers are this package's
// readExcludedFields and test/examples' readExcludedFixtureKeys, in
// test/examples/inventoryguidekeys_test.go: neither test package may import the
// other, so each holds its own copy of the parser, and this table is what holds
// the two to the same documents. Both run it, each against its own copy, through
// TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords.
const excludedKeysRecordTablePath = "test/examples/testdata/inventory_guide_excluded_key_records.txt"

// excludedFields are the fields PRD #571 excludes from every projection of
// the search and messaging calls: an access policy carries the account's
// principals, a security option its settings, an endpoint or a bootstrap address
// a host with the account's name in it.
type excludedFields struct {
	// names are the fields a projection may not name.
	names []string
	// prefixes are the fields a projection may not name whatever their suffix: a
	// cluster's ZooKeeper connection strings and its broker addresses.
	prefixes []string
}

// The two kinds a record of the shared excluded-key list may have.
const (
	// recordKindKey is the kind of a record naming one field a projection of
	// the guide's search and messaging calls may not name.
	recordKindKey = "key"
	// recordKindPrefix is the kind of a record naming a prefix no field may
	// start with.
	recordKindPrefix = "prefix"
)

// The reasons the shared excluded-key list, and the table of its record
// documents, reject a document: one error each, so a case of the table can name
// the reason it expects and both copies of the parser can be held to it (review
// finding on PR #654).
var (
	// errRecordMalformed rejects a record that is neither `key: <field>` nor
	// `prefix: <prefix>`.
	errRecordMalformed = errors.New("a record is neither `key: <field>` nor `prefix: <prefix>`")
	// errRecordKind rejects a record of a kind the list does not know.
	errRecordKind = errors.New("a record's kind is neither `key` nor `prefix`")
	// errRecordComment rejects a record line that carries a comment, which
	// would become part of the field's name.
	errRecordComment = errors.New("a record line carries a comment")
	// errRecordRepeat rejects a record an earlier line already wrote, which
	// would hide that first copy.
	errRecordRepeat = errors.New("a record repeats an earlier one")
	// errRecordEmptyHalf rejects a list that holds no key or no prefix.
	errRecordEmptyHalf = errors.New("the list holds no key or no prefix")
)

// excludedKeyRecord is one record of the shared excluded-key list: a field a
// projection of the guide's search and messaging calls may not name, or a prefix
// no field may start with (PRD #571).
type excludedKeyRecord struct {
	// kind is recordKindKey or recordKindPrefix.
	kind string
	// name is the field, or the prefix a field may not start with.
	name string
}

// spelling renders the record as the shared list writes it.
func (r excludedKeyRecord) spelling() string {
	return r.kind + ": " + r.name
}

// parseExcludedFields reads the records of the shared excluded-key list (PRD
// #571). A record is `key: <field>` or `prefix: <prefix>`; a comment and a blank
// line are no record. A malformed record, a record line that carries a comment
// (which would become part of the field's name), a record of an unknown kind or a
// record repeated is an error, and so is a list with an empty half: a list this
// parser skipped or shrank would let a field the PRD excludes through both
// checks, and a repeat would hide its first copy.
//
// The same parser, kept by hand, is test/examples' parseExcludedKeys in
// test/examples/inventoryguidekeys_test.go, behind its readExcludedFixtureKeys
// (the twin of this package's readExcludedFields). Neither test package may
// import the other, so excludedKeysRecordTablePath is what holds the two copies
// to the same documents, and
// TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords runs it against this
// one (review finding on PR #654).
func parseExcludedFields(document string) ([]excludedKeyRecord, error) {
	var (
		records  []excludedKeyRecord
		keys     int
		prefixes int
		// seen maps a record to the line that wrote it, so a repeat names both
		// lines.
		seen = map[string]int{}
	)

	for number, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.Contains(trimmed, "#") {
			return nil, fmt.Errorf("%w: line %d reads %q, want the comment on a line of its own, or the comment is part of the field's name and no field matches it",
				errRecordComment, number+1, trimmed)
		}

		kind, name, ok := strings.Cut(trimmed, ": ")
		if !ok || name == "" {
			return nil, fmt.Errorf("%w: line %d reads %q, want `key: <field>` or `prefix: <prefix>`",
				errRecordMalformed, number+1, trimmed)
		}

		if first, repeated := seen[trimmed]; repeated {
			return nil, fmt.Errorf("%w: line %d repeats line %d's record %q, want one record per field, since a second copy hides the first and leaves unclear which one the two checks read",
				errRecordRepeat, number+1, first, trimmed)
		}

		seen[trimmed] = number + 1

		switch kind {
		case recordKindKey:
			keys++
		case recordKindPrefix:
			prefixes++
		default:
			return nil, fmt.Errorf("%w: line %d reads %q, a record of kind %q, want `key: <field>` or `prefix: <prefix>`",
				errRecordKind, number+1, trimmed, kind)
		}

		records = append(records, excludedKeyRecord{kind: kind, name: name})
	}

	if keys == 0 || prefixes == 0 {
		return nil, fmt.Errorf("%w: it holds %d key(s) and %d prefix(es), want both, since the list is the whole of what both checks exclude and an empty half would let a field the PRD excludes through",
			errRecordEmptyHalf, keys, prefixes)
	}

	return records, nil
}

// readExcludedFields reads the shared list of the fields every projection and
// fixture of the guide's search and messaging calls leaves out (PRD #571), and
// hands it to parseExcludedFields. Its twin is test/examples'
// readExcludedFixtureKeys in test/examples/inventoryguidekeys_test.go, which
// reads the same file for the recorded fixtures with a copy of that parser. This
// package cannot import the other's reader, so it has one of its own, as the two
// readers of test/examples/testdata/namespace_may_follow_delimiters.txt do (issue
// #630); excludedKeysRecordTablePath is the table both run, so a document
// either copy accepted or rejected differently from the other fails both (review
// findings on PRs #639 and #654).
func readExcludedFields(t *testing.T) excludedFields {
	t.Helper()

	records, err := parseExcludedFields(string(readRepositoryFile(t, excludedKeysPath)))
	if err != nil {
		t.Fatalf("%s: %v", excludedKeysPath, err)
	}

	var excluded excludedFields

	for _, record := range records {
		switch record.kind {
		case recordKindKey:
			excluded.names = append(excluded.names, record.name)
		case recordKindPrefix:
			excluded.prefixes = append(excluded.prefixes, record.name)
		}
	}

	return excluded
}

// excludedKeyRecordRejections maps a rejection reason the shared table names to
// the error parseExcludedFields returns for it: a document the table rejects as
// "duplicate" is one this parser and test/examples' must both reject as
// errRecordRepeat, and so on. A table case naming a reason this map does not
// hold fails the table's test rather than shrinking what the two parsers are held
// to.
var excludedKeyRecordRejections = map[string]error{
	"malformed":    errRecordMalformed,
	"unknown kind": errRecordKind,
	"comment":      errRecordComment,
	"duplicate":    errRecordRepeat,
	"empty half":   errRecordEmptyHalf,
}

// excludedKeyRecordCase is one case of the shared table of record documents: a
// list both copies of the parser are given, and the outcome each expects of it.
type excludedKeyRecordCase struct {
	// document is the list text both parsers are given.
	document string
	// want is the records the document holds, each spelled as the list spells it
	// and joined by " ; ", or "" for a document both parsers reject.
	want string
	// rejection is the reason both parsers reject the document, one of
	// excludedKeyRecordRejections' keys, or "" for a document both accept.
	rejection string
}

// name names the case for a subtest.
func (tc excludedKeyRecordCase) name() string {
	outcome := "accepted as " + tc.want
	if tc.rejection != "" {
		outcome = "rejected as " + tc.rejection
	}

	return outcome + ": " + strings.ReplaceAll(tc.document, "\n", ", ")
}

// excludedKeyRecordCases returns the shared table of record documents,
// excludedKeysRecordTablePath: a case per line, `<outcome> | <document>`, with
// `\n` for a newline in the document and the outcome being the records the
// document holds or `rejected: <reason>`. It fails when the table holds no case,
// no accepted document, or no document rejected for one of the reasons above, so
// that a reason the two parsers agree on cannot lose its pin silently.
func excludedKeyRecordCases(t *testing.T) []excludedKeyRecordCase {
	t.Helper()

	var cases []excludedKeyRecordCase

	reasons := map[string]bool{}

	for number, line := range strings.Split(string(readRepositoryFile(t, excludedKeysRecordTablePath)), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		outcome, document, ok := strings.Cut(trimmed, " | ")
		if !ok {
			t.Fatalf("%s line %d reads %q, want `<outcome> | <document>`",
				excludedKeysRecordTablePath, number+1, trimmed)
		}

		tc := excludedKeyRecordCase{document: strings.ReplaceAll(document, `\n`, "\n")}

		if reason, rejected := strings.CutPrefix(outcome, "rejected: "); rejected {
			tc.rejection = reason
			reasons[reason] = true
		} else {
			tc.want = outcome
		}

		cases = append(cases, tc)
	}

	if len(cases) == 0 {
		t.Fatalf("%s holds no case, want one per record document the two parsers are held to",
			excludedKeysRecordTablePath)
	}

	if !slices.ContainsFunc(cases, func(tc excludedKeyRecordCase) bool { return tc.rejection == "" }) {
		t.Errorf("%s holds no accepted document, want one: a table of rejected documents alone says nothing about what the two parsers accept",
			excludedKeysRecordTablePath)
	}

	for reason := range excludedKeyRecordRejections {
		if !reasons[reason] {
			t.Errorf("%s holds no document rejected as %q, want one: a reason no case exercises leaves the two parsers' agreement on it unpinned",
				excludedKeysRecordTablePath, reason)
		}
	}

	return cases
}

// TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords pins that this
// package's reader of the shared excluded-key list and test/examples' agree
// (review finding on PR #654). parseExcludedFields and test/examples'
// parseExcludedKeys, in test/examples/inventoryguidekeys_test.go, are hand-kept
// copies of one parser, since neither test package may import the other, and one
// copy that grew to accept a document the other rejects would let one half of PRD
// #571's promise hold while the other let a field through. Both run
// excludedKeysRecordTablePath, each against its own copy, and a document the
// table holds is accepted as the records it lists, or rejected for the reason it
// names, in both.
func TestTheTwoExcludedKeyReadersAcceptAndRejectTheSameRecords(t *testing.T) {
	t.Parallel()

	for _, tc := range excludedKeyRecordCases(t) {
		t.Run(tc.name(), func(t *testing.T) {
			t.Parallel()

			records, err := parseExcludedFields(tc.document)

			if tc.rejection != "" {
				reason, known := excludedKeyRecordRejections[tc.rejection]
				if !known {
					t.Fatalf("%s rejects a document as %q, a reason this test does not know: add it to excludedKeyRecordRejections, and to test/examples' copy of that map",
						excludedKeysRecordTablePath, tc.rejection)
				}

				if !errors.Is(err, reason) {
					t.Fatalf("parseExcludedFields reads %q as %v, want it rejected as %q (%v): the shared table's document is one both parsers must reject, and for the same reason",
						tc.document, err, tc.rejection, reason)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseExcludedFields rejects %q as %v, want the records %q: the shared table's document is one both parsers must accept",
					tc.document, err, tc.want)
			}

			spellings := make([]string, 0, len(records))
			for _, record := range records {
				spellings = append(spellings, record.spelling())
			}

			if got := strings.Join(spellings, " ; "); got != tc.want {
				t.Errorf("parseExcludedFields reads %q as %q, want %q: the records the document holds, in the list's order",
					tc.document, got, tc.want)
			}
		})
	}
}

// awsSearchAndMessagingCall finds one search or messaging call of an AWS
// command: `aws <service> <operation>` in the guide's spelling.
var awsSearchAndMessagingCall = regexp.MustCompile(`aws (?:opensearchserverless|opensearch|kafka) [a-z0-9-]+`)

// awsRoute53Call finds one Route 53 call of an AWS command: `aws route53
// <operation>` in the guide's spelling.
var awsRoute53Call = regexp.MustCompile(`aws route53 [a-z0-9-]+`)

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
	excluded := readExcludedFields(t)

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

			for _, key := range excluded.names {
				if strings.Contains(call.query, key) {
					t.Errorf("%q's --query names %q, want no such field: PRD #571's *Only the fields the program reads* leaves it out of every projection\n%s",
						call.name, key, command)
				}
			}

			for _, prefix := range excluded.prefixes {
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

// gcpManagedKafkaCall is the call the guide's GCP section reads the settings of
// its Managed Service for Apache Kafka clusters from (PRD #571, milestone 3).
// The clusters themselves come from the asset listing the guide already runs;
// this call adds the capacity the program divides by the cluster's brokers, the
// count Google derives from that capacity.
const gcpManagedKafkaCall = "gcloud managed-kafka clusters list"

// gcpManagedKafkaFields are the fields the guide's call may keep: the cluster's
// name and its capacity, or the two numbers of that capacity (PRD #571, *Only
// the fields the program reads*). The capacity is the cluster's total over its
// brokers, so the program reads it to give each broker its share.
var gcpManagedKafkaFields = []string{"name", "capacityConfig", "vcpuCount", "memoryBytes"}

// gcloudFormatProjection matches the object projection of a gcloud `--format`:
// `json(...)`. A projection that formats the output some other way (a table, a
// value alone) keeps no field of its own, and this check cannot read it.
var gcloudFormatProjection = regexp.MustCompile(`--format='?json\(([^)]*)\)'?`)

// roleName matches a predefined role a guide names: roles/managedkafka.viewer.
var roleName = regexp.MustCompile(`roles/[A-Za-z0-9._-]+`)

// TestInventoryGuideGCPManagedKafkaCallKeepsOnlyTheFieldsItReads pins the GCP
// half of PRD #571's *Only the fields the program reads*, on the guide's command
// text: the Managed Service for Apache Kafka call has an explicit `--format`
// whose projection names the cluster's name and its capacity and nothing else.
// The recorded fixture cannot pin it on its own, since a fixture written after
// the fact could leave out the fields a command without a projection writes.
// The bootstrap address, the TLS settings and the access config a cluster's full
// output holds name the cluster's hosts and the networks allowed to reach them.
func TestInventoryGuideGCPManagedKafkaCallKeepsOnlyTheFieldsItReads(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	gcp := guideSourceByKey(t, "gcp")
	commands := guideCommands(t, guide, gcp)

	var calls []string

	for _, command := range commands {
		if strings.Contains(command, gcpManagedKafkaCall) {
			calls = append(calls, command)
		}
	}

	if len(calls) != 1 {
		t.Fatalf("%s's %s section runs %d `%s` command(s), want exactly one: PRD #571 reads a Managed Service for Apache Kafka cluster's capacity there, and a second call would leave which output the program reads unclear\n%s",
			guidePath, gcp.name, len(calls), gcpManagedKafkaCall, describeCommands(commands))
	}

	match := gcloudFormatProjection.FindStringSubmatch(calls[0])
	if match == nil {
		t.Fatalf("%s's `%s` command passes no `--format='json(...)'` projection, want one naming only %s: without it the call writes the cluster's whole configuration to the file, bootstrap address, TLS config and access config included, and the guide has nothing to read the capacity from\n%s",
			guidePath, gcpManagedKafkaCall, strings.Join(gcpManagedKafkaFields, ", "), calls[0])
	}

	keys := projectionKeys(match[1])

	for _, key := range keys {
		if !slices.Contains(gcpManagedKafkaFields, key) {
			t.Errorf("%s's `%s` --format names %q, want only the fields PRD #571 reads of a cluster (%s): a key the program does not read would keep a field the guide's raw output should drop\n%s",
				guidePath, gcpManagedKafkaCall, key, strings.Join(gcpManagedKafkaFields, ", "), calls[0])
		}
	}

	// A projection that named none of the fields would pass the check above and
	// keep nothing the program reads, so the two the program needs are required by
	// name: the cluster's own, and its capacity or the two numbers of it.
	if !slices.Contains(keys, "name") {
		t.Errorf("%s's `%s` --format names no `name`, want the cluster's own: the program writes the resource's short ID and its region from that name, and a cluster it cannot name is a resource it cannot list\n%s",
			guidePath, gcpManagedKafkaCall, calls[0])
	}

	capacity := slices.Contains(keys, "capacityConfig") ||
		(slices.Contains(keys, "vcpuCount") && slices.Contains(keys, "memoryBytes"))

	if !capacity {
		t.Errorf("%s's `%s` --format names no `capacityConfig` and no both of `vcpuCount` and `memoryBytes`, want one of the two: PRD #571 keeps a cluster's capacity to see that its total is no node's size, and a projection without it leaves the call reading nothing the program uses\n%s",
			guidePath, gcpManagedKafkaCall, calls[0])
	}
}

// gcpRoleBullets returns the predefined roles the guide's "GCP permissions"
// section lists as the narrow ones to prefer, in the guide's order. The roles of
// the fallback paragraph below the list are not bullets of it: they are the
// broad alternative the section recommends against.
func gcpRoleBullets(t *testing.T, guide string) []string {
	t.Helper()

	lines := strings.Split(guide, "\n")

	start := -1

	for i, line := range lines {
		if headingText(line) == "GCP permissions" {
			start = i + 1

			break
		}
	}

	if start < 0 {
		t.Fatalf("docs/guide/inventory.md holds no \"GCP permissions\" heading, so this check read nothing: the section lists the narrow roles the guide's calls need")
	}

	var roles []string

	for _, line := range lines[start:] {
		if headingText(line) != "" {
			break
		}

		if !strings.HasPrefix(strings.TrimSpace(line), "- ") {
			continue
		}

		roles = append(roles, roleName.FindAllString(line, -1)...)
	}

	if len(roles) == 0 {
		t.Fatalf("docs/guide/inventory.md's \"GCP permissions\" section lists no `roles/...` bullet, so this check read nothing: PRD #571 adds the role its Managed Service for Apache Kafka call needs to that list")
	}

	return roles
}

// TestInventoryGuideGCPRolesCoverTheManagedKafkaCall pins the GCP permission
// PRD #571's milestone 3 adds, and the one it forbids: the Managed Service for
// Apache Kafka call reads a cluster's capacity with `roles/managedkafka.viewer`,
// which lacks `managedkafka.clusters.connect` and so reaches no broker and no
// message, while `roles/managedkafka.client` grants the data plane - topics,
// messages and consumer groups - which the guide never touches. The guide's list
// is the only permission it asks a reader to hold, so a missing viewer role
// leaves the call unanswered and a client role would give its holder more access
// than the move needs.
func TestInventoryGuideGCPRolesCoverTheManagedKafkaCall(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	roles := gcpRoleBullets(t, guide)

	if !slices.Contains(roles, "roles/managedkafka.viewer") {
		t.Errorf("%s's GCP permissions list names %v, want `roles/managedkafka.viewer` among them: the guide's `%s` call reads a cluster's configuration with that role, and a reader on the list without it gets no answer from the call and loses every cluster's capacity",
			guidePath, roles, gcpManagedKafkaCall)
	}

	if strings.Contains(guide, "roles/managedkafka.client") {
		t.Errorf("%s names `roles/managedkafka.client`, want it nowhere: that role grants the Kafka data plane (topics, messages and consumer groups), which the guide's read-only calls never touch, and PRD #571 forbids granting it\n%s",
			guidePath, guideRoleParagraphs(guide))
	}
}

// guideRoleParagraphs returns the paragraphs of the guide that name a
// `roles/managedkafka` role, for a failure message.
func guideRoleParagraphs(guide string) string {
	var paragraphs []string

	for _, paragraph := range strings.Split(guide, "\n\n") {
		if strings.Contains(paragraph, "roles/managedkafka") {
			paragraphs = append(paragraphs, paragraph)
		}
	}

	return strings.Join(paragraphs, "\n\n")
}

// azureResourceGraphQueryMarker marks the command of the guide's Azure section
// that builds the Resource Graph query: one shell variable the loop below hands
// to `az graph query`, page after page (PRD #571, milestone 4).
const azureResourceGraphQueryMarker = `query="Resources`

// azureResourceGraphQueryPrefix is how that command opens the variable: the name,
// a `="`, and the query's first pipe-less line.
const azureResourceGraphQueryPrefix = `query="`

// azureHDInsightSourceFields are the fields of an HDInsight cluster the guide's
// Resource Graph branch may name as the source of the properties it packs (PRD
// #571, *Only the fields the program reads*): the cluster's kind, which
// classifies it as messaging.kafka, its clusterState, which decides whether it is
// read at all, and the worker role's name, count, size and disks, which the
// program counts and sizes. Every other field of a cluster stays out of the
// branch, osProfile (the admin user, its SSH key and its domain-join
// credentials), scriptActions (the scripts and their parameters),
// virtualNetworkProfile and connectivityEndpoints first among them.
var azureHDInsightSourceFields = []string{
	"properties.clusterDefinition.kind",
	"properties.clusterState",
	"role.name",
	"role.targetInstanceCount",
	"role.hardwareProfile.vmSize",
	"role.dataDisksGroups[0].disksPerNode",
	"role.dataDisksGroups[0].diskSizeGB",
}

// azureHDInsightRolesPath is the one field the branch may name only as the
// source of an `mv-expand`: Resource Graph cannot trim inside an array, so the
// branch expands the roles into one row per role, keeps the worker role and
// packs that role back as a one-element array. Named as a value of bag_pack, or
// projected as a column, the array would reach the file whole, every role's
// osProfile, scriptActions and virtualNetworkProfile with it.
const azureHDInsightRolesPath = "properties.computeProfile.roles"

// azureHDInsightType is the Resource Manager type of an HDInsight cluster, in the
// casing azure.jq compares it in. Written out once, so the filters below quote it
// rather than spelling it again.
const azureHDInsightType = "microsoft.hdinsight/clusters"

// azureHDInsightTypeFilter matches the filter that opens the branch of the
// Resource Graph query that reads an HDInsight cluster: `where type =~ 'microsoft.hdinsight/clusters'`,
// in any casing and with any spacing. The branch is found by that filter, not by
// the first place the query names the type: keyed on the first occurrence, the
// check read the wrong part of the query as soon as the legs of the union were
// written in another order (review finding N1).
var azureHDInsightTypeFilter = regexp.MustCompile(`(?i)\bwhere\s+type\s+=~\s*'` + regexp.QuoteMeta(azureHDInsightType) + `'`)

// azureHDInsightTypeExclusion matches the one other place the query may name the
// type: the filter that keeps HDInsight clusters out of the branch reading every
// other type, `where type !~ 'microsoft.hdinsight/clusters'`. Any other naming of
// the type, a `case(...)` arm first among them, would pass a cluster's properties
// on whole, past the branch that packs only the fields the program reads.
var azureHDInsightTypeExclusion = regexp.MustCompile(`(?i)\btype\s+!~\s*'` + regexp.QuoteMeta(azureHDInsightType) + `'`)

// azureUnionOpen matches the `| union (` whose parenthesised group holds the
// query's legs (PRD #571, milestone 4).
var azureUnionOpen = regexp.MustCompile(`(?i)\|\s*union\s*\(`)

// azureResourceGraphColumns are the columns the query's final projection writes,
// and the only ones it may write: the resource's short ID, its name, type, kind,
// region, zones and SKU, and the reduced `properties` bag each branch builds.
var azureResourceGraphColumns = []string{"id", "name", "type", "kind", "location", "zones", "sku", "properties"}

// azureFieldPath matches one field path of the guide's Resource Graph query: the
// name `properties` or a role, then one or more dotted segments, each optionally
// indexed, so that properties.clusterDefinition.kind and
// role.dataDisksGroups[0].disksPerNode are each one path. White space around the
// dots and the index is part of the match, so a path the query breaks over two
// lines is still read as one.
var azureFieldPath = regexp.MustCompile(`(?i)\b(?:properties|role)(?:\s*\.\s*[A-Za-z0-9_]+(?:\s*\[\s*\d+\s*\])?)+`)

// azureExpandSource matches the `mv-expand <role> =` the branch must write before
// the roles array it expands.
var azureExpandSource = regexp.MustCompile(`(?i)\bmv-expand\s+[A-Za-z_][A-Za-z0-9_]*\s*=\s*$`)

// azureBareProperties matches the name `properties` wherever a query writes it.
var azureBareProperties = regexp.MustCompile(`(?i)\bproperties\b`)

// azureBagPackOpen matches the start of a `bag_pack(` call, the only place a
// branch writes the keys of a reduced properties bag.
var azureBagPackOpen = regexp.MustCompile(`(?i)\bbag_pack\s*\(`)

// azureResourceGraphQuery returns the KQL the guide's Azure section builds for
// its Resource Graph call.
func azureResourceGraphQuery(t *testing.T, guide string) string {
	t.Helper()

	azure := guideSourceByKey(t, "azure")
	commands := guideCommands(t, guide, azure)

	var assignments []string

	for _, command := range commands {
		if strings.Contains(command, azureResourceGraphQueryMarker) {
			assignments = append(assignments, command)
		}
	}

	if len(assignments) != 1 {
		t.Fatalf("docs/guide/inventory.md's %s section builds %d Resource Graph quer(y/ies) (`%s`), want exactly one: PRD #571's Azure classification reads an HDInsight cluster's properties from that query, and a second one would leave which query the program's fields come from unclear\n%s",
			azure.name, len(assignments), azureResourceGraphQueryMarker, describeCommands(commands))
	}

	command := assignments[0]
	rest := command[strings.Index(command, azureResourceGraphQueryPrefix)+len(azureResourceGraphQueryPrefix):]

	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("docs/guide/inventory.md's Resource Graph query is not closed by a `\"`, so this check cannot read the query it builds\n%s", command)
	}

	return rest[:end]
}

// azureHDInsightBranch returns the part of the Resource Graph query that reads an
// HDInsight cluster: the text from the filter that opens that branch to the end of
// it. A branch ends at the `| union (` that opens the next leg, at the next one's
// type filter, at the `case(...)`'s closing `dynamic(null)`, at the line that
// closes the `union(...)` the branch sits in, and at the query's last `| project`
// line, which names the reduced properties column of every type and so is no
// branch's own text. It reports false when the query opens no branch with a
// `where type =~ 'microsoft.hdinsight/clusters'` filter.
//
// The `| union (` is what ends the branch when the legs are written in the
// guide's order: the filter that keeps clusters out of the next leg is no
// `type =~`, and the leg's `case(...)` arms come after it, so without that
// terminator the branch ran on into the next leg (review finding on PR #685).
//
// The branch is anchored on that filter rather than on the first occurrence of the
// type's name, so the legs of the union may be written in any order (review finding
// N1).
//
// This reads the query's shape rather than parsing KQL: a branch written in
// another shape may end elsewhere, and the review of the guide's change has to
// weigh that. What this cannot decide is left to the review rather than guessed
// at here.
func azureHDInsightBranch(query string) (string, bool) {
	filter := azureHDInsightTypeFilter.FindStringIndex(query)
	if filter == nil {
		return "", false
	}

	start := filter[1]
	end := len(query)

	lowered := strings.ToLower(query)

	for _, terminator := range []string{"type =~", "type in~", "dynamic(null)"} {
		if i := strings.Index(lowered[start:], terminator); i >= 0 {
			end = min(end, start+i)
		}
	}

	if union := azureUnionOpen.FindStringIndex(query[start:]); union != nil {
		end = min(end, start+union[0])
	}

	for _, offset := range azureBranchEnds(query) {
		if offset > start {
			end = min(end, offset)
		}
	}

	return query[start:end], true
}

// azureBranchEnds returns the offsets of the lines that end a branch of a union:
// a line opening with the parenthesised union's closing `)`, and the query's
// last `| project` line, which every branch of the union is fed through.
func azureBranchEnds(query string) []int {
	var ends []int

	for offset := 0; offset < len(query); {
		line := query[offset:]
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}

		if strings.HasPrefix(strings.TrimSpace(line), ")") {
			ends = append(ends, offset)
		}

		offset += len(line) + 1
	}

	if at, _, ok := azureLastProjection(query); ok {
		ends = append(ends, at)
	}

	return ends
}

// azureLastProjection returns the offset of the query's last `| project` line and
// the columns it names: every branch of the union is fed through that projection,
// and it is the only place the query chooses what the file holds of a resource.
func azureLastProjection(query string) (int, []string, bool) {
	at, columns, found := -1, []string(nil), false

	for offset := 0; offset < len(query); {
		line := query[offset:]
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}

		if projection, ok := strings.CutPrefix(strings.TrimSpace(line), "| project "); ok {
			at, found, columns = offset, true, nil

			for _, column := range strings.Split(projection, ",") {
				columns = append(columns, strings.TrimSpace(column))
			}
		}

		offset += len(line) + 1
	}

	return at, columns, found
}

// azureUnionCloseOffset returns the offset of the character that closes the
// parenthesised group a `| union (` opens. A parenthesis inside a quoted KQL
// string is no parenthesis, and a group the query never closes reports false.
func azureUnionCloseOffset(query string) (int, bool) {
	open := azureUnionOpen.FindStringIndex(query)
	if open == nil {
		return 0, false
	}

	depth := 1
	quote := rune(0)

	for offset, r := range query[open[1]:] {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			depth--

			if depth == 0 {
				return open[1] + offset, true
			}
		}
	}

	return 0, false
}

// azureProjectionProblems returns what is wrong with the query's final projection
// (audit finding L2 + review finding N1): the query's outermost last `| project`
// has to come after the line that closes the union, and to write exactly the
// columns the program reads. A projection inside the union leaves each branch
// writing the columns it happens to carry, which is what the reduced `properties`
// bag exists to prevent, and a column the program does not read, the `role` the
// HDInsight branch expands first among them, is a cluster's property the file then
// holds.
func azureProjectionProblems(query string) []string {
	var problems []string

	at, columns, ok := azureLastProjection(query)
	if !ok {
		return append(problems, "ends in no `| project`, want one after the union naming exactly "+strings.Join(azureResourceGraphColumns, ", ")+": without it the query writes every column of every resource to the file, and the program's fields cannot be told from the rest")
	}

	if closeOffset, ok := azureUnionCloseOffset(query); !ok {
		problems = append(problems, "closes no `| union (...)`, want the resources of every type unioned in one parenthesised group with the final projection after it")
	} else if at < closeOffset {
		problems = append(problems, "projects its columns inside the union's parentheses, want the `| project` on a line after the one that closes the group: a projection inside the union leaves each branch writing the columns it happens to carry, and the worker `role` the HDInsight branch expands reaches the file with them")
	}

	want := slices.Clone(azureResourceGraphColumns)
	got := slices.Clone(columns)

	slices.Sort(want)
	slices.Sort(got)

	if !slices.Equal(want, got) {
		problems = append(problems, fmt.Sprintf("projects the columns %v, want exactly %v: a column the program does not read, the worker `role` the HDInsight branch expands first among them, is a property of the resource the file then holds whole, and a column it reads but the query leaves out is missing from every resource",
			columns, azureResourceGraphColumns))
	}

	return problems
}

// azureHDInsightTypeOccurrenceProblems returns what is wrong with every place the
// query names `microsoft.hdinsight/clusters` besides the filter that opens its
// branch (audit finding L1): each has to be a `type !~` exclusion. A `case(...)`
// arm that named the type would pass a cluster's properties on whole, osProfile
// and connectivityEndpoints with them, past the branch that packs only the fields
// the program reads.
func azureHDInsightTypeOccurrenceProblems(query string) []string {
	var problems []string

	lowered := strings.ToLower(query)
	exclusions := azureHDInsightTypeExclusion.FindAllStringIndex(query, -1)
	filter := azureHDInsightTypeFilter.FindStringIndex(query)

	for offset := 0; offset < len(query); {
		found := strings.Index(lowered[offset:], azureHDInsightType)
		if found < 0 {
			break
		}

		at := offset + found
		offset = at + len(azureHDInsightType)

		if filter != nil && at >= filter[0] && at < filter[1] {
			continue
		}

		if slices.ContainsFunc(exclusions, func(exclusion []int) bool { return at >= exclusion[0] && at < exclusion[1] }) {
			continue
		}

		problems = append(problems, "names microsoft.hdinsight/clusters outside the filter that opens its branch and outside a `type !~` exclusion, want it in neither: a `case(...)` arm that names the type passes a cluster's properties on whole, osProfile, connectivityEndpoints and its scripts with them, so the branch that packs only the fields the program reads is the one place a cluster may be read")
	}

	return problems
}

// azureResourceGraphQueryProblems returns what is wrong with a Resource Graph
// query of the guide's shape: its final projection, the reduced `properties` bag
// every branch has to build, the places it names an HDInsight cluster's type, and
// the fields its HDInsight branch keeps. It returns one sentence per problem and
// none for the query the guide has to write.
func azureResourceGraphQueryProblems(query string, hdInsightExcluded []string) []string {
	problems := azureProjectionProblems(query)

	if !azurePacksProperties(query) {
		problems = append(problems, "builds no reduced `properties` bag (`properties = bag_pack(...)`, or the `extend properties = case(...)` it has today), want one: without it the query writes every resource's properties whole, an HDInsight cluster's osProfile and connectivityEndpoints included")
	}

	problems = append(problems, azureHDInsightTypeOccurrenceProblems(query)...)

	branch, ok := azureHDInsightBranch(query)
	if !ok {
		return append(problems, "opens no branch with a `where type =~ 'microsoft.hdinsight/clusters'` filter, want one that expands the cluster's worker roles and packs only "+strings.Join(azureHDInsightSourceFields, ", ")+": an HDInsight cluster of kind Kafka is messaging.kafka (PRD #571), and the program reads its kind, its worker count and its workers' size from that branch")
	}

	for _, problem := range azureHDInsightBranchProblems(branch, hdInsightExcluded) {
		problems = append(problems, "keeps an HDInsight branch that "+problem)
	}

	return problems
}

// azureHDInsightBranchProblems returns what is wrong with the part of the query
// that reads an HDInsight cluster, one sentence per problem and no problem for a
// branch that keeps only the fields the program reads (PRD #571). The names in
// hdInsightExcluded are the fields the branch must hold nowhere, read from the
// shared excluded-key list (excludedKeysPath, through this package's
// readExcludedFields) rather than from a copy kept here, so the guide's query
// check and the fixture check cannot drift apart while they read the one list:
// PRD #571 excludes osProfile (the cluster's admin user, its SSH key and its
// domain-join credentials), connectivityEndpoints (the hosts it answers on),
// scriptActions and virtualNetworkProfile, and the cluster's securityProfile,
// storageProfile and kafkaRestProperties with them (audit finding and review
// finding N3 on PRD #571's milestone 4; three properties of a cluster had reached
// neither).
//
// A field is named only as a whole, as a segment of one of the property paths the
// branch writes or as the quoted key of a `bag_pack(...)` call, and compared
// whatever its case: a field whose name merely holds an excluded one inside it, a
// key `'storageProfileless'` or the path role.hardwareProfile.vmSize, is no naming
// of that field (review suggestion on PR #685).
func azureHDInsightBranchProblems(branch string, hdInsightExcluded []string) []string {
	var problems []string

	expansions := 0

	named := map[string]bool{}

	for _, loc := range azureFieldPath.FindAllStringIndex(branch, -1) {
		path := withoutSpace(branch[loc[0]:loc[1]])
		named[path] = true

		if strings.EqualFold(path, azureHDInsightRolesPath) {
			if !azureExpandsInPlace(branch, loc[0]) {
				problems = append(problems, fmt.Sprintf("names %q as a value or a column, want it only as the source of an `mv-expand`: the array passed whole carries every role's osProfile, scriptActions and virtualNetworkProfile into the file the program reads", path))

				continue
			}

			expansions++

			continue
		}

		if !slices.Contains(azureHDInsightSourceFields, path) {
			problems = append(problems, fmt.Sprintf("names %q, want only the fields the program reads of a cluster (%s): a field the program does not read would keep a property the guide's raw output should drop",
				path, strings.Join(azureHDInsightSourceFields, ", ")))
		}
	}

	if expansions == 0 {
		problems = append(problems, "never expands the roles array with an `mv-expand`, want one naming properties.computeProfile.roles: Resource Graph cannot trim inside an array, so the branch expands it and packs the worker role back alone")
	}

	for _, field := range azureHDInsightSourceFields {
		if !named[field] {
			problems = append(problems, fmt.Sprintf("names no %q, want it: the program reads the cluster's kind to classify it, its clusterState to leave out a cluster being deleted or failed, and its worker role to count and size it", field))
		}
	}

	if !strings.Contains(strings.ToLower(branch), "workernode") {
		problems = append(problems, "names no worker role, want the branch to filter one by name (`where role.name =~ 'workernode'`): a cluster's worker count and size are its workers' alone, not its head nodes'")
	}

	for _, excluded := range hdInsightExcluded {
		if azureHDInsightBranchNamesField(branch, excluded) {
			problems = append(problems, fmt.Sprintf("names %q, want it nowhere: PRD #571's *Only the fields the program reads* excludes it from every projection of an HDInsight cluster and from the guide's search and messaging calls, and the program reads no field of it", excluded))
		}
	}

	for _, loc := range azureBareProperties.FindAllStringIndex(branch, -1) {
		if azurePropertiesPathKeyOrTarget(branch, loc[0], loc[1]) {
			continue
		}

		problems = append(problems, "names `properties` whole, want the branch to build it with bag_pack from the fields the program reads: a cluster's properties bag passed on as it comes carries osProfile and connectivityEndpoints with it")
	}

	return problems
}

// azureHDInsightBranchNamesField reports whether the branch names field, as one
// of the segments of a property path it writes or as the quoted key of one of its
// `bag_pack(...)` calls. Both comparisons are whole and case-insensitive, so a
// field whose name merely holds an excluded one inside it is no naming of it.
func azureHDInsightBranchNamesField(branch, field string) bool {
	return slices.ContainsFunc(azureFieldPathSegments(branch), func(segment string) bool {
		return strings.EqualFold(segment, field)
	}) || slices.ContainsFunc(azureBagPackKeys(branch), func(key string) bool {
		return strings.EqualFold(key, field)
	})
}

// azureFieldPathSegments returns the segments of every property path the branch
// writes, each without its index: properties.computeProfile.roles gives
// properties, computeProfile and roles, and role.dataDisksGroups[0].disksPerNode
// gives role, dataDisksGroups and disksPerNode.
func azureFieldPathSegments(branch string) []string {
	var segments []string

	for _, loc := range azureFieldPath.FindAllStringIndex(branch, -1) {
		for _, segment := range strings.Split(withoutSpace(branch[loc[0]:loc[1]]), ".") {
			if at := strings.IndexByte(segment, '['); at >= 0 {
				segment = segment[:at]
			}

			segments = append(segments, segment)
		}
	}

	return segments
}

// azureBagPackKeys returns the quoted keys of every `bag_pack(...)` call in the
// branch: the argument at an even position of a call, when it is one quoted string
// and nothing else. A quoted value, one at an odd position, is no key, and neither
// is any other expression.
func azureBagPackKeys(branch string) []string {
	var keys []string

	for _, loc := range azureBagPackOpen.FindAllStringIndex(branch, -1) {
		for i, argument := range azureCallArguments(branch[loc[1]:]) {
			if i%2 != 0 {
				continue
			}

			if key, ok := azureQuotedLiteral(argument); ok {
				keys = append(keys, key)
			}
		}
	}

	return keys
}

// azureCallArguments returns the arguments of the call whose opening parenthesis
// the offset after names, one string per top-level comma, with the call's closing
// `)` left out: a comma inside a quoted string or inside a nested call is neither
// one's to split on.
func azureCallArguments(after string) []string {
	var (
		arguments []string
		opened    int
		quote     byte
		start     int
	)

	for i := 0; i < len(after); i++ {
		switch c := after[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[':
			opened++
		case c == ')' || c == ']':
			if opened == 0 {
				return append(arguments, after[start:i])
			}

			opened--
		case c == ',' && opened == 0:
			arguments = append(arguments, after[start:i])

			start = i + 1
		}
	}

	return nil
}

// azureQuotedLiteral returns the value of an argument that is one quoted string
// and nothing else, and false for any other expression.
func azureQuotedLiteral(argument string) (string, bool) {
	trimmed := strings.TrimSpace(argument)
	if len(trimmed) < 2 {
		return "", false
	}

	quote := trimmed[0]
	if quote != '\'' && quote != '"' {
		return "", false
	}

	literal := trimmed[1 : len(trimmed)-1]
	if trimmed[len(trimmed)-1] != quote || strings.IndexByte(literal, quote) >= 0 {
		return "", false
	}

	return literal, true
}

// azureExpandsInPlace reports whether the path at offset at is the source of an
// `mv-expand <role> =`, the only way the branch may name the roles array.
func azureExpandsInPlace(branch string, at int) bool {
	before := branch[:at]

	if i := strings.LastIndexAny(before, "\n|;"); i >= 0 {
		before = before[i+1:]
	}

	return azureExpandSource.MatchString(collapseSpace(before))
}

// azurePropertiesPathKeyOrTarget reports whether the name `properties` at a match
// of azureBareProperties is no bare naming of the bag: the start of a property
// path, the quoted key of a bag_pack, or the name the branch packs its reduced
// properties into.
func azurePropertiesPathKeyOrTarget(branch string, start, end int) bool {
	after := strings.TrimLeft(branch[end:], " \t\r\n")
	if strings.HasPrefix(after, ".") || strings.HasPrefix(after, "[") || strings.HasPrefix(after, "=") {
		return true
	}

	before := strings.TrimRight(branch[:start], " \t\r\n")

	return strings.HasSuffix(before, "'") || strings.HasSuffix(before, `"`)
}

// azurePacksProperties reports whether the query builds a reduced properties bag
// at a `properties =` assignment of its own, as every shape of it has to.
func azurePacksProperties(query string) bool {
	for _, loc := range azureBareProperties.FindAllStringIndex(query, -1) {
		assigned := strings.HasPrefix(strings.TrimLeft(query[loc[1]:], " \t\r\n"), "=")
		quoted := strings.HasSuffix(strings.TrimRight(query[:loc[0]], " \t\r\n"), "'")

		if assigned && !quoted {
			return true
		}
	}

	return false
}

// TestInventoryGuideAzureResourceGraphQueryKeepsOnlyTheFieldsTheProgramReads
// pins the Azure half of PRD #571's *Only the fields the program reads* on the
// guide's own text: the Resource Graph query keeps its columns with an explicit
// `| project` after the union and no column but those the program reads, and the
// branch that reads an HDInsight cluster names only the properties the program
// reads of it, never osProfile or connectivityEndpoints. The recorded fixture
// cannot pin this on its own, since a fixture written after the fact could leave
// out the fields a query without the projection writes.
func TestInventoryGuideAzureResourceGraphQueryKeepsOnlyTheFieldsTheProgramReads(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	query := azureResourceGraphQuery(t, guide)

	problems := azureResourceGraphQueryProblems(query, readExcludedFields(t).names)

	for _, problem := range problems {
		t.Errorf("%s's Resource Graph query %s\n%s", guidePath, problem, query)
	}
}

// TestInventoryGuideAzureResourceGraphQueryRulesFlagALeakingQuery pins the rules
// the check above applies, against queries written here: a query that named the
// HDInsight type in a `case(...)` arm, that added a column the program does not
// read, or that projected inside the union is reported, and the query the guide
// has to write is not, whatever the order of its legs (audit findings L1 and L2,
// review findings N1 and N3). The check above reads the guide's own query; this
// one holds the rules to queries written here, so a rule that stopped reading a
// leak cannot pass on a query it never saw.
func TestInventoryGuideAzureResourceGraphQueryRulesFlagALeakingQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// query is the Resource Graph query the rules read.
		query string
		// want are the phrases each of which a reported problem has to hold.
		want []string
	}{{
		name:  "the query the guide has to write",
		query: azureResourceGraphQueryOfGuideShape(),
	}, {
		name:  "a query whose legs are written in the other order",
		query: azureOtherTypesLeg + "\n| union (Resources" + azureHDInsightLeg + ")\n" + azureResourceGraphProjection,
	}, {
		name:  "a query whose other leg names an excluded field after the HDInsight branch",
		query: "Resources" + azureHDInsightLeg + "\n| union (" + azureOtherTypesLegNamingAnExcludedField + ")\n" + azureResourceGraphProjection,
		// No want: a field the union's other leg names is no leak of the HDInsight
		// branch, and a branch that ran on past its `| union (` into that leg
		// reported it as one (review finding on PR #685).
	}, {
		name:  "a query that reads HDInsight clusters through a case(...) arm as well",
		query: "Resources" + azureHDInsightLeg + "\n| union (" + azureOtherTypesLegWithoutExclusion + "\n    type =~ 'microsoft.hdinsight/clusters', properties,\n    dynamic(null)))\n" + azureResourceGraphProjection,
		want:  []string{"names microsoft.hdinsight/clusters outside the filter that opens its branch"},
	}, {
		name:  "a query that projects the worker role with the other columns",
		query: azureResourceGraphQueryOfGuideShape() + ", role",
		want:  []string{"projects the columns"},
	}, {
		name:  "a query that projects inside the union's parentheses",
		query: "Resources" + azureHDInsightLeg + "\n| union (" + azureOtherTypesLegWithoutExclusion + "\n    dynamic(null)\n" + azureResourceGraphProjection + "))",
		want:  []string{"projects its columns inside the union's parentheses"},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := azureResourceGraphQueryProblems(tc.query, readExcludedFields(t).names)

			for _, phrase := range tc.want {
				if !slices.ContainsFunc(problems, func(problem string) bool {
					return strings.Contains(problem, phrase)
				}) {
					t.Errorf("azureResourceGraphQueryProblems reported no problem holding %q, want one: a query that leaks a field past PRD #571 has to fail the guide's check, not pass it\nreported:\n%s\nquery:%s",
						phrase, strings.Join(problems, "\n"), tc.query)
				}
			}

			if len(tc.want) == 0 && len(problems) != 0 {
				t.Errorf("azureResourceGraphQueryProblems reported %d problem(s) for a query that keeps only the fields the program reads, want none: this check would fail the query the guide has to write\nreported:\n%s\nquery:%s",
					len(problems), strings.Join(problems, "\n"), tc.query)
			}
		})
	}
}

// azureHDInsightLeg is the leg of the Resource Graph query that reads an HDInsight
// cluster as the guide has to write it: the cluster's kind, which classifies it,
// and its worker role's count, size and disks, packed back field by field.
const azureHDInsightLeg = `
| where type =~ 'microsoft.hdinsight/clusters'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| extend properties = bag_pack(
    'clusterDefinition', bag_pack('kind', properties.clusterDefinition.kind),
    'clusterState', properties.clusterState,
    'computeProfile', bag_pack('roles', pack_array(bag_pack(
      'name', role.name,
      'targetInstanceCount', role.targetInstanceCount,
      'hardwareProfile', bag_pack('vmSize', role.hardwareProfile.vmSize),
      'dataDisksGroups', pack_array(bag_pack(
        'disksPerNode', role.dataDisksGroups[0].disksPerNode,
        'diskSizeGB', role.dataDisksGroups[0].diskSizeGB))))))`

// azureOtherTypesLeg is the union's other leg, which reads every type but an
// HDInsight cluster and packs the properties the program reads of each of them.
const azureOtherTypesLeg = `Resources
| where type !~ 'microsoft.hdinsight/clusters'
| extend properties = case(
    type =~ 'microsoft.compute/virtualmachines', bag_pack(
      'hardwareProfile', bag_pack('vmSize', properties.hardwareProfile.vmSize)),
    dynamic(null))`

// azureOtherTypesLegWithoutExclusion is azureOtherTypesLeg without the filter
// that keeps HDInsight clusters out of it, so that a query written from it can
// read a cluster through a `case(...)` arm instead.
const azureOtherTypesLegWithoutExclusion = `Resources
| extend properties = case(
    type =~ 'microsoft.compute/virtualmachines', bag_pack(
      'hardwareProfile', bag_pack('vmSize', properties.hardwareProfile.vmSize)),`

// azureOtherTypesLegNamingAnExcludedField is the union's other leg as a hand edit
// may write it: neither its filter nor its `case(...)` arm compares a `type =~`
// or a `type in~`, so no type filter ends a branch before it, and the arm names a
// field PRD #571 keeps out of an HDInsight cluster's branch. A branch that ran on
// past its `| union (` read the arm as the cluster's own and reported that field
// (review finding on PR #685).
const azureOtherTypesLegNamingAnExcludedField = `Resources
| where isnotempty(location)
| extend properties = case(
    isnotempty(properties.osProfile), bag_pack('osProfile', properties.osProfile),
    bag_pack('hardwareProfile', bag_pack('vmSize', properties.hardwareProfile.vmSize)))`

// azureResourceGraphProjection is the query's final projection, on the line after
// the one that closes the union.
const azureResourceGraphProjection = `| project id, name, type, kind, location, zones, sku, properties`

// azureResourceGraphQueryOfGuideShape assembles the Resource Graph query the
// guide has to write out of the parts above: the HDInsight branch first, the
// other types unioned in behind it, and the final projection after the union.
func azureResourceGraphQueryOfGuideShape() string {
	return "Resources" + azureHDInsightLeg + "\n| union (" + azureOtherTypesLeg + ")\n" + azureResourceGraphProjection
}

// TestInventoryGuideAzureHDInsightBranchRulesFlagALeakingBranch pins the branch
// rules the check above applies: a branch that passes a field past PRD #571 is
// reported, and the branch the guide has to write is not. The check above reads
// the guide's own branch and the rules it holds to it; this one holds the same
// rules to branches written here, so a rule that stopped reading a leak cannot
// pass on a branch it never saw.
func TestInventoryGuideAzureHDInsightBranchRulesFlagALeakingBranch(t *testing.T) {
	t.Parallel()

	const workerRole = `
      'computeProfile', bag_pack('roles', pack_array(bag_pack(
        'name', role.name,
        'targetInstanceCount', role.targetInstanceCount,
        'hardwareProfile', bag_pack('vmSize', role.hardwareProfile.vmSize),
        'dataDisksGroups', pack_array(bag_pack(
          'disksPerNode', role.dataDisksGroups[0].disksPerNode,
          'diskSizeGB', role.dataDisksGroups[0].diskSizeGB)))))`

	// kind is the part of the branch that classifies a cluster: its kind and the
	// state the program leaves a deleting or failed cluster by.
	const kind = `'clusterDefinition', bag_pack('kind', properties.clusterDefinition.kind),
    'clusterState', properties.clusterState`

	tests := []struct {
		name string
		// branch is the part of the query that reads an HDInsight cluster, as
		// azureHDInsightBranch hands it to the rules.
		branch string
		// want are the phrases each of which a reported problem has to hold.
		want []string
		// notWant are the phrases none of the reported problems may hold: a
		// branch that packs a field the program merely does not read may be
		// reported as naming an unlisted field, and never as naming an excluded
		// one.
		notWant []string
	}{{
		name: "a branch that expands the worker role and packs only the fields the program reads",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, kind, location, zones, sku, properties = bag_pack(` + kind + `,` + workerRole + `)`,
	}, {
		name: "a branch that passes the roles array whole",
		branch: `'
| project id, name, type, location, zones, properties = bag_pack(` + kind + `,
    'computeProfile', bag_pack('roles', properties.computeProfile.roles))`,
		want: []string{
			`names "properties.computeProfile.roles" as a value or a column`,
			"never expands the roles array",
		},
	}, {
		name: "a branch that names a property the program does not read",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, location, zones, properties = bag_pack(` + kind + `,
    'securityProfile', properties.securityProfile,` + workerRole + `)`,
		want: []string{`names "properties.securityProfile"`},
	}, {
		name: "a branch that names connectivityEndpoints, storageProfile and kafkaRestProperties",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, location, zones, properties = bag_pack(` + kind + `,
    'connectivityEndpoints', properties.connectivityEndpoints,
    'storageProfile', properties.storageProfile,
    'kafkaRestProperties', properties.kafkaRestProperties,` + workerRole + `)`,
		want: []string{`names "connectivityEndpoints"`, `names "storageProfile"`, `names "kafkaRestProperties"`},
	}, {
		// The excluded word is a substring of the field's name, not its whole
		// name: the branch names a field whose name merely starts with an
		// excluded one, as `'storageProfileless'` starts with `storageProfile`.
		// That field is one the program does not read, so the branch is reported
		// for it, and it is no field PRD #571 excludes, so the branch may not be
		// reported as naming one (review suggestion on PR #685).
		name: "a branch that packs a field whose name holds an excluded one as a substring",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, kind, location, zones, sku, properties = bag_pack(` + kind + `,` + workerRole + `,
    'storageProfileless', properties.storageProfileless)`,
		want:    []string{`names "properties.storageProfileless"`},
		notWant: []string{"want it nowhere"},
	}, {
		name: "a branch that names osProfile",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, location, zones, properties = bag_pack(` + kind + `,
    'osProfile', role.osProfile,` + workerRole + `)`,
		want: []string{`names "osProfile"`},
	}, {
		name: "a branch that names scriptActions and virtualNetworkProfile",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, location, zones, properties = bag_pack(` + kind + `,
    'scriptActions', role.scriptActions,
    'virtualNetworkProfile', role.virtualNetworkProfile,` + workerRole + `)`,
		want: []string{`names "scriptActions"`, `names "virtualNetworkProfile"`},
	}, {
		name: "a branch that projects properties whole for the type",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| where role.name =~ 'workernode'
| project id, name, type, kind, location, zones, sku, properties`,
		want: []string{"names `properties` whole"},
	}, {
		name: "a branch that names no worker role",
		branch: `'
| mv-expand role = properties.computeProfile.roles
| project id, name, type, kind, location, zones, sku, properties = bag_pack(` + kind + `,` + workerRole + `)`,
		want: []string{"names no worker role"},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := azureHDInsightBranchProblems(tc.branch, readExcludedFields(t).names)

			for _, phrase := range tc.want {
				if !slices.ContainsFunc(problems, func(problem string) bool {
					return strings.Contains(problem, phrase)
				}) {
					t.Errorf("azureHDInsightBranchProblems reported no problem holding %q, want one: a branch that leaks a field past PRD #571 has to fail the guide's check, not pass it\nreported:\n%s\nbranch:%s",
						phrase, strings.Join(problems, "\n"), tc.branch)
				}
			}

			for _, phrase := range tc.notWant {
				if slices.ContainsFunc(problems, func(problem string) bool {
					return strings.Contains(problem, phrase)
				}) {
					t.Errorf("azureHDInsightBranchProblems reported a problem holding %q, want none: a field whose name merely holds an excluded one inside it is no field PRD #571 excludes, and reporting it as one makes the guide's operator rename a field the list never named\nreported:\n%s\nbranch:%s",
						phrase, strings.Join(problems, "\n"), tc.branch)
				}
			}

			if len(tc.want) == 0 && len(problems) != 0 {
				t.Errorf("azureHDInsightBranchProblems reported %d problem(s) for a branch that keeps only the fields the program reads, want none: this check would fail the branch the guide has to write\nreported:\n%s\nbranch:%s",
					len(problems), strings.Join(problems, "\n"), tc.branch)
			}
		})
	}
}

// hdInsightCLICommand is the `az` command the guide must not run (PRD #571, *No
// data-plane or configuration access*): Azure groups its configuration and
// credentials calls for an HDInsight cluster under that command, and the guide
// reads the cluster through the Resource Graph projection alone. The group's name
// is spelled as the CLI spells it, which misspell reads as a misspelling of an
// English word.
//
//nolint:misspell // the Azure CLI's own group name, not a word
const hdInsightCLICommand = "az hdinsight"

// TestInventoryGuideReadsNoHDInsightConfiguration pins PRD #571's *No
// data-plane or configuration access* for Azure: an HDInsight cluster is read
// through the Resource Graph projection alone, so the guide runs no
// configuration command for one and names no configurations action, which would
// ask the cluster for its settings and its credentials instead of their public
// shape.
func TestInventoryGuideReadsNoHDInsightConfiguration(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	for _, source := range guideSources {
		for _, command := range guideCommands(t, guide, source) {
			if strings.Contains(command, hdInsightCLICommand) {
				t.Errorf("%s's %s section runs `%s`, want no such call: PRD #571 reads an HDInsight cluster through the Resource Graph projection alone, and a configuration call reads the cluster's settings and credentials\n%s",
					guidePath, source.name, hdInsightCLICommand, command)
			}
		}
	}

	const configurationsAction = "Microsoft.HDInsight/clusters/configurations/action"

	if strings.Contains(guide, configurationsAction) {
		t.Errorf("%s names `%s`, want it nowhere: it is the configuration and credentials read PRD #571 forbids, and the guide's Azure role grants no such action\n%s",
			guidePath, configurationsAction, guideRoleParagraphs(guide))
	}
}

// TestInventoryGuideAzureEventHubsHandRule pins the sentence beside the Azure
// classification table that PRD #571 asks for: an Event Hubs namespace stays
// unclassified unless every client of it uses the namespace's Kafka endpoint,
// in which case the reader sets its category to messaging.kafka by hand. The
// check reads only the two names the rule cannot leave out, so the wording is
// the guide's to choose.
func TestInventoryGuideAzureEventHubsHandRule(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	var paragraphs []string

	for _, paragraph := range strings.Split(guide, "\n\n") {
		if strings.Contains(paragraph, "Event Hubs") && strings.Contains(paragraph, "messaging.kafka") {
			paragraphs = append(paragraphs, paragraph)
		}
	}

	if len(paragraphs) == 0 {
		t.Errorf("%s names no paragraph holding both `Event Hubs` and `messaging.kafka`, want one beside the Azure classification table: an Event Hubs namespace is no Kafka cluster unless every client of it uses the namespace's Kafka endpoint, and PRD #571 has the guide say when to set its category by hand",
			guidePath)
	}
}

// TestInventoryGuidePagesListCollectionsByHand pins the page limit the guide has
// to work around (review finding B1): `opensearchserverless list-collections`
// has no AWS CLI paginator, so the CLI answers with one page of at most 100
// collections and nothing tells the reader another page is waiting. A guide that
// passes no `--max-results` gets the default page of 20, and one that keeps no
// `nextToken` loses every collection past the first page: the inventory holds
// two of five collections, and the move looks smaller than it is. The guide's
// command therefore asks for the largest page, keeps the token and asks again
// until the token is null, writing every page to the collections file.
func TestInventoryGuidePagesListCollectionsByHand(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	command := awsListCollectionsCommand(t, guide)

	if !strings.Contains(command, "--max-results 100") && !strings.Contains(command, "--max-results=100") {
		t.Errorf("the guide's `aws opensearchserverless list-collections` command passes no `--max-results 100`: the call has no CLI paginator and answers with one page of 20 by default, so a guide that does not ask for its documented maximum of 100 sees fewer collections than the region holds\n%s", command)
	}

	if !strings.Contains(command, "--next-token") {
		t.Errorf("the guide's `aws opensearchserverless list-collections` command passes no `--next-token`: the second page of a region's collections is asked for with the token the first page returned, and a command that never passes it lists the first page only\n%s", command)
	}

	if !strings.Contains(command, "nextToken") {
		t.Errorf("the guide's `aws opensearchserverless list-collections` command never reads `nextToken`, so it cannot tell whether the page it holds is the last one: keep the token in the collections file and stop when it is null\n%s", command)
	}

	if !strings.Contains(command, "while") && !strings.Contains(command, "until") {
		t.Errorf("the guide's `aws opensearchserverless list-collections` command holds no `while` or `until` loop: the CLI has no paginator for the call, so the command itself repeats it page by page and writes each page to the collections file\n%s", command)
	}
}

// unpagedPhrases are the ways the guide may say that the AWS CLI does not page
// `list-collections` for the reader. The check below accepts any of them rather
// than pinning one wording, and names them in its failure, so the sentence only
// has to say that one call is the exception and which one it is.
var unpagedPhrases = []string{"no paginator", "not paginated", "not paginate", "no CLI pagination", "by hand"}

// TestInventoryGuideNamesTheOneCallTheCLIDoesNotPage pins the other half of
// review finding B1: the guide's sentence that tells the reader the AWS CLI
// fetches every page of every call above has to name `list-collections` as the
// one call it does not, and say that the reader pages it. Otherwise the sentence
// it does read promises pages the CLI never fetches, and the collections past
// the first page are missing from the inventory with nothing to show for it.
func TestInventoryGuideNamesTheOneCallTheCLIDoesNotPage(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	var paragraphs []string

	for _, paragraph := range strings.Split(guide, "\n\n") {
		if strings.Contains(paragraph, "paginat") || strings.Contains(paragraph, "every page") {
			paragraphs = append(paragraphs, paragraph)
		}
	}

	if len(paragraphs) == 0 {
		t.Fatalf("docs/guide/inventory.md holds no paragraph about pagination, so this check read nothing: the guide tells the reader the AWS CLI fetches every page of its calls, and names the one call it does not")
	}

	named := make([]string, 0, len(paragraphs))

	for _, paragraph := range paragraphs {
		if strings.Contains(paragraph, "list-collections") {
			named = append(named, paragraph)
		}
	}

	if len(named) == 0 {
		t.Errorf("docs/guide/inventory.md's pagination paragraph does not name `opensearchserverless list-collections`, want it as the exception: `list-collections` has no AWS CLI paginator, so the CLI fetches no second page of collections and the guide's loop does, and a reader told that every call is paged for them loses every collection past the first page\n%s",
			strings.Join(paragraphs, "\n\n"))

		return
	}

	for _, paragraph := range named {
		for _, phrase := range unpagedPhrases {
			if strings.Contains(paragraph, phrase) {
				return
			}
		}
	}

	t.Errorf("docs/guide/inventory.md names `list-collections` in its pagination paragraph but never says the CLI does not page it, want one of %q: the sentence that promises every call above is paginated would still cover the one call without a paginator\n%s",
		unpagedPhrases, strings.Join(named, "\n\n"))
}

// awsListCollectionsCommand returns the guide's `opensearchserverless
// list-collections` command: the lines the visitor copies for it, with the calls
// it feeds. Exactly one command makes the call, so a second one that listed the
// collections again fails here rather than being checked in place of the first.
func awsListCollectionsCommand(t *testing.T, guide string) string {
	t.Helper()

	var commands []string

	for _, command := range guideCommands(t, guide, guideSources[0]) {
		if strings.Contains(command, "aws opensearchserverless list-collections") {
			commands = append(commands, command)
		}
	}

	if len(commands) != 1 {
		t.Fatalf("docs/guide/inventory.md runs `aws opensearchserverless list-collections` in %d commands, want exactly one, so this check read %s",
			len(commands), describeCommands(commands))
	}

	return commands[0]
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

	return awsCallsIn(t, command, awsSearchAndMessagingCall)
}

// awsCallsIn returns the calls of one AWS command that the call pattern finds,
// each with the --query projection it passes, and fails the test when a call
// passes no --query or passes one that is no single-quoted projection: the page
// shows the command whole, so a call that kept the whole response would put the
// response in the scratch directory the guide deletes.
func awsCallsIn(t *testing.T, command string, callPattern *regexp.Regexp) []awsCall {
	t.Helper()

	var calls []awsCall

	for _, location := range callPattern.FindAllStringIndex(command, -1) {
		name := strings.Join(strings.Fields(command[location[0]:location[1]]), " ")

		rest := command[location[1]:]

		at := strings.Index(rest, "--query")
		if at < 0 {
			t.Errorf("%q passes no --query, want an explicit projection that keeps only the fields the program reads\n%s",
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

// awsIAMCall is one `aws <service> <operation>` call of the guide's AWS collect
// commands, which the policy check maps to an IAM action.
type awsIAMCall struct {
	service   string
	operation string
}

// awsCollectCall is one call of the guide's AWS collect commands, with the IAM
// action that permits it and the page of the AWS Service Authorization Reference
// (https://docs.aws.amazon.com/service-authorization/latest/reference/) that
// lists the action.
//
// The action is written out, never derived from the call's name, because a call's
// action is not always its operation in PascalCase: `aws rds describe-db-instances`
// needs rds:DescribeDBInstances (not rds:DescribeDbInstances), and `aws s3api
// list-buckets` needs s3:ListAllMyBuckets. No call of the guide differs today
// beyond those two, but the next one may: `aws s3api get-bucket-encryption`,
// which the guide does not make, needs s3:GetEncryptionConfiguration, a name the
// operation's own spelling does not hold. A derived name can be no action IAM
// knows at all, so a policy written to match the derivation grants nothing and
// the reader who follows the guide meets AccessDenied (review finding on PR
// #639).
type awsCollectCall struct {
	service   string
	operation string
	// action is the IAM action the AWS Service Authorization Reference lists for
	// the operation, in its `service:ActionName` spelling.
	action string
	// page is the reference's page for the operation's service.
	page string
	// optional says the guide tells its reader the call (and so its action) is
	// optional: needed for one capability only, and leaving that capability out
	// leaves the call out too. The policy may grant it or not, so the check below
	// accepts either (PRD #581, *The record-set permissions*).
	optional bool
}

// awsServiceAuthorizationReference is where AWS publishes the action each
// service's API calls need: one page per service, named in each page of
// awsCollectCalls.
const awsServiceAuthorizationReference = "https://docs.aws.amazon.com/service-authorization/latest/reference/"

// awsCollectCalls is every `aws <service> <operation>` call the guide's AWS
// collect commands make, with its IAM action and that action's citation. It is
// the whole of what the policy check knows: a call the guide gains without a row
// fails the check, naming the call, so the policy cannot fall behind the
// commands (PRD #571, milestone 2).
//
// A CLI service whose name is not its IAM prefix says so on its rows: `aws
// configservice` drives AWS Config (config), `aws efs` Elastic File System
// (elasticfilesystem), `aws elb` and `aws elbv2` both Elastic Load Balancing
// (elasticloadbalancing), `aws opensearch` OpenSearch Service (es), `aws
// opensearchserverless` OpenSearch Serverless (aoss) and `aws s3api` S3 (s3).
var awsCollectCalls = []awsCollectCall{
	// Every page below is the reference's current short name for its service. The
	// long names answer 301 to these pages or, for EKS, MSK, RDS, SQS and SNS, 302
	// to the reference index, so a citation of one of those led the reader nowhere
	// (review finding on PR #639).
	// list_autoscaling.html.
	{service: "autoscaling", operation: "describe-auto-scaling-groups", action: "autoscaling:DescribeAutoScalingGroups", page: "list_autoscaling.html"},
	// list_config.html: AWS Config's page. `aws configservice select-resource-config`
	// is the optional call that lists everything else a region holds.
	{service: "configservice", operation: "select-resource-config", action: "config:SelectResourceConfig", page: "list_config.html"},
	// list_ec2.html. The four describe calls need one action each, and
	// describe-instance-types is the one the audit found the policy missing: the
	// domain and broker sizes are read from it.
	{service: "ec2", operation: "describe-instances", action: "ec2:DescribeInstances", page: "list_ec2.html"},
	{service: "ec2", operation: "describe-instance-types", action: "ec2:DescribeInstanceTypes", page: "list_ec2.html"},
	{service: "ec2", operation: "describe-volumes", action: "ec2:DescribeVolumes", page: "list_ec2.html"},
	{service: "ec2", operation: "describe-snapshots", action: "ec2:DescribeSnapshots", page: "list_ec2.html"},
	// list_efs.html: `aws efs` drives Elastic File System, whose IAM prefix is
	// elasticfilesystem.
	{service: "efs", operation: "describe-file-systems", action: "elasticfilesystem:DescribeFileSystems", page: "list_efs.html"},
	// list_eks.html: the four calls read the region's clusters and their
	// nodegroups, and their actions keep the API's spelling of both
	// (ListNodegroups, DescribeNodegroup).
	{service: "eks", operation: "list-clusters", action: "eks:ListClusters", page: "list_eks.html"},
	{service: "eks", operation: "describe-cluster", action: "eks:DescribeCluster", page: "list_eks.html"},
	{service: "eks", operation: "list-nodegroups", action: "eks:ListNodegroups", page: "list_eks.html"},
	{service: "eks", operation: "describe-nodegroup", action: "eks:DescribeNodegroup", page: "list_eks.html"},
	// list_elasticache.html: the three calls read the caches of the region.
	{service: "elasticache", operation: "describe-replication-groups", action: "elasticache:DescribeReplicationGroups", page: "list_elasticache.html"},
	{service: "elasticache", operation: "describe-cache-clusters", action: "elasticache:DescribeCacheClusters", page: "list_elasticache.html"},
	{service: "elasticache", operation: "describe-serverless-caches", action: "elasticache:DescribeServerlessCaches", page: "list_elasticache.html"},
	// list_elb.html (Classic) and list_elbv2.html: both APIs' actions carry the
	// elasticloadbalancing prefix, and Classic's describe-load-balancers is the v2
	// call's action too.
	{service: "elb", operation: "describe-load-balancers", action: "elasticloadbalancing:DescribeLoadBalancers", page: "list_elb.html"},
	{service: "elbv2", operation: "describe-load-balancers", action: "elasticloadbalancing:DescribeLoadBalancers", page: "list_elbv2.html"},
	{service: "elbv2", operation: "describe-listeners", action: "elasticloadbalancing:DescribeListeners", page: "list_elbv2.html"},
	{service: "elbv2", operation: "describe-target-groups", action: "elasticloadbalancing:DescribeTargetGroups", page: "list_elbv2.html"},
	{service: "elbv2", operation: "describe-tags", action: "elasticloadbalancing:DescribeTags", page: "list_elbv2.html"},
	// list_kafka.html: MSK's prefix is kafka, and the CLI's list-clusters-v2 is
	// the API's ListClustersV2.
	{service: "kafka", operation: "list-clusters-v2", action: "kafka:ListClustersV2", page: "list_kafka.html"},
	// list_lambda.html.
	{service: "lambda", operation: "list-functions", action: "lambda:ListFunctions", page: "list_lambda.html"},
	// list_es.html: `aws opensearch` drives OpenSearch Service, whose actions IAM
	// spells with the es prefix.
	{service: "opensearch", operation: "list-domain-names", action: "es:ListDomainNames", page: "list_es.html"},
	{service: "opensearch", operation: "describe-domains", action: "es:DescribeDomains", page: "list_es.html"},
	// list_opensearchserverless.html: OpenSearch Serverless's prefix is aoss.
	{service: "opensearchserverless", operation: "list-collections", action: "aoss:ListCollections", page: "list_opensearchserverless.html"},
	{service: "opensearchserverless", operation: "batch-get-collection", action: "aoss:BatchGetCollection", page: "list_opensearchserverless.html"},
	// list_rds.html: the RDS API calls the identifier DBInstance, so its action
	// spells the acronym in caps rather than the operation's DescribeDbInstances.
	{service: "rds", operation: "describe-db-instances", action: "rds:DescribeDBInstances", page: "list_rds.html"},
	// list_s3.html: `aws s3api` calls the S3 API, whose prefix is s3. The CLI's
	// list-buckets returns the buckets of the account, which is why IAM names the
	// action for the buckets (ListAllMyBuckets) rather than for the call.
	{service: "s3api", operation: "list-buckets", action: "s3:ListAllMyBuckets", page: "list_s3.html"},
	{service: "s3api", operation: "get-bucket-versioning", action: "s3:GetBucketVersioning", page: "list_s3.html"},
	// list_route53.html: Route 53's prefix is route53. `list-hosted-zones` lists
	// the public and private zones of the account, and `get-dnssec` a zone's signed
	// state, which is the dnssec capability (PRD #581, milestone 2).
	// `list-resource-record-sets` is the optional call the dns-routing capability
	// needs: it returns every record of a zone to its caller, so the guide marks
	// the action optional rather than asking every reader for it.
	{service: "route53", operation: "list-hosted-zones", action: "route53:ListHostedZones", page: "list_route53.html"},
	{service: "route53", operation: "get-dnssec", action: "route53:GetDNSSEC", page: "list_route53.html"},
	{
		service: "route53", operation: "list-resource-record-sets",
		action: "route53:ListResourceRecordSets", page: "list_route53.html", optional: true,
	},
	// list_sqs.html: the policy needs the account's queue list and one queue's
	// attributes.
	{service: "sqs", operation: "list-queues", action: "sqs:ListQueues", page: "list_sqs.html"},
	{service: "sqs", operation: "get-queue-attributes", action: "sqs:GetQueueAttributes", page: "list_sqs.html"},
	// list_sns.html: the same for a topic.
	{service: "sns", operation: "list-topics", action: "sns:ListTopics", page: "list_sns.html"},
	{service: "sns", operation: "get-topic-attributes", action: "sns:GetTopicAttributes", page: "list_sns.html"},
}

// awsCollectCallRows indexes awsCollectCalls by the call each row names, and
// fails on a call the table names twice: one call has one IAM action, and a
// second row would hide the first row's action from the check.
func awsCollectCallRows(t *testing.T) map[awsIAMCall]awsCollectCall {
	t.Helper()

	rows := make(map[awsIAMCall]awsCollectCall, len(awsCollectCalls))

	for _, row := range awsCollectCalls {
		call := awsIAMCall{service: row.service, operation: row.operation}

		if _, ok := rows[call]; ok {
			t.Fatalf("awsCollectCalls names `aws %s %s` twice, want one row per call: one call has one IAM action, and a second row would hide the first row's action",
				row.service, row.operation)
		}

		rows[call] = row
	}

	return rows
}

// TestInventoryGuideAWSPolicyCoversEveryCollectCall pins that the minimal policy
// the guide tells a reader to create grants every call the guide's AWS collect
// commands make (PRD #571, milestone 2): a reader who follows the guide with that
// policy and no other permission must not meet AccessDenied, which would leave
// the resources of the call out of the inventory unsized. The audit found the
// policy missing ec2:DescribeInstanceTypes, the only call the policy lacked.
//
// Each call's action comes from awsCollectCalls, a table written out from the AWS
// Service Authorization Reference and cited row by row, not derived from the
// operation's name: a derivation accepts a policy whose action is no action IAM
// knows, which grants nothing (review finding on PR #639). A call the table does
// not hold fails the check too, so a new command cannot slip past it.
func TestInventoryGuideAWSPolicyCoversEveryCollectCall(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	actions := guideAWSPolicyActions(t, guide)

	source := guideSourceByKey(t, "aws")

	var commands []string

	for _, block := range fencedBashBlocks(t, guide, source.collect) {
		commands = append(commands, splitCommands(block)...)
	}

	if len(commands) == 0 {
		t.Fatalf("docs/guide/inventory.md holds no bash block under %q, so this check read nothing", source.collect)
	}

	calls := guideAWSCalls(t, commands)
	if len(calls) == 0 {
		t.Fatalf("the guide's %q commands hold no `aws <service> <operation>` call, so this check read nothing:\n%s",
			source.collect, describeCommands(commands))
	}

	rows := awsCollectCallRows(t)

	var unlisted, ungranted []string

	for _, call := range calls {
		row, ok := rows[call]
		if !ok {
			unlisted = append(unlisted, fmt.Sprintf("`aws %s %s`", call.service, call.operation))

			continue
		}

		if !slices.Contains(actions, row.action) {
			// A call the guide marks optional may be left out of the policy along
			// with the capability it serves: the reader who leaves dns-routing out
			// makes no record-set call, so the policy need not grant the action.
			if row.optional {
				continue
			}

			ungranted = append(ungranted, fmt.Sprintf("%s for `aws %s %s`, the action %s%s lists",
				row.action, call.service, call.operation, awsServiceAuthorizationReference, row.page))
		}
	}

	if len(unlisted) > 0 {
		t.Errorf("the guide's AWS collect commands make %d call(s) awsCollectCalls holds no row for, want a row for every one of them:\n  %s\nAdd each call with the IAM action the AWS Service Authorization Reference lists for it and the page that lists it (%s), so this check covers the call rather than trusting a name derived from the operation, which may be no action IAM knows (review finding on PR #639)",
			len(unlisted), strings.Join(unlisted, "\n  "), awsServiceAuthorizationReference)
	}

	if len(ungranted) > 0 {
		t.Errorf("the guide's AWS collect commands make %d call(s) whose IAM action the guide's minimal policy does not allow, want every one of them allowed:\n  %s\nThe policy is the only permission the guide asks a reader to hold, so each of these calls meets AccessDenied and the resource it describes or sizes stays out of the inventory (PRD #571, milestone 2, audit finding)",
			len(ungranted), strings.Join(ungranted, "\n  "))
	}

	// The table names no call the guide has stopped making: a row left behind
	// would be an action the check keeps asking the policy for while no command
	// makes the call, and the next reader would not know which row is dead.
	for _, row := range awsCollectCalls {
		call := awsIAMCall{service: row.service, operation: row.operation}

		if slices.Contains(calls, call) {
			continue
		}

		t.Errorf("awsCollectCalls holds `aws %s %s` -> %s, a call the guide's %q commands no longer make: drop the row with the command that made it, or the check asks the policy for an action nothing uses",
			row.service, row.operation, row.action, source.collect)
	}
}

// awsReadActionPrefixes are the operation-name prefixes of the IAM actions that
// only read: a Describe, List, Get, BatchGet or Select action says what a resource
// is or which resources there are, and changes nothing.
var awsReadActionPrefixes = []string{"Describe", "List", "Get", "BatchGet", "Select"}

// TestInventoryGuideAWSPolicyGrantsOnlyReadActions pins the read-only rule of the
// policy the guide tells a reader to create (CLAUDE.md rule 1, PRD #571,
// milestone 2): every action it lists is a read, its operation starting with
// Describe, List, Get, BatchGet or Select, and no action holds a wildcard, which
// would grant whatever AWS adds to the same prefix later. The policy is the one
// permission the guide asks a reader to hold, so an action that changed
// something would break the inventory's single promise.
func TestInventoryGuideAWSPolicyGrantsOnlyReadActions(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	actions := guideAWSPolicyActions(t, guide)

	for _, action := range actions {
		if strings.Contains(action, "*") {
			t.Errorf("the guide's AWS policy allows %q, want no wildcard: the policy grants the reads the page's calls make, and a wildcard grants whatever AWS adds to the same prefix later",
				action)

			continue
		}

		service, operation, ok := strings.Cut(action, ":")
		if !ok || service == "" || operation == "" {
			t.Errorf("the guide's AWS policy allows %q, want `service:Operation`, so this check can read the operation the action names",
				action)

			continue
		}

		if !slices.ContainsFunc(awsReadActionPrefixes, func(prefix string) bool { return strings.HasPrefix(operation, prefix) }) {
			t.Errorf("the guide's AWS policy allows %q, want a read: its operation starts with one of %s, and %q starts with none of them",
				action, strings.Join(awsReadActionPrefixes, ", "), operation)
		}
	}
}

// guideAWSCalls returns every `aws <service> <operation>` call the commands
// make, in the guide's order and once each. A call counts wherever it stands,
// including after an `xargs`, and a comment's words never do: the guide's
// comments name services and operations in prose.
func guideAWSCalls(t *testing.T, commands []string) []awsIAMCall {
	t.Helper()

	var calls []awsIAMCall

	for _, command := range commands {
		words := guideCallWords(command)

		for i, word := range words {
			if word != "aws" {
				continue
			}

			if i+2 >= len(words) || strings.HasPrefix(words[i+1], "-") || strings.HasPrefix(words[i+2], "-") {
				t.Fatalf("the guide's AWS collect command holds `aws` with no service and operation after it, so this check cannot map the call to an IAM action:\n%s", command)
			}

			call := awsIAMCall{service: words[i+1], operation: words[i+2]}

			if !slices.Contains(calls, call) {
				calls = append(calls, call)
			}
		}
	}

	return calls
}

// shellPunctuation matches the punctuation that can glue a word to the `aws` of
// a call: a command substitution keeps `aws` inside `page=$(aws`, and a pipe or
// a redirect can hold the words apart with no space at all.
var shellPunctuation = strings.NewReplacer(
	"$(", " ", "(", " ", ")", " ", "`", " ",
	"{", " ", "}", " ", ";", " ", "|", " ", "&", " ", "=", " ",
)

// guideCallWords returns the words of a command's code, without the comment lines
// the guide uses to describe the call a comment heads, and with the shell's
// punctuation split off, so a call the guide writes inside a substitution or
// after a pipe yields the words `aws`, its service and its operation like any
// other.
func guideCallWords(command string) []string {
	var code []string

	for _, line := range strings.Split(command, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		code = append(code, line)
	}

	return strings.Fields(shellPunctuation.Replace(strings.Join(code, "\n")))
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

// awsRoute53Calls are the calls PRD #581's AWS classification makes of Route 53,
// and the only keys each call's --query may name: a zone's identity and whether
// it is private, its signed state, and a count of its routed record sets. No call
// names a record's values, its name, its routing settings or the keys DNSSEC
// signs a zone with.
var awsRoute53Calls = map[string][]string{
	"aws route53 list-hosted-zones": {
		// HostedZones is the response's list of zones, the one the projection maps
		// over, as ResourceRecordSets is for the record-set call; Config is the
		// parent object of the one field read, Config.PrivateZone.
		"HostedZones", "Id", "Name", "PrivateZone", "Config",
	},
	"aws route53 get-dnssec": {
		"Status", "ServeSignature",
	},
	"aws route53 list-resource-record-sets": {
		// The predicate counts the record sets that carry a field
		// awsRoute53RoutingMarkers lists; ResourceRecordSets is the response's list,
		// the one the predicate filters. The count is all the program reads.
		"ResourceRecordSets", "SetIdentifier", "HealthCheckId", "MultiValueAnswer",
		"TrafficPolicyInstanceId",
	},
}

// awsRoute53RoutingMarkers are the fields a record set of a hosted zone carries
// when it routes by client or by health rather than answering the same way for
// every client: a set identifier (weighted, latency, failover, geolocation,
// geoproximity, IP-based and multivalue answer routing all need one),
// a health check, a multivalue answer, and a TrafficPolicyInstanceId, which a
// record set a Route 53 traffic policy created carries because its routing lives
// in the policy (PRD #581, *The AWS classification*). The guide's --query counts
// the record sets that carry any one of them, and a marker the projection leaves
// out makes a zone answer by client or by health without the `dns-routing`
// capability that says so.
var awsRoute53RoutingMarkers = []string{"SetIdentifier", "HealthCheckId", "MultiValueAnswer", "TrafficPolicyInstanceId"}

// route53ZoneIDArgument matches the argument a per-zone Route 53 call passes: the
// zone's ID, quoted, and never its name, which two zones of the same account
// cannot share but which the call does not accept (PRD #581, *The per-zone
// loops*). The loop's variable is the one the guide reads the zone list into.
var route53ZoneIDArgument = regexp.MustCompile(`--hosted-zone-id[= ]+"\$(?:id|zone)"`)

// TestInventoryGuideAWSRoute53CallsKeepOnlyTheFieldsTheyRead pins the AWS half of
// PRD #581's milestone 2 on the guide's command text: `list-hosted-zones` keeps
// only each zone's ID, name and private flag; `get-dnssec` keeps the signed state
// alone, never the key-signing keys; and `list-resource-record-sets` keeps a
// count of the routed record sets rather than any record. A --query that named a
// field of a record would leave the user's hosts and their addresses in the raw
// output the guide tells the reader to delete.
func TestInventoryGuideAWSRoute53CallsKeepOnlyTheFieldsTheyRead(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	seen := map[string]bool{}

	for _, command := range guideCommands(t, guide, guideSources[0]) {
		for _, call := range awsCallsIn(t, command, awsRoute53Call) {
			seen[call.name] = true

			allowed := awsRoute53Calls[call.name]
			if allowed == nil {
				t.Errorf("docs/guide/inventory.md runs %q, a call PRD #581 does not list: the zone classification is `aws route53 list-hosted-zones`, and per public zone `get-dnssec` and `list-resource-record-sets`\n%s",
					call.name, command)

				continue
			}

			keys := projectionKeys(call.query)

			for _, key := range keys {
				if !slices.Contains(allowed, key) {
					t.Errorf("%q's --query names %q, want only the fields PRD #581 reads of that call (%s): a key the program does not read would keep a field of a zone's records in the raw output\n%s",
						call.name, key, strings.Join(allowed, ", "), command)
				}
			}

			if !slices.ContainsFunc(allowed, func(key string) bool { return slices.Contains(keys, key) }) {
				t.Errorf("%q's --query names none of the fields PRD #581 reads of that call (%s), so the program has nothing to classify a zone from\n%s",
					call.name, strings.Join(allowed, ", "), command)
			}

			if call.name != "aws route53 list-hosted-zones" && !route53ZoneIDArgument.MatchString(command) {
				t.Errorf("%q passes no `--hosted-zone-id \"$id\"`, want the zone's ID quoted: the call asks for one zone by its ID, and the guide's loop reads the ID of each zone the list returned into `$id` (PRD #581, *The per-zone loops*)\n%s",
					call.name, command)
			}

			if call.name == "aws route53 list-resource-record-sets" {
				for _, marker := range awsRoute53RoutingMarkers {
					if !slices.Contains(keys, marker) {
						t.Errorf("%q's --query names no %q, want every field that makes a record set one that routes by client or by health (%s): a record set a traffic policy created carries no set identifier and no health check of its own, so a count that leaves its TrafficPolicyInstanceId out counts a zone that answers by policy as one that does not (PRD #581)\n%s",
							call.name, marker, strings.Join(awsRoute53RoutingMarkers, ", "), command)
					}
				}
			}
		}
	}

	for name := range awsRoute53Calls {
		if !seen[name] {
			t.Errorf("docs/guide/inventory.md runs no %q, want one: PRD #581's AWS classification lists the hosted zones, reads each public zone's signed state, and counts each public zone's routed record sets",
				name)
		}
	}
}

// TestInventoryGuideAWSRoute53LoopReadsTheZoneList pins the loop PRD #581's
// milestone 2 runs (PRD #581, *The per-zone loops*): the calls that ask for one
// zone's DNSSEC state and one zone's record sets run once per zone, over the list
// `list-hosted-zones` wrote, read line by line with `while IFS= read -r`. A loop
// read with a bare `read` drops a trailing backslash, and one fed by `for` splits
// the zone's ID on white space, so a zone would be asked for under a mangled
// name and its capability would be lost.
func TestInventoryGuideAWSRoute53LoopReadsTheZoneList(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	commands := guideCommands(t, guide, guideSources[0])

	// The file each per-zone call's output is recorded in: the fixture under
	// test/e2e/testdata/inventory-guide/aws/<region>/ holds exactly what the call
	// writes there, and the program reads that shape.
	outputFiles := map[string]string{
		"aws route53 get-dnssec":                "route53-dnssec.json",
		"aws route53 list-resource-record-sets": "route53-routed-record-sets.json",
	}

	seen := map[string]bool{}

	for _, command := range commands {
		var perZoneCalls []string

		for call := range outputFiles {
			if strings.Contains(command, call) {
				perZoneCalls = append(perZoneCalls, call)
			}
		}

		slices.Sort(perZoneCalls)

		for _, call := range perZoneCalls {
			seen[call] = true

			if !strings.Contains(command, "while IFS= read -r") {
				t.Errorf("the command that runs %q holds no `while IFS= read -r`, want the zone list read line by line (PRD #581, *The per-zone loops*): a bare `read` drops a trailing backslash, and a `for` loop splits a zone's ID on white space\n%s",
					call, command)
			}

			if !strings.Contains(command, "route53-hosted-zones.json") {
				t.Errorf("the command that runs %q reads no zone list, want the zones the `list-hosted-zones` call wrote to route53-hosted-zones.json (PRD #581, milestone 2): each zone of that list is one pass of the loop\n%s",
					call, command)
			}

			if !strings.Contains(command, outputFiles[call]) {
				t.Errorf("the command that runs %q writes no %s, want each zone's answer there: the fixture the guide's tests read holds one entry per public zone in that file (PRD #581, milestone 2)\n%s",
					call, outputFiles[call], command)
			}
		}
	}

	for call := range outputFiles {
		if !seen[call] {
			t.Fatalf("docs/guide/inventory.md's %s section runs no %q, want it once per public zone: PRD #581 reads each zone's signed state and routed record-set count there\n%s",
				guideSources[0].collect, call, describeCommands(commands))
		}
	}
}

// loopVariables are the variables the guide's loops read a resource's identity
// into, which every command has to quote. Unquoted, the shell splits a variable
// on white space and expands the glob characters in it, so a zone, project or
// resource group whose name holds one reaches the call as several arguments or as
// another name's (PRD #581, *The per-zone loops*; CLAUDE.md rule 1's read-only
// accident).
var loopVariables = []string{"id", "zone", "project", "rg"}

// TestInventoryGuideCommandsQuoteEveryLoopVariable pins that no command of any
// source holds an unquoted `$id`, `$zone`, `$project` or `$rg`, and that none uses
// `eval`: the page shows the command whole and a visitor copies it, so an unquoted
// variable is the visitor's data reaching a read-only call split in two, and an
// `eval` runs whatever a provider's answer happens to hold.
func TestInventoryGuideCommandsQuoteEveryLoopVariable(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))

	for _, source := range guideSources {
		t.Run(source.key, func(t *testing.T) {
			t.Parallel()

			commands := guideCommands(t, guide, source)
			if len(commands) == 0 {
				t.Fatalf("docs/guide/inventory.md holds no bash block under %q and %q, so the check read nothing",
					source.collect, source.build)
			}

			for _, command := range commands {
				for _, reference := range unquotedLoopVariables(command) {
					t.Errorf("%s's command holds the unquoted loop variable %s, want it inside double quotes: an unquoted variable splits on white space and expands the glob characters in it, so the call reads another resource's name or several arguments\n%s",
						source.name, reference, command)
				}

				if holdsEval(command) {
					t.Errorf("%s's command runs `eval`, want none: the guide runs the calls it writes, and `eval` runs whatever a provider's answer holds\n%s",
						source.name, command)
				}
			}
		})
	}
}

// unquotedLoopVariables returns the loop-variable references of a command that
// stand outside double quotes, `$id` and `${id}` alike, and none that is no
// variable of its own ($ids is another one). A comment names services and
// operations in prose and is no code, so it is skipped.
func unquotedLoopVariables(command string) []string {
	var found []string

	for _, line := range strings.Split(command, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		for _, name := range loopVariables {
			for _, spelling := range []string{"$" + name, "${" + name + "}"} {
				for at := 0; at < len(line); {
					index := strings.Index(line[at:], spelling)
					if index < 0 {
						break
					}

					index += at
					at = index + len(spelling)

					if spelling == "$"+name && at < len(line) && isShellNameChar(line[at]) {
						continue
					}

					if quotesBefore(line, index)%2 == 1 {
						continue
					}

					found = append(found, spelling)
				}
			}
		}
	}

	return found
}

// isShellNameChar reports whether c may continue a shell variable's name, which
// makes the reference that ends here another variable's.
func isShellNameChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// quotesBefore returns how many unescaped double quotes stand before at.
func quotesBefore(line string, at int) int {
	quotes := 0

	for i := 0; i < at; i++ {
		if line[i] != '"' {
			continue
		}

		if i > 0 && line[i-1] == '\\' {
			continue
		}

		quotes++
	}

	return quotes
}

// holdsEval reports whether a command's code words name the shell's `eval`.
func holdsEval(command string) bool {
	for _, word := range guideCallWords(command) {
		if word == "eval" {
			return true
		}
	}

	return false
}

// TestInventoryGuideUnquotedLoopVariableCheckFindsAPlantedReference pins that the
// check above can fail: it reports a variable outside double quotes, and accepts
// the quoted spellings the guide uses, the braced form included.
func TestInventoryGuideUnquotedLoopVariableCheckFindsAPlantedReference(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "an unquoted zone ID",
			command: `aws route53 get-dnssec --hosted-zone-id $id --output json`,
			want:    []string{"$id"},
		},
		{
			name:    "an unquoted braced zone name",
			command: `gcloud dns record-sets list --zone=${zone}`,
			want:    []string{"${zone}"},
		},
		{
			name:    "an unquoted project and resource group",
			command: "gcloud asset search-all-resources --scope=projects/$project\naz group show --name $rg",
			want:    []string{"$project", "$rg"},
		},
		{
			name:    "a quoted variable",
			command: `aws route53 list-resource-record-sets --hosted-zone-id "$id" --output json`,
			want:    nil,
		},
		{
			name:    "another variable whose name starts alike",
			command: `out="inventories/aws/$ideas"`,
			want:    nil,
		},
		{
			name:    "a comment naming a variable",
			command: "# --hosted-zone-id $id is quoted below\naws route53 get-dnssec --hosted-zone-id \"$id\"",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := unquotedLoopVariables(tc.command)
			if !slices.Equal(got, tc.want) {
				t.Errorf("unquotedLoopVariables(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

// route53RecordSetPermission is the IAM action of the one Route 53 call the guide
// marks optional (PRD #581, *The record-set permissions*): it returns every record
// of a zone to its holder, so a reader who leaves the dns-routing capability out
// makes no such call.
const route53RecordSetPermission = "route53:ListResourceRecordSets"

// TestInventoryGuideAWSPolicyCoversTheRoute53Calls pins the permissions PRD #581'
// milestone 2 asks for: the guide's AWS policy grants `route53:ListHostedZones`,
// which lists the zones, and `route53:GetDNSSEC`, which reads a zone's signed
// state; and the guide says in prose that `route53:ListResourceRecordSets` is
// optional and that it lets its holder read every record, so a reader knows what
// asking for it costs before adding it.
func TestInventoryGuideAWSPolicyCoversTheRoute53Calls(t *testing.T) {
	t.Parallel()

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	actions := guideAWSPolicyActions(t, guide)

	for _, action := range []string{"route53:ListHostedZones", "route53:GetDNSSEC"} {
		if !slices.Contains(actions, action) {
			t.Errorf("the guide's AWS policy does not allow %q, want it: PRD #581's zone classification makes the call that action permits, and the policy is the only permission the guide asks for",
				action)
		}
	}

	var optional []string

	for _, paragraph := range strings.Split(guide, "\n\n") {
		if !strings.Contains(paragraph, route53RecordSetPermission) {
			continue
		}

		if !strings.Contains(strings.ToLower(paragraph), "optional") {
			continue
		}

		if !strings.Contains(strings.ToLower(paragraph), "record") {
			continue
		}

		optional = append(optional, paragraph)
	}

	if len(optional) == 0 {
		t.Errorf("docs/guide/inventory.md never calls %q optional beside what it reads, want a paragraph naming it as optional and saying it lets its holder read every record: the call returns every record of a zone, so the reader decides whether the dns-routing capability is worth the permission (PRD #581, *The record-set permissions*)",
			route53RecordSetPermission)
	}
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

// gcpCloudDNSCalls are the calls PRD #581's GCP classification makes of Cloud
// DNS: the managed zones of the project, and, per public zone, whether any of
// its record sets routes by a policy. The zones themselves come from the asset
// listing the guide already runs; these two calls add the details a zone is
// classified and given its capabilities from.
const (
	gcpManagedZonesCall = "gcloud dns managed-zones list"
	gcpRecordSetsCall   = "gcloud dns record-sets list"
)

// gcpManagedZoneFields are the fields the guide's managed-zones call may keep
// (PRD #581, *How the sources classify*, the GCP row): the zone's number, which
// is the `id` the asset row names the zone by and one half of the join; the
// zone's own name, which is the ID the inventory gives a zone the join finds
// and the other half of it; the visibility that tells a public zone from a
// private one; and the DNSSEC configuration's signed state. `kind` is the
// response's own kind. Every field is read by the program; a zone's dnsName, its
// description or its name servers are not, and would keep the user's domains and
// Google's servers in the raw output the guide tells the reader to delete.
var gcpManagedZoneFields = []string{"kind", "id", "name", "visibility", "dnssecConfig", "state"}

// gcpProjectJoin matches the jq filter that adds the project to each entry of the
// managed-zones list, which writes no project of its own (PRD #581, milestone 3):
// the program joins the entries to the asset search's dns.googleapis.com/
// ManagedZone rows on the project and the zone's number, so an entry without a
// project is no entry the program can place. A jq object holding a `project` key
// and a `project` assignment both count.
var gcpProjectJoin = regexp.MustCompile(`(?:\{[^}]*\bproject\s*:|\.project\s*=)`)

// gcpRecordValueFields are the fields a Cloud DNS record set carries when the
// call reads the record itself: its values and their signatures, and the TTL
// they live for. The guide's record-set call keeps a count, never one of them.
var gcpRecordValueFields = []string{"rrdatas", "signatureRrdatas", "ttl"}

// commandCode returns a command without its comment lines: a comment may name a
// field or a permission in prose without the command reading it.
func commandCode(command string) string {
	var code []string

	for _, line := range strings.Split(command, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		code = append(code, line)
	}

	return strings.Join(code, "\n")
}

// gcpCommandsRunning returns the GCP section's commands that run one of the
// calls, failing the test when the count is not the one the milestone's calls
// have: a second call of the same shape leaves which output the program reads
// unclear.
func gcpCommandsRunning(t *testing.T, commands []string, call string, want int) []string {
	t.Helper()

	var calls []string

	for _, command := range commands {
		if strings.Contains(commandCode(command), call) {
			calls = append(calls, command)
		}
	}

	if len(calls) != want {
		t.Fatalf("docs/guide/inventory.md's GCP section runs %q in %d command(s), want exactly %d: PRD #581's GCP classification makes that call once, and this check cannot tell which output the program reads otherwise\n%s",
			call, len(calls), want, describeCommands(commands))
	}

	return calls
}

// TestInventoryGuideGCPCloudDNSCallsKeepOnlyTheFieldsTheyRead pins the GCP half
// of PRD #581's milestone 3 on the guide's command text: `gcloud dns
// managed-zones list` keeps only each zone's kind, number, name, visibility and
// signed state, with the project the guide's jq filter adds for the join, the
// number being the half of it the asset row already holds; and `gcloud
// dns record-sets list` keeps the record type of each routed record set and
// nothing else, so the command's output is a count and no record. A --format
// that named a record's values would leave the user's hosts and their addresses
// in the raw output the guide tells the reader to delete.
func TestInventoryGuideGCPCloudDNSCallsKeepOnlyTheFieldsTheyRead(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	gcp := guideSourceByKey(t, "gcp")
	commands := guideCommands(t, guide, gcp)

	managedZones := gcpCommandsRunning(t, commands, gcpManagedZonesCall, 1)[0]

	if !strings.Contains(managedZones, `--project="$project"`) {
		t.Errorf("%s's `%s` command passes no `--project=\"$project\"`, want the project quoted: the call lists one project's zones, and the guide's loop reads the project into `$project` (PRD #581, *The per-zone loops*)\n%s",
			guidePath, gcpManagedZonesCall, managedZones)
	}

	match := gcloudFormatProjection.FindStringSubmatch(managedZones)
	if match == nil {
		t.Fatalf("%s's `%s` command passes no `--format='json(...)'` projection, want one naming only %s: without it the call writes each zone's whole configuration to the file, its dnsName, its description and its name servers included\n%s",
			guidePath, gcpManagedZonesCall, strings.Join(gcpManagedZoneFields, ", "), managedZones)
	}

	keys := projectionKeys(match[1])

	for _, key := range keys {
		if !slices.Contains(gcpManagedZoneFields, key) {
			t.Errorf("%s's `%s` --format names %q, want only the fields PRD #581 reads of a zone (%s): a key the program does not read would keep a field of the user's zone in the guide's raw output\n%s",
				guidePath, gcpManagedZonesCall, key, strings.Join(gcpManagedZoneFields, ", "), managedZones)
		}
	}

	// A projection that named none of the fields the program reads would pass the
	// check above and keep nothing to classify a zone from, so they are required by
	// name: the zone's number and its name, the two halves of the join, and the
	// visibility and signed state that decide its category and capabilities.
	for _, required := range []struct {
		key string
		why string
	}{
		{
			"id",
			"the asset row names a zone by the number Google assigned it, never by its name, so the list's `id` is the only half of the join the asset side already holds: without it no zone joins and every one stays unclassified (review finding S2)",
		},
		{
			"name",
			"the program writes a joined zone's resource ID from the zone's name, and the record-set call takes that name",
		},
		{
			"visibility",
			"it tells a public zone from a private one, which decides whether the zone is network.dns at all",
		},
	} {
		if !slices.Contains(keys, required.key) {
			t.Errorf("%s's `%s` --format names no %q, want it: %s\n%s",
				guidePath, gcpManagedZonesCall, required.key, required.why, managedZones)
		}
	}

	if !slices.Contains(keys, "dnssecConfig") && !slices.Contains(keys, "state") {
		t.Errorf("%s's `%s` --format names no `dnssecConfig` and no `state`, want one: dnssecConfig.state is the only field that says whether Google signs the zone, and without it no zone carries dnssec (PRD #581, milestone 3)\n%s",
			guidePath, gcpManagedZonesCall, managedZones)
	}

	if !gcpProjectJoin.MatchString(commandCode(managedZones)) {
		t.Errorf("%s's `%s` command adds no project to each entry, want a jq filter writing a `project` key or assigning `.project`: the list output carries no project, and the program joins its entries to the asset rows on the project and the zone's number (PRD #581, milestone 3)\n%s",
			guidePath, gcpManagedZonesCall, managedZones)
	}

	recordSets := gcpCommandsRunning(t, commands, gcpRecordSetsCall, 1)[0]
	recordSetsCode := commandCode(recordSets)

	for _, argument := range []struct {
		name  string
		value string
		want  string
	}{
		{"project", `--project="$project"`, "the project quoted, which the guide reads into `$project`"},
		{"zone", `--zone="$zone"`, "the zone's name quoted, which the loop reads out of the managed-zones file for each public zone"},
	} {
		if !strings.Contains(recordSetsCode, argument.value) {
			t.Errorf("%s's `%s` command passes no `%s`, want %s (PRD #581, *The per-zone loops*)\n%s",
				guidePath, gcpRecordSetsCall, argument.value, argument.want, recordSets)
		}
	}

	if !strings.Contains(recordSetsCode, `--filter='routingPolicy:*'`) {
		t.Errorf("%s's `%s` command passes no `--filter='routingPolicy:*'`, want it: only a record set that carries a routing policy routes by client or by health, which is what dns-routing says of the zone (PRD #581, milestone 3)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	if !strings.Contains(recordSetsCode, `--format='value(type)'`) {
		t.Errorf("%s's `%s` command passes no `--format='value(type)'`, want it: the call writes one record type per routed record set and nothing else, and the count of those lines is all the program reads (PRD #581, *The guide reads no zone contents*)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	for _, field := range gcpRecordValueFields {
		if strings.Contains(recordSetsCode, field) {
			t.Errorf("%s's `%s` command names %q, want no part of a record's values: the call keeps a count of routed record sets, never a record's contents (PRD #581)\n%s",
				guidePath, gcpRecordSetsCall, field, recordSets)
		}
	}
}

// gcpZoneLoop matches the loop the guide runs over its managed zones, one line
// at a time: `while read -r <variable>...` or `while IFS=<separator> read -r
// <variable>...`. The line holds each zone's number and its name with a space
// between them; the variables are what the loop body quotes.
var gcpZoneLoop = regexp.MustCompile(`(?m)while\s+((?:IFS=('[^']*'|"[^"]*"|\S*)\s+)?)read\s+-r\s+([A-Za-z_][A-Za-z0-9_]*(?:\s+[A-Za-z_][A-Za-z0-9_]*)*)`)

// gcpBashDollarQuote matches a `$'...'` word, which is bash's own quoting and no
// POSIX shell's: the guide says its commands run under sh, dash and zsh too.
var gcpBashDollarQuote = regexp.MustCompile(`\$'`)

// gcpRoutedEntryID matches the `id` key of the object the record-set loop writes
// per zone, with its value or with a `--arg` name: the count is joined to the
// zone's asset row on that number, so an entry keyed by the zone's name alone
// joins nothing.
var gcpRoutedEntryID = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])id\s*:`)

// gcpMachineTypesVariable matches the shell variable the guide's machine-type
// call reads the instance types into, outside a longer name: `types`, `$types`
// and `types=` are that variable, `routed_types` is another one.
var gcpMachineTypesVariable = regexp.MustCompile(`[^A-Za-z0-9_]types(?:[^A-Za-z0-9_]|$)`)

// TestInventoryGuideGCPCloudDNSLoopReadsTheZoneList pins the loop PRD #581's
// milestone 3 runs (PRD #581, *The per-zone loops*): the record-set call runs
// once per public zone of the project, over the list the managed-zones call
// wrote, read line by line with `while read -r` or `while IFS=' ' read -r`. A
// loop read with a bare `read` drops a trailing backslash, and one fed by `for`
// splits a zone's name on white space, so the call would ask about a zone that
// does not exist and the zone would lose dns-routing. No command holds an
// unquoted loop variable, and none holds a `$'...'` word: that is a bashism, and
// under dash `IFS=$'\t'` is the three characters `$`, `\` and `t`, so a zone
// named `test-zone` is cut at its own `t` (review finding, round 2).
//
// The line the loop reads holds the zone's number and its name: the asset row
// names a zone by the number, the record-set call takes the name, and the count
// is written under the number, so a loop that read the name alone or gave the
// call the number asks about a zone no call knows (review finding S2). The
// loop's temporary file is not named `types`, which is the machine-type call's
// variable earlier in the GCP collect step (auditor finding).
func TestInventoryGuideGCPCloudDNSLoopReadsTheZoneList(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	gcp := guideSourceByKey(t, "gcp")
	commands := guideCommands(t, guide, gcp)

	recordSets := gcpCommandsRunning(t, commands, gcpRecordSetsCall, 1)[0]
	code := commandCode(recordSets)

	// The guide promises a POSIX shell (sh, dash, bash, zsh), and `$'...'` is
	// bash's quoting alone: under dash it is the three characters `$`, `\` and
	// `t`, so a line `1234567890123456789<tab>shop-zone` reads as one field and
	// an empty one, and Google is asked about a zone named none (review finding,
	// round 2).
	if gcpBashDollarQuote.MatchString(code) {
		t.Errorf("%s's command that runs `%s` holds a `$'...'` word, want none: the guide's commands run under sh and dash too, where `$'` quotes nothing and `$'\\t'` is the three characters `$`, `\\` and `t`, so a loop separated by it reads a zone's number and name wrong and the call asks about a zone that does not exist\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	if loop := gcpZoneLoop.FindStringSubmatch(code); loop == nil {
		t.Errorf("%s's command that runs `%s` holds no `while read -r ...` and no `while IFS=... read -r ...`, want the zone list read line by line (PRD #581, *The per-zone loops*): a bare `read` drops a trailing backslash, and a `for` loop splits a zone's name on white space\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	} else {
		// The line the list holds is the zone's number, a space and its name, so
		// the loop splits on the default IFS, which takes white space, or on the
		// one space it names itself. Any other separator is a character no line
		// holds, and the name stays empty or joined to the number.
		if separator := strings.Trim(loop[2], `'"`); loop[1] != "" && separator != " " {
			t.Errorf("%s's command that runs `%s` sets IFS to %q, want `IFS=' '` or no IFS at all: the managed-zones file holds each zone's number and its name separated by a space, and a separator the line does not hold leaves the name empty or the number and the name joined (PRD #581, milestone 3)\n%s",
				guidePath, gcpRecordSetsCall, loop[2], recordSets)
		}

		fields := strings.Fields(loop[3])

		switch {
		case len(fields) != 2:
			t.Errorf("%s's command that runs `%s` reads %d field(s) per line (`read -r %s`), want two: the managed-zones file holds each zone's number and its name, the call takes the name and the count is joined to the asset row on the number, so a loop that read one of them alone asks about a zone no call knows or writes a count nothing joins (PRD #581, milestone 3)\n%s",
				guidePath, gcpRecordSetsCall, len(fields), loop[3], recordSets)
		default:
			number, name := fields[0], fields[1]

			if !strings.Contains(code, `--zone="$`+name+`"`) {
				t.Errorf("%s's command that runs `%s` passes no `--zone=\"$%s\"`, want the name the loop reads second: the call takes the zone's name, and the number is no zone name\n%s",
					guidePath, gcpRecordSetsCall, name, recordSets)
			}

			if !strings.Contains(code, `"$`+number+`"`) {
				t.Errorf("%s's command that runs `%s` holds no quoted `\"$%s\"`, want the number the loop reads first, written under that name for the join: an unquoted or unused number gives the count to no zone (PRD #581, milestone 3)\n%s",
					guidePath, gcpRecordSetsCall, number, recordSets)
			}
		}
	}

	// The two fields have to come from the zone list: the loop's selection reads
	// the number and the name the list wrote, not the name twice.
	for _, field := range []string{".id", ".name"} {
		if !strings.Contains(code, field) {
			t.Errorf("%s's command that runs `%s` reads no %s from the zone list, want each public zone's number and its name: the join needs the number the asset row names the zone by, and the call needs the name\n%s",
				guidePath, gcpRecordSetsCall, field, recordSets)
		}
	}

	if !strings.Contains(code, "routedRecordSets") {
		t.Errorf("%s's command that runs `%s` writes no routedRecordSets, want the count of each public zone's routed record sets under it: the program reads that key per zone (PRD #581, milestone 3)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	if !gcpRoutedEntryID.MatchString(code) {
		t.Errorf("%s's command that runs `%s` keys no entry by `id`, want the zone's number beside `routedRecordSets`: the program joins the count to the zone's asset row on the project and the number, so a count under the zone's name alone joins nothing (PRD #581, milestone 3)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	if gcpMachineTypesVariable.MatchString(code) {
		t.Errorf("%s's command that runs `%s` holds the shell variable `types`, want another name: the machine-type call earlier in the GCP collect step reads the instance types in use into `types`, so a second `types` here is another value under the same name (auditor finding)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	if !strings.Contains(code, "dns-managed-zones.json") {
		t.Errorf("%s's command that runs `%s` reads no zone list, want the zones `%s` wrote to dns-managed-zones.json (PRD #581, milestone 3): each public zone of that list is one pass of the loop\n%s",
			guidePath, gcpRecordSetsCall, gcpManagedZonesCall, recordSets)
	}

	if !strings.Contains(code, "dns-routed-record-sets.json") {
		t.Errorf("%s's command that runs `%s` writes no dns-routed-record-sets.json, want the count of each public zone's routed record sets there: the fixture the guide's tests read holds one entry per public zone in that file (PRD #581, milestone 3)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	if !strings.Contains(code, "length") && !strings.Contains(code, "wc -l") {
		t.Errorf("%s's command that runs `%s` reduces the call to no count, want one number per zone (jq's `length` over the record types the call wrote, or a line count of them): the program reads a count, and a command that wrote the record types through would put the zone's contents in the file (PRD #581)\n%s",
			guidePath, gcpRecordSetsCall, recordSets)
	}

	// The page shows each command whole and a visitor copies it, so a variable
	// outside double quotes is the visitor's own project or zone reaching a
	// read-only call split in two (PRD #581, *The per-zone loops*).
	for _, command := range commands {
		for _, reference := range unquotedLoopVariables(command) {
			t.Errorf("%s's command holds the unquoted loop variable %s, want it inside double quotes: an unquoted variable splits on white space and expands the glob characters in it, so the call reads another project's or zone's name, or several arguments\n%s",
				guidePath, reference, command)
		}

		if holdsEval(command) {
			t.Errorf("%s's command runs `eval`, want none: the guide runs the calls it writes, and `eval` runs whatever a provider's answer holds\n%s",
				guidePath, command)
		}
	}
}

// gcpRecordSetPermission is the Cloud DNS permission that returns every record
// of a zone to its holder, which `roles/dns.reader` includes (PRD #581, *The
// record-set permissions*). The guide offers a custom role without it for a
// reader who leaves dns-routing out.
const gcpRecordSetPermission = "dns.resourceRecordSets.list"

// TestInventoryGuideGCPRolesCoverTheCloudDNSCalls pins the GCP permissions
// PRD #581's milestone 3 asks for: `roles/dns.reader`, which lists the managed
// zones and reads their configuration, is in the guide's role list; the guide
// says in prose that the record-set call is optional, that it needs
// `dns.resourceRecordSets.list` and that the permission lets its holder read
// every record, and what else the role reads beside it (each zone's change
// history and the project's response policy rules, auditor finding 1); and it
// names a custom role holding `dns.managedZones.list` alone for a reader who
// does not want dns-routing.
func TestInventoryGuideGCPRolesCoverTheCloudDNSCalls(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	roles := gcpRoleBullets(t, guide)

	if !slices.Contains(roles, "roles/dns.reader") {
		t.Errorf("%s's GCP permissions list names %v, want `roles/dns.reader` among them: the guide's `%s` and `%s` calls read Cloud DNS configuration with that role, and a reader on the list without it gets no answer from either and loses every zone's capabilities\n%s",
			guidePath, roles, gcpManagedZonesCall, gcpRecordSetsCall, guideRoleParagraphsOf(guide, "roles/dns.reader"))
	}

	for _, name := range []string{gcpRecordSetPermission, "dns.managedZones.list"} {
		if !strings.Contains(guide, name) {
			t.Errorf("%s names no %q, want it beside the Cloud DNS roles: the record-set step needs %q, which lets its holder read every record, and a reader who leaves dns-routing out needs a custom role with `dns.managedZones.list` alone (PRD #581, *The record-set permissions*)\n%s",
				guidePath, name, gcpRecordSetPermission, guideRoleParagraphsOf(guide, "roles/dns.reader"))
		}
	}

	// The permission has to be called optional beside what it reads, or a reader
	// adds it without knowing what asking for it costs.
	var optional []string

	for _, paragraph := range strings.Split(guide, "\n\n") {
		if !strings.Contains(paragraph, gcpRecordSetPermission) {
			continue
		}

		if !strings.Contains(strings.ToLower(paragraph), "optional") {
			continue
		}

		if !strings.Contains(strings.ToLower(paragraph), "every record") {
			continue
		}

		optional = append(optional, paragraph)
	}

	if len(optional) == 0 {
		t.Errorf("%s never calls %q optional beside what it reads, want a paragraph naming it as optional and saying that it lets its holder read every record: the permission returns every record of a zone to the caller, so the reader decides whether the dns-routing capability is worth it (PRD #581, *The record-set permissions*)",
			guidePath, gcpRecordSetPermission)
	}

	// roles/dns.reader reads far more of Cloud DNS than the zones' visibility and
	// signed state (auditor finding 1): beside every record set of every zone, it
	// also reads each zone's change history (dns.changes.list) and the project's
	// response policy rules. The guide says so where it names the role, so a reader
	// who grants it knows what it exposes.
	reader := strings.ToLower(guideRoleParagraphsOf(guide, "roles/dns.reader"))

	for _, want := range []struct {
		name string
		text string
	}{
		{"the change history permission `dns.changes.list`", "dns.changes.list"},
		{"the response policy rules it reads", "response polic"},
	} {
		if !strings.Contains(reader, want.text) {
			t.Errorf("%s names `roles/dns.reader` without %s, want it beside the record-set permission: the role reads every record set of every zone, each zone's change history and the project's response policy rules, and a reader decides whether to grant it knowing what it exposes (PRD #581, *The record-set permissions*, auditor finding 1)\n%s",
				guidePath, want.name, guideRoleParagraphsOf(guide, "roles/dns.reader"))
		}
	}
}

// guideRoleParagraphsOf returns the paragraphs of the guide that name a string,
// for a failure message.
func guideRoleParagraphsOf(guide, name string) string {
	var paragraphs []string

	for _, paragraph := range strings.Split(guide, "\n\n") {
		if strings.Contains(paragraph, name) {
			paragraphs = append(paragraphs, paragraph)
		}
	}

	return strings.Join(paragraphs, "\n\n")
}

// azureDNSZonesType is the Resource Manager type of a public DNS zone, in the
// casing the guide's KQL compares a type in (Resource Graph writes types in lower
// case). PRD #581's Azure row classifies the zones of this type, and the
// privateDnsZones ones it does not name stay unclassified.
const azureDNSZonesType = "microsoft.network/dnszones"

// azureZoneTypeKey is the one property key the Resource Graph query keeps of a
// DNS zone (PRD #581, *How the sources classify*, the Azure row): the zone type
// that tells a public zone from a legacy private one. The zone's signing keys and
// name servers are never projected.
const azureZoneTypeKey = "zoneType"

// azureDNSSECCall is the call PRD #581's Azure classification makes of Azure DNS,
// once per public zone: the zone's DNSSEC configuration, read for its signed
// state alone. The zones and their type come from the Resource Graph query the
// guide already runs; this call adds what the graph does not hold.
const azureDNSSECCall = "az network dns dnssec-config show"

// azureDNSSECFile is the file the DNSSEC loop writes: one configuration per signed
// public zone, and nothing for a zone Azure does not sign.
const azureDNSSECFile = "dns-dnssec.json"

// azureDNSSECQuery matches the `--query` argument of the guide's DNSSEC call,
// quoted either way and written as one word or two: the projection Azure applies
// before the output reaches the file.
var azureDNSSECQuery = regexp.MustCompile(`--query(?:\s+|=)["']([^"']*)["']`)

// azureDNSSECSeparateFlag matches one of the DNSSEC call's two loop-variable
// flags written with its value as a separate word, `--resource-group "$rg"`: the
// form in which the CLI reads a value that starts with `-` as an option of its
// own, never as the flag's value.
var azureDNSSECSeparateFlag = regexp.MustCompile(`--(?:resource-group|zone-name)\s`)

// azureDNSSECQueryFields are the fields the guide's `--query` keeps of a zone's
// DNSSEC configuration (PRD #581, *How the sources classify*): the configuration's
// ID, which the program joins to its zone case-insensitively, its type, and the
// provisioning state that says whether the zone is signed.
var azureDNSSECQueryFields = []string{"id", "type", "provisioningState"}

// azureDNSSECExcludedFields are the fields a DNS zone's DNSSEC configuration
// carries and the guide keeps nowhere (PRD #581): the zone's signing keys with
// their delegation signer records, and Azure's own name servers, which the
// Resource Graph projection drops.
var azureDNSSECExcludedFields = []string{"signingKeys", "publicKey", "delegationSignerInfo", "nameServers"}

// azureZoneArm returns the part of the guide's Resource Graph query that reads a
// DNS zone: the text from the type that names the zone type to the end of that
// arm, which the next `type ...` arm or the case's `dynamic(null)` ends. It
// reports false when the query names no DNS zone type at all, since without an
// arm the zone's properties stay `null` and every zone is unclassified.
func azureZoneArm(query string) (string, bool) {
	start := strings.Index(strings.ToLower(query), azureDNSZonesType)
	if start < 0 {
		return "", false
	}

	end := len(query)
	offset := 0

	for _, line := range strings.Split(query, "\n") {
		if offset > start {
			trimmed := strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(trimmed, "type ") || strings.HasPrefix(trimmed, "dynamic(") {
				end = offset

				break
			}
		}

		offset += len(line) + 1
	}

	return query[start:end], true
}

// TestInventoryGuideAzureDNSZonesKeepOnlyTheZoneType pins the Azure half of
// PRD #581's *How the sources classify* on the guide's own Resource Graph query:
// the query has an arm for `microsoft.network/dnszones` that keeps the zone's
// `properties.zoneType` and nothing else, so a public zone reaches the program
// with the one property that tells it from a legacy private one, and no zone
// reaches it with its signing keys or its name servers. The recorded fixture
// cannot pin this on its own, since a fixture written after the fact could hold a
// field a query without the arm never writes.
func TestInventoryGuideAzureDNSZonesKeepOnlyTheZoneType(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	query := azureResourceGraphQuery(t, guide)

	arm, ok := azureZoneArm(query)
	if !ok {
		t.Fatalf("%s's Resource Graph query names no %s, want the arm that keeps a DNS zone's zone type: without it a zone's properties stay null, and every public zone is unclassified (PRD #581, milestone 4)\n%s",
			guidePath, azureDNSZonesType, query)
	}

	var paths []string

	for _, loc := range azureFieldPath.FindAllStringIndex(arm, -1) {
		paths = append(paths, withoutSpace(arm[loc[0]:loc[1]]))
	}

	if !slices.ContainsFunc(paths, func(path string) bool {
		return strings.EqualFold(path, "properties."+azureZoneTypeKey)
	}) {
		t.Errorf("%s's Resource Graph query keeps no properties.%s for a DNS zone, want it: it is the field that says whether the zone is public, and `Public` is the API's default when it is missing (PRD #581, milestone 4)\n%s",
			guidePath, azureZoneTypeKey, query)
	}

	if diff := cmp.Diff([]string{azureZoneTypeKey}, azureBagPackKeys(arm)); diff != "" {
		t.Errorf("%s's Resource Graph query packs the wrong keys of a DNS zone (-want +got):\n%s\nwant only %q: the zone's signing keys and name servers are properties azure.jq reads nothing of, so the arm that keeps one of them would write the user's signing material to the file (PRD #581)\n%s",
			guidePath, diff, azureZoneTypeKey, query)
	}

	lowered := strings.ToLower(query)

	for _, field := range azureDNSSECExcludedFields {
		if strings.Contains(lowered, strings.ToLower(field)) {
			t.Errorf("%s's Resource Graph query names %q, want no field of a zone's keys or its name servers: none is read by azure.jq, and the zone resource holds its signing keys (PRD #581, milestone 4)\n%s",
				guidePath, field, query)
		}
	}
}

// TestInventoryGuideAzureDNSSECCommandsKeepOnlyTheFieldsTheyRead pins PRD #581's
// milestone 4 on the guide's Azure command text: `az network dns dnssec-config
// show` runs once per public zone the Resource Graph pages hold, takes the zone's
// resource group and name quoted and attached to their flags, and keeps of the
// configuration only its ID, its type and its provisioning state. A `--query` that
// named the signing keys would leave the zone's signing material in the raw output
// the guide tells the reader to delete; a flag whose value is a word of its own
// would let a resource group name that starts with `-` reach the CLI as an option.
func TestInventoryGuideAzureDNSSECCommandsKeepOnlyTheFieldsTheyRead(t *testing.T) {
	t.Parallel()

	const guidePath = "docs/guide/inventory.md"

	guide := string(readRepositoryFile(t, filepath.Join("docs", "guide", "inventory.md")))
	azure := guideSourceByKey(t, "azure")
	commands := guideCommands(t, guide, azure)

	var calls []string

	for _, command := range commands {
		if strings.Contains(commandCode(command), azureDNSSECCall) {
			calls = append(calls, command)
		}
	}

	if len(calls) != 1 {
		t.Fatalf("%s's %s section runs %q in %d command(s), want exactly one: PRD #581's Azure classification makes that call once per public zone, in one per-zone loop, and this check cannot tell which output the program reads otherwise\n%s",
			guidePath, azure.name, azureDNSSECCall, len(calls), describeCommands(commands))
	}

	command := calls[0]
	code := commandCode(command)

	for _, want := range []struct {
		name string
		why  string
	}{
		{"graph-", "the Resource Graph pages the query wrote, which hold every zone with its resource group and its name"},
		{azureDNSZonesType, "the type whose zones are DNS zones: a privateDnsZones zone is never signed, so the loop may not ask about one"},
		{azureZoneTypeKey, "the zone type the query projects, which tells a public zone from a legacy private one"},
		{"while", "the per-zone loop PRD #581's *The per-zone loops* describes"},
		{"read -r", "the zone list read line by line, which a bare `read` would cut a name at its backslash"},
		{azureDNSSECFile, "the file the loop writes the configurations to"},
		{"--resource-group", "the zone's resource group, which the call needs beside the zone's name"},
		{"--zone-name", "the zone's name, which the call takes"},
		{`"$rg"`, "the resource group quoted, which the page shows whole and a visitor copies"},
		{`"$zone"`, "the zone's name quoted, which the page shows whole and a visitor copies"},
	} {
		if !strings.Contains(code, want.name) {
			t.Errorf("%s's `%s` command holds no %q, want %s (PRD #581, milestone 4)\n%s",
				guidePath, azureDNSSECCall, want.name, want.why, command)
		}
	}

	// A resource group's name may start with `-` (audit finding on PRD #581's
	// milestone 4). With the flag and its value as two words the CLI parses
	// `-rg-legacy` as an option, not as the flag's value, so the call fails; the
	// loop's `|| :` hides that, and a signed zone loses dnssec. The attached form
	// leaves the CLI no word of the value to parse as an option.
	for _, want := range []string{`--resource-group="$rg"`, `--zone-name="$zone"`} {
		if !strings.Contains(code, want) {
			t.Errorf("%s's `%s` command passes no %s, want the flag and its value attached: a resource group name may start with `-`, which the CLI reads as an option when the flag and its value are two words, so the call fails, the loop's `|| :` hides it, and a signed zone loses dnssec (audit finding, PRD #581, milestone 4)\n%s",
				guidePath, azureDNSSECCall, want, command)
		}
	}

	if separate := azureDNSSECSeparateFlag.FindAllString(code, -1); len(separate) > 0 {
		for i, flag := range separate {
			separate[i] = strings.TrimSpace(flag)
		}

		t.Errorf("%s's `%s` command passes %s with its value as a separate word, want the attached form: a resource group name may start with `-`, which the CLI then reads as an option, so the call fails, the loop's `|| :` hides it, and a signed zone loses dnssec (audit finding, PRD #581, milestone 4)\n%s",
			guidePath, azureDNSSECCall, strings.Join(separate, ", "), command)
	}

	if !strings.Contains(code, "--output json") && !strings.Contains(code, "--output=json") {
		t.Errorf("%s's `%s` command passes no `--output json`, want the JSON output the program reads the provisioning state from (PRD #581, milestone 4)\n%s",
			guidePath, azureDNSSECCall, command)
	}

	match := azureDNSSECQuery.FindStringSubmatch(code)
	if match == nil {
		t.Fatalf("%s's `%s` command passes no `--query`, want one naming only %s: without it the call writes each zone's signing keys to the file (PRD #581, milestone 4)\n%s",
			guidePath, azureDNSSECCall, strings.Join(azureDNSSECQueryFields, ", "), command)
	}

	query := match[1]

	for _, field := range azureDNSSECQueryFields {
		if !strings.Contains(query, field) {
			t.Errorf("%s's `%s` --query keeps no %q, want it: the program joins the configuration to its zone by the ID, and reads the provisioning state to decide dnssec\n%s",
				guidePath, azureDNSSECCall, field, command)
		}
	}

	for _, field := range azureDNSSECExcludedFields {
		if strings.Contains(query, field) {
			t.Errorf("%s's `%s` --query keeps %q, want no field of a zone's keys or its name servers: the configuration's signed state is all the program reads, and the keys are the zone's signing material (PRD #581)\n%s",
				guidePath, azureDNSSECCall, field, command)
		}
	}

	// The page shows each command whole and a visitor copies it, so a variable
	// outside double quotes is the visitor's own resource group or zone reaching a
	// read-only call split in two (PRD #581, *The per-zone loops*).
	for _, azureCommand := range commands {
		for _, reference := range unquotedLoopVariables(azureCommand) {
			t.Errorf("%s's command holds the unquoted loop variable %s, want it inside double quotes: an unquoted variable splits on white space and expands the glob characters in it, so the call reads another zone's name, or several arguments\n%s",
				guidePath, reference, azureCommand)
		}

		if holdsEval(azureCommand) {
			t.Errorf("%s's command runs `eval`, want none: the guide runs the calls it writes, and `eval` runs whatever a provider's answer holds\n%s",
				guidePath, azureCommand)
		}
	}
}
