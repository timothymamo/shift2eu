package publish_test

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/ci/auditpublish/internal/publish"
)

// otherKey returns a second invented key, so a test can show that a text is
// refused for the key it holds its bytes and for nothing else.
func otherKey(t *testing.T) publish.FingerprintKey {
	t.Helper()

	const text = "another-key-that-is-not-this-one-ok"

	key, err := publish.NewFingerprintKey([]byte(text))
	if err != nil {
		t.Fatalf("NewFingerprintKey over %d bytes: %v", len(text), err)
	}

	return key
}

// wantRefused fails the test unless err wraps ErrScanRefused and its text holds
// nothing of what was found.
func wantRefused(t *testing.T, err error, found string) {
	t.Helper()

	if err == nil {
		t.Fatalf("the scan allowed the text, want an error wrapping ErrScanRefused")
	}

	if !errors.Is(err, publish.ErrScanRefused) {
		t.Fatalf("the scan's error %v does not wrap ErrScanRefused", err)
	}

	if found != "" && strings.Contains(err.Error(), found) {
		t.Errorf("the scan's error %q holds the text it found, want nothing of it", err.Error())
	}
}

// wantAllowed fails the test when the scan refused a text it must publish.
func wantAllowed(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("the scan refused the text: %v", err)
	}
}

// TestScanRefusesTokenPatterns pins each pattern of ADR 0040's scan in the
// clear, over advisory text: a Claude token, a private key's PEM header, a
// GitHub token of each kind, a fine-grained token and an age secret key. Every
// hit wraps ErrScanRefused and names no text.
func TestScanRefusesTokenPatterns(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	tokens := []struct {
		name  string
		token string
	}{
		{"a Claude API key", "sk-ant-" + strings.Repeat("abcDEF123_-", 3)},
		{"a Claude OAuth token", "sk-ant-oat01-" + strings.Repeat("A1b2C3d4E5", 3)},
		{"a PEM private key", "-----BEGIN RSA PRIVATE KEY-----"},
		{"an OpenSSH private key", "-----BEGIN OPENSSH PRIVATE KEY-----"},
		{"an EC private key", "-----BEGIN EC PRIVATE KEY-----"},
		{"a PKCS#8 private key", "-----BEGIN PRIVATE KEY-----"},
		{"an encrypted PKCS#8 private key", "-----BEGIN ENCRYPTED PRIVATE KEY-----"},
		{"a GitHub token of the ghp kind", "ghp_" + strings.Repeat("a1B2c3D4", 6)},
		{"a GitHub token of the gho kind", "gho_" + strings.Repeat("a1B2c3D4", 6)},
		{"a GitHub token of the ghu kind", "ghu_" + strings.Repeat("a1B2c3D4", 6)},
		{"a GitHub token of the ghs kind", "ghs_" + strings.Repeat("a1B2c3D4", 6)},
		{"a GitHub token of the ghr kind", "ghr_" + strings.Repeat("a1B2c3D4", 6)},
		{"a GitHub fine-grained token", "github_pat_" + strings.Repeat("a1B2C3D4E5", 3)},
		{"an age secret key", "AGE-SECRET-KEY-1" + strings.Repeat("Q", 58)},
		{"an age secret key in lower case", "age-secret-key-1" + strings.Repeat("q", 58)},
	}

	for _, tc := range tokens {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			text := "the history holds " + tc.token + " at line 3"

			// The token is not a run the encoded-run refusal would refuse on
			// its own, so it is the pattern that stops it.
			if run := longestEncodedRun(tc.token); len(run) >= publish.EncodedRunMinLength && entropy(run) >= publish.EncodedRunMinEntropy {
				t.Fatalf("the fixture's token holds the run %q, which the encoded-run check refuses on its own, so this row cannot pin the pattern", run)
			}

			wantRefused(t, publish.ScanAdvisory(text, key), tc.token)

			// The same pattern is refused in public text, before the
			// encoded-run check could be what refused it: this token's longest
			// run is under the run threshold.
			wantRefused(t, publish.ScanPublic(text, nil, key), tc.token)
		})
	}
}

// encodedForm is one way a token reaches a text: in the clear, or encoded once
// or twice, in either alphabet, padded and raw, at odd lengths too.
type encodedForm struct {
	name string
	text string
	// binary is set when the text is not the token's own characters, so the
	// error may not be expected to avoid them.
	binary bool
}

// TestScanRefusesEncodedTokens pins the encoded-token check: base64 and
// base64url, padded and raw, hex, the trailing character of a run one over
// four, and a token encoded twice, all of them refused.
func TestScanRefusesEncodedTokens(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	tokens := []struct {
		name  string
		token string
	}{
		{"a PEM private key", "-----BEGIN RSA PRIVATE KEY-----"},
		{"a Claude API key", "sk-ant-" + strings.Repeat("abcDEF123_-", 3)},
		{"a GitHub token", "ghp_" + strings.Repeat("a1B2c3D4", 6)},
	}

	for _, token := range tokens {
		raw := []byte(token.token)
		padded := base64.StdEncoding.EncodeToString(raw)
		hexed := hex.EncodeToString(raw)

		b64OfB64 := base64.StdEncoding.EncodeToString([]byte(padded))
		hexOfB64 := hex.EncodeToString([]byte(padded))
		b64OfHex := base64.StdEncoding.EncodeToString([]byte(hexed))

		forms := []encodedForm{
			{name: "in the clear", text: token.token},
			{name: "base64, padded", text: padded},
			{name: "base64, raw", text: base64.RawStdEncoding.EncodeToString(raw)},
			{name: "base64url, padded", text: base64.URLEncoding.EncodeToString(raw), binary: true},
			{name: "base64url, raw", text: base64.RawURLEncoding.EncodeToString(raw), binary: true},
			{name: "base64 one character over four", text: padded + "A"},
			{name: "hex", text: hexed},
			{name: "hex starting one character late", text: "a" + hexed},
			{name: "base64 twice", text: b64OfB64},
			{name: "hex of the base64", text: hexOfB64},
			{name: "base64 of the hex", text: b64OfHex},
		}

		for _, form := range forms {
			t.Run(token.name+"/"+form.name, func(t *testing.T) {
				t.Parallel()

				if form.name != "in the clear" && len(form.text) < 16 {
					t.Fatalf("the fixture's encoded run is %d characters, too short to be read", len(form.text))
				}

				found := form.text
				if form.binary {
					found = token.token
				}

				wantRefused(t, publish.ScanAdvisory("see "+form.text+" in the trace", key), found)
			})
		}

		t.Run(token.name+"/a run under 16 characters is not decoded", func(t *testing.T) {
			t.Parallel()

			// Fifteen characters of the encoding, cut where a decoder cannot
			// reach the token: the run is not one the check reads.
			wantAllowed(t, publish.ScanAdvisory("see "+padded[:15]+" in the trace", key))
			wantAllowed(t, publish.ScanAdvisory("see "+hexed[:15]+" in the trace", key))
		})
	}
}

// TestScanRefusesTheDERKeyStart pins the DER private key's start, which only
// decoded bytes can hold: the three shapes of its length field, and a payload
// whose base64 run holds / and +, so both alphabets are read.
func TestScanRefusesTheDERKeyStart(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	// filler pads a payload to a base64 run of 24 characters, and its first
	// two bytes make the standard encoding hold / and +.
	filler := func(der []byte) []byte {
		payload := append([]byte{0xfb, 0xff}, der...)
		for len(payload) < 18 {
			payload = append(payload, 0x41)
		}

		return payload
	}

	positives := []struct {
		name string
		der  []byte
	}{
		{"a two-byte length", []byte{0x30, 0x82, 0x01, 0x02, 0x02, 0x01, 0x00}},
		{"a one-byte length", []byte{0x30, 0x81, 0x02, 0x02, 0x01, 0x01}},
		{"a short length", []byte{0x30, 0x7f, 0x02, 0x01, 0x01}},
		{"an EC private key", []byte{0x30, 0x81, 0x87, 0x02, 0x01, 0x00}},
	}

	for _, tc := range positives {
		payload := filler(tc.der)

		std := base64.StdEncoding.EncodeToString(payload)
		url := base64.URLEncoding.EncodeToString(payload)

		if !strings.ContainsAny(std, "+/") {
			t.Fatalf("the fixture's standard base64 holds no + or /, so it cannot pin the two alphabets: %s", std)
		}

		for _, form := range []struct{ name, text string }{
			{"base64", std},
			{"base64url", url},
			{"base64, raw", base64.RawStdEncoding.EncodeToString(payload)},
			{"base64url, raw", base64.RawURLEncoding.EncodeToString(payload)},
			{"hex", hex.EncodeToString(payload)},
			{"hex, one character over", "a" + hex.EncodeToString(payload)},
			{"base64 twice", base64.StdEncoding.EncodeToString([]byte(std))},
		} {
			t.Run(tc.name+"/"+form.name, func(t *testing.T) {
				t.Parallel()

				wantRefused(t, publish.ScanAdvisory("the trace holds "+form.text, key), form.text)
			})
		}
	}

	negatives := []struct {
		name string
		der  []byte
	}{
		{"another tag", []byte{0x31, 0x82, 0x01, 0x02, 0x02, 0x01, 0x01}},
		{"no version", []byte{0x30, 0x82, 0x01, 0x02, 0x02, 0x02, 0x01}},
		{"no integer", []byte{0x30, 0x7f, 0x02, 0x02, 0x01}},
		{"a version of 2", []byte{0x30, 0x81, 0x02, 0x02, 0x01, 0x02}},
	}

	for _, tc := range negatives {
		payload := filler(tc.der)

		for _, form := range []struct{ name, text string }{
			{"base64", base64.StdEncoding.EncodeToString(payload)},
			{"hex", hex.EncodeToString(payload)},
		} {
			t.Run(tc.name+"/"+form.name, func(t *testing.T) {
				t.Parallel()

				wantAllowed(t, publish.ScanAdvisory("the trace holds "+form.text, key))
			})
		}
	}
}

// TestScanRefusesTheFingerprintKeysBytes pins that the key the scan is given is
// what it looks for, in the clear and encoded, and that another key's bytes are
// no hit.
func TestScanRefusesTheFingerprintKeysBytes(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	other := otherKey(t)

	encoded := base64.StdEncoding.EncodeToString([]byte(testKeyBytes))

	refused := []struct {
		name string
		text string
	}{
		{"in advisory text", "the key is " + testKeyBytes + " here"},
		{"in public text", "the key is " + testKeyBytes + " here"},
		{"base64 encoded", "the key is " + encoded + " here"},
		{"hex encoded", "the key is " + hex.EncodeToString([]byte(testKeyBytes)) + " here"},
	}

	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wantRefused(t, publish.ScanAdvisory(tc.text, key), testKeyBytes)
			wantRefused(t, publish.ScanPublic(tc.text, nil, key), testKeyBytes)
		})
	}

	t.Run("another key's bytes are no hit", func(t *testing.T) {
		t.Parallel()

		text := "the key is " + testKeyBytes + " here"

		wantAllowed(t, publish.ScanAdvisory(text, other))
		wantAllowed(t, publish.ScanPublic(text, nil, other))
	})
}

// TestScanPublicRefusesEncodedRuns pins the encoded-run refusal: a run of at
// least EncodedRunMinLength model-written bytes whose entropy reaches
// EncodedRunMinEntropy, and nothing shorter or flatter, over the report's model
// fields only.
func TestScanPublicRefusesEncodedRuns(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	tests := []struct {
		name string
		// run is the run the case is about, when the field holds one.
		run string
		// field is the model-written fragment the renderer handed the scanner.
		field string
		// wantRefused is what the scan must do.
		wantRefused bool
	}{
		{
			name:        "a run of 23 characters is under L",
			run:         "abcdefghijklmnopqrstuvw",
			field:       "abcdefghijklmnopqrstuvw",
			wantRefused: false,
		},
		{
			name:        "a run of 24 characters reaches L",
			run:         "abcdefghijklmnopqrstuvwx",
			field:       "abcdefghijklmnopqrstuvwx",
			wantRefused: true,
		},
		{
			name:        "a run of 24 with 18 symbols is just under N",
			run:         "0123456789abcdefgh012345",
			field:       "0123456789abcdefgh012345",
			wantRefused: false,
		},
		{
			name:        "a run of 25 with 19 symbols reaches N",
			run:         "0123456789abcdefghi012345",
			field:       "0123456789abcdefghi012345",
			wantRefused: true,
		},
		{
			name:        "a long camel-case identifier trips it, the documented cost",
			run:         "TestSomethingWithAVeryLongCamelCaseName",
			field:       "TestSomethingWithAVeryLongCamelCaseName",
			wantRefused: true,
		},
		{
			name:        "a commit SHA never trips it",
			run:         testCommit,
			field:       testCommit,
			wantRefused: false,
		},
		{
			name:        "an image digest never trips it",
			run:         testDigest,
			field:       testDigest,
			wantRefused: false,
		},
		{
			name:        "a hex run of 24 never trips it",
			run:         "0123456789abcdef01234567",
			field:       "0123456789abcdef01234567",
			wantRefused: false,
		},
		{
			name:        "a path is many short runs",
			field:       "docs/security/audits/2026-11-03-18422000001.md",
			wantRefused: false,
		},
		{
			name:        "a crash input is a run",
			run:         strings.Repeat("A1b2C3d4E5f6G7h8I9j0", 2),
			field:       strings.Repeat("A1b2C3d4E5f6G7h8I9j0", 2),
			wantRefused: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.run != "" {
				// The fixture is what the rule says it is: a short run, a flat
				// run and a long random one are the sides of L and N.
				got := len(tc.run) >= publish.EncodedRunMinLength && entropy(tc.run) >= publish.EncodedRunMinEntropy
				if got != tc.wantRefused {
					t.Fatalf("the fixture's run is %d bytes with %.3f bits per character, which the rule refuses as %t, want %t: choose another run",
						len(tc.run), entropy(tc.run), got, tc.wantRefused)
				}
			}

			err := publish.ScanPublic("the report holds a fragment", []string{tc.field}, key)

			if tc.wantRefused {
				wantRefused(t, err, tc.field)

				return
			}

			wantAllowed(t, err)
		})
	}

	t.Run("a run in the report itself is not a model field", func(t *testing.T) {
		t.Parallel()

		body := "the report holds " + strings.Repeat("A1b2C3d4E5f6G7h8I9j0", 2)

		wantAllowed(t, publish.ScanPublic(body, nil, key))
	})

	t.Run("no model field at all", func(t *testing.T) {
		t.Parallel()

		wantAllowed(t, publish.ScanPublic("the report", nil, key))
	})
}

// TestScanAdvisoryPublishesCrashInputs pins the other half of the refusal: an
// advisory holds PoCs and crash inputs, which are often base64 and high in
// entropy, so an advisory text gets the patterns and the encoded-token check
// and never the encoded-run refusal.
func TestScanAdvisoryPublishesCrashInputs(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	crashInput := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("A1b2C3d4E5f6G7h8I9j0", 10)))

	if len(crashInput) < 48 || entropy(crashInput) < 5 {
		t.Fatalf("the fixture's crash input is %d bytes with %.3f bits per character, want a long, high-entropy run",
			len(crashInput), entropy(crashInput))
	}

	advisory := "a crash input the fuzzer found:\n\n" + crashInput + "\n"

	// The advisory is published, and the same run in public text is refused:
	// the refusal is what an advisory escapes, not the check for tokens.
	wantAllowed(t, publish.ScanAdvisory(advisory, key))
	wantRefused(t, publish.ScanPublic("the report\n\n"+crashInput, []string{crashInput}, key), crashInput)
}

// TestScanPublishesLongPathsAndDigests pins the PRD's named case for public
// text: a long repository path and an image digest are published, since neither
// holds a run the refusal reads.
func TestScanPublishesLongPathsAndDigests(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	path := "docs/security/audits/2026-11-03-18422000001.md"
	fields := []string{path, testDigest, "internal/adapter/target/ovhcloud/snapshot_2026_11.json", testCommit}

	body := "the report's path is " + path + " and its image is " + testDigest

	wantAllowed(t, publish.ScanPublic(body, fields, key))

	// The scan is awake: a token in a model field is refused by the same call,
	// so a scan that allowed everything could not pass both.
	token := base64.StdEncoding.EncodeToString([]byte("sk-ant-" + strings.Repeat("abcDEF123_-", 3)))
	wantRefused(t, publish.ScanPublic(body, []string{token}, key), token)
}

// TestScanErrorNamesNoText pins that no refusal ever hands back what it found:
// the error is what the entry point may log, and the run's summary keeps no
// finding's text.
func TestScanErrorNamesNoText(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	token := "sk-ant-" + strings.Repeat("abcDEF123_-", 3)

	wantRefused(t, publish.ScanAdvisory("found "+token, key), token)
}

// longestEncodedRun returns the longest run of bytes the encoded-run refusal
// reads: a maximal sequence of [A-Za-z0-9+=_-], as ADR 0040 defines a run.
func longestEncodedRun(s string) string {
	longest, current := "", strings.Builder{}

	flush := func() {
		if current.Len() > len(longest) {
			longest = current.String()
		}

		current.Reset()
	}

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '+' || c == '=' || c == '_' || c == '-' {
			current.WriteByte(c)

			continue
		}

		flush()
	}

	flush()

	return longest
}
