// Package livecheck checks the live site against a release's manifest (PRD
// #297 milestone 3, issue #346, ADR 0043): the library of ci/sitecheck, which the
// release's check and milestone 4's scheduled workflow run.
//
// It fetches, over HTTPS and plain HTTP, only URLs it builds itself from the
// configured canonical host and apex and the manifest's paths, and judges what
// the edge serves:
//
//   - every manifest file, digest after content decoding, no redirect followed
//     (KindManifestFile);
//   - the root, the bytes of the manifest's index.html (KindRoot);
//   - no service worker script answers 200 (KindServiceWorker);
//   - every HTML page's Cache-Control max-age is at most 300 (KindHTMLCache);
//   - the redirect matrix of the apex and plain HTTP (KindRedirect);
//   - the certificates of the host and the apex verify and do not expire within
//     the margin (KindCertificate, CheckCertificate);
//   - Strict-Transport-Security against the HSTS record (KindHSTS) and the
//     pages' statements against both records (KindStatement);
//   - the live version, untrusted text, against the repository's tag list
//     (KindVersion), whose match is the one way to construct a Tag, the only
//     input of the argv builders (DownloadArgv, AttestationVerifyArgv);
//   - the module script's transferred and decoded size and its Content-Type
//     (KindModuleScriptSize).
//
// Run repeats the checks until they pass or the retry bound, at least the
// HTML's max-age of 300 seconds, is spent, and returns a Report, whose text
// form quotes and caps every piece of text taken from a response.
//
// Everything it depends on that is not a pure function is injected through
// Config: the dialer (Dialer), the root certificates, the clock and the sleep,
// so its tests run against local httptest servers, TLS included, never contact
// a real host and never wait in real time. It opens no file, reads no
// environment variable and starts no process: it builds the gh commands of
// milestone 4 as argv slices and runs none of them. Manifest paths are only
// ever used as URL paths on the canonical host. ci/sitecheck reads the files
// its flags name and hands their bytes to the parsers here.
//
// It is CI tooling, no part of the product, held by the depguard rule
// site-check-library to named standard packages, without os, os/exec, syscall
// or unsafe. Go's internal rule keeps it out of reach of cmd/ and internal/.
package livecheck
