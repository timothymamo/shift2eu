package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/version"
)

// Deps are the adapters the web adapter runs with. The composition root
// (cmd/shift2eu-web, through its wire package) builds them; the web adapter
// never constructs an adapter itself. New refuses a Deps that lacks any of
// them.
type Deps struct {
	// NewInventorySource returns the inventory source over one inventory
	// document held in memory (inventoryfile.FromBytes), whose errors never
	// repeat a value from the document.
	NewInventorySource func(doc []byte) analysis.InventorySource
	// Targets maps every supported target key to the constructor of its
	// catalog source, as cli.Deps.Targets does.
	Targets map[string]func() analysis.CatalogSource
	// Markdown is the Markdown report, the page's Markdown download.
	Markdown Format
	// JSON is the JSON report (ADR 0018), the page's JSON download.
	JSON Format
	// HTML is the HTML fragment the page shows (htmlreport).
	HTML Format
}

// Format is one report format: the constructors of its reporters, as
// cli.Format has them. Each takes the schemaVersion the report is written in,
// which a format without one (Markdown, HTML) ignores.
type Format struct {
	// NewReporter returns the reporter of an assessment against one target.
	NewReporter func(schemaVersion int) analysis.Reporter
	// NewComparisonReporter returns the reporter of a comparison of several
	// targets.
	NewComparisonReporter func(schemaVersion int) analysis.ComparisonReporter
}

// Options are the choices the user made in the page, as the page supplies
// them, unchecked: Assess checks them by the CLI's rules.
type Options struct {
	// Targets are the chosen target keys, in the order given, or the single
	// value analysis.AllTargets for every target, as --target takes them.
	Targets []string
	// Regions are the regions chosen, one per target at most, in the order
	// given, as --region takes them. A target the user chose no region for has
	// no entry: an entry with an empty region is refused as --region
	// "<target>=" is.
	Regions []Region
	// Estimate turns plans and monthly estimates on, as leaving out
	// --no-estimate does.
	Estimate bool
	// SchemaVersion is the schemaVersion of the JSON report, as
	// --schema-version takes it: "2" or "3", or "" for the default,
	// analysis.DefaultSchemaVersion.
	SchemaVersion string
	// InventoryName is the base name of the file the inventory was read from,
	// which the reports name as the CLI's name the --inventory file, or "" for
	// an inventory pasted into the page, which the reports name not at all. A
	// name analysis.ReportMeta does not take is a usage error.
	InventoryName string
}

// Region is the region chosen for one target.
type Region struct {
	// Target is the target's key.
	Target string
	// Region is the region, which the target's catalog must list.
	Region string
}

// Result is what one assessment returns to the page.
type Result struct {
	// Markdown is the Markdown report: the bytes `shift2eu assess --format
	// markdown` writes for the same inventory and options.
	Markdown []byte
	// JSON is the JSON report: the bytes `shift2eu assess --format json`
	// writes for the same inventory and options.
	JSON []byte
	// HTML is the HTML fragment of the report, which the page shows.
	HTML []byte
}

// Kind is what an *Error is about.
type Kind int

// The kinds of an *Error.
const (
	// KindUsage is a choice the options rules refuse, the CLI's usage error
	// (exit code 2): a target, a region (one the target's catalog does not
	// list too), a schemaVersion or the inventory name. The page shows it and
	// stays usable.
	KindUsage Kind = iota + 1
	// KindInventory is an inventory document the inventory reader refuses
	// (malformed JSON, an unknown schema version or field, a document over the
	// reader's size cap). Its message names a position and a field, never a
	// value from the document.
	KindInventory
	// KindInternal is a failure that is neither: a Deps without an adapter it
	// needs, a catalog or a reporter that failed.
	KindInternal
)

// Error is the error Assess returns: one line the page shows, and its kind.
type Error struct {
	// Kind is what the error is about.
	Kind Kind
	// Err is the error, whose message is the line the page shows.
	Err error
}

// Error returns the message of Err.
func (e *Error) Error() string { return e.Err.Error() }

// Unwrap returns Err.
func (e *Error) Unwrap() error { return e.Err }

// errMissingAdapter is returned by New for a Deps without an adapter it
// needs: a wiring mistake in the composition root.
var errMissingAdapter = errors.New("the web adapter is wired without an adapter it needs")

// Assessor runs assessments for the page. It holds only its Deps, so one
// Assessor may run any number of assessments, one after another or
// concurrently.
type Assessor struct {
	deps Deps
}

// New returns an Assessor over deps, or an error when deps lacks an adapter
// (the inventory reader, a target, a reporter constructor) or holds no target.
func New(deps Deps) (*Assessor, error) {
	if deps.NewInventorySource == nil || len(deps.Targets) == 0 {
		return nil, errMissingAdapter
	}

	for _, newCatalog := range deps.Targets {
		if newCatalog == nil {
			return nil, errMissingAdapter
		}
	}

	for _, format := range []Format{deps.Markdown, deps.JSON, deps.HTML} {
		if format.NewReporter == nil || format.NewComparisonReporter == nil {
			return nil, errMissingAdapter
		}
	}

	return &Assessor{deps: deps}, nil
}

// Assess checks opts, reads the inventory document doc, assesses it against
// the chosen target, or compares the chosen targets, and returns the Markdown
// report, the JSON report and the HTML fragment. Every error is an *Error:
// KindUsage for options the CLI would refuse with exit code 2, checked before
// doc is read, KindInventory for a document the inventory reader refuses and
// KindInternal for anything else. Assess takes ownership of doc, as
// inventoryfile.FromBytes does.
func (a *Assessor) Assess(ctx context.Context, doc []byte, opts Options) (Result, error) {
	run, err := a.plan(opts)
	if err != nil {
		return Result{}, &Error{Kind: KindUsage, Err: err}
	}

	reports := &reportSet{
		markdown:       a.deps.Markdown,
		json:           a.deps.JSON,
		html:           a.deps.HTML,
		schemaVersion:  run.schemaVersion,
		markdownReport: &bytes.Buffer{},
		jsonReport:     &bytes.Buffer{},
		htmlReport:     &bytes.Buffer{},
	}

	source := &inventorySource{source: a.deps.NewInventorySource(doc)}
	meta := analysis.ReportMeta{InventoryName: opts.InventoryName}

	if err := a.run(ctx, run, source, reports, meta); err != nil {
		return Result{}, classify(err, run.targets)
	}

	// A run cancelled while the reports were rendered returns none: the page
	// asked it to stop.
	if err := ctx.Err(); err != nil {
		return Result{}, &Error{Kind: KindInternal, Err: fmt.Errorf("cancelled before returning the report: %w", err)}
	}

	return Result{
		Markdown: reports.markdownReport.Bytes(),
		JSON:     reports.jsonReport.Bytes(),
		HTML:     reports.htmlReport.Bytes(),
	}, nil
}

// runOptions are the checked options of one run.
type runOptions struct {
	// targets are the selected target keys, sorted and distinct.
	targets []string
	// regions maps a selected target's key to its chosen region.
	regions map[string]string
	// estimate turns plans and estimates on.
	estimate bool
	// schemaVersion is the JSON report's schemaVersion.
	schemaVersion int
}

// plan checks opts by the application's option rules, in the CLI's order
// (targets, regions, the schemaVersion), and returns the run they select.
// Every error it returns is a usage error.
func (a *Assessor) plan(opts Options) (runOptions, error) {
	targets, err := analysis.SelectTargets(opts.Targets, a.targetKeys())
	if err != nil {
		return runOptions{}, err
	}

	choices := make([]analysis.RegionChoice, 0, len(opts.Regions))
	for _, r := range opts.Regions {
		// The page writes no <target>=<region> text, so a refusal quotes the
		// choice as the CLI's --region would have taken it.
		choices = append(choices, analysis.RegionChoice{Target: r.Target, Region: r.Region, Text: r.Target + "=" + r.Region})
	}

	regions, err := analysis.SelectRegions(choices, targets)
	if err != nil {
		return runOptions{}, err
	}

	// The page always offers the JSON download, so the schemaVersion is
	// checked as the CLI checks it for --format json.
	schemaVersion, err := analysis.SelectSchemaVersion(analysis.JSONFormat, opts.SchemaVersion, opts.SchemaVersion != "")
	if err != nil {
		return runOptions{}, err
	}

	return runOptions{targets: targets, regions: regions, estimate: opts.Estimate, schemaVersion: schemaVersion}, nil
}

// targetKeys returns the keys of every supported target, sorted.
func (a *Assessor) targetKeys() []string {
	return slices.Sorted(maps.Keys(a.deps.Targets))
}

// run builds the use case the checked options select, one target or a
// comparison, over source and the reporters of reports, and runs it once: the
// inventory is read once and every format rendered from the same assessment.
func (a *Assessor) run(ctx context.Context, run runOptions, source analysis.InventorySource, reports *reportSet,
	meta analysis.ReportMeta,
) error {
	if len(run.targets) == 1 {
		key := run.targets[0]

		single, err := analysis.New(source, a.deps.Targets[key](), reports)
		if err != nil {
			return err
		}

		return single.RunWith(ctx, io.Discard, meta, analysis.Options{Region: run.regions[key], Estimate: run.estimate})
	}

	targets := make([]analysis.Target, 0, len(run.targets))
	for _, key := range run.targets {
		targets = append(targets, analysis.Target{Key: key, Catalog: a.deps.Targets[key](), Region: run.regions[key]})
	}

	comparison, err := analysis.NewComparison(source, targets, reports)
	if err != nil {
		return err
	}

	return comparison.RunWith(ctx, io.Discard, meta, analysis.ComparisonOptions{Estimate: run.estimate})
}

// classify returns the *Error for err, the error of a run over targets: a
// region the target's catalog does not list and an inventory name ReportMeta
// refuses are usage errors, a document the inventory reader refused an
// inventory error, anything else internal.
func classify(err error, targets []string) *Error {
	if regionErr := analysis.RegionChoiceError(err, targets); regionErr != nil {
		return &Error{Kind: KindUsage, Err: regionErr}
	}

	if errors.Is(err, analysis.ErrInvalidReportMeta) {
		return &Error{Kind: KindUsage, Err: err}
	}

	var refused *inventoryError
	if errors.As(err, &refused) {
		return &Error{Kind: KindInventory, Err: refused}
	}

	return &Error{Kind: KindInternal, Err: err}
}

// inventorySource is the inventory source of one run: the one Deps gives, its
// refusals marked as *inventoryError, so a document the reader refused is told
// apart from every other failure however the use case wraps it.
type inventorySource struct {
	source analysis.InventorySource
}

// Inventory returns the inventory of the source. An error the source returns
// while ctx is still live is the document's, an *inventoryError; one after
// ctx is done is the cancellation's and is returned as it is.
func (s *inventorySource) Inventory(ctx context.Context) (inventory.Inventory, error) {
	inv, err := s.source.Inventory(ctx)
	if err != nil && ctx.Err() == nil {
		return inventory.Inventory{}, &inventoryError{err: err}
	}

	return inv, err
}

// inventoryError is a document the inventory reader refused. Its message is the
// reader's, which names a position and a field, never a value (the in-memory
// mode of inventoryfile.FromBytes).
type inventoryError struct {
	err error
}

// Error returns the reader's message.
func (e *inventoryError) Error() string { return e.err.Error() }

// Unwrap returns the reader's error.
func (e *inventoryError) Unwrap() error { return e.err }

// reportSet is the one reporter a run renders through: it renders the
// assessment, or the comparison, once per format into that format's buffer
// and writes nothing to the writer the use case gives it. Each format's
// reporter is built once, the JSON one with the run's schemaVersion and the
// others with 0, as the CLI builds them.
type reportSet struct {
	markdown, json, html Format
	schemaVersion        int
	// markdownReport, jsonReport and htmlReport hold each format's report.
	markdownReport, jsonReport, htmlReport *bytes.Buffer
}

// Report renders a in every format.
func (r *reportSet) Report(ctx context.Context, _ io.Writer, a assessment.Assessment, meta analysis.ReportMeta) error {
	for _, f := range r.formats() {
		if err := f.format.NewReporter(f.schemaVersion).Report(ctx, f.out, a, meta); err != nil {
			return err
		}
	}

	return nil
}

// ReportComparison renders c in every format.
func (r *reportSet) ReportComparison(ctx context.Context, _ io.Writer, c assessment.Comparison,
	meta analysis.ReportMeta,
) error {
	for _, f := range r.formats() {
		if err := f.format.NewComparisonReporter(f.schemaVersion).ReportComparison(ctx, f.out, c, meta); err != nil {
			return err
		}
	}

	return nil
}

// renderTarget is one format a reportSet renders: its reporters, the
// schemaVersion they are built with and the buffer they write to.
type renderTarget struct {
	format        Format
	schemaVersion int
	out           io.Writer
}

// formats returns the formats r renders, Markdown, JSON and HTML, in that
// order.
func (r *reportSet) formats() []renderTarget {
	return []renderTarget{
		{format: r.markdown, out: r.markdownReport},
		{format: r.json, schemaVersion: r.schemaVersion, out: r.jsonReport},
		{format: r.html, out: r.htmlReport},
	}
}

// Version returns the version of the build, as `shift2eu version` prints it.
func Version() string {
	return version.String()
}
