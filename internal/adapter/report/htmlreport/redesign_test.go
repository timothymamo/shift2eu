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
// tag, the markup it holds and the element it sits in.
type fragElement struct {
	tag     string
	attrs   string
	content string
	// parent is the index of the element this one sits in, -1 for the top.
	parent int
	// children are the indices of the elements directly inside this one.
	children []int
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
	)

	for _, match := range tag.FindAllStringSubmatchIndex(fragment, -1) {
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

			for _, fit := range []mapping.Fit{mapping.Direct, mapping.Partial, mapping.None} {
				badges := elementsWithClass(t, elements, "fit", "fit-"+string(fit))

				if boxes := boxesOf(t, elements, summary, badges); len(boxes) != 1 {
					t.Errorf("the Summary part section holds %d boxes with the %s badge, want exactly one box per fit (issue #373, item 10-2)",
						len(boxes), fit)

					continue
				} else if !regexp.MustCompile(`\b` + strconv.Itoa(counts[fit]) + `\b`).MatchString(textOfElement(elements, boxes[0])) {
					t.Errorf("the %s box reads %q, want it to hold the count %d (issue #373, item 10-2)",
						fit, textOfElement(elements, boxes[0]), counts[fit])
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
