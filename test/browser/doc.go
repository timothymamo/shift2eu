// Package browser holds the site's headless-browser smoke test (PRD #297):
// what a real browser does with the built bundle, which the static checks in
// test/site cannot see. It loads the pages of the bundle from a local
// httptest origin, drives them with the real controls through the DevTools
// protocol, and pins the criteria the PRD marks *browser*: the assessment and
// the comparison in the page, the downloads' parity with the CLI, the two trust
// layers (the glue's text-only errors, the Content Security Policy's refusals
// on the wire, the routes the policy does not govern), the privacy and
// verification pages, the module's lifecycle and the accessibility of the flow.
//
// The test starts no browser: `make test-browser` runs a headless Chromium in a
// container pinned by digest with the host network, its DevTools on
// 127.0.0.1 and a port the kernel picks for each run (no fixed port to attach
// to, and the DevTools ports and containers of two runs do not collide). It
// passes the browser's DevTools address, read back from Chromium's log, in
// SHIFT2EU_DEVTOOLS_URL, and in SHIFT2EU_SITE_BUNDLE a bundle of the run's
// own: ci/sitebuild builds it into a temporary directory that the run removes
// when it ends, never bin/site, so a second run's `make site` in the same
// worktree cannot delete it from under the browser (issue #423). The address's
// browser id makes the test attach to the browser its run started: a
// correctness guarantee, not access control, since any local process that
// finds the port can read the id from /json/version. Without either variable
// the test fails, with a message saying which, and never skips: a smoke test
// that skips reports the same green as one that passed. It never touches the internet: "another origin" is a second
// local httptest server on its own port, and pinning that proves the bundle
// runs unchanged on any origin.
//
// The test files carry the build constraint `browser && !js`: `go test ./...`
// and `make test` never compile this package's tests and never need Chromium.
// The js/wasm run of the lint skips them too, since the driver, chromedp,
// needs syscall.SysProcAttr.Setsid, which js/wasm lacks: this smoke test drives
// a browser from the host and never runs inside one. The lint's other runs load
// them on the browser tag; the depguard rule browser-smoke-test holds the test
// files, chromedp-only-in-the-browser-test confines the driver to them, and
// this file, which imports nothing and is the package's only non-test file, is
// held by browser-smoke-test-doc, as test/site's doc.go is by
// test-docs-are-pure: without it `go test ./...` would report a package whose
// build constraints exclude every file.
package browser
