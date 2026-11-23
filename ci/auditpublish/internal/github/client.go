package github

import (
	"context"
	"fmt"
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

// Client runs gh api for the calls the endpoint table allows.
type Client struct{}

// NewClient returns a Client, and refuses (publish.ErrConfig) a gh path that is
// not absolute, a temporary directory that is not an absolute existing
// directory, and a scope NewRun would refuse.
func NewClient(_ ClientConfig) (*Client, error) {
	return nil, fmt.Errorf("github client: %w", errNotImplemented)
}

// Call checks the request against the endpoint table (Allowed) and only then
// runs gh api --method <M> --include with the two fixed headers, the body on
// stdin, and gh's own environment. gh's stderr is discarded unread. A refused
// request wraps ErrEndpointRefused and runs nothing; a gh that fails without a
// status line, or whose output is over the cap, wraps publish.ErrGitHub.
func (c *Client) Call(_ context.Context, _ Request) (Response, error) {
	return Response{}, fmt.Errorf("github call: %w", errNotImplemented)
}

// Close removes gh's configuration directory.
func (c *Client) Close() error {
	return nil
}
