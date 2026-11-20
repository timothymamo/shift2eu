package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/aireview/internal/review"
)

// TestReportSkippedPrintsTheWarningAndAppendsTheSummary pins what the entry
// point does with a skipped run (issues #367, #376, ADR 0033): one warning
// annotation on its stdout, and one line appended to the job summary when one is
// set, after what the file already holds. A summary it cannot write is one more
// warning, and the skip stays no failure: reportSkipped returns nothing that
// could fail the run. The test writes to files of its own, never to the
// process's stdout, so it shares no process state.
func TestReportSkippedPrintsTheWarningAndAppendsTheSummary(t *testing.T) {
	t.Parallel()

	skip := review.Skip{Reason: review.SkipNoUsableAnswer, Message: "The model gave no usable answer twice, so the AI review was skipped."}

	cases := []struct {
		name string
		// summary returns the job summary's path in dir, "" for none.
		summary func(dir string) string
		// before is what the summary file holds before the run, when it exists.
		before string
		// wantSummary is what the summary file must hold after, when it exists.
		wantSummary string
		// wantLines are the lines stdout must hold, by their prefix.
		wantLines []string
	}{
		{
			name:        "a summary is appended to",
			summary:     func(dir string) string { return filepath.Join(dir, "summary.md") },
			before:      "# Earlier step\n",
			wantSummary: "# Earlier step\n" + review.SkipSummaryLine(skip) + "\n",
			wantLines:   []string{"::warning title=AI review skipped::"},
		},
		{
			name:      "no summary is set",
			summary:   func(string) string { return "" },
			wantLines: []string{"::warning title=AI review skipped::"},
		},
		{
			name:      "a summary that cannot be written is one more warning",
			summary:   func(dir string) string { return filepath.Join(dir, "missing", "summary.md") },
			wantLines: []string{"::warning title=AI review skipped::", "::warning title=AI review summary not written::"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			summary := tc.summary(dir)

			if tc.before != "" {
				if err := os.WriteFile(summary, []byte(tc.before), 0o600); err != nil {
					t.Fatalf("write the job summary: %v", err)
				}
			}

			stdout, err := os.Create(filepath.Join(dir, "stdout"))
			if err != nil {
				t.Fatalf("create the file stdout is written to: %v", err)
			}

			reportSkipped(stdout, summary, skip)

			if err := stdout.Close(); err != nil {
				t.Fatalf("close stdout's file: %v", err)
			}

			written, err := os.ReadFile(stdout.Name())
			if err != nil {
				t.Fatalf("read stdout's file: %v", err)
			}

			lines := strings.Split(strings.TrimSuffix(string(written), "\n"), "\n")
			if len(lines) != len(tc.wantLines) {
				t.Fatalf("stdout holds %d line(s) (%q), want %d", len(lines), written, len(tc.wantLines))
			}

			for i, want := range tc.wantLines {
				if !strings.HasPrefix(lines[i], want) {
					t.Errorf("stdout's line %d is %q, want it to start with %q", i+1, lines[i], want)
				}
			}

			if lines[0] != review.SkipAnnotation(skip) {
				t.Errorf("the warning is %q, want review.SkipAnnotation's %q", lines[0], review.SkipAnnotation(skip))
			}

			if tc.wantSummary == "" {
				return
			}

			got, err := os.ReadFile(summary)
			if err != nil {
				t.Fatalf("read the job summary: %v", err)
			}

			if string(got) != tc.wantSummary {
				t.Errorf("the job summary is %q, want %q: one line appended after what it held", got, tc.wantSummary)
			}
		})
	}
}
