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
//   - every service worker path answers, with a status outside 2xx; no
//     response at all fails too (KindServiceWorker);
//   - every HTML page's Cache-Control max-age, and s-maxage,
//     stale-while-revalidate and stale-if-error when present, is at most 300
//     (KindHTMLCache);
//   - the redirect matrix of the apex and plain HTTP (KindRedirect); where the
//     HSTS record allows plain HTTP on the canonical host to refuse or close,
//     only a refused connection, or one closed or reset before any response
//     byte, counts as that: a timeout, or bytes Go cannot parse as a response
//     (HTTP/0.9, a malformed header block), fails;
//   - the certificates of the host and the apex verify, and neither the leaf
//     nor an intermediate of the verified chain expires within the margin, the
//     handshake bounded by the request timeout (KindCertificate,
//     CheckCertificate);
//   - Strict-Transport-Security against the HSTS record (KindHSTS) and the
//     pages' statements against both records (KindStatement), the records read
//     with exactly their lower-case keys, none twice;
//   - the live version, untrusted text read only from a root whose bytes are
//     the manifest's index.html, in one linear scan, against the repository's
//     tag list (KindVersion), whose match is the one way to construct a Tag,
//     the only input of the argv builders (DownloadArgv,
//     AttestationVerifyArgv);
//   - the module script's transferred and decoded size and its Content-Type
//     (KindModuleScriptSize).
//
// Every request goes over HTTP/1.1 only: the transport dials through the
// injected Dialer with a TLS configuration of its own, which turns HTTP/2 off,
// so headers, compression and size are judged as the edge serves them over
// HTTP/1.1.
//
// Run repeats the checks until they pass or the retry bound, at least the
// HTML's max-age of 300 seconds, is spent, and returns a Report, whose text
// form quotes and caps every piece of text taken from a response. When the
// only failing checks are records that could not be read, it reports after
// that round: no later round can change them.
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
