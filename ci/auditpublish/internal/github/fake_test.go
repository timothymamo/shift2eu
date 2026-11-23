package github_test

// The fake gh and the fake gitleaks every test of the GitHub side runs against
// (PRD #175 milestone 2: "tested against a fake gh with canned API
// responses"). Both are this test binary, started through a symbolic link whose
// name TestMain reads; each keeps its files next to the link, in a directory of
// the test's own.
//
// The fake gh answers from routes, records every call, and holds a guard of its
// own, written from the PRD and not from the library's endpoint table: a call
// outside the PRD's fixed set, or an advisory state other than draft, is a
// violation, answered 418, and fails the test at cleanup.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// fakeGHName and fakeGitleaksName are the links' names, which TestMain
	// reads.
	fakeGHName       = "gh"
	fakeGitleaksName = "gitleaks"

	// The fakes' files, next to their links.
	ghRoutesFile      = "gh.routes.json"
	ghStateFile       = "gh.state.json"
	ghCallsFile       = "gh.calls.jsonl"
	gitleaksCallsFile = "gitleaks.calls.jsonl"
	gitleaksExitFile  = "gitleaks.exit"
	gitleaksSleepFile = "gitleaks.sleep"

	// fakeGitleaksHit is the text the fake gitleaks reports as a leak (exit
	// 10) when its stdin holds it.
	fakeGitleaksHit = "FAKE-GITLEAKS-HIT"

	// fakeGitleaksLeakExit is the exit code the library asks gitleaks to use
	// for a leak (--exit-code 10).
	fakeGitleaksLeakExit = 10

	// fakeOutputMarker is what both fakes write to their stdout and stderr
	// besides their answer, so a test can show it reaches no output of the
	// program.
	fakeOutputMarker = "FAKE-TOOL-OUTPUT"

	// The two headers the library passes gh on every call.
	ghAcceptHeader  = "Accept: application/vnd.github+json"
	ghVersionHeader = "X-GitHub-Api-Version: 2022-11-28"

	// fakeGuardStatus is the status a violation is answered with.
	fakeGuardStatus = 418
)

// Route is one canned answer of the fake gh. Method and Endpoint match a call
// exactly, the endpoint with its query string. Several routes with one method
// and endpoint answer successive calls in order, and the last one repeats.
type Route struct {
	Method   string
	Endpoint string
	// Status is the HTTP status the answer's status line carries.
	Status int
	// Body is marshalled to JSON as the answer's body; a json.RawMessage is
	// written verbatim, nil as null.
	Body any
	// Link is the answer's Link header, none when empty.
	Link string
	// Trailing is written after the body, verbatim: data after the JSON
	// document, which no JSON answer of GitHub's carries.
	Trailing string
	// Delay is how long the fake waits before answering, none when zero: a
	// route that outlives the caller's deadline.
	Delay time.Duration
}

// routeRecord is a Route as gh.routes.json holds it.
type routeRecord struct {
	Method   string          `json:"method"`
	Endpoint string          `json:"endpoint"`
	Status   int             `json:"status"`
	Body     json.RawMessage `json:"body"`
	Link     string          `json:"link,omitempty"`
	Trailing string          `json:"trailing,omitempty"`
	DelayMS  int64           `json:"delay_ms,omitempty"`
}

// Call is one invocation of the fake gh as it recorded it.
type Call struct {
	// Args are gh's arguments, without argv[0].
	Args []string `json:"args"`
	// Env is the whole environment gh was started with.
	Env []string `json:"env"`
	// Method and Endpoint are read from Args; empty when Args had another
	// shape.
	Method   string `json:"method"`
	Endpoint string `json:"endpoint"`
	// Body is stdin, when --input - was given and stdin was not empty.
	Body json.RawMessage `json:"body,omitempty"`
	// Unmatched is a call no route answered: it got 404.
	Unmatched bool `json:"unmatched"`
	// Violation names the fake's guard rule the call broke; empty when none.
	Violation string `json:"violation,omitempty"`
}

// FakeGH is a fake gh in a directory of the test's own.
type FakeGH struct {
	// Path is the fake's absolute path, given to the library as gh's.
	Path string

	dir string

	mu              sync.Mutex
	allowViolations bool
}

// newFakeGH makes a fake gh that answers with routes. At cleanup it fails the
// test on any call that broke the fake's guard, unless AllowViolations was
// called.
func newFakeGH(t *testing.T, routes ...Route) *FakeGH {
	t.Helper()

	dir := fakeDir(t, fakeGHName)
	records := make([]routeRecord, 0, len(routes))

	for _, r := range routes {
		body, err := json.Marshal(r.Body)
		if err != nil {
			t.Fatalf("newFakeGH: the body of %s %s: %v", r.Method, r.Endpoint, err)
		}

		records = append(records, routeRecord{
			Method:   r.Method,
			Endpoint: r.Endpoint,
			Status:   r.Status,
			Body:     body,
			Link:     r.Link,
			Trailing: r.Trailing,
			DelayMS:  r.Delay.Milliseconds(),
		})
	}

	writeJSON(t, filepath.Join(dir, ghRoutesFile), records)

	f := &FakeGH{Path: filepath.Join(dir, fakeGHName), dir: dir}

	t.Cleanup(func() {
		f.mu.Lock()
		allowed := f.allowViolations
		f.mu.Unlock()

		if allowed {
			return
		}

		for _, c := range f.Calls(t) {
			if c.Violation != "" {
				t.Errorf("the fake gh refused %s %s: %s (PRD #175: only the fixed endpoints, every advisory state draft)", c.Method, c.Endpoint, c.Violation)
			}
		}
	})

	return f
}

// AllowViolations stops the cleanup from failing the test on a violation: for
// a test of the fake's own guard.
func (f *FakeGH) AllowViolations() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.allowViolations = true
}

// Calls returns every call the fake recorded, in order.
func (f *FakeGH) Calls(t *testing.T) []Call {
	t.Helper()

	return readJSONLines[Call](t, filepath.Join(f.dir, ghCallsFile))
}

// Unmatched returns the calls no route answered.
func (f *FakeGH) Unmatched(t *testing.T) []Call {
	t.Helper()

	var out []Call

	for _, c := range f.Calls(t) {
		if c.Unmatched {
			out = append(out, c)
		}
	}

	return out
}

// GitleaksCall is one invocation of the fake gitleaks.
type GitleaksCall struct {
	Args  []string `json:"args"`
	Env   []string `json:"env"`
	Stdin string   `json:"stdin"`
}

// FakeGitleaks is a fake gitleaks in a directory of the test's own.
type FakeGitleaks struct {
	// Path is the fake's absolute path, given to the library as gitleaks'.
	Path string

	dir string
}

// newFakeGitleaks makes a fake gitleaks that exits 10 (a leak) when its stdin
// holds fakeGitleaksHit, and 0 otherwise.
func newFakeGitleaks(t *testing.T) *FakeGitleaks {
	t.Helper()

	dir := fakeDir(t, fakeGitleaksName)

	return &FakeGitleaks{Path: filepath.Join(dir, fakeGitleaksName), dir: dir}
}

// newFakeGitleaksExit makes a fake gitleaks that always exits code.
func newFakeGitleaksExit(t *testing.T, code int) *FakeGitleaks {
	t.Helper()

	f := newFakeGitleaks(t)
	writeJSON(t, filepath.Join(f.dir, gitleaksExitFile), code)

	return f
}

// newFakeGitleaksSleep makes a fake gitleaks that waits delay before answering,
// as a route's Delay does for the fake gh: a scan that outlives the caller's
// deadline.
func newFakeGitleaksSleep(t *testing.T, delay time.Duration) *FakeGitleaks {
	t.Helper()

	f := newFakeGitleaks(t)
	writeJSON(t, filepath.Join(f.dir, gitleaksSleepFile), delay.Milliseconds())

	return f
}

// Calls returns every call the fake gitleaks recorded, in order.
func (f *FakeGitleaks) Calls(t *testing.T) []GitleaksCall {
	t.Helper()

	return readJSONLines[GitleaksCall](t, filepath.Join(f.dir, gitleaksCallsFile))
}

// fakeDir makes a fresh directory holding a link named name to this test
// binary.
func fakeDir(t *testing.T, name string) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	if err := os.Symlink(exe, filepath.Join(dir, name)); err != nil {
		t.Fatalf("link the fake %s: %v", name, err)
	}

	return dir
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readJSONLines[T any](t *testing.T, path string) []T {
	t.Helper()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var out []T

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<26)

	for scanner.Scan() {
		var v T
		if err := json.Unmarshal(scanner.Bytes(), &v); err != nil {
			t.Fatalf("%s: a line that is no record: %v", path, err)
		}

		out = append(out, v)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return out
}

// The fake gh's guard, written from the PRD's list of calls (milestone 2, "It
// calls only a fixed set of endpoints"), independently of the library's
// endpoint table.
var (
	fakeEndpointFamily = regexp.MustCompile(`^repos/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/` +
		`(actions/runs/[0-9]+/artifacts\?|releases/tags/v|attestations/sha256:|git/commits/[0-9a-f]{40}$|` +
		`git/ref/heads/security-audit/|git/blobs$|git/trees$|git/commits$|git/refs$|compare/|pulls$|` +
		`issues\?|issues$|security-advisories\?|security-advisories$|security-advisories/GHSA-)`)
	fakeForbiddenSegment = regexp.MustCompile(`(?i)/(cve|merge|reviews|comments|publish|tags/[^v]|releases$)(/|\?|$)`)
	fakePostEndpoint     = regexp.MustCompile(`/(git/blobs|git/trees|git/commits|git/refs|pulls|issues|security-advisories)$`)
	fakePatchEndpoint    = regexp.MustCompile(`/security-advisories/GHSA-[a-z0-9-]+$`)
)

// fakeGuard returns the rule a call breaks, empty when none.
func fakeGuard(method, endpoint string, body json.RawMessage) string {
	if !fakeEndpointFamily.MatchString(endpoint) {
		return "an endpoint outside the PRD's fixed set"
	}

	if fakeForbiddenSegment.MatchString(endpoint) {
		return "a CVE, merge, review, comment, publish, tag or release endpoint"
	}

	switch method {
	case "GET":
		if body != nil {
			return "a GET with a body"
		}

		return ""
	case "POST":
		if !fakePostEndpoint.MatchString(endpoint) {
			return "a POST to an endpoint that is no create of the fixed set"
		}
	case "PATCH":
		if !fakePatchEndpoint.MatchString(endpoint) {
			return "a PATCH of anything but a security advisory"
		}
	default:
		return "a method other than GET, POST and PATCH"
	}

	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return "a write whose body is no JSON object"
	}

	switch {
	case strings.HasSuffix(endpoint, "/security-advisories"):
		for _, key := range []string{"state", "cve_id", "credits", "collaborating_users", "collaborating_teams"} {
			if _, ok := fields[key]; ok {
				return "an advisory created with " + key
			}
		}
	case fakePatchEndpoint.MatchString(endpoint):
		if fields["state"] != "draft" {
			return "an advisory update whose state is not draft"
		}

		if _, ok := fields["cve_id"]; ok {
			return "an advisory update with cve_id"
		}
	case strings.HasSuffix(endpoint, "/git/refs"):
		ref, _ := fields["ref"].(string)
		if !strings.HasPrefix(ref, "refs/heads/security-audit/") {
			return "a ref outside refs/heads/security-audit/"
		}
	case strings.HasSuffix(endpoint, "/pulls"):
		if fields["base"] != "main" {
			return "a pull request into another branch than main"
		}
	}

	return ""
}

// parseGHArgs reads gh api --method <M> --include -H <accept> -H <version>
// [--input -] <endpoint>, the one shape the library may use.
func parseGHArgs(args []string) (method, endpoint string, input bool, violation string) {
	prefixOK := len(args) >= 9 && args[0] == "api" && args[1] == "--method" && args[3] == "--include" &&
		args[4] == "-H" && args[5] == ghAcceptHeader && args[6] == "-H" && args[7] == ghVersionHeader

	switch {
	case prefixOK && len(args) == 9:
		return args[2], args[8], false, ""
	case prefixOK && len(args) == 11 && args[8] == "--input" && args[9] == "-":
		return args[2], args[10], true, ""
	default:
		return "", "", false, "arguments of another shape than gh api --method <M> --include -H <accept> -H <version> [--input -] <endpoint>"
	}
}

// fakeGHMain is the fake gh: it reads its call, checks it, answers it from the
// routes, and records it.
func fakeGHMain() int {
	dir := filepath.Dir(os.Args[0])
	call := Call{Args: append([]string{}, os.Args[1:]...), Env: os.Environ()}

	method, endpoint, input, violation := parseGHArgs(call.Args)
	call.Method, call.Endpoint, call.Violation = method, endpoint, violation

	if input {
		stdin, err := io.ReadAll(os.Stdin)
		if err != nil {
			call.Violation = "stdin unreadable"
		} else if len(stdin) > 0 {
			if json.Valid(stdin) {
				call.Body = stdin
			} else {
				call.Violation = "a body that is no JSON"
			}
		}
	}

	if call.Violation == "" {
		call.Violation = fakeGuard(method, endpoint, call.Body)
	}

	status, body, link := fakeGuardStatus, json.RawMessage(`{"message":"refused by the fake gh"}`), ""
	var (
		trailing string
		delay    time.Duration
	)

	if call.Violation == "" {
		route, ok, err := nextRoute(dir, method, endpoint)
		switch {
		case err != nil:
			_, _ = fmt.Fprintln(os.Stderr, "fake gh:", err)

			return 1
		case ok:
			status, body, link, trailing = route.Status, route.Body, route.Link, route.Trailing
			delay = time.Duration(route.DelayMS) * time.Millisecond
		default:
			call.Unmatched = true
			status, body = 404, json.RawMessage(`{"message":"no route"}`)
		}
	}

	if err := appendJSONLine(filepath.Join(dir, ghCallsFile), call); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "fake gh:", err)

		return 1
	}

	// The call is recorded before the wait, so a caller that gives up on it
	// leaves the record of the answer it never read.
	time.Sleep(delay)

	var out strings.Builder

	fmt.Fprintf(&out, "HTTP/2.0 %d %s\r\n", status, reason(status))
	out.WriteString("Content-Type: application/json\r\n")

	if link != "" {
		fmt.Fprintf(&out, "Link: %s\r\n", link)
	}

	out.WriteString("\r\n")
	out.Write(body)
	out.WriteString(trailing)

	_, _ = io.WriteString(os.Stdout, out.String())

	if status/100 == 2 {
		return 0
	}

	_, _ = fmt.Fprintf(os.Stderr, "gh: %s (HTTP %d) %s\n", reason(status), status, fakeOutputMarker)

	return 1
}

// reason is the status line's reason phrase.
func reason(status int) string {
	switch status {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 418:
		return "I'm a teapot"
	case 422:
		return "Unprocessable Entity"
	default:
		return "Status"
	}
}

// nextRoute returns the route that answers the next call of method and
// endpoint, and counts the call in gh.state.json.
func nextRoute(dir, method, endpoint string) (routeRecord, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, ghRoutesFile)) //nolint:gosec // G703: the fake's own file, next to its link in the test's temporary directory
	if err != nil {
		return routeRecord{}, false, err
	}

	var routes []routeRecord
	if err := json.Unmarshal(data, &routes); err != nil {
		return routeRecord{}, false, err
	}

	var matching []routeRecord

	for _, r := range routes {
		if r.Method == method && r.Endpoint == endpoint {
			matching = append(matching, r)
		}
	}

	if len(matching) == 0 {
		return routeRecord{}, false, nil
	}

	counts := map[string]int{}

	statePath := filepath.Join(dir, ghStateFile)
	if state, err := os.ReadFile(statePath); err == nil { //nolint:gosec // G703: the fake's own file, next to its link in the test's temporary directory
		if err := json.Unmarshal(state, &counts); err != nil {
			return routeRecord{}, false, err
		}
	}

	key := method + " " + endpoint
	n := counts[key]
	counts[key] = n + 1

	state, err := json.Marshal(counts)
	if err != nil {
		return routeRecord{}, false, err
	}

	if err := os.WriteFile(statePath, state, 0o600); err != nil { //nolint:gosec // G703: the fake's own file, next to its link in the test's temporary directory
		return routeRecord{}, false, err
	}

	return matching[min(n, len(matching)-1)], true, nil
}

// fakeGitleaksMain is the fake gitleaks: it records its call and exits 10 on
// fakeGitleaksHit, or the code of gitleaks.exit when the test set one.
func fakeGitleaksMain() int {
	dir := filepath.Dir(os.Args[0])

	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 1
	}

	call := GitleaksCall{Args: append([]string{}, os.Args[1:]...), Env: os.Environ(), Stdin: string(stdin)}
	if err := appendJSONLine(filepath.Join(dir, gitleaksCallsFile), call); err != nil {
		return 1
	}

	if data, err := os.ReadFile(filepath.Join(dir, gitleaksSleepFile)); err == nil { //nolint:gosec // G703: the fake's own file, next to its link in the test's temporary directory
		var milliseconds int64
		if err := json.Unmarshal(data, &milliseconds); err != nil {
			return 1
		}

		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	}

	_, _ = fmt.Fprintln(os.Stdout, fakeOutputMarker)
	_, _ = fmt.Fprintln(os.Stderr, fakeOutputMarker)

	if data, err := os.ReadFile(filepath.Join(dir, gitleaksExitFile)); err == nil { //nolint:gosec // G703: the fake's own file, next to its link in the test's temporary directory
		var code int
		if err := json.Unmarshal(data, &code); err != nil {
			return 1
		}

		return code
	}

	if strings.Contains(call.Stdin, fakeGitleaksHit) {
		return fakeGitleaksLeakExit
	}

	return 0
}

func appendJSONLine(path string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G703: the fake's own record, next to its link in the test's temporary directory
	if err != nil {
		return err
	}

	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}
