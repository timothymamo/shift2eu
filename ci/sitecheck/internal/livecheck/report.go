package livecheck

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The kinds of check a Report holds, one Check per kind and target.
const (
	// KindManifestFile: one manifest file served at https://<host>/<path>
	// (behaviour 1); Target is the path ("style.<h>.css").
	KindManifestFile = "manifest-file"
	// KindRoot: https://<host>/ serves index.html's bytes (behaviour 3);
	// Target is "/".
	KindRoot = "root"
	// KindServiceWorker: one path of ServiceWorkerPaths does not answer 200
	// (behaviour 4); Target is the path ("/sw.js").
	KindServiceWorker = "service-worker"
	// KindHTMLCache: one HTML page's Cache-Control (behaviour 5); Target is
	// "/" or the page's path ("privacy.html").
	KindHTMLCache = "html-cache"
	// KindRedirect: one URL of the redirect matrix (behaviour 6); Target is
	// the URL requested ("http://shift2eu.com/").
	KindRedirect = "redirect"
	// KindCertificate: one host's certificate (behaviour 7); Target is the
	// host.
	KindCertificate = "certificate"
	// KindHSTS: one host's Strict-Transport-Security header against the HSTS
	// record, or a record that could not be read (behaviour 8); Target is the
	// host, or "hsts.json" / "preload.json" for a record's error.
	KindHSTS = "hsts"
	// KindStatement: one page carries one statement (behaviour 8); Target is
	// "<page>#<marker>" ("privacy.html#downgrade-risk").
	KindStatement = "statement"
	// KindVersion: the live version is a tag of the list (behaviour 9);
	// Target is "/".
	KindVersion = "version"
	// KindModuleScriptSize: the module script's budgets and Content-Type
	// (behaviour 10); Target is its path.
	KindModuleScriptSize = "module-script-size"
)

// MaxQuotedBytes caps the bytes of response text Quote keeps.
const MaxQuotedBytes = 200

// Check is the outcome of one check of one round.
type Check struct {
	// Kind is one of the Kind* constants.
	Kind string
	// Target says what was checked; see each kind.
	Target string
	// Passed says whether the check passed.
	Passed bool
	// Reason says why it failed ("" when it passed). Any text taken from a
	// response in it has gone through Quote.
	Reason string
}

// Response records one response of the last round (behaviour 2).
type Response struct {
	// URL is the URL requested, built by Run, never taken from a response.
	URL string
	// RemoteAddr is the address of the connection that carried it, as the
	// dialled connection reports it; "" when no connection was made.
	RemoteAddr string
	// Status is the HTTP status, 0 when no response came (refused, closed).
	Status int
	// Age is the response's Age header.
	Age Age
	// CacheControl is the Cache-Control values joined by ", ", recorded,
	// judged only for HTML (KindHTMLCache); printed only through Quote.
	CacheControl string
	// HSTS is the Strict-Transport-Security values joined by ", ", recorded
	// (the apex's is judged only when preload is pursued); printed only
	// through Quote, and only when not empty.
	HSTS string
}

// Report is the outcome of Run.
type Report struct {
	// Host and Apex are the hosts checked, from the Config.
	Host string
	Apex string
	// Rounds is the number of rounds run, at least 1 after a valid Config.
	Rounds int
	// Checks are the last round's checks, in a fixed order: by kind in the
	// order of the Kind* constants, then by Target.
	Checks []Check
	// Responses are the last round's responses, in request order.
	Responses []Response
	// ModuleScript is the module script's measurement.
	ModuleScript Size
	// Version is the matched tag; zero when the KindVersion check failed.
	Version Tag
}

// Passed reports whether r has at least one check and every check passed.
func (r Report) Passed() bool {
	if len(r.Checks) == 0 {
		return false
	}

	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}

	return true
}

// WriteText writes r as a plain-text report to w (behaviour 11): one line per
// check ("PASS"/"FAIL", kind, target, reason), the hosts, the rounds, one line
// per response with its URL, remote address, status and Age ("absent",
// seconds, or "unparseable " and the quoted raw value), the module script's
// transferred and decoded sizes and encoding (saying when the edge does not
// compress it), the version, and a last line "result: PASS" or
// "result: FAIL". Every line starts with a fixed word of its own, and every
// piece of text taken from a response is printed only through Quote, so no
// response can start a line, such as a workflow command ("::"), or add one.
func (r Report) WriteText(w io.Writer) error {
	var b strings.Builder

	fmt.Fprintf(&b, "hosts: host %s, apex %s\n", oneLine(r.Host), oneLine(r.Apex))
	fmt.Fprintf(&b, "rounds: %d\n", r.Rounds)

	for _, check := range r.Checks {
		if check.Passed {
			fmt.Fprintf(&b, "PASS %s %s\n", oneLine(check.Kind), oneLine(check.Target))
		} else {
			fmt.Fprintf(&b, "FAIL %s %s: %s\n", oneLine(check.Kind), oneLine(check.Target), oneLine(check.Reason))
		}
	}

	for _, response := range r.Responses {
		fmt.Fprintf(&b, "response %s remote %s status %s age %s cache-control %s",
			oneLine(response.URL), orNone(oneLine(response.RemoteAddr)), status(response.Status),
			ageText(response.Age), Quote(response.CacheControl))

		if response.HSTS != "" {
			fmt.Fprintf(&b, " hsts %s", Quote(response.HSTS))
		}

		b.WriteString("\n")
	}

	b.WriteString(moduleScriptLine(r.ModuleScript))

	if r.Version.IsZero() {
		b.WriteString("version: none matched\n")
	} else {
		fmt.Fprintf(&b, "version: %s\n", r.Version.String())
	}

	if r.Passed() {
		b.WriteString("result: PASS\n")
	} else {
		b.WriteString("result: FAIL\n")
	}

	_, err := io.WriteString(w, b.String())

	return err
}

// moduleScriptLine returns the report's line of the module script's size.
func moduleScriptLine(s Size) string {
	if !s.Measured {
		return "module-script: not measured\n"
	}

	encoding := "gzip"
	if s.Encoding != EncodingGzip {
		encoding = "identity (the edge does not compress it)"
	}

	return fmt.Sprintf("module-script: %s, %d bytes transferred, %d bytes decoded, content-type %s\n",
		encoding, s.Transferred, s.Decoded, Quote(s.ContentType))
}

// status returns a response's status for the report, "none" when no response
// came.
func status(code int) string {
	if code == 0 {
		return "none"
	}

	return strconv.Itoa(code)
}

// orNone returns s, or "none" when it is empty.
func orNone(s string) string {
	if s == "" {
		return "none"
	}

	return s
}

// ageText returns a response's Age for the report.
func ageText(age Age) string {
	switch age.State {
	case AgeParsed:
		return strconv.FormatInt(age.Seconds, 10)
	case AgeUnparseable:
		return "unparseable " + Quote(age.Raw)
	default:
		return "absent"
	}
}

// oneLine returns s as it is when every character of it is printable, and
// through Quote otherwise, so no text of a check or a response (an error
// message may carry a server's words) breaks a line of the report.
func oneLine(s string) string {
	if !utf8.ValidString(s) {
		return Quote(s)
	}

	for _, r := range s {
		if !unicode.IsPrint(r) {
			return Quote(s)
		}
	}

	return s
}

// Quote returns s, text taken from a response, safe to print on one line of
// the report: its first MaxQuotedBytes bytes (cut at a UTF-8 boundary) quoted
// as %q does, followed by "...(truncated)" outside the quotes when s was
// longer. The result holds no line break and no control character.
func Quote(s string) string {
	if len(s) <= MaxQuotedBytes {
		return strconv.Quote(s)
	}

	cut := MaxQuotedBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return strconv.Quote(s[:cut]) + "...(truncated)"
}
