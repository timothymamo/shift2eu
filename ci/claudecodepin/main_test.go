// White-box tests of the pin check's entry point: ci/claudecodepin is a main
// package, so its tests are package main, the only way a test file reaches it
// (Go refuses an import of a main package). The registry is a fake transport
// that answers from the library's fixtures, so no test opens a connection.
package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// publishedIntegrity is the integrity the 2.1.330 fixture publishes.
	publishedIntegrity = "sha512-9Gn+V7DtJ8M1nztP9CeDVl9g3RtrepI7rOFTyPRRnZe8D6zebIHKmC7OvBauucuAP0X0k85SDN/LPVZyjsGezQ=="

	// otherIntegrity is a well-formed sha512 integrity the fixture does not
	// publish: the published one with one character changed.
	otherIntegrity = "sha512-8Gn+V7DtJ8M1nztP9CeDVl9g3RtrepI7rOFTyPRRnZe8D6zebIHKmC7OvBauucuAP0X0k85SDN/LPVZyjsGezQ=="

	wantURL = "https://registry.npmjs.org/@anthropic-ai/claude-code-linux-x64/2.1.330"
)

// fakeRegistry answers every request with status and body, and records the
// requests it was sent.
type fakeRegistry struct {
	status int
	body   string
	header http.Header
	// bounce, when set, is the Location of a 302 the fake answers the first
	// request with: only a client that follows the redirect ever sees the
	// answer status, body and header configure.
	bounce string
	got    []*http.Request
}

func (f *fakeRegistry) RoundTrip(req *http.Request) (*http.Response, error) {
	f.got = append(f.got, req)
	if len(f.got) == 1 && f.bounce != "" {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {f.bounce}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}
	header := f.header
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: f.status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Request:    req,
	}, nil
}

// fixture returns a registry fixture of the library's testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("internal", "pincheck", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// pinFile writes the one committed pin file, .github/claude-code.yaml, pinning
// 2.1.330 with integrity, and returns its path.
func pinFile(t *testing.T, integrity string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude-code.yaml")
	content := "# The Claude Code CLI the AI reviewer and the audit run (ADR 0040).\n" +
		"# renovate: datasource=npm depName=@anthropic-ai/claude-code-linux-x64\n" +
		"version: 2.1.330\n" +
		"integrity: " + integrity + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		integrity string
		status    int
		fixture   string // a registry fixture of the library's testdata, or
		body      string // the registry's answer itself when fixture is empty
		header    http.Header
		// bounce is the Location of a 302 the fake registry answers the
		// first request with, before status, body and header.
		bounce string
		// productionClient builds the client with the check's own
		// newRegistryClient rather than the test's no-follow client below.
		productionClient bool
		// pinName is the -pin file's name in a fresh temporary directory; empty
		// means the fixture pin file that pins integrity. A name whose file does
		// not exist makes run() read an error text holding the name as it was
		// given.
		pinName string
		// wantRequests is how many registry requests the check sends: none
		// when it fails before the fetch.
		wantRequests int
		wantCode     int
		wantStdout   string
		// wantStderr are substrings of the one ::error line.
		wantStderr []string
	}{
		{
			name:         "published integrity passes",
			integrity:    publishedIntegrity,
			status:       http.StatusOK,
			fixture:      "registry-2.1.330.json",
			wantRequests: 1,
			wantCode:     0,
			wantStdout:   "Claude Code pin: ",
		},
		{
			name:         "stale integrity fails with both values and the npm command",
			integrity:    otherIntegrity,
			status:       http.StatusOK,
			fixture:      "registry-2.1.330.json",
			wantRequests: 1,
			wantCode:     1,
			wantStderr: []string{
				"::error title=Claude Code pin::",
				otherIntegrity,
				publishedIntegrity,
				"npm view @anthropic-ai/claude-code-linux-x64@2.1.330 dist.integrity",
			},
		},
		{
			name:         "a registry error fails closed",
			integrity:    publishedIntegrity,
			status:       http.StatusInternalServerError,
			fixture:      "registry-2.1.330.json",
			wantRequests: 1,
			wantCode:     1,
			wantStderr:   []string{"the registry answered 500, want 200"},
		},
		{
			name:         "a missing version fails",
			integrity:    publishedIntegrity,
			status:       http.StatusNotFound,
			fixture:      "registry-2.1.330.json",
			wantRequests: 1,
			wantCode:     1,
			wantStderr:   []string{"the registry answered 404, want 200"},
		},
		{
			// A redirect answer holds a valid registry document: the check
			// must fail on the status, never read the document the Location
			// names. The test client answers the redirect with its 302 (the
			// standard library's no-follow policy), so the case pins fetch's
			// own refusal of a non-200.
			name:         "a redirect answer fails closed instead of being followed",
			integrity:    publishedIntegrity,
			status:       http.StatusFound,
			fixture:      "registry-2.1.330.json",
			header:       http.Header{"Location": {"https://example.com/elsewhere"}},
			wantRequests: 1,
			wantCode:     1,
			wantStderr:   []string{"the registry answered 302, want 200"},
		},
		{
			// The check builds its client with newRegistryClient, which
			// refuses every redirect with errRedirect. The fake would answer
			// the request the Location names, on another host, with the very
			// document the check accepts, so a client that followed it would
			// pass: this case pins the refusal in the production client
			// itself, not fetch's non-200 branch.
			name:             "the production client refuses to follow a redirect",
			integrity:        publishedIntegrity,
			status:           http.StatusOK,
			fixture:          "registry-2.1.330.json",
			bounce:           "https://registry.example.com/x",
			productionClient: true,
			wantRequests:     1,
			wantCode:         1,
			wantStderr:       []string{errRedirect.Error()},
		},
		{
			name:         "a body over the bound fails",
			integrity:    publishedIntegrity,
			status:       http.StatusOK,
			body:         strings.Repeat(" ", maxBody+1),
			wantRequests: 1,
			wantCode:     1,
			wantStderr:   []string{"over 1048576 bytes"},
		},
		{
			// The newline is in the package name, which Verify quotes with
			// %q, so this case pins Verify's quoting rather than the
			// annotation's escaping: the quoted name holds the raw newline
			// as the two characters \ and n, so no ::warning command can
			// follow it.
			name:      "a registry package name with a newline is quoted by Verify",
			integrity: publishedIntegrity,
			status:    http.StatusOK,
			body: `{"name":"@anthropic-ai/claude-code-linux-x64\n::warning::injected","version":"2.1.330",` +
				`"dist":{"integrity":"` + publishedIntegrity + `"}}`,
			wantRequests: 1,
			wantCode:     1,
			wantStderr: []string{
				"package",
				`"@anthropic-ai/claude-code-linux-x64\n::warning::injected"`,
			},
		},
		{
			// The -pin path reaches the ::error message unquoted (read
			// %s), so its CR, LF and % must be escaped or a workflow command
			// of the path would run, and the message would span lines.
			name:         "a pin file path's CR, LF and % are escaped in the one ::error line",
			integrity:    publishedIntegrity,
			status:       http.StatusOK,
			pinName:      "claude-code\n::warning::injected\r100%.yaml",
			wantRequests: 0,
			wantCode:     1,
			wantStderr:   []string{"claude-code%0A::warning::injected%0D100%25.yaml"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := tc.body
			if tc.fixture != "" {
				body = fixture(t, tc.fixture)
			}
			registry := &fakeRegistry{status: tc.status, body: body, header: tc.header, bounce: tc.bounce}
			client := &http.Client{
				Transport: registry,
				// The check's own client refuses redirects with errRedirect
				// in main, which run() does not build; the standard library's
				// no-follow policy hands the 302 itself to run() here.
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			if tc.productionClient {
				client = newRegistryClient(registry)
			}

			path := pinFile(t, tc.integrity)
			if tc.pinName != "" {
				path = filepath.Join(t.TempDir(), tc.pinName)
			}

			var stdout, stderr bytes.Buffer
			code := run(t.Context(), []string{"-pin", path}, client, &stdout, &stderr)

			if code != tc.wantCode {
				t.Errorf("run() = %d, want %d (stderr %q)", code, tc.wantCode, stderr.String())
			}
			if len(registry.got) != tc.wantRequests {
				t.Fatalf("run() sent %d requests, want %d", len(registry.got), tc.wantRequests)
			}
			if len(registry.got) == 1 {
				req := registry.got[0]
				if req.URL.String() != wantURL || req.Method != http.MethodGet {
					t.Errorf("run() sent %s %s, want GET %s", req.Method, req.URL, wantURL)
				}
				if accept := req.Header.Values("Accept"); len(req.Header) != 1 || len(accept) != 1 || accept[0] != "application/json" {
					t.Errorf("run() sent headers %v, want Accept: application/json alone", req.Header)
				}
			}

			if tc.wantCode == 0 {
				out := stdout.String()
				if !strings.HasPrefix(out, tc.wantStdout) || strings.Count(out, "\n") != 1 {
					t.Errorf("stdout = %q, want one line starting %q", out, tc.wantStdout)
				}
				for _, want := range []string{"@anthropic-ai/claude-code-linux-x64@2.1.330", tc.integrity} {
					if !strings.Contains(out, want) {
						t.Errorf("stdout = %q, want it to contain %q", out, want)
					}
				}
				if stderr.Len() != 0 {
					t.Errorf("stderr = %q, want nothing", stderr.String())
				}
				return
			}

			out := stderr.String()
			if strings.Count(out, "\n") != 1 || strings.Contains(out, "\r") || !strings.HasPrefix(out, "::error title=Claude Code pin::") {
				t.Errorf("stderr = %q, want one CR-less ::error line", out)
			}
			for _, want := range tc.wantStderr {
				if !strings.Contains(out, want) {
					t.Errorf("stderr = %q, want it to contain %q", out, want)
				}
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing on a failure", stdout.String())
			}
		})
	}
}

func TestRunRefusesAPinFileWithoutAPin(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "claude-code.yaml")
	if err := os.WriteFile(path, []byte("# no pin here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := &fakeRegistry{status: http.StatusOK}

	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"-pin", path}, &http.Client{Transport: registry}, &stdout, &stderr); code != 1 {
		t.Errorf("run() = %d, want 1", code)
	}
	if len(registry.got) != 0 {
		t.Errorf("run() sent %d requests, want none before a pin is read", len(registry.got))
	}
	out := stderr.String()
	if strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "::error title=Claude Code pin::") {
		t.Errorf("stderr = %q, want one ::error line", out)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("stderr = %q, want it to name the version line the pin file holds", out)
	}
}

// TestDefaultPinIsTheOneCommittedPinFile pins the file the check reads when its
// workflow gives it no -pin: .github/claude-code.yaml, the one committed pin
// file, which both workflows' install steps read too (ADR 0040's milestone-2
// amendment, issue #277). Renovate bumps the version there; a check that read
// another file would hold a pin nothing installs.
func TestDefaultPinIsTheOneCommittedPinFile(t *testing.T) {
	t.Parallel()

	if defaultPin != ".github/claude-code.yaml" {
		t.Errorf("defaultPin = %q, want .github/claude-code.yaml: the one committed pin file, which Renovate bumps and both workflows read", defaultPin)
	}
}

func TestAnnotationDataEscapesLineBreaks(t *testing.T) {
	t.Parallel()

	got := annotationData("a\r\n::warning::b 100%")
	if want := "a%0D%0A::warning::b 100%25"; got != want {
		t.Errorf("annotationData() = %q, want %q", got, want)
	}
}
