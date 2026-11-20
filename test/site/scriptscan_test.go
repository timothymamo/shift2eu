package site_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/timothymamo/shift2eu/test/site/internal/jsscan"
)

// This file holds PRD #297's Layer 1 check over the one hand-written script the
// site ships: site/page.js, and the copy the bundle serves under a
// content-addressed name, must pass the page-script scan. The scan's rules and
// their hostile fixtures are test/site/internal/jsscan's own tests; here the
// real script has to pass them.

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
