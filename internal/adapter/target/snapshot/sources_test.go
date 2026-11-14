// The documentation rule every embedded snapshot follows is pinned here: each
// offering in a snapshot has its own section in the SOURCES.md beside it, and
// that section names the https pages the offering comes from, the fact behind
// each capability and region, the name of each plan and the amount of each price
// and fixed fee as the catalog holds them (ADR 0024, ADR 0026), and the licence
// of each source. The test globs the
// snapshots beside this package, so it runs for every target with an embedded
// snapshot, adding one as each target arrives.

package snapshot_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/target/snapshot"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// The files a snapshot directory holds, and the headings its SOURCES.md must
// carry. Paths are relative to this package's directory, where go test runs.
const (
	catalogFileName  = "catalog.json"
	sourcesFileName  = "SOURCES.md"
	licencesHeading  = "## Licences of the sources"
	offeringsHeading = "## Offerings"
	// defaultRegionHeading is the section that names the region estimates use
	// when the user names none, and why it is that region (ADR 0024).
	defaultRegionHeading = "## Default region"
)

// requiredSnapshots are the snapshots the rule is known to apply to today: every
// target adapter that embeds one. Finding none of them would make the glob below
// pass while checking nothing, so the absence of any one is a failure, and a new
// target's snapshot is added here so the check cannot silently skip it.
var requiredSnapshots = []string{"../scaleway/catalog.json", "../ovhcloud/catalog.json", "../upcloud/catalog.json"}

// TestEveryOfferingHasASourcesSection pins that every offering in an embedded
// snapshot has exactly one section of its own in the SOURCES.md beside it, that
// no section names an offering the snapshot does not have, that the section
// names at least one https page the offering comes from, and that the section
// names each of the offering's categories, capabilities and regions as a
// Markdown code span, so the fact behind each one can be traced to a page.
func TestEveryOfferingHasASourcesSection(t *testing.T) {
	t.Parallel()

	// Every target adapter's embedded snapshot is a sibling directory of this
	// package, so one glob covers every target, today and as more arrive.
	paths, err := filepath.Glob("../*/" + catalogFileName)
	if err != nil {
		t.Fatalf("globbing the snapshots beside this package: %v", err)
	}

	if len(paths) == 0 {
		t.Fatalf("found no embedded snapshot: no %s matches ../*/%s", catalogFileName, catalogFileName)
	}

	for _, want := range requiredSnapshots {
		if !slices.Contains(paths, want) {
			t.Fatalf("the snapshots found are %v, want them to include %s", paths, want)
		}
	}

	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			t.Parallel()

			checkSnapshotSources(t, path)
		})
	}
}

// mdSection is one "### <name>" subsection of a SOURCES.md: the heading's text
// and the lines under it, up to the next third-level heading.
type mdSection struct {
	name string
	body []string
}

// checkSnapshotSources checks one snapshot and the SOURCES.md beside it, and
// reports every problem it finds rather than the first, so one run shows all the
// gaps of a snapshot.
func checkSnapshotSources(t *testing.T, catalogPath string) {
	t.Helper()

	dir := filepath.Dir(catalogPath)

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("%s: reading the snapshot: %v", dir, err)
	}

	document, err := snapshot.Parse(data)
	if err != nil {
		t.Fatalf("%s: the snapshot does not parse through the shared reader: %v", dir, err)
	}

	sourcesPath := filepath.Join(dir, sourcesFileName)

	sources, err := os.ReadFile(sourcesPath)
	if err != nil {
		t.Fatalf("%s: no %s beside the snapshot: %v", dir, sourcesFileName, err)
	}

	lines := strings.Split(string(sources), "\n")

	checkDefaultRegionSection(t, sourcesPath, lines, document.Catalog.Snapshot().DefaultRegion)

	if licences, ok := section(lines, licencesHeading); !ok {
		t.Errorf("%s: no %q section: every source must state its licence", sourcesPath, licencesHeading)
	} else if !hasContent(licences) {
		t.Errorf("%s: the %q section is blank: every source must state its licence", sourcesPath, licencesHeading)
	}

	offerings, ok := section(lines, offeringsHeading)
	if !ok {
		t.Errorf("%s: no %q section: every offering needs a section of its own", sourcesPath, offeringsHeading)

		return
	}

	sections := subsections(offerings)
	offeringSections := make(map[string]int, len(sections))

	for _, s := range sections {
		offeringSections[s.name]++
	}

	byName := make(map[string]catalog.Offering, len(sections))

	for _, offering := range document.Catalog.Offerings() {
		byName[offering.Name] = offering

		switch count := offeringSections[offering.Name]; {
		case count == 0:
			t.Errorf("%s: offering %s has no %s section",
				sourcesPath, text.Quote(offering.Name), text.Quote("### "+offering.Name))
		case count > 1:
			t.Errorf("%s: offering %s has %d %s sections, want exactly one",
				sourcesPath, text.Quote(offering.Name), count, text.Quote("### "+offering.Name))
		}
	}

	for _, s := range sections {
		offering, ok := byName[s.name]
		if !ok {
			t.Errorf("%s: section %s matches no offering in %s",
				sourcesPath, text.Quote("### "+s.name), catalogPath)

			continue
		}

		spans := codeSpans(s.body)

		// A section that names no page states a fact no reader can check: the PRD
		// requires it to name the pages the offering comes from.
		if !hasLink(s.body) {
			t.Errorf("%s: offering %s: its section links no %s page, so where the offering comes from is untraceable",
				sourcesPath, text.Quote(offering.Name), linkScheme)
		}

		for _, category := range offering.Categories {
			if _, ok := spans[string(category)]; !ok {
				t.Errorf("%s: offering %s: category %s has no code span in its section, so the fact behind it is untraceable",
					sourcesPath, text.Quote(offering.Name), text.Quote(string(category)))
			}
		}

		for _, capability := range offering.Capabilities {
			if _, ok := spans[string(capability)]; !ok {
				t.Errorf("%s: offering %s: capability %s has no code span in its section, so the fact behind it is untraceable",
					sourcesPath, text.Quote(offering.Name), text.Quote(string(capability)))
			}
		}

		for _, region := range offering.Regions {
			if _, ok := spans[region]; !ok {
				t.Errorf("%s: offering %s: region %s has no code span in its section, so the fact behind it is untraceable",
					sourcesPath, text.Quote(offering.Name), text.Quote(region))
			}
		}
	}

	for _, violation := range priceCitationViolations(document, string(sources)) {
		t.Errorf("%s: %s", sourcesPath, violation)
	}
}

// priceCitationViolations returns every way a snapshot's plans, prices, fixed
// fees and plan billing cap are not cited in the SOURCES.md beside it: in each
// offering's own "### " subsection, every plan name appears exactly as the
// catalog holds it and every price amount as Amount.String renders it, as a
// Markdown code span, so a price in SOURCES.md and a price in catalog.json can be
// compared as text (ADR 0024). A fixed monthly fee of an offering is a price and
// is cited the same way (ADR 0026). The most hours a month an offering bills one
// instance of one of its plans for is cited as a code span too, as the number of
// hours, so the cap behind a plan price is as traceable as the price (ADR 0029).
// An offering with no plans, storage prices, fixed fees and no cap has nothing to
// cite.
//
// It is a pure function so the rule can be pinned on an invented priced document:
// the invented document pins the rule independently of the embedded snapshots'
// data, whichever plans and fees they happen to carry. A missing "## Offerings"
// section, an offering without a section and a section naming no offering are
// checkSnapshotSources's own findings, reported there, so this function yields
// nothing for them.
func priceCitationViolations(doc snapshot.Document, sources string) []string {
	offerings, ok := section(strings.Split(sources, "\n"), offeringsHeading)
	if !ok {
		return nil
	}

	byName := make(map[string]catalog.Offering, len(doc.Catalog.Offerings()))

	for _, offering := range doc.Catalog.Offerings() {
		byName[offering.Name] = offering
	}

	var violations []string

	for _, s := range subsections(offerings) {
		offering, ok := byName[s.name]
		if !ok {
			continue
		}

		spans := codeSpans(s.body)

		var prices []catalog.Price

		for _, plan := range offering.Plans {
			if _, ok := spans[plan.Name]; !ok {
				violations = append(violations, fmt.Sprintf(
					"offering %s: plan %s has no code span in its section, so its price is untraceable",
					text.Quote(offering.Name), text.Quote(plan.Name)))
			}

			prices = append(prices, plan.Prices...)
		}

		prices = append(prices, offering.StoragePrices...)
		prices = append(prices, offering.FixedFees...)

		for _, price := range prices {
			amount := price.Amount.String()

			if _, ok := spans[amount]; !ok {
				violations = append(violations, fmt.Sprintf(
					"offering %s: price %s has no code span in its section, so the amount is untraceable",
					text.Quote(offering.Name), text.Quote(amount)))
			}
		}

		// A cap is cited as the number of hours, so a reader can find the sentence
		// that states it beside the plan prices it applies to (ADR 0029).
		if offering.PlanBillingCapHours > 0 {
			hours := strconv.FormatInt(offering.PlanBillingCapHours, 10)

			if _, ok := spans[hours]; !ok {
				violations = append(violations, fmt.Sprintf(
					"offering %s: plan billing cap %s has no code span in its section, so the cap is untraceable",
					text.Quote(offering.Name), text.Quote(hours)))
			}
		}
	}

	return violations
}

// checkDefaultRegionSection checks the snapshot's "## Default region" section:
// it exists, is not blank, and names the default region as a code span, so a
// reader can see which region estimates use and, from the prose around it, why
// (ADR 0024).
func checkDefaultRegionSection(t *testing.T, sourcesPath string, lines []string, defaultRegion string) {
	t.Helper()

	body, ok := section(lines, defaultRegionHeading)
	if !ok {
		t.Errorf("%s: no %q section: estimates need to say which region they use by default",
			sourcesPath, defaultRegionHeading)

		return
	}

	if !hasContent(body) {
		t.Errorf("%s: the %q section is blank: it must name the default region and why it is the default",
			sourcesPath, defaultRegionHeading)

		return
	}

	if _, ok := codeSpans(body)[defaultRegion]; !ok {
		t.Errorf("%s: the %q section does not name the default region %s as a code span",
			sourcesPath, defaultRegionHeading, text.Quote(defaultRegion))
	}
}

// section returns the lines under the heading whose text is exactly heading, up
// to the next second-level heading, and whether the document has such a heading.
func section(lines []string, heading string) ([]string, bool) {
	start := slices.Index(lines, heading)
	if start < 0 {
		return nil, false
	}

	end := len(lines)

	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i

			break
		}
	}

	return lines[start+1 : end], true
}

// subsections splits lines into the "### <name>" sections under them, each body
// running to the next third-level heading or to the end.
func subsections(lines []string) []mdSection {
	var (
		sections []mdSection
		current  *mdSection
	)

	for _, line := range lines {
		if name, ok := strings.CutPrefix(line, "### "); ok {
			if current != nil {
				sections = append(sections, *current)
			}

			current = &mdSection{name: strings.TrimSpace(name)}

			continue
		}

		if current != nil {
			current.body = append(current.body, line)
		}
	}

	if current != nil {
		sections = append(sections, *current)
	}

	return sections
}

// codeSpanPattern matches a Markdown code span: text between single backticks on
// one line.
var codeSpanPattern = regexp.MustCompile("`([^`\n]+)`")

// linkScheme is what a section's page links start with: an absolute link to the
// page the offering's facts come from.
const linkScheme = "https://"

// hasLink reports whether lines hold a link to a page: the pages a section names
// as where its offering comes from.
func hasLink(lines []string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		return strings.Contains(line, linkScheme)
	})
}

// codeSpans returns the code spans in lines as a set.
func codeSpans(lines []string) map[string]struct{} {
	spans := make(map[string]struct{})

	for _, match := range codeSpanPattern.FindAllStringSubmatch(strings.Join(lines, "\n"), -1) {
		spans[match[1]] = struct{}{}
	}

	return spans
}

// hasContent reports whether lines hold a line that is not blank.
func hasContent(lines []string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		return strings.TrimSpace(line) != ""
	})
}

// TestSourcesHelpers pins the two pure rules the SOURCES.md check is built from,
// on invented Markdown that no snapshot holds: a section is the body under a
// second-level heading, up to the next one, and a code span is the text between
// single backticks on one line. They are what makes a plan name and a price
// amount citable, and the default region nameable (ADR 0024), so a change to
// either helper is a change to the rule every snapshot's sources follow.
func TestSourcesHelpers(t *testing.T) {
	t.Parallel()

	invented := []string{
		"# Sources",
		"",
		defaultRegionHeading,
		"",
		"Estimates use `fr-par`, because it is the target's first-listed region.",
		"",
		offeringsHeading,
		"",
		"### Example Compute",
		"",
		"Plan `EC-2-4` costs `12.50` EUR a month; a GiB of storage costs `0.04`.",
		"",
		licencesHeading,
		"",
		"MIT.",
	}

	t.Run("default region section", func(t *testing.T) {
		t.Parallel()

		body, ok := section(invented, defaultRegionHeading)
		if !ok {
			t.Fatalf("section(%q) found no section in the invented sources", defaultRegionHeading)
		}

		if !hasContent(body) {
			t.Errorf("section(%q) is blank, want the region and why it is the default", defaultRegionHeading)
		}

		if _, ok := codeSpans(body)["fr-par"]; !ok {
			t.Errorf("section(%q) has no `fr-par` code span, want the default region named in one", defaultRegionHeading)
		}

		// A section ends at the next second-level heading, so the offering's code
		// spans are not mistaken for the default region section's.
		if _, ok := codeSpans(body)["EC-2-4"]; ok {
			t.Errorf("section(%q) reaches into the next section, want it to stop at the next `## ` heading",
				defaultRegionHeading)
		}

		if _, ok := section(invented, "## No such heading"); ok {
			t.Error("section() found a heading the invented sources do not have")
		}
	})

	t.Run("code spans", func(t *testing.T) {
		t.Parallel()

		spans := codeSpans([]string{"Plan `EC-2-4` costs `12.50`, or `13.0125` where prices differ; `0.04` per GiB."})

		for _, want := range []string{"EC-2-4", "12.50", "13.0125", "0.04"} {
			if _, ok := spans[want]; !ok {
				t.Errorf("codeSpans() has no %s span, want every code span on the line", text.Quote(want))
			}
		}

		// An amount is cited as Amount.String renders it, trailing zeros kept:
		// the span is 12.50, never the 12.5 the document writes.
		if _, ok := spans["12.5"]; ok {
			t.Error("codeSpans() found a `12.5` span, want the amount cited as Amount.String renders it")
		}
	})
}

// sourcesPricedDocument is a version 2 snapshot that sells a plan, publishes a
// storage price, charges a fixed fee per instance and caps the hours it bills a
// plan for, so the rule that all four are cited in the SOURCES.md can be pinned
// on a document that exercises it, independently of the embedded snapshots' data:
// the plan's amount is written 12.5 in the document but rendered 12.50 by
// Amount.String. Every name, region and amount is invented.
const sourcesPricedDocument = `{
  "schemaVersion": 2,
  "target": "Example Cloud",
  "sourceURL": "https://www.example.com/pricing/",
  "retrievedOn": "2026-11-13",
  "regions": ["xx-par"],
  "defaultRegion": "xx-par",
  "offerings": [
    {
      "name": "Example Compute",
      "categories": ["compute.vm"],
      "capabilities": ["ipv6"],
      "regions": ["xx-par"],
      "plans": [
        {
          "name": "EX-M",
          "vcpus": 2,
          "memoryGiB": 4.5,
          "storageGiB": 40,
          "prices": [{ "amount": 12.5, "currency": "EUR", "regions": ["xx-par"] }]
        }
      ],
      "storagePrices": [{ "amount": 0.04, "currency": "EUR", "regions": ["xx-par"] }],
      "fixedFees": [{ "amount": 65.7, "currency": "EUR", "regions": ["xx-par"] }],
      "planBillingCapHours": 672
    }
  ]
}`

// inventedSources returns a SOURCES.md whose "### Example Compute" section cites
// each of spans as a code span on a line of its own, beside the page the offering
// comes from.
func inventedSources(spans ...string) string {
	lines := []string{
		"# Sources",
		"",
		offeringsHeading,
		"",
		"### Example Compute",
		"",
		"The offering comes from https://www.example.com/pricing/.",
		"",
	}

	for _, span := range spans {
		lines = append(lines, "Cited: `"+span+"`.")
	}

	return strings.Join(append(lines, "", licencesHeading, "", "MIT."), "\n")
}

// TestPriceCitations pins the plans-and-prices rule of ADR 0024 on an invented
// priced document and invented Markdown: every plan of an offering and every
// price amount of it, plan prices, storage prices and fixed fees alike, is a code
// span in the offering's own subsection, the plan by its name exactly as the
// catalog holds it and the amount as Amount.String renders it, so a reader can
// trace each price in the snapshot to the page it came from. An offering's plan
// billing cap is cited as the number of hours it bills a plan for, so the cap the
// plan part of an estimate follows is traceable too (ADR 0029). A plan name that
// is not cited, an amount cited as the document writes it rather than as the
// catalog renders it, a missing storage-price or fixed-fee amount, and a cap with
// no code span are each reported; a section that cites them all is not.
func TestPriceCitations(t *testing.T) {
	t.Parallel()

	document, err := snapshot.Parse([]byte(sourcesPricedDocument))
	if err != nil {
		t.Fatalf("Parse() of the invented priced document returned unexpected error: %v", err)
	}

	tests := []struct {
		name    string
		sources string
		// want holds what one violation must say, one entry per violation the
		// rule must report. An empty list pins that the section cites
		// everything it must.
		want []string
	}{
		{
			name:    "every plan name and amount cited",
			sources: inventedSources("EX-M", "12.50", "0.04", "65.70", "672"),
		},
		{
			name:    "plan name with no code span",
			sources: inventedSources("12.50", "0.04", "65.70", "672"),
			want:    []string{`plan "EX-M" has no code span in its section`},
		},
		{
			// The document writes the amount 12.5, and the catalog holds 12.50: a
			// price can only be compared with its source when it is cited as
			// Amount.String renders it.
			name:    "amount cited as the document writes it",
			sources: inventedSources("EX-M", "12.5", "0.04", "65.70", "672"),
			want:    []string{`price "12.50" has no code span in its section`},
		},
		{
			name:    "storage price amount with no code span",
			sources: inventedSources("EX-M", "12.50", "65.70", "672"),
			want:    []string{`price "0.04" has no code span in its section`},
		},
		{
			// A fixed monthly fee is a price of its offering, so its amount is
			// cited as Amount.String renders it too (ADR 0026).
			name:    "fixed fee amount with no code span",
			sources: inventedSources("EX-M", "12.50", "0.04", "672"),
			want:    []string{`price "65.70" has no code span in its section`},
		},
		{
			// The cap is the number of hours the plan prices are the price of, so a
			// reader can trace the plan part of an estimate back to its page
			// (ADR 0029).
			name:    "plan billing cap with no code span",
			sources: inventedSources("EX-M", "12.50", "0.04", "65.70"),
			want:    []string{`plan billing cap "672" has no code span in its section`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := priceCitationViolations(document, tc.sources)

			if len(got) != len(tc.want) {
				t.Fatalf("priceCitationViolations() returned %d violations %q, want %d",
					len(got), got, len(tc.want))
			}

			for i, want := range tc.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("violation %d = %q, want it to name %s", i, got[i], text.Quote(want))
				}
			}
		})
	}
}
