package web

import (
	"context"
	"errors"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
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

// errNotImplemented is what the skeleton's Assess returns.
var errNotImplemented = errors.New("web: not implemented")

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
func (*Assessor) Assess(_ context.Context, _ []byte, _ Options) (Result, error) {
	return Result{}, &Error{Kind: KindInternal, Err: errNotImplemented}
}

// Version returns the version of the build, as `shift2eu version` prints it.
func Version() string {
	return version.String()
}
