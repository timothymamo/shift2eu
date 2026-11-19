// Pins the loop make deck starts, dev/agents-app-token.sh refresh, where it is fragile: it must survive a long
// sleep of the Mac and a GitHub or a VM that is briefly not there, it must stop on an error only a maintainer
// can fix, and only one refresher per VM instance may run (PRD #161 milestone 1 review items 1 and 7,
// .dot-agent-deck/prd161m1-fixes.md).
package devenv_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// TestAgentsAppTokenRefreshSurvivesAGitConfigLockAnotherWriterHolds pins the other half of the mint's refusal when
// another writer holds the VM's ~/.gitconfig.lock: the refusal is transient, because a lock git config takes for the
// moment of one write is gone by the next check, so the loop says it will try again instead of stopping and never
// removes the other writer's lock (issue #241, the mint's status 4).
func TestAgentsAppTokenRefreshSurvivesAGitConfigLockAnotherWriterHolds(t *testing.T) {
	t.Parallel()

	const lockBody = "a git config another writer runs in the VM holds this lock\n"

	f := newMintFixture(t)
	env := f.refreshEagerEnv()

	lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
	writeFileMode(t, lock, []byte(lockBody), 0o600)

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	// Two mints: the first write was refused because the lock was held, and the loop tried again instead of
	// stopping. mint_once mints before it writes, so a second token proves the retry.
	waitFor(t, 30*time.Second, "the refresh to try a second mint while ~/.gitconfig.lock is held", func() bool {
		return len(f.api.minted()) >= 2 || !refresh.running()
	})

	if !refresh.running() {
		t.Fatalf("the refresh exited while another writer held the VM's %s, although a held lock is gone by the next check and the loop must never stop on it; stderr:\n%s",
			lock, refresh.errOut())
	}

	if strings.Contains(refresh.errOut(), "agents App token refresh stopped") {
		t.Errorf("the refresh said it stopped while another writer held the VM's %s: a lock held for the moment of one write is retried, never stopped on (issue #241)\nstderr:\n%s",
			lock, refresh.errOut())
	}

	// The refusal the loop retried is the lock's, not some other failure that leaves the lock looking untouched.
	if text := strings.ToLower(refresh.errOut()); !strings.Contains(text, "lock") {
		t.Errorf("the refresh retried but no line mentions the lock on ~/.gitconfig, so this test cannot tell a refusal by the lock from any other transient failure (issue #241)\nstderr:\n%s",
			refresh.errOut())
	}

	held, err := os.ReadFile(lock)
	if err != nil {
		t.Fatalf("the refresh removed the VM's %s, a lock it did not create: another writer's git config must survive it untouched (%v)", lock, err)
	}

	if string(held) != lockBody {
		t.Errorf("the VM's %s holds %q after the refresh retried, want %q: the refresh leaves another writer's lock exactly as it found it (issue #241)",
			lock, held, lockBody)
	}

	// The token is written before the bot's identity is known, so the held lock refuses the identity and not the
	// token; the identity file must not be there, because no write of it succeeded.
	if _, err := os.Stat(f.sb.vmPath(agentsIdentityFile)); err == nil {
		t.Errorf("the refresh wrote %s although another writer's lock refused every write of the bot identity (issue #241)",
			agentsIdentityFile)
	}

	refresh.signal(t, syscall.SIGTERM)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh did not stop on SIGTERM while another writer held the VM's %s; stderr:\n%s", lock, refresh.errOut())
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshStopsOnALockThatIsNotARegularFile is the other half of the pair the test above pins: a
// ~/.gitconfig.lock that is a symlink is no lock git wrote, so refusing it is permanent (status 3) and the loop
// stops with the stop line and status 1, where another writer's regular lock is transient and only retried. A
// refresh that retried this one would mint a token at every check for as long as it runs, and none would ever
// reach the VM's git configuration (issue #241).
func TestAgentsAppTokenRefreshStopsOnALockThatIsNotARegularFile(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEagerEnv()

	target := filepath.Join(f.sb.vmHome, "stale-lock-target")
	writeFileMode(t, target, []byte(staleGitLockBody), 0o600)

	lock := filepath.Join(f.sb.vmHome, ".gitconfig.lock")
	if err := os.Symlink(target, lock); err != nil {
		t.Fatalf("symlink %s to %s: %v", lock, target, err)
	}

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh was still running 30s after the fake VM's %s was a symlink, an entry no git wrote and no retry can turn into a lock; stderr:\n%s",
			lock, refresh.errOut())
	}

	if code := refresh.exitCode(t); code != 1 {
		t.Errorf("the refresh exited with %d although the fake VM's %s is a symlink, not the regular file git itself creates, want 1: that refusal is permanent, so the loop stops like every failure a maintainer has to fix (issue #241)\nstderr:\n%s",
			code, lock, refresh.errOut())
	}

	assertStopLineNamesWhereToRestart(t, refresh.errOut())

	// The refusal the stop line follows is the lock's, not some other permanent failure.
	if text := strings.ToLower(refresh.errOut()); !strings.Contains(text, "lock") {
		t.Errorf("the refresh stopped but no line mentions the lock on ~/.gitconfig, so a maintainer cannot tell what to remove (issue #241)\nstderr:\n%s",
			refresh.errOut())
	}

	// An entry the mint may not take for a lock is left exactly as it was found: never followed, never removed.
	info, err := os.Lstat(lock)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the refresh removed or replaced %s, a symlink to a file it may neither follow nor remove (%v)", lock, err)
	}

	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}

	if string(body) != staleGitLockBody {
		t.Errorf("%s holds %q after the refresh stopped, want %q: no write of the bot identity went through the lock's symlink (issue #241)",
			target, body, staleGitLockBody)
	}

	if _, err := os.Stat(filepath.Join(f.sb.vmHome, ".gitconfig")); err == nil {
		t.Errorf("the refresh created the fake VM's ~/.gitconfig although it refused to write the bot identity: a refused write leaves no half-written file behind (issue #241)")
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshIgnoresAStaleGitLockWhenTheIdentityIsAlreadyTheBots pins the third review's finding as
// the refresh sees it: once the VM's ~/.gitconfig holds exactly the bot's identity, a lock a killed git left behind
// is no obstacle to any later mint, so the loop keeps the VM's token fresh across mints with the stale lock in
// place, and leaves both the file and the lock exactly as it found them (issue #241).
func TestAgentsAppTokenRefreshIgnoresAStaleGitLockWhenTheIdentityIsAlreadyTheBots(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)

	name := f.api.slug + "[bot]"
	email := fmt.Sprintf("%d+%s[bot]@users.noreply.github.com", f.api.botUserID, f.api.slug)

	gitconfig := filepath.Join(f.sb.vmHome, ".gitconfig")
	writeFileMode(t, gitconfig, gitConfigBody(name, email), 0o600)

	before, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig before the refresh: %v", err)
	}

	lock := f.writeStaleGitLock(t)
	env := f.refreshEagerEnv()

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	// A second token in the file proves a mint that ran while the stale lock was there both minted and wrote: the
	// loop mints at every check (refreshEagerEnv), so a token file that still held the first token is the refusal.
	// The loop polls here rather than through waitFor, because the failure has to say what the VM's file still holds
	// and why the loop refused, not only that a condition did not come true.
	replaced := false

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !replaced {
		tokens := f.api.minted()
		if len(tokens) >= 2 {
			if line, ok := tokenLine(f.sb.vmPath(agentsTokenFile)); ok && line != tokens[0] {
				replaced = true
			}
		}

		if !replaced {
			time.Sleep(25 * time.Millisecond)
		}
	}

	if !replaced {
		line, _ := tokenLine(f.sb.vmPath(agentsTokenFile))

		t.Fatalf("the refresh minted %d tokens in 30s but the fake VM's %s still holds %q: the stale %s beside a ~/.gitconfig that already holds the bot's identity refused every write, so the VM's gh keeps a token the loop cannot replace although nothing needed writing (issue #241)\nstderr:\n%s",
			len(f.api.minted()), agentsTokenFile, line, lock, refresh.errOut())
	}

	if !refresh.running() {
		t.Fatalf("the refresh exited although the fake VM's ~/.gitconfig already held the bot's identity and the stale %s belongs to no writer; stderr:\n%s",
			lock, refresh.errOut())
	}

	if strings.Contains(refresh.errOut(), "agents App token refresh stopped") {
		t.Errorf("the refresh stopped with a stale %s beside a ~/.gitconfig that already holds the bot's identity: a lock nobody holds is no reason to stop the loop (issue #241)\nstderr:\n%s",
			lock, refresh.errOut())
	}

	refresh.signal(t, syscall.SIGTERM)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
	}

	after, err := os.Stat(gitconfig)
	if err != nil {
		t.Fatalf("stat the fake VM's .gitconfig after the refresh: %v", err)
	}

	if !os.SameFile(before, after) {
		t.Errorf("the refresh replaced the fake VM's ~/.gitconfig although it already held exactly the bot's identity %s <%s>: every mint of the loop would otherwise rewrite a file nothing changed (issue #241)",
			name, email)
	}

	body, err := os.ReadFile(gitconfig)
	if err != nil {
		t.Fatalf("read the fake VM's .gitconfig after the refresh: %v", err)
	}

	if want := gitConfigBody(name, email); !bytes.Equal(body, want) {
		t.Errorf("the fake VM's ~/.gitconfig holds\n%s\nafter the refresh, want\n%s\n(issue #241)", body, want)
	}

	held, err := os.ReadFile(lock)
	if err != nil {
		t.Errorf("the refresh removed %s, a lock it never took: a stale lock belongs to no writer, and the mint that took none leaves it alone (%v)",
			lock, err)
	} else if string(held) != staleGitLockBody {
		t.Errorf("the fake VM's %s holds %q after the refresh, want %q: the mint that needed no write touched no lock (issue #241)",
			lock, held, staleGitLockBody)
	}

	identity := readTwoLines(t, f.sb.vmPath(agentsIdentityFile), 0o600)
	if identity[0] != name || identity[1] != email {
		t.Errorf("%s holds %q <%q> after the refresh, want the bot %q <%q>: the identity file is written by every mint that knows the bot (issue #241)",
			agentsIdentityFile, identity[0], identity[1], name, email)
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
			name: "both headers and the reset is the later one",
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

// TestAgentsAppTokenRefreshStopsOnAMalformedSlug pins the second review's finding that a slug no App has stops the
// loop instead of being retried forever: a slug comes from GitHub and never fixes itself, and a `-example-agents` would
// reach the VM's name check and be refused at every check, so a refresh that took that for a transient failure would
// retry it for as long as it runs, with no token ever reaching the VM (issue #241).
func TestAgentsAppTokenRefreshStopsOnAMalformedSlug(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEagerEnv()

	f.api.setSlug("-example-agents")

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh was still running 30s after GET /app answered the slug -example-agents, which no retry can fix: the loop must stop as it does on any other permanent failure (issue #241)\nstderr:\n%s",
			refresh.errOut())
	}

	if code := refresh.exitCode(t); code != 1 {
		t.Errorf("the refresh exited with %d after GET /app answered the slug -example-agents, want 1: make deck, and the maintainer reading its log, relies on a stopped refresh being a failure (issue #241)", code)
	}

	assertStopLineNamesWhereToRestart(t, refresh.errOut())

	if posted := f.api.posted(); len(posted) != 0 {
		t.Errorf("the refresh posted to %v although the App's slug begins with -: the slug is refused before a token is minted, at every check as at the first (issue #241)",
			posted)
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshSurvivesAnEmptySlug pins the other side of TestAgentsAppTokenRefreshStopsOnAMalformedSlug:
// an empty slug is no malformed one. A 200 whose answer holds no slug reads as GitHub having said nothing yet, so the
// loop keeps trying, and a refresh that took every unusable slug for a permanent failure would stop on it and leave the
// VM without a token until a maintainer restarted it (issue #241).
func TestAgentsAppTokenRefreshSurvivesAnEmptySlug(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEagerEnv()

	f.api.setSlug("")

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	// Two requests for the App's slug: the loop went back to GitHub instead of stopping after the first.
	waitFor(t, 30*time.Second, "the refresh to ask GET /app again after an empty slug", func() bool {
		asked := 0

		for _, request := range f.api.requestOrder() {
			if request == "GET /app" {
				asked++
			}
		}

		return asked >= 2 || !refresh.running()
	})

	if !refresh.running() {
		t.Fatalf("the refresh exited after GET /app answered an empty slug, which is transient and must never stop it; stderr:\n%s",
			refresh.errOut())
	}

	if strings.Contains(refresh.errOut(), "agents App token refresh stopped") {
		t.Errorf("the refresh said it stopped after GET /app answered an empty slug: an answer with no slug is retried, not stopped on (issue #241)\nstderr:\n%s",
			refresh.errOut())
	}

	refresh.signal(t, syscall.SIGTERM)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh did not stop on SIGTERM after an empty slug; stderr:\n%s", refresh.errOut())
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
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

// TestAgentsAppTokenRefreshNeverTakesItsOwnLockForAnotherRefresh pins issue #240. The takeover of a stale lock is
// atomic, but the winner is not done once it has renamed its temporary directory into place: a second contender
// that moved the stale lock aside too can move the winner's fresh lock aside and, finding a pid there that is not
// the owner it read while the lock was missing, put it back. The winner's own check then finds no lock, and the
// lock it reads next holds its own pid. A refresh must never take its own pid for another refresher's: it holds
// the lock, so it keeps it and mints the VM's token. The buggy script says another refresh is running with the
// winner's own pid and exits 0, so no refresher is left and nothing mints (issue #240).
//
// No run forces that interleaving by itself, so the test forces it with the mv and rm shims of
// useLockRacedByAContender: one moves the lock the refresh has just won aside, where the refresh's own read finds
// no lock, and the other puts it back before the refresh reads the lock's owner, exactly where the race does.
func TestAgentsAppTokenRefreshNeverTakesItsOwnLockForAnotherRefresh(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	// The lock a refresher killed without its trap leaves: the directory, with the pid of a process that is
	// gone.
	lock := f.sb.refreshLockDir("shift2eu")

	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatalf("create the stale lock %s: %v", lock, err)
	}

	writeFileMode(t, filepath.Join(lock, "pid"), []byte(strconv.Itoa(deadPid(t))+"\n"), 0o600)

	race := f.sb.useLockRacedByAContender(t, env, lock)

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	// What a failure has to show: the refresh's own pid and output, what the lock directory holds, and whether the
	// shims forced the interleaving at all.
	contest := func() string {
		return fmt.Sprintf("the refresh (pid %d, running %t) stderr:\n%s\nstate directory:\n%s\nforced interleaving:\n%s",
			refresh.pid(), refresh.running(), refresh.errOut(), describeTree(f.sb.stateDir()),
			readOutput(filepath.Join(race, "log")))
	}

	minted := func() bool {
		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return ok && line != ""
	}

	// The refresh either keeps the lock and writes the VM's token or exits: the buggy one exits.
	if !poll(30*time.Second, func() bool { return minted() || !refresh.running() }) {
		t.Fatalf("timed out after 30s waiting for the refresh either to mint the VM's token or to exit\n%s", contest())
	}

	if !refresh.running() {
		why := "no refresher is left to mint the VM's token"

		if own := fmt.Sprintf("is running (pid %d", refresh.pid()); strings.Contains(refresh.errOut(), own) {
			why = fmt.Sprintf("it took the lock for another refresher's because it holds its own pid %d", refresh.pid())
		}

		t.Fatalf("the refresh won the stale lock and had the lock it had just won moved aside and put back, as the other contender does in the race of issue #240, but it exited with %d instead of keeping the lock it holds and minting: %s (issue #240)\n%s",
			refresh.exitCode(t), why, contest())
	}

	// The one refresher that is left holds the lock: the pid in it is the running refresher's own.
	body, err := os.ReadFile(filepath.Join(lock, "pid"))
	if err != nil {
		t.Fatalf("read %s: %v\n%s", filepath.Join(lock, "pid"), err, contest())
	}

	owner, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("%s holds %q, not a pid\n%s", filepath.Join(lock, "pid"), strings.TrimSpace(string(body)), contest())
	}

	if owner != refresh.pid() {
		t.Errorf("the lock %s holds pid %d but the only running refresher is %d: the refresher that wins the takeover must be the one that holds the lock (issue #240)\n%s",
			lock, owner, refresh.pid(), contest())
	}

	// It mints: the lock exists for the VM's token, and a refresher that exited above left none.
	waitFor(t, 30*time.Second, "the refresh that kept the lock to write the VM's token", minted)

	// The interleaving was forced: without both steps the assertions above proved nothing about the race.
	raced, err := os.ReadFile(filepath.Join(race, "log"))
	if err != nil {
		t.Fatalf("read %s: %v: the shims never moved the lock the refresh had won aside and put it back, so this run forced no interleaving of issue #240\n%s",
			filepath.Join(race, "log"), err, contest())
	}

	if steps := strings.Fields(string(raced)); len(steps) != 2 || steps[0] != "moved-aside" || steps[1] != "put-back" {
		t.Fatalf("%s holds %v, want the other contender's two steps, moved-aside then put-back, so this run forced no interleaving of issue #240\n%s",
			filepath.Join(race, "log"), steps, contest())
	}

	refresh.signal(t, syscall.SIGTERM)

	if !refresh.waitExit(30 * time.Second) {
		t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
	}

	if _, err := os.Stat(lock); err == nil {
		t.Errorf("the lock %s outlived the refresher that held it", lock)
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshThreeContendersOnAStaleLockLeaveOneHolder pins issue #288. The takeover of a stale
// lock is atomic, but a refresher that has won it is not safe yet: a contender that read the stale owner first
// can still move the winner's fresh lock aside, and when it puts that lock back while a third contender has a
// lock of its own in place, mv puts it back inside that lock, as a nested copy. The winner then mints beside the
// contender that holds the lock, two refreshers each believing they hold it, and the put-back left a copy of the
// winner's lock inside the lock nobody will ever remove until the holder exits (issue #288).
//
// The interleaving is forced by shims, not by speed: each of the three refreshers is held at the step it needs
// (useThreeContendersOnAStaleLock), so the lock is the winner's before the mover moves it aside, the taker's
// before the mover puts it back, and the winner's own check runs after all of that. A refresh must find the lock
// no longer its own and stop as a losing refresher does, and the mover must not leave the nested copy.
//
// The winner is held in its loop right after the clock read and before its mint, so the check it needs is the one
// before the mint itself, not one at the top of the loop: between the two, the lock can turn over exactly as it
// does here.
//
// The order in which the three start is the test's too, not the runner's: the mover and the taker start first and
// are held before either can touch the lock, the mover only after it has read the stale owner, and the winner
// starts once both are held. Started together, the winner's takeover of the stale lock and the mover's read of
// that lock's owner raced: the winner could take the lock over before the mover read the owner, which then found
// a running refresh where it expected the stale one and left as a losing refresher without ever moving a lock
// aside, and a mover that created its own lock in the winner's takeover window won the lock outright and left the
// winner to do the same. Both left this test's holds unreached under load.
func TestAgentsAppTokenRefreshThreeContendersOnAStaleLockLeaveOneHolder(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	lock := f.sb.refreshLockDir("shift2eu")

	// The stale lock: a lock whose pid is alive but runs no refresh, which take_lock takes over as it does a
	// lock a killed refresher left behind.
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatalf("create the stale lock %s: %v", lock, err)
	}

	writeFileMode(t, filepath.Join(lock, "pid"), []byte(strconv.Itoa(liveNonRefreshPid(t))+"\n"), 0o600)

	race := f.sb.useThreeContendersOnAStaleLock(t, f.refreshEnv(), lock)

	// The mover and the taker start against the stale lock: the mover is held once it has read the stale owner,
	// before it asks whether that owner runs a refresh, and the taker at the top of its take_lock, before it
	// touches the lock at all. Neither can take the lock over from here.
	mover := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, race.envMover)
	taker := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, race.envTaker)

	// What a failure has to show: each refresher's pid, whether it runs and its output, and the state directory
	// with the lock's pid and whatever the put-back left behind. The winner joins the report once it starts.
	type contender struct {
		name string
		run  *runningScript
	}

	contenders := []contender{{"the mover", mover}, {"the taker", taker}}

	contest := func() string {
		lines := make([]string, 0, len(contenders)+2)
		for _, c := range contenders {
			lines = append(lines, fmt.Sprintf("%s (pid %d, running %t) stderr:\n%s", c.name, c.run.pid(), c.run.running(), c.run.errOut()))
		}

		lines = append(lines, "state directory:", describeTree(f.sb.stateDir()))

		return strings.Join(lines, "\n")
	}

	// The mover has read the stale lock's owner and the taker is at the top of its take_lock: the stale lock is
	// still the stale one, and the winner is the only refresher left that can take it over.
	if !poll(30*time.Second, func() bool {
		return race.held(contenderAskedOwnership) && race.held(contenderReachedItsTake)
	}) {
		t.Fatalf("the mover and the taker did not reach their holds before the winner started within 30s: the steps reached were %s; the mover must reach ps only for the stale owner it read, so no refresher may have taken the stale lock over yet\n%s", race.reached(), contest())
	}

	// The winner starts now, and takes the stale lock over: nothing else can touch the lock until the test lets a
	// contender go.
	winner := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, race.envWinner)

	contenders = append(contenders, contender{"the winner", winner})

	// The winner reaches its hold with the lock in hand, before anything mints.
	if !poll(30*time.Second, func() bool {
		return race.held(contenderReadTheClock) && lockHoldsPid(lock, winner.pid())
	}) {
		t.Fatalf("the winner did not reach its hold in the refresh loop with the stale lock in hand within 30s: the steps reached were %s; the winner is the only refresher that may take the stale lock over, so a missing step above names the contender that took it\n%s", race.reached(), contest())
	}

	if minted := len(f.api.minted()); minted != 0 {
		t.Fatalf("%d token(s) were minted before the test released any refresher, so the holds staged no interleaving: the winner must wait in its loop before it mints (issue #288)\n%s", minted, contest())
	}

	// The mover takes its turn: the lock it finds is the winner's, and the pid it reads there is not the stale
	// owner it read before, so what it moves aside is the winner's fresh lock and not the stale one.
	race.release(t, holdTheMover)

	if !poll(30*time.Second, func() bool { return race.held(contenderMovedTheLockAside) }) {
		t.Fatalf("the mover did not move the winner's fresh lock aside within 30s, so this run forced no interleaving of issue #288: the steps reached were %s\n%s", race.reached(), contest())
	}

	if _, err := os.Stat(lock); err == nil {
		t.Fatalf("the lock %s is still in place after the mover moved the winner's lock aside, so the mover took something else: this run forced no interleaving of issue #288\n%s", lock, contest())
	}

	// The taker takes the lock the mover freed, and mints with it.
	race.release(t, holdTheTaker)

	waitFor(t, 30*time.Second, "the taker to take the free lock over and mint with it", func() bool {
		return lockHoldsPid(lock, taker.pid()) && len(f.api.minted()) == 1
	})

	// The mover puts back what it moved, but the taker's lock is in place by now: it drops what it moved aside
	// instead of nesting it inside that lock, and then stops as a losing refresher does (issue #288).
	race.release(t, holdThePutBack)

	waitFor(t, 30*time.Second, "the mover to stop as a losing refresher", func() bool {
		return !mover.running()
	})

	if strings.TrimSpace(mover.errOut()) == "" {
		t.Errorf("the mover stopped without a word, so a maintainer cannot tell why it left the lock alone\n%s", contest())
	}

	// While the winner still holds its own belief and the taker holds the lock, the lock must hold nothing but
	// the taker's pid: the copy the put-back nested inside it is what issue #288 names, and it belongs to a
	// refresher that will never remove it.
	if names := dirNames(lock); strings.Join(names, ",") != "pid" {
		t.Errorf("the lock %s holds %v while the taker holds it, want only pid: the mover's put-back landed inside the taker's lock as a nested copy that only the taker's own exit removes (issue #288)\n%s",
			lock, names, contest())
	}

	// The winner goes on: it must find the lock no longer its own and stop as a losing refresher does, not mint
	// beside the taker.
	race.release(t, holdTheWinner)

	if !poll(30*time.Second, func() bool { return !winner.running() || len(f.api.minted()) > 1 }) {
		t.Fatalf("the winner neither stopped nor minted 30s after the test let it go with the lock no longer its own\n%s", contest())
	}

	if winner.running() {
		t.Errorf("the refresher that won the stale lock still runs after another refresher took the lock over: two refreshers each believe they hold the lock, so both refresh the VM's token file (issue #288)\n%s", contest())
	}

	if minted := len(f.api.minted()); minted != 1 {
		t.Errorf("%d token(s) were minted, want one: exactly one refresher holds the lock and mints, and the winner minted beside the taker (issue #288)\n%s", minted, contest())
	}

	if !winner.running() && strings.TrimSpace(winner.errOut()) == "" {
		t.Errorf("the winner stopped without a word, so a maintainer cannot tell why it left the lock to the taker\n%s", contest())
	}

	if !winner.running() {
		t.Logf("the refresher that lost the lock exited with %d", winner.exitCode(t))
	}

	if !taker.running() {
		t.Errorf("the taker exited after taking the lock over, so no refresher is left to refresh the VM's token\n%s", contest())
	}

	if !lockHoldsPid(lock, taker.pid()) {
		t.Errorf("the lock %s does not hold the taker's pid %d: the only refresher left must be the one holding the lock\n%s", lock, taker.pid(), contest())
	}

	taker.signal(t, syscall.SIGTERM)

	if !taker.waitExit(30 * time.Second) {
		t.Fatalf("the taker did not stop on SIGTERM; stderr:\n%s", taker.errOut())
	}

	if _, err := os.Stat(lock); err == nil {
		t.Errorf("the lock %s outlived the refresher that held it", lock)
	}

	for _, name := range dirNames(f.sb.stateDir()) {
		if strings.Contains(name, ".lock") {
			t.Errorf("the state directory still holds %s after every refresher stopped: an orphan lock outlives the refreshers it came from (issue #288)\n%s", name, contest())
		}
	}

	f.assertNoLeaks(t, winner.out()+mover.out()+taker.out(), winner.errOut()+mover.errOut()+taker.errOut())
}

// TestAgentsAppTokenRefreshLeavesNoSleepWhenASignalArrivesInTheWindow pins issue #289. The refresh loop waits
// with `sleep "${wait_seconds}" &` and then stores that sleep's pid in sleeper, and its TERM and INT traps kill
// ${sleeper} only when it is set: a signal that arrives between those two lines leaves the sleep running after
// the script has exited, for up to an hour, holding whatever it inherited. Under launchd the job's process group
// hides that; a hand-started refresh leaves a stray process behind. The trap must kill the last background job as
// well, or the sleep outlives the refresh.
//
// The window cannot be forced in the running loop from outside: a shell forks the sleep and stores $! long before
// the forked sleep has even exec'd, so a sleep shim that signals the script from its first line always arrives
// after the store (measured through dash, and PR #286 met the remaining sliver only under load), and no tool the
// loop runs lies between the two lines to hold the shell there. So this test forces the window where it can: it
// replays the window with the loop's own trap, taken from dev/agents-app-token.sh's text, one sleep started, and
// no sleeper stored. The seam that would let the test use the loop instead is a hold between the two lines, which
// the script does not have and no PATH shim can add.
//
// The replay also pins the other half of the same variable: after a mint the loop stores `sleeper=none` once the
// sleep ran, and between `sleeper=""` and `sleep ... &` of the next round $! still names that finished sleep, a
// pid the system may already have given to an unrelated process since. A trap that kills `$!` whenever sleeper is
// empty kills that unrelated process, so the replay runs a background job to its end and reaps it, and records
// every pid the trap asks to kill through a kill function of its own.
//
// The loop's first round has a third state: `sleeper=""` before the loop's first `sleep ... &`, when the shell
// has never started a background job and $! is unset rather than the pid of a finished one. The script runs under
// `set -eu`, and the trap names $! (`${sleeper:-$!}`) for both the ps call that asks whether it is a child of
// this shell and the kill: a signal there makes the shell report the unset parameter ("!: parameter not set" in
// dash, "$!: unbound variable" in bash --posix, measured), which the refresh writes into its log. So the replay
// runs under `set -eu` as the script does, and expects the trap to name no pid there at all.
func TestAgentsAppTokenRefreshLeavesNoSleepWhenASignalArrivesInTheWindow(t *testing.T) {
	t.Parallel()

	script := readRepoFile(t, agentsMintScript)

	// The states a signal in the loop finds once its trap is installed: its own `sleep ... &` started with the
	// store of its pid still on the way (issue #289's window), and a background job that has finished and been
	// reaped while sleeper is empty or none, which is every round of the loop just before its sleep starts.
	const (
		beforeTheStore = `sleeper=""
sleep 300 &
printf '%s\n' "$!" > "${SHIFT2EU_TEST_SLEEP_PID:?}"`
		anEmptySleeperOverAReapedJob = `sleeper=""
: &
job=$!
printf '%s\n' "${job}" > "${SHIFT2EU_TEST_JOB_PID:?}"
wait "${job}" || true`
		aSleeperOfNoneOverAReapedJob = `sleeper=none
: &
job=$!
printf '%s\n' "${job}" > "${SHIFT2EU_TEST_JOB_PID:?}"
wait "${job}" || true`
		// noBackgroundJobEver is the loop's first round before its first `sleep ... &`: sleeper is empty and the
		// shell has never started a background job, so $! is unset rather than the pid of a finished one. The
		// script runs under `set -eu`, so a trap that expands $! here aborts instead of running to its end.
		noBackgroundJobEver = `sleeper=""`
	)

	for _, tc := range []struct {
		name   string
		signal syscall.Signal
		trap   string
		status int
		// window is the state the replay is in when the signal arrives.
		window string
		// startsTheSleep is set for the window between `sleep ... &` and `sleeper=$!`: the trap must kill the
		// sleep the replay has just started.
		startsTheSleep bool
		// killsNothing is set for the window where sleeper is none and only a finished job's pid is left in $!:
		// the trap must send no kill at all. The window where sleeper is empty over the same finished job must
		// send none for that pid, which is the one case that names a pid a reused one would hit. The window
		// before the loop's first `sleep ... &` names no pid either, and has none to kill.
		killsNothing bool
		// noBackgroundJob is set for the window in which the shell has never started a background job, so the
		// replay has no pid to record and $! is unset.
		noBackgroundJob bool
	}{
		{
			name: "a TERM between the sleep and the store of its pid", signal: syscall.SIGTERM, trap: "TERM", status: 143,
			window: beforeTheStore, startsTheSleep: true,
		},
		{
			name: "an INT between the sleep and the store of its pid", signal: syscall.SIGINT, trap: "INT", status: 130,
			window: beforeTheStore, startsTheSleep: true,
		},
		{
			name: "a TERM after a mint, with the last sleep already reaped", signal: syscall.SIGTERM, trap: "TERM", status: 143,
			window: anEmptySleeperOverAReapedJob,
		},
		{
			name: "a TERM while no sleep runs, with the last sleep already reaped", signal: syscall.SIGTERM, trap: "TERM", status: 143,
			window: aSleeperOfNoneOverAReapedJob, killsNothing: true,
		},
		{
			name: "a TERM before any background job has ever been started", signal: syscall.SIGTERM, trap: "TERM", status: 143,
			window: noBackgroundJobEver, killsNothing: true, noBackgroundJob: true,
		},
		{
			name: "an INT before any background job has ever been started", signal: syscall.SIGINT, trap: "INT", status: 130,
			window: noBackgroundJobEver, killsNothing: true, noBackgroundJob: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sb := newSandbox(t)
			harness := filepath.Join(sb.root, "window.sh")
			sleepPidFile := filepath.Join(sb.root, "sleep.pid")
			jobPidFile := filepath.Join(sb.root, "job.pid")
			killedFile := filepath.Join(sb.root, "killed")
			releasedFile := filepath.Join(sb.root, "released")
			held := filepath.Join(sb.root, "held")
			hold := filepath.Join(sb.root, "hold.fifo")

			makeFifo(t, hold)

			// The loop's window, replayed: its own trap, then the state a signal in the loop finds, and then a
			// hold. The hold is a fifo no one ever writes, so the replay is still in the window when the signal
			// arrives. stopped stands in for the script's own, which needs the mint's state and says only where the
			// refresh stopped. release_lock records that the trap ran to its end, the step a trap that aborts on an
			// unset variable never reaches. kill is a function here, not the builtin: a POSIX shell finds a
			// function of a name before the builtin of that name, so the replay records every pid the trap asks to
			// kill and a run can prove no kill went to a pid that a reused one would hit. The replay runs under
			// `set -eu` as dev/agents-app-token.sh does, so an expansion the script survives without the option
			// fails here as it would there.
			harnessText := fmt.Sprintf(`#!/bin/sh
set -eu
stopped() { :; }
release_lock() { printf 'released\n' >> "${SHIFT2EU_TEST_RELEASED:?}"; }
kill() {
  if [ "$#" -gt 0 ]; then
    for pid in "$@"; do printf '%%s\n' "${pid}" >> "${SHIFT2EU_TEST_KILLED:?}"; done
    command kill "$@"
  fi
  return 0
}
sleeper=none
%s
%s
: > "${SHIFT2EU_TEST_HELD:?}"
read -r go < "${SHIFT2EU_TEST_HOLD_FIFO:?}"
printf 'the signal arrived after the trap, so this run forced no window\n' >&2
`, refreshTrap(t, script, tc.trap), tc.window)
			writeFileMode(t, harness, []byte(harnessText), 0o700)

			env := sb.baseEnv()
			env["SHIFT2EU_TEST_SLEEP_PID"] = sleepPidFile
			env["SHIFT2EU_TEST_JOB_PID"] = jobPidFile
			env["SHIFT2EU_TEST_KILLED"] = killedFile
			env["SHIFT2EU_TEST_RELEASED"] = releasedFile
			env["SHIFT2EU_TEST_HELD"] = held
			env["SHIFT2EU_TEST_HOLD_FIFO"] = hold

			// The harness runs like a script the test starts: in a process group of its own, with its output in
			// files, so the sleep the trap may leave behind cannot block the wait (issue #267).
			run := sb.start(t, harness, nil, env)

			waitFor(t, 30*time.Second, "the replay to come to its hold with the window in place", func() bool {
				_, err := os.Stat(held)

				return err == nil
			})

			// jobPid is the pid the trap finds in sleeper or in $!: the sleep the replay started, or the background
			// job it ran to its end and reaped. A window in which the shell never started a background job has none,
			// and leaves jobPid zero.
			var jobPid int

			switch {
			case tc.noBackgroundJob:
				// Nothing to wait for: the replay started no background job at all, so $! is unset.
			case tc.startsTheSleep:
				jobPid = pidFrom(t, sleepPidFile, "the sleep the window started")

				// The signal goes in once the sleep is the sleep: a background child a shell has just forked can
				// lose a signal to its own exec still being on the way (measured through dash), which is no
				// behaviour of the refresh and would make this test flaky. The window itself, a running sleep and
				// an empty sleeper, is in place either way.
				waitFor(t, 30*time.Second, "the replay's sleep to be running before the signal goes in", func() bool {
					comm, err := exec.Command(realTool(t, "ps"), "-p", strconv.Itoa(jobPid), "-o", "comm=").Output()

					return err == nil && strings.TrimSpace(string(comm)) == "sleep"
				})
			default:
				jobPid = pidFrom(t, jobPidFile, "the background job the window ran to its end")
			}

			run.signal(t, tc.signal)

			if !run.waitExit(30 * time.Second) {
				t.Fatalf("the replay of issue #289's window did not stop 30s after the %s; stderr:\n%s", tc.trap, run.errOut())
			}

			if status := run.exitCode(t); status != tc.status {
				t.Fatalf("the refresh's %s trap ended the script with status %d, want %d: the trap, not the signal's default action, must handle it; stderr:\n%s",
					tc.trap, status, tc.status, run.errOut())
			}

			if strings.Contains(run.errOut(), "forced no window") {
				t.Errorf("the replay went on after its signal, so the trap never ran and this run proved nothing about issue #289; stderr:\n%s", run.errOut())
			}

			if strings.Contains(run.errOut(), "not found") {
				t.Fatalf("the loop's %s trap names a command this replay does not define, so the trap ran only in part and this run proves nothing about issue #289: define it in the harness as the script does, or replay the command the trap now names; stderr:\n%s",
					tc.trap, run.errOut())
			}

			// release_lock is the trap's last step before its exit: a trap that aborted early, on an expansion under
			// `set -u` say, never gets there and leaves the lock it holds behind.
			if body, err := os.ReadFile(releasedFile); err != nil || strings.TrimSpace(string(body)) == "" {
				t.Fatalf("the refresh's %s trap never reached release_lock, so it aborted before its end and left the lock the refresher holds unreleased (err %v); stderr:\n%s",
					tc.trap, err, run.errOut())
			}

			// Under `set -u` the shell reports every expansion of a variable it has no value for: dash says
			// "!: parameter not set", bash --posix "$!: unbound variable". The loop's first round, before its first
			// `sleep ... &`, has no $! at all, so a trap that names it there writes such an error into the refresh's
			// log on the one signal it takes. The stderr also carries the replay's interrupted fifo open, which is the
			// hold's own doing and no behaviour of the script; only the shell's report of the unset parameter is.
			for _, report := range []string{"parameter not set", "unbound variable"} {
				if strings.Contains(run.errOut(), report) {
					t.Fatalf("the refresh's %s trap made the shell report %q: the trap expands $! (${sleeper:-$!}) before the loop's first `sleep ... &`, when the shell has never started a background job and $! is unset, so a signal in that window writes a shell error into the refresh's log; the trap must name no pid while no background job has run (issue #289)\nstderr:\n%s",
						tc.trap, report, run.errOut())
				}
			}

			if tc.startsTheSleep {
				// A killed sleep is a zombie until the system reaps it, so the sleep stopping is what the test
				// waits for; a sleep the trap never killed sleeps on until its own time is up.
				if !poll(10*time.Second, func() bool { return sleepStopped(t, jobPid) }) {
					t.Errorf("the refresh's %s trap left the sleep it had just started running (pid %d) after the script had exited: a signal that arrives between `sleep ... &` and `sleeper=$!` must kill the last background job too, or the sleep outlives the refresh for up to an hour (issue #289)\nstderr:\n%s",
						tc.trap, jobPid, run.errOut())
				}

				return
			}

			killed := killedPids(t, killedFile)

			if tc.killsNothing {
				if len(killed) > 0 {
					t.Errorf("the refresh's %s trap killed %v although the window held no sleep of the loop to kill (sleeper none, or empty with no background job started yet): a finished sleep's pid, which the system may have given to another process since, must never be killed, and before the loop's first `sleep ... &` there is no pid at all (issue #289)\nstderr:\n%s",
						tc.trap, killed, run.errOut())
				}

				return
			}

			if jobPid == 0 {
				return
			}

			for _, pid := range killed {
				if pid == strconv.Itoa(jobPid) {
					t.Errorf("the refresh's %s trap killed pid %d, the pid of a background job that had already finished and been reaped, because $! still names it while sleeper is empty: that pid may belong to any process by the time a signal arrives between `sleeper=\"\"` and `sleep ... &`, so the trap must kill nothing there (issue #289)\nstderr:\n%s",
						tc.trap, jobPid, run.errOut())
				}
			}
		})
	}
}

// pidFrom is the pid the replay wrote to pidFile, for what names the process in a failure message: the sleep the
// window started, or the background job it ran to its end and reaped.
func pidFrom(t *testing.T, pidFile, what string) int {
	t.Helper()

	body, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read the pid of %s: %v: the replay never reached it, so it forced no window", what, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("%s holds %q, not the pid of %s", pidFile, strings.TrimSpace(string(body)), what)
	}

	return pid
}

// killedPids is the pids the replay's kill recorded, in the order the trap asked for them: the replay writes one
// pid per line and writes nothing when the trap killed nothing.
func killedPids(t *testing.T, path string) []string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		t.Fatalf("read the pids the replay's trap killed: %v", err)
	}

	return strings.Fields(string(body))
}

// refreshTrap returns the one-line `trap '<command>' NAME` the refresh loop sets for name, out of the script's
// own text. The window of issue #289 cannot be forced in the running loop (see the test above), so that test
// replays the window with the trap exactly as the script writes it: a trap a change words otherwise has to be
// replayed otherwise, and this function's failure says so.
func refreshTrap(t *testing.T, script, name string) string {
	t.Helper()

	line := regexp.MustCompile(`(?m)^[ \t]*(trap '.*' ` + regexp.QuoteMeta(name) + `[ \t]*)$`)
	found := line.FindAllStringSubmatch(script, -1)

	if len(found) == 0 {
		t.Fatalf("dev/agents-app-token.sh sets no one-line `trap '<command>' %s` any more, so the replay of issue #289's window cannot take the refresh's %s trap from it: word the trap as one line again, or replay the command it now names", name, name)
	}

	return found[len(found)-1][1]
}

// sleepStopped reports whether the sleep of pid stopped: a sleep the trap killed is gone, or a zombie the system
// has not reaped yet, and it sleeps on when no trap killed it.
func sleepStopped(t *testing.T, pid int) bool {
	t.Helper()

	state, err := exec.Command(realTool(t, "ps"), "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		return true // ps finds no such process any more: the sleep is gone
	}

	return strings.HasPrefix(strings.TrimSpace(string(state)), "Z")
}

// assertSaysTheLockIsGone fails unless the refresher's last word says that the lock it held is gone and that a
// refresh must be started again: the only thing a maintainer can do about a lock that vanished while a refresher
// held it, and the opposite of the word a message that cannot tell a lock in transit from a lost one prints
// (issue #288).
func assertSaysTheLockIsGone(t *testing.T, text string) {
	t.Helper()

	if !strings.Contains(strings.ToLower(text), "gone") {
		t.Errorf("the refresher's stderr never says the lock is GONE, so a maintainer reads that a refresh holds a lock that nothing holds (issue #288): %s", text)
	}

	restart := []string{"start it again", "started again", "start a refresh", "start another refresh", "start a new refresh", "make agents-token-refresh-start"}

	for _, said := range restart {
		if strings.Contains(text, said) {
			return
		}
	}

	t.Errorf("the refresher's stderr does not say that a refresh must be started again (any of %s), so a maintainer cannot tell that nothing refreshes the VM's token now (issue #288): %s",
		strings.Join(restart, ", "), text)
}

// TestAgentsAppTokenRefreshHandlesALockThatGoesMissing pins issue #288's other half: the lock a refresher holds
// can go missing while it holds it. A contender that read a stale owner moves the lock it finds aside, and the one
// that reads it back while it is missing must not take the missing lock for another refresher's: a lock in transit
// is put back, so the refresher reads its own pid again and goes on, exactly once; a lock that never comes back at
// all leaves it nothing to mint with, and it must say that the lock is gone rather than that another refresh took
// it over.
//
// Every hold is a fifo and the clock is a shim, so no test waits on real time: the test decides when the lock goes
// missing, when it comes back and when the refresher's marches of a second run through.
func TestAgentsAppTokenRefreshHandlesALockThatGoesMissing(t *testing.T) {
	t.Parallel()

	t.Run("the lock comes back during the retry, and the refresher mints once and keeps it", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)
		env := f.refreshEnv()
		lock := f.sb.refreshLockDir("shift2eu")
		gone := f.sb.useGoneLockShims(t, env, lock)

		gone.holdTheNextClock(t)
		gone.holdTheNextWait(t)

		refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

		// The refresher holds the lock and waits at the loop's clock read, the last point before still_locked
		// reads it.
		if !poll(30*time.Second, func() bool {
			return gone.reached(heldAtTheClock) && lockHoldsPid(lock, refresh.pid())
		}) {
			t.Fatalf("the refresher did not come to its clock read with the lock in hand within 30s\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		// A contender that read the stale owner of the lock, or a refresher that has just lost it, moves it aside:
		// the refresher holds a lock that is not there, which is the one state still_locked may retry.
		gone.moveTheLockAside(t)
		gone.letTheClockGo(t)

		if !poll(30*time.Second, func() bool { return gone.reached(heldAtTheWait) }) {
			t.Fatalf("the refresher did not wait for the lock it holds once it found it missing within 30s\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		if _, err := os.Stat(lock); err == nil {
			t.Fatalf("the lock %s is in place while the test has not put it back yet\n%s", lock, gone.describe())
		}

		gone.putTheLockBack(t)

		// The retry returns once the lock is back, and the loop's own sleep comes next: the test holds that one too,
		// so the refresher parks at it instead of spinning its loop - the sleep shim returns at once while no hold
		// is armed - until the SIGTERM below.
		gone.holdTheNextWait(t)
		gone.letTheWaitGo(t)

		waitFor(t, 30*time.Second, "the refresher whose lock came back during the retry to write a token", func() bool {
			line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

			return ok && line != ""
		})

		if !refresh.running() {
			t.Fatalf("the refresher exited although the lock it holds came back during its retry: a lock that is missing is only in transit, moved aside by a contender that puts it back, so it must go on, mint and keep the lock (issue #288)\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		if got := len(f.api.minted()); got != 1 {
			t.Errorf("%d token(s) were minted, want one: the one refresher that holds the lock mints once, and a lock that came back during the retry is the same lock it held (issue #288)\n%s\nstderr:\n%s",
				got, gone.describe(), refresh.errOut())
		}

		if !lockHoldsPid(lock, refresh.pid()) {
			t.Errorf("the lock %s does not hold the pid %d of the only refresher that runs: the refresher that kept the lock must go on holding it (issue #288)\n%s",
				lock, refresh.pid(), gone.describe())
		}

		refresh.signal(t, syscall.SIGTERM)

		if !refresh.waitExit(30 * time.Second) {
			t.Fatalf("the refresher did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
		}

		if _, err := os.Stat(lock); err == nil {
			t.Errorf("the lock %s outlived the refresher that held it", lock)
		}

		f.assertNoLeaks(t, refresh.out(), refresh.errOut())
	})

	t.Run("the margin check uses the clock of after the wait", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)
		env := f.refreshEnv() // a token is minted again once its margin, a minute, is under
		lock := f.sb.refreshLockDir("shift2eu")
		gone := f.sb.useGoneLockShims(t, env, lock)

		// The loop's first sleep comes after its first mint, and the test holds it: the clock read after it is the
		// one the margin check of the second round runs with.
		gone.holdTheNextWait(t)

		refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

		if !poll(30*time.Second, func() bool { return gone.reached(heldAtTheWait) }) {
			t.Fatalf("the refresher did not come to its first sleep within 30s\n%s\nstderr:\n%s", gone.describe(), refresh.errOut())
		}

		if got := len(f.api.minted()); got != 1 {
			t.Fatalf("the first round minted %d token(s), want one: this test needs a token whose margin the clock can run out of\n%s",
				got, gone.describe())
		}

		gone.holdTheNextClock(t)
		gone.letTheWaitGo(t)

		if !poll(30*time.Second, func() bool {
			return gone.reached(heldAtTheClock) && lockHoldsPid(lock, refresh.pid())
		}) {
			t.Fatalf("the refresher did not come to the clock read of its second round with the lock in hand within 30s\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		gone.moveTheLockAside(t)
		gone.holdTheNextWait(t)
		gone.letTheClockGo(t)

		if !poll(30*time.Second, func() bool { return gone.reached(heldAtTheWait) }) {
			t.Fatalf("the refresher did not wait for the lock it holds once it found it missing within 30s\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		// The Mac slept through the wait: the clock is now 3300s, the token's whole hour less its margin, ahead of
		// the reading the refresher took before it waited, and the token it minted is spent. A margin check on that
		// earlier reading still sees the token valid and mints nothing this round; one on the clock of after the
		// wait mints now.
		writeFileMode(t, filepath.Join(gone.dir, "clock-offset"), []byte("3300\n"), 0o600)
		gone.putTheLockBack(t)

		// The loop's own sleep of this round comes next, and the test holds it: the mint the margin check of this
		// round makes is there before the refresher comes to that sleep, while a mint that waits for the next
		// round's clock read - which the offset has moved on too - comes only after it.
		gone.holdTheNextWait(t)
		gone.letTheWaitGo(t)

		if !poll(30*time.Second, func() bool { return gone.reached(heldAtTheWait) }) {
			t.Fatalf("the refresher did not come to the sleep of its round within 30s\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		if got := len(f.api.minted()); got != 2 {
			t.Errorf("%d token(s) were minted, want two: the refresher read the clock before it waited for its lock and never again after the wait, so the margin check of this round saw 3300s of the token it minted left instead of none, and only the next round's clock read minted (issue #288)\n%s\nstderr:\n%s",
				got, gone.describe(), refresh.errOut())
		}

		if !refresh.running() {
			t.Errorf("the refresher exited although the lock it holds came back during its retry\n%s\nstderr:\n%s", gone.describe(), refresh.errOut())
		}

		refresh.signal(t, syscall.SIGTERM)

		if !refresh.waitExit(30 * time.Second) {
			t.Fatalf("the refresher did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
		}

		f.assertNoLeaks(t, refresh.out(), refresh.errOut())
	})

	t.Run("the lock stays gone, and the refresher says so, mints nothing and exits", func(t *testing.T) {
		t.Parallel()

		f := newMintFixture(t)
		env := f.refreshEnv()
		lock := f.sb.refreshLockDir("shift2eu")
		gone := f.sb.useGoneLockShims(t, env, lock)

		gone.holdTheNextClock(t)

		refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

		if !poll(30*time.Second, func() bool {
			return gone.reached(heldAtTheClock) && lockHoldsPid(lock, refresh.pid())
		}) {
			t.Fatalf("the refresher did not come to its clock read with the lock in hand within 30s\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		// The lock goes and never comes back. No hold-wait is armed, so the retry's own sleeps return at once and
		// still_locked runs through all of them before the test reads the refresher's last word.
		gone.moveTheLockAside(t)
		gone.letTheClockGo(t)

		if !refresh.waitExit(30 * time.Second) {
			t.Fatalf("the refresher kept running 30s after its lock went missing and never came back: it holds no lock to mint with, and the retries of still_locked are the only waiting it may do (issue #288)\n%s\nstderr:\n%s",
				gone.describe(), refresh.errOut())
		}

		if status := refresh.exitCode(t); status != 0 {
			t.Errorf("the refresher exited with %d, want 0: a refresher that lost its lock leaves the VM's token file to the refresher that holds one, as a losing refresher does (issue #288)\nstderr:\n%s",
				status, refresh.errOut())
		}

		if got := len(f.api.minted()); got != 0 {
			t.Errorf("%d token(s) were minted: a refresher whose lock is gone holds nothing to mint beside another refresher with (issue #288)\nstderr:\n%s",
				got, refresh.errOut())
		}

		if _, err := os.Stat(lock); err == nil {
			t.Errorf("the lock %s is there again, although the test's copy in %s is the only one that exists: the refresher must not write a lock it does not hold\n%s",
				lock, gone.aside, gone.describe())
		}

		for _, name := range dirNames(f.sb.stateDir()) {
			if strings.Contains(name, ".lock") {
				t.Errorf("the state directory holds %s after the refresher that gave up its lock exited: an orphan lock or half-taken lock outlives it (issue #288)\n%s",
					name, gone.describe())
			}
		}

		// The message must name what is: the lock is gone, nothing holds it, and a refresh must be started again. It
		// must not name another refresher that took it over, which is what a lock in transit and a lost lock look
		// alike for, and it must not print a pid it does not have: "pid none" names no refresher at all.
		assertSaysTheLockIsGone(t, refresh.errOut())

		for _, wrong := range []string{"took it over", "pid none"} {
			if strings.Contains(refresh.errOut(), wrong) {
				t.Errorf("the refresher's stderr says %q although no lock is there and no refresh holds one: it took the missing lock for another refresher's (issue #288)\nstderr:\n%s",
					wrong, refresh.errOut())
			}
		}

		f.assertNoLeaks(t, refresh.out(), refresh.errOut())
	})
}

// TestAgentsAppTokenRefreshDoesNotEchoTheLockFilesPid pins the finding of issue #288's review: the file the lock
// keeps its owner's pid in is a file in the refresh's state directory, and the refresh echoes what it reads there
// into the log a maintainer reads, unvalidated. A pid file holding text instead of a pid - with the escape that
// clears a terminal in it, and long - then reaches that terminal and that log verbatim. The refresh must print no
// owner it did not read as a pid: the word invalid, or no pid at all.
func TestAgentsAppTokenRefreshDoesNotEchoTheLockFilesPid(t *testing.T) {
	t.Parallel()

	f := newMintFixture(t)
	env := f.refreshEnv()

	lock := f.sb.refreshLockDir("shift2eu")

	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatalf("create the lock %s: %v", lock, err)
	}

	// Text that is no pid, with the escape that clears a terminal and a long run of characters after it.
	garbage := "no-pid\x1b[2J\x1b[31m" + strings.Repeat("z", 120) + "\x1b[0m"

	writeFileMode(t, filepath.Join(lock, "pid"), []byte(garbage+"\n"), 0o600)

	refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

	// The refresh gets past the lock it cannot read an owner from: a pid file holding no pid says nothing about a
	// running refresh, so the lock is a stale one to take over - or the refresh stops, which is its choice.
	if !poll(30*time.Second, func() bool {
		line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

		return (ok && line != "") || !refresh.running()
	}) {
		t.Fatalf("the refresh neither took the lock over and wrote a token nor exited within 30s, so this run never reached the word it has for the lock's owner\nstate directory:\n%s\nstderr:\n%s",
			describeTree(f.sb.stateDir()), refresh.errOut())
	}

	out := refresh.out() + refresh.errOut()

	// No part of the pid file reaches the output: not the text, not the long run in it, and no terminal escape out
	// of it.
	if strings.Contains(out, garbage) || strings.Contains(out, strings.Repeat("z", 20)) || strings.Contains(out, "\x1b") {
		t.Errorf("the refresh printed the lock's pid file, escape characters and all, into the output a maintainer reads (issue #288): it echoes what it reads there without checking that it is a pid, so the refresh must print the word invalid, or no pid at all\noutput:\n%q",
			out)
	}

	if refresh.running() {
		refresh.signal(t, syscall.SIGTERM)

		if !refresh.waitExit(30 * time.Second) {
			t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
		}
	}

	f.assertNoLeaks(t, refresh.out(), refresh.errOut())
}

// TestAgentsAppTokenRefreshTakesOverALockWhosePidIsNoNumber pins the third review's finding on the lock's pid
// file: take_lock asks is_refresh whether the pid it read runs a refresh, and is_refresh ends in `ps -p "$1"`,
// which takes a list of pids too ("123,456" and "123 456"). A stale lock whose pid file lists a running refresh's
// pid among others must still be taken over: no refresh owns that lock, so the next refresher takes it and mints,
// as it does a lock whose pid is gone, where a lock read as a running refresh's stays behind and nothing writes
// the VM's token. The script as it stands asks is_number first and kill -0 next, and kill -0 rejects both forms
// on the Mac and in CI, so the pin already holds; what it catches is a change that lets such text read as a
// running refresh's, which is what the guard is there to stop.
func TestAgentsAppTokenRefreshTakesOverALockWhosePidIsNoNumber(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// list writes a pid file holding both pids, in the form this case pins.
		list func(first, second int) string
	}{
		{
			name: "a comma-separated list of pids",
			list: func(first, second int) string { return fmt.Sprintf("%d,%d", first, second) },
		},
		{
			name: "a space-separated list of pids",
			list: func(first, second int) string { return fmt.Sprintf("%d %d", first, second) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()

			// A live process that runs no refresh, and one whose command line is a refresh's: the pid file lists
			// both, so the text ps is handed names a running refresh without being a pid of one.
			bystander := liveNonRefreshPid(t)
			live := standInForARefresh(t, f.sb)

			// The stale lock a refresher killed without its trap leaves: the directory, with a pid file that holds
			// a list instead of one pid.
			lock := f.sb.refreshLockDir("shift2eu")

			if err := os.MkdirAll(lock, 0o700); err != nil {
				t.Fatalf("create the stale lock %s: %v", lock, err)
			}

			pidFile := filepath.Join(lock, "pid")
			listed := tc.list(bystander, live.pid())

			writeFileMode(t, pidFile, []byte(listed+"\n"), 0o600)

			refresh := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, env)

			// What a failure has to show: what the lock holds, what the refresher said and whether it still runs.
			contest := func() string {
				return fmt.Sprintf("the lock's %s holds %q; the refresher (pid %d, running %t) stderr:\n%s\nstate directory:\n%s",
					pidFile, listed, refresh.pid(), refresh.running(), refresh.errOut(), describeTree(f.sb.stateDir()))
			}

			minted := func() bool {
				line, ok := tokenLine(f.sb.vmPath(agentsTokenFile))

				return ok && line != ""
			}

			if !poll(30*time.Second, func() bool { return minted() || !refresh.running() }) {
				t.Fatalf("the refresh neither took the stale lock over and wrote a token nor exited within 30s\n%s", contest())
			}

			// The refresher that read the list must not leave the lock to a refresh that is none: it takes the
			// stale lock over, keeps it and mints, where a refresher that read the list as a running refresh's says
			// another refresh is running and exits.
			if !refresh.running() {
				t.Fatalf("the refresh exited with %d instead of taking over the stale lock %s, whose pid file holds %q: a pid file that is no pid is no running refresh's, so nothing refreshes the VM's token now\n%s",
					refresh.exitCode(t), lock, listed, contest())
			}

			if strings.Contains(refresh.errOut(), "another agents App token refresh") {
				t.Errorf("the refresh said another agents App token refresh is running with the list %q it read from the lock's pid file, so it left a stale lock to a refresh that is none (issue #288)\n%s",
					listed, contest())
			}

			waitFor(t, 30*time.Second, "the refresh that took the stale lock over to write the VM's token", minted)

			if got := len(f.api.minted()); got != 1 {
				t.Errorf("%d token(s) were minted, want one: the refresher that took the stale lock over is the only one that mints\n%s",
					got, contest())
			}

			if !lockHoldsPid(lock, refresh.pid()) {
				t.Errorf("the lock %s does not hold the pid %d of the refresher that took the stale one over\n%s",
					lock, refresh.pid(), contest())
			}

			refresh.signal(t, syscall.SIGTERM)

			if !refresh.waitExit(30 * time.Second) {
				t.Fatalf("the refresh did not stop on SIGTERM; stderr:\n%s", refresh.errOut())
			}

			if _, err := os.Stat(lock); err == nil {
				t.Errorf("the lock %s outlived the refresh that took it over", lock)
			}

			f.assertNoLeaks(t, refresh.out(), refresh.errOut())
		})
	}
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

			// What tells a loaded runner from a takeover neither contender won (issue #240): both scripts'
			// output, whether each still runs, and what the lock directory holds.
			contest := func() string {
				return fmt.Sprintf("first (pid %d, running %t) stderr:\n%s\nsecond (pid %d, running %t) stderr:\n%s\nstate directory:\n%s",
					first.pid(), first.running(), first.errOut(), second.pid(), second.running(), second.errOut(), describeTree(f.sb.stateDir()))
			}

			if !poll(30*time.Second, func() bool { return len(f.api.minted()) >= 1 }) {
				t.Fatalf("timed out after 30s waiting for one of the two refreshers to take the stale lock over and mint\n%s", contest())
			}

			// The contender that lost says so and exits 0; the one that won keeps running. Wait for the exit
			// itself rather than a while: when both win, neither exits and the check below names both.
			if !poll(30*time.Second, func() bool { return !first.running() || !second.running() }) {
				t.Fatalf("both refreshers that found the same stale lock are still running 30s after the first mint: a takeover that two contenders can win puts two loops on one token file (review item 5)\n%s", contest())
			}

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
				t.Fatalf("neither refresher took the stale lock over\n%s", contest())
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

// putBackMVShim stands in for mv in front of the mover's own mv shim: a put-back whose source is a moved-aside lock
// fails, the way a full disk or a permission makes it fail, and every other call is the sandbox's own mv shim, so
// the interleaving of issue #288 still comes from the holds that staged it. SHIFT2EU_TEST_PUTBACK says how the
// put-back fails: once (a transient failure a second attempt gets past), always, or gone (the moved-aside lock
// vanishes too, so neither it nor a lock at the lock's path is left).
const putBackMVShim = `#!/bin/sh
case "${1:-}" in
*/.agents-token-refresh-*.lock.stale.*)
  case "${SHIFT2EU_TEST_PUTBACK:?set SHIFT2EU_TEST_PUTBACK}" in
  once)
    if [ ! -e "${SHIFT2EU_TEST_PUTBACK_DIR:?}/failed-once" ]; then
      : > "${SHIFT2EU_TEST_PUTBACK_DIR}/failed-once"
      echo "mv: cannot move ${1}: failed by the devenv tests" >&2
      exit 1
    fi
    ;;
  always)
    echo "mv: cannot move ${1}: failed by the devenv tests" >&2
    exit 1
    ;;
  gone)
    "${SHIFT2EU_TEST_REAL_MV:?}" "${1}" "${SHIFT2EU_TEST_PUTBACK_DIR:?}/vanished" 2>/dev/null || true
    echo "mv: cannot stat ${1}: no such file or directory" >&2
    exit 1
    ;;
  *)
    echo "the devenv tests know no SHIFT2EU_TEST_PUTBACK=${SHIFT2EU_TEST_PUTBACK}" >&2
    exit 64
    ;;
  esac
  ;;
esac
exec %s "$@"
`

// locksHoldingPid returns the names in dir of the directories whose pid file holds pid: the lock a refresher holds,
// wherever a contender that moved it aside left it (issues #288 and #319).
func locksHoldingPid(dir string, pid int) []string {
	var names []string

	for _, name := range dirNames(dir) {
		if lockHoldsPid(filepath.Join(dir, name), pid) {
			names = append(names, name)
		}
	}

	return names
}

// assertSaysThePutBackFailed fails unless the refresher's output says that the lock it moved aside could not be put
// back: a line about a lock, with one of the words a message about that state uses. The wording is not pinned, only
// that a maintainer reading the run learns that neither a lock at the lock's path nor the one that was moved aside
// is left, rather than seeing a silent takeover of a lock the winner still expected (issue #319).
func assertSaysThePutBackFailed(t *testing.T, text string) {
	t.Helper()

	said := []string{"put back", "put it back", "cannot move", "both", "neither", "moved aside", "restore", "lost"}

	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "lock") {
			continue
		}

		for _, word := range said {
			if strings.Contains(lower, word) {
				return
			}
		}
	}

	t.Errorf("the refresher's output says nothing about the lock it moved aside and could not put back: no line names a lock with one of %s, so a maintainer reads a silent takeover although neither the aside nor a lock at the lock's path is left (issue #319)\noutput:\n%s",
		strings.Join(said, ", "), text)
}

// TestAgentsAppTokenRefreshKeepsTheMovedAsideLockWhenThePutBackFails pins issue #319: in take_lock a contender that
// moved a lock aside puts it back only while no lock exists, and a put-back that fails for another reason
// (permissions, a full disk) was swallowed and followed by the removal of the aside - the very directory that holds
// the winner's lock. The aside must be kept unless a lock exists after the put-back attempt (only then is it a
// duplicate), the winner must keep its lock, and when neither the aside nor the lock is left the refresher says so.
//
// The staging is the three-contender one of issue #288: the winner takes a stale lock over and is held in its loop
// before it mints, and the mover, which read the stale owner before the winner took it, is held after it moved the
// winner's fresh lock aside and before it puts it back. An mv shim on the mover's PATH fails that put-back alone,
// so no test waits on a clock and the test alone decides how it fails.
func TestAgentsAppTokenRefreshKeepsTheMovedAsideLockWhenThePutBackFails(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// mode is how the mv shim fails the put-back, and check pins what the refreshers and the lock's state must
		// show afterwards, while the winner is still held and before it reads its lock again.
		mode  string
		check func(t *testing.T, f *mintFixture, lock string, mover, winner *runningScript)
	}{
		{
			// A put-back that fails once is the transient failure the retry or the next attempt gets past: the
			// winner's lock must be back in place, so the winner goes on holding it and mints.
			name: "the put-back fails once, and the winner keeps its lock",
			mode: "once",
			check: func(t *testing.T, f *mintFixture, lock string, mover, winner *runningScript) {
				t.Helper()

				if !lockHoldsPid(lock, winner.pid()) {
					t.Errorf("the lock %s does not hold the winner's pid %d after the mover's put-back failed once: the aside holds the winner's lock and must be put back, never removed, or the winner's still_locked finds the lock gone and no refresher runs until the next start (issue #319)\nstate directory:\n%s",
						lock, winner.pid(), describeTree(f.sb.stateDir()))
				}

				if mover.running() {
					t.Errorf("the mover (pid %d) still runs although the winner holds the lock %s: the put-back failed once, and a second attempt must find the lock free and put it back, after which the mover loses as it does to any lock another refresher holds (issue #319)\nmover stderr:\n%s",
						mover.pid(), lock, mover.errOut())
				}

				for _, name := range dirNames(f.sb.stateDir()) {
					if strings.Contains(name, ".stale.") {
						t.Errorf("the state directory holds %s after the aside was put back: the aside left over is no duplicate of the lock it was moved from, and nothing removes it (issues #288 and #319)\nstate directory:\n%s",
							name, describeTree(f.sb.stateDir()))
					}
				}
			},
		},
		{
			// A put-back that keeps failing is the disk that stays full: the aside must be kept - it holds the
			// winner's lock, and deleting it is what issue #319 names - and the mover must not take it for its own.
			name: "the put-back keeps failing, and the moved-aside lock is kept",
			mode: "always",
			check: func(t *testing.T, f *mintFixture, lock string, _ *runningScript, winner *runningScript) {
				t.Helper()

				held := locksHoldingPid(f.sb.stateDir(), winner.pid())
				if len(held) == 0 {
					t.Fatalf("no directory in the state directory holds the winner's pid %d after the put-back kept failing: the moved-aside lock was removed, and with it the lock the winner holds, so its still_locked finds it gone and nothing refreshes the VM's token (issue #319)\nstate directory:\n%s",
						winner.pid(), describeTree(f.sb.stateDir()))
				}

				var kept bool

				for _, name := range held {
					if name != filepath.Base(lock) {
						kept = true
					}
				}

				if !kept {
					t.Errorf("only %s holds the winner's pid %d after the put-back kept failing: the moved-aside directory that holds the winner's lock must be kept, since no lock exists at %s (issue #319)\nstate directory:\n%s",
						filepath.Base(lock), winner.pid(), lock, describeTree(f.sb.stateDir()))
				}
			},
		},
		{
			// Neither the aside nor a lock at the lock's path is left: the refresher must say so, rather than take
			// a fresh lock over what a maintainer can no longer find.
			name: "both the lock and the aside are gone, and the refresher says so",
			mode: "gone",
			check: func(t *testing.T, f *mintFixture, _ string, mover, winner *runningScript) {
				t.Helper()

				assertSaysThePutBackFailed(t, mover.out()+mover.errOut())

				if got := locksHoldingPid(f.sb.stateDir(), winner.pid()); len(got) != 0 {
					t.Errorf("%v holds the winner's pid %d although the mv shim took the moved-aside lock away: this case needs neither the lock nor the aside to be there (issue #319)\nstate directory:\n%s",
						got, winner.pid(), describeTree(f.sb.stateDir()))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newMintFixture(t)
			env := f.refreshEnv()
			lock := f.sb.refreshLockDir("shift2eu")

			// The stale lock a refresher killed without its trap leaves: a lock whose pid is alive but runs no refresh.
			if err := os.MkdirAll(lock, 0o700); err != nil {
				t.Fatalf("create the stale lock %s: %v", lock, err)
			}

			writeFileMode(t, filepath.Join(lock, "pid"), []byte(strconv.Itoa(liveNonRefreshPid(t))+"\n"), 0o600)

			race := f.sb.useThreeContendersOnAStaleLock(t, env, lock)

			// The put-back shim in front of the mover's own mv shim: it fails only the move whose source is a
			// moved-aside lock, and the contender shim behind it still forces the interleaving.
			putBack := filepath.Join(race.dir, "putback")

			writeFileMode(t, filepath.Join(putBack, "mv"),
				[]byte(fmt.Sprintf(putBackMVShim, strconv.Quote(filepath.Join(race.dir, "mover", "mv")))), 0o700)

			race.envMover["PATH"] = putBack + ":" + race.envMover["PATH"]
			race.envMover["SHIFT2EU_TEST_PUTBACK"] = tc.mode
			race.envMover["SHIFT2EU_TEST_PUTBACK_DIR"] = putBack
			race.envMover["SHIFT2EU_TEST_REAL_MV"] = realTool(t, "mv")

			contest := func(contenders ...*runningScript) string {
				lines := make([]string, 0, len(contenders)+2)
				for _, c := range contenders {
					lines = append(lines, fmt.Sprintf("pid %d, running %t, stderr:\n%s", c.pid(), c.running(), c.errOut()))
				}

				lines = append(lines, "state directory:", describeTree(f.sb.stateDir()))

				return strings.Join(lines, "\n")
			}

			// The mover starts against the stale lock and is held once it has read the stale owner, before it asks
			// whether that owner runs a refresh: it cannot take the stale lock over from here.
			mover := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, race.envMover)

			if !poll(30*time.Second, func() bool { return race.held(contenderAskedOwnership) }) {
				t.Fatalf("the mover did not read the stale lock's owner within 30s: the steps reached were %s\n%s",
					race.reached(), contest(mover))
			}

			// The winner takes the stale lock over and is held in its loop, with its own lock in hand, before it mints.
			winner := f.sb.start(t, scriptPath(t, agentsMintScript), []string{"refresh"}, race.envWinner)

			if !poll(30*time.Second, func() bool {
				return race.held(contenderReadTheClock) && lockHoldsPid(lock, winner.pid())
			}) {
				t.Fatalf("the winner did not reach its hold in the refresh loop with the stale lock in hand within 30s: the steps reached were %s\n%s",
					race.reached(), contest(mover, winner))
			}

			if minted := len(f.api.minted()); minted != 0 {
				t.Fatalf("%d token(s) were minted before the test released any refresher, so the holds staged no interleaving: the winner must wait in its loop before it mints (issue #319)\n%s",
					minted, contest(mover, winner))
			}

			// The mover takes the stale lock over and moves the winner's fresh lock aside, because the pid it reads
			// there is not the stale owner it read before; it is then held before it puts that lock back.
			race.release(t, holdTheMover)

			if !poll(30*time.Second, func() bool { return race.held(contenderMovedTheLockAside) }) {
				t.Fatalf("the mover did not move the winner's fresh lock aside within 30s: the steps reached were %s\n%s",
					race.reached(), contest(mover, winner))
			}

			if _, err := os.Stat(lock); err == nil {
				t.Fatalf("the lock %s is still in place after the mover moved the winner's lock aside, so the mover took something else: this run forced no put-back (issue #319)\n%s",
					lock, contest(mover, winner))
			}

			// The put-back: the shim fails it, in the way this case names.
			race.release(t, holdThePutBack)

			if !poll(30*time.Second, func() bool { return !mover.running() || lockHoldsPid(lock, mover.pid()) }) {
				t.Fatalf("the mover neither lost the lock nor took a fresh one within 30s after its put-back failed: this run did not reach the state issue #319 is about\n%s",
					contest(mover, winner))
			}

			tc.check(t, f, lock, mover, winner)

			// The winner goes on from its hold with whatever lock it holds now: it mints with its own, or says that
			// it lost it and stops, as a refresher whose lock another one holds does.
			race.release(t, holdTheWinner)

			if !poll(30*time.Second, func() bool { return !winner.running() || len(f.api.minted()) > 0 }) {
				t.Fatalf("the winner neither minted nor stopped 30s after the test let it go\n%s", contest(mover, winner))
			}

			if tc.mode == "once" {
				if !winner.running() {
					t.Errorf("the winner (pid %d) exited although its lock was put back after one failed attempt: a transient failure of the put-back must not cost the only refresher its lock (issue #319)\n%s",
						winner.pid(), contest(mover, winner))
				}

				out := strings.ToLower(winner.errOut())

				for _, wrong := range []string{"is gone", "took it over", "no longer this refresh"} {
					if strings.Contains(out, wrong) {
						t.Errorf("the winner's stderr says %q although its own lock was put back: a put-back that failed once must leave the winner holding the lock it won (issue #319)\nstderr:\n%s",
							wrong, winner.errOut())
					}
				}
			}

			if winner.running() {
				winner.signal(t, syscall.SIGTERM)

				if !winner.waitExit(30 * time.Second) {
					t.Fatalf("the winner did not stop on SIGTERM; stderr:\n%s", winner.errOut())
				}
			}

			if mover.running() {
				mover.signal(t, syscall.SIGTERM)

				if !mover.waitExit(30 * time.Second) {
					t.Fatalf("the mover did not stop on SIGTERM; stderr:\n%s", mover.errOut())
				}
			}

			f.assertNoLeaks(t, mover.out()+winner.out(), mover.errOut()+winner.errOut())
		})
	}
}
