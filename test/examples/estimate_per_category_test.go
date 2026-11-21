package examples_test

// This file pins one worked estimate per (target, taxonomy category the target
// offers) on the real embedded catalog snapshots, the acceptance criterion PRD
// #190 leaves unticked: "A test table pins one example per priced category on
// each target." TestFor (internal/domain/estimate) pins the same formula on an
// invented catalog and the sample goldens pin real inventories; this table pins
// the plan and the amount the real snapshots give one representative resource
// of every category.
//
// The targets and their categories come from the snapshots themselves, never
// from a hand-written list: a category added to a snapshot grows a row, and one
// that has no representative size here fails the test until a size is chosen
// for it. The rows run through assessment.AssessWith with estimates on, the
// path `shift2eu assess` takes (internal/app/analysis), so the plan, the amount
// and the reason come from the domain, not from parsing a rendered report.

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// representative is the one sized resource a category gets in the table: the
// size and usage the category takes in inventory format version 2. A nil size
// or usage is "not given", as in the format (ADR 0025), and only a field the
// category allows may be set (inventory's allowedFields): vcpus on
// storage.object, say, is refused by inventory.New.
type representative struct {
	size  *inventory.Size
	usage *inventory.Usage
}

// representatives is the representative resource of every taxonomy category the
// embedded snapshots offer, sized by the PRD's sizing rules (see the golden's
// header for the same table in prose):
//
//   - compute.vm: 2 vCPU, 4 GiB of memory, count 1, 730 hours a month;
//   - compute.baremetal: 4 vCPU, 32 GiB of memory, count 1, 730 hours a month,
//     the smallest plan every target's bare-metal offering covers (the cheapest
//     OVHcloud and Scaleway offerings start at 4 vCPU and 32 GiB, so the
//     compute.vm size would be "no plan large enough" there);
//   - kubernetes.managed: 2 vCPU, 4 GiB of memory per node, 3 nodes, 730 hours
//     a month;
//   - database.postgresql and database.mysql: 2 vCPU, 4 GiB of memory, 50 GiB of
//     storage, count 1, 730 hours a month;
//   - storage.block: one 100 GiB volume (count 1);
//   - storage.object: 100 GiB stored;
//   - serverless.function: a function of 1 GiB of memory, the only size field
//     the category takes;
//   - messaging.queue: no size: the category takes no size field, and it is
//     priced by usage.
var representatives = map[taxonomy.Category]representative{
	taxonomy.ComputeVM: {
		size:  &inventory.Size{VCPUs: number(2), MemoryGiB: number(4), Count: count(1)},
		usage: &inventory.Usage{HoursPerMonth: number(730)},
	},
	taxonomy.ComputeBareMetal: {
		size:  &inventory.Size{VCPUs: number(4), MemoryGiB: number(32), Count: count(1)},
		usage: &inventory.Usage{HoursPerMonth: number(730)},
	},
	taxonomy.KubernetesManaged: {
		size:  &inventory.Size{VCPUs: number(2), MemoryGiB: number(4), Count: count(3)},
		usage: &inventory.Usage{HoursPerMonth: number(730)},
	},
	taxonomy.DatabasePostgreSQL: {
		size:  &inventory.Size{VCPUs: number(2), MemoryGiB: number(4), StorageGiB: number(50), Count: count(1)},
		usage: &inventory.Usage{HoursPerMonth: number(730)},
	},
	taxonomy.DatabaseMySQL: {
		size:  &inventory.Size{VCPUs: number(2), MemoryGiB: number(4), StorageGiB: number(50), Count: count(1)},
		usage: &inventory.Usage{HoursPerMonth: number(730)},
	},
	taxonomy.StorageBlock: {
		size: &inventory.Size{StorageGiB: number(100), Count: count(1)},
	},
	taxonomy.StorageObject: {
		size: &inventory.Size{StorageGiB: number(100)},
	},
	taxonomy.ServerlessFunction: {
		size: &inventory.Size{MemoryGiB: number(1)},
	},
	taxonomy.MessagingQueue: {},
}

// TestEstimatePerTargetAndCategory renders one estimate per target and taxonomy
// category into a golden: the plan chosen, the monthly amount in EUR rounded to
// the cent, and, for a resource that is not estimated, the reason. The target
// and its categories are read from each embedded snapshot, and the rows are
// sorted by target, then category, so the golden is stable. Regenerate it with
// `go test ./test/examples -update`.
func TestEstimatePerTargetAndCategory(t *testing.T) {
	t.Parallel()

	var out strings.Builder

	cats := make([]catalog.Catalog, 0, len(targets))

	out.WriteString("# Monthly estimates: one representative resource per target and taxonomy category\n\n")
	out.WriteString("One row per target and taxonomy category the target's embedded snapshot offers, through\n")
	out.WriteString("`assessment.AssessWith` with estimates on (the path `shift2eu assess` takes), never through a\n")
	out.WriteString("rendered report. The plan is the cheapest covering plan the estimate chose, the amount is the\n")
	out.WriteString("monthly list price in EUR rounded to the cent, and a resource that is not estimated gives its\n")
	out.WriteString("reason instead. Regenerate with `go test ./test/examples -update`.\n\n")

	out.WriteString("Sizes, from PRD #190's sizing rules: a virtual machine is 2 vCPU, 4 GiB of memory, count 1 and\n")
	out.WriteString("730 hours a month; a bare-metal server is 4 vCPU and 32 GiB, the smallest plan every target's\n")
	out.WriteString("bare-metal offering covers; Kubernetes counts 3 nodes of the virtual-machine size; a managed\n")
	out.WriteString("database is 2 vCPU, 4 GiB of memory and 50 GiB of storage; block storage is one 100 GiB volume\n")
	out.WriteString("and object storage 100 GiB stored; a function is 1 GiB of memory; a queue takes no size.\n\n")

	for _, target := range targets {
		cat, err := target.catalog().Catalog(t.Context())
		if err != nil {
			t.Fatalf("%s: Catalog: %v", target.key, err)
		}

		snap := cat.Snapshot()

		out.WriteString("- `" + cat.Target() + "`: prices retrieved " + snap.RetrievedOn.UTC().Format(time.DateOnly) +
			" from `" + snap.SourceURL + "`, estimated in `" + snap.DefaultRegion + "` (its default region)\n")

		cats = append(cats, cat)
	}

	out.WriteString("\n| Target | Category | Size | Offering | Plan | Monthly estimate | Reason |\n")
	out.WriteString("|---|---|---|---|---|---|---|\n")

	// The categories are rendered per target, each target's sorted, so the table
	// reads target by target, category by category.
	for _, cat := range cats {
		for _, category := range offeredCategories(cat) {
			out.WriteString(estimateRow(t, cat, category))
		}
	}

	testutil.Golden(t, "testdata/estimates-per-category.md.golden", []byte(out.String()))
}

// offeredCategories returns every taxonomy category cat's offerings serve,
// sorted, so the table's rows follow from the snapshot rather than a list.
func offeredCategories(cat catalog.Catalog) []taxonomy.Category {
	seen := make(map[taxonomy.Category]bool)

	for _, o := range cat.Offerings() {
		for _, c := range o.Categories {
			seen[c] = true
		}
	}

	categories := make([]taxonomy.Category, 0, len(seen))

	for c := range seen {
		categories = append(categories, c)
	}

	slices.Sort(categories)

	return categories
}

// estimateRow assesses the representative resource of category against cat and
// renders it as one row of the golden's table: the size assessed, the offering
// and plan the estimate names, the monthly amount, and the reason a resource
// that is not estimated gives.
func estimateRow(t *testing.T, cat catalog.Catalog, category taxonomy.Category) string {
	t.Helper()

	rep, ok := representatives[category]
	if !ok {
		t.Fatalf("category %q is offered by %s's snapshot but has no representative size: add one to representatives in this file and state it in the golden's header",
			category, cat.Target())
	}

	inv, err := inventory.New("aws", []inventory.Resource{{
		ID:       "res-example",
		RawType:  "Example::Resource",
		Category: category,
		Region:   "eu-west-1",
		Size:     rep.size,
		Usage:    rep.usage,
	}})
	if err != nil {
		t.Fatalf("%s %q: inventory.New: %v", cat.Target(), category, err)
	}

	a := assessment.AssessWith(inv, cat, assessment.Options{Estimate: true})
	if len(a.Verdicts) != 1 || a.Verdicts[0].Estimate == nil {
		t.Fatalf("%s %q: AssessWith gave %d verdict(s) with estimates %v, want one with an estimate",
			cat.Target(), category, len(a.Verdicts), a.Verdicts)
	}

	e := a.Verdicts[0].Estimate

	plan := "none"
	if e.Plan != nil {
		plan = "`" + e.Plan.Name + "`"
	}

	offering := "none"
	if e.Offering != "" {
		offering = "`" + e.Offering + "`"
	}

	monthly := "—"

	reason := ""
	if e.Estimated {
		monthly = euros(e.Monthly)
	} else {
		reason = "`" + string(e.Reason) + "`"
	}

	return "| `" + cat.Target() + "` | `" + string(category) + "` | " + rep.String() + " | " +
		offering + " | " + plan + " | " + monthly + " | " + reason + " |\n"
}

// String renders the representative resource as the table's Size cell: its size
// and usage fields in the order the inventory format lists them, or "none" for
// a category that takes no size.
func (r representative) String() string {
	var parts []string

	if r.size != nil {
		if r.size.VCPUs != nil {
			parts = append(parts, decimal(*r.size.VCPUs)+" vCPU")
		}

		if r.size.MemoryGiB != nil {
			parts = append(parts, decimal(*r.size.MemoryGiB)+" GiB memory")
		}

		if r.size.StorageGiB != nil {
			parts = append(parts, decimal(*r.size.StorageGiB)+" GiB storage")
		}

		if r.size.Count != nil {
			parts = append(parts, "count "+strconv.Itoa(*r.size.Count))
		}
	}

	if r.usage != nil && r.usage.HoursPerMonth != nil {
		parts = append(parts, decimal(*r.usage.HoursPerMonth)+" h/month")
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

// number returns v as the inventory's optional float64 fields take it.
func number(v float64) *float64 { return &v }

// count returns v as the inventory's optional count takes it.
func count(v int) *int { return &v }

// decimal renders v as the decimal it was written as, without an exponent or
// trailing zeros: 2 is "2", 0.5 is "0.5".
func decimal(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// euros renders an amount in euros as the reports do: "€1,234.56", rounded to
// the cent by estimate.Money.String.
func euros(m estimate.Money) string {
	return "€" + thousands(m.String())
}

// thousands inserts the thousands separators of a plain decimal: "1234.56" is
// "1,234.56".
func thousands(decimal string) string {
	whole, fraction, found := strings.Cut(decimal, ".")

	var b strings.Builder

	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(digit)
	}

	if found {
		b.WriteString("." + fraction)
	}

	return b.String()
}
