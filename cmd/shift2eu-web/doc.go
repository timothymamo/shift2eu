//go:build js && wasm

// Command shift2eu-web is the site's WebAssembly module (PRD #297): the
// assessment of `shift2eu assess`, run in the user's browser, so no inventory
// leaves it. It is built with Go's standard toolchain, GOOS=js GOARCH=wasm.
//
// main is the composition root and the thin glue between the page's script
// and internal/web: it takes the adapters from the wire package, which builds
// them, and registers one JavaScript function, assessFunction, that converts
// the page's values to web.Options, calls web.Assessor.Assess and converts the
// result back. It holds no assessment logic. It is the only package that
// imports syscall/js, and it imports only wire, internal/web, syscall/js and a
// named list of standard packages: never internal/cli, internal/httpapi, a
// request decoder or a cloud SDK.
package main
