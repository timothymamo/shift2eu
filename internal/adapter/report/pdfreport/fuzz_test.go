package pdfreport_test

// The native fuzz test: one field of the report at a time holds the fuzzed
// string, and every structural invariant of the PRD must hold, the value's
// drawing included. The seeds are in the file, so they run under make test; go
// test -fuzz widens the corpus on demand.

import (
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// fuzzField is one field of the report the fuzzed string is put in.
type fuzzField struct {
	// name is the field, for a failure message.
	name string
	// build builds the report's input with value in that field. It returns
	// false when the domain refuses the value, so no report exists to render.
	build func(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool)
}

// fuzzFields is the fields the fuzz test fills one at a time: the three ports'
// untrusted values the report shows, and the report metadata's file name.
func fuzzFields() []fuzzField {
	return []fuzzField{
		{name: "resource ID", build: fuzzResourceID},
		{name: "resource raw type", build: fuzzRawType},
		{name: "resource region", build: fuzzResourceRegion},
		{name: "inventory source", build: fuzzSource},
		{name: "inventory file name", build: fuzzInventoryName},
		{name: "offering name", build: fuzzOfferingName},
		{name: "target display name", build: fuzzTargetName},
	}
}

// fuzzResourceID builds a report whose one resource's ID is value.
func fuzzResourceID(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	return fuzzAssessment(tb, inventory.Resource{
		ID:           value,
		RawType:      "AWS::EC2::Instance",
		Category:     taxonomy.ComputeVM,
		Region:       "eu-west-1",
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
	})
}

// fuzzRawType builds a report whose one resource's raw type is value.
func fuzzRawType(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	return fuzzAssessment(tb, inventory.Resource{
		ID:           "i-0123456789abcdef0",
		RawType:      value,
		Category:     taxonomy.ComputeVM,
		Region:       "eu-west-1",
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
	})
}

// fuzzResourceRegion builds a report whose one resource's region is value.
func fuzzResourceRegion(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	return fuzzAssessment(tb, inventory.Resource{
		ID:           "i-0123456789abcdef0",
		RawType:      "AWS::EC2::Instance",
		Category:     taxonomy.ComputeVM,
		Region:       value,
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
	})
}

// fuzzSource builds a report whose inventory's source is value.
func fuzzSource(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	inv, err := inventory.New(value, fuzzResources())
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	return assessment.Assess(inv, testutil.ReportCatalog(tb)),
		analysis.ReportMeta{InventoryName: inventoryFileName}, true
}

// fuzzInventoryName builds a report whose inventory file name is value.
func fuzzInventoryName(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	inv, err := inventory.New(sourceName, fuzzResources())
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	return assessment.Assess(inv, testutil.ReportCatalog(tb)),
		analysis.ReportMeta{InventoryName: value}, true
}

// fuzzOfferingName builds a report whose one candidate offering's name is value.
func fuzzOfferingName(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	cat, err := catalog.New(reportTarget, testutil.PricedSnapshot(), []catalog.Offering{{
		Name:         value,
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		Regions:      []string{"eu-west-1"},
	}})
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	inv, err := inventory.New(sourceName, fuzzResources())
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	return assessment.Assess(inv, cat), analysis.ReportMeta{InventoryName: inventoryFileName}, true
}

// fuzzTargetName builds a report whose catalog's target display name is value.
func fuzzTargetName(tb testing.TB, value string) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	cat, err := catalog.New(value, testutil.PricedSnapshot(), []catalog.Offering{{
		Name:         "Example Virtual Machines",
		Categories:   []taxonomy.Category{taxonomy.ComputeVM},
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
		Regions:      []string{"eu-west-1"},
	}})
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	inv, err := inventory.New(sourceName, fuzzResources())
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	return assessment.Assess(inv, cat), analysis.ReportMeta{InventoryName: inventoryFileName}, true
}

// fuzzResources is the one resource of the fuzz report: compute.vm, so the
// fixture catalog's compute offering is a candidate and shows its name.
func fuzzResources() []inventory.Resource {
	return []inventory.Resource{{
		ID:           "i-0123456789abcdef0",
		RawType:      "AWS::EC2::Instance",
		Category:     taxonomy.ComputeVM,
		Region:       "eu-west-1",
		Capabilities: []taxonomy.Capability{taxonomy.IPv6},
	}}
}

// fuzzAssessment assesses one resource against the report fixture catalog, and
// reports false when the domain refuses the resource.
func fuzzAssessment(tb testing.TB, resource inventory.Resource) (assessment.Assessment, analysis.ReportMeta, bool) {
	tb.Helper()

	inv, err := inventory.New(sourceName, []inventory.Resource{resource})
	if err != nil {
		return assessment.Assessment{}, analysis.ReportMeta{}, false
	}

	return assessment.Assess(inv, testutil.ReportCatalog(tb)),
		analysis.ReportMeta{InventoryName: inventoryFileName}, true
}

// FuzzReport writes reports in which one field at a time holds the fuzzed
// string, and fails when a structural invariant of the PRD does not hold or the
// value is not drawn as its escaped form.
func FuzzReport(f *testing.F) {
	for _, seed := range fuzzSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, value string) {
		if value == "" || len(value) > textMaxLen {
			// The domain refuses a value that is empty where it may not be and one
			// longer than text.MaxLen; neither reaches a report.
			return
		}

		for _, field := range fuzzFields() {
			a, meta, ok := field.build(t, value)
			if !ok {
				continue
			}

			out := renderReport(t, a, meta)
			file := checkPDFStructure(t, out)

			escaped := pdfreport.EscapeValue(value)

			// checkWrappedValue is the check for any value, wrapped or not: the
			// text extracted for it, joined across its lines, is exactly its
			// escaped form.
			wrapped := checkWrappedValue(t, file, value)

			// A value that fits one line is drawn as one run, so the document's
			// squashed text holds its escaped form whole. A wider value wraps, as
			// the PRD's Tables and page breaks requires, and squashing then puts
			// a space between its lines: checkWrappedValue has already checked it
			// line by line, so there is nothing left to look for here.
			if len(wrapped.chunks) == 1 && !strings.Contains(file.squashed(), squash(escaped)) {
				t.Fatalf("the %s field is not shown; the value's escaped form is %q and the report's text is:\n%s",
					field.name, escaped, file.squashed())
			}
		}
	})
}

// fuzzSeeds is the seed corpus: the hostile values from the PRD and its escape
// classes, one per field in turn by the fuzz target.
func fuzzSeeds() []string {
	return []string{
		"plain",
		"a(b)c",
		") Tj ET",
		`back\slash`,
		"endstream",
		"%%EOF",
		"/JavaScript",
		"<< >>",
		"obj",
		"Ж",
		"ЖЖ ж",
		"é€‘’“”",
		"😀",
		"x\u00a0y",
		"x\u00ady",
		"\u202eevil",
		"trailer<<>>",
		strings.Repeat("\u0416", 100),
		strings.Repeat(`\`, 100),
		strings.Repeat("😀", 40),
		// Found by go test -fuzz: a value of several escape classes whose chunks
		// differ in length, so a chunk after the first starts the escaped form
		// and is the longest run that does.
		"ЖЖЖЖЖЖЖЖЖЖЖЖЖЖЖЖЖ\x960\x96ЖЖЖЖЖЖЖЖЖЖЖ\xd0 ЖЖЖЖЖЖЖЖЖЖЖЖЖЖЖ\xd0 ЖЖЖЖЖЖЖЖЖЖЖЖЖЖЖЖ000000000000000000000000",
	}
}
