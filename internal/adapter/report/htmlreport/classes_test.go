package htmlreport_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// This file pins the constant classes the fragment carries for the site's
// redesign (PRD #297, issue #373; the design brief's Tier B): a fit badge on
// every Fit cell, a fit-coloured row on each Summary table, a section part per
// report section, and the Cost estimate's total item.
//
// Every class is a constant chosen from a closed set, never a value: the report
// still writes no value into a tag or an attribute, so the escaping tests hold
// and the site needs no script to colour a report. The goldens are regenerated
// by the reporter's own `-update`; the checks here read the markup, so they say
// what the classes are for rather than which bytes the fragment has.

// The closed sets of class names the report's markup may carry: the fragment's
// own element, the fit badges, the part sections, and the total item.
var (
	// fitClasses are the badge classes of a Fit cell: the word and the fit it
	// names, which the stylesheet draws as a pill with a glyph.
	fitClasses = []string{"fit", "fit-direct", "fit-partial", "fit-none"}

	// summaryClasses are the row classes of a Summary table: one per fit, and
	// the total row's own.
	summaryClasses = []string{"fit-direct", "fit-partial", "fit-none", "fit-total"}

	// partKinds are the report sections a part section wraps, one kind per
	// heading the fragment emits.
	partKinds = []string{
		"summary", "resources", "changes", "blockers", "workloads", "cost", "comparison", "target",
	}

	// closedClasses is every class name a report element may carry.
	closedClasses = func() map[string]bool {
		closed := map[string]bool{"shift2eu-report": true, "total": true, "part": true}

		for _, class := range append(append([]string{}, fitClasses...), summaryClasses...) {
			closed[class] = true
		}

		for _, kind := range partKinds {
			closed["part-"+kind] = true
		}

		return closed
	}()
)

// fitWords are the words a Fit cell shows, which are the mapping's fits.
var fitWords = []string{string(mapping.Direct), string(mapping.Partial), string(mapping.None)}

// partExpectation is one section the fragment wraps in a part section: the
// markup inside its <h3> and the kind its class names.
type partExpectation struct {
	// heading is the exact markup the <h3> holds: the report's own words, or a
	// value in <code>.
	heading string
	// kind is the section's name in partKinds.
	kind string
}

// reportFixture is one rendering of the report the checks read: the fixtures
// report_test.go renders for its goldens.
type reportFixture struct {
	// name is the fixture's name, as the failure names it.
	name string
	// render writes the fixture's fragment.
	render func(t *testing.T) []byte
	// parts are the h3 sections the fragment wraps, in document order.
	parts []partExpectation
	// totals are the Cost estimate totals the fragment states, in document
	// order, without the "Total: " prefix; none when it has no estimate.
	totals []string
}

// reportFixtures returns the renderings the checks read: every fit without
// estimates, with estimates, a comparison of three targets, and the two hostile
// renderings, whose values would be markup or a class if the reporter wrote them
// into one.
func reportFixtures() []reportFixture {
	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	return []reportFixture{
		{
			name: "every fit, without estimates",
			render: func(t *testing.T) []byte {
				t.Helper()

				return render(t, assessment.Assess(newInventory(t, sourceName, awsResources()), testutil.ScalewayCatalog(t)), meta)
			},
			parts: []partExpectation{
				{heading: "Summary", kind: "summary"},
				{heading: "Resources", kind: "resources"},
				{heading: "Required changes", kind: "changes"},
				{heading: "Blockers", kind: "blockers"},
			},
		},
		{
			name: "with estimates",
			render: func(t *testing.T) []byte {
				t.Helper()

				return render(t, assessment.AssessWith(newInventory(t, sourceName, estimateResources()),
					testutil.ReportCatalog(t), assessment.Options{Estimate: true}), meta)
			},
			parts: []partExpectation{
				{heading: "Summary", kind: "summary"},
				{heading: "Resources", kind: "resources"},
				{heading: "Required changes", kind: "changes"},
				{heading: "Blockers", kind: "blockers"},
				{heading: "Workloads", kind: "workloads"},
				{heading: "Cost estimate", kind: "cost"},
			},
			totals: []string{"€2,535.00 per month for 4 of 5 resources"},
		},
		{
			name: "a comparison of three targets",
			render: func(t *testing.T) []byte {
				t.Helper()

				inv := newInventory(t, sourceName, comparisonResources())

				return renderComparison(t, assessment.Compare([]assessment.TargetAssessment{
					{Key: "ovhcloud", Assessment: assessment.Assess(inv, testutil.OVHcloudCatalog(t))},
					{Key: "scaleway", Assessment: assessment.Assess(inv, testutil.ScalewayCatalog(t))},
					{Key: "upcloud", Assessment: assessment.Assess(inv, testutil.UpCloudCatalog(t))},
				}), meta)
			},
			parts: []partExpectation{
				{heading: "Comparison", kind: "comparison"},
				{heading: "<code>OVHcloud</code>", kind: "target"},
				{heading: "<code>Scaleway</code>", kind: "target"},
				{heading: "<code>UpCloud</code>", kind: "target"},
			},
		},
		{
			name: "hostile values without estimates",
			render: func(t *testing.T) []byte {
				t.Helper()

				return render(t, assessment.Assess(newInventory(t, hostileSourceName, hostileResources()), hostileCatalog(t)),
					analysis.ReportMeta{InventoryName: hostileInventoryName})
			},
			parts: []partExpectation{
				{heading: "Summary", kind: "summary"},
				{heading: "Resources", kind: "resources"},
				{heading: "Required changes", kind: "changes"},
				{heading: "Blockers", kind: "blockers"},
				{heading: "Workloads", kind: "workloads"},
			},
		},
		{
			name: "a hostile comparison with estimates",
			render: func(t *testing.T) []byte {
				t.Helper()

				return renderComparison(t, hostileComparison(t), analysis.ReportMeta{InventoryName: hostileInventoryName})
			},
			parts: []partExpectation{
				{heading: "Comparison", kind: "comparison"},
				{heading: "<code>" + escapedText(`Evil "Target" & <b>`) + "</code>", kind: "target"},
				{heading: "<code>" + escapedText(`<script>Target</script> "Two"`) + "</code>", kind: "target"},
			},
			totals: []string{
				"€45.00 per month for 3 of 4 resources",
				"€45.00 per month for 3 of 4 resources",
			},
		},
	}
}

// hostileComparison is the comparison the last fixture renders: the hostile
// inventory against two hostile targets, both with estimates on, so the
// comparison's sections, its targets' sections and its totals all carry values
// that would be markup or a class name if the reporter wrote them into one.
// Each target prices its offerings in its own region, as the hostile comparison
// golden's two targets do.
func hostileComparison(tb testing.TB) assessment.Comparison {
	tb.Helper()

	inv := newInventory(tb, hostileSourceName, hostileComparisonResources())

	targets := []struct {
		key       string
		target    string
		snapshot  catalog.Snapshot
		offerings []catalog.Offering
	}{
		{
			key:       "ovhcloud",
			target:    `Evil "Target" & <b>`,
			snapshot:  testutil.OVHcloudSnapshot(),
			offerings: hostilePricedOfferings("gra", `Evil "Target" & <b>`),
		},
		{
			key:       "scaleway",
			target:    `<script>Target</script> "Two"`,
			snapshot:  testutil.ScalewaySnapshot(),
			offerings: hostilePricedOfferings("fr-par", `<script>Target</script> "Two"`),
		},
	}

	assessed := make([]assessment.TargetAssessment, 0, len(targets))

	for _, target := range targets {
		assessed = append(assessed, assessment.TargetAssessment{
			Key: target.key,
			Assessment: assessment.AssessWith(inv, catalogFor(tb, target.target, target.snapshot, target.offerings),
				assessment.Options{Estimate: true}),
		})
	}

	return assessment.Compare(assessed)
}

// TestReportFitClasses pins that every Fit cell of every Resources table carries
// the badge class of the fit it shows: `fit fit-direct` for a direct fit, and so
// on for partial and none (PRD #297, issue #373). A Resources table is one whose
// header holds a Fit column: "Fit" in a single report, "<target> fit" in a
// comparison.
func TestReportFitClasses(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			fitCells, resources, fits := 0, 0, map[string]int{}

			for _, table := range fragmentTables(t, fixture.render(t)) {
				// A Resources table is headed by ID; the Summary table's first
				// column shows the same three words, but its rows carry no badge.
				if len(table.head) == 0 || table.head[0] != "ID" {
					continue
				}

				resources++

				columns := fitColumns(table)
				if len(columns) == 0 {
					t.Errorf("the Resources table headed %q holds no Fit column, want one: every resource's verdict is a fit badge", strings.Join(table.head, " | "))

					continue
				}

				for _, column := range columns {
					for _, row := range table.rows {
						if column >= len(row.cells) {
							t.Fatalf("a row of the Resources table headed %q holds %d cells, want at least %d: its Fit column is missing",
								strings.Join(table.head, " | "), len(row.cells), column+1)
						}

						cell := row.cells[column]

						if !stringsHasAny(cell.text, fitWords) {
							t.Errorf("a Fit cell of the Resources table headed %q reads %q, want one of %v: the column shows the verdict's fit",
								strings.Join(table.head, " | "), cell.text, fitWords)

							continue
						}

						if want := `class="fit fit-` + cell.text + `"`; cell.attrs != want {
							t.Errorf("the Fit cell showing %q carries %q, want %q: the stylesheet draws the badge from a constant class of the fit, never from a value",
								cell.text, orNone(cell.attrs), want)
						}

						fitCells++
						fits[cell.text]++
					}
				}
			}

			if resources == 0 {
				t.Fatalf("the fragment holds no Resources table, so the check read nothing")
			}

			if fitCells == 0 {
				t.Fatalf("the fragment holds no Fit cell in a table with a Fit column, so the check read nothing")
			}

			if fits[string(mapping.Direct)] == 0 {
				t.Errorf("the fragment holds no direct fit cell, want at least one: every fixture has a resource that moves directly, so a missing badge class would show here")
			}
		})
	}
}

// TestReportUnknownFitBadge pins the badge of a fit the report does not know: a
// verdict whose fit is no value of the mapping's enum gets the constant class
// "fit" alone, never a class made from its text, and the text reaches the cell
// escaped as text, never as an attribute (PRD #297, issue #373). The reporter
// renders such a verdict rather than failing, so a fragment of another version's
// fit still shows a badge, just without a colour.
func TestReportUnknownFitBadge(t *testing.T) {
	t.Parallel()

	hostileFit := mapping.Fit(`bogus" onclick=x`)

	a := assessment.Assessment{
		Target:   "Scaleway",
		Snapshot: testutil.ScalewaySnapshot(),
		Source:   sourceName,
		Verdicts: []assessment.Verdict{{
			Resource: inventory.Resource{
				ID:       "vm-example",
				RawType:  "AWS::EC2::Instance",
				Category: taxonomy.ComputeVM,
				Region:   "eu-west-1",
			},
			Fit: hostileFit,
		}},
	}

	fragment := string(render(t, a, analysis.ReportMeta{InventoryName: inventoryFileName}))

	fits := 0

	for _, table := range fragmentTables(t, []byte(fragment)) {
		if len(table.head) == 0 || table.head[0] != "ID" {
			continue
		}

		columns := fitColumns(table)
		if len(columns) != 1 {
			t.Fatalf("the Resources table headed %q holds %d Fit columns, want 1: every resource's verdict is a fit badge",
				strings.Join(table.head, " | "), len(columns))
		}

		for _, row := range table.rows {
			if columns[0] >= len(row.cells) {
				t.Fatalf("a row of the Resources table holds %d cells, want at least %d: its Fit column is missing",
					len(row.cells), columns[0]+1)
			}

			cell := row.cells[columns[0]]

			if cell.attrs != `class="fit"` {
				t.Errorf("the Fit cell showing %q carries %q, want %q: a fit the report does not know gets the badge class alone, never a class made from its text",
					cell.text, orNone(cell.attrs), `class="fit"`)
			}

			if want := escapedText(string(hostileFit)); cell.text != want {
				t.Errorf("the Fit cell reads %q, want %q: the fit's text is shown escaped as text", cell.text, want)
			}

			fits++
		}
	}

	if fits == 0 {
		t.Fatalf("the fragment holds no Fit cell, so the check read nothing")
	}

	for _, value := range classAttributes(fragment) {
		for _, class := range strings.Fields(value) {
			if !closedClasses[class] {
				t.Errorf("a class attribute holds %q, want only the constants of the closed set %v: the fit's text never becomes a class",
					class, sortedClasses())
			}
		}
	}
}

// TestReportSummaryRows pins that each Summary table carries a row class per fit
// and a class on its total row: `fit-direct`, `fit-partial`, `fit-none` on the
// three fit rows, in that order, and `fit-total` on the row that totals them
// (PRD #297, issue #373). A comparison's Summary table states its rows the same
// way; its estimate rows follow them and carry no fit class.
func TestReportSummaryRows(t *testing.T) {
	t.Parallel()

	want := []struct {
		class string
		word  string
	}{
		{class: "fit-direct", word: string(mapping.Direct)},
		{class: "fit-partial", word: string(mapping.Partial)},
		{class: "fit-none", word: string(mapping.None)},
		{class: "fit-total", word: "total"},
	}

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			summaries := 0

			for _, table := range fragmentTables(t, fixture.render(t)) {
				if len(table.head) == 0 || table.head[0] != "Fit" {
					continue
				}

				summaries++

				if len(table.rows) < len(want) {
					t.Fatalf("the Summary table headed %q holds %d rows, want at least %d: one per fit and the total",
						strings.Join(table.head, " | "), len(table.rows), len(want))
				}

				for i, row := range want {
					got := table.rows[i]

					if want := `class="` + row.class + `"`; got.attrs != want {
						t.Errorf("the %q row of the Summary table headed %q carries %q, want %q: the stylesheet colours the row of a fit, and the total row on its own",
							row.word, strings.Join(table.head, " | "), orNone(got.attrs), want)
					}

					if len(got.cells) == 0 || got.cells[0].text != row.word {
						t.Errorf("row %d of the Summary table headed %q starts with %q, want %q",
							i+1, strings.Join(table.head, " | "), firstCell(got), row.word)
					}
				}
			}

			if summaries == 0 {
				t.Fatalf("the fragment holds no Summary table, so the check read nothing")
			}
		})
	}
}

// TestReportPartSections pins that each h3 the fragment emits sits in a
// `<section class="part part-<kind>">` whose kind is a constant of the closed set
// partKinds, in document order (PRD #297, issue #373): the stylesheet draws each
// part as a card, and the blockers and required changes get their own rules.
// Wrapping a section is a section's job, never a div's.
func TestReportPartSections(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			fragment := string(fixture.render(t))
			parts := headingSections(fragmentSections(t, fragment))

			if len(parts) != len(fixture.parts) {
				t.Fatalf("the fragment holds %d sections with an h3, want %d (%v): each of the report's own sections is one part",
					len(parts), len(fixture.parts), partHeadings(fixture.parts))
			}

			for i, want := range fixture.parts {
				got := parts[i]

				content := strings.TrimSpace(got.content)

				if head := "<h3>" + want.heading + "</h3>"; !strings.HasPrefix(content, head) {
					t.Errorf("section %d holds %q, want it to begin with %q", i+1, excerpt(content), head)
				}

				if got.attrs != `class="part part-`+want.kind+`"` {
					t.Errorf("the section of the h3 %q carries %q, want %q: each part's class is a constant of the closed set %v",
						want.heading, orNone(got.attrs), `class="part part-`+want.kind+`"`, partKinds)
				}
			}

			if strings.Contains(strings.ToLower(fragment), "<div") {
				t.Errorf("the fragment holds a <div>, want sections only: a part is a <section>, and the page's script refuses a div inside the report")
			}
		})
	}
}

// TestReportCostTotalItem pins that a Cost estimate states its total as the one
// list item with class "total": the stylesheet shows it as the report's large
// euro figure, above the basis it is given on (PRD #297, issue #373).
func TestReportCostTotalItem(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			var totals []string

			for _, item := range fragmentItems(t, string(fixture.render(t))) {
				if item.attrs != `class="total"` {
					continue
				}

				totals = append(totals, item.text)
			}

			want := make([]string, 0, len(fixture.totals))
			for _, total := range fixture.totals {
				want = append(want, "Total: "+total)
			}

			if len(totals) != len(want) {
				t.Fatalf("the fragment holds %d list items with class \"total\" (%q), want %d (%q): the Cost estimate's total is the only one",
					len(totals), totals, len(want), want)
			}

			for i, got := range totals {
				if got != want[i] {
					t.Errorf("total item %d reads %q, want %q", i+1, got, want[i])
				}
			}
		})
	}
}

// TestReportListsKeepTheirListRole pins that the two lists the stylesheet strips
// of markers announce their role (PR #379 review, issue #373): the `<ul>` a
// Blockers heading leads and the one a Cost estimate heading leads, at h3 and h4
// level alike, carry the constant attribute `role="list"`, and no other element
// of the fragment carries a role. Safari and VoiceOver drop the list role and the
// item count of a list whose markers are gone, so the report says it is a list
// rather than relying on the marker; the report's other lists keep their markers
// and need no attribute.
func TestReportListsKeepTheirListRole(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			fragment := string(fixture.render(t))

			lists := headedLists(t, fragment)
			if len(lists) == 0 {
				t.Fatalf("the fragment holds no <ul> under a Blockers or a Cost estimate heading, so the check read nothing: every fixture has a blockers list")
			}

			for _, list := range lists {
				if role, ok := roleOf(list.attrs); !ok || role != "list" {
					t.Errorf("the <ul> under the %q heading carries %s, want %s: the stylesheet strips its markers with list-style: none, and Safari and VoiceOver drop the list role and its item count once the markers are gone (PR #379 review, issue #373, Accessibility)",
						list.heading, orNone(list.attrs), `role="list"`)
				}
			}

			carriers := roleCarriers(fragment)

			if len(carriers) != len(lists) {
				t.Fatalf("the fragment holds %d elements stating a role (%s), want exactly %d: the <ul> under each Blockers and Cost estimate heading is the only element that states one",
					len(carriers), describeRoles(carriers), len(lists))
			}

			for _, carrier := range carriers {
				if carrier.tag != "ul" || carrier.role != "list" {
					t.Errorf("the fragment's <%s> carries role=%q, want the only role to be role=\"list\" on a <ul>: no other element of the report names a role",
						carrier.tag, carrier.role)
				}
			}
		})
	}
}

// TestReportRoleValuesAreClosed pins that the only role value a fragment states is
// "list" (PR #379 review, issue #373): like a class, a role is a constant of this
// package, never a value of an inventory or a catalog. The hostile fixtures carry
// values that would show here if the reporter wrote one into a role.
func TestReportRoleValuesAreClosed(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			carriers := roleCarriers(string(fixture.render(t)))
			if len(carriers) == 0 {
				t.Fatalf("the fragment states no role attribute, so the check read nothing: the Blockers and Cost estimate lists announce role=\"list\"")
			}

			for _, carrier := range carriers {
				if carrier.role != "list" {
					t.Errorf("the fragment's <%s> carries role=%q, want only role=\"list\": a role is a constant, so no value of an inventory or a catalog can reach one (PR #379 review, issue #373)",
						carrier.tag, carrier.role)
				}
			}
		})
	}
}

// TestReportClassValuesAreClosed pins that no class attribute of the fragment
// holds anything outside the closed set of names a report element may carry: a
// class is always a constant, so no value of an inventory or a catalog can
// become one (PRD #297, issue #373). The hostile fixtures carry values that a
// reporter writing one into a class would show here.
func TestReportClassValuesAreClosed(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			fragment := string(fixture.render(t))

			classes := classAttributes(fragment)
			if len(classes) == 0 {
				t.Fatalf("the fragment carries no class attribute, so the check read nothing: the redesign's badges and parts are classes")
			}

			for _, value := range classes {
				if strings.TrimSpace(value) == "" {
					t.Errorf("the fragment carries an empty class attribute, want a constant class name")

					continue
				}

				for _, class := range strings.Fields(value) {
					if !closedClasses[class] {
						t.Errorf("a class attribute holds %q, want only the constants of the closed set %v: a class name is never a value, so no text of an inventory or a catalog can reach one",
							class, sortedClasses())
					}
				}
			}
		})
	}
}

// The patterns the checks read a fragment's markup with. They are no HTML parser:
// the fragment is written by this package from constants, so its tables, rows,
// cells, sections and items are well formed and never nested inside one another
// the way a page's may be.
var (
	tablePattern = regexp.MustCompile(`(?s)<table([^>]*)>(.*?)</table>`)
	rowPattern   = regexp.MustCompile(`(?s)<tr([^>]*)>(.*?)</tr>`)
	cellPattern  = regexp.MustCompile(`(?s)<t([hd])([^>]*)>(.*?)</t[hd]>`)
	classPattern = regexp.MustCompile(`class="([^"]*)"`)
	tagPattern   = regexp.MustCompile(`(?s)<[^>]*>`)

	// elementTagPattern matches one element's start tag: its name and its
	// attributes, a quoted value kept whole so a `>` inside one ends no tag.
	elementTagPattern = regexp.MustCompile(`(?s)<([a-zA-Z][a-zA-Z0-9]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	rolePattern       = regexp.MustCompile(`\brole="([^"]*)"`)
	// headedListPattern matches the list a Blockers or a Cost estimate heading
	// leads: the heading and the `<ul>` start tag directly after it.
	headedListPattern = regexp.MustCompile(`(?s)<h([345])>(Blockers|Cost estimate)</h[345]>\s*<ul([^>]*)>`)
)

// fragmentTable is one <table> of a fragment: the cells of its header row and its
// body rows.
type fragmentTable struct {
	// head is the text of each header cell, in column order.
	head []string
	// rows are the table's body rows, in order.
	rows []fragmentRow
}

// fragmentRow is one <tr>: its start tag's attributes and its cells.
type fragmentRow struct {
	// attrs are the attributes of the <tr> start tag, trimmed.
	attrs string
	// cells are the row's cells, in column order.
	cells []fragmentCell
}

// fragmentCell is one <td> or <th>.
type fragmentCell struct {
	// attrs are the attributes of the cell's start tag, trimmed.
	attrs string
	// text is the cell's text content, tags removed and white space collapsed.
	text string
}

// fragmentTables returns the tables of fragment, in order.
func fragmentTables(t *testing.T, fragment []byte) []fragmentTable {
	t.Helper()

	var tables []fragmentTable

	for _, match := range tablePattern.FindAllStringSubmatch(string(fragment), -1) {
		var rows []fragmentRow

		for _, row := range rowPattern.FindAllStringSubmatch(match[2], -1) {
			r := fragmentRow{attrs: strings.TrimSpace(row[1])}

			for _, cell := range cellPattern.FindAllStringSubmatch(row[2], -1) {
				r.cells = append(r.cells, fragmentCell{
					attrs: strings.TrimSpace(cell[2]),
					text:  textOf(cell[3]),
				})
			}

			rows = append(rows, r)
		}

		if len(rows) == 0 {
			continue
		}

		table := fragmentTable{rows: rows[1:]}

		for _, cell := range rows[0].cells {
			table.head = append(table.head, cell.text)
		}

		tables = append(tables, table)
	}

	return tables
}

// fitColumns returns the indices of table's Fit columns: a header cell reading
// "Fit" in a single report, and "<target> fit" in a comparison.
func fitColumns(table fragmentTable) []int {
	var columns []int

	for i, head := range table.head {
		if head == "Fit" || strings.HasSuffix(head, " fit") {
			columns = append(columns, i)
		}
	}

	return columns
}

// fragmentSection is one <section> of a fragment: its start tag's attributes and
// the markup it holds.
type fragmentSection struct {
	// attrs are the attributes of the <section> start tag, trimmed.
	attrs string
	// content is the markup between the start tag and its own end tag.
	content string
}

// fragmentSections returns the sections of fragment, in the order they open,
// failing the test when one is left open or closed twice: a browser reads a
// mis-nested fragment as a different document from the one the reporter wrote.
func fragmentSections(t *testing.T, fragment string) []fragmentSection {
	t.Helper()

	const (
		openTag  = "<section"
		closeTag = "</section>"
	)

	sections := make([]fragmentSection, 0, 8)

	var opened []int

	starts := make([]int, 0, 8)

	for i := 0; i < len(fragment); {
		nextOpen := strings.Index(fragment[i:], openTag)
		nextClose := strings.Index(fragment[i:], closeTag)

		switch {
		case nextOpen < 0 && nextClose < 0:
			i = len(fragment)
		case nextOpen >= 0 && (nextClose < 0 || nextOpen < nextClose):
			at := i + nextOpen + len(openTag)

			end := strings.IndexByte(fragment[at:], '>')
			if end < 0 {
				t.Fatalf("the fragment's <section start tag never ends: %q", fragment[at:])
			}

			sections = append(sections, fragmentSection{attrs: strings.TrimSpace(fragment[at : at+end])})
			opened = append(opened, len(sections)-1)
			starts = append(starts, at+end+1)

			i = at + end + 1
		default:
			if len(opened) == 0 {
				t.Fatalf("the fragment closes a </section> it never opened, near %q", excerpt(fragment[i+nextClose:]))
			}

			last := len(opened) - 1
			sections[opened[last]].content = fragment[starts[last] : i+nextClose]
			opened = opened[:last]
			starts = starts[:last]

			i += nextClose + len(closeTag)
		}
	}

	if len(opened) != 0 {
		t.Fatalf("the fragment leaves %d <section> elements open, want each one closed", len(opened))
	}

	return sections
}

// headingSections returns the sections whose content begins with an h3, in order:
// the report's own sections, as opposed to the sections inside one.
func headingSections(sections []fragmentSection) []fragmentSection {
	var parts []fragmentSection

	for _, section := range sections {
		if strings.HasPrefix(strings.TrimSpace(section.content), "<h3>") {
			parts = append(parts, section)
		}
	}

	return parts
}

// fragmentItem is one <li> of a fragment.
type fragmentItem struct {
	// attrs are the attributes of the <li> start tag, trimmed.
	attrs string
	// text is the item's text content, tags removed and white space collapsed.
	text string
}

// fragmentItems returns the list items of fragment, in order.
func fragmentItems(t *testing.T, fragment string) []fragmentItem {
	t.Helper()

	var items []fragmentItem

	for _, match := range itemPattern.FindAllStringSubmatch(fragment, -1) {
		items = append(items, fragmentItem{attrs: strings.TrimSpace(match[1]), text: textOf(match[2])})
	}

	return items
}

// itemPattern matches one list item. A nested list is a list item's own content,
// so the pattern stops at the item's first end tag: the checks read the items
// whose class they ask for, which hold no list.
var itemPattern = regexp.MustCompile(`(?s)<li([^>]*)>(.*?)</li>`)

// textOf returns the text content of markup: its tags removed and its white
// space collapsed to single spaces.
func textOf(markup string) string {
	return strings.Join(strings.Fields(tagPattern.ReplaceAllString(markup, "")), " ")
}

// classAttributes returns the value of every class attribute in fragment, in
// order.
func classAttributes(fragment string) []string {
	var values []string

	for _, match := range classPattern.FindAllStringSubmatch(fragment, -1) {
		values = append(values, match[1])
	}

	return values
}

// stringsHasAny reports whether s is one of want.
func stringsHasAny(s string, want []string) bool {
	for _, w := range want {
		if s == w {
			return true
		}
	}

	return false
}

// orNone names an attribute value in a failure message, and says when there is
// none at all.
func orNone(attrs string) string {
	if attrs == "" {
		return "no attribute"
	}

	return attrs
}

// firstCell names the first cell of a row in a failure message.
func firstCell(row fragmentRow) string {
	if len(row.cells) == 0 {
		return "no cell"
	}

	return row.cells[0].text
}

// partHeadings returns the headings of parts, for a failure message.
func partHeadings(parts []partExpectation) []string {
	headings := make([]string, 0, len(parts))

	for _, part := range parts {
		headings = append(headings, part.heading)
	}

	return headings
}

// sortedClasses returns the closed set of class names, sorted, for a failure
// message.
func sortedClasses() []string {
	classes := make([]string, 0, len(closedClasses))

	for class := range closedClasses {
		classes = append(classes, class)
	}

	sort.Strings(classes)

	return classes
}

// headedList is one `<ul>` the fragment emits directly under a Blockers or a Cost
// estimate heading: the two kinds of list the stylesheet strips of markers.
type headedList struct {
	// heading is the heading's own text: "Blockers" or "Cost estimate".
	heading string
	// attrs are the attributes of the <ul> start tag, trimmed.
	attrs string
}

// headedLists returns the lists of fragment that a Blockers or a Cost estimate
// heading leads, at h3 and h4 level, in document order, failing the test when a
// heading holds neither of the two names: the check would read nothing.
func headedLists(t *testing.T, fragment string) []headedList {
	t.Helper()

	matches := headedListPattern.FindAllStringSubmatch(fragment, -1)

	lists := make([]headedList, 0, len(matches))

	for _, match := range matches {
		lists = append(lists, headedList{heading: match[2], attrs: strings.TrimSpace(match[3])})
	}

	return lists
}

// roleOf returns the value of the role attribute of a start tag's attributes, and
// whether one is stated.
func roleOf(attrs string) (string, bool) {
	match := rolePattern.FindStringSubmatch(attrs)
	if match == nil {
		return "", false
	}

	return match[1], true
}

// roleCarrier is one element of a fragment that states a role attribute.
type roleCarrier struct {
	// tag is the element's name, lower case.
	tag string
	// role is the role attribute's value, as the fragment spells it.
	role string
}

// roleCarriers returns every element of fragment that states a role attribute, in
// order.
func roleCarriers(fragment string) []roleCarrier {
	var carriers []roleCarrier

	for _, match := range elementTagPattern.FindAllStringSubmatch(fragment, -1) {
		role, ok := roleOf(match[2])
		if !ok {
			continue
		}

		carriers = append(carriers, roleCarrier{tag: strings.ToLower(match[1]), role: role})
	}

	return carriers
}

// describeRoles names the roles a fragment states, for a failure message.
func describeRoles(carriers []roleCarrier) string {
	names := make([]string, 0, len(carriers))

	for _, carrier := range carriers {
		names = append(names, "<"+carrier.tag+" role=\""+carrier.role+"\">")
	}

	return strings.Join(names, ", ")
}

// excerpt returns the first line of content, trimmed and shortened, for a
// failure message.
func excerpt(content string) string {
	trimmed := strings.TrimSpace(content)

	if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
		trimmed = trimmed[:newline]
	}

	if len(trimmed) > 120 {
		return trimmed[:120] + "…"
	}

	return trimmed
}
