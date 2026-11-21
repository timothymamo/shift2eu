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
	got    []*http.Request
}

func (f *fakeRegistry) RoundTrip(req *http.Request) (*http.Response, error) {
	f.got = append(f.got, req)
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

// workflow writes a workflow excerpt that pins 2.1.330 with integrity, and
// returns its path.
func workflow(t *testing.T, integrity string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ai-review.yml")
	content := "jobs:\n  review:\n    steps:\n      - name: Install the Claude Code CLI\n        env:\n" +
		"          CLAUDE_CODE_VERSION: 2.1.330\n" +
		"          CLAUDE_CODE_INTEGRITY: " + integrity + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		integrity  string
		status     int
		fixture    string // a registry fixture of the library's testdata, or
		body       string // the registry's answer itself when fixture is empty
		header     http.Header
		wantCode   int
		wantStdout string
		// wantStderr are substrings of the one ::error line.
		wantStderr []string
	}{
		{
			name:       "published integrity passes",
			integrity:  publishedIntegrity,
			status:     http.StatusOK,
			fixture:    "registry-2.1.330.json",
			wantCode:   0,
			wantStdout: "Claude Code pin: ",
		},
		{
			name:      "stale integrity fails with both values and the npm command",
			integrity: otherIntegrity,
			status:    http.StatusOK,
			fixture:   "registry-2.1.330.json",
			wantCode:  1,
			wantStderr: []string{
				"::error title=Claude Code pin::",
				otherIntegrity,
				publishedIntegrity,
				"npm view @anthropic-ai/claude-code-linux-x64@2.1.330 dist.integrity",
			},
		},
		{
			name:       "a registry error fails closed",
			integrity:  publishedIntegrity,
			status:     http.StatusInternalServerError,
			fixture:    "registry-2.1.330.json",
			wantCode:   1,
			wantStderr: []string{"the registry answered 500, want 200"},
		},
		{
			name:       "a missing version fails",
			integrity:  publishedIntegrity,
			status:     http.StatusNotFound,
			fixture:    "registry-2.1.330.json",
			wantCode:   1,
			wantStderr: []string{"the registry answered 404, want 200"},
		},
		{
			name:       "a redirect is not followed",
			integrity:  publishedIntegrity,
			status:     http.StatusFound,
			header:     http.Header{"Location": {"https://example.com/elsewhere"}},
			wantCode:   1,
			wantStderr: []string{"redirect"},
		},
		{
			name:       "a body over the bound fails",
			integrity:  publishedIntegrity,
			status:     http.StatusOK,
			body:       strings.Repeat(" ", maxBody+1),
			wantCode:   1,
			wantStderr: []string{"over 1048576 bytes"},
		},
		{
			name:      "registry text cannot start a workflow command",
			integrity: publishedIntegrity,
			status:    http.StatusOK,
			body: `{"name":"@anthropic-ai/claude-code-linux-x64\n::warning::injected","version":"2.1.330",` +
				`"dist":{"integrity":"` + publishedIntegrity + `"}}`,
			wantCode:   1,
			wantStderr: []string{"package"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := tc.body
			if tc.fixture != "" {
				body = fixture(t, tc.fixture)
			}
			registry := &fakeRegistry{status: tc.status, body: body, header: tc.header}
			client := &http.Client{
				Transport: registry,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return errRedirect
				},
			}

			var stdout, stderr bytes.Buffer
			code := run(t.Context(), []string{"-workflow", workflow(t, tc.integrity)}, client, &stdout, &stderr)

			if code != tc.wantCode {
				t.Errorf("run() = %d, want %d (stderr %q)", code, tc.wantCode, stderr.String())
			}
			if len(registry.got) != 1 {
				t.Fatalf("run() sent %d requests, want 1", len(registry.got))
			}
			req := registry.got[0]
			if req.URL.String() != wantURL || req.Method != http.MethodGet {
				t.Errorf("run() sent %s %s, want GET %s", req.Method, req.URL, wantURL)
			}
			if req.Header.Get("Authorization") != "" || len(req.Header) != 1 {
				t.Errorf("run() sent headers %v, want Accept alone", req.Header)
			}

			if tc.wantCode == 0 {
				if !strings.HasPrefix(stdout.String(), tc.wantStdout) || strings.Count(stdout.String(), "\n") != 1 {
					t.Errorf("stdout = %q, want one line starting %q", stdout.String(), tc.wantStdout)
				}
				if stderr.Len() != 0 {
					t.Errorf("stderr = %q, want nothing", stderr.String())
				}
				return
			}

			out := stderr.String()
			if strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "::error title=Claude Code pin::") {
				t.Errorf("stderr = %q, want one ::error line", out)
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

func TestRunRefusesAWorkflowWithoutAPin(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ai-review.yml")
	if err := os.WriteFile(path, []byte("jobs: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := &fakeRegistry{status: http.StatusOK}

	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"-workflow", path}, &http.Client{Transport: registry}, &stdout, &stderr); code != 1 {
		t.Errorf("run() = %d, want 1", code)
	}
	if len(registry.got) != 0 {
		t.Errorf("run() sent %d requests, want none before a pin is read", len(registry.got))
	}
	if !strings.Contains(stderr.String(), "CLAUDE_CODE_VERSION") {
		t.Errorf("stderr = %q, want it to name CLAUDE_CODE_VERSION", stderr.String())
	}
}

func TestAnnotationDataEscapesLineBreaks(t *testing.T) {
	t.Parallel()

	got := annotationData("a\r\n::warning::b 100%")
	if want := "a%0D%0A::warning::b 100%25"; got != want {
		t.Errorf("annotationData() = %q, want %q", got, want)
	}
}
