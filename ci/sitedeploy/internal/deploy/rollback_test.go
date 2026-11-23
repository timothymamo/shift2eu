package deploy_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// TestRollbackGuard pins doc.go's step 2: before it uploads, Deploy reads the
// live page once and refuses only when the page's version is a release tag
// higher than the bundle's AND the page's bytes are the index.html that tag's
// manifest names. Every other case is an overwrite with a visible warning in the
// summary, so a page a leaked key wrote, whatever version it claims, cannot
// block the redeploy that repairs the site.
func TestRollbackGuard(t *testing.T) {
	t.Parallel()

	// genuineHigher is the page the v0.14.0 release uploaded, and forgedHigher a
	// page a leaked key wrote that claims the same version.
	genuineHigher := page("v0.14.0", "Privacy")
	forgedHigher := append(page("v0.14.0", "Privacy"), []byte("<!-- a page a leaked key wrote -->\n")...)

	tests := []struct {
		name string
		// releases is the repository's tag list the guard trusts.
		releases []string
		// manifests maps a tag to the index.html its manifest names.
		manifests map[string][]byte
		// live is the live page's answer.
		live seenResponse
		// noLivePage closes the live page's server before Deploy runs, so the
		// guard's read fails before it reaches the page.
		noLivePage bool
		// wantRefuse reports whether the guard must refuse the deploy.
		wantRefuse bool
		// wantNote reports whether the summary must carry the guard's plain note
		// and no warning: the live page is a genuine release not higher than the
		// bundle's. Every other case must carry the warning and no note.
		wantNote bool
		// wantNamed is the version the warning must name, "" when the guard has
		// no version to name.
		wantNamed string
	}{
		{
			name:       "a genuine higher release is refused",
			releases:   []string{testedVersion, "v0.14.0"},
			manifests:  map[string][]byte{"v0.14.0": genuineHigher},
			live:       seenResponse{Status: http.StatusOK, Body: genuineHigher},
			wantRefuse: true,
		},
		{
			name:      "a higher tag whose page is not its manifest's is overwritten",
			releases:  []string{testedVersion, "v0.14.0"},
			manifests: map[string][]byte{"v0.14.0": genuineHigher},
			live:      seenResponse{Status: http.StatusOK, Body: forgedHigher},
			wantNamed: "v0.14.0",
		},
		{
			name:      "a version that is no release tag is overwritten",
			releases:  []string{testedVersion},
			manifests: map[string][]byte{"v0.14.0": genuineHigher},
			live:      seenResponse{Status: http.StatusOK, Body: genuineHigher},
			wantNamed: "v0.14.0",
		},
		{
			name:      "a tag with no manifest is overwritten",
			releases:  []string{testedVersion, "v0.14.0"},
			manifests: map[string][]byte{},
			live:      seenResponse{Status: http.StatusOK, Body: genuineHigher},
			wantNamed: "v0.14.0",
		},
		{
			name:      "the same version is overwritten",
			releases:  []string{testedVersion},
			manifests: map[string][]byte{testedVersion: page(testedVersion, "Privacy")},
			live:      seenResponse{Status: http.StatusOK, Body: page(testedVersion, "Privacy")},
			wantNote:  true,
			wantNamed: testedVersion,
		},
		{
			name:      "a lower version is overwritten",
			releases:  []string{earlierVersion, testedVersion},
			manifests: map[string][]byte{earlierVersion: page(earlierVersion, "Privacy")},
			live:      seenResponse{Status: http.StatusOK, Body: page(earlierVersion, "Privacy")},
			wantNote:  true,
			wantNamed: earlierVersion,
		},
		{
			name:     "a version that cannot be parsed is overwritten",
			releases: []string{testedVersion},
			live:     seenResponse{Status: http.StatusOK, Body: page("latest", "Privacy")},
		},
		{
			name:     "a page with no version is overwritten",
			releases: []string{testedVersion},
			live:     seenResponse{Status: http.StatusOK, Body: []byte(`<!DOCTYPE html><html><body><p class="footer-links"><a href="privacy.html">Privacy</a></p></body></html>`)},
		},
		{
			name:     "a page that is not 200 is overwritten",
			releases: []string{testedVersion},
			live:     seenResponse{Status: http.StatusServiceUnavailable, Body: []byte("the pipeline is down")},
		},
		{
			name:       "an unreachable page is overwritten",
			releases:   []string{testedVersion},
			noLivePage: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dc := newDeployCase(t)
			dc.config.Releases = tc.releases
			dc.config.Manifests = manifestsFS(t, tc.manifests)

			if tc.noLivePage {
				dc.live.Server.Close()
			} else {
				dc.live.setRespond(func(seenRequest) seenResponse { return tc.live })
			}

			err := dc.deploy(t)

			if !tc.noLivePage {
				if paths := dc.live.paths(); len(paths) != 1 {
					t.Errorf("the live page received %d requests (%q), want exactly one GET: the guard reads it once", len(paths), paths)
				}
			}

			if tc.wantRefuse {
				if !errors.Is(err, deploy.ErrRollback) {
					t.Fatalf("Deploy = %v, want an error wrapping ErrRollback: the live page is a genuine release higher than the bundle's", err)
				}

				if puts := dc.store.puts(); len(puts) != 0 {
					t.Errorf("the object store was sent %d PUTs, want none: a refused deploy changes nothing", len(puts))
				}

				return
			}

			if err != nil {
				t.Fatalf("Deploy = %v, want nil: the guard overwrites and warns", err)
			}

			if diff := cmp.Diff(wantUploadOrder(dc.bundle), dc.store.putKeys()); diff != "" {
				t.Errorf("the objects Deploy PUT, in order (-want +got):\n%s\nwant the whole bundle: an overwrite uploads every file", diff)
			}

			summary := dc.summary.String()

			if !strings.Contains(strings.ToLower(summary), "overwrit") {
				t.Errorf("the summary reads %q, want a visible warning that the deploy overwrote the live page and which case it was", summary)
			}

			if tc.wantNamed != "" && !strings.Contains(summary, tc.wantNamed) {
				t.Errorf("the summary reads %q, want the live page's version %q named, so the maintainer can see what was overwritten", summary, tc.wantNamed)
			}

			// A genuine release not higher than the bundle's is only a notice;
			// every page the guard does not trust is a warning, since a leaked key
			// could have written it.
			hasNote := strings.Contains(summary, "> [!NOTE]")
			hasWarning := strings.Contains(summary, "> [!WARNING]")

			if tc.wantNote {
				if !hasNote {
					t.Errorf("the summary reads %q, want the guard's plain note (> [!NOTE]): the live page is a genuine release not higher than the bundle's, so there is nothing to distrust", summary)
				}

				if hasWarning {
					t.Errorf("the summary reads %q, want no warning (> [!WARNING]) besides the note: the live page matches its release's manifest", summary)
				}

				return
			}

			if !hasWarning {
				t.Errorf("the summary reads %q, want a visible warning (> [!WARNING]): the live page is not trusted, so the maintainer must see which case it was", summary)
			}

			if hasNote {
				t.Errorf("the summary reads %q, want no note (> [!NOTE]): a page the guard does not trust must not be presented as a release's own", summary)
			}
		})
	}
}

// TestRollbackGuardFollowsNoRedirect pins that the guard reads the live page
// once and follows no redirect: a 302 answered with a genuine higher release at
// its target is an overwrite, not a refusal, and the target is never fetched. A
// followed redirect would carry the guard's read to another host, where the
// answer decides whether the site is repaired.
func TestRollbackGuardFollowsNoRedirect(t *testing.T) {
	t.Parallel()

	genuineHigher := page("v0.14.0", "Privacy")

	dc := newDeployCase(t)
	dc.config.Releases = []string{testedVersion, "v0.14.0"}
	dc.config.Manifests = manifestsFS(t, map[string][]byte{"v0.14.0": genuineHigher})
	dc.live.setRespond(func(request seenRequest) seenResponse {
		if request.Path == "/" {
			return seenResponse{
				Status: http.StatusFound,
				Header: http.Header{"Location": []string{"/elsewhere"}},
			}
		}

		return seenResponse{Status: http.StatusOK, Body: genuineHigher}
	})

	if err := dc.deploy(t); err != nil {
		t.Fatalf("Deploy = %v, want nil: a redirect is not the live page's answer, so the guard overwrites and warns", err)
	}

	if paths := dc.live.paths(); len(paths) != 1 || paths[0] != "/" {
		t.Errorf("the live page received the requests %q, want exactly one GET of \"/\": the guard follows no redirect", paths)
	}

	if !strings.Contains(strings.ToLower(dc.summary.String()), "overwrit") {
		t.Errorf("the summary reads %q, want a visible warning that the deploy overwrote the live page", dc.summary.String())
	}
}
