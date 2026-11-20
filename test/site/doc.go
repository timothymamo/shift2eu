// Package site holds the static checks of the site's bundle (PRD #297): the
// bundle ci/sitebuild assembles from site/, the pages it emits, and the rules
// the PRD's three trust layers put on them (the Content Security Policy,
// Subresource Integrity, the scripts per page, the module script's template,
// wasm_exec.js, the markers, the links, the contrast, the archive and its
// reproducibility, the banned-API scan of the page script, the glue's allow list
// and the names it calls, and the page script's catch clause, which shows one
// constant message).
//
// The checks are test files: they build the bundle with `go run ./ci/sitebuild`
// into temporary directories, never run make, never start a browser and never
// touch the network. This file is the package's only non-test file and
// imports nothing; the depguard rule test-docs-are-pure holds it.
// internal/jsscan, the page-script scan, is a helper only this tree imports.
package site
