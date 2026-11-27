package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/cli"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/domain/catalog"
	"github.com/timothymamo/shift2eu/internal/domain/inventory"
	"github.com/timothymamo/shift2eu/internal/domain/taxonomy"
	"github.com/timothymamo/shift2eu/internal/domain/text"
	"github.com/timothymamo/shift2eu/internal/testutil"
	"github.com/timothymamo/shift2eu/internal/version"
)

// truncationRE matches the marker a cut error message ends with, on the one
// line the message is printed as.
var truncationRE = regexp.MustCompile(`\.\.\.\(truncated, [0-9]+ bytes\)\n$`)

// escapeRE matches a complete escape of the form sanitise writes: \uXXXX for an
// escaped rune and \xNN for a byte that is not valid UTF-8.
var escapeRE = regexp.MustCompile(`\\u[0-9a-f]{4}|\\x[0-9a-f]{2}`)

const (
	// reportBody is what the fake reporter writes, so a test can tell the
	// reporter's output from anything the CLI writes itself.
	reportBody = "# shift2eu report\n"
	// inventoryPath is the --inventory value the tests below use: a path with a
	// directory, so the test can see that a report shows the base name only.
	inventoryPath = "dir/inv.json"
	// inventoryName is the base name of inventoryPath, the only part of it a
	// report may show.
	inventoryName = "inv.json"
	// targetKey is the one target the fake deps support, as the CLI spells it.
	targetKey = "scaleway"
	// markdownFormat, jsonFormat and pdfFormat are the three --format values the
	// fake deps support, and supportedFormats is the list a usage error gives,
	// sorted.
	markdownFormat   = "markdown"
	jsonFormat       = "json"
	pdfFormat        = "pdf"
	supportedFormats = jsonFormat + ", " + markdownFormat + ", " + pdfFormat
	// jsonReportBody is what the fake json reporter writes, so a test can tell a
	// --format json run from the default one.
	jsonReportBody = `{"schemaVersion": 1, "kind": "assessment"}` + "\n"
	// pdfReportBody is what the fake pdf reporter writes: a PDF header, a comment
	// line that is not valid UTF-8 and the %%EOF line, so a test can see that a
	// run copies the reporter's bytes unchanged rather than text.
	pdfReportBody = "%PDF-1.7\n%\xe2\xe3\xcf\xd3\n%%EOF\n"
	// errPrefix is how the CLI writes an error to stderr.
	errPrefix = "shift2eu: "
	// maxErrorLine is the longest error line the CLI may write: the cap on the
	// message, the prefix, a short truncation marker and the newline.
	maxErrorLine = 1024 + len(errPrefix) + 64 + 1
)

// fakeInventorySource is an analysis.InventorySource that returns a fixed
// inventory or a fixed error and records how often it was called. onInventory,
// when set, runs inside Inventory before the inventory is returned, so a test
// can look at the filesystem while the inventory is being loaded.
type fakeInventorySource struct {
	inv         inventory.Inventory
	err         error
	onInventory func()
	calls       int
}

func (s *fakeInventorySource) Inventory(ctx context.Context) (inventory.Inventory, error) {
	s.calls++

	if s.onInventory != nil {
		s.onInventory()
	}

	if err := ctx.Err(); err != nil {
		return inventory.Inventory{}, err
	}

	return s.inv, s.err
}

// fakeCatalogSource is an analysis.CatalogSource that returns a fixed catalog or
// a fixed error and records how often it was called.
type fakeCatalogSource struct {
	cat   catalog.Catalog
	err   error
	calls int
}

func (s *fakeCatalogSource) Catalog(ctx context.Context) (catalog.Catalog, error) {
	s.calls++

	if err := ctx.Err(); err != nil {
		return catalog.Catalog{}, err
	}

	if s.err != nil {
		return catalog.Catalog{}, s.err
	}

	return s.cat, nil
}

// fakeReporter is an analysis.Reporter that writes a fixed body and then
// returns a fixed error. It records the metadata it was given, so a test can
// check what the CLI told the reporter about the run. onReport, when set, runs
// inside Report after the context is checked and before the body is written, so
// a test can cancel the run in the middle of rendering.
type fakeReporter struct {
	body     []byte
	err      error
	onReport func()
	meta     analysis.ReportMeta
	// calls is the number of times Report was called.
	calls int
}

func (r *fakeReporter) Report(ctx context.Context, w io.Writer, _ assessment.Assessment, meta analysis.ReportMeta) error {
	r.calls++
	r.meta = meta

	if err := ctx.Err(); err != nil {
		return err
	}

	if r.onReport != nil {
		r.onReport()
	}

	if len(r.body) > 0 {
		if _, err := w.Write(r.body); err != nil {
			return err
		}
	}

	return r.err
}

// harness holds the fakes the CLI is run with and what they were asked for, so
// each test says what it expects of the run without building deps itself. It
// wires one format per --format value, each with its own reporters, so a test can
// see which format a run picked.
type harness struct {
	inventory  *fakeInventorySource
	catalog    *fakeCatalogSource
	reporter   *fakeReporter
	comparison *fakeComparisonReporter
	// jsonReporter and jsonComparison are the reporters of the "json" format.
	jsonReporter   *fakeReporter
	jsonComparison *fakeComparisonReporter
	// pdfReporter and pdfComparison are the reporters of the "pdf" format.
	pdfReporter   *fakeReporter
	pdfComparison *fakeComparisonReporter
	// paths is every --inventory value the CLI passed to NewInventorySource.
	paths []string
}

// newHarness returns a harness whose fakes succeed.
func newHarness(t *testing.T) *harness {
	t.Helper()

	inv, err := inventory.New("aws", []inventory.Resource{{
		ID:       "i-0123456789abcdef0",
		RawType:  "AWS::EC2::Instance",
		Category: taxonomy.ComputeVM,
		Region:   "eu-west-1",
	}})
	if err != nil {
		t.Fatalf("inventory.New() returned unexpected error: %v", err)
	}

	return &harness{
		inventory:      &fakeInventorySource{inv: inv},
		catalog:        &fakeCatalogSource{cat: testutil.ScalewayCatalog(t)},
		reporter:       &fakeReporter{body: []byte(reportBody)},
		comparison:     &fakeComparisonReporter{},
		jsonReporter:   &fakeReporter{body: []byte(jsonReportBody)},
		jsonComparison: &fakeComparisonReporter{},
		pdfReporter:    &fakeReporter{body: []byte(pdfReportBody)},
		pdfComparison:  &fakeComparisonReporter{},
	}
}

// reporters returns the single-target reporter of every supported --format value,
// so a test can see which format rendered a report.
func (h *harness) reporters() map[string]*fakeReporter {
	return map[string]*fakeReporter{
		markdownFormat: h.reporter,
		jsonFormat:     h.jsonReporter,
		pdfFormat:      h.pdfReporter,
	}
}

// comparisons returns the comparison reporter of every supported --format value.
func (h *harness) comparisons() map[string]*fakeComparisonReporter {
	return map[string]*fakeComparisonReporter{
		markdownFormat: h.comparison,
		jsonFormat:     h.jsonComparison,
		pdfFormat:      h.pdfComparison,
	}
}

// deps returns the Deps the CLI is run with: a format per supported --format
// value, each with the reporters that format renders through.
func (h *harness) deps() cli.Deps {
	return cli.Deps{
		NewInventorySource: func(path string) analysis.InventorySource {
			h.paths = append(h.paths, path)

			return h.inventory
		},
		Targets: map[string]func() analysis.CatalogSource{
			targetKey: func() analysis.CatalogSource { return h.catalog },
		},
		Formats: map[string]cli.Format{
			markdownFormat: {
				NewReporter:           func(int) analysis.Reporter { return h.reporter },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return h.comparison },
			},
			jsonFormat: {
				NewReporter:           func(int) analysis.Reporter { return h.jsonReporter },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return h.jsonComparison },
			},
			pdfFormat: {
				NewReporter:           func(int) analysis.Reporter { return h.pdfReporter },
				NewComparisonReporter: func(int) analysis.ComparisonReporter { return h.pdfComparison },
			},
		},
	}
}

// run runs the CLI with args and returns the exit code, stdout and stderr.
func (h *harness) run(ctx context.Context, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer

	code = cli.Run(ctx, args, &out, &errOut, h.deps())

	return code, out.String(), errOut.String()
}

// checkErrorLine fails the test unless stderr is exactly one line that starts
// with the prefix and holds no character a terminal or a log reader would act
// on: no raw control character (the final newline aside), no format character
// and no line or paragraph separator.
func checkErrorLine(t *testing.T, stderr string) {
	t.Helper()

	if !strings.HasPrefix(stderr, errPrefix) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, errPrefix)
	}

	if !strings.HasSuffix(stderr, "\n") || strings.Count(stderr, "\n") != 1 {
		t.Errorf("stderr = %q, want exactly one line ending in a newline", stderr)
	}

	if len(stderr) <= len(errPrefix)+1 {
		t.Errorf("stderr = %q, want a message after the prefix", stderr)
	}

	if len(stderr) > maxErrorLine {
		t.Errorf("stderr is %d bytes, want at most %d", len(stderr), maxErrorLine)
	}

	// The line is read by a terminal and a log reader, so it must also be text.
	if !utf8.ValidString(stderr) {
		t.Errorf("stderr = %q is not valid UTF-8", stderr)
	}

	for _, r := range stderr {
		if r == '\n' {
			continue
		}

		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			t.Errorf("stderr = %q holds the raw character %U; it must be escaped", stderr, r)

			break
		}
	}
}

// TestRunAssessToStdout pins the happy path: the assessment runs, the report
// goes to stdout, and the inventory file is named by its base name.
func TestRunAssessToStdout(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath, "--target", targetKey)

	if code != 0 {
		t.Errorf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	if stdout != reportBody {
		t.Errorf("stdout = %q, want the reporter's output %q", stdout, reportBody)
	}

	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	if len(h.paths) != 1 || h.paths[0] != inventoryPath {
		t.Errorf("NewInventorySource() was called with %q, want [%q]", h.paths, inventoryPath)
	}

	if h.reporter.calls != 1 {
		t.Errorf("the reporter was called %d times, want 1", h.reporter.calls)
	}

	if got := h.reporter.meta.InventoryName; got != inventoryName {
		t.Errorf("ReportMeta.InventoryName = %q, want the base name %q", got, inventoryName)
	}
}

// TestRunAssessToFile pins --output: the report is written to the file, nothing
// goes to stdout, and a failure leaves an existing destination untouched.
func TestRunAssessToFile(t *testing.T) {
	t.Parallel()

	t.Run("writes a new file with mode 0600", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "report.md")
		h := newHarness(t)

		code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--output", dest)

		if code != 0 {
			t.Errorf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty with --output", stdout)
		}

		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("the report file was not written: %v", err)
		}

		if string(got) != reportBody {
			t.Errorf("report file = %q, want %q", got, reportBody)
		}

		checkFileMode(t, dest)
		checkOnlyFile(t, dir, "report.md")
	})

	t.Run("writes the pdf report's bytes unchanged", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "report.pdf")
		h := newHarness(t)

		code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--format", pdfFormat, "--output", dest)

		if code != 0 {
			t.Errorf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty with --output", stdout)
		}

		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("the report file was not written: %v", err)
		}

		// A PDF is binary: the file holds the reporter's bytes, never a text
		// rendering of them.
		if !bytes.Equal(got, []byte(pdfReportBody)) {
			t.Errorf("report file = %q, want the pdf reporter's bytes %q", got, pdfReportBody)
		}

		checkFileMode(t, dest)
		checkOnlyFile(t, dir, "report.pdf")
	})

	t.Run("replaces an existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "report.md")

		if err := os.WriteFile(dest, []byte("old report\n"), 0o600); err != nil {
			t.Fatalf("writing the existing destination: %v", err)
		}

		h := newHarness(t)

		code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--output", dest)

		if code != 0 {
			t.Errorf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty with --output", stdout)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading the report file: %v", err)
		}

		if string(got) != reportBody {
			t.Errorf("report file = %q, want %q", got, reportBody)
		}

		checkFileMode(t, dest)
	})

	t.Run("keeps the destination when the reporter fails", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "report.md")
		old := []byte("old report\n")

		if err := os.WriteFile(dest, old, 0o600); err != nil {
			t.Fatalf("writing the existing destination: %v", err)
		}

		h := newHarness(t)
		// A reporter may write part of its output before it fails; the CLI must
		// not let that reach the destination.
		h.reporter.body = []byte("# partial report\n")
		h.reporter.err = errors.New("the reporter failed")

		code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--output", dest)

		if code != 1 {
			t.Errorf("Run() = %d, want 1", code)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty", stdout)
		}

		checkErrorLine(t, stderr)

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading the destination after a failed run: %v", err)
		}

		if !bytes.Equal(got, old) {
			t.Errorf("destination = %q, want it unchanged as %q", got, old)
		}

		checkOnlyFile(t, dir, "report.md")
	})
}

// TestRunAssessRuntimeError pins the error line of a failed run: one line,
// prefixed, sanitised and capped, naming what failed.
func TestRunAssessRuntimeError(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inventory.err = errors.New(hostileMessage())

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath, "--target", targetKey)

	if code != 1 {
		t.Errorf("Run() = %d, want 1", code)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	checkErrorLine(t, stderr)

	// The message says which stage failed, which is how a user tells a bad
	// inventory from a bad catalog.
	if !strings.Contains(stderr, "inventory") {
		t.Errorf("stderr = %q, want it to name the inventory stage", stderr)
	}

	// Every hostile character is escaped, one by one, as strconv spells it.
	for _, want := range []string{
		`\n`, `\x1b`, `\u202e`, `\u2028`, `\u2029`, `\u0085`, `\x7f`, `\xff`, `\ufeff`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want the escape %s", stderr, want)
		}
	}

	// A message too long for the cap is cut, and says so.
	if !truncationRE.MatchString(stderr) {
		t.Errorf("stderr = %q, want it to end with a truncation marker", stderr)
	}
}

// TestRunAssessErrorLineEscapes pins that each character a terminal or a log
// reader would act on rather than show reaches the error line escaped, on the
// one line, as strconv spells it.
func TestRunAssessErrorLineEscapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		// want is the escape stderr must hold for msg.
		want string
	}{
		{name: "line feed", msg: "a\nb", want: `a\nb`},
		{name: "carriage return", msg: "a\rb", want: `a\rb`},
		{name: "terminal escape", msg: "a\x1b[2J", want: `a\x1b[2J`},
		{name: "right-to-left override", msg: "a" + string(rune(0x202e)) + "b", want: `a\u202eb`},
		{name: "line separator", msg: "a" + string(rune(0x2028)) + "b", want: `a\u2028b`},
		{name: "paragraph separator", msg: "a" + string(rune(0x2029)) + "b", want: `a\u2029b`},
		{name: "next line", msg: "a" + string(rune(0x0085)) + "b", want: `a\u0085b`},
		{name: "delete", msg: "a" + string(rune(0x7f)) + "b", want: `a\x7fb`},
		// A lone 0xff byte is not valid UTF-8; it is escaped, never passed on.
		{name: "invalid UTF-8 byte", msg: "a\xffb", want: `a\xffb`},
		{name: "byte order mark", msg: "a" + string(rune(0xfeff)) + "b", want: `a\ufeffb`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.inventory.err = errors.New(tc.msg)

			code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
				"--target", targetKey)

			if code != 1 {
				t.Errorf("Run() = %d, want 1", code)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			checkErrorLine(t, stderr)

			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr = %q, want it to hold the escape %s", stderr, tc.want)
			}
		})
	}
}

// TestRunAssessErrorLineCutIsNeverInsideAnEscape pins that a cut made by the cap
// falls between characters: a message padded so that the cap lands on U+202E
// still holds that character whole, or not at all, never half its escape.
func TestRunAssessErrorLineCutIsNeverInsideAnEscape(t *testing.T) {
	t.Parallel()

	// The pads straddle the cap, so the cut lands on either side of the escape
	// and inside each of its bytes in turn.
	for pad := 1000; pad <= 1040; pad++ {
		h := newHarness(t)
		h.inventory.err = errors.New(strings.Repeat("x", pad) + string(rune(0x202e)) + "tail")

		code, _, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath, "--target", targetKey)

		if code != 1 {
			t.Errorf("Run() = %d, want 1 with a pad of %d", code, pad)
		}

		checkErrorLine(t, stderr)

		if !truncationRE.MatchString(stderr) {
			t.Errorf("stderr = %q, want it to end with a truncation marker (pad %d)", stderr, pad)
		}

		// What is left once the complete escapes are taken out must hold no
		// backslash: a cut inside \u202e would leave one.
		if rest := escapeRE.ReplaceAllString(stderr, ""); strings.Contains(rest, `\`) {
			t.Errorf("stderr = %q holds half an escape; the cut must fall between characters (pad %d)",
				stderr, pad)
		}
	}
}

// TestRunAssessRuntimeErrorLeavesNoFile pins that a failed run with --output
// creates no destination at all when there was none.
func TestRunAssessRuntimeErrorLeavesNoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "report.md")
	h := newHarness(t)
	h.inventory.err = errors.New("the inventory file is not a valid inventory document")

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
		"--target", targetKey, "--output", dest)

	if code != 1 {
		t.Errorf("Run() = %d, want 1", code)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	checkErrorLine(t, stderr)

	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the destination exists after a failed run; stat error = %v", err)
	}

	checkOnlyFile(t, dir, "")
}

// TestRunAssessFormat pins --format for one target: markdown is the default, and
// the format a run names renders the report through its own reporter.
func TestRunAssessFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// format is the --format value, or empty when the run passes none.
		format string
		// wantBody is what stdout holds.
		wantBody string
		// wantReporter is the --format value whose reporter must have rendered the
		// report.
		wantReporter string
	}{
		{name: "no format is markdown", wantBody: reportBody, wantReporter: markdownFormat},
		{name: "markdown named explicitly", format: markdownFormat, wantBody: reportBody, wantReporter: markdownFormat},
		{name: "json", format: jsonFormat, wantBody: jsonReportBody, wantReporter: jsonFormat},
		{name: "pdf", format: pdfFormat, wantBody: pdfReportBody, wantReporter: pdfFormat},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			args := []string{"assess", "--inventory", inventoryPath, "--target", targetKey}
			if tc.format != "" {
				args = append(args, "--format", tc.format)
			}

			code, stdout, stderr := h.run(t.Context(), args...)

			if code != 0 {
				t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
			}

			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}

			if stdout != tc.wantBody {
				t.Errorf("stdout = %q, want the selected format's output %q", stdout, tc.wantBody)
			}

			// One format renders the report, through its own reporter.
			for name, reporter := range h.reporters() {
				want := 0
				if name == tc.wantReporter {
					want = 1
				}

				if reporter.calls != want {
					t.Errorf("the %s reporter was called %d times, want %d", name, reporter.calls, want)
				}
			}

			// One target renders a report, never a comparison.
			for name, comparison := range h.comparisons() {
				if comparison.calls != 0 {
					t.Errorf("the %s comparison reporter was called %d times, want 0", name, comparison.calls)
				}
			}
		})
	}
}

// TestRunAssessFormatUsage pins the usage error of a wrong --format: exit code 2,
// the message the format gives with its supported list, the usage text, nothing
// on stdout and no adapter built.
func TestRunAssessFormatUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
	}{
		{name: "an unknown format", format: "yaml"},
		{name: "an unknown format docx", format: "docx"},
		{name: "an empty format", format: ""},
		{name: "a format in another case", format: "JSON"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
				"--target", targetKey, "--format", tc.format)

			if code != 2 {
				t.Errorf("Run() = %d, want 2; stderr = %q", code, stderr)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			wantFirstLine := errPrefix + "unknown format " + text.Quote(tc.format) +
				"; supported formats: " + supportedFormats

			firstLine, _, _ := strings.Cut(stderr, "\n")
			if firstLine != wantFirstLine {
				t.Errorf("the first line = %q, want %q", firstLine, wantFirstLine)
			}

			// The alternatives are the supported formats, sorted, so a user who
			// mistypes one is offered the PDF report next to the others.
			for _, want := range []string{jsonFormat, markdownFormat, pdfFormat} {
				if !strings.Contains(firstLine, want) {
					t.Errorf("the first line %q, want it to offer %q among the alternatives", firstLine, want)
				}
			}

			if !strings.Contains(strings.ToLower(stderr), "usage") {
				t.Errorf("stderr = %q, want usage text", stderr)
			}

			if len(h.paths) != 0 || h.reporter.calls != 0 || h.jsonReporter.calls != 0 || h.pdfReporter.calls != 0 {
				t.Errorf("a usage error built adapters: inventory paths %q, markdown calls %d, json calls %d, pdf calls %d",
					h.paths, h.reporter.calls, h.jsonReporter.calls, h.pdfReporter.calls)
			}
		})
	}
}

// TestRunAssessPDFSchemaVersionUsage pins --schema-version with --format pdf (PRD
// #584 milestone 1, ADR 0049): the PDF report has no schemaVersion, so naming one
// is a usage error, and a run never quietly ignores the flag. The error line
// names both the flag and the format, the usage text follows it, nothing goes to
// standard output, and no report is rendered or inventory read.
func TestRunAssessPDFSchemaVersionUsage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
		"--target", targetKey, "--format", pdfFormat, "--schema-version", "3")

	if code != 2 {
		t.Errorf("Run() = %d, want 2; stderr = %q", code, stderr)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	firstLine, _, _ := strings.Cut(stderr, "\n")

	for _, want := range []string{"--schema-version", text.Quote(pdfFormat)} {
		if !strings.Contains(firstLine, want) {
			t.Errorf("the error line %q, want it to name %q", firstLine, want)
		}
	}

	if !strings.Contains(strings.ToLower(stderr), "usage") {
		t.Errorf("stderr = %q, want the usage text", stderr)
	}

	if h.pdfReporter.calls != 0 {
		t.Errorf("the pdf reporter was called %d times, want 0", h.pdfReporter.calls)
	}

	if len(h.paths) != 0 {
		t.Errorf("a usage error built adapters: inventory paths %q", h.paths)
	}
}

// TestRunAssessWithoutFormats pins that a Deps wired with no report format at all
// is a missing adapter, not a user error: --format defaults to markdown, which a
// complete Deps supports, so a run that holds no format lacks an adapter rather
// than naming an unknown one. The run fails with exit code 1 as a run-time error,
// on the one error line, with nothing on stdout and no adapter built.
func TestRunAssessWithoutFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		formats map[string]cli.Format
	}{
		{name: "nil formats"},
		{name: "no formats", formats: map[string]cli.Format{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			deps := h.deps()
			deps.Formats = tc.formats

			var out, errOut bytes.Buffer

			code := cli.Run(t.Context(), []string{"assess", "--inventory", inventoryPath, "--target", targetKey},
				&out, &errOut, deps)

			if code != 1 {
				t.Errorf("Run() = %d, want 1: a Deps without a report format is a wiring mistake, not a usage error; "+
					"stderr = %q", code, errOut.String())
			}

			if out.Len() != 0 {
				t.Errorf("stdout = %q, want empty", out.String())
			}

			checkErrorLine(t, errOut.String())

			// The line names the mistake: an adapter the command needs is missing.
			if !strings.Contains(errOut.String(), "adapter") {
				t.Errorf("stderr = %q, want the missing-adapter error", errOut.String())
			}

			// It is not the usage error of an unknown --format: markdown is valid,
			// the command was just wired without it.
			if strings.Contains(errOut.String(), "unknown format") {
				t.Errorf("stderr = %q, want the missing-adapter error, not the unknown-format usage error",
					errOut.String())
			}

			if len(h.paths) != 0 || h.reporter.calls != 0 || h.jsonReporter.calls != 0 {
				t.Errorf("the failed run built adapters: inventory paths %q, markdown calls %d, json calls %d",
					h.paths, h.reporter.calls, h.jsonReporter.calls)
			}
		})
	}
}

// TestRunAssessFormatFlagHelp pins that the usage text tells the user what
// --format accepts: the supported formats, sorted, and which one is the default.
func TestRunAssessFormatFlagHelp(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--help")

	if code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	entry := flagHelp(t, stdout, "--format")
	lowered := strings.ToLower(entry)

	for _, want := range []string{jsonFormat, markdownFormat, supportedFormats, "default"} {
		if !strings.Contains(lowered, want) {
			t.Errorf("the --format usage entry %q, want it to mention %q", entry, want)
		}
	}
}

// TestRunUsageErrors pins every usage error: exit code 2, usage text on stderr,
// nothing on stdout and no adapter built.
func TestRunUsageErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		// wantStderr lists values stderr must hold besides the usage text.
		wantStderr []string
		// wantFirstLine lists values the first line, the error itself, must hold.
		wantFirstLine []string
	}{
		{name: "no arguments", args: nil},
		{name: "unknown command", args: []string{"discover"}},
		{name: "assess without flags", args: []string{"assess"}},
		{name: "missing inventory", args: []string{"assess", "--target", targetKey}},
		{name: "missing target", args: []string{"assess", "--inventory", inventoryPath}},
		{
			name: "unknown flag",
			args: []string{"assess", "--inventory", inventoryPath, "--target", targetKey, "--bogus"},
		},
		{
			name: "extra positional argument",
			args: []string{"assess", "--inventory", inventoryPath, "--target", targetKey, "extra"},
		},
		{
			name:          "unknown target",
			args:          []string{"assess", "--inventory", inventoryPath, "--target", "nope"},
			wantStderr:    []string{targetKey},
			wantFirstLine: []string{targetKey},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			code, stdout, stderr := h.run(t.Context(), tc.args...)

			if code != 2 {
				t.Errorf("Run() = %d, want 2; stderr = %q", code, stderr)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			if !strings.Contains(strings.ToLower(stderr), "usage") {
				t.Errorf("stderr = %q, want usage text", stderr)
			}

			for _, want := range tc.wantStderr {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, want it to name %q", stderr, want)
				}
			}

			// The error is the first line; the usage text follows it. A user who
			// mistypes a target learns the supported ones from that line.
			firstLine, _, _ := strings.Cut(stderr, "\n")
			for _, want := range tc.wantFirstLine {
				if !strings.Contains(firstLine, want) {
					t.Errorf("the first line %q, want it to name %q", firstLine, want)
				}
			}

			if len(h.paths) != 0 || h.reporter.calls != 0 {
				t.Errorf("a usage error built adapters: inventory paths %q, reporter calls %d",
					h.paths, h.reporter.calls)
			}
		})
	}
}

// TestRunVersion pins that the version command still works: the version line,
// exit code 0 and nothing on stderr.
func TestRunVersion(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			code, stdout, stderr := h.run(t.Context(), arg)

			if code != 0 {
				t.Errorf("Run(%q) = %d, want 0; stderr = %q", arg, code, stderr)
			}

			if want := version.String() + "\n"; stdout != want {
				t.Errorf("stdout = %q, want %q", stdout, want)
			}

			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

// TestRunCancelled pins that a cancelled run, as SIGINT gives it, stops with
// exit code 1 and one error line.
func TestRunCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	h := newHarness(t)
	code, stdout, stderr := h.run(ctx, "assess", "--inventory", inventoryPath, "--target", targetKey)

	if code != 1 {
		t.Errorf("Run() = %d, want 1", code)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	checkErrorLine(t, stderr)

	if h.reporter.calls != 0 {
		t.Errorf("the reporter was called %d times for a cancelled run, want 0", h.reporter.calls)
	}
}

// TestRunHelp pins every way to ask for help: the usage text on stdout, exit
// code 0, nothing on stderr and no adapter built.
func TestRunHelp(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"help"},
		{"-h"},
		{"-help"},
		{"--help"},
		{"assess", "-h"},
		{"assess", "--help"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			code, stdout, stderr := h.run(t.Context(), args...)

			if code != 0 {
				t.Errorf("Run(%q) = %d, want 0; stderr = %q", args, code, stderr)
			}

			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("stdout = %q, want the usage text", stdout)
			}

			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}

			if len(h.paths) != 0 || h.reporter.calls != 0 {
				t.Errorf("help built adapters: inventory paths %q, reporter calls %d",
					h.paths, h.reporter.calls)
			}
		})
	}
}

// TestRunCancelledWhileRendering pins that a run cancelled after the report was
// rendered, as SIGINT gives it, writes nothing: no report on stdout, no half
// report in the --output file and no temporary file left behind.
func TestRunCancelledWhileRendering(t *testing.T) {
	t.Parallel()

	t.Run("to standard output", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		h := newHarness(t)
		h.reporter.onReport = cancel

		code, stdout, stderr := h.run(ctx, "assess", "--inventory", inventoryPath, "--target", targetKey)

		if code != 1 {
			t.Errorf("Run() = %d, want 1", code)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty for a run cancelled while rendering", stdout)
		}

		checkErrorLine(t, stderr)

		if h.reporter.calls != 1 {
			t.Errorf("the reporter was called %d times, want 1", h.reporter.calls)
		}
	})

	t.Run("to an existing file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "report.md")
		old := []byte("old report\n")

		if err := os.WriteFile(dest, old, 0o600); err != nil {
			t.Fatalf("writing the existing destination: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		h := newHarness(t)
		h.reporter.onReport = cancel

		code, stdout, stderr := h.run(ctx, "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--output", dest)

		if code != 1 {
			t.Errorf("Run() = %d, want 1", code)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty", stdout)
		}

		checkErrorLine(t, stderr)

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading the destination after a cancelled run: %v", err)
		}

		if !bytes.Equal(got, old) {
			t.Errorf("destination = %q, want it unchanged as %q", got, old)
		}

		checkOnlyFile(t, dir, "report.md")
	})
}

// TestRunAssessQuotesInventoryPath pins that a path holding a line feed, which
// the CLI refuses before it reads anything, reaches the error line quoted by
// text.Quote: the escape the quoting added appears once, never doubled.
func TestRunAssessQuotesInventoryPath(t *testing.T) {
	t.Parallel()

	// A path the shell would let through and a line feed would split.
	path := "dir/a\nb.json"
	// The report names the inventory file by its base name, so that is the value
	// the error shows.
	quoted := text.Quote(filepath.Base(path))

	h := newHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", path, "--target", targetKey)

	if code != 1 {
		t.Errorf("Run() = %d, want 1", code)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	checkErrorLine(t, stderr)

	if !strings.Contains(stderr, quoted) {
		t.Errorf("stderr = %q, want it to hold the quoted name %s", stderr, quoted)
	}

	if strings.Contains(stderr, `\\n`) {
		t.Errorf("stderr = %q holds a doubled escape; the quoted name must reach the user as it is", stderr)
	}

	if h.inventory.calls != 0 || h.reporter.calls != 0 {
		t.Errorf("a refused inventory name read the inventory %d times and reported %d, want 0 and 0",
			h.inventory.calls, h.reporter.calls)
	}
}

// TestRunAssessOutputHasNoTemporaryFileWhileLoading pins that the --output file
// is not created, even temporarily, before the report is rendered: while the
// inventory is loaded the output directory holds nothing.
func TestRunAssessOutputHasNoTemporaryFileWhileLoading(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "report.md")

	h := newHarness(t)

	var seen []string
	h.inventory.onInventory = func() {
		seen = append(seen, tempReportFiles(t, dir)...)
	}

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
		"--target", targetKey, "--output", dest)

	if code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	if len(seen) != 0 {
		t.Errorf("the output directory holds %q while the inventory is loaded, want nothing: "+
			"the report file must not exist before the report is rendered", seen)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("the report file was not written: %v", err)
	}

	if string(got) != reportBody {
		t.Errorf("report file = %q, want %q", got, reportBody)
	}

	checkOnlyFile(t, dir, "report.md")
}

// TestRunAssessOutputClashWithInventory pins that a report is never written over
// the inventory it assesses, whether --output repeats the --inventory path or
// names another hard link to the same file: the run fails, names the clash, and
// leaves the inventory as it was.
func TestRunAssessOutputClashWithInventory(t *testing.T) {
	t.Parallel()

	t.Run("the same path", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "inventory.json")

		checkOutputClash(t, path, path, dir, "inventory.json")
	})

	t.Run("a hard link", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "inventory.json")
		dest := filepath.Join(dir, "report.md")

		writeInventory(t, path)

		if err := os.Link(path, dest); err != nil {
			t.Skipf("hard links are not supported here: %v", err)
		}

		checkOutputClash(t, path, dest, dir, "inventory.json", "report.md")
	})

	t.Run("the inventory is a symbolic link to the destination", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "real.json")
		path := filepath.Join(dir, "inventory.json")

		writeInventory(t, dest)

		if err := os.Symlink("real.json", path); err != nil {
			t.Skipf("symbolic links are not supported here: %v", err)
		}

		checkOutputClash(t, path, dest, dir, "inventory.json", "real.json")
	})

	t.Run("the destination is a symbolic link to the inventory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "inventory.json")
		dest := filepath.Join(dir, "report.md")

		writeInventory(t, path)

		if err := os.Symlink("inventory.json", dest); err != nil {
			t.Skipf("symbolic links are not supported here: %v", err)
		}

		checkOutputClash(t, path, dest, dir, "inventory.json", "report.md")

		// The clash names the destination, so the report is never written
		// through the link: the link itself must still be there.
		checkSymlink(t, dest, "inventory.json")
	})
}

// TestRunAssessOutputNotARegularFile pins that --output naming something that is
// not a report file fails the run and leaves it as it was: a directory, or a
// symbolic link to one, is never replaced, no temporary file is left behind,
// and the run is refused before the inventory is read.
func TestRunAssessOutputNotARegularFile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// link makes the destination a symbolic link to the directory instead of
		// being that directory.
		link bool
		// wantEntries lists the output directory after the run, sorted.
		wantEntries []string
	}{
		{name: "a directory", wantEntries: []string{"real"}},
		{
			name:        "a symbolic link to a directory",
			link:        true,
			wantEntries: []string{"real", "reports"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			real := filepath.Join(dir, "real")

			if err := os.Mkdir(real, 0o700); err != nil {
				t.Fatalf("creating the destination directory: %v", err)
			}

			dest := real
			if tc.link {
				dest = filepath.Join(dir, "reports")

				if err := os.Symlink("real", dest); err != nil {
					t.Skipf("symbolic links are not supported here: %v", err)
				}
			}

			marker := filepath.Join(real, "keep.md")
			if err := os.WriteFile(marker, []byte("keep\n"), 0o600); err != nil {
				t.Fatalf("writing a file into the destination directory: %v", err)
			}

			h := newHarness(t)

			code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
				"--target", targetKey, "--output", dest)

			if code != 1 {
				t.Errorf("Run() = %d, want 1", code)
			}

			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}

			checkErrorLine(t, stderr)

			info, err := os.Stat(real)
			if err != nil {
				t.Fatalf("the destination directory is gone: %v", err)
			}

			if !info.IsDir() {
				t.Errorf("the destination directory is no longer a directory: mode %s", info.Mode())
			}

			if got, err := os.ReadFile(marker); err != nil || string(got) != "keep\n" {
				t.Errorf("the file inside the destination directory = %q (error %v), want it unchanged",
					got, err)
			}

			// A refused destination is refused as it stands, so a link to the
			// directory is still that link: the report must never take its place.
			if tc.link {
				checkSymlink(t, dest, "real")
			}

			if got := tempReportFiles(t, dir); len(got) != 0 {
				t.Errorf("the output directory holds the temporary files %q, want none", got)
			}

			checkEntries(t, dir, tc.wantEntries...)

			// The destination is refused before anything is read.
			if h.inventory.calls != 0 || h.reporter.calls != 0 {
				t.Errorf("a refused output read the inventory %d times and reported %d, want 0 and 0",
					h.inventory.calls, h.reporter.calls)
			}
		})
	}
}

// TestRunAssessOutputSymlinkIsReplacedNotFollowed pins that --output naming a
// symbolic link to a regular file, or a dangling link, is accepted: the rename
// replaces the link itself with the report, so the link's target keeps its old
// content and the report never reaches it.
func TestRunAssessOutputSymlinkIsReplacedNotFollowed(t *testing.T) {
	t.Parallel()

	t.Run("a link to a regular file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		target := filepath.Join(dir, "target.md")
		dest := filepath.Join(dir, "report.md")
		old := []byte("old report\n")

		if err := os.WriteFile(target, old, 0o600); err != nil {
			t.Fatalf("writing the link's target: %v", err)
		}

		if err := os.Symlink("target.md", dest); err != nil {
			t.Skipf("symbolic links are not supported here: %v", err)
		}

		h := newHarness(t)

		code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--output", dest)

		if code != 0 {
			t.Errorf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty", stdout)
		}

		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}

		// The link was replaced, not followed: the destination is the report.
		info, err := os.Lstat(dest)
		if err != nil {
			t.Fatalf("the destination is gone: %v", err)
		}

		if !info.Mode().IsRegular() {
			t.Errorf("the destination has mode %s, want a regular file", info.Mode())
		}

		if got, err := os.ReadFile(dest); err != nil || string(got) != reportBody {
			t.Errorf("the report file = %q (error %v), want %q", got, err, reportBody)
		}

		if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, old) {
			t.Errorf("the link's target = %q (error %v), want it unchanged as %q", got, err, old)
		}

		checkEntries(t, dir, "report.md", "target.md")
	})

	t.Run("a dangling link", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		dest := filepath.Join(dir, "report.md")

		if err := os.Symlink("missing.md", dest); err != nil {
			t.Skipf("symbolic links are not supported here: %v", err)
		}

		h := newHarness(t)

		code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
			"--target", targetKey, "--output", dest)

		if code != 0 {
			t.Errorf("Run() = %d, want 0; stderr = %q", code, stderr)
		}

		if stdout != "" {
			t.Errorf("stdout = %q, want empty", stdout)
		}

		if stderr != "" {
			t.Errorf("stderr = %q, want empty", stderr)
		}

		info, err := os.Lstat(dest)
		if err != nil {
			t.Fatalf("the destination is gone: %v", err)
		}

		if !info.Mode().IsRegular() {
			t.Errorf("the destination has mode %s, want a regular file", info.Mode())
		}

		if got, err := os.ReadFile(dest); err != nil || string(got) != reportBody {
			t.Errorf("the report file = %q (error %v), want %q", got, err, reportBody)
		}

		checkEntries(t, dir, "report.md")
	})
}

// TestRunAssessOutputRenameFailureLeavesNoTemporaryFile pins that a rename that
// fails after the report was written leaves no temporary file behind: the
// destination becomes a directory while the report is rendered, so the final
// rename fails and only the destination is left in the output directory.
func TestRunAssessOutputRenameFailureLeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "report.md")

	h := newHarness(t)
	// checkOutput has passed, since the destination did not exist; a directory
	// takes its place before the report is written, so the rename fails.
	h.reporter.onReport = func() {
		if err := os.Mkdir(dest, 0o700); err != nil {
			t.Errorf("creating the destination directory: %v", err)
		}
	}

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", inventoryPath,
		"--target", targetKey, "--output", dest)

	if code != 1 {
		t.Errorf("Run() = %d, want 1", code)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	checkErrorLine(t, stderr)

	if got := tempReportFiles(t, dir); len(got) != 0 {
		t.Errorf("the output directory holds the temporary files %q, want none", got)
	}

	checkOnlyFile(t, dir, "report.md")
}

// checkSymlink fails the test unless path is a symbolic link to want.
func checkSymlink(t *testing.T, path, want string) {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s has mode %s, want it still a symbolic link", path, info.Mode())

		return
	}

	got, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("reading the link %s: %v", path, err)
	}

	if got != want {
		t.Errorf("%s points at %q, want %q", path, got, want)
	}
}

// checkEntries fails the test unless dir holds exactly the entries want, in the
// sorted name order os.ReadDir gives.
func checkEntries(t *testing.T, dir string, want ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the output directory: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	if !slices.Equal(names, want) {
		t.Errorf("the output directory holds %q, want %q", names, want)
	}
}

// checkOutputClash pins the run of assess with --inventory path and --output
// dest, two names of the same file: exit code 1, one error line naming the
// clash, nothing on stdout, and the inventory unchanged byte for byte.
func checkOutputClash(t *testing.T, path, dest, dir string, wantEntries ...string) {
	t.Helper()

	original := writeInventory(t, path)

	h := newHarness(t)

	code, stdout, stderr := h.run(t.Context(), "assess", "--inventory", path,
		"--target", targetKey, "--output", dest)

	if code != 1 {
		t.Errorf("Run() = %d, want 1", code)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	checkErrorLine(t, stderr)

	firstLine, _, _ := strings.Cut(stderr, "\n")
	if !strings.Contains(firstLine, text.Quote(path)) && !strings.Contains(firstLine, text.Quote(dest)) {
		t.Errorf("the first line %q, want it to name the clashing file", firstLine)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the inventory file is gone after the run: %v", err)
	}

	if !bytes.Equal(got, original) {
		t.Errorf("the inventory file = %q, want it unchanged as %q", got, original)
	}

	if want := tempReportFiles(t, dir); len(want) != 0 {
		t.Errorf("the output directory holds the temporary files %q, want none", want)
	}

	checkEntries(t, dir, wantEntries...)

	// The clash is refused before anything is read.
	if h.inventory.calls != 0 || h.reporter.calls != 0 {
		t.Errorf("a refused output read the inventory %d times and reported %d, want 0 and 0",
			h.inventory.calls, h.reporter.calls)
	}
}

// writeInventory writes an inventory file at path, which the fake inventory
// source never reads, and returns what it wrote.
func writeInventory(t *testing.T, path string) []byte {
	t.Helper()

	content := []byte("{\"schemaVersion\":1}\n")

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing the inventory file: %v", err)
	}

	return content
}

// tempReportFiles returns the names in dir that are a temporary report file: an
// entry whose name starts with the prefix the CLI writes one under.
func tempReportFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the output directory: %v", err)
	}

	var names []string

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".shift2eu-report-") {
			names = append(names, e.Name())
		}
	}

	return names
}

// hostileMessage is an error text no error line may reproduce: a line feed that
// would end the line, a terminal escape that would clear the screen, a
// right-to-left override that would reverse it, the Unicode line and paragraph
// separators, the C1 next line, DEL, a byte that is not valid UTF-8, a byte
// order mark, and enough length to grow without a cap. Every non-ASCII
// character is built from its code point, so this source file stays ASCII.
func hostileMessage() string {
	var b strings.Builder

	for _, s := range []string{
		"inventory ",
		"\n",
		"\x1b[2J",
		string(rune(0x202e)),
		string(rune(0x2028)),
		string(rune(0x2029)),
		string(rune(0x0085)),
		string(rune(0x7f)),
		"\xff",
		string(rune(0xfeff)),
	} {
		b.WriteString(s)
	}

	for b.Len() < 5000 {
		b.WriteString("x")
	}

	return b.String()
}

// checkFileMode fails the test unless path was written with mode 0600. Modes
// are not modelled the same way on every operating system, so the check runs
// where they are.
func checkFileMode(t *testing.T, path string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("%s has mode %04o, want 0600", path, got)
	}
}

// checkOnlyFile fails the test unless dir holds exactly one entry, want, or no
// entry at all when want is empty. A temporary file left behind by a failed
// --output is a second entry.
func checkOnlyFile(t *testing.T, dir, want string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the output directory: %v", err)
	}

	if want == "" {
		if len(entries) != 0 {
			t.Errorf("the output directory holds %d entries, want none", len(entries))
		}

		return
	}

	if len(entries) != 1 || entries[0].Name() != want {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Errorf("the output directory holds %q, want only %q", names, want)
	}
}
