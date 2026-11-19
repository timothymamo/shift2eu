//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// This file pins the built shift2eu-server binary end to end (PRD #103, milestone
// 3): it starts the server on 127.0.0.1:0, learns the port from the line it
// prints on stderr, and calls it as a client would. The dependency check at the
// end reads the binary's import graph instead.

const (
	// serverErrPrefix is how the server prefixes a startup error on stderr.
	serverErrPrefix = "shift2eu-server: "
	// listeningPrefix is the line the server prints once it listens, with the
	// bound address after it. It is the only way a test learns a port the kernel
	// chose.
	listeningPrefix = "shift2eu-server: listening on "
	// serverTokenEnv is the name of the environment variable the bearer token
	// comes from, never a token.
	serverTokenEnv = "SHIFT2EU_SERVER_TOKEN" //nolint:gosec // this is the variable's name, not a credential
	// testServerToken is the token the token cases configure. It is a test
	// value, never a credential, and at least the 32 characters the server
	// accepts as the shortest token.
	testServerToken = "e2e-0123456789abcdef0123456789abcdef"
	// serverStartBound bounds how long the server may take to announce its
	// address, and how long a test waits for the announcement.
	serverStartBound = 30 * time.Second
	// serverStopBound bounds how long the server may take to exit once it is
	// signalled.
	serverStopBound = 30 * time.Second
	// serverRequestBound bounds one HTTP request a test makes.
	serverRequestBound = 120 * time.Second
	// maxBodySize is the largest request body the API accepts,
	// assessmentrequest.MaxBodySize: the inventory reader's 10 MiB cap plus
	// 64 KiB for the request envelope. It is written out because this package is
	// black box and may import no adapter (the e2e-is-black-box depguard rule).
	maxBodySize = 10<<20 + 64<<10
	// probeInterval is how often a test looks for the server's announcement.
	probeInterval = 25 * time.Millisecond
	// serverLoopbackListen is the --listen value the tests use unless they name
	// one: port 0, so the kernel picks a free port and no two runs collide.
	serverLoopbackListen = "127.0.0.1:0"
)

// serverRun is one run of the built server binary.
type serverRun struct {
	cmd *exec.Cmd
	// addr is the host:port the server announced, as it announced it.
	addr string
	// dial is the address a test connects to: 127.0.0.1 with the announced port,
	// so a server that binds every interface is still reached over loopback only.
	dial string
	// stderr holds what the server has written so far, for the failure messages.
	stderr *lockedBuffer

	// exited is closed once the process has ended, with waitErr set.
	exited chan struct{}
	mu     sync.Mutex
	// waitErr is what cmd.Wait reported; read it only after exited is closed.
	waitErr error
}

// lockedBuffer is a buffer the server writes to from its own goroutine while the
// test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

// String returns what has been written.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// startServerBinary starts the built server with args and env and returns it once
// it has announced its address. --listen defaults to 127.0.0.1:0 unless args name
// it. The server is killed when the test ends, so a failing test leaves no
// process behind.
func startServerBinary(t *testing.T, env []string, args ...string) *serverRun {
	t.Helper()

	argv := append([]string{}, args...)
	if !slices.Contains(argv, "--listen") {
		argv = append([]string{"--listen", serverLoopbackListen}, argv...)
	}

	requireServerBinary(t)

	stderr := &lockedBuffer{}

	cmd := exec.Command(serverBinary(t), argv...)
	// exec.Cmd reads a nil Env as "inherit the parent environment", which would
	// hand the server the test's own environment; every caller passes one.
	cmd.Env = append([]string{}, env...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting shift2eu-server %q: %v", argv, err)
	}

	s := &serverRun{cmd: cmd, stderr: stderr, exited: make(chan struct{})}

	go func() {
		err := cmd.Wait()

		s.mu.Lock()
		s.waitErr = err
		s.mu.Unlock()

		close(s.exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-s.exited
	})

	s.addr = waitForListening(t, s, argv)
	s.dial = loopbackDial(t, s.addr)

	return s
}

// waitForListening waits for the server's announcement line and returns the
// address it holds. A server that exits first fails the test with its stderr, and
// one that says nothing within serverStartBound fails it too.
func waitForListening(t *testing.T, s *serverRun, argv []string) string {
	t.Helper()

	deadline := time.Now().Add(serverStartBound)

	for {
		if addr := announcedAddress(s.stderr.String()); addr != "" {
			return addr
		}

		select {
		case <-s.exited:
			t.Fatalf("shift2eu-server %q exited before listening: %v; stderr = %q", argv, s.wait(t), s.stderr.String())
		case <-time.After(probeInterval):
		}

		if time.Now().After(deadline) {
			t.Fatalf("shift2eu-server %q did not announce its address within %s; stderr = %q",
				argv, serverStartBound, s.stderr.String())
		}
	}
}

// announcedAddress returns the address of the announcement line in stderr, or
// empty when the server has not announced one yet.
func announcedAddress(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if addr, ok := strings.CutPrefix(line, listeningPrefix); ok {
			return strings.TrimSpace(addr)
		}
	}

	return ""
}

// loopbackDial returns the address a test connects to for an announced address:
// 127.0.0.1 with its port, whatever host the server bound.
func loopbackDial(t *testing.T, announced string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(announced)
	if err != nil {
		t.Fatalf("the announced address %q is not host:port: %v", announced, err)
	}

	if host == "" || port == "" {
		t.Fatalf("the announced address %q holds no host or no port", announced)
	}

	if n, err := strconv.Atoi(port); err != nil || n <= 0 {
		t.Fatalf("the announced address %q holds no usable port: %v", announced, err)
	}

	return net.JoinHostPort("127.0.0.1", port)
}

// wait waits for the server process to end and returns the error cmd.Wait
// reported.
func (s *serverRun) wait(t *testing.T) error {
	t.Helper()

	<-s.exited

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.waitErr
}

// exitCode waits for the server to end and returns its exit code.
func (s *serverRun) exitCode(t *testing.T) int {
	t.Helper()

	err := s.wait(t)
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	t.Fatalf("waiting for shift2eu-server: %v", err)

	return 0
}

// terminate signals the server with SIGTERM and returns its exit code.
func (s *serverRun) terminate(t *testing.T) int {
	t.Helper()

	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling shift2eu-server with SIGTERM: %v", err)
	}

	exited := make(chan int, 1)

	go func() { exited <- s.exitCode(t) }()

	select {
	case code := <-exited:
		return code
	case <-time.After(serverStopBound):
		t.Fatalf("shift2eu-server did not exit within %s of SIGTERM; stderr = %q", serverStopBound, s.stderr.String())

		return 0
	}
}

// answer is one response a test read: its status and its body. The body is read
// and the connection released by (*serverRun).send, so no test holds an unclosed
// one.
type answer struct {
	status int
	body   []byte
}

// send sends req to the server and returns the answer.
func (s *serverRun) send(t *testing.T, req *http.Request) answer {
	t.Helper()

	if !req.URL.IsAbs() {
		req.URL.Scheme = "http"
		req.URL.Host = s.dial
	}

	client := &http.Client{Timeout: serverRequestBound}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v; stderr = %q", req.Method, req.URL.Path, err, s.stderr.String())
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: reading the response body: %v", req.Method, req.URL.Path, err)
	}

	return answer{status: resp.StatusCode, body: body}
}

// get sends GET path to the server and returns the status and the body.
func (s *serverRun) get(t *testing.T, path string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("building GET %s: %v", path, err)
	}

	got := s.send(t, req)

	return got.status, got.body
}

// assessmentRequest is the body POST /v1/assessments takes: the inventory, the
// targets, and the report's schemaVersion when the request names one.
type assessmentRequest struct {
	Inventory     json.RawMessage `json:"inventory"`
	Targets       []string        `json:"targets"`
	SchemaVersion *int            `json:"schemaVersion,omitempty"`
}

// assessRequest returns the body POST /v1/assessments takes for document and
// targets, naming no schemaVersion: the API answers the version it defaults to.
func assessRequest(t *testing.T, document []byte, targets ...string) []byte {
	t.Helper()

	return encodeAssessmentRequest(t, assessmentRequest{Inventory: document, Targets: targets})
}

// assessRequestVersion returns the body POST /v1/assessments takes for document
// and targets, naming schemaVersion: the API answers that version
// (docs/adr/0018, issue #307).
func assessRequestVersion(t *testing.T, document []byte, schemaVersion int, targets ...string) []byte {
	t.Helper()

	return encodeAssessmentRequest(t, assessmentRequest{
		Inventory:     document,
		Targets:       targets,
		SchemaVersion: &schemaVersion,
	})
}

// encodeAssessmentRequest encodes body as a request body, failing the test when
// it cannot.
func encodeAssessmentRequest(t *testing.T, body assessmentRequest) []byte {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request body: %v", err)
	}

	return encoded
}

// postAssessment posts an assessment of document against targets and returns the
// answer.
func (s *serverRun) postAssessment(t *testing.T, document []byte, targets ...string) answer {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/assessments",
		bytes.NewReader(assessRequest(t, document, targets...)))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	return s.send(t, req)
}

// TestServerServesTheAPI pins the server from the outside: the built binary
// listens on the address it is told, announces it, answers the health and
// readiness probes, lists every target and assesses one inventory, both against
// one target and against every target. The assessments run in parallel, so the
// server is told to serve as many at once as the test sends.
func TestServerServesTheAPI(t *testing.T) {
	t.Parallel()

	s := startServerBinary(t, emptyEnv(), "--concurrency", "4")

	document := readFixture(t, filepath.Join("testdata", "inventory.json"))

	t.Run("healthz", func(t *testing.T) {
		t.Parallel()

		if status, body := s.get(t, "/healthz"); status != http.StatusOK {
			t.Errorf("GET /healthz = %d, want %d; body: %q", status, http.StatusOK, body)
		}
	})

	t.Run("readyz", func(t *testing.T) {
		t.Parallel()

		if status, body := s.get(t, "/readyz"); status != http.StatusOK {
			t.Errorf("GET /readyz = %d, want %d; body: %q", status, http.StatusOK, body)
		}
	})

	t.Run("targets", func(t *testing.T) {
		t.Parallel()

		status, body := s.get(t, "/v1/targets")

		if status != http.StatusOK {
			t.Fatalf("GET /v1/targets = %d, want %d; body: %q", status, http.StatusOK, body)
		}

		var document struct {
			Targets []struct {
				Key string `json:"key"`
			} `json:"targets"`
		}

		if err := json.Unmarshal(body, &document); err != nil {
			t.Fatalf("GET /v1/targets is not JSON: %v\n%s", err, body)
		}

		var got, want []string

		for _, target := range document.Targets {
			got = append(got, target.Key)
		}

		for _, tc := range targets() {
			want = append(want, tc.key)
		}

		slices.Sort(want)

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /v1/targets keys mismatch (-want +got):\n%s", diff)
		}
	})

	lists := []struct {
		name    string
		targets []string
	}{
		{name: "one target", targets: []string{ovhcloudKey}},
		{name: "every target", targets: []string{allKey}},
	}

	for _, tc := range lists {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := s.postAssessment(t, document, tc.targets...)

			if got.status != http.StatusOK {
				t.Fatalf("POST /v1/assessments = %d, want %d; body: %q", got.status, http.StatusOK, got.body)
			}

			if !json.Valid(got.body) {
				t.Errorf("the answer is not valid JSON:\n%s", got.body)
			}

			if !bytes.HasSuffix(got.body, []byte("\n")) {
				t.Errorf("the answer does not end with a newline")
			}
		})
	}
}

// TestServerErrorStatuses pins the statuses a client can provoke, as the built
// binary answers them: one JSON error document with a code, and the right status.
// The token case runs a server of its own, because a token changes the
// configuration of the whole process.
func TestServerErrorStatuses(t *testing.T) {
	t.Parallel()

	s := startServerBinary(t, emptyEnv(), "--concurrency", "4")

	valid := assessRequest(t, readFixture(t, filepath.Join("testdata", "inventory.json")), ovhcloudKey)

	// One byte over the cap, which the server refuses before it reads the body.
	tooLarge := bytes.Repeat([]byte(" "), maxBodySize+1)

	tests := []struct {
		name        string
		method      string
		path        string
		body        []byte
		contentType string
		want        int
		wantCode    string
	}{
		{
			name:        "a malformed body",
			method:      http.MethodPost,
			path:        "/v1/assessments",
			body:        []byte(`{"inventory": `),
			contentType: "application/json",
			want:        http.StatusBadRequest,
			wantCode:    "invalid_request",
		},
		{
			name:   "an inventory the reader refuses",
			method: http.MethodPost,
			path:   "/v1/assessments",
			// The value is unmistakable, so a body that echoes it fails the
			// check below: the API names the field, never the value.
			body:        []byte(`{"inventory": {"schemaVersion": 987654321, "source": "aws", "resources": []}, "targets": ["ovhcloud"]}`),
			contentType: "application/json",
			want:        http.StatusBadRequest,
			wantCode:    "invalid_inventory",
		},
		{
			name:        "an unknown target",
			method:      http.MethodPost,
			path:        "/v1/assessments",
			body:        []byte(`{"inventory": {"schemaVersion": 1, "source": "aws", "resources": []}, "targets": ["nope"]}`),
			contentType: "application/json",
			want:        http.StatusBadRequest,
			wantCode:    "invalid_targets",
		},
		{
			name:     "an unknown path",
			method:   http.MethodGet,
			path:     "/v1/unknown",
			want:     http.StatusNotFound,
			wantCode: "not_found",
		},
		{
			name:     "a method a route does not serve",
			method:   http.MethodGet,
			path:     "/v1/assessments",
			want:     http.StatusMethodNotAllowed,
			wantCode: "method_not_allowed",
		},
		{
			name:        "a body over the cap",
			method:      http.MethodPost,
			path:        "/v1/assessments",
			body:        tooLarge,
			contentType: "application/json",
			want:        http.StatusRequestEntityTooLarge,
			wantCode:    "payload_too_large",
		},
		{
			name:        "a body that is not JSON",
			method:      http.MethodPost,
			path:        "/v1/assessments",
			body:        valid,
			contentType: "text/plain",
			want:        http.StatusUnsupportedMediaType,
			wantCode:    "unsupported_media_type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var reader io.Reader
			if tc.body != nil {
				reader = bytes.NewReader(tc.body)
			}

			req, err := http.NewRequestWithContext(t.Context(), tc.method, tc.path, reader)
			if err != nil {
				t.Fatalf("building the request: %v", err)
			}

			if tc.body != nil {
				req.Header.Set("Content-Type", tc.contentType)
			}

			got := s.send(t, req)

			if got.status != tc.want {
				t.Fatalf("%s %s = %d, want %d; body: %q", tc.method, tc.path, got.status, tc.want, got.body)
			}

			if code := errorCode(t, got.body); code != tc.wantCode {
				t.Errorf("%s %s error code = %q, want %q; body: %q", tc.method, tc.path, code, tc.wantCode, got.body)
			}

			// An error body names a field or a position, never a value the
			// client sent (ADR 0019, the CLI's rule for its own errors).
			for _, echoed := range []string{"987654321", "nope"} {
				if bytes.Contains(got.body, []byte(echoed)) {
					t.Errorf("%s %s echoes %q in its error body: %q", tc.method, tc.path, echoed, got.body)
				}
			}
		})
	}

	t.Run("no bearer token", func(t *testing.T) {
		t.Parallel()

		secured := startServerBinary(t, []string{serverTokenEnv + "=" + testServerToken})

		if status, body := secured.get(t, "/v1/targets"); status != http.StatusUnauthorized {
			t.Errorf("GET /v1/targets without a token = %d, want %d; body: %q",
				status, http.StatusUnauthorized, body)
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/targets", nil)
		if err != nil {
			t.Fatalf("building the request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer "+testServerToken)

		got := secured.send(t, req)

		if got.status != http.StatusOK {
			t.Errorf("GET /v1/targets with the token = %d, want %d; body: %q", got.status, http.StatusOK, got.body)
		}

		// The health endpoints stay open for the platform's probes.
		if status, body := secured.get(t, "/healthz"); status != http.StatusOK {
			t.Errorf("GET /healthz with a token configured = %d, want %d; body: %q", status, http.StatusOK, body)
		}
	})
}

// errorCode returns the code of an error document, and fails the test when body
// is not one.
func errorCode(t *testing.T, body []byte) string {
	t.Helper()

	var document struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the error body is not a JSON error document: %v; body: %q", err, body)
	}

	if document.Error.Code == "" {
		t.Fatalf("the error body holds no code: %q", body)
	}

	return document.Error.Code
}

// TestServerLoopbackRule pins the startup rule of ADR 0019 from the outside: the
// built binary refuses to listen on a non-loopback address without a token, and
// starts when the operator passes --insecure-no-auth or configures a token. A
// refused run exits non-zero with one line and opens no port.
func TestServerLoopbackRule(t *testing.T) {
	t.Parallel()

	requireServerBinary(t)

	t.Run("refused without a token", func(t *testing.T) {
		t.Parallel()

		stderr := &lockedBuffer{}

		cmd := exec.Command(serverBinary(t), "--listen", "0.0.0.0:0")
		cmd.Env = []string{}
		cmd.Stderr = stderr

		err := cmd.Run()
		if err == nil {
			t.Fatalf("shift2eu-server listened on 0.0.0.0:0 without a token; stderr = %q", stderr.String())
		}

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running shift2eu-server: %v", err)
		}

		if code := exitErr.ExitCode(); code == 0 {
			t.Errorf("exit code = %d, want non-zero", code)
		}

		if announcedAddress(stderr.String()) != "" {
			t.Errorf("the server announced an address, so it listened: %q", stderr.String())
		}

		if !strings.HasPrefix(stderr.String(), serverErrPrefix) {
			t.Errorf("stderr = %q, want it to start with %q", stderr.String(), serverErrPrefix)
		}

		if strings.Count(strings.TrimSuffix(stderr.String(), "\n"), "\n") != 0 {
			t.Errorf("stderr = %q, want exactly one line", stderr.String())
		}
	})

	t.Run("started with --insecure-no-auth", func(t *testing.T) {
		t.Parallel()

		s := startServerBinary(t, emptyEnv(), "--insecure-no-auth", "--listen", "0.0.0.0:0")

		if status, body := s.get(t, "/healthz"); status != http.StatusOK {
			t.Errorf("GET /healthz = %d, want %d; body: %q", status, http.StatusOK, body)
		}
	})

	t.Run("started with a token", func(t *testing.T) {
		t.Parallel()

		s := startServerBinary(t, []string{serverTokenEnv + "=" + testServerToken}, "--listen", "0.0.0.0:0")

		if status, body := s.get(t, "/healthz"); status != http.StatusOK {
			t.Errorf("GET /healthz = %d, want %d; body: %q", status, http.StatusOK, body)
		}
	})
}

// TestServerShutsDownOnSIGTERM pins the shutdown a container platform relies on:
// SIGTERM ends the process with exit code 0, not the 143 of a killed process.
func TestServerShutsDownOnSIGTERM(t *testing.T) {
	t.Parallel()

	s := startServerBinary(t, emptyEnv())

	// The server answers before the signal, so a process that never listened
	// cannot pass this test by exiting.
	if status, body := s.get(t, "/healthz"); status != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want %d; body: %q", status, http.StatusOK, body)
	}

	if code := s.terminate(t); code != 0 {
		t.Errorf("exit code after SIGTERM = %d, want 0; stderr = %q", code, s.stderr.String())
	}
}

// TestServerMatchesTheCLIBytes pins the byte parity PRD #103 promises end to end:
// for the shared fixture and for each target and every target, the body of POST
// /v1/assessments equals the stdout of `shift2eu assess` for the same inventory and
// targets, in JSON and in Markdown, and in both JSON report versions, each binary
// asked for the version that makes the two comparable (docs/adr/0018, issue #307).
// Each binary is wired by its own composition root, so a difference in decoding,
// ordering or rendering shows as a difference in the bytes.
func TestServerMatchesTheCLIBytes(t *testing.T) {
	t.Parallel()

	inventory := filepath.Join("testdata", "inventory.json")
	document := readFixture(t, inventory)

	// The subtests run in parallel, each one assessment: tell the server to
	// serve them all at once rather than to refuse some as busy.
	s := startServerBinary(t, emptyEnv(), "--concurrency", "8")

	targetLists := make([][]string, 0, len(targets())+1)

	for _, tc := range targets() {
		targetLists = append(targetLists, []string{tc.key})
	}

	targetLists = append(targetLists, []string{allKey})

	// The JSON report has two versions, and the two binaries default to different
	// ones: the API answers version 2 unless the request names a version, and the
	// CLI writes version 3 unless --schema-version names one (docs/adr/0018,
	// docs/adr/0030, issue #307). Each version is compared like with like. The
	// Markdown report has no version.
	formats := []struct {
		name   string
		format string
		accept string
		// apiVersion is the schemaVersion the request body names, and 0 for no
		// schemaVersion at all: the version the API defaults to.
		apiVersion int
		// cliArgs are the arguments that select the version the CLI writes.
		cliArgs []string
	}{
		{
			name:    "JSON in the API's default version 2",
			format:  jsonFormat,
			accept:  "application/json",
			cliArgs: []string{"--schema-version", "2"},
		},
		{
			name:       "JSON in version 3",
			format:     jsonFormat,
			accept:     "application/json",
			apiVersion: 3,
		},
		{name: "Markdown", format: markdownFormat, accept: "text/markdown"},
	}

	for _, targetList := range targetLists {
		for _, f := range formats {
			t.Run(f.name+"/"+strings.Join(targetList, "+"), func(t *testing.T) {
				t.Parallel()

				body := assessRequest(t, document, targetList...)
				if f.apiVersion != 0 {
					body = assessRequestVersion(t, document, f.apiVersion, targetList...)
				}

				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/assessments",
					bytes.NewReader(body))
				if err != nil {
					t.Fatalf("building the request: %v", err)
				}

				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", f.accept)

				got := s.send(t, req)

				if got.status != http.StatusOK {
					t.Fatalf("POST /v1/assessments = %d, want %d; body: %q",
						got.status, http.StatusOK, got.body)
				}

				cliArgs := append([]string{"--format", f.format}, f.cliArgs...)

				cli := assess(t, strings.Join(targetList, ","), inventory, cliArgs...)

				if cli.code != 0 {
					t.Fatalf("shift2eu assess %s = %d; stderr = %q",
						strings.Join(cliArgs, " "), cli.code, cli.stderr)
				}

				if diff := cmp.Diff(cli.stdout, string(got.body)); diff != "" {
					t.Errorf("the API and the CLI differ for %v in %s (-CLI +API):\n%s",
						targetList, f.name, diff)
				}
			})
		}
	}
}

// TestServerPathHasNoCloudSDKOrProcess pins the dependency criterion of PRD #103
// for the server: no package of `go list -deps ./cmd/shift2eu-server` is a cloud
// SDK, and none starts a process. The server serves HTTP, so it does import net
// and net/http: the check is the one its role needs, beside
// TestAssessPathHasNoNetworkOrCloudSDK for the CLI, which stays unchanged.
func TestServerPathHasNoCloudSDKOrProcess(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}

	cmd := exec.Command("go", "list", "-deps", "./cmd/shift2eu-server")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list -deps ./cmd/shift2eu-server: %v\n%s", err, exitErr.Stderr)
		}

		t.Fatalf("go list -deps ./cmd/shift2eu-server: %v", err)
	}

	deps := strings.Fields(string(out))

	for _, pkg := range deps {
		if forbiddenServerDependency(pkg) {
			t.Errorf("the server depends on %s; a server package may reach no cloud SDK and start no process", pkg)
		}
	}

	// The listing must hold the server and what it serves, so a run that read
	// nothing of it cannot pass.
	for _, want := range []string{
		"github.com/timothymamo/shift2eu/cmd/shift2eu-server",
		"github.com/timothymamo/shift2eu/internal/httpapi",
	} {
		if !slices.Contains(deps, want) {
			t.Errorf("the server does not depend on %s, so the checks above cover nothing of it", want)
		}
	}
}

// forbiddenServerDependency reports whether pkg is one the server must not depend
// on: a cloud SDK, which would put a cloud client with credentials in a process
// that serves untrusted clients, or os/exec, which would let it start a process.
// A denied entry is a module path without a trailing slash, matched as the
// package itself and as the prefix of its subpackages; the slash the prefix test
// adds is why an entry that ends in one would match nothing.
func forbiddenServerDependency(pkg string) bool {
	if pkg == "os/exec" || strings.HasPrefix(pkg, "os/exec/") {
		return true
	}

	for _, denied := range []string{
		"github.com/aws/aws-sdk-go-v2",
		"github.com/Azure/azure-sdk-for-go",
		"cloud.google.com/go",
		"google.golang.org/api",
		"github.com/scaleway",
		"github.com/ovh",
		"github.com/UpCloudLtd",
	} {
		if pkg == denied || strings.HasPrefix(pkg, denied+"/") {
			return true
		}
	}

	return false
}

// readFixture returns the bytes of a fixture of this package.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()

	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture %s: %v", path, err)
	}

	return document
}

// requireServerBinary fails the test when the harness could not build the server
// binary, reporting the build's own output.
func requireServerBinary(t *testing.T) {
	t.Helper()

	if serverBuildErr != nil {
		t.Fatalf("the shift2eu-server binary did not build: %v", serverBuildErr)
	}
}

// serverBinary returns the path of the built server binary.
func serverBinary(t *testing.T) string {
	t.Helper()

	requireServerBinary(t)

	return serverBinaryPath
}
