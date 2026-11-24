package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// BodyRule names the check a request body must pass for its endpoint.
type BodyRule int

// The body rules of the endpoint table (ADR 0040, "The publish program's
// GitHub side").
const (
	// BodyNone is a call that sends no body.
	BodyNone BodyRule = iota
	// BodyBlob is exactly {content, encoding: "utf-8"}.
	BodyBlob
	// BodyTree is {base_tree: <40 hex>, tree: [one entry]}: the entry's path is
	// this run's report, its mode "100644", its type "blob", its sha 40 hex.
	BodyTree
	// BodyCommit is {message, tree: <40 hex>, parents: [the audited commit]}.
	BodyCommit
	// BodyRef is {ref: "refs/heads/<this run's branch>", sha: <40 hex>}.
	BodyRef
	// BodyPull is {title, body, head: this run's branch, base: "main",
	// maintainer_can_modify: false, draft: false}.
	BodyPull
	// BodyIssue is exactly {title, body, labels: ["security"]}.
	BodyIssue
	// BodyAdvisoryCreate holds only summary, description, severity and
	// vulnerabilities: no state, cve_id, credits or collaborators. summary and
	// description are required non-empty strings, severity a string when
	// present, and every vulnerabilities element an object of GitHub's four
	// keys, its package of exactly ecosystem and name.
	BodyAdvisoryCreate
	// BodyAdvisoryUpdate holds only summary, description, severity and state,
	// and state is present and exactly "draft".
	BodyAdvisoryUpdate
)

// Endpoint is one row of the fixed table of calls the publish program may
// make. Pattern is anchored at both ends when Allowed compiles it, and its
// placeholders are filled from the Scope, each value quoted:
//
//   - {repo}: repos/<owner>/<name> of the scope's repository;
//   - {run}: the scope's run ID;
//   - {branch}: security-audit/<date>-<run ID> of the scope;
//   - {commit}: the scope's audited commit;
//   - {sha}: any 40 lowercase hex;
//   - {tag}: a release tag v<major>.<minor>.<patch>;
//   - {digest}: sha256: and 64 lowercase hex;
//   - {ghsa}: a GHSA identifier.
//
// A row whose pattern or body rule needs the audited commit matches nothing
// for a scope without one (the artifact check's).
type Endpoint struct {
	Method  string
	Pattern string
	Body    BodyRule
}

// Endpoints returns a copy of the endpoint table: every call the publish
// program and the artifact check may make, and nothing else: no PUT, no
// DELETE, no merge or review of a pull request, no CVE request, no tag,
// release or other branch. The issue listing accepts pages 1 to 10 only: a
// listing longer than ten pages fails closed. The table itself is
// unexported, so no caller can change what Allowed checks.
func Endpoints() []Endpoint { return slices.Clone(endpoints) }

// endpoints is the endpoint table Allowed checks every call against.
var endpoints = []Endpoint{
	{Method: "GET", Pattern: `{repo}/actions/runs/{run}/artifacts\?per_page=100`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/releases/tags/{tag}`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/attestations/{digest}\?per_page=100`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/git/commits/{sha}`, Body: BodyNone},
	{Method: "GET", Pattern: `{repo}/git/ref/heads/{branch}`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/git/blobs`, Body: BodyBlob},
	{Method: "POST", Pattern: `{repo}/git/trees`, Body: BodyTree},
	{Method: "POST", Pattern: `{repo}/git/commits`, Body: BodyCommit},
	{Method: "POST", Pattern: `{repo}/git/refs`, Body: BodyRef},
	{Method: "GET", Pattern: `{repo}/compare/{commit}\.\.\.{sha}`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/pulls`, Body: BodyPull},
	{Method: "GET", Pattern: `{repo}/issues\?creator=shift2eu-audit%5Bbot%5D&state=all&per_page=100&page=(?:[1-9]|10)`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/issues`, Body: BodyIssue},
	{Method: "GET", Pattern: `{repo}/security-advisories\?per_page=100(?:&after=[A-Za-z0-9+/=_%-]{1,200})?`, Body: BodyNone},
	{Method: "POST", Pattern: `{repo}/security-advisories`, Body: BodyAdvisoryCreate},
	{Method: "PATCH", Pattern: `{repo}/security-advisories/{ghsa}`, Body: BodyAdvisoryUpdate},
}

// The placeholders whose values do not depend on the scope.
const (
	shaPattern    = `[0-9a-f]{40}`
	tagPattern    = `v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)`
	digestPattern = `sha256:[0-9a-f]{64}`
	ghsaPattern   = `GHSA-[23456789cfghjmpqrvwx]{4}(?:-[23456789cfghjmpqrvwx]{4}){2}`
)

var (
	sha40        = regexp.MustCompile(`^` + shaPattern + `$`)
	endpointSafe = regexp.MustCompile(`^[A-Za-z0-9._~%/?&=+:-]+$`)
)

// The report's branch and file of a run: the names the core's templates
// render (security-audit/<date>-<run ID>, docs/security/audits/<date>-<run
// ID>.md).
func branchOf(s Scope) string { return "security-audit/" + s.Date + "-" + s.RunID }

func reportPathOf(s Scope) string {
	return "docs/security/audits/" + s.Date + "-" + s.RunID + ".md"
}

// Allowed reports whether a call is in the endpoint table for the scope: its
// method and endpoint match one row, and its JSON body, nil for none, passes
// that row's body rule. A refusal wraps ErrEndpointRefused and names the rule,
// never the endpoint or the body.
func Allowed(scope Scope, method, endpoint string, body []byte) error {
	if err := validateScope(scope); err != nil {
		return fmt.Errorf("endpoint check: the scope is invalid: %w", ErrEndpointRefused)
	}

	path, _, _ := strings.Cut(endpoint, "?")
	if !endpointSafe.MatchString(endpoint) || strings.HasPrefix(endpoint, "-") ||
		slices.ContainsFunc(strings.Split(path, "/"), func(seg string) bool { return seg == "" || seg == "." || seg == ".." }) ||
		strings.Contains(endpoint, "://") {
		return fmt.Errorf("endpoint check: an endpoint gh could read as a flag, a URL or another path: %w", ErrEndpointRefused)
	}

	for _, row := range endpoints {
		if row.Method != method {
			continue
		}

		re, ok := compileRow(row, scope)
		if !ok || !re.MatchString(endpoint) {
			continue
		}

		if err := checkBody(row.Body, scope, body); err != nil {
			return fmt.Errorf("endpoint check: %w", err)
		}

		return nil
	}

	return fmt.Errorf("endpoint check: a method and endpoint outside the table: %w", ErrEndpointRefused)
}

// compileRow fills a row's placeholders from the scope and anchors it. It
// reports false for a row that needs the audited commit when the scope has
// none.
func compileRow(row Endpoint, scope Scope) (*regexp.Regexp, bool) {
	if strings.Contains(row.Pattern, "{commit}") && scope.Commit == "" {
		return nil, false
	}

	pattern := strings.NewReplacer(
		"{repo}", regexp.QuoteMeta("repos/"+scope.Repository),
		"{run}", regexp.QuoteMeta(scope.RunID),
		"{branch}", regexp.QuoteMeta(branchOf(scope)),
		"{commit}", regexp.QuoteMeta(scope.Commit),
		"{sha}", shaPattern,
		"{tag}", tagPattern,
		"{digest}", digestPattern,
		"{ghsa}", ghsaPattern,
	).Replace(row.Pattern)

	re, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		return nil, false
	}

	return re, true
}

// checkBody checks a body against its row's rule. Every rule but BodyNone
// wants one JSON object, with no key twice, and names its keys exactly.
func checkBody(rule BodyRule, scope Scope, body []byte) error {
	if rule == BodyNone {
		if len(body) != 0 {
			return fmt.Errorf("a body on a call that sends none: %w", ErrEndpointRefused)
		}

		return nil
	}

	obj, ok := decodeObject(body)
	if !ok {
		return fmt.Errorf("a write body that is no JSON object with distinct keys: %w", ErrEndpointRefused)
	}

	if !bodyRules[rule](obj, scope) {
		return fmt.Errorf("a body outside its endpoint's rule: %w", ErrEndpointRefused)
	}

	return nil
}

// bodyRules holds each write rule's check.
var bodyRules = map[BodyRule]func(map[string]json.RawMessage, Scope) bool{
	BodyBlob: func(o map[string]json.RawMessage, _ Scope) bool {
		return exactKeys(o, "content", "encoding") && isString(o["content"]) && isStringValue(o["encoding"], "utf-8")
	},
	BodyTree: func(o map[string]json.RawMessage, s Scope) bool {
		if !exactKeys(o, "base_tree", "tree") || !isSHA(o["base_tree"]) {
			return false
		}

		var entries []json.RawMessage
		if json.Unmarshal(o["tree"], &entries) != nil || len(entries) != 1 {
			return false
		}

		e, ok := decodeObject(entries[0])

		return ok && exactKeys(e, "path", "mode", "type", "sha") &&
			isStringValue(e["path"], reportPathOf(s)) && isStringValue(e["mode"], "100644") &&
			isStringValue(e["type"], "blob") && isSHA(e["sha"])
	},
	BodyCommit: func(o map[string]json.RawMessage, s Scope) bool {
		if s.Commit == "" || !exactKeys(o, "message", "tree", "parents") || !isString(o["message"]) || !isSHA(o["tree"]) {
			return false
		}

		var parents []json.RawMessage

		return json.Unmarshal(o["parents"], &parents) == nil && len(parents) == 1 && isStringValue(parents[0], s.Commit)
	},
	BodyRef: func(o map[string]json.RawMessage, s Scope) bool {
		return exactKeys(o, "ref", "sha") && isStringValue(o["ref"], "refs/heads/"+branchOf(s)) && isSHA(o["sha"])
	},
	BodyPull: func(o map[string]json.RawMessage, s Scope) bool {
		return exactKeys(o, "title", "body", "head", "base", "maintainer_can_modify", "draft") &&
			isString(o["title"]) && isString(o["body"]) && isStringValue(o["head"], branchOf(s)) &&
			isStringValue(o["base"], "main") && isFalse(o["maintainer_can_modify"]) && isFalse(o["draft"])
	},
	BodyIssue: func(o map[string]json.RawMessage, _ Scope) bool {
		if !exactKeys(o, "title", "body", "labels") || !isString(o["title"]) || !isString(o["body"]) {
			return false
		}

		var labels []json.RawMessage

		return json.Unmarshal(o["labels"], &labels) == nil && len(labels) == 1 && isStringValue(labels[0], "security")
	},
	BodyAdvisoryCreate: func(o map[string]json.RawMessage, _ Scope) bool {
		if !onlyKeys(o, "summary", "description", "severity", "vulnerabilities") ||
			!isNonEmptyString(o["summary"]) || !isNonEmptyString(o["description"]) || !stringsIfPresent(o, "severity") {
			return false
		}

		v, ok := o["vulnerabilities"]
		if !ok {
			return true
		}

		var list []json.RawMessage
		if json.Unmarshal(v, &list) != nil || list == nil {
			return false
		}

		return !slices.ContainsFunc(list, func(e json.RawMessage) bool { return !isVulnerability(e) })
	},
	BodyAdvisoryUpdate: func(o map[string]json.RawMessage, _ Scope) bool {
		return onlyKeys(o, "summary", "description", "severity", "state") &&
			stringsIfPresent(o, "summary", "description", "severity") && isStringValue(o["state"], "draft")
	},
}

// isVulnerability reports whether v is one element of a row-15 body's
// vulnerabilities: an object of only package, vulnerable_version_range,
// patched_versions and vulnerable_functions, no key twice. package is
// required, an object of exactly ecosystem and name, both strings; the range
// and the patched versions are a string or null, the functions an array of
// strings or null.
func isVulnerability(v json.RawMessage) bool {
	o, ok := decodeObject(v)
	if !ok || !onlyKeys(o, "package", "vulnerable_version_range", "patched_versions", "vulnerable_functions") {
		return false
	}

	pkg, ok := decodeObject(o["package"])
	if !ok || !exactKeys(pkg, "ecosystem", "name") || !isString(pkg["ecosystem"]) || !isString(pkg["name"]) {
		return false
	}

	for _, k := range []string{"vulnerable_version_range", "patched_versions"} {
		if s, present := o[k]; present && !isNull(s) && !isString(s) {
			return false
		}
	}

	functions, present := o["vulnerable_functions"]
	if !present || isNull(functions) {
		return true
	}

	var names []json.RawMessage
	if json.Unmarshal(functions, &names) != nil || names == nil {
		return false
	}

	return !slices.ContainsFunc(names, func(n json.RawMessage) bool { return !isString(n) })
}

// decodeObject reads exactly one JSON object and refuses a key twice, so a
// body cannot hold one value for this check and another for GitHub.
func decodeObject(data []byte) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))

	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}

	obj := map[string]json.RawMessage{}

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}

		key, ok := tok.(string)
		if !ok {
			return nil, false
		}

		if _, dup := obj[key]; dup {
			return nil, false
		}

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}

		obj[key] = value
	}

	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}

	return obj, true
}

// exactKeys reports whether o holds exactly keys.
func exactKeys(o map[string]json.RawMessage, keys ...string) bool {
	if len(o) != len(keys) {
		return false
	}

	return onlyKeys(o, keys...)
}

// onlyKeys reports whether every key of o is one of keys.
func onlyKeys(o map[string]json.RawMessage, keys ...string) bool {
	for k := range o {
		found := false

		for _, want := range keys {
			if k == want {
				found = true

				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

// stringsIfPresent reports whether each of keys that o holds is a string.
func stringsIfPresent(o map[string]json.RawMessage, keys ...string) bool {
	for _, k := range keys {
		if v, ok := o[k]; ok && !isString(v) {
			return false
		}
	}

	return true
}

func stringOf(v json.RawMessage) (string, bool) {
	var s string
	if v == nil || json.Unmarshal(v, &s) != nil || bytes.TrimSpace(v)[0] != '"' {
		return "", false
	}

	return s, true
}

func isString(v json.RawMessage) bool {
	_, ok := stringOf(v)

	return ok
}

func isNonEmptyString(v json.RawMessage) bool {
	s, ok := stringOf(v)

	return ok && s != ""
}

func isNull(v json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

func isStringValue(v json.RawMessage, want string) bool {
	s, ok := stringOf(v)

	return ok && s == want
}

func isSHA(v json.RawMessage) bool {
	s, ok := stringOf(v)

	return ok && sha40.MatchString(s)
}

func isFalse(v json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(v), []byte("false"))
}
