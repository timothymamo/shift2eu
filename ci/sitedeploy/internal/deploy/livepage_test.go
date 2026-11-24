package deploy_test

import (
	"bytes"
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

// builtPages are the five pages ci/sitebuild writes (ADR 0036, issue #528),
// each with the footer LiveVersion reads.
var builtPages = []string{"about.html", "index.html", "inventory.html", "privacy.html", "verify.html"}

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
				t.Fatalf("LiveVersion(%s) = %v, want %q; the page's footer was %q", name, err, builtVersion, footerOf(page))
			}

			if got != builtVersion {
				t.Errorf("LiveVersion(%s) = %q, want %q; the page's footer was %q", name, got, builtVersion, footerOf(page))
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

// footerLimit bounds what footerOf prints, so a page that has lost its </p>
// cannot put a whole page into a failure message.
const footerLimit = 300

// footerOf returns the page's footer paragraph, from <p class="footer-links"> to
// the next </p>: the markup LiveVersion had to read, so a failure says what the
// page actually showed instead of only that the version was not there. A page
// without the opening paragraph says so. A paragraph longer than footerLimit
// bytes is cut in the middle rather than at the end, because the version's span
// is the paragraph's last element, past what a head alone would show.
func footerOf(page []byte) string {
	const open = `<p class="footer-links">`

	start := bytes.Index(page, []byte(open))
	if start < 0 {
		return "no " + open + " in the page"
	}

	footer := page[start:]
	if end := bytes.Index(footer, []byte("</p>")); end >= 0 {
		footer = footer[:end+len("</p>")]
	}

	if len(footer) > footerLimit {
		half := footerLimit / 2

		return string(footer[:half]) + " ... " + string(footer[len(footer)-half:])
	}

	return string(footer)
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
	// the environment win. The child stays offline (issue #514): GOPROXY=off
	// keeps a module from being fetched, GOTOOLCHAIN=local a toolchain from
	// being fetched, GOSUMDB=off the checksum database from being contacted,
	// and -mod=readonly makes a go.sum miss fail. GOSUMDB=off is needed on its
	// own because the go command falls back to a direct sumdb lookup for a
	// module's checksum even with GOPROXY=off and GOTOOLCHAIN=local. The test
	// reads an inventory and a catalog snapshot and needs no module, toolchain
	// or checksum it does not have, so a missing one fails here instead of
	// reaching the network.
	cmd.Env = append(cmd.Environ(), "GOOS=", "GOARCH=", "GOFLAGS=-mod=readonly", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local", "GOSUMDB=off")

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			t.Fatalf("go run ./ci/sitebuild %q did not finish within %v: %v\n%s", args, buildTimeout, err, output)
		}

		t.Fatalf("go run ./ci/sitebuild %q: %v\n%s", args, err, output)
	}

	return out
}

// trackTemplates opens every page template the builder reads from site/, so
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
