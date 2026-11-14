package targettest_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/target/targettest"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// scalewayTarget is the target display name the fixture catalog carries.
const scalewayTarget = "Scaleway"

// scalewayExpectations says what the Scaleway adapter must deliver for the
// shared fixture: its display name, its EU regions, the categories the catalog
// has to serve, and the region its estimates use when the user names none.
func scalewayExpectations() targettest.Expectations {
	return targettest.Expectations{
		Target:        scalewayTarget,
		Regions:       []string{"fr-par", "nl-ams", "pl-waw"},
		DefaultRegion: "fr-par",
		Categories: []taxonomy.Category{
			taxonomy.ComputeVM,
			taxonomy.DatabaseMySQL,
			taxonomy.DatabasePostgreSQL,
			taxonomy.KubernetesManaged,
			taxonomy.StorageObject,
		},
	}
}

// newCatalog builds the only kind of catalog an adapter can return: one that
// went through catalog.New.
func newCatalog(t *testing.T, target string, offerings []catalog.Offering) catalog.Catalog {
	t.Helper()

	cat, err := catalog.New(target, testutil.ScalewaySnapshot(), offerings)
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// computeOffering is one minimal, valid offering, for catalogs a test breaks on
// purpose.
func computeOffering(name string, regions ...string) catalog.Offering {
	return catalog.Offering{
		Name:       name,
		Categories: []taxonomy.Category{taxonomy.ComputeVM},
		Regions:    regions,
	}
}

// variantCatalog is the Scaleway fixture catalog with offering i changed by
// mutate. It keeps the target, the snapshot and every offering name and count, so
// the only difference Check can report is the changed offering.
func variantCatalog(t *testing.T, i int, mutate func(*catalog.Offering)) catalog.Catalog {
	t.Helper()

	offerings := testutil.ScalewayOfferings()
	mutate(&offerings[i])

	return newCatalog(t, scalewayTarget, offerings)
}

// catalogWithSnapshot is the Scaleway fixture catalog taken from a changed
// snapshot, so two catalogs can differ in their snapshot alone.
func catalogWithSnapshot(t *testing.T, mutate func(*catalog.Snapshot)) catalog.Catalog {
	t.Helper()

	snapshot := testutil.ScalewaySnapshot()
	mutate(&snapshot)

	cat, err := catalog.New(scalewayTarget, snapshot, testutil.ScalewayOfferings())
	if err != nil {
		t.Fatalf("catalog.New() returned unexpected error: %v", err)
	}

	return cat
}

// catalogWithoutOffering is the Scaleway fixture catalog without the offering
// named name, so two catalogs can differ by a missing offering.
func catalogWithoutOffering(t *testing.T, name string) catalog.Catalog {
	t.Helper()

	return newCatalog(t, scalewayTarget, slices.DeleteFunc(testutil.ScalewayOfferings(), func(o catalog.Offering) bool {
		return o.Name == name
	}))
}

// catalogWithOffering is the Scaleway fixture catalog with an extra offering, so
// two catalogs can differ by an offering that was added.
func catalogWithOffering(t *testing.T, extra catalog.Offering) catalog.Catalog {
	t.Helper()

	return newCatalog(t, scalewayTarget, append(testutil.ScalewayOfferings(), extra))
}

// pricedOfferings is the Scaleway fixture offerings with plans and prices on the
// Instances offering, so two catalogs can differ in a plan, a plan price or a
// storage price alone.
func pricedOfferings() []catalog.Offering {
	offerings := testutil.ScalewayOfferings()

	for i, o := range offerings {
		if o.Name != "Instances" {
			continue
		}

		offerings[i].Plans = []catalog.Plan{
			{
				Name:               "S-1-1",
				VCPUs:              1,
				MemoryMilliGiB:     1000,
				IncludedStorageGiB: 10,
				Prices: []catalog.Price{
					{Amount: 5000, Currency: catalog.EUR, Regions: []string{"fr-par"}},
					{Amount: 5500, Currency: catalog.EUR, Regions: []string{"nl-ams"}},
				},
			},
			{
				Name:           "S-2-2",
				VCPUs:          2,
				MemoryMilliGiB: 2000,
				Prices:         []catalog.Price{{Amount: 10000, Currency: catalog.EUR}},
			},
		}
		offerings[i].StoragePrices = []catalog.Price{{Amount: 40, Currency: catalog.EUR}}
	}

	return offerings
}

// pricedCatalog is the Scaleway fixture catalog with plans and prices, built
// through catalog.New like every catalog an adapter returns.
func pricedCatalog(t *testing.T) catalog.Catalog {
	t.Helper()

	return newCatalog(t, scalewayTarget, pricedOfferings())
}

// catalogWithPlanName is the priced fixture catalog with the name of its first
// plan changed, so two catalogs can differ in a plan alone.
func catalogWithPlanName(t *testing.T, name string) catalog.Catalog {
	t.Helper()

	offerings := pricedOfferings()

	for i, o := range offerings {
		if len(o.Plans) > 0 {
			offerings[i].Plans[0].Name = name
		}
	}

	return newCatalog(t, scalewayTarget, offerings)
}

// catalogWithPlanPrice is the priced fixture catalog with the first price of its
// first plan changed, so two catalogs can differ in one plan price alone.
func catalogWithPlanPrice(t *testing.T, amount catalog.Amount) catalog.Catalog {
	t.Helper()

	offerings := pricedOfferings()

	for i, o := range offerings {
		if len(o.Plans) > 0 && len(o.Plans[0].Prices) > 0 {
			offerings[i].Plans[0].Prices[0].Amount = amount
		}
	}

	return newCatalog(t, scalewayTarget, offerings)
}

// feeOfferings is the Scaleway fixture offerings with amount as the fixed
// monthly fee per instance of the Instances offering, so two catalogs can differ
// in a fixed fee alone.
func feeOfferings(amount catalog.Amount) []catalog.Offering {
	offerings := testutil.ScalewayOfferings()

	for i, o := range offerings {
		if o.Name == "Instances" {
			offerings[i].FixedFees = []catalog.Price{{Amount: amount, Currency: catalog.EUR}}
		}
	}

	return offerings
}

// catalogWithFixedFee is the Scaleway fixture catalog whose Instances offering
// charges amount as a fixed monthly fee per instance, built through catalog.New
// like every catalog an adapter returns.
func catalogWithFixedFee(t *testing.T, amount catalog.Amount) catalog.Catalog {
	t.Helper()

	return newCatalog(t, scalewayTarget, feeOfferings(amount))
}

// cappedOfferings is the priced fixture offerings with the monthly billing cap
// of the Instances offering's plans set to hours (ADR 0029), so two catalogs can
// differ in a plan billing cap alone.
func cappedOfferings(hours int64) []catalog.Offering {
	offerings := pricedOfferings()

	for i, o := range offerings {
		if o.Name == "Instances" {
			offerings[i].PlanBillingCapHours = hours
		}
	}

	return offerings
}

// catalogWithPlanBillingCap is the priced fixture catalog whose Instances
// offering bills one instance of one of its plans for at most hours a month,
// built through catalog.New like every catalog an adapter returns.
func catalogWithPlanBillingCap(t *testing.T, hours int64) catalog.Catalog {
	t.Helper()

	return newCatalog(t, scalewayTarget, cappedOfferings(hours))
}

// catalogWithStoragePrice is the priced fixture catalog with its first storage
// price changed, so two catalogs can differ in a storage price alone.
func catalogWithStoragePrice(t *testing.T, amount catalog.Amount) catalog.Catalog {
	t.Helper()

	offerings := pricedOfferings()

	for i, o := range offerings {
		if len(o.StoragePrices) > 0 {
			offerings[i].StoragePrices[0].Amount = amount
		}
	}

	return newCatalog(t, scalewayTarget, offerings)
}

// fakeTarget is an in-memory CatalogSource whose answers a test scripts. It
// returns err if set and catalogs in call order, repeating the last one, and
// records how often it was called. Unless it ignores the context, it returns
// ctx.Err() once the context is done, like a well-behaved adapter.
type fakeTarget struct {
	catalogs      []catalog.Catalog
	err           error
	ignoreContext bool
	calls         int
}

func (f *fakeTarget) Catalog(ctx context.Context) (catalog.Catalog, error) {
	f.calls++

	if !f.ignoreContext {
		if err := ctx.Err(); err != nil {
			return catalog.Catalog{}, err
		}
	}

	if f.err != nil {
		return catalog.Catalog{}, f.err
	}

	if len(f.catalogs) == 0 {
		return catalog.Catalog{}, nil
	}

	i := min(f.calls-1, len(f.catalogs)-1)

	return f.catalogs[i], nil
}

// mentions reports whether err says at least one of keywords. The suite's exact
// wording is not part of the contract; that every violation names its check is.
func mentions(err error, keywords ...string) bool {
	msg := err.Error()

	return slices.ContainsFunc(keywords, func(keyword string) bool {
		return strings.Contains(msg, keyword)
	})
}

// errHostile is an adapter error a terminal must never receive raw: it
// holds a newline and an ANSI escape sequence that clears the screen.
var errHostile = errors.New("boom\n\x1b[2J")

// hostileSourceURL is a source URL that must never be echoed: it may carry a
// credential in its query.
const hostileSourceURL = "https://example.com/catalog?token=SENTINEL-1234"

// TestCheck pins the first checks of the target contract suite: the catalog
// describes the expected target, is a real snapshot, stays inside the target's EU
// regions, serves every expected category, is stable and honours cancellation.
func TestCheck(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)
	priced := pricedCatalog(t)
	want := scalewayExpectations()

	// wantMentions is a list of alternatives: the violation must contain at
	// least one keyword of every group.
	tests := []struct {
		name         string
		tgt          *fakeTarget
		wantErr      bool
		wantMentions [][]string
		wantCancel   bool
		// wantQuoted, when set, is an adapter error the violation must show
		// quoted: the message may contain neither its raw newline nor its escape.
		wantQuoted error
		// wantAbsent lists text the violation must not echo at all.
		wantAbsent []string
		// wantOnlyQuoted lists values the violation must show only through
		// text.Quote, never as they are.
		wantOnlyQuoted []string
	}{
		{
			name: "conforming target",
			tgt:  &fakeTarget{catalogs: []catalog.Catalog{scaleway}},
		},
		{
			name:         "catalog for another target",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{newCatalog(t, "OVHcloud", testutil.ScalewayOfferings())}},
			wantErr:      true,
			wantMentions: [][]string{{"target"}},
		},
		{
			name:         "zero catalog",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{{}}},
			wantErr:      true,
			wantMentions: [][]string{{"snapshot", "source URL", "SourceURL", "retrieved", "RetrievedOn"}},
		},
		{
			name: "offering outside the EU regions",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{
				newCatalog(t, scalewayTarget, []catalog.Offering{computeOffering("Instances", "us-east-1")}),
			}},
			wantErr:      true,
			wantMentions: [][]string{{"us-east-1"}, {"Instances"}},
		},
		{
			name: "expected category not served",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{
				newCatalog(t, scalewayTarget, []catalog.Offering{
					computeOffering("Instances", "fr-par"),
					{
						Name:       "Object Storage",
						Categories: []taxonomy.Category{taxonomy.StorageObject},
						Regions:    []string{"fr-par"},
					},
					{
						Name:       "Kapsule",
						Categories: []taxonomy.Category{taxonomy.KubernetesManaged},
						Regions:    []string{"fr-par"},
					},
					{
						Name:       "Managed Database for PostgreSQL",
						Categories: []taxonomy.Category{taxonomy.DatabasePostgreSQL},
						Regions:    []string{"fr-par"},
					},
				}),
			}},
			wantErr:      true,
			wantMentions: [][]string{{"database.mysql"}},
		},
		{
			name: "changes its answer between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{
				scaleway,
				newCatalog(t, scalewayTarget, []catalog.Offering{computeOffering("Instances", "fr-par")}),
			}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat", "consecutive", "same", "equal", "stable", "state"}},
		},
		{
			name:         "ignores a cancelled context",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{scaleway}, ignoreContext: true},
			wantErr:      true,
			wantMentions: [][]string{{"cancel", "context"}},
			wantCancel:   true,
		},
		// A catalog that is stable in its shape but not in its content is not
		// stable: every field of an offering is part of the answer, so a suite that
		// compared a subset would pass a source that rewrites itself.
		{
			name: "changes one offering's categories between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{scaleway, variantCatalog(t, 5, func(o *catalog.Offering) {
				o.Categories = []taxonomy.Category{taxonomy.StorageObject, taxonomy.ComputeVM}
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		{
			name: "changes one offering's capabilities between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{scaleway, variantCatalog(t, 5, func(o *catalog.Offering) {
				o.Capabilities = []taxonomy.Capability{taxonomy.IPv6}
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		{
			name: "changes one offering's regions between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{scaleway, variantCatalog(t, 1, func(o *catalog.Offering) {
				o.Regions = []string{"fr-par", "nl-ams"}
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		// The snapshot's source URL may carry a credential, so a violation over it
		// must say what differs without echoing either URL.
		{
			name:         "changes only the snapshot source URL between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{scaleway, catalogWithSnapshot(t, func(s *catalog.Snapshot) { s.SourceURL = hostileSourceURL })}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
			wantAbsent:   []string{"SENTINEL", hostileSourceURL, testutil.ScalewaySnapshot().SourceURL},
		},
		// An adapter that stamps the retrieval date with the current time is not
		// stable: the date the data was taken is part of the answer.
		{
			name:         "changes only the snapshot retrieval date between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{scaleway, catalogWithSnapshot(t, func(s *catalog.Snapshot) { s.RetrievedOn = s.RetrievedOn.AddDate(0, 0, 1) })}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		// The target's display name is part of the catalog, so an adapter that
		// rewrites itself as another target is not stable either.
		{
			name:         "changes only the target between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{scaleway, newCatalog(t, "OVHcloud", testutil.ScalewayOfferings())}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		// Object Storage sorts after every other fixture offering, so a missing
		// offering can only be reported by the length check: a suite that compared
		// offerings pairwise would pass it.
		{
			name:         "drops the last-sorting offering between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{scaleway, catalogWithoutOffering(t, "Object Storage")}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		// An extra offering that sorts after every existing one is caught by the
		// length check on the other side, not by the pairwise compare.
		{
			name: "adds an offering that sorts last between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{scaleway, catalogWithOffering(t, catalog.Offering{
				Name:       "zz-extra-offering",
				Categories: []taxonomy.Category{taxonomy.StorageObject},
				Regions:    []string{"fr-par"},
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		// Offering names are untrusted text from the target's site: a violation
		// shows them quoted, never as they are.
		{
			name: "renames one offering between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{scaleway, variantCatalog(t, 5, func(o *catalog.Offering) {
				o.Name = "Kubernetes Engine"
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
			wantOnlyQuoted: []string{
				"Kubernetes Engine",
				"Kapsule",
			},
		},
		// An adapter that fails has still broken the contract, and the violation
		// must say so under the check it breaks.
		{
			name:         "returns an error",
			tgt:          &fakeTarget{err: errors.New("catalog fetch failed")},
			wantErr:      true,
			wantMentions: [][]string{{"catalog"}},
		},
		// An adapter error is untrusted text: it reaches a terminal through the
		// violation, so a newline or an escape sequence in it must be escaped, and
		// the error itself must stay in the chain for errors.Is.
		{
			name:         "returns an error that would corrupt a terminal",
			tgt:          &fakeTarget{err: errHostile},
			wantErr:      true,
			wantMentions: [][]string{{"catalog"}},
			wantQuoted:   errHostile,
		},
		// A catalog with plans, prices and a storage price is conforming: the
		// prices and price-regions checks accept it, so a target listing real
		// prices is not reported for them. The violating side of those two checks
		// cannot be built here: catalog.New refuses a price whose currency is not
		// EUR, whose amount is negative, whose region the offering is not in, or a
		// plan without a price, and an adapter can only return a catalog that went
		// through New.
		{
			name: "conforming target with plans and prices",
			tgt:  &fakeTarget{catalogs: []catalog.Catalog{priced}},
		},
		{
			// Estimates use the default region when the user names none, so an
			// adapter that names another one than the target declares violates the
			// contract under the default-region label alone.
			name: "default region differs from the expectation",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{catalogWithSnapshot(t, func(s *catalog.Snapshot) {
				s.DefaultRegion = "pl-waw"
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"default-region"}},
			wantAbsent:   []string{"regions:", "prices:", "price-regions:", "repeat:"},
		},
		{
			// The default region is one of the regions the target declares, or the
			// report would estimate in a region the target does not serve.
			name: "default region outside the expected regions",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{catalogWithSnapshot(t, func(s *catalog.Snapshot) {
				s.DefaultRegion = "eu-north"
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"default-region"}, {"eu-north"}},
			wantAbsent:   []string{"regions:", "prices:", "price-regions:", "repeat:"},
		},
		// Plans and prices are part of the catalog's content, so an adapter that
		// rewrites a plan name, a plan price or a storage price between calls is
		// not stable.
		{
			name:         "changes one offering's plans between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{priced, catalogWithPlanName(t, "S-0-0")}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		{
			name:         "changes one offering's plan prices between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{priced, catalogWithPlanPrice(t, 1)}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		{
			name:         "changes one offering's storage prices between calls",
			tgt:          &fakeTarget{catalogs: []catalog.Catalog{priced, catalogWithStoragePrice(t, 1)}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
		// A fixed monthly fee per instance is part of an offering, so an adapter
		// that rewrites the amount alone between calls is not stable. The violation
		// names the fee: both catalogues' FixedFees are shown as Amount.String
		// renders them.
		{
			name: "changes one offering's fixed fee between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{
				catalogWithFixedFee(t, 150000),
				catalogWithFixedFee(t, 250000),
			}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}, {"FixedFees"}, {"15.00"}, {"25.00"}},
		},
		// A plan billing cap is part of an offering too, so an adapter that
		// rewrites the cap alone between calls is not stable (ADR 0029). The
		// violation shows it: both catalogs' PlanBillingCapHours appear as describe
		// renders them.
		{
			name: "changes one offering's plan billing cap between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{
				catalogWithPlanBillingCap(t, 672),
				catalogWithPlanBillingCap(t, 744),
			}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}, {"PlanBillingCapHours: 672"}, {"PlanBillingCapHours: 744"}},
		},
		{
			// The default region is part of the snapshot the catalog describes, so a
			// catalog that changes it between calls is not stable either.
			name: "changes only the default region between calls",
			tgt: &fakeTarget{catalogs: []catalog.Catalog{scaleway, catalogWithSnapshot(t, func(s *catalog.Snapshot) {
				s.DefaultRegion = "nl-ams"
			})}},
			wantErr:      true,
			wantMentions: [][]string{{"repeat"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := targettest.Check(t.Context(), tt.tgt, want)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Check() returned unexpected error: %v", err)
				}

				if tt.tgt.calls < 2 {
					t.Errorf("Check() called Catalog() %d times, want at least 2 to check stability", tt.tgt.calls)
				}

				return
			}

			if err == nil {
				t.Fatal("Check() returned nil, want an error naming the violated check")
			}

			for i, group := range tt.wantMentions {
				if !mentions(err, group...) {
					t.Errorf("Check() error = %q, want it to mention violation %d (one of %v)", err, i+1, group)
				}
			}

			if got := errors.Is(err, context.Canceled); got != tt.wantCancel {
				t.Errorf("Check() error wraps context.Canceled = %t, want %t (error: %v)", got, tt.wantCancel, err)
			}

			msg := err.Error()

			for _, absent := range tt.wantAbsent {
				if strings.Contains(msg, absent) {
					t.Errorf("Check() error = %q, want it not to contain %q", msg, absent)
				}
			}

			for _, value := range tt.wantOnlyQuoted {
				quoted := text.Quote(value)

				if !strings.Contains(msg, quoted) {
					t.Errorf("Check() error = %q, want it to contain the quoted value %s", msg, quoted)
				}

				if rest := strings.ReplaceAll(msg, quoted, ""); strings.Contains(rest, value) {
					t.Errorf("Check() error = %q, want %q only in its quoted form %s", msg, value, quoted)
				}
			}

			if tt.wantQuoted != nil {
				if strings.Contains(msg, "\x1b") {
					t.Errorf("Check() error = %q, want the adapter error's escape sequence escaped", msg)
				}

				// violations are joined with a newline, so an adapter error echoed
				// raw would show its own newline directly after its text.
				if strings.Contains(msg, "boom\n") {
					t.Errorf("Check() error = %q, want the adapter error's newline escaped", msg)
				}

				if quoted := text.Quote(tt.wantQuoted.Error()); !strings.Contains(msg, quoted) {
					t.Errorf("Check() error = %q, want it to contain the quoted adapter error %s", msg, quoted)
				}

				if !errors.Is(err, tt.wantQuoted) {
					t.Errorf("Check() error = %v, want it to wrap the adapter error %v", err, tt.wantQuoted)
				}
			}
		})
	}
}

// TestCheckExpectations pins that a suite with nothing to compare against says
// so instead of passing vacuously: the expectations are part of the contract, and
// an adapter author who forgot one learns which.
func TestCheckExpectations(t *testing.T) {
	t.Parallel()

	scaleway := testutil.ScalewayCatalog(t)
	full := scalewayExpectations()

	tests := []struct {
		name string
		want targettest.Expectations
		// wantField is the empty field the violation must name; empty when the
		// expectations are complete.
		wantField string
		// wantNoViolation is a check that must not report a violation of its own:
		// an empty expectation is the expectations violation, not a reason to
		// repeat it for every offering.
		wantNoViolation string
	}{
		{name: "every field set", want: full},
		{
			name:      "missing target",
			want:      targettest.Expectations{Regions: full.Regions, Categories: full.Categories},
			wantField: "target",
		},
		{
			name:            "missing regions",
			want:            targettest.Expectations{Target: full.Target, Categories: full.Categories},
			wantField:       "regions",
			wantNoViolation: "regions",
		},
		{
			name:      "missing categories",
			want:      targettest.Expectations{Target: full.Target, Regions: full.Regions},
			wantField: "categories",
		},
		{
			// Estimates fall back to the target's default region, so a suite without
			// one has nothing to compare the catalog's default region against.
			name:            "missing default region",
			want:            targettest.Expectations{Target: full.Target, Regions: full.Regions, Categories: full.Categories},
			wantField:       "defaultregion",
			wantNoViolation: "default-region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := targettest.Check(t.Context(), &fakeTarget{catalogs: []catalog.Catalog{scaleway}}, tt.want)

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Check() returned unexpected error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("Check() returned nil, want an error naming the missing expectation")
			}

			msg := strings.ToLower(err.Error())

			for _, keyword := range []string{"expectations", tt.wantField} {
				if !strings.Contains(msg, keyword) {
					t.Errorf("Check() error = %q, want it to mention %q", err, keyword)
				}
			}

			if tt.wantNoViolation == "" {
				return
			}

			// Every violation line starts with the name of the check it breaks, so
			// the field name inside the expectations line does not count.
			for _, line := range strings.Split(msg, "\n") {
				if strings.HasPrefix(line, tt.wantNoViolation+":") {
					t.Errorf("Check() error = %q, want no %q violation line, only the expectations one",
						err, tt.wantNoViolation)
				}
			}
		})
	}
}

// TestRun pins that a conforming target passes the suite.
func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("conforming target", func(t *testing.T) {
		t.Parallel()

		targettest.Run(t, &fakeTarget{catalogs: []catalog.Catalog{testutil.ScalewayCatalog(t)}}, scalewayExpectations())
	})
}
