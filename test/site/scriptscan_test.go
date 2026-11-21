package site_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/test/site/internal/jsscan"
)

// This file holds PRD #297's Layer 1 check over the one hand-written script the
// site ships: site/page.js, and the copy the bundle serves under a
// content-addressed name, must pass the page-script scan. The scan's rules and
// their hostile fixtures are test/site/internal/jsscan's own tests; here the
// real script has to pass them.

// TestPageScriptConfinesTheClipboardWrite pins the clipboard (issue #373, item
// 15): the page script writes to it in exactly one function, copyCommand, and
// nowhere else, as the download function is the only place a blob URL is
// created. Every command on the inventory page has its own copy button, whose
// click handler calls that one function, so no other code path can put text on
// the clipboard.
func TestPageScriptConfinesTheClipboardWrite(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	source, err := os.ReadFile(filepath.Join(theRepositoryRoot(t), "site", "page.js"))
	if err != nil {
		t.Fatalf("read site/page.js: %v", err)
	}

	name, built := bundle.asset(t, "page", ".js")

	for _, script := range []struct {
		name string
		src  []byte
	}{
		{name: "site/page.js", src: source},
		{name: name, src: built},
	} {
		t.Run(script.name, func(t *testing.T) {
			t.Parallel()

			text := string(script.src)

			writes := strings.Count(text, "navigator.clipboard.writeText")
			if writes != 1 {
				t.Errorf("%s writes to the clipboard %d times, want exactly one: copyCommand is the one function that copies a command, as download is the one that offers a file (issue #373, item 15)",
					script.name, writes)
			}

			if !copyCommandDeclared.Match(script.src) {
				t.Errorf("%s declares no function named copyCommand, want the one function that writes to the clipboard: the scan confines the write to that name (issue #373, item 15)",
					script.name)
			}

			violations, err := jsscan.Scan(script.src)
			if err != nil {
				t.Fatalf("scan %s: %v", script.name, err)
			}

			for _, violation := range violations {
				t.Errorf("%s:%d:%d: %s\nA page script may hold none of the banned APIs, and the clipboard write is confined to copyCommand (issue #373, item 15)",
					script.name, violation.Line, violation.Col, violation.Message)
			}
		})
	}
}

// copyCommandDeclared matches the declaration of the one function the clipboard
// write is confined to.
var copyCommandDeclared = regexp.MustCompile(`function\s+copyCommand\s*\(`)

// TestPageScriptPassesTheBannedApiScan pins that the page script, as written and
// as built, holds none of the browser APIs that can send what it holds, no
// computed property name and no reflection call that could name one at run time,
// one Trusted Types policy of the fixed name shift2eu-report, and its framing
// guard's, download function's, unregister function's and report insertion
// point's uses confined to those functions (PRD #297, Layer 1).
func TestPageScriptPassesTheBannedApiScan(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	source, err := os.ReadFile(filepath.Join(theRepositoryRoot(t), "site", "page.js"))
	if err != nil {
		t.Fatalf("read site/page.js: %v", err)
	}

	if len(source) == 0 {
		t.Fatal("site/page.js is empty: the page script runs the module, the framing guard, the unregister function and the downloads")
	}

	name, built := bundle.asset(t, "page", ".js")

	if !bytes.Equal(source, built) {
		t.Errorf("the bundle's %s (%d bytes) differs from site/page.js (%d bytes): the page script ships as written, under a content-addressed name",
			name, len(built), len(source))
	}

	for _, script := range []struct {
		name string
		src  []byte
	}{
		{name: "site/page.js", src: source},
		{name: name, src: built},
	} {
		t.Run(script.name, func(t *testing.T) {
			t.Parallel()

			violations, err := jsscan.Scan(script.src)
			if err != nil {
				t.Fatalf("scan %s: %v", script.name, err)
			}

			for _, violation := range violations {
				t.Errorf("%s:%d:%d: %s\nA page script may hold none of the banned APIs, no computed property name, no reflection call and exactly one Trusted Types policy; the framing guard, the download function, the unregister function and the report's insertion point are the only exceptions, each confined to its own function (PRD #297, Layer 1)",
					script.name, violation.Line, violation.Col, violation.Message)
			}
		})
	}
}
