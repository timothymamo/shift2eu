package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// GitleaksConfig is how Gitleaks runs gitleaks.
type GitleaksConfig struct {
	// Path is gitleaks' absolute path. It is never looked up on PATH.
	Path string
	// TempDir is an existing absolute directory under which Gitleaks makes
	// gitleaks' fresh HOME, removed by Close.
	TempDir string
	// Timeout bounds each scan; zero is defaultTimeout, a negative one is no
	// configuration.
	Timeout time.Duration
}

// defaultTimeout is a call's and a scan's deadline when the configuration sets
// none (ADR 0040, "Deadlines and signals").
const defaultTimeout = 2 * time.Minute

// waitDelay bounds how long a killed gh or gitleaks may keep its output pipes
// open after its context ended.
const waitDelay = time.Second

// timeoutOf returns a configured timeout, zero being defaultTimeout, and
// refuses (publish.ErrConfig) a negative one.
func timeoutOf(d time.Duration) (time.Duration, error) {
	switch {
	case d < 0:
		return 0, refuse(publish.CheckTimeout, "a negative timeout", publish.ErrConfig)
	case d == 0:
		return defaultTimeout, nil
	default:
		return d, nil
	}
}

// allowMarker is gitleaks' inline allow comment, which it honours in the text
// it reads; a text holding it, in any case, is refused before gitleaks runs.
// It is built from two parts so that no line of this repository spells the
// marker out: a model reading the source would otherwise quote that line, and
// the publish program would then refuse its whole run.
const allowMarker = "gitleaks" + ":allow"

// gitleaksArgs is what every scan starts gitleaks with: its stdin mode, no
// banner, no colour, a redacted report and exit code 10 for a leak. No
// --config, --log-opts or --report-path is among them, so no configuration
// file of the repository is read and nothing is written.
var gitleaksArgs = []string{"stdin", "--no-banner", "--no-color", "--redact", "--exit-code", "10"}

// gitleaksLeakExit is the exit code gitleaksArgs asks for on a leak.
const gitleaksLeakExit = 10

// Gitleaks scans texts with gitleaks' stdin mode before anything is
// published.
type Gitleaks struct {
	path    string
	home    string
	timeout time.Duration
}

// NewGitleaks returns a Gitleaks, and refuses (publish.ErrConfig) a path that
// is not absolute and clean, a temporary directory that is not an absolute
// existing directory and a negative timeout. It checks the path's shape only: a file that cannot be
// started is Scan's failure, a refused scan. It makes gitleaks' fresh HOME
// under the temporary directory; Close removes it.
func NewGitleaks(cfg GitleaksConfig) (*Gitleaks, error) {
	if cfg.Path == "" || !filepath.IsAbs(cfg.Path) || filepath.Clean(cfg.Path) != cfg.Path {
		return nil, refuse(publish.CheckGitleaksPath, "gitleaks: the path is not absolute", publish.ErrConfig)
	}

	if err := checkTempDir(cfg.TempDir); err != nil {
		return nil, fmt.Errorf("gitleaks: %w", err)
	}

	timeout, err := timeoutOf(cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("gitleaks: %w", err)
	}

	home, err := os.MkdirTemp(cfg.TempDir, "gitleaks-")
	if err != nil {
		return nil, refuse(publish.CheckGitleaksHome, "gitleaks: its home directory not made", publish.ErrConfig)
	}

	return &Gitleaks{path: cfg.Path, home: home, timeout: timeout}, nil
}

// Scan runs gitleaks stdin --no-banner --no-color --redact --exit-code 10 on
// text, with its own environment and under the configured timeout, and
// discards its output unread. Exit 0 is clean; exit 10, any other exit, a
// failure to start and a scan whose context ended wrap publish.ErrScanRefused:
// a scan that did not run fails closed. A text holding gitleaks' inline allow
// comment, in any case, is refused before gitleaks runs. No error carries the
// text or the tool's output.
func (g *Gitleaks) Scan(ctx context.Context, text string) error {
	if g == nil || g.home == "" {
		return fmt.Errorf("gitleaks scan: no open scanner: %w", publish.ErrScanRefused)
	}

	if strings.Contains(strings.ToLower(text), allowMarker) {
		return fmt.Errorf("gitleaks scan: the text holds gitleaks' inline allow comment: %w", publish.ErrScanRefused)
	}

	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	//nolint:gosec // G204: gitleaks' absolute path from the config; the arguments are fixed
	cmd := exec.CommandContext(ctx, g.path, gitleaksArgs...)
	cmd.Env = []string{"HOME=" + g.home, "NO_COLOR=1"}
	cmd.Dir = g.home
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = waitDelay

	err := cmd.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("gitleaks scan: the scan's context ended: %w", publish.ErrScanRefused)
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == gitleaksLeakExit {
			return fmt.Errorf("gitleaks scan: a leak found: %w", publish.ErrScanRefused)
		}

		return fmt.Errorf("gitleaks scan: the scan did not run to its end: %w", publish.ErrScanRefused)
	}

	return nil
}

// Close removes gitleaks' HOME, and only it. It may be called more than once.
func (g *Gitleaks) Close() error {
	if g == nil || g.home == "" {
		return nil
	}

	if err := os.RemoveAll(g.home); err != nil {
		return refuse(publish.CheckGitleaksHomeRemoved, "gitleaks: its home directory not removed", publish.ErrConfig)
	}

	g.home = ""

	return nil
}
