package deploy_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// fixedTime is the clock every test signs with, through Config.Now: no test
// depends on the wall clock.
var fixedTime = time.Date(2026, time.November, 20, 10, 30, 0, 0, time.UTC)

// deployCase is one run of Deploy against a fake object store and a fake live
// page: the two servers, the bundle, the writers Config needs, and the
// configuration itself.
type deployCase struct {
	store  *fakeServer
	live   *fakeServer
	bundle *bundle

	summary bytes.Buffer
	log     bytes.Buffer

	config deploy.Config
}

// newDeployCase returns the configuration Deploy must accept: a bundle at
// testedVersion, a live page at the release before it (so the guard overwrites),
// the invented key, a fixed clock and the two writers.
func newDeployCase(t *testing.T) *deployCase {
	t.Helper()

	dc := &deployCase{
		store:  newFakeServer(t, nil),
		bundle: newBundle(t, testedVersion),
	}

	dc.live = newFakeServer(t, func(seenRequest) seenResponse {
		return seenResponse{Status: http.StatusOK, Body: page(earlierVersion, "Privacy")}
	})

	dc.config = deploy.Config{
		Endpoint:    dc.store.Server.URL,
		Bucket:      testBucket,
		Region:      testRegion,
		Credentials: testCredentials,
		Client:      &http.Client{},
		Version:     dc.bundle.version,
		LivePage:    dc.live.Server.URL + "/",
		Releases:    []string{earlierVersion, testedVersion},
		Manifests:   manifestsFS(t, map[string][]byte{earlierVersion: page(earlierVersion, "Privacy")}),
		Summary:     &dc.summary,
		Log:         &dc.log,
		Now:         func() time.Time { return fixedTime },
	}
	dc.useBundle(t)

	return dc
}

// useBundle hands the case's current bundle to the configuration: a test that
// changes the bundle's files or manifest calls it after the change.
func (dc *deployCase) useBundle(t *testing.T) {
	t.Helper()

	dc.config.Bundle = dc.bundle.fsys(t)
}

// deploy runs Deploy once with the case's configuration.
func (dc *deployCase) deploy(t *testing.T) error {
	t.Helper()

	return deploy.Deploy(t.Context(), dc.config)
}

// wantUploadOrder returns the objects Deploy must PUT, in order: every
// content-addressed asset, sorted, then manifest.json, then the four pages,
// sorted (doc.go's step 4).
func wantUploadOrder(b *bundle) []string {
	var assets, pages []string

	for _, name := range b.keys() {
		if strings.HasSuffix(name, ".html") {
			pages = append(pages, name)
		} else {
			assets = append(assets, name)
		}
	}

	return append(append(assets, deploy.ManifestName), pages...)
}

// TestDeployUploadsTheAssetsThenTheManifestThenThePages pins the upload order and
// the requests Deploy is allowed to send (doc.go's step 4): every
// content-addressed asset before manifest.json, manifest.json before any page,
// each object at the path-style path /<bucket>/<name>, and no method but GET and
// PUT ever reaching the store, so a deploy never deletes an earlier release's
// file.
func TestDeployUploadsTheAssetsThenTheManifestThenThePages(t *testing.T) {
	t.Parallel()

	dc := newDeployCase(t)

	if err := dc.deploy(t); err != nil {
		t.Fatalf("Deploy = %v, want nil for a valid configuration and bundle", err)
	}

	if diff := cmp.Diff(wantUploadOrder(dc.bundle), dc.store.putKeys()); diff != "" {
		t.Errorf("the objects Deploy PUT, in order (-want +got):\n%s\nwant every content-addressed asset first, then manifest.json, then the pages: a failed upload before the pages leaves every page as it was", diff)
	}

	for _, request := range dc.store.seen() {
		if request.Method != http.MethodPut {
			t.Errorf("the object store received a %s of %s, want PUT only while ListBucket is off: Deploy reads the live page from its own URL and never deletes (%s)",
				request.Method, request.Path, request.Method)
		}

		if want := putPath(objectKey(request.Path)); request.Path != want {
			t.Errorf("an object was PUT to %q, want the path-style path %q", request.Path, want)
		}
	}

	if paths := dc.live.paths(); len(paths) != 1 {
		t.Errorf("the live page received %d requests (%q), want exactly one GET: the rollback guard reads it once", len(paths), paths)
	}

	requests := len(dc.store.seen()) + len(dc.live.seen())
	if lines := logLines(dc.log.String()); len(lines) != requests {
		t.Errorf("the log holds %d lines for %d requests, want one line per request (method, key, status): %q", len(lines), requests, lines)
	}
}

// TestDeployStopsAtTheFirstFailedUpload pins doc.go's step 4 for every object
// uploaded before the pages and for a page: an upload that fails stops the run
// with an error wrapping ErrUpload, and no object after the failing one is PUT
// at all, so the site keeps serving the previous release.
func TestDeployStopsAtTheFirstFailedUpload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
	}{
		{name: "the stylesheet", key: assetName("style", ".css", []byte(styleBody))},
		{name: "the page script", key: assetName("page", ".js", []byte(pageBody))},
		{name: "the toolchain's script", key: assetName("wasm_exec", ".js", []byte(wasmExecBody))},
		{name: "the module script", key: assetName("module", ".js", moduleBody)},
		{name: "the manifest", key: deploy.ManifestName},
		{name: "a page", key: "inventory.html"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dc := newDeployCase(t)
			dc.store.setRespond(func(request seenRequest) seenResponse {
				if request.Method == http.MethodPut && objectKey(request.Path) == tc.key {
					return seenResponse{Status: http.StatusInternalServerError, Body: []byte("<Error><Code>InternalError</Code></Error>")}
				}

				return seenResponse{Status: http.StatusOK}
			})

			err := dc.deploy(t)
			if !errors.Is(err, deploy.ErrUpload) {
				t.Fatalf("Deploy with %s failing = %v, want an error wrapping ErrUpload", tc.name, err)
			}

			order := wantUploadOrder(dc.bundle)

			failed := slices.Index(order, tc.key)
			if failed < 0 {
				t.Fatalf("the test's failing key %q is no object of the upload order %q", tc.key, order)
			}

			if diff := cmp.Diff(order[:failed+1], dc.store.putKeys()); diff != "" {
				t.Errorf("the objects Deploy PUT, in order (-want +got):\n%s\nwant the run stopped at the first failed upload, so nothing after %q is PUT: a page uploaded after a failure would serve a page of a bundle that is not there", diff, tc.key)
			}
		})
	}
}

// TestDeploySetsTheHeadersOfEveryObject pins doc.go's header table: each file's
// media type and charset, the cache policy of a content-addressed asset (a year
// and immutable) against the pages' and the manifest's (300 seconds at most, so
// a release shows within minutes), gzip on the module script alone with the
// bundle's own bytes behind it, and on every PUT a SigV4 Authorization header
// and the SHA-256 of the body sent.
func TestDeploySetsTheHeadersOfEveryObject(t *testing.T) {
	t.Parallel()

	dc := newDeployCase(t)

	if err := dc.deploy(t); err != nil {
		t.Fatalf("Deploy = %v, want nil for a valid configuration and bundle", err)
	}

	puts := dc.store.puts()
	if len(puts) != len(wantUploadOrder(dc.bundle)) {
		t.Fatalf("the store received %d PUTs, want one per file of the bundle (%d)", len(puts), len(wantUploadOrder(dc.bundle)))
	}

	for _, put := range puts {
		name := objectKey(put.Path)
		want := wantHeaders(t, name)

		mediaType, params, err := mime.ParseMediaType(put.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("%s is PUT with Content-Type %q, want a media type: %v", name, put.Header.Get("Content-Type"), err)

			continue
		}

		if mediaType != want.mediaType {
			t.Errorf("%s is PUT with Content-Type %q, want %q", name, mediaType, want.mediaType)
		}

		if want.charset != "" && params["charset"] != want.charset {
			t.Errorf("%s is PUT with charset %q, want %q: the pages and the text assets are UTF-8", name, params["charset"], want.charset)
		}

		if got := put.Header.Get("Content-Encoding"); got != want.encoding {
			t.Errorf("%s is PUT with Content-Encoding %q, want %q: only the module script is sent gzip-compressed", name, got, want.encoding)
		}

		// The expected body is the bundle's own bytes: files holds every file but
		// manifest.json, whose bytes the bundle builds from them.
		wantBody := dc.bundle.files[name]
		if name == deploy.ManifestName {
			wantBody = dc.bundle.manifestBytes(t)
		}

		if want.encoding == "gzip" {
			if diff := cmp.Diff(wantBody, gunzip(t, name, put.Body)); diff != "" {
				t.Errorf("the decoded body of %s (-want +got):\n%s\nwant the bundle's own bytes: the manifest's digest is of the decoded file, as the live check compares after content decoding", name, diff)
			}
		} else if diff := cmp.Diff(wantBody, put.Body); diff != "" {
			t.Errorf("the body of %s (-want +got):\n%s\nwant the bundle's own bytes", name, diff)
		}

		cacheControl := put.Header.Get("Cache-Control")

		seconds, ok := maxAge(cacheControl)
		if !ok {
			t.Errorf("%s is PUT with Cache-Control %q, want a max-age", name, cacheControl)

			continue
		}

		if want.immutable {
			if seconds < 31536000 {
				t.Errorf("%s is PUT with Cache-Control %q (max-age %d), want a max-age of at least a year: it is content-addressed, so it never changes", name, cacheControl, seconds)
			}

			if !strings.Contains(strings.ToLower(cacheControl), "immutable") {
				t.Errorf("%s is PUT with Cache-Control %q, want immutable: a content-addressed file never changes", name, cacheControl)
			}

			continue
		}

		if seconds > 300 {
			t.Errorf("%s is PUT with Cache-Control %q (max-age %d), want a max-age of 300 or less: a release must show within minutes without a cache purge", name, cacheControl, seconds)
		}
	}

	for _, put := range puts {
		if got := put.Header.Get("Authorization"); !strings.HasPrefix(got, "AWS4-HMAC-SHA256 ") {
			t.Errorf("%s is PUT with Authorization %q, want a SigV4 header starting %q", objectKey(put.Path), got, "AWS4-HMAC-SHA256 ")
		}

		if got, want := put.Header.Get("X-Amz-Content-Sha256"), digest(put.Body); got != want {
			t.Errorf("%s is PUT with X-Amz-Content-Sha256 %q, want %q, the SHA-256 of the body sent", objectKey(put.Path), got, want)
		}

		if got := put.Header.Get("X-Amz-Date"); got != fixedTime.Format("20060102T150405Z") {
			t.Errorf("%s is PUT with X-Amz-Date %q, want %q, the date Config.Now gives", objectKey(put.Path), got, fixedTime.Format("20060102T150405Z"))
		}
	}
}

// headerWant is what one object's PUT must carry.
type headerWant struct {
	// mediaType is the Content-Type's media type.
	mediaType string
	// charset is the Content-Type's charset parameter, "" when it has none.
	charset string
	// encoding is the Content-Encoding, "" when there is none.
	encoding string
	// immutable reports whether the object is content-addressed, and so cached
	// for a year.
	immutable bool
}

// wantHeaders returns the headers doc.go's table gives the file name.
func wantHeaders(t *testing.T, name string) headerWant {
	t.Helper()

	switch {
	case strings.HasSuffix(name, ".html"):
		return headerWant{mediaType: "text/html", charset: "utf-8"}
	case name == deploy.ManifestName:
		return headerWant{mediaType: "application/json"}
	case strings.HasPrefix(name, "style."):
		return headerWant{mediaType: "text/css", charset: "utf-8", immutable: true}
	case strings.HasPrefix(name, "module."):
		return headerWant{mediaType: "text/javascript", charset: "utf-8", encoding: "gzip", immutable: true}
	case strings.HasPrefix(name, "page."), strings.HasPrefix(name, "wasm_exec."):
		return headerWant{mediaType: "text/javascript", charset: "utf-8", immutable: true}
	}

	t.Fatalf("the bundle holds %q, which this test knows no headers for", name)

	return headerWant{}
}

// TestDeployNeverPrintsTheKey pins doc.go's "The key": with a store that echoes
// both halves of the key back in an error body, a header and an S3 error code, a
// failed upload's error, the summary and the log hold neither value.
func TestDeployNeverPrintsTheKey(t *testing.T) {
	t.Parallel()

	dc := newDeployCase(t)
	dc.store.setRespond(func(seenRequest) seenResponse {
		return seenResponse{
			Status: http.StatusForbidden,
			Header: http.Header{
				"Content-Type":      []string{"application/xml"},
				"X-Amz-Request-Id":  []string{testSecretKey},
				"X-Echo-Access-Key": []string{testAccessKey},
			},
			Body: []byte(`<Error><Code>` + testAccessKey + `</Code><Message>the key ` + testSecretKey + ` is not valid</Message></Error>`),
		}
	})

	err := dc.deploy(t)
	if !errors.Is(err, deploy.ErrUpload) {
		t.Fatalf("Deploy against a store refusing every PUT = %v, want an error wrapping ErrUpload", err)
	}

	written := map[string]string{
		"the error":   err.Error(),
		"the summary": dc.summary.String(),
		"the log":     dc.log.String(),
	}

	for where, text := range written {
		for _, key := range []string{testAccessKey, testSecretKey} {
			if strings.Contains(text, key) {
				t.Errorf("%s holds %q, want neither half of the key: a hostile server response echoing the key back must not be copied into anything the job prints (%s)", where, key, text)
			}
		}
	}

	// The assertions above are only worth something if the run did print the
	// failed request and the guard's warning: an empty log and an empty summary
	// hold no key for the wrong reason.
	var failedPut bool

	for _, line := range logLines(dc.log.String()) {
		if strings.HasPrefix(line, "PUT ") && strings.Contains(line, " 403") {
			failedPut = true
		}
	}

	if !failedPut {
		t.Errorf("the log reads %q, want a PUT line holding the status 403: the run logs every request, the failed ones included (doc.go, Config.Log)", dc.log.String())
	}

	summary := dc.summary.String()

	if strings.TrimSpace(summary) == "" {
		t.Fatalf("the summary is empty, want the rollback guard's notice or warning: a failed upload must not swallow what the run already wrote, so the key's redaction is pinned against a summary a maintainer reads")
	}

	if !strings.Contains(summary, "> [!NOTE]") && !strings.Contains(summary, "> [!WARNING]") {
		t.Errorf("the summary reads %q, want the rollback guard's notice or warning: the guard's verdict is what the summary opens with", summary)
	}
}

// TestDeployNeverPrintsWhatAServerWrote pins doc.go's "The key" against a
// server's own bytes: a transport error's text is whatever net/http makes of the
// answer, and net/http quotes the server's status line back. None of it may
// reach the error, the log or the summary, whatever the server wrote.
func TestDeployNeverPrintsWhatAServerWrote(t *testing.T) {
	t.Parallel()

	// The one token net/http reads as the response's status code holds the
	// marker and both halves of the key, so its parse error quotes them today:
	// HTTP/1.1 MARKER-<access key>-<secret key> 2OO.
	statusLine := "HTTP/1.1 MARKER-" + testAccessKey + "-" + testSecretKey + " 2OO\r\n\r\n"

	tests := []struct {
		name string
		// store reports whether the raw host answers the object store instead of
		// the live page.
		store bool
		// listBucket turns the listing on, so the store's first request is the
		// listing rather than a PUT.
		listBucket bool
		// wantErr reports whether Deploy must fail: the live page's failure is
		// only a warning, so that run overwrites.
		wantErr bool
	}{
		{name: "the live page", store: false},
		{name: "a PUT", store: true, wantErr: true},
		{name: "the listing", store: true, listBucket: true, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dc := newDeployCase(t)
			raw := newRawServer(t, statusLine)
			dc.config.ListBucket = tc.listBucket

			if tc.store {
				dc.config.Endpoint = raw.URL()
			} else {
				dc.config.LivePage = raw.URL() + "/"
			}

			err := dc.deploy(t)

			if tc.wantErr && err == nil {
				t.Fatalf("Deploy against a raw host answering an unparseable status line = nil, want an error: a transport error to the store fails the run")
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("Deploy with an unreadable live page = %v, want nil: the guard overwrites and warns when it cannot read the page", err)
			}

			errorText := ""
			if err != nil {
				errorText = err.Error()
			}

			written := map[string]string{
				"the error":   errorText,
				"the log":     dc.log.String(),
				"the summary": dc.summary.String(),
			}

			for where, text := range written {
				for _, secret := range []string{"MARKER", testAccessKey, testSecretKey} {
					if strings.Contains(text, secret) {
						t.Errorf("%s holds %q, which only the raw host wrote, want a fixed classification of the transport error (\"timed out\", \"cancelled\", \"transport error\"): net/http's own message quotes the server's status line, so formatting the transport error with %%v puts a server's bytes into what the job prints (%s)",
							where, secret, text)
					}
				}
			}
		})
	}
}

// TestDeploySignsNoRequestToTheLivePage pins doc.go's step 2, "The guard's GET
// is never signed": the live page is no S3 host and reads nothing of the key, so
// the guard's request carries no Authorization header, no X-Amz-Date, no
// X-Amz-Content-Sha256 and no header holding either half of the key.
func TestDeploySignsNoRequestToTheLivePage(t *testing.T) {
	t.Parallel()

	dc := newDeployCase(t)

	if err := dc.deploy(t); err != nil {
		t.Fatalf("Deploy = %v, want nil for a valid configuration and bundle", err)
	}

	requests := dc.live.seen()
	if len(requests) != 1 {
		t.Fatalf("the live page received %d requests, want one GET: the rollback guard reads it once", len(requests))
	}

	for _, name := range []string{"Authorization", "X-Amz-Date", "X-Amz-Content-Sha256"} {
		if got := requests[0].Header.Get(name); got != "" {
			t.Errorf("the live page's request carries %s %q, want no signing header: the key must never reach the live page's host, which is no S3 store", name, got)
		}
	}

	for name, values := range requests[0].Header {
		for _, value := range values {
			for _, key := range []string{testAccessKey, testSecretKey} {
				if strings.Contains(value, key) {
					t.Errorf("the live page's request carries the header %s: %q, which holds the key %q, want no request to the live page to hold either half of the key", name, value, key)
				}
			}
		}
	}
}

// TestDeployFollowsNoStoreRedirect pins the other half of Config.Client's copied
// policy: a store that answers a PUT with a 307 and a Location follows no
// redirect, so the signed request is never sent to the second host, and the run
// stops at that PUT with ErrUpload.
func TestDeployFollowsNoStoreRedirect(t *testing.T) {
	t.Parallel()

	dc := newDeployCase(t)
	elsewhere := newFakeServer(t, nil)

	dc.store.setRespond(func(seenRequest) seenResponse {
		return seenResponse{
			Status: http.StatusTemporaryRedirect,
			Header: http.Header{"Location": []string{elsewhere.Server.URL + "/x"}},
		}
	})

	err := dc.deploy(t)
	if !errors.Is(err, deploy.ErrUpload) {
		t.Fatalf("Deploy against a store answering a PUT with a 307 = %v, want an error wrapping ErrUpload: a redirect is no successful upload", err)
	}

	if requests := elsewhere.seen(); len(requests) != 0 {
		t.Errorf("the redirect's target received %d requests (%q), want none: a followed redirect would carry a signed request to another host", len(requests), elsewhere.paths())
	}

	for _, key := range dc.store.putKeys() {
		if strings.HasSuffix(key, ".html") {
			t.Errorf("the page %q was PUT although the first PUT was redirected, want the run stopped at the first failed upload: a page uploaded after a failure serves a page of a bundle that is not there", key)
		}
	}
}

// TestDeployRefusesAnInvalidConfigurationOrBundle pins doc.go's validation: each
// configuration and each bundle it names is refused before any request, with an
// error wrapping ErrInvalidConfig or ErrInvalidBundle, so neither the object
// store nor the live page is contacted.
func TestDeployRefusesAnInvalidConfigurationOrBundle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(t *testing.T, dc *deployCase)
	}{
		{
			name: "a bucket name too short",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bucket = "ab"
			},
		},
		{
			name: "a bucket name with a capital letter",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bucket = "Shift2eu-site"
			},
		},
		{
			name: "a bucket name with an underscore",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bucket = "shift2eu_site"
			},
		},
		{
			name: "a bucket name starting with a dot",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bucket = ".shift2eu-site"
			},
		},
		{
			name: "a bucket name ending with a dash",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bucket = "shift2eu-site-"
			},
		},
		{
			name: "a bucket name longer than 63 characters",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bucket = strings.Repeat("shift2eu-", 8) + "site"
			},
		},
		{
			name: "an endpoint with a path",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Endpoint = dc.store.Server.URL + "/objects"
			},
		},
		{
			name: "an endpoint that is not a URL",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Endpoint = "s3.fr-par.scw.cloud"
			},
		},
		{
			name: "no client",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Client = nil
			},
		},
		{
			name: "no summary writer",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Summary = nil
			},
		},
		{
			name: "no log writer",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Log = nil
			},
		},
		{
			name: "no clock",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Now = nil
			},
		},
		{
			name: "no live page",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.LivePage = ""
			},
		},
		{
			name: "no access key",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Credentials.AccessKey = deploy.NewSecret("")
			},
		},
		{
			name: "no secret key",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Credentials.SecretKey = deploy.NewSecret("")
			},
		},
		{
			name: "no bundle",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Bundle = nil
			},
		},
		{
			name: "no version",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Version = ""
			},
		},
		{
			name: "a version that is no release tag",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Version = strings.TrimPrefix(dc.bundle.version, "v")
			},
		},
		{
			name: "a release that is no release tag",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Releases = []string{earlierVersion, "main"}
			},
		},
		{
			name: "a version that is not the manifest's",
			mutate: func(_ *testing.T, dc *deployCase) {
				dc.config.Version = "v0.14.0"
			},
		},
		{
			name: "a manifest path with a slash",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				withManifestPath(t, dc, "sub/index.html")
			},
		},
		{
			name: "a manifest path with two dots",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				withManifestPath(t, dc, "../index.html")
			},
		},
		{
			name: "a manifest path with a backslash",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				withManifestPath(t, dc, `index\html`)
			},
		},
		{
			name: "a manifest path with a percent sign",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				withManifestPath(t, dc, "index%2Ehtml")
			},
		},
		{
			name: "an empty manifest path",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				withManifestPath(t, dc, "")
			},
		},
		{
			name: "a manifest path that is no file of the bundle",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				withManifestPath(t, dc, "index.css")
			},
		},
		{
			name: "an asset whose name is not its digest's prefix",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.files[assetName("style", ".css", []byte(styleBody))] = []byte("the stylesheet, changed after it was named")
				dc.useBundle(t)
			},
		},
		{
			name: "a file whose digest is not the manifest's",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.manifest = manifestOf(t, dc.bundle.version, dc.bundle.files)
				dc.bundle.files["index.html"] = append([]byte("changed after the manifest was written\n"), dc.bundle.files["index.html"]...)
				dc.useBundle(t)
			},
		},
		{
			name: "a file the manifest does not list",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.files["extra.html"] = []byte("a page no manifest names\n")
				dc.useBundle(t)
			},
		},
		{
			name: "a file the manifest lists and the bundle does not hold",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.manifest = manifestOf(t, dc.bundle.version, dc.bundle.files)
				delete(dc.bundle.files, assetName("page", ".js", []byte(pageBody)))
				dc.useBundle(t)
			},
		},
		{
			name: "a manifest with no files",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.files = map[string][]byte{}
				dc.bundle.manifest = []byte(`{"version":"` + dc.bundle.version + `","files":[]}`)
				dc.useBundle(t)
			},
		},
		{
			name: "a bundle without index.html",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				delete(dc.bundle.files, "index.html")
				dc.bundle.manifest = manifestOf(t, dc.bundle.version, dc.bundle.files)
				dc.useBundle(t)
			},
		},
		{
			name: "a bundle with two stylesheets",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.files[assetName("style", ".css", []byte(secondStyleBody))] = []byte(secondStyleBody)
				dc.bundle.manifest = manifestOf(t, dc.bundle.version, dc.bundle.files)
				dc.useBundle(t)
			},
		},
		{
			name: "a manifest that is not JSON",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.manifest = []byte("not a manifest\n")
				dc.useBundle(t)
			},
		},
		{
			name: "a manifest with a duplicate field",
			mutate: func(t *testing.T, dc *deployCase) {
				t.Helper()

				dc.bundle.manifest = []byte(`{"version":"v0.13.0","version":"v0.13.0","files":[]}`)
				dc.useBundle(t)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dc := newDeployCase(t)
			tc.mutate(t, dc)

			err := dc.deploy(t)
			if err == nil {
				t.Fatalf("Deploy = nil for %s, want an error wrapping ErrInvalidConfig or ErrInvalidBundle", tc.name)
			}

			if !errors.Is(err, deploy.ErrInvalidConfig) && !errors.Is(err, deploy.ErrInvalidBundle) {
				t.Fatalf("Deploy for %s = %v, want an error wrapping ErrInvalidConfig or ErrInvalidBundle", tc.name, err)
			}

			if requests := dc.store.seen(); len(requests) != 0 {
				t.Errorf("the object store received %d requests for %s, want none: the configuration and the bundle are refused before any request", len(requests), tc.name)
			}

			if requests := dc.live.seen(); len(requests) != 0 {
				t.Errorf("the live page received %d requests for %s, want none: the configuration and the bundle are refused before any request", len(requests), tc.name)
			}
		})
	}
}

// withManifestPath hands Deploy a manifest naming one path of the test's
// choosing, and nothing else.
func withManifestPath(t *testing.T, dc *deployCase, path string) {
	t.Helper()

	dc.bundle.manifest = manifestNaming(t, dc.bundle.version, []manifestFileDoc{
		{Path: path, SHA256: digest(dc.bundle.files["index.html"])},
	})
	dc.useBundle(t)
}

// gunzip returns the decoded bytes of a gzip body, failing the test when it is
// not gzip at all.
func gunzip(t *testing.T, name string, body []byte) []byte {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s is sent with Content-Encoding gzip, but its body is not gzip: %v", name, err)
	}

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("decode the gzip body of %s: %v", name, err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("close the gzip reader of %s: %v", name, err)
	}

	return decoded
}

// maxAge returns the max-age of a Cache-Control header, and whether it carries
// one.
func maxAge(cacheControl string) (int, bool) {
	for directive := range strings.SplitSeq(cacheControl, ",") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(directive), "max-age=")
		if !ok {
			continue
		}

		seconds, err := strconv.Atoi(rest)
		if err != nil {
			return 0, false
		}

		return seconds, true
	}

	return 0, false
}

// logLines returns the non-empty lines of the log, so a test counts the lines
// Deploy wrote rather than the trailing newline.
func logLines(log string) []string {
	var lines []string

	for line := range strings.SplitSeq(log, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	return lines
}
