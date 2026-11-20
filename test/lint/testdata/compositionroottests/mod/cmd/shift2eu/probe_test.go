// The CLI composition root's _test.go file, for
// TestCompositionRootTestsReachNoForeignCodeProbe: every import that ends in a
// want comment is one the composition-root-tests rule refuses, and each stands
// for a way a test can run code the CLI never runs: start a program (os/exec),
// hand a path to a CGI handler (net/http/cgi), load a plugin (plugin), read a tree
// (go/build), call C (runtime/cgo), look a user up in the OS database (os/user,
// issue #152), or reach any package's symbol through unsafe. The imports are
// used, so the file type-checks, and no line does anything at run time: the probe
// lints the file and never runs it.
package main

import (
	"go/build"     // want "import 'go/build' is not allowed from list 'composition-root-tests'"
	"net/http/cgi" // want "import 'net/http/cgi' is not allowed from list 'composition-root-tests'"
	"os/exec"      // want "import 'os/exec' is not allowed from list 'composition-root-tests'"
	"os/user"      // want "import 'os/user' is not allowed from list 'composition-root-tests'"
	"plugin"       // want "import 'plugin' is not allowed from list 'composition-root-tests'"
	"runtime/cgo"  // want "import 'runtime/cgo' is not allowed from list 'composition-root-tests'"
	"testing"
	"unsafe" // want "import 'unsafe' is not allowed from list 'composition-root-tests'"
)

var (
	_ = build.Default
	_ = cgi.Handler{}
	_ = exec.CommandContext
	_ = user.Current
	_ = plugin.Open
	_ = cgo.NewHandle
	_ = unsafe.Sizeof(0)
)

// TestProbe keeps the refused imports used without calling any of them.
func TestProbe(t *testing.T) {
	t.Parallel()
}
