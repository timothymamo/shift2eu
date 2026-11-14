// Pins the loop make deck starts, dev/agents-app-token.sh refresh, where it is fragile: it must survive a long
// sleep of the Mac and a GitHub or a VM that is briefly not there, it must stop on an error only a maintainer
// can fix, and only one refresher per VM instance may run (PRD #161 milestone 1 review items 1 and 7,
// .dot-agent-deck/prd161m1-fixes.md).
package devenv_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// refreshEnv is the environment of a refresh loop in a test: it checks the wall clock every second, and mints
// again only when the token's margin is under a minute, so a loop that minted once does not mint again and a
// test can count the mints it caused.
func (f *mintFixture) refreshEnv() map[string]string {
	f.env["SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS"] = "1"
	f.env["SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS"] = "60"

	return f.env
}

// refreshEagerEnv is the environment of a refresh loop that mints at every check, so a test can make the next
// attempt fail without waiting for a token to get old.
func (f *mintFixture) refreshEagerEnv() map[string]string {
	f.env["SHIFT2EU_AGENTS_TOKEN_CHECK_SECONDS"] = "1"
	f.env["SHIFT2EU_AGENTS_TOKEN_MARGIN_SECONDS"] = "86400"

	return f.env
}

// refreshLockDir is the lock the refresh takes, one per VM instance, so two refreshers of the same VM cannot
// both write its token file (PRD #161 milestone 1 review, item 7).
func (sb *sandbox) refreshLockDir(instance string) string {
	return filepath.Join(sb.stateDir(), "agents-token-refresh-"+instance+".lock")
}

// TestAgentsAppTokenRefreshSurvivesTransientFailures pins the first half of review item 1: a failed mint that
// GitHub or the Mac's network causes (5xx, 429, a dropped connection) says so in one line per failure and
// never stops the loop, even though no valid token is left, so a Mac that slept for hours recovers by itself.
func TestAgentsAppTokenRefreshSurvivesTransientFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		failures int
		fail     func(api *fakeGitHub)
	}{
		{
			name:     "GitHub answers 500",
			failures: 2,
			fail:     func(api *fakeGitHub) { api.failNextMints(http.StatusInternalServerError, 2) },
		},
		{
			name:     "GitHub answers 503",
			failures: 2,
			fail:     func(api *fakeGitHub) { api.failNextMints(http.StatusServiceUnavailable, 2) },
		},
		{
			name:     "GitHub answers 429",
			failures: 2,
			fail:     func(api *fakeGitHub) { api.failNextMints(http.StatusTooManyRequests, 2) },
		},
		{
			name:     "the connection is dropped",
			failures: 2,
			fail:     func(api *fakeGitHub) { api.dropNextMints(2) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()
			tc.fail(f.api)

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			// One line per failure: the loop is invisible in make deck otherwise, and a maintainer reading
			// the log cannot tell a transient failure from a stopped refresher.
			waitFor(t, 30*time.Second, "one line on stderr for the start and for each transient failure", func() bool {
				return nonEmptyLines(refresh.errOut()) >= tc.failures+1
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited after %s, although a transient failure must never stop it; stderr:\n%s", tc.name, refresh.errOut())
			}

			waitFor(t, 30*time.Second, "the refresh to write a token once GitHub answers again", func() bool {
				line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

				return ok && line != ""
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited after it recovered from %s; it must keep running until a signal or a permanent error; stderr:\n%s", tc.name, refresh.errOut())
			}

			token := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)
			if token[0] != f.api.minted()[len(f.api.minted())-1] {
				t.Errorf("%s does not hold the token the fake API handed out after %s, so the VM still has no token", agentsTokenFile, tc.name)
			}

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
}

// TestAgentsAppTokenRefreshSurvivesAVMThatIsNotRunning pins the other transient failure of review item 1:
// while limactl fails, or the VM is not running at all, the loop keeps minting and retrying, and it writes the
// token as soon as the VM is back.
func TestAgentsAppTokenRefreshSurvivesAVMThatIsNotRunning(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	marker := filepath.Join(f.sb.root, "vm-down")
	f.sb.useFailingLimactl(t, marker)
	env["SHIFT2EU_TEST_LIMA_FAIL_WHILE"] = marker
	env["SHIFT2EU_TEST_REAL_LIMACTL"] = filepath.Join(f.sb.binDir, "limactl-real")

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	// Two mints with a VM that is not running: the first attempt failed to write, and the loop tried again
	// instead of stopping. mint_once mints before it writes, so a second token proves the retry.
	waitFor(t, 30*time.Second, "the refresh to try a second mint while the VM is not running", func() bool {
		return len(f.api.minted()) >= 2
	})

	if !refresh.running() {
		t.Fatalf("the refresh exited because limactl failed, although a VM that is not running is transient; stderr:\n%s", refresh.errOut())
	}

	if _, err := os.Stat(f.sb.vmPath(agentsTokenFile)); err == nil {
		t.Fatalf("%s is there although every limactl call failed", agentsTokenFile)
	}

	// The VM comes back: the next check writes the token without anyone restarting the refresh.
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove %s: %v", marker, err)
	}

	waitFor(t, 30*time.Second, "the refresh to write the token once the VM is running again", func() bool {
		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return ok && line != ""
	})

	if !refresh.running() {
		t.Fatalf("the refresh exited after the VM came back; stderr:\n%s", refresh.errOut())
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshStopsOnAPermanentFailure pins the second half of review item 1: only an error a
// maintainer has to fix stops the loop, with the stop line and status 1, even while the VM's token would still
// be valid for a while.
func TestAgentsAppTokenRefreshStopsOnAPermanentFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		status    int
		afterMint bool // refuse the mint only once one has worked
	}{
		{name: "401 at the first mint", status: http.StatusUnauthorized},
		{name: "403 at the first mint", status: http.StatusForbidden},
		{name: "404 at the first mint", status: http.StatusNotFound},
		{name: "401 after a mint that worked", status: http.StatusUnauthorized, afterMint: true},
		{name: "403 after a mint that worked", status: http.StatusForbidden, afterMint: true},
		{name: "404 after a mint that worked", status: http.StatusNotFound, afterMint: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEagerEnv()

			if !tc.afterMint {
				f.api.refuseMintsWith(tc.status)
			}

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			if tc.afterMint {
				waitFor(t, 30*time.Second, "the first mint to work", func() bool {
					line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

					return ok && line != ""
				})

				f.api.refuseMintsWith(tc.status)
			}

			if !refresh.waitExit(30 * time.Second) {
				t.Fatalf("the refresh was still running 30s after GitHub answered HTTP %d, which no retry can fix; stderr:\n%s",
					tc.status, refresh.errOut())
			}

			if code := refresh.exitCode(t); code != 1 {
				t.Errorf("the refresh exited with %d after HTTP %d, want 1: make deck, and the maintainer reading its log, relies on a stopped refresh being a failure", code, tc.status)
			}

			stopped := refresh.errOut()
			if !strings.Contains(stopped, "agents App token refresh stopped") {
				t.Errorf("the refresh's stderr does not say 'agents App token refresh stopped' after HTTP %d: %s", tc.status, stopped)
			}

			if !strings.Contains(stopped, "make agents-token-refresh") {
				t.Errorf("the refresh's stderr does not name make agents-token-refresh, the command that starts it again: %s", stopped)
			}

			f.assertNoLeaks(t, refresh.out(), stopped)
		})
	}
}

// TestAgentsAppTokenRefreshHoldsOneLockPerVM pins the first half of review item 7: the refresh takes a lock
// named after the VM instance, a second refresher of the same VM exits at once without minting anything, and
// the lock goes when the first one does, so make deck cannot stack two loops on one token file.
func TestAgentsAppTokenRefreshHoldsOneLockPerVM(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	first := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	waitFor(t, 30*time.Second, "the first refresh to write a token", func() bool {
		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return ok && line != ""
	})

	lock := f.sb.refreshLockDir("shift2eu")

	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("the refresh holds no lock at %s, so nothing stops a second refresher of the same VM from writing the same token file: %v", lock, err)
	}

	mints := len(f.api.minted())

	second := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	if !second.waitExit(30 * time.Second) {
		t.Fatalf("a second refresh of the same VM kept running; stderr:\n%s", second.errOut())
	}

	// The status is the coder's choice (0 or non-zero, documented); what it must not do is mint or leave the
	// first loop alone.
	t.Logf("a second refresher of the same VM exited with %d", second.exitCode(t))

	if got := len(f.api.minted()); got != mints {
		t.Errorf("the second refresh minted %d token(s): a second refresher of the same VM must exit before it mints", got-mints)
	}

	if !first.running() {
		t.Fatalf("the first refresh exited because a second one started; stderr:\n%s", first.errOut())
	}

	if strings.TrimSpace(second.errOut()) == "" {
		t.Errorf("a second refresh exits without a word, so a maintainer cannot tell why nothing was minted")
	}

	assertNoSecrets(t, "the second refresh's stderr", second.errOut(), f.secrets())
	assertNoSecrets(t, "the second refresh's stdout", second.out(), f.secrets())

	first.signal(t, syscall.SIGTERM)

	if !first.waitExit(30 * time.Second) {
		t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", first.errOut())
	}

	if _, err := os.Stat(lock); err == nil {
		t.Errorf("the lock %s outlived the refresh: its exit trap removes it, or the next refresh never starts", lock)
	}
}

// TestAgentsAppTokenRefreshTakesOverAStaleLock pins the second half of review item 7: a refresher that was
// killed without running its trap leaves the lock behind, and the next one takes it over instead of refusing to
// start, so make deck recovers from a killed refresher by itself.
func TestAgentsAppTokenRefreshTakesOverAStaleLock(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	first := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	waitFor(t, 30*time.Second, "the first refresh to write a token", func() bool {
		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return ok && line != ""
	})

	lock := f.sb.refreshLockDir("shift2eu")

	// SIGKILL runs no trap, so the lock stays behind exactly as a crashed refresher leaves it.
	first.kill(t)

	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("the lock %s is gone after a SIGKILL, so this test cannot create the stale lock it pins: %v", lock, err)
	}

	mints := len(f.api.minted())

	second := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	waitFor(t, 30*time.Second, "the next refresh to take over the stale lock and mint", func() bool {
		return len(f.api.minted()) > mints
	})

	if !second.running() {
		t.Fatalf("the next refresh exited instead of taking over a stale lock whose pid is dead; stderr:\n%s", second.errOut())
	}

	second.signal(t, syscall.SIGTERM)

	if !second.waitExit(30 * time.Second) {
		t.Fatalf("the refresh that took over the stale lock did not stop on SIGTERM; stderr:\n%s", second.errOut())
	}

	if _, err := os.Stat(lock); err == nil {
		t.Errorf("the lock %s outlived the refresh that took it over", lock)
	}

	f.assertNoLeaks(t, second.out(), second.errOut())
}
