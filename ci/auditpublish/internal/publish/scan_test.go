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
		{"a Claude OAuth token", "sk-ant-oat01-" + strings.Repeat("z9Y8x7W6", 4)},
		{"a PEM private key", "-----BEGIN RSA PRIVATE KEY-----"},
		{"an OpenSSH private key", "-----BEGIN OPENSSH PRIVATE KEY-----"},
		{"an EC private key", "-----BEGIN EC PRIVATE KEY-----"},
		{"a PKCS#8 private key", "-----BEGIN PRIVATE KEY-----"},
		{"an encrypted PKCS#8 private key", "-----BEGIN ENCRYPTED PRIVATE KEY-----"},
		{"a GitHub token of the ghp kind", "ghp_" + strings.Repeat("z9Y8x7W6", 6)},
		{"a GitHub token of the gho kind", "gho_" + strings.Repeat("z9Y8x7W6", 6)},
		{"a GitHub token of the ghu kind", "ghu_" + strings.Repeat("z9Y8x7W6", 6)},
		{"a GitHub token of the ghs kind", "ghs_" + strings.Repeat("z9Y8x7W6", 6)},
		{"a GitHub token of the ghr kind", "ghr_" + strings.Repeat("z9Y8x7W6", 6)},
		// A letter before the prefix: GFM's word boundary is no part of the
		// pattern, as gitleaks' rule has none.
		{"a GitHub token after a letter", "Xghp_" + strings.Repeat("z9Y8x7W6", 6)},
		{"a GitHub fine-grained token", "github_pat_" + strings.Repeat("z9Y8x7W6", 3)},
		{"an age secret key", "AGE-SECRET-KEY-1" + strings.Repeat("Q", 58)},
		{"an age secret key in lower case", "age-secret-key-1" + strings.Repeat("q", 58)},
	}

	for _, tc := range tokens {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			text := "the history holds " + tc.token + " at line 3"

			// The token is no run the refusal reads on its own, so it is the
			// pattern that stops it.
			if reason := encodedRunRefusal(tc.token); reason != "" {
				t.Fatalf("the fixture's token is refused by the encoded-run rules (%s), so this row cannot pin the pattern", reason)
			}

			wantRefused(t, publish.ScanAdvisory(text, key), tc.token)

			// The same pattern is refused in public text, before the
			// encoded-run rules could be what refused it.
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
			// Only the two base64url forms set binary: scan.go reads a run's bytes
			// through the standard alphabet (isBase64Byte) and through the URL one
			// (isBase64URLByte), and a text holding - or _ breaks the standard run
			// where the URL run holds it whole (decodeBase64Run switches encoding
			// on those two bytes). A base64url text is therefore not the token's
			// own characters whatever it holds, and what the scan matched in it is
			// the token's bytes: the assertion that the error names nothing is
			// made on the token. None of these fixtures' encodings holds + or /,
			// so their URL form and their standard form are the same bytes.
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
// what it looks for, in the clear and encoded, in both the texts a run sends
// (an advisory's and a public one's) and in a public text's model fields, and
// that another key's bytes are no hit.
func TestScanRefusesTheFingerprintKeysBytes(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	other := otherKey(t)

	encoded := base64.StdEncoding.EncodeToString([]byte(testKeyBytes))

	appearances := []struct {
		name string
		text string
	}{
		{"in the clear", testKeyBytes},
		{"base64 encoded", encoded},
		{"hex encoded", hex.EncodeToString([]byte(testKeyBytes))},
	}

	for _, tc := range appearances {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Both scans read the text's own bytes: the advisory's, which an
			// advisory's description is, and the public one's, which the
			// report's or an issue's is.
			text := "the key is " + tc.text + " here"

			wantRefused(t, publish.ScanAdvisory(text, key), testKeyBytes)
			wantRefused(t, publish.ScanPublic(text, nil, key), testKeyBytes)
		})
	}

	t.Run("in a public text's model field", func(t *testing.T) {
		t.Parallel()

		// A public text is scanned twice: as a text, and as the escaped model
		// fragments the renderer built it from. The model field holds the
		// key's bytes in a code span, and ScanAdvisory never sees them there.
		field := publish.CodeSpan("the key is " + testKeyBytes + " here")

		wantAllowed(t, publish.ScanAdvisory("the report's body", key))
		wantRefused(t, publish.ScanPublic("the report's body", []string{field}, key), testKeyBytes)
	})

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
			name:        "a path's whole run stays under N",
			run:         "docs/security/audits/2026-11-03-18422000001",
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

// TestScanPublicRefusesHexAndSlashRuns pins the run classes the refusal adds to
// the one without /: a hex run of at least EncodedRunMinLength that is no 40- or
// 64-character SHA or digest, a 40- or 64-character hex run whose decoded bytes
// are all printable ASCII (a token body written as a digest), and the class that
// holds a /, which the class without one splits at every /.
//
// The class that holds / is measured inside a word that holds no dot, a word
// being a maximal run of non-whitespace and its trailing punctuation trimmed, so
// a secret ending a sentence is still measured. A word that holds a dot
// elsewhere is measured by the class without / alone: that is what keeps a path
// such as .github/workflows/claude-code-pin.yml publishable, and it is also the
// residual risk, since a dot inside a base64 secret drops it to that class.
func TestScanPublicRefusesHexAndSlashRuns(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	// The token's own body without its prefix: what a steered model writes once
	// it knows the patterns read the prefix.
	body := strings.Repeat("a1B2c3D4", 6)

	// A standard base64 encoding of 32 invented bytes, one of its characters a /
	// near the middle: the class that holds / measures one long run and refuses
	// it, while the class without / measures the 23 and 20 characters on either
	// side and publishes it.
	const secret = "QM49sZjjIjz7lvMJPz26iZW/gKrV8xFqygiyUyrzIN8="

	// A second standard base64 encoding of 32 invented bytes, its / near the
	// start so that the class without / is left a long, high-entropy piece: a dot
	// inside this one does not save it.
	const longPieceSecret = "kyOt5uW/3GisfUmsuNGP0NNivM1R61BYqJ0tIwqgHJM="

	tests := []struct {
		name  string
		field string
		// dotless is the field with the dot this case inserts removed, so the
		// guard can show that the dot, and not something else, changes the
		// verdict.
		dotless     string
		wantRefused bool
	}{
		{
			name:        "the prefix-less token body, hex-encoded",
			field:       hex.EncodeToString([]byte(body)),
			wantRefused: true,
		},
		{
			name:        "a 64-character chunk of the token body, hex-encoded",
			field:       hex.EncodeToString([]byte(body[:32])),
			wantRefused: true,
		},
		{
			name:        "the same chunk in upper case",
			field:       strings.ToUpper(hex.EncodeToString([]byte(body[:32]))),
			wantRefused: true,
		},
		{
			name:        "a 40-character run that decodes to text",
			field:       hex.EncodeToString([]byte(strings.Repeat("A", 20))),
			wantRefused: true,
		},
		{
			name:        "a hex run one character short of a SHA",
			field:       strings.Repeat("ab", 19) + "a",
			wantRefused: true,
		},
		{
			name:        "a 40-character SHA passes",
			field:       testCommit,
			wantRefused: false,
		},
		{
			name:        "a 64-character digest passes",
			field:       strings.Repeat("1", 64),
			wantRefused: false,
		},
		{
			name:        "standard base64 of 32 bytes that holds a /",
			field:       secret,
			wantRefused: true,
		},
		{
			name:        "the same secret followed by a sentence-ending dot",
			field:       secret + ".",
			wantRefused: true,
		},
		{
			// The dot is the whole difference: the same secret with one inside
			// its word is the class without /'s to judge, and that class passes it.
			name:        "the same secret with a dot inside the word, the residual risk",
			field:       "QM49sZjjIjz7lvMJPz26iZW/.gKrV8xFqygiyUyrzIN8=",
			dotless:     secret,
			wantRefused: false,
		},
		{
			name:        "a second secret with a dot inside the word, which the class without / still refuses",
			field:       "kyOt5u.W/3GisfUmsuNGP0NNivM1R61BYqJ0tIwqgHJM=",
			dotless:     longPieceSecret,
			wantRefused: true,
		},
		{
			// No dot and high entropy: the class that holds / reads the whole
			// word and refuses it, although the class without / would publish it.
			name:        "a dot-free path-like word is refused, the class's cost",
			field:       "abcDEF1234/ghiJKL5678/mnoPQR9012",
			wantRefused: true,
		},
	}

	t.Run("the secrets are standard base64 of 32 bytes that hold a /", func(t *testing.T) {
		t.Parallel()

		for _, fixture := range []string{secret, longPieceSecret} {
			raw, err := base64.StdEncoding.DecodeString(fixture)
			if err != nil || len(raw) != 32 {
				t.Fatalf("the fixture %q decodes to %d bytes with %v, want 32 bytes of standard base64", fixture, len(raw), err)
			}

			if !strings.Contains(fixture, "/") {
				t.Fatalf("the fixture %q holds no /, so it cannot pin the class that holds one", fixture)
			}
		}
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := encodedRunRefusal(tc.field) != ""; got != tc.wantRefused {
				t.Fatalf("the fixture %q is refused by the rule as %t, want %t: choose another", tc.field, got, tc.wantRefused)
			}

			if tc.dotless != "" && encodedRunRefusal(tc.dotless) == "" {
				t.Fatalf("the fixture %q, this case's field without its dot, is published, so the dot is no part of the verdict and the case pins nothing: choose another field", tc.dotless)
			}

			err := publish.ScanPublic("the report holds a fragment", []string{tc.field}, key)

			if tc.wantRefused {
				wantRefused(t, err, tc.field)

				return
			}

			wantAllowed(t, err)
		})
	}
}

// TestScanPublicPublishesTheRepositorysPaths pins the refusal against the text a
// report really carries: real paths of this repository, written here as literals
// and never read from the tree, are published. A path holds a dot, so the class
// without / measures it and its pieces are short; the class that holds / costs a
// run only for a long, high-entropy word that holds no dot at all, which
// TestScanPublicRefusesHexAndSlashRuns pins.
func TestScanPublicPublishesTheRepositorysPaths(t *testing.T) {
	t.Parallel()

	key := testKey(t)

	paths := []struct {
		name string
		path string
	}{
		{"the workflow file the workflows check reads", ".github/workflows/claude-code-pin.yml"},
		{"the workflow that checks an audit pull request's paths", ".github/workflows/audit-paths.yml"},
		{"a file of the publish core", "ci/auditpublish/internal/publish/result.go"},
		{"the publish core's test helpers", "ci/auditpublish/internal/publish/helpers_test.go"},
		{"a source probe's integration test", "test/architecture/internal/check/testdata/sourceprobe/a/source/integration/integration_test.go"},
		{"an adapter stub's target", "test/lint/testdata/adaptersbuiltincmd/mod/internal/adapter/target/ovhcloud/ovhcloud.go"},
		{"a lint fixture's read-only probe", "test/lint/testdata/cloudreadonly/mod/internal/adapter/source/aws/probe_integration_test.go"},
		// A dot-free path, which the class that holds / measures and publishes
		// too: its run is flat enough to stay under N.
		{"a target's testdata directory", "internal/adapter/target/scaleway/testdata"},
		{"a domain package", "internal/domain/taxonomy"},
	}

	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if reason := encodedRunRefusal(tc.path); reason != "" {
				t.Fatalf("the fixture %q is refused (%s), want a path whose runs stay under L or under N", tc.path, reason)
			}

			wantAllowed(t, publish.ScanPublic("a finding's location", []string{tc.path}, key))
		})
	}
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
	fields := []string{path, testDigest, "internal/adapter/target/scaleway/testdata", testCommit}

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

// encodedRunRefusal returns the rule of the encoded-run refusal that refuses
// field, or "" when no rule does: the entropy of a run of [A-Za-z0-9+/=_-] of
// at least EncodedRunMinLength bytes inside a word that holds no dot, the same
// measure of a run of [A-Za-z0-9+=_-] inside a word that holds one, and a hex
// run of at least that length that is neither 40 nor 64 bytes long, or that
// decodes to nothing but printable ASCII. The tests compute it from the field
// alone, so a fixture can show that it pins the rule it says it does.
func encodedRunRefusal(field string) string {
	for _, word := range strings.Fields(field) {
		measured := trimTrailingPunctuation(word)

		class := isSlashRunByte
		if strings.Contains(measured, ".") {
			class = isPlainRunByte
		}

		for _, run := range runsOf(measured, class) {
			if entropy(run) >= publish.EncodedRunMinEntropy {
				return "the run " + run + " reaches N"
			}
		}
	}

	for _, run := range runsOf(field, isHexByte) {
		switch {
		case len(run) != 40 && len(run) != 64:
			return "the hex run " + run + " is no SHA and no digest"
		case allPrintableHex(run):
			return "the hex run " + run + " decodes to text"
		}
	}

	return ""
}

// runsOf returns the maximal runs of field's bytes that in accepts, of at least
// EncodedRunMinLength bytes.
func runsOf(field string, in func(byte) bool) []string {
	var out []string

	start := -1

	for i := 0; i <= len(field); i++ {
		if i < len(field) && in(field[i]) {
			if start < 0 {
				start = i
			}

			continue
		}

		if start >= 0 && i-start >= publish.EncodedRunMinLength {
			out = append(out, field[start:i])
		}

		start = -1
	}

	return out
}

// isSlashRunByte is a byte of the run class that holds /: every other byte ends
// a run.
func isSlashRunByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
		c == '+' || c == '/' || c == '=' || c == '_' || c == '-'
}

// isPlainRunByte is a byte of the run class without /: the class a word that
// holds a dot is measured with, so a path is many short runs.
func isPlainRunByte(c byte) bool {
	return isSlashRunByte(c) && c != '/'
}

// trimTrailingPunctuation trims the backticks and then the punctuation a
// sentence or a code span puts after a word, so the word is measured as it was
// written and a base64 secret that ends a sentence is still measured.
func trimTrailingPunctuation(word string) string {
	for {
		trimmed := strings.TrimRight(strings.TrimRight(word, "`"), ".,;:!?)]}'\"")

		if trimmed == word {
			return trimmed
		}

		word = trimmed
	}
}

// isHexByte is a byte of a hex run.
func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// allPrintableHex reports whether every byte run decodes to is printable ASCII,
// 0x20 to 0x7e. An odd-length run is read as far as it decodes in pairs.
func allPrintableHex(run string) bool {
	dst := make([]byte, len(run)/2)

	n, err := hex.Decode(dst, []byte(run[:len(run)-len(run)%2]))
	if err != nil || n == 0 {
		return false
	}

	for _, c := range dst[:n] {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}

	return true
}
