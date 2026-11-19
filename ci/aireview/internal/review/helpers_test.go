package review_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
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

// writeExecutable writes content to path as an executable without the ETXTBSY
// race (issue #287, as test/devenv's writeFileMode does since #286).
// syscall.ForkLock.RLock is held while the file is created, written and closed,
// so a fork in a parallel test cannot inherit the write descriptor and make a
// later exec of the file fail with "text file busy"; the executable bit is set
// after the descriptor is closed, and chmod opens no write descriptor.
func writeExecutable(t *testing.T, path string, content []byte) {
	t.Helper()

	syscall.ForkLock.RLock()
	err := os.WriteFile(path, content, 0o600)
	syscall.ForkLock.RUnlock()

	if err != nil {
		t.Fatalf("write the executable %s: %v", path, err)
	}

	if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // a fake CLI the test runs must be executable
		t.Fatalf("make %s executable: %v", path, err)
	}
}
