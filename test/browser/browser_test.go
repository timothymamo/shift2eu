//go:build browser

package browser_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// This file holds the smoke test's harness: the environment it needs, the one
// browser it shares, the local origins it serves the bundle from, and the
// helpers that drive the page's real controls. Every test in the package is a
// black-box test of the built bundle: it reads the bundle, never site/, so what
// it pins is what a release ships.

// The environment `make test-browser` fills in. The test starts no browser of
// its own and builds no bundle of its own.
const (
	devtoolsEnv = "SHIFT2EU_DEVTOOLS_URL"
	bundleEnv   = "SHIFT2EU_SITE_BUNDLE"
)

var (
	// devtoolsURL is the DevTools address of the headless Chromium the
	// container runs, such as ws://127.0.0.1:9222 or http://127.0.0.1:9222.
	devtoolsURL string
	// bundleDir is the directory `make site` built the bundle into.
	bundleDir string
)

// TestMain fails, and never skips, when the harness is not configured: a smoke
// test that skips reports the same green as one that passed.
func TestMain(m *testing.M) {
	devtoolsURL = os.Getenv(devtoolsEnv)
	bundleDir = os.Getenv(bundleEnv)

	if message, ok := missingHarness(devtoolsURL, bundleDir); !ok {
		fmt.Fprintln(os.Stderr, message)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// missingHarness reports why the harness cannot run, if it cannot: the two
// variables and the bundle are what the smoke test needs.
func missingHarness(devtools, bundle string) (string, bool) {
	if devtools == "" {
		return fmt.Sprintf("%s is not set: this test does not start Chromium. `make test-browser` runs "+
			"the containerised headless Chromium (pinned by digest, DevTools on 127.0.0.1) and passes its "+
			"DevTools address in %s, such as http://127.0.0.1:9222.", devtoolsEnv, devtoolsEnv), false
	}

	if bundle == "" {
		return fmt.Sprintf("%s is not set: `make site` builds the bundle, and `make test-browser` points "+
			"%s at it (a release points it at the attested bundle).", bundleEnv, bundleEnv), false
	}

	if _, err := os.Stat(filepath.Join(bundle, "manifest.json")); err != nil {
		return fmt.Sprintf("%s is %q, which holds no manifest.json: build the bundle with `make site`.",
			bundleEnv, bundle), false
	}

	return "", true
}

var (
	// allocOnce dials the browser once for the whole test binary.
	allocOnce sync.Once
	// browserCtx is the allocator's context: every tab belongs to the one
	// browser the container runs.
	browserCtx  context.Context
	browserStop context.CancelFunc
	// tabs bounds the tabs that are open at once. Each page loads the module —
	// about 6 MiB of WebAssembly — so a container with little memory runs the
	// tests a few at a time rather than all of them; the tests are parallel, so
	// the suite stays quick.
	// Each page loads about 6 MiB of WebAssembly, and a container that runs the
	// suite beside other work has little memory to spare, so three tabs are open
	// at once.
	tabs = make(chan struct{}, 3)
)

// allocator returns the context of the shared browser, dialing it on first use.
func allocator(t *testing.T) context.Context {
	t.Helper()

	allocOnce.Do(func() {
		// NewRemoteAllocator's successor: chromedp moved it to the remote
		// package in v0.21.0 (chromedp's docs/MIGRATION.md), and it takes the
		// ws://host:port and http://host:port forms SHIFT2EU_DEVTOOLS_URL uses.
		browserCtx, browserStop = remote.NewAllocator(context.Background(), devtoolsURL,
			remote.WithDialTimeout(20*time.Second))
	})

	return browserCtx
}

// newTab opens a fresh tab in the shared browser and closes it when the test
// ends. Contexts that share a browser run in separate tabs, so tests may run in
// parallel; a few tabs are open at once.
func newTab(t *testing.T) context.Context {
	t.Helper()

	tabs <- struct{}{}
	t.Cleanup(func() { <-tabs })

	ctx, cancel := chromedp.NewContext(allocator(t))
	t.Cleanup(cancel)

	// The tab is started here, with no deadline: the first action of a context
	// binds the browser's life to it. Everything the test does gets a deadline
	// below, so a wedged page fails the test instead of hanging the suite.
	if err := chromedp.Do(ctx); err != nil {
		t.Fatalf("open a tab: %v", err)
	}

	deadline, stop := context.WithTimeout(ctx, 5*time.Minute)
	t.Cleanup(stop)

	return deadline
}

// do runs the actions on the tab in order, failing the test at the first error.
func do(t *testing.T, ctx context.Context, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()

	if err := chromedp.Do(ctx, steps...); err != nil {
		t.Fatalf("driving the page: %v", err)
	}
}

// run runs one action and returns its value, failing the test on error.
func run[T any](t *testing.T, ctx context.Context, action chromedp.Action[T]) T {
	t.Helper()

	value, err := chromedp.Run(ctx, action)
	if err != nil {
		t.Fatalf("driving the page: %v", err)
	}

	return value
}

// eval evaluates the expression in the page and decodes its result into T.
func eval[T any](t *testing.T, ctx context.Context, expression string) T {
	t.Helper()

	value, err := chromedp.Run(ctx, chromedp.Evaluate[T](expression))
	if err != nil {
		t.Fatalf("evaluating %s: %v", clip(expression), err)
	}

	return value
}

// evalAsync evaluates the expression in the page, waits for its promise and
// decodes the promise's value into T.
func evalAsync[T any](t *testing.T, ctx context.Context, expression string) T {
	t.Helper()

	value, err := chromedp.Run(ctx, chromedp.Evaluate[T](expression, chromedp.EvalAwaitPromise))
	if err != nil {
		t.Fatalf("evaluating %s: %v", clip(expression), err)
	}

	return value
}

// waitFor polls a boolean JavaScript expression until it is true, and fails the
// test with the expression when it never is. Waits are on DOM conditions, never
// on a sleep.
func waitFor(t *testing.T, ctx context.Context, what, expression string) {
	t.Helper()

	var err error

	// A navigation replaces the document's execution context. A poll that starts
	// while the browser has reported the load but chromedp has not registered the
	// new context yet runs in the old one and fails with "Cannot find context";
	// that one error is retried, every other is final. The DOM condition is still
	// what is polled.
	for attempt := 0; attempt < 10; attempt++ {
		if _, err = chromedp.Run(ctx, chromedp.Poll[bool](expression, chromedp.WithPollingTimeout(90*time.Second))); err == nil {
			return
		}

		if !strings.Contains(err.Error(), "Cannot find context") {
			break
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("waiting for %s: %v\n%s", what, err, clip(expression))
}

// clip shortens an expression for an error message.
func clip(expression string) string {
	const limit = 500

	if len(expression) <= limit {
		return expression
	}

	return expression[:limit] + "…"
}

// jsString returns value as a JavaScript string literal.
func jsString(value string) string {
	return strconv.Quote(value)
}

// request is one request an origin answered.
type request struct {
	Method string
	Path   string
	Query  string
	Status int
}

// site is one origin: the bundle's files at the top level, the test's own files
// under names the bundle does not hold, and a log of every request it answered.
// The bundle is served from SHIFT2EU_SITE_BUNDLE, never copied: what the test
// drives is the released bytes.
type site struct {
	mu       sync.Mutex
	requests []request
	files    map[string]file
	server   *httptest.Server
}

// file is one file the test serves on an origin: a page of its own, a worker
// script, or a navigation target.
type file struct {
	body        []byte
	contentType string
}

// newSite starts an origin on 127.0.0.1, which a browser counts as a secure
// context, and stops it when the test ends.
func newSite(t *testing.T) *site {
	t.Helper()

	s := &site{files: make(map[string]file)}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)

	return s
}

// url returns the address of path on the site's origin.
func (s *site) url(path string) string {
	return s.server.URL + "/" + path
}

// addHTML serves the HTML document at path on this origin.
func (s *site) addHTML(path, document string) {
	s.addFile(path, file{body: []byte(document), contentType: "text/html; charset=utf-8"})
}

// addScript serves the JavaScript source at path on this origin.
func (s *site) addScript(path, source string) {
	s.addFile(path, file{body: []byte(source), contentType: "text/javascript; charset=utf-8"})
}

// addFile serves body at path on this origin.
func (s *site) addFile(path string, f file) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.files[path] = f
}

// log returns the requests the origin has answered so far.
func (s *site) log() []request {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.requests)
}

// serve answers one request, recording it before it is written.
func (s *site) serve(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}

	body, contentType, found := s.lookup(name)

	status := http.StatusOK
	if !found {
		status = http.StatusNotFound
	}

	s.mu.Lock()
	s.requests = append(s.requests, request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Status: status})
	s.mu.Unlock()

	if !found {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")

	// This origin is a static file server for the site's own files, which is
	// what the smoke test drives the browser against; the bytes are the bundle's
	// or the test's own pages, never a request's.
	_, _ = w.Write(body) //nolint:gosec // G705: the bytes are the bundle's files or the test's own pages, as a static file server writes them
}

// lookup returns the bytes and content type of the file name of this origin:
// one of the test's own files, or a file of the bundle at the top level.
func (s *site) lookup(name string) ([]byte, string, bool) {
	s.mu.Lock()
	f, ok := s.files[name]
	s.mu.Unlock()

	if ok {
		return f.body, f.contentType, true
	}

	// The bundle holds no directories, and a name with a separator or a ".."
	// is not one of its files: the test's origin serves nothing outside it.
	if strings.ContainsAny(name, `\/`) || name == "." || name == ".." {
		return nil, "", false
	}

	root, err := os.OpenRoot(bundleDir)
	if err != nil {
		return nil, "", false
	}
	defer root.Close()

	data, err := root.ReadFile(name)
	if err != nil {
		return nil, "", false
	}

	return data, contentTypeOf(name), true
}

// contentTypeOf names a bundle file's type by its extension: the pages, the
// stylesheet and the scripts must be served as what they are, or the browser
// refuses to run a script it was told is not JavaScript.
func contentTypeOf(name string) string {
	switch filepath.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// bundleFile returns one file of the bundle.
func bundleFile(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(bundleDir, name))
	if err != nil {
		t.Fatalf("read the bundle's %s: %v", name, err)
	}

	return data
}

// bundleManifest is manifest.json: the version and every file of the bundle.
type bundleManifest struct {
	Version string `json:"version"`
	Files   []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// manifestOf reads the bundle's manifest.json.
func manifestOf(t *testing.T) bundleManifest {
	t.Helper()

	var m bundleManifest

	if err := json.Unmarshal(bundleFile(t, "manifest.json"), &m); err != nil {
		t.Fatalf("decode manifest.json: %v", err)
	}

	if len(m.Files) == 0 {
		t.Fatalf("manifest.json lists no file, so the bundle holds nothing to check")
	}

	return m
}

// bundleAsset returns the name of the bundle's one file with the prefix and
// suffix, such as the page script page.<hash>.js.
func bundleAsset(t *testing.T, prefix, suffix string) string {
	t.Helper()

	var found []string

	for _, f := range manifestOf(t).Files {
		if strings.HasPrefix(f.Path, prefix+".") && strings.HasSuffix(f.Path, suffix) {
			found = append(found, f.Path)
		}
	}

	if len(found) != 1 {
		t.Fatalf("the bundle holds %d %s.<hash>%s files (%v), want exactly one", len(found), prefix, suffix, found)
	}

	return found[0]
}

// openPage opens one of the bundle's pages in a fresh tab and waits for the
// document to finish loading.
func openPage(t *testing.T, s *site, page string) context.Context {
	t.Helper()

	ctx := newTab(t)
	do(t, ctx, chromedp.Navigate(s.url(page)))
	waitFor(t, ctx, page+" to load", `document.readyState === "complete"`)

	return ctx
}

// openAssessment opens the assessment page and waits until the module has been
// instantiated and the page's controls are wired, which is what enables the run
// button.
func openAssessment(t *testing.T, s *site) context.Context {
	t.Helper()

	ctx := openPage(t, s, "index.html")
	waitFor(t, ctx, "the assessment module to load", `(() => {
		const run = document.getElementById("run");
		return run !== null && !run.disabled;
	})()`)

	return ctx
}

// clickAndWaitForLoad clicks the control that navigates and waits for the
// document it opens: a poll does not survive a navigation, but a subscription to
// the load event does.
func clickAndWaitForLoad[S chromedp.Selectable](t *testing.T, ctx context.Context, selector S) {
	t.Helper()

	loads := chromedp.Events(ctx, page.LoadEventFired)
	do(t, ctx, chromedp.Click(selector))

	for _, err := range loads {
		if err != nil {
			t.Fatalf("waiting for the page the link opens: %v", err)
		}

		break
	}
}

// setValue sets the value of the control with the id and dispatches the input
// event a user's paste sends, so the page's own handler runs.
func setValue(t *testing.T, ctx context.Context, id, value string) {
	t.Helper()

	ok := eval[bool](t, ctx, fmt.Sprintf(`(() => {
		const el = document.getElementById(%s);
		if (el === null) {
			return false;
		}
		el.value = %s;
		el.dispatchEvent(new Event("input", {bubbles: true}));
		return true;
	})()`, jsString(id), jsString(value)))

	if !ok {
		t.Fatalf("the page holds no control with id %q", id)
	}
}

// pasteInventory pastes the document into the page's inventory field.
func pasteInventory(t *testing.T, ctx context.Context, document string) {
	t.Helper()

	setValue(t, ctx, "inventory", document)
}

// loadExample picks the bundled example and clicks the page's load-example
// button.
func loadExample(t *testing.T, ctx context.Context, key string) {
	t.Helper()

	setValue(t, ctx, "example", key)
	do(t, ctx, chromedp.Click(chromedp.ID("load-example")))
	waitFor(t, ctx, "the "+key+" example to be loaded",
		`document.getElementById("inventory").value.indexOf('"schemaVersion"') >= 0`)
}

// targetChecked reports whether the target's checkbox is checked.
func targetChecked(t *testing.T, ctx context.Context, target string) bool {
	t.Helper()

	return eval[bool](t, ctx, fmt.Sprintf(`document.getElementById(%s).checked`, jsString("target-"+target)))
}

// setTargets checks exactly the target checkboxes named, with a click as a user
// makes, and unchecks the rest.
func setTargets(t *testing.T, ctx context.Context, targets ...string) {
	t.Helper()

	for _, target := range []string{"ovhcloud", "scaleway", "upcloud"} {
		if want := slices.Contains(targets, target); targetChecked(t, ctx, target) != want {
			do(t, ctx, chromedp.Click(chromedp.ID("target-"+target)))

			if got := targetChecked(t, ctx, target); got != want {
				t.Fatalf("the %s checkbox is %t after it was clicked, want %t", target, got, want)
			}
		}
	}
}

// snapshotDatePattern matches a catalog snapshot date, as the page shows it.
var snapshotDatePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// snapshotOf returns the catalog snapshot date the page shows for the target.
func snapshotOf(t *testing.T, ctx context.Context, target string) string {
	t.Helper()

	block := eval[string](t, ctx, fmt.Sprintf(`(() => {
		const box = document.getElementById(%s);
		return box === null ? "" : box.closest(".target").textContent;
	})()`, jsString("target-"+target)))

	date := snapshotDatePattern.FindString(block)
	if date == "" {
		t.Fatalf("the page shows no catalog snapshot date for %s: %q", target, block)
	}

	return date
}

// regionOf returns the first region the target's region control lists, which is
// a region the target's catalog holds.
func regionOf(t *testing.T, ctx context.Context, target string) string {
	t.Helper()

	regions := eval[[]string](t, ctx, fmt.Sprintf(`Array.from(document.querySelectorAll(%s))
		.map(function (option) { return option.value; })
		.filter(function (value) { return value !== ""; })`, jsString("#region-"+target+" option")))

	if len(regions) == 0 {
		t.Fatalf("the page lists no region for %s, so a region cannot be chosen", target)
	}

	return regions[0]
}

// chooseRegion selects the region for the target, as the page's own control
// offers it.
func chooseRegion(t *testing.T, ctx context.Context, target, region string) {
	t.Helper()

	setValue(t, ctx, "region-"+target, region)

	if got := eval[string](t, ctx, fmt.Sprintf(`document.getElementById(%s).value`, jsString("region-"+target))); got != region {
		t.Fatalf("the %s region control reads %q after %q was selected, want %q", target, got, region, region)
	}
}

// setEstimate turns the estimate switch on or off with a click.
func setEstimate(t *testing.T, ctx context.Context, want bool) {
	t.Helper()

	if eval[bool](t, ctx, `document.getElementById("estimate").checked`) != want {
		do(t, ctx, chromedp.Click(chromedp.ID("estimate")))

		if got := eval[bool](t, ctx, `document.getElementById("estimate").checked`); got != want {
			t.Fatalf("the estimate switch is %t after it was clicked, want %t", got, want)
		}
	}
}

// runAssessment clicks the page's run button and waits for what it shows: the
// report, or the error in the page's alert element.
func runAssessment(t *testing.T, ctx context.Context) {
	t.Helper()

	do(t, ctx, chromedp.Click(chromedp.ID("run")))
	waitForAssessment(t, ctx)
}

// waitForAssessment waits until the page shows a report or an error.
func waitForAssessment(t *testing.T, ctx context.Context) {
	t.Helper()

	waitFor(t, ctx, "the assessment to finish", `(() => {
		const status = document.getElementById("status");
		const report = document.getElementById("report");
		return status.textContent !== "" || report.childElementCount > 0;
	})()`)
}

// alertView is what the page's one alert element holds: the message, the markup
// the element holds for it, and whether the message is text only, which it is
// when the element's innerHTML is exactly the message escaped.
type alertView struct {
	Text     string `json:"text"`
	HTML     string `json:"html"`
	TextOnly bool   `json:"textOnly"`
	Elements int    `json:"elements"`
}

// alertOf reads the page's role="alert" element.
func alertOf(t *testing.T, ctx context.Context) alertView {
	t.Helper()

	return eval[alertView](t, ctx, `(() => {
		const el = document.querySelector('[role="alert"]');
		if (el === null) {
			return {text: "", html: "", textOnly: false, elements: 0};
		}
		const escaped = el.textContent
			.replace(/&/g, "&amp;")
			.replace(/</g, "&lt;")
			.replace(/>/g, "&gt;");
		return {
			text: el.textContent,
			html: el.innerHTML,
			textOnly: el.innerHTML === escaped,
			elements: el.querySelectorAll("*").length,
		};
	})()`)
}

var (
	// cliOnce builds the CLI once for the whole test binary.
	cliOnce sync.Once
	// cliPath is the CLI the parity test runs, empty until cliOnce ran.
	cliPath string
	// cliFailure is why the CLI could not be built.
	cliFailure error
)

// cliBinary builds the shift2eu CLI of this checkout and returns its path, for
// the download parity test: the page's downloads are compared with the CLI's
// own bytes, as PRD #297 words the criterion.
func cliBinary(t *testing.T) string {
	t.Helper()

	cliOnce.Do(func() {
		// The CLI is built once for the whole test binary, so its directory
		// outlives the first test that asked for it.
		dir, err := os.MkdirTemp("", "shift2eu-browser-cli-") //nolint:usetesting // the directory outlives the test that builds the CLI
		if err != nil {
			cliFailure = fmt.Errorf("create a directory for the CLI: %w", err)

			return
		}

		cliPath = filepath.Join(dir, "shift2eu")
		cliFailure = buildCLI(repositoryRoot(t), cliPath)
	})

	if cliFailure != nil {
		t.Fatalf("build the CLI: %v", cliFailure)
	}

	return cliPath
}

// buildCLI builds ./cmd/shift2eu of root into binary.
func buildCLI(root, binary string) error {
	output, err := execCommand(context.Background(), root, "go", "build", "-o", binary, "./cmd/shift2eu").CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build ./cmd/shift2eu: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// execCommand returns the command to run in dir with the environment make
// fixes.
func execCommand(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = buildEnv()

	return cmd
}

// buildEnv is the environment the CLI and the module build run in: the test's
// own, with the settings make fixes, so a developer's GOOS, GOARCH, GOFLAGS or
// GOWORK cannot decide what is built.
func buildEnv() []string {
	drop := map[string]bool{
		"GOOS":        true,
		"GOARCH":      true,
		"CGO_ENABLED": true,
		"GOFLAGS":     true,
		"GOWORK":      true,
	}

	env := make([]string, 0, len(os.Environ())+2)

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}

		env = append(env, entry)
	}

	return append(env, "GOFLAGS=-mod=readonly", "GOWORK=off")
}

// goEnv returns one value of the toolchain that builds the module, such as
// GOVERSION.
func goEnv(t *testing.T, name string) string {
	t.Helper()

	output, err := execCommand(t.Context(), repositoryRoot(t), "go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}

	return strings.TrimSpace(string(output))
}

// repositoryRoot returns the checkout the test runs from, which the CLI is built
// from: the package directory is <root>/test/browser.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("read the working directory: %v", err)
	}

	root := filepath.Dir(filepath.Dir(dir))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the checkout holding this test is not at %s: %v", root, err)
	}

	return root
}

// cliReport runs the CLI's assessment over the inventory with args and returns
// what it wrote.
func cliReport(t *testing.T, binary, inventory string, args ...string) []byte {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	if err := os.WriteFile(path, []byte(inventory), 0o600); err != nil {
		t.Fatalf("write the inventory: %v", err)
	}

	full := append([]string{"assess", "--inventory", path}, args...)

	var stdout, stderr strings.Builder

	cmd := execCommand(t.Context(), repositoryRoot(t), binary, full...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", binary, strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}

	return []byte(stdout.String())
}

// cliUsageMessage runs the CLI over the inventory with a command line it must
// refuse, and returns the message of its first diagnostic line without the
// "shift2eu: " prefix: that line is what the page shows for the same choice,
// since both check it by the application's option rules.
func cliUsageMessage(t *testing.T, inventory string, args ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "inventory.json")

	if err := os.WriteFile(path, []byte(inventory), 0o600); err != nil {
		t.Fatalf("write the inventory: %v", err)
	}

	full := append([]string{"assess", "--inventory", path}, args...)

	var stdout, stderr strings.Builder

	cmd := execCommand(t.Context(), repositoryRoot(t), cliBinary(t), full...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		t.Fatalf("`shift2eu %s` accepted the command line, want the usage error it refuses it with", strings.Join(full, " "))
	}

	line, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")

	message, ok := strings.CutPrefix(line, "shift2eu: ")
	if !ok {
		t.Fatalf("the CLI's first diagnostic line is %q, want it to start with %q", line, "shift2eu: ")
	}

	return message
}
