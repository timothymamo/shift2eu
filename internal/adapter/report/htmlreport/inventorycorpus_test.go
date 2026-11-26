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
// Markdown, JSON and HTML reports do with hostile text; TestHostileInventoryNames
// pins what they do with a hostile inventory file name, which is user-controlled
// too (issue #671); and FuzzInventoryDecoding runs the same path on arbitrary
// bytes and an arbitrary name, seeded with the corpus and with hostile names, so
// the scheduled audit's fuzz step finds the decoder and the renderers (ADR 0040).
package htmlreport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
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
// document that is not strict JSON. The decoder accepts no field but the ones
// inventory.New validates: the free-text fields text.Validate reads (source, id,
// rawType, region, workload, a relation's target) and a currency, which must be
// three upper-case ASCII letters, not free text. The format has no tags, so an
// inventory that passes loading cannot carry such a character anywhere.
var corpusDecisions = []corpusDecision{
	{
		file: "html-metacharacters.json",
		why:  "HTML metacharacters (< > & \" ') and script, style and attribute-injection payloads in the source and in every free-text field of a resource",
	},
	{
		file: "length-limits.json",
		why:  "every free-text value exactly text.MaxLen (2048) bytes, five of them: the source a run of s, the ID a run of i, the raw type a run of <, the region a run of | and the workload a run of backticks",
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

// nameInventory is the inventory the file-name cases assess: one sized and used
// compute instance, so a report shows every section beside the name it shows, and
// no hostility of its own, so a golden's only difference from its neighbour is the
// name line. Every value is sanitised (CLAUDE.md rule 2).
const nameInventory = `{
  "schemaVersion": 2,
  "source": "aws",
  "resources": [
    {
      "id": "i-0123456789abcdef0",
      "rawType": "AWS::EC2::Instance",
      "category": "compute.vm",
      "region": "eu-west-1",
      "size": { "vcpus": 2, "memoryGiB": 8, "storageGiB": 100, "count": 1 },
      "usage": { "hoursPerMonth": 730 }
    }
  ]
}`

// inventoryNameCase is one inventory file name a run is asked to report on,
// and what it holds.
type inventoryNameCase struct {
	// slug names the case's golden files; it is never the name itself, which
	// holds characters a file path does not.
	slug string
	// name is the file name as the CLI hands it on: filepath.Base of the path it
	// was given, so a name that is not a base name reaches a run through another
	// caller alone.
	name string
	// why says what the name holds and, for a name a run refuses, which check
	// stops it.
	why string
	// wantErr is the sentinel a run must refuse the name with; nil for a name a
	// report must show.
	wantErr error
}

// hostileInventoryNames are the inventory file names the three reports are held
// to. The name is user-controlled, it is whatever the file a run is given is
// called, and every report shows it: the Markdown and HTML reports as a code
// span and a code element in their header, the JSON document as its
// "inventoryName" string.
//
// A name a report must show is one analysis.ReportMeta accepts: valid free text
// (text.Validate) of at most text.MaxLen bytes that is a base name, with no '/'
// and no '\' and not "." or "..". The goldens under
// testdata/hostile-inventory-names/ show what each format does with it.
//
// A name a run refuses is refused by ReportMeta's checks before any port is
// called, so it never reaches a renderer, an assessment or a file: the line
// breaks, control, format and separator characters, and the lengths text.Validate
// refuses, and the paths, separators, "." and ".." the base-name rule refuses.
// The last two have no case here: internal/app/analysis/analysis_test.go's
// TestRunReportMeta covers them (the "current directory" and "parent
// directory" cases). The Markdown reporter's and the HTML reporter's escaping
// of what does reach them is pinned by the goldens above and by
// FuzzInventoryDecoding, which fuzzes the name too.
var hostileInventoryNames = []inventoryNameCase{
	{
		slug: "markdown-metacharacters",
		name: "| a | *b* _c_ `d` #e [f](g) ~h~ !.json",
		why:  "Markdown metacharacters (| * _ ` # [ ] ( ) ~ !) in a name the Markdown report shows as a code span",
	},
	{
		slug: "html-metacharacters",
		name: `<img src=x onerror=alert("example.com")>&'"&.json`,
		why:  "HTML metacharacters (< > & \" ') and an attribute-injection payload in a name the HTML report shows in a code element",
	},
	{
		slug: "unicode-lookalikes",
		name: "\u0430ws-\u0435\u0301\u0432\u0440\u043e\u043f\u0430\u00a0.json",
		why:  "Cyrillic homoglyphs, a combining mark and a no-break space: Unicode that only looks hostile and that text.Validate accepts",
	},
	{
		slug: "longest",
		name: strings.Repeat("a", text.MaxLen-len(".json")) + ".json",
		why:  "a name of exactly text.MaxLen (2048) bytes, the longest text.Validate accepts",
	},
	{
		slug:    "line-break",
		name:    "a\nb.json",
		why:     "a line break, a control character text.Validate refuses, so no name can start a line of a report",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "bidi-override",
		name:    "inv\u202enosj.json",
		why:     "a right-to-left override (U+202E), a format character text.Validate refuses",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "zero-width",
		name:    "inv\u200bentory.json",
		why:     "a zero-width space (U+200B), a format character text.Validate refuses",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug: "backticks-both-ends",
		name: "`inventory.json`",
		why:  "a name fenced the way CommonMark would read it without padding: it starts and ends with a backtick, so the reporter pads the code span with a space on each side",
	},
	{
		slug: "leading-backtick",
		name: "`inventory.json",
		why:  "a name that starts with a backtick and ends with none, so the reporter pads only the side the fence would otherwise merge with",
	},
	{
		slug: "backtick-run",
		name: "inventory```name.json",
		why:  "a run of three backticks inside a name, so the reporter fences the code span with four",
	},
	{
		slug:    "too-long",
		name:    strings.Repeat("a", text.MaxLen) + ".json",
		why:     "a name longer than text.MaxLen, refused by text.Validate",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "surrounding-spaces",
		name:    " inventory.json ",
		why:     "a name with a space at both ends, refused by text.Validate (leading or trailing white space), so the reporter's space padding is reached by a backtick alone",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "line-separator",
		name:    "inv\u2028entory.json",
		why:     "a line separator (U+2028), a separator character text.Validate refuses",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "parent-directory",
		name:    "../inventory.json",
		why:     "a relative path, refused by the base-name rule",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "absolute-path",
		name:    "/etc/inventory.json",
		why:     "an absolute path, refused by the base-name rule",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "windows-path",
		name:    `C:\data\inventory.json`,
		why:     "a Windows path, refused by the base-name rule on every OS, so a report shows no directory whatever built the name",
		wantErr: analysis.ErrInvalidReportMeta,
	},
	{
		slug:    "closing-tag",
		name:    "<script>alert(1)</script>.json",
		why:     "a script element's closing tag, refused by the base-name rule because its '/' is a path separator: a file whose name holds one cannot reach a report, whatever the report would do with it",
		wantErr: analysis.ErrInvalidReportMeta,
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
// of it reaches a report as markup. checkReport holds each report to its
// format's rule besides the golden, so a golden updated over unescaped output
// fails here too.
//
// Each file is decoded once and its inventory rendered three times: the decoder's
// output is the same on every call (the inventory-file adapter's contract), so
// the one decode is the decode each of the three runs would have made.
func TestHostileCorpusReports(t *testing.T) {
	t.Parallel()

	for _, tc := range corpusDecisions {
		if tc.wantErr != nil {
			continue
		}

		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			data := readHostileCorpus(t, tc.file)

			inv, err := inventoryfile.FromBytes(data).Inventory(t.Context())
			if err != nil {
				t.Fatalf("Inventory() refused a corpus file that must load: %v (%s)", err, tc.why)
			}

			for _, format := range reportFormats() {
				t.Run(format.name, func(t *testing.T) {
					t.Parallel()

					got, _, err := renderedReport(t, tc.file, inv, scaleway.New(), format.newReporter())
					if err != nil {
						t.Fatalf("%s report of an inventory that loads returned unexpected error: %v", format.name, err)
					}

					checkReport(t, format.name, got)

					testutil.Golden(t, filepath.Join("testdata", "hostile-corpus", tc.file+"."+format.name+".golden"), got)

					checkInventoryUnchanged(t, data, inv)
				})
			}
		})
	}
}

// TestHostileInventoryNames pins what the three reports do with the inventory
// file's base name, which is user-controlled too: every name a run accepts is
// rendered by all three reporters, and by the comparison report of all three
// embedded targets, against the Scaleway target with estimates on, and held to
// its golden under testdata/hostile-inventory-names/ (run the test with -update
// to accept a change); every name a run refuses is refused with the report
// metadata's sentinel before anything is written or read.
//
// The goldens are where the escaping of a name shows: Markdown and HTML must
// escape it where they write it as a value, and the JSON document must encode
// it, so no name a caller can give a run reaches a report as markup. The name
// line, the code element and the inventoryName field are parsed back, not only
// matched by the golden, so a golden updated over an escaped-once-too-often
// name fails here too.
func TestHostileInventoryNames(t *testing.T) {
	t.Parallel()

	inv, err := inventoryfile.FromBytes([]byte(nameInventory)).Inventory(t.Context())
	if err != nil {
		t.Fatalf("Inventory() refused the inventory the file-name cases assess: %v", err)
	}

	for _, tc := range hostileInventoryNames {
		t.Run(tc.slug, func(t *testing.T) {
			t.Parallel()

			for _, format := range reportFormats() {
				t.Run(format.name, func(t *testing.T) {
					t.Parallel()

					got, calls, err := renderedReport(t, tc.name, inv, scaleway.New(), format.newReporter())
					if tc.wantErr != nil {
						checkRefusedName(t, tc.name, got, calls, err)

						return
					}

					if err != nil {
						t.Fatalf("%s report of the inventory name %q returned unexpected error: %v (%s)",
							format.name, tc.name, err, tc.why)
					}

					checkReport(t, format.name, got)
					checkName(t, format.name, tc.name, got)

					testutil.Golden(t, filepath.Join("testdata", "hostile-inventory-names", tc.slug+"."+format.name+".golden"), got)
				})
			}

			for _, format := range comparisonFormats() {
				t.Run("comparison/"+format.name, func(t *testing.T) {
					t.Parallel()

					got, calls, err := renderedComparison(t, tc.name, inv, embeddedAnalysisTargets(), format.newReporter())
					if tc.wantErr != nil {
						checkRefusedName(t, tc.name, got, calls, err)

						return
					}

					if err != nil {
						t.Fatalf("%s comparison report of the inventory name %q returned unexpected error: %v (%s)",
							format.name, tc.name, err, tc.why)
					}

					checkReport(t, format.name, got)
					checkName(t, format.name, tc.name, got)

					testutil.Golden(t, filepath.Join("testdata", "hostile-inventory-names", tc.slug+".comparison."+format.name+".golden"), got)
				})
			}
		})
	}
}

// FuzzInventoryDecoding runs the decoder on arbitrary bytes and the reporters on
// an arbitrary inventory file name: whatever it is given, the decoder never
// panics, and a refusal is a controlled error, one line that wraps a sentinel.
// Every inventory it accepts is assessed against all three embedded targets,
// with estimates on, and rendered by all three reporters without an error, and
// what they write is valid UTF-8: the HTML fragment opens no element no reporter
// writes, the Markdown tables stay whole and the Markdown report shows the name
// in a code span, and the JSON document is valid and holds no raw markup
// character. A name a run refuses is refused with the report metadata's
// sentinel, in one line and before anything is written.
//
// Its seed corpus is the audit's hostile corpus and the inventory-file adapter's
// version 1 and version 2 fixtures, under a plain file name, and the name cases
// under their own names, so a plain run of this package's tests runs them all as
// subtests, and the scheduled audit's fuzz step fuzzes the decoder and the name
// from them (ADR 0040, row 14).
func FuzzInventoryDecoding(f *testing.F) {
	for _, name := range hostileCorpusFiles(f) {
		f.Add(readHostileCorpus(f, name), "inventory.json")
	}

	for _, name := range []string{"valid.json", "valid-v2.json"} {
		data, err := os.ReadFile(filepath.Join(inventoryFixtures, name))
		if err != nil {
			f.Fatalf("read the inventory fixture %s: %v", name, err)
		}

		f.Add(data, "inventory.json")
	}

	for _, tc := range hostileInventoryNames {
		f.Add([]byte(nameInventory), tc.name)
	}

	f.Fuzz(func(t *testing.T, data []byte, name string) {
		// The inventory is decoded once and the one inventory rendered nine times,
		// once per target and format, then three times more, once per comparison
		// format, as the CLI's nine runs would have decoded it once each: the
		// decoder's output is the same on every call (the inventory-file adapter's
		// contract), and assess and render, the other two stages of every run, run
		// here as often as the CLI runs them. At the end the document is decoded
		// again, so no render may have changed the inventory they were given.
		inv, err := inventoryfile.FromBytes(data).Inventory(t.Context())
		if err != nil {
			checkControlledRefusal(t, err)

			return
		}

		for _, target := range embeddedTargets() {
			for _, format := range reportFormats() {
				got, calls, err := renderedReport(t, name, inv, target.source, format.newReporter())
				if err != nil {
					checkRefusedName(t, name, got, calls, err)
					checkComparisonRefusals(t, name, inv)

					return
				}

				checkReport(t, format.name, got)
				checkName(t, format.name, name, got)
			}
		}

		for _, format := range comparisonFormats() {
			got, calls, err := renderedComparison(t, name, inv, embeddedAnalysisTargets(), format.newReporter())
			if err != nil {
				checkRefusedName(t, name, got, calls, err)

				return
			}

			checkReport(t, format.name, got)
			checkName(t, format.name, name, got)
		}

		checkInventoryUnchanged(t, data, inv)
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

// comparisonFormat is one of the three reporters' comparison renderings, with
// the name its golden files take. The CLI wires the Markdown and JSON ones for
// --target all, and the site's module wires the HTML one beside them; all three
// show the inventory file's name.
type comparisonFormat struct {
	name        string
	newReporter func() analysis.ComparisonReporter
}

// comparisonFormats returns the comparison renderings of the three reports the
// package's reportFormats names, over the same three embedded targets.
func comparisonFormats() []comparisonFormat {
	return []comparisonFormat{
		{name: "markdown", newReporter: func() analysis.ComparisonReporter { return markdown.New() }},
		{name: "json", newReporter: func() analysis.ComparisonReporter { return jsonreport.New(analysis.DefaultSchemaVersion) }},
		{name: "html", newReporter: func() analysis.ComparisonReporter { return htmlreport.New() }},
	}
}

// embeddedAnalysisTargets returns the three embedded catalog targets as the
// comparison use case takes them, keyed as cmd/shift2eu keys them.
func embeddedAnalysisTargets() []analysis.Target {
	targets := make([]analysis.Target, 0, len(embeddedTargets()))

	for _, target := range embeddedTargets() {
		targets = append(targets, analysis.Target{Key: target.key, Catalog: target.source})
	}

	return targets
}

// decodedInventory is an analysis.InventorySource over an inventory already
// decoded, so the decoder runs once for a document assessed and rendered many
// times over. The CLI's run reads it through inventoryfile, which decodes on
// every call; the adapter's contract fixes that the same document decodes to the
// same inventory, so the nine runs of one fuzz input share the one decode the
// CLI would have made for each of them. calls counts the port's calls, so a name
// a run refuses can be held to calling it not at all.
type decodedInventory struct {
	inv   inventory.Inventory
	calls int
}

// Inventory returns the decoded inventory: it is the decoder's own result, and
// a document the decoder refused never reaches a run. It counts the call.
func (d *decodedInventory) Inventory(context.Context) (inventory.Inventory, error) {
	d.calls++

	return d.inv, nil
}

// renderedReport runs the whole assess path of one target on inv, the inventory
// of a document the decoder accepted, and returns what rep wrote with name as the
// report's inventory name, and how often the run called its inventory source:
// the inventory source is the decoded inventory (decodedInventory), the catalog
// is the target's embedded snapshot, and estimates are on, as they are by
// default. The bytes are what rep wrote before an error, if it returned one; the
// error is whatever RunWith returned.
func renderedReport(tb testing.TB, name string, inv inventory.Inventory, cat analysis.CatalogSource, rep analysis.Reporter) ([]byte, int, error) {
	tb.Helper()

	source := &decodedInventory{inv: inv}

	a, err := analysis.New(source, cat, rep)
	if err != nil {
		tb.Fatalf("analysis.New() with the inventory name %q returned unexpected error: %v", name, err)
	}

	var out bytes.Buffer

	err = a.RunWith(tb.Context(), &out, analysis.ReportMeta{InventoryName: name}, analysis.Options{Estimate: true})

	return out.Bytes(), source.calls, err
}

// renderedComparison runs the whole comparison path of the three embedded
// targets on inv and returns what rep wrote with name as the comparison's
// inventory name, and how often the run called its inventory source: the
// inventory source is the decoded inventory (decodedInventory), the catalogs are
// the targets' embedded snapshots, and estimates are on, as they are by default.
// The bytes are what rep wrote before an error, if it returned one; the error is
// whatever RunWith returned.
func renderedComparison(tb testing.TB, name string, inv inventory.Inventory, targets []analysis.Target, rep analysis.ComparisonReporter) ([]byte, int, error) {
	tb.Helper()

	source := &decodedInventory{inv: inv}

	c, err := analysis.NewComparison(source, targets, rep)
	if err != nil {
		tb.Fatalf("analysis.NewComparison() with the inventory name %q returned unexpected error: %v", name, err)
	}

	var out bytes.Buffer

	err = c.RunWith(tb.Context(), &out, analysis.ReportMeta{InventoryName: name}, analysis.ComparisonOptions{Estimate: true})

	return out.Bytes(), source.calls, err
}

// checkComparisonRefusals fails unless every comparison format refuses the
// inventory name a single-target run just refused, and calls no inventory
// source to do it.
func checkComparisonRefusals(t *testing.T, name string, inv inventory.Inventory) {
	t.Helper()

	for _, format := range comparisonFormats() {
		got, calls, err := renderedComparison(t, name, inv, embeddedAnalysisTargets(), format.newReporter())
		checkRefusedName(t, name, got, calls, err)
	}
}

// checkInventoryUnchanged fails when rendering changed the inventory a run was
// given: the document is decoded again and both of the inventory's observable
// parts, its source and its resources, must still be what the first decode
// returned. The inventory is opaque outside its domain package, so the two
// accessors are what a render could change visibly; the adapter's own contract
// tests compare them the same way.
func checkInventoryUnchanged(t *testing.T, data []byte, inv inventory.Inventory) {
	t.Helper()

	again, err := inventoryfile.FromBytes(data).Inventory(t.Context())
	if err != nil {
		t.Fatalf("Inventory() refused a document it accepted before rendering: %v", err)
	}

	if diff := cmp.Diff(inv.Source(), again.Source()); diff != "" {
		t.Fatalf("rendering changed the inventory's source (-before +after):\n%s", diff)
	}

	if diff := cmp.Diff(inv.Resources(), again.Resources()); diff != "" {
		t.Fatalf("rendering changed the inventory's resources (-before +after):\n%s", diff)
	}
}

// checkRefusedName fails unless err is a run's refusal of the inventory name
// name, the run wrote nothing, and it called its inventory source not at all: a
// name that is not valid free text, is a path rather than a base name or is
// longer than text.MaxLen is refused by the report metadata's check before any
// port is called, in one line, so no reporter, assessment or file ever sees it.
// Any other error, a refusal that leaves part of a report behind, or a run that
// read the inventory anyway, is the failure.
func checkRefusedName(t *testing.T, name string, got []byte, calls int, err error) {
	t.Helper()

	if !errors.Is(err, analysis.ErrInvalidReportMeta) {
		t.Fatalf("a run with the inventory name %q returned the error %v, want a refusal wrapping %v",
			name, err, analysis.ErrInvalidReportMeta)
	}

	if msg := err.Error(); strings.ContainsAny(msg, "\r\n") {
		t.Fatalf("the refusal of the inventory name %q is more than one line: %q", name, msg)
	}

	if calls != 0 {
		t.Fatalf("a run refused the inventory name %q after calling the inventory source %d times, want none: the name is checked before any port is called",
			name, calls)
	}

	if len(got) != 0 {
		t.Fatalf("a run refused the inventory name %q and wrote %d bytes of report anyway:\n%s", name, len(got), got)
	}
}

// checkName fails unless a report of format shows name, a name the run
// accepted, as the value it is: Markdown in a code span on its inventory line,
// JSON in the document's "inventoryName" field and HTML in a code element of
// its header line. Each is parsed back to the name it shows, not only matched
// by the golden, so a golden updated over a name escaped once too often fails
// here too.
func checkName(t *testing.T, format, name string, got []byte) {
	t.Helper()

	switch format {
	case "markdown":
		checkNameLine(t, name, got)
	case "json":
		checkNameJSON(t, name, got)
	case "html":
		checkNameHTML(t, name, got)
	default:
		t.Fatalf("no name check for the report format %q", format)
	}
}

// checkNameLine fails unless the Markdown report shows name, a name the run
// accepted, as a CommonMark code span on its inventory line: the third line of
// the report, after the title and the blank line under it, which is where the
// reporter writes the name and the only place it writes it. The span is parsed
// as CommonMark reads one: a fence of n backticks, content, and a closing run of
// exactly n backticks that ends the line, n being one more than the longest run
// of backticks in the name (markdown/codeSpan, which the package cannot import);
// one padding space is stripped from each side of the content when it has one on
// both and is not all spaces. What is left must be the name exactly, so no
// escaping that changes a character and no missing or doubled padding passes.
// An empty name has no line at all, so it is not checked.
func checkNameLine(t *testing.T, name string, got []byte) {
	t.Helper()

	if name == "" {
		return
	}

	const prefix = "- Inventory file: "

	lines := strings.Split(string(got), "\n")
	if len(lines) < 3 {
		t.Fatalf("the Markdown report of the inventory name %q has %d lines, want at least a title, a blank line and the inventory line:\n%s",
			name, len(lines), got)
	}

	line := lines[2]
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("line 3 of the Markdown report of the inventory name %q is %q, want it to start with %q:\n%s",
			name, line, prefix, got)
	}

	span := line[len(prefix):]

	opening := 0
	for opening < len(span) && span[opening] == '`' {
		opening++
	}

	if opening == 0 {
		t.Fatalf("line 3 of the Markdown report of the inventory name %q holds %q, want the name in a code span:\n%s",
			name, span, got)
	}

	closing := 0
	for closing < len(span) && span[len(span)-1-closing] == '`' {
		closing++
	}

	if closing != opening || len(span) < 2*opening {
		t.Fatalf("line 3 of the Markdown report of the inventory name %q fences the value with %d backticks and closes it with a run of %d, want a closing run of exactly %d ending the line:\n%s",
			name, opening, closing, opening, got)
	}

	content := span[opening : len(span)-opening]
	if len(content) > 2 && strings.HasPrefix(content, " ") && strings.HasSuffix(content, " ") &&
		strings.Trim(content, " ") != "" {
		content = content[1 : len(content)-1]
	}

	if want := longestBacktickRun(name) + 1; opening != want {
		t.Fatalf("line 3 of the Markdown report of the inventory name %q fences the name with %d backticks, want %d, one more than the longest run of backticks in the name:\n%s",
			name, opening, want, got)
	}

	if content != name {
		t.Fatalf("line 3 of the Markdown report of the inventory name %q shows %q in its code span, want the name exactly:\n%s",
			name, content, got)
	}
}

// longestBacktickRun returns the length of the longest run of backticks in s,
// which is what CommonMark's fence for a code span has to be one longer than.
func longestBacktickRun(s string) int {
	longest, run := 0, 0

	for i := range len(s) {
		if s[i] != '`' {
			run = 0

			continue
		}

		run++
		longest = max(longest, run)
	}

	return longest
}

// checkNameJSON fails unless the JSON document's "inventoryName" field is name,
// a name the run accepted: the field every single-target and comparison
// document writes, decoded as a JSON string, so the name reaches the document
// as its value and not as markup or an escaped variant of itself. An empty name
// is the document's empty field, which has no name to check.
func checkNameJSON(t *testing.T, name string, got []byte) {
	t.Helper()

	if name == "" {
		return
	}

	var doc struct {
		InventoryName string `json:"inventoryName"`
	}

	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("the JSON report of the inventory name %q is not one JSON document: %v:\n%s", name, err, got)
	}

	if doc.InventoryName != name {
		t.Fatalf("the JSON report of the inventory name %q shows the inventoryName %q, want the name exactly:\n%s",
			name, doc.InventoryName, got)
	}
}

// checkNameHTML fails unless a code element of the HTML fragment's header line
// holds name, a name the run accepted, read as a browser reads it: the header
// line is the fragment's first list element carrying role="list", and
// html.UnescapeString turns its code elements' character references back into
// the text a reader sees, so a name escaped once too often or not at all is not
// the name it shows. An empty name has no element at all, so it is not checked.
func checkNameHTML(t *testing.T, name string, got []byte) {
	t.Helper()

	if name == "" {
		return
	}

	shown := headerCodeContents(string(got))

	for _, content := range shown {
		if content == name {
			return
		}
	}

	t.Fatalf("the HTML report of the inventory name %q shows the header code elements %q, want one holding the name exactly:\n%s",
		name, shown, got)
}

// headerCodeContents returns the content of every code element of a fragment's
// header line, its first list element carrying role="list", with the character
// references decoded as a browser decodes them.
func headerCodeContents(fragment string) []string {
	const open = `<ul role="list">`

	start := strings.Index(fragment, open)
	if start < 0 {
		return nil
	}

	start += len(open)

	end := strings.Index(fragment[start:], "</ul>")
	if end < 0 {
		return nil
	}

	items := fragment[start : start+end]

	var contents []string

	for {
		i := strings.Index(items, "<code>")
		if i < 0 {
			return contents
		}

		rest := items[i+len("<code>"):]

		j := strings.Index(rest, "</code>")
		if j < 0 {
			return contents
		}

		contents = append(contents, html.UnescapeString(rest[:j]))

		items = rest[j+len("</code>"):]
	}
}

// checkReport fails unless a report of an inventory that loads is text a reader
// can show: valid UTF-8 for all three, and, for each format, what it must never
// let through. The HTML fragment opens no element and holds no attribute no
// reporter writes, so no value reached it as markup (checkNoInjectedMarkup, this
// package's own check, so neither a fuzz run nor the goldens can accept
// unescaped output). The Markdown report keeps every table row whole. The JSON
// document is one valid document holding no raw <, > or &, which a JSON encoder
// escapes so the document cannot be read as markup.
func checkReport(t *testing.T, format string, got []byte) {
	t.Helper()

	if !utf8.Valid(got) {
		t.Fatalf("the %s report is not valid UTF-8:\n%q", format, got)
	}

	switch format {
	case "html":
		checkNoInjectedMarkup(t, got)

		return
	case "markdown":
		checkTableIntegrity(t, got)

		return
	case "json":
	default:
		t.Fatalf("no check for the report format %q", format)
	}

	if !json.Valid(got) {
		t.Fatalf("the JSON report is not one valid JSON document:\n%s", got)
	}

	if i := strings.IndexAny(string(got), "<>&"); i >= 0 {
		t.Fatalf("the JSON report holds the raw character %q at byte %d, want it escaped, so the document cannot be read as markup:\n%s",
			string(got)[i], i, got)
	}
}

// tableRowCounts is where a Markdown table row and its table's header disagree
// on how many cell boundaries they have.
type tableRowCounts struct {
	// line is the row's 1-based line number in the report.
	line int
	// row is the number of cell boundaries the row has.
	row int
	// header is the number of cell boundaries the table's header has.
	header int
}

// firstBrokenTableRow returns where the first broken table row of a Markdown
// report is, and false when every table row has as many cell boundaries as its
// header. A cell boundary is a pipe no backslash escapes, which is what the
// renderer writes for every pipe in a cell, in running text and in a code span
// alike (markdown/codeSpan and markdown/escapeText), and what the table
// extension of CommonMark reads as one. A line that does not start with a pipe
// ends the table it was in: every table row of the renderer starts and ends with
// one, and no value can start a line, since line endings become character
// references in running text and spaces in a code span.
func firstBrokenTableRow(report []byte) (tableRowCounts, bool) {
	header := -1

	for i, line := range strings.Split(string(report), "\n") {
		if !strings.HasPrefix(line, "|") {
			header = -1

			continue
		}

		boundaries := cellBoundaries(line)

		if header < 0 {
			header = boundaries

			continue
		}

		if boundaries != header {
			return tableRowCounts{line: i + 1, row: boundaries, header: header}, true
		}
	}

	return tableRowCounts{}, false
}

// cellBoundaries returns the number of pipes of a table row that bound a cell:
// every pipe of it that no backslash escapes. A backslash byte cannot be part of
// another character's encoding, so looking at the byte before the pipe is the
// whole rule.
func cellBoundaries(line string) int {
	boundaries := 0

	for i := range len(line) {
		if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			boundaries++
		}
	}

	return boundaries
}

// checkTableIntegrity fails unless every table row of a Markdown report has as
// many cell boundaries as its table's header, so no value reached a row as a
// cell boundary and no row gained or lost a cell.
func checkTableIntegrity(t *testing.T, got []byte) {
	t.Helper()

	broken, ok := firstBrokenTableRow(got)
	if !ok {
		return
	}

	t.Fatalf("line %d of the Markdown report has %d cell boundaries, its table's header has %d, so a value reached the table as a cell boundary:\n%s",
		broken.line, broken.row, broken.header, got)
}

// TestFirstBrokenTableRow pins the Markdown table check the corpus reports and
// the fuzz hold: every table shape the renderer writes passes it, a table after
// a table is a table of its own, and a value that reached a row as a raw cell
// boundary, the regression this check exists for, is what it reports and where.
func TestFirstBrokenTableRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report string
		// broken is the row the check must report; the zero value for a report
		// whose every table is whole.
		broken tableRowCounts
	}{
		{
			name: "a table the renderer writes",
			report: "| ID | Raw type | Category |\n" +
				"|---|---|---|\n" +
				"| `i-1` | `AWS::EC2::Instance` | `compute.vm` |\n" +
				"| `i-2` | `AWS::S3::Bucket` | `storage.object` |\n",
		},
		{
			name: "a pipe a code span escapes",
			report: "| ID | Fit |\n" +
				"|---|---|\n" +
				"| `a\\|b` | direct |\n",
		},
		{
			name: "a pipe running text escapes",
			report: "| ID | Fit |\n" +
				"|---|---|\n" +
				"| `x` | `Instances` offers `a\\|b` |\n",
		},
		{
			name: "a cell whose code span is fenced with backticks",
			report: "| ID | Fit |\n" +
				"|---|---|\n" +
				"| ````` ```` a run of backticks ```` ````` | direct |\n",
		},
		{
			name: "a cell that ends with a backslash",
			report: "| ID | Fit |\n" +
				"|---|---|\n" +
				"| `a\\` | `b` |\n",
		},
		{
			name: "two tables of different widths",
			report: "| Fit | Resources |\n" +
				"|---|---|\n" +
				"| direct | 4 |\n" +
				"\n" +
				"| ID | Raw type | Category |\n" +
				"|---|---|---|\n" +
				"| `i-1` | `x` | `compute.vm` |\n",
		},
		{
			name:   "a report without a table",
			report: "# Migration assessment: `Scaleway`\n\nNone.\n",
		},
		{
			name: "a value that reached a row as a cell boundary",
			report: "| ID | Raw type | Category |\n" +
				"|---|---|---|\n" +
				"| `a|b` | `AWS::X` | `compute.vm` |\n",
			broken: tableRowCounts{line: 3, row: 5, header: 4},
		},
		{
			name: "a row that lost a cell",
			report: "| ID | Raw type | Category |\n" +
				"|---|---|---|\n" +
				"| `i-1` | `AWS::X` |\n",
			broken: tableRowCounts{line: 3, row: 3, header: 4},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := firstBrokenTableRow([]byte(tc.report))

			want := tc.broken != (tableRowCounts{})
			if ok != want {
				t.Fatalf("firstBrokenTableRow() = %+v, %t, want a broken row: %t (%q)", got, ok, want, tc.report)
			}

			if ok && got != tc.broken {
				t.Errorf("firstBrokenTableRow() = %+v, want %+v", got, tc.broken)
			}
		})
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
