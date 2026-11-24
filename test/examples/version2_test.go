package examples_test

// This file pins what PRD #190 milestone 9 makes of the sample inventories under
// examples/: they move to inventory format version 2 (docs/adr/0025) with a
// size on every priced resource, at least two named workloads, relations from
// the whole closed vocabulary, and the source's monthly cost on some resources
// only, so the reports show a partial coverage. It also pins the reports the
// walkthrough and the guide quote beside the default ones: one sample with the
// estimates off, one with a region chosen, and the JSON report in schema
// versions 2 and 3, each checked against the schema jsonreport publishes for
// it.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
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

	// assessmentSchemaName is the JSON report schema a sample's document must
	// match, from this package's directory, with the document's schemaVersion in
	// its name: the single-target schema of that version (docs/adr/0018,
	// docs/adr/0030). Version 2 is the frozen format the CLI writes with
	// --schema-version 2, version 3 the one it writes by default.
	assessmentSchemaName = repoRoot + "/internal/adapter/report/jsonreport/schema/assessment-v%d.schema.json"
)

// pricedSize says which size fields the estimate prices a category by (PRD
// #190): vcpus and memoryGiB for a category priced by plan, memoryGiB alone for
// cache.redis, matched on memory alone (ADR 0045), storageGiB for one priced by
// stored GiB. A category that is not in pricedSizes is not priced at all:
// messaging.queue, messaging.topic, serverless.function and
// serverless.container are priced by usage, and network.loadbalancer,
// registry.container and unclassified never are.
//
// They are not the only fields a category may give: docs/adr/0025 lets every
// compute, Kubernetes and database resource give every size field, so a sample
// may carry storageGiB beside the plan's vcpus and memoryGiB. The pin requires
// the fields below and judges no other size field (sizeGaps).
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
	taxonomy.DatabaseMongoDB:    {vcpus: true, memory: true},
	taxonomy.CacheRedis:         {memory: true},
	taxonomy.StorageBlock:       {storage: true},
	taxonomy.StorageObject:      {storage: true},
	taxonomy.StorageFile:        {storage: true},
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

			for _, gap := range sizeGaps(inv) {
				if gap.Field == "" {
					priced, _ := pricedSizes[gap.Category]
					t.Errorf("%s: resource %q (%s) has no size; a priced category is priced by %s",
						file.name, gap.ID, gap.Category, sizeFields(priced))

					continue
				}

				t.Errorf("%s: resource %q (%s) gives no %s; its category is priced by that field",
					file.name, gap.ID, gap.Category, gap.Field)
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

// sizeGap is one size a priced resource of a sample does not give the way the
// estimate reads it: the resource's ID and category, and the field's path as the
// inventory file spells it (empty when the resource gives no size at all).
type sizeGap struct {
	ID       string
	Category taxonomy.Category
	Field    string
}

// sizeGaps returns a gap for every priced resource that leaves out a size field
// its category is priced by, in the inventory's order.
//
// Which size fields a category takes is not decided here. docs/adr/0025 makes
// that the domain's rule (inventory.New, inventory.ErrFieldNotForCategory), and
// the reader a sample is decoded through enforces it, so a sample that gives a
// field its category does not take never loads. A second table here would refuse
// what the format allows - storageGiB on a database above all - so the pin judges
// no field but the ones the estimate needs, which the reader cannot know.
func sizeGaps(inv inventory.Inventory) []sizeGap {
	var gaps []sizeGap

	for _, resource := range inv.Resources() {
		priced, ok := pricedSizes[resource.Category]
		if !ok {
			continue
		}

		if resource.Size == nil {
			gaps = append(gaps, sizeGap{ID: resource.ID, Category: resource.Category})

			continue
		}

		for _, field := range []struct {
			name   string
			priced bool
			value  *float64
		}{
			{name: "vcpus", priced: priced.vcpus, value: resource.Size.VCPUs},
			{name: "memoryGiB", priced: priced.memory, value: resource.Size.MemoryGiB},
			{name: "storageGiB", priced: priced.storage, value: resource.Size.StorageGiB},
		} {
			if field.priced && field.value == nil {
				gaps = append(gaps, sizeGap{
					ID:       resource.ID,
					Category: resource.Category,
					Field:    "size." + field.name,
				})
			}
		}
	}

	return gaps
}

// TestSizePinFollowsTheFormat pins that the sizing pin reads a document the way
// docs/adr/0025 writes it: every size field the format allows a category is not
// a gap. storageGiB on a database is a real size - the instance's disk, which the
// estimate prices above what the plan includes (docs/adr/0027) - and so is
// storageGiB on a compute instance or a Kubernetes node (the local disk, per
// instance or per node). A field a priced category is priced by and the resource
// leaves out is a gap, so the pin still catches a sample that shows no estimate.
func TestSizePinFollowsTheFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name is the subtest's name.
		name string
		// id is the resource's ID, as the document writes it.
		id string
		// category is the resource's category, as the document writes it.
		category taxonomy.Category
		// size is the document's size object without the braces, or "" for a
		// resource that gives none.
		size string
		// want lists the gaps the pin must report.
		want []sizeGap
	}{
		{
			name:     "database sized by plan and disk",
			id:       "shop-db",
			category: taxonomy.DatabasePostgreSQL,
			size:     `"vcpus": 2, "memoryGiB": 8, "storageGiB": 50`,
		},
		{
			name:     "mysql database sized by plan and disk",
			id:       "shop-legacy-mysql",
			category: taxonomy.DatabaseMySQL,
			size:     `"vcpus": 2, "memoryGiB": 8, "storageGiB": 100`,
		},
		{
			name:     "compute instance sized by plan and local disk",
			id:       "shop-web-1",
			category: taxonomy.ComputeVM,
			size:     `"vcpus": 2, "memoryGiB": 8, "storageGiB": 100`,
		},
		{
			name:     "kubernetes node sized by plan and local disk",
			id:       "shop-eks",
			category: taxonomy.KubernetesManaged,
			size:     `"vcpus": 4, "memoryGiB": 16, "storageGiB": 50, "count": 3`,
		},
		{
			name:     "compute instance with no vcpus",
			id:       "shop-web-1",
			category: taxonomy.ComputeVM,
			size:     `"memoryGiB": 8`,
			want: []sizeGap{
				{ID: "shop-web-1", Category: taxonomy.ComputeVM, Field: "size.vcpus"},
			},
		},
		{
			name:     "database with no size",
			id:       "shop-db",
			category: taxonomy.DatabaseMySQL,
			want: []sizeGap{
				{ID: "shop-db", Category: taxonomy.DatabaseMySQL},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sizeGaps(sizedSample(t, tc.id, tc.category, tc.size))

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("sizeGaps(%s) mismatch (-want +got):\n%s", tc.name, diff)
			}
		})
	}
}

// sizedSample decodes a one-resource version 2 document whose resource has the
// size object written as size, through the real inventory-file adapter, the one
// cmd/shift2eu wires.
func sizedSample(t *testing.T, id string, category taxonomy.Category, size string) inventory.Inventory {
	t.Helper()

	doc := `{"schemaVersion": 2, "source": "aws", "resources": [{"id": "` + id + `", "category": "` +
		string(category) + `"`

	if size != "" {
		doc += `, "size": { ` + size + ` }`
	}

	doc += `}]}`

	inv, err := inventoryfile.FromBytes([]byte(doc)).Inventory(t.Context())
	if err != nil {
		t.Fatalf("decode the sample document %s: %v", doc, err)
	}

	return inv
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
	Fit      string `json:"fit"`
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
// off, the same sample with a region chosen, and its JSON document in schema
// version 2 and in the default version 3 (issue #330). Each is rendered through
// the path the CLI's assess command uses and kept as a golden file, regenerated
// with `go test ./test/examples -update`, so a catalog snapshot or a sample
// change shows up in the diff of testdata/.
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
			want: []string{"## Resources", "## Workloads"},
			// The sample carries the source's own monthly cost in USD, which the
			// report shows beside the estimates only: with them off, neither
			// the label nor the currency may appear.
			unwanted: []string{"€", "$", "Source cost", "| Plan |", "| Monthly estimate |", "## Cost estimate"},
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

				return runSingleWith(t, inventoryPath, scaleway.New(), jsonreport.New(jsonreport.Version2),
					analysis.Options{Estimate: true}, meta)
			},
			check: func(t *testing.T, report []byte) {
				t.Helper()

				checkSampleReportDocument(t, report, jsonreport.Version2)
			},
		},
		{
			name:   "json report against scaleway with estimates in the default schema version",
			golden: "testdata/aws-scaleway-estimates.v3.json.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				// The version the CLI writes when --schema-version names none:
				// the application selects it, exactly as the CLI asks it to, so
				// the test does not import internal/cli (issue #330).
				version, err := analysis.SelectSchemaVersion(analysis.JSONFormat, "", false)
				if err != nil {
					t.Fatalf("selecting the default schema version of %q: %v", analysis.JSONFormat, err)
				}

				return runSingleWith(t, inventoryPath, scaleway.New(),
					jsonreport.New(version), analysis.Options{Estimate: true}, meta)
			},
			check: func(t *testing.T, report []byte) {
				t.Helper()

				// The default is version 3 (issue #307): a default moved to
				// another version fails here, in the schemaVersion the document
				// carries, not only in the golden file.
				checkSampleReportDocument(t, report, jsonreport.Version3)
			},
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

// checkSampleReportDocument fails unless the document is a schema version
// wantVersion assessment of the sized sample: it carries workloads and
// estimates, each priced category the sample holds has an estimated resource,
// and the document matches the schema the jsonreport package publishes for that
// version.
//
// The document is decoded here, into structs of this package, rather than read
// from the golden file: -update rewrites the golden, so only an assertion of
// its own keeps the sample from losing what milestone 9 documents.
func checkSampleReportDocument(t *testing.T, report []byte, wantVersion int) {
	t.Helper()

	var doc sampleReportDocument
	if err := json.Unmarshal(report, &doc); err != nil {
		t.Fatalf("the report is not a JSON document: %v", err)
	}

	if doc.SchemaVersion != wantVersion {
		t.Errorf("schemaVersion = %d, want %d (docs/adr/0018, docs/adr/0030)",
			doc.SchemaVersion, wantVersion)
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

		// Only a category the target serves can be estimated: a resource of fit
		// none (a category the snapshot has no offering for yet, such as the
		// ones PRD #552 adds before their catalog milestones) is "no fit".
		if !slices.ContainsFunc(doc.Resources, func(r sampleReportResource) bool {
			return r.Category == string(category) && r.Fit != string(mapping.None)
		}) {
			continue
		}

		if !estimated[string(category)] {
			t.Errorf("no %s resource is estimated, though the sample holds one with a fit: give it a size "+
				"a plan covers", category)
		}
	}

	validateSampleReportSchema(t, report, wantVersion)
}

// validateSampleReportSchema fails unless the document matches the JSON report
// schema the jsonreport package publishes for a single target in schema version
// version. The validator is the one the repository shares (internal/testutil),
// so no third-party dependency is added.
func validateSampleReportSchema(t *testing.T, report []byte, version int) {
	t.Helper()

	path := fmt.Sprintf(assessmentSchemaName, version)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the report schema %s: %v", path, err)
	}

	_, schema, err := testutil.ParseSchema(data)
	if err != nil {
		t.Fatalf("parse the report schema %s: %v", path, err)
	}

	schema.ValidateJSON(t, report)
}
