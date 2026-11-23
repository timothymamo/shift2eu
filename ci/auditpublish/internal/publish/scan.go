package publish

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// The encoded-run refusal's thresholds (ADR 0040, "The secret and
// encoded-token scan").
const (
	// EncodedRunMinLength is L: the padded standard base64 length of a
	// 128-bit secret, longer than a fingerprint's 15 characters. Raw or URL
	// base64 of 16 bytes is 22 characters and stays under it.
	EncodedRunMinLength = 24

	// EncodedRunMinEntropy is N, in bits per character: above hex's 4.0, so
	// a commit SHA or a digest never trips it.
	EncodedRunMinEntropy = 4.1
)

// minDecodedRun is the shortest run of base64 or hex characters the
// encoded-token check decodes.
const minDecodedRun = 16

// tokenPatterns are the token and key patterns, matched over a whole text and
// over every decoded run.
var tokenPatterns = []*regexp.Regexp{
	// Claude OAuth tokens and API keys.
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`),
	// The App's PEM key, and any other.
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
	// GitHub tokens, wherever they start: no \b, so a letter before the
	// prefix hides nothing.
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),
	// The model and publish keys' private halves.
	regexp.MustCompile(`(?i)AGE-SECRET-KEY-1[0-9A-Z]{58}`),
}

// ScanPublic scans a public text, the report or a pull request's or an
// issue's text: the token and key patterns and the encoded-token check over
// body and over each of modelFields, the escaped fragments of model text the
// renderer built it from, and the encoded-run refusal over modelFields only.
// A hit is an error wrapping ErrScanRefused that names no text.
func ScanPublic(body string, modelFields []string, key FingerprintKey) error {
	if err := ScanAdvisory(body, key); err != nil {
		return err
	}

	for _, field := range modelFields {
		if err := ScanAdvisory(field, key); err != nil {
			return err
		}

		if encodedRun(field) {
			return fmt.Errorf("public text: a model field holds an encoded run: %w", ErrScanRefused)
		}
	}

	return nil
}

// ScanAdvisory scans an advisory's text: the token and key patterns and the
// encoded-token check, not the encoded-run refusal, since an advisory holds
// PoCs and crash inputs, which are often base64. A hit is an error wrapping
// ErrScanRefused that names no text; a key that was never set or is shorter
// than 32 bytes is ErrConfig.
func ScanAdvisory(text string, key FingerprintKey) error {
	if len(key.bytes) < minKeyBytes {
		return fmt.Errorf("AUDIT_FINGERPRINT_KEY: not set or shorter than %d bytes: %w", minKeyBytes, ErrConfig)
	}

	if holdsToken([]byte(text), key) {
		return fmt.Errorf("a token, a key or an encoded one: %w", ErrScanRefused)
	}

	return nil
}

// holdsToken matches the patterns over the text, then over every run decoded
// once, then over every run of those decoded once more: a token encoded twice.
func holdsToken(text []byte, key FingerprintKey) bool {
	if matches(text, key, false) {
		return true
	}

	for _, once := range decodeRuns(text) {
		if matches(once, key, true) {
			return true
		}

		for _, twice := range decodeRuns(once) {
			if matches(twice, key, true) {
				return true
			}
		}
	}

	return false
}

// matches reports whether b holds a token pattern or the key's bytes, and, in
// decoded bytes, a DER private key's start.
func matches(b []byte, key FingerprintKey, decoded bool) bool {
	for _, pattern := range tokenPatterns {
		if pattern.Match(b) {
			return true
		}
	}

	if bytes.Contains(b, key.bytes) {
		return true
	}

	return decoded && holdsDERKeyStart(b)
}

// holdsDERKeyStart reports whether b holds a DER private key's start, a
// SEQUENCE and then the INTEGER version 0 or 1:
// \x30(\x82..|\x81.|[\x00-\x7f])\x02\x01[\x00\x01]. It reads bytes, not
// runes, so it is no regular expression.
func holdsDERKeyStart(b []byte) bool {
	for i := range b {
		if b[i] != 0x30 || i+1 >= len(b) {
			continue
		}

		var length int

		switch {
		case b[i+1] == 0x82:
			length = 3
		case b[i+1] == 0x81:
			length = 2
		case b[i+1] <= 0x7f:
			length = 1
		default:
			continue
		}

		if i+1+length+3 > len(b) {
			continue
		}

		if rest := b[i+1+length:]; rest[0] == 0x02 && rest[1] == 0x01 && rest[2] <= 0x01 {
			return true
		}
	}

	return false
}

// decodeRuns returns the bytes of every run of base64, base64url or hex
// characters of at least minDecodedRun, decoded at every alignment.
func decodeRuns(b []byte) [][]byte {
	var out [][]byte

	for _, run := range runs(b, isBase64Byte) {
		out = append(out, decodeBase64Run(run)...)
	}

	for _, run := range runs(b, isBase64URLByte) {
		out = append(out, decodeBase64Run(run)...)
	}

	for _, run := range runs(b, isHexByte) {
		for offset := 0; offset < 2; offset++ {
			s := run[offset:]
			s = s[:len(s)-len(s)%2]

			dst := make([]byte, hex.DecodedLen(len(s)))
			if n, err := hex.Decode(dst, s); err == nil {
				out = append(out, dst[:n])
			}
		}
	}

	return out
}

// decodeBase64Run decodes one run of a base64 alphabet: each piece the run's
// padding ends, padded or raw, with one trailing character dropped when the
// length leaves 1 over 4, at each of the four alignments, so a token is found
// wherever its encoding starts in the run.
func decodeBase64Run(run []byte) [][]byte {
	var out [][]byte

	for _, piece := range pieces(run) {
		trimmed := bytes.TrimRight(piece, "=")

		enc := base64.RawStdEncoding
		if bytes.ContainsAny(trimmed, "-_") {
			enc = base64.RawURLEncoding
		}

		for offset := 0; offset < 4 && offset < len(trimmed); offset++ {
			s := trimmed[offset:]
			if len(s)%4 == 1 {
				s = s[:len(s)-1]
			}

			dst := make([]byte, enc.DecodedLen(len(s)))

			// A run that is no base64 at all still yields the bytes before
			// the first byte it cannot decode.
			n, _ := enc.Decode(dst, s)
			if n > 0 {
				out = append(out, dst[:n])
			}
		}
	}

	return out
}

// pieces splits a base64 run after each group of padding, so two encodings
// written one after the other, or one followed by a stray character, are each
// decoded.
func pieces(run []byte) [][]byte {
	var out [][]byte

	start := 0

	for i := 0; i < len(run); i++ {
		if run[i] == '=' && (i+1 == len(run) || run[i+1] != '=') {
			out = append(out, run[start:i+1])
			start = i + 1
		}
	}

	if start < len(run) {
		out = append(out, run[start:])
	}

	return out
}

// runs returns the maximal runs of b's bytes that in accepts, of at least
// minDecodedRun bytes.
func runs(b []byte, in func(byte) bool) [][]byte {
	var out [][]byte

	start := -1

	for i := 0; i <= len(b); i++ {
		if i < len(b) && in(b[i]) {
			if start < 0 {
				start = i
			}

			continue
		}

		if start >= 0 && i-start >= minDecodedRun {
			out = append(out, b[start:i])
		}

		start = -1
	}

	return out
}

func isAlphanumeric(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func isBase64Byte(c byte) bool {
	return isAlphanumeric(c) || c == '+' || c == '/' || c == '='
}

func isBase64URLByte(c byte) bool {
	return isAlphanumeric(c) || c == '-' || c == '_' || c == '='
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// isPlainRunByte is a byte of the plain run class, [A-Za-z0-9+=_-]: every
// other byte, / . \ and | included, ends a run, so a path is many short runs.
func isPlainRunByte(c byte) bool {
	return isAlphanumeric(c) || c == '+' || c == '=' || c == '_' || c == '-'
}

// isSlashRunByte is a byte of the slash run class, [A-Za-z0-9+/=_-]: the plain
// class and /, so standard base64 is one run.
func isSlashRunByte(c byte) bool {
	return isPlainRunByte(c) || c == '/'
}

// trailingPunctuation is what a sentence or a code span puts after a word;
// encodedRun trims it, backticks first, before it looks for a dot.
const trailingPunctuation = ".,;:!?)]}'\""

// encodedRun reports whether a model field holds an encoded run (ADR 0040,
// "The encoded-run refusal"):
//
//   - the plain class: a run of [A-Za-z0-9+=_-] of at least
//     EncodedRunMinLength bytes and EncodedRunMinEntropy bits per character;
//   - the slash class: the same measure of a run of [A-Za-z0-9+/=_-] inside a
//     word, a maximal run of non-whitespace with its trailing punctuation
//     trimmed, that holds no dot, so a path, which holds one, is measured by
//     the plain class alone;
//   - the hex rule: a maximal hex run of at least EncodedRunMinLength bytes
//     that is neither 40 nor 64 bytes long, or that is and decodes to nothing
//     but printable ASCII.
func encodedRun(field string) bool {
	if highEntropyRun(field, isPlainRunByte) || suspectHexRun(field) {
		return true
	}

	for _, word := range strings.Fields(field) {
		word = trimTrailing(word)

		if !strings.Contains(word, ".") && highEntropyRun(word, isSlashRunByte) {
			return true
		}
	}

	return false
}

// trimTrailing trims a word's trailing backticks and then its trailing
// punctuation, until neither is left.
func trimTrailing(word string) string {
	for {
		trimmed := strings.TrimRight(strings.TrimRight(word, "`"), trailingPunctuation)
		if trimmed == word {
			return word
		}

		word = trimmed
	}
}

// highEntropyRun reports whether s holds a maximal run of bytes in accepts of
// at least EncodedRunMinLength bytes whose Shannon entropy over its own byte
// frequencies is at least EncodedRunMinEntropy bits per character.
func highEntropyRun(s string, in func(byte) bool) bool {
	for _, run := range runsOf(s, in, EncodedRunMinLength) {
		if entropy(run) >= EncodedRunMinEntropy {
			return true
		}
	}

	return false
}

// suspectHexRun reports whether s holds a maximal hex run of at least
// EncodedRunMinLength bytes that is no 40-byte SHA and no 64-byte digest, or
// that is one but decodes to nothing but printable ASCII, 0x20 to 0x7e: a
// real SHA or digest essentially never does, a token body written as hex does.
func suspectHexRun(s string) bool {
	for _, run := range runsOf(s, isHexByte, EncodedRunMinLength) {
		if len(run) != 40 && len(run) != 64 {
			return true
		}

		decoded, err := hex.DecodeString(run)
		if err == nil && printableASCII(decoded) {
			return true
		}
	}

	return false
}

// printableASCII reports whether every byte of b is 0x20 to 0x7e.
func printableASCII(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}

	return true
}

// runsOf returns the maximal runs of s's bytes that in accepts, of at least
// minLength bytes.
func runsOf(s string, in func(byte) bool, minLength int) []string {
	var out []string

	start := -1

	for i := 0; i <= len(s); i++ {
		if i < len(s) && in(s[i]) {
			if start < 0 {
				start = i
			}

			continue
		}

		if start >= 0 && i-start >= minLength {
			out = append(out, s[start:i])
		}

		start = -1
	}

	return out
}

// entropy is the Shannon entropy of s over its own byte frequencies, in bits
// per character, summed in byte order so one text always gives one value.
func entropy(s string) float64 {
	var counts [256]int

	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}

	total := float64(len(s))
	h := 0.0

	for _, c := range counts {
		if c == 0 {
			continue
		}

		p := float64(c) / total
		h -= p * math.Log2(p)
	}

	return h
}
