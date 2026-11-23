package publish_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// fingerprintPrefix is the versioned prefix of a fingerprint's digest input.
// The ADR writes it once; the tests build the input from it rather than from an
// implementation's output.
const fingerprintPrefix = "shift2eu-audit-fingerprint/v1"

// fingerprintInput is the ADR's digest input: the versioned prefix, then the
// rule, the file and the sink, each after the NUL byte that ends the field
// before it.
func fingerprintInput(rule, file, sink string) []byte {
	return []byte(fingerprintPrefix + "\x00" + rule + "\x00" + file + "\x00" + sink)
}

// testKeyBytes is an invented key of the runbook's minimum length: the bytes of
// a phrase, so the scan tests can find them in a text. It is no credential.
const testKeyBytes = "please-do-not-use-this-key-abcdefgh"

// testKey returns a FingerprintKey over testKeyBytes.
func testKey(t *testing.T) publish.FingerprintKey {
	t.Helper()

	key, err := publish.NewFingerprintKey([]byte(testKeyBytes))
	if err != nil {
		t.Fatalf("NewFingerprintKey over %d bytes: %v", len(testKeyBytes), err)
	}

	return key
}

// TestPlainFingerprintIsTheFirstSixBytesOfTheDigest pins the fingerprint's
// shape and its input: fp- and the first six bytes of SHA-256 over the ADR's
// versioned, NUL-separated input, computed here from that input and never
// copied from an implementation.
func TestPlainFingerprintIsTheFirstSixBytesOfTheDigest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rule, file string
		sink       string
	}{
		{
			name: "a workflow rule",
			rule: "zizmor:template-injection",
			file: ".github/workflows/ci.yml",
			sink: "run",
		},
		{
			name: "a dependency rule",
			rule: "GO-2026-1234",
			file: "go.mod",
			sink: "net/http.(*Client).Do",
		},
		{
			name: "a release artifact",
			rule: "checksums-mismatch",
			file: "shift2eu_0.1.0_linux_amd64.tar.gz",
			sink: "checksums.txt",
		},
		{
			name: "a history secret names the commit",
			rule: "gitleaks:generic-api-key",
			file: testCommit,
			sink: "history",
		},
		{
			name: "a sink with a bracket and a star",
			rule: "G304",
			file: "internal/cli/run.go",
			sink: "os.Open([*]string)",
		},
	}

	shape := regexp.MustCompile(`^fp-[0-9a-f]{12}$`)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sum := sha256.Sum256(fingerprintInput(tc.rule, tc.file, tc.sink))
			want := publish.Fingerprint("fp-" + hex.EncodeToString(sum[:6]))

			got := publish.PlainFingerprint(tc.rule, tc.file, tc.sink)
			if got != want {
				t.Errorf("PlainFingerprint(%q, %q, %q) = %q, want %q: fp- and the first six bytes of SHA-256 over the ADR's input",
					tc.rule, tc.file, tc.sink, got, want)
			}

			if !shape.MatchString(string(got)) {
				t.Errorf("PlainFingerprint(%q, %q, %q) = %q, want fp- and 12 lowercase hex characters",
					tc.rule, tc.file, tc.sink, got)
			}
		})
	}
}

// TestFingerprintInputSeparatesItsFields pins the NUL separator: two findings
// whose concatenated fields are one string keep two fingerprints, so nobody can
// move a rule boundary and land on another finding's fingerprint.
func TestFingerprintInputSeparatesItsFields(t *testing.T) {
	t.Parallel()

	if a, b := publish.PlainFingerprint("a", "b", "c"), publish.PlainFingerprint("ab", "", "c"); a == b {
		t.Errorf("PlainFingerprint(\"a\", \"b\", \"c\") and PlainFingerprint(\"ab\", \"\", \"c\") are both %q, want different fingerprints: the fields are NUL-separated", a)
	}

	if a, b := publish.PlainFingerprint("ab", "c", "d"), publish.PlainFingerprint("a", "bc", "d"); a == b {
		t.Errorf("PlainFingerprint(\"ab\", \"c\", \"d\") and PlainFingerprint(\"a\", \"bc\", \"d\") are both %q, want different fingerprints: the fields are NUL-separated", a)
	}
}

// TestKeyedFingerprintIsHMAC pins that a private finding's fingerprint is
// HMAC-SHA-256 with the key's bytes over the same input, that it differs from
// the plain one, and that the whole key is used: a longer key gives another
// fingerprint, so truncating the secret cannot pass for it.
func TestKeyedFingerprintIsHMAC(t *testing.T) {
	t.Parallel()

	const (
		rule = "gitleaks:generic-api-key"
		file = testCommit
		sink = "history"
	)

	key := testKey(t)

	mac := hmac.New(sha256.New, []byte(testKeyBytes))
	mac.Write(fingerprintInput(rule, file, sink))

	want := publish.Fingerprint("fp-" + hex.EncodeToString(mac.Sum(nil)[:6]))

	got := publish.KeyedFingerprint(key, rule, file, sink)
	if got != want {
		t.Errorf("KeyedFingerprint(key, %q, %q, %q) = %q, want %q: fp- and the first six bytes of HMAC-SHA-256 with the key",
			rule, file, sink, got, want)
	}

	if got == publish.PlainFingerprint(rule, file, sink) {
		t.Errorf("KeyedFingerprint(key, %q, %q, %q) = %q, the plain fingerprint: a private finding's fingerprint must not be testable without the key",
			rule, file, sink, got)
	}

	if again := publish.KeyedFingerprint(key, rule, file, sink); again != got {
		t.Errorf("KeyedFingerprint with one key gave %q and then %q, want one fingerprint", got, again)
	}

	longer, err := publish.NewFingerprintKey([]byte(testKeyBytes + "-and-more"))
	if err != nil {
		t.Fatalf("NewFingerprintKey over %d bytes: %v", len(testKeyBytes+"-and-more"), err)
	}

	if extended := publish.KeyedFingerprint(longer, rule, file, sink); extended == got {
		t.Errorf("a key with extra bytes on the end gives the same fingerprint %q, want another: the whole key is the HMAC's key", extended)
	}
}

// TestNewFingerprintKey pins the runbook's minimum: the secret's bytes as they
// are, refused below 32 bytes as a configuration error.
func TestNewFingerprintKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		length  int
		wantErr bool
	}{
		{"no bytes", 0, true},
		{"one byte", 1, true},
		{"31 bytes, one short", 31, true},
		{"32 bytes, the minimum", 32, false},
		{"64 bytes", 64, false},
		{"a long secret", 200, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			secret := make([]byte, tc.length)
			for i := range secret {
				secret[i] = byte('a' + i%26)
			}

			key, err := publish.NewFingerprintKey(secret)

			switch {
			case tc.wantErr && err == nil:
				t.Fatalf("NewFingerprintKey over %d bytes returned the key %v, want an error wrapping ErrConfig", tc.length, key)
			case !tc.wantErr && err != nil:
				t.Fatalf("NewFingerprintKey over %d bytes: %v", tc.length, err)
			}

			if !tc.wantErr {
				// The key holds the secret's bytes as they are: the fingerprint
				// of a fixed input matches HMAC over those very bytes.
				mac := hmac.New(sha256.New, secret)
				mac.Write(fingerprintInput("r", "f", "s"))
				want := publish.Fingerprint("fp-" + hex.EncodeToString(mac.Sum(nil)[:6]))

				if got := publish.KeyedFingerprint(key, "r", "f", "s"); got != want {
					t.Errorf("KeyedFingerprint over a %d-byte secret = %q, want %q: the key is the secret's bytes unchanged", tc.length, got, want)
				}

				return
			}

			if !errors.Is(err, publish.ErrConfig) {
				t.Errorf("NewFingerprintKey over %d bytes: error %v does not wrap ErrConfig", tc.length, err)
			}

			// A secret of fewer than eight bytes has no eight-byte run to look
			// for: the needle itself is made of the error's own letters.
			if msg := err.Error(); msg == "" || (len(secret) >= 8 && strings.Contains(msg, string(secret[:8]))) {
				t.Errorf("NewFingerprintKey over %d bytes: error %q names the secret's bytes, want a field and a rule only", tc.length, msg)
			}
		})
	}
}

// TestFingerprintIsStableForTheSameInput pins that the plain fingerprint is a
// function of rule, file and sink alone: the same three values give the same
// fingerprint in every call, and a change in any one of them gives another.
func TestFingerprintIsStableForTheSameInput(t *testing.T) {
	t.Parallel()

	base := publish.PlainFingerprint("G304", "internal/cli/run.go", "os.Open")

	if again := publish.PlainFingerprint("G304", "internal/cli/run.go", "os.Open"); again != base {
		t.Errorf("PlainFingerprint gave %q and then %q for one input, want one fingerprint", base, again)
	}

	others := map[string]publish.Fingerprint{
		"another rule": publish.PlainFingerprint("G305", "internal/cli/run.go", "os.Open"),
		"another file": publish.PlainFingerprint("G304", "internal/cli/open.go", "os.Open"),
		"another sink": publish.PlainFingerprint("G304", "internal/cli/run.go", "os.Create"),
	}

	for name, got := range others {
		if got == base {
			t.Errorf("%s gives the fingerprint %q, want another: all three fields are inputs", name, got)
		}
	}
}
