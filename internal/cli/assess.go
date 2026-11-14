package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/text"
)

// assessOptions are the flags of the assess command.
type assessOptions struct {
	// inventory is the path of the inventory file.
	inventory string
	// targets are the keys of the selected targets in Deps.Targets, sorted and
	// distinct: one key for a single-target report, several for a comparison.
	targets []string
	// output is the path of the report file, or empty for standard output.
	output string
	// format is the key of the selected report format in Deps.Formats.
	format string
	// regions maps a selected target's key to the one region --region chose
	// for it; a target without an entry is assessed across all its regions.
	regions map[string]string
	// estimate turns plan matching and monthly estimates on: true unless
	// --no-estimate was given (PRD #190).
	estimate bool
}

// runAssess runs the assess command with args, the arguments after "assess",
// and returns the exit code.
func runAssess(ctx context.Context, args []string, stdout, stderr io.Writer, deps Deps) int {
	opts, err := parseAssess(args, deps)

	switch {
	case errors.Is(err, flag.ErrHelp):
		printUsage(stdout, deps)

		return exitOK
	case err != nil:
		return usageError(stderr, deps, err)
	}

	if err := assess(ctx, opts, stdout, deps); err != nil {
		// A --region the target's catalog does not list is only found once
		// the catalog is loaded, before the inventory is read, but it is
		// still a mistake on the command line.
		if regionErr := regionUsageError(err, opts); regionErr != nil {
			return usageError(stderr, deps, regionErr)
		}

		return runError(stderr, err)
	}

	return exitOK
}

// parseAssess parses the flags of the assess command and checks them against
// deps. Every error it returns is a usage error, or flag.ErrHelp when help was
// asked for.
func parseAssess(args []string, deps Deps) (assessOptions, error) {
	var opts assessOptions

	flags := flag.NewFlagSet("assess", flag.ContinueOnError)
	// The flag package's own messages show the command line unescaped, so it
	// prints nothing: its errors reach the user through printError.
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	var target, region string

	noEstimate := false

	flags.StringVar(&opts.inventory, "inventory", "", "the inventory file")
	flags.StringVar(&target, "target", "", "the targets to compare with")
	flags.StringVar(&opts.output, "output", "", "the report file")
	flags.StringVar(&opts.format, "format", defaultFormat, "the report format")
	flags.StringVar(&region, "region", "", "the region per target, <target>=<region>,...")
	flags.BoolVar(&noEstimate, "no-estimate", false, "leave out plans and estimates")

	if err := flags.Parse(args); err != nil {
		return assessOptions{}, err
	}

	switch {
	case opts.inventory == "":
		return assessOptions{}, errors.New("assess needs --inventory")
	case target == "":
		return assessOptions{}, errors.New("assess needs --target")
	case flags.NArg() > 0:
		return assessOptions{}, fmt.Errorf("unexpected argument %s", text.Quote(flags.Arg(0)))
	}

	targets, err := parseTargets(target, deps)
	if err != nil {
		return assessOptions{}, err
	}

	opts.targets = targets
	opts.estimate = !noEstimate

	regions, err := parseRegions(region, targets)
	if err != nil {
		return assessOptions{}, err
	}

	opts.regions = regions

	// A Deps without any format is a wiring mistake that newRunner reports,
	// not a usage error.
	if _, ok := deps.Formats[opts.format]; !ok && len(deps.Formats) > 0 {
		return assessOptions{}, fmt.Errorf("unknown format %s; supported formats: %s",
			text.Quote(opts.format), strings.Join(formatKeys(deps), ", "))
	}

	return opts, nil
}

// defaultFormat is the --format value of a run that names none: the Markdown
// report every run wrote before --format existed.
const defaultFormat = "markdown"

// allTargets is the --target value that selects every supported target.
const allTargets = "all"

// parseTargets parses the --target value: one key of deps.Targets, a
// comma-separated list of distinct keys, or all for every key. It returns the
// selected keys sorted. Every error it returns is a usage error that ends with
// the sorted list of supported targets.
func parseTargets(value string, deps Deps) ([]string, error) {
	supported := strings.Join(targetKeys(deps), ", ")

	if value == allTargets {
		return targetKeys(deps), nil
	}

	elements := strings.Split(value, ",")
	selected := make([]string, 0, len(elements))

	for _, key := range elements {
		switch {
		case key == "":
			return nil, fmt.Errorf("--target %s has an empty target; supported targets: %s",
				text.Quote(value), supported)
		case key == allTargets:
			return nil, fmt.Errorf("--target %s cannot be combined with other targets; supported targets: %s",
				text.Quote(allTargets), supported)
		case slices.Contains(selected, key):
			return nil, fmt.Errorf("target %s given more than once; supported targets: %s",
				text.Quote(key), supported)
		}

		if _, ok := deps.Targets[key]; !ok {
			return nil, fmt.Errorf("unknown target %s; supported targets: %s", text.Quote(key), supported)
		}

		selected = append(selected, key)
	}

	slices.Sort(selected)

	return selected, nil
}

// parseRegions parses the --region value: comma-separated <target>=<region>
// pairs, each naming one of the selected targets at most once. Spaces around a
// pair, its key and its region are dropped, so "a=x, b=y" is two pairs. It
// returns nil for an empty value. Whether the target lists the region is checked against
// its catalog once loaded (analysis.RegionError), before the inventory is read.
// Every error it returns is a usage error.
func parseRegions(value string, selected []string) (map[string]string, error) {
	if value == "" {
		return nil, nil
	}

	regions := make(map[string]string)

	for _, pair := range strings.Split(value, ",") {
		key, region, found := strings.Cut(pair, "=")
		key, region = strings.TrimSpace(key), strings.TrimSpace(region)

		switch {
		case !found || key == "" || region == "":
			return nil, fmt.Errorf("--region %s is not <target>=<region>; selected targets: %s",
				text.Quote(pair), strings.Join(selected, ", "))
		case !slices.Contains(selected, key):
			return nil, fmt.Errorf("--region names target %s, which --target does not select; selected targets: %s",
				text.Quote(key), strings.Join(selected, ", "))
		}

		if _, ok := regions[key]; ok {
			return nil, fmt.Errorf("--region names target %s more than once", text.Quote(key))
		}

		regions[key] = region
	}

	return regions, nil
}

// regionUsageError returns the usage error for an *analysis.RegionError inside
// err, naming the target by its --target key also for a single-target run, and
// nil when err holds none.
func regionUsageError(err error, opts assessOptions) error {
	var regionErr *analysis.RegionError
	if !errors.As(err, &regionErr) {
		return nil
	}

	named := *regionErr
	if named.Target == "" && len(opts.targets) == 1 {
		named.Target = opts.targets[0]
	}

	return fmt.Errorf("--region: %w", &named)
}

// runFunc runs the use case assess built, the Run method of analysis.Analysis
// for one target or of analysis.Comparison for several.
type runFunc func(ctx context.Context, w io.Writer, meta analysis.ReportMeta) error

// newRunner builds the adapters the selected targets need and the use case that
// runs them: one target keeps the single-target report, several get a
// comparison. Only the selected targets' catalog sources are built, and only
// the reporter of the selected format the use case needs.
func newRunner(opts assessOptions, deps Deps) (runFunc, error) {
	if deps.NewInventorySource == nil {
		return nil, errMissingAdapter
	}

	format, ok := deps.Formats[opts.format]
	if !ok {
		return nil, errMissingAdapter
	}

	if len(opts.targets) == 1 {
		newCatalog := deps.Targets[opts.targets[0]]
		if newCatalog == nil || format.NewReporter == nil {
			return nil, errMissingAdapter
		}

		single, err := analysis.New(deps.NewInventorySource(opts.inventory), newCatalog(), format.NewReporter())
		if err != nil {
			return nil, err
		}

		options := analysis.Options{Region: opts.regions[opts.targets[0]], Estimate: opts.estimate}

		return func(ctx context.Context, w io.Writer, meta analysis.ReportMeta) error {
			return single.RunWith(ctx, w, meta, options)
		}, nil
	}

	if format.NewComparisonReporter == nil {
		return nil, errMissingAdapter
	}

	for _, key := range opts.targets {
		if deps.Targets[key] == nil {
			return nil, errMissingAdapter
		}
	}

	targets := make([]analysis.Target, 0, len(opts.targets))
	for _, key := range opts.targets {
		targets = append(targets, analysis.Target{Key: key, Catalog: deps.Targets[key](), Region: opts.regions[key]})
	}

	comparison, err := analysis.NewComparison(deps.NewInventorySource(opts.inventory), targets,
		format.NewComparisonReporter())
	if err != nil {
		return nil, err
	}

	options := analysis.ComparisonOptions{Estimate: opts.estimate}

	return func(ctx context.Context, w io.Writer, meta analysis.ReportMeta) error {
		return comparison.RunWith(ctx, w, meta, options)
	}, nil
}

// assess builds the adapters from deps, runs the analysis, or the comparison of
// several targets, and writes the report
// to stdout or to the --output file. It refuses an unsafe --output before it
// reads anything, renders the whole report into memory, and writes nothing
// unless the whole report was rendered.
func assess(ctx context.Context, opts assessOptions, stdout io.Writer, deps Deps) error {
	run, err := newRunner(opts, deps)
	if err != nil {
		return err
	}

	if opts.output != "" {
		if err := checkOutput(opts.inventory, opts.output); err != nil {
			return err
		}
	}

	// A report names the inventory file by its base name, never by its full
	// path; analysis.Run checks the name before it reads anything.
	meta := analysis.ReportMeta{InventoryName: filepath.Base(opts.inventory)}

	var report bytes.Buffer

	if err := run(ctx, &report, meta); err != nil {
		return err
	}

	// A run cancelled while the report was rendered writes nothing: the user
	// asked it to stop.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cancelled before writing the report: %w", err)
	}

	if opts.output == "" {
		return writeStdout(stdout, report.Bytes())
	}

	return writeFile(opts.output, report.Bytes())
}

// checkOutput refuses an --output dest the report must not replace: anything
// but a regular file, a symbolic link to one or a dangling link (a directory, a
// named pipe, a socket, a device, or a link to one of them), and the inventory
// file itself, by the same path, a hard link or a symbolic link to it. A dest
// that does not exist yet is accepted.
func checkOutput(inventory, dest string) error {
	info, err := os.Lstat(dest)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("output %s: %w", text.Quote(dest), withoutPath(err))
	case !info.Mode().IsRegular() && info.Mode()&fs.ModeSymlink == 0:
		return fmt.Errorf("output %s is %s, not a regular file", text.Quote(dest), fileKind(info.Mode()))
	}

	// os.Stat follows symbolic links, so destInfo describes what a link at dest
	// points to. The rename would replace the link, not its target, but a link
	// to something that is not a report file is refused as it stands: run as
	// root, --output /dev/stdout would otherwise replace the system's
	// /dev/stdout link with a regular file. A link that cannot be resolved
	// (dangling, a loop, a target out of reach) is replaced.
	destInfo, destErr := os.Stat(dest)
	if destErr == nil && !destInfo.Mode().IsRegular() {
		return fmt.Errorf("output %s links to %s, not a regular file; leave out --output to write to standard output",
			text.Quote(dest), fileKind(destInfo.Mode()))
	}

	// os.Stat follows symbolic links on both sides, so a link to the inventory,
	// or an inventory that is a link to dest, is the same file. A path that
	// cannot be resolved is no clash: the inventory source reports a missing
	// inventory, and a dangling link at dest is replaced.
	invInfo, invErr := os.Stat(inventory)

	if invErr == nil && destErr == nil && os.SameFile(invInfo, destInfo) {
		return fmt.Errorf("output %s is the inventory file %s; write the report to another file",
			text.Quote(dest), text.Quote(inventory))
	}

	return nil
}

// fileKind names the kind of file mode describes, for an output that is not a
// regular file.
func fileKind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeDevice != 0:
		return "a device"
	default:
		return "a special file"
	}
}

// writeStdout writes the rendered report to stdout in one call.
func writeStdout(stdout io.Writer, report []byte) error {
	if _, err := stdout.Write(report); err != nil {
		return fmt.Errorf("write the report to standard output: %w", withoutPath(err))
	}

	return nil
}

// reportMode is the mode of a report file: a report describes an account's
// infrastructure, so only its owner may read it.
const reportMode = 0o600

// writeFile writes the rendered report to a temporary file in the directory of
// dest and renames it onto dest once it is written and synced. The temporary
// file exists only for this final write, never while the report is rendered.
// On any error it removes the temporary file, so dest is either the whole new
// report or left as it was, and never truncated. Every error names dest, never
// the temporary file.
func writeFile(dest string, report []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".shift2eu-report-*.tmp")
	if err != nil {
		return fmt.Errorf("output %s: create a temporary file in its directory: %w",
			text.Quote(dest), withoutPath(err))
	}

	renamed := false

	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if err := tmp.Chmod(reportMode); err != nil {
		return fmt.Errorf("output %s: set the mode of the temporary file: %w", text.Quote(dest), withoutPath(err))
	}

	if _, err := tmp.Write(report); err != nil {
		return fmt.Errorf("output %s: write the report: %w", text.Quote(dest), withoutPath(err))
	}

	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("output %s: sync the temporary file: %w", text.Quote(dest), withoutPath(err))
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("output %s: close the temporary file: %w", text.Quote(dest), withoutPath(err))
	}

	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("output %s: replace it with the report: %w", text.Quote(dest), withoutPath(err))
	}

	renamed = true

	return nil
}

// withoutPath returns the OS error inside an *fs.PathError or *os.LinkError,
// which repeat file names unquoted, so that an error names the file once,
// through text.Quote. Any other error is returned as it is.
func withoutPath(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return linkErr.Err
	}

	return err
}
