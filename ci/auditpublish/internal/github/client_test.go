package github_test

// Group B of PR 4a: the gh calling contract (ADR 0040, "The publish program's
// GitHub side"). gh runs by its absolute path with the exact arguments and an
// environment of the library's own, nothing inherited; the body reaches it on
// stdin and the status comes from --include's status line on stdout, never from
// its stderr; the output is capped; and a call outside the table runs nothing.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/github"
	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// TestNewClientRefusesAConfigItCannotRunGHWith pins the values NewClient
// refuses as configuration: a gh path or a temporary directory that is not an
// absolute existing one, an empty token, and a scope without this run's
// identity. Each refusal wraps the core's ErrConfig, which the entry point
// prints as "configuration invalid" (exit 9).
func TestNewClientRefusesAConfigItCannotRunGHWith(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// refuse breaks one value of a config that is otherwise valid.
		refuse func(*github.ClientConfig)
	}{
		{name: "a relative gh path", refuse: func(c *github.ClientConfig) { c.GHPath = "bin/gh" }},
		{name: "an empty gh path", refuse: func(c *github.ClientConfig) { c.GHPath = "" }},
		{name: "a gh path that does not exist", refuse: func(c *github.ClientConfig) { c.GHPath = filepath.Join(c.TempDir, "no-such-gh") }},
		{name: "a relative temporary directory", refuse: func(c *github.ClientConfig) { c.TempDir = "tmp" }},
		{name: "an empty temporary directory", refuse: func(c *github.ClientConfig) { c.TempDir = "" }},
		{name: "a temporary directory that does not exist", refuse: func(c *github.ClientConfig) { c.TempDir = filepath.Join(c.TempDir, "missing") }},
		{name: "an empty token", refuse: func(c *github.ClientConfig) { c.Token = github.NewToken("") }},
		{name: "an empty repository", refuse: func(c *github.ClientConfig) { c.Scope.Repository = "" }},
		{name: "a repository without an owner", refuse: func(c *github.ClientConfig) { c.Scope.Repository = "shift2eu" }},
		{name: "an empty run ID", refuse: func(c *github.ClientConfig) { c.Scope.RunID = "" }},
		{name: "a run ID that is not a number", refuse: func(c *github.ClientConfig) { c.Scope.RunID = "12ab" }},
		{name: "a negative run ID", refuse: func(c *github.ClientConfig) { c.Scope.RunID = "-1" }},
		{name: "an empty date", refuse: func(c *github.ClientConfig) { c.Scope.Date = "" }},
		{name: "a date that is not YYYY-MM-DD", refuse: func(c *github.ClientConfig) { c.Scope.Date = "2026-12-3" }},
		{name: "a month outside the year", refuse: func(c *github.ClientConfig) { c.Scope.Date = "2026-13-03" }},
		{name: "a commit that is not 40 hex", refuse: func(c *github.ClientConfig) { c.Scope.Commit = "0123" }},
		{name: "a 64-hex commit", refuse: func(c *github.ClientConfig) { c.Scope.Commit = strings.Repeat("0", 64) }},
		{name: "an uppercase commit", refuse: func(c *github.ClientConfig) { c.Scope.Commit = strings.ToUpper(fixtureCommit) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t)
			cfg := github.ClientConfig{
				GHPath:  fake.Path,
				Token:   github.NewToken(fixtureToken(t)),
				TempDir: t.TempDir(),
				Scope:   testScope(),
			}
			tc.refuse(&cfg)

			if _, err := github.NewClient(cfg); !errors.Is(err, publish.ErrConfig) {
				t.Errorf("NewClient with %s = %v, want an error wrapping publish.ErrConfig", tc.name, err)
			}
		})
	}
}

// TestCallRunsGHWithTheExactArguments pins gh's argv: one shape, gh api with
// the method, --include and the two fixed headers, and --input - only when the
// call sends a body.
func TestCallRunsGHWithTheExactArguments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		request  github.Request
		route    Route
		wantArgs []string
	}{
		{
			name:    "a read",
			request: github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint},
			route:   Route{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}},
			wantArgs: []string{
				"api", "--method", "GET", "--include", "-H", ghAcceptHeader, "-H", ghVersionHeader,
				fixtureReleaseEndpoint,
			},
		},
		{
			name: "a write",
			request: github.Request{
				Method:   "POST",
				Endpoint: fixtureRepoPrefix + "/git/blobs",
				Body:     map[string]string{"content": "aGVsbG8=", "encoding": "utf-8"},
			},
			route: Route{Method: "POST", Endpoint: fixtureRepoPrefix + "/git/blobs", Status: 201, Body: map[string]string{"sha": fixtureSHA}},
			wantArgs: []string{
				"api", "--method", "POST", "--include", "-H", ghAcceptHeader, "-H", ghVersionHeader,
				"--input", "-", fixtureRepoPrefix + "/git/blobs",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, tc.route)
			client := newFixtureClient(t, fake.Path)

			if _, err := client.Call(t.Context(), tc.request); err != nil {
				t.Fatalf("Call: %v", err)
			}

			calls := fake.Calls(t)
			if len(calls) != 1 {
				t.Fatalf("the fake recorded %d calls, want 1", len(calls))
			}

			if diff := cmp.Diff(tc.wantArgs, calls[0].Args); diff != "" {
				t.Errorf("gh's arguments: %s", diff)
			}
		})
	}
}

// TestCallSendsTheWriteBodyOnStdin pins where the body goes: the marshalled
// JSON on stdin, and nothing at all for a read.
func TestCallSendsTheWriteBodyOnStdin(t *testing.T) {
	t.Parallel()

	endpoint := fixtureRepoPrefix + "/git/blobs"
	fake := newFakeGH(t, Route{Method: "POST", Endpoint: endpoint, Status: 201, Body: map[string]string{"sha": fixtureSHA}})
	client := newFixtureClient(t, fake.Path)

	_, err := client.Call(t.Context(), github.Request{
		Method:   "POST",
		Endpoint: endpoint,
		Body:     map[string]string{"content": "aGVsbG8=", "encoding": "utf-8"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	calls := fake.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("the fake recorded %d calls, want 1", len(calls))
	}

	if got, want := string(calls[0].Body), `{"content":"aGVsbG8=","encoding":"utf-8"}`; got != want {
		t.Errorf("stdin held %s, want %s", got, want)
	}
}

// TestCallRunsGHWithItsOwnEnvironment pins gh's environment: exactly the six
// entries of the contract, built by the library, nothing inherited. The fake
// records the environment of a child started with the library's own cmd.Env,
// so a library that inherited the test process's environment (a nil cmd.Env)
// would record every variable of it instead. tests-no-process-state refuses
// t.Setenv in this package (issue #153), so the proof is the exact set: a
// variable set beside it could not reach a child that is given none of them.
func TestCallRunsGHWithItsOwnEnvironment(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, Route{Method: "GET", Endpoint: fixtureReleaseEndpoint, Status: 200, Body: map[string]string{"tag_name": fixtureTag}})
	token := fixtureToken(t)
	tempDir := t.TempDir()
	client := newTestClient(t, github.ClientConfig{
		GHPath:  fake.Path,
		Token:   github.NewToken(token),
		TempDir: tempDir,
		Scope:   testScope(),
	})

	if _, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint}); err != nil {
		t.Fatalf("Call: %v", err)
	}

	calls := fake.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("the fake recorded %d calls, want 1", len(calls))
	}

	env := map[string]string{}

	for _, entry := range calls[0].Env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("an environment entry without '=': %q", entry)
		}

		env[name] = value
	}

	wantNames := []string{"GH_CONFIG_DIR", "GH_NO_UPDATE_NOTIFIER", "GH_PROMPT_DISABLED", "GH_TOKEN", "HOME", "NO_COLOR"}
	if diff := cmp.Diff(wantNames, slices.Sorted(maps.Keys(env))); diff != "" {
		t.Errorf("gh's environment names: %s", diff)
	}

	// The variables an earlier step could set through $GITHUB_ENV reach gh only
	// if the library inherited the process's environment, which the exact set
	// above already refuses; these are the contract's own names.
	for _, name := range []string{"GH_HOST", "GH_DEBUG", "HTTPS_PROXY", "GH_ENTERPRISE_TOKEN"} {
		if _, ok := env[name]; ok {
			t.Errorf("gh's environment holds %s, want it not to reach gh", name)
		}
	}

	configDir := env["GH_CONFIG_DIR"]
	if configDir == "" || env["HOME"] != configDir {
		t.Errorf("GH_CONFIG_DIR = %q and HOME = %q, want one fresh directory for both", configDir, env["HOME"])
	}

	if !strings.HasPrefix(configDir, tempDir+string(os.PathSeparator)) {
		t.Errorf("GH_CONFIG_DIR = %q, want a directory under %q", configDir, tempDir)
	}

	if env["GH_TOKEN"] != token {
		t.Error("GH_TOKEN is not the configured token")
	}

	for _, fixed := range []struct{ name, want string }{
		{name: "GH_NO_UPDATE_NOTIFIER", want: "1"},
		{name: "GH_PROMPT_DISABLED", want: "1"},
		{name: "NO_COLOR", want: "1"},
	} {
		if got := env[fixed.name]; got != fixed.want {
			t.Errorf("%s = %q, want %q", fixed.name, got, fixed.want)
		}
	}
}

// TestCallReadsTheStatusAndBodyFromTheIncludeOutput pins a 2xx answer: the
// status of the status line, the body after the blank line, and the Link header
// beside them.
func TestCallReadsTheStatusAndBodyFromTheIncludeOutput(t *testing.T) {
	t.Parallel()

	const link = `<https://api.github.com/repositories/1/security-advisories?after=Y3Vyc29y>; rel="next"`

	fake := newFakeGH(t, Route{
		Method:   "GET",
		Endpoint: fixtureAdvisoriesEndpoint,
		Status:   200,
		Body:     []string{"first"},
		Link:     link,
	})
	client := newFixtureClient(t, fake.Path)

	resp, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureAdvisoriesEndpoint})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	if resp.Status != 200 {
		t.Errorf("Resp.Status = %d, want 200", resp.Status)
	}

	if got, want := string(resp.Body), `["first"]`; got != want {
		t.Errorf("Resp.Body = %q, want %q", got, want)
	}

	if resp.Link != link {
		t.Errorf("Resp.Link = %q, want %q", resp.Link, link)
	}
}

// TestCallReturnsTheStatusOfAnAbsentResource pins the 404 the caller reads as
// "absent": a status line is an answer, so it is no ErrGitHub; the caller
// decides what 404 means. The fake answers a call no route matches with 404 and
// records it as unmatched.
func TestCallReturnsTheStatusOfAnAbsentResource(t *testing.T) {
	t.Parallel()

	endpoint := fixtureRepoPrefix + "/git/ref/heads/" + fixtureBranch

	fake := newFakeGH(t)
	client := newFixtureClient(t, fake.Path)

	resp, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: endpoint})
	if err != nil {
		t.Fatalf("Call of a 404: %v, want the status without an error", err)
	}

	if resp.Status != 404 {
		t.Errorf("Resp.Status = %d, want 404", resp.Status)
	}

	if unmatched := fake.Unmatched(t); len(unmatched) != 1 {
		t.Errorf("the fake recorded %d unmatched calls, want 1: %+v", len(unmatched), unmatched)
	}
}

// TestCallRefusesAnAnswerWithoutAStatusLine pins a gh that fails with output
// the status cannot be read from: that is a GitHub failure (exit 8), and neither
// its stderr nor its stdio marker reaches the error's text. The program stands
// in for such a gh: it writes no status line and exits 1.
func TestCallRefusesAnAnswerWithoutAStatusLine(t *testing.T) {
	t.Parallel()

	notGH := newFakeGitleaksExit(t, 1)
	client := newFixtureClient(t, notGH.Path)

	_, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint})
	if !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("Call of a gh without a status line = %v, want an error wrapping publish.ErrGitHub", err)
	}

	if strings.Contains(err.Error(), fakeOutputMarker) {
		t.Errorf("the error carries the program's output marker: %v", err)
	}
}

// TestCallRefusesOutputOverTheCap pins the cap: an answer larger than 8 MiB is a
// GitHub failure, never a truncated body.
func TestCallRefusesOutputOverTheCap(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t, Route{
		Method:   "GET",
		Endpoint: fixtureReleaseEndpoint,
		Status:   200,
		Body:     strings.Repeat("a", 9<<20),
	})
	client := newFixtureClient(t, fake.Path)

	_, err := client.Call(t.Context(), github.Request{Method: "GET", Endpoint: fixtureReleaseEndpoint})
	if !errors.Is(err, publish.ErrGitHub) {
		t.Fatalf("Call over the cap = %v, want an error wrapping publish.ErrGitHub", err)
	}
}

// TestCloseRemovesTheTemporaryDirectory pins that a client leaves nothing
// behind: NewClient makes its fresh directory under TempDir, and Close removes
// it.
func TestCloseRemovesTheTemporaryDirectory(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t)
	tempDir := t.TempDir()

	client, err := github.NewClient(github.ClientConfig{
		GHPath:  fake.Path,
		Token:   github.NewToken(fixtureToken(t)),
		TempDir: tempDir,
		Scope:   testScope(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	before, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read %s: %v", tempDir, err)
	}

	if len(before) != 1 || !before[0].IsDir() {
		t.Fatalf("the client's directory under %s holds %+v, want exactly one directory", tempDir, before)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	after, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read %s: %v", tempDir, err)
	}

	if len(after) != 0 {
		t.Errorf("after Close, %s holds %+v, want nothing", tempDir, after)
	}
}

// TestClientFormatsWithoutItsToken pins that the values a program logs are
// safe: a Client holding the token, its dereferenced value, and a Gitleaks
// holding the text it scanned print neither the token nor the text, whatever
// the verb (audit finding 6).
func TestClientFormatsWithoutItsToken(t *testing.T) {
	t.Parallel()

	token := fixtureToken(t)
	client := newTestClient(t, github.ClientConfig{
		GHPath:  newFakeGH(t).Path,
		Token:   github.NewToken(token),
		TempDir: t.TempDir(),
		Scope:   testScope(),
	})

	scanner := newTestGitleaks(t, newFakeGitleaks(t).Path)
	if err := scanner.Scan(t.Context(), gitleaksTextHit); !errors.Is(err, publish.ErrScanRefused) {
		t.Fatalf("Scan of the text the fake gitleaks reports as a leak = %v, want an error wrapping publish.ErrScanRefused", err)
	}

	formatted := []struct {
		what  string
		value any
	}{
		{what: "a *Client", value: client},
		{what: "a Client", value: *client},
		{what: "a *Gitleaks", value: scanner},
		{what: "a Gitleaks", value: *scanner},
	}

	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, f := range formatted {
			got := fmt.Sprintf(format, f.value)

			if strings.Contains(got, token) {
				t.Errorf("%s with %s prints the token: %q", f.what, format, got)
			}
		}
	}

	// A Gitleaks keeps no text it scanned either: the scanner is not a place a
	// secret lives, and printing one cannot leak what was scanned.
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		got := fmt.Sprintf(format, scanner)

		for _, forbidden := range []string{gitleaksTextMarker, fakeGitleaksHit} {
			if strings.Contains(got, forbidden) {
				t.Errorf("a *Gitleaks with %s prints %q, want it to hold none of the text it scanned", format, got)
			}
		}
	}
}

// TestTokenFormatsAsAPlaceholder pins that the token can be formatted and
// logged without its value: Token's own verbs and a ClientConfig holding one
// both print a placeholder.
func TestTokenFormatsAsAPlaceholder(t *testing.T) {
	t.Parallel()

	token := fixtureToken(t)

	for _, format := range []string{"%v", "%s", "%#v", "%+v"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			formatted := []struct {
				what  string
				value any
			}{
				{what: "a Token", value: github.NewToken(token)},
				{what: "a ClientConfig holding the token", value: github.ClientConfig{
					GHPath:  "/usr/bin/gh",
					Token:   github.NewToken(token),
					TempDir: "/tmp",
					Scope:   testScope(),
				}},
			}

			for _, f := range formatted {
				got := fmt.Sprintf(format, f.value)
				if strings.Contains(got, token) {
					t.Errorf("%s with %s prints the token: %q", f.what, format, got)
				}

				if !strings.Contains(got, "redacted") {
					t.Errorf("%s with %s prints %q, want a redacted placeholder", f.what, format, got)
				}
			}
		})
	}
}
