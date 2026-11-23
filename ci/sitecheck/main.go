package main

import (
	"context"
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
	_, _, _, _, _ = ctx, args, stdin, stdout, base
	_, _, _, _ = exitPassed, exitFailed, retryInterval, defaultRetryBound

	fmt.Fprintln(stderr, "sitecheck: not implemented")

	return exitConfig
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
