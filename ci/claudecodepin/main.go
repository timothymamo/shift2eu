package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/ci/claudecodepin/internal/pincheck"
)

const (
	// defaultWorkflow is the workflow that pins the Claude Code CLI, relative
	// to the repository root CI runs from.
	defaultWorkflow = ".github/workflows/ai-review.yml"

	// fetchTimeout bounds the whole registry request, body included.
	fetchTimeout = 30 * time.Second

	// maxBody bounds the registry document read: a version document of the
	// platform package is a few KiB.
	maxBody = 1 << 20
)

// errRedirect refuses every redirect: the document is read from the one URL
// the pin names, or not at all.
var errRedirect = errors.New("the registry answered with a redirect, which the check does not follow")

func main() {
	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirect
		},
	}
	os.Exit(run(context.Background(), os.Args[1:], client, os.Stdout, os.Stderr))
}

// run checks the pin of the workflow args names (or defaultWorkflow) against
// the registry client reaches, and returns the exit code: 0 on a pass, 1 on
// every failure, after one ::error line on stderr.
func run(ctx context.Context, args []string, client *http.Client, stdout, stderr io.Writer) int {
	if err := check(ctx, args, client, stdout); err != nil {
		fmt.Fprintf(stderr, "::error title=Claude Code pin::%s\n", annotationData(err.Error()))
		return 1
	}
	return 0
}

func check(ctx context.Context, args []string, client *http.Client, stdout io.Writer) error {
	flags := flag.NewFlagSet("claudecodepin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workflow := flags.String("workflow", defaultWorkflow, "the workflow that pins the Claude Code CLI")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("usage: claudecodepin [-workflow <file>]: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("usage: claudecodepin [-workflow <file>]: no positional argument")
	}

	data, err := os.ReadFile(*workflow)
	if err != nil {
		return fmt.Errorf("read %s: %w", *workflow, err)
	}
	pin, err := pincheck.FromWorkflow(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", *workflow, err)
	}

	doc, err := fetch(ctx, client, pin.RegistryURL())
	if err != nil {
		return fmt.Errorf("%s@%s: %w", pincheck.PackageName, pin.Version, err)
	}
	if err := pincheck.Verify(pin, bytes.NewReader(doc)); err != nil {
		return fmt.Errorf("%s: %w", *workflow, err)
	}

	fmt.Fprintf(stdout, "Claude Code pin: %s pins %s@%s with the integrity the npm registry publishes, %s\n",
		*workflow, pincheck.PackageName, pin.Version, pin.Integrity)
	return nil
}

// fetch GETs url and returns its body, failing on any status but 200 and on a
// body over maxBody. The error names the status code alone, never the body.
func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build the registry request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch the registry's document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the registry answered %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read the registry's document: %w", err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("the registry's document is over %d bytes", maxBody)
	}
	return body, nil
}

// annotationEscaper escapes a workflow command's message as the runner reads
// it, so the text stays on the command's one line.
var annotationEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")

// annotationData escapes s for the message of a workflow command.
func annotationData(s string) string {
	return annotationEscaper.Replace(s)
}
