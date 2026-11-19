//go:build browser

package browser_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// This file holds what the page is, apart from any assessment: the secure
// context the unregister function and the worker attempts rest on, the
// module's lifecycle (its one global, the loading notice, the no-WebAssembly
// message), the version the page shows, and the assessment that runs with the
// network disabled.

// TestSecureContext pins the maintainer's decision: the bundle runs in a secure
// context (a loopback origin is one, localhost too, and so is the site), which
// is what makes navigator.serviceWorker exist at all, so the worker attempts
// cannot pass vacuously and the unregister function has something to unregister.
func TestSecureContext(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	if !eval[bool](t, ctx, `window.isSecureContext === true`) {
		t.Errorf("window.isSecureContext is false on %s, but the bundle must run in a secure context: a loopback origin is one, and so is https://www.shift2eu.com", s.url("index.html"))
	}

	if !eval[bool](t, ctx, `typeof navigator.serviceWorker === "object" && navigator.serviceWorker !== null`) {
		t.Errorf("navigator.serviceWorker is not defined on %s, so this is not a secure context and the unregister function and the worker attempts would prove nothing", s.url("index.html"))
	}
}

// TestModuleLifecycle pins what the page does with the module's global (PRD
// #297, Layer 2): it reads globalThis.__shift2euModule once, checks its type,
// deletes it before any report is inserted, and replaces the static loading
// notice once the module has been instantiated.
func TestModuleLifecycle(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	if got := eval[string](t, ctx, `typeof globalThis.__shift2euModule`); got != "undefined" {
		t.Errorf("globalThis.__shift2euModule is %s after load, want undefined: the page reads the global once and deletes it, so no report element can shadow it later", got)
	}

	static := staticNotice(t)
	shown := eval[string](t, ctx, `(() => {
		const notice = document.getElementById("loading-notice");
		return notice === null ? "" : notice.textContent.trim();
	})()`)

	if shown == "" {
		t.Fatalf("the page holds no loading-notice element after load, want it replaced by the module's own message")
	}

	if shown == static {
		t.Errorf("the loading notice still reads %q after the module was instantiated, want it replaced", shown)
	}

	if !eval[bool](t, ctx, `!document.getElementById("run").disabled`) {
		t.Error("the run button is still disabled after the module was instantiated, so the page's controls were not wired")
	}
}

// staticNotice returns the loading notice's text as the built page carries it
// in the HTML itself, before any script runs.
func staticNotice(t *testing.T) string {
	t.Helper()

	const pattern = `(?s)<p id="loading-notice"[^>]*>(.*?)</p>`

	match := regexp.MustCompile(pattern).FindStringSubmatch(string(bundleFile(t, "index.html")))
	if match == nil {
		t.Fatalf("the built assessment page holds no <p id=\"loading-notice\">: the static loading notice is part of the HTML, so no script delays it")
	}

	return strings.TrimSpace(match[1])
}

// TestNoWebAssembly pins the message a browser without WebAssembly shows (PRD
// #297, Page content): the page script shows the same message the <noscript>
// element carries, before it reads the module, and wires no control.
func TestNoWebAssembly(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := newTab(t)

	// The script runs before the page's own scripts, so globalThis has no
	// WebAssembly when they read it: it is the only way to test that branch
	// without a second browser.
	if _, err := chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: `delete globalThis.WebAssembly;`,
	}); err != nil {
		t.Fatalf("install the script that removes WebAssembly: %v", err)
	}

	do(t, ctx, chromedp.Navigate(s.url("index.html")))
	waitFor(t, ctx, "the no-WebAssembly message", `(() => {
		const notice = document.getElementById("loading-notice");
		return notice !== null && notice.textContent.indexOf("WebAssembly") >= 0;
	})()`)

	message := eval[string](t, ctx, `document.getElementById("loading-notice").textContent.replace(/\s+/g, " ")`)

	for _, want := range []string{"WebAssembly", "CLI"} {
		if !strings.Contains(message, want) {
			t.Errorf("the no-WebAssembly message %q does not name %q: without WebAssembly the page says what the assessment needs and points to the CLI, as the <noscript> message does", message, want)
		}
	}

	if !eval[bool](t, ctx, `document.getElementById("run").disabled`) {
		t.Error("the run button is enabled although the browser has no WebAssembly: the page cannot run an assessment it cannot start")
	}
}

// TestGoVersionIsShown pins the version line the page shows (PRD #297, Page
// content): the version the bundle was built as, which is what a user checks a
// release against, and the Go version the module was built with, which the
// release pins through go.mod.
func TestGoVersionIsShown(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	shown := eval[string](t, ctx, `document.getElementById("go-version").textContent.trim()`)
	want := goEnv(t, "GOVERSION")

	if shown != want {
		t.Errorf("the page shows the module built with Go %q, want %q, the toolchain this module builds with (go.mod pins it, and the release sets up Go from it)", shown, want)
	}

	version := manifestOf(t).Version
	footer := eval[string](t, ctx, `document.querySelector("footer").textContent.replace(/\s+/g, " ")`)

	if !strings.Contains(footer, "shift2eu "+version) {
		t.Errorf("the page's footer %q does not show the version manifest.json names, %q: every page shows the version that built it", footer, version)
	}
}

// TestOfflineAssessment pins that the assessment needs no network once the page
// is loaded (PRD #297, Size and speed): the module is in the page, the
// inventories are in the page, and the report is rendered in the page, so an
// assessment runs with the browser offline.
func TestOfflineAssessment(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	// The modern form of Network.emulateNetworkConditions, which CDP no longer
	// carries: a global rule drops every request, and the navigator state is
	// overridden so the page sees itself offline too.
	run(t, ctx, chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(ctx, network.EmulateNetworkConditionsByRule, network.EmulateNetworkConditionsByRuleParams{
			MatchedNetworkConditions: []*network.Conditions{
				{URLPattern: "", Latency: 0, DownloadThroughput: -1, UploadThroughput: -1, Offline: true},
			},
		})
		if err != nil {
			return err
		}

		_, err = chromedp.Call(ctx, network.OverrideNetworkState, network.OverrideNetworkStateParams{
			Offline: true, Latency: 0, DownloadThroughput: -1, UploadThroughput: -1,
		})

		return err
	}))

	waitFor(t, ctx, "the browser to report itself offline", `navigator.onLine === false`)

	before := len(s.log())

	loadExample(t, ctx, "aws")
	setTargets(t, ctx, "ovhcloud")
	runAssessment(t, ctx)

	if message := alertOf(t, ctx).Text; message != "" {
		t.Fatalf("the page shows the error %q while offline, want the report: the assessment runs in the page and needs no network", message)
	}

	if report := reportOf(t, ctx); !report.hasSection("Summary") {
		t.Fatalf("the page shows no report while offline: %v", report.Sections)
	}

	if after := s.log(); len(after) != before {
		t.Errorf("the origin answered %d requests while the browser was offline (%v), want none: the page sent nothing", len(after)-before, after[before:])
	}
}
