package web_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/version"
	"github.com/timothymamo/shift2eu/internal/web"
)

// The fakes below stand in for every adapter the page's composition root wires:
// the inventory reader over bytes, two target catalogs and three report formats.
// The web adapter runs the real use cases over them, so a test observes what the
// page would receive.

// The target keys the fake Deps supports, sorted as web must sort them.
const (
	alphaKey = "alpha"
	betaKey  = "beta"
	// fakeRegion is the one region both fake catalogs list.
	fakeRegion = "xx-par"
	// marker is the text an inventory error the page must never echo.
	marker = "MARKER-do-not-echo-4f5b"
)

// fakeInventorySource counts how often the inventory is read, which tells bad
// options (checked before anything is read) from a refused document.
type fakeInventorySource struct {
	inventory inventory.Inventory
	err       error
	reads     int
}

// Inventory returns the fixture inventory, the fake error, or ctx.Err(), and
// counts the call.
func (s *fakeInventorySource) Inventory(ctx context.Context) (inventory.Inventory, error) {
	s.reads++

	if s.err != nil {
		return inventory.Inventory{}, s.err
	}

	return s.inventory, ctx.Err()
}

// fakeCatalogSource is a target catalog with one offering that serves
// compute.vm in fakeRegion.
type fakeCatalogSource struct {
	target string
}

// Catalog returns the fake catalog.
func (s *fakeCatalogSource) Catalog(ctx context.Context) (catalog.Catalog, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Catalog{}, err
	}

	cat, err := catalog.New(s.target, catalog.Snapshot{
		SourceURL:     "https://example.com/catalog",
		RetrievedOn:   time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
		DefaultRegion: fakeRegion,
	}, []catalog.Offering{
		{
			Name:       "Example Virtual Machines",
			Categories: []taxonomy.Category{taxonomy.ComputeVM},
			Regions:    []string{fakeRegion},
		},
	})
	if err != nil {
		return catalog.Catalog{}, err
	}

	return cat, nil
}

// recordingReporter writes a marker for the format it stands for and records
// what it was asked to render.
type recordingReporter struct {
	kind        string
	schema      int
	assessments []assessment.Assessment
	comparisons []assessment.Comparison
	metas       []analysis.ReportMeta
}

// Report writes the assessment marker and records a and meta.
func (r *recordingReporter) Report(_ context.Context, w io.Writer, a assessment.Assessment, meta analysis.ReportMeta) error {
	r.assessments = append(r.assessments, a)
	r.metas = append(r.metas, meta)

	_, err := io.WriteString(w, "assessment:"+r.kind+"\n")

	return err
}

// ReportComparison writes the comparison marker and records c and meta.
func (r *recordingReporter) ReportComparison(_ context.Context, w io.Writer, c assessment.Comparison,
	meta analysis.ReportMeta,
) error {
	r.comparisons = append(r.comparisons, c)
	r.metas = append(r.metas, meta)

	_, err := io.WriteString(w, "comparison:"+r.kind+"\n")

	return err
}

// formatFakes builds the reporters of one format and records what the web
// adapter asked for: the schemaVersion of each constructor and the reports it
// rendered.
type formatFakes struct {
	kind string
	// versions and comparisonVersions are the schemaVersions the constructors
	// were called with, in call order.
	versions           []int
	comparisonVersions []int
	reports            []*recordingReporter
	comparisons        []*recordingReporter
}

// format returns the port value the web Deps takes for this format.
func (f *formatFakes) format() web.Format {
	return web.Format{
		NewReporter: func(schemaVersion int) analysis.Reporter {
			f.versions = append(f.versions, schemaVersion)

			reporter := &recordingReporter{kind: f.kind, schema: schemaVersion}
			f.reports = append(f.reports, reporter)

			return reporter
		},
		NewComparisonReporter: func(schemaVersion int) analysis.ComparisonReporter {
			f.comparisonVersions = append(f.comparisonVersions, schemaVersion)

			reporter := &recordingReporter{kind: f.kind, schema: schemaVersion}
			f.comparisons = append(f.comparisons, reporter)

			return reporter
		},
	}
}

// fixture is one web adapter over fakes, with what the fakes saw.
type fixture struct {
	assessor  *web.Assessor
	inventory *fakeInventorySource
	markdown  *formatFakes
	json      *formatFakes
	html      *formatFakes
}

// newFixture returns the fixture with one compute.vm resource, so an assessment
// with estimates on has something to price, and the two fake catalogs.
func newFixture(tb testing.TB) *fixture {
	tb.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{
		{
			ID:       "vm-example",
			RawType:  "AWS::EC2::Instance",
			Category: taxonomy.ComputeVM,
			Region:   "eu-west-1",
		},
	})
	if err != nil {
		tb.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	f := &fixture{
		inventory: &fakeInventorySource{inventory: inv},
		markdown:  &formatFakes{kind: "markdown"},
		json:      &formatFakes{kind: "json"},
		html:      &formatFakes{kind: "html"},
	}

	deps := web.Deps{
		NewInventorySource: func([]byte) analysis.InventorySource { return f.inventory },
		Targets: map[string]func() analysis.CatalogSource{
			alphaKey: func() analysis.CatalogSource { return &fakeCatalogSource{target: "Alpha Cloud"} },
			betaKey:  func() analysis.CatalogSource { return &fakeCatalogSource{target: "Beta Cloud"} },
		},
		Markdown: f.markdown.format(),
		JSON:     f.json.format(),
		HTML:     f.html.format(),
	}

	assessor, err := web.New(deps)
	if err != nil {
		tb.Fatalf("web.New() returned unexpected error: %v", err)
	}

	f.assessor = assessor

	return f
}

// TestNewRefusesAnIncompleteDeps pins that New refuses a Deps without an adapter
// the page needs: a wiring mistake in the composition root.
func TestNewRefusesAnIncompleteDeps(t *testing.T) {
	t.Parallel()

	complete := func() web.Deps {
		f := newFixture(t)

		return web.Deps{
			NewInventorySource: func([]byte) analysis.InventorySource { return f.inventory },
			Targets: map[string]func() analysis.CatalogSource{
				alphaKey: func() analysis.CatalogSource { return &fakeCatalogSource{target: "Alpha Cloud"} },
			},
			Markdown: f.markdown.format(),
			JSON:     f.json.format(),
			HTML:     f.html.format(),
		}
	}

	tests := []struct {
		name   string
		mutate func(*web.Deps)
	}{
		{
			name:   "no inventory reader",
			mutate: func(d *web.Deps) { d.NewInventorySource = nil },
		},
		{
			name:   "no target",
			mutate: func(d *web.Deps) { d.Targets = nil },
		},
		{
			name:   "a nil target constructor",
			mutate: func(d *web.Deps) { d.Targets = map[string]func() analysis.CatalogSource{alphaKey: nil} },
		},
		{
			name:   "no Markdown reporter",
			mutate: func(d *web.Deps) { d.Markdown.NewReporter = nil },
		},
		{
			name:   "no Markdown comparison reporter",
			mutate: func(d *web.Deps) { d.Markdown.NewComparisonReporter = nil },
		},
		{
			name:   "no JSON reporter",
			mutate: func(d *web.Deps) { d.JSON = web.Format{} },
		},
		{
			name:   "no HTML reporter",
			mutate: func(d *web.Deps) { d.HTML.NewReporter = nil },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			deps := complete()
			tc.mutate(&deps)

			if _, err := web.New(deps); err == nil {
				t.Error("web.New() returned no error for an incomplete Deps, want it refused")
			}
		})
	}
}

// TestAssessReturnsEveryFormat pins that one assessment answers the page with
// the Markdown report, the JSON report and the HTML fragment from their own
// reporters, for a single target and for a comparison of several.
func TestAssessReturnsEveryFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// targets is the page's choice, and the keys the comparison must hold.
		targets []string
		want    web.Result
		// wantCompared is whether the comparison reporters rendered the report.
		wantCompared bool
	}{
		{
			name:    "one target",
			targets: []string{alphaKey},
			want: web.Result{
				Markdown: []byte("assessment:markdown\n"),
				JSON:     []byte("assessment:json\n"),
				HTML:     []byte("assessment:html\n"),
			},
		},
		{
			name:    "several targets",
			targets: []string{alphaKey, betaKey},
			want: web.Result{
				Markdown: []byte("comparison:markdown\n"),
				JSON:     []byte("comparison:json\n"),
				HTML:     []byte("comparison:html\n"),
			},
			wantCompared: true,
		},
		{
			name:    "every target",
			targets: []string{analysis.AllTargets},
			want: web.Result{
				Markdown: []byte("comparison:markdown\n"),
				JSON:     []byte("comparison:json\n"),
				HTML:     []byte("comparison:html\n"),
			},
			wantCompared: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)

			got, err := f.assessor.Assess(t.Context(), []byte(`{}`), web.Options{
				Targets:       tc.targets,
				InventoryName: "inventory.json",
			})
			if err != nil {
				t.Fatalf("Assess() returned unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Assess() (-want +got):\n%s", diff)
			}

			if f.inventory.reads != 1 {
				t.Errorf("Assess() read the inventory %d times, want once", f.inventory.reads)
			}

			for _, format := range []*formatFakes{f.markdown, f.json, f.html} {
				if tc.wantCompared && len(format.reports) != 0 {
					t.Errorf("Assess() rendered a single-target %s report, want the comparison one", format.kind)
				}

				if !tc.wantCompared && len(format.comparisons) != 0 {
					t.Errorf("Assess() rendered a %s comparison, want the single-target report", format.kind)
				}
			}
		})
	}
}

// TestAssessRefusesBadOptions pins that the page's choices are checked by the
// application's option rules before the inventory is read, with a usage error
// whose message is the CLI's, without the CLI's prefix or its usage text.
func TestAssessRefusesBadOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts web.Options
		want string
	}{
		{
			name: "no target chosen",
			opts: web.Options{},
			want: "assess needs --target",
		},
		{
			name: "an unknown target",
			opts: web.Options{Targets: []string{"nowhere"}},
			want: `unknown target "nowhere"; supported targets: alpha, beta`,
		},
		{
			name: "an empty target key",
			opts: web.Options{Targets: []string{alphaKey, ""}},
			want: `--target "alpha," has an empty target; supported targets: alpha, beta`,
		},
		{
			name: "every target beside another",
			opts: web.Options{Targets: []string{analysis.AllTargets, alphaKey}},
			want: `--target "all" cannot be combined with other targets; supported targets: alpha, beta`,
		},
		{
			name: "a target given more than once",
			opts: web.Options{Targets: []string{alphaKey, alphaKey}},
			want: `target "alpha" given more than once; supported targets: alpha, beta`,
		},
		{
			name: "a region for a target not selected",
			opts: web.Options{
				Targets: []string{alphaKey},
				Regions: []web.Region{{Target: betaKey, Region: fakeRegion}},
			},
			want: `--region names target "beta", which --target does not select; selected targets: alpha`,
		},
		{
			name: "a region given for one target more than once",
			opts: web.Options{
				Targets: []string{alphaKey, betaKey},
				Regions: []web.Region{{Target: alphaKey, Region: fakeRegion}, {Target: alphaKey, Region: fakeRegion}},
			},
			want: `--region names target "alpha" more than once`,
		},
		{
			name: "an empty region",
			opts: web.Options{
				Targets: []string{alphaKey},
				Regions: []web.Region{{Target: alphaKey}},
			},
			want: `--region "alpha=" is not <target>=<region>; selected targets: alpha`,
		},
		{
			name: "an unknown schema version",
			opts: web.Options{
				Targets:       []string{alphaKey},
				SchemaVersion: "4",
			},
			want: `--schema-version: unknown schema version "4"; supported schema versions: 2, 3`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)

			_, err := f.assessor.Assess(t.Context(), []byte(`{}`), tc.opts)
			if err == nil {
				t.Fatalf("Assess(%+v) returned no error, want a usage error", tc.opts)
			}

			var usage *web.Error
			if !errors.As(err, &usage) {
				t.Fatalf("Assess(%+v) returned %T, want a *web.Error", tc.opts, err)
			}

			if usage.Kind != web.KindUsage {
				t.Errorf("Assess(%+v) returned Kind %v, want KindUsage", tc.opts, usage.Kind)
			}

			if usage.Error() != tc.want {
				t.Errorf("Assess(%+v) returned the message %q, want the CLI's %q", tc.opts, usage.Error(), tc.want)
			}

			if strings.HasPrefix(usage.Error(), "shift2eu: ") {
				t.Errorf("the message %q carries the CLI's prefix, which the page shows as it is", usage.Error())
			}

			if f.inventory.reads != 0 {
				t.Errorf("Assess(%+v) read the inventory %d times, want 0: a bad choice reads nothing",
					tc.opts, f.inventory.reads)
			}
		})
	}
}

// TestAssessRefusesAnUnknownRegion pins that a region a target's catalog does
// not list is a usage error, as `--region` is, found before the inventory is
// read.
func TestAssessRefusesAnUnknownRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets []string
		want    string
	}{
		{
			name:    "one target",
			targets: []string{alphaKey},
			want:    `--region: region "xx-nope" is not one of the target's regions: "xx-par"`,
		},
		{
			name:    "a comparison names the target",
			targets: []string{alphaKey, betaKey},
			want:    `--region: target "beta": region "xx-nope" is not one of the target's regions: "xx-par"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)

			_, err := f.assessor.Assess(t.Context(), []byte(`{}`), web.Options{
				Targets: tc.targets,
				Regions: []web.Region{{Target: betaKey, Region: "xx-nope"}},
			})
			if err == nil {
				t.Fatal("Assess() returned no error for an unknown region, want a usage error")
			}

			var usage *web.Error
			if !errors.As(err, &usage) || usage.Kind != web.KindUsage {
				t.Fatalf("Assess() returned %v (%T), want a KindUsage *web.Error", err, err)
			}

			if usage.Error() != tc.want {
				t.Errorf("Assess() returned the message %q, want %q", usage.Error(), tc.want)
			}

			if f.inventory.reads != 0 {
				t.Errorf("Assess() read the inventory %d times, want 0: an unknown region reads nothing",
					f.inventory.reads)
			}
		})
	}
}

// TestAssessReportsAnInventoryError pins that a document the inventory reader
// refuses is a KindInventory error whose message never repeats a value from the
// document.
func TestAssessReportsAnInventoryError(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.inventory.err = errors.New("inventory document: " + marker + ": not a valid inventory document")

	_, err := f.assessor.Assess(t.Context(), []byte(`{}`), web.Options{Targets: []string{alphaKey}})
	if err == nil {
		t.Fatal("Assess() returned no error for a refused inventory, want a KindInventory error")
	}

	var inventoryErr *web.Error
	if !errors.As(err, &inventoryErr) {
		t.Fatalf("Assess() returned %T, want a *web.Error", err)
	}

	if inventoryErr.Kind != web.KindInventory {
		t.Errorf("Assess() returned Kind %v, want KindInventory", inventoryErr.Kind)
	}

	if !strings.Contains(inventoryErr.Error(), "not a valid inventory document") {
		t.Errorf("the message %q does not say the document is invalid, so the page shows no reason", inventoryErr.Error())
	}
}

// TestAssessHonoursACancelledContext pins that a cancelled run returns an error
// wrapping ctx.Err() rather than a report.
func TestAssessHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := f.assessor.Assess(ctx, []byte(`{}`), web.Options{Targets: []string{alphaKey}})
	if err == nil {
		t.Fatalf("Assess() with a cancelled context returned the result %+v, want an error", got)
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Assess() with a cancelled context returned %v, want an error wrapping context.Canceled", err)
	}

	if len(got.Markdown) != 0 || len(got.JSON) != 0 || len(got.HTML) != 0 {
		t.Errorf("Assess() with a cancelled context returned %+v, want no report", got)
	}
}

// TestAssessPassesTheSchemaVersion pins that the JSON reporter is built with the
// schemaVersion the page chose, the default 3 when it names none, and that a
// format without a schemaVersion is built with 0, as the CLI's reporters are.
func TestAssessPassesTheSchemaVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    int
	}{
		{name: "the default", version: "", want: analysis.DefaultSchemaVersion},
		{name: "version 2", version: "2", want: 2},
		{name: "version 3", version: "3", want: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)

			if _, err := f.assessor.Assess(t.Context(), []byte(`{}`), web.Options{
				Targets:       []string{alphaKey},
				SchemaVersion: tc.version,
			}); err != nil {
				t.Fatalf("Assess() returned unexpected error: %v", err)
			}

			if diff := cmp.Diff([]int{tc.want}, f.json.versions); diff != "" {
				t.Errorf("the JSON reporter was built with (-want +got):\n%s", diff)
			}

			for _, format := range []*formatFakes{f.markdown, f.html} {
				if diff := cmp.Diff([]int{0}, format.versions); diff != "" {
					t.Errorf("the %s reporter was built with (-want +got):\n%s", format.kind, diff)
				}
			}
		})
	}
}

// TestAssessPassesTheEstimateChoice pins that the page's estimate switch reaches
// the assessment: on, every verdict has an estimate and the assessment a total;
// off, neither does, as `--no-estimate` leaves them out.
func TestAssessPassesTheEstimateChoice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		estimate bool
	}{
		{name: "estimates on", estimate: true},
		{name: "estimates off", estimate: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)

			if _, err := f.assessor.Assess(t.Context(), []byte(`{}`), web.Options{
				Targets:  []string{alphaKey},
				Estimate: tc.estimate,
			}); err != nil {
				t.Fatalf("Assess() returned unexpected error: %v", err)
			}

			if len(f.markdown.reports) != 1 {
				t.Fatalf("the Markdown reporter rendered %d reports, want 1", len(f.markdown.reports))
			}

			a := f.markdown.reports[0].assessments[0]

			if len(a.Verdicts) != 1 {
				t.Fatalf("the assessment holds %d verdicts, want 1", len(a.Verdicts))
			}

			switch {
			case tc.estimate && a.Verdicts[0].Estimate == nil:
				t.Error("the verdict carries no estimate, want estimates on")
			case tc.estimate && a.Total == nil:
				t.Error("the assessment carries no total, want estimates on")
			case !tc.estimate && a.Verdicts[0].Estimate != nil:
				t.Error("the verdict carries an estimate, want estimates off")
			case !tc.estimate && a.Total != nil:
				t.Error("the assessment carries a total, want estimates off")
			}
		})
	}
}

// TestVersion pins that the page shows the same version `shift2eu version`
// prints.
func TestVersion(t *testing.T) {
	t.Parallel()

	if got, want := web.Version(), version.String(); got != want {
		t.Errorf("web.Version() = %q, want %q", got, want)
	}
}
