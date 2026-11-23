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

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// GitleaksConfig is how Gitleaks runs gitleaks.
type GitleaksConfig struct {
	// Path is gitleaks' absolute path. It is never looked up on PATH.
	Path string
	// TempDir is an existing absolute directory under which Gitleaks makes
	// gitleaks' fresh HOME, removed by Close.
	TempDir string
}

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
	path string
	home string
}

// NewGitleaks returns a Gitleaks, and refuses (publish.ErrConfig) a path that
// is not absolute and clean and a temporary directory that is not an absolute
// existing directory. It checks the path's shape only: a file that cannot be
// started is Scan's failure, a refused scan. It makes gitleaks' fresh HOME
// under the temporary directory; Close removes it.
func NewGitleaks(cfg GitleaksConfig) (*Gitleaks, error) {
	if cfg.Path == "" || !filepath.IsAbs(cfg.Path) || filepath.Clean(cfg.Path) != cfg.Path {
		return nil, fmt.Errorf("gitleaks: the path is not absolute: %w", publish.ErrConfig)
	}

	if err := checkTempDir(cfg.TempDir); err != nil {
		return nil, fmt.Errorf("gitleaks: %w", err)
	}

	home, err := os.MkdirTemp(cfg.TempDir, "gitleaks-")
	if err != nil {
		return nil, fmt.Errorf("gitleaks: its home directory not made: %w", publish.ErrConfig)
	}

	return &Gitleaks{path: cfg.Path, home: home}, nil
}

// Scan runs gitleaks stdin --no-banner --no-color --redact --exit-code 10 on
// text, with its own environment, and discards its output unread. Exit 0 is
// clean; exit 10, any other exit and a failure to start wrap
// publish.ErrScanRefused: a scan that did not run fails closed. No error
// carries the text or the tool's output.
func (g *Gitleaks) Scan(ctx context.Context, text string) error {
	if g == nil || g.home == "" {
		return fmt.Errorf("gitleaks scan: no open scanner: %w", publish.ErrScanRefused)
	}

	//nolint:gosec // G204: gitleaks' absolute path from the config; the arguments are fixed
	cmd := exec.CommandContext(ctx, g.path, gitleaksArgs...)
	cmd.Env = []string{"HOME=" + g.home, "NO_COLOR=1"}
	cmd.Dir = g.home
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
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
		return fmt.Errorf("gitleaks: its home directory not removed: %w", publish.ErrConfig)
	}

	g.home = ""

	return nil
}
