// Command sitebuild assembles the site's bundle (PRD #297 milestone 2): the
// static pages under site/, the WebAssembly module of cmd/shift2eu-web and
// Go's wasm_exec.js, as the files a static host serves, and the reproducible
// archive a release attests.
//
//	go run ./ci/sitebuild -out <dir> -archive <file.tar.gz> [-version <vX.Y.Z>] [-site <dir>]
//
// It writes exactly these files into -out, which must be missing or empty:
// index.html, inventory.html, privacy.html and verify.html, rendered from the pages of -site
// (site/ of the module by default); style.<h>.css and page.<h>.js, site/style.css
// and site/page.js byte for byte; wasm_exec.<h>.js, the building toolchain's
// $(go env GOROOT)/lib/wasm/wasm_exec.js byte for byte; module.<h>.js, the
// module built with -trimpath, -buildvcs=false, GOOS=js, GOARCH=wasm and
// CGO_ENABLED=0 and
// embedded, base64-encoded, in the one fixed template
// globalThis.__shift2euModule="<base64>"; and one newline; and manifest.json,
// the version and every other file's path and SHA-256, sorted by path. <h> is
// the first 16 hexadecimal characters of the SHA-256 of the file's bytes, so a
// changed file gets a new name. site/records/ and every other file of -site
// stay out of the bundle.
//
// The pages are html/template templates. The builder fills in the version (the
// -version tag, or internal/version's development value when untagged, as
// `shift2eu version` prints it), the links' git ref (the tag, or main), the Go
// version of the toolchain that builds the module (the value its
// runtime.Version() reports), each script's and the stylesheet's name and
// Subresource Integrity value (SHA-384), each target catalog's snapshot date
// and regions, each target's head-office country, the three bundled example
// inventories of examples/ as inert <template> content the page script reads
// as text, the inventory page's commands for AWS, GCP and Azure, which it
// reads from the bash blocks of docs/guide/inventory.md under each source's
// "Collect from" and "Build the ... inventory" headings (guide.go: the guide
// is their one source of truth, issue #373; a guide it cannot read them from
// fails the build, naming the heading), and whether the HSTS record site/records/hsts.json says waived (a
// missing record is not waived).
//
// The build is reproducible: no clock, no map order and no path reaches a
// file, the module is built with -trimpath and no VCS stamp (-buildvcs=false),
// so it depends on the sources alone, and the archive's entries are sorted, of one fixed
// time, owner and mode, gzip-compressed at a fixed level with no modification
// time. The test/site tests build it twice and compare.
//
// The builder reads no environment variable itself: it runs the go command
// with the environment it was given, as make does, with GOOS, GOARCH,
// CGO_ENABLED, GOFLAGS (-mod=readonly) and GOWORK (off) fixed, so a
// developer's settings cannot change what is built. It is CI tooling, no part
// of the product: a main package, which nothing imports, held by the strict
// depguard rule site-builder and the forbidigo group site-builder-env. Unlike
// the other CI tooling it is not standard library only: it constructs the
// three embedded-snapshot targets (ovhcloud, scaleway, upcloud) to read their
// catalogs (internal/domain/catalog) and takes the development version from
// internal/version, the one place outside cmd/ that constructs adapters
// (ADR 0036). The targets read only their embedded snapshots.
package main
