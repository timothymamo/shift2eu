// Package deploy uploads the site's bundle (PRD #297 milestone 3) to the
// site's Scaleway Object Storage bucket through the S3 API, signing every
// request with AWS Signature Version 4, written by hand on the standard
// library (Sign).
//
// It is no part of the product: only ci/sitedeploy imports it (Go's internal
// rule), and it is held by the strict depguard rule site-deploy-library to
// named standard packages, without os, os/exec, syscall or unsafe, and to no
// package of the module. It reads no environment variable and opens no file:
// ci/sitedeploy hands it the key, the bundle and the release manifests as
// values and fs.FS trees, and the HTTP client it talks through.
//
// # The bundle it consumes
//
// Config.Bundle is the root of the bundle ci/sitebuild writes (ADR 0036),
// exactly these flat files, no directory:
//
//   - index.html, inventory.html, privacy.html, verify.html: the pages;
//   - style.<h>.css, page.<h>.js, wasm_exec.<h>.js, module.<h>.js: the
//     content-addressed assets, <h> the first 16 lowercase hexadecimal
//     characters of the SHA-256 of the file's bytes;
//   - manifest.json: {"version": "<v>", "files": [{"path": "<name>",
//     "sha256": "<64 lowercase hex>"}, ...]}, every other file once, sorted by
//     path (ParseManifest).
//
// Deploy refuses the bundle before any request when the manifest does not
// parse, its version is not Config.Version, a manifest path is not one of the
// names above (so no "/", "..", "\", "%", control character, absolute or
// empty path can become an object key), an asset's <h> is not its digest's
// prefix, a listed file is missing or its SHA-256 differs, or the bundle holds
// a file the manifest does not list.
//
// # What Deploy does, in order
//
//  1. Validates the Config (ErrInvalidConfig) and the bundle
//     (ErrInvalidBundle), with no request.
//  2. The rollback guard: one GET of Config.LivePage, following no redirect.
//     It refuses (ErrRollback, no upload) only when the page's version
//     (LiveVersion) is a tag of Config.Releases higher than Config.Version AND
//     the SHA-256 of the body equals the index.html entry of that tag's
//     manifest (Config.Manifests). In every other case (an error or non-200
//     answer, no or an unparseable version, a version that is no known tag, a
//     tag with no manifest, a body that does not match it, or a version not
//     higher than the bundle's) it overwrites. It writes to Config.Summary a
//     plain notice when the live page is a genuine release not higher than
//     the bundle's (a known tag whose manifest's index.html the body
//     matches), and otherwise a visible warning saying which case it was;
//     both say it overwrites and name the live version where there is one.
//     The guard's GET is never signed. The page's text never becomes a path,
//     a tag or a command: the tag is taken from Config.Releases. The body is
//     read up to 1 MiB; a longer page is untrusted.
//  3. Only when Config.ListBucket is true (off by default; ADR 0042
//     records that it needs runbook step R7's ListBucket grant): lists the
//     bucket (ListObjectsV2, every page) and fails with ErrUnknownObject,
//     before any upload, naming each key that is in no release's manifest
//     (Config.Releases with Config.Manifests), not in this bundle's and not
//     manifest.json. A listed key is only compared, never sent; a page of the
//     answer is read up to 8 MiB, and at most 1000 pages.
//  4. Uploads with PUT only, path-style (<Endpoint>/<Bucket>/<key>): first
//     every content-addressed asset, then manifest.json, then the four pages
//     last, each in sorted order. The first failed upload stops the run with
//     an error, so a failure before the pages leaves every page as it was.
//     Deploy never sends DELETE, nor any request but GET (live page, listing)
//     and PUT (an object of the bundle) to the bucket. It uploads the bytes it
//     checked in step 1, held in memory, never a second read of the bundle.
//
// Each upload carries, per file:
//
//	file              Content-Type                    Cache-Control                        Content-Encoding
//	*.html            text/html; charset=utf-8        public, max-age=300                  (none)
//	manifest.json     application/json                public, max-age=300                  (none)
//	style.<h>.css     text/css; charset=utf-8         public, max-age=31536000, immutable  (none)
//	page.<h>.js,
//	wasm_exec.<h>.js  text/javascript; charset=utf-8  public, max-age=31536000, immutable  (none)
//	module.<h>.js     text/javascript; charset=utf-8  public, max-age=31536000, immutable  gzip
//
// The module script is gzip-compressed by Deploy (Hosting decision 4, ADR
// 0042) at gzip.BestCompression with no name and no modification time, so the same bundle gives
// the same bytes; its manifest digest is of the decoded bytes, as the live
// check compares after content decoding. Every PUT also carries
// X-Amz-Content-Sha256, the SHA-256 of the bytes sent, which the store checks.
//
// # The key
//
// The access key and the secret key are Secrets, whose String, GoString,
// Format, MarshalText and MarshalJSON print "[redacted]". Neither key ever
// reaches Config.Summary, Config.Log or an error's text: errors name the
// request (method, key, status code) and at most an S3 error code that
// matches [A-Za-z]{1,64}, never a response body or header, and every text
// Deploy writes is also scrubbed of both keys' values, so a hostile server
// that echoes the key back gets nothing printed. An error answer is read up
// to 64 KiB, only for its code. The errors Deploy returns wrap only its
// sentinels (and ctx's error), never an error whose text a server wrote.
package deploy
