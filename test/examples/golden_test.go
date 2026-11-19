package examples_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// TestSampleReports pins the walkthrough's quoted output as golden files
// (issue #179): the Markdown report of the AWS sample against Scaleway, the
// Markdown comparison of the same sample against every target, and the JSON
// report against Scaleway. Each is rendered through the path the CLI's assess
// command uses, so a catalog snapshot change or a reporter change shows up in
// the diff of testdata/. Regenerate with `go test ./test/examples -update`.
func TestSampleReports(t *testing.T) {
	t.Parallel()

	inventoryPath := samplePath("aws")
	meta := analysis.ReportMeta{InventoryName: filepath.Base(inventoryPath)}

	tests := []struct {
		name   string
		golden string
		render func(t *testing.T) []byte
	}{
		{
			name:   "markdown report against scaleway",
			golden: "testdata/aws-scaleway.md.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				return runSingle(t, inventoryPath, scaleway.New(), markdown.New(), meta)
			},
		},
		{
			name:   "markdown comparison against every target",
			golden: "testdata/aws-all.md.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				return runComparison(t, inventoryPath, markdown.New(), meta)
			},
		},
		{
			name:   "json report against scaleway",
			golden: "testdata/aws-scaleway.json.golden",
			render: func(t *testing.T) []byte {
				t.Helper()

				return runSingle(t, inventoryPath, scaleway.New(), jsonreport.New(jsonreport.Version2), meta)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testutil.Golden(t, test.golden, test.render(t))
		})
	}
}

// runSingle renders what the CLI writes for `assess --target <one target>`:
// the analysis use case over the real inventory file and catalog, through rep.
func runSingle(t *testing.T, inventoryPath string, catalog analysis.CatalogSource, rep analysis.Reporter,
	meta analysis.ReportMeta,
) []byte {
	t.Helper()

	useCase, err := analysis.New(inventoryfile.New(inventoryPath), catalog, rep)
	if err != nil {
		t.Fatalf("analysis.New: %v", err)
	}

	var report bytes.Buffer

	if err := useCase.Run(t.Context(), &report, meta); err != nil {
		t.Fatalf("assess %s: %v", inventoryPath, err)
	}

	return report.Bytes()
}

// runComparison renders what the CLI writes for `assess --target all`: the
// comparison use case over the real inventory file and every target's catalog,
// through rep.
func runComparison(t *testing.T, inventoryPath string, rep analysis.ComparisonReporter,
	meta analysis.ReportMeta,
) []byte {
	t.Helper()

	selected := make([]analysis.Target, 0, len(targets))
	for _, target := range targets {
		selected = append(selected, analysis.Target{Key: target.key, Catalog: target.catalog()})
	}

	comparison, err := analysis.NewComparison(inventoryfile.New(inventoryPath), selected, rep)
	if err != nil {
		t.Fatalf("analysis.NewComparison: %v", err)
	}

	var report bytes.Buffer

	if err := comparison.Run(t.Context(), &report, meta); err != nil {
		t.Fatalf("compare %s against every target: %v", inventoryPath, err)
	}

	return report.Bytes()
}
