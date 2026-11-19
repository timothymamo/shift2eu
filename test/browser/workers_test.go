//go:build browser && !js

package browser_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// This file holds the worker criterion (PRD #297, Trust: worker-src 'none').
// `new Worker(url)`, `new SharedWorker(url)` and
// `navigator.serviceWorker.register(url)` are Trusted Types TrustedScriptURL
// sinks, so on the built page a string URL throws a TypeError before the
// Content Security Policy checks worker-src, and a test there would pass with
// worker-src deleted. The four attempts therefore run on a test-only copy of the
// assessment page whose policy is the built one with only `trusted-types`
// widened by one policy name the test mints its TrustedScriptURLs through: every
// other directive is asserted byte-identical.

// testPolicy is the Trusted Types policy name the test-only page allows beside
// the page's own.
const testPolicy = "shift2eu-browser-test"

// workerAttempt is one worker the page tried to start.
type workerAttempt struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Outcome string `json:"outcome"`
	Message string `json:"message"`
}

// workerResult is what the page's worker attempts produced, and every violation
// the browser reported while they ran.
type workerResult struct {
	Attempts   []workerAttempt `json:"attempts"`
	Violations []violation     `json:"violations"`
}

// workerAttemptsExpression mints the TrustedScriptURLs through the test policy
// and attempts a dedicated worker from a same-origin URL and from a blob: URL, a
// shared worker and a service worker registration. It waits until one worker-src
// violation per attempt has arrived, so a missing violation fails with what
// came instead rather than with a timeout.
func workerAttemptsExpression(workerURL string) string {
	return fmt.Sprintf(`(async () => {
	const violations = globalThis.__violations;
	const attempts = [];
	const policy = trustedTypes.createPolicy(%s, {
		createHTML: function (html) { return html; },
		createScriptURL: function (url) { return url; },
	});

	function attempt(name, url, run) {
		let outcome = "created";
		let message = "";
		try {
			run();
		} catch (error) {
			outcome = "threw";
			message = String(error !== null && error !== undefined
				? (error.name + ": " + error.message)
				: error);
		}
		attempts.push({name: name, url: url, outcome: outcome, message: message});
	}

	const worker = %s;
	const blob = URL.createObjectURL(new Blob(["postMessage('the worker ran');"], {type: "text/javascript"}));

	attempt("a dedicated worker from the same origin", worker, function () {
		new Worker(policy.createScriptURL(worker));
	});
	attempt("a dedicated worker from a blob: URL", blob, function () {
		new Worker(policy.createScriptURL(blob));
	});
	attempt("a shared worker from the same origin", worker, function () {
		new SharedWorker(policy.createScriptURL(worker));
	});

	let registered = "registered";
	let message = "";
	try {
		await navigator.serviceWorker.register(policy.createScriptURL(worker));
	} catch (error) {
		registered = "rejected";
		message = String(error !== null && error !== undefined
			? (error.name + ": " + error.message)
			: error);
	}
	attempts.push({name: "a service worker registration", url: worker, outcome: registered, message: message});

	function refused() {
		return violations.filter(function (v) { return v.directive === "worker-src"; }).length;
	}

	await new Promise(function (resolve, reject) {
		let frames = 0;
		function frame() {
			if (refused() >= attempts.length) {
				resolve();
				return;
			}
			if (++frames > 300) {
				reject(new Error("the browser reported " + violations.length + " violations, " +
					refused() + " of them worker-src, want one per attempt (" + attempts.length + ")"));
				return;
			}
			requestAnimationFrame(frame);
		}
		frame();
	});

	return {attempts: attempts, violations: violations};
})()`, jsString(testPolicy), jsString(workerURL))
}

// TestWorkersAreBlocked pins the worker criterion (PRD #297, Trust): every one
// of the four worker attempts is refused by worker-src 'none', with no Trusted
// Types error, no worker running and no worker script requested.
func TestWorkersAreBlocked(t *testing.T) {
	t.Parallel()

	s := newSite(t)

	// The worker script: a request for it would show that a worker ran, and the
	// bundle holds no such file.
	s.addScript("worker.js", `postMessage("the worker ran");`)

	s.addHTML("browser-test.html", string(widenTrustedTypes(t, bundleFile(t, "index.html"))))

	ctx := openPage(t, s, "browser-test.html")

	if !eval[bool](t, ctx, collectViolations) {
		t.Fatal("the page's securitypolicyviolation listener could not be installed")
	}

	if !eval[bool](t, ctx, `typeof navigator.serviceWorker === "object" && navigator.serviceWorker !== null`) {
		t.Fatal("navigator.serviceWorker is not defined, so the registration attempt would prove nothing: the origin must be a secure context")
	}

	result := evalAsync[workerResult](t, ctx, workerAttemptsExpression(s.url("worker.js")))

	if len(result.Attempts) != 4 {
		t.Fatalf("the page made %d of the four worker attempts: %v", len(result.Attempts), result.Attempts)
	}

	// Every refusal is a worker-src violation: none came from Trusted Types,
	// and none from another directive.
	refused := 0

	for _, v := range result.Violations {
		if v.Directive == "worker-src" {
			refused++
			continue
		}

		t.Errorf("the browser reported a %q violation while the workers were attempted (%q), want worker-src: the test page's policy differs from the built one in trusted-types alone",
			v.Directive, v.Blocked)
	}

	if refused < len(result.Attempts) {
		t.Errorf("the browser reported %d worker-src violations, want one per attempt (%d): worker-src 'none' refuses every worker",
			refused, len(result.Attempts))
	}

	// No attempt hit Trusted Types: on this page the policy admits the test's
	// policy, so a TrustedScriptURL throws nothing and the CSP is what refuses.
	for _, attempt := range result.Attempts {
		for _, unwanted := range []string{"TrustedScriptURL", "Trusted Types", "requires 'Trusted"} {
			if strings.Contains(attempt.Message, unwanted) {
				t.Errorf("%s (%q) failed with %q, a Trusted Types error, so the CSP was never reached and this test would pass with worker-src deleted",
					attempt.Name, attempt.URL, attempt.Message)
			}
		}
	}

	// No worker ran and no worker script was requested.
	for _, r := range s.log() {
		if strings.HasSuffix(r.Path, "worker.js") {
			t.Errorf("the bundle's origin answered %s %s, want no request: no worker's script may be fetched, so no worker runs", r.Method, r.Path)
		}
	}
}

// widenTrustedTypes returns the built assessment page with only its
// trusted-types directive widened by the test policy's name, and fails the test
// unless every other directive is byte-identical to the built page's.
func widenTrustedTypes(t *testing.T, page []byte) []byte {
	t.Helper()

	const (
		built   = "trusted-types shift2eu-report"
		widened = "trusted-types shift2eu-report " + testPolicy
	)

	if got := strings.Count(string(page), built); got != 1 {
		t.Fatalf("the assessment page carries %q %d times, want once: the policy names the one policy that inserts the report", built, got)
	}

	widenedPage := []byte(strings.Replace(string(page), built, widened, 1))

	before := cspDirectives(policyOf(t, page))
	after := cspDirectives(policyOf(t, widenedPage))

	if got, want := withoutTrustedTypes(after), withoutTrustedTypes(before); !slices.Equal(got, want) {
		t.Fatalf("the test page's policy differs from the built one outside trusted-types:\nbuilt: %q\ntest:  %q", want, got)
	}

	if !slices.Contains(after, widened) {
		t.Fatalf("the test page's policy does not carry %q: %v", widened, after)
	}

	return widenedPage
}

// policyOf returns the Content Security Policy the page carries in its meta
// element.
func policyOf(t *testing.T, page []byte) string {
	t.Helper()

	const pattern = `(?i)<meta http-equiv="Content-Security-Policy" content="([^"]*)"`

	match := regexp.MustCompile(pattern).FindSubmatch(page)
	if match == nil {
		t.Fatalf("the page carries no Content Security Policy in a meta element")
	}

	return string(match[1])
}

// cspDirectives splits a policy into its directives, in the order written.
func cspDirectives(policy string) []string {
	parts := strings.Split(policy, ";")

	out := make([]string, 0, len(parts))

	for _, part := range parts {
		if directive := strings.TrimSpace(part); directive != "" {
			out = append(out, directive)
		}
	}

	return out
}

// withoutTrustedTypes returns the directives except trusted-types, whose value
// the test policy widens.
func withoutTrustedTypes(directives []string) []string {
	out := make([]string, 0, len(directives))

	for _, directive := range directives {
		if name, _, _ := strings.Cut(directive, " "); name == "trusted-types" {
			continue
		}

		out = append(out, directive)
	}

	return out
}
