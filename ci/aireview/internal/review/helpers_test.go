package review_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The commits, logins and credentials the tests use: 40 hex characters, as
// GitHub names a commit, a login of the reviewer App, and obvious fakes for a
// token and an API key, never a real credential (CLAUDE.md rule 3).
const (
	headSHA        = "0123456789abcdef0123456789abcdef01234567"
	baseSHA        = "fedcba9876543210fedcba9876543210fedcba98"
	oldSHA         = "1111111111111111111111111111111111111111"
	testToken      = "test-token"
	testModelToken = "test-oauth-token"
	testAPIKey     = "test-anthropic-api-key"
	testBotLogin   = "shift2eu-reviewer[bot]"
)

// server starts an httptest server for handle and closes it when the test ends.
func server(t *testing.T, handle http.HandlerFunc) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(handle)
	t.Cleanup(srv.Close)

	return srv
}
