package deploy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// TestSecretIsRedacted pins every printed form of a Secret and of the
// Credentials that hold two of them (doc.go, "The key"): whatever the verb, the
// flags or the encoder, the key's value never appears, so a Config, a
// Credentials or a Secret in an error, a log line or a JSON document prints
// "[redacted]".
func TestSecretIsRedacted(t *testing.T) {
	t.Parallel()

	secret := deploy.NewSecret(testSecretKey)
	credentials := deploy.Credentials{
		AccessKey: deploy.NewSecret(testAccessKey),
		SecretKey: secret,
	}

	// printed are the ways a value reaches a job's output: fmt's verbs, the
	// flags that print Go syntax and quoted strings, and the JSON encoder.
	printed := map[string]string{
		"%v":                     fmt.Sprintf("%v", secret),
		"%+v":                    fmt.Sprintf("%+v", secret),
		"%#v":                    fmt.Sprintf("%#v", secret),
		"%s":                     fmt.Sprintf("%s", secret),
		"%q":                     fmt.Sprintf("%q", secret),
		"%x":                     fmt.Sprintf("%x", secret),
		"%+v of the credentials": fmt.Sprintf("%+v", credentials),
		"%#v of the credentials": fmt.Sprintf("%#v", credentials),
		"a struct holding the key": fmt.Sprintf("%+v", struct {
			Region string
			Key    deploy.Secret
		}{Region: testRegion, Key: secret}),
	}

	for verb, text := range printed {
		if strings.Contains(text, testSecretKey) || strings.Contains(text, testAccessKey) {
			t.Errorf("%s prints %q, want neither half of the key", verb, text)
		}
	}

	for name, value := range map[string]any{
		"the secret":      secret,
		"the credentials": credentials,
		"a struct holding the key": struct {
			Key deploy.Secret `json:"key"`
		}{Key: secret},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("json.Marshal(%s): %v", name, err)
		}

		if text := string(data); strings.Contains(text, testSecretKey) || strings.Contains(text, testAccessKey) {
			t.Errorf("json.Marshal(%s) = %s, want neither half of the key", name, text)
		}
	}

	text, err := secret.MarshalText()
	if err != nil {
		t.Fatalf("Secret.MarshalText: %v", err)
	}

	if strings.Contains(string(text), testSecretKey) {
		t.Errorf("Secret.MarshalText = %q, want the key redacted", text)
	}

	if got, want := secret.String(), secret.GoString(); got != want {
		t.Errorf("Secret.String() = %q and GoString() = %q, want the same redacted text", got, want)
	}

	if diff := cmp.Diff(true, deploy.NewSecret("").IsZero()); diff != "" {
		t.Errorf("NewSecret(\"\").IsZero() (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(false, secret.IsZero()); diff != "" {
		t.Errorf("a Secret holding a key reports IsZero() (-want +got):\n%s", diff)
	}
}
