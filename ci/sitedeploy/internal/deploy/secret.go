package deploy

import "fmt"

// redacted is what every printed form of a Secret shows.
const redacted = "[redacted]"

// Secret holds one half of the deploy key. Its value is unexported and every
// way fmt, encoding/json or encoding/text print it shows "[redacted]", so a
// Secret in a struct, an error or a log line never prints the key.
type Secret struct {
	value string
}

// NewSecret wraps a key's value.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool {
	return s.value == ""
}

// String returns "[redacted]".
func (s Secret) String() string {
	return redacted
}

// GoString returns "[redacted]", for %#v.
func (s Secret) GoString() string {
	return redacted
}

// Format prints "[redacted]" for every verb and flag, %x and %q included.
func (s Secret) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(redacted))
}

// MarshalText returns "[redacted]".
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// MarshalJSON returns the JSON string "[redacted]".
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}

// Credentials is the deploy key: the IAM application's access key and secret
// key (SCW_ACCESS_KEY and SCW_SECRET_KEY, read by ci/sitedeploy).
type Credentials struct {
	AccessKey Secret
	SecretKey Secret
}
