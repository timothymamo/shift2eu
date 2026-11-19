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
// container pinned by digest with the host network, and passes its DevTools
// address in SHIFT2EU_DEVTOOLS_URL and the bundle `make site` built in
// SHIFT2EU_SITE_BUNDLE. Without either the test fails, with a message saying
// which, and never skips: a smoke test that skips reports the same green as one
// that passed. It never touches the internet: "another origin" is a second
// local httptest server on its own port, and pinning that proves the bundle
// runs unchanged on any origin.
//
// The test files carry the browser build tag, so `go test ./...`, `make test`
// and `make lint` never compile this package's tests and never need Chromium.
// This file imports nothing and is the package's only non-test file, as
// test/site's doc.go is: the depguard rule test-docs-are-pure holds it, and
// without it `go test ./...` would report a package whose build constraints
// exclude every file.
package browser
