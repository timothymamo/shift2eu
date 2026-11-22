package htmlreport_test

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins the layout of the fragment the site's assessment page shows
// (issue #373, the maintainer's feedback): one header line naming the inventory
// file, the source, the catalog's retrieval date as a link and the region, a
// summary of one box per fit and a monthly-estimate box, a resources table in a
// box that scrolls sideways, required changes and blockers side by side, one box
// per workload, and a one-line cost estimate.
//
// The checks read the fragment's markup, so they say what the layout is for
// rather than which bytes it has: an element is found by its tag and its class,
// and its text is read as a browser reads it.

// fragElement is one element of a fragment: its tag, the attributes of its start
// tag, the markup it holds, the element it sits in, and its own runs of text.
type fragElement struct {
	tag     string
	attrs   string
	content string
	// parent is the index of the element this one sits in, -1 for the top.
	parent int
	// children are the indices of the elements directly inside this one.
	children []int
	// texts are the element's own runs of text, each with the child it comes
	// before: they are what lets a check read what a reader sees and what only a
	// screen reader reads, as a browser tells them apart.
	texts []fragTextRun
}

// fragTextRun is one run of an element's own text, and the child it comes
// before: at is the index in the element's children of the child the run
// precedes, and the number of children for the run after the last one.
type fragTextRun struct {
	at   int
	text string
}

// addText appends a run of the element's own text: a run that directly follows
// the previous one joins it, so a text interrupted by no child stays one run.
func (e *fragElement) addText(text string) {
	if n := len(e.texts); n > 0 && e.texts[n-1].at == len(e.children) {
		e.texts[n-1].text += text

		return
	}

	e.texts = append(e.texts, fragTextRun{at: len(e.children), text: text})
}

// voidElement reports whether the element has no content and no end tag.
func voidElement(tag string) bool {
	switch tag {
	case "br", "col", "hr", "img", "input", "link", "meta", "source", "track", "wbr":
		return true
	}

	return false
}

// fragmentElements reads the elements of a fragment, in the order they open,
// failing the test on an element the fragment leaves open or closes twice: the
// reporter writes well-formed markup, and a check that guessed would read a
// different document from the browser's.
func fragmentElements(t *testing.T, fragment string) []fragElement {
	t.Helper()

	tag := regexp.MustCompile(`(?s)<(/?)([a-zA-Z][a-zA-Z0-9]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)

	elements := make([]fragElement, 0, 256)

	var (
		stack  []int
		starts []int
		// prev is the byte the text before the current match starts at.
		prev int
	)

	for _, match := range tag.FindAllStringSubmatchIndex(fragment, -1) {
		if len(stack) > 0 && match[0] > prev {
			elements[stack[len(stack)-1]].addText(fragment[prev:match[0]])
		}

		prev = match[1]

		closing := match[3] > match[2]

		name := strings.ToLower(fragment[match[4]:match[5]])
		attrs := strings.TrimSpace(fragment[match[6]:match[7]])

		if closing {
			if len(stack) == 0 {
				t.Fatalf("the fragment closes </%s> at byte %d, which it never opens", name, match[0])
			}

			last := len(stack) - 1
			open := stack[last]

			if elements[open].tag != name {
				t.Fatalf("the fragment closes <%s> with </%s> at byte %d", elements[open].tag, name, match[0])
			}

			elements[open].content = fragment[starts[last]:match[0]]
			stack = stack[:last]
			starts = starts[:last]

			continue
		}

		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}

		element := fragElement{tag: name, attrs: attrs, parent: parent}

		elements = append(elements, element)
		index := len(elements) - 1

		if parent >= 0 {
			elements[parent].children = append(elements[parent].children, index)
		}

		if voidElement(name) || strings.HasSuffix(attrs, "/") {
			continue
		}

		stack = append(stack, index)
		starts = append(starts, match[1])
	}

	if len(stack) != 0 {
		t.Fatalf("the fragment leaves %d elements open, want each one closed: <%s> at the top",
			len(stack), elements[stack[len(stack)-1]].tag)
	}

	return elements
}

// classOf returns the class names of an element, without the class attribute's
// own name.
func classOf(t *testing.T, elements []fragElement, index int) []string {
	t.Helper()

	value, ok := attributeOf(elements[index].attrs, "class")
	if !ok {
		return nil
	}

	return strings.Fields(value)
}

// attributeOf returns the value of the attribute name of a start tag's
// attributes, as the fragment spells it, and whether the tag states it.
func attributeOf(attrs, name string) (string, bool) {
	pattern := regexp.MustCompile(`(?s)(?:^|\s)` + regexp.QuoteMeta(name) + `="([^"]*)"`)

	match := pattern.FindStringSubmatch(" " + attrs)
	if match == nil {
		return "", false
	}

	return match[1], true
}

// elementsWithClass returns the indices of the elements whose class attribute
// holds every name of names.
func elementsWithClass(t *testing.T, elements []fragElement, names ...string) []int {
	t.Helper()

	var out []int

	for i := range elements {
		classes := classOf(t, elements, i)
		if len(classes) == 0 {
			continue
		}

		has := true

		for _, name := range names {
			if !slices.Contains(classes, name) {
				has = false
			}
		}

		if has {
			out = append(out, i)
		}
	}

	return out
}

// textOfElement returns the text content of an element: its markup's tags
// removed and its white space collapsed, as textOf reads a fragment.
func textOfElement(elements []fragElement, index int) string {
	return textOf(elements[index].content)
}

// descendants returns the indices of the elements inside index, in document
// order, index itself excluded.
func descendants(elements []fragElement, index int) []int {
	var out []int

	var walk func(int)

	walk = func(i int) {
		for _, child := range elements[i].children {
			out = append(out, child)
			walk(child)
		}
	}

	walk(index)

	return out
}

// fitsAssessment is the fragment's fixture with every fit and no estimates: the
// Scaleway fixture catalog serves a direct, a partial and two unserved
// categories.
func fitsAssessment(tb testing.TB) assessment.Assessment {
	tb.Helper()

	return assessment.Assess(newInventory(tb, sourceName, awsResources()), testutil.ScalewayCatalog(tb))
}

// estimatesAssessment is the fragment's fixture with estimates on: the report
// catalog prices the sized inventory, so the summary and the cost estimate have
// an amount to show.
func estimatesAssessment(tb testing.TB) assessment.Assessment {
	tb.Helper()

	return assessment.AssessWith(newInventory(tb, sourceName, estimateResources()),
		testutil.ReportCatalog(tb), assessment.Options{Estimate: true})
}

// TestReportHeaderNamesTheSourceTheFileTheCatalogAndTheRegion pins the one line
// above the summary (issue #373, item 10-1): the source, the inventory file, the
// catalog's retrieval date and the region, separated by dots, with the date and
// the words "catalog retrieved" a link to the catalog's own URL that opens in a
// new tab. The region carries no note about how it was chosen, since the
// maintainer removed it.
func TestReportHeaderNamesTheSourceTheFileTheCatalogAndTheRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    assessment.Assessment
	}{
		{name: "every fit, without estimates", a: fitsAssessment(t)},
		{name: "with estimates", a: estimatesAssessment(t)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := tc.a
			fragment := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))
			elements := fragmentElements(t, fragment)

			date := a.Snapshot.RetrievedOn.UTC().Format("2006-01-02")

			line := "source " + a.Source + " · " + inventoryFileName + " · catalog retrieved " + date

			region := a.Region
			if region == "" {
				region = a.EstimateRegion
			}

			if region != "" {
				line += " · region " + region
			}

			if text := textOf(fragment); !strings.Contains(text, line) {
				t.Errorf("the fragment's text does not carry the header line %q (issue #373, item 10-1)\n%s", line, text)
			}

			for _, bracketed := range []string{"(the target's default", "(chosen with", "for estimates)"} {
				if text := textOf(fragment); strings.Contains(text, bracketed) {
					t.Errorf("the fragment's text carries %q, want the region with no note about how it was chosen (issue #373, item 10-1)\n%s", bracketed, text)
				}
			}

			anchors := func() []int {
				var out []int

				for i, element := range elements {
					if element.tag == "a" {
						out = append(out, i)
					}
				}

				return out
			}()

			if len(anchors) != 1 {
				t.Fatalf("the fragment holds %d <a> elements, want exactly one: \\\"catalog retrieved <date>\\\" links the catalog (issue #373, item 10-1)", len(anchors))
			}

			anchor := elements[anchors[0]]

			if want := `href="` + escapedText(a.Snapshot.SourceURL) + `"`; !strings.Contains(anchor.attrs, want) {
				t.Errorf("the catalog link carries %q, want %q: it names the catalog's own URL, escaped (issue #373, item 10-1)", anchor.attrs, want)
			}

			if got, _ := attributeOf(anchor.attrs, "target"); got != "_blank" {
				t.Errorf("the catalog link carries target=%q, want \"_blank\": the reader keeps the report they are reading (issue #373, item 10-1)", got)
			}

			if got, _ := attributeOf(anchor.attrs, "rel"); !slices.Equal(strings.Fields(got), []string{"noopener", "noreferrer"}) {
				t.Errorf("the catalog link carries rel=%q, want \"noopener noreferrer\": the page it opens gets no handle on this one (issue #373, item 10-1)", got)
			}

			if want := "catalog retrieved " + date; textOfElement(elements, anchors[0]) != want {
				t.Errorf("the catalog link reads %q, want %q (issue #373, item 10-1)", textOfElement(elements, anchors[0]), want)
			}
		})
	}
}

// TestReportCatalogLinkIsEscaped pins that a catalog URL holding a character that
// would end an attribute reaches the page escaped (issue #373, item 10-1): the
// link is the one value the fragment writes into an attribute, so it is escaped
// as the value in an href and never opens a tag of its own.
func TestReportCatalogLinkIsEscaped(t *testing.T) {
	t.Parallel()

	const hostileURL = `https://www.example.com/pricing/?q="x"&page=<2>&y='z'`

	a := fitsAssessment(t)
	a.Snapshot.SourceURL = hostileURL

	got := render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName})

	checkNoInjectedMarkup(t, got)

	fragment := string(got)

	if want := `href="` + escapedText(hostileURL) + `"`; !strings.Contains(fragment, want) {
		t.Errorf("the catalog link does not carry %q, want the URL escaped in its href, as the fragment escapes every value (issue #373, item 10-1)\n%s", want, fragment)
	}

	for _, raw := range []string{"<2>", `q="x"`, "y='z'"} {
		if strings.Contains(fragment, raw) {
			t.Errorf("the fragment holds the raw %q, want every character of the URL escaped (issue #373, item 10-1)\n%s", raw, fragment)
		}
	}
}

// TestReportCatalogURLThatIsNoHTTPSURLIsWords pins the reporter's last line of
// defence around the catalog link (issue #373, item 10-1, audit finding 1): a
// catalog URL that is no absolute https URL reaches the page as the words alone,
// with no link at all, so a URL of another scheme (javascript:, data:) or of
// another form (http, a leading space, upper-case HTTPS) never reaches an href,
// even if the domain stopped refusing such a URL. The date is still shown, as
// text: the reader loses the link, never the catalog's date.
func TestReportCatalogURLThatIsNoHTTPSURLIsWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "javascript", url: "javascript:alert(1)"},
		{name: "javascript in capitals", url: "JAVASCRIPT:alert(1)"},
		{name: "data", url: "data:text/html,x"},
		{name: "http", url: "http://example.com/"},
		{name: "a leading space", url: " https://x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := fitsAssessment(t)
			a.Snapshot.SourceURL = tc.url

			got := render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName})

			checkNoInjectedMarkup(t, got)

			fragment := string(got)

			for _, unwanted := range []string{"<a", "href="} {
				if strings.Contains(fragment, unwanted) {
					t.Errorf("the fragment whose catalog URL is %q holds %q, want the words alone with no link: the reporter writes an href only for an absolute https URL (issue #373, item 10-1)\n%s",
						tc.url, unwanted, fragment)
				}
			}

			date := a.Snapshot.RetrievedOn.UTC().Format("2006-01-02")

			if want := "catalog retrieved " + date; !strings.Contains(textOf(fragment), want) {
				t.Errorf("the fragment whose catalog URL is %q reads %q, want it to carry %q as text: a URL that is no https URL loses the link, never the catalog's date (issue #373, item 10-1)\n%s",
					tc.url, textOf(fragment), want, fragment)
			}
		})
	}
}

// TestReportTableScrollsByKeyboard pins that a table's box can be reached and
// scrolled with the keyboard (issue #373, item 11, review finding 6): each box
// that scrolls sideways is a named region of its own, focusable, so Safari's and
// Firefox's keyboard users can scroll it and a screen reader announces what it
// is. The name is a constant of the reporter's, never a value from the
// inventory, so the fragment's escaping invariant holds: text from an inventory
// is never markup, and no attribute of the fragment is taken from one.
func TestReportTableScrollsByKeyboard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report func(t *testing.T) []byte
		ids    []string
	}{
		{
			name: "every fit, without estimates",
			report: func(t *testing.T) []byte {
				t.Helper()

				return render(t, fitsAssessment(t), analysis.ReportMeta{InventoryName: inventoryFileName})
			},
			ids: resourceIDs(awsResources()),
		},
		{
			name: "with estimates",
			report: func(t *testing.T) []byte {
				t.Helper()

				return render(t, estimatesAssessment(t), analysis.ReportMeta{InventoryName: inventoryFileName})
			},
			ids: resourceIDs(estimateResources()),
		},
		{
			name: "a comparison of three targets",
			report: func(t *testing.T) []byte {
				t.Helper()

				inv := newInventory(t, sourceName, comparisonResources())

				return renderComparison(t, assessment.Compare([]assessment.TargetAssessment{
					{Key: "ovhcloud", Assessment: assessment.Assess(inv, testutil.OVHcloudCatalog(t))},
					{Key: "scaleway", Assessment: assessment.Assess(inv, testutil.ScalewayCatalog(t))},
					{Key: "upcloud", Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
				}), analysis.ReportMeta{InventoryName: inventoryFileName})
			},
			ids: resourceIDs(comparisonResources()),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			elements := fragmentElements(t, string(tc.report(t)))

			boxes := elementsWithClass(t, elements, "table-scroll")
			if len(boxes) == 0 {
				t.Fatal("the fragment holds no box carrying the class \"table-scroll\", so this check read nothing: each table sits in a box of its own that scrolls sideways (issue #373, item 11)")
			}

			for _, box := range boxes {
				if got, _ := attributeOf(elements[box].attrs, "tabindex"); got != "0" {
					t.Errorf("a table's box carries tabindex=%q, want \"0\": the box scrolls sideways, so a keyboard visitor must be able to focus it and scroll it (issue #373, item 11)\n%s",
						got, elements[box].attrs)
				}

				if got, _ := attributeOf(elements[box].attrs, "role"); got != "region" {
					t.Errorf("a table's box carries role=%q, want \"region\": a focusable box that scrolls is a region of its own, announced as one (issue #373, item 11)\n%s",
						got, elements[box].attrs)
				}

				label, ok := attributeOf(elements[box].attrs, "aria-label")
				if !ok || strings.TrimSpace(label) == "" {
					t.Errorf("a table's box carries aria-label=%q, want a non-empty constant that names the table: a region with no name tells a screen reader nothing (issue #373, item 11)",
						label)
				}

				for _, id := range tc.ids {
					if strings.Contains(label, id) {
						t.Errorf("a table's box is named %q, which holds the inventory's value %q, want a constant of the reporter's: no attribute of the fragment is taken from the inventory (issue #373, item 10-1)\n%s",
							label, id, elements[box].attrs)
					}
				}
			}

			for i, element := range elements {
				if element.tag != "table" {
					continue
				}

				header := headerCells(t, elements, i)
				if len(header) < 2 || header[0] != "ID" || header[1] != "Raw type" {
					continue
				}

				box := element.parent
				if box < 0 {
					t.Fatalf("a Resources table sits at the fragment's top level, want it in a box of its own (issue #373, item 11)")
				}

				if label, _ := attributeOf(elements[box].attrs, "aria-label"); label != resourcesTableLabel {
					t.Errorf("the Resources table's box is named %q, want %q: the box is a named region, and its name is a constant of the reporter's (issue #373, item 11)",
						label, resourcesTableLabel)
				}
			}
		})
	}
}

// resourcesTableLabel is the name the resources table's scrolling box carries.
const resourcesTableLabel = "Resources table"

// TestReportSummaryShowsABoxPerFit pins the summary (issue #373, item 10-2): one
// box per fit, each holding the fit's badge and the fit's count, and, when the
// report has estimates, one more box holding the monthly estimate. The summary is
// no table: a box is an element of its own, so the stylesheet can lay the four
// out as tiles.
func TestReportSummaryShowsABoxPerFit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    assessment.Assessment
		// amounts is the monthly total the estimate box shows, "" for a report
		// without estimates.
		amounts string
	}{
		{name: "every fit, without estimates", a: fitsAssessment(t)},
		{name: "with estimates", a: estimatesAssessment(t), amounts: "€2,535.00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := tc.a
			fragment := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))
			elements := fragmentElements(t, fragment)

			summaries := elementsWithClass(t, elements, "part-summary")
			if len(summaries) != 1 {
				t.Fatalf("the fragment holds %d Summary part sections, want exactly one", len(summaries))
			}

			summary := summaries[0]

			if tables := tableIndices(elements, summary); len(tables) != 0 {
				t.Errorf("the Summary part section holds %d tables, want none: the four summary tiles are boxes, not a table's rows (issue #373, item 10-2)", len(tables))
			}

			counts := map[mapping.Fit]int{
				mapping.Direct:  a.Summary.Direct,
				mapping.Partial: a.Summary.Partial,
				mapping.None:    a.Summary.None,
			}

			// captions are the words each fit's tile ends with (issue #373, item
			// 03-2): the direct tile keeps the count's total, and the partial and
			// none tiles say what that fit means for the move.
			captions := map[mapping.Fit]string{
				mapping.Direct:  ofResources(a.Summary.Total),
				mapping.Partial: "needs changes",
				mapping.None:    "blocked",
			}

			for _, fit := range []mapping.Fit{mapping.Direct, mapping.Partial, mapping.None} {
				badges := elementsWithClass(t, elements, "fit", "fit-"+string(fit))

				boxes := boxesOf(t, elements, summary, badges)
				if len(boxes) != 1 {
					t.Errorf("the Summary part section holds %d boxes with the %s badge, want exactly one box per fit (issue #373, item 10-2)",
						len(boxes), fit)

					continue
				}

				box := textOfElement(elements, boxes[0])

				if !regexp.MustCompile(`\b` + strconv.Itoa(counts[fit]) + `\b`).MatchString(box) {
					t.Errorf("the %s box reads %q, want it to hold the count %d (issue #373, item 10-2)",
						fit, box, counts[fit])
				}

				if !strings.HasSuffix(box, captions[fit]) {
					t.Errorf("the %s box reads %q, want it to end with %q, as the artifact's tiles have it (issue #373, item 03-2)",
						fit, box, captions[fit])
				} else if fit != mapping.Direct && strings.Contains(box, ofResources(a.Summary.Total)) {
					t.Errorf("the %s box reads %q, want no %q: only the direct tile counts the resources the total is of (issue #373, item 03-2)",
						fit, box, ofResources(a.Summary.Total))
				}

				for _, badge := range badgesInside(elements, summary, badges) {
					if !glyphIsSilent(elements, badge) {
						t.Errorf("the %s badge holds no element with aria-hidden=\"true\", want its glyph hidden from screen readers so the badge is read as the fit's word (issue #373, item 10-2)",
							fit)
					}
				}
			}

			if tc.amounts == "" {
				return
			}

			estimateBox := -1

			for _, child := range elements[summary].children {
				if strings.Contains(textOfElement(elements, child), "Monthly estimate") {
					estimateBox = child
				}
			}

			if estimateBox < 0 {
				t.Fatalf("the Summary part section holds no box reading \\\"Monthly estimate\\\", want the monthly estimate beside the three fits (issue #373, item 10-2)")
			}

			amountShown := false

			for _, descendant := range descendants(elements, estimateBox) {
				if textOfElement(elements, descendant) == tc.amounts {
					amountShown = true
				}
			}

			if !amountShown {
				t.Errorf("the monthly-estimate box reads %q, want the amount %q in an element of its own (issue #373, item 10-2)",
					textOfElement(elements, estimateBox), tc.amounts)
			}
		})
	}
}

// boxesOf returns the boxes of badges: the elements directly inside the section
// that hold one of them.
func boxesOf(t *testing.T, elements []fragElement, section int, badges []int) []int {
	t.Helper()

	want := make(map[int]bool, len(badges))
	for _, badge := range badges {
		want[badge] = true
	}

	var boxes []int

	for _, child := range elements[section].children {
		for _, descendant := range append([]int{child}, descendants(elements, child)...) {
			if want[descendant] {
				boxes = append(boxes, child)

				break
			}
		}
	}

	return boxes
}

// badgesInside returns the badges that sit inside the section.
func badgesInside(elements []fragElement, section int, badges []int) []int {
	allowed := make(map[int]bool, len(elements[section].children))

	for _, descendant := range descendants(elements, section) {
		allowed[descendant] = true
	}

	var out []int

	for _, badge := range badges {
		if allowed[badge] {
			out = append(out, badge)
		}
	}

	return out
}

// glyphIsSilent reports whether the badge holds an element that the browser does
// not read out: an element with aria-hidden="true" and some text, which is the
// glyph.
func glyphIsSilent(elements []fragElement, badge int) bool {
	for _, descendant := range descendants(elements, badge) {
		hidden, ok := attributeOf(elements[descendant].attrs, "aria-hidden")
		if !ok || hidden != "true" {
			continue
		}

		if strings.TrimSpace(textOfElement(elements, descendant)) != "" {
			return true
		}
	}

	return false
}

// tableIndices returns the indices of the tables inside the element index.
func tableIndices(elements []fragElement, index int) []int {
	var out []int

	for _, descendant := range descendants(elements, index) {
		if elements[descendant].tag == "table" {
			out = append(out, descendant)
		}
	}

	return out
}

// TestReportResourcesTableScrollsInItsOwnBox pins the resources table's box
// (issue #373, item 11): the table sits in an element of its own that the
// stylesheet can scroll sideways, and every column the Markdown report has stays
// in it.
func TestReportResourcesTableScrollsInItsOwnBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    assessment.Assessment
		want []string
	}{
		{
			name: "every fit, without estimates",
			a:    fitsAssessment(t),
			want: []string{"ID", "Raw type", "Category", "Region", "Fit", "Best offering", "Reasons"},
		},
		{
			name: "with estimates",
			a:    estimatesAssessment(t),
			want: []string{"ID", "Raw type", "Category", "Region", "Fit", "Best offering", "Plan", "Monthly estimate", "Reasons"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fragment := string(render(t, tc.a, analysis.ReportMeta{InventoryName: inventoryFileName}))
			elements := fragmentElements(t, fragment)

			resources := -1

			for i, element := range elements {
				if element.tag != "table" {
					continue
				}

				header := headerCells(t, elements, i)
				if len(header) > 0 && header[0] == "ID" {
					resources = i
				}
			}

			if resources < 0 {
				t.Fatalf("the fragment holds no Resources table, want the table whose first column is the resource's ID (issue #373, item 11)")
			}

			header := headerCells(t, elements, resources)

			if diff := missingInOrder(tc.want, header); diff != "" {
				t.Errorf("the Resources table's header is %q, want every column the Markdown report has, in order: %s (issue #373, item 11)", header, diff)
			}

			parent := elements[resources].parent
			if parent < 0 {
				t.Fatalf("the Resources table sits at the fragment's top level, want it in a box of its own that scrolls sideways (issue #373, item 11)")
			}

			if classes := classOf(t, elements, parent); !slices.Contains(classes, "table-scroll") {
				t.Errorf("the Resources table sits in <%s class=%q>, want an element carrying the class \"table-scroll\": the table's box is what scrolls sideways on a phone (issue #373, item 11)",
					elements[parent].tag, strings.Join(classes, " "))
			}
		})
	}
}

// headerCells returns the text of the header cells of the table at index.
func headerCells(t *testing.T, elements []fragElement, index int) []string {
	t.Helper()

	var cells []string

	head := -1

	for _, descendant := range descendants(elements, index) {
		if elements[descendant].tag == "thead" {
			head = descendant
		}
	}

	if head < 0 {
		return nil
	}

	for _, descendant := range descendants(elements, head) {
		if elements[descendant].tag == "th" {
			cells = append(cells, textOfElement(elements, descendant))
		}
	}

	return cells
}

// missingInOrder reports a header that does not hold the columns want in the
// order they are given, and "" when it does. The columns need not be adjacent, so
// a report that adds one of its own is still read.
func missingInOrder(want, got []string) string {
	next := 0

	for _, column := range got {
		if next < len(want) && column == want[next] {
			next++
		}
	}

	if next == len(want) {
		return ""
	}

	return "the column " + strconv.Quote(want[next]) + " is missing or out of order"
}

// TestReportRequiredChangesAndBlockersAreSideBySide pins the two boxes of
// required changes and blockers (issue #373, item 12): sibling boxes inside one
// container, so the stylesheet puts them side by side on a wide page, and each
// point stays a list item.
func TestReportRequiredChangesAndBlockersAreSideBySide(t *testing.T) {
	t.Parallel()

	a := fitsAssessment(t)
	fragment := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))
	elements := fragmentElements(t, fragment)

	changes := elementsWithClass(t, elements, "part-changes")
	blockers := elementsWithClass(t, elements, "part-blockers")

	if len(changes) != 1 || len(blockers) != 1 {
		t.Fatalf("the fragment holds %d required-changes sections and %d blockers sections, want one of each", len(changes), len(blockers))
	}

	container := elements[changes[0]].parent

	if container < 0 || container != elements[blockers[0]].parent {
		t.Errorf("the required changes and blockers sections sit in different elements, want two sibling boxes inside one container, so the stylesheet puts them side by side (issue #373, item 12)")
	} else if container == 0 {
		t.Errorf("the required changes and blockers sections are the fragment's own children, want them inside one container, so the stylesheet puts them side by side (issue #373, item 12)")
	}

	for _, part := range []struct {
		name  string
		index int
	}{
		{name: "required changes", index: changes[0]},
		{name: "blockers", index: blockers[0]},
	} {
		items := 0

		for _, descendant := range descendants(elements, part.index) {
			if elements[descendant].tag == "li" {
				items++
			}
		}

		if items == 0 {
			t.Errorf("the %s box holds no list item, want one <li> per point (issue #373, item 12)", part.name)
		}
	}
}

// TestReportWorkloadsAreBoxes pins the workload boxes (issue #373, item 13): one
// box per workload, holding its name, a badge with the count of each fit, and its
// monthly estimate, and holding no resource of its own. The per-resource detail
// the fragment repeated under each workload is gone.
func TestReportWorkloadsAreBoxes(t *testing.T) {
	t.Parallel()

	a := estimatesAssessment(t)
	fragment := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))
	elements := fragmentElements(t, fragment)

	workloads := elementsWithClass(t, elements, "part-workloads")
	if len(workloads) != 1 {
		t.Fatalf("the fragment holds %d workloads sections, want exactly one", len(workloads))
	}

	want := map[string]struct {
		direct, partial, none int
		amount                string
	}{
		"orders": {direct: 1, partial: 0, none: 1, amount: "€10.00"},
		"shop":   {direct: 3, partial: 0, none: 0, amount: "€2,525.00"},
	}

	boxes := 0

	for _, child := range elements[workloads[0]].children {
		headings := 0

		for _, descendant := range descendants(elements, child) {
			if elements[descendant].tag == "h4" {
				headings++
			}
		}

		if headings == 0 {
			continue
		}

		boxes++

		text := textOfElement(elements, child)

		name := ""

		for candidate := range want {
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(candidate) + `\b`).MatchString(text) {
				name = candidate
			}
		}

		if name == "" {
			t.Errorf("a workload box reads %q, want it to name one of the workloads %v (issue #373, item 13)", text, want)

			continue
		}

		expected := want[name]

		for fit, count := range map[string]int{"direct": expected.direct, "partial": expected.partial, "none": expected.none} {
			badges := 0

			for _, descendant := range descendants(elements, child) {
				if slices.Contains(classOf(t, elements, descendant), "fit-"+fit) {
					badges++

					if !regexp.MustCompile(`\b` + strconv.Itoa(count) + `\b`).MatchString(textOfElement(elements, descendant)) {
						t.Errorf("the %s box's %s badge reads %q, want it to hold the count %d (issue #373, item 13)",
							name, fit, textOfElement(elements, descendant), count)
					}

					if got := visibleTextOf(t, elements, descendant); got != strconv.Itoa(count) {
						t.Errorf("the %s box's %s bubble reads %q to a reader, want only the count %d: the fit's word is not shown in a workload's bubble (issue #373, item 05)",
							name, fit, got, count)
					} else if !readsToScreenReaders(t, elements, descendant, fit) {
						t.Errorf("the %s box's %s bubble holds no element only a screen reader reads that says %q, want the fit's word there: a colour and a glyph alone tell a screen reader nothing (issue #373, item 05)",
							name, fit, fit)
					}
				}
			}

			if badges != 1 {
				t.Errorf("the %s box holds %d %s badges, want exactly one with the fit's count (issue #373, item 13)", name, badges, fit)
			}
		}

		if !strings.Contains(text, expected.amount) {
			t.Errorf("the %s box reads %q, want it to show the monthly estimate %q (issue #373, item 13)", name, text, expected.amount)
		}
	}

	if boxes != len(want) {
		t.Errorf("the workloads section holds %d boxes, want %d: one per workload (issue #373, item 13)", boxes, len(want))
	}

	text := textOfElement(elements, workloads[0])

	for _, id := range resourceIDs(estimateResources()) {
		if strings.Contains(text, id) {
			t.Errorf("the workloads section reads %q, want no resource of its own: the per-resource detail was removed from the workload boxes (issue #373, item 13)",
				text)
		}
	}
}

// TestReportCostEstimateIsOneLine pins the cost estimate (issue #373, item 14):
// the total in an element of its own, the words "per month, <x> of <y>
// resources" on the same line, and one closing line that says what the prices
// are. The wordy lines the estimate used to list are gone.
func TestReportCostEstimateIsOneLine(t *testing.T) {
	t.Parallel()

	a := estimatesAssessment(t)
	fragment := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))
	elements := fragmentElements(t, fragment)

	const (
		amount = "€2,535.00"
		basis  = "4 of 5 resources"
	)

	totals := elementsWithClass(t, elements, "total")
	if len(totals) != 1 {
		t.Fatalf("the fragment holds %d elements with the class \"total\", want exactly one: the cost estimate's total (issue #373, item 14)", len(totals))
	}

	if got := textOfElement(elements, totals[0]); got != amount {
		t.Errorf("the total element reads %q, want %q alone in its own element, with the basis beside it (issue #373, item 14)", got, amount)
	}

	line := ""

	if parent := elements[totals[0]].parent; parent >= 0 {
		line = textOfElement(elements, parent)
	}

	if !strings.Contains(line, "per month, "+basis) {
		t.Errorf("the total's line reads %q, want it to carry %q (issue #373, item 14)", line, "per month, "+basis)
	}

	costs := elementsWithClass(t, elements, "part-cost")
	if len(costs) != 1 {
		t.Fatalf("the fragment holds %d cost-estimate sections, want exactly one", len(costs))
	}

	note := "List prices in " + a.EstimateRegion + " on " + a.Snapshot.RetrievedOn.UTC().Format("2006-01-02") + ", excluding VAT and discounts."

	noted := false

	for _, descendant := range descendants(elements, costs[0]) {
		if strings.HasPrefix(textOfElement(elements, descendant), note) {
			noted = true
		}
	}

	if !noted {
		t.Errorf("the cost estimate holds no closing line beginning %q, want the artifact's one line (issue #373, item 14)\n%s",
			note, textOfElement(elements, costs[0]))
	}

	for _, wordy := range []string{"Hours:", "Prices in ", "Estimate: list prices"} {
		if strings.Contains(textOfElement(elements, costs[0]), wordy) {
			t.Errorf("the cost estimate reads %q, want no %q line: the closing note is one line, as the artifact has it (issue #373, item 14)",
				textOfElement(elements, costs[0]), wordy)
		}
	}
}

// ofResources is "of N resources", the words the summary's direct tile ends
// with, and "of 1 resource" for one.
func ofResources(n int) string {
	if n == 1 {
		return "of 1 resource"
	}

	return "of " + strconv.Itoa(n) + " resources"
}

// visibleTextOf returns the text of the element at index as a reader sees it:
// the text of its own runs and of its children's, but none of a subtree the page
// hides from assistive technology. A check that reads the whole text cannot tell
// a word a reader sees from a word only a screen reader reads, which is what the
// workload bubbles now say (issue #373, item 05).
func visibleTextOf(t *testing.T, elements []fragElement, index int) string {
	t.Helper()

	var b strings.Builder

	var walk func(int)

	walk = func(i int) {
		if hiddenFromReaders(t, elements, i) {
			return
		}

		next := 0

		for childIndex, child := range elements[i].children {
			for ; next < len(elements[i].texts) && elements[i].texts[next].at <= childIndex; next++ {
				b.WriteString(elements[i].texts[next].text)
			}

			walk(child)
		}

		for ; next < len(elements[i].texts); next++ {
			b.WriteString(elements[i].texts[next].text)
		}
	}

	walk(index)

	return textOf(b.String())
}

// hiddenFromReaders reports whether an element is hidden from assistive
// technology: it carries aria-hidden="true", as the badges' glyphs do, or the
// class the site's stylesheet keeps for text only a screen reader reads.
func hiddenFromReaders(t *testing.T, elements []fragElement, index int) bool {
	t.Helper()

	if hidden, ok := attributeOf(elements[index].attrs, "aria-hidden"); ok && hidden == "true" {
		return true
	}

	return slices.Contains(classOf(t, elements, index), "visually-hidden")
}

// readsToScreenReaders reports whether the element holds the word in a subtree
// only a screen reader reads: an element of the site's visually-hidden class.
func readsToScreenReaders(t *testing.T, elements []fragElement, index int, word string) bool {
	t.Helper()

	for _, descendant := range descendants(elements, index) {
		if !slices.Contains(classOf(t, elements, descendant), "visually-hidden") {
			continue
		}

		if strings.Contains(textOfElement(elements, descendant), word) {
			return true
		}
	}

	return false
}

// resourcesTableIndex returns the index of the Resources table: the one whose
// first column is the resource's ID.
func resourcesTableIndex(t *testing.T, elements []fragElement) int {
	t.Helper()

	for i, element := range elements {
		if element.tag != "table" {
			continue
		}

		if header := headerCells(t, elements, i); len(header) > 0 && header[0] == "ID" {
			return i
		}
	}

	t.Fatal("the fragment holds no Resources table, so this check read nothing: the table whose first column is the resource's ID (issue #373, item 04)")

	return -1
}

// bodyRows returns the body cells of the table at index, one slice of <td>
// indices per row, in document order.
func bodyRows(t *testing.T, elements []fragElement, index int) [][]int {
	t.Helper()

	body := -1

	for _, descendant := range descendants(elements, index) {
		if elements[descendant].tag == "tbody" {
			body = descendant
		}
	}

	if body < 0 {
		t.Fatalf("the table at index %d holds no <tbody>, want its body rows (issue #373, item 04)", index)
	}

	rows := make([][]int, 0, len(elements[body].children))

	for _, row := range elements[body].children {
		if elements[row].tag != "tr" {
			continue
		}

		var cells []int

		for _, child := range elements[row].children {
			if elements[child].tag == "td" {
				cells = append(cells, child)
			}
		}

		rows = append(rows, cells)
	}

	return rows
}

// column returns the index of the table's column whose heading is name, failing
// the test when the table holds none.
func column(t *testing.T, elements []fragElement, table int, name string) int {
	t.Helper()

	header := headerCells(t, elements, table)

	for i, heading := range header {
		if heading == name {
			return i
		}
	}

	t.Fatalf("the Resources table's header %q holds no %q column, so this check read nothing (issue #373, item 04)", header, name)

	return -1
}

// TestReportResourcesTableHasNoRegionColumn pins the resources table's columns
// after the maintainer's review (issue #373, item 04): the Region column is gone,
// column and cells alike, so the table fits the page.
func TestReportResourcesTableHasNoRegionColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    assessment.Assessment
	}{
		{name: "every fit, without estimates", a: fitsAssessment(t)},
		{name: "with estimates", a: estimatesAssessment(t)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := tc.a
			elements := fragmentElements(t, string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName})))
			table := resourcesTableIndex(t, elements)

			for _, heading := range headerCells(t, elements, table) {
				if heading == "Region" {
					t.Errorf("the Resources table holds a Region column, want none: the maintainer removed it to make the table narrower (issue #373, item 04)")
				}
			}

			regions := make(map[string]bool, len(a.Verdicts))
			for _, verdict := range a.Verdicts {
				regions[verdict.Resource.Region] = true
			}

			for row, cells := range bodyRows(t, elements, table) {
				for _, cell := range cells {
					if !regions[textOfElement(elements, cell)] {
						continue
					}

					t.Errorf("the Resources table's row %d holds the region %q in a cell, want no region cell: the column is gone, not only its heading (issue #373, item 04)",
						row+1, textOfElement(elements, cell))

					break
				}
			}
		})
	}
}

// TestReportPlanCellHoldsOnlyThePlanName pins the Plan cell (issue #373, item
// 04): the plan's name alone, without the vCPU and GiB detail in brackets the
// cell showed beside it. The width the detail cost goes to the columns that need
// it.
func TestReportPlanCellHoldsOnlyThePlanName(t *testing.T) {
	t.Parallel()

	elements := fragmentElements(t, string(render(t, estimatesAssessment(t), analysis.ReportMeta{InventoryName: inventoryFileName})))
	plan := column(t, elements, resourcesTableIndex(t, elements), "Plan")

	named := 0

	for row, cells := range bodyRows(t, elements, resourcesTableIndex(t, elements)) {
		if plan >= len(cells) {
			t.Fatalf("the Resources table's row %d holds %d cells, too few for the Plan column %d", row+1, len(cells), plan)
		}

		text := textOfElement(elements, cells[plan])
		if text == "" {
			continue
		}

		named++

		for _, detail := range []string{"vCPU", "GiB", "(", ")"} {
			if !strings.Contains(text, detail) {
				continue
			}

			t.Errorf("the Resources table's row %d holds the Plan cell %q, want the plan's name alone: the vCPU and GiB detail in brackets is not shown in this cell (issue #373, item 04)",
				row+1, text)

			break
		}
	}

	if named == 0 {
		t.Error("no Plan cell names a plan, so this check read nothing: the fixture's sized resources are priced with a plan")
	}
}

// TestReportEstimateCellHoldsOnlyTheAmount pins the Monthly estimate cell (issue
// #373, item 04): the amount alone, or "not estimated" alone. The fixed fee, the
// count and the hours an estimate assumes, and why a resource is not estimated
// stay in the Markdown report, never in this cell.
func TestReportEstimateCellHoldsOnlyTheAmount(t *testing.T) {
	t.Parallel()

	elements := fragmentElements(t, string(render(t, estimatesAssessment(t), analysis.ReportMeta{InventoryName: inventoryFileName})))
	table := resourcesTableIndex(t, elements)
	estimate := column(t, elements, table, "Monthly estimate")

	amount := regexp.MustCompile(`^€[0-9][0-9,]*\.[0-9]{2}$`)

	amounts := 0

	for row, cells := range bodyRows(t, elements, table) {
		if estimate >= len(cells) {
			t.Fatalf("the Resources table's row %d holds %d cells, too few for the Monthly estimate column %d", row+1, len(cells), estimate)
		}

		text := textOfElement(elements, cells[estimate])

		switch {
		case text == "not estimated":
		case amount.MatchString(text):
			amounts++
		default:
			t.Errorf("the Resources table's row %d holds the Monthly estimate cell %q, want the amount alone, as €18.43, or \"not estimated\" alone: no fee, count, hours or reason beside it (issue #373, item 04)",
				row+1, text)
		}
	}

	if amounts == 0 {
		t.Error("no Monthly estimate cell holds an amount, so this check read nothing: the fixture's sized resources are priced")
	}
}

// TestReportNamesNoCurrencyCode pins the currency of every amount (issue #373,
// item 05): the amount carries its sign, never its code, as the artifact has it.
// The prices of the catalogs are in euros, so an amount of the source's own cost
// is the only place a code could appear, and there too the artifact shows the
// amount alone.
func TestReportNamesNoCurrencyCode(t *testing.T) {
	t.Parallel()

	fragment := string(render(t, estimatesAssessment(t), analysis.ReportMeta{InventoryName: inventoryFileName}))

	if strings.Contains(fragment, "USD") {
		t.Errorf("the fragment names the currency code \"USD\", want the amount's sign alone, as the artifact has it (issue #373, item 05)\n%s", fragment)
	}

	for _, wanted := range []string{"$40.00", "€2,535.00"} {
		if !strings.Contains(textOf(fragment), wanted) {
			t.Errorf("the fragment does not show the amount %q, want the amount itself unchanged: no currency code is named, and no amount is lost (issue #373, item 05)\n%s",
				wanted, textOf(fragment))
		}
	}
}

// TestReportComparisonSummaryIsOneBoxPerTarget pins the comparison of several
// targets (issue #373, item 10): each target's summary is its own box under its
// own heading, and the combined summary table the fragment showed beside them is
// gone. The maintainer's screenshot of a comparison showed both at once.
func TestReportComparisonSummaryIsOneBoxPerTarget(t *testing.T) {
	t.Parallel()

	inv := newInventory(t, sourceName, comparisonResources())
	targets := []assessment.TargetAssessment{
		{Key: "ovhcloud", Assessment: assessment.Assess(inv, testutil.OVHcloudCatalog(t))},
		{Key: "scaleway", Assessment: assessment.Assess(inv, testutil.ScalewayCatalog(t))},
		{Key: "upcloud", Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
	}

	elements := fragmentElements(t, string(renderComparison(t, assessment.Compare(targets),
		analysis.ReportMeta{InventoryName: inventoryFileName})))

	comparison := elementsWithClass(t, elements, "part-comparison")
	if len(comparison) != 1 {
		t.Fatalf("the comparison holds %d Comparison sections, want exactly one", len(comparison))
	}

	for _, table := range tableIndices(elements, comparison[0]) {
		if header := headerCells(t, elements, table); len(header) > 2 && header[0] == "Fit" {
			t.Errorf("the Comparison section holds the combined summary table %q, want none: each target's summary is its own box, never a table counting every target side by side (issue #373, item 10)",
				header)
		}
	}

	summaries := elementsWithClass(t, elements, "part-summary")
	if len(summaries) != len(targets) {
		t.Errorf("the comparison holds %d per-target summary boxes, want %d, one per target (issue #373, item 10)",
			len(summaries), len(targets))
	}

	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Assessment.Target)
	}

	counted := make(map[string]int, len(names))

	for _, summary := range summaries {
		name := targetNaming(t, elements, summary, names)
		if name == "" {
			t.Errorf("a Summary box sits under no heading naming a target, want each target's summary under its own name (issue #373, item 10)")

			continue
		}

		counted[name]++
	}

	for _, name := range names {
		if counted[name] != 1 {
			t.Errorf("the comparison holds %d Summary boxes for %s, want exactly one: each target's summary is its own box (issue #373, item 10)",
				counted[name], name)
		}
	}
}

// targetNaming returns the target whose name heads the section the summary box
// sits in: the first heading of the target's own section, or "" when the box
// sits in none.
func targetNaming(t *testing.T, elements []fragElement, summary int, names []string) string {
	t.Helper()

	for ancestor := elements[summary].parent; ancestor >= 0; ancestor = elements[ancestor].parent {
		if !slices.Contains(classOf(t, elements, ancestor), "part-target") {
			continue
		}

		for _, descendant := range descendants(elements, ancestor) {
			if elements[descendant].tag != "h3" {
				continue
			}

			if text := textOfElement(elements, descendant); slices.Contains(names, text) {
				return text
			}

			break
		}

		return ""
	}

	return ""
}
