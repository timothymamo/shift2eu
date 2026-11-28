//go:build !(js && wasm)

// Command shift2eu analyses infrastructure running in a hyperscaler (AWS, Azure,
// GCP) and reports what can move to an EU cloud provider (OVHcloud, Scaleway,
// UpCloud).
//
// main is the composition root: it is the only place that wires concrete
// adapters into the application use cases. The command line itself is handled
// by internal/cli.
//
// The CLI is never built for js/wasm, the site's module target, where syscall
// has no SIGHUP: make lint lints that target too (docs/adr/0035), so the
// package holds no file there.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/timothymamo/shift2eu/internal/adapter/report/jsonreport"
	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/adapter/report/pdfreport"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/adapter/target/ovhcloud"
	"github.com/timothymamo/shift2eu/internal/adapter/target/scaleway"
	"github.com/timothymamo/shift2eu/internal/adapter/target/upcloud"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/cli"
	"github.com/timothymamo/shift2eu/internal/version"
)

func main() {
	// SIGINT, SIGTERM and SIGHUP (the terminal closed) cancel ctx, which ends a
	// run with exit code 1; a signal before the final write writes no report.
	// A signal during the final write or rename may still let it complete. Once
	// ctx is cancelled, stop restores the default behaviour, so a second signal
	// ends the process at once. SIGQUIT keeps the Go runtime's default: a
	// goroutine dump and exit code 2.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	context.AfterFunc(ctx, stop)

	// Exit as soon as Run returns, without waiting for adapter goroutines: after
	// a cancellation the inventory-file adapter may leave one blocked in open()
	// on a FIFO with no writer (ADR 0006).
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, deps()))
}

// deps wires the adapters of the assess path: the inventory file, the embedded
// OVHcloud, Scaleway and UpCloud catalogs, and the three report formats: the
// Markdown reporter, the default, the JSON reporter (docs/adr/0018) and the PDF
// reporter (docs/adr/0049), each rendering both the single-target report and
// the comparison of several targets. The PDF reporter names the build in its
// footer with the line `shift2eu version` prints. None of them uses the network
// or credentials.
func deps() cli.Deps {
	return cli.Deps{
		NewInventorySource: func(path string) analysis.InventorySource {
			return inventoryfile.New(path)
		},
		Targets: map[string]func() analysis.CatalogSource{
			"ovhcloud": func() analysis.CatalogSource { return ovhcloud.New() },
			"scaleway": func() analysis.CatalogSource { return scaleway.New() },
			"upcloud":  func() analysis.CatalogSource { return upcloud.New() },
		},
		Formats: map[string]cli.Format{
			"markdown": {
				NewReporter:           func(int) analysis.Reporter { return markdown.New() },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return markdown.New() },
			},
			"json": {
				NewReporter: func(schemaVersion int) analysis.Reporter {
					return jsonreport.New(schemaVersion)
				},
				NewComparisonReporter: func(schemaVersion int) analysis.ComparisonReporter {
					return jsonreport.New(schemaVersion)
				},
			},
			"pdf": {
				NewReporter:           func(int) analysis.Reporter { return pdfreport.New(version.String()) },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return pdfreport.New(version.String()) },
			},
		},
	}
}
