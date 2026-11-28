// This file holds the PDF reporter against the corpus the scheduled security
// audit runs the released CLI on (PRD #175, ADR 0040, issue #661): every file of
// the corpus is untrusted input. The inventory decoder refuses some of them with
// a controlled error and accepts the rest, and an inventory it accepts must go
// through the whole assess path and come out as a PDF whose structural
// invariants hold: the single-target report of the Scaleway target and the
// comparison of all three embedded targets.
//
// The set of files is derived, never written down: the parent decodes every file
// of the corpus and runs the subtests on the ones the decoder accepts, so a file
// added to the corpus is covered without a change here, and a file that starts
// loading cannot pass unnoticed.
//
// The corpus's half that is refused is the decoder's own business, pinned by
// htmlreport's TestHostileCorpusDecisions: what a refusal looks like is not the
// PDF reporter's to state.
package pdfreport_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
)

// auditHostileCorpus is the corpus the scheduled security audit's row 13 copies
// into the container the released CLI reads it in (ADR 0040), relative to this
// package's directory: it holds one JSON inventory per kind of hostility, and
// every value is sanitised (CLAUDE.md rule 2).
const auditHostileCorpus = "../../../../.github/security-audit/testdata/hostile-inventories"

// TestHostileCorpusPDFs pins that every hostile inventory the decoder accepts
// gives a report and a comparison whose structural invariants all hold: the file
// parses, holds only the writer's objects, names, fonts and operators, no name a
// reader would act on, text stays inside its box and no heading ends a page.
func TestHostileCorpusPDFs(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(auditHostileCorpus)
	if err != nil {
		t.Fatalf("read the audit's hostile corpus: %v", err)
	}

	accepted := 0

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		name := entry.Name()
		data := readCorpusFile(t, name)

		if _, err := inventoryfile.FromBytes(data).Inventory(t.Context()); err != nil {
			// The decoder refuses this file: htmlreport's TestHostileCorpusDecisions
			// pins that refusal, and no report is written for it.
			continue
		}

		accepted++

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("report", func(t *testing.T) {
				t.Parallel()

				checkPDFStructure(t, corpusReport(t, name, data))
			})

			t.Run("comparison", func(t *testing.T) {
				t.Parallel()

				checkPDFStructure(t, corpusComparison(t, name, data))
			})
		})
	}

	if accepted == 0 {
		t.Fatalf("the audit's hostile corpus holds no inventory the decoder accepts, so this test pinned no PDF: %s",
			auditHostileCorpus)
	}
}

// readCorpusFile returns the bytes of the corpus file name.
func readCorpusFile(tb testing.TB, name string) []byte {
	tb.Helper()

	data, err := os.ReadFile(filepath.Join(auditHostileCorpus, name))
	if err != nil {
		tb.Fatalf("read the corpus file %s: %v", name, err)
	}

	return data
}

// corpusReport runs the whole assess path of the Scaleway target on data, an
// inventory document the decoder accepts, and returns the PDF report it
// renders: the inventory is read by the inventory-file adapter, the catalog is
// the target's embedded snapshot, and estimates are on, as they are by default.
func corpusReport(tb testing.TB, name string, data []byte) []byte {
	tb.Helper()

	a, err := analysis.New(inventoryfile.FromBytes(data), scaleway.New(), pdfreport.New(versionLine))
	if err != nil {
		tb.Fatalf("analysis.New() with the inventory %s returned unexpected error: %v", name, err)
	}

	var out bytes.Buffer
	if err := a.RunWith(tb.Context(), &out, analysis.ReportMeta{InventoryName: name}, analysis.Options{Estimate: true}); err != nil {
		tb.Fatalf("the report of the inventory %s returned unexpected error: %v", name, err)
	}

	return out.Bytes()
}

// corpusComparison runs the whole comparison path on data: the same inventory
// assessed against the three embedded targets, with estimates on, rendered as
// the comparison PDF.
func corpusComparison(tb testing.TB, name string, data []byte) []byte {
	tb.Helper()

	targets := []analysis.Target{
		{Key: ovhcloudKey, Catalog: ovhcloud.New()},
		{Key: scalewayKey, Catalog: scaleway.New()},
		{Key: upcloudKey, Catalog: upcloud.New()},
	}

	c, err := analysis.NewComparison(inventoryfile.FromBytes(data), targets, pdfreport.New(versionLine))
	if err != nil {
		tb.Fatalf("analysis.NewComparison() with the inventory %s returned unexpected error: %v", name, err)
	}

	var out bytes.Buffer
	if err := c.RunWith(tb.Context(), &out, analysis.ReportMeta{InventoryName: name}, analysis.ComparisonOptions{Estimate: true}); err != nil {
		tb.Fatalf("the comparison of the inventory %s returned unexpected error: %v", name, err)
	}

	return out.Bytes()
}
