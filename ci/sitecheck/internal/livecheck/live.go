package livecheck

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// LiveVersionConfig is everything LiveVersion depends on: the scheduled
// check's question of which release the live site serves (PRD #297
// milestone 4, ADR 0043). Every field without a stated default is required;
// Validate says which is wrong.
type LiveVersionConfig struct {
	// Host is the canonical host (DefaultHost in production): a plain DNS
	// name, no port, no scheme.
	Host string
	// Tags is the repository's tag list (ReadTagList).
	Tags []string
	// Anchor is the committer date of the commit the highest release tag
	// points to, and Bound the version window's fixed length after it
	// (Window); Bound must be positive.
	Anchor time.Time
	Bound  time.Duration

	// Dial, Roots, Now, MaxBodyBytes and RequestTimeout are Config's fields
	// of the same names, with the same defaults: Now is required, and is
	// both the certificates' clock and the window's.
	Dial           Dialer
	Roots          *x509.CertPool
	Now            func() time.Time
	MaxBodyBytes   int64
	RequestTimeout time.Duration
}

// Validate returns nil when c is a usable configuration, or an error naming
// the first field that is not: Host not a DNS name, Bound not positive, Now
// nil, MaxBodyBytes or RequestTimeout negative. Tags is never a configuration
// error: an empty list refuses the live version instead.
func (c LiveVersionConfig) Validate() error {
	switch {
	case !isDNSName(c.Host):
		return fmt.Errorf("the host %q is not a DNS name", c.Host)
	case c.Bound <= 0:
		return fmt.Errorf("the version window's bound %v is not positive", c.Bound)
	case c.Now == nil:
		return errors.New("no clock (Now)")
	case c.MaxBodyBytes < 0:
		return fmt.Errorf("the body cap %d is negative", c.MaxBodyBytes)
	case c.RequestTimeout < 0:
		return fmt.Errorf("the request timeout %v is negative", c.RequestTimeout)
	}

	return nil
}

// LiveVersion fetches https://<Host>/ once, through the same client as Run
// (no redirect followed, no proxy, Accept-Encoding: gzip, the body capped),
// reads the live version from it as Run's KindVersion check does
// (VersionText, MatchVersion) and judges it with AcceptLive against Tags and
// the Window of Anchor, Bound and Now(). It returns the tag list's own entry.
//
// An invalid c is returned as the error before any connection. No response,
// a status other than 200, an unreadable body, a missing, hidden or
// malformed version, one no tag equals and one the window refuses are
// errors. Text taken from the response reaches an error only through Quote.
//
// Unlike Run, it does not compare the root with a manifest: the scheduled
// check asks which release is live before it fetches that release's
// manifest, and the version text is judged only by its match against the
// tag list, which is what constructs the Tag.
func LiveVersion(ctx context.Context, c LiveVersionConfig) (Tag, error) {
	if err := c.Validate(); err != nil {
		return Tag{}, err
	}

	cfg := Config{
		Host:           c.Host,
		Dial:           c.Dial,
		Roots:          c.Roots,
		Now:            c.Now,
		MaxBodyBytes:   c.MaxBodyBytes,
		RequestTimeout: c.RequestTimeout,
	}.withDefaults()

	checker := newChecker(cfg)
	defer checker.close()

	url := "https://" + cfg.Host + "/"
	root := (&round{checker: checker}).fetch(ctx, url)

	switch {
	case root.err != nil:
		return Tag{}, fmt.Errorf("%s: %w", url, root.err)
	case root.status != http.StatusOK:
		return Tag{}, fmt.Errorf("%s answered %d, want 200", url, root.status)
	case root.bodyErr != nil:
		return Tag{}, fmt.Errorf("%s: the body could not be read: %w", url, root.bodyErr)
	}

	text, err := VersionText(root.body.Data)
	if err != nil {
		return Tag{}, err
	}

	tag, err := MatchVersion(text, c.Tags)
	if err != nil {
		return Tag{}, err
	}

	if err := AcceptLive(tag, c.Tags, Window{Anchor: c.Anchor, Bound: c.Bound, Now: c.Now()}); err != nil {
		return Tag{}, err
	}

	return tag, nil
}
