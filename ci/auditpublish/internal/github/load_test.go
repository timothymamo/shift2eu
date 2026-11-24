package github_test

// Group E of PR 4b: the previous merged report (PRD #175 milestone 2, "Report
// and triage"; ADR 0040). A run derives from the newest report of the
// checkout's docs/security/audits and no other: the newest report a broken
// one can hide is refused whole - a symbolic link, a directory, a file over
// the cap and a text the core cannot read - and a report the run never reads
// cannot fail it.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// reportNameOf returns the file name of the report of a run.
func reportNameOf(date, id string) string { return date + "-" + id + ".md" }

// writeReport renders and writes one run's report with one public finding of
// its own rule, into dir, and returns its file name.
func writeReport(t *testing.T, dir, date, id, rule string) string {
	t.Helper()

	result := fixtureResult(t, fixtureFinding(rule, "low", "hardening"))
	rendered := renderFixtureReport(t, result, publish.PrivateNone, nil, date, id)
	writeFixtureReport(t, dir, rendered)

	return filepath.Base(rendered.Path)
}

// TestLoadPreviousReadsTheNewestReport pins the one report a run derives from:
// the newest name of the directory by publish.NewestReport, by date and then by
// run ID as a number, and no other file of it.
func TestLoadPreviousReadsTheNewestReport(t *testing.T) {
	t.Parallel()

	type report struct{ date, id string }

	for _, tc := range []struct {
		name string
		// reports are the run's reports the directory holds, and others the
		// files that are none; want is the report the run must read, the zero
		// one for a directory with no report.
		reports []report
		others  []string
		want    report
	}{
		{
			name:    "the latest date",
			reports: []report{{"2026-11-01", "1"}, {"2026-12-01", "1"}, {"2026-11-30", "9"}},
			want:    report{"2026-12-01", "1"},
		},
		{
			name:    "the largest run ID of one date, as a number",
			reports: []report{{"2026-12-01", "9"}, {"2026-12-01", "10"}, {"2026-12-01", "2"}},
			want:    report{"2026-12-01", "10"},
		},
		{
			name:   "no report name at all",
			others: []string{"README.md", "TEMPLATE.md", "x.md", "2026-12-01.md"},
		},
		{name: "no file at all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := fixtureReportsDir(t)

			for _, r := range tc.reports {
				name := writeReport(t, dir, r.date, r.id, "G304")
				if name != reportNameOf(r.date, r.id) {
					t.Fatalf("the report is named %q, want %q", name, reportNameOf(r.date, r.id))
				}
			}

			for _, name := range tc.others {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("not a report\n"), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}

			previous, err := github.LoadPrevious(dir)
			if err != nil {
				t.Fatalf("LoadPrevious(%s) = %v, want no error", dir, err)
			}

			if tc.want == (report{}) {
				if previous != nil {
					t.Errorf("LoadPrevious(%s) = %+v, want nil: no name of the directory is a report", dir, previous)
				}

				return
			}

			if previous == nil {
				t.Fatalf("LoadPrevious(%s) = nil, want the report %s", dir, reportNameOf(tc.want.date, tc.want.id))
			}

			findings := previous.PublicFindings()
			if len(findings) != 1 {
				t.Fatalf("the previous report holds %d public finding(s), want 1: %+v", len(findings), findings)
			}

			if want := "docs/security/audits/" + reportNameOf(tc.want.date, tc.want.id); findings[0].Report != want {
				t.Errorf("LoadPrevious read the report %q, want %q", findings[0].Report, want)
			}
		})
	}
}

// TestLoadPreviousSkipsAnOlderBrokenReport pins that only the newest report is
// opened: a broken report the run never reads cannot fail it.
func TestLoadPreviousSkipsAnOlderBrokenReport(t *testing.T) {
	t.Parallel()

	dir := fixtureReportsDir(t)
	newest := writeReport(t, dir, "2026-12-01", "1", "G304")

	if err := os.WriteFile(filepath.Join(dir, reportNameOf("2026-11-01", "1")), []byte("not a report\n"), 0o600); err != nil {
		t.Fatalf("write the older report: %v", err)
	}

	previous, err := github.LoadPrevious(dir)
	if err != nil {
		t.Fatalf("LoadPrevious = %v, want no error: the older report is never read", err)
	}

	if findings := previous.PublicFindings(); len(findings) != 1 || findings[0].Report != "docs/security/audits/"+newest {
		t.Errorf("LoadPrevious read %+v, want the newest report %s", findings, newest)
	}
}

// TestLoadPreviousRefusesANewestReportItCannotRead pins fail-closed reading:
// the newest report is a symbolic link, no regular file, over the cap or a
// text the core cannot read, and the run stops rather than publish a report
// that forgot the previous one (outcome 7).
func TestLoadPreviousRefusesANewestReportItCannotRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// breakIt makes the newest report one LoadPrevious must refuse, and
		// returns the directory.
		breakIt func(t *testing.T) string
	}{
		{
			name: "a symbolic link to a report",
			breakIt: func(t *testing.T) string {
				t.Helper()

				dir := fixtureReportsDir(t)
				target := writeReport(t, dir, "2026-11-01", "1", "G304")

				if err := os.Symlink(filepath.Join(dir, target), filepath.Join(dir, reportNameOf("2026-12-01", "1"))); err != nil {
					t.Fatalf("link the newest report: %v", err)
				}

				return dir
			},
		},
		{
			name: "a directory named like a report",
			breakIt: func(t *testing.T) string {
				t.Helper()

				dir := fixtureReportsDir(t)
				if err := os.Mkdir(filepath.Join(dir, reportNameOf("2026-12-01", "1")), 0o700); err != nil {
					t.Fatalf("make the directory: %v", err)
				}

				return dir
			},
		},
		{
			name: "a report over the cap",
			breakIt: func(t *testing.T) string {
				t.Helper()

				dir := fixtureReportsDir(t)
				path := filepath.Join(dir, reportNameOf("2026-12-01", "1"))

				f, err := os.Create(path)
				if err != nil {
					t.Fatalf("create %s: %v", path, err)
				}

				if err := f.Truncate(github.MaxPreviousReportBytes + 1); err != nil {
					t.Fatalf("truncate %s: %v", path, err)
				}

				if err := f.Close(); err != nil {
					t.Fatalf("close %s: %v", path, err)
				}

				return dir
			},
		},
		{
			name: "a text the core cannot read",
			breakIt: func(t *testing.T) string {
				t.Helper()

				dir := fixtureReportsDir(t)
				if err := os.WriteFile(filepath.Join(dir, reportNameOf("2026-12-01", "1")), []byte("not a report\n"), 0o600); err != nil {
					t.Fatalf("write the report: %v", err)
				}

				return dir
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := tc.breakIt(t)

			previous, err := github.LoadPrevious(dir)
			if !errors.Is(err, publish.ErrPreviousReport) {
				t.Fatalf("LoadPrevious(%s) = %+v, %v, want an error wrapping ErrPreviousReport", dir, previous, err)
			}

			if previous != nil {
				t.Errorf("LoadPrevious(%s) returned the report %+v beside its error, want nil", dir, previous)
			}

			if code := publish.OutcomeOf(err).Code(); code != 7 {
				t.Errorf("the error's outcome is %d, want 7: an unreadable previous report is its own outcome", code)
			}
		})
	}
}

// writeResultFile writes the fixture result document into dir and returns its
// path: a result file a run may read.
func writeResultFile(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "result.json")
	if err := os.WriteFile(path, fixtureResultJSON(t), 0o600); err != nil {
		t.Fatalf("write the result file %s: %v", path, err)
	}

	return path
}

// TestLoadResultReadsTheModelResult pins the decrypted result's reading: an
// absolute, clean path to one regular file under the cap, parsed with the
// audited commit; a path outside those rules is no configuration, and a file
// outside them is no result.
func TestLoadResultReadsTheModelResult(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// path builds the path LoadResult reads, and want the class the error
		// must wrap, nil for the file a run reads.
		path func(t *testing.T) string
		want error
	}{
		{
			name: "the result file",
			path: func(t *testing.T) string {
				t.Helper()

				return writeResultFile(t, t.TempDir())
			},
		},
		{name: "no path at all", path: func(*testing.T) string { return "" }, want: publish.ErrConfig},
		{name: "a relative path", path: func(*testing.T) string { return "result.json" }, want: publish.ErrConfig},
		{name: "an unclean path", path: func(*testing.T) string { return "/tmp/../result.json" }, want: publish.ErrConfig},
		{
			name: "a symbolic link",
			path: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				target := writeResultFile(t, dir)
				link := filepath.Join(dir, "link.json")

				if err := os.Symlink(target, link); err != nil {
					t.Fatalf("link the result file: %v", err)
				}

				return link
			},
			want: publish.ErrResultInvalid,
		},
		{
			name: "a directory",
			path: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				if err := os.Mkdir(filepath.Join(dir, "result.json"), 0o700); err != nil {
					t.Fatalf("make the directory: %v", err)
				}

				return filepath.Join(dir, "result.json")
			},
			want: publish.ErrResultInvalid,
		},
		{
			name: "a missing file",
			path: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "no-such-result.json")
			},
			want: publish.ErrResultInvalid,
		},
		{
			name: "a file over the cap",
			path: func(t *testing.T) string {
				t.Helper()

				path := filepath.Join(t.TempDir(), "result.json")

				f, err := os.Create(path)
				if err != nil {
					t.Fatalf("create %s: %v", path, err)
				}

				if err := f.Truncate(publish.MaxResultBytes + 1); err != nil {
					t.Fatalf("truncate %s: %v", path, err)
				}

				if err := f.Close(); err != nil {
					t.Fatalf("close %s: %v", path, err)
				}

				return path
			},
			want: publish.ErrResultInvalid,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := tc.path(t)

			result, err := github.LoadResult(path, fixtureCommit)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("LoadResult(%q) = %+v, %v, want an error wrapping %v", path, result, err, tc.want)
				}

				return
			}

			if err != nil {
				t.Fatalf("LoadResult(%q) = %v, want no error", path, err)
			}

			if result.Commit != fixtureCommit || result.ReleaseTag != fixtureTag {
				t.Errorf("LoadResult(%q) read %q of %q, want %q of %q", path, result.Commit, result.ReleaseTag, fixtureCommit, fixtureTag)
			}
		})
	}
}

// TestLoadPreviousRefusesADirItCannotRead pins the directory: it must be an
// absolute, clean, existing directory, and anything else is no configuration.
func TestLoadPreviousRefusesADirItCannotRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{name: "no directory at all", dir: func(*testing.T) string { return "" }},
		{name: "a relative directory", dir: func(*testing.T) string { return "docs/security/audits" }},
		{
			name: "an unclean directory",
			dir:  func(*testing.T) string { return "/tmp/../docs/security/audits" },
		},
		{
			name: "a directory that does not exist",
			dir: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "no-such-dir")
			},
		},
		{
			name: "a file where the directory should be",
			dir: func(t *testing.T) string {
				t.Helper()

				path := filepath.Join(t.TempDir(), "audits")
				if err := os.WriteFile(path, []byte("not a directory\n"), 0o600); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}

				return path
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			previous, err := github.LoadPrevious(tc.dir(t))
			if !errors.Is(err, publish.ErrConfig) {
				t.Fatalf("LoadPrevious(%q) = %+v, %v, want an error wrapping publish.ErrConfig", tc.dir(t), previous, err)
			}

			if previous != nil {
				t.Errorf("LoadPrevious returned the report %+v beside its error, want nil", previous)
			}
		})
	}
}
