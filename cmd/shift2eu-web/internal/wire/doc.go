// Package wire builds the adapters of the site's WebAssembly module
// (PRD #297) into internal/web's Deps: the inventory reader over the document
// the page holds (inventoryfile.FromBytes), the embedded OVHcloud, Scaleway and
// UpCloud catalogs, as cmd/shift2eu wires them, and the Markdown, JSON and HTML
// reporters. None of them uses the network, a file or credentials.
//
// It is the part of cmd/shift2eu-web's composition root that needs no
// browser: the package carries no build constraint, so it builds and its
// tests run natively under `make test`. Its tests hold the parity of the web
// adapter with the CLI: they wire internal/cli from the same Deps (the
// targets, the Markdown and JSON reporters) and an inventory file reader, and
// compare the options both accept and the reports both write. Only those test
// files import internal/cli; the package itself never does, so the module
// carries no CLI. The depguard rules web-composition-root-wire and
// web-composition-root-wire-tests hold the two to strict allow lists
// (docs/adr/0035).
package wire
