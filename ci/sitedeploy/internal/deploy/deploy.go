package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The errors Deploy and the parsers return; compare with errors.Is. Each
// error Deploy returns wraps exactly one of them (and, for a cancelled run,
// also ctx's error).
var (
	// ErrInvalidConfig is a Config Deploy refuses before any request.
	ErrInvalidConfig = errors.New("invalid deploy configuration")
	// ErrInvalidBundle is a bundle or manifest Deploy refuses before any
	// request.
	ErrInvalidBundle = errors.New("invalid site bundle")
	// ErrRollback is the rollback guard's refusal: the live page is a
	// genuine release higher than the bundle's. Nothing was uploaded.
	ErrRollback = errors.New("refused to roll the site back")
	// ErrUnknownObject is the listing's refusal (Config.ListBucket): the
	// bucket holds a key that is in no release's manifest. Nothing was
	// uploaded.
	ErrUnknownObject = errors.New("the bucket holds an object no release names")
	// ErrUpload is a failed upload to the bucket (a transport error or a
	// status other than 200 to a PUT); a failed listing is ErrList. The
	// pages are uploaded last, so a failure before them leaves every page as
	// it was.
	ErrUpload = errors.New("upload to the bucket failed")
	// ErrList is a failed listing of the bucket (Config.ListBucket): a
	// transport error, a status other than 200, an answer that does not
	// parse or a listing out of bounds. Nothing was uploaded.
	ErrList = errors.New("listing the bucket failed")
	// ErrNoLiveVersion is LiveVersion's: the page shows no version it can
	// read.
	ErrNoLiveVersion = errors.New("the live page shows no readable version")
)

// Config is everything one deploy needs. ci/sitedeploy builds it from its
// flags and the step's environment; tests build it against an httptest
// server and fstest.MapFS trees.
type Config struct {
	// Endpoint is the S3 endpoint's base URL, scheme and host and no path,
	// query or user info, such as "https://s3.fr-par.scw.cloud". Requests
	// are path-style: <Endpoint>/<Bucket>/<key>. The scheme http is accepted
	// only for the tests' fake servers: ci/sitedeploy always passes https.
	Endpoint string
	// Bucket is the site's bucket. It must be a valid S3 bucket name
	// (3 to 63 of [a-z0-9.-], starting and ending with a letter or digit).
	Bucket string
	// Region is the SigV4 signing region, such as "fr-par".
	Region string
	// Credentials is the deploy key. Both halves are required.
	Credentials Credentials
	// Client sends every request: the uploads, the listing and the live
	// page's GET. Deploy makes its own copy whose CheckRedirect follows no
	// redirect, so a redirect cannot carry a signed request or the guard's
	// read to another host. Required.
	Client *http.Client

	// Bundle is the root of the bundle to deploy (doc.go, "The bundle it
	// consumes"), such as os.DirFS of the release's unpacked bundle.
	Bundle fs.FS
	// Version is the bundle's release tag, vMAJOR.MINOR.PATCH. It must equal
	// the version of the bundle's manifest.
	Version string

	// LivePage is the URL of the live site's root page, whose version the
	// rollback guard reads, such as "https://www.shift2eu.com/". The scheme
	// http is accepted only for the tests' fake servers: ci/sitedeploy
	// always passes https.
	LivePage string
	// Releases is the repository's release tags on main, as the release's
	// latest job lists them. Each must be vMAJOR.MINOR.PATCH; Deploy
	// refuses the Config otherwise.
	Releases []string
	// Manifests holds the manifests of earlier releases, one file
	// "<tag>.json" per tag of Releases that has a site bundle (ParseManifest
	// reads each). A tag with no file has no manifest: a live page claiming
	// it is not trusted. The names Deploy opens are built from Releases,
	// never from the live page or the bucket. Required.
	Manifests fs.FS
	// ListBucket turns on the listing before the upload (doc.go, step 3).
	// Off by default: it needs the ListBucket grant of runbook step R7.
	ListBucket bool

	// Summary receives the run's visible notices and warnings (the rollback
	// guard's overwrites) and one closing line, as Markdown for the job
	// summary. Required.
	Summary io.Writer
	// Log receives one line per request (method, key, status). Required.
	Log io.Writer
	// Now is the clock that dates the signatures. Required.
	Now func() time.Time
}

// The shapes Deploy holds the configuration to.
var (
	bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// run is one deploy: the validated configuration, the bundle read into
// memory, and what every request and every text it writes goes through.
type run struct {
	cfg      Config
	client   *http.Client
	endpoint *url.URL
	signer   Signer
	scrubber *strings.Replacer

	manifest Manifest
	// files maps every file of the manifest, and manifest.json, to the bytes
	// read and checked: the bytes uploaded.
	files map[string][]byte
}

// Deploy deploys cfg.Bundle to cfg.Bucket as doc.go describes: it validates
// the configuration and the bundle, runs the rollback guard, lists the bucket
// when cfg.ListBucket asks, and uploads the assets, the manifest and the
// pages, in that order. It returns nil once every file is uploaded, and
// otherwise an error wrapping ErrInvalidConfig, ErrInvalidBundle,
// ErrRollback, ErrList, ErrUnknownObject or ErrUpload (or ctx's error), whose
// text never holds either key nor any text a server wrote.
func Deploy(ctx context.Context, cfg Config) error {
	r, err := prepare(cfg)
	if err != nil {
		return err
	}

	if err := r.guard(ctx); err != nil {
		return err
	}

	if cfg.ListBucket {
		if err := r.listBucket(ctx); err != nil {
			return err
		}
	}

	order := r.uploadOrder()

	for _, name := range order {
		if err := r.upload(ctx, name); err != nil {
			return err
		}
	}

	r.summaryf("Deployed %s to the bucket %s: %d files uploaded.\n", cfg.Version, cfg.Bucket, len(order))

	return nil
}

// prepare validates cfg and reads and checks the bundle, sending no request.
func prepare(cfg Config) (*run, error) {
	r := &run{cfg: cfg, scrubber: newScrubber(cfg.Credentials)}

	endpoint, err := r.validateConfig()
	if err != nil {
		return nil, r.fail(ErrInvalidConfig, "%s", err)
	}

	r.endpoint = endpoint

	r.manifest, r.files, err = readBundle(cfg.Bundle, cfg.Version)
	if err != nil {
		return nil, r.fail(ErrInvalidBundle, "%s", err)
	}

	client := *cfg.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r.client = &client

	r.signer = Signer{Credentials: cfg.Credentials, Region: cfg.Region, Service: ServiceS3}

	return r, nil
}

// validateConfig checks every field of the Config and returns the parsed
// endpoint.
func (r *run) validateConfig() (*url.URL, error) {
	cfg := r.cfg

	switch {
	case cfg.Client == nil:
		return nil, errors.New("no HTTP client")
	case cfg.Summary == nil:
		return nil, errors.New("no summary writer")
	case cfg.Log == nil:
		return nil, errors.New("no log writer")
	case cfg.Now == nil:
		return nil, errors.New("no clock")
	case cfg.Bundle == nil:
		return nil, errors.New("no bundle")
	case cfg.Manifests == nil:
		return nil, errors.New("no release manifests")
	case cfg.Credentials.AccessKey.IsZero() || cfg.Credentials.SecretKey.IsZero():
		return nil, errors.New("the deploy key is incomplete")
	case !bucketPattern.MatchString(cfg.Bucket) || strings.Contains(cfg.Bucket, ".."):
		return nil, fmt.Errorf("the bucket %s is not an S3 bucket name", quote(cfg.Bucket))
	case !regionPattern.MatchString(cfg.Region):
		return nil, fmt.Errorf("the region %s is not a region name", quote(cfg.Region))
	case !isTag(cfg.Version):
		return nil, fmt.Errorf("the version %s is not a release tag", quote(cfg.Version))
	}

	for _, tag := range cfg.Releases {
		if !isTag(tag) {
			return nil, fmt.Errorf("the release %s is not a release tag", quote(tag))
		}
	}

	endpoint, err := parseBaseURL(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("the endpoint: %w", err)
	}

	if endpoint.Path != "" {
		return nil, errors.New("the endpoint has a path")
	}

	if _, err := parseBaseURL(cfg.LivePage); err != nil {
		return nil, fmt.Errorf("the live page: %w", err)
	}

	return endpoint, nil
}

// parseBaseURL parses an http or https URL with a host and no user info,
// query or fragment.
func parseBaseURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("not a URL")
	}

	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return nil, errors.New("not an http or https URL")
	case u.Host == "" || u.Opaque != "":
		return nil, errors.New("no host")
	case u.User != nil:
		return nil, errors.New("user info in the URL")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return nil, errors.New("a query or fragment in the URL")
	}

	return u, nil
}

// newScrubber returns the replacer that removes both halves of the key from
// every text Deploy writes, the longer value first.
func newScrubber(creds Credentials) *strings.Replacer {
	values := []string{creds.SecretKey.reveal(), creds.AccessKey.reveal()}
	if len(values[1]) > len(values[0]) {
		values[0], values[1] = values[1], values[0]
	}

	var pairs []string

	for _, v := range values {
		if v != "" {
			pairs = append(pairs, v, redacted)
		}
	}

	return strings.NewReplacer(pairs...)
}

// scrub removes both halves of the key from s.
func (r *run) scrub(s string) string {
	return r.scrubber.Replace(s)
}

// fail returns an error wrapping kind, whose text is the formatted detail
// scrubbed of the key. Only the sentinels are wrapped, never an error whose
// text could hold server-controlled bytes.
func (r *run) fail(kind error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", kind, r.scrub(fmt.Sprintf(format, args...)))
}

// failRequest is fail for a request's failure: it also wraps ctx's error
// when the run was cancelled or ran out of time.
func (r *run) failRequest(ctx context.Context, kind error, format string, args ...any) error {
	err := r.fail(kind, format, args...)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", err, ctxErr)
	}

	return err
}

// transportFailure classifies a failed request as a fixed text: "timed out",
// "cancelled" or "transport error". The error's own text is never written,
// since net/http's messages can quote what a server sent.
func transportFailure(err error) string {
	var timeout interface{ Timeout() bool }

	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "transport error"
	}
}

// logf writes one scrubbed line to the log.
func (r *run) logf(format string, args ...any) {
	_, _ = io.WriteString(r.cfg.Log, r.scrub(fmt.Sprintf(format, args...))+"\n")
}

// summaryf writes scrubbed Markdown to the summary.
func (r *run) summaryf(format string, args ...any) {
	_, _ = io.WriteString(r.cfg.Summary, r.scrub(fmt.Sprintf(format, args...)))
}
