// Pins the loop make deck starts, dev/agents-app-token.sh refresh, where it is fragile: it must survive a long
// sleep of the Mac and a GitHub or a VM that is briefly not there, it must stop on an error only a maintainer
// can fix, and only one refresher per VM instance may run (PRD #161 milestone 1 review items 1 and 7,
// .dot-agent-deck/prd161m1-fixes.md).
package devenv_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

// assertStopLineNamesWhereToRestart fails unless text holds the refresh's stop line and names both the host
// clone and make agents-token-refresh-start: the command a maintainer runs again, and the only place it may run
// from, where the App's key and its path live (PRD #161 milestone 1 third review, item 4).
func assertStopLineNamesWhereToRestart(t *testing.T, text string) {
	t.Helper()

	for _, want := range []string{"agents App token refresh stopped", "make agents-token-refresh-start", "host clone"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refresh's stderr does not hold %q, so a maintainer cannot tell where to start it again after it stopped (third review, item 4): %s", want, text)
		}
	}
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
		// A rate limit is a 403 too, and GitHub says so in the body or in the headers: it is transient, not the
		// permanent refusal of the App the 403 branch used to take it for (second review, item 4).
		{
			name:     "GitHub answers 403 with a rate limit message",
			failures: 2,
			fail: func(api *fakeGitHub) {
				api.failNextMintsWith(http.StatusForbidden,
					`{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
					nil, 2)
			},
		},
		{
			name:     "GitHub answers 403 with no rate limit left",
			failures: 2,
			fail: func(api *fakeGitHub) {
				api.failNextMintsWith(http.StatusForbidden, `{"message":"Forbidden"}`,
					map[string]string{"X-RateLimit-Remaining": "0"}, 2)
			},
		},
		{
			name:     "GitHub answers 403 with a retry-after",
			failures: 2,
			fail: func(api *fakeGitHub) {
				api.failNextMintsWith(http.StatusForbidden, `{"message":"Forbidden"}`,
					map[string]string{"Retry-After": "1"}, 2)
			},
		},
		{
			name:     "the bot user lookup is rate limited",
			failures: 2,
			fail: func(api *fakeGitHub) {
				api.failNextUserLookupsWith(http.StatusForbidden,
					`{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
					nil, 2)
			},
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

// TestAgentsAppTokenRefreshWaitsOutARateLimit pins the third review's item 2 (S1), which replaces the five-check
// cap of the second review: a rate limit's wait is honoured up to an hour. A refresh that still capped it would
// retry about two seconds before this test allows, because five checks of one second are five seconds.
func TestAgentsAppTokenRefreshWaitsOutARateLimit(t *testing.T) {
	t.Parallel()

	// The wait GitHub asks for is seven seconds and the check is one second (refreshEnv), so the second POST
	// belongs about seven seconds after the first. Six seconds is the bound a retry cut down to the old cap of
	// five checks misses, and a bound a load spike or -race cannot push past: sleep only ever lasts longer.
	const (
		minWait = 6 * time.Second
		wait    = 7 * time.Second
	)

	cases := []struct {
		name string
		fail func(api *fakeGitHub)
	}{
		{
			name: "a retry-after longer than five checks",
			fail: func(api *fakeGitHub) { api.failNextMintsWithRateLimit(7, 0, 1) },
		},
		{
			name: "no request left until the reset",
			fail: func(api *fakeGitHub) { api.failNextMintsWithRateLimit(0, time.Now().Add(wait).Unix(), 1) },
		},
		{
			// Never earlier than either: the retry-after asks for two seconds, the reset lies seven away.
			name: "both, and the reset is the later one",
			fail: func(api *fakeGitHub) { api.failNextMintsWithRateLimit(2, time.Now().Add(wait).Unix(), 1) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()
			tc.fail(f.api)

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			// One line for the start and one for the rate limit, then nothing until the wait is over.
			waitFor(t, 30*time.Second, "one line on stderr for the start and for the rate limit", func() bool {
				return nonEmptyLines(refresh.errOut()) >= 2
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited on a 403 that says it is a rate limit, which it must survive; stderr:\n%s", refresh.errOut())
			}

			// The wait is generous against -race; what it must not do is come back early.
			waitFor(t, 90*time.Second, "the refresh's second access_tokens request after the rate limit", func() bool {
				return len(f.api.postTimes()) >= 2
			})

			posts := f.api.postTimes()

			if waited := posts[1].Sub(posts[0]); waited < minWait {
				t.Errorf("the refresh asked GitHub for a token again %s after the rate-limited request, want at least %s: a rate limit's retry-after and its x-ratelimit-reset are honoured up to 3600s and never cut down to a few checks (third review, item 2)\nstderr:\n%s",
					waited.Round(time.Millisecond), minWait, refresh.errOut())
			}

			waitFor(t, 30*time.Second, "the refresh to write the token GitHub minted after the wait", func() bool {
				line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

				return ok && line != ""
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited after it recovered from the rate limit; stderr:\n%s", refresh.errOut())
			}

			token := readTwoLines(t, f.sb.vmPath(agentsTokenFile), 0o600)
			if tokens := f.api.minted(); token[0] != tokens[len(tokens)-1] {
				t.Errorf("%s does not hold the token the fake API minted after the wait", agentsTokenFile)
			}

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
}

// TestAgentsAppTokenRefreshSurvivesAZeroPaddedRateLimitReset pins the reviewer's N-a and the auditor's L-A: a
// zero-padded x-ratelimit-reset is a number like any other, so a rate limit whose header is padded must not end
// the loop. The shell reads a leading 0 as octal and dies on a digit 8 or 9 ("Illegal number" in dash, "value
// too great for base" in bash), which is what such a header used to do: no stop line, no refresher, and a stale
// pid for make agents-token-refresh-start.
func TestAgentsAppTokenRefreshSurvivesAZeroPaddedRateLimitReset(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		reset string
	}{
		{
			// Nothing to wait for: a padded reset in the past, which the loop must read as the number it is.
			name:  "a padded reset in the past",
			reset: "0000000009",
		},
		{
			// A padded reset a few seconds ahead: the digits still say when the rate limit ends.
			name:  "a padded reset a few seconds ahead",
			reset: zeroPadWithAnEightOrNine(time.Now().Add(3 * time.Second).Unix()),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()

			f.api.failNextMintsWith(http.StatusForbidden,
				`{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
				map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": tc.reset}, 1)

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			// The loop either mints again or ends, so a crash fails the test at once instead of at the timeout.
			waitFor(t, 60*time.Second, "the refresh to mint again after a zero-padded X-RateLimit-Reset", func() bool {
				return len(f.api.postTimes()) >= 2 || !refresh.running()
			})

			if !refresh.running() {
				t.Fatalf("the refresh exited on a rate limit whose X-RateLimit-Reset was zero-padded (%s), although a rate limit is transient and must never stop it: the shell reads the leading 0 as octal, and a digit 8 or 9 then ends it without a stop line (reviewer N-a, auditor L-A)\nstderr:\n%s",
					tc.reset, refresh.errOut())
			}

			if strings.Contains(refresh.errOut(), "agents App token refresh stopped") {
				t.Errorf("the refresh said it stopped after a zero-padded X-RateLimit-Reset (%s): the padding says nothing about the rate limit, so the loop keeps running\nstderr:\n%s",
					tc.reset, refresh.errOut())
			}

			waitFor(t, 30*time.Second, "the refresh to write the token of the mint that followed the rate limit", func() bool {
				token, ok := tokenLine(f.sb.vmPath(agentsTokenFile))
				tokens := f.api.minted()

				return ok && token != "" && len(tokens) > 0 && token == tokens[len(tokens)-1]
			})

			if got := len(f.api.postTimes()); got < 2 {
				t.Errorf("the refresh asked GitHub for a token %d time(s), want at least two: the first was rate limited by a padded header, and the second is the loop still minting", got)
			}

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
}

// TestAgentsAppTokenRefreshSaysItWaitsAnHourAtMost pins the reviewer's N-b and the third review's item 2 from
// the side a test can see: neither a wait of 99999999 seconds nor a reset a month away can be waited out, so
// the loop must say what it will do - try again in 3600s, the cap - and then keep running, without asking
// GitHub for a token before the hour is over.
func TestAgentsAppTokenRefreshSaysItWaitsAnHourAtMost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		fail func(api *fakeGitHub)
	}{
		{
			name: "a retry-after of 99999999 seconds",
			fail: func(api *fakeGitHub) {
				api.failNextMintsWith(http.StatusForbidden, "", map[string]string{"Retry-After": "99999999"}, 1)
			},
		},
		{
			name: "an X-RateLimit-Reset a month away",
			fail: func(api *fakeGitHub) {
				api.failNextMintsWithRateLimit(0, time.Now().Add(30*24*time.Hour).Unix(), 1)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()
			tc.fail(f.api)

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			waitFor(t, 30*time.Second, "the refresh to say how long it will wait before trying again", func() bool {
				return waitLine(refresh.errOut()) != ""
			})

			line := waitLine(refresh.errOut())
			if !strings.Contains(line, "3600s") {
				t.Errorf("the refresh says %q after %s, want a wait of 3600s: a retry-after and an x-ratelimit-reset are honoured up to an hour, and neither can be outlasted here (third review, item 2)\nstderr:\n%s",
					line, tc.name, refresh.errOut())
			}

			if !refresh.running() {
				t.Fatalf("the refresh exited instead of waiting out %s for an hour; stderr:\n%s", tc.name, refresh.errOut())
			}

			// The wait it announced is the wait it takes: no second request while the hour is still ahead.
			if got := len(f.api.postTimes()); got != 1 {
				t.Errorf("the refresh asked GitHub for a token %d time(s) while it said it waits 3600s, want one: a rate limit is never retried before its wait is over", got)
			}

			refresh.signal(t, syscall.SIGTERM)

			if !refresh.waitExit(30 * time.Second) {
				t.Fatalf("the refresh did not stop on SIGTERM after waiting out %s; stderr:\n%s", tc.name, refresh.errOut())
			}

			assertStopLineNamesWhereToRestart(t, refresh.errOut())

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
}

// waitLine is the refresh's line that says how long it will wait before it tries again, and "" until it has
// written one.
func waitLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "will try again in") {
			return line
		}
	}

	return ""
}

// TestAgentsAppTokenRefreshKeepsTheTokenWhenTheLookupFails pins the third review's item 3 (L-2, N1): a minted
// token is written into the VM as soon as GitHub hands it out, before the bot user is looked up, so a lookup that
// fails costs the agents their identity for one check, never the token. The next check looks the bot user up
// again without minting a second token.
func TestAgentsAppTokenRefreshKeepsTheTokenWhenTheLookupFails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		fail func(api *fakeGitHub)
	}{
		{
			name: "the lookup answers 503",
			fail: func(api *fakeGitHub) { api.failNextUserLookups(http.StatusServiceUnavailable, 1) },
		},
		{
			name: "the lookup is rate limited",
			fail: func(api *fakeGitHub) {
				api.failNextUserLookupsWith(http.StatusForbidden,
					`{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
					nil, 1)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			// The margin is under the hour a token lives, so one mint is enough for the whole test: a second
			// access_tokens request can only come from the retry of the lookup, which is what this test forbids.
			env := f.refreshEnv()
			tc.fail(f.api)

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			// The failure is logged: at that moment the token belongs in the VM already, and the identity - the
			// fruit of the lookup that failed - must not be there yet.
			waitFor(t, 30*time.Second, "the refresh to log the failed bot user lookup", func() bool {
				return nonEmptyLines(refresh.errOut()) >= 2
			})

			tokens := f.api.minted()
			if len(tokens) == 0 {
				t.Fatalf("the fake API minted no token, so this test cannot tell whether a minted token reaches the VM; stderr:\n%s", refresh.errOut())
			}

			line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))
			if !ok || line != tokens[0] {
				t.Fatalf("%s does not hold the token the fake API minted before the bot user lookup failed: a failed lookup must never discard a minted token, so the token is written as soon as it is minted (third review, item 3)\nstderr:\n%s",
					agentsTokenFile, refresh.errOut())
			}

			if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
				t.Errorf("%s is there although the bot user lookup failed: the identity file is written from a lookup that succeeded, which is the next check's", agentsIdentityFile)
			}

			waitFor(t, 30*time.Second, "the refresh to look the bot user up again at its next check", func() bool {
				_, err := os.Stat(f.sb.vmPath(agentsIdentityFile))

				return err == nil
			})

			if got := len(f.api.posted()); got != 1 {
				t.Errorf("the refresh posted to access_tokens %d times, want one: retrying the lookup must not mint a second token while the one in the VM is still valid (third review, item 3)", got)
			}

			if got := f.api.botUserLookups(); got != 1 {
				t.Errorf("the fake API answered %d bot user lookups with the bot user, want one: one lookup failed, the next one worked", got)
			}

			if refreshed, ok := tokenLine(f.sb.vmPath(agentsTokenFile)); !ok || refreshed != tokens[0] {
				t.Errorf("%s no longer holds the token the failed lookup must have kept", agentsTokenFile)
			}

			identity := readTwoLines(t, f.sb.vmPath(agentsIdentityFile), 0o600)
			if want := f.api.slug + "[bot]"; identity[0] != want {
				t.Errorf("%s's first line is %q, want the App's bot user.name %q", agentsIdentityFile, identity[0], want)
			}

			if !refresh.running() {
				t.Fatalf("the refresh exited after a failed bot user lookup and its retry; it must keep running; stderr:\n%s", refresh.errOut())
			}

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
}

// TestAgentsAppTokenRefreshStopsOnAPermanentLookupFailure pins the other half of the third review's item 3: a
// lookup error no retry fixes (4xx other than a rate limit) still stops the refresh with the stop line and
// status 1, and the token the mint already wrote stays in the VM for the agents to use meanwhile.
func TestAgentsAppTokenRefreshStopsOnAPermanentLookupFailure(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEagerEnv()

	f.api.refuseUserLookupsWith(http.StatusNotFound)

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh was still running 30s after the bot user lookup answered 404, which no retry can fix; stderr:\n%s", refresh.errOut())
	}

	if code := refresh.exitCode(t); code != 1 {
		t.Errorf("the refresh exited with %d after a bot user lookup GitHub answered 404, want 1: make deck, and the maintainer reading its log, relies on a stopped refresh being a failure", code)
	}

	assertStopLineNamesWhereToRestart(t, refresh.errOut())

	tokens := f.api.minted()
	if len(tokens) == 0 {
		t.Fatalf("the fake API minted no token, so this test cannot tell whether a minted token survives the lookup; stderr:\n%s", refresh.errOut())
	}

	if line, ok := tokenLine(f.sb.vmPath(agentsTokenFile)); !ok || line != tokens[0] {
		t.Errorf("%s does not hold the token the mint wrote before the bot user lookup was refused: a failed lookup never discards a minted token, not even one that stops the refresh (third review, item 3)\nstderr:\n%s",
			agentsTokenFile, refresh.errOut())
	}

	if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
		t.Errorf("%s is there although no bot user lookup succeeded", agentsIdentityFile)
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

			assertStopLineNamesWhereToRestart(t, stopped)

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

// TestAgentsAppTokenRefreshNeverClaimsAPastExpiryWhenItStops pins the second review's item 9: the stop line must
// not tell a maintainer the VM's token is valid until a time that has already passed, which is what a token that
// GitHub already expired, or a VM whose clock ran ahead, leaves behind.
func TestAgentsAppTokenRefreshNeverClaimsAPastExpiryWhenItStops(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEagerEnv()

	// Tokens the fake API hands out have already expired: the refresh writes one, so the expiry it knows is in
	// the past when the permanent failure stops it.
	f.api.setTokenLifetime(-2 * time.Hour)

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	waitFor(t, 30*time.Second, "the refresh to write a token whose expiry has already passed", func() bool {
		body, err := os.ReadFile(f.sb.vmPath(agentsTokenFile))
		if err != nil {
			return false
		}

		lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
		if len(lines) != 2 {
			return false
		}

		expiry, err := strconv.ParseInt(lines[1], 10, 64)

		return err == nil && expiry < time.Now().Unix()
	})

	f.api.refuseMintsWith(http.StatusUnauthorized)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh was still running 30s after GitHub answered 401, which no retry can fix; stderr:\n%s", refresh.errOut())
	}

	var stopLine string

	for _, line := range strings.Split(refresh.errOut(), "\n") {
		if strings.Contains(line, "agents App token refresh stopped") {
			stopLine = line
		}
	}

	if stopLine == "" {
		t.Fatalf("the refresh's stderr has no line saying 'agents App token refresh stopped', so a stopped refresh is invisible: %s", refresh.errOut())
	}

	if strings.Contains(stopLine, "valid until") {
		t.Errorf("the stop line tells the maintainer the VM's token is valid until a time that has already passed (review item 9): %q", stopLine)
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshTakesOverAStaleLockOnce pins the second review's item 5: two refreshers that find the
// same stale lock cannot both take it over. The takeover is atomic - one contender moves the stale lock aside
// and the other cannot - so after both have settled exactly one refresher is running and the lock holds its pid.
// The window between them is a race, so the test repeats a few rounds; what it asserts holds for every
// interleaving, never for a lucky one.
func TestAgentsAppTokenRefreshTakesOverAStaleLockOnce(t *testing.T) {
	t.Parallel()

	const rounds = 6

	for round := range rounds {
		t.Run(fmt.Sprintf("round %d", round+1), func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()

			// The lock a refresher killed without its trap leaves: the directory, with the pid of a process
			// that is gone.
			stale := deadPid(t)
			lock := f.sb.refreshLockDir("shift2eu")

			if err := os.MkdirAll(lock, 0o700); err != nil {
				t.Fatalf("create the stale lock %s: %v", lock, err)
			}

			writeFileMode(t, filepath.Join(lock, "pid"), []byte(strconv.Itoa(stale)+"\n"), 0o600)

			// Both contenders start before either is waited on, so both find the same stale lock.
			first := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)
			second := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			waitFor(t, 30*time.Second, "one of the two refreshers to take the stale lock over and mint", func() bool {
				return len(f.api.minted()) >= 1
			})

			// Let the contender that lost finish its exit before counting the survivors.
			time.Sleep(500 * time.Millisecond)

			var holder *runningScript

			for _, contender := range []*runningScript{first, second} {
				if !contender.running() {
					continue
				}

				if holder != nil {
					t.Fatalf("both refreshers that found the same stale lock are still running (pids %d and %d): a takeover that two contenders can win puts two loops on one token file (review item 5)\nfirst stderr:\n%s\nsecond stderr:\n%s",
						first.pid(), second.pid(), first.errOut(), second.errOut())
				}

				holder = contender
			}

			if holder == nil {
				t.Fatalf("neither refresher took the stale lock over; first stderr:\n%s\nsecond stderr:\n%s", first.errOut(), second.errOut())
			}

			body, err := os.ReadFile(filepath.Join(lock, "pid"))
			if err != nil {
				t.Fatalf("read %s: %v: the refresher that took the lock over must write its pid there", filepath.Join(lock, "pid"), err)
			}

			owner, err := strconv.Atoi(strings.TrimSpace(string(body)))
			if err != nil {
				t.Fatalf("%s holds %q, not a pid", filepath.Join(lock, "pid"), strings.TrimSpace(string(body)))
			}

			if owner != holder.pid() {
				t.Errorf("the lock %s holds pid %d but the only running refresher is %d: the refresher that holds the lock must be the one running", lock, owner, holder.pid())
			}

			holder.signal(t, syscall.SIGTERM)

			if !holder.waitExit(30 * time.Second) {
				t.Fatalf("the refresher that took the lock over did not stop on SIGTERM; stderr:\n%s", holder.errOut())
			}

			if _, err := os.Stat(lock); err == nil {
				t.Errorf("the lock %s outlived the refresher that took it over", lock)
			}

			f.assertNoLeaks(t, holder.out(), holder.errOut())
		})
	}
}
