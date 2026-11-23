package publish

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
)

// Fingerprint is a finding's identity across reports: fp- and 12 lowercase hex
// characters, the first 6 bytes of a digest of its rule, file and sink.
type Fingerprint string

// fingerprintPrefix versions the digest's input, so a later change of the
// input starts a new series on purpose.
const fingerprintPrefix = "shift2eu-audit-fingerprint/v1"

// minKeyBytes is the runbook's minimum length of AUDIT_FINGERPRINT_KEY.
const minKeyBytes = 32

// fingerprintPattern is a fingerprint's shape.
var fingerprintPattern = regexp.MustCompile(`^fp-[0-9a-f]{12}$`)

// FingerprintKey is AUDIT_FINGERPRINT_KEY's value, the HMAC key of a private
// finding's fingerprint. It prints as a placeholder, never as its bytes.
type FingerprintKey struct {
	bytes []byte
}

// NewFingerprintKey returns the key over the secret's bytes, unchanged, and
// refuses fewer than 32 bytes (ErrConfig). The error names no byte of it.
func NewFingerprintKey(b []byte) (FingerprintKey, error) {
	if len(b) < minKeyBytes {
		return FingerprintKey{}, fmt.Errorf("AUDIT_FINGERPRINT_KEY: shorter than %s bytes: %w", strconv.Itoa(minKeyBytes), ErrConfig)
	}

	return FingerprintKey{bytes: append([]byte(nil), b...)}, nil
}

// String keeps the key's bytes out of every formatted text.
func (FingerprintKey) String() string { return "FingerprintKey(redacted)" }

// GoString keeps the key's bytes out of %#v.
func (FingerprintKey) GoString() string { return "FingerprintKey(redacted)" }

// PlainFingerprint returns the SHA-256 fingerprint of a public finding, and
// the one every false-positive match compares.
func PlainFingerprint(rule, file, sink string) Fingerprint {
	sum := sha256.Sum256(fingerprintInput(rule, file, sink))

	return toFingerprint(sum[:])
}

// KeyedFingerprint returns the HMAC-SHA-256 fingerprint of a private finding,
// which nobody without the key can test a guess against. A key shorter than
// 32 bytes, the zero FingerprintKey included, is ErrConfig: an HMAC under an
// empty key is a digest anyone can compute.
func KeyedFingerprint(key FingerprintKey, rule, file, sink string) (Fingerprint, error) {
	if len(key.bytes) < minKeyBytes {
		return "", fmt.Errorf("AUDIT_FINGERPRINT_KEY: not set or shorter than %s bytes: %w", strconv.Itoa(minKeyBytes), ErrConfig)
	}

	mac := hmac.New(sha256.New, key.bytes)
	mac.Write(fingerprintInput(rule, file, sink))

	return toFingerprint(mac.Sum(nil)), nil
}

// fingerprintInput is the digest's input: the versioned prefix, then rule,
// file and sink, each after a NUL, which none of them may hold.
func fingerprintInput(rule, file, sink string) []byte {
	return []byte(fingerprintPrefix + "\x00" + rule + "\x00" + file + "\x00" + sink)
}

func toFingerprint(digest []byte) Fingerprint {
	return Fingerprint("fp-" + hex.EncodeToString(digest[:6]))
}
