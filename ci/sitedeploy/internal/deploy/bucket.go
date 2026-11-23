package deploy

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// The bounds on what Deploy reads from the store: an error answer, a page of
// the listing, the number of pages and a continuation token.
const (
	maxErrorBytes   = 64 << 10
	maxListBytes    = 8 << 20
	maxListPages    = 1000
	maxTokenBytes   = 1024
	maxUnknownNamed = 20
)

// s3Code is the shape of an S3 error code Deploy names in an error.
var s3Code = regexp.MustCompile(`^[A-Za-z]{1,64}$`)

// The cache policies of doc.go's header table.
const (
	cacheImmutable = "public, max-age=31536000, immutable"
	cacheShort     = "public, max-age=300"
)

// uploadOrder returns the objects in the order Deploy uploads them: every
// content-addressed asset, then manifest.json, then the pages, each group
// sorted (the manifest's files are sorted already).
func (r *run) uploadOrder() []string {
	var assets, pages []string

	for _, f := range r.manifest.Files {
		if pageName.MatchString(f.Path) {
			pages = append(pages, f.Path)
		} else {
			assets = append(assets, f.Path)
		}
	}

	return slices.Concat(assets, []string{ManifestName}, pages)
}

// object returns the bytes to PUT for name and its headers (doc.go's table).
func (r *run) object(name string) ([]byte, http.Header, error) {
	header := http.Header{}

	body, ok := r.files[name]
	if !ok {
		return nil, nil, errors.New("no such file in the checked bundle")
	}

	switch {
	case name == ManifestName:
		header.Set("Content-Type", "application/json")
		header.Set("Cache-Control", cacheShort)
	case strings.HasSuffix(name, ".html"):
		header.Set("Content-Type", "text/html; charset=utf-8")
		header.Set("Cache-Control", cacheShort)
	case strings.HasSuffix(name, ".css"):
		header.Set("Content-Type", "text/css; charset=utf-8")
		header.Set("Cache-Control", cacheImmutable)
	default:
		header.Set("Content-Type", "text/javascript; charset=utf-8")
		header.Set("Cache-Control", cacheImmutable)
	}

	if strings.HasPrefix(name, "module.") {
		compressed, err := gzipped(body)
		if err != nil {
			return nil, nil, err
		}

		header.Set("Content-Encoding", "gzip")

		return compressed, header, nil
	}

	return body, header, nil
}

// gzipped compresses body at gzip.BestCompression with no name and a zero
// modification time, so the same bytes give the same output.
func gzipped(body []byte) ([]byte, error) {
	var buf bytes.Buffer

	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}

	if _, err := w.Write(body); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// objectURL returns the path-style URL of an object: <Endpoint>/<Bucket>/<name>.
// name is one of the bundle's fixed names, so it needs no escaping and
// cannot leave the bucket.
func (r *run) objectURL(name string) *url.URL {
	return &url.URL{Scheme: r.endpoint.Scheme, Host: r.endpoint.Host, Path: "/" + r.cfg.Bucket + "/" + name}
}

// upload PUTs one object of the bundle.
func (r *run) upload(ctx context.Context, name string) error {
	if name != ManifestName && !isBundleFile(name) {
		return r.fail(ErrUpload, "refused to upload %s, which is no file of a bundle", quote(name))
	}

	body, header, err := r.object(name)
	if err != nil {
		return r.fail(ErrUpload, "prepare %s: %s", name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, r.objectURL(name).String(), bytes.NewReader(body))
	if err != nil {
		return r.fail(ErrUpload, "build the PUT of %s", name)
	}

	req.Header = header

	status, answer, err := r.send(ctx, req, HashPayload(body), maxErrorBytes)
	if err != nil {
		r.logf("PUT %s failed", name)

		return r.failRequest(ctx, ErrUpload, "PUT %s: %s", name, err)
	}

	r.logf("PUT %s %d", name, status)

	if status != http.StatusOK {
		return r.fail(ErrUpload, "PUT %s: HTTP %d%s", name, status, r.errorCode(answer))
	}

	return nil
}

// send signs req and sends it, reading at most limit bytes of the answer.
// Its error's text is fixed: Sign's own, or the transport failure's
// classification (transportFailure), never the HTTP client's message.
func (r *run) send(ctx context.Context, req *http.Request, payloadHash string, limit int64) (int, []byte, error) {
	if err := r.signer.Sign(req, payloadHash, r.cfg.Now()); err != nil {
		return 0, nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, errors.New(transportFailure(err))
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil && ctx.Err() != nil {
		return 0, nil, errors.New(transportFailure(ctx.Err()))
	}

	return resp.StatusCode, body, nil
}

// errorCode returns ", S3 error <Code>" for an S3 error answer whose code
// has the shape of one and holds neither half of the key, and "" otherwise.
// Nothing else of an answer, body or header, is ever written.
func (r *run) errorCode(answer []byte) string {
	var doc struct {
		Code string `xml:"Code"`
	}

	if xml.Unmarshal(answer, &doc) != nil || !s3Code.MatchString(doc.Code) || r.scrub(doc.Code) != doc.Code {
		return ""
	}

	return ", S3 error " + doc.Code
}

// listBucket lists the bucket (doc.go, step 3) and fails with
// ErrUnknownObject on a key that is in no release's manifest, not in this
// bundle and not manifest.json. A listed key is only compared, never sent.
// It keeps a count of the unknown keys and only the maxUnknownNamed smallest
// of them, the ones its error names, so its memory does not grow with the
// listing.
func (r *run) listBucket(ctx context.Context) error {
	known := r.knownKeys()

	var (
		smallest []string
		count    int
	)

	token := ""

	for page := 0; ; page++ {
		if page == maxListPages {
			return r.fail(ErrList, "the listing has more than %d pages", maxListPages)
		}

		result, err := r.listPage(ctx, token)
		if err != nil {
			return err
		}

		for _, object := range result.Contents {
			if _, ok := known[object.Key]; ok {
				continue
			}

			var kept bool

			smallest, kept = keepSmallest(smallest, object.Key, maxUnknownNamed)
			if kept {
				count++
			}
		}

		if !result.IsTruncated {
			break
		}

		token = result.NextContinuationToken
		if token == "" || len(token) > maxTokenBytes {
			return r.fail(ErrList, "the listing is truncated without a usable continuation token")
		}
	}

	if count == 0 {
		return nil
	}

	named := make([]string, 0, len(smallest))
	for _, key := range smallest {
		// Scrubbed before quote cuts it, so a cut cannot leave a prefix of
		// the key that the scrubber no longer recognises.
		named = append(named, quote(r.scrub(key)))
	}

	more := ""
	if count > len(smallest) {
		more = " and others"
	}

	return r.fail(ErrUnknownObject, "%d keys in no release's manifest: %s%s", count, strings.Join(named, ", "), more)
}

// keepSmallest inserts key into sorted, a sorted list of at most limit
// distinct keys, keeping only the limit smallest. It reports false only for
// a key sorted already holds, a repeat that must not be counted twice; a key
// the list holds no room for is still a new key.
func keepSmallest(sorted []string, key string, limit int) ([]string, bool) {
	i, found := slices.BinarySearch(sorted, key)
	if found {
		return sorted, false
	}

	if i < limit {
		sorted = slices.Insert(sorted, i, key)
		if len(sorted) > limit {
			sorted = sorted[:limit]
		}
	}

	return sorted, true
}

// listResult is one page of a ListObjectsV2 answer.
type listResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

// listPage sends one ListObjectsV2 request and parses its answer.
func (r *run) listPage(ctx context.Context, token string) (listResult, error) {
	query := url.Values{"list-type": {"2"}}
	if token != "" {
		query.Set("continuation-token", token)
	}

	// The query is sent exactly as Sign canonicalises it (a space as %20, a
	// plus as %2B), so the store reads the token the signature covers.
	u := &url.URL{Scheme: r.endpoint.Scheme, Host: r.endpoint.Host, Path: "/" + r.cfg.Bucket, RawQuery: canonicalQuery(query)}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return listResult{}, r.fail(ErrList, "build the listing")
	}

	status, answer, err := r.send(ctx, req, HashPayload(nil), maxListBytes+1)
	if err != nil {
		r.logf("GET /%s?list-type=2 failed", r.cfg.Bucket)

		return listResult{}, r.failRequest(ctx, ErrList, "list the bucket: %s", err)
	}

	r.logf("GET /%s?list-type=2 %d", r.cfg.Bucket, status)

	if status != http.StatusOK {
		return listResult{}, r.fail(ErrList, "list the bucket: HTTP %d%s", status, r.errorCode(answer))
	}

	if len(answer) > maxListBytes {
		return listResult{}, r.fail(ErrList, "a page of the listing is over %d bytes", maxListBytes)
	}

	var result listResult
	if xml.Unmarshal(answer, &result) != nil {
		return listResult{}, r.fail(ErrList, "a page of the listing does not parse")
	}

	return result, nil
}

// knownKeys returns every key a release names: this bundle's files,
// manifest.json, and every file of the manifest of each tag of
// Config.Releases that has one. A manifest that does not parse names
// nothing, and the summary says so.
func (r *run) knownKeys() map[string]struct{} {
	known := map[string]struct{}{ManifestName: {}}

	for name := range r.files {
		known[name] = struct{}{}
	}

	for _, tag := range r.cfg.Releases {
		m, err := r.releaseManifest(tag)
		if err != nil {
			if !errors.Is(err, errNoManifest) {
				r.summaryf("> [!WARNING]\n> The listing counts no key of release %s: %s.\n\n", tag, err)
			}

			continue
		}

		for _, f := range m.Files {
			known[f.Path] = struct{}{}
		}
	}

	return known
}
