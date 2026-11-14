package targettest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// Expectations is what a target adapter declares about the catalog it must
// produce. Every field is required; Check reports an expectations violation for
// an empty one.
type Expectations struct {
	// Target is the target's display name the catalog must carry, for example
	// Scaleway.
	Target string
	// Regions lists every EU region the target's offerings may be available in.
	// An offering in any other region violates the contract.
	Regions []string
	// Categories lists the taxonomy categories the catalog must serve, each by at
	// least one offering.
	Categories []taxonomy.Category
	// DefaultRegion is the region the catalog's snapshot must name as the one
	// estimates use when the user names none. Check reports a default-region
	// violation when the snapshot's default region is not one of Regions.
	DefaultRegion string
}

// Check runs the target contract against src and returns every violation it
// finds, joined with errors.Join, or nil if src conforms. Each violation starts
// with the name of the check it breaks:
//
//   - expectations: every field of want is set;
//   - catalog: Catalog returns without error;
//   - target: the catalog's Target is want.Target;
//   - snapshot: the catalog's snapshot has a source URL and a retrieval date;
//   - default-region: the snapshot's default region is want.DefaultRegion
//     (skipped when want.DefaultRegion is empty, which is already an
//     expectations violation) and one of want.Regions (skipped when
//     want.Regions is empty);
//   - regions: every offering is available only in want.Regions (skipped
//     when want.Regions is empty, which is already an expectations violation);
//   - category: every category of want.Categories is served;
//   - repeat: a second call returns the same catalog as the first, plans,
//     prices, storage prices and the default region included;
//   - cancel: a call with a cancelled context returns an error wrapping
//     context.Canceled (the violation wraps context.Canceled too).
//
// The contract has no check of its own that every plan is priced in EUR at an
// amount of at least 0, or that a price applies only to its offering's regions:
// a catalog.Catalog is built only by catalog.New, which already refuses both
// (ADR 0024).
//
// An error from src is shown through text.Quote and stays in the violation's
// chain.
func Check(ctx context.Context, src analysis.CatalogSource, want Expectations) error {
	errs := checkExpectations(want)

	first, firstErr := src.Catalog(ctx)
	if firstErr != nil {
		errs = append(errs, fmt.Errorf("catalog: Catalog() returned error: %w", adapterError{firstErr}))
	} else {
		errs = append(errs, checkCatalog(first, want)...)
	}

	second, secondErr := src.Catalog(ctx)
	if secondErr != nil {
		errs = append(errs, fmt.Errorf("repeat: second Catalog() call returned error: %w", adapterError{secondErr}))
	} else if firstErr == nil {
		if d := diff(second, first); d != "" {
			errs = append(errs, fmt.Errorf("repeat: second Catalog() call differs from the first (want): %s", d))
		}
	}

	if err := checkCancel(ctx, src); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// Run runs Check with the test's context and fails the test with every
// violation if src does not conform.
func Run(t *testing.T, src analysis.CatalogSource, want Expectations) {
	t.Helper()

	if err := Check(t.Context(), src, want); err != nil {
		t.Fatalf("target contract violated:\n%v", err)
	}
}

// checkExpectations returns an expectations violation for every empty field
// of want, naming the field: a check with nothing to compare against would pass
// vacuously.
func checkExpectations(want Expectations) []error {
	var errs []error

	if want.Target == "" {
		errs = append(errs, errors.New("expectations: Target is empty, want the target's display name"))
	}

	if len(want.Regions) == 0 {
		errs = append(errs, errors.New("expectations: Regions is empty, want every EU region the offerings may be in"))
	}

	if len(want.Categories) == 0 {
		errs = append(errs, errors.New("expectations: Categories is empty, want every category the catalog must serve"))
	}

	if want.DefaultRegion == "" {
		errs = append(errs, errors.New("expectations: DefaultRegion is empty, want the region estimates use by default"))
	}

	return errs
}

// checkCatalog returns the violations of the target, snapshot, default-region,
// regions and category checks by cat.
func checkCatalog(cat catalog.Catalog, want Expectations) []error {
	var errs []error

	if cat.Target() != want.Target {
		errs = append(errs, fmt.Errorf("target: Target() = %s, want %s", text.Quote(cat.Target()), text.Quote(want.Target)))
	}

	snapshot := cat.Snapshot()

	if snapshot.SourceURL == "" {
		errs = append(errs, errors.New("snapshot: SourceURL is empty, want the page or API the catalog was taken from"))
	}

	if snapshot.RetrievedOn.IsZero() {
		errs = append(errs, errors.New("snapshot: RetrievedOn is zero, want the date the catalog was retrieved"))
	}

	errs = append(errs, checkDefaultRegion(snapshot.DefaultRegion, want)...)
	errs = append(errs, checkRegions(cat, want.Regions)...)

	for _, category := range want.Categories {
		if len(cat.OfferingsFor(category)) == 0 {
			errs = append(errs, fmt.Errorf("category: no offering serves %s", text.Quote(string(category))))
		}
	}

	return errs
}

// checkDefaultRegion returns a default-region violation when region, the
// catalog's default region, is not want.DefaultRegion or not one of
// want.Regions. An empty expectation skips its half of the check: it is already
// an expectations violation.
func checkDefaultRegion(region string, want Expectations) []error {
	var errs []error

	if want.DefaultRegion != "" && region != want.DefaultRegion {
		errs = append(errs, fmt.Errorf("default-region: Snapshot().DefaultRegion = %s, want %s",
			text.Quote(region), text.Quote(want.DefaultRegion)))
	}

	if len(want.Regions) > 0 && !slices.Contains(want.Regions, region) {
		errs = append(errs, fmt.Errorf("default-region: Snapshot().DefaultRegion = %s, want one of %s",
			text.Quote(region), quoteAll(want.Regions)))
	}

	return errs
}

// checkRegions returns a regions violation for every offering region of cat
// outside allowed. With no allowed regions it returns nothing: the empty
// expectation is already an expectations violation, and repeating it for every
// offering region would only bury it.
func checkRegions(cat catalog.Catalog, allowed []string) []error {
	if len(allowed) == 0 {
		return nil
	}

	var errs []error

	for _, o := range cat.Offerings() {
		for _, region := range o.Regions {
			if !slices.Contains(allowed, region) {
				errs = append(errs, fmt.Errorf("regions: offering %s is available in region %s, want one of %s",
					text.Quote(o.Name), text.Quote(region), quoteAll(allowed)))
			}
		}
	}

	return errs
}

// adapterError is an error returned by the adapter under test, as a violation
// shows it: Error quotes the adapter's message through text.Quote, so a newline
// or an escape sequence in it never reaches a terminal raw, and Unwrap keeps the
// adapter's error in the chain for errors.Is and errors.As.
type adapterError struct {
	err error
}

// Error returns the adapter's message through text.Quote.
func (e adapterError) Error() string {
	return text.Quote(e.err.Error())
}

// Unwrap returns the adapter's error.
func (e adapterError) Unwrap() error {
	return e.err
}

// checkCancel calls src with an already-cancelled context and returns a
// violation wrapping context.Canceled unless src returns an error wrapping it.
func checkCancel(ctx context.Context, src analysis.CatalogSource) error {
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	_, err := src.Catalog(cancelled)

	switch {
	case err == nil:
		return fmt.Errorf("cancel: Catalog() with a cancelled context returned no error, want one wrapping %w",
			context.Canceled)
	case !errors.Is(err, context.Canceled):
		return fmt.Errorf("cancel: Catalog() with a cancelled context returned error %w, want one wrapping %w",
			adapterError{err}, context.Canceled)
	default:
		return nil
	}
}

// diff describes the first difference between got and want, or returns "" if
// they are equal. Values are shown through text.Quote.
func diff(got, want catalog.Catalog) string {
	if got.Target() != want.Target() {
		return fmt.Sprintf("target %s, want %s", text.Quote(got.Target()), text.Quote(want.Target()))
	}

	gs, ws := got.Snapshot(), want.Snapshot()

	if gs.SourceURL != ws.SourceURL {
		return "snapshot source URLs differ"
	}

	if !gs.RetrievedOn.Equal(ws.RetrievedOn) {
		return fmt.Sprintf("snapshot retrieved on %s, want %s",
			gs.RetrievedOn.Format(time.RFC3339), ws.RetrievedOn.Format(time.RFC3339))
	}

	if gs.DefaultRegion != ws.DefaultRegion {
		return fmt.Sprintf("snapshot default region %s, want %s", text.Quote(gs.DefaultRegion), text.Quote(ws.DefaultRegion))
	}

	g, w := got.Offerings(), want.Offerings()

	for i := range max(len(g), len(w)) {
		switch {
		case i >= len(g):
			return fmt.Sprintf("offering %s missing, want %d offerings, got %d", text.Quote(w[i].Name), len(w), len(g))
		case i >= len(w):
			return fmt.Sprintf("unexpected offering %s, want %d offerings, got %d", text.Quote(g[i].Name), len(w), len(g))
		case !equalOffering(g[i], w[i]):
			return fmt.Sprintf("offerings[%d] = %s, want %s", i, describe(g[i]), describe(w[i]))
		}
	}

	return ""
}

// equalOffering reports whether a and b are the same offering, field by field,
// plans, prices, storage prices, fixed fees and plan billing cap included.
func equalOffering(a, b catalog.Offering) bool {
	return a.Name == b.Name &&
		slices.Equal(a.Categories, b.Categories) &&
		slices.Equal(a.Capabilities, b.Capabilities) &&
		slices.Equal(a.Regions, b.Regions) &&
		slices.EqualFunc(a.Plans, b.Plans, equalPlan) &&
		slices.EqualFunc(a.StoragePrices, b.StoragePrices, equalPrice) &&
		slices.EqualFunc(a.FixedFees, b.FixedFees, equalPrice) &&
		a.PlanBillingCapHours == b.PlanBillingCapHours
}

// equalPlan reports whether a and b are the same plan, prices included.
func equalPlan(a, b catalog.Plan) bool {
	return a.Name == b.Name &&
		a.VCPUs == b.VCPUs &&
		a.MemoryMilliGiB == b.MemoryMilliGiB &&
		a.IncludedStorageGiB == b.IncludedStorageGiB &&
		slices.EqualFunc(a.Prices, b.Prices, equalPrice)
}

// equalPrice reports whether a and b are the same price.
func equalPrice(a, b catalog.Price) bool {
	return a.Amount == b.Amount && a.Currency == b.Currency && slices.Equal(a.Regions, b.Regions)
}

// describe formats o for a violation message, quoting every value.
func describe(o catalog.Offering) string {
	return fmt.Sprintf("{Name: %s, Categories: %s, Capabilities: %s, Regions: %s, Plans: %s, StoragePrices: %s, FixedFees: %s, "+
		"PlanBillingCapHours: %d}",
		text.Quote(o.Name), quoteAll(o.Categories), quoteAll(o.Capabilities), quoteAll(o.Regions),
		describePlans(o.Plans), describePrices(o.StoragePrices), describePrices(o.FixedFees), o.PlanBillingCapHours)
}

// describePlans formats plans for a violation message, quoting every value.
func describePlans(plans []catalog.Plan) string {
	described := make([]string, len(plans))
	for i, p := range plans {
		described[i] = fmt.Sprintf("{Name: %s, VCPUs: %d, MemoryMilliGiB: %d, IncludedStorageGiB: %d, Prices: %s}",
			text.Quote(p.Name), p.VCPUs, p.MemoryMilliGiB, p.IncludedStorageGiB, describePrices(p.Prices))
	}

	return fmt.Sprint(described)
}

// describePrices formats prices for a violation message, quoting every value.
func describePrices(prices []catalog.Price) string {
	described := make([]string, len(prices))
	for i, p := range prices {
		described[i] = fmt.Sprintf("{Amount: %s, Currency: %s, Regions: %s}",
			p.Amount, text.Quote(p.Currency), quoteAll(p.Regions))
	}

	return fmt.Sprint(described)
}

// quoteAll formats values as a bracketed list, each value through text.Quote.
func quoteAll[S ~string](values []S) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = text.Quote(string(v))
	}

	return fmt.Sprint(quoted)
}
