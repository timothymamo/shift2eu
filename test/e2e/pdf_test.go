//go:build e2e

package e2e_test

// This file pins --format pdf end to end (PRD #584 milestone 1, ADR 0049): the
// built binary, run with no environment, turns an inventory into the PDF report
// the pdfreport adapter renders, for one target and for the comparison of every
// target, and writes the same bytes into the --output file. It also pins what
// the audit's hostile corpus (PRD #175, ADR 0040) does to the format, and that
// every sample inventory gives a PDF against every target.
//
// The PDF's structural invariants are pdfreport's own tests' business: they live
// beside the writer, and e2e-is-black-box refuses every package of the module
// here but internal/testutil. What this tier can see is the framing of a file a
// reader receives: the header, the %%EOF line, and bytes that are printable
// ASCII plus line feeds. The goldens under testdata/ hold the rest of it.
//
// A failed run's stderr is reported by its first line, the error itself: a usage
// error follows it with the whole usage text, which no failure needs.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/testutil"
)

const (
	// pdfFormat is the --format value that selects the PDF report.
	pdfFormat = "pdf"
	// pdfHeader starts every PDF file the writer emits: "%PDF-" and the file's
	// PDF version.
	pdfHeader = "%PDF-"
	// pdfEOF ends every PDF file the writer emits: the %%EOF marker and the line
	// feed after it.
	pdfEOF = "%%EOF\n"
	// pdfSingleGolden and pdfComparisonGolden are the CLI goldens of the shared
	// fixture: one target's report, and the comparison of every target. They are
	// the specification of the PDF's content for the CLI's path, as
	// assess-ovhcloud.golden is for the Markdown report.
	pdfSingleGolden     = "assess-ovhcloud.pdf.golden"
	pdfComparisonGolden = "assess-all.pdf.golden"
	// hostileCorpusDir is the corpus the scheduled security audit runs the
	// released CLI on (PRD #175, ADR 0040), relative to this package's directory:
	// one JSON inventory per kind of hostility, every value sanitised (CLAUDE.md
	// rule 2).
	hostileCorpusDir = "../../.github/security-audit/testdata/hostile-inventories"
)

// TestAssessPDFReport pins --format pdf on the shared fixture end to end: the
// built binary writes the PDF report of one target, and the comparison of every
// target for --target all, to standard output, matching the golden file of each,
// and --output writes exactly the same bytes into the file.
//
// A golden file is written by the change that adds the format, with -update, and
// reviewed like any other golden: it is the specification of what the PDF holds.
func TestAssessPDFReport(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")

	tests := []struct {
		name   string
		target string
		golden string
	}{
		{name: "one target", target: ovhcloudKey, golden: pdfSingleGolden},
		{name: "every target is a comparison", target: allKey, golden: pdfComparisonGolden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := assess(t, tc.target, inventory, "--format", pdfFormat)

			if got.code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, firstLine(got.stderr))
			}

			if got.stderr != "" {
				t.Errorf("stderr = %q, want empty", got.stderr)
			}

			checkPDF(t, []byte(got.stdout))

			testutil.Golden(t, goldenPath(tc.golden), []byte(got.stdout))

			t.Run("--output writes the same bytes", func(t *testing.T) {
				t.Parallel()

				dest := filepath.Join(t.TempDir(), "report.pdf")

				toFile := assess(t, tc.target, inventory, "--format", pdfFormat, "--output", dest)

				if toFile.code != 0 {
					t.Fatalf("exit code = %d, want 0; stderr = %q", toFile.code, firstLine(toFile.stderr))
				}

				if toFile.stdout != "" {
					t.Errorf("stdout = %q, want empty with --output", toFile.stdout)
				}

				if toFile.stderr != "" {
					t.Errorf("stderr = %q, want empty", toFile.stderr)
				}

				written, err := os.ReadFile(dest)
				if err != nil {
					t.Fatalf("the report file was not written: %v", err)
				}

				if !bytes.Equal(written, []byte(got.stdout)) {
					t.Errorf("the --output file holds %d bytes, want the %d bytes standard output holds",
						len(written), len(got.stdout))
				}
			})
		})
	}
}

// TestAssessPDFUsage pins that --schema-version is refused with --format pdf: the
// PDF report has no schemaVersion, and a run never quietly ignores the flag. It
// is a usage error (exit code 2), the error line names both flags, nothing is
// written to standard output and no --output file is created.
func TestAssessPDFUsage(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "report.pdf")

	got := run(t, emptyEnv(), "assess", "--inventory", filepath.Join("testdata", "inventory.json"),
		"--target", scalewayKey, "--format", pdfFormat, "--schema-version", "3", "--output", dest)

	if got.code != 2 {
		t.Errorf("exit code = %d, want 2; stderr = %q", got.code, firstLine(got.stderr))
	}

	if got.stdout != "" {
		t.Errorf("stdout = %q, want empty", got.stdout)
	}

	if !strings.Contains(strings.ToLower(got.stderr), "usage") {
		t.Errorf("stderr = %q, want usage text", got.stderr)
	}

	// The error is the first line; the usage text follows it.
	first := firstLine(got.stderr)

	for _, want := range []string{"--schema-version", "--format", `"` + pdfFormat + `"`} {
		if !strings.Contains(first, want) {
			t.Errorf("the error line %q, want it to name %q", first, want)
		}
	}

	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the --output file exists after a usage error; stat error = %v", err)
	}
}

// TestAssessPDFNamesTheBuildVersion pins that the PDF names the build it was
// written by: the version line in its footer is the one `shift2eu version`
// prints, of the same binary, never a line the wiring makes up or leaves empty.
func TestAssessPDFNamesTheBuildVersion(t *testing.T) {
	t.Parallel()

	version := run(t, emptyEnv(), "version")

	if version.code != 0 {
		t.Fatalf("exit code of version = %d, want 0; stderr = %q", version.code, firstLine(version.stderr))
	}

	line := strings.TrimSuffix(version.stdout, "\n")
	if line == "" {
		t.Fatalf("shift2eu version printed %q, want a version line", version.stdout)
	}

	got := assess(t, scalewayKey, filepath.Join("testdata", "inventory.json"), "--format", pdfFormat)

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", got.code, firstLine(got.stderr))
	}

	checkPDF(t, []byte(got.stdout))

	// The PDF draws the version line as a value inside a content stream's literal
	// string, so it appears in the file in that form: the line itself, with the
	// three characters a literal string treats as syntax escaped.
	if want := pdfLiteral(line); !strings.Contains(got.stdout, want) {
		t.Errorf("the PDF holds no version line: want %q, the line shift2eu version prints of this build, "+
			"as the writer puts it in the file", want)
	}
}

// TestAssessPDFHostileInventoryCorpus pins --format pdf on the corpus the
// scheduled security audit runs the released CLI on (PRD #175, ADR 0040, issue
// #661): every one of its files is untrusted input. The Markdown report of the
// same binary is the reference, so no list of files is written down here: a file
// the Markdown report of the target accepts must give a PDF with exit code 0 and
// the PDF's framing, and a file it refuses must be refused with --format pdf
// too, with the same exit code, the same error line and nothing on standard
// output.
//
// The parent classifies every file with the Markdown report, which is also how it
// counts the corpus's two halves, and the parallel subtests only compare the PDF
// run with that reference: a count that is all of one half would leave the other
// half of this test unrun.
func TestAssessPDFHostileInventoryCorpus(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(hostileCorpusDir)
	if err != nil {
		t.Fatalf("read the audit's hostile inventory corpus: %v", err)
	}

	accepted, refused := 0, 0

	files := make([]corpusFile, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		inventory := filepath.Join(hostileCorpusDir, entry.Name())

		markdown := assess(t, scalewayKey, inventory)

		if markdown.code != 0 {
			refused++
		} else {
			accepted++
		}

		files = append(files, corpusFile{name: entry.Name(), inventory: inventory, markdown: markdown})
	}

	if accepted == 0 || refused == 0 {
		t.Errorf("the corpus holds %d inventories the Markdown report accepts and %d it refuses, "+
			"want at least one of each: this test pins both halves of the corpus", accepted, refused)
	}

	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			t.Parallel()

			got := assess(t, scalewayKey, file.inventory, "--format", pdfFormat)

			if file.markdown.code != 0 {
				if got.code != file.markdown.code {
					t.Fatalf("exit code with --format pdf = %d, want %d, the exit code the Markdown report gives; "+
						"stderr = %q", got.code, file.markdown.code, firstLine(got.stderr))
				}

				if got.stdout != "" {
					t.Errorf("stdout = %q with --format pdf, want empty for an inventory the decoder refuses: "+
						"a refusal writes no report", got.stdout)
				}

				if want := firstLine(file.markdown.stderr); firstLine(got.stderr) != want {
					t.Errorf("the --format pdf refusal is %q, want the Markdown report's refusal %q",
						firstLine(got.stderr), want)
				}

				return
			}

			if got.code != 0 {
				t.Fatalf("exit code with --format pdf = %d, want 0, the exit code the Markdown report gives; "+
					"stderr = %q", got.code, firstLine(got.stderr))
			}

			if got.stderr != "" {
				t.Errorf("stderr = %q, want empty", got.stderr)
			}

			checkPDF(t, []byte(got.stdout))
		})
	}
}

// corpusFile is one JSON file of the hostile corpus, as the parent test
// classified it: the path the binary reads, and the Markdown report's run on it,
// which accepted or refused it.
type corpusFile struct {
	// name is the file's name in the corpus, and the name of its subtest.
	name string
	// inventory is the file's path, as the run's --inventory argument.
	inventory string
	// markdown is the Markdown report's run on the file: the reference the PDF
	// run is compared with, for the exit code, the standard output and the error
	// line.
	markdown result
}

// TestExamplesPDFReports pins that every sample inventory gives a PDF against
// every target the binary offers, with estimates on and with --no-estimate: exit
// code 0, the file's framing, and the same bytes from two runs, so nothing in the
// report depends on map order or the clock.
func TestExamplesPDFReports(t *testing.T) {
	t.Parallel()

	targets := []string{ovhcloudKey, scalewayKey, upcloudKey, allKey}
	estimateModes := []struct {
		name  string
		flags []string
	}{
		{name: "estimates"},
		{name: "no estimates", flags: []string{"--no-estimate"}},
	}

	for _, source := range sampleSources {
		for _, target := range targets {
			for _, mode := range estimateModes {
				t.Run(source+"/"+target+"/"+mode.name, func(t *testing.T) {
					t.Parallel()

					inventory := sampleInventoryPath(source)
					args := append([]string{"--format", pdfFormat}, mode.flags...)

					first := assess(t, target, inventory, args...)

					if first.code != 0 {
						t.Fatalf("exit code = %d, want 0; stderr = %q", first.code, firstLine(first.stderr))
					}

					if first.stderr != "" {
						t.Errorf("stderr = %q, want empty", first.stderr)
					}

					checkPDF(t, []byte(first.stdout))

					second := assess(t, target, inventory, args...)

					if second.code != 0 {
						t.Fatalf("exit code of the second run = %d, want 0; stderr = %q",
							second.code, firstLine(second.stderr))
					}

					if first.stdout != second.stdout {
						t.Errorf("two runs differ: %d bytes and %d bytes", len(first.stdout), len(second.stdout))
					}
				})
			}
		}
	}
}

// checkPDF checks the framing of every PDF the writer emits, as far as a
// black-box test can see it: the file starts with the header, ends with the
// %%EOF line, and holds nothing but printable ASCII and line feeds, so writing
// it to a terminal cannot send an escape sequence and a golden file diffs as
// text. pdfreport's own structural invariants (object types, fonts, the /Info
// entry, the names a file must not hold, the text runs inside their boxes and a
// heading never ending a page) are checked beside the writer, which this tier
// may not import.
func checkPDF(t *testing.T, data []byte) {
	t.Helper()

	if len(data) == 0 {
		t.Fatal("the PDF is empty")
	}

	if !bytes.HasPrefix(data, []byte(pdfHeader)) {
		t.Fatalf("the PDF starts with %q, want %q", data[:min(len(data), len(pdfHeader)+8)], pdfHeader)
	}

	if !bytes.HasSuffix(data, []byte(pdfEOF)) {
		t.Fatalf("the PDF ends with %q, want %q",
			data[max(0, len(data)-len(pdfEOF)-8):], pdfEOF)
	}

	for i, b := range data {
		if b == '\n' || (b >= 0x20 && b <= 0x7E) {
			continue
		}

		t.Fatalf("byte %d of the PDF is %#02x, outside printable ASCII plus line feeds: %q",
			i, b, data[max(0, i-12):min(len(data), i+12)])
	}
}

// pdfLiteral returns line as the writer puts it in a content stream: inside a PDF
// literal string, where ( ) and \ take a backslash. A build's version line holds
// no backslash, the only other escape the writer applies to it, and it is short
// enough to stay on one line, so the whole string appears in the file.
func pdfLiteral(line string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(line)

	return "(" + escaped + ")"
}

// firstLine returns the first line of s: of a failed run's stderr, the error
// line, which the usage text follows for a usage error. A failure message names
// that line, never the whole usage text.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")

	return line
}
