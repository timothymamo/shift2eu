package examples_test

// This file pins the reports the samples render with estimates, which PRD #190
// milestone 8 makes the CLI's default: one Markdown report per sample against
// Scaleway and one comparison against every target, for the samples as they are
// in examples/ today (inventory format version 1). Milestone 9 moves the samples
// to version 2 and regenerates these goldens beside the walkthrough's.

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// TestSampleReportsWithEstimates pins the report of every sample with the
// estimates a run without --no-estimate writes: the single-target Markdown
// report against Scaleway and the Markdown comparison against every target, both
// rendered through the path the CLI's assess command uses. A golden is left for
// `go test ./test/examples -update` wherever it does not exist yet.
func TestSampleReportsWithEstimates(t *testing.T) {
	t.Parallel()

	for _, file := range sampleFiles(t) {
		meta := analysis.ReportMeta{InventoryName: filepath.Base(file.path)}

		tests := []struct {
			name   string
			golden string
			render func(t *testing.T) []byte
		}{
			{
				name:   "markdown report against scaleway, with estimates",
				golden: "testdata/" + file.source + "-scaleway-estimates.md.golden",
				render: func(t *testing.T) []byte {
					t.Helper()

					return runSingleWith(t, file.path, scaleway.New(), markdown.New(),
						analysis.Options{Estimate: true}, meta)
				},
			},
			{
				name:   "markdown comparison against every target, with estimates",
				golden: "testdata/" + file.source + "-all-estimates.md.golden",
				render: func(t *testing.T) []byte {
					t.Helper()

					return runComparisonWith(t, file.path, markdown.New(),
						analysis.ComparisonOptions{Estimate: true}, meta)
				},
			},
		}

		for _, test := range tests {
			t.Run(file.name+"/"+test.name, func(t *testing.T) {
				t.Parallel()

				got := test.render(t)
				if len(got) == 0 {
					t.Fatalf("the sample %s rendered no report for %q", file.name, test.name)
				}

				testutil.Golden(t, test.golden, got)
			})
		}
	}
}

// runSingleWith renders what the CLI writes for `assess --target <one target>`
// with opts: the analysis use case over the real inventory file and catalog,
// through rep.
func runSingleWith(t *testing.T, inventoryPath string, catalog analysis.CatalogSource, rep analysis.Reporter,
	opts analysis.Options, meta analysis.ReportMeta,
) []byte {
	t.Helper()

	useCase, err := analysis.New(inventoryfile.New(inventoryPath), catalog, rep)
	if err != nil {
		t.Fatalf("analysis.New: %v", err)
	}

	var report bytes.Buffer

	if err := useCase.RunWith(t.Context(), &report, meta, opts); err != nil {
		t.Fatalf("assess %s: %v", inventoryPath, err)
	}

	return report.Bytes()
}

// runComparisonWith renders what the CLI writes for `assess --target all` with
// opts: the comparison use case over the real inventory file and every target's
// catalog, through rep.
func runComparisonWith(t *testing.T, inventoryPath string, rep analysis.ComparisonReporter,
	opts analysis.ComparisonOptions, meta analysis.ReportMeta,
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

	if err := comparison.RunWith(t.Context(), &report, meta, opts); err != nil {
		t.Fatalf("compare %s against every target: %v", inventoryPath, err)
	}

	return report.Bytes()
}
