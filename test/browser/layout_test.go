//go:build browser && !js

package browser_test

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// This file pins the page's own layout for the maintainer's third round of
// feedback on the redesigned page (issue #373): the summary boxes and the
// workload boxes fill the width of the section that holds them (items 02 and
// 03), a phone's width still scrolls neither way with three targets compared,
// and the space between the no-cookies notice and the Inventory box is small
// (item 01). The measurements are the browser's own: offset and border boxes of
// the elements the report really renders, read through the page, never a copy of
// the stylesheet.

const (
	// desktopWidth and desktopHeight are the window the desktop layout is
	// measured in: a laptop's.
	desktopWidth  = 1280
	desktopHeight = 900
	// phoneWidth and phoneHeight are the window of a phone.
	phoneWidth  = 375
	phoneHeight = 812
	// fillTolerance is how far a box's edge may sit from its section's edge and
	// still count as filling it: a sub-pixel from the device scale factor.
	fillTolerance = 2
	// maxNoticeGap is the most space the page may leave between the no-cookies
	// notice and the Inventory box (issue #373, item 01).
	maxNoticeGap = 24
)

// fillView is one row of boxes measured against the section that holds it.
type fillView struct {
	// Section names the row in a failure: the summary or the workloads.
	Section string `json:"section"`
	// Count is the number of boxes the section holds.
	Count int `json:"count"`
	// SectionLeft and SectionRight are the left and right edge of the section's
	// content box.
	SectionLeft  float64 `json:"sectionLeft"`
	SectionRight float64 `json:"sectionRight"`
	// BoxesLeft and BoxesRight are the edges the boxes themselves reach.
	BoxesLeft  float64 `json:"boxesLeft"`
	BoxesRight float64 `json:"boxesRight"`
}

// fillExpression measures every summary and every workloads section of the
// page's report: the section's content box and the extent its boxes reach. A
// section's boxes are its <p> children (the summary's tiles) and its <section>
// children (the workload cards); the heading is not a box.
const fillExpression = `(() => {
	const measure = function (kind, selector) {
		const sections = Array.from(document.querySelectorAll("#report " + selector));
		return sections.map(function (section) {
			const style = getComputedStyle(section);
			const box = section.getBoundingClientRect();
			const boxes = Array.from(section.children).filter(function (el) {
				return el.matches("p, section");
			}).map(function (el) {
				return el.getBoundingClientRect();
			});
			if (boxes.length === 0) {
				return {section: kind, count: 0, sectionLeft: 0, sectionRight: 0, boxesLeft: 0, boxesRight: 0};
			}
			return {
				section: kind,
				count: boxes.length,
				sectionLeft: box.left + parseFloat(style.paddingLeft) + parseFloat(style.borderLeftWidth),
				sectionRight: box.right - parseFloat(style.paddingRight) - parseFloat(style.borderRightWidth),
				boxesLeft: Math.min.apply(null, boxes.map(function (b) { return b.left; })),
				boxesRight: Math.max.apply(null, boxes.map(function (b) { return b.right; })),
			};
		});
	};
	return measure("the summary", ".part-summary").concat(measure("the workloads", ".part-workloads"));
})()`

// TestReportBoxesFillTheirSection pins items 02 and 03 of the maintainer's third
// round (issue #373): at a desktop's width the summary boxes and the workload
// boxes fill the section that holds them, whatever the number of targets. The
// maintainer's screenshots showed both rows ending well before the report's right
// edge, with the rest of the line empty, for one target and for several. The
// check reads the first box's left edge and the last box's right edge against the
// section's content box, so a row that wraps or leaves a gap fails it.
func TestReportBoxesFillTheirSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		targets []string
	}{
		{name: "one target", targets: []string{"ovhcloud"}},
		{name: "two targets", targets: []string{"ovhcloud", "scaleway"}},
		{name: "three targets", targets: []string{"ovhcloud", "scaleway", "upcloud"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := assessmentAtWidth(t, desktopWidth, desktopHeight, tc.targets...)

			views := eval[[]fillView](t, ctx, fillExpression)

			if len(views) != 2*len(tc.targets) {
				t.Fatalf("the report of %d targets holds %d measured rows, want %d: a summary and the workloads of each target (issue #373, item 05): %v",
					len(tc.targets), len(views), 2*len(tc.targets), views)
			}

			for _, view := range views {
				t.Logf("%s: the section spans %.1f to %.1f pixels, its %d boxes %.1f to %.1f",
					view.Section, view.SectionLeft, view.SectionRight, view.Count, view.BoxesLeft, view.BoxesRight)

				if view.Count < 2 {
					t.Fatalf("%s holds %d boxes, want at least two, so that the row's extent says something", view.Section, view.Count)
				}

				if gap := view.BoxesLeft - view.SectionLeft; gap < -fillTolerance || gap > fillTolerance {
					t.Errorf("%s: the boxes start %.1f pixels from the section's left edge, want them at it: %v (issue #373, items 02 and 03)",
						view.Section, gap, view)
				}

				if gap := view.SectionRight - view.BoxesRight; gap < -fillTolerance || gap > fillTolerance {
					t.Errorf("%s: the boxes end %.1f pixels short of the section's right edge, want them at it, so the row fills the page's width (issue #373, items 02 and 03): %v",
						view.Section, gap, view)
				}
			}
		})
	}
}

// TestNoHorizontalScrollAtPhoneWidthWithThreeTargets pins the phone layout with
// a comparison of three targets (issue #373, items 02 and 03): the page fits the
// window and scrolls in neither direction, however wide the report's tables and
// however many boxes a row holds.
func TestNoHorizontalScrollAtPhoneWidthWithThreeTargets(t *testing.T) {
	t.Parallel()

	ctx := assessmentAtWidth(t, phoneWidth, phoneHeight, "ovhcloud", "scaleway", "upcloud")

	assertNoHorizontalScroll(t, ctx, "index.html with a three-target report")
}

// noticeGapView is the space the page leaves between the no-cookies notice and
// the Inventory box, and the two edges it is measured from.
type noticeGapView struct {
	// Found reports whether both elements are in the page.
	Found bool `json:"found"`
	// Gap is the distance from the notice's bottom to the Inventory box's top.
	Gap float64 `json:"gap"`
	// NoticeBottom and InventoryTop are the two edges Gap is the difference of.
	NoticeBottom float64 `json:"noticeBottom"`
	InventoryTop float64 `json:"inventoryTop"`
}

// TestNoCookiesNoticeSitsCloseToTheInventoryBox pins item 01 of the maintainer's
// third round (issue #373): at a desktop's width the page leaves at most
// maxNoticeGap pixels between the no-cookies notice and the Inventory box. The
// maintainer's screenshot showed a band of empty space there much taller than the
// space between the boxes that follow.
func TestNoCookiesNoticeSitsCloseToTheInventoryBox(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := newTab(t)

	do(t, ctx, chromedp.EmulateViewport(desktopWidth, desktopHeight), chromedp.Navigate(s.url("index.html")))
	waitFor(t, ctx, "the assessment module to load", `(() => {
		const run = document.getElementById("run");
		return run !== null && !run.disabled;
	})()`)

	got := eval[noticeGapView](t, ctx, `(() => {
		const notice = document.querySelector("main .hero > .notice.with-icon");
		const inventory = document.querySelector("main fieldset.card");
		if (notice === null || inventory === null) {
			return {found: false, gap: 0, noticeBottom: 0, inventoryTop: 0};
		}
		const first = notice.getBoundingClientRect();
		const second = inventory.getBoundingClientRect();
		return {found: true, gap: second.top - first.bottom, noticeBottom: first.bottom, inventoryTop: second.top};
	})()`)

	if !got.Found {
		t.Fatalf("the page holds no no-cookies notice above the Inventory box, so the gap cannot be measured: the notice is the hero's notice carrying the class \"with-icon\", the Inventory box the first fieldset of the class \"card\"")
	}

	t.Logf("the page leaves %.1f pixels between the no-cookies notice (bottom %.1f) and the Inventory box (top %.1f) at %d pixels wide",
		got.Gap, got.NoticeBottom, got.InventoryTop, desktopWidth)

	if got.Gap > maxNoticeGap {
		t.Errorf("the page leaves %.1f pixels between the no-cookies notice and the Inventory box, want at most %d: the empty band the maintainer asked to shrink (issue #373, item 01)",
			got.Gap, maxNoticeGap)
	}
}

// assessmentAtWidth opens the assessment page in a window of the given size,
// assesses the bundled aws example against the targets as the page's own controls
// do, and returns the tab.
func assessmentAtWidth(t *testing.T, width, height int64, targets ...string) context.Context {
	t.Helper()

	s := newSite(t)
	ctx := newTab(t)

	do(t, ctx, chromedp.EmulateViewport(width, height), chromedp.Navigate(s.url("index.html")))
	waitFor(t, ctx, "the assessment module to load", `(() => {
		const run = document.getElementById("run");
		return run !== null && !run.disabled;
	})()`)

	loadExample(t, ctx, "aws")
	setTargets(t, ctx, targets...)
	runAssessment(t, ctx)

	if message := alertOf(t, ctx).Text; message != "" {
		t.Fatalf("the page shows the error %q for the aws example with %d targets, want the report", message, len(targets))
	}

	return ctx
}
