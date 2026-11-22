package htmlreport_test

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

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
// every Fit cell and on every Summary tile, the box every table sits in, the
// class of the words only a screen reader reads, a section part per report
// section, and the Cost estimate's total item.
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

	// hiddenClasses are the class names of the words only a screen reader
	// reads: the word of a workload bubble's fit, which a reader sees as the
	// count alone (issue #373, item 05).
	hiddenClasses = []string{"visually-hidden"}

	// tableClasses are the class names of the box a table sits in: the one box
	// that scrolls sideways on its own, so the page itself never does (issue
	// #373, item 11).
	tableClasses = []string{"table-scroll"}

	// partKinds are the report sections a part section wraps, one kind per
	// heading the fragment emits.
	partKinds = []string{
		"summary", "resources", "changes", "blockers", "workloads", "cost", "comparison", "target",
	}

	// closedClasses is every class name a report element may carry.
	closedClasses = func() map[string]bool {
		closed := map[string]bool{"shift2eu-report": true, "total": true, "part": true}

		for _, classes := range [][]string{fitClasses, hiddenClasses, tableClasses} {
			for _, class := range classes {
				closed[class] = true
			}
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
	// totals are the amounts the Cost estimate states as its total, in document
	// order, without the "Total: " prefix or the basis; none when it has no
	// estimate.
	totals []string
	// summary is what the Summary counts for each fit in a single report, whose
	// Summary is boxes rather than a table; nil for a comparison.
	summary *summaryCounts
	// targets is what each target's own Summary counts for each fit in a
	// comparison, in the comparison's target order; nil for a single report.
	targets []summaryCounts
}

// summaryCounts is what a single report's Summary states per fit: how many of
// the report's resources have that fit, and how many it assessed.
type summaryCounts struct {
	direct    int
	partial   int
	none      int
	resources int
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
			summary: &summaryCounts{direct: 2, partial: 1, none: 2, resources: 5},
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
			totals:  []string{"€2,535.00"},
			summary: &summaryCounts{direct: 4, partial: 0, none: 1, resources: 5},
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
				{heading: "<code>OVHcloud</code>", kind: "target"},
				{heading: "<code>Scaleway</code>", kind: "target"},
				{heading: "<code>UpCloud</code>", kind: "target"},
			},
			targets: []summaryCounts{
				{direct: 2, partial: 1, none: 1, resources: 4},
				{direct: 2, partial: 1, none: 1, resources: 4},
				{direct: 0, partial: 2, none: 2, resources: 4},
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
			summary: &summaryCounts{direct: 1, partial: 0, none: 3, resources: 4},
		},
		{
			name: "a hostile comparison with estimates",
			render: func(t *testing.T) []byte {
				t.Helper()

				return renderComparison(t, hostileComparison(t), analysis.ReportMeta{InventoryName: hostileInventoryName})
			},
			parts: []partExpectation{
				{heading: "<code>" + escapedText(`Evil "Target" & <b>`) + "</code>", kind: "target"},
				{heading: "<code>" + escapedText(`<script>Target</script> "Two"`) + "</code>", kind: "target"},
			},
			totals: []string{
				"€45.00",
				"€45.00",
			},
			targets: []summaryCounts{
				{direct: 2, partial: 1, none: 1, resources: 4},
				{direct: 2, partial: 1, none: 1, resources: 4},
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

// TestReportSummaryRows pins what a Summary states per fit (PRD #297, issue
// #373): a Summary is the redesign's boxes (item 10-2), one per fit, so each
// fit's box states that fit's count and its caption -- the direct box the
// number its count is of, the partial and none boxes what that fit means for
// the move (item 03-2). A comparison holds one such Summary per target, in that
// target's own section, and no combined table counting every target side by
// side (item 10).
func TestReportSummaryRows(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			fragment := string(fixture.render(t))

			if fixture.summary != nil {
				checkSummaryBoxes(t, fragment, *fixture.summary)

				return
			}

			checkTargetSummaries(t, fragment, fixture.targets)
		})
	}
}

// summaryCaptions are the words each fit's Summary box ends with (issue #373,
// item 03-2): the direct box states the total its count is of, and the partial
// and none boxes say what that fit means for the move.
func summaryCaptions(want summaryCounts) map[mapping.Fit]string {
	return map[mapping.Fit]string{
		mapping.Direct:  ofResources(want.resources),
		mapping.Partial: "needs changes",
		mapping.None:    "blocked",
	}
}

// checkSummaryBoxes pins the counts and captions a single report's Summary boxes
// state: the box of each fit states that fit's count, the direct box the
// resources the counts are of, as the Summary table stated before item 10-2
// turned it into boxes. The summary holds no table at all.
func checkSummaryBoxes(t *testing.T, fragment string, want summaryCounts) {
	t.Helper()

	elements := fragmentElements(t, fragment)

	summaries := elementsWithClass(t, elements, "part-summary")
	if len(summaries) != 1 {
		t.Fatalf("the fragment holds %d Summary part sections, want exactly one", len(summaries))
	}

	checkSummaryBox(t, elements, summaries[0], want)
}

// checkSummaryBox pins the three fit boxes of one Summary part section: each
// fit's own badge, its count and its caption, and no table, because a Summary is
// boxes now (issue #373, item 10-2).
func checkSummaryBox(t *testing.T, elements []fragElement, summary int, want summaryCounts) {
	t.Helper()

	if tables := tableIndices(elements, summary); len(tables) != 0 {
		t.Errorf("the Summary part section holds %d tables, want none: a Summary is one box per fit, not a table of counts (PRD #297, issue #373, item 10-2)", len(tables))
	}

	captions := summaryCaptions(want)

	boxes := []struct {
		fit   mapping.Fit
		count int
	}{
		{fit: mapping.Direct, count: want.direct},
		{fit: mapping.Partial, count: want.partial},
		{fit: mapping.None, count: want.none},
	}

	read := 0

	for _, box := range boxes {
		badges := elementsWithClass(t, elements, "fit", "fit-"+string(box.fit))
		count := boxesOf(t, elements, summary, badges)

		if len(count) != 1 {
			t.Errorf("a Summary part section holds %d boxes with the %s badge, want one box per fit (PRD #297, issue #373, item 10-2)",
				len(count), box.fit)

			continue
		}

		text := textOfElement(elements, count[0])

		if figure, ok := figureIn(elements, count[0]); !ok || figure != strconv.Itoa(box.count) {
			t.Errorf("the %s box reads %q, want the count %d in an element of its own: every box states its own fit's count (PRD #297, issue #373, item 10-2)",
				box.fit, text, box.count)
		}

		if caption := captions[box.fit]; !strings.HasSuffix(text, caption) {
			t.Errorf("the %s box reads %q, want it to end with %q (PRD #297, issue #373, item 03-2)",
				box.fit, text, caption)
		}

		read++
	}

	if read == 0 {
		t.Fatalf("the Summary part section holds no box stating a count, so the check read nothing: a Summary is one box per fit")
	}
}

// figureIn returns the text of the one <strong> inside the element index, and
// whether it holds exactly one: a Summary box states its count in a figure of its
// own, apart from the caption beside it.
func figureIn(elements []fragElement, index int) (string, bool) {
	var found []string

	for _, descendant := range descendants(elements, index) {
		if elements[descendant].tag == "strong" {
			found = append(found, textOfElement(elements, descendant))
		}
	}

	if len(found) != 1 {
		return "", false
	}

	return found[0], true
}

// checkTargetSummaries pins a comparison's Summary (issue #373, item 10): the
// combined table counting every target side by side is gone, and each target's
// own section holds its Summary, in the comparison's target order, stating that
// target's counts.
func checkTargetSummaries(t *testing.T, fragment string, want []summaryCounts) {
	t.Helper()

	if len(want) == 0 {
		t.Fatalf("the fixture states no per-target Summary counts, so the check read nothing")
	}

	for _, table := range fragmentTables(t, []byte(fragment)) {
		if len(table.head) > 0 && table.head[0] == "Fit" {
			t.Errorf("the comparison holds the combined Summary table %q, want one Summary per target instead (PRD #297, issue #373, item 10)",
				strings.Join(table.head, " | "))
		}
	}

	elements := fragmentElements(t, fragment)

	summaries := elementsWithClass(t, elements, "part-summary")
	if len(summaries) != len(want) {
		t.Fatalf("the comparison holds %d Summary part sections, want %d, one per target (PRD #297, issue #373, item 10)",
			len(summaries), len(want))
	}

	for i, counts := range want {
		checkSummaryBox(t, elements, summaries[i], counts)
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
				t.Errorf("the fragment holds a <div>, want the sections alone that a report is made of: a part is a <section>, and so is the box a table scrolls in (docs/adr/0035)")
			}
		})
	}
}

// TestReportCostTotalItem pins that a Cost estimate states its total as the one
// element with class "total", holding the amount alone: the stylesheet shows it
// as the report's large euro figure, with the basis it is given on beside it
// (PRD #297, issue #373, item 14).
func TestReportCostTotalItem(t *testing.T) {
	t.Parallel()

	for _, fixture := range reportFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()

			fragment := string(fixture.render(t))
			elements := fragmentElements(t, fragment)

			var totals []string

			for _, element := range elementsWithClass(t, elements, "total") {
				totals = append(totals, textOfElement(elements, element))
			}

			want := fixture.totals

			if diff := cmp.Diff(want, totals); diff != "" {
				t.Errorf("the elements with class \"total\" differ from want (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReportListsKeepTheirListRole pins that the three lists the stylesheet
// strips of markers announce their role (PR #379 review, issue #373): the `<ul>`
// a Blockers heading leads, the one a Cost estimate heading leads, at h3 and h4
// level alike, and the title list the fragment's own `<h2>` leads -- the
// metadata list of inventory file, source and catalogs -- carry the constant
// attribute `role="list"`, and no other element of the fragment carries a role
// but the box each table sits in, a `<section class="table-scroll">` that carries
// the constant attribute `role="region"` and is reachable by keyboard (issue
// #373, item 11).
// Safari and VoiceOver drop the list role and the item count of a list whose
// markers are gone, so the report says it is a list rather than relying on the
// marker; the report's other lists keep their markers and need no attribute.
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

			titles := titleLists(t, fragment)
			if len(titles) == 0 {
				t.Fatalf("the fragment holds no <ul> directly after its <h2>, so the check read nothing: every fragment begins with the title list")
			}

			for _, title := range titles {
				if role, ok := roleOf(title); !ok || role != "list" {
					t.Errorf("the title list the <h2> leads carries %s, want %s: the stylesheet strips its markers with `#report .shift2eu-report > h2 + ul { list-style: none }`, and Safari and VoiceOver drop the list role and its item count once the markers are gone (PR #379 review, issue #373, Accessibility)",
						orNone(title), `role="list"`)
				}
			}

			boxes := tableScrollBoxes(fragment)

			carriers := roleCarriers(fragment)

			if count := len(lists) + len(titles) + len(boxes); len(carriers) != count {
				t.Fatalf("the fragment holds %d elements stating a role (%s), want exactly %d: the <ul> under each Blockers and Cost estimate heading, the title list under the <h2> and the box each table scrolls in are the only elements that state one",
					len(carriers), describeRoles(carriers), count)
			}

			for _, carrier := range carriers {
				if carrier.tag == "ul" && carrier.role == "list" {
					continue
				}

				if isTableScrollBox(carrier) {
					continue
				}

				t.Errorf("the fragment's <%s> carries role=%q on class=%q, want the only roles to be role=\"list\" on a <ul> and role=\"region\" on the table's <section class=\"table-scroll\">: no other element of the report names a role",
					carrier.tag, carrier.role, strings.Join(carrier.classes, " "))
			}
		})
	}
}

// TestReportRoleValuesAreClosed pins that the only role values a fragment states
// are "list", on the lists whose markers the stylesheet strips, and "region", on
// the box each table scrolls in (PR #379 review, issue #373): like a class, a role
// is a constant of this package, never a value of an inventory or a catalog. The
// hostile fixtures carry values that would show here if the reporter wrote one
// into a role.
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
				if carrier.role == "list" || isTableScrollBox(carrier) {
					continue
				}

				t.Errorf("the fragment's <%s> carries role=%q on class=%q, want only role=\"list\" or role=\"region\" on the table's box: a role is a constant, so no value of an inventory or a catalog can reach one (PR #379 review, issue #373)",
					carrier.tag, carrier.role, strings.Join(carrier.classes, " "))
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
	// titleListPattern matches the title list: the `<ul>` start tag directly
	// after the fragment's own <h2>, the report's metadata list.
	titleListPattern = regexp.MustCompile(`(?s)<h2>.*?</h2>\s*<ul([^>]*)>`)
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

// titleLists returns the start tag's attributes of each list the fragment's own
// <h2> leads, in document order: the title list, whose markers the stylesheet
// strips with `#report .shift2eu-report > h2 + ul { list-style: none }`.
func titleLists(t *testing.T, fragment string) []string {
	t.Helper()

	matches := titleListPattern.FindAllStringSubmatch(fragment, -1)

	lists := make([]string, 0, len(matches))

	for _, match := range matches {
		lists = append(lists, strings.TrimSpace(match[1]))
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
	// classes are the class names the element's start tag carries.
	classes []string
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

		carriers = append(carriers, roleCarrier{
			tag:     strings.ToLower(match[1]),
			role:    role,
			classes: classesOf(match[2]),
		})
	}

	return carriers
}

// classesOf returns the class names a start tag's attributes hold, split as the
// attribute's value spells them.
func classesOf(attrs string) []string {
	var classes []string

	for _, match := range classPattern.FindAllStringSubmatch(attrs, -1) {
		classes = append(classes, strings.Fields(match[1])...)
	}

	return classes
}

// tableScrollBoxes returns the attributes of each `<section class="table-scroll">`
// start tag of fragment, in document order: the box each table sits in, which
// scrolls sideways on its own and is a region a keyboard visitor can reach
// (issue #373, item 11).
func tableScrollBoxes(fragment string) []string {
	var boxes []string

	for _, match := range elementTagPattern.FindAllStringSubmatch(fragment, -1) {
		if !strings.EqualFold(match[1], "section") || !carriesTableClass(classesOf(match[2])) {
			continue
		}

		boxes = append(boxes, strings.TrimSpace(match[2]))
	}

	return boxes
}

// carriesTableClass reports whether classes hold the class name of the box a
// table sits in.
func carriesTableClass(classes []string) bool {
	for _, class := range tableClasses {
		if slices.Contains(classes, class) {
			return true
		}
	}

	return false
}

// isTableScrollBox reports whether carrier is the box a table sits in: a
// `<section class="table-scroll">` stating role="region", the one element besides
// the marker-less lists the fragment states a role on (issue #373, item 11).
func isTableScrollBox(carrier roleCarrier) bool {
	return carrier.tag == "section" && carrier.role == "region" && carriesTableClass(carrier.classes)
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
