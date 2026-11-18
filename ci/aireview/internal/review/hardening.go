package review

import (
	"net/http"
	"strings"
	"time"
)

// NewHTTPClient returns the client both APIs are built with: it has timeout
// and follows no redirect. A 3xx answer is returned as it is, a status other
// than 200, so the call fails with ErrModel or ErrGitHub and the host the
// answer names never sees the x-api-key, the token or the diff (ADR 0033).
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// annotationEscaper escapes a workflow command's data. "%" comes first, so an
// escape the text already holds is escaped itself.
var annotationEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")

// AnnotationData escapes s as the data of a GitHub workflow command (% as %25,
// CR as %0D, LF as %0A), so the text stays on the annotation's one line and
// cannot start a command of its own.
func AnnotationData(s string) string {
	return annotationEscaper.Replace(s)
}
