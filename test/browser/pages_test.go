//go:build browser && !js

package browser_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// This file holds the pages around the assessment: the framing guard and the
// opener (PRD #297, Layer 1), and the privacy and verification pages (the
// Privacy statement criterion): the downgrade-risk statement is shown, the HSTS
// waiver statement is shown when the record says waived, those two pages load
// only the page script, and their links work.

// marker is what the test reads of a statement the PRD requires to be shown.
type marker struct {
	Exists  bool    `json:"exists"`
	Visible bool    `json:"visible"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Text    string  `json:"text"`
}

// markerOf reads the element with the id from the page.
func markerOf(t *testing.T, ctx context.Context, id string) marker {
	t.Helper()

	return eval[marker](t, ctx, fmt.Sprintf(`(() => {
		const el = document.getElementById(%s);
		if (el === null) {
			return {exists: false, visible: false, width: 0, height: 0, text: ""};
		}
		const box = el.getBoundingClientRect();
		return {
			exists: true,
			visible: el.checkVisibility(),
			width: box.width,
			height: box.height,
			text: el.textContent.trim(),
		};
	})()`, jsString(id)))
}

// assertMarkerShown pins a statement the PRD requires to be shown: the element
// is there, it has text, checkVisibility() is true and its box has a width and
// a height, which is the browser's own answer to "is this shown".
func assertMarkerShown(t *testing.T, ctx context.Context, page, id string) {
	t.Helper()

	got := markerOf(t, ctx, id)

	switch {
	case !got.Exists:
		t.Errorf("%s holds no element with id %q: the statement has a stable marker and is shown by default", page, id)
	case strings.TrimSpace(got.Text) == "":
		t.Errorf("%s's element with id %q holds no text: it is a statement, so it says something", page, id)
	case !got.Visible:
		t.Errorf("%s's element with id %q is not visible: checkVisibility() is false", page, id)
	case got.Width == 0 || got.Height == 0:
		t.Errorf("%s's element with id %q has a box of %vx%v, want a non-zero width and height: it is shown", page, id, got.Width, got.Height)
	}
}

// TestOpenerIsDroppedOnLoad pins the framing guard's second half (PRD #297,
// Layer 1, and the *browser* clause of that criterion): a page opened by
// another page with window.open has window.opener === null after load, so the
// page drops its own handle to its opener as defence in depth behind the
// opener ban. The opener keeps its handle to the page, which the mitigation
// cannot close (no COOP) and which the privacy page states.
func TestOpenerIsDroppedOnLoad(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	s.addHTML("opener.html", `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>opener</title></head>
<body>
<p id="state">not opened</p>
<button type="button" id="open">open the assessment page</button>
<script>
document.getElementById("open").addEventListener("click", function () {
	globalThis.__opened = window.open("index.html", "_blank");
	document.getElementById("state").textContent = globalThis.__opened === null ? "blocked" : "opened";
});
</script>
</body>
</html>`)

	ctx := openPage(t, s, "opener.html")
	do(t, ctx, chromedp.Click(chromedp.ID("open")))

	if eval[bool](t, ctx, `globalThis.__opened === null`) {
		t.Fatalf("the browser refused window.open from a click, so the assessment page never opened and this check would prove nothing")
	}

	waitFor(t, ctx, "the opened page to drop its opener", `(() => {
		const opened = globalThis.__opened;
		if (opened === null || opened.closed) { return false; }
		if (opened.document.readyState !== "complete") { return false; }
		return opened.opener === null;
	})()`)

	// It is the assessment page that opened, and no frame guard stopped it.
	state := eval[struct {
		Page   string `json:"page"`
		Opener string `json:"opener"`
		Run    int    `json:"run"`
	}](t, ctx, `(() => {
		const opened = globalThis.__opened;
		return {
			page: opened.document.body === null ? "" : String(opened.document.body.getAttribute("data-page")),
			opener: String(opened.opener),
			run: opened.document.querySelectorAll("#run").length,
		};
	})()`)

	if state.Page != "assessment" || state.Run != 1 {
		t.Errorf("the opened page is not the assessment page (data-page %q, %d run buttons): %+v", state.Page, state.Run, state)
	}

	if state.Opener != "null" {
		t.Errorf("the opened page's window.opener is %q after load, want null: the framing guard sets it to null on load", state.Opener)
	}
}

// TestFramingStopsThePage pins the framing guard (PRD #297, Layer 1): a meta
// Content Security Policy cannot carry frame-ancestors, so the page script
// stops when the page is framed, and shows why.
func TestFramingStopsThePage(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	s.addHTML("frame.html", `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>frame</title></head>
<body><iframe id="frame" src="index.html" title="the assessment page"></iframe></body>
</html>`)

	ctx := openPage(t, s, "frame.html")

	waitFor(t, ctx, "the framed page to stop and say why", `(() => {
		const frame = document.getElementById("frame");
		const doc = frame === null ? null : frame.contentDocument;
		if (doc === null || doc.body === null) { return false; }
		return doc.body.textContent.indexOf("refuses to run inside a frame") >= 0;
	})()`)

	state := eval[struct {
		Text     string `json:"text"`
		Children int    `json:"children"`
		Controls int    `json:"controls"`
	}](t, ctx, `(() => {
		const doc = document.getElementById("frame").contentDocument;
		return {
			text: doc.body.textContent.replace(/\s+/g, " ").trim(),
			children: doc.body.children.length,
			controls: doc.querySelectorAll("input, button, select, textarea").length,
		};
	})()`)

	if state.Children != 0 || state.Controls != 0 {
		t.Errorf("the framed page left %d elements and %d controls in its body, want none: it stops and says why", state.Children, state.Controls)
	}

	if !strings.Contains(state.Text, "www.shift2eu.com") {
		t.Errorf("the framed page's message %q does not say where to open the page instead", state.Text)
	}
}

// TestPrivacyAndVerificationPages pins the privacy and verification criteria
// (PRD #297, Privacy statement): the downgrade-risk statement is shown on the
// privacy page, the HSTS waiver statement is shown on both pages when the
// record says waived (a build without the record is not waived), those pages
// load only the page script, and their links work.
func TestPrivacyAndVerificationPages(t *testing.T) {
	t.Parallel()

	t.Run("the privacy page", func(t *testing.T) {
		t.Parallel()

		s := newSite(t)
		ctx := openPage(t, s, "privacy.html")

		assertMarkerShown(t, ctx, "privacy.html", "downgrade-risk")
		assertHSTSWaiver(t, ctx, "privacy.html")
		assertLoadsOnlyThePageScript(t, ctx, s, "privacy.html")

		// The link to the assessment page works.
		clickAndWaitForLoad(t, ctx, chromedp.CSS(`nav a[href="index.html"]`))
		waitFor(t, ctx, "the assessment page to be ready",
			`location.pathname.indexOf("/index.html") >= 0 && document.getElementById("run") !== null`)

		if title := eval[string](t, ctx, `document.title`); !strings.Contains(strings.ToLower(title), "assess") {
			t.Errorf("the page the privacy page's link opened has the title %q, want the assessment page's", title)
		}
	})

	t.Run("the verification page", func(t *testing.T) {
		t.Parallel()

		s := newSite(t)
		ctx := openPage(t, s, "verify.html")

		assertHSTSWaiver(t, ctx, "verify.html")
		assertLoadsOnlyThePageScript(t, ctx, s, "verify.html")

		clickAndWaitForLoad(t, ctx, chromedp.CSS(`nav a[href="privacy.html"]`))
		waitFor(t, ctx, "the privacy page to be ready",
			`location.pathname.indexOf("/privacy.html") >= 0 && document.getElementById("downgrade-risk") !== null`)
	})
}

// assertHSTSWaiver pins the HSTS waiver statement: the page shows it when the
// bundle was built from a record that says waived, and the default bundle, whose
// record does not exist yet, has none. The bundle under test is the one
// SHIFT2EU_SITE_BUNDLE names, so the test reads the branch the bundle was built
// with instead of rebuilding one.
func assertHSTSWaiver(t *testing.T, ctx context.Context, page string) {
	t.Helper()

	got := markerOf(t, ctx, "hsts-waived")

	if got.Exists {
		t.Logf("%s shows the HSTS waiver statement, so this bundle was built from a record that says waived", page)
		assertMarkerShown(t, ctx, page, "hsts-waived")

		return
	}

	t.Logf("%s shows no HSTS waiver statement, which is the default bundle: a missing record reads as not waived", page)
}

// assertLoadsOnlyThePageScript pins the scripts per page (PRD #297, Layer 2):
// the privacy and verification pages load exactly one script, the page script,
// and never wasm_exec.js or the module script, so the origin answered those and
// nothing else.
func assertLoadsOnlyThePageScript(t *testing.T, ctx context.Context, s *site, page string) {
	t.Helper()

	want := bundleAsset(t, "page", ".js")

	scripts := eval[[]string](t, ctx, `Array.from(document.querySelectorAll("script")).map(function (script) {
		return script.getAttribute("src") || "";
	})`)

	if !slices.Equal(scripts, []string{want}) {
		t.Fatalf("%s loads the scripts %v, want the page script %q alone: the page script runs only the framing guard and the unregister function there", page, scripts, want)
	}

	allowed := map[string]bool{
		"/" + page:                            true,
		"/" + bundleAsset(t, "style", ".css"): true,
		"/" + want:                            true,
		"/favicon.ico":                        true,
	}

	for _, request := range s.log() {
		if !allowed[request.Path] {
			t.Errorf("%s's origin answered %s %s?%s with %d, want only the page, its stylesheet and the page script",
				page, request.Method, request.Path, request.Query, request.Status)
		}
	}

	for _, asset := range []string{"/" + want, "/" + bundleAsset(t, "style", ".css")} {
		if !slices.ContainsFunc(s.log(), func(r request) bool { return r.Path == asset }) {
			t.Errorf("%s's origin never answered a request for %q", page, asset)
		}
	}
}
