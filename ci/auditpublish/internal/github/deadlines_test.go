package github_test

// Group P of PR 4a: the deadlines of the GitHub side (audit finding 5, review
// finding 6, ADR 0040, "The publish program's GitHub side"). Every gh call and
// every gitleaks scan is bounded: an outside service that never answers must
// not hold a run, a call that did not run is a failure and never a status the
// caller could read as "absent", and only a positive deadline is a
// configuration. The two other groups keep their own files; this one holds what
// only a deadline can observe, and the fakes' Delay and sleep are its.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestNewClientAndNewGitleaksRefuseANegativeTimeout pins the deadline a caller
// may set: zero is the default, and a negative one is no configuration, which
// the entry point prints as "configuration invalid" (exit 9).
func TestNewClientAndNewGitleaksRefuseANegativeTimeout(t *testing.T) {
	t.Parallel()

	t.Run("a GitHub call's negative timeout", func(t *testing.T) {
		t.Parallel()

		cfg := github.ClientConfig{
			GHPath:      newFakeGH(t).Path,
			Token:       github.NewToken(fixtureToken(t)),
			TempDir:     t.TempDir(),
			Scope:       testScope(),
			CallTimeout: -time.Second,
		}

		if _, err := github.NewClient(cfg); !errors.Is(err, publish.ErrConfig) {
			t.Errorf("NewClient with a negative CallTimeout = %v, want an error wrapping publish.ErrConfig", err)
		}
	})

	t.Run("a scan's negative timeout", func(t *testing.T) {
		t.Parallel()

		cfg := github.GitleaksConfig{Path: newFakeGitleaks(t).Path, TempDir: t.TempDir(), Timeout: -time.Second}

		if _, err := github.NewGitleaks(cfg); !errors.Is(err, publish.ErrConfig) {
			t.Errorf("NewGitleaks with a negative Timeout = %v, want an error wrapping publish.ErrConfig", err)
		}
	})
}

// TestCallRunsUnderItsDeadline pins that a call is bounded and that a context
// that is already done is no answer: a gh that outlives the configured timeout
// is a failed call (exit 8), returned at the deadline rather than at the tool's,
// and a cancelled call never comes back as a status the caller could read as
// "absent" - only a gh that ran can say 404.
func TestCallRunsUnderItsDeadline(t *testing.T) {
	t.Parallel()

	const (
		callTimeout = 200 * time.Millisecond
		routeDelay  = 10 * time.Second
		// slack is how long the call may take and still be well under the
		// route's delay: generous, so the test does not depend on exact timing.
		slack = 5 * time.Second
	)

	t.Run("a route past the call's deadline", func(t *testing.T) {
		t.Parallel()

		fake := newFakeGH(t, Route{
			Method:   "GET",
			Endpoint: fixtureReleaseEndpoint,
			Status:   200,
			Body:     map[string]string{"tag_name": fixtureTag},
			Delay:    routeDelay,
		})
		client := newTestClient(t, github.ClientConfig{
			GHPath:      fake.Path,
			Token:       github.NewToken(fixtureToken(t)),
			TempDir:     t.TempDir(),
			Scope:       testScope(),
			CallTimeout: callTimeout,
		})

		started := time.Now()

		resp, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint})

		elapsed := time.Since(started)

		if !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("Call of a gh that outlives its deadline = %v, want an error wrapping publish.ErrGitHub", err)
		}

		if diff := cmp.Diff(github.Response{}, resp); diff != "" {
			t.Errorf("Call of a gh that outlives its deadline returned a Response (-want +got):\n%s", diff)
		}

		if elapsed >= slack {
			t.Errorf("Call took %s, want well under the fake's %s delay: every call runs under its timeout", elapsed, routeDelay)
		}
	})

	t.Run("a context that is already cancelled", func(t *testing.T) {
		t.Parallel()

		// The fake would answer 404, which the caller reads as "absent".
		fake := newFakeGH(t)
		client := newFixtureClient(t, fake.Path)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		resp, err := client.Call(ctx, github.Request{Method: "GET", Endpoint: fixtureRepoPrefix + "/git/ref/heads/" + fixtureBranch})

		if !errors.Is(err, publish.ErrGitHub) {
			t.Fatalf("Call with a cancelled context = %v, want an error wrapping publish.ErrGitHub: a call that did not run is no answer", err)
		}

		if resp.Status != 0 {
			t.Errorf("Call with a cancelled context returned the status %d, want no status at all: only a gh that ran can say 404", resp.Status)
		}

		if calls := fake.Calls(t); len(calls) != 0 {
			t.Errorf("the cancelled call ran gh %d time(s), want 0", len(calls))
		}
	})
}

// TestScanRunsUnderItsDeadline pins that a scan is bounded: a gitleaks that
// outlives the configured timeout is a scan that did not finish, which fails
// closed like any other, and the caller is told at its deadline instead of the
// tool's.
func TestScanRunsUnderItsDeadline(t *testing.T) {
	t.Parallel()

	const (
		timeout = 200 * time.Millisecond
		delay   = 10 * time.Second
		// slack is how long the scan may take and still be well under the
		// fake's delay: generous, so the test does not depend on exact timing.
		slack = 5 * time.Second
	)

	fake := newFakeGitleaksSleep(t, delay)

	g, err := github.NewGitleaks(github.GitleaksConfig{Path: fake.Path, TempDir: t.TempDir(), Timeout: timeout})
	if err != nil {
		t.Fatalf("NewGitleaks: %v", err)
	}

	t.Cleanup(func() {
		if err := g.Close(); err != nil {
			t.Errorf("Close(): %v", err)
		}
	})

	started := time.Now()

	err = g.Scan(t.Context(), gitleaksTextClean)

	elapsed := time.Since(started)

	if !errors.Is(err, publish.ErrScanRefused) {
		t.Fatalf("Scan of a gitleaks that outlives its timeout = %v, want an error wrapping publish.ErrScanRefused: a scan that did not finish fails closed", err)
	}

	if elapsed >= slack {
		t.Errorf("Scan took %s, want well under the fake's %s delay: every scan runs under its timeout", elapsed, delay)
	}
}
