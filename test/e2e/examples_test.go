//go:build e2e

package e2e_test

// This file runs the built binary on the sample inventories under examples/,
// the files the walkthrough and a demo use, and pins each report as a golden
// file. It is the end-to-end half of PRD #190 milestone 9: the samples move to
// inventory format version 2 with sizes, workloads, relations and a monthly
// cost, so a report of a sample must carry the Workloads section and a non-zero
// estimate total, which a version 1 sample cannot produce.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/testutil"
)

const (
	// samplesDir is the directory of sample inventories, from this package.
	samplesDir = "../../examples"
	// sampleRegion is the region one sample is assessed in, so the golden shows
	// the header line --region adds. The Scaleway catalog lists fr-par, it-mil,
	// nl-ams and pl-waw.
	sampleRegion = "nl-ams"
	// workloadsHeading is the heading that starts the per-workload section,
	// which a report with a workload has.
	workloadsHeading = "## Workloads"
	// costHeading is the heading that starts a single-target report's cost
	// summary, which a report with estimates has. A comparison gives it per
	// target, one heading level lower.
	costHeading = "## Cost estimate"
	// estimateTotalPrefix is the wording of every total, single-target or in a
	// comparison's summary: "EUR 1,234.56 per month for 9 of 13 resources".
	estimateTotalPrefix = " per month for "
	// noEstimateTotal is the count of a total that estimates no resource, which
	// a version 1 sample, whose resources are not sized, produces.
	noEstimateTotal = " per month for 0 of "
)

// sampleSources are the source directories under examples/, one per
// hyperscaler: each holds an inventory.json the walkthrough assesses.
var sampleSources = []string{"aws", "azure", "gcp"}

// sampleInventoryPath returns the sample inventory of source, from this package.
func sampleInventoryPath(source string) string {
	return filepath.Join(samplesDir, source, "inventory.json")
}

// TestExamplesAssess pins the report of every sample end to end: the built
// binary, run with no environment, assesses examples/<source>/inventory.json
// against Scaleway and against every target, and each run writes the golden file
// beside the other reports. The assertions beyond the golden file are what
// milestone 9 makes of the samples, and what -update must not be able to accept
// away: the report groups the sample's workloads and estimates at least one
// resource, so the sample is sized.
func TestExamplesAssess(t *testing.T) {
	t.Parallel()

	for _, source := range sampleSources {
		tests := []struct {
			name   string
			target string
			golden string
		}{
			{
				name:   "single target",
				target: scalewayKey,
				golden: "examples-" + source + "-scaleway.golden",
			},
			{
				name:   "comparison",
				target: allKey,
				golden: "examples-" + source + "-all.golden",
			},
		}

		for _, tc := range tests {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				got := assess(t, tc.target, sampleInventoryPath(source))
				if got.code != 0 {
					t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
				}

				if got.stderr != "" {
					t.Errorf("stderr = %q, want empty", got.stderr)
				}

				if !strings.Contains(got.stdout, workloadsHeading) {
					t.Errorf("the report holds no %q section: the sample names workloads, so the report "+
						"groups its resources per workload:\n%s", workloadsHeading, got.stdout)
				}

				// The report with estimates on sums the sized resources: without
				// a total it estimated nothing, and a total that estimates no
				// resource means the sample is not sized.
				if !strings.Contains(got.stdout, estimateTotalPrefix) {
					t.Errorf("the report holds no estimate total (\"... per month for N of M resources\"): "+
						"estimates are on by default, and the sample is sized:\n%s", got.stdout)
				} else if strings.Contains(got.stdout, noEstimateTotal) {
					t.Errorf("the cost summary estimates no resource: the sample gives its priced resources "+
						"a size, so the report shows estimates:\n%s", got.stdout)
				}

				testutil.Golden(t, goldenPath(tc.golden), []byte(got.stdout))
			})
		}
	}
}

// TestExamplesReportVariants pins the report options the guide and the
// walkthrough document, on one sample: a run with the estimates off, a run in a
// region chosen with --region, and the JSON document. The goldens are
// regenerated with `go test ./test/e2e -tags e2e -update`.
func TestExamplesReportVariants(t *testing.T) {
	t.Parallel()

	inventory := sampleInventoryPath("aws")

	t.Run("no estimate", func(t *testing.T) {
		t.Parallel()

		got := assess(t, scalewayKey, inventory, "--no-estimate")
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
		}

		if got.stderr != "" {
			t.Errorf("stderr = %q, want empty", got.stderr)
		}

		// With the estimates off the report still groups the workloads: the
		// switch turns prices off, never fits, workloads or relations.
		if !strings.Contains(got.stdout, workloadsHeading) {
			t.Errorf("the report of a run with --no-estimate holds no %q section: the sample names "+
				"workloads:\n%s", workloadsHeading, got.stdout)
		}

		// The sample carries the source's own monthly cost in USD, which the run
		// shows beside the estimates only: with them off, neither the label nor
		// the currency may appear.
		for _, unwanted := range []string{"€", "$", "Source cost", "| Plan |", "| Monthly estimate |", costHeading} {
			if strings.Contains(got.stdout, unwanted) {
				t.Errorf("the report of a run with --no-estimate holds %q:\n%s", unwanted, got.stdout)
			}
		}

		testutil.Golden(t, goldenPath("examples-aws-scaleway-no-estimate.golden"), []byte(got.stdout))
	})

	t.Run("chosen region", func(t *testing.T) {
		t.Parallel()

		got := assess(t, scalewayKey, inventory, "--region", scalewayKey+"="+sampleRegion)
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
		}

		if got.stderr != "" {
			t.Errorf("stderr = %q, want empty", got.stderr)
		}

		want := "- Region: `" + sampleRegion + "` (chosen with `--region`)"
		if !strings.Contains(got.stdout, want) {
			t.Errorf("the report does not hold %q:\n%s", want, got.stdout)
		}

		if !strings.Contains(got.stdout, workloadsHeading) {
			t.Errorf("the report holds no %q section:\n%s", workloadsHeading, got.stdout)
		}

		testutil.Golden(t, goldenPath("examples-aws-scaleway-nl-ams.golden"), []byte(got.stdout))
	})

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		got := assess(t, scalewayKey, inventory, "--format", jsonFormat)
		if got.code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, got.stderr)
		}

		if got.stderr != "" {
			t.Errorf("stderr = %q, want empty", got.stderr)
		}

		if !json.Valid([]byte(got.stdout)) {
			t.Fatalf("stdout is not valid JSON:\n%s", got.stdout)
		}

		var doc exampleDocument
		if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
			t.Fatalf("decoding the document: %v", err)
		}

		if doc.SchemaVersion != 2 {
			t.Errorf("schemaVersion = %d, want 2 (docs/adr/0030)", doc.SchemaVersion)
		}

		if doc.Kind != "assessment" {
			t.Errorf("kind = %q, want %q", doc.Kind, "assessment")
		}

		if len(doc.Workloads) == 0 {
			t.Errorf("the document holds no workload, want the sample's workloads:\n%s", got.stdout)
		}

		if doc.Estimate == nil {
			t.Fatalf("the document holds no estimate: the sample is sized\n%s", got.stdout)
		}

		if doc.Estimate.Total.Estimated == 0 {
			t.Errorf("the estimate total counts %d estimated resource(s) of %d, want at least one: "+
				"the sample is sized", doc.Estimate.Total.Estimated, doc.Estimate.Total.Resources)
		}

		testutil.Golden(t, goldenPath("examples-aws-scaleway.json.golden"), []byte(got.stdout))
	})
}

// exampleDocument is the part of a JSON report this file checks: the version
// and kind, the workloads and the estimate total.
type exampleDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Workloads     []struct {
		Name      string `json:"name"`
		Resources int    `json:"resources"`
	} `json:"workloads"`
	Estimate *struct {
		Region string `json:"region"`
		Total  struct {
			Amount    string `json:"amount"`
			Estimated int    `json:"estimated"`
			Resources int    `json:"resources"`
		} `json:"total"`
	} `json:"estimate"`
}
