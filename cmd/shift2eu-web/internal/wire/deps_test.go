package wire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/cmd/shift2eu-web/internal/wire"
	"github.com/timothymamo/shift2eu/internal/adapter/source/inventoryfile"
	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/cli"
	"github.com/timothymamo/shift2eu/internal/web"
)

// This file holds what the wire package's parity tests share: the adapters as
// the page's composition root and the CLI's wire them, an inventory written to
// disk, and the CLI's answer read back as the message the page would show. The
// page and the CLI are compared over the same adapters, so a difference in
// options, decoding, ordering or rendering shows up as a difference in the
// bytes or in the message.
//
// Importing internal/cli is this wire package's test files' privilege (PRD
// #297, the web-composition-root depguard rule): the package itself may not, and
// the page never runs the CLI.

// The formats both sides render, as the CLI's --format values.
const (
	markdownFormat = "markdown"
	jsonFormat     = "json"
)

// inventoryFileName is the name both reports carry: the page is given the base
// name of the file the CLI reads, so the two reports are comparable byte for
// byte.
const inventoryFileName = "inventory.json"

// webDeps returns the page's adapters, as cmd/shift2eu-web wires them.
func webDeps() web.Deps {
	return wire.Deps()
}

// newAssessor returns the page's adapter over the real wire Deps.
func newAssessor(tb testing.TB) *web.Assessor {
	tb.Helper()

	assessor, err := web.New(webDeps())
	if err != nil {
		tb.Fatalf("web.New(wire.Deps()) returned unexpected error: %v", err)
	}

	return assessor
}

// cliDeps returns the CLI's adapters built from the same pieces the page's wire
// package builds: the inventory file reader, the three embedded catalogs and the
// Markdown and JSON reports. The page reads an inventory document, so its
// inventory reader is the in-memory one; the CLI reads a file, and the reports
// must not differ for that.
//
// It mirrors the deps function of cmd/shift2eu/main.go, which this test cannot
// import: that is a main package, and a main package is not importable. Every
// target, format and reader the CLI's composition root wires must be wired here
// too, and kept in step with it by hand, or the parity below compares the page
// with a CLI nobody runs.
func cliDeps() cli.Deps {
	deps := webDeps()

	return cli.Deps{
		NewInventorySource: func(path string) analysis.InventorySource {
			return inventoryfile.New(path)
		},
		Targets: deps.Targets,
		Formats: map[string]cli.Format{
			markdownFormat: {
				NewReporter:           deps.Markdown.NewReporter,
				NewComparisonReporter: deps.Markdown.NewComparisonReporter,
			},
			jsonFormat: {
				NewReporter:           deps.JSON.NewReporter,
				NewComparisonReporter: deps.JSON.NewComparisonReporter,
			},
		},
	}
}

// cliOutcome is what one `shift2eu assess` run answered.
type cliOutcome struct {
	// exit is the process's exit code: 0 for a run, 2 for a refused command
	// line.
	exit int
	// stdout is the report, empty when the run failed.
	stdout []byte
	// message is the error line with the CLI's "shift2eu: " prefix and the usage
	// text after it removed: the line the page shows instead.
	message string
}

// runCLI runs the real CLI with args and returns what it answered.
func runCLI(t *testing.T, args ...string) cliOutcome {
	t.Helper()

	var stdout, stderr bytes.Buffer

	exit := cli.Run(t.Context(), args, &stdout, &stderr, cliDeps())

	return cliOutcome{exit: exit, stdout: stdout.Bytes(), message: cliMessage(t, stderr.String())}
}

// cliMessage returns the message of the CLI's diagnostics: its first line,
// without the "shift2eu: " prefix. Everything after it is the blank line and the
// usage text, which the page does not show.
func cliMessage(t *testing.T, stderr string) string {
	t.Helper()

	if stderr == "" {
		return ""
	}

	line, _, _ := strings.Cut(stderr, "\n")
	message, ok := strings.CutPrefix(line, "shift2eu: ")
	if !ok {
		t.Fatalf("the CLI's first diagnostic line %q does not start with %q", line, "shift2eu: ")
	}

	return message
}

// writeInventory writes document to a fresh temporary directory as name and
// returns its path, so the CLI reads the document the page is given and reports
// the name the page reports.
func writeInventory(t *testing.T, name string, document []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatalf("writing the inventory %s: %v", path, err)
	}

	return path
}

// readInventory returns the bytes of an inventory fixture.
func readInventory(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the inventory %s: %v", path, err)
	}

	return data
}
