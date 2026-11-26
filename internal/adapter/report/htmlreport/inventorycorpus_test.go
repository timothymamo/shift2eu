// The tests in this file hold the whole assess path against the corpus the
// scheduled security audit runs the released CLI on (PRD #175, ADR 0040,
// issue #661): every file of the corpus is untrusted input, so the inventory
// decoder either refuses it with a controlled error or accepts it, and an
// accepted inventory must go through the assessment of all three embedded
// targets and through all three reporters without an error.
//
// They live in this package's test files, whose imports depguard leaves free,
// because the HTML reporter is one of the three renderers under test and
// web-imported-only-by-the-web-root refuses an import of it everywhere but here,
// internal/web and the site's composition root. The inventory decoder has no
// test of its own for this: its package's tests may not import this reporter.
//
// They are also the corpus's regression test: TestHostileCorpusDecisions pins
// which files load and which are refused, and why, so a new file cannot become
// a refused one without a decision here; TestHostileCorpusReports pins what the
// Markdown, JSON and HTML reports do with hostile text; and
// FuzzInventoryDecoding runs the same path on arbitrary bytes, seeded with the
// corpus, so the scheduled audit's fuzz step finds the decoder (ADR 0040).
package htmlreport_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/adapter/report/htmlreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// auditHostileCorpus is the corpus the scheduled security audit's row 13 copies
// into the container the released CLI reads it in (ADR 0040), relative to this
// package's directory: it holds one JSON inventory per kind of hostility, and
// every value is sanitised (CLAUDE.md rule 2). The corpus is committed once, for
// the audit and for these tests.
const auditHostileCorpus = "../../../../.github/security-audit/testdata/hostile-inventories"

// inventoryFixtures is the inventory-file adapter's own fixture directory,
// relative to this package's directory: the version 1 and version 2 inventories
// the decoder is tested with, which seed FuzzInventoryDecoding besides the
// corpus. The path is spelled out because the fixture belongs to the package
// under test and is read here, never copied.
const inventoryFixtures = "../../../adapter/source/inventoryfile/testdata"

// corpusDecision is what the decoder must do with one corpus file: refuse it,
// wrapping the sentinel wantErr, or, when wantErr is nil, accept it.
type corpusDecision struct {
	file string
	// wantErr is the sentinel a refusal wraps; nil for a file that loads.
	wantErr error
	// why says what the file holds and, for a refusal, which check stops it.
	why string
}

// corpusDecisions is the decision for every file of the corpus, in the order a
// reader of the corpus meets them.
//
// A file that loads carries hostile text that no rule of the decoder or of the
// domain refuses: printable markup (Markdown and HTML metacharacters, in every
// free-text field), Unicode that only looks hostile (homoglyphs, combining
// marks, right-to-left letters, private-use and astral code points, a
// no-break space inside a value), values exactly at text.MaxLen bytes, and
// version 2 numbers at the ends of their ranges. A file that is refused holds
// one defect, refused by the check named in why: the corpus's other half, which
// pins that a refusal is controlled, one line and wrapping a sentinel.
//
// Two kinds of hostility are refused wherever they appear and so have no file
// that loads: any character text.Validate refuses (control characters, the bidi
// overrides and other format characters, the line and paragraph separators,
// variation selectors, noncharacters, invalid UTF-8) in a free-text field, and a
// document that is not strict JSON. The decoder accepts no field but the
// free-text ones inventory.New validates (source, id, rawType, region, workload,
// a relation's target, three upper-case letters of a currency), and the format
// has no tags, so an inventory that passes loading cannot carry such a character
// anywhere.
var corpusDecisions = []corpusDecision{
	{
		file: "html-metacharacters.json",
		why:  "HTML metacharacters (< > & \" ') and script, style and attribute-injection payloads in the source and in every free-text field of a resource",
	},
	{
		file: "length-limits.json",
		why:  "every free-text value exactly text.MaxLen (2048) bytes, of markup characters: < in a raw type, | in a region, ` in a workload",
	},
	{
		file: "markdown-metacharacters.json",
		why:  "Markdown metacharacters (| * _ ` # [ ] ( ) ~ \\ !) in the source and in every free-text field: table rows, code fences, links, images, headings, blockquotes, lists, footnotes and reference definitions",
	},
	{
		file: "markup-injection.json",
		why:  "Markdown and HTML syntax in IDs, raw types, workloads and a relation's target",
	},
	{
		file: "number-limits.json",
		why:  "version 2 numbers at the ends of every range inventory.New accepts (a size, a usage and a monthly cost at their maxima, and a size at its minima)",
	},
	{
		file:    "number-overflow.json",
		why:     "a number too large for a float64 (1e400), refused where it is read",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "numbers.json",
		why:     "numbers out of the range of their field, refused by the domain",
		wantErr: inventory.ErrInvalidSize,
	},
	{
		file:    "path-like-names.json",
		why:     "path-like values, one of them refused as invalid text",
		wantErr: text.ErrInvalid,
	},
	{
		file:    "relations-graph.json",
		why:     "relations that point at themselves, at a missing ID and at a repeated ID, refused as a duplicate ID",
		wantErr: inventory.ErrDuplicateID,
	},
	{
		file:    "schema-duplicate-key.json",
		why:     "a key repeated in one object, refused by the strict decoder",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-escaped-keys.json",
		why:     "a key that differs from an exact key by case, refused by the strict decoder",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-lone-surrogate.json",
		why:     "a JSON escape of a lone surrogate, refused by the strict decoder",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-nulls.json",
		why:     "null in the places null is refused: a resource, and a capability",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-prototype-keys.json",
		why:     "prototype-pollution keys, refused as unknown keys by the strict decoder",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-trailing-document.json",
		why:     "a second JSON document after the first, refused by the strict decoder",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-v1-with-v2-key.json",
		why:     "a version 2 key in a version 1 file, refused as unknown",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "schema-version-fraction.json",
		why:     "a schemaVersion that is not the integer 1 or 2",
		wantErr: inventoryfile.ErrSchemaVersion,
	},
	{
		file:    "schema-version-string.json",
		why:     "a schemaVersion that is a string, refused by its type alone",
		wantErr: inventoryfile.ErrSchemaVersion,
	},
	{
		file:    "schema-wrong-types.json",
		why:     "a field of the wrong JSON type, refused by the strict decoder",
		wantErr: inventoryfile.ErrDecode,
	},
	{
		file:    "terminal-escapes.json",
		why:     "terminal escape sequences and control characters in free text, refused by text.Validate",
		wantErr: text.ErrInvalid,
	},
	{
		file: "unicode-lookalikes.json",
		why:  "Unicode that only looks hostile and that text.Validate accepts: Cyrillic homoglyphs, combining marks, Hebrew and Cyrillic letters, a no-break space inside a value, fullwidth markup, a private-use code point and an astral emoji",
	},
	{
		file:    "unicode-tricks.json",
		why:     "a bidi override and other invisible characters in free text, refused by text.Validate",
		wantErr: text.ErrInvalid,
	},
	{
		file:    "unknown-values.json",
		why:     "a category and a capability outside the taxonomy, refused by the domain",
		wantErr: taxonomy.ErrUnknownCategory,
	},
}

// TestHostileCorpusDecisions pins what the decoder does with every file of the
// audit's hostile corpus: the files that load, and the sentinel each refusal
// wraps. It also pins that corpusDecisions lists every JSON file the corpus
// holds, so a file added to the corpus needs a decision here, and a file that
// stops loading cannot pass unnoticed.
func TestHostileCorpusDecisions(t *testing.T) {
	t.Parallel()

	files := hostileCorpusFiles(t)

	decided := make([]string, 0, len(corpusDecisions))
	for _, tc := range corpusDecisions {
		decided = append(decided, tc.file)
	}

	if diff := cmp.Diff(decided, files); diff != "" {
		t.Fatalf("corpusDecisions and the corpus disagree (-decisions +corpus):\n%s", diff)
	}

	for _, tc := range corpusDecisions {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			data := readHostileCorpus(t, tc.file)

			_, err := inventoryfile.FromBytes(data).Inventory(t.Context())
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Inventory() refused a corpus file that must load: %v (%s)", err, tc.why)
				}

				return
			}

			if err == nil {
				t.Fatalf("Inventory() accepted a corpus file that must be refused with %v (%s)", tc.wantErr, tc.why)
			}

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Inventory() error = %v, want a refusal wrapping %v (%s)", err, tc.wantErr, tc.why)
			}

			checkControlledRefusal(t, err)
		})
	}
}

// TestHostileCorpusReports pins what the three reporters do with every corpus
// file that loads: the assessment of the Scaleway target, with estimates on,
// rendered as Markdown, as the version 3 JSON document and as the site's HTML
// fragment, byte for byte against the goldens under testdata (run the test with
// -update to accept a change). The goldens are where the escaping shows: every
// value of the corpus that loads is hostile text that no rule refuses, and none
// of it reaches a report as markup.
func TestHostileCorpusReports(t *testing.T) {
	t.Parallel()

	for _, tc := range corpusDecisions {
		if tc.wantErr != nil {
			continue
		}

		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			data := readHostileCorpus(t, tc.file)

			for _, format := range reportFormats() {
				t.Run(format.name, func(t *testing.T) {
					t.Parallel()

					got, err := renderedReport(t, tc.file, data, scaleway.New(), format.newReporter())
					if err != nil {
						t.Fatalf("%s report of an inventory that loads returned unexpected error: %v", format.name, err)
					}

					checkReport(t, format.name, got)

					testutil.Golden(t, filepath.Join("testdata", "hostile-corpus", tc.file+"."+format.name+".golden"), got)
				})
			}
		})
	}
}

// FuzzInventoryDecoding runs the decoder on arbitrary bytes: whatever it is
// given, it never panics, and a refusal is a controlled error, one line that
// wraps a sentinel. Every inventory it accepts is assessed against all three
// embedded targets, with estimates on, and rendered by all three reporters
// without an error, and what they write is valid UTF-8 (valid JSON, for the JSON
// reporter).
//
// Its seed corpus is the audit's hostile corpus and the inventory-file adapter's
// version 1 and version 2 fixtures, so a plain run of this package's tests runs
// them all as subtests, and the scheduled audit's fuzz step fuzzes the decoder
// from them (ADR 0040, row 14).
func FuzzInventoryDecoding(f *testing.F) {
	for _, name := range hostileCorpusFiles(f) {
		f.Add(readHostileCorpus(f, name))
	}

	for _, name := range []string{"valid.json", "valid-v2.json"} {
		data, err := os.ReadFile(filepath.Join(inventoryFixtures, name))
		if err != nil {
			f.Fatalf("read the inventory fixture %s: %v", name, err)
		}

		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := inventoryfile.FromBytes(data).Inventory(t.Context()); err != nil {
			checkControlledRefusal(t, err)

			return
		}

		for _, target := range embeddedTargets() {
			for _, format := range reportFormats() {
				got, err := renderedReport(t, "inventory.json", data, target.source, format.newReporter())
				if err != nil {
					t.Fatalf("%s: the %s report of an inventory the decoder accepted returned unexpected error: %v",
						target.key, format.name, err)
				}

				checkReport(t, format.name, got)
			}
		}
	})
}

// embeddedTarget is one of the three targets the binary embeds, with the key the
// CLI names it by.
type embeddedTarget struct {
	key    string
	source analysis.CatalogSource
}

// embeddedTargets returns the three embedded catalog targets, in key order, as
// cmd/shift2eu wires them.
func embeddedTargets() []embeddedTarget {
	return []embeddedTarget{
		{key: "ovhcloud", source: ovhcloud.New()},
		{key: "scaleway", source: scaleway.New()},
		{key: "upcloud", source: upcloud.New()},
	}
}

// reportFormat is one of the three reporters, with the name its golden files
// take.
type reportFormat struct {
	name        string
	newReporter func() analysis.Reporter
}

// reportFormats returns the Markdown reporter, the JSON reporter at the version
// a run that names none writes, and this package's HTML reporter, as
// cmd/shift2eu and cmd/shift2eu-web wire them.
func reportFormats() []reportFormat {
	return []reportFormat{
		{name: "markdown", newReporter: func() analysis.Reporter { return markdown.New() }},
		{name: "json", newReporter: func() analysis.Reporter { return jsonreport.New(analysis.DefaultSchemaVersion) }},
		{name: "html", newReporter: func() analysis.Reporter { return htmlreport.New() }},
	}
}

// renderedReport runs the whole assess path of one target on data, an inventory
// document the decoder accepts, and returns what rep wrote: the inventory is
// read by the inventory-file adapter, the catalog is the target's embedded
// snapshot, and estimates are on, as they are by default. The error is whatever
// RunWith returned.
func renderedReport(tb testing.TB, name string, data []byte, cat analysis.CatalogSource, rep analysis.Reporter) ([]byte, error) {
	tb.Helper()

	a, err := analysis.New(inventoryfile.FromBytes(data), cat, rep)
	if err != nil {
		tb.Fatalf("analysis.New() with the inventory %s returned unexpected error: %v", name, err)
	}

	var out bytes.Buffer
	if err := a.RunWith(tb.Context(), &out, analysis.ReportMeta{InventoryName: name}, analysis.Options{Estimate: true}); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// checkReport fails unless a report of an inventory that loads is text a reader
// can show: valid UTF-8, and, for JSON, one valid document holding no raw <, >
// or &, which a JSON encoder escapes so the document cannot be read as markup.
func checkReport(t *testing.T, format string, got []byte) {
	t.Helper()

	if !utf8.Valid(got) {
		t.Fatalf("the %s report is not valid UTF-8:\n%q", format, got)
	}

	if format != "json" {
		return
	}

	if !json.Valid(got) {
		t.Fatalf("the JSON report is not one valid JSON document:\n%s", got)
	}

	if i := strings.IndexAny(string(got), "<>&"); i >= 0 {
		t.Fatalf("the JSON report holds the raw character %q at byte %d, want it escaped, so the document cannot be read as markup:\n%s",
			string(got)[i], i, got)
	}
}

// checkControlledRefusal fails unless err, a refusal of an inventory document,
// is one line that wraps a sentinel of the decoder, the domain inventory or
// taxonomy, or the domain's free-text rule: a refusal a caller can match with
// errors.Is, never a message a value of the document could forge into several
// lines.
func checkControlledRefusal(t *testing.T, err error) {
	t.Helper()

	if msg := err.Error(); strings.ContainsAny(msg, "\r\n") {
		t.Fatalf("the refusal is more than one line: %q", msg)
	}

	if !slices.ContainsFunc(refusalSentinels(), func(sentinel error) bool { return errors.Is(err, sentinel) }) {
		t.Fatalf("the refusal wraps no sentinel a caller can match: %v", err)
	}
}

// refusalSentinels returns every sentinel a refusal of an inventory document
// held in memory wraps: the decoder's own, for the document's size, its JSON and
// its version, and the domain's, for a value it refuses. ErrRead belongs to a
// file and is not among them: no bytes read here reach a file.
func refusalSentinels() []error {
	return []error{
		inventoryfile.ErrTooLarge,
		inventoryfile.ErrDecode,
		inventoryfile.ErrSchemaVersion,
		inventoryfile.ErrTooManyResources,
		inventory.ErrEmptySource,
		inventory.ErrEmptyID,
		inventory.ErrDuplicateID,
		inventory.ErrUnknownRelationType,
		inventory.ErrUnknownRelationTarget,
		inventory.ErrSelfRelation,
		inventory.ErrInvalidSize,
		inventory.ErrInvalidUsage,
		inventory.ErrFieldNotForCategory,
		inventory.ErrInvalidCost,
		taxonomy.ErrUnknownCategory,
		taxonomy.ErrUnknownCapability,
		text.ErrInvalid,
	}
}

// hostileCorpusFiles returns the names of the JSON files of the audit's hostile
// corpus, sorted, and fails when the corpus is missing or holds none.
func hostileCorpusFiles(tb testing.TB) []string {
	tb.Helper()

	entries, err := os.ReadDir(auditHostileCorpus)
	if err != nil {
		tb.Fatalf("read the audit's hostile corpus: %v", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		files = append(files, e.Name())
	}

	if len(files) == 0 {
		tb.Fatalf("the audit's hostile corpus %s holds no JSON file", auditHostileCorpus)
	}

	slices.Sort(files)

	return files
}

// readHostileCorpus returns the bytes of the corpus file name.
func readHostileCorpus(tb testing.TB, name string) []byte {
	tb.Helper()

	data, err := os.ReadFile(filepath.Join(auditHostileCorpus, name))
	if err != nil {
		tb.Fatalf("read the corpus file %s: %v", name, err)
	}

	return data
}
