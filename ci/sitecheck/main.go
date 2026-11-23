package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitecheck/internal/livecheck"
)

// The exit codes of run.
const (
	// exitPassed: every check passed.
	exitPassed = 0
	// exitFailed: a check failed (the report says which).
	exitFailed = 1
	// exitConfig: a usage or configuration error; no report, or no check ran.
	exitConfig = 2
)

// retryInterval is the wait between two rounds of checks.
const retryInterval = 30 * time.Second

// defaultRetryBound is -retry-bound's default: twice the HTML's max-age.
const defaultRetryBound = 2 * livecheck.MinRetryBound

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, livecheck.Config{
		Now:   time.Now,
		Sleep: sleep,
	})

	stop()
	os.Exit(code)
}

// run is the program without os.Exit, its environment passed in. args are
// the arguments after the program's name:
//
//	sitecheck -manifest <manifest.json> -tags <file or -> [-hsts-record <file>] [-preload-record <file>] [-host <host>] [-apex <apex>] [-retry-bound <duration>]
//
// -manifest (required) is the release's manifest.json; -tags (required) the
// repository's tag list, one per line, "-" for stdin; -hsts-record and
// -preload-record default to site/records/hsts.json and
// site/records/preload.json of the working directory; -host and -apex to
// livecheck.DefaultHost and livecheck.DefaultApex; -retry-bound to
// defaultRetryBound, and a value below livecheck.MinRetryBound is a
// configuration error. run reads exactly the files its flags name, each
// capped (livecheck.MaxManifestBytes, MaxRecordBytes, MaxTagListBytes), and
// no other file and no environment variable.
//
// A manifest or tag list that cannot be read or parsed, an unknown flag, a
// positional argument, or a Config livecheck refuses is a configuration
// error: one line on stderr and exitConfig. A record file that is missing,
// unreadable or refused is no configuration error: its error goes into
// livecheck.Records, and the run fails safe.
//
// base carries what main injects and a test replaces: Dial, Roots, Now,
// Sleep (and, for a test, MaxBodyBytes and RequestTimeout); run fills in every
// other field from the flags (CertMargin livecheck.MinCertMargin,
// RetryInterval retryInterval) and calls livecheck.Run. It writes the
// report's text (Report.WriteText) to stdout and returns exitPassed when the
// report passed, exitFailed otherwise.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, base livecheck.Config) int {
	cfg, err := configure(args, stdin, base)
	if err != nil {
		fmt.Fprintf(stderr, "sitecheck: %v\n", err)

		return exitConfig
	}

	report, err := livecheck.Run(ctx, cfg)
	if err != nil && report.Rounds == 0 {
		fmt.Fprintf(stderr, "sitecheck: %v\n", err)

		return exitConfig
	}

	if writeErr := report.WriteText(stdout); writeErr != nil {
		fmt.Fprintf(stderr, "sitecheck: write the report: %v\n", writeErr)

		return exitFailed
	}

	if err != nil || !report.Passed() {
		return exitFailed
	}

	return exitPassed
}

// configure parses args, reads the files they name and returns the Config of
// the run: base with every other field filled in.
func configure(args []string, stdin io.Reader, base livecheck.Config) (livecheck.Config, error) {
	flags := flag.NewFlagSet("sitecheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	manifestPath := flags.String("manifest", "", "the release's manifest.json (required)")
	tagsPath := flags.String("tags", "", `the repository's tag list, one tag per line, "-" for stdin (required)`)
	hstsPath := flags.String("hsts-record", "site/records/hsts.json", "the HSTS record")
	preloadPath := flags.String("preload-record", "site/records/preload.json", "the preload record")
	host := flags.String("host", livecheck.DefaultHost, "the canonical host")
	apex := flags.String("apex", livecheck.DefaultApex, "the apex")
	retryBound := flags.Duration("retry-bound", defaultRetryBound, "how long failing rounds are repeated")

	if err := flags.Parse(args); err != nil {
		return livecheck.Config{}, err
	}

	switch {
	case flags.NArg() > 0:
		return livecheck.Config{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	case *manifestPath == "":
		return livecheck.Config{}, errors.New("-manifest is required")
	case *tagsPath == "":
		return livecheck.Config{}, errors.New("-tags is required")
	case *retryBound < livecheck.MinRetryBound:
		return livecheck.Config{}, fmt.Errorf("-retry-bound %v is below the HTML's max-age, %v", *retryBound, livecheck.MinRetryBound)
	}

	manifest, err := readFile(*manifestPath, livecheck.ParseManifest)
	if err != nil {
		return livecheck.Config{}, fmt.Errorf("-manifest %s: %w", *manifestPath, err)
	}

	var tags []string

	if *tagsPath == "-" {
		tags, err = livecheck.ReadTagList(stdin)
	} else {
		tags, err = readFile(*tagsPath, livecheck.ReadTagList)
	}

	if err != nil {
		return livecheck.Config{}, fmt.Errorf("-tags %s: %w", *tagsPath, err)
	}

	cfg := base
	cfg.Host = *host
	cfg.Apex = *apex
	cfg.Manifest = manifest
	cfg.Tags = tags
	cfg.Records.HSTS, cfg.Records.HSTSErr = readFile(*hstsPath, livecheck.ParseHSTSRecord)
	cfg.Records.Preload, cfg.Records.PreloadErr = readFile(*preloadPath, livecheck.ParsePreloadRecord)
	cfg.RetryBound = *retryBound
	cfg.RetryInterval = retryInterval
	cfg.CertMargin = livecheck.MinCertMargin

	if err := cfg.Validate(); err != nil {
		return livecheck.Config{}, err
	}

	return cfg, nil
}

// readFile opens path and hands it to parse, which caps what it reads.
func readFile[T any](path string, parse func(io.Reader) (T, error)) (T, error) {
	var zero T

	f, err := os.Open(path) //nolint:gosec // G304: the path is the one a flag of the operator names, read capped by its parser, never a path taken from a manifest or a response
	if err != nil {
		return zero, err
	}

	defer func() { _ = f.Close() }()

	value, err := parse(f)
	if err != nil {
		return zero, fmt.Errorf("%s: %w", path, err)
	}

	return value, nil
}

// sleep waits d, or returns ctx's error when ctx ends first.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
