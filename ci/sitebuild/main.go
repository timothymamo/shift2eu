package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "sitebuild: %v\n", err)
		os.Exit(1)
	}
}

// run parses the flags and builds the bundle.
func run(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("sitebuild", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var opts options

	flags.StringVar(&opts.out, "out", "", "the directory to write the bundle into; missing or empty")
	flags.StringVar(&opts.archive, "archive", "", "the .tar.gz archive of the bundle to write")
	flags.StringVar(&opts.version, "version", "", "the release tag the pages show (vX.Y.Z); empty for internal/version's development value")
	flags.StringVar(&opts.site, "site", "", "the directory of the site's sources; empty for site/ of the module")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments %q", flags.Args())
	}

	if opts.out == "" || opts.archive == "" {
		return fmt.Errorf("both -out and -archive are required")
	}

	return build(ctx, opts)
}
