package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/version"
)

// The exit codes Run returns.
const (
	// exitOK means the command ran. For assess, blockers are a result, not a
	// failure.
	exitOK = 0
	// exitError means the command failed at run time: the inventory, the
	// catalog, the report or the output could not be read, built or written.
	exitError = 1
	// exitUsage means the command line is wrong; nothing was run.
	exitUsage = 2
)

// Deps are the adapters the CLI runs with. The composition root (cmd/shift2eu)
// builds them; the CLI never constructs an adapter itself. Run calls a
// constructor only once the command line is valid, so a usage error builds
// nothing.
type Deps struct {
	// NewInventorySource returns the inventory source that reads the inventory
	// file at path, the --inventory value as given.
	NewInventorySource func(path string) analysis.InventorySource
	// Targets maps every supported --target value to the constructor of its
	// catalog source.
	Targets map[string]func() analysis.CatalogSource
	// Formats maps every supported --format value to the reporters of that
	// report format.
	Formats map[string]Format
}

// Format is one report format --format selects: the constructors of its
// reporters. Run builds only the one the run needs.
type Format struct {
	// NewReporter returns the reporter that renders the report of a run with one
	// target.
	NewReporter func() analysis.Reporter
	// NewComparisonReporter returns the reporter that renders the comparison
	// report of a run with several targets (docs/adr/0016).
	NewComparisonReporter func() analysis.ComparisonReporter
}

// errMissingAdapter is returned when Deps lacks a constructor the command
// needs: a wiring mistake in the composition root, not a user error.
var errMissingAdapter = errors.New("the command is wired without an adapter it needs")

// Run runs the command line args, without the program name, writes the
// command's output to stdout and diagnostics to stderr, and returns the exit
// code: 0 when the command ran, 1 for a run-time error and 2 for a usage error.
// The package documentation describes the commands and what each exit code
// writes.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int {
	if len(args) == 0 {
		return usageError(stderr, deps, errors.New("no command given"))
	}

	switch args[0] {
	case "version", "--version":
		if _, err := fmt.Fprintln(stdout, version.String()); err != nil {
			return runError(stderr, fmt.Errorf("write the version to standard output: %w", err))
		}

		return exitOK
	case "help", "-h", "-help", "--help":
		printUsage(stdout, deps)

		return exitOK
	case "assess":
		return runAssess(ctx, args[1:], stdout, stderr, deps)
	default:
		return usageError(stderr, deps, fmt.Errorf("unknown command %s", text.Quote(args[0])))
	}
}

// runError prints err as the one error line of a failed run and returns the
// run-time exit code.
func runError(stderr io.Writer, err error) int {
	printError(stderr, err)

	return exitError
}

// usageError prints err as an error line followed by the usage text and returns
// the usage exit code.
func usageError(stderr io.Writer, deps Deps, err error) int {
	printError(stderr, err)
	fmt.Fprintln(stderr)
	printUsage(stderr, deps)

	return exitUsage
}

// printError writes err to w as "shift2eu: " and its message made safe by
// sanitise, on one line. It is the only way the CLI prints an error.
func printError(w io.Writer, err error) {
	msg := err.Error()
	if msg == "" {
		msg = "unknown error"
	}

	fmt.Fprintf(w, "shift2eu: %s\n", sanitise(msg))
}

// usage is the help text; the verbs are the list of supported targets, the
// list of supported formats and the default format.
const usage = `Usage:
  shift2eu assess --inventory <file> --target <targets> [--format <format>]
               [--output <file>] [--region <target>=<region>,...]
               [--no-estimate]
  shift2eu version
  shift2eu help

Commands:
  assess   Assess the resources of an inventory file against the catalog of a
           target, or compare several targets, and write a report.
           Runs offline and needs no credentials.
  version  Print the version.
  help     Print this help.

Flags of assess:
  --inventory <file>  The inventory file to assess, in the JSON format of
                      docs/adr/0006. A path: standard input is not supported.
  --target <targets>  One target, a comma-separated list of targets, or all
                      for every target:
                      %s.
                      Several targets give one report that compares them,
                      then each target's own report.
  --format <format>   The report format, one of: %s.
                      The default is %s. json is the versioned JSON
                      report of docs/adr/0018.
  --output <file>     Write the report to this file, with mode 0600, instead of
                      standard output. An existing regular file, a symbolic
                      link to one or a dangling link is replaced only once the
                      whole report is written; a failed run leaves it as it
                      was. Anything else (a directory, a pipe, a device, a
                      socket, a link to one) and the inventory file itself are
                      refused.
  --region <target>=<region>,...
                      Assess and estimate a selected target in one of its
                      regions, such as scaleway=nl-ams. A target without one is
                      assessed in all its regions, with estimates in its
                      default region. A region the target does not list is a
                      usage error that names the target's regions.
  --no-estimate       Leave out plans and monthly estimates: the report holds
                      no price. Estimates are on by default: list prices on the
                      catalog's retrieval date, excluding VAT and discounts.

Exit codes:
  0  the assessment ran (blockers are a result, not a failure)
  1  the inventory, the catalog, the report or the output failed
  2  the command line is wrong
`

// printUsage writes the help text to w.
func printUsage(w io.Writer, deps Deps) {
	fmt.Fprintf(w, usage, strings.Join(targetKeys(deps), ", "), strings.Join(formatKeys(deps), ", "), defaultFormat)
}

// targetKeys returns the supported --target values, sorted.
func targetKeys(deps Deps) []string {
	return slices.Sorted(maps.Keys(deps.Targets))
}

// formatKeys returns the supported --format values, sorted.
func formatKeys(deps Deps) []string {
	return slices.Sorted(maps.Keys(deps.Formats))
}
