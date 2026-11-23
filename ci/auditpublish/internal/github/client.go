package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// Token is gh's token, which the entry point passes through from the step's
// GH_TOKEN. It is used only as the GH_TOKEN entry of gh's environment, and it
// formats as a placeholder, never as its value.
type Token struct {
	value string
}

// NewToken returns the token.
func NewToken(value string) Token { return Token{value: value} }

// String is the placeholder a Token formats as.
func (Token) String() string { return "Token(redacted)" }

// GoString is the placeholder a Token formats as with %#v.
func (Token) GoString() string { return "Token(redacted)" }

// Scope is what the endpoint table fills its patterns and body rules with: the
// repository and the run the program publishes for.
type Scope struct {
	// Repository is GITHUB_REPOSITORY, owner/name.
	Repository string
	// RunID is GITHUB_RUN_ID.
	RunID string
	// Date is the run's UTC date, YYYY-MM-DD, which names the report's branch
	// and file.
	Date string
	// Commit is the audited main commit, 40 lowercase hex: the report commit's
	// one parent and the compare check's base. Empty for the artifact check.
	Commit string
}

// ClientConfig is how a Client runs gh.
type ClientConfig struct {
	// GHPath is gh's absolute path. gh is never looked up on PATH.
	GHPath string
	// Token is gh's GH_TOKEN.
	Token Token
	// TempDir is an existing absolute directory under which the Client makes
	// gh's fresh configuration directory, removed by Close.
	TempDir string
	// Scope is the repository and run every call is checked against.
	Scope Scope
	// CallTimeout bounds each call; zero is defaultTimeout, a negative one is
	// no configuration.
	CallTimeout time.Duration
	// ReadOnly refuses every method but GET before the endpoint table and
	// before gh runs: the artifacts subcommand's client, which reads the run's
	// artifacts and needs no wider table than that.
	ReadOnly bool
}

// Request is one GitHub API call.
type Request struct {
	// Method is GET, POST or PATCH.
	Method string
	// Endpoint is the path with its query string, without a leading slash:
	// repos/<owner>/<name>/....
	Endpoint string
	// Body is marshalled to JSON and given to gh on stdin; nil sends none.
	Body any
}

// Response is GitHub's answer as gh's --include output gives it.
type Response struct {
	// Status is the HTTP status of the status line.
	Status int
	// Link is the Link header, empty when there is none.
	Link string
	// Body is the response body.
	Body []byte
}

// maxOutput caps gh's stdout, status line and headers included: an answer
// over it is a GitHub failure, never a truncated body.
const maxOutput = 8 << 20

// The two headers every call passes gh.
const (
	acceptHeader  = "Accept: application/vnd.github+json"
	versionHeader = "X-GitHub-Api-Version: 2022-11-28"
)

var (
	statusLine = regexp.MustCompile(`^HTTP/[0-9](?:\.[0-9])? ([1-5][0-9]{2})(?: [^\r\n]*)?$`)
	commitHex  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Client runs gh api for the calls the endpoint table allows. It formats as a
// placeholder, never with its token.
type Client struct {
	ghPath   string
	token    Token
	dir      string
	scope    Scope
	timeout  time.Duration
	readOnly bool
}

// clientPlaceholder is what a Client formats as with every verb: fmt does not
// call Token's String on an unexported field, so the Client hides it itself.
const clientPlaceholder = "github.Client(token redacted)"

// String is the placeholder a Client formats as.
func (Client) String() string { return clientPlaceholder }

// GoString is the placeholder a Client formats as with %#v.
func (Client) GoString() string { return clientPlaceholder }

// NewClient returns a Client, and refuses (publish.ErrConfig) a gh path that is
// not absolute, a temporary directory that is not an absolute existing
// directory, an empty token, a negative call timeout, and a scope NewRun would
// refuse. It makes gh's
// fresh configuration directory under the temporary directory; Close removes
// it.
func NewClient(cfg ClientConfig) (*Client, error) {
	if err := checkProgram(cfg.GHPath); err != nil {
		return nil, fmt.Errorf("github client: gh: %w", err)
	}

	if err := checkTempDir(cfg.TempDir); err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}

	if cfg.Token.value == "" {
		return nil, fmt.Errorf("github client: an empty token: %w", publish.ErrConfig)
	}

	if err := validateScope(cfg.Scope); err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}

	timeout, err := timeoutOf(cfg.CallTimeout)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}

	dir, err := os.MkdirTemp(cfg.TempDir, "gh-")
	if err != nil {
		return nil, fmt.Errorf("github client: gh's configuration directory not made: %w", publish.ErrConfig)
	}

	return &Client{ghPath: cfg.GHPath, token: cfg.Token, dir: dir, scope: cfg.Scope, timeout: timeout, readOnly: cfg.ReadOnly}, nil
}

// checkProgram refuses a program path that is not absolute and clean or does
// not name an existing file.
func checkProgram(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("the path is not absolute: %w", publish.ErrConfig)
	}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("the path names no file: %w", publish.ErrConfig)
	}

	return nil
}

// checkTempDir refuses a temporary directory that is not absolute or not an
// existing directory.
func checkTempDir(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("the temporary directory is not absolute: %w", publish.ErrConfig)
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("the temporary directory is no directory: %w", publish.ErrConfig)
	}

	return nil
}

// validateScope refuses a scope whose repository, run ID or date the core's
// NewRun refuses, or whose commit is neither empty nor 40 lowercase hex. The
// server URL is no part of a scope: NewRun is given a fixed valid one.
func validateScope(s Scope) error {
	if _, err := publish.NewRun("https://github.com", s.Repository, s.RunID, s.Date); err != nil {
		return fmt.Errorf("the scope: %w", err)
	}

	if s.Commit != "" && !commitHex.MatchString(s.Commit) {
		return fmt.Errorf("the scope: the commit is not 40 lowercase hex: %w", publish.ErrConfig)
	}

	return nil
}

// Call refuses any method but GET on a read-only client, checks the request
// against the endpoint table (Allowed) and only then
// runs gh api --method <M> --include with the two fixed headers, the body on
// stdin (--input - only when there is one), and gh's own environment. gh's
// stderr is discarded unread.
//
// Every answer with a status line is a Response and no error, a 404 or any
// other non-2xx included: the caller decides what a status means. A refused
// request wraps ErrEndpointRefused and runs nothing; a gh that fails without a
// status line, or whose output is over the cap, wraps publish.ErrGitHub.
//
// Each call runs under the configured timeout. A call whose context ended,
// its deadline or the caller's cancellation, wraps publish.ErrGitHub with an
// empty Response whatever gh printed, and a context already done never runs
// gh: only a gh that ran to its end can answer 404.
func (c *Client) Call(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.dir == "" {
		return Response{}, fmt.Errorf("github call: no open client: %w", publish.ErrConfig)
	}

	if c.readOnly && req.Method != "GET" {
		return Response{}, fmt.Errorf("github call: a method other than GET on a read-only client: %w", ErrEndpointRefused)
	}

	var body []byte

	if req.Body != nil {
		b, err := json.Marshal(req.Body)
		if err != nil {
			return Response{}, fmt.Errorf("github call: a body that is no JSON: %w", ErrEndpointRefused)
		}

		body = b
	}

	if err := Allowed(c.scope, req.Method, req.Endpoint, body); err != nil {
		return Response{}, fmt.Errorf("github call: %w", err)
	}

	if ctx.Err() != nil {
		return Response{}, fmt.Errorf("github call: the call's context ended before gh ran: %w", publish.ErrGitHub)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{"api", "--method", req.Method, "--include", "-H", acceptHeader, "-H", versionHeader}
	if body != nil {
		args = append(args, "--input", "-")
	}

	args = append(args, req.Endpoint)

	out := &cappedBuffer{limit: maxOutput}

	//nolint:gosec // G204: gh's absolute path from the config; the arguments are fixed and the endpoint passed Allowed above
	cmd := exec.CommandContext(ctx, c.ghPath, args...)
	cmd.Env = c.env()
	cmd.Dir = c.dir
	cmd.Stdout = out
	cmd.Stderr = nil
	cmd.WaitDelay = waitDelay

	if body != nil {
		cmd.Stdin = bytes.NewReader(body)
	}

	runErr := cmd.Run()

	if ctx.Err() != nil {
		return Response{}, fmt.Errorf("github call: the call's context ended: %w", publish.ErrGitHub)
	}

	if out.over {
		return Response{}, fmt.Errorf("github call: gh's output is over the cap: %w", publish.ErrGitHub)
	}

	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return Response{}, fmt.Errorf("github call: gh did not run to its end: %w", publish.ErrGitHub)
	}

	resp, ok := parseInclude(out.buf.Bytes())
	if !ok {
		return Response{}, fmt.Errorf("github call: gh's output has no status line: %w", publish.ErrGitHub)
	}

	if runErr != nil && resp.Status/100 == 2 {
		return Response{}, fmt.Errorf("github call: gh failed on a success status: %w", publish.ErrGitHub)
	}

	return resp, nil
}

// env is gh's whole environment: nothing is inherited.
func (c *Client) env() []string {
	return []string{
		"GH_TOKEN=" + c.token.value,
		"GH_CONFIG_DIR=" + c.dir,
		"GH_NO_UPDATE_NOTIFIER=1",
		"GH_PROMPT_DISABLED=1",
		"NO_COLOR=1",
		"HOME=" + c.dir,
	}
}

// parseInclude reads gh api --include's output: a status line, headers, a
// blank line and the body. It reports false when the status line is missing or
// malformed or the headers never end.
func parseInclude(out []byte) (Response, bool) {
	var (
		status int
		links  []string
	)

	rest := out

	for i := 0; ; i++ {
		raw, next, ok := bytes.Cut(rest, []byte("\n"))
		if !ok {
			return Response{}, false
		}

		rest = next
		line := strings.TrimSuffix(string(raw), "\r")

		if i == 0 {
			m := statusLine.FindStringSubmatch(line)
			if m == nil {
				return Response{}, false
			}

			s, err := strconv.Atoi(m[1])
			if err != nil {
				return Response{}, false
			}

			status = s

			continue
		}

		if line == "" {
			return Response{Status: status, Link: strings.Join(links, ", "), Body: rest}, true
		}

		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return Response{}, false
		}

		if strings.EqualFold(strings.TrimSpace(name), "Link") {
			links = append(links, strings.TrimSpace(value))
		}
	}
}

// cappedBuffer keeps at most limit bytes and records that more arrived. It
// never fails a write, so gh is never blocked on a full pipe: the excess is
// read and dropped, and the call fails on over.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.over = true
		b.buf.Write(p[:max(room, 0)])

		return len(p), nil
	}

	b.buf.Write(p)

	return len(p), nil
}

// Close removes gh's configuration directory, and only it. It may be called
// more than once.
func (c *Client) Close() error {
	if c == nil || c.dir == "" {
		return nil
	}

	if err := os.RemoveAll(c.dir); err != nil {
		return fmt.Errorf("github client: gh's configuration directory not removed: %w", publish.ErrConfig)
	}

	c.dir = ""

	return nil
}
