package deploy_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
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

		dc := newDeployCase(t)
		dc.config.ListBucket = true

		dc.store.setRespond(listResponder(t, listPage(testBucket, []string{
			"../x",
			"/etc/passwd",
			"index%2F..%2Findex.html",
			"a%2Fb",
		}, "")))

		err := dc.deploy(t)
		if err != nil && !errors.Is(err, deploy.ErrUnknownObject) {
			t.Fatalf("Deploy with hostile keys in the bucket = %v, want nil or an error wrapping ErrUnknownObject: either the listing refuses or the deploy proceeds, never a write of a hostile key", err)
		}

		allowed := dc.bundle.keys()

		for _, request := range dc.store.seen() {
			switch request.Method {
			case http.MethodGet, http.MethodPut:
			default:
				t.Errorf("the object store received a %s of %q, want GET (a listing) or PUT (an object of the bundle) only", request.Method, request.Path)
			}

			if request.Method != http.MethodPut {
				continue
			}

			key := objectKey(request.Path)
			if key != deploy.ManifestName && !slices.Contains(allowed, key) {
				t.Errorf("an object was PUT to %q (the key %q), want a name of the bundle only: a hostile listing key must not become an object key", request.Path, key)
			}

			if strings.Contains(key, "..") || strings.Contains(key, "%2F") || strings.HasPrefix(key, "/") {
				t.Errorf("an object was PUT to %q, want no path traversal, no absolute key and no encoded separator", request.Path)
			}
		}
	})
}
