// Command sitecheck checks the live site against a release's manifest (PRD
// #297 milestone 3, issue #346): the release's check and milestone 4's
// scheduled workflow run it as
//
//	go run ./ci/sitecheck -manifest <manifest.json> -tags <file or -> [-hsts-record <file>] [-preload-record <file>] [-host <host>] [-apex <apex>] [-retry-bound <duration>]
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
