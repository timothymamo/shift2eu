package site_test

import (
	"slices"
	"strings"
	"testing"
)

// This file holds the checks of a built page's head and of the resources it
// loads: the Content Security Policy (PRD #297, Layer 2), the DNS prefetch
// refusal, the scripts each page loads with their attributes and order, and the
// Subresource Integrity value of every script and stylesheet.

// cspDirective is one directive of the site's policy: its name and the exact
// sources it allows, in PRD #297's order.
type cspDirective struct {
	name    string
	sources []string
}

// cspPolicy is the policy every built page must carry, byte for byte in these
// directives and in no other: default-src 'none'; script-src 'self'
// 'wasm-unsafe-eval'; style-src 'self'; img-src 'none'; connect-src 'none';
// form-action 'none'; base-uri 'none'; object-src 'none'; worker-src 'none';
// child-src 'none'; frame-src 'none'; manifest-src 'none';
// require-trusted-types-for 'script'; trusted-types shift2eu-report.
//
// Every source list is exhaustive: a directive the policy does not name here is
// an added directive, and so a widening. font-src and media-src are absent on
// purpose (fonts and media fall to default-src 'none', so the site uses the
// system fonts), and report-uri and report-to would carry blocked URLs and
// samples to a host.
var cspPolicy = []cspDirective{
	{name: "default-src", sources: []string{"'none'"}},
	{name: "script-src", sources: []string{"'self'", "'wasm-unsafe-eval'"}},
	{name: "style-src", sources: []string{"'self'"}},
	{name: "img-src", sources: []string{"'none'"}},
	{name: "connect-src", sources: []string{"'none'"}},
	{name: "form-action", sources: []string{"'none'"}},
	{name: "base-uri", sources: []string{"'none'"}},
	{name: "object-src", sources: []string{"'none'"}},
	{name: "worker-src", sources: []string{"'none'"}},
	{name: "child-src", sources: []string{"'none'"}},
	{name: "frame-src", sources: []string{"'none'"}},
	{name: "manifest-src", sources: []string{"'none'"}},
	{name: "require-trusted-types-for", sources: []string{"'script'"}},
	{name: "trusted-types", sources: []string{"shift2eu-report"}},
}

// cspPolicyString is cspPolicy as the pages spell it, which the failure messages
// quote so a page that drops a directive names the whole policy it must carry.
func cspPolicyString() string {
	parts := make([]string, 0, len(cspPolicy))

	for _, directive := range cspPolicy {
		parts = append(parts, directive.name+" "+strings.Join(directive.sources, " "))
	}

	return strings.Join(parts, "; ")
}

// checkCSP parses a policy and returns every problem with it: a missing
// directive, an added or duplicated one, a widened or narrowed source list, a
// duplicate source, a violation-reporting directive and 'allow-duplicates'.
func checkCSP(policy string) []string {
	var problems []string

	seen := make(map[string]bool)
	expected := make(map[string]cspDirective, len(cspPolicy))

	for _, directive := range cspPolicy {
		expected[directive.name] = directive
	}

	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}

		name, sources := fields[0], fields[1:]

		switch name {
		case "report-uri", "report-to":
			problems = append(problems, "the directive "+name+" reports violations to a host, which would carry the blocked URL and a sample of the page: remove it")

			continue
		case "font-src", "media-src":
			problems = append(problems, "the directive "+name+" is not part of the policy: fonts and media fall to default-src 'none', so the site uses the system fonts and ships no media")

			continue
		}

		want, known := expected[name]
		if !known {
			problems = append(problems, "the policy holds the directive "+name+", which is not part of it: a directive added beside the policy widens it")

			continue
		}

		if seen[name] {
			problems = append(problems, "the policy names the directive "+name+" more than once")
		}

		seen[name] = true

		if slices.Contains(sources, "'allow-duplicates'") {
			problems = append(problems, "the directive "+name+" allows 'allow-duplicates', which lets a second Trusted Types policy of the report's name exist: the page creates exactly one policy")
		}

		if duplicate := firstDuplicate(sources); duplicate != "" {
			problems = append(problems, "the directive "+name+" names "+duplicate+" twice")
		}

		if diff := sortedDiff(want.sources, sources); diff != "" {
			problems = append(problems, "the directive "+name+" allows "+strings.Join(sources, " ")+", want exactly "+strings.Join(want.sources, " ")+" (the sources differ: "+diff+")")
		}
	}

	for _, directive := range cspPolicy {
		if !seen[directive.name] {
			problems = append(problems, "the policy is missing the directive "+directive.name+" "+strings.Join(directive.sources, " "))
		}
	}

	return problems
}

// firstDuplicate returns the first value that appears twice in values, or "".
func firstDuplicate(values []string) string {
	seen := make(map[string]bool, len(values))

	for _, value := range values {
		if seen[value] {
			return value
		}

		seen[value] = true
	}

	return ""
}

// sortedDiff returns "" when the two source lists name the same sources, and a
// rendering of them otherwise, so a failure says what moved without depending on
// the order a policy spells its sources.
func sortedDiff(want, got []string) string {
	sortedWant := slices.Clone(want)
	sortedGot := slices.Clone(got)

	slices.Sort(sortedWant)
	slices.Sort(sortedGot)

	if slices.Equal(sortedWant, sortedGot) {
		return ""
	}

	return "-" + strings.Join(sortedWant, " ") + " +" + strings.Join(sortedGot, " ")
}

// TestContentSecurityPolicyIsExact pins PRD #297's Layer 2: every built page
// carries the policy of cspPolicy in a <meta http-equiv> element that is the
// first child of <head>, before any <link> or <script>, so it applies from the
// first byte the page loads.
func TestContentSecurityPolicyIsExact(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, page)
			head := doc.head(t)

			if len(head.children) == 0 {
				t.Fatalf("%s's <head> holds no element, so it carries no Content Security Policy", page)
			}

			first := head.children[0]

			if first.tag != "meta" {
				t.Fatalf("%s's first element in <head> is <%s>, want the <meta http-equiv=\"Content-Security-Policy\"> element: the policy must come before any <link> or <script>", page, first.tag)
			}

			if got, _ := first.attr("http-equiv"); !strings.EqualFold(got, "content-security-policy") {
				t.Fatalf("%s's first element in <head> is a <meta> with http-equiv %q, want Content-Security-Policy", page, got)
			}

			policies := 0

			for _, meta := range doc.find("meta") {
				if equiv, ok := meta.attr("http-equiv"); ok && strings.EqualFold(equiv, "content-security-policy") {
					policies++
				}
			}

			if policies != 1 {
				t.Errorf("%s carries %d Content Security Policy elements, want exactly one: a second policy is an intersection nobody reviews", page, policies)
			}

			policy, ok := first.attr("content")
			if !ok {
				t.Fatalf("%s's Content Security Policy element has no content attribute", page)
			}

			for _, problem := range checkCSP(policy) {
				t.Errorf("%s: %s", page, problem)
			}

			if len(checkCSP(policy)) != 0 {
				t.Errorf("%s's whole policy must be: %s", page, cspPolicyString())
			}
		})
	}
}

// TestContentSecurityPolicyCheckRejects pins that the check above fires: each
// row is a policy PRD #297 refuses, and the check must report it and name what
// is wrong.
func TestContentSecurityPolicyCheckRejects(t *testing.T) {
	t.Parallel()

	// policy builds the exact policy with one directive replaced, added or
	// removed, so each row differs from a passing policy in one thing.
	policy := func(change func(map[string][]string)) string {
		directives := make(map[string][]string, len(cspPolicy))

		for _, directive := range cspPolicy {
			directives[directive.name] = slices.Clone(directive.sources)
		}

		change(directives)

		parts := make([]string, 0, len(directives))
		names := make([]string, 0, len(directives))

		for name := range directives {
			names = append(names, name)
		}

		slices.Sort(names)

		for _, name := range names {
			if directives[name] == nil {
				continue
			}

			parts = append(parts, name+" "+strings.Join(directives[name], " "))
		}

		return strings.Join(parts, "; ")
	}

	tests := []struct {
		name   string
		policy string
		want   string
	}{
		{
			name:   "the exact policy is accepted",
			policy: cspPolicyString(),
		},
		{
			name:   "a missing directive",
			policy: policy(func(d map[string][]string) { delete(d, "worker-src") }),
			want:   "missing the directive worker-src",
		},
		{
			name:   "a violation report",
			policy: policy(func(d map[string][]string) { d["report-uri"] = []string{"https://example.com/report"} }),
			want:   "report-uri",
		},
		{
			name:   "a report-to directive",
			policy: policy(func(d map[string][]string) { d["report-to"] = []string{"csp"} }),
			want:   "report-to",
		},
		{
			name:   "a font source",
			policy: policy(func(d map[string][]string) { d["font-src"] = []string{"'self'"} }),
			want:   "font-src",
		},
		{
			name:   "a media source",
			policy: policy(func(d map[string][]string) { d["media-src"] = []string{"'self'"} }),
			want:   "media-src",
		},
		{
			name:   "a widened script-src",
			policy: policy(func(d map[string][]string) { d["script-src"] = []string{"'self'", "'unsafe-eval'"} }),
			want:   "script-src",
		},
		{
			name: "an unsafe-inline script-src",
			policy: policy(func(d map[string][]string) {
				d["script-src"] = []string{"'self'", "'wasm-unsafe-eval'", "'unsafe-inline'"}
			}),
			want: "script-src",
		},
		{
			name:   "an own-origin img-src",
			policy: policy(func(d map[string][]string) { d["img-src"] = []string{"'self'"} }),
			want:   "img-src",
		},
		{
			name:   "a widened connect-src",
			policy: policy(func(d map[string][]string) { d["connect-src"] = []string{"'self'"} }),
			want:   "connect-src",
		},
		{
			name:   "a widened worker-src",
			policy: policy(func(d map[string][]string) { d["worker-src"] = []string{"'self'"} }),
			want:   "worker-src",
		},
		{
			name:   "allow-duplicates",
			policy: policy(func(d map[string][]string) { d["trusted-types"] = []string{"shift2eu-report", "'allow-duplicates'"} }),
			want:   "allow-duplicates",
		},
		{
			name:   "a second Trusted Types policy name",
			policy: policy(func(d map[string][]string) { d["trusted-types"] = []string{"shift2eu-report", "other"} }),
			want:   "trusted-types",
		},
		{
			name: "a duplicated directive",
			policy: policy(func(map[string][]string) {}) + "; " +
				"default-src 'none'",
			want: "default-src",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			problems := checkCSP(tc.policy)

			if tc.want == "" {
				if len(problems) != 0 {
					t.Errorf("checkCSP(%q) reported %v, want no problem", tc.policy, problems)
				}

				return
			}

			if !slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, tc.want) }) {
				t.Errorf("checkCSP(%q) reported %v, want a problem naming %q", tc.policy, problems, tc.want)
			}
		})
	}
}

// TestNoDnsPrefetchHints pins that no page asks the browser to resolve or fetch
// a name ahead of time: every page carries
// <meta http-equiv="x-dns-prefetch-control" content="off">, and no page holds a
// <link rel> of dns-prefetch, preconnect, prefetch or prerender (PRD #297,
// Layer 1). A DNS prefetch leaves the browser outside the CSP's reach.
func TestNoDnsPrefetchHints(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)
	refused := []string{"dns-prefetch", "preconnect", "prefetch", "prerender"}

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, page)

			off := 0

			for _, meta := range doc.find("meta") {
				equiv, _ := meta.attr("http-equiv")
				content, _ := meta.attr("content")

				if strings.EqualFold(equiv, "x-dns-prefetch-control") && strings.EqualFold(content, "off") {
					off++
				}
			}

			if off != 1 {
				t.Errorf("%s carries %d <meta http-equiv=\"x-dns-prefetch-control\" content=\"off\"> elements, want exactly one: the browser must resolve no name ahead of time", page, off)
			}

			for _, link := range doc.find("link") {
				rel, _ := link.attr("rel")

				for _, token := range strings.Fields(strings.ToLower(rel)) {
					if slices.Contains(refused, token) {
						t.Errorf("%s holds <link rel=%q>: the page asks the browser to resolve or fetch a name ahead of time", page, rel)
					}
				}
			}
		})
	}
}

// TestScriptsPerPage pins the scripts each page loads, with their attributes and
// order (PRD #297, Layer 2): the assessment page loads exactly wasm_exec.js, the
// module script and the page script, all classic scripts with defer and never
// async or type="module", in that order, so the global is set before the page
// script reads it; the privacy and verification pages load the page script
// alone, which runs the framing guard and the unregister function and reads no
// module.
func TestScriptsPerPage(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	wasmExec, _ := bundle.asset(t, "wasm_exec", ".js")
	module, _ := bundle.asset(t, "module", ".js")
	page, _ := bundle.asset(t, "page", ".js")

	tests := []struct {
		page string
		want []string
	}{
		{page: "index.html", want: []string{wasmExec, module, page}},
		{page: "about.html", want: []string{page}},
		{page: "inventory.html", want: []string{page}},
		{page: "privacy.html", want: []string{page}},
		{page: "verify.html", want: []string{page}},
	}

	for _, tc := range tests {
		t.Run(tc.page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, tc.page)

			scripts := doc.find("script")

			got := make([]string, 0, len(scripts))

			for _, script := range scripts {
				src, ok := script.attr("src")
				if !ok {
					t.Errorf("%s holds an inline <script> with no src: the page runs no inline script (style-src and script-src name no 'unsafe-inline')", tc.page)

					got = append(got, "<inline>")

					continue
				}

				got = append(got, src)

				if !script.hasAttr("defer") {
					t.Errorf("%s loads %s without defer: the scripts are deferred classic scripts, so they run in document order after parsing", tc.page, src)
				}

				if script.hasAttr("async") {
					t.Errorf("%s loads %s with async: an async script runs whenever it arrives, so the module global may be unset when the page script reads it", tc.page, src)
				}

				if kind, ok := script.attr("type"); ok && kind != "" && !strings.EqualFold(kind, "text/javascript") {
					t.Errorf("%s loads %s with type=%q: the scripts are classic scripts, never type=\"module\"", tc.page, src, kind)
				}
			}

			if diff := diffNames(tc.want, got); diff != "" {
				t.Errorf("%s loads the scripts (-want +got):\n%s", tc.page, diff)
			}
		})
	}
}

// TestAssessmentPageIsMarked pins the fixed attribute the page script reads to
// know whether to decode the module: the assessment page's <body> carries
// data-page="assessment", and every other page, the about and inventory pages
// included, carries no data-page at all, so it loads no module (PRD #297, Layer
// 2). Nothing else reads the attribute: site/page.js compares it with
// "assessment" and nothing else (reviewer of issue #528).
func TestAssessmentPageIsMarked(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	tests := []struct {
		page string
		want string
	}{
		{page: "index.html", want: "assessment"},
		{page: "about.html"},
		{page: "inventory.html"},
		{page: "privacy.html"},
		{page: "verify.html"},
	}

	for _, tc := range tests {
		t.Run(tc.page, func(t *testing.T) {
			t.Parallel()

			got, ok := bundle.html(t, tc.page).body(t).attr("data-page")

			if tc.want == "" {
				if ok {
					t.Errorf("%s's <body> carries data-page=%q, want none: the page loads neither wasm_exec.js nor the module script and reads no module", tc.page, got)
				}

				return
			}

			if got != tc.want {
				t.Errorf("%s's <body> carries data-page=%q, want %q: the page script reads the module only on the page whose body names the assessment (PRD #297, Layer 2)", tc.page, got, tc.want)
			}
		})
	}
}

// TestSubresourceIntegrity pins that every script and stylesheet is loaded with
// Subresource Integrity, SHA-384: each <script src> and <link rel="stylesheet">
// carries an integrity attribute of that file's SHA-384, so a changed asset is
// refused by the browser and the bundle's files are pinned by the page that
// loads them (PRD #297, Layer 2).
func TestSubresourceIntegrity(t *testing.T) {
	t.Parallel()

	bundle := theBundle(t)

	for _, page := range pageNames {
		t.Run(page, func(t *testing.T) {
			t.Parallel()

			doc := bundle.html(t, page)

			checkIntegrity := func(element *element, kind string) {
				ref, ok := element.attr(kind)
				if !ok {
					t.Errorf("%s holds a <%s> with no %s", page, element.tag, kind)

					return
				}

				file := bundleFile(t, bundle, ref)
				integrity, ok := element.attr("integrity")

				if !ok {
					t.Errorf("%s loads %s with no integrity attribute: every script and stylesheet is loaded with Subresource Integrity, SHA-384", page, ref)

					return
				}

				if want := "sha384-" + sha384Base64(bundle.read(t, file)); integrity != want {
					t.Errorf("%s loads %s with integrity=%q, want %q, the SHA-384 of the file's bytes", page, ref, integrity, want)
				}
			}

			for _, script := range doc.find("script") {
				checkIntegrity(script, "src")
			}

			stylesheets := 0

			for _, link := range doc.find("link") {
				rel, _ := link.attr("rel")

				if !strings.EqualFold(strings.TrimSpace(rel), "stylesheet") {
					continue
				}

				stylesheets++

				checkIntegrity(link, "href")
			}

			if stylesheets != 1 {
				t.Errorf("%s loads %d stylesheets, want exactly one, content-addressed and pinned by SRI", page, stylesheets)
			}
		})
	}
}
