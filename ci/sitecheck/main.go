package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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
	// A runner ends a job with SIGTERM: it cancels the run like an
	// interrupt, so the partial report is still printed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

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
//	sitecheck -manifest <manifest.json> -tags <file or -> [-hsts-record <file>] [-preload-record <file>] [-host <host>] [-apex <apex>] [-retry-bound <duration>] [-plain-http-www redirect|refuse-or-close]
//
// -manifest (required) is the release's manifest.json; -tags (required) the
// repository's tag list, one per line, "-" for stdin; -hsts-record and
// -preload-record default to site/records/hsts.json and
// site/records/preload.json of the working directory; -host and -apex to
// livecheck.DefaultHost and livecheck.DefaultApex; -retry-bound to
// defaultRetryBound, and a value below livecheck.MinRetryBound is a
// configuration error; -plain-http-www to livecheck.PlainHTTPRedirect, and a
// value other than "redirect" or "refuse-or-close" is a configuration error
// naming the flag. run reads exactly the files its flags name, each
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
	if len(args) > 0 && args[0] == liveVersionCommand {
		return runLiveVersion(ctx, args[1:], stdin, stdout, stderr, base)
	}

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
	plainHTTPWWW := flags.String("plain-http-www", string(livecheck.PlainHTTPRedirect), `what plain HTTP on the canonical host must do: "redirect" or "refuse-or-close"`)

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
	case *plainHTTPWWW != string(livecheck.PlainHTTPRedirect) && *plainHTTPWWW != string(livecheck.PlainHTTPRefuseOrClose):
		return livecheck.Config{}, fmt.Errorf("-plain-http-www %q, want %q or %q", *plainHTTPWWW, livecheck.PlainHTTPRedirect, livecheck.PlainHTTPRefuseOrClose)
	}

	manifest, err := readFile(*manifestPath, livecheck.ParseManifest)
	if err != nil {
		return livecheck.Config{}, fmt.Errorf("-manifest %s: %w", *manifestPath, err)
	}

	tags, err := readTags(*tagsPath, stdin)
	if err != nil {
		return livecheck.Config{}, err
	}

	cfg := base
	cfg.Host = *host
	cfg.Apex = *apex
	cfg.Manifest = manifest
	cfg.Tags = tags
	cfg.Records.HSTS, cfg.Records.HSTSErr = readFile(*hstsPath, livecheck.ParseHSTSRecord)
	cfg.Records.Preload, cfg.Records.PreloadErr = readFile(*preloadPath, livecheck.ParsePreloadRecord)
	cfg.PlainHTTPWWW = livecheck.PlainHTTPWWW(*plainHTTPWWW)
	cfg.RetryBound = *retryBound
	cfg.RetryInterval = retryInterval
	cfg.CertMargin = livecheck.MinCertMargin

	if err := cfg.Validate(); err != nil {
		return livecheck.Config{}, err
	}

	return cfg, nil
}

// readTags reads the tag list -tags names: the file at path, or stdin for "-".
func readTags(path string, stdin io.Reader) ([]string, error) {
	var (
		tags []string
		err  error
	)

	if path == "-" {
		tags, err = livecheck.ReadTagList(stdin)
	} else {
		tags, err = readFile(path, livecheck.ReadTagList)
	}

	if err != nil {
		return nil, fmt.Errorf("-tags %s: %w", path, err)
	}

	return tags, nil
}

// liveVersionCommand is the subcommand of the scheduled check (PRD #297
// milestone 4, ADR 0043): which release does the live site serve, and may it?
const liveVersionCommand = "live-version"

// runLiveVersion is the live-version subcommand. args are the arguments after
// its name:
//
//	sitecheck live-version -tags <file or -> -anchor <RFC 3339 time> -window <duration> [-host <host>]
//
// -tags (required) is the repository's tag list, "-" for stdin; -anchor
// (required) the committer date of the commit the highest release tag points
// to (git log -1 --format=%cI <tag>); -window (required, positive) the version
// window's bound; -host defaults to livecheck.DefaultHost. It fetches
// https://<host>/ once (livecheck.LiveVersion, with base's Dial, Roots, Now,
// MaxBodyBytes and RequestTimeout) and, when the window accepts the live
// version, writes the tag list's own entry and a line break to stdout and
// returns exitPassed. A refused version, a root that does not answer 200 and
// a missing, hidden or malformed version are one line on stderr and
// exitFailed; a usage error is one line on stderr and exitConfig, before any
// connection.
func runLiveVersion(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, base livecheck.Config) int {
	cfg, err := configureLiveVersion(args, stdin, base)
	if err != nil {
		fmt.Fprintf(stderr, "sitecheck %s: %v\n", liveVersionCommand, err)

		return exitConfig
	}

	tag, err := livecheck.LiveVersion(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "sitecheck %s: %v\n", liveVersionCommand, err)

		return exitFailed
	}

	if _, err := fmt.Fprintln(stdout, tag.String()); err != nil {
		fmt.Fprintf(stderr, "sitecheck %s: write the version: %v\n", liveVersionCommand, err)

		return exitFailed
	}

	return exitPassed
}

// configureLiveVersion parses the live-version subcommand's args, reads the
// tag list and returns the subcommand's configuration.
func configureLiveVersion(args []string, stdin io.Reader, base livecheck.Config) (livecheck.LiveVersionConfig, error) {
	flags := flag.NewFlagSet(liveVersionCommand, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	tagsPath := flags.String("tags", "", `the repository's tag list, one tag per line, "-" for stdin (required)`)
	anchor := flags.String("anchor", "", "the committer date of the highest release tag's commit, RFC 3339 (required)")
	window := flags.Duration("window", 0, "the version window's bound after the anchor (required, positive)")
	host := flags.String("host", livecheck.DefaultHost, "the canonical host")

	if err := flags.Parse(args); err != nil {
		return livecheck.LiveVersionConfig{}, err
	}

	switch {
	case flags.NArg() > 0:
		return livecheck.LiveVersionConfig{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	case *tagsPath == "":
		return livecheck.LiveVersionConfig{}, errors.New("-tags is required")
	case *anchor == "":
		return livecheck.LiveVersionConfig{}, errors.New("-anchor is required")
	case *window <= 0:
		return livecheck.LiveVersionConfig{}, fmt.Errorf("-window %v is missing or not positive", *window)
	}

	anchorTime, err := time.Parse(time.RFC3339, *anchor)
	if err != nil {
		return livecheck.LiveVersionConfig{}, fmt.Errorf("-anchor %q is not an RFC 3339 time", *anchor)
	}

	tags, err := readTags(*tagsPath, stdin)
	if err != nil {
		return livecheck.LiveVersionConfig{}, err
	}

	cfg := livecheck.LiveVersionConfig{
		Host:           *host,
		Tags:           tags,
		Anchor:         anchorTime,
		Bound:          *window,
		Dial:           base.Dial,
		Roots:          base.Roots,
		Now:            base.Now,
		MaxBodyBytes:   base.MaxBodyBytes,
		RequestTimeout: base.RequestTimeout,
	}

	if err := cfg.Validate(); err != nil {
		return livecheck.LiveVersionConfig{}, err
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
