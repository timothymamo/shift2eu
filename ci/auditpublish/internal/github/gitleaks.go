package github

import (
	"context"
	"fmt"
)

// GitleaksConfig is how Gitleaks runs gitleaks.
type GitleaksConfig struct {
	// Path is gitleaks' absolute path. It is never looked up on PATH.
	Path string
	// TempDir is an existing absolute directory under which Gitleaks makes
	// gitleaks' fresh HOME, removed by Close.
	TempDir string
}

// Gitleaks scans texts with gitleaks' stdin mode before anything is
// published.
type Gitleaks struct{}

// NewGitleaks returns a Gitleaks, and refuses (publish.ErrConfig) a path that
// is not absolute and a temporary directory that is not an absolute existing
// directory.
func NewGitleaks(_ GitleaksConfig) (*Gitleaks, error) {
	return nil, fmt.Errorf("gitleaks: %w", errNotImplemented)
}

// Scan runs gitleaks stdin --no-banner --no-color --redact --exit-code 10 on
// text, with its own environment, and discards its output unread. Exit 0 is
// clean; exit 10, any other exit and a failure to start wrap
// publish.ErrScanRefused: a scan that did not run fails closed.
func (g *Gitleaks) Scan(_ context.Context, _ string) error {
	return fmt.Errorf("gitleaks scan: %w", errNotImplemented)
}

// Close removes gitleaks' HOME.
func (g *Gitleaks) Close() error {
	return nil
}
