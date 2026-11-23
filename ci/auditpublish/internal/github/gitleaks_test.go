package github_test

// Group M of PR 4a: gitleaks (PRD #175 milestone 2, ADR 0040, "The publish
// program's GitHub side"). Every text the program is about to publish is
// scanned by gitleaks, read from stdin, before the first write. A scan that
// did not run fails closed: a leak, any other exit and a binary that cannot
// start are all the core's ErrScanRefused. gitleaks runs by its absolute path
// with an environment of its own, and its output never reaches an error.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

const (
	// gitleaksTextClean is a text gitleaks finds nothing in.
	gitleaksTextClean = "the report's text, with no secret in it\n"

	// gitleaksTextMarker is text only the scanned text holds, so a test can
	// show that no error quotes it.
	gitleaksTextMarker = "SCANNED-TEXT-MARKER"

	// gitleaksTextHit is a text the fake gitleaks reports as a leak: it holds
	// fakeGitleaksHit.
	gitleaksTextHit = gitleaksTextMarker + " " + fakeGitleaksHit

	// gitleaksArgv is what Scan starts gitleaks with: its stdin mode, no
	// banner, no colour, a redacted report and exit code 10 for a leak
	// (ci/auditpublish/internal/github/gitleaks.go; the ADR names --redact for
	// the history scan and leaves the rest to the program). No --config, --log-
	// opts or --report-path entry is among them, so no configuration file of
	// the repository is read and nothing is written beside the scanned text.
	gitleaksArgv = "stdin --no-banner --no-color --redact --exit-code 10"
)

// gitleaksArgs is gitleaksArgv as the argument vector Scan must pass.
func gitleaksArgs() []string {
	return strings.Fields(gitleaksArgv)
}

// gitleaksEnv is the whole environment gitleaks is started with: HOME, the
// fresh directory under the configured temporary directory, and NO_COLOR. The
// value of HOME is filled in by the test that compares it.
func gitleaksEnv(home string) []string {
	return []string{"HOME=" + home, "NO_COLOR=1"}
}

// gitleaksHome returns the HOME the fake gitleaks was started with.
func gitleaksHome(t *testing.T, call GitleaksCall) string {
	t.Helper()

	for _, entry := range call.Env {
		if name, value, ok := strings.Cut(entry, "="); ok && name == "HOME" {
			return value
		}
	}

	t.Fatalf("gitleaks' environment holds no HOME: %v (the library gives it an environment of its own)", call.Env)

	return ""
}

// TestNewGitleaksRefusesAConfigItCannotRunGitleaksWith pins the values
// NewGitleaks refuses as configuration: a gitleaks path or a temporary
// directory that is not absolute, an empty one, and a temporary directory that
// does not exist. Each refusal wraps the core's ErrConfig, which the entry
// point prints as "configuration invalid" (exit 9).
func TestNewGitleaksRefusesAConfigItCannotRunGitleaksWith(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// refuse breaks one value of a config that is otherwise valid.
		refuse func(*github.GitleaksConfig)
	}{
		{name: "a relative gitleaks path", refuse: func(c *github.GitleaksConfig) { c.Path = "bin/gitleaks" }},
		{name: "an empty gitleaks path", refuse: func(c *github.GitleaksConfig) { c.Path = "" }},
		{name: "a relative temporary directory", refuse: func(c *github.GitleaksConfig) { c.TempDir = "tmp" }},
		{name: "an empty temporary directory", refuse: func(c *github.GitleaksConfig) { c.TempDir = "" }},
		{name: "a temporary directory that does not exist", refuse: func(c *github.GitleaksConfig) { c.TempDir = filepath.Join(c.TempDir, "missing") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGitleaks(t)
			cfg := github.GitleaksConfig{Path: fake.Path, TempDir: t.TempDir()}
			tc.refuse(&cfg)

			if _, err := github.NewGitleaks(cfg); !errors.Is(err, publish.ErrConfig) {
				t.Errorf("NewGitleaks with %s = %v, want an error wrapping publish.ErrConfig", tc.name, err)
			}
		})
	}
}

// TestScanRunsGitleaksOnStdinAndFailsClosed pins the verdict of a scan: exit 0
// is clean, a leak (exit 10) is refused, and every other ending - another exit
// code, or a binary that cannot start at all - is refused too. A scan that did
// not run never accepts a text.
func TestScanRunsGitleaksOnStdinAndFailsClosed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// newGitleaks returns the gitleaks a Scan runs, and whether NewGitleaks
		// must accept it.
		newGitleaks func(t *testing.T) *github.Gitleaks
		text        string
		want        error
	}{
		{
			name: "a clean text",
			newGitleaks: func(t *testing.T) *github.Gitleaks {
				t.Helper()

				return newTestGitleaks(t, newFakeGitleaks(t).Path)
			},
			text: gitleaksTextClean,
			want: nil,
		},
		{
			name: "a text the scan reports as a leak",
			newGitleaks: func(t *testing.T) *github.Gitleaks {
				t.Helper()

				return newTestGitleaks(t, newFakeGitleaks(t).Path)
			},
			text: gitleaksTextHit,
			want: publish.ErrScanRefused,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := tc.newGitleaks(t)

			err := g.Scan(t.Context(), tc.text)

			switch {
			case tc.want == nil && err != nil:
				t.Fatalf("Scan of a clean text = %v, want no error: exit 0 is a clean scan", err)
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("Scan = %v, want an error wrapping %v", err, tc.want)
			}
		})
	}

	// Every other ending of gitleaks is a scan that did not run: exit 1 (the
	// CLI's own error), 2 (a usage error) and 126 (not executable), and a
	// binary that cannot be started at all.
	for _, code := range []int{1, 2, 126} {
		t.Run(fmt.Sprintf("an exit code of %d", code), func(t *testing.T) {
			t.Parallel()

			g := newTestGitleaks(t, newFakeGitleaksExit(t, code).Path)

			if err := g.Scan(t.Context(), gitleaksTextClean); !errors.Is(err, publish.ErrScanRefused) {
				t.Errorf("Scan with gitleaks exiting %d = %v, want an error wrapping publish.ErrScanRefused: a scan that did not finish fails closed", code, err)
			}
		})
	}

	t.Run("a binary that cannot start", func(t *testing.T) {
		t.Parallel()

		// NewGitleaks checks the path's shape, not whether it can run: a file
		// that cannot be executed is a start failure, which is a refused scan,
		// never a configuration error.
		path := filepath.Join(t.TempDir(), "gitleaks")
		if err := os.WriteFile(path, []byte("not a program\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}

		g := newTestGitleaks(t, path)

		if err := g.Scan(t.Context(), gitleaksTextClean); !errors.Is(err, publish.ErrScanRefused) {
			t.Errorf("Scan with a gitleaks that cannot start = %v, want an error wrapping publish.ErrScanRefused: a scan that did not run fails closed", err)
		}
	})
}

// TestScanPassesTheTextOnStdinWithItsOwnEnvironmentAndArgs pins what gitleaks
// is given: the whole text on stdin, the argv of the contract, and an
// environment of HOME (its own fresh directory under the temporary directory)
// and NO_COLOR alone. tests-no-process-state refuses t.Setenv in this package,
// so the exact set is the proof that nothing of the test process's environment
// is inherited: a child given its own list cannot hold anything else.
func TestScanPassesTheTextOnStdinWithItsOwnEnvironmentAndArgs(t *testing.T) {
	t.Parallel()

	fake := newFakeGitleaks(t)
	tempDir := t.TempDir()
	g := newTestGitleaksIn(t, fake.Path, tempDir)

	if err := g.Scan(t.Context(), gitleaksTextClean); err != nil {
		t.Fatalf("Scan of a clean text: %v", err)
	}

	calls := fake.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("the fake gitleaks recorded %d calls, want 1", len(calls))
	}

	if calls[0].Stdin != gitleaksTextClean {
		t.Errorf("gitleaks read %q on stdin, want the whole text %q: a text reaches the scan as it is", calls[0].Stdin, gitleaksTextClean)
	}

	if diff := cmp.Diff(gitleaksArgs(), calls[0].Args); diff != "" {
		t.Errorf("gitleaks' arguments (-want +got):\n%s", diff)
	}

	home := gitleaksHome(t, calls[0])
	if !strings.HasPrefix(home, tempDir+string(os.PathSeparator)) {
		t.Errorf("gitleaks' HOME is %q, want a fresh directory under %q", home, tempDir)
	}

	got := slices.Clone(calls[0].Env)
	want := gitleaksEnv(home)
	slices.Sort(got)
	slices.Sort(want)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("gitleaks' environment (-want +got):\n%s", diff)
	}

	// The variables an earlier step could write to $GITHUB_ENV, and the ones a
	// gitleaks run would otherwise take from the process, are not there.
	for _, name := range []string{"XDG_CONFIG_HOME", "GITLEAKS_CONFIG", "GITHUB_TOKEN", "HTTPS_PROXY"} {
		if _, ok := envValue(calls[0].Env, name); ok {
			t.Errorf("gitleaks' environment holds %s, want it not to reach gitleaks", name)
		}
	}
}

// TestScanKeepsTheToolsOutputAndTheTextOutOfItsErrors pins that a refused scan
// names the rule alone: neither the text it read nor what the tool printed
// reaches the error, which the entry point could print.
func TestScanKeepsTheToolsOutputAndTheTextOutOfItsErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// newGitleaks returns the gitleaks a Scan runs.
		newGitleaks func(t *testing.T) *github.Gitleaks
		text        string
	}{
		{
			name: "a leak",
			newGitleaks: func(t *testing.T) *github.Gitleaks {
				t.Helper()

				return newTestGitleaks(t, newFakeGitleaks(t).Path)
			},
			text: gitleaksTextHit,
		},
		{
			name: "an error exit",
			newGitleaks: func(t *testing.T) *github.Gitleaks {
				t.Helper()

				return newTestGitleaks(t, newFakeGitleaksExit(t, 1).Path)
			},
			text: gitleaksTextMarker,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.newGitleaks(t).Scan(t.Context(), tc.text)
			if err == nil {
				t.Fatalf("Scan of a text gitleaks refuses = nil, want an error wrapping publish.ErrScanRefused")
			}

			for _, forbidden := range []string{gitleaksTextMarker, fakeGitleaksHit, fakeOutputMarker} {
				if strings.Contains(err.Error(), forbidden) {
					t.Errorf("the scan's error is %q, want it to hold neither the scanned text nor the tool's output (%q)", err, forbidden)
				}
			}

			if !errors.Is(err, publish.ErrScanRefused) {
				t.Errorf("Scan = %v, want an error wrapping publish.ErrScanRefused", err)
			}
		})
	}
}

// TestScanRefusesAnInlineAllowComment pins the refusal of gitleaks' own inline
// allow list (audit finding 1): gitleaks honours its inline allow comment in the
// text it reads, so a text that carries one, in any letter case, is refused
// before gitleaks runs and never reaches its allow list. A text without the
// marker still runs the scan, once.
func TestScanRefusesAnInlineAllowComment(t *testing.T) {
	t.Parallel()

	// allowComment is gitleaks' inline allow comment, built from its two halves so
	// that no line of this repository spells it out: the scheduled audit's model
	// reads the repository, and a line it quotes refuses the whole run (ADR 0040).
	allowComment := "gitleaks" + ":allow"

	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "as the tool's own spelling", text: gitleaksTextMarker + " " + allowComment + "\n"},
		{name: "in capitals", text: gitleaksTextMarker + " " + strings.ToUpper(allowComment) + "\n"},
		{name: "in mixed case", text: gitleaksTextMarker + " " + "Gitleaks" + ":Allow\n"},
		{name: "as a comment at the end of a line", text: "a line of the report #" + allowComment + "\n"},
		{name: "in the middle of a multi-line text", text: "the report's first line\n" + gitleaksTextMarker + " # " + allowComment + "\nthe report's last line\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGitleaks(t)
			g := newTestGitleaks(t, fake.Path)

			err := g.Scan(t.Context(), tc.text)
			if !errors.Is(err, publish.ErrScanRefused) {
				t.Fatalf("Scan of a text with gitleaks' inline allow comment = %v, want an error wrapping publish.ErrScanRefused: the comment could carry a secret past the scan", err)
			}

			if calls := fake.Calls(t); len(calls) != 0 {
				t.Errorf("the scan ran gitleaks %d time(s), want 0: a text with an inline allow comment is refused before gitleaks runs", len(calls))
			}

			if strings.Contains(strings.ToLower(err.Error()), allowComment) {
				t.Errorf("the refusal is %q, want it to hold neither the scanned text nor the comment's marker", err)
			}

			if strings.Contains(err.Error(), gitleaksTextMarker) {
				t.Errorf("the refusal is %q, want it not to hold the scanned text", err)
			}
		})
	}

	t.Run("a text without the marker still runs gitleaks once", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGitleaks(t)
		g := newTestGitleaks(t, fake.Path)

		if err := g.Scan(t.Context(), gitleaksTextClean); err != nil {
			t.Fatalf("Scan of a clean text without the marker: %v, want no error", err)
		}

		if calls := fake.Calls(t); len(calls) != 1 {
			t.Errorf("the scan ran gitleaks %d time(s), want 1", len(calls))
		}
	})
}

// TestCloseRemovesGitleaksHomeAndNothingElse pins that a scan leaves nothing
// behind: NewGitleaks makes gitleaks' fresh HOME under the temporary directory,
// and Close removes it, leaving the temporary directory itself, which the
// caller owns.
func TestCloseRemovesGitleaksHomeAndNothingElse(t *testing.T) {
	t.Parallel()

	fake := newFakeGitleaks(t)
	tempDir := t.TempDir()

	g, err := github.NewGitleaks(github.GitleaksConfig{Path: fake.Path, TempDir: tempDir})
	if err != nil {
		t.Fatalf("NewGitleaks: %v", err)
	}

	before, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read %s: %v", tempDir, err)
	}

	if len(before) != 1 || !before[0].IsDir() {
		t.Fatalf("NewGitleaks' directory under %s holds %+v, want exactly one directory", tempDir, before)
	}

	if err := g.Scan(t.Context(), gitleaksTextClean); err != nil {
		t.Fatalf("Scan of a clean text: %v", err)
	}

	calls := fake.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("the fake gitleaks recorded %d calls, want 1", len(calls))
	}

	home := gitleaksHome(t, calls[0])

	if _, err := os.Stat(home); err != nil {
		t.Errorf("gitleaks' HOME %q: %v, want the directory the scan ran with", home, err)
	}

	if err := g.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after Close, gitleaks' HOME %q: %v, want it removed", home, err)
	}

	after, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read %s: %v", tempDir, err)
	}

	if len(after) != 0 {
		t.Errorf("after Close, %s holds %+v, want nothing", tempDir, after)
	}
}

// envValue returns the value of name in env.
func envValue(env []string, name string) (string, bool) {
	for _, entry := range env {
		if got, value, ok := strings.Cut(entry, "="); ok && got == name {
			return value, true
		}
	}

	return "", false
}

// newTestGitleaks returns a Gitleaks for path, under a fresh temporary
// directory, and closes it at the end of the test.
func newTestGitleaks(t *testing.T, path string) *github.Gitleaks {
	t.Helper()

	return newTestGitleaksIn(t, path, t.TempDir())
}

// newTestGitleaksIn returns a Gitleaks for path, under tempDir, and closes it
// at the end of the test.
func newTestGitleaksIn(t *testing.T, path, tempDir string) *github.Gitleaks {
	t.Helper()

	g, err := github.NewGitleaks(github.GitleaksConfig{Path: path, TempDir: tempDir})
	if err != nil {
		t.Fatalf("NewGitleaks(%q, %q): %v", path, tempDir, err)
	}

	t.Cleanup(func() {
		if err := g.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	return g
}
