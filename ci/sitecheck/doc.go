// Command sitecheck checks the live site against a release's manifest (PRD
// #297 milestone 3, issue #346): the release's check and milestone 4's
// scheduled workflow run it as
//
//	go run ./ci/sitecheck -manifest <manifest.json> -tags <file or -> [-hsts-record <file>] [-preload-record <file>] [-host <host>] [-apex <apex>] [-retry-bound <duration>] [-plain-http-www redirect|refuse-or-close]
//
// and the scheduled workflow (.github/workflows/site-check.yml) first asks
// which release is live with the live-version subcommand:
//
//	go run ./ci/sitecheck live-version -tags <file or -> -anchor <RFC 3339 time> -window <duration> [-host <host>]
//
// The subcommand fetches https://<host>/ once, reads the id="site-version" text as the
// check does, matches it against the tag list and applies the version window
// (livecheck.AcceptLive: the highest tag always, the one immediately below it
// only before anchor+window); it prints the tag list's entry and exits 0, or
// one line on stderr and exits 1 (refused) or 2 (usage error).
//
// -plain-http-www is what plain HTTP on the canonical host must do: redirect
// (the default) or refuse-or-close (ADR 0043); ADR 0042's records carry no
// field for it.
//
// It reads the release's manifest, the HSTS and preload records of the
// commit it runs from (site/records/hsts.json and site/records/preload.json
// by default) and the repository's tag list, exactly the files its flags
// name, hands them to ci/sitecheck/internal/livecheck with the production
// dialer, the system roots, the wall clock and a real sleep, prints the
// plain-text report on stdout and exits 0 when every check passed, 1 when a
// check failed and 2 on a usage or configuration error. Text taken from a
// response is printed only quoted and capped (livecheck.Quote).
//
// It reads no environment variable and starts no process: the commands of
// milestone 4 are built as argv by the library and run by the workflow. It is
// CI tooling, no part of the product: a main package, which nothing can
// import, built by no release, held by the depguard rule site-check-main and
// the forbidigo group site-check-env.
package main
