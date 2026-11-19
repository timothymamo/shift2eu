//go:build browser && !js

package browser_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// This file holds Layer 2 of the trust model (PRD #297, Trust: nothing leaves
// the browser) as the browser enforces it: every connection, every image and
// every CSS URL the page tries is refused by the Content Security Policy, and
// the only requests the origin answers are the bundle's own files. It also
// records the routes the policy does not govern (navigation, WebRTC), which
// layer 1 closes and which the test pins as *not* blocked so no statement
// pretends otherwise.

// violation is one Content Security Policy violation the page reported.
type violation struct {
	// Directive is the effective directive that refused the attempt.
	Directive string `json:"directive"`
	// Violated is the directive as the policy wrote it.
	Violated string `json:"violated"`
	// Blocked is the blocked URI or resource.
	Blocked string `json:"blockedURI"`
	// Disposition is the policy's disposition.
	Disposition string `json:"disposition"`
}

// collectViolations installs the page's one securitypolicyviolation listener,
// which records every refusal the browser reports. Nothing is inferred: the
// directive the test asserts on is the one the browser names.
const collectViolations = `(() => {
	globalThis.__violations = [];
	document.addEventListener("securitypolicyviolation", function (event) {
		globalThis.__violations.push({
			directive: event.effectiveDirective,
			violated: event.violatedDirective,
			blockedURI: event.blockedURI,
			disposition: event.disposition,
		});
	});
	return true;
})()`

// attempt is one network attempt the page made.
type attempt struct {
	// Kind is what was attempted: a fetch, an XMLHttpRequest, an Image, an
	// inserted <img>, or a background-image built through CSSOM.
	Kind string `json:"kind"`
	// URL is the address the attempt named.
	URL string `json:"url"`
	// Outcome is "blocked" when the API raised the failure a refused request
	// raises, and "sent" when it returned as if the request had gone out.
	Outcome string `json:"outcome"`
	// Message is what the failure said.
	Message string `json:"message"`
}

// networkAttemptsExpression returns the in-page attempts: a fetch and an
// XMLHttpRequest to another origin, to an own-origin path and to the module
// script's own URL; an Image with its src set and an inserted <img>, each to an
// own-origin path and to another origin; and a background-image in a style rule
// built through CSSOM, each to an own-origin path and to another origin.
func networkAttemptsExpression(localOrigin, otherOrigin, moduleURL string) string {
	return fmt.Sprintf(`(async () => {
	const attempts = [];

	// record runs one attempt. The violation it causes is reported
	// asynchronously, so the test waits for the violations from the outside and
	// matches them by the URL each attempt named: a frame loop in the page is
	// no good, since a browser throttles animation frames in a tab that is not
	// shown.
	async function record(kind, url, run) {
		let outcome = "sent";
		let message = "";
		try {
			await run();
		} catch (error) {
			outcome = "blocked";
			message = String(error !== null && error !== undefined && error.message !== undefined ? error.message : error);
		}
		attempts.push({kind: kind, url: url, outcome: outcome, message: message});
	}

	function xhr(url) {
		return new Promise(function (resolve, reject) {
			const request = new XMLHttpRequest();
			request.open("GET", url);
			request.onload = function () { resolve(); };
			request.onerror = function () { reject(new Error("the request's error event")); };
			request.send();
		});
	}

	function image(url) {
		return new Promise(function (resolve, reject) {
			const img = new Image();
			img.onload = function () { resolve(); };
			img.onerror = function () { reject(new Error("the image's error event")); };
			img.src = url;
		});
	}

	// An <img> inserted with createElement and a src assignment: innerHTML
	// would be a Trusted Types sink and throw before the CSP saw the load.
	function insertedImage(url) {
		return new Promise(function (resolve, reject) {
			const img = document.createElement("img");
			img.onload = function () {
				img.remove();
				resolve();
			};
			img.onerror = function () {
				img.remove();
				reject(new Error("the image's error event"));
			};
			img.src = url;
			document.body.appendChild(img);
		});
	}

	// A background-image built through CSSOM: a constructable stylesheet is not
	// an inline style, so style-src does not refuse the rule and img-src
	// governs the load it starts. Each attempt keeps its own element and its own
	// sheet, so a later rule cannot cancel a load the browser has not started
	// yet.
	function background(url) {
		const probe = document.createElement("div");
		probe.className = "probe";
		probe.setAttribute("data-probe", String(attempts.length));
		document.body.appendChild(probe);

		const sheet = new CSSStyleSheet();
		sheet.insertRule('div.probe[data-probe="' + attempts.length + '"] { width: 16px; height: 16px; background-image: url("' + url + '"); }', 0);
		document.adoptedStyleSheets = document.adoptedStyleSheets.concat([sheet]);
	}

	const local = %s;
	const other = %s;
	const module = %s;

	await record("fetch", other + "/other-origin-path", function () { return fetch(other + "/other-origin-path"); });
	await record("fetch", local + "/own-origin-path", function () { return fetch(local + "/own-origin-path"); });
	await record("fetch", module, function () { return fetch(module); });
	await record("xmlhttprequest", other + "/other-origin-path", function () { return xhr(other + "/other-origin-path"); });
	await record("xmlhttprequest", local + "/own-origin-path", function () { return xhr(local + "/own-origin-path"); });
	await record("image", other + "/other-origin-image.png", function () { return image(other + "/other-origin-image.png"); });
	await record("image", local + "/own-origin-image.png", function () { return image(local + "/own-origin-image.png"); });
	await record("inserted image", other + "/other-origin-image.png", function () { return insertedImage(other + "/other-origin-image.png"); });
	await record("inserted image", local + "/own-origin-image.png", function () { return insertedImage(local + "/own-origin-image.png"); });
	await record("background image", other + "/other-origin-background.png", function () { return background(other + "/other-origin-background.png"); });
	await record("background image", local + "/own-origin-background.png", function () { return background(local + "/own-origin-background.png"); });

	return attempts;
})()`, jsString(localOrigin), jsString(otherOrigin), jsString(moduleURL))
}

// remoteLog records the responses the browser's DevTools reports. A request the
// CSP refuses never gets one, so the log is what the browser's network stack
// actually fetched.
type remoteLog struct {
	mu   sync.Mutex
	urls []string
}

// add records one response URL.
func (l *remoteLog) add(url string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.urls = append(l.urls, url)
}

// snapshot returns the URLs recorded so far.
func (l *remoteLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.urls)
}

// sameURL reports whether the blocked URI the policy named is the URL the
// attempt used: the browser resolves a relative URL, so one is a prefix of the
// other when the attempt built it from the origin.
func sameURL(blocked, tried string) bool {
	return blocked != "" && (blocked == tried || strings.HasPrefix(tried, blocked) || strings.HasPrefix(blocked, tried))
}

// waitForViolations waits until the page has reported at least want CSP
// violations, and returns them. On a timeout it reports every attempt and every
// violation seen, which is what says which attempt went unrefused.
func waitForViolations(t *testing.T, ctx context.Context, want int, attempts []attempt) []violation {
	t.Helper()

	_, err := chromedp.Run(ctx, chromedp.Poll[bool](fmt.Sprintf(`globalThis.__violations.length >= %d`, want),
		chromedp.WithPollingTimeout(60*time.Second)))

	violations := eval[[]violation](t, ctx, `globalThis.__violations`)

	if err != nil {
		t.Fatalf("waiting for %d CSP violations, one per network attempt: %v\nthe page reported %d: %v\nthe attempts: %v",
			want, err, len(violations), violations, attempts)
	}

	return violations
}

// TestNetworkIsBlocked pins the Layer 2 network criterion (PRD #297, Trust):
// from the loaded page, a fetch and an XMLHttpRequest to another origin, to an
// own-origin path and to the module script's own URL are all refused by
// connect-src 'none'; an Image with its src set, an inserted <img> and a
// CSSOM-built background-image, own-origin and cross-origin, are all refused by
// img-src 'none'; and the requests the origin answered and the browser's
// DevTools reported are only the page, its stylesheet and its scripts, plus at
// most a browser-initiated GET /favicon.ico with no query answered 404.
func TestNetworkIsBlocked(t *testing.T) {
	t.Parallel()

	local := newSite(t)
	other := newSite(t)

	ctx := newTab(t)

	// The DevTools log is subscribed and enabled before the page loads, so the
	// page's own requests are in it too.
	log := &remoteLog{}
	responses := chromedp.Events(ctx, network.ResponseReceived)

	go func() {
		for event, err := range responses {
			if err != nil {
				return
			}

			log.add(event.Response.URL)
		}
	}()

	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		t.Fatalf("enable the Network domain: %v", err)
	}

	do(t, ctx, chromedp.Navigate(local.url("index.html")))
	waitFor(t, ctx, "the assessment module to load", `(() => {
		const run = document.getElementById("run");
		return run !== null && !run.disabled;
	})()`)

	if !eval[bool](t, ctx, collectViolations) {
		t.Fatal("the page's securitypolicyviolation listener could not be installed")
	}

	moduleURL := eval[string](t, ctx, `(() => {
		const scripts = Array.from(document.querySelectorAll("script[src]"));
		const module = scripts.find(function (script) {
			return script.getAttribute("src").indexOf("module.") === 0;
		});
		return module === undefined ? "" : module.src;
	})()`)

	if moduleURL == "" {
		t.Fatal("the page loads no module script, so the attempt against the module's own URL would prove nothing")
	}

	attempts := evalAsync[[]attempt](t, ctx, networkAttemptsExpression(local.server.URL, other.server.URL, moduleURL))

	if len(attempts) != 11 {
		t.Fatalf("the page made %d of the eleven network attempts, want all of them: %v", len(attempts), attempts)
	}

	// The violations arrive asynchronously, one per attempt, so the test waits
	// for them outside the page and then matches each to its attempt.
	violations := waitForViolations(t, ctx, len(attempts), attempts)

	if len(violations) != len(attempts) {
		t.Errorf("the browser reported %d violations for %d attempts, want one per attempt: %v", len(violations), len(attempts), violations)
	}

	wantDirective := map[string]string{
		"fetch":            "connect-src",
		"xmlhttprequest":   "connect-src",
		"image":            "img-src",
		"inserted image":   "img-src",
		"background image": "img-src",
	}

	for _, a := range attempts {
		want, ok := wantDirective[a.Kind]
		if !ok {
			t.Fatalf("the test knows no directive for the %q attempt", a.Kind)
		}

		refused := false

		for _, v := range violations {
			if !sameURL(v.Blocked, a.URL) {
				continue
			}

			refused = true

			if v.Directive != want {
				t.Errorf("the page's %s to %q was refused by %q, want %q", a.Kind, a.URL, v.Directive, want)
			}
		}

		if !refused {
			t.Errorf("the page's %s to %q produced no CSP violation (outcome %q, %q), want %q to refuse it: the violations seen were %v",
				a.Kind, a.URL, a.Outcome, a.Message, want, violations)
		}

		// Every API but CSSOM raises the failure a refused request raises.
		if a.Kind != "background image" && a.Outcome != "blocked" {
			t.Errorf("the page's %s to %q returned as if it had been sent (%q), want the failure a request the policy refuses raises",
				a.Kind, a.URL, a.Message)
		}
	}

	// The other origin must have answered nothing at all.
	if got := other.log(); len(got) != 0 {
		t.Errorf("the other origin answered %d requests (%v), want none: nothing from the page may reach it", len(got), got)
	}

	allowed := map[string]bool{"/index.html": true}

	for _, asset := range eval[[]string](t, ctx, `Array.from(document.querySelectorAll("link[rel=stylesheet], script[src]")).
		map(function (el) { return el.getAttribute("href") || el.getAttribute("src"); })`) {
		allowed["/"+strings.TrimPrefix(asset, "./")] = true
	}

	for _, request := range local.log() {
		switch {
		case request.Method == "GET" && allowed[request.Path] && request.Query == "":
		case request.Method == "GET" && request.Path == "/favicon.ico" && request.Query == "" && request.Status == 404:
		default:
			t.Errorf("the bundle's origin answered %s %s?%s with %d, which is not one of the page, its stylesheet or its scripts, nor the browser's own favicon request: the page sent something",
				request.Method, request.Path, request.Query, request.Status)
		}
	}

	for name := range allowed {
		if !slices.ContainsFunc(local.log(), func(r request) bool { return r.Path == name }) {
			t.Errorf("the origin never answered a request for %q: the page did not load a file it shows", name)
		}
	}

	// The browser's own DevTools events agree: only the bundle's files were
	// fetched and answered.
	for _, url := range log.snapshot() {
		if !strings.HasPrefix(url, local.server.URL) {
			t.Errorf("the browser's DevTools reported a response from %q, which is not the bundle's origin: the page fetched something elsewhere", url)
			continue
		}

		path := strings.TrimPrefix(url, local.server.URL)
		if allowed[path] || path == "/favicon.ico" {
			continue
		}

		t.Errorf("the browser's DevTools reported a response for %q, which is not one of the page, its stylesheet or its scripts", path)
	}
}

// TestCSPGapsAreRecordedNotBlocked pins the gaps criterion (PRD #297, Trust):
// a navigation and an RTCPeerConnection started from the page are NOT blocked
// by the Content Security Policy. They are recorded, so the ADR, the privacy
// page and this test agree that layer 1 closes them, and no statement claims
// the CSP covers them.
func TestCSPGapsAreRecordedNotBlocked(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	s.addHTML("navigated.html", `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>navigated</title></head>
<body><p id="navigated">the page navigated</p></body>
</html>`)

	ctx := openAssessment(t, s)

	if !eval[bool](t, ctx, collectViolations) {
		t.Fatal("the page's securitypolicyviolation listener could not be installed")
	}

	// WebRTC: the CSP governs no peer connection, and the page's own code is
	// what has to refuse it (the banned-API scan of the page script and of the
	// glue).
	offer := evalAsync[string](t, ctx, `(async () => {
		const connection = new RTCPeerConnection();
		connection.createDataChannel("probe");
		const description = await connection.createOffer();
		connection.close();
		return description.type;
	})()`)

	if offer != "offer" {
		t.Errorf("the page's RTCPeerConnection produced an offer of type %q, want %q: the CSP does not block WebRTC, which is why layer 1 does", offer, "offer")
	}

	if violations := eval[[]violation](t, ctx, `globalThis.__violations`); len(violations) != 0 {
		t.Errorf("the RTCPeerConnection produced the CSP violations %v, want none: the policy governs no peer connection", violations)
	}

	// A navigation: no navigate-to directive, so the page leaves. A poll does
	// not survive a navigation, so the load event is what the test waits for.
	loads := chromedp.Events(ctx, page.LoadEventFired)

	eval[bool](t, ctx, `(() => {
		window.location.href = "/navigated.html";
		return true;
	})()`)

	for range loads { //nolint:errcheck // the loop's first value is the only load event the wait needs; its error can be none, since the click navigated
		break
	}

	if got := eval[string](t, ctx, `String(document.URL)`); !strings.Contains(got, "/navigated.html") {
		t.Errorf("the page is at %q after the navigation, want /navigated.html: the CSP has no navigate-to and blocks no navigation", got)
	}

	if !slices.ContainsFunc(s.log(), func(r request) bool { return r.Path == "/navigated.html" }) {
		t.Errorf("the bundle's origin never answered a request for /navigated.html, want the navigation to happen: the CSP does not block navigation, which is why layer 1 does (the page's script navigates nowhere)")
	}
}
