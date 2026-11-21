//go:build browser && !js

package browser_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// This file holds the smoke test of the inventory page (issue #373, item 15):
// the page that shows how to build inventory.json for each source. It loads with
// no error and no policy violation, its three source buttons work by mouse and by
// keyboard and say which one is chosen, a copy button puts exactly its own
// command on the clipboard, and at phone width the page scrolls in neither
// direction. Everything it drives is the released bundle's own markup and script.

// inventoryPage is the bundle's name of the page.
const inventoryPage = "inventory.html"

// consoleMessages collects what the page writes to the console and throws, so a
// load can be held to "no error at all".
type consoleMessages struct {
	mu   sync.Mutex
	msgs []chromedp.ConsoleMessage
}

// collect starts collecting the tab's console messages, exceptions and log
// entries, and stops when the test ends.
func collect(t *testing.T, ctx context.Context) *consoleMessages {
	t.Helper()

	collected := &consoleMessages{}
	messages := chromedp.Console(ctx)

	go func() {
		for message, err := range messages {
			if err != nil {
				return
			}

			collected.mu.Lock()
			collected.msgs = append(collected.msgs, message)
			collected.mu.Unlock()
		}
	}()

	return collected
}

// errors returns the messages that are errors or exceptions.
func (c *consoleMessages) errors() []chromedp.ConsoleMessage {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out []chromedp.ConsoleMessage

	for _, message := range c.msgs {
		switch message.Type {
		case chromedp.ConsoleError, chromedp.ConsoleException:
			out = append(out, message)
		default:
			// A log, a debug, an info or a warning message is no error.
		}
	}

	return out
}

// settle waits until no message has arrived for a few polls, so a message the
// browser sent while the test was reading is not missed. It waits on the count
// the browser reports, never on a fixed time.
func (c *consoleMessages) settle(t *testing.T) {
	t.Helper()

	stable := 0
	last := -1

	for attempt := 0; attempt < 100 && stable < 3; attempt++ {
		c.mu.Lock()
		count := len(c.msgs)
		c.mu.Unlock()

		if count == last {
			stable++
		} else {
			stable = 0
			last = count
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// requireInventoryPage fails the test at once when the bundle holds no inventory
// page, so a missing page names itself rather than a poll that times out.
func requireInventoryPage(t *testing.T) {
	t.Helper()

	bundleFile(t, inventoryPage)
}

// TestInventoryPageLoadsWithoutAnError pins that the page runs clean (issue
// #373, item 15): it loads with the page's own script, nothing is thrown, nothing
// is written to the console as an error, and the browser reports no Content
// Security Policy violation, so the page stays inside the policy every page
// carries.
func TestInventoryPageLoadsWithoutAnError(t *testing.T) {
	t.Parallel()

	requireInventoryPage(t)

	s := newSite(t)
	ctx := newTab(t)

	messages := collect(t, ctx)

	do(t, ctx, chromedp.Navigate(s.url(inventoryPage)))
	waitFor(t, ctx, "the inventory page to load", `document.readyState === "complete" && document.querySelectorAll("button").length >= 3`)

	if !eval[bool](t, ctx, collectViolations) {
		t.Fatal("the page's securitypolicyviolation listener could not be installed")
	}

	// A policy violation is reported as it happens, so the page's own markup is
	// read once more after the listener is in place: a control that breaks the
	// policy reports it here, and one that broke it while loading reports it
	// before the listener can see it.
	do(t, ctx, chromedp.Click(chromedp.CSS(`button[aria-pressed="true"]`)))

	violations := eval[[]violation](t, ctx, `globalThis.__violations`)
	if len(violations) != 0 {
		t.Errorf("the browser reported %d Content Security Policy violations on %s: %v, want none: the page carries the same policy as every other page (issue #373, item 15)",
			len(violations), inventoryPage, violations)
	}

	messages.settle(t)

	for _, message := range messages.errors() {
		t.Errorf("%s writes %q to the console (%s at %s:%d), want no error and no exception: the page runs its script and nothing else (issue #373, item 15)",
			inventoryPage, message.Text, message.Type, message.URL, message.Line)
	}

	for _, name := range []string{"AWS", "GCP", "Azure"} {
		if !strings.Contains(buttonTexts(t, ctx), name) {
			t.Errorf("%s holds no button reading %q, want one per source (issue #373, item 15)", inventoryPage, name)
		}
	}
}

// collectViolations is installed on the page to record every refusal the browser
// reports; it is the network test's own listener.

// buttonTexts returns the text of each button of the page, in document order.
func buttonTexts(t *testing.T, ctx context.Context) string {
	t.Helper()

	return strings.Join(eval[[]string](t, ctx, `Array.from(document.querySelectorAll("button")).map(function (button) {
		return button.textContent.trim();
	})`), " ")
}

// clickButton clicks the button at index in document order, with the mouse.
func clickButton(t *testing.T, ctx context.Context, index int) {
	t.Helper()

	ids := run(t, ctx, chromedp.QueryNodeIDs(chromedp.CSSAll("button")))
	if index >= len(ids) {
		t.Fatalf("the page holds %d buttons, so there is no button %d", len(ids), index+1)
	}

	do(t, ctx, chromedp.Click(chromedp.NodeIDs{ids[index]}))
}

// TestInventorySourceButtonsWorkByPointerAndKeyboard pins the source control
// (issue #373, item 15): clicking the GCP button marks it pressed and unmarks the
// others, shows the GCP section and hides the other two, and the same holds when a
// visitor presses Enter or Space on a button, so the control works without a mouse.
func TestInventorySourceButtonsWorkByPointerAndKeyboard(t *testing.T) { //nolint:tparallel // the cases run in order on one page: each presses the button the one before left alone
	t.Parallel()

	requireInventoryPage(t)

	s := newSite(t)
	ctx := openPage(t, s, inventoryPage)

	waitFor(t, ctx, "the page's source buttons to be wired", `document.querySelectorAll("button[aria-controls]").length === 3`)

	tests := []struct {
		name    string
		press   string
		index   int
		pressed string
	}{
		{name: "a mouse click chooses GCP", press: "click", index: 1, pressed: "gcp"},
		{name: "Enter chooses AWS", press: "enter", index: 0, pressed: "aws"},
		{name: "Space chooses Azure", press: "space", index: 2, pressed: "azure"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch tc.press {
			case "click":
				clickButton(t, ctx, tc.index)
			case "enter":
				pressButton(t, ctx, tc.index, kb.Enter)
			case "space":
				pressButton(t, ctx, tc.index, " ")
			}

			waitFor(t, ctx, "the chosen source to change", fmt.Sprintf(`(() => {
				const buttons = Array.from(document.querySelectorAll("button[aria-controls]"));
				return buttons.length === 3 && buttons.every(function (button, index) {
					return button.getAttribute("aria-pressed") === (index === %d ? "true" : "false");
				});
			})()`, tc.index))

			states := eval[[]sourceState](t, ctx, `Array.from(document.querySelectorAll("button[aria-controls]")).map(function (button) {
				const panel = document.getElementById(button.getAttribute("aria-controls"));
				return {
					name: button.textContent.trim(),
					pressed: button.getAttribute("aria-pressed"),
					panel: button.getAttribute("aria-controls"),
					shown: panel !== null && panel.checkVisibility(),
				};
			})`)

			for i, state := range states {
				want := "false"
				if i == tc.index {
					want = "true"
				}

				if state.Pressed != want {
					t.Errorf("%q carries aria-pressed=%q after %s, want %q (issue #373, item 15)", state.Name, state.Pressed, tc.name, want)
				}

				if shown := i == tc.index; state.Shown != shown {
					t.Errorf("%q's section is shown=%t, want %t: the page shows the chosen source's commands and hides the others (issue #373, item 15)",
						state.Name, state.Shown, shown)
				}
			}
		})
	}
}

// sourceState is one source button as the page shows it: its name, the state of
// its aria-pressed attribute, the section it controls and whether that section is
// shown.
type sourceState struct {
	Name    string `json:"name"`
	Pressed string `json:"pressed"`
	Panel   string `json:"panel"`
	Shown   bool   `json:"shown"`
}

// pressButton focuses the button at index and presses the key on it, as a visitor
// who tabs to it does.
func pressButton(t *testing.T, ctx context.Context, index int, key string) {
	t.Helper()

	ids := run(t, ctx, chromedp.QueryNodeIDs(chromedp.CSSAll("button")))

	do(t, ctx, chromedp.Focus(chromedp.NodeIDs{ids[index]}), chromedp.KeyEvent(key))
}

// TestInventoryCopyPutsOneCommandOnTheClipboard pins the copy control (issue #373,
// item 15): the clipboard is granted to the origin, as the hosting's
// Permissions-Policy must allow it, and a copy button then puts exactly its own
// command on the clipboard, in place of whatever was there: not the whole page and
// not the other commands. The page shows one source's section at a time, so a
// command of a section that is not shown is copied after its source's button is
// pressed, as a visitor reaches it.
func TestInventoryCopyPutsOneCommandOnTheClipboard(t *testing.T) {
	t.Parallel()

	requireInventoryPage(t)

	s := newSite(t)
	ctx := openPage(t, s, inventoryPage)

	grantClipboard(t, ctx, s.server.URL)

	waitFor(t, ctx, "the page's copy buttons to be present", `Array.from(document.querySelectorAll("button")).filter(function (button) {
		return ((button.getAttribute("aria-label") || button.textContent) || "").toLowerCase().indexOf("copy") >= 0;
	}).length >= 2`)

	tests := []struct {
		name string
		// section is the id of the source's section the command is copied from.
		section string
		// shown says whether the page shows that section on load, which the
		// AWS one does and the others do not.
		shown bool
		// last copies the section's last command rather than its first, so the
		// two copies are never the same button.
		last bool
	}{
		{name: "AWS", section: "source-aws", shown: true},
		{name: "Azure", section: "source-azure", last: true},
	}

	for _, tc := range tests {
		if !tc.shown {
			clickButton(t, ctx, sourceButtonIndex(t, ctx, tc.section))

			waitFor(t, ctx, "the "+tc.name+" section to be shown", fmt.Sprintf(`(() => {
				const panel = document.getElementById(%s);
				return panel !== null && !panel.hidden;
			})()`, jsString(tc.section)))
		}

		copies := copyButtonsIn(t, ctx, tc.section)
		if len(copies) == 0 {
			t.Fatalf("the %s section of %s holds no copy button, want one per command (issue #373, item 15)", tc.name, inventoryPage)
		}

		copy := copies[0]
		if tc.last {
			copy = copies[len(copies)-1]
		}

		command := eval[string](t, ctx, fmt.Sprintf(`(() => {
			const button = document.querySelectorAll("button")[%d];
			const box = button.parentElement;
			const command = box === null ? null : box.querySelector("pre");
			return command === null ? "" : command.textContent;
		})()`, copy))

		if command == "" {
			t.Fatalf("the copy button %d sits in no element holding a command, so the test cannot tell what it copies (issue #373, item 15)", copy+1)
		}

		clickButton(t, ctx, copy)

		clipboard := readClipboard(t, ctx)

		if clipboard != command {
			t.Errorf("the %s section's copy button puts %q on the clipboard, want exactly its own command %q (issue #373, item 15)",
				tc.name, clipboard, command)
		}
	}
}

// TestInventoryPickerIsShownOnceTheScriptRuns pins the other half of the picker
// (issue #373, item 15, review finding 10): the page hides the three source
// buttons from a visitor whose browser runs no script, and the script removes
// that hiding once it has wired them, so a visitor whose browser runs it gets the
// control and its buttons still choose a source.
func TestInventoryPickerIsShownOnceTheScriptRuns(t *testing.T) {
	t.Parallel()

	requireInventoryPage(t)

	s := newSite(t)
	ctx := openPage(t, s, inventoryPage)

	waitFor(t, ctx, "the page's source buttons to be wired", `document.querySelectorAll("button[aria-controls]").length === 3`)

	waitFor(t, ctx, "the picker to be shown", `(() => {
		const buttons = Array.from(document.querySelectorAll("button[aria-controls]"));
		if (buttons.length !== 3) {
			return false;
		}
		const picker = buttons[0].parentElement;
		return picker !== null && !picker.hasAttribute("hidden") && buttons.every(function (button) {
			return button.checkVisibility();
		});
	})()`)

	// The control still works: the buttons choose a source as they did.
	clickButton(t, ctx, 1)

	waitFor(t, ctx, "the chosen source to change", `(() => {
		const buttons = Array.from(document.querySelectorAll("button[aria-controls]"));
		return buttons.length === 3 && buttons[0].getAttribute("aria-pressed") === "false" &&
			buttons[1].getAttribute("aria-pressed") === "true";
	})()`)

	states := eval[[]sourceState](t, ctx, `Array.from(document.querySelectorAll("button[aria-controls]")).map(function (button) {
		const panel = document.getElementById(button.getAttribute("aria-controls"));
		return {
			name: button.textContent.trim(),
			pressed: button.getAttribute("aria-pressed"),
			panel: button.getAttribute("aria-controls"),
			shown: panel !== null && panel.checkVisibility(),
		};
	})`)

	for i, state := range states {
		if shown := i == 1; state.Shown != shown {
			t.Errorf("%q's section is shown=%t after GCP is chosen, want %t: a source button the script repaired still chooses its section (issue #373, item 15)",
				state.Name, state.Shown, shown)
		}
	}
}

// TestInventoryCopyFailureSaysSo pins what a visitor sees when the page cannot
// write to the clipboard (issue #373, item 15, review finding 5): a browser
// without the clipboard API, or a page in a context where the API is absent,
// throws where the write is called, so the copy function catches that and says
// the copy failed, and the page throws nothing a visitor would never see. The
// clipboard is made undefined in this page only, as such a browser leaves it.
func TestInventoryCopyFailureSaysSo(t *testing.T) {
	t.Parallel()

	requireInventoryPage(t)

	s := newSite(t)
	ctx := newTab(t)

	messages := collect(t, ctx)

	do(t, ctx, chromedp.Navigate(s.url(inventoryPage)))
	waitFor(t, ctx, "the page's source buttons to be wired", `document.querySelectorAll("button[aria-controls]").length === 3`)

	copies := copyButtonsIn(t, ctx, "source-aws")
	if len(copies) == 0 {
		t.Fatalf("the AWS section of %s holds no copy button, want one per command (issue #373, item 15)", inventoryPage)
	}

	if kind := eval[string](t, ctx, `(() => {
		Object.defineProperty(navigator, "clipboard", { value: undefined });
		return typeof navigator.clipboard;
	})()`); kind != "undefined" {
		t.Fatalf("the page's clipboard is %s after the test took it away, want undefined: the check needs a page without the clipboard API (issue #373, item 15)", kind)
	}

	clickButton(t, ctx, copies[0])

	waitFor(t, ctx, "the copy's status to say the copy failed", fmt.Sprintf(`(() => {
		const button = document.querySelectorAll("button")[%d];
		const box = button === undefined ? null : button.parentElement;
		const status = box === null ? null : box.querySelector(".copy-status");
		return status !== null && /failed/i.test(status.textContent);
	})()`, copies[0]))

	messages.settle(t)

	for _, message := range messages.errors() {
		t.Errorf("copying a command with no clipboard API throws %q (%s at %s:%d), want no uncaught exception: the copy function catches the missing API and says the copy failed (issue #373, item 15)",
			message.Text, message.Type, message.URL, message.Line)
	}
}

// copyButtonsIn returns the indices, in the document's button order, of the copy
// buttons inside the section with the id.
func copyButtonsIn(t *testing.T, ctx context.Context, section string) []int {
	t.Helper()

	return eval[[]int](t, ctx, fmt.Sprintf(`(() => {
		const indices = [];
		document.querySelectorAll("button").forEach(function (button, index) {
			const name = ((button.getAttribute("aria-label") || button.textContent) || "").toLowerCase();
			if (button.closest(%s) !== null && name.indexOf("copy") >= 0) {
				indices.push(index);
			}
		});
		return indices;
	})()`, jsString("#"+section)))
}

// sourceButtonIndex returns the index, in the document's button order, of the
// source button that controls the section with the id.
func sourceButtonIndex(t *testing.T, ctx context.Context, section string) int {
	t.Helper()

	indices := eval[[]int](t, ctx, fmt.Sprintf(`(() => {
		const indices = [];
		document.querySelectorAll("button").forEach(function (button, index) {
			if ((button.getAttribute("aria-controls") || "") === %s) {
				indices.push(index);
			}
		});
		return indices;
	})()`, jsString(section)))

	if len(indices) != 1 {
		t.Fatalf("%s holds %d buttons controlling the section %q, want exactly one (issue #373, item 15)", inventoryPage, len(indices), section)
	}

	return indices[0]
}

// grantClipboard grants the origin the clipboard permissions the copy control
// needs, as the site's hosting has to allow them.
func grantClipboard(t *testing.T, ctx context.Context, origin string) {
	t.Helper()

	permission := func(name string) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
			_, err := chromedp.CallBrowser(ctx, cdpbrowser.SetPermission, cdpbrowser.SetPermissionParams{
				Permission: &cdpbrowser.PermissionDescriptor{Name: name},
				Setting:    cdpbrowser.PermissionSettingGranted,
				Origin:     origin,
			})

			return err
		})
	}

	do(t, ctx, permission("clipboard-read"), permission("clipboard-write"))
}

// readClipboard reads the browser's clipboard as the page sees it.
func readClipboard(t *testing.T, ctx context.Context) string {
	t.Helper()

	return evalAsync[string](t, ctx, `navigator.clipboard.readText()`)
}

// TestNoHorizontalScrollAtPhoneWidth pins the phone layout (issue #373, item 15
// and the report's item 11): at a phone's width the inventory page and the
// assessment page with a report scroll in neither direction, so nothing pokes out
// of the page. The report's own table may still scroll inside its box.
func TestNoHorizontalScrollAtPhoneWidth(t *testing.T) {
	t.Parallel()

	t.Run("the inventory page", func(t *testing.T) {
		t.Parallel()

		requireInventoryPage(t)

		s := newSite(t)
		ctx := newTab(t)

		do(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(s.url(inventoryPage)))
		waitFor(t, ctx, "the inventory page to load", `document.readyState === "complete" && document.querySelectorAll("pre").length > 0`)

		assertNoHorizontalScroll(t, ctx, inventoryPage)
	})

	t.Run("the assessment page with a report", func(t *testing.T) {
		t.Parallel()

		s := newSite(t)
		ctx := newTab(t)

		do(t, ctx, chromedp.EmulateViewport(390, 844), chromedp.Navigate(s.url("index.html")))
		waitFor(t, ctx, "the assessment module to load", `(() => {
			const run = document.getElementById("run");
			return run !== null && !run.disabled;
		})()`)

		// The page without a report is measured first, so a failure names which of
		// the two widths overflows: the page's own or the report's.
		before := pageWidth(t, ctx)

		loadExample(t, ctx, "aws")
		setTargets(t, ctx, "ovhcloud")
		runAssessment(t, ctx)

		if message := alertOf(t, ctx).Text; message != "" {
			t.Fatalf("the page shows the error %q for the aws example, want the report", message)
		}

		t.Logf("at phone width index.html is %d pixels wide without a report and %d with one, in a %d pixel window",
			before.Document, pageWidth(t, ctx).Document, before.Window)

		assertNoHorizontalScroll(t, ctx, "index.html with a report")
	})
}

// assertNoHorizontalScroll fails when the page is wider than the window, which is
// what a visitor on a phone sees as a page that scrolls sideways.
func assertNoHorizontalScroll(t *testing.T, ctx context.Context, page string) {
	t.Helper()

	widths := pageWidth(t, ctx)

	if widest := widths.Document; widest > widths.Window {
		t.Errorf("%s is %d pixels wide in a %d pixel window, want no page scroll: the page fits a phone's width, and only the report's table's own box scrolls (issue #373, item 15); the elements that reach past the window are %v",
			page, widest, widths.Window, widths.Offenders)
	} else if widths.Body > widths.Window {
		t.Errorf("%s's body is %d pixels wide in a %d pixel window, want no page scroll: the page fits a phone's width (issue #373, item 15)",
			page, widths.Body, widths.Window)
	}
}

// pageWidth returns how wide the page is, how wide the window is, and which
// elements reach past the window's right edge, so a failure names what overflows.
func pageWidth(t *testing.T, ctx context.Context) pageWidths {
	t.Helper()

	return eval[pageWidths](t, ctx, `(() => {
		const root = document.documentElement;
		const offenders = [];
		document.querySelectorAll("*").forEach(function (el) {
			const box = el.getBoundingClientRect();
			if (box.width > 0 && box.right > root.clientWidth + 1) {
				const classes = typeof el.className === "string" && el.className.trim() !== "" ? "." + el.className.trim().split(/\s+/).join(".") : "";
				offenders.push(el.tagName.toLowerCase() + classes);
			}
		});
		return {
			window: window.innerWidth,
			document: root.scrollWidth,
			body: document.body === null ? 0 : document.body.scrollWidth,
			offenders: offenders.slice(0, 12),
		};
	})()`)
}

// pageWidths is how wide the page is, how wide the window is, and which elements
// reach past the window's right edge.
type pageWidths struct {
	Window    int      `json:"window"`
	Document  int      `json:"document"`
	Body      int      `json:"body"`
	Offenders []string `json:"offenders"`
}
