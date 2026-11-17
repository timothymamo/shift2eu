package examples_test

// This file pins what PRD #190 milestone 9 makes of the sample inventories under
// examples/: they move to inventory format version 2 (docs/adr/0025) with a
// size on every priced resource, at least two named workloads, relations from
// the whole closed vocabulary, and the source's monthly cost on some resources
// only, so the reports show a partial coverage. It also pins the reports the
// walkthrough and the guide quote beside the default ones: one sample with the
// estimates off, one with a region chosen, and the JSON report checked against
// the schema jsonreport publishes.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/mapping"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

const (
	// sampleSchemaVersion is the inventory format version the samples declare
	// after PRD #190 milestone 9: version 2 adds the size, usage, workload,
	// relations and monthly cost (docs/adr/0025).
	sampleSchemaVersion = 2

	// regionChoice is the Scaleway region one sample report is rendered in, so
	// the walkthrough shows the header line --region adds. The catalog lists
	// fr-par, it-mil, nl-ams and pl-waw.
	regionChoice = "nl-ams"

	// assessmentSchema is the JSON report schema the sample's document must
	// match, from this package's directory: the single-target schema version 2
	// (docs/adr/0018, docs/adr/0030).
	assessmentSchema = repoRoot + "/internal/adapter/report/jsonreport/schema/assessment-v2.schema.json"
)

// pricedSize says which size fields a priced category is priced by (PRD #190):
// vcpus and memoryGiB for a category priced by plan, storageGiB for one priced
// by stored GiB. A category that is not in pricedSizes is not priced at all:
// messaging.queue and serverless.function are priced by usage, and unclassified
// never is.
type pricedSize struct {
	vcpus   bool
	memory  bool
	storage bool
}

// pricedSizes are the priced categories of the taxonomy and the size each is
// priced by. They are written out by hand, as is the fit vocabulary in
// assessment_test.go: the taxonomy declares the categories but not how the
// estimate prices them.
var pricedSizes = map[taxonomy.Category]pricedSize{
	taxonomy.ComputeVM:          {vcpus: true, memory: true},
	taxonomy.ComputeBareMetal:   {vcpus: true, memory: true},
	taxonomy.KubernetesManaged:  {vcpus: true, memory: true},
	taxonomy.DatabasePostgreSQL: {vcpus: true, memory: true},
	taxonomy.DatabaseMySQL:      {vcpus: true, memory: true},
	taxonomy.StorageBlock:       {storage: true},
	taxonomy.StorageObject:      {storage: true},
}

// relationVocabulary is the closed vocabulary of a relation's type, in the order
// docs/adr/0025 declares it. It is written out because the inventory package
// exports the four values but no list of them.
var relationVocabulary = []inventory.RelationType{
	inventory.ReplicaOf,
	inventory.DeadLetterQueueOf,
	inventory.AttachedTo,
	inventory.DependsOn,
}

// sampleDocument is the part of a sample file this file checks before the
// inventory-file adapter reads it: the format version the file declares.
type sampleDocument struct {
	SchemaVersion int `json:"schemaVersion"`
}

// TestSampleInventoriesAreVersion2 pins that every sample declares inventory
// format version 2: a version 1 document still reads, but its resources carry
// no size, workload, relation or monthly cost, so a sample left at version 1
// would silently show none of what milestone 9 documents.
func TestSampleInventoriesAreVersion2(t *testing.T) {
	t.Parallel()

	for _, file := range sampleFiles(t) {
		t.Run(file.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(file.path)
			if err != nil {
				t.Fatalf("read %s: %v", file.path, err)
			}

			var doc sampleDocument
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("%s is not JSON: %v", file.name, err)
			}

			if doc.SchemaVersion != sampleSchemaVersion {
				t.Errorf("%s: schemaVersion = %d, want %d: the sample gives its resources a size, "+
					"workload, relations and monthly cost (docs/adr/0025)", file.name, doc.SchemaVersion,
					sampleSchemaVersion)
			}
		})
	}
}

// TestSampleSizesCoverPricedCategories pins both halves of the sizing promise:
// every sample gives a size to every resource whose category is priced, with the
// fields that category is priced by, and the assessment estimates at least one
// resource of each priced category the sample holds, on every target that
// offers that category. A report that names a size for every resource but still
// estimates none (a size no plan covers, say) fails the second half; checking
// the report through the assessment, not the file alone, is what makes that
// visible.
func TestSampleSizesCoverPricedCategories(t *testing.T) {
	t.Parallel()

	for _, file := range sampleFiles(t) {
		inv := loadSample(t, file.path)

		t.Run(file.name+"/sizes", func(t *testing.T) {
			t.Parallel()

			for _, resource := range inv.Resources() {
				want, priced := pricedSizes[resource.Category]
				if !priced {
					continue
				}

				if resource.Size == nil {
					t.Errorf("%s: resource %q (%s) has no size; a priced category is priced by %s",
						file.name, resource.ID, resource.Category, sizeFields(want))

					continue
				}

				assertSizeField(t, file.name, resource, "vcpus", want.vcpus, resource.Size.VCPUs)
				assertSizeField(t, file.name, resource, "memoryGiB", want.memory, resource.Size.MemoryGiB)
				assertSizeField(t, file.name, resource, "storageGiB", want.storage, resource.Size.StorageGiB)
			}
		})

		for _, target := range targets {
			t.Run(file.name+"/"+target.key+"/estimated", func(t *testing.T) {
				t.Parallel()

				cat, err := target.catalog().Catalog(t.Context())
				if err != nil {
					t.Fatalf("load the %s catalog: %v", target.key, err)
				}

				// counted says, per priced category, how many resources the
				// target offers a candidate for (fit is not none) and how many
				// the report estimates.
				type counted struct {
					fitted    int
					estimated int
				}

				counts := make(map[taxonomy.Category]*counted)

				for _, verdict := range assessment.AssessWith(inv, cat, assessment.Options{Estimate: true}).Verdicts {
					if _, priced := pricedSizes[verdict.Resource.Category]; !priced {
						continue
					}

					c := counts[verdict.Resource.Category]
					if c == nil {
						c = &counted{}
						counts[verdict.Resource.Category] = c
					}

					if verdict.Fit != mapping.None {
						c.fitted++
					}

					if verdict.Estimate != nil && verdict.Estimate.Estimated {
						c.estimated++
					}
				}

				// The taxonomy's own order, so a failure names the categories
				// in the order the vocabulary declares them.
				for _, category := range taxonomy.Categories() {
					if _, priced := pricedSizes[category]; !priced {
						continue
					}

					c := counts[category]
					if c == nil || c.fitted == 0 || c.estimated > 0 {
						continue
					}

					t.Errorf("%s assessed against %s estimates no %s resource, though %d of them have a "+
						"fit there: give them a size a plan covers, so the report shows an estimate for "+
						"every priced category", file.name, target.key, category, c.fitted)
				}
			})
		}
	}
}

// sizeFields names the size fields a priced category takes, for an error
// message: "vcpus and memoryGiB", "storageGiB".
func sizeFields(want pricedSize) string {
	var fields []string

	if want.vcpus {
		fields = append(fields, "vcpus")
	}

	if want.memory {
		fields = append(fields, "memoryGiB")
	}

	if want.storage {
		fields = append(fields, "storageGiB")
	}

	switch len(fields) {
	case 0:
		return ""
	case 1:
		return fields[0]
	}

	return strings.Join(fields[:len(fields)-1], ", ") + " and " + fields[len(fields)-1]
}

// assertSizeField fails the test when the category wants the field (want) and
// the resource leaves it out, or when the category does not want it and the
// resource gives it anyway: either way the size would not be the one the
// estimate reads.
func assertSizeField(t *testing.T, name string, resource inventory.Resource, field string, want bool,
	value *float64,
) {
	t.Helper()

	switch {
	case want && value == nil:
		t.Errorf("%s: resource %q (%s) gives no size.%s; its category is priced by that field",
			name, resource.ID, resource.Category, field)
	case !want && value != nil:
		t.Errorf("%s: resource %q (%s) gives size.%s = %v, which makes no sense for the category",
			name, resource.ID, resource.Category, field, *value)
	}
}

// TestSampleWorkloadsAndRelations pins that every sample names at least two
// workloads, that one of them groups two or more resources, and that it holds
// at least one relation: a sample that names one workload or none shows neither
// the per-workload section nor the relations column of the report.
func TestSampleWorkloadsAndRelations(t *testing.T) {
	t.Parallel()

	for _, file := range sampleFiles(t) {
		t.Run(file.name, func(t *testing.T) {
			t.Parallel()

			resources := loadSample(t, file.path).Resources()

			perWorkload := make(map[string]int)

			for _, resource := range resources {
				if resource.Workload != "" {
					perWorkload[resource.Workload]++
				}
			}

			workloads := make([]string, 0, len(perWorkload))
			for name := range perWorkload {
				workloads = append(workloads, name)
			}

			slices.Sort(workloads)

			if len(workloads) < 2 {
				t.Errorf("%s: the sample names %d workload(s) (%v), want at least 2: the report groups "+
					"resources per workload", file.name, len(workloads), workloads)
			}

			if !slices.ContainsFunc(workloads, func(name string) bool { return perWorkload[name] >= 2 }) {
				t.Errorf("%s: no workload groups two or more resources (workloads: %v), want one: a "+
					"workload of one resource shows no group", file.name, workloads)
			}

			relations := 0
			for _, resource := range resources {
				relations += len(resource.Relations)
			}

			if relations == 0 {
				t.Errorf("%s: the sample holds no relation, want at least one: the report lists each "+
					"resource's relations", file.name)
			}
		})
	}
}

// TestSamplesUseEveryRelationType pins that the samples together use every type
// of the closed relation vocabulary (docs/adr/0025), so the walkthrough shows
// each of them. A type no sample uses fails here, whichever one is forgotten.
func TestSamplesUseEveryRelationType(t *testing.T) {
	t.Parallel()

	used := make(map[inventory.RelationType]bool)

	for _, file := range sampleFiles(t) {
		for _, resource := range loadSample(t, file.path).Resources() {
			for _, relation := range resource.Relations {
				used[relation.Type] = true
			}
		}
	}

	var usedTypes []inventory.RelationType

	for _, relationType := range relationVocabulary {
		if used[relationType] {
			usedTypes = append(usedTypes, relationType)
		}
	}

	if diff := cmp.Diff(relationVocabulary, usedTypes); diff != "" {
		t.Errorf("the sample inventories together do not use every relation type; add a relation of "+
			"each missing type (declared minus used):\n%s", diff)
	}
}

// TestSampleMonthlyCostsArePartial pins that every sample carries the source's
// monthly cost on some resources but not on all: the report then shows the
// source's cost next to the estimate and says how many resources it covers, so
// a sample with none hides that line and one with all of them hides its
// covered-of-total wording.
func TestSampleMonthlyCostsArePartial(t *testing.T) {
	t.Parallel()

	for _, file := range sampleFiles(t) {
		t.Run(file.name, func(t *testing.T) {
			t.Parallel()

			resources := loadSample(t, file.path).Resources()

			costed := 0

			for _, resource := range resources {
				if resource.MonthlyCost != nil {
					costed++
				}
			}

			switch {
			case costed == 0:
				t.Errorf("%s: no resource gives a monthlyCost, want some: the report shows the source's "+
					"cost next to the estimate", file.name)
			case costed == len(resources):
				t.Errorf("%s: every resource gives a monthlyCost, want some but not all: the report says "+
					"how many resources the source's cost covers", file.name)
			}
		})
	}
}

// sampleReportResource is the part of a JSON report's resource this file checks:
// its category and its estimate.
type sampleReportResource struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Estimate *struct {
		Estimated bool   `json:"estimated"`
		Amount    string `json:"amount"`
	} `json:"estimate"`
}

// sampleReportDocument is the part of the sample's JSON report this file
// checks: the version and kind, the workloads, and the estimate of the total and
// of each resource.
type sampleReportDocument struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Kind          string                 `json:"kind"`
	Resources     []sampleReportResource `json:"resources"`
	Workloads     []struct {
		Name       string `json:"name"`
		NoWorkload bool   `json:"noWorkload"`
		Resources  int    `json:"resources"`
	} `json:"workloads"`
	Estimate *struct {
		Region       string `json:"region"`
		RegionChosen bool   `json:"regionChosen"`
		Total        struct {
			Amount    string `json:"amount"`
			Estimated int    `json:"estimated"`
			Resources int    `json:"resources"`
		} `json:"total"`
	} `json:"estimate"`
}

// TestSampleReportVariants pins the reports the walkthrough and the guide quote
// beside the default one: the AWS sample against Scaleway with the estimates
// off, the same sample with a region chosen, and its JSON document. Each is
// rendered through the path the CLI's assess command uses and kept as a golden
// file, regenerated with `go test ./test/examples -update`, so a catalog
// snapshot or a sample change shows up in the diff of testdata/.
func TestSampleReportVariants(t *testing.T) {
	t.Parallel()

	inventoryPath := samplePath("aws")
	meta := analysis.ReportMeta{InventoryName: filepath.Base(inventoryPath)}

	tests := []struct {
		// name is the subtest's name.
		name string
		// golden is the path of the golden file, from this package.
		golden string
		// render writes the report the CLI would write for the variant.
		render func(t *testing.T) []byte
		// want lists text the report must hold: the lines that make the
		// variant what it is, which a golden file alone cannot pin after a
		// careless -update.
		want []string
		// unwanted lists text the report must not hold.
		unwanted []string
		// check makes the assertions the golden file cannot: the JSON
		// document's own structure and its schema.
		check func(t *testing.T, report []byte)
	}{
		{
			name:   "markdown report against scaleway with the estimates off",
			golden: "testdata/aws-scaleway-no-estimate.md.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				return runSingleWith(t, inventoryPath, scaleway.New(), markdown.New(),
					analysis.Options{Estimate: false}, meta)
			},
			want:     []string{"## Resources", "## Workloads"},
			unwanted: []string{"€", "| Plan |", "| Monthly estimate |", "## Cost estimate"},
		},
		{
			name:   "markdown report against scaleway in a chosen region",
			golden: "testdata/aws-scaleway-nl-ams.md.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				return runSingleWith(t, inventoryPath, scaleway.New(), markdown.New(),
					analysis.Options{Region: regionChoice, Estimate: true}, meta)
			},
			want: []string{
				"- Region: `" + regionChoice + "` (chosen with `--region`)",
				"## Workloads",
				"## Cost estimate",
			},
		},
		{
			name:   "json report against scaleway with estimates",
			golden: "testdata/aws-scaleway-estimates.json.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				return runSingleWith(t, inventoryPath, scaleway.New(), jsonreport.New(),
					analysis.Options{Estimate: true}, meta)
			},
			check: checkSampleReportDocument,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := test.render(t)
			if len(got) == 0 {
				t.Fatalf("the sample %s rendered no report for %q", inventoryPath, test.name)
			}

			report := string(got)

			for _, want := range test.want {
				if !strings.Contains(report, want) {
					t.Errorf("the report does not hold %q:\n%s", want, report)
				}
			}

			for _, unwanted := range test.unwanted {
				if strings.Contains(report, unwanted) {
					t.Errorf("the report holds %q, which belongs to another variant:\n%s", unwanted, report)
				}
			}

			if test.check != nil {
				test.check(t, got)
			}

			testutil.Golden(t, test.golden, got)
		})
	}
}

// checkSampleReportDocument fails unless the document is a schema version 2
// assessment of the sized sample: it carries workloads and estimates, each
// priced category the sample holds has an estimated resource, and the document
// matches the schema the jsonreport package publishes for it.
//
// The document is decoded here, into structs of this package, rather than read
// from the golden file: -update rewrites the golden, so only an assertion of
// its own keeps the sample from losing what milestone 9 documents.
func checkSampleReportDocument(t *testing.T, report []byte) {
	t.Helper()

	var doc sampleReportDocument
	if err := json.Unmarshal(report, &doc); err != nil {
		t.Fatalf("the report is not a JSON document: %v", err)
	}

	if doc.SchemaVersion != 2 {
		t.Errorf("schemaVersion = %d, want 2 (docs/adr/0030)", doc.SchemaVersion)
	}

	if doc.Kind != "assessment" {
		t.Errorf("kind = %q, want %q", doc.Kind, "assessment")
	}

	if len(doc.Workloads) == 0 {
		t.Errorf("the document holds no workload, want the sample's workloads:\n%s", report)
	}

	if doc.Estimate == nil {
		t.Fatalf("the document holds no estimate, want one: the sample's resources are sized\n%s", report)
	}

	if doc.Estimate.Total.Estimated == 0 {
		t.Errorf("the estimate total counts %d estimated resource(s) of %d, want at least one: "+
			"the sample's resources are sized", doc.Estimate.Total.Estimated, doc.Estimate.Total.Resources)
	}

	if doc.Estimate.RegionChosen {
		t.Errorf("the estimate says region %q was chosen, but the run chose no region: it estimates in "+
			"the target's default region", doc.Estimate.Region)
	}

	estimated := make(map[string]bool)

	for _, resource := range doc.Resources {
		if resource.Estimate != nil && resource.Estimate.Estimated {
			estimated[resource.Category] = true
		}
	}

	for _, category := range taxonomy.Categories() {
		if _, priced := pricedSizes[category]; !priced {
			continue
		}

		if !slices.ContainsFunc(doc.Resources, func(r sampleReportResource) bool {
			return r.Category == string(category)
		}) {
			continue
		}

		if !estimated[string(category)] {
			t.Errorf("no %s resource is estimated, though the sample holds one: give it a size a plan "+
				"covers", category)
		}
	}

	validateSampleReportSchema(t, report)
}

// validateSampleReportSchema fails unless the document matches the JSON report
// schema the jsonreport package publishes for a single target, schema version 2.
// The validator is the one the repository shares (internal/testutil), so no
// third-party dependency is added.
func validateSampleReportSchema(t *testing.T, report []byte) {
	t.Helper()

	data, err := os.ReadFile(assessmentSchema)
	if err != nil {
		t.Fatalf("read the report schema %s: %v", assessmentSchema, err)
	}

	_, schema, err := testutil.ParseSchema(data)
	if err != nil {
		t.Fatalf("parse the report schema %s: %v", assessmentSchema, err)
	}

	schema.ValidateJSON(t, report)
}
