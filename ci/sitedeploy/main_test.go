// White-box tests of the deploy program's entry point: ci/sitedeploy is a main
// package, so its tests are package main, the only way a test file reaches the
// package at all (Go refuses an import of a main package).
//
// environment takes the lookup function as a parameter, so a test passes a map
// of its own and never reads the process's environment (golangci-lint's
// site-deploy-env group refuses os.LookupEnv outside main.go). The keys are
// invented (CLAUDE.md rule 2).
package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// The invented keys the environment tests set, and the variables the entry point
// reads.
const (
	accessKeyValue = "SCWEXAMPLEACCESSKEY"
	secretKeyValue = "exampleSecretKey0123456789abcdefghijklmn"
	summaryValue   = "/tmp/the-step-summary.md"
)

// TestEnvironmentReadsTheKeyAndTheSummary pins what the entry point takes from
// the step's environment: both halves of the deploy key and the job summary's
// path, and nothing else. A missing or empty key is an error that names the
// variable and never a value.
func TestEnvironmentReadsTheKeyAndTheSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		// wantErr is the variable the error must name, "" when the call must
		// succeed.
		wantErr string
		// wantSummary is the summary path the call must return.
		wantSummary string
		// wantKey reports whether the credentials must be set.
		wantKey bool
	}{
		{
			name:    "both halves of the key",
			env:     map[string]string{envAccessKey: accessKeyValue, envSecretKey: secretKeyValue},
			wantKey: true,
		},
		{
			name:        "the key and the summary path",
			env:         map[string]string{envAccessKey: accessKeyValue, envSecretKey: secretKeyValue, envStepSummary: summaryValue},
			wantKey:     true,
			wantSummary: summaryValue,
		},
		{
			name:    "no access key",
			env:     map[string]string{envSecretKey: secretKeyValue},
			wantErr: envAccessKey,
		},
		{
			name:    "an empty access key",
			env:     map[string]string{envAccessKey: "", envSecretKey: secretKeyValue},
			wantErr: envAccessKey,
		},
		{
			name:    "no secret key",
			env:     map[string]string{envAccessKey: accessKeyValue},
			wantErr: envSecretKey,
		},
		{
			name:    "an empty secret key",
			env:     map[string]string{envAccessKey: accessKeyValue, envSecretKey: ""},
			wantErr: envSecretKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			settings, err := environment(lookupIn(tc.env))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("environment = %+v, nil, want an error naming %s", settings, tc.wantErr)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("the error reads %q, want it to name the variable %s", err, tc.wantErr)
				}

				for _, key := range []string{accessKeyValue, secretKeyValue} {
					if strings.Contains(err.Error(), key) {
						t.Errorf("the error reads %q (the key %q), want no value of the key: it names the variable, never a credential", err, key)
					}
				}

				return
			}

			if err != nil {
				t.Fatalf("environment = %v, want nil", err)
			}

			if diff := cmp.Diff(tc.wantSummary, settings.summaryPath); diff != "" {
				t.Errorf("the summary path (-want +got):\n%s", diff)
			}

			if settings.credentials.AccessKey.IsZero() != !tc.wantKey || settings.credentials.SecretKey.IsZero() != !tc.wantKey {
				t.Errorf("the credentials are %+v, want both halves set (wantKey %t)", settings.credentials, tc.wantKey)
			}

			for _, text := range []string{
				fmt.Sprintf("%v", settings.credentials),
				fmt.Sprintf("%+v", settings.credentials),
				fmt.Sprintf("%#v", settings.credentials),
			} {
				for _, key := range []string{accessKeyValue, secretKeyValue} {
					if strings.Contains(text, key) {
						t.Errorf("printing the credentials reads %q, want neither half of the key", text)
					}
				}
			}
		})
	}
}

// TestSettingsNeverPrintsTheKey pins the other half of the key's hygiene at the
// entry point: the settings environment returns hold the key, so printing them,
// with any verb, must not print either half of it. A settings value in a log
// line, an error or a test failure names the key no more than a Config does
// (doc.go, "The key"; CLAUDE.md rule 3).
func TestSettingsNeverPrintsTheKey(t *testing.T) {
	t.Parallel()

	settings := settings{
		credentials: deploy.Credentials{
			AccessKey: deploy.NewSecret(accessKeyValue),
			SecretKey: deploy.NewSecret(secretKeyValue),
		},
		summaryPath: summaryValue,
	}

	printed := map[string]string{
		"%v":  fmt.Sprintf("%v", settings),
		"%+v": fmt.Sprintf("%+v", settings),
		"%#v": fmt.Sprintf("%#v", settings),
	}

	for verb, text := range printed {
		for _, key := range []string{accessKeyValue, secretKeyValue} {
			if strings.Contains(text, key) {
				t.Errorf("%s of the settings reads %q, want neither half of the key: the settings hold the deploy key, so every printed form of them is redacted", verb, text)
			}
		}
	}
}

// lookupIn returns the lookup function environment reads a test's environment
// with.
func lookupIn(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := env[name]

		return value, ok
	}
}
