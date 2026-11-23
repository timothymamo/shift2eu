package github_test

// Smoke tests of the fakes themselves, run directly and not through the
// library: every behaviour test of the GitHub side relies on them answering,
// recording and refusing as fake_test.go says.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const smokeRepo = "repos/example/shift2eu"

// runFake runs a fake by its path with args, stdin and env, and returns its
// stdout, stderr and exit code.
func runFake(t *testing.T, path string, args []string, stdin string, env []string) (string, string, int) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), path, args...)
	// A nil Env would inherit the test's environment; the fakes get exactly
	// env, as the library gives them its own.
	cmd.Env = append([]string{}, env...)
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError

	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &exitErr):
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	default:
		t.Fatalf("run the fake %s: %v", path, err)

		return "", "", -1
	}
}

func ghArgs(method, endpoint string, input bool) []string {
	args := []string{"api", "--method", method, "--include", "-H", ghAcceptHeader, "-H", ghVersionHeader}
	if input {
		args = append(args, "--input", "-")
	}

	return append(args, endpoint)
}

// TestFakeGHAnswersRoutesInOrderAndRecords pins the fake gh's answer: the
// status line, the headers and the body of --include, the routes of one
// endpoint in order with the last repeating, and one record per call with its
// arguments, environment and body.
func TestFakeGHAnswersRoutesInOrderAndRecords(t *testing.T) {
	t.Parallel()

	endpoint := smokeRepo + "/security-advisories?per_page=100"
	fake := newFakeGH(t,
		Route{Method: "GET", Endpoint: endpoint, Status: 200, Body: []string{"first"}, Link: `<https://api.github.com/x?after=abc>; rel="next"`},
		Route{Method: "GET", Endpoint: endpoint, Status: 200, Body: []string{"second"}},
	)

	env := []string{"GH_TOKEN=fake-token-for-tests", "NO_COLOR=1"}

	for i, want := range []string{`["first"]`, `["second"]`, `["second"]`} {
		stdout, _, code := runFake(t, fake.Path, ghArgs("GET", endpoint, false), "", env)
		if code != 0 {
			t.Fatalf("call %d: exit %d, want 0", i, code)
		}

		head, body, ok := strings.Cut(stdout, "\r\n\r\n")
		if !ok {
			t.Fatalf("call %d: output %q has no blank line after its headers", i, stdout)
		}

		if !strings.HasPrefix(head, "HTTP/2.0 200 OK\r\n") {
			t.Errorf("call %d: status line of %q, want HTTP/2.0 200 OK", i, head)
		}

		if gotLink := strings.Contains(head, "\r\nLink: "); gotLink != (i == 0) {
			t.Errorf("call %d: Link header present = %v, want %v", i, gotLink, i == 0)
		}

		if body != want {
			t.Errorf("call %d: body %q, want %q", i, body, want)
		}
	}

	calls := fake.Calls(t)
	if len(calls) != 3 {
		t.Fatalf("the fake recorded %d calls, want 3", len(calls))
	}

	if calls[0].Method != "GET" || calls[0].Endpoint != endpoint || calls[0].Unmatched || calls[0].Violation != "" {
		t.Errorf("the first record is %+v, want GET %s, matched, no violation", calls[0], endpoint)
	}

	if !slices.Equal(calls[0].Env, env) {
		t.Errorf("the recorded environment is %q, want exactly %q", calls[0].Env, env)
	}
}

// TestFakeGHRecordsTheBodyAndAnswersAnError pins a write: the body on stdin is
// recorded, and a non-2xx route exits 1 after writing its answer, as gh api
// --include does.
func TestFakeGHRecordsTheBodyAndAnswersAnError(t *testing.T) {
	t.Parallel()

	endpoint := smokeRepo + "/security-advisories"
	fake := newFakeGH(t, Route{Method: "POST", Endpoint: endpoint, Status: 403, Body: map[string]string{"message": "Resource not accessible by integration"}})

	stdout, stderr, code := runFake(t, fake.Path, ghArgs("POST", endpoint, true), `{"summary":"s","description":"d"}`, nil)
	if code != 1 {
		t.Errorf("exit %d, want 1 for a 403", code)
	}

	if !strings.HasPrefix(stdout, "HTTP/2.0 403 Forbidden\r\n") {
		t.Errorf("stdout %q, want the 403 status line first", stdout)
	}

	if !strings.Contains(stderr, fakeOutputMarker) {
		t.Errorf("stderr %q, want the marker a test looks for in the program's output", stderr)
	}

	calls := fake.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("the fake recorded %d calls, want 1", len(calls))
	}

	var body map[string]string
	if err := json.Unmarshal(calls[0].Body, &body); err != nil || body["summary"] != "s" {
		t.Errorf("the recorded body is %s, want the stdin's JSON", calls[0].Body)
	}
}

// TestFakeGHAnswersAnUnmatchedCall404 pins a call with no route: 404, exit 1,
// recorded as unmatched and no violation.
func TestFakeGHAnswersAnUnmatchedCall404(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t)
	endpoint := smokeRepo + "/git/ref/heads/security-audit/2026-12-03-1234567890"

	stdout, _, code := runFake(t, fake.Path, ghArgs("GET", endpoint, false), "", nil)
	if code != 1 || !strings.HasPrefix(stdout, "HTTP/2.0 404 Not Found\r\n") {
		t.Errorf("exit %d, stdout %q; want exit 1 and a 404 status line", code, stdout)
	}

	if got := fake.Unmatched(t); len(got) != 1 || got[0].Endpoint != endpoint || got[0].Violation != "" {
		t.Errorf("Unmatched() = %+v, want the one call to %s, no violation", got, endpoint)
	}
}

// TestFakeGHGuardRefusesCallsOutsideThePRDsSet pins the fake's own guard: each
// call outside the PRD's fixed set, or with an advisory state other than
// draft, is answered 418 and recorded as a violation, before any route is
// read.
func TestFakeGHGuardRefusesCallsOutsideThePRDsSet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		method   string
		endpoint string
		body     string
	}{
		{name: "an advisory published", method: "PATCH", endpoint: smokeRepo + "/security-advisories/GHSA-2222-3333-4444", body: `{"state":"published"}`},
		{name: "an advisory update without a state", method: "PATCH", endpoint: smokeRepo + "/security-advisories/GHSA-2222-3333-4444", body: `{"summary":"s"}`},
		{name: "an advisory created with a state", method: "POST", endpoint: smokeRepo + "/security-advisories", body: `{"summary":"s","state":"draft"}`},
		{name: "a CVE request", method: "POST", endpoint: smokeRepo + "/security-advisories/GHSA-2222-3333-4444/cve", body: `{}`},
		{name: "a merge", method: "PUT", endpoint: smokeRepo + "/pulls/1/merge", body: `{}`},
		{name: "a review", method: "POST", endpoint: smokeRepo + "/pulls/1/reviews", body: `{"event":"APPROVE"}`},
		{name: "a ref outside security-audit", method: "POST", endpoint: smokeRepo + "/git/refs", body: `{"ref":"refs/heads/main","sha":"x"}`},
		{name: "a tag ref", method: "POST", endpoint: smokeRepo + "/git/refs", body: `{"ref":"refs/tags/v9.9.9","sha":"x"}`},
		{name: "a pull request into another branch", method: "POST", endpoint: smokeRepo + "/pulls", body: `{"base":"release","head":"security-audit/x"}`},
		{name: "a DELETE", method: "DELETE", endpoint: smokeRepo + "/git/refs/heads/security-audit/x"},
		{name: "a release created", method: "POST", endpoint: smokeRepo + "/releases", body: `{}`},
		{name: "an issue comment", method: "POST", endpoint: smokeRepo + "/issues/1/comments", body: `{}`},
		{name: "another family", method: "GET", endpoint: smokeRepo + "/actions/secrets"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeGH(t, Route{Method: tc.method, Endpoint: tc.endpoint, Status: 200, Body: map[string]string{}})
			fake.AllowViolations()

			stdout, _, code := runFake(t, fake.Path, ghArgs(tc.method, tc.endpoint, tc.body != ""), tc.body, nil)
			if code != 1 || !strings.HasPrefix(stdout, "HTTP/2.0 418 ") {
				t.Errorf("exit %d, stdout %q; want exit 1 and the guard's 418", code, stdout)
			}

			calls := fake.Calls(t)
			if len(calls) != 1 || calls[0].Violation == "" {
				t.Errorf("the records are %+v, want one call with a violation", calls)
			}
		})
	}
}

// TestFakeGHGuardRefusesAnotherArgumentShape pins the argument shape the fake
// accepts: gh api with the method, --include and the two headers, nothing
// else.
func TestFakeGHGuardRefusesAnotherArgumentShape(t *testing.T) {
	t.Parallel()

	fake := newFakeGH(t)
	fake.AllowViolations()

	_, _, code := runFake(t, fake.Path, []string{"api", smokeRepo + "/pulls", "--paginate"}, "", nil)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}

	if calls := fake.Calls(t); len(calls) != 1 || calls[0].Violation == "" {
		t.Errorf("the records are %+v, want one call with a violation", calls)
	}
}

// TestFakeGitleaksExitsOnTheHitAndRecords pins the fake gitleaks: exit 10 on
// fakeGitleaksHit, 0 otherwise, a fixed exit when the test set one, and a
// record of each call's stdin.
func TestFakeGitleaksExitsOnTheHitAndRecords(t *testing.T) {
	t.Parallel()

	fake := newFakeGitleaks(t)

	if _, _, code := runFake(t, fake.Path, []string{"stdin"}, "a clean text", nil); code != 0 {
		t.Errorf("a clean text: exit %d, want 0", code)
	}

	stdout, stderr, code := runFake(t, fake.Path, []string{"stdin"}, "a text with "+fakeGitleaksHit, nil)
	if code != fakeGitleaksLeakExit {
		t.Errorf("a hit: exit %d, want %d", code, fakeGitleaksLeakExit)
	}

	if !strings.Contains(stdout, fakeOutputMarker) || !strings.Contains(stderr, fakeOutputMarker) {
		t.Errorf("stdout %q, stderr %q; want the marker in both", stdout, stderr)
	}

	calls := fake.Calls(t)
	if len(calls) != 2 || calls[1].Stdin != "a text with "+fakeGitleaksHit {
		t.Errorf("the records are %+v, want two calls, the second with the hit", calls)
	}

	fixed := newFakeGitleaksExit(t, 2)
	if _, _, code := runFake(t, fixed.Path, []string{"stdin"}, "a clean text", nil); code != 2 {
		t.Errorf("a fixed exit: exit %d, want 2", code)
	}
}
