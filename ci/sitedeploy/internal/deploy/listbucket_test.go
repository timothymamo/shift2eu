package deploy_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// listPage returns one page of an S3 ListObjectsV2 answer (doc.go's step 3),
// holding keys and, when next is not empty, the continuation token of the page
// after it.
func listPage(bucket string, keys []string, next string) []byte {
	var contents strings.Builder

	for _, key := range keys {
		fmt.Fprintf(&contents, "  <Contents><Key>%s</Key><Size>1</Size><StorageClass>STANDARD</StorageClass></Contents>\n", key)
	}

	truncated := "false"
	token := ""

	if next != "" {
		truncated = "true"
		token = "  <NextContinuationToken>" + next + "</NextContinuationToken>\n"
	}

	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>` + bucket + `</Name>
  <Prefix></Prefix>
  <KeyCount>` + fmt.Sprint(len(keys)) + `</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>` + truncated + `</IsTruncated>
` + token + contents.String() + `</ListBucketResult>
`)
}

// TestListBucket's cases pin doc.go's step 3: a Config that does not ask for the
// listing sends no listing request; a listing that is asked for walks every page
// (ListObjectsV2 pages) and fails on a key that is in no release's manifest and
// in no bundle before it uploads anything; a listing of known keys proceeds; and
// a hostile key a leaked key wrote never becomes a write or a delete.
//
// A fake store's listing answer returns the keys the test chose, including keys
// no manifest names and paths that would be dangerous if they became requests of
// their own.
func TestListBucket(t *testing.T) {
	t.Parallel()

	t.Run("the zero value sends no listing", func(t *testing.T) {
		t.Parallel()

		dc := newDeployCase(t)

		if dc.config.ListBucket {
			t.Fatal("the configuration asks for the listing, want ListBucket off by default")
		}

		if err := dc.deploy(t); err != nil {
			t.Fatalf("Deploy = %v, want nil: the listing is off by default", err)
		}

		if listings := listRequests(dc.store.seen()); len(listings) != 0 {
			t.Errorf("the object store received %d listing requests, want none: ListBucket is off by default (the hosting ADR records that it needs runbook step R7's grant)", len(listings))
		}
	})

	t.Run("a key no release names fails before the upload", func(t *testing.T) {
		t.Parallel()

		dc := newDeployCase(t)
		dc.config.ListBucket = true

		dc.store.setRespond(listResponder(t,
			listPage(testBucket, []string{"index.html", deploy.ManifestName}, "page-2"),
			listPage(testBucket, []string{"stray.html"}, ""),
		))

		err := dc.deploy(t)
		if !errors.Is(err, deploy.ErrUnknownObject) {
			t.Fatalf("Deploy with a bucket holding an object no release names = %v, want an error wrapping ErrUnknownObject", err)
		}

		if !strings.Contains(err.Error(), "stray.html") {
			t.Errorf("the error reads %q, want it to name the object that is in no release's manifest, %q", err, "stray.html")
		}

		if puts := dc.store.puts(); len(puts) != 0 {
			t.Errorf("the object store was sent %d PUTs, want none: the listing refuses before any upload", len(puts))
		}

		listings := listRequests(dc.store.seen())
		if len(listings) != 2 {
			t.Fatalf("the object store received %d listing requests, want two: the answer's first page is truncated, so the second page must be read", len(listings))
		}

		for _, listing := range listings {
			if listing.Method != http.MethodGet {
				t.Errorf("a listing was sent with %s, want GET", listing.Method)
			}

			if want := "/" + testBucket; listing.Path != want {
				t.Errorf("a listing was sent to %q, want %q", listing.Path, want)
			}
		}

		if got, want := listings[1].Query.Get("continuation-token"), "page-2"; got != want {
			t.Errorf("the second listing carries the continuation token %q, want %q from the first page", got, want)
		}
	})

	t.Run("a bucket of known keys proceeds", func(t *testing.T) {
		t.Parallel()

		dc := newDeployCase(t)
		dc.config.ListBucket = true

		// The keys a release leaves in the bucket: this bundle's, the manifest
		// itself, and an earlier release's content-addressed asset, which no
		// manifest of this bundle names.
		known := append(dc.bundle.keys(), deploy.ManifestName, "style.0123456789abcdef.css")
		slices.Sort(known)
		halfway := len(known) / 2

		dc.store.setRespond(listResponder(t,
			listPage(testBucket, known[:halfway], "page-2"),
			listPage(testBucket, known[halfway:], ""),
		))

		if err := dc.deploy(t); err != nil {
			t.Fatalf("Deploy with a bucket holding only keys a release names = %v, want nil", err)
		}

		if diff := cmp.Diff(wantUploadOrder(dc.bundle), dc.store.putKeys()); diff != "" {
			t.Errorf("the objects Deploy PUT, in order (-want +got):\n%s\nwant the whole bundle: a listing of known keys proceeds", diff)
		}

		if listings := listRequests(dc.store.seen()); len(listings) != 2 {
			t.Errorf("the object store received %d listing requests, want two, the answer's two pages", len(listings))
		}
	})

	t.Run("hostile keys never become a write or a delete", func(t *testing.T) {
		t.Parallel()

		// longKey holds the access key straddling byte 64 of the object name, so a
		// cut to 64 bytes before scrubbing would print a prefix of the key; the
		// other keys are dangerous if they became requests of their own, and one
		// starts with a line break and a workflow command.
		longKey := strings.Repeat("k", 55) + testAccessKey + strings.Repeat("z", 10) + ".html"

		hostile := []string{
			"../x",
			"/etc/passwd",
			"index%2F..%2Findex.html",
			"a%2Fb",
			"\n::error::x",
			longKey,
		}

		dc := newDeployCase(t)
		dc.config.ListBucket = true

		dc.store.setRespond(listResponder(t, listPage(testBucket, hostile, "")))

		err := dc.deploy(t)
		if !errors.Is(err, deploy.ErrUnknownObject) {
			t.Fatalf("Deploy with hostile keys in the bucket = %v, want an error wrapping ErrUnknownObject: a listed key that is in no manifest refuses the deploy before any upload, and a hostile key is no reason to let it through", err)
		}

		if puts := dc.store.puts(); len(puts) != 0 {
			t.Errorf("the object store was sent %d PUTs (%q), want none: the listing refuses before any upload", len(puts), dc.store.putKeys())
		}

		if listings := listRequests(dc.store.seen()); len(listings) != 1 {
			t.Errorf("the object store received %d listing requests, want one: the first page's keys are enough to refuse, and the store must not be asked again", len(listings))
		}

		for _, key := range hostile {
			// A key the error cuts is named by its quoted beginning: the error must
			// not hold a 64-byte cut of a key it has not scrubbed.
			if len(key) > 64 {
				if !strings.Contains(err.Error(), `"`+key[:16]) {
					t.Errorf("the error reads %q, want it to quote the beginning of the hostile key %q", err, key[:16]+"...")
				}

				continue
			}

			if want := strconv.Quote(key); !strings.Contains(err.Error(), want) {
				t.Errorf("the error reads %q, want it to name the hostile key %s: quoting escapes a line break and a quote, so neither can start another workflow command", err, want)
			}
		}

		written := map[string]string{
			"the error":   err.Error(),
			"the log":     dc.log.String(),
			"the summary": dc.summary.String(),
		}

		for where, text := range written {
			for n := 8; n <= len(testAccessKey); n++ {
				if strings.Contains(text, testAccessKey[:n]) {
					t.Errorf("%s holds %q, the first %d characters of the access key: a key must be scrubbed before it is quoted and cut, or a long object name prints a prefix of it (%s)", where, testAccessKey[:n], n, text)

					break
				}
			}
		}
	})

	t.Run("a continuation token is encoded as Sign canonicalises it", func(t *testing.T) {
		t.Parallel()

		dc := newDeployCase(t)
		dc.config.ListBucket = true

		known := append(dc.bundle.keys(), deploy.ManifestName)
		slices.Sort(known)

		// The first page's token holds a space and a plus sign: the canonical query
		// sends them as %20 and %2B, so the request must send the same, or the
		// signature the store checks is of a query the store never received.
		dc.store.setRespond(listResponder(t,
			listPage(testBucket, known, "a b+c"),
			listPage(testBucket, nil, ""),
		))

		if err := dc.deploy(t); err != nil {
			t.Fatalf("Deploy = %v, want nil: every listed key is a key of the bundle", err)
		}

		listings := listRequests(dc.store.seen())
		if len(listings) != 2 {
			t.Fatalf("the object store received %d listing requests, want two: the first page is truncated", len(listings))
		}

		if got, want := listings[1].Query.Get("continuation-token"), "a b+c"; got != want {
			t.Errorf("the second listing carries the continuation token %q, want %q from the first page", got, want)
		}

		if got := listings[1].RawQuery; !strings.Contains(got, "continuation-token=a%20b%2Bc") {
			t.Errorf("the second listing's query reads %q, want the token encoded as continuation-token=a%%20b%%2Bc: SigV4 canonicalises a space as %%20 and a plus as %%2B, so the request must send the same", got)
		}
	})

	t.Run("a listing that fails is no upload failure", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name   string
			answer seenResponse
		}{
			{
				name:   "the store refuses the listing",
				answer: seenResponse{Status: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: []byte(`<Error><Code>AccessDenied</Code></Error>`)},
			},
			{
				name:   "the answer is not a listing",
				answer: seenResponse{Status: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: []byte("this is not a listing\n")},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				dc := newDeployCase(t)
				dc.config.ListBucket = true
				dc.store.setRespond(func(seenRequest) seenResponse { return tc.answer })

				err := dc.deploy(t)
				if !errors.Is(err, deploy.ErrList) {
					t.Fatalf("Deploy with %s = %v, want an error wrapping ErrList: the listing failed before any upload, so the run is no failed upload", tc.name, err)
				}

				if errors.Is(err, deploy.ErrUpload) {
					t.Errorf("%v also wraps ErrUpload, want ErrList alone: a failed listing uploaded nothing, and the two failures name different repairs", err)
				}

				if puts := dc.store.puts(); len(puts) != 0 {
					t.Errorf("the object store was sent %d PUTs, want none: the listing refuses before any upload", len(puts))
				}
			})
		}
	})
}
