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

// awsExcludedKeysPath is the one list of the fields no projection or fixture of
// the guide's search and messaging calls may name (PRD #571, *Only the fields the
// program reads*). test/examples' fixture check reads the same file, so the two
// halves of the promise, the guide's --query and the recorded output, cannot
// drift apart (review finding on PR #639).
const awsExcludedKeysPath = "test/examples/testdata/inventory_guide_excluded_keys.txt"

// awsExcludedFields are the fields PRD #571 excludes from every projection of
// the search and messaging calls: an access policy carries the account's
// principals, a security option its settings, an endpoint or a bootstrap address
// a host with the account's name in it.
type awsExcludedFields struct {
	// names are the fields a projection may not name.
	names []string
	// prefixes are the fields a projection may not name whatever their suffix: a
	// cluster's ZooKeeper connection strings and its broker addresses.
	prefixes []string
}

// readAWSExcludedFields reads the shared list of the fields every projection and
// fixture of the guide's search and messaging calls leaves out (PRD #571), which
// test/examples' fixture check reads too. A record is `key: <field>` or
// `prefix: <prefix>`; comments and blank lines are no record. This package cannot
// import the other's reader, so it has one of its own, as the two readers of
// test/examples/testdata/namespace_may_follow_delimiters.txt do (issue #630); the
// two read this file the same way, record by record, so a record either accepts
// or rejects in both. A malformed record, a record line that carries a comment
// (which would become part of the field's name) or a record repeated fails the
// test rather than shrinking the list or hiding its first copy.
func readAWSExcludedFields(t *testing.T) awsExcludedFields {
	t.Helper()

	var excluded awsExcludedFields

	// seen maps a record to the line that wrote it, so a repeat names both lines.
	seen := map[string]int{}

	for number, line := range strings.Split(string(readRepositoryFile(t, awsExcludedKeysPath)), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.Contains(trimmed, "#") {
			t.Fatalf("%s line %d reads %q, a record with a comment on it: write the comment on a line of its own, or the comment is part of the field's name and no field matches it",
				awsExcludedKeysPath, number+1, trimmed)
		}

		kind, name, ok := strings.Cut(trimmed, ": ")
		if !ok || name == "" {
			t.Fatalf("%s line %d reads %q, want `key: <field>` or `prefix: <prefix>`",
				awsExcludedKeysPath, number+1, trimmed)
		}

		if first, repeated := seen[trimmed]; repeated {
			t.Fatalf("%s line %d repeats line %d's record %q, want one record per field: a second copy hides the first and makes the reader wonder which one the two checks read",
				awsExcludedKeysPath, number+1, first, trimmed)
		}

		seen[trimmed] = number + 1

		switch kind {
		case "key":
			excluded.names = append(excluded.names, name)
		case "prefix":
			excluded.prefixes = append(excluded.prefixes, name)
		default:
			t.Fatalf("%s line %d reads %q, a record of kind %q: want `key: <field>` or `prefix: <prefix>`",
				awsExcludedKeysPath, number+1, trimmed, kind)
		}
	}

	if len(excluded.names) == 0 || len(excluded.prefixes) == 0 {
		t.Fatalf("%s holds %d key(s) and %d prefix(es), want both: the list is the whole of what both checks exclude, so an empty half would let a field the PRD excludes through",
			awsExcludedKeysPath, len(excluded.names), len(excluded.prefixes))
	}

	return excluded
}

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
	excluded := readAWSExcludedFields(t)

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
	// long names (`list_amazonelasticcontainerserviceforkubernetes.html` and its
	// like) either redirect to the index or, for EC2, EKS, MSK, RDS, S3, SQS and
	// SNS, answered 302 to it, so a citation of one led the reader nowhere
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
