package deploy_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// builtVersion is the release tag the test builds the site with: the version
// the built pages' footer must show.
const builtVersion = "v0.13.0"

// buildTimeout bounds one site build, so a builder that hangs fails the test
// that needed it instead of go test's ten-minute default, which panics the
// whole package.
const buildTimeout = 5 * time.Minute

// builtPages are the four pages ci/sitebuild writes (ADR 0036), each with the
// footer LiveVersion reads.
var builtPages = []string{"index.html", "inventory.html", "privacy.html", "verify.html"}

// TestLiveVersionOnTheBuiltPages pins LiveVersion against the real build rather
// than a fixture: every page ci/sitebuild writes ends its footer's paragraph
// with <span class="mono" id="site-version">v0.13.0</span></p>, so LiveVersion
// reads v0.13.0 from each. A fixture of markup no page of site/ has any more
// passes while every deployed page is refused, which is why the rollback guard
// reads the built pages here.
func TestLiveVersionOnTheBuiltPages(t *testing.T) {
	t.Parallel()

	bundle := buildSite(t, builtVersion)

	for _, name := range builtPages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			page := readPage(t, filepath.Join(bundle, name))

			got, err := deploy.LiveVersion(page)
			if err != nil {
				t.Fatalf("LiveVersion(%s) = %v, want %q", name, err, builtVersion)
			}

			if got != builtVersion {
				t.Errorf("LiveVersion(%s) = %q, want %q", name, got, builtVersion)
			}
		})
	}
}

// TestLiveVersionRefusesADevelopmentBuild pins the other side: a bundle built
// without -version, whose footer shows internal/version's development value
// instead of a release tag, is no live version at all. The guard overwrites
// such a page rather than trusting a version it reads out of it.
func TestLiveVersionRefusesADevelopmentBuild(t *testing.T) {
	t.Parallel()

	bundle := buildSite(t, "")

	page := readPage(t, filepath.Join(bundle, "index.html"))

	got, err := deploy.LiveVersion(page)
	if !errors.Is(err, deploy.ErrNoLiveVersion) {
		t.Fatalf("LiveVersion = %q, %v, want an error wrapping ErrNoLiveVersion", got, err)
	}

	if got != "" {
		t.Errorf("LiveVersion = %q with its error, want no version at all", got)
	}
}

// buildSite runs ci/sitebuild and returns the directory of the bundle it wrote.
// An empty version builds the site as a development build (no -version), as the
// other tests' fixtures cannot show.
func buildSite(t *testing.T, version string) string {
	t.Helper()

	root := moduleRoot(t)
	trackTemplates(t, root)

	scratch := t.TempDir()

	out := filepath.Join(scratch, "bundle")
	archive := filepath.Join(scratch, "bundle.tar.gz")

	args := []string{"run", "./ci/sitebuild", "-out", out, "-archive", archive}
	if version != "" {
		args = append(args, "-version", version)
	}

	ctx, cancel := context.WithTimeout(t.Context(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = root
	// The builder builds cmd/shift2eu-web for js/wasm itself, so an inherited
	// GOOS or GOARCH of another target must not reach it; the later entries of
	// the environment win.
	cmd.Env = append(cmd.Environ(), "GOOS=", "GOARCH=", "GOFLAGS=-mod=readonly", "GOWORK=off")

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("go run ./ci/sitebuild %q did not finish within %v: %v\n%s", args, buildTimeout, err, output)
		}

		t.Fatalf("go run ./ci/sitebuild %q: %v\n%s", args, err, output)
	}

	return out
}

// trackTemplates opens the four page templates the builder reads from site/, so
// go test's result cache records them and a footer edit invalidates a cached
// PASS: the builder is a child process, whose reads the cache cannot see.
func trackTemplates(t *testing.T, root string) {
	t.Helper()

	for _, name := range builtPages {
		if _, err := os.ReadFile(filepath.Join(root, "site", name)); err != nil {
			t.Fatalf("read the site's page template %s: %v", name, err)
		}
	}
}

// moduleRoot walks up from the test's working directory, the package's own
// directory, to the directory holding go.mod: ci/sitebuild is built and run
// from the module root.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get the working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package's directory")
		}

		dir = parent
	}
}

// readPage returns a built page's bytes.
func readPage(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the built page: %v", err)
	}

	return data
}
