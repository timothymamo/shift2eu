// Package sitefront holds the tests of the site's front (PRD #297 milestone 4,
// docs/adr/0045): the Caddyfile in infra/site that the serverless container
// runs, and what that container answers.
//
// Two tests share the package. The static one, in files with no build
// constraint, runs under make test and pins what infra/site/Caddyfile says,
// token by token, against the file the container actually runs: the two global
// options, the one plain-HTTP site address, the exact X-Forwarded-Proto and
// hostport expressions the scheme and www are matched by (and no other matcher
// for either), every 308's target and the path guard the escaped request URI
// hides behind, the response headers against site/records/hsts.json and the
// policies every page carries in its <meta> element, each header attributed to
// the route its block answers (www over HTTPS, any other host over HTTPS, plain
// HTTP), so the apex's HTTPS 308 carries exactly one Strict-Transport-Security:
// max-age=31536000 and no plain-HTTP answer carries one (issue #601), the reverse proxy to
// {$UPSTREAM} and the request and response headers it removes, the one global
// log that keeps Caddy's request loggers out of the container's output, and
// the directives the design refuses: a log outside the global options, encode,
// cache and tls. It reads the files, never a copy of them, and it parses the
// Caddyfile with a small tokenizer of its own, so a comment cannot satisfy a
// check the configuration no longer does.
//
// The behaviour test, in the files built with the sitefront tag alone, runs the
// image infra/site/container.tf pins, with the command and arguments that file
// gives it and the Caddyfile's bytes, against a local HTTPS upstream the test
// starts, and drives every case of docs/adr/0045's list over raw HTTP/1.1: the
// four rows of its table, the joined X-Forwarded-Proto, the Location of every
// redirect, its own headers against the upstream's, the upstream's headers that
// are not the site's, its 502, a gzip object passing byte for byte, the request
// headers the upstream never sees, OPTIONS *, and a log that names no visitor,
// read from a whole log that holds Caddy's startup line. `make test-sitefront`
// runs it, outside make test and make check, as `make test-browser` runs the
// browser smoke test (docs/adr/0036).
//
// Neither test builds an image, calls Scaleway or reads a credential, and
// neither runs in the js/wasm lint: the behaviour test drives a container from
// the host, as the browser smoke test drives a browser.
package sitefront
