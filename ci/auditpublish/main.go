package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	code := run(ctx, os.Args[1:], os.LookupEnv, time.Now, os.Stdout, os.Stderr)

	stop()
	os.Exit(code)
}

// run is the entry point with its arguments, the step's environment as a
// lookup function, the clock the run's UTC date is read from, and where its
// one line goes; it returns the exit code. A test passes its own lookup and
// clock. It reads exactly the variables the package's documentation names,
// and refuses a run in whose environment AUDIT_APP_PRIVATE_KEY,
// AUDIT_MODEL_PRIVATE_KEY, AUDIT_PUBLISH_PRIVATE_KEY or
// CLAUDE_CODE_OAUTH_TOKEN is set at all (configuration invalid, exit 9).
//
// The skeleton (PRD #175 milestone 2, PR 4) fails every run.
func run(_ context.Context, _ []string, _ func(string) (string, bool), _ func() time.Time, stdout, _ io.Writer) int {
	_, _ = io.WriteString(stdout, "publish failed: see runbook\n")

	return 1
}
