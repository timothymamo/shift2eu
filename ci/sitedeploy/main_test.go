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
	"io"
	"os"
	"path/filepath"
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

// TestParseFlags pins parseFlags: every flag the program needs is required, so a
// missing one is reported as missing rather than as a value of the wrong shape;
// the region is held to a Scaleway region's shape before it reaches the
// endpoint's URL (a dotted or slashed value would send the key to another host);
// and a stray positional argument is refused.
func TestParseFlags(t *testing.T) {
	t.Parallel()

	// everyFlag is the command line of a run that must parse.
	everyFlag := []string{
		"-bundle", "/bundle",
		"-version", "v0.13.0",
		"-bucket", "shift2eu-site",
		"-region", "fr-par",
		"-releases", "/releases",
		"-manifests", "/manifests",
	}

	// without returns everyFlag without the flag and its value.
	without := func(flag string) []string {
		args := make([]string, 0, len(everyFlag))

		for i := 0; i < len(everyFlag); i += 2 {
			if everyFlag[i] == flag {
				continue
			}

			args = append(args, everyFlag[i], everyFlag[i+1])
		}

		return args
	}

	// with returns everyFlag with the flag's value replaced.
	with := func(flag, value string) []string {
		args := make([]string, 0, len(everyFlag))

		for i := 0; i < len(everyFlag); i += 2 {
			if everyFlag[i] == flag {
				args = append(args, flag, value)

				continue
			}

			args = append(args, everyFlag[i], everyFlag[i+1])
		}

		return args
	}

	tests := []struct {
		name string
		args []string
		// wantFlag is the flag the error must name, "" when the call must
		// succeed or the error names no flag.
		wantFlag string
		// wantRequired reports whether the error must say the flag is missing or
		// required, rather than that its value is of the wrong shape.
		wantRequired bool
		// wantText is a substring the error must hold, "" when the test checks
		// nothing of its text.
		wantText string
	}{
		{name: "every flag", args: everyFlag},
		{name: "no bundle", args: without("-bundle"), wantFlag: "-bundle", wantRequired: true},
		{name: "no version", args: without("-version"), wantFlag: "-version", wantRequired: true},
		{name: "no bucket", args: without("-bucket"), wantFlag: "-bucket", wantRequired: true},
		{name: "no region", args: without("-region"), wantFlag: "-region", wantRequired: true},
		{name: "no releases", args: without("-releases"), wantFlag: "-releases", wantRequired: true},
		{name: "no manifests", args: without("-manifests"), wantFlag: "-manifests", wantRequired: true},
		{name: "a region with a fourth part", args: with("-region", "fr-par.x"), wantFlag: "-region"},
		{name: "a region that is a host name", args: with("-region", "fr-par.evil.example"), wantFlag: "-region"},
		{name: "a region in upper case", args: with("-region", "FR-PAR"), wantFlag: "-region"},
		{name: "a region with a slash", args: with("-region", "fr-par/x"), wantFlag: "-region"},
		{name: "a stray argument", args: append(append([]string{}, everyFlag...), "extra"), wantText: "unexpected"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseFlags(tc.args, io.Discard)

			if tc.wantFlag == "" && tc.wantText == "" {
				if err != nil {
					t.Fatalf("parseFlags(%q) = %v, want nil", tc.args, err)
				}

				if opts.region != "fr-par" {
					t.Errorf("parseFlags(%q) took the region %q, want %q", tc.args, opts.region, "fr-par")
				}

				return
			}

			if err == nil {
				t.Fatalf("parseFlags(%q) = %+v, nil, want an error", tc.args, opts)
			}

			if tc.wantFlag != "" && !strings.Contains(err.Error(), tc.wantFlag) {
				t.Errorf("the error reads %q, want it to name the flag %s: the job's maintainer must know which flag to add", err, tc.wantFlag)
			}

			if tc.wantRequired {
				text := strings.ToLower(err.Error())

				if !strings.Contains(text, "required") && !strings.Contains(text, "missing") {
					t.Errorf("the error reads %q for the missing flag %s, want it to say the flag is missing or required: an empty flag gets no value of the wrong shape (a missing -region is no malformed region)", err, tc.wantFlag)
				}
			}

			if tc.wantText != "" && !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("the error reads %q, want it to hold %q", err, tc.wantText)
			}
		})
	}
}

// TestAnnotationDataEscapesAWorkflowCommand pins fail's escaping: the error text
// becomes one workflow command's data, so a % and a line break in it must not
// end the annotation or start another command of the job's log.
func TestAnnotationDataEscapesAWorkflowCommand(t *testing.T) {
	t.Parallel()

	const input = "a%b\r\n::warning::x"

	got := annotationData(input)

	if want := "a%25b%0D%0A::warning::x"; got != want {
		t.Errorf("annotationData(%q) = %q, want %q: a percent sign and every line break are escaped as a workflow command's data", input, got, want)
	}

	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("annotationData(%q) = %q, want no CR and no LF: a line break in an annotation's data starts another workflow command", input, got)
	}
}

// TestReadReleasesTrimsBlankLinesAndCRLF pins readReleases: the tag list is
// written wherever the release job runs, so a file with CRLF line ends and blank
// lines must give the same tags as a LF one, or the rollback guard trusts no
// earlier release at all.
func TestReadReleasesTrimsBlankLinesAndCRLF(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "releases.txt")

	content := "v0.12.0\r\n\r\nv0.13.0\n\n  v0.14.0  \r\n\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write the release tags: %v", err)
	}

	got, err := readReleases(path)
	if err != nil {
		t.Fatalf("readReleases = %v, want nil", err)
	}

	want := []string{"v0.12.0", "v0.13.0", "v0.14.0"}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("readReleases (-want +got):\n%s\nwant every tag once, with the blank lines and the CRLF line ends trimmed", diff)
	}
}
