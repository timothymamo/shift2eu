package deploy

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"time"
)

// The errors Deploy and the parsers return; compare with errors.Is. Each
// error Deploy returns wraps exactly one of them.
var (
	// ErrNotImplemented is returned by every entry point until the
	// behaviour lands (test first, PRD #297 milestone 3).
	ErrNotImplemented = errors.New("site deploy: not implemented")
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
	// ErrUpload is a failed request to the bucket (a transport error or a
	// status other than 200). The pages are uploaded last, so a failure
	// before them leaves every page as it was.
	ErrUpload = errors.New("upload to the bucket failed")
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
	// are path-style: <Endpoint>/<Bucket>/<key>.
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
	// rollback guard reads, such as "https://www.shift2eu.com/".
	LivePage string
	// Releases is the repository's release tags on main, as the release's
	// latest job lists them. Each must be vMAJOR.MINOR.PATCH; Deploy
	// refuses the Config otherwise.
	Releases []string
	// Manifests holds the manifests of earlier releases, one file
	// "<tag>.json" per tag of Releases that has a site bundle (ParseManifest
	// reads each). A tag with no file has no manifest: a live page claiming
	// it is not trusted. The names Deploy opens are built from Releases,
	// never from the live page or the bucket.
	Manifests fs.FS
	// ListBucket turns on the listing before the upload (doc.go, step 3).
	// Off by default: it needs the ListBucket grant of runbook step R7.
	ListBucket bool

	// Summary receives the run's visible warnings (the rollback guard's
	// overwrites) and one closing line, as Markdown for the job summary.
	// Required.
	Summary io.Writer
	// Log receives one line per request (method, key, status). Required.
	Log io.Writer
	// Now is the clock that dates the signatures. Required.
	Now func() time.Time
}

// Deploy deploys cfg.Bundle to cfg.Bucket as doc.go describes: it validates
// the configuration and the bundle, runs the rollback guard, lists the bucket
// when cfg.ListBucket asks, and uploads the assets, the manifest and the
// pages, in that order. It returns nil once every file is uploaded, and
// otherwise an error wrapping ErrInvalidConfig, ErrInvalidBundle,
// ErrRollback, ErrUnknownObject or ErrUpload (or ctx's error), whose text
// never holds either key.
func Deploy(ctx context.Context, cfg Config) error {
	_, _ = ctx, cfg

	return ErrNotImplemented
}
