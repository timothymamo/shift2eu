//go:build browser

package browser_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// This file holds the download parity criterion (PRD #297, Assessment in the
// browser): the Markdown and the JSON downloads of one example, one target and
// the estimates on are byte-identical to what `shift2eu assess --format
// markdown` and `--format json` write for the same inventory and options. The
// page's own download function makes the bytes; the test reads them from the
// blob it creates, and compares them with the CLI's stdout.

// downloaded is one file the page's download function produced: the blob's
// bytes, the file name it offered and its type.
type downloaded struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

// recordDownloads makes the page's own download function observable: the test
// keeps the blob and the name its anchor carried, then lets the page's code
// run unchanged. It is how the bytes are read without a file system: the blob
// is the download.
const recordDownloads = `(() => {
	globalThis.__downloads = [];
	const createObjectURL = URL.createObjectURL;
	URL.createObjectURL = function (blob) {
		const url = createObjectURL.call(URL, blob);
		globalThis.__downloads.push({blob: blob, url: url, type: blob.type, name: ""});
		return url;
	};
	const click = HTMLAnchorElement.prototype.click;
	HTMLAnchorElement.prototype.click = function () {
		if (this.download) {
			const last = globalThis.__downloads[globalThis.__downloads.length - 1];
			if (last !== undefined) {
				last.name = this.download;
				last.href = this.href;
			}
		}
		return click.call(this);
	};
	return true;
})()`

// readDownloads returns the files the page's download function produced, with
// their text.
const readDownloads = `Promise.all(globalThis.__downloads.map(function (download) {
	return download.blob.text().then(function (text) {
		return {name: download.name, type: download.type, text: text, url: download.url};
	});
}))`

// TestDownloadParity pins the download parity criterion (PRD #297, Assessment
// in the browser): the page's Markdown and JSON downloads of one example are
// byte-identical to the CLI's own output for the same inventory and options.
func TestDownloadParity(t *testing.T) {
	t.Parallel()

	s := newSite(t)
	ctx := openAssessment(t, s)

	loadExample(t, ctx, "aws")
	setTargets(t, ctx, "ovhcloud")
	inventory := eval[string](t, ctx, `document.getElementById("inventory").value`)

	// The blob is the download, so the browser is told to refuse the file
	// itself: nothing is written to a disk and the page's code is unchanged.
	run(t, ctx, chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.CallBrowser(ctx, cdpbrowser.SetDownloadBehavior, cdpbrowser.SetDownloadBehaviorParams{
			Behavior: cdpbrowser.SetDownloadBehaviorBehaviorDeny,
		})

		return err
	}))

	if !eval[bool](t, ctx, recordDownloads) {
		t.Fatal("the download function could not be made observable")
	}

	runAssessment(t, ctx)

	if message := alertOf(t, ctx).Text; message != "" {
		t.Fatalf("the page shows the error %q for the aws example, want the report", message)
	}

	for _, id := range []string{"download-markdown", "download-json"} {
		waitFor(t, ctx, "the "+id+" button to be enabled", `!document.getElementById(`+jsString(id)+`).disabled`)
	}

	do(t, ctx, chromedp.Click(chromedp.ID("download-markdown")))
	do(t, ctx, chromedp.Click(chromedp.ID("download-json")))

	files := evalAsync[[]downloaded](t, ctx, readDownloads)

	if len(files) != 2 {
		t.Fatalf("the page's download function produced %d files, want the Markdown and the JSON report: %v", len(files), files)
	}

	want := []struct {
		name string
		typ  string
		args []string
	}{
		{name: "shift2eu-report.md", typ: "text/markdown;charset=utf-8", args: []string{"--target", "ovhcloud", "--format", "markdown"}},
		{name: "shift2eu-report.json", typ: "application/json", args: []string{"--target", "ovhcloud", "--format", "json"}},
	}

	for i, w := range want {
		got := files[i]

		if got.Name != w.name {
			t.Errorf("download %d is offered as %q, want %q", i+1, got.Name, w.name)
		}

		if got.Type != w.typ {
			t.Errorf("download %d (%s) is a %q blob, want %q", i+1, w.name, got.Type, w.typ)
		}

		if !strings.HasPrefix(got.URL, "blob:") {
			t.Errorf("download %d (%s) is offered from %q, want a blob: URL, which is same-document and sends nothing", i+1, w.name, got.URL)
		}

		expected := string(cliReport(t, cliBinary(t), inventory, w.args...))
		if got.Text != expected {
			t.Errorf("the page's %s download is not byte-identical to `shift2eu assess %s` over the same inventory and options:\n%s",
				w.name, strings.Join(w.args, " "), firstDifference(expected, got.Text))
		}
	}
}

// firstDifference describes where the CLI's report and the page's download
// differ, so a byte-identity failure is readable: a whole report would not be.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}

	line := strings.Count(want[:at], "\n") + 1

	return fmt.Sprintf("the first difference is at byte %d, line %d: the CLI wrote %q, the page's download has %q",
		at, line, lineAt(want, line), lineAt(got, line))
}

// lineAt returns the line of text, counting from one, or "" when it has none.
func lineAt(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}

	return lines[line-1]
}
