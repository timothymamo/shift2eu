package sitefront_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// This file holds the front's static test: what infra/site/Caddyfile says, read
// from the file the container runs, token by token (docs/adr/0045). It runs
// under make test, needs no container and no network, and never reads a copy of
// the Caddyfile: a comment, a directive the design refuses or a redirect built
// from the decoded path is refused.
//
// Each check below is a function from a parsed Caddyfile to the problems it
// finds, so that the checks themselves are tested: TestChecksAcceptTheDesigned
// runs every one of them on the configuration docs/adr/0045 describes, and
// TestChecksRefuseBrokenConfigurations runs them on configurations that break
// one rule each. A check that is vacuous, or one that refuses the designed
// configuration, fails there rather than in the coder's next pull request.

// caddyfilePath is the Caddyfile from the repository root, the one file in
// infra/site that the container runs byte for byte.
const caddyfilePath = "infra/site/Caddyfile"

// The two expressions docs/adr/0045 matches the scheme and www by, as their
// tokens. Caddy joins repeated X-Forwarded-Proto fields with ", " before a
// matcher sees them, so a header that is not exactly https is anything else;
// {http.request.hostport} is the request's raw Host, so a port, another case or
// a trailing dot misses. Both are the whole expression: a substring matcher
// (header, host) could not tell :443 or WWW. apart.
const (
	schemeExpression = "expression {http.request.header.X-Forwarded-Proto} == https"
	hostExpression   = "expression {http.request.hostport} == www.shift2eu.com"
)

// TestCaddyfileGlobalOptions pins the two options the front needs: no automatic
// HTTPS (the front speaks plain HTTP behind the platform's TLS, docs/adr/0045)
// and no admin endpoint (nothing administers it).
func TestCaddyfileGlobalOptions(t *testing.T) {
	t.Parallel()

	report(t, checkGlobalOptions(readCaddyfile(t)))
}

// TestCaddyfileSiteAddress pins the one site address: :8080, with no scheme, so
// the container answers plain HTTP on the port infra/site declares.
func TestCaddyfileSiteAddress(t *testing.T) {
	t.Parallel()

	report(t, checkSiteAddress(readCaddyfile(t)))
}

// TestCaddyfileSchemeAndHost pins the two expressions the request is judged by,
// and refuses every other way of judging it (docs/adr/0045, *What the server
// answers*): no header or host matcher, no {scheme}, no protocol matcher and no
// trusted_proxies. A substring or scheme matcher would answer a spoofed or
// forwarded header, and trusted_proxies would make Caddy rewrite the forwarding
// headers this front removes.
func TestCaddyfileSchemeAndHost(t *testing.T) {
	t.Parallel()

	report(t, checkExpressions(readCaddyfile(t)))
}

// TestCaddyfileRedirects pins every redirect (docs/adr/0045): a 308, never a
// 301 or a 302, to https://www.shift2eu.com plus either the request's URI or
// exactly /, and the URI form only behind the guard that the request's path
// starts with /. The Location is built from {http.request.uri}, Go's
// EscapedPath plus the raw query: the decoded path would put a literal ?, # or
// a line break into the header, {uri} is Caddy's own spelling of the same thing
// and is refused like the decoded path, and uri.path_escaped is the query
// escaper, not the path's.
func TestCaddyfileRedirects(t *testing.T) {
	t.Parallel()

	report(t, checkRedirects(readCaddyfile(t)))
}

// TestCaddyfileHeaders pins the response headers of www (docs/adr/0045, *The
// headers*): HSTS as site/records/hsts.json implies it, the pages'
// Content-Security-Policy plus frame-ancestors 'none', nosniff, no referrer,
// and a Permissions-Policy that turns every feature off (the site uses none).
func TestCaddyfileHeaders(t *testing.T) {
	t.Parallel()

	f := readCaddyfile(t)
	wantHSTS, sendHSTS := hstsHeaderFor(t)
	wantCSP := withFrameAncestors(cspDirectives(pageCSP(t)))

	report(t, checkHeaders(f, wantHSTS, sendHSTS, wantCSP))
}

// TestCaddyfileReverseProxy pins the upstream (docs/adr/0045, *The upstream*):
// the pipeline endpoint from {$UPSTREAM} over HTTPS with the certificate
// verified, Host as the upstream's own host and port (C3's
// --change-host-header), and the request and response headers removed, so
// neither the visitor's address nor the platform's forwarding headers nor a
// cookie reach Edge Services, and the upstream's own copies of the three
// headers, its cookie and its X-Amz-* headers never reach the client as
// duplicates.
func TestCaddyfileReverseProxy(t *testing.T) {
	t.Parallel()

	report(t, checkReverseProxy(readCaddyfile(t)))
}

// TestCaddyfileRefusedDirectives pins the directives the design refuses
// (docs/adr/0045, docs/adr/0043): no access log in a site block (the visitor's
// address, request line and headers are nobody's to keep, and the platform ships
// the container's output to Cockpit), no encode and no cache, so a module
// uploaded with Content-Encoding: gzip is served with its bytes, and no tls,
// since the platform terminates TLS and auto_https is off. The one global log it
// requires is TestCheckGlobalLogForms's.
func TestCaddyfileRefusedDirectives(t *testing.T) {
	t.Parallel()

	report(t, checkRefusedDirectives(readCaddyfile(t)))
}

// designedCaddyfile is the configuration docs/adr/0045's design describes: the
// two global options, the plain-HTTP site, the two expressions, the headers,
// the proxy to {$UPSTREAM} with its removed headers, and the two redirects with
// their guard. It is what the checks below must accept; the file in infra/site
// is what they run on under make test.
const designedCaddyfile = `{
	auto_https off
	admin off
	log {
		exclude http.log.error http.handlers.reverse_proxy http.stdlib
	}
}

:8080 {
	@https expression {http.request.header.X-Forwarded-Proto} == "https"
	@www expression {http.request.hostport} == "www.shift2eu.com"
	@site @https @www

	handle @site {
		header {
			Strict-Transport-Security "max-age=31536000; includeSubDomains"
			Content-Security-Policy "default-src 'none'; frame-ancestors 'none'"
			X-Content-Type-Options "nosniff"
			Referrer-Policy "no-referrer"
			Permissions-Policy "browsing-topics=(), camera=(), geolocation=(), microphone=(), payment=(), usb=()"
		}
		reverse_proxy https://{$UPSTREAM} {
			header_up Host {upstream_hostport}
			header_up -X-Forwarded-For
			header_up -X-Forwarded-Proto
			header_up -X-Forwarded-Host
			header_up -X-Forwarded-Port
			header_up -Forwarded
			header_up -X-Real-IP
			header_up -Via
			header_up -X-Request-Id
			header_up -Cookie
			header_up -Authorization
			header_up -X-Envoy-*
			header_down -Content-Security-Policy
			header_down -Strict-Transport-Security
			header_down -X-Content-Type-Options
			header_down -Referrer-Policy
			header_down -Permissions-Policy
			header_down -Set-Cookie
			header_down -X-Amz-*
		}
	}

	@path expression {http.request.uri.path}.startsWith("/")
	handle @path {
		redir https://www.shift2eu.com{http.request.uri} 308
	}
	redir https://www.shift2eu.com/ 308
}
`

// designedCSP is the policy the designed configuration repeats from a page,
// with frame-ancestors added.
var designedCSP = []string{"default-src 'none'", "frame-ancestors 'none'"}

// TestChecksAcceptTheDesignedConfiguration fails when a check refuses the
// configuration docs/adr/0045 describes: the checks would then refuse the
// coder's file for following the design.
func TestChecksAcceptTheDesignedConfiguration(t *testing.T) {
	t.Parallel()

	f := parseFile(designedCaddyfile)

	report(t, checkGlobalOptions(f))
	report(t, checkSiteAddress(f))
	report(t, checkExpressions(f))
	report(t, checkRedirects(f))
	report(t, checkHeaders(f, "max-age=31536000; includeSubDomains", true, designedCSP))
	report(t, checkReverseProxy(f))
	report(t, checkRefusedDirectives(f))
}

// TestChecksRefuseBrokenConfigurations fails when a check accepts a
// configuration that breaks one rule: a check that finds nothing here would
// pass on any file.
func TestChecksRefuseBrokenConfigurations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		check func(*caddyfile) []string
		want  string
		// exactly, when not zero, is the number of problems the check must report: the
		// configuration breaks the one rule the case names and nothing else.
		exactly int
	}{
		{
			name:  "a header matcher for the scheme",
			src:   broken(`expression {http.request.header.X-Forwarded-Proto} == "https"`, `@https header X-Forwarded-Proto https`),
			check: checkExpressions,
			want:  "outside an expression",
		},
		{
			name:  "a {scheme} token",
			src:   broken(`expression {http.request.header.X-Forwarded-Proto} == "https"`, `@x {scheme}`),
			check: checkExpressions,
			want:  `"{scheme}"`,
		},
		{
			name:  "the scheme compared without the joined value",
			src:   broken(`@https expression {http.request.header.X-Forwarded-Proto} == "https"`, `@https expression {http.request.header.X-Forwarded-Proto}.startsWith("https")`),
			check: checkExpressions,
			want:  "the scheme is matched by",
		},
		{
			name:  "an unguarded Location from the request URI",
			src:   broken(`@path expression {http.request.uri.path}.startsWith("/")`, `redir https://www.shift2eu.com{http.request.uri} 308`),
			check: checkRedirects,
			want:  "not guarded",
		},
		{
			name:  "the {uri} shorthand",
			src:   broken(`redir https://www.shift2eu.com{http.request.uri} 308`, `redir https://www.shift2eu.com{uri} 308`),
			check: checkRedirects,
			want:  "{uri} is refused",
		},
		{
			name:  "a 302",
			src:   broken(`@path expression {http.request.uri.path}.startsWith("/")`, `redir https://www.shift2eu.com/ 302`),
			check: checkRedirects,
			want:  "want 308",
		},
		{
			name:  "the decoded path in the Location",
			src:   broken(`@path expression {http.request.uri.path}.startsWith("/")`, `redir https://www.shift2eu.com{http.request.uri.path} 308`),
			check: checkRedirects,
			want:  "{http.request.uri.path}",
		},
		{
			name:  "no redir at all",
			src:   strings.NewReplacer("redir https://www.shift2eu.com{http.request.uri} 308", "respond \"no\" 200", "\tredir https://www.shift2eu.com/ 308\n", "").Replace(designedCaddyfile),
			check: checkRedirects,
			want:  "holds no redir",
		},
		{
			name:    "an access log",
			src:     withLine(`	log`),
			check:   checkRefusedDirectives,
			want:    "access log",
			exactly: 1,
		},
		{
			name:    "an encode directive",
			src:     withLine(`	encode gzip`),
			check:   checkRefusedDirectives,
			want:    "encode directive",
			exactly: 1,
		},
		{
			name:    "a tls directive",
			src:     withLine(`	tls internal`),
			check:   checkRefusedDirectives,
			want:    "tls directive",
			exactly: 1,
		},
		{
			name:  "the Host not sent as the upstream's",
			src:   strings.Replace(designedCaddyfile, "header_up Host {upstream_hostport}", "", 1),
			check: checkReverseProxy,
			want:  "Host",
		},
		{
			name:  "a forwarding header not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_up -X-Forwarded-For\n", "", 1),
			check: checkReverseProxy,
			want:  "X-Forwarded-For",
		},
		{
			name:  "the upstream's own HSTS not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_down -Strict-Transport-Security\n", "", 1),
			check: checkReverseProxy,
			want:  "Strict-Transport-Security",
		},
		{
			name:  "the port the platform forwarded not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_up -X-Forwarded-Port\n", "", 1),
			check: checkReverseProxy,
			want:  "X-Forwarded-Port",
		},
		{
			name:  "the client's Via not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_up -Via\n", "", 1),
			check: checkReverseProxy,
			want:  "Via",
		},
		{
			name:  "the upstream's own nosniff not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_down -X-Content-Type-Options\n", "", 1),
			check: checkReverseProxy,
			want:  "X-Content-Type-Options",
		},
		{
			name:  "the upstream's own Referrer-Policy not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_down -Referrer-Policy\n", "", 1),
			check: checkReverseProxy,
			want:  "Referrer-Policy",
		},
		{
			name:  "the upstream's own cookie not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_down -Set-Cookie\n", "", 1),
			check: checkReverseProxy,
			want:  "Set-Cookie",
		},
		{
			name:  "the bucket's X-Amz-* headers not removed",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_down -X-Amz-*\n", "", 1),
			check: checkReverseProxy,
			want:  "X-Amz-Request-Id",
		},
		{
			name:  "the bucket's X-Amz-* headers named one by one instead of the prefix",
			src:   strings.Replace(designedCaddyfile, "\t\t\theader_down -X-Amz-*\n", "\t\t\theader_down -X-Amz-Request-Id\n\t\t\theader_down -X-Amz-Id-2\n\t\t\theader_down -X-Amz-Meta-Foo\n", 1),
			check: checkReverseProxy,
			want:  "open set",
		},
		{
			name:  "no auto_https off",
			src:   strings.Replace(designedCaddyfile, "\tauto_https off\n", "", 1),
			check: checkGlobalOptions,
			want:  "auto_https off",
		},
		{
			name:  "another site address",
			src:   strings.Replace(designedCaddyfile, "\n:8080 {", "\nhttp://:8080 {", 1),
			check: checkSiteAddress,
			want:  ":8080",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := tc.check(parseFile(tc.src))
			if len(problems) == 0 {
				t.Fatalf("the check accepts a configuration that breaks the rule: want a problem naming %q", tc.want)
			}
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
				t.Errorf("the check reports %q, want one naming %q", problems, tc.want)
			}
			if tc.exactly != 0 && len(problems) != tc.exactly {
				t.Errorf("the check reports %d problems (%q), want %d: the configuration breaks exactly the one rule the case names", len(problems), problems, tc.exactly)
			}
		})
	}
}

// TestCheckHeadersRefuses pins the header check on the three values that decide
// it, which the table above cannot state in a Caddyfile alone: an HSTS value
// the record does not imply, a header the record waives, and a policy missing
// the framing guard.
func TestCheckHeadersRefuses(t *testing.T) {
	t.Parallel()

	const hsts = "max-age=31536000; includeSubDomains"

	tests := []struct {
		name     string
		src      string
		wantHSTS string
		sendHSTS bool
		wantCSP  []string
		want     string
	}{
		{
			name:     "another HSTS value",
			src:      designedCaddyfile,
			wantHSTS: "max-age=31536000; includeSubDomains; preload",
			sendHSTS: true,
			wantCSP:  designedCSP,
			want:     "Strict-Transport-Security",
		},
		{
			name:     "an HSTS header the record waives",
			src:      designedCaddyfile,
			sendHSTS: false,
			wantCSP:  designedCSP,
			want:     "waives it",
		},
		{
			name:     "a policy without the framing guard",
			src:      designedCaddyfile,
			wantHSTS: hsts,
			sendHSTS: true,
			wantCSP:  []string{"default-src 'none'"},
			want:     "Content-Security-Policy",
		},
		{
			name:     "another Permissions-Policy",
			src:      strings.Replace(designedCaddyfile, "usb=()", "usb=(self)", 1),
			wantHSTS: hsts,
			sendHSTS: true,
			wantCSP:  designedCSP,
			want:     "usb",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := checkHeaders(parseFile(tc.src), tc.wantHSTS, tc.sendHSTS, tc.wantCSP)
			if len(problems) == 0 {
				t.Fatalf("the header check accepts a configuration that breaks the rule: want a problem naming %q", tc.want)
			}
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
				t.Errorf("the header check reports %q, want one naming %q", problems, tc.want)
			}
		})
	}
}

// TestCheckGlobalLogForms pins the two shapes of the front's one global log, and the ones it refuses
// (docs/adr/0045, *The log*): Caddy's default logger still writes http.log.error and
// http.handlers.reverse_proxy, each with the request's remote_ip and client_ip, its uri and its headers,
// and http.stdlib, Go's server error log, whose panic line names the remote address; the platform ships the
// container's output to Cockpit, so the global options hold a log that excludes all three loggers or one
// whose output is discarded. Both accepted shapes are pinned, since either is the coder's to
// choose, and neither may be written any other way: the two-name exclusion leaves http.stdlib writing, so it
// is refused with the rest. A log in a site block stays refused, as the table above pins.
func TestCheckGlobalLogForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string // a problem the check must report; "" when it must report none
	}{
		{
			name: "the exclude form",
			src:  withGlobalLog("\tlog {\n\t\texclude http.log.error http.handlers.reverse_proxy http.stdlib\n\t}\n"),
		},
		{
			name: "the exclude form, the three loggers in another order",
			src:  withGlobalLog("\tlog {\n\t\texclude http.stdlib http.handlers.reverse_proxy http.log.error\n\t}\n"),
		},
		{
			name: "the discard form",
			src:  withGlobalLog("\tlog {\n\t\toutput discard\n\t}\n"),
		},
		{
			name: "one of the loggers left writing",
			src:  withGlobalLog("\tlog {\n\t\texclude http.log.error\n\t}\n"),
			want: "want either",
		},
		{
			name: "the two loggers without http.stdlib",
			src:  withGlobalLog("\tlog {\n\t\texclude http.log.error http.handlers.reverse_proxy\n\t}\n"),
			want: "want either",
		},
		{
			name: "another output",
			src:  withGlobalLog("\tlog {\n\t\toutput stderr\n\t}\n"),
			want: "want either",
		},
		{
			name: "a discard beside the exclusion",
			src:  withGlobalLog("\tlog {\n\t\texclude http.log.error http.handlers.reverse_proxy http.stdlib\n\t\toutput discard\n\t}\n"),
			want: "output",
		},
		{
			name: "a named log",
			src:  withGlobalLog("\tlog default {\n\t\toutput discard\n\t}\n"),
			want: "log default",
		},
		{
			name: "a log with no block",
			src:  withGlobalLog("\tlog\n"),
			want: "want either",
		},
		{
			name: "no log at all",
			src:  withGlobalLog(""),
			want: "hold no log",
		},
		{
			name: "a log in a site block",
			src:  strings.Replace(withGlobalLog("\tlog {\n\t\toutput discard\n\t}\n"), "\n:8080 {\n", "\n:8080 {\n\tlog\n", 1),
			want: "access log",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := checkRefusedDirectives(parseFile(tc.src))

			switch {
			case tc.want == "" && len(problems) > 0:
				t.Fatalf("the check refuses %q, which the design accepts: %q", describeLogs(tc.src), problems)
			case tc.want != "" && len(problems) == 0:
				t.Fatalf("the check accepts %q, which it must refuse: want a problem naming %q", describeLogs(tc.src), tc.want)
			case tc.want != "" && !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }):
				t.Errorf("the check reports %q, want one naming %q", problems, tc.want)
			}
		})
	}
}

// globalLogConfiguration is a configuration with the front's two global options and nothing else, for the
// cases that write one global log into it: the only problems they may find are the log's own.
const globalLogConfiguration = "{\n\tauto_https off\n\tadmin off\n}\n\n:8080 {\n}\n"

// withGlobalLog returns globalLogConfiguration with lines added to its global options block.
func withGlobalLog(lines string) string {
	return strings.Replace(globalLogConfiguration, "\tadmin off\n", "\tadmin off\n"+lines, 1)
}

// describeLogs returns the log directives of src as the source writes them, for a message that says which
// log the check was given rather than quoting the whole configuration.
func describeLogs(src string) string {
	var logs []string

	for _, n := range parseFile(src).all() {
		if name, _ := n.head(); name == "log" {
			logs = append(logs, describe(n))
		}
	}

	if len(logs) == 0 {
		return "no log"
	}

	return strings.Join(logs, " | ")
}

// The checks. Each returns the problems it finds, worded as the test reports
// them, and none when the file says what docs/adr/0045 says.

// checkGlobalOptions holds the leading block of global options.
func checkGlobalOptions(f *caddyfile) []string {
	globals := f.globalBlock()
	if globals == nil {
		return []string{fmt.Sprintf("the Caddyfile does not begin with a block of global options: its top-level lines are %q, want a lone {", f.topTokens())}
	}

	var problems []string

	for _, want := range [][]string{{"auto_https", "off"}, {"admin", "off"}} {
		if findExact(globals.children, want...) == nil {
			problems = append(problems, fmt.Sprintf("the global options block holds no %q: the front needs both (docs/adr/0045)", strings.Join(want, " ")))
		}
	}

	return problems
}

// checkSiteAddress holds that the file has exactly one site block, addressed
// :8080 with no scheme.
func checkSiteAddress(f *caddyfile) []string {
	sites := f.sites()
	if len(sites) != 1 {
		return []string{fmt.Sprintf("the Caddyfile holds %d site blocks, want exactly one: its top-level lines are %q", len(sites), f.topTokens())}
	}

	want := []string{":8080"}
	if got := sites[0].tokens; !slices.Equal(got, want) {
		return []string{fmt.Sprintf("the site block's address is %q, want %q: a scheme (http://, https://) or another port changes what the container answers", got, want)}
	}

	return nil
}

// checkExpressions holds the two expressions the request is judged by, and
// refuses every other matcher for the scheme or the host.
func checkExpressions(f *caddyfile) []string {
	var problems []string

	var scheme, host int

	for _, n := range f.all() {
		name, _ := n.head()
		if name == "" || name == "header_up" || name == "header_down" {
			// A header_up/header_down deletion names a header too; only an
			// expression may match on one.
			continue
		}

		joined := strings.Join(n.directiveTokens(), " ")

		switch {
		case strings.Contains(joined, "X-Forwarded-Proto"):
			if name != "expression" {
				problems = append(problems, fmt.Sprintf("line %d: %q matches on X-Forwarded-Proto outside an expression: the scheme is matched by %q alone, never by a header matcher (docs/adr/0045)", n.line, joined, schemeExpression))

				continue
			}
			if joined != schemeExpression {
				problems = append(problems, fmt.Sprintf("line %d: the scheme is matched by %q, want %q: the joined header must equal https exactly, so two headers, a second value or https, https all miss (docs/adr/0045)", n.line, joined, schemeExpression))

				continue
			}
			scheme++
		case strings.Contains(joined, "hostport"):
			if name != "expression" || joined != hostExpression {
				problems = append(problems, fmt.Sprintf("line %d: the host is matched by %q, want %q: the request's raw host must equal www.shift2eu.com exactly, so a port, another case or a trailing dot misses (docs/adr/0045)", n.line, joined, hostExpression))

				continue
			}
			host++
		}
	}

	if scheme == 0 {
		problems = append(problems, fmt.Sprintf("no expression matches the scheme by %q: the front answers a request whose joined X-Forwarded-Proto is exactly https as the site and every other one with a 308 (docs/adr/0045)", schemeExpression))
	}
	if host == 0 {
		problems = append(problems, fmt.Sprintf("no expression matches www by %q: the site is served for that host alone (docs/adr/0045)", hostExpression))
	}

	for _, n := range f.all() {
		for _, tok := range n.tokens {
			switch tok {
			case "{scheme}", "trusted_proxies", "protocol", "host":
				problems = append(problems, fmt.Sprintf("line %d: the token %q has no place in this front: the scheme is matched by the joined X-Forwarded-Proto expression and www by the hostport expression, and the front trusts no proxy (docs/adr/0045)", n.line, tok))
			}
		}
	}

	return problems
}

// checkRedirects holds every redirect, and the guard of the one that carries
// the request URI.
func checkRedirects(f *caddyfile) []string {
	var problems []string

	const (
		withURI = "https://www.shift2eu.com{http.request.uri}"
		toRoot  = "https://www.shift2eu.com/"
	)

	redirects := 0
	for _, n := range f.all() {
		name, args := n.head()
		if name != "redir" {
			continue
		}
		redirects++

		args = withoutMatchers(args)
		if len(args) != 2 {
			problems = append(problems, fmt.Sprintf("line %d: redir takes a target and a status, got %q", n.line, n.tokens))

			continue
		}

		target, status := args[0], args[1]
		if status != "308" {
			problems = append(problems, fmt.Sprintf("line %d: redir answers %q, want 308: a 301 or a 302 lets a client repeat the request over plain HTTP (docs/adr/0045)", n.line, status))
		}

		switch {
		case target == toRoot:
		case target == withURI:
			if !f.guarded(n) {
				problems = append(problems, fmt.Sprintf("line %d: redir to %q is not guarded by the path-starts-with-/ test: for an absolute-form, an opaque or a * request target that placeholder is not a path, and the Location is not a URL of the site (docs/adr/0045)", n.line, withURI))
			}
		case strings.Contains(target, "{uri}"):
			problems = append(problems, fmt.Sprintf("line %d: redir to %q: {uri} is refused, as the decoded path is: the front builds the Location from {http.request.uri} behind the guard (docs/adr/0045)", n.line, target))
		default:
			problems = append(problems, fmt.Sprintf("line %d: redir goes to %q, want %q (guarded) or %q: every redirect lands on https://www.shift2eu.com (docs/adr/0045)", n.line, target, withURI, toRoot))
		}

		for _, bad := range []string{"{http.request.uri.path}", "uri.path_escaped"} {
			if strings.Contains(target, bad) {
				problems = append(problems, fmt.Sprintf("line %d: the Location is built with %s: the decoded path puts a ? or a # into it as a query or a fragment, and uri.path_escaped is the query escaper, not the path's (docs/adr/0045)", n.line, bad))
			}
		}
	}

	if redirects == 0 {
		problems = append(problems, "the Caddyfile holds no redir: every request whose joined X-Forwarded-Proto is not exactly https, and every request for another host, gets a 308 to https://www.shift2eu.com (docs/adr/0045)")
	}

	return problems
}

// checkHeaders holds the five response headers of www against the values the
// record, the pages and docs/adr/0045 imply.
func checkHeaders(f *caddyfile, wantHSTS string, sendHSTS bool, wantCSP []string) []string {
	var problems []string

	headers := headerValues(f.all())

	gotHSTS := headers["strict-transport-security"]
	switch {
	case !sendHSTS && len(gotHSTS) > 0:
		problems = append(problems, fmt.Sprintf("the Caddyfile sends Strict-Transport-Security %q, but site/records/hsts.json waives it: a header the record does not claim is held to nothing (docs/adr/0042, docs/adr/0045)", gotHSTS))
	case sendHSTS && len(gotHSTS) != 1:
		problems = append(problems, fmt.Sprintf("the Caddyfile sends %d Strict-Transport-Security headers, want exactly one %q: it is the header site/records/hsts.json implies (docs/adr/0042, docs/adr/0045)", len(gotHSTS), wantHSTS))
	case sendHSTS && gotHSTS[0] != wantHSTS:
		problems = append(problems, fmt.Sprintf("the Caddyfile sends Strict-Transport-Security %q, want %q: it is the header site/records/hsts.json implies (docs/adr/0042, docs/adr/0045)", gotHSTS[0], wantHSTS))
	}

	gotCSP := headers["content-security-policy"]
	switch {
	case len(gotCSP) != 1:
		problems = append(problems, fmt.Sprintf("the Caddyfile sends %d Content-Security-Policy headers, want exactly one: the pages' policy plus frame-ancestors 'none' (docs/adr/0036, docs/adr/0045)", len(gotCSP)))
	default:
		if got := cspDirectives(gotCSP[0]); !slices.Equal(got, wantCSP) {
			problems = append(problems, fmt.Sprintf("the Content-Security-Policy header is %q, want the pages' policy plus frame-ancestors 'none', as the directive set %q (order does not count): a change of a page's policy must fail until the configuration follows (docs/adr/0045)", gotCSP[0], wantCSP))
		}
	}

	for _, want := range []struct{ name, value string }{
		{"x-content-type-options", "nosniff"},
		{"referrer-policy", "no-referrer"},
	} {
		if got := headers[want.name]; !slices.Equal(got, []string{want.value}) {
			problems = append(problems, fmt.Sprintf("the Caddyfile sends %s %q, want exactly %q (docs/adr/0045)", want.name, got, want.value))
		}
	}

	if got := headers["permissions-policy"]; len(got) != 1 {
		problems = append(problems, fmt.Sprintf("the Caddyfile sends %d Permissions-Policy headers, want exactly one (docs/adr/0045)", len(got)))
	} else if err := checkPermissionsPolicy(got[0]); err != nil {
		problems = append(problems, fmt.Sprintf("the Permissions-Policy header %q: %v", got[0], err))
	}

	return problems
}

// checkReverseProxy holds the proxy line, the Host it sends and the headers it
// removes in both directions.
func checkReverseProxy(f *caddyfile) []string {
	var problems []string

	proxies := find(f.all(), func(n *node) bool {
		return len(n.tokens) > 0 && n.tokens[0] == "reverse_proxy"
	})
	if len(proxies) != 1 {
		return []string{fmt.Sprintf("the Caddyfile holds %d reverse_proxy directives, want exactly one: www is proxied to the Edge Services pipeline (docs/adr/0045)", len(proxies))}
	}
	proxy := proxies[0]

	if want := []string{"reverse_proxy", "https://{$UPSTREAM}"}; !slices.Equal(proxy.tokens, want) {
		problems = append(problems, fmt.Sprintf("the reverse_proxy line is %q, want %q: the endpoint comes from the container's UPSTREAM variable and is reached over HTTPS with its certificate verified (docs/adr/0045)", proxy.tokens, want))
	}

	up := map[string][]string{}
	down := map[string][]string{}

	for _, child := range proxy.children {
		if len(child.tokens) < 2 {
			continue
		}
		switch child.tokens[0] {
		case "header_up":
			up[child.tokens[1]] = child.tokens[2:]
		case "header_down":
			down[child.tokens[1]] = child.tokens[2:]
		}
	}

	if got := up["Host"]; !slices.Equal(got, []string{"{upstream_hostport}"}) {
		problems = append(problems, fmt.Sprintf("the proxy sends Host %q upstream, want {upstream_hostport}: Edge Services selects the site by the endpoint it is asked for (C3's --change-host-header, docs/adr/0045)", got))
	}

	// The platform's headers, the visitor's address and what a client sends
	// along: none of them may reach Edge Services, which needs nothing of the
	// visitor.
	deletedUp := deleted(up)

	for _, name := range []string{
		"X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-Port",
		"Forwarded", "X-Real-IP", "Via", "Cookie", "Authorization", "X-Request-Id",
	} {
		if !deletedUp[strings.ToLower(name)] {
			problems = append(problems, fmt.Sprintf("the proxy does not remove %s from the upstream request (header_up -%s): it carries the visitor's address, a client's forwarding header or a credential, and Edge Services needs none of them (docs/adr/0045)", name, name))
		}
	}

	if !deletesPrefix(deletedUp, "X-Envoy-", "X-Envoy-Upstream-Service-Time", "X-Envoy-External-Address") {
		problems = append(problems, "the proxy does not remove every X-Envoy-* header from the upstream request: header_up -X-Envoy-* removes the whole prefix, and the platform's X-Envoy-Upstream-Service-Time and X-Envoy-External-Address are what a client must not be able to relay (docs/adr/0045)")
	}

	deletedDown := deleted(down)
	for _, name := range []string{"Content-Security-Policy", "Strict-Transport-Security", "X-Content-Type-Options", "Referrer-Policy", "Permissions-Policy"} {
		if !deletedDown[strings.ToLower(name)] {
			problems = append(problems, fmt.Sprintf("the proxy does not remove %s from the upstream response (header_down -%s): the client would see Caddy's copy and the upstream's, and only Caddy's is the site's (docs/adr/0045)", name, name))
		}
	}

	// The upstream's own headers that are nobody's but the platform's: a cookie it sets and the S3 headers
	// of the bucket it serves. The client sees the site's headers, not Edge Services'.
	if !deletedDown["set-cookie"] {
		problems = append(problems, "the proxy does not remove Set-Cookie from the upstream response (header_down -Set-Cookie): a cookie Edge Services or the bucket sets is the platform's, not the site's, and the front sets none (docs/adr/0045)")
	}

	// Only the prefix delete covers the bucket's headers: its X-Amz-Meta-* names are an open set, so no list
	// of named deletes, however long, stands for header_down -X-Amz-*.
	if !deletedDown["x-amz-*"] {
		problems = append(problems, "the proxy does not remove every X-Amz-* header from the upstream response: the bucket's X-Amz-Meta-* names are an open set no list of named deletes covers, so only header_down -X-Amz-* removes them all, and its X-Amz-Request-Id, X-Amz-Id-2 and X-Amz-Meta-* are Edge Services' and the store's, not the site's (docs/adr/0045)")
	}

	return problems
}

// deletesPrefix reports whether deleted holds the prefix deletion prefix*, or, instead, every one of the
// headers examples names: a header_down of the whole prefix and one of each header it covers remove the same
// headers, and the check accepts either. Only a prefix whose names are a closed set may fall back to the
// examples: X-Envoy's are, the bucket's X-Amz-Meta-* are not, and its check wants the prefix alone.
func deletesPrefix(deleted map[string]bool, prefix string, examples ...string) bool {
	if deleted[strings.ToLower(prefix)+"*"] {
		return true
	}

	for _, name := range examples {
		if !deleted[strings.ToLower(name)] {
			return false
		}
	}

	return true
}

// refusedLoggers are the three loggers the front's global log must silence when it keeps writing: Caddy's
// http.log.error writes every request a handler fails with the request's remote_ip and client_ip, its uri
// and its headers, http.handlers.reverse_proxy writes the proxy's own failed round trip the same way, and
// http.stdlib is Go's server error log, whose panic line names the remote address. The platform ships the
// container's output to Cockpit, where such a line is a third party's record of a visitor (docs/adr/0045,
// *The log*).
var refusedLoggers = []string{"http.log.error", "http.handlers.reverse_proxy", "http.stdlib"}

// checkRefusedDirectives refuses the directives the design turns off, and holds the global log: no site
// block may turn an access log on, and the one global log must either exclude the three loggers that write
// the visitor's data or write nothing at all (docs/adr/0045).
func checkRefusedDirectives(f *caddyfile) []string {
	var problems []string

	globals := f.globalBlock()
	logs := 0

	for _, n := range f.all() {
		name, _ := n.head()
		switch name {
		case "log":
			if n.parent != globals {
				problems = append(problems, fmt.Sprintf("line %d: the Caddyfile turns an access log on: a site block's log writes the visitor's address, the request line it asked for and the headers it sent, for every request, and the platform ships the container's output to Cockpit (docs/adr/0045)", n.line))

				continue
			}

			logs++
			problems = append(problems, checkGlobalLog(n)...)
		case "encode":
			problems = append(problems, fmt.Sprintf("line %d: the Caddyfile holds an encode directive: the module must keep the Content-Encoding: gzip the deploy uploaded (docs/adr/0043, docs/adr/0045)", n.line))
		case "cache":
			problems = append(problems, fmt.Sprintf("line %d: the Caddyfile holds a cache directive: Edge Services caches, the container does not (docs/adr/0045)", n.line))
		case "tls":
			problems = append(problems, fmt.Sprintf("line %d: the Caddyfile holds a tls directive: the platform terminates TLS and auto_https is off (docs/adr/0045)", n.line))
		}
	}

	if logs == 0 {
		problems = append(problems, fmt.Sprintf("the global options hold no log: Caddy's default logger still writes %s, each with the request's remote_ip and client_ip, its uri and its headers, and the platform ships the container's output to Cockpit, so the global options hold either `log { exclude %s }` or `log { output discard }` (docs/adr/0045)", strings.Join(refusedLoggers, " and "), strings.Join(refusedLoggers, " ")))
	}

	return problems
}

// checkGlobalLog holds the one shape the global log may have (docs/adr/0045): its block excludes every
// logger that writes the visitor's data, or its output is discarded. Either exactly: a named log, another
// output beside the exclusion, or a setting of its own beside the one that decides what is written leaves
// Caddy's logger writing what the front must not keep.
func checkGlobalLog(n *node) []string {
	want := fmt.Sprintf("either `log { exclude %s }` or `log { output discard }`", strings.Join(refusedLoggers, " "))

	if len(n.tokens) != 1 || len(n.children) != 1 {
		return []string{fmt.Sprintf("line %d: the global log is %q, want %s (docs/adr/0045)", n.line, describe(n), want)}
	}

	name, args := n.children[0].head()

	switch {
	case name == "exclude" && excludesEvery(args, refusedLoggers):
		return nil
	case name == "output" && slices.Equal(args, []string{"discard"}):
		return nil
	}

	return []string{fmt.Sprintf("line %d: the global log is %q, want %s (docs/adr/0045)", n.line, describe(n), want)}
}

// excludesEvery reports whether args names every logger in want, as an exclude line of the global log does.
func excludesEvery(args, want []string) bool {
	if len(args) != len(want) {
		return false
	}

	for _, name := range want {
		if !slices.Contains(args, name) {
			return false
		}
	}

	return true
}

// describe returns a directive of the file as the source writes it, its block's lines after it, so a
// message quotes what the file holds rather than only the part of it the check read.
func describe(n *node) string {
	var b strings.Builder

	b.WriteString(strings.Join(n.tokens, " "))

	for _, child := range n.children {
		b.WriteString(" { ")
		b.WriteString(strings.Join(child.tokens, " "))
		b.WriteString(" }")
	}

	return b.String()
}

// report fails the test on every problem a check found.
func report(t *testing.T, problems []string) {
	t.Helper()

	for _, problem := range problems {
		t.Error(problem)
	}
}

// broken returns the designed configuration with one of its lines replaced by
// another, so a check can be held to the configuration that breaks its rule.
func broken(from, to string) string {
	if !strings.Contains(designedCaddyfile, from) {
		panic(fmt.Sprintf("the designed configuration holds no %q", from))
	}

	return strings.Replace(designedCaddyfile, from, to, 1)
}

// withLine returns the designed configuration with line added to the site
// block, on a line of its own, for the directives a configuration must not hold
// at all. The line is followed by a newline: the site block's own first line
// follows it, and a line glued to that one would be a line of its own tokens
// rather than the directive the case names.
func withLine(line string) string {
	const site = "\n:8080 {\n"

	return strings.Replace(designedCaddyfile, site, site+line+"\n", 1)
}

// parseFile parses src as a Caddyfile.
func parseFile(src string) *caddyfile {
	return &caddyfile{src: src, nodes: parseCaddyfile(tokenize(src))}
}

// token is one token of a Caddyfile and the line it starts on.
type token struct {
	text string
	line int
}

// tokenize splits a Caddyfile into tokens the way Caddy's own lexer does for
// the constructs infra/site/Caddyfile may hold: white space separates tokens
// and a newline ends a line, # starts a comment when it begins a token and the
// comment runs to the end of the line, "quotes" group a token (with \" and \\
// escaped) and `backticks` group one verbatim, and a brace is an ordinary
// character: a lone { or } is a token of its own, so a block's braces are
// tokens, and a placeholder such as {http.request.uri} stays inside its
// directive's token. A \ at the end of a line, which Caddy reads as a
// continuation, is not read here: no front of this repository needs one.
func tokenize(src string) []token {
	var toks []token

	line := 1
	atTokenStart := true

	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == '\n':
			line++
			i++
			atTokenStart = true
		case c == ' ' || c == '\t' || c == '\r':
			i++
			atTokenStart = true
		case c == '#' && atTokenStart:
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"' || c == '`':
			quote := c
			start := line
			i++

			var text strings.Builder
			for i < len(src) && src[i] != quote {
				if quote == '"' && src[i] == '\\' && i+1 < len(src) {
					i++
					if src[i] != 'n' {
						text.WriteByte(src[i])
					} else {
						text.WriteByte('\n')
					}
					i++

					continue
				}
				if src[i] == '\n' {
					line++
				}
				text.WriteByte(src[i])
				i++
			}
			if i < len(src) {
				i++ // the closing quote
			}

			toks = append(toks, token{text: text.String(), line: start})
			atTokenStart = false
		default:
			start := line

			var text strings.Builder
			for i < len(src) && !isSpace(src[i]) {
				text.WriteByte(src[i])
				i++
			}

			toks = append(toks, token{text: text.String(), line: start})
			atTokenStart = false
		}
	}

	return toks
}

// isSpace reports whether b separates tokens.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// node is one directive or block of a Caddyfile: the tokens of its line and,
// when that line ends in {, the nodes of its block.
type node struct {
	line     int
	tokens   []string
	children []*node
	parent   *node
}

// directiveTokens returns the node's tokens without the name of a matcher it
// defines: a line @name expression ... defines @name with the expression that
// follows it.
func (n *node) directiveTokens() []string {
	if len(n.tokens) > 0 && strings.HasPrefix(n.tokens[0], "@") {
		return n.tokens[1:]
	}

	return n.tokens
}

// head returns the directive's name and the tokens after it, the matcher
// definition's name skipped.
func (n *node) head() (string, []string) {
	toks := n.directiveTokens()
	if len(toks) == 0 {
		return "", nil
	}

	return toks[0], toks[1:]
}

// parseCaddyfile parses the tokens into the file's top-level nodes.
func parseCaddyfile(toks []token) []*node {
	i := 0

	return parseNodes(toks, &i, nil)
}

// parseNodes parses the nodes of one block from *i, up to its closing } or the
// end of the tokens, and leaves *i past the }.
func parseNodes(toks []token, i *int, parent *node) []*node {
	var nodes []*node

	for *i < len(toks) {
		if toks[*i].text == "}" {
			*i++

			return nodes
		}

		n := &node{line: toks[*i].line, parent: parent}

		var words []string

		opened := false
		for *i < len(toks) && toks[*i].line == n.line {
			if toks[*i].text == "{" {
				*i++
				opened = true

				break
			}
			words = append(words, toks[*i].text)
			*i++
		}

		n.tokens = words
		if opened {
			n.children = parseNodes(toks, i, n)
		}
		nodes = append(nodes, n)
	}

	return nodes
}

// caddyfile is infra/site/Caddyfile, parsed, with the lookups the checks make.
type caddyfile struct {
	src   string
	nodes []*node
}

// readCaddyfile reads and parses infra/site/Caddyfile, from the repository
// root. A missing file fails the test: it is the configuration the container
// runs, and nothing else is a front to hold.
func readCaddyfile(t *testing.T) *caddyfile {
	t.Helper()

	path := filepath.Join(theRepositoryRoot(t), filepath.FromSlash(caddyfilePath))

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v: the container in infra/site runs this file byte for byte, so the front has no configuration to test until it exists (docs/adr/0045)", caddyfilePath, err)
	}

	return parseFile(string(src))
}

// theRepositoryRoot returns the module's root, two directories above the
// package's own, as the other test packages find it.
func theRepositoryRoot(t *testing.T) string {
	t.Helper()

	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}

	return root
}

// repositoryRoot returns the module's root for the behaviour test's harness,
// which runs before any test and has no *testing.T to fail with.
func repositoryRoot() (string, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", fmt.Errorf("resolve the module root: %w", err)
	}

	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("the module root %s holds no go.mod: %w", root, err)
	}

	return root, nil
}

// globalBlock returns the leading block of global options, whose line carries
// no name, or nil when the file does not begin with one.
func (f *caddyfile) globalBlock() *node {
	if f == nil || len(f.nodes) == 0 {
		return nil
	}

	if len(f.nodes[0].tokens) != 0 || len(f.nodes[0].children) == 0 {
		return nil
	}

	return f.nodes[0]
}

// sites returns the site blocks, every top-level block but the global options.
func (f *caddyfile) sites() []*node {
	var sites []*node

	for i, n := range f.nodes {
		if i == 0 && len(n.tokens) == 0 && len(n.children) > 0 {
			continue // the global options
		}
		sites = append(sites, n)
	}

	return sites
}

// topTokens returns the tokens of every top-level line, for a message naming
// what the file began with instead.
func (f *caddyfile) topTokens() [][]string {
	out := make([][]string, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, n.tokens)
	}

	return out
}

// all returns every node of the file, at every depth, in document order.
func (f *caddyfile) all() []*node {
	var out []*node

	var walk func([]*node)
	walk = func(nodes []*node) {
		for _, n := range nodes {
			out = append(out, n)
			walk(n.children)
		}
	}

	walk(f.nodes)

	return out
}

// matchers returns the matcher definitions the file holds, by their name with
// the @: a line whose first token is @name defines it.
func (f *caddyfile) matchers() map[string][]string {
	defs := map[string][]string{}
	for _, n := range f.all() {
		if len(n.tokens) > 0 && strings.HasPrefix(n.tokens[0], "@") {
			defs[n.tokens[0]] = n.tokens[1:]
		}
	}

	return defs
}

// guarded reports whether n sits inside a block whose matcher names another
// matcher, or a chain of them, that tests the request's path as starting with
// /: the expression {http.request.uri.path}.startsWith("/") or the equivalent
// path_regexp. A line naming a matcher (@a @b) stands for the matchers it
// names.
func (f *caddyfile) guarded(n *node) bool {
	defs := f.matchers()

	var defines func(name string, seen map[string]bool) bool
	defines = func(name string, seen map[string]bool) bool {
		if seen[name] {
			return false
		}
		seen[name] = true

		toks, ok := defs[name]
		if !ok {
			return false
		}

		if isPathGuard(toks) {
			return true
		}

		for _, tok := range toks {
			if strings.HasPrefix(tok, "@") && defines(tok, seen) {
				return true
			}
		}

		return false
	}

	for cur := n.parent; cur != nil; cur = cur.parent {
		for _, tok := range cur.tokens {
			if strings.HasPrefix(tok, "@") && defines(tok, map[string]bool{}) {
				return true
			}
		}
	}

	return false
}

// isPathGuard reports whether toks, a matcher definition, tests the request's
// path as starting with /.
func isPathGuard(toks []string) bool {
	if len(toks) == 0 {
		return false
	}
	if toks[0] == "path_regexp" {
		return true
	}

	joined := strings.Join(toks, " ")

	return strings.Contains(joined, "{http.request.uri.path}") &&
		strings.Contains(joined, "startsWith") &&
		(strings.Contains(joined, `("/")`) || strings.Contains(joined, `('/')`))
}

// withoutMatchers returns a directive's arguments without the inline matchers
// it names: a token starting with @, and a leading not.
func withoutMatchers(toks []string) []string {
	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		if strings.HasPrefix(tok, "@") || tok == "not" {
			continue
		}
		out = append(out, tok)
	}

	return out
}

// headerValues returns the response headers the file's header directives set,
// by their lower-case name: the one-line form (header Name value, a leading +
// skipped and a deletion with -Name ignored) and the block form (header {
// Name value }). It takes every node of the file, as caddyfile.all returns them.
func headerValues(nodes []*node) map[string][]string {
	out := map[string][]string{}

	for _, n := range nodes {
		if len(n.tokens) == 0 || n.tokens[0] != "header" {
			continue
		}

		switch {
		case len(n.tokens) == 1 && len(n.children) > 0:
			for _, child := range n.children {
				if len(child.tokens) < 2 {
					continue
				}
				name := strings.ToLower(strings.TrimPrefix(child.tokens[0], "+"))
				out[name] = append(out[name], strings.Join(child.tokens[1:], " "))
			}
		case len(n.tokens) >= 3 && !strings.HasPrefix(n.tokens[1], "-"):
			name := strings.ToLower(strings.TrimPrefix(n.tokens[1], "+"))
			out[name] = append(out[name], strings.Join(n.tokens[2:], " "))
		}
	}

	return out
}

// deleted returns the lower-case names of the fields a set of header_up or
// header_down operations removes, the ones written -Name.
func deleted(ops map[string][]string) map[string]bool {
	out := map[string]bool{}
	for name := range ops {
		if strings.HasPrefix(name, "-") {
			out[strings.ToLower(name[1:])] = true
		}
	}

	return out
}

// find returns the nodes pred accepts.
func find(nodes []*node, pred func(*node) bool) []*node {
	var out []*node
	for _, n := range nodes {
		if pred(n) {
			out = append(out, n)
		}
	}

	return out
}

// findExact returns the first node whose tokens are exactly want.
func findExact(nodes []*node, want ...string) *node {
	for _, n := range nodes {
		if slices.Equal(n.tokens, want) {
			return n
		}
	}

	return nil
}

// stripParents copies nodes without their parent links, so a test can compare a
// parse with what it expects.
func stripParents(nodes []*node) []*node {
	out := make([]*node, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, &node{line: n.line, tokens: n.tokens, children: stripParents(n.children)})
	}

	return out
}

// diffNodes reports how got differs from want, line by line, or "" when they
// are equal.
func diffNodes(want, got []*node) string {
	if len(want) != len(got) {
		return fmt.Sprintf("want %d nodes, got %d: %v", len(want), len(got), got)
	}

	var b strings.Builder
	for i := range want {
		if want[i].line != got[i].line {
			fmt.Fprintf(&b, "node %d: want line %d, got %d\n", i, want[i].line, got[i].line)
		}
		if !slices.Equal(want[i].tokens, got[i].tokens) {
			fmt.Fprintf(&b, "node %d: want tokens %q, got %q\n", i, want[i].tokens, got[i].tokens)
		}
		if d := diffNodes(want[i].children, got[i].children); d != "" {
			fmt.Fprintf(&b, "node %d's children: %s", i, d)
		}
	}

	return b.String()
}

// TestTokenize pins the tokenizer the checks parse with: the way it separates
// tokens, groups a quoted one, keeps a placeholder inside its token, reads a
// comment and numbers the lines. A bug here would let a check pass on a
// configuration it never read.
func TestTokenize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []token
	}{
		{
			name: "white space separates tokens",
			src:  "auto_https   off\n",
			want: []token{{text: "auto_https", line: 1}, {text: "off", line: 1}},
		},
		{
			name: "a newline ends a line and starts the next",
			src:  "a b\na b",
			want: []token{{text: "a", line: 1}, {text: "b", line: 1}, {text: "a", line: 2}, {text: "b", line: 2}},
		},
		{
			name: "a comment runs to the end of its line",
			src:  "header X nosniff # and this is not a directive\nredir / 308\n",
			want: []token{
				{text: "header", line: 1},
				{text: "X", line: 1},
				{text: "nosniff", line: 1},
				{text: "redir", line: 2},
				{text: "/", line: 2},
				{text: "308", line: 2},
			},
		},
		{
			name: "a # inside a token is part of it",
			src:  "location https://www.shift2eu.com/a%23b\n",
			want: []token{{text: "location", line: 1}, {text: "https://www.shift2eu.com/a%23b", line: 1}},
		},
		{
			name: "a quoted token keeps its spaces and loses its quotes",
			src:  "header Permissions-Policy \"camera=(), microphone=()\"\n",
			want: []token{
				{text: "header", line: 1},
				{text: "Permissions-Policy", line: 1},
				{text: "camera=(), microphone=()", line: 1},
			},
		},
		{
			name: "a backquoted token is verbatim",
			src:  "respond `a b\\n`\n",
			want: []token{{text: "respond", line: 1}, {text: "a b\\n", line: 1}},
		},
		{
			name: "a brace alone is a token, a placeholder stays inside its token",
			src:  ":8080 {\n\tredir https://www.shift2eu.com{http.request.uri} 308\n}\n",
			want: []token{
				{text: ":8080", line: 1},
				{text: "{", line: 1},
				{text: "redir", line: 2},
				{text: "https://www.shift2eu.com{http.request.uri}", line: 2},
				{text: "308", line: 2},
				{text: "}", line: 3},
			},
		},
		{
			name: "a quoted token inside a plain one is kept",
			src:  "@path expression {http.request.uri.path}.startsWith(\"/\")\n",
			want: []token{
				{text: "@path", line: 1},
				{text: "expression", line: 1},
				{text: `{http.request.uri.path}.startsWith("/")`, line: 1},
			},
		},
		{
			name: "an empty file holds no token",
			src:  "",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tokenize(tc.src)
			if !slices.Equal(got, tc.want) {
				t.Errorf("tokenize(%q):\n got %v\nwant %v", tc.src, got, tc.want)
			}
		})
	}
}

// TestParseCaddyfile pins the parse the checks read: a directive's tokens on
// their own line, a line ending in { opening a block whose nodes are its
// children, and a lone } closing it. A parse that lost a block's nesting would
// let a redirect pass the guard check.
func TestParseCaddyfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []*node
	}{
		{
			name: "one directive",
			src:  "auto_https off\n",
			want: []*node{{line: 1, tokens: []string{"auto_https", "off"}}},
		},
		{
			name: "a block",
			src:  ":8080 {\n\tredir / 308\n}\n",
			want: []*node{{line: 1, tokens: []string{":8080"}, children: []*node{
				{line: 2, tokens: []string{"redir", "/", "308"}},
			}}},
		},
		{
			name: "a block with no name",
			src:  "{\n\tauto_https off\n}\n:8080 {\n\tredir / 308\n}\n",
			want: []*node{
				{line: 1, children: []*node{{line: 2, tokens: []string{"auto_https", "off"}}}},
				{line: 4, tokens: []string{":8080"}, children: []*node{
					{line: 5, tokens: []string{"redir", "/", "308"}},
				}},
			},
		},
		{
			name: "a block inside a block",
			src:  ":8080 {\n\thandle @m {\n\t\tredir / 308\n\t}\n}\n",
			want: []*node{{line: 1, tokens: []string{":8080"}, children: []*node{
				{line: 2, tokens: []string{"handle", "@m"}, children: []*node{
					{line: 3, tokens: []string{"redir", "/", "308"}},
				}},
			}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The parent links are the parse's own bookkeeping; compare
			// without them.
			got := stripParents(parseCaddyfile(tokenize(tc.src)))
			if diff := diffNodes(tc.want, got); diff != "" {
				t.Errorf("parse(%q) differs (-want +got):\n%s", tc.src, diff)
			}
		})
	}
}
